package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"golang.org/x/mod/semver"

	"github.com/Luqueee/ladygraph/internal/app"
	"github.com/Luqueee/ladygraph/internal/config"
	"github.com/Luqueee/ladygraph/internal/facts"
	"github.com/Luqueee/ladygraph/internal/goworkspace"
	"github.com/Luqueee/ladygraph/internal/hotsnapshot"
	"github.com/Luqueee/ladygraph/internal/indexer"
	"github.com/Luqueee/ladygraph/internal/indexing"
	"github.com/Luqueee/ladygraph/internal/logging"
	mcpserver "github.com/Luqueee/ladygraph/internal/mcp"
	"github.com/Luqueee/ladygraph/internal/rebuild"
	"github.com/Luqueee/ladygraph/internal/storage/generation"
	"github.com/Luqueee/ladygraph/internal/storage/ladybug"
	"github.com/Luqueee/ladygraph/internal/synthetic"
	"github.com/Luqueee/ladygraph/internal/update"
	"github.com/Luqueee/ladygraph/internal/upgrade"
	"github.com/Luqueee/ladygraph/internal/version"
	"github.com/Luqueee/ladygraph/internal/webapi"
	"github.com/Luqueee/ladygraph/internal/webassets"
	"github.com/Luqueee/ladygraph/internal/workspace"
)

type mcpRunner func(context.Context) error

// helpRequested reports an explicit --help before any other parsing, so the
// two long-running commands answer it like every other command instead of
// starting a server or logging a parse error.
func helpRequested(args []string) bool {
	for _, argument := range args {
		switch argument {
		case "--help", "-h":
			return true
		case "--":
			return false
		}
	}
	return false
}

func main() {
	logger := logging.New(os.Stderr)
	if len(os.Args) >= 2 && helpRequested(os.Args[2:]) {
		switch os.Args[1] {
		case "ui":
			configPath, address := "", ""
			writeCommandHelp(os.Stdout, "ui", uiFlagSet(&configPath, &address))
			return
		case "serve":
			configPath := ""
			writeCommandHelp(os.Stdout, "serve", serveFlagSet(&configPath))
			return
		}
	}
	if len(os.Args) >= 2 && os.Args[1] == "ui" {
		logger.Info("starting web viewer", "command", "ui")
		ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
		defer stop()
		if err := runConfiguredUI(ctx, os.Args[2:], func(ctx context.Context, address string, handler http.Handler) error {
			if !isLoopbackListenAddress(address) {
				logger.Warn("web viewer is unauthenticated and exposes source metadata", "address", address)
			}
			return webapi.Run(ctx, address, handler, webapi.OnListen(func(bound net.Addr) {
				logger.Info("web viewer listening",
					"command", "ui", "address", bound.String(), "url", "http://"+bound.String())
			}))
		}, webassets.Available()); err != nil {
			logger.Error("web viewer stopped with error", "command", "ui", "error", err)
			os.Exit(1)
		}
		logger.Info("web viewer stopped", "command", "ui")
		return
	}
	if len(os.Args) >= 2 && os.Args[1] == "serve" {
		logger.Info("starting MCP server", "command", "serve")
		ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
		defer stop()
		if err := runConfiguredServe(ctx, os.Args[2:], func(ctx context.Context, store *hotsnapshot.SnapshotStore, indexer indexing.ProjectIndexer) error {
			return mcpserver.RunWithSnapshotStoreAndIndexer(ctx, store, indexer)
		}); err != nil {
			logger.Error("MCP server stopped with error", "command", "serve", "error", err)
			os.Exit(1)
		}
		logger.Info("MCP server stopped", "command", "serve")
		return
	}

	// A release notice is deliberately limited to the bare interactive
	// invocation. It is cached and time-bounded so commands and scripts never
	// acquire a network dependency.
	if len(os.Args) == 1 && isTerminal(os.Stderr) {
		writeUpdateNotice(os.Stderr)
	}

	// A one-shot command reports to whoever is listening: plain text for the
	// operator at a terminal, the structured record other tooling parses when
	// stderr is a pipe or a file. serve and ui above always log structurally,
	// because a client reads their stderr for the life of the process.
	if isTerminal(os.Stderr) {
		os.Exit(run(os.Args, os.Stdout, os.Stderr))
	}
	exitCode := run(os.Args, os.Stdout, logging.NewErrorWriter(logger))
	if exitCode != 0 {
		logger.Error("command failed", "command", "cli", "exit_code", exitCode)
	}
	os.Exit(exitCode)
}

func writeUpdateNotice(stderr io.Writer) {
	result, err := update.CheckNotice(context.Background(), update.NoticeOptions{
		APIBaseURL:     os.Getenv("LADYGRAPH_UPDATE_API_URL"),
		CurrentVersion: version.Value,
		Token:          os.Getenv("LADYGRAPH_GITHUB_TOKEN"),
	})
	if err != nil || !result.UpdateAvailable {
		return
	}
	writeWarning(stderr, "ladygraph update available: %s -> %s; run \"ladygraph update\" to install it",
		result.CurrentVersion, result.LatestVersion)
}

// runServe owns the MCP loop through the application lifecycle. MCP's stdio
// session is closed by cancelling the shared context; future long-lived
// components register their watcher, worker, connections, and database with
// the same lifecycle before this function waits.
func runServe(ctx context.Context, runMCP mcpRunner) error {
	if ctx == nil {
		ctx = context.Background()
	}
	lifecycle := app.NewLifecycle(ctx)
	if err := lifecycle.Go("mcp", app.RunFunc(runMCP)); err != nil {
		return err
	}

	runDone := make(chan error, 1)
	go func() { runDone <- lifecycle.Wait() }()
	select {
	case runErr := <-runDone:
		return errors.Join(runErr, lifecycle.Shutdown(context.Background()))
	case <-ctx.Done():
		return lifecycle.Shutdown(context.Background())
	}
}

type storageDiagnoser func(context.Context, string) (ladybug.StorageDiagnosis, error)

type graphRebuilder func(context.Context, rebuild.Options) (rebuild.Report, error)
type configuredMCPRunner func(context.Context, *hotsnapshot.SnapshotStore, indexing.ProjectIndexer) error
type configuredWebRunner func(context.Context, string, http.Handler) error

// ensureConfiguration writes the default configuration when there is none.
//
// An MCP client starts its servers itself: it spawns `ladygraph serve` and
// speaks the protocol over the pipe. A server that exits because nobody ran
// `init` first turns installing the integration into a terminal session, and
// the client only reports that the server failed. Creating the defaults costs
// two files and leaves the graph exactly as empty as it was: this registers no
// repository and indexes nothing, so the first query still answers
// INDEX_NOT_READY until someone asks for an index.
//
// Only an absent configuration is created. One that exists and cannot be read
// is a failure, never something to overwrite.
func ensureConfiguration(configPath string) error {
	resolved := strings.TrimSpace(configPath)
	if resolved == "" {
		defaultPath, err := config.DefaultConfigPath()
		if err != nil {
			return fmt.Errorf("resolve configuration path: %w", err)
		}
		resolved = defaultPath
	}
	if _, err := os.Stat(resolved); err == nil {
		return nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("inspect configuration %q: %w", resolved, err)
	}
	result, err := config.Initialize(config.InitOptions{ConfigPath: configPath})
	if err != nil {
		return fmt.Errorf("initialize configuration: %w", err)
	}
	logger := logging.New(os.Stderr)
	logger.Info("created the default configuration",
		"config", result.ConfigPath, "repositories", result.RepositoriesPath)
	return nil
}

func loadConfiguredSnapshot(ctx context.Context, configPath string) (config.Loaded, *hotsnapshot.SnapshotStore, error) {
	if err := ensureConfiguration(configPath); err != nil {
		return config.Loaded{}, nil, err
	}
	loaded, err := config.Load(configPath)
	if err != nil {
		return config.Loaded{}, nil, fmt.Errorf("load configuration: %w", err)
	}
	layout, err := rebuild.Roles(ctx, rebuild.LayoutOptions{
		Root:  filepath.Dir(loaded.Config.Storage.DatabasePath),
		Store: generation.DefaultConfig(),
	})
	if err != nil {
		return config.Loaded{}, nil, fmt.Errorf("resolve active generation: %w", err)
	}
	store := hotsnapshot.NewSnapshotStore(nil)
	keepStore := false
	defer func() {
		if !keepStore {
			store.Close()
		}
	}()
	if layout.Active.ID != "" {
		generationNumber, err := strconv.ParseUint(layout.Active.ID, 10, 64)
		if err != nil {
			return config.Loaded{}, nil, fmt.Errorf("parse active generation %q: %w", layout.Active.ID, err)
		}
		snapshot, report, err := rebuild.BuildSnapshot(ctx, rebuild.BuildSnapshotOptions{
			DatabasePath: layout.Active.DatabasePath,
			SnapshotID:   generationNumber,
		})
		if err != nil {
			return config.Loaded{}, nil, fmt.Errorf("build active snapshot %q: %w", layout.Active.ID, err)
		}
		if !report.Passed {
			return config.Loaded{}, nil, fmt.Errorf("build active snapshot %q did not pass", layout.Active.ID)
		}
		if err := store.Publish(snapshot); err != nil {
			return config.Loaded{}, nil, fmt.Errorf("publish active snapshot %q: %w", layout.Active.ID, err)
		}
	}
	keepStore = true
	return loaded, store, nil
}

func runConfiguredUI(
	ctx context.Context,
	args []string,
	runWeb configuredWebRunner,
	assetsAvailable bool,
) error {
	if ctx == nil {
		ctx = context.Background()
	}
	if runWeb == nil {
		return errors.New("ui: web runner is required")
	}
	configPath := ""
	address := ""
	flags := uiFlagSet(&configPath, &address)
	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() != 0 {
		return fmt.Errorf("ui: unexpected arguments: %v", flags.Args())
	}
	// The published MCP bundle carries no web assets, so this command could
	// only open a server whose every page says the bundle is missing.
	// Saying it here costs one line instead of a browser tab.
	if !assetsAvailable {
		return errors.New(
			"ui: this binary carries no web bundle; build one with scripts/build-bundle.sh " +
				"(without --mcp-only), or run the viewer from a source checkout with the webassets build tag")
	}
	loaded, store, err := loadConfiguredSnapshot(ctx, configPath)
	if err != nil {
		return err
	}
	defer store.Close()
	stopFollower := followPublishedGeneration(ctx, loaded, store, "ui", indexing.FollowOptions{})
	defer stopFollower()
	if address == "" {
		address = loaded.Config.Web.Address
	}
	return runWeb(ctx, address, webapi.NewHandler(store))
}

// uiFlagSet and serveFlagSet exist so the two long-running commands describe
// their flags in one place: the parser that runs them and the help that
// answers --help read the same definitions.
func uiFlagSet(configPath, address *string) *flag.FlagSet {
	flags := flag.NewFlagSet("ui", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	flags.StringVar(configPath, "config", "", "configuration file")
	flags.StringVar(address, "addr", "", "HTTP listen address")
	return flags
}

func serveFlagSet(configPath *string) *flag.FlagSet {
	flags := flag.NewFlagSet("serve", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	flags.StringVar(configPath, "config", "", "configuration file")
	return flags
}

func runConfiguredServe(ctx context.Context, args []string, runMCP configuredMCPRunner) error {
	if ctx == nil {
		ctx = context.Background()
	}
	if runMCP == nil {
		return errors.New("serve: MCP runner is required")
	}
	configPath := ""
	flags := serveFlagSet(&configPath)
	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() != 0 {
		return fmt.Errorf("serve: unexpected arguments: %v", flags.Args())
	}
	loaded, store, err := loadConfiguredSnapshot(ctx, configPath)
	if err != nil {
		return err
	}
	defer store.Close()
	projectIndexer := indexing.NewService(loaded, store, version.Value, "")
	stopFollower := followPublishedGeneration(ctx, loaded, store, "serve", indexing.FollowOptions{})
	defer stopFollower()
	return runServe(ctx, func(ctx context.Context) error {
		return runMCP(ctx, store, projectIndexer)
	})
}

// followPublishedGeneration keeps a long-running command on the generation the
// store root publishes. A server loads the HotSnapshot once, so without this
// an `index --full` run in another terminal leaves it answering from a graph
// that no longer exists, with nothing in its output to say so.
//
// The follower never fails the command: a generation it cannot build is
// logged, and the one already published keeps answering.
//
// It also never outlives the call that started it. The returned function stops
// the goroutine and waits for it, so nothing touches the store or the state
// directory after the command has returned -- a caller that owns temporary
// directories would otherwise be deleting them underneath a live follower.
func followPublishedGeneration(
	ctx context.Context,
	loaded config.Loaded,
	store *hotsnapshot.SnapshotStore,
	command string,
	options indexing.FollowOptions,
) func() {
	logger := logging.New(os.Stderr)
	options.Root = filepath.Dir(loaded.Config.Storage.DatabasePath)
	options.Store = generation.DefaultConfig()
	if options.OnPublish == nil {
		options.OnPublish = func(id uint64) {
			logger.Info("serving published generation", "command", command, "generation", id)
		}
	}
	if options.OnError == nil {
		options.OnError = func(err error) {
			logger.Error("could not follow the published generation", "command", command, "error", err)
		}
	}
	followCtx, cancel := context.WithCancel(ctx)
	done := make(chan struct{})
	go func() {
		defer close(done)
		if err := indexing.Follow(followCtx, store, options); err != nil {
			logger.Error("generation follower stopped", "command", command, "error", err)
		}
	}()
	return func() {
		cancel()
		<-done
	}
}

func isLoopbackListenAddress(address string) bool {
	host, _, err := net.SplitHostPort(address)
	if err != nil {
		return false
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

type graphVerifier func(context.Context, string) (ladybug.CanonicalIntegrityReport, error)

type graphRoleResolver func(context.Context, rebuild.LayoutOptions) (rebuild.Layout, error)

type graphRollbacker func(context.Context, rebuild.RollbackOptions) (rebuild.RollbackReport, error)

type snapshotBuilder func(context.Context, rebuild.GenerationSnapshotOptions) (*hotsnapshot.GraphSnapshot, rebuild.SnapshotReport, error)

func run(args []string, stdout, stderr io.Writer) int {
	return runWithStorageDiagnoser(args, stdout, stderr, ladybug.DiagnoseStorage)
}

func runWithStorageDiagnoser(args []string, stdout, stderr io.Writer, diagnose storageDiagnoser) int {
	return runWithGraphRebuilder(args, stdout, stderr, diagnose, rebuild.Run)
}

func runWithGraphRebuilder(args []string, stdout, stderr io.Writer, diagnose storageDiagnoser, rebuilder graphRebuilder) int {
	return runWithGraphVerifier(args, stdout, stderr, diagnose, rebuilder, ladybug.VerifyCanonicalIntegrity)
}

func runWithGraphVerifier(args []string, stdout, stderr io.Writer, diagnose storageDiagnoser, rebuilder graphRebuilder, verify graphVerifier) int {
	return runWithGraphRoles(args, stdout, stderr, diagnose, rebuilder, verify, rebuild.Roles)
}

func runWithGraphRoles(args []string, stdout, stderr io.Writer, diagnose storageDiagnoser, rebuilder graphRebuilder, verify graphVerifier, roles graphRoleResolver) int {
	return runWithGraphRollback(args, stdout, stderr, diagnose, rebuilder, verify, roles, rebuild.Rollback)
}

func runWithGraphRollback(args []string, stdout, stderr io.Writer, diagnose storageDiagnoser, rebuilder graphRebuilder, verify graphVerifier, roles graphRoleResolver, rollback graphRollbacker) int {
	return runWithSnapshotBuilder(args, stdout, stderr, diagnose, rebuilder, verify, roles, rollback, rebuild.SnapshotGeneration)
}

func runWithSnapshotBuilder(args []string, stdout, stderr io.Writer, diagnose storageDiagnoser, rebuilder graphRebuilder, verify graphVerifier, roles graphRoleResolver, rollback graphRollbacker, build snapshotBuilder) int {
	if len(args) >= 2 && args[1] == "version" {
		if len(args) == 2 {
			fmt.Fprintln(stdout, version.Value)
			return 0
		}
		if len(args) == 3 && args[2] == "--json" {
			return runVersionJSON(stdout, stderr)
		}
	}
	if len(args) >= 2 && args[1] == "update" {
		return runUpdate(args[2:], stdout, stderr)
	}
	if len(args) >= 2 && args[1] == "mcp" {
		return runMCPCommand(args[2:], stdout, stderr)
	}
	if len(args) >= 2 && args[1] == "skill" {
		return runSkillCommand(args[2:], stdout, stderr)
	}
	if len(args) >= 2 && args[1] == "init" {
		return runInit(args[2:], stdout, stderr)
	}
	if len(args) >= 2 && args[1] == "doctor" && (len(args) == 2 || (len(args) >= 3 && strings.HasPrefix(args[2], "--"))) {
		return runDoctor(args[2:], stdout, stderr)
	}
	if len(args) >= 3 && args[1] == "index" && args[2] == "--full" {
		return runIndexFull(args[3:], stdout, stderr)
	}
	if len(args) >= 2 && args[1] == "index" {
		writeCommandError(stderr, "index: only --full is supported")
		return 2
	}
	if len(args) >= 3 && args[1] == "doctor" && args[2] == "storage" {
		return runDoctorStorage(args[3:], stdout, stderr, diagnose)
	}
	if len(args) >= 3 && args[1] == "doctor" && args[2] == "graph" {
		return runDoctorGraph(args[3:], stdout, stderr, verify)
	}
	if len(args) >= 3 && args[1] == "benchmark" && args[2] == "generate-graph" {
		return runGenerateGraph(args[3:], stdout, stderr)
	}
	if len(args) >= 2 && args[1] == "upgrade" {
		return runUpgrade(args[2:], stdout, stderr)
	}
	if len(args) >= 2 && args[1] == "clean" {
		return runClean(args[2:], stdout, stderr)
	}
	if len(args) >= 2 && args[1] == "rebuild" {
		return runRebuild(args[2:], stdout, stderr, rebuilder)
	}
	if len(args) >= 3 && args[1] == "graph" && args[2] == "status" {
		return runGraphStatus(args[3:], stdout, stderr, roles)
	}
	if len(args) >= 2 && args[1] == "rollback" {
		return runRollback(args[2:], stdout, stderr, rollback)
	}
	if len(args) >= 2 && args[1] == "snapshot" {
		return runSnapshot(args[2:], stdout, stderr, build)
	}

	program := filepath.Base(args[0])
	if len(args) >= 2 {
		switch args[1] {
		case "--help", "-h", "help":
			writeHelp(stdout, program)
			return 0
		}
		writeUsageError(stderr, program, fmt.Sprintf("unknown command %q", args[1]))
		return 2
	}
	writeUsageError(stderr, program, "no command given")
	return 2
}

type updateRunner func(context.Context, update.Options) (update.Result, error)

func runUpdate(args []string, stdout, stderr io.Writer) int {
	return runUpdateWithRunner(args, stdout, stderr, update.Run)
}

func runUpdateWithRunner(args []string, stdout, stderr io.Writer, runner updateRunner) int {
	flags := flag.NewFlagSet("update", flag.ContinueOnError)
	checkOnly := false
	flags.BoolVar(&checkOnly, "check", false, "check for a newer release without installing it")
	if parsed, code := parseCommandFlags("update", flags, args, stdout, stderr); !parsed {
		return code
	}
	if flags.NArg() != 0 {
		writeCommandError(stderr, "update: unexpected arguments")
		return 2
	}
	result, err := runner(context.Background(), update.Options{
		APIBaseURL:     os.Getenv("LADYGRAPH_UPDATE_API_URL"),
		CurrentVersion: version.Value,
		Token:          os.Getenv("LADYGRAPH_GITHUB_TOKEN"),
		CheckOnly:      checkOnly,
	})
	if err != nil {
		writeCommandError(stderr, "update: %v", err)
		return 1
	}
	if !result.UpdateAvailable {
		writeSuccess(stdout, "ladygraph is up to date (%s)", result.CurrentVersion)
		return 0
	}
	if checkOnly {
		writeInfo(stdout, "ladygraph update available: %s -> %s", result.CurrentVersion, result.LatestVersion)
		return 0
	}
	if !result.Updated {
		writeCommandError(stderr, "update: release %s was not installed", result.LatestVersion)
		return 1
	}
	writeSuccess(stdout, "ladygraph updated: %s -> %s", result.CurrentVersion, result.LatestVersion)
	return 0
}

type stringList []string

func (values *stringList) String() string {
	return strings.Join(*values, ",")
}

func (values *stringList) Set(value string) error {
	value = strings.TrimSpace(value)
	if value == "" {
		return errors.New("value must not be empty")
	}
	*values = append(*values, value)
	return nil
}

func runInit(args []string, stdout, stderr io.Writer) int {
	flags := flag.NewFlagSet("init", flag.ContinueOnError)
	configPath := ""
	repositoriesPath := ""
	force := false
	languages := "go"
	var repositorySpecs stringList
	flags.StringVar(&configPath, "config", "", "configuration file")
	flags.StringVar(&repositoriesPath, "repositories", "", "repository registry file")
	flags.BoolVar(&force, "force", false, "replace existing configuration files")
	flags.StringVar(&languages, "languages", languages, "comma-separated repository languages")
	flags.Var(&repositorySpecs, "repository", "register NAME=PATH (repeatable)")
	if parsed, code := parseCommandFlags("init", flags, args, stdout, stderr); !parsed {
		return code
	}
	if flags.NArg() != 0 {
		writeCommandError(stderr, "init: unexpected arguments: %v", flags.Args())
		return 2
	}

	result, err := config.Initialize(config.InitOptions{
		ConfigPath:       configPath,
		RepositoriesPath: repositoriesPath,
		Force:            force,
	})
	if err != nil {
		writeCommandError(stderr, "init: %v", err)
		return 1
	}
	writeInfo(stdout, "config: %s (%s)", initFileState(result.ConfigCreated), result.ConfigPath)
	writeInfo(stdout, "repositories: %s (%s)", initFileState(result.RepositoriesCreated), result.RepositoriesPath)

	if len(repositorySpecs) == 0 {
		return 0
	}
	parsedLanguages, err := parseLanguages(languages)
	if err != nil {
		writeCommandError(stderr, "init: %v", err)
		return 2
	}
	additions := make([]config.Repository, 0, len(repositorySpecs))
	for _, specification := range repositorySpecs {
		repository, err := parseRepositorySpec(specification, parsedLanguages)
		if err != nil {
			writeCommandError(stderr, "init: %v", err)
			return 2
		}
		additions = append(additions, repository)
	}
	if err := config.RegisterRepositories(result.RepositoriesPath, additions); err != nil {
		writeCommandError(stderr, "init: register repositories: %v", err)
		return 1
	}
	writeSuccess(stdout, "repositories registered: %d", len(additions))
	return 0
}

func initFileState(created bool) string {
	if created {
		return "created"
	}
	return "existing"
}

func parseLanguages(value string) ([]string, error) {
	parts := strings.Split(value, ",")
	languages := make([]string, 0, len(parts))
	seen := make(map[string]struct{}, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			return nil, errors.New("languages must contain no empty values")
		}
		if _, exists := seen[part]; exists {
			return nil, fmt.Errorf("languages contains duplicate %q", part)
		}
		seen[part] = struct{}{}
		languages = append(languages, part)
	}
	if len(languages) == 0 {
		return nil, errors.New("languages must contain at least one value")
	}
	return languages, nil
}

func parseRepositorySpec(specification string, languages []string) (config.Repository, error) {
	name, path, found := strings.Cut(specification, "=")
	name = strings.TrimSpace(name)
	path = strings.TrimSpace(path)
	if !found || name == "" || path == "" {
		return config.Repository{}, fmt.Errorf("repository %q must use NAME=PATH", specification)
	}
	return config.Repository{
		Name:      name,
		Path:      path,
		Languages: append([]string(nil), languages...),
	}, nil
}

func runIndexFull(args []string, stdout, stderr io.Writer) int {
	flags := flag.NewFlagSet("index --full", flag.ContinueOnError)
	configPath := ""
	repositoriesPath := ""
	resolverVersion := version.Value
	flags.StringVar(&configPath, "config", "", "configuration file")
	flags.StringVar(&repositoriesPath, "repositories", "", "repository registry file override")
	flags.StringVar(&resolverVersion, "resolver-version", resolverVersion, "resolver version recorded in the graph")
	if parsed, code := parseCommandFlags("index --full", flags, args, stdout, stderr); !parsed {
		return code
	}
	if flags.NArg() != 0 {
		writeCommandError(stderr, "index --full: unexpected arguments: %v", flags.Args())
		return 2
	}

	ctx := context.Background()
	loaded, err := config.Load(configPath)
	if err != nil {
		writeCommandError(stderr, "index --full: load configuration: %v", err)
		return 1
	}
	if repositoriesPath != "" {
		loaded.Repositories, err = config.LoadRepositories(repositoriesPath)
		if err != nil {
			writeCommandError(stderr, "index --full: load repositories: %v", err)
			return 1
		}
	}
	registry, err := workspace.NewRegistry(ctx, loaded.Repositories)
	if err != nil {
		writeCommandError(stderr, "index --full: register repositories: %v", err)
		return 1
	}
	workingDirectory, err := os.Getwd()
	if err != nil {
		writeCommandError(stderr, "index --full: resolve working directory: %v", err)
		return 1
	}
	progressStart := time.Now()
	root := filepath.Dir(loaded.Config.Storage.DatabasePath)
	fullResult, err := indexing.RunFull(ctx, indexing.FullOptions{
		Repositories:      registry.List(),
		SyntheticWorkFile: loaded.Config.Go.SyntheticWorkFile,
		IncludeTests:      loaded.Config.Go.IncludeTests,
		GoBuildTags:       loaded.Config.Go.BuildTags,
		GoAllowNetwork:    loaded.Config.Go.AllowNetwork,
		TypeScriptWorker:  loaded.Config.TypeScript.WorkerCommand,
		WorkingDirectory:  workingDirectory,
		Root:              root,
		ResolverVersion:   resolverVersion,
		Store:             generation.DefaultConfig(),
		Progress: func(event indexer.ProgressEvent) {
			writeIndexProgress(stderr, progressStart, event)
		},
		RebuildProgress: func(stage rebuild.StageName) {
			writeInfo(stderr, "[%6.1fs] rebuild %s", time.Since(progressStart).Seconds(), stage)
		},
	})
	indexReport := fullResult.IndexReport
	writeResult(stdout, err == nil, "index.full: %s", passFail(err == nil))
	writeInfo(stdout, "index.go: repositories=%d modules=%d workspaces=%d loads=%d definitions=%d references=%d unresolved=%d diagnostics=%d",
		indexReport.GoRepositories,
		indexReport.GoModules,
		indexReport.GoWorkspaces,
		indexReport.GoLoads,
		indexReport.GoDefinitions,
		indexReport.GoReferences,
		indexReport.GoUnresolved,
		indexReport.GoLoadDiagnostics,
	)
	writeInfo(stdout, "index.typescript: repositories=%d symbols=%d references=%d unresolved=%d",
		indexReport.TypeScriptRepositories,
		indexReport.TypeScriptSymbols,
		indexReport.TypeScriptReferences,
		indexReport.TypeScriptUnresolved,
	)
	rebuildReport := fullResult.RebuildReport
	writeResult(stdout, err == nil && rebuildReport.Passed, "rebuild: %s generation=%s", passFail(err == nil && rebuildReport.Passed), rebuildReport.GenerationID)
	for _, stage := range rebuildReport.Stages {
		if stage.Detail == "" {
			writeResult(stdout, stage.Passed, "stage.%s: %s", stage.Name, passFail(stage.Passed))
			continue
		}
		writeResult(stdout, stage.Passed, "stage.%s: %s (%s)", stage.Name, passFail(stage.Passed), stage.Detail)
	}
	if err != nil {
		writeCommandError(stderr, "index --full: %v", err)
		return 1
	}
	return 0
}

// writeIndexProgress renders one indexing progress event as a single line on
// stderr, where it cannot be confused with the report stdout carries.
func writeIndexProgress(stderr io.Writer, start time.Time, event indexer.ProgressEvent) {
	elapsed := time.Since(start).Seconds()
	position := ""
	if event.Total > 0 {
		position = fmt.Sprintf(" %d/%d", event.Completed, event.Total)
	}
	subject := event.Repository
	if subject == "" {
		subject = string(event.Phase)
	} else {
		subject = fmt.Sprintf("%s %s", event.Phase, subject)
	}
	state := "done"
	if event.Started {
		state = "start"
	}
	if event.Detail == "" {
		writeInfo(stderr, "[%6.1fs]%s %s %s", elapsed, position, subject, state)
		return
	}
	writeInfo(stderr, "[%6.1fs]%s %s %s (%s)", elapsed, position, subject, state, event.Detail)
}

func runUpgrade(args []string, stdout, stderr io.Writer) int {
	flags := flag.NewFlagSet("upgrade", flag.ContinueOnError)
	configPath := ""
	repositoriesPath := ""
	resolverVersion := version.Value
	flags.StringVar(&configPath, "config", "", "configuration file")
	flags.StringVar(&repositoriesPath, "repositories", "", "repository registry file override")
	flags.StringVar(&resolverVersion, "resolver-version", resolverVersion, "resolver version recorded in the graph")
	if parsed, code := parseCommandFlags("upgrade", flags, args, stdout, stderr); !parsed {
		return code
	}
	if flags.NArg() != 0 {
		writeCommandError(stderr, "upgrade: unexpected arguments: %v", flags.Args())
		return 2
	}

	ctx := context.Background()
	loaded, err := config.Load(configPath)
	if err != nil {
		writeCommandError(stderr, "upgrade: load configuration: %v", err)
		return 1
	}
	if repositoriesPath != "" {
		loaded.Repositories, err = config.LoadRepositories(repositoriesPath)
		if err != nil {
			writeCommandError(stderr, "upgrade: load repositories: %v", err)
			return 1
		}
	}
	registry, err := workspace.NewRegistry(ctx, loaded.Repositories)
	if err != nil {
		writeCommandError(stderr, "upgrade: register repositories: %v", err)
		return 1
	}
	workingDirectory, err := os.Getwd()
	if err != nil {
		writeCommandError(stderr, "upgrade: resolve working directory: %v", err)
		return 1
	}
	root := filepath.Dir(loaded.Config.Storage.DatabasePath)
	report, err := upgrade.Run(ctx, upgrade.Options{
		Root:            root,
		BackupRoot:      loaded.Config.Storage.BackupsPath,
		ResolverVersion: resolverVersion,
		Full: indexer.FullOptions{
			Repositories:      registry.List(),
			SyntheticWorkFile: loaded.Config.Go.SyntheticWorkFile,
			IncludeTests:      loaded.Config.Go.IncludeTests,
			GoBuildTags:       loaded.Config.Go.BuildTags,
			GoAllowNetwork:    loaded.Config.Go.AllowNetwork,
			TypeScriptWorker:  loaded.Config.TypeScript.WorkerCommand,
			WorkingDirectory:  workingDirectory,
		},
	})
	for _, stage := range report.Stages {
		if stage.Detail == "" {
			writeResult(stdout, stage.Passed, "upgrade.%s: %s", stage.Name, passFail(stage.Passed))
			continue
		}
		writeResult(stdout, stage.Passed, "upgrade.%s: %s (%s)", stage.Name, passFail(stage.Passed), stage.Detail)
	}
	if report.BackupPath != "" {
		writeInfo(stdout, "upgrade.backup: %s", report.BackupPath)
	}
	if report.To.ID != "" {
		writeInfo(stdout, "upgrade.generation: %s", report.To.ID)
	}
	writeResult(stdout, err == nil && report.Passed, "upgrade: %s", passFail(err == nil && report.Passed))
	if err != nil {
		writeCommandError(stderr, "upgrade: %v", err)
		return 1
	}
	return 0
}

// runClean removes published graph generations.
//
// It reports what it would remove and changes nothing until --yes, because a
// typo here costs a full reindex and there is no undo: rollback restores a
// backup generation, and this command is what removes those too.
func runClean(args []string, stdout, stderr io.Writer) int {
	flags := flag.NewFlagSet("clean", flag.ContinueOnError)
	configPath := ""
	keepActive := false
	confirmed := false
	flags.StringVar(&configPath, "config", "", "configuration file")
	flags.BoolVar(&keepActive, "keep-active", false, "keep the generation currently published")
	flags.BoolVar(&confirmed, "yes", false, "remove the generations instead of listing them")
	if parsed, code := parseCommandFlags("clean", flags, args, stdout, stderr); !parsed {
		return code
	}
	if flags.NArg() != 0 {
		writeCommandError(stderr, "clean: unexpected arguments: %v", flags.Args())
		return 2
	}

	ctx := context.Background()
	loaded, err := config.Load(configPath)
	if err != nil {
		writeCommandError(stderr, "clean: load configuration: %v", err)
		return 1
	}
	root := filepath.Dir(loaded.Config.Storage.DatabasePath)
	store, err := generation.New(root, generation.DefaultConfig())
	if err != nil {
		writeCommandError(stderr, "clean: open generation store: %v", err)
		return 1
	}
	generations, err := store.List(ctx)
	if err != nil {
		writeCommandError(stderr, "clean: list generations: %v", err)
		return 1
	}
	active, err := store.Current(ctx)
	if err != nil && !errors.Is(err, generation.ErrNoCurrent) {
		writeCommandError(stderr, "clean: read active generation: %v", err)
		return 1
	}

	if keepActive && active.ID == "" {
		writeCommandError(stderr, "clean: --keep-active: no generation is published; run clean without it to remove everything")
		return 1
	}
	doomed := make([]string, 0, len(generations))
	for _, candidate := range generations {
		if keepActive && candidate.ID == active.ID {
			continue
		}
		doomed = append(doomed, candidate.ID)
	}
	if len(doomed) == 0 {
		writeInfo(stdout, "clean: nothing to remove (%d generation(s) kept)", len(generations))
		return 0
	}
	if !confirmed {
		writeInfo(stdout, "clean: would remove generation(s) %s from %s", strings.Join(doomed, ", "), root)
		if !keepActive {
			writeInfo(stdout, "clean: the graph would be unpublished; every query fails until the next index --full")
		}
		writeInfo(stdout, "clean: nothing was removed; pass --yes to proceed")
		return 0
	}

	removed, err := cleanGenerations(ctx, store, keepActive, active.ID)
	if err != nil {
		writeCommandError(stderr, "clean: %v", err)
		return 1
	}
	writeResult(stdout, true, "clean: removed generation(s) %s", strings.Join(removed, ", "))
	if keepActive {
		writeInfo(stdout, "clean: generation %s is still published; rollback has nothing to restore", active.ID)
		return 0
	}
	// Publish only accepts a newer identifier, and the next index starts
	// again at 000001, so a server holding the graph that was just removed
	// can never install another one.
	writeInfo(stdout, "clean: restart any running serve or ui before the next index --full")
	return 0
}

func cleanGenerations(
	ctx context.Context,
	store *generation.Store,
	keepActive bool,
	activeID string,
) ([]string, error) {
	if keepActive {
		removed, err := store.DiscardExcept(ctx, activeID)
		if err != nil {
			return nil, fmt.Errorf("discard generations: %w", err)
		}
		return removed, nil
	}
	removed, err := store.Discard(ctx)
	if err != nil {
		return nil, fmt.Errorf("discard generations: %w", err)
	}
	return removed, nil
}

func passFail(passed bool) string {
	if passed {
		return "PASS"
	}
	return "FAIL"
}

func runDoctor(args []string, stdout, stderr io.Writer) int {
	flags := flag.NewFlagSet("doctor", flag.ContinueOnError)
	configPath := ""
	flags.StringVar(&configPath, "config", "", "configuration file")
	if parsed, code := parseCommandFlags("doctor", flags, args, stdout, stderr); !parsed {
		return code
	}
	if flags.NArg() != 0 {
		writeCommandError(stderr, "doctor: unexpected arguments: %v", flags.Args())
		return 2
	}

	loaded, err := config.Load(configPath)
	if err != nil {
		writeResult(stdout, false, "config: FAIL (%v)", err)
		writeResult(stdout, false, "doctor: FAIL")
		return 1
	}
	failed := false
	doctorResult := func(name string, passed bool, detail string) {
		if detail == "" {
			writeResult(stdout, passed, "%s: %s", name, passFail(passed))
		} else {
			writeResult(stdout, passed, "%s: %s (%s)", name, passFail(passed), detail)
		}
		if !passed {
			failed = true
		}
	}
	doctorResult("config", true, fmt.Sprintf("schema=%d", loaded.Config.Version))

	for _, stateDirectory := range []struct {
		name string
		path string
	}{
		{name: "state.database_parent", path: filepath.Dir(loaded.Config.Storage.DatabasePath)},
		{name: "state.snapshots", path: loaded.Config.Storage.SnapshotsPath},
		{name: "state.backups", path: loaded.Config.Storage.BackupsPath},
		{name: "state.synthetic_parent", path: filepath.Dir(loaded.Config.Go.SyntheticWorkFile)},
	} {
		passed, detail := inspectDoctorDirectory(stateDirectory.path)
		doctorResult(stateDirectory.name, passed, detail)
	}

	registry, registryErr := workspace.NewRegistry(context.Background(), loaded.Repositories)
	if registryErr != nil {
		doctorResult("repositories", false, registryErr.Error())
	} else {
		doctorResult("repositories", true, fmt.Sprintf("count=%d", len(registry.List())))
	}
	if registryErr == nil {
		runDoctorToolchains(stdout, doctorResult, loaded.Config, registry.List())
	} else {
		doctorResult("toolchains", false, "repository metadata unavailable")
	}

	root := filepath.Dir(loaded.Config.Storage.DatabasePath)
	layout, layoutErr := rebuild.Roles(context.Background(), rebuild.LayoutOptions{
		Root:  root,
		Store: generation.DefaultConfig(),
	})
	if layoutErr != nil {
		doctorResult("graph.store", false, layoutErr.Error())
	} else if layout.Active.ID == "" {
		doctorResult("graph.store", true, "no published generation")
		doctorResult("snapshot", true, "no published generation")
		doctorResult("unresolved", true, "no published generation")
	} else {
		integrity, integrityErr := ladybug.VerifyCanonicalIntegrity(context.Background(), layout.Active.DatabasePath)
		if integrityErr != nil {
			doctorResult("graph.store", false, integrityErr.Error())
		} else {
			doctorResult("graph.store", integrity.Passed, fmt.Sprintf("generation=%s", layout.Active.ID))
		}
		digestPath := filepath.Join(layout.Active.Path, "snapshot.sha256")
		digestInfo, digestErr := os.Stat(digestPath)
		doctorResult("snapshot.digest", digestErr == nil && digestInfo.Mode().IsRegular(), digestPath)
		snapshotID, snapshotIDErr := snapshotReportID(layout.Active.ID)
		if snapshotIDErr != nil {
			doctorResult("snapshot", false, snapshotIDErr.Error())
			doctorResult("unresolved", false, "snapshot unavailable")
		} else {
			snapshot, snapshotReport, snapshotErr := rebuild.BuildSnapshot(context.Background(), rebuild.BuildSnapshotOptions{
				DatabasePath: layout.Active.DatabasePath,
				SnapshotID:   snapshotID,
			})
			if snapshotErr != nil {
				doctorResult("snapshot", false, snapshotErr.Error())
				doctorResult("unresolved", false, "snapshot unavailable")
			} else {
				doctorResult("snapshot", snapshot != nil && snapshotReport.Passed, fmt.Sprintf("symbols=%d", snapshotReport.Stats.Symbols))
				doctorResult("unresolved", snapshot != nil && snapshotReport.Passed, fmt.Sprintf("retained=%d", snapshotReport.Stats.Unresolved))
			}
		}
	}
	if failed {
		writeResult(stdout, false, "doctor: FAIL")
		return 1
	}
	writeResult(stdout, true, "doctor: PASS")
	return 0
}

func runDoctorToolchains(stdout io.Writer, report func(string, bool, string), configuration config.Config, repositories []workspace.Repository) {
	needsGo := false
	needsTypeScript := false
	for _, repository := range repositories {
		for _, language := range repository.Languages {
			switch strings.ToLower(strings.TrimSpace(language)) {
			case "go":
				needsGo = true
			case "typescript", "javascript", "ts", "js":
				needsTypeScript = true
			}
		}
	}
	if needsGo {
		output, err := exec.CommandContext(context.Background(), "go", "version").CombinedOutput()
		report("toolchain.go", err == nil, strings.TrimSpace(string(output)))
		reportGoLanguageCeiling(report, repositories)
	} else {
		report("toolchain.go", true, "not configured")
	}
	if !needsTypeScript {
		report("toolchain.typescript", true, "not configured")
		return
	}
	command := strings.Fields(strings.TrimSpace(configuration.TypeScript.WorkerCommand))
	if len(command) == 0 {
		report("toolchain.typescript", false, "worker command is empty")
		return
	}
	if _, err := exec.LookPath(command[0]); err == nil {
		report("toolchain.typescript", true, command[0])
		return
	}
	workingDirectory, err := os.Getwd()
	if err == nil && command[0] == "ladygraph-ts-worker" {
		factsEntry := filepath.Join(workingDirectory, "ts-worker", "src", "facts-cli.ts")
		if _, factsErr := os.Stat(factsEntry); factsErr == nil {
			if _, pnpmErr := exec.LookPath("pnpm"); pnpmErr == nil {
				report("toolchain.typescript", true, "pnpm fallback")
				return
			}
		}
	}
	report("toolchain.typescript", false, fmt.Sprintf("command %q is unavailable", command[0]))
}

// reportGoLanguageCeiling states the language version this build can type
// check and names every registered module above it.
//
// The `go` on PATH is not the number that decides whether a repository can be
// indexed: go/types travels linked inside this binary, so a module written for
// a newer language version is unreadable however new the go command is.
// Reporting only the PATH toolchain invites the opposite conclusion.
func reportGoLanguageCeiling(report func(string, bool, string), repositories []workspace.Repository) {
	ceiling := goworkspace.LanguageVersion()
	if ceiling == "" {
		report("toolchain.typecheck", true, "unknown")
		return
	}
	plan, err := goworkspace.BuildPlan(context.Background(), repositories, goworkspace.Options{})
	if err != nil {
		if errors.Is(err, goworkspace.ErrGoVersionUnsupported) {
			report("toolchain.typecheck", false, err.Error())
			return
		}
		// Everything else -- an ambiguous module, an unreadable
		// manifest -- belongs to the index, which reports it with its
		// own context. This check answers one question only.
		report("toolchain.typecheck", true, "go "+ceiling)
		return
	}
	highest := ceiling
	for _, module := range plan.Modules {
		if version := strings.TrimSpace(module.GoVersion); version != "" && semver.Compare("v"+version, "v"+highest) > 0 {
			highest = version
		}
	}
	report("toolchain.typecheck", true, fmt.Sprintf("go %s (highest registered module: go %s)", ceiling, highest))
}

func inspectDoctorDirectory(path string) (bool, string) {
	info, err := os.Stat(path)
	if err != nil {
		return false, err.Error()
	}
	if !info.IsDir() {
		return false, "not a directory"
	}
	if info.Mode().Perm()&0o077 != 0 {
		return false, fmt.Sprintf("permissions %04o are broader than 0700", info.Mode().Perm())
	}
	return true, path
}

func snapshotReportID(id string) (uint64, error) {
	value, err := strconv.ParseUint(id, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("parse generation %q: %w", id, err)
	}
	return value, nil
}

func runDoctorStorage(args []string, stdout, stderr io.Writer, diagnose storageDiagnoser) int {
	flags := flag.NewFlagSet("doctor storage", flag.ContinueOnError)
	var databasePath string
	flags.StringVar(&databasePath, "database", "", "existing LadybugDB database path")
	if parsed, code := parseCommandFlags("doctor storage", flags, args, stdout, stderr); !parsed {
		return code
	}
	if flags.NArg() != 0 {
		fmt.Fprintf(stderr, "doctor storage: unexpected arguments: %v\n", flags.Args())
		return 2
	}
	if databasePath == "" {
		fmt.Fprintln(stderr, "doctor storage: --database is required")
		return 2
	}

	diagnosis, err := diagnose(context.Background(), databasePath)
	if err != nil {
		fmt.Fprintf(stderr, "doctor storage: %v\n", err)
		return 1
	}
	state := "FAIL"
	if diagnosis.Healthy {
		state = "PASS"
	}
	fmt.Fprintf(stdout, "storage doctor: %s\n", state)
	fmt.Fprintf(stdout, "database: %s\n", diagnosis.Path)
	// A diagnosis that does not say which layout it validated cannot be
	// interpreted: the same path can hold either schema.
	if diagnosis.Schema == ladybug.SchemaCanonical {
		fmt.Fprintf(stdout, "schema: %s (version %d)\n", diagnosis.Schema, diagnosis.SchemaVersion)
	} else {
		fmt.Fprintf(stdout, "schema: %s\n", diagnosis.Schema)
	}
	for _, check := range diagnosis.Checks {
		fmt.Fprintf(stdout, "[%s] %s: %s\n", check.Status, check.Name, check.Detail)
	}
	if diagnosis.Healthy {
		return 0
	}
	return 1
}

func runDoctorGraph(args []string, stdout, stderr io.Writer, verify graphVerifier) int {
	flags := flag.NewFlagSet("doctor graph", flag.ContinueOnError)
	var databasePath string
	flags.StringVar(&databasePath, "database", "", "published canonical LadybugDB database path")
	if parsed, code := parseCommandFlags("doctor graph", flags, args, stdout, stderr); !parsed {
		return code
	}
	if flags.NArg() != 0 {
		fmt.Fprintf(stderr, "doctor graph: unexpected arguments: %v\n", flags.Args())
		return 2
	}
	if databasePath == "" {
		fmt.Fprintln(stderr, "doctor graph: --database is required")
		return 2
	}

	report, err := verify(context.Background(), databasePath)
	if err != nil {
		fmt.Fprintf(stderr, "doctor graph: %v\n", err)
		return 1
	}
	writeIntegrityReport(stdout, databasePath, report)
	if report.Passed {
		return 0
	}
	return 1
}

// writeIntegrityReport prints one line per canonical integrity rule with its
// PASS/FAIL state and violation count and, under every failed rule, the
// offending samples VerifyCanonicalIntegrity already bounded: table, key and
// detail.
func writeIntegrityReport(stdout io.Writer, databasePath string, report ladybug.CanonicalIntegrityReport) {
	state := "FAIL"
	if report.Passed {
		state = "PASS"
	}
	fmt.Fprintf(stdout, "graph doctor: %s\n", state)
	fmt.Fprintf(stdout, "database: %s\n", databasePath)
	writeIntegrityFindings(stdout, report.Findings)
}

// writeIntegrityFindings prints one line per rule with its PASS/FAIL state
// and violation count and, under every failed rule, the offending samples,
// shared by doctor graph and rollback so both name a broken invariant
// exactly the same way.
func writeIntegrityFindings(stdout io.Writer, findings []ladybug.IntegrityFinding) {
	for _, finding := range findings {
		findingState := "FAIL"
		if finding.Passed {
			findingState = "PASS"
		}
		fmt.Fprintf(stdout, "[%s] %s: %d violation(s)\n", findingState, finding.Rule, finding.Violations)
		if finding.Passed {
			continue
		}
		for _, sample := range finding.Samples {
			fmt.Fprintf(stdout, "    %s %s: %s\n", sample.Table, sample.Key, sample.Detail)
		}
	}
}

func runGenerateGraph(args []string, stdout, stderr io.Writer) int {
	config := synthetic.DefaultConfig()
	flags := flag.NewFlagSet("generate-graph", flag.ContinueOnError)
	flags.IntVar(&config.Repositories, "repositories", config.Repositories, "number of repositories")
	flags.IntVar(&config.Files, "files", config.Files, "number of files")
	flags.IntVar(&config.Symbols, "symbols", config.Symbols, "number of symbols")
	flags.IntVar(&config.Edges, "edges", config.Edges, "number of total edges")
	flags.Int64Var(&config.Seed, "seed", config.Seed, "deterministic corpus seed")
	flags.StringVar(&config.OutputDir, "output", config.OutputDir, "output directory")
	if parsed, code := parseCommandFlags("generate-graph", flags, args, stdout, stderr); !parsed {
		return code
	}
	if flags.NArg() != 0 {
		fmt.Fprintf(stderr, "generate-graph: unexpected arguments: %v\n", flags.Args())
		return 2
	}

	manifest, err := synthetic.Generate(context.Background(), config)
	if err != nil {
		fmt.Fprintf(stderr, "generate-graph: %v\n", err)
		return 1
	}
	fmt.Fprintf(stdout, "generated %d repositories, %d files, %d symbols, %d edges at %s (seed %d)\n",
		manifest.Repositories,
		manifest.Files,
		manifest.Symbols,
		manifest.Edges,
		config.OutputDir,
		manifest.Seed,
	)
	return 0
}

func runRebuild(args []string, stdout, stderr io.Writer, rebuilder graphRebuilder) int {
	flags := flag.NewFlagSet("rebuild", flag.ContinueOnError)
	var (
		factsPath       string
		root            string
		generationID    string
		resolverVersion string
		snapshotID      int64
	)
	flags.StringVar(&factsPath, "facts", "", "JSON file with a serialized facts.Set")
	flags.StringVar(&root, "root", "", "generation store root directory")
	flags.StringVar(&generationID, "generation", "", "six digit generation id to publish")
	flags.StringVar(&resolverVersion, "resolver-version", "", "resolver version stamped on every semantic edge")
	flags.Int64Var(&snapshotID, "snapshot-id", 0, "snapshot id stamped on every semantic edge")
	if parsed, code := parseCommandFlags("rebuild", flags, args, stdout, stderr); !parsed {
		return code
	}
	if flags.NArg() != 0 {
		fmt.Fprintf(stderr, "rebuild: unexpected arguments: %v\n", flags.Args())
		return 2
	}
	switch {
	case factsPath == "":
		fmt.Fprintln(stderr, "rebuild: --facts is required")
		return 2
	case root == "":
		fmt.Fprintln(stderr, "rebuild: --root is required")
		return 2
	case generationID == "":
		fmt.Fprintln(stderr, "rebuild: --generation is required")
		return 2
	case resolverVersion == "":
		fmt.Fprintln(stderr, "rebuild: --resolver-version is required")
		return 2
	}

	factsData, err := os.ReadFile(factsPath)
	if err != nil {
		fmt.Fprintf(stderr, "rebuild: read facts: %v\n", err)
		return 1
	}
	var set facts.Set
	if err := json.Unmarshal(factsData, &set); err != nil {
		fmt.Fprintf(stderr, "rebuild: decode facts: %v\n", err)
		return 1
	}

	report, err := rebuilder(context.Background(), rebuild.Options{
		Root:            root,
		GenerationID:    generationID,
		Facts:           set,
		ResolverVersion: resolverVersion,
		SnapshotID:      snapshotID,
		Store:           generation.DefaultConfig(),
	})
	writeRebuildReport(stdout, report)
	if err != nil {
		fmt.Fprintf(stderr, "rebuild: %v\n", err)
		return 1
	}
	if !report.Passed {
		fmt.Fprintf(stderr, "rebuild: %s\n", rebuildFailureReason(report))
		return 1
	}
	return 0
}

// writeRebuildReport transcribes the pipeline report Run already computed;
// it never re-derives pass/fail so stdout and the exit code cannot disagree.
func writeRebuildReport(stdout io.Writer, report rebuild.Report) {
	for _, stage := range report.Stages {
		fmt.Fprintf(stdout, "[%s] %s: %.2fms", rebuildState(stage.Passed), stage.Name, stage.DurationMS)
		if stage.Detail != "" {
			fmt.Fprintf(stdout, " - %s", stage.Detail)
		}
		fmt.Fprintln(stdout)
	}
	for _, check := range report.Integrity {
		if check.Passed {
			continue
		}
		fmt.Fprintf(stdout, "[FAIL] integrity %s: expected %d, observed %d\n", check.Table, check.Expected, check.Observed)
	}
	for _, finding := range report.Invariants.Findings {
		if finding.Passed {
			continue
		}
		fmt.Fprintf(stdout, "[FAIL] invariant %s: %d violation(s)\n", finding.Rule, finding.Violations)
		for _, sample := range finding.Samples {
			fmt.Fprintf(stdout, "    %s %s: %s\n", sample.Table, sample.Key, sample.Detail)
		}
	}
	for _, probe := range report.Probes {
		if probe.Passed {
			continue
		}
		fmt.Fprintf(stdout, "[FAIL] probe %s: %s\n", probe.Probe, probe.Detail)
	}
	if report.SnapshotDigest != "" {
		fmt.Fprintf(stdout, "snapshot digest: %s\n", report.SnapshotDigest)
	} else {
		fmt.Fprintln(stdout, "snapshot digest: none")
	}
	if report.Publication.Generation.ID != "" {
		fmt.Fprintf(stdout, "generation published: %s (%s)\n", report.Publication.Generation.ID, report.Publication.Generation.Path)
	} else {
		fmt.Fprintln(stdout, "generation published: none")
	}
	if len(report.Pruned) != 0 {
		fmt.Fprintf(stdout, "generations pruned: %s\n", strings.Join(report.Pruned, ", "))
	} else {
		fmt.Fprintln(stdout, "generations pruned: none")
	}
}

// rebuildFailureReason finds the first broken stage, check, invariant or
// probe so the exit path always names a concrete cause instead of a
// generic failure.
func rebuildFailureReason(report rebuild.Report) string {
	for _, stage := range report.Stages {
		if !stage.Passed {
			return fmt.Sprintf("stage %q failed: %s", stage.Name, stage.Detail)
		}
	}
	for _, check := range report.Integrity {
		if !check.Passed {
			return fmt.Sprintf("integrity check %q failed: expected %d, observed %d", check.Table, check.Expected, check.Observed)
		}
	}
	for _, finding := range report.Invariants.Findings {
		if !finding.Passed {
			return fmt.Sprintf("invariant %q failed: %d violation(s)", finding.Rule, finding.Violations)
		}
	}
	for _, probe := range report.Probes {
		if !probe.Passed {
			return fmt.Sprintf("probe %q failed: %s", probe.Probe, probe.Detail)
		}
	}
	return "full rebuild failed"
}

func rebuildState(passed bool) string {
	if passed {
		return "PASS"
	}
	return "FAIL"
}

func runGraphStatus(args []string, stdout, stderr io.Writer, roles graphRoleResolver) int {
	flags := flag.NewFlagSet("graph status", flag.ContinueOnError)
	var root string
	flags.StringVar(&root, "root", "", "generation store root directory")
	if parsed, code := parseCommandFlags("graph status", flags, args, stdout, stderr); !parsed {
		return code
	}
	if flags.NArg() != 0 {
		fmt.Fprintf(stderr, "graph status: unexpected arguments: %v\n", flags.Args())
		return 2
	}
	if root == "" {
		fmt.Fprintln(stderr, "graph status: --root is required")
		return 2
	}

	layout, err := roles(context.Background(), rebuild.LayoutOptions{Root: root, Store: generation.DefaultConfig()})
	if err != nil {
		fmt.Fprintf(stderr, "graph status: %v\n", err)
		return 1
	}
	writeGraphStatus(stdout, root, layout)
	return 0
}

// writeGraphStatus prints the three roles LUQUE-0905 requires
// (graph.active, graph.next, graph.backup) with the path each one names on
// disk, plus the full retained set. graph.active and graph.backup print
// "none" when the store has never published (respectively backed up) a
// generation: that is a legitimate layout, not a rendering error, matching
// the exit code runGraphStatus already returns for it (0).
func writeGraphStatus(stdout io.Writer, root string, layout rebuild.Layout) {
	fmt.Fprintf(stdout, "%s: ", rebuild.RoleActive)
	if layout.Active.ID == "" {
		fmt.Fprintln(stdout, "none")
	} else {
		fmt.Fprintf(stdout, "%s (%s)\n", layout.Active.ID, layout.Active.Path)
	}

	// graph.next never exists on disk until a rebuild actually publishes:
	// this is where generation.Store.Publish would build it, following the
	// documented <root>/generations/<id>.tmp layout.
	generationsDir := filepath.Join(root, "generations")
	if absRoot, err := filepath.Abs(root); err == nil {
		generationsDir = filepath.Join(absRoot, "generations")
	}
	fmt.Fprintf(stdout, "%s: %s\n", rebuild.RoleNext, filepath.Join(generationsDir, layout.NextID+".tmp"))

	fmt.Fprintf(stdout, "%s: ", rebuild.RoleBackup)
	if !layout.HasBackup {
		fmt.Fprintln(stdout, "none")
	} else {
		fmt.Fprintf(stdout, "%s (%s)\n", layout.Backup.ID, layout.Backup.Path)
	}

	if len(layout.Retained) == 0 {
		fmt.Fprintln(stdout, "retained: none")
		return
	}
	fmt.Fprintf(stdout, "retained: %s\n", strings.Join(layout.Retained, ", "))
}

func runRollback(args []string, stdout, stderr io.Writer, rollback graphRollbacker) int {
	flags := flag.NewFlagSet("rollback", flag.ContinueOnError)
	var root, generationID string
	flags.StringVar(&root, "root", "", "generation store root directory")
	flags.StringVar(&generationID, "generation", "", "six digit generation id to roll back to; defaults to the registered graph.backup")
	if parsed, code := parseCommandFlags("rollback", flags, args, stdout, stderr); !parsed {
		return code
	}
	if flags.NArg() != 0 {
		fmt.Fprintf(stderr, "rollback: unexpected arguments: %v\n", flags.Args())
		return 2
	}
	if root == "" {
		fmt.Fprintln(stderr, "rollback: --root is required")
		return 2
	}

	report, err := rollback(context.Background(), rebuild.RollbackOptions{
		Root:         root,
		Store:        generation.DefaultConfig(),
		GenerationID: generationID,
	})
	writeRollbackReport(stdout, report)
	if err != nil {
		fmt.Fprintf(stderr, "rollback: %v\n", err)
		return 1
	}
	if !report.Passed {
		fmt.Fprintln(stderr, "rollback: report did not pass despite no error")
		return 1
	}
	return 0
}

// writeRollbackReport prints where the rollback moved from and to, the
// digest it expected (the generation's own snapshot.sha256) against the
// one it recomputed from live table counts, and the invariant verdict, so
// a failed rollback is diagnosable from stdout alone even though it never
// reaches the passed state runRollback checks for the exit code.
func writeRollbackReport(stdout io.Writer, report rebuild.RollbackReport) {
	fmt.Fprintf(stdout, "rollback: %s -> %s\n", orNone(report.From.ID), orNone(report.To.ID))
	fmt.Fprintf(stdout, "digest expected: %s\n", orNone(report.Expected))
	fmt.Fprintf(stdout, "digest observed: %s\n", orNone(report.Digest))
	if len(report.Invariants.Findings) == 0 {
		fmt.Fprintln(stdout, "invariants: not evaluated")
	} else {
		invariantState := "FAIL"
		if report.Invariants.Passed {
			invariantState = "PASS"
		}
		fmt.Fprintf(stdout, "invariants: %s\n", invariantState)
		writeIntegrityFindings(stdout, report.Invariants.Findings)
	}
	fmt.Fprintf(stdout, "rollback result: %s\n", rebuildState(report.Passed))
}

func orNone(value string) string {
	if value == "" {
		return "none"
	}
	return value
}

func runSnapshot(args []string, stdout, stderr io.Writer, build snapshotBuilder) int {
	flags := flag.NewFlagSet("snapshot", flag.ContinueOnError)
	var root, generationID string
	var snapshotID int64
	flags.StringVar(&root, "root", "", "generation store root directory")
	flags.StringVar(&generationID, "generation", "", "six digit generation id to snapshot; defaults to the registered graph.active")
	flags.Int64Var(&snapshotID, "snapshot-id", 0, "snapshot id stamped on the built HotSnapshot")
	if parsed, code := parseCommandFlags("snapshot", flags, args, stdout, stderr); !parsed {
		return code
	}
	if flags.NArg() != 0 {
		fmt.Fprintf(stderr, "snapshot: unexpected arguments: %v\n", flags.Args())
		return 2
	}
	if root == "" {
		fmt.Fprintln(stderr, "snapshot: --root is required")
		return 2
	}

	_, report, err := build(context.Background(), rebuild.GenerationSnapshotOptions{
		Root:         root,
		Store:        generation.DefaultConfig(),
		GenerationID: generationID,
		SnapshotID:   uint64(snapshotID),
	})
	writeSnapshotReport(stdout, report)
	if err != nil {
		fmt.Fprintf(stderr, "snapshot: %v\n", err)
		return 1
	}
	if !report.Passed {
		fmt.Fprintln(stderr, "snapshot: report did not pass despite no error")
		return 1
	}
	return 0
}

// writeSnapshotReport prints the account BuildSnapshot/SnapshotGeneration
// already computed: id, row format version, content digest, per table
// counts, and the edges that cannot be represented in the CSR (structural
// and Package to Package relations — see README.md), so an operator can
// tell a healthy generation from a broken one without a debugger.
func writeSnapshotReport(stdout io.Writer, report rebuild.SnapshotReport) {
	fmt.Fprintf(stdout, "snapshot: %s\n", rebuildState(report.Passed))
	fmt.Fprintf(stdout, "snapshot id: %d\n", report.SnapshotID)
	fmt.Fprintf(stdout, "version: %d\n", report.Version)
	fmt.Fprintf(stdout, "digest: %s\n", orNone(report.Digest))
	fmt.Fprintf(stdout, "repositories: %d\n", report.Stats.Repositories)
	fmt.Fprintf(stdout, "packages: %d\n", report.Stats.Packages)
	fmt.Fprintf(stdout, "files: %d\n", report.Stats.Files)
	fmt.Fprintf(stdout, "symbols: %d\n", report.Stats.Symbols)
	fmt.Fprintf(stdout, "evidence: %d\n", report.Stats.Evidence)
	fmt.Fprintf(stdout, "edges: %d\n", report.Stats.Edges)
	fmt.Fprintf(stdout, "edges not represented in the CSR: %d\n", report.Stats.SkippedEdges)
}
