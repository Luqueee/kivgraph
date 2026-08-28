package relay

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/Luqueee/kivgraph/internal/daemon"
)

const testToken = "a-token-the-daemon-published"

// fakeDaemon is a Streamable HTTP server that answers one tool and records
// which HTTP methods it was asked, which is how the standalone-SSE invariant
// is checked: that stream is a GET and nothing else here is.
type fakeDaemon struct {
	server   *httptest.Server
	mu       sync.Mutex
	requests []string
}

func startFakeDaemon(t *testing.T, serverVersion string) *fakeDaemon {
	t.Helper()
	server := sdkmcp.NewServer(&sdkmcp.Implementation{Name: "kivgraph", Version: serverVersion}, nil)
	sdkmcp.AddTool(server,
		&sdkmcp.Tool{Name: "graph_status", Description: "the graph"},
		func(context.Context, *sdkmcp.CallToolRequest, map[string]any) (*sdkmcp.CallToolResult, any, error) {
			return &sdkmcp.CallToolResult{
				Content: []sdkmcp.Content{&sdkmcp.TextContent{Text: "answered by the daemon"}},
			}, nil, nil
		})
	fake := &fakeDaemon{}
	handler := sdkmcp.NewStreamableHTTPHandler(
		func(*http.Request) *sdkmcp.Server { return server }, nil)
	// Mounted at the path the real daemon uses, and only there, because the
	// difference between the two GETs this file cares about is the path: the
	// reachability probe asks for one the daemon does not serve, and the
	// standalone SSE stream would ask for this one.
	mux := http.NewServeMux()
	mux.Handle(daemon.MCPPath, http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		// The token is the only thing that separates this daemon from whatever
		// else can bind a loopback port, so it is checked here too.
		if request.Header.Get("Authorization") != daemon.BearerHeader(testToken) {
			writer.WriteHeader(http.StatusUnauthorized)
			return
		}
		handler.ServeHTTP(writer, request)
	}))
	fake.server = httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		fake.mu.Lock()
		fake.requests = append(fake.requests, request.Method+" "+request.URL.Path)
		fake.mu.Unlock()
		mux.ServeHTTP(writer, request)
	}))
	t.Cleanup(fake.server.Close)
	return fake
}

func (fake *fakeDaemon) endpoint() daemon.Endpoint {
	return daemon.Endpoint{URL: fake.server.URL + daemon.MCPPath, Token: testToken, PID: 4242}
}

func (fake *fakeDaemon) saw(method, path string) bool {
	fake.mu.Lock()
	defer fake.mu.Unlock()
	for _, seen := range fake.requests {
		if seen == method+" "+path {
			return true
		}
	}
	return false
}

// connectThroughRelay runs a relay over an in-memory pair and returns a client
// session speaking to it, plus the channel the relay's own outcome arrives on.
func connectThroughRelay(
	t *testing.T,
	fake *fakeDaemon,
	ourVersion string,
) (*sdkmcp.ClientSession, chan error) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	t.Cleanup(cancel)

	clientSide, relaySide := sdkmcp.NewInMemoryTransports()
	outcome := make(chan error, 1)
	go func() { outcome <- run(ctx, fake.endpoint(), relaySide, ourVersion) }()

	client := sdkmcp.NewClient(&sdkmcp.Implementation{Name: "agent", Version: "1.0.0"}, nil)
	session, err := client.Connect(ctx, clientSide, nil)
	if err != nil {
		select {
		case relayErr := <-outcome:
			t.Fatalf("connect through the relay: %v (the relay said: %v)", err, relayErr)
		default:
			t.Fatalf("connect through the relay: %v", err)
		}
	}
	t.Cleanup(func() { _ = session.Close() })
	return session, outcome
}

// The whole claim of the relay: a tool the relay knows nothing about answers
// through it. Messages cross opaque, so a tool added to the daemon reaches the
// agent without the relay changing.
func TestARelayForwardsAToolItKnowsNothingAbout(t *testing.T) {
	fake := startFakeDaemon(t, "9.9.9")
	session, _ := connectThroughRelay(t, fake, "9.9.9")

	response, err := session.CallTool(context.Background(),
		&sdkmcp.CallToolParams{Name: "graph_status", Arguments: map[string]any{}})
	if err != nil {
		t.Fatalf("CallTool(graph_status) through the relay: %v", err)
	}
	text, ok := response.Content[0].(*sdkmcp.TextContent)
	if !ok || text.Text != "answered by the daemon" {
		t.Fatalf("the relay did not forward the daemon's answer: %#v", response.Content[0])
	}
	// The tools the agent sees are the daemon's, listed without the relay
	// holding a surface of its own.
	tools, err := session.ListTools(context.Background(), nil)
	if err != nil {
		t.Fatalf("ListTools through the relay: %v", err)
	}
	if len(tools.Tools) != 1 || tools.Tools[0].Name != "graph_status" {
		t.Fatalf("tools through the relay = %#v, want the daemon's one", tools.Tools)
	}
}

// TestTheRelayOpensNoStandaloneStream pins the invariant that would otherwise
// fail in silence.
//
// The persistent GET that carries messages a server starts with no request in
// flight is opened by `sessionUpdated`, which only the SDK's Client calls -- and
// the relay has none, so the stream never opens whatever `DisableStandaloneSSE`
// says. Today nothing is lost, because the two messages kivgraph initiates both
// happen inside a call and come back down that call's own stream. The day the
// daemon sends something with no request open, the relay swallows it. This
// test is what makes that a decision rather than an accident.
func TestTheRelayOpensNoStandaloneStream(t *testing.T) {
	fake := startFakeDaemon(t, "9.9.9")
	session, _ := connectThroughRelay(t, fake, "9.9.9")
	if _, err := session.CallTool(context.Background(),
		&sdkmcp.CallToolParams{Name: "graph_status", Arguments: map[string]any{}}); err != nil {
		t.Fatalf("CallTool(graph_status) through the relay: %v", err)
	}
	if !fake.saw(http.MethodPost, daemon.MCPPath) {
		t.Fatal("the daemon saw no POST, so this proves nothing about the GET")
	}
	// A GET elsewhere is the reachability probe and is meant to be there; a GET
	// on the MCP path is the standalone stream, and that is the one the relay
	// cannot service.
	if fake.saw(http.MethodGet, daemon.MCPPath) {
		t.Fatal("the relay opened the standalone SSE stream, which it cannot service")
	}
}

// TestTheRelayRefusesADaemonOnAnotherRelease is the one failure this design
// adds that could not happen before, and it is the price of the daemon having
// one owner.
//
// The `.mcpb` bundle carries its own binary but takes `stateDirectory` from the
// configuration, so two installations share a daemon by construction. Restarting
// it would take the graph away from whoever else is using it, so the relay
// refuses instead -- and refuses before the agent is handed the handshake, so
// nobody is left holding a session this process has decided not to serve.
func TestTheRelayRefusesADaemonOnAnotherRelease(t *testing.T) {
	fake := startFakeDaemon(t, "0.8.1")
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	clientSide, relaySide := sdkmcp.NewInMemoryTransports()
	outcome := make(chan error, 1)
	go func() { outcome <- run(ctx, fake.endpoint(), relaySide, "0.9.1") }()

	client := sdkmcp.NewClient(&sdkmcp.Implementation{Name: "agent", Version: "1.0.0"}, nil)
	if session, err := client.Connect(ctx, clientSide, nil); err == nil {
		_ = session.Close()
		t.Fatal("the agent was handed a session the relay had decided not to serve")
	}

	select {
	case err := <-outcome:
		if !errors.Is(err, ErrVersionSkew) {
			t.Fatalf("relay error = %v, want it to wrap ErrVersionSkew", err)
		}
		// The message has to be actionable: a refusal that names neither
		// version nor the way out is a wall.
		for _, want := range []string{"0.8.1", "0.9.1", "kivgraph update", "kivgraph daemon install"} {
			if !strings.Contains(err.Error(), want) {
				t.Fatalf("the refusal does not mention %q: %v", want, err)
			}
		}
	case <-ctx.Done():
		t.Fatal("the relay never returned")
	}
}

// A daemon that reports no version is a release older than this check, and it
// has the same right to answer as it always had. Refusing it would make an
// upgrade of this side break every client of the other.
func TestTheRelayServesADaemonThatNamesNoVersion(t *testing.T) {
	fake := startFakeDaemon(t, "")
	session, _ := connectThroughRelay(t, fake, "0.9.1")
	if _, err := session.CallTool(context.Background(),
		&sdkmcp.CallToolParams{Name: "graph_status", Arguments: map[string]any{}}); err != nil {
		t.Fatalf("CallTool(graph_status) through the relay: %v", err)
	}
}

// TestAnUnreachableDaemonIsAFallbackAndNotAFailure is what the caller branches
// on. It is a distinct error because a relay that could not start has an
// in-process server to fall back to, and a relay that started and refused does
// not -- and because it must arrive before stdio is touched, or there is
// nothing left to fall back with.
func TestAnUnreachableDaemonIsAFallbackAndNotAFailure(t *testing.T) {
	// A port nothing is listening on: bind one, learn its address, release it.
	closed := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	address := closed.URL
	closed.Close()

	endpoint := daemon.Endpoint{URL: address, Token: testToken}
	if err := Reachable(context.Background(), endpoint); !errors.Is(err, ErrDaemonUnreachable) {
		t.Fatalf("Reachable() = %v, want it to wrap ErrDaemonUnreachable", err)
	}
	// And Run refuses before it can consume anything, which is what lets the
	// caller hand the same stdio to an in-process server.
	agent := &countingTransport{}
	if err := run(context.Background(), endpoint, agent, "0.9.1"); !errors.Is(err, ErrDaemonUnreachable) {
		t.Fatalf("run() = %v, want it to wrap ErrDaemonUnreachable", err)
	}
	if agent.connects != 0 {
		t.Fatalf("the relay connected the agent %d times before giving up", agent.connects)
	}
}

// TestAnEndpointWithNoHostIsUnreachable keeps a malformed endpoint file out of
// the failure column: it is another way to have no daemon, not a broken relay.
func TestAnEndpointWithNoHostIsUnreachable(t *testing.T) {
	for _, endpointURL := range []string{"", "not a url", "http://"} {
		err := Reachable(context.Background(), daemon.Endpoint{URL: endpointURL})
		if !errors.Is(err, ErrDaemonUnreachable) {
			t.Fatalf("Reachable(%q) = %v, want it to wrap ErrDaemonUnreachable", endpointURL, err)
		}
	}
}

// countingTransport records whether the relay ever reached for the agent's
// stream, which is the thing the fallback depends on it not doing.
type countingTransport struct{ connects int }

func (transport *countingTransport) Connect(context.Context) (sdkmcp.Connection, error) {
	transport.connects++
	return nil, errors.New("the agent must not have been connected")
}

// TestAListenerThatAnswersNothingIsUnreachable is why the probe is an HTTP
// request and not a TCP dial.
//
// A process that accepts the connection and never answers passes a dial. The
// agent's own handshake would then be the first thing to find out -- with no
// timeout on it, because an MCP session has to be able to sit idle, and with
// stdio already consumed so there is nothing left to fall back to. The
// difference between the two probes is a hang and a fallback.
func TestAListenerThatAnswersNothingIsUnreachable(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	t.Cleanup(func() { _ = listener.Close() })

	var held []net.Conn
	var mu sync.Mutex
	go func() {
		for {
			accepted, acceptErr := listener.Accept()
			if acceptErr != nil {
				return
			}
			// Held open and answered with nothing, which is the whole fixture:
			// closing it would be a refusal a dial could already see.
			mu.Lock()
			held = append(held, accepted)
			mu.Unlock()
		}
	}()
	t.Cleanup(func() {
		mu.Lock()
		defer mu.Unlock()
		for _, open := range held {
			_ = open.Close()
		}
	})

	endpoint := daemon.Endpoint{URL: "http://" + listener.Addr().String(), Token: testToken}
	started := time.Now()
	if err := Reachable(context.Background(), endpoint); !errors.Is(err, ErrDaemonUnreachable) {
		t.Fatalf("Reachable() = %v, want it to wrap ErrDaemonUnreachable", err)
	}
	// And it gave up on its own deadline rather than on the caller's patience.
	if waited := time.Since(started); waited > 4*reachTimeout {
		t.Fatalf("the probe waited %s, which is not bounded by reachTimeout (%s)", waited, reachTimeout)
	}
}

// TestAClientHangingUpIsNotAnError is the normal end of every MCP session, and
// it was being reported as a failure.
//
// The agent exits, its end of the stream closes, and the read that was waiting
// on it returns EOF. Handing that back made `serve` exit non-zero and log an
// error on the ordinary path -- which is what a client shows its user as a
// server that crashed. Measured before the fix: exit code 1 and
// `msg="MCP server stopped with error" error="EOF"` on a session that did
// nothing but say goodbye.
func TestAClientHangingUpIsNotAnError(t *testing.T) {
	fake := startFakeDaemon(t, "9.9.9")
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	clientSide, relaySide := sdkmcp.NewInMemoryTransports()
	outcome := make(chan error, 1)
	go func() { outcome <- run(ctx, fake.endpoint(), relaySide, "9.9.9") }()

	client := sdkmcp.NewClient(&sdkmcp.Implementation{Name: "agent", Version: "1.0.0"}, nil)
	session, err := client.Connect(ctx, clientSide, nil)
	if err != nil {
		t.Fatalf("connect through the relay: %v", err)
	}
	if _, err := session.CallTool(ctx,
		&sdkmcp.CallToolParams{Name: "graph_status", Arguments: map[string]any{}}); err != nil {
		t.Fatalf("CallTool(graph_status) through the relay: %v", err)
	}
	if err := session.Close(); err != nil {
		t.Fatalf("the agent could not close its session: %v", err)
	}

	select {
	case err := <-outcome:
		if err != nil {
			t.Fatalf("the relay reported %v when the client simply went away", err)
		}
	case <-ctx.Done():
		t.Fatal("the relay never returned after the client closed")
	}
}

// TestASessionThatBreaksIsStillAnError keeps the fix above from swallowing what
// it is supposed to let through: a relay that stopped for a reason has to say
// so, or `serve` exits zero on a daemon that refused it.
func TestASessionThatBreaksIsStillAnError(t *testing.T) {
	broken := errors.New("the daemon stopped answering mid-session")
	if endOfSession(broken) == nil {
		t.Fatal("a real failure was reported as a session that simply ended")
	}
	if err := endOfSession(fmt.Errorf("relaying: %w", ErrVersionSkew)); err == nil {
		t.Fatal("a version-skew refusal was swallowed as a clean end")
	}
}
