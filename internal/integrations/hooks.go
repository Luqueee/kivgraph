package integrations

import (
	_ "embed"
	"fmt"
	"path/filepath"
	"strings"
)

// embeddedOpenCodePlugin is the shim OpenCode loads, before its executable path
// is filled in.
//
//go:embed assets/hooks/opencode.js
var embeddedOpenCodePlugin []byte

// executablePlaceholder is what the template carries where the path goes.
const executablePlaceholder = "__KIVGRAPH_EXECUTABLE__"

// preToolUse is the event all three agents fire before running a tool.
const preToolUse = "PreToolUse"

// hookOperation is the command an agent runs to reach the gate.
const hookOperation = "hook run"

// claudeMatcher is the tool list both Claude clients gate on.
//
// Task is stock Claude Code's research dispatch and Agent is the same tool in
// newer harnesses; naming one would leave the other ungated in half the
// installations.
const claudeMatcher = "Bash|Glob|Grep|Task|Agent"

// hookTimeoutSeconds bounds the gate from the agent's side as well as its own.
//
// It is a belt on top of the command's braces: the command already gives up on
// a daemon that will not answer, and this is what stops a process that never
// exits at all from holding a tool call open.
const hookTimeoutSeconds = 5

// hookKind is how a target hosts the gate.
type hookKind uint8

const (
	// hookEntry is an entry in a file of hooks the agent already reads. It
	// is the zero value because it is what three of the four targets are.
	hookEntry hookKind = iota
	// hookPlugin is a file the agent loads as code, because it has no
	// shell-hook contract at all.
	hookPlugin
)

// hookDocument is where and how one target hosts the gate.
type hookDocument struct {
	target  Target
	scope   Scope
	path    string
	kind    hookKind
	matcher string
}

// hookDocumentFor resolves a target's gate.
//
// Four of the five clients can host one. Oh My Pi is the exception: its own
// documentation calls its hook subsystem legacy and says the runtime uses an
// extension runner instead, and writing a file against that would be writing
// against a moving target.
func (manager Manager) hookDocumentFor(target Target, scope Scope) (hookDocument, error) {
	if err := validateScope(scope); err != nil {
		return hookDocument{}, err
	}
	document := hookDocument{target: target, scope: scope}
	switch target {
	case TargetClaudeCode:
		// Claude Code keeps hooks in settings.json, which is not the
		// .claude.json its MCP servers live in. Two files, one client.
		document.path = filepath.Join(manager.scopeRoot(scope), ".claude", "settings.json")
		document.matcher = claudeMatcher
	case TargetClaudeDesktop:
		// Claude Desktop bundles a Claude Code and runs it for its agent
		// work, and it does not give that process a configuration
		// directory of its own: a session started from the desktop app
		// writes its transcript to `~/.claude/projects`, which is
		// `$CLAUDE_CONFIG_DIR/projects` with the variable unset. So the
		// gate it reads is the one in the file below -- the same file
		// claude-code installs into, named separately because a reader
		// looking for the desktop app should not have to know that.
		if scope != ScopeUser {
			return hookDocument{}, fmt.Errorf(
				"target %q has no project scope: it reads only the user's settings", target)
		}
		document.path = filepath.Join(manager.homeDir, ".claude", "settings.json")
		document.matcher = claudeMatcher
	case TargetCodex:
		document.path = filepath.Join(manager.scopeRoot(scope), ".codex", "hooks.json")
		// Codex gates the shell, apply_patch and MCP calls. Only the
		// first is a search.
		document.matcher = "Bash"
	case TargetOpenCode:
		document.kind = hookPlugin
		if scope == ScopeUser {
			document.path = filepath.Join(manager.homeDir, ".config", "opencode", "plugins", "kivgraph.js")
			break
		}
		document.path = filepath.Join(manager.projectDir, ".opencode", "plugins", "kivgraph.js")
	default:
		return hookDocument{}, fmt.Errorf(
			"target %q hosts no pre-tool-use gate (want %s)", target,
			strings.Join(targetNames(HookTargets()), ", "))
	}
	return document, nil
}

// scopeRoot is the directory a scope's dot-directories hang from.
func (manager Manager) scopeRoot(scope Scope) string {
	if scope == ScopeProject {
		return manager.projectDir
	}
	return manager.homeDir
}

// hookCommand is the command line an agent runs.
func (manager Manager) hookCommand() string {
	executable := manager.executable
	if strings.ContainsAny(executable, " \t") {
		executable = `"` + executable + `"`
	}
	return executable + " " + hookOperation
}

// InstallHook registers the gate with one client.
func (manager Manager) InstallHook(target Target, scope Scope, dryRun, force bool) (Plan, error) {
	document, err := manager.hookDocumentFor(target, scope)
	if err != nil {
		return Plan{}, err
	}
	if document.kind == hookEntry {
		return manager.installHookEntry(document, dryRun)
	}
	return manager.installPlugin(document, dryRun, force)
}

// RemoveHook removes only the entry Kivgraph owns.
func (manager Manager) RemoveHook(target Target, scope Scope, dryRun, force bool) (Plan, error) {
	document, err := manager.hookDocumentFor(target, scope)
	if err != nil {
		return Plan{}, err
	}
	if document.kind == hookEntry {
		return manager.removeHookEntry(document, dryRun)
	}
	return manager.removePlugin(document, dryRun, force)
}

// StatusHook inspects one client's gate.
func (manager Manager) StatusHook(target Target, scope Scope) (Plan, error) {
	document, err := manager.hookDocumentFor(target, scope)
	if err != nil {
		return Plan{}, err
	}
	if document.kind != hookEntry {
		return manager.statusPlugin(document)
	}
	state, err := manager.readHooks(document)
	if err != nil {
		return Plan{}, err
	}
	return manager.plan(ActionStatus, document, state.status, hookStatusDetail(state.status)), nil
}

// DetectHookTargets reports which clients can host a gate and appear installed.
func (manager Manager) DetectHookTargets(scope Scope) ([]TargetDetection, error) {
	detections := []TargetDetection{}
	for _, target := range HookTargets() {
		document, err := manager.hookDocumentFor(target, scope)
		if err != nil {
			continue
		}
		detected, err := anyPathExists(manager.hookMarkers(target, document))
		if err != nil {
			return nil, err
		}
		detections = append(detections, TargetDetection{Target: target, Detected: detected})
	}
	return detections, nil
}

// hookMarkers are the paths that say a client is installed.
//
// For three of the four it is the directory the client keeps its configuration
// in, because the gate's own file may not exist yet and its absence says
// nothing about the client. Claude Desktop needs its own answer: it writes into
// `~/.claude`, which is Claude Code's directory, so keying off that would
// report the desktop app as installed on every machine that has ever run the
// CLI.
func (manager Manager) hookMarkers(target Target, document hookDocument) []string {
	if target != TargetClaudeDesktop {
		return []string{filepath.Dir(document.path)}
	}
	return manager.claudeDesktopMarkers()
}

// claudeDesktopMarkers are the paths that say the desktop app is installed.
//
// The Linux entry is `com.anthropic.Claude.desktop`, which is what the
// `claude-desktop` package actually ships -- `dpkg -L claude-desktop` names
// exactly one `.desktop` file and that is it. The unqualified `claude.desktop`
// beside it is kept for a packaging that uses it, and because dropping a marker
// can only ever cost a detection.
func (manager Manager) claudeDesktopMarkers() []string {
	if manager.goos == "darwin" {
		return []string{
			filepath.Join(manager.homeDir, "Applications", "Claude.app"),
			"/Applications/Claude.app",
		}
	}
	if manager.goos == "windows" {
		// Three, because Windows has had two installers and they agree on
		// nothing. Measured on a host running Claude Desktop 1.37937.3.0 from
		// the Store: the first two are **both absent**, and only the package
		// directory is there.
		//
		//   - the MSIX package's own data directory, which is where a Store
		//     install keeps everything including the configuration this
		//     manager writes;
		//   - `%APPDATA%\Claude`, which the older Win32 build created and
		//     which every piece of documentation still names;
		//   - `%LOCALAPPDATA%\AnthropicClaude`, which the Win32 installer
		//     created and filled with `app-X.Y.Z` directories.
		//
		// All three are kept because a marker that is wrong costs a detection
		// and a marker that is missing costs the same, so the cheaper mistake
		// is to look in every place a real install has ever put itself.
		markers := []string{
			filepath.Join(manager.roamingDir(), "Claude"),
			filepath.Join(manager.localDir(), "AnthropicClaude"),
		}
		if packaged, found := manager.claudeDesktopPackage(); found {
			markers = append([]string{packaged}, markers...)
		}
		return markers
	}
	return []string{
		filepath.Join(manager.homeDir, ".local", "share", "applications", "com.anthropic.Claude.desktop"),
		"/usr/share/applications/com.anthropic.Claude.desktop",
		filepath.Join(manager.homeDir, ".local", "share", "applications", "claude.desktop"),
		"/usr/share/applications/claude.desktop",
	}
}

// anyPathExists reports whether at least one marker is present.
func anyPathExists(paths []string) (bool, error) {
	for _, path := range paths {
		exists, err := pathExists(path)
		if err != nil {
			return false, err
		}
		if exists {
			return true, nil
		}
	}
	return false, nil
}

// plan is the common shape of every answer this file gives.
func (manager Manager) plan(action Action, document hookDocument, status, detail string) Plan {
	return Plan{Action: action, Target: document.target, Scope: document.scope,
		Path: document.path, Status: status, Detail: detail}
}

// HookTargets are the clients that can host a pre-tool-use gate.
//
// It is a shorter list than KnownTargets and has to be said separately: help
// text that named all five would send a reader to a --target that answers with
// an error.
func HookTargets() []Target {
	return []Target{TargetClaudeCode, TargetClaudeDesktop, TargetCodex, TargetOpenCode}
}

// targetNames spells a target list for an error message.
func targetNames(targets []Target) []string {
	names := make([]string, 0, len(targets))
	for _, target := range targets {
		names = append(names, string(target))
	}
	return names
}
