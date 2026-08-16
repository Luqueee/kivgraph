package goloader

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/Luqueee/kivgraph/internal/goworkspace"
	"github.com/Luqueee/kivgraph/internal/testsupport"
	"github.com/Luqueee/kivgraph/internal/workspace"
)

var goFixtureRoot = filepath.Join("..", "..", "testdata", "go", "cross-repository")

// goFixtureRepositories are the repositories of the LUQUE-0811 fixture.
func goFixtureRepositories(t *testing.T) []workspace.Repository {
	t.Helper()
	root, err := filepath.Abs(goFixtureRoot)
	if err != nil {
		t.Fatalf("resolve fixture root: %v", err)
	}
	repositories := make([]workspace.Repository, 0, 3)
	for _, name := range []string{"shared-library", "consumer-a", "consumer-b"} {
		path := filepath.Join(root, name)
		repositories = append(repositories, workspace.Repository{
			Name: name, Path: path, RealPath: path,
		})
	}
	return repositories
}

// goFixtureWorkspace writes the synthetic workspace outside the fixture.
func goFixtureWorkspace(t *testing.T, repositories []workspace.Repository) string {
	t.Helper()
	plan, err := goworkspace.BuildPlan(context.Background(), repositories, goworkspace.Options{})
	if err != nil {
		t.Fatalf("BuildPlan() error = %v", err)
	}
	if len(plan.Conflicts) != 0 {
		t.Fatalf("fixture workspace has conflicts: %#v", plan.Conflicts)
	}
	target := filepath.Join(testsupport.TempDir(t), "state", "go.work")
	if _, err := goworkspace.Write(context.Background(), target, plan, repositories); err != nil {
		t.Fatalf("Write() error = %v", err)
	}
	return target
}

type fixtureFacts struct {
	references []Reference
	cross      map[string]CrossRepositoryReference
	unresolved []UnresolvedReference
}

func loadFixtureConsumer(t *testing.T, name string) fixtureFacts {
	t.Helper()
	repositories := goFixtureRepositories(t)
	workFile := goFixtureWorkspace(t, repositories)

	var directory string
	for _, repository := range repositories {
		if repository.Name == name {
			directory = repository.Path
		}
	}
	if directory == "" {
		t.Fatalf("unknown fixture repository %q", name)
	}

	result, err := Load(context.Background(), Options{Directory: directory, WorkFile: workFile})
	if err != nil {
		t.Fatalf("Load(%s) error = %v", name, err)
	}
	if len(result.Errors) != 0 {
		t.Fatalf("fixture %s does not load cleanly: %#v", name, result.Errors)
	}
	uses, err := ExtractUses(context.Background(), result, UseOptions{Repository: name})
	if err != nil {
		t.Fatalf("ExtractUses() error = %v", err)
	}
	references, err := ClassifyReferences(context.Background(), result, uses)
	if err != nil {
		t.Fatalf("ClassifyReferences() error = %v", err)
	}
	registry, err := NewModuleRegistry(context.Background(), repositories)
	if err != nil {
		t.Fatalf("NewModuleRegistry() error = %v", err)
	}
	crossReferences, err := ResolveCrossRepository(context.Background(), uses, registry,
		CrossRepositoryOptions{ConsumerRepository: name})
	if err != nil {
		t.Fatalf("ResolveCrossRepository() error = %v", err)
	}
	unresolved, err := ClassifyUnresolved(context.Background(), result, crossReferences,
		UnresolvedOptions{Repository: name})
	if err != nil {
		t.Fatalf("ClassifyUnresolved() error = %v", err)
	}

	byTarget := make(map[string]CrossRepositoryReference, len(crossReferences))
	for _, reference := range crossReferences {
		byTarget[reference.TargetQualifiedName] = reference
	}
	return fixtureFacts{references: references, cross: byTarget, unresolved: unresolved}
}

func referenceKinds(references []Reference) map[string][]ReferenceKind {
	kinds := make(map[string][]ReferenceKind)
	for _, reference := range references {
		kinds[reference.TargetQualifiedName] = append(kinds[reference.TargetQualifiedName], reference.Kind)
	}
	return kinds
}

func TestGoFixtureConsumerAResolvesCallsMethodsAndCallbacks(t *testing.T) {
	facts := loadFixtureConsumer(t, "consumer-a")
	kinds := referenceKinds(facts.references)

	if countKind(kinds["Compute"], ReferenceCallsDirect) != 1 {
		t.Fatalf("direct call = %v", kinds["Compute"])
	}
	if countKind(kinds["Compute"], ReferencePassesAsCallback) != 1 {
		t.Fatalf("callback = %v", kinds["Compute"])
	}
	if countKind(kinds["Shape.Area"], ReferenceCallsDirect) != 1 {
		t.Fatalf("method call = %v", kinds["Shape.Area"])
	}
	if countKind(kinds["Register"], ReferenceCallsDirect) != 1 {
		t.Fatalf("callback receiver call = %v", kinds["Register"])
	}
	if countKind(kinds["Shape"], ReferenceTypeUses) != 1 {
		t.Fatalf("composite literal type = %v", kinds["Shape"])
	}
	if countKind(kinds["Shape.Width"], ReferenceRead) != 2 {
		t.Fatalf("field reads = %v", kinds["Shape.Width"])
	}

	for name, reference := range facts.cross {
		if reference.Status != CrossRepositoryResolved {
			t.Fatalf("target %q status = %q", name, reference.Status)
		}
		if reference.Provider.Repository != "shared-library" {
			t.Fatalf("target %q provider = %#v", name, reference.Provider)
		}
	}
	if len(facts.unresolved) != 0 {
		t.Fatalf("unresolved = %#v", facts.unresolved)
	}
}

func TestGoFixtureConsumerBResolvesAliasAndReplacedModule(t *testing.T) {
	facts := loadFixtureConsumer(t, "consumer-b")

	aliased, resolved := facts.cross["Compute"]
	if !resolved {
		t.Fatalf("aliased import was not resolved: %#v", facts.cross)
	}
	if aliased.Provider.Repository != "shared-library" ||
		aliased.TargetPackagePath != "example.com/kivgraph-fixture/shared/api" {
		t.Fatalf("aliased target = %#v", aliased)
	}
	if aliased.Status != CrossRepositoryResolved || aliased.TargetObjectPath != "Compute" {
		t.Fatalf("aliased status = %#v", aliased)
	}

	// The replaced module is provided by the consumer repository itself, so
	// the edge stays inside consumer-b while crossing a module boundary.
	legacy, replaced := facts.cross["Legacy"]
	if !replaced {
		t.Fatalf("replaced module target missing: %#v", facts.cross)
	}
	if legacy.Provider.Repository != "consumer-b" ||
		legacy.TargetModulePath != "example.com/kivgraph-fixture/legacy" {
		t.Fatalf("replaced target = %#v", legacy)
	}
	if legacy.Status != CrossRepositoryResolved {
		t.Fatalf("replaced status = %q", legacy.Status)
	}
	if len(facts.unresolved) != 0 {
		t.Fatalf("unresolved = %#v", facts.unresolved)
	}
}

func TestGoFixtureTargetsMatchProviderDefinitions(t *testing.T) {
	repositories := goFixtureRepositories(t)
	own := make(map[string]KeyedDefinition)
	for _, repository := range repositories {
		// A repository may hold several modules; each one is its own load.
		modules, err := workspace.NewGoModuleRegistry(context.Background(), repository)
		if err != nil {
			t.Fatalf("NewGoModuleRegistry(%s) error = %v", repository.Name, err)
		}
		for _, module := range modules.List() {
			result, err := Load(context.Background(), Options{Directory: module.RootPath})
			if err != nil {
				t.Fatalf("Load(%s) error = %v", module.ModulePath, err)
			}
			definitions, err := ExtractDefinitions(context.Background(), result,
				DefinitionOptions{Repository: repository.Name})
			if err != nil {
				t.Fatalf("ExtractDefinitions() error = %v", err)
			}
			keyed, err := AssignStableKeys(context.Background(), definitions)
			if err != nil {
				t.Fatalf("AssignStableKeys() error = %v", err)
			}
			for _, definition := range keyed {
				own[definition.PackagePath+"."+definition.QualifiedName] = definition
			}
		}
	}

	for _, name := range []string{"consumer-a", "consumer-b"} {
		facts := loadFixtureConsumer(t, name)
		if len(facts.cross) == 0 {
			t.Fatalf("fixture %s produced no cross-repository edge", name)
		}
		for target, reference := range facts.cross {
			declaration, declared := own[reference.TargetPackagePath+"."+target]
			if !declared {
				t.Fatalf("no provider declares %q", reference.TargetPackagePath+"."+target)
			}
			if reference.TargetStableKey != declaration.StableKey {
				t.Fatalf("target %q key differs from its declaration:\nuse: %s\nown: %s",
					target, reference.TargetCanonicalIdentity, declaration.CanonicalIdentity)
			}
		}
	}
}
