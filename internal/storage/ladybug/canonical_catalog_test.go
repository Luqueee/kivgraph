package ladybug

import (
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"testing"
)

// TestCanonicalCatalogsCoverEveryDeclaredValue keeps the hand written catalogs
// of canonical_integrity.go in step with the vocabulary of the facts package.
//
// This test carries no build tag on purpose. The same check exists next to the
// native integrity queries, but that file only compiles with `ladybug` and a
// checkout without the native library never runs it: a provenance added for
// Rust reached a published bundle, where the integrity stage rejected 36 edges
// as `unknown_confidence`. A catalog that only fails at the end of a release
// is a catalog nobody checks.
func TestCanonicalCatalogsCoverEveryDeclaredValue(t *testing.T) {
	declared := declaredFactsConstants(t)

	confidences := make([]string, 0, len(canonicalConfidenceValues))
	for _, value := range canonicalConfidenceValues {
		confidences = append(confidences, string(value))
	}
	provenances := make([]string, 0, len(canonicalProvenanceValues))
	for _, value := range canonicalProvenanceValues {
		provenances = append(provenances, string(value))
	}
	for name, catalog := range map[string][]string{
		"Confidence": confidences,
		"Provenance": provenances,
	} {
		known := make(map[string]struct{}, len(catalog))
		for _, value := range catalog {
			known[value] = struct{}{}
		}
		for _, value := range declared[name] {
			if _, exists := known[value]; !exists {
				t.Errorf("%s(%q) is declared in facts.go and missing from the canonical catalog: the integrity stage would reject every edge that carries it",
					name, value)
			}
		}
		if len(catalog) != len(declared[name]) {
			t.Errorf("facts.go declares %d %s constants and the canonical catalog has %d",
				len(declared[name]), name, len(catalog))
		}
	}
}

// TestEveryCanonicalProvenanceIsAcceptedByTheValidators is the other half: the
// two validators that gate a write must accept exactly what the catalog holds.
func TestEveryCanonicalProvenanceIsAcceptedByTheValidators(t *testing.T) {
	for _, value := range canonicalProvenanceValues {
		if !isValidProvenance(string(value)) {
			t.Errorf("isValidProvenance(%q) = false for a catalogued value", value)
		}
	}
	for _, value := range canonicalConfidenceValues {
		if !isValidConfidence(string(value)) {
			t.Errorf("isValidConfidence(%q) = false for a catalogued value", value)
		}
	}
	if isValidProvenance("RUST_SOMETHING_INVENTED") || isValidConfidence("ALMOST_EXACT") {
		t.Error("a value outside the catalog must not validate")
	}
}

// declaredFactsConstants reads the vocabulary from its source. Parsing it is
// what makes this test fail when a constant is added without a catalog entry,
// instead of when a release is built.
func declaredFactsConstants(t *testing.T) map[string][]string {
	t.Helper()
	path := filepath.Join("..", "..", "facts", "facts.go")
	file, err := parser.ParseFile(token.NewFileSet(), path, nil, 0)
	if err != nil {
		t.Fatalf("parse %q: %v", path, err)
	}
	declared := make(map[string][]string)
	for _, declaration := range file.Decls {
		general, ok := declaration.(*ast.GenDecl)
		if !ok || general.Tok != token.CONST {
			continue
		}
		typeName := ""
		for _, spec := range general.Specs {
			value, ok := spec.(*ast.ValueSpec)
			if !ok {
				continue
			}
			if identifier, ok := value.Type.(*ast.Ident); ok {
				typeName = identifier.Name
			}
			if typeName != "Confidence" && typeName != "Provenance" {
				continue
			}
			for _, item := range value.Values {
				literal, ok := item.(*ast.BasicLit)
				if !ok || literal.Kind != token.STRING {
					continue
				}
				declared[typeName] = append(declared[typeName], literal.Value[1:len(literal.Value)-1])
			}
		}
	}
	if len(declared["Confidence"]) == 0 || len(declared["Provenance"]) == 0 {
		t.Fatal("no vocabulary found in facts.go: the parser lost track of the constants")
	}
	return declared
}
