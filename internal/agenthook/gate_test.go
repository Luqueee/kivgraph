package agenthook

import (
	"context"
	"errors"
	"strings"
	"testing"
)

// fakeGraph states facts outright, so a decision can be tested against the
// shape of the graph rather than against a daemon that happens to be running.
type fakeGraph struct {
	symbol Facts
	intent Facts
	err    error
	asked  int
}

func (fake *fakeGraph) Symbol(_ context.Context, _ string) (Facts, error) {
	fake.asked++
	return fake.symbol, fake.err
}

func (fake *fakeGraph) Intent(_ context.Context, _ string) (Facts, error) {
	fake.asked++
	return fake.intent, fake.err
}

// TestGateStandsAsideWhenItHasNoGrounds is the negative half. Each case is a
// reason the gate cannot honestly claim the graph answers better, and in every
// one of them the call has to go through untouched.
func TestGateStandsAsideWhenItHasNoGrounds(t *testing.T) {
	unique := Facts{Declarations: 1, Repositories: 1, References: 2}
	for _, testCase := range []struct {
		name     string
		gate     Gate
		question Question
		because  string
	}{
		{"no daemon is running",
			Gate{}, Question{Kind: KindSymbol, Pattern: "NewServer"},
			"a hook never starts a daemon, so an absent one is the common case"},
		{"the daemon failed",
			Gate{Graph: &fakeGraph{err: errors.New("connection refused")}},
			Question{Kind: KindSymbol, Pattern: "NewServer"},
			"a gate that learned nothing has no standing to refuse"},
		{"the graph has never heard of it",
			Gate{Graph: &fakeGraph{}}, Question{Kind: KindSymbol, Pattern: "NewServer"},
			"grep reaches comments, strings and config the graph does not index"},
		{"a rare name in one repository",
			Gate{Graph: &fakeGraph{symbol: unique}},
			Question{Kind: KindSymbol, Pattern: "MergeAll"},
			"mcp-token-cost measures this very symbol at 0,85x -- ours to lose"},
		{"a unique name that many places use",
			Gate{Graph: &fakeGraph{symbol: Facts{
				Declarations: 1, Repositories: 1, References: 240}}},
			Question{Kind: KindSymbol, Pattern: "Load"},
			"no corpus measures a name this busy, so any floor would be invented"},
		{"a glob over files nobody indexed",
			Gate{Indexed: IndexedExtensions([]string{".go"})},
			Question{Kind: KindFiles, Pattern: "**/*.md"},
			"markdown is not in the graph"},
		{"a glob with no extension at all",
			Gate{Indexed: IndexedExtensions([]string{".go"})},
			Question{Kind: KindFiles, Pattern: "**/*"},
			"that is not a question about code"},
		{"a call the classifier had no opinion about",
			Gate{Graph: &fakeGraph{symbol: Facts{Declarations: 9, Repositories: 4}}},
			Question{Kind: KindNone, Pattern: "anything"},
			"the gate refuses searches, and this was not one"},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			if decision := testCase.gate.Decide(context.Background(), testCase.question); decision.Deny {
				t.Fatalf("refused %s (%s): %s", testCase.name, testCase.because, decision.Reason)
			}
		})
	}
}

// TestGateRefusesWhatTheGraphAnswersBetter covers the two positive facts that
// justify a refusal, and checks that the refusal names the call to make
// instead: a redirect without a next call is just an obstacle.
func TestGateRefusesWhatTheGraphAnswersBetter(t *testing.T) {
	for _, testCase := range []struct {
		name     string
		gate     Gate
		question Question
		names    string
	}{
		{"an ambiguous name",
			Gate{Graph: &fakeGraph{symbol: Facts{
				Declarations: 4, Repositories: 2, References: 60,
				Sample: []string{"kena internal/mcp/server.go:41 mcp.NewServer"}}}},
			Question{Kind: KindSymbol, Pattern: "NewServer"},
			"find_references"},
		{"a name several repositories share",
			Gate{Graph: &fakeGraph{symbol: Facts{
				Declarations: 3, Repositories: 2, References: 60}}},
			Question{Kind: KindSymbol, Pattern: "Publish"},
			"get_blast_radius"},
		{"a regex groping for a name",
			Gate{Graph: &fakeGraph{intent: Facts{Declarations: 3, Repositories: 1}}},
			Question{Kind: KindIntent, Pattern: "New.*Server"},
			"find_by_intent"},
		{"a glob over indexed sources",
			Gate{Indexed: IndexedExtensions([]string{".go"})},
			Question{Kind: KindFiles, Pattern: "**/*.go"},
			"get_file_outline"},
		{"a subagent sent to read the codebase",
			Gate{}, Question{Kind: KindResearchAgent, Pattern: "find where indexing happens"},
			"find_by_intent"},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			decision := testCase.gate.Decide(context.Background(), testCase.question)
			if !decision.Deny {
				t.Fatalf("allowed %s", testCase.name)
			}
			if !strings.Contains(decision.Reason, testCase.names) {
				t.Fatalf("refusal names no %s call:\n%s", testCase.names, decision.Reason)
			}
			if !strings.Contains(decision.Reason, DisableVariable) {
				t.Fatalf("refusal offers no way to insist:\n%s", decision.Reason)
			}
		})
	}
}

// TestASubagentIsRefusedWithoutAskingTheGraph holds the one branch that needs
// no facts. Asking would be a round trip whose answer could not change the
// decision, and a hook pays that cost on the user's keystroke.
func TestASubagentIsRefusedWithoutAskingTheGraph(t *testing.T) {
	graph := &fakeGraph{symbol: Facts{Declarations: 1, Repositories: 1}}
	decision := Gate{Graph: graph}.Decide(context.Background(),
		Question{Kind: KindResearchAgent, Pattern: "map the indexer"})
	if !decision.Deny {
		t.Fatal("allowed a research subagent")
	}
	if graph.asked != 0 {
		t.Fatalf("asked the graph %d times for a decision it cannot change", graph.asked)
	}
}
