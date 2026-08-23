package hotsnapshot

import (
	"cmp"
	"fmt"
	"slices"
	"sort"
)

// The indexes in this file replace the maps GraphSnapshot used to hold. A map is
// the last thing that stops the graph from being a sequence of bytes: its buckets
// are allocated by the runtime, keyed by a hash this process seeds at startup,
// and there is no layout a reader could address without rebuilding it. Flat
// arrays have one, so they can be mapped -- which is what LUQUE-2004 does next.
//
// Every one of them answers by binary search over integers. There is no byte
// comparison on the hot path: the keys are InternedString and dense IDs, not
// strings, so a lookup is a handful of comparisons over contiguous memory rather
// than a hash plus a bucket walk plus a key comparison.

// symbolIndex answers "which symbols carry this interned value".
//
// It is CSR, the same shape the edges already use: keys ascending and unique,
// offsets bounding each run, and one contiguous array of results. The runs keep
// ascending SymbolID order because that order is the contract -- pages and
// cursors depend on it -- and a run is handed out as a subslice that the page
// builder copies, so nothing internal escapes.
type symbolIndex struct {
	keys    []InternedString
	offsets []uint32
	values  []SymbolID
}

// newSymbolIndex derives the index from the records themselves.
//
// It used to flatten a map the caller accumulated, and that map was the largest
// thing a load allocated and threw away -- `16,5 MB` on `kena`, measured in
// `benchmarks/snapshot-heap`. Nothing was in it that the records did not
// already say: the key of symbol i is a field of symbol i, which is why the
// check that the two agreed could never fail.
//
// The construction sorts one array of packed integers. Key and id fit a uint64
// together -- both are uint32 -- so ordering the packed values orders by key and
// then by id, which is the order pages and cursors depend on, and the sort needs
// no comparator at all. Reading the key out of the record through a comparison
// function instead cost `18 ms` a load over this corpus: two dynamic calls per
// comparison, and two million comparisons.
//
// What it still throws away is that packed array -- eight bytes a symbol, `940
// kB` here against the map's `16,5 MB`.
func newSymbolIndex(symbols []SymbolRecord, keyFor func(SymbolRecord) InternedString) symbolIndex {
	if len(symbols) == 0 {
		return symbolIndex{offsets: []uint32{0}}
	}
	packed := make([]uint64, len(symbols))
	for index, symbol := range symbols {
		packed[index] = uint64(keyFor(symbol))<<32 | uint64(uint32(index))
	}
	slices.Sort(packed)
	// Counted before allocating, so the two run arrays are exact rather than
	// grown: an index over one key would otherwise reserve room for as many
	// runs as there are symbols.
	runs := 1
	for position := 1; position < len(packed); position++ {
		if packed[position]>>32 != packed[position-1]>>32 {
			runs++
		}
	}
	index := symbolIndex{
		keys:    make([]InternedString, 0, runs),
		offsets: make([]uint32, 1, runs+1),
		values:  make([]SymbolID, len(packed)),
	}
	for position, entry := range packed {
		index.values[position] = SymbolID(uint32(entry))
	}
	for position := 0; position < len(packed); {
		key := InternedString(packed[position] >> 32)
		end := position + 1
		for end < len(packed) && InternedString(packed[end]>>32) == key {
			end++
		}
		index.keys = append(index.keys, key)
		index.offsets = append(index.offsets, uint32(end))
		position = end
	}
	return index
}

// lookup returns the run for key, or nil when the index does not hold it.
//
// nil rather than an empty slice: the map this replaced answered a miss with a
// nil slice, and the accessors above copy whatever comes back, so a miss has to
// keep producing a nil page rather than an allocated empty one.
func (index symbolIndex) lookup(key InternedString) []SymbolID {
	position := sort.Search(len(index.keys), func(at int) bool { return index.keys[at] >= key })
	if position >= len(index.keys) || index.keys[position] != key {
		return nil
	}
	return index.values[index.offsets[position]:index.offsets[position+1]]
}

// validShape reports whether the three arrays bound each other.
//
// It is not the check this used to carry. That one re-derived the index from the
// records and compared, which was worth doing while a caller could hand one in;
// now that the constructor above is the only way one exists, comparing it
// against the records it was built from is comparing a function to itself. What
// is left is the shape a reader indexes blindly -- the same treatment
// packageIncomingIndex already gets, and for the same reason.
func (index symbolIndex) validShape(symbols int) bool {
	if len(index.offsets) != len(index.keys)+1 || len(index.values) != symbols {
		return false
	}
	return index.offsets[len(index.keys)] == uint32(len(index.values))
}

// fileIndex answers an exact repository/path pair.
//
// Two sorted arrays rather than CSR, because the answer is one file: a path is
// unique inside a repository, and the snapshot refuses to open otherwise.
type fileIndex struct {
	keys  []RepoPathKey
	files []FileID
}

func repoPathLess(left, right RepoPathKey) bool {
	if left.Repository != right.Repository {
		return left.Repository < right.Repository
	}
	return left.Path < right.Path
}

// newFileIndex derives the index from the file records, and it is where a
// repository holding two files at one path is refused.
//
// That rejection used to live in indexSnapshotInput, which needed a map to
// notice a key it had already seen. Sorting notices it without one: duplicates
// land next to each other. The reason it is refused at all is unchanged -- a
// dense ID that two keys resolve to is a wrong answer no query can detect.
func newFileIndex(files []FileRecord) (fileIndex, error) {
	if len(files) == 0 {
		return fileIndex{}, nil
	}
	order := make([]FileID, len(files))
	for index := range files {
		order[index] = FileID(index)
	}
	keyOf := func(id FileID) RepoPathKey {
		return RepoPathKey{Repository: files[id].Repository, Path: files[id].Path}
	}
	slices.SortFunc(order, func(left, right FileID) int {
		if repoPathLess(keyOf(left), keyOf(right)) {
			return -1
		}
		if repoPathLess(keyOf(right), keyOf(left)) {
			return 1
		}
		return cmp.Compare(left, right)
	})
	index := fileIndex{keys: make([]RepoPathKey, len(order)), files: order}
	for position, id := range order {
		index.keys[position] = keyOf(id)
		if position > 0 && index.keys[position] == index.keys[position-1] {
			return fileIndex{}, fmt.Errorf("repository %d holds two files at path %d",
				index.keys[position].Repository, index.keys[position].Path)
		}
	}
	return index, nil
}

func (index fileIndex) lookup(key RepoPathKey) (FileID, bool) {
	position := sort.Search(len(index.keys), func(at int) bool { return !repoPathLess(index.keys[at], key) })
	if position >= len(index.keys) || index.keys[position] != key {
		return 0, false
	}
	return index.files[position], true
}

// validShape reports whether the two arrays name every file once each. Like the
// symbol index above, agreement with the records is no longer something a check
// can establish: the constructor is the only producer, and it reads those very
// records.
func (index fileIndex) validShape(files int) bool {
	return len(index.keys) == files && len(index.files) == files
}

// packageIncomingIndex answers which dependencies point at a package.
//
// It needs no key array at all, which is what makes it the cheapest of the
// three: PackageID is already a dense index into the package table, so the
// offsets are addressed by the id itself. What it stores are positions in the
// dependency table rather than copies of the rows, so a package that is depended
// on twenty times costs twenty uint32s instead of twenty records.
type packageIncomingIndex struct {
	offsets []uint32
	values  []uint32
}

func newPackageIncomingIndex(packages int, dependencies []PackageDependencyRecord) packageIncomingIndex {
	counts := make([]uint32, packages+1)
	for _, dependency := range dependencies {
		if uint64(dependency.Target) < uint64(packages) {
			counts[dependency.Target+1]++
		}
	}
	for position := 1; position <= packages; position++ {
		counts[position] += counts[position-1]
	}
	index := packageIncomingIndex{offsets: counts, values: make([]uint32, counts[packages])}
	// cursor walks a copy of the offsets, so the offsets stay the boundaries
	// they were counted into while the values are filled in dependency order --
	// which is what keeps each run in the order the map version produced.
	cursor := append([]uint32(nil), counts[:packages]...)
	for position, dependency := range dependencies {
		if uint64(dependency.Target) >= uint64(packages) {
			continue
		}
		index.values[cursor[dependency.Target]] = uint32(position)
		cursor[dependency.Target]++
	}
	return index
}

// rows materializes the records pointing at target, in the order the dependency
// table holds them.
func (index packageIncomingIndex) rows(target PackageID, dependencies []PackageDependencyRecord) []PackageDependencyRecord {
	if uint64(target)+1 >= uint64(len(index.offsets)) {
		return nil
	}
	start, end := index.offsets[target], index.offsets[target+1]
	if start == end {
		return nil
	}
	out := make([]PackageDependencyRecord, 0, end-start)
	for _, position := range index.values[start:end] {
		out = append(out, dependencies[position])
	}
	return out
}
