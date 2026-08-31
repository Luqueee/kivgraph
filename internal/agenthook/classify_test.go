package agenthook

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"
)

// payloadOf builds a payload the way an agent sends one.
func payloadOf(t *testing.T, tool string, input any) Payload {
	t.Helper()
	raw, err := json.Marshal(input)
	if err != nil {
		t.Fatalf("marshal tool input: %v", err)
	}
	return Payload{HookEventName: "PreToolUse", CWD: "/repo", ToolName: tool, ToolInput: raw}
}

// TestClassifyRefusesToGateWhatGrepAnswersBetter is the negative half, and it
// is the half that decides whether the gate is usable. Every call here is one a
// developer makes constantly; a gate that refuses any of them is worse than no
// gate, because the escape hatch costs more than the search did.
func TestClassifyRefusesToGateWhatGrepAnswersBetter(t *testing.T) {
	for _, testCase := range []struct {
		name    string
		tool    string
		input   any
		because string
	}{
		{"a listing", "Bash", bashInput{Command: "ls -la"},
			"not a search at all"},
		{"a git log", "Bash", bashInput{Command: "git log --oneline -5"},
			"history is not in the graph"},
		{"free text", "Bash", bashInput{Command: `grep -rn "TODO fix this later" .`},
			"the graph has no text index and would answer worse"},
		{"prose in a regex", "Grep", grepInput{Pattern: "the.*thing"},
			"punctuation around ordinary words is not a name"},
		{"a short pattern", "Grep", grepInput{Pattern: "id"},
			"two characters name nothing"},
		{"a build", "Bash", bashInput{Command: "go build ./... && go test ./..."},
			"neither segment searches"},
		{"find by age", "Bash", bashInput{Command: "find . -mtime -1"},
			"no filename predicate, so not a question about files"},
		{"the escape hatch", "Bash",
			bashInput{Command: "KIVGRAPH_DISABLE_HOOK=1 grep -rn NewServer ."},
			"the caller said it already knows"},
		{"a writing subagent", "Task",
			agentInput{SubagentType: "statusline-setup", Prompt: "configure the status line"},
			"refusing it would refuse the task, not redirect the question"},
		{"an unknown tool", "WebFetch", map[string]string{"url": "https://example.com"},
			"a tool the gate cannot recognise is not known to be a search"},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			question := Classify(payloadOf(t, testCase.tool, testCase.input))
			if question.Kind != KindNone {
				t.Fatalf("gated %s (%s): got %#v", testCase.name, testCase.because, question)
			}
		})
	}
}

// TestClassifyReadsTheQuestionOutOfTheCall covers the calls the gate exists
// for, and checks the whole question rather than its kind: routing a symbol
// search to the intent tool would pass a kind-only assertion and still give the
// caller a worse answer than the grep it refused.
func TestClassifyReadsTheQuestionOutOfTheCall(t *testing.T) {
	for _, testCase := range []struct {
		name  string
		tool  string
		input any
		want  Question
	}{
		{"a recursive grep for a name", "Bash",
			bashInput{Command: `grep -rn "NewServer" .`},
			Question{Kind: KindSymbol, Pattern: "NewServer", Paths: []string{"."}, Tool: "grep"}},
		{"a declaration search", "Bash",
			bashInput{Command: `rg "func NewServer" internal/`},
			Question{Kind: KindSymbol, Pattern: "NewServer", Paths: []string{"internal/"}, Tool: "rg"}},
		{"a pattern behind -e", "Bash",
			bashInput{Command: `grep -e NewServer -rn internal`},
			Question{Kind: KindSymbol, Pattern: "NewServer", Paths: []string{"internal"}, Tool: "grep"}},
		{"git grep", "Bash",
			bashInput{Command: `git grep NewServer`},
			Question{Kind: KindSymbol, Pattern: "NewServer", Paths: []string{}, Tool: "grep"}},
		{"a search after a pipe", "Bash",
			bashInput{Command: `cat x | rg Indexer_new`},
			Question{Kind: KindSymbol, Pattern: "Indexer_new", Paths: []string{}, Tool: "rg"}},
		{"a qualified name", "Grep",
			grepInput{Pattern: "graph.NewServer"},
			Question{Kind: KindSymbol, Pattern: "graph.NewServer", Tool: "grep"}},
		{"a name the caller cannot spell", "Grep",
			grepInput{Pattern: "New.*Server", Glob: "*.go"},
			Question{Kind: KindIntent, Pattern: "New.*Server", Paths: []string{"*.go"}, Tool: "grep"}},
		{"a glob over sources", "Glob",
			globInput{Pattern: "**/*.go"},
			Question{Kind: KindFiles, Pattern: "**/*.go", Paths: []string{"**/*.go"}, Tool: "glob"}},
		{"find by name", "Bash",
			bashInput{Command: `find . -name "*.go"`},
			Question{Kind: KindFiles, Pattern: "*.go", Paths: []string{".", "*.go"}, Tool: "find"}},
		{"a research subagent", "Task",
			agentInput{SubagentType: "Explore", Description: "find where indexing happens"},
			Question{Kind: KindResearchAgent, Pattern: "find where indexing happens", Tool: "agent"}},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			got := Classify(payloadOf(t, testCase.tool, testCase.input))
			if !questionsEqual(got, testCase.want) {
				t.Fatalf("got %#v, want %#v", got, testCase.want)
			}
		})
	}
}

// TestClassifyReadsEveryDialectsSpelling defends the reason Dialect exists: the
// same call, spelled as each supported agent spells it, has to read as the
// same question. A classifier that only knew Claude Code's capitals would let
// every lowercase module call through in silence.
func TestClassifyReadsEveryDialectsSpelling(t *testing.T) {
	for _, tool := range []string{"Bash", "bash", "shell"} {
		question := Classify(payloadOf(t, tool, bashInput{Command: "grep -rn NewServer ."}))
		if question.Kind != KindSymbol {
			t.Fatalf("tool %q read as %#v", tool, question)
		}
	}
	for _, tool := range []string{"Task", "Agent", "task"} {
		question := Classify(payloadOf(t, tool,
			agentInput{SubagentType: "explore", Description: "map the indexer"}))
		if question.Kind != KindResearchAgent {
			t.Fatalf("tool %q read as %#v", tool, question)
		}
	}
}

func questionsEqual(got, want Question) bool {
	if got.Kind != want.Kind || got.Pattern != want.Pattern || got.Tool != want.Tool {
		return false
	}
	if len(got.Paths) != len(want.Paths) {
		return false
	}
	for index := range got.Paths {
		if got.Paths[index] != want.Paths[index] {
			return false
		}
	}
	return true
}

// TestFirstLineBoundsWhatARefusalQuotes keeps a subagent's prompt from arriving
// in an error message whole. The refusal is one line the caller reads, not a
// transcript of what it asked for.
func TestFirstLineBoundsWhatARefusalQuotes(t *testing.T) {
	for _, testCase := range []struct{ prompt, want string }{
		{"find where indexing happens", "find where indexing happens"},
		{"find the indexer. Then read it.", "find the indexer"},
		{"first line\nsecond line", "first line"},
		{"  padded  ", "padded"},
		{"", ""},
	} {
		if got := firstLine(testCase.prompt); got != testCase.want {
			t.Fatalf("firstLine(%q) = %q, want %q", testCase.prompt, got, testCase.want)
		}
	}
	long := firstLine(strings.Repeat("word ", 60))
	if len([]rune(long)) > 121 {
		t.Fatalf("a long prompt was quoted at %d runes", len([]rune(long)))
	}
	if !strings.HasSuffix(long, "…") {
		t.Fatalf("a clipped prompt does not say it was clipped: %q", long)
	}
}

// TestGraphToolsAreRecognisedWithoutSwallowingNeighbours pins both sides of the
// prefix match.
//
// The gate keys on the server name rather than a table of operations, so the
// risk is not a tool it misses but a tool it claims: another MCP server whose
// name merely starts the same way would be briefed as if it were Kivgraph's,
// and every one of its calls would be classified by a gate that knows nothing
// about it. The negatives come first for that reason.
func TestGraphToolsAreRecognisedWithoutSwallowingNeighbours(t *testing.T) {
	for _, testCase := range []struct {
		name string
		tool string
		want Kind
	}{
		{"a server whose name extends kivgraph", "mcp__kivgraphx_find_references", KindNone},
		{"a different server entirely", "mcp__ripgrep__search", KindNone},
		{"the bare server name with no operation", "mcp__kivgraph", KindNone},
		{"an unrelated tool that merely contains the name", "run_kivgraph_index", KindNone},
		{"claude code's spelling", "mcp__kivgraph__find_references", KindGraphTool},
		{"the oh my pi spelling", "mcp__kivgraph_get_source", KindGraphTool},
		{"through 1mcp", "kivgraph_1mcp_get_blast_radius", KindGraphTool},
		{"a tool the server has not grown yet", "mcp__kivgraph__some_future_tool", KindGraphTool},
		{"spelled by a host that shouts", "MCP__KIVGRAPH__FIND_SYMBOL", KindGraphTool},
		{"padded by the host", "  mcp__kivgraph__find_symbol  ", KindGraphTool},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			got := Classify(Payload{ToolName: testCase.tool})
			if got.Kind != testCase.want {
				t.Fatalf("classified %q as kind %d, want %d", testCase.tool, got.Kind, testCase.want)
			}
		})
	}
}

// TestGraphToolCarriesItsSpellingAndNothingElse checks that the arguments are
// not read.
//
// Every Kivgraph tool takes a different shape and none of them changes whether
// the session has been briefed, so a classifier that parsed them would be doing
// work whose result is discarded -- and would be a place for a malformed
// argument to turn a briefing into an error.
func TestGraphToolCarriesItsSpellingAndNothingElse(t *testing.T) {
	got := Classify(Payload{
		ToolName:  "mcp__kivgraph__find_references",
		ToolInput: json.RawMessage(`{"this is not":[valid json at all`),
	})
	want := Question{Kind: KindGraphTool, Tool: "mcp__kivgraph__find_references"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %#v, want %#v", got, want)
	}
}

// TestPayloadReadsTheSessionId is the field the briefing cannot work without.
func TestPayloadReadsTheSessionId(t *testing.T) {
	var payload Payload
	raw := `{"hook_event_name":"PreToolUse","cwd":"/w","tool_name":"Bash","session_id":"abc-123"}`
	if err := json.Unmarshal([]byte(raw), &payload); err != nil {
		t.Fatalf("decode payload: %v", err)
	}
	if payload.SessionID != "abc-123" {
		t.Fatalf("session id is %q; the briefing cannot be once per anything without it", payload.SessionID)
	}
}
