package workspace

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Luqueee/ladygraph/internal/testsupport"
)

// buildMonorepoFixture writes a repository with three referenced projects, two
// compiler installs and a shared base config.
func buildMonorepoFixture(t *testing.T) string {
	t.Helper()
	root := testsupport.TempDir(t)

	writeDiscoveryFile(t, filepath.Join(root, "package.json"), `{"name": "root", "private": true, "workspaces": ["packages/*"]}`)
	writeInstalledTypeScript(t, root, "7.0.2")
	writeDiscoveryFile(t, filepath.Join(root, "tsconfig.base.json"), `{
  // shared by every package
  "compilerOptions": {
    "composite": true,
    "strict": true,
    "declarationDir": "./types",
    "target": "ES2022"
  }
}`)

	// core: no references, inherits everything.
	core := filepath.Join(root, "packages", "core")
	writeDiscoveryFile(t, filepath.Join(core, "package.json"), `{"name": "@example/core"}`)
	writeDiscoveryFile(t, filepath.Join(core, "tsconfig.json"), `{
  "extends": "../../tsconfig.base.json",
  "compilerOptions": {"outDir": "./dist"},
  "include": ["src/**/*.ts"]
}`)
	writeDiscoveryFile(t, filepath.Join(core, "src", "index.ts"), "export const core = 1;\n")
	writeDiscoveryFile(t, filepath.Join(core, "src", "nested", "deep.ts"), "export const deep = 2;\n")
	writeDiscoveryFile(t, filepath.Join(core, "src", "notes.md"), "ignored\n")

	// api: references core, pins an old compiler of its own.
	api := filepath.Join(root, "packages", "api")
	writeDiscoveryFile(t, filepath.Join(api, "package.json"), `{"name": "@example/api", "devDependencies": {"typescript": "4.9.5"}}`)
	writeDiscoveryFile(t, filepath.Join(api, "tsconfig.json"), `{
  "extends": "../../tsconfig.base.json",
  "compilerOptions": {"outDir": "./dist", "strict": false},
  "include": ["src/**/*.ts"],
  "references": [{"path": "../core"}]
}`)
	writeDiscoveryFile(t, filepath.Join(api, "src", "server.ts"), "export const server = 3;\n")
	writeInstalledTypeScript(t, api, "4.9.5")

	// app: references both.
	app := filepath.Join(root, "packages", "app")
	writeDiscoveryFile(t, filepath.Join(app, "package.json"), `{"name": "@example/app"}`)
	writeDiscoveryFile(t, filepath.Join(app, "tsconfig.json"), `{
  "extends": "../../tsconfig.base.json",
  "files": ["src/main.ts"],
  "references": [{"path": "../core"}, {"path": "../api"}]
}`)
	writeDiscoveryFile(t, filepath.Join(app, "src", "main.ts"), "export const main = 4;\n")

	return root
}

func buildFixtureGraph(t *testing.T, root string) *TypeScriptProjectGraph {
	t.Helper()
	repository := Repository{Name: "repo", RealPath: root}
	discovery, err := DiscoverTypeScript(context.Background(), repository)
	if err != nil {
		t.Fatalf("DiscoverTypeScript() error = %v", err)
	}
	resolver := newTestResolver(t, root, testEngine())
	graph, err := NewTypeScriptProjectGraph(context.Background(), repository, discovery, resolver)
	if err != nil {
		t.Fatalf("NewTypeScriptProjectGraph() error = %v", err)
	}
	return graph
}

func TestNewTypeScriptProjectGraphResolvesTheWholeRepository(t *testing.T) {
	root := buildMonorepoFixture(t)
	graph := buildFixtureGraph(t, root)

	// tsconfig.base.json is a config too: it is discovered and resolved even
	// though nothing references it.
	if graph.Len() != 4 {
		t.Fatalf("Len() = %d, want 4 projects: %v", graph.Len(), graph.Order())
	}

	corePath := filepath.Join(root, "packages", "core", "tsconfig.json")
	apiPath := filepath.Join(root, "packages", "api", "tsconfig.json")
	appPath := filepath.Join(root, "packages", "app", "tsconfig.json")

	order := graph.Order()
	positions := make(map[string]int, len(order))
	for index, configPath := range order {
		positions[configPath] = index
	}
	if positions[corePath] > positions[apiPath] || positions[apiPath] > positions[appPath] {
		t.Fatalf("Order() = %v, want core before api before app", order)
	}
	if got := graph.Dependents(corePath); len(got) != 2 || got[0] != apiPath || got[1] != appPath {
		t.Fatalf("Dependents(core) = %v, want api and app sorted", got)
	}
}

func TestTypeScriptProgramMergesInheritedCompilerOptions(t *testing.T) {
	root := buildMonorepoFixture(t)
	graph := buildFixtureGraph(t, root)

	core, ok := graph.Get(filepath.Join(root, "packages", "core", "tsconfig.json"))
	if !ok {
		t.Fatal("Get(core) reported the project as missing")
	}
	if len(core.Extends) != 1 || core.Extends[0] != filepath.Join(root, "tsconfig.base.json") {
		t.Fatalf("Extends = %v, want the shared base", core.Extends)
	}
	if strict, _ := core.CompilerOptions["strict"].(bool); !strict {
		t.Fatalf("strict = %v, want it inherited as true", core.CompilerOptions["strict"])
	}
	if !core.Composite {
		t.Fatal("Composite = false, want it inherited from the base config")
	}
	// A path option declared by the parent is relative to the parent, so it
	// must land next to the base config, not inside the package.
	wantDeclarationDir := filepath.Join(root, "types")
	if got := core.CompilerOptions["declarationDir"]; got != wantDeclarationDir {
		t.Fatalf("declarationDir = %v, want %q", got, wantDeclarationDir)
	}
	// A path option declared by the child is relative to the child.
	wantOutDir := filepath.Join(root, "packages", "core", "dist")
	if got := core.CompilerOptions["outDir"]; got != wantOutDir {
		t.Fatalf("outDir = %v, want %q", got, wantOutDir)
	}

	api, ok := graph.Get(filepath.Join(root, "packages", "api", "tsconfig.json"))
	if !ok {
		t.Fatal("Get(api) reported the project as missing")
	}
	// The child wins over the base config.
	if strict, _ := api.CompilerOptions["strict"].(bool); strict {
		t.Fatal("strict = true for api, want the child override to win")
	}
}

func TestTypeScriptProgramCollectsSourceFiles(t *testing.T) {
	root := buildMonorepoFixture(t)
	graph := buildFixtureGraph(t, root)

	core, _ := graph.Get(filepath.Join(root, "packages", "core", "tsconfig.json"))
	want := []string{
		filepath.Join(root, "packages", "core", "src", "index.ts"),
		filepath.Join(root, "packages", "core", "src", "nested", "deep.ts"),
	}
	if len(core.SourceFiles) != len(want) {
		t.Fatalf("SourceFiles = %v, want %v", core.SourceFiles, want)
	}
	for index := range want {
		if core.SourceFiles[index] != want[index] {
			t.Fatalf("SourceFiles = %v, want %v", core.SourceFiles, want)
		}
	}

	app, _ := graph.Get(filepath.Join(root, "packages", "app", "tsconfig.json"))
	wantApp := filepath.Join(root, "packages", "app", "src", "main.ts")
	if len(app.SourceFiles) != 1 || app.SourceFiles[0] != wantApp {
		t.Fatalf("app SourceFiles = %v, want exactly %q", app.SourceFiles, wantApp)
	}
}

func TestTypeScriptProgramCarriesThePerProjectCompiler(t *testing.T) {
	root := buildMonorepoFixture(t)
	graph := buildFixtureGraph(t, root)

	core, _ := graph.Get(filepath.Join(root, "packages", "core", "tsconfig.json"))
	if core.Version.Source != TypeScriptVersionWorkspace || core.Version.Version != "7.0.2" {
		t.Fatalf("core version = %#v, want the hoisted 7.0.2", core.Version)
	}
	if !core.Version.WithinSupportedWindow {
		t.Fatalf("core is outside the window: %q", core.Version.OutsideWindowReason)
	}

	api, _ := graph.Get(filepath.Join(root, "packages", "api", "tsconfig.json"))
	if api.Version.Source != TypeScriptVersionLocal || api.Version.Version != "4.9.5" {
		t.Fatalf("api version = %#v, want the local 4.9.5", api.Version)
	}

	// The audit list is what ADR 0010 needs: these projects may not produce
	// exact facts.
	unsupported := graph.Unsupported()
	if len(unsupported) != 1 || unsupported[0].ConfigPath != api.ConfigPath {
		t.Fatalf("Unsupported() = %v, want only api", unsupported)
	}
	if !strings.Contains(unsupported[0].Version.OutsideWindowReason, "below the supported minimum") {
		t.Fatalf("reason = %q, want it to explain the degradation", unsupported[0].Version.OutsideWindowReason)
	}
}

func TestTypeScriptProjectGraphIsImmutable(t *testing.T) {
	root := buildMonorepoFixture(t)
	graph := buildFixtureGraph(t, root)
	corePath := filepath.Join(root, "packages", "core", "tsconfig.json")

	first, _ := graph.Get(corePath)
	first.CompilerOptions["strict"] = false
	first.SourceFiles[0] = "tampered"
	if len(first.References) == 0 {
		first.References = append(first.References, "tampered")
	}

	second, _ := graph.Get(corePath)
	if strict, _ := second.CompilerOptions["strict"].(bool); !strict {
		t.Fatal("mutating a returned program changed the graph")
	}
	if strings.Contains(second.SourceFiles[0], "tampered") {
		t.Fatal("mutating returned source files changed the graph")
	}
}

func TestNewTypeScriptProjectGraphRequiresAResolver(t *testing.T) {
	root := buildMonorepoFixture(t)
	repository := Repository{Name: "repo", RealPath: root}
	discovery, err := DiscoverTypeScript(context.Background(), repository)
	if err != nil {
		t.Fatalf("DiscoverTypeScript() error = %v", err)
	}
	if _, err := NewTypeScriptProjectGraph(context.Background(), repository, discovery, nil); err == nil {
		t.Fatal("NewTypeScriptProjectGraph() accepted a nil resolver")
	}
}

func TestNewTypeScriptProjectGraphRejectsAReferenceCycle(t *testing.T) {
	root := testsupport.TempDir(t)
	left := filepath.Join(root, "left")
	right := filepath.Join(root, "right")
	writeDiscoveryFile(t, filepath.Join(root, "package.json"), `{"name": "root"}`)
	writeInstalledTypeScript(t, root, "7.0.2")
	writeDiscoveryFile(t, filepath.Join(left, "tsconfig.json"), `{"compilerOptions": {"composite": true}, "references": [{"path": "../right"}]}`)
	writeDiscoveryFile(t, filepath.Join(right, "tsconfig.json"), `{"compilerOptions": {"composite": true}, "references": [{"path": "../left"}]}`)

	repository := Repository{Name: "repo", RealPath: root}
	discovery, err := DiscoverTypeScript(context.Background(), repository)
	if err != nil {
		t.Fatalf("DiscoverTypeScript() error = %v", err)
	}
	resolver := newTestResolver(t, root, testEngine())
	_, err = NewTypeScriptProjectGraph(context.Background(), repository, discovery, resolver)
	if err == nil {
		t.Fatal("NewTypeScriptProjectGraph() accepted a reference cycle")
	}
	if !strings.Contains(err.Error(), "cycle") {
		t.Fatalf("error = %v, want it to name the cycle", err)
	}
}

func TestNewTypeScriptProjectGraphIsDeterministic(t *testing.T) {
	root := buildMonorepoFixture(t)
	first := buildFixtureGraph(t, root).Order()
	for attempt := range 5 {
		got := buildFixtureGraph(t, root).Order()
		if len(got) != len(first) {
			t.Fatalf("attempt %d: Order() length = %d, want %d", attempt, len(got), len(first))
		}
		for index := range first {
			if got[index] != first[index] {
				t.Fatalf("attempt %d: Order() = %v, want %v", attempt, got, first)
			}
		}
	}
}
