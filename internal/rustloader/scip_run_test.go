package rustloader

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Luqueee/ladygraph/internal/rustloader/scipwire"
	"github.com/Luqueee/ladygraph/internal/testsupport"
)

// analyzerFixture copies the recorded Rust workspace into a private directory,
// because indexing must never touch the repository the fixture lives in.
func analyzerFixture(t *testing.T) (workspace, output string) {
	t.Helper()
	root := testsupport.TempDir(t)
	workspace = filepath.Join(root, "workspace")
	output = filepath.Join(root, "out")
	source := filepath.Join("..", "..", "testdata", "rust", "workspace")
	if err := os.CopyFS(workspace, os.DirFS(source)); err != nil {
		t.Fatalf("copy fixture: %v", err)
	}
	if err := os.MkdirAll(output, 0o700); err != nil {
		t.Fatalf("MkdirAll(%q) error = %v", output, err)
	}
	return workspace, output
}

func defaultRunOptions(workspace, output string) RunOptions {
	return RunOptions{
		Workspace:       workspace,
		OutputDirectory: output,
		AnalyzerCommand: "rust-analyzer",
		TargetDirectory: filepath.Join(output, "target"),
		BuildScripts:    true,
		ProcMacros:      true,
		Sysroot:         "discover",
		Threads:         2,
	}
}

func requireAnalyzer(t *testing.T) {
	t.Helper()
	testsupport.RequireRustAnalyzer(t)
}

// TestRunIndexesAWorkspaceWithoutWritingToIt is the contract the whole Rust
// path stands on: the analyzer runs build scripts, so the run must leave the
// sources exactly as it found them.
func TestRunIndexesAWorkspaceWithoutWritingToIt(t *testing.T) {
	requireAnalyzer(t)
	workspace, output := analyzerFixture(t)

	result, err := Run(context.Background(), defaultRunOptions(workspace, output))
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if result.ToolVersion == "" || len(result.Index.Documents) != 3 {
		t.Fatalf("result = tool %q documents %d", result.ToolVersion, len(result.Index.Documents))
	}
	if result.Duration <= 0 {
		t.Fatalf("duration = %s", result.Duration)
	}
	for _, forbidden := range []string{"target", "Cargo.lock"} {
		if _, err := os.Stat(filepath.Join(workspace, forbidden)); err == nil {
			t.Fatalf("the run created %q inside the repository", forbidden)
		} else if !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("stat %q error = %v", forbidden, err)
		}
	}
	if _, err := os.Stat(filepath.Join(output, "index.scip")); err != nil {
		t.Fatalf("index was not written to the output directory: %v", err)
	}

	// The generated configuration is what keeps cargo out of the sources.
	configuration, err := os.ReadFile(filepath.Join(output, "rust-analyzer.json"))
	if err != nil {
		t.Fatalf("read generated configuration: %v", err)
	}
	for _, want := range []string{"CARGO_TARGET_DIR", "CARGO_NET_OFFLINE", "--offline", "--locked"} {
		if !strings.Contains(string(configuration), want) {
			t.Fatalf("configuration = %s, want %q", configuration, want)
		}
	}
}

func TestRunClassifiesAMissingAnalyzer(t *testing.T) {
	workspace, output := analyzerFixture(t)
	options := defaultRunOptions(workspace, output)
	options.AnalyzerCommand = "ladygraph-rust-analyzer-that-is-not-installed"

	_, err := Run(context.Background(), options)
	var runErr *RunError
	if !errors.As(err, &runErr) || runErr.Kind != RunErrorAnalyzerUnavailable {
		t.Fatalf("Run() error = %v, want %s", err, RunErrorAnalyzerUnavailable)
	}
}

func TestRunRefusesToWriteInsideTheRepository(t *testing.T) {
	workspace, output := analyzerFixture(t)

	tests := map[string]func(options *RunOptions){
		"output inside the repository": func(options *RunOptions) {
			options.OutputDirectory = filepath.Join(workspace, "out")
		},
		"target inside the repository": func(options *RunOptions) {
			options.TargetDirectory = filepath.Join(workspace, "target")
		},
		"relative output": func(options *RunOptions) {
			options.OutputDirectory = "out"
		},
		"empty command": func(options *RunOptions) {
			options.AnalyzerCommand = "  "
		},
		"features and all features": func(options *RunOptions) {
			options.AllFeatures = true
			options.Features = []string{"serde"}
		},
	}
	for name, mutate := range tests {
		t.Run(name, func(t *testing.T) {
			options := defaultRunOptions(workspace, output)
			mutate(&options)
			_, err := Run(context.Background(), options)
			var runErr *RunError
			if !errors.As(err, &runErr) || runErr.Kind != RunErrorInvalidOptions {
				t.Fatalf("Run() error = %v, want %s", err, RunErrorInvalidOptions)
			}
		})
	}
}

func TestRunStopsWithTheContext(t *testing.T) {
	requireAnalyzer(t)
	workspace, output := analyzerFixture(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := Run(ctx, defaultRunOptions(workspace, output))
	var runErr *RunError
	if !errors.As(err, &runErr) || runErr.Kind != RunErrorCanceled {
		t.Fatalf("Run() error = %v, want %s", err, RunErrorCanceled)
	}
}

// TestClassifyDiagnosticsKeepsWhatExplainsAMissingCrate separates the
// analyzer's progress log, which says nothing, from the warning that explains
// why a crate resolved no dependencies.
func TestClassifyDiagnosticsKeepsWhatExplainsAMissingCrate(t *testing.T) {
	output := strings.Join([]string{
		"Generating SCIP start...",
		"rust-analyzer: Loading cargo metadata: started",
		"2026-08-11T21:57:13Z ERROR Config Error(s) error_sink=ConfigErrors([])",
		"2026-08-11T21:57:13Z  WARN `cargo metadata` failed and returning succeeded result with `--no-deps`",
		"   6: <std::thread::lifecycle::spawn_unchecked>",
		"Generating SCIP finished 2.7s",
	}, "\n")

	diagnostics := classifyDiagnostics(output)
	if len(diagnostics) != 1 || !strings.Contains(diagnostics[0], "cargo metadata` failed") {
		t.Fatalf("diagnostics = %#v", diagnostics)
	}
}

func TestValidateIndexRefusesAnIndexWithoutBodies(t *testing.T) {
	requireAnalyzer(t)
	workspace, output := analyzerFixture(t)
	result, err := Run(context.Background(), defaultRunOptions(workspace, output))
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}

	if err := validateIndex(result.Index); err != nil {
		t.Fatalf("validateIndex() rejected a real index: %v", err)
	}

	// The documents share their backing arrays with the decoded index, so
	// the degraded copy is built rather than mutated in place.
	stripped := result.Index
	stripped.Documents = make([]scipwire.Document, len(result.Index.Documents))
	for documentIndex, document := range result.Index.Documents {
		copied := document
		copied.Occurrences = make([]scipwire.Occurrence, len(document.Occurrences))
		for occurrenceIndex, occurrence := range document.Occurrences {
			occurrence.EnclosingRange.Present = false
			copied.Occurrences[occurrenceIndex] = occurrence
		}
		stripped.Documents[documentIndex] = copied
	}
	if err := validateIndex(stripped); err == nil {
		t.Fatal("validateIndex() accepted an index no reference could be attributed in")
	}
}

// TestClassifyDiagnosticsKeepsDuplicateSymbolReports covers the one report the
// analyzer writes without a level prefix: duplicate symbols are what make a
// lookup ambiguous, and a filter on WARN or ERROR used to drop them.
func TestClassifyDiagnosticsKeepsDuplicateSymbolReports(t *testing.T) {
	output := strings.Join([]string{
		"Generating SCIP start...",
		"Encountered duplicate scip symbols, indicating an internal rust-analyzer bug.",
		"src/lib.rs:3:0-5:1",
		"  Duplicate symbol: rust-analyzer cargo probe 0.1.0 impl#[Widget]",
		"Generating SCIP finished 1.0s",
	}, "\n")

	diagnostics := classifyDiagnostics(output)
	if len(diagnostics) != 2 {
		t.Fatalf("diagnostics = %#v, want the banner and the symbol", diagnostics)
	}
	if !strings.Contains(diagnostics[1], "impl#[Widget]") {
		t.Fatalf("diagnostics = %#v", diagnostics)
	}
}

// TestResolveAnalyzerPrefersTheBundledBinary is the rule an installation
// depends on: a bundle ships its own engine, and two machines with the same
// bundle must index with the same one whatever their PATH holds.
func TestResolveAnalyzerPrefersTheBundledBinary(t *testing.T) {
	if _, source, err := ResolveAnalyzer("rust-analyzer"); err == nil {
		// The test binary runs from a directory with no sibling analyzer, so
		// a resolution here can only have come from the PATH.
		if source != AnalyzerPath {
			t.Fatalf("source = %q, want %q", source, AnalyzerPath)
		}
	}

	directory := testsupport.TempDir(t)
	explicit := filepath.Join(directory, "ladygraph-fake-analyzer")
	if err := os.WriteFile(explicit, []byte("#!/bin/sh\necho fake\n"), 0o755); err != nil {
		t.Fatalf("write fake analyzer: %v", err)
	}
	resolved, source, err := ResolveAnalyzer(explicit)
	if err != nil {
		t.Fatalf("ResolveAnalyzer() error = %v", err)
	}
	if resolved != explicit || source != AnalyzerExplicit {
		t.Fatalf("resolved = %q from %q", resolved, source)
	}
	if _, _, err := ResolveAnalyzer("  "); err == nil {
		t.Fatal("an empty command must not resolve")
	}
	if _, _, err := ResolveAnalyzer("ladygraph-analyzer-that-does-not-exist"); err == nil {
		t.Fatal("a missing command must not resolve")
	}
}
