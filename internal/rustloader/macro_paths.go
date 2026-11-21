package rustloader

import (
	"sort"
	"strings"

	tree_sitter "github.com/tree-sitter/go-tree-sitter"
)

// MacroPath is a path written in the expansion of a `macro_rules!` definition.
//
// rust-analyzer resolves nothing there: the body of a declarative macro is a
// token tree, not code, so the index carries no occurrence for a name used
// inside one. The grammar still sees the tokens, which is enough to say which
// declaration a path names when only one can carry it, and never enough to
// prove it.
type MacroPath struct {
	// Segments are the names of the path without a leading `$crate`, `crate`,
	// `self` or `super`.
	Segments []string
	// Called is true when the path is followed by an argument list.
	Called bool
	// Start and End are the byte span of the last segment.
	Start int
	End   int
}

// pathRoots are the leading segments that name a scope, not a declaration.
var pathRoots = map[string]bool{"$crate": true, "crate": true, "self": true, "super": true}

// MacroPaths lists the paths written in the expansions of the macros this file
// defines, in source order. A name that is only a name -- not part of a path
// and not called -- is left out: a bare word in a token tree is as likely to be
// a local binding or a keyword of the macro's own language as a declaration. A
// path after `.` is a method or a field, which needs the receiver's type to
// resolve, and one before `!` is a macro invocation.
func (source *Source) MacroPaths() []MacroPath {
	if source == nil || source.root == nil {
		return nil
	}
	var paths []MacroPath
	source.walkNodes(func(node *tree_sitter.Node) {
		if node.Kind() != "macro_rule" {
			return
		}
		if expansion := node.ChildByFieldName("right"); expansion != nil {
			paths = append(paths, source.pathsIn(expansion)...)
		}
	})
	sort.Slice(paths, func(left, right int) bool { return paths[left].Start < paths[right].Start })
	return paths
}

// pathsIn reads the leaves of one token tree as a flat sequence, because the
// grammar nests parentheses and repetitions but never groups a path.
func (source *Source) pathsIn(root *tree_sitter.Node) []MacroPath {
	var leaves []*tree_sitter.Node
	var collect func(node *tree_sitter.Node)
	collect = func(node *tree_sitter.Node) {
		if node.ChildCount() == 0 {
			leaves = append(leaves, node)
			return
		}
		for index := uint(0); index < node.ChildCount(); index++ {
			if child := node.Child(index); child != nil {
				collect(child)
			}
		}
	}
	collect(root)

	var paths []MacroPath
	for index := 0; index < len(leaves); {
		if !isPathSegment(leaves[index]) || (index > 0 && leaves[index-1].Kind() == ".") {
			index++
			continue
		}
		segments := []string{source.leafText(leaves[index])}
		last := index
		for last+2 < len(leaves) && leaves[last+1].Kind() == "::" && isPathSegment(leaves[last+2]) {
			last += 2
			segments = append(segments, source.leafText(leaves[last]))
		}
		next := last + 1
		called := next < len(leaves) && leaves[next].Kind() == "("
		invoked := next < len(leaves) && leaves[next].Kind() == "!"
		for len(segments) > 0 && pathRoots[segments[0]] {
			segments = segments[1:]
		}
		if len(segments) > 0 && !invoked && (len(segments) > 1 || called) && !pathRoots[segments[len(segments)-1]] && !hasMetavariable(segments) {
			start, end := leaves[last].ByteRange()
			paths = append(paths, MacroPath{Segments: segments, Called: called, Start: int(start), End: int(end)})
		}
		index = last + 1
	}
	return paths
}

// hasMetavariable reports whether a segment stands for whatever the caller
// passes, which names nothing.
func hasMetavariable(segments []string) bool {
	for _, segment := range segments {
		if strings.HasPrefix(segment, "$") {
			return true
		}
	}
	return false
}

// isPathSegment reports whether a leaf can be one step of a path.
func isPathSegment(leaf *tree_sitter.Node) bool {
	switch leaf.Kind() {
	case "identifier", "crate", "self", "super":
		return true
	case "metavariable":
		return true
	}
	return false
}

func (source *Source) leafText(leaf *tree_sitter.Node) string {
	start, end := leaf.ByteRange()
	return string(source.code[start:end])
}

// macroTarget is a declaration a macro path can name: where it sits in its
// crate, spelled as the names of its descriptor path.
type macroTarget struct {
	key   string
	kind  string
	crate string
	names []string
}

// macroIndex finds the declaration a path in a macro body names.
type macroIndex map[string][]macroTarget

func newMacroIndex(definitions map[string]Definition) macroIndex {
	index := make(macroIndex)
	for _, definition := range definitions {
		identity, err := ParseSymbol(definition.Symbol)
		if err != nil || !identity.Addressable() {
			continue
		}
		names := make([]string, len(identity.Descriptors))
		for position, descriptor := range identity.Descriptors {
			names[position] = descriptor.Name
		}
		last := names[len(names)-1]
		index[last] = append(index[last], macroTarget{
			key: string(definition.StableKey), kind: definition.Kind, crate: definition.Crate.Name, names: names,
		})
	}
	return index
}

// resolve answers the one declaration of the macro's own crate whose path ends
// with the written one. Two declarations that fit -- the trait method and the
// same name in another module -- name nothing: choosing between them would be
// a guess, and a guess is not an edge. A bare called name only matches a free
// function, because a method needs a receiver this pass does not have.
func (index macroIndex) resolve(path MacroPath, crate string) (macroTarget, bool) {
	var found macroTarget
	matches := 0
	for _, target := range index[path.Segments[len(path.Segments)-1]] {
		if target.crate != crate || !endsWith(target.names, path.Segments) {
			continue
		}
		if len(path.Segments) == 1 && target.kind != "function" {
			continue
		}
		if matches == 0 || target.key != found.key {
			matches++
		}
		found = target
	}
	return found, matches == 1
}

func endsWith(names, suffix []string) bool {
	if len(suffix) > len(names) {
		return false
	}
	for offset := range suffix {
		if names[len(names)-len(suffix)+offset] != suffix[offset] {
			return false
		}
	}
	return true
}
