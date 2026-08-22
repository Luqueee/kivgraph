//go:build ladybug && cgo

package rebuild

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/Luqueee/kivgraph/internal/hotsnapshot"
)

// TestPublishedSnapshotMatchesADerivedOne is the oracle on real data. The unit
// oracle proves a round trip preserves a fixture; this one proves that what a
// real generation carries is what its canonical graph derives, over a corpus
// nobody wrote by hand.
//
// It compares through the public surface, symbol by symbol and edge by edge,
// because that is what a query reaches: two snapshots agreeing on their counts
// while disagreeing on one symbol's file would pass anything coarser.
//
//	KIVGRAPH_SNAPSHOT_BUILD_DB=~/.local/state/kivgraph/generations/000090/graph.db \
//	  make test-ladybug PKGS=./internal/rebuild ARGS='-run TestPublishedSnapshotMatchesADerivedOne -v'
func TestPublishedSnapshotMatchesADerivedOne(t *testing.T) {
	databasePath := os.Getenv("KIVGRAPH_SNAPSHOT_BUILD_DB")
	if databasePath == "" {
		t.Skip("set KIVGRAPH_SNAPSHOT_BUILD_DB to a published generation's graph.db")
	}
	directory := filepath.Dir(databasePath)
	if _, err := os.Stat(filepath.Join(directory, PublishedSnapshotFileName)); err != nil {
		t.Skipf("generation carries no %s: %v", PublishedSnapshotFileName, err)
	}

	derived, report, err := BuildSnapshot(context.Background(), BuildSnapshotOptions{
		DatabasePath: databasePath, SnapshotID: 1,
	})
	if err != nil || !report.Passed {
		t.Fatalf("derive: err = %v, report = %+v", err, report)
	}
	loaded, err := loadPublishedSnapshot(directory)
	if err != nil {
		t.Fatalf("load published snapshot: %v", err)
	}

	if loaded.Metadata().Counts != derived.Metadata().Counts {
		t.Fatalf("counts\n loaded %+v\nderived %+v", loaded.Metadata().Counts, derived.Metadata().Counts)
	}
	counts := derived.Metadata().Counts
	if counts.Symbols == 0 || counts.Edges == 0 {
		t.Fatal("the graph carries no symbols or no edges, so this comparison proves nothing")
	}

	for id := range hotsnapshot.SymbolID(counts.Symbols) {
		want, okWant := derived.Symbol(id)
		got, okGot := loaded.Symbol(id)
		if okWant != okGot || want != got {
			t.Fatalf("symbol %d\n loaded %+v (%v)\nderived %+v (%v)", id, got, okGot, want, okWant)
		}
		// The stable key has to resolve to the same dense id in both, or a
		// query would answer with another symbol's identity. It is resolved to
		// its characters first and looked up as a string on the other side:
		// comparing the two dense ids would compare an id against itself and
		// stop proving that the published key still names this symbol.
		key, okKey := derived.StableKey(want.StableKey)
		if !okKey {
			t.Fatalf("symbol %d has no stable key in the derived snapshot", id)
		}
		if resolved, found := loaded.SymbolByStableKey(key); !found || resolved != id {
			t.Fatalf("stable key %q resolves to %d in the loaded snapshot, want %d", key, resolved, id)
		}
		if diff := compareEdges(derived.Outgoing(id), loaded.Outgoing(id)); diff != "" {
			t.Fatalf("outgoing edges of symbol %d: %s", id, diff)
		}
		if diff := compareEdges(derived.Incoming(id), loaded.Incoming(id)); diff != "" {
			t.Fatalf("incoming edges of symbol %d: %s", id, diff)
		}
	}
	for id := range hotsnapshot.FileID(counts.Files) {
		want, okWant := derived.File(id)
		got, okGot := loaded.File(id)
		if okWant != okGot || want != got {
			t.Fatalf("file %d\n loaded %+v (%v)\nderived %+v (%v)", id, got, okGot, want, okWant)
		}
	}
	for id := range hotsnapshot.RepositoryID(counts.Repositories) {
		want, _ := derived.Repository(id)
		got, _ := loaded.Repository(id)
		if want != got {
			t.Fatalf("repository %d\n loaded %+v\nderived %+v", id, got, want)
		}
	}
	for id := range hotsnapshot.EvidenceID(counts.Evidence) {
		want, _ := derived.Evidence(id)
		got, _ := loaded.Evidence(id)
		if want != got {
			t.Fatalf("evidence %d\n loaded %+v\nderived %+v", id, got, want)
		}
	}
	// Interned ids are internal to a snapshot, so the two tables are only
	// comparable through what they resolve to: every symbol's name and
	// qualified name has to name the same string in both.
	for id := range hotsnapshot.SymbolID(counts.Symbols) {
		record, _ := derived.Symbol(id)
		for _, interned := range []hotsnapshot.InternedString{record.Name, record.QualifiedName, record.Kind, record.Language, record.Signature} {
			want, okWant := derived.Strings().String(interned)
			got, okGot := loaded.Strings().String(interned)
			if okWant != okGot || want != got {
				t.Fatalf("interned string %d of symbol %d: loaded %q (%v), derived %q (%v)", interned, id, got, okGot, want, okWant)
			}
		}
	}
	t.Logf("compared %d symbols, %d files, %d edges", counts.Symbols, counts.Files, counts.Edges)
}

func compareEdges(want, got []hotsnapshot.PackedEdge) string {
	if len(want) != len(got) {
		return "different lengths"
	}
	for index := range want {
		if want[index] != got[index] {
			return fmt.Sprintf("edge %d differs: %+v against %+v", index, want[index], got[index])
		}
	}
	return ""
}
