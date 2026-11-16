package indexer

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/Luqueee/kivgraph/internal/executable"
	"github.com/Luqueee/kivgraph/internal/testsupport"
)

// A worker built by `tsc` is a directory of modules: the entry script imports
// the rest, and a build that changes only those leaves the entry's own bytes
// alone. The cache has to notice, or facts the old worker produced keep being
// served -- measured once, with the fixed build swapped in place and the next
// pass answering the old symbol count from the cache for 33 of 39 units.
//
// The fixtures stand in for `node dist/facts-cli.js`: the worker command is a
// program the platform can run, and the script it is given is the entry.
type workerFixture struct {
	t       *testing.T
	root    string
	dist    string
	entry   string
	options FullOptions
}

func newWorkerFixture(t *testing.T) workerFixture {
	t.Helper()
	root := testsupport.TempDir(t)
	program := filepath.Join(root, executable.Name("node"))
	writeFullFixture(t, program, "#!/bin/sh\necho node\n")
	if err := os.Chmod(program, 0o700); err != nil {
		t.Fatalf("Chmod() error = %v", err)
	}
	fixture := workerFixture{t: t, root: root, dist: filepath.Join(root, "ts-worker", "dist")}
	fixture.entry = filepath.Join(fixture.dist, "facts-cli.js")
	writeFullFixture(t, fixture.entry, "import './symbol-extractor.js'\n")
	writeFullFixture(t, filepath.Join(fixture.dist, "symbol-extractor.js"), "export const one = 1\n")
	fixture.options = FullOptions{TypeScriptWorker: program + " " + fixture.entry}
	return fixture
}

func (fixture workerFixture) write(relative, contents string) {
	fixture.t.Helper()
	writeFullFixture(fixture.t, filepath.Join(fixture.dist, filepath.FromSlash(relative)), contents)
}

func (fixture workerFixture) fingerprint() string {
	fixture.t.Helper()
	value := typeScriptWorkerFingerprint(fixture.options)
	if value == "unavailable" {
		fixture.t.Fatal("the worker fixture was not resolvable, so the test would read a constant")
	}
	return value
}

func TestWorkerIdentityFollowsEveryModuleOfTheBuild(t *testing.T) {
	for name, change := range map[string]func(workerFixture){
		"an imported module is edited": func(f workerFixture) {
			f.write("symbol-extractor.js", "export const one = 2\n")
		},
		"a module is added": func(f workerFixture) {
			f.write("reference-extractor.js", "export const added = 1\n")
		},
		"a module is added below a directory": func(f workerFixture) {
			f.write("nested/deeper/helper.mjs", "export const nested = 1\n")
		},
		"a module is removed": func(f workerFixture) {
			if err := os.Remove(filepath.Join(f.dist, "symbol-extractor.js")); err != nil {
				f.t.Fatal(err)
			}
		},
		"a module is renamed": func(f workerFixture) {
			if err := os.Rename(filepath.Join(f.dist, "symbol-extractor.js"), filepath.Join(f.dist, "renamed.js")); err != nil {
				f.t.Fatal(err)
			}
		},
		"the entry is edited": func(f workerFixture) {
			f.write("facts-cli.js", "import './symbol-extractor.js'\n// edited\n")
		},
	} {
		t.Run(name, func(t *testing.T) {
			fixture := newWorkerFixture(t)
			before := fixture.fingerprint()
			change(fixture)
			if fixture.fingerprint() == before {
				t.Fatalf("the worker's identity ignored: %s", name)
			}
		})
	}
}

// What a build leaves in dist that no analysis reads must not throw the cache
// away: declarations, source maps and the compiled tests. Being wrong in this
// direction costs one analysis; the test is what keeps it from happening on every
// pass of someone who rebuilds often.
func TestWorkerIdentityIgnoresWhatTheAnalysisNeverRuns(t *testing.T) {
	fixture := newWorkerFixture(t)
	before := fixture.fingerprint()
	fixture.write("symbol-extractor.d.ts", "export declare const one: number;\n")
	fixture.write("symbol-extractor.js.map", "{\"version\":3}\n")
	fixture.write("symbol-extractor.test.js", "// a compiled test\n")
	fixture.write("nested/helper.test.mjs", "// a compiled test\n")
	fixture.write("README.txt", "notes\n")
	if fixture.fingerprint() != before {
		t.Fatal("the worker's identity followed a file the worker never loads")
	}
}

// The identity is the content and the layout of the build and nothing about
// when or where it was made.
func TestWorkerIdentityIsTheContentAndNotTheMomentOrThePlace(t *testing.T) {
	fixture := newWorkerFixture(t)
	before := fixture.fingerprint()

	writeFullFixture(t, filepath.Join(fixture.dist, "symbol-extractor.js"), "export const one = 1\n")
	if fixture.fingerprint() != before {
		t.Fatal("rewriting a module with the same bytes changed the identity")
	}

	moved := newWorkerFixture(t)
	if moved.fingerprint() != before {
		t.Fatal("the same build in another directory has another identity")
	}
}

// Only a build output directory is a module tree. A worker configured as a
// script somewhere else keeps the behaviour it always had, and its directory --
// which can be anything, a home directory included -- is not walked.
func TestWorkerIdentityDoesNotWalkADirectoryThatIsNotABuild(t *testing.T) {
	fixture := newWorkerFixture(t)
	other := filepath.Join(fixture.root, "scripts")
	entry := filepath.Join(other, "worker.js")
	writeFullFixture(t, entry, "console.log('worker')\n")
	writeFullFixture(t, filepath.Join(other, "sibling.js"), "console.log('one')\n")
	options := FullOptions{TypeScriptWorker: filepath.Join(fixture.root, executable.Name("node")) + " " + entry}
	before := typeScriptWorkerFingerprint(options)
	if before == "unavailable" {
		t.Fatal("the fixture was not resolvable")
	}

	writeFullFixture(t, filepath.Join(other, "sibling.js"), "console.log('two')\n")
	if typeScriptWorkerFingerprint(options) != before {
		t.Fatal("a directory that is not a build output was walked")
	}
	writeFullFixture(t, entry, "console.log('changed')\n")
	if typeScriptWorkerFingerprint(options) == before {
		t.Fatal("the entry script stopped counting")
	}
}

// A build directory that cannot be read is a fact of its own and not an empty
// one, or a worker whose modules vanished would keep the identity of one that
// had none.
func TestWorkerIdentityNamesABuildDirectoryThatIsGone(t *testing.T) {
	fixture := newWorkerFixture(t)
	before := fixture.fingerprint()
	if err := os.RemoveAll(fixture.dist); err != nil {
		t.Fatal(err)
	}
	after := typeScriptWorkerFingerprint(fixture.options)
	if after == before || after == "unavailable" {
		t.Fatalf("fingerprint of a missing build = %q, want a value that differs from the built one", after)
	}
}

// A module that is there and cannot be read is not an absent one, and not an
// unchanged one either: the identity must stop matching the build it had.
func TestWorkerIdentityNamesAModuleItCannotRead(t *testing.T) {
	testsupport.SkipWithoutModeBits(t)
	if os.Geteuid() == 0 {
		t.Skip("root reads a file whatever its mode")
	}
	fixture := newWorkerFixture(t)
	before := fixture.fingerprint()
	if err := os.Chmod(filepath.Join(fixture.dist, "symbol-extractor.js"), 0o000); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(filepath.Join(fixture.dist, "symbol-extractor.js"), 0o600) })
	after := typeScriptWorkerFingerprint(fixture.options)
	if after == before || after == "unavailable" {
		t.Fatalf("fingerprint with an unreadable module = %q, want a value of its own", after)
	}
}
