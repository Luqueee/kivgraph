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

	groups, total := snapshot.UnresolvedScopeGroups(repository, "", true, 10)
	if len(groups) != 4 || total != 4 {
		t.Fatalf("groups = %d, total = %d, want 4 and 4: twelve failures of four packages", len(groups), total)
	}
	for _, group := range groups {
		if group.Occurrences != 3 {
			t.Fatalf("occurrences = %d, want the three failures behind each package", group.Occurrences)
		}
	}

	cut, cutTotal := snapshot.UnresolvedScopeGroups(repository, "", true, 2)
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

// A scope is recorded in the language whose analyzer could not read it, and
// bounds answers in that language or when it recorded none.
func TestUnresolvedScopeGroupsReadsTheLanguageOfTheScope(t *testing.T) {
	rows := scopeRows(2, 1)
	rows.Unresolved = append(rows.Unresolved,
		UnresolvedReferenceRow{Key: "ts", RepositoryKey: "repo-a", Language: "typescript",
			RequestedPackage: "@scope/pkg", Reason: "PACKAGE_PROVIDER_NOT_FOUND"},
		UnresolvedReferenceRow{Key: "none", RepositoryKey: "repo-a",
			RequestedPackage: "somewhere", Reason: "REPOSITORY_NOT_LOADED"},
	)
	snapshot, err := BuildGraphSnapshot(rows, 9, time.Unix(1_700_000_000, 0).UTC(), 1)
	if err != nil {
		t.Fatalf("BuildGraphSnapshot() error = %v", err)
	}
	repository, _ := snapshot.RepositoryByName("alpha")
	// The TypeScript scope is a dependency nobody registered: it bounds a
	// search by name, and no answer about a declaration, in any language.
	for language, want := range map[string]int{"": 4, "go": 3, "typescript": 1, "rust": 1} {
		groups, total := snapshot.UnresolvedScopeGroups(repository, language, true, 10)
		if len(groups) != want || total != want {
			t.Errorf("language %q: %d groups, total %d, want %d", language, len(groups), total, want)
		}
	}
	// The total is of what the language keeps, also when the page is cut.
	if groups, total := snapshot.UnresolvedScopeGroups(repository, "go", true, 1); len(groups) != 1 || total != 3 {
		t.Errorf("cut: %d groups, total %d, want 1 and 3", len(groups), total)
	}
}

// A scope that only says implementation relations are incomplete bounds an
// answer one of them could be part of, and is not counted for any other --
// in the total either, which is what a cut page reports.
func TestUnresolvedScopeGroupsLeavesOutImplementationCoverageWhenNoneCanAnswer(t *testing.T) {
	rows := scopeRows(1, 1)
	rows.Unresolved = append(rows.Unresolved, UnresolvedReferenceRow{
		Key: "coverage", RepositoryKey: "repo-a", Language: "go",
		RequestedPackage: "example.com/app", Reason: ImplementationCoverageReason,
		Detail: "Imported targets that are not an interface or a type alias are excluded.",
	})
	snapshot, err := BuildGraphSnapshot(rows, 11, time.Unix(1_700_000_000, 0).UTC(), 1)
	if err != nil {
		t.Fatalf("BuildGraphSnapshot() error = %v", err)
	}
	repository, _ := snapshot.RepositoryByName("alpha")
	for _, language := range [...]string{"", "go"} {
		for implementations, want := range map[bool]int{true: 2, false: 1} {
			groups, total := snapshot.UnresolvedScopeGroups(repository, language, implementations, 10)
			if len(groups) != want || total != want {
				t.Errorf("language %q, implementations %t: %d groups, total %d, want %d",
					language, implementations, len(groups), total, want)
			}
			if _, total := snapshot.UnresolvedScopeGroups(repository, language, implementations, 0); total != want {
				t.Errorf("language %q, implementations %t, empty page: total %d, want %d",
					language, implementations, total, want)
			}
		}
	}
}
