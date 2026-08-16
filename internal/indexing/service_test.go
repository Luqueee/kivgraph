package indexing

import (
	"fmt"
	"strings"
	"testing"

	"github.com/Luqueee/kivgraph/internal/config"
)

// Indexing a project that is already registered must not be a conflict. The
// caller asked for that repository to be in the graph; it is. Refusing sends
// an operator hunting through the registry for a name they already chose --
// which is exactly what happens after the graph is discarded and the registry,
// correctly, survives it.
func TestUpsertRepositoryIsIdempotentForTheSameProject(t *testing.T) {
	registry := config.RepositoriesFile{Version: 1, Repositories: []config.Repository{
		{Name: "kivgraph", Path: "/repos/kivgraph", Languages: []string{"go", "typescript"},
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
		{Name: "kivgraph", Path: "/repos/kivgraph", Languages: []string{"go"},
			Exclusions: []string{"**/testdata", "**/dist"}},
	}}

	changed, err := upsertRepository(&registry, config.Repository{
		Name: "kivgraph", Path: "/repos/kivgraph", Languages: []string{"go", "typescript"},
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
		Name: "mole", Path: "/elsewhere/mole", Languages: []string{"go"},
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
		{Name: "kivgraph", Path: "/repos/kivgraph", Languages: []string{"go"}},
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

// The batch is registered in full before anything is built. The rebuild that
// follows is the expensive part and it is paid once, so a caller adding
// eleven repositories must be able to hand them over together.
func TestUpsertRepositoryAccumulatesAWholeBatch(t *testing.T) {
	registry := config.RepositoriesFile{Version: 1, Repositories: []config.Repository{
		{Name: "kivgraph", Path: "/repos/kivgraph", Languages: []string{"go"}},
	}}

	for index := range 11 {
		changed, err := upsertRepository(&registry, config.Repository{
			Name:      fmt.Sprintf("repo-%02d", index),
			Path:      fmt.Sprintf("/repos/repo-%02d", index),
			Languages: []string{"go"},
		})
		if err != nil {
			t.Fatalf("upsertRepository(%d) error = %v", index, err)
		}
		if !changed {
			t.Fatalf("project %d did not reach the registry", index)
		}
	}
	if len(registry.Repositories) != 12 {
		t.Fatalf("repositories = %d, want the original plus the batch", len(registry.Repositories))
	}
	// The batch is validated as one registry, so a name repeated inside it
	// is caught before a single rebuild runs.
	if _, err := upsertRepository(&registry, config.Repository{
		Name: "repo-03", Path: "/elsewhere/repo-03", Languages: []string{"go"},
	}); err == nil {
		t.Fatal("a name already held inside the batch was accepted")
	}
}

// TestNormalizeProjectLanguagesFollowsTheOneVocabulary keeps this entry point
// from growing a second list: what `init` writes and what the pass analyses
// must be the same set, or a caller registers a repository nothing indexes.
func TestNormalizeProjectLanguagesFollowsTheOneVocabulary(t *testing.T) {
	languages, err := normalizeProjectLanguages([]string{"Rust", " go "})
	if err != nil {
		t.Fatalf("normalizeProjectLanguages() error = %v", err)
	}
	if len(languages) != 2 || languages[0] != "rust" || languages[1] != "go" {
		t.Fatalf("languages = %#v", languages)
	}
	for _, invalid := range [][]string{{"python"}, {"rust", "rust"}, {""}, nil} {
		if _, err := normalizeProjectLanguages(invalid); err == nil {
			t.Fatalf("normalizeProjectLanguages(%#v) accepted the input", invalid)
		}
	}
}

// TestOptionsFromConfigCarriesEveryConfiguredLanguage is why this mapping
// exists at all. The MCP tool built its own request and named no Rust field in
// it, so every project registered through it failed on "the Rust analyzer
// command is required" -- the pass was asking for a setting the caller never
// forwarded, while the same configuration worked from the CLI.
func TestOptionsFromConfigCarriesEveryConfiguredLanguage(t *testing.T) {
	configuration := config.DefaultConfig()
	configuration.Go.BuildTags = []string{"ladybug"}
	configuration.TypeScript.WorkerCommand = "node worker.js"
	configuration.Rust.AnalyzerCommand = "/opt/bin/rust-analyzer"
	configuration.Rust.TargetDirectory = "/state/rust-target"
	configuration.Rust.Features = []string{"tokio"}

	options := OptionsFromConfig(configuration)

	if len(options.GoBuildTags) != 1 || options.GoBuildTags[0] != "ladybug" {
		t.Fatalf("go build tags = %#v", options.GoBuildTags)
	}
	if options.TypeScriptWorker != "node worker.js" {
		t.Fatalf("typescript worker = %q", options.TypeScriptWorker)
	}
	if options.RustAnalyzer != "/opt/bin/rust-analyzer" || options.RustTargetDirectory != "/state/rust-target" {
		t.Fatalf("rust analyzer = %q, target directory = %q", options.RustAnalyzer, options.RustTargetDirectory)
	}
	if len(options.RustFeatures) != 1 || options.RustFeatures[0] != "tokio" {
		t.Fatalf("rust features = %#v", options.RustFeatures)
	}
	if !options.RustBuildScripts || !options.RustProcMacros || !options.RustIncludeTests {
		t.Fatalf("rust expansion defaults = %+v", options)
	}
	if options.Root == "" || options.CacheDirectory == "" {
		t.Fatalf("storage options = %+v", options)
	}
	// The caller owns exactly these, and nothing else.
	if options.Repositories != nil || options.WorkingDirectory != "" || options.ResolverVersion != "" {
		t.Fatalf("options = %+v, want the caller's own fields left empty", options)
	}
	if options.Progress != nil || options.RebuildProgress != nil {
		t.Fatal("the configuration decides no progress sink")
	}
}
