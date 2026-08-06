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

// typeRelationsFixtureRoot is the single self-contained module used by the
// IMPLEMENTS, EMBEDS and OVERRIDES tests: it needs no synthetic go.work.
var typeRelationsFixtureRoot = filepath.Join("..", "..", "testdata", "go", "type-relations")

// normalizeTypeRelationsFixture indexes the type-relations fixture end to
// end, including its IMPLEMENTS, EMBEDS and OVERRIDES structural relations.
func normalizeTypeRelationsFixture(t *testing.T) (Set, GoReport) {
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

	set, report, err := NormalizeGo(context.Background(), GoInput{
		Repository:    repository,
		Definitions:   keyed,
		TypeRelations: relations,
	})
	if err != nil {
		t.Fatalf("NormalizeGo() error = %v", err)
	}
	return set, report
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
