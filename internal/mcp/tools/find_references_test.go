package tools

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/Luqueee/ladygraph/internal/facts"
	"github.com/Luqueee/ladygraph/internal/hotsnapshot"
)

func TestFindReferencesDirectionsFiltersAndPagination(t *testing.T) {
	client := newFindReferencesToolClient(t, referenceSnapshot(t, 31))

	first := callFindReferences(t, client, map[string]any{
		"stable_key": "symbol-target", "direction": FindReferencesDirectionIncoming, "limit": 1,
	})
	if first.Total != 2 || first.Returned != 1 || !first.Truncated || first.NextCursor == nil {
		t.Fatalf("first incoming page = %#v", first)
	}
	if first.Coverage != (Coverage{Exact: 1, Candidate: 1}) {
		t.Fatalf("incoming coverage = %#v", first.Coverage)
	}
	if first.Results[0].SourceKey != "symbol-caller-a" || first.Results[0].TargetKey != "symbol-target" {
		t.Fatalf("first incoming result = %#v", first.Results[0])
	}
	if first.Results[0].Kind != string(facts.References) || first.Results[0].Confidence != string(facts.ExactTypechecked) || first.Results[0].EvidenceKind != "checker" {
		t.Fatalf("first incoming relation = %#v", first.Results[0])
	}
	if first.Results[0].SourceRepositoryKey != "repo-a" || first.Results[0].SourceLanguage != "go" || first.Results[0].SourceFileKey != "file-a" || first.Results[0].SourceFilePath != "src/caller.go" {
		t.Fatalf("first incoming location = %#v", first.Results[0])
	}

	second := callFindReferences(t, client, map[string]any{
		"stable_key": "symbol-target", "direction": FindReferencesDirectionIncoming,
		"limit": 1, "cursor": *first.NextCursor,
	})
	if second.Total != 2 || second.Returned != 1 || second.Truncated || second.NextCursor != nil {
		t.Fatalf("second incoming page = %#v", second)
	}
	if second.Results[0].SourceKey != "symbol-caller-b" || second.Results[0].Kind != string(facts.CallsDirect) {
		t.Fatalf("second incoming result = %#v", second.Results[0])
	}

	outgoing := callFindReferences(t, client, map[string]any{
		"stable_key": "symbol-target", "direction": FindReferencesDirectionOutgoing,
	})
	if outgoing.Total != 1 || outgoing.Returned != 1 || outgoing.Results[0].TargetKey != "symbol-result" {
		t.Fatalf("outgoing result = %#v", outgoing)
	}

	filtered := callFindReferences(t, client, map[string]any{
		"stable_key": "symbol-target", "direction": FindReferencesDirectionIncoming,
		"repo": "repo-b", "language": "typescript", "edge_kinds": []string{string(facts.CallsDirect)},
		"confidence": string(facts.Candidate),
	})
	if filtered.Total != 1 || filtered.Returned != 1 || filtered.Results[0].SourceKey != "symbol-caller-b" {
		t.Fatalf("filtered result = %#v", filtered)
	}
}

func TestFindReferencesClassifiesInvalidInputAndMissingSnapshot(t *testing.T) {
	store := referenceSnapshot(t, 32)
	cases := []struct {
		name      string
		arguments FindReferencesInput
		store     *hotsnapshot.SnapshotStore
		wantCode  string
	}{
		{name: "empty key", arguments: FindReferencesInput{}, store: store, wantCode: CodeInvalidArgument},
		{name: "unsupported direction", arguments: FindReferencesInput{StableKey: "symbol-target", Direction: "sideways"}, store: store, wantCode: CodeInvalidArgument},
		{name: "invalid edge kind", arguments: FindReferencesInput{StableKey: "symbol-target", EdgeKinds: []string{"DEFINES"}}, store: store, wantCode: CodeInvalidArgument},
		{name: "invalid confidence", arguments: FindReferencesInput{StableKey: "symbol-target", Confidence: "MAYBE"}, store: store, wantCode: CodeInvalidArgument},
		{name: "invalid limit", arguments: FindReferencesInput{StableKey: "symbol-target", Limit: MaximumReferenceLimit + 1}, store: store, wantCode: CodeInvalidArgument},
		{name: "missing symbol", arguments: FindReferencesInput{StableKey: "symbol-missing"}, store: store, wantCode: CodeSymbolNotFound},
		{name: "missing snapshot", arguments: FindReferencesInput{StableKey: "symbol-target"}, store: hotsnapshot.NewSnapshotStore(nil), wantCode: CodeIndexNotReady},
		{name: "missing store", arguments: FindReferencesInput{StableKey: "symbol-target"}, wantCode: CodeIndexNotReady},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			_, _, err := findReferences(context.Background(), nil, test.arguments, test.store)
			if got := ErrorCode(err); got != test.wantCode {
				t.Fatalf("error code = %q, want %q (err=%v)", got, test.wantCode, err)
			}
		})
	}
}

func TestFindReferencesIsRegisteredReadOnly(t *testing.T) {
	server := sdkmcp.NewServer(&sdkmcp.Implementation{Name: "test", Version: "0.0.1"}, nil)
	RegisterFindReferences(server)
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
		if tool.Name == findReferencesToolName {
			if tool.Annotations == nil || !tool.Annotations.ReadOnlyHint {
				t.Fatalf("find_references annotations = %#v, want read-only", tool.Annotations)
			}
			return
		}
	}
	t.Fatal("find_references is not registered")
}

func callFindReferences(t *testing.T, client *sdkmcp.ClientSession, arguments map[string]any) Response[[]ReferenceSummary] {
	t.Helper()
	result, err := client.CallTool(context.Background(), &sdkmcp.CallToolParams{Name: findReferencesToolName, Arguments: arguments})
	if err != nil {
		t.Fatalf("find_references CallTool() error = %v", err)
	}
	if result.IsError {
		t.Fatalf("find_references CallTool() returned an error: %#v", result.Content)
	}
	data, err := json.Marshal(result.StructuredContent)
	if err != nil {
		t.Fatalf("Marshal structured content: %v", err)
	}
	var response Response[[]ReferenceSummary]
	if err := json.Unmarshal(data, &response); err != nil {
		t.Fatalf("Unmarshal structured content: %v", err)
	}
	return response
}

func newFindReferencesToolClient(t *testing.T, store *hotsnapshot.SnapshotStore) *sdkmcp.ClientSession {
	t.Helper()
	server := sdkmcp.NewServer(&sdkmcp.Implementation{Name: "test", Version: "0.0.1"}, nil)
	RegisterFindReferencesWithSnapshotStore(server, store)
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
	return clientSession
}

func referenceSnapshot(t *testing.T, id uint64) *hotsnapshot.SnapshotStore {
	t.Helper()
	code := func(value uint8) uint8 { return value }
	rows := hotsnapshot.LadybugSnapshotRows{
		Repositories: []hotsnapshot.RepositoryRow{
			{Key: "repo-a", Name: "repo-a", Path: "/repo-a", Languages: "go"},
			{Key: "repo-b", Name: "repo-b", Path: "/repo-b", Languages: "typescript"},
		},
		Packages: []hotsnapshot.PackageRow{
			{Key: "package-a", RepositoryKey: "repo-a", Name: "pkg-a", ModulePath: "example.com/a"},
			{Key: "package-b", RepositoryKey: "repo-b", Name: "pkg-b", ModulePath: "example.com/b"},
		},
		Files: []hotsnapshot.FileRow{
			{Key: "file-a", RepositoryKey: "repo-a", PackageKey: "package-a", Path: "src/caller.go", Language: "go"},
			{Key: "file-b", RepositoryKey: "repo-b", PackageKey: "package-b", Path: "src/caller.ts", Language: "typescript"},
			{Key: "file-result", RepositoryKey: "repo-a", PackageKey: "package-a", Path: "src/result.go", Language: "go"},
		},
		Symbols: []hotsnapshot.SymbolRow{
			{StableKey: "symbol-caller-a", CanonicalIdentity: "go:caller-a", FileKey: "file-a", Language: "go", Name: "callerA", QualifiedName: "pkg.CallerA", Kind: "function"},
			{StableKey: "symbol-caller-b", CanonicalIdentity: "ts:caller-b", FileKey: "file-b", Language: "typescript", Name: "callerB", QualifiedName: "pkg.CallerB", Kind: "function"},
			{StableKey: "symbol-result", CanonicalIdentity: "go:result", FileKey: "file-result", Language: "go", Name: "result", QualifiedName: "pkg.Result", Kind: "function"},
			{StableKey: "symbol-target", CanonicalIdentity: "go:target", FileKey: "file-a", Language: "go", Name: "target", QualifiedName: "pkg.Target", Kind: "function"},
		},
		Edges: []hotsnapshot.EdgeRow{
			{SourceKey: "symbol-caller-a", TargetKey: "symbol-target", Kind: code(mustFactsEdgeCode(t, facts.References)), Confidence: code(mustFactsConfidenceCode(t, facts.ExactTypechecked)), Provenance: code(mustFactsProvenanceCode(t, facts.GoTypesUse)), EvidenceKind: "checker", EvidenceSourceFileKey: "file-a", EvidenceTargetFileKey: "file-a"},
			{SourceKey: "symbol-caller-b", TargetKey: "symbol-target", Kind: code(mustFactsEdgeCode(t, facts.CallsDirect)), Confidence: code(mustFactsConfidenceCode(t, facts.Candidate)), Provenance: code(mustFactsProvenanceCode(t, facts.GoASTCall)), EvidenceKind: "checker", EvidenceSourceFileKey: "file-b", EvidenceTargetFileKey: "file-a"},
			{SourceKey: "symbol-target", TargetKey: "symbol-result", Kind: code(mustFactsEdgeCode(t, facts.Exports)), Confidence: code(mustFactsConfidenceCode(t, facts.ExactDeclarationMapped)), Provenance: code(mustFactsProvenanceCode(t, facts.GoTypesDefinition)), EvidenceKind: "declaration", EvidenceSourceFileKey: "file-a", EvidenceTargetFileKey: "file-result"},
		},
	}
	snapshot, err := hotsnapshot.BuildGraphSnapshot(rows, id, time.Unix(1_700_000_000+int64(id), 0).UTC(), 1)
	if err != nil {
		t.Fatalf("BuildGraphSnapshot() error = %v", err)
	}
	return hotsnapshot.NewSnapshotStore(snapshot)
}

func mustFactsEdgeCode(t *testing.T, kind facts.EdgeKind) uint8 {
	t.Helper()
	code, err := kind.Code()
	if err != nil {
		t.Fatalf("edge kind code: %v", err)
	}
	return code
}

func mustFactsConfidenceCode(t *testing.T, confidence facts.Confidence) uint8 {
	t.Helper()
	code, err := confidence.Code()
	if err != nil {
		t.Fatalf("confidence code: %v", err)
	}
	return code
}

func mustFactsProvenanceCode(t *testing.T, provenance facts.Provenance) uint8 {
	t.Helper()
	code, err := provenance.Code()
	if err != nil {
		t.Fatalf("provenance code: %v", err)
	}
	return code
}
