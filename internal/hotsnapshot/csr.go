package hotsnapshot

import (
	"errors"
	"math"
)

var ErrInvalidCSR = errors.New("invalid compressed sparse row adjacency")

// SourcedEdge is one outgoing edge before it is packed into forward CSR.
type SourcedEdge struct {
	Source SymbolID
	Edge   PackedEdge
}

// BuildForwardCSR groups outgoing edges contiguously by source SymbolID. Edge
// order within one source remains the input order, making builds deterministic
// whenever the source scan is deterministic.
func BuildForwardCSR(symbolCount uint32, edges []SourcedEdge) ([]uint32, []PackedEdge, error) {
	if uint64(len(edges)) > math.MaxUint32 {
		return nil, nil, ErrIDOverflow
	}
	offsets := make([]uint32, uint64(symbolCount)+1)
	for _, sourced := range edges {
		if sourced.Source >= SymbolID(symbolCount) || sourced.Edge.Target >= SymbolID(symbolCount) {
			return nil, nil, ErrInvalidCSR
		}
		offsets[uint64(sourced.Source)+1]++
	}
	for index := 1; index < len(offsets); index++ {
		offsets[index] += offsets[index-1]
	}

	packed := make([]PackedEdge, len(edges))
	cursor := append([]uint32(nil), offsets[:len(offsets)-1]...)
	for _, sourced := range edges {
		index := cursor[sourced.Source]
		packed[index] = sourced.Edge
		cursor[sourced.Source]++
	}
	return offsets, packed, nil
}

// BuildReverseCSR creates incoming CSR from a validated forward CSR. Each
// reverse edge points to the original source and preserves all edge metadata.
func BuildReverseCSR(symbolCount uint32, forwardOffsets []uint32, forwardEdges []PackedEdge) ([]uint32, []PackedEdge, error) {
	if !validCSR(int(symbolCount), forwardOffsets, forwardEdges) {
		return nil, nil, ErrInvalidCSR
	}
	offsets := make([]uint32, uint64(symbolCount)+1)
	for _, edge := range forwardEdges {
		offsets[uint64(edge.Target)+1]++
	}
	for index := 1; index < len(offsets); index++ {
		offsets[index] += offsets[index-1]
	}

	reverse := make([]PackedEdge, len(forwardEdges))
	cursor := append([]uint32(nil), offsets[:len(offsets)-1]...)
	for source := 0; source < int(symbolCount); source++ {
		for _, edge := range forwardEdges[forwardOffsets[source]:forwardOffsets[source+1]] {
			target := edge.Target
			edge.Target = SymbolID(source)
			index := cursor[target]
			reverse[index] = edge
			cursor[target]++
		}
	}
	return offsets, reverse, nil
}

func validCSR(symbols int, offsets []uint32, edges []PackedEdge) bool {
	if len(offsets) != symbols+1 || len(offsets) == 0 || offsets[0] != 0 || uint64(offsets[len(offsets)-1]) != uint64(len(edges)) {
		return false
	}
	for index := 1; index < len(offsets); index++ {
		if offsets[index] < offsets[index-1] {
			return false
		}
	}
	for _, edge := range edges {
		if uint64(edge.Target) >= uint64(symbols) {
			return false
		}
	}
	return true
}
