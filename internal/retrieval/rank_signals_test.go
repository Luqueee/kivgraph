package retrieval

import "testing"

// TestScoreDiscountsAWordTooShortToBeVocabulary is the negative that matters:
// frequency alone cannot tell a question's grammar from its subject, and the
// ranking used to be decided by the grammar.
//
// Measured on the published graph of this repository, `is` is carried by 178 of
// 22,299 symbols, so the frequency discount reads it as almost pure signal and
// put IsValidModuleKey first for three unrelated questions. The word is two
// runes; every word those questions were actually about is longer than the fold.
func TestScoreDiscountsAWordTooShortToBeVocabulary(t *testing.T) {
	// The particle is the rarer of the two, so nothing but its length can sink
	// it: 178 of 22,299 against 480 of 22,299.
	particle := Signals{Hits: 1, Frequencies: []int{178}, Lengths: []int{2}, Symbols: 22_299, Kind: "func"}
	vocabulary := Signals{Hits: 1, Frequencies: []int{480}, Lengths: []int{7}, Symbols: 22_299, Kind: "func"}
	if Score(particle) >= Score(vocabulary) {
		t.Fatalf("a two-rune particle scored %v against a seven-rune word at %v, and it is rarer: length is not being read",
			Score(particle), Score(vocabulary))
	}
	// And two of them still lose, which linear weighting would not deliver.
	pair := Signals{Hits: 2, Frequencies: []int{178, 178}, Lengths: []int{2, 2}, Symbols: 22_299, Kind: "func"}
	if Score(pair) >= Score(vocabulary) {
		t.Errorf("two particles scored %v against one real word at %v", Score(pair), Score(vocabulary))
	}
}

// TestScoreSaturatesAtTheFoldLength pins that a word longer than the term it
// folds to is worth exactly what the term is worth. `traversal` and `traverse`
// are one key, and neither can outrank the other by being longer.
func TestScoreSaturatesAtTheFoldLength(t *testing.T) {
	base := Signals{Hits: 1, Frequencies: []int{100}, Symbols: 10_000, Kind: "func"}
	atFold, beyond := base, base
	atFold.Lengths = []int{TermPrefixRunes}
	beyond.Lengths = []int{TermPrefixRunes + 40}
	if Score(atFold) != Score(beyond) {
		t.Fatalf("a word of %d runes scored %v and one of %d scored %v, want the same term to be worth the same",
			TermPrefixRunes, Score(atFold), TermPrefixRunes+40, Score(beyond))
	}
}

// TestScoreAsksForLengthsWithoutRequiringThem covers the caller that supplies
// none, and the row that supplies a nonsense one. A missing length is a caller
// asking not to weigh by length; a length of zero is a word that is not there.
func TestScoreAsksForLengthsWithoutRequiringThem(t *testing.T) {
	unweighted := Signals{Hits: 1, Frequencies: []int{100}, Symbols: 10_000, Kind: "func"}
	full := unweighted
	full.Lengths = []int{TermPrefixRunes}
	if Score(unweighted) != Score(full) {
		t.Errorf("omitting lengths scored %v, want the full weight of %v", Score(unweighted), Score(full))
	}
	empty := unweighted
	empty.Lengths = []int{0}
	if Score(empty) != 0 && Score(empty) >= Score(full) {
		t.Errorf("a word of no length scored %v, want it below %v", Score(empty), Score(full))
	}
	// Fewer lengths than frequencies is a caller that lost track of one term.
	// The terms it did describe still weigh, and the rest count whole.
	partial := Signals{Hits: 2, Frequencies: []int{100, 100}, Lengths: []int{2}, Symbols: 10_000, Kind: "func"}
	if Score(partial) <= 0 {
		t.Errorf("a partially described candidate scored %v, want it ordered anyway", Score(partial))
	}
}

// TestScoreKeepsCarryingATermAboveReachingOne is the bound on the graph signal.
// A symbol named after the question has to outrank one that merely calls
// something named after it, at every count, or the credit stops being evidence
// and starts being a second opinion.
func TestScoreKeepsCarryingATermAboveReachingOne(t *testing.T) {
	// The comparison is like for like: the same two terms, once both carried and
	// once one carried and one reached. Terms of unequal worth are a different
	// question, and reaching three rare ones may well beat carrying one that the
	// whole corpus carries.
	carries := Signals{Hits: 2, Frequencies: []int{50, 50}, Lengths: []int{9, 9}, Symbols: 10_000, Kind: "func"}
	reaches := Signals{Hits: 1, Frequencies: []int{50}, Lengths: []int{9}, Symbols: 10_000, Kind: "func", Neighbours: 99}
	if Score(reaches) >= Score(carries) {
		t.Fatalf("reaching a term through 99 edges scored %v against carrying it at %v",
			Score(reaches), Score(carries))
	}
	none := Signals{Hits: 1, Frequencies: []int{50}, Lengths: []int{9}, Symbols: 10_000, Kind: "func"}
	if Score(none) >= Score(reaches) {
		t.Errorf("neighbours earned no credit at all: %v against %v", Score(reaches), Score(none))
	}
	// And the credit grows with the count until it stops: two reached terms are
	// stronger evidence than one, and ninety-nine are not stronger than three.
	one := reaches
	one.Neighbours = 1
	two := reaches
	two.Neighbours = 2
	if !(Score(none) < Score(one) && Score(one) < Score(two) && Score(two) <= Score(reaches)) {
		t.Errorf("credit is not monotone then bounded: %v < %v < %v <= %v",
			Score(none), Score(one), Score(two), Score(reaches))
	}
	if Score(two) == Score(reaches) {
		t.Error("two reached terms already sit at the ceiling: the credit cannot grow at all")
	}

	// A candidate carrying nothing is not a candidate, however much it calls.
	if score := Score(Signals{Hits: 0, Neighbours: 99, Kind: "func"}); score != 0 {
		t.Errorf("a symbol carrying no term scored %v on its neighbours alone", score)
	}
}
