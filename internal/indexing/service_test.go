package indexing

import (
	"strings"
	"testing"

	"github.com/Luqueee/ladygraph/internal/config"
)

// Indexing a project that is already registered must not be a conflict. The
// caller asked for that repository to be in the graph; it is. Refusing sends
// an operator hunting through the registry for a name they already chose --
// which is exactly what happens after the graph is discarded and the registry,
// correctly, survives it.
func TestUpsertRepositoryIsIdempotentForTheSameProject(t *testing.T) {
	registry := config.RepositoriesFile{Version: 1, Repositories: []config.Repository{
		{Name: "ladygraph", Path: "/repos/ladygraph", Languages: []string{"go", "typescript"},
			Exclusions: []string{"**/testdata"}},
		{Name: "mole", Path: "/repos/mole", Languages: []string{"go"}},
	}}

	changed, err := upsertRepository(&registry, config.Repository{
		Name: "mole", Path: "/repos/mole", Languages: []string{"go"},
	})
	if err != nil {
		t.Fatalf("upsertRepository() error = %v", err)
	}
	if changed {
		t.Fatal("re-registering an identical project rewrote the registry")
	}
	if len(registry.Repositories) != 2 {
		t.Fatalf("repositories = %#v, want the registry untouched", registry.Repositories)
	}
}

// A repository registered again with different languages keeps everything the
// request cannot express. Exclusions are the operator's, not the caller's.
func TestUpsertRepositoryKeepsExclusionsWhenLanguagesChange(t *testing.T) {
	registry := config.RepositoriesFile{Version: 1, Repositories: []config.Repository{
		{Name: "ladygraph", Path: "/repos/ladygraph", Languages: []string{"go"},
			Exclusions: []string{"**/testdata", "**/dist"}},
	}}

	changed, err := upsertRepository(&registry, config.Repository{
		Name: "ladygraph", Path: "/repos/ladygraph", Languages: []string{"go", "typescript"},
	})
	if err != nil {
		t.Fatalf("upsertRepository() error = %v", err)
	}
	if !changed {
		t.Fatal("a language change must reach the registry")
	}
	entry := registry.Repositories[0]
	if strings.Join(entry.Languages, ",") != "go,typescript" {
		t.Fatalf("languages = %v, want the requested set", entry.Languages)
	}
	if strings.Join(entry.Exclusions, ",") != "**/testdata,**/dist" {
		t.Fatalf("exclusions = %v, want the ones already on file", entry.Exclusions)
	}
}

// A name held by another directory stays a conflict: nothing can decide which
// of the two repositories the name means.
func TestUpsertRepositoryRefusesANameHeldByAnotherPath(t *testing.T) {
	registry := config.RepositoriesFile{Version: 1, Repositories: []config.Repository{
		{Name: "mole", Path: "/repos/mole", Languages: []string{"go"}},
	}}

	_, err := upsertRepository(&registry, config.Repository{
		Name: "Mole", Path: "/elsewhere/mole", Languages: []string{"go"},
	})
	if err == nil {
		t.Fatal("upsertRepository() accepted a name already held by another directory")
	}
	for _, want := range []string{"already registered", "/repos/mole"} {
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("error = %q, want it to mention %q", err, want)
		}
	}
	if len(registry.Repositories) != 1 || registry.Repositories[0].Path != "/repos/mole" {
		t.Fatalf("repositories = %#v, want the registry untouched", registry.Repositories)
	}
}

func TestUpsertRepositoryAppendsAProjectThatIsNew(t *testing.T) {
	registry := config.RepositoriesFile{Version: 1, Repositories: []config.Repository{
		{Name: "ladygraph", Path: "/repos/ladygraph", Languages: []string{"go"}},
	}}

	changed, err := upsertRepository(&registry, config.Repository{
		Name: "mole", Path: "/repos/mole", Languages: []string{"go"},
	})
	if err != nil {
		t.Fatalf("upsertRepository() error = %v", err)
	}
	if !changed || len(registry.Repositories) != 2 {
		t.Fatalf("repositories = %#v, want the new project appended", registry.Repositories)
	}
}
