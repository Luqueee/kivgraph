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
		{"an rtk status command", "Bash", bashInput{Command: "rtk gain --history"},
			"the wrapper's own command is not a source search"},
		{"rtk without a command", "Bash", bashInput{Command: "rtk"},
			"a missing wrapped command cannot be classified"},
		{"rtk proxy without a command", "Bash", bashInput{Command: "rtk proxy"},
			"a missing proxied command cannot be classified"},
		{"free text", "Bash", bashInput{Command: `grep -rn "TODO fix this later" .`},
			"the graph has no text index and would answer worse"},
		{"prose in a regex", "Grep", grepInput{Pattern: "the.*thing"},
			"punctuation around ordinary words is not a name"},
		{"a short lowercase alternation", "Bash",
			bashInput{Command: `grep -E 'error|warning|failed' app.log`},
			"three exact log levels are still a text search, not broad code discovery"},
		{"four alternatives with a regular expression branch", "Grep",
			grepInput{Pattern: "plain|lower|words|thing.*"},
			"every branch must be an identifier before a lowercase alternation is intent"},
		{"a short pattern", "Grep", grepInput{Pattern: "id"},
			"two characters name nothing"},
		{"a build", "Bash", bashInput{Command: "go build ./... && go test ./..."},
			"neither segment searches"},
		{"rg file listing under a directory", "Bash", bashInput{Command: "rg --files internal | sort"},
			"the directory is a scope, not a symbol pattern"},
		{"rg file listing missing its glob", "Bash", bashInput{Command: "rg --files -g"},
			"a missing flag value cannot become a pattern"},
		{"rg file listing with only an exclusion", "Bash",
			bashInput{Command: "rg --files --glob=!vendor/**"},
			"an exclusion says what not to list, not what source files to find"},
		{"rg files after the option terminator", "Bash",
			bashInput{Command: "rg --glob '*.go' -- --files"},
			"an argument after -- is a search pattern, not file-listing mode"},
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
				t.Fatalf("tool=%q input=%#v gated %s (%s): got %#v",
					testCase.tool, testCase.input, testCase.name, testCase.because, question)
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
		{"four lowercase concepts", "Grep", grepInput{Pattern: "http|route|handler|serve"},
			Question{Kind: KindIntent, Pattern: "http|route|handler|serve", Tool: "grep"}},
		{"broad lowercase concepts from a natural-language task", "Bash",
			bashInput{Command: `rg -l -i 'http|route|handler|listen|serve' --glob '*.go' --glob '*.md'`},
			Question{Kind: KindIntent, Pattern: "http|route|handler|listen|serve", Paths: []string{}, Tool: "rg"}},
		{"an intent search behind rtk", "Bash",
			bashInput{Command: `rtk rg -n "IMPLEMENTS|CALLS|IMPORTS|interface|hierarchy|diagnostic|rename|edit|memory|onboard|query_project|external" internal/mcp internal/hotsnapshot internal/facts docs/adr docs/protocol | sed -n '1,280p'`},
			Question{Kind: KindIntent,
				Pattern: "IMPLEMENTS|CALLS|IMPORTS|interface|hierarchy|diagnostic|rename|edit|memory|onboard|query_project|external",
				Paths:   []string{"internal/mcp", "internal/hotsnapshot", "internal/facts", "docs/adr", "docs/protocol"}, Tool: "rg"}},
		{"a symbol search behind rtk proxy", "Bash",
			bashInput{Command: `/usr/local/bin/rtk proxy rg -n NewServer internal`},
			Question{Kind: KindSymbol, Pattern: "NewServer", Paths: []string{"internal"}, Tool: "rg"}},
		{"a glob over sources", "Glob",
			globInput{Pattern: "**/*.go"},
			Question{Kind: KindFiles, Pattern: "**/*.go", Paths: []string{"**/*.go"}, Tool: "glob"}},
		{"rg file listing with a source glob", "Bash",
			bashInput{Command: `rg --files -g '*.go' internal | sort`},
			Question{Kind: KindFiles, Pattern: "*.go", Paths: []string{"internal", "*.go"}, Tool: "rg"}},
		{"rg file listing skips exclusions and valued flags", "Bash",
			bashInput{Command: `rg --files --glob='!vendor/**' --glob='*.go' --type go --hidden internal`},
			Question{Kind: KindFiles, Pattern: "*.go", Paths: []string{"internal", "*.go"}, Tool: "rg"}},
		{"rg file listing tolerates a missing type", "Bash",
			bashInput{Command: `rg --files --glob=*.go --type`},
			Question{Kind: KindFiles, Pattern: "*.go", Paths: []string{"*.go"}, Tool: "rg"}},
		{"rg file listing treats post-terminator options as paths", "Bash",
			bashInput{Command: `rg --files --glob=*.go -- --hidden`},
			Question{Kind: KindFiles, Pattern: "*.go", Paths: []string{"--hidden", "*.go"}, Tool: "rg"}},
		{"find by name", "Bash",
			bashInput{Command: `find . -name "*.go"`},
			Question{Kind: KindFiles, Pattern: "*.go", Paths: []string{".", "*.go"}, Tool: "find"}},
		{"a research subagent", "Task",
			agentInput{SubagentType: "Explore", Description: "find where indexing happens"},
			Question{Kind: KindResearchAgent, Pattern: "find where indexing happens", Tool: "agent"}},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			got := Classify(payloadOf(t, testCase.tool, testCase.input))
			if !reflect.DeepEqual(got, testCase.want) {
				t.Fatalf("tool=%q input=%#v got %#v, want %#v",
					testCase.tool, testCase.input, got, testCase.want)
			}
		})
	}
}

// TestClassifyReadsEveryDialectsSpelling defends the reason Dialect exists: the
// same call, spelled as each supported agent spells it, has to read as the
// same question. A classifier that only knew Claude Code's capitals would let
// every lowercase module call through in silence.
func TestClassifyReadsEveryDialectsSpelling(t *testing.T) {
	for _, testCase := range []struct {
		tool  string
		input any
		want  Question
	}{
		{"Bash", bashInput{Command: "grep -rn NewServer ."},
			Question{Kind: KindSymbol, Pattern: "NewServer", Paths: []string{"."}, Tool: "grep"}},
		{"bash", bashInput{Command: "grep -rn NewServer ."},
			Question{Kind: KindSymbol, Pattern: "NewServer", Paths: []string{"."}, Tool: "grep"}},
		{"shell", bashInput{Command: "grep -rn NewServer ."},
			Question{Kind: KindSymbol, Pattern: "NewServer", Paths: []string{"."}, Tool: "grep"}},
	} {
		got := Classify(payloadOf(t, testCase.tool, testCase.input))
		if !reflect.DeepEqual(got, testCase.want) {
			t.Fatalf("tool=%q input=%#v classified as %#v, want %#v",
				testCase.tool, testCase.input, got, testCase.want)
		}
	}
	for _, tool := range []string{"Task", "Agent", "task"} {
		input := agentInput{SubagentType: "explore", Description: "map the indexer"}
		want := Question{Kind: KindResearchAgent, Pattern: "map the indexer", Tool: "agent"}
		got := Classify(payloadOf(t, tool, input))
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("tool=%q input=%#v classified as %#v, want %#v", tool, input, got, want)
		}
	}
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

// TestPayloadReadsHostIdentity checks the two temporal fields that affect the
// protocol: session_id scopes a briefing, while Codex's turn_id selects its
// exit-code refusal wire.
func TestPayloadReadsHostIdentity(t *testing.T) {
	var payload Payload
	raw := `{"hook_event_name":"PreToolUse","cwd":"/w","tool_name":"Bash",` +
		`"session_id":"abc-123","turn_id":"turn-456"}`
	if err := json.Unmarshal([]byte(raw), &payload); err != nil {
		t.Fatalf("decode payload %q: %v", raw, err)
	}
	if payload.SessionID != "abc-123" {
		t.Fatalf("payload %q session id is %q; the briefing cannot be once per anything without it",
			raw, payload.SessionID)
	}
	if payload.Dialect() != DialectCodex {
		t.Fatalf("payload %q turn_id selected dialect %q, want %q",
			raw, payload.Dialect(), DialectCodex)
	}
	payload.TurnID = ""
	if payload.Dialect() != "" {
		t.Fatalf("payload %q without turn_id selected dialect %q", raw, payload.Dialect())
	}
}
