package tsworker

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"io"
	"os"
	"testing"
	"time"
)

func mustEnvelope(t *testing.T, id uint64, messageType string, payload any) Envelope {
	t.Helper()
	envelope, err := NewEnvelope(id, messageType, payload)
	if err != nil {
		t.Fatalf("NewEnvelope() error = %v", err)
	}
	return envelope
}

func framingKind(t *testing.T, err error) FramingErrorKind {
	t.Helper()
	var framing *FramingError
	if !errors.As(err, &framing) {
		t.Fatalf("error %v is not a *FramingError", err)
	}
	return framing.Kind
}

func TestWriteFrameProducesBigEndianLengthPrefixedJSON(t *testing.T) {
	sink := &bytes.Buffer{}
	writer := NewWriter(sink)
	envelope := mustEnvelope(t, 7, "HELLO", map[string]any{"supervisor_version": "0.1.0-dev"})
	if err := writer.WriteFrame(envelope); err != nil {
		t.Fatalf("WriteFrame() error = %v", err)
	}

	frame := sink.Bytes()
	if len(frame) < frameHeaderBytes {
		t.Fatalf("frame is shorter than its header: %d bytes", len(frame))
	}
	declared := binary.BigEndian.Uint32(frame[:frameHeaderBytes])
	body := frame[frameHeaderBytes:]
	if int(declared) != len(body) {
		t.Fatalf("length prefix = %d, body = %d bytes", declared, len(body))
	}
	// The prefix must be big-endian on the wire, not host order.
	if !bytes.Equal(frame[:frameHeaderBytes], []byte{0, 0, byte(len(body) >> 8), byte(len(body))}) {
		t.Fatalf("length prefix is not big-endian: %v", frame[:frameHeaderBytes])
	}
	if body[0] != '{' || body[len(body)-1] != '}' {
		t.Fatalf("body is not a bare JSON object: %q", body)
	}

	decoded, err := NewReader(bytes.NewReader(frame)).ReadFrame(context.Background())
	if err != nil {
		t.Fatalf("ReadFrame() error = %v", err)
	}
	if decoded.V != ProtocolVersion || decoded.ID != 7 || decoded.Type != "HELLO" {
		t.Fatalf("round-tripped envelope = %#v", decoded)
	}
	if string(decoded.Payload) != string(envelope.Payload) {
		t.Fatalf("payload = %s, want %s", decoded.Payload, envelope.Payload)
	}
}

func TestReadFrameConsumesConsecutiveFramesInOrder(t *testing.T) {
	sink := &bytes.Buffer{}
	writer := NewWriter(sink)
	for index := uint64(1); index <= 3; index++ {
		if err := writer.WriteFrame(mustEnvelope(t, index, "GET_STATUS", map[string]any{"n": index})); err != nil {
			t.Fatalf("WriteFrame(%d) error = %v", index, err)
		}
	}

	reader := NewReader(bytes.NewReader(sink.Bytes()))
	for index := uint64(1); index <= 3; index++ {
		envelope, err := reader.ReadFrame(context.Background())
		if err != nil {
			t.Fatalf("ReadFrame(%d) error = %v", index, err)
		}
		if envelope.ID != index {
			t.Fatalf("frame %d has id %d", index, envelope.ID)
		}
	}
	if _, err := reader.ReadFrame(context.Background()); !errors.Is(err, io.EOF) {
		t.Fatalf("end of stream error = %v, want io.EOF", err)
	}
}

func TestReadFrameWaitsForPartialFrameAndReportsTruncation(t *testing.T) {
	sink := &bytes.Buffer{}
	if err := NewWriter(sink).WriteFrame(mustEnvelope(t, 1, "INDEX_PROJECT", map[string]any{"project_id": "p"})); err != nil {
		t.Fatalf("WriteFrame() error = %v", err)
	}
	complete := sink.Bytes()

	incoming, outgoing, err := os.Pipe()
	if err != nil {
		t.Fatalf("Pipe() error = %v", err)
	}
	defer incoming.Close()

	go func() {
		defer outgoing.Close()
		_, _ = outgoing.Write(complete[:frameHeaderBytes+2])
		time.Sleep(30 * time.Millisecond)
		_, _ = outgoing.Write(complete[frameHeaderBytes+2:])
	}()

	reader := NewReader(incoming)
	envelope, err := reader.ReadFrame(context.Background())
	if err != nil {
		t.Fatalf("ReadFrame() over a split frame error = %v", err)
	}
	if envelope.Type != "INDEX_PROJECT" {
		t.Fatalf("envelope = %#v", envelope)
	}

	truncated := NewReader(bytes.NewReader(complete[:len(complete)-3]))
	if _, err := truncated.ReadFrame(context.Background()); framingKind(t, err) != FrameTruncated {
		t.Fatalf("truncated frame error = %v, want FRAME_TRUNCATED", err)
	}
}

func TestReadFrameRejectsOversizedLengthWithoutAllocating(t *testing.T) {
	header := make([]byte, frameHeaderBytes)
	binary.BigEndian.PutUint32(header, MaxFrameBytes+1)
	reader := NewReader(bytes.NewReader(header))

	_, err := reader.ReadFrame(context.Background())
	if framingKind(t, err) != FrameTooLarge {
		t.Fatalf("oversized frame error = %v, want FRAME_TOO_LARGE", err)
	}
	// A hostile prefix must never make the reader reserve the announced size.
	if cap(reader.body) >= MaxFrameBytes {
		t.Fatalf("reader allocated %d bytes for a rejected frame", cap(reader.body))
	}

	empty := make([]byte, frameHeaderBytes)
	if _, err := NewReader(bytes.NewReader(empty)).ReadFrame(context.Background()); framingKind(t, err) != FrameEmpty {
		t.Fatalf("empty frame error = %v, want FRAME_EMPTY", err)
	}
}

func TestReadFrameClassifiesInvalidPayloadAsRecoverable(t *testing.T) {
	sink := &bytes.Buffer{}
	appendRaw := func(body string) {
		header := make([]byte, frameHeaderBytes)
		binary.BigEndian.PutUint32(header, uint32(len(body)))
		sink.Write(header)
		sink.WriteString(body)
	}
	appendRaw(`{"v":1,"id":1,"type":"HELLO"`)
	appendRaw(`{"v":1,"id":2,"type":"","payload":{}}`)
	if err := NewWriter(sink).WriteFrame(mustEnvelope(t, 3, "GET_STATUS", map[string]any{})); err != nil {
		t.Fatalf("WriteFrame() error = %v", err)
	}

	reader := NewReader(bytes.NewReader(sink.Bytes()))
	for _, want := range []FramingErrorKind{InvalidPayload, InvalidPayload} {
		_, err := reader.ReadFrame(context.Background())
		if framingKind(t, err) != want {
			t.Fatalf("error = %v, want %s", err, want)
		}
		var framing *FramingError
		errors.As(err, &framing)
		if framing.Fatal() {
			t.Fatal("INVALID_PAYLOAD must keep the session usable")
		}
	}
	// The stream stayed aligned, so the following valid frame still decodes.
	envelope, err := reader.ReadFrame(context.Background())
	if err != nil || envelope.ID != 3 {
		t.Fatalf("recovery frame = %#v, err = %v", envelope, err)
	}
}

func TestReadFrameRejectsForeignProtocolVersion(t *testing.T) {
	body := `{"v":2,"id":1,"type":"HELLO","payload":{}}`
	header := make([]byte, frameHeaderBytes)
	binary.BigEndian.PutUint32(header, uint32(len(body)))
	reader := NewReader(bytes.NewReader(append(header, body...)))

	_, err := reader.ReadFrame(context.Background())
	if framingKind(t, err) != VersionMismatch {
		t.Fatalf("foreign version error = %v, want VERSION_MISMATCH", err)
	}
	var framing *FramingError
	errors.As(err, &framing)
	if !framing.Fatal() {
		t.Fatal("VERSION_MISMATCH must end the session")
	}
}

func TestReadFrameHonoursDeadlineAndCancellationOnAPipe(t *testing.T) {
	// The transport is the one the supervisor gives a session, not os.Pipe.
	// The two are the same call on Unix and are not on Windows, where an
	// anonymous pipe cannot be interrupted at all -- and this test asserting
	// os.Pipe was how that went unnoticed: it did not fail there, it hung,
	// and took the package's whole timeout with it.
	incoming, outgoing, err := interruptibleOutputPipe()
	if err != nil {
		t.Fatalf("interruptibleOutputPipe() error = %v", err)
	}
	defer incoming.Close()
	defer outgoing.Close()

	reader := NewReader(incoming)
	if !reader.SupportsInterruption() {
		t.Fatal("a pipe transport must support interrupting a blocked read")
	}

	timed, cancelTimed := context.WithTimeout(context.Background(), 40*time.Millisecond)
	defer cancelTimed()
	started := time.Now()
	if _, err := reader.ReadFrame(timed); framingKind(t, err) != Timeout {
		t.Fatalf("blocked read error = %v, want TIMEOUT", err)
	}
	if elapsed := time.Since(started); elapsed > 2*time.Second {
		t.Fatalf("blocked read took %v, deadline was not applied", elapsed)
	}

	canceled, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(20 * time.Millisecond)
		cancel()
	}()
	if _, err := reader.ReadFrame(canceled); framingKind(t, err) != Canceled {
		t.Fatalf("cancelled read error = %v, want CANCELED", err)
	}

	// The transport must remain usable once the deadline is cleared.
	go func() {
		_ = NewWriter(outgoing).WriteFrame(mustEnvelope(t, 9, "SHUTDOWN", map[string]any{}))
	}()
	envelope, err := reader.ReadFrame(context.Background())
	if err != nil || envelope.ID != 9 {
		t.Fatalf("post-cancellation frame = %#v, err = %v", envelope, err)
	}
}

func TestWriteFrameValidatesEnvelopesAndClosedSessions(t *testing.T) {
	writer := NewWriter(&bytes.Buffer{})
	invalid := []Envelope{
		{V: 2, ID: 1, Type: "HELLO", Payload: json.RawMessage(`{}`)},
		{V: ProtocolVersion, ID: 1, Type: "", Payload: json.RawMessage(`{}`)},
		{V: ProtocolVersion, ID: 1, Type: "HELLO"},
	}
	for index, envelope := range invalid {
		if err := writer.WriteFrame(envelope); framingKind(t, err) != InvalidPayload {
			t.Fatalf("invalid[%d] error = %v, want INVALID_PAYLOAD", index, err)
		}
	}

	writer.Close()
	if err := writer.WriteFrame(mustEnvelope(t, 1, "HELLO", map[string]any{})); !errors.Is(err, ErrSessionClosed) {
		t.Fatalf("closed writer error = %v, want ErrSessionClosed", err)
	}
	reader := NewReader(bytes.NewReader(nil))
	reader.Close()
	if _, err := reader.ReadFrame(context.Background()); !errors.Is(err, ErrSessionClosed) {
		t.Fatalf("closed reader error = %v, want ErrSessionClosed", err)
	}
}

func TestWriteFrameIsSafeForConcurrentEmitters(t *testing.T) {
	sink := &bytes.Buffer{}
	writer := NewWriter(sink)
	done := make(chan struct{})
	for worker := 0; worker < 4; worker++ {
		go func(worker int) {
			defer func() { done <- struct{}{} }()
			for index := 0; index < 25; index++ {
				if err := writer.WriteFrame(mustEnvelope(t, uint64(worker*100+index), "FACTS", map[string]any{"w": worker})); err != nil {
					t.Errorf("WriteFrame() error = %v", err)
					return
				}
			}
		}(worker)
	}
	for worker := 0; worker < 4; worker++ {
		<-done
	}

	reader := NewReader(bytes.NewReader(sink.Bytes()))
	seen := 0
	for {
		if _, err := reader.ReadFrame(context.Background()); errors.Is(err, io.EOF) {
			break
		} else if err != nil {
			t.Fatalf("ReadFrame() after concurrent writes error = %v", err)
		}
		seen++
	}
	if seen != 100 {
		t.Fatalf("decoded %d frames, want 100", seen)
	}
}
