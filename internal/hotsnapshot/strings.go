package hotsnapshot

import (
	"encoding/binary"
	"errors"
	"math"
	"slices"
	"strings"
)

// InternedString indexes one string in an immutable StringTable. It is internal
// to a snapshot and must not be used as external or persistent identity.
type InternedString uint32

const InvalidInternedString InternedString = math.MaxUint32

var (
	ErrInternerFrozen       = errors.New("string interner is frozen")
	ErrMalformedStringTable = errors.New("malformed string table")
)

// StringTableStats reports compact-table usage without exposing its storage.
type StringTableStats struct {
	Entries uint32
	Bytes   uint64
}

// StringTable is immutable after construction and safe for concurrent reads.
//
// Lookup is a binary search over ids sorted by their value, not a hash map.
// Measured on a real generation -- 481.494 interned values -- the map cost
// 20,45 MB in every process that held the snapshot, against 1,9 MB for four
// bytes per value, while Lookup is called once or twice per query to resolve a
// name. Ordering is also what proves the values are distinct: strictly
// increasing leaves no room for a duplicate, so no separate check has to agree
// with this one.
type StringTable struct {
	values []string
	order  []InternedString
	stats  StringTableStats
}

// String returns the interned value identified by id.
func (table StringTable) String(id InternedString) (string, bool) {
	if id == InvalidInternedString || uint64(id) >= uint64(len(table.values)) {
		return "", false
	}
	return table.values[id], true
}

// Lookup returns the ID assigned to value.
func (table StringTable) Lookup(value string) (InternedString, bool) {
	position, found := slices.BinarySearchFunc(table.order, value,
		func(id InternedString, target string) int { return strings.Compare(table.values[id], target) })
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
	size := uint64(4)
	for _, value := range table.values {
		size += 4 + uint64(len(value))
		if size > uint64(math.MaxInt) {
			return nil, ErrIDOverflow
		}
	}
	data := make([]byte, size)
	binary.LittleEndian.PutUint32(data, uint32(len(table.values)))
	offset := 4
	for _, value := range table.values {
		binary.LittleEndian.PutUint32(data[offset:], uint32(len(value)))
		offset += 4
		offset += copy(data[offset:], value)
	}
	return data, nil
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
	table.order = make([]InternedString, len(table.values))
	for index := range table.order {
		table.order[index] = InternedString(index)
	}
	slices.SortFunc(table.order, func(left, right InternedString) int {
		return strings.Compare(table.values[left], table.values[right])
	})
	if err := validateStringOrder(table.values, table.order); err != nil {
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
	if err := validateStringOrder(table.values, order); err != nil {
		return StringTable{}, err
	}
	table.order = order
	return table, nil
}

func validateStringOrder(values []string, order []InternedString) error {
	if len(order) != len(values) {
		return ErrMalformedStringTable
	}
	for position, id := range order {
		if uint64(id) >= uint64(len(values)) {
			return ErrMalformedStringTable
		}
		if position > 0 && strings.Compare(values[order[position-1]], values[id]) >= 0 {
			return ErrMalformedStringTable
		}
	}
	return nil
}

// parseStringValues reads the values in ID order, which is the whole format: no
// map iteration ever participates in it.
func parseStringValues(data []byte) (StringTable, error) {
	if len(data) < 4 {
		return StringTable{}, ErrMalformedStringTable
	}
	count := uint64(binary.LittleEndian.Uint32(data))
	if count >= uint64(math.MaxUint32) {
		return StringTable{}, ErrMalformedStringTable
	}
	values := make([]string, 0, count)
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
		values = append(values, string(data[offset:offset+length]))
		offset += length
		valueBytes += length
	}
	if offset != uint64(len(data)) {
		return StringTable{}, ErrMalformedStringTable
	}
	return StringTable{values: values, stats: StringTableStats{Entries: uint32(count), Bytes: valueBytes}}, nil
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
// The interner keeps a map while it builds, because interning is per row and has
// to be constant time. The table it hands over does not: it sorts the ids once
// here so that every process holding the result -- and a server holds it for its
// whole life -- carries four bytes per value instead of a hash table entry. The
// sort is paid where the peak already dies, in the pass that builds the graph.
func (interner *StringInterner) Freeze() StringTable {
	if interner.frozen {
		return StringTable{}
	}
	interner.frozen = true
	order := make([]InternedString, len(interner.values))
	for index := range order {
		order[index] = InternedString(index)
	}
	values := interner.values
	slices.SortFunc(order, func(left, right InternedString) int {
		return strings.Compare(values[left], values[right])
	})
	table := StringTable{
		values: values,
		order:  order,
		stats: StringTableStats{
			Entries: uint32(len(interner.values)),
			Bytes:   interner.bytes,
		},
	}
	interner.values = nil
	interner.index = nil
	return table
}
