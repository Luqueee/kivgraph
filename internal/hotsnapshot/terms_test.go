package hotsnapshot

import (
	"bytes"
	"crypto/sha256"
	"fmt"
	"reflect"
	"testing"
	"time"

	"github.com/Luqueee/kivgraph/internal/retrieval"
)

// TestTermIndexFoldsWhatAQuestionCanReach pins the contract: a term reaches the
// symbols whose own text carries it, whether that text is the name, the
// qualified name, the kind or the path, and it reaches them without the caller
// spelling any of them exactly.
func TestTermIndexFoldsWhatAQuestionCanReach(t *testing.T) {
	snapshot, err := BuildGraphSnapshot(builderRows(), 1, time.Unix(1, 0).UTC(), 1)
	if err != nil {
		t.Fatal(err)
	}

	// s-a and s-b are both named `shared`; s-c is named `c`, which is one rune
	// and folds to nothing.
	found, frequency := snapshot.SymbolsByTerm(retrieval.Fold("shared"))
	if len(found) != 2 || frequency != 2 {
		t.Fatalf("shared = %v (df %d), want both declarations", found, frequency)
	}
	if found[0] >= found[1] {
		t.Fatalf("run = %v, want ascending symbol order", found)
	}

	// The path is indexed, so the file a symbol lives in is a way to reach it.
	// Both fixture files are `src/<letter>.ts`, so `src` reaches everything.
	if found, frequency := snapshot.SymbolsByTerm(retrieval.Fold("src")); len(found) != 3 || frequency != 3 {
		t.Fatalf("src = %v (df %d), want every symbol", found, frequency)
	}

	// The kind is indexed too: these are all functions.
	if found, _ := snapshot.SymbolsByTerm(retrieval.Fold("function")); len(found) != 3 {
		t.Fatalf("function = %v, want every symbol", found)
	}

	// A term nothing folds to is a miss, and a miss is empty rather than wrong.
	if found, frequency := snapshot.SymbolsByTerm(retrieval.Fold("absent")); len(found) != 0 || frequency != 0 {
		t.Fatalf("absent = %v (df %d), want nothing", found, frequency)
	}

	// A one-rune token folds to nothing, so it can never be looked up: `c` is a
	// real symbol name in the fixture and still must not be a term.
	if key := retrieval.Fold("c"); key != retrieval.TermKeyNone {
		t.Fatalf("Fold(%q) = %d, want no key for a single rune", "c", key)
	}
}

// TestTermIndexCountsASymbolOncePerTerm guards the one statistic the index
// publishes. `A.shared` carries `shared` in both its name and its qualified
// name; if that counted twice, the document frequency a ranker reads would say
// the term is commoner than it is, and the ranker's whole job is to weigh that.
func TestTermIndexCountsASymbolOncePerTerm(t *testing.T) {
	snapshot, err := BuildGraphSnapshot(builderRows(), 1, time.Unix(1, 0).UTC(), 1)
	if err != nil {
		t.Fatal(err)
	}
	found, frequency := snapshot.SymbolsByTerm(retrieval.Fold("shared"))
	if len(found) != frequency {
		t.Fatalf("run %v disagrees with frequency %d", found, frequency)
	}
	seen := map[SymbolID]int{}
	for _, id := range found {
		seen[id]++
	}
	for id, count := range seen {
		if count != 1 {
			t.Fatalf("symbol %d appears %d times in one run", id, count)
		}
	}
}

// TestTermIndexSurvivesAnEmptyGraph keeps the empty case a shape rather than a
// special case: an index over no symbols must still bound its own arrays, or
// the snapshot's validity check would reject a legitimately empty generation.
func TestTermIndexSurvivesAnEmptyGraph(t *testing.T) {
	index := newTermIndex(nil, nil, StringTable{})
	if !index.validShape() {
		t.Fatalf("empty index = %#v, want a valid shape", index)
	}
	if index.lookup(retrieval.Fold("anything")) != nil {
		t.Fatal("empty index answered a lookup")
	}
}

// generatedTermRows builds a corpus wide enough that the index has to be an
// index: many symbols, repeated names across files, several kinds, and paths
// whose segments are terms of their own.
func generatedTermRows(symbols int) LadybugSnapshotRows {
	rows := LadybugSnapshotRows{
		Repositories: []RepositoryRow{{Key: "repo", Name: "repo", Commit: "commit"}},
		Packages:     []PackageRow{{Key: "pkg", RepositoryKey: "repo", Name: "pkg", ModulePath: "example.com/pkg"}},
	}
	nouns := []string{"Snapshot", "Symbol", "Reference", "Traversal", "Publisher", "Resolver", "Index", "Witness"}
	verbs := []string{"Build", "Read", "Write", "Resolve", "Publish", "Traverse", "Fold", "Register"}
	kinds := []string{"func", "struct_method", "class", "interface"}
	dirs := []string{"internal/storage", "internal/mcp/tools", "internal/hotsnapshot", "cmd/kivgraph"}
	for index := 0; index < symbols; index++ {
		directory := dirs[index%len(dirs)]
		fileKey := fmt.Sprintf("file-%d", index%len(dirs))
		if index < len(dirs) {
			rows.Files = append(rows.Files, FileRow{
				Key: fileKey, RepositoryKey: "repo", PackageKey: "pkg",
				Path: fmt.Sprintf("%s/unit.go", directory),
			})
		}
		name := verbs[index%len(verbs)] + nouns[(index/len(verbs))%len(nouns)]
		rows.Symbols = append(rows.Symbols, SymbolRow{
			StableKey: StableKey(fmt.Sprintf("sym-%05d", index)), CanonicalIdentity: fmt.Sprintf("identity-%05d", index),
			FileKey: fileKey, Name: name, QualifiedName: "pkg." + name,
			Kind: kinds[index%len(kinds)], StartLine: uint32(index * 4), EndLine: uint32(index*4 + 2),
		})
	}
	return rows
}

// TestTermIndexAnswersExactlyWhatALinearScanWould is the correctness test that
// matters: for every term the index holds, the run it returns must equal the set
// a brute-force pass over the records produces. An index is only ever a faster
// way to say the same thing, and this is the only test that can tell whether it
// still says it.
func TestTermIndexAnswersExactlyWhatALinearScanWould(t *testing.T) {
	snapshot, err := BuildGraphSnapshot(generatedTermRows(512), 1, time.Unix(1, 0).UTC(), 1)
	if err != nil {
		t.Fatal(err)
	}
	table := snapshot.Strings()

	// Brute force: fold every symbol's own text, exactly as the index claims to.
	expected := map[retrieval.TermKey]map[SymbolID]bool{}
	for index, symbol := range snapshot.symbols {
		texts := []InternedString{symbol.Name, symbol.QualifiedName, symbol.Kind}
		if file, found := snapshot.File(symbol.File); found {
			texts = append(texts, file.Path)
		}
		for _, interned := range texts {
			text, ok := table.String(interned)
			if !ok {
				continue
			}
			for _, span := range retrieval.AppendSpans(nil, text) {
				key := retrieval.Fold(text[span.Start:span.End])
				if key == retrieval.TermKeyNone {
					continue
				}
				if expected[key] == nil {
					expected[key] = map[SymbolID]bool{}
				}
				expected[key][SymbolID(uint32(index))] = true
			}
		}
	}

	if got := snapshot.TermCount(); got != len(expected) {
		t.Fatalf("index holds %d terms, a linear scan finds %d", got, len(expected))
	}
	for key, want := range expected {
		found, frequency := snapshot.SymbolsByTerm(key)
		if frequency != len(want) {
			t.Fatalf("term %d: frequency %d, want %d", key, frequency, len(want))
		}
		if len(found) != len(want) {
			t.Fatalf("term %d: %d symbols, want %d", key, len(found), len(want))
		}
		for _, id := range found {
			if !want[id] {
				t.Fatalf("term %d returned symbol %d, which carries no such token", key, id)
			}
		}
	}
}

// TestTermIndexKeepsItsArraysOrdered checks the shape a reader indexes blindly:
// keys ascending and unique, offsets monotonic and bounding the values, runs
// ascending and inside the symbol table. A violation of any of these is a wrong
// answer no query could detect.
func TestTermIndexKeepsItsArraysOrdered(t *testing.T) {
	snapshot, err := BuildGraphSnapshot(generatedTermRows(256), 1, time.Unix(1, 0).UTC(), 1)
	if err != nil {
		t.Fatal(err)
	}
	index := snapshot.symbolsByTerm
	if !index.validShape() {
		t.Fatal("index reports an invalid shape")
	}
	for position := 1; position < len(index.keys); position++ {
		if index.keys[position-1] >= index.keys[position] {
			t.Fatalf("keys %d and %d are not ascending and unique", position-1, position)
		}
	}
	for position := range index.keys {
		run := index.values[index.offsets[position]:index.offsets[position+1]]
		if len(run) == 0 {
			t.Fatalf("term %d has an empty run", index.keys[position])
		}
		for at := 1; at < len(run); at++ {
			if run[at-1] >= run[at] {
				t.Fatalf("term %d: run is not ascending: %v", index.keys[position], run)
			}
		}
		for _, id := range run {
			if uint64(id) >= uint64(len(snapshot.symbols)) {
				t.Fatalf("term %d points at symbol %d, past the table", index.keys[position], id)
			}
		}
	}
}

// TestTermIndexReachesASymbolByEachOfItsTexts pins the four sources the fold
// reads. Each one alone must be enough to find a symbol, because a caller asking
// in plain language does not know which of them holds the word they chose.
func TestTermIndexReachesASymbolByEachOfItsTexts(t *testing.T) {
	rows := LadybugSnapshotRows{
		Repositories: []RepositoryRow{{Key: "repo", Name: "repo", Commit: "commit"}},
		Packages:     []PackageRow{{Key: "pkg", RepositoryKey: "repo", Name: "pkg", ModulePath: "example.com/pkg"}},
		Files:        []FileRow{{Key: "file", RepositoryKey: "repo", PackageKey: "pkg", Path: "internal/publishing/store.go"}},
		Symbols: []SymbolRow{{
			StableKey: StableKey("sym"), CanonicalIdentity: "identity", FileKey: "file",
			Name: "reconcileGeneration", QualifiedName: "generations.reconcileGeneration",
			Kind: "func", StartLine: 10, EndLine: 20,
		}},
	}
	snapshot, err := BuildGraphSnapshot(rows, 1, time.Unix(1, 0).UTC(), 1)
	if err != nil {
		t.Fatal(err)
	}
	for _, term := range []string{
		"reconcile",   // from the name
		"generations", // from the qualified name only
		"func",        // from the kind
		"publishing",  // from the path only
		"store",       // from the path only
	} {
		found, frequency := snapshot.SymbolsByTerm(retrieval.Fold(term))
		if len(found) != 1 || frequency != 1 {
			t.Errorf("%q reached %v (df %d), want the one symbol", term, found, frequency)
		}
	}
	// And a word none of the four texts contains reaches nothing.
	if found, _ := snapshot.SymbolsByTerm(retrieval.Fold("unrelated")); len(found) != 0 {
		t.Errorf("`unrelated` reached %v, want nothing", found)
	}
}

// TestTermIndexSurvivesTheFileTableDisagreeing covers the defensive read in the
// builder. A symbol whose file id is past the table is a snapshot that will be
// rejected moments later, and the index must not panic on the way there:
// crashing during construction would turn a rejected file into a dead process.
func TestTermIndexSurvivesTheFileTableDisagreeing(t *testing.T) {
	symbols := []SymbolRecord{{Name: 0, QualifiedName: 0, Kind: 0, File: 99}}
	index := newTermIndex(symbols, nil, StringTable{})
	if !index.validShape() {
		t.Fatal("index over a disagreeing table reports an invalid shape")
	}
}

// TestTermIndexIsDerivedOnRead keeps the promise that makes this index free of
// the file format: it is rebuilt from the tables when a snapshot is read, so a
// generation published before it existed answers exactly like one built now.
func TestTermIndexIsDerivedOnRead(t *testing.T) {
	built, err := BuildGraphSnapshot(generatedTermRows(128), 7, time.Unix(1, 0).UTC(), 1)
	if err != nil {
		t.Fatal(err)
	}
	var payload bytes.Buffer
	if _, err := WriteSnapshot(&payload, built, [sha256.Size]byte{}); err != nil {
		t.Fatal(err)
	}
	read, err := ReadSnapshot(payload.Bytes(), [sha256.Size]byte{})
	if err != nil {
		t.Fatal(err)
	}
	if read.TermCount() != built.TermCount() {
		t.Fatalf("read back %d terms, built %d", read.TermCount(), built.TermCount())
	}
	for position, key := range built.symbolsByTerm.keys {
		wantRun, wantFrequency := built.SymbolsByTerm(key)
		gotRun, gotFrequency := read.SymbolsByTerm(key)
		if gotFrequency != wantFrequency || !reflect.DeepEqual(gotRun, wantRun) {
			t.Fatalf("term %d (position %d): read %v (df %d), built %v (df %d)",
				key, position, gotRun, gotFrequency, wantRun, wantFrequency)
		}
	}
}

// BenchmarkNewTermIndex is the cost this index adds to reading a snapshot,
// which is the number that decides whether it ever needs to be serialised into
// the file instead of derived from it.
func BenchmarkNewTermIndex(b *testing.B) {
	snapshot, err := BuildGraphSnapshot(generatedTermRows(20_000), 1, time.Unix(1, 0).UTC(), 1)
	if err != nil {
		b.Fatal(err)
	}
	symbols, files, table := snapshot.symbols, snapshot.files, snapshot.Strings()
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		if index := newTermIndex(symbols, files, table); len(index.keys) == 0 {
			b.Fatal("empty index")
		}
	}
}

// TestTermIndexRejectsAMalformedShape exercises the guard a reader leans on.
// Nothing in this package can produce these shapes today, which is exactly why
// the check has to be tested directly: it exists for the reader that will one
// day take the arrays from a file instead of from the constructor, and an
// untested guard is a guard that silently stops guarding.
func TestTermIndexRejectsAMalformedShape(t *testing.T) {
	for name, index := range map[string]termIndex{
		"offsets too short": {
			keys:    []retrieval.TermKey{1, 2},
			offsets: []uint32{0, 1},
			values:  []SymbolID{0},
		},
		"offsets do not start at zero": {
			keys:    []retrieval.TermKey{1},
			offsets: []uint32{1, 2},
			values:  []SymbolID{0, 1},
		},
		"offsets go backwards": {
			keys:    []retrieval.TermKey{1, 2},
			offsets: []uint32{0, 2, 1},
			values:  []SymbolID{0, 1},
		},
		"offsets do not reach the values": {
			keys:    []retrieval.TermKey{1},
			offsets: []uint32{0, 1},
			values:  []SymbolID{0, 1},
		},
	} {
		if index.validShape() {
			t.Errorf("%s: validShape() = true, want the shape refused", name)
		}
	}

	// And the shape the constructor produces is accepted, so the check above is
	// rejecting malformation rather than everything.
	valid := termIndex{keys: []retrieval.TermKey{1, 2}, offsets: []uint32{0, 1, 3}, values: []SymbolID{0, 1, 2}}
	if !valid.validShape() {
		t.Error("a well-formed index was refused")
	}
}

// TestIncomingCountCountsWhatPointsAtASymbol pins the one number the ranking
// takes from the graph rather than from the text. It is the fan-in the resolver
// established, so a symbol nobody calls has to read zero and not one, and an id
// outside the table has to read zero rather than reach past its end.
func TestIncomingCountCountsWhatPointsAtASymbol(t *testing.T) {
	rows := builderRows()
	snapshot, err := BuildGraphSnapshot(rows, 1, time.Unix(1, 0).UTC(), 1)
	if err != nil {
		t.Fatal(err)
	}
	// The fixture's edges are s-a -> s-b -> s-c, so the root of that chain is
	// the symbol nobody points at.
	counts := map[StableKey]int{}
	for _, key := range []StableKey{"s-a", "s-b", "s-c"} {
		id, ok := snapshot.SymbolByStableKey(key)
		if !ok {
			t.Fatalf("the fixture has no symbol %q", key)
		}
		counts[key] = snapshot.IncomingCount(id)
	}
	if counts["s-a"] != 0 {
		t.Errorf("incoming(s-a) = %d, want zero: it is the root of the fixture chain", counts["s-a"])
	}
	if counts["s-b"] != 1 || counts["s-c"] != 1 {
		t.Errorf("incoming(s-b) = %d and incoming(s-c) = %d, want the one edge each carries",
			counts["s-b"], counts["s-c"])
	}
	if count := snapshot.IncomingCount(SymbolID(len(snapshot.symbols))); count != 0 {
		t.Errorf("incoming(one past the table) = %d, want zero", count)
	}
	if count := snapshot.IncomingCount(SymbolID(9_999)); count != 0 {
		t.Errorf("incoming(far past the table) = %d, want zero", count)
	}
}
