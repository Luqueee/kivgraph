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
