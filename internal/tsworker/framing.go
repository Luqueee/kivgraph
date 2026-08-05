// Package tsworker implements the Go side of the Luque TypeScript worker
// protocol described in docs/protocol/ts-worker-v1.md.
package tsworker

import (
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"sync"
	"time"
)

const (
	// ProtocolVersion is the only protocol version this package speaks.
	ProtocolVersion = 1
	// MaxFrameBytes is the largest body accepted or produced, per the protocol.
	MaxFrameBytes = 16 << 20
	// frameHeaderBytes is the size of the big-endian length prefix.
	frameHeaderBytes = 4
)

// FramingErrorKind classifies transport failures. Fatal kinds end the session
// because the byte stream can no longer be trusted; the protocol forbids
// resynchronising a corrupted stream.
type FramingErrorKind string

const (
	// FrameTooLarge reports a length prefix above MaxFrameBytes. Fatal.
	FrameTooLarge FramingErrorKind = "FRAME_TOO_LARGE"
	// FrameEmpty reports a zero-length body, which the protocol forbids. Fatal.
	FrameEmpty FramingErrorKind = "FRAME_EMPTY"
	// FrameTruncated reports end of input in the middle of a frame. Fatal.
	FrameTruncated FramingErrorKind = "FRAME_TRUNCATED"
	// InvalidPayload reports a well-read frame whose body is not a valid
	// envelope. Recoverable: the stream stays aligned.
	InvalidPayload FramingErrorKind = "INVALID_PAYLOAD"
	// VersionMismatch reports an envelope declaring another protocol version. Fatal.
	VersionMismatch FramingErrorKind = "VERSION_MISMATCH"
	// Timeout reports a read or write that exceeded its deadline. Fatal.
	Timeout FramingErrorKind = "TIMEOUT"
	// Canceled reports a read or write stopped by context cancellation. Fatal.
	Canceled FramingErrorKind = "CANCELED"
	// IOFailure reports an underlying transport failure. Fatal.
	IOFailure FramingErrorKind = "IO_FAILURE"
)

// ErrSessionClosed reports use of a reader or writer after Close.
var ErrSessionClosed = errors.New("tsworker session is closed")

// FramingError is a classified transport failure.
type FramingError struct {
	Kind FramingErrorKind
	Op   string
	Err  error
}

func (err *FramingError) Error() string {
	if err == nil {
		return "<nil>"
	}
	if err.Err == nil {
		return fmt.Sprintf("tsworker %s: %s", err.Op, err.Kind)
	}
	return fmt.Sprintf("tsworker %s: %s: %v", err.Op, err.Kind, err.Err)
}

func (err *FramingError) Unwrap() error {
	if err == nil {
		return nil
	}
	return err.Err
}

// Fatal reports whether the error invalidates the session. Only InvalidPayload
// leaves the stream usable, because the frame boundary was still honoured.
func (err *FramingError) Fatal() bool {
	return err != nil && err.Kind != InvalidPayload
}

func newFramingError(kind FramingErrorKind, op string, cause error) error {
	return &FramingError{Kind: kind, Op: op, Err: cause}
}

// Envelope is the outer object carried by every frame.
type Envelope struct {
	V       int             `json:"v"`
	ID      uint64          `json:"id"`
	Type    string          `json:"type"`
	Payload json.RawMessage `json:"payload"`
}

// Validate checks the invariants the protocol requires of every envelope.
func (envelope Envelope) Validate() error {
	if envelope.V != ProtocolVersion {
		return fmt.Errorf("protocol version %d is not supported", envelope.V)
	}
	if envelope.Type == "" {
		return errors.New("message type must not be empty")
	}
	if len(envelope.Payload) == 0 {
		return errors.New("payload must be present")
	}
	return nil
}

// deadlineSetter is implemented by *os.File pipes and net.Conn, which is how
// a blocking read is actually interrupted. A transport without it can only be
// cancelled between frames.
type deadlineSetter interface {
	SetReadDeadline(time.Time) error
}

// Reader decodes frames from a transport. It is not safe for concurrent use.
type Reader struct {
	source   io.Reader
	deadline deadlineSetter
	header   [frameHeaderBytes]byte
	body     []byte
	closed   bool
}

// NewReader builds a frame reader. When source supports read deadlines, an
// in-flight read is interrupted on context cancellation; otherwise the context
// is only observed between frames.
func NewReader(source io.Reader) *Reader {
	reader := &Reader{source: source}
	if candidate, ok := source.(deadlineSetter); ok {
		reader.deadline = candidate
	}
	return reader
}

// SupportsInterruption reports whether a blocked read can be cancelled.
func (reader *Reader) SupportsInterruption() bool {
	return reader != nil && reader.deadline != nil
}

// Close releases the reader. Further reads fail with ErrSessionClosed.
func (reader *Reader) Close() {
	if reader != nil {
		reader.closed = true
		reader.body = nil
	}
}

// ReadFrame reads exactly one frame and decodes its envelope. The returned
// payload slice is owned by the reader and is reused by the next call.
func (reader *Reader) ReadFrame(ctx context.Context) (Envelope, error) {
	if reader == nil || reader.closed {
		return Envelope{}, newFramingError(IOFailure, "read", ErrSessionClosed)
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return Envelope{}, newFramingError(Canceled, "read", err)
	}

	stop := reader.armDeadline(ctx)
	defer stop()

	if _, err := io.ReadFull(reader.source, reader.header[:]); err != nil {
		return Envelope{}, reader.classify(ctx, "read header", err, true)
	}
	length := binary.BigEndian.Uint32(reader.header[:])
	if length == 0 {
		return Envelope{}, newFramingError(FrameEmpty, "read header", nil)
	}
	// The length is validated before any allocation, so a hostile prefix cannot
	// make the process reserve 4 GiB.
	if length > MaxFrameBytes {
		return Envelope{}, newFramingError(FrameTooLarge, "read header", fmt.Errorf("length %d exceeds %d", length, MaxFrameBytes))
	}

	if uint32(cap(reader.body)) < length {
		reader.body = make([]byte, length)
	}
	body := reader.body[:length]
	if _, err := io.ReadFull(reader.source, body); err != nil {
		return Envelope{}, reader.classify(ctx, "read body", err, false)
	}

	var envelope Envelope
	if err := json.Unmarshal(body, &envelope); err != nil {
		return Envelope{}, newFramingError(InvalidPayload, "decode", err)
	}
	if envelope.V != ProtocolVersion {
		return Envelope{}, newFramingError(VersionMismatch, "decode", fmt.Errorf("protocol version %d is not supported", envelope.V))
	}
	if err := envelope.Validate(); err != nil {
		return Envelope{}, newFramingError(InvalidPayload, "decode", err)
	}
	return envelope, nil
}

// armDeadline interrupts a blocked read when the context ends, and restores the
// transport afterwards.
func (reader *Reader) armDeadline(ctx context.Context) func() {
	if reader.deadline == nil || ctx.Done() == nil {
		return func() {}
	}
	if deadline, ok := ctx.Deadline(); ok {
		_ = reader.deadline.SetReadDeadline(deadline)
	}
	finished := make(chan struct{})
	watcherDone := make(chan struct{})
	go func() {
		defer close(watcherDone)
		select {
		case <-ctx.Done():
			_ = reader.deadline.SetReadDeadline(time.Unix(1, 0))
		case <-finished:
		}
	}()
	return func() {
		// The watcher must be observed as finished before the deadline is
		// cleared; otherwise a late interruption would leak an expired
		// deadline into the next read.
		close(finished)
		<-watcherDone
		_ = reader.deadline.SetReadDeadline(time.Time{})
	}
}

// classify maps a transport failure onto a protocol error. A clean EOF between
// frames ends the session; the same EOF inside a frame is truncation.
func (reader *Reader) classify(ctx context.Context, op string, cause error, atBoundary bool) error {
	if ctxErr := ctx.Err(); ctxErr != nil {
		if errors.Is(ctxErr, context.DeadlineExceeded) {
			return newFramingError(Timeout, op, ctxErr)
		}
		return newFramingError(Canceled, op, ctxErr)
	}
	if errors.Is(cause, io.EOF) {
		if atBoundary {
			return io.EOF
		}
		return newFramingError(FrameTruncated, op, cause)
	}
	if errors.Is(cause, io.ErrUnexpectedEOF) {
		return newFramingError(FrameTruncated, op, cause)
	}
	if isTimeout(cause) {
		return newFramingError(Timeout, op, cause)
	}
	return newFramingError(IOFailure, op, cause)
}

func isTimeout(err error) bool {
	var timeout interface{ Timeout() bool }
	return errors.As(err, &timeout) && timeout.Timeout()
}

// Writer encodes frames onto a transport. It is safe for concurrent use so the
// supervisor can answer while emitting events.
type Writer struct {
	mu     sync.Mutex
	sink   io.Writer
	buffer []byte
	closed bool
}

// NewWriter builds a frame writer.
func NewWriter(sink io.Writer) *Writer {
	return &Writer{sink: sink}
}

// Close releases the writer. Further writes fail with ErrSessionClosed.
func (writer *Writer) Close() {
	if writer == nil {
		return
	}
	writer.mu.Lock()
	writer.closed = true
	writer.buffer = nil
	writer.mu.Unlock()
}

// WriteFrame encodes one envelope as a single frame.
func (writer *Writer) WriteFrame(envelope Envelope) error {
	if writer == nil {
		return newFramingError(IOFailure, "write", ErrSessionClosed)
	}
	if err := envelope.Validate(); err != nil {
		return newFramingError(InvalidPayload, "encode", err)
	}
	body, err := json.Marshal(envelope)
	if err != nil {
		return newFramingError(InvalidPayload, "encode", err)
	}
	if len(body) > MaxFrameBytes {
		return newFramingError(FrameTooLarge, "encode", fmt.Errorf("length %d exceeds %d", len(body), MaxFrameBytes))
	}

	writer.mu.Lock()
	defer writer.mu.Unlock()
	if writer.closed {
		return newFramingError(IOFailure, "write", ErrSessionClosed)
	}
	total := frameHeaderBytes + len(body)
	if cap(writer.buffer) < total {
		writer.buffer = make([]byte, total)
	}
	frame := writer.buffer[:total]
	binary.BigEndian.PutUint32(frame[:frameHeaderBytes], uint32(len(body)))
	copy(frame[frameHeaderBytes:], body)
	if _, err := writer.sink.Write(frame); err != nil {
		if isTimeout(err) {
			return newFramingError(Timeout, "write", err)
		}
		return newFramingError(IOFailure, "write", err)
	}
	return nil
}

// NewEnvelope builds a validated envelope with the current protocol version.
func NewEnvelope(id uint64, messageType string, payload any) (Envelope, error) {
	encoded, err := json.Marshal(payload)
	if err != nil {
		return Envelope{}, newFramingError(InvalidPayload, "encode", err)
	}
	envelope := Envelope{V: ProtocolVersion, ID: id, Type: messageType, Payload: encoded}
	if err := envelope.Validate(); err != nil {
		return Envelope{}, newFramingError(InvalidPayload, "encode", err)
	}
	return envelope, nil
}
