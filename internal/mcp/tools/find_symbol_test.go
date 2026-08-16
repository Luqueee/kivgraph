package tools

import (
	"context"
	"testing"
	"time"

	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/Luqueee/kivgraph/internal/hotsnapshot"
)

func TestFindSymbolModesAndPagination(t *testing.T) {
	client := newSymbolToolClient(t, symbolSnapshot(t, 11))

	exact := callFindSymbol(t, client, map[string]any{"name": "alpha", "mode": FindSymbolModeExact})
	if exact.Total != 1 || exact.Returned != 1 || exact.Truncated || exact.NextCursor != nil {
		t.Fatalf("exact page metadata = %#v", exact)
	}
	if exact.Results[0].StableKey != "symbol-alpha" || exact.Results[0].QualifiedName != "pkg.Alpha" {
		t.Fatalf("exact result = %#v", exact.Results[0])
	}

	qualified := callFindSymbol(t, client, map[string]any{"name": "pkg.Alpha", "mode": FindSymbolModeQualifiedExact})
	if qualified.Total != 1 || qualified.Results[0].StableKey != "symbol-alpha" {
		t.Fatalf("qualified_exact result = %#v", qualified)
	}

	firstPrefix := callFindSymbol(t, client, map[string]any{"name": "alp", "mode": FindSymbolModePrefix, "limit": 1})
	if firstPrefix.Total != 2 || firstPrefix.Returned != 1 || !firstPrefix.Truncated || firstPrefix.NextCursor == nil {
		t.Fatalf("first prefix page = %#v", firstPrefix)
	}
	if firstPrefix.Results[0].Name != "alpha" {
		t.Fatalf("first prefix result = %#v", firstPrefix.Results[0])
	}
	secondPrefix := callFindSymbol(t, client, map[string]any{
		"name": "alp", "mode": FindSymbolModePrefix, "limit": 1, "cursor": *firstPrefix.NextCursor,
	})
	if secondPrefix.Total != 2 || secondPrefix.Returned != 1 || secondPrefix.Truncated || secondPrefix.NextCursor != nil {
		t.Fatalf("second prefix page = %#v", secondPrefix)
	}
	if secondPrefix.Results[0].Name != "alphabet" {
		t.Fatalf("second prefix result = %#v", secondPrefix.Results[0])
	}

	ambiguous := callFindSymbol(t, client, map[string]any{"name": "shared"})
	if ambiguous.Total != 2 || ambiguous.Returned != 2 || ambiguous.Truncated {
		t.Fatalf("ambiguous exact page = %#v", ambiguous)
	}
	if ambiguous.Results[0].StableKey != "symbol-shared-a" || ambiguous.Results[1].StableKey != "symbol-shared-b" {
		t.Fatalf("ambiguous exact ordering = %#v", ambiguous.Results)
	}

	missing := callFindSymbol(t, client, map[string]any{"name": "missing"})
	if missing.Total != 0 || missing.Returned != 0 || missing.Results == nil || missing.Truncated {
		t.Fatalf("missing exact page = %#v", missing)
	}
}

func TestFindSymbolRejectsUnsupportedModeAndInvalidName(t *testing.T) {
	store := symbolSnapshot(t, 11)
	if _, _, err := findSymbol(context.Background(), nil, FindSymbolInput{Name: "alpha", Mode: "fuzzy"}, store); ErrorCode(err) != CodeInvalidArgument {
		t.Fatalf("unsupported mode error code = %q, want %q", ErrorCode(err), CodeInvalidArgument)
	}
	if _, _, err := findSymbol(context.Background(), nil, FindSymbolInput{Name: " alpha "}, store); ErrorCode(err) != CodeInvalidArgument {
		t.Fatalf("invalid name error code = %q, want %q", ErrorCode(err), CodeInvalidArgument)
	}
	if _, _, err := findSymbol(context.Background(), nil, FindSymbolInput{Name: "alpha", Limit: MaximumSymbolLimit + 1}, store); ErrorCode(err) != CodeInvalidArgument {
		t.Fatalf("invalid limit error code = %q, want %q", ErrorCode(err), CodeInvalidArgument)
	}
	if _, _, err := findSymbol(context.Background(), nil, FindSymbolInput{Name: "alpha"}, hotsnapshot.NewSnapshotStore(nil)); ErrorCode(err) != CodeIndexNotReady {
		t.Fatalf("unpublished snapshot error code = %q, want %q", ErrorCode(err), CodeIndexNotReady)
	}
}

func TestFindSymbolCursorExpiresAfterSnapshotPublication(t *testing.T) {
	store := symbolSnapshot(t, 11)
	client := newSymbolToolClient(t, store)
	first := callFindSymbol(t, client, map[string]any{"name": "alp", "mode": FindSymbolModePrefix, "limit": 1})
	if first.NextCursor == nil {
		t.Fatal("first page has no cursor")
	}
	secondSnapshot := buildSymbolSnapshot(t, 12)
	if err := store.Publish(secondSnapshot); err != nil {
		t.Fatalf("SnapshotStore.Publish() error = %v", err)
	}
	result, err := client.CallTool(context.Background(), &sdkmcp.CallToolParams{
		Name: "find_symbol", Arguments: map[string]any{"name": "alp", "mode": FindSymbolModePrefix, "limit": 1, "cursor": *first.NextCursor},
	})
	if err != nil {
		t.Fatalf("expired cursor CallTool() error = %v", err)
	}
	if !result.IsError {
		t.Fatalf("expired cursor result = %#v, want an error", result)
	}
}

func callFindSymbol(t *testing.T, client *sdkmcp.ClientSession, arguments map[string]any) Response[[]SymbolSummary] {
	t.Helper()
	result, err := client.CallTool(context.Background(), &sdkmcp.CallToolParams{Name: "find_symbol", Arguments: arguments})
	if err != nil {
		t.Fatalf("find_symbol CallTool() error = %v", err)
	}
	if result.IsError {
		t.Fatalf("find_symbol CallTool() returned an error: %#v", result.Content)
	}
	return decodeResponse[[]SymbolSummary](t, result)
}

func newSymbolToolClient(t *testing.T, store *hotsnapshot.SnapshotStore) *sdkmcp.ClientSession {
	t.Helper()
	server := sdkmcp.NewServer(&sdkmcp.Implementation{Name: "test", Version: "0.0.1"}, nil)
	RegisterFindSymbolWithSnapshotStore(server, store)
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

func symbolSnapshot(t *testing.T, id uint64) *hotsnapshot.SnapshotStore {
	t.Helper()
	return hotsnapshot.NewSnapshotStore(buildSymbolSnapshot(t, id))
}

func buildSymbolSnapshot(t *testing.T, id uint64) *hotsnapshot.GraphSnapshot {
	t.Helper()
	snapshot, err := hotsnapshot.BuildGraphSnapshot(
		hotsnapshot.LadybugSnapshotRows{
			Repositories: []hotsnapshot.RepositoryRow{{Key: "repo-a", Name: "repo-a", Path: "/repo-a", Languages: "go"}},
			Packages:     []hotsnapshot.PackageRow{{Key: "package-a", RepositoryKey: "repo-a", Name: "pkg", ModulePath: "example.com/pkg"}},
			Files:        []hotsnapshot.FileRow{{Key: "file-a", RepositoryKey: "repo-a", PackageKey: "package-a", Path: "alpha.go"}},
			Symbols: []hotsnapshot.SymbolRow{
				{StableKey: "symbol-shared-b", CanonicalIdentity: "go:shared:b", FileKey: "file-a", Name: "shared", QualifiedName: "pkg.SharedB", Kind: "function", Signature: "func SharedB()"},
				{StableKey: "symbol-alpha", CanonicalIdentity: "go:alpha", FileKey: "file-a", Name: "alpha", QualifiedName: "pkg.Alpha", Kind: "function", Signature: "func Alpha()"},
				{StableKey: "symbol-shared-a", CanonicalIdentity: "go:shared:a", FileKey: "file-a", Name: "shared", QualifiedName: "pkg.SharedA", Kind: "function", Signature: "func SharedA()"},
				{StableKey: "symbol-alphabet", CanonicalIdentity: "go:alphabet", FileKey: "file-a", Name: "alphabet", QualifiedName: "pkg.Alphabet", Kind: "function", Signature: "func Alphabet()"},
			},
		},
		id,
		time.Unix(1_700_000_000+int64(id), 0).UTC(),
		1,
	)
	if err != nil {
		t.Fatalf("BuildGraphSnapshot() error = %v", err)
	}
	return snapshot
}
