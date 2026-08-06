package tools

import (
	"context"
	"encoding/json"
	"testing"

	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"
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

	data, err := json.Marshal(result.StructuredContent)
	if err != nil {
		t.Fatalf("Marshal structured content: %v", err)
	}
	var response Response[GraphStatus]
	if err := json.Unmarshal(data, &response); err != nil {
		t.Fatalf("Unmarshal structured content: %v", err)
	}
	status := response.Results
	if status.Status != "empty" || status.Repositories != 0 || status.Symbols != 0 || status.Edges != 0 {
		t.Fatalf("graph status = %#v, want empty response", status)
	}
	if response.SnapshotID != nil || response.SnapshotAgeMS != nil || response.NextCursor != nil {
		t.Fatalf("optional metadata = %#v, want nil for empty graph", response)
	}
	if response.Total != 1 || response.Returned != 1 || response.Truncated {
		t.Fatalf("response metadata = %#v, want one untruncated status result", response)
	}
}
