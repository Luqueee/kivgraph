package agenthook

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
)

// TestMalformedToolInputIsNotASearch covers every branch that decodes a tool's
// arguments. An agent that sends a shape we cannot read has told us nothing,
// and the gate refuses only what it can positively describe -- so each of these
// has to fall out as KindNone rather than as a half-read question.
func TestMalformedToolInputIsNotASearch(t *testing.T) {
	for _, testCase := range []struct{ name, tool, input string }{
		{"grep arguments that are not an object", "Grep", `"pattern"`},
		{"grep with no pattern at all", "Grep", `{"path":"internal"}`},
		{"glob arguments that are not an object", "Glob", `[1,2]`},
		{"glob with no pattern", "Glob", `{"path":"."}`},
		{"agent arguments that are not an object", "Task", `null`},
		{"bash arguments that are not an object", "Bash", `42`},
		{"bash with an empty command", "Bash", `{"command":""}`},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			question := Classify(Payload{
				ToolName: testCase.tool, ToolInput: json.RawMessage(testCase.input)})
			if question.Kind != KindNone {
				t.Fatalf("read %#v out of unreadable input", question)
			}
		})
	}
}

// TestASubagentWithNothingToQuoteStillGetsAnActionableRefusal keeps the message
// from naming an empty string as the thing to look for.
func TestASubagentWithNothingToQuoteStillGetsAnActionableRefusal(t *testing.T) {
	question := Classify(Payload{ToolName: "Task",
		ToolInput: json.RawMessage(`{"subagent_type":"explore"}`)})
	if question.Kind != KindResearchAgent {
		t.Fatalf("question = %#v", question)
	}
	decision := Gate{}.Decide(context.Background(), question)
	if !decision.Deny {
		t.Fatal("a research subagent was allowed")
	}
	if strings.Contains(decision.Reason, `intent="")`) {
		t.Fatalf("the refusal quotes an empty intent:\n%s", decision.Reason)
	}
}

// TestARefusalCountsRepositoriesInWords is small and it is what a reader sees:
// "1 repositories" reads as a bug in the tool that printed it.
func TestARefusalCountsRepositoriesInWords(t *testing.T) {
	for _, testCase := range []struct {
		facts Facts
		want  string
	}{
		{Facts{Declarations: 2, Repositories: 1}, "in 1 repository"},
		{Facts{Declarations: 4, Repositories: 3}, "in 3 repositories"},
	} {
		decision := Gate{Graph: &fakeGraph{symbol: testCase.facts}}.Decide(
			context.Background(), Question{Kind: KindSymbol, Pattern: "Load"})
		if !strings.Contains(decision.Reason, testCase.want) {
			t.Fatalf("refusal does not say %q:\n%s", testCase.want, decision.Reason)
		}
	}
}

// TestAnIntentRefusalSurvivesHavingNoRowsToShow covers the branch where the
// graph is confident enough to answer but the page carried no sample.
func TestAnIntentRefusalSurvivesHavingNoRowsToShow(t *testing.T) {
	decision := Gate{Graph: &fakeGraph{intent: Facts{Declarations: 2}}}.Decide(
		context.Background(), Question{Kind: KindIntent, Pattern: "New.*Server"})
	if !decision.Deny {
		t.Fatal("allowed a regex the graph could answer")
	}
	if strings.Contains(decision.Reason, "It matches:") {
		t.Fatalf("the refusal promises rows it does not have:\n%s", decision.Reason)
	}
	if !strings.Contains(decision.Reason, "find_by_intent") {
		t.Fatalf("the refusal names no call:\n%s", decision.Reason)
	}
}

// TestAnIntentTheGraphCannotPlaceIsAllowed is the negative beside it.
func TestAnIntentTheGraphCannotPlaceIsAllowed(t *testing.T) {
	decision := Gate{Graph: &fakeGraph{}}.Decide(
		context.Background(), Question{Kind: KindIntent, Pattern: "New.*Server"})
	if decision.Deny {
		t.Fatalf("refused a regex the graph could not place:\n%s", decision.Reason)
	}
}

// TestAnAbsolutePathNamesTheProgramItRuns keeps a shell command from escaping
// the gate by spelling its search out in full.
func TestAnAbsolutePathNamesTheProgramItRuns(t *testing.T) {
	for _, command := range []string{
		`/usr/bin/grep -rn NewServer .`,
		`/opt/homebrew/bin/rg NewServer`,
		`grep -rn NewServer .`,
	} {
		question := Classify(Payload{ToolName: "Bash",
			ToolInput: json.RawMessage(`{"command":` + quoteJSON(command) + `}`)})
		if question.Kind != KindSymbol {
			t.Fatalf("%q read as %#v", command, question)
		}
	}
}

// TestAnAnswerWithNoTextIsReadAsEmpty covers a tool result carrying content the
// gate cannot read, which has to decode as nothing rather than panic.
func TestAnAnswerWithNoTextIsReadAsEmpty(t *testing.T) {
	if facts := ambiguityFacts(""); facts.Known() {
		t.Fatalf("an empty answer produced facts: %#v", facts)
	}
}

func quoteJSON(text string) string {
	encoded, err := json.Marshal(text)
	if err != nil {
		panic(err)
	}
	return string(encoded)
}
