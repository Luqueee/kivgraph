package rustloader

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/Luqueee/kivgraph/internal/scip/scipwire"
	"github.com/Luqueee/kivgraph/internal/syntax"
	"github.com/Luqueee/kivgraph/internal/testsupport"
	"github.com/Luqueee/kivgraph/internal/workspace"
)

// A fuzz target is a whole program written inside one macro call: the file
// declares nothing, so no declaration of it contains any of its uses. They
// were counted and dropped, and the file was in no answer about what it uses
// -- an absence the graph asserted without having looked.
//
// The use is published with no source, which is how the file's own scope
// becomes its owner; the use inside a declaration keeps the declaration.
func TestAnalyzeKeepsAUseNoDeclarationOfItsFileContains(t *testing.T) {
	root := testsupport.TempDir(t)
	write := func(name, contents string) {
		t.Helper()
		path := filepath.Join(root, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
			t.Fatalf("create %s: %v", filepath.Dir(path), err)
		}
		if err := os.WriteFile(path, []byte(contents), 0o600); err != nil {
			t.Fatalf("write %s: %v", name, err)
		}
	}
	write("Cargo.toml", "[package]\nname = \"demo\"\nversion = \"0.1.0\"\nedition = \"2021\"\n")
	write("src/lib.rs", "pub fn helper() {}\n\npub fn caller() {\n    helper();\n}\n")
	write("src/bin/target.rs", "use demo::helper;\n\nrun!(|| {\n    helper();\n});\n")

	span := func(line, start, end int32) scipwire.Range {
		return scipwire.Range{StartLine: line, StartCharacter: start, EndLine: line, EndCharacter: end, Present: true}
	}
	body := func(startLine, endLine int32) scipwire.Range {
		return scipwire.Range{StartLine: startLine, EndLine: endLine, EndCharacter: 1, Present: true}
	}
	helper := "rust-analyzer cargo demo 0.1.0 helper()."
	caller := "rust-analyzer cargo demo 0.1.0 caller()."
	index := scipwire.Index{
		ToolName: "rust-analyzer",
		Documents: []scipwire.Document{
			{
				RelativePath: "src/lib.rs", Language: "rust", PositionEncoding: scipwire.PositionUTF8,
				Occurrences: []scipwire.Occurrence{
					{Symbol: helper, Roles: scipwire.RoleDefinition, Range: span(0, 7, 13), EnclosingRange: body(0, 0)},
					{Symbol: caller, Roles: scipwire.RoleDefinition, Range: span(2, 7, 13), EnclosingRange: body(2, 4)},
					{Symbol: helper, Range: span(3, 4, 10)},
				},
				Symbols: []scipwire.SymbolInformation{
					{Symbol: helper, DisplayName: "helper", Signature: "pub fn helper()"},
					{Symbol: caller, DisplayName: "caller", Signature: "pub fn caller()"},
				},
			},
			{
				// The analyzer defines the crate root here and nothing the
				// graph publishes: the two uses have no declaration around them.
				RelativePath: "src/bin/target.rs", Language: "rust", PositionEncoding: scipwire.PositionUTF8,
				Occurrences: []scipwire.Occurrence{
					{Symbol: helper, Range: span(0, 10, 16)},
					{Symbol: helper, Range: span(3, 4, 10)},
				},
			},
		},
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
	analysis, err := Analyze(context.Background(), AnalyzeOptions{
		Repository: repository, Workspace: discovery.Workspaces[0], Crates: discovery.Crates,
		Index: index, Parsers: parsers, ProcMacros: true, BuildScripts: true,
	})
	if err != nil {
		t.Fatalf("Analyze() error = %v", err)
	}

	type use struct {
		file, source string
		line         int
		crate        string
	}
	got := make([]use, 0, len(analysis.References))
	for _, reference := range analysis.References {
		if reference.TargetKey != string(definitionNamed(t, analysis, "helper").StableKey) {
			t.Fatalf("a reference to something else: %#v", reference)
		}
		got = append(got, use{reference.File, reference.SourceKey, reference.StartLine, reference.SourceCrate.Name})
	}
	want := []use{
		{"src/bin/target.rs", "", 1, "demo"},
		{"src/bin/target.rs", "", 4, "demo"},
		{"src/lib.rs", string(definitionNamed(t, analysis, "caller").StableKey), 4, "demo"},
	}
	if len(got) != len(want) {
		t.Fatalf("references = %#v, want %#v", got, want)
	}
	for index := range want {
		if got[index] != want[index] {
			t.Fatalf("reference %d = %#v, want %#v", index, got[index], want[index])
		}
	}
}
