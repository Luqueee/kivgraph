package tools

import (
	"context"
	"strings"
	"testing"
)

// TestEmptyAnswersNameTheNextCall is the adoption contract of LUQUE-1904 that a
// test can actually check. An empty answer read as "no such thing" sends the
// session to grep and it does not come back; read as "nothing references this,
// and here is how to widen it", it is an answer.
func TestEmptyAnswersNameTheNextCall(t *testing.T) {
	store := referenceSnapshot(t, 51)

	// symbol-caller-a references the target and nothing references it.
	_, incoming, err := findReferences(context.Background(), nil, FindReferencesInput{
		StableKey: "symbol-caller-a", Direction: FindReferencesDirectionIncoming,
	}, store)
	if err != nil {
		t.Fatal(err)
	}
	if incoming.Total != 0 {
		t.Fatalf("fixture changed: symbol-caller-a has %d incoming references", incoming.Total)
	}
	if !strings.Contains(incoming.Guidance, "nothing references this symbol") ||
		!strings.Contains(incoming.Guidance, "find_cross_repo_consumers") {
		t.Fatalf("empty incoming guidance = %q, want an absence and the next call", incoming.Guidance)
	}

	// A non-empty answer says nothing: fifteen tokens of advice on every row is
	// how a saving turns into a cost.
	_, populated, err := findReferences(context.Background(), nil, FindReferencesInput{
		StableKey: "symbol-target", Direction: FindReferencesDirectionIncoming,
	}, store)
	if err != nil {
		t.Fatal(err)
	}
	if populated.Guidance != "" {
		t.Fatalf("populated answer guidance = %q, want silence", populated.Guidance)
	}

	// A truncated answer says which filters narrow it, before offering the page.
	_, truncated, err := findReferences(context.Background(), nil, FindReferencesInput{
		StableKey: "symbol-target", Direction: FindReferencesDirectionIncoming, Limit: 1,
	}, store)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(truncated.Guidance, "narrow with") || !strings.Contains(truncated.Guidance, "cursor") {
		t.Fatalf("truncated guidance = %q, want narrowing before paging", truncated.Guidance)
	}
}
