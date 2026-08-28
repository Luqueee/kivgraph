package scip

import (
	"testing"
)

// TestReconstructedRangesNest is the invariant the whole attribution rests on,
// and the one a reader of reconstructEnclosingRanges has to take on faith
// otherwise: a member's reconstructed span is contained in the span of the
// type that declares it.
//
// It matters because references are attributed innermost-first. If a member
// could reach past its parent's end, a use after the type closed would be
// counted as coming from the last member of it rather than from the file --
// an edge sourced at a symbol that does not contain it.
//
// The stack in reconstructEnclosingRanges closes a parent and its children at
// the same position, so containment holds by construction rather than by
// arithmetic. This states it as a property over the real fixture instead of
// over an example, which is what makes it survive a change to the algorithm.
func TestReconstructedRangesNest(t *testing.T) {
	payload := convertCoverage(t)

	byName := map[string]facts0{}
	for _, symbol := range payload.Symbols {
		byName[symbol.QualifiedName] = facts0{
			startLine: symbol.StartLine, endLine: symbol.EndLine, kind: symbol.Kind,
		}
	}

	// Every symbol whose qualified name extends another's is nested in it by
	// definition of the descriptor path, so its range must be too.
	checked := 0
	for inner, innerSpan := range byName {
		for outer, outerSpan := range byName {
			if inner == outer || !isDescendantName(inner, outer) {
				continue
			}
			if outerSpan.kind == facts0ModuleKind || innerSpan.kind == facts0ModuleKind {
				continue
			}
			checked++
			if innerSpan.startLine < outerSpan.startLine {
				t.Errorf("%s starts at %d, before its parent %s at %d",
					inner, innerSpan.startLine, outer, outerSpan.startLine)
			}
			if innerSpan.endLine > outerSpan.endLine {
				t.Errorf("%s ends at %d, past its parent %s at %d",
					inner, innerSpan.endLine, outer, outerSpan.endLine)
			}
		}
	}
	if checked == 0 {
		t.Fatal("the fixture produced no nested pair, so this proves nothing")
	}
}

type facts0 struct {
	startLine, endLine int
	kind               string
}

const facts0ModuleKind = "module"

// isDescendantName reports whether one qualified name sits under another. It
// is the dotted spelling of the descriptor prefix test, and the separator
// matters: `Catalog.Add` is not under `Catalog.A`.
func isDescendantName(inner, outer string) bool {
	return len(inner) > len(outer)+1 &&
		inner[:len(outer)] == outer &&
		inner[len(outer)] == '.'
}

// TestReconstructedRangesCoverTheirOwnDeclaration is the other half: a span
// that does not contain the name it belongs to cannot contain anything at all,
// and every reference in the file would fall through to the module symbol.
func TestReconstructedRangesCoverTheirOwnDeclaration(t *testing.T) {
	payload := convertCoverage(t)
	for _, symbol := range payload.Symbols {
		if symbol.Kind == facts0ModuleKind {
			continue
		}
		if symbol.EndLine < symbol.StartLine {
			t.Errorf("%s spans %d..%d, which is backwards",
				symbol.QualifiedName, symbol.StartLine, symbol.EndLine)
		}
		if symbol.End < symbol.Start {
			t.Errorf("%s has byte range %d..%d, which is backwards",
				symbol.QualifiedName, symbol.Start, symbol.End)
		}
	}
}
