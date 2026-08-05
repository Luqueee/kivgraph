package syntax

import (
	"context"
	"testing"
)

func TestBuildInventoryExtractsAllInitialCandidateCategories(t *testing.T) {
	manager, err := NewParserManager(1)
	if err != nil {
		t.Fatalf("NewParserManager() error = %v", err)
	}
	defer manager.Close()

	source := []byte(`import { helper as alias } from "./dep";
export class Parser {
  method(value: number) { return helper(value); }
}
interface Config { value: number }
const answer = helper(1);
`)
	tree, err := manager.Parse(context.Background(), LanguageTypeScript, source)
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	defer tree.Close()

	inventory, err := BuildInventory(tree, source)
	if err != nil {
		t.Fatalf("BuildInventory() error = %v", err)
	}
	if inventory.HasErrors {
		t.Fatal("valid TypeScript source produced syntax errors")
	}
	seen := make(map[CandidateKind]bool)
	for _, candidate := range inventory.List() {
		seen[candidate.Kind] = true
		if candidate.EndByte <= candidate.StartByte || candidate.NodeKind == "" {
			t.Fatalf("invalid candidate range: %#v", candidate)
		}
		if candidate.EndByte > uint(len(source)) {
			t.Fatalf("candidate exceeds source: %#v", candidate)
		}
	}
	for _, kind := range []CandidateKind{
		CandidateDeclaration,
		CandidateImport,
		CandidateExport,
		CandidateCall,
		CandidateIdentifier,
		CandidateClass,
		CandidateInterface,
		CandidateMethod,
	} {
		if !seen[kind] {
			t.Fatalf("inventory did not contain %s: %#v", kind, inventory.List())
		}
	}
}

func TestBuildInventorySupportsGoAndJavaScript(t *testing.T) {
	manager, err := NewParserManager(2)
	if err != nil {
		t.Fatalf("NewParserManager() error = %v", err)
	}
	defer manager.Close()

	sources := map[Language][]byte{
		LanguageGo:         []byte("package example\n\nimport \"fmt\"\ntype Config struct { Value int }\nfunc (c Config) Method() { fmt.Println(c.Value) }\n"),
		LanguageJavaScript: []byte("import value from './value.js'; export function run() { return value(); }\n"),
	}
	for language, source := range sources {
		tree, err := manager.Parse(context.Background(), language, source)
		if err != nil {
			t.Fatalf("Parse(%s) error = %v", language, err)
		}
		inventory, err := BuildInventory(tree, source)
		tree.Close()
		if err != nil {
			t.Fatalf("BuildInventory(%s) error = %v", language, err)
		}
		if inventory.Language != language || len(inventory.Candidates) == 0 || inventory.HasErrors {
			t.Fatalf("inventory(%s) = %#v", language, inventory)
		}
	}
}

func TestBuildInventoryRejectsClosedOrMismatchedSource(t *testing.T) {
	manager, err := NewParserManager(1)
	if err != nil {
		t.Fatalf("NewParserManager() error = %v", err)
	}
	defer manager.Close()

	source := []byte("const value = 1;")
	tree, err := manager.Parse(context.Background(), LanguageJavaScript, source)
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	if _, err := BuildInventory(tree, source[:4]); err == nil {
		t.Fatal("BuildInventory() accepted a source shorter than the tree")
	}
	tree.Close()
	if _, err := BuildInventory(tree, source); err == nil {
		t.Fatal("BuildInventory() accepted a closed tree")
	}
	if _, err := BuildInventory(nil, source); err == nil {
		t.Fatal("BuildInventory() accepted a nil tree")
	}
}

func TestSortCandidatesReturnsIndependentStableSourceOrder(t *testing.T) {
	input := []SyntaxCandidate{
		{Kind: CandidateIdentifier, NodeKind: "identifier", StartByte: 10, EndByte: 12},
		{Kind: CandidateClass, NodeKind: "class", StartByte: 2, EndByte: 8},
		{Kind: CandidateDeclaration, NodeKind: "declaration", StartByte: 2, EndByte: 8},
	}
	ordered := SortCandidates(input)
	if ordered[0].StartByte != 2 || ordered[1].StartByte != 2 || ordered[2].StartByte != 10 {
		t.Fatalf("SortCandidates() = %#v", ordered)
	}
	ordered[0].NodeKind = "mutated"
	if input[1].NodeKind == "mutated" {
		t.Fatal("SortCandidates() returned the input slice")
	}
}
