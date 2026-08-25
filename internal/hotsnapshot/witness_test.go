package hotsnapshot

import (
	"errors"
	"testing"
	"time"
)

// TestWitnessPathReportsTheRouteItWalked pins what a witness claims: the
// ordered edges from the seed to the target, seed first, each hop carrying the
// symbol it came from and the edge it crossed. The fixture chain is
// s-a -> s-b -> s-c, so the route is the whole chain.
func TestWitnessPathReportsTheRouteItWalked(t *testing.T) {
	snapshot, err := BuildGraphSnapshot(builderRows(), 1, time.Unix(1, 0).UTC(), 1)
	if err != nil {
		t.Fatal(err)
	}

	path, truncated, err := snapshot.WitnessPath([]SymbolID{0}, 2, TraversalOptions{
		Direction: TraversalOutgoing, MaxDepth: 5, MaxNodes: 10,
	})
	if err != nil || truncated {
		t.Fatalf("WitnessPath() = %v, truncated %t", err, truncated)
	}
	want := []TraversalVisit{
		{ID: 0, Depth: 0, Source: InvalidSymbolID},
		{ID: 1, Depth: 1, Source: 0, Edge: path[1].Edge},
		{ID: 2, Depth: 2, Source: 1, Edge: path[2].Edge},
	}
	if len(path) != 3 || path[0] != want[0] || path[1] != want[1] || path[2] != want[2] {
		t.Fatalf("route = %#v, want the seed and both hops in order", path)
	}
	// The edges are the fixture's own, not a re-derivation: a route made of
	// edges the caller cannot check is worth no more than a guess.
	if path[1].Edge.Kind != 1 || path[1].Edge.Confidence != 9 || path[2].Edge.Kind != 2 || path[2].Edge.Confidence != 8 {
		t.Fatalf("route edges = %#v, %#v, want the fixture's kinds and confidences", path[1].Edge, path[2].Edge)
	}
}

// TestWitnessPathPrefersTheShorterRoute is the whole point of a shortest path:
// with both a direct edge and a two-hop chain to the same target, the direct
// one is the answer.
func TestWitnessPathPrefersTheShorterRoute(t *testing.T) {
	rows := builderRows()
	rows.Edges = append(rows.Edges, EdgeRow{
		SourceKey: "s-a", TargetKey: "s-c", Kind: 3, Confidence: 7, Provenance: 4,
		EvidenceKind: "checker", EvidenceSourceFileKey: "file-a", EvidenceTargetFileKey: "file-b",
	})
	snapshot, err := BuildGraphSnapshot(rows, 1, time.Unix(1, 0).UTC(), 1)
	if err != nil {
		t.Fatal(err)
	}

	path, _, err := snapshot.WitnessPath([]SymbolID{0}, 2, TraversalOptions{
		Direction: TraversalOutgoing, MaxDepth: 5, MaxNodes: 10,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(path) != 2 || path[1].ID != 2 || path[1].Source != 0 || path[1].Depth != 1 {
		t.Fatalf("route = %#v, want the direct edge rather than the chain", path)
	}
}

// TestWitnessPathSeparatesAbsenceFromItsBounds is the honesty contract. Three
// different situations return no route, and only one of them is an absence:
// the depth bound, the node budget, and a gate that makes the target
// unreachable. The node budget must say so, because a caller that reads a
// budget as an absence states something the search never established.
func TestWitnessPathSeparatesAbsenceFromItsBounds(t *testing.T) {
	snapshot, err := BuildGraphSnapshot(builderRows(), 1, time.Unix(1, 0).UTC(), 1)
	if err != nil {
		t.Fatal(err)
	}

	path, truncated, err := snapshot.WitnessPath([]SymbolID{0}, 2, TraversalOptions{
		Direction: TraversalOutgoing, MaxDepth: 1, MaxNodes: 10,
	})
	if err != nil || path != nil || truncated {
		t.Fatalf("depth-bounded = %#v, truncated %t, err %v; want no route and no truncation", path, truncated, err)
	}

	path, truncated, err = snapshot.WitnessPath([]SymbolID{0}, 2, TraversalOptions{
		Direction: TraversalOutgoing, MaxDepth: 5, MaxNodes: 1,
	})
	if err != nil || path != nil || !truncated {
		t.Fatalf("budget-bounded = %#v, truncated %t, err %v; want no route and truncation reported", path, truncated, err)
	}

	// The first hop is kind 1, so allowing only kind 2 makes the target
	// unreachable rather than merely unreported.
	path, truncated, err = snapshot.WitnessPath([]SymbolID{0}, 2, TraversalOptions{
		Direction: TraversalOutgoing, MaxDepth: 5, MaxNodes: 10, EdgeKinds: []uint8{2},
	})
	if err != nil || path != nil || truncated {
		t.Fatalf("kind-gated = %#v, truncated %t, err %v; want no route", path, truncated, err)
	}
}

// TestWitnessPathAnswersForASeedAndRejectsInvalidBounds covers the two edges of
// the contract: a seed is its own witness at no hops, and the bounds are
// validated the same way the frontier validates them.
func TestWitnessPathAnswersForASeedAndRejectsInvalidBounds(t *testing.T) {
	snapshot, err := BuildGraphSnapshot(builderRows(), 1, time.Unix(1, 0).UTC(), 1)
	if err != nil {
		t.Fatal(err)
	}

	path, truncated, err := snapshot.WitnessPath([]SymbolID{0, 1}, 1, TraversalOptions{
		Direction: TraversalOutgoing, MaxDepth: 3, MaxNodes: 10,
	})
	if err != nil || truncated || len(path) != 1 || path[0].ID != 1 || path[0].Source != InvalidSymbolID {
		t.Fatalf("seed target = %#v, truncated %t, err %v; want the seed alone", path, truncated, err)
	}

	options := TraversalOptions{Direction: TraversalOutgoing, MaxDepth: 3, MaxNodes: 10}
	if _, _, err := snapshot.WitnessPath([]SymbolID{0}, SymbolID(len(snapshot.symbols)), options); !errors.Is(err, ErrInvalidTraversal) {
		t.Fatalf("target past the table = %v, want ErrInvalidTraversal", err)
	}
	if _, _, err := snapshot.WitnessPath(nil, 2, options); !errors.Is(err, ErrInvalidTraversal) {
		t.Fatalf("no seed = %v, want ErrInvalidTraversal", err)
	}
	if _, _, err := snapshot.WitnessPath([]SymbolID{0}, 2, TraversalOptions{
		Direction: TraversalOutgoing, MaxDepth: MaxTraversalDepth + 1, MaxNodes: 10,
	}); !errors.Is(err, ErrInvalidTraversal) {
		t.Fatalf("depth past the ceiling = %v, want ErrInvalidTraversal", err)
	}
	if _, _, err := snapshot.WitnessPath([]SymbolID{0}, 2, TraversalOptions{
		MaxDepth: 3, MaxNodes: 10,
	}); !errors.Is(err, ErrInvalidTraversal) {
		t.Fatalf("no direction = %v, want ErrInvalidTraversal", err)
	}

	// A deadline already past is a timeout, not an empty answer: an empty
	// answer would read as an absence.
	if _, _, err := snapshot.WitnessPath([]SymbolID{0}, 2, TraversalOptions{
		Direction: TraversalOutgoing, MaxDepth: 3, MaxNodes: 10, Deadline: time.Unix(1, 0),
	}); !errors.Is(err, ErrTraversalTimeout) {
		t.Fatalf("expired deadline = %v, want ErrTraversalTimeout", err)
	}
}

// TestWitnessPathWalksIncomingEdgesToo keeps the direction a gate rather than a
// second algorithm: the route from s-c back to s-a exists only on the reverse
// CSR.
func TestWitnessPathWalksIncomingEdgesToo(t *testing.T) {
	snapshot, err := BuildGraphSnapshot(builderRows(), 1, time.Unix(1, 0).UTC(), 1)
	if err != nil {
		t.Fatal(err)
	}

	path, _, err := snapshot.WitnessPath([]SymbolID{2}, 0, TraversalOptions{
		Direction: TraversalIncoming, MaxDepth: 5, MaxNodes: 10,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(path) != 3 || path[0].ID != 2 || path[1].ID != 1 || path[2].ID != 0 {
		t.Fatalf("incoming route = %#v, want s-c, s-b, s-a", path)
	}
}
