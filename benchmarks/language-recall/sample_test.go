package main

import (
	"math/rand/v2"
	"reflect"
	"testing"
)

// The rename must hit the declaring occurrence and nothing else: a rename of the
// wrong occurrence measures the rename, not the tool.
func TestLocateFindsTheDeclaringOccurrence(t *testing.T) {
	tests := []struct {
		name     string
		language string
		kind     string
		source   string
		want     string // the text the offset must point at, with its right context
	}{
		{"go method whose receiver type has the same name", "go", "method", "func (n *Name) Name() string {\n", "Name() string"},
		{"go field after a doc line", "go", "field", "// doc\n\tArgs PositionalArgs\n", "Args PositionalArgs"},
		{"go const in a block", "go", "const", "const (\n\tFirst = iota\n)\n", "First = iota"},
		{"typescript private method", "typescript", "method", "class A {\n\tasync #load(x: string) {\n", "#load(x: string)"},
		{"typescript exported variable", "typescript", "variable", "export const limit = 3;\n", "limit = 3"},
		{"typescript function whose parameter has the same name", "typescript", "function", "export function run(run: number) {\n", "run(run: number)"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			name := test.want[:indexOfNameEnd(test.want)]
			line := 1 + countLinesBefore(test.source, test.want)
			offset, ok := locate(test.language, []byte(test.source), declaration{Name: name, Kind: test.kind, Line: line})
			if !ok {
				t.Fatalf("locate refused %q", test.name)
			}
			if got := test.source[offset:]; got[:len(test.want)] != test.want {
				t.Fatalf("offset points at %q, want %q", got, test.want)
			}
		})
	}
}

func TestLocateRefusesWhatItCannotRenameExactly(t *testing.T) {
	tests := []struct {
		name   string
		source string
		decl   declaration
	}{
		{"name absent from its line", "func other() {}\n", declaration{Name: "wanted", Kind: "func", Line: 1}},
		{"kind not understood", "func wanted() {}\n", declaration{Name: "wanted", Kind: "package", Line: 1}},
		{"skipped name", "func main() {}\n", declaration{Name: "main", Kind: "func", Line: 1}},
		{"line past the end", "func wanted() {}\n", declaration{Name: "wanted", Kind: "func", Line: 9}},
		{"line zero", "func wanted() {}\n", declaration{Name: "wanted", Kind: "func", Line: 0}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if _, ok := locate("go", []byte(test.source), test.decl); ok {
				t.Fatal("locate accepted a declaration it cannot rename exactly")
			}
		})
	}
}

func TestRenameChangesOnlyTheChosenOccurrence(t *testing.T) {
	source := []byte("func Name() { Name() }\n")
	got := string(rename(source, 5, "Name"))
	if want := "func NameKvg() { Name() }\n"; got != want {
		t.Fatalf("rename = %q, want %q", got, want)
	}
}

// The sample has to be reproducible, and every kind has to appear before any
// kind repeats.
func TestDrawIsDeterministicAndCyclesThroughKinds(t *testing.T) {
	var candidates []candidate
	for _, kind := range []string{"func", "type", "field"} {
		for _, key := range []string{"a", "b", "c", "d"} {
			candidates = append(candidates, candidate{declaration: declaration{Kind: kind, StableKey: kind + key}})
		}
	}
	first := draw(append([]candidate(nil), candidates...), 6, 7)
	again := draw(append([]candidate(nil), candidates...), 6, 7)
	if !reflect.DeepEqual(first, again) {
		t.Fatal("the same seed drew different samples")
	}
	seen := map[string]int{}
	for _, picked := range first[:3] {
		seen[picked.Kind]++
	}
	if len(seen) != 3 {
		t.Fatalf("the first three picks covered kinds %v, want one of each", seen)
	}
	if got := len(draw(candidates, 100, 7)); got != len(candidates) {
		t.Fatalf("asking for more than exist returned %d, want %d", got, len(candidates))
	}
}

func TestProportionStaysInsideTheUnitInterval(t *testing.T) {
	for _, r := range []ratio{{0, 5}, {5, 5}, {1, 1}, {0, 1}} {
		m := proportion(r)
		if m.Low < 0 || m.High > 1.0000001 || m.Low > m.Value || m.Value > m.High {
			t.Fatalf("proportion(%v) = %+v, want 0 <= low <= value <= high <= 1", r, m)
		}
	}
	if m := proportion(ratio{}); m.Den != 0 || m.Value != 0 {
		t.Fatalf("an empty proportion reported %+v", m)
	}
}

func TestBootstrapOfOneRatioIsThatRatio(t *testing.T) {
	m := bootstrap([]ratio{{3, 4}}, rand.New(rand.NewPCG(1, 1)))
	if m.Value != 0.75 || m.Low != 0.75 || m.High != 0.75 {
		t.Fatalf("bootstrap of a single ratio = %+v, want 0.75 with no spread", m)
	}
	if m := bootstrap(nil, rand.New(rand.NewPCG(1, 1))); m.Den != 0 {
		t.Fatalf("bootstrap of nothing = %+v", m)
	}
}

// An error about a type no longer satisfying an interface is a relation, not a
// use of the renamed name.
func TestRelationErrorsAreNotDependents(t *testing.T) {
	if !isRelationError("go", oracleError{Message: "cannot use x as I value: T does not implement I (missing method Run)"}) {
		t.Fatal("a Go interface-satisfaction error was counted as a use")
	}
	if isRelationError("go", oracleError{Message: "undefined: Run"}) {
		t.Fatal("an undefined name was set aside as a relation")
	}
	if !isRelationError("typescript", oracleError{Code: "TS2420"}) || isRelationError("typescript", oracleError{Code: "TS2305"}) {
		t.Fatal("TypeScript relation codes are wrong")
	}
}

func indexOfNameEnd(text string) int {
	for i, r := range text {
		if !(r == '#' || r == '_' || r >= '0' && r <= '9' || r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z') {
			return i
		}
	}
	return len(text)
}

func countLinesBefore(source, marker string) int {
	for i := 0; i+len(marker) <= len(source); i++ {
		if source[i:i+len(marker)] == marker {
			lines := 0
			for _, r := range source[:i] {
				if r == '\n' {
					lines++
				}
			}
			return lines
		}
	}
	return 0
}
