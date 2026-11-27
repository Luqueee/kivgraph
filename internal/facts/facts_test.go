package facts

import (
	"reflect"
	"strings"
	"testing"
)

// scrambledSet has every collection out of order, with enough in each for an
// order to mean something and an edge pair that only the target tells apart.
func scrambledSet() Set {
	return Set{
		Repositories: []Repository{{Key: "r:b"}, {Key: "r:a"}, {Key: "r:c"}},
		Packages:     []Package{{Key: "p:c"}, {Key: "p:a"}, {Key: "p:b"}},
		Files:        []File{{Key: "f:2"}, {Key: "f:3"}, {Key: "f:1"}},
		Symbols:      []Symbol{{Key: "s:z"}, {Key: "s:m"}, {Key: "s:a"}},
		Evidence:     []Evidence{{Key: "e:2"}, {Key: "e:1"}},
		Edges: []Edge{
			{SourceKey: "s:m", TargetKey: "s:z", Kind: "CALLS_DIRECT"},
			{SourceKey: "s:a", TargetKey: "s:z", Kind: "CALLS_DIRECT"},
			{SourceKey: "s:m", TargetKey: "s:a", Kind: "CALLS_DIRECT"},
		},
		Unresolved: []UnresolvedReference{
			{FileKey: "f:2", Start: Position{Offset: 9}, Reason: "x"},
			{FileKey: "f:2", Start: Position{Offset: 3}, Reason: "x"},
			{FileKey: "f:1", Start: Position{Offset: 7}, Reason: "x"},
		},
	}
}

// Sorted is Sort that leaves the set alone. The two have to agree on every
// collection, and the input has to come back in the order it went in: a caller
// that handed over a set has every right to expect it unchanged.
func TestSortedIsSortWithoutChangingTheSet(t *testing.T) {
	input := scrambledSet()

	want := scrambledSet()
	want.Sort()

	got := input.Sorted()
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("Sorted() = %+v, want what Sort gives: %+v", got, want)
	}
	if !reflect.DeepEqual(input, scrambledSet()) {
		t.Fatalf("Sorted() changed the set it was given: %+v", input)
	}
}

// A collection already in order is shared, and one that is not is copied: the
// copy is the whole reason the method exists, and sharing a collection that was
// out of order would sort the caller's own data.
func TestSortedCopiesOnlyWhatIsOutOfOrder(t *testing.T) {
	input := scrambledSet()
	input.Symbols = []Symbol{{Key: "s:a"}, {Key: "s:m"}, {Key: "s:z"}}

	got := input.Sorted()
	if &got.Symbols[0] != &input.Symbols[0] {
		t.Fatal("a collection already in order was copied")
	}
	if &got.Files[0] == &input.Files[0] {
		t.Fatal("a collection out of order was sorted in place")
	}
	if got.Files[0].Key != "f:1" || input.Files[0].Key != "f:2" {
		t.Fatalf("files: sorted copy starts at %q and the input at %q, want f:1 and f:2", got.Files[0].Key, input.Files[0].Key)
	}
}

// Nothing to sort is not an error: every collection of an empty set comes back
// as it was.
func TestSortedOfAnEmptySetIsEmpty(t *testing.T) {
	if got := (Set{}).Sorted(); !reflect.DeepEqual(got, Set{}) {
		t.Fatalf("Sorted() of an empty set = %+v", got)
	}
}

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

// TestValidateRejectsASymbolWithTwoDefiners keeps the DEFINES multiplicity where
// the facts are still readable. LadybugDB enforces it too, and its message is a
// node offset: a set that reaches storage with two declarations sharing one
// identity fails minutes into a rebuild, naming neither file.
func TestValidateRejectsASymbolWithTwoDefiners(t *testing.T) {
	repository := Repository{Key: "repo:acme", Name: "acme", RootPath: "/repos/acme", Languages: []Language{LanguageGo}}
	pkg := Package{Key: "pkg:acme:widgets", RepositoryKey: repository.Key, Language: LanguageGo, Name: "widgets", RootPath: "/repos/acme/widgets"}
	first := File{Key: "file:acme:a.go", RepositoryKey: repository.Key, PackageKey: pkg.Key, Path: "widgets/a.go", Language: LanguageGo}
	second := File{Key: "file:acme:b.go", RepositoryKey: repository.Key, PackageKey: pkg.Key, Path: "widgets/b.go", Language: LanguageGo}
	symbol := Symbol{
		Key: "sym:acme:widgets.Thing", CanonicalIdentity: "acme/widgets.Thing",
		RepositoryKey: repository.Key, PackageKey: pkg.Key, FileKey: first.Key,
		Language: LanguageGo, Name: "Thing", QualifiedName: "widgets.Thing", Kind: "field",
	}
	set := Set{
		Repositories: []Repository{repository},
		Packages:     []Package{pkg},
		Files:        []File{first, second},
		Symbols:      []Symbol{symbol},
		Edges: []Edge{
			{Kind: Defines, SourceKey: first.Key, TargetKey: symbol.Key, Confidence: StructuralCertain, Provenance: GoTypesDefinition},
			{Kind: Defines, SourceKey: second.Key, TargetKey: symbol.Key, Confidence: StructuralCertain, Provenance: GoTypesDefinition},
		},
	}

	err := set.Validate()
	if err == nil {
		t.Fatal("Validate() error = nil, want two definers rejected")
	}
	for _, named := range []string{symbol.Key, first.Key, second.Key} {
		if !strings.Contains(err.Error(), named) {
			t.Fatalf("Validate() error = %v, want it to name %q", err, named)
		}
	}

	// The same file defining it twice is a different defect, and it also fails.
	set.Edges[1].SourceKey = first.Key
	if err := set.Validate(); err == nil || !strings.Contains(err.Error(), "twice") {
		t.Fatalf("Validate() error = %v, want the repeated definition rejected", err)
	}

	set.Edges = set.Edges[:1]
	if err := set.Validate(); err != nil {
		t.Fatalf("Validate() error = %v, want one definer to pass", err)
	}
}

// TestValidateNamesWhereAnEdgeWithAnUnknownTargetWasObserved keeps the error
// of a dangling edge readable. The key of the missing symbol is base32 that is
// in no source file, so the message has to carry what can be opened: the
// symbol the edge leaves from and the line that holds its evidence.
func TestValidateNamesWhereAnEdgeWithAnUnknownTargetWasObserved(t *testing.T) {
	repository := Repository{Key: "repo:acme", Name: "acme", RootPath: "/repos/acme", Languages: []Language{LanguageGo}}
	pkg := Package{Key: "pkg:acme:widgets", RepositoryKey: repository.Key, Language: LanguageGo, Name: "widgets", RootPath: "/repos/acme/widgets"}
	file := File{Key: "file:acme:a.go", RepositoryKey: repository.Key, PackageKey: pkg.Key, Path: "widgets/a.go", Language: LanguageGo}
	symbol := Symbol{
		Key: "sym:acme:widgets.Caller", CanonicalIdentity: "acme/widgets.Caller",
		RepositoryKey: repository.Key, PackageKey: pkg.Key, FileKey: file.Key,
		Language: LanguageGo, Name: "Caller", QualifiedName: "widgets.Caller", Kind: "func",
	}
	evidence := Evidence{Key: "evidence:1", RepositoryKey: repository.Key, FileKey: file.Key, Start: Position{Line: 12, Column: 4}}
	set := Set{
		Repositories: []Repository{repository},
		Packages:     []Package{pkg},
		Files:        []File{file},
		Symbols:      []Symbol{symbol},
		Evidence:     []Evidence{evidence},
		Edges: []Edge{
			{Kind: Defines, SourceKey: file.Key, TargetKey: symbol.Key, Confidence: StructuralCertain, Provenance: GoTypesDefinition},
			{Kind: References, SourceKey: symbol.Key, TargetKey: "sym:elsewhere", Confidence: ExactTypechecked, Provenance: GoTypesUse, EvidenceKey: evidence.Key},
		},
	}
	err := set.Validate()
	if err == nil {
		t.Fatal("Validate() error = nil, want the dangling edge rejected")
	}
	for _, named := range []string{"sym:elsewhere", "source widgets.Caller", "widgets/a.go:12:5"} {
		if !strings.Contains(err.Error(), named) {
			t.Fatalf("Validate() error = %v, want it to name %q", err, named)
		}
	}
}

// TestValidateRejectsAFileUnderTwoPackages is the CONTAINS_FILE counterpart of
// the test above. A file has one container; two payloads that both claim it
// merge into one File and two edges, and LadybugDB refuses the second with a
// node offset after the whole analysis has run. The message has to name the file
// by its path and both packages, because a key names nothing a reader can open.
func TestValidateRejectsAFileUnderTwoPackages(t *testing.T) {
	repository := Repository{Key: "repo:acme", Name: "acme", RootPath: "/repos/acme", Languages: []Language{LanguageTypeScript}}
	outer := Package{Key: "pkg:acme:root", RepositoryKey: repository.Key, Language: LanguageTypeScript, Name: "root", RootPath: "/repos/acme"}
	inner := Package{Key: "pkg:acme:web", RepositoryKey: repository.Key, Language: LanguageTypeScript, Name: "@acme/web", RootPath: "/repos/acme/apps/web"}
	file := File{Key: "file:acme:apps/web/src/a.ts", RepositoryKey: repository.Key, PackageKey: inner.Key, Path: "apps/web/src/a.ts", Language: LanguageTypeScript}
	set := Set{
		Repositories: []Repository{repository},
		Packages:     []Package{outer, inner},
		Files:        []File{file},
		Edges: []Edge{
			{Kind: ContainsFile, SourceKey: inner.Key, TargetKey: file.Key, Confidence: StructuralCertain, Provenance: PackageManifest},
			{Kind: ContainsFile, SourceKey: outer.Key, TargetKey: file.Key, Confidence: StructuralCertain, Provenance: PackageManifest},
		},
	}

	err := set.Validate()
	if err == nil {
		t.Fatal("Validate() error = nil, want a file under two packages rejected")
	}
	for _, named := range []string{file.Path, outer.Name, inner.Name} {
		if !strings.Contains(err.Error(), named) {
			t.Fatalf("Validate() error = %v, want it to name %q", err, named)
		}
	}

	// The same package listing a file twice is one container, not two.
	set.Edges[1].SourceKey = inner.Key
	if err := set.Validate(); err != nil {
		t.Fatalf("Validate() error = %v, want a repeated edge from one package to pass", err)
	}
	set.Edges = set.Edges[:1]
	if err := set.Validate(); err != nil {
		t.Fatalf("Validate() error = %v, want one container to pass", err)
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
