package hotsnapshot

import "sort"

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

// newSymbolIndex flattens the caller's map.
//
// The map is transient by design: it is the ergonomic way to accumulate the
// index while records are being read, and this is where it stops existing. What
// the snapshot keeps is only the three arrays.
func newSymbolIndex(source map[InternedString][]SymbolID) symbolIndex {
	if len(source) == 0 {
		return symbolIndex{offsets: []uint32{0}}
	}
	keys := make([]InternedString, 0, len(source))
	total := 0
	for key, ids := range source {
		keys = append(keys, key)
		total += len(ids)
	}
	sort.Slice(keys, func(left, right int) bool { return keys[left] < keys[right] })

	index := symbolIndex{
		keys:    keys,
		offsets: make([]uint32, 1, len(keys)+1),
		values:  make([]SymbolID, 0, total),
	}
	for _, key := range keys {
		ids := source[key]
		// Sorted rather than trusted. The builder appends in ascending id
		// order, but map iteration is not the only way this index is fed, and a
		// run out of order would reorder a published page instead of failing.
		run := append([]SymbolID(nil), ids...)
		sort.Slice(run, func(left, right int) bool { return run[left] < run[right] })
		index.values = append(index.values, run...)
		index.offsets = append(index.offsets, uint32(len(index.values)))
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

// valid reports whether every symbol appears exactly once, under the key its own
// record carries. It is the invariant the map version checked, kept whole: an
// index that agreed with itself but not with the records would answer with
// another symbol's id, and no query could detect that.
func (index symbolIndex) valid(symbols []SymbolRecord, keyFor func(SymbolRecord) InternedString) bool {
	if len(index.offsets) != len(index.keys)+1 {
		return false
	}
	if len(index.keys) > 0 && index.offsets[len(index.keys)] != uint32(len(index.values)) {
		return false
	}
	seen := make([]bool, len(symbols))
	for position, key := range index.keys {
		if position > 0 && index.keys[position-1] >= key {
			return false
		}
		start, end := index.offsets[position], index.offsets[position+1]
		if start > end || uint64(end) > uint64(len(index.values)) {
			return false
		}
		for _, id := range index.values[start:end] {
			if uint64(id) >= uint64(len(symbols)) || seen[id] || keyFor(symbols[id]) != key {
				return false
			}
			seen[id] = true
		}
	}
	for _, found := range seen {
		if !found {
			return false
		}
	}
	return true
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

func newFileIndex(source map[RepoPathKey]FileID) fileIndex {
	if len(source) == 0 {
		return fileIndex{}
	}
	keys := make([]RepoPathKey, 0, len(source))
	for key := range source {
		keys = append(keys, key)
	}
	sort.Slice(keys, func(left, right int) bool { return repoPathLess(keys[left], keys[right]) })
	index := fileIndex{keys: keys, files: make([]FileID, 0, len(keys))}
	for _, key := range keys {
		index.files = append(index.files, source[key])
	}
	return index
}

func (index fileIndex) lookup(key RepoPathKey) (FileID, bool) {
	position := sort.Search(len(index.keys), func(at int) bool { return !repoPathLess(index.keys[at], key) })
	if position >= len(index.keys) || index.keys[position] != key {
		return 0, false
	}
	return index.files[position], true
}

// valid reports whether the index names every file exactly once, under the
// repository and path the file record itself carries.
func (index fileIndex) valid(files []FileRecord) bool {
	if len(index.keys) != len(files) || len(index.files) != len(files) {
		return false
	}
	for id, file := range files {
		found, exists := index.lookup(RepoPathKey{Repository: file.Repository, Path: file.Path})
		if !exists || found != FileID(id) {
			return false
		}
	}
	for position := 1; position < len(index.keys); position++ {
		if !repoPathLess(index.keys[position-1], index.keys[position]) {
			return false
		}
	}
	return true
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
