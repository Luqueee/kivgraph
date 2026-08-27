package main

import (
	"strings"
	"testing"

	"github.com/Luqueee/kivgraph/internal/config"
)

// TestTheGateStandsAsideOnEveryFailure is the contract the whole command
// rests on. A hook runs in front of a tool the user asked for, so every way
// this can fail has to end in silence: writing nothing leaves the agent's own
// permission flow untouched, and writing anything else refuses a call the gate
// never formed an opinion about.
func TestTheGateStandsAsideOnEveryFailure(t *testing.T) {
	for _, testCase := range []struct {
		name    string
		stdin   string
		because string
	}{
		{"empty input", "", "an agent that sent nothing asked nothing"},
		{"not json", "not json at all", "a payload we cannot read teaches us nothing"},
		{"json that is not a payload", `["a","b"]`, "same"},
		{"no tool name", `{"cwd":"/tmp"}`, "there is no call to have an opinion about"},
		{"a tool the gate does not know", `{"cwd":"/tmp","tool_name":"WebFetch",` +
			`"tool_input":{"url":"https://example.com"}}`, "not a search"},
		{"a working directory no repository holds",
			`{"cwd":"/nonexistent/elsewhere","tool_name":"Bash",` +
				`"tool_input":{"command":"grep -rn NewServer ."}}`,
			"grep is the only tool that works outside the graph"},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			var out strings.Builder
			if code := runHookRun(strings.NewReader(testCase.stdin), &out); code != 0 {
				t.Fatalf("exit %d; a non-zero exit is itself a refusal", code)
			}
			if out.Len() != 0 {
				t.Fatalf("wrote %q (%s)", out.String(), testCase.because)
			}
		})
	}
}

// TestRepositoryHoldingAnswersWithTheInnermostMatch defends the reason this
// asks the registry instead of walking up for a marker directory: the graph
// spans several repositories by absolute path, and a nested one has its own
// languages. Answering with the outer repository would gate a Dart file with
// Go's extensions.
func TestRepositoryHoldingAnswersWithTheInnermostMatch(t *testing.T) {
	loaded := config.Loaded{Repositories: config.RepositoriesFile{Repositories: []config.Repository{
		{Name: "outer", Path: "/src/outer", Languages: []string{"go"}},
		{Name: "inner", Path: "/src/outer/packages/inner", Languages: []string{"dart"}},
	}}}
	for _, testCase := range []struct {
		cwd  string
		want string
	}{
		{"/src/outer", "outer"},
		{"/src/outer/internal/mcp", "outer"},
		{"/src/outer/packages/inner", "inner"},
		{"/src/outer/packages/inner/lib", "inner"},
	} {
		repository, found := repositoryHolding(loaded, testCase.cwd)
		if !found || repository.Name != testCase.want {
			t.Fatalf("cwd %q: got %q (found=%v), want %q",
				testCase.cwd, repository.Name, found, testCase.want)
		}
	}
}

// TestRepositoryHoldingRefusesWhatIsMerelyNextDoor is the negative: a sibling
// whose path shares a prefix is not inside.
func TestRepositoryHoldingRefusesWhatIsMerelyNextDoor(t *testing.T) {
	loaded := config.Loaded{Repositories: config.RepositoriesFile{Repositories: []config.Repository{
		{Name: "outer", Path: "/src/outer", Languages: []string{"go"}},
	}}}
	for _, cwd := range []string{"/src/outerly", "/src", "/", "", "/elsewhere"} {
		if repository, found := repositoryHolding(loaded, cwd); found {
			t.Fatalf("cwd %q read as inside %q", cwd, repository.Name)
		}
	}
}

// TestTheEscapeHatchIsReadTheWayAShellWouldWriteIt keeps the off switch usable:
// a user who exports it expects it to hold, and one who sets it to 0 or false
// expects it not to.
func TestTheEscapeHatchIsReadTheWayAShellWouldWriteIt(t *testing.T) {
	for _, value := range []string{"1", "true", "yes", "on"} {
		if !disabled(value) {
			t.Fatalf("%q did not turn the gate off", value)
		}
	}
	for _, value := range []string{"", "0", "false", "False", "  "} {
		if disabled(value) {
			t.Fatalf("%q turned the gate off", value)
		}
	}
}
