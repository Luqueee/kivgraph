package tools

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/Luqueee/ladygraph/internal/hotsnapshot"
)

func TestListRepositoriesReturnsStableEmptyPage(t *testing.T) {
	ctx := context.Background()
	server := sdkmcp.NewServer(&sdkmcp.Implementation{Name: "test", Version: "0.0.1"}, nil)
	RegisterListRepositories(server)

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
		if candidate.Name == "list_repositories" {
			tool = candidate
			break
		}
	}
	if tool == nil {
		t.Fatal("list_repositories is not registered")
	}
	if tool.Annotations == nil || !tool.Annotations.ReadOnlyHint {
		t.Fatalf("list_repositories annotations = %#v, want read-only", tool.Annotations)
	}

	result, err := clientSession.CallTool(ctx, &sdkmcp.CallToolParams{Name: "list_repositories"})
	if err != nil {
		t.Fatalf("CallTool() error = %v", err)
	}
	if result.IsError {
		t.Fatalf("CallTool() returned an error result: %#v", result.Content)
	}

	data, err := json.Marshal(result.StructuredContent)
	if err != nil {
		t.Fatalf("Marshal structured content: %v", err)
	}
	var response Response[[]RepositorySummary]
	if err := json.Unmarshal(data, &response); err != nil {
		t.Fatalf("Unmarshal structured content: %v", err)
	}
	if response.Results == nil || len(response.Results) != 0 {
		t.Fatalf("repositories = %#v, want empty array", response.Results)
	}
	if response.Total != 0 || response.Returned != 0 || response.Truncated {
		t.Fatalf("page metadata = %#v, want zero non-truncated page", response)
	}
	if response.SnapshotID != nil || response.SnapshotAgeMS != nil || response.NextCursor != nil {
		t.Fatalf("optional metadata = %#v, want nil for empty graph", response)
	}
}

func TestListRepositoriesUsesPublishedEmptySnapshot(t *testing.T) {
	client := newRepositoryToolClient(t, repositorySnapshot(t, 7))
	result, err := client.CallTool(context.Background(), &sdkmcp.CallToolParams{Name: "list_repositories"})
	if err != nil {
		t.Fatalf("CallTool() error = %v", err)
	}
	if result.IsError {
		t.Fatalf("CallTool() returned an error result: %#v", result.Content)
	}
	response := decodeRepositoryResponse(t, result)
	if response.Results == nil || len(response.Results) != 0 {
		t.Fatalf("repositories = %#v, want empty array", response.Results)
	}
	if response.Total != 0 || response.Returned != 0 || response.Truncated || response.NextCursor != nil {
		t.Fatalf("empty page metadata = %#v", response)
	}
	if response.SnapshotID == nil || *response.SnapshotID != 7 {
		t.Fatalf("snapshot_id = %#v, want 7", response.SnapshotID)
	}
	if response.SnapshotAgeMS == nil || *response.SnapshotAgeMS < 0 {
		t.Fatalf("snapshot_age_ms = %#v, want non-negative value", response.SnapshotAgeMS)
	}
}

func TestListRepositoriesPaginatesPublishedRepositoriesByStableKey(t *testing.T) {
	client := newRepositoryToolClient(t, repositorySnapshot(t, 8,
		hotsnapshot.RepositoryRow{Key: "repo-c", Name: "gamma", Path: "/gamma", Languages: "typescript"},
		hotsnapshot.RepositoryRow{Key: "repo-a", Name: "alpha", Path: "/alpha", Languages: "go,typescript"},
		hotsnapshot.RepositoryRow{Key: "repo-b", Name: "beta", Path: "/beta", Languages: "go"},
	))

	first, err := client.CallTool(context.Background(), &sdkmcp.CallToolParams{
		Name:      "list_repositories",
		Arguments: map[string]any{"limit": 2},
	})
	if err != nil {
		t.Fatalf("first CallTool() error = %v", err)
	}
	if first.IsError {
		t.Fatalf("first CallTool() returned an error result: %#v", first.Content)
	}
	firstResponse := decodeRepositoryResponse(t, first)
	if firstResponse.Total != 3 || firstResponse.Returned != 2 || !firstResponse.Truncated || firstResponse.NextCursor == nil {
		t.Fatalf("first page metadata = %#v", firstResponse)
	}
	if got := []string{firstResponse.Results[0].Name, firstResponse.Results[1].Name}; !equalStrings(got, []string{"alpha", "beta"}) {
		t.Fatalf("first page names = %v, want alpha,beta", got)
	}
	if got := firstResponse.Results[0].Languages; !equalStrings(got, []string{"go", "typescript"}) {
		t.Fatalf("alpha languages = %v, want go,typescript", got)
	}

	second, err := client.CallTool(context.Background(), &sdkmcp.CallToolParams{
		Name:      "list_repositories",
		Arguments: map[string]any{"cursor": *firstResponse.NextCursor, "limit": 2},
	})
	if err != nil {
		t.Fatalf("second CallTool() error = %v", err)
	}
	if second.IsError {
		t.Fatalf("second CallTool() returned an error result: %#v", second.Content)
	}
	secondResponse := decodeRepositoryResponse(t, second)
	if secondResponse.Total != 3 || secondResponse.Returned != 1 || secondResponse.Truncated || secondResponse.NextCursor != nil {
		t.Fatalf("second page metadata = %#v", secondResponse)
	}
	if len(secondResponse.Results) != 1 || secondResponse.Results[0].Name != "gamma" || secondResponse.Results[0].Path != "/gamma" {
		t.Fatalf("second page repositories = %#v, want gamma", secondResponse.Results)
	}

	invalid, err := client.CallTool(context.Background(), &sdkmcp.CallToolParams{
		Name: "list_repositories", Arguments: map[string]any{"limit": MaximumRepositoryLimit + 1},
	})
	if err != nil {
		t.Fatalf("invalid-limit CallTool() error = %v", err)
	}
	if !invalid.IsError {
		t.Fatalf("invalid-limit CallTool() = %#v, want an error result", invalid)
	}
}

func repositorySnapshot(t *testing.T, id uint64, repositories ...hotsnapshot.RepositoryRow) *hotsnapshot.SnapshotStore {
	t.Helper()
	snapshot, err := hotsnapshot.BuildGraphSnapshot(
		hotsnapshot.LadybugSnapshotRows{Repositories: repositories},
		id,
		time.Unix(1_700_000_000+int64(id), 0).UTC(),
		1,
	)
	if err != nil {
		t.Fatalf("BuildGraphSnapshot() error = %v", err)
	}
	return hotsnapshot.NewSnapshotStore(snapshot)
}

func newRepositoryToolClient(t *testing.T, store *hotsnapshot.SnapshotStore) *sdkmcp.ClientSession {
	t.Helper()
	server := sdkmcp.NewServer(&sdkmcp.Implementation{Name: "test", Version: "0.0.1"}, nil)
	RegisterListRepositoriesWithSnapshotStore(server, store)
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

func decodeRepositoryResponse(t *testing.T, result *sdkmcp.CallToolResult) Response[[]RepositorySummary] {
	t.Helper()
	data, err := json.Marshal(result.StructuredContent)
	if err != nil {
		t.Fatalf("Marshal structured content: %v", err)
	}
	var response Response[[]RepositorySummary]
	if err := json.Unmarshal(data, &response); err != nil {
		t.Fatalf("Unmarshal structured content: %v", err)
	}
	return response
}

func equalStrings(left, right []string) bool {
	if len(left) != len(right) {
		return false
	}
	for index := range left {
		if left[index] != right[index] {
			return false
		}
	}
	return true
}
