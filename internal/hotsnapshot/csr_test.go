package hotsnapshot

import "testing"

func TestBuildForwardCSR(t *testing.T) {
	offsets, packed, err := BuildForwardCSR(4, []SourcedEdge{
		{Source: 2, Edge: PackedEdge{Target: 1, Kind: 2}},
		{Source: 0, Edge: PackedEdge{Target: 3, Kind: 1}},
		{Source: 2, Edge: PackedEdge{Target: 0, Kind: 3}},
	})
	if err != nil {
		t.Fatalf("BuildForwardCSR() error = %v", err)
	}
	wantOffsets := []uint32{0, 1, 1, 3, 3}
	if !equalUint32s(offsets, wantOffsets) {
		t.Fatalf("offsets = %v, want %v", offsets, wantOffsets)
	}
	wantTargets := []SymbolID{3, 1, 0}
	for index, want := range wantTargets {
		if packed[index].Target != want {
			t.Fatalf("packed[%d].Target = %d, want %d", index, packed[index].Target, want)
		}
	}
}

func TestBuildForwardCSRHandlesNoEdgesAndLastSymbol(t *testing.T) {
	offsets, packed, err := BuildForwardCSR(3, nil)
	if err != nil {
		t.Fatalf("BuildForwardCSR() error = %v", err)
	}
	if !equalUint32s(offsets, []uint32{0, 0, 0, 0}) || len(packed) != 0 {
		t.Fatalf("empty CSR = %v, %v", offsets, packed)
	}

	const edgeCount = 4_096
	edges := make([]SourcedEdge, edgeCount)
	for index := range edges {
		edges[index] = SourcedEdge{Source: 2, Edge: PackedEdge{Target: SymbolID(index % 3)}}
	}
	offsets, packed, err = BuildForwardCSR(3, edges)
	if err != nil {
		t.Fatalf("BuildForwardCSR() error = %v", err)
	}
	if !equalUint32s(offsets, []uint32{0, 0, 0, edgeCount}) {
		t.Fatalf("offsets = %v", offsets)
	}
	if len(packed) != edgeCount || packed[edgeCount-1].Target != SymbolID((edgeCount-1)%3) {
		t.Fatalf("last-symbol range lost edges: len=%d last=%#v", len(packed), packed[edgeCount-1])
	}
}

func TestBuildForwardCSRRejectsInvalidSymbolIDs(t *testing.T) {
	for _, edge := range []SourcedEdge{
		{Source: 2, Edge: PackedEdge{Target: 0}},
		{Source: 0, Edge: PackedEdge{Target: 2}},
	} {
		if _, _, err := BuildForwardCSR(2, []SourcedEdge{edge}); err != ErrInvalidCSR {
			t.Fatalf("BuildForwardCSR(%#v) error = %v, want ErrInvalidCSR", edge, err)
		}
	}
}

func TestBuildReverseCSR(t *testing.T) {
	forwardOffsets, forwardEdges, err := BuildForwardCSR(4, []SourcedEdge{
		{Source: 2, Edge: PackedEdge{Target: 1, Evidence: 7, Kind: 2}},
		{Source: 0, Edge: PackedEdge{Target: 3, Evidence: 8, Kind: 1}},
		{Source: 2, Edge: PackedEdge{Target: 0, Evidence: 9, Kind: 3}},
	})
	if err != nil {
		t.Fatal(err)
	}
	reverseOffsets, reverseEdges, err := BuildReverseCSR(4, forwardOffsets, forwardEdges)
	if err != nil {
		t.Fatalf("BuildReverseCSR() error = %v", err)
	}
	if !equalUint32s(reverseOffsets, []uint32{0, 1, 2, 2, 3}) {
		t.Fatalf("reverse offsets = %v", reverseOffsets)
	}
	want := []struct {
		target   SymbolID
		evidence EvidenceID
	}{
		{target: 2, evidence: 9},
		{target: 2, evidence: 7},
		{target: 0, evidence: 8},
	}
	for index, edge := range reverseEdges {
		if edge.Target != want[index].target || edge.Evidence != want[index].evidence {
			t.Fatalf("reverse[%d] = %#v, want target=%d evidence=%d", index, edge, want[index].target, want[index].evidence)
		}
	}
}

func TestBuildReverseCSRRejectsMalformedForward(t *testing.T) {
	if _, _, err := BuildReverseCSR(2, []uint32{0, 2}, []PackedEdge{{Target: 1}}); err != ErrInvalidCSR {
		t.Fatalf("BuildReverseCSR() error = %v, want ErrInvalidCSR", err)
	}
}

func TestGraphSnapshotOutgoingUsesForwardCSR(t *testing.T) {
	input := graphSnapshotTestInput()
	offsets, edges, err := BuildForwardCSR(2, []SourcedEdge{{Source: 1, Edge: PackedEdge{Target: 0, Evidence: 0}}})
	if err != nil {
		t.Fatal(err)
	}
	input.ForwardOffsets = offsets
	input.ForwardEdges = edges
	input.ReverseOffsets = []uint32{0, 1, 1}
	input.ReverseEdges = []PackedEdge{{Target: 1, Evidence: 0}}
	snapshot, err := NewGraphSnapshot(input)
	if err != nil {
		t.Fatal(err)
	}
	if got := snapshot.Outgoing(0); len(got) != 0 {
		t.Fatalf("Outgoing(0) = %v", got)
	}
	outgoing := snapshot.Outgoing(1)
	if len(outgoing) != 1 || outgoing[0].Target != 0 {
		t.Fatalf("Outgoing(1) = %v", outgoing)
	}
	incoming := snapshot.Incoming(0)
	if len(incoming) != 1 || incoming[0].Target != 1 {
		t.Fatalf("Incoming(0) = %v", incoming)
	}
	outgoing[0].Target = 1
	incoming[0].Target = 0
	if got := snapshot.Outgoing(1); got[0].Target != 0 {
		t.Fatalf("Outgoing() exposed mutable storage: %v", got)
	}
	if got := snapshot.Incoming(0); got[0].Target != 1 {
		t.Fatalf("Incoming() exposed mutable storage: %v", got)
	}
	if got := snapshot.Outgoing(2); got != nil {
		t.Fatalf("Outgoing(2) = %v, want nil", got)
	}
	if got := snapshot.Incoming(2); got != nil {
		t.Fatalf("Incoming(2) = %v, want nil", got)
	}
}

func equalUint32s(left, right []uint32) bool {
	if len(left) != len(right) {
		return false
	}
	for index := range left {
		if left[index] != right[index] {
			return false
		}
	}
	return true
}
