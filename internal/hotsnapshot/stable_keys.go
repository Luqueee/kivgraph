package hotsnapshot

import (
	"errors"
	"math"
	"sort"
	"strings"
	"unsafe"
)

// ErrMalformedStableKeyTable reports a key table whose offsets do not describe
// its arena, or whose entries are not in strict byte order.
var ErrMalformedStableKeyTable = errors.New("hotsnapshot: malformed stable key table")

// StableKeyID addresses one stable key inside a snapshot's key table. It is a
// dense index, so a SymbolRecord can name its key without holding a pointer --
// which is the whole point: a table with a pointer in it cannot be mapped.
type StableKeyID uint32

// InvalidStableKeyID is the zero value no entry ever has.
const InvalidStableKeyID StableKeyID = math.MaxUint32

// StableKeyTableStats reports table usage without exposing its storage.
type StableKeyTableStats struct {
	Entries uint32
	Bytes   uint64
}

// StableKeyTable holds every symbol's stable key in one arena, stored in byte
// order.
//
// It is deliberately not the StringTable the names live in. Keys are unique per
// symbol, so interning them there would add one entry per symbol to a reverse
// index that exists to find names -- and that index is one of the maps this
// phase has to retire.
//
// Being stored in order is what removes the hash table. StringTable needs a
// separate `order` slice because insertion order and byte order differ there;
// here the builder receives keys already sorted -- symbols are sorted by stable
// key before any ID is allocated -- so entry i is both the i-th key in byte
// order and the key of SymbolID i. A lookup is a binary search over the offsets
// themselves, and it answers with the symbol's own ID.
type StableKeyTable struct {
	arena   []byte
	offsets []uint32
	stats   StableKeyTableStats
	// borrowed says the arena is memory this table does not own -- a mapped
	// file. It changes exactly one thing, and it has to: what Key copies.
	borrowed bool
}

// value answers one key without copying it.
//
// unsafe.String is sound here for the same reason it is in StringTable: the
// arena is written once while the table is built and never again, so no reader
// can observe it change and no writer can invalidate what a reader holds.
//
// It is unexported because what it returns must not leave the package when the
// arena is borrowed. Inside, it is what makes Lookup free: it compares and
// discards.
func (table StableKeyTable) value(id StableKeyID) StableKey {
	start, end := table.offsets[id], table.offsets[id+1]
	if start == end {
		return ""
	}
	return StableKey(unsafe.String(&table.arena[start], int(end-start)))
}

// Key returns the stable key identified by id.
//
// A borrowed arena is mapped memory the collector cannot see, so a key pointing
// into it would not keep the mapping alive and would answer from freed pages
// once the snapshot became unreachable. What leaves this package is therefore a
// copy when the arena is borrowed, and a view when the table owns its bytes.
func (table StableKeyTable) Key(id StableKeyID) (StableKey, bool) {
	if id == InvalidStableKeyID || uint64(id)+1 >= uint64(len(table.offsets)) {
		return "", false
	}
	value := table.value(id)
	if table.borrowed {
		return StableKey(strings.Clone(string(value))), true
	}
	return value, true
}

// Lookup returns the ID of key, which is the SymbolID of the symbol it names.
//
// The search is over positions rather than a slice of keys, because a slice of
// keys is exactly what this table exists not to hold: value() reads each
// candidate out of the arena and discards it.
func (table StableKeyTable) Lookup(key StableKey) (StableKeyID, bool) {
	entries := int(table.Entries())
	position := sort.Search(entries, func(index int) bool {
		return table.value(StableKeyID(index)) >= key
	})
	if position < entries && table.value(StableKeyID(position)) == key {
		return StableKeyID(position), true
	}
	return InvalidStableKeyID, false
}

// Entries returns how many keys the table holds.
func (table StableKeyTable) Entries() uint32 {
	if len(table.offsets) == 0 {
		return 0
	}
	return uint32(len(table.offsets) - 1)
}

// Stats returns table cardinality and the bytes occupied by keys.
func (table StableKeyTable) Stats() StableKeyTableStats { return table.stats }

// Arena exposes the bytes and offsets for serialization. The caller must not
// mutate either: they are the table's own storage.
func (table StableKeyTable) Arena() ([]byte, []uint32) { return table.arena, table.offsets }

// NewStableKeyTable copies keys into an arena the table owns.
//
// The copy is the point, not an implementation detail. The keys a builder
// receives are views over the buffer the database was read through -- ScanCanonical
// hands out every value as a string over the scan's own arena -- so a snapshot
// that stored those views would keep that entire buffer reachable for as long as
// it lived. Measured on kena: 6.4 MB of keys pinned 58 MB of read buffers, a
// third of the resident footprint. See benchmarks/hot-snapshot-footprint.
//
// Strict byte order is required rather than sorted here, because the order is
// load-bearing: it is what lets a lookup binary search and what makes entry i
// the key of SymbolID i. A caller that has not sorted its symbols is not a
// caller whose keys can be silently reordered -- its IDs would move.
func NewStableKeyTable(keys []StableKey) (StableKeyTable, error) {
	total := 0
	for index, key := range keys {
		if key == "" {
			return StableKeyTable{}, ErrMalformedStableKeyTable
		}
		if index > 0 && keys[index-1] >= key {
			return StableKeyTable{}, ErrMalformedStableKeyTable
		}
		total += len(key)
	}
	if total > math.MaxUint32 {
		return StableKeyTable{}, ErrMalformedStableKeyTable
	}
	arena := make([]byte, 0, total)
	offsets := make([]uint32, 1, len(keys)+1)
	for _, key := range keys {
		arena = append(arena, key...)
		offsets = append(offsets, uint32(len(arena)))
	}
	return StableKeyTable{
		arena:   arena,
		offsets: offsets,
		stats:   StableKeyTableStats{Entries: uint32(len(keys)), Bytes: uint64(total)},
	}, nil
}

// StableKeyTableFromArena builds a table over an arena and its offsets,
// borrowing the bytes rather than copying them.
//
// borrowed is what a caller promises about the arena's lifetime: that it
// outlives the table. Mapped memory does not outlive its munmap, so whoever
// passes true owns keeping it mapped for as long as anything can reach the
// table.
//
// The offsets are validated rather than trusted, because they are the only
// thing that says where a key starts and ends, and the byte order is validated
// for the same reason: a lookup that binary searches unordered entries does not
// fail, it answers wrong.
func StableKeyTableFromArena(arena []byte, offsets []uint32, borrowed bool) (StableKeyTable, error) {
	if len(offsets) == 0 {
		return StableKeyTable{}, ErrMalformedStableKeyTable
	}
	var keyBytes uint64
	for index := 1; index < len(offsets); index++ {
		if offsets[index] <= offsets[index-1] || uint64(offsets[index]) > uint64(len(arena)) {
			return StableKeyTable{}, ErrMalformedStableKeyTable
		}
		keyBytes += uint64(offsets[index] - offsets[index-1])
	}
	if offsets[0] != 0 || uint64(offsets[len(offsets)-1]) != uint64(len(arena)) {
		return StableKeyTable{}, ErrMalformedStableKeyTable
	}
	table := StableKeyTable{
		arena:   arena,
		offsets: offsets,
		stats:   StableKeyTableStats{Entries: uint32(len(offsets) - 1), Bytes: keyBytes},
		// Set before validating the order, because validation reads through
		// value, and borrowing is what value describes.
		borrowed: borrowed,
	}
	for id := StableKeyID(1); uint64(id) < uint64(table.Entries()); id++ {
		if table.value(id-1) >= table.value(id) {
			return StableKeyTable{}, ErrMalformedStableKeyTable
		}
	}
	return table, nil
}
