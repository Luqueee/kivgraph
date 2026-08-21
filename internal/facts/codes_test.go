package facts

import (
	"errors"
	"go/ast"
	"go/parser"
	"go/token"
	"testing"
)

// goldenCodes is the frozen numbering. A snapshot on disk or in another
// process is only readable by code that agrees with this table, so a change
// here is a format break: this test exists to make that change deliberate,
// never incidental.
var goldenCodes = struct {
	kinds       map[EdgeKind]uint8
	confidences map[Confidence]uint8
	provenances map[Provenance]uint8
}{
	kinds: map[EdgeKind]uint8{
		"CONTAINS_PACKAGE": 1, "CONTAINS_FILE": 2, "DEFINES": 3,
		"PACKAGE_DEPENDS_ON": 4, "MODULE_DEPENDS_ON": 5,
		"IMPORTS_SYMBOL": 6, "EXPORTS": 7, "REEXPORTS": 8,
		"REFERENCES": 9, "CALLS_DIRECT": 10, "PASSES_AS_CALLBACK": 11,
		"ASSIGNS_FUNCTION": 12, "RETURNS_FUNCTION": 13,
		"TYPE_USES": 14, "IMPLEMENTS": 15, "EXTENDS": 16, "EMBEDS": 17,
		"OVERRIDES": 18, "PART_OF": 19,
	},
	confidences: map[Confidence]uint8{
		"EXACT_TYPECHECKED": 1, "EXACT_DECLARATION_MAPPED": 2,
		"EXACT_PACKAGE_MAPPED": 3, "STRUCTURAL_CERTAIN": 4,
		"CANDIDATE": 5, "UNRESOLVED": 6,
	},
	provenances: map[Provenance]uint8{
		"TYPESCRIPT_CHECKER": 1, "TYPESCRIPT_MODULE_RESOLUTION": 2,
		"TYPESCRIPT_DECLARATION_MAP": 3, "TYPESCRIPT_PROJECT_REFERENCE": 4,
		"GO_TYPES_DEF": 5, "GO_TYPES_USE": 6, "GO_TYPES_SELECTION": 7,
		"GO_AST_CALL": 8, "GO_AST_CALLBACK": 9, "GO_OBJECT_PATH": 10,
		"TREE_SITTER_SYNTAX": 11, "PACKAGE_MANIFEST": 12,
		// Appended when Rust landed. The numbers above never move.
		"RUST_ANALYZER_DEF": 13, "RUST_ANALYZER_USE": 14,
		"RUST_ANALYZER_MONIKER": 15, "RUST_SYNTAX_CALL": 16,
		"RUST_SYNTAX_TYPE": 17, "RUST_SYNTAX_IMPL": 18,
		// Appended when function values landed. Never inserted above.
		"RUST_SYNTAX_CALLBACK": 19,
		"PYTHON_INDEXER_DEF":   20, "PYTHON_INDEXER_USE": 21,
		"PYTHON_SYNTAX_CALL": 22, "DART_ANALYZER_DEF": 23,
		"DART_ANALYZER_USE": 24, "DART_SYNTAX_CALL": 25,
	},
}

func TestCodesMatchTheFrozenNumbering(t *testing.T) {
	for kind, want := range goldenCodes.kinds {
		got, err := kind.Code()
		if err != nil {
			t.Errorf("EdgeKind(%q).Code() error = %v", kind, err)
			continue
		}
		if got != want {
			t.Errorf("EdgeKind(%q).Code() = %d, want the frozen %d: renumbering breaks every stored snapshot", kind, got, want)
		}
	}
	for confidence, want := range goldenCodes.confidences {
		got, err := confidence.Code()
		if err != nil {
			t.Errorf("Confidence(%q).Code() error = %v", confidence, err)
			continue
		}
		if got != want {
			t.Errorf("Confidence(%q).Code() = %d, want the frozen %d", confidence, got, want)
		}
	}
	for provenance, want := range goldenCodes.provenances {
		got, err := provenance.Code()
		if err != nil {
			t.Errorf("Provenance(%q).Code() error = %v", provenance, err)
			continue
		}
		if got != want {
			t.Errorf("Provenance(%q).Code() = %d, want the frozen %d", provenance, got, want)
		}
	}
}

func TestEveryDeclaredConstantHasACode(t *testing.T) {
	// The vocabulary lives in facts.go as typed constants. Parsing it is what
	// makes this test fail when a constant is added without a code, instead of
	// silently encoding it as unset.
	declared := declaredConstants(t, "facts.go")

	for _, name := range []string{"EdgeKind", "Confidence", "Provenance"} {
		values := declared[name]
		if len(values) == 0 {
			t.Fatalf("no %s constants found in facts.go: the parser lost track of the vocabulary", name)
		}
		var golden int
		switch name {
		case "EdgeKind":
			golden = len(goldenCodes.kinds)
		case "Confidence":
			golden = len(goldenCodes.confidences)
		case "Provenance":
			golden = len(goldenCodes.provenances)
		}
		if len(values) != golden {
			t.Errorf("facts.go declares %d %s constants but the frozen table has %d: a new constant needs a code appended, never inserted", len(values), name, golden)
		}
		for _, value := range values {
			if err := codeOf(name, value); err != nil {
				t.Errorf("%s(%q) has no code: %v", name, value, err)
			}
		}
	}
}

func TestDecodeRejectsUnsetAndUnknownCodes(t *testing.T) {
	// Zero is reserved: an unset PackedEdge field must never read as a real
	// value, which is the whole reason the numbering starts at one.
	for _, code := range []uint8{0, 200} {
		if kind, err := EdgeKindFromCode(code); !errors.Is(err, ErrUnknownCode) {
			t.Errorf("EdgeKindFromCode(%d) = %q, %v; want ErrUnknownCode", code, kind, err)
		}
		if confidence, err := ConfidenceFromCode(code); !errors.Is(err, ErrUnknownCode) {
			t.Errorf("ConfidenceFromCode(%d) = %q, %v; want ErrUnknownCode", code, confidence, err)
		}
		if provenance, err := ProvenanceFromCode(code); !errors.Is(err, ErrUnknownCode) {
			t.Errorf("ProvenanceFromCode(%d) = %q, %v; want ErrUnknownCode", code, provenance, err)
		}
	}
}

func TestCodesRoundTrip(t *testing.T) {
	for kind := range goldenCodes.kinds {
		code, err := kind.Code()
		if err != nil {
			t.Fatalf("EdgeKind(%q).Code() error = %v", kind, err)
		}
		back, err := EdgeKindFromCode(code)
		if err != nil || back != kind {
			t.Errorf("EdgeKindFromCode(%d) = %q, %v; want %q", code, back, err, kind)
		}
	}
	for confidence := range goldenCodes.confidences {
		code, err := confidence.Code()
		if err != nil {
			t.Fatalf("Confidence(%q).Code() error = %v", confidence, err)
		}
		back, err := ConfidenceFromCode(code)
		if err != nil || back != confidence {
			t.Errorf("ConfidenceFromCode(%d) = %q, %v; want %q", code, back, err, confidence)
		}
	}
	for provenance := range goldenCodes.provenances {
		code, err := provenance.Code()
		if err != nil {
			t.Fatalf("Provenance(%q).Code() error = %v", provenance, err)
		}
		back, err := ProvenanceFromCode(code)
		if err != nil || back != provenance {
			t.Errorf("ProvenanceFromCode(%d) = %q, %v; want %q", code, back, err, provenance)
		}
	}
}

func TestUnknownVocabularyHasNoCode(t *testing.T) {
	if _, err := EdgeKind("INVENTED").Code(); !errors.Is(err, ErrUnknownCode) {
		t.Errorf("EdgeKind(\"INVENTED\").Code() error = %v, want ErrUnknownCode", err)
	}
	if _, err := Confidence("VERY_SURE").Code(); !errors.Is(err, ErrUnknownCode) {
		t.Errorf("Confidence(\"VERY_SURE\").Code() error = %v, want ErrUnknownCode", err)
	}
	if _, err := Provenance("VIBES").Code(); !errors.Is(err, ErrUnknownCode) {
		t.Errorf("Provenance(\"VIBES\").Code() error = %v, want ErrUnknownCode", err)
	}
}

func codeOf(typeName, value string) error {
	switch typeName {
	case "EdgeKind":
		_, err := EdgeKind(value).Code()
		return err
	case "Confidence":
		_, err := Confidence(value).Code()
		return err
	default:
		_, err := Provenance(value).Code()
		return err
	}
}

// declaredConstants collects the string values of every typed constant of the
// named types, so the test tracks the real source instead of a copy.
func declaredConstants(t *testing.T, path string) map[string][]string {
	t.Helper()
	file, err := parser.ParseFile(token.NewFileSet(), path, nil, 0)
	if err != nil {
		t.Fatalf("parse %s: %v", path, err)
	}
	declared := map[string][]string{}
	for _, declaration := range file.Decls {
		group, ok := declaration.(*ast.GenDecl)
		if !ok || group.Tok != token.CONST {
			continue
		}
		for _, spec := range group.Specs {
			value, ok := spec.(*ast.ValueSpec)
			if !ok {
				continue
			}
			name, ok := value.Type.(*ast.Ident)
			if !ok {
				continue
			}
			for _, literal := range value.Values {
				text, ok := literal.(*ast.BasicLit)
				if !ok || text.Kind != token.STRING {
					continue
				}
				declared[name.Name] = append(declared[name.Name], text.Value[1:len(text.Value)-1])
			}
		}
	}
	return declared
}
