package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"sync"

	"github.com/modelcontextprotocol/go-sdk/jsonrpc"
	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"
)

// ErrNoStream is returned by a StreamTransport with nothing to talk over.
var ErrNoStream = errors.New("mcp: transport has no stream")

// StreamTransport serves one MCP session over a byte stream.
//
// The SDK ships a transport for stdin/stdout and one for an in-memory pipe, and
// the stdio one hardcodes the process's own handles. A daemon accepting
// connections needs neither: it has a net.Conn per client and one server behind
// them, which is the whole point -- one mapped snapshot instead of one per
// client. The wire is the same newline-delimited JSON either way, so what is new
// here is only where the bytes come from.
//
// The stream must unblock a pending Read when it is closed, because that is how
// the SDK cancels a session. A net.Conn does; os.Stdin does not, which is why
// the SDK's own stdio connection carries a reader goroutine and a channel that
// this one does not need.
type StreamTransport struct {
	Stream io.ReadWriteCloser
}

// Connect implements the sdkmcp.Transport interface.
func (transport *StreamTransport) Connect(context.Context) (sdkmcp.Connection, error) {
	if transport == nil || transport.Stream == nil {
		return nil, ErrNoStream
	}
	return &streamConnection{stream: transport.Stream, decoder: json.NewDecoder(transport.Stream)}, nil
}

// streamConnection is the logical JSON-RPC connection over one stream.
//
// Reads are serialized by the session that owns them, so the decoder needs no
// lock; writes do, because a response and a notification can be produced
// concurrently by tool handlers.
type streamConnection struct {
	stream  io.ReadWriteCloser
	decoder *json.Decoder

	writeMu sync.Mutex

	closeOnce sync.Once
	closeErr  error
}

// Read returns the next message on the stream.
//
// Cancellation arrives by closing the stream, not through ctx: a decode already
// blocked on a read does not observe a context, and the Connection contract
// names Close as the way to unblock one. The context is still checked, so an
// already-cancelled session does not start a read it would have to abandon.
//
// A JSON-RPC batch -- an array where a message is expected -- comes back as an
// error from the decode below rather than being mishandled. Protocol revision
// 2025-06-18 removed batching and the clients this serves never produce one, so
// there is nothing here that unpacks an array: answering it as if it were a
// single message would correlate the wrong response to the wrong request.
func (connection *streamConnection) Read(ctx context.Context) (jsonrpc.Message, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	var raw json.RawMessage
	if err := connection.decoder.Decode(&raw); err != nil {
		return nil, err
	}
	message, err := jsonrpc.DecodeMessage(raw)
	if err != nil {
		return nil, fmt.Errorf("decode message: %w", err)
	}
	return message, nil
}

// Write encodes message and terminates it with a newline.
func (connection *streamConnection) Write(ctx context.Context, message jsonrpc.Message) error {
	data, err := jsonrpc.EncodeMessage(message)
	if err != nil {
		return fmt.Errorf("encode message: %w", err)
	}
	data = append(data, '\n')

	connection.writeMu.Lock()
	defer connection.writeMu.Unlock()
	if err := ctx.Err(); err != nil {
		return err
	}
	if _, err := connection.stream.Write(data); err != nil {
		return fmt.Errorf("write message: %w", err)
	}
	return nil
}

// Close closes the stream. It may be called more than once and concurrently
// with Read, which is what the Connection contract requires.
func (connection *streamConnection) Close() error {
	connection.closeOnce.Do(func() { connection.closeErr = connection.stream.Close() })
	return connection.closeErr
}

// SessionID is empty for a stream, the same answer the SDK's stdio connection
// gives: the id belongs to transports that multiplex sessions over one
// endpoint, and this one carries exactly one.
func (connection *streamConnection) SessionID() string { return "" }
