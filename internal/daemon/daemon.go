// Package daemon serves MCP to many clients from one process, over a unix socket
// and over Streamable HTTP at once.
//
// The saving is measured, and it is the slope rather than any one total. The load
// was counted, not chosen: over two days of real use, 48 of 51 servers were asked
// nothing at all, so the median session makes no call whatsoever.
//
//	load      daemon slope       N processes    8 clients            1 client
//	none      indistinguishable  10 MB/client   10-13 against 77-81  daemon +2-3 MB
//	8 calls   0,6-0,9 MB/client  39 MB/client   60-62 against 323-330  ties
//
// Starting up is no longer the cost: since ADR 0067 the graph is read by the
// first query that needs it, so an idle server holds 10 MB where it used to hold
// 33. What a daemon still saves is the rest -- and at one client it now loses by a
// couple of megabytes, which is the extra process.
//
// The two doors are indistinguishable at both loads -- 9,8-10,6 MB per client over
// HTTP against 10,0-10,7 over the socket -- and the door that matters is HTTP
// because no MCP client configuration dials a unix socket: it takes an executable
// or a url. Over 108.737 symbols of kena, on Linux, in benchmarks/daemon-cost.
//
// The widest gap is the peak, and it does not depend on anyone asking anything:
// 179-186 MB for eight processes against 26-29 for one daemon, because eight
// editors starting at once pay eight processes at once.
//
// Under sustained traffic HTTP costs more -- 11-13 MB per client at 2000 calls a
// session -- because the SDK gives every session a 10 MiB resumption buffer that
// response bytes fill. That is a ceiling no real session reaches, it depends on
// the corpus, and it cannot be capped from here: the handler builds its own
// transport and takes no event store.
//
// What is *not* the saving is the snapshot. It is the same mapped file in every
// server and those pages are clean, so a reader expecting its `87 MB` to
// disappear is looking at the wrong half: the bytes at stake are the private
// ones. Nor did allocating less buy them -- `LUQUE-2216` to `LUQUE-2220` took
// `60,5 MB` off what a load allocates and the resident figure moved `0,75 %`,
// because the allocator recycles those pages rather than keeping them.
//
// At one client a daemon loses by a couple of megabytes on both doors, because a
// server that answers nothing no longer reads the graph. It wins from the second.
package daemon

import (
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"sync"
	"syscall"

	sdkjsonrpc "github.com/modelcontextprotocol/go-sdk/jsonrpc"
	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/Luqueee/kivgraph/internal/hotsnapshot"
	"github.com/Luqueee/kivgraph/internal/indexing"
	kivmcp "github.com/Luqueee/kivgraph/internal/mcp"
	"github.com/Luqueee/kivgraph/internal/metrics"
)

// SocketName is the socket a daemon listens on inside the state directory.
//
// The state directory is the key: two configurations pointing at different
// directories get different daemons, so a client never reaches a graph built
// from someone else's repositories. Nothing here shares by machine or by user.
const SocketName = "daemon.sock"

// maximumSocketPath is the shortest limit among the platforms this project
// targets. A unix socket address is a fixed-size field in the kernel -- 104
// bytes on darwin, 108 on linux -- and a path over it is truncated by bind
// rather than refused, which would leave two state directories sharing one
// socket. So the length is checked here and named.
const maximumSocketPath = 104

// ErrSocketPathTooLong reports a state directory whose socket path does not fit
// the kernel's address field.
var ErrSocketPathTooLong = errors.New("daemon: socket path is too long")

// SocketPath returns the socket a daemon for stateDirectory listens on.
func SocketPath(stateDirectory string) (string, error) {
	path := filepath.Join(stateDirectory, SocketName)
	if len(path) >= maximumSocketPath {
		return "", fmt.Errorf("%w: %d bytes, limit %d: %s",
			ErrSocketPathTooLong, len(path), maximumSocketPath, path)
	}
	return path, nil
}

// Options are what a daemon serves.
//
// The store, the registry and the indexer are shared by every session, and that
// sharing is the point: the snapshot is mapped once and the metrics graph_status
// reports are the process's, not one client's.
type Options struct {
	StateDirectory string
	SnapshotStore  *hotsnapshot.SnapshotStore
	Registry       *metrics.Registry
	Indexer        indexing.ProjectIndexer

	// OnSession, when set, is called when a session starts and when it ends.
	// The error is nil on a clean end. It is a typed hook rather than a
	// formatter because the caller logs structurally, and a format string would
	// arrive there as one opaque message.
	OnSession func(event string, err error)
}

// Listen binds the daemon's socket.
//
// A stale socket file left by a killed process is removed first: a unix socket
// is a file, and bind refuses an existing path. Removing it is safe because the
// path is inside the state directory and because a live daemon holding it would
// answer -- which is what Dial checks before this is called.
//
// The socket is created with its mode already restricted rather than chmod'd
// afterwards, and that is not a style choice: chmod on a socket returns EINVAL
// on some filesystems -- a virtiofs bind mount is one -- so a daemon that set the
// mode after binding could not start there at all. Measured while running
// `benchmarks/daemon-cost` under Docker.
//
// What the mode buys is platform-dependent, so neither half is left implicit.
// Linux checks write permission on the socket at connect, so 0600 is a real
// gate. Darwin ignores socket permissions for connect entirely; there the gate
// is the state directory, which has to be traversed to reach the path at all.
func Listen(options Options) (net.Listener, error) {
	path, err := SocketPath(options.StateDirectory)
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(options.StateDirectory, 0o700); err != nil {
		return nil, fmt.Errorf("create the state directory: %w", err)
	}
	if _, err := os.Stat(path); err == nil {
		if connection, dialErr := net.Dial("unix", path); dialErr == nil {
			_ = connection.Close()
			return nil, fmt.Errorf("a daemon is already listening on %s", path)
		}
		if err := os.Remove(path); err != nil {
			return nil, fmt.Errorf("remove the stale socket %s: %w", path, err)
		}
	}
	// The socket carries the whole graph, so it is the owner's alone whatever
	// umask the caller happened to have.
	var listener net.Listener
	if err := withPrivateUmask(func() error {
		bound, err := net.Listen("unix", path)
		if err != nil {
			return fmt.Errorf("listen on %s: %w", path, err)
		}
		listener = bound
		return nil
	}); err != nil {
		return nil, err
	}
	return listener, nil
}

// Serve accepts connections until ctx is cancelled or the listener fails.
//
// Every accepted connection gets its own MCP server rather than sharing one.
// That is not about isolation: the tool surface is decided when a server is
// built, because a process with no published generation publishes only
// index_project and says so in its instructions. A daemon outlives generations,
// so a server built once at startup would keep telling every future client that
// there is no graph. Building one per session costs eleven tool registrations
// and answers with the generation that exists now.
func Serve(ctx context.Context, listener net.Listener, options Options) error {
	if listener == nil {
		return errors.New("daemon: no listener")
	}
	notify := options.OnSession
	if notify == nil {
		notify = func(string, error) {}
	}

	var sessions sync.WaitGroup
	// Closing the listener is what unblocks Accept; there is no other portable
	// way to interrupt it. Go unlinks the socket file as part of that close,
	// because this listener is the one that created it.
	go func() {
		<-ctx.Done()
		_ = listener.Close()
	}()

	var acceptErr error
	for {
		connection, err := listener.Accept()
		if err != nil {
			if ctx.Err() == nil {
				acceptErr = fmt.Errorf("accept: %w", err)
			}
			break
		}
		sessions.Add(1)
		go func() {
			defer sessions.Done()
			defer func() { _ = connection.Close() }()
			// Cancellation reaches a session by closing its connection, which
			// is what the Connection contract names as the way to unblock a
			// read waiting for input. Without this the accept loop would stop
			// and then wait forever for sessions whose clients had not hung up:
			// a blocked decode does not observe a context.
			stopped := make(chan struct{})
			defer close(stopped)
			go func() {
				select {
				case <-ctx.Done():
					_ = connection.Close()
				case <-stopped:
				}
			}()
			notify("started", nil)
			notify("ended", serveSession(ctx, connection, options))
		}()
	}
	sessions.Wait()
	return acceptErr
}

// serveSession runs one MCP session to completion over connection.
func serveSession(ctx context.Context, connection net.Conn, options Options) error {
	server := kivmcp.NewServerWithMetricsAndSnapshotStoreAndIndexer(
		options.Registry, options.SnapshotStore, options.Indexer)
	session, err := server.Connect(ctx, &kivmcp.StreamTransport{Stream: connection}, nil)
	if err != nil {
		return fmt.Errorf("connect: %w", err)
	}
	if err := session.Wait(); err != nil && !isDisconnect(err) {
		return err
	}
	return nil
}

// isDisconnect reports the errors a client leaving produces. A closed
// connection is how a session ends, not a failure the daemon should report as
// one, and the alternative -- logging every departure as an error -- is how a
// real failure stops being noticed.
//
// A departure reaches this function in three shapes, and the SDK's v1
// transport is why there is more than one. Under v0.8.0 a client leaving
// arrived as io.EOF and was swallowed; v1 surfaces whichever end of the race
// finished first, so an ordinary Close() by a client can produce any of them:
//
//   - the socket layer: EPIPE or ECONNRESET, the same event seen from the
//     writing side -- the peer is gone before this end finished answering it
//   - the connection layer: net.ErrClosed, os.ErrClosed, context.Canceled or
//     the SDK's own ErrConnectionClosed
//   - the protocol layer: a JSON-RPC error carrying code -32003 or -32004
//
// Those two codes are matched rather than their sentinels because the
// sentinels live in the SDK's internal jsonrpc2 package. The public jsonrpc
// alias exposes the error's Code, and the code is the part of the contract
// that travels on the wire -- unlike the message, which is prose. The
// alternative here is not a stricter check, it is matching "server is
// closing: EOF" as text.
//
// Getting this wrong logs an error for every ordinary departure, which ADR
// 0065 names as the outcome this function exists to prevent: a line per
// client is how a real failure stops being noticed.
func isDisconnect(err error) bool {
	if err == nil {
		return true
	}
	if errors.Is(err, net.ErrClosed) || errors.Is(err, os.ErrClosed) ||
		errors.Is(err, context.Canceled) || errors.Is(err, sdkmcp.ErrConnectionClosed) ||
		errors.Is(err, syscall.EPIPE) || errors.Is(err, syscall.ECONNRESET) {
		return true
	}
	var wire *sdkjsonrpc.Error
	if errors.As(err, &wire) {
		return wire.Code == wireClientClosing || wire.Code == wireServerClosing
	}
	return false
}

// The JSON-RPC codes the SDK answers with while either end is closing.
const (
	wireClientClosing = -32003
	wireServerClosing = -32004
)
