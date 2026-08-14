package tools

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/Luqueee/ladygraph/internal/facts"
	"github.com/Luqueee/ladygraph/internal/hotsnapshot"
	"github.com/Luqueee/ladygraph/internal/metrics"
)

func TestGraphStatusIsReadOnlyAndEmpty(t *testing.T) {
	ctx := context.Background()
	server := sdkmcp.NewServer(&sdkmcp.Implementation{Name: "test", Version: "0.0.1"}, nil)
	RegisterGraphStatus(server)

	serverTransport, clientTransport := sdkmcp.NewInMemoryTransports()
	serverSession, err := server.Connect(ctx, serverTransport, nil)
	if err != nil {
		t.Fatalf("server.Connect() error = %v", err)
	}
	defer serverSession.Close()

	client := sdkmcp.NewClient(&sdkmcp.Implementation{Name: "test-client", Version: "0.0.1"}, nil)
	clientSession, err := client.Connect(ctx, clientTransport, nil)
	if err != nil {
		t.Fatalf("client.Connect() error = %v", err)
	}
	defer clientSession.Close()

	listed, err := clientSession.ListTools(ctx, nil)
	if err != nil {
		t.Fatalf("ListTools() error = %v", err)
	}
	var tool *sdkmcp.Tool
	for _, candidate := range listed.Tools {
		if candidate.Name == "graph_status" {
			tool = candidate
			break
		}
	}
	if tool == nil {
		t.Fatal("graph_status is not registered")
	}
	if tool.Annotations == nil || !tool.Annotations.ReadOnlyHint {
		t.Fatalf("graph_status annotations = %#v, want read-only", tool.Annotations)
	}

	result, err := clientSession.CallTool(ctx, &sdkmcp.CallToolParams{Name: "graph_status"})
	if err != nil {
		t.Fatalf("CallTool() error = %v", err)
	}
	if result.IsError {
		t.Fatalf("CallTool() returned an error result: %#v", result.Content)
	}

	response := decodeResponse[GraphStatus](t, result)
	status := response.Results
	if status.Status != "empty" || status.Repositories != 0 || status.Symbols != 0 || status.Edges != 0 {
		t.Fatalf("graph status = %#v, want empty response", status)
	}
	if response.SnapshotID != nil || response.SnapshotAgeMS != nil || response.NextCursor != nil {
		t.Fatalf("optional metadata = %#v, want nil for empty graph", response)
	}
	if status.Metrics != nil {
		t.Fatalf("metrics = %#v, want omitted without a registry", status.Metrics)
	}
	if response.Total != 1 || response.Returned != 1 || response.Truncated {
		t.Fatalf("response metadata = %#v, want one untruncated status result", response)
	}
}

// TestGraphStatusReportsPublishedSnapshotProvenanceAndCounts is the whole point
// of the tool: what is being served, where it came from, and what it holds.
func TestGraphStatusReportsPublishedSnapshotProvenanceAndCounts(t *testing.T) {
	store := graphStatusStore(t, 61)

	_, response, err := graphStatus(context.Background(), nil, struct{}{}, store, nil)
	if err != nil {
		t.Fatalf("graphStatus() error = %v", err)
	}
	status := response.Results
	if status.Status != GraphStatusReady {
		t.Fatalf("status = %q, want %q", status.Status, GraphStatusReady)
	}
	if response.SnapshotID == nil || *response.SnapshotID != 61 || response.SnapshotAgeMS == nil {
		t.Fatalf("envelope snapshot metadata = %#v", response)
	}
	if status.SchemaVersion != 2 || status.ResolverVersion != "resolver-v7" || status.SnapshotRowFormat != 1 {
		t.Fatalf("provenance = %#v, want the schema and resolver behind the graph", status)
	}
	if status.SnapshotBuiltAt != "2023-11-14T22:13:20Z" {
		t.Fatalf("snapshot_built_at = %q", status.SnapshotBuiltAt)
	}
	if status.Repositories != 2 || status.Packages != 2 || status.Files != 2 || status.Symbols != 3 ||
		status.Edges != 2 || status.Unresolved != 2 {
		t.Fatalf("counts = %#v", status)
	}
	wantEdges := []GraphStatusCount{
		{Key: string(facts.CallsDirect), Count: 1},
		{Key: string(facts.References), Count: 1},
	}
	if len(status.EdgesByKind) != 2 || status.EdgesByKind[0] != wantEdges[0] || status.EdgesByKind[1] != wantEdges[1] {
		t.Fatalf("edges_by_kind = %#v, want %#v", status.EdgesByKind, wantEdges)
	}
	wantReasons := []GraphStatusCount{
		{Key: "PACKAGE_PROVIDER_NOT_FOUND", Count: 1},
		{Key: "package_not_found", Count: 1},
	}
	if len(status.UnresolvedByReason) != 2 || status.UnresolvedByReason[0] != wantReasons[0] || status.UnresolvedByReason[1] != wantReasons[1] {
		t.Fatalf("unresolved_by_reason = %#v, want %#v", status.UnresolvedByReason, wantReasons)
	}
	// The server neither opens the database nor runs the worker, so health
	// says that rather than claiming success -- or suggesting, with
	// "not_configured", that something was left unwired.
	if status.Worker.State != HealthNotApplicable || status.Storage.State != HealthNotApplicable {
		t.Fatalf("health without a probe = %#v/%#v, want %q", status.Worker, status.Storage, HealthNotApplicable)
	}
	if status.Worker.Detail == "" || status.Storage.Detail == "" {
		t.Fatalf("health = %#v/%#v, want each state to say why", status.Worker, status.Storage)
	}
	if status.LastRebuildAt != "" || status.LastUpdateAt != "" {
		t.Fatalf("host timestamps without a probe = %q/%q, want empty", status.LastRebuildAt, status.LastUpdateAt)
	}
}

func TestGraphStatusReportsHostProbeResults(t *testing.T) {
	store := graphStatusStore(t, 62)
	probe := func(context.Context) (HostStatus, error) {
		return HostStatus{
			LastRebuildAt: time.Unix(1_700_000_100, 0).UTC(),
			LastUpdateAt:  time.Unix(1_700_000_200, 0).UTC(),
			Worker:        ComponentHealth{State: "healthy", Detail: "typescript worker v1"},
			Storage:       ComponentHealth{State: "degraded", Detail: "integrity sample pending"},
		}, nil
	}

	_, response, err := graphStatus(context.Background(), nil, struct{}{}, store, probe)
	if err != nil {
		t.Fatal(err)
	}
	status := response.Results
	if status.LastRebuildAt != "2023-11-14T22:15:00Z" || status.LastUpdateAt != "2023-11-14T22:16:40Z" {
		t.Fatalf("host timestamps = %q/%q", status.LastRebuildAt, status.LastUpdateAt)
	}
	if status.Worker.State != "healthy" || status.Storage.State != "degraded" {
		t.Fatalf("health = %#v/%#v", status.Worker, status.Storage)
	}
}

// TestGraphStatusAnswersWithoutASnapshot keeps the tool usable for the one
// question it exists to answer when everything else is refusing to serve.
func TestGraphStatusAnswersWithoutASnapshot(t *testing.T) {
	for name, store := range map[string]*hotsnapshot.SnapshotStore{
		"missing store":        nil,
		"unpublished snapshot": hotsnapshot.NewSnapshotStore(nil),
	} {
		t.Run(name, func(t *testing.T) {
			_, response, err := graphStatus(context.Background(), nil, struct{}{}, store, nil)
			if err != nil {
				t.Fatalf("graphStatus() error = %v, want an empty status", err)
			}
			status := response.Results
			if status.Status != GraphStatusEmpty || status.Symbols != 0 || len(status.EdgesByKind) != 0 {
				t.Fatalf("status = %#v, want empty", status)
			}
			if response.SnapshotID != nil || response.SnapshotAgeMS != nil {
				t.Fatalf("envelope = %#v, want no snapshot identity", response)
			}
		})
	}
}

func TestGraphStatusClassifiesProbeFailure(t *testing.T) {
	probe := func(context.Context) (HostStatus, error) { return HostStatus{}, errors.New("probe timed out") }
	_, _, err := graphStatus(context.Background(), nil, struct{}{}, graphStatusStore(t, 63), probe)
	if got := ErrorCode(err); got != CodeSnapshotUnavailable {
		t.Fatalf("error code = %q, want %q (err=%v)", got, CodeSnapshotUnavailable, err)
	}
}

func TestGraphStatusIncludesConfiguredMetrics(t *testing.T) {
	registry := metrics.NewRegistry()
	snapshotID := uint64(61)
	ageMS := int64(4)
	registry.ObserveQuery(metrics.QueryObservation{
		ToolName:      "find_symbol",
		Elapsed:       2 * time.Millisecond,
		Returned:      3,
		Truncated:     true,
		SnapshotID:    &snapshotID,
		SnapshotAgeMS: &ageMS,
	})
	registry.ObserveIndex(metrics.IndexObservation{
		Duration:   7 * time.Millisecond,
		Files:      2,
		Symbols:    3,
		Edges:      2,
		Unresolved: 2,
	})
	registry.ObserveWorker(metrics.WorkerObservation{Restarts: 1, MemoryBytes: 2048})
	registry.ObserveLadybug(metrics.LadybugObservation{
		TransactionDuration: 3 * time.Millisecond,
		DatabaseBytes:       4096,
	})

	_, response, err := graphStatus(context.Background(), nil, struct{}{}, graphStatusStore(t, 61), nil, registry)
	if err != nil {
		t.Fatalf("graphStatus() error = %v", err)
	}
	if response.Results.Metrics == nil {
		t.Fatal("graph_status metrics = nil, want configured report")
	}
	report := response.Results.Metrics
	query := report.Queries["find_symbol"]
	if query.Calls != 1 || query.Results != 3 || query.Truncated != 1 || query.LatencyMax != 2*time.Millisecond {
		t.Fatalf("query metrics = %+v", query)
	}
	if report.Snapshot.ID != snapshotID || report.Snapshot.Age != 4*time.Millisecond {
		t.Fatalf("snapshot metrics = %+v", report.Snapshot)
	}
	if report.Index.Files != 2 || report.Index.Unresolved != 2 {
		t.Fatalf("index metrics = %+v", report.Index)
	}
	if report.Worker.Restarts != 1 || report.Worker.MemoryBytes != 2048 {
		t.Fatalf("worker metrics = %+v", report.Worker)
	}
	if report.Ladybug.Transactions != 1 || report.Ladybug.DatabaseBytes != 4096 {
		t.Fatalf("Ladybug metrics = %+v", report.Ladybug)
	}

	encoded, err := json.Marshal(response.Results)
	if err != nil {
		t.Fatalf("marshal graph status = %v", err)
	}
	if !strings.Contains(string(encoded), `"metrics"`) {
		t.Fatalf("graph status JSON = %s, want metrics field", encoded)
	}
}

// TestGraphStatusReportsRepositoryFreshness defends the reason graph_status is
// the first call of a session: it has to answer whether the graph still
// describes the code on disk, without a second call per repository.
func TestGraphStatusReportsRepositoryFreshness(t *testing.T) {
	current := writeGitCheckout(t, indexedFixtureCommit, "main")
	drifted := writeGitCheckout(t, movedFixtureCommit, "feature/x")
	snapshot, err := hotsnapshot.BuildGraphSnapshot(hotsnapshot.LadybugSnapshotRows{
		Repositories: []hotsnapshot.RepositoryRow{
			{Key: "repo-1", Name: "current", Path: current, Languages: "go", Commit: indexedFixtureCommit, Branch: "main"},
			{Key: "repo-2", Name: "drifted", Path: drifted, Languages: "go", Commit: indexedFixtureCommit, Branch: "main"},
		},
	}, 71, time.Unix(1_700_000_000, 0).UTC(), 1)
	if err != nil {
		t.Fatalf("BuildGraphSnapshot() error = %v", err)
	}

	_, response, err := graphStatus(
		context.Background(), nil, struct{}{}, hotsnapshot.NewSnapshotStore(snapshot), nil,
	)
	if err != nil {
		t.Fatalf("graphStatus() error = %v", err)
	}
	status := response.Results
	if status.RepositoriesMoved != 1 {
		t.Fatalf("repositories_moved = %d, want 1", status.RepositoriesMoved)
	}
	// The count of repositories in the snapshot keeps its own field: the
	// freshness array is additional information, not a replacement.
	if status.Repositories != 2 {
		t.Fatalf("repositories = %d, want the snapshot count", status.Repositories)
	}
	summaries := repositorySummariesByName(t, status.RepositoryFreshness, 2)
	if summaries["current"].Moved || summaries["current"].CurrentCommit != indexedFixtureCommit {
		t.Fatalf("current repository = %#v", summaries["current"])
	}
	moved := summaries["drifted"]
	if !moved.Moved || moved.CurrentCommit != movedFixtureCommit {
		t.Fatalf("moved repository = %#v", moved)
	}
	if !strings.Contains(moved.MovedDetail, indexedFixtureCommit[:7]) ||
		!strings.Contains(moved.MovedDetail, movedFixtureCommit[:7]) {
		t.Fatalf("moved_detail = %q, want both commits named", moved.MovedDetail)
	}
}

// TestGraphStatusReportsNoRepositoryFreshnessWithoutASnapshot keeps the empty
// answer an empty array rather than a null the client has to special-case.
func TestGraphStatusReportsNoRepositoryFreshnessWithoutASnapshot(t *testing.T) {
	_, response, err := graphStatus(context.Background(), nil, struct{}{}, nil, nil)
	if err != nil {
		t.Fatalf("graphStatus() error = %v", err)
	}
	if response.Results.RepositoryFreshness == nil || len(response.Results.RepositoryFreshness) != 0 {
		t.Fatalf("repository_freshness = %#v, want an empty array", response.Results.RepositoryFreshness)
	}
	if response.Results.RepositoriesMoved != 0 {
		t.Fatalf("repositories_moved = %d, want 0", response.Results.RepositoriesMoved)
	}
}

func BenchmarkGraphStatusWithMetrics(b *testing.B) {
	registry := metrics.NewRegistry()
	for _, name := range []string{
		"graph_status",
		"list_repositories",
		"find_symbol",
		"get_symbol",
		"find_references",
		"find_cross_repo_consumers",
		"trace_dependencies",
		"get_blast_radius",
		"get_unresolved_references",
	} {
		registry.ObserveQuery(metrics.QueryObservation{
			ToolName: name,
			Elapsed:  500 * time.Microsecond,
			Returned: 1,
		})
	}
	store := graphStatusStore(b, 61)
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		_, response, err := graphStatus(context.Background(), nil, struct{}{}, store, nil, registry)
		if err != nil || response.Results.Metrics == nil {
			b.Fatalf("graphStatus() error = %v, metrics = %#v", err, response.Results.Metrics)
		}
	}
}

func graphStatusStore(t testing.TB, id uint64) *hotsnapshot.SnapshotStore {
	t.Helper()
	snapshot, err := hotsnapshot.BuildGraphSnapshot(hotsnapshot.LadybugSnapshotRows{
		SchemaVersion:   2,
		ResolverVersion: "resolver-v7",
		Repositories: []hotsnapshot.RepositoryRow{
			{Key: "repo-go", Name: "go", Languages: "go"},
			{Key: "repo-ts", Name: "ts", Languages: "ts"},
		},
		Packages: []hotsnapshot.PackageRow{
			{Key: "pkg-go", RepositoryKey: "repo-go", Language: "go", Name: "go", ModulePath: "example.com/go"},
			{Key: "pkg-ts", RepositoryKey: "repo-ts", Language: "ts", Name: "ts", ModulePath: "@acme/ts"},
		},
		Files: []hotsnapshot.FileRow{
			{Key: "file-go", RepositoryKey: "repo-go", PackageKey: "pkg-go", Path: "main.go", Language: "go"},
			{Key: "file-ts", RepositoryKey: "repo-ts", PackageKey: "pkg-ts", Path: "index.ts", Language: "ts"},
		},
		Symbols: []hotsnapshot.SymbolRow{
			{StableKey: "sym-a", CanonicalIdentity: "go:a", FileKey: "file-go", Language: "go", Name: "A", QualifiedName: "go.A", Kind: "func"},
			{StableKey: "sym-b", CanonicalIdentity: "go:b", FileKey: "file-go", Language: "go", Name: "B", QualifiedName: "go.B", Kind: "func"},
			{StableKey: "sym-c", CanonicalIdentity: "ts:c", FileKey: "file-ts", Language: "ts", Name: "C", QualifiedName: "ts.C", Kind: "function"},
		},
		Edges: []hotsnapshot.EdgeRow{
			{SourceKey: "sym-a", TargetKey: "sym-b", Kind: facts.CodeCallsDirect, Confidence: facts.CodeExactTypechecked, Provenance: facts.CodeGoTypesUse, EvidenceKind: "types", EvidenceSourceFileKey: "file-go", EvidenceTargetFileKey: "file-go"},
			{SourceKey: "sym-c", TargetKey: "sym-a", Kind: facts.CodeReferences, Confidence: facts.CodeCandidate, Provenance: facts.CodeTreeSitterSyntax, EvidenceKind: "syntax", EvidenceSourceFileKey: "file-ts", EvidenceTargetFileKey: "file-go"},
		},
		Unresolved: []hotsnapshot.UnresolvedReferenceRow{
			{Key: "unresolved-go", RepositoryKey: "repo-go", Language: "go", RequestedPackage: "example.com/missing", Reason: "package_not_found"},
			{Key: "unresolved-ts", RepositoryKey: "repo-ts", Language: "ts", RequestedPackage: "@acme/other", Reason: "PACKAGE_PROVIDER_NOT_FOUND"},
		},
	}, id, time.Unix(1_700_000_000, 0).UTC(), 1)
	if err != nil {
		t.Fatalf("BuildGraphSnapshot() error = %v", err)
	}
	return hotsnapshot.NewSnapshotStore(snapshot)
}
