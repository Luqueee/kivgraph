package hotsnapshot

import (
	"errors"
	"testing"
	"time"
)

func TestExactSearchesAndPagination(t *testing.T) {
	snapshot, err := BuildGraphSnapshot(builderRows(), 1, time.Unix(1, 0).UTC(), 1)
	if err != nil {
		t.Fatal(err)
	}
	name, found := snapshot.Strings().Lookup("shared")
	if !found {
		t.Fatal("shared was not interned")
	}
	page, err := snapshot.SearchSymbolsByName(name, 0, 1)
	if err != nil {
		t.Fatal(err)
	}
	if page.Total != 2 || len(page.IDs) != 1 || page.IDs[0] != 0 || !page.HasMore {
		t.Fatalf("first exact page = %#v", page)
	}
	page, err = snapshot.SearchSymbolsByName(name, 1, 1)
	if err != nil {
		t.Fatal(err)
	}
	if page.Total != 2 || len(page.IDs) != 1 || page.IDs[0] != 1 || page.HasMore {
		t.Fatalf("second exact page = %#v", page)
	}
	page, err = snapshot.SearchSymbolsByName(name, 10, 10)
	if err != nil {
		t.Fatal(err)
	}
	if page.Total != 2 || len(page.IDs) != 0 || page.HasMore {
		t.Fatalf("past-end exact page = %#v", page)
	}
	qualified, found := snapshot.Strings().Lookup("A.shared")
	if !found {
		t.Fatal("qualified name was not interned")
	}
	page, err = snapshot.SearchSymbolsByQName(qualified, 0, 10)
	if err != nil || len(page.IDs) != 1 || page.IDs[0] != 0 {
		t.Fatalf("qualified exact page = %#v, %v", page, err)
	}
	nearMiss, found := snapshot.Strings().Lookup("shared.extra")
	if !found {
		nearMiss = InvalidInternedString - 1
	}
	page, err = snapshot.SearchSymbolsByName(nearMiss, 0, 10)
	if err != nil || page.Total != 0 || len(page.IDs) != 0 {
		t.Fatalf("nominal near miss page = %#v, %v", page, err)
	}
}

func TestPrefixSearchIsNameOnlyAndStable(t *testing.T) {
	snapshot, err := BuildGraphSnapshot(builderRows(), 1, time.Unix(1, 0).UTC(), 1)
	if err != nil {
		t.Fatal(err)
	}
	page, err := snapshot.SearchSymbolsByNamePrefix("sha", 0, 10)
	if err != nil {
		t.Fatal(err)
	}
	if page.Total != 2 || len(page.IDs) != 2 || page.IDs[0] != 0 || page.IDs[1] != 1 || page.HasMore {
		t.Fatalf("prefix page = %#v", page)
	}
	page, err = snapshot.SearchSymbolsByNamePrefix("A.", 0, 10)
	if err != nil {
		t.Fatal(err)
	}
	if page.Total != 0 || len(page.IDs) != 0 {
		t.Fatalf("qualified-name prefix unexpectedly matched = %#v", page)
	}
}

func TestExactSearchRejectsInvalidPagination(t *testing.T) {
	snapshot, err := BuildGraphSnapshot(builderRows(), 1, time.Unix(1, 0).UTC(), 1)
	if err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		offset int
		limit  int
		want   error
	}{
		{offset: -1, limit: 10, want: ErrInvalidExactOffset},
		{offset: 0, limit: 0, want: ErrInvalidExactLimit},
		{offset: 0, limit: MaxExactResults + 1, want: ErrInvalidExactLimit},
	} {
		if _, err := snapshot.SearchSymbolsByName(0, test.offset, test.limit); !errors.Is(err, test.want) {
			t.Fatalf("SearchSymbolsByName(%d,%d) error = %v, want %v", test.offset, test.limit, err, test.want)
		}
	}
}
