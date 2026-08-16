package tools

import (
	"strings"
	"testing"
	"time"

	"github.com/Luqueee/kivgraph/internal/hotsnapshot"
)

// TestSymbolSelectorNarrowsAnAmbiguousNameAndNeverGuesses is the whole reason a
// list response can stop shipping stable keys. The triple has to resolve when it
// is unique, narrow when it is not, and refuse when even the narrowing leaves
// two candidates -- because choosing one would be a nominal coincidence, which
// this project forbids in the graph and which is worth no more in the surface.
func TestSymbolSelectorNarrowsAnAmbiguousNameAndNeverGuesses(t *testing.T) {
	snapshot := selectorSnapshot(t)

	// Unique in one repository but not in the graph: the message names the
	// candidates by where they are, because that is the narrowing the caller can
	// express next.
	_, err := resolveSymbolSelector(snapshot, symbolSelector{QualifiedName: "pkg.Merge"})
	if err == nil {
		t.Fatal("resolveSymbolSelector(ambiguous) error = nil, want an ambiguity")
	}
	message := err.Error()
	for _, want := range []string{"names 2 symbols", "alpha src/set.go:10-20", "beta src/set.go:5-9"} {
		if !strings.Contains(message, want) {
			t.Fatalf("ambiguity message = %q, want it to contain %q", message, want)
		}
	}

	// The repository is enough to separate them.
	id, err := resolveSymbolSelector(snapshot, symbolSelector{Repository: "alpha", QualifiedName: "pkg.Merge"})
	if err != nil {
		t.Fatalf("resolveSymbolSelector(narrowed) error = %v", err)
	}
	symbol, found := snapshot.Symbol(id)
	if !found || symbol.StableKey != "alpha-merge" {
		t.Fatalf("narrowed selector resolved to %#v, want alpha-merge", symbol)
	}

	// Two homonyms in the same file leave nothing but the key, and the message
	// says so instead of repeating a narrowing that cannot help.
	_, err = resolveSymbolSelector(snapshot, symbolSelector{
		Repository: "alpha", Path: "src/twins.go", QualifiedName: "pkg.Twin",
	})
	if err == nil {
		t.Fatal("resolveSymbolSelector(homonyms) error = nil, want an ambiguity")
	}
	message = err.Error()
	if !strings.Contains(message, "only a stable_key separates them") ||
		!strings.Contains(message, "alpha-twin-a") || !strings.Contains(message, "alpha-twin-b") {
		t.Fatalf("homonym message = %q, want the keys offered", message)
	}
}

func TestNormalizeSymbolSelectorRejectsContradictions(t *testing.T) {
	for name, selector := range map[string][4]string{
		"nothing":       {"", "", "", ""},
		"both":          {"key", "", "", "pkg.Merge"},
		"key narrowed":  {"key", "alpha", "", ""},
		"path alone":    {"", "", "src/set.go", "pkg.Merge"},
		"padded key":    {" key", "", "", ""},
		"padded name":   {"", "", "", "pkg.Merge "},
		"absolute path": {"", "alpha", "/src/set.go", "pkg.Merge"},
		"escaping path": {"", "alpha", "../set.go", "pkg.Merge"},
	} {
		if _, err := normalizeSymbolSelector(selector[0], selector[1], selector[2], selector[3]); err == nil {
			t.Fatalf("%s: normalizeSymbolSelector(%q, %q, %q, %q) error = nil, want a rejection",
				name, selector[0], selector[1], selector[2], selector[3])
		}
	}
	selector, err := normalizeSymbolSelector("", "alpha", "src/set.go/", "pkg.Merge")
	if err != nil {
		t.Fatalf("normalizeSymbolSelector(trailing slash) error = %v", err)
	}
	if selector.Path != "src/set.go" {
		t.Fatalf("normalized path = %q, want the trailing separator dropped", selector.Path)
	}
}

// selectorSnapshot holds one qualified name in two repositories and one name
// twice in the same file, which are the two ambiguities the selector has to
// tell apart.
func selectorSnapshot(t *testing.T) *hotsnapshot.GraphSnapshot {
	t.Helper()
	rows := hotsnapshot.LadybugSnapshotRows{
		Repositories: []hotsnapshot.RepositoryRow{
			{Key: "repo-alpha", Name: "alpha", Path: "/tmp/alpha", Languages: "go"},
			{Key: "repo-beta", Name: "beta", Path: "/tmp/beta", Languages: "go"},
		},
		Packages: []hotsnapshot.PackageRow{
			{Key: "package-alpha", RepositoryKey: "repo-alpha", Language: "go", Name: "example.com/alpha/pkg", ModulePath: "example.com/alpha"},
			{Key: "package-beta", RepositoryKey: "repo-beta", Language: "go", Name: "example.com/beta/pkg", ModulePath: "example.com/beta"},
		},
		Files: []hotsnapshot.FileRow{
			{Key: "file-alpha", RepositoryKey: "repo-alpha", PackageKey: "package-alpha", Path: "src/set.go", Language: "go"},
			{Key: "file-twins", RepositoryKey: "repo-alpha", PackageKey: "package-alpha", Path: "src/twins.go", Language: "go"},
			{Key: "file-beta", RepositoryKey: "repo-beta", PackageKey: "package-beta", Path: "src/set.go", Language: "go"},
		},
		Symbols: []hotsnapshot.SymbolRow{
			{StableKey: "alpha-merge", CanonicalIdentity: "go:alpha:pkg.Merge", FileKey: "file-alpha", Language: "go", Name: "Merge", QualifiedName: "pkg.Merge", Kind: "func", StartLine: 10, EndLine: 20},
			{StableKey: "beta-merge", CanonicalIdentity: "go:beta:pkg.Merge", FileKey: "file-beta", Language: "go", Name: "Merge", QualifiedName: "pkg.Merge", Kind: "func", StartLine: 5, EndLine: 9},
			{StableKey: "alpha-twin-a", CanonicalIdentity: "go:alpha:pkg.Twin#a", FileKey: "file-twins", Language: "go", Name: "Twin", QualifiedName: "pkg.Twin", Kind: "func", StartLine: 3, EndLine: 6},
			{StableKey: "alpha-twin-b", CanonicalIdentity: "go:alpha:pkg.Twin#b", FileKey: "file-twins", Language: "go", Name: "Twin", QualifiedName: "pkg.Twin", Kind: "method", StartLine: 8, EndLine: 12},
		},
	}
	snapshot, err := hotsnapshot.BuildGraphSnapshot(rows, 81, time.Unix(1_700_000_081, 0).UTC(), 1)
	if err != nil {
		t.Fatalf("BuildGraphSnapshot() error = %v", err)
	}
	return snapshot
}
