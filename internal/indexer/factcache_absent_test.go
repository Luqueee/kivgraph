package indexer

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/Luqueee/kivgraph/internal/testsupport"
)

// TestJavaIndexerFingerprintIsStableWhenAbsent is the regression for a defect
// that could only fail on a machine without scip-java -- which is nearly every
// machine, and was not this one.
//
// The fingerprint wrote `time.Now().UnixNano()` when the indexer did not
// resolve, copying the rule the Python worker uses. That rule is defensible
// there and wrong here: the Python producer is a file this repository ships,
// so failing to find it means something is broken; scip-java is a third-party
// tool most machines do not have, and a repository that declares no Java never
// asks for it.
//
// The consequence was not a Java problem. The analyzer fingerprint is one
// value for the whole pass, so a timestamp in it changed on every call and
// **no entry of any language could ever be served**: the fact cache was off,
// silently, everywhere scip-java was absent. CI reported it as five failing
// cache tests that have nothing to do with Java.
//
// This measures `javaIndexerFingerprint` rather than the whole hash, and that
// is deliberate. An earlier version asserted on `analyzerFingerprint` and
// failed on macOS for a different contributor's absence -- the Python
// worker's -- which is a real question and not this one. A test that cannot
// say which input made it fail is a test that gets deleted.
func TestJavaIndexerFingerprintIsStableWhenAbsent(t *testing.T) {
	// A directory with nothing in it is a machine with no indexer.
	t.Setenv("PATH", testsupport.TempDir(t))

	options := FullOptions{JavaIndexerCommand: "scip-java"}
	absent := javaIndexerFingerprint(options)
	if absent != javaIndexerFingerprint(options) {
		t.Fatal("the Java indexer fingerprint changes between two reads when the indexer is absent, so no cache entry can ever be served")
	}
	if absent == "" {
		t.Fatal("an absent indexer produced an empty fingerprint, which cannot be told from an unset field")
	}

	// Installing one still has to invalidate what a machine without it wrote.
	directory := testsupport.TempDir(t)
	if err := os.WriteFile(filepath.Join(directory, "scip-java"),
		[]byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", directory)
	present := javaIndexerFingerprint(options)
	if present == absent {
		t.Error("an installed indexer produced the same fingerprint as an absent one")
	}
	if present != javaIndexerFingerprint(options) {
		t.Error("the fingerprint of an installed indexer is not stable")
	}

	// And upgrading it in place has to invalidate too: the same path with
	// different content is a different producer.
	if err := os.WriteFile(filepath.Join(directory, "scip-java"),
		[]byte("#!/bin/sh\necho upgraded\nexit 0\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	if javaIndexerFingerprint(options) == present {
		t.Error("an upgraded indexer produced the same fingerprint as the old one")
	}
}
