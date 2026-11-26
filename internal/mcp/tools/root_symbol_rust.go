package tools

import (
	"errors"
	"fmt"
	"strings"
	"unicode"

	"golang.org/x/text/unicode/norm"

	"github.com/Luqueee/kivgraph/internal/facts"
	"github.com/Luqueee/kivgraph/internal/hotsnapshot"
)

// maxUnprovenRustSuggestions bounds the candidates an absence names. They say
// where to look and none of them is an answer, so a long list is only cost.
const maxUnprovenRustSuggestions = 5

// maxProvenRustExamples bounds the verified methods an ambiguity names. The
// error still counts every one of them: the count is what makes the name
// ambiguous, and a few examples are enough to choose between or to narrow by.
const maxProvenRustExamples = 5

// rustReceiverEvidence says what the index proves about one method of the
// right name for the receiver a caller wrote.
type rustReceiverEvidence int

const (
	rustReceiverUnrelated rustReceiverEvidence = iota
	rustReceiverProven
	rustReceiverUnproven
)

// Only the analyzer's METHOD_OF edge proves ownership. Descriptor text may
// suggest a canonical name for retry, but is never evidence for selecting it.
func resolveRustReceiverMethod(
	snapshot *hotsnapshot.GraphSnapshot,
	selector symbolSelector,
) (hotsnapshot.SymbolID, error) {
	receiver, method, ok := splitRustReceiverPath(selector.QualifiedName)
	if !ok {
		return 0, errSelectorNotFound(selector)
	}
	interned, found := snapshot.Strings().Lookup(method)
	if !found {
		return 0, errSelectorNotFound(selector)
	}
	// The narrowing is the exact lookup's: it names where the method is, not
	// where its type is.
	filter := hotsnapshot.SymbolFilter{RepositoryName: selector.Repository, Path: selector.Path}
	var proven, unproven []hotsnapshot.SymbolID
	provenTotal, unprovenTotal := 0, 0
	for offset := 0; ; offset += hotsnapshot.MaxExactResults {
		page, err := snapshot.SearchSymbolsByName(interned, filter, offset, hotsnapshot.MaxExactResults)
		if err != nil {
			return 0, WrapToolError(CodeSnapshotUnavailable, "name lookup failed", err)
		}
		for _, id := range page.IDs {
			switch classifyRustReceiver(snapshot, id, receiver) {
			case rustReceiverProven:
				provenTotal++
				if len(proven) < maxProvenRustExamples {
					proven = append(proven, id)
				}
			case rustReceiverUnproven:
				unprovenTotal++
				if len(unproven) < maxUnprovenRustSuggestions {
					unproven = append(unproven, id)
				}
			}
		}
		if !page.HasMore {
			break
		}
	}
	switch provenTotal {
	case 0:
		return 0, errRustReceiverUnproven(snapshot, selector, unproven, unprovenTotal)
	case 1:
		if unprovenTotal == 0 {
			return proven[0], nil
		}
		return 0, errRustReceiverMixed(snapshot, selector, proven[0], unproven, unprovenTotal)
	}
	return 0, errRustReceiverAmbiguous(snapshot, selector, receiver, proven, provenTotal)
}

// splitRustReceiverPath reads `Type::method` or `module::Type::method`.
//
// It is strict on purpose: every step must be a plain identifier. A generic, a
// reference, a `<T as Trait>` path, a leading `::` or an argument list is not
// something the index pairs with a declared type, so none of them is attempted.
func splitRustReceiverPath(qualifiedName string) (receiver, method string, ok bool) {
	receiver, method, ok = strings.Cut(qualifiedName, "::")
	if !ok || !isRustIdentifier(receiver) {
		return "", "", false
	}
	for step := range strings.SplitSeq(method, "::") {
		if !isRustIdentifier(step) {
			return "", "", false
		}
	}
	last := strings.LastIndex(qualifiedName, "::")
	return qualifiedName[:last], qualifiedName[last+2:], true
}

func isRustIdentifier(step string) bool {
	if step == "" || step == "_" {
		return false
	}
	for index, r := range step {
		start := index == 0
		if r < 128 {
			if r == '_' || 'a' <= r && r <= 'z' || 'A' <= r && r <= 'Z' || !start && '0' <= r && r <= '9' {
				continue
			}
			return false
		}
		if !rustIDRune(r, start) {
			return false
		}
		// XID is closed under NFKC. Check the expansion, never use it for lookup.
		for i, normalized := range norm.NFKC.String(string(r)) {
			if !rustIDRune(normalized, start && i == 0) {
				return false
			}
		}
	}
	return true
}

func rustIDRune(r rune, start bool) bool {
	if unicode.In(r, unicode.Pattern_Syntax, unicode.Pattern_White_Space) || r == '\u200c' || r == '\u200d' {
		return false
	}
	if unicode.In(r, unicode.L, unicode.Nl, unicode.Other_ID_Start) {
		return true
	}
	return !start && unicode.In(r, unicode.Mn, unicode.Mc, unicode.Nd, unicode.Pc, unicode.Other_ID_Continue)
}

// classifyRustReceiver reports whether the index pairs this method with the
// receiver. Only a METHOD_OF edge from rust-analyzer's own definition proves it;
// a Go method has the same edge from another provenance and never counts. The
// same edge refutes the receiver when it names another type, and then the
// canonical name is not read: it cannot tell `a::Cursor` from `b::Cursor`.
func classifyRustReceiver(
	snapshot *hotsnapshot.GraphSnapshot,
	id hotsnapshot.SymbolID,
	receiver string,
) rustReceiverEvidence {
	symbol, ok := snapshot.Symbol(id)
	if !ok {
		return rustReceiverUnrelated
	}
	table := snapshot.Strings()
	language, _ := table.String(symbol.Language)
	kind, _ := table.String(symbol.Kind)
	if language != string(facts.LanguageRust) || kind != "method" {
		return rustReceiverUnrelated
	}
	owned := false
	for _, edge := range snapshot.Outgoing(id) {
		if edge.Kind != facts.CodeMethodOf || edge.Provenance != facts.CodeRustAnalyzerDefinition || edge.Confidence != facts.CodeStructuralCertain {
			continue
		}
		owner, found := snapshot.Symbol(edge.Target)
		if !found {
			continue
		}
		if rustTypeNamed(snapshot, owner, receiver) {
			return rustReceiverProven
		}
		owned = true
	}
	if owned {
		return rustReceiverUnrelated
	}
	qualifiedName, _ := table.String(symbol.QualifiedName)
	if rustPathHasStep(qualifiedName, receiver) {
		return rustReceiverUnproven
	}
	return rustReceiverUnrelated
}

// rustTypeNamed reports whether a declared type is the one a receiver names: by
// its whole canonical name, or -- for a receiver written as one identifier, as
// source does inside the module that declares the type -- by its own name.
func rustTypeNamed(snapshot *hotsnapshot.GraphSnapshot, owner hotsnapshot.SymbolRecord, receiver string) bool {
	table := snapshot.Strings()
	if qualifiedName, ok := table.String(owner.QualifiedName); ok && qualifiedName == receiver {
		return true
	}
	if strings.Contains(receiver, "::") {
		return false
	}
	name, ok := table.String(owner.Name)
	return ok && name == receiver
}

// rustPathHasStep reports whether a canonical name carries the receiver's own
// name as a step before its last one, which is what an impl block the index did
// not pair with a declared type looks like. It only selects what to suggest:
// nothing resolves through it.
func rustPathHasStep(qualifiedName, receiver string) bool {
	if pos := strings.LastIndex(receiver, "::"); pos >= 0 {
		receiver = receiver[pos+2:]
	}
	pos := strings.LastIndex(qualifiedName, "::")
	if pos < 0 {
		return false
	}
	for step := range strings.SplitSeq(qualifiedName[:pos], "::") {
		base, _, _ := strings.Cut(step, "<")
		if base == receiver {
			return true
		}
	}
	return false
}

// errRustReceiverAmbiguous names a bounded sample of the methods the index
// proves and counts all of them, so a receiver and method common to many
// repositories does not make an error as long as the list of matches.
func errRustReceiverAmbiguous(
	snapshot *hotsnapshot.GraphSnapshot,
	selector symbolSelector,
	receiver string,
	examples []hotsnapshot.SymbolID,
	total int,
) error {
	labels, shared := rustCandidateLabels(snapshot, examples)
	more := rustMore(total, len(examples))
	if more != "" {
		more += "; add repository or path to this call to narrow them"
	}
	return NewToolError(CodeAmbiguousSymbol, fmt.Sprintf(
		"qualified name %q names %d Rust methods of %s; %s: %s%s",
		selector.QualifiedName, total, receiver, rustRetryAdvice(shared), strings.Join(labels, ", "), more,
	))
}

// errRustReceiverMixed is the ambiguity between the one method the index
// proves and same-named methods it cannot vouch for: an unproven one might be
// the method the caller meant, so the verified one is not picked.
func errRustReceiverMixed(
	snapshot *hotsnapshot.GraphSnapshot,
	selector symbolSelector,
	verified hotsnapshot.SymbolID,
	unproven []hotsnapshot.SymbolID,
	total int,
) error {
	labels, shared := rustCandidateLabels(snapshot, append([]hotsnapshot.SymbolID{verified}, unproven...))
	return NewToolError(CodeAmbiguousSymbol, fmt.Sprintf(
		"qualified name %q has a verified match %s but receiver ownership is unproven for %d other candidate(s): %s%s; %s",
		selector.QualifiedName, labels[0], total, strings.Join(labels[1:], ", "), rustMore(total, len(unproven)), rustRetryAdvice(shared),
	))
}

// errRustReceiverUnproven is the ordinary absence, plus the methods that look
// like the one asked for and that the index cannot vouch for.
func errRustReceiverUnproven(
	snapshot *hotsnapshot.GraphSnapshot,
	selector symbolSelector,
	unproven []hotsnapshot.SymbolID,
	total int,
) error {
	absent := errSelectorNotFound(selector)
	if len(unproven) == 0 {
		return absent
	}
	var notFound *ToolError
	if !errors.As(absent, &notFound) {
		return absent
	}
	labels, _ := rustCandidateLabels(snapshot, unproven)
	return NewToolError(notFound.Code, fmt.Sprintf(
		"%s; the index pairs none of these with a declared Rust type, so they are unproven -- repeat with the exact qualified_name of the one you mean: %s%s",
		notFound.Message, strings.Join(labels, ", "), rustMore(total, len(unproven)),
	))
}

// rustCandidateLabels names each method by its canonical name, which is what
// the next call resolves exactly, and by where it is. Methods that share
// repository, path and canonical name are told apart only by their stable_key,
// so those carry it, and shared says that some do.
func rustCandidateLabels(snapshot *hotsnapshot.GraphSnapshot, ids []hotsnapshot.SymbolID) (labels []string, shared bool) {
	type site struct{ repository, path, name string }
	type candidate struct {
		label string // everything but the closing parenthesis
		at    site
		key   string
	}
	candidates := make([]candidate, 0, len(ids))
	sites := make(map[site]int, len(ids))
	for _, id := range ids {
		symbol, ok := snapshot.Symbol(id)
		if !ok {
			continue
		}
		qualifiedName, _ := snapshot.Strings().String(symbol.QualifiedName)
		key := symbolStableKey(snapshot, symbol)
		location, err := resolveSymbolLocation(snapshot, symbol)
		if err != nil {
			candidates = append(candidates, candidate{label: qualifiedName + " (" + key})
			continue
		}
		at := site{location.RepositoryName, location.FilePath, qualifiedName}
		sites[at]++
		candidates = append(candidates, candidate{
			label: qualifiedName + " (" + locationLabel(location.RepositoryName, location.FilePath, symbol.StartLine),
			at:    at,
			key:   key,
		})
	}
	for _, c := range candidates {
		if sites[c.at] > 1 {
			shared = true
			c.label += ", stable_key " + c.key
		}
		labels = append(labels, c.label+")")
	}
	return labels, shared
}

// rustMore says how many candidates a bounded list left out.
func rustMore(total, shown int) string {
	if total <= shown {
		return ""
	}
	return fmt.Sprintf(" (+%d more)", total-shown)
}

// rustRetryAdvice says what selects one of the methods listed. A canonical name
// can occur in several files and repositories, so the repository and path go
// with it. Methods that share all three are told apart only by a stable_key,
// which goes alone: a stable_key is refused beside a qualified_name,
// repository or path.
func rustRetryAdvice(shared bool) string {
	advice := "repeat with the exact qualified_name of the one you mean plus its repository and path"
	if shared {
		advice += ", or, for a candidate that shows a stable_key, with that stable_key alone"
	}
	return advice
}
