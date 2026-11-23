package indexer

import (
	"bytes"
	"encoding/binary"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"

	"github.com/Luqueee/kivgraph/internal/facts"
)

// richEntry has every kind of value a cache entry holds: strings that repeat and
// strings that do not, numbers of both signs, bools, nested slices, maps, and the
// two ways a slice can be empty.
func richEntry() cacheEntry {
	repository := facts.RepositoryKey("acme/widgets")
	file := facts.FileKey(repository, "widgets.go")
	return cacheEntry{
		Version:      cacheEntryVersion,
		Unit:         "go\x00acme\x00/work/acme\x00example.com/acme",
		Analyzer:     "analyzer",
		LocalAddress: "local",
		AnalyzerParts: map[string]string{
			"go":       "go1.26",
			"resolver": "0.10.2",
		},
		Inputs: []cacheInput{{Kind: inputFile, Name: "go.mod", Fingerprint: "abc"}, {Kind: inputProvider, Name: "p", Fingerprint: "absent"}},
		Set: facts.Set{
			Repositories: []facts.Repository{{Key: repository, Name: "acme/widgets", RootPath: "/work/acme", Languages: []facts.Language{facts.LanguageGo}, Dirty: true}},
			Files:        []facts.File{{Key: file, RepositoryKey: repository, Path: "widgets.go", Language: facts.LanguageGo, Generated: true}},
			Symbols: []facts.Symbol{{
				Key: "symbol:a", RepositoryKey: repository, FileKey: file, Name: "New", Kind: "func",
				Start: facts.Position{Line: 3, Column: 1, Offset: 40}, End: facts.Position{Line: 9, Offset: -1},
			}},
			Edges:      []facts.Edge{{Kind: facts.CallsDirect, SourceKey: "symbol:a", TargetKey: "symbol:a"}},
			Evidence:   []facts.Evidence{},
			Unresolved: nil,
		},
		NotLoaded:       true,
		LoadDiagnostics: -2,
		Detail:          "naïve — 数据\x00",
		Diagnostics:     []string{"one", "one", ""},
		Composed:        map[string]composedTarget{"k": {Repository: "r", Package: "p", Symbol: "s"}},
	}
}

// What goes in comes out, including what JSON told apart: a nil slice and an empty
// one, an empty string and an absent map. The verify mode of the cache compares a
// stored entry with a fresh analysis by what they marshal to, and those two
// differ by it.
func TestCacheEntryRoundTripsEveryKindOfValue(t *testing.T) {
	for name, entry := range map[string]cacheEntry{
		"a rich entry": richEntry(),
		"nothing":      {},
	} {
		got, err := decodeCacheEntry(encodeCacheEntry(entry))
		if err != nil {
			t.Fatalf("%s: decode error = %v", name, err)
		}
		if !reflect.DeepEqual(got, entry) {
			t.Errorf("%s: round trip changed the entry\n got %#v\nwant %#v", name, got, entry)
		}
	}
	got, err := decodeCacheEntry(encodeCacheEntry(richEntry()))
	if err != nil {
		t.Fatal(err)
	}
	if got.Set.Evidence == nil || got.Set.Unresolved != nil {
		t.Errorf("empty and nil slices came back as %#v and %#v", got.Set.Evidence, got.Set.Unresolved)
	}
}

// The same entry is the same bytes, whatever order a map was built in: the cache
// is content addressed and a build that differs by map order would never hit.
func TestCacheEntryBytesDoNotDependOnMapOrder(t *testing.T) {
	first := richEntry()
	first.AnalyzerParts = map[string]string{}
	second := first
	second.AnalyzerParts = map[string]string{}
	keys := []string{"a", "b", "c", "d", "e", "f", "g", "h"}
	for _, key := range keys {
		first.AnalyzerParts[key] = key + "v"
	}
	for index := len(keys) - 1; index >= 0; index-- {
		second.AnalyzerParts[keys[index]] = keys[index] + "v"
	}
	for range 20 {
		if !bytes.Equal(encodeCacheEntry(first), encodeCacheEntry(second)) {
			t.Fatal("the same entry encoded to different bytes")
		}
	}
}

// Nothing a file holds makes the decoder panic, or ask for the memory a file of
// that size could not have filled: each way for the bytes to be wrong is a
// refusal. The entries are damaged at every length and at every byte.
func TestCacheEntryRefusesEveryDamagedForm(t *testing.T) {
	valid := encodeCacheEntry(richEntry())

	for name, data := range map[string][]byte{
		"nothing":         nil,
		"the magic alone": []byte(cacheEntryMagic),
		"another magic":   append([]byte("JSON"), valid[4:]...),
		"a JSON entry":    []byte(`{"version":5,"unit":"go"}`),
		"trailing bytes":  append(append([]byte(nil), valid...), 0),
		"a header only":   valid[:len(cacheEntryMagic)+cacheFingerprintBytes],
		"no body":         valid[:bytes.Index(valid, []byte("naïve"))],
	} {
		if _, err := decodeCacheEntry(data); !errors.Is(err, errCacheEntryMalformed) {
			t.Errorf("%s: error = %v, want errCacheEntryMalformed", name, err)
		}
	}

	for length := range len(valid) {
		if _, err := decodeCacheEntry(valid[:length]); err == nil {
			t.Fatalf("an entry cut at %d of %d bytes was accepted", length, len(valid))
		}
	}

	for position := range valid {
		damaged := append([]byte(nil), valid...)
		damaged[position] ^= 0xff
		// Whatever it decodes to, it must come back: a flipped byte inside a
		// string is still a well formed entry, and what is not must be an error.
		_, _ = decodeCacheEntry(damaged)
	}
}

// Lengths are the way to ask for memory: each is checked against the bytes that
// remain, so an entry of a few hundred bytes cannot claim a billion strings or a
// billion elements.
func TestCacheEntryRefusesALengthTheFileCouldNotHold(t *testing.T) {
	valid := encodeCacheEntry(cacheEntry{})
	headerEnd := len(cacheEntryMagic) + cacheFingerprintBytes

	huge := binary.AppendUvarint(nil, 1<<40)
	for name, mutate := range map[string]func() []byte{
		"a unit longer than the file": func() []byte {
			return append(append([]byte(nil), valid[:headerEnd]...), huge...)
		},
		"a billion strings": func() []byte {
			data := append([]byte(nil), valid[:headerEnd]...)
			data = binary.AppendUvarint(data, 0) // the unit
			return append(data, binary.AppendUvarint(nil, 1<<31)...)
		},
		"a string table whose lengths overrun it": func() []byte {
			data := append([]byte(nil), valid[:headerEnd]...)
			data = binary.AppendUvarint(data, 0)
			data = binary.AppendUvarint(data, 2)
			data = binary.AppendUvarint(data, 5)
			data = binary.AppendUvarint(data, 1<<30)
			return append(data, "abc"...)
		},
		"a string that is not in the table": func() []byte {
			data := append([]byte(nil), valid[:headerEnd]...)
			data = binary.AppendUvarint(data, 0)
			data = binary.AppendUvarint(data, 0) // no string at all
			return append(data, 0)               // and the body wants string 0
		},
	} {
		if _, err := decodeCacheEntry(mutate()); !errors.Is(err, errCacheEntryMalformed) {
			t.Errorf("%s: error = %v, want errCacheEntryMalformed", name, err)
		}
	}
}

// A bool is one byte of 0 or 1, and nothing else is a value.
func TestCacheEntryRefusesABoolThatIsNeitherValue(t *testing.T) {
	valid := encodeCacheEntry(cacheEntry{NotLoaded: true})
	// NotLoaded is the one byte of 1 in an otherwise empty entry's body; no other
	// bool is true, so the last 1 in the file is it.
	position := bytes.LastIndexByte(valid, 1)
	damaged := append([]byte(nil), valid...)
	damaged[position] = 2
	if _, err := decodeCacheEntry(damaged); !errors.Is(err, errCacheEntryMalformed) {
		t.Fatalf("a bool of 2: error = %v, want errCacheEntryMalformed", err)
	}
}

// The structure is part of the file: an entry written by a build whose facts had
// another shape is refused as incompatible, never decoded into the wrong fields.
func TestCacheEntryOfAnotherStructureIsIncompatibleAndNotMalformed(t *testing.T) {
	valid := encodeCacheEntry(richEntry())
	for position := len(cacheEntryMagic); position < len(cacheEntryMagic)+cacheFingerprintBytes; position++ {
		damaged := append([]byte(nil), valid...)
		damaged[position] ^= 1
		_, err := decodeCacheEntry(damaged)
		if !errors.Is(err, errCacheEntryIncompatible) || errors.Is(err, errCacheEntryMalformed) {
			t.Fatalf("a structure that differs at byte %d: error = %v, want errCacheEntryIncompatible", position, err)
		}
	}
}

// The fingerprint reads the shape of the type and nothing else: it moves when a
// field is added, renamed, reordered or retyped, and not when a type is moved or
// an unexported field appears.
func TestDescribeEntryTypeReadsTheShapeOfAType(t *testing.T) {
	type base struct {
		Name  string
		Count int
		Tags  []string
	}
	type renamed struct {
		Label string
		Count int
		Tags  []string
	}
	type reordered struct {
		Count int
		Name  string
		Tags  []string
	}
	type retyped struct {
		Name  string
		Count int64
		Tags  []string
	}
	type withHidden struct {
		Name   string
		Count  int
		Tags   []string
		hidden bool
	}
	type extended struct {
		Name  string
		Count int
		Tags  []string
		More  bool
	}
	want := describeEntryType(reflect.TypeFor[base]())
	for name, kind := range map[string]reflect.Type{
		"renamed":   reflect.TypeFor[renamed](),
		"reordered": reflect.TypeFor[reordered](),
		"extended":  reflect.TypeFor[extended](),
	} {
		if describeEntryType(kind) == want {
			t.Errorf("a %s struct describes like the original: %s", name, want)
		}
	}
	// int and int64 are different kinds, so retyping a field is seen too.
	if describeEntryType(reflect.TypeFor[retyped]()) == want {
		t.Errorf("a retyped field describes like the original: %s", want)
	}
	if describeEntryType(reflect.TypeFor[withHidden]()) != want {
		t.Errorf("an unexported field changed the description")
	}
	_ = withHidden{hidden: true} // the field exists to be ignored
}

// A type the format cannot store is found when the plan is built, which is a
// test, and not by the first cache hit that reaches it.
func TestPlanEntryNodeRefusesATypeItCannotStore(t *testing.T) {
	type withPointer struct{ Next *string }
	type withFloat struct{ Ratio float64 }
	type withInterface struct{ Anything any }
	type withIntegerKeys struct{ Names map[int]string }
	type empty struct{}
	type slicesOfEmpty struct{ Nothing []empty }
	for name, kind := range map[string]reflect.Type{
		"a pointer":        reflect.TypeFor[withPointer](),
		"a float":          reflect.TypeFor[withFloat](),
		"an interface":     reflect.TypeFor[withInterface](),
		"integer keys":     reflect.TypeFor[withIntegerKeys](),
		"a struct of none": reflect.TypeFor[slicesOfEmpty](),
	} {
		func() {
			defer func() {
				if recover() == nil {
					t.Errorf("%s: the plan was built", name)
				}
			}()
			planEntryNode(kind)
		}()
	}
}

// The entry the cache stores is a type this format can store, and the fingerprint
// of it is a function of its shape.
func TestTheCacheEntryHasAPlan(t *testing.T) {
	if entryPlan == nil || len(entryPlan.fields) == 0 {
		t.Fatal("cacheEntry has no plan")
	}
	if strings.Count(describeEntryType(reflect.TypeFor[cacheEntry]()), "struct{") < 5 {
		t.Fatalf("the description of cacheEntry does not reach the facts: %s", describeEntryType(reflect.TypeFor[cacheEntry]()))
	}
}

// A slice or a map claims its length before its elements, and a length checked
// only against what memory allows would let a few bytes ask for a million of
// them: it is checked against what the bytes that remain could hold.
func TestCacheEntryRefusesASliceLongerThanTheBytesThatRemain(t *testing.T) {
	valid := encodeCacheEntry(cacheEntry{Diagnostics: []string{"a"}})
	// The body ends with the diagnostics -- a stored length of 2, which is one
	// element, and its string -- then the two nil collections that follow it.
	at := len(valid) - 4
	if valid[at] != 2 {
		t.Fatalf("the stored length of the diagnostics is %d, want 2 (one element)", valid[at])
	}
	damaged := append(append([]byte(nil), valid[:at]...), binary.AppendUvarint(nil, 5_000_000)...)
	damaged = append(damaged, valid[at+1:]...)
	var before, after runtime.MemStats
	runtime.ReadMemStats(&before)
	_, err := decodeCacheEntry(damaged)
	runtime.ReadMemStats(&after)
	if !errors.Is(err, errCacheEntryMalformed) {
		t.Fatalf("a slice of five million strings in four bytes: error = %v, want errCacheEntryMalformed", err)
	}
	// Refused is not enough: the slice of five million strings is eighty megabytes,
	// and the point of checking the length is never to allocate it.
	if allocated := after.TotalAlloc - before.TotalAlloc; allocated > 8<<20 {
		t.Fatalf("refusing the entry allocated %d bytes, want it refused before the slice is made", allocated)
	}
}

// Retention names a unit from the header alone, and from nothing else: a file
// cut inside the header, or one that claims more than a header may, has none.
func TestReadEntryUnitReadsTheHeaderAndNothingElse(t *testing.T) {
	directory := t.TempDir()
	write := func(name string, data []byte) string {
		path := filepath.Join(directory, name)
		if err := os.WriteFile(path, data, 0o600); err != nil {
			t.Fatal(err)
		}
		return path
	}
	valid := encodeCacheEntry(richEntry())
	headerEnd := len(cacheEntryMagic) + cacheFingerprintBytes

	if unit, ok := readEntryUnit(write("valid", valid)); !ok || unit != richEntry().Unit {
		t.Errorf("readEntryUnit(valid) = %q, %v, want %q", unit, ok, richEntry().Unit)
	}
	// Only the header is read: the facts after it are not needed.
	if unit, ok := readEntryUnit(write("header-only", valid[:headerEnd+1+len(richEntry().Unit)])); !ok || unit != richEntry().Unit {
		t.Errorf("readEntryUnit(header only) = %q, %v, want %q", unit, ok, richEntry().Unit)
	}
	for name, data := range map[string][]byte{
		"empty":             nil,
		"cut in the magic":  valid[:2],
		"cut in the print":  valid[:headerEnd-3],
		"cut in the unit":   valid[:headerEnd+3],
		"another magic":     append([]byte("JSON"), valid[4:]...),
		"a unit of no byte": append(append([]byte(nil), valid[:headerEnd]...), 0),
		"a unit too long":   append(append([]byte(nil), valid[:headerEnd]...), binary.AppendUvarint(nil, cacheMaximumUnitBytes+1)...),
		"no length at all":  valid[:headerEnd],
	} {
		if unit, ok := readEntryUnit(write(name, data)); ok {
			t.Errorf("%s: readEntryUnit = %q, true, want no unit", name, unit)
		}
	}
	if _, ok := readEntryUnit(filepath.Join(directory, "missing")); ok {
		t.Error("a file that is not there has a unit")
	}
}

// Whatever bytes it is given, the decoder returns: an error, or an entry that
// stores and reads back as itself.
func FuzzDecodeCacheEntry(f *testing.F) {
	valid := encodeCacheEntry(richEntry())
	f.Add(valid)
	f.Add(valid[:len(valid)/2])
	f.Add(encodeCacheEntry(cacheEntry{}))
	f.Add([]byte(cacheEntryMagic))
	f.Add([]byte{})
	f.Fuzz(func(t *testing.T, data []byte) {
		entry, err := decodeCacheEntry(data)
		if err != nil {
			return
		}
		again, err := decodeCacheEntry(encodeCacheEntry(entry))
		if err != nil || !reflect.DeepEqual(again, entry) {
			t.Fatalf("an entry that decoded did not survive being stored again: %v", err)
		}
	})
}
