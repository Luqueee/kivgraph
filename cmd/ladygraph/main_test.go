package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Luqueee/ladygraph/internal/config"
	"github.com/Luqueee/ladygraph/internal/facts"
	"github.com/Luqueee/ladygraph/internal/hotsnapshot"
	"github.com/Luqueee/ladygraph/internal/indexing"
	"github.com/Luqueee/ladygraph/internal/logging"
	"github.com/Luqueee/ladygraph/internal/rebuild"
	"github.com/Luqueee/ladygraph/internal/storage/generation"
	"github.com/Luqueee/ladygraph/internal/storage/ladybug"
	"github.com/Luqueee/ladygraph/internal/synthetic"
	"github.com/Luqueee/ladygraph/internal/update"
	"github.com/Luqueee/ladygraph/internal/version"
)

func TestRunVersion(t *testing.T) {
	var stdout, stderr bytes.Buffer

	if got := run([]string{"ladygraph", "version"}, &stdout, &stderr); got != 0 {
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

	if got := run([]string{"ladygraph", "version", "--json"}, &stdout, &stderr); got != 0 {
		t.Fatalf("run() exit code = %d, want 0", got)
	}
	if stderr.Len() != 0 {
		t.Fatalf("stderr = %q, want empty", stderr.String())
	}

	var provenance version.Provenance
	if err := json.Unmarshal(stdout.Bytes(), &provenance); err != nil {
		t.Fatalf("version JSON = %q: %v", stdout.String(), err)
	}
	if provenance.Ladygraph != version.Value {
		t.Fatalf("Ladygraph = %q, want %q", provenance.Ladygraph, version.Value)
	}
	if provenance.Go == "" || provenance.Ladybug != ladybug.CoreVersion || provenance.GoLadybug != ladybug.GoBindingVersion {
		t.Fatalf("provenance = %#v", provenance)
	}
	if provenance.Schema != ladybug.CanonicalSchemaVersion || provenance.SnapshotRowFormat != rebuild.SnapshotRowFormatVersion {
		t.Fatalf("schema = %d/%d, want %d/%d", provenance.Schema, provenance.SnapshotRowFormat, ladybug.CanonicalSchemaVersion, rebuild.SnapshotRowFormatVersion)
	}
	if provenance.Grammars.Manifest != "grammars/manifest.json" || len(provenance.Grammars.Versions) != 4 {
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

	if got := runUpdateWithRunner([]string{"--check"}, &stdout, &stderr, runner); got != 0 {
		t.Fatalf("runUpdateWithRunner() exit code = %d, stderr=%q", got, stderr.String())
	}
	if !called {
		t.Fatal("update runner was not called")
	}
	if got, want := stdout.String(), "ladygraph update available: "+version.Value+" -> 0.1.1\n"; got != want {
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

	if got := runUpdateWithRunner(nil, &stdout, &stderr, runner); got != 0 {
		t.Fatalf("runUpdateWithRunner() exit code = %d, stderr=%q", got, stderr.String())
	}
	if got, want := stdout.String(), "ladygraph updated: "+version.Value+" -> 0.1.1\n"; got != want {
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
	t.Setenv("HOME", home)
	configPath := filepath.Join(root, "config.yaml")
	repositoriesPath := filepath.Join(root, "repositories.yaml")

	var initStdout, initStderr bytes.Buffer
	if got := run([]string{
		"ladygraph",
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
	if got := run([]string{"ladygraph", "doctor", "--config", configPath}, &doctorStdout, &doctorStderr); got != 0 {
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
	t.Setenv("HOME", home)
	configPath := filepath.Join(root, "config.yaml")
	repositoriesPath := filepath.Join(root, "repositories.yaml")
	var initStdout, initStderr bytes.Buffer
	if got := run([]string{
		"ladygraph",
		"init",
		"--config", configPath,
		"--repositories", repositoriesPath,
	}, &initStdout, &initStderr); got != 0 {
		t.Fatalf("init exit code = %d, stderr=%q", got, initStderr.String())
	}

	var stdout, stderr bytes.Buffer
	if got := run([]string{"ladygraph", "upgrade", "--config", configPath}, &stdout, &stderr); got != 1 {
		t.Fatalf("upgrade exit code = %d, stdout=%q stderr=%q", got, stdout.String(), stderr.String())
	}
	if !strings.Contains(stderr.String(), "no published generation") {
		t.Fatalf("upgrade stderr = %q, want no-generation failure", stderr.String())
	}
}

func TestRunDoctorRejectsInaccessibleRepository(t *testing.T) {
	root := t.TempDir()
	home := filepath.Join(root, "home")
	if err := os.Mkdir(home, 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("HOME", home)
	configPath := filepath.Join(root, "config.yaml")
	repositoriesPath := filepath.Join(root, "repositories.yaml")
	var initStdout, initStderr bytes.Buffer
	if got := run([]string{
		"ladygraph",
		"init",
		"--config", configPath,
		"--repositories", repositoriesPath,
		"--repository", "missing=" + filepath.Join(root, "does-not-exist"),
	}, &initStdout, &initStderr); got != 0 {
		t.Fatalf("init exit code = %d, stderr=%q", got, initStderr.String())
	}

	var stdout, stderr bytes.Buffer
	if got := run([]string{"ladygraph", "doctor", "--config", configPath}, &stdout, &stderr); got != 1 {
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
	t.Setenv("HOME", home)
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
	err := runConfiguredServe(context.Background(), []string{"--config", configPath},
		func(_ context.Context, store *hotsnapshot.SnapshotStore, indexer indexing.ProjectIndexer) error {
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
	t.Setenv("HOME", filepath.Join(root, "home"))
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

	// A published generation whose database is not a graph makes the
	// follower fail on every tick. That failure is the signal: a follower
	// still running after stop keeps producing it.
	generations, err := generation.New(filepath.Dir(loaded.Config.Storage.DatabasePath), generation.DefaultConfig())
	if err != nil {
		t.Fatalf("generation.New() error = %v", err)
	}
	nextID, err := generations.NextID(context.Background())
	if err != nil {
		t.Fatalf("NextID() error = %v", err)
	}
	if _, err := generations.Publish(context.Background(), generation.PublishRequest{
		ID: nextID,
		Build: func(_ context.Context, directory string) error {
			return os.WriteFile(filepath.Join(directory, "graph.db"), []byte("not a graph"), 0o600)
		},
		Validate: func(context.Context, generation.Generation) error { return nil },
	}); err != nil {
		t.Fatalf("Publish() error = %v", err)
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
	t.Setenv("HOME", filepath.Join(root, "home"))
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
	})
	if err != nil {
		t.Fatalf("runConfiguredUI() error = %v", err)
	}
	if !called {
		t.Fatal("web runner was not called")
	}
}
func TestRunConfiguredUIUsesLoopbackDefaultAddress(t *testing.T) {
	root := t.TempDir()
	t.Setenv("HOME", filepath.Join(root, "home"))
	configPath := filepath.Join(root, "config.yaml")
	repositoriesPath := filepath.Join(root, "repositories.yaml")
	if _, err := config.Initialize(config.InitOptions{
		ConfigPath:       configPath,
		RepositoriesPath: repositoriesPath,
	}); err != nil {
		t.Fatalf("config.Initialize() error = %v", err)
	}

	err := runConfiguredUI(context.Background(), []string{"--config", configPath}, func(_ context.Context, address string, _ http.Handler) error {
		if address != "127.0.0.1:7777" {
			t.Fatalf("address = %q, want 127.0.0.1:7777", address)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("runConfiguredUI() error = %v", err)
	}
}

func TestRunWithoutCommandPointsAtTheHelp(t *testing.T) {
	var stdout, stderr bytes.Buffer

	if got := run([]string{"ladygraph"}, &stdout, &stderr); got != 2 {
		t.Fatalf("run() exit code = %d, want 2", got)
	}
	if stdout.Len() != 0 {
		t.Fatalf("stdout = %q, want empty", stdout.String())
	}
	for _, want := range []string{"ladygraph: no command given", `Run "ladygraph --help"`} {
		if !strings.Contains(stderr.String(), want) {
			t.Fatalf("stderr = %q, want it to contain %q", stderr.String(), want)
		}
	}
}

func TestRunNamesAnUnknownCommand(t *testing.T) {
	var stdout, stderr bytes.Buffer

	if got := run([]string{"ladygraph", "inedx"}, &stdout, &stderr); got != 2 {
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
		if got := run([]string{"ladygraph", argument}, &stdout, &stderr); got != 0 {
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

	if got := run([]string{"ladygraph", "rollback", "--help"}, &stdout, &stderr); got != 0 {
		t.Fatalf("run() exit code = %d, want 0", got)
	}
	if stderr.Len() != 0 {
		t.Fatalf("stderr = %q, want empty", stderr.String())
	}
	for _, want := range []string{"ladygraph rollback", "Flags", "--root", "--generation"} {
		if !strings.Contains(stdout.String(), want) {
			t.Fatalf("stdout = %q, want it to contain %q", stdout.String(), want)
		}
	}
}

// TestUnknownFlagNamesItselfAndExitsTwo keeps a real mistake distinguishable
// from a help request.
func TestUnknownFlagNamesItselfAndExitsTwo(t *testing.T) {
	var stdout, stderr bytes.Buffer

	if got := run([]string{"ladygraph", "doctor", "--nope"}, &stdout, &stderr); got != 2 {
		t.Fatalf("run() exit code = %d, want 2", got)
	}
	if stdout.Len() != 0 {
		t.Fatalf("stdout = %q, want empty", stdout.String())
	}
	if !strings.Contains(stderr.String(), "doctor: flag provided but not defined: -nope") {
		t.Fatalf("stderr = %q, want the rejected flag named", stderr.String())
	}
}

func TestCLIErrorWriterEmitsJSONToStderr(t *testing.T) {
	var stdout, stderr bytes.Buffer
	logger := logging.New(&stderr)

	if got := run([]string{"ladygraph"}, &stdout, logging.NewErrorWriter(logger)); got != 2 {
		t.Fatalf("run() exit code = %d, want 2", got)
	}
	if stdout.Len() != 0 {
		t.Fatalf("stdout = %q, want empty", stdout.String())
	}

	var record map[string]any
	if err := json.Unmarshal([]byte(strings.SplitN(stderr.String(), "\n", 2)[0]), &record); err != nil {
		t.Fatalf("stderr = %q, want JSON records: %v", stderr.String(), err)
	}
	if record["level"] != "ERROR" || record["msg"] != "command stderr" {
		t.Fatalf("record = %#v, want structured command error", record)
	}
	message, ok := record["message"].(string)
	if !ok || !strings.Contains(message, "no command given") {
		t.Fatalf("record message = %#v, want the usage error", record["message"])
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

	code := runWithStorageDiagnoser([]string{"ladygraph", "doctor", "storage", "--database", "/tmp/graph.db"}, &stdout, &stderr, diagnose)
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

	code := runWithStorageDiagnoser([]string{"ladygraph", "doctor", "storage", "--database=/tmp/graph.db"}, &stdout, &stderr, diagnose)
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

	if code := runWithStorageDiagnoser([]string{"ladygraph", "doctor", "storage"}, &stdout, &stderr, diagnose); code != 2 {
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

	code := runWithGraphVerifier([]string{"ladygraph", "doctor", "graph", "--database", "/tmp/graph.db"}, &stdout, &stderr, ladybug.DiagnoseStorage, rebuild.Run, verify)
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

	code := runWithGraphVerifier([]string{"ladygraph", "doctor", "graph", "--database", "/tmp/graph.db"}, &stdout, &stderr, ladybug.DiagnoseStorage, rebuild.Run, verify)
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

	code := runWithGraphVerifier([]string{"ladygraph", "doctor", "graph"}, &stdout, &stderr, ladybug.DiagnoseStorage, rebuild.Run, verify)
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
		"ladygraph", "benchmark", "generate-graph",
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
	args := []string{"ladygraph", "benchmark", "generate-graph", "--files", "2", "--symbols", "9", "--edges", "10"}
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
		if options.Root != "/tmp/ladygraph-graph" {
			t.Fatalf("root = %q, want /tmp/ladygraph-graph", options.Root)
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
				Generation: generation.Generation{ID: "000123", Path: "/tmp/ladygraph-graph/generations/000123"},
			},
			Passed: true,
		}, nil
	}

	code := runWithGraphRebuilder([]string{
		"ladygraph", "rebuild",
		"--facts", factsPath,
		"--root", "/tmp/ladygraph-graph",
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
		"ladygraph", "rebuild",
		"--facts", factsPath,
		"--root", "/tmp/ladygraph-graph",
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
		"ladygraph", "rebuild",
		"--facts", factsPath,
		"--root", "/tmp/ladygraph-graph",
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
		"ladygraph", "rebuild",
		"--root", "/tmp/ladygraph-graph",
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
		"ladygraph", "rebuild",
		"--facts", factsPath,
		"--root", "/tmp/ladygraph-graph",
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
		if options.Root != "/tmp/ladygraph-graph" {
			t.Fatalf("root = %q, want /tmp/ladygraph-graph", options.Root)
		}
		return rebuild.Layout{
			Active:    generation.Generation{ID: "000002", Path: "/tmp/ladygraph-graph/generations/000002"},
			Backup:    generation.Generation{ID: "000001", Path: "/tmp/ladygraph-graph/generations/000001"},
			HasBackup: true,
			NextID:    "000003",
			Retained:  []string{"000001", "000002"},
		}, nil
	}

	code := runWithGraphRoles([]string{
		"ladygraph", "graph", "status", "--root", "/tmp/ladygraph-graph",
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
		"ladygraph", "graph", "status", "--root", "/tmp/ladygraph-empty",
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
		"ladygraph", "graph", "status",
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
		"ladygraph", "graph", "status", "--root", "/tmp/ladygraph-graph",
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
		if options.Root != "/tmp/ladygraph-graph" {
			t.Fatalf("root = %q, want /tmp/ladygraph-graph", options.Root)
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
		"ladygraph", "rollback", "--root", "/tmp/ladygraph-graph", "--generation", "000001",
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
		"ladygraph", "rollback", "--root", "/tmp/ladygraph-graph",
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
		"ladygraph", "rollback",
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
		"ladygraph", "rollback", "--root", "/tmp/ladygraph-graph",
	}, &stdout, &stderr, ladybug.DiagnoseStorage, rebuild.Run, ladybug.VerifyCanonicalIntegrity, rebuild.Roles, rollback)
	if code != 0 {
		t.Fatalf("exit code = %d, stderr = %q", code, stderr.String())
	}
}

func TestRunSnapshotPrintsReportOnSuccess(t *testing.T) {
	var stdout, stderr bytes.Buffer
	build := func(_ context.Context, options rebuild.GenerationSnapshotOptions) (*hotsnapshot.GraphSnapshot, rebuild.SnapshotReport, error) {
		if options.Root != "/tmp/ladygraph-graph" {
			t.Fatalf("root = %q, want /tmp/ladygraph-graph", options.Root)
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
		"ladygraph", "snapshot", "--root", "/tmp/ladygraph-graph", "--generation", "000002",
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
		"ladygraph", "snapshot", "--root", "/tmp/ladygraph-graph",
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
		"ladygraph", "snapshot", "--root", "/tmp/ladygraph-graph",
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
		"ladygraph", "snapshot",
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
		"ladygraph", "snapshot", "--root", "/tmp/ladygraph-graph",
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
		"ladygraph", "snapshot", "--root", "/tmp/ladygraph-graph", "--snapshot-id", "42",
	}, &stdout, &stderr, ladybug.DiagnoseStorage, rebuild.Run, ladybug.VerifyCanonicalIntegrity, rebuild.Roles, rebuild.Rollback, build)
	if code != 0 {
		t.Fatalf("exit code = %d, stderr = %q", code, stderr.String())
	}
}
