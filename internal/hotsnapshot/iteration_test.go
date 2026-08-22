package hotsnapshot

import (
	"context"
	"errors"
	"reflect"
	"testing"
)

func TestVisitSymbolsUsesDenseRangeWithoutEscapingRecords(t *testing.T) {
	snapshot, err := NewGraphSnapshot(graphSnapshotTestInput())
	if err != nil {
		t.Fatalf("NewGraphSnapshot() error = %v", err)
	}

	var ids []SymbolID
	var records []SymbolRecord
	err = snapshot.VisitSymbols(context.Background(), 0, 2, func(id SymbolID, record SymbolRecord) error {
		ids = append(ids, id)
		records = append(records, record)
		record.StableKey = InvalidStableKeyID
		return nil
	})
	if err != nil {
		t.Fatalf("VisitSymbols() error = %v", err)
	}
	if !reflect.DeepEqual(ids, []SymbolID{0, 1}) {
		t.Fatalf("visited IDs = %v", ids)
	}
	if len(records) != 2 {
		t.Fatalf("visited records = %#v", records)
	}
	for index, want := range []StableKey{"symbol-a", "symbol-b"} {
		if key, ok := snapshot.StableKey(records[index].StableKey); !ok || key != want {
			t.Fatalf("visited record %d stable key = %q (%t), want %q", index, key, ok, want)
		}
	}
	if symbol, ok := snapshot.Symbol(0); !ok || symbol.StableKey != 0 {
		t.Fatalf("snapshot record changed through visitor: %#v, %t", symbol, ok)
	}

	called := false
	if err := snapshot.VisitSymbols(context.Background(), 1, 1, func(SymbolID, SymbolRecord) error {
		called = true
		return nil
	}); err != nil {
		t.Fatalf("empty VisitSymbols() error = %v", err)
	}
	if called {
		t.Fatal("empty symbol range invoked visitor")
	}
}
func TestVisitDenseTables(t *testing.T) {
	snapshot, err := NewGraphSnapshot(graphSnapshotTestInput())
	if err != nil {
		t.Fatalf("NewGraphSnapshot() error = %v", err)
	}

	if err := snapshot.VisitRepositories(context.Background(), 0, 1, func(id RepositoryID, record RepositoryRecord) error {
		if id != 0 || record.Name != 0 {
			t.Fatalf("repository = %d, %#v", id, record)
		}
		return nil
	}); err != nil {
		t.Fatalf("VisitRepositories() error = %v", err)
	}
	if err := snapshot.VisitPackages(context.Background(), 0, 1, func(id PackageID, record PackageRecord) error {
		if id != 0 || record.Repository != 0 {
			t.Fatalf("package = %d, %#v", id, record)
		}
		return nil
	}); err != nil {
		t.Fatalf("VisitPackages() error = %v", err)
	}
	if err := snapshot.VisitFiles(context.Background(), 0, 1, func(id FileID, record FileRecord) error {
		if id != 0 || record.Package != 0 {
			t.Fatalf("file = %d, %#v", id, record)
		}
		return nil
	}); err != nil {
		t.Fatalf("VisitFiles() error = %v", err)
	}
	if err := snapshot.VisitEvidence(context.Background(), 0, 1, func(id EvidenceID, record EvidenceRecord) error {
		if id != 0 || record.SourceFile != 0 || record.TargetFile != 0 {
			t.Fatalf("evidence = %d, %#v", id, record)
		}
		return nil
	}); err != nil {
		t.Fatalf("VisitEvidence() error = %v", err)
	}
}

func TestVisitSymbolsRejectsInvalidRangesAndPropagatesErrors(t *testing.T) {
	snapshot, err := NewGraphSnapshot(graphSnapshotTestInput())
	if err != nil {
		t.Fatalf("NewGraphSnapshot() error = %v", err)
	}
	visitor := SymbolVisitor(func(SymbolID, SymbolRecord) error { return nil })
	for name, bounds := range map[string]struct{ start, end SymbolID }{
		"start after end":        {start: 1, end: 0},
		"end outside snapshot":   {start: 0, end: 3},
		"invalid start sentinel": {start: InvalidSymbolID, end: InvalidSymbolID},
	} {
		t.Run(name, func(t *testing.T) {
			if err := snapshot.VisitSymbols(context.Background(), bounds.start, bounds.end, visitor); !errors.Is(err, ErrInvalidSnapshotRange) {
				t.Fatalf("VisitSymbols(%d,%d) error = %v, want ErrInvalidSnapshotRange", bounds.start, bounds.end, err)
			}
		})
	}
	if err := snapshot.VisitSymbols(context.Background(), 0, 1, nil); !errors.Is(err, ErrNilSnapshotVisitor) {
		t.Fatalf("nil visitor error = %v, want ErrNilSnapshotVisitor", err)
	}

	want := errors.New("stop symbol iteration")
	if err := snapshot.VisitSymbols(context.Background(), 0, 2, func(SymbolID, SymbolRecord) error { return want }); !errors.Is(err, want) {
		t.Fatalf("visitor error = %v, want %v", err, want)
	}
}

func TestVisitSymbolsHonorsCancellation(t *testing.T) {
	snapshot, err := NewGraphSnapshot(graphSnapshotTestInput())
	if err != nil {
		t.Fatalf("NewGraphSnapshot() error = %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	calls := 0
	err = snapshot.VisitSymbols(ctx, 0, 2, func(SymbolID, SymbolRecord) error {
		calls++
		cancel()
		return nil
	})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("VisitSymbols() error = %v, want context.Canceled", err)
	}
	if calls != 1 {
		t.Fatalf("visitor calls = %d, want 1 after cancellation", calls)
	}
}

func TestCSRRangeAndVisitEdgesPreserveDirectionAndOwnership(t *testing.T) {
	snapshot, err := NewGraphSnapshot(graphSnapshotTestInput())
	if err != nil {
		t.Fatalf("NewGraphSnapshot() error = %v", err)
	}

	start, end, ok := snapshot.CSRRange(TraversalOutgoing, 0)
	if !ok || start != 0 || end != 1 {
		t.Fatalf("outgoing CSR range = %d,%d,%t", start, end, ok)
	}
	var visited []EdgeID
	err = snapshot.VisitEdges(context.Background(), TraversalOutgoing, start, end, func(id EdgeID, edge PackedEdge) error {
		visited = append(visited, id)
		edge.Target = 0
		return nil
	})
	if err != nil {
		t.Fatalf("VisitEdges() error = %v", err)
	}
	if !reflect.DeepEqual(visited, []EdgeID{0}) {
		t.Fatalf("visited edge IDs = %v", visited)
	}
	if outgoing := snapshot.Outgoing(0); len(outgoing) != 1 || outgoing[0].Target != 1 {
		t.Fatalf("edge changed through visitor: %v", outgoing)
	}

	start, end, ok = snapshot.CSRRange(TraversalIncoming, 1)
	if !ok || start != 0 || end != 1 {
		t.Fatalf("incoming CSR range = %d,%d,%t", start, end, ok)
	}
	if err := snapshot.VisitEdges(context.Background(), TraversalIncoming, start, end, func(_ EdgeID, edge PackedEdge) error {
		if edge.Target != 0 {
			t.Fatalf("incoming edge target = %d, want 0", edge.Target)
		}
		return nil
	}); err != nil {
		t.Fatalf("incoming VisitEdges() error = %v", err)
	}

	if _, _, ok := snapshot.CSRRange(TraversalOutgoing, InvalidSymbolID); ok {
		t.Fatal("CSRRange accepted invalid symbol ID")
	}
	if _, _, ok := snapshot.CSRRange(TraversalDirection(99), 0); ok {
		t.Fatal("CSRRange accepted invalid direction")
	}
}

func TestVisitEdgesRejectsInvalidRangesAndHonorsCancellation(t *testing.T) {
	snapshot, err := NewGraphSnapshot(graphSnapshotTestInput())
	if err != nil {
		t.Fatalf("NewGraphSnapshot() error = %v", err)
	}
	visitor := EdgeVisitor(func(EdgeID, PackedEdge) error { return nil })
	cases := []struct {
		name      string
		direction TraversalDirection
		start     EdgeID
		end       EdgeID
		want      error
	}{
		{name: "invalid direction", direction: TraversalDirection(99), want: ErrInvalidCSRDirection},
		{name: "start after end", direction: TraversalOutgoing, start: 1, end: 0, want: ErrInvalidSnapshotRange},
		{name: "end outside snapshot", direction: TraversalOutgoing, start: 0, end: 2, want: ErrInvalidSnapshotRange},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			if err := snapshot.VisitEdges(context.Background(), test.direction, test.start, test.end, visitor); !errors.Is(err, test.want) {
				t.Fatalf("VisitEdges() error = %v, want %v", err, test.want)
			}
		})
	}
	if err := snapshot.VisitEdges(context.Background(), TraversalOutgoing, 0, 1, nil); !errors.Is(err, ErrNilSnapshotVisitor) {
		t.Fatalf("nil edge visitor error = %v, want ErrNilSnapshotVisitor", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	calls := 0
	err = snapshot.VisitEdges(ctx, TraversalOutgoing, 0, 1, func(EdgeID, PackedEdge) error {
		calls++
		return nil
	})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled VisitEdges() error = %v, want context.Canceled", err)
	}
	if calls != 0 {
		t.Fatalf("canceled edge visitor calls = %d, want 0", calls)
	}
}
