package goloader

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/Luqueee/ladygraph/internal/goworkspace"
	"github.com/Luqueee/ladygraph/internal/testsupport"
	"github.com/Luqueee/ladygraph/internal/workspace"
)

var goNegativeRoot = filepath.Join("..", "..", "testdata", "go", "cross-repository-negative")

// negativeRepositories returns the fixture repositories.
//
// `twin-b` declares the same module path as `twin-a`: the workspace can only
// use one of them, while the registry must keep seeing both.
func negativeRepositories(t *testing.T) (workspaceRepositories, registryRepositories []workspace.Repository) {
	t.Helper()
	root, err := filepath.Abs(goNegativeRoot)
	if err != nil {
		t.Fatalf("resolve fixture root: %v", err)
	}
	repository := func(name string) workspace.Repository {
		path := filepath.Join(root, name)
		return workspace.Repository{Name: name, Path: path, RealPath: path}
	}
	workspaceRepositories = []workspace.Repository{
		repository("decoy"), repository("mirror"), repository("twin-a"), repository("consumer"),
	}
	registryRepositories = append(append([]workspace.Repository(nil), workspaceRepositories...),
		repository("twin-b"))
	return workspaceRepositories, registryRepositories
}

type negativeFacts struct {
	uses       []Use
	references []Reference
	cross      []CrossRepositoryReference
	unresolved []UnresolvedReference
	conflicts  []goworkspace.Conflict
}

func loadNegativeFixture(t *testing.T) negativeFacts {
	t.Helper()
	workspaceRepositories, registryRepositories := negativeRepositories(t)

	plan, err := goworkspace.BuildPlan(context.Background(), workspaceRepositories, goworkspace.Options{})
	if err != nil {
		t.Fatalf("BuildPlan() error = %v", err)
	}
	workFile := filepath.Join(testsupport.TempDir(t), "state", "go.work")
	if _, err := goworkspace.Write(context.Background(), workFile, plan, workspaceRepositories); err != nil {
		t.Fatalf("Write() error = %v", err)
	}

	consumer := workspaceRepositories[len(workspaceRepositories)-1]
	result, err := Load(context.Background(), Options{Directory: consumer.Path, WorkFile: workFile})
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if len(result.Errors) != 0 {
		t.Fatalf("negative fixture must still compile: %#v", result.Errors)
	}
	uses, err := ExtractUses(context.Background(), result, UseOptions{Repository: consumer.Name})
	if err != nil {
		t.Fatalf("ExtractUses() error = %v", err)
	}
	references, err := ClassifyReferences(context.Background(), result, uses)
	if err != nil {
		t.Fatalf("ClassifyReferences() error = %v", err)
	}
	registry, err := NewModuleRegistry(context.Background(), registryRepositories)
	if err != nil {
		t.Fatalf("NewModuleRegistry() error = %v", err)
	}
	cross, err := ResolveCrossRepository(context.Background(), uses, registry,
		CrossRepositoryOptions{ConsumerRepository: consumer.Name})
	if err != nil {
		t.Fatalf("ResolveCrossRepository() error = %v", err)
	}
	unresolved, err := ClassifyUnresolved(context.Background(), result, cross, UnresolvedOptions{
		Repository:         consumer.Name,
		WorkspaceConflicts: plan.Conflicts,
	})
	if err != nil {
		t.Fatalf("ClassifyUnresolved() error = %v", err)
	}
	return negativeFacts{
		uses:       uses,
		references: references,
		cross:      cross,
		unresolved: unresolved,
		conflicts:  plan.Conflicts,
	}
}

func TestGoNegativeFixtureKeepsHomonymsApart(t *testing.T) {
	facts := loadNegativeFixture(t)

	for _, use := range facts.uses {
		switch use.TargetQualifiedName {
		case "Compute", "Shape.Area", "Shape", "Shape.Width":
		default:
			continue
		}
		if use.TargetPackagePath == "example.com/ladygraph-fixture/mirror/api" {
			t.Fatalf("a homonym of an unimported package was linked: %#v", use)
		}
	}

	// The local Compute and the provider Compute share a name and nothing else.
	locals := 0
	providers := 0
	for _, use := range facts.uses {
		if use.TargetQualifiedName != "Compute" {
			continue
		}
		switch use.TargetPackagePath {
		case "example.com/ladygraph-fixture/consumer-negative":
			locals++
		case "example.com/ladygraph-fixture/decoy/api", "example.com/ladygraph-fixture/twin/api":
			providers++
		default:
			t.Fatalf("unexpected Compute target: %#v", use)
		}
	}
	if locals != 2 {
		t.Fatalf("local Compute uses = %d, want the direct call and the callback", locals)
	}
	if providers != 2 {
		t.Fatalf("provider Compute uses = %d, want decoy and twin", providers)
	}

	// The callback passed to the provider is the local function, not its
	// homonym in the provider package.
	callbacks := 0
	for _, reference := range facts.references {
		if reference.Kind != ReferencePassesAsCallback {
			continue
		}
		callbacks++
		if reference.TargetPackagePath != "example.com/ladygraph-fixture/consumer-negative" {
			t.Fatalf("callback resolved to %q", reference.TargetPackagePath)
		}
	}
	if callbacks != 1 {
		t.Fatalf("callbacks = %d, want exactly one", callbacks)
	}
}

func TestGoNegativeFixtureKeepsReceiversApart(t *testing.T) {
	facts := loadNegativeFixture(t)

	byPackage := make(map[string]int)
	for _, use := range facts.uses {
		if use.TargetQualifiedName != "Shape.Area" {
			continue
		}
		byPackage[use.TargetPackagePath]++
	}
	if byPackage["example.com/ladygraph-fixture/consumer-negative"] != 1 {
		t.Fatalf("local Area uses = %#v", byPackage)
	}
	if byPackage["example.com/ladygraph-fixture/decoy/api"] != 1 {
		t.Fatalf("provider Area uses = %#v", byPackage)
	}
	if len(byPackage) != 2 {
		t.Fatalf("Area was attributed to a third receiver: %#v", byPackage)
	}
}

func TestGoNegativeFixtureRefusesAmbiguousAndConflictingFacts(t *testing.T) {
	facts := loadNegativeFixture(t)

	// The duplicated module keeps two candidates and produces no identity.
	ambiguous := 0
	for _, reference := range facts.cross {
		if reference.TargetModulePath != "example.com/ladygraph-fixture/twin" {
			continue
		}
		ambiguous++
		if reference.Status != AmbiguousModuleProvider {
			t.Fatalf("twin reference status = %q", reference.Status)
		}
		if reference.TargetStableKey != "" || len(reference.Providers) != 2 {
			t.Fatalf("twin reference = %#v", reference)
		}
	}
	if ambiguous == 0 {
		t.Fatalf("the duplicated module produced no reference: %#v", facts.cross)
	}

	// The decoy module resolves normally: ambiguity is not contagious.
	for _, reference := range facts.cross {
		if reference.TargetModulePath != "example.com/ladygraph-fixture/decoy" {
			continue
		}
		if reference.Status != CrossRepositoryResolved || reference.Provider.Repository != "decoy" {
			t.Fatalf("decoy reference = %#v", reference)
		}
	}

	reasons := reasonsOf(facts.unresolved)
	if len(reasons[UnresolvedAmbiguousModuleProvider]) == 0 {
		t.Fatalf("ambiguity was not classified: %#v", facts.unresolved)
	}
	if len(reasons[UnresolvedReplaceConflict]) != 1 {
		t.Fatalf("replace conflict = %#v", reasons[UnresolvedReplaceConflict])
	}
	if reasons[UnresolvedReplaceConflict][0].RequestedModulePath != "example.com/ladygraph-fixture/pinned" {
		t.Fatalf("replace conflict subject = %#v", reasons[UnresolvedReplaceConflict][0])
	}
	if len(reasons[UnresolvedTypecheckFailed]) != 0 || len(reasons[UnresolvedPackageNotLoaded]) != 0 {
		t.Fatalf("the fixture must compile cleanly: %#v", facts.unresolved)
	}
}

// TestGoNegativeFixtureRefusesEdgesOnAGuessedReplacement checks the rule the
// workspace override makes necessary: go needs a single replacement to load
// at all, so Ladygraph emits one, and every edge into that module is refused.
func TestGoNegativeFixtureRefusesEdgesOnAGuessedReplacement(t *testing.T) {
	facts := loadNegativeFixture(t)
	if len(facts.uses) == 0 {
		t.Fatalf("fixture produced no uses")
	}

	registryRepositories := func() []workspace.Repository {
		_, registry := negativeRepositories(t)
		return registry
	}()
	registry, err := NewModuleRegistry(context.Background(), registryRepositories)
	if err != nil {
		t.Fatalf("NewModuleRegistry() error = %v", err)
	}

	// The decoy module is declared conflicting, as if its replacement had been
	// guessed by the workspace.
	references, err := ResolveCrossRepository(context.Background(), facts.uses, registry,
		CrossRepositoryOptions{
			ConsumerRepository: "consumer",
			ConflictingModules: []string{"example.com/ladygraph-fixture/decoy"},
		})
	if err != nil {
		t.Fatalf("ResolveCrossRepository() error = %v", err)
	}

	guessed := 0
	for _, reference := range references {
		if reference.TargetModulePath != "example.com/ladygraph-fixture/decoy" {
			continue
		}
		guessed++
		if reference.Status != ReplaceConflictTarget {
			t.Fatalf("reference %q status = %q", reference.TargetQualifiedName, reference.Status)
		}
		if reference.TargetStableKey != "" {
			t.Fatalf("an edge was built on a guessed replacement: %#v", reference)
		}
	}
	if guessed == 0 {
		t.Fatalf("no reference targeted the conflicting module")
	}

	unresolved, err := ClassifyUnresolved(context.Background(), Result{}, references,
		UnresolvedOptions{Repository: "consumer"})
	if err != nil {
		t.Fatalf("ClassifyUnresolved() error = %v", err)
	}
	if len(reasonsOf(unresolved)[UnresolvedReplaceConflict]) != guessed {
		t.Fatalf("replace conflicts = %#v", unresolved)
	}
}
