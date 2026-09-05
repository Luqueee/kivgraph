package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/Luqueee/kivgraph/internal/indexing"
)

type controlledProjectIndexer struct {
	started chan struct{}
	release chan struct{}
	err     error
}

func newControlledProjectIndexer() *controlledProjectIndexer {
	return &controlledProjectIndexer{
		started: make(chan struct{}),
		release: make(chan struct{}),
	}
}

func (indexer *controlledProjectIndexer) IndexProjects(
	ctx context.Context,
	projects []indexing.Project,
	progress func(indexing.ProjectProgress),
) (indexing.ProjectResult, error) {
	close(indexer.started)
	if progress != nil {
		progress(indexing.ProjectProgress{
			Phase: "go", Repository: projects[0].Name, Completed: 0, Total: 1,
		})
	}
	select {
	case <-indexer.release:
	case <-ctx.Done():
		return indexing.ProjectResult{}, ctx.Err()
	}
	if indexer.err != nil {
		return indexing.ProjectResult{}, indexer.err
	}
	return indexing.ProjectResult{
		Project: projects[0], Projects: projects, GenerationID: "8", SnapshotID: 8,
	}, nil
}

type startIndexProjectResponse struct {
	OperationID string `json:"operation_id"`
	Status      string `json:"status"`
}

type getIndexStatusResponse struct {
	OperationID string                    `json:"operation_id"`
	Status      string                    `json:"status"`
	Progress    *indexing.ProjectProgress `json:"progress"`
	Result      *indexing.ProjectResult   `json:"result"`
	Failure     *struct {
		Code    string `json:"code"`
		Message string `json:"message"`
	} `json:"failure"`
}

func decodeToolResponse[T any](t *testing.T, result *sdkmcp.CallToolResult) T {
	t.Helper()
	var decoded T
	if err := json.Unmarshal([]byte(contentText(t, result)), &decoded); err != nil {
		t.Fatalf("decode tool response: %v", err)
	}
	return decoded
}

type fakeProjectIndexer struct {
	mu       sync.Mutex
	calls    int
	project  indexing.Project
	batch    []indexing.Project
	progress int
}

type fakeProfileProjectIndexer struct {
	fakeProjectIndexer
	profile string
}

func (fake *fakeProfileProjectIndexer) IndexProjectsInProfile(
	ctx context.Context,
	profile string,
	projects []indexing.Project,
	progress func(indexing.ProjectProgress),
) (indexing.ProjectResult, error) {
	fake.profile = profile
	return fake.IndexProjects(ctx, projects, progress)
}

func TestIndexProjectRejectsNamedProfileWhenIndexerCannotRouteIt(t *testing.T) {
	fake := &fakeProjectIndexer{}
	session := connectToServer(t, NewServerWithIndexer(fake))
	result, err := session.CallTool(context.Background(), &sdkmcp.CallToolParams{
		Name: "index_project",
		Arguments: map[string]any{
			"profile": "other", "name": "demo", "path": "/tmp/demo",
			"languages": []any{"go"}, "confirmed": true,
		},
	})
	if err != nil {
		t.Fatalf("CallTool() transport error = %v", err)
	}
	if result == nil || !result.IsError {
		t.Fatalf("CallTool() result = %#v, want indexing error", result)
	}
	if calls := fake.callCount(); calls != 0 {
		t.Fatalf("default indexer calls = %d, want 0", calls)
	}
}

func TestIndexProjectRoutesNamedProfile(t *testing.T) {
	fake := &fakeProfileProjectIndexer{}
	session := connectToServer(t, NewServerWithIndexer(fake))
	result, err := session.CallTool(context.Background(), &sdkmcp.CallToolParams{
		Name: "index_project",
		Arguments: map[string]any{
			"profile": "other", "name": "demo", "path": "/tmp/demo",
			"languages": []any{"go"}, "confirmed": true,
		},
	})
	if err != nil {
		t.Fatalf("CallTool() transport error = %v", err)
	}
	if result == nil || result.IsError {
		t.Fatalf("CallTool() result = %#v, want success", result)
	}
	if fake.profile != "other" {
		t.Fatalf("profile = %q, want other", fake.profile)
	}
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

func TestStartIndexProjectRequiresExplicitConsent(t *testing.T) {
	indexer := newControlledProjectIndexer()
	session := connectToServer(t, NewServerWithIndexer(indexer))
	result, err := session.CallTool(context.Background(), &sdkmcp.CallToolParams{
		Name: "start_index_project",
		Arguments: map[string]any{
			"name": "demo", "path": "/tmp/demo", "languages": []any{"go"},
		},
	})
	if err != nil {
		t.Fatalf("CallTool() transport error = %v", err)
	}
	if result == nil || !result.IsError {
		t.Fatalf("CallTool() result = %#v, want permission error", result)
	}
	select {
	case <-indexer.started:
		t.Fatal("indexing started without consent")
	default:
	}
}

func TestStartIndexProjectRejectsInvalidInputBeforeStarting(t *testing.T) {
	indexer := newControlledProjectIndexer()
	session := connectToServer(t, NewServerWithIndexer(indexer))
	for name, arguments := range map[string]map[string]any{
		"missing project": {"confirmed": true},
		"unsupported profile": {
			"profile": "other", "name": "demo", "path": "/tmp/demo",
			"languages": []any{"go"}, "confirmed": true,
		},
	} {
		t.Run(name, func(t *testing.T) {
			result, err := session.CallTool(context.Background(), &sdkmcp.CallToolParams{
				Name: "start_index_project", Arguments: arguments,
			})
			if err != nil {
				t.Fatalf("CallTool() transport error = %v", err)
			}
			if result == nil || !result.IsError {
				t.Fatalf("CallTool() result = %#v, want invalid argument", result)
			}
		})
	}
	select {
	case <-indexer.started:
		t.Fatal("indexing started for invalid input")
	default:
	}
}

func TestStartIndexProjectReturnsBeforeTheIndexFinishes(t *testing.T) {
	indexer := newControlledProjectIndexer()
	session := connectToServer(t, NewServerWithIndexer(indexer))
	callContext, cancelCall := context.WithCancel(context.Background())
	result, err := session.CallTool(callContext, &sdkmcp.CallToolParams{
		Name: "start_index_project",
		Arguments: map[string]any{
			"name": "demo", "path": "/tmp/demo", "languages": []any{"go"}, "confirmed": true,
		},
	})
	if err != nil {
		t.Fatalf("CallTool() transport error = %v", err)
	}
	if result == nil || result.IsError {
		t.Fatalf("CallTool() result = %#v, want accepted operation", result)
	}
	started := decodeToolResponse[startIndexProjectResponse](t, result)
	if started.OperationID == "" || started.Status != "working" {
		t.Fatalf("start response = %#v, want a working operation ID", started)
	}
	select {
	case <-indexer.started:
	case <-time.After(time.Second):
		t.Fatal("background index did not start")
	}

	// The operation belongs to the server, not to the short tools/call
	// request. Cancelling that request after its response must not cancel the
	// rebuild the response just accepted.
	cancelCall()
	close(indexer.release)
	status := waitForIndexStatus(t, session, started.OperationID, "completed")
	if status.Result == nil || status.Result.GenerationID != "8" || status.Result.SnapshotID != 8 {
		t.Fatalf("completed status = %#v, want the published result", status)
	}
}

func TestGetIndexStatusRejectsMissingAndUnknownOperationIDs(t *testing.T) {
	session := connectToServer(t, NewServerWithIndexer(&fakeProjectIndexer{}))
	for name, operationID := range map[string]string{
		"missing":         "",
		"unknown":         "0123456789abcdef0123456789abcdef",
		"corrupt":         "not-an-operation-id",
		"non hexadecimal": "gggggggggggggggggggggggggggggggg",
	} {
		t.Run(name, func(t *testing.T) {
			result, err := session.CallTool(context.Background(), &sdkmcp.CallToolParams{
				Name: "get_index_status", Arguments: map[string]any{"operation_id": operationID},
			})
			if err != nil {
				t.Fatalf("CallTool() transport error = %v", err)
			}
			if result == nil || !result.IsError {
				t.Fatalf("CallTool() result = %#v, want invalid argument", result)
			}
			if text := contentText(t, result); !strings.Contains(text, "operation_id") {
				t.Fatalf("error = %q, want narrowing guidance", text)
			}
		})
	}
}

func TestStartIndexProjectRoutesANamedProfileAsynchronously(t *testing.T) {
	indexer := &fakeProfileProjectIndexer{}
	session := connectToServer(t, NewServerWithIndexer(indexer))
	result, err := session.CallTool(context.Background(), &sdkmcp.CallToolParams{
		Name: "start_index_project",
		Arguments: map[string]any{
			"profile": "other", "name": "demo", "path": "/tmp/demo",
			"languages": []any{"go"}, "confirmed": true,
		},
	})
	if err != nil || result == nil || result.IsError {
		t.Fatalf("start CallTool() = %#v, %v", result, err)
	}
	started := decodeToolResponse[startIndexProjectResponse](t, result)
	waitForIndexStatus(t, session, started.OperationID, "completed")
	if indexer.profile != "other" {
		t.Fatalf("profile = %q, want other", indexer.profile)
	}
}

func TestIndexStatusBoundsCompletedHistory(t *testing.T) {
	session := connectToServer(t, NewServerWithIndexer(&fakeProjectIndexer{}))
	operationIDs := make([]string, 0, 33)
	for index := range 33 {
		result, err := session.CallTool(context.Background(), &sdkmcp.CallToolParams{
			Name: "start_index_project",
			Arguments: map[string]any{
				"name": fmt.Sprintf("demo-%d", index), "path": fmt.Sprintf("/tmp/demo-%d", index),
				"languages": []any{"go"}, "confirmed": true,
			},
		})
		if err != nil || result == nil || result.IsError {
			t.Fatalf("start %d = %#v, %v", index, result, err)
		}
		started := decodeToolResponse[startIndexProjectResponse](t, result)
		operationIDs = append(operationIDs, started.OperationID)
		waitForIndexStatus(t, session, started.OperationID, "completed")
	}
	result, err := session.CallTool(context.Background(), &sdkmcp.CallToolParams{
		Name: "get_index_status", Arguments: map[string]any{"operation_id": operationIDs[0]},
	})
	if err != nil {
		t.Fatalf("get pruned status transport error = %v", err)
	}
	if result == nil || !result.IsError || !strings.Contains(contentText(t, result), "no longer retained") {
		t.Fatalf("oldest status = %#v, want bounded-history refusal", result)
	}
}

func TestStartIndexProjectRejectsASecondActiveOperation(t *testing.T) {
	indexer := newControlledProjectIndexer()
	session := connectToServer(t, NewServerWithIndexer(indexer))
	arguments := map[string]any{
		"name": "demo", "path": "/tmp/demo", "languages": []any{"go"}, "confirmed": true,
	}
	first, err := session.CallTool(context.Background(), &sdkmcp.CallToolParams{
		Name: "start_index_project", Arguments: arguments,
	})
	if err != nil || first == nil || first.IsError {
		t.Fatalf("first CallTool() = %#v, %v", first, err)
	}
	started := decodeToolResponse[startIndexProjectResponse](t, first)
	select {
	case <-indexer.started:
	case <-time.After(time.Second):
		t.Fatal("background index did not start")
	}
	second, err := session.CallTool(context.Background(), &sdkmcp.CallToolParams{
		Name: "start_index_project", Arguments: arguments,
	})
	if err != nil {
		t.Fatalf("second CallTool() transport error = %v", err)
	}
	if second == nil || !second.IsError || !strings.Contains(contentText(t, second), "already running") ||
		!strings.Contains(contentText(t, second), started.OperationID) {
		t.Fatalf("second CallTool() = %#v, want active-operation refusal", second)
	}
	close(indexer.release)
}

func TestGetIndexStatusReportsProgressAndFailure(t *testing.T) {
	indexer := newControlledProjectIndexer()
	indexer.err = errors.New("analyzer exploded")
	session := connectToServer(t, NewServerWithIndexer(indexer))
	result, err := session.CallTool(context.Background(), &sdkmcp.CallToolParams{
		Name: "start_index_project",
		Arguments: map[string]any{
			"name": "demo", "path": "/tmp/demo", "languages": []any{"go"}, "confirmed": true,
		},
	})
	if err != nil || result == nil || result.IsError {
		t.Fatalf("start CallTool() = %#v, %v", result, err)
	}
	started := decodeToolResponse[startIndexProjectResponse](t, result)
	select {
	case <-indexer.started:
	case <-time.After(time.Second):
		t.Fatal("background index did not start")
	}
	running := waitForIndexStatus(t, session, started.OperationID, "working")
	if running.Progress == nil || running.Progress.Phase != "go" || running.Progress.Repository != "demo" {
		t.Fatalf("working status = %#v, want observed progress", running)
	}
	close(indexer.release)
	failed := waitForIndexStatus(t, session, started.OperationID, "failed")
	if failed.Failure == nil || failed.Failure.Code != "INDEXING_FAILED" ||
		!strings.Contains(failed.Failure.Message, "analyzer exploded") {
		t.Fatalf("failed status = %#v, want classified observed cause", failed)
	}
}

func TestIndexStatusSurvivesAcrossDaemonStyleSessions(t *testing.T) {
	indexer := newControlledProjectIndexer()
	jobs := NewIndexJobs(indexer)
	newSession := func() *sdkmcp.ClientSession {
		return connectToServer(t, newServerWithIndexer(
			nil, nil, nil, indexer, ServerOptions{IndexJobs: jobs},
		))
	}
	starter := newSession()
	result, err := starter.CallTool(context.Background(), &sdkmcp.CallToolParams{
		Name: "start_index_project",
		Arguments: map[string]any{
			"name": "demo", "path": "/tmp/demo", "languages": []any{"go"}, "confirmed": true,
		},
	})
	if err != nil || result == nil || result.IsError {
		t.Fatalf("start CallTool() = %#v, %v", result, err)
	}
	started := decodeToolResponse[startIndexProjectResponse](t, result)
	select {
	case <-indexer.started:
	case <-time.After(time.Second):
		t.Fatal("background index did not start")
	}

	observer := newSession()
	working := waitForIndexStatus(t, observer, started.OperationID, "working")
	if working.OperationID != started.OperationID {
		t.Fatalf("operation ID = %q, want %q", working.OperationID, started.OperationID)
	}
	close(indexer.release)
	waitForIndexStatus(t, observer, started.OperationID, "completed")
}

func waitForIndexStatus(
	t *testing.T,
	session *sdkmcp.ClientSession,
	operationID string,
	want string,
) getIndexStatusResponse {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		result, err := session.CallTool(context.Background(), &sdkmcp.CallToolParams{
			Name: "get_index_status", Arguments: map[string]any{"operation_id": operationID},
		})
		if err != nil {
			t.Fatalf("get_index_status transport error = %v", err)
		}
		if result == nil || result.IsError {
			t.Fatalf("get_index_status result = %#v", result)
		}
		status := decodeToolResponse[getIndexStatusResponse](t, result)
		if status.Status == want {
			return status
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("operation %q did not reach %q", operationID, want)
	return getIndexStatusResponse{}
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

func TestIndexProjectUsesConfirmedFallbackForURLOnlyElicitation(t *testing.T) {
	fake := &fakeProjectIndexer{}
	var unexpectedElicitation atomic.Bool
	session := connectWithNamedElicitation(t, NewServerWithIndexer(fake), "url-only-client", &sdkmcp.ClientCapabilities{
		Elicitation: &sdkmcp.ElicitationCapabilities{
			URL: &sdkmcp.URLElicitationCapabilities{},
		},
	}, func(context.Context, *sdkmcp.ElicitRequest) (*sdkmcp.ElicitResult, error) {
		unexpectedElicitation.Store(true)
		return nil, fmt.Errorf("form elicitation requested for client=url-only-client with elicitation.url capability")
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
	if unexpectedElicitation.Load() {
		t.Fatal("form elicitation requested for client=url-only-client with elicitation.url capability")
	}
	if err != nil || result == nil || result.IsError {
		t.Fatalf("CallTool() = %#v, error %v; want success", result, err)
	}
	if calls := fake.callCount(); calls != 1 {
		t.Fatalf("indexer calls = %d, want 1", calls)
	}
	result, err = session.CallTool(context.Background(), &sdkmcp.CallToolParams{
		Name: "index_project",
		Arguments: map[string]any{
			"name":      "demo",
			"path":      "/tmp/demo",
			"languages": []any{"go"},
		},
	})
	if err != nil || result == nil || !result.IsError {
		t.Fatalf("CallTool() = %#v, error %v; want permission error without confirmed", result, err)
	}
	if calls := fake.callCount(); calls != 1 {
		t.Fatalf("indexer calls after missing confirmation = %d, want 1", calls)
	}
}

func TestIndexProjectUsesConfirmedFallbackForCodexNativeApproval(t *testing.T) {
	fake := &fakeProjectIndexer{}
	session := connectWithNamedElicitation(t, NewServerWithIndexer(fake), "codex", &sdkmcp.ClientCapabilities{
		Elicitation: &sdkmcp.ElicitationCapabilities{
			Form: &sdkmcp.FormElicitationCapabilities{},
		},
	}, func(context.Context, *sdkmcp.ElicitRequest) (*sdkmcp.ElicitResult, error) {
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
	if err != nil || result == nil || result.IsError {
		t.Fatalf("CallTool() = %#v, error %v; want success", result, err)
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
	return connectWithNamedElicitation(t, server, "index-project-test", nil, handler)
}

func connectWithNamedElicitation(
	t *testing.T,
	server *sdkmcp.Server,
	name string,
	capabilities *sdkmcp.ClientCapabilities,
	handler func(context.Context, *sdkmcp.ElicitRequest) (*sdkmcp.ElicitResult, error),
) *sdkmcp.ClientSession {
	t.Helper()
	serverTransport, clientTransport := sdkmcp.NewInMemoryTransports()
	serverSession, err := server.Connect(context.Background(), serverTransport, nil)
	if err != nil {
		t.Fatalf("server.Connect() error = %v", err)
	}
	t.Cleanup(func() { _ = serverSession.Close() })
	client := sdkmcp.NewClient(&sdkmcp.Implementation{Name: name, Version: "0.0.1"}, &sdkmcp.ClientOptions{
		Capabilities:       capabilities,
		ElicitationHandler: handler,
	})
	clientSession, err := client.Connect(context.Background(), clientTransport, nil)
	if err != nil {
		t.Fatalf("client.Connect() error = %v", err)
	}
	t.Cleanup(func() { _ = clientSession.Close() })
	return clientSession
}
