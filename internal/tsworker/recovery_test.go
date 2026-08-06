package tsworker

import (
	"context"
	"syscall"
	"testing"
	"time"
)

// TestSupervisorRecoversFromExternalSignals covers the two ways an operator or
// the kernel takes the worker away: a polite SIGTERM and an unblockable
// SIGKILL. Neither is a supervisor-initiated shutdown, so both must end in a
// new session serving requests again.
func TestSupervisorRecoversFromExternalSignals(t *testing.T) {
	for name, signalToSend := range map[string]syscall.Signal{
		"SIGTERM": syscall.SIGTERM,
		"SIGKILL": syscall.SIGKILL,
	} {
		t.Run(name, func(t *testing.T) {
			supervisor := newFakeSupervisor(t, fakeHealthy, nil)
			if err := supervisor.Start(context.Background()); err != nil {
				t.Fatalf("Start() error = %v", err)
			}
			before := supervisor.Status()
			if before.PID == 0 {
				t.Fatalf("Status() = %#v, want a running worker", before)
			}
			if _, err := supervisor.Call(context.Background(), MessageGetStatus, map[string]any{}); err != nil {
				t.Fatalf("Call() before the signal error = %v", err)
			}

			if err := syscall.Kill(before.PID, signalToSend); err != nil {
				t.Fatalf("Kill(%d, %v) error = %v", before.PID, signalToSend, err)
			}

			after := waitForNewSession(t, supervisor, 10*time.Second, before.Generation)
			if after.PID == before.PID {
				t.Fatalf("PID = %d, want a replacement worker", after.PID)
			}
			if after.Restarts == 0 {
				t.Fatalf("Restarts = %d, want the restart accounted for", after.Restarts)
			}
			if _, err := supervisor.Call(context.Background(), MessageGetStatus, map[string]any{}); err != nil {
				t.Fatalf("Call() after the signal error = %v", err)
			}
		})
	}
}

// TestSupervisorSurvivesAnInvalidPayloadFrame pins the one recoverable framing
// failure: the body was garbage but the frame boundary held, so the stream is
// still aligned and killing the session would be an overreaction.
func TestSupervisorSurvivesAnInvalidPayloadFrame(t *testing.T) {
	supervisor := newFakeSupervisor(t, fakeInvalidPayload, nil)
	if err := supervisor.Start(context.Background()); err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	started := supervisor.Status()

	deadline := time.Now().Add(5 * time.Second)
	var status Status
	for time.Now().Before(deadline) {
		status = supervisor.Status()
		if status.InvalidFrames > 0 {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if status.InvalidFrames == 0 {
		t.Fatalf("InvalidFrames = 0, want the malformed frame counted (%#v)", status)
	}
	if status.Generation != started.Generation || status.Restarts != started.Restarts {
		t.Fatalf("session changed after a recoverable frame: %#v -> %#v", started, status)
	}
	if _, err := supervisor.Call(context.Background(), MessageGetStatus, map[string]any{}); err != nil {
		t.Fatalf("Call() after an invalid payload error = %v", err)
	}
}

// TestSupervisorRestartsAfterFatalFramingFailures is the other half: once the
// byte stream itself is untrustworthy the protocol forbids resynchronising, so
// the session must die and be replaced.
func TestSupervisorRestartsAfterFatalFramingFailures(t *testing.T) {
	for name, behaviour := range map[string]string{
		"truncated frame": fakeCorruptStream,
		"oversized frame": fakeOversizedFrame,
	} {
		t.Run(name, func(t *testing.T) {
			supervisor := newFakeSupervisor(t, behaviour, func(options *Options) {
				options.RestartLimit = 1
				options.RestartBackoff = 5 * time.Millisecond
			})
			if err := supervisor.Start(context.Background()); err != nil {
				t.Fatalf("Start() error = %v", err)
			}
			started := supervisor.Status()

			status := waitForState(t, supervisor, 10*time.Second, StateFailed, StateRestarting)
			if status.Restarts == started.Restarts && status.Generation == started.Generation {
				t.Fatalf("supervisor kept a corrupted session: %#v", status)
			}
		})
	}
}

// TestSupervisorFailsClosedAfterACrashLoop states what happens when recovery
// itself cannot succeed: the supervisor stops burning restarts, reports FAILED,
// and every later call is refused with a classified code instead of hanging.
func TestSupervisorFailsClosedAfterACrashLoop(t *testing.T) {
	supervisor := newFakeSupervisor(t, fakeCrashAfterHello, func(options *Options) {
		options.RestartLimit = 2
		options.RestartWindow = time.Minute
		options.RestartBackoff = 5 * time.Millisecond
	})
	if err := supervisor.Start(context.Background()); err != nil {
		t.Fatalf("Start() error = %v", err)
	}

	status := waitForState(t, supervisor, 10*time.Second, StateFailed)
	if status.Restarts > 2 {
		t.Fatalf("Restarts = %d, want the budget to cap them at 2", status.Restarts)
	}
	_, err := supervisor.Call(context.Background(), MessageGetStatus, map[string]any{})
	if code := ErrorCode(err); code != CodeRestartLimit {
		t.Fatalf("Call() after the crash loop error code = %q, want %q (err=%v)", code, CodeRestartLimit, err)
	}
}

// TestSupervisorTimeoutInvalidatesAndRecovers completes the timeout contract:
// section 6 says a timed-out worker can no longer be trusted, and the session
// that replaces it must serve the next request.
func TestSupervisorTimeoutInvalidatesAndRecovers(t *testing.T) {
	supervisor := newFakeSupervisor(t, fakeSilentRequest, func(options *Options) {
		options.RequestTimeout = 150 * time.Millisecond
	})
	if err := supervisor.Start(context.Background()); err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	before := supervisor.Status()

	_, err := supervisor.Call(context.Background(), MessageGetStatus, map[string]any{})
	if code := ErrorCode(err); code != CodeTimeout {
		t.Fatalf("Call() error code = %q, want %q (err=%v)", code, CodeTimeout, err)
	}

	after := waitForNewSession(t, supervisor, 10*time.Second, before.Generation)
	if after.PID == before.PID {
		t.Fatalf("PID = %d, want the timed-out worker replaced", after.PID)
	}
}
