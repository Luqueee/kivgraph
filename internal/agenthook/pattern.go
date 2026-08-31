package agenthook

import (
	"regexp"
	"strings"
)

// bareName is a pattern that is nothing but a name, qualified or not.
//
// `NewServer`, `indexer`, `pkg.NewServer`, `Indexer::new` and `graph->load` all
// match: every one of them is a caller asking where something is or who uses
// it, which is the question `find_symbol` and `find_references` were built for.
var bareName = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*(?:(?:\.|::|->)[A-Za-z_][A-Za-z0-9_]*)*$`)

// declaration is a name preceded by the keyword that declares it.
//
// `grep "func NewServer"` is the single most common way an agent looks for a
// definition, and it is exactly `find_symbol`. Matching it here is worth more
// than every other rule in this file: without it the most gateable search in
// the corpus reads as prose and walks straight through.
var declaration = regexp.MustCompile(
	`^(?:func|fn|def|type|class|struct|interface|trait|impl|enum|const|var|let)\s+` +
		`([A-Za-z_][A-Za-z0-9_]*)\b`)

// nameFragment is an identifier hiding inside a regular expression.
var nameFragment = regexp.MustCompile(`[A-Za-z_][A-Za-z0-9_]{2,}`)

// metacharacters are what tells a regular expression from a literal.
const metacharacters = `*+?[]{}()|^$\`

// patternQuestion answers what a search pattern is really asking.
//
// The three answers are not a ranking. A name is a symbol question, a regex
// built out of name fragments is an intent question, and free text is neither:
// `grep` is the right tool for finding the words `TODO fix this later`, the
// graph does not index them, and refusing that search would trade a correct
// cheap answer for a wrong one.
func patternQuestion(pattern string) Question {
	text := unquote(strings.TrimSpace(pattern))
	if len(text) < 3 {
		return Question{}
	}
	if match := declaration.FindStringSubmatch(text); match != nil {
		return Question{Kind: KindSymbol, Pattern: match[1]}
	}
	if bareName.MatchString(text) {
		return Question{Kind: KindSymbol, Pattern: text}
	}
	if !strings.ContainsAny(text, metacharacters) {
		// Several plain words. The graph has no text index and would
		// answer worse than the grep this would have refused.
		return Question{}
	}
	// A regular expression. It is worth redirecting only when what it is
	// built out of are names -- `New.*Server` is a caller groping for a
	// symbol it cannot spell, and that is what `find_by_intent` is for --
	// and not when it is punctuation around ordinary words.
	fragments := nameFragment.FindAllString(text, -1)
	if len(fragments) == 0 || (!anyCodeShaped(fragments) && !broadNameAlternation(text)) {
		return Question{}
	}
	return Question{Kind: KindIntent, Pattern: text}
}

// broadNameAlternation recognises the file-discovery query an agent derives
// from a natural-language task when it has no symbol name yet.
//
// `http|route|handler|listen|serve` is not code-shaped by capitalization, but
// five alternative identifiers make it deliberately broad rather than a
// request for one literal string. Four is the boundary: shorter alternations
// such as `error|warning|failed` are ordinary text searches and stay with
// grep. Every branch must still be a bare name, so punctuation-heavy regular
// expressions do not cross this route by merely containing several pipes.
func broadNameAlternation(text string) bool {
	parts := strings.Split(text, "|")
	if len(parts) < 4 {
		return false
	}
	for _, part := range parts {
		if !bareName.MatchString(part) {
			return false
		}
	}
	return true
}

// anyCodeShaped reports whether a fragment is spelled the way code is spelled
// and prose is not: an inner capital or an underscore.
//
// `New.*Server` passes and `the.*thing` does not. A lowercase fragment on its
// own is not enough, because that is also every English word, and a rule that
// treated it as a symbol would refuse `grep -r "error handling" docs/`.
func anyCodeShaped(fragments []string) bool {
	for _, fragment := range fragments {
		if strings.ContainsRune(fragment, '_') {
			return true
		}
		if strings.ToLower(fragment) != fragment {
			return true
		}
	}
	return false
}

// unquote strips one layer of shell quoting from a pattern.
func unquote(text string) string {
	if len(text) < 2 {
		return text
	}
	first, last := text[0], text[len(text)-1]
	if first == last && (first == '\'' || first == '"') {
		return text[1 : len(text)-1]
	}
	return text
}
