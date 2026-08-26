package hotsnapshot

import (
	"fmt"
	"testing"
	"time"
)

// scopeRows builds one repository whose only content is scope failures: rows
// without a file, which is what makes a failure a scope and not a reference.
// distinct packages each carry occurrences failures.
func scopeRows(distinct, occurrences int) LadybugSnapshotRows {
	rows := LadybugSnapshotRows{
		Repositories: []RepositoryRow{{Key: "repo-a", Name: "alpha", Path: "/repo-a"}},
	}
	for scope := 0; scope < distinct; scope++ {
		for occurrence := 0; occurrence < occurrences; occurrence++ {
			rows.Unresolved = append(rows.Unresolved, UnresolvedReferenceRow{
				Key:              fmt.Sprintf("unres-%d-%d", scope, occurrence),
				RepositoryKey:    "repo-a",
				Language:         "go",
				RequestedPackage: fmt.Sprintf("example.com/pkg-%d", scope),
				RequestedSymbol:  fmt.Sprintf("Symbol%d", occurrence),
				Reason:           "DECLARATION_OUTSIDE_REPOSITORY",
				Detail:           "declared in a Go build cache entry",
			})
		}
	}
	return rows
}

// TestUnresolvedScopeGroupsCountsScopesAndKeepsACutListHonest defends the two
// numbers a caller reports from one page. The list is bounded, so a repository
// with more invisible packages than the cap still has to say how many there are
// -- and the occurrences of a scope that did not make the page must not be
// added to one that did, which is the way a bounded group silently grows.
func TestUnresolvedScopeGroupsCountsScopesAndKeepsACutListHonest(t *testing.T) {
	snapshot, err := BuildGraphSnapshot(scopeRows(4, 3), 7, time.Unix(1_700_000_000, 0).UTC(), 1)
	if err != nil {
		t.Fatalf("BuildGraphSnapshot() error = %v", err)
	}
	repository, found := snapshot.RepositoryByName("alpha")
	if !found {
		t.Fatalf("repository alpha is missing from the snapshot")
	}

	groups, total := snapshot.UnresolvedScopeGroups(repository, 10)
	if len(groups) != 4 || total != 4 {
		t.Fatalf("groups = %d, total = %d, want 4 and 4: twelve failures of four packages", len(groups), total)
	}
	for _, group := range groups {
		if group.Occurrences != 3 {
			t.Fatalf("occurrences = %d, want the three failures behind each package", group.Occurrences)
		}
	}

	cut, cutTotal := snapshot.UnresolvedScopeGroups(repository, 2)
	if len(cut) != 2 {
		t.Fatalf("cut groups = %d, want the page size", len(cut))
	}
	if cutTotal != 4 {
		t.Fatalf("cut total = %d, want every distinct scope counted", cutTotal)
	}
	for _, group := range cut {
		if group.Occurrences != 3 {
			t.Fatalf("cut occurrences = %d, want a shown scope to keep its own count", group.Occurrences)
		}
	}
}
