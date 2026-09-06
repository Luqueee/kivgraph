package main

import (
	"bufio"
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
	"runtime"
	"slices"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"golang.org/x/mod/semver"

	"github.com/Luqueee/kivgraph/internal/app"
	"github.com/Luqueee/kivgraph/internal/config"
	"github.com/Luqueee/kivgraph/internal/eventlog"
	"github.com/Luqueee/kivgraph/internal/facts"
	"github.com/Luqueee/kivgraph/internal/freshness"
	"github.com/Luqueee/kivgraph/internal/goworkspace"
	"github.com/Luqueee/kivgraph/internal/hotsnapshot"
	"github.com/Luqueee/kivgraph/internal/indexer"
	"github.com/Luqueee/kivgraph/internal/indexing"
	"github.com/Luqueee/kivgraph/internal/invalidation"
	"github.com/Luqueee/kivgraph/internal/logging"
	mcpserver "github.com/Luqueee/kivgraph/internal/mcp"
	"github.com/Luqueee/kivgraph/internal/procstat"
	"github.com/Luqueee/kivgraph/internal/rebuild"
	"github.com/Luqueee/kivgraph/internal/rustloader"
	"github.com/Luqueee/kivgraph/internal/storage/generation"
	"github.com/Luqueee/kivgraph/internal/storage/ladybug"
	"github.com/Luqueee/kivgraph/internal/synthetic"
	"github.com/Luqueee/kivgraph/internal/update"
	"github.com/Luqueee/kivgraph/internal/upgrade"
	"github.com/Luqueee/kivgraph/internal/version"
	"github.com/Luqueee/kivgraph/internal/watcher"
	"github.com/Luqueee/kivgraph/internal/webapi"
	"github.com/Luqueee/kivgraph/internal/webassets"
	"github.com/Luqueee/kivgraph/internal/workspace"
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
		// Only a bare long-running command is answered here: its flag set is
		// the one main owns, and the table never sees it. Every other help --
		// `daemon install --help` included -- has to reach the table, which is
		// why this asks the registry instead of comparing the first word.
		switch {
		case interceptsLongRunning("ui", os.Args[1:]):
			configPath, address, profile := "", "", ""
			writeCommandHelp(os.Stdout, "ui", uiFlagSet(&configPath, &address, &profile))
			return
		case interceptsLongRunning("serve", os.Args[1:]):
			configPath := ""
			var options serveOptions
			writeCommandHelp(os.Stdout, "serve", serveFlagSet(&configPath, &options))
			return
		case interceptsLongRunning("daemon", os.Args[1:]):
			configPath := ""
			var options daemonOptions
			writeCommandHelp(os.Stdout, "daemon", daemonFlagSet(&configPath, &options))
			return
		}
	}
	if interceptsLongRunning("ui", os.Args[1:]) {
		// Nothing is announced before the viewer is known to exist: a
		// binary without web assets used to log that it was starting
		// one and then fail, which reads like a crash rather than a
		// build that never carried a viewer.
		ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
		defer stop()
		if err := runConfiguredUI(ctx, os.Args[2:], func(ctx context.Context, address string, handler http.Handler) error {
			// The default bind is every interface, so this warning is
			// the only thing standing between an unauthenticated
			// viewer and the network it is on. It names what travels
			// in a response and how to close it, because a warning
			// that only says "unauthenticated" is one nobody acts on.
			if !isLoopbackListenAddress(address) {
				logger.Warn("web viewer is unauthenticated and reachable from the network",
					"command", "ui", "address", address,
					"exposes", "repository paths, file paths, symbol names and signatures",
					"restrict_with", "--addr 127.0.0.1:7777 or web.address in the configuration")
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
	if interceptsLongRunning("serve", os.Args[1:]) {
		logger.Info("starting MCP server", "command", "serve")
		ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
		defer stop()
		configPath := ""
		var options serveOptions
		// The options are read inside the runner, not here: runConfiguredServe
		// parses the flag set before it calls back, so this closure sees what
		// the command line said rather than the zero value.
		if err := runConfiguredServe(ctx, "serve", os.Args[2:], serveFlagSet(&configPath, &options), &configPath, &options, provisionDaemon, func(ctx context.Context, _ config.Loaded, store *hotsnapshot.SnapshotStore, indexer indexing.ProjectIndexer, events *eventlog.Writer) error {
			return mcpserver.RunWithMetricsAndSnapshotStoreAndIndexerOptions(ctx, toolMetricsRegistry(events), store, indexer,
				mcpserver.ServerOptions{ExposeUnavailableTools: options.Introspection})
		}); err != nil {
			logger.Error("MCP server stopped with error", "command", "serve", "error", err)
			os.Exit(1)
		}
		logger.Info("MCP server stopped", "command", "serve")
		return
	}
	if interceptsLongRunning("daemon", os.Args[1:]) {
		logger.Info("starting MCP daemon", "command", "daemon")
		ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
		defer stop()
		configPath := ""
		var options daemonOptions
		flags := daemonFlagSet(&configPath, &options)
		// The daemon is the thing being relayed *to*, so it provisions
		// nothing: relayToTheDaemon declines on the command name before it
		// ever asks, and naming the real provisioner here would say
		// otherwise.
		if err := runConfiguredServe(ctx, "daemon", os.Args[2:], flags, &configPath, nil, nil,
			runDaemon(logger, &options)); err != nil {
			logger.Error("MCP daemon stopped with error", "command", "daemon", "error", err)
			os.Exit(1)
		}
		logger.Info("MCP daemon stopped", "command", "daemon")
		return
	}

	// A release notice is deliberately limited to the bare interactive
	// invocation. It is cached and time-bounded so commands and scripts never
	// acquire a network dependency.
	if len(os.Args) == 1 && isTerminal(os.Stderr) {
		writeUpdateNotice(os.Stderr)
	}

	// A Codex PreToolUse refusal is a small wire protocol of its own: exit 2
	// and a plain reason on stderr. The regular non-interactive CLI wraps stderr
	// as structured logs and appends a generic failure record, which would turn
	// that reason into two unrelated JSON messages before Codex reads it.
	if len(os.Args) >= 3 && os.Args[1] == "hook" && os.Args[2] == "run" {
		os.Exit(run(os.Args, os.Stdout, os.Stderr))
	}

	// A one-shot command reports to whoever is listening: plain text for the
	// operator at a terminal, the structured record other tooling parses when
	// stderr is a pipe or a file. serve and ui above always log structurally,
	// because a client reads their stderr for the life of the process.
	if isTerminal(os.Stderr) {
		os.Exit(run(os.Args, os.Stdout, os.Stderr))
	}
	exitCode := run(os.Args, os.Stdout, logging.NewCommandWriter(logger))
	if exitCode != 0 {
		logger.Error("command failed", "command", "cli", "exit_code", exitCode)
	}
	os.Exit(exitCode)
}

func writeUpdateNotice(stderr io.Writer) {
	result, err := update.CheckNotice(context.Background(), update.NoticeOptions{
		APIBaseURL:     os.Getenv("KIVGRAPH_UPDATE_API_URL"),
		CurrentVersion: version.Value,
		Token:          os.Getenv("KIVGRAPH_GITHUB_TOKEN"),
		Channel:        os.Getenv("KIVGRAPH_UPDATE_CHANNEL"),
	})
	if err != nil || !result.UpdateAvailable {
		return
	}
	writeWarning(stderr, "kivgraph update available%s: %s -> %s; run \"kivgraph update\" to install it",
		channelLabel(result.Channel),
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

// configuredMCPRunner serves MCP with everything the configuration produced.
// It receives the loaded configuration because a runner that binds a socket has
// to know which state directory it belongs to: that directory is the key that
// keeps two configurations from sharing one daemon.
type configuredMCPRunner func(context.Context, config.Loaded, *hotsnapshot.SnapshotStore, indexing.ProjectIndexer, *eventlog.Writer) error
type configuredWebRunner func(context.Context, string, http.Handler) error

// ensureConfiguration writes the default configuration when there is none.
//
// An MCP client starts its servers itself: it spawns `kivgraph serve` and
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

// ensureLoadedConfiguration is the half a relay needs and the whole a
// configuration command needs: the file exists afterwards, and it is read.
//
// A client MCP starts this process itself, so a configuration that is not
// there is written rather than refused -- exiting because nobody ran `init`
// turns installing the integration into a terminal session, and the client
// only reports that the server failed.
func ensureLoadedConfiguration(configPath string) (config.Loaded, error) {
	if err := ensureConfiguration(configPath); err != nil {
		return config.Loaded{}, err
	}
	loaded, err := config.LoadProfile(configPath, "")
	if err != nil {
		return config.Loaded{}, fmt.Errorf("load configuration: %w", err)
	}
	return loaded, nil
}

// openConfiguredSnapshot resolves the published generation this process will
// answer from. A relay never calls it: it holds no graph.
func openConfiguredSnapshot(ctx context.Context, loaded config.Loaded) (*hotsnapshot.SnapshotStore, error) {
	layout, err := rebuild.Roles(ctx, rebuild.LayoutOptions{
		Root:  filepath.Dir(loaded.Config.Storage.DatabasePath),
		Store: generation.DefaultConfig(),
	})
	if err != nil {
		return nil, fmt.Errorf("resolve active generation: %w", err)
	}
	if layout.Active.ID == "" {
		return hotsnapshot.NewSnapshotStore(nil), nil
	}
	generationNumber, err := strconv.ParseUint(layout.Active.ID, 10, 64)
	if err != nil {
		return nil, fmt.Errorf("parse active generation %q: %w", layout.Active.ID, err)
	}
	// The graph is not read here. It is read by whatever first needs it, which
	// on most servers is never: 48 of 51 in a real event log were started and
	// asked nothing, and mapping a generation costs some thirty megabytes of
	// private indexes whether or not anyone asks. See ADR 0067.
	//
	// What runs inside the loader is exactly what used to run here, fallback
	// included: a missing, foreign, stale or corrupt snapshot file still costs a
	// derivation from the canonical graph rather than an answer. Only the moment
	// moved.
	return hotsnapshot.NewDeferredSnapshotStore(generationNumber, func() (*hotsnapshot.GraphSnapshot, error) {
		currentLayout, err := rebuild.Roles(ctx, rebuild.LayoutOptions{
			Root:  filepath.Dir(loaded.Config.Storage.DatabasePath),
			Store: generation.DefaultConfig(),
		})
		if err != nil {
			return nil, fmt.Errorf("resolve active generation while opening snapshot: %w", err)
		}
		currentGeneration, err := strconv.ParseUint(currentLayout.Active.ID, 10, 64)
		if err != nil {
			return nil, fmt.Errorf("parse active generation %q: %w", currentLayout.Active.ID, err)
		}
		snapshot, report, err := rebuild.LoadOrBuildSnapshot(ctx, rebuild.BuildSnapshotOptions{
			DatabasePath: currentLayout.Active.DatabasePath,
			SnapshotID:   currentGeneration,
		})
		// A server holds this snapshot for its whole life; what building it
		// borrowed is dead the moment it is published, and returning it here
		// is what keeps a long-running process near what it actually holds.
		// Loading it borrows far less, which is the point of ADR 0045, but the
		// scavenge still runs: a fallback derived it the expensive way.
		defer rebuild.ReturnBuildMemory()
		if err != nil {
			return nil, fmt.Errorf("build active snapshot %q: %w", currentLayout.Active.ID, err)
		}
		if !report.Passed {
			return nil, fmt.Errorf("build active snapshot %q did not pass", currentLayout.Active.ID)
		}
		// Which of the two routes was taken is worth a line, because nothing else
		// distinguishes them: a server that derives answers exactly like one that
		// read the file, and costs a gigabyte more to start.
		switch {
		case report.Loaded:
			logging.New(os.Stderr).Info("read the published snapshot",
				"generation", currentLayout.Active.ID, "symbols", report.Stats.Symbols)
		default:
			logging.New(os.Stderr).Info("derived the snapshot from the canonical graph",
				"generation", currentLayout.Active.ID, "symbols", report.Stats.Symbols,
				"reason", report.LoadRefused)
		}
		return snapshot, nil
	}), nil
}

// openConfiguredProfileSnapshots keeps the historical single store when only
// one profile exists and otherwise groups one deferred store per profile. No
// graph is mapped here; each loader runs only when a query selects it.
func openConfiguredProfileSnapshots(ctx context.Context, loaded config.Loaded) (*hotsnapshot.SnapshotStore, error) {
	profiles, err := config.ListProfiles(loaded.ConfigPath)
	if err != nil {
		return nil, fmt.Errorf("list configured profiles: %w", err)
	}
	stores := make(map[string]*hotsnapshot.SnapshotStore, len(profiles))
	for _, profile := range profiles {
		profileLoaded, err := config.LoadProfile(loaded.ConfigPath, profile.Name)
		if err != nil {
			return nil, fmt.Errorf("load profile %q: %w", profile.Name, err)
		}
		store, err := openConfiguredSnapshot(ctx, profileLoaded)
		if err != nil {
			return nil, fmt.Errorf("open profile %q: %w", profile.Name, err)
		}
		stores[profile.Name] = store
	}
	aggregate, err := hotsnapshot.NewProfileSnapshotStore(loaded.Config.Profiles.Default, stores)
	if err != nil {
		for _, store := range stores {
			store.Close()
		}
		return nil, err
	}
	if err := aggregate.SetMaxOpenProfiles(loaded.Config.Profiles.MaxOpen); err != nil {
		aggregate.Close()
		return nil, err
	}
	return aggregate, nil
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
	profile := ""
	flags := uiFlagSet(&configPath, &address, &profile)
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
	if err := ensureConfiguration(configPath); err != nil {
		return err
	}
	loaded, err := config.LoadProfile(configPath, profile)
	if err != nil {
		return fmt.Errorf("ui: load profile: %w", err)
	}
	var store *hotsnapshot.SnapshotStore
	var stopFollower func()
	if profile == "" {
		store, err = openConfiguredProfileSnapshots(ctx, loaded)
		if err != nil {
			return err
		}
		stopFollower, err = followConfiguredProfiles(ctx, loaded, store, "ui")
		if err != nil {
			store.Close()
			return err
		}
	} else {
		store, err = openConfiguredSnapshot(ctx, loaded)
		if err != nil {
			return err
		}
		stopFollower = followPublishedGeneration(ctx, loaded, store, "ui", indexing.FollowOptions{})
	}
	defer store.Close()
	defer stopFollower()
	if address == "" {
		address = loaded.Config.Web.Address
	}
	return runWeb(ctx, address, webapi.NewHandlerWithTopology(store, webapi.TopologyOptions{
		ConfigPath:       loaded.ConfigPath,
		Profile:          loaded.Profile,
		InvalidationRoot: stateDirectory(loaded),
	}))
}

// followConfiguredProfiles keeps an aggregate viewer current for every
// profile it can select. Each child store follows its own generation root;
// following the aggregate would only observe its default profile.
func followConfiguredProfiles(
	ctx context.Context,
	loaded config.Loaded,
	store *hotsnapshot.SnapshotStore,
	command string,
) (func(), error) {
	profiles, err := store.ResolveProfiles([]string{"*"})
	if err != nil {
		return nil, fmt.Errorf("follow configured profiles: %w", err)
	}
	stops := make([]func(), 0, len(profiles))
	stopAll := func() {
		for index := len(stops) - 1; index >= 0; index-- {
			stops[index]()
		}
	}
	for _, profile := range profiles {
		profileLoaded, err := config.LoadProfile(loaded.ConfigPath, profile.Name)
		if err != nil {
			stopAll()
			return nil, fmt.Errorf("follow profile %q: %w", profile.Name, err)
		}
		stops = append(stops, followPublishedGeneration(ctx, profileLoaded, profile.Store, command, indexing.FollowOptions{}))
	}
	return stopAll, nil
}

// uiFlagSet and serveFlagSet exist so the two long-running commands describe
// their flags in one place: the parser that runs them and the help that
// answers --help read the same definitions.
func uiFlagSet(configPath, address, profile *string) *flag.FlagSet {
	flags := flag.NewFlagSet("ui", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	flags.StringVar(configPath, "config", "", "configuration file")
	flags.StringVar(address, "addr", "", "HTTP listen address")
	flags.StringVar(profile, "profile", "", "graph profile (defaults to profiles.default)")
	return flags
}

// serveOptions carries the flags only serve has. It is a struct rather than a
// loose bool so the next one does not thread another argument through the help,
// the completion and the runner all over again.
type serveOptions struct {
	// Introspection lists the query tools before a generation exists. It
	// creates no index and relaxes no check: what it changes is the
	// catalogue an inspector reads, and every tool it lists still refuses
	// with INDEX_NOT_READY until something is published.
	Introspection bool
}

func serveFlagSet(configPath *string, options *serveOptions) *flag.FlagSet {
	flags := flag.NewFlagSet("serve", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	flags.StringVar(configPath, "config", "", "configuration file")
	flags.BoolVar(&options.Introspection, "introspection", false,
		"expose the complete MCP tool catalog even when no index is available")
	return flags
}

// daemonOptions carries the flags only the daemon has: it is the one long-lived
// command that binds a port, so it is the one that has to be told where.
type daemonOptions struct {
	Address     string
	AllowRemote bool
}

func daemonFlagSet(configPath *string, options *daemonOptions) *flag.FlagSet {
	flags := flag.NewFlagSet("daemon", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	flags.StringVar(configPath, "config", "", "configuration file")
	flags.StringVar(&options.Address, "addr", "", "HTTP address for MCP clients that take a url (defaults to the persisted port or 127.0.0.1:7788)")
	flags.BoolVar(&options.AllowRemote, "allow-remote", false, "permit a bind outside loopback, which sends names, paths and source metadata off this host")
	return flags
}

// runConfiguredServe builds everything a long-lived MCP command needs and hands
// it to runMCP.
//
// command names the invocation in the logs, the event log and the errors. Two
// commands share this: `serve`, which speaks over its own stdio, and `daemon`,
// which speaks over a socket and over HTTP to many clients. Everything before
// the runner is identical -- the same configuration, the same store, the same
// follower and the same resync -- and a reader of the event log has to be able to
// tell which one wrote a line.
//
// flags is the command's own set, already bound to configPath, because the two
// commands do not take the same options: only the daemon binds a port.
func runConfiguredServe(
	ctx context.Context,
	command string,
	args []string,
	flags *flag.FlagSet,
	configPath *string,
	// options is a pointer because it is filled by the parse below, not by
	// the caller: taking it by value would read the zero value of a struct
	// the command line has not been applied to yet.
	options *serveOptions,
	// provision installs a supervised daemon when none is answering, and it
	// is a parameter for the reason daemonProvisioner already is: a test must
	// never reach the developer's own supervisor. It was reaching it -- this
	// function named the real one, so every test of it installed a launchd
	// agent or a systemd unit pointed at a directory the test was about to
	// delete, and left the registration behind when it did. Two hundred of
	// them accumulated on one workstation before a CI run tripped over the
	// deletion race and said so.
	provision daemonProvisioner,
	runMCP configuredMCPRunner,
) error {
	if ctx == nil {
		ctx = context.Background()
	}
	if command == "" {
		command = "serve"
	}
	if runMCP == nil {
		return fmt.Errorf("%s: MCP runner is required", command)
	}
	if flags == nil || configPath == nil {
		return fmt.Errorf("%s: a flag set bound to a configuration path is required", command)
	}
	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() != 0 {
		return fmt.Errorf("%s: unexpected arguments: %v", command, flags.Args())
	}
	loaded, err := ensureLoadedConfiguration(*configPath)
	if err != nil {
		return err
	}
	// Before the store, the follower and the resync, because a relay needs
	// none of the three: it holds no graph, so there is no generation to
	// follow and no branch change to answer. Paying for them would put back
	// exactly the per-client cost the relay exists to remove.
	if relayed, err := relayToTheDaemon(ctx, command, *configPath, loaded, provision,
		options != nil && options.Introspection); relayed {
		return err
	}
	// After the relay decision, because the transport reported is the one that
	// is about to serve and not the one that was preferred. The relaying path
	// returned above and reported for itself.
	announceFirstRun(loaded, command)
	store, err := openConfiguredProfileSnapshots(ctx, loaded)
	if err != nil {
		return err
	}
	defer store.Close()
	profileIndexer := newProfileProjectIndexer(loaded.ConfigPath, store)
	events := openEventLog(loaded.Config, os.Stderr)
	// The started/stopped pair is what makes the tool lines between them
	// readable: without it a reader cannot tell one server's calls from the
	// calls of the server that replaced it after an update.
	started := time.Now()
	events.Append(eventlog.Event{
		Kind:       eventlog.KindServe,
		Message:    command + " started",
		Generation: publishedGenerationID(store),
	})
	// The close belongs in the same deferred call as the last line written,
	// because two defers would run in the wrong order and close the file
	// before the "stopped" event reached it.
	//
	// It was missing entirely. `index` closed its writer and the long-running
	// commands -- the ones that hold the handle for hours -- did not, which on
	// Unix costs a descriptor until the process exits and shows up nowhere. On
	// Windows an open file cannot be deleted, so a test's own temporary
	// directory outlived the test and said so.
	defer func() {
		events.Append(eventlog.Event{
			Kind:    eventlog.KindServe,
			Message: command + " stopped",
		}.WithDuration(time.Since(started)))
		if err := events.Close(); err != nil {
			writeWarning(os.Stderr, "events: close: %v", err)
		}
	}()
	stopProfiles, err := watchConfiguredProfiles(ctx, loaded, store, profileIndexer, command)
	if err != nil {
		return err
	}
	defer stopProfiles()
	return runServe(ctx, func(ctx context.Context) error {
		return runMCP(ctx, loaded, store, profileIndexer, events)
	})
}

func watchConfiguredProfiles(
	ctx context.Context,
	loaded config.Loaded,
	store *hotsnapshot.SnapshotStore,
	indexer *profileProjectIndexer,
	command string,
) (func(), error) {
	profiles, err := config.ListProfiles(loaded.ConfigPath)
	if err != nil {
		return nil, fmt.Errorf("watch configured profiles: %w", err)
	}
	manager, err := invalidation.Open(stateDirectory(loaded))
	if err != nil {
		return nil, fmt.Errorf("watch configured profiles: open invalidation state: %w", err)
	}
	scheduler := newInvalidationScheduler(ctx, manager, indexer, logging.New(os.Stderr))
	var watchersMu sync.Mutex
	stops := make([]func(), 0, len(profiles)*3+1)
	// Profile watchers stop before the queue so no callback can enqueue work
	// while the scheduler is shutting down.
	stops = append(stops, scheduler.Close)
	watched := make(map[string]struct{}, len(profiles))
	monitorStops := make(map[string]func(), len(profiles))
	monitorStarts := make(map[string]uint64, len(profiles))
	var monitorStartsWG sync.WaitGroup
	monitorStartContext, cancelMonitorStarts := context.WithCancel(ctx)
	var nextMonitorStart uint64
	closed := false
	indexer.setDefaultProfile(loaded.Config.Profiles.Default)
	startFreshnessMonitor := func(name string, profileLoaded config.Loaded, profileStore *hotsnapshot.SnapshotStore) {
		cache := indexer.freshnessCache(name, profileStore)
		watchersMu.Lock()
		if closed {
			watchersMu.Unlock()
			return
		}
		nextMonitorStart++
		startID := nextMonitorStart
		monitorStarts[name] = startID
		previous := monitorStops[name]
		delete(monitorStops, name)
		monitorStartsWG.Add(1)
		watchersMu.Unlock()
		if previous != nil {
			previous()
		}
		go func() {
			defer monitorStartsWG.Done()
			monitor, monitorErr := freshness.NewRegistryMonitor(
				monitorStartContext,
				profileLoaded.Repositories,
				profileLoaded.RepositoriesPath,
				filepath.Dir(profileLoaded.Config.Storage.DatabasePath),
				cache,
			)
			if monitorErr != nil {
				watchersMu.Lock()
				current := !closed && monitorStarts[name] == startID
				if current {
					delete(monitorStarts, name)
				}
				watchersMu.Unlock()
				if current && !errors.Is(monitorErr, context.Canceled) {
					cache.MarkUnavailable(monitorErr.Error())
					writeWarning(os.Stderr, "content freshness for profile %q: %v", name, monitorErr)
				}
				return
			}
			watchersMu.Lock()
			current := !closed && monitorStarts[name] == startID
			if current {
				monitorStops[name] = monitor.Close
			}
			watchersMu.Unlock()
			if !current {
				monitor.Close()
			}
		}()
	}
	register := func(name string, profileLoaded config.Loaded, profileStore *hotsnapshot.SnapshotStore) {
		watchersMu.Lock()
		if closed {
			watchersMu.Unlock()
			return
		}
		_, found := watched[name]
		if !found {
			watched[name] = struct{}{}
		}
		watchersMu.Unlock()
		startFreshnessMonitor(name, profileLoaded, profileStore)
		if found {
			return
		}
		followStop := followPublishedGeneration(ctx, profileLoaded, profileStore, command, indexing.FollowOptions{})
		resyncStop := resyncOnBranchChange(ctx, profileLoaded, profileStore, namedProfileReindexer{indexer, name}, command)
		var sourceStop func()
		if profileLoaded.Config.Watcher.Enabled {
			sourceStop = watchProfileSources(ctx, profileLoaded, manager, scheduler, command)
		}
		watchersMu.Lock()
		if closed {
			watchersMu.Unlock()
			if sourceStop != nil {
				sourceStop()
			}
			resyncStop()
			followStop()
			return
		}
		stops = append(stops, followStop, resyncStop)
		if sourceStop != nil {
			stops = append(stops, sourceStop)
		}
		watchersMu.Unlock()
	}
	cleanup := func() {
		watchersMu.Lock()
		if closed {
			watchersMu.Unlock()
			return
		}
		closed = true
		cancelMonitorStarts()
		current := stops
		stops = nil
		for name, stop := range monitorStops {
			current = append(current, stop)
			delete(monitorStops, name)
		}
		watchersMu.Unlock()
		for index := len(current) - 1; index >= 0; index-- {
			current[index]()
		}
		monitorStartsWG.Wait()
	}
	for _, profile := range profiles {
		profileLoaded, err := config.LoadProfile(loaded.ConfigPath, profile.Name)
		if err != nil {
			cleanup()
			return nil, fmt.Errorf("watch profile %q: %w", profile.Name, err)
		}
		selected, err := store.ResolveProfiles([]string{profile.Name})
		if err != nil {
			cleanup()
			return nil, fmt.Errorf("watch profile %q: %w", profile.Name, err)
		}
		register(profile.Name, profileLoaded, selected[0].Store)
	}
	indexer.setProfileWatcher(register)
	return func() {
		indexer.setProfileWatcher(nil)
		cleanup()
	}, nil
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
			logger.Info("serving published generation", "command", command, "profile", loaded.Profile, "generation", id)
		}
	}
	if options.OnError == nil {
		options.OnError = func(err error) {
			logger.Error("could not follow the published generation", "command", command, "profile", loaded.Profile, "error", err)
		}
	}
	followCtx, cancel := context.WithCancel(ctx)
	done := make(chan struct{})
	go func() {
		defer close(done)
		if err := indexing.Follow(followCtx, store, options); err != nil {
			logger.Error("generation follower stopped", "command", command, "profile", loaded.Profile, "error", err)
		}
	}()
	return func() {
		cancel()
		<-done
	}
}

// resyncOnBranchChange keeps the published graph on the code that is actually
// checked out. A checkout, pull, merge, rebase or reset moves HEAD and leaves
// every path and line the server returns describing something else; without
// this the only way back is a person remembering to run `index --full`.
//
// It observes HEAD and nothing else. A push moves no local ref and rewrites no
// file, so it changes nothing the graph describes. A commit does move HEAD
// without rewriting anything, so before rebuilding, the content the graph
// recorded is compared against the bytes on disk: the cheapest rebuild is the
// one that does not happen.
//
// Like the follower, it never fails the command and never outlives it.
func resyncOnBranchChange(
	ctx context.Context,
	loaded config.Loaded,
	store *hotsnapshot.SnapshotStore,
	indexer interface{ Reindex(context.Context) error },
	command string,
) func() {
	logger := logging.New(os.Stderr)
	resyncCtx, cancel := context.WithCancel(ctx)
	done := make(chan struct{})
	go func() {
		defer close(done)
		// Discovering the workspace reads HEAD, the branch and the working tree of
		// every registered repository, which is one git invocation each: measured
		// 0.09 s at two repositories and 1.29 s at 37. Done before the caller
		// returns, that delay sits between the process starting and the MCP
		// transport opening, and a host that waits less than that records the
		// server as still connecting and then defers its tools for the whole
		// session -- the server works and answers, and its tools are invisible to
		// the model. Claude Code does exactly this on a 37-repository workspace.
		//
		// So the watcher discovers the workspace on its own time. It already never
		// fails the command and never outlives it; this only stops it from holding
		// the door shut on the way in.
		registry, err := registryForProfile(resyncCtx, loaded)
		if err != nil {
			// Moving discovery off the startup path put it in reach of shutdown:
			// a command that exits while git is still being asked about the
			// second of thirty-seven repositories cancels this context. That is
			// the command ending, not a registry that cannot be read, and
			// reporting it as one teaches a reader to distrust the log. A real
			// failure -- an entry that is not a git repository, an unreadable
			// HEAD -- still arrives here and is still reported.
			if !errors.Is(err, context.Canceled) {
				logger.Error("could not read the repository registry", "command", command, "error", err)
			}
			return
		}
		repositories := registry.List()
		if len(repositories) == 0 {
			return
		}
		state := filepath.Dir(loaded.Config.Storage.DatabasePath)
		options := indexing.ResyncOptions{
			Repositories: repositories,
			LockPath:     filepath.Join(state, "resync.lock"),
			Resync: func(ctx context.Context, moved []workspace.Repository) error {
				// The indexer decides the route. This loop only decides when.
				return indexer.Reindex(ctx)
			},
			ContentUnchanged: func(ctx context.Context, moved []indexing.RepositoryMovement) (bool, error) {
				return commitChangedNothing(ctx, moved), nil
			},
			OnMoved: func(batch []indexing.RepositoryMovement) {
				for _, movement := range batch {
					logger.Info("working tree moved",
						"command", command, "repository", movement.Repository.Name,
						"from", movement.From, "to", movement.To, "branch", movement.Branch)
				}
			},
			OnResynced: func(batch []indexing.RepositoryMovement) {
				logger.Info("graph resynchronised", "command", command, "repositories", len(batch))
			},
			OnSkipped: func(batch []indexing.RepositoryMovement) {
				logger.Info("no rebuild needed, the indexed content is unchanged",
					"command", command, "repositories", len(batch))
			},
			OnError: func(err error) {
				logger.Error("could not resynchronise the graph", "command", command, "error", err)
			},
			OnGaveUp: func(batch []indexing.RepositoryMovement, err error) {
				// One line, at the end, naming the number of attempts: the
				// failures themselves are already above it, and what a reader
				// cannot infer from them is that no more are coming.
				logger.Error("gave up resynchronising a movement that keeps failing",
					"command", command, "repositories", len(batch),
					"attempts", indexing.ResyncAttempts,
					"remedy", "fix the failure and run `kivgraph index --full`, or move the tree again",
					"error", err)
			},
		}
		if err := indexing.Resync(resyncCtx, options); err != nil {
			logger.Error("resynchroniser stopped", "command", command, "error", err)
		}
	}()
	return func() {
		cancel()
		<-done
	}
}

// commitChangedNothing reports whether the movement each repository made left
// its files exactly as the graph indexed them.
//
// It is the difference between a commit and a checkout. Both move HEAD; only
// one changes the code, and rebuilding the corpus to find out that nothing
// changed spends seconds of every commit producing the graph that is already
// published.
func commitChangedNothing(ctx context.Context, moved []indexing.RepositoryMovement) bool {
	for _, movement := range moved {
		if !watcher.CommitsHaveIdenticalTrees(ctx, movement.Repository.RealPath, movement.From, movement.To) {
			return false
		}
	}
	return len(moved) > 0
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

// runWithSnapshotBuilder is the dispatch. It walks the one table in
// commands.go, longest invocation first, so `doctor storage` is never read as
// `doctor` with a stray argument and `index --full` is never read as `index`.
func runWithSnapshotBuilder(args []string, stdout, stderr io.Writer, diagnose storageDiagnoser, rebuilder graphRebuilder, verify graphVerifier, roles graphRoleResolver, rollback graphRollbacker, build snapshotBuilder) int {
	deps := dependencies{
		diagnose:  diagnose,
		rebuilder: rebuilder,
		verify:    verify,
		roles:     roles,
		rollback:  rollback,
		build:     build,
	}
	program := filepath.Base(args[0])
	if len(args) < 2 {
		// A bare invocation is a question, not a mistake: it asks what this
		// program does. Answering it with a usage error on stderr sent the
		// one reader who has not read the help yet to look for the help.
		writeHelp(stdout, program)
		return 0
	}
	switch args[1] {
	case "--help", "-h", "help":
		writeHelp(stdout, program)
		return 0
	}
	if spec, consumed, found := findCommand(args[1:]); found && spec.run != nil {
		return spec.run(deps, args[1+consumed:], stdout, stderr)
	}
	writeUsageError(stderr, program, fmt.Sprintf("unknown command %q", args[1]))
	return 2
}

type updateRunner func(context.Context, update.Options) (update.Result, error)

func runUpdate(args []string, stdout, stderr io.Writer) int {
	executable, err := os.Executable()
	if err != nil {
		writeCommandError(stderr, "update: resolve this executable: %v", err)
		return 1
	}
	restart := func(targets []procstat.Process) (daemonRestart, error) {
		return restartSupervisedDaemonAt(executable, targets)
	}
	return runUpdateWithRunnerAtExecutable(args, os.Stdin, stdout, stderr, update.Run,
		procstat.List, signalProcess, restart, gracefulStopSupported, executable,
		refreshInstalledRuntimeWithResult)
}

// daemonOwnership is what `update` managed to establish about who owns the
// stale daemon, which is a different question from whether one was restarted.
//
// Three states and not a boolean, because the advice printed under the process
// list -- install a supervisor, or stopping this leaves you with none -- is
// only true of one of them, and a boolean made every case that could not be
// established read as the one where it was.
type daemonOwnership int

const (
	// ownershipUnknown is the answer whenever nothing was established: no
	// configuration to locate the daemon by, no endpoint to identify it with,
	// or a stale daemon that is not the one this configuration publishes.
	// Nothing is restarted and nothing is advised.
	ownershipUnknown daemonOwnership = iota
	// ownershipNone means it was established that no supervisor has this
	// daemon: it published an endpoint, its pid is one of the stale processes,
	// and no unit exists for it. This is the only state the advice fits.
	ownershipNone
	// ownershipSupervised means a supervisor has it, whether or not it came
	// back: a unit somebody edited by hand and a restart that failed are both
	// this, and telling either operator to install a supervisor would say the
	// one thing that is not true.
	ownershipSupervised
)

// daemonRestart is what `update` learned about the one stale process a
// supervisor might own.
type daemonRestart struct {
	// Label names the unit, when there is one.
	Label string
	// PID is the daemon that was restarted, zero when none was.
	PID int
	// Ownership is who has it, as far as this could be established.
	Ownership daemonOwnership
	// Detail is why a supervised daemon was not restarted, when nothing failed.
	Detail string
}

// supervisedDaemonRestart puts the supervised daemon among the stale processes
// back on the executable that is now on disk.
//
// It is injected for two reasons: a test must never reach the developer's own
// supervisor -- `update` is the command whose defect was that it reached
// nothing -- and the outcome that matters is the one no CI runner can produce,
// a machine where systemd or launchd owns the daemon. A nil restarter consults
// no supervisor at all.
type supervisedDaemonRestart func(targets []procstat.Process) (daemonRestart, error)

// updateOptions carries the flags of `kivgraph update`.
type updateOptions struct {
	CheckOnly bool
	StopStale bool
	Channel   string
}

// updateFlagSet declares them in one place, so the parser that runs the
// command and the help and completion that describe it read the same
// definitions.
func updateFlagSet(options *updateOptions) *flag.FlagSet {
	flags := flag.NewFlagSet("update", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	flags.BoolVar(&options.CheckOnly, "check", false, "check for a newer release without installing it")
	flags.BoolVar(&options.StopStale, "stop", false, "stop the processes still running the previous release without asking")
	flags.StringVar(&options.Channel, "channel", "", "release channel: stable or dev (default follows the installed version)")
	return flags
}

func runUpdateWithRunner(
	args []string,
	stdin io.Reader,
	stdout, stderr io.Writer,
	runner updateRunner,
	list processLister,
	signal processSignaller,
	restart supervisedDaemonRestart,
	graceful bool,
) int {
	return runUpdateWithRunnerAndPostInstall(args, stdin, stdout, stderr, runner, list,
		signal, restart, graceful, nil)
}

func runUpdateWithRunnerAndPostInstall(
	args []string,
	stdin io.Reader,
	stdout, stderr io.Writer,
	runner updateRunner,
	list processLister,
	signal processSignaller,
	restart supervisedDaemonRestart,
	graceful bool,
	postInstall updatePostInstall,
) int {
	var postInstallWithResult updatePostInstallWithResult
	if postInstall != nil {
		postInstallWithResult = func(executable string, stdout, stderr io.Writer) updatePostInstallResult {
			return updatePostInstallResult{Err: postInstall(executable, stdout, stderr)}
		}
	}
	executable, err := os.Executable()
	if err != nil {
		writeCommandError(stderr, "update: resolve this executable: %v", err)
		return 1
	}
	return runUpdateWithRunnerAtExecutable(args, stdin, stdout, stderr, runner, list,
		signal, restart, graceful, executable, postInstallWithResult)
}

type updatePostInstall func(executable string, stdout, stderr io.Writer) error
type updatePostInstallWithResult func(executable string, stdout, stderr io.Writer) updatePostInstallResult

type updatePostInstallResult struct {
	RefreshedDaemonPID  int
	SupervisedDaemonPID int
	Err                 error
}

func runUpdateWithRunnerAtExecutable(
	args []string,
	stdin io.Reader,
	stdout, stderr io.Writer,
	runner updateRunner,
	list processLister,
	signal processSignaller,
	restart supervisedDaemonRestart,
	graceful bool,
	executable string,
	postInstall updatePostInstallWithResult,
) int {
	var options updateOptions
	flags := updateFlagSet(&options)
	if parsed, code := parseCommandFlags("update", flags, args, stdout, stderr); !parsed {
		return code
	}
	if flags.NArg() != 0 {
		writeCommandError(stderr, "update: unexpected arguments")
		return 2
	}
	channel := options.Channel
	if channel == "" {
		channel = os.Getenv("KIVGRAPH_UPDATE_CHANNEL")
	}
	result, err := runner(context.Background(), update.Options{
		APIBaseURL:     os.Getenv("KIVGRAPH_UPDATE_API_URL"),
		CurrentVersion: version.Value,
		Token:          os.Getenv("KIVGRAPH_GITHUB_TOKEN"),
		ExecutablePath: executable,
		CheckOnly:      options.CheckOnly,
		Channel:        channel,
	})
	if err != nil {
		writeCommandError(stderr, "update: %v", err)
		return 1
	}
	if !result.UpdateAvailable {
		writeSuccess(stdout, "kivgraph is up to date (%s)", result.CurrentVersion)
		return 0
	}
	if options.CheckOnly {
		writeInfo(stdout, "kivgraph update available%s: %s -> %s", channelLabel(result.Channel), result.CurrentVersion, result.LatestVersion)
		return 0
	}
	if !result.Updated {
		writeCommandError(stderr, "update: release %s was not installed", result.LatestVersion)
		return 1
	}
	writeSuccess(stdout, "kivgraph updated%s: %s -> %s", channelLabel(result.Channel), result.CurrentVersion, result.LatestVersion)
	postInstallResult := updatePostInstallResult{}
	if postInstall != nil {
		postInstallResult = postInstall(executable, stdout, stderr)
		if postInstallResult.Err != nil {
			writeCommandError(stderr, "update: refresh installed runtime integrations: %v", postInstallResult.Err)
		}
	}
	stopCode := stopStaleProcesses(stdin, stdout, stderr, list, signal, restart, options.StopStale,
		result.LatestVersion, graceful, postInstallResult.RefreshedDaemonPID,
		postInstallResult.SupervisedDaemonPID)
	if stopCode != 0 {
		return stopCode
	}
	if postInstallResult.Err != nil {
		return 1
	}
	return 0
}

func channelLabel(channel string) string {
	if channel == "" || channel == update.ChannelStable {
		return ""
	}
	return " (" + channel + " channel)"
}

// stopStaleProcesses offers to end the servers that outlived the bundle they
// were started from.
//
// The update replaced the installation directory, so a `serve` or `ui` that was
// already running keeps answering from the image that was swapped out -- with
// the old tools, the old descriptions and the old bugs -- and nothing in its
// output says so. A client spawned it and will not restart it on its own.
//
// Refusing to stop anything is the default whenever the answer cannot be asked
// for: these are processes a client owns, and ending one silently would look to
// that client exactly like a crash.
//
// That caution is right for `serve` and `ui` and wrong for the daemon, which is
// the one process here with an owner -- ADR 0068 gave it one -- so a supervised
// daemon is restarted before the rest are offered up. See restartTheDaemon.
func stopStaleProcesses(
	stdin io.Reader,
	stdout, stderr io.Writer,
	list processLister,
	signal processSignaller,
	restart supervisedDaemonRestart,
	stopStale bool,
	release string,
	graceful bool,
	refreshedDaemonPID int,
	supervisedDaemonPID int,
) int {
	processes, err := list()
	if err != nil {
		// The update itself succeeded. Failing the command now would say
		// the release did not install, which is false.
		writeWarning(stderr, "update: could not list the processes still running the previous release: %v", err)
		return 0
	}
	targets := stoppableProcesses(processes, os.Getpid())
	for _, protectedPID := range []int{refreshedDaemonPID, supervisedDaemonPID} {
		if protectedPID == 0 {
			continue
		}
		targets = slices.DeleteFunc(targets, func(target procstat.Process) bool {
			return target.PID == protectedPID
		})
	}
	if len(targets) == 0 {
		return 0
	}
	targets, ownership := restartTheDaemon(targets, restart, stdout, stderr)
	if len(targets) == 0 {
		return 0
	}
	writeWarning(stdout, "update: %d process(es) still run the release this update replaced", len(targets))
	for _, target := range targets {
		writeInfo(stdout, "update.stale: pid=%d %s", target.PID, target.Command())
	}
	warnAboutAnUnownedDaemon(stdout, targets, ownership)
	if !stopStale {
		if !promptYes(stdin, stdout, fmt.Sprintf("stop them now so they answer from %s?", release)) {
			writeInfo(stdout, "update: nothing was stopped; run \"kivgraph stop\" when the clients can reconnect")
			return 0
		}
	}
	killed, failed := stopTargets(targets, stdout, stderr, list, signal, graceful)
	if failed != 0 {
		writeResult(stdout, false, "update.stop: FAIL (%d of %d)", failed, len(targets))
		return 1
	}
	writeSuccess(stdout, "update.stop: %d process(es) stopped, %d killed", len(targets)-killed, killed)
	return 0
}

// restartTheDaemon deals with the one stale process that has an owner, and
// returns the targets that are left for the question below it.
//
// It runs before anything is printed, so a machine whose only stale process was
// the supervised daemon says nothing about processes at all: there is nothing
// left for the operator to decide.
func restartTheDaemon(
	targets []procstat.Process,
	restart supervisedDaemonRestart,
	stdout, stderr io.Writer,
) (remaining []procstat.Process, ownership daemonOwnership) {
	// A caller with no restarter establishes nothing, which is not the same as
	// establishing that nobody supervises this.
	if restart == nil || !slices.ContainsFunc(targets, isDaemonProcess) {
		return targets, ownershipUnknown
	}
	outcome, err := restart(targets)
	if err != nil {
		writeWarning(stderr, "update: the supervised daemon was not restarted: %v", err)
		return targets, outcome.Ownership
	}
	if outcome.PID == 0 {
		// A supervised daemon that was not restarted is a unit somebody edited
		// by hand: reported rather than repaired, which is the same answer
		// `daemon status` gives, and the reason the daemon is still stale.
		if outcome.Ownership == ownershipSupervised {
			writeWarning(stdout, "update: %s owns this daemon and could not be used: %s",
				outcome.Label, outcome.Detail)
		}
		return targets, outcome.Ownership
	}
	writeSuccess(stdout, "update.daemon: %s restarted; pid=%d was answering from the replaced release",
		outcome.Label, outcome.PID)
	remaining = make([]procstat.Process, 0, len(targets))
	for _, target := range targets {
		if target.PID != outcome.PID {
			remaining = append(remaining, target)
		}
	}
	return remaining, ownershipSupervised
}

// warnAboutAnUnownedDaemon says why "run kivgraph stop" is bad advice for one
// of the processes just listed.
//
// `stop` asks politely first, and both supervisors leave a clean exit alone on
// purpose -- systemd's `Restart=on-failure`, launchd's `KeepAlive` with
// `SuccessfulExit` false. For a daemon nobody supervises there is nothing to
// leave alone and nothing to bring it back either, so following that advice
// ends with no daemon rather than with a new one. The better the daemon
// behaves, the more certainly it stays down.
//
// It says this only where it was established, which is the point of the
// parameter. A daemon whose unit exists but is hand-edited, or whose restart
// failed, is supervised, and telling its operator to install a supervisor
// would be the one wrong thing to say. A daemon nothing could be established
// about gets no advice at all, because the advice would be a guess.
func warnAboutAnUnownedDaemon(stdout io.Writer, targets []procstat.Process, ownership daemonOwnership) {
	if ownership != ownershipNone || !slices.ContainsFunc(targets, isDaemonProcess) {
		return
	}
	writeWarning(stdout, "update: one of those is a daemon no supervisor owns, so stopping it "+
		"leaves none running; \"kivgraph daemon install\" gives it an owner that starts it again")
}

func isDaemonProcess(process procstat.Process) bool {
	_, command := process.Invocation()
	return command == "daemon"
}

// promptYes asks a yes-or-no question and defaults to no.
//
// A destination that is not a terminal is not asked at all: `kivgraph update`
// runs from scripts and from other agents, and a prompt written into a pipe
// would block on an answer that is never coming.
func promptYes(stdin io.Reader, stdout io.Writer, question string) bool {
	if stdin == nil || !integrationTUIIsInteractive(stdout) {
		return false
	}
	if file, ok := stdin.(*os.File); ok && !isTerminal(file) {
		return false
	}
	styles := styleFor(stdout)
	fmt.Fprintf(stdout, "%s%s%s [y/N] ", styles.accent, question, styles.reset)
	reader := bufio.NewReader(stdin)
	answer, err := reader.ReadString('\n')
	if err != nil && answer == "" {
		fmt.Fprintln(stdout, "")
		return false
	}
	switch strings.ToLower(strings.TrimSpace(answer)) {
	case "y", "yes":
		return true
	default:
		return false
	}
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

// initOptions carries the flags of `kivgraph init`.
type initOptions struct {
	ConfigPath       string
	RepositoriesPath string
	Force            bool
	Languages        string
	Repository       stringList
}

// initFlagSet declares them in one place, so the parser that runs the
// command and the help and completion that describe it read the same
// definitions.
func initFlagSet(options *initOptions) *flag.FlagSet {
	flags := flag.NewFlagSet("init", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	flags.StringVar(&options.ConfigPath, "config", "", "configuration file")
	flags.StringVar(&options.RepositoriesPath, "repositories", "", "repository registry file")
	flags.BoolVar(&options.Force, "force", false, "replace existing configuration files")
	flags.StringVar(&options.Languages, "languages", "go", "comma-separated repository languages")
	flags.Var(&options.Repository, "repository", "register NAME=PATH (repeatable)")
	return flags
}

func runInit(args []string, stdout, stderr io.Writer) int {
	var options initOptions
	flags := initFlagSet(&options)
	if parsed, code := parseCommandFlags("init", flags, args, stdout, stderr); !parsed {
		return code
	}
	if flags.NArg() != 0 {
		writeCommandError(stderr, "init: unexpected arguments: %v", flags.Args())
		return 2
	}

	result, err := config.Initialize(config.InitOptions{
		ConfigPath:       options.ConfigPath,
		RepositoriesPath: options.RepositoriesPath,
		Force:            options.Force,
	})
	if err != nil {
		writeCommandError(stderr, "init: %v", err)
		return 1
	}
	writeInfo(stdout, "config: %s (%s)", initFileState(result.ConfigCreated), result.ConfigPath)
	writeInfo(stdout, "repositories: %s (%s)", initFileState(result.RepositoriesCreated), result.RepositoriesPath)

	if len(options.Repository) == 0 {
		return 0
	}
	parsedLanguages, err := parseLanguages(options.Languages)
	if err != nil {
		writeCommandError(stderr, "init: %v", err)
		return 2
	}
	additions := make([]config.Repository, 0, len(options.Repository))
	for _, specification := range options.Repository {
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

// indexFullOptions carries the flags of `kivgraph index --full`.
type indexFullOptions struct {
	ConfigPath       string
	RepositoriesPath string
	ResolverVersion  string
	Profile          string
	JSONOutput       bool
}

// indexFullFlagSet declares them in one place, so the parser that runs the
// command and the help and completion that describe it read the same
// definitions.
func indexFullFlagSet(options *indexFullOptions) *flag.FlagSet {
	flags := flag.NewFlagSet("index --full", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	flags.StringVar(&options.ConfigPath, "config", "", "configuration file")
	flags.StringVar(&options.RepositoriesPath, "repositories", "", "repository registry file override")
	flags.StringVar(&options.ResolverVersion, "resolver-version", version.Value, "resolver version recorded in the graph")
	flags.StringVar(&options.Profile, "profile", "", "graph profile (defaults to profiles.default)")
	flags.BoolVar(&options.JSONOutput, "json", false, "write the pass as a JSON event stream on stdout")
	return flags
}

// writeIndexSummary reports what each language pass observed, one line per
// language, always all five. A language absent from the output reads as a
// language with no code, and the not-loaded counts are what keep that from
// being confused with a repository this machine could not read: they were
// counted before they were printed, and a count nobody prints is a warning
// nobody has.
func writeIndexSummary(stdout io.Writer, indexReport indexer.FullReport) {
	writeInfo(stdout, "index.go: repositories=%d modules=%d not_loaded=%d workspaces=%d loads=%d definitions=%d references=%d unresolved=%d diagnostics=%d",
		indexReport.GoRepositories,
		indexReport.GoModules,
		indexReport.GoModulesNotLoaded,
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
	writeInfo(stdout, "index.rust: repositories=%d workspaces=%d crates=%d not_loaded=%d symbols=%d references=%d unresolved=%d",
		indexReport.RustRepositories,
		indexReport.RustWorkspaces,
		indexReport.RustCrates,
		indexReport.RustWorkspacesNotLoaded,
		indexReport.RustSymbols,
		indexReport.RustReferences,
		indexReport.RustUnresolved,
	)
	writeInfo(stdout, "index.python: repositories=%d not_loaded=%d symbols=%d references=%d unresolved=%d",
		indexReport.PythonRepositories,
		indexReport.PythonRepositoriesNotLoaded,
		indexReport.PythonSymbols,
		indexReport.PythonReferences,
		indexReport.PythonUnresolved,
	)
	writeInfo(stdout, "index.dart: repositories=%d not_loaded=%d symbols=%d references=%d unresolved=%d",
		indexReport.DartRepositories,
		indexReport.DartRepositoriesNotLoaded,
		indexReport.DartSymbols,
		indexReport.DartReferences,
		indexReport.DartUnresolved,
	)
	writeInfo(stdout, "index.java: repositories=%d not_loaded=%d symbols=%d references=%d unresolved=%d",
		indexReport.JavaRepositories,
		indexReport.JavaRepositoriesNotLoaded,
		indexReport.JavaSymbols,
		indexReport.JavaReferences,
		indexReport.JavaUnresolved,
	)
	writeInfo(stdout, "index.csharp: repositories=%d not_loaded=%d symbols=%d references=%d unresolved=%d",
		indexReport.CSharpRepositories,
		indexReport.CSharpRepositoriesNotLoaded,
		indexReport.CSharpSymbols,
		indexReport.CSharpReferences,
		indexReport.CSharpUnresolved,
	)
}

func runIndexFull(args []string, stdout, stderr io.Writer) int {
	var options indexFullOptions
	flags := indexFullFlagSet(&options)
	if parsed, code := parseCommandFlags("index --full", flags, args, stdout, stderr); !parsed {
		return code
	}
	if flags.NArg() != 0 {
		writeCommandError(stderr, "index --full: unexpected arguments: %v", flags.Args())
		return 2
	}

	ctx := context.Background()
	loaded, err := config.LoadProfile(options.ConfigPath, options.Profile)
	if err != nil {
		writeCommandError(stderr, "index --full: load configuration: %v", err)
		return 1
	}
	if options.RepositoriesPath != "" {
		loaded.Repositories, err = config.LoadRepositories(options.RepositoriesPath)
		if err != nil {
			writeCommandError(stderr, "index --full: load repositories: %v", err)
			return 1
		}
	}
	registry, err := registryForProfile(ctx, loaded)
	if err != nil {
		writeCommandError(stderr, "index --full: register profile repositories: %v", err)
		return 1
	}
	if !options.JSONOutput {
		writeProfileDiagnostics(stdout, loaded.Profile, registry)
	}
	workingDirectory, err := os.Getwd()
	if err != nil {
		writeCommandError(stderr, "index --full: resolve working directory: %v", err)
		return 1
	}
	progressStart := time.Now()
	events := openEventLog(loaded.Config, stderr)
	defer events.Close()
	indexOptions := indexing.OptionsFromConfig(loaded.Config)
	indexOptions.Profile = loaded.Profile
	indexOptions.SharedTargetsLockPath = filepath.Join(stateDirectory(loaded), "analyzer-targets.lock")
	indexOptions.Repositories = registry.List()
	if composition, present := registry.Composition(); present {
		indexOptions.Composition = &composition
	}
	indexOptions.WorkingDirectory = workingDirectory
	indexOptions.ResolverVersion = options.ResolverVersion
	invalidationManager, err := invalidation.Open(stateDirectory(loaded))
	if err != nil {
		writeCommandError(stderr, "index --full: open invalidation state: %v", err)
		return 1
	}
	indexOptions.Invalidation = invalidationManager
	if options.JSONOutput {
		return runIndexFullEvents(ctx, indexOptions, events, progressStart, stdout, stderr)
	}
	events.Append(eventlog.Event{
		Kind:    eventlog.KindIndex,
		Message: fmt.Sprintf("index --full started over %d repository(ies)", len(indexOptions.Repositories)),
	})
	indexOptions.Progress = func(event indexer.ProgressEvent) {
		writeIndexProgress(stderr, progressStart, event)
	}
	indexOptions.RebuildProgress = func(stage rebuild.StageName) {
		writeInfo(stderr, "[%6.1fs] rebuild %s", time.Since(progressStart).Seconds(), stage)
	}
	fullResult, err := indexing.RunFull(ctx, indexOptions)
	recordIndexRun(events, fullResult.RebuildReport, int64(fullResult.Counts.Symbols), time.Since(progressStart), err)
	if fullResult.RecordingError != nil {
		writeWarning(stdout, "index.recording: %v", fullResult.RecordingError)
	}
	indexReport := fullResult.IndexReport
	writeResult(stdout, err == nil, "index.full: %s", passFail(err == nil))
	writeIndexSummary(stdout, indexReport)
	// A count says something happened; the lines say what. Both are on
	// stdout with the rest of the report, because a warning in a log the
	// caller is not reading is a warning nobody has.
	for _, module := range boundedReportLines(indexReport.GoModulesNotRead, 20) {
		writeWarning(stdout, "index.go.not_read: %s", module)
	}
	for _, diagnostic := range boundedReportLines(indexReport.GoDiagnostics, 20) {
		writeWarning(stdout, "index.go.diagnostic: %s", diagnostic)
	}
	for _, repository := range boundedReportLines(indexReport.TypeScriptWithoutPackages, 20) {
		writeWarning(stdout, "index.typescript.no_package: %s declares no package with a TypeScript project, so it contributes nothing", repository)
	}
	for _, diagnostic := range boundedReportLines(indexReport.RustDiagnostics, 20) {
		writeWarning(stdout, "index.rust.diagnostic: %s", diagnostic)
	}
	for _, repository := range boundedReportLines(indexReport.RustWithoutWorkspaces, 20) {
		writeWarning(stdout, "index.rust.no_workspace: %s declares no Cargo manifest, so it contributes nothing", repository)
	}
	if cache := indexReport.Cache; cache.Mode != "" && cache.Mode != indexer.CacheOff {
		writeInfo(stdout, "index.cache: mode=%s hits=%d misses=%d verified=%d",
			cache.Mode, cache.Hits, cache.Misses, cache.Verified)
	}
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

// runIndexFullEvents runs the pass for a caller that reads it rather than a
// person: stdout carries only the event stream internal/indexing declares, so
// the report is not written at all and the progress a person would read on
// stderr travels as events instead.
//
// This is what a server runs when it indexes, and the reason it can: the pass
// happens in this process, which exits, instead of in the one answering
// queries, which does not. See ADR 0042.
func runIndexFullEvents(
	ctx context.Context,
	options indexing.FullOptions,
	events *eventlog.Writer,
	started time.Time,
	stdout, stderr io.Writer,
) int {
	events.Append(eventlog.Event{
		Kind:    eventlog.KindIndex,
		Message: fmt.Sprintf("index --full started over %d repository(ies)", len(options.Repositories)),
	})
	encoder := json.NewEncoder(stdout)
	emit := func(event indexing.FullEvent) {
		// A caller that stopped reading is not a reason to abandon a pass
		// that is about to publish a generation.
		_ = encoder.Encode(event)
	}
	report := func(progress indexing.ProjectProgress) {
		emit(indexing.FullEvent{Event: indexing.FullEventProgress, Progress: &progress})
	}
	options.Progress = func(event indexer.ProgressEvent) {
		report(indexing.ProgressFromEvent(event))
	}
	options.RebuildProgress = func(stage rebuild.StageName) {
		report(indexing.ProjectProgress{Phase: "rebuild", Detail: string(stage)})
	}

	result, err := indexing.RunFull(ctx, options)
	recordIndexRun(events, result.RebuildReport, int64(result.Counts.Symbols), time.Since(started), err)
	document := indexing.DocumentFromResult(result)
	if err != nil {
		document.Passed = false
		document.Error = err.Error()
	}
	emit(indexing.FullEvent{Event: indexing.FullEventResult, Result: &document})
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

// upgradeOptions carries the flags of `kivgraph upgrade`.
type upgradeOptions struct {
	ConfigPath       string
	RepositoriesPath string
	ResolverVersion  string
}

// upgradeFlagSet declares them in one place, so the parser that runs the
// command and the help and completion that describe it read the same
// definitions.
func upgradeFlagSet(options *upgradeOptions) *flag.FlagSet {
	flags := flag.NewFlagSet("upgrade", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	flags.StringVar(&options.ConfigPath, "config", "", "configuration file")
	flags.StringVar(&options.RepositoriesPath, "repositories", "", "repository registry file override")
	flags.StringVar(&options.ResolverVersion, "resolver-version", version.Value, "resolver version recorded in the graph")
	return flags
}

func runUpgrade(args []string, stdout, stderr io.Writer) int {
	var options upgradeOptions
	flags := upgradeFlagSet(&options)
	if parsed, code := parseCommandFlags("upgrade", flags, args, stdout, stderr); !parsed {
		return code
	}
	if flags.NArg() != 0 {
		writeCommandError(stderr, "upgrade: unexpected arguments: %v", flags.Args())
		return 2
	}

	ctx := context.Background()
	loaded, err := config.Load(options.ConfigPath)
	if err != nil {
		writeCommandError(stderr, "upgrade: load configuration: %v", err)
		return 1
	}
	if options.RepositoriesPath != "" {
		loaded.Repositories, err = config.LoadRepositories(options.RepositoriesPath)
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
		ResolverVersion: options.ResolverVersion,
		Full: indexer.FullOptions{
			Repositories:                      registry.List(),
			SyntheticWorkFile:                 loaded.Config.Go.SyntheticWorkFile,
			IncludeTests:                      loaded.Config.Go.IncludeTests,
			GoBuildTags:                       loaded.Config.Go.BuildTags,
			GoAllowNetwork:                    loaded.Config.Go.AllowNetwork,
			GoMaximumLoads:                    loaded.Config.Go.MaximumLoads,
			TypeScriptMaximumWorkers:          loaded.Config.TypeScript.MaximumWorkers,
			TypeScriptWorker:                  loaded.Config.TypeScript.WorkerCommand,
			TypeScriptIncludeUnclaimedSources: loaded.Config.TypeScript.IncludeUnclaimedSources,
			CacheMode:                         indexer.CacheMode(loaded.Config.Indexing.FactCache),
			CacheDirectory:                    loaded.Config.Indexing.FactCachePath,
			WorkingDirectory:                  workingDirectory,
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

// cleanOptions carries the flags of `kivgraph clean`.
type cleanOptions struct {
	ConfigPath string
	KeepActive bool
	Confirmed  bool
}

// cleanFlagSet declares them in one place, so the parser that runs the
// command and the help and completion that describe it read the same
// definitions.
func cleanFlagSet(options *cleanOptions) *flag.FlagSet {
	flags := flag.NewFlagSet("clean", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	flags.StringVar(&options.ConfigPath, "config", "", "configuration file")
	flags.BoolVar(&options.KeepActive, "keep-active", false, "keep the generation currently published")
	flags.BoolVar(&options.Confirmed, "yes", false, "remove the generations instead of listing them")
	return flags
}

// runClean removes published graph generations.
//
// It reports what it would remove and changes nothing until --yes, because a
// typo here costs a full reindex and there is no undo: rollback restores a
// backup generation, and this command is what removes those too.
func runClean(args []string, stdout, stderr io.Writer) int {
	var options cleanOptions
	flags := cleanFlagSet(&options)
	if parsed, code := parseCommandFlags("clean", flags, args, stdout, stderr); !parsed {
		return code
	}
	if flags.NArg() != 0 {
		writeCommandError(stderr, "clean: unexpected arguments: %v", flags.Args())
		return 2
	}

	ctx := context.Background()
	loaded, err := config.Load(options.ConfigPath)
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

	if options.KeepActive && active.ID == "" {
		writeCommandError(stderr, "clean: --keep-active: no generation is published; run clean without it to remove everything")
		return 1
	}
	doomed := make([]string, 0, len(generations))
	for _, candidate := range generations {
		if options.KeepActive && candidate.ID == active.ID {
			continue
		}
		doomed = append(doomed, candidate.ID)
	}
	if len(doomed) == 0 {
		writeInfo(stdout, "clean: nothing to remove (%d generation(s) kept)", len(generations))
		return 0
	}
	if !options.Confirmed {
		writeInfo(stdout, "clean: would remove generation(s) %s from %s", strings.Join(doomed, ", "), root)
		if !options.KeepActive {
			writeInfo(stdout, "clean: the graph would be unpublished; every query fails until the next index --full")
		}
		writeInfo(stdout, "clean: nothing was removed; pass --yes to proceed")
		return 0
	}

	removed, err := cleanGenerations(ctx, store, options.KeepActive, active.ID)
	if err != nil {
		writeCommandError(stderr, "clean: %v", err)
		return 1
	}
	writeResult(stdout, true, "clean: removed generation(s) %s", strings.Join(removed, ", "))
	if options.KeepActive {
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

// doctorOptions carries the flags of `kivgraph doctor`.
type doctorOptions struct {
	ConfigPath string
}

// doctorFlagSet declares them in one place, so the parser that runs the
// command and the help and completion that describe it read the same
// definitions.
func doctorFlagSet(options *doctorOptions) *flag.FlagSet {
	flags := flag.NewFlagSet("doctor", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	flags.StringVar(&options.ConfigPath, "config", "", "configuration file")
	return flags
}

func runDoctor(args []string, stdout, stderr io.Writer) int {
	var options doctorOptions
	flags := doctorFlagSet(&options)
	if parsed, code := parseCommandFlags("doctor", flags, args, stdout, stderr); !parsed {
		return code
	}
	if flags.NArg() != 0 {
		writeCommandError(stderr, "doctor: unexpected arguments: %v", flags.Args())
		return 2
	}

	loaded, err := config.Load(options.ConfigPath)
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
	// A retired key is not a defect in the store and not a reason to fail: the
	// file was valid when it was written and the key never did anything. Saying
	// so is what lets someone delete it; silence would leave it there forever.
	if len(loaded.RetiredKeys) > 0 {
		doctorResult("config.retired", true, fmt.Sprintf(
			"%s: accepted and ignored, safe to delete", strings.Join(loaded.RetiredKeys, ", ")))
	}

	for _, stateDirectory := range []struct {
		name string
		path string
	}{
		{name: "state.database_parent", path: filepath.Dir(loaded.Config.Storage.DatabasePath)},
		{name: "state.backups", path: loaded.Config.Storage.BackupsPath},
		{name: "state.synthetic_parent", path: filepath.Dir(loaded.Config.Go.SyntheticWorkFile)},
		{name: "state.fact_cache_parent", path: factCacheStateParent(loaded.Config)},
	} {
		passed, detail := inspectDoctorDirectory(stateDirectory.path)
		doctorResult(stateDirectory.name, passed, detail)
	}
	// Whether the daemon has an owner is a fact about this installation, not a
	// defect: an absent supervisor is the state of a machine that never asked
	// for one, and a client registered against `serve` needs no daemon at all.
	// It is reported because it is what decides whether a `url` registration is
	// safe -- an unsupervised daemon takes every client down with it -- and a
	// reader cannot see it any other way.
	reportDoctorSupervisor(doctorResult, loaded)

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
		// Whether the generation carries a usable snapshot decides whether every
		// server on this machine reads it or derives the graph, and the two are
		// invisible from the outside: they produce the same answers and differ by
		// a gigabyte of peak per install. Absence is not a failure -- a generation
		// published before the file existed has none, and deriving is what always
		// happened -- but a file that is there and cannot be used is, because
		// something in the store is wrong.
		published, publishedErr := rebuild.InspectPublishedSnapshot(layout.Active.Path)
		switch {
		case errors.Is(publishedErr, rebuild.ErrNoPublishedSnapshot):
			doctorResult("snapshot.published", true,
				"absent, so every server derives the graph from the canonical store")
		case errors.Is(publishedErr, hotsnapshot.ErrSnapshotFileVersion),
			errors.Is(publishedErr, rebuild.ErrNoRecordedGraphDigest):
			// Same class as absent, and for the same reason: nothing is wrong
			// with the store, the layout moved. Reporting an upgrade as a
			// failure is how a real failure stops being noticed.
			doctorResult("snapshot.published", true,
				publishedErr.Error()+"; the next index replaces it, and until then every server derives the graph")
		case publishedErr != nil:
			doctorResult("snapshot.published", false, publishedErr.Error())
		default:
			doctorResult("snapshot.published", true, fmt.Sprintf("%s (%d symbols, %d bytes)",
				published.Path, published.Symbols, published.Bytes))
		}
		snapshotID, snapshotIDErr := snapshotReportID(layout.Active.ID)
		if snapshotIDErr != nil {
			doctorResult("snapshot", false, snapshotIDErr.Error())
			doctorResult("unresolved", false, "snapshot unavailable")
		} else {
			// doctor derives the snapshot and never reads the published one,
			// which is not an oversight to optimise away: what it reports is
			// whether *this graph* can still become a snapshot. Reading a file
			// that was written when the graph was healthy would answer a
			// different question and answer it reassuringly.
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
	needsRust := false
	needsPython := false
	needsDart := false
	needsJava := false
	needsCSharp := false
	for _, repository := range repositories {
		for _, language := range repository.Languages {
			switch strings.ToLower(strings.TrimSpace(language)) {
			case "go":
				needsGo = true
			case "typescript", "javascript", "ts", "js":
				needsTypeScript = true
			case "rust", "rs":
				needsRust = true
			case "python", "py":
				needsPython = true
			case "dart":
				needsDart = true
			case "java":
				needsJava = true
			case "csharp", "cs":
				needsCSharp = true
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
	reportTypeScriptToolchain(report, configuration, needsTypeScript)
	reportRustToolchain(report, configuration, needsRust)
	reportPythonToolchain(report, configuration, needsPython)
	reportExternalToolchain(report, "dart", configuration.Dart.AnalyzerCommand, needsDart)
	reportExternalToolchain(report, "java", configuration.Java.IndexerCommand, needsJava)
	reportExternalToolchain(report, "csharp", configuration.CSharp.IndexerCommand, needsCSharp)
}

func reportPythonToolchain(report func(string, bool, string), configuration config.Config, needed bool) {
	if !needed {
		report("toolchain.python", true, "not configured")
		return
	}
	command := strings.Fields(strings.TrimSpace(configuration.Python.IndexerCommand))
	if len(command) != 0 {
		if resolved, err := exec.LookPath(command[0]); err == nil {
			report("toolchain.python", true, resolved)
			return
		}
	}
	pythonPath := strings.TrimSpace(configuration.Python.PythonPath)
	if pythonPath == "" {
		pythonPath = "python3"
	}
	if resolved, err := exec.LookPath(strings.Fields(pythonPath)[0]); err == nil {
		report("toolchain.python", true, fmt.Sprintf("bundled AST fallback (%s)", resolved))
		return
	}
	if len(command) == 0 {
		report("toolchain.python", false, "indexer command is empty and Python fallback is unavailable")
		return
	}
	report("toolchain.python", false, fmt.Sprintf("command %q and Python fallback are unavailable", command[0]))
}

func reportExternalToolchain(report func(string, bool, string), language, configured string, needed bool) {
	if !needed {
		report("toolchain."+language, true, "not configured")
		return
	}
	command := strings.Fields(strings.TrimSpace(configured))
	if len(command) == 0 {
		report("toolchain."+language, false, "command is empty")
		return
	}
	if resolved, err := exec.LookPath(command[0]); err == nil {
		report("toolchain."+language, true, resolved)
		return
	}
	report("toolchain."+language, false, fmt.Sprintf("command %q is unavailable", command[0]))
}

func reportTypeScriptToolchain(report func(string, bool, string), configuration config.Config, needsTypeScript bool) {
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
	if err == nil && command[0] == "kivgraph-ts-worker" {
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

// reportRustToolchain states whether the external analyzer this build depends
// on for Rust is present, and which one.
//
// Rust is the one language Kivgraph does not analyse itself: `rust-analyzer`
// is a prerequisite, like the Node runtime of the TypeScript worker, and a
// missing one is a repository that will contribute nothing.
func reportRustToolchain(report func(string, bool, string), configuration config.Config, needsRust bool) {
	if !needsRust {
		report("toolchain.rust", true, "not configured")
		return
	}
	command := strings.Fields(strings.TrimSpace(configuration.Rust.AnalyzerCommand))
	if len(command) == 0 {
		report("toolchain.rust", false, "analyzer command is empty")
		return
	}
	resolved, source, err := rustloader.ResolveAnalyzer(command[0])
	if err != nil {
		report("toolchain.rust", false, fmt.Sprintf("command %q is unavailable", command[0]))
		return
	}
	arguments := append(append([]string(nil), command[1:]...), "--version")
	output, err := exec.CommandContext(context.Background(), resolved, arguments...).CombinedOutput()
	if err != nil {
		report("toolchain.rust", false, fmt.Sprintf("%s --version failed: %v", resolved, err))
		return
	}
	// Which binary answers matters as much as its version: a bundle ships its
	// own, and a PATH may hold another.
	report("toolchain.rust", true, fmt.Sprintf("%s (%s)", strings.TrimSpace(string(output)), source))

	// The analyzer cannot load a Cargo workspace without cargo, and the bundle
	// does not ship a Rust toolchain: a missing cargo is a failure even when
	// the analyzer travels inside the installation.
	cargoOutput, cargoErr := exec.CommandContext(context.Background(), "cargo", "--version").CombinedOutput()
	if cargoErr != nil {
		report("toolchain.cargo", false, "cargo is unavailable, so no workspace can be loaded")
		return
	}
	report("toolchain.cargo", true, strings.TrimSpace(string(cargoOutput)))
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

// factCacheStateParent answers where the fact cache will live. The directory
// itself is created by the first pass that uses it, so what doctor can check
// beforehand is the parent it will be created in.
func factCacheStateParent(configuration config.Config) string {
	if configuration.Indexing.FactCache == string(indexer.CacheOff) {
		return filepath.Dir(configuration.Storage.DatabasePath)
	}
	return filepath.Dir(configuration.Indexing.FactCachePath)
}

func inspectDoctorDirectory(path string) (bool, string) {
	info, err := os.Stat(path)
	if err != nil {
		return false, err.Error()
	}
	if !info.IsDir() {
		return false, "not a directory"
	}
	// Whether the directory is the owner's alone is a question with no answer
	// on a platform that does not keep POSIX mode bits. Go reports every
	// directory on Windows as 0777, so asking here would fail every state
	// directory on every machine, and a check that cannot be wrong is not a
	// check -- it is a permanent red that teaches an operator to ignore the
	// whole report. What the platform does have is an ACL, which nothing here
	// sets yet, so the honest answer names the gap instead of asserting either
	// side of it. workspace.validateDirectoryPermissions already declines the
	// same question for the same reason.
	if runtime.GOOS == "windows" {
		return true, path + " (privacy unchecked: this platform keeps an ACL, not mode bits)"
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

// doctorStorageOptions carries the flags of `kivgraph doctor storage`.
type doctorStorageOptions struct {
	Database string
}

// doctorStorageFlagSet declares them in one place, so the parser that runs
// the command and the help and completion that describe it read the same
// definitions.
func doctorStorageFlagSet(options *doctorStorageOptions) *flag.FlagSet {
	flags := flag.NewFlagSet("doctor storage", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	flags.StringVar(&options.Database, "database", "", "existing LadybugDB database path")
	return flags
}

func runDoctorStorage(args []string, stdout, stderr io.Writer, diagnose storageDiagnoser) int {
	var options doctorStorageOptions
	flags := doctorStorageFlagSet(&options)
	if parsed, code := parseCommandFlags("doctor storage", flags, args, stdout, stderr); !parsed {
		return code
	}
	if flags.NArg() != 0 {
		writeCommandError(stderr, "doctor storage: unexpected arguments: %v", flags.Args())
		return 2
	}
	if options.Database == "" {
		writeCommandError(stderr, "doctor storage: --database is required")
		return 2
	}

	diagnosis, err := diagnose(context.Background(), options.Database)
	if err != nil {
		writeCommandError(stderr, "doctor storage: %v", err)
		return 1
	}
	state := "FAIL"
	if diagnosis.Healthy {
		state = "PASS"
	}
	writeResult(stdout, diagnosis.Healthy, "storage doctor: %s", state)
	writeInfo(stdout, "database: %s", diagnosis.Path)
	// A diagnosis that does not say which layout it validated cannot be
	// interpreted: the same path can hold either schema.
	if diagnosis.Schema == ladybug.SchemaCanonical {
		writeInfo(stdout, "schema: %s (version %d)", diagnosis.Schema, diagnosis.SchemaVersion)
	} else {
		writeInfo(stdout, "schema: %s", diagnosis.Schema)
	}
	for _, check := range diagnosis.Checks {
		writeResult(stdout, check.Status == ladybug.DiagnosticPass,
			"[%s] %s: %s", check.Status, check.Name, check.Detail)
	}
	if diagnosis.Healthy {
		return 0
	}
	return 1
}

// doctorGraphOptions carries the flags of `kivgraph doctor graph`.
type doctorGraphOptions struct {
	Database string
}

// doctorGraphFlagSet declares them in one place, so the parser that runs the
// command and the help and completion that describe it read the same
// definitions.
func doctorGraphFlagSet(options *doctorGraphOptions) *flag.FlagSet {
	flags := flag.NewFlagSet("doctor graph", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	flags.StringVar(&options.Database, "database", "", "published canonical LadybugDB database path")
	return flags
}

func runDoctorGraph(args []string, stdout, stderr io.Writer, verify graphVerifier) int {
	var options doctorGraphOptions
	flags := doctorGraphFlagSet(&options)
	if parsed, code := parseCommandFlags("doctor graph", flags, args, stdout, stderr); !parsed {
		return code
	}
	if flags.NArg() != 0 {
		writeCommandError(stderr, "doctor graph: unexpected arguments: %v", flags.Args())
		return 2
	}
	if options.Database == "" {
		writeCommandError(stderr, "doctor graph: --database is required")
		return 2
	}

	report, err := verify(context.Background(), options.Database)
	if err != nil {
		writeCommandError(stderr, "doctor graph: %v", err)
		return 1
	}
	writeIntegrityReport(stdout, options.Database, report)
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
	writeResult(stdout, report.Passed, "graph doctor: %s", state)
	writeInfo(stdout, "database: %s", databasePath)
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
		writeResult(stdout, finding.Passed, "[%s] %s: %d violation(s)", findingState, finding.Rule, finding.Violations)
		if finding.Passed {
			continue
		}
		for _, sample := range finding.Samples {
			fmt.Fprintf(stdout, "    %s %s: %s\n", sample.Table, sample.Key, sample.Detail)
		}
	}
}

// generateGraphOptions carries the flags of `kivgraph generate-graph`.
type generateGraphOptions struct {
	Config synthetic.Config
}

// generateGraphFlagSet declares them in one place, so the parser that runs
// the command and the help and completion that describe it read the same
// definitions.
func generateGraphFlagSet(options *generateGraphOptions) *flag.FlagSet {
	options.Config = synthetic.DefaultConfig()
	flags := flag.NewFlagSet("generate-graph", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	flags.IntVar(&options.Config.Repositories, "repositories", options.Config.Repositories, "number of repositories")
	flags.IntVar(&options.Config.Files, "files", options.Config.Files, "number of files")
	flags.IntVar(&options.Config.Symbols, "symbols", options.Config.Symbols, "number of symbols")
	flags.IntVar(&options.Config.Edges, "edges", options.Config.Edges, "number of total edges")
	flags.Int64Var(&options.Config.Seed, "seed", options.Config.Seed, "deterministic corpus seed")
	flags.StringVar(&options.Config.OutputDir, "output", options.Config.OutputDir, "output directory")
	return flags
}

func runGenerateGraph(args []string, stdout, stderr io.Writer) int {
	var options generateGraphOptions
	flags := generateGraphFlagSet(&options)
	if parsed, code := parseCommandFlags("generate-graph", flags, args, stdout, stderr); !parsed {
		return code
	}
	if flags.NArg() != 0 {
		writeCommandError(stderr, "generate-graph: unexpected arguments: %v", flags.Args())
		return 2
	}

	manifest, err := synthetic.Generate(context.Background(), options.Config)
	if err != nil {
		writeCommandError(stderr, "generate-graph: %v", err)
		return 1
	}
	writeSuccess(stdout, "generated %d repositories, %d files, %d symbols, %d edges at %s (seed %d)",
		manifest.Repositories,
		manifest.Files,
		manifest.Symbols,
		manifest.Edges,
		options.Config.OutputDir,
		manifest.Seed,
	)
	return 0
}

// rebuildOptions carries the flags of `kivgraph rebuild`.
type rebuildOptions struct {
	Facts           string
	Root            string
	Generation      string
	ResolverVersion string
	SnapshotID      int64
}

// rebuildFlagSet declares them in one place, so the parser that runs the
// command and the help and completion that describe it read the same
// definitions.
func rebuildFlagSet(options *rebuildOptions) *flag.FlagSet {
	flags := flag.NewFlagSet("rebuild", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	flags.StringVar(&options.Facts, "facts", "", "JSON file with a serialized facts.Set")
	flags.StringVar(&options.Root, "root", "", "generation store root directory")
	flags.StringVar(&options.Generation, "generation", "", "six digit generation id to publish")
	flags.StringVar(&options.ResolverVersion, "resolver-version", "", "resolver version stamped on every semantic edge")
	flags.Int64Var(&options.SnapshotID, "snapshot-id", 0, "snapshot id stamped on every semantic edge")
	return flags
}

func runRebuild(args []string, stdout, stderr io.Writer, rebuilder graphRebuilder) int {
	var options rebuildOptions
	flags := rebuildFlagSet(&options)
	if parsed, code := parseCommandFlags("rebuild", flags, args, stdout, stderr); !parsed {
		return code
	}
	if flags.NArg() != 0 {
		writeCommandError(stderr, "rebuild: unexpected arguments: %v", flags.Args())
		return 2
	}
	switch {
	case options.Facts == "":
		writeCommandError(stderr, "rebuild: --facts is required")
		return 2
	case options.Root == "":
		writeCommandError(stderr, "rebuild: --root is required")
		return 2
	case options.Generation == "":
		writeCommandError(stderr, "rebuild: --generation is required")
		return 2
	case options.ResolverVersion == "":
		writeCommandError(stderr, "rebuild: --resolver-version is required")
		return 2
	}

	factsData, err := os.ReadFile(options.Facts)
	if err != nil {
		writeCommandError(stderr, "rebuild: read facts: %v", err)
		return 1
	}
	var set facts.Set
	if err := json.Unmarshal(factsData, &set); err != nil {
		writeCommandError(stderr, "rebuild: decode facts: %v", err)
		return 1
	}

	report, err := rebuilder(context.Background(), rebuild.Options{
		Root:            options.Root,
		GenerationID:    options.Generation,
		Facts:           set,
		ResolverVersion: options.ResolverVersion,
		SnapshotID:      options.SnapshotID,
		Store:           generation.DefaultConfig(),
	})
	writeRebuildReport(stdout, report)
	if err != nil {
		writeCommandError(stderr, "rebuild: %v", err)
		return 1
	}
	if !report.Passed {
		writeCommandError(stderr, "rebuild: %s", rebuildFailureReason(report))
		return 1
	}
	return 0
}

// writeRebuildReport transcribes the pipeline report Run already computed;
// it never re-derives pass/fail so stdout and the exit code cannot disagree.
func writeRebuildReport(stdout io.Writer, report rebuild.Report) {
	for _, stage := range report.Stages {
		message := fmt.Sprintf("[%s] %s: %.2fms", rebuildState(stage.Passed), stage.Name, stage.DurationMS)
		if stage.Detail != "" {
			message += fmt.Sprintf(" - %s", stage.Detail)
		}
		writeResult(stdout, stage.Passed, "%s", message)
	}
	for _, check := range report.Integrity {
		if check.Passed {
			continue
		}
		writeCommandError(stdout, "[FAIL] integrity %s: expected %d, observed %d", check.Table, check.Expected, check.Observed)
	}
	for _, finding := range report.Invariants.Findings {
		if finding.Passed {
			continue
		}
		writeCommandError(stdout, "[FAIL] invariant %s: %d violation(s)", finding.Rule, finding.Violations)
		for _, sample := range finding.Samples {
			fmt.Fprintf(stdout, "    %s %s: %s\n", sample.Table, sample.Key, sample.Detail)
		}
	}
	for _, probe := range report.Probes {
		if probe.Passed {
			continue
		}
		writeCommandError(stdout, "[FAIL] probe %s: %s", probe.Probe, probe.Detail)
	}
	if report.SnapshotDigest != "" {
		writeInfo(stdout, "snapshot digest: %s", report.SnapshotDigest)
	} else {
		writeInfo(stdout, "snapshot digest: none")
	}
	if report.Publication.Generation.ID != "" {
		writeSuccess(stdout, "generation published: %s (%s)", report.Publication.Generation.ID, report.Publication.Generation.Path)
	} else {
		writeInfo(stdout, "generation published: none")
	}
	if len(report.Pruned) != 0 {
		writeInfo(stdout, "generations pruned: %s", strings.Join(report.Pruned, ", "))
	} else {
		writeInfo(stdout, "generations pruned: none")
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

// graphStatusOptions carries the flags of `kivgraph graph status`.
type graphStatusOptions struct {
	Root string
}

// graphStatusFlagSet declares them in one place, so the parser that runs the
// command and the help and completion that describe it read the same
// definitions.
func graphStatusFlagSet(options *graphStatusOptions) *flag.FlagSet {
	flags := flag.NewFlagSet("graph status", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	flags.StringVar(&options.Root, "root", "", "generation store root directory")
	return flags
}

func runGraphStatus(args []string, stdout, stderr io.Writer, roles graphRoleResolver) int {
	var options graphStatusOptions
	flags := graphStatusFlagSet(&options)
	if parsed, code := parseCommandFlags("graph status", flags, args, stdout, stderr); !parsed {
		return code
	}
	if flags.NArg() != 0 {
		writeCommandError(stderr, "graph status: unexpected arguments: %v", flags.Args())
		return 2
	}
	if options.Root == "" {
		writeCommandError(stderr, "graph status: --root is required")
		return 2
	}

	layout, err := roles(context.Background(), rebuild.LayoutOptions{Root: options.Root, Store: generation.DefaultConfig()})
	if err != nil {
		writeCommandError(stderr, "graph status: %v", err)
		return 1
	}
	writeGraphStatus(stdout, options.Root, layout)
	return 0
}

// writeGraphStatus prints the three roles LUQUE-0905 requires
// (graph.active, graph.next, graph.backup) with the path each one names on
// disk, plus the full retained set. graph.active and graph.backup print
// "none" when the store has never published (respectively backed up) a
// generation: that is a legitimate layout, not a rendering error, matching
// the exit code runGraphStatus already returns for it (0).
func writeGraphStatus(stdout io.Writer, root string, layout rebuild.Layout) {
	if integrationTUIIsInteractive(stdout) {
		generationsDir := generation.GenerationsDir(root)
		if absRoot, err := filepath.Abs(root); err == nil {
			generationsDir = generation.GenerationsDir(absRoot)
		}
		active := "none"
		if layout.Active.ID != "" {
			active = fmt.Sprintf("%s (%s)", layout.Active.ID, layout.Active.Path)
		}
		backup := "none"
		if layout.HasBackup {
			backup = fmt.Sprintf("%s (%s)", layout.Backup.ID, layout.Backup.Path)
		}
		retained := "none"
		if len(layout.Retained) != 0 {
			retained = strings.Join(layout.Retained, ", ")
		}
		writeKeyValueTable(stdout, "Graph roles", []keyValueRow{
			{Key: "Root", Value: root},
			{Key: "Active", Value: active},
			{Key: "Next", Value: filepath.Join(generationsDir, layout.NextID+".tmp")},
			{Key: "Backup", Value: backup},
			{Key: "Retained", Value: retained},
		})
		return
	}
	fmt.Fprintf(stdout, "%s: ", rebuild.RoleActive)
	if layout.Active.ID == "" {
		fmt.Fprintln(stdout, "none")
	} else {
		fmt.Fprintf(stdout, "%s (%s)\n", layout.Active.ID, layout.Active.Path)
	}

	// graph.next never exists on disk until a rebuild actually publishes:
	// this is where generation.Store.Publish would build it, following the
	// documented <root>/generations/<id>.tmp layout.
	generationsDir := generation.GenerationsDir(root)
	if absRoot, err := filepath.Abs(root); err == nil {
		generationsDir = generation.GenerationsDir(absRoot)
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

// rollbackOptions carries the flags of `kivgraph rollback`.
type rollbackOptions struct {
	Root       string
	Generation string
}

// rollbackFlagSet declares them in one place, so the parser that runs the
// command and the help and completion that describe it read the same
// definitions.
func rollbackFlagSet(options *rollbackOptions) *flag.FlagSet {
	flags := flag.NewFlagSet("rollback", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	flags.StringVar(&options.Root, "root", "", "generation store root directory")
	flags.StringVar(&options.Generation, "generation", "", "six digit generation id to roll back to; defaults to the registered graph.backup")
	return flags
}

func runRollback(args []string, stdout, stderr io.Writer, rollback graphRollbacker) int {
	var options rollbackOptions
	flags := rollbackFlagSet(&options)
	if parsed, code := parseCommandFlags("rollback", flags, args, stdout, stderr); !parsed {
		return code
	}
	if flags.NArg() != 0 {
		writeCommandError(stderr, "rollback: unexpected arguments: %v", flags.Args())
		return 2
	}
	if options.Root == "" {
		writeCommandError(stderr, "rollback: --root is required")
		return 2
	}

	report, err := rollback(context.Background(), rebuild.RollbackOptions{
		Root:         options.Root,
		Store:        generation.DefaultConfig(),
		GenerationID: options.Generation,
	})
	writeRollbackReport(stdout, report)
	if err != nil {
		writeCommandError(stderr, "rollback: %v", err)
		return 1
	}
	if !report.Passed {
		writeCommandError(stderr, "rollback: report did not pass despite no error")
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
	if integrationTUIIsInteractive(stdout) {
		invariants := "not evaluated"
		if len(report.Invariants.Findings) != 0 {
			invariants = rebuildState(report.Invariants.Passed)
		}
		paint := styleFor(stdout)
		writeKeyValueTable(stdout, "Rollback", []keyValueRow{
			{Key: "Generation", Value: fmt.Sprintf("%s -> %s", orNone(report.From.ID), orNone(report.To.ID))},
			{Key: "Digest expected", Value: orNone(report.Expected)},
			{Key: "Digest observed", Value: orNone(report.Digest)},
			{Key: "Invariants", Value: invariants, ValueStyle: passFailStyle(report.Invariants.Passed, paint)},
			{Key: "Result", Value: rebuildState(report.Passed), ValueStyle: passFailStyle(report.Passed, paint)},
		})
		if len(report.Invariants.Findings) != 0 {
			writeIntegrityFindings(stdout, report.Invariants.Findings)
		}
		return
	}
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

// snapshotOptions carries the flags of `kivgraph snapshot`.
type snapshotOptions struct {
	Root       string
	Generation string
	SnapshotID int64
}

// snapshotFlagSet declares them in one place, so the parser that runs the
// command and the help and completion that describe it read the same
// definitions.
func snapshotFlagSet(options *snapshotOptions) *flag.FlagSet {
	flags := flag.NewFlagSet("snapshot", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	flags.StringVar(&options.Root, "root", "", "generation store root directory")
	flags.StringVar(&options.Generation, "generation", "", "six digit generation id to snapshot; defaults to the registered graph.active")
	flags.Int64Var(&options.SnapshotID, "snapshot-id", 0, "snapshot id stamped on the built HotSnapshot")
	return flags
}

func runSnapshot(args []string, stdout, stderr io.Writer, build snapshotBuilder) int {
	var options snapshotOptions
	flags := snapshotFlagSet(&options)
	if parsed, code := parseCommandFlags("snapshot", flags, args, stdout, stderr); !parsed {
		return code
	}
	if flags.NArg() != 0 {
		writeCommandError(stderr, "snapshot: unexpected arguments: %v", flags.Args())
		return 2
	}
	if options.Root == "" {
		writeCommandError(stderr, "snapshot: --root is required")
		return 2
	}

	_, report, err := build(context.Background(), rebuild.GenerationSnapshotOptions{
		Root:         options.Root,
		Store:        generation.DefaultConfig(),
		GenerationID: options.Generation,
		SnapshotID:   uint64(options.SnapshotID),
	})
	writeSnapshotReport(stdout, report)
	if err != nil {
		writeCommandError(stderr, "snapshot: %v", err)
		return 1
	}
	if !report.Passed {
		writeCommandError(stderr, "snapshot: report did not pass despite no error")
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
	if integrationTUIIsInteractive(stdout) {
		paint := styleFor(stdout)
		writeKeyValueTable(stdout, "Snapshot", []keyValueRow{
			{Key: "State", Value: rebuildState(report.Passed), ValueStyle: passFailStyle(report.Passed, paint)},
			{Key: "Snapshot ID", Value: fmt.Sprintf("%d", report.SnapshotID)},
			{Key: "Version", Value: fmt.Sprintf("%d", report.Version)},
			{Key: "Digest", Value: orNone(report.Digest)},
			{Key: "Repositories", Value: fmt.Sprintf("%d", report.Stats.Repositories)},
			{Key: "Packages", Value: fmt.Sprintf("%d", report.Stats.Packages)},
			{Key: "Files", Value: fmt.Sprintf("%d", report.Stats.Files)},
			{Key: "Symbols", Value: fmt.Sprintf("%d", report.Stats.Symbols)},
			{Key: "Evidence", Value: fmt.Sprintf("%d", report.Stats.Evidence)},
			{Key: "Edges", Value: fmt.Sprintf("%d", report.Stats.Edges)},
			{Key: "Edges outside CSR", Value: fmt.Sprintf("%d", report.Stats.SkippedEdges)},
		})
		return
	}
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

// stopGracePeriod is how long a viewer or a server gets to shut down after
// SIGTERM before it is killed. Both close a snapshot store and a listener,
// which is bounded work; a process still alive after this is stuck.
var stopGracePeriod = 5 * time.Second

// processLister and processSignaller are the two things `stop` needs from the
// operating system, injected so the command can be tested without spawning
// servers and without the test being able to signal anything.
type processLister func() ([]procstat.Process, error)

type processSignaller func(pid int, signal syscall.Signal) error

func signalProcess(pid int, signal syscall.Signal) error {
	process, err := os.FindProcess(pid)
	if err != nil {
		return err
	}
	return process.Signal(signal)
}

// stopOptions carries the flags of `kivgraph stop`.
type stopOptions struct {
	DryRun bool
}

// stopFlagSet declares them in one place, so the parser that runs the
// command and the help and completion that describe it read the same
// definitions.
func stopFlagSet(options *stopOptions) *flag.FlagSet {
	flags := flag.NewFlagSet("stop", flag.ContinueOnError)
	flags.SetOutput(io.Discard)
	flags.BoolVar(&options.DryRun, "dry-run", false, "report what would be stopped and stop nothing")
	return flags
}

// runStop ends every `kivgraph serve` and `kivgraph ui` of this user.
//
// It matches on the invocation, not on the executable: an index in flight is
// left alone, because killing one throws away minutes of analysis, and the
// stop command does not stop itself. Nothing else running on the machine can
// match, since the first argument has to be a kivgraph binary.
// graceful is passed rather than read from the build, so the platform that
// cannot ask a process to exit is exercised on the platform that can. The
// branch would otherwise be reachable only where CI does not run.
func runStop(args []string, stdout, stderr io.Writer, list processLister, signal processSignaller, graceful bool) int {
	var options stopOptions
	flags := stopFlagSet(&options)
	if parsed, code := parseCommandFlags("stop", flags, args, stdout, stderr); !parsed {
		return code
	}
	if flags.NArg() != 0 {
		writeCommandError(stderr, "stop: unexpected arguments: %v", flags.Args())
		return 2
	}

	processes, err := list()
	if err != nil {
		writeCommandError(stderr, "stop: list processes: %v", err)
		return 1
	}
	targets := stoppableProcesses(processes, os.Getpid())
	if len(targets) == 0 {
		writeInfo(stdout, "stop: no kivgraph %s process is running",
			strings.Join(longRunningCommands, ", "))
		return 0
	}
	if options.DryRun {
		for _, target := range targets {
			writeInfo(stdout, "stop.would: pid=%d %s", target.PID, target.Command())
		}
		writeSuccess(stdout, "stop: %d process(es) would be stopped", len(targets))
		return 0
	}

	killed, failed := stopTargets(targets, stdout, stderr, list, signal, graceful)
	if failed != 0 {
		writeResult(stdout, false, "stop: FAIL (%d of %d)", failed, len(targets))
		return 1
	}
	writeSuccess(stdout, "stop: %d process(es) stopped, %d killed", len(targets)-killed, killed)
	return 0
}

// stopTargets ends each target and reports how many had to be killed and how
// many could not be ended at all.
//
// It is shared with `update`, which faces the same processes for a different
// reason: after a bundle is replaced they are still running the image that was
// swapped out. The escalation must be the one `stop` uses -- SIGTERM, a bounded
// wait, and a second identity check before SIGKILL, because a pid freed during
// that wait can already belong to something else -- so there is exactly one
// copy of it.
func stopTargets(
	targets []procstat.Process,
	stdout, stderr io.Writer,
	list processLister,
	signal processSignaller,
	graceful bool,
) (killed, failed int) {
	for _, target := range targets {
		if graceful {
			if err := signal(target.PID, syscall.SIGTERM); err != nil {
				writeCommandError(stderr, "stop: pid=%d: %v", target.PID, err)
				failed++
				continue
			}
			if waitForExit(target.PID, list, stopGracePeriod) {
				writeInfo(stdout, "stop: pid=%d %s", target.PID, target.Command())
				continue
			}
			if !stillRunning(target, list) {
				writeInfo(stdout, "stop: pid=%d %s", target.PID, target.Command())
				continue
			}
		} else if !stillRunning(target, list) {
			// Without a polite stage there is no bounded wait either, so the
			// only thing between listing the process and ending it is this
			// second look -- and it is the one that matters, because a pid
			// freed in between can already belong to something else.
			writeInfo(stdout, "stop: pid=%d %s", target.PID, target.Command())
			continue
		}
		if err := signal(target.PID, syscall.SIGKILL); err != nil {
			writeCommandError(stderr, "stop: pid=%d did not exit and could not be killed: %v", target.PID, err)
			failed++
			continue
		}
		if graceful {
			writeWarning(stdout, "stop.killed: pid=%d did not exit in %s %s", target.PID, stopGracePeriod, target.Command())
		} else {
			writeWarning(stdout, "stop.terminated: pid=%d was ended without being asked, which is all this platform offers %s", target.PID, target.Command())
		}
		killed++
	}
	return killed, failed
}

// longRunningCommands are the invocations that keep a process alive waiting for
// work, which is what `stop` is for. It is a list rather than a rule because the
// rule would be wrong: `index --full` also runs for minutes, and killing one
// mid-publication is not what a reader asking to stop a server means.
var longRunningCommands = []string{"serve", "daemon", "ui"}

// stoppableProcesses selects the long-running commands of this user that are
// not this process.
func stoppableProcesses(processes []procstat.Process, self int) []procstat.Process {
	targets := make([]procstat.Process, 0)
	for _, process := range processes {
		if process.PID == self {
			continue
		}
		program, command := process.Invocation()
		if program != "kivgraph" {
			continue
		}
		if !slices.Contains(longRunningCommands, command) {
			continue
		}
		targets = append(targets, process)
	}
	return targets
}

// waitForExit polls until the process is gone or the grace period ends. The
// process is not a child of this one, so there is nothing to wait on.
func waitForExit(pid int, list processLister, within time.Duration) bool {
	deadline := time.Now().Add(within)
	for {
		if !processRunning(pid, list) {
			return true
		}
		if time.Now().After(deadline) {
			return false
		}
		time.Sleep(50 * time.Millisecond)
	}
}

func processRunning(pid int, list processLister) bool {
	processes, err := list()
	if err != nil {
		return true
	}
	for _, process := range processes {
		if process.PID == pid {
			return true
		}
	}
	return false
}

// stillRunning reports whether the pid is alive and is still the same
// invocation, so a reused pid is never killed.
func stillRunning(target procstat.Process, list processLister) bool {
	processes, err := list()
	if err != nil {
		return false
	}
	for _, process := range processes {
		if process.PID != target.PID {
			continue
		}
		return process.Command() == target.Command()
	}
	return false
}
