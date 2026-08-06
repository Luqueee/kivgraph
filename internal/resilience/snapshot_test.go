package resilience

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/Luqueee/ladygraph/internal/hotsnapshot"
	"github.com/Luqueee/ladygraph/internal/mcp/tools"
	"github.com/Luqueee/ladygraph/internal/rebuild"
	"github.com/Luqueee/ladygraph/internal/storage/ladybug"
)

// TestCorruptSnapshotDigestDoesNotDisturbReaders is the LUQUE-1204 requirement
// seen from a client: corrupting what is persisted about a generation cannot
// change the answers being served, because what is served is the in-memory
// snapshot and it is derived, never loaded from that file.
func TestCorruptSnapshotDigestDoesNotDisturbReaders(t *testing.T) {
	root := t.TempDir()
	store := hotsnapshot.NewSnapshotStore(publishedSnapshot(t))
	session := connectServer(t, store)
	before := querySymbol(t, session)

	if _, err := rebuild.Run(context.Background(), rebuildOptions(root, "000001")); err != nil {
		t.Fatalf("seed rebuild error = %v", err)
	}
	digestPath := filepath.Join(root, "generations", "000001", "snapshot.sha256")
	if err := os.WriteFile(digestPath, []byte("corrupted\n"), 0o600); err != nil {
		t.Fatalf("corrupt digest: %v", err)
	}

	if after := querySymbol(t, session); after != before {
		t.Fatalf("served graph changed when the digest was corrupted:\n%s\n%s", before, after)
	}
}

// TestServiceRecoversByRebuildingAfterCorruption completes the requirement:
// after the corruption is noticed, the snapshot is rebuilt from the definitive
// graph and published, and readers move to it in one atomic step. There is no
// window where a client sees a partially rebuilt graph.
func TestServiceRecoversByRebuildingAfterCorruption(t *testing.T) {
	root := t.TempDir()
	store := hotsnapshot.NewSnapshotStore(publishedSnapshot(t))
	session := connectServer(t, store)
	if _, _, err := lookupSymbol(session, "sym-root"); err != nil {
		t.Fatalf("fixture symbol does not resolve before the rebuild: %v", err)
	}

	if _, err := rebuild.Run(context.Background(), rebuildOptions(root, "000001")); err != nil {
		t.Fatalf("seed rebuild error = %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, "generations", "000001", "snapshot.sha256"), []byte("corrupted\n"), 0o600); err != nil {
		t.Fatalf("corrupt digest: %v", err)
	}

	rebuilt, report, err := rebuild.SnapshotGeneration(context.Background(), rebuild.GenerationSnapshotOptions{
		Root: root, SnapshotID: 92,
		Scan: func(context.Context, string) (ladybug.CanonicalGraph, error) {
			return rebuildCanonicalGraph(), nil
		},
	})
	if err != nil || !report.Passed {
		t.Fatalf("SnapshotGeneration() error = %v, report = %+v", err, report)
	}
	if err := store.Publish(rebuilt); err != nil {
		t.Fatalf("Publish() error = %v", err)
	}

	// The rebuilt snapshot is the definitive graph's own corpus, so readers
	// move to it wholesale: its symbol answers, and the previous fixture's no
	// longer exists. The swap is one atomic pointer exchange, so no client can
	// observe a state where neither resolves.
	if _, _, err := lookupSymbol(session, "sym-stable-new"); err != nil {
		t.Fatalf("rebuilt snapshot does not answer for its own symbol: %v", err)
	}
	code, _, err := lookupSymbol(session, "sym-root")
	if err == nil || code != tools.CodeSymbolNotFound {
		t.Fatalf("previous symbol still resolves after the rebuild: code = %q, err = %v", code, err)
	}
}

// TestUnbuildableGraphLeavesTheServiceHonest is the cold-start case: nothing is
// published and the graph cannot be converted. The tools must say the index is
// not ready rather than serve a partial graph.
func TestUnbuildableGraphLeavesTheServiceHonest(t *testing.T) {
	root := t.TempDir()
	store := hotsnapshot.NewSnapshotStore(nil)
	session := connectServer(t, store)

	if _, err := rebuild.Run(context.Background(), rebuildOptions(root, "000001")); err != nil {
		t.Fatalf("seed rebuild error = %v", err)
	}
	_, _, err := rebuild.SnapshotGeneration(context.Background(), rebuild.GenerationSnapshotOptions{
		Root: root, SnapshotID: 93,
		Scan: func(context.Context, string) (ladybug.CanonicalGraph, error) {
			graph := rebuildCanonicalGraph()
			graph.Edges = append(graph.Edges, ladybug.CanonicalEdge{
				Table: "CALLS_DIRECT", SourceKey: "sym-missing", TargetKey: "sym-stable-new",
				Confidence: "EXACT_TYPECHECKED", Provenance: "GO_TYPES_USE", EvidenceKey: "evidence-ghost",
			})
			return graph, nil
		},
	})
	if !errors.Is(err, rebuild.ErrSnapshotBuildFailed) {
		t.Fatalf("SnapshotGeneration() error = %v, want ErrSnapshotBuildFailed", err)
	}
	if snapshot := store.Load(); snapshot != nil {
		t.Fatalf("store published %#v, want nothing", snapshot.Metadata())
	}

	code, _, err := lookupSymbol(session, "sym-root")
	if err == nil {
		t.Fatal("get_symbol answered without a published snapshot")
	}
	if code != tools.CodeIndexNotReady {
		t.Fatalf("error code = %q, want %q", code, tools.CodeIndexNotReady)
	}
}
