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
	if !found || symbolStableKey(snapshot, symbol) != "alpha-merge" {
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

// A name that several repositories declare is refused with every candidate
// named by where it is, because that is the narrowing the caller can express
// next. Answering with either one would be a nominal coincidence, which this
// surface refuses exactly as the graph does.
func TestResolveDeclarationByNameRefusesAnAmbiguousName(t *testing.T) {
	snapshot := selectorSnapshot(t)
	_, _, err := resolveDeclarationByName(snapshot, "Merge", "", "")
	if err == nil {
		t.Fatal("resolveDeclarationByName(ambiguous) error = nil, want an ambiguity")
	}
	message := err.Error()
	for _, want := range []string{
		`name "Merge" declares 2 symbols`,
		"repeat with the repository and path",
		"alpha:src/set.go:10",
		"beta:src/set.go:5",
	} {
		if !strings.Contains(message, want) {
			t.Fatalf("ambiguity message = %q, want it to contain %q", message, want)
		}
	}
}

// Two declarations of one name in one repository are still ambiguous, and the
// narrowing the caller already applied does not make the answer a guess.
func TestResolveDeclarationByNameStaysAmbiguousWithinOneRepository(t *testing.T) {
	snapshot := selectorSnapshot(t)
	_, _, err := resolveDeclarationByName(snapshot, "Twin", "alpha", "src/twins.go")
	if err == nil {
		t.Fatal("resolveDeclarationByName(homonyms) error = nil, want an ambiguity")
	}
	if !strings.Contains(err.Error(), `name "Twin" declares 2 symbols`) {
		t.Fatalf("ambiguity message = %q, want it to count both declarations", err.Error())
	}
}

// The narrowing resolves it, and the qualified name travels back so the
// caller never has to ask a second time for what the lookup already read.
func TestResolveDeclarationByNameResolvesANarrowedName(t *testing.T) {
	snapshot := selectorSnapshot(t)
	id, qualifiedName, err := resolveDeclarationByName(snapshot, "Merge", "beta", "")
	if err != nil {
		t.Fatalf("resolveDeclarationByName() error = %v", err)
	}
	if qualifiedName != "pkg.Merge" {
		t.Fatalf("qualified name = %q, want pkg.Merge", qualifiedName)
	}
	symbol, found := snapshot.Symbol(id)
	if !found || symbolStableKey(snapshot, symbol) != "beta-merge" {
		t.Fatalf("resolved %#v, want beta-merge", symbol)
	}
}

// A repository the graph does not hold is a different answer from a name it
// does not hold, and both are different from a name that is only mentioned.
// Collapsing them would send the caller to narrow a search that was never
// going to find anything.
func TestResolveDeclarationByNameSeparatesItsThreeAbsences(t *testing.T) {
	snapshot := selectorSnapshot(t)
	for _, testCase := range [...]struct {
		name       string
		symbol     string
		repository string
		want       string
	}{
		{
			name:       "a repository the graph does not hold",
			symbol:     "Merge",
			repository: "gamma",
			want:       `repository "gamma" is not in the published graph`,
		},
		{
			name:   "a name the graph does not hold",
			symbol: "Absent",
			want:   `name "Absent" was not found`,
		},
		{
			name:       "a name no repository under this narrowing declares",
			symbol:     "Merge",
			repository: "beta",
			want:       "",
		},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			_, _, err := resolveDeclarationByName(snapshot, testCase.symbol, testCase.repository, "")
			if testCase.want == "" {
				if err != nil {
					t.Fatalf("resolveDeclarationByName() error = %v, want it to resolve", err)
				}
				return
			}
			if err == nil {
				t.Fatalf("resolveDeclarationByName() error = nil, want %q", testCase.want)
			}
			if !strings.Contains(err.Error(), testCase.want) {
				t.Fatalf("error = %q, want it to contain %q", err.Error(), testCase.want)
			}
		})
	}
}

// A name that the graph only ever imports or re-exports is not a name it
// does not hold: the caller has to be told the declaration is elsewhere,
// because "not found" would send them to give up on a symbol that exists.
func TestResolveDeclarationByNameSaysWhenANameIsOnlyMentioned(t *testing.T) {
	rows := hotsnapshot.LadybugSnapshotRows{
		Repositories: []hotsnapshot.RepositoryRow{
			{Key: "repo-alpha", Name: "alpha", Path: "/tmp/alpha", Languages: "go"},
		},
		Packages: []hotsnapshot.PackageRow{
			{Key: "package-alpha", RepositoryKey: "repo-alpha", Language: "go", Name: "example.com/alpha/pkg", ModulePath: "example.com/alpha"},
		},
		Files: []hotsnapshot.FileRow{
			{Key: "file-alpha", RepositoryKey: "repo-alpha", PackageKey: "package-alpha", Path: "src/uses.go", Language: "go"},
		},
		Symbols: []hotsnapshot.SymbolRow{
			{
				StableKey: "alpha-import", CanonicalIdentity: "go:alpha:pkg.Borrowed",
				FileKey: "file-alpha", Language: "go", Name: "Borrowed",
				QualifiedName: "pkg.Borrowed", Kind: "import", StartLine: 1, EndLine: 1,
			},
		},
	}
	snapshot, err := hotsnapshot.BuildGraphSnapshot(rows, 82, time.Unix(1_700_000_082, 0).UTC(), 1)
	if err != nil {
		t.Fatalf("BuildGraphSnapshot() error = %v", err)
	}
	_, _, err = resolveDeclarationByName(snapshot, "Borrowed", "", "")
	if err == nil {
		t.Fatal("resolveDeclarationByName(mentioned) error = nil, want the mention reported")
	}
	if !strings.Contains(err.Error(), "is only imported or re-exported here, never declared") {
		t.Fatalf("error = %q, want it to separate a mention from an absence", err.Error())
	}
}

// TestANameThatIsNotASymbolIsSentToFindByIntent covers both ways a bare name
// comes back empty, because they are two returns and only a shared helper keeps
// them saying the same thing.
//
// One is a name the string arena never interned. The other is a name it did --
// a repository name, a path, a kind -- that no symbol carries, which reaches a
// different return eighty lines down. Over five days these were `dart`,
// `posthog`, `playw` and `HEAD`: search terms, not identifiers, from a caller
// using a symbol lookup as grep. The answer they got named no next step, while
// the neighbour that handles a narrowed qualified name has named one since it
// was written.
func TestANameThatIsNotASymbolIsSentToFindByIntent(t *testing.T) {
	snapshot := selectorSnapshot(t)
	for _, testCase := range [...]struct {
		name   string
		symbol string
	}{
		{name: "a name the arena never interned", symbol: "Absent"},
		// Interned as a repository name and carried by no symbol, which is the
		// second return.
		{name: "a name the arena holds that no symbol carries", symbol: "alpha"},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			_, _, err := resolveDeclarationByName(snapshot, testCase.symbol, "", "")
			if err == nil {
				t.Fatal("resolveDeclarationByName() resolved a name no symbol carries")
			}
			if code := ErrorCode(err); code != CodeSymbolNotFound {
				t.Fatalf("code = %q, want %q: this is still a failure to answer", code, CodeSymbolNotFound)
			}
			if !strings.Contains(err.Error(), "find_by_intent") {
				t.Fatalf("error = %q, want it to name the tool that answers this question", err.Error())
			}
		})
	}
}
