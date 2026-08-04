package hotsnapshot

import (
	"errors"
	"math"
	"testing"
)

func TestIDAllocatorAssignsZeroBasedDenseIDs(t *testing.T) {
	allocator := &IDAllocator{}
	allocators := []struct {
		name     string
		allocate func(*IDAllocator) (uint64, error)
	}{
		{"repository", func(value *IDAllocator) (uint64, error) { id, err := value.Repository(); return uint64(id), err }},
		{"package", func(value *IDAllocator) (uint64, error) { id, err := value.Package(); return uint64(id), err }},
		{"file", func(value *IDAllocator) (uint64, error) { id, err := value.File(); return uint64(id), err }},
		{"symbol", func(value *IDAllocator) (uint64, error) { id, err := value.Symbol(); return uint64(id), err }},
		{"evidence", func(value *IDAllocator) (uint64, error) { id, err := value.Evidence(); return uint64(id), err }},
		{"edge", func(value *IDAllocator) (uint64, error) { id, err := value.Edge(); return uint64(id), err }},
	}
	for _, test := range allocators {
		t.Run(test.name, func(t *testing.T) {
			first, err := test.allocate(allocator)
			if err != nil || first != 0 {
				t.Fatalf("first ID = %d, %v; want 0, nil", first, err)
			}
			second, err := test.allocate(allocator)
			if err != nil || second != 1 {
				t.Fatalf("second ID = %d, %v; want 1, nil", second, err)
			}
		})
	}
	if got, want := allocator.Counts(), (IDCounts{Repositories: 2, Packages: 2, Files: 2, Symbols: 2, Evidence: 2, Edges: 2}); got != want {
		t.Fatalf("Counts() = %#v, want %#v", got, want)
	}
}

func TestIDAllocatorRestartsForEachSnapshot(t *testing.T) {
	first := &IDAllocator{}
	if _, err := first.Symbol(); err != nil {
		t.Fatal(err)
	}
	second := &IDAllocator{}
	id, err := second.Symbol()
	if err != nil || id != 0 {
		t.Fatalf("new allocator Symbol() = %d, %v; want 0, nil", id, err)
	}
}

func TestIDAllocatorRejectsExhaustion(t *testing.T) {
	allocator := &IDAllocator{counts: IDCounts{Symbols: math.MaxUint32, Edges: math.MaxUint64}}
	if id, err := allocator.Symbol(); !errors.Is(err, ErrIDOverflow) || id != InvalidSymbolID {
		t.Fatalf("Symbol() = %d, %v; want %d, ErrIDOverflow", id, err, InvalidSymbolID)
	}
	if id, err := allocator.Edge(); !errors.Is(err, ErrIDOverflow) || id != InvalidEdgeID {
		t.Fatalf("Edge() = %d, %v; want %d, ErrIDOverflow", id, err, InvalidEdgeID)
	}
	if got, want := allocator.Counts(), (IDCounts{Symbols: math.MaxUint32, Edges: math.MaxUint64}); got != want {
		t.Fatalf("Counts() after overflow = %#v, want %#v", got, want)
	}
}
