package tools

import (
	"context"
	"testing"
	"time"

	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/Luqueee/ladygraph/internal/facts"
	"github.com/Luqueee/ladygraph/internal/hotsnapshot"
)

func TestFindCrossRepoConsumersReturnsAllConfidenceCategories(t *testing.T) {
	snapshot, err := hotsnapshot.BuildGraphSnapshot(crossRepoConsumerRows(), 17, time.Unix(1_700_000_000, 0).UTC(), 1)
	if err != nil {
		t.Fatalf("BuildGraphSnapshot() error = %v", err)
	}
	store := hotsnapshot.NewSnapshotStore(snapshot)
	_, response, err := findCrossRepoConsumers(context.Background(), nil, FindCrossRepoConsumersInput{StableKey: "sym-target"}, store)
	if err != nil {
		t.Fatalf("findCrossRepoConsumers() error = %v", err)
	}
	if response.Total != 4 || response.Returned != 4 || response.Truncated {
		t.Fatalf("response pagination = %#v, want four untruncated results", response)
	}
	// The package dependency is evidence about the package, never a use of
	// the symbol: it is counted apart from the one exact consumer.
	if response.Coverage != (Coverage{Exact: 1, Candidate: 1, UnresolvedRelated: 1, PackageLevel: 1}) {
		t.Fatalf("response coverage = %#v, want exact=1 candidate=1 unresolved=1 package=1", response.Coverage)
	}
	categories := []string{
		response.Results[0].Category,
		response.Results[1].Category,
		response.Results[2].Category,
		response.Results[3].Category,
	}
	wantCategories := []string{CrossRepoConsumerExactSymbol, CrossRepoConsumerPackage, CrossRepoConsumerCandidate, CrossRepoConsumerUnresolved}
	for index := range categories {
		if categories[index] != wantCategories[index] {
			t.Fatalf("result categories = %v, want %v", categories, wantCategories)
		}
	}
	if response.Results[0].ConsumerSymbolKey != "sym-exact" || response.Results[0].Kind != string(facts.References) {
		t.Fatalf("exact result = %#v", response.Results[0])
	}
	if response.Results[1].ConsumerSymbolKey != "" || response.Results[1].ConsumerPackageKey != "pkg-consumer" {
		t.Fatalf("package result = %#v, want package-only consumer", response.Results[1])
	}
	if response.Results[2].Confidence != string(facts.Candidate) {
		t.Fatalf("candidate result = %#v", response.Results[2])
	}
	unresolved := response.Results[3]
	if unresolved.UnresolvedKey != "unresolved-consumer" || unresolved.Reason != "package_not_indexed" || unresolved.TargetSymbolKey != "sym-target" {
		t.Fatalf("unresolved result = %#v", unresolved)
	}
}

func TestFindCrossRepoConsumersIgnoresFailuresThatNamedNoSymbol(t *testing.T) {
	snapshot, err := hotsnapshot.BuildGraphSnapshot(crossRepoConsumerRows(), 19, time.Unix(1_700_000_000, 0).UTC(), 1)
	if err != nil {
		t.Fatalf("BuildGraphSnapshot() error = %v", err)
	}
	store := hotsnapshot.NewSnapshotStore(snapshot)
	_, response, err := findCrossRepoConsumers(context.Background(), nil, FindCrossRepoConsumersInput{StableKey: "sym-target"}, store)
	if err != nil {
		t.Fatalf("findCrossRepoConsumers() error = %v", err)
	}
	// The fixture holds a failure for the whole `example.com/target` import
	// with no requested symbol. It belongs to the package, not to every
	// symbol the package exports, so a query about one symbol never sees it.
	for _, result := range response.Results {
		if result.UnresolvedKey == "unresolved-module" {
			t.Fatalf("symbol query returned a package-level failure: %#v", result)
		}
	}
	if response.Coverage.UnresolvedRelated != 1 {
		t.Fatalf("unresolved_related = %d, want only the failure that named this symbol", response.Coverage.UnresolvedRelated)
	}
}

func TestFindCrossRepoConsumersFiltersAndPaginatesWithSnapshotCursor(t *testing.T) {
	snapshot, err := hotsnapshot.BuildGraphSnapshot(crossRepoConsumerRows(), 18, time.Unix(1_700_000_000, 0).UTC(), 1)
	if err != nil {
		t.Fatal(err)
	}
	store := hotsnapshot.NewSnapshotStore(snapshot)
	_, first, err := findCrossRepoConsumers(context.Background(), nil, FindCrossRepoConsumersInput{StableKey: "sym-target", Language: "go", Limit: 2}, store)
	if err != nil {
		t.Fatal(err)
	}
	if first.Returned != 2 || !first.Truncated || first.NextCursor == nil {
		t.Fatalf("first page = %#v, want two results and a cursor", first)
	}
	_, second, err := findCrossRepoConsumers(context.Background(), nil, FindCrossRepoConsumersInput{StableKey: "sym-target", Language: "go", Limit: 2, Cursor: *first.NextCursor}, store)
	if err != nil {
		t.Fatal(err)
	}
	if second.Returned != 2 || second.Truncated || second.NextCursor != nil {
		t.Fatalf("second page = %#v, want final two results", second)
	}
	_, filtered, err := findCrossRepoConsumers(context.Background(), nil, FindCrossRepoConsumersInput{StableKey: "sym-target", Repo: "repo-consumer"}, store)
	if err != nil {
		t.Fatal(err)
	}
	if filtered.Total != 4 {
		t.Fatalf("repo filter total = %d, want four consumer facts", filtered.Total)
	}
}

func TestFindCrossRepoConsumersIsRegisteredReadOnly(t *testing.T) {
	server := sdkmcp.NewServer(&sdkmcp.Implementation{Name: "test", Version: "0.0.1"}, nil)
	RegisterFindCrossRepoConsumers(server)
	serverTransport, clientTransport := sdkmcp.NewInMemoryTransports()
	serverSession, err := server.Connect(context.Background(), serverTransport, nil)
	if err != nil {
		t.Fatalf("server.Connect() error = %v", err)
	}
	t.Cleanup(func() { _ = serverSession.Close() })

	client := sdkmcp.NewClient(&sdkmcp.Implementation{Name: "test-client", Version: "0.0.1"}, nil)
	clientSession, err := client.Connect(context.Background(), clientTransport, nil)
	if err != nil {
		t.Fatalf("client.Connect() error = %v", err)
	}
	t.Cleanup(func() { _ = clientSession.Close() })

	listed, err := clientSession.ListTools(context.Background(), nil)
	if err != nil {
		t.Fatalf("ListTools() error = %v", err)
	}
	for _, tool := range listed.Tools {
		if tool.Name == findCrossRepoConsumersToolName {
			if tool.Annotations == nil || !tool.Annotations.ReadOnlyHint {
				t.Fatalf("find_cross_repo_consumers annotations = %#v, want read-only", tool.Annotations)
			}
			return
		}
	}
	t.Fatal("find_cross_repo_consumers is not registered")
}

func crossRepoConsumerRows() hotsnapshot.LadybugSnapshotRows {
	return hotsnapshot.LadybugSnapshotRows{
		Repositories: []hotsnapshot.RepositoryRow{
			{Key: "repo-target", Name: "target", Languages: "go"},
			{Key: "repo-consumer", Name: "consumer", Languages: "go"},
		},
		Packages: []hotsnapshot.PackageRow{
			{Key: "pkg-target", RepositoryKey: "repo-target", Language: "go", Name: "target", ModulePath: "example.com/target"},
			{Key: "pkg-consumer", RepositoryKey: "repo-consumer", Language: "go", Name: "consumer", ModulePath: "example.com/consumer"},
		},
		Files: []hotsnapshot.FileRow{
			{Key: "file-target", RepositoryKey: "repo-target", PackageKey: "pkg-target", Path: "target.go", Language: "go"},
			{Key: "file-consumer", RepositoryKey: "repo-consumer", PackageKey: "pkg-consumer", Path: "consumer.go", Language: "go"},
		},
		Symbols: []hotsnapshot.SymbolRow{
			{StableKey: "sym-target", CanonicalIdentity: "go:target.Target", FileKey: "file-target", Language: "go", Name: "Target", QualifiedName: "target.Target", Kind: "func"},
			{StableKey: "sym-exact", CanonicalIdentity: "go:consumer.Exact", FileKey: "file-consumer", Language: "go", Name: "Exact", QualifiedName: "consumer.Exact", Kind: "func"},
			{StableKey: "sym-candidate", CanonicalIdentity: "go:consumer.Candidate", FileKey: "file-consumer", Language: "go", Name: "Candidate", QualifiedName: "consumer.Candidate", Kind: "func"},
			{StableKey: "sym-unresolved", CanonicalIdentity: "go:consumer.Unresolved", FileKey: "file-consumer", Language: "go", Name: "Unresolved", QualifiedName: "consumer.Unresolved", Kind: "func"},
		},
		Edges: []hotsnapshot.EdgeRow{
			{SourceKey: "sym-exact", TargetKey: "sym-target", Kind: facts.CodeReferences, Confidence: facts.CodeExactTypechecked, Provenance: facts.CodeGoTypesUse, EvidenceKind: "types", EvidenceSourceFileKey: "file-consumer", EvidenceTargetFileKey: "file-target"},
			{SourceKey: "sym-candidate", TargetKey: "sym-target", Kind: facts.CodeReferences, Confidence: facts.CodeCandidate, Provenance: facts.CodeTreeSitterSyntax, EvidenceKind: "syntax", EvidenceSourceFileKey: "file-consumer", EvidenceTargetFileKey: "file-target"},
		},
		PackageDependencies: []hotsnapshot.PackageDependencyRow{
			{SourceKey: "pkg-consumer", TargetKey: "pkg-target", Kind: facts.CodePackageDependsOn, Confidence: facts.CodeStructuralCertain, Provenance: facts.CodePackageManifest},
		},
		Unresolved: []hotsnapshot.UnresolvedReferenceRow{
			{Key: "unresolved-consumer", RepositoryKey: "repo-consumer", FileKey: "file-consumer", SourceKey: "sym-unresolved", Language: "go", RequestedPackage: "example.com/target", RequestedSymbol: "Target", Reason: "package_not_indexed", Detail: "target package was not available during resolution", StartLine: 12, StartColumn: 4, StartOffset: 180},
			// A failure that names the package and no symbol: the whole
			// import broke, so no symbol was ever requested.
			{Key: "unresolved-module", RepositoryKey: "repo-consumer", FileKey: "file-consumer", Language: "go", RequestedPackage: "example.com/target", Reason: "module_not_resolved", Detail: "example.com/target is not installed", StartLine: 3, StartColumn: 1, StartOffset: 20},
		},
	}
}
