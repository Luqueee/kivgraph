package retrieval

import (
	"reflect"
	"strings"
	"testing"
	"unicode/utf8"
)

// TestFoldBoundaries covers where the fold starts and stops mattering: what is
// too short to be a term, where the prefix is cut, and that the cut never lands
// inside a character.
func TestFoldBoundaries(t *testing.T) {
	for _, test := range []struct {
		name  string
		token string
		want  TermKey
	}{
		{"empty", "", TermKeyNone},
		{"one rune", "a", TermKeyNone},
		{"one wide rune", "ü", TermKeyNone},
	} {
		if got := Fold(test.token); got != test.want {
			t.Errorf("%s: Fold(%q) = %d, want %d", test.name, test.token, got, test.want)
		}
	}

	// Two runes is the shortest thing that earns a key.
	if Fold("go") == TermKeyNone {
		t.Error(`Fold("go") folded to nothing, want a key`)
	}

	// Everything past the prefix is cut, so a token and its longer relatives
	// share one key. This is the fold, not a coincidence.
	short := Fold("trave")
	for _, longer := range []string{"traver", "travers", "traverse", "traversal", "traversing"} {
		if Fold(longer) != short {
			t.Errorf("Fold(%q) != Fold(%q), want the prefix to decide", longer, "trave")
		}
	}

	// Anything shorter than the prefix is its whole self, so it cannot collide
	// with a longer token that merely starts the same way.
	if Fold("run") == Fold("running") {
		t.Error(`Fold("run") == Fold("running"), want a short token to keep its length`)
	}

	// Case never survives the fold: an exported and an unexported name are the
	// same term.
	if Fold("Snapshot") != Fold("snapshot") || Fold("SNAPSHOT") != Fold("snapshot") {
		t.Error("Fold is case sensitive, want the case folded away")
	}
}

// TestFoldNeverSplitsACharacter is the invariant that a byte-packed key could
// silently break: the key holds bytes, the prefix is counted in runes, and a
// multi-byte rune that does not fit must be dropped whole rather than halved.
func TestFoldNeverSplitsACharacter(t *testing.T) {
	// Five two-byte runes are ten bytes and cannot all fit in the key, so the
	// fold has to stop early -- and the bytes it did keep must still decode.
	wide := "üüüüü"
	if utf8.RuneCountInString(wide) != 5 || len(wide) != 10 {
		t.Fatalf("fixture %q is %d runes and %d bytes, expected five and ten", wide, utf8.RuneCountInString(wide), len(wide))
	}
	key := Fold(wide)
	if key == TermKeyNone {
		t.Fatalf("Fold(%q) folded to nothing", wide)
	}

	// Rebuild the bytes the key holds and require them to be valid UTF-8: a
	// halved rune would leave an invalid tail here.
	var packed [termKeyBytes]byte
	for index := termKeyBytes - 1; index >= 0; index-- {
		packed[index] = byte(key)
		key >>= 8
	}
	trimmed := strings.TrimRight(string(packed[:]), "\x00")
	if !utf8.ValidString(trimmed) {
		t.Fatalf("key bytes %q are not valid UTF-8, want the prefix cut at a rune boundary", trimmed)
	}

	// A four-byte rune exceeds the key on its own terms too, and must not
	// produce a broken key either.
	if key := Fold("🎉🎉"); key != TermKeyNone {
		var wide [termKeyBytes]byte
		for index := termKeyBytes - 1; index >= 0; index-- {
			wide[index] = byte(key)
			key >>= 8
		}
		if !utf8.ValidString(strings.TrimRight(string(wide[:]), "\x00")) {
			t.Error("an emoji token produced a key with a split rune")
		}
	}
}

// TestFoldCollapsesTheMorphologyCodeUses is the measured claim this whole layer
// rests on. These are the pairs a plain-language question needs and that an
// English stemmer gets wrong: Porter2 turns `register` into `regist` but
// `registration` into `registr`, and `parse` into `pars` but `parser` into
// `parser`, so it separates exactly the words a caller means together.
func TestFoldCollapsesTheMorphologyCodeUses(t *testing.T) {
	for _, group := range [][]string{
		{"register", "registered", "registers", "registration", "registry"},
		{"parse", "parser", "parsed"},
		{"resolve", "resolver", "resolved", "resolution"},
		{"traverse", "traversal", "traverses", "traversing"},
		{"publish", "published", "publishing", "publication"},
		{"validate", "validation", "validated", "validator"},
		{"truncate", "truncated", "truncation"},
		{"normalize", "normalization", "normalized"},
		{"serialize", "serialization", "serialized"},
		{"reference", "references", "referenced"},
		{"depend", "dependency", "dependencies", "dependent"},
	} {
		want := Fold(group[0])
		if want == TermKeyNone {
			t.Fatalf("Fold(%q) folded to nothing", group[0])
		}
		for _, word := range group[1:] {
			if got := Fold(word); got != want {
				t.Errorf("Fold(%q) = %d, want the key of %q (%d)", word, got, group[0], want)
			}
		}
	}
}

// TestFoldKeepsUnrelatedWordsApart is the other half of the claim, and the one
// that fails first if the prefix is ever shortened: a fold that collapses
// everything recovers every pair and answers every question with the whole
// corpus. These are the near misses a four-rune prefix would merge.
func TestFoldKeepsUnrelatedWordsApart(t *testing.T) {
	for _, pair := range [][2]string{
		{"index", "interface"},    // both `inte` at four runes
		{"register", "reject"},    // both `rej`/`reg` neighbours
		{"traverse", "transform"}, // both `tra` at three
		{"snapshot", "snake"},
		{"symbol", "symmetry"},
		{"package", "packed"},
		{"resolve", "resource"},
	} {
		if Fold(pair[0]) == Fold(pair[1]) {
			t.Errorf("Fold(%q) == Fold(%q), want unrelated words kept apart", pair[0], pair[1])
		}
	}
}

// TestFoldOrdersLikeItsText keeps the packing honest. Bytes go into the key
// big-endian precisely so integer order equals byte order; if that inverted,
// the binary search that finds a term would still work by equality but the
// index would no longer be sorted the way its comments claim.
func TestFoldOrdersLikeItsText(t *testing.T) {
	ordered := []string{"alpha", "beta", "delta", "gamma", "omega"}
	for position := 1; position < len(ordered); position++ {
		if Fold(ordered[position-1]) >= Fold(ordered[position]) {
			t.Errorf("Fold(%q) >= Fold(%q), want key order to follow text order",
				ordered[position-1], ordered[position])
		}
	}
}

// TestAppendKeysSkipsWhatFoldsToNothing keeps the single characters of a split
// acronym out of the index: `uHTTP123` splits into `u`, `HTTP` and `123`, and
// `u` is a term that would match a large share of the corpus while saying
// nothing.
func TestAppendKeysSkipsWhatFoldsToNothing(t *testing.T) {
	const value = "uHTTP123"
	spans := AppendSpans(nil, value)
	if len(spans) != 3 {
		t.Fatalf("AppendSpans(%q) = %d spans, want three", value, len(spans))
	}
	keys := AppendKeys(nil, spans, value)
	if len(keys) != 2 {
		t.Fatalf("AppendKeys(%q) = %d keys, want the single character dropped", value, len(keys))
	}
	if keys[0] != Fold("http") || keys[1] != Fold("123") {
		t.Fatalf("keys = %v, want the folds of http and 123", keys)
	}
}

// TestAppendTextKeysReusesBothBuffers is why the signature returns the span
// slice: folding every symbol of the graph must not allocate per symbol, and a
// caller that threads both buffers through the loop gets that for free.
func TestAppendTextKeysReusesBothBuffers(t *testing.T) {
	keys := make([]TermKey, 0, 32)
	spans := make([]Span, 0, 32)
	for _, text := range []string{"TraverseFrom", "internal/hotsnapshot/traversal.go", "snake_case_name"} {
		keys, spans = AppendTextKeys(keys[:0], spans, text)
		if len(keys) == 0 {
			t.Fatalf("AppendTextKeys(%q) produced no keys", text)
		}
	}
	if cap(keys) != 32 || cap(spans) != 32 {
		t.Fatalf("capacities grew to %d keys and %d spans, want the caller's buffers kept", cap(keys), cap(spans))
	}
}

// TestQueryTermsReadsAQuestion covers the shape of real input: prose with
// punctuation, an identifier pasted mid-sentence, and repeats that must not
// become two lookups of the same term.
func TestQueryTermsReadsAQuestion(t *testing.T) {
	terms := QueryTerms("Where is the snapshot published? See publishSnapshot().", nil)
	if len(terms) == 0 {
		t.Fatal("QueryTerms produced nothing for a real question")
	}
	for position := 1; position < len(terms); position++ {
		for earlier := 0; earlier < position; earlier++ {
			if terms[position] == terms[earlier] {
				t.Fatalf("term %d repeats term %d; `published` and `publishSnapshot` fold alike and must be asked once", position, earlier)
			}
		}
	}
	// The identifier pasted into the sentence is split like an identifier, so
	// its parts are terms in their own right.
	if !containsKey(terms, Fold("snapshot")) {
		t.Error("QueryTerms lost `snapshot`, which the question names twice")
	}

	// Order is the order the question was written, because a caller that shows
	// per-term hits shows them in the order the reader typed.
	first := QueryTerms("alpha beta", nil)
	if len(first) != 2 || first[0] != Fold("alpha") || first[1] != Fold("beta") {
		t.Fatalf("QueryTerms = %v, want the written order", first)
	}
}

// TestQueryTermsFoldsKeywordsAfterTheQuestion pins where the agent's synonyms
// land: they extend the question, they do not replace it, and a keyword that
// repeats a term of the question adds no second lookup.
func TestQueryTermsFoldsKeywordsAfterTheQuestion(t *testing.T) {
	terms := QueryTerms("publish", []string{"generation", "publish", "snapshot store"})
	want := []TermKey{Fold("publish"), Fold("generation"), Fold("snapshot"), Fold("store")}
	if !reflect.DeepEqual(terms, want) {
		t.Fatalf("QueryTerms = %v, want %v", terms, want)
	}
}

// TestQueryTermsKeepsCommonWords documents a deliberate absence. There is no
// stopword list, so `the` and `is` do produce terms; what makes them harmless
// is the document frequency the index reports for them, which is a decision the
// ranker makes with data rather than one this layer makes with a word list.
func TestQueryTermsKeepsCommonWords(t *testing.T) {
	terms := QueryTerms("the file is written", nil)
	if !containsKey(terms, Fold("the")) {
		t.Error("QueryTerms dropped `the`; filtering belongs to the ranker, by frequency")
	}
	if len(terms) != 4 {
		t.Fatalf("QueryTerms = %v, want one term per word", terms)
	}
}

// TestQueryTermsSurvivesInputWithNoWords keeps an empty answer an empty answer
// rather than a panic or a phantom term.
func TestQueryTermsSurvivesInputWithNoWords(t *testing.T) {
	for _, input := range []string{"", "   ", "?!,.", "a b c", "-- // ***"} {
		if terms := QueryTerms(input, nil); len(terms) != 0 {
			t.Errorf("QueryTerms(%q) = %v, want nothing", input, terms)
		}
	}
}

// TestFoldHasKnownLimits records what the prefix does not do, because a fold
// that recovered everything would recover the whole corpus and a reader of this
// package should not have to discover the trade at a call site.
//
// Two shapes escape it, and both were found by these tests rather than argued:
//
//   - A gerund whose stem diverges before the prefix ends. `parse` and `parsing`
//     part at the fifth rune, so they are different terms. A shorter prefix
//     recovers them and merges `resolve` with `resource` and `package` with
//     `packed` instead, which measured noisier over this corpus.
//   - Unrelated words that genuinely share their first runes. `publish` and
//     `public` are one term, as are any two words agreeing that far.
//
// Neither is corrected here. What absorbs them is the ranker: a term matching a
// large share of the corpus is discounted by its own frequency, and a symbol
// matching one term of a question loses to one matching several.
func TestFoldHasKnownLimits(t *testing.T) {
	if Fold("parse") == Fold("parsing") {
		t.Error("Fold now collapses parse and parsing; the prefix changed, so re-measure the noise before keeping it")
	}
	if Fold("publish") != Fold("public") {
		t.Error("Fold now separates publish and public; the prefix changed, so re-measure the recall before keeping it")
	}
}

// TestFoldRejectsASingleCharacterHowever it is written. This is a regression
// guard: the first version of the guard counted bytes, so a one-character token
// in two bytes -- every accented letter, every Cyrillic or Greek identifier --
// became a term that matched a large share of the corpus.
func TestFoldRejectsASingleCharacterHowever(t *testing.T) {
	for _, token := range []string{"a", "Z", "1", "ü", "Ü", "ñ", "é", "λ", "д", "字", "🎉"} {
		if key := Fold(token); key != TermKeyNone {
			t.Errorf("Fold(%q) = %d, want no key for a single character", token, key)
		}
	}
	// And two characters is a term whatever their width.
	for _, token := range []string{"go", "ün", "字典", "λλ"} {
		if Fold(token) == TermKeyNone {
			t.Errorf("Fold(%q) folded to nothing, want a key for two characters", token)
		}
	}
}
