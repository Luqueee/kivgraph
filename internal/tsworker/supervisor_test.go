package tsworker

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Luqueee/kivgraph/internal/metrics"
)

// --- arranque y handshake -------------------------------------------------

func TestSupervisorStartCompletesHandshakeAndExposesCapabilities(t *testing.T) {
	supervisor := newFakeSupervisor(t, fakeHealthy, nil)
	if err := supervisor.Start(context.Background()); err != nil {
		t.Fatalf("Start() error = %v", err)
	}

	status := supervisor.Status()
	if status.State != StateReady {
		t.Fatalf("State = %q, want %q", status.State, StateReady)
	}
	if status.Starts != 1 || status.Restarts != 0 {
		t.Fatalf("Starts/Restarts = %d/%d, want 1/0", status.Starts, status.Restarts)
	}
	if status.PID == 0 || !processAlive(status.PID) {
		t.Fatalf("PID = %d, want a live process", status.PID)
	}
	if status.Handshake == nil {
		t.Fatal("Handshake is nil after a successful start")
	}
	if got, want := status.Handshake.EngineVersion, fakeCapabilities().EngineVersion; got != want {
		t.Fatalf("EngineVersion = %q, want %q", got, want)
	}
	if got := status.Handshake.MaxBatchPositions; got != 256 {
		t.Fatalf("MaxBatchPositions = %d, want 256", got)
	}
}

func TestSupervisorStartRejectsHandshakeOutsideProtocolLimits(t *testing.T) {
	supervisor := newFakeSupervisor(t, fakeBadCapabilities, nil)
	err := supervisor.Start(context.Background())
	if err == nil {
		t.Fatal("Start() accepted a handshake announcing max_concurrent 0")
	}
	if code := ErrorCode(err); code != CodeVersionMismatch {
		t.Fatalf("ErrorCode = %q, want %q (err %v)", code, CodeVersionMismatch, err)
	}
	if state := supervisor.Status().State; state != StateStopped {
		t.Fatalf("State = %q, want %q", state, StateStopped)
	}
}

func TestSupervisorStartSurfacesWorkerVersionMismatch(t *testing.T) {
	supervisor := newFakeSupervisor(t, fakeRejectVersion, nil)
	err := supervisor.Start(context.Background())
	if err == nil {
		t.Fatal("Start() succeeded against a worker that rejected the version")
	}
	var workerErr *WorkerError
	if !errors.As(err, &workerErr) {
		t.Fatalf("error type = %T, want *WorkerError (err %v)", err, err)
	}
	if workerErr.Code != CodeVersionMismatch || !workerErr.Fatal() {
		t.Fatalf("WorkerError = %+v, want a fatal VERSION_MISMATCH", workerErr)
	}
}

func TestSupervisorStartFailsWhenWorkerDiesBeforeHandshake(t *testing.T) {
	supervisor := newFakeSupervisor(t, fakeExitBeforeHello, nil)
	err := supervisor.Start(context.Background())
	if err == nil {
		t.Fatal("Start() succeeded against a worker that exited immediately")
	}
	if code := ErrorCode(err); code != CodeHandshake {
		t.Fatalf("ErrorCode = %q, want %q (err %v)", code, CodeHandshake, err)
	}
}

// --- timeout --------------------------------------------------------------

func TestSupervisorHandshakeTimesOut(t *testing.T) {
	supervisor := newFakeSupervisor(t, fakeSilentHandshake, func(options *Options) {
		options.HandshakeTimeout = 150 * time.Millisecond
	})

	start := time.Now()
	err := supervisor.Start(context.Background())
	elapsed := time.Since(start)

	if err == nil {
		t.Fatal("Start() succeeded against a worker that never answers HELLO")
	}
	if code := ErrorCode(err); code != CodeTimeout {
		t.Fatalf("ErrorCode = %q, want %q (err %v)", code, CodeTimeout, err)
	}
	if elapsed > 2*time.Second {
		t.Fatalf("handshake took %s, want it bounded by the timeout", elapsed)
	}
}

func TestSupervisorRequestTimeoutInvalidatesTheSession(t *testing.T) {
	supervisor := newFakeSupervisor(t, fakeSilentRequest, func(options *Options) {
		options.RequestTimeout = 150 * time.Millisecond
	})
	if err := supervisor.Start(context.Background()); err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	firstGeneration := supervisor.Status().Generation

	_, err := supervisor.Call(context.Background(), MessageGetStatus, struct{}{})
	if code := ErrorCode(err); code != CodeTimeout {
		t.Fatalf("ErrorCode = %q, want %q (err %v)", code, CodeTimeout, err)
	}

	// Section 6: a timeout invalidates the worker state, so the session is
	// replaced instead of being reused.
	status := waitForNewSession(t, supervisor, 5*time.Second, firstGeneration)
	if status.Restarts != 1 {
		t.Fatalf("Restarts = %d, want 1", status.Restarts)
	}
}

// --- cancelación ----------------------------------------------------------

func TestSupervisorCallerCancellationKeepsTheSession(t *testing.T) {
	supervisor := newFakeSupervisor(t, fakeSilentRequest, func(options *Options) {
		options.RequestTimeout = 5 * time.Second
	})
	if err := supervisor.Start(context.Background()); err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	generation := supervisor.Status().Generation

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	_, err := supervisor.Call(ctx, MessageGetStatus, struct{}{})
	if code := ErrorCode(err); code != CodeCanceled {
		t.Fatalf("ErrorCode = %q, want %q (err %v)", code, CodeCanceled, err)
	}

	// A cancelled request is not a broken worker: section 3.7 keeps the
	// session, so no restart may happen.
	time.Sleep(150 * time.Millisecond)
	status := supervisor.Status()
	if status.State != StateReady || status.Generation != generation {
		t.Fatalf("Status = %q/gen %d, want READY on generation %d", status.State, status.Generation, generation)
	}
	if status.Restarts != 0 {
		t.Fatalf("Restarts = %d, want 0 after a caller cancellation", status.Restarts)
	}
	if status.PendingRequests != 0 {
		t.Fatalf("PendingRequests = %d, want 0", status.PendingRequests)
	}
}

// --- respuestas y errores del worker --------------------------------------

func TestSupervisorCorrelatesConcurrentReplies(t *testing.T) {
	supervisor := newFakeSupervisor(t, fakeHealthy, nil)
	if err := supervisor.Start(context.Background()); err != nil {
		t.Fatalf("Start() error = %v", err)
	}

	const callers = 8
	var group sync.WaitGroup
	echoes := make([]uint64, callers)
	failures := make([]error, callers)
	for index := range callers {
		group.Add(1)
		go func() {
			defer group.Done()
			payload, err := supervisor.Call(context.Background(), MessageGetStatus, struct{}{})
			if err != nil {
				failures[index] = err
				return
			}
			var body struct {
				Echo uint64 `json:"echo"`
			}
			if err := json.Unmarshal(payload, &body); err != nil {
				failures[index] = err
				return
			}
			echoes[index] = body.Echo
		}()
	}
	group.Wait()

	seen := make(map[uint64]bool, callers)
	for index, err := range failures {
		if err != nil {
			t.Fatalf("Call(%d) error = %v", index, err)
		}
		if echoes[index] == 0 || seen[echoes[index]] {
			t.Fatalf("reply %d carried id %d, want a unique non-zero id", index, echoes[index])
		}
		seen[echoes[index]] = true
	}
}

func TestSupervisorReportsWorkerErrorsWithoutKillingTheSession(t *testing.T) {
	supervisor := newFakeSupervisor(t, fakeHealthy, nil)
	if err := supervisor.Start(context.Background()); err != nil {
		t.Fatalf("Start() error = %v", err)
	}

	_, err := supervisor.Call(context.Background(), MessageIndexProject, map[string]any{"project_id": "repo-a"})
	var workerErr *WorkerError
	if !errors.As(err, &workerErr) {
		t.Fatalf("error type = %T, want *WorkerError (err %v)", err, err)
	}
	if workerErr.Code != CodeUnsupportedMessage || workerErr.Fatal() {
		t.Fatalf("WorkerError = %+v, want a non-fatal UNSUPPORTED_MESSAGE", workerErr)
	}
	if state := supervisor.Status().State; state != StateReady {
		t.Fatalf("State = %q, want the session to survive a worker error", state)
	}
	if _, err := supervisor.Call(context.Background(), MessageGetStatus, struct{}{}); err != nil {
		t.Fatalf("Call() after a worker error = %v", err)
	}
}

func TestSupervisorIgnoresUnmatchedRepliesAndDeliversEvents(t *testing.T) {
	events := make(chan Event, 4)
	supervisor := newFakeSupervisor(t, fakeUnsolicitedFrames, func(options *Options) {
		options.OnEvent = func(event Event) { events <- event }
	})
	if err := supervisor.Start(context.Background()); err != nil {
		t.Fatalf("Start() error = %v", err)
	}

	select {
	case event := <-events:
		if event.Type != "PROJECT_INDEXED" {
			t.Fatalf("event type = %q, want PROJECT_INDEXED", event.Type)
		}
		if event.Generation == 0 {
			t.Fatal("event generation is zero")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("no event delivered")
	}

	// The stray reply must not break the session or be mistaken for an event.
	if _, err := supervisor.Call(context.Background(), MessageGetStatus, struct{}{}); err != nil {
		t.Fatalf("Call() after an unmatched reply = %v", err)
	}
	status := supervisor.Status()
	if status.State != StateReady {
		t.Fatalf("State = %q, want READY", status.State)
	}
	// The anomaly is counted, not silently dropped and not disguised as worker
	// stderr output.
	if status.UnmatchedReplies != 1 {
		t.Fatalf("UnmatchedReplies = %d, want 1", status.UnmatchedReplies)
	}
	if len(status.StderrTail) != 0 {
		t.Fatalf("StderrTail = %v, want no worker output", status.StderrTail)
	}
}

// --- reinicio y lotes incompletos -----------------------------------------

func TestSupervisorRestartsAfterAnUnexpectedExit(t *testing.T) {
	supervisor := newFakeSupervisor(t, fakeCrashOnRequest, nil)
	if err := supervisor.Start(context.Background()); err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	firstGeneration := supervisor.Status().Generation

	_, err := supervisor.Call(context.Background(), MessageGetStatus, struct{}{})
	if code := ErrorCode(err); code != CodeEngineUnavailable {
		t.Fatalf("ErrorCode = %q, want %q (err %v)", code, CodeEngineUnavailable, err)
	}

	status := waitForState(t, supervisor, 5*time.Second, StateReady)
	if status.Generation <= firstGeneration {
		t.Fatalf("Generation = %d, want greater than %d", status.Generation, firstGeneration)
	}
	if status.Starts != 2 || status.Restarts != 1 {
		t.Fatalf("Starts/Restarts = %d/%d, want 2/1", status.Starts, status.Restarts)
	}
	if status.Handshake == nil {
		t.Fatal("the restarted session has no handshake")
	}
}

func TestSupervisorReportsPartialBatchesAsLostOnSessionDeath(t *testing.T) {
	var (
		mu     sync.Mutex
		facts  []Event
		losses []SessionLoss
	)
	supervisor := newFakeSupervisor(t, fakeCrashMidBatch, func(options *Options) {
		options.OnEvent = func(event Event) {
			mu.Lock()
			facts = append(facts, event)
			mu.Unlock()
		}
		options.OnSessionLost = func(loss SessionLoss) {
			mu.Lock()
			losses = append(losses, loss)
			mu.Unlock()
		}
	})
	if err := supervisor.Start(context.Background()); err != nil {
		t.Fatalf("Start() error = %v", err)
	}

	_, err := supervisor.Call(context.Background(), MessageGetStatus, struct{}{})
	if code := ErrorCode(err); code != CodeEngineUnavailable {
		t.Fatalf("ErrorCode = %q, want %q (err %v)", code, CodeEngineUnavailable, err)
	}
	waitForState(t, supervisor, 5*time.Second, StateReady, StateFailed)

	mu.Lock()
	defer mu.Unlock()
	if len(facts) == 0 {
		t.Fatal("no FACTS event was delivered before the crash")
	}
	var partial bool
	for _, event := range facts {
		if event.Type == "FACTS" && strings.Contains(string(event.Payload), `"final":false`) {
			partial = true
		}
	}
	if !partial {
		t.Fatal("expected a non-final FACTS batch before the crash")
	}
	if len(losses) != 1 {
		t.Fatalf("SessionLoss count = %d, want 1", len(losses))
	}
	// The aborted request id is what lets the consumer drop the half-emitted
	// batch instead of committing it.
	if len(losses[0].Pending) != 1 {
		t.Fatalf("SessionLoss.Pending = %v, want exactly the aborted request", losses[0].Pending)
	}
	if !losses[0].WillRestart {
		t.Fatal("SessionLoss.WillRestart = false, want a restart within budget")
	}
}

// --- límite de reinicios --------------------------------------------------

func TestSupervisorStopsRestartingAfterTheBudget(t *testing.T) {
	supervisor := newFakeSupervisor(t, fakeCrashAfterHello, func(options *Options) {
		options.RestartLimit = 2
		options.RestartBackoff = 5 * time.Millisecond
	})
	if err := supervisor.Start(context.Background()); err != nil {
		t.Fatalf("Start() error = %v", err)
	}

	status := waitForState(t, supervisor, 10*time.Second, StateFailed)
	if status.Restarts != 2 {
		t.Fatalf("Restarts = %d, want the configured limit of 2", status.Restarts)
	}

	// A crash loop must fail fast instead of spinning forever.
	_, err := supervisor.Call(context.Background(), MessageGetStatus, struct{}{})
	if code := ErrorCode(err); code != CodeRestartLimit {
		t.Fatalf("ErrorCode = %q, want %q (err %v)", code, CodeRestartLimit, err)
	}
	if status.LastError == "" {
		t.Fatal("LastError is empty after exhausting the restart budget")
	}
}

// --- stderr separado ------------------------------------------------------

func TestSupervisorCapturesStderrOutsideTheProtocol(t *testing.T) {
	lines := make(chan string, 8)
	supervisor := newFakeSupervisor(t, fakeNoisyStderr, func(options *Options) {
		options.OnStderr = func(line string) { lines <- line }
	})
	if err := supervisor.Start(context.Background()); err != nil {
		t.Fatalf("Start() error = %v", err)
	}

	seen := make([]string, 0, 2)
	deadline := time.After(2 * time.Second)
	for len(seen) < 2 {
		select {
		case line := <-lines:
			seen = append(seen, line)
		case <-deadline:
			t.Fatalf("stderr lines seen = %v, want 2", seen)
		}
	}
	if !strings.Contains(seen[0], "native server listening") {
		t.Fatalf("first stderr line = %q", seen[0])
	}

	// stderr noise must not disturb the protocol stream.
	if _, err := supervisor.Call(context.Background(), MessageGetStatus, struct{}{}); err != nil {
		t.Fatalf("Call() after stderr output = %v", err)
	}
	tail := supervisor.Status().StderrTail
	if len(tail) < 2 {
		t.Fatalf("StderrTail = %v, want the retained lines", tail)
	}
}

func TestStderrTailKeepsTheMostRecentLines(t *testing.T) {
	ring := newLineRing(3)
	for _, line := range []string{"a", "b", "c", "d", "e"} {
		ring.add(line)
	}
	got := ring.lines()
	want := []string{"c", "d", "e"}
	if len(got) != len(want) {
		t.Fatalf("lines() = %v, want %v", got, want)
	}
	for index := range want {
		if got[index] != want[index] {
			t.Fatalf("lines() = %v, want %v", got, want)
		}
	}
}

// --- shutdown -------------------------------------------------------------

func TestSupervisorShutdownIsClean(t *testing.T) {
	supervisor := newFakeSupervisor(t, fakeHealthy, nil)
	if err := supervisor.Start(context.Background()); err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	pid := supervisor.Status().PID

	if err := supervisor.Close(context.Background()); err != nil {
		t.Fatalf("Close() error = %v", err)
	}
	status := supervisor.Status()
	if status.State != StateClosed {
		t.Fatalf("State = %q, want %q", status.State, StateClosed)
	}
	if status.ForcedKills != 0 {
		t.Fatalf("ForcedKills = %d, want 0 for a worker that honours SHUTDOWN", status.ForcedKills)
	}
	if status.LastError != "" {
		t.Fatalf("LastError = %q, want empty after a clean shutdown", status.LastError)
	}
	if processAlive(pid) {
		t.Fatalf("pid %d is still alive after Close()", pid)
	}
	if _, err := supervisor.Call(context.Background(), MessageGetStatus, struct{}{}); ErrorCode(err) != CodeClosed {
		t.Fatalf("Call() after Close() = %v, want %q", err, CodeClosed)
	}
	if err := supervisor.Close(context.Background()); err != nil {
		t.Fatalf("second Close() error = %v", err)
	}
}

func TestSupervisorTerminatesAWorkerThatIgnoresShutdown(t *testing.T) {
	supervisor := newFakeSupervisor(t, fakeIgnoreShutdown, func(options *Options) {
		options.ShutdownGrace = 150 * time.Millisecond
	})
	if err := supervisor.Start(context.Background()); err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	pid := supervisor.Status().PID

	err := supervisor.Close(context.Background())
	if code := ErrorCode(err); code != CodeShutdownForced {
		t.Fatalf("ErrorCode = %q, want %q (err %v)", code, CodeShutdownForced, err)
	}
	if got := supervisor.Status().ForcedKills; got != 1 {
		t.Fatalf("ForcedKills = %d, want 1", got)
	}
	if processAlive(pid) {
		t.Fatalf("pid %d survived a forced shutdown", pid)
	}
}

func TestSupervisorKillsAWorkerThatIgnoresSignals(t *testing.T) {
	supervisor := newFakeSupervisor(t, fakeIgnoreSignals, func(options *Options) {
		options.ShutdownGrace = 150 * time.Millisecond
	})
	if err := supervisor.Start(context.Background()); err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	pid := supervisor.Status().PID

	err := supervisor.Close(context.Background())
	if code := ErrorCode(err); code != CodeShutdownForced {
		t.Fatalf("ErrorCode = %q, want %q (err %v)", code, CodeShutdownForced, err)
	}
	if !strings.Contains(err.Error(), "killed") {
		t.Fatalf("error = %v, want the escalation to reach the kill step", err)
	}
	if processAlive(pid) {
		t.Fatalf("pid %d survived SIGKILL", pid)
	}
}

// --- estado observable ----------------------------------------------------

func TestSupervisorRejectsCallsBeforeStart(t *testing.T) {
	supervisor := newFakeSupervisor(t, fakeHealthy, nil)
	if state := supervisor.Status().State; state != StateStopped {
		t.Fatalf("State = %q, want %q", state, StateStopped)
	}
	_, err := supervisor.Call(context.Background(), MessageGetStatus, struct{}{})
	if code := ErrorCode(err); code != CodeNotStarted {
		t.Fatalf("ErrorCode = %q, want %q (err %v)", code, CodeNotStarted, err)
	}
}

func TestSupervisorRejectsASecondStart(t *testing.T) {
	supervisor := newFakeSupervisor(t, fakeHealthy, nil)
	if err := supervisor.Start(context.Background()); err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	if err := supervisor.Start(context.Background()); err == nil {
		t.Fatal("a second Start() was accepted")
	}
	if got := supervisor.Status().Starts; got != 1 {
		t.Fatalf("Starts = %d, want 1", got)
	}
}

func TestNewSupervisorRequiresACommand(t *testing.T) {
	if _, err := NewSupervisor(Options{}); ErrorCode(err) != CodeSpawn {
		t.Fatalf("NewSupervisor() error = %v, want %q", err, CodeSpawn)
	}
}

func TestSupervisorRecordsRestartMetrics(t *testing.T) {
	registry := metrics.NewRegistry()
	supervisor := &Supervisor{
		options: Options{Metrics: registry, RestartWindow: time.Minute},
	}

	supervisor.mu.Lock()
	supervisor.recordRestartLocked()
	supervisor.recordRestartLocked()
	supervisor.mu.Unlock()

	observed := registry.Report().Worker
	if observed.Restarts != 2 {
		t.Fatalf("worker metrics = %+v, want two restarts", observed)
	}
}

func TestSupervisorRecordsWorkerMemoryMetrics(t *testing.T) {
	registry := metrics.NewRegistry()
	supervisor := newFakeSupervisor(t, fakeHealthy, func(options *Options) {
		options.Metrics = registry
	})
	if err := supervisor.Start(context.Background()); err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	if status := supervisor.Status(); status.PID == 0 {
		t.Fatalf("Status().PID = %d, want a running worker", status.PID)
	}
	if got := registry.Report().Worker.MemoryBytes; got <= 0 {
		t.Fatalf("worker memory metric = %d, want a positive resident size", got)
	}
}
