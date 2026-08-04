package hotsnapshot

import (
	"encoding/binary"
	"errors"
	"math"
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
type StringTable struct {
	values []string
	index  map[string]InternedString
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
	id, found := table.index[value]
	return id, found
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

// UnmarshalStringTable restores a table serialized by MarshalBinary. Duplicate
// values are rejected because each string must have precisely one dense ID.
func UnmarshalStringTable(data []byte) (StringTable, error) {
	if len(data) < 4 {
		return StringTable{}, ErrMalformedStringTable
	}
	count := uint64(binary.LittleEndian.Uint32(data))
	if count >= uint64(math.MaxUint32) {
		return StringTable{}, ErrMalformedStringTable
	}
	values := make([]string, 0, count)
	index := make(map[string]InternedString, count)
	offset := uint64(4)
	var valueBytes uint64
	for id := uint64(0); id < count; id++ {
		if offset+4 > uint64(len(data)) {
			return StringTable{}, ErrMalformedStringTable
		}
		length := uint64(binary.LittleEndian.Uint32(data[offset:]))
		offset += 4
		if length > uint64(len(data))-offset {
			return StringTable{}, ErrMalformedStringTable
		}
		value := string(data[offset : offset+length])
		offset += length
		if _, exists := index[value]; exists {
			return StringTable{}, ErrMalformedStringTable
		}
		index[value] = InternedString(id)
		values = append(values, value)
		valueBytes += length
	}
	if offset != uint64(len(data)) {
		return StringTable{}, ErrMalformedStringTable
	}
	return StringTable{values: values, index: index, stats: StringTableStats{Entries: uint32(count), Bytes: valueBytes}}, nil
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
func (interner *StringInterner) Freeze() StringTable {
	if interner.frozen {
		return StringTable{}
	}
	interner.frozen = true
	table := StringTable{
		values: interner.values,
		index:  interner.index,
		stats: StringTableStats{
			Entries: uint32(len(interner.values)),
			Bytes:   interner.bytes,
		},
	}
	interner.values = nil
	interner.index = nil
	return table
}
