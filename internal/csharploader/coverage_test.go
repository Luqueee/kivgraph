package csharploader

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Luqueee/kivgraph/internal/facts"
	"github.com/Luqueee/kivgraph/internal/scip/scipwire"
	"github.com/Luqueee/kivgraph/internal/workspace"
)

const (
	coverageFixture = "../../testdata/csharp/coverage"
	coverageIndex   = "../../testdata/csharp/index/coverage.scip"
)

func coveragePayload(t *testing.T) facts.SemanticPayload {
	t.Helper()
	data, err := os.ReadFile(coverageIndex)
	if err != nil {
		t.Fatalf("read coverage index: %v", err)
	}
	index, err := scipwire.Decode(data)
	if err != nil {
		t.Fatalf("decode coverage index: %v", err)
	}
	root, err := filepath.Abs(coverageFixture)
	if err != nil {
		t.Fatal(err)
	}
	payload, err := Convert(index,
		Options{Repository: workspace.Repository{Name: "coverage", Path: root, RealPath: root}},
		root, filepath.Join(root, "coverage.csproj"))
	if err != nil {
		t.Fatalf("convert: %v", err)
	}
	return payload
}

func names(payload facts.SemanticPayload) map[string]facts.SemanticSymbol {
	byName := map[string]facts.SemanticSymbol{}
	for _, symbol := range payload.Symbols {
		byName[symbol.QualifiedName] = symbol
	}
	return byName
}

func TestCoverageDeclarations(t *testing.T) {
	byName := names(coveragePayload(t))
	for _, qualified := range []string{
		"Coverage.IShape",
		"Coverage.IShape.Area",
		"Coverage.ShapeKind",
		"Coverage.ShapeKind.Circle",
		"Coverage.Point",
		"Coverage.ShapeBase",
		"Coverage.ShapeBase.Origin",
		"Coverage.Circle",
		"Coverage.Circle.radius",
		"Coverage.Catalog",
		"Coverage.Catalog.Total",
		"Coverage.Catalog.Label",
		"Coverage.Catalog.First",
	} {
		if _, present := byName[qualified]; !present {
			t.Errorf("%s is missing from the payload", qualified)
		}
	}
}

// TestCoverageEveryEdgeCarriesACSharpProvenance is the check the whole exercise
// exists for: a language wired everywhere except the provenance table publishes
// a correct-looking graph stamped with another language's name.
func TestCoverageEveryEdgeCarriesACSharpProvenance(t *testing.T) {
	set, err := facts.NormalizeSemantic(t.Context(), repositoryFixture(t), coveragePayload(t))
	if err != nil {
		t.Fatalf("normalize: %v", err)
	}
	if err := set.Validate(); err != nil {
		t.Fatalf("validate: %v", err)
	}
	seen := map[facts.Provenance]int{}
	for _, edge := range set.Edges {
		seen[edge.Provenance]++
		switch edge.Provenance {
		case facts.CSharpScipDefinition, facts.CSharpScipUse, facts.PackageManifest:
		default:
			t.Errorf("a C# edge carries provenance %q", edge.Provenance)
		}
	}
	if seen[facts.CSharpScipDefinition] == 0 || seen[facts.CSharpScipUse] == 0 {
		t.Errorf("provenances = %v, want both CSHARP_SCIP_DEF and CSHARP_SCIP_USE", seen)
	}
}

// TestCoverageBuildOutputIsNotInTheGraph is the filter that matters most here.
// The SDK writes `obj/Debug/net8.0/coverage.GlobalUsings.g.cs` and an assembly
// attributes file into every project it restores, and scip-dotnet indexes them.
// Publishing them puts symbols in the graph that nobody wrote and that vanish
// on `dotnet clean`.
func TestCoverageBuildOutputIsNotInTheGraph(t *testing.T) {
	payload := coveragePayload(t)
	for _, file := range payload.Files {
		if strings.HasPrefix(file.Path, "obj/") || strings.HasPrefix(file.Path, "bin/") {
			t.Errorf("build output entered the graph: %s", file.Path)
		}
	}
	if len(payload.Files) != 2 {
		t.Errorf("files = %d, want the two sources of the fixture", len(payload.Files))
	}
}

// TestCoverageReferencesAreAttributedToADeclaration is what the reconstructed
// enclosing ranges buy. scip-dotnet emits no enclosing_range at all, so without
// the reconstruction every reference in the language would be sourced at its
// file's module symbol and `find_references` would answer with files.
func TestCoverageReferencesAreAttributedToADeclaration(t *testing.T) {
	payload := coveragePayload(t)
	byID := map[string]facts.SemanticSymbol{}
	for _, symbol := range payload.Symbols {
		byID[symbol.ID] = symbol
	}
	total, atModule := 0, 0
	for _, reference := range payload.References {
		total++
		if byID[reference.SourceID].Kind == facts.ModuleSymbolKind {
			atModule++
		}
	}
	if total == 0 {
		t.Fatal("the payload has no references")
	}
	// Some references genuinely belong to the file: a namespace-level using.
	// Most must not.
	if atModule*2 >= total {
		t.Errorf("%d of %d references are sourced at a module symbol, want most of them attributed to a declaration",
			atModule, total)
	}
}

func TestCoverageTypeHierarchy(t *testing.T) {
	payload := coveragePayload(t)
	kinds := map[string]string{}
	for _, reference := range payload.References {
		if reference.Kind == "" || reference.Kind == "REFERENCES" {
			continue
		}
		kinds[shortName(reference.SourceID)+"->"+shortName(reference.TargetID)] = reference.Kind
	}
	// ShapeBase implements IShape and Circle extends ShapeBase. scip-dotnet
	// sets no symbol kind, so the bridge cannot tell an interface from a class
	// and both are EXTENDS -- the weaker claim. See the ADR.
	for _, pair := range []string{"ShapeBase#->IShape#", "Circle#->ShapeBase#"} {
		if _, present := kinds[pair]; !present {
			t.Errorf("%s is missing from the hierarchy", pair)
		}
	}
	if kinds["Circle#Area().->ShapeBase#Area()."] != "OVERRIDES" {
		t.Errorf("Circle.Area does not override ShapeBase.Area: %v", kinds)
	}
}

// TestCoverageUnresolvedDiagnostics is the negative half: the BCL is not in the
// graph and the payload has to say so rather than invent it.
func TestCoverageUnresolvedDiagnostics(t *testing.T) {
	payload := coveragePayload(t)
	found := false
	for _, unresolved := range payload.Unresolved {
		if unresolved.Reason == "" {
			t.Error("an unresolved row carries no reason")
		}
		if strings.Contains(unresolved.Detail, "System/") {
			found = true
		}
	}
	if !found {
		t.Error("no reference into the base class library is reported unresolved")
	}
	for _, symbol := range payload.Symbols {
		if strings.HasPrefix(symbol.QualifiedName, "System.") {
			t.Errorf("a BCL type was published as a local symbol: %s", symbol.QualifiedName)
		}
	}
}

// TestCoverageEncodingIsCodeUnits pins the same measurement Java's fixture
// pins, for the other producer: the accented literal shifts every column after
// it by one between UTF-8 bytes and UTF-16 code units.
func TestCoverageEncodingIsCodeUnits(t *testing.T) {
	source, err := os.ReadFile(filepath.Join(coverageFixture, "Shapes.cs"))
	if err != nil {
		t.Fatal(err)
	}
	payload := coveragePayload(t)
	checked := 0
	for _, reference := range payload.References {
		if reference.File != "Shapes.cs" || reference.End <= reference.Start {
			continue
		}
		if reference.End > len(source) {
			t.Fatalf("reference range %d..%d is past the end of a %d byte file",
				reference.Start, reference.End, len(source))
		}
		text := string(source[reference.Start:reference.End])
		if strings.TrimSpace(text) == "" {
			t.Errorf("reference at %d:%d lands on whitespace %q: the position encoding is being read wrong",
				reference.StartLine, reference.StartColumn, text)
		}
		checked++
	}
	if checked == 0 {
		t.Fatal("no reference in Shapes.cs carried a range to check")
	}
}

func repositoryFixture(t *testing.T) workspace.Repository {
	t.Helper()
	absolute, err := filepath.Abs(coverageFixture)
	if err != nil {
		t.Fatal(err)
	}
	return workspace.Repository{
		Name: "coverage", Path: absolute, RealPath: absolute, Languages: []string{"csharp"},
	}
}

func shortName(symbol string) string {
	if index := strings.LastIndex(symbol, "Coverage/"); index >= 0 {
		return symbol[index+len("Coverage/"):]
	}
	return symbol
}
