package hotsnapshot

import (
	"cmp"
	"slices"
	"sort"

	"github.com/Luqueee/kivgraph/internal/retrieval"
)

// termIndex answers "which symbols carry a token that folds to this term".
//
// It is the same CSR shape as symbolIndex, and for the same reason: keys
// ascending and unique, offsets bounding each run, one contiguous array of
// results, and a lookup that is a binary search over integers with no byte
// comparison anywhere. The difference is what a key is. symbolIndex keys on an
// InternedString, which is exact equality on a whole name; this keys on a
// retrieval.TermKey, which is a token folded to its first runes, so one key
// gathers the symbols a question can reach without spelling any name exactly.
//
// A run is ascending SymbolID because that is the order the rest of the surface
// pages in, and a caller that ranks the run still starts from a deterministic
// sequence.
type termIndex struct {
	keys    []retrieval.TermKey
	offsets []uint32
	values  []SymbolID
}

// termPosting is one (term, symbol) pair while the index is being built.
//
// symbolIndex packs its pair into a single uint64 and sorts without a
// comparator, which it can because both halves are uint32. A term key is a full
// uint64 on its own, so the pair cannot share a word and the sort needs a
// comparison function. It is the one place this file departs from the pattern
// next door, and it costs one dynamic call per comparison over a few hundred
// thousand entries rather than over two million.
type termPosting struct {
	key    retrieval.TermKey
	symbol SymbolID
}

// newTermIndex folds every symbol's own text into terms and inverts the result.
//
// The text of a symbol is its name, its qualified name, its kind and the path of
// the file that declares it. The path earns its place: it is the only one of the
// four that says what a symbol is *for* rather than what it is called, and in a
// repository whose directories are named after its subsystems that is most of
// what a plain-language question is actually asking about.
//
// Nothing here reads a docstring, because the graph does not store one. That is
// the ceiling of this index and it is worth naming: a question whose vocabulary
// appears in no identifier and no path has nothing to match, and no amount of
// ranking invents it.
func newTermIndex(symbols []SymbolRecord, files []FileRecord, strings StringTable) termIndex {
	if len(symbols) == 0 {
		return termIndex{offsets: []uint32{0}}
	}
	postings := make([]termPosting, 0, len(symbols)*4)
	var (
		spans []retrieval.Span
		keys  []retrieval.TermKey
	)
	for index, symbol := range symbols {
		keys = keys[:0]
		for _, interned := range [...]InternedString{symbol.Name, symbol.QualifiedName, symbol.Kind} {
			if text, ok := strings.String(interned); ok {
				keys, spans = retrieval.AppendTextKeys(keys, spans, text)
			}
		}
		if uint64(symbol.File) < uint64(len(files)) {
			if path, ok := strings.String(files[symbol.File].Path); ok {
				keys, spans = retrieval.AppendTextKeys(keys, spans, path)
			}
		}
		// A symbol that names the same term twice -- `Snapshot` in both its name
		// and its path -- is one posting, not two. Weighing that repetition is
		// the ranker's business, and a duplicated posting would instead corrupt
		// the one statistic this index publishes: how much of the corpus a term
		// matches.
		slices.Sort(keys)
		for _, key := range slices.Compact(keys) {
			postings = append(postings, termPosting{key: key, symbol: SymbolID(uint32(index))})
		}
	}
	slices.SortFunc(postings, func(left, right termPosting) int {
		return cmp.Or(cmp.Compare(left.key, right.key), cmp.Compare(left.symbol, right.symbol))
	})

	// Counted before allocating, so the run arrays are exact rather than grown.
	runs := 0
	for position := range postings {
		if position == 0 || postings[position].key != postings[position-1].key {
			runs++
		}
	}
	index := termIndex{
		keys:    make([]retrieval.TermKey, 0, runs),
		offsets: make([]uint32, 1, runs+1),
		values:  make([]SymbolID, len(postings)),
	}
	for position, posting := range postings {
		index.values[position] = posting.symbol
	}
	for position := 0; position < len(postings); {
		key := postings[position].key
		end := position + 1
		for end < len(postings) && postings[end].key == key {
			end++
		}
		index.keys = append(index.keys, key)
		index.offsets = append(index.offsets, uint32(end))
		position = end
	}
	return index
}

// lookup returns the run for key, or nil when no symbol folds to it. nil rather
// than an empty slice, so a miss reads as a miss the way the other indexes do.
func (index termIndex) lookup(key retrieval.TermKey) []SymbolID {
	position := sort.Search(len(index.keys), func(at int) bool { return index.keys[at] >= key })
	if position >= len(index.keys) || index.keys[position] != key {
		return nil
	}
	return index.values[index.offsets[position]:index.offsets[position+1]]
}

// validShape reports whether the three arrays bound each other. Like the
// indexes next door, agreement with the records is not something a check can
// establish once the constructor is their only producer.
func (index termIndex) validShape() bool {
	if len(index.offsets) != len(index.keys)+1 || index.offsets[0] != 0 {
		return false
	}
	for position := 1; position < len(index.offsets); position++ {
		if index.offsets[position] < index.offsets[position-1] {
			return false
		}
	}
	return int(index.offsets[len(index.offsets)-1]) == len(index.values)
}

// SymbolsByTerm returns the symbols whose own text carries a token folding to
// term, and how much of the corpus that term matches.
//
// The second return is the term's document frequency, and it is the number a
// caller needs to not be fooled by the first: a term matching a large share of
// the symbols separates nothing, and this is what replaces a stopword list.
// It is published rather than applied, because where the threshold sits is the
// ranker's decision and this index has no business making it.
//
// The run is copied, so nothing internal to the snapshot escapes.
func (snapshot *GraphSnapshot) SymbolsByTerm(term retrieval.TermKey) ([]SymbolID, int) {
	run := snapshot.symbolsByTerm.lookup(term)
	return append([]SymbolID(nil), run...), len(run)
}

// TermCount reports how many distinct terms the index holds, which is the only
// way a caller can tell an index that was built from an index that was not.
func (snapshot *GraphSnapshot) TermCount() int {
	return len(snapshot.symbolsByTerm.keys)
}

// IncomingCount reports how many resolved references point at a symbol.
//
// It reads the reverse CSR that already exists rather than counting anything: a
// symbol's incoming run is bounded by two offsets, so the answer is a
// subtraction. It is the one ranking signal no text search can have, and the
// reason a question answered from this graph beats the same question answered by
// matching names -- the number is what analysers resolved, not what looked alike.
func (snapshot *GraphSnapshot) IncomingCount(symbol SymbolID) int {
	if uint64(symbol)+1 >= uint64(len(snapshot.reverseOffsets)) {
		return 0
	}
	return int(snapshot.reverseOffsets[symbol+1] - snapshot.reverseOffsets[symbol])
}
