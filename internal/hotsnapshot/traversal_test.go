package hotsnapshot

import (
	"errors"
	"testing"
	"time"
)

func TestTraverseOutgoingIncomingAndGrouping(t *testing.T) {
	rows := builderRows()
	rows.Edges = append(rows.Edges, EdgeRow{SourceKey: "s-c", TargetKey: "s-a", Kind: 3, Confidence: 7, Provenance: 4, EvidenceKind: "checker", EvidenceSourceFileKey: "file-b", EvidenceTargetFileKey: "file-a"})
	snapshot, err := BuildGraphSnapshot(rows, 1, time.Unix(1, 0).UTC(), 1)
	if err != nil {
		t.Fatal(err)
	}
	result, err := snapshot.Traverse(0, TraversalOptions{Direction: TraversalOutgoing, MaxDepth: 5, MaxNodes: 10})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Visits) != 3 || result.Visits[0].ID != 0 || result.Visits[1].ID != 1 || result.Visits[2].ID != 2 || result.Truncated {
		t.Fatalf("outgoing traversal = %#v", result)
	}
	if len(result.Repositories) != 2 || result.Repositories[0].Repository != 0 || result.Repositories[0].Count != 2 || result.Repositories[1].Repository != 1 || result.Repositories[1].Count != 1 {
		t.Fatalf("repository grouping = %#v", result.Repositories)
	}

	result, err = snapshot.Traverse(0, TraversalOptions{Direction: TraversalIncoming, MaxDepth: 5, MaxNodes: 10})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Visits) != 3 || result.Visits[0].ID != 0 || result.Visits[1].ID != 2 || result.Visits[2].ID != 1 {
		t.Fatalf("incoming traversal = %#v", result)
	}
}

func TestTraverseAppliesDepthKindAndNodeLimits(t *testing.T) {
	rows := builderRows()
	rows.Edges = append(rows.Edges, EdgeRow{SourceKey: "s-c", TargetKey: "s-a", Kind: 3, Confidence: 7, Provenance: 4, EvidenceKind: "checker", EvidenceSourceFileKey: "file-b", EvidenceTargetFileKey: "file-a"})
	snapshot, err := BuildGraphSnapshot(rows, 1, time.Unix(1, 0).UTC(), 1)
	if err != nil {
		t.Fatal(err)
	}
	result, err := snapshot.Traverse(0, TraversalOptions{Direction: TraversalOutgoing, MaxDepth: 1, MaxNodes: 10})
	if err != nil || len(result.Visits) != 2 || result.Visits[1].Depth != 1 {
		t.Fatalf("depth-bounded traversal = %#v, %v", result, err)
	}
	result, err = snapshot.Traverse(0, TraversalOptions{Direction: TraversalOutgoing, MaxDepth: 5, MaxNodes: 2})
	if err != nil || len(result.Visits) != 2 || !result.Truncated {
		t.Fatalf("node-bounded traversal = %#v, %v", result, err)
	}
	result, err = snapshot.Traverse(0, TraversalOptions{Direction: TraversalOutgoing, MaxDepth: 5, MaxNodes: 10, EdgeKinds: []uint8{99}})
	if err != nil || len(result.Visits) != 1 || result.Truncated {
		t.Fatalf("filtered traversal = %#v, %v", result, err)
	}
	result, err = snapshot.Traverse(0, TraversalOptions{Direction: TraversalOutgoing, MaxDepth: 5, MaxNodes: 10, Deadline: time.Unix(1, 0)})
	if !errors.Is(err, ErrTraversalTimeout) || len(result.Visits) != 0 {
		t.Fatalf("expired traversal = %#v, %v", result, err)
	}
}

// TestTraverseRecordsDiscoveringEdgeAndFiltersConfidence covers the two facts
// a dependency query needs from the frontier: how each symbol was reached, and
// that a confidence filter gates reachability instead of only hiding rows.
func TestTraverseRecordsDiscoveringEdgeAndFiltersConfidence(t *testing.T) {
	snapshot, err := BuildGraphSnapshot(builderRows(), 1, time.Unix(1, 0).UTC(), 1)
	if err != nil {
		t.Fatal(err)
	}
	result, err := snapshot.Traverse(0, TraversalOptions{Direction: TraversalOutgoing, MaxDepth: 5, MaxNodes: 10})
	if err != nil {
		t.Fatal(err)
	}
	if len(result.Visits) != 3 {
		t.Fatalf("traversal = %#v, want three visits", result.Visits)
	}
	if root := result.Visits[0]; root.Source != InvalidSymbolID || root.Edge != (PackedEdge{}) {
		t.Fatalf("start visit = %#v, want no discovering edge", root)
	}
	if first := result.Visits[1]; first.Source != 0 || first.Edge.Target != 1 || first.Edge.Kind != 1 || first.Edge.Confidence != 9 {
		t.Fatalf("s-b visit = %#v, want discovery from s-a over kind 1", first)
	}
	if second := result.Visits[2]; second.Source != 1 || second.Edge.Target != 2 || second.Edge.Kind != 2 || second.Edge.Confidence != 8 {
		t.Fatalf("s-c visit = %#v, want discovery from s-b over kind 2", second)
	}

	// Only the s-a -> s-b edge carries confidence 9, so s-c becomes
	// unreachable rather than merely unlisted.
	filtered, err := snapshot.Traverse(0, TraversalOptions{Direction: TraversalOutgoing, MaxDepth: 5, MaxNodes: 10, Confidences: []uint8{9}})
	if err != nil {
		t.Fatal(err)
	}
	if len(filtered.Visits) != 2 || filtered.Visits[1].ID != 1 || filtered.Truncated {
		t.Fatalf("confidence-filtered traversal = %#v", filtered)
	}
}

func TestTraverseRejectsInvalidOptions(t *testing.T) {
	snapshot, err := BuildGraphSnapshot(builderRows(), 1, time.Unix(1, 0).UTC(), 1)
	if err != nil {
		t.Fatal(err)
	}
	for _, options := range []TraversalOptions{
		{Direction: 0, MaxDepth: 1, MaxNodes: 10},
		{Direction: TraversalOutgoing, MaxDepth: -1, MaxNodes: 10},
		{Direction: TraversalOutgoing, MaxDepth: MaxTraversalDepth + 1, MaxNodes: 10},
		{Direction: TraversalOutgoing, MaxDepth: 1, MaxNodes: 0},
		{Direction: TraversalOutgoing, MaxDepth: 1, MaxNodes: MaxTraversalNodes + 1},
	} {
		if _, err := snapshot.Traverse(0, options); !errors.Is(err, ErrInvalidTraversal) {
			t.Fatalf("Traverse(%#v) error = %v, want ErrInvalidTraversal", options, err)
		}
	}
	if _, err := snapshot.Traverse(InvalidSymbolID, TraversalOptions{Direction: TraversalOutgoing, MaxDepth: 1, MaxNodes: 10}); !errors.Is(err, ErrInvalidTraversal) {
		t.Fatalf("Traverse(invalid ID) error = %v", err)
	}
}
