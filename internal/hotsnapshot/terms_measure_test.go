package hotsnapshot

import (
	"crypto/sha256"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/Luqueee/kivgraph/internal/retrieval"
)

// TestTermIndexOverThePublishedGraph measures the index against the generation
// this machine actually published, because every number that decided this
// design -- the prefix length, the absence of a stemmer, the absence of a
// stopword list -- was measured on real identifiers and has to stay measurable.
//
// It skips rather than fails without a published generation: a unit suite that
// depends on the developer's own store would fail on any other machine, and
// what this asserts is a shape, not a corpus.
func TestTermIndexOverThePublishedGraph(t *testing.T) {
	path := publishedSnapshotPath(t)
	if path == "" {
		t.Skip("no published snapshot on this machine")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Skipf("cannot read the published snapshot: %v", err)
	}
	// The zero digest is the documented way to read a file without asserting
	// which generation it belongs to, which is all this measurement needs.
	start := time.Now()
	snapshot, err := ReadSnapshot(data, [sha256.Size]byte{})
	if err != nil {
		t.Skipf("published snapshot is not readable by this build: %v", err)
	}
	loaded := time.Since(start)

	symbols := len(snapshot.symbols)
	terms := snapshot.TermCount()
	postings := len(snapshot.symbolsByTerm.values)
	if symbols == 0 || terms == 0 || postings == 0 {
		t.Fatalf("published graph has %d symbols, %d terms, %d postings", symbols, terms, postings)
	}
	bytes := terms*8 + (terms+1)*4 + postings*4
	t.Logf("symbols %d | terms %d | postings %d (%.2f per symbol) | index %.2f MB | load+derive %s",
		symbols, terms, postings, float64(postings)/float64(symbols),
		float64(bytes)/(1<<20), loaded.Round(time.Millisecond))

	// The whole point of folding: a question that spells no name exactly still
	// reaches the symbols. `traverse` must reach TraverseFrom, and the caller
	// never wrote `From`.
	found, frequency := snapshot.SymbolsByTerm(retrieval.Fold("traversal"))
	if len(found) == 0 {
		t.Fatal("the published graph holds no symbol under `traversal`")
	}
	t.Logf("traversal -> %d symbols (df %d)", len(found), frequency)

	// And the statistic that replaces a stopword list has to be informative:
	// the commonest term must match a large share of the corpus, or there is
	// nothing for a ranker to discount.
	widest, widestFrequency := retrieval.TermKeyNone, 0
	for position, key := range snapshot.symbolsByTerm.keys {
		run := int(snapshot.symbolsByTerm.offsets[position+1] - snapshot.symbolsByTerm.offsets[position])
		if run > widestFrequency {
			widest, widestFrequency = key, run
		}
	}
	share := float64(widestFrequency) / float64(symbols) * 100
	t.Logf("widest term matches %d of %d symbols (%.1f%%), key %d", widestFrequency, symbols, share, widest)
	if share < 1 {
		t.Fatalf("widest term matches only %.2f%% of the corpus, so document frequency separates nothing", share)
	}
}

// publishedSnapshotPath finds the active generation's snapshot without linking
// the storage layer: this package must stay readable without the native build
// tag, so the path is walked rather than resolved through the store.
func publishedSnapshotPath(t *testing.T) string {
	t.Helper()
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	generations := filepath.Join(home, ".local", "state", "kivgraph", "generations")
	entries, err := os.ReadDir(generations)
	if err != nil {
		return ""
	}
	newest := ""
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		candidate := filepath.Join(generations, entry.Name(), "snapshot.kvsnap")
		if _, err := os.Stat(candidate); err == nil && candidate > newest {
			newest = candidate
		}
	}
	return newest
}
