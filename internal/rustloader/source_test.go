package rustloader

import (
	"context"
	"strings"
	"testing"

	"github.com/Luqueee/kivgraph/internal/syntax"
)

// TestReferenceClassifiesValuePositions pins the half of the contract the
// grammar owns. The analyzer never says how an occurrence is used, so if these
// shapes stop being told apart the graph silently loses the difference between
// calling a function and handing it over. No analyzer is needed to check it.
func TestReferenceClassifiesValuePositions(t *testing.T) {
	source := `fn callee(f: fn(i32) -> i32) -> i32 { f(1) }
fn direct() -> i32 { target(1) }
fn argument() -> i32 { callee(target) }
fn nested_argument() -> i32 { callee(module::target) }
fn borrowed_argument() -> i32 { callee(&target) }
fn method_argument(v: Vec<i32>) -> Vec<i32> { v.into_iter().map(target).collect() }
fn binding() -> i32 { let held = target; held(1) }
fn field_binding() -> Holder { Holder { slot: target } }
fn constant_binding() -> [fn(i32) -> i32; 1] { const TABLE: [fn(i32) -> i32; 1] = [target]; TABLE }
fn tail() -> fn(i32) -> i32 { target }
fn keyword() -> fn(i32) -> i32 { return target; }
fn typed(value: Target) -> Target { value }
fn plain() -> i32 { target.field }
`
	wanted := map[int]ReferenceKind{
		2:  ReferenceCall,
		3:  ReferenceCallback,
		4:  ReferenceCallback,
		5:  ReferenceCallback,
		6:  ReferenceCallback,
		7:  ReferenceAssign,
		8:  ReferenceAssign,
		9:  ReferenceAssign,
		10: ReferenceReturn,
		11: ReferenceReturn,
		13: ReferenceUse,
	}

	manager, err := syntax.NewParserManager(1)
	if err != nil {
		t.Fatalf("NewParserManager() error = %v", err)
	}
	t.Cleanup(func() { manager.Close() })
	parsed, err := NewSource(context.Background(), manager, "values.rs", []byte(source))
	if err != nil {
		t.Fatalf("NewSource() error = %v", err)
	}
	t.Cleanup(func() { parsed.Close() })

	for line, want := range wanted {
		offset := offsetOfOccurrence(t, source, line, "target")
		got := parsed.Reference(offset, offset+len("target"))
		if got != want {
			t.Errorf("line %d (%s): Reference() = %q, want %q",
				line, strings.TrimSpace(strings.Split(source, "\n")[line-1]), got, want)
		}
	}

	typeLine := offsetOfOccurrence(t, source, 12, "Target")
	if got := parsed.Reference(typeLine, typeLine+len("Target")); got != ReferenceType {
		t.Errorf("a type in parameter position = %q, want %q", got, ReferenceType)
	}
}

// offsetOfOccurrence finds the byte offset of the first occurrence of name on
// a one-based line.
func offsetOfOccurrence(t *testing.T, source string, line int, name string) int {
	t.Helper()
	offset := 0
	for index, text := range strings.Split(source, "\n") {
		if index+1 == line {
			column := strings.Index(text, name)
			if column < 0 {
				t.Fatalf("line %d has no %q: %s", line, name, text)
			}
			return offset + column
		}
		offset += len(text) + 1
	}
	t.Fatalf("source has no line %d", line)
	return 0
}
