package indexing

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/Luqueee/kivgraph/internal/indexer"
)

// TestSummaryCarriesEveryNotLoadedCount is the negative that matters: a caller
// reading this protocol cannot tell a language with no code from a language this
// machine could not read, and both contribute zero symbols. Three of the four
// reasons a repository is absent used to be counted into a field nobody
// published -- one of them never even reached the human report -- so a client
// that only saw the symbol counts read silence as coverage.
func TestSummaryCarriesEveryNotLoadedCount(t *testing.T) {
	summary := SummaryFromReport(indexer.FullReport{
		GoModulesNotLoaded:          3,
		RustWorkspacesNotLoaded:     5,
		PythonRepositoriesNotLoaded: 7,
		DartRepositoriesNotLoaded:   9,
	})

	want := IndexSummary{
		GoModulesNotLoaded:          3,
		RustWorkspacesNotLoaded:     5,
		PythonRepositoriesNotLoaded: 7,
		DartRepositoriesNotLoaded:   9,
	}
	if summary != want {
		t.Fatalf("summary = %#v, want %#v", summary, want)
	}

	// The wire is the contract, not the field name: a caller reads these keys.
	encoded, err := json.Marshal(summary)
	if err != nil {
		t.Fatalf("Marshal() error = %v", err)
	}
	for _, key := range [...]string{
		"go_modules_not_loaded",
		"rust_workspaces_not_loaded",
		"python_repositories_not_loaded",
		"dart_repositories_not_loaded",
	} {
		if !strings.Contains(string(encoded), key) {
			t.Errorf("result is missing %q: %s", key, encoded)
		}
	}
}
