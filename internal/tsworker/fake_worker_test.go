package tsworker

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"testing"
	"time"
)

// The supervisor tests drive a real child process. Re-executing the test
// binary keeps them hermetic: no Node.js, no build step, and the fake speaks
// the same codec as production because it is the same package.
const fakeWorkerEnv = "LADYGRAPH_FAKE_WORKER"

// Fake worker behaviours.
const (
	fakeHealthy           = "healthy"
	fakeExitBeforeHello   = "exit-before-hello"
	fakeSilentHandshake   = "silent-handshake"
	fakeRejectVersion     = "reject-version"
	fakeBadCapabilities   = "bad-capabilities"
	fakeCrashAfterHello   = "crash-after-hello"
	fakeCrashOnRequest    = "crash-on-request"
	fakeSilentRequest     = "silent-request"
	fakeCrashMidBatch     = "crash-mid-batch"
	fakeNoisyStderr       = "noisy-stderr"
	fakeIgnoreShutdown    = "ignore-shutdown"
	fakeIgnoreSignals     = "ignore-signals"
	fakeUnsolicitedFrames = "unsolicited-frames"
)

func TestMain(m *testing.M) {
	if behaviour := os.Getenv(fakeWorkerEnv); behaviour != "" {
		os.Exit(runFakeWorker(behaviour))
	}
	os.Exit(m.Run())
}

// newFakeSupervisor builds a supervisor wired to the fake worker.
func newFakeSupervisor(t *testing.T, behaviour string, tune func(*Options)) *Supervisor {
	t.Helper()

	executable, err := os.Executable()
	if err != nil {
		t.Fatalf("os.Executable() error = %v", err)
	}
	options := Options{
		Command:           executable,
		Env:               append(os.Environ(), fakeWorkerEnv+"="+behaviour),
		SupervisorVersion: "0.1.0-test",
		HandshakeTimeout:  2 * time.Second,
		RequestTimeout:    2 * time.Second,
		// A race-instrumented Go binary takes about a second to exit after its
		// last write, so a clean shutdown needs more room than production.
		ShutdownGrace:  3 * time.Second,
		RestartLimit:   2,
		RestartWindow:  time.Minute,
		RestartBackoff: 10 * time.Millisecond,
	}
	if tune != nil {
		tune(&options)
	}
	supervisor, err := NewSupervisor(options)
	if err != nil {
		t.Fatalf("NewSupervisor() error = %v", err)
	}
	t.Cleanup(func() { _ = supervisor.Close(context.Background()) })
	return supervisor
}

// fakeCapabilities is the handshake the healthy fake announces.
func fakeCapabilities() HelloResponse {
	return HelloResponse{
		ProtocolVersion:     ProtocolVersion,
		WorkerVersion:       "0.1.0-fake",
		Engine:              "typescript-native",
		EngineVersion:       "7.0.2",
		Runtime:             "go-test",
		MaxConcurrent:       4,
		MaxFrameBytes:       MaxFrameBytes,
		MaxBatchPositions:   256,
		SupportedTypeScript: SupportedTypeScript{Min: "5.0.0", Max: "7.0.2"},
	}
}

func runFakeWorker(behaviour string) int {
	if behaviour == fakeExitBeforeHello {
		return 3
	}
	if behaviour == fakeIgnoreSignals || behaviour == fakeIgnoreShutdown {
		ignored := make(chan os.Signal, 4)
		signal.Notify(ignored, syscall.SIGTERM, syscall.SIGINT)
		if behaviour == fakeIgnoreShutdown {
			// This fake ignores the SHUTDOWN message but still honours SIGTERM,
			// so the supervisor escalation stops at the first step.
			signal.Reset(syscall.SIGTERM)
		}
		go func() {
			for range ignored {
			}
		}()
	}

	ctx := context.Background()
	reader := NewReader(os.Stdin)
	writer := NewWriter(os.Stdout)

	hello, err := reader.ReadFrame(ctx)
	if err != nil {
		return 4
	}

	switch behaviour {
	case fakeSilentHandshake:
		blockForever()
	case fakeRejectVersion:
		writeFake(writer, hello.ID, MessageError, ErrorPayload{
			Code:      CodeVersionMismatch,
			Message:   "no common protocol version",
			Retryable: false,
		})
		return 0
	case fakeBadCapabilities:
		capabilities := fakeCapabilities()
		capabilities.MaxConcurrent = 0
		writeFake(writer, hello.ID, MessageHello, capabilities)
		blockForever()
	}

	writeFake(writer, hello.ID, MessageHello, fakeCapabilities())

	switch behaviour {
	case fakeCrashAfterHello:
		return 9
	case fakeNoisyStderr:
		fmt.Fprintln(os.Stderr, "worker: native server listening")
		fmt.Fprintln(os.Stderr, "worker: warm cache ready")
	case fakeUnsolicitedFrames:
		// A reply to an id nobody asked for, plus a well-formed event.
		writeFake(writer, 4242, MessageGetStatus, map[string]any{"stray": true})
		writeFake(writer, 0, "PROJECT_INDEXED", map[string]any{"project_id": "repo-a"})
	}

	for {
		frame, err := reader.ReadFrame(ctx)
		if err != nil {
			if behaviour == fakeIgnoreShutdown || behaviour == fakeIgnoreSignals {
				blockForever()
			}
			return 0
		}

		switch frame.Type {
		case MessageShutdown:
			if behaviour == fakeIgnoreShutdown || behaviour == fakeIgnoreSignals {
				continue
			}
			writeFake(writer, frame.ID, MessageShutdown, map[string]any{"ok": true})
			return 0
		case MessageCancel:
			var request CancelRequest
			_ = decodeFake(frame.Payload, &request)
			writeFake(writer, request.TargetID, MessageError, ErrorPayload{
				Code:      CodeCanceled,
				Message:   "request canceled",
				Retryable: true,
			})
			writeFake(writer, frame.ID, MessageCancel, map[string]any{"ok": true})
		case MessageGetStatus:
			switch behaviour {
			case fakeCrashOnRequest:
				os.Exit(7)
			case fakeSilentRequest:
				continue
			case fakeCrashMidBatch:
				writeFake(writer, 0, "FACTS", map[string]any{
					"request_id": frame.ID,
					"file":       "src/index.ts",
					"final":      false,
				})
				os.Exit(8)
			}
			writeFake(writer, frame.ID, MessageGetStatus, map[string]any{
				"projects_open":    1,
				"files_loaded":     2,
				"pending_requests": 0,
				"engine_alive":     true,
				"uptime_ms":        1,
				"echo":             frame.ID,
			})
		default:
			writeFake(writer, frame.ID, MessageError, ErrorPayload{
				Code:      CodeUnsupportedMessage,
				Message:   "unsupported message " + frame.Type,
				Retryable: false,
			})
		}
	}
}

func writeFake(writer *Writer, id uint64, messageType string, payload any) {
	envelope, err := NewEnvelope(id, messageType, payload)
	if err != nil {
		fmt.Fprintln(os.Stderr, "fake worker encode error: "+err.Error())
		return
	}
	if err := writer.WriteFrame(envelope); err != nil {
		fmt.Fprintln(os.Stderr, "fake worker write error: "+err.Error())
	}
}

func decodeFake(payload json.RawMessage, target any) error {
	return json.Unmarshal(payload, target)
}

// blockForever parks the fake worker. A bare `select {}` would trip the Go
// deadlock detector and exit the process, which is the opposite of the
// unresponsive worker these tests need.
func blockForever() {
	time.Sleep(time.Hour)
}

// processAlive reports whether the pid still exists.
func processAlive(pid int) bool {
	if pid <= 0 {
		return false
	}
	process, err := os.FindProcess(pid)
	if err != nil {
		return false
	}
	return process.Signal(syscall.Signal(0)) == nil
}

// waitForState polls until the supervisor reaches one of the wanted states.
func waitForState(t *testing.T, supervisor *Supervisor, timeout time.Duration, wanted ...State) Status {
	t.Helper()
	deadline := time.Now().Add(timeout)
	var status Status
	for time.Now().Before(deadline) {
		status = supervisor.Status()
		for _, state := range wanted {
			if status.State == state {
				return status
			}
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("state %q never reached %v (last error %q)", status.State, wanted, status.LastError)
	return status
}

// waitForNewSession polls until a session newer than previous is ready. The
// state alone is not enough: right after an invalidation the old session is
// still published as READY.
func waitForNewSession(t *testing.T, supervisor *Supervisor, timeout time.Duration, previous uint64) Status {
	t.Helper()
	deadline := time.Now().Add(timeout)
	var status Status
	for time.Now().Before(deadline) {
		status = supervisor.Status()
		if status.State == StateReady && status.Generation > previous {
			return status
		}
		if status.State == StateFailed || status.State == StateClosed {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("no session newer than %d became ready (state %q, last error %q)", previous, status.State, status.LastError)
	return status
}
