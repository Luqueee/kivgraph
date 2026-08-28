package javaloader

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
	coverageFixture = "../../testdata/java/coverage"
	coverageIndex   = "../../testdata/java/index/coverage.scip"
)

// coveragePayload is the capability matrix testdata/semantic-coverage names for
// Java. Every capability listed there has an assertion below; a capability with
// no assertion is a claim, not coverage.
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
	payload, err := Convert(index, Options{
		Repository: workspace.Repository{Name: "coverage", Path: root, RealPath: root},
	}, root)
	if err != nil {
		t.Fatalf("convert: %v", err)
	}
	return payload
}

func TestCoverageDeclarations(t *testing.T) {
	payload := coveragePayload(t)
	kinds := map[string]string{}
	for _, symbol := range payload.Symbols {
		kinds[symbol.QualifiedName] = symbol.Kind
	}

	for _, want := range []struct{ qualified, kind string }{
		// files, packages, classes, interfaces
		{"com.example.coverage.Shapes", "interface"},
		{"com.example.coverage.Catalog", "class"},
		// inheritance and abstract types
		{"com.example.coverage.Shapes.Base", "class"},
		{"com.example.coverage.Shapes.Circle", "class"},
		// enums and enum members
		{"com.example.coverage.Shapes.Kind", "enum"},
		// records. scip-java classifies a record as unspecified, so the kind
		// falls back to what the descriptor says. `type` is the honest answer:
		// calling it a class would be Kivgraph's inference, not a fact the
		// producer stated.
		{"com.example.coverage.Shapes.Point", "type"},
		// methods, constructors, fields
		{"com.example.coverage.Shapes.area", "method"},
		{"com.example.coverage.Catalog.total", "method"},
		{"com.example.coverage.Catalog.entries", "field"},
		{"com.example.coverage.Catalog.LABEL", "field"},
	} {
		kind, present := kinds[want.qualified]
		if !present {
			t.Errorf("%s is missing from the payload", want.qualified)
			continue
		}
		if kind != want.kind {
			t.Errorf("%s is %q, want %q", want.qualified, kind, want.kind)
		}
	}
}

// TestCoverageOverloadsAreDistinctSymbols is the capability that would silently
// collapse: `add(Shapes)` and `add(Point, double)` differ only by their
// disambiguator, and a qualified name that dropped it would give both one key.
func TestCoverageOverloadsAreDistinctSymbols(t *testing.T) {
	payload := coveragePayload(t)
	var adds []facts.SemanticSymbol
	for _, symbol := range payload.Symbols {
		if strings.HasPrefix(symbol.QualifiedName, "com.example.coverage.Catalog.add") {
			adds = append(adds, symbol)
		}
	}
	if len(adds) != 2 {
		t.Fatalf("found %d `add` declarations, want the two overloads", len(adds))
	}
	if adds[0].QualifiedName == adds[1].QualifiedName {
		t.Errorf("both overloads share the qualified name %q", adds[0].QualifiedName)
	}
	if adds[0].Signature == adds[1].Signature {
		t.Errorf("both overloads share the signature %q", adds[0].Signature)
	}
}

// TestCoverageCallsInsideALambdaHaveASource is the case the module symbol
// exists for. A call inside `entries.forEach(shape -> ...)` sits in no
// declaration of its own, and a reference with no source is dropped by the
// normaliser rather than reported.
func TestCoverageCallsInsideALambdaHaveASource(t *testing.T) {
	payload := coveragePayload(t)
	describe := ""
	for _, symbol := range payload.Symbols {
		if symbol.QualifiedName == "com.example.coverage.Catalog.describe" {
			describe = symbol.ID
		}
	}
	if describe == "" {
		t.Fatal("Catalog.describe is missing")
	}
	for _, reference := range payload.References {
		if reference.SourceID == "" {
			t.Fatalf("a reference in %s has no source", reference.File)
		}
	}
	found := false
	for _, reference := range payload.References {
		if reference.SourceID == describe && strings.Contains(reference.TargetID, "LABEL") {
			found = true
		}
	}
	if !found {
		t.Error("the use of LABEL inside the lambda is not attributed to describe")
	}
}

func TestCoverageCrossFileReferencesResolve(t *testing.T) {
	payload := coveragePayload(t)
	// Catalog names Shapes, which is declared in the other file. If the
	// declaration table were built per document this would be unresolved.
	for _, unresolved := range payload.Unresolved {
		if unresolved.Detail == "semanticdb maven maven/com.example/coverage 1.0.0 com/example/coverage/Shapes#" {
			t.Fatalf("a same-package declaration was reported unresolved: %+v", unresolved)
		}
		// Whatever else is unresolved inside this package is a declaration
		// nobody observed, never an import: the package is one this payload
		// declares in.
		if unresolved.RequestedPackage == "maven/com.example/coverage" &&
			unresolved.Reason != "DEFINITION_NOT_INDEXED" {
			t.Errorf("same-package %q is reported as %q", unresolved.RequestedSymbol, unresolved.Reason)
		}
	}
	found := false
	for _, reference := range payload.References {
		if strings.Contains(reference.SourceID, "Catalog#") &&
			strings.Contains(reference.TargetID, "coverage/Shapes#") {
			found = true
		}
	}
	if !found {
		t.Error("Catalog does not reference Shapes across the file boundary")
	}
}

// TestCoverageUnresolvedDiagnostics is the negative half of the contract: the
// JDK is not in the graph, and the payload has to say so rather than invent it.
func TestCoverageUnresolvedDiagnostics(t *testing.T) {
	payload := coveragePayload(t)
	wanted := []string{"java/util/List#", "java/lang/Math#"}
	for _, want := range wanted {
		found := false
		for _, unresolved := range payload.Unresolved {
			if strings.Contains(unresolved.Detail, want) {
				found = true
			}
		}
		if !found {
			t.Errorf("%s is not reported unresolved", want)
		}
	}
	for _, unresolved := range payload.Unresolved {
		if unresolved.Reason == "" {
			t.Error("an unresolved row carries no reason")
		}
	}
}

// TestCoverageTypeHierarchy is the capability SCIP relationships buy, and the
// one a caller asking "what breaks if I change this interface" depends on.
func TestCoverageTypeHierarchy(t *testing.T) {
	payload := coveragePayload(t)
	kinds := map[string]string{}
	for _, reference := range payload.References {
		if reference.Kind == "" || reference.Kind == "REFERENCES" {
			continue
		}
		kinds[shortName(reference.SourceID)+"->"+shortName(reference.TargetID)] = reference.Kind
	}
	for pair, want := range map[string]string{
		"Shapes#Base#->Shapes#":                      "IMPLEMENTS",
		"Shapes#Circle#->Shapes#Base#":               "EXTENDS",
		"Shapes#Circle#kind().->Shapes#Base#kind().": "OVERRIDES",
	} {
		got, present := kinds[pair]
		if !present {
			t.Errorf("%s is missing from the hierarchy", pair)
			continue
		}
		if got != want {
			t.Errorf("%s is %s, want %s", pair, got, want)
		}
	}
}

func shortName(symbol string) string {
	if index := strings.LastIndex(symbol, "com/example/coverage/"); index >= 0 {
		return symbol[index+len("com/example/coverage/"):]
	}
	return symbol
}

func TestCoverageNormalizesAndValidates(t *testing.T) {
	root, err := filepath.Abs(coverageFixture)
	if err != nil {
		t.Fatal(err)
	}
	set, err := facts.NormalizeSemantic(t.Context(),
		workspace.Repository{Name: "coverage", Path: root, RealPath: root, Languages: []string{"java"}},
		coveragePayload(t))
	if err != nil {
		t.Fatalf("normalize: %v", err)
	}
	if err := set.Validate(); err != nil {
		t.Fatalf("validate: %v", err)
	}
	for _, edge := range set.Edges {
		switch edge.Provenance {
		case facts.JavaScipDefinition, facts.JavaScipUse, facts.PackageManifest:
		default:
			t.Fatalf("edge %s carries provenance %q", edge.Kind, edge.Provenance)
		}
	}
}
