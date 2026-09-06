package integrations

import (
	"encoding/json"
	"github.com/Luqueee/kivgraph/internal/executable"
	"github.com/Luqueee/kivgraph/internal/testsupport"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
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
		testsupport.InstalledExecutable() + " hook run",
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
	movedExecutable := testsupport.MovedExecutable()
	moved, err := New(Options{HomeDir: home, ProjectDir: t.TempDir(),
		Executable: movedExecutable, GOOS: "linux"})
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
	// Not compared against a spelled-out string: the manager quotes an
	// executable whose path contains a space, and on Windows the plausible
	// install location is under "Program Files". Asserting the unquoted
	// spelling would be asserting that the quoting is absent.
	if len(commands) != 1 {
		t.Fatalf("after reinstall, commands = %q, want exactly one", commands)
	}
	if !strings.Contains(commands[0], movedExecutable) || !strings.HasSuffix(commands[0], hookOperation) {
		t.Fatalf("after reinstall, commands = %q, want one naming the new path", commands)
	}
}

// A path with a space in it is the ordinary case on Windows, where an
// installation lands under "Program Files", and it is the case the entry has
// to survive twice: quoted on the way in so the agent runs one program rather
// than two arguments, and recognised as ours on the way back out so that a
// reinstall replaces the entry instead of appending beside it.
//
// Neither half was covered. The quoting is a single `strings.ContainsAny` in
// hookCommand and the unquoting a single `strings.Trim` in hooks_entry.go, and
// a round trip that nobody exercises is a round trip that holds until the
// platform where it is the default arrives.
func TestHookEntrySurvivesAnExecutablePathWithASpace(t *testing.T) {
	home := t.TempDir()
	spaced := filepath.Join(home, "Program Files", "kivgraph", "bin",
		executable.Name("kivgraph"))
	manager, err := New(Options{HomeDir: home, ProjectDir: t.TempDir(),
		Executable: spaced, GOOS: runtime.GOOS})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	path := filepath.Join(home, ".claude", "settings.json")

	if _, err := manager.InstallHook(TargetClaudeCode, ScopeUser, false, false); err != nil {
		t.Fatalf("InstallHook() error = %v", err)
	}
	commands := commandsIn(t, readSettings(t, path))
	if len(commands) != 1 {
		t.Fatalf("commands = %q, want exactly one", commands)
	}
	if !strings.HasPrefix(commands[0], `"`+spaced+`"`) {
		t.Fatalf("command = %q, want the executable quoted: unquoted, the agent "+
			"runs the first word and passes the rest as arguments", commands[0])
	}

	// The second install must read its own entry back and recognise it. If the
	// unquoting is wrong the entry looks like somebody else's and a second one
	// is appended, which is the accumulation this gate exists to prevent.
	second, err := manager.InstallHook(TargetClaudeCode, ScopeUser, false, false)
	if err != nil {
		t.Fatalf("second InstallHook() error = %v", err)
	}
	if second.Status != "managed" || second.Changed {
		t.Fatalf("second install = %#v, want a no-op: the quoted entry was not recognised as ours", second)
	}
	if commands := commandsIn(t, readSettings(t, path)); len(commands) != 1 {
		t.Fatalf("after reinstall, commands = %q, want the entry replaced rather than appended", commands)
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
	for _, target := range []Target{TargetClaudeCode, TargetCodex, TargetOpenCode, TargetOhMyPi} {
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
// hooks.json; OpenCode and Oh My Pi auto-discover extension modules in their
// respective extension directories.
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
		{TargetOhMyPi, ScopeUser, filepath.Join(home, ".omp", "agent", "extensions", "kivgraph.js")},
		{TargetOhMyPi, ScopeProject, filepath.Join(project, ".omp", "extensions", "kivgraph.js")},
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
	// The desktop labels command executions "Shell" in its transcript, but
	// the PreToolUse payload names the tool Bash. Codex matchers are literal,
	// so matching the presentation label leaves the hook disconnected.
	if entry["matcher"] != "Bash" {
		t.Fatalf("InstallHook(%q, %q) matcher = %v, want Bash",
			TargetCodex, ScopeUser, entry["matcher"])
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
	if !strings.Contains(string(body), escapedPath(t, testsupport.InstalledExecutable())) {
		t.Fatal("the plugin does not name this installation's binary")
	}
	if !strings.Contains(string(body), "tool.execute.before") {
		t.Fatal("the plugin registers no pre-tool hook")
	}
}

// TestOhMyPiIncompatibleExtensionRequiresForce is the negative for the new
// target. Installing over a module that Kivgraph did not write must not replace
// another extension unless the caller explicitly opts in.
func TestOhMyPiIncompatibleExtensionRequiresForce(t *testing.T) {
	manager, home, _ := testManager(t)
	path := filepath.Join(home, ".omp", "agent", "extensions", "kivgraph.js")
	original := []byte("export default function otherExtension() {}\n")
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, original, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := manager.InstallHook(TargetOhMyPi, ScopeUser, false, false); err == nil {
		t.Fatal("incompatible Oh My Pi extension was replaced without --force")
	}
	if data, err := os.ReadFile(path); err != nil || string(data) != string(original) {
		t.Fatalf("failed install changed the extension: data=%q err=%v", data, err)
	}
	plan, err := manager.InstallHook(TargetOhMyPi, ScopeUser, false, true)
	if err != nil {
		t.Fatalf("forced InstallHook() error = %v", err)
	}
	if plan.Status != "installed" || !plan.Changed {
		t.Fatalf("forced install plan = %#v", plan)
	}
	if _, err := os.Stat(path + ".kivgraph.bak"); err != nil {
		t.Fatalf("forced install did not preserve the incompatible extension: %v", err)
	}
}

// TestTheOhMyPiExtensionRunsTheGateAndBlocksItsDenial invokes the generated
// extension through Node, the same module boundary Oh My Pi uses. A source
// literal check would pass even if the handler sent the wrong payload or
// ignored a denial.
func TestTheOhMyPiExtensionRunsTheGateAndBlocksItsDenial(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("the shell executable fixture is Unix-specific")
	}
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skipf("node is not installed: %v", err)
	}
	if output, err := exec.Command(node, "--input-type=module", "--eval", "").CombinedOutput(); err != nil {
		t.Skipf("node cannot run the ESM harness: %v\n%s", err, output)
	}
	home := t.TempDir()
	project := t.TempDir()
	capture := filepath.Join(t.TempDir(), "payload.json")
	count := filepath.Join(t.TempDir(), "invocations")
	executable := filepath.Join(t.TempDir(), "kivgraph")
	script := "#!/bin/sh\nif [ -e \"$KIVGRAPH_TEST_COUNT\" ]; then\ncat >/dev/null\nprintf '%s\\n' 'malformed gate response'\nelse\ntouch \"$KIVGRAPH_TEST_COUNT\"\ncat > \"$KIVGRAPH_TEST_CAPTURE\"\nprintf '%s\\n' '{\"hookSpecificOutput\":{\"permissionDecision\":\"deny\",\"permissionDecisionReason\":\"use find_symbol\"}}'\nfi\n"
	if err := os.WriteFile(executable, []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	manager, err := New(Options{HomeDir: home, ProjectDir: project,
		Executable: executable, GOOS: runtime.GOOS})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := manager.InstallHook(TargetOhMyPi, ScopeUser, false, false); err != nil {
		t.Fatalf("InstallHook() error = %v", err)
	}
	path := filepath.Join(home, ".omp", "agent", "extensions", "kivgraph.js")
	if err := os.WriteFile(filepath.Join(home, ".omp", "agent", "package.json"),
		[]byte(`{"type":"module"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	harness := `import { pathToFileURL } from "node:url"
const extension = await import(pathToFileURL(process.env.OMP_EXTENSION).href)
const handlers = []
extension.default({ on(name, handler) {
  if (name !== "tool_call") throw new Error("unexpected event: " + name)
  handlers.push(handler)
} })
if (handlers.length !== 1) throw new Error("extension registered the wrong handlers")
const result = await handlers[0]({
  toolName: "Grep",
  input: { pattern: "NewServer", path: "internal" },
}, { cwd: "/repo" })
if (result?.block !== true || result.reason !== "use find_symbol") {
  throw new Error("unexpected hook result: " + JSON.stringify(result))
}
const failOpen = await handlers[0]({
  toolName: "Grep",
  input: { pattern: "Indexer", path: "internal" },
}, { cwd: "/repo" })
if (failOpen !== undefined) {
  throw new Error("malformed gate response blocked the call: " + JSON.stringify(failOpen))
}
`
	command := exec.Command(node, "--input-type=module", "--eval", harness)
	command.Env = append(os.Environ(), "KIVGRAPH_TEST_CAPTURE="+capture,
		"KIVGRAPH_TEST_COUNT="+count, "OMP_EXTENSION="+path)
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("Oh My Pi handler failed for target=%s scope=%s path=%s: %v\n%s",
			TargetOhMyPi, ScopeUser, path, err, output)
	}
	body, err := os.ReadFile(capture)
	if err != nil {
		t.Fatalf("Oh My Pi handler did not invoke target=%s scope=%s path=%s: %v",
			TargetOhMyPi, ScopeUser, path, err)
	}
	var payload map[string]any
	if err := json.Unmarshal(body, &payload); err != nil {
		t.Fatalf("Oh My Pi handler sent invalid payload for target=%s scope=%s path=%s: %v",
			TargetOhMyPi, ScopeUser, path, err)
	}
	if payload["hook_event_name"] != "PreToolUse" || payload["cwd"] != "/repo" || payload["tool_name"] != "Grep" {
		t.Fatalf("Oh My Pi handler sent payload=%#v for target=%s scope=%s path=%s",
			payload, TargetOhMyPi, ScopeUser, path)
	}
	input, ok := payload["tool_input"].(map[string]any)
	if !ok || input["pattern"] != "NewServer" || input["path"] != "internal" {
		t.Fatalf("Oh My Pi handler sent tool_input=%#v for target=%s scope=%s path=%s",
			payload["tool_input"], TargetOhMyPi, ScopeUser, path)
	}
}

// TestOhMyPiExtensionInstallIsIdempotentAndRemovable keeps the native module
// on the same lifecycle as the other generated hook integrations.
func TestOhMyPiExtensionInstallIsIdempotentAndRemovable(t *testing.T) {
	manager, home, _ := testManager(t)
	first, err := manager.InstallHook(TargetOhMyPi, ScopeUser, false, false)
	if err != nil {
		t.Fatalf("first InstallHook() error = %v", err)
	}
	if first.Status != "installed" || !first.Changed {
		t.Fatalf("first install = %#v", first)
	}
	second, err := manager.InstallHook(TargetOhMyPi, ScopeUser, false, false)
	if err != nil {
		t.Fatalf("second InstallHook() error = %v", err)
	}
	if second.Status != "managed" || second.Changed {
		t.Fatalf("second install = %#v, want a no-op", second)
	}
	removed, err := manager.RemoveHook(TargetOhMyPi, ScopeUser, false, false)
	if err != nil {
		t.Fatalf("RemoveHook() error = %v", err)
	}
	if removed.Status != "removed" || !removed.Changed {
		t.Fatalf("remove = %#v", removed)
	}
	path := filepath.Join(home, ".omp", "agent", "extensions", "kivgraph.js")
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("extension still exists after removal: %v", err)
	}
	if _, err := os.Stat(path + ".kivgraph.bak"); err != nil {
		t.Fatalf("removal did not preserve a backup: %v", err)
	}
}

// TestATargetWithNoGateIsRefusedByName keeps a client that cannot host the gate
// from being told it can.
func TestATargetWithNoGateIsRefusedByName(t *testing.T) {
	manager, _, _ := testManager(t)
	for _, target := range []Target{Target("cursor")} {
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
		"/tmp/kivgraph-hook",              // a development build
		testsupport.InstalledExecutable(), // the ordinary install
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

func TestClaudeDesktopDetectionUsesConfiguredSystemRoot(t *testing.T) {
	manager, _, _ := testManager(t)
	entry := filepath.Join(manager.systemRoot, "Applications", "Claude.app")
	if err := os.MkdirAll(entry, 0o700); err != nil {
		t.Fatal(err)
	}
	detections, err := manager.DetectHookTargets(ScopeUser)
	if err != nil {
		t.Fatalf("DetectHookTargets() error = %v", err)
	}
	for _, detection := range detections {
		if detection.Target == TargetClaudeDesktop {
			if !detection.Detected {
				t.Fatalf("system application below configured root %q was not detected", manager.systemRoot)
			}
			return
		}
	}
	t.Fatalf("claude-desktop is not offered as a hook target with system root %q", manager.systemRoot)
}

// TestOhMyPiProjectIsDetectedByItsAgentRoot keeps project selection from
// treating the extension file as the installation marker. The project root is
// what Oh My Pi owns even before a Kivgraph extension is written.
func TestOhMyPiProjectIsDetectedByItsAgentRoot(t *testing.T) {
	manager, _, project := testManager(t)
	if err := os.MkdirAll(filepath.Join(project, ".omp"), 0o700); err != nil {
		t.Fatal(err)
	}
	detections, err := manager.DetectHookTargets(ScopeProject)
	if err != nil {
		t.Fatalf("DetectHookTargets() error = %v", err)
	}
	for _, detection := range detections {
		if detection.Target == TargetOhMyPi {
			if !detection.Detected {
				t.Fatal("a project .omp root was not detected")
			}
			return
		}
	}
	t.Fatal("oh-my-pi is not offered as a project hook target")
}
