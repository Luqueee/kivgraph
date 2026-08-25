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
	score := rarity(signals)
	score *= kindWeight(signals.Kind)
	if signals.Exported {
		score *= 1.5
	}
	score *= pathWeight(signals.Path)
	score *= callerWeight(signals.Callers)
	return score
}

// rarity is the base: how many terms were hit, each discounted by how much of
// the corpus its term matches.
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
func rarity(signals Signals) float64 {
	if signals.Symbols <= 0 || len(signals.Frequencies) == 0 {
		// No corpus to compare against: every hit counts once, which is what a
		// caller that did not supply frequencies is asking for.
		return float64(signals.Hits)
	}
	base := 0.0
	for _, frequency := range signals.Frequencies {
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
		base += 1 - share
	}
	return base
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
