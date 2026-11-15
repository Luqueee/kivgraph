package agenthook

import (
	"context"
	"testing"
)

// A search that says where it looks, and looks only at files the graph does
// not cover, is not a question the graph answers. The gate asked about the
// pattern alone, so `grep -n version package.json` was refused because some
// symbol somewhere is called version, and the caller was sent to
// find_references for a JSON key.
func TestGateLeavesASearchOverFilesTheGraphDoesNotCover(t *testing.T) {
	ambiguous := Facts{Declarations: 4, Repositories: 2, References: 60}
	indexed := IndexedExtensions([]string{".go"})
	for _, testCase := range []struct {
		name  string
		kind  Kind
		paths []string
		deny  bool
	}{
		// Not source: the gate has nothing to say.
		{"a manifest", KindSymbol, []string{"package.json"}, false},
		{"a log", KindSymbol, []string{"logs/app.log"}, false},
		{"a directory restricted by a glob to configuration", KindSymbol, []string{"src", "*.yaml"}, false},
		{"a document, asked by regex", KindIntent, []string{"docs/notes.md"}, false},
		{"a language this repository does not index", KindSymbol, []string{"tools/gen.py"}, false},

		// Source, or possibly source: the refusal stands.
		{"no path at all", KindSymbol, nil, true},
		{"a source file", KindSymbol, []string{"internal/server.go"}, true},
		{"a source glob", KindSymbol, []string{"*.go"}, true},
		{"a directory, which can hold anything", KindSymbol, []string{"internal"}, true},
		{"a directory with a trailing separator", KindSymbol, []string{"docs/"}, true},
		{"a document beside a source file", KindSymbol, []string{"README.md", "main.go"}, true},
		// `--type rust` reaches here as *.rust: a language named by its type
		// rather than its extension is still a search over code.
		{"a type filter that names a language", KindSymbol, []string{"*.rust"}, true},
		{"a source file, asked by regex", KindIntent, []string{"internal/server.go"}, true},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			gate := Gate{Graph: &fakeGraph{symbol: ambiguous, intent: ambiguous}, Indexed: indexed}
			decision := gate.Decide(context.Background(), Question{
				Kind: testCase.kind, Pattern: "version", Paths: testCase.paths,
			})
			if decision.Deny != testCase.deny {
				t.Fatalf("paths %v: deny = %v, want %v\n%s", testCase.paths, decision.Deny, testCase.deny, decision.Reason)
			}
		})
	}

	// A gate that was not told what the repository indexes cannot call a
	// path uncovered, so it decides as it did before.
	blind := Gate{Graph: &fakeGraph{symbol: ambiguous}}
	if !blind.Decide(context.Background(), Question{Kind: KindSymbol, Pattern: "version", Paths: []string{"package.json"}}).Deny {
		t.Fatal("a gate with no index of extensions stood aside over a path it knows nothing about")
	}
}

// The same decision from the commands an agent actually types, so the paths
// the gate reads are the ones the classifier records and not ones a test
// supplied by hand.
func TestGateDecidesRealSearchesByWhereTheyLook(t *testing.T) {
	gate := Gate{
		Graph:   &fakeGraph{symbol: Facts{Declarations: 4, Repositories: 2, References: 60}},
		Indexed: IndexedExtensions([]string{".go"}),
	}
	for _, testCase := range []struct {
		tool  string
		input any
		deny  bool
	}{
		{"Bash", bashInput{Command: "grep -n version package.json"}, false},
		{"Bash", bashInput{Command: "rg version docs/config.yaml"}, false},
		{"Grep", grepInput{Pattern: "version", Glob: "*.yaml"}, false},
		{"Grep", grepInput{Pattern: "version", Path: "deploy", Glob: "*.toml"}, false},
		{"Bash", bashInput{Command: "grep -rn version internal/"}, true},
		{"Bash", bashInput{Command: "rg version cmd/main.go"}, true},
		{"Grep", grepInput{Pattern: "version"}, true},
		{"Grep", grepInput{Pattern: "version", Type: "go"}, true},
	} {
		question := Classify(payloadOf(t, testCase.tool, testCase.input))
		if question.Kind != KindSymbol {
			t.Fatalf("%s %#v classified as %#v, want a symbol search", testCase.tool, testCase.input, question)
		}
		if decision := gate.Decide(context.Background(), question); decision.Deny != testCase.deny {
			t.Fatalf("%s %#v (paths %v): deny = %v, want %v", testCase.tool, testCase.input, question.Paths, decision.Deny, testCase.deny)
		}
	}
}
