package resilience

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/Luqueee/kivgraph/internal/facts"
	"github.com/Luqueee/kivgraph/internal/hotsnapshot"
	mcpserver "github.com/Luqueee/kivgraph/internal/mcp"
	"github.com/Luqueee/kivgraph/internal/mcp/tools"
	"github.com/Luqueee/kivgraph/internal/tsworker"
)

// TestPublishedSnapshotSurvivesWorkerLoss is the requirement of LUQUE-1201 that
// no single package can state on its own: while the TypeScript worker is being
// killed, restarted and finally given up on, the graph already published stays
// readable and byte-for-byte identical through the real MCP server.
//
// The worker feeds indexing; queries read the published snapshot. If a dead
// worker could take answers away from readers, those two paths would be
// coupled, and this test would fail.
func TestPublishedSnapshotSurvivesWorkerLoss(t *testing.T) {
	store := hotsnapshot.NewSnapshotStore(publishedSnapshot(t))
	session := connectServer(t, store)
	before := querySymbol(t, session)

	supervisor := startWorker(t, func(options *tsworker.Options) {
		options.RestartLimit = 1
		options.RestartWindow = time.Minute
		options.RestartBackoff = 5 * time.Millisecond
	})
	status := supervisor.Status()
	if status.PID == 0 {
		t.Fatalf("worker did not start: %#v", status)
	}

	if err := killProcess(status.PID); err != nil {
		t.Fatalf("killProcess() error = %v", err)
	}
	duringRestart := querySymbol(t, session)

	// Keep killing replacements until the restart budget is spent, so the
	// supervisor gives up instead of quietly recovering behind the test.
	if !waitForWorkerGiveUp(t, supervisor) {
		t.Fatalf("supervisor never reached a terminal state: %#v", supervisor.Status())
	}
	terminal := supervisor.Status()
	if terminal.State != tsworker.StateFailed || terminal.Restarts == 0 {
		t.Fatalf("worker end state = %#v, want FAILED after spending its restart budget", terminal)
	}
	afterGiveUp := querySymbol(t, session)

	if before != duringRestart || before != afterGiveUp {
		t.Fatalf("published graph changed with the worker:\n%s\n%s\n%s", before, duringRestart, afterGiveUp)
	}
}

// TestClosedSnapshotStoreStopsServing is the other side of the same seam: the
// published graph survives a dead worker, but it must not survive its own store
// being closed. Otherwise "still serving" would prove nothing.
func TestClosedSnapshotStoreStopsServing(t *testing.T) {
	store := hotsnapshot.NewSnapshotStore(publishedSnapshot(t))
	session := connectServer(t, store)
	querySymbol(t, session)

	store.Close()

	code, text, err := lookupSymbol(session, "sym-root")
	if err == nil {
		t.Fatalf("get_symbol still answered after Close: %q", text)
	}
	if code != tools.CodeIndexNotReady {
		t.Fatalf("error code = %q, want %q (%q)", code, tools.CodeIndexNotReady, text)
	}
}

// querySymbol reads through the real MCP handler and returns the canonical
// rendering of the answer, so any change in what a reader observes shows up.
func querySymbol(t *testing.T, session *sdkmcp.ClientSession) string {
	t.Helper()
	result, err := session.CallTool(context.Background(), &sdkmcp.CallToolParams{
		Name:      "get_symbol",
		Arguments: map[string]any{"stable_key": "sym-root"},
	})
	if err != nil {
		t.Fatalf("CallTool() error = %v", err)
	}
	if result.IsError {
		t.Fatalf("get_symbol returned an error: %s", contentText(result))
	}
	// The tools answer in one channel: the text block. A test that read
	// structuredContent would compare a copy the server no longer sends.
	if result.StructuredContent != nil {
		t.Fatalf("get_symbol carries structuredContent as well as text: %#v", result.StructuredContent)
	}
	var response tools.Response[tools.SymbolDetails]
	if err := json.Unmarshal([]byte(contentText(result)), &response); err != nil {
		t.Fatalf("Unmarshal() error = %v", err)
	}
	// snapshot_age_ms grows between calls by design; everything else must be
	// identical, so it is the one field excluded from the comparison.
	response.SnapshotAgeMS = nil
	stable, err := json.Marshal(response)
	if err != nil {
		t.Fatalf("Marshal() error = %v", err)
	}
	return string(stable)
}

func contentText(result *sdkmcp.CallToolResult) string {
	for _, content := range result.Content {
		if text, ok := content.(*sdkmcp.TextContent); ok {
			return text.Text
		}
	}
	return ""
}

// lookupSymbol calls get_symbol and reports the classified error code when the
// call fails, so a test can assert on the contract instead of on message text.
func lookupSymbol(session *sdkmcp.ClientSession, stableKey string) (string, string, error) {
	result, err := session.CallTool(context.Background(), &sdkmcp.CallToolParams{
		Name:      "get_symbol",
		Arguments: map[string]any{"stable_key": stableKey},
	})
	if err != nil {
		return "", "", err
	}
	text := contentText(result)
	if result.IsError {
		code, _, _ := strings.Cut(text, ":")
		return code, text, errors.New(text)
	}
	return "", text, nil
}

func connectServer(t *testing.T, store *hotsnapshot.SnapshotStore) *sdkmcp.ClientSession {
	t.Helper()
	server := mcpserver.NewServerWithSnapshotStore(store)
	serverTransport, clientTransport := sdkmcp.NewInMemoryTransports()
	serverSession, err := server.Connect(context.Background(), serverTransport, nil)
	if err != nil {
		t.Fatalf("server.Connect() error = %v", err)
	}
	t.Cleanup(func() { _ = serverSession.Close() })

	client := sdkmcp.NewClient(&sdkmcp.Implementation{Name: "resilience-test", Version: "0.0.1"}, nil)
	clientSession, err := client.Connect(context.Background(), clientTransport, nil)
	if err != nil {
		t.Fatalf("client.Connect() error = %v", err)
	}
	t.Cleanup(func() { _ = clientSession.Close() })
	return clientSession
}

// publishedSnapshotTime fixes the build time of every fixture snapshot, so a
// comparison across calls cannot drift on wall clock alone.
var publishedSnapshotTime = time.Unix(1_700_000_000, 0).UTC()

func publishedSnapshot(t *testing.T) *hotsnapshot.GraphSnapshot {
	t.Helper()
	snapshot, err := hotsnapshot.BuildGraphSnapshot(hotsnapshot.LadybugSnapshotRows{
		SchemaVersion:   2,
		ResolverVersion: "resolver-v1",
		Repositories:    []hotsnapshot.RepositoryRow{{Key: "repo-a", Name: "a", Path: "/repo-a", Languages: "ts"}},
		Packages:        []hotsnapshot.PackageRow{{Key: "pkg-a", RepositoryKey: "repo-a", Language: "ts", Name: "a", ModulePath: "@acme/a"}},
		Files:           []hotsnapshot.FileRow{{Key: "file-a", RepositoryKey: "repo-a", PackageKey: "pkg-a", Path: "index.ts", Language: "ts"}},
		Symbols: []hotsnapshot.SymbolRow{
			{StableKey: "sym-root", CanonicalIdentity: "ts:a.Root", FileKey: "file-a", Language: "ts", Name: "Root", QualifiedName: "a.Root", Kind: "function", Signature: "(): void", StartLine: 1, EndLine: 4},
			{StableKey: "sym-leaf", CanonicalIdentity: "ts:a.Leaf", FileKey: "file-a", Language: "ts", Name: "Leaf", QualifiedName: "a.Leaf", Kind: "function", Signature: "(): void", StartLine: 6, EndLine: 8},
		},
		Edges: []hotsnapshot.EdgeRow{{
			SourceKey: "sym-root", TargetKey: "sym-leaf",
			Kind: facts.CodeCallsDirect, Confidence: facts.CodeExactTypechecked, Provenance: facts.CodeGoTypesUse,
			EvidenceKind: "types", EvidenceSourceFileKey: "file-a", EvidenceTargetFileKey: "file-a",
		}},
	}, 91, publishedSnapshotTime, 1)
	if err != nil {
		t.Fatalf("BuildGraphSnapshot() error = %v", err)
	}
	return snapshot
}

// waitForWorkerGiveUp keeps killing replacements until the supervisor stops
// restarting, and reports whether it reached that terminal state.
func waitForWorkerGiveUp(t *testing.T, supervisor *tsworker.Supervisor) bool {
	t.Helper()
	deadline := time.Now().Add(20 * time.Second)
	for time.Now().Before(deadline) {
		status := supervisor.Status()
		if status.State == tsworker.StateFailed || status.State == tsworker.StateStopped {
			return true
		}
		if status.PID != 0 && status.State == tsworker.StateReady {
			_ = killProcess(status.PID)
		}
		time.Sleep(20 * time.Millisecond)
	}
	return false
}

// killProcess ends a worker the way something outside the supervisor would,
// without naming a signal. What this test needs is that the process is gone
// and that the supervisor did not ask for it -- SIGKILL where there are
// signals and TerminateProcess where there are not both satisfy that, and
// naming syscall.SIGKILL here would have made the whole file Unix-only for a
// detail it never asserts on.
func killProcess(pid int) error {
	process, err := os.FindProcess(pid)
	if err != nil {
		return err
	}
	return process.Kill()
}

func startWorker(t *testing.T, tune func(*tsworker.Options)) *tsworker.Supervisor {
	t.Helper()
	executable, err := os.Executable()
	if err != nil {
		t.Fatalf("os.Executable() error = %v", err)
	}
	options := tsworker.Options{
		Command:           executable,
		Env:               append(os.Environ(), workerEnv+"=1"),
		SupervisorVersion: "0.1.0-test",
		HandshakeTimeout:  5 * time.Second,
		RequestTimeout:    5 * time.Second,
		ShutdownGrace:     3 * time.Second,
	}
	if tune != nil {
		tune(&options)
	}
	supervisor, err := tsworker.NewSupervisor(options)
	if err != nil {
		t.Fatalf("NewSupervisor() error = %v", err)
	}
	t.Cleanup(func() { _ = supervisor.Close(context.Background()) })
	if err := supervisor.Start(context.Background()); err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	return supervisor
}
