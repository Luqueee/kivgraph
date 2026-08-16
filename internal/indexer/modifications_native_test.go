//go:build ladybug && cgo

package indexer

import (
	"context"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/Luqueee/kivgraph/internal/facts"
	"github.com/Luqueee/kivgraph/internal/hotsnapshot"
	"github.com/Luqueee/kivgraph/internal/rebuild"
	"github.com/Luqueee/kivgraph/internal/storage/generation"
	"github.com/Luqueee/kivgraph/internal/storage/ladybug"
	"github.com/Luqueee/kivgraph/internal/testsupport"
)

// TestUpdateModificationsMatchAFullLoad covers the five LUQUE-1010 change
// boundaries with the real delta transaction. The definitive graph after a
// modification must equal a clean load of the next state; comparing only
// mutation counters would miss stale symbols, evidence, or relations.
func TestUpdateModificationsMatchAFullLoad(t *testing.T) {
	cases := []struct {
		name    string
		plan    InvalidationPlan
		prepare func(*testing.T) (facts.Set, facts.Set)
	}{
		{name: "body", plan: InvalidationPlan{Class: ChangeBodyOnly, Actions: []InvalidationAction{ActionReindexFile}}, prepare: prepareBody},
		{name: "signature", plan: InvalidationPlan{Class: ChangeSignatureChanged, Actions: []InvalidationAction{ActionReindexProvider, ActionInvalidateConsumers}}, prepare: prepareSignature},
		{name: "callback", plan: InvalidationPlan{Class: ChangeBodyOnly, Actions: []InvalidationAction{ActionReindexFile}}, prepare: prepareCallback},
		{name: "import", plan: InvalidationPlan{Class: ChangeImportsChanged, Actions: []InvalidationAction{ActionReindexFile, ActionResolveReferences}}, prepare: prepareImport},
		{name: "provider", plan: InvalidationPlan{Class: ChangeSignatureChanged, Actions: []InvalidationAction{ActionReindexProvider, ActionInvalidateConsumers}}, prepare: prepareProvider},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			previous, next := testCase.prepare(t)
			runNativeModification(t, previous, next, testCase.plan)
		})
	}
}

func runNativeModification(t *testing.T, previous, next facts.Set, plan InvalidationPlan) {
	t.Helper()
	ctx := context.Background()
	loadOptions := ladybug.CanonicalLoadOptions{SnapshotID: 21, ResolverVersion: "modification-native"}
	activePath := testsupport.TempDir(t)
	activeDatabase := filepath.Join(activePath, "graph.db")
	if _, err := ladybug.LoadCanonical(ctx, activeDatabase, previous, loadOptions); err != nil {
		t.Fatalf("load previous state: %v", err)
	}
	layout := rebuild.Layout{
		Active: generation.Generation{ID: "000021", Path: activePath, DatabasePath: activeDatabase},
		NextID: "000022",
	}
	hotStore := hotsnapshot.NewSnapshotStore(nil)
	result, err := Update(ctx, UpdateOptions{
		Root: testsupport.TempDir(t), Plans: []InvalidationPlan{plan}, Previous: previous, Next: next,
		SnapshotID: loadOptions.SnapshotID, ResolverVersion: loadOptions.ResolverVersion, SnapshotStore: hotStore,
		Layout: func(context.Context, rebuild.LayoutOptions) (rebuild.Layout, error) { return layout, nil },
		Republish: func(context.Context, rebuild.Options) (rebuild.Report, error) {
			t.Fatal("a single-file modification must not republish")
			return rebuild.Report{}, nil
		},
	})
	if err != nil {
		t.Fatalf("Update() error = %v", err)
	}
	if result.Decision.Route != RouteDelta || !result.Passed {
		t.Fatalf("result = %#v, want a passing DELTA route", result)
	}
	if snapshot := hotStore.Load(); snapshot == nil || snapshot.Metadata().ID != uint64(loadOptions.SnapshotID) {
		t.Fatalf("published HotSnapshot = %#v, want generation %d", snapshot, loadOptions.SnapshotID)
	}

	referenceDatabase := filepath.Join(testsupport.TempDir(t), "graph.db")
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
		t.Fatalf("post-modification graph differs from full load:\nmutated = %#v\nreference = %#v", mutated, reference)
	}
	integrity, err := ladybug.VerifyCanonicalIntegrity(ctx, activeDatabase)
	if err != nil {
		t.Fatalf("verify canonical integrity: %v", err)
	}
	if !integrity.Passed {
		t.Fatalf("integrity = %#v, want all invariants passing", integrity)
	}
}

func prepareBody(t *testing.T) (facts.Set, facts.Set) {
	t.Helper()
	previous := updateFixture(t)
	return previous, touchFileA(t, previous)
}

func prepareSignature(t *testing.T) (facts.Set, facts.Set) {
	t.Helper()
	previous := updateFixture(t)
	next := cloneUpdateSet(previous)
	for index := range next.Symbols {
		if next.Symbols[index].Key == "symbol:go:acme/widgets.A" {
			next.Symbols[index].Signature = "func A(value int) string"
			next.Symbols[index].End = facts.Position{Line: 5, Offset: 60}
		}
	}
	next.Sort()
	if err := next.Validate(); err != nil {
		t.Fatalf("signature fixture is invalid: %v", err)
	}
	return previous, next
}

func prepareCallback(t *testing.T) (facts.Set, facts.Set) {
	t.Helper()
	previous := withCallback(updateFixture(t))
	next := cloneUpdateSet(previous)
	for index := range next.Evidence {
		if next.Evidence[index].Key == facts.EvidenceKey(facts.FileKey(facts.RepositoryKey("acme/widgets"), "a.go"), 42, 48) {
			next.Evidence[index].Text = "B(value)"
			next.Evidence[index].End = facts.Position{Line: 43, Offset: 55}
		}
	}
	next.Sort()
	if err := next.Validate(); err != nil {
		t.Fatalf("callback fixture is invalid: %v", err)
	}
	return previous, next
}

func withCallback(previous facts.Set) facts.Set {
	next := cloneUpdateSet(previous)
	repositoryKey := facts.RepositoryKey("acme/widgets")
	fileKey := facts.FileKey(repositoryKey, "a.go")
	evidenceKey := facts.EvidenceKey(fileKey, 42, 48)
	next.Evidence = append(next.Evidence, facts.Evidence{
		Key: evidenceKey, RepositoryKey: repositoryKey, FileKey: fileKey,
		Start: facts.Position{Line: 42, Offset: 42}, End: facts.Position{Line: 42, Offset: 48}, Text: "B()",
	})
	next.Edges = append(next.Edges, facts.Edge{
		Kind: facts.PassesAsCallback, SourceKey: "symbol:go:acme/widgets.A", TargetKey: "symbol:go:acme/widgets.B",
		Confidence: facts.ExactTypechecked, Provenance: facts.GoASTCallback, EvidenceKey: evidenceKey,
	})
	next.Sort()
	return next
}

func prepareImport(t *testing.T) (facts.Set, facts.Set) {
	t.Helper()
	previous := withUnresolvedImport(updateFixture(t))
	next := resolveImport(t, previous)
	return previous, next
}

func withUnresolvedImport(previous facts.Set) facts.Set {
	next := cloneUpdateSet(previous)
	repositoryKey := facts.RepositoryKey("acme/widgets")
	fileKey := facts.FileKey(repositoryKey, "a.go")
	next.Unresolved = append(next.Unresolved, facts.UnresolvedReference{
		RepositoryKey: repositoryKey, FileKey: fileKey, Language: facts.LanguageGo,
		SourceSymbolKey: "symbol:go:acme/widgets.A", RequestedPackage: "acme/widgets",
		RequestedSymbol: "B", Reason: "symbol_not_found", Start: facts.Position{Line: 52, Offset: 52},
	})
	next.Sort()
	return next
}

func resolveImport(t *testing.T, previous facts.Set) facts.Set {
	t.Helper()
	next := cloneUpdateSet(previous)
	next.Unresolved = nil
	repositoryKey := facts.RepositoryKey("acme/widgets")
	fileKey := facts.FileKey(repositoryKey, "a.go")
	evidenceKey := facts.EvidenceKey(fileKey, 52, 62)
	next.Evidence = append(next.Evidence, facts.Evidence{
		Key: evidenceKey, RepositoryKey: repositoryKey, FileKey: fileKey,
		Start: facts.Position{Line: 52, Offset: 52}, End: facts.Position{Line: 52, Offset: 62}, Text: "import B",
	})
	next.Edges = append(next.Edges, facts.Edge{
		Kind: facts.ImportsSymbol, SourceKey: "symbol:go:acme/widgets.A", TargetKey: "symbol:go:acme/widgets.B",
		Confidence: facts.ExactTypechecked, Provenance: facts.GoTypesUse, EvidenceKey: evidenceKey,
	})
	next.Sort()
	if err := next.Validate(); err != nil {
		t.Fatalf("import fixture is invalid: %v", err)
	}
	return next
}

func prepareProvider(t *testing.T) (facts.Set, facts.Set) {
	t.Helper()
	previous := updateFixture(t)
	next := cloneUpdateSet(previous)
	for index := range next.Symbols {
		if next.Symbols[index].Key == "symbol:go:acme/widgets.B" {
			next.Symbols[index].Signature = "func B(value int) string"
			next.Symbols[index].End = facts.Position{Line: 5, Offset: 60}
		}
	}
	next.Sort()
	if err := next.Validate(); err != nil {
		t.Fatalf("provider fixture is invalid: %v", err)
	}
	return previous, next
}
