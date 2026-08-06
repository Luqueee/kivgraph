package tools

import (
	"context"
	"testing"
	"time"

	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/Luqueee/ladygraph/internal/facts"
	"github.com/Luqueee/ladygraph/internal/hotsnapshot"
)

// TestGetBlastRadiusGroupsImpactAlongFourAxes checks the direction and the
// aggregation contract at once: the traversal walks incoming edges, the root is
// never counted as affected by its own change, and every axis accounts for the
// same set of symbols.
func TestGetBlastRadiusGroupsImpactAlongFourAxes(t *testing.T) {
	store := blastRadiusStore(t, 41)

	_, response, err := getBlastRadius(context.Background(), nil, GetBlastRadiusInput{StableKey: "sym-core"}, store)
	if err != nil {
		t.Fatalf("getBlastRadius() error = %v", err)
	}
	radius := response.Results
	if radius.RootKey != "sym-core" || radius.RootRepositoryKey != "repo-core" {
		t.Fatalf("root = %#v", radius)
	}
	if radius.Affected != 3 || radius.DeepestDepth != 2 || radius.TraversalTruncated {
		t.Fatalf("impact metadata = %#v, want three affected symbols across two depths", radius)
	}
	wantRepositories := []BlastRadiusGroup{{Key: "repo-app", Count: 2}, {Key: "repo-core", Count: 1}}
	if len(radius.ByRepository) != 2 || radius.ByRepository[0] != wantRepositories[0] || radius.ByRepository[1] != wantRepositories[1] {
		t.Fatalf("by_repository = %#v, want %#v", radius.ByRepository, wantRepositories)
	}
	wantDepths := []BlastRadiusDepthGroup{{Depth: 1, Count: 2}, {Depth: 2, Count: 1}}
	if len(radius.ByDepth) != 2 || radius.ByDepth[0] != wantDepths[0] || radius.ByDepth[1] != wantDepths[1] {
		t.Fatalf("by_depth = %#v, want %#v", radius.ByDepth, wantDepths)
	}
	wantKinds := []BlastRadiusGroup{{Key: string(facts.CallsDirect), Count: 2}, {Key: string(facts.References), Count: 1}}
	if len(radius.ByKind) != 2 || radius.ByKind[0] != wantKinds[0] || radius.ByKind[1] != wantKinds[1] {
		t.Fatalf("by_kind = %#v, want %#v", radius.ByKind, wantKinds)
	}
	if total := blastRadiusGroupTotal(radius); total != radius.Affected {
		t.Fatalf("axes account for %d symbols, want %d", total, radius.Affected)
	}
	if response.Coverage != (Coverage{Exact: 2, Candidate: 1}) {
		t.Fatalf("coverage = %#v", response.Coverage)
	}
}

// TestGetBlastRadiusPagesPackagesAndKeepsOtherAxesComplete fixes what the
// envelope pages over: only the package axis, because it is the one that grows
// with the corpus.
func TestGetBlastRadiusPagesPackagesAndKeepsOtherAxesComplete(t *testing.T) {
	store := blastRadiusStore(t, 42)

	_, first, err := getBlastRadius(context.Background(), nil, GetBlastRadiusInput{StableKey: "sym-core", Limit: 1}, store)
	if err != nil {
		t.Fatal(err)
	}
	if first.Total != 2 || first.Returned != 1 || !first.Truncated || first.NextCursor == nil {
		t.Fatalf("first page = %#v, want one of two package groups", first)
	}
	if first.Results.ByPackage[0].PackageKey != "pkg-app" || first.Results.ByPackage[0].Count != 2 {
		t.Fatalf("first package group = %#v", first.Results.ByPackage)
	}
	if len(first.Results.ByRepository) != 2 || len(first.Results.ByDepth) != 2 || first.Results.Affected != 3 {
		t.Fatalf("non-paged axes = %#v, want the complete aggregate on every page", first.Results)
	}

	_, second, err := getBlastRadius(context.Background(), nil, GetBlastRadiusInput{
		StableKey: "sym-core", Limit: 1, Cursor: *first.NextCursor,
	}, store)
	if err != nil {
		t.Fatal(err)
	}
	if second.Returned != 1 || second.Truncated || second.NextCursor != nil {
		t.Fatalf("second page = %#v, want the final package group", second)
	}
	if second.Results.ByPackage[0].PackageKey != "pkg-core" || second.Results.ByPackage[0].RepositoryKey != "repo-core" {
		t.Fatalf("second package group = %#v", second.Results.ByPackage)
	}
}

// TestGetBlastRadiusBoundsReachability keeps depth, confidence and max_nodes
// honest: each one changes what is reachable, not only what is displayed.
func TestGetBlastRadiusBoundsReachability(t *testing.T) {
	store := blastRadiusStore(t, 43)

	_, shallow, err := getBlastRadius(context.Background(), nil, GetBlastRadiusInput{StableKey: "sym-core", Depth: 1}, store)
	if err != nil {
		t.Fatal(err)
	}
	if shallow.Results.Affected != 2 || shallow.Results.DeepestDepth != 1 {
		t.Fatalf("depth-1 impact = %#v, want only direct consumers", shallow.Results)
	}

	_, exactOnly, err := getBlastRadius(context.Background(), nil, GetBlastRadiusInput{
		StableKey: "sym-core", Confidence: string(facts.ExactTypechecked),
	}, store)
	if err != nil {
		t.Fatal(err)
	}
	if exactOnly.Results.Affected != 2 {
		t.Fatalf("exact-only impact = %#v, want the candidate consumer excluded with its subtree", exactOnly.Results)
	}

	_, bounded, err := getBlastRadius(context.Background(), nil, GetBlastRadiusInput{StableKey: "sym-core", MaxNodes: 2}, store)
	if err != nil {
		t.Fatal(err)
	}
	if !bounded.Results.TraversalTruncated || bounded.Results.Affected != 1 {
		t.Fatalf("node-bounded impact = %#v, want one affected symbol and a truncated traversal", bounded.Results)
	}
}

func TestGetBlastRadiusClassifiesFailures(t *testing.T) {
	store := blastRadiusStore(t, 44)
	expired, cancel := context.WithDeadline(context.Background(), time.Unix(1, 0))
	defer cancel()

	cases := []struct {
		name      string
		ctx       context.Context
		arguments GetBlastRadiusInput
		store     *hotsnapshot.SnapshotStore
		wantCode  string
	}{
		{name: "empty key", ctx: context.Background(), arguments: GetBlastRadiusInput{}, store: store, wantCode: CodeInvalidArgument},
		{name: "depth above maximum", ctx: context.Background(), arguments: GetBlastRadiusInput{StableKey: "sym-core", Depth: MaximumBlastRadiusDepth + 1}, store: store, wantCode: CodeInvalidArgument},
		{name: "max_nodes above maximum", ctx: context.Background(), arguments: GetBlastRadiusInput{StableKey: "sym-core", MaxNodes: MaximumBlastRadiusMaxNodes + 1}, store: store, wantCode: CodeInvalidArgument},
		{name: "limit above maximum", ctx: context.Background(), arguments: GetBlastRadiusInput{StableKey: "sym-core", Limit: MaximumBlastRadiusLimit + 1}, store: store, wantCode: CodeInvalidArgument},
		{name: "unsupported edge kind", ctx: context.Background(), arguments: GetBlastRadiusInput{StableKey: "sym-core", EdgeKinds: []string{"CONTAINS_FILE"}}, store: store, wantCode: CodeInvalidArgument},
		{name: "missing symbol", ctx: context.Background(), arguments: GetBlastRadiusInput{StableKey: "sym-missing"}, store: store, wantCode: CodeSymbolNotFound},
		{name: "invalid cursor", ctx: context.Background(), arguments: GetBlastRadiusInput{StableKey: "sym-core", Cursor: "not-a-cursor"}, store: store, wantCode: CodeCursorInvalid},
		{name: "unpublished snapshot", ctx: context.Background(), arguments: GetBlastRadiusInput{StableKey: "sym-core"}, store: hotsnapshot.NewSnapshotStore(nil), wantCode: CodeIndexNotReady},
		{name: "missing store", ctx: context.Background(), arguments: GetBlastRadiusInput{StableKey: "sym-core"}, wantCode: CodeIndexNotReady},
		{name: "expired request deadline", ctx: expired, arguments: GetBlastRadiusInput{StableKey: "sym-core"}, store: store, wantCode: CodeTraversalLimitReached},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			_, _, err := getBlastRadius(test.ctx, nil, test.arguments, test.store)
			if got := ErrorCode(err); got != test.wantCode {
				t.Fatalf("error code = %q, want %q (err=%v)", got, test.wantCode, err)
			}
		})
	}
}

func TestGetBlastRadiusIsRegisteredReadOnly(t *testing.T) {
	server := sdkmcp.NewServer(&sdkmcp.Implementation{Name: "test", Version: "0.0.1"}, nil)
	RegisterGetBlastRadius(server)
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
		if tool.Name == blastRadiusToolName {
			if tool.Annotations == nil || !tool.Annotations.ReadOnlyHint {
				t.Fatalf("get_blast_radius annotations = %#v, want read-only", tool.Annotations)
			}
			return
		}
	}
	t.Fatal("get_blast_radius is not registered")
}

func blastRadiusGroupTotal(radius BlastRadius) int {
	total := 0
	for _, group := range radius.ByRepository {
		total += group.Count
	}
	return total
}

// blastRadiusStore builds an impact fan-in on sym-core: two direct consumers,
// one of them candidate-confidence, and one indirect consumer behind the exact
// one. Consumers live in a second repository so the repository axis is real.
func blastRadiusStore(t *testing.T, id uint64) *hotsnapshot.SnapshotStore {
	t.Helper()
	snapshot, err := hotsnapshot.BuildGraphSnapshot(hotsnapshot.LadybugSnapshotRows{
		Repositories: []hotsnapshot.RepositoryRow{
			{Key: "repo-core", Name: "core", Languages: "go"},
			{Key: "repo-app", Name: "app", Languages: "go"},
		},
		Packages: []hotsnapshot.PackageRow{
			{Key: "pkg-core", RepositoryKey: "repo-core", Language: "go", Name: "core", ModulePath: "example.com/core"},
			{Key: "pkg-app", RepositoryKey: "repo-app", Language: "go", Name: "app", ModulePath: "example.com/app"},
		},
		Files: []hotsnapshot.FileRow{
			{Key: "file-core", RepositoryKey: "repo-core", PackageKey: "pkg-core", Path: "core.go", Language: "go"},
			{Key: "file-app", RepositoryKey: "repo-app", PackageKey: "pkg-app", Path: "app.go", Language: "go"},
		},
		Symbols: []hotsnapshot.SymbolRow{
			{StableKey: "sym-core", CanonicalIdentity: "go:core.Core", FileKey: "file-core", Language: "go", Name: "Core", QualifiedName: "core.Core", Kind: "func"},
			{StableKey: "sym-direct", CanonicalIdentity: "go:app.Direct", FileKey: "file-app", Language: "go", Name: "Direct", QualifiedName: "app.Direct", Kind: "func"},
			{StableKey: "sym-loose", CanonicalIdentity: "go:core.Loose", FileKey: "file-core", Language: "go", Name: "Loose", QualifiedName: "core.Loose", Kind: "func"},
			{StableKey: "sym-indirect", CanonicalIdentity: "go:app.Indirect", FileKey: "file-app", Language: "go", Name: "Indirect", QualifiedName: "app.Indirect", Kind: "func"},
		},
		Edges: []hotsnapshot.EdgeRow{
			{SourceKey: "sym-direct", TargetKey: "sym-core", Kind: facts.CodeCallsDirect, Confidence: facts.CodeExactTypechecked, Provenance: facts.CodeGoTypesUse, EvidenceKind: "types", EvidenceSourceFileKey: "file-app", EvidenceTargetFileKey: "file-core"},
			{SourceKey: "sym-loose", TargetKey: "sym-core", Kind: facts.CodeReferences, Confidence: facts.CodeCandidate, Provenance: facts.CodeTreeSitterSyntax, EvidenceKind: "syntax", EvidenceSourceFileKey: "file-core", EvidenceTargetFileKey: "file-core"},
			{SourceKey: "sym-indirect", TargetKey: "sym-direct", Kind: facts.CodeCallsDirect, Confidence: facts.CodeExactTypechecked, Provenance: facts.CodeGoTypesUse, EvidenceKind: "types", EvidenceSourceFileKey: "file-app", EvidenceTargetFileKey: "file-app"},
		},
	}, id, time.Unix(1_700_000_000, 0).UTC(), 1)
	if err != nil {
		t.Fatalf("BuildGraphSnapshot() error = %v", err)
	}
	return hotsnapshot.NewSnapshotStore(snapshot)
}
