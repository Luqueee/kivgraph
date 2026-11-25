package tools

import (
	"errors"
	"fmt"
	"strings"
	"unicode"

	"github.com/Luqueee/kivgraph/internal/facts"
	"github.com/Luqueee/kivgraph/internal/hotsnapshot"
)

// maxUnprovenRustSuggestions bounds the candidates an absence names. They say
// where to look and none of them is an answer, so a long list is only cost.
const maxUnprovenRustSuggestions = 5

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
	unprovenTotal := 0
	for offset := 0; ; offset += hotsnapshot.MaxExactResults {
		page, err := snapshot.SearchSymbolsByName(interned, filter, offset, hotsnapshot.MaxExactResults)
		if err != nil {
			return 0, WrapToolError(CodeSnapshotUnavailable, "name lookup failed", err)
		}
		for _, id := range page.IDs {
			switch classifyRustReceiver(snapshot, id, receiver) {
			case rustReceiverProven:
				proven = append(proven, id)
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
	switch len(proven) {
	case 0:
		return 0, errRustReceiverUnproven(snapshot, selector, unproven, unprovenTotal)
	case 1:
		if unprovenTotal == 0 {
			return proven[0], nil
		}
		return 0, NewToolError(CodeAmbiguousSymbol, fmt.Sprintf(
			"qualified name %q has a verified match %s but receiver ownership is unproven for %d other candidate(s): %s; repeat with an exact qualified_name",
			selector.QualifiedName, rustCandidateLabels(snapshot, proven)[0], unprovenTotal,
			strings.Join(rustCandidateLabels(snapshot, unproven), ", "),
		))
	}
	return 0, errRustReceiverAmbiguous(snapshot, selector, receiver, proven)
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
	if step == "" {
		return false
	}
	for index, r := range step {
		if r == '_' || unicode.IsLetter(r) || (index > 0 && unicode.IsDigit(r)) {
			continue
		}
		return false
	}
	return true
}

// classifyRustReceiver reports whether the index pairs this method with the
// receiver. Only a METHOD_OF edge from rust-analyzer's own definition proves it;
// a Go method has the same edge from another provenance and never counts.
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
	for _, edge := range snapshot.Outgoing(id) {
		if edge.Kind != facts.CodeMethodOf || edge.Provenance != facts.CodeRustAnalyzerDefinition || edge.Confidence != facts.CodeStructuralCertain {
			continue
		}
		if owner, found := snapshot.Symbol(edge.Target); found && rustTypeNamed(snapshot, owner, receiver) {
			return rustReceiverProven
		}
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

func errRustReceiverAmbiguous(
	snapshot *hotsnapshot.GraphSnapshot,
	selector symbolSelector,
	receiver string,
	methods []hotsnapshot.SymbolID,
) error {
	return NewToolError(CodeAmbiguousSymbol, fmt.Sprintf(
		"qualified name %q names %d Rust methods of %s; repeat with the exact qualified_name of the one you mean: %s",
		selector.QualifiedName, len(methods), receiver, strings.Join(rustCandidateLabels(snapshot, methods), ", "),
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
	more := ""
	if total > len(unproven) {
		more = fmt.Sprintf(" (+%d more)", total-len(unproven))
	}
	var notFound *ToolError
	if !errors.As(absent, &notFound) {
		return absent
	}
	return NewToolError(notFound.Code, fmt.Sprintf(
		"%s; the index pairs none of these with a declared Rust type, so they are unproven -- repeat with the exact qualified_name of the one you mean: %s%s",
		notFound.Message, strings.Join(rustCandidateLabels(snapshot, unproven), ", "), more,
	))
}

// rustCandidateLabels names each method by its canonical name, which is what
// the next call resolves exactly, and by where it is.
func rustCandidateLabels(snapshot *hotsnapshot.GraphSnapshot, ids []hotsnapshot.SymbolID) []string {
	labels := make([]string, 0, len(ids))
	for _, id := range ids {
		symbol, ok := snapshot.Symbol(id)
		if !ok {
			continue
		}
		qualifiedName, _ := snapshot.Strings().String(symbol.QualifiedName)
		location, err := resolveSymbolLocation(snapshot, symbol)
		if err != nil {
			labels = append(labels, qualifiedName+" ("+symbolStableKey(snapshot, symbol)+")")
			continue
		}
		labels = append(labels, qualifiedName+" ("+locationLabel(location.RepositoryName, location.FilePath, symbol.StartLine)+")")
	}
	return labels
}
