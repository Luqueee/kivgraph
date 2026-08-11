package mcp

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/Luqueee/ladygraph/internal/indexing"
)

type fakeProjectIndexer struct {
	mu       sync.Mutex
	calls    int
	project  indexing.Project
	batch    []indexing.Project
	progress int
}

func (fake *fakeProjectIndexer) IndexProjects(
	_ context.Context,
	projects []indexing.Project,
	progress func(indexing.ProjectProgress),
) (indexing.ProjectResult, error) {
	if progress != nil {
		for index, project := range projects {
			progress(indexing.ProjectProgress{
				Phase: "go", Repository: project.Name, Completed: index + 1, Total: len(projects),
			})
		}
	}
	fake.mu.Lock()
	defer fake.mu.Unlock()
	fake.calls++
	fake.batch = projects
	if len(projects) != 0 {
		fake.project = projects[0]
	}
	if progress != nil {
		fake.progress += len(projects)
	}
	return indexing.ProjectResult{
		Project:      fake.project,
		Projects:     projects,
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

// A full rebuild outlives the timeout an MCP client applies to a call, so the
// tool reports progress and the client that asked for it waits. Without this
// the client cancels work that is progressing, and the index finishes anyway
// with nobody listening.
func TestIndexProjectReportsProgressToAClientThatAsksForIt(t *testing.T) {
	fake := &fakeProjectIndexer{}
	serverTransport, clientTransport := sdkmcp.NewInMemoryTransports()
	serverSession, err := NewServerWithIndexer(fake).Connect(context.Background(), serverTransport, nil)
	if err != nil {
		t.Fatalf("server.Connect() error = %v", err)
	}
	t.Cleanup(func() { _ = serverSession.Close() })

	var mu sync.Mutex
	seen := make([]float64, 0, 4)
	client := sdkmcp.NewClient(&sdkmcp.Implementation{Name: "progress-test", Version: "0.0.1"}, &sdkmcp.ClientOptions{
		ProgressNotificationHandler: func(_ context.Context, request *sdkmcp.ProgressNotificationClientRequest) {
			mu.Lock()
			defer mu.Unlock()
			seen = append(seen, request.Params.Progress)
		},
	})
	clientSession, err := client.Connect(context.Background(), clientTransport, nil)
	if err != nil {
		t.Fatalf("client.Connect() error = %v", err)
	}
	t.Cleanup(func() { _ = clientSession.Close() })

	params := &sdkmcp.CallToolParams{
		Name: "index_project",
		Meta: sdkmcp.Meta{"progressToken": "index-1"},
		Arguments: map[string]any{
			"projects": []any{
				map[string]any{"name": "one", "path": "/tmp/one", "languages": []any{"go"}},
				map[string]any{"name": "two", "path": "/tmp/two", "languages": []any{"go"}},
			},
			"confirmed": true,
		},
	}
	if _, err := clientSession.CallTool(context.Background(), params); err != nil {
		t.Fatalf("CallTool() error = %v", err)
	}

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		mu.Lock()
		enough := len(seen) >= 2
		mu.Unlock()
		if enough {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(seen) != 2 {
		t.Fatalf("progress notifications = %v, want one per unit of work", seen)
	}
	// The protocol requires a value that always increases.
	if seen[0] >= seen[1] {
		t.Fatalf("progress = %v, want strictly increasing values", seen)
	}
}

// A client that sends no progress token gets no notifications, and the index
// pays for no callback at all.
func TestIndexProjectSkipsProgressWithoutAToken(t *testing.T) {
	fake := &fakeProjectIndexer{}
	session := connectToServer(t, NewServerWithIndexer(fake))
	if _, err := session.CallTool(context.Background(), &sdkmcp.CallToolParams{
		Name: "index_project",
		Arguments: map[string]any{
			"name": "demo", "path": "/tmp/demo", "languages": []any{"go"}, "confirmed": true,
		},
	}); err != nil {
		t.Fatalf("CallTool() error = %v", err)
	}
	fake.mu.Lock()
	defer fake.mu.Unlock()
	if fake.progress != 0 {
		t.Fatalf("progress callbacks = %d, want none without a token", fake.progress)
	}
}

// A rebuild resolves cross-repository edges over the complete fact set, so it
// costs the whole corpus whatever was added. Registering eleven projects one
// call at a time pays that cost eleven times and keeps only the last graph;
// the batch pays it once. This is the difference between minutes and an
// afternoon, so it is a contract, not an optimisation.
func TestIndexProjectRebuildsOnceForAWholeBatch(t *testing.T) {
	fake := &fakeProjectIndexer{}
	session := connectToServer(t, NewServerWithIndexer(fake))

	projects := make([]any, 0, 11)
	for index := range 11 {
		projects = append(projects, map[string]any{
			"name":      fmt.Sprintf("repo-%02d", index),
			"path":      fmt.Sprintf("/tmp/repo-%02d", index),
			"languages": []any{"go"},
		})
	}
	result, err := session.CallTool(context.Background(), &sdkmcp.CallToolParams{
		Name:      "index_project",
		Arguments: map[string]any{"projects": projects, "confirmed": true},
	})
	if err != nil {
		t.Fatalf("CallTool() error = %v", err)
	}
	if result.IsError {
		t.Fatalf("CallTool() result = %#v", result)
	}

	fake.mu.Lock()
	defer fake.mu.Unlock()
	if fake.calls != 1 {
		t.Fatalf("rebuilds = %d, want exactly one for the batch", fake.calls)
	}
	if len(fake.batch) != 11 {
		t.Fatalf("indexed projects = %d, want every project of the batch", len(fake.batch))
	}
	if fake.batch[0].Name != "repo-00" || fake.batch[10].Name != "repo-10" {
		t.Fatalf("batch = %#v, want the requested order preserved", fake.batch)
	}
}

// The two forms cannot be mixed: they could only disagree, and guessing which
// one the caller meant is how a repository ends up registered twice.
func TestIndexProjectRejectsAnUnusableRequest(t *testing.T) {
	fake := &fakeProjectIndexer{}
	session := connectToServer(t, NewServerWithIndexer(fake))

	for name, arguments := range map[string]map[string]any{
		"both forms": {
			"name": "one", "path": "/tmp/one", "languages": []any{"go"},
			"projects":  []any{map[string]any{"name": "two", "path": "/tmp/two", "languages": []any{"go"}}},
			"confirmed": true,
		},
		"neither form": {"confirmed": true},
	} {
		t.Run(name, func(t *testing.T) {
			result, err := session.CallTool(context.Background(), &sdkmcp.CallToolParams{
				Name: "index_project", Arguments: arguments,
			})
			if err != nil {
				t.Fatalf("CallTool() transport error = %v", err)
			}
			if result == nil || !result.IsError {
				t.Fatalf("CallTool() result = %#v, want an invalid argument error", result)
			}
		})
	}
	fake.mu.Lock()
	defer fake.mu.Unlock()
	if fake.calls != 0 {
		t.Fatalf("indexer calls = %d, want none", fake.calls)
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
