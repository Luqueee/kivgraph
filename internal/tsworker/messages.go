package tsworker

import (
	"errors"
	"fmt"
)

// Message types defined by section 3 of docs/protocol/ts-worker-v1.md.
// GET_STATUS is the normative name; the backlog abbreviates it as STATUS.
const (
	MessageHello         = "HELLO"
	MessageOpenWorkspace = "OPEN_WORKSPACE"
	MessageIndexProject  = "INDEX_PROJECT"
	MessageUpdateFiles   = "UPDATE_FILES"
	MessageRemoveFiles   = "REMOVE_FILES"
	MessageGetStatus     = "GET_STATUS"
	MessageCancel        = "CANCEL"
	MessageShutdown      = "SHUTDOWN"
	// MessageError is the reply type carrying a classified protocol error.
	MessageError = "ERROR"
)

// Protocol error codes from section 6. FrameTooLarge and VersionMismatch end
// the session; the rest keep it.
const (
	CodeVersionMismatch    = "VERSION_MISMATCH"
	CodeUnsupportedMessage = "UNSUPPORTED_MESSAGE"
	CodeInvalidPayload     = "INVALID_PAYLOAD"
	CodeUnknownProject     = "UNKNOWN_PROJECT"
	CodeBatchTooLarge      = "BATCH_TOO_LARGE"
	CodeFrameTooLarge      = "FRAME_TOO_LARGE"
	CodeEngineUnavailable  = "ENGINE_UNAVAILABLE"
	CodeCanceled           = "CANCELED"
	CodeInternal           = "INTERNAL"
)

// HelloRequest is the first frame of every session.
type HelloRequest struct {
	ProtocolVersions  []int  `json:"protocol_versions"`
	SupervisorVersion string `json:"supervisor_version"`
}

// SupportedTypeScript is the version window whose facts may be exact.
type SupportedTypeScript struct {
	Min string `json:"min"`
	Max string `json:"max"`
}

// HelloResponse is the worker capability announcement. Its limits bound every
// later request, so the supervisor validates them before accepting a session.
type HelloResponse struct {
	ProtocolVersion     int                 `json:"protocol_version"`
	WorkerVersion       string              `json:"worker_version"`
	Engine              string              `json:"engine"`
	EngineVersion       string              `json:"engine_version"`
	Runtime             string              `json:"runtime"`
	MaxConcurrent       int                 `json:"max_concurrent"`
	MaxFrameBytes       int                 `json:"max_frame_bytes"`
	MaxBatchPositions   int                 `json:"max_batch_positions"`
	SupportedTypeScript SupportedTypeScript `json:"supported_typescript"`
}

// Validate rejects a handshake the supervisor cannot honour. An announcement
// outside the protocol limits is a contract violation, not a negotiation.
func (response HelloResponse) Validate(offered []int) error {
	if !containsVersion(offered, response.ProtocolVersion) {
		return fmt.Errorf("worker selected protocol version %d, which was not offered", response.ProtocolVersion)
	}
	if response.MaxConcurrent < 1 {
		return fmt.Errorf("max_concurrent must be positive, got %d", response.MaxConcurrent)
	}
	if response.MaxFrameBytes < 1 || response.MaxFrameBytes > MaxFrameBytes {
		return fmt.Errorf("max_frame_bytes must be within 1..%d, got %d", MaxFrameBytes, response.MaxFrameBytes)
	}
	if response.MaxBatchPositions < 1 {
		return fmt.Errorf("max_batch_positions must be positive, got %d", response.MaxBatchPositions)
	}
	if response.WorkerVersion == "" {
		return errors.New("worker_version must not be empty")
	}
	if response.EngineVersion == "" {
		return errors.New("engine_version must not be empty")
	}
	return nil
}

func containsVersion(versions []int, wanted int) bool {
	for _, version := range versions {
		if version == wanted {
			return true
		}
	}
	return false
}

// CancelRequest asks the worker to stop a request already in flight.
type CancelRequest struct {
	TargetID uint64 `json:"target_id"`
}

// ErrorPayload is the body of an ERROR reply.
type ErrorPayload struct {
	Code      string `json:"code"`
	Message   string `json:"message"`
	Retryable bool   `json:"retryable"`
}

// WorkerError is an error reported by the worker itself. It is distinct from a
// supervisor failure: the session is healthy unless the code says otherwise.
type WorkerError struct {
	// Request is the message type that produced the error.
	Request   string
	Code      string
	Message   string
	Retryable bool
}

func (err *WorkerError) Error() string {
	return fmt.Sprintf("tsworker %s: %s: %s", err.Request, err.Code, err.Message)
}

// Fatal reports whether the code ends the session, per section 6.
func (err *WorkerError) Fatal() bool {
	return err != nil && (err.Code == CodeFrameTooLarge || err.Code == CodeVersionMismatch)
}
