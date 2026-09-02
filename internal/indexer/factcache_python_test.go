package indexer

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Luqueee/kivgraph/internal/testsupport"
)

// TestPythonProducerFingerprintIsStableWhenAbsent is the last of the three
// producer fingerprints to get this rule, and the one whose defect was the
// oldest.
//
// The Python worker is a file this repository ships, so a missing one was read
// as "something is broken" and fingerprinted with a timestamp. But
// ProducerFile also returns nothing when `python3` is simply not installed,
// which is ordinary -- and the analyzer fingerprint is a single value for the
// whole pass, so that timestamp switched the fact cache off for **every**
// language on any machine without Python.
//
// Java and C# had the same shape and were fixed first. `binary` below is
// deliberately still a timestamp: os.Executable failing means the identity
// could not be read, not that there is none, and serving one build's facts to
// another is what the hash exists to prevent.
func TestPythonProducerFingerprintIsStableWhenAbsent(t *testing.T) {
	// A working directory with no `python-worker/` and a PATH with no
	// interpreter is a machine with no Python producer.
	t.Setenv("PATH", testsupport.TempDir(t))
	options := FullOptions{
		PythonIndexer:    "kivgraph-python-worker",
		PythonPath:       "python3",
		WorkingDirectory: testsupport.TempDir(t),
	}

	absent := pythonProducerFingerprint(options)
	if absent != pythonProducerFingerprint(options) {
		t.Fatal("the Python producer fingerprint changes between two reads when no producer resolves, so no cache entry of any language can ever be served")
	}
	if absent == "" {
		t.Fatal("an absent producer produced an empty fingerprint, which cannot be told from an unset field")
	}
}

// TestAnalyzerFingerprintIsStableWithoutAnyAnalyzer is the property all three
// fixes exist for, stated over the value that actually decides whether an
// entry is served. It is the assertion an earlier version of the Java test
// tried to make and could not, because the Python branch was still unstable.
func TestAnalyzerFingerprintIsStableWithoutAnyAnalyzer(t *testing.T) {
	t.Setenv("PATH", testsupport.TempDir(t))
	options := FullOptions{
		PythonIndexer:        "kivgraph-python-worker",
		PythonPath:           "python3",
		JavaIndexerCommand:   "scip-java",
		CSharpIndexerCommand: "scip-dotnet",
		WorkingDirectory:     testsupport.TempDir(t),
	}
	first := analyzerFingerprint(options)
	if second := analyzerFingerprint(options); first != second {
		t.Fatal("the analyzer fingerprint changes between two reads on a machine with no analyzers installed, so the fact cache is off for every language")
	}
}

// TestGoEnvironmentFingerprintIsStableWithoutTheToolchain is the highest-cost
// instance of the same defect, and the least obvious.
//
// Kivgraph is published as a binary. A user indexing a Java or a TypeScript
// repository has no reason to install Go, `go env` then fails, and the
// timestamp that failure produced switched the fact cache off for every
// language on every pass. Nothing about that is visible: a pass that should
// have been warm is simply slow.
//
// A present but failing `go` is a separate observation error because its
// toolchain identity could not be read rather than being absent.
func TestGoEnvironmentFingerprintIsStableWithoutTheToolchain(t *testing.T) {
	t.Setenv("PATH", testsupport.TempDir(t))
	absent, err := goEnvironmentFingerprint()
	if err != nil {
		t.Fatalf("go environment fingerprint with PATH=%q: %v", os.Getenv("PATH"), err)
	}
	again, err := goEnvironmentFingerprint()
	if err != nil {
		t.Fatalf("second go environment fingerprint with PATH=%q: %v", os.Getenv("PATH"), err)
	}
	if absent != again {
		t.Fatal("the Go environment fingerprint changes between two reads with no toolchain installed, so the fact cache is off for every language")
	}
	if absent != "absent" {
		t.Errorf("fingerprint = %q, want a stable value naming the absence", absent)
	}
}

func TestGoEnvironmentFingerprintPreservesEmptyValuesAndPositions(t *testing.T) {
	first, err := goEnvironmentFingerprintFromOutput([]byte("go1.24\n/root\n\n/mod\n/path\n\n"))
	if err != nil {
		t.Fatalf("goEnvironmentFingerprintFromOutput() error = %v", err)
	}
	second, err := goEnvironmentFingerprintFromOutput([]byte("go1.24\n\n/root\n/mod\n/path\n\n"))
	if err != nil {
		t.Fatalf("second goEnvironmentFingerprintFromOutput() error = %v", err)
	}
	if first == second {
		t.Fatalf("fingerprint = %q for different environment variable positions", first)
	}
	if !strings.Contains(first, "GOFLAGS=0:\x00") || !strings.Contains(first, "GOPRIVATE=0:\x00") {
		t.Fatalf("fingerprint = %q, want named empty values", first)
	}
}

func TestGoEnvironmentFingerprintRejectsIncompleteOutput(t *testing.T) {
	if _, err := goEnvironmentFingerprintFromOutput([]byte("go1.24\n/root\n")); err == nil || !strings.Contains(err.Error(), "got 2 values, want 6") {
		t.Fatalf("goEnvironmentFingerprintFromOutput() error = %v, want value count refusal", err)
	}
}

// TestFileFingerprintAlreadyNamedAbsence records where the vocabulary comes
// from. fileFingerprint has always answered `absent` for a file that is not
// there, and typeScriptWorkerFingerprint `unavailable` for a worker it cannot
// resolve; the three producer branches were the ones that diverged from a rule
// their own file already stated.
func TestFileFingerprintAlreadyNamedAbsence(t *testing.T) {
	missing := filepath.Join(testsupport.TempDir(t), "not-there")
	first := fileFingerprint(missing)
	if first != "absent" {
		t.Errorf("fileFingerprint of a missing file = %q, want \"absent\"", first)
	}
	// Two separate calls, held in two variables: the point is that the value
	// repeats, and writing it as one expression compared with itself is both
	// unreadable and an SA4000.
	second := fileFingerprint(missing)
	if first != second {
		t.Errorf("fileFingerprint is not stable for a missing file: %q then %q", first, second)
	}
}

// TestPythonProducerFingerprintFollowsTheProducerThatRuns keeps the original
// contract that e0d8d52 established: the key is the content of the script the
// loader resolves, so editing it forces a re-observation.
func TestPythonProducerFingerprintFollowsTheProducerThatRuns(t *testing.T) {
	root := testsupport.TempDir(t)
	script := filepath.Join(root, "python-worker", "index.py")
	if err := os.MkdirAll(filepath.Dir(script), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(script, []byte("print('one')\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	options := FullOptions{
		PythonIndexer:    "kivgraph-python-worker",
		PythonPath:       "python3",
		WorkingDirectory: root,
	}
	present := pythonProducerFingerprint(options)
	if present == "absent" {
		t.Skip("python3 is not installed, so the producer cannot resolve here")
	}
	if err := os.WriteFile(script, []byte("print('two')\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if pythonProducerFingerprint(options) == present {
		t.Error("editing the worker the pass runs did not change the fingerprint")
	}
}
