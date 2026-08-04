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
