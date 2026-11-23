package indexer

import (
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"math"
	"os"
	"reflect"
	"sort"
	"strings"
)

// An entry of the fact cache is a binary file, not JSON.
//
// A pass served from the cache spent most of its CPU decoding it: on a graph of
// `543,631` symbols and `2,020,464` edges the entries were `1,516 MB` of JSON,
// `7.1 s` to decode and `1.6 GB` of strings once decoded, and the largest single
// thing a pass held when it merged them. The same entries in this format are
// `384 MB`, `0.97 s` and `385 MB`: every distinct string of an entry is stored
// once, in a table at the front, and the rest is varints.
//
//	magic        4 bytes  "KVFE"
//	fingerprint 16 bytes  of the structure of cacheEntry (see structureFingerprint)
//	unit         uvarint length, then the unit's bytes
//	strings      uvarint count, count uvarint lengths, then the bytes of all of them
//	body         the entry, field by field
//
// The unit is in the header, in the clear, because retention reads it from the
// first bytes of a file without decoding anything else.
//
// The format follows the Go type of cacheEntry by reflection, so a field added
// to a fact is stored without anybody remembering to say so, and for the same
// reason the fingerprint exists: it hashes the names, order and kinds of every
// exported field the entry reaches, so an entry written by a build whose facts
// had a different shape is refused as incompatible, not decoded into the wrong
// fields.
const cacheEntryMagic = "KVFE"

const cacheFingerprintBytes = 16

var (
	// errCacheEntryIncompatible reports an entry of another structure: well
	// formed, written by a build whose facts looked different.
	errCacheEntryIncompatible = errors.New("fact cache entry has another structure")
	// errCacheEntryMalformed reports bytes that are not an entry of this format:
	// truncated, damaged, or not a cache file at all.
	errCacheEntryMalformed = errors.New("fact cache entry is malformed")
)

// entryNode is how one Go type is stored.
type entryNode struct {
	kind   reflect.Kind
	elem   *entryNode
	key    *entryNode
	fields []entryField
	// minimum is the fewest bytes a value of this type can take. A length is
	// checked against the bytes that remain divided by it, so a damaged length
	// cannot ask for more memory than the file could have held.
	minimum int
}

type entryField struct {
	index int
	node  *entryNode
}

// entryPlan is built once, from the type, and describes cacheEntry for good.
var entryPlan, entryFingerprint = func() (*entryNode, [cacheFingerprintBytes]byte) {
	kind := reflect.TypeOf(cacheEntry{})
	node := planEntryNode(kind)
	sum := sha256.Sum256([]byte("kvfe1\x00" + describeEntryType(kind)))
	var fingerprint [cacheFingerprintBytes]byte
	copy(fingerprint[:], sum[:])
	return node, fingerprint
}()

// planEntryNode describes how a type is stored, and panics on one it cannot
// store: a pointer, an interface or a float in a fact is a decision to make when
// it is written, not a surprise on the first cache hit. A test builds the plan.
func planEntryNode(kind reflect.Type) *entryNode {
	node := &entryNode{kind: kind.Kind()}
	switch kind.Kind() {
	case reflect.String, reflect.Bool,
		reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
		reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		node.minimum = 1
	case reflect.Slice:
		node.elem = planEntryNode(kind.Elem())
		node.minimum = 1
	case reflect.Map:
		if kind.Key().Kind() != reflect.String {
			panic(fmt.Sprintf("fact cache: map key of %s is not a string", kind))
		}
		node.key = planEntryNode(kind.Key())
		node.elem = planEntryNode(kind.Elem())
		node.minimum = 1
	case reflect.Struct:
		for index := range kind.NumField() {
			if !kind.Field(index).IsExported() {
				continue
			}
			field := planEntryNode(kind.Field(index).Type)
			node.fields = append(node.fields, entryField{index: index, node: field})
			node.minimum += field.minimum
		}
		if node.minimum == 0 {
			panic(fmt.Sprintf("fact cache: %s stores no byte, so a length of it could not be checked", kind))
		}
	default:
		panic(fmt.Sprintf("fact cache: %s (%s) cannot be stored", kind, kind.Kind()))
	}
	return node
}

// describeEntryType spells the structure of a type: names, order and kinds, and
// nothing that would change by moving a type to another package.
func describeEntryType(kind reflect.Type) string {
	switch kind.Kind() {
	case reflect.Slice:
		return "[]" + describeEntryType(kind.Elem())
	case reflect.Map:
		return "map[string]" + describeEntryType(kind.Elem())
	case reflect.Struct:
		var text strings.Builder
		text.WriteString("struct{")
		for index := range kind.NumField() {
			field := kind.Field(index)
			if field.IsExported() {
				text.WriteString(field.Name + " " + describeEntryType(field.Type) + ";")
			}
		}
		text.WriteString("}")
		return text.String()
	default:
		return kind.Kind().String()
	}
}

// encodeCacheEntry stores an entry. The bytes are the same for the same entry:
// strings are numbered in the order they first appear and a map is written in key
// order.
func encodeCacheEntry(entry cacheEntry) []byte {
	encoder := &entryEncoder{table: make(map[string]uint64)}
	encoder.write(entryPlan, reflect.ValueOf(entry))

	out := make([]byte, 0, len(encoder.body)+len(encoder.lengths)+encoder.size+len(entry.Unit)+64)
	out = append(out, cacheEntryMagic...)
	out = append(out, entryFingerprint[:]...)
	out = binary.AppendUvarint(out, uint64(len(entry.Unit)))
	out = append(out, entry.Unit...)
	out = binary.AppendUvarint(out, uint64(len(encoder.strings)))
	out = append(out, encoder.lengths...)
	for _, text := range encoder.strings {
		out = append(out, text...)
	}
	return append(out, encoder.body...)
}

type entryEncoder struct {
	table   map[string]uint64
	strings []string
	lengths []byte
	size    int
	body    []byte
}

func (encoder *entryEncoder) write(node *entryNode, value reflect.Value) {
	switch node.kind {
	case reflect.String:
		text := value.String()
		index, known := encoder.table[text]
		if !known {
			index = uint64(len(encoder.strings))
			encoder.table[text] = index
			encoder.strings = append(encoder.strings, text)
			encoder.lengths = binary.AppendUvarint(encoder.lengths, uint64(len(text)))
			encoder.size += len(text)
		}
		encoder.body = binary.AppendUvarint(encoder.body, index)
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		encoder.body = binary.AppendVarint(encoder.body, value.Int())
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		encoder.body = binary.AppendUvarint(encoder.body, value.Uint())
	case reflect.Bool:
		if value.Bool() {
			encoder.body = append(encoder.body, 1)
		} else {
			encoder.body = append(encoder.body, 0)
		}
	case reflect.Slice:
		// A nil slice and an empty one are two values: a length of nothing is nil,
		// and every other length is stored one more than it is.
		if value.IsNil() {
			encoder.body = append(encoder.body, 0)
			return
		}
		encoder.body = binary.AppendUvarint(encoder.body, uint64(value.Len())+1)
		for index := range value.Len() {
			encoder.write(node.elem, value.Index(index))
		}
	case reflect.Map:
		if value.IsNil() {
			encoder.body = append(encoder.body, 0)
			return
		}
		keys := value.MapKeys()
		sort.Slice(keys, func(left, right int) bool { return keys[left].String() < keys[right].String() })
		encoder.body = binary.AppendUvarint(encoder.body, uint64(len(keys))+1)
		for _, key := range keys {
			encoder.write(node.key, key)
			encoder.write(node.elem, value.MapIndex(key))
		}
	case reflect.Struct:
		for _, field := range node.fields {
			encoder.write(field.node, value.Field(field.index))
		}
	}
}

// decodeCacheEntry reads an entry, and refuses anything it does not fully
// understand: another structure is errCacheEntryIncompatible, and every other
// way for the bytes to be wrong -- a length past the end, a string that is not
// in the table, a bool that is neither, bytes left over -- is
// errCacheEntryMalformed. Nothing a file can hold makes it panic or ask for
// memory the file could not have filled.
func decodeCacheEntry(data []byte) (cacheEntry, error) {
	var entry cacheEntry
	decoder := &entryDecoder{data: data}
	if !decoder.expect(cacheEntryMagic) {
		return entry, errCacheEntryMalformed
	}
	fingerprint := decoder.take(cacheFingerprintBytes)
	if fingerprint == nil {
		return entry, errCacheEntryMalformed
	}
	if string(fingerprint) != string(entryFingerprint[:]) {
		return entry, errCacheEntryIncompatible
	}
	decoder.skip(decoder.length(1)) // the unit, which the body repeats

	count := decoder.length(1)
	lengths := make([]int, 0, count)
	total := 0
	for range count {
		length := decoder.uvarint()
		if decoder.failed || length > uint64(len(decoder.data)) {
			return entry, errCacheEntryMalformed
		}
		lengths = append(lengths, int(length))
		total += int(length)
		if total > len(decoder.data) {
			return entry, errCacheEntryMalformed
		}
	}
	blob := decoder.take(total)
	if decoder.failed {
		return entry, errCacheEntryMalformed
	}
	// One allocation holds every string of the entry, and each string is a piece
	// of it: decoding a string costs no allocation of its own.
	text := string(blob)
	decoder.strings = make([]string, count)
	offset := 0
	for index, length := range lengths {
		decoder.strings[index] = text[offset : offset+length]
		offset += length
	}

	decoder.read(entryPlan, reflect.ValueOf(&entry).Elem())
	if decoder.failed || len(decoder.data) != 0 {
		return cacheEntry{}, errCacheEntryMalformed
	}
	return entry, nil
}

type entryDecoder struct {
	data    []byte
	strings []string
	failed  bool
}

// take answers the next count bytes, or nil and a failure.
func (decoder *entryDecoder) take(count int) []byte {
	if decoder.failed || count < 0 || count > len(decoder.data) {
		decoder.failed = true
		return nil
	}
	taken := decoder.data[:count:count]
	decoder.data = decoder.data[count:]
	return taken
}

func (decoder *entryDecoder) expect(text string) bool {
	taken := decoder.take(len(text))
	return taken != nil && string(taken) == text
}

func (decoder *entryDecoder) skip(count int) {
	decoder.take(count)
}

func (decoder *entryDecoder) uvarint() uint64 {
	if decoder.failed {
		return 0
	}
	value, size := binary.Uvarint(decoder.data)
	if size <= 0 {
		decoder.failed = true
		return 0
	}
	decoder.data = decoder.data[size:]
	return value
}

func (decoder *entryDecoder) varint() int64 {
	if decoder.failed {
		return 0
	}
	value, size := binary.Varint(decoder.data)
	if size <= 0 {
		decoder.failed = true
		return 0
	}
	decoder.data = decoder.data[size:]
	return value
}

// length reads a count of things that each take at least minimum bytes, and
// fails when the bytes that remain could not hold that many.
func (decoder *entryDecoder) length(minimum int) int {
	length := decoder.uvarint()
	if decoder.failed || length > uint64(len(decoder.data)/minimum) || length > math.MaxInt32 {
		decoder.failed = true
		return 0
	}
	return int(length)
}

// count reads the length of a slice or a map as the encoder wrote it: nothing for
// nil, and one more than the length otherwise. It checks that length like length
// does.
func (decoder *entryDecoder) count(minimum int) (length int, present bool) {
	stored := decoder.uvarint()
	if decoder.failed || stored == 0 {
		return 0, false
	}
	stored--
	if stored > uint64(len(decoder.data)/minimum) || stored > math.MaxInt32 {
		decoder.failed = true
		return 0, false
	}
	return int(stored), true
}

func (decoder *entryDecoder) read(node *entryNode, target reflect.Value) {
	if decoder.failed {
		return
	}
	switch node.kind {
	case reflect.String:
		index := decoder.uvarint()
		if decoder.failed || index >= uint64(len(decoder.strings)) {
			decoder.failed = true
			return
		}
		target.SetString(decoder.strings[index])
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		value := decoder.varint()
		if decoder.failed || target.OverflowInt(value) {
			decoder.failed = true
			return
		}
		target.SetInt(value)
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		value := decoder.uvarint()
		if decoder.failed || target.OverflowUint(value) {
			decoder.failed = true
			return
		}
		target.SetUint(value)
	case reflect.Bool:
		taken := decoder.take(1)
		if taken == nil || taken[0] > 1 {
			decoder.failed = true
			return
		}
		target.SetBool(taken[0] == 1)
	case reflect.Slice:
		length, present := decoder.count(node.elem.minimum)
		if decoder.failed || !present {
			return
		}
		slice := reflect.MakeSlice(target.Type(), length, length)
		for index := range length {
			decoder.read(node.elem, slice.Index(index))
		}
		target.Set(slice)
	case reflect.Map:
		length, present := decoder.count(node.key.minimum + node.elem.minimum)
		if decoder.failed || !present {
			return
		}
		result := reflect.MakeMapWithSize(target.Type(), length)
		for range length {
			key := reflect.New(target.Type().Key()).Elem()
			decoder.read(node.key, key)
			element := reflect.New(target.Type().Elem()).Elem()
			decoder.read(node.elem, element)
			if decoder.failed {
				return
			}
			result.SetMapIndex(key, element)
		}
		target.Set(result)
	case reflect.Struct:
		for _, field := range node.fields {
			decoder.read(field.node, target.Field(field.index))
		}
	}
}

// readEntryUnit answers the unit an entry is about, from its first bytes: the
// header of the format holds it in the clear, so the facts that make an entry
// large are never read. An entry that is not of this format, or whose header does
// not say, has no unit.
func readEntryUnit(path string) (string, bool) {
	file, err := os.Open(path)
	if err != nil {
		return "", false
	}
	defer file.Close()
	prefix := make([]byte, len(cacheEntryMagic)+cacheFingerprintBytes+binary.MaxVarintLen64)
	read, err := io.ReadFull(file, prefix)
	if err != nil && read == 0 {
		return "", false
	}
	prefix = prefix[:read]
	if len(prefix) < len(cacheEntryMagic)+cacheFingerprintBytes || string(prefix[:len(cacheEntryMagic)]) != cacheEntryMagic {
		return "", false
	}
	length, size := binary.Uvarint(prefix[len(cacheEntryMagic)+cacheFingerprintBytes:])
	if size <= 0 || length == 0 || length > cacheMaximumUnitBytes {
		return "", false
	}
	unit := make([]byte, length)
	offset := int64(len(cacheEntryMagic) + cacheFingerprintBytes + size)
	if _, err := file.ReadAt(unit, offset); err != nil {
		return "", false
	}
	return string(unit), true
}

// cacheMaximumUnitBytes bounds what a header may claim for the unit, so a damaged
// one cannot make retention allocate what the file does not hold. The identity of
// a unit is a path, a name and a map of providers: kilobytes.
const cacheMaximumUnitBytes = 1 << 20
