package tools

import (
	"context"
	"strings"
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
	// The subject is stated once, not on every row, and each row names the
	// other end so the agent can open it without a second call.
	if first.Results.Subject.QualifiedName != "pkg.Target" || first.Results.Subject.Name != "target" {
		t.Fatalf("subject = %#v", first.Results.Subject)
	}
	if first.Results.Direction != FindReferencesDirectionIncoming {
		t.Fatalf("direction = %q", first.Results.Direction)
	}
	caller := first.Results.References[0]
	if caller.QualifiedName != "pkg.CallerA" || caller.Name != "callerA" || caller.Kind != "function" {
		t.Fatalf("first incoming result = %#v", caller)
	}
	if caller.EdgeKind != string(facts.References) || caller.Confidence != string(facts.ExactTypechecked) {
		t.Fatalf("first incoming relation = %#v", caller)
	}
	// The concise row omits the evidence kind, the stable key and the derived
	// keys: a caller addresses the next call with repository, path and
	// qualified name, all of which the row carries.
	if caller.EvidenceKind != "" || caller.StableKey != "" || caller.FileKey != "" || caller.RepositoryKey != "" {
		t.Fatalf("concise row carries derived fields = %#v", caller)
	}
	if caller.Language != "go" || caller.FilePath != "src/caller.go" || caller.Repository != "repo-a" {
		t.Fatalf("first incoming location = %#v", caller)
	}
	// The row has to be openable as it stands. Without EndLine the agent pays
	// one get_symbol per row before it can read anything: 15 of those over the
	// six questions of benchmarks/mcp-token-cost.
	if caller.StartLine == 0 || caller.EndLine < caller.StartLine {
		t.Fatalf("first incoming range = %d-%d, want a declaration range", caller.StartLine, caller.EndLine)
	}

	detailed := callFindReferences(t, client, map[string]any{
		"stable_key": "symbol-target", "direction": FindReferencesDirectionIncoming, "limit": 1,
		"response_format": ResponseFormatDetailed,
	})
	if detailed.Results.References[0].EvidenceKind != "checker" ||
		detailed.Results.References[0].FileKey != "file-a" ||
		detailed.Results.References[0].RepositoryKey != facts.RepositoryKey("repo-a") {
		t.Fatalf("detailed row = %#v", detailed.Results.References[0])
	}

	second := callFindReferences(t, client, map[string]any{
		"stable_key": "symbol-target", "direction": FindReferencesDirectionIncoming,
		"limit": 1, "cursor": *first.NextCursor,
	})
	if second.Total != 2 || second.Returned != 1 || second.Truncated || second.NextCursor != nil {
		t.Fatalf("second incoming page = %#v", second)
	}
	if second.Results.References[0].QualifiedName != "pkg.CallerB" || second.Results.References[0].EdgeKind != string(facts.CallsDirect) {
		t.Fatalf("second incoming result = %#v", second.Results.References[0])
	}

	outgoing := callFindReferences(t, client, map[string]any{
		"stable_key": "symbol-target", "direction": FindReferencesDirectionOutgoing,
	})
	if outgoing.Total != 1 || outgoing.Returned != 1 || outgoing.Results.References[0].QualifiedName != "pkg.Result" {
		t.Fatalf("outgoing result = %#v", outgoing)
	}
	if outgoing.Results.Subject.QualifiedName != "pkg.Target" {
		t.Fatalf("outgoing subject = %#v", outgoing.Results.Subject)
	}

	filtered := callFindReferences(t, client, map[string]any{
		"stable_key": "symbol-target", "direction": FindReferencesDirectionIncoming,
		"repo": "repo-b", "language": "typescript", "edge_kinds": []string{string(facts.CallsDirect)},
		"confidence": string(facts.Candidate),
	})
	if filtered.Total != 1 || filtered.Returned != 1 || filtered.Results.References[0].QualifiedName != "pkg.CallerB" {
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

func callFindReferences(t *testing.T, client *sdkmcp.ClientSession, arguments map[string]any) Response[ReferenceResult] {
	t.Helper()
	result, err := client.CallTool(context.Background(), &sdkmcp.CallToolParams{Name: findReferencesToolName, Arguments: arguments})
	if err != nil {
		t.Fatalf("find_references CallTool() error = %v", err)
	}
	if result.IsError {
		t.Fatalf("find_references CallTool() returned an error: %#v", result.Content)
	}
	response := decodeResponse[ReferenceResult](t, result)
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
			{StableKey: "symbol-caller-a", CanonicalIdentity: "go:caller-a", FileKey: "file-a", Language: "go", Name: "callerA", QualifiedName: "pkg.CallerA", Kind: "function", StartLine: 12, EndLine: 20},
			{StableKey: "symbol-caller-b", CanonicalIdentity: "ts:caller-b", FileKey: "file-b", Language: "typescript", Name: "callerB", QualifiedName: "pkg.CallerB", Kind: "function", StartLine: 4, EndLine: 9},
			{StableKey: "symbol-result", CanonicalIdentity: "go:result", FileKey: "file-result", Language: "go", Name: "result", QualifiedName: "pkg.Result", Kind: "function", StartLine: 30, EndLine: 34},
			{StableKey: "symbol-target", CanonicalIdentity: "go:target", FileKey: "file-a", Language: "go", Name: "target", QualifiedName: "pkg.Target", Kind: "function", StartLine: 40, EndLine: 55},
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

// TestFindReferencesAddressesASymbolWithoutItsKey is the contract that makes it
// affordable for every list response to withhold stable keys: a caller must be
// able to name a symbol with the repository, path and qualified name it just
// read, and get the same answer the key gives.
func TestFindReferencesAddressesASymbolWithoutItsKey(t *testing.T) {
	client := newFindReferencesToolClient(t, referenceSnapshot(t, 33))

	byKey := callFindReferences(t, client, map[string]any{
		"stable_key": "symbol-target", "direction": FindReferencesDirectionIncoming,
	})
	byTriple := callFindReferences(t, client, map[string]any{
		"repository": "repo-a", "path": "src/caller.go", "qualified_name": "pkg.Target",
		"direction": FindReferencesDirectionIncoming,
	})
	if byTriple.Total != byKey.Total || byTriple.Results.Subject.QualifiedName != byKey.Results.Subject.QualifiedName {
		t.Fatalf("triple answered %#v, want the same subject and total as the key: %#v", byTriple.Results, byKey.Results)
	}
	// The qualified name alone is enough when it is unique in the graph.
	byName := callFindReferences(t, client, map[string]any{
		"qualified_name": "pkg.Target", "direction": FindReferencesDirectionIncoming,
	})
	if byName.Total != byKey.Total {
		t.Fatalf("qualified name answered %d references, want %d", byName.Total, byKey.Total)
	}
}

// TestFindReferencesRejectsContradictoryAndAmbiguousSelectors covers the two
// ways a selector fails. Neither may be resolved quietly: picking one of two
// symbols with the same name is the nominal coincidence the graph forbids.
func TestFindReferencesRejectsContradictoryAndAmbiguousSelectors(t *testing.T) {
	client := newFindReferencesToolClient(t, referenceSnapshot(t, 34))

	for name, arguments := range map[string]map[string]any{
		"both selectors": {"stable_key": "symbol-target", "qualified_name": "pkg.Target"},
		"key narrowed":   {"stable_key": "symbol-target", "repository": "repo-a"},
		"path alone":     {"qualified_name": "pkg.Target", "path": "src/caller.go"},
		"no selector":    {"direction": FindReferencesDirectionIncoming},
	} {
		result, err := client.CallTool(context.Background(), &sdkmcp.CallToolParams{
			Name: findReferencesToolName, Arguments: arguments,
		})
		if err != nil {
			t.Fatalf("%s: CallTool() error = %v", name, err)
		}
		if !result.IsError {
			t.Fatalf("%s: result = %#v, want a classified argument error", name, result)
		}
	}

	// A name the narrowing excluded is not the same answer as a name nobody
	// declares, and the message says which one happened.
	missing, err := client.CallTool(context.Background(), &sdkmcp.CallToolParams{
		Name:      findReferencesToolName,
		Arguments: map[string]any{"repository": "repo-a", "path": "src/result.go", "qualified_name": "pkg.Target"},
	})
	if err != nil {
		t.Fatalf("CallTool() error = %v", err)
	}
	if !missing.IsError || !strings.Contains(contentText(missing), "search the whole graph") {
		t.Fatalf("narrowed miss = %#v, want a message naming the wider search", missing.Content)
	}
}

func contentText(result *sdkmcp.CallToolResult) string {
	for _, content := range result.Content {
		if text, ok := content.(*sdkmcp.TextContent); ok {
			return text.Text
		}
	}
	return ""
}
