package tools

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/Luqueee/ladygraph/internal/hotsnapshot"
)

func TestGetSymbolReturnsDetailByStableKey(t *testing.T) {
	client := newGetSymbolToolClient(t, getSymbolSnapshot(t, 21))
	result, err := client.CallTool(context.Background(), &sdkmcp.CallToolParams{
		Name:      getSymbolToolName,
		Arguments: map[string]any{"stable_key": "symbol-alpha"},
	})
	if err != nil {
		t.Fatalf("get_symbol CallTool() error = %v", err)
	}
	if result.IsError {
		t.Fatalf("get_symbol CallTool() returned an error: %#v", result.Content)
	}

	data, err := json.Marshal(result.StructuredContent)
	if err != nil {
		t.Fatalf("Marshal structured content: %v", err)
	}
	var response Response[SymbolDetails]
	if err := json.Unmarshal(data, &response); err != nil {
		t.Fatalf("Unmarshal structured content: %v", err)
	}
	if response.SnapshotID == nil || *response.SnapshotID != 21 {
		t.Fatalf("snapshot_id = %#v, want 21", response.SnapshotID)
	}
	if response.SnapshotAgeMS == nil || *response.SnapshotAgeMS < 0 {
		t.Fatalf("snapshot_age_ms = %#v, want non-negative value", response.SnapshotAgeMS)
	}
	if response.Total != 1 || response.Returned != 1 || response.Truncated || response.NextCursor != nil {
		t.Fatalf("response metadata = %#v", response)
	}
	if response.Results.StableKey != "symbol-alpha" || response.Results.CanonicalIdentity != "go:alpha" {
		t.Fatalf("identity = %#v", response.Results)
	}
	if response.Results.RepositoryKey != "repo-a" || response.Results.RepositoryName != "alpha-repo" || response.Results.RepositoryPath != "/repo-a" {
		t.Fatalf("repository detail = %#v", response.Results)
	}
	if response.Results.PackageName != "pkg" || response.Results.ModulePath != "example.com/pkg" || response.Results.FilePath != "src/alpha.go" {
		t.Fatalf("containment detail = %#v", response.Results)
	}
	if response.Results.Name != "alpha" || response.Results.QualifiedName != "pkg.Alpha" || response.Results.Kind != "function" || response.Results.Signature != "func Alpha()" {
		t.Fatalf("symbol detail = %#v", response.Results)
	}
	if response.Results.StartLine != 3 || response.Results.EndLine != 9 {
		t.Fatalf("source range = %#v", response.Results)
	}
}

func TestGetSymbolClassifiesInvalidAndMissingKeys(t *testing.T) {
	store := getSymbolSnapshot(t, 22)
	cases := []struct {
		name      string
		arguments GetSymbolInput
		store     *hotsnapshot.SnapshotStore
		wantCode  string
	}{
		{name: "empty key", arguments: GetSymbolInput{}, store: store, wantCode: CodeInvalidArgument},
		{name: "surrounding whitespace", arguments: GetSymbolInput{StableKey: " symbol-alpha"}, store: store, wantCode: CodeInvalidArgument},
		{name: "missing key", arguments: GetSymbolInput{StableKey: "symbol-missing"}, store: store, wantCode: CodeSymbolNotFound},
		{name: "unpublished snapshot", arguments: GetSymbolInput{StableKey: "symbol-alpha"}, store: hotsnapshot.NewSnapshotStore(nil), wantCode: CodeIndexNotReady},
		{name: "missing store", arguments: GetSymbolInput{StableKey: "symbol-alpha"}, wantCode: CodeIndexNotReady},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			_, _, err := getSymbol(context.Background(), nil, test.arguments, test.store)
			if got := ErrorCode(err); got != test.wantCode {
				t.Fatalf("error code = %q, want %q (err=%v)", got, test.wantCode, err)
			}
		})
	}
}

func TestGetSymbolIsRegisteredReadOnly(t *testing.T) {
	server := sdkmcp.NewServer(&sdkmcp.Implementation{Name: "test", Version: "0.0.1"}, nil)
	RegisterGetSymbol(server)
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
		if tool.Name == getSymbolToolName {
			if tool.Annotations == nil || !tool.Annotations.ReadOnlyHint {
				t.Fatalf("get_symbol annotations = %#v, want read-only", tool.Annotations)
			}
			return
		}
	}
	t.Fatal("get_symbol is not registered")
}

func newGetSymbolToolClient(t *testing.T, store *hotsnapshot.SnapshotStore) *sdkmcp.ClientSession {
	t.Helper()
	server := sdkmcp.NewServer(&sdkmcp.Implementation{Name: "test", Version: "0.0.1"}, nil)
	RegisterGetSymbolWithSnapshotStore(server, store)
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

func getSymbolSnapshot(t *testing.T, id uint64) *hotsnapshot.SnapshotStore {
	t.Helper()
	snapshot, err := hotsnapshot.BuildGraphSnapshot(
		hotsnapshot.LadybugSnapshotRows{
			Repositories: []hotsnapshot.RepositoryRow{{Key: "repo-a", Name: "alpha-repo", Path: "/repo-a", Languages: "go"}},
			Packages:     []hotsnapshot.PackageRow{{Key: "package-a", RepositoryKey: "repo-a", Name: "pkg", ModulePath: "example.com/pkg"}},
			Files:        []hotsnapshot.FileRow{{Key: "file-a", RepositoryKey: "repo-a", PackageKey: "package-a", Path: "src/alpha.go"}},
			Symbols: []hotsnapshot.SymbolRow{{
				StableKey: "symbol-alpha", CanonicalIdentity: "go:alpha", FileKey: "file-a",
				Name: "alpha", QualifiedName: "pkg.Alpha", Kind: "function", Signature: "func Alpha()",
				StartLine: 3, EndLine: 9,
			}},
		},
		id,
		time.Unix(1_700_000_000+int64(id), 0).UTC(),
		1,
	)
	if err != nil {
		t.Fatalf("BuildGraphSnapshot() error = %v", err)
	}
	return hotsnapshot.NewSnapshotStore(snapshot)
}
