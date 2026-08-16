package hotsnapshot

import (
	"encoding/binary"
	"errors"
	"math"
	"slices"
	"strings"
	"unsafe"
)

// InternedString indexes one string in an immutable StringTable. It is internal
// to a snapshot and must not be used as external or persistent identity.
type InternedString uint32

const InvalidInternedString InternedString = math.MaxUint32

var (
	ErrMalformedStringTable = errors.New("malformed string table")
	ErrInternerFrozen       = errors.New("string interner is frozen")
)

// StringTableStats reports compact-table usage without exposing its storage.
type StringTableStats struct {
	Entries uint32
	Bytes   uint64
}

// StringTable is immutable after construction and safe for concurrent reads.
//
// The values live in one arena with an offset per id, not as a []string. Half a
// million Go strings are half a million allocations, each rounded up to a size
// class, plus sixteen bytes of header apiece -- 7,35 MB of headers alone on a
// real generation. An arena is four bytes per value, one allocation, and the one
// shape a mapped file could ever be: the tables around it already are.
//
// Lookup is a binary search over ids sorted by their value, not a hash map. That
// map cost 20,45 MB in every process that held the snapshot, against 1,9 MB for
// the order, while Lookup is called once or twice per query to resolve a name.
// The order is also what proves the values are distinct: strictly increasing
// leaves no room for a duplicate, so no separate check has to agree with this
// one.
type StringTable struct {
	arena   []byte
	offsets []uint32
	order   []InternedString
	stats   StringTableStats
}

// value answers one interned value without copying it.
//
// unsafe.String is sound here for one reason, and the reason is the type's own
// contract: the arena is written once while the table is built and never again,
// so no reader can observe it change and no writer can invalidate what a reader
// holds. A table that started mutating its arena would break every string it
// ever handed out.
func (table StringTable) value(id InternedString) string {
	start, end := table.offsets[id], table.offsets[id+1]
	if start == end {
		return ""
	}
	return unsafe.String(&table.arena[start], int(end-start))
}

// String returns the interned value identified by id.
func (table StringTable) String(id InternedString) (string, bool) {
	if id == InvalidInternedString || uint64(id)+1 >= uint64(len(table.offsets)) {
		return "", false
	}
	return table.value(id), true
}

// Lookup returns the ID assigned to value.
func (table StringTable) Lookup(value string) (InternedString, bool) {
	position, found := slices.BinarySearchFunc(table.order, value,
		func(id InternedString, target string) int { return strings.Compare(table.value(id), target) })
	if !found {
		return InvalidInternedString, false
	}
	return table.order[position], true
}

// Stats returns table cardinality and the bytes occupied by values.
func (table StringTable) Stats() StringTableStats { return table.stats }

// MarshalBinary serializes values in ID order. IDs are reconstructed from that
// order on load; no map iteration participates in the format.
func (table StringTable) MarshalBinary() ([]byte, error) {
	count := table.entries()
	size := uint64(4) + uint64(count)*4 + uint64(len(table.arena))
	if size > uint64(math.MaxInt) {
		return nil, ErrIDOverflow
	}
	data := make([]byte, size)
	binary.LittleEndian.PutUint32(data, uint32(count))
	offset := 4
	for id := range InternedString(count) {
		value := table.value(id)
		binary.LittleEndian.PutUint32(data[offset:], uint32(len(value)))
		offset += 4
		offset += copy(data[offset:], value)
	}
	return data, nil
}

func (table StringTable) entries() int {
	if len(table.offsets) == 0 {
		return 0
	}
	return len(table.offsets) - 1
}

// UnmarshalStringTable restores a table serialized by MarshalBinary, sorting its
// values to build the lookup order. A caller that already has that order -- a
// published snapshot carries it, because it is derived and deterministic --
// should hand it over instead: sorting half a million values is the one
// expensive thing about loading a table.
func UnmarshalStringTable(data []byte) (StringTable, error) {
	table, err := parseStringValues(data)
	if err != nil {
		return StringTable{}, err
	}
	count := table.entries()
	table.order = make([]InternedString, count)
	for index := range table.order {
		table.order[index] = InternedString(index)
	}
	slices.SortFunc(table.order, func(left, right InternedString) int {
		return strings.Compare(table.value(left), table.value(right))
	})
	if err := table.validateOrder(table.order); err != nil {
		return StringTable{}, err
	}
	return table, nil
}

// unmarshalStringTableWithOrder restores a table whose lookup order was read
// rather than computed, and refuses one that is not an ordering of its values.
//
// The check is what makes reading it safe, and it is cheaper than the sort it
// replaces: strictly increasing values prove the order is a permutation of
// distinct values -- a repeated id cannot be strictly greater than itself -- in
// one pass instead of n log n. A silently wrong order would not corrupt the
// graph, which is worse than if it did: every Lookup would just quietly answer
// with another value's id.
func unmarshalStringTableWithOrder(data []byte, order []InternedString) (StringTable, error) {
	table, err := parseStringValues(data)
	if err != nil {
		return StringTable{}, err
	}
	if err := table.validateOrder(order); err != nil {
		return StringTable{}, err
	}
	table.order = order
	return table, nil
}

func (table StringTable) validateOrder(order []InternedString) error {
	count := table.entries()
	if len(order) != count {
		return ErrMalformedStringTable
	}
	for position, id := range order {
		if int(id) >= count {
			return ErrMalformedStringTable
		}
		if position > 0 && strings.Compare(table.value(order[position-1]), table.value(id)) >= 0 {
			return ErrMalformedStringTable
		}
	}
	return nil
}

// parseStringValues reads the values in ID order into one arena, which is the
// whole format: no map iteration ever participates in it.
//
// The lengths are validated in a first pass so the arena can be allocated once,
// at exactly its size. Growing it would copy tens of megabytes for nothing.
func parseStringValues(data []byte) (StringTable, error) {
	if len(data) < 4 {
		return StringTable{}, ErrMalformedStringTable
	}
	count := uint64(binary.LittleEndian.Uint32(data))
	if count >= uint64(math.MaxUint32) {
		return StringTable{}, ErrMalformedStringTable
	}
	offset := uint64(4)
	var valueBytes uint64
	for range count {
		if offset+4 > uint64(len(data)) {
			return StringTable{}, ErrMalformedStringTable
		}
		length := uint64(binary.LittleEndian.Uint32(data[offset:]))
		offset += 4
		if length > uint64(len(data))-offset {
			return StringTable{}, ErrMalformedStringTable
		}
		offset += length
		valueBytes += length
	}
	if offset != uint64(len(data)) {
		return StringTable{}, ErrMalformedStringTable
	}
	if valueBytes > uint64(math.MaxUint32) {
		return StringTable{}, ErrIDOverflow
	}

	arena := make([]byte, 0, valueBytes)
	offsets := make([]uint32, 0, count+1)
	offset = 4
	for range count {
		length := uint64(binary.LittleEndian.Uint32(data[offset:]))
		offset += 4
		offsets = append(offsets, uint32(len(arena)))
		arena = append(arena, data[offset:offset+length]...)
		offset += length
	}
	offsets = append(offsets, uint32(len(arena)))
	return StringTable{
		arena:   arena,
		offsets: offsets,
		stats:   StringTableStats{Entries: uint32(count), Bytes: valueBytes},
	}, nil
}

// StringInterner assigns IDs while a snapshot is built. Freeze transfers its
// storage into a StringTable and permanently prevents further insertion.
type StringInterner struct {
	values []string
	index  map[string]InternedString
	bytes  uint64
	frozen bool
}

func NewStringInterner() *StringInterner {
	return &StringInterner{index: make(map[string]InternedString)}
}

// Intern returns the existing ID for value or assigns its next dense ID.
func (interner *StringInterner) Intern(value string) (InternedString, error) {
	if interner.frozen {
		return InvalidInternedString, ErrInternerFrozen
	}
	if id, exists := interner.index[value]; exists {
		return id, nil
	}
	if len(interner.values) >= math.MaxUint32 {
		return InvalidInternedString, ErrIDOverflow
	}
	id := InternedString(len(interner.values))
	interner.values = append(interner.values, value)
	interner.index[value] = id
	interner.bytes += uint64(len(value))
	return id, nil
}

// Freeze transfers table ownership and prevents any subsequent insertions.
//
// The interner keeps a map and a []string while it builds, because interning is
// per row and has to be constant time. What it hands over is neither: the values
// are copied into one arena and the ids sorted once, so that every process
// holding the result -- and a server holds it for its whole life -- carries four
// bytes per value twice over instead of a hash table entry and a string header.
// Both costs are paid where the peak already dies, in the pass that builds the
// graph.
func (interner *StringInterner) Freeze() StringTable {
	if interner.frozen {
		return StringTable{}
	}
	interner.frozen = true
	arena := make([]byte, 0, interner.bytes)
	offsets := make([]uint32, 0, len(interner.values)+1)
	for _, value := range interner.values {
		offsets = append(offsets, uint32(len(arena)))
		arena = append(arena, value...)
	}
	offsets = append(offsets, uint32(len(arena)))
	table := StringTable{
		arena:   arena,
		offsets: offsets,
		stats: StringTableStats{
			Entries: uint32(len(interner.values)),
			Bytes:   interner.bytes,
		},
	}
	table.order = make([]InternedString, len(interner.values))
	for index := range table.order {
		table.order[index] = InternedString(index)
	}
	slices.SortFunc(table.order, func(left, right InternedString) int {
		return strings.Compare(table.value(left), table.value(right))
	})
	interner.values = nil
	interner.index = nil
	return table
}
