package mcp

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/jsonrpc"
	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"
)

// pipe returns the two ends of a connection, which is the shape the daemon
// hands this transport: a net.Conn, not the process's own handles.
func pipe(t *testing.T) (sdkmcp.Connection, net.Conn) {
	t.Helper()
	server, client := net.Pipe()
	connection, err := (&StreamTransport{Stream: server}).Connect(context.Background())
	if err != nil {
		t.Fatalf("Connect() error = %v", err)
	}
	t.Cleanup(func() {
		_ = connection.Close()
		_ = client.Close()
	})
	return connection, client
}

// TestConnectRefusesATransportWithNoStream keeps the zero value from looking
// like a working transport. A nil stream would otherwise fail later, inside a
// read, where the cause is no longer visible.
func TestConnectRefusesATransportWithNoStream(t *testing.T) {
	if _, err := (&StreamTransport{}).Connect(context.Background()); !errors.Is(err, ErrNoStream) {
		t.Fatalf("Connect() error = %v, want ErrNoStream", err)
	}
}

// TestReadDecodesOneMessagePerLine pins the wire format. It is the same
// newline-delimited JSON the SDK's stdio transport speaks, which is what lets a
// client talk to a daemon without knowing it is not talking to a subprocess.
func TestReadDecodesOneMessagePerLine(t *testing.T) {
	connection, client := pipe(t)
	go func() {
		_, _ = client.Write([]byte(`{"jsonrpc":"2.0","id":1,"method":"first"}` + "\n" +
			`{"jsonrpc":"2.0","id":2,"method":"second"}` + "\n"))
	}()

	for _, want := range []string{"first", "second"} {
		message, err := connection.Read(context.Background())
		if err != nil {
			t.Fatalf("Read() error = %v", err)
		}
		request, ok := message.(*jsonrpc.Request)
		if !ok {
			t.Fatalf("Read() returned %T, want *jsonrpc.Request", message)
		}
		if request.Method != want {
			t.Fatalf("Read() method = %q, want %q", request.Method, want)
		}
	}
}

// TestReadRefusesABatchInsteadOfMishandlingIt is the case that would be silent.
// A batch is an array where a message belongs, and unpacking one would answer a
// request with another request's response. Protocol revision 2025-06-18 removed
// batching, so refusing it is the correct answer -- but it has to be an error and
// not a guess.
func TestReadRefusesABatchInsteadOfMishandlingIt(t *testing.T) {
	connection, client := pipe(t)
	go func() {
		_, _ = client.Write([]byte(`[{"jsonrpc":"2.0","id":1,"method":"first"}]` + "\n"))
	}()

	message, err := connection.Read(context.Background())
	if err == nil {
		t.Fatalf("Read() accepted a batch and returned %#v", message)
	}
}

// TestCloseUnblocksAPendingRead is the property the daemon's cancellation rests
// on. A decode already blocked on a read does not observe a context, so closing
// the stream is the only thing that ends a session -- and if it did not, a
// cancelled daemon would wait forever for clients that had not hung up.
func TestCloseUnblocksAPendingRead(t *testing.T) {
	connection, _ := pipe(t)
	failed := make(chan error, 1)
	go func() {
		_, err := connection.Read(context.Background())
		failed <- err
	}()

	// Give the read time to block; without this the test could pass by racing
	// ahead of it, which would prove nothing.
	time.Sleep(20 * time.Millisecond)
	if err := connection.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}
	select {
	case err := <-failed:
		if err == nil {
			t.Fatal("Read() returned no error after the stream was closed")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Close() did not unblock a pending Read")
	}
}

// TestReadOnACancelledContextConsumesNothing is why the context is checked
// before the decode. A session that has already ended must not take the next
// queued message off the stream: it would not answer it, and the bytes are gone.
func TestReadOnACancelledContextConsumesNothing(t *testing.T) {
	connection, client := pipe(t)
	go func() {
		_, _ = client.Write([]byte(`{"jsonrpc":"2.0","id":1,"method":"queued"}` + "\n"))
	}()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := connection.Read(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("Read() on a cancelled context error = %v, want context.Canceled", err)
	}

	// The message is still there, which is the whole point: a later read gets
	// it instead of finding the stream drained.
	message, err := connection.Read(context.Background())
	if err != nil {
		t.Fatalf("Read() error = %v", err)
	}
	request, ok := message.(*jsonrpc.Request)
	if !ok {
		t.Fatalf("Read() returned %T, want *jsonrpc.Request", message)
	}
	if request.Method != "queued" {
		t.Fatalf("Read() method = %q, want %q", request.Method, "queued")
	}
}

// TestCloseIsIdempotent covers what the Connection contract asks for: Close is
// called whenever a Read or Write fails, so a session that ends badly closes
// more than once and must not report the second one as a failure.
func TestCloseIsIdempotent(t *testing.T) {
	connection, _ := pipe(t)
	if err := connection.Close(); err != nil {
		t.Fatalf("first Close() error = %v", err)
	}
	if err := connection.Close(); err != nil {
		t.Fatalf("second Close() error = %v", err)
	}
}

// splittingWriter delivers every Write in two pieces, with a gap in between.
//
// This is what makes the lock observable. A net.Conn hands one Write to the peer
// whole, so two unsynchronised writers over one would still produce clean
// messages and the test would pass with the lock removed. An io.Writer promises
// no such thing -- a short write is legal, and the stream this transport is given
// is an io.ReadWriteCloser -- so the gap here is a permitted behaviour, not a
// contrived one.
type splittingWriter struct {
	mu      sync.Mutex
	written []byte
}

func (writer *splittingWriter) Write(data []byte) (int, error) {
	half := len(data) / 2
	writer.append(data[:half])
	// Yield between the halves, so another writer takes the stream if nothing
	// stops it.
	runtime.Gosched()
	writer.append(data[half:])
	return len(data), nil
}

func (writer *splittingWriter) append(data []byte) {
	writer.mu.Lock()
	defer writer.mu.Unlock()
	writer.written = append(writer.written, data...)
}

func (writer *splittingWriter) Read([]byte) (int, error) { return 0, io.EOF }
func (writer *splittingWriter) Close() error             { return nil }

func (writer *splittingWriter) bytes() []byte {
	writer.mu.Lock()
	defer writer.mu.Unlock()
	return append([]byte(nil), writer.written...)
}

// TestConcurrentWritesDoNotInterleave is why Write holds a lock. Tool handlers
// produce responses and notifications concurrently, and two encodings sharing
// the stream would produce a line that is neither message.
func TestConcurrentWritesDoNotInterleave(t *testing.T) {
	stream := &splittingWriter{}
	connection, err := (&StreamTransport{Stream: stream}).Connect(context.Background())
	if err != nil {
		t.Fatalf("Connect() error = %v", err)
	}

	const writers = 8
	var writing sync.WaitGroup
	writing.Add(writers)
	for index := range writers {
		go func() {
			defer writing.Done()
			id, err := jsonrpc.MakeID(fmt.Sprintf("write-%d", index))
			if err != nil {
				t.Errorf("MakeID() error = %v", err)
				return
			}
			request := &jsonrpc.Request{ID: id, Method: strings.Repeat("m", 512)}
			if err := connection.Write(context.Background(), request); err != nil {
				t.Errorf("Write() error = %v", err)
			}
		}()
	}
	writing.Wait()

	// A decoder is the only judge of a torn write, because torn bytes are still
	// bytes. Every message has to come back out whole, and there have to be as
	// many as went in.
	decoder := json.NewDecoder(bytes.NewReader(stream.bytes()))
	seen := 0
	for {
		var raw json.RawMessage
		if err := decoder.Decode(&raw); err != nil {
			if errors.Is(err, io.EOF) {
				break
			}
			t.Fatalf("a concurrent write tore a message: %v", err)
		}
		if _, err := jsonrpc.DecodeMessage(raw); err != nil {
			t.Fatalf("a concurrent write tore a message: %v", err)
		}
		seen++
	}
	if seen != writers {
		t.Fatalf("read %d messages, want %d", seen, writers)
	}
}

// TestWriteTerminatesEveryMessageWithANewline pins the wire format from the
// writing side. The peer is a newline-delimited reader -- the SDK's own stdio
// connection refuses anything else after a message -- so a message written
// without its delimiter is silently unreadable by every real client.
func TestWriteTerminatesEveryMessageWithANewline(t *testing.T) {
	stream := &splittingWriter{}
	connection, err := (&StreamTransport{Stream: stream}).Connect(context.Background())
	if err != nil {
		t.Fatalf("Connect() error = %v", err)
	}
	id, err := jsonrpc.MakeID("one")
	if err != nil {
		t.Fatalf("MakeID() error = %v", err)
	}
	if err := connection.Write(context.Background(), &jsonrpc.Request{ID: id, Method: "one"}); err != nil {
		t.Fatalf("Write() error = %v", err)
	}
	written := stream.bytes()
	if len(written) == 0 || written[len(written)-1] != '\n' {
		t.Fatalf("Write() produced %q, want it to end with a newline", written)
	}
	if bytes.Count(written, []byte("\n")) != 1 {
		t.Fatalf("Write() produced %d newlines for one message, want 1",
			bytes.Count(written, []byte("\n")))
	}
}

// TestWriteReportsAClosedStream keeps a failed write from looking like a
// delivered one. The session decides to end on this error; swallowing it would
// leave a client waiting for a response that was never sent.
func TestWriteReportsAClosedStream(t *testing.T) {
	connection, _ := pipe(t)
	if err := connection.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}
	id, err := jsonrpc.MakeID("after-close")
	if err != nil {
		t.Fatalf("MakeID() error = %v", err)
	}
	err = connection.Write(context.Background(), &jsonrpc.Request{ID: id, Method: "after-close"})
	if err == nil {
		t.Fatal("Write() to a closed stream reported success")
	}
	if !errors.Is(err, io.ErrClosedPipe) && !strings.Contains(err.Error(), "closed") {
		t.Fatalf("Write() error = %v, want it to name the closed stream", err)
	}
}

// TestSessionIDIsEmptyForOneSessionPerStream pins the same answer the SDK's
// stdio connection gives. An id belongs to transports that multiplex sessions
// over one endpoint; inventing one here would make a client think it can address
// a session that does not exist.
func TestSessionIDIsEmptyForOneSessionPerStream(t *testing.T) {
	connection, _ := pipe(t)
	if id := connection.SessionID(); id != "" {
		t.Fatalf("SessionID() = %q, want empty", id)
	}
}
