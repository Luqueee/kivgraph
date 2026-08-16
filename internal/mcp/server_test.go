package mcp

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/Luqueee/kivgraph/internal/hotsnapshot"
	"github.com/Luqueee/kivgraph/internal/mcp/tools"
	"github.com/Luqueee/kivgraph/internal/metrics"
	"github.com/Luqueee/kivgraph/internal/version"
)

func TestServerInitializesWithIdentityAndCapabilities(t *testing.T) {
	ctx := context.Background()
	server := NewServer()
	serverTransport, clientTransport := sdkmcp.NewInMemoryTransports()

	serverSession, err := server.Connect(ctx, serverTransport, nil)
	if err != nil {
		t.Fatalf("server.Connect() error = %v", err)
	}
	defer serverSession.Close()

	client := sdkmcp.NewClient(&sdkmcp.Implementation{
		Name:    "test-client",
		Version: "0.0.1",
	}, nil)
	clientSession, err := client.Connect(ctx, clientTransport, nil)
	if err != nil {
		t.Fatalf("client.Connect() error = %v", err)
	}
	defer clientSession.Close()

	result := clientSession.InitializeResult()
	if result == nil {
		t.Fatal("InitializeResult() = nil")
	}
	if result.ServerInfo == nil {
		t.Fatal("InitializeResult().ServerInfo = nil")
	}
	if result.ServerInfo.Name != serverName {
		t.Fatalf("server name = %q, want %q", result.ServerInfo.Name, serverName)
	}
	if result.ServerInfo.Version != version.Value {
		t.Fatalf("server version = %q, want %q", result.ServerInfo.Version, version.Value)
	}
	if result.Capabilities == nil {
		t.Fatal("InitializeResult().Capabilities = nil")
	}
}

func TestServerRecordsQueryMetrics(t *testing.T) {
	ctx := context.Background()
	registry := metrics.NewRegistry()
	server := NewServerWithMetricsAndSnapshotStore(registry, hotsnapshot.NewSnapshotStore(metricsSnapshot(t)))
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

	statusResult, err := clientSession.CallTool(ctx, &sdkmcp.CallToolParams{Name: "graph_status"})
	if err != nil {
		t.Fatalf("graph_status call error = %v", err)
	}
	var statusResponse tools.Response[tools.GraphStatus]
	if statusResult.StructuredContent != nil {
		t.Fatalf("graph_status carries structuredContent as well as text: %#v", statusResult.StructuredContent)
	}
	if len(statusResult.Content) != 1 {
		t.Fatalf("graph_status returned %d content blocks, want exactly one", len(statusResult.Content))
	}
	statusText, ok := statusResult.Content[0].(*sdkmcp.TextContent)
	if !ok {
		t.Fatalf("graph_status content block is %T, want text", statusResult.Content[0])
	}
	if err := json.Unmarshal([]byte(statusText.Text), &statusResponse); err != nil {
		t.Fatalf("unmarshal graph_status text = %v", err)
	}
	if statusResponse.Results.Metrics == nil {
		t.Fatal("graph_status metrics = nil, want configured report")
	}
	// A tool error against a published graph: the key names no symbol. Before
	// there was a graph at all, find_symbol failed with "index not ready", which
	// measured the absence of a snapshot rather than the classification of an
	// error.
	result, err := clientSession.CallTool(ctx, &sdkmcp.CallToolParams{
		Name:      "get_symbol",
		Arguments: map[string]any{"stable_key": "missing"},
	})
	if err != nil {
		t.Fatalf("get_symbol transport error = %v", err)
	}
	if result == nil || !result.IsError {
		t.Fatalf("get_symbol result = %#v, want classified tool error", result)
	}

	report := registry.Report()
	status := report.Queries["graph_status"]
	if status.Calls != 1 || status.Errors != 0 || status.Results != 1 {
		t.Fatalf("graph_status metrics = %+v", status)
	}
	find := report.Queries["get_symbol"]
	if find.Calls != 1 || find.Errors != 1 || find.Results != 0 {
		t.Fatalf("get_symbol metrics = %+v", find)
	}
}

func TestServerRecordsResultAndTruncationMetrics(t *testing.T) {
	snapshot, err := hotsnapshot.BuildGraphSnapshot(hotsnapshot.LadybugSnapshotRows{
		Repositories: []hotsnapshot.RepositoryRow{
			{Key: "repo-a", Name: "a", Languages: "go"},
			{Key: "repo-b", Name: "b", Languages: "go"},
		},
	}, 1, time.Unix(1_700_000_000, 0).UTC(), 1)
	if err != nil {
		t.Fatalf("BuildGraphSnapshot() error = %v", err)
	}

	ctx := context.Background()
	registry := metrics.NewRegistry()
	server := NewServerWithMetricsAndSnapshotStore(registry, hotsnapshot.NewSnapshotStore(snapshot))
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

	result, err := clientSession.CallTool(ctx, &sdkmcp.CallToolParams{
		Name:      "list_repositories",
		Arguments: map[string]any{"limit": 1},
	})
	if err != nil {
		t.Fatalf("list_repositories transport error = %v", err)
	}
	if result == nil || result.IsError {
		t.Fatalf("list_repositories result = %#v, want success", result)
	}

	report := registry.ReportAt(time.Unix(1_700_000_000, 0).UTC())
	repositories := report.Queries["list_repositories"]
	if repositories.Calls != 1 || repositories.Results != 1 || repositories.Truncated != 1 || repositories.Errors != 0 {
		t.Fatalf("list_repositories metrics = %+v", repositories)
	}
	if report.Snapshot.ID != 1 {
		t.Fatalf("snapshot metrics = %+v, want id 1", report.Snapshot)
	}
}

// metricsSnapshot is the smallest published generation a server can answer
// from. The metrics tests are about what a call records, so the graph only has
// to exist.
func metricsSnapshot(t *testing.T) *hotsnapshot.GraphSnapshot {
	t.Helper()
	snapshot, err := hotsnapshot.BuildGraphSnapshot(hotsnapshot.LadybugSnapshotRows{
		Repositories: []hotsnapshot.RepositoryRow{{Key: "repo-a", Name: "a", Languages: "go"}},
	}, 1, time.Unix(1_700_000_000, 0).UTC(), 1)
	if err != nil {
		t.Fatalf("BuildGraphSnapshot() error = %v", err)
	}
	return snapshot
}
