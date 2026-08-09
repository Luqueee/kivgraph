//go:build ladybug && cgo

package indexer

import (
	"context"
	"errors"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/Luqueee/ladygraph/internal/facts"
	"github.com/Luqueee/ladygraph/internal/hotsnapshot"
	"github.com/Luqueee/ladygraph/internal/rebuild"
	"github.com/Luqueee/ladygraph/internal/storage/generation"
	"github.com/Luqueee/ladygraph/internal/storage/ladybug"
	"github.com/Luqueee/ladygraph/internal/testsupport"
)

// TestUpdateDeltaRouteRollsBackOnRealStorage is the LUQUE-1203 contract against
// the engine rather than a fake: a delta that fails part way through leaves the
// canonical graph exactly as it was.
//
// Reaching the transaction at all takes care. A delta whose fact sets are
// invalid never gets there -- facts.Diff rejects it first -- so the failure is
// injected the way it actually happens in operation: state drift. The active
// database is loaded from a graph that never had b.go, while the indexer diffs
// two states that both contain it. Both sets validate, the delta is well
// formed, and the engine is the first component in a position to notice that
// the edge's target symbol is not in the database it is mutating.
//
// The delta also retires and restates a.go, so real work lands before the
// failing statement. Without one transaction, that work would survive.
func TestUpdateDeltaRouteRollsBackOnRealStorage(t *testing.T) {
	ctx := context.Background()
	previous := updateFixture(t)
	next := touchFileACallingB(t, previous)
	options := ladybug.CanonicalLoadOptions{SnapshotID: 1, ResolverVersion: "native-test"}

	activePath := testsupport.TempDir(t)
	activeDatabase := filepath.Join(activePath, "graph.db")
	if _, err := ladybug.LoadCanonical(ctx, activeDatabase, withoutFileB(t, previous), options); err != nil {
		t.Fatalf("load drifted state: %v", err)
	}
	before, err := ladybug.ScanCanonical(ctx, activeDatabase)
	if err != nil {
		t.Fatalf("scan before: %v", err)
	}

	layout := rebuild.Layout{
		Active: generation.Generation{ID: "000001", Path: activePath, DatabasePath: activeDatabase},
		NextID: "000002",
	}
	hotStore := hotsnapshot.NewSnapshotStore(nil)
	result, err := Update(ctx, UpdateOptions{
		Root:            testsupport.TempDir(t),
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
	if !errors.Is(err, ErrUpdateFailed) {
		t.Fatalf("Update() error = %v, want ErrUpdateFailed", err)
	}
	// The failure must come from the engine applying the delta. A rejection
	// anywhere earlier would leave nothing to roll back, and this test would
	// prove nothing.
	if !strings.Contains(err.Error(), "apply delta to") {
		t.Fatalf("Update() error = %v, want a failure inside the delta application", err)
	}
	if result.Passed {
		t.Fatalf("result = %#v, want a failed update", result)
	}

	after, err := ladybug.ScanCanonical(ctx, activeDatabase)
	if err != nil {
		t.Fatalf("scan after: %v", err)
	}
	if !reflect.DeepEqual(before, after) {
		t.Fatalf("failed delta mutated the canonical graph:\n before = %#v\n after = %#v", before, after)
	}

	invariants, err := ladybug.VerifyCanonicalIntegrity(ctx, activeDatabase)
	if err != nil {
		t.Fatalf("verify invariants: %v", err)
	}
	if !invariants.Passed {
		t.Fatalf("invariants = %#v, want a healthy graph after a rolled back delta", invariants)
	}
	if snapshot := hotStore.Load(); snapshot != nil {
		t.Fatalf("published HotSnapshot = %#v, want nothing published for a failed delta", snapshot.Metadata())
	}
}

// TestUpdateDeltaRouteSucceedsAfterARollback is the control: the rollback left
// the database usable, not merely unchanged. A delta that does resolve against
// the same file must apply cleanly right after the failed one.
func TestUpdateDeltaRouteSucceedsAfterARollback(t *testing.T) {
	ctx := context.Background()
	full := updateFixture(t)
	drifted := withoutFileB(t, full)
	options := ladybug.CanonicalLoadOptions{SnapshotID: 1, ResolverVersion: "native-test"}

	activePath := testsupport.TempDir(t)
	activeDatabase := filepath.Join(activePath, "graph.db")
	if _, err := ladybug.LoadCanonical(ctx, activeDatabase, drifted, options); err != nil {
		t.Fatalf("load drifted state: %v", err)
	}

	layout := rebuild.Layout{
		Active: generation.Generation{ID: "000001", Path: activePath, DatabasePath: activeDatabase},
		NextID: "000002",
	}
	layoutFunc := func(context.Context, rebuild.LayoutOptions) (rebuild.Layout, error) { return layout, nil }
	plans := []InvalidationPlan{{Class: ChangeBodyOnly, Actions: []InvalidationAction{ActionReindexFile}}}

	if _, err := Update(ctx, UpdateOptions{
		Root: testsupport.TempDir(t), Plans: plans, Previous: full, Next: touchFileACallingB(t, full),
		SnapshotID: options.SnapshotID, ResolverVersion: options.ResolverVersion, Layout: layoutFunc,
	}); !errors.Is(err, ErrUpdateFailed) {
		t.Fatalf("drifted Update() error = %v, want ErrUpdateFailed", err)
	}

	// The drifted state holds a single file, so restating it is 100% of the
	// corpus and the default ratio would route this to a republish. The
	// control is about the database being usable after a rollback, not about
	// routing, so the ratio is widened to keep the delta route.
	good := touchFileA(t, drifted)
	result, err := Update(ctx, UpdateOptions{
		Root: testsupport.TempDir(t), Plans: plans, Previous: drifted, Next: good, RepublishRatio: 1,
		SnapshotID: options.SnapshotID, ResolverVersion: options.ResolverVersion, Layout: layoutFunc,
	})
	if err != nil {
		t.Fatalf("Update() after rollback error = %v", err)
	}
	if !result.Passed || result.Decision.Route != RouteDelta {
		t.Fatalf("result = %#v, want a passing DELTA route", result)
	}

	reference := filepath.Join(testsupport.TempDir(t), "graph.db")
	if _, err := ladybug.LoadCanonical(ctx, reference, good, options); err != nil {
		t.Fatalf("load reference: %v", err)
	}
	mutated, err := ladybug.ScanCanonical(ctx, activeDatabase)
	if err != nil {
		t.Fatalf("scan mutated: %v", err)
	}
	expected, err := ladybug.ScanCanonical(ctx, reference)
	if err != nil {
		t.Fatalf("scan reference: %v", err)
	}
	if !reflect.DeepEqual(mutated, expected) {
		t.Fatalf("graph after rollback+delta differs from a clean load:\n mutated = %#v\nexpected = %#v", mutated, expected)
	}
}

// touchFileACallingB restates a.go with an edge into b.go's symbol. Against the
// full fixture the set is valid; against a database that never had b.go the
// engine cannot resolve the target.
func touchFileACallingB(t *testing.T, set facts.Set) facts.Set {
	t.Helper()
	changed := touchFileA(t, set)
	repositoryKey := facts.RepositoryKey("acme/widgets")
	fileA := facts.FileKey(repositoryKey, "a.go")
	var symbolA, symbolB string
	for _, symbol := range changed.Symbols {
		if symbol.FileKey == fileA {
			symbolA = symbol.Key
			continue
		}
		symbolB = symbol.Key
	}
	if symbolA == "" || symbolB == "" {
		t.Fatalf("fixture does not have both symbols: %#v", changed.Symbols)
	}
	changed.Edges = append(changed.Edges, facts.Edge{
		Kind: facts.CallsDirect, SourceKey: symbolA, TargetKey: symbolB,
		Confidence: facts.ExactTypechecked, Provenance: facts.GoASTCall,
	})
	changed.Sort()
	if err := changed.Validate(); err != nil {
		t.Fatalf("fixture with a cross-file call is invalid: %v", err)
	}
	return changed
}
