package tools

import (
	"fmt"
	"strings"

	"github.com/Luqueee/ladygraph/internal/hotsnapshot"
)

// resolveRootSymbol accepts either a durable stable key or a qualified name.
//
// An agent that just read an outline holds the key. One reading a diff, a
// stack trace or a review comment holds the name, and making it run a search
// first is a call it should not have to make.
//
// A qualified name matching more than one symbol is ambiguous and says so,
// naming the keys, instead of silently picking one: the two answers would
// differ and nobody would know which was given.
func resolveRootSymbol(
	snapshot *hotsnapshot.GraphSnapshot,
	stableKey, qualifiedName string,
) (hotsnapshot.SymbolID, error) {
	if stableKey != "" {
		id, found := snapshot.SymbolByStableKey(hotsnapshot.StableKey(stableKey))
		if !found {
			return 0, NewToolError(CodeSymbolNotFound, fmt.Sprintf("symbol %q was not found", stableKey))
		}
		return id, nil
	}

	interned, found := snapshot.Strings().Lookup(qualifiedName)
	if !found {
		return 0, NewToolError(CodeSymbolNotFound, fmt.Sprintf("qualified name %q was not found", qualifiedName))
	}
	page, err := snapshot.SearchSymbolsByQName(interned, hotsnapshot.SymbolFilter{}, 0, hotsnapshot.MaxExactResults)
	if err != nil {
		return 0, WrapToolError(CodeSnapshotUnavailable, "qualified name lookup failed", err)
	}
	switch len(page.IDs) {
	case 0:
		return 0, NewToolError(CodeSymbolNotFound, fmt.Sprintf("qualified name %q was not found", qualifiedName))
	case 1:
		return page.IDs[0], nil
	}
	keys := make([]string, 0, len(page.IDs))
	for _, id := range page.IDs {
		if symbol, ok := snapshot.Symbol(id); ok {
			keys = append(keys, string(symbol.StableKey))
		}
	}
	return 0, NewToolError(CodeAmbiguousSymbol, fmt.Sprintf(
		"qualified name %q names %d symbols, pass one of these stable keys instead: %s",
		qualifiedName, page.Total, strings.Join(keys, ", "),
	))
}

// normalizeRootSelector checks that exactly one root selector was given. Two
// selectors is not a preference to resolve quietly: they can disagree, and
// answering one of them is answering a question nobody asked.
func normalizeRootSelector(stableKey, qualifiedName string) (string, string, error) {
	key := strings.TrimSpace(stableKey)
	name := strings.TrimSpace(qualifiedName)
	if key != stableKey || name != qualifiedName {
		return "", "", NewToolError(CodeInvalidArgument, "root selectors must not carry surrounding whitespace")
	}
	switch {
	case key == "" && name == "":
		return "", "", NewToolError(CodeInvalidArgument, "one of stable_key or qualified_name is required")
	case key != "" && name != "":
		return "", "", NewToolError(CodeInvalidArgument, "pass either stable_key or qualified_name, not both")
	}
	return key, name, nil
}
