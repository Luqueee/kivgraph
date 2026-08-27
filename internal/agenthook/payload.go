// Package agenthook decides whether a coding agent's search call should be
// answered by the graph instead of by reading text.
//
// The decision is a gate in front of a tool call, not a policy: it runs before
// `grep`, refuses the calls the graph answers better, and names the call that
// answers them. Three agents can host it -- Claude Code, Codex and OpenCode --
// and the first two hand it the same payload on stdin and read the same verdict
// on stdout. OpenCode cannot spawn a process itself, so a generated plugin
// speaks for it; the payload it forwards is this one, which is why the dialect
// is a field rather than three parsers.
//
// Every failure here is an allow. A gate that cannot read its input, cannot
// find the graph or cannot reach the daemon has learned nothing about the call,
// and a hook that blocks on its own bugs would make the agent unusable in a
// repository that never asked for it.
package agenthook

import (
	"encoding/json"
	"strings"
)

// Dialect is the agent whose tool vocabulary a payload speaks.
//
// It exists because the three agents spell the same four tools differently --
// Claude Code's `Task` is OpenCode's `task`, and Codex names its editor
// `apply_patch` where the others say `Edit` -- and a classifier that matched on
// the spelling of one of them would silently pass every call from the other
// two. The empty dialect means "infer from the spelling", which is what a
// payload from an agent we have not named yet gets.
type Dialect string

const (
	// DialectClaudeCode is Claude Code, which reads `hookSpecificOutput`.
	DialectClaudeCode Dialect = "claude-code"
	// DialectCodex is the Codex CLI, whose PreToolUse contract is byte for
	// byte the one Claude Code reads. The two differ in where the hook is
	// registered, not in what it says.
	DialectCodex Dialect = "codex"
	// DialectOpenCode is OpenCode, reached through a generated plugin because
	// its `tool.execute.before` returns `Promise<void>` and blocks by
	// throwing. The plugin translates a deny into that throw.
	DialectOpenCode Dialect = "opencode"
)

// Payload is the call an agent is about to make.
//
// The field names are Claude Code's and Codex's, which are the same, and the
// generated OpenCode plugin builds this shape rather than forwarding its own:
// one wire format is the reason a single `hook run` serves all three.
type Payload struct {
	HookEventName string          `json:"hook_event_name"`
	CWD           string          `json:"cwd"`
	ToolName      string          `json:"tool_name"`
	ToolInput     json.RawMessage `json:"tool_input"`
}

// tool is a search-shaped tool, named once for all three dialects.
type tool uint8

const (
	toolOther tool = iota
	toolBash
	toolGrep
	toolGlob
	toolAgent
)

// toolNames maps every spelling the three agents use onto the tool it names.
//
// The spellings are the agents', not ours. Claude Code dispatches research to
// `Task` and this harness to `Agent`; OpenCode lowercases everything; Codex
// names the shell `Bash` like Claude Code but has no glob tool at all. A name
// missing from this table is `toolOther`, which is always allowed -- the gate
// refuses searches, and a tool it cannot recognise is not known to be one.
var toolNames = map[string]tool{
	"bash": toolBash, "shell": toolBash,
	"grep": toolGrep, "search": toolGrep,
	"glob": toolGlob,
	"task": toolAgent, "agent": toolAgent,
}

// classifyTool answers which search-shaped tool a payload names.
func classifyTool(name string) tool {
	return toolNames[strings.ToLower(strings.TrimSpace(name))]
}
