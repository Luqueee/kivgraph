package daemon

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	sdkjsonrpc "github.com/modelcontextprotocol/go-sdk/jsonrpc"
	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/Luqueee/kivgraph/internal/hotsnapshot"
	"github.com/Luqueee/kivgraph/internal/indexing"
	kivmcp "github.com/Luqueee/kivgraph/internal/mcp"
	"github.com/Luqueee/kivgraph/internal/metrics"
)

// dial connects a client to the daemon over the socket, which is the path a
// real client takes: no in-memory transport, no shared process memory.
func dial(t *testing.T, socket string) *sdkmcp.ClientSession {
	t.Helper()
	connection, err := net.Dial("unix", socket)
	if err != nil {
		t.Fatalf("dial %s: %v", socket, err)
	}
	client := sdkmcp.NewClient(&sdkmcp.Implementation{Name: "test", Version: "0.0.1"}, nil)
	session, err := client.Connect(context.Background(), &kivmcp.StreamTransport{Stream: connection}, nil)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	t.Cleanup(func() { _ = session.Close() })
	return session
}

// shortTempDir returns a directory whose socket path fits the kernel's address
// field. t.TempDir() does not: on darwin it hands out `/var/folders/...` names
// that are already 100 bytes before the socket is appended, which is a fact
// about the test runner and not about a real state directory -- the default one
// is `~/.local/state/kivgraph`, well inside the limit.
func shortTempDir(t *testing.T) string {
	t.Helper()
	directory, err := os.MkdirTemp("/tmp", "kivd")
	if err != nil {
		t.Fatalf("create a short temporary directory: %v", err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(directory) })
	return directory
}

// stubIndexer stands in for the real indexing service. The daemon tests are
// about sessions and sockets, so what matters here is only that an indexer
// exists: index_project is not registered without one.
type stubIndexer struct{}

func (stubIndexer) IndexProjects(context.Context, []indexing.Project, func(indexing.ProjectProgress)) (indexing.ProjectResult, error) {
	return indexing.ProjectResult{}, nil
}

// start runs a daemon over a temporary state directory and returns its socket.
func start(t *testing.T, store *hotsnapshot.SnapshotStore) (string, context.CancelFunc, *sync.WaitGroup) {
	t.Helper()
	options := Options{
		StateDirectory: shortTempDir(t),
		SnapshotStore:  store,
		Registry:       metrics.NewRegistry(),
		Indexer:        stubIndexer{},
	}
	listener, err := Listen(options)
	if err != nil {
		t.Fatalf("Listen() error = %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	var served sync.WaitGroup
	served.Add(1)
	go func() {
		defer served.Done()
		if err := Serve(ctx, listener, options); err != nil {
			t.Errorf("Serve() error = %v", err)
		}
	}()
	return listener.Addr().String(), cancel, &served
}

// TestAClientLeavingIsNotReportedAsAFailure is what isDisconnect buys. A closed
// connection is how every session ends, so reporting it as an error would put a
// line in the log for each departure -- which is exactly how a real failure stops
// being noticed.
func TestAClientLeavingIsNotReportedAsAFailure(t *testing.T) {
	ended := make(chan error, 4)
	options := Options{
		StateDirectory: shortTempDir(t),
		Registry:       metrics.NewRegistry(),
		Indexer:        stubIndexer{},
		OnSession: func(event string, err error) {
			if event == "ended" {
				ended <- err
			}
		},
	}
	listener, err := Listen(options)
	if err != nil {
		t.Fatalf("Listen() error = %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() { _ = Serve(ctx, listener, options) }()

	socket := listener.Addr().String()
	connection, err := net.Dial("unix", socket)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	client := sdkmcp.NewClient(&sdkmcp.Implementation{Name: "test", Version: "0.0.1"}, nil)
	session, err := client.Connect(context.Background(), &kivmcp.StreamTransport{Stream: connection}, nil)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	if _, err := session.ListTools(context.Background(), nil); err != nil {
		t.Fatalf("ListTools() error = %v", err)
	}

	// A client hanging up, which is the ordinary end of a session.
	if err := session.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}
	select {
	case err := <-ended:
		if err != nil {
			t.Fatalf("a client closing its session was reported as %v, want no error", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("the session never ended after the client left")
	}

	// And the other way a client leaves: the socket dies under it, with no
	// shutdown handshake. That is a killed editor or a dropped connection, and
	// it reaches the daemon as a read failure rather than as a clean end -- which
	// is the case isDisconnect exists for.
	abrupt, err := net.Dial("unix", socket)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	abruptSession, err := sdkmcp.NewClient(&sdkmcp.Implementation{Name: "test", Version: "0.0.1"}, nil).
		Connect(context.Background(), &kivmcp.StreamTransport{Stream: abrupt}, nil)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	if _, err := abruptSession.ListTools(context.Background(), nil); err != nil {
		t.Fatalf("ListTools() error = %v", err)
	}
	if err := abrupt.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}
	select {
	case err := <-ended:
		if err != nil {
			t.Fatalf("a dropped connection was reported as %v, want no error", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("the session never ended after the connection dropped")
	}
}

// TestADepartureIsNotAFailureInAnyOfItsShapes fixes the shapes the error
// arrives in, which is the whole difficulty: the SDK's v1 transport surfaces
// whichever end of the race finished first, so one ordinary Close() by a
// client can produce a socket error, a connection error or a JSON-RPC one.
// Each is wrapped the way the SDK wraps it -- the syscall three layers deep,
// the wire error under a %w with its cause formatted by %v -- so a classifier
// that forgot to unwrap, or one that matched on the message, fails here.
func TestADepartureIsNotAFailureInAnyOfItsShapes(t *testing.T) {
	socket := func(errno syscall.Errno) error {
		return fmt.Errorf("write message: %w", &net.OpError{
			Op:  "write",
			Net: "unix",
			Err: &os.SyscallError{Syscall: "write", Err: errno},
		})
	}
	// conn.go wraps the cause with %v, so io.EOF does not travel: the code is
	// the only thing left to classify by.
	wire := func(code int64, message string) error {
		return fmt.Errorf("%w: %v", &sdkjsonrpc.Error{Code: code, Message: message}, io.EOF)
	}

	for _, departure := range []error{
		socket(syscall.EPIPE),
		socket(syscall.ECONNRESET),
		wire(wireServerClosing, "server is closing"),
		wire(wireClientClosing, "client is closing"),
	} {
		if isDisconnect(departure) {
			continue
		}
		t.Fatalf("isDisconnect(%v) = false, want a departing client treated as one", departure)
	}

	// And the classifier still refuses what is not a departure: a coded error
	// that means the peer sent something invalid is the daemon's business.
	invalid := fmt.Errorf("handling request: %w", &sdkjsonrpc.Error{Code: -32602, Message: "invalid params"})
	if isDisconnect(invalid) {
		t.Fatalf("isDisconnect(%v) = true, want a protocol failure reported", invalid)
	}
}

// TestShuttingDownIsNotReportedAsAFailure is the case isDisconnect exists for.
// A departing client reaches the daemon as io.EOF, which the SDK already
// swallows -- but a shutdown does not: cancelling the context closes the
// connection under an open session, and the read fails with net.ErrClosed or a
// cancelled context. Both are the daemon stopping, not a session that broke.
func TestShuttingDownIsNotReportedAsAFailure(t *testing.T) {
	ended := make(chan error, 4)
	options := Options{
		StateDirectory: shortTempDir(t),
		Registry:       metrics.NewRegistry(),
		Indexer:        stubIndexer{},
		OnSession: func(event string, err error) {
			if event == "ended" {
				ended <- err
			}
		},
	}
	listener, err := Listen(options)
	if err != nil {
		t.Fatalf("Listen() error = %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	stopped := make(chan error, 1)
	go func() { stopped <- Serve(ctx, listener, options) }()

	session := dial(t, listener.Addr().String())
	if _, err := session.ListTools(context.Background(), nil); err != nil {
		t.Fatalf("ListTools() error = %v", err)
	}

	// The session is open and idle, which is how a real client waits between
	// calls: this is the state a shutdown finds it in.
	cancel()
	select {
	case err := <-ended:
		if err != nil {
			t.Fatalf("shutting down reported the session as %v, want no error", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("the session never ended after the daemon was cancelled")
	}
	select {
	case err := <-stopped:
		if err != nil {
			t.Fatalf("Serve() error = %v, want no error on shutdown", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Serve() did not return after the context was cancelled")
	}
}

// TestListenCreatesThePrivateSocketWithoutAChmod pins both halves of how the
// mode is set. The mode itself matters on Linux, where connect checks write
// permission on the socket; and it has to be there without a chmod, because
// chmod on a socket returns EINVAL on some filesystems and a daemon that needed
// it could not start there at all.
func TestListenCreatesThePrivateSocketWithoutAChmod(t *testing.T) {
	// A permissive umask, which is what makes this a test of the code and not
	// of the environment: without narrowing, the socket would be 0777 here.
	previous := syscall.Umask(0)
	t.Cleanup(func() { syscall.Umask(previous) })

	directory := shortTempDir(t)
	listener, err := Listen(Options{StateDirectory: directory})
	if err != nil {
		t.Fatalf("Listen() error = %v", err)
	}
	defer func() { _ = listener.Close() }()

	info, err := os.Lstat(filepath.Join(directory, SocketName))
	if err != nil {
		t.Fatalf("stat the socket: %v", err)
	}
	if mode := info.Mode().Perm(); mode != 0o600 {
		t.Fatalf("socket mode = %#o, want 0600 under a permissive umask", mode)
	}
	// And the umask is given back: a library that kept it would silently
	// narrow every file the rest of the process creates.
	if now := syscall.Umask(0); now != 0 {
		t.Fatalf("umask after Listen = %#o, want the 0 it was called with", now)
	}
}

// TestDaemonAnswersManySessionsFromOneProcess is the claim the daemon exists
// for. Two clients, one process, one snapshot: if sessions had to share a
// server built once, or if the transport mixed their streams, one of these two
// would get the other's answer or none.
func TestDaemonAnswersManySessionsFromOneProcess(t *testing.T) {
	socket, cancel, served := start(t, nil)
	defer func() {
		cancel()
		served.Wait()
	}()

	first, second := dial(t, socket), dial(t, socket)
	for label, session := range map[string]*sdkmcp.ClientSession{"first": first, "second": second} {
		result, err := session.ListTools(context.Background(), nil)
		if err != nil {
			t.Fatalf("%s: ListTools() error = %v", label, err)
		}
		if len(result.Tools) == 0 {
			t.Fatalf("%s: the daemon published no tools", label)
		}
	}

	// Interleaved, because the point is that they are concurrent sessions and
	// not a queue: the second client's request is written while the first
	// session is still open.
	if _, err := first.ListTools(context.Background(), nil); err != nil {
		t.Fatalf("first session stopped answering after the second connected: %v", err)
	}
}

// TestASessionSeesAGenerationPublishedAfterStartup is the reason a server is
// built per session instead of once at startup. The tool surface is decided when
// a server is built -- a process with no published generation publishes only
// index_project -- and a daemon outlives generations. A server built once would
// keep telling every future client that there is no graph.
func TestASessionSeesAGenerationPublishedAfterStartup(t *testing.T) {
	store := hotsnapshot.NewSnapshotStore(nil)
	socket, cancel, served := start(t, store)
	defer func() {
		cancel()
		served.Wait()
	}()

	if names := toolNames(t, socket); len(names) != 1 || names[0] != "index_project" {
		t.Fatalf("tools before publishing = %v, want only index_project", names)
	}

	if err := store.Publish(emptySnapshot(t)); err != nil {
		t.Fatalf("Publish() error = %v", err)
	}

	after := toolNames(t, socket)
	if len(after) <= 1 {
		t.Fatalf("tools after publishing = %v, want the query surface", after)
	}
	for _, name := range after {
		if name == "find_references" {
			return
		}
	}
	t.Fatalf("tools after publishing = %v, want find_references among them", after)
}

// emptySnapshot is a published generation with no rows. What the daemon tests
// need from it is only that it exists: the tool surface turns on whether a
// generation is published, not on what it holds.
func emptySnapshot(t *testing.T) *hotsnapshot.GraphSnapshot {
	t.Helper()
	interner := hotsnapshot.NewStringInterner()
	snapshot, err := hotsnapshot.NewGraphSnapshot(hotsnapshot.GraphSnapshotInput{
		ID:             1,
		CreatedAt:      time.Unix(1, 0).UTC(),
		Version:        1,
		Strings:        interner.Freeze(),
		ForwardOffsets: []uint32{0},
		ReverseOffsets: []uint32{0},
	})
	if err != nil {
		t.Fatalf("NewGraphSnapshot() error = %v", err)
	}
	return snapshot
}

// toolNames opens a session and returns what it was told it can call.
func toolNames(t *testing.T, socket string) []string {
	t.Helper()
	result, err := dial(t, socket).ListTools(context.Background(), nil)
	if err != nil {
		t.Fatalf("ListTools() error = %v", err)
	}
	names := make([]string, 0, len(result.Tools))
	for _, tool := range result.Tools {
		names = append(names, tool.Name)
	}
	return names
}

// TestListenRefusesASecondDaemonAndClearsAStaleSocket separates the two states
// a socket file can be in. A live daemon holding it must not be replaced, and a
// file left by a killed one must not block the next start -- and the only way to
// tell them apart is to try talking to it.
func TestListenRefusesASecondDaemonAndClearsAStaleSocket(t *testing.T) {
	directory := shortTempDir(t)
	options := Options{StateDirectory: directory}
	listener, err := Listen(options)
	if err != nil {
		t.Fatalf("Listen() error = %v", err)
	}
	if _, err := Listen(options); err == nil {
		t.Fatal("Listen() bound a socket a live daemon already holds")
	}

	// A killed daemon leaves the file behind: closing without unlinking is what
	// that looks like from the next process.
	unixListener, ok := listener.(*net.UnixListener)
	if !ok {
		t.Fatalf("listener is %T, want *net.UnixListener", listener)
	}
	unixListener.SetUnlinkOnClose(false)
	if err := listener.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}
	revived, err := Listen(options)
	if err != nil {
		t.Fatalf("Listen() refused a stale socket: %v", err)
	}
	if err := revived.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}
}

// TestSocketPathIsPerStateDirectory is the isolation the daemon depends on: two
// configurations must never answer each other's clients, and the state
// directory is the only thing that decides which graph a socket serves.
func TestSocketPathIsPerStateDirectory(t *testing.T) {
	first, err := SocketPath("/one/state")
	if err != nil {
		t.Fatalf("SocketPath() error = %v", err)
	}
	second, err := SocketPath("/another/state")
	if err != nil {
		t.Fatalf("SocketPath() error = %v", err)
	}
	if first == second {
		t.Fatalf("two state directories share a socket: %s", first)
	}
	if want := filepath.Join("/one/state", SocketName); first != want {
		t.Fatalf("SocketPath() = %s, want %s", first, want)
	}
}

// TestSocketPathRefusesAPathTheKernelWouldTruncate covers the failure that would
// otherwise be silent. A unix address is a fixed-size field, and bind truncates
// rather than refusing -- which would let two state directories with a long
// common prefix land on one socket.
func TestSocketPathRefusesAPathTheKernelWouldTruncate(t *testing.T) {
	long := "/" + strings.Repeat("d", maximumSocketPath)
	if _, err := SocketPath(long); !errors.Is(err, ErrSocketPathTooLong) {
		t.Fatalf("SocketPath(long) error = %v, want ErrSocketPathTooLong", err)
	}
	// And the message has to say what the limit is: an operator reads this
	// while choosing where to put a state directory.
	_, err := SocketPath(long)
	if !strings.Contains(err.Error(), "limit 104") {
		t.Fatalf("error = %q, want it to name the limit", err)
	}
}

// TestServeStopsWithTheContextAndUnlinksTheSocket pins the lifecycle a
// supervisor depends on: a cancelled context ends the accept loop, and the
// socket file goes with it so the next daemon can bind.
func TestServeStopsWithTheContextAndUnlinksTheSocket(t *testing.T) {
	socket, cancel, served := start(t, nil)
	session := dial(t, socket)
	if _, err := session.ListTools(context.Background(), nil); err != nil {
		t.Fatalf("ListTools() error = %v", err)
	}
	cancel()
	served.Wait()

	deadline := time.Now().Add(2 * time.Second)
	for {
		if _, err := net.Dial("unix", socket); err != nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("the socket still accepts connections after the context was cancelled")
		}
		time.Sleep(10 * time.Millisecond)
	}
}
