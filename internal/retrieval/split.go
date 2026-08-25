// Package retrieval turns identifiers and a plain-language question into the
// terms a lexical index stores and looks up.
//
// It holds no state and knows nothing about the snapshot: it maps text to
// tokens, and the caller decides what to do with them. That boundary is what
// keeps the index format free of language rules and the language rules free of
// the index format.
package retrieval

import "unicode"

// Span is one token's half-open byte range inside the string it came from.
//
// Ranges rather than strings, because splitting runs over every symbol of the
// graph on every index pass and the caller almost always wants to intern the
// token rather than keep it: a []string would allocate the slice and one string
// per part, and then be thrown away a line later.
type Span struct {
	Start uint32
	End   uint32
}

// splitting states. A token boundary is a change of character class, plus the
// special case of a run of capitals handing its last letter to the word that
// follows it -- which is what makes HTTPServer two words and not one.
const (
	stateNone = iota
	stateLower
	stateFirstUpper
	stateUpper
	stateSymbol
)

// AppendSpans appends the token ranges of one identifier to dst and returns it.
//
// The algorithm is Daniel G. Taylor's `casing.Split`, adapted to append ranges
// instead of building a []string; see the attribution below. It handles the
// four conventions a mixed-language graph actually contains -- CamelCase,
// lowerCamel, snake_case, kebab-case -- plus digits, symbols and non-ASCII
// letters, and it drops separators instead of emitting them as tokens.
//
// Behaviour is pinned against the upstream test table in split_test.go, so an
// adaptation that drifted from the original would fail rather than quietly
// answer something else.
//
// Attribution: derived from github.com/danielgtaylor/casing v1.0.0
// (casing.go, func Split), Copyright 2021 Daniel G. Taylor, MIT licence.
// Copied rather than imported: the module is frozen at v1.0.0 and declares
// go 1.15, so it offers no upgrade stream to follow while pulling six modules
// into go.sum for one self-contained function, none of which reach the binary.
// The permission notice travels in THIRD_PARTY_NOTICES.md.
func AppendSpans(dst []Span, value string) []Span {
	start := 0
	state := stateNone
	emit := func(from, to int) {
		if to > from {
			dst = append(dst, Span{Start: uint32(from), End: uint32(to)})
		}
	}

	for index, symbol := range value {
		// Whatever the state, these end a token: it is how snake_case and
		// kebab-case are read, and the separator itself is not a token.
		if unicode.IsSpace(symbol) || unicode.IsPunct(symbol) {
			emit(start, index)
			start = index + 1
			state = stateNone
			continue
		}

		switch {
		case state != stateFirstUpper && state != stateUpper && unicode.IsUpper(symbol):
			// An initial capital may open a word, as in CamelCase.
			if start != index {
				emit(start, index)
				start = index
			}
			state = stateFirstUpper
		case state == stateFirstUpper && unicode.IsUpper(symbol):
			// A run of capitals to be kept together, as in HTTP.
			state = stateUpper
		case state != stateSymbol && !unicode.IsLetter(symbol):
			// Anything to a non-letter: digits and symbols are their own token.
			if start != index {
				emit(start, index)
				start = index
			}
			state = stateSymbol
		case state != stateLower && unicode.IsLower(symbol):
			if state == stateUpper {
				// A run of capitals meeting a lowercase letter: the last capital
				// belongs to the word that starts here, not to the run. This is
				// the whole reason HTTPServer is HTTP and Server.
				if index > 0 && start != index-1 {
					emit(start, index-1)
					start = index - 1
				}
			} else if state != stateFirstUpper {
				// The first capital is part of this same word, so it is the only
				// state that does not close a token here.
				if index > 0 && start != index {
					emit(start, index)
					start = index
				}
			}
			state = stateLower
		}
	}
	emit(start, len(value))
	return dst
}
