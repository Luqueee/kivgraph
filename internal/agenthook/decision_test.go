package agenthook

import (
	"encoding/json"
	"strings"
	"testing"
)

// TestAllowWritesNothing is the negative that matters most in this package.
//
// An explicit `allow` on this wire does not mean "carry on", it means "skip the
// permission prompt". If the gate ever emits one for the calls it has no
// opinion about -- which is nearly all of them -- it silently auto-approves
// every shell command in the session. Writing nothing is the only answer that
// leaves the agent's permission flow untouched.
func TestAllowWritesNothing(t *testing.T) {
	var out strings.Builder
	if err := Allow.Write(&out); err != nil {
		t.Fatalf("write allow: %v", err)
	}
	if out.Len() != 0 {
		t.Fatalf("an allow wrote %q; anything here auto-approves the call", out.String())
	}
}

// TestDenyIsReadableByBothHostingAgents checks the whole document, because the
// two spellings have to agree: an agent that reads `hookSpecificOutput` and one
// that reads `permission` must not be told different things about the same
// call.
func TestDenyIsReadableByBothHostingAgents(t *testing.T) {
	const reason = "STOP: `NewServer` has 4 declarations in 2 repositories."
	var out strings.Builder
	if err := (Decision{Deny: true, Reason: reason}).Write(&out); err != nil {
		t.Fatalf("write deny: %v", err)
	}
	var got map[string]any
	if err := json.Unmarshal([]byte(out.String()), &got); err != nil {
		t.Fatalf("decode deny: %v", err)
	}
	want := map[string]any{
		"permission":    "deny",
		"user_message":  reason,
		"agent_message": reason,
		"hookSpecificOutput": map[string]any{
			"hookEventName":            "PreToolUse",
			"permissionDecision":       "deny",
			"permissionDecisionReason": reason,
		},
	}
	if !jsonEqual(got, want) {
		t.Fatalf("got %#v, want %#v", got, want)
	}
}

func jsonEqual(got, want any) bool {
	left, err := json.Marshal(got)
	if err != nil {
		return false
	}
	right, err := json.Marshal(want)
	if err != nil {
		return false
	}
	return string(left) == string(right)
}

// TestAdvisoryVotesOnNothing is the negative that guards the advisory.
//
// The tempting shape for attaching context is `permissionDecision: "allow"`
// with the text in `permissionDecisionReason`: it delivers the same words and
// it is one field shorter. It also grants the call, and on this wire granting
// means skipping the permission prompt the user configured -- so a briefing
// written that way would quietly strip the prompt from every Kivgraph tool call
// it rode along with. The absence of those fields is the whole point, so it is
// asserted directly rather than inferred from the document.
func TestAdvisoryVotesOnNothing(t *testing.T) {
	const context = "Kivgraph, before the first call of this session."
	var out strings.Builder
	if err := (Decision{Context: context}).Write(&out); err != nil {
		t.Fatalf("write advisory: %v", err)
	}
	var got map[string]any
	if err := json.Unmarshal([]byte(out.String()), &got); err != nil {
		t.Fatalf("decode advisory: %v", err)
	}
	want := map[string]any{
		"hookSpecificOutput": map[string]any{
			"hookEventName":     "PreToolUse",
			"additionalContext": context,
		},
	}
	if !jsonEqual(got, want) {
		t.Fatalf("got %#v, want %#v", got, want)
	}
	for _, granting := range []string{"permission", "permissionDecision", "user_message", "agent_message"} {
		if strings.Contains(out.String(), granting) {
			t.Fatalf("an advisory wrote %q; it must not answer the permission question at all", granting)
		}
	}
}

// TestDenyIgnoresAnyContext keeps one call from being told two things.
//
// Context is only ever read when Deny is false. A refusal already carries the
// call to make instead, and a document holding both a refusal and a paragraph
// of guidance invites an agent to act on the half that was not the verdict.
func TestDenyIgnoresAnyContext(t *testing.T) {
	const reason = "STOP: `New` has 24 declarations in 5 repositories."
	var out strings.Builder
	if err := (Decision{Deny: true, Reason: reason, Context: "ignored"}).Write(&out); err != nil {
		t.Fatalf("write deny: %v", err)
	}
	if strings.Contains(out.String(), "ignored") || strings.Contains(out.String(), "additionalContext") {
		t.Fatalf("a refusal carried context as well as a verdict: %s", out.String())
	}
}
