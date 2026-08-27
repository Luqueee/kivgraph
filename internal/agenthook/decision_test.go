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
