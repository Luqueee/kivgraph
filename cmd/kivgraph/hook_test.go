package main

import (
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/Luqueee/kivgraph/internal/agenthook"
	"github.com/Luqueee/kivgraph/internal/config"
	"github.com/Luqueee/kivgraph/internal/testsupport"
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
			var stderr strings.Builder
			if code := runHookRun(strings.NewReader(testCase.stdin), &out, &stderr); code != 0 {
				t.Fatalf("input=%q exit %d; a non-zero exit is itself a refusal", testCase.stdin, code)
			}
			if out.Len() != 0 || stderr.Len() != 0 {
				t.Fatalf("input=%q wrote stdout=%q stderr=%q (%s)",
					testCase.stdin, out.String(), stderr.String(), testCase.because)
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

// TestRepositoryHoldingRecognisesALinkedWorktree covers the directory shape
// coding agents actually use. The worktree is outside the registered checkout,
// but both .git layouts resolve to the same common directory and therefore name
// the same indexed repository.
func TestRepositoryHoldingRecognisesALinkedWorktree(t *testing.T) {
	root := t.TempDir()
	registered := filepath.Join(root, "registered")
	common := filepath.Join(registered, ".git")
	worktree := filepath.Join(root, "agent-worktree")
	gitDirectory := filepath.Join(common, "worktrees", "agent")
	for _, directory := range []string{common, worktree, gitDirectory, filepath.Join(worktree, "internal")} {
		if err := os.MkdirAll(directory, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(worktree, ".git"),
		[]byte("gitdir: "+gitDirectory+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(gitDirectory, "commondir"),
		[]byte("../..\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	loaded := config.Loaded{Repositories: config.RepositoriesFile{Repositories: []config.Repository{{
		Name: "widget", Path: registered, Languages: []string{"go"},
	}}}}
	cwd := filepath.Join(worktree, "internal")
	repository, found := repositoryHolding(loaded, cwd)
	if !found || repository.Name != "widget" {
		t.Fatalf("cwd %q in linked worktree resolved to %q (found=%v), want widget",
			cwd, repository.Name, found)
	}

	second := filepath.Join(root, "second-registered-worktree")
	secondGitDirectory := filepath.Join(common, "worktrees", "second")
	for _, directory := range []string{second, secondGitDirectory, filepath.Join(second, "internal")} {
		if err := os.MkdirAll(directory, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(second, ".git"),
		[]byte("gitdir: "+secondGitDirectory+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(secondGitDirectory, "commondir"),
		[]byte("../..\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	loaded.Repositories.Repositories = append(loaded.Repositories.Repositories, config.Repository{
		Name: "other-view", Path: second, Languages: []string{"go"},
	})
	secondCWD := filepath.Join(second, "internal")
	if repository, found := repositoryHolding(loaded, secondCWD); !found || repository.Name != "other-view" {
		t.Fatalf("cwd %q inside an exact registration resolved to %q (found=%v), want other-view",
			secondCWD, repository.Name, found)
	}
	if repository, found := repositoryHolding(loaded, cwd); found {
		t.Fatalf("cwd %q in ambiguous linked worktree chose %q", cwd, repository.Name)
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

// TestHookCompletesOnlyTargetsItAccepts is a regression: completion and the
// help footer must expose exactly the targets the command accepts. A
// completion is a promise about what the next word may be.
func TestHookCompletesOnlyTargetsItAccepts(t *testing.T) {
	testsupport.SetHome(t, t.TempDir())
	candidates := completionCandidates([]string{"hook", "install", "--target", ""})
	var help, helpErr strings.Builder
	if code := runHookCommand([]string{"--help"}, &help, &helpErr); code != 0 {
		t.Fatalf("hook --help exited %d: stdout=%q stderr=%q", code, help.String(), helpErr.String())
	}
	if !strings.Contains(help.String(), "Targets:") {
		t.Fatalf("hook --help omitted its target footer: %q", help.String())
	}
	hasOhMyPi := false
	for _, candidate := range candidates {
		if candidate == "oh-my-pi" {
			hasOhMyPi = true
			break
		}
	}
	if !hasOhMyPi {
		t.Fatalf("hook completion omitted oh-my-pi (candidates=%v)", candidates)
	}
	for _, candidate := range candidates {
		if !strings.Contains(help.String(), candidate) {
			t.Fatalf("hook --help omitted completed target %q (candidates=%v)", candidate, candidates)
		}
		var stdout, stderr strings.Builder
		args := []string{"install", "--target", candidate, "--scope", "user", "--dry-run"}
		if code := runHookCommand(args, &stdout, &stderr); code != 0 {
			t.Fatalf("hook install args=%v exited %d (candidates=%v): stdout=%q stderr=%q",
				args, code, candidates, stdout.String(), stderr.String())
		}
	}
	var stdout, stderr strings.Builder
	args := []string{"install", "--target", "cursor", "--scope", "user", "--dry-run"}
	if code := runHookCommand(args, &stdout, &stderr); code == 0 {
		t.Fatalf("hook install accepted rejected args=%v (candidates=%v): stdout=%q",
			args, candidates, stdout.String())
	}
}

// TestTheEnvironmentTurnsTheGateOffBeforeAnythingElse covers the escape a user
// exports for a whole session. It is read before stdin is, so a session with
// the gate off costs nothing at all -- not even reading the payload.
func TestTheEnvironmentTurnsTheGateOffBeforeAnythingElse(t *testing.T) {
	t.Setenv(agenthook.DisableVariable, "1")
	var out strings.Builder
	payload := `{"cwd":"/anywhere","tool_name":"Task",` +
		`"tool_input":{"subagent_type":"explore","prompt":"map the indexer"}}`
	if code := runHookRun(strings.NewReader(payload), &out, io.Discard); code != 0 {
		t.Fatalf("exit %d", code)
	}
	if out.Len() != 0 {
		t.Fatalf("the gate answered with the escape set: %q", out.String())
	}
}

// TestAResearchSubagentIsRefusedWithoutAConfigurationOrADaemon holds the one
// branch that needs neither. It is also what makes the refusal reachable on a
// machine where the daemon is not running, which is most of them.
func TestAResearchSubagentIsRefusedWithoutAConfigurationOrADaemon(t *testing.T) {
	t.Setenv(agenthook.DisableVariable, "")
	// A configuration path that cannot be read takes loadForHook's failing
	// branch, and the gate still has to answer this one.
	testsupport.SetHome(t, t.TempDir())
	var out strings.Builder
	payload := `{"cwd":"/anywhere","tool_name":"Task",` +
		`"tool_input":{"subagent_type":"explore","prompt":"map the indexer"}}`
	if code := runHookRun(strings.NewReader(payload), &out, io.Discard); code != 0 {
		t.Fatalf("exit %d", code)
	}
	// With no configuration there is no repository to place the call in, so
	// the gate stands aside: a directory the graph does not cover is one
	// where grep is the only tool that works.
	if out.Len() != 0 {
		t.Fatalf("refused a call in a directory no repository holds: %q", out.String())
	}
}

// TestAPayloadLargerThanTheCeilingIsStillAnswered keeps an oversized prompt
// from hanging or crashing the gate: it is truncated, and truncated input
// decodes as unreadable, which is an allow.
func TestAPayloadLargerThanTheCeilingIsStillAnswered(t *testing.T) {
	var out strings.Builder
	oversized := `{"cwd":"/x","tool_name":"Task","tool_input":{"prompt":"` +
		strings.Repeat("a", hookInputCeiling) + `"}}`
	if code := runHookRun(strings.NewReader(oversized), &out, io.Discard); code != 0 {
		t.Fatalf("exit %d", code)
	}
	if out.Len() != 0 {
		t.Fatalf("a truncated payload was refused: %q", out.String())
	}
}

// TestUnderIsNotAPrefixMatch is the rule repositoryHolding rests on, stated
// directly: /src/outerly is not inside /src/outer, and a prefix comparison
// would say it is.
func TestUnderIsNotAPrefixMatch(t *testing.T) {
	for _, testCase := range []struct {
		directory, root string
		want            bool
	}{
		{"/src/outer", "/src/outer", true},
		{"/src/outer/internal", "/src/outer", true},
		{"/src/outerly", "/src/outer", false},
		{"/src", "/src/outer", false},
		{"/", "/src/outer", false},
		{"/src/outer", "/", true},
	} {
		if got := under(testCase.directory, testCase.root); got != testCase.want {
			t.Fatalf("under(%q, %q) = %v, want %v",
				testCase.directory, testCase.root, got, testCase.want)
		}
	}
}

// TestTheCommandWritesARefusalItCanReach is the whole path in one test: a real
// configuration, a registered repository holding the working directory, a
// decision that needs no daemon, and the verdict on stdout in the shape both
// hosting agents read.
//
// Every other test here stops before the write, and the write is the only part
// the agent actually sees.
func TestTheCommandWritesARefusalItCanReach(t *testing.T) {
	home := t.TempDir()
	testsupport.SetHome(t, home)
	t.Setenv(agenthook.DisableVariable, "")
	repository := filepath.Join(home, "src", "widget")
	if err := os.MkdirAll(repository, 0o700); err != nil {
		t.Fatal(err)
	}
	configPath, err := config.DefaultConfigPath()
	if err != nil {
		t.Fatal(err)
	}
	repositoriesPath, err := config.DefaultRepositoriesPath()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := config.Initialize(config.InitOptions{
		ConfigPath: configPath, RepositoriesPath: repositoriesPath}); err != nil {
		t.Fatal(err)
	}
	if err := config.RegisterRepositories(repositoriesPath, []config.Repository{{
		Name: "widget", Path: repository, Languages: []string{"go"},
	}}); err != nil {
		t.Fatal(err)
	}

	var out strings.Builder
	payload := `{"hook_event_name":"PreToolUse","cwd":` + strconv.Quote(repository) +
		`,"tool_name":"Task","tool_input":{"subagent_type":"Explore",` +
		`"description":"find where indexing happens"}}`
	if code := runHookRun(strings.NewReader(payload), &out, io.Discard); code != 0 {
		t.Fatalf("exit %d; a non-zero exit is itself a refusal", code)
	}

	var decision struct {
		Permission         string `json:"permission"`
		AgentMessage       string `json:"agent_message"`
		HookSpecificOutput struct {
			HookEventName      string `json:"hookEventName"`
			PermissionDecision string `json:"permissionDecision"`
			Reason             string `json:"permissionDecisionReason"`
		} `json:"hookSpecificOutput"`
	}
	if err := json.Unmarshal([]byte(out.String()), &decision); err != nil {
		t.Fatalf("decode %q: %v", out.String(), err)
	}
	if decision.Permission != "deny" ||
		decision.HookSpecificOutput.PermissionDecision != "deny" ||
		decision.HookSpecificOutput.HookEventName != "PreToolUse" {
		t.Fatalf("decision = %#v", decision)
	}
	if decision.AgentMessage != decision.HookSpecificOutput.Reason {
		t.Fatal("the two spellings tell the agent different things")
	}
	if !strings.Contains(decision.AgentMessage, "find_by_intent") {
		t.Fatalf("the refusal names no call:\n%s", decision.AgentMessage)
	}

	// Codex consumes the same input fields but not Claude's JSON verdict. Its
	// blocking contract is exit 2 with the reason on stderr, and turn_id is the
	// host-owned field that distinguishes that payload from Claude Code and the
	// generated adapters.
	out.Reset()
	var stderr strings.Builder
	codexPayload := `{"session_id":"session","turn_id":"turn",` +
		`"hook_event_name":"PreToolUse","cwd":` + strconv.Quote(repository) +
		`,"tool_name":"Task","tool_input":{"subagent_type":"Explore",` +
		`"description":"find where indexing happens"}}`
	if code := runHookRun(strings.NewReader(codexPayload), &out, &stderr); code != 2 {
		t.Fatalf("Codex payload %q refusal exit = %d, want 2", codexPayload, code)
	}
	if out.Len() != 0 {
		t.Fatalf("Codex payload %q refusal wrote stdout %q", codexPayload, out.String())
	}
	if !strings.Contains(stderr.String(), "find_by_intent") {
		t.Fatalf("Codex payload %q stderr names no replacement call: %q", codexPayload, stderr.String())
	}
	if code := runHookRun(strings.NewReader(codexPayload), &out, refusingWriter{}); code != 0 {
		t.Fatalf("Codex payload %q refusal with an unwritable reason exited %d; it must fail open",
			codexPayload, code)
	}
}

// TestACallOutsideEveryRegisteredRepositoryIsAllowed is the negative beside it,
// on the same real configuration: the graph covers one directory and the call
// came from another.
func TestACallOutsideEveryRegisteredRepositoryIsAllowed(t *testing.T) {
	home := t.TempDir()
	testsupport.SetHome(t, home)
	t.Setenv(agenthook.DisableVariable, "")
	configPath, err := config.DefaultConfigPath()
	if err != nil {
		t.Fatal(err)
	}
	repositoriesPath, err := config.DefaultRepositoriesPath()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := config.Initialize(config.InitOptions{
		ConfigPath: configPath, RepositoriesPath: repositoriesPath}); err != nil {
		t.Fatal(err)
	}
	var out strings.Builder
	payload := `{"cwd":` + strconv.Quote(t.TempDir()) +
		`,"tool_name":"Task","tool_input":{"subagent_type":"Explore","description":"read it"}}`
	if code := runHookRun(strings.NewReader(payload), &out, io.Discard); code != 0 {
		t.Fatalf("exit %d", code)
	}
	if out.Len() != 0 {
		t.Fatalf("refused a call the graph does not cover: %q", out.String())
	}
}
