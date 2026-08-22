package rebuild

import (
	"context"
	"encoding/binary"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Luqueee/kivgraph/internal/hotsnapshot"
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

// TestAPublishedSnapshotIsNeverServedWhenItIsWrong covers the five ways a
// published snapshot can be wrong, at the level a generation is actually read.
//
// Each one has to end the same way, and that is the point: the file is refused,
// the reason is recorded where a server can say it out loud, and the graph is
// derived from the database and answered anyway. A file that is wrong must cost
// a rebuild, never an answer.
//
// The unit oracle in internal/hotsnapshot fixes what the parser rejects. This
// one fixes what a caller gets when it does, which is a different contract: a
// refusal that returned no graph would turn a corrupt cache into an outage.
func TestAPublishedSnapshotIsNeverServedWhenItIsWrong(t *testing.T) {
	for name, testCase := range map[string]struct {
		break_  func(t *testing.T, path string)
		expects string
	}{
		// Caught by the section bounds rather than by the digest, which is the
		// better of the two: the table still describes a payload that is no
		// longer there, so the file is refused before a single byte is hashed.
		"truncated": {
			break_: func(t *testing.T, path string) {
				data := readSnapshotFile(t, path)
				writeSnapshotFile(t, path, data[:len(data)/2])
			},
			expects: "payload bytes",
		},
		"a foreign magic": {
			break_: func(t *testing.T, path string) {
				data := readSnapshotFile(t, path)
				copy(data[0:8], []byte("NOTKIVSN"))
				writeSnapshotFile(t, path, data)
			},
			expects: "foreign magic",
		},
		"another format version": {
			break_: func(t *testing.T, path string) {
				data := readSnapshotFile(t, path)
				binary.LittleEndian.PutUint32(data[8:12], hotsnapshot.SnapshotFileFormatVersion+1)
				writeSnapshotFile(t, path, data)
			},
			expects: "format version",
		},
		"another generation": {
			// The header repeats the generation's own content digest, so a
			// snapshot left behind by a different graph is caught even though
			// its own bytes are intact.
			break_: func(t *testing.T, path string) {
				data := readSnapshotFile(t, path)
				for index := 40; index < 72; index++ {
					data[index] ^= 0xff
				}
				writeSnapshotFile(t, path, data)
			},
			expects: "content digest",
		},
		"another payload digest": {
			break_: func(t *testing.T, path string) {
				data := readSnapshotFile(t, path)
				for index := 72; index < 104; index++ {
					data[index] ^= 0xff
				}
				writeSnapshotFile(t, path, data)
			},
			expects: "payload digest",
		},
	} {
		t.Run(name, func(t *testing.T) {
			root := t.TempDir()
			seedGeneration(t, root)
			path := filepath.Join(root, "generations", "000001", PublishedSnapshotFileName)
			if _, err := os.Stat(path); err != nil {
				t.Fatalf("the generation carries no published snapshot to break: %v", err)
			}
			testCase.break_(t, path)

			snapshot, report, err := LoadOrBuildSnapshot(context.Background(), BuildSnapshotOptions{
				DatabasePath: generationDatabase(root, "000001"), SnapshotID: 91, Scan: fakeScan,
			})
			if err != nil {
				t.Fatalf("LoadOrBuildSnapshot() error = %v, want a derived graph", err)
			}
			if snapshot == nil || !report.Passed {
				t.Fatalf("snapshot = %v, report = %+v, want a usable derived graph", snapshot, report)
			}
			if report.Loaded {
				t.Fatal("a wrong file was served instead of refused")
			}
			if !strings.Contains(report.LoadRefused, testCase.expects) {
				t.Fatalf("LoadRefused = %q, want it to name %q", report.LoadRefused, testCase.expects)
			}
			if snapshot.Metadata().Counts.Symbols == 0 {
				t.Fatal("the derived graph carries no symbols, so this proves nothing")
			}
		})
	}
}

func readSnapshotFile(t *testing.T, path string) []byte {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read published snapshot: %v", err)
	}
	return data
}

func writeSnapshotFile(t *testing.T, path string, data []byte) {
	t.Helper()
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatalf("write published snapshot: %v", err)
	}
}
