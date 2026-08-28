package tools

import (
	"errors"
	"fmt"
	"testing"
)

// TestIsRefusalNamesOnlyTheDesignedDecline pins the vocabulary the counters
// read. A code that joins RefusalCodes stops being reported as a failure
// everywhere at once, so the list is a contract and not a convenience.
func TestIsRefusalNamesOnlyTheDesignedDecline(t *testing.T) {
	if !IsRefusal(NewToolError(CodeAmbiguousSymbol, "several declarations")) {
		t.Fatal("the ambiguity refusal ADR 0077 designed is not recognised as one")
	}
	// The three that share its exit path and are not it. SYMBOL_NOT_FOUND is
	// the one that matters: it is 31 of the 63 the measurement counted, and it
	// is a real failure to answer.
	for _, code := range []string{CodeSymbolNotFound, CodeInvalidArgument, CodeSnapshotUnavailable} {
		if IsRefusal(NewToolError(code, "no")) {
			t.Fatalf("%s was classified as a designed refusal", code)
		}
	}
	if IsRefusal(nil) {
		t.Fatal("a call that did not fail was classified as a refusal")
	}
	if IsRefusal(errors.New("plain")) {
		t.Fatal("an unclassified error was read as a refusal")
	}
	// Wrapped, because that is how it reaches a caller that added context.
	if !IsRefusal(fmt.Errorf("resolve: %w", NewToolError(CodeAmbiguousSymbol, "several"))) {
		t.Fatal("a wrapped refusal stopped being one")
	}
}
