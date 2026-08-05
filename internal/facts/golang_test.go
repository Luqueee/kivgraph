package facts

import (
	"context"
	"errors"
	"path/filepath"
	"testing"

	"github.com/Luqueee/luque/internal/goloader"
	"github.com/Luqueee/luque/internal/goworkspace"
	"github.com/Luqueee/luque/internal/workspace"
)

var fixtureRoot = filepath.Join("..", "..", "testdata", "go", "cross-repository")

func fixtureRepositories(t *testing.T) []workspace.Repository {
	t.Helper()
	root, err := filepath.Abs(fixtureRoot)
	if err != nil {
		t.Fatalf("resolve fixture: %v", err)
	}
	repositories := make([]workspace.Repository, 0, 3)
	for _, name := range []string{"shared-library", "consumer-a", "consumer-b"} {
		path := filepath.Join(root, name)
		repositories = append(repositories, workspace.Repository{Name: name, Path: path, RealPath: path})
	}
	return repositories
}

// normalizeFixture indexes one repository of the fixture end to end.
func normalizeFixture(t *testing.T, repositoryName string) (Set, GoReport) {
	t.Helper()
	repositories := fixtureRepositories(t)
	plan, err := goworkspace.BuildPlan(context.Background(), repositories, goworkspace.Options{})
	if err != nil {
		t.Fatalf("BuildPlan() error = %v", err)
	}
	workFile := filepath.Join(t.TempDir(), "go.work")
	if _, err := goworkspace.Write(context.Background(), workFile, plan, repositories); err != nil {
		t.Fatalf("Write() error = %v", err)
	}

	var repository workspace.Repository
	for _, candidate := range repositories {
		if candidate.Name == repositoryName {
			repository = candidate
		}
	}
	if repository.Name == "" {
		t.Fatalf("unknown fixture repository %q", repositoryName)
	}

	modules, err := workspace.NewGoModuleRegistry(context.Background(), repository)
	if err != nil {
		t.Fatalf("NewGoModuleRegistry() error = %v", err)
	}
	registry, err := goloader.NewModuleRegistry(context.Background(), repositories)
	if err != nil {
		t.Fatalf("NewModuleRegistry() error = %v", err)
	}

	merged := Set{}
	total := GoReport{}
	for _, module := range modules.List() {
		result, err := goloader.Load(context.Background(), goloader.Options{
			Directory: module.RootPath,
			WorkFile:  workFile,
		})
		if err != nil {
			t.Fatalf("Load(%s) error = %v", module.ModulePath, err)
		}
		if len(result.Errors) != 0 {
			t.Fatalf("load errors = %#v", result.Errors)
		}
		definitions, err := goloader.ExtractDefinitions(context.Background(), result,
			goloader.DefinitionOptions{Repository: repository.Name})
		if err != nil {
			t.Fatalf("ExtractDefinitions() error = %v", err)
		}
		keyed, err := goloader.AssignStableKeys(context.Background(), definitions)
		if err != nil {
			t.Fatalf("AssignStableKeys() error = %v", err)
		}
		uses, err := goloader.ExtractUses(context.Background(), result,
			goloader.UseOptions{Repository: repository.Name})
		if err != nil {
			t.Fatalf("ExtractUses() error = %v", err)
		}
		references, err := goloader.ClassifyReferences(context.Background(), result, uses)
		if err != nil {
			t.Fatalf("ClassifyReferences() error = %v", err)
		}
		cross, err := goloader.ResolveCrossRepository(context.Background(), uses, registry,
			goloader.CrossRepositoryOptions{ConsumerRepository: repository.Name})
		if err != nil {
			t.Fatalf("ResolveCrossRepository() error = %v", err)
		}
		unresolved, err := goloader.ClassifyUnresolved(context.Background(), result, cross,
			goloader.UnresolvedOptions{Repository: repository.Name, WorkspaceConflicts: plan.Conflicts})
		if err != nil {
			t.Fatalf("ClassifyUnresolved() error = %v", err)
		}

		set, report, err := NormalizeGo(context.Background(), GoInput{
			Repository:      repository,
			Definitions:     keyed,
			References:      references,
			CrossRepository: cross,
			Unresolved:      unresolved,
		})
		if err != nil {
			t.Fatalf("NormalizeGo() error = %v", err)
		}
		merged.Merge(set)
		total.EdgesWithoutSource += report.EdgesWithoutSource
		total.EdgesWithoutTarget += report.EdgesWithoutTarget
		total.UnresolvedWithoutFile += report.UnresolvedWithoutFile
	}
	return merged, total
}

func TestNormalizeGoProducesAValidatedGraph(t *testing.T) {
	set, _ := normalizeFixture(t, "shared-library")

	if err := set.Validate(); err != nil {
		t.Fatalf("Validate() error = %v", err)
	}
	if len(set.Repositories) != 1 || set.Repositories[0].Key != RepositoryKey("shared-library") {
		t.Fatalf("repositories = %#v", set.Repositories)
	}
	if len(set.Packages) != 1 ||
		set.Packages[0].Name != "example.com/luque-fixture/shared/api" ||
		set.Packages[0].Container != "example.com/luque-fixture/shared" {
		t.Fatalf("packages = %#v", set.Packages)
	}
	if len(set.Files) != 1 || set.Files[0].Path != "api/api.go" {
		t.Fatalf("files = %#v", set.Files)
	}

	// Paths are repository relative: a key must not embed the machine.
	for _, file := range set.Files {
		if filepath.IsAbs(file.Path) {
			t.Fatalf("file key embeds an absolute path: %#v", file)
		}
	}

	byQualifiedName := make(map[string]Symbol, len(set.Symbols))
	for _, symbol := range set.Symbols {
		byQualifiedName[symbol.QualifiedName] = symbol
	}
	for _, name := range []string{"Answer", "Shape", "Shape.Width", "Shape.Area", "Compute", "Register"} {
		symbol, declared := byQualifiedName[name]
		if !declared {
			t.Fatalf("symbol %q missing from %v", name, byQualifiedName)
		}
		if symbol.Key == "" || symbol.CanonicalIdentity == "" || symbol.Language != LanguageGo {
			t.Fatalf("symbol %q = %#v", name, symbol)
		}
	}

	defines := 0
	for _, edge := range set.Edges {
		switch edge.Kind {
		case Defines:
			defines++
			if edge.Confidence != StructuralCertain || edge.Provenance != GoTypesDefinition {
				t.Fatalf("DEFINES edge = %#v", edge)
			}
		case ContainsPackage, ContainsFile:
			if edge.Provenance != PackageManifest {
				t.Fatalf("container edge = %#v", edge)
			}
		}
	}
	if defines != len(set.Symbols) {
		t.Fatalf("DEFINES edges = %d, want one per symbol (%d)", defines, len(set.Symbols))
	}
}

func TestNormalizeGoKeepsCrossRepositoryIdentity(t *testing.T) {
	provider, _ := normalizeFixture(t, "shared-library")
	consumer, report := normalizeFixture(t, "consumer-a")

	if err := consumer.Validate(); err == nil {
		t.Fatalf("a consumer alone must not validate: its targets live elsewhere")
	}

	providerKeys := make(map[string]Symbol, len(provider.Symbols))
	for _, symbol := range provider.Symbols {
		providerKeys[symbol.Key] = symbol
	}

	kinds := make(map[EdgeKind]int)
	crossEdges := 0
	for _, edge := range consumer.Edges {
		if edge.Kind == Defines || edge.Kind == ContainsFile || edge.Kind == ContainsPackage {
			continue
		}
		kinds[edge.Kind]++
		if symbol, exists := providerKeys[edge.TargetKey]; exists {
			crossEdges++
			if !edge.Confidence.Exact() || edge.Provenance != GoObjectPath {
				t.Fatalf("cross-repository edge = %#v to %q", edge, symbol.QualifiedName)
			}
			if edge.EvidenceKey == "" {
				t.Fatalf("cross-repository edge without evidence: %#v", edge)
			}
		}
	}
	if crossEdges != 8 {
		t.Fatalf("cross-repository edges = %d, want the eight fixture edges", crossEdges)
	}
	for _, kind := range []EdgeKind{CallsDirect, PassesAsCallback, TypeUses, References} {
		if kinds[kind] == 0 {
			t.Fatalf("edge kind %q missing: %#v", kind, kinds)
		}
	}
	if report.EdgesWithoutTarget != 0 {
		t.Fatalf("dropped targets = %d, want none in the positive fixture", report.EdgesWithoutTarget)
	}

	// Merged with its provider, the same graph validates.
	merged := Set{}
	merged.Merge(provider)
	merged.Merge(consumer)
	if err := merged.Validate(); err != nil {
		t.Fatalf("merged Validate() error = %v", err)
	}
}

func TestNormalizeGoIsDeterministic(t *testing.T) {
	first, _ := normalizeFixture(t, "consumer-b")
	second, _ := normalizeFixture(t, "consumer-b")

	if len(first.Edges) != len(second.Edges) || len(first.Symbols) != len(second.Symbols) {
		t.Fatalf("normalisation is not deterministic")
	}
	for index := range first.Edges {
		if first.Edges[index] != second.Edges[index] {
			t.Fatalf("edge %d differs between runs:\n%#v\n%#v", index, first.Edges[index], second.Edges[index])
		}
	}
	for index := range first.Symbols {
		if first.Symbols[index].Key != second.Symbols[index].Key {
			t.Fatalf("symbol %d differs between runs", index)
		}
	}
}

func TestNormalizeGoRejectsAnIncompleteRequest(t *testing.T) {
	if _, _, err := NormalizeGo(context.Background(), GoInput{}); !errors.Is(err, ErrInvalidFacts) {
		t.Fatalf("NormalizeGo() error = %v, want ErrInvalidFacts", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, _, err := NormalizeGo(ctx, GoInput{
		Repository: workspace.Repository{Name: "repository", Path: t.TempDir()},
	}); !errors.Is(err, context.Canceled) {
		t.Fatalf("NormalizeGo() error = %v, want context.Canceled", err)
	}
}
