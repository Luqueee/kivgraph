package hotsnapshot

import (
	"testing"
	"time"
)

// The lookup behind every completeness verdict, on its own: which failures
// count as having asked for a name once the declaration's language is known.
func TestUnresolvedNamingSymbolReadsTheLanguageOfTheFailure(t *testing.T) {
	row := func(key, language, requested string) UnresolvedReferenceRow {
		return UnresolvedReferenceRow{
			Key: key, RepositoryKey: "repo", FileKey: "file", Language: language,
			RequestedPackage: "example.com/other", RequestedSymbol: requested, Reason: "TARGET_NOT_INDEXED", StartLine: 1,
		}
	}
	snapshot, err := BuildGraphSnapshot(LadybugSnapshotRows{
		Repositories: []RepositoryRow{{Key: "repo", Name: "repo", Languages: "go,rust"}},
		Packages:     []PackageRow{{Key: "pkg", RepositoryKey: "repo", Language: "go", Name: "pkg", ModulePath: "example.com/pkg"}},
		Files:        []FileRow{{Key: "file", RepositoryKey: "repo", PackageKey: "pkg", Path: "a.go", Language: "go"}},
		Unresolved: []UnresolvedReferenceRow{
			row("go-exact", "go", "Command"),
			row("go-member", "go", "exec.Command"),
			row("rust-exact", "rust", "Command"),
			row("rust-member", "rust", "process.Command"),
			row("unknown", "", "Command"),
			row("go-other", "go", "Commander"),
			// A scope failure names no file and is not a failure about a name.
			{Key: "scope", RepositoryKey: "repo", Language: "go", RequestedPackage: "example.com/other", RequestedSymbol: "Command", Reason: "PACKAGE_NOT_BUILDABLE"},
		},
	}, 1, time.Unix(1_700_000_000, 0).UTC(), 1)
	if err != nil {
		t.Fatal(err)
	}
	for language, want := range map[string]int{
		"":       5, // any: every failure that asked for the name
		"go":     3, // its own two, and the one that recorded no language
		"rust":   3,
		"python": 1, // only the one that cannot be ruled out
	} {
		matched, total := snapshot.UnresolvedNamingSymbol("Command", language, 10)
		if total != want || len(matched) != want {
			t.Errorf("language %q: %d failures (%d returned), want %d", language, total, len(matched), want)
		}
	}
	// The limit bounds what is returned and never what is counted.
	matched, total := snapshot.UnresolvedNamingSymbol("Command", "go", 1)
	if len(matched) != 1 || total != 3 {
		t.Errorf("with a limit of 1: %d returned, %d counted, want 1 and 3", len(matched), total)
	}
	if matched, total := snapshot.UnresolvedNamingSymbol("", "go", 10); total != 0 || matched != nil {
		t.Errorf("an empty name matched %d failures", total)
	}
	if matched, total := snapshot.UnresolvedNamingSymbol("Command", "go", -1); total != 0 || matched != nil {
		t.Errorf("a negative limit matched %d failures", total)
	}
}
