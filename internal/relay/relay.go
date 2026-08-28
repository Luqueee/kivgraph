// Package relay forwards one stdio MCP session to a running daemon.
//
// It is a package of its own and not a file in internal/mcp, which is where
// ADR 0084 expected it: internal/daemon imports internal/mcp, so a relay that
// needs an Endpoint and a bearer header cannot live there without a cycle.
package relay

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"sync"
	"time"

	"github.com/modelcontextprotocol/go-sdk/jsonrpc"
	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/Luqueee/kivgraph/internal/daemon"
	"github.com/Luqueee/kivgraph/internal/version"
)

// ErrDaemonUnreachable reports that no daemon answered where the endpoint file
// said one would. It is the caller's signal to serve in process instead, and it
// is deliberately distinct from every other failure here: a relay that could
// not start is a fallback, and a relay that started and refused is not.
var ErrDaemonUnreachable = errors.New("relay: no daemon answered the published endpoint")

// ErrVersionSkew reports a daemon running a different release from this
// process.
//
// It is refused rather than repaired. The `.mcpb` bundle carries its own
// binary but takes `stateDirectory` from the configuration, so two
// installations share a daemon by construction -- and restarting it would take
// the graph away from whoever else is using it. This is the one failure this
// design adds that could not happen before, because before there were never
// two processes that could disagree.
var ErrVersionSkew = errors.New("relay: the daemon runs a different kivgraph release")

// reachTimeout bounds the probe that decides between relaying and serving in
// process. It is short on purpose: a client is waiting on the handshake behind
// it, and the question is only whether something answers HTTP here.
const reachTimeout = 2 * time.Second

// Run serves one MCP session by forwarding it to a running daemon, holding no
// graph of its own.
//
// Neither the SDK's Server nor its Client appears. A Connection is Read, Write
// and Close of a jsonrpc.Message, so the relay needs no session state: messages
// cross opaque, and a tool added to the daemon reaches the agent without this
// file knowing it exists.
//
// One message is not opaque, and it is the version check ADR 0084 decided on.
// See skewWatch.
func Run(ctx context.Context, endpoint daemon.Endpoint) error {
	return run(ctx, endpoint, &sdkmcp.StdioTransport{}, version.Value)
}

func run(
	ctx context.Context,
	endpoint daemon.Endpoint,
	agentTransport sdkmcp.Transport,
	ourVersion string,
) error {
	if err := reachable(ctx, endpoint); err != nil {
		return err
	}
	// The daemon is connected before the agent, and the order is the whole
	// fallback. Connecting stdio first and failing here would leave the caller
	// unable to serve in process: the agent's `initialize` would already have
	// been read off a stream the in-process server was about to be given.
	served, err := (&sdkmcp.StreamableClientTransport{
		Endpoint:   endpoint.URL,
		HTTPClient: &http.Client{Transport: bearer{token: endpoint.Token, next: http.DefaultTransport}},
		// Written by hand because the SDK's default is the opposite and
		// nothing here would notice. The standalone GET that carries messages
		// the server starts on its own is opened by `sessionUpdated`, which
		// only the SDK's Client calls -- and this relay has no Client, so the
		// stream is never opened whatever this field says. Leaving it at the
		// default would describe a stream that does not exist.
		//
		// Today nothing is lost: NotifyProgress and Elicit are the only two
		// messages kivgraph starts, and both happen inside a call already in
		// flight, so they come back down that call's own SSE. The day the
		// daemon sends something with no request open, this swallows it.
		DisableStandaloneSSE: true,
	}).Connect(ctx)
	if err != nil {
		return fmt.Errorf("%w: %s: %w", ErrDaemonUnreachable, endpoint.URL, err)
	}
	defer served.Close()

	agent, err := agentTransport.Connect(ctx)
	if err != nil {
		return fmt.Errorf("connect the agent: %w", err)
	}
	defer agent.Close()

	watch := &skewWatch{ourVersion: ourVersion, url: endpoint.URL}
	// Buffered for two, so the loop that does not fail first can still return
	// into a channel nobody is reading and let the process exit.
	failures := make(chan error, 2)
	go func() { failures <- pipe(ctx, agent, served, watch.sent) }()
	go func() { failures <- pipe(ctx, served, agent, watch.received) }()
	return endOfSession(<-failures)
}

// endOfSession separates a session that ended from one that broke.
//
// A client closing its end is how *every* MCP session finishes: the agent
// exits, stdin reaches EOF, and the read that was waiting on it returns. There
// is nothing wrong with that, and reporting it as a failure made `serve` exit
// non-zero and log an error on the normal path -- which is what a client shows
// its user as "the server crashed".
//
// The context being cancelled is the same event from the other side: this
// process got a SIGTERM and `signal.NotifyContext` did its job.
func endOfSession(err error) error {
	switch {
	case err == nil,
		errors.Is(err, io.EOF),
		errors.Is(err, io.ErrClosedPipe),
		errors.Is(err, net.ErrClosed),
		errors.Is(err, context.Canceled):
		return nil
	}
	return err
}

// Reachable answers whether a daemon is listening where the endpoint file says.
//
// It is exported because only the caller can fall back: by the time Run has
// read the agent's handshake there is no in-process server left to hand it to.
// Run probes again for its own sake, so a caller that forgets still fails
// before touching stdio rather than after.
func Reachable(ctx context.Context, endpoint daemon.Endpoint) error {
	return reachable(ctx, endpoint)
}

// reachable answers whether a daemon is listening where the endpoint file says.
//
// It dials rather than trusting the file, because the file outlives a daemon
// that was killed: a relay that believed it would consume the agent's
// handshake before finding out, and by then there is no falling back.
//
// It asks for a path the daemon does not serve, which is every path but
// `/mcp`, and accepts any status at all. That is deliberate on three counts: it
// opens no session, it cannot be mistaken for the standalone SSE stream a GET
// on `/mcp` would start, and a 404 arrives with its headers immediately
// whatever the daemon is busy with.
//
// A TCP dial was not enough, and the difference is a hang rather than a
// fallback. A listener that accepts and never answers passes a dial, and the
// agent's own handshake is then the first thing to find out -- with no timeout
// on it, because an MCP session must be able to sit idle, and with stdio
// already consumed so there is nothing left to fall back to.
//
// It is stricter than `daemon.Reachable`, which dials, and the two are kept
// apart on purpose: that one decides whether to write a url into a client's
// configuration, where a listener that never answers surfaces later as a failed
// call the client can report. Here it would surface as a hang with nothing left
// to fall back to.
//
// What it still does not prove is that this is *our* daemon: another process
// can hold the port and answer 404 as readily. The bearer token settles that
// one request later, and by then the session is committed. That window is
// declared rather than closed, because closing it costs a full authenticated
// handshake before the agent's own.
func reachable(ctx context.Context, endpoint daemon.Endpoint) error {
	parsed, err := url.Parse(endpoint.URL)
	if err != nil {
		return fmt.Errorf("%w: %q: %w", ErrDaemonUnreachable, endpoint.URL, err)
	}
	if parsed.Host == "" || parsed.Scheme == "" {
		return fmt.Errorf("%w: %q names no host to reach", ErrDaemonUnreachable, endpoint.URL)
	}
	probeCtx, cancel := context.WithTimeout(ctx, reachTimeout)
	defer cancel()
	// Deliberately not daemon.MCPPath: that is the one path where a GET means
	// "open the stream this relay cannot service".
	probe := (&url.URL{Scheme: parsed.Scheme, Host: parsed.Host, Path: "/"}).String()
	request, err := http.NewRequestWithContext(probeCtx, http.MethodGet, probe, nil)
	if err != nil {
		return fmt.Errorf("%w: %q: %w", ErrDaemonUnreachable, endpoint.URL, err)
	}
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		return fmt.Errorf("%w: %s: %w", ErrDaemonUnreachable, parsed.Host, err)
	}
	// The body is not read: Do has already returned the headers, which is the
	// whole of the answer.
	return response.Body.Close()
}

// pipe copies messages one way until either end stops, showing each to inspect
// before it is forwarded.
//
// inspect runs before the write and not after, which is what lets the version
// check refuse: a response handed to the agent first would leave it holding a
// session this process has already decided not to serve.
func pipe(
	ctx context.Context,
	from, to sdkmcp.Connection,
	inspect func(jsonrpc.Message) error,
) error {
	for {
		message, err := from.Read(ctx)
		if err != nil {
			return err
		}
		if err := inspect(message); err != nil {
			return err
		}
		if err := to.Write(ctx, message); err != nil {
			return err
		}
	}
}

// skewWatch is the one place the relay looks inside a message.
//
// It reads the version out of the daemon's answer to `initialize` rather than
// asking for it separately, because a separate ask is a second session against
// a process this one is about to talk to anyway -- and because the handshake
// carries it on every protocol version, where the endpoint file would have to
// start carrying it and would then be absent on every daemon written before.
//
// It matches the response by id and not by position. A relay is one session, so
// in practice the answer to `initialize` is the first thing the daemon sends;
// relying on that would make the check silently stop running the day anything
// else arrives first.
type skewWatch struct {
	ourVersion string
	url        string

	mu         sync.Mutex
	initialize jsonrpc.ID
	pending    bool
	checked    bool
}

// sent remembers the id the agent's handshake carried.
func (watch *skewWatch) sent(message jsonrpc.Message) error {
	request, ok := message.(*jsonrpc.Request)
	if !ok || request.Method != "initialize" {
		return nil
	}
	watch.mu.Lock()
	watch.initialize, watch.pending = request.ID, true
	watch.mu.Unlock()
	return nil
}

// received refuses the session when the daemon names another release.
func (watch *skewWatch) received(message jsonrpc.Message) error {
	response, ok := message.(*jsonrpc.Response)
	if !ok {
		return nil
	}
	watch.mu.Lock()
	pending, wanted, checked := watch.pending, watch.initialize, watch.checked
	if pending && response.ID == wanted {
		watch.checked = true
	}
	watch.mu.Unlock()
	if !pending || checked || response.ID != wanted {
		return nil
	}
	var result struct {
		ServerInfo struct {
			Version string `json:"version"`
		} `json:"serverInfo"`
	}
	// A handshake this cannot decode is not a skew. Refusing on a shape we did
	// not recognise would turn every future field of the result into an outage.
	if err := json.Unmarshal(response.Result, &result); err != nil {
		return nil
	}
	// And neither is a daemon that names no version: that is a release older
	// than this check, and it has the same right to answer as it always had.
	if result.ServerInfo.Version == "" || result.ServerInfo.Version == watch.ourVersion {
		return nil
	}
	return fmt.Errorf(
		"%w: the daemon at %s runs kivgraph %s and this server is %s. They share a state directory, "+
			"so one of them is answering from a bundle the other replaced. Restart the daemon onto the "+
			"release you want: \"kivgraph update\" does it for a supervised daemon, and "+
			"\"kivgraph daemon install\" gives it the supervisor that can",
		ErrVersionSkew, watch.url, result.ServerInfo.Version, watch.ourVersion)
}

// bearer attaches the token the daemon published to every request, not only the
// first: the Streamable transport reconnects on its own, and a token sent once
// would fail the reconnection rather than the connection.
type bearer struct {
	token string
	next  http.RoundTripper
}

func (attach bearer) RoundTrip(request *http.Request) (*http.Response, error) {
	cloned := request.Clone(request.Context())
	cloned.Header.Set("Authorization", daemon.BearerHeader(attach.token))
	return attach.next.RoundTrip(cloned)
}
