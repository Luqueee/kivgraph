package indexer

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/Luqueee/kivgraph/internal/testsupport"
)

// TestCSharpIndexerFingerprintIsStableWhenAbsent is the C# half of what
// TestJavaIndexerFingerprintIsStableWhenAbsent pins, and it exists because the
// defect it guards is a property of the shape rather than of Java: an absent
// third-party indexer that fingerprints as a timestamp turns the one analyzer
// fingerprint of the whole pass into a value that never repeats, so no entry
// of any language can ever be served.
//
// It measures the C# contribution alone rather than the whole hash. Asserting
// on the sum makes a failure say "something is unstable" without saying which
// input, and the first version of the Java test failed on macOS for the Python
// worker's absence -- a real question, and not this one.
func TestCSharpIndexerFingerprintIsStableWhenAbsent(t *testing.T) {
	t.Setenv("PATH", testsupport.TempDir(t))

	options := FullOptions{CSharpIndexerCommand: "scip-dotnet"}
	absent := cSharpIndexerFingerprint(options)
	if absent != cSharpIndexerFingerprint(options) {
		t.Fatal("the C# indexer fingerprint changes between two reads when the indexer is absent, so no cache entry can ever be served")
	}
	if absent == "" {
		t.Fatal("an absent indexer produced an empty fingerprint, which cannot be told from an unset field")
	}

	directory := testsupport.TempDir(t)
	if err := os.WriteFile(filepath.Join(directory, "scip-dotnet"),
		[]byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", directory)
	present := cSharpIndexerFingerprint(options)
	if present == absent {
		t.Error("an installed indexer produced the same fingerprint as an absent one")
	}
	if present != cSharpIndexerFingerprint(options) {
		t.Error("the fingerprint of an installed indexer is not stable")
	}

	if err := os.WriteFile(filepath.Join(directory, "scip-dotnet"),
		[]byte("#!/bin/sh\necho upgraded\nexit 0\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	if cSharpIndexerFingerprint(options) == present {
		t.Error("an upgraded indexer produced the same fingerprint as the old one")
	}
}
