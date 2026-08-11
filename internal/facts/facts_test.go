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

// The published snapshot indexes unresolved references by what was requested,
// so a set that carries a reason with no subject must be rejected while it is
// still a fact set, not five stages later when the snapshot is built.
func TestValidateRejectsUnresolvedReferenceWithoutRequestedPackage(t *testing.T) {
	repositoryKey := RepositoryKey("acme/widgets")
	set := Set{
		Repositories: []Repository{
			{Key: repositoryKey, Name: "acme/widgets", RootPath: "/repos/widgets", Languages: []Language{LanguageTypeScript}},
		},
		Unresolved: []UnresolvedReference{{
			RepositoryKey: repositoryKey,
			Language:      LanguageTypeScript,
			Reason:        "DECLARATION_NOT_RESOLVED",
			Start:         Position{Line: 3, Column: 0, Offset: 30},
		}},
	}

	if err := set.Validate(); err == nil {
		t.Fatal("Validate() error = nil, want a rejected unresolved reference")
	}

	set.Unresolved[0].RequestedPackage = "@acme/widgets"
	set.Unresolved[0].RequestedSymbol = "EventEmitter"
	if err := set.Validate(); err != nil {
		t.Fatalf("Validate() error = %v, want the named reference to pass", err)
	}
}

// TestMergeAllKeepsTheFirstRecordOfEveryKey pins what makes two facts one.
// A pass merges one set per unit of work, and the same symbol is defined
// once but referenced from many units: whichever unit is merged first owns
// the record, and a later duplicate must not silently replace it with a
// different payload.
func TestMergeAllKeepsTheFirstRecordOfEveryKey(t *testing.T) {
	repository := Repository{Key: "repo:acme", Name: "acme", RootPath: "/repos/acme", Languages: []Language{LanguageGo}}
	pkg := Package{Key: "pkg:acme:widgets", RepositoryKey: repository.Key, Language: LanguageGo, Name: "widgets", RootPath: "/repos/acme/widgets"}
	file := File{Key: "file:acme:widgets/a.go", RepositoryKey: repository.Key, PackageKey: pkg.Key, Path: "widgets/a.go", Language: LanguageGo, ContentHash: "hash"}
	defined := Symbol{
		Key: "sym:acme:widgets.Thing", CanonicalIdentity: "acme/widgets.Thing",
		RepositoryKey: repository.Key, PackageKey: pkg.Key, FileKey: file.Key,
		Language: LanguageGo, Name: "Thing", QualifiedName: "acme/widgets.Thing",
		Kind: "type", Exported: true, Signature: "type Thing struct{}",
	}
	stub := defined
	stub.Signature = "stub carried by a consumer"

	first := Set{Repositories: []Repository{repository}, Packages: []Package{pkg}, Files: []File{file}, Symbols: []Symbol{defined}}
	second := Set{Repositories: []Repository{repository}, Packages: []Package{pkg}, Files: []File{file}, Symbols: []Symbol{stub}}

	merged := MergeAll([]Set{first, second})
	if len(merged.Symbols) != 1 {
		t.Fatalf("symbols = %d, want the duplicate key merged into one", len(merged.Symbols))
	}
	if merged.Symbols[0].Signature != defined.Signature {
		t.Fatalf("signature = %q, want the first record %q", merged.Symbols[0].Signature, defined.Signature)
	}
	if len(merged.Repositories) != 1 || len(merged.Packages) != 1 || len(merged.Files) != 1 {
		t.Fatalf("containers were not deduplicated: %d repositories, %d packages, %d files",
			len(merged.Repositories), len(merged.Packages), len(merged.Files))
	}
}

// TestMergeAllSeparatesEdgesByEvidence keeps the merge identity of an edge at
// the full tuple. Two calls from the same function to the same target are two
// facts observed at two places, and collapsing them by endpoints alone would
// erase the second observation.
func TestMergeAllSeparatesEdgesByEvidence(t *testing.T) {
	left := Edge{
		Kind: CallsDirect, SourceKey: "sym:a", TargetKey: "sym:b",
		Confidence: ExactTypechecked, Provenance: GoASTCall, EvidenceKey: "evidence:one",
	}
	right := left
	right.EvidenceKey = "evidence:two"
	same := left

	merged := MergeAll([]Set{{Edges: []Edge{right}}, {Edges: []Edge{left, same}}})
	if len(merged.Edges) != 2 {
		t.Fatalf("edges = %d, want the two observations kept and the repeat dropped", len(merged.Edges))
	}
	// Sort runs once over the merged result, so the order never depends on
	// which set carried which edge.
	if merged.Edges[0].EvidenceKey != "evidence:one" || merged.Edges[1].EvidenceKey != "evidence:two" {
		t.Fatalf("merged edges are not sorted: %q then %q", merged.Edges[0].EvidenceKey, merged.Edges[1].EvidenceKey)
	}
}
