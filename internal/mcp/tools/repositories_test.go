package tools

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/Luqueee/ladygraph/internal/hotsnapshot"
	"github.com/Luqueee/ladygraph/internal/testsupport"
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

	response := decodeResponse[[]RepositorySummary](t, result)
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
	response := decodeResponse[[]RepositorySummary](t, result)
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

const (
	indexedFixtureCommit = "1a2b3c4d5e6f708192a3b4c5d6e7f80912345678"
	movedFixtureCommit   = "9f8e7d6c5b4a39281706f5e4d3c2b1a098765432"
)

// TestListRepositoriesReportsWorkingTreeMovement covers the four answers the
// tool can give about freshness. Three of them are not "moved": a tree that
// still holds the indexed commit, a path that is not a checkout, and a graph
// built before the commit was recorded. Only the first of those three means
// the results can be trusted, so the other two have to say so out loud.
func TestListRepositoriesReportsWorkingTreeMovement(t *testing.T) {
	current := writeGitCheckout(t, indexedFixtureCommit, "main")
	drifted := writeGitCheckout(t, movedFixtureCommit, "feature/x")
	notACheckout := testsupport.TempDir(t)
	unrecorded := writeGitCheckout(t, indexedFixtureCommit, "main")

	client := newRepositoryToolClient(t, repositorySnapshot(t, 9,
		hotsnapshot.RepositoryRow{
			Key: "repo-1", Name: "current", Path: current, Languages: "go",
			Commit: indexedFixtureCommit, Branch: "main",
		},
		hotsnapshot.RepositoryRow{
			Key: "repo-2", Name: "drifted", Path: drifted, Languages: "go",
			Commit: indexedFixtureCommit, Branch: "main", Dirty: true,
		},
		hotsnapshot.RepositoryRow{
			Key: "repo-3", Name: "gone", Path: notACheckout, Languages: "go",
			Commit: indexedFixtureCommit, Branch: "main",
		},
		hotsnapshot.RepositoryRow{
			Key: "repo-4", Name: "unrecorded", Path: unrecorded, Languages: "go",
		},
	))

	result, err := client.CallTool(context.Background(), &sdkmcp.CallToolParams{Name: "list_repositories"})
	if err != nil {
		t.Fatalf("CallTool() error = %v", err)
	}
	if result.IsError {
		t.Fatalf("CallTool() returned an error result: %#v", result.Content)
	}
	summaries := repositorySummariesByName(t, decodeRepositoryResponse(t, result).Results, 4)

	unchanged := summaries["current"]
	if unchanged.Moved || unchanged.MovedDetail != "" {
		t.Fatalf("unchanged repository = %#v, want moved false with no detail", unchanged)
	}
	if unchanged.IndexedCommit != indexedFixtureCommit || unchanged.CurrentCommit != indexedFixtureCommit {
		t.Fatalf("unchanged commits = %q/%q", unchanged.IndexedCommit, unchanged.CurrentCommit)
	}
	if unchanged.IndexedBranch != "main" || unchanged.CurrentBranch != "main" || unchanged.IndexedDirty {
		t.Fatalf("unchanged branches = %q/%q dirty=%t", unchanged.IndexedBranch, unchanged.CurrentBranch, unchanged.IndexedDirty)
	}

	moved := summaries["drifted"]
	if !moved.Moved {
		t.Fatalf("moved repository = %#v, want moved true", moved)
	}
	if moved.CurrentCommit != movedFixtureCommit || moved.CurrentBranch != "feature/x" {
		t.Fatalf("moved head = %q on %q, want the tree on disk", moved.CurrentCommit, moved.CurrentBranch)
	}
	if !moved.IndexedDirty {
		t.Fatalf("moved repository indexed_dirty = false, want the recorded dirty tree")
	}
	// The sentence has to name both positions: a detail that only says
	// "moved" sends the reader back for the numbers it already had.
	for _, want := range []string{indexedFixtureCommit[:7], movedFixtureCommit[:7], "main", "feature/x"} {
		if !strings.Contains(moved.MovedDetail, want) {
			t.Fatalf("moved_detail = %q, want it to name %q", moved.MovedDetail, want)
		}
	}
	if strings.Contains(moved.MovedDetail, indexedFixtureCommit) {
		t.Fatalf("moved_detail = %q, want short commit prefixes in the prose", moved.MovedDetail)
	}

	unreadable := summaries["gone"]
	if unreadable.Moved {
		t.Fatalf("unreadable repository = %#v, want moved false: nothing was compared", unreadable)
	}
	if unreadable.CurrentCommit != "" || unreadable.CurrentBranch != "" {
		t.Fatalf("unreadable head = %q/%q, want empty", unreadable.CurrentCommit, unreadable.CurrentBranch)
	}
	if unreadable.MovedDetail == "" {
		t.Fatalf("unreadable repository = %#v, want a reason: an unknown answer must not read as a good one", unreadable)
	}
	if unreadable.IndexedCommit != indexedFixtureCommit {
		t.Fatalf("unreadable indexed_commit = %q, want what the graph recorded", unreadable.IndexedCommit)
	}

	gap := summaries["unrecorded"]
	if gap.Moved || gap.IndexedCommit != "" || gap.IndexedBranch != "" {
		t.Fatalf("repository without an indexed commit = %#v", gap)
	}
	if gap.CurrentCommit != "" || gap.CurrentBranch != "" {
		t.Fatalf("repository without an indexed commit reported a head = %q/%q, want nothing to compare", gap.CurrentCommit, gap.CurrentBranch)
	}
	if !strings.Contains(gap.MovedDetail, "does not record") {
		t.Fatalf("moved_detail = %q, want it to name the gap in the graph", gap.MovedDetail)
	}
}

func repositorySummariesByName(t *testing.T, summaries []RepositorySummary, want int) map[string]RepositorySummary {
	t.Helper()
	if len(summaries) != want {
		t.Fatalf("repositories = %#v, want %d entries", summaries, want)
	}
	byName := make(map[string]RepositorySummary, len(summaries))
	for _, summary := range summaries {
		byName[summary.Name] = summary
	}
	return byName
}

// writeGitCheckout builds a repository whose HEAD resolves to commit on
// branch. HEAD and the loose reference are written directly: running git would
// make the fixture depend on an installed binary and on the ambient user
// configuration to assert something both files already state.
func writeGitCheckout(t *testing.T, commit, branch string) string {
	t.Helper()
	root := testsupport.TempDir(t)
	reference := filepath.Join(root, ".git", "refs", "heads", filepath.FromSlash(branch))
	if err := os.MkdirAll(filepath.Dir(reference), 0o755); err != nil {
		t.Fatalf("create git reference directory: %v", err)
	}
	head := filepath.Join(root, ".git", "HEAD")
	if err := os.WriteFile(head, []byte("ref: refs/heads/"+branch+"\n"), 0o644); err != nil {
		t.Fatalf("write HEAD: %v", err)
	}
	if err := os.WriteFile(reference, []byte(commit+"\n"), 0o644); err != nil {
		t.Fatalf("write branch reference: %v", err)
	}
	return root
}
