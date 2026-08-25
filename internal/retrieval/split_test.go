package retrieval

import (
	"reflect"
	"testing"
)

// tokens is the readable form of a span list, for tests only: the production
// path interns the ranges and never builds these strings.
func tokens(value string) []string {
	spans := AppendSpans(nil, value)
	out := make([]string, 0, len(spans))
	for _, span := range spans {
		out = append(out, value[span.Start:span.End])
	}
	return out
}

// TestAppendSpansMatchesTheUpstreamTable is the adaptation's guard rail. Every
// case is copied verbatim from casing_test.go of github.com/danielgtaylor/casing
// v1.0.0, so a span version that drifted from the function it was derived from
// fails here instead of quietly answering something else.
func TestAppendSpansMatchesTheUpstreamTable(t *testing.T) {
	for _, test := range []struct {
		input string
		want  []string
	}{
		{"CamelCaseTest", []string{"Camel", "Case", "Test"}},
		{"lowerCamelTest", []string{"lower", "Camel", "Test"}},
		{"snake_case_test", []string{"snake", "case", "test"}},
		{"kabob-case-test", []string{"kabob", "case", "test"}},
		{"Space delimited test", []string{"Space", "delimited", "test"}},
		{"AnyKind of_string", []string{"Any", "Kind", "of", "string"}},
		{"hello__man how-Are you??", []string{"hello", "man", "how", "Are", "you"}},
		{"UserID", []string{"User", "ID"}},
		{"HTTPServer", []string{"HTTP", "Server"}},
		{"Test123Test", []string{"Test", "123", "Test"}},
		{"Test123test", []string{"Test", "123", "test"}},
		{"Dupe-_---test", []string{"Dupe", "test"}},
		{"ÜberWürsteÄußerst", []string{"Über", "Würste", "Äußerst"}},
		{"MakeAWish", []string{"Make", "A", "Wish"}},
		{"uHTTP123", []string{"u", "HTTP", "123"}},
		{"aB1-1Ba", []string{"a", "B", "1", "1", "Ba"}},
		{"a.bc.d", []string{"a", "bc", "d"}},
		{"Emojis 🎉🎊-🎈", []string{"Emojis", "🎉🎊", "🎈"}},
		{"a b c", []string{"a", "b", "c"}},
		{"1 2 3", []string{"1", "2", "3"}},
	} {
		if got := tokens(test.input); !reflect.DeepEqual(got, test.want) {
			t.Errorf("AppendSpans(%q) = %q, want %q", test.input, got, test.want)
		}
	}
}

// TestAppendSpansReadsTheIdentifiersThisGraphHolds covers the shapes the five
// indexed languages actually produce, which is the reason this package exists.
// The upstream table proves the adaptation; this one proves the choice.
func TestAppendSpansReadsTheIdentifiersThisGraphHolds(t *testing.T) {
	for _, test := range []struct {
		input string
		want  []string
	}{
		{"TraverseFrom", []string{"Traverse", "From"}},                         // Go, exported
		{"newServerWithIndexer", []string{"new", "Server", "With", "Indexer"}}, // Go, unexported
		{"GraphSnapshot.TraverseFrom", []string{"Graph", "Snapshot", "Traverse", "From"}},
		{"XMLParser", []string{"XML", "Parser"}},
		{"HTTPSProxy", []string{"HTTPS", "Proxy"}},
		{"SCREAMING_SNAKE", []string{"SCREAMING", "SNAKE"}}, // Rust and Go constants
		{"snake_case_name", []string{"snake", "case", "name"}},
		{"__dunder__", []string{"dunder"}},                     // Python
		{"kebab-case-name", []string{"kebab", "case", "name"}}, // package names
		{"internal/hotsnapshot/traversal.go", []string{"internal", "hotsnapshot", "traversal", "go"}},
	} {
		if got := tokens(test.input); !reflect.DeepEqual(got, test.want) {
			t.Errorf("AppendSpans(%q) = %q, want %q", test.input, got, test.want)
		}
	}
}

// TestAppendSpansReusesTheCallersBuffer is the reason the signature takes dst:
// splitting runs over every symbol of the graph on every index pass, and a
// caller that reuses one slice must not pay an allocation per identifier.
func TestAppendSpansReusesTheCallersBuffer(t *testing.T) {
	buffer := make([]Span, 0, 16)
	for _, identifier := range []string{"TraverseFrom", "XMLParser", "snake_case_name"} {
		buffer = AppendSpans(buffer[:0], identifier)
		if len(buffer) == 0 {
			t.Fatalf("AppendSpans(%q) produced no spans", identifier)
		}
	}
	if capacity := cap(buffer); capacity != 16 {
		t.Fatalf("capacity = %d, want the caller's buffer kept at 16", capacity)
	}

	// A span addresses the string it was given, so the caller can intern
	// without copying. If that stopped holding, every caller would be reading
	// the wrong bytes rather than failing.
	const value = "HTTPServer"
	spans := AppendSpans(nil, value)
	if len(spans) != 2 || value[spans[0].Start:spans[0].End] != "HTTP" || value[spans[1].Start:spans[1].End] != "Server" {
		t.Fatalf("spans = %#v, want ranges addressing %q", spans, value)
	}
}
