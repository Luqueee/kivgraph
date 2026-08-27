package agenthook

import (
	"encoding/json"
	"io"
)

// Decision is what the gate tells the agent.
//
// There are two of them and not three. A gate that has nothing to say writes
// nothing, and that is not the same as approving: in Claude Code and Codex an
// explicit `allow` **skips the permission prompt**, so a gate that answered
// `allow` to every call it had no opinion about would silently auto-approve
// every shell command in the session, including the ones the user configured a
// prompt for. Saying nothing leaves the agent's own permission flow exactly
// where it found it, which is the only safe meaning of "no opinion".
type Decision struct {
	// Deny is set when the graph answers this question better.
	Deny bool
	// Reason is what the agent is told, and what the user reads. It names
	// the call to make instead, because a refusal without one is an
	// obstacle rather than a redirect.
	Reason string
}

// Allow is the gate having no opinion.
var Allow = Decision{}

// hookSpecificOutput is the block Claude Code and Codex both read.
//
// The two agents are byte for byte identical here, which is why one binary
// serves both: they differ in where the hook is registered, not in what it
// says. OpenCode reads neither and gets this through a generated plugin that
// turns a deny into a thrown error.
type hookSpecificOutput struct {
	HookEventName            string `json:"hookEventName"`
	PermissionDecision       string `json:"permissionDecision"`
	PermissionDecisionReason string `json:"permissionDecisionReason"`
}

// wireDecision is a deny on the wire.
//
// `permission` and the two message fields are the older spelling, kept because
// they cost one line and they are what a harness predating `hookSpecificOutput`
// reads. They are only ever written for a deny, so an old harness cannot read
// an approval out of them either.
type wireDecision struct {
	Permission         string             `json:"permission"`
	UserMessage        string             `json:"user_message"`
	AgentMessage       string             `json:"agent_message"`
	HookSpecificOutput hookSpecificOutput `json:"hookSpecificOutput"`
}

// Write emits a decision in the form both hosting agents read.
//
// An allow writes nothing at all. See Decision for why that is deliberate.
func (decision Decision) Write(stdout io.Writer) error {
	if !decision.Deny {
		return nil
	}
	return json.NewEncoder(stdout).Encode(wireDecision{
		Permission:   "deny",
		UserMessage:  decision.Reason,
		AgentMessage: decision.Reason,
		HookSpecificOutput: hookSpecificOutput{
			HookEventName:            "PreToolUse",
			PermissionDecision:       "deny",
			PermissionDecisionReason: decision.Reason,
		},
	})
}
