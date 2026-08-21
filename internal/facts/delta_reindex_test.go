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
		// The subject keeps its identity and its body changes, so every
		// evidence key in the file moves while every stable key must not.
		//
		// This is the LUQUE-2002 regression. Growing this body replaces
		// geometry.go and nothing else, and the six edges from `units/` into
		// it are anchored on `units/` files whose own facts did not change --
		// so nothing restates them. Retirement used to delete every edge
		// touching a replaced file's symbols, incoming ones included, and all
		// six went. ADR 0056 narrowed it: a symbol the Upsert restates keeps
		// its node, so those edges are never deleted to begin with.
		"a body grows a line": func(t *testing.T, root string) {
			rewrite(t, filepath.Join(root, "geometry.go"),
				"func (circle Circle) Area() float64 { return 3.14159 * circle.Radius * circle.Radius }",
				"func (circle Circle) Area() float64 {\n\treturn 3.14159 * circle.Radius * circle.Radius\n}")
		},
		// The other direction: a symbol really does disappear, so the edges
		// into it must go -- and the files that used it are replaced too,
		// because their own fact sets shrank.
		"a function other packages use disappears": func(t *testing.T, root string) {
			rewrite(t, filepath.Join(root, "geometry.go"),
				"func Measure(shape Shape) float64 { return shape.Area() }", "")
			rewrite(t, filepath.Join(root, "units", "handlers.go"),
				"var measurer = geometry.Measure", "var measurer = func(geometry.Shape) float64 { return 0 }")
			rewrite(t, filepath.Join(root, "units", "handlers.go"),
				"\treturn geometry.Measure\n", "\treturn measurer\n")
		},
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
