package main

import (
	"context"
	"errors"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Luqueee/kivgraph/internal/config"
	"github.com/Luqueee/kivgraph/internal/daemon"
	"github.com/Luqueee/kivgraph/internal/hotsnapshot"
)

// TestAFailedStartLeavesNoEndpointBehind covers the half-started daemon.
//
// HTTP is published before the socket is bound, and that order is deliberate: a
// unix socket accepts the moment it is bound, before anyone calls Accept, so a
// client treating a successful dial as readiness would reach a daemon whose
// endpoint file did not exist yet. `benchmarks/daemon-cost` did exactly that and
// failed on the race.
//
// Racing for that window would be a test that passes under either order -- it is
// microseconds wide. This one is deterministic instead, and catches the order
// through its consequences rather than its timing: publishing HTTP first is what
// makes the token exist even when the socket bind fails afterwards, and it is
// what makes an endpoint exist that the failure path then has to withdraw.
// Reversing the two lines fails on the token; dropping the withdrawal fails on
// the endpoint. A file claiming a daemon is answering sends the next client to a
// closed port with no way to tell that from a bug.
func TestAFailedStartLeavesNoEndpointBehind(t *testing.T) {
	// A state directory whose socket path the kernel would truncate: the socket
	// address field is 104 bytes on darwin and 108 on linux, so this fails the
	// bind on both, deterministically and for a reason production declares.
	directory := filepath.Join("/tmp", strings.Repeat("kivgraph-daemon-startup-failure/", 4))
	if err := os.MkdirAll(directory, 0o700); err != nil {
		t.Fatalf("create the deep directory: %v", err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(filepath.Join("/tmp", "kivgraph-daemon-startup-failure")) })

	if _, err := daemon.SocketPath(directory); err == nil {
		t.Skipf("this platform accepts a %d-byte socket path, so the bind cannot be made to fail here", len(directory))
	}

	loaded := config.Loaded{}
	loaded.Config.Storage.DatabasePath = filepath.Join(directory, "graph.lbdb")

	store := hotsnapshot.NewSnapshotStore(nil)
	t.Cleanup(store.Close)

	err := runDaemon(
		slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError})),
		daemon.HTTPOptions{Address: "127.0.0.1:0"},
	)(context.Background(), loaded, store, nil, nil)

	if err == nil {
		t.Fatal("runDaemon() succeeded with a socket path the kernel would truncate")
	}
	if !errors.Is(err, daemon.ErrSocketPathTooLong) {
		t.Fatalf("error = %v, want ErrSocketPathTooLong", err)
	}
	// The endpoint was published before the socket was attempted, so the failure
	// path has to withdraw it. Leaving it is the defect this test exists for.
	if _, statErr := os.Stat(daemon.EndpointPath(directory)); !errors.Is(statErr, os.ErrNotExist) {
		t.Fatalf("a failed start left an endpoint behind: %v", statErr)
	}
	// The token stays: it is identity, and a client configured with it is not
	// wrong just because one start failed.
	if _, statErr := os.Stat(daemon.TokenPath(directory)); statErr != nil {
		t.Fatalf("the token did not survive a failed start: %v", statErr)
	}
}
