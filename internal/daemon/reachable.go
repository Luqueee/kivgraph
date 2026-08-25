package daemon

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/url"
	"time"
)

// ErrEndpointUnreachable reports a published endpoint nothing answers on.
//
// It is a distinct error from a missing file because the two need different
// remedies: no file means no daemon ever ran, an unreachable one means a daemon
// died without withdrawing its endpoint. A caller about to write that url into a
// client configuration has to tell them apart.
var ErrEndpointUnreachable = errors.New("daemon: the published endpoint answers nothing")

// Reachable reports whether something is listening on a published endpoint.
//
// It dials rather than checking the PID, and that is the point: a PID answers
// whether a process exists, and a client needs to know whether a connection
// succeeds. On a machine that recycles PIDs the two disagree, and the connection
// is the one that decides whether the registration works.
//
// It does not send a request. Establishing the connection is what a client's
// first call does anyway, and an MCP handshake here would cost a session on the
// daemon to learn something the dial already answered.
func Reachable(ctx context.Context, endpoint Endpoint, timeout time.Duration) error {
	parsed, err := url.Parse(endpoint.URL)
	if err != nil {
		return fmt.Errorf("daemon: parse endpoint url %q: %w", endpoint.URL, err)
	}
	host := parsed.Host
	if host == "" {
		return fmt.Errorf("daemon: endpoint url %q names no host", endpoint.URL)
	}
	if parsed.Port() == "" {
		// A url with no port would dial port zero. The daemon always publishes
		// one, so this is a malformed file rather than a default to guess.
		return fmt.Errorf("daemon: endpoint url %q names no port", endpoint.URL)
	}
	dialer := net.Dialer{Timeout: timeout}
	connection, err := dialer.DialContext(ctx, "tcp", host)
	if err != nil {
		return fmt.Errorf("%w: %s: %w", ErrEndpointUnreachable, host, err)
	}
	return connection.Close()
}

// WaitReachable waits for a published endpoint to answer.
//
// A supervisor starts the daemon asynchronously, so a caller that installs one
// and reads the endpoint immediately would find no file at all. Polling is what
// closes that gap; the deadline is what keeps a daemon that cannot bind from
// hanging the command.
//
// It re-reads the endpoint on every attempt rather than once, because the file a
// dead daemon left behind carries the previous port: waiting on a stale address
// would time out while the new daemon was already answering elsewhere.
func WaitReachable(ctx context.Context, stateDirectory string, deadline time.Duration) (Endpoint, error) {
	const interval = 100 * time.Millisecond
	waitCtx, cancel := context.WithTimeout(ctx, deadline)
	defer cancel()

	var last error
	for {
		endpoint, err := ReadEndpoint(stateDirectory)
		if err == nil {
			if last = Reachable(waitCtx, endpoint, interval); last == nil {
				return endpoint, nil
			}
		} else {
			last = err
		}
		select {
		case <-waitCtx.Done():
			return Endpoint{}, fmt.Errorf("daemon: no endpoint answered in %s: %w", deadline, last)
		case <-time.After(interval):
		}
	}
}
