package integrations

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// foreignSettings is a settings.json shaped the way a real one is.
//
// It was reduced from a working ~/.claude/settings.json that another tool had
// already installed hooks into, which is the case that matters: the array is
// where every gate on the machine lives, and a writer that treated it as its
// own would silently disarm whatever was there first.
const foreignSettings = `{
  "permissions": {"allow": ["mcp__other__*"]},
  "hooks": {
    "PreToolUse": [
      {"matcher": "Agent|Grep|Bash|Glob",
       "hooks": [{"type": "command", "command": "/home/u/.cargo/bin/tokensave hook-pre-tool-use"}]}
    ],
    "Stop": [
      {"hooks": [{"type": "command", "command": "/home/u/.cargo/bin/tokensave hook-stop"}]}
    ]
  },
  "theme": "dark"
}
`

// readSettings decodes a written hook file for inspection.
func readSettings(t *testing.T, path string) map[string]any {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	var document map[string]any
	if err := json.Unmarshal(data, &document); err != nil {
		t.Fatalf("parse %s: %v", path, err)
	}
	return document
}

// preToolEntries is the PreToolUse array of a decoded document.
func preToolEntries(t *testing.T, document map[string]any) []any {
	t.Helper()
	hooks, ok := document["hooks"].(map[string]any)
	if !ok {
		return nil
	}
	entries, _ := hooks[preToolUse].([]any)
	return entries
}

// commandsIn lists every command a document registers before a tool runs.
func commandsIn(t *testing.T, document map[string]any) []string {
	t.Helper()
	commands := []string{}
	for _, raw := range preToolEntries(t, document) {
		entry, _ := raw.(map[string]any)
		handlers, _ := entry["hooks"].([]any)
		for _, rawHandler := range handlers {
			handler, _ := rawHandler.(map[string]any)
			if command, ok := handler["command"].(string); ok {
				commands = append(commands, command)
			}
		}
	}
	return commands
}

// TestInstallLeavesAnotherToolsGateAlone is the negative this whole writer
// exists for. Unlike mcpServers, where our key is ours, a PreToolUse array
// holds every gate the user installed -- and a writer that replaced the array
// instead of appending to it would disarm another tool without saying so.
func TestInstallLeavesAnotherToolsGateAlone(t *testing.T) {
	manager, home, _ := testManager(t)
	path := filepath.Join(home, ".claude", "settings.json")
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(foreignSettings), 0o600); err != nil {
		t.Fatal(err)
	}

	if _, err := manager.InstallHook(TargetClaudeCode, ScopeUser, false, false); err != nil {
		t.Fatalf("InstallHook() error = %v", err)
	}
	document := readSettings(t, path)
	commands := commandsIn(t, document)
	want := []string{
		"/home/u/.cargo/bin/tokensave hook-pre-tool-use",
		"/opt/kivgraph/bin/kivgraph hook run",
	}
	if strings.Join(commands, "|") != strings.Join(want, "|") {
		t.Fatalf("PreToolUse commands = %q, want %q", commands, want)
	}
	// Everything the file held that is none of our business survives.
	if document["theme"] != "dark" || document["permissions"] == nil {
		t.Fatalf("install rewrote unrelated settings: %#v", document)
	}
	if hooks, _ := document["hooks"].(map[string]any); hooks["Stop"] == nil {
		t.Fatal("install dropped the Stop hooks")
	}

	// And a remove gives the file back exactly as it was found.
	if _, err := manager.RemoveHook(TargetClaudeCode, ScopeUser, false, false); err != nil {
		t.Fatalf("RemoveHook() error = %v", err)
	}
	after := readSettings(t, path)
	if commands := commandsIn(t, after); len(commands) != 1 || commands[0] != want[0] {
		t.Fatalf("after remove, PreToolUse commands = %q, want only the foreign one", commands)
	}
}

// TestInstallIsIdempotentAndReplacesAMovedBinary keeps a second copy of the
// gate from accumulating. An entry that names a kivgraph which has since moved
// is still ours, so install has to replace it rather than append beside it.
func TestInstallIsIdempotentAndReplacesAMovedBinary(t *testing.T) {
	manager, home, _ := testManager(t)
	path := filepath.Join(home, ".claude", "settings.json")

	first, err := manager.InstallHook(TargetClaudeCode, ScopeUser, false, false)
	if err != nil {
		t.Fatalf("InstallHook() error = %v", err)
	}
	if first.Status != "installed" || !first.Changed {
		t.Fatalf("first install = %#v", first)
	}
	second, err := manager.InstallHook(TargetClaudeCode, ScopeUser, false, false)
	if err != nil {
		t.Fatalf("InstallHook() error = %v", err)
	}
	if second.Status != "managed" || second.Changed {
		t.Fatalf("second install = %#v, want a no-op", second)
	}

	// Move the binary and reinstall: one entry, the new path.
	moved, err := New(Options{HomeDir: home, ProjectDir: t.TempDir(),
		Executable: "/usr/local/bin/kivgraph", GOOS: "linux"})
	if err != nil {
		t.Fatal(err)
	}
	status, err := moved.StatusHook(TargetClaudeCode, ScopeUser)
	if err != nil {
		t.Fatalf("StatusHook() error = %v", err)
	}
	if status.Status != statusSuperseded {
		t.Fatalf("status of a moved binary = %q, want %q", status.Status, statusSuperseded)
	}
	if _, err := moved.InstallHook(TargetClaudeCode, ScopeUser, false, false); err != nil {
		t.Fatalf("InstallHook() error = %v", err)
	}
	commands := commandsIn(t, readSettings(t, path))
	if len(commands) != 1 || commands[0] != "/usr/local/bin/kivgraph hook run" {
		t.Fatalf("after reinstall, commands = %q, want one naming the new path", commands)
	}
}

// TestRemoveLeavesNoEmptyScaffolding holds the shape of a file nobody installed
// into. An emptied array written back as `[]` is a trace of us, not an absence.
func TestRemoveLeavesNoEmptyScaffolding(t *testing.T) {
	manager, home, _ := testManager(t)
	path := filepath.Join(home, ".claude", "settings.json")
	if _, err := manager.InstallHook(TargetClaudeCode, ScopeUser, false, false); err != nil {
		t.Fatal(err)
	}
	if _, err := manager.RemoveHook(TargetClaudeCode, ScopeUser, false, false); err != nil {
		t.Fatal(err)
	}
	if document := readSettings(t, path); document["hooks"] != nil {
		t.Fatalf("remove left %#v behind", document["hooks"])
	}
}

// TestDryRunWritesNothing is the promise the flag makes.
func TestDryRunWritesNothing(t *testing.T) {
	manager, home, _ := testManager(t)
	for _, target := range []Target{TargetClaudeCode, TargetCodex, TargetOpenCode} {
		plan, err := manager.InstallHook(target, ScopeUser, true, false)
		if err != nil {
			t.Fatalf("InstallHook(%s) error = %v", target, err)
		}
		if plan.Status != "would-install" || !plan.DryRun {
			t.Fatalf("%s dry run = %#v", target, plan)
		}
		if _, err := os.Stat(plan.Path); !os.IsNotExist(err) {
			t.Fatalf("%s dry run wrote %s", target, plan.Path)
		}
	}
	entries, err := os.ReadDir(home)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Fatalf("dry run created %d entries under the home directory", len(entries))
	}
}

// TestEveryTargetWritesWhereItsOwnClientLooks is the fixture check: a path
// nobody reads is a gate that silently never fires. Claude Code's hooks live in
// settings.json and not in the .claude.json its MCP servers use; Codex reads
// hooks.json; OpenCode scans a plugins directory and loads what it finds.
func TestEveryTargetWritesWhereItsOwnClientLooks(t *testing.T) {
	manager, home, project := testManager(t)
	for _, testCase := range []struct {
		target Target
		scope  Scope
		want   string
	}{
		{TargetClaudeCode, ScopeUser, filepath.Join(home, ".claude", "settings.json")},
		{TargetClaudeCode, ScopeProject, filepath.Join(project, ".claude", "settings.json")},
		{TargetClaudeDesktop, ScopeUser, filepath.Join(home, ".claude", "settings.json")},
		{TargetCodex, ScopeUser, filepath.Join(home, ".codex", "hooks.json")},
		{TargetCodex, ScopeProject, filepath.Join(project, ".codex", "hooks.json")},
		{TargetOpenCode, ScopeUser, filepath.Join(home, ".config", "opencode", "plugins", "kivgraph.js")},
		{TargetOpenCode, ScopeProject, filepath.Join(project, ".opencode", "plugins", "kivgraph.js")},
	} {
		plan, err := manager.StatusHook(testCase.target, testCase.scope)
		if err != nil {
			t.Fatalf("StatusHook(%s, %s) error = %v", testCase.target, testCase.scope, err)
		}
		if plan.Path != testCase.want {
			t.Fatalf("%s/%s writes to %s, want %s", testCase.target, testCase.scope, plan.Path, testCase.want)
		}
	}
}

// TestCodexKeepsItsOwnWrapper defends the one structural difference between the
// two agents that share this writer. Codex nests the events under a top-level
// "hooks" key, and so does Claude Code -- the shapes agree, and this is the
// fixture that says so rather than an assumption.
func TestCodexKeepsItsOwnWrapper(t *testing.T) {
	manager, home, _ := testManager(t)
	if _, err := manager.InstallHook(TargetCodex, ScopeUser, false, false); err != nil {
		t.Fatalf("InstallHook() error = %v", err)
	}
	document := readSettings(t, filepath.Join(home, ".codex", "hooks.json"))
	entries := preToolEntries(t, document)
	if len(entries) != 1 {
		t.Fatalf("PreToolUse = %#v, want one entry", entries)
	}
	entry, _ := entries[0].(map[string]any)
	// Codex names the shell Bash and has no glob tool at all, so gating
	// Claude Code's four names here would register matchers Codex never
	// fires.
	if entry["matcher"] != "Bash" {
		t.Fatalf("codex matcher = %v, want Bash", entry["matcher"])
	}
}

// TestTheOpenCodePluginNamesThisInstallationsBinary keeps the shim from
// resolving kivgraph on PATH, which an editor launched from a desktop entry
// does not have.
func TestTheOpenCodePluginNamesThisInstallationsBinary(t *testing.T) {
	manager, home, _ := testManager(t)
	if _, err := manager.InstallHook(TargetOpenCode, ScopeUser, false, false); err != nil {
		t.Fatalf("InstallHook() error = %v", err)
	}
	body, err := os.ReadFile(filepath.Join(home, ".config", "opencode", "plugins", "kivgraph.js"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(body), executablePlaceholder) {
		t.Fatal("the plugin still carries its placeholder")
	}
	if !strings.Contains(string(body), `"/opt/kivgraph/bin/kivgraph"`) {
		t.Fatal("the plugin does not name this installation's binary")
	}
	if !strings.Contains(string(body), "tool.execute.before") {
		t.Fatal("the plugin registers no pre-tool hook")
	}
}

// TestATargetWithNoGateIsRefusedByName keeps a client that cannot host the gate
// from being told it can. Oh My Pi's own documentation calls its hook subsystem
// legacy and says the runtime uses an extension runner instead.
func TestATargetWithNoGateIsRefusedByName(t *testing.T) {
	manager, _, _ := testManager(t)
	for _, target := range []Target{TargetOhMyPi, Target("cursor")} {
		_, err := manager.InstallHook(target, ScopeUser, false, false)
		if err == nil {
			t.Fatalf("%s was offered a gate it cannot host", target)
		}
		if !strings.Contains(err.Error(), string(target)) {
			t.Fatalf("%s: error does not name the target: %v", target, err)
		}
	}
}

// TestABinaryNotCalledKivgraphStillOwnsItsGate is a regression, and the way it
// was found is the point: every unit test above happened to use a path ending
// in `kivgraph`, so recognising an entry by that base name passed all of them.
// A sandbox install of a build called `kivgraph-hook` registered a gate that
// status then reported absent and remove refused to touch -- an install that
// could be repeated forever, stacking a copy each time.
func TestABinaryNotCalledKivgraphStillOwnsItsGate(t *testing.T) {
	home := t.TempDir()
	for _, executable := range []string{
		"/tmp/kivgraph-hook",         // a development build
		"/opt/kivgraph/bin/kivgraph", // the ordinary install
		"/home/u/.local/bin/kivgraph-0.8.0",
	} {
		t.Run(filepath.Base(executable), func(t *testing.T) {
			root := filepath.Join(home, filepath.Base(executable))
			manager, err := New(Options{HomeDir: root, ProjectDir: t.TempDir(),
				Executable: executable, GOOS: "linux"})
			if err != nil {
				t.Fatal(err)
			}
			if _, err := manager.InstallHook(TargetClaudeCode, ScopeUser, false, false); err != nil {
				t.Fatalf("InstallHook() error = %v", err)
			}
			status, err := manager.StatusHook(TargetClaudeCode, ScopeUser)
			if err != nil {
				t.Fatalf("StatusHook() error = %v", err)
			}
			if status.Status != "managed" {
				t.Fatalf("status = %q, want managed: the gate it just wrote is invisible to it",
					status.Status)
			}
			removal, err := manager.RemoveHook(TargetClaudeCode, ScopeUser, false, false)
			if err != nil {
				t.Fatalf("RemoveHook() error = %v", err)
			}
			if removal.Status != "removed" {
				t.Fatalf("remove = %q, want removed", removal.Status)
			}
		})
	}
}

// TestAForeignGateIsNeverClaimed is the other side of the same rule: a command
// that is not ours stays not ours, however it is spelled.
func TestAForeignGateIsNeverClaimed(t *testing.T) {
	manager, _, _ := testManager(t)
	for _, command := range []string{
		"/home/u/.cargo/bin/tokensave hook-pre-tool-use",
		"/usr/local/bin/someone-else guard",
		"/opt/other/bin/other hook run",
		"",
	} {
		entry := hookEntryValue{Matcher: "Bash", Hooks: []hookHandlerValue{
			{Type: "command", Command: command}}}
		if manager.ownsHookEntry(entry) {
			t.Fatalf("claimed a foreign gate: %q", command)
		}
	}
}

// TestClaudeDesktopIsGatedThroughTheFileItActuallyReads records the fact that
// found this target, because it is not guessable from the app's own directory.
//
// Claude Desktop bundles a Claude Code and runs it for agent work without
// giving it a configuration directory of its own. The proof is a transcript: a
// session the desktop app records under its own `claude-code-sessions` as
// `cliSessionId` 6c7bf9db-2774-45bf-8371-8764497bb74a was written to
// ~/.claude/projects/-home-devlabs-claude/6c7bf9db-....jsonl, which is
// $CLAUDE_CONFIG_DIR/projects with the variable unset. So the settings it reads
// are the user's, and the gate reaches it through the same file claude-code
// installs into.
//
// The two targets therefore share a document, and that is the behaviour to
// hold: installing one and asking about the other has to say it is there,
// because it is.
func TestClaudeDesktopIsGatedThroughTheFileItActuallyReads(t *testing.T) {
	manager, home, _ := testManager(t)
	if _, err := manager.InstallHook(TargetClaudeCode, ScopeUser, false, false); err != nil {
		t.Fatalf("InstallHook(claude-code) error = %v", err)
	}
	status, err := manager.StatusHook(TargetClaudeDesktop, ScopeUser)
	if err != nil {
		t.Fatalf("StatusHook(claude-desktop) error = %v", err)
	}
	if status.Status != "managed" {
		t.Fatalf("claude-desktop status = %q after installing claude-code, want managed", status.Status)
	}
	if status.Path != filepath.Join(home, ".claude", "settings.json") {
		t.Fatalf("claude-desktop reads %s", status.Path)
	}
	// And installing it again is a no-op rather than a second entry.
	plan, err := manager.InstallHook(TargetClaudeDesktop, ScopeUser, false, false)
	if err != nil {
		t.Fatalf("InstallHook(claude-desktop) error = %v", err)
	}
	if plan.Changed {
		t.Fatalf("installing claude-desktop over claude-code changed the file: %#v", plan)
	}
}

// TestClaudeDesktopHasNoProjectScope holds the one way the two targets differ:
// the desktop app reads the user's settings and has no repository to put a
// project file in.
func TestClaudeDesktopHasNoProjectScope(t *testing.T) {
	manager, _, _ := testManager(t)
	if _, err := manager.InstallHook(TargetClaudeDesktop, ScopeProject, false, false); err == nil {
		t.Fatal("claude-desktop accepted a project scope it cannot read")
	}
}

// TestClaudeDesktopIsDetectedByItsOwnEntry keeps the desktop app from being
// reported as installed on every machine that has ever run the CLI -- they
// share ~/.claude, so the configuration directory says nothing about it. The
// Linux filename is the one the claude-desktop package actually ships.
func TestClaudeDesktopIsDetectedByItsOwnEntry(t *testing.T) {
	manager, home, _ := testManager(t)
	if err := os.MkdirAll(filepath.Join(home, ".claude"), 0o700); err != nil {
		t.Fatal(err)
	}
	detected := func() bool {
		detections, err := manager.DetectHookTargets(ScopeUser)
		if err != nil {
			t.Fatalf("DetectHookTargets() error = %v", err)
		}
		for _, detection := range detections {
			if detection.Target == TargetClaudeDesktop {
				return detection.Detected
			}
		}
		t.Fatal("claude-desktop is not offered as a hook target")
		return false
	}
	if detected() {
		t.Fatal("a bare ~/.claude reported the desktop app as installed")
	}
	entry := filepath.Join(home, "Applications", "Claude.app")
	if err := os.MkdirAll(entry, 0o700); err != nil {
		t.Fatal(err)
	}
	if !detected() {
		t.Fatal("the desktop app is installed and was not detected")
	}
}
