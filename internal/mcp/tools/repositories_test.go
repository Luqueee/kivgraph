package tools

import (
	"context"
	"encoding/json"
	"testing"

	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"
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
