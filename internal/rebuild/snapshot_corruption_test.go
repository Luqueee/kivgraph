package rebuild

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Luqueee/kivgraph/internal/storage/ladybug"
)

// A generation persists two things: snapshot.sha256, the digest of its canonical
// table counts, and since ADR 0045 the HotSnapshot itself. Neither is required
// to answer a query. The digest is what a published snapshot proves it belongs
// to, and the snapshot is an economy, so "load the last valid snapshot or
// rebuild it" has both branches now and either one has to end in a usable
// graph. These tests fix the second branch: whatever is wrong with what was
// written down, the definitive graph is still there and still enough.

// TestSnapshotGenerationRebuildsDespiteACorruptDigest is the recovery half: a
// corrupted digest must not cost the graph. The digest records what a previous
// build observed; the snapshot is rebuilt from the database, so a garbled
// digest file cannot make a healthy graph unqueryable.
func TestSnapshotGenerationRebuildsDespiteACorruptDigest(t *testing.T) {
	root := t.TempDir()
	seedGeneration(t, root)
	corruptDigest(t, root, "000001", "not-a-digest\n")

	snapshot, report, err := SnapshotGeneration(context.Background(), GenerationSnapshotOptions{
		Root: root, SnapshotID: 11, Scan: fakeScan,
	})
	if err != nil {
		t.Fatalf("SnapshotGeneration() error = %v, want a rebuilt snapshot", err)
	}
	if !report.Passed || snapshot == nil {
		t.Fatalf("report = %+v, snapshot = %v, want a usable rebuild", report, snapshot)
	}
	if metadata := snapshot.Metadata(); metadata.ID != 11 || metadata.Counts.Symbols == 0 {
		t.Fatalf("metadata = %+v, want the graph's own contents", metadata)
	}
}

// TestCorruptDigestBlocksRollbackUntilItIsRestored is the whole loop: while the
// digest is corrupt the generation cannot be reactivated, and once it is
// recomputed from the database the same rollback succeeds.
//
// The refusal matters more than the recovery. A generation whose recorded
// digest disagrees with its content is a generation nobody can vouch for, and
// reactivating it would replace a working graph with an unverified one.
func TestCorruptDigestBlocksRollbackUntilItIsRestored(t *testing.T) {
	root := t.TempDir()
	firstID, secondID := publishTwoGenerations(t, root)
	corruptDigest(t, root, firstID, "0000000000000000000000000000000000000000000000000000000000000000\n")

	_, err := Rollback(context.Background(), rollbackOptions(root, firstID))
	if !errors.Is(err, ErrRollbackFailed) || !strings.Contains(err.Error(), "digest mismatch") {
		t.Fatalf("Rollback() error = %v, want a refused digest mismatch", err)
	}
	if got := currentGenerationID(t, root); got != secondID {
		t.Fatalf("CURRENT = %q, want %q untouched", got, secondID)
	}

	// Recovery is a recomputation, not a repair: the digest is derived again
	// from the counts the database reports right now.
	counts, err := fakeCounts(context.Background(), generationDatabase(root, firstID))
	if err != nil {
		t.Fatalf("read counts: %v", err)
	}
	if _, err := RefreshSnapshotDigest(filepath.Join(root, "generations", firstID), counts); err != nil {
		t.Fatalf("RefreshSnapshotDigest() error = %v", err)
	}

	if _, err := Rollback(context.Background(), rollbackOptions(root, firstID)); err != nil {
		t.Fatalf("Rollback() after restoring the digest error = %v", err)
	}
	if got := currentGenerationID(t, root); got != firstID {
		t.Fatalf("CURRENT = %q, want %q after the recovered rollback", got, firstID)
	}
}

// TestSnapshotGenerationFailsLoudlyOnAnUnconvertibleGraph is the other side: a
// digest that cannot be trusted is recoverable, but a graph that cannot become
// a snapshot is not. It must fail instead of publishing a partial graph.
func TestSnapshotGenerationFailsLoudlyOnAnUnconvertibleGraph(t *testing.T) {
	root := t.TempDir()
	seedGeneration(t, root)

	snapshot, report, err := SnapshotGeneration(context.Background(), GenerationSnapshotOptions{
		Root: root, SnapshotID: 12,
		Scan: func(context.Context, string) (ladybug.CanonicalGraph, error) {
			graph := fakeCanonicalGraph()
			// An edge whose source no other row defines: the builder must
			// refuse it rather than drop it silently.
			graph.Edges = append(graph.Edges, ladybug.CanonicalEdge{
				Table: "CALLS_DIRECT", SourceKey: "sym-does-not-exist", TargetKey: "sym-stable-helper",
				Confidence: "EXACT_TYPECHECKED", Provenance: "GO_TYPES_USE", EvidenceKey: "evidence-ghost",
			})
			return graph, nil
		},
	})
	if err == nil {
		t.Fatalf("SnapshotGeneration() error = nil, want a refused graph (report = %+v)", report)
	}
	if !errors.Is(err, ErrSnapshotBuildFailed) {
		t.Fatalf("errors.Is(err, ErrSnapshotBuildFailed) = false; err = %v", err)
	}
	if snapshot != nil || report.Passed {
		t.Fatalf("snapshot = %v, report = %+v, want nothing usable", snapshot, report)
	}
}

func corruptDigest(t *testing.T, root, generationID, content string) {
	t.Helper()
	path := filepath.Join(root, "generations", generationID, snapshotFileName)
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("generation %s has no digest to corrupt: %v", generationID, err)
	}
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("corrupt digest: %v", err)
	}
}

func generationDatabase(root, generationID string) string {
	return filepath.Join(root, "generations", generationID, "graph.db")
}
