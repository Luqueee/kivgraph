// Command prototype is the relay ADR 0084 gated itself on, and nothing more.
//
// It is not the relay. There is no fallback for a machine with no daemon, no
// provisioning of a supervisor unit, no version check between the two ends and
// no test -- those are commits 2, 3 and 4 of `LUQUE-2233`, and they only exist
// if this one turns out to be cheap enough to pay for them. What it does have
// is the arrangement whose cost is the open question: one process holding a
// stdio connection on the agent's side and a Streamable HTTP connection on the
// daemon's, copying messages between them.
//
// Neither the SDK's Server nor its Client appears here, and that is the design
// rather than an omission. A Connection is Read, Write and Close of a
// jsonrpc.Message, so a relay needs no session state of its own: the message
// crosses opaque, and a tool added to the daemon reaches the agent without this
// file knowing it exists.
//
// What that costs is one real capability, and `DisableStandaloneSSE` below says
// where.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"

	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/Luqueee/kivgraph/internal/daemon"
)

func main() {
	stateDirectory := flag.String("state-dir", "", "state directory the daemon published its endpoint in")
	flag.Parse()
	if err := run(context.Background(), *stateDirectory); err != nil && !errors.Is(err, io.EOF) {
		fmt.Fprintf(os.Stderr, "relay: %v\n", err)
		os.Exit(1)
	}
}

func run(ctx context.Context, stateDirectory string) error {
	endpoint, err := daemon.ReadEndpoint(stateDirectory)
	if err != nil {
		return fmt.Errorf("read the daemon's endpoint: %w", err)
	}
	agent, err := (&sdkmcp.StdioTransport{}).Connect(ctx)
	if err != nil {
		return fmt.Errorf("connect the agent: %w", err)
	}
	defer agent.Close()

	served, err := (&sdkmcp.StreamableClientTransport{
		Endpoint:   endpoint.URL,
		HTTPClient: &http.Client{Transport: bearer{token: endpoint.Token, next: http.DefaultTransport}},
		// Written by hand, because the SDK's default is the opposite and
		// nothing here would notice. The standalone GET that carries messages
		// the server starts on its own is opened by `sessionUpdated`, which
		// only the SDK's Client calls -- and this relay has no Client, so the
		// stream is never opened whatever this field says. Leaving it at its
		// default would describe a stream that does not exist.
		//
		// Today nothing is lost: NotifyProgress and Elicit are the only two
		// messages kivgraph starts, and both happen inside a call already in
		// flight, so they come back down that call's own SSE. The day the
		// daemon sends something with no request open, this swallows it.
		DisableStandaloneSSE: true,
	}).Connect(ctx)
	if err != nil {
		return fmt.Errorf("connect %s: %w", endpoint.URL, err)
	}
	defer served.Close()

	// Buffered for two, so the loop that does not fail first can still return
	// into a channel nobody is reading and let the process exit.
	failures := make(chan error, 2)
	go func() { failures <- pipe(ctx, agent, served) }()
	go func() { failures <- pipe(ctx, served, agent) }()
	return <-failures
}

// pipe copies messages one way until either end stops. The agent closing stdin
// arrives here as an EOF from the read, which is how the relay learns that its
// client is gone -- the daemon behind it outlives both.
func pipe(ctx context.Context, from, to sdkmcp.Connection) error {
	for {
		message, err := from.Read(ctx)
		if err != nil {
			return err
		}
		if err := to.Write(ctx, message); err != nil {
			return err
		}
	}
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
