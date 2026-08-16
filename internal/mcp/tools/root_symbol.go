package tools

import (
	"fmt"
	"strings"

	"github.com/Luqueee/kivgraph/internal/hotsnapshot"
)

// symbolSelector names one symbol the way a caller already holds it.
//
// A stable key is durable and exact, and it is also 35 tokens of base32 that
// nothing but this server can read. Every row this surface returns already
// carries a repository, a repository-relative path and a qualified name, so the
// triple lets a caller address the next call out of the answer it just got,
// without the key ever entering the conversation. That is what makes it
// affordable to withhold keys from list responses.
type symbolSelector struct {
	StableKey     string
	Repository    string
	Path          string
	QualifiedName string
}

// normalizeSymbolSelector checks that exactly one way of naming a symbol was
// given, and that the narrowing fields make sense on their own.
//
// Two selectors is not a preference to resolve quietly: they can disagree, and
// answering one of them is answering a question nobody asked.
func normalizeSymbolSelector(stableKey, repository, path, qualifiedName string) (symbolSelector, error) {
	selector := symbolSelector{
		StableKey:     stableKey,
		Repository:    repository,
		Path:          path,
		QualifiedName: qualifiedName,
	}
	for field, value := range map[string]string{
		"stable_key": stableKey, "repository": repository, "path": path, "qualified_name": qualifiedName,
	} {
		if strings.TrimSpace(value) != value {
			return symbolSelector{}, NewToolError(CodeInvalidArgument, fmt.Sprintf("%s must not carry surrounding whitespace", field))
		}
	}
	switch {
	case selector.StableKey == "" && selector.QualifiedName == "":
		return symbolSelector{}, NewToolError(CodeInvalidArgument, "one of stable_key or qualified_name is required")
	case selector.StableKey != "" && selector.QualifiedName != "":
		return symbolSelector{}, NewToolError(CodeInvalidArgument, "pass either stable_key or qualified_name, not both")
	case selector.StableKey != "" && (selector.Repository != "" || selector.Path != ""):
		return symbolSelector{}, NewToolError(CodeInvalidArgument, "repository and path narrow a qualified_name; a stable_key already names one symbol")
	case selector.Path != "" && selector.Repository == "":
		return symbolSelector{}, NewToolError(CodeInvalidArgument, "path is repository-relative, so it requires repository")
	}
	if selector.Path != "" {
		normalized, err := normalizeOutlinePath(selector.Path)
		if err != nil {
			return symbolSelector{}, err
		}
		selector.Path = normalized
	}
	return selector, nil
}

// resolveSymbolSelector turns a selector into one symbol.
//
// A qualified name matching more than one symbol is ambiguous and says so
// instead of silently picking one. What the message offers depends on what is
// left to narrow: with no repository or path it names them, because that is a
// narrowing the caller can express in the next call; once both were given and
// the name still matches twice, only the key separates them and the keys are
// listed. Choosing for the caller would be the nominal coincidence this project
// forbids in the graph, and it is worth no more in the surface.
func resolveSymbolSelector(
	snapshot *hotsnapshot.GraphSnapshot,
	selector symbolSelector,
) (hotsnapshot.SymbolID, error) {
	if selector.StableKey != "" {
		id, found := snapshot.SymbolByStableKey(hotsnapshot.StableKey(selector.StableKey))
		if !found {
			return 0, NewToolError(CodeSymbolNotFound, fmt.Sprintf("symbol %q was not found", selector.StableKey))
		}
		return id, nil
	}

	if selector.Repository != "" {
		if _, found := snapshot.RepositoryByName(selector.Repository); !found {
			return 0, NewToolError(CodeRepositoryNotFound, fmt.Sprintf(
				"repository %q is not in the published graph", selector.Repository,
			))
		}
	}
	interned, found := snapshot.Strings().Lookup(selector.QualifiedName)
	if !found {
		return 0, errSelectorNotFound(selector)
	}
	filter := hotsnapshot.SymbolFilter{RepositoryName: selector.Repository, PathPrefix: selector.Path}
	page, err := snapshot.SearchSymbolsByQName(interned, filter, 0, hotsnapshot.MaxExactResults)
	if err != nil {
		return 0, WrapToolError(CodeSnapshotUnavailable, "qualified name lookup failed", err)
	}
	switch len(page.IDs) {
	case 0:
		return 0, errSelectorNotFound(selector)
	case 1:
		return page.IDs[0], nil
	}
	return 0, errSelectorAmbiguous(snapshot, selector, page)
}

// errSelectorNotFound distinguishes a name nobody declares from a name the
// narrowing excluded. They read the same to a caller that only sees "not
// found", and they need different fixes.
func errSelectorNotFound(selector symbolSelector) error {
	if selector.Repository == "" && selector.Path == "" {
		return NewToolError(CodeSymbolNotFound, fmt.Sprintf("qualified name %q was not found", selector.QualifiedName))
	}
	where := selector.Repository
	if selector.Path != "" {
		where += " " + selector.Path
	}
	return NewToolError(CodeSymbolNotFound, fmt.Sprintf(
		"qualified name %q was not found under %s; call it without repository and path to search the whole graph",
		selector.QualifiedName, where,
	))
}

func errSelectorAmbiguous(
	snapshot *hotsnapshot.GraphSnapshot,
	selector symbolSelector,
	page hotsnapshot.SymbolPage,
) error {
	narrowed := selector.Repository != "" && selector.Path != ""
	candidates := make([]string, 0, len(page.IDs))
	for _, id := range page.IDs {
		symbol, ok := snapshot.Symbol(id)
		if !ok {
			continue
		}
		if narrowed {
			candidates = append(candidates, string(symbol.StableKey))
			continue
		}
		location, err := resolveSymbolLocation(snapshot, symbol)
		if err != nil {
			candidates = append(candidates, string(symbol.StableKey))
			continue
		}
		candidates = append(candidates, fmt.Sprintf("%s %s:%d-%d",
			location.RepositoryName, location.FilePath, symbol.StartLine, symbol.EndLine))
	}
	if narrowed {
		return NewToolError(CodeAmbiguousSymbol, fmt.Sprintf(
			"qualified name %q names %d symbols in %s %s, so only a stable_key separates them: %s",
			selector.QualifiedName, page.Total, selector.Repository, selector.Path, strings.Join(candidates, ", "),
		))
	}
	return NewToolError(CodeAmbiguousSymbol, fmt.Sprintf(
		"qualified name %q names %d symbols; narrow with repository and path: %s",
		selector.QualifiedName, page.Total, strings.Join(candidates, ", "),
	))
}
