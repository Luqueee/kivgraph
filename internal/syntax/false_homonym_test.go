package syntax

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestFalseHomonymFixtureProducesCandidatesWithoutSemanticEdges(t *testing.T) {
	manager, err := NewParserManager(1)
	if err != nil {
		t.Fatalf("NewParserManager() error = %v", err)
	}
	defer manager.Close()

	fixtureRoot := filepath.Join("..", "..", "testdata", "syntax", "false-homonym")
	inventories := make(map[string]SyntaxInventory, 2)
	for _, name := range []string{"left.ts", "right.ts"} {
		source, err := os.ReadFile(filepath.Join(fixtureRoot, name))
		if err != nil {
			t.Fatalf("ReadFile(%s) error = %v", name, err)
		}
		tree, err := manager.Parse(context.Background(), LanguageTypeScript, source)
		if err != nil {
			t.Fatalf("Parse(%s) error = %v", name, err)
		}
		inventory, err := BuildInventory(tree, source)
		tree.Close()
		if err != nil {
			t.Fatalf("BuildInventory(%s) error = %v", name, err)
		}
		if inventory.HasErrors {
			t.Fatalf("fixture %s has syntax errors", name)
		}
		inventories[name] = inventory
	}

	for _, name := range []string{"left.ts", "right.ts"} {
		if !hasNamedCandidate(inventories[name], CandidateDeclaration, "parse") {
			t.Fatalf("fixture %s has no parse declaration: %#v", name, inventories[name].List())
		}
	}
	if !hasCallContaining(inventories["right.ts"], "parse") {
		t.Fatalf("fixture right.ts has no parse call: %#v", inventories["right.ts"].List())
	}
	if hasNamedCandidate(inventories["right.ts"], CandidateImport, "parse") {
		t.Fatal("fixture unexpectedly contains a syntactic import for parse")
	}

	// BuildInventory returns candidates only; semantic edges are not part of
	// the Tree-sitter output and cannot be inferred from the homonymous names.
}

func hasNamedCandidate(inventory SyntaxInventory, kind CandidateKind, name string) bool {
	for _, candidate := range inventory.Candidates {
		if candidate.Kind == kind && candidate.Name == name {
			return true
		}
	}
	return false
}

func hasCallContaining(inventory SyntaxInventory, name string) bool {
	for _, candidate := range inventory.Candidates {
		if candidate.Kind == CandidateCall && strings.Contains(candidate.Name, name) {
			return true
		}
	}
	return false
}
