package facts

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/Luqueee/kivgraph/internal/goloader"
	"github.com/Luqueee/kivgraph/internal/goworkspace"
	"github.com/Luqueee/kivgraph/internal/workspace"
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
	return normalizeRepositories(t, fixtureRepositories(t), repositoryName)
}

// normalizeRepositories runs the whole Go pipeline over an arbitrary set of
// repositories and returns the canonical facts of one of them. It is
// normalizeFixture's body, extracted so a test can point the same pipeline at a
// working copy it is allowed to edit.
func normalizeRepositories(t *testing.T, repositories []workspace.Repository, repositoryName string) (Set, GoReport) {
	t.Helper()
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
		packageDependencies, err := goloader.ResolvePackageDependencies(context.Background(), uses)
		if err != nil {
			t.Fatalf("ResolvePackageDependencies() error = %v", err)
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
			Repository:          repository,
			Definitions:         keyed,
			References:          references,
			CrossRepository:     cross,
			PackageDependencies: packageDependencies,
			Unresolved:          unresolved,
		})
		if err != nil {
			t.Fatalf("NormalizeGo() error = %v", err)
		}
		merged.Merge(set)
		total.EdgesWithoutSource += report.EdgesWithoutSource
		total.EdgesWithoutTarget += report.EdgesWithoutTarget
		total.UnresolvedWithoutFile += report.UnresolvedWithoutFile
		total.FactsOutsideRepository += report.FactsOutsideRepository
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
		set.Packages[0].Name != "example.com/kivgraph-fixture/shared/api" ||
		set.Packages[0].Container != "example.com/kivgraph-fixture/shared" {
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

// typeRelationsFixtureRoot is the single self-contained module used by the
// IMPLEMENTS, EMBEDS and OVERRIDES tests: it needs no synthetic go.work.
var typeRelationsFixtureRoot = filepath.Join("..", "..", "testdata", "go", "type-relations")

// typeRelationsInput assembles the loader facts of the type-relations fixture,
// which is the input a caller hands to NormalizeGo.
func typeRelationsInput(t *testing.T) GoInput {
	t.Helper()
	root, err := filepath.Abs(typeRelationsFixtureRoot)
	if err != nil {
		t.Fatalf("resolve fixture: %v", err)
	}
	repository := workspace.Repository{Name: "type-relations", Path: root, RealPath: root}

	result, err := goloader.Load(context.Background(), goloader.Options{Directory: root})
	if err != nil {
		t.Fatalf("Load() error = %v", err)
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
	relations, err := goloader.ResolveTypeRelations(context.Background(), result,
		goloader.TypeRelationOptions{Repository: repository.Name})
	if err != nil {
		t.Fatalf("ResolveTypeRelations() error = %v", err)
	}
	uses, err := goloader.ExtractUses(context.Background(), result,
		goloader.UseOptions{Repository: repository.Name})
	if err != nil {
		t.Fatalf("ExtractUses() error = %v", err)
	}
	packageDependencies, err := goloader.ResolvePackageDependencies(context.Background(), uses)
	if err != nil {
		t.Fatalf("ResolvePackageDependencies() error = %v", err)
	}

	return GoInput{
		Repository:          repository,
		Definitions:         keyed,
		TypeRelations:       relations,
		PackageDependencies: packageDependencies,
	}
}

// normalizeTypeRelationsFixture indexes the type-relations fixture end to
// end, including its IMPLEMENTS, EMBEDS and OVERRIDES structural relations.
func normalizeTypeRelationsFixture(t *testing.T) (Set, GoReport) {
	t.Helper()
	set, report, err := NormalizeGo(context.Background(), typeRelationsInput(t))
	if err != nil {
		t.Fatalf("NormalizeGo() error = %v", err)
	}
	return set, report
}

// TestNormalizeGoRefusesFactsFromOutsideTheRepository defends the invariant
// that a fact is evidence for the repository holding its file and for no
// other. Asking the loader for compiled files means a package built with cgo
// or from generated sources reports positions inside the build cache; a graph
// that accepted them would key a File by an absolute path, naming the machine
// that produced it, and would answer a blast radius with a cache entry.
func TestNormalizeGoRefusesFactsFromOutsideTheRepository(t *testing.T) {
	input := typeRelationsInput(t)
	clean, cleanReport, err := NormalizeGo(context.Background(), input)
	if err != nil {
		t.Fatalf("NormalizeGo() error = %v", err)
	}
	if len(input.Definitions) == 0 || len(input.TypeRelations) == 0 {
		t.Fatalf("fixture carries no facts to displace")
	}

	// The shape observed in a published generation: $GOCACHE/<xx>/<hash>-d.
	cache := filepath.Join(t.TempDir(), "go-build", "27",
		"27bf728258fd9290eefce3c1972e594f6c46a1b2c552e6caf61374702bf0ecc3-d")
	declaration := input.Definitions[0]
	declaration.FileName = cache
	relation := input.TypeRelations[0]
	relation.FileName = cache

	poisoned := input
	poisoned.Definitions = append(append([]goloader.KeyedDefinition{}, input.Definitions...), declaration)
	poisoned.TypeRelations = append(append([]goloader.TypeRelation{}, input.TypeRelations...), relation)

	got, report, err := NormalizeGo(context.Background(), poisoned)
	if err != nil {
		t.Fatalf("NormalizeGo() error = %v", err)
	}

	// The graph is the one the repository can account for, unchanged.
	if diff := diffSets(clean, got); diff != "" {
		t.Fatalf("out-of-repository facts changed the graph:\n%s", diff)
	}
	for _, file := range got.Files {
		if filepath.IsAbs(file.Path) || strings.Contains(file.Path, "go-build") {
			t.Fatalf("file path is not repository-relative: %q", file.Path)
		}
	}

	// The loss is counted and retained with its reason, never hidden.
	if got, want := report.FactsOutsideRepository, 2; got != want {
		t.Fatalf("FactsOutsideRepository = %d, want %d", got, want)
	}
	if cleanReport.FactsOutsideRepository != 0 {
		t.Fatalf("clean pass reported %d facts outside the repository",
			cleanReport.FactsOutsideRepository)
	}
	var retained []UnresolvedReference
	for _, entry := range got.Unresolved {
		if entry.Reason == string(goloader.UnresolvedDeclarationOutsideRepository) {
			retained = append(retained, entry)
		}
	}
	if len(retained) != 1 {
		t.Fatalf("retained %d out-of-repository declarations, want 1: %#v", len(retained), retained)
	}
	if retained[0].Detail != cache {
		t.Fatalf("Detail = %q, want the observed path %q", retained[0].Detail, cache)
	}
	if retained[0].FileKey != "" {
		t.Fatalf("FileKey = %q, want none: the file is not in this repository", retained[0].FileKey)
	}
	if retained[0].RequestedSymbol != declaration.QualifiedName {
		t.Fatalf("RequestedSymbol = %q, want %q", retained[0].RequestedSymbol, declaration.QualifiedName)
	}
}

// diffSets reports the first structural difference between two fact sets,
// ignoring the unresolved rows a caller compares on their own.
func diffSets(want, got Set) string {
	if len(want.Files) != len(got.Files) {
		return fmt.Sprintf("files: %d != %d", len(want.Files), len(got.Files))
	}
	if len(want.Symbols) != len(got.Symbols) {
		return fmt.Sprintf("symbols: %d != %d", len(want.Symbols), len(got.Symbols))
	}
	if len(want.Packages) != len(got.Packages) {
		return fmt.Sprintf("packages: %d != %d", len(want.Packages), len(got.Packages))
	}
	if !reflect.DeepEqual(want.Evidence, got.Evidence) {
		return fmt.Sprintf("evidence:\n want %#v\n got  %#v", want.Evidence, got.Evidence)
	}
	if !reflect.DeepEqual(want.Edges, got.Edges) {
		return fmt.Sprintf("edges:\n want %#v\n got  %#v", want.Edges, got.Edges)
	}
	return ""
}

func TestNormalizeGoEmitsTypeRelationEdges(t *testing.T) {
	set, report := normalizeTypeRelationsFixture(t)
	if err := set.Validate(); err != nil {
		t.Fatalf("Validate() error = %v", err)
	}

	byQualifiedName := make(map[string]Symbol, len(set.Symbols))
	for _, symbol := range set.Symbols {
		byQualifiedName[symbol.QualifiedName] = symbol
	}
	findEdge := func(kind EdgeKind, source, target string) (Edge, bool) {
		sourceSymbol, hasSource := byQualifiedName[source]
		targetSymbol, hasTarget := byQualifiedName[target]
		if !hasSource || !hasTarget {
			return Edge{}, false
		}
		for _, candidate := range set.Edges {
			if candidate.Kind == kind && candidate.SourceKey == sourceSymbol.Key && candidate.TargetKey == targetSymbol.Key {
				return candidate, true
			}
		}
		return Edge{}, false
	}

	implements, found := findEdge(Implements, "Circle", "Shape")
	if !found {
		t.Fatalf("Circle IMPLEMENTS Shape edge missing: %#v", set.Edges)
	}
	if implements.Confidence != ExactTypechecked || implements.Provenance != GoTypesUse {
		t.Fatalf("IMPLEMENTS edge = %#v", implements)
	}
	if implements.EvidenceKey == "" {
		t.Fatalf("IMPLEMENTS edge carries no evidence: %#v", implements)
	}
	if _, found := findEdge(Implements, "Square", "Shape"); !found {
		t.Fatalf("Square IMPLEMENTS Shape edge missing: %#v", set.Edges)
	}
	if _, found := findEdge(Implements, "Triangle", "Shape"); found {
		t.Fatalf("Triangle must not implement Shape")
	}

	embeds, found := findEdge(Embeds, "Circle", "Base")
	if !found {
		t.Fatalf("Circle EMBEDS Base edge missing: %#v", set.Edges)
	}
	if embeds.Confidence != ExactTypechecked || embeds.Provenance != GoTypesUse {
		t.Fatalf("EMBEDS edge = %#v", embeds)
	}
	if _, found := findEdge(Embeds, "Square", "Base"); !found {
		t.Fatalf("Square EMBEDS Base edge missing: %#v", set.Edges)
	}
	if _, found := findEdge(Embeds, "Solid", "Shape"); !found {
		t.Fatalf("Solid EMBEDS Shape edge missing: %#v", set.Edges)
	}

	overrides, found := findEdge(Overrides, "Circle.ID", "Base.ID")
	if !found {
		t.Fatalf("Circle.ID OVERRIDES Base.ID edge missing: %#v", set.Edges)
	}
	if overrides.Confidence != ExactTypechecked || overrides.Provenance != GoTypesUse {
		t.Fatalf("OVERRIDES edge = %#v", overrides)
	}

	if symbol, exists := byQualifiedName["Anything"]; exists {
		for _, candidate := range set.Edges {
			if candidate.Kind == Implements && candidate.TargetKey == symbol.Key {
				t.Fatalf("the empty interface must not be an IMPLEMENTS target: %#v", candidate)
			}
		}
	}

	// Circle.String satisfies fmt.Stringer, which has no repository in this
	// workspace: its target cannot be keyed, so the relation must be
	// dropped and counted rather than produce a dangling edge.
	if report.EdgesWithoutTarget == 0 {
		t.Fatalf("expected fmt.Stringer's IMPLEMENTS relation to be dropped and counted: report = %#v", report)
	}
}

// TestNormalizeGoResolvesTypeRelationTargetsAcrossRepositories proves the
// wiring bullet 2 requires: a relation whose target lives in another
// repository resolves through the same CrossRepository identity a plain
// reference to that symbol would already have produced, keyed by the
// target alone, never by the occurrence that reached it.
func TestNormalizeGoResolvesTypeRelationTargetsAcrossRepositories(t *testing.T) {
	repositoryPath := t.TempDir()
	repository := workspace.Repository{Name: "consumer", Path: repositoryPath}
	fileName := filepath.Join(repositoryPath, "geometry.go")

	definitions := []goloader.KeyedDefinition{{
		Definition: goloader.Definition{
			Repository:    repository.Name,
			ModulePath:    "example.com/consumer",
			PackagePath:   "example.com/consumer",
			PackageName:   "geometry",
			FileName:      fileName,
			Name:          "Circle",
			QualifiedName: "Circle",
			Kind:          goloader.KindType,
			Exported:      true,
		},
		ObjectPath:        "Circle",
		CanonicalIdentity: "go:v1:consumer:example.com/consumer example.com/consumer:Circle:type:none",
		StableKey:         "stable-circle",
	}}

	relation := goloader.TypeRelation{
		Use: goloader.Use{
			Repository:          repository.Name,
			ModulePath:          "example.com/consumer",
			PackagePath:         "example.com/consumer",
			FileName:            fileName,
			Name:                "Shape",
			SourceQualifiedName: "Circle",
			SourceKind:          goloader.KindType,
			TargetModulePath:    "example.com/provider",
			TargetPackagePath:   "example.com/provider/api",
			TargetQualifiedName: "Shape",
			TargetKind:          goloader.KindType,
			Offset:              10,
			EndOffset:           16,
			StartLine:           1,
			StartColumn:         1,
		},
		Kind: goloader.RelationImplements,
	}

	cross := []goloader.CrossRepositoryReference{{
		Use:                     relation.Use,
		Status:                  goloader.CrossRepositoryResolved,
		TargetStableKey:         "stable-shape",
		TargetCanonicalIdentity: "go:v1:provider:example.com/provider example.com/provider/api:Shape:type:none",
	}}

	set, report, err := NormalizeGo(context.Background(), GoInput{
		Repository:      repository,
		Definitions:     definitions,
		TypeRelations:   []goloader.TypeRelation{relation},
		CrossRepository: cross,
	})
	if err != nil {
		t.Fatalf("NormalizeGo() error = %v", err)
	}
	if report.EdgesWithoutTarget != 0 {
		t.Fatalf("the cross-repository target must resolve: report = %#v", report)
	}

	found := false
	for _, edge := range set.Edges {
		if edge.Kind == Implements && edge.TargetKey == "stable-shape" {
			found = true
			if edge.Confidence != ExactTypechecked || edge.Provenance != GoObjectPath {
				t.Fatalf("cross-repository IMPLEMENTS edge = %#v", edge)
			}
		}
	}
	if !found {
		t.Fatalf("expected an IMPLEMENTS edge to the cross-repository target: %#v", set.Edges)
	}
}

// TestNormalizeGoEmitsPackageDependencyEdgesAcrossRepositories proves
// acceptance criteria (a) and (c) of the Go package-dependency wiring:
// consumer-a depends on shared/api across repositories, so it gets both
// PACKAGE_DEPENDS_ON and, because shared and consumer-a are different Go
// modules, MODULE_DEPENDS_ON too. The target key of both edges must be byte
// identical to the key the provider repository assigns to its own package
// when it normalises itself — the same parity already proven for symbols in
// TestNormalizeGoKeepsCrossRepositoryIdentity.
func TestNormalizeGoEmitsPackageDependencyEdgesAcrossRepositories(t *testing.T) {
	provider, _ := normalizeFixture(t, "shared-library")
	consumer, report := normalizeFixture(t, "consumer-a")

	if len(provider.Packages) != 1 {
		t.Fatalf("provider packages = %#v, want exactly the shared/api package", provider.Packages)
	}
	providerPackage := provider.Packages[0]
	if providerPackage.Name != "example.com/kivgraph-fixture/shared/api" {
		t.Fatalf("provider package = %#v", providerPackage)
	}
	if len(consumer.Packages) != 1 {
		t.Fatalf("consumer packages = %#v, want exactly the consumer-a package", consumer.Packages)
	}
	consumerPackage := consumer.Packages[0]

	var packageDepends, moduleDepends []Edge
	for _, edge := range consumer.Edges {
		switch edge.Kind {
		case PackageDependsOn:
			packageDepends = append(packageDepends, edge)
		case ModuleDependsOn:
			moduleDepends = append(moduleDepends, edge)
		}
	}
	if len(packageDepends) != 1 {
		t.Fatalf("PACKAGE_DEPENDS_ON edges = %#v, want exactly one: consumer-a's single package "+
			"uses many symbols of shared/api, but they form a single dependency", packageDepends)
	}
	if len(moduleDepends) != 1 {
		t.Fatalf("MODULE_DEPENDS_ON edges = %#v, want exactly one: shared and consumer-a are "+
			"different Go modules", moduleDepends)
	}

	dependency := packageDepends[0]
	// The parity check covers both ends: the source key this pass derived
	// must match the key consumer-a assigns to its own package, and the
	// target key must match the key the provider assigns to its own
	// package — the same parity already proven for symbols in
	// TestNormalizeGoKeepsCrossRepositoryIdentity.
	if dependency.SourceKey != consumerPackage.Key {
		t.Fatalf("PACKAGE_DEPENDS_ON source = %q, want the consumer's own key %q",
			dependency.SourceKey, consumerPackage.Key)
	}
	if dependency.TargetKey != providerPackage.Key {
		t.Fatalf("PACKAGE_DEPENDS_ON target = %q, want the provider's own key %q",
			dependency.TargetKey, providerPackage.Key)
	}
	if dependency.Confidence != ExactTypechecked || dependency.Provenance != GoObjectPath {
		t.Fatalf("PACKAGE_DEPENDS_ON edge = %#v", dependency)
	}
	if dependency.EvidenceKey == "" {
		t.Fatalf("PACKAGE_DEPENDS_ON edge carries no evidence: %#v", dependency)
	}

	// MODULE_DEPENDS_ON must describe exactly the same pair, sharing the
	// same evidence: one witness proves both facts about the same use.
	if moduleDepends[0].SourceKey != dependency.SourceKey ||
		moduleDepends[0].TargetKey != dependency.TargetKey ||
		moduleDepends[0].EvidenceKey != dependency.EvidenceKey {
		t.Fatalf("MODULE_DEPENDS_ON = %#v, want the same pair and evidence as PACKAGE_DEPENDS_ON %#v",
			moduleDepends[0], dependency)
	}

	if report.EdgesWithoutTarget != 0 {
		t.Fatalf("dropped package targets = %d, want none in the positive fixture", report.EdgesWithoutTarget)
	}

	merged := Set{}
	merged.Merge(provider)
	merged.Merge(consumer)
	if err := merged.Validate(); err != nil {
		t.Fatalf("merged Validate() error = %v", err)
	}
}

// TestNormalizeGoEmitsPackageDependencyForANestedModuleOfTheSameRepository
// proves a dependency that crosses a module boundary without crossing a
// repository boundary still gets MODULE_DEPENDS_ON: consumer-b reaches its
// own internal/legacy module through a local replace directive, and that
// nested module is a different Go module even though it lives in the same
// repository as the consumer.
func TestNormalizeGoEmitsPackageDependencyForANestedModuleOfTheSameRepository(t *testing.T) {
	provider, _ := normalizeFixture(t, "shared-library")
	consumer, report := normalizeFixture(t, "consumer-b")

	// consumer-b also references shared-library's Answer and Compute, so it
	// only validates once merged with its other provider, exactly like
	// TestNormalizeGoKeepsCrossRepositoryIdentity already establishes.
	merged := Set{}
	merged.Merge(provider)
	merged.Merge(consumer)
	if err := merged.Validate(); err != nil {
		t.Fatalf("Validate() error = %v", err)
	}

	byName := make(map[string]Package, len(consumer.Packages))
	for _, entry := range consumer.Packages {
		byName[entry.Name] = entry
	}
	main, hasMain := byName["example.com/kivgraph-fixture/consumer-b"]
	legacy, hasLegacy := byName["example.com/kivgraph-fixture/legacy"]
	if !hasMain || !hasLegacy {
		t.Fatalf("packages = %#v, want both main and legacy", consumer.Packages)
	}
	if legacy.RepositoryKey != main.RepositoryKey {
		t.Fatalf("legacy repository = %q, main repository = %q, want the same repository",
			legacy.RepositoryKey, main.RepositoryKey)
	}
	if legacy.Container == main.Container {
		t.Fatalf("legacy and main share a module %q, want different modules", legacy.Container)
	}

	var packageDepends, moduleDepends []Edge
	for _, edge := range consumer.Edges {
		if edge.SourceKey != main.Key || edge.TargetKey != legacy.Key {
			continue
		}
		switch edge.Kind {
		case PackageDependsOn:
			packageDepends = append(packageDepends, edge)
		case ModuleDependsOn:
			moduleDepends = append(moduleDepends, edge)
		}
	}
	if len(packageDepends) != 1 || len(moduleDepends) != 1 {
		t.Fatalf("main -> legacy edges: PACKAGE_DEPENDS_ON=%#v MODULE_DEPENDS_ON=%#v, want one of each",
			packageDepends, moduleDepends)
	}
	if report.EdgesWithoutTarget != 0 {
		t.Fatalf("dropped package targets = %d, want none: legacy is indexed in this same repository pass",
			report.EdgesWithoutTarget)
	}
}

// TestNormalizeGoEmitsIntraModulePackageDependencyWithoutModuleDependsOn
// proves acceptance criterion (b): two packages of the same Go module
// produce PACKAGE_DEPENDS_ON but never MODULE_DEPENDS_ON, because they share
// one Container. The cross-repository fixture has no same-module,
// two-package case, so the type-relations fixture's units package exercises
// it instead.
func TestNormalizeGoEmitsIntraModulePackageDependencyWithoutModuleDependsOn(t *testing.T) {
	set, _ := normalizeTypeRelationsFixture(t)
	if err := set.Validate(); err != nil {
		t.Fatalf("Validate() error = %v", err)
	}

	byName := make(map[string]Package, len(set.Packages))
	for _, entry := range set.Packages {
		byName[entry.Name] = entry
	}
	units, hasUnits := byName["example.com/kivgraph-fixture/type-relations/units"]
	geometry, hasGeometry := byName["example.com/kivgraph-fixture/type-relations"]
	if !hasUnits || !hasGeometry {
		t.Fatalf("packages = %#v, want both units and the geometry root package", set.Packages)
	}
	if units.Container == "" || units.Container != geometry.Container {
		t.Fatalf("units.Container = %q, geometry.Container = %q, want the same module",
			units.Container, geometry.Container)
	}

	var packageDepends, moduleDepends []Edge
	for _, edge := range set.Edges {
		if edge.SourceKey != units.Key || edge.TargetKey != geometry.Key {
			continue
		}
		switch edge.Kind {
		case PackageDependsOn:
			packageDepends = append(packageDepends, edge)
		case ModuleDependsOn:
			moduleDepends = append(moduleDepends, edge)
		}
	}
	if len(packageDepends) != 1 {
		t.Fatalf("PACKAGE_DEPENDS_ON units -> geometry edges = %#v, want exactly one", packageDepends)
	}
	if packageDepends[0].Confidence != ExactTypechecked || packageDepends[0].Provenance != GoTypesUse {
		t.Fatalf("PACKAGE_DEPENDS_ON edge = %#v, want a local resolution", packageDepends[0])
	}
	if len(moduleDepends) != 0 {
		t.Fatalf("MODULE_DEPENDS_ON units -> geometry edges = %#v, want none: same module", moduleDepends)
	}
}

// TestNormalizeGoNeverDuplicatesAPackageDependencyEdge proves acceptance
// criterion (d): PACKAGE_DEPENDS_ON and MODULE_DEPENDS_ON never repeat for
// the same (source, target) package pair, however many uses cross that
// boundary.
func TestNormalizeGoNeverDuplicatesAPackageDependencyEdge(t *testing.T) {
	consumerA, _ := normalizeFixture(t, "consumer-a")
	consumerB, _ := normalizeFixture(t, "consumer-b")
	typeRelations, _ := normalizeTypeRelationsFixture(t)

	checked := 0
	for _, set := range []Set{consumerA, consumerB, typeRelations} {
		counts := make(map[string]int)
		for _, edge := range set.Edges {
			if edge.Kind != PackageDependsOn && edge.Kind != ModuleDependsOn {
				continue
			}
			counts[string(edge.Kind)+"\x00"+edge.SourceKey+"\x00"+edge.TargetKey]++
			checked++
		}
		for key, count := range counts {
			if count != 1 {
				t.Fatalf("edge %q appears %d times, want exactly one", key, count)
			}
		}
	}
	if checked == 0 {
		t.Fatalf("no PACKAGE_DEPENDS_ON or MODULE_DEPENDS_ON edges were produced to check")
	}
}
