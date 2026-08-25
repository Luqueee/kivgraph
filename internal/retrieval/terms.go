package retrieval

import (
	"strings"
	"unicode"
	"unicode/utf8"
)

// TermPrefixRunes is how many runes of a token make its term.
//
// It is the one constant of this layer that was chosen by measurement rather
// than by argument, and the measurement is in benchmarks: over the identifiers
// of this repository, folding tokens to their first runes recovers the
// morphological pairs a query needs -- `registration` finding `register`,
// `parser` finding `parse`, `resolution` finding `resolve` -- which neither
// exact matching nor an English stemmer does. Exact matching missed a quarter
// of them; Porter2 misses agent nouns and `-ation` nominalisations, which is
// most of how code names things.
//
// Four and five scored the same recall and differed only in noise, so the value
// here is the quieter of the two. Lowering it widens every answer; raising it
// past six starts losing the pairs that motivate the fold at all.
const TermPrefixRunes = 5

// TermKey is a token folded to the term it is indexed under: the first
// TermPrefixRunes runes, lowercased, packed into one integer.
//
// An integer rather than a string, because the indexes of this snapshot are
// built on the rule that nothing compares bytes on the hot path. A term of five
// runes is at most a handful of bytes, and packing them big-endian keeps
// integer order equal to byte order, so one uint64 comparison replaces a string
// comparison and the term dictionary needs no arena, no offset table and no
// copying to answer.
//
// TermKeyNone is the key of a token that folds to nothing, which is what an
// empty or unusable token produces. It is never stored.
type TermKey uint64

// TermKeyNone is the zero key: no token folds to it, so it doubles as "absent".
const TermKeyNone TermKey = 0

// termKeyBytes is how many bytes of the folded prefix fit the key. Five runes
// of ASCII are five bytes; five runes of anything wider are truncated at a rune
// boundary, so a key never holds half a character.
const termKeyBytes = 8

// Fold returns the term key of one token, or TermKeyNone when the token carries
// nothing to index.
//
// Single characters fold to nothing on purpose. A one-letter token is a loop
// variable or the tail of a split acronym, it appears in a large share of the
// corpus, and a term that matches a large share of the corpus tells a caller
// nothing it did not already know.
func Fold(token string) TermKey {
	var packed [termKeyBytes]byte
	written, runes := 0, 0
	for _, symbol := range token {
		if runes == TermPrefixRunes {
			break
		}
		lowered := unicode.ToLower(symbol)
		width := utf8.RuneLen(lowered)
		if width < 0 || written+width > termKeyBytes {
			break
		}
		utf8.EncodeRune(packed[written:], lowered)
		written += width
		runes++
	}
	// Counted in runes, not bytes: `ü` is one character in two bytes, and a byte
	// guard let it through as a term. A single character is a loop variable or
	// the tail of a split acronym, and it matches a large share of the corpus.
	if runes < 2 {
		return TermKeyNone
	}
	key := TermKey(0)
	for index := 0; index < termKeyBytes; index++ {
		key = key<<8 | TermKey(packed[index])
	}
	return key
}

// AppendKeys folds every token of value onto dst, skipping what folds to
// nothing. The buffer is the caller's so that indexing every symbol of the
// graph costs no allocation per symbol.
func AppendKeys(dst []TermKey, spans []Span, value string) []TermKey {
	for _, span := range spans {
		if key := Fold(value[span.Start:span.End]); key != TermKeyNone {
			dst = append(dst, key)
		}
	}
	return dst
}

// AppendTextKeys splits value and folds it in one pass, for the callers that
// hold no span buffer of their own.
func AppendTextKeys(dst []TermKey, spans []Span, value string) ([]TermKey, []Span) {
	spans = AppendSpans(spans[:0], value)
	return AppendKeys(dst, spans, value), spans
}

// QueryTerms folds a plain-language question into the keys to look up, in the
// order they were written and without repeats.
//
// It applies no stopword list. An English list is the wrong data twice over:
// it holds `himself` and `yourselves`, which no identifier contains, and it
// lacks `get`, `set`, `new`, `data` and `handler`, which are the terms that
// actually say nothing in a codebase -- and it would be an English answer for a
// graph that also holds Rust, Python, Dart and TypeScript. What a term is worth
// is decided where it can be measured instead: by how much of the corpus it
// matches, which the index already knows.
func QueryTerms(intent string, keywords []string) []TermKey {
	var (
		keys  []TermKey
		spans []Span
	)
	fold := func(text string) {
		for _, word := range strings.FieldsFunc(text, isSeparator) {
			spans = AppendSpans(spans[:0], word)
			for _, span := range spans {
				key := Fold(word[span.Start:span.End])
				if key == TermKeyNone {
					continue
				}
				if !containsKey(keys, key) {
					keys = append(keys, key)
				}
			}
		}
	}
	fold(intent)
	for _, keyword := range keywords {
		fold(keyword)
	}
	return keys
}

// isSeparator splits a question into words. A question is prose, so anything
// that is not a letter or a digit ends a word -- including the punctuation that
// an identifier pasted into a sentence brings with it.
func isSeparator(symbol rune) bool {
	return !unicode.IsLetter(symbol) && !unicode.IsDigit(symbol)
}

// containsKey reports whether keys already holds key. A linear scan because a
// question is a handful of words: a map to deduplicate ten terms would cost
// more to build than the scan it replaces.
func containsKey(keys []TermKey, key TermKey) bool {
	for _, existing := range keys {
		if existing == key {
			return true
		}
	}
	return false
}
