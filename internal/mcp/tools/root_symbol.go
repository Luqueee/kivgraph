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
			candidates = append(candidates, symbolStableKey(snapshot, symbol))
			continue
		}
		location, err := resolveSymbolLocation(snapshot, symbol)
		if err != nil {
			candidates = append(candidates, symbolStableKey(snapshot, symbol))
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

// declarationKinds are the kinds that declare a symbol rather than mention one.
// An import row and a re-export row are facts about the same name, but a
// question about references is a question about the declaration: resolving to
// the import of a name would answer for the file that borrowed it.
var declarationKinds = map[string]struct{}{
	"function": {}, "func": {}, "method": {}, "class": {}, "interface": {},
	"struct": {}, "enum": {}, "type": {}, "trait": {}, "const": {},
	"variable": {}, "field": {}, "module": {}, "namespace": {},
}

func isDeclarationKind(kind string) bool {
	_, found := declarationKinds[kind]
	return found
}

// resolveDeclarationByName turns an unqualified name into the one symbol that
// declares it, so the common question costs one call instead of two.
//
// It is the same refusal to guess as resolveSymbolSelector: several
// declarations of one name -- `withRetry` names seven symbols across three
// languages in `workspace` -- return the candidates as `repository:path:line`, which
// measured 49 to 144 tokens against the 2.293 of listing every row of
// find_symbol, imports and re-exports included.
func resolveDeclarationByName(
	snapshot *hotsnapshot.GraphSnapshot,
	name, repository, path string,
) (hotsnapshot.SymbolID, string, error) {
	if repository != "" {
		if _, found := snapshot.RepositoryByName(repository); !found {
			return 0, "", NewToolError(CodeRepositoryNotFound, fmt.Sprintf(
				"repository %q is not in the published graph", repository,
			))
		}
	}
	interned, found := snapshot.Strings().Lookup(name)
	if !found {
		return 0, "", errNameNotFound(name)
	}
	filter := hotsnapshot.SymbolFilter{RepositoryName: repository, PathPrefix: path}
	page, err := snapshot.SearchSymbolsByName(interned, filter, 0, hotsnapshot.MaxExactResults)
	if err != nil {
		return 0, "", WrapToolError(CodeSnapshotUnavailable, "name lookup failed", err)
	}

	declarations := make([]hotsnapshot.SymbolID, 0, len(page.IDs))
	mentions := 0
	for _, id := range page.IDs {
		symbol, ok := snapshot.Symbol(id)
		if !ok {
			continue
		}
		kind, kindOK := snapshot.Strings().String(symbol.Kind)
		if !kindOK {
			continue
		}
		if isDeclarationKind(kind) {
			declarations = append(declarations, id)
			continue
		}
		mentions++
	}

	switch len(declarations) {
	case 1:
		symbol, ok := snapshot.Symbol(declarations[0])
		if !ok {
			return 0, "", NewToolError(CodeSnapshotUnavailable, "resolved symbol left the snapshot")
		}
		qualifiedName, qualifiedNameOK := snapshot.Strings().String(symbol.QualifiedName)
		if !qualifiedNameOK {
			return 0, "", NewToolError(CodeSnapshotUnavailable, "resolved symbol has an invalid qualified name")
		}
		return declarations[0], qualifiedName, nil
	case 0:
		if mentions > 0 {
			return 0, "", NewToolError(CodeSymbolNotFound, fmt.Sprintf(
				"name %q is only imported or re-exported here, never declared; pass the repository and path that declares it",
				name,
			))
		}
		return 0, "", errNameNotFound(name)
	}
	return 0, "", errNameAmbiguous(snapshot, name, declarations)
}

// errNameNotFound routes instead of only reporting.
//
// The names that reach this path are mostly not identifiers. Over five days
// they were `dart`, `posthog`, `websites`, `playw`, `HEAD` and `adria`:
// somebody is using a symbol lookup as grep, and the routing table in the
// repository's own instructions already says where that question belongs --
// "no sé cómo se llama, qué archivos abro: find_by_intent, con keywords". The
// error did not say it, while its neighbour eighty lines up has named the next
// step since it was written: a qualified name missing under a narrowing is
// told to drop the narrowing.
//
// This is `31` of the `63` non-answers that measurement counted -- more than
// the ambiguity refusal, and unlike it a real failure to answer. It stays one.
// What changes is that it costs the caller one more call instead of a guess.
//
// The tool is named through its registration constant and not as a literal.
// An error that routes to a tool the server does not publish is not a wording
// problem -- `internal/mcp/AGENTS.md` says it of the skill and it is true of
// every message that names one: it sends the question to a call that fails.
// Building the sentence from the constant is what makes a rename impossible to
// get half-done.
func errNameNotFound(name string) *ToolError {
	return NewToolError(CodeSymbolNotFound, fmt.Sprintf(
		"name %q was not found; if it names a topic rather than a symbol, call %s with it as a keyword",
		name, findByIntentToolName,
	))
}

func errNameAmbiguous(
	snapshot *hotsnapshot.GraphSnapshot,
	name string,
	declarations []hotsnapshot.SymbolID,
) error {
	candidates := make([]string, 0, len(declarations))
	for _, id := range declarations {
		symbol, ok := snapshot.Symbol(id)
		if !ok {
			continue
		}
		location, err := resolveSymbolLocation(snapshot, symbol)
		if err != nil {
			candidates = append(candidates, symbolStableKey(snapshot, symbol))
			continue
		}
		candidates = append(candidates, locationLabel(location.RepositoryName, location.FilePath, symbol.StartLine))
	}
	return NewToolError(CodeAmbiguousSymbol, fmt.Sprintf(
		"name %q declares %d symbols; repeat with the repository and path of the one you mean: %s",
		name, len(declarations), strings.Join(candidates, ", "),
	))
}
