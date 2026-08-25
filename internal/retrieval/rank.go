package retrieval

import "strings"

// Signals are what a candidate symbol carries that a question did not ask for
// and that still decides whether it is the answer.
//
// Every field is already in the snapshot's symbol row or derivable from it. The
// two the graph cannot supply are absent on purpose: there is no docstring to
// weigh, and no recency, because a snapshot has one age and it is the same for
// every row in it.
type Signals struct {
	// Hits is how many distinct terms of the question this symbol carries.
	Hits int
	// Frequencies are the document frequencies of those hit terms, in any order.
	// A term matching most of the corpus separates nothing, and this is what
	// replaces a stopword list.
	Frequencies []int
	// Lengths are the rune counts of the words that hit, positionally aligned
	// with Frequencies. Frequency alone cannot tell a question's grammar from
	// its vocabulary: measured on this repository, `is` carries 178 of 22,299
	// symbols, which the discount below reads as almost pure signal, and it put
	// IsValidModuleKey first for three unrelated questions. Length can tell
	// them apart, and it does it without an English word list: the fold is a
	// prefix of five runes, so a two-rune word matched a two-rune segment of an
	// identifier, and in code those are particles -- Is, To, Of, At, In -- while
	// the vocabulary a question is actually about is longer than the fold.
	Lengths []int
	// Symbols is the size of the corpus the frequencies are counted against.
	Symbols int
	// Kind is the symbol's kind as the graph spells it.
	Kind string
	// Exported reports whether the declaration is visible outside its package.
	Exported bool
	// Path is the repository-relative path of the declaring file.
	Path string
	// Callers is how many resolved references point at the symbol.
	Callers int
	// Neighbours is how many terms of the question this symbol does not carry
	// itself and reaches through a resolved outgoing edge.
	//
	// It is the one signal in this struct that no text search can have. The
	// question that named this file -- where is every tool of this server
	// registered -- is answered by a function whose own name carries one term of
	// it and which calls twelve symbols carrying another; the file that holds it
	// ranked thirteenth on text alone and no amount of weighing its words moves
	// it, because the words are not there. Grep cannot see it, and a graph whose
	// edges come from matching names cannot claim it: an edge asserted by a
	// resolver is what makes this evidence rather than a coincidence.
	Neighbours int
}

// Score ranks one candidate. Higher is earlier; the number means nothing on its
// own.
//
// That last sentence is the contract, not a caveat. The score exists to order
// candidates against each other inside one answer: multiply every weight below
// by ten and the order is identical. It is therefore never published -- a
// caller shown `68.05` learns nothing it can act on, and would reasonably read
// it as a confidence this layer cannot claim.
//
// The shape is a product of independent multipliers over a base of term hits,
// because the signals are independent: being a function does not make a symbol
// more or less exported, and a question that hits two terms is not twice as
// likely to want a test fixture.
func Score(signals Signals) float64 {
	if signals.Hits <= 0 {
		return 0
	}
	score := evidence(signals)
	if score <= 0 {
		// Every term of the question is carried by the whole corpus, so the text
		// separates nothing -- and the structure still does. Falling to exactly
		// zero here would discard the kind, the visibility, the directory and
		// the caller count, and answer a degenerate question in stable-key
		// order: alphabetically, with the fixture first. The floor is far below
		// one hit of the commonest term a real corpus holds, so it only ever
		// decides an order that rarity declined to.
		score = rarityFloor
	}
	score *= kindWeight(signals.Kind)
	if signals.Exported {
		score *= 1.5
	}
	score *= pathWeight(signals.Path)
	score *= callerWeight(signals.Callers)
	score *= neighbourWeight(signals.Neighbours)
	return score
}

// neighbourWeight credits a symbol for the terms it reaches rather than holds.
//
// It is deliberately weaker than carrying the term: a symbol named after the
// question outranks one that merely calls something named after it, at every
// count. So this is a bounded multiplier over the text base and never an
// addition to it -- a candidate that carries no term at all is not a candidate,
// however much it calls.
func neighbourWeight(neighbours int) float64 {
	if neighbours <= 0 {
		return 1
	}
	weight := 1 + 0.3*float64(neighbours)
	if weight > neighbourCeiling {
		return neighbourCeiling
	}
	return weight
}

// neighbourCeiling is strictly below doubling, and the margin is the whole
// point: at exactly two, a symbol carrying one term and reaching another scores
// the same as one carrying both, and a tie is decided by the stable key -- which
// is alphabetical, and puts the fixture first. Reaching is weaker evidence than
// carrying, so it has to lose that comparison rather than draw it.
const neighbourCeiling = 1.9

// rarityFloor is what a candidate is worth when its terms are worthless. See
// the comment in Score: it exists so the structural signals can still order.
const rarityFloor = 1e-9

// evidence is the base: how many terms were hit, each discounted twice -- by how
// much of the corpus its term matches, and by how much of a term the word was.
//
// A term matching four symbols in five -- which the fold of `internal` does on
// this very repository -- carries almost no information, and counting it as a
// whole hit would let a question about anything rank a file by the directory it
// happens to sit in. A term matching a handful of symbols is nearly the whole
// answer.
//
// The discount is linear in the share rather than logarithmic. The classic
// inverse document frequency is a smoother curve and was not chosen, because
// what this needs is not a smooth curve but a floor: a term is worth its full
// hit when it is rare and worth almost nothing when it is everywhere, and the
// interesting behaviour is at the two ends rather than in the middle.
func evidence(signals Signals) float64 {
	if signals.Symbols <= 0 || len(signals.Frequencies) == 0 {
		// No corpus to compare against: every hit counts once, which is what a
		// caller that did not supply frequencies is asking for.
		return float64(signals.Hits)
	}
	base := 0.0
	for index, frequency := range signals.Frequencies {
		share := float64(frequency) / float64(signals.Symbols)
		if share < 0 {
			share = 0
		}
		if share > 1 {
			share = 1
		}
		// A term nobody carries contributes nothing rather than a full hit: the
		// caller counted a term it did not actually find.
		if frequency <= 0 {
			continue
		}
		base += (1 - share) * lengthWeight(signals, index)
	}
	return base
}

// lengthWeight is how much of a term a word was, squared.
//
// It saturates at the fold length because a word longer than the prefix carries
// no more evidence than the prefix does: `traversal` and `traverse` are one
// term, and neither is worth more than the other. It is squared rather than
// linear because the interesting behaviour is at the short end -- linear leaves
// a two-rune particle worth 0.4 of a real word, which two of them still outrank
// a real one with, and squared leaves it worth 0.16.
//
// A caller that supplies no lengths gets the full weight, which is what asking
// without them means.
func lengthWeight(signals Signals, index int) float64 {
	if index >= len(signals.Lengths) {
		return 1
	}
	runes := signals.Lengths[index]
	if runes <= 0 {
		return 0
	}
	if runes >= TermPrefixRunes {
		return 1
	}
	share := float64(runes) / float64(TermPrefixRunes)
	return share * share
}

// kindWeight prefers the kinds a question about behaviour is asking about.
//
// A question is nearly always about something that runs or something that holds
// state, and nearly never about the import that borrowed the name -- which is a
// row that names the same symbol without being it, and which would otherwise
// outrank the declaration in any file that imports a lot.
func kindWeight(kind string) float64 {
	switch kind {
	case "func", "function", "method", "struct_method", "constructor", "procedure":
		return 2
	case "struct", "class", "interface", "interface_type", "trait", "enum", "type", "type_alias", "record":
		return 1.5
	case "module", "package", "go_package", "namespace", "impl":
		return 1.2
	case "field", "property", "variable", "const", "constant", "static":
		return 0.5
	case "enum_variant", "enum_member":
		return 0.3
	case "import", "export", "use", "include", "reexport":
		return 0.2
	default:
		return 1
	}
}

// pathWeight reads the directories a repository is organised into.
//
// A fixture is the sharpest case: it is written to look exactly like production
// code, so its identifiers match a question as well as the real thing does, and
// it is never the answer. The weights below are the ones that survived being
// measured on this graph, where the widest term of all is a path segment.
func pathWeight(path string) float64 {
	normalized := strings.ToLower(strings.ReplaceAll(path, "\\", "/"))
	segments := strings.Split(normalized, "/")
	weight := 1.0
	for _, segment := range segments {
		switch segment {
		case "testdata", "fixtures", "__fixtures__", "node_modules", "vendor", "dist", "build", "target",
			".venv", "venv", "site-packages", "__pycache__", ".next", "out":
			return 0.1
		case "test", "tests", "spec", "benchmarks", "benchmark", "examples", "example", "samples", "sample", "demo", "demos":
			weight = 0.4
		case "internal", "src", "app", "lib", "pkg", "cmd":
			if weight == 1.0 {
				weight = 1.25
			}
		}
	}
	// A Go test file is not in a test directory, so the directory rules above
	// never see it.
	base := segments[len(segments)-1]
	if strings.HasSuffix(base, "_test.go") || strings.Contains(base, ".test.") ||
		strings.Contains(base, ".spec.") || strings.Contains(base, "_test.") {
		return 0.4
	}
	return weight
}

// callerWeight lets the graph speak.
//
// It is the one signal here that no text search can have, and the reason a
// question answered from this snapshot beats the same question answered by
// grep: how many resolved references point at a symbol is a fact an analyser
// established, not a guess about a name. It is bounded to double, because a
// popular helper is worth surfacing and is not worth burying the specific thing
// the question asked about.
func callerWeight(callers int) float64 {
	switch {
	case callers <= 0:
		return 1
	case callers >= 15:
		return 2
	default:
		// Doubling every time the count doubles, saturating at fifteen: the
		// difference between one caller and four matters, the difference
		// between forty and eighty does not.
		weight := 1.0
		for reached := 1; reached <= callers; reached *= 2 {
			weight += 0.25
		}
		return weight
	}
}
