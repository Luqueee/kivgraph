package hotsnapshot

import (
	"fmt"
	"reflect"
	"strings"
	"testing"
)

// corpusKeys builds keys with the shape a real one has -- 52 base32 characters --
// because the table's cost and its binary search both depend on key length, and a
// three-character fixture would exercise neither.
func corpusKeys(count int) []StableKey {
	keys := make([]StableKey, count)
	for index := range keys {
		keys[index] = StableKey(fmt.Sprintf("%s%040d", "kv", index))
	}
	return keys
}

func TestStableKeyTableResolvesEveryKeyToItsOwnID(t *testing.T) {
	keys := corpusKeys(4096)
	table, err := NewStableKeyTable(keys)
	if err != nil {
		t.Fatalf("NewStableKeyTable() error = %v", err)
	}
	if table.Entries() != uint32(len(keys)) {
		t.Fatalf("entries = %d, want %d", table.Entries(), len(keys))
	}
	for index, want := range keys {
		id, found := table.Lookup(want)
		if !found || id != StableKeyID(index) {
			t.Fatalf("Lookup(%q) = %d, %v; want %d, true", want, id, found, index)
		}
		got, ok := table.Key(StableKeyID(index))
		if !ok || got != want {
			t.Fatalf("Key(%d) = %q, %v; want %q, true", index, got, ok, want)
		}
	}
}

func TestStableKeyTableRefusesAKeyItDoesNotHold(t *testing.T) {
	table, err := NewStableKeyTable(corpusKeys(64))
	if err != nil {
		t.Fatalf("NewStableKeyTable() error = %v", err)
	}
	// A miss has to be a miss and not the neighbour a binary search landed on:
	// a prefix, a suffix and a value past the end all sit next to a real entry.
	for _, absent := range []StableKey{"", "kv", "kv0000000000000000000000000000000000000000000", "zz", StableKey("kv" + strings.Repeat("9", 40))} {
		if id, found := table.Lookup(absent); found {
			t.Fatalf("Lookup(%q) = %d, true; want a miss", absent, id)
		}
	}
}

func TestNewStableKeyTableRefusesInputItCannotIndex(t *testing.T) {
	// Every one of these would produce a table that answers wrongly rather than
	// failing: a binary search over unordered entries finds nothing where
	// something is, and a duplicate makes two symbols share one ID.
	for name, keys := range map[string][]StableKey{
		"unordered":  {"kv-b", "kv-a"},
		"duplicated": {"kv-a", "kv-a"},
		"empty key":  {""},
		"empty then": {"", "kv-a"},
	} {
		if _, err := NewStableKeyTable(keys); err == nil {
			t.Fatalf("NewStableKeyTable(%s) accepted %v", name, keys)
		}
	}
}

func TestStableKeyTableFromArenaValidatesWhatItIsHanded(t *testing.T) {
	table, err := NewStableKeyTable(corpusKeys(8))
	if err != nil {
		t.Fatalf("NewStableKeyTable() error = %v", err)
	}
	arena, offsets := table.Arena()

	if _, err := StableKeyTableFromArena(arena, offsets, true); err != nil {
		t.Fatalf("StableKeyTableFromArena() rejected its own storage: %v", err)
	}

	// The offsets are the only thing that says where a key ends, so each of
	// these is a key read from the wrong bytes -- a symbol answering to another
	// symbol's identity, which no later validation can catch.
	broken := map[string]func() ([]byte, []uint32){
		"no offsets":      func() ([]byte, []uint32) { return arena, nil },
		"first not zero":  func() ([]byte, []uint32) { return arena, append([]uint32{1}, offsets[1:]...) },
		"past the arena":  func() ([]byte, []uint32) { return arena[:len(arena)-1], offsets },
		"going backwards": func() ([]byte, []uint32) { return arena, swapped(offsets, 1, 2) },
	}
	for name, mutate := range broken {
		mutatedArena, mutatedOffsets := mutate()
		if _, err := StableKeyTableFromArena(mutatedArena, mutatedOffsets, false); err == nil {
			t.Fatalf("StableKeyTableFromArena(%s) accepted malformed storage", name)
		}
	}
}

func swapped(offsets []uint32, left, right int) []uint32 {
	out := append([]uint32(nil), offsets...)
	out[left], out[right] = out[right], out[left]
	return out
}

// TestStableKeyTableCopiesWhatItIsGiven is the defect this table exists for. A
// snapshot that stored the caller's strings would keep the buffer they were read
// from reachable: measured at 58 MB pinned by 6.4 MB of keys on kena.
func TestStableKeyTableCopiesWhatItIsGiven(t *testing.T) {
	source := []byte("kv-aaaa")
	keys := []StableKey{StableKey(source)}
	table, err := NewStableKeyTable(keys)
	if err != nil {
		t.Fatalf("NewStableKeyTable() error = %v", err)
	}
	arena, _ := table.Arena()
	if len(arena) > 0 && len(source) > 0 && &arena[0] == &source[0] {
		t.Fatal("the table aliases the caller's bytes instead of copying them")
	}
	if got, ok := table.Key(0); !ok || got != "kv-aaaa" {
		t.Fatalf("Key(0) = %q, %v; want \"kv-aaaa\", true", got, ok)
	}
}

// TestBorrowedStableKeyTableHandsOutCopies covers the other half: a mapped arena
// the collector cannot see, where a view would name freed pages once the mapping
// went away.
func TestBorrowedStableKeyTableHandsOutCopies(t *testing.T) {
	owned, err := NewStableKeyTable(corpusKeys(4))
	if err != nil {
		t.Fatalf("NewStableKeyTable() error = %v", err)
	}
	arena, offsets := owned.Arena()
	borrowed, err := StableKeyTableFromArena(arena, offsets, true)
	if err != nil {
		t.Fatalf("StableKeyTableFromArena() error = %v", err)
	}
	key, ok := borrowed.Key(2)
	if !ok {
		t.Fatal("Key(2) not found in a borrowed table")
	}
	if len(key) == 0 || &[]byte(string(key))[0] == &arena[offsets[2]] {
		t.Fatal("a borrowed table handed out a view into the arena")
	}
	if want, _ := owned.Key(2); key != want {
		t.Fatalf("borrowed Key(2) = %q, want %q", key, want)
	}
}

// TestMappableTablesHoldNoPointers is the invariant the dense key exists for. A
// record with a pointer in it cannot be read out of a mapped file: the pointer
// would name this process's heap, and nothing in the file could restore it.
//
// It covers every record the snapshot stores as a dense table rather than only
// SymbolRecord, because the next task in this phase maps them together and a
// pointer smuggled into any one of them breaks the same way.
func TestMappableTablesHoldNoPointers(t *testing.T) {
	for _, table := range []any{
		SymbolRecord{}, RepositoryRecord{}, PackageRecord{}, FileRecord{},
		EvidenceRecord{}, PackageDependencyRecord{}, UnresolvedReferenceRecord{},
		PackedEdge{},
	} {
		recordType := reflect.TypeOf(table)
		for index := range recordType.NumField() {
			field := recordType.Field(index)
			switch field.Type.Kind() {
			case reflect.Ptr, reflect.String, reflect.Slice, reflect.Map,
				reflect.Interface, reflect.Chan, reflect.Func, reflect.UnsafePointer:
				t.Errorf("%s.%s is a %s, which cannot be read from a mapped file",
					recordType.Name(), field.Name, field.Type.Kind())
			}
		}
	}
}
