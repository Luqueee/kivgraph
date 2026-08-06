package facts

import (
	"errors"
	"reflect"
	"sort"
	"testing"
)

// --- fixture key helpers -----------------------------------------------
//
// Centralised so every test computes the same durable key for the same
// entity: a magic string duplicated across tests is how these things drift.

func widgetsRepoKey() string            { return RepositoryKey("acme/widgets") }
func widgetsPkgKey() string             { return PackageKey(LanguageGo, widgetsRepoKey(), "widgets") }
func widgetsFileKey(path string) string { return FileKey(widgetsRepoKey(), path) }

const (
	fooKey       = "symbol:go:acme/widgets.Foo"
	fooHelperKey = "symbol:go:acme/widgets.FooHelper"
	barKey       = "symbol:go:acme/widgets.Bar"
	isolatedKey  = "symbol:go:acme/widgets.Isolated"
)

func widgetsCallEvidenceKey() string {
	return EvidenceKey(widgetsFileKey("a.go"), 40, 46)
}

// baseSet is a small but complete, Validate-passing fact set: one
// repository, one package, three files. fileA defines Foo (which calls
// Bar, defined in fileB) and FooHelper (unrelated, so "modify one symbol"
// tests can show the file's *other* symbol survives untouched); fileB
// defines Bar; fileIsolated defines Isolated and has no relation to
// anything else, so it is safe to remove on its own without any ripple
// effect on fileA/fileB.
func baseSet() Set {
	repoKey := widgetsRepoKey()
	pkgKey := widgetsPkgKey()
	fileAKey := widgetsFileKey("a.go")
	fileBKey := widgetsFileKey("b.go")
	fileIsolatedKey := widgetsFileKey("isolated.go")
	evidenceKey := widgetsCallEvidenceKey()

	set := Set{
		Repositories: []Repository{
			{Key: repoKey, Name: "acme/widgets", RootPath: "/repos/widgets", Languages: []Language{LanguageGo}},
		},
		Packages: []Package{
			{Key: pkgKey, RepositoryKey: repoKey, Language: LanguageGo, Name: "widgets", RootPath: "/repos/widgets"},
		},
		Files: []File{
			{Key: fileAKey, RepositoryKey: repoKey, PackageKey: pkgKey, Path: "a.go", Language: LanguageGo, ContentHash: "hash-a-1"},
			{Key: fileBKey, RepositoryKey: repoKey, PackageKey: pkgKey, Path: "b.go", Language: LanguageGo, ContentHash: "hash-b-1"},
			{Key: fileIsolatedKey, RepositoryKey: repoKey, PackageKey: pkgKey, Path: "isolated.go", Language: LanguageGo, ContentHash: "hash-i-1"},
		},
		Symbols: []Symbol{
			{
				Key: fooKey, CanonicalIdentity: "go:acme/widgets.Foo", RepositoryKey: repoKey, PackageKey: pkgKey,
				FileKey: fileAKey, Language: LanguageGo, Name: "Foo", QualifiedName: "widgets.Foo", Kind: "func",
				Exported: true, Start: Position{Line: 5, Offset: 30}, End: Position{Line: 8, Offset: 90},
			},
			{
				Key: fooHelperKey, CanonicalIdentity: "go:acme/widgets.FooHelper", RepositoryKey: repoKey, PackageKey: pkgKey,
				FileKey: fileAKey, Language: LanguageGo, Name: "FooHelper", QualifiedName: "widgets.FooHelper", Kind: "func",
				Start: Position{Line: 10, Offset: 100}, End: Position{Line: 12, Offset: 140},
			},
			{
				Key: barKey, CanonicalIdentity: "go:acme/widgets.Bar", RepositoryKey: repoKey, PackageKey: pkgKey,
				FileKey: fileBKey, Language: LanguageGo, Name: "Bar", QualifiedName: "widgets.Bar", Kind: "func",
				Start: Position{Line: 3, Offset: 20}, End: Position{Line: 6, Offset: 70},
			},
			{
				Key: isolatedKey, CanonicalIdentity: "go:acme/widgets.Isolated", RepositoryKey: repoKey, PackageKey: pkgKey,
				FileKey: fileIsolatedKey, Language: LanguageGo, Name: "Isolated", QualifiedName: "widgets.Isolated", Kind: "func",
				Start: Position{Line: 1, Offset: 0}, End: Position{Line: 3, Offset: 40},
			},
		},
		Evidence: []Evidence{
			{Key: evidenceKey, RepositoryKey: repoKey, FileKey: fileAKey, Start: Position{Line: 6, Column: 4, Offset: 40}, End: Position{Line: 6, Column: 9, Offset: 46}, Text: "Bar()"},
		},
		Edges: []Edge{
			{Kind: ContainsPackage, SourceKey: repoKey, TargetKey: pkgKey, Confidence: StructuralCertain, Provenance: PackageManifest},
			{Kind: ContainsFile, SourceKey: pkgKey, TargetKey: fileAKey, Confidence: StructuralCertain, Provenance: PackageManifest},
			{Kind: ContainsFile, SourceKey: pkgKey, TargetKey: fileBKey, Confidence: StructuralCertain, Provenance: PackageManifest},
			{Kind: ContainsFile, SourceKey: pkgKey, TargetKey: fileIsolatedKey, Confidence: StructuralCertain, Provenance: PackageManifest},
			{Kind: Defines, SourceKey: fileAKey, TargetKey: fooKey, Confidence: StructuralCertain, Provenance: GoTypesDefinition},
			{Kind: Defines, SourceKey: fileAKey, TargetKey: fooHelperKey, Confidence: StructuralCertain, Provenance: GoTypesDefinition},
			{Kind: Defines, SourceKey: fileBKey, TargetKey: barKey, Confidence: StructuralCertain, Provenance: GoTypesDefinition},
			{Kind: Defines, SourceKey: fileIsolatedKey, TargetKey: isolatedKey, Confidence: StructuralCertain, Provenance: GoTypesDefinition},
			{Kind: CallsDirect, SourceKey: fooKey, TargetKey: barKey, Confidence: ExactTypechecked, Provenance: GoASTCall, EvidenceKey: evidenceKey},
		},
		Unresolved: []UnresolvedReference{
			{
				RepositoryKey: repoKey, FileKey: fileAKey, Language: LanguageGo, SourceSymbolKey: fooKey,
				RequestedPackage: "acme/missing", RequestedSymbol: "Thing", Reason: "package_not_found",
				Start: Position{Line: 7, Column: 2, Offset: 80},
			},
		},
	}
	set.Sort()
	return set
}

// --- generic test helpers -----------------------------------------------

func contains[T any](items []T, predicate func(T) bool) bool {
	for _, item := range items {
		if predicate(item) {
			return true
		}
	}
	return false
}

func filterOut[T any](items []T, drop func(T) bool) []T {
	var filtered []T
	for _, item := range items {
		if !drop(item) {
			filtered = append(filtered, item)
		}
	}
	return filtered
}

func removeEdge(edges []Edge, kind EdgeKind, source, target string) []Edge {
	return filterOut(edges, func(edge Edge) bool {
		return edge.Kind == kind && edge.SourceKey == source && edge.TargetKey == target
	})
}

func removeEvidenceKey(evidence []Evidence, key string) []Evidence {
	return filterOut(evidence, func(entry Evidence) bool { return entry.Key == key })
}

func symbolsForFile(symbols []Symbol, fileKey string) []Symbol {
	var filtered []Symbol
	for _, symbol := range symbols {
		if symbol.FileKey == fileKey {
			filtered = append(filtered, symbol)
		}
	}
	return filtered
}

// removeFileAndItsFacts simulates what a valid producer hands Diff as next
// when a file disappears: not just the File row, but every symbol/evidence/
// unresolved reference anchored on it, every edge that would otherwise
// dangle (touching the file itself or one of its now-gone symbols, on
// either end), and any evidence left orphaned by a retracted edge — exactly
// what Set.Validate demands of a closed, self-consistent set.
func removeFileAndItsFacts(set Set, fileKey string) Set {
	removedSymbols := make(map[string]struct{})
	files := filterOut(set.Files, func(file File) bool { return file.Key == fileKey })
	symbols := filterOut(set.Symbols, func(symbol Symbol) bool {
		if symbol.FileKey == fileKey {
			removedSymbols[symbol.Key] = struct{}{}
			return true
		}
		return false
	})
	edges := filterOut(set.Edges, func(edge Edge) bool {
		if edge.SourceKey == fileKey || edge.TargetKey == fileKey {
			return true
		}
		_, sourceGone := removedSymbols[edge.SourceKey]
		_, targetGone := removedSymbols[edge.TargetKey]
		return sourceGone || targetGone
	})
	liveEvidence := make(map[string]struct{}, len(edges))
	for _, edge := range edges {
		if edge.EvidenceKey != "" {
			liveEvidence[edge.EvidenceKey] = struct{}{}
		}
	}
	evidence := filterOut(set.Evidence, func(entry Evidence) bool {
		if entry.FileKey == fileKey {
			return true
		}
		_, referenced := liveEvidence[entry.Key]
		return !referenced
	})
	unresolved := filterOut(set.Unresolved, func(entry UnresolvedReference) bool { return entry.FileKey == fileKey })

	result := Set{
		Repositories: append([]Repository(nil), set.Repositories...),
		Packages:     append([]Package(nil), set.Packages...),
		Files:        files,
		Symbols:      symbols,
		Evidence:     evidence,
		Edges:        edges,
		Unresolved:   unresolved,
	}
	result.Sort()
	return result
}

// applyDelta simulates, for test purposes, what a transactional applier is
// expected to do with a Delta: withdraw everything anchored on
// ReplacedFiles and RemovedFiles — cascading in both directions, exactly as
// design decision 2 requires, so a reference into a retracted file never
// survives its target — then merge in Upsert. This is deliberately an
// independent implementation from Diff/edgeAnchor: it exists to catch a
// Diff that computes the wrong thing, so it must not share Diff's
// machinery.
func applyDelta(t *testing.T, previous Set, delta Delta) Set {
	t.Helper()
	retractedFiles := make(map[string]struct{}, len(delta.ReplacedFiles)+len(delta.RemovedFiles))
	for _, key := range delta.ReplacedFiles {
		retractedFiles[key] = struct{}{}
	}
	for _, key := range delta.RemovedFiles {
		retractedFiles[key] = struct{}{}
	}

	retractedSymbols := make(map[string]struct{})
	result := Set{
		Repositories: append([]Repository(nil), previous.Repositories...),
		Packages:     append([]Package(nil), previous.Packages...),
	}
	for _, file := range previous.Files {
		if _, gone := retractedFiles[file.Key]; !gone {
			result.Files = append(result.Files, file)
		}
	}
	for _, symbol := range previous.Symbols {
		if _, gone := retractedFiles[symbol.FileKey]; gone {
			retractedSymbols[symbol.Key] = struct{}{}
			continue
		}
		result.Symbols = append(result.Symbols, symbol)
	}
	for _, entry := range previous.Evidence {
		if _, gone := retractedFiles[entry.FileKey]; !gone {
			result.Evidence = append(result.Evidence, entry)
		}
	}
	for _, edge := range previous.Edges {
		_, sourceFileGone := retractedFiles[edge.SourceKey]
		_, targetFileGone := retractedFiles[edge.TargetKey]
		_, sourceSymbolGone := retractedSymbols[edge.SourceKey]
		_, targetSymbolGone := retractedSymbols[edge.TargetKey]
		if sourceFileGone || targetFileGone || sourceSymbolGone || targetSymbolGone {
			continue
		}
		result.Edges = append(result.Edges, edge)
	}
	for _, entry := range previous.Unresolved {
		if entry.FileKey != "" {
			if _, gone := retractedFiles[entry.FileKey]; gone {
				continue
			}
		}
		result.Unresolved = append(result.Unresolved, entry)
	}

	result.Merge(delta.Upsert)
	return result
}

// assertSetsEqual compares two sets field by field, treating an empty
// collection as equal regardless of whether it is nil or zero length:
// Set.Merge always allocates a fresh (possibly zero length) slice for every
// field, hand written fixture literals usually leave an unused field nil,
// and that difference carries no meaning.
func assertSetsEqual(t *testing.T, got, want Set) {
	t.Helper()
	got.Sort()
	want.Sort()
	got = normalizeSet(got)
	want = normalizeSet(want)

	if !reflect.DeepEqual(got.Repositories, want.Repositories) {
		t.Fatalf("Repositories differ:\n got  = %+v\n want = %+v", got.Repositories, want.Repositories)
	}
	if !equalSlice(got.Packages, want.Packages) {
		t.Fatalf("Packages differ:\n got  = %+v\n want = %+v", got.Packages, want.Packages)
	}
	if !equalSlice(got.Files, want.Files) {
		t.Fatalf("Files differ:\n got  = %+v\n want = %+v", got.Files, want.Files)
	}
	if !equalSlice(got.Symbols, want.Symbols) {
		t.Fatalf("Symbols differ:\n got  = %+v\n want = %+v", got.Symbols, want.Symbols)
	}
	if !equalSlice(got.Evidence, want.Evidence) {
		t.Fatalf("Evidence differs:\n got  = %+v\n want = %+v", got.Evidence, want.Evidence)
	}
	if !equalSlice(got.Edges, want.Edges) {
		t.Fatalf("Edges differ:\n got  = %+v\n want = %+v", got.Edges, want.Edges)
	}
	if !equalSlice(got.Unresolved, want.Unresolved) {
		t.Fatalf("Unresolved differs:\n got  = %+v\n want = %+v", got.Unresolved, want.Unresolved)
	}
}

func normalizeSet(set Set) Set {
	if len(set.Repositories) == 0 {
		set.Repositories = nil
	}
	if len(set.Packages) == 0 {
		set.Packages = nil
	}
	if len(set.Files) == 0 {
		set.Files = nil
	}
	if len(set.Symbols) == 0 {
		set.Symbols = nil
	}
	if len(set.Evidence) == 0 {
		set.Evidence = nil
	}
	if len(set.Edges) == 0 {
		set.Edges = nil
	}
	if len(set.Unresolved) == 0 {
		set.Unresolved = nil
	}
	return set
}

func reverseSet(set Set) Set {
	reversed := Set{
		Repositories: append([]Repository(nil), set.Repositories...),
		Packages:     append([]Package(nil), set.Packages...),
		Files:        append([]File(nil), set.Files...),
		Symbols:      append([]Symbol(nil), set.Symbols...),
		Evidence:     append([]Evidence(nil), set.Evidence...),
		Edges:        append([]Edge(nil), set.Edges...),
		Unresolved:   append([]UnresolvedReference(nil), set.Unresolved...),
	}
	reverseRepositories(reversed.Repositories)
	reversePackages(reversed.Packages)
	reverseFiles(reversed.Files)
	reverseSymbols(reversed.Symbols)
	reverseEvidence(reversed.Evidence)
	reverseEdges(reversed.Edges)
	reverseUnresolved(reversed.Unresolved)
	return reversed
}

func reverseRepositories(items []Repository) {
	for left, right := 0, len(items)-1; left < right; left, right = left+1, right-1 {
		items[left], items[right] = items[right], items[left]
	}
}
func reversePackages(items []Package) {
	for left, right := 0, len(items)-1; left < right; left, right = left+1, right-1 {
		items[left], items[right] = items[right], items[left]
	}
}
func reverseFiles(items []File) {
	for left, right := 0, len(items)-1; left < right; left, right = left+1, right-1 {
		items[left], items[right] = items[right], items[left]
	}
}
func reverseSymbols(items []Symbol) {
	for left, right := 0, len(items)-1; left < right; left, right = left+1, right-1 {
		items[left], items[right] = items[right], items[left]
	}
}
func reverseEvidence(items []Evidence) {
	for left, right := 0, len(items)-1; left < right; left, right = left+1, right-1 {
		items[left], items[right] = items[right], items[left]
	}
}
func reverseEdges(items []Edge) {
	for left, right := 0, len(items)-1; left < right; left, right = left+1, right-1 {
		items[left], items[right] = items[right], items[left]
	}
}
func reverseUnresolved(items []UnresolvedReference) {
	for left, right := 0, len(items)-1; left < right; left, right = left+1, right-1 {
		items[left], items[right] = items[right], items[left]
	}
}

func newRepoPkgFileSymbolFixture(next *Set) (repoKey, pkgKey, fileKey, symbolKey string) {
	repoKey = RepositoryKey("acme/other")
	pkgKey = PackageKey(LanguageGo, repoKey, "other")
	fileKey = FileKey(repoKey, "d.go")
	symbolKey = "symbol:go:acme/other.Qux"

	next.Repositories = append(next.Repositories, Repository{Key: repoKey, Name: "acme/other", RootPath: "/repos/other", Languages: []Language{LanguageGo}})
	next.Packages = append(next.Packages, Package{Key: pkgKey, RepositoryKey: repoKey, Language: LanguageGo, Name: "other", RootPath: "/repos/other"})
	next.Files = append(next.Files, File{Key: fileKey, RepositoryKey: repoKey, PackageKey: pkgKey, Path: "d.go", Language: LanguageGo})
	next.Symbols = append(next.Symbols, Symbol{
		Key: symbolKey, CanonicalIdentity: "go:acme/other.Qux", RepositoryKey: repoKey, PackageKey: pkgKey,
		FileKey: fileKey, Language: LanguageGo, Name: "Qux", QualifiedName: "other.Qux", Kind: "func",
	})
	next.Edges = append(next.Edges,
		Edge{Kind: ContainsPackage, SourceKey: repoKey, TargetKey: pkgKey, Confidence: StructuralCertain, Provenance: PackageManifest},
		Edge{Kind: ContainsFile, SourceKey: pkgKey, TargetKey: fileKey, Confidence: StructuralCertain, Provenance: PackageManifest},
		Edge{Kind: Defines, SourceKey: fileKey, TargetKey: symbolKey, Confidence: StructuralCertain, Provenance: GoTypesDefinition},
	)
	return repoKey, pkgKey, fileKey, symbolKey
}

// --- Delta.Empty ----------------------------------------------------------

func TestDeltaEmpty(t *testing.T) {
	if !(Delta{}).Empty() {
		t.Fatal("zero value Delta.Empty() = false, want true")
	}
	if (Delta{RemovedFiles: []string{"file:x"}}).Empty() {
		t.Fatal("Delta with RemovedFiles: Empty() = true, want false")
	}
	if (Delta{ReplacedFiles: []string{"file:x"}}).Empty() {
		t.Fatal("Delta with ReplacedFiles: Empty() = true, want false")
	}
	if (Delta{Upsert: Set{Files: []File{{Key: "file:x", Path: "x"}}}}).Empty() {
		t.Fatal("Delta with a non empty Upsert: Empty() = true, want false")
	}
}

// --- Diff: core contract ---------------------------------------------------

func TestDiff_SelfIsEmpty(t *testing.T) {
	set := baseSet()
	delta, err := Diff(set, set)
	if err != nil {
		t.Fatalf("Diff: %v", err)
	}
	if !delta.Empty() {
		t.Fatalf("Diff(set, set).Empty() = false, want true; delta = %+v", delta)
	}
}

func TestDiff_AddFileInExistingPackage(t *testing.T) {
	previous := baseSet()
	next := baseSet()

	repoKey := widgetsRepoKey()
	pkgKey := widgetsPkgKey()
	fileCKey := widgetsFileKey("c.go")
	bazKey := "symbol:go:acme/widgets.Baz"

	next.Files = append(next.Files, File{Key: fileCKey, RepositoryKey: repoKey, PackageKey: pkgKey, Path: "c.go", Language: LanguageGo})
	next.Symbols = append(next.Symbols, Symbol{
		Key: bazKey, CanonicalIdentity: "go:acme/widgets.Baz", RepositoryKey: repoKey, PackageKey: pkgKey,
		FileKey: fileCKey, Language: LanguageGo, Name: "Baz", QualifiedName: "widgets.Baz", Kind: "func",
	})
	next.Edges = append(next.Edges,
		Edge{Kind: ContainsFile, SourceKey: pkgKey, TargetKey: fileCKey, Confidence: StructuralCertain, Provenance: PackageManifest},
		Edge{Kind: Defines, SourceKey: fileCKey, TargetKey: bazKey, Confidence: StructuralCertain, Provenance: GoTypesDefinition},
	)
	next.Sort()

	delta, err := Diff(previous, next)
	if err != nil {
		t.Fatalf("Diff: %v", err)
	}

	if len(delta.ReplacedFiles) != 0 {
		t.Fatalf("ReplacedFiles = %v, want none", delta.ReplacedFiles)
	}
	if len(delta.RemovedFiles) != 0 {
		t.Fatalf("RemovedFiles = %v, want none", delta.RemovedFiles)
	}
	if !contains(delta.Upsert.Files, func(f File) bool { return f.Key == fileCKey }) {
		t.Fatalf("Upsert.Files missing %q: %+v", fileCKey, delta.Upsert.Files)
	}
	if !contains(delta.Upsert.Symbols, func(s Symbol) bool { return s.Key == bazKey }) {
		t.Fatalf("Upsert.Symbols missing %q: %+v", bazKey, delta.Upsert.Symbols)
	}
	if !contains(delta.Upsert.Edges, func(e Edge) bool { return e.Kind == Defines && e.SourceKey == fileCKey && e.TargetKey == bazKey }) {
		t.Fatalf("Upsert.Edges missing Defines(%s,%s): %+v", fileCKey, bazKey, delta.Upsert.Edges)
	}
	if !contains(delta.Upsert.Edges, func(e Edge) bool { return e.Kind == ContainsFile && e.SourceKey == pkgKey && e.TargetKey == fileCKey }) {
		t.Fatalf("Upsert.Edges missing ContainsFile(%s,%s): %+v", pkgKey, fileCKey, delta.Upsert.Edges)
	}
	// The package/repository the new file needs travel with it, even though
	// neither is itself new.
	if !contains(delta.Upsert.Repositories, func(r Repository) bool { return r.Key == repoKey }) {
		t.Fatalf("Upsert.Repositories missing %q: %+v", repoKey, delta.Upsert.Repositories)
	}
	if !contains(delta.Upsert.Packages, func(p Package) bool { return p.Key == pkgKey }) {
		t.Fatalf("Upsert.Packages missing %q: %+v", pkgKey, delta.Upsert.Packages)
	}
}

func TestDiff_AddFileFromNewPackageBringsPackageAndRepository(t *testing.T) {
	previous := baseSet()
	next := baseSet()
	newRepoKey, newPkgKey, _, _ := newRepoPkgFileSymbolFixture(&next)
	next.Sort()

	delta, err := Diff(previous, next)
	if err != nil {
		t.Fatalf("Diff: %v", err)
	}

	if len(delta.ReplacedFiles) != 0 {
		t.Fatalf("ReplacedFiles = %v, want none", delta.ReplacedFiles)
	}
	if len(delta.RemovedFiles) != 0 {
		t.Fatalf("RemovedFiles = %v, want none", delta.RemovedFiles)
	}
	if !contains(delta.Upsert.Repositories, func(r Repository) bool { return r.Key == newRepoKey }) {
		t.Fatalf("Upsert.Repositories missing the new repository %q: %+v", newRepoKey, delta.Upsert.Repositories)
	}
	if !contains(delta.Upsert.Packages, func(p Package) bool { return p.Key == newPkgKey }) {
		t.Fatalf("Upsert.Packages missing the new package %q: %+v", newPkgKey, delta.Upsert.Packages)
	}
	// The package is not just present as a node: it must be reachable from
	// its repository, or it is a floating node no traversal ever reaches.
	if !contains(delta.Upsert.Edges, func(e Edge) bool {
		return e.Kind == ContainsPackage && e.SourceKey == newRepoKey && e.TargetKey == newPkgKey
	}) {
		t.Fatalf("Upsert.Edges missing CONTAINS_PACKAGE(%s,%s): %+v", newRepoKey, newPkgKey, delta.Upsert.Edges)
	}
	oldRepoKey := widgetsRepoKey()
	if contains(delta.Upsert.Repositories, func(r Repository) bool { return r.Key == oldRepoKey }) {
		t.Fatalf("Upsert.Repositories should not include the unrelated existing repository %q", oldRepoKey)
	}

	applied := applyDelta(t, previous, delta)
	assertSetsEqual(t, applied, next)
}

func TestDiff_RemoveFile(t *testing.T) {
	previous := baseSet()
	fileIsolatedKey := widgetsFileKey("isolated.go")
	next := removeFileAndItsFacts(previous, fileIsolatedKey)

	delta, err := Diff(previous, next)
	if err != nil {
		t.Fatalf("Diff: %v", err)
	}

	if len(delta.RemovedFiles) != 1 || delta.RemovedFiles[0] != fileIsolatedKey {
		t.Fatalf("RemovedFiles = %v, want [%s]", delta.RemovedFiles, fileIsolatedKey)
	}
	if len(delta.ReplacedFiles) != 0 {
		t.Fatalf("ReplacedFiles = %v, want none: removing an unrelated file must not touch fileA/fileB", delta.ReplacedFiles)
	}
	if !upsertEmpty(delta.Upsert) {
		t.Fatalf("Upsert = %+v, want empty: nothing new was asserted", delta.Upsert)
	}
	if delta.Empty() {
		t.Fatal("Empty() = true, want false: a file was removed")
	}

	applied := applyDelta(t, previous, delta)
	assertSetsEqual(t, applied, next)
}

func TestDiff_ModifySymbolReplacesItsFile(t *testing.T) {
	previous := baseSet()
	next := baseSet()
	for index := range next.Symbols {
		if next.Symbols[index].Key == fooKey {
			next.Symbols[index].Signature = "func Foo(x int) string"
		}
	}
	next.Sort()

	delta, err := Diff(previous, next)
	if err != nil {
		t.Fatalf("Diff: %v", err)
	}

	fileAKey := widgetsFileKey("a.go")
	if len(delta.ReplacedFiles) != 1 || delta.ReplacedFiles[0] != fileAKey {
		t.Fatalf("ReplacedFiles = %v, want [%s]", delta.ReplacedFiles, fileAKey)
	}
	if len(delta.RemovedFiles) != 0 {
		t.Fatalf("RemovedFiles = %v, want none", delta.RemovedFiles)
	}

	// The full, current symbol set of fileA must be restated: both the
	// changed Foo and the untouched FooHelper, not just the one that moved.
	fileASymbols := symbolsForFile(delta.Upsert.Symbols, fileAKey)
	if len(fileASymbols) != 2 {
		t.Fatalf("Upsert.Symbols for %s has %d entries, want 2: %+v", fileAKey, len(fileASymbols), fileASymbols)
	}
	if !contains(fileASymbols, func(s Symbol) bool { return s.Key == fooKey && s.Signature == "func Foo(x int) string" }) {
		t.Fatalf("Upsert.Symbols does not carry the updated Foo: %+v", fileASymbols)
	}
	if !contains(fileASymbols, func(s Symbol) bool { return s.Key == fooHelperKey }) {
		t.Fatalf("Upsert.Symbols dropped the unrelated FooHelper: %+v", fileASymbols)
	}
}

func TestDiff_EdgeDisappearsWithBothEndpointsIntact(t *testing.T) {
	previous := baseSet()
	next := baseSet()

	// Foo stops calling Bar; both symbols and both files are otherwise
	// completely untouched.
	next.Edges = removeEdge(next.Edges, CallsDirect, fooKey, barKey)
	next.Evidence = removeEvidenceKey(next.Evidence, widgetsCallEvidenceKey())
	next.Sort()

	delta, err := Diff(previous, next)
	if err != nil {
		t.Fatalf("Diff: %v", err)
	}

	fileAKey := widgetsFileKey("a.go")
	fileBKey := widgetsFileKey("b.go")

	if len(delta.ReplacedFiles) != 1 || delta.ReplacedFiles[0] != fileAKey {
		t.Fatalf("ReplacedFiles = %v, want [%s] (the file that anchored the call)", delta.ReplacedFiles, fileAKey)
	}
	if contains(delta.ReplacedFiles, func(key string) bool { return key == fileBKey }) {
		t.Fatalf("ReplacedFiles must not include %s: its own facts did not change", fileBKey)
	}
	if len(delta.RemovedFiles) != 0 {
		t.Fatalf("RemovedFiles = %v, want none", delta.RemovedFiles)
	}
	if contains(delta.Upsert.Edges, func(e Edge) bool { return e.Kind == CallsDirect && e.SourceKey == fooKey && e.TargetKey == barKey }) {
		t.Fatalf("Upsert.Edges still carries the retracted CallsDirect edge: %+v", delta.Upsert.Edges)
	}
	if !contains(delta.Upsert.Symbols, func(s Symbol) bool { return s.Key == fooKey }) {
		t.Fatalf("Upsert.Symbols dropped Foo, whose file was restated: %+v", delta.Upsert.Symbols)
	}

	applied := applyDelta(t, previous, delta)
	assertSetsEqual(t, applied, next)
}

// TestDiff_TargetDisappearsRetractsTheDanglingEdge is the "difficult case"
// called out by the ticket: an edge whose source lives in a file that does
// not change, and whose target's file disappears entirely. See Diff's
// inline comment on the fragment comparison for why the source file
// (fileA) also ends up replaced here — the same general mechanism that
// handles TestDiff_EdgeDisappearsWithBothEndpointsIntact, not a special
// case for this scenario.
func TestDiff_TargetDisappearsRetractsTheDanglingEdge(t *testing.T) {
	previous := baseSet()
	fileBKey := widgetsFileKey("b.go")
	next := removeFileAndItsFacts(previous, fileBKey)

	delta, err := Diff(previous, next)
	if err != nil {
		t.Fatalf("Diff: %v", err)
	}

	fileAKey := widgetsFileKey("a.go")

	if !contains(delta.RemovedFiles, func(key string) bool { return key == fileBKey }) {
		t.Fatalf("RemovedFiles = %v, want it to include %s", delta.RemovedFiles, fileBKey)
	}
	if !contains(delta.ReplacedFiles, func(key string) bool { return key == fileAKey }) {
		t.Fatalf("ReplacedFiles = %v, want it to include %s", delta.ReplacedFiles, fileAKey)
	}
	if contains(delta.Upsert.Symbols, func(s Symbol) bool { return s.Key == barKey }) {
		t.Fatalf("Upsert.Symbols must not resurrect %s", barKey)
	}
	if contains(delta.Upsert.Edges, func(e Edge) bool { return e.Kind == CallsDirect && e.TargetKey == barKey }) {
		t.Fatalf("Upsert.Edges must not carry an edge into the removed %s: %+v", barKey, delta.Upsert.Edges)
	}

	applied := applyDelta(t, previous, delta)
	assertSetsEqual(t, applied, next)
}

func TestDiff_FromEmptyPrevious(t *testing.T) {
	next := baseSet()
	delta, err := Diff(Set{}, next)
	if err != nil {
		t.Fatalf("Diff: %v", err)
	}
	if len(delta.RemovedFiles) != 0 {
		t.Fatalf("RemovedFiles = %v, want none", delta.RemovedFiles)
	}
	if len(delta.ReplacedFiles) != 0 {
		t.Fatalf("ReplacedFiles = %v, want none: every file is new", delta.ReplacedFiles)
	}
	if len(delta.Upsert.Files) != len(next.Files) {
		t.Fatalf("Upsert.Files has %d entries, want %d (every file in next)", len(delta.Upsert.Files), len(next.Files))
	}

	applied := applyDelta(t, Set{}, delta)
	assertSetsEqual(t, applied, next)
}

// TestDiff_ApplyingToPreviousReproducesNext is the test that gives the rest
// of the suite its value: it combines a removal, a replacement (a symbol
// edit plus an edge disappearing on the same file) and an addition (a
// brand new repository/package/file) in one delta, applies that delta to
// previous by simulation, and checks the result against next field by
// field.
func TestDiff_ApplyingToPreviousReproducesNext(t *testing.T) {
	previous := baseSet()

	fileIsolatedKey := widgetsFileKey("isolated.go")
	fileAKey := widgetsFileKey("a.go")
	fileBKey := widgetsFileKey("b.go")

	next := removeFileAndItsFacts(baseSet(), fileIsolatedKey)
	for index := range next.Symbols {
		if next.Symbols[index].Key == fooKey {
			next.Symbols[index].Signature = "func Foo(x int) string"
		}
	}
	next.Edges = removeEdge(next.Edges, CallsDirect, fooKey, barKey)
	next.Evidence = removeEvidenceKey(next.Evidence, widgetsCallEvidenceKey())
	newRepoKey, _, newFileKey, _ := newRepoPkgFileSymbolFixture(&next)
	next.Sort()

	if err := next.Validate(); err != nil {
		t.Fatalf("test fixture next is invalid: %v", err)
	}

	delta, err := Diff(previous, next)
	if err != nil {
		t.Fatalf("Diff: %v", err)
	}
	if err := delta.Validate(); err != nil {
		t.Fatalf("computed delta is invalid: %v", err)
	}

	if !contains(delta.RemovedFiles, func(key string) bool { return key == fileIsolatedKey }) {
		t.Fatalf("RemovedFiles = %v, want it to include %s", delta.RemovedFiles, fileIsolatedKey)
	}
	if !contains(delta.ReplacedFiles, func(key string) bool { return key == fileAKey }) {
		t.Fatalf("ReplacedFiles = %v, want it to include %s", delta.ReplacedFiles, fileAKey)
	}
	if contains(delta.ReplacedFiles, func(key string) bool { return key == fileBKey }) {
		t.Fatalf("ReplacedFiles must not include %s: it did not change", fileBKey)
	}
	if contains(delta.RemovedFiles, func(key string) bool { return key == fileBKey }) {
		t.Fatalf("RemovedFiles must not include %s", fileBKey)
	}
	if !contains(delta.Upsert.Files, func(f File) bool { return f.Key == newFileKey }) {
		t.Fatalf("Upsert.Files = %+v, want the new file %s", delta.Upsert.Files, newFileKey)
	}
	if !contains(delta.Upsert.Repositories, func(r Repository) bool { return r.Key == newRepoKey }) {
		t.Fatalf("Upsert.Repositories = %+v, want the new repository %s", delta.Upsert.Repositories, newRepoKey)
	}

	applied := applyDelta(t, previous, delta)
	assertSetsEqual(t, applied, next)
}

func TestDiff_DeterministicRegardlessOfInputOrder(t *testing.T) {
	previous := baseSet()
	next := baseSet()
	newRepoPkgFileSymbolFixture(&next)
	next.Sort()

	wantDelta, err := Diff(previous, next)
	if err != nil {
		t.Fatalf("Diff: %v", err)
	}

	gotDelta, err := Diff(reverseSet(previous), reverseSet(next))
	if err != nil {
		t.Fatalf("Diff on shuffled input: %v", err)
	}

	assertSetsEqual(t, gotDelta.Upsert, wantDelta.Upsert)
	if !equalSlice(gotDelta.ReplacedFiles, wantDelta.ReplacedFiles) {
		t.Fatalf("ReplacedFiles is not order independent:\n got  = %v\n want = %v", gotDelta.ReplacedFiles, wantDelta.ReplacedFiles)
	}
	if !equalSlice(gotDelta.RemovedFiles, wantDelta.RemovedFiles) {
		t.Fatalf("RemovedFiles is not order independent:\n got  = %v\n want = %v", gotDelta.RemovedFiles, wantDelta.RemovedFiles)
	}
	if !sort.StringsAreSorted(gotDelta.ReplacedFiles) {
		t.Fatalf("ReplacedFiles is not sorted: %v", gotDelta.ReplacedFiles)
	}
	if !sort.StringsAreSorted(gotDelta.RemovedFiles) {
		t.Fatalf("RemovedFiles is not sorted: %v", gotDelta.RemovedFiles)
	}
}

func TestDiff_RejectsInvalidPrevious(t *testing.T) {
	previous := baseSet()
	previous.Edges = append(previous.Edges, Edge{Kind: CallsDirect, SourceKey: "symbol:does-not-exist", TargetKey: "symbol:also-missing", Confidence: Candidate, Provenance: GoTypesUse})
	previous.Sort()

	_, err := Diff(previous, baseSet())
	if err == nil {
		t.Fatal("Diff() = nil error, want one: previous is not a valid, closed fact set")
	}
	if !errors.Is(err, ErrInvalidDelta) {
		t.Fatalf("Diff() error = %v, want it to wrap ErrInvalidDelta", err)
	}
	if !errors.Is(err, ErrInvalidFacts) {
		t.Fatalf("Diff() error = %v, want it to also wrap ErrInvalidFacts", err)
	}
}

func TestDiff_RejectsInvalidNext(t *testing.T) {
	next := baseSet()
	next.Edges = append(next.Edges, Edge{Kind: CallsDirect, SourceKey: "symbol:does-not-exist", TargetKey: "symbol:also-missing", Confidence: Candidate, Provenance: GoTypesUse})
	next.Sort()

	_, err := Diff(baseSet(), next)
	if err == nil {
		t.Fatal("Diff() = nil error, want one: next is not a valid, closed fact set")
	}
	if !errors.Is(err, ErrInvalidDelta) {
		t.Fatalf("Diff() error = %v, want it to wrap ErrInvalidDelta", err)
	}
	if !errors.Is(err, ErrInvalidFacts) {
		t.Fatalf("Diff() error = %v, want it to also wrap ErrInvalidFacts", err)
	}
}

// --- Delta.Validate ---------------------------------------------------

func TestDeltaValidate_RejectsFileInBothLists(t *testing.T) {
	fileKey := widgetsFileKey("a.go")
	delta := Delta{ReplacedFiles: []string{fileKey}, RemovedFiles: []string{fileKey}}

	err := delta.Validate()
	if err == nil {
		t.Fatal("Validate() = nil, want an error: a file cannot be both replaced and removed")
	}
	if !errors.Is(err, ErrInvalidDelta) {
		t.Fatalf("Validate() error = %v, want it to wrap ErrInvalidDelta", err)
	}
}

func TestDeltaValidate_AcceptsEdgesPointingOutsideTheFragment(t *testing.T) {
	repoKey := widgetsRepoKey()
	pkgKey := widgetsPkgKey()
	fileAKey := widgetsFileKey("a.go")

	upsert := Set{
		Repositories: []Repository{{Key: repoKey, Name: "acme/widgets", RootPath: "/repos/widgets"}},
		Packages:     []Package{{Key: pkgKey, RepositoryKey: repoKey, Language: LanguageGo, Name: "widgets", RootPath: "/repos/widgets"}},
		Files:        []File{{Key: fileAKey, RepositoryKey: repoKey, PackageKey: pkgKey, Path: "a.go", Language: LanguageGo}},
		Symbols: []Symbol{
			{Key: fooKey, CanonicalIdentity: "go:acme/widgets.Foo", RepositoryKey: repoKey, PackageKey: pkgKey, FileKey: fileAKey, Language: LanguageGo, Name: "Foo", QualifiedName: "widgets.Foo", Kind: "func"},
		},
		Edges: []Edge{
			// barKey lives in a file this fragment does not carry.
			{Kind: CallsDirect, SourceKey: fooKey, TargetKey: barKey, Confidence: ExactTypechecked, Provenance: GoASTCall},
		},
	}
	delta := Delta{ReplacedFiles: []string{fileAKey}, Upsert: upsert}

	if err := delta.Validate(); err != nil {
		t.Fatalf("Validate() = %v, want nil: an edge may legitimately point outside the fragment", err)
	}
}

func TestDeltaValidate_RejectsSymbolWithoutItsFile(t *testing.T) {
	repoKey := widgetsRepoKey()
	pkgKey := widgetsPkgKey()
	fileAKey := widgetsFileKey("a.go")

	upsert := Set{
		Repositories: []Repository{{Key: repoKey, Name: "acme/widgets", RootPath: "/repos/widgets"}},
		Packages:     []Package{{Key: pkgKey, RepositoryKey: repoKey, Language: LanguageGo, Name: "widgets", RootPath: "/repos/widgets"}},
		// fileAKey deliberately omitted from Files.
		Symbols: []Symbol{
			{Key: fooKey, CanonicalIdentity: "go:acme/widgets.Foo", RepositoryKey: repoKey, PackageKey: pkgKey, FileKey: fileAKey, Language: LanguageGo, Name: "Foo", QualifiedName: "widgets.Foo", Kind: "func"},
		},
	}
	delta := Delta{Upsert: upsert}

	err := delta.Validate()
	if err == nil {
		t.Fatal("Validate() = nil, want an error: a symbol's own file must travel with it")
	}
	if !errors.Is(err, ErrInvalidDelta) {
		t.Fatalf("Validate() error = %v, want it to wrap ErrInvalidDelta", err)
	}
}

func TestDeltaValidate_RejectsDuplicateFile(t *testing.T) {
	repoKey := widgetsRepoKey()
	pkgKey := widgetsPkgKey()
	fileAKey := widgetsFileKey("a.go")
	file := File{Key: fileAKey, RepositoryKey: repoKey, PackageKey: pkgKey, Path: "a.go", Language: LanguageGo}

	upsert := Set{
		Repositories: []Repository{{Key: repoKey, Name: "acme/widgets", RootPath: "/repos/widgets"}},
		Packages:     []Package{{Key: pkgKey, RepositoryKey: repoKey, Language: LanguageGo, Name: "widgets", RootPath: "/repos/widgets"}},
		Files:        []File{file, file},
	}
	delta := Delta{Upsert: upsert}

	err := delta.Validate()
	if err == nil {
		t.Fatal("Validate() = nil, want an error: Upsert carries the same file twice")
	}
	if !errors.Is(err, ErrInvalidDelta) {
		t.Fatalf("Validate() error = %v, want it to wrap ErrInvalidDelta", err)
	}
}
