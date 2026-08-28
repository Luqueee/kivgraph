package scip

import (
	"testing"

	"github.com/Luqueee/kivgraph/internal/scip/scipwire"
)

// declarationAt builds a declaration the way a definition occurrence would.
func declarationAt(symbol string, line int32) declaration {
	return declaration{
		symbol: symbol,
		selection: scipwire.Range{
			StartLine: line, StartCharacter: 4,
			EndLine: line, EndCharacter: 10, Present: true,
		},
	}
}

func withEnclosing(entry declaration, startLine, endLine int32) declaration {
	entry.enclosing = scipwire.Range{
		StartLine: startLine, EndLine: endLine, Present: true,
	}
	return entry
}

const pkg = "scip-dotnet nuget . . "

// TestReconstructionYieldsToTheProducer is the guard that keeps Java away from
// this code entirely. scip-java sets an enclosing range on every definition, so
// a reconstruction would be replacing a fact with an inference.
func TestReconstructionYieldsToTheProducer(t *testing.T) {
	given := []declaration{
		withEnclosing(declarationAt(pkg+"App/Thing#", 2), 2, 40),
		declarationAt(pkg+"App/Thing#Run().", 5),
	}
	got := reconstructEnclosingRanges(given, newOffsetTable([]byte("x\n"), 0))
	if got[0].enclosing.EndLine != 40 {
		t.Errorf("the producer's range was overwritten: %+v", got[0].enclosing)
	}
	// The partial case is deliberately all-or-nothing. Mixing a producer's
	// ranges with invented ones would put two incompatible notions of
	// containment in one document, and the one that wins would depend on
	// declaration order.
	if got[1].enclosing.Present {
		t.Error("a range was invented for a document the producer described")
	}
}

// TestReconstructionNestsByDescriptor is the property the whole thing rests on:
// containment comes from the SCIP descriptor path, not from where a
// declaration happens to sit. `App/Thing#Run().` is inside `App/Thing#`
// because its descriptors say so.
func TestReconstructionNestsByDescriptor(t *testing.T) {
	source := []byte("line0\nline1\nline2\nline3\nline4\nline5\nline6\nline7\nline8\n")
	got := reconstructEnclosingRanges([]declaration{
		declarationAt(pkg+"App/Thing#", 1),
		declarationAt(pkg+"App/Thing#Field.", 2),
		declarationAt(pkg+"App/Thing#Run().", 4),
		declarationAt(pkg+"App/Other#", 6),
	}, newOffsetTable(source, 0))

	spans := map[string]scipwire.Range{}
	for _, entry := range got {
		spans[entry.symbol] = entry.enclosing
	}

	// The type reaches to the next declaration that is not one of its own
	// members, which is the sibling type.
	thing := spans[pkg+"App/Thing#"]
	if thing.StartLine != 1 || thing.EndLine != 6 {
		t.Errorf("Thing spans %d..%d, want 1..6", thing.StartLine, thing.EndLine)
	}
	// A member stops at the next declaration, whatever it is.
	field := spans[pkg+"App/Thing#Field."]
	if field.StartLine != 2 || field.EndLine != 4 {
		t.Errorf("Field spans %d..%d, want 2..4", field.StartLine, field.EndLine)
	}
	// The last declaration runs to the end of the file.
	other := spans[pkg+"App/Other#"]
	if other.StartLine != 6 || other.EndLine < 8 {
		t.Errorf("Other spans %d..%d, want it to reach the end of the file",
			other.StartLine, other.EndLine)
	}
}

// TestReconstructionContainsItsMembers is what the attribution actually needs:
// a reference inside a method must be inside the method, and the method inside
// its type, so the innermost-first search finds the method.
func TestReconstructionContainsItsMembers(t *testing.T) {
	source := []byte("0\n1\n2\n3\n4\n5\n6\n7\n8\n9\n")
	got := reconstructEnclosingRanges([]declaration{
		declarationAt(pkg+"App/Thing#", 1),
		declarationAt(pkg+"App/Thing#Run().", 3),
		declarationAt(pkg+"App/Other#", 7),
	}, newOffsetTable(source, 0))

	var thing, run scipwire.Range
	for _, entry := range got {
		switch entry.symbol {
		case pkg + "App/Thing#":
			thing = entry.enclosing
		case pkg + "App/Thing#Run().":
			run = entry.enclosing
		}
	}
	use := scipwire.Range{StartLine: 4, StartCharacter: 8, EndLine: 4, EndCharacter: 12, Present: true}
	if !run.Contains(use) {
		t.Errorf("a use on line 4 is not inside Run() (%d..%d)", run.StartLine, run.EndLine)
	}
	if !thing.Contains(use) {
		t.Errorf("a use on line 4 is not inside Thing (%d..%d)", thing.StartLine, thing.EndLine)
	}
	// And the method is the narrower of the two, which is what makes the
	// innermost-first search pick it.
	if !narrower(declaration{enclosing: run}, declaration{enclosing: thing}) {
		t.Error("the method is not narrower than its type")
	}
}

func TestDescendsReadsTheDescriptorPathOnly(t *testing.T) {
	for _, testCase := range []struct {
		inner, outer string
		want         bool
	}{
		{pkg + "App/Thing#Run().", pkg + "App/Thing#", true},
		{pkg + "App/Thing#", pkg + "App/Thing#Run().", false},
		{pkg + "App/Thing#", pkg + "App/Thing#", false},
		{pkg + "App/Other#", pkg + "App/Thing#", false},
		// Different package coordinates, same descriptors: still nested. The
		// coordinates say where a symbol came from, not what contains it, and
		// scip-dotnet writes `.` for both on everything a project declares.
		{"scip-dotnet nuget X 1 App/Thing#Run().", pkg + "App/Thing#", true},
		{"local 3", pkg + "App/Thing#", false},
	} {
		if got := descends(testCase.inner, testCase.outer); got != testCase.want {
			t.Errorf("descends(%q, %q) = %t, want %t",
				testCase.inner, testCase.outer, got, testCase.want)
		}
	}
}

// TestReconstructionSurvivesAnEmptyDocument covers a file the producer listed
// and found nothing in. A panic here would take down the whole pass over one
// empty source file.
func TestReconstructionSurvivesAnEmptyDocument(t *testing.T) {
	if got := reconstructEnclosingRanges(nil, newOffsetTable(nil, 0)); len(got) != 0 {
		t.Errorf("got %d declarations from none", len(got))
	}
	got := reconstructEnclosingRanges(
		[]declaration{declarationAt(pkg+"App/Thing#", 0)}, newOffsetTable(nil, 0))
	if len(got) != 1 {
		t.Fatalf("got %d declarations, want 1", len(got))
	}
	if !got[0].enclosing.Present {
		t.Error("the only declaration of an empty file got no range")
	}
}
