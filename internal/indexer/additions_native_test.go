//go:build ladybug && cgo

package indexer

import (
	"context"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/Luqueee/ladygraph/internal/facts"
	"github.com/Luqueee/ladygraph/internal/rebuild"
	"github.com/Luqueee/ladygraph/internal/storage/generation"
	"github.com/Luqueee/ladygraph/internal/storage/ladybug"
	"github.com/Luqueee/ladygraph/internal/testsupport"
)

// TestUpdateAdditionsMatchAFullLoad exercises every LUQUE-1009 addition
// boundary through the real canonical loader and delta applier. A successful
// test is stronger than checking mutation counters: the post-delta graph must
// be byte-for-byte equivalent to loading the next fact set from scratch.
func TestUpdateAdditionsMatchAFullLoad(t *testing.T) {
	cases := []struct {
		name string
		add  func(*facts.Set)
	}{
		{name: "file", add: addFile},
		{name: "symbol", add: addSymbol},
		{name: "export", add: addExport},
		{name: "consumer", add: addConsumer},
		{name: "package", add: addPackage},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			previous := updateFixture(t)
			next := cloneUpdateSet(previous)
			testCase.add(&next)
			next.Sort()
			if err := next.Validate(); err != nil {
				t.Fatalf("next fixture is invalid: %v", err)
			}
			runNativeAddition(t, previous, next)
		})
	}
}

func runNativeAddition(t *testing.T, previous, next facts.Set) {
	t.Helper()
	ctx := context.Background()
	loadOptions := ladybug.CanonicalLoadOptions{SnapshotID: 11, ResolverVersion: "addition-native"}

	activePath := testsupport.TempDir(t)
	activeDatabase := filepath.Join(activePath, "graph.db")
	if _, err := ladybug.LoadCanonical(ctx, activeDatabase, previous, loadOptions); err != nil {
		t.Fatalf("load previous state: %v", err)
	}
	layout := rebuild.Layout{
		Active: generation.Generation{ID: "000011", Path: activePath, DatabasePath: activeDatabase},
		NextID: "000012",
	}
	result, err := Update(ctx, UpdateOptions{
		Root:       testsupport.TempDir(t),
		Plans:      []InvalidationPlan{{Class: ChangeDeclarationAdded, Actions: []InvalidationAction{ActionReindexProvider, ActionResolveReferences}}},
		Previous:   previous,
		Next:       next,
		SnapshotID: loadOptions.SnapshotID, ResolverVersion: loadOptions.ResolverVersion,
		Layout: func(context.Context, rebuild.LayoutOptions) (rebuild.Layout, error) {
			return layout, nil
		},
		Republish: func(context.Context, rebuild.Options) (rebuild.Report, error) {
			t.Fatal("an addition within the delta budget must not republish")
			return rebuild.Report{}, nil
		},
	})
	if err != nil {
		t.Fatalf("Update() error = %v", err)
	}
	if result.Decision.Route != RouteDelta || !result.Passed {
		t.Fatalf("result = %#v, want a passing DELTA route", result)
	}
	if result.Mutation.UpsertedNodes == 0 && result.Mutation.UpsertedEdges == 0 {
		t.Fatalf("mutation = %#v, want at least one upsert", result.Mutation)
	}

	referenceDatabase := filepath.Join(testsupport.TempDir(t), "graph.db")
	if _, err := ladybug.LoadCanonical(ctx, referenceDatabase, next, loadOptions); err != nil {
		t.Fatalf("load next state from scratch: %v", err)
	}
	mutated, err := ladybug.ScanCanonical(ctx, activeDatabase)
	if err != nil {
		t.Fatalf("scan mutated graph: %v", err)
	}
	reference, err := ladybug.ScanCanonical(ctx, referenceDatabase)
	if err != nil {
		t.Fatalf("scan reference graph: %v", err)
	}
	if !reflect.DeepEqual(mutated, reference) {
		t.Fatalf("post-delta graph differs from full load:\nmutated = %#v\nreference = %#v", mutated, reference)
	}
	integrity, err := ladybug.VerifyCanonicalIntegrity(ctx, activeDatabase)
	if err != nil {
		t.Fatalf("verify canonical integrity: %v", err)
	}
	if !integrity.Passed {
		t.Fatalf("integrity = %#v, want all invariants passing", integrity)
	}
}

func cloneUpdateSet(set facts.Set) facts.Set {
	return facts.Set{
		Repositories: append([]facts.Repository(nil), set.Repositories...),
		Packages:     append([]facts.Package(nil), set.Packages...),
		Files:        append([]facts.File(nil), set.Files...),
		Symbols:      append([]facts.Symbol(nil), set.Symbols...),
		Evidence:     append([]facts.Evidence(nil), set.Evidence...),
		Edges:        append([]facts.Edge(nil), set.Edges...),
		Unresolved:   append([]facts.UnresolvedReference(nil), set.Unresolved...),
	}
}

func addFile(set *facts.Set) {
	repositoryKey := facts.RepositoryKey("acme/widgets")
	packageKey := facts.PackageKey(facts.LanguageGo, repositoryKey, "widgets")
	fileKey := facts.FileKey(repositoryKey, "c.go")
	set.Files = append(set.Files, facts.File{Key: fileKey, RepositoryKey: repositoryKey, PackageKey: packageKey, Path: "c.go", Language: facts.LanguageGo, ContentHash: "hash-c-1"})
	set.Edges = append(set.Edges, facts.Edge{Kind: facts.ContainsFile, SourceKey: packageKey, TargetKey: fileKey, Confidence: facts.StructuralCertain, Provenance: facts.PackageManifest})
}

func addSymbol(set *facts.Set) {
	repositoryKey := facts.RepositoryKey("acme/widgets")
	packageKey := facts.PackageKey(facts.LanguageGo, repositoryKey, "widgets")
	fileKey := facts.FileKey(repositoryKey, "a.go")
	symbolKey := "symbol:go:acme/widgets.Added"
	set.Symbols = append(set.Symbols, facts.Symbol{
		Key: symbolKey, CanonicalIdentity: "go:acme/widgets.Added", RepositoryKey: repositoryKey, PackageKey: packageKey,
		FileKey: fileKey, Language: facts.LanguageGo, Name: "Added", QualifiedName: "widgets.Added", Kind: "func",
		Exported: true, Start: facts.Position{Line: 5, Offset: 50}, End: facts.Position{Line: 7, Offset: 80},
	})
	set.Edges = append(set.Edges, facts.Edge{Kind: facts.Defines, SourceKey: fileKey, TargetKey: symbolKey, Confidence: facts.StructuralCertain, Provenance: facts.GoTypesDefinition})
}

func addExport(set *facts.Set) {
	repositoryKey := facts.RepositoryKey("acme/widgets")
	fileKey := facts.FileKey(repositoryKey, "a.go")
	symbolA := "symbol:go:acme/widgets.A"
	symbolB := "symbol:go:acme/widgets.B"
	evidenceKey := facts.EvidenceKey(fileKey, 8, 18)
	set.Evidence = append(set.Evidence, facts.Evidence{Key: evidenceKey, RepositoryKey: repositoryKey, FileKey: fileKey, Start: facts.Position{Line: 8, Offset: 80}, End: facts.Position{Line: 9, Offset: 90}, Text: "export B"})
	set.Edges = append(set.Edges, facts.Edge{Kind: facts.Exports, SourceKey: symbolA, TargetKey: symbolB, Confidence: facts.ExactTypechecked, Provenance: facts.GoTypesUse, EvidenceKey: evidenceKey})
}

func addConsumer(set *facts.Set) {
	repositoryKey := facts.RepositoryKey("acme/widgets")
	packageKey := facts.PackageKey(facts.LanguageGo, repositoryKey, "widgets")
	fileKey := facts.FileKey(repositoryKey, "consumer.go")
	symbolKey := "symbol:go:acme/widgets.Consumer"
	targetKey := "symbol:go:acme/widgets.A"
	evidenceKey := facts.EvidenceKey(fileKey, 4, 14)
	set.Files = append(set.Files, facts.File{Key: fileKey, RepositoryKey: repositoryKey, PackageKey: packageKey, Path: "consumer.go", Language: facts.LanguageGo, ContentHash: "hash-consumer-1"})
	set.Symbols = append(set.Symbols, facts.Symbol{
		Key: symbolKey, CanonicalIdentity: "go:acme/widgets.Consumer", RepositoryKey: repositoryKey, PackageKey: packageKey,
		FileKey: fileKey, Language: facts.LanguageGo, Name: "Consumer", QualifiedName: "widgets.Consumer", Kind: "func",
		Start: facts.Position{Line: 1, Offset: 0}, End: facts.Position{Line: 4, Offset: 40},
	})
	set.Evidence = append(set.Evidence, facts.Evidence{Key: evidenceKey, RepositoryKey: repositoryKey, FileKey: fileKey, Start: facts.Position{Line: 4, Offset: 30}, End: facts.Position{Line: 4, Offset: 40}, Text: "A()"})
	set.Edges = append(set.Edges,
		facts.Edge{Kind: facts.ContainsFile, SourceKey: packageKey, TargetKey: fileKey, Confidence: facts.StructuralCertain, Provenance: facts.PackageManifest},
		facts.Edge{Kind: facts.Defines, SourceKey: fileKey, TargetKey: symbolKey, Confidence: facts.StructuralCertain, Provenance: facts.GoTypesDefinition},
		facts.Edge{Kind: facts.CallsDirect, SourceKey: symbolKey, TargetKey: targetKey, Confidence: facts.ExactTypechecked, Provenance: facts.GoASTCall, EvidenceKey: evidenceKey},
	)
}

func addPackage(set *facts.Set) {
	repositoryKey := facts.RepositoryKey("acme/extra")
	packageKey := facts.PackageKey(facts.LanguageGo, repositoryKey, "extra")
	fileKey := facts.FileKey(repositoryKey, "extra.go")
	symbolKey := "symbol:go:acme/extra.Extra"
	set.Repositories = append(set.Repositories, facts.Repository{Key: repositoryKey, Name: "acme/extra", RootPath: "/repos/extra", Languages: []facts.Language{facts.LanguageGo}})
	set.Packages = append(set.Packages, facts.Package{Key: packageKey, RepositoryKey: repositoryKey, Language: facts.LanguageGo, Name: "extra", RootPath: "/repos/extra"})
	set.Files = append(set.Files, facts.File{Key: fileKey, RepositoryKey: repositoryKey, PackageKey: packageKey, Path: "extra.go", Language: facts.LanguageGo, ContentHash: "hash-extra-1"})
	set.Symbols = append(set.Symbols, facts.Symbol{
		Key: symbolKey, CanonicalIdentity: "go:acme/extra.Extra", RepositoryKey: repositoryKey, PackageKey: packageKey,
		FileKey: fileKey, Language: facts.LanguageGo, Name: "Extra", QualifiedName: "extra.Extra", Kind: "func",
		Start: facts.Position{Line: 1, Offset: 0}, End: facts.Position{Line: 3, Offset: 30},
	})
	set.Edges = append(set.Edges,
		facts.Edge{Kind: facts.ContainsPackage, SourceKey: repositoryKey, TargetKey: packageKey, Confidence: facts.StructuralCertain, Provenance: facts.PackageManifest},
		facts.Edge{Kind: facts.ContainsFile, SourceKey: packageKey, TargetKey: fileKey, Confidence: facts.StructuralCertain, Provenance: facts.PackageManifest},
		facts.Edge{Kind: facts.Defines, SourceKey: fileKey, TargetKey: symbolKey, Confidence: facts.StructuralCertain, Provenance: facts.GoTypesDefinition},
	)
}
