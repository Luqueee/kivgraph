package pythonloader

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Luqueee/kivgraph/internal/facts"
	"github.com/Luqueee/kivgraph/internal/workspace"
)

// The source both workers are shown. Every way it uses a declaration that is
// not a bare name is here: through a receiver, through a module, inside a
// quoted annotation, inside `cast`. The strings that are not annotations are
// here too, because reading those as names would be the opposite mistake.
const occurrencesSource = `import typing as t
from typing import Literal

P = t.ParamSpec("P")


class Vehicle:
    wheels: int = 4

    def drive(self) -> "Vehicle":
        return self


def wrap(function: "t.Callable[P, Vehicle]") -> "t.Callable[P, t.Optional[Vehicle]]":
    return function


def use(vehicle: Vehicle, mode: Literal["nothing"] = "nothing") -> int:
    vehicle.drive()
    other = t.cast("Vehicle", vehicle)
    label = "Vehicle is not a use here"
    return other.wheels + len(label)
`

func occurrencesProject(t *testing.T) (string, workspace.Repository) {
	t.Helper()
	root := filepath.Join(t.TempDir(), "occurrences")
	if err := os.MkdirAll(root, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "main.py"), []byte(occurrencesSource), 0o600); err != nil {
		t.Fatal(err)
	}
	return root, workspace.Repository{Name: "occurrences", Path: root, RealPath: root}
}

// lineOf is where a piece of the source is, so an assertion names a line and
// not a number somebody has to count.
func lineOf(t *testing.T, fragment string) int {
	t.Helper()
	index := strings.Index(occurrencesSource, fragment)
	if index < 0 {
		t.Fatalf("the source has no %q", fragment)
	}
	return 1 + strings.Count(occurrencesSource[:index], "\n")
}

func textAt(reference facts.SemanticReference) string {
	return occurrencesSource[reference.Start:reference.End]
}

// The syntactic worker cannot say what `vehicle.drive` is. It used to say
// nothing, so `find_references` answered COMPLETE for a method while missing
// every use of it: 15 of 75 such answers on real code. It now records that it
// saw the name and did not resolve it, which is what turns the verdict.
func TestFallbackRecordsEveryMemberItDoesNotResolve(t *testing.T) {
	root, repository := occurrencesProject(t)
	payload, err := Run(context.Background(), "missing-scippython", "python3", repository, root)
	if err != nil {
		t.Fatal(err)
	}
	unresolved := map[string][]int{}
	for _, row := range payload.Unresolved {
		if row.Reason != "NAME_NOT_RESOLVED" {
			t.Errorf("unresolved %q has reason %q", row.RequestedSymbol, row.Reason)
		}
		if got := occurrencesSource[row.Start : row.Start+len(row.RequestedSymbol)]; got != row.RequestedSymbol {
			t.Errorf("unresolved %q points at %q", row.RequestedSymbol, got)
		}
		unresolved[row.RequestedSymbol] = append(unresolved[row.RequestedSymbol], row.StartLine)
	}
	for member, line := range map[string]int{
		"drive":  lineOf(t, "vehicle.drive()"),
		"wheels": lineOf(t, "other.wheels"),
		"cast":   lineOf(t, `t.cast("Vehicle"`),
	} {
		if !containsLine(unresolved[member], line) {
			t.Errorf("no unresolved row for .%s on line %d: have %v", member, line, unresolved[member])
		}
	}
	// Members inside a quoted annotation are members too.
	if !containsLine(unresolved["Callable"], lineOf(t, "def wrap(")) {
		t.Errorf("no unresolved row for Callable inside the quoted annotation: %v", unresolved["Callable"])
	}
	for _, value := range []string{"nothing", "Vehicle is not a use here"} {
		if len(unresolved[value]) != 0 {
			t.Errorf("%q was read as a name: %v", value, unresolved[value])
		}
	}
}

// insideQuotes says whether an offset of the source is inside a string literal
// of its line, which is where a quoted annotation's names are.
func insideQuotes(offset int) bool {
	lineStart := strings.LastIndex(occurrencesSource[:offset], "\n") + 1
	return strings.Count(occurrencesSource[lineStart:offset], `"`)%2 == 1
}

func TestFallbackResolvesANameInsideAQuotedAnnotation(t *testing.T) {
	root, repository := occurrencesProject(t)
	payload, err := Run(context.Background(), "missing-scippython", "python3", repository, root)
	if err != nil {
		t.Fatal(err)
	}
	quoted := map[int]map[string]bool{}
	for _, reference := range payload.References {
		if !insideQuotes(reference.Start) {
			continue
		}
		if reference.Kind != "TYPE_USES" || textAt(reference) != reference.Text {
			t.Errorf("quoted reference %+v: want a TYPE_USES whose evidence is its own name", reference)
		}
		if quoted[reference.StartLine] == nil {
			quoted[reference.StartLine] = map[string]bool{}
		}
		quoted[reference.StartLine][reference.Text] = true
	}
	for fragment, name := range map[string]string{
		`def drive(self)`:  "Vehicle",
		`t.cast("Vehicle"`: "Vehicle",
		`def wrap(`:        "P",
	} {
		if line := lineOf(t, fragment); !quoted[line][name] {
			t.Errorf("line %d: no use of %q inside the quotes, have %v", line, name, quoted[line])
		}
	}
	// A value of a Literal and an ordinary string name nothing.
	for _, fragment := range []string{`Literal["nothing"]`, `label = "Vehicle is n`} {
		if line := lineOf(t, fragment); len(quoted[line]) != 0 {
			t.Errorf("line %d: a string that is not an annotation was read as names: %v", line, quoted[line])
		}
	}
}

func containsLine(lines []int, want int) bool {
	for _, line := range lines {
		if line == want {
			return true
		}
	}
	return false
}

// The exact adapter asks Pyright about every name, and a name inside quotes was
// never asked about. `P` here is used nowhere else: it was a declaration with
// no reference at all, answered COMPLETE.
func TestExactPyrightResolvesNamesInsideQuotedAnnotations(t *testing.T) {
	analyzer := os.Getenv("KIVGRAPH_PYRIGHT_LANGSERVER")
	if analyzer == "" {
		analyzer, _ = exec.LookPath("pyright-langserver")
	}
	if analyzer == "" {
		t.Skip("set KIVGRAPH_PYRIGHT_LANGSERVER or install pyright-langserver")
	}
	_, repository := occurrencesProject(t)
	payload, err := RunWithOptions(context.Background(), Options{
		AnalyzerCommand: "kivgraph-python-pyright --analyzer " + analyzer,
		AnalyzerMode:    "exact",
		PythonPath:      "python3",
	}, repository, filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	targets := map[string]map[int]bool{}
	for _, reference := range payload.References {
		if textAt(reference) != reference.Text {
			t.Errorf("reference to %q has evidence %q", reference.Text, textAt(reference))
		}
		name := reference.TargetID[strings.Index(reference.TargetID, "\x00")+1:]
		name = name[:strings.Index(name, "\x00")]
		if targets[name] == nil {
			targets[name] = map[int]bool{}
		}
		targets[name][reference.StartLine] = true
	}
	for target, line := range map[string]int{
		"main.P":       lineOf(t, "def wrap("),
		"main.Vehicle": lineOf(t, "def wrap("),
	} {
		if !targets[target][line] {
			t.Errorf("no reference to %s on line %d: have lines %v", target, line, targets[target])
		}
	}
	// Pyright does not navigate from the string of a `cast`. That is its answer
	// and it is recorded as one: a row that says the name was seen and not
	// resolved, which is what keeps the verdict from saying COMPLETE.
	castLine, seen := lineOf(t, `t.cast("Vehicle"`), false
	for _, row := range payload.Unresolved {
		seen = seen || row.StartLine == castLine && row.RequestedSymbol == "Vehicle"
	}
	if !seen && !targets["main.Vehicle"][castLine] {
		t.Errorf("the name inside cast(...) on line %d left neither a reference nor an unresolved row", castLine)
	}
	if !targets["main.Vehicle"][lineOf(t, "def drive(self)")] {
		t.Errorf("the quoted return type of drive is not a reference to Vehicle: %v", targets["main.Vehicle"])
	}
	for _, row := range payload.Unresolved {
		if row.RequestedSymbol == "nothing" || strings.Contains(row.RequestedSymbol, " ") {
			t.Errorf("a string value was asked about as a name: %+v", row)
		}
	}
	if targets["main.Vehicle"][lineOf(t, `label = "Vehicle is n`)] {
		t.Error("a string that is not an annotation was read as a use of Vehicle")
	}
}
