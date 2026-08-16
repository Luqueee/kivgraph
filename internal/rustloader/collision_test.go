package rustloader

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Luqueee/kivgraph/internal/rustloader/scipwire"
	"github.com/Luqueee/kivgraph/internal/syntax"
	"github.com/Luqueee/kivgraph/internal/testsupport"
	"github.com/Luqueee/kivgraph/internal/workspace"
)

// collidingIndex builds an index in which two analyzer symbols share one stable
// key. That is not a hypothetical: the key carries the crate name, the
// descriptor path and the kind and deliberately not the crate version, and
// rust-analyzer has bugs that emit one declaration under two versions -- it
// says so itself, in the diagnostics it writes while indexing the standard
// library's `std` crate root.
func collidingIndex(t *testing.T, root string) scipwire.Index {
	t.Helper()
	span := func(line, start, end int32) scipwire.Range {
		return scipwire.Range{StartLine: line, StartCharacter: start, EndLine: line, EndCharacter: end, Present: true}
	}
	body := func(startLine, endLine int32) scipwire.Range {
		return scipwire.Range{StartLine: startLine, EndLine: endLine, EndCharacter: 1, Present: true}
	}
	// Two versions of the same crate path. `later` sorts after `earlier`, so a
	// deterministic reader publishes `earlier` whatever order it met them in.
	earlier := "rust-analyzer cargo demo 0.0.0 lib/Root#"
	later := "rust-analyzer cargo demo 9.9.9 lib/Root#"
	used := "rust-analyzer cargo demo 0.0.0 lib/helper()."
	return scipwire.Index{
		ToolName: "rust-analyzer",
		Documents: []scipwire.Document{
			{
				RelativePath:     "src/lib.rs",
				Language:         "rust",
				PositionEncoding: scipwire.PositionUTF8,
				Occurrences: []scipwire.Occurrence{
					{Symbol: earlier, Roles: scipwire.RoleDefinition, Range: span(0, 11, 15), EnclosingRange: body(0, 6)},
					{Symbol: later, Roles: scipwire.RoleDefinition, Range: span(1, 11, 15), EnclosingRange: body(1, 6)},
					{Symbol: used, Roles: scipwire.RoleDefinition, Range: span(3, 7, 13), EnclosingRange: body(3, 5)},
					// A use inside the body both declarations claim.
					{Symbol: used, Range: span(4, 4, 10)},
				},
				Symbols: []scipwire.SymbolInformation{
					{Symbol: earlier, DisplayName: "Root", Signature: "struct Root"},
					{Symbol: later, DisplayName: "Root", Signature: "struct Root"},
					{Symbol: used, DisplayName: "helper", Signature: "fn helper()"},
				},
			},
		},
	}
}

// TestAnalyzeResolvesAKeyCollisionTheSameWayEveryTime is a regression guard for
// a graph that changed between two passes over one unchanged corpus.
//
// The index of colliding keys was built by walking a map, so iteration order
// decided which declaration owned the key -- and with it whether the uses inside
// its body had a source symbol at all. Indexing the standard library made it
// visible: two passes published a hundred and seventy edges of difference at the
// root of `std`.
func TestAnalyzeResolvesAKeyCollisionTheSameWayEveryTime(t *testing.T) {
	root := testsupport.TempDir(t)
	if err := os.WriteFile(filepath.Join(root, "Cargo.toml"),
		[]byte("[package]\nname = \"demo\"\nversion = \"0.0.0\"\nedition = \"2021\"\n"), 0o600); err != nil {
		t.Fatalf("write manifest: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(root, "src"), 0o700); err != nil {
		t.Fatalf("create src: %v", err)
	}
	source := "pub struct Root {}\npub struct Root2 {}\n\npub fn helper() {\n    helper();\n}\n"
	if err := os.WriteFile(filepath.Join(root, "src", "lib.rs"), []byte(source), 0o600); err != nil {
		t.Fatalf("write source: %v", err)
	}
	repository := workspace.Repository{Name: "demo", Path: root, RealPath: root}
	discovery, err := workspace.DiscoverCargo(context.Background(), repository)
	if err != nil {
		t.Fatalf("DiscoverCargo() error = %v", err)
	}
	parsers, err := syntax.NewParserManager(1)
	if err != nil {
		t.Fatalf("NewParserManager() error = %v", err)
	}
	t.Cleanup(func() { parsers.Close() })

	// Go randomises map iteration, so the old code would disagree with itself
	// across these runs rather than fail every time. The assertion does not
	// depend on that: it names the winner.
	var first string
	for attempt := range 12 {
		analysis, err := Analyze(context.Background(), AnalyzeOptions{
			Repository:   repository,
			Workspace:    discovery.Workspaces[0],
			Crates:       discovery.Crates,
			Index:        collidingIndex(t, root),
			Parsers:      parsers,
			ProcMacros:   true,
			BuildScripts: true,
		})
		if err != nil {
			t.Fatalf("Analyze() error = %v", err)
		}
		// One key is one node. Publishing both declarations asserts that a
		// symbol lives in two files, which the canonical schema rejects when the
		// generation is built -- `Copy exception: Node has more than one
		// neighbour in table DEFINES` -- long after this pass said PASS.
		var winner string
		published := make(map[string]int, len(analysis.Definitions))
		for _, definition := range analysis.Definitions {
			if string(definition.StableKey) == "" {
				t.Fatalf("definition %#v has no key", definition)
			}
			published[string(definition.StableKey)]++
			if definition.QualifiedName != "lib::Root" {
				continue
			}
			if winner != "" {
				t.Fatalf("two declarations published for one key: %q and %q",
					winner, definition.Symbol)
			}
			winner = definition.Symbol
		}
		if winner == "" {
			t.Fatalf("no definition of lib::Root: %#v", analysis.Definitions)
		}
		for key, count := range published {
			if count != 1 {
				t.Fatalf("key %q published %d declarations, want exactly one", key, count)
			}
		}
		// And the winner is the one a sorted reader meets first, not whichever
		// the map handed over.
		if winner != "rust-analyzer cargo demo 0.0.0 lib/Root#" {
			t.Fatalf("winner = %q, want the first by symbol string", winner)
		}
		// The collision is named, never silently resolved.
		named := false
		for _, line := range analysis.Diagnostics {
			if strings.Contains(line, "share one identity") {
				named = true
			}
		}
		if !named {
			t.Fatalf("diagnostics = %#v, want the key collision named", analysis.Diagnostics)
		}
		if attempt == 0 {
			first = winner
			continue
		}
		if winner != first {
			t.Fatalf("attempt %d published %q, attempt 0 published %q: the winner must not depend on map order",
				attempt, winner, first)
		}
	}
}
