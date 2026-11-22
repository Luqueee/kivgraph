package indexing

import (
	"reflect"
	"testing"
	"time"

	"github.com/Luqueee/kivgraph/internal/facts"
	"github.com/Luqueee/kivgraph/internal/hotsnapshot"
)

// The active generation: one repository, Go and TypeScript, three files that
// hold a declaration and one that holds none.
func lostFilesActive(t *testing.T) *hotsnapshot.GraphSnapshot {
	t.Helper()
	rows := hotsnapshot.LadybugSnapshotRows{
		Repositories: []hotsnapshot.RepositoryRow{{Key: "repo", Name: "app", Path: "/src/app", Languages: "go,typescript"}},
		Packages: []hotsnapshot.PackageRow{
			{Key: "pkg-go", RepositoryKey: "repo", Language: "go", Name: "example.com/app", ModulePath: "example.com/app"},
			{Key: "pkg-ts", RepositoryKey: "repo", Language: "typescript", Name: "app"},
		},
		Files: []hotsnapshot.FileRow{
			{Key: "a", RepositoryKey: "repo", PackageKey: "pkg-go", Path: "a.go", Language: "go"},
			{Key: "b", RepositoryKey: "repo", PackageKey: "pkg-go", Path: "sub/b.go", Language: "go"},
			{Key: "empty", RepositoryKey: "repo", PackageKey: "pkg-go", Path: "empty.go", Language: "go"},
			{Key: "w", RepositoryKey: "repo", PackageKey: "pkg-ts", Path: "web/w.ts", Language: "typescript"},
		},
	}
	for _, file := range []struct{ key, language, name string }{{"a", "go", "A"}, {"b", "go", "B"}, {"w", "typescript", "W"}} {
		rows.Symbols = append(rows.Symbols, hotsnapshot.SymbolRow{
			StableKey: hotsnapshot.StableKey("key-" + file.name), CanonicalIdentity: "id:" + file.name,
			FileKey: file.key, Language: file.language, Name: file.name, QualifiedName: file.name,
			Kind: "function", StartLine: 1, EndLine: 1,
		})
	}
	snapshot, err := hotsnapshot.BuildGraphSnapshot(rows, 7, time.Unix(1_700_000_000, 0).UTC(), 1)
	if err != nil {
		t.Fatalf("BuildGraphSnapshot() error = %v", err)
	}
	return snapshot
}

// The candidate of a pass that read `read` (paths) and recorded `failures`.
func lostFilesCandidate(languages []facts.Language, read []string, failures ...facts.UnresolvedReference) facts.Set {
	set := facts.Set{
		Repositories: []facts.Repository{{Key: "repository:app", Name: "app", RootPath: "/src/app", Languages: languages}},
		Unresolved:   failures,
	}
	for _, path := range read {
		set.Files = append(set.Files, facts.File{Key: "file:" + path, RepositoryKey: "repository:app", Path: path})
		set.Symbols = append(set.Symbols, facts.Symbol{Key: "symbol:" + path, FileKey: "file:" + path})
	}
	return set
}

func TestLostFilesNamesWhatAPassCouldNotLoadAndNothingElse(t *testing.T) {
	active := lostFilesActive(t)
	both := []facts.Language{facts.LanguageGo, facts.LanguageTypeScript}
	goFailed := facts.UnresolvedReference{RepositoryKey: "repository:app", Language: facts.LanguageGo, Reason: "MODULE_NOT_LOADED"}
	everything := func(string) bool { return true }

	for name, testCase := range map[string]struct {
		candidate facts.Set
		exists    func(string) bool
		want      []string
	}{
		"the module was not loaded": {
			candidate: lostFilesCandidate(both, []string{"web/w.ts"}, goFailed),
			exists:    everything,
			want:      []string{"app:a.go", "app:sub/b.go"},
		},
		"part of it was": {
			candidate: lostFilesCandidate(both, []string{"web/w.ts", "a.go"}, goFailed),
			exists:    everything,
			want:      []string{"app:sub/b.go"},
		},
		"everything was read": {
			candidate: lostFilesCandidate(both, []string{"web/w.ts", "a.go", "sub/b.go"}, goFailed),
			exists:    everything,
		},
		// Not a loss: the pass read all it was asked to, and a configuration
		// or an edit is why the file holds nothing.
		"no failure was recorded": {
			candidate: lostFilesCandidate(both, []string{"web/w.ts"}),
			exists:    everything,
		},
		"the failure is of another kind": {
			candidate: lostFilesCandidate(both, []string{"web/w.ts"},
				facts.UnresolvedReference{RepositoryKey: "repository:app", Language: facts.LanguageGo, Reason: "PACKAGE_PROVIDER_NOT_FOUND"}),
			exists: everything,
		},
		"the failure is in another language": {
			candidate: lostFilesCandidate(both, []string{"web/w.ts"},
				facts.UnresolvedReference{RepositoryKey: "repository:app", Language: facts.LanguageTypeScript, Reason: "PACKAGE_NOT_LOADED"}),
			exists: everything,
		},
		"the failure is in another repository": {
			candidate: lostFilesCandidate(both, []string{"web/w.ts"},
				facts.UnresolvedReference{RepositoryKey: "repository:other", Language: facts.LanguageGo, Reason: "MODULE_NOT_LOADED"}),
			exists: everything,
		},
		"the files were deleted": {
			candidate: lostFilesCandidate(both, []string{"web/w.ts"}, goFailed),
			exists:    func(path string) bool { return path == "/src/app/sub/b.go" },
			want:      []string{"app:sub/b.go"},
		},
		"the language was withdrawn": {
			candidate: lostFilesCandidate([]facts.Language{facts.LanguageTypeScript}, []string{"web/w.ts"}, goFailed),
			exists:    everything,
		},
		"the repository was withdrawn": {
			candidate: facts.Set{Unresolved: []facts.UnresolvedReference{goFailed}},
			exists:    everything,
		},
	} {
		if got := lostFiles(active, testCase.candidate, testCase.exists); len(got) != len(testCase.want) ||
			(len(got) > 0 && !reflect.DeepEqual(got, testCase.want)) {
			t.Errorf("%s: lost = %v, want %v", name, got, testCase.want)
		}
	}
}
