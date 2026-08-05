package goloader

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/Luqueee/luque/internal/goworkspace"
	"github.com/Luqueee/luque/internal/workspace"
)

const crossProviderSource = `package api

// Answer is an exported constant.
const Answer = 42

// Shape carries an exported method.
type Shape struct {
	// Width is exported.
	Width int
}

// Area is an exported method.
func (shape Shape) Area() int { return shape.Width }

// Compute is an exported function.
func Compute(input int) int { return input + Answer }
`

const crossConsumerSource = `package main

import (
	"example.com/provider/api"
)

func main() {
	shape := api.Shape{Width: 1}
	_ = api.Compute(shape.Area() + api.Answer)
}
`

type crossFixture struct {
	root         string
	provider     string
	consumer     string
	repositories []workspace.Repository
	workFile     string
}

func newCrossFixture(t *testing.T, extraRepositories ...string) crossFixture {
	t.Helper()
	root := t.TempDir()
	provider := filepath.Join(root, "provider")
	consumer := filepath.Join(root, "consumer")
	writeFiles(t, provider, map[string]string{
		"go.mod":     "module example.com/provider\n\ngo 1.24\n",
		"api/api.go": crossProviderSource,
	})
	writeFiles(t, consumer, map[string]string{
		"go.mod":  "module example.com/consumer\n\ngo 1.24\n",
		"main.go": crossConsumerSource,
	})

	repositories := []workspace.Repository{
		{Name: "provider", Path: provider, RealPath: provider},
		{Name: "consumer", Path: consumer, RealPath: consumer},
	}
	for _, name := range extraRepositories {
		duplicate := filepath.Join(root, name)
		writeFiles(t, duplicate, map[string]string{
			"go.mod":     "module example.com/provider\n\ngo 1.24\n",
			"api/api.go": crossProviderSource,
		})
		repositories = append(repositories, workspace.Repository{
			Name: name, Path: duplicate, RealPath: duplicate,
		})
	}

	// Only the two canonical repositories enter the workspace: a duplicate
	// module path cannot be used twice by go itself.
	plan, err := goworkspace.BuildPlan(context.Background(), repositories[:2], goworkspace.Options{})
	if err != nil {
		t.Fatalf("BuildPlan() error = %v", err)
	}
	workFile := filepath.Join(root, "state", "go.work")
	if _, err := goworkspace.Write(context.Background(), workFile, plan, repositories[:2]); err != nil {
		t.Fatalf("Write() error = %v", err)
	}
	return crossFixture{
		root:         root,
		provider:     provider,
		consumer:     consumer,
		repositories: repositories,
		workFile:     workFile,
	}
}

func (fixture crossFixture) resolve(t *testing.T) []CrossRepositoryReference {
	t.Helper()
	result, err := Load(context.Background(), Options{
		Directory: fixture.consumer,
		WorkFile:  fixture.workFile,
	})
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if len(result.Errors) != 0 {
		t.Fatalf("errors = %#v", result.Errors)
	}
	uses, err := ExtractUses(context.Background(), result, UseOptions{Repository: "consumer"})
	if err != nil {
		t.Fatalf("ExtractUses() error = %v", err)
	}
	registry, err := NewModuleRegistry(context.Background(), fixture.repositories)
	if err != nil {
		t.Fatalf("NewModuleRegistry() error = %v", err)
	}
	references, err := ResolveCrossRepository(context.Background(), uses, registry,
		CrossRepositoryOptions{ConsumerRepository: "consumer"})
	if err != nil {
		t.Fatalf("ResolveCrossRepository() error = %v", err)
	}
	return references
}

func TestResolveCrossRepositoryAttributesTargetsToTheirProvider(t *testing.T) {
	fixture := newCrossFixture(t)
	references := fixture.resolve(t)
	if len(references) == 0 {
		t.Fatalf("no cross-repository references were resolved")
	}

	byTarget := make(map[string]CrossRepositoryReference)
	for _, reference := range references {
		if reference.Status != CrossRepositoryResolved {
			t.Fatalf("reference %q status = %q", reference.TargetQualifiedName, reference.Status)
		}
		byTarget[reference.TargetQualifiedName] = reference
	}

	compute, exists := byTarget["Compute"]
	if !exists {
		t.Fatalf("targets = %#v", byTarget)
	}
	if compute.Provider.Repository != "provider" ||
		compute.Provider.ModulePath != "example.com/provider" {
		t.Fatalf("Compute provider = %#v", compute.Provider)
	}
	if compute.ConsumerRepository != "consumer" {
		t.Fatalf("consumer repository = %q", compute.ConsumerRepository)
	}
	if compute.TargetObjectPath != "Compute" || compute.TargetStableKey == "" {
		t.Fatalf("Compute identity = %#v", compute)
	}

	// The key must be the one the provider repository would assign to its own
	// declaration, otherwise the cross-repository edge would dangle.
	providerResult, err := Load(context.Background(), Options{Directory: fixture.provider})
	if err != nil {
		t.Fatalf("Load(provider) error = %v", err)
	}
	definitions, err := ExtractDefinitions(context.Background(), providerResult,
		DefinitionOptions{Repository: "provider"})
	if err != nil {
		t.Fatalf("ExtractDefinitions() error = %v", err)
	}
	keyed, err := AssignStableKeys(context.Background(), definitions)
	if err != nil {
		t.Fatalf("AssignStableKeys() error = %v", err)
	}
	own := make(map[string]KeyedDefinition, len(keyed))
	for _, definition := range keyed {
		own[definition.QualifiedName] = definition
	}
	for _, name := range []string{"Compute", "Answer", "Shape", "Shape.Area", "Shape.Width"} {
		reference, referenced := byTarget[name]
		if !referenced {
			continue
		}
		if own[name].StableKey == "" {
			t.Fatalf("provider did not define %q", name)
		}
		if reference.TargetStableKey != own[name].StableKey {
			t.Fatalf("target %q key differs from the provider definition:\nuse: %s\nown: %s",
				name, reference.TargetCanonicalIdentity, own[name].CanonicalIdentity)
		}
	}
}

func TestResolveCrossRepositoryReportsAmbiguityAndMissingProviders(t *testing.T) {
	ambiguous := newCrossFixture(t, "duplicate")
	for _, reference := range ambiguous.resolve(t) {
		if reference.Status != AmbiguousModuleProvider {
			t.Fatalf("reference %q status = %q, want ambiguity", reference.TargetQualifiedName, reference.Status)
		}
		if len(reference.Providers) != 2 {
			t.Fatalf("candidates = %#v", reference.Providers)
		}
		if reference.TargetStableKey != "" {
			t.Fatalf("an ambiguous provider must not produce an identity")
		}
	}

	// Without the provider repository registered, nothing can be attributed.
	fixture := newCrossFixture(t)
	result, err := Load(context.Background(), Options{
		Directory: fixture.consumer,
		WorkFile:  fixture.workFile,
	})
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	uses, err := ExtractUses(context.Background(), result, UseOptions{Repository: "consumer"})
	if err != nil {
		t.Fatalf("ExtractUses() error = %v", err)
	}
	registry, err := NewModuleRegistry(context.Background(), fixture.repositories[1:2])
	if err != nil {
		t.Fatalf("NewModuleRegistry() error = %v", err)
	}
	references, err := ResolveCrossRepository(context.Background(), uses, registry,
		CrossRepositoryOptions{ConsumerRepository: "consumer"})
	if err != nil {
		t.Fatalf("ResolveCrossRepository() error = %v", err)
	}
	if len(references) == 0 {
		t.Fatalf("expected unattributed references")
	}
	for _, reference := range references {
		if reference.Status != ModuleProviderNotFound {
			t.Fatalf("reference %q status = %q", reference.TargetQualifiedName, reference.Status)
		}
	}
}

func TestResolveCrossRepositoryExcludesIntraModuleUses(t *testing.T) {
	fixture := newCrossFixture(t)
	result, err := Load(context.Background(), Options{Directory: fixture.provider})
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	uses, err := ExtractUses(context.Background(), result, UseOptions{Repository: "provider"})
	if err != nil {
		t.Fatalf("ExtractUses() error = %v", err)
	}
	registry, err := NewModuleRegistry(context.Background(), fixture.repositories)
	if err != nil {
		t.Fatalf("NewModuleRegistry() error = %v", err)
	}
	references, err := ResolveCrossRepository(context.Background(), uses, registry,
		CrossRepositoryOptions{ConsumerRepository: "provider"})
	if err != nil {
		t.Fatalf("ResolveCrossRepository() error = %v", err)
	}
	if len(references) != 0 {
		t.Fatalf("intra-module uses were reported as cross-repository: %#v", references)
	}
}

// TestResolveCrossRepositoryFollowsGenericOrigins proves that a member of an
// instantiated generic keeps the identity of the declared member: the encoder
// indexes declarations, so the instance must fall back to its origin.
func TestResolveCrossRepositoryFollowsGenericOrigins(t *testing.T) {
	root := t.TempDir()
	provider := filepath.Join(root, "provider")
	consumer := filepath.Join(root, "consumer")
	writeFiles(t, provider, map[string]string{
		"go.mod": "module example.com/provider\n\ngo 1.24\n",
		"api/api.go": `package api

// Box is a generic container.
type Box[T any] struct {
	// Value is the stored item.
	Value T
}

// Unwrap returns the stored item.
func (box Box[T]) Unwrap() T { return box.Value }
`,
	})
	writeFiles(t, consumer, map[string]string{
		"go.mod": "module example.com/consumer\n\ngo 1.24\n",
		"main.go": `package main

import "example.com/provider/api"

func main() {
	box := api.Box[int]{Value: 1}
	_ = box.Unwrap()
}
`,
	})
	repositories := []workspace.Repository{
		{Name: "provider", Path: provider, RealPath: provider},
		{Name: "consumer", Path: consumer, RealPath: consumer},
	}
	plan, err := goworkspace.BuildPlan(context.Background(), repositories, goworkspace.Options{})
	if err != nil {
		t.Fatalf("BuildPlan() error = %v", err)
	}
	workFile := filepath.Join(root, "state", "go.work")
	if _, err := goworkspace.Write(context.Background(), workFile, plan, repositories); err != nil {
		t.Fatalf("Write() error = %v", err)
	}
	result, err := Load(context.Background(), Options{Directory: consumer, WorkFile: workFile})
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	uses, err := ExtractUses(context.Background(), result, UseOptions{Repository: "consumer"})
	if err != nil {
		t.Fatalf("ExtractUses() error = %v", err)
	}
	registry, err := NewModuleRegistry(context.Background(), repositories)
	if err != nil {
		t.Fatalf("NewModuleRegistry() error = %v", err)
	}
	references, err := ResolveCrossRepository(context.Background(), uses, registry,
		CrossRepositoryOptions{ConsumerRepository: "consumer"})
	if err != nil {
		t.Fatalf("ResolveCrossRepository() error = %v", err)
	}

	paths := make(map[string]string, len(references))
	for _, reference := range references {
		if reference.Status != CrossRepositoryResolved {
			t.Fatalf("reference %q status = %q", reference.TargetQualifiedName, reference.Status)
		}
		paths[reference.TargetQualifiedName] = reference.TargetObjectPath
	}
	if paths["Box.Unwrap"] == "" || paths["Box.Value"] == "" {
		t.Fatalf("generic members were not addressed: %#v", paths)
	}

	// The identity must match what the provider assigns to its declaration.
	providerResult, err := Load(context.Background(), Options{Directory: provider})
	if err != nil {
		t.Fatalf("Load(provider) error = %v", err)
	}
	definitions, err := ExtractDefinitions(context.Background(), providerResult,
		DefinitionOptions{Repository: "provider"})
	if err != nil {
		t.Fatalf("ExtractDefinitions() error = %v", err)
	}
	keyed, err := AssignStableKeys(context.Background(), definitions)
	if err != nil {
		t.Fatalf("AssignStableKeys() error = %v", err)
	}
	own := make(map[string]KeyedDefinition, len(keyed))
	for _, definition := range keyed {
		own[definition.QualifiedName] = definition
	}
	for _, reference := range references {
		declaration, declared := own[reference.TargetQualifiedName]
		if !declared {
			t.Fatalf("provider does not declare %q", reference.TargetQualifiedName)
		}
		if reference.TargetStableKey != declaration.StableKey {
			t.Fatalf("generic target %q key differs:\nuse: %s\nown: %s",
				reference.TargetQualifiedName,
				reference.TargetCanonicalIdentity, declaration.CanonicalIdentity)
		}
	}
}
