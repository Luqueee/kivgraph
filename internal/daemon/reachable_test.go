package daemon

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// TestReachableTellsALiveEndpointFromAStaleOne is the distinction the whole file
// exists for. A killed daemon leaves its endpoint behind, and a caller that
// wrote that url into a client configuration would hand every client an address
// nothing answers on.
//
// A plain listener, not a daemon: what separates the two cases is whether a
// connection succeeds, and serving MCP here would test the fixture instead.
func TestReachableTellsALiveEndpointFromAStaleOne(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	endpoint := Endpoint{URL: "http://" + listener.Addr().String() + "/mcp", Token: "live", PID: os.Getpid()}

	if err := Reachable(context.Background(), endpoint, time.Second); err != nil {
		t.Fatalf("Reachable() on a live endpoint = %v, want nil", err)
	}
	if err := listener.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}
	// The same endpoint value, now with nothing behind it. Only the connection
	// tells them apart, which is what a client's first call makes.
	if err := Reachable(context.Background(), endpoint, 200*time.Millisecond); !errors.Is(err, ErrEndpointUnreachable) {
		t.Fatalf("Reachable() on a dead endpoint = %v, want ErrEndpointUnreachable", err)
	}
}

// TestReachableRefusesAMalformedEndpoint keeps a damaged file from being dialled
// as if it were an address. A url with no port would dial port zero, which
// succeeds on nothing and fails for the wrong reason.
func TestReachableRefusesAMalformedEndpoint(t *testing.T) {
	for name, endpoint := range map[string]Endpoint{
		"no host": {URL: "http:///mcp", Token: "t"},
		"no port": {URL: "http://127.0.0.1/mcp", Token: "t"},
		"garbage": {URL: "http://[::1", Token: "t"},
	} {
		t.Run(name, func(t *testing.T) {
			err := Reachable(context.Background(), endpoint, time.Second)
			if err == nil {
				t.Fatal("Reachable() accepted a malformed endpoint")
			}
			if errors.Is(err, ErrEndpointUnreachable) {
				t.Fatalf("a malformed url was reported as unreachable, which names the wrong remedy: %v", err)
			}
		})
	}
}

// publishLate binds a listener and publishes an endpoint for it after a delay,
// which is what a supervisor does: it starts the daemon asynchronously, so the
// file does not exist when the caller asks.
//
// It listens rather than serving MCP because WaitReachable dials and does not
// handshake -- a full daemon here would test the fixture, not the wait. Errors
// go to a channel because t.Fatal from a goroutine is not allowed.
func publishLate(t *testing.T, directory string, delay time.Duration) <-chan Endpoint {
	t.Helper()
	published := make(chan Endpoint, 1)
	go func() {
		time.Sleep(delay)
		listener, err := net.Listen("tcp", "127.0.0.1:0")
		if err != nil {
			t.Errorf("listen: %v", err)
			close(published)
			return
		}
		t.Cleanup(func() { _ = listener.Close() })
		endpoint := Endpoint{
			URL:   "http://" + listener.Addr().String() + "/mcp",
			Token: "live",
			PID:   os.Getpid(),
		}
		encoded, err := json.Marshal(endpoint)
		if err != nil {
			t.Errorf("marshal: %v", err)
			close(published)
			return
		}
		if err := os.WriteFile(filepath.Join(directory, EndpointName), encoded, 0o600); err != nil {
			t.Errorf("write endpoint: %v", err)
			close(published)
			return
		}
		published <- endpoint
	}()
	return published
}

// TestWaitReachableFindsADaemonThatStartsLate covers what a supervisor does: it
// starts the daemon asynchronously, so a caller that read the endpoint once
// would find no file at all.
func TestWaitReachableFindsADaemonThatStartsLate(t *testing.T) {
	directory := shortTempDir(t)
	published := publishLate(t, directory, 150*time.Millisecond)

	endpoint, err := WaitReachable(context.Background(), directory, 5*time.Second)
	if err != nil {
		t.Fatalf("WaitReachable() error = %v", err)
	}
	if endpoint.URL != (<-published).URL {
		t.Fatalf("WaitReachable() returned %q, which is not the daemon that started", endpoint.URL)
	}
}

// TestWaitReachableGivesUpAndSaysWhy keeps a daemon that never comes up from
// hanging the command that asked for it. The deadline is named in the error
// because a caller has to be able to report it.
func TestWaitReachableGivesUpAndSaysWhy(t *testing.T) {
	_, err := WaitReachable(context.Background(), shortTempDir(t), 250*time.Millisecond)
	if err == nil {
		t.Fatal("WaitReachable() succeeded with no daemon at all")
	}
	if !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("error = %v, want it to carry the missing endpoint", err)
	}
}

// TestWaitReachableRereadsTheEndpoint covers the stale-address case: a dead
// daemon's file carries the previous port, and waiting on it would time out
// while the new daemon was already answering somewhere else.
func TestWaitReachableRereadsTheEndpoint(t *testing.T) {
	directory := shortTempDir(t)
	// A file naming a port nothing listens on, which is what a killed daemon
	// leaves behind.
	stale, err := json.Marshal(Endpoint{URL: "http://127.0.0.1:1/mcp", Token: "stale", PID: 1})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if err := os.WriteFile(filepath.Join(directory, EndpointName), stale, 0o600); err != nil {
		t.Fatalf("write endpoint: %v", err)
	}
	publishLate(t, directory, 150*time.Millisecond)

	endpoint, err := WaitReachable(context.Background(), directory, 5*time.Second)
	if err != nil {
		t.Fatalf("WaitReachable() error = %v", err)
	}
	if endpoint.Token == "stale" {
		t.Fatal("WaitReachable() returned the stale endpoint, so a client would get a dead address")
	}
}
