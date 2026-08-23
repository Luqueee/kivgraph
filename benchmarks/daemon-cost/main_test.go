package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"sync/atomic"
	"testing"

	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/Luqueee/kivgraph/internal/mcpworkload"
)

// fakeServer answers the tools this harness drives and counts what it was
// asked. The count is the whole point of these tests: an idle arm is defined by
// the number being zero when the sample is taken, and nothing else about the
// arrangement can show that.
type fakeServer struct {
	calls      atomic.Int64
	snapshotID uint64
}

func (fake *fakeServer) session(t *testing.T) *sdkmcp.ClientSession {
	t.Helper()
	ctx := context.Background()
	server := sdkmcp.NewServer(&sdkmcp.Implementation{Name: "fake", Version: "1.0.0"}, nil)

	answer := func(body string) sdkmcp.ToolHandler {
		return func(context.Context, *sdkmcp.CallToolRequest) (*sdkmcp.CallToolResult, error) {
			fake.calls.Add(1)
			return &sdkmcp.CallToolResult{
				Content: []sdkmcp.Content{&sdkmcp.TextContent{Text: body}},
			}, nil
		}
	}
	empty := &sdkmcp.Tool{Name: "", InputSchema: map[string]any{"type": "object"}}

	status := fmt.Sprintf(`{"snapshot_id":%d,"results":{"symbols":117499}}`, fake.snapshotID)
	rows := make([]map[string]string, 0, 16)
	for index := range 16 {
		rows = append(rows, map[string]string{
			"name":       fmt.Sprintf("probe%02d", index),
			"stable_key": fmt.Sprintf("go:probe%02d", index),
		})
	}
	found, err := json.Marshal(map[string]any{"results": rows})
	if err != nil {
		t.Fatalf("marshal probes: %v", err)
	}

	for name, body := range map[string]string{
		"graph_status":                             status,
		string(mcpworkload.FindSymbol):             string(found),
		string(mcpworkload.GetSymbol):              `{"results":[]}`,
		string(mcpworkload.FindReferences):         `{"results":[]}`,
		string(mcpworkload.FindCrossRepoConsumers): `{"results":[]}`,
		string(mcpworkload.GetBlastRadius):         `{"results":[]}`,
	} {
		tool := *empty
		tool.Name = name
		server.AddTool(&tool, answer(body))
	}

	serverSide, clientSide := sdkmcp.NewInMemoryTransports()
	if _, err := server.Connect(ctx, serverSide, nil); err != nil {
		t.Fatalf("connect server: %v", err)
	}
	client := sdkmcp.NewClient(&sdkmcp.Implementation{Name: "test", Version: "1.0.0"}, nil)
	session, err := client.Connect(ctx, clientSide, nil)
	if err != nil {
		t.Fatalf("connect client: %v", err)
	}
	t.Cleanup(func() { _ = session.Close() })
	return session
}

// sampleAt drives one arm and reports what the server had been asked at the
// instant the memory was read. The pids callback is that instant, so it is the
// only honest place to observe it.
func sampleAt(t *testing.T, fake *fakeServer, calls int) (arm, int64, error) {
	t.Helper()
	session := fake.session(t)
	observed := int64(-1)
	measured, err := driveAndSample(
		context.Background(),
		config{GenerationDir: "/tmp/generations/000007", Calls: calls, Seed: 42},
		[]*sdkmcp.ClientSession{session},
		func() []int {
			observed = fake.calls.Load()
			return []int{os.Getpid()}
		},
	)
	return measured, observed, err
}

// TestAnIdleArmAsksNothingBeforeTheSample is the measurement this benchmark
// could not take. The generation guard used to run first, so every arm had
// answered a graph_status before its bytes were read -- and one call is the
// entire load when the load under test is none.
func TestAnIdleArmAsksNothingBeforeTheSample(t *testing.T) {
	fake := &fakeServer{snapshotID: 7}
	measured, atSample, err := sampleAt(t, fake, 0)
	if err != nil {
		t.Fatalf("drive: %v", err)
	}
	if atSample != 0 {
		t.Fatalf("the server had answered %d calls when it was sampled, want 0", atSample)
	}
	if total := fake.calls.Load(); total != 1 {
		t.Fatalf("the run made %d calls in total, want exactly 1: the guard after the sample", total)
	}
	if measured.Symbols != 117499 || measured.SnapshotID != 7 {
		t.Fatalf("the guard did not report the generation: %+v", measured)
	}
	if measured.Latency.Calls != 0 || measured.Latency.P99MS != nil {
		t.Fatalf("an idle arm published a latency: %+v", measured.Latency)
	}
}

// TestALoadedArmIsSampledAfterItsWork is the other half of the same invariant:
// moving the guard must not have moved the workload.
func TestALoadedArmIsSampledAfterItsWork(t *testing.T) {
	fake := &fakeServer{snapshotID: 7}
	measured, atSample, err := sampleAt(t, fake, 10)
	if err != nil {
		t.Fatalf("drive: %v", err)
	}
	// One harvest plus ten workload calls, all before the sample.
	if atSample < 11 {
		t.Fatalf("the server had answered %d calls when it was sampled, want at least 11", atSample)
	}
	if measured.Latency.Calls != 10 || measured.Latency.P99MS == nil {
		t.Fatalf("a loaded arm published no latency: %+v", measured.Latency)
	}
}

// TestTheGuardStillRejectsTheWrongGeneration is why the guard cannot simply be
// deleted to make an idle run possible. A server pointed at another state
// directory answers exactly like the right one, and this benchmark has already
// been fooled by it once.
func TestTheGuardStillRejectsTheWrongGeneration(t *testing.T) {
	fake := &fakeServer{snapshotID: 9}
	if _, _, err := sampleAt(t, fake, 0); err == nil {
		t.Fatal("a server serving snapshot 9 passed a run that names generation 7")
	} else if !strings.Contains(err.Error(), "snapshot 9") || !strings.Contains(err.Error(), "generation 7") {
		t.Fatalf("the error does not name both sides: %v", err)
	}
}

// TestWhatWasNotMeasuredIsAbsentFromTheFile defends the published shape. A zero
// in these fields reads as a result -- an instant answer, an infinitely faster
// daemon -- and this benchmark has twice published a number that was not what a
// reader took it for.
func TestWhatWasNotMeasuredIsAbsentFromTheFile(t *testing.T) {
	idle := arm{Name: "daemon", Latency: latencyOf(nil), NewClientConnectMS: 4.5}
	encoded, err := json.Marshal(point{
		Clients:    1,
		Arms:       []arm{idle, idle},
		Comparison: compare(idle, idle),
	})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	for _, forbidden := range []string{"p50_ms", "p95_ms", "p99_ms", "new_client_ms", "p99_ratio", "new_client_speedup"} {
		if strings.Contains(string(encoded), forbidden) {
			t.Fatalf("an idle point published %q: %s", forbidden, encoded)
		}
	}
	if !strings.Contains(string(encoded), "new_client_connect_ms") {
		t.Fatalf("the connect wait is measured under every load and must be published: %s", encoded)
	}
}

// TestAnIdleRunThatAnsweredSomethingIsNotPublished defends the only guard that
// can catch a probe surviving on the no-call path. Those probes need a live
// server, so deleting the skip in `startServer` breaks nothing a laptop runs --
// which is exactly why the run itself has to refuse the file.
func TestAnIdleRunThatAnsweredSomethingIsNotPublished(t *testing.T) {
	answered := 12.5
	probed := arm{Name: "processes", FirstAnswersMS: []float64{answered}}
	if err := checkIdle(0, 4, arm{Name: "daemon"}, probed); err == nil {
		t.Fatal("an idle run that timed a first answer was accepted")
	} else if !strings.Contains(err.Error(), "processes") {
		t.Fatalf("the error does not name the arm that answered: %v", err)
	}
	if err := checkIdle(0, 4, arm{Name: "daemon", Latency: latencyOf([]int64{1})}); err == nil {
		t.Fatal("an idle run with answered calls was accepted")
	}
	// A loaded run answers by definition, and must not be rejected for it.
	if err := checkIdle(2000, 4, probed); err != nil {
		t.Fatalf("a loaded run was rejected for answering: %v", err)
	}
	if err := checkIdle(0, 4, arm{Name: "daemon"}, arm{Name: "processes"}); err != nil {
		t.Fatalf("a genuinely idle run was rejected: %v", err)
	}
}
