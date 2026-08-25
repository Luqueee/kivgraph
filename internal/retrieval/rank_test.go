package retrieval

import (
	"math"
	"sort"
	"testing"
)

// The negative cases come first, per AGENTS.md: a ranker is asked to order
// candidates a caller assembled, so what it does with an input that does not
// describe a candidate decides whether a bad page is a wrong order or a wrong
// answer.

// TestScoreRefusesACandidateThatHitNothing is the first rejection. A symbol that
// carries no term of the question is not a weak answer, it is not an answer, and
// a positive score would put it on a page as if a term had matched.
func TestScoreRefusesACandidateThatHitNothing(t *testing.T) {
	for name, signals := range map[string]Signals{
		"no hits":       {Hits: 0, Symbols: 100, Kind: "func", Exported: true},
		"negative hits": {Hits: -3, Symbols: 100, Kind: "func"},
		"no hits but every other signal strong": {
			Hits: 0, Frequencies: []int{1}, Symbols: 1000,
			Kind: "func", Exported: true, Path: "internal/mcp/tools/find.go", Callers: 40,
		},
	} {
		if score := Score(signals); score != 0 {
			t.Errorf("%s: Score = %v, want zero", name, score)
		}
	}
}

// TestScoreSurvivesFrequenciesThatCannotBeTrue covers the arithmetic that would
// otherwise produce a negative or infinite score and silently invert an order.
// None of these can be built by the index today, which is exactly why the guard
// needs a test: the frequencies will one day arrive from a file.
func TestScoreSurvivesFrequenciesThatCannotBeTrue(t *testing.T) {
	for name, signals := range map[string]Signals{
		"frequency past the corpus": {Hits: 1, Frequencies: []int{5_000}, Symbols: 100, Kind: "func"},
		"negative frequency":        {Hits: 1, Frequencies: []int{-7}, Symbols: 100, Kind: "func"},
		"zero frequency":            {Hits: 1, Frequencies: []int{0}, Symbols: 100, Kind: "func"},
		"corpus of zero":            {Hits: 2, Frequencies: []int{1, 2}, Symbols: 0, Kind: "func"},
		"negative corpus":           {Hits: 2, Frequencies: []int{1, 2}, Symbols: -10, Kind: "func"},
		"no frequencies at all":     {Hits: 3, Symbols: 100, Kind: "func"},
		"negative callers":          {Hits: 1, Frequencies: []int{1}, Symbols: 100, Kind: "func", Callers: -4},
	} {
		score := Score(signals)
		if math.IsNaN(score) || math.IsInf(score, 0) || score < 0 {
			t.Errorf("%s: Score = %v, want a finite score at or above zero", name, score)
		}
	}
}

// TestScoreIgnoresATermNobodyCarries is the boundary between the two zeros. A
// frequency of zero means the caller counted a term it did not find, and it must
// contribute nothing -- not a full hit, which is what a naive `1 - share` gives.
func TestScoreIgnoresATermNobodyCarries(t *testing.T) {
	found := Score(Signals{Hits: 2, Frequencies: []int{1, 1}, Symbols: 1000, Kind: "func"})
	phantom := Score(Signals{Hits: 2, Frequencies: []int{1, 0}, Symbols: 1000, Kind: "func"})
	single := Score(Signals{Hits: 1, Frequencies: []int{1}, Symbols: 1000, Kind: "func"})
	if phantom >= found {
		t.Errorf("a phantom term scored %v against %v for two real ones", phantom, found)
	}
	if math.Abs(phantom-single) > 1e-9 {
		t.Errorf("a phantom term scored %v, want the score of the one real term, %v", phantom, single)
	}
}

// TestScoreDiscountsATermThatMatchesTheCorpus is the measured claim that
// replaced a stopword list. On the published graph the widest term is the fold
// of `internal`, matching four symbols in five; a question that hits only that
// must rank below one that hits a term matching a handful.
func TestScoreDiscountsATermThatMatchesTheCorpus(t *testing.T) {
	const symbols = 22_299
	everywhere := Score(Signals{Hits: 1, Frequencies: []int{17_896}, Symbols: symbols, Kind: "func"})
	rare := Score(Signals{Hits: 1, Frequencies: []int{82}, Symbols: symbols, Kind: "func"})
	if everywhere >= rare {
		t.Fatalf("the widest term scored %v against a rare one at %v", everywhere, rare)
	}
	if everywhere/rare > 0.25 {
		t.Fatalf("the widest term kept %.0f%% of a rare term's weight, want it nearly discounted away",
			everywhere/rare*100)
	}
	// A term the whole corpus carries is worth nothing at all, which is the
	// floor the linear discount exists to provide.
	if score := Score(Signals{Hits: 1, Frequencies: []int{symbols}, Symbols: symbols, Kind: "func"}); score != 0 {
		t.Fatalf("a term every symbol carries scored %v, want zero", score)
	}
}

// TestScoreSinksAFixture is the case a lexical answer gets wrong most often: a
// fixture is written to look exactly like production code, so it matches a
// question just as well and is never the answer.
func TestScoreSinksAFixture(t *testing.T) {
	question := Signals{Hits: 2, Frequencies: []int{40, 60}, Symbols: 10_000, Kind: "func", Exported: true}

	production := question
	production.Path = "internal/storage/generation/publish.go"
	fixture := question
	fixture.Path = "internal/storage/generation/testdata/publish.go"
	unitTest := question
	unitTest.Path = "internal/storage/generation/publish_test.go"
	benchmark := question
	benchmark.Path = "benchmarks/snapshot-heap/main.go"
	vendored := question
	vendored.Path = "ts-worker/node_modules/typescript/lib/publish.js"

	for name, weaker := range map[string]Signals{
		"fixture": fixture, "test file": unitTest, "benchmark": benchmark, "vendored": vendored,
	} {
		if Score(weaker) >= Score(production) {
			t.Errorf("%s scored %v, at or above production code at %v", name, Score(weaker), Score(production))
		}
	}
	// And a fixture is the weakest of them: a test file is still the project's
	// own code and can be what the question is about.
	if Score(fixture) >= Score(unitTest) {
		t.Errorf("a fixture scored %v, at or above a test file at %v", Score(fixture), Score(unitTest))
	}
}

// TestScoreOrdersKindsByWhatAQuestionMeans pins the one ordering a caller can
// predict: an import row names a symbol without being it, and must never
// outrank the declaration.
func TestScoreOrdersKindsByWhatAQuestionMeans(t *testing.T) {
	base := Signals{Hits: 2, Frequencies: []int{30, 40}, Symbols: 5_000, Path: "internal/mcp/tools/find.go"}
	scoreOf := func(kind string) float64 {
		signals := base
		signals.Kind = kind
		return Score(signals)
	}
	ordered := []string{"func", "struct", "module", "unknown_kind", "field", "enum_variant", "import"}
	for position := 1; position < len(ordered); position++ {
		if scoreOf(ordered[position-1]) <= scoreOf(ordered[position]) {
			t.Errorf("%q (%v) does not outrank %q (%v)",
				ordered[position-1], scoreOf(ordered[position-1]),
				ordered[position], scoreOf(ordered[position]))
		}
	}
	// An unfamiliar kind is neither promoted nor buried: the graph spells kinds
	// per language, and a kind this table has not met is not thereby wrong.
	if scoreOf("gdscript_signal") != scoreOf("unknown_kind") {
		t.Error("two unmet kinds scored differently, want the neutral weight for both")
	}
}

// TestScoreLetsTheGraphSpeak covers the signal no text search can have, and its
// ceiling. A helper with forty callers is worth surfacing; it is not worth
// burying the specific symbol a question named.
func TestScoreLetsTheGraphSpeak(t *testing.T) {
	base := Signals{Hits: 1, Frequencies: []int{20}, Symbols: 5_000, Kind: "func", Path: "internal/x.go"}
	previous := 0.0
	for _, callers := range []int{0, 1, 2, 4, 8, 15} {
		signals := base
		signals.Callers = callers
		score := Score(signals)
		if score < previous {
			t.Errorf("%d callers scored %v, below the %v of fewer", callers, score, previous)
		}
		previous = score
	}
	saturated := base
	saturated.Callers = 15
	beyond := base
	beyond.Callers = 4_000
	if Score(beyond) != Score(saturated) {
		t.Errorf("four thousand callers scored %v against %v at fifteen, want saturation",
			Score(beyond), Score(saturated))
	}
	uncalled := base
	if Score(beyond)/Score(uncalled) > 2.0001 {
		t.Errorf("callers multiplied the score by %v, want at most double", Score(beyond)/Score(uncalled))
	}
}

// TestScoreIsScaleFreeAndOrdersOnly is the contract that keeps the number out of
// the response: it exists to order candidates against each other, so scaling
// every weight leaves the order untouched and the magnitude means nothing a
// caller could act on.
func TestScoreIsScaleFreeAndOrdersOnly(t *testing.T) {
	candidates := []Signals{
		{Hits: 1, Frequencies: []int{5_000}, Symbols: 10_000, Kind: "import", Path: "internal/a.go"},
		{Hits: 2, Frequencies: []int{40, 60}, Symbols: 10_000, Kind: "func", Exported: true, Path: "internal/b.go", Callers: 12},
		{Hits: 1, Frequencies: []int{80}, Symbols: 10_000, Kind: "struct", Path: "internal/c.go"},
		{Hits: 3, Frequencies: []int{10, 20, 30}, Symbols: 10_000, Kind: "func", Path: "testdata/d.go"},
	}
	order := func(scale int) []int {
		positions := []int{0, 1, 2, 3}
		scores := make([]float64, len(candidates))
		for index, signals := range candidates {
			scaled := signals
			scaled.Symbols *= scale
			scaled.Frequencies = make([]int, len(signals.Frequencies))
			for at, frequency := range signals.Frequencies {
				scaled.Frequencies[at] = frequency * scale
			}
			scores[index] = Score(scaled)
		}
		sort.SliceStable(positions, func(left, right int) bool {
			return scores[positions[left]] > scores[positions[right]]
		})
		return positions
	}
	small, large := order(1), order(7)
	for position := range small {
		if small[position] != large[position] {
			t.Fatalf("order changed with the size of the corpus: %v then %v", small, large)
		}
	}
	// The strongest is the exported, called function hitting two rare terms.
	// The weakest is the import row: it names a symbol without being it, and the
	// one term it hits matches half the corpus.
	//
	// The fixture is not last, and that is the ranker working rather than
	// failing. Hitting three rare terms outweighs sitting in testdata, which is
	// the right trade: the path discount says "prefer production code, all else
	// equal", not "a fixture is never an answer". What the path guarantees is
	// the pairwise contract in TestScoreSinksAFixture -- the same signals in
	// testdata always rank below the same signals in production -- and a total
	// order over unlike candidates is not something a caller can predict.
	if small[0] != 1 || small[len(small)-1] != 0 {
		t.Fatalf("order = %v, want the exported called function first and the import row last", small)
	}
}
