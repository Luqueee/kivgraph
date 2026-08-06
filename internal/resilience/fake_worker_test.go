package resilience

import (
	"context"
	"os"
	"testing"

	"github.com/Luqueee/ladygraph/internal/tsworker"
)

// workerEnv makes the test binary re-execute itself as a protocol-speaking
// worker. Re-exec keeps the test hermetic: no Node.js, no build step, and the
// fake uses the production codec because it links the same package.
const workerEnv = "LADYGRAPH_RESILIENCE_WORKER"

func TestMain(m *testing.M) {
	if os.Getenv(workerEnv) != "" {
		os.Exit(runFakeWorker())
	}
	os.Exit(m.Run())
}

// runFakeWorker completes the handshake and then answers GET_STATUS until the
// stream ends. It is deliberately healthy: the failures in this package are
// injected from outside, with signals, so a worker that misbehaved on its own
// would blur what is being tested.
func runFakeWorker() int {
	ctx := context.Background()
	reader := tsworker.NewReader(os.Stdin)
	writer := tsworker.NewWriter(os.Stdout)

	hello, err := reader.ReadFrame(ctx)
	if err != nil {
		return 4
	}
	if !writeFrame(writer, hello.ID, tsworker.MessageHello, workerCapabilities()) {
		return 5
	}

	for {
		frame, err := reader.ReadFrame(ctx)
		if err != nil {
			return 0
		}
		switch frame.Type {
		case tsworker.MessageShutdown:
			writeFrame(writer, frame.ID, tsworker.MessageShutdown, map[string]any{"ok": true})
			return 0
		case tsworker.MessageGetStatus:
			writeFrame(writer, frame.ID, tsworker.MessageGetStatus, map[string]any{
				"projects_open":    0,
				"files_loaded":     0,
				"pending_requests": 0,
				"engine_alive":     true,
				"uptime_ms":        1,
			})
		default:
			writeFrame(writer, frame.ID, tsworker.MessageError, tsworker.ErrorPayload{
				Code:      tsworker.CodeUnsupportedMessage,
				Message:   "unsupported message " + frame.Type,
				Retryable: false,
			})
		}
	}
}

func workerCapabilities() tsworker.HelloResponse {
	return tsworker.HelloResponse{
		ProtocolVersion:     tsworker.ProtocolVersion,
		WorkerVersion:       "0.1.0-fake",
		Engine:              "typescript-native",
		EngineVersion:       "7.0.2",
		Runtime:             "go-test",
		MaxConcurrent:       4,
		MaxFrameBytes:       tsworker.MaxFrameBytes,
		MaxBatchPositions:   256,
		SupportedTypeScript: tsworker.SupportedTypeScript{Min: "5.0.0", Max: "7.0.2"},
	}
}

func writeFrame(writer *tsworker.Writer, id uint64, messageType string, payload any) bool {
	envelope, err := tsworker.NewEnvelope(id, messageType, payload)
	if err != nil {
		return false
	}
	return writer.WriteFrame(envelope) == nil
}
