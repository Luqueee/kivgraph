package tools

import (
	"errors"
	"fmt"
	"slices"
)

// Stable tool error codes are part of the public MCP contract. Clients must
// branch on Code, never on the human-readable message.
const (
	CodeInvalidArgument       = "INVALID_ARGUMENT"
	CodeSymbolNotFound        = "SYMBOL_NOT_FOUND"
	CodeAmbiguousSymbol       = "AMBIGUOUS_SYMBOL"
	CodeRepositoryNotFound    = "REPOSITORY_NOT_FOUND"
	CodeCursorInvalid         = "CURSOR_INVALID"
	CodeCursorSnapshotExpired = "CURSOR_SNAPSHOT_EXPIRED"
	CodeTraversalLimitReached = "TRAVERSAL_LIMIT_REACHED"
	CodeSnapshotUnavailable   = "SNAPSHOT_UNAVAILABLE"
	CodeIndexNotReady         = "INDEX_NOT_READY"
	CodePermissionRequired    = "PERMISSION_REQUIRED"
	CodePermissionDenied      = "PERMISSION_DENIED"
	CodeIndexingInProgress    = "INDEXING_IN_PROGRESS"
	CodeIndexingFailed        = "INDEXING_FAILED"
)

// ToolError is both an error for Go callers and the JSON-serializable error
// payload used by MCP tool failures.
type ToolError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
	Details any    `json:"details,omitempty"`
	cause   error  `json:"-"`
}

func (err *ToolError) Error() string {
	if err == nil {
		return "<nil>"
	}
	if err.Message == "" {
		return err.Code
	}
	if err.Code == "" {
		return err.Message
	}
	return fmt.Sprintf("%s: %s", err.Code, err.Message)
}

// Unwrap preserves the underlying cause for errors.Is/errors.As while keeping
// internal details out of the wire representation.
func (err *ToolError) Unwrap() error {
	if err == nil {
		return nil
	}
	return err.cause
}

// NewToolError creates a classified tool error without an internal cause.
func NewToolError(code, message string) *ToolError {
	return &ToolError{Code: code, Message: message}
}

// WrapToolError classifies a failure and retains its cause for server-side
// diagnostics. The cause is deliberately omitted when the value is marshaled.
func WrapToolError(code, message string, cause error) *ToolError {
	return &ToolError{Code: code, Message: message, cause: cause}
}

// ErrIndexNotReady reports that no graph is published yet.
//
// It is the first answer a freshly installed client gets, so it names the two
// ways out instead of only the state. The code stays stable; only the message
// carries the guidance.
func ErrIndexNotReady() *ToolError {
	return NewToolError(CodeIndexNotReady,
		"no graph is published yet: start a project index with start_index_project, or run \"kivgraph index --full\"")
}

// RefusalCodes are the codes a tool returns when declining *is* the good
// answer, rather than the report of one that could not be produced.
//
// There is one today and ADR 0077 designed it. An ambiguous name is answered by
// naming the candidates, which costs `129` tokens where the `find_symbol` it
// replaced cost `750`, and the caller narrows by copying one of them. It leaves
// through the error path because it returns no rows -- and that is what needed
// separating, because every counter that reads the error path was scoring the
// good answer as a failure. Measured over five days, `29` of `find_references`'
// `63` "failures" were this.
//
// It is a list and not a constant so a second designed refusal has one place to
// join, and so the readers that classify -- `tool-stats`, and the bridge into
// `internal/metrics` -- read the vocabulary from the package that owns it
// instead of restating it.
func RefusalCodes() []string {
	return []string{CodeAmbiguousSymbol}
}

// IsRefusal reports whether err is one of those: an answer, not a failure.
func IsRefusal(err error) bool {
	code := ErrorCode(err)
	if code == "" {
		return false
	}
	return slices.Contains(RefusalCodes(), code)
}

// IsExpectedAbsence reports whether err says a lookup completed but found no
// matching symbol. The MCP result remains an error so clients can branch on
// its stable code, while the durable operator log renders this ordinary absence
// as not_found rather than an operational failure.
func IsExpectedAbsence(err error) bool {
	return ErrorCode(err) == CodeSymbolNotFound
}

// ErrorCode returns the stable public code carried by err, or an empty string
// when err is not a classified MCP tool error.
func ErrorCode(err error) string {
	var toolErr *ToolError
	if errors.As(err, &toolErr) {
		return toolErr.Code
	}
	return ""
}
