package syntax

import (
	"errors"
	"fmt"
	"sort"
	"strings"

	tree_sitter "github.com/tree-sitter/go-tree-sitter"
)

// CandidateKind is a syntactic category, not semantic evidence.
type CandidateKind string

const (
	CandidateDeclaration CandidateKind = "DECLARATION"
	CandidateImport      CandidateKind = "IMPORT"
	CandidateExport      CandidateKind = "EXPORT"
	CandidateCall        CandidateKind = "CALL"
	CandidateIdentifier  CandidateKind = "IDENTIFIER"
	CandidateClass       CandidateKind = "CLASS"
	CandidateInterface   CandidateKind = "INTERFACE"
	CandidateMethod      CandidateKind = "METHOD"
)

// SyntaxCandidate records one syntax node classified as a candidate. It never
// represents an exact symbol, provider, or semantic edge.
type SyntaxCandidate struct {
	Kind       CandidateKind
	Name       string
	NodeKind   string
	Signature  string
	StartByte  uint
	EndByte    uint
	StartPoint InputPoint
	EndPoint   InputPoint
}

// SyntaxInventory is the deterministic syntactic output for one source tree.
type SyntaxInventory struct {
	Language   Language
	HasErrors  bool
	Candidates []SyntaxCandidate
}

// List returns a copy of the inventory candidates.
func (inventory SyntaxInventory) List() []SyntaxCandidate {
	return append([]SyntaxCandidate(nil), inventory.Candidates...)
}

// BuildInventory extracts syntactic candidates from a parsed source tree.
// Candidate classifications are intentionally independent of semantic APIs.
func BuildInventory(tree *SyntaxTree, source []byte) (SyntaxInventory, error) {
	if tree == nil {
		return SyntaxInventory{}, errors.New("syntax tree must not be nil")
	}
	root, err := tree.RootNode()
	if err != nil {
		return SyntaxInventory{}, err
	}
	if uint(len(source)) < root.EndByte() {
		return SyntaxInventory{}, fmt.Errorf("source is shorter than tree range: source=%d tree_end=%d", len(source), root.EndByte())
	}
	hasErrors := root.HasError()
	candidates := make([]SyntaxCandidate, 0)
	stack := []*tree_sitter.Node{root}
	for len(stack) > 0 {
		last := len(stack) - 1
		node := stack[last]
		stack = stack[:last]
		for _, kind := range classifyNode(node.Kind()) {
			candidates = append(candidates, makeCandidate(kind, node, source))
		}
		for index := int(node.NamedChildCount()) - 1; index >= 0; index-- {
			child := node.NamedChild(uint(index))
			if child != nil {
				stack = append(stack, child)
			}
		}
	}
	return SyntaxInventory{Language: tree.Language(), HasErrors: hasErrors, Candidates: candidates}, nil
}

func classifyNode(nodeKind string) []CandidateKind {
	kind := strings.ToLower(strings.TrimSpace(nodeKind))
	if kind == "" {
		return nil
	}
	kinds := make([]CandidateKind, 0, 2)
	appendKind := func(candidate CandidateKind) {
		for _, existing := range kinds {
			if existing == candidate {
				return
			}
		}
		kinds = append(kinds, candidate)
	}
	if isDeclarationNode(kind) {
		appendKind(CandidateDeclaration)
	}
	if strings.Contains(kind, "import") || kind == "require_call" || isRustImportNode(kind) {
		appendKind(CandidateImport)
	}
	if strings.Contains(kind, "export") {
		appendKind(CandidateExport)
	}
	if strings.Contains(kind, "call") || strings.Contains(kind, "invocation") {
		appendKind(CandidateCall)
	}
	if isIdentifierNode(kind) {
		appendKind(CandidateIdentifier)
	}
	if strings.Contains(kind, "class") {
		appendKind(CandidateClass)
	}
	if strings.Contains(kind, "interface") {
		appendKind(CandidateInterface)
	}
	if strings.Contains(kind, "method") {
		appendKind(CandidateMethod)
	}
	return kinds
}

func isDeclarationNode(kind string) bool {
	if strings.HasSuffix(kind, "_declaration") || strings.HasSuffix(kind, "_definition") || strings.HasSuffix(kind, "_item") {
		return true
	}
	switch kind {
	case "declaration", "declarations", "lexical_declaration", "variable_declaration", "type_declaration", "const_declaration", "var_declaration", "short_var_declaration", "function_declaration", "function_definition", "method_declaration", "method_definition":
		return true
	default:
		return false
	}
}

// isRustImportNode names the Rust forms that bring a path into scope. Rust
// spells an import `use`, so the substring rule the other grammars rely on
// never matches one.
func isRustImportNode(kind string) bool {
	switch kind {
	case "use_declaration", "extern_crate_declaration":
		return true
	default:
		return false
	}
}

func isIdentifierNode(kind string) bool {
	if kind == "identifier" || strings.HasSuffix(kind, "_identifier") {
		return true
	}
	switch kind {
	case "type_identifier", "property_identifier", "field_identifier", "namespace_identifier", "shorthand_property_identifier", "shorthand_property_identifier_pattern":
		return true
	default:
		return false
	}
}

func makeCandidate(kind CandidateKind, node *tree_sitter.Node, source []byte) SyntaxCandidate {
	start, end := node.ByteRange()
	return SyntaxCandidate{
		Kind:       kind,
		Name:       candidateName(node, source),
		NodeKind:   node.Kind(),
		Signature:  candidateSignature(node, source),
		StartByte:  start,
		EndByte:    end,
		StartPoint: pointFromTreeSitter(node.StartPosition()),
		EndPoint:   pointFromTreeSitter(node.EndPosition()),
	}
}

func candidateName(node *tree_sitter.Node, source []byte) string {
	for _, field := range []string{"name", "function", "constructor", "alias", "property", "left", "declarator"} {
		child := node.ChildByFieldName(field)
		if child == nil || child.EndByte() > uint(len(source)) {
			continue
		}
		if text := strings.TrimSpace(child.Utf8Text(source)); text != "" {
			if field == "declarator" {
				if nested := candidateName(child, source); nested != text {
					return nested
				}
			}
			return text
		}
	}
	if isDeclarationNode(strings.ToLower(node.Kind())) {
		for index := uint(0); index < node.NamedChildCount(); index++ {
			child := node.NamedChild(index)
			if child == nil || child.EndByte() > uint(len(source)) {
				continue
			}
			childKind := strings.ToLower(child.Kind())
			if !strings.Contains(childKind, "declarator") && !strings.HasSuffix(childKind, "_spec") {
				continue
			}
			if nested := candidateName(child, source); nested != "" {
				return nested
			}
		}
	}
	if node.EndByte() <= uint(len(source)) {
		return strings.TrimSpace(node.Utf8Text(source))
	}
	return ""
}

func candidateSignature(node *tree_sitter.Node, source []byte) string {
	if node.EndByte() > uint(len(source)) {
		return ""
	}
	start, end := node.ByteRange()
	signatureEnd := end
	body := node.ChildByFieldName("body")
	if body != nil && body.StartByte() >= start {
		signatureEnd = body.StartByte()
	}
	text := strings.TrimSpace(string(source[start:signatureEnd]))
	if body == nil {
		if arrow := strings.Index(text, "=>"); arrow >= 0 {
			text = strings.TrimSpace(text[:arrow])
		}
	}
	return strings.Join(strings.Fields(text), " ")
}

func pointFromTreeSitter(point tree_sitter.Point) InputPoint {
	return InputPoint{Row: point.Row, Column: point.Column}
}

// SortCandidates returns a stable source-order copy for callers that combine
// inventories from multiple trees.
func SortCandidates(candidates []SyntaxCandidate) []SyntaxCandidate {
	result := append([]SyntaxCandidate(nil), candidates...)
	sort.SliceStable(result, func(left, right int) bool {
		if result[left].StartByte != result[right].StartByte {
			return result[left].StartByte < result[right].StartByte
		}
		if result[left].EndByte != result[right].EndByte {
			return result[left].EndByte < result[right].EndByte
		}
		if result[left].Kind != result[right].Kind {
			return result[left].Kind < result[right].Kind
		}
		return result[left].NodeKind < result[right].NodeKind
	})
	return result
}
