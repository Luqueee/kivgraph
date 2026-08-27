//go:build unix

package daemon

// The daemon's private files, tested where "private" has a meaning.
//
// These three assert POSIX mode bits: the socket, the endpoint and the token
// are readable by their owner and by nobody else, and Listen gets that without
// a chmod and without keeping the umask it narrowed. All of it is Unix, from
// the primitive up.
//
// Windows has no counterpart here yet, and the absence is deliberate rather
// than pending: withPrivateUmask is a documented no-op off Unix, so the socket
// inherits its directory's ACL, and whether the daemon should set an explicit
// DACL or move to a named pipe is an open decision recorded in
// docs/development/windows.md. When it is settled, a sibling file makes the
// same three claims in the vocabulary that platform actually has. Until then
// this file states what is verified and where, instead of asserting a mode
// that means nothing there.

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"
)

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

// TestEndpointIsPrivateAndNamesItsDaemon pins the file that carries the key. A
// world-readable token is the same as no token, and a reader has to be able to
// tell a live endpoint from one a killed daemon left behind.
func TestEndpointIsPrivateAndNamesItsDaemon(t *testing.T) {
	previous := syscall.Umask(0)
	t.Cleanup(func() { syscall.Umask(previous) })

	served, endpoint, _ := startHTTP(t, nil)
	info, err := os.Lstat(served.path)
	if err != nil {
		t.Fatalf("stat the endpoint: %v", err)
	}
	if mode := info.Mode().Perm(); mode != 0o600 {
		t.Fatalf("endpoint mode = %#o, want 0600 under a permissive umask", mode)
	}
	if endpoint.PID != os.Getpid() {
		t.Fatalf("endpoint pid = %d, want %d", endpoint.PID, os.Getpid())
	}
	if endpoint.Token == "" || len(endpoint.Token) < 32 {
		t.Fatalf("endpoint token = %q, want a long random string", endpoint.Token)
	}
	if !strings.HasSuffix(endpoint.URL, MCPPath) {
		t.Fatalf("endpoint url = %q, want it to end with %q", endpoint.URL, MCPPath)
	}
	// The socket is named too, so a client that cannot speak HTTP finds the
	// other door from the same file.
	if !strings.HasSuffix(endpoint.Socket, SocketName) {
		t.Fatalf("endpoint socket = %q, want the daemon's socket", endpoint.Socket)
	}
}

// TestTheTokenFileIsPrivateAndSurvivesTheEndpoint pins the split between the two
// files: identity persists, liveness does not.
func TestTheTokenFileIsPrivateAndSurvivesTheEndpoint(t *testing.T) {
	previous := syscall.Umask(0)
	t.Cleanup(func() { syscall.Umask(previous) })

	served, _, cancel := startHTTP(t, nil)
	directory := filepath.Dir(served.path)

	info, err := os.Lstat(TokenPath(directory))
	if err != nil {
		t.Fatalf("stat the token: %v", err)
	}
	if mode := info.Mode().Perm(); mode != 0o600 {
		t.Fatalf("token mode = %#o, want 0600 under a permissive umask", mode)
	}

	cancel()
	deadline := time.Now().Add(3 * time.Second)
	for {
		if _, err := os.Stat(served.path); errors.Is(err, os.ErrNotExist) {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("the endpoint outlived its daemon")
		}
		time.Sleep(20 * time.Millisecond)
	}
	if _, err := os.Stat(TokenPath(directory)); err != nil {
		t.Fatalf("the token did not survive the daemon that wrote it: %v", err)
	}
}
