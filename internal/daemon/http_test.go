package daemon

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/Luqueee/kivgraph/internal/hotsnapshot"
	"github.com/Luqueee/kivgraph/internal/metrics"
)

// startHTTP runs a daemon's HTTP half over a temporary state directory and
// returns its published endpoint.
//
// Port zero, because a fixed port makes a test suite depend on what else is
// running on the machine. The endpoint file carries the address that was
// actually bound, which is the same path a real client takes.
func startHTTP(t *testing.T, store *hotsnapshot.SnapshotStore) (*HTTPServer, Endpoint, context.CancelFunc) {
	t.Helper()
	directory := shortTempDir(t)
	options := Options{
		StateDirectory: directory,
		SnapshotStore:  store,
		Registry:       metrics.NewRegistry(),
		Indexer:        stubIndexer{},
	}
	served, err := ListenHTTP(options, HTTPOptions{Address: "127.0.0.1:0"})
	if err != nil {
		t.Fatalf("ListenHTTP() error = %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	var running sync.WaitGroup
	running.Add(1)
	go func() {
		defer running.Done()
		if err := served.Serve(ctx); err != nil {
			t.Errorf("Serve() error = %v", err)
		}
	}()
	t.Cleanup(func() {
		cancel()
		running.Wait()
	})

	endpoint, err := ReadEndpoint(directory)
	if err != nil {
		t.Fatalf("ReadEndpoint() error = %v", err)
	}
	return served, endpoint, cancel
}

// connectHTTP opens an MCP session over the published endpoint.
func connectHTTP(t *testing.T, endpoint Endpoint) *sdkmcp.ClientSession {
	t.Helper()
	client := sdkmcp.NewClient(&sdkmcp.Implementation{Name: "test", Version: "0.0.1"}, nil)
	transport := &sdkmcp.StreamableClientTransport{
		Endpoint: endpoint.URL,
		HTTPClient: &http.Client{
			Transport: bearer{token: endpoint.Token, next: http.DefaultTransport},
			Timeout:   10 * time.Second,
		},
	}
	session, err := client.Connect(context.Background(), transport, nil)
	if err != nil {
		t.Fatalf("connect %s: %v", endpoint.URL, err)
	}
	t.Cleanup(func() { _ = session.Close() })
	return session
}

// bearer attaches the token the endpoint published.
type bearer struct {
	token string
	next  http.RoundTripper
}

func (attach bearer) RoundTrip(request *http.Request) (*http.Response, error) {
	cloned := request.Clone(request.Context())
	cloned.Header.Set("Authorization", BearerHeader(attach.token))
	return attach.next.RoundTrip(cloned)
}

// TestHTTPAnswersAClientThatTakesAURL is the whole reason this transport exists.
// An editor's configuration takes an executable or a url, never a socket path,
// so without this the daemon's measured saving is unreachable.
func TestHTTPAnswersAClientThatTakesAURL(t *testing.T) {
	_, endpoint, _ := startHTTP(t, nil)
	result, err := connectHTTP(t, endpoint).ListTools(context.Background(), nil)
	if err != nil {
		t.Fatalf("ListTools() error = %v", err)
	}
	if len(result.Tools) == 0 {
		t.Fatal("the daemon published no tools over HTTP")
	}
}

// TestHTTPRefusesARequestWithNoTokenOrAWrongOne is the guard that replaces the
// socket's mode. A port has no path, so any local process can reach it; without
// the token the graph is readable by anything on the machine.
func TestHTTPRefusesARequestWithNoTokenOrAWrongOne(t *testing.T) {
	_, endpoint, _ := startHTTP(t, nil)

	body := strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"tools/list","params":{}}`)
	for name, token := range map[string]string{"none": "", "wrong": "not-the-token"} {
		request, err := http.NewRequest(http.MethodPost, endpoint.URL, body)
		if err != nil {
			t.Fatalf("%s: new request: %v", name, err)
		}
		request.Header.Set("Content-Type", "application/json")
		request.Header.Set("Accept", "application/json, text/event-stream")
		if token != "" {
			request.Header.Set("Authorization", BearerHeader(token))
		}
		response, err := http.DefaultClient.Do(request)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		_ = response.Body.Close()
		if response.StatusCode != http.StatusUnauthorized {
			t.Fatalf("%s token: status = %d, want 401", name, response.StatusCode)
		}
	}
}

// TestHTTPRefusesARemoteOrigin covers what the token cannot: a web page cannot
// read the token, but it can make the user's own browser send requests to
// 127.0.0.1. The spec asks a local server to validate Origin for exactly this.
func TestHTTPRefusesARemoteOrigin(t *testing.T) {
	_, endpoint, _ := startHTTP(t, nil)

	tests := map[string]struct {
		origin string
		want   int
	}{
		"remote page":    {origin: "https://evil.example", want: http.StatusForbidden},
		"loopback page":  {origin: "http://127.0.0.1:5173", want: http.StatusOK},
		"localhost page": {origin: "http://localhost:5173", want: http.StatusOK},
	}
	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			request, err := http.NewRequest(http.MethodPost, endpoint.URL,
				strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-06-18","capabilities":{},"clientInfo":{"name":"t","version":"1"}}}`))
			if err != nil {
				t.Fatalf("new request: %v", err)
			}
			request.Header.Set("Content-Type", "application/json")
			request.Header.Set("Accept", "application/json, text/event-stream")
			request.Header.Set("Authorization", BearerHeader(endpoint.Token))
			request.Header.Set("Origin", test.origin)
			response, err := http.DefaultClient.Do(request)
			if err != nil {
				t.Fatalf("do: %v", err)
			}
			defer func() { _ = response.Body.Close() }()
			if test.want == http.StatusForbidden && response.StatusCode != http.StatusForbidden {
				t.Fatalf("status = %d, want 403 for %s", response.StatusCode, test.origin)
			}
			if test.want == http.StatusOK && response.StatusCode == http.StatusForbidden {
				t.Fatalf("status = 403, want a loopback origin to be allowed: %s", test.origin)
			}
		})
	}
}

// TestHTTPRefusesABindOutsideLoopback is a refusal rather than a warning. The
// socket's whole guarantee is that the graph does not leave the machine, and a
// warning on a server nobody is watching is not a decision.
func TestHTTPRefusesABindOutsideLoopback(t *testing.T) {
	options := Options{StateDirectory: shortTempDir(t), Registry: metrics.NewRegistry()}
	for _, address := range []string{"0.0.0.0:0", ":0", "192.0.2.1:0"} {
		if _, err := ListenHTTP(options, HTTPOptions{Address: address}); !errors.Is(err, ErrAddressNotLoopback) {
			t.Fatalf("ListenHTTP(%q) error = %v, want ErrAddressNotLoopback", address, err)
		}
	}

	// And the escape hatch works, because refusing outright would leave a real
	// deployment with no path at all. It warns, and the warning names the cost.
	var warned []string
	served, err := ListenHTTP(options, HTTPOptions{
		Address:     "0.0.0.0:0",
		AllowRemote: true,
		OnWarning:   func(message string) { warned = append(warned, message) },
	})
	if err != nil {
		t.Fatalf("ListenHTTP() with --allow-remote error = %v", err)
	}
	defer func() { _ = served.listener.Close() }()
	if len(warned) != 1 || !strings.Contains(warned[0], "not loopback") {
		t.Fatalf("warnings = %v, want one naming the bind", warned)
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

// TestTwoDaemonsGetDifferentTokens is what keeps two configurations apart on a
// transport that has no path. The socket was separated by its directory; here
// only the token can do it.
func TestTwoDaemonsGetDifferentTokens(t *testing.T) {
	_, first, _ := startHTTP(t, nil)
	_, second, _ := startHTTP(t, nil)
	if first.Token == second.Token {
		t.Fatal("two daemons minted the same token, so either would answer the other's client")
	}

	// And the first daemon's token must not open the second.
	request, err := http.NewRequest(http.MethodPost, second.URL,
		strings.NewReader(`{"jsonrpc":"2.0","id":1,"method":"tools/list","params":{}}`))
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Accept", "application/json, text/event-stream")
	request.Header.Set("Authorization", BearerHeader(first.Token))
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatalf("do: %v", err)
	}
	_ = response.Body.Close()
	if response.StatusCode != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401 for another daemon's token", response.StatusCode)
	}
}

// TestServeRemovesTheEndpointWhenItStops keeps a dead port with a dead secret
// out of a client's configuration path.
func TestServeRemovesTheEndpointWhenItStops(t *testing.T) {
	served, _, cancel := startHTTP(t, nil)
	if _, err := os.Stat(served.path); err != nil {
		t.Fatalf("the endpoint was not published: %v", err)
	}
	cancel()

	deadline := time.Now().Add(3 * time.Second)
	for {
		if _, err := os.Stat(served.path); errors.Is(err, os.ErrNotExist) {
			return
		}
		if time.Now().After(deadline) {
			t.Fatal("the endpoint file outlived the daemon that published it")
		}
		time.Sleep(20 * time.Millisecond)
	}
}

// TestReadEndpointRefusesAFileWithNoToken keeps a truncated or hand-edited file
// from being read as an endpoint a client could use.
func TestReadEndpointRefusesAFileWithNoToken(t *testing.T) {
	directory := shortTempDir(t)
	for name, content := range map[string]any{
		"no token": Endpoint{URL: "http://127.0.0.1:7788/mcp"},
		"no url":   Endpoint{Token: "abc"},
	} {
		encoded, err := json.Marshal(content)
		if err != nil {
			t.Fatalf("%s: marshal: %v", name, err)
		}
		if err := os.WriteFile(EndpointPath(directory), encoded, 0o600); err != nil {
			t.Fatalf("%s: write: %v", name, err)
		}
		if _, err := ReadEndpoint(directory); err == nil {
			t.Fatalf("%s: ReadEndpoint() accepted it", name)
		}
	}
}

// TestLoopbackAddressKnowsWhatIsNotLocal pins the case a reader most often gets
// wrong: an empty host means every interface, so `:7788` is not loopback.
func TestLoopbackAddressKnowsWhatIsNotLocal(t *testing.T) {
	local := []string{"127.0.0.1:7788", "localhost:7788", "[::1]:7788", "127.9.9.9:1"}
	remote := []string{":7788", "0.0.0.0:7788", "[::]:7788", "192.0.2.1:7788", "example.com:7788", "garbage"}
	for _, address := range local {
		if !LoopbackAddress(address) {
			t.Fatalf("LoopbackAddress(%q) = false, want true", address)
		}
	}
	for _, address := range remote {
		if LoopbackAddress(address) {
			t.Fatalf("LoopbackAddress(%q) = true, want false", address)
		}
	}
}

// TestHTTPAndSocketServeTheSameGraph is the property that makes two transports
// one daemon rather than two: a generation published after startup reaches a
// client on either door, because each session builds its own server.
func TestHTTPAndSocketServeTheSameGraph(t *testing.T) {
	store := hotsnapshot.NewSnapshotStore(nil)
	directory := shortTempDir(t)
	options := Options{
		StateDirectory: directory,
		SnapshotStore:  store,
		Registry:       metrics.NewRegistry(),
		Indexer:        stubIndexer{},
	}
	listener, err := Listen(options)
	if err != nil {
		t.Fatalf("Listen() error = %v", err)
	}
	served, err := ListenHTTP(options, HTTPOptions{Address: "127.0.0.1:0"})
	if err != nil {
		t.Fatalf("ListenHTTP() error = %v", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	var running sync.WaitGroup
	running.Add(2)
	go func() { defer running.Done(); _ = Serve(ctx, listener, options) }()
	go func() { defer running.Done(); _ = served.Serve(ctx) }()
	defer func() {
		cancel()
		running.Wait()
	}()

	endpoint, err := ReadEndpoint(directory)
	if err != nil {
		t.Fatalf("ReadEndpoint() error = %v", err)
	}

	if err := store.Publish(emptySnapshot(t)); err != nil {
		t.Fatalf("Publish() error = %v", err)
	}

	overHTTP := toolNamesOf(t, connectHTTP(t, endpoint))
	overSocket := toolNamesOf(t, dial(t, listener.Addr().String()))
	if len(overHTTP) != len(overSocket) {
		t.Fatalf("HTTP published %d tools and the socket %d: %v vs %v",
			len(overHTTP), len(overSocket), overHTTP, overSocket)
	}
	for index := range overHTTP {
		if overHTTP[index] != overSocket[index] {
			t.Fatalf("the two transports published different tools: %v vs %v", overHTTP, overSocket)
		}
	}
}

// toolNamesOf lists what a session was told it can call, in order.
func toolNamesOf(t *testing.T, session *sdkmcp.ClientSession) []string {
	t.Helper()
	result, err := session.ListTools(context.Background(), nil)
	if err != nil {
		t.Fatalf("ListTools() error = %v", err)
	}
	names := make([]string, 0, len(result.Tools))
	for _, tool := range result.Tools {
		names = append(names, tool.Name)
	}
	return names
}

// TestListenHTTPNamesAPortAlreadyTaken keeps a collision from looking like a
// daemon that started. Two state directories share the default port on purpose:
// the second one has to fail and say so, because an ephemeral port would leave
// every client configuration pointing at a dead one.
func TestListenHTTPNamesAPortAlreadyTaken(t *testing.T) {
	held, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer func() { _ = held.Close() }()

	options := Options{StateDirectory: shortTempDir(t), Registry: metrics.NewRegistry()}
	_, err = ListenHTTP(options, HTTPOptions{Address: held.Addr().String()})
	if err == nil {
		t.Fatal("ListenHTTP() bound a port another listener holds")
	}
	if !strings.Contains(err.Error(), held.Addr().String()) {
		t.Fatalf("error = %v, want it to name the address", err)
	}
}

// TestTheTokenSurvivesARestart is what makes the transport configurable at all.
// A client configuration holds the token; a token minted afresh on every start
// would invalidate that file every time the daemon came back, which is not an
// integration but a chore.
func TestTheTokenSurvivesARestart(t *testing.T) {
	directory := shortTempDir(t)
	options := Options{StateDirectory: directory, Registry: metrics.NewRegistry()}

	first, err := ListenHTTP(options, HTTPOptions{Address: "127.0.0.1:0"})
	if err != nil {
		t.Fatalf("first ListenHTTP() error = %v", err)
	}
	_ = first.listener.Close()

	second, err := ListenHTTP(options, HTTPOptions{Address: "127.0.0.1:0"})
	if err != nil {
		t.Fatalf("second ListenHTTP() error = %v", err)
	}
	defer func() { _ = second.listener.Close() }()

	if first.endpoint.Token != second.endpoint.Token {
		t.Fatal("the daemon minted a new token on restart, so every configured client would break")
	}
	// And the address is re-read rather than remembered: two binds on port zero
	// land on different ports, and the endpoint has to say which.
	if first.endpoint.URL == second.endpoint.URL {
		t.Fatalf("both runs published %s, but each bound its own port", first.endpoint.URL)
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

// TestALeakyTokenFileIsNamedRatherThanReplaced covers the choice a reader would
// most likely make the other way. Re-minting would fix the permission and break
// every client configured with the old token; the warning fixes neither, and
// says so.
func TestALeakyTokenFileIsNamedRatherThanReplaced(t *testing.T) {
	directory := shortTempDir(t)
	if err := os.MkdirAll(directory, 0o700); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(TokenPath(directory), []byte("a-token-a-client-already-has\n"), 0o644); err != nil {
		t.Fatalf("write the token: %v", err)
	}

	var warned []string
	served, err := ListenHTTP(
		Options{StateDirectory: directory, Registry: metrics.NewRegistry()},
		HTTPOptions{Address: "127.0.0.1:0", OnWarning: func(m string) { warned = append(warned, m) }},
	)
	if err != nil {
		t.Fatalf("ListenHTTP() error = %v", err)
	}
	defer func() { _ = served.listener.Close() }()

	if served.endpoint.Token != "a-token-a-client-already-has" {
		t.Fatalf("token = %q, want the one already on disk", served.endpoint.Token)
	}
	if len(warned) != 1 || !strings.Contains(warned[0], "read the token") {
		t.Fatalf("warnings = %v, want one naming the leak", warned)
	}
}

// TestATruncatedTokenFileIsMintedOver is the other half: an empty file is a
// failed write, not a token, and reusing it would serve a daemon no client can
// authenticate against.
func TestATruncatedTokenFileIsMintedOver(t *testing.T) {
	directory := shortTempDir(t)
	if err := os.MkdirAll(directory, 0o700); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(TokenPath(directory), []byte("  \n"), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}
	served, err := ListenHTTP(
		Options{StateDirectory: directory, Registry: metrics.NewRegistry()},
		HTTPOptions{Address: "127.0.0.1:0"},
	)
	if err != nil {
		t.Fatalf("ListenHTTP() error = %v", err)
	}
	defer func() { _ = served.listener.Close() }()
	if len(served.endpoint.Token) < 32 {
		t.Fatalf("token = %q, want a freshly minted one", served.endpoint.Token)
	}
}

// TestCloseWithdrawsAnEndpointThatNeverServed is the case a caller reaches when
// something else fails after the bind. The file claims a daemon is answering, so
// leaving it behind sends the next client to a closed port with no way to tell
// that from a bug.
func TestCloseWithdrawsAnEndpointThatNeverServed(t *testing.T) {
	directory := shortTempDir(t)
	served, err := ListenHTTP(
		Options{StateDirectory: directory, Registry: metrics.NewRegistry()},
		HTTPOptions{Address: "127.0.0.1:0"},
	)
	if err != nil {
		t.Fatalf("ListenHTTP() error = %v", err)
	}
	address := served.Addr().String()
	if _, err := os.Stat(EndpointPath(directory)); err != nil {
		t.Fatalf("the endpoint was not published: %v", err)
	}

	if err := served.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}
	if _, err := os.Stat(EndpointPath(directory)); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("the endpoint survived Close(): %v", err)
	}
	// The bind is released too, or the next daemon cannot start.
	again, err := net.Listen("tcp", address)
	if err != nil {
		t.Fatalf("the port was still held after Close(): %v", err)
	}
	_ = again.Close()

	// And the token stays: it is identity, not liveness, and a client is
	// configured with it.
	if _, err := os.Stat(TokenPath(directory)); err != nil {
		t.Fatalf("Close() took the token with it: %v", err)
	}
}
