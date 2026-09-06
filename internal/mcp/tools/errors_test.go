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
	// The three that share its exit path and are not it. SYMBOL_NOT_FOUND has a
	// neutral durable-log status, but its MCP error still is not a refusal.
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

func TestIsExpectedAbsenceNamesOnlyAMissingSymbol(t *testing.T) {
	if !IsExpectedAbsence(NewToolError(CodeSymbolNotFound, "no match")) {
		t.Fatal("a missing symbol was not recognised as an expected absence")
	}
	for _, code := range []string{CodeRepositoryNotFound, CodeInvalidArgument, CodeSnapshotUnavailable} {
		if IsExpectedAbsence(NewToolError(code, "no")) {
			t.Fatalf("%s was classified as an expected absence", code)
		}
	}
	if IsExpectedAbsence(errors.New("plain")) {
		t.Fatal("an unclassified error was read as an expected absence")
	}
	if IsExpectedAbsence(nil) {
		t.Fatal("err=nil was classified as an expected absence")
	}
	if !IsExpectedAbsence(fmt.Errorf("resolve: %w", NewToolError(CodeSymbolNotFound, "no match"))) {
		t.Fatal("a wrapped absence stopped being one")
	}
}
