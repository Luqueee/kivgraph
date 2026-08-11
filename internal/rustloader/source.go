package rustloader

import (
	"context"
	"fmt"
	"strings"

	tree_sitter "github.com/tree-sitter/go-tree-sitter"

	"github.com/Luqueee/ladygraph/internal/syntax"
)

// UseKind says how a `use` brings a name into scope, which is the difference
// between an import and a re-export.
type UseKind string

const (
	// UseNone means the occurrence is not inside a use declaration.
	UseNone UseKind = ""
	// UseImport is a private `use`: the name is visible in this module only.
	UseImport UseKind = "import"
	// UseReexport is a `pub use`: the module offers the name to others.
	UseReexport UseKind = "reexport"
)

// ReferenceKind is the syntactic role of one occurrence.
//
// The analyzer says which symbol an occurrence resolves to, and never how it
// is used: `symbol_roles` distinguishes a definition and nothing else, and
// `syntax_kind` is left unset. The shape of the code decides the relation,
// exactly as the Go AST decides GO_AST_CALL over a go/types resolution.
type ReferenceKind string

const (
	ReferenceUse  ReferenceKind = "use"
	ReferenceCall ReferenceKind = "call"
	ReferenceType ReferenceKind = "type"
	// ReferenceCallback is a function named as the argument of a call.
	ReferenceCallback ReferenceKind = "callback"
	// ReferenceAssign is a function bound to a name instead of called.
	ReferenceAssign ReferenceKind = "assign"
	// ReferenceReturn is a function that leaves a body as its result.
	ReferenceReturn ReferenceKind = "return"
)

// Declaration is one item a Rust file declares, as the grammar sees it.
type Declaration struct {
	NodeKind  string
	Name      string
	StartByte int
	EndByte   int
	StartLine int
}

// Source is one parsed Rust file: its bytes, its line offsets and its tree.
type Source struct {
	// Path is the repository relative path of the file.
	Path string
	code []byte
	// lineOffsets[i] is the byte offset where line i starts, zero based.
	lineOffsets []int
	tree        *syntax.SyntaxTree
	root        *tree_sitter.Node
}

// NewSource parses one file. The caller owns the source and must Close it.
func NewSource(ctx context.Context, manager *syntax.ParserManager, path string, code []byte) (*Source, error) {
	if manager == nil {
		return nil, fmt.Errorf("parse %q: no parser manager", path)
	}
	tree, err := manager.Parse(ctx, syntax.LanguageRust, code)
	if err != nil {
		return nil, fmt.Errorf("parse %q: %w", path, err)
	}
	root, err := tree.RootNode()
	if err != nil {
		tree.Close()
		return nil, fmt.Errorf("parse %q: %w", path, err)
	}
	return &Source{Path: path, code: code, lineOffsets: lineOffsets(code), tree: tree, root: root}, nil
}

// Close releases the parsed tree.
func (source *Source) Close() {
	if source == nil || source.tree == nil {
		return
	}
	source.tree.Close()
	source.tree = nil
	source.root = nil
}

// Offset converts a zero based line and byte column into a byte offset.
//
// SCIP positions are line and column pairs; every durable key of an
// observation is built from byte offsets, so the two views must agree exactly.
func (source *Source) Offset(line, column int) (int, bool) {
	if source == nil || line < 0 || line >= len(source.lineOffsets) {
		return 0, false
	}
	offset := source.lineOffsets[line] + column
	if offset < 0 || offset > len(source.code) {
		return 0, false
	}
	return offset, true
}

// Text answers the source excerpt of a byte span.
func (source *Source) Text(start, end int) string {
	if source == nil || start < 0 || end > len(source.code) || start >= end {
		return ""
	}
	return string(source.code[start:end])
}

// Exported reports whether the declaration that owns a span is public.
//
// SCIP carries no visibility, so `pub` is read from the grammar. Two Rust
// rules make that more than a check for the keyword: an item of a trait is as
// visible as the trait, and a method of a trait implementation is reachable
// through the trait wherever the trait is. Neither ever writes `pub`.
func (source *Source) Exported(start, end int) bool {
	node := source.declarationAt(start, end)
	if node == nil {
		return false
	}
	if hasVisibilityModifier(node) {
		return true
	}
	for current := node.Parent(); current != nil; current = current.Parent() {
		switch current.Kind() {
		case "trait_item":
			return hasVisibilityModifier(current)
		case "impl_item":
			return current.ChildByFieldName("trait") != nil
		}
	}
	return false
}

// Use answers how a `use` declaration containing the span brings its name into
// scope.
func (source *Source) Use(start, end int) UseKind {
	node := source.nodeAt(start, end)
	for current := node; current != nil; current = current.Parent() {
		if current.Kind() != "use_declaration" {
			continue
		}
		if hasVisibilityModifier(current) {
			return UseReexport
		}
		return UseImport
	}
	return UseNone
}

// Reference classifies one occurrence by the shape of the code around it.
func (source *Source) Reference(start, end int) ReferenceKind {
	node := source.nodeAt(start, end)
	if node == nil {
		return ReferenceUse
	}
	if isTypePosition(node) {
		return ReferenceType
	}
	if isCallee(node) {
		return ReferenceCall
	}
	return valuePosition(node)
}

// Declarations lists the items the grammar sees, which is what an index is
// compared against when a declaration produced no symbol.
func (source *Source) Declarations() []Declaration {
	if source == nil || source.root == nil {
		return nil
	}
	declarations := make([]Declaration, 0, 16)
	var walk func(node *tree_sitter.Node)
	walk = func(node *tree_sitter.Node) {
		for index := uint(0); index < node.NamedChildCount(); index++ {
			child := node.NamedChild(index)
			if child == nil {
				continue
			}
			if declaredItems[child.Kind()] && !isExternalModule(child) {
				start, end := child.ByteRange()
				declarations = append(declarations, Declaration{
					NodeKind:  child.Kind(),
					Name:      declarationName(child, source.code),
					StartByte: int(start),
					EndByte:   int(end),
					StartLine: int(child.StartPosition().Row),
				})
			}
			walk(child)
		}
	}
	walk(source.root)
	return declarations
}

// declaredItems are the Rust forms that introduce a named item. A `use` is not
// one: it introduces an alias of something declared elsewhere.
var declaredItems = map[string]bool{
	"function_item":           true,
	"struct_item":             true,
	"enum_item":               true,
	"union_item":              true,
	"trait_item":              true,
	"type_item":               true,
	"const_item":              true,
	"static_item":             true,
	"mod_item":                true,
	"macro_definition":        true,
	"function_signature_item": true,
}

// isExternalModule reports a `mod name;` whose body lives in another file.
// The analyzer indexes that module where its source is, so the statement is
// not a declaration this file failed to index.
func isExternalModule(node *tree_sitter.Node) bool {
	return node.Kind() == "mod_item" && node.ChildByFieldName("body") == nil
}

func declarationName(node *tree_sitter.Node, code []byte) string {
	name := node.ChildByFieldName("name")
	if name == nil {
		return ""
	}
	start, end := name.ByteRange()
	if int(end) > len(code) {
		return ""
	}
	return strings.TrimSpace(string(code[start:end]))
}

// declarationAt answers the item that owns a span.
func (source *Source) declarationAt(start, end int) *tree_sitter.Node {
	node := source.nodeAt(start, end)
	for current := node; current != nil; current = current.Parent() {
		kind := current.Kind()
		if declaredItems[kind] || kind == "field_declaration" || kind == "enum_variant" {
			return current
		}
	}
	return nil
}

func (source *Source) nodeAt(start, end int) *tree_sitter.Node {
	if source == nil || source.root == nil {
		return nil
	}
	if start < 0 || end > len(source.code) || start > end {
		return nil
	}
	return source.root.NamedDescendantForByteRange(uint(start), uint(end))
}

func hasVisibilityModifier(node *tree_sitter.Node) bool {
	for index := uint(0); index < node.ChildCount(); index++ {
		child := node.Child(index)
		if child != nil && child.Kind() == "visibility_modifier" {
			return true
		}
	}
	return false
}

// isTypePosition reports whether a node stands where Rust expects a type.
func isTypePosition(node *tree_sitter.Node) bool {
	switch node.Kind() {
	case "type_identifier":
		return true
	}
	parent := node.Parent()
	if parent == nil {
		return false
	}
	switch parent.Kind() {
	case "generic_type", "scoped_type_identifier", "type_arguments", "trait_bounds", "impl_item":
		return true
	}
	return false
}

// isCallee reports whether a node is the function of a call expression.
//
// The path of a call is nested: `support::double(x)` puts the identifier under
// a scoped_identifier, and `value.method()` under a field_expression, so the
// walk climbs while the node keeps being the callee of its parent.
func isCallee(node *tree_sitter.Node) bool {
	current := node
	for depth := 0; current != nil && depth < 4; depth++ {
		parent := current.Parent()
		if parent == nil {
			return false
		}
		if parent.Kind() == "call_expression" {
			function := parent.ChildByFieldName("function")
			return function != nil && function.Equals(*current)
		}
		switch parent.Kind() {
		case "scoped_identifier", "field_expression", "generic_function":
			current = parent
			continue
		}
		return false
	}
	return false
}

// valuePosition reports the role of a node that stands where Rust expects a
// value, which is what separates naming a function from calling it.
//
// Rust writes the same identifier in every one of these places, and only the
// shape around it says whether the function travels as an argument, is bound
// to a name, or leaves the body as a result. The walk climbs the forms that
// wrap a value without changing what it is -- a path, a borrow, an array --
// and stops at the first parent that decides, so `takes(&[f])` is an argument
// and not an assignment.
func valuePosition(node *tree_sitter.Node) ReferenceKind {
	current := node
	for depth := 0; current != nil && depth < 6; depth++ {
		parent := current.Parent()
		if parent == nil {
			return ReferenceUse
		}
		switch parent.Kind() {
		case "arguments":
			if grandparent := parent.Parent(); grandparent != nil && grandparent.Kind() == "call_expression" {
				return ReferenceCallback
			}
			return ReferenceUse
		case "let_declaration", "const_item", "static_item", "field_initializer":
			if value := parent.ChildByFieldName("value"); value != nil && value.Equals(*current) {
				return ReferenceAssign
			}
			return ReferenceUse
		case "return_expression":
			return ReferenceReturn
		case "block":
			if isTailExpression(parent, current) {
				return ReferenceReturn
			}
			return ReferenceUse
		// A field access is not transparent: returning `target.field` returns
		// the field, not the function that owns it. A path, a borrow or a
		// literal container keeps naming the same function.
		case "scoped_identifier", "generic_function", "reference_expression",
			"parenthesized_expression", "array_expression", "tuple_expression":
			current = parent
			continue
		}
		return ReferenceUse
	}
	return ReferenceUse
}

// isTailExpression reports whether a node is the value a block evaluates to,
// and that block is the body of a function. Rust returns the last expression
// of a body without a keyword, so this is the idiomatic half of `return`.
func isTailExpression(block, node *tree_sitter.Node) bool {
	last := block.NamedChild(block.NamedChildCount() - 1)
	if last == nil || !last.Equals(*node) {
		return false
	}
	owner := block.Parent()
	if owner == nil {
		return false
	}
	switch owner.Kind() {
	case "function_item", "closure_expression":
		body := owner.ChildByFieldName("body")
		return body != nil && body.Equals(*block)
	}
	return false
}

func lineOffsets(code []byte) []int {
	offsets := make([]int, 0, 64)
	offsets = append(offsets, 0)
	for index, character := range code {
		if character == '\n' {
			offsets = append(offsets, index+1)
		}
	}
	return offsets
}

// ImplementationHeader is the `impl ... for ...` line of one implementation
// block: the spans where the trait and the self type are written.
type ImplementationHeader struct {
	// TraitStart and TraitEnd are empty for an inherent implementation.
	TraitStart int
	TraitEnd   int
	TypeStart  int
	TypeEnd    int
	BodyStart  int
	BodyEnd    int
}

// Implementations lists the implementation blocks of the file.
func (source *Source) Implementations() []ImplementationHeader {
	if source == nil || source.root == nil {
		return nil
	}
	headers := make([]ImplementationHeader, 0, 4)
	source.walkNodes(func(node *tree_sitter.Node) {
		if node.Kind() != "impl_item" {
			return
		}
		typeNode := node.ChildByFieldName("type")
		if typeNode == nil {
			return
		}
		header := ImplementationHeader{}
		start, end := typeNode.ByteRange()
		header.TypeStart, header.TypeEnd = int(start), int(end)
		if traitNode := node.ChildByFieldName("trait"); traitNode != nil {
			traitStart, traitEnd := traitNode.ByteRange()
			header.TraitStart, header.TraitEnd = int(traitStart), int(traitEnd)
		}
		if body := node.ChildByFieldName("body"); body != nil {
			bodyStart, bodyEnd := body.ByteRange()
			header.BodyStart, header.BodyEnd = int(bodyStart), int(bodyEnd)
		} else {
			blockStart, blockEnd := node.ByteRange()
			header.BodyStart, header.BodyEnd = int(blockStart), int(blockEnd)
		}
		headers = append(headers, header)
	})
	return headers
}

// TraitBound is one supertrait span of a trait declaration.
type TraitBound struct {
	NameStart int
	NameEnd   int
	// TraitStart and TraitEnd are the name span of the declaring trait.
	TraitStart int
	TraitEnd   int
}

// TraitBounds lists the supertraits every trait of the file declares.
func (source *Source) TraitBounds() []TraitBound {
	if source == nil || source.root == nil {
		return nil
	}
	bounds := make([]TraitBound, 0, 4)
	source.walkNodes(func(node *tree_sitter.Node) {
		if node.Kind() != "trait_item" {
			return
		}
		name := node.ChildByFieldName("name")
		list := node.ChildByFieldName("bounds")
		if name == nil || list == nil {
			return
		}
		nameStart, nameEnd := name.ByteRange()
		for index := uint(0); index < list.NamedChildCount(); index++ {
			bound := list.NamedChild(index)
			if bound == nil {
				continue
			}
			boundStart, boundEnd := bound.ByteRange()
			bounds = append(bounds, TraitBound{
				NameStart:  int(boundStart),
				NameEnd:    int(boundEnd),
				TraitStart: int(nameStart),
				TraitEnd:   int(nameEnd),
			})
		}
	})
	return bounds
}

func (source *Source) walkNodes(visit func(*tree_sitter.Node)) {
	var walk func(node *tree_sitter.Node)
	walk = func(node *tree_sitter.Node) {
		for index := uint(0); index < node.NamedChildCount(); index++ {
			child := node.NamedChild(index)
			if child == nil {
				continue
			}
			visit(child)
			walk(child)
		}
	}
	walk(source.root)
}
