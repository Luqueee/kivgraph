package mcp

import (
	"context"
	"sync"
	"testing"

	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/Luqueee/ladygraph/internal/indexing"
)

type fakeProjectIndexer struct {
	mu      sync.Mutex
	calls   int
	project indexing.Project
}

func (fake *fakeProjectIndexer) IndexProject(_ context.Context, project indexing.Project) (indexing.ProjectResult, error) {
	fake.mu.Lock()
	defer fake.mu.Unlock()
	fake.calls++
	fake.project = project
	return indexing.ProjectResult{

		Project:      project,
		GenerationID: "7",
		SnapshotID:   7,
	}, nil
}
func TestIndexProjectIsAnnotatedAsMutating(t *testing.T) {
	session := connectToServer(t, NewServerWithIndexer(&fakeProjectIndexer{}))
	listed, err := session.ListTools(context.Background(), nil)
	if err != nil {
		t.Fatalf("ListTools() error = %v", err)
	}
	for _, tool := range listed.Tools {
		if tool.Name != "index_project" {
			continue
		}
		if tool.Annotations == nil || tool.Annotations.ReadOnlyHint {
			t.Fatalf("index_project annotations = %#v, want writable", tool.Annotations)
		}
		if tool.Annotations.DestructiveHint == nil || !*tool.Annotations.DestructiveHint {
			t.Fatalf("index_project annotations = %#v, want destructive hint", tool.Annotations)
		}
		return
	}
	t.Fatal("index_project was not listed")
}

func (fake *fakeProjectIndexer) callCount() int {
	fake.mu.Lock()
	defer fake.mu.Unlock()
	return fake.calls
}

func TestIndexProjectRequiresExplicitConsentWithoutElicitation(t *testing.T) {
	fake := &fakeProjectIndexer{}
	session := connectToServer(t, NewServerWithIndexer(fake))

	result, err := session.CallTool(context.Background(), &sdkmcp.CallToolParams{
		Name: "index_project",
		Arguments: map[string]any{
			"name":      "demo",
			"path":      "/tmp/demo",
			"languages": []any{"go"},
		},
	})
	if err != nil {
		t.Fatalf("CallTool() transport error = %v", err)
	}
	if result == nil || !result.IsError {
		t.Fatalf("CallTool() result = %#v, want permission error", result)
	}
	if calls := fake.callCount(); calls != 0 {
		t.Fatalf("indexer calls = %d, want 0", calls)
	}
}

func TestIndexProjectRunsAfterClientConfirmedFallback(t *testing.T) {
	fake := &fakeProjectIndexer{}
	session := connectToServer(t, NewServerWithIndexer(fake))

	result, err := session.CallTool(context.Background(), &sdkmcp.CallToolParams{
		Name: "index_project",
		Arguments: map[string]any{
			"name":      "demo",
			"path":      "/tmp/demo",
			"languages": []any{"go"},
			"confirmed": true,
		},
	})
	if err != nil {
		t.Fatalf("CallTool() transport error = %v", err)
	}
	if result == nil || result.IsError {
		t.Fatalf("CallTool() result = %#v, want success", result)
	}
	if calls := fake.callCount(); calls != 1 {
		t.Fatalf("indexer calls = %d, want 1", calls)
	}
	fake.mu.Lock()
	defer fake.mu.Unlock()
	if fake.project.Name != "demo" || fake.project.Path != "/tmp/demo" || len(fake.project.Languages) != 1 || fake.project.Languages[0] != "go" {
		t.Fatalf("project = %#v", fake.project)
	}
}

func TestIndexProjectUsesMcpElicitationWhenAdvertised(t *testing.T) {
	t.Run("accepted", func(t *testing.T) {
		fake := &fakeProjectIndexer{}
		session := connectWithElicitation(t, NewServerWithIndexer(fake), func(_ context.Context, request *sdkmcp.ElicitRequest) (*sdkmcp.ElicitResult, error) {
			if request.Params.Message == "" {
				t.Fatal("elicitation message is empty")
			}
			return &sdkmcp.ElicitResult{Action: "accept", Content: map[string]any{"confirmed": true}}, nil
		})
		result, err := session.CallTool(context.Background(), &sdkmcp.CallToolParams{
			Name: "index_project",
			Arguments: map[string]any{
				"name":      "demo",
				"path":      "/tmp/demo",
				"languages": []any{"go"},
			},
		})
		if err != nil || result == nil || result.IsError {
			t.Fatalf("CallTool() = %#v, error %v; want success", result, err)
		}
		if calls := fake.callCount(); calls != 1 {
			t.Fatalf("indexer calls = %d, want 1", calls)
		}
	})

	t.Run("declined", func(t *testing.T) {
		fake := &fakeProjectIndexer{}
		session := connectWithElicitation(t, NewServerWithIndexer(fake), func(context.Context, *sdkmcp.ElicitRequest) (*sdkmcp.ElicitResult, error) {
			return &sdkmcp.ElicitResult{Action: "cancel"}, nil
		})
		result, err := session.CallTool(context.Background(), &sdkmcp.CallToolParams{
			Name: "index_project",
			Arguments: map[string]any{
				"name":      "demo",
				"path":      "/tmp/demo",
				"languages": []any{"go"},
				"confirmed": true,
			},
		})
		if err != nil {
			t.Fatalf("CallTool() transport error = %v", err)
		}
		if result == nil || !result.IsError {
			t.Fatalf("CallTool() result = %#v, want permission error", result)
		}
		if calls := fake.callCount(); calls != 0 {
			t.Fatalf("indexer calls = %d, want 0", calls)
		}
	})
}

func connectWithElicitation(
	t *testing.T,
	server *sdkmcp.Server,
	handler func(context.Context, *sdkmcp.ElicitRequest) (*sdkmcp.ElicitResult, error),
) *sdkmcp.ClientSession {
	t.Helper()
	serverTransport, clientTransport := sdkmcp.NewInMemoryTransports()
	serverSession, err := server.Connect(context.Background(), serverTransport, nil)
	if err != nil {
		t.Fatalf("server.Connect() error = %v", err)
	}
	t.Cleanup(func() { _ = serverSession.Close() })
	client := sdkmcp.NewClient(&sdkmcp.Implementation{Name: "index-project-test", Version: "0.0.1"}, &sdkmcp.ClientOptions{
		ElicitationHandler: handler,
	})
	clientSession, err := client.Connect(context.Background(), clientTransport, nil)
	if err != nil {
		t.Fatalf("client.Connect() error = %v", err)
	}
	t.Cleanup(func() { _ = clientSession.Close() })
	return clientSession
}
