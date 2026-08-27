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

// hookTimeoutSeconds bounds the gate from the agent's side as well as its own.
//
// It is a belt on top of the command's braces: the command already gives up on
// a daemon that will not answer, and this is what stops a process that never
// exits at all from holding a tool call open.
const hookTimeoutSeconds = 5

// hookKind is how a target hosts the gate.
type hookKind uint8

const (
	// hookEntry is an entry in a file of hooks the agent already reads.
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
// Only three of the five clients can host one, and the two that cannot are
// refused by name rather than left to fail later. Claude Desktop has no
// pre-tool contract at all, and Oh My Pi's own documentation calls its hook
// subsystem legacy and says the runtime uses an extension runner instead --
// writing a file against that would be writing against a moving target.
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
		// Task is stock Claude Code's research dispatch and Agent is the
		// same tool in newer harnesses; naming one would leave the other
		// ungated in half the installations.
		document.matcher = "Bash|Glob|Grep|Task|Agent"
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
			strings.Join([]string{TargetClaudeCode, TargetCodex, TargetOpenCode}, ", "))
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
	if document.kind == hookPlugin {
		return manager.installPlugin(document, dryRun, force)
	}
	return manager.installHookEntry(document, dryRun)
}

// RemoveHook removes only the entry Kivgraph owns.
func (manager Manager) RemoveHook(target Target, scope Scope, dryRun, force bool) (Plan, error) {
	document, err := manager.hookDocumentFor(target, scope)
	if err != nil {
		return Plan{}, err
	}
	if document.kind == hookPlugin {
		return manager.removePlugin(document, dryRun, force)
	}
	return manager.removeHookEntry(document, dryRun)
}

// StatusHook inspects one client's gate.
func (manager Manager) StatusHook(target Target, scope Scope) (Plan, error) {
	document, err := manager.hookDocumentFor(target, scope)
	if err != nil {
		return Plan{}, err
	}
	if document.kind == hookPlugin {
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
	for _, target := range []Target{TargetClaudeCode, TargetCodex, TargetOpenCode} {
		document, err := manager.hookDocumentFor(target, scope)
		if err != nil {
			continue
		}
		// The gate's own file may not exist yet, so what is looked for is
		// the directory the client keeps its configuration in: that is
		// what says the client is installed rather than that we already
		// wrote to it.
		exists, err := pathExists(filepath.Dir(document.path))
		if err != nil {
			return nil, err
		}
		detections = append(detections, TargetDetection{Target: target, Detected: exists})
	}
	return detections, nil
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
	return []Target{TargetClaudeCode, TargetCodex, TargetOpenCode}
}
