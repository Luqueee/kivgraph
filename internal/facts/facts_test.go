package facts

import "testing"

// TestUnresolvedKeyMatchesTheMergeIdentity keeps UnresolvedKey and Set.Merge
// deduplicating on the same tuple: a key that drifts from Merge would let two
// distinct unresolved references collide, or split one into two graph nodes.
func TestUnresolvedKeyMatchesTheMergeIdentity(t *testing.T) {
	base := UnresolvedReference{
		FileKey:          "file:acme/widgets:widgets/a.go",
		Reason:           "package_not_found",
		RequestedPackage: "acme/missing",
		RequestedSymbol:  "Thing",
		Start:            Position{Line: 4, Column: 2, Offset: 42},
	}

	identical := base
	if UnresolvedKey(base) != UnresolvedKey(identical) {
		t.Fatalf("UnresolvedKey is not stable for identical references: %q != %q",
			UnresolvedKey(base), UnresolvedKey(identical))
	}

	differentOffset := base
	differentOffset.Start.Offset = 43
	if UnresolvedKey(base) == UnresolvedKey(differentOffset) {
		t.Fatalf("UnresolvedKey did not distinguish references that only differ by offset: %q",
			UnresolvedKey(base))
	}

	// Merge deduplicates unresolved references by exactly this tuple; the
	// same two references must collapse to one entry, not two.
	var set Set
	set.Merge(Set{Unresolved: []UnresolvedReference{base}})
	set.Merge(Set{Unresolved: []UnresolvedReference{identical}})
	if len(set.Unresolved) != 1 {
		t.Fatalf("Merge kept %d unresolved references, want 1 deduplicated entry", len(set.Unresolved))
	}

	set.Merge(Set{Unresolved: []UnresolvedReference{differentOffset}})
	if len(set.Unresolved) != 2 {
		t.Fatalf("Merge kept %d unresolved references, want 2 distinct entries", len(set.Unresolved))
	}
}

// TestUnresolvedKeyDistinguishesEveryIdentityField guards each field Merge
// dedups on individually, not just the offset called out by the ticket.
func TestUnresolvedKeyDistinguishesEveryIdentityField(t *testing.T) {
	base := UnresolvedReference{
		FileKey:          "file:acme/widgets:widgets/a.go",
		Reason:           "package_not_found",
		RequestedPackage: "acme/missing",
		RequestedSymbol:  "Thing",
		Start:            Position{Line: 4, Column: 2, Offset: 42},
	}
	baseKey := UnresolvedKey(base)

	variants := map[string]UnresolvedReference{
		"file key":          withFileKey(base, "file:acme/widgets:widgets/b.go"),
		"reason":            withReason(base, "symbol_not_found"),
		"requested package": withRequestedPackage(base, "acme/other"),
		"requested symbol":  withRequestedSymbol(base, "OtherThing"),
	}
	for name, variant := range variants {
		if UnresolvedKey(variant) == baseKey {
			t.Fatalf("UnresolvedKey did not distinguish a change in %s: %q", name, baseKey)
		}
	}
}

func withFileKey(reference UnresolvedReference, key string) UnresolvedReference {
	reference.FileKey = key
	return reference
}

func withReason(reference UnresolvedReference, reason string) UnresolvedReference {
	reference.Reason = reason
	return reference
}

func withRequestedPackage(reference UnresolvedReference, requestedPackage string) UnresolvedReference {
	reference.RequestedPackage = requestedPackage
	return reference
}

func withRequestedSymbol(reference UnresolvedReference, requestedSymbol string) UnresolvedReference {
	reference.RequestedSymbol = requestedSymbol
	return reference
}
