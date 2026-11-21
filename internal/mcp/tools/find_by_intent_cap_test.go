package tools

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/Luqueee/kivgraph/internal/hotsnapshot"
)

// crowdedIntentStore holds more symbols carrying "publish" than the candidate
// bound admits, all in one file, and a handful of others whose stable keys sort
// after every one of them -- so anything admitted in key order never reaches
// them.
func crowdedIntentStore(t *testing.T, id uint64) *hotsnapshot.SnapshotStore {
	t.Helper()
	rows := hotsnapshot.LadybugSnapshotRows{
		Repositories: []hotsnapshot.RepositoryRow{{Key: "repo", Name: "repo", Languages: "go"}},
		Packages:     []hotsnapshot.PackageRow{{Key: "pkg", RepositoryKey: "repo", Language: "go", Name: "pkg", ModulePath: "example.com/pkg"}},
		Files: []hotsnapshot.FileRow{
			{Key: "file-wide", RepositoryKey: "repo", PackageKey: "pkg", Path: "internal/wide/wide.go", Language: "go"},
			{Key: "file-narrow", RepositoryKey: "repo", PackageKey: "pkg", Path: "internal/narrow/narrow.go", Language: "go"},
		},
	}
	add := func(key, fileKey, name string, line int) {
		rows.Symbols = append(rows.Symbols, hotsnapshot.SymbolRow{
			StableKey: hotsnapshot.StableKey(key), CanonicalIdentity: "go:pkg." + name,
			FileKey: fileKey, Language: "go", Name: name, QualifiedName: "pkg." + name,
			Kind: "func", StartLine: uint32(line), EndLine: uint32(line + 1),
		})
	}
	for index := 0; index < maximumIntentCandidates+500; index++ {
		add(fmt.Sprintf("sym-%06d", index), "file-wide", fmt.Sprintf("Publish%06d", index), index*3+1)
	}
	// Past every key above.
	add("zzz-narrow-1", "file-narrow", "PublishLedger", 1)
	add("zzz-narrow-2", "file-narrow", "PublishJournal", 4)
	add("zzz-rare-1", "file-wide", "Quorum", 900_001)
	snapshot, err := hotsnapshot.BuildGraphSnapshot(rows, id, time.Unix(1, 0).UTC(), 1)
	if err != nil {
		t.Fatal(err)
	}
	return hotsnapshot.NewSnapshotStore(snapshot)
}

func intentNames(symbols []IntentSymbol) []string {
	names := make([]string, 0, len(symbols))
	for _, symbol := range symbols {
		names = append(names, symbol.QualifiedName)
	}
	return names
}

// repo, kind and path_prefix narrow which candidates are considered. The bound
// used to be spent before they applied, so a narrowing that selected symbols
// past the first few thousand matches came back empty -- and the guidance
// blamed the narrowing for excluding what was never looked at.
func TestFindByIntentSpendsTheCandidateBoundInsideTheNarrowing(t *testing.T) {
	_, response, err := findByIntent(context.Background(), nil, FindByIntentInput{
		Intent: "publish", View: ViewFull, PathPrefix: "internal/narrow",
	}, crowdedIntentStore(t, 131))
	if err != nil {
		t.Fatal(err)
	}
	got := intentNames(response.Results.Symbols)
	if response.Total != 2 || len(got) != 2 {
		t.Fatalf("total = %d, rows = %v, guidance = %q; want the two symbols under internal/narrow",
			response.Total, got, response.Guidance)
	}
	// Both fit, so nothing was left out and nothing needs saying.
	if response.Guidance != "" {
		t.Fatalf("guidance = %q, want none: every symbol inside the narrowing was ranked", response.Guidance)
	}
}

// The bound used to be spent in the order the question was written. A word most
// of the graph carries then filled it, and the symbols a rarer word named --
// the ones that answer the question -- never became candidates.
func TestFindByIntentAdmitsTheRarestTermFirst(t *testing.T) {
	_, response, err := findByIntent(context.Background(), nil, FindByIntentInput{
		Intent: "publish quorum", View: ViewFull, Limit: MaximumIntentLimit,
	}, crowdedIntentStore(t, 132))
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, name := range intentNames(response.Results.Symbols) {
		found = found || name == "pkg.Quorum"
	}
	if !found {
		t.Fatalf("pkg.Quorum is not in the first page of %d rows; the only symbol the rare word names was not ranked",
			response.Total)
	}
	if response.Total != maximumIntentCandidates {
		t.Fatalf("total = %d, want the bound of %d: the common word still fills what the rare one left",
			response.Total, maximumIntentCandidates)
	}
}
