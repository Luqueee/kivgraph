package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"testing"
	"time"

	"github.com/Luqueee/kivgraph/internal/config"
	"github.com/Luqueee/kivgraph/internal/eventlog"
	"github.com/Luqueee/kivgraph/internal/facts"
	"github.com/Luqueee/kivgraph/internal/goworkspace"
	"github.com/Luqueee/kivgraph/internal/hotsnapshot"
	"github.com/Luqueee/kivgraph/internal/indexer"
	"github.com/Luqueee/kivgraph/internal/indexing"
	"github.com/Luqueee/kivgraph/internal/logging"
	"github.com/Luqueee/kivgraph/internal/procstat"
	"github.com/Luqueee/kivgraph/internal/rebuild"
	"github.com/Luqueee/kivgraph/internal/storage/generation"
	"github.com/Luqueee/kivgraph/internal/storage/ladybug"
	"github.com/Luqueee/kivgraph/internal/synthetic"
	"github.com/Luqueee/kivgraph/internal/testsupport"
	"github.com/Luqueee/kivgraph/internal/update"
	"github.com/Luqueee/kivgraph/internal/version"
	"github.com/Luqueee/kivgraph/internal/webassets"
	"github.com/Luqueee/kivgraph/internal/workspace"
)

func TestRunVersion(t *testing.T) {
	var stdout, stderr bytes.Buffer

	if got := run([]string{"kivgraph", "version"}, &stdout, &stderr); got != 0 {
		t.Fatalf("run() exit code = %d, want 0", got)
	}
	if got := stdout.String(); got != version.Value+"\n" {
		t.Fatalf("version output = %q, want %q", got, version.Value+"\n")
	}
	if stderr.Len() != 0 {
		t.Fatalf("stderr = %q, want empty", stderr.String())
	}
}

func TestRunVersionJSON(t *testing.T) {
	var stdout, stderr bytes.Buffer

	if got := run([]string{"kivgraph", "version", "--json"}, &stdout, &stderr); got != 0 {
		t.Fatalf("run() exit code = %d, want 0", got)
	}
	if stderr.Len() != 0 {
		t.Fatalf("stderr = %q, want empty", stderr.String())
	}

	var provenance version.Provenance
	if err := json.Unmarshal(stdout.Bytes(), &provenance); err != nil {
		t.Fatalf("version JSON = %q: %v", stdout.String(), err)
	}
	if provenance.Kivgraph != version.Value {
		t.Fatalf("Kivgraph = %q, want %q", provenance.Kivgraph, version.Value)
	}
	if provenance.Go == "" || provenance.Ladybug != ladybug.CoreVersion || provenance.GoLadybug != ladybug.GoBindingVersion {
		t.Fatalf("provenance = %#v", provenance)
	}
	if provenance.Schema != ladybug.CanonicalSchemaVersion || provenance.SnapshotRowFormat != rebuild.SnapshotRowFormatVersion {
		t.Fatalf("schema = %d/%d, want %d/%d", provenance.Schema, provenance.SnapshotRowFormat, ladybug.CanonicalSchemaVersion, rebuild.SnapshotRowFormatVersion)
	}
	if provenance.Grammars.Manifest != "grammars/manifest.json" || len(provenance.Grammars.Versions) != 5 {
		t.Fatalf("grammars = %#v", provenance.Grammars)
	}
}
func TestRunUpdateCheckUsesReleaseRunner(t *testing.T) {
	var stdout, stderr bytes.Buffer
	called := false
	runner := func(ctx context.Context, options update.Options) (update.Result, error) {
		called = true
		if ctx == nil {
			t.Fatal("update runner received nil context")
		}
		if options.CurrentVersion != version.Value {
			t.Fatalf("current version = %q, want %q", options.CurrentVersion, version.Value)
		}
		if !options.CheckOnly {
			t.Fatal("update runner CheckOnly = false, want true")
		}
		return update.Result{
			CurrentVersion:  version.Value,
			LatestVersion:   "0.1.1",
			UpdateAvailable: true,
		}, nil
	}

	if got := runUpdateWithRunner([]string{"--check"}, nil, &stdout, &stderr, runner, noProcesses, nil, true); got != 0 {
		t.Fatalf("runUpdateWithRunner(, true) exit code = %d, stderr=%q", got, stderr.String())
	}
	if !called {
		t.Fatal("update runner was not called")
	}
	if got, want := stdout.String(), "kivgraph update available: "+version.Value+" -> 0.1.1\n"; got != want {
		t.Fatalf("stdout = %q, want %q", got, want)
	}
	if stderr.Len() != 0 {
		t.Fatalf("stderr = %q, want empty", stderr.String())
	}
}

func TestRunUpdateReportsInstalledRelease(t *testing.T) {
	var stdout, stderr bytes.Buffer
	runner := func(_ context.Context, options update.Options) (update.Result, error) {
		if options.CheckOnly {
			t.Fatal("update runner CheckOnly = true, want false")
		}
		return update.Result{
			CurrentVersion:  version.Value,
			LatestVersion:   "0.1.1",
			UpdateAvailable: true,
			Updated:         true,
		}, nil
	}

	if got := runUpdateWithRunner(nil, nil, &stdout, &stderr, runner, noProcesses, nil, true); got != 0 {
		t.Fatalf("runUpdateWithRunner(, true) exit code = %d, stderr=%q", got, stderr.String())
	}
	if got, want := stdout.String(), "kivgraph updated: "+version.Value+" -> 0.1.1\n"; got != want {
		t.Fatalf("stdout = %q, want %q", got, want)
	}
	if stderr.Len() != 0 {
		t.Fatalf("stderr = %q, want empty", stderr.String())
	}
}

func TestRunInitAndDoctorUseConfiguredState(t *testing.T) {
	root := t.TempDir()
	home := filepath.Join(root, "home")
	if err := os.Mkdir(home, 0o700); err != nil {
		t.Fatal(err)
	}
	testsupport.SetHome(t, home)
	configPath := filepath.Join(root, "config.yaml")
	repositoriesPath := filepath.Join(root, "repositories.yaml")

	var initStdout, initStderr bytes.Buffer
	if got := run([]string{
		"kivgraph",
		"init",
		"--config", configPath,
		"--repositories", repositoriesPath,
	}, &initStdout, &initStderr); got != 0 {
		t.Fatalf("init exit code = %d, stderr=%q", got, initStderr.String())
	}
	loaded, err := config.Load(configPath)
	if err != nil {
		t.Fatalf("config.Load() after init: %v", err)
	}
	if len(loaded.Repositories.Repositories) != 0 {
		t.Fatalf("repositories after init = %#v, want empty", loaded.Repositories.Repositories)
	}

	var doctorStdout, doctorStderr bytes.Buffer
	if got := run([]string{"kivgraph", "doctor", "--config", configPath}, &doctorStdout, &doctorStderr); got != 0 {
		t.Fatalf("doctor exit code = %d, stdout=%q stderr=%q", got, doctorStdout.String(), doctorStderr.String())
	}
	if !strings.Contains(doctorStdout.String(), "graph.store: PASS (no published generation)") ||
		!strings.Contains(doctorStdout.String(), "doctor: PASS") {
		t.Fatalf("doctor output = %q", doctorStdout.String())
	}
}

func TestRunUpgradeRequiresPublishedGeneration(t *testing.T) {
	root := t.TempDir()
	home := filepath.Join(root, "home")
	if err := os.Mkdir(home, 0o700); err != nil {
		t.Fatal(err)
	}
	testsupport.SetHome(t, home)
	configPath := filepath.Join(root, "config.yaml")
	repositoriesPath := filepath.Join(root, "repositories.yaml")
	var initStdout, initStderr bytes.Buffer
	if got := run([]string{
		"kivgraph",
		"init",
		"--config", configPath,
		"--repositories", repositoriesPath,
	}, &initStdout, &initStderr); got != 0 {
		t.Fatalf("init exit code = %d, stderr=%q", got, initStderr.String())
	}

	var stdout, stderr bytes.Buffer
	if got := run([]string{"kivgraph", "upgrade", "--config", configPath}, &stdout, &stderr); got != 1 {
		t.Fatalf("upgrade exit code = %d, stdout=%q stderr=%q", got, stdout.String(), stderr.String())
	}
	if !strings.Contains(stderr.String(), "no published generation") {
		t.Fatalf("upgrade stderr = %q, want no-generation failure", stderr.String())
	}
}

// clean is the one command with no undo, so it must say what it would do and
// change nothing until it is told to. On a store with nothing published there
// is nothing to say, and asking to keep the published generation when none
// exists is a mistake worth naming rather than a licence to remove everything.
func TestRunCleanRefusesToGuessOnAnEmptyStore(t *testing.T) {
	root := t.TempDir()
	home := filepath.Join(root, "home")
	if err := os.Mkdir(home, 0o700); err != nil {
		t.Fatal(err)
	}
	testsupport.SetHome(t, home)
	configPath := filepath.Join(root, "config.yaml")
	var initStdout, initStderr bytes.Buffer
	if got := run([]string{
		"kivgraph", "init",
		"--config", configPath,
		"--repositories", filepath.Join(root, "repositories.yaml"),
	}, &initStdout, &initStderr); got != 0 {
		t.Fatalf("init exit code = %d, stderr=%q", got, initStderr.String())
	}

	var stdout, stderr bytes.Buffer
	if got := run([]string{"kivgraph", "clean", "--config", configPath}, &stdout, &stderr); got != 0 {
		t.Fatalf("clean exit code = %d, stderr=%q", got, stderr.String())
	}
	if !strings.Contains(stdout.String(), "nothing to remove") {
		t.Fatalf("clean stdout = %q, want nothing to remove", stdout.String())
	}

	stdout.Reset()
	stderr.Reset()
	if got := run([]string{"kivgraph", "clean", "--config", configPath, "--keep-active", "--yes"}, &stdout, &stderr); got != 1 {
		t.Fatalf("clean --keep-active exit code = %d, stdout=%q stderr=%q", got, stdout.String(), stderr.String())
	}
	if !strings.Contains(stderr.String(), "no generation is published") {
		t.Fatalf("clean stderr = %q, want the missing generation named", stderr.String())
	}

	stdout.Reset()
	stderr.Reset()
	if got := run([]string{"kivgraph", "clean", "--config", configPath, "everything"}, &stdout, &stderr); got != 2 {
		t.Fatalf("clean with an argument exit code = %d, want 2", got)
	}
}

// An MCP client spawns the server itself, so a server that exits because
// nobody ran init first turns installing the integration into a terminal
// session -- and the client only reports that the server failed. serve writes
// the defaults and keeps going. It registers no repository and indexes
// nothing: the graph stays as empty as it was.
// testConfigPath is where a test's flag set writes --config. Each call gets a
// fresh set, so sharing the destination is safe and keeps the calls readable.
var testConfigPath string

func TestRunConfiguredServeCreatesTheConfigurationOnFirstRun(t *testing.T) {
	root := t.TempDir()
	home := filepath.Join(root, "home")
	if err := os.Mkdir(home, 0o700); err != nil {
		t.Fatal(err)
	}
	testsupport.SetHome(t, home)
	configPath := filepath.Join(home, ".config", "kivgraph", "config.yaml")
	if _, err := os.Stat(configPath); !os.IsNotExist(err) {
		t.Fatalf("config error = %v, want a home with no configuration", err)
	}

	var gotStore *hotsnapshot.SnapshotStore
	if err := runConfiguredServe(context.Background(), "serve", nil, serveFlagSet(&testConfigPath), &testConfigPath,
		func(_ context.Context, _ config.Loaded, store *hotsnapshot.SnapshotStore, _ indexing.ProjectIndexer, _ *eventlog.Writer) error {
			gotStore = store
			return nil
		}); err != nil {
		t.Fatalf("runConfiguredServe() error = %v", err)
	}
	if gotStore == nil {
		t.Fatal("serve runner received nil snapshot store")
	}
	if gotStore.Load() != nil {
		t.Fatal("serve published a graph nobody indexed")
	}
	if _, err := os.Stat(configPath); err != nil {
		t.Fatalf("configuration was not created: %v", err)
	}
	registry, err := config.LoadRepositories(filepath.Join(home, ".config", "kivgraph", "repositories.yaml"))
	if err != nil {
		t.Fatalf("LoadRepositories() error = %v", err)
	}
	if len(registry.Repositories) != 0 {
		t.Fatalf("repositories = %#v, want none registered", registry.Repositories)
	}
}

// A configuration that exists and cannot be read is a failure, never something
// to overwrite: the alternative silently replaces whatever the operator wrote.
func TestRunConfiguredServeRefusesAnUnreadableConfiguration(t *testing.T) {
	root := t.TempDir()
	configPath := filepath.Join(root, "config.yaml")
	if err := os.WriteFile(configPath, []byte("version: 1\nnot: valid\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	err := runConfiguredServe(context.Background(), "serve", []string{"--config", configPath}, serveFlagSet(&testConfigPath), &testConfigPath,
		func(context.Context, config.Loaded, *hotsnapshot.SnapshotStore, indexing.ProjectIndexer, *eventlog.Writer) error {
			t.Fatal("serve ran with a configuration it could not read")
			return nil
		})
	if err == nil {
		t.Fatal("runConfiguredServe() accepted an invalid configuration")
	}
	data, readErr := os.ReadFile(configPath)
	if readErr != nil {
		t.Fatal(readErr)
	}
	if !strings.Contains(string(data), "not: valid") {
		t.Fatalf("configuration was rewritten: %q", data)
	}
}

// The published MCP bundle carries no web assets, so ui could only open a
// server whose every page says the bundle is missing. It says so up front
// instead, and never binds a port to serve nothing.
func TestRunConfiguredUIRefusesWithoutTheWebBundle(t *testing.T) {
	root := t.TempDir()
	testsupport.SetHome(t, filepath.Join(root, "home"))
	err := runConfiguredUI(context.Background(), []string{"--config", filepath.Join(root, "config.yaml")},
		func(context.Context, string, http.Handler) error {
			t.Fatal("ui opened a server with no viewer to serve")
			return nil
		}, false)
	if err == nil {
		t.Fatal("runConfiguredUI() served a viewer this binary does not carry")
	}
	for _, want := range []string{"no web bundle", "--mcp-only"} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("error = %q, want it to mention %q", err, want)
		}
	}
}

func TestRunDoctorRejectsInaccessibleRepository(t *testing.T) {
	root := t.TempDir()
	home := filepath.Join(root, "home")
	if err := os.Mkdir(home, 0o700); err != nil {
		t.Fatal(err)
	}
	testsupport.SetHome(t, home)
	configPath := filepath.Join(root, "config.yaml")
	repositoriesPath := filepath.Join(root, "repositories.yaml")
	var initStdout, initStderr bytes.Buffer
	if got := run([]string{
		"kivgraph",
		"init",
		"--config", configPath,
		"--repositories", repositoriesPath,
		"--repository", "missing=" + filepath.Join(root, "does-not-exist"),
	}, &initStdout, &initStderr); got != 0 {
		t.Fatalf("init exit code = %d, stderr=%q", got, initStderr.String())
	}

	var stdout, stderr bytes.Buffer
	if got := run([]string{"kivgraph", "doctor", "--config", configPath}, &stdout, &stderr); got != 1 {
		t.Fatalf("doctor exit code = %d, stdout=%q stderr=%q", got, stdout.String(), stderr.String())
	}
	if !strings.Contains(stdout.String(), "repositories: FAIL") || !strings.Contains(stdout.String(), "doctor: FAIL") {
		t.Fatalf("doctor output = %q", stdout.String())
	}
}

func TestRunServeStopsMCPOnContextCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	started := make(chan struct{})
	stopped := make(chan struct{})
	result := make(chan error, 1)
	go func() {
		result <- runServe(ctx, func(ctx context.Context) error {
			close(started)
			<-ctx.Done()
			close(stopped)
			return ctx.Err()
		})
	}()

	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("MCP runner did not start")
	}
	cancel()

	select {
	case err := <-result:
		if err != nil {
			t.Fatalf("runServe() error = %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("runServe() did not return after cancellation")
	}
	select {
	case <-stopped:
	default:
		t.Fatal("runServe() returned before the MCP runner stopped")
	}
}
func TestRunConfiguredServeProvidesProjectIndexer(t *testing.T) {
	root := t.TempDir()
	home := filepath.Join(root, "home")
	if err := os.Mkdir(home, 0o700); err != nil {
		t.Fatal(err)
	}
	testsupport.SetHome(t, home)
	configPath := filepath.Join(root, "config.yaml")
	repositoriesPath := filepath.Join(root, "repositories.yaml")
	if _, err := config.Initialize(config.InitOptions{
		ConfigPath:       configPath,
		RepositoriesPath: repositoriesPath,
	}); err != nil {
		t.Fatalf("config.Initialize() error = %v", err)
	}

	var gotStore *hotsnapshot.SnapshotStore
	var gotIndexer indexing.ProjectIndexer
	err := runConfiguredServe(context.Background(), "serve", []string{"--config", configPath}, serveFlagSet(&testConfigPath), &testConfigPath,
		func(_ context.Context, _ config.Loaded, store *hotsnapshot.SnapshotStore, indexer indexing.ProjectIndexer, _ *eventlog.Writer) error {
			gotStore = store
			gotIndexer = indexer
			return nil
		})
	if err != nil {
		t.Fatalf("runConfiguredServe() error = %v", err)
	}
	if gotStore == nil {
		t.Fatal("serve runner received nil snapshot store")
	}
	if gotIndexer == nil {
		t.Fatal("serve runner received nil project indexer")
	}
}

// A long-running command owns its follower. When the command returns, nothing
// may still be reading the generation store or writing the state directory:
// the caller usually owns those paths and is about to delete them, and a
// goroutine that outlives its starter turns that into a failure nobody can
// place.
func TestFollowPublishedGenerationStopsWithItsCaller(t *testing.T) {
	root := t.TempDir()
	testsupport.SetHome(t, filepath.Join(root, "home"))
	configPath := filepath.Join(root, "config.yaml")
	if _, err := config.Initialize(config.InitOptions{
		ConfigPath:       configPath,
		RepositoriesPath: filepath.Join(root, "repositories.yaml"),
	}); err != nil {
		t.Fatalf("config.Initialize() error = %v", err)
	}
	loaded, err := config.Load(configPath)
	if err != nil {
		t.Fatalf("config.Load() error = %v", err)
	}
	store := hotsnapshot.NewSnapshotStore(nil)
	defer store.Close()

	// A CURRENT pointer that cannot be read makes the follower report on
	// every tick without publishing anything. That report is the signal: a
	// follower still running after stop keeps producing it, and the test
	// stays clear of the production space policy, which depends on how full
	// the machine happens to be.
	stateRoot := filepath.Dir(loaded.Config.Storage.DatabasePath)
	if err := os.MkdirAll(filepath.Join(stateRoot, "CURRENT"), 0o700); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}

	var ticks atomic.Int64
	stop := followPublishedGeneration(context.Background(), loaded, store, "test", indexing.FollowOptions{
		Interval:  time.Millisecond,
		OnPublish: func(uint64) { ticks.Add(1) },
		OnError:   func(error) { ticks.Add(1) },
	})
	deadline := time.Now().Add(5 * time.Second)
	for ticks.Load() == 0 && time.Now().Before(deadline) {
		time.Sleep(2 * time.Millisecond)
	}
	if ticks.Load() == 0 {
		t.Fatal("the follower never reported: the test cannot observe whether it stopped")
	}
	stop()

	settled := ticks.Load()
	time.Sleep(50 * time.Millisecond)
	if moved := ticks.Load(); moved != settled {
		t.Fatalf("follower reported %d more times after it was stopped", moved-settled)
	}
}

func TestRunConfiguredUILoadsPublishedStore(t *testing.T) {
	root := t.TempDir()
	testsupport.SetHome(t, filepath.Join(root, "home"))
	configPath := filepath.Join(root, "config.yaml")
	repositoriesPath := filepath.Join(root, "repositories.yaml")
	if _, err := config.Initialize(config.InitOptions{
		ConfigPath:       configPath,
		RepositoriesPath: repositoriesPath,
	}); err != nil {
		t.Fatalf("config.Initialize() error = %v", err)
	}

	called := false
	err := runConfiguredUI(context.Background(), []string{
		"--config", configPath,
		"--addr", "127.0.0.1:0",
	}, func(_ context.Context, address string, handler http.Handler) error {
		called = true
		if address != "127.0.0.1:0" {
			t.Fatalf("address = %q, want override", address)
		}
		request := httptest.NewRequest(http.MethodGet, "/api/v1/meta", nil)
		response := httptest.NewRecorder()
		handler.ServeHTTP(response, request)
		if response.Code != http.StatusServiceUnavailable {
			t.Fatalf("meta status = %d, want %d", response.Code, http.StatusServiceUnavailable)
		}
		return nil
	}, true)
	if err != nil {
		t.Fatalf("runConfiguredUI() error = %v", err)
	}
	if !called {
		t.Fatal("web runner was not called")
	}
}

// TestRunConfiguredUIListensOnEveryInterfaceByDefault pins the bind a fresh
// configuration gets. The viewer is usually wanted from another machine -- the
// graph is indexed where the repositories are -- and a loopback default made
// every remote use start with an edit. What guards it is the warning, which
// TestUIWarnsWhenTheBindIsReachable keeps.
func TestRunConfiguredUIListensOnEveryInterfaceByDefault(t *testing.T) {
	root := t.TempDir()
	testsupport.SetHome(t, filepath.Join(root, "home"))
	configPath := filepath.Join(root, "config.yaml")
	repositoriesPath := filepath.Join(root, "repositories.yaml")
	if _, err := config.Initialize(config.InitOptions{
		ConfigPath:       configPath,
		RepositoriesPath: repositoriesPath,
	}); err != nil {
		t.Fatalf("config.Initialize() error = %v", err)
	}

	err := runConfiguredUI(context.Background(), []string{"--config", configPath}, func(_ context.Context, address string, _ http.Handler) error {
		if address != "0.0.0.0:7777" {
			t.Fatalf("address = %q, want 0.0.0.0:7777", address)
		}
		return nil
	}, true)
	if err != nil {
		t.Fatalf("runConfiguredUI() error = %v", err)
	}
}

// TestRunWithoutCommandWritesTheCommandTable is the contract of a bare
// invocation: it asks what the program does, so it is answered on stdout with
// the same table `--help` writes, and it is not a failure.
func TestRunWithoutCommandWritesTheCommandTable(t *testing.T) {
	var stdout, stderr bytes.Buffer

	if got := run([]string{"kivgraph"}, &stdout, &stderr); got != 0 {
		t.Fatalf("run() exit code = %d, want 0", got)
	}
	if stderr.Len() != 0 {
		t.Fatalf("stderr = %q, want empty", stderr.String())
	}
	var help bytes.Buffer
	if got := run([]string{"kivgraph", "--help"}, &help, &bytes.Buffer{}); got != 0 {
		t.Fatalf("run(--help) exit code = %d, want 0", got)
	}
	if stdout.String() != help.String() {
		t.Fatalf("bare invocation wrote %q, want the same table as --help", stdout.String())
	}
	for _, want := range []string{"doctor repositories", "index --full", "Run \"kivgraph <command> --help\""} {
		if !strings.Contains(stdout.String(), want) {
			t.Fatalf("stdout = %q, want it to contain %q", stdout.String(), want)
		}
	}
}

func TestRunNamesAnUnknownCommand(t *testing.T) {
	var stdout, stderr bytes.Buffer

	if got := run([]string{"kivgraph", "inedx"}, &stdout, &stderr); got != 2 {
		t.Fatalf("run() exit code = %d, want 2", got)
	}
	if !strings.Contains(stderr.String(), `unknown command "inedx"`) {
		t.Fatalf("stderr = %q, want the unknown command named", stderr.String())
	}
}

// TestRunHelpIsNotAnError keeps --help on stdout with exit 0: a request
// answered, not a mistake reported.
func TestRunHelpIsNotAnError(t *testing.T) {
	for _, argument := range []string{"--help", "-h", "help"} {
		var stdout, stderr bytes.Buffer
		if got := run([]string{"kivgraph", argument}, &stdout, &stderr); got != 0 {
			t.Fatalf("run(%s) exit code = %d, want 0", argument, got)
		}
		if stderr.Len() != 0 {
			t.Fatalf("run(%s) stderr = %q, want empty", argument, stderr.String())
		}
		for _, want := range []string{"Usage", "Getting started", "index --full", "doctor storage --database PATH"} {
			if !strings.Contains(stdout.String(), want) {
				t.Fatalf("run(%s) stdout = %q, want it to contain %q", argument, stdout.String(), want)
			}
		}
	}
}

// TestCommandHelpListsFlagsOnStdout covers the promise the help footer makes.
func TestCommandHelpListsFlagsOnStdout(t *testing.T) {
	var stdout, stderr bytes.Buffer

	if got := run([]string{"kivgraph", "rollback", "--help"}, &stdout, &stderr); got != 0 {
		t.Fatalf("run() exit code = %d, want 0", got)
	}
	if stderr.Len() != 0 {
		t.Fatalf("stderr = %q, want empty", stderr.String())
	}
	for _, want := range []string{"kivgraph rollback", "Flags", "--root", "--generation"} {
		if !strings.Contains(stdout.String(), want) {
			t.Fatalf("stdout = %q, want it to contain %q", stdout.String(), want)
		}
	}
}

// TestUnknownFlagNamesItselfAndExitsTwo keeps a real mistake distinguishable
// from a help request.
func TestUnknownFlagNamesItselfAndExitsTwo(t *testing.T) {
	var stdout, stderr bytes.Buffer

	if got := run([]string{"kivgraph", "doctor", "--nope"}, &stdout, &stderr); got != 2 {
		t.Fatalf("run() exit code = %d, want 2", got)
	}
	if stdout.Len() != 0 {
		t.Fatalf("stdout = %q, want empty", stdout.String())
	}
	if !strings.Contains(stderr.String(), "doctor: flag provided but not defined: -nope") {
		t.Fatalf("stderr = %q, want the rejected flag named", stderr.String())
	}
}

// TestCLIErrorWriterEmitsJSONToStderr keeps a failure recorded at ERROR with
// its own text as the message: a reader greps the message, and a record whose
// message is always "command stderr" hides the one line that matters.
func TestCLIErrorWriterEmitsJSONToStderr(t *testing.T) {
	var stdout, stderr bytes.Buffer
	logger := logging.New(&stderr)

	if got := run([]string{"kivgraph", "inedx"}, &stdout, logging.NewCommandWriter(logger)); got != 2 {
		t.Fatalf("run() exit code = %d, want 2", got)
	}
	if stdout.Len() != 0 {
		t.Fatalf("stdout = %q, want empty", stdout.String())
	}

	var record map[string]any
	if err := json.Unmarshal([]byte(strings.SplitN(stderr.String(), "\n", 2)[0]), &record); err != nil {
		t.Fatalf("stderr = %q, want JSON records: %v", stderr.String(), err)
	}
	if record["level"] != "ERROR" {
		t.Fatalf("record = %#v, want the failure at ERROR", record)
	}
	message, ok := record["msg"].(string)
	if !ok || !strings.Contains(message, `unknown command "inedx"`) {
		t.Fatalf("record msg = %#v, want the usage error itself", record["msg"])
	}
}

// TestCLIProgressIsNotAnError keeps a pass that succeeds from reporting every
// unit of work it finished at ERROR. The level is the only thing a program
// reading stderr can filter on, and it used to say ERROR for a graph that
// published cleanly.
func TestCLIProgressIsNotAnError(t *testing.T) {
	var stderr bytes.Buffer
	writer := logging.NewCommandWriter(logging.New(&stderr))
	writeInfo(writer, "[  1.2s] rebuild publish")

	var record map[string]any
	if err := json.Unmarshal([]byte(strings.TrimSpace(stderr.String())), &record); err != nil {
		t.Fatalf("stderr = %q, want a JSON record: %v", stderr.String(), err)
	}
	if record["level"] != "INFO" {
		t.Fatalf("record = %#v, want progress at INFO", record)
	}
	if message, _ := record["msg"].(string); !strings.Contains(message, "rebuild publish") {
		t.Fatalf("record msg = %#v, want the progress line itself", record["msg"])
	}
}

func TestRunDoctorStorageReportsEveryHealthyCheck(t *testing.T) {
	var stdout, stderr bytes.Buffer
	checks := make([]ladybug.DiagnosticCheck, 0, 10)
	for _, name := range []string{"location", "size", "permissions", "lock", "open", "version", "schema", "transactions", "counts", "integrity"} {
		checks = append(checks, ladybug.DiagnosticCheck{Name: name, Status: ladybug.DiagnosticPass, Detail: name + " ok"})
	}
	diagnose := func(_ context.Context, path string) (ladybug.StorageDiagnosis, error) {
		if path != "/tmp/graph.db" {
			t.Fatalf("diagnostic path = %q", path)
		}
		return ladybug.StorageDiagnosis{Path: path, Checks: checks, Healthy: true}, nil
	}

	code := runWithStorageDiagnoser([]string{"kivgraph", "doctor", "storage", "--database", "/tmp/graph.db"}, &stdout, &stderr, diagnose)
	if code != 0 {
		t.Fatalf("exit code = %d, want 0", code)
	}
	if stderr.Len() != 0 {
		t.Fatalf("stderr = %q", stderr.String())
	}
	for _, check := range checks {
		if !strings.Contains(stdout.String(), "[PASS] "+check.Name+": "+check.Detail) {
			t.Fatalf("stdout missing %s: %q", check.Name, stdout.String())
		}
	}
}

func TestRunDoctorStorageReturnsFailureForUnhealthyDatabase(t *testing.T) {
	var stdout, stderr bytes.Buffer
	diagnose := func(context.Context, string) (ladybug.StorageDiagnosis, error) {
		return ladybug.StorageDiagnosis{
			Path:   "/tmp/graph.db",
			Checks: []ladybug.DiagnosticCheck{{Name: "integrity", Status: ladybug.DiagnosticFail, Detail: "1 violation"}},
		}, nil
	}

	code := runWithStorageDiagnoser([]string{"kivgraph", "doctor", "storage", "--database=/tmp/graph.db"}, &stdout, &stderr, diagnose)
	if code != 1 {
		t.Fatalf("exit code = %d, want 1", code)
	}
	if !strings.Contains(stdout.String(), "storage doctor: FAIL") || !strings.Contains(stdout.String(), "[FAIL] integrity: 1 violation") {
		t.Fatalf("stdout = %q", stdout.String())
	}
	if stderr.Len() != 0 {
		t.Fatalf("stderr = %q", stderr.String())
	}
}

func TestRunDoctorStorageRequiresDatabasePath(t *testing.T) {
	var stdout, stderr bytes.Buffer
	called := false
	diagnose := func(context.Context, string) (ladybug.StorageDiagnosis, error) {
		called = true
		return ladybug.StorageDiagnosis{}, nil
	}

	if code := runWithStorageDiagnoser([]string{"kivgraph", "doctor", "storage"}, &stdout, &stderr, diagnose); code != 2 {
		t.Fatalf("exit code = %d, want 2", code)
	}
	if called {
		t.Fatal("diagnoser was called")
	}
	if !strings.Contains(stderr.String(), "--database is required") {
		t.Fatalf("stderr = %q", stderr.String())
	}
}

func TestRunDoctorGraphReportsCleanInvariants(t *testing.T) {
	var stdout, stderr bytes.Buffer
	report := ladybug.CanonicalIntegrityReport{
		Findings: []ladybug.IntegrityFinding{
			{Rule: ladybug.RuleExactEdgeWithoutSource, Passed: true},
			{Rule: ladybug.RuleDuplicateStableKey, Passed: true},
		},
		Passed: true,
	}
	verify := func(_ context.Context, path string) (ladybug.CanonicalIntegrityReport, error) {
		if path != "/tmp/graph.db" {
			t.Fatalf("verify path = %q, want /tmp/graph.db", path)
		}
		return report, nil
	}

	code := runWithGraphVerifier([]string{"kivgraph", "doctor", "graph", "--database", "/tmp/graph.db"}, &stdout, &stderr, ladybug.DiagnoseStorage, rebuild.Run, verify)
	if code != 0 {
		t.Fatalf("exit code = %d, want 0; stdout=%q stderr=%q", code, stdout.String(), stderr.String())
	}
	if stderr.Len() != 0 {
		t.Fatalf("stderr = %q, want empty", stderr.String())
	}
	if !strings.Contains(stdout.String(), "graph doctor: PASS") {
		t.Fatalf("stdout = %q, want overall PASS", stdout.String())
	}
	if !strings.Contains(stdout.String(), "[PASS] exact_edge_without_source: 0 violation(s)") {
		t.Fatalf("stdout missing passing rule: %q", stdout.String())
	}
	if !strings.Contains(stdout.String(), "[PASS] duplicate_stable_key: 0 violation(s)") {
		t.Fatalf("stdout missing passing rule: %q", stdout.String())
	}
}

func TestRunDoctorGraphReportsViolationsAndSamples(t *testing.T) {
	var stdout, stderr bytes.Buffer
	report := ladybug.CanonicalIntegrityReport{
		Findings: []ladybug.IntegrityFinding{
			{Rule: ladybug.RuleExactEdgeWithoutSource, Passed: true},
			{
				Rule:       ladybug.RuleDuplicateStableKey,
				Violations: 2,
				Passed:     false,
				Samples: []ladybug.IntegrityViolation{
					{Rule: ladybug.RuleDuplicateStableKey, Table: "Package", Key: "pkg:acme/widgets", Detail: "also used by table File"},
				},
			},
		},
		Passed: false,
	}
	verify := func(context.Context, string) (ladybug.CanonicalIntegrityReport, error) {
		return report, nil
	}

	code := runWithGraphVerifier([]string{"kivgraph", "doctor", "graph", "--database", "/tmp/graph.db"}, &stdout, &stderr, ladybug.DiagnoseStorage, rebuild.Run, verify)
	if code != 1 {
		t.Fatalf("exit code = %d, want 1", code)
	}
	if !strings.Contains(stdout.String(), "graph doctor: FAIL") {
		t.Fatalf("stdout missing overall FAIL: %q", stdout.String())
	}
	if !strings.Contains(stdout.String(), "[FAIL] duplicate_stable_key: 2 violation(s)") {
		t.Fatalf("stdout missing failed rule: %q", stdout.String())
	}
	if !strings.Contains(stdout.String(), "Package pkg:acme/widgets: also used by table File") {
		t.Fatalf("stdout missing sample: %q", stdout.String())
	}
	if strings.Contains(stdout.String(), "[FAIL] exact_edge_without_source") {
		t.Fatalf("stdout should not report the passing rule as failed: %q", stdout.String())
	}
}

// TestRunDoctorGraphRequiresDatabasePath is the (d) contract: doctor graph
// without --database exits with the same usage failure code (2) that
// doctor storage already uses for a missing required flag.
func TestRunDoctorGraphRequiresDatabasePath(t *testing.T) {
	var stdout, stderr bytes.Buffer
	called := false
	verify := func(context.Context, string) (ladybug.CanonicalIntegrityReport, error) {
		called = true
		return ladybug.CanonicalIntegrityReport{}, nil
	}

	code := runWithGraphVerifier([]string{"kivgraph", "doctor", "graph"}, &stdout, &stderr, ladybug.DiagnoseStorage, rebuild.Run, verify)
	if code != 2 {
		t.Fatalf("exit code = %d, want 2", code)
	}
	if called {
		t.Fatal("verifier was called")
	}
	if !strings.Contains(stderr.String(), "--database is required") {
		t.Fatalf("stderr = %q", stderr.String())
	}
}

func TestRunGenerateGraph(t *testing.T) {
	outputDir := filepath.Join(t.TempDir(), "synthetic")
	var stdout, stderr bytes.Buffer
	args := []string{
		"kivgraph", "benchmark", "generate-graph",
		"--repositories", "2",
		"--files", "10",
		"--symbols", "20",
		"--edges", "100",
		"--seed", "42",
		"--output", outputDir,
	}
	if got := run(args, &stdout, &stderr); got != 0 {
		t.Fatalf("run() exit code = %d, stderr = %q", got, stderr.String())
	}
	if !strings.Contains(stdout.String(), "generated 2 repositories, 10 files, 20 symbols, 100 edges") {
		t.Fatalf("stdout = %q, want generation summary", stdout.String())
	}
	data, err := os.ReadFile(filepath.Join(outputDir, "manifest.json"))
	if err != nil {
		t.Fatalf("read manifest: %v", err)
	}
	var manifest synthetic.Manifest
	if err := json.Unmarshal(data, &manifest); err != nil {
		t.Fatalf("decode manifest: %v", err)
	}
	if manifest.Seed != 42 || manifest.Edges != 100 {
		t.Fatalf("manifest = %#v, want seed 42 and 100 edges", manifest)
	}
}

func TestRunGenerateGraphRejectsInvalidSize(t *testing.T) {
	var stdout, stderr bytes.Buffer
	args := []string{"kivgraph", "benchmark", "generate-graph", "--files", "2", "--symbols", "9", "--edges", "10"}
	if got := run(args, &stdout, &stderr); got != 1 {
		t.Fatalf("run() exit code = %d, want 1", got)
	}
	if !strings.Contains(stderr.String(), "edges must be at least") {
		t.Fatalf("stderr = %q, want validation error", stderr.String())
	}
}

func writeFactsFile(t *testing.T, set facts.Set) string {
	t.Helper()
	data, err := json.Marshal(set)
	if err != nil {
		t.Fatalf("marshal facts: %v", err)
	}
	path := filepath.Join(t.TempDir(), "facts.json")
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatalf("write facts file: %v", err)
	}
	return path
}

func TestRunRebuildPrintsAllStagesOnSuccess(t *testing.T) {
	factsPath := writeFactsFile(t, facts.Set{})
	var stdout, stderr bytes.Buffer

	rebuilder := func(_ context.Context, options rebuild.Options) (rebuild.Report, error) {
		if options.GenerationID != "000123" {
			t.Fatalf("generation id = %q, want 000123", options.GenerationID)
		}
		if options.Root != "/tmp/kivgraph-graph" {
			t.Fatalf("root = %q, want /tmp/kivgraph-graph", options.Root)
		}
		if options.ResolverVersion != "resolver-v1" {
			t.Fatalf("resolver version = %q, want resolver-v1", options.ResolverVersion)
		}
		if options.SnapshotID != 7 {
			t.Fatalf("snapshot id = %d, want 7", options.SnapshotID)
		}
		return rebuild.Report{
			GenerationID: options.GenerationID,
			Stages: []rebuild.Stage{
				{Name: rebuild.StageFacts, Passed: true, DurationMS: 1},
				{Name: rebuild.StageStaging, Passed: true, DurationMS: 2},
				{Name: rebuild.StageGraphNext, Passed: true, DurationMS: 3},
				{Name: rebuild.StageBulkLoad, Passed: true, DurationMS: 4},
				{Name: rebuild.StageIntegrity, Passed: true, DurationMS: 5},
				{Name: rebuild.StageSnapshot, Passed: true, DurationMS: 6},
				{Name: rebuild.StageProbes, Passed: true, DurationMS: 7},
				{Name: rebuild.StagePublish, Passed: true, DurationMS: 8},
			},
			SnapshotDigest: "deadbeef",
			Publication: generation.Publication{
				Generation: generation.Generation{ID: "000123", Path: "/tmp/kivgraph-graph/generations/000123"},
			},
			Passed: true,
		}, nil
	}

	code := runWithGraphRebuilder([]string{
		"kivgraph", "rebuild",
		"--facts", factsPath,
		"--root", "/tmp/kivgraph-graph",
		"--generation", "000123",
		"--resolver-version", "resolver-v1",
		"--snapshot-id", "7",
	}, &stdout, &stderr, ladybug.DiagnoseStorage, rebuilder)

	if code != 0 {
		t.Fatalf("run() exit code = %d, stderr = %q", code, stderr.String())
	}
	if stderr.Len() != 0 {
		t.Fatalf("stderr = %q, want empty", stderr.String())
	}
	for _, stage := range []rebuild.StageName{
		rebuild.StageFacts, rebuild.StageStaging, rebuild.StageGraphNext, rebuild.StageBulkLoad,
		rebuild.StageIntegrity, rebuild.StageSnapshot, rebuild.StageProbes, rebuild.StagePublish,
	} {
		if !strings.Contains(stdout.String(), string(stage)) {
			t.Fatalf("stdout missing stage %q: %q", stage, stdout.String())
		}
	}
	if !strings.Contains(stdout.String(), "000123") {
		t.Fatalf("stdout missing published generation id: %q", stdout.String())
	}
}

func TestRunRebuildReturnsFailureAndExplainsStage(t *testing.T) {
	factsPath := writeFactsFile(t, facts.Set{})
	var stdout, stderr bytes.Buffer

	rebuilder := func(context.Context, rebuild.Options) (rebuild.Report, error) {
		return rebuild.Report{
			Stages: []rebuild.Stage{
				{Name: rebuild.StageFacts, Passed: true, DurationMS: 1},
				{Name: rebuild.StageStaging, Passed: true, DurationMS: 2},
				{Name: rebuild.StageGraphNext, Passed: false, Detail: "unresolved package for symbol", DurationMS: 3},
			},
			Passed: false,
		}, nil
	}

	code := runWithGraphRebuilder([]string{
		"kivgraph", "rebuild",
		"--facts", factsPath,
		"--root", "/tmp/kivgraph-graph",
		"--generation", "000124",
		"--resolver-version", "resolver-v1",
	}, &stdout, &stderr, ladybug.DiagnoseStorage, rebuilder)

	if code != 1 {
		t.Fatalf("exit code = %d, want 1", code)
	}
	if !strings.Contains(stderr.String(), string(rebuild.StageGraphNext)) || !strings.Contains(stderr.String(), "unresolved package for symbol") {
		t.Fatalf("stderr = %q, want failing stage explanation", stderr.String())
	}
}

func TestRunRebuildReportsIntegrityDiscrepanciesAndFailedProbes(t *testing.T) {
	factsPath := writeFactsFile(t, facts.Set{})
	var stdout, stderr bytes.Buffer

	rebuilder := func(context.Context, rebuild.Options) (rebuild.Report, error) {
		return rebuild.Report{
			Stages: []rebuild.Stage{
				{Name: rebuild.StageIntegrity, Passed: false, Detail: "1 mismatch", DurationMS: 1},
			},
			Integrity: []rebuild.IntegrityCheck{
				{Table: "Symbol", Expected: 100, Observed: 99, Passed: false},
				{Table: "File", Expected: 10, Observed: 10, Passed: true},
			},
			Invariants: ladybug.CanonicalIntegrityReport{
				Findings: []ladybug.IntegrityFinding{
					{Rule: ladybug.RuleExactEdgeWithoutSource, Passed: true},
					{
						Rule:       ladybug.RuleDuplicateStableKey,
						Violations: 1,
						Passed:     false,
						Samples: []ladybug.IntegrityViolation{
							{Rule: ladybug.RuleDuplicateStableKey, Table: "Package", Key: "pkg:acme/widgets", Detail: "also used by table File"},
						},
					},
				},
				Passed: false,
			},
			Probes: []ladybug.CanonicalProbeResult{
				{Probe: "calls-direct", Rows: 0, Passed: false, Detail: "no rows"},
				{Probe: "imports-symbol", Rows: 5, Passed: true},
			},
			Passed: false,
		}, nil
	}

	code := runWithGraphRebuilder([]string{
		"kivgraph", "rebuild",
		"--facts", factsPath,
		"--root", "/tmp/kivgraph-graph",
		"--generation", "000127",
		"--resolver-version", "resolver-v1",
	}, &stdout, &stderr, ladybug.DiagnoseStorage, rebuilder)

	if code != 1 {
		t.Fatalf("exit code = %d, want 1", code)
	}
	if !strings.Contains(stdout.String(), "integrity Symbol: expected 100, observed 99") {
		t.Fatalf("stdout missing integrity discrepancy: %q", stdout.String())
	}
	if strings.Contains(stdout.String(), "integrity File") {
		t.Fatalf("stdout should not report a passing integrity check: %q", stdout.String())
	}
	if !strings.Contains(stdout.String(), "calls-direct") || !strings.Contains(stdout.String(), "no rows") {
		t.Fatalf("stdout missing failed probe: %q", stdout.String())
	}
	if strings.Contains(stdout.String(), "imports-symbol") {
		t.Fatalf("stdout should not report a passing probe: %q", stdout.String())
	}
	if !strings.Contains(stdout.String(), "invariant duplicate_stable_key: 1 violation(s)") {
		t.Fatalf("stdout missing invariant violation: %q", stdout.String())
	}
	if !strings.Contains(stdout.String(), "Package pkg:acme/widgets: also used by table File") {
		t.Fatalf("stdout missing invariant sample: %q", stdout.String())
	}
	if strings.Contains(stdout.String(), "invariant exact_edge_without_source") {
		t.Fatalf("stdout should not report a passing invariant: %q", stdout.String())
	}
}

func TestRunRebuildRequiresFacts(t *testing.T) {
	var stdout, stderr bytes.Buffer
	called := false
	rebuilder := func(context.Context, rebuild.Options) (rebuild.Report, error) {
		called = true
		return rebuild.Report{}, nil
	}

	code := runWithGraphRebuilder([]string{
		"kivgraph", "rebuild",
		"--root", "/tmp/kivgraph-graph",
		"--generation", "000125",
		"--resolver-version", "resolver-v1",
	}, &stdout, &stderr, ladybug.DiagnoseStorage, rebuilder)

	if code != 2 {
		t.Fatalf("exit code = %d, want 2", code)
	}
	if called {
		t.Fatal("rebuilder was called")
	}
	if !strings.Contains(stderr.String(), "--facts is required") {
		t.Fatalf("stderr = %q, want missing flag message", stderr.String())
	}
}

func TestRunRebuildRejectsInvalidFactsJSON(t *testing.T) {
	factsPath := filepath.Join(t.TempDir(), "facts.json")
	if err := os.WriteFile(factsPath, []byte("not json"), 0o644); err != nil {
		t.Fatalf("write facts file: %v", err)
	}
	var stdout, stderr bytes.Buffer
	called := false
	rebuilder := func(context.Context, rebuild.Options) (rebuild.Report, error) {
		called = true
		return rebuild.Report{}, nil
	}

	code := runWithGraphRebuilder([]string{
		"kivgraph", "rebuild",
		"--facts", factsPath,
		"--root", "/tmp/kivgraph-graph",
		"--generation", "000126",
		"--resolver-version", "resolver-v1",
	}, &stdout, &stderr, ladybug.DiagnoseStorage, rebuilder)

	if code != 1 {
		t.Fatalf("exit code = %d, want 1", code)
	}
	if called {
		t.Fatal("rebuilder was called")
	}
	if !strings.Contains(stderr.String(), "rebuild: decode facts") {
		t.Fatalf("stderr = %q, want decode error", stderr.String())
	}
}

func TestRunGraphStatusReportsRolesAndRetainedGenerations(t *testing.T) {
	var stdout, stderr bytes.Buffer
	roles := func(_ context.Context, options rebuild.LayoutOptions) (rebuild.Layout, error) {
		if options.Root != "/tmp/kivgraph-graph" {
			t.Fatalf("root = %q, want /tmp/kivgraph-graph", options.Root)
		}
		return rebuild.Layout{
			Active:    generation.Generation{ID: "000002", Path: "/tmp/kivgraph-graph/generations/000002"},
			Backup:    generation.Generation{ID: "000001", Path: "/tmp/kivgraph-graph/generations/000001"},
			HasBackup: true,
			NextID:    "000003",
			Retained:  []string{"000001", "000002"},
		}, nil
	}

	code := runWithGraphRoles([]string{
		"kivgraph", "graph", "status", "--root", "/tmp/kivgraph-graph",
	}, &stdout, &stderr, ladybug.DiagnoseStorage, rebuild.Run, ladybug.VerifyCanonicalIntegrity, roles)
	if code != 0 {
		t.Fatalf("exit code = %d, stderr = %q", code, stderr.String())
	}
	if stderr.Len() != 0 {
		t.Fatalf("stderr = %q, want empty", stderr.String())
	}
	out := stdout.String()
	if !strings.Contains(out, "graph.active: 000002") {
		t.Fatalf("stdout missing active role: %q", out)
	}
	if !strings.Contains(out, "graph.backup: 000001") {
		t.Fatalf("stdout missing backup role: %q", out)
	}
	if !strings.Contains(out, "graph.next:") || !strings.Contains(out, "000003") {
		t.Fatalf("stdout missing next role: %q", out)
	}
	if !strings.Contains(out, "retained: 000001, 000002") {
		t.Fatalf("stdout missing retained generations: %q", out)
	}
}

// TestRunGraphStatusReportsEmptyLayoutWithoutFailing is the point 5
// contract: a store with no active generation is reported legibly, exit 0,
// not treated as a program error.
func TestRunGraphStatusReportsEmptyLayoutWithoutFailing(t *testing.T) {
	var stdout, stderr bytes.Buffer
	roles := func(context.Context, rebuild.LayoutOptions) (rebuild.Layout, error) {
		return rebuild.Layout{NextID: "000001"}, nil
	}

	code := runWithGraphRoles([]string{
		"kivgraph", "graph", "status", "--root", "/tmp/kivgraph-empty",
	}, &stdout, &stderr, ladybug.DiagnoseStorage, rebuild.Run, ladybug.VerifyCanonicalIntegrity, roles)
	if code != 0 {
		t.Fatalf("exit code = %d, stderr = %q, want 0: an empty store is not a program error", code, stderr.String())
	}
	out := stdout.String()
	if !strings.Contains(out, "graph.active: none") {
		t.Fatalf("stdout missing empty active role: %q", out)
	}
	if !strings.Contains(out, "graph.backup: none") {
		t.Fatalf("stdout missing empty backup role: %q", out)
	}
	if !strings.Contains(out, "retained: none") {
		t.Fatalf("stdout missing empty retained set: %q", out)
	}
}

func TestRunGraphStatusRequiresRoot(t *testing.T) {
	var stdout, stderr bytes.Buffer
	called := false
	roles := func(context.Context, rebuild.LayoutOptions) (rebuild.Layout, error) {
		called = true
		return rebuild.Layout{}, nil
	}

	code := runWithGraphRoles([]string{
		"kivgraph", "graph", "status",
	}, &stdout, &stderr, ladybug.DiagnoseStorage, rebuild.Run, ladybug.VerifyCanonicalIntegrity, roles)
	if code != 2 {
		t.Fatalf("exit code = %d, want 2", code)
	}
	if called {
		t.Fatal("roles resolver was called")
	}
	if !strings.Contains(stderr.String(), "--root is required") {
		t.Fatalf("stderr = %q", stderr.String())
	}
}

func TestRunGraphStatusReturnsFailureOnRolesError(t *testing.T) {
	var stdout, stderr bytes.Buffer
	roles := func(context.Context, rebuild.LayoutOptions) (rebuild.Layout, error) {
		return rebuild.Layout{}, errors.New("open generation store: boom")
	}

	code := runWithGraphRoles([]string{
		"kivgraph", "graph", "status", "--root", "/tmp/kivgraph-graph",
	}, &stdout, &stderr, ladybug.DiagnoseStorage, rebuild.Run, ladybug.VerifyCanonicalIntegrity, roles)
	if code != 1 {
		t.Fatalf("exit code = %d, want 1", code)
	}
	if !strings.Contains(stderr.String(), "boom") {
		t.Fatalf("stderr = %q, want the roles error", stderr.String())
	}
}

func TestRunRollbackPrintsTransitionDigestsAndInvariants(t *testing.T) {
	var stdout, stderr bytes.Buffer
	rollback := func(_ context.Context, options rebuild.RollbackOptions) (rebuild.RollbackReport, error) {
		if options.Root != "/tmp/kivgraph-graph" {
			t.Fatalf("root = %q, want /tmp/kivgraph-graph", options.Root)
		}
		if options.GenerationID != "000001" {
			t.Fatalf("generation id = %q, want 000001", options.GenerationID)
		}
		return rebuild.RollbackReport{
			From:     generation.Generation{ID: "000002"},
			To:       generation.Generation{ID: "000001"},
			Digest:   "deadbeef",
			Expected: "deadbeef",
			Invariants: ladybug.CanonicalIntegrityReport{
				Findings: []ladybug.IntegrityFinding{{Rule: ladybug.RuleExactEdgeWithoutSource, Passed: true}},
				Passed:   true,
			},
			Passed: true,
		}, nil
	}

	code := runWithGraphRollback([]string{
		"kivgraph", "rollback", "--root", "/tmp/kivgraph-graph", "--generation", "000001",
	}, &stdout, &stderr, ladybug.DiagnoseStorage, rebuild.Run, ladybug.VerifyCanonicalIntegrity, rebuild.Roles, rollback)

	if code != 0 {
		t.Fatalf("exit code = %d, stderr = %q", code, stderr.String())
	}
	if stderr.Len() != 0 {
		t.Fatalf("stderr = %q, want empty", stderr.String())
	}
	out := stdout.String()
	if !strings.Contains(out, "rollback: 000002 -> 000001") {
		t.Fatalf("stdout missing transition: %q", out)
	}
	if !strings.Contains(out, "digest expected: deadbeef") || !strings.Contains(out, "digest observed: deadbeef") {
		t.Fatalf("stdout missing digests: %q", out)
	}
	if !strings.Contains(out, "invariants: PASS") {
		t.Fatalf("stdout missing invariants verdict: %q", out)
	}
	if !strings.Contains(out, "rollback result: PASS") {
		t.Fatalf("stdout missing overall verdict: %q", out)
	}
}

func TestRunRollbackReturnsFailureAndExplainsCause(t *testing.T) {
	var stdout, stderr bytes.Buffer
	rollback := func(context.Context, rebuild.RollbackOptions) (rebuild.RollbackReport, error) {
		return rebuild.RollbackReport{
			From:     generation.Generation{ID: "000002"},
			To:       generation.Generation{ID: "000001"},
			Digest:   "aaa",
			Expected: "bbb",
			Passed:   false,
		}, fmt.Errorf("%w: snapshot digest mismatch", rebuild.ErrRollbackFailed)
	}

	code := runWithGraphRollback([]string{
		"kivgraph", "rollback", "--root", "/tmp/kivgraph-graph",
	}, &stdout, &stderr, ladybug.DiagnoseStorage, rebuild.Run, ladybug.VerifyCanonicalIntegrity, rebuild.Roles, rollback)

	if code != 1 {
		t.Fatalf("exit code = %d, want 1", code)
	}
	if !strings.Contains(stderr.String(), "snapshot digest mismatch") {
		t.Fatalf("stderr = %q, want the rollback failure reason", stderr.String())
	}
	if !strings.Contains(stdout.String(), "digest expected: bbb") || !strings.Contains(stdout.String(), "digest observed: aaa") {
		t.Fatalf("stdout = %q, want the mismatched digests reported even on failure", stdout.String())
	}
	if !strings.Contains(stdout.String(), "rollback result: FAIL") {
		t.Fatalf("stdout = %q, want a FAIL verdict", stdout.String())
	}
}

func TestRunRollbackRequiresRoot(t *testing.T) {
	var stdout, stderr bytes.Buffer
	called := false
	rollback := func(context.Context, rebuild.RollbackOptions) (rebuild.RollbackReport, error) {
		called = true
		return rebuild.RollbackReport{}, nil
	}

	code := runWithGraphRollback([]string{
		"kivgraph", "rollback",
	}, &stdout, &stderr, ladybug.DiagnoseStorage, rebuild.Run, ladybug.VerifyCanonicalIntegrity, rebuild.Roles, rollback)

	if code != 2 {
		t.Fatalf("exit code = %d, want 2", code)
	}
	if called {
		t.Fatal("rollback hook was called")
	}
	if !strings.Contains(stderr.String(), "--root is required") {
		t.Fatalf("stderr = %q", stderr.String())
	}
}

// TestRunRollbackWithoutGenerationDefaultsToBackup is the CLI half of
// "--generation is optional": leaving it out must reach Rollback with an
// empty GenerationID, letting Rollback itself resolve graph.backup.
func TestRunRollbackWithoutGenerationDefaultsToBackup(t *testing.T) {
	var stdout, stderr bytes.Buffer
	rollback := func(_ context.Context, options rebuild.RollbackOptions) (rebuild.RollbackReport, error) {
		if options.GenerationID != "" {
			t.Fatalf("generation id = %q, want empty (defaults to the registered backup)", options.GenerationID)
		}
		return rebuild.RollbackReport{Passed: true, From: generation.Generation{ID: "000002"}, To: generation.Generation{ID: "000001"}}, nil
	}

	code := runWithGraphRollback([]string{
		"kivgraph", "rollback", "--root", "/tmp/kivgraph-graph",
	}, &stdout, &stderr, ladybug.DiagnoseStorage, rebuild.Run, ladybug.VerifyCanonicalIntegrity, rebuild.Roles, rollback)
	if code != 0 {
		t.Fatalf("exit code = %d, stderr = %q", code, stderr.String())
	}
}

func TestRunSnapshotPrintsReportOnSuccess(t *testing.T) {
	var stdout, stderr bytes.Buffer
	build := func(_ context.Context, options rebuild.GenerationSnapshotOptions) (*hotsnapshot.GraphSnapshot, rebuild.SnapshotReport, error) {
		if options.Root != "/tmp/kivgraph-graph" {
			t.Fatalf("root = %q, want /tmp/kivgraph-graph", options.Root)
		}
		if options.GenerationID != "000002" {
			t.Fatalf("generation id = %q, want 000002", options.GenerationID)
		}
		return nil, rebuild.SnapshotReport{
			SnapshotID: 9, Version: 1, Digest: "deadbeef",
			Stats:  rebuild.SnapshotStats{Repositories: 1, Packages: 1, Files: 2, Symbols: 2, Evidence: 1, Edges: 1, SkippedEdges: 5},
			Passed: true,
		}, nil
	}

	code := runWithSnapshotBuilder([]string{
		"kivgraph", "snapshot", "--root", "/tmp/kivgraph-graph", "--generation", "000002",
	}, &stdout, &stderr, ladybug.DiagnoseStorage, rebuild.Run, ladybug.VerifyCanonicalIntegrity, rebuild.Roles, rebuild.Rollback, build)
	if code != 0 {
		t.Fatalf("exit code = %d, stderr = %q", code, stderr.String())
	}
	if stderr.Len() != 0 {
		t.Fatalf("stderr = %q, want empty", stderr.String())
	}
	out := stdout.String()
	if !strings.Contains(out, "snapshot: PASS") {
		t.Fatalf("stdout missing verdict: %q", out)
	}
	if !strings.Contains(out, "digest: deadbeef") {
		t.Fatalf("stdout missing digest: %q", out)
	}
	if !strings.Contains(out, "symbols: 2") || !strings.Contains(out, "edges not represented in the CSR: 5") {
		t.Fatalf("stdout missing stats: %q", out)
	}
}

func TestRunSnapshotReturnsFailureWhenBuildErrors(t *testing.T) {
	var stdout, stderr bytes.Buffer
	build := func(context.Context, rebuild.GenerationSnapshotOptions) (*hotsnapshot.GraphSnapshot, rebuild.SnapshotReport, error) {
		return nil, rebuild.SnapshotReport{}, fmt.Errorf("%w: edge table \"BOGUS\" is outside the canonical vocabulary", rebuild.ErrSnapshotBuildFailed)
	}

	code := runWithSnapshotBuilder([]string{
		"kivgraph", "snapshot", "--root", "/tmp/kivgraph-graph",
	}, &stdout, &stderr, ladybug.DiagnoseStorage, rebuild.Run, ladybug.VerifyCanonicalIntegrity, rebuild.Roles, rebuild.Rollback, build)
	if code != 1 {
		t.Fatalf("exit code = %d, want 1", code)
	}
	if !strings.Contains(stderr.String(), "outside the canonical vocabulary") {
		t.Fatalf("stderr = %q", stderr.String())
	}
}

func TestRunSnapshotReturnsFailureWhenReportDidNotPass(t *testing.T) {
	var stdout, stderr bytes.Buffer
	build := func(context.Context, rebuild.GenerationSnapshotOptions) (*hotsnapshot.GraphSnapshot, rebuild.SnapshotReport, error) {
		return nil, rebuild.SnapshotReport{Passed: false}, nil
	}

	code := runWithSnapshotBuilder([]string{
		"kivgraph", "snapshot", "--root", "/tmp/kivgraph-graph",
	}, &stdout, &stderr, ladybug.DiagnoseStorage, rebuild.Run, ladybug.VerifyCanonicalIntegrity, rebuild.Roles, rebuild.Rollback, build)
	if code != 1 {
		t.Fatalf("exit code = %d, want 1", code)
	}
	if !strings.Contains(stderr.String(), "report did not pass") {
		t.Fatalf("stderr = %q", stderr.String())
	}
}

func TestRunSnapshotRequiresRoot(t *testing.T) {
	var stdout, stderr bytes.Buffer
	called := false
	build := func(context.Context, rebuild.GenerationSnapshotOptions) (*hotsnapshot.GraphSnapshot, rebuild.SnapshotReport, error) {
		called = true
		return nil, rebuild.SnapshotReport{}, nil
	}

	code := runWithSnapshotBuilder([]string{
		"kivgraph", "snapshot",
	}, &stdout, &stderr, ladybug.DiagnoseStorage, rebuild.Run, ladybug.VerifyCanonicalIntegrity, rebuild.Roles, rebuild.Rollback, build)
	if code != 2 {
		t.Fatalf("exit code = %d, want 2", code)
	}
	if called {
		t.Fatal("build hook was called")
	}
	if !strings.Contains(stderr.String(), "--root is required") {
		t.Fatalf("stderr = %q", stderr.String())
	}
}

// TestRunSnapshotWithoutGenerationDefaultsToEmpty is the CLI half of
// "--generation is optional": leaving it out must reach SnapshotGeneration
// with an empty GenerationID, letting it resolve graph.active itself.
func TestRunSnapshotWithoutGenerationDefaultsToEmpty(t *testing.T) {
	var stdout, stderr bytes.Buffer
	build := func(_ context.Context, options rebuild.GenerationSnapshotOptions) (*hotsnapshot.GraphSnapshot, rebuild.SnapshotReport, error) {
		if options.GenerationID != "" {
			t.Fatalf("generation id = %q, want empty (defaults to graph.active)", options.GenerationID)
		}
		return nil, rebuild.SnapshotReport{Passed: true}, nil
	}

	code := runWithSnapshotBuilder([]string{
		"kivgraph", "snapshot", "--root", "/tmp/kivgraph-graph",
	}, &stdout, &stderr, ladybug.DiagnoseStorage, rebuild.Run, ladybug.VerifyCanonicalIntegrity, rebuild.Roles, rebuild.Rollback, build)
	if code != 0 {
		t.Fatalf("exit code = %d, stderr = %q", code, stderr.String())
	}
}

func TestRunSnapshotPassesSnapshotIDFlagThrough(t *testing.T) {
	var stdout, stderr bytes.Buffer
	build := func(_ context.Context, options rebuild.GenerationSnapshotOptions) (*hotsnapshot.GraphSnapshot, rebuild.SnapshotReport, error) {
		if options.SnapshotID != 42 {
			t.Fatalf("snapshot id = %d, want 42", options.SnapshotID)
		}
		return nil, rebuild.SnapshotReport{Passed: true}, nil
	}

	code := runWithSnapshotBuilder([]string{
		"kivgraph", "snapshot", "--root", "/tmp/kivgraph-graph", "--snapshot-id", "42",
	}, &stdout, &stderr, ladybug.DiagnoseStorage, rebuild.Run, ladybug.VerifyCanonicalIntegrity, rebuild.Roles, rebuild.Rollback, build)
	if code != 0 {
		t.Fatalf("exit code = %d, stderr = %q", code, stderr.String())
	}
}

// TestHelpMarksACommandThisBuildCannotRun keeps the help from advertising a
// viewer this binary cannot serve. The published MCP bundle carries no web
// assets on purpose, and `ui` used to appear in "Getting started" like any
// other command, log that it was starting and then exit 1.
func TestHelpMarksACommandThisBuildCannotRun(t *testing.T) {
	var stdout, stderr bytes.Buffer
	if code := run([]string{"kivgraph", "--help"}, &stdout, &stderr); code != 0 {
		t.Fatalf("--help exit code = %d, want 0", code)
	}
	line := ""
	for _, candidate := range strings.Split(stdout.String(), "\n") {
		if strings.Contains(candidate, "ui [--addr") {
			line = candidate
		}
	}
	if line == "" {
		t.Fatalf("help does not mention ui:\n%s", stdout.String())
	}
	marked := strings.Contains(line, "unavailable: this build carries no web bundle")
	if marked == webassets.Available() {
		t.Fatalf("ui line = %q, but webassets.Available() = %t", line, webassets.Available())
	}
}

// TestUsageNamesOnlyRealFlags is what replaced the old check that the
// unavailable-command map named a real command: absence is a field on the spec
// now, so that drift cannot happen. This is the drift that still can. The usage
// line is a curated summary -- naming all eight flags of `logs` would make the
// help worse -- so it may name fewer flags than the command has, but never one
// the command does not accept.
func TestUsageNamesOnlyRealFlags(t *testing.T) {
	for _, spec := range allCommands() {
		if spec.hidden {
			continue
		}
		// `index --full` spells part of its invocation with dashes, so
		// the words themselves are not flags to look up.
		declared := map[string]bool{"help": true}
		for _, word := range spec.words {
			declared[strings.TrimPrefix(word, "--")] = true
		}
		forEachFlag(spec, func(entry *flag.Flag) { declared[entry.Name] = true })
		for _, named := range flagNamesIn(spec.usage) {
			if !declared[named] {
				t.Fatalf("the usage of %q names --%s, which its flag set does not declare",
					spec.name(), named)
			}
		}
	}
}

// TestInterceptedCommandsDeclareTheFlagsTheyParse closes the direction the test
// above deliberately leaves open. That one allows a usage line to name fewer
// flags than the command has, because curating it is what makes the help
// readable. It cannot catch a spec wired to a *different* command's flag set,
// and that is what happened: `daemon` declared `serveFlagSet`, so the global
// help printed a bare `daemon` and completion offered only `--config` while
// `--addr` and `--allow-remote` worked. `daemon --help` was right the whole
// time, because main parses its own set -- which is exactly why nobody noticed.
//
// The long-lived commands are the ones at risk: main intercepts them before
// `run`, so their spec is documentation with no execution path to contradict it.
func TestInterceptedCommandsDeclareTheFlagsTheyParse(t *testing.T) {
	var path string
	var options daemonOptions
	var address string
	parsed := map[string]*flag.FlagSet{
		"serve":  serveFlagSet(&path),
		"daemon": daemonFlagSet(&path, &options),
		"ui":     uiFlagSet(&path, &address),
	}
	for _, spec := range allCommands() {
		want, intercepted := parsed[spec.name()]
		if !intercepted {
			continue
		}
		declared := map[string]bool{}
		forEachFlag(spec, func(entry *flag.Flag) { declared[entry.Name] = true })
		want.VisitAll(func(entry *flag.Flag) {
			if !declared[entry.Name] {
				t.Errorf("%q parses --%s but its spec does not declare it, so the help and the completion cannot name it",
					spec.name(), entry.Name)
			}
		})
	}
}

// flagNamesIn answers the long flag names a usage line spells.
func flagNamesIn(usage string) []string {
	names := make([]string, 0, 4)
	for _, field := range strings.Fields(usage) {
		trimmed := strings.Trim(field, "[]|")
		if !strings.HasPrefix(trimmed, "--") {
			continue
		}
		names = append(names, strings.TrimPrefix(trimmed, "--"))
	}
	return names
}

// TestUIWarnsWhenTheBindIsReachable keeps the only guard the viewer has. It
// listens on every interface by default, carries no authentication, and its
// responses contain repository paths, file paths, symbol names and
// signatures: the warning is what tells an operator that, and it must name
// what is exposed and how to close it rather than say "unauthenticated".
func TestUIWarnsWhenTheBindIsReachable(t *testing.T) {
	for address, wantWarning := range map[string]bool{
		"0.0.0.0:7777":   true,
		"192.168.1.4:80": true,
		"127.0.0.1:7777": false,
		"[::1]:7777":     false,
	} {
		if got := !isLoopbackListenAddress(address); got != wantWarning {
			t.Fatalf("%q warns = %t, want %t", address, got, wantWarning)
		}
	}
}

// stopFixture drives runStop over a process table the test owns, so the
// escalation path can be exercised without signalling anything real.
type stopFixture struct {
	processes []procstat.Process
	signals   []string
	// diesOn is the signal after which a pid disappears from the table.
	diesOn map[int]syscall.Signal
	// replaceOnTerm swaps a pid for a different command, which is what a
	// reused pid looks like from the outside.
	replaceOnTerm map[int]string
	failOn        map[int]error
}

func (fixture *stopFixture) list() ([]procstat.Process, error) {
	return append([]procstat.Process(nil), fixture.processes...), nil
}

func (fixture *stopFixture) signal(pid int, signal syscall.Signal) error {
	fixture.signals = append(fixture.signals, fmt.Sprintf("%d:%v", pid, signal))
	if err, exists := fixture.failOn[pid]; exists {
		return err
	}
	if replacement, exists := fixture.replaceOnTerm[pid]; exists && signal == syscall.SIGTERM {
		for index := range fixture.processes {
			if fixture.processes[index].PID == pid {
				fixture.processes[index].Args = strings.Fields(replacement)
			}
		}
		return nil
	}
	if dies, exists := fixture.diesOn[pid]; exists && dies == signal {
		remaining := fixture.processes[:0]
		for _, process := range fixture.processes {
			if process.PID != pid {
				remaining = append(remaining, process)
			}
		}
		fixture.processes = remaining
	}
	return nil
}

// shortenStopGrace keeps the escalation tests exercising the real wait
// without paying five seconds of it each.
func shortenStopGrace(t *testing.T) {
	t.Helper()
	previous := stopGracePeriod
	stopGracePeriod = 50 * time.Millisecond
	t.Cleanup(func() { stopGracePeriod = previous })
}

func kivgraphProcess(pid int, command string) procstat.Process {
	return procstat.Process{PID: pid, Args: []string{"/opt/kivgraph/bin/kivgraph", command}}
}

// TestStopEndsLongRunningCommandsOnly is the whole risk of this command: what it
// does not kill. An index in flight is minutes of analysis, the process
// running stop is a kivgraph process too, and anything else on the machine
// must not match however it is named.
func TestStopEndsLongRunningCommandsOnly(t *testing.T) {
	fixture := &stopFixture{
		processes: []procstat.Process{
			kivgraphProcess(11, "serve"),
			kivgraphProcess(12, "ui"),
			// A daemon is the third long-running command, and it is the one a
			// list-based rule forgets: it was added after this command was.
			kivgraphProcess(13, "daemon"),
			kivgraphProcess(14, "index"),
			kivgraphProcess(15, "stop"),
			{PID: 16, Args: []string{"/usr/bin/vim", "kivgraph", "serve"}},
			{PID: 17, Args: []string{"/opt/other/kivgraph-ts-worker", "serve"}},
		},
		diesOn: map[int]syscall.Signal{
			11: syscall.SIGTERM,
			12: syscall.SIGTERM,
			13: syscall.SIGTERM,
		},
	}

	var stdout, stderr bytes.Buffer
	if code := runStop(nil, &stdout, &stderr, fixture.list, fixture.signal, true); code != 0 {
		t.Fatalf("runStop() = %d, want 0: %s", code, stderr.String())
	}
	want := "11:terminated,12:terminated,13:terminated"
	if got := strings.Join(fixture.signals, ","); got != want {
		t.Fatalf("signals = %q, want %q", got, want)
	}
	if !strings.Contains(stdout.String(), "3 process(es) stopped, 0 killed") {
		t.Fatalf("stdout = %q, want the summary", stdout.String())
	}
}

// TestStopKillsWhatDoesNotExit covers the stuck process: SIGTERM first, and
// SIGKILL only after the grace period, so a shutdown that is doing bounded
// work is never cut short.
func TestStopKillsWhatDoesNotExit(t *testing.T) {
	shortenStopGrace(t)
	fixture := &stopFixture{
		processes: []procstat.Process{kivgraphProcess(21, "serve")},
		diesOn:    map[int]syscall.Signal{21: syscall.SIGKILL},
	}

	var stdout, stderr bytes.Buffer
	if code := runStop(nil, &stdout, &stderr, fixture.list, fixture.signal, true); code != 0 {
		t.Fatalf("runStop() = %d, want 0: %s", code, stderr.String())
	}
	if got := strings.Join(fixture.signals, ","); got != "21:terminated,21:killed" {
		t.Fatalf("signals = %q, want SIGTERM then SIGKILL", got)
	}
	if !strings.Contains(stdout.String(), "stop.killed: pid=21") {
		t.Fatalf("stdout = %q, want the kill reported rather than hidden", stdout.String())
	}
}

// TestStopWithoutAGracefulStageTerminatesAndSaysSo covers the platform that
// cannot ask a process it did not start to exit, from the platform that can.
//
// The branch is Windows-only in production and would otherwise be reachable
// only where CI does not run, which is how a platform-specific path stays
// broken for a release. Two things are asserted and both are the point: one
// signal rather than two -- there is no polite one to send first -- and a
// report that says the process was ended rather than that it agreed to stop.
func TestStopWithoutAGracefulStageTerminatesAndSaysSo(t *testing.T) {
	shortenStopGrace(t)
	fixture := &stopFixture{
		processes: []procstat.Process{kivgraphProcess(31, "daemon")},
		diesOn:    map[int]syscall.Signal{31: syscall.SIGKILL},
	}

	var stdout, stderr bytes.Buffer
	if code := runStop(nil, &stdout, &stderr, fixture.list, fixture.signal, false); code != 0 {
		t.Fatalf("runStop() = %d, want 0: %s", code, stderr.String())
	}
	if got := strings.Join(fixture.signals, ","); got != "31:killed" {
		t.Fatalf("signals = %q, want the kill alone and no SIGTERM before it", got)
	}
	if output := stdout.String(); !strings.Contains(output, "stop.terminated: pid=31") {
		t.Fatalf("stdout = %q, want the termination named rather than reported as a stop", output)
	}
	if output := stdout.String(); strings.Contains(output, "stop.killed") {
		t.Fatalf("stdout = %q, want no claim that a grace period elapsed", output)
	}
}

// TestStopNeverKillsAReusedPid keeps the escalation honest. A pid freed during
// the grace period can already belong to something else, and SIGKILL on the
// strength of a number alone would end an unrelated process.
func TestStopNeverKillsAReusedPid(t *testing.T) {
	shortenStopGrace(t)
	fixture := &stopFixture{
		processes:     []procstat.Process{kivgraphProcess(31, "serve")},
		replaceOnTerm: map[int]string{31: "/usr/bin/postgres -D /var/lib/postgres"},
	}

	var stdout, stderr bytes.Buffer
	if code := runStop(nil, &stdout, &stderr, fixture.list, fixture.signal, true); code != 0 {
		t.Fatalf("runStop() = %d, want 0: %s", code, stderr.String())
	}
	if got := strings.Join(fixture.signals, ","); got != "31:terminated" {
		t.Fatalf("signals = %q, want no SIGKILL once the pid names something else", got)
	}
}

// TestStopDryRunSignalsNothing keeps the look-before-you-kill option honest.
func TestStopDryRunSignalsNothing(t *testing.T) {
	fixture := &stopFixture{processes: []procstat.Process{kivgraphProcess(41, "serve")}}

	var stdout, stderr bytes.Buffer
	if code := runStop([]string{"--dry-run"}, &stdout, &stderr, fixture.list, fixture.signal, true); code != 0 {
		t.Fatalf("runStop() = %d, want 0: %s", code, stderr.String())
	}
	if len(fixture.signals) != 0 {
		t.Fatalf("signals = %v, want none", fixture.signals)
	}
	if !strings.Contains(stdout.String(), "stop.would: pid=41") {
		t.Fatalf("stdout = %q, want the process named", stdout.String())
	}
}

// TestStopReportsWhatItCouldNotStop keeps a failure out of the success line.
func TestStopReportsWhatItCouldNotStop(t *testing.T) {
	fixture := &stopFixture{
		processes: []procstat.Process{kivgraphProcess(51, "serve")},
		failOn:    map[int]error{51: errors.New("operation not permitted")},
	}

	var stdout, stderr bytes.Buffer
	if code := runStop(nil, &stdout, &stderr, fixture.list, fixture.signal, true); code != 1 {
		t.Fatalf("runStop() = %d, want 1", code)
	}
	if !strings.Contains(stderr.String(), "operation not permitted") {
		t.Fatalf("stderr = %q, want the reason", stderr.String())
	}
}

// TestStopSaysWhenNothingIsRunning keeps an idempotent command from looking
// like a failure.
func TestStopSaysWhenNothingIsRunning(t *testing.T) {
	fixture := &stopFixture{processes: []procstat.Process{kivgraphProcess(61, "index")}}

	var stdout, stderr bytes.Buffer
	if code := runStop(nil, &stdout, &stderr, fixture.list, fixture.signal, true); code != 0 {
		t.Fatalf("runStop() = %d, want 0", code)
	}
	// The message has to name every command it would have stopped: a reader who
	// left a daemon running and is told only about `serve` concludes the daemon
	// is not a thing `stop` handles.
	for _, command := range longRunningCommands {
		if !strings.Contains(stdout.String(), command) {
			t.Fatalf("stdout = %q, want it to name %q", stdout.String(), command)
		}
	}
	if !strings.Contains(stdout.String(), "process is running") {
		t.Fatalf("stdout = %q, want it said plainly", stdout.String())
	}
}

// TestReportRustToolchainNamesTheMissingPrerequisite covers the one language
// this build does not analyse itself: an absent `rust-analyzer` must be a
// named failure rather than a repository that silently contributes nothing.
func TestReportRustToolchainNamesTheMissingPrerequisite(t *testing.T) {
	tests := map[string]struct {
		command   string
		needsRust bool
		wantPass  bool
		wantValue string
	}{
		"no rust repository": {command: "rust-analyzer", needsRust: false, wantPass: true, wantValue: "not configured"},
		"command is absent": {
			command: "kivgraph-rust-analyzer-that-is-not-installed", needsRust: true,
			wantPass: false, wantValue: "is unavailable",
		},
		"command is empty": {command: "   ", needsRust: true, wantPass: false, wantValue: "analyzer command is empty"},
	}
	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			reported := make(map[string]string)
			passed := make(map[string]bool)
			report := func(check string, ok bool, detail string) {
				reported[check] = detail
				passed[check] = ok
			}
			configuration := config.DefaultConfig()
			configuration.Rust.AnalyzerCommand = test.command
			reportRustToolchain(report, configuration, test.needsRust)

			detail, exists := reported["toolchain.rust"]
			if !exists {
				t.Fatalf("reported = %#v, want a toolchain.rust entry", reported)
			}
			if passed["toolchain.rust"] != test.wantPass {
				t.Fatalf("toolchain.rust passed = %t, want %t (%q)", passed["toolchain.rust"], test.wantPass, detail)
			}
			if !strings.Contains(detail, test.wantValue) {
				t.Fatalf("toolchain.rust detail = %q, want %q", detail, test.wantValue)
			}
		})
	}
}

// TestRunIndexFullEventsWritesOnlyTheEventStream fixes the channel a server
// reads. stdout is the protocol: one result event, and no line of the report a
// person would read, because a report interleaved with events is a stream the
// parent cannot parse.
//
// The pass fails on a missing root, which is the cheapest way to reach the
// result event without building a graph, and it also fixes the other half: a
// failure is reported as a result the caller can read, never as silence.
func TestRunIndexFullEventsWritesOnlyTheEventStream(t *testing.T) {
	var stdout, stderr bytes.Buffer
	if code := runIndexFullEvents(
		context.Background(),
		indexing.FullOptions{ResolverVersion: "test"},
		nil,
		time.Now(),
		&stdout,
		&stderr,
	); code != 1 {
		t.Fatalf("runIndexFullEvents() = %d, want 1", code)
	}

	lines := strings.Split(strings.TrimSpace(stdout.String()), "\n")
	if len(lines) != 1 {
		t.Fatalf("stdout carried %d lines, want exactly the result event:\n%s", len(lines), stdout.String())
	}
	var event indexing.FullEvent
	if err := json.Unmarshal([]byte(lines[0]), &event); err != nil {
		t.Fatalf("stdout line is not an event: %v: %s", err, lines[0])
	}
	if event.Event != indexing.FullEventResult || event.Result == nil {
		t.Fatalf("event = %#v, want a result event", event)
	}
	if event.Result.Passed {
		t.Fatal("result reported a pass that never happened")
	}
	if !strings.Contains(event.Result.Error, "root is required") {
		t.Fatalf("result error = %q, want the reason the pass stopped", event.Result.Error)
	}
	if strings.Contains(stdout.String(), "index.full:") {
		t.Fatalf("stdout carried the human report as well:\n%s", stdout.String())
	}
	if !strings.Contains(stderr.String(), "index --full:") {
		t.Fatalf("stderr = %q, want the failure named there", stderr.String())
	}
}

// Discovery moved off the startup path so the MCP transport opens immediately,
// which put it in reach of shutdown: a serve that exits while git is still being
// asked about the second of thirty-seven repositories cancels it. The pair below
// is the whole contract -- that cancellation is silent, and every other failure
// is still named -- because either half alone is satisfied by a log that says
// nothing at all.
func TestResyncOnBranchChangeKeepsShutdownOutOfTheLog(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	capture := captureStderr(t)
	// Stopping waits for the watcher, so once it returns the log is complete and
	// an absence is a fact rather than a race not yet lost.
	resyncOnBranchChange(ctx, loadedWithRepository(config.Repository{
		Name: "kivgraph", Path: t.TempDir(),
	}), nil, nil, "serve")()
	logged := capture.stop(t)

	if strings.Contains(logged, "could not read the repository registry") {
		t.Fatalf("stderr = %q, want shutdown to say nothing", logged)
	}
}

func TestResyncOnBranchChangeReportsAnUnreadableRegistry(t *testing.T) {
	capture := captureStderr(t)
	stop := resyncOnBranchChange(context.Background(), loadedWithRepository(config.Repository{
		Name: "absent", Path: filepath.Join(t.TempDir(), "not-a-repository"),
	}), nil, nil, "serve")
	// Waiting for the line rather than stopping first: stopping cancels, and a
	// cancelled read is exactly the case this test must not be able to observe.
	capture.waitFor(t, "could not read the repository registry")
	stop()
	capture.stop(t)
}

func loadedWithRepository(repository config.Repository) config.Loaded {
	return config.Loaded{Repositories: config.RepositoriesFile{
		Version:      1,
		Repositories: []config.Repository{repository},
	}}
}

// stderrCapture collects what the logger writes, which goes to os.Stderr rather
// than to a writer the caller chooses.
type stderrCapture struct {
	mutex    sync.Mutex
	buffer   bytes.Buffer
	reader   *os.File
	writer   *os.File
	original *os.File
	drained  chan struct{}
}

func captureStderr(t *testing.T) *stderrCapture {
	t.Helper()
	reader, writer, err := os.Pipe()
	if err != nil {
		t.Fatalf("os.Pipe() error = %v", err)
	}
	capture := &stderrCapture{
		reader: reader, writer: writer, original: os.Stderr, drained: make(chan struct{}),
	}
	os.Stderr = writer
	go func() {
		defer close(capture.drained)
		chunk := make([]byte, 4096)
		for {
			read, err := reader.Read(chunk)
			if read > 0 {
				capture.mutex.Lock()
				capture.buffer.Write(chunk[:read])
				capture.mutex.Unlock()
			}
			if err != nil {
				return
			}
		}
	}()
	return capture
}

func (capture *stderrCapture) text() string {
	capture.mutex.Lock()
	defer capture.mutex.Unlock()
	return capture.buffer.String()
}

func (capture *stderrCapture) waitFor(t *testing.T, want string) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		if strings.Contains(capture.text(), want) {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("stderr = %q, want %q named there", capture.text(), want)
}

func (capture *stderrCapture) stop(t *testing.T) string {
	t.Helper()
	os.Stderr = capture.original
	if err := capture.writer.Close(); err != nil {
		t.Fatalf("close stderr writer: %v", err)
	}
	<-capture.drained
	if err := capture.reader.Close(); err != nil {
		t.Fatalf("close stderr reader: %v", err)
	}
	return capture.text()
}

// TestWriteIndexSummaryNamesWhatWasNotLoaded is the negative that matters. Every
// language pass reports zero symbols when it has no code, and zero symbols is
// also what a repository whose analyzer is missing reports. Without the
// not-loaded count on the same line, a reader cannot tell the two apart -- and
// three of the four counts existed for a release before any of them was printed.
func TestWriteIndexSummaryNamesWhatWasNotLoaded(t *testing.T) {
	var stdout bytes.Buffer
	writeIndexSummary(&stdout, indexer.FullReport{
		GoModulesNotLoaded:          1,
		RustWorkspacesNotLoaded:     2,
		PythonRepositoriesNotLoaded: 3,
		DartRepositoriesNotLoaded:   4,
	})

	report := stdout.String()
	for _, want := range [...]string{
		"index.go: repositories=0 modules=0 not_loaded=1",
		"index.rust: repositories=0 workspaces=0 crates=0 not_loaded=2",
		"index.python: repositories=0 not_loaded=3",
		"index.dart: repositories=0 not_loaded=4",
	} {
		if !strings.Contains(report, want) {
			t.Errorf("report is missing %q:\n%s", want, report)
		}
	}
	// Five languages, always, so a missing line is never read as a missing
	// language.
	for _, language := range [...]string{"index.go:", "index.typescript:", "index.rust:", "index.python:", "index.dart:"} {
		if !strings.Contains(report, language) {
			t.Errorf("report does not name %s:\n%s", language, report)
		}
	}
}

// A generation is named by an unsigned decimal, and anything else is a
// request the command cannot honour rather than a generation zero: reading a
// rejected identifier as 0 would point every malformed request at the same
// real generation.
func TestSnapshotReportIDRejectsWhatIsNotAGeneration(t *testing.T) {
	for _, id := range [...]string{"", " ", "-1", "1.0", "0x10", "seven", "18446744073709551616"} {
		value, err := snapshotReportID(id)
		if err == nil {
			t.Fatalf("snapshotReportID(%q) = %d, want an error", id, value)
		}
		if value != 0 {
			t.Fatalf("snapshotReportID(%q) = %d alongside an error, want 0", id, value)
		}
		if !strings.Contains(err.Error(), "parse generation") || !strings.Contains(err.Error(), id) {
			t.Fatalf("snapshotReportID(%q) error = %v, want it to name the parse and the input", id, err)
		}
	}
}

// The accepted range is the whole of uint64, both ends included: a generation
// counter that wrapped at a smaller width would reject identifiers the store
// can legitimately hold.
func TestSnapshotReportIDAcceptsTheWholeRange(t *testing.T) {
	for id, want := range map[string]uint64{
		"0":                    0,
		"1":                    1,
		"18446744073709551615": ^uint64(0),
	} {
		value, err := snapshotReportID(id)
		if err != nil {
			t.Fatalf("snapshotReportID(%q) error = %v", id, err)
		}
		if value != want {
			t.Fatalf("snapshotReportID(%q) = %d, want %d", id, value, want)
		}
	}
}

// A failed rebuild is explained by the first thing that failed, in the order
// the rebuild ran: stage, then integrity, then invariant, then probe. A
// report whose every check failed must still name the stage, because that is
// the failure that caused the rest.
func TestRebuildFailureReasonNamesTheEarliestFailure(t *testing.T) {
	everything := rebuild.Report{
		Stages: []rebuild.Stage{
			{Name: "load", Passed: true},
			{Name: "verify", Passed: false, Detail: "row count diverged"},
		},
		Integrity: []rebuild.IntegrityCheck{
			{Table: "symbols", Expected: 10, Observed: 9, Passed: false},
		},
		Invariants: ladybug.CanonicalIntegrityReport{
			Findings: []ladybug.IntegrityFinding{{Rule: "dangling_edge", Violations: 3, Passed: false}},
		},
		Probes: []ladybug.CanonicalProbeResult{
			{Probe: "symbol_lookup", Passed: false, Detail: "no rows"},
		},
	}
	if got, want := rebuildFailureReason(everything), `stage "verify" failed: row count diverged`; got != want {
		t.Fatalf("rebuildFailureReason() = %q, want %q", got, want)
	}

	withoutStage := everything
	withoutStage.Stages = []rebuild.Stage{{Name: "load", Passed: true}}
	if got, want := rebuildFailureReason(withoutStage),
		`integrity check "symbols" failed: expected 10, observed 9`; got != want {
		t.Fatalf("rebuildFailureReason() = %q, want %q", got, want)
	}

	withoutIntegrity := withoutStage
	withoutIntegrity.Integrity = nil
	if got, want := rebuildFailureReason(withoutIntegrity),
		`invariant "dangling_edge" failed: 3 violation(s)`; got != want {
		t.Fatalf("rebuildFailureReason() = %q, want %q", got, want)
	}

	withoutInvariants := withoutIntegrity
	withoutInvariants.Invariants = ladybug.CanonicalIntegrityReport{}
	if got, want := rebuildFailureReason(withoutInvariants),
		`probe "symbol_lookup" failed: no rows`; got != want {
		t.Fatalf("rebuildFailureReason() = %q, want %q", got, want)
	}
}

// A report that failed and names nothing still has to say so. Returning the
// empty string would print a bare "rebuild failed:" with the cause missing
// exactly where the reader is looking for it.
func TestRebuildFailureReasonFallsBackWhenNothingIsMarkedFailed(t *testing.T) {
	for _, report := range [...]rebuild.Report{
		{},
		{
			Stages:     []rebuild.Stage{{Name: "load", Passed: true}},
			Integrity:  []rebuild.IntegrityCheck{{Table: "symbols", Passed: true}},
			Invariants: ladybug.CanonicalIntegrityReport{Findings: []ladybug.IntegrityFinding{{Rule: "ok", Passed: true}}},
			Probes:     []ladybug.CanonicalProbeResult{{Probe: "lookup", Passed: true}},
		},
	} {
		if got, want := rebuildFailureReason(report), "full rebuild failed"; got != want {
			t.Fatalf("rebuildFailureReason(%+v) = %q, want %q", report, got, want)
		}
	}
}

// A progress line names the phase, the repository, the position and the
// state, and each of those is absent from the line exactly when it is absent
// from the event. A line that printed "0/0" or a bare phase where a
// repository was named would report work nobody did.
func TestWriteIndexProgressNamesOnlyWhatTheEventCarries(t *testing.T) {
	// The elapsed seconds are fixed by placing the start in the past, so
	// the assertion covers the format without depending on the clock.
	start := time.Now().Add(-12 * time.Second)
	for _, testCase := range [...]struct {
		name  string
		event indexer.ProgressEvent
		want  string
	}{
		{
			name:  "a phase with no repository and no total",
			event: indexer.ProgressEvent{Phase: "publish", Started: true},
			want:  "[  12.0s] publish start\n",
		},
		{
			name:  "a repository carries the phase and its position",
			event: indexer.ProgressEvent{Phase: "index", Repository: "workspace", Completed: 2, Total: 7},
			want:  "[  12.0s] 2/7 index workspace done\n",
		},
		{
			name:  "a detail is parenthesised after the state",
			event: indexer.ProgressEvent{Phase: "load", Repository: "workspace", Detail: "3 modules", Started: true},
			want:  "[  12.0s] load workspace start (3 modules)\n",
		},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			var stderr bytes.Buffer
			writeIndexProgress(&stderr, start, testCase.event)
			if stderr.String() != testCase.want {
				t.Fatalf("writeIndexProgress() wrote %q, want %q", stderr.String(), testCase.want)
			}
		})
	}
}

// No movement is not the same as movement that changed nothing. An empty list
// means nothing moved, so there is no commit whose tree could be compared,
// and answering true would skip a rebuild on the strength of a comparison
// nobody made.
func TestCommitChangedNothingIsFalseWhenNothingMoved(t *testing.T) {
	if commitChangedNothing(context.Background(), nil) {
		t.Fatal("commitChangedNothing(nil) = true, want false: nothing moved is not an unchanged tree")
	}
	if commitChangedNothing(context.Background(), []indexing.RepositoryMovement{}) {
		t.Fatal("commitChangedNothing(empty) = true, want false")
	}
}

// A pid that names no process is an error, not a silent success: `stop`
// escalates to SIGKILL only after confirming the process is still there, so a
// nil error here would escalate against a pid that has already been reused.
//
// The os.FindProcess failure branch stays uncovered: on Unix FindProcess is
// documented always to succeed, so reaching it would need a platform this
// build does not target.
func TestSignalProcessFailsOnAPidThatNamesNoProcess(t *testing.T) {
	// The maximum pid a Linux kernel hands out is far below this, and a
	// negative pid would address a process group instead of a process.
	if err := signalProcess(1<<30, syscall.Signal(0)); err == nil {
		t.Fatal("signalProcess(unused pid, 0) = nil, want an error")
	}
}

// goCeilingRepository registers a directory holding one module that declares
// the given language version. The repository is built literally because
// reportGoLanguageCeiling reads the registry entries rather than the registry,
// so a commit would be state the check never looks at.
func goCeilingRepository(t *testing.T, name, goVersion string) workspace.Repository {
	t.Helper()
	root := testsupport.TempDir(t)
	module := "module example.com/" + name + "\n\ngo " + goVersion + "\n"
	if err := os.WriteFile(filepath.Join(root, "go.mod"), []byte(module), 0o600); err != nil {
		t.Fatalf("WriteFile(go.mod) error = %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, "doc.go"), []byte("package "+name+"\n"), 0o600); err != nil {
		t.Fatalf("WriteFile(doc.go) error = %v", err)
	}
	return workspace.Repository{
		Name: name, Path: root, RealPath: root, Languages: []string{"go"},
	}
}

// The ceiling is what this build type-checks with, and it is reported as a
// pass even when nothing is registered: an empty registry cannot raise the
// requirement above what the build supports.
//
// The branch where LanguageVersion() answers nothing stays uncovered: it
// requires runtime.Version() to be unparseable, which no real toolchain
// produces and no test can arrange without a function that exists only for
// the test.
func TestReportGoLanguageCeilingPassesWithNothingRegistered(t *testing.T) {
	var name, detail string
	var passed bool
	reportGoLanguageCeiling(func(gotName string, gotPassed bool, gotDetail string) {
		name, passed, detail = gotName, gotPassed, gotDetail
	}, nil)
	if name != "toolchain.typecheck" || !passed {
		t.Fatalf("report(%q, %v, %q), want a passing toolchain.typecheck", name, passed, detail)
	}
	ceiling := goworkspace.LanguageVersion()
	if !strings.Contains(detail, "go "+ceiling) {
		t.Fatalf("detail = %q, want it to name the ceiling go %s", detail, ceiling)
	}
}

// A module that needs a newer language version than this build understands is
// the one failure this check reports, and it has to fail rather than name a
// ceiling the module exceeds.
func TestReportGoLanguageCeilingFailsOnAModuleAboveTheCeiling(t *testing.T) {
	var passed bool
	var detail string
	reportGoLanguageCeiling(func(_ string, gotPassed bool, gotDetail string) {
		passed, detail = gotPassed, gotDetail
	}, []workspace.Repository{goCeilingRepository(t, "ahead", "1.99")})
	if passed {
		t.Fatalf("report passed with detail %q, want a failure for a module above the ceiling", detail)
	}
	if !strings.Contains(detail, "requires go 1.99") {
		t.Fatalf("detail = %q, want it to name the version the module requires", detail)
	}
}

// A module at or below the ceiling leaves the verdict passing, and the detail
// names the highest registered module so a reader can see how close the
// registry is to the limit.
func TestReportGoLanguageCeilingNamesTheHighestRegisteredModule(t *testing.T) {
	var passed bool
	var detail string
	reportGoLanguageCeiling(func(_ string, gotPassed bool, gotDetail string) {
		passed, detail = gotPassed, gotDetail
	}, []workspace.Repository{goCeilingRepository(t, "behind", "1.21")})
	if !passed {
		t.Fatalf("report failed with detail %q, want a module below the ceiling to pass", detail)
	}
	if !strings.Contains(detail, "highest registered module") {
		t.Fatalf("detail = %q, want it to name the highest registered module", detail)
	}
}

// A module may declare a patch release of the ceiling -- go 1.24.3 against a
// build that type-checks with go 1.24 -- and that is accepted, because a
// patch never adds a language feature. The detail still has to name it as the
// highest registered module: a reader comparing the two numbers is asking how
// close the registry is to the limit, and rounding the module down to the
// ceiling hides that it is already at it.
func TestReportGoLanguageCeilingNamesAPatchAboveTheCeiling(t *testing.T) {
	ceiling := goworkspace.LanguageVersion()
	if ceiling == "" {
		t.Skip("this build reports no Go language ceiling")
	}
	patch := ceiling + ".3"
	var passed bool
	var detail string
	reportGoLanguageCeiling(func(_ string, gotPassed bool, gotDetail string) {
		passed, detail = gotPassed, gotDetail
	}, []workspace.Repository{goCeilingRepository(t, "patched", patch)})
	if !passed {
		t.Fatalf("report failed with detail %q, want a patch of the ceiling to pass", detail)
	}
	if !strings.Contains(detail, "highest registered module: go "+patch) {
		t.Fatalf("detail = %q, want it to name go %s as the highest registered module", detail, patch)
	}
}

// cleanTestStore opens a real generation store, because what cleanGenerations
// adds to the store is the choice between the two discards and the context of
// the error, and a fake store would test neither.
func cleanTestStore(t *testing.T) *generation.Store {
	t.Helper()
	store, err := generation.New(testsupport.TempDir(t), generation.DefaultConfig())
	if err != nil {
		testsupport.RequireSpaceOrSkip(t, err)
		t.Fatalf("generation.New() error = %v", err)
	}
	return store
}

// A store that never published has nothing to discard, and that is a success
// with an empty list rather than a failure: `clean` on a fresh installation
// must not report an error.
func TestCleanGenerationsAcceptsAStoreThatNeverPublished(t *testing.T) {
	removed, err := cleanGenerations(context.Background(), cleanTestStore(t), false, "")
	if err != nil {
		t.Fatalf("cleanGenerations() error = %v", err)
	}
	if len(removed) != 0 {
		t.Fatalf("cleanGenerations() = %v, want nothing removed", removed)
	}
}

// Keeping a generation requires naming one, and the empty name is refused
// rather than read as "keep nothing" -- which would discard everything under
// a flag that asks to preserve. runClean never gets here: it refuses
// --keep-active on a store with no published generation first, and this test
// is what keeps that guard from being the only thing standing between the
// flag and the opposite of what it asks for.
func TestCleanGenerationsRefusesToKeepAGenerationWithNoName(t *testing.T) {
	removed, err := cleanGenerations(context.Background(), cleanTestStore(t), true, "")
	if err == nil {
		t.Fatalf("cleanGenerations(keepActive, \"\") = %v, want an error", removed)
	}
	if removed != nil {
		t.Fatalf("cleanGenerations() = %v alongside an error, want nothing", removed)
	}
}

// Keeping the published generation removes the others and leaves that one
// alone. This is what --keep-active is for, so a run that removed it -- or
// that removed nothing -- would defeat the flag in the two opposite ways.
func TestCleanGenerationsKeepsOnlyTheActiveGeneration(t *testing.T) {
	store := cleanTestStore(t)
	for _, id := range [...]string{"000001", "000002", "000003"} {
		publishCleanGeneration(t, store, id)
	}
	removed, err := cleanGenerations(context.Background(), store, true, "000003")
	if err != nil {
		t.Fatalf("cleanGenerations() error = %v", err)
	}
	if strings.Join(removed, ",") != "000001,000002" {
		t.Fatalf("cleanGenerations() = %v, want the two generations that are not active", removed)
	}
	active, err := store.Current(context.Background())
	if err != nil {
		t.Fatalf("Current() error = %v", err)
	}
	if active.ID != "000003" {
		t.Fatalf("Current() = %q, want the kept generation 000003", active.ID)
	}
}

// publishCleanGeneration publishes a generation whose payload is only what
// the store requires, because what is under test is which generations survive
// a discard, not what they hold.
func publishCleanGeneration(t *testing.T, store *generation.Store, id string) {
	t.Helper()
	_, err := store.Publish(context.Background(), generation.PublishRequest{
		ID: id,
		Build: func(_ context.Context, path string) error {
			return os.WriteFile(filepath.Join(path, "graph.db"), []byte(id), 0o600)
		},
		Validate: func(context.Context, generation.Generation) error { return nil },
	})
	if err != nil {
		testsupport.RequireSpaceOrSkip(t, err)
		t.Fatalf("Publish(%s) error = %v", id, err)
	}
}

// Keeping a generation the store does not hold is a request it cannot honour,
// and the failure has to name the discard: an error that only said "not
// found" would leave the reader guessing which half of `clean` failed.
func TestCleanGenerationsFailsWhenTheKeptGenerationIsAbsent(t *testing.T) {
	removed, err := cleanGenerations(context.Background(), cleanTestStore(t), true, "000007")
	if err == nil {
		t.Fatalf("cleanGenerations() = %v, want an error for a generation the store does not hold", removed)
	}
	if removed != nil {
		t.Fatalf("cleanGenerations() = %v alongside an error, want nothing", removed)
	}
	if !strings.Contains(err.Error(), "discard generations:") {
		t.Fatalf("error = %v, want it to name the discard", err)
	}
}

// unregisterableConfig writes a configuration whose registry names a
// directory that is not a repository, which is the shape registration
// refuses.
func unregisterableConfig(t *testing.T) string {
	t.Helper()
	directory := t.TempDir()
	configPath := filepath.Join(directory, "config.yaml")
	initConfig(t, configPath)
	err := config.RegisterRepositories(filepath.Join(directory, "repositories.yaml"), []config.Repository{{
		Name: "loose", Path: testsupport.TempDir(t), Languages: []string{"go"},
	}})
	if err != nil {
		t.Fatalf("config.RegisterRepositories() error = %v", err)
	}
	return configPath
}

// A request `index --full` cannot use is rejected before a repository is
// read: a pass costs minutes, and starting one on a misspelled flag would
// spend them answering for settings nobody asked for.
func TestRunIndexFullRejectsAnUnusableRequest(t *testing.T) {
	for _, args := range [][]string{
		{"--nonexistent"},
		{"unexpected"},
	} {
		var stdout, stderr bytes.Buffer
		if code := runIndexFull(args, &stdout, &stderr); code != 2 {
			t.Fatalf("runIndexFull(%v) = %d, want 2 (stderr=%q)", args, code, stderr.String())
		}
	}
}

// A configuration or a registry that cannot be read stops the pass. Indexing
// the default set instead would publish a graph built from repositories the
// caller did not name.
//
// These are every branch of runIndexFull a test can reach without running a
// pass. What is left below the registration is the pass itself -- minutes of
// work over real repositories, ending in a published generation -- which
// belongs to an integration suite and not here, and the os.Getwd failure,
// which needs the working directory to be removed underneath the process.
func TestRunIndexFullFailsOnSourcesItCannotRead(t *testing.T) {
	absent := filepath.Join(t.TempDir(), "absent.yaml")
	configPath := filepath.Join(t.TempDir(), "config.yaml")
	initConfig(t, configPath)
	for _, testCase := range [...]struct {
		name string
		args []string
	}{
		{name: "an unreadable configuration", args: []string{"--config", absent}},
		{
			name: "an unreadable registry override",
			args: []string{"--config", configPath, "--repositories", absent},
		},
		{
			name: "a registered path that is not a repository",
			args: []string{"--config", unregisterableConfig(t)},
		},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			var stdout, stderr bytes.Buffer
			if code := runIndexFull(testCase.args, &stdout, &stderr); code != 1 {
				t.Fatalf("runIndexFull(%v) = %d, want 1 (stderr=%q)", testCase.args, code, stderr.String())
			}
			if !strings.Contains(stderr.String(), "index --full:") {
				t.Fatalf("stderr = %q, want it to name the command that failed", stderr.String())
			}
		})
	}
}
