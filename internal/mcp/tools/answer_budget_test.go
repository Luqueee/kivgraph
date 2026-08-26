package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"

	"github.com/Luqueee/kivgraph/internal/hotsnapshot"
)

// MaximumAnswerBytes bounds one answer about a symbol on a repository the index
// could not read completely.
//
// internal/mcp.MaximumResidentSurfaceBytes bounds what a host keeps resident,
// and nothing bounded what an answer costs. That is how LUQUE-2229 shipped: a
// repository-level block grew to 91-98 % of every response -- 3219 tokens for a
// three-reference answer whose rows were 148 -- and it was found months later by
// a benchmark nobody was running, not by a test.
//
// Bytes, not tokens: this package has no tokenizer, and
// benchmarks/mcp-token-cost measures the token figure. The two move together.
//
// The number is measured, not chosen: 1629 bytes for the five unreadable
// packages this repository carries, with the headroom of one more row. Most of
// what is left is the same 89-character sentence repeated once per package, and
// emitting each distinct detail once is the next thing that would move this
// ceiling -- which is a change of output shape, so it needs its own ADR rather
// than a looser guard here.
const MaximumAnswerBytes = 1800

// MaximumOccurrenceGrowthBytes bounds what enumerating more failures of the
// same scopes may add to an answer. It is the digits of a counter, not a row:
// a scope is a package, so ten failures inside one and a thousand inside it
// describe the same unreadable package.
const MaximumOccurrenceGrowthBytes = 64

// invisibleScopeRows builds distinct unreadable packages, each recorded
// occurrences times, in the repository completenessSnapshot names core.
func invisibleScopeRows(distinct, occurrences int) []hotsnapshot.UnresolvedReferenceRow {
	rows := make([]hotsnapshot.UnresolvedReferenceRow, 0, distinct*occurrences)
	for scope := range distinct {
		for occurrence := range occurrences {
			rows = append(rows, hotsnapshot.UnresolvedReferenceRow{
				Key:              fmt.Sprintf("unres-%d-%d", scope, occurrence),
				RepositoryKey:    "repo-core",
				Language:         "go",
				RequestedPackage: fmt.Sprintf("example.com/core/generated-%d", scope),
				RequestedSymbol:  fmt.Sprintf("_Cfunc_probe_%d", occurrence),
				Reason:           "DECLARATION_OUTSIDE_REPOSITORY",
				Detail:           "declared in a Go build cache entry: the package is built from generated or cgo sources",
			})
		}
	}
	return rows
}

// answerBytes is what a session pays for one answer about Core, marshalled the
// way the transport carries it.
func answerBytes(t *testing.T, id uint64, rows []hotsnapshot.UnresolvedReferenceRow) int {
	t.Helper()
	store := completenessSnapshot(t, id, rows...)
	_, response, err := findReferences(context.Background(), nil,
		FindReferencesInput{Name: "Core", Repo: "core"}, store)
	if err != nil {
		t.Fatalf("find_references error = %v", err)
	}
	encoded, err := json.Marshal(response)
	if err != nil {
		t.Fatalf("marshal response: %v", err)
	}
	return len(encoded)
}

// TestAnAnswerStaysWithinItsBudget is the ceiling the resident surface has and
// an answer did not. Five unreadable packages is what this repository actually
// carries.
func TestAnAnswerStaysWithinItsBudget(t *testing.T) {
	bytes := answerBytes(t, 81, invisibleScopeRows(5, 8))
	t.Logf("answer with five invisible scopes = %d bytes", bytes)
	if bytes > MaximumAnswerBytes {
		t.Fatalf("answer = %d bytes, want at most %d: the part that does not "+
			"depend on the question is dominating the part that does", bytes, MaximumAnswerBytes)
	}
}

// TestAnAnswerDoesNotGrowWithFailuresInsideTheSameScopes is the invariant the
// ceiling above cannot express on its own: the answer must scale with the
// number of unreadable packages, never with the number of failures inside them.
// A repository with forty failures of five packages must cost what one with
// five costs.
func TestAnAnswerDoesNotGrowWithFailuresInsideTheSameScopes(t *testing.T) {
	one := answerBytes(t, 82, invisibleScopeRows(5, 1))
	many := answerBytes(t, 83, invisibleScopeRows(5, 8))
	growth := many - one
	t.Logf("one failure per scope = %d bytes, eight = %d bytes, growth = %d", one, many, growth)
	if growth < 0 {
		t.Fatalf("more failures made the answer smaller: %d against %d", many, one)
	}
	if growth > MaximumOccurrenceGrowthBytes {
		t.Fatalf("forty failures of five packages cost %d bytes more than five, "+
			"want at most %d: the answer is enumerating occurrences, not scopes",
			growth, MaximumOccurrenceGrowthBytes)
	}
}
