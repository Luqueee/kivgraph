//go:build ladybug && cgo

package indexer

import (
	"context"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/Luqueee/ladygraph/internal/facts"
	"github.com/Luqueee/ladygraph/internal/hotsnapshot"
	"github.com/Luqueee/ladygraph/internal/rebuild"
	"github.com/Luqueee/ladygraph/internal/storage/generation"
	"github.com/Luqueee/ladygraph/internal/storage/ladybug"
)

// TestUpdateDeletionsLeaveNoGhosts exercises symbol and file withdrawal on a
// real canonical database. Each removed reference is represented by an
// unresolved fact in the next state, and the published HotSnapshot is checked
// after the transaction as well as the persistent graph.
func TestUpdateDeletionsLeaveNoGhosts(t *testing.T) {
	cases := []struct {
		name       string
		removeFile bool
	}{
		{name: "symbol", removeFile: false},
		{name: "file", removeFile: true},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			previous := withDirectCall(updateFixture(t))
			next := removeCallToUnresolved(t, previous, testCase.removeFile)
			runNativeDeletion(t, previous, next, testCase.removeFile)
		})
	}
}

func runNativeDeletion(t *testing.T, previous, next facts.Set, removedFile bool) {
	t.Helper()
	ctx := context.Background()
	loadOptions := ladybug.CanonicalLoadOptions{SnapshotID: 31, ResolverVersion: "deletion-native"}
	activePath := t.TempDir()
	activeDatabase := filepath.Join(activePath, "graph.db")
	if _, err := ladybug.LoadCanonical(ctx, activeDatabase, previous, loadOptions); err != nil {
		t.Fatalf("load previous state: %v", err)
	}
	layout := rebuild.Layout{
		Active: generation.Generation{ID: "000031", Path: activePath, DatabasePath: activeDatabase},
		NextID: "000032",
	}
	hotStore := hotsnapshot.NewSnapshotStore(nil)
	result, err := Update(ctx, UpdateOptions{
		Root: t.TempDir(),
		Plans: []InvalidationPlan{{Class: ChangeFileDeleted, Actions: []InvalidationAction{
			ActionRemoveFile, ActionInvalidateConsumers, ActionResolveReferences,
		}}},
		Previous: previous, Next: next, RepublishRatio: 2,
		SnapshotID: loadOptions.SnapshotID, ResolverVersion: loadOptions.ResolverVersion, SnapshotStore: hotStore,
		Layout: func(context.Context, rebuild.LayoutOptions) (rebuild.Layout, error) { return layout, nil },
		Republish: func(context.Context, rebuild.Options) (rebuild.Report, error) {
			t.Fatal("a file-scoped deletion must not republish")
			return rebuild.Report{}, nil
		},
	})
	if err != nil {
		t.Fatalf("Update() error = %v", err)
	}
	if result.Decision.Route != RouteDelta || !result.Passed {
		t.Fatalf("result = %#v, want a passing DELTA route", result)
	}
	if result.Mutation.RemovedEdges == 0 || result.Mutation.RemovedSymbols == 0 {
		t.Fatalf("mutation = %#v, want withdrawn edges and symbols", result.Mutation)
	}
	if snapshot := hotStore.Load(); snapshot == nil || snapshot.Metadata().ID != uint64(loadOptions.SnapshotID) {
		t.Fatalf("published HotSnapshot = %#v, want generation %d", snapshot, loadOptions.SnapshotID)
	}
	if _, found := hotStore.Load().SymbolByStableKey("symbol:go:acme/widgets.B"); found {
		t.Fatal("deleted provider symbol remains in the published HotSnapshot")
	}
	if _, found := hotStore.Load().SymbolByStableKey("symbol:go:acme/widgets.A"); !found {
		t.Fatal("surviving source symbol disappeared from the published HotSnapshot")
	}

	referenceDatabase := filepath.Join(t.TempDir(), "graph.db")
	if _, err := ladybug.LoadCanonical(ctx, referenceDatabase, next, loadOptions); err != nil {
		t.Fatalf("load next state from scratch: %v", err)
	}
	mutated, err := ladybug.ScanCanonical(ctx, activeDatabase)
	if err != nil {
		t.Fatalf("scan mutated graph: %v", err)
	}
	reference, err := ladybug.ScanCanonical(ctx, referenceDatabase)
	if err != nil {
		t.Fatalf("scan reference graph: %v", err)
	}
	if !reflect.DeepEqual(mutated, reference) {
		t.Fatalf("post-deletion graph differs from full load:\nmutated = %#v\nreference = %#v", mutated, reference)
	}

	mutatedCounts, err := ladybug.CanonicalTableCounts(ctx, activeDatabase)
	if err != nil {
		t.Fatalf("count mutated graph: %v", err)
	}
	referenceCounts, err := ladybug.CanonicalTableCounts(ctx, referenceDatabase)
	if err != nil {
		t.Fatalf("count reference graph: %v", err)
	}
	if mutatedCounts["UnresolvedReference"] != 1 || mutatedCounts["UnresolvedReference"] != referenceCounts["UnresolvedReference"] {
		t.Fatalf("unresolved counts = mutated %d/reference %d, want one in both", mutatedCounts["UnresolvedReference"], referenceCounts["UnresolvedReference"])
	}
	integrity, err := ladybug.VerifyCanonicalIntegrity(ctx, activeDatabase)
	if err != nil {
		t.Fatalf("verify canonical integrity: %v", err)
	}
	if !integrity.Passed {
		t.Fatalf("integrity = %#v, want all invariants passing", integrity)
	}
}

func withDirectCall(previous facts.Set) facts.Set {
	next := cloneUpdateSet(previous)
	repositoryKey := facts.RepositoryKey("acme/widgets")
	fileKey := facts.FileKey(repositoryKey, "a.go")
	evidenceKey := facts.EvidenceKey(fileKey, 70, 78)
	next.Evidence = append(next.Evidence, facts.Evidence{
		Key: evidenceKey, RepositoryKey: repositoryKey, FileKey: fileKey,
		Start: facts.Position{Line: 70, Offset: 70}, End: facts.Position{Line: 70, Offset: 78}, Text: "B()",
	})
	next.Edges = append(next.Edges, facts.Edge{
		Kind: facts.CallsDirect, SourceKey: "symbol:go:acme/widgets.A", TargetKey: "symbol:go:acme/widgets.B",
		Confidence: facts.ExactTypechecked, Provenance: facts.GoASTCall, EvidenceKey: evidenceKey,
	})
	next.Sort()
	return next
}

func removeCallToUnresolved(t *testing.T, previous facts.Set, removeFile bool) facts.Set {
	t.Helper()
	next := cloneUpdateSet(previous)
	repositoryKey := facts.RepositoryKey("acme/widgets")
	fileAKey := facts.FileKey(repositoryKey, "a.go")
	fileBKey := facts.FileKey(repositoryKey, "b.go")
	symbolBKey := "symbol:go:acme/widgets.B"
	callEvidenceKey := facts.EvidenceKey(fileAKey, 70, 78)

	if removeFile {
		next.Files = filterDeletion(next.Files, func(file facts.File) bool { return file.Key == fileBKey })
	}
	next.Symbols = filterDeletion(next.Symbols, func(symbol facts.Symbol) bool { return symbol.Key == symbolBKey })
	next.Evidence = filterDeletion(next.Evidence, func(evidence facts.Evidence) bool { return evidence.Key == callEvidenceKey })
	next.Edges = filterDeletion(next.Edges, func(edge facts.Edge) bool {
		return edge.TargetKey == symbolBKey ||
			edge.Kind == facts.CallsDirect && edge.SourceKey == "symbol:go:acme/widgets.A" && edge.TargetKey == symbolBKey ||
			removeFile && (edge.SourceKey == fileBKey || edge.TargetKey == fileBKey)
	})
	next.Unresolved = append(next.Unresolved, facts.UnresolvedReference{
		RepositoryKey: repositoryKey, FileKey: fileAKey, Language: facts.LanguageGo,
		SourceSymbolKey: "symbol:go:acme/widgets.A", RequestedPackage: "acme/widgets",
		RequestedSymbol: "B", Reason: "symbol_not_found", Start: facts.Position{Line: 70, Offset: 70},
	})
	next.Sort()
	if err := next.Validate(); err != nil {
		t.Fatalf("deletion fixture is invalid: %v", err)
	}
	return next
}

func filterDeletion[T any](items []T, remove func(T) bool) []T {
	filtered := make([]T, 0, len(items))
	for _, item := range items {
		if !remove(item) {
			filtered = append(filtered, item)
		}
	}
	return filtered
}
