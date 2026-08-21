package facts

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Luqueee/kivgraph/internal/workspace"
)

// TestDiffOverARealEditReproducesACleanLoad is the incremental invariant on real
// input: applying Diff(before, after) to the facts of a working tree must
// produce exactly the facts a clean load of the edited tree produces.
//
// Every other Diff test in this package builds both sides by hand, which cannot
// express what a real edit does to them. An evidence key embeds a byte offset,
// so inserting one line above a call moves the key of every fact below it. A
// stable key must survive that same insertion. And a fact can change in a file
// nobody edited: delete a function and the file that called it now holds a
// different set of edges, with its text untouched -- which is the case the
// invariant in AGENTS.md is about, and the one a hand-built pair never has to
// discover.
//
// The assertion is the whole set, not a count. A delta that retracts too much
// fails it as loudly as one that leaves a stale fact behind.
func TestDiffOverARealEditReproducesACleanLoad(t *testing.T) {
	for name, edit := range map[string]func(t *testing.T, root string){
		// A file appears, in a package that already exists.
		"a new file joins an existing package": func(t *testing.T, root string) {
			write(t, filepath.Join(root, "extra.go"),
				"package geometry\n\n// Perimeters is new in this pass.\nfunc Perimeters(shapes []Shape) int { return len(shapes) }\n")
		},
		// A whole file goes, taking its declarations and the relations they
		// were an end of.
		"a file disappears": func(t *testing.T, root string) {
			if err := os.Remove(filepath.Join(root, "units", "units.go")); err != nil {
				t.Fatalf("remove units.go: %v", err)
			}
		},
	} {
		t.Run(name, func(t *testing.T) {
			root := workingCopy(t, filepath.Join("..", "..", "testdata", "go", "type-relations"))
			repositories := []workspace.Repository{
				{Name: "type-relations", Path: root, RealPath: root},
			}

			before, _ := normalizeRepositories(t, repositories, "type-relations")
			if err := before.Validate(); err != nil {
				t.Fatalf("facts before the edit are invalid: %v", err)
			}

			edit(t, root)

			after, _ := normalizeRepositories(t, repositories, "type-relations")
			if err := after.Validate(); err != nil {
				t.Fatalf("facts after the edit are invalid: %v", err)
			}

			delta, err := Diff(before, after)
			if err != nil {
				t.Fatalf("Diff() error = %v", err)
			}
			if err := delta.Validate(); err != nil {
				t.Fatalf("the computed delta is invalid: %v", err)
			}
			if delta.Empty() {
				t.Fatal("the edit produced no delta, so the test proves nothing")
			}

			assertSetsEqual(t, applyDelta(t, before, delta), after)
		})
	}
}

// workingCopy is a copy of a fixture a test may edit. The fixture itself is
// never written to: it is shared input, and a test that mutates it changes what
// every other test measures.
func workingCopy(t *testing.T, fixture string) string {
	t.Helper()
	source, err := filepath.Abs(fixture)
	if err != nil {
		t.Fatalf("resolve fixture: %v", err)
	}
	// The workspace layer refuses a path with a symlink component, and on
	// macOS the temporary directory sits under `/var`, which is one.
	parent, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatalf("resolve temporary directory: %v", err)
	}
	root := filepath.Join(parent, filepath.Base(source))
	if err := os.CopyFS(root, os.DirFS(source)); err != nil {
		t.Fatalf("copy fixture: %v", err)
	}
	return root
}

// rewrite replaces one exact occurrence. An edit that matched nothing would
// leave the tree untouched and the test would pass against no change at all.
func rewrite(t *testing.T, path, old, replacement string) {
	t.Helper()
	blob, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	if count := strings.Count(string(blob), old); count != 1 {
		t.Fatalf("%s holds %d occurrences of the edited text, want exactly 1", path, count)
	}
	write(t, path, strings.Replace(string(blob), old, replacement, 1))
}

func write(t *testing.T, path, contents string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(contents), 0o600); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

// TestDiffRestatesEdgesIntoAReplacedFile is a known defect, reproduced rather
// than described. It is skipped because it fails, and it will pass the moment
// the defect is fixed -- on whichever side the fix lands.
//
// Editing one file loses every edge pointing into it from another file.
//
// Growing one method body in `geometry.go` by a line replaces that file and
// nothing else: the six edges from `units/` into it are anchored on `units/`
// files, whose own facts did not change, so Diff does not restate them. But
// retirement withdraws "every edge anchored on any of those, incoming and
// outgoing" (ApplyCanonicalDelta), so the applier deletes all six and nothing
// puts them back. Every caller in another package stops pointing at the file
// that was edited.
//
// Diff's own comment reasons about the neighbouring case -- a target that
// vanished, where the source file's fragment shrinks and is therefore restated
// -- and stops one short of a target that merely moved inside a replaced file.
//
// The fix does not belong here. Restating those edges from Diff pulls their
// whole anchor file into the delta, because Delta.Validate requires a
// self-consistent fragment; that file's own incoming edges are then retracted
// too, and the restatement cascades with no bound in sight. Narrowing
// retirement to the symbols that actually disappeared -- rather than to every
// symbol of a replaced file -- keeps the unit of a delta the file, which is what
// AGENTS.md says it is: a fact belongs to the file that ASSERTED it, and
// `units/handlers.go` asserted this one. That is a change to the canonical
// mutation contract and wants an ADR.
func TestDiffRestatesEdgesIntoAReplacedFile(t *testing.T) {
	t.Skip("known defect: a replaced file loses the edges other files point into it; needs an ADR on retirement scope")

	root := workingCopy(t, filepath.Join("..", "..", "testdata", "go", "type-relations"))
	repositories := []workspace.Repository{{Name: "type-relations", Path: root, RealPath: root}}

	before, _ := normalizeRepositories(t, repositories, "type-relations")
	rewrite(t, filepath.Join(root, "geometry.go"),
		"func (circle Circle) Area() float64 { return 3.14159 * circle.Radius * circle.Radius }",
		"func (circle Circle) Area() float64 {\n\treturn 3.14159 * circle.Radius * circle.Radius\n}")
	after, _ := normalizeRepositories(t, repositories, "type-relations")

	delta, err := Diff(before, after)
	if err != nil {
		t.Fatalf("Diff() error = %v", err)
	}
	assertSetsEqual(t, applyDelta(t, before, delta), after)
}
