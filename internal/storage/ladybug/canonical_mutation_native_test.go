//go:build ladybug && cgo

package ladybug

import (
	"context"
	"errors"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/Luqueee/kivgraph/internal/facts"
)

// Fixture identity shared by every test in this file: one repository, one
// package, both of which every delta below repeats in its own Upsert
// (design decision 4 -- a file's repository and package always travel
// alongside it) and which ApplyCanonicalDelta's node upsert must accept as
// an idempotent no-op re-assertion, never a failure.
const (
	mutationFixtureRepositoryName = "repoM"
	mutationFixturePackageName    = "pkgM"
)

var (
	mutationFixtureRepositoryKey = facts.RepositoryKey(mutationFixtureRepositoryName)
	mutationFixturePackageKey    = facts.PackageKey(facts.LanguageGo, mutationFixtureRepositoryName, mutationFixturePackageName)
)

func mutationFixtureRepository() facts.Repository {
	return facts.Repository{
		Key: mutationFixtureRepositoryKey, Name: mutationFixtureRepositoryName, RootPath: "/repos/" + mutationFixtureRepositoryName,
		Languages: []facts.Language{facts.LanguageGo},
	}
}

func mutationFixturePackage() facts.Package {
	return facts.Package{
		Key: mutationFixturePackageKey, RepositoryKey: mutationFixtureRepositoryKey, Language: facts.LanguageGo,
		Name: mutationFixturePackageName, RootPath: "/repos/" + mutationFixtureRepositoryName,
	}
}

func mutationFixtureFile(path, contentHash string) facts.File {
	return facts.File{
		Key: facts.FileKey(mutationFixtureRepositoryName, path), RepositoryKey: mutationFixtureRepositoryKey,
		PackageKey: mutationFixturePackageKey, Path: path, Language: facts.LanguageGo, ContentHash: contentHash,
	}
}

func mutationFixtureSymbol(key string, file facts.File, name string) facts.Symbol {
	return facts.Symbol{
		Key: key, CanonicalIdentity: "go:" + key, RepositoryKey: mutationFixtureRepositoryKey, PackageKey: mutationFixturePackageKey,
		FileKey: file.Key, Language: facts.LanguageGo, Name: name, QualifiedName: name, Kind: "function", Exported: true,
		Signature: "func " + name + "()", Start: facts.Position{Line: 1}, End: facts.Position{Line: 3, Offset: 20},
	}
}

// mutationContainmentEdges renders CONTAINS_PACKAGE + one CONTAINS_FILE per
// file: the structural skeleton every fixture below needs regardless of
// what else it contains.
func mutationContainmentEdges(files ...facts.File) []facts.Edge {
	edges := []facts.Edge{
		{Kind: facts.ContainsPackage, SourceKey: mutationFixtureRepositoryKey, TargetKey: mutationFixturePackageKey, Confidence: facts.StructuralCertain, Provenance: facts.PackageManifest},
	}
	for _, file := range files {
		edges = append(edges, facts.Edge{Kind: facts.ContainsFile, SourceKey: mutationFixturePackageKey, TargetKey: file.Key, Confidence: facts.StructuralCertain, Provenance: facts.PackageManifest})
	}
	return edges
}

func mutationDefinesEdge(file facts.File, symbolKey string) facts.Edge {
	return facts.Edge{Kind: facts.Defines, SourceKey: file.Key, TargetKey: symbolKey, Confidence: facts.StructuralCertain, Provenance: facts.GoTypesDefinition}
}

// loadMutationFixture publishes set at a fresh path via LoadCanonical and
// fails the test immediately on any error.
func loadMutationFixture(t *testing.T, ctx context.Context, set facts.Set, options CanonicalLoadOptions) string {
	t.Helper()
	set.Sort()
	if err := set.Validate(); err != nil {
		t.Fatalf("fixture set is invalid: %v", err)
	}
	path := filepath.Join(t.TempDir(), "graph.db")
	if _, err := LoadCanonical(ctx, path, set, options); err != nil {
		t.Fatalf("LoadCanonical() error = %v", err)
	}
	return path
}

func mustCanonicalCounts(t *testing.T, ctx context.Context, path string) map[string]int64 {
	t.Helper()
	counts, err := CanonicalTableCounts(ctx, path)
	if err != nil {
		t.Fatalf("CanonicalTableCounts() error = %v", err)
	}
	return counts
}

func requireCleanIntegrity(t *testing.T, ctx context.Context, path string) {
	t.Helper()
	report, err := VerifyCanonicalIntegrity(ctx, path)
	if err != nil {
		t.Fatalf("VerifyCanonicalIntegrity() error = %v", err)
	}
	if !report.Passed {
		t.Fatalf("VerifyCanonicalIntegrity() did not pass: %#v", report.Findings)
	}
}

// (a) Adding a new file with its symbols and edges leaves the graph with
// the expected counts and a clean VerifyCanonicalIntegrity.
func TestApplyCanonicalDeltaAddsNewFileWithSymbolsAndEdges(t *testing.T) {
	ctx := context.Background()
	helperFile := mutationFixtureFile("helper.go", "helper-h1")
	helperKey := "symbol:repoM:helper.go:Helper"
	base := facts.Set{
		Repositories: []facts.Repository{mutationFixtureRepository()},
		Packages:     []facts.Package{mutationFixturePackage()},
		Files:        []facts.File{helperFile},
		Symbols:      []facts.Symbol{mutationFixtureSymbol(helperKey, helperFile, "Helper")},
		Edges:        append(mutationContainmentEdges(helperFile), mutationDefinesEdge(helperFile, helperKey)),
	}
	loadOptions := CanonicalLoadOptions{SnapshotID: 1, ResolverVersion: "v1"}
	path := loadMutationFixture(t, ctx, base, loadOptions)
	before := mustCanonicalCounts(t, ctx, path)

	newFile := mutationFixtureFile("new.go", "new-h1")
	newFuncKey := "symbol:repoM:new.go:NewFunc"
	delta := facts.Delta{
		Upsert: facts.Set{
			Repositories: []facts.Repository{mutationFixtureRepository()},
			Packages:     []facts.Package{mutationFixturePackage()},
			Files:        []facts.File{newFile},
			Symbols:      []facts.Symbol{mutationFixtureSymbol(newFuncKey, newFile, "NewFunc")},
			Edges: []facts.Edge{
				{Kind: facts.ContainsFile, SourceKey: mutationFixturePackageKey, TargetKey: newFile.Key, Confidence: facts.StructuralCertain, Provenance: facts.PackageManifest},
				mutationDefinesEdge(newFile, newFuncKey),
				// NewFunc calls the already published Helper: Helper is
				// neither new nor replaced, so it is not itself part of
				// Upsert -- this exercises the external endpoint
				// completion canonicalUpsertRows performs.
				{Kind: facts.CallsDirect, SourceKey: newFuncKey, TargetKey: helperKey, Confidence: facts.ExactTypechecked, Provenance: facts.GoASTCall},
			},
		},
	}
	applyOptions := CanonicalLoadOptions{SnapshotID: 2, ResolverVersion: "v2"}

	result, err := ApplyCanonicalDelta(ctx, path, delta, applyOptions)
	if err != nil {
		t.Fatalf("ApplyCanonicalDelta() error = %v", err)
	}
	want := CanonicalMutationResult{
		UpsertedNodes: 8, // GraphMetadata(4) + Repository + Package + File + Symbol
		UpsertedEdges: 3, // CONTAINS_FILE + DEFINES + CALLS_DIRECT
	}
	if result != want {
		t.Fatalf("ApplyCanonicalDelta() result = %#v, want %#v", result, want)
	}

	after := mustCanonicalCounts(t, ctx, path)
	for table, delta := range map[string]int64{"File": 1, "Symbol": 1, "CONTAINS_FILE": 1, "DEFINES": 1, "CALLS_DIRECT": 1} {
		if after[table] != before[table]+delta {
			t.Fatalf("counts[%s] = %d, want %d (before %d)", table, after[table], before[table]+delta, before[table])
		}
	}
	requireCleanIntegrity(t, ctx, path)
}

// (b) Replacing a file substitutes its symbols and edges for the new ones,
// leaving no trace of the old ones -- including the evidence the old
// internal edge carried.
func TestApplyCanonicalDeltaReplacesFileContentWithoutTraceOfOld(t *testing.T) {
	ctx := context.Background()
	helperFile := mutationFixtureFile("helper.go", "helper-h1")
	oldAKey := "symbol:repoM:helper.go:OldA"
	oldBKey := "symbol:repoM:helper.go:OldB"
	evidenceKey := facts.EvidenceKey(helperFile.Key, 10, 20)
	base := facts.Set{
		Repositories: []facts.Repository{mutationFixtureRepository()},
		Packages:     []facts.Package{mutationFixturePackage()},
		Files:        []facts.File{helperFile},
		Symbols: []facts.Symbol{
			mutationFixtureSymbol(oldAKey, helperFile, "OldA"),
			mutationFixtureSymbol(oldBKey, helperFile, "OldB"),
		},
		Evidence: []facts.Evidence{
			{Key: evidenceKey, RepositoryKey: mutationFixtureRepositoryKey, FileKey: helperFile.Key, Start: facts.Position{Line: 2, Offset: 10}, End: facts.Position{Line: 2, Offset: 18}, Text: "OldB()"},
		},
		Edges: append(mutationContainmentEdges(helperFile),
			mutationDefinesEdge(helperFile, oldAKey),
			mutationDefinesEdge(helperFile, oldBKey),
			facts.Edge{Kind: facts.CallsDirect, SourceKey: oldAKey, TargetKey: oldBKey, Confidence: facts.ExactTypechecked, Provenance: facts.GoASTCall, EvidenceKey: evidenceKey},
		),
	}
	loadOptions := CanonicalLoadOptions{SnapshotID: 1, ResolverVersion: "v1"}
	path := loadMutationFixture(t, ctx, base, loadOptions)
	before := mustCanonicalCounts(t, ctx, path)
	if before["Symbol"] != 2 || before["Evidence"] != 1 || before["CALLS_DIRECT"] != 1 {
		t.Fatalf("fixture sanity check failed: %#v", before)
	}

	helperFileV2 := mutationFixtureFile("helper.go", "helper-h2")
	newAKey := "symbol:repoM:helper.go:NewA"
	delta := facts.Delta{
		ReplacedFiles: []string{helperFile.Key},
		Upsert: facts.Set{
			Repositories: []facts.Repository{mutationFixtureRepository()},
			Packages:     []facts.Package{mutationFixturePackage()},
			Files:        []facts.File{helperFileV2},
			Symbols:      []facts.Symbol{mutationFixtureSymbol(newAKey, helperFileV2, "NewA")},
			Edges: []facts.Edge{
				{Kind: facts.ContainsFile, SourceKey: mutationFixturePackageKey, TargetKey: helperFileV2.Key, Confidence: facts.StructuralCertain, Provenance: facts.PackageManifest},
				mutationDefinesEdge(helperFileV2, newAKey),
			},
		},
	}
	applyOptions := CanonicalLoadOptions{SnapshotID: 2, ResolverVersion: "v2"}

	result, err := ApplyCanonicalDelta(ctx, path, delta, applyOptions)
	if err != nil {
		t.Fatalf("ApplyCanonicalDelta() error = %v", err)
	}
	want := CanonicalMutationResult{
		RemovedSymbols: 2, RemovedEvidence: 1, RemovedEdges: 4, // DEFINES x2 + CALLS_DIRECT + OBSERVED_IN
		UpsertedNodes: 8, // GraphMetadata(4) + Repository + Package + File + Symbol
		UpsertedEdges: 2, // CONTAINS_FILE (updated in place) + DEFINES
	}
	if result != want {
		t.Fatalf("ApplyCanonicalDelta() result = %#v, want %#v", result, want)
	}

	after := mustCanonicalCounts(t, ctx, path)
	if after["File"] != 1 {
		t.Fatalf("counts[File] = %d, want 1 (the file persists, rewritten)", after["File"])
	}
	if after["Symbol"] != 1 || after["Evidence"] != 0 || after["CALLS_DIRECT"] != 0 || after["DEFINES"] != 1 {
		t.Fatalf("counts after replace = %#v, want Symbol=1 Evidence=0 CALLS_DIRECT=0 DEFINES=1", after)
	}

	// The old symbols/evidence must be gone by key, not merely by count.
	for _, missing := range []string{oldAKey, oldBKey} {
		symbol, found, err := scanSymbolByKey(ctx, path, missing)
		if err != nil {
			t.Fatalf("scan for %s: %v", missing, err)
		}
		if found {
			t.Fatalf("old symbol %s survived the replace: %#v", missing, symbol)
		}
	}
	requireCleanIntegrity(t, ctx, path)
}

// (c) Removing a file whose symbols were the target of an edge from
// another, untouched file leaves no dangling edge behind: the whole point
// of this task.
func TestApplyCanonicalDeltaRemovingFileClearsIncomingEdgesFromOtherFiles(t *testing.T) {
	ctx := context.Background()
	mainFile := mutationFixtureFile("main.go", "main-h1")
	oldFile := mutationFixtureFile("old.go", "old-h1")
	mainKey := "symbol:repoM:main.go:Main"
	oldFuncKey := "symbol:repoM:old.go:OldFunc"
	base := facts.Set{
		Repositories: []facts.Repository{mutationFixtureRepository()},
		Packages:     []facts.Package{mutationFixturePackage()},
		Files:        []facts.File{mainFile, oldFile},
		Symbols: []facts.Symbol{
			mutationFixtureSymbol(mainKey, mainFile, "Main"),
			mutationFixtureSymbol(oldFuncKey, oldFile, "OldFunc"),
		},
		Unresolved: []facts.UnresolvedReference{
			{RepositoryKey: mutationFixtureRepositoryKey, FileKey: oldFile.Key, Language: facts.LanguageGo, SourceSymbolKey: oldFuncKey, RequestedPackage: "missing", RequestedSymbol: "X", Reason: "package_not_indexed", Start: facts.Position{Line: 2, Offset: 5}},
		},
		Edges: append(mutationContainmentEdges(mainFile, oldFile),
			mutationDefinesEdge(mainFile, mainKey),
			mutationDefinesEdge(oldFile, oldFuncKey),
			// Main (untouched) references OldFunc (about to be removed):
			// exactly the "destino desaparece" case.
			facts.Edge{Kind: facts.References, SourceKey: mainKey, TargetKey: oldFuncKey, Confidence: facts.ExactTypechecked, Provenance: facts.GoTypesUse},
		),
	}
	loadOptions := CanonicalLoadOptions{SnapshotID: 1, ResolverVersion: "v1"}
	path := loadMutationFixture(t, ctx, base, loadOptions)
	before := mustCanonicalCounts(t, ctx, path)
	if before["REFERENCES"] != 1 || before["UnresolvedReference"] != 1 {
		t.Fatalf("fixture sanity check failed: %#v", before)
	}
	requireCleanIntegrity(t, ctx, path)

	delta := facts.Delta{RemovedFiles: []string{oldFile.Key}}
	applyOptions := CanonicalLoadOptions{SnapshotID: 2, ResolverVersion: "v2"}

	result, err := ApplyCanonicalDelta(ctx, path, delta, applyOptions)
	if err != nil {
		t.Fatalf("ApplyCanonicalDelta() error = %v", err)
	}
	want := CanonicalMutationResult{
		RemovedFiles: 1, RemovedSymbols: 1,
		RemovedEdges:  4, // DEFINES(old.go->OldFunc) + REFERENCES(Main->OldFunc) + REPORTS_UNRESOLVED + CONTAINS_FILE(pkg->old.go)
		UpsertedNodes: 4, // GraphMetadata only: Upsert is otherwise empty
	}
	if result != want {
		t.Fatalf("ApplyCanonicalDelta() result = %#v, want %#v", result, want)
	}

	after := mustCanonicalCounts(t, ctx, path)
	for table, want := range map[string]int64{"File": 1, "Symbol": 1, "REFERENCES": 0, "DEFINES": 1, "CONTAINS_FILE": 1, "UnresolvedReference": 0, "REPORTS_UNRESOLVED": 0} {
		if after[table] != want {
			t.Fatalf("counts[%s] = %d, want %d: %#v", table, after[table], want, after)
		}
	}

	// The specific invariant this task exists for.
	report, err := VerifyCanonicalIntegrity(ctx, path)
	if err != nil {
		t.Fatalf("VerifyCanonicalIntegrity() error = %v", err)
	}
	finding, ok := report.Finding(RuleExactEdgeWithoutTarget)
	if !ok {
		t.Fatalf("report has no finding for %s", RuleExactEdgeWithoutTarget)
	}
	if finding.Violations != 0 {
		t.Fatalf("%s = %d violations, want 0: %#v", RuleExactEdgeWithoutTarget, finding.Violations, finding.Samples)
	}
	if !report.Passed {
		t.Fatalf("VerifyCanonicalIntegrity() did not pass: %#v", report.Findings)
	}
}

// (d) An Upsert edge to a nonexistent symbol fails with
// ErrInvalidCanonicalDelta and leaves the database untouched, even though
// the same delta's node upsert (a real new file and symbol) would otherwise
// have succeeded on its own -- proving the whole delta is one transaction,
// not node-then-maybe-edges.
func TestApplyCanonicalDeltaRejectsDanglingUpsertEdgeWithoutModifyingDatabase(t *testing.T) {
	ctx := context.Background()
	mainFile := mutationFixtureFile("main.go", "main-h1")
	mainKey := "symbol:repoM:main.go:Main"
	base := facts.Set{
		Repositories: []facts.Repository{mutationFixtureRepository()},
		Packages:     []facts.Package{mutationFixturePackage()},
		Files:        []facts.File{mainFile},
		Symbols:      []facts.Symbol{mutationFixtureSymbol(mainKey, mainFile, "Main")},
		Edges:        append(mutationContainmentEdges(mainFile), mutationDefinesEdge(mainFile, mainKey)),
	}
	loadOptions := CanonicalLoadOptions{SnapshotID: 1, ResolverVersion: "v1"}
	path := loadMutationFixture(t, ctx, base, loadOptions)
	before := mustCanonicalCounts(t, ctx, path)
	beforeGraph, err := ScanCanonical(ctx, path)
	if err != nil {
		t.Fatalf("ScanCanonical() before error = %v", err)
	}

	extraFile := mutationFixtureFile("extra.go", "extra-h1")
	extraKey := "symbol:repoM:extra.go:Extra"
	delta := facts.Delta{
		Upsert: facts.Set{
			Repositories: []facts.Repository{mutationFixtureRepository()},
			Packages:     []facts.Package{mutationFixturePackage()},
			Files:        []facts.File{extraFile},
			Symbols:      []facts.Symbol{mutationFixtureSymbol(extraKey, extraFile, "Extra")},
			Edges: []facts.Edge{
				{Kind: facts.ContainsFile, SourceKey: mutationFixturePackageKey, TargetKey: extraFile.Key, Confidence: facts.StructuralCertain, Provenance: facts.PackageManifest},
				mutationDefinesEdge(extraFile, extraKey),
				// CONTAINS_FILE and DEFINES above are well formed and sort
				// before CALLS_DIRECT in CanonicalRelationshipTables order,
				// so they succeed before this one fails -- proving rollback
				// undoes real prior progress within the same transaction,
				// not just a no-op.
				{Kind: facts.CallsDirect, SourceKey: extraKey, TargetKey: "symbol:repoM:nowhere.go:Ghost", Confidence: facts.ExactTypechecked, Provenance: facts.GoASTCall},
			},
		},
	}
	applyOptions := CanonicalLoadOptions{SnapshotID: 2, ResolverVersion: "v2"}

	_, err = ApplyCanonicalDelta(ctx, path, delta, applyOptions)
	if !errors.Is(err, ErrInvalidCanonicalDelta) {
		t.Fatalf("ApplyCanonicalDelta() error = %v, want ErrInvalidCanonicalDelta", err)
	}

	after := mustCanonicalCounts(t, ctx, path)
	if !reflect.DeepEqual(before, after) {
		t.Fatalf("counts changed after a rejected delta:\nbefore = %#v\nafter  = %#v", before, after)
	}
	afterGraph, err := ScanCanonical(ctx, path)
	if err != nil {
		t.Fatalf("ScanCanonical() after error = %v", err)
	}
	if !reflect.DeepEqual(beforeGraph, afterGraph) {
		t.Fatalf("graph changed after a rejected delta:\nbefore = %#v\nafter  = %#v", beforeGraph, afterGraph)
	}
}

// (e) Applying the same delta twice leaves the same graph as applying it
// once: the upsert (nodes and, thanks to evidence_key normalization,
// evidence bearing edges too) is idempotent by construction, not merely by
// coincidence of retirement clearing the way first -- this delta never
// touches ReplacedFiles/RemovedFiles, so retirement runs neither time.
func TestApplyCanonicalDeltaTwiceIsIdempotent(t *testing.T) {
	ctx := context.Background()
	helperFile := mutationFixtureFile("helper.go", "helper-h1")
	helperKey := "symbol:repoM:helper.go:Helper"
	base := facts.Set{
		Repositories: []facts.Repository{mutationFixtureRepository()},
		Packages:     []facts.Package{mutationFixturePackage()},
		Files:        []facts.File{helperFile},
		Symbols:      []facts.Symbol{mutationFixtureSymbol(helperKey, helperFile, "Helper")},
		Edges:        append(mutationContainmentEdges(helperFile), mutationDefinesEdge(helperFile, helperKey)),
	}
	loadOptions := CanonicalLoadOptions{SnapshotID: 1, ResolverVersion: "v1"}
	path := loadMutationFixture(t, ctx, base, loadOptions)

	newFile := mutationFixtureFile("new.go", "new-h1")
	newFuncKey := "symbol:repoM:new.go:NewFunc"
	evidenceKey := facts.EvidenceKey(newFile.Key, 5, 15)
	delta := facts.Delta{
		Upsert: facts.Set{
			Repositories: []facts.Repository{mutationFixtureRepository()},
			Packages:     []facts.Package{mutationFixturePackage()},
			Files:        []facts.File{newFile},
			Symbols:      []facts.Symbol{mutationFixtureSymbol(newFuncKey, newFile, "NewFunc")},
			Evidence: []facts.Evidence{
				{Key: evidenceKey, RepositoryKey: mutationFixtureRepositoryKey, FileKey: newFile.Key, Start: facts.Position{Line: 1, Offset: 5}, End: facts.Position{Line: 1, Offset: 14}, Text: "Helper()"},
			},
			Edges: []facts.Edge{
				{Kind: facts.ContainsFile, SourceKey: mutationFixturePackageKey, TargetKey: newFile.Key, Confidence: facts.StructuralCertain, Provenance: facts.PackageManifest},
				mutationDefinesEdge(newFile, newFuncKey),
				// Carries evidence, deliberately: this is the shape that
				// needed the NULL/'' evidence_key normalization fix.
				{Kind: facts.CallsDirect, SourceKey: newFuncKey, TargetKey: helperKey, Confidence: facts.ExactTypechecked, Provenance: facts.GoASTCall, EvidenceKey: evidenceKey},
			},
		},
	}
	applyOptions := CanonicalLoadOptions{SnapshotID: 2, ResolverVersion: "v2"}

	if _, err := ApplyCanonicalDelta(ctx, path, delta, applyOptions); err != nil {
		t.Fatalf("first ApplyCanonicalDelta() error = %v", err)
	}
	firstGraph, err := ScanCanonical(ctx, path)
	if err != nil {
		t.Fatalf("ScanCanonical() after first apply error = %v", err)
	}

	if _, err := ApplyCanonicalDelta(ctx, path, delta, applyOptions); err != nil {
		t.Fatalf("second ApplyCanonicalDelta() error = %v", err)
	}
	secondGraph, err := ScanCanonical(ctx, path)
	if err != nil {
		t.Fatalf("ScanCanonical() after second apply error = %v", err)
	}

	if !reflect.DeepEqual(firstGraph, secondGraph) {
		t.Fatalf("graph after second apply differs from after first apply:\nfirst  = %#v\nsecond = %#v", firstGraph, secondGraph)
	}
	requireCleanIntegrity(t, ctx, path)
}

// (f) The graph that applying a delta produces matches the graph of
// loading the final state from scratch -- the strongest available proof,
// combining an add, a replace and a remove (including the cross-file
// endpoint completion path) in one delta.
func TestApplyCanonicalDeltaMatchesFreshLoadOfFinalState(t *testing.T) {
	ctx := context.Background()
	keepFile := mutationFixtureFile("keep.go", "keep-h1")
	replaceFileV1 := mutationFixtureFile("replace.go", "replace-h1")
	removeFile := mutationFixtureFile("remove.go", "remove-h1")
	keepKey := "symbol:repoM:keep.go:KeepFunc"
	oldReplaceKey := "symbol:repoM:replace.go:OldReplace"
	toRemoveKey := "symbol:repoM:remove.go:ToRemove"

	base := facts.Set{
		Repositories: []facts.Repository{mutationFixtureRepository()},
		Packages:     []facts.Package{mutationFixturePackage()},
		Files:        []facts.File{keepFile, replaceFileV1, removeFile},
		Symbols: []facts.Symbol{
			mutationFixtureSymbol(keepKey, keepFile, "KeepFunc"),
			mutationFixtureSymbol(oldReplaceKey, replaceFileV1, "OldReplace"),
			mutationFixtureSymbol(toRemoveKey, removeFile, "ToRemove"),
		},
		Edges: append(mutationContainmentEdges(keepFile, replaceFileV1, removeFile),
			mutationDefinesEdge(keepFile, keepKey),
			mutationDefinesEdge(replaceFileV1, oldReplaceKey),
			mutationDefinesEdge(removeFile, toRemoveKey),
			// KeepFunc (untouched) references ToRemove (about to
			// disappear): the same hard case as scenario (c), layered
			// into a combined delta.
			facts.Edge{Kind: facts.References, SourceKey: keepKey, TargetKey: toRemoveKey, Confidence: facts.ExactTypechecked, Provenance: facts.GoTypesUse},
		),
	}
	loadOptions := CanonicalLoadOptions{SnapshotID: 1, ResolverVersion: "v1"}
	path := loadMutationFixture(t, ctx, base, loadOptions)

	replaceFileV2 := mutationFixtureFile("replace.go", "replace-h2")
	newReplaceKey := "symbol:repoM:replace.go:NewReplace"
	delta := facts.Delta{
		ReplacedFiles: []string{replaceFileV1.Key},
		RemovedFiles:  []string{removeFile.Key},
		Upsert: facts.Set{
			Repositories: []facts.Repository{mutationFixtureRepository()},
			Packages:     []facts.Package{mutationFixturePackage()},
			Files:        []facts.File{replaceFileV2},
			Symbols:      []facts.Symbol{mutationFixtureSymbol(newReplaceKey, replaceFileV2, "NewReplace")},
			Edges: []facts.Edge{
				{Kind: facts.ContainsFile, SourceKey: mutationFixturePackageKey, TargetKey: replaceFileV2.Key, Confidence: facts.StructuralCertain, Provenance: facts.PackageManifest},
				mutationDefinesEdge(replaceFileV2, newReplaceKey),
				// NewReplace calls KeepFunc: KeepFunc is untouched by this
				// delta and so is not itself part of Upsert -- again
				// exercises canonicalUpsertRows' completion path.
				{Kind: facts.CallsDirect, SourceKey: newReplaceKey, TargetKey: keepKey, Confidence: facts.ExactTypechecked, Provenance: facts.GoASTCall},
			},
		},
	}
	applyOptions := CanonicalLoadOptions{SnapshotID: 2, ResolverVersion: "v2"}
	if _, err := ApplyCanonicalDelta(ctx, path, delta, applyOptions); err != nil {
		t.Fatalf("ApplyCanonicalDelta() error = %v", err)
	}
	requireCleanIntegrity(t, ctx, path)
	appliedGraph, err := ScanCanonical(ctx, path)
	if err != nil {
		t.Fatalf("ScanCanonical(applied) error = %v", err)
	}

	// next: what the repository looks like after the same change, computed
	// independently of ApplyCanonicalDelta/retirement -- keep.go untouched,
	// replace.go now defines NewReplace and calls out to KeepFunc,
	// remove.go and everything it defined or was targeted by are gone.
	next := facts.Set{
		Repositories: []facts.Repository{mutationFixtureRepository()},
		Packages:     []facts.Package{mutationFixturePackage()},
		Files:        []facts.File{keepFile, replaceFileV2},
		Symbols: []facts.Symbol{
			mutationFixtureSymbol(keepKey, keepFile, "KeepFunc"),
			mutationFixtureSymbol(newReplaceKey, replaceFileV2, "NewReplace"),
		},
		Edges: append(mutationContainmentEdges(keepFile, replaceFileV2),
			mutationDefinesEdge(keepFile, keepKey),
			mutationDefinesEdge(replaceFileV2, newReplaceKey),
			facts.Edge{Kind: facts.CallsDirect, SourceKey: newReplaceKey, TargetKey: keepKey, Confidence: facts.ExactTypechecked, Provenance: facts.GoASTCall},
		),
	}
	freshPath := loadMutationFixture(t, ctx, next, applyOptions)
	freshGraph, err := ScanCanonical(ctx, freshPath)
	if err != nil {
		t.Fatalf("ScanCanonical(fresh) error = %v", err)
	}

	if !reflect.DeepEqual(appliedGraph, freshGraph) {
		t.Fatalf("graph from ApplyCanonicalDelta differs from a fresh LoadCanonical of the final state:\napplied = %#v\nfresh   = %#v", appliedGraph, freshGraph)
	}
}

// TestApplyCanonicalDeltaRetiresEdgesWhoseEvidenceIsWithdrawn covers the one
// shape the endpoint sweep cannot reach: PACKAGE_DEPENDS_ON joins two
// Package nodes, which a file grained delta never retires, yet it is
// asserted by the file where the import was observed and carries that
// file's evidence. Deleting only edges that touch a retired node left it
// behind pointing at evidence that no longer existed — a ghost edge, found
// by running a real incremental pass over testdata/go/type-relations.
func TestApplyCanonicalDeltaRetiresEdgesWhoseEvidenceIsWithdrawn(t *testing.T) {
	ctx := context.Background()
	options := CanonicalLoadOptions{SnapshotID: 1, ResolverVersion: "test"}

	consumerFile := mutationFixtureFile("consumer.go", "consumer-h1")
	providerPackage := facts.Package{
		Key:           facts.PackageKey(facts.LanguageGo, mutationFixtureRepositoryName, "provider"),
		RepositoryKey: mutationFixtureRepositoryKey, Language: facts.LanguageGo,
		Name: "provider", RootPath: "/repos/" + mutationFixtureRepositoryName + "/provider",
	}
	// The import observation lives in the consumer file, so retiring that
	// file must retire the package edge it asserted.
	observation := facts.Evidence{
		Key:           facts.EvidenceKey(consumerFile.Key, 10, 20),
		RepositoryKey: mutationFixtureRepositoryKey, FileKey: consumerFile.Key,
		Start: facts.Position{Line: 1, Offset: 10}, End: facts.Position{Line: 1, Offset: 20},
		Text: `import "provider"`,
	}
	dependency := facts.Edge{
		Kind: facts.PackageDependsOn, SourceKey: mutationFixturePackageKey, TargetKey: providerPackage.Key,
		Confidence: facts.ExactTypechecked, Provenance: facts.GoTypesUse, EvidenceKey: observation.Key,
	}

	initial := facts.Set{
		Repositories: []facts.Repository{mutationFixtureRepository()},
		Packages:     []facts.Package{mutationFixturePackage(), providerPackage},
		Files:        []facts.File{consumerFile},
		Evidence:     []facts.Evidence{observation},
		Edges: append(mutationContainmentEdges(consumerFile),
			facts.Edge{Kind: facts.ContainsPackage, SourceKey: mutationFixtureRepositoryKey, TargetKey: providerPackage.Key, Confidence: facts.StructuralCertain, Provenance: facts.PackageManifest},
			dependency,
		),
	}
	path := loadMutationFixture(t, ctx, initial, options)

	if counts := mustCanonicalCounts(t, ctx, path); counts["PACKAGE_DEPENDS_ON"] != 1 {
		t.Fatalf("PACKAGE_DEPENDS_ON before the delta = %d, want 1", counts["PACKAGE_DEPENDS_ON"])
	}

	// The consumer file is restated without the import: no evidence, no
	// package dependency. Both Package nodes survive untouched.
	replacement := facts.Set{
		Repositories: []facts.Repository{mutationFixtureRepository()},
		Packages:     []facts.Package{mutationFixturePackage()},
		Files:        []facts.File{mutationFixtureFile("consumer.go", "consumer-h2")},
		Edges:        mutationContainmentEdges(mutationFixtureFile("consumer.go", "consumer-h2")),
	}
	replacement.Sort()
	delta := facts.Delta{ReplacedFiles: []string{consumerFile.Key}, Upsert: replacement}

	if _, err := ApplyCanonicalDelta(ctx, path, delta, options); err != nil {
		t.Fatalf("ApplyCanonicalDelta() error = %v", err)
	}

	counts := mustCanonicalCounts(t, ctx, path)
	if counts["PACKAGE_DEPENDS_ON"] != 0 {
		t.Errorf("PACKAGE_DEPENDS_ON after the delta = %d, want 0: the edge outlived the evidence that asserted it", counts["PACKAGE_DEPENDS_ON"])
	}
	if counts["Evidence"] != 0 {
		t.Errorf("Evidence after the delta = %d, want 0", counts["Evidence"])
	}
	if counts["Package"] != 2 {
		t.Errorf("Package after the delta = %d, want both packages to survive", counts["Package"])
	}
	requireCleanIntegrity(t, ctx, path)
}

// scanSymbolByKey is a small, single purpose lookup ScanCanonical's own
// bulk read does not offer: used only to prove a specific old symbol is
// truly gone by key, not merely absent from an aggregate count.
func scanSymbolByKey(ctx context.Context, path, key string) (CanonicalSymbol, bool, error) {
	graph, err := ScanCanonical(ctx, path)
	if err != nil {
		return CanonicalSymbol{}, false, err
	}
	for _, symbol := range graph.Symbols {
		if symbol.StableKey == key {
			return symbol, true, nil
		}
	}
	return CanonicalSymbol{}, false, nil
}

// TestApplyCanonicalDeltaKeepsEdgesIntoASurvivingSymbol is the LUQUE-2002
// regression at the level the defect lived on.
//
// A file is replaced and its declaration survives under the same stable key --
// what an ordinary edit is: a body changes, the symbol does not. Another file
// calls it and was not touched, so nothing restates its call. Retirement used to
// delete every edge touching a replaced file's symbols, incoming ones included,
// and the caller's edge went with them: after editing one file, every caller in
// another file stopped pointing at it.
//
// The assertion is the whole graph against a fresh load of the final state, so
// a retirement that keeps too much fails it as loudly as one that keeps too
// little.
func TestApplyCanonicalDeltaKeepsEdgesIntoASurvivingSymbol(t *testing.T) {
	ctx := context.Background()
	editedFile := mutationFixtureFile("edited.go", "edited-h1")
	callerFile := mutationFixtureFile("caller.go", "caller-h1")
	survivorKey := "symbol:repoM:edited.go:Survivor"
	callerKey := "symbol:repoM:caller.go:Caller"
	callEvidence := facts.Evidence{
		Key: facts.EvidenceKey(callerFile.Key, 10, 18), FileKey: callerFile.Key,
		Start: facts.Position{Line: 4, Offset: 10}, End: facts.Position{Line: 4, Offset: 18},
	}

	base := facts.Set{
		Repositories: []facts.Repository{mutationFixtureRepository()},
		Packages:     []facts.Package{mutationFixturePackage()},
		Files:        []facts.File{editedFile, callerFile},
		Symbols: []facts.Symbol{
			mutationFixtureSymbol(survivorKey, editedFile, "Survivor"),
			mutationFixtureSymbol(callerKey, callerFile, "Caller"),
		},
		Evidence: []facts.Evidence{callEvidence},
		Edges: append(mutationContainmentEdges(editedFile, callerFile),
			mutationDefinesEdge(editedFile, survivorKey),
			mutationDefinesEdge(callerFile, callerKey),
			facts.Edge{
				Kind: facts.CallsDirect, SourceKey: callerKey, TargetKey: survivorKey,
				Confidence: facts.ExactTypechecked, Provenance: facts.GoTypesUse, EvidenceKey: callEvidence.Key,
			},
		),
	}
	loadOptions := CanonicalLoadOptions{SnapshotID: 1, ResolverVersion: "v1"}
	path := loadMutationFixture(t, ctx, base, loadOptions)

	// The edit: same file, same symbol key, new content hash. caller.go is not
	// named anywhere in the delta, because its own facts did not change.
	editedFileV2 := mutationFixtureFile("edited.go", "edited-h2")
	delta := facts.Delta{
		ReplacedFiles: []string{editedFile.Key},
		Upsert: facts.Set{
			Repositories: []facts.Repository{mutationFixtureRepository()},
			Packages:     []facts.Package{mutationFixturePackage()},
			Files:        []facts.File{editedFileV2},
			Symbols:      []facts.Symbol{mutationFixtureSymbol(survivorKey, editedFileV2, "Survivor")},
			Edges:        []facts.Edge{mutationDefinesEdge(editedFileV2, survivorKey)},
		},
	}
	applyOptions := CanonicalLoadOptions{SnapshotID: 2, ResolverVersion: "v2"}
	if _, err := ApplyCanonicalDelta(ctx, path, delta, applyOptions); err != nil {
		t.Fatalf("ApplyCanonicalDelta() error = %v", err)
	}
	requireCleanIntegrity(t, ctx, path)

	final := base
	final.Files = []facts.File{editedFileV2, callerFile}
	freshPath := loadMutationFixture(t, ctx, final, applyOptions)

	applied, err := ScanCanonical(ctx, path)
	if err != nil {
		t.Fatalf("ScanCanonical(applied) error = %v", err)
	}
	fresh, err := ScanCanonical(ctx, freshPath)
	if err != nil {
		t.Fatalf("ScanCanonical(fresh) error = %v", err)
	}
	// The graphs agree on content and disagree on one thing: a row nothing
	// restated keeps the stamp of the snapshot that wrote it, while a clean
	// load stamps everything with the new one. That is the honest record --
	// the call really was observed in generation 1 and nothing re-observed it
	// -- and nothing filters on the column: it is provenance, not identity.
	// So an incremental graph is not byte-identical to a rebuild, and the
	// difference is exactly this.
	if diff := canonicalEdgeContent(applied); !reflect.DeepEqual(canonicalEdgeContent(fresh), diff) {
		t.Fatalf("edge content differs from a fresh load:\napplied = %#v\nfresh   = %#v", diff, canonicalEdgeContent(fresh))
	}
	applied.Edges, fresh.Edges = nil, nil
	if !reflect.DeepEqual(fresh, applied) {
		t.Fatalf("the applied graph differs from a fresh load outside its edges:\napplied = %#v\nfresh   = %#v", applied, fresh)
	}

	surviving := canonicalEdge(t, path, "CALLS_DIRECT", callerKey, survivorKey)
	if surviving.SourceSnapshot != 1 || surviving.ResolverVersion != "v1" {
		t.Fatalf("the surviving call was re-stamped: %#v", surviving)
	}
	// A restated row is stamped exactly as a clean load stamps it, whatever
	// that is for its table: the divergence above is confined to the rows
	// nothing restated.
	restated := canonicalEdge(t, path, "DEFINES", editedFileV2.Key, survivorKey)
	if want := canonicalEdge(t, freshPath, "DEFINES", editedFileV2.Key, survivorKey); restated != want {
		t.Fatalf("the restated definition differs from a clean load:\n applied = %#v\n fresh   = %#v", restated, want)
	}
}

// canonicalEdgeContent is every edge without the per-row provenance stamp, so
// two graphs can be compared on what they assert rather than on which pass
// wrote each row.
func canonicalEdgeContent(graph CanonicalGraph) []CanonicalEdge {
	out := make([]CanonicalEdge, 0, len(graph.Edges))
	for _, edge := range graph.Edges {
		edge.SourceSnapshot = 0
		edge.ResolverVersion = ""
		out = append(out, edge)
	}
	return out
}

// canonicalEdge finds one edge by table and endpoints, and fails if the graph
// does not hold exactly one.
func canonicalEdge(t *testing.T, path, table, sourceKey, targetKey string) CanonicalEdge {
	t.Helper()
	graph, err := ScanCanonical(context.Background(), path)
	if err != nil {
		t.Fatalf("ScanCanonical() error = %v", err)
	}
	var found []CanonicalEdge
	for _, edge := range graph.Edges {
		if edge.Table == table && edge.SourceKey == sourceKey && edge.TargetKey == targetKey {
			found = append(found, edge)
		}
	}
	if len(found) != 1 {
		t.Fatalf("%s %s -> %s: %d edges, want 1", table, sourceKey, targetKey, len(found))
	}
	return found[0]
}
