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

// TestUpdateDeltaRouteMatchesAFullLoadOfTheNextState drives the delta route
func TestUpdateDeltaRouteMatchesAFullLoadOfTheNextState(t *testing.T) {
	ctx := context.Background()
	previous := updateFixture(t)
	next := touchFileA(t, previous)
	options := ladybug.CanonicalLoadOptions{SnapshotID: 1, ResolverVersion: "native-test"}

	activePath := t.TempDir()
	activeDatabase := filepath.Join(activePath, "graph.db")
	if _, err := ladybug.LoadCanonical(ctx, activeDatabase, previous, options); err != nil {
		t.Fatalf("load previous state: %v", err)
	}

	layout := rebuild.Layout{
		Active: generation.Generation{ID: "000001", Path: activePath, DatabasePath: activeDatabase},
		NextID: "000002",
	}
	hotStore := hotsnapshot.NewSnapshotStore(nil)
	result, err := Update(ctx, UpdateOptions{
		Root:            t.TempDir(),
		Plans:           []InvalidationPlan{{Class: ChangeBodyOnly, Actions: []InvalidationAction{ActionReindexFile}}},
		Previous:        previous,
		Next:            next,
		SnapshotID:      options.SnapshotID,
		ResolverVersion: options.ResolverVersion,
		SnapshotStore:   hotStore,
		Layout: func(context.Context, rebuild.LayoutOptions) (rebuild.Layout, error) {
			return layout, nil
		},
		Republish: func(context.Context, rebuild.Options) (rebuild.Report, error) {
			t.Fatal("this fixture must take the delta route")
			return rebuild.Report{}, nil
		},
	})
	if err != nil {
		t.Fatalf("Update() error = %v", err)
	}
	if snapshot := hotStore.Load(); snapshot == nil || snapshot.Metadata().ID != 1 {
		t.Fatalf("published HotSnapshot = %#v, want native snapshot generation 1", snapshot)
	}
	if result.Decision.Route != RouteDelta || !result.Passed {
		t.Fatalf("result = %#v, want a passing DELTA route", result)
	}

	referenceDatabase := filepath.Join(t.TempDir(), "graph.db")
	if _, err := ladybug.LoadCanonical(ctx, referenceDatabase, next, options); err != nil {
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
		t.Fatalf("mutated graph differs from a full load of the next state:\n mutated = %#v\nreference = %#v", mutated, reference)
	}

	invariants, err := ladybug.VerifyCanonicalIntegrity(ctx, activeDatabase)
	if err != nil {
		t.Fatalf("verify invariants: %v", err)
	}
	if !invariants.Passed {
		t.Fatalf("invariants = %#v, want all passing after a delta", invariants)
	}

	// The refreshed digest must equal what the mutated graph's own counts
	// produce, which is exactly what a later rollback recomputes.
	counts, err := ladybug.CanonicalTableCounts(ctx, activeDatabase)
	if err != nil {
		t.Fatalf("read mutated counts: %v", err)
	}
	expected, err := rebuild.RefreshSnapshotDigest(t.TempDir(), counts)
	if err != nil {
		t.Fatalf("recompute digest: %v", err)
	}
	if result.SnapshotDigest != expected {
		t.Fatalf("digest = %q, want %q", result.SnapshotDigest, expected)
	}
}

// TestUpdateDeltaRouteRemovesAFileWithoutGhosts proves the removal half of
// the gate on real storage: the symbols and edges a deleted file asserted
// are gone, and the graph again equals a clean load of the next state.
func TestUpdateDeltaRouteRemovesAFileWithoutGhosts(t *testing.T) {
	ctx := context.Background()
	previous := updateFixture(t)
	next := withoutFileB(t, previous)
	options := ladybug.CanonicalLoadOptions{SnapshotID: 2, ResolverVersion: "native-test"}

	activePath := t.TempDir()
	activeDatabase := filepath.Join(activePath, "graph.db")
	if _, err := ladybug.LoadCanonical(ctx, activeDatabase, previous, options); err != nil {
		t.Fatalf("load previous state: %v", err)
	}

	result, err := Update(ctx, UpdateOptions{
		Root:            t.TempDir(),
		Plans:           []InvalidationPlan{{Class: ChangeFileDeleted, Actions: []InvalidationAction{ActionRemoveFile, ActionInvalidateConsumers}}},
		Previous:        previous,
		Next:            next,
		SnapshotID:      options.SnapshotID,
		ResolverVersion: options.ResolverVersion,
		Layout: func(context.Context, rebuild.LayoutOptions) (rebuild.Layout, error) {
			return rebuild.Layout{
				Active: generation.Generation{ID: "000001", Path: activePath, DatabasePath: activeDatabase},
				NextID: "000002",
			}, nil
		},
	})
	if err != nil {
		t.Fatalf("Update() error = %v", err)
	}
	if result.Decision.Route != RouteDelta {
		t.Fatalf("decision = %#v, want DELTA", result.Decision)
	}
	if result.Mutation.RemovedFiles != 1 || result.Mutation.RemovedSymbols != 1 {
		t.Fatalf("mutation = %#v, want one file and one symbol withdrawn", result.Mutation)
	}

	referenceDatabase := filepath.Join(t.TempDir(), "graph.db")
	if _, err := ladybug.LoadCanonical(ctx, referenceDatabase, next, options); err != nil {
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
		t.Fatalf("removal left the graph different from a clean load:\n mutated = %#v\nreference = %#v", mutated, reference)
	}
}

// withoutFileB drops b.go and everything it anchored.
func withoutFileB(t *testing.T, set facts.Set) facts.Set {
	t.Helper()
	fileB := facts.FileKey(facts.RepositoryKey("acme/widgets"), "b.go")
	reduced := facts.Set{
		Repositories: append([]facts.Repository(nil), set.Repositories...),
		Packages:     append([]facts.Package(nil), set.Packages...),
	}
	for _, file := range set.Files {
		if file.Key != fileB {
			reduced.Files = append(reduced.Files, file)
		}
	}
	for _, symbol := range set.Symbols {
		if symbol.FileKey != fileB {
			reduced.Symbols = append(reduced.Symbols, symbol)
		}
	}
	for _, evidence := range set.Evidence {
		if evidence.FileKey != fileB {
			reduced.Evidence = append(reduced.Evidence, evidence)
		}
	}
	for _, edge := range set.Edges {
		if edge.SourceKey == fileB || edge.TargetKey == fileB {
			continue
		}
		if edge.Kind == facts.Defines && edge.SourceKey == fileB {
			continue
		}
		reduced.Edges = append(reduced.Edges, edge)
	}
	reduced.Sort()
	if err := reduced.Validate(); err != nil {
		t.Fatalf("reduced fixture is invalid: %v", err)
	}
	return reduced
}
