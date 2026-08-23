package hotsnapshot

import (
	"fmt"
	"math"
	"math/rand/v2"
	"slices"
	"strings"
	"testing"
)

// The index constructors in indexes.go had no test of their own until the maps
// they replaced were retired. What covered them was eighty integration tests
// that happened to build a snapshot, which is why sabotaging one field of the
// packed key surfaced as a failure in find_references rather than here.
//
// The oracle below is the retired map, written out again. It is not a
// tautology: newSymbolIndex sorts packed integers and walks runs, the oracle
// appends into a map and sorts each bucket, and the only thing they share is the
// specification. When they disagree, one of them is wrong.

func oracleSymbolIndex(symbols []SymbolRecord, keyFor func(SymbolRecord) InternedString) map[InternedString][]SymbolID {
	out := make(map[InternedString][]SymbolID)
	for index, symbol := range symbols {
		key := keyFor(symbol)
		out[key] = append(out[key], SymbolID(index))
	}
	for _, run := range out {
		slices.Sort(run)
	}
	return out
}

// symbolsWithNames builds records carrying the given name keys. Only Name and
// QualifiedName matter here: the index reads nothing else.
func symbolsWithNames(names ...InternedString) []SymbolRecord {
	records := make([]SymbolRecord, len(names))
	for index, name := range names {
		// The qualified name is deliberately a different function of the row, so
		// a test that confuses the two indexes fails instead of passing by
		// coincidence.
		records[index] = SymbolRecord{Name: name, QualifiedName: name + 1000}
	}
	return records
}

func TestNewSymbolIndexAgreesWithTheMapItReplaced(t *testing.T) {
	for _, testCase := range []struct {
		name  string
		names []InternedString
	}{
		{"empty", nil},
		{"one symbol", []InternedString{7}},
		{"one key, many symbols", []InternedString{4, 4, 4, 4}},
		{"every symbol its own key", []InternedString{1, 2, 3, 4}},
		{"records in descending key order", []InternedString{9, 7, 5, 3, 1}},
		{"runs interleaved in record order", []InternedString{2, 1, 2, 1, 2}},
		{"key zero present", []InternedString{0, 5, 0}},
		{"key at the top of the range", []InternedString{math.MaxUint32, 0, math.MaxUint32}},
		{"adjacent keys", []InternedString{5, 6, 5, 6}},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			records := symbolsWithNames(testCase.names...)
			for _, direction := range []struct {
				label  string
				keyFor func(SymbolRecord) InternedString
			}{
				{"name", symbolName},
				{"qualified name", symbolQualifiedName},
			} {
				index := newSymbolIndex(records, direction.keyFor)
				want := oracleSymbolIndex(records, direction.keyFor)
				if got := len(index.keys); got != len(want) {
					t.Fatalf("%s: keys = %d, want %d", direction.label, got, len(want))
				}
				for key, run := range want {
					if got := index.lookup(key); !slices.Equal(got, run) {
						t.Fatalf("%s: lookup(%d) = %v, want %v", direction.label, key, got, run)
					}
				}
				// A key nobody carries has to answer nil rather than an empty
				// slice: the accessor copies whatever comes back, and a page of
				// no rows and a page that does not exist are different answers.
				for _, absent := range []InternedString{math.MaxUint32 - 1, 999_999} {
					if _, present := want[absent]; present {
						continue
					}
					if got := index.lookup(absent); got != nil {
						t.Fatalf("%s: lookup(%d) = %v, want nil", direction.label, absent, got)
					}
				}
			}
		})
	}
}

// TestNewSymbolIndexHoldsTheInvariantsItsLookupNeeds pins the three properties a
// binary search over runs depends on. A constructor that broke any of them would
// still answer most queries, which is why they are asserted rather than assumed.
func TestNewSymbolIndexHoldsTheInvariantsItsLookupNeeds(t *testing.T) {
	// Enough symbols to leave the insertion-sort path of the sort, with keys
	// drawn from a small alphabet so runs are long and the fixed seed keeps the
	// case reproducible.
	source := rand.New(rand.NewPCG(1, 2))
	records := make([]SymbolRecord, 400)
	for index := range records {
		key := InternedString(source.IntN(17))
		records[index] = SymbolRecord{Name: key, QualifiedName: key + 1000}
	}
	index := newSymbolIndex(records, symbolName)

	if len(index.offsets) != len(index.keys)+1 {
		t.Fatalf("offsets = %d for %d keys", len(index.offsets), len(index.keys))
	}
	if index.offsets[0] != 0 || int(index.offsets[len(index.keys)]) != len(records) {
		t.Fatalf("offsets do not bound the values: %v", index.offsets)
	}
	// The run count is taken before allocating precisely so these two are not
	// grown, which is a claim only their capacity can settle: 17 keys over 400
	// symbols would otherwise reserve room for 400.
	if cap(index.keys) != len(index.keys) || cap(index.offsets) != len(index.offsets) {
		t.Fatalf("run arrays were grown: keys %d/%d, offsets %d/%d",
			len(index.keys), cap(index.keys), len(index.offsets), cap(index.offsets))
	}
	for position := 1; position < len(index.keys); position++ {
		if index.keys[position-1] >= index.keys[position] {
			t.Fatalf("keys not strictly ascending at %d: %v", position, index.keys[:position+1])
		}
	}
	seen := make([]bool, len(records))
	for position, key := range index.keys {
		run := index.values[index.offsets[position]:index.offsets[position+1]]
		if len(run) == 0 {
			t.Fatalf("key %d has an empty run", key)
		}
		if !slices.IsSorted(run) {
			t.Fatalf("run of key %d is not ascending: %v", key, run)
		}
		for _, id := range run {
			if seen[id] {
				t.Fatalf("symbol %d appears twice", id)
			}
			seen[id] = true
			if records[id].Name != key {
				t.Fatalf("symbol %d is filed under %d but carries %d", id, key, records[id].Name)
			}
		}
	}
	for id, found := range seen {
		if !found {
			t.Fatalf("symbol %d is in no run", id)
		}
	}
}

func filesAt(pairs ...[2]InternedString) []FileRecord {
	records := make([]FileRecord, len(pairs))
	for index, pair := range pairs {
		records[index] = FileRecord{Repository: RepositoryID(pair[0]), Path: pair[1]}
	}
	return records
}

func TestNewFileIndexResolvesEveryFileAndOrdersItsKeys(t *testing.T) {
	// Repository and path both out of order, and one path repeated across two
	// repositories -- which is not a duplicate, and is the case a key that
	// ignored the repository would break.
	files := filesAt(
		[2]InternedString{2, 10},
		[2]InternedString{0, 30},
		[2]InternedString{1, 10},
		[2]InternedString{0, 10},
		[2]InternedString{2, 5},
	)
	index, err := newFileIndex(files)
	if err != nil {
		t.Fatalf("newFileIndex() error = %v", err)
	}
	for id, file := range files {
		found, exists := index.lookup(RepoPathKey{Repository: file.Repository, Path: file.Path})
		if !exists || found != FileID(id) {
			t.Fatalf("lookup(%d,%d) = %d, %t; want %d", file.Repository, file.Path, found, exists, id)
		}
	}
	want := []RepoPathKey{{0, 10}, {0, 30}, {1, 10}, {2, 5}, {2, 10}}
	if !slices.Equal(index.keys, want) {
		t.Fatalf("keys = %v, want %v", index.keys, want)
	}
	for _, absent := range []RepoPathKey{{0, 11}, {3, 10}, {1, 30}} {
		if _, exists := index.lookup(absent); exists {
			t.Fatalf("lookup(%v) found a file", absent)
		}
	}
}

func TestNewFileIndexRefusesTwoFilesAtOnePath(t *testing.T) {
	for _, testCase := range []struct {
		name  string
		files []FileRecord
	}{
		{"adjacent in record order", filesAt([2]InternedString{1, 4}, [2]InternedString{1, 4})},
		{"apart in record order", filesAt(
			[2]InternedString{1, 4}, [2]InternedString{0, 9}, [2]InternedString{1, 4})},
		{"three at one path", filesAt(
			[2]InternedString{1, 4}, [2]InternedString{1, 4}, [2]InternedString{1, 4})},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			_, err := newFileIndex(testCase.files)
			if err == nil {
				t.Fatal("newFileIndex() accepted two files at one path")
			}
			// The message has to name the pair: this error reaches an operator
			// through a load that failed, and a repository id alone does not say
			// which path to look at.
			if want := "repository 1 holds two files at path 4"; !strings.Contains(err.Error(), want) {
				t.Fatalf("error = %q, want it to contain %q", err, want)
			}
		})
	}
}

func TestNewFileIndexAndSymbolIndexAreEmptyForNoRecords(t *testing.T) {
	index, err := newFileIndex(nil)
	if err != nil {
		t.Fatalf("newFileIndex(nil) error = %v", err)
	}
	if index.keys != nil || index.files != nil {
		t.Fatalf("newFileIndex(nil) = %#v, want the zero value", index)
	}
	if _, exists := index.lookup(RepoPathKey{}); exists {
		t.Fatal("an empty file index answered a lookup")
	}
	// The symbol index keeps a single offset instead of nothing, because lookup
	// indexes offsets[position+1] and a key count of zero still has to bound.
	symbols := newSymbolIndex(nil, symbolName)
	if !slices.Equal(symbols.offsets, []uint32{0}) || symbols.keys != nil || symbols.values != nil {
		t.Fatalf("newSymbolIndex(nil) = %#v", symbols)
	}
	if got := symbols.lookup(0); got != nil {
		t.Fatalf("lookup on an empty index = %v, want nil", got)
	}
}

// TestValidShapeRejectsArraysThatDoNotBound covers what replaced the agreement
// check. Agreement is no longer establishable -- the constructor reads the very
// records a check would compare against -- so what is left is the shape a reader
// indexes blindly.
func TestValidShapeRejectsArraysThatDoNotBound(t *testing.T) {
	records := symbolsWithNames(1, 1, 2)
	sound := newSymbolIndex(records, symbolName)
	if !sound.validShape(len(records)) {
		t.Fatal("a derived index failed its own shape check")
	}
	for name, broken := range map[string]symbolIndex{
		"offsets one short":    {keys: sound.keys, offsets: sound.offsets[:len(sound.offsets)-1], values: sound.values},
		"values short of rows": {keys: sound.keys, offsets: sound.offsets, values: sound.values[:1]},
		// The one case that only the row count catches: the arrays bound each
		// other perfectly and still describe fewer symbols than there are.
		"consistent but two symbols short": {
			keys:    sound.keys[:1],
			offsets: []uint32{0, 1},
			values:  sound.values[:1],
		},
		"last offset too small": {keys: sound.keys, offsets: []uint32{0, 1, 2}, values: sound.values},
	} {
		if broken.validShape(len(records)) {
			t.Fatalf("%s passed the shape check", name)
		}
	}
	files := filesAt([2]InternedString{0, 1}, [2]InternedString{0, 2})
	fileSound, err := newFileIndex(files)
	if err != nil {
		t.Fatalf("newFileIndex() error = %v", err)
	}
	if !fileSound.validShape(len(files)) {
		t.Fatal("a derived file index failed its own shape check")
	}
	if (fileIndex{keys: fileSound.keys, files: fileSound.files[:1]}).validShape(len(files)) {
		t.Fatal("a file index with fewer ids than keys passed the shape check")
	}
}

// TestPackageIncomingIndexKeepsDependencyOrder is the third index, which was
// already derived. It is here because the other two now are too, and the three
// answer the same class of question.
func TestPackageIncomingIndexKeepsDependencyOrder(t *testing.T) {
	dependencies := []PackageDependencyRecord{
		{Source: 0, Target: 2},
		{Source: 1, Target: 0},
		{Source: 2, Target: 2},
		// Out of range, so it is counted by nobody and must not shift the runs
		// of the packages that exist.
		{Source: 0, Target: 9},
		{Source: 0, Target: 2},
	}
	index := newPackageIncomingIndex(3, dependencies)
	for target, want := range map[PackageID][]PackageDependencyRecord{
		0: {{Source: 1, Target: 0}},
		1: nil,
		2: {{Source: 0, Target: 2}, {Source: 2, Target: 2}, {Source: 0, Target: 2}},
	} {
		got := index.rows(target, dependencies)
		if fmt.Sprint(got) != fmt.Sprint(want) {
			t.Fatalf("rows(%d) = %v, want %v", target, got, want)
		}
	}
	if got := index.rows(9, dependencies); got != nil {
		t.Fatalf("rows(out of range) = %v, want nil", got)
	}
}
