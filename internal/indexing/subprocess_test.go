package indexing

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Luqueee/kivgraph/internal/testsupport"
)

func TestRunDetachedForwardsProgressAndReturnsTheReport(t *testing.T) {
	executable, _ := fakeIndexChild(t, fakeChildScript{Stdout: []string{
		`{"event":"progress","progress":{"phase":"go","repository":"one","completed":1,"total":2}}`,
		`{"event":"progress","progress":{"phase":"rust","repository":"two","completed":2,"total":2}}`,
		`{"event":"result","result":{"passed":true,"generation_id":"000054","counts":{"symbols":7},"index":{"rust_symbols":3}}}`,
	}})

	var reported []ProjectProgress
	document, err := RunDetached(context.Background(), DetachedOptions{
		Executable: executable,
		Progress:   func(update ProjectProgress) { reported = append(reported, update) },
	})
	if err != nil {
		t.Fatalf("RunDetached() error = %v", err)
	}
	if !document.Passed || document.GenerationID != "000054" {
		t.Fatalf("document = %#v, want the published generation", document)
	}
	if document.Counts.Symbols != 7 || document.Index.RustSymbols != 3 {
		t.Fatalf("document counts = %#v, want the child's counts", document)
	}
	if len(reported) != 2 {
		t.Fatalf("reported %d progress steps, want 2: a client that sees no sign of life cancels the call", len(reported))
	}
	if reported[0].Phase != "go" || reported[0].Repository != "one" || reported[0].Total != 2 {
		t.Fatalf("first progress = %#v, want the child's own step", reported[0])
	}
}

// TestRunDetachedPassesTheConfigurationToTheChild fixes what the child is told.
// The child reads the registry from disk, so a parent that forgot to name its
// configuration would index a different graph than the one it serves -- and on
// a temporary configuration, would publish into the real state directory.
func TestRunDetachedPassesTheConfigurationToTheChild(t *testing.T) {
	executable, argumentsFile := fakeIndexChild(t, fakeChildScript{Stdout: []string{
		`{"event":"result","result":{"passed":true,"generation_id":"000001"}}`,
	}})

	if _, err := RunDetached(context.Background(), DetachedOptions{
		Executable:       executable,
		ConfigPath:       "/state/config.yaml",
		RepositoriesPath: "/state/repositories.yaml",
		ResolverVersion:  "9.9.9",
	}); err != nil {
		t.Fatalf("RunDetached() error = %v", err)
	}
	recorded, err := os.ReadFile(argumentsFile)
	if err != nil {
		t.Fatalf("read recorded arguments: %v", err)
	}
	arguments := strings.Fields(string(recorded))
	want := []string{
		"index", "--full", "--json",
		"--config", "/state/config.yaml",
		"--repositories", "/state/repositories.yaml",
		"--resolver-version", "9.9.9",
	}
	if len(arguments) != len(want) {
		t.Fatalf("arguments = %v, want %v", arguments, want)
	}
	for index := range want {
		if arguments[index] != want[index] {
			t.Fatalf("arguments = %v, want %v", arguments, want)
		}
	}
}

// TestRunDetachedOmitsFlagsItWasNotGiven keeps the child on its own defaults:
// an empty value is not a value, and passing `--config ""` would make the child
// resolve a path from an empty string instead of using the default location.
func TestRunDetachedOmitsFlagsItWasNotGiven(t *testing.T) {
	executable, argumentsFile := fakeIndexChild(t, fakeChildScript{Stdout: []string{
		`{"event":"result","result":{"passed":true}}`,
	}})

	if _, err := RunDetached(context.Background(), DetachedOptions{Executable: executable}); err != nil {
		t.Fatalf("RunDetached() error = %v", err)
	}
	recorded, err := os.ReadFile(argumentsFile)
	if err != nil {
		t.Fatalf("read recorded arguments: %v", err)
	}
	if got := strings.Fields(string(recorded)); len(got) != 3 {
		t.Fatalf("arguments = %v, want only the subcommand and --json", got)
	}
}

// TestRunDetachedPrefersTheChildsOwnReason keeps the useful half of a failure.
// The child knows why it stopped; the exit code only says that it did.
func TestRunDetachedPrefersTheChildsOwnReason(t *testing.T) {
	executable, _ := fakeIndexChild(t, fakeChildScript{
		Stdout: []string{
			`{"event":"result","result":{"passed":false,"error":"rebuild graph did not pass its gates"}}`,
		},
		ExitCode: 1,
	})

	document, err := RunDetached(context.Background(), DetachedOptions{Executable: executable})
	if !errors.Is(err, ErrIndexProcess) {
		t.Fatalf("RunDetached() error = %v, want ErrIndexProcess", err)
	}
	if !strings.Contains(err.Error(), "did not pass its gates") {
		t.Fatalf("error = %v, want the child's own reason", err)
	}
	if document.Passed {
		t.Fatal("document reported a pass that the child said failed")
	}
}

// TestRunDetachedRejectsAPassThatPublishedNothing is the case that must never
// be mistaken for success: the child exited zero without reporting a
// generation, so nothing was published and the caller must not report one.
func TestRunDetachedRejectsAPassThatPublishedNothing(t *testing.T) {
	executable, _ := fakeIndexChild(t, fakeChildScript{
		Stderr: []string{"the analyzer could not start"},
	})

	if _, err := RunDetached(context.Background(), DetachedOptions{Executable: executable}); err == nil {
		t.Fatal("RunDetached() error = nil, want a failure: no report means no generation")
	} else if !strings.Contains(err.Error(), "no report") ||
		!strings.Contains(err.Error(), "analyzer could not start") {
		t.Fatalf("error = %v, want the missing report and what the child said", err)
	}
}

// TestRunDetachedIgnoresLinesItDoesNotUnderstand keeps the stream extensible:
// a server built before an event kind existed must still read the result.
func TestRunDetachedIgnoresLinesItDoesNotUnderstand(t *testing.T) {
	executable, _ := fakeIndexChild(t, fakeChildScript{Stdout: []string{
		`not json at all`,
		`{"event":"invented","payload":{}}`,
		`{"event":"result","result":{"passed":true,"generation_id":"000002"}}`,
	}})

	document, err := RunDetached(context.Background(), DetachedOptions{Executable: executable})
	if err != nil {
		t.Fatalf("RunDetached() error = %v", err)
	}
	if document.GenerationID != "000002" {
		t.Fatalf("document = %#v, want the result that followed the unknown lines", document)
	}
}

func TestRunDetachedReportsAnExecutableItCannotRun(t *testing.T) {
	missing := filepath.Join(testsupport.TempDir(t), "absent")
	if _, err := RunDetached(context.Background(), DetachedOptions{Executable: missing}); !errors.Is(err, ErrIndexProcess) {
		t.Fatalf("RunDetached() error = %v, want ErrIndexProcess naming the executable", err)
	}
}

// TestTailBufferKeepsTheEnd defends the bound on what a failure carries. A
// loader can print thousands of diagnostic lines, and the reason it stopped is
// the last of them.
func TestTailBufferKeepsTheEnd(t *testing.T) {
	buffer := &tailBuffer{limit: 8}
	for _, chunk := range []string{"aaaa", "bbbb", "cccc"} {
		if _, err := buffer.Write([]byte(chunk)); err != nil {
			t.Fatalf("Write() error = %v", err)
		}
	}
	if got := buffer.String(); got != "bbbbcccc" {
		t.Fatalf("String() = %q, want the last 8 bytes", got)
	}
}

// TestRunDetachedGivesTheChildNoStdin is the hazard this boundary introduced.
// A server's stdin is the MCP stream: a child that inherited it would read the
// client's next request and lose it. The child reports what it read, so the
// assertion is on the observed end of the pipe and not on the wiring.
func TestRunDetachedGivesTheChildNoStdin(t *testing.T) {
	executable, _ := fakeIndexChild(t, fakeChildScript{Stdout: []string{
		`{"event":"result","result":{"passed":true,"generation_id":"` + stdinPlaceholder + `"}}`,
	}})

	document, err := RunDetached(context.Background(), DetachedOptions{Executable: executable})
	if err != nil {
		t.Fatalf("RunDetached() error = %v", err)
	}
	if document.GenerationID != "none" {
		t.Fatalf("child read %q from stdin, want nothing: a server's stdin is the client's request stream",
			document.GenerationID)
	}
}

// TestRunDetachedMirrorsTheChildLog keeps the loader's own diagnostics readable.
// A pass reports what it could not load without failing, and a diagnostic that
// only reaches a discarded buffer is a diagnostic nobody has.
func TestRunDetachedMirrorsTheChildLog(t *testing.T) {
	executable, _ := fakeIndexChild(t, fakeChildScript{
		Stderr: []string{`{"level":"WARN","msg":"module not loaded"}`},
		Stdout: []string{`{"event":"result","result":{"passed":true,"generation_id":"000001"}}`},
	})

	log := &strings.Builder{}
	if _, err := RunDetached(context.Background(), DetachedOptions{
		Executable: executable,
		Log:        log,
	}); err != nil {
		t.Fatalf("RunDetached() error = %v", err)
	}
	if !strings.Contains(log.String(), "module not loaded") {
		t.Fatalf("log = %q, want the child's own record", log.String())
	}
}
