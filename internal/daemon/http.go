package daemon

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	sdkauth "github.com/modelcontextprotocol/go-sdk/auth"
	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"

	kivmcp "github.com/Luqueee/kivgraph/internal/mcp"
)

// EndpointName is the file a daemon publishes its HTTP endpoint in, inside the
// state directory.
//
// It exists because a port has no path. The socket carried its own key -- who can
// traverse the state directory can reach it -- and a loopback port carries none:
// any local process can connect. So the key moves into this file, which holds
// the token, and who can read it is exactly who could have opened the socket.
const EndpointName = "daemon.json"

// MCPPath is where the streamable HTTP transport is served.
const MCPPath = "/mcp"

// DefaultAddress is the daemon's preferred HTTP bind, loopback and on a port of
// its own. If that port is busy and the caller did not choose an address, the
// daemon selects and persists another loopback port.
//
// It is not the viewer's 7777: those are two contracts versioned independently,
// and sharing a port would mix them.
const DefaultAddress = "127.0.0.1:7788"

// PortName is the state file that keeps an automatically selected port stable
// across supervisor restarts. It contains only the decimal TCP port, not a
// secret or a public address.
const PortName = "daemon.port"

// tokenBytes is the length of the bearer token before encoding.
const tokenBytes = 32

// Endpoint is what a client needs to reach a running daemon.
type Endpoint struct {
	// URL is the streamable HTTP endpoint, ready to paste into a client.
	URL string `json:"url"`
	// Token authorises a request. A client sends it as `Authorization: Bearer`.
	Token string `json:"token"`
	// Socket is the unix socket the same daemon also serves, for a client that
	// cannot speak HTTP.
	Socket string `json:"socket"`
	// PID is the daemon holding both, so a reader can tell a live endpoint from
	// a file left behind.
	PID int `json:"pid"`
}

// EndpointPath returns the file a daemon for stateDirectory publishes.
func EndpointPath(stateDirectory string) string {
	return filepath.Join(stateDirectory, EndpointName)
}

// PortPath returns the persisted automatic HTTP port for a state directory.
func PortPath(stateDirectory string) string {
	return filepath.Join(stateDirectory, PortName)
}

func readPersistedPort(stateDirectory string) (int, error) {
	raw, err := os.ReadFile(PortPath(stateDirectory))
	if errors.Is(err, os.ErrNotExist) {
		return 0, nil
	}
	if err != nil {
		return 0, fmt.Errorf("read %s: %w", PortName, err)
	}
	port, err := strconv.Atoi(strings.TrimSpace(string(raw)))
	if err != nil || port < 1 || port > 65535 {
		return 0, fmt.Errorf("decode %s: %q is not a TCP port", PortName, strings.TrimSpace(string(raw)))
	}
	return port, nil
}

// ReadEndpoint reads a published endpoint.
//
// It does not check that the daemon is alive: a caller that needs to know dials
// it. Reporting a stale file as an error here would make every reader implement
// the same recovery.
func ReadEndpoint(stateDirectory string) (Endpoint, error) {
	raw, err := os.ReadFile(EndpointPath(stateDirectory))
	if err != nil {
		return Endpoint{}, err
	}
	var endpoint Endpoint
	if err := json.Unmarshal(raw, &endpoint); err != nil {
		return Endpoint{}, fmt.Errorf("decode %s: %w", EndpointName, err)
	}
	if endpoint.URL == "" || endpoint.Token == "" {
		return Endpoint{}, fmt.Errorf("%s names no url or no token", EndpointName)
	}
	return endpoint, nil
}

// TokenName is the file the token itself lives in, separately from the endpoint.
//
// The split is the difference between a transport a client can be configured
// for and one it cannot. The endpoint carries liveness -- an address and a pid --
// and is removed when the daemon stops. The token is identity, and it has to
// outlive the process: a client configuration holds it, and a token minted
// afresh on every restart would invalidate that configuration every time the
// daemon came back.
const TokenName = "daemon.token"

// TokenPath returns the token file for a state directory.
func TokenPath(stateDirectory string) string {
	return filepath.Join(stateDirectory, TokenName)
}

// loadOrCreateToken returns the state directory's token, minting one the first
// time.
//
// A token readable by others is worth a warning rather than a refusal: it is
// still the token those clients are configured with, and re-minting silently
// would break every one of them to fix a permission the user set.
func loadOrCreateToken(directory string, warn func(string)) (string, error) {
	path := TokenPath(directory)
	switch info, err := os.Lstat(path); {
	case err == nil:
		// The mode is only evidence where the platform keeps one. Go reports
		// every file on Windows as 0666, so asking here would warn about every
		// token on every host -- and a warning that is always printed is one
		// an operator learns to scroll past, which is the opposite of what
		// this line is for. What guards the file there is the ACL
		// writePrivateFile sets.
		if mode := info.Mode().Perm(); modeBitsAreEvidence && mode&0o077 != 0 && warn != nil {
			warn(fmt.Sprintf("%s is mode %#o: any user on this host can read the token that opens the graph", path, mode))
		}
		raw, err := os.ReadFile(path)
		if err != nil {
			return "", fmt.Errorf("read %s: %w", TokenName, err)
		}
		if token := strings.TrimSpace(string(raw)); token != "" {
			return token, nil
		}
		// An empty file is a truncated write, not a token. Mint over it.
	case errors.Is(err, os.ErrNotExist):
	default:
		return "", fmt.Errorf("inspect %s: %w", TokenName, err)
	}

	token, err := newToken()
	if err != nil {
		return "", err
	}
	if err := writePrivateFile(path, []byte(token+"\n")); err != nil {
		return "", err
	}
	return token, nil
}

// newToken mints a bearer token.
func newToken() (string, error) {
	raw := make([]byte, tokenBytes)
	if _, err := rand.Read(raw); err != nil {
		return "", fmt.Errorf("generate a token: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(raw), nil
}

// ErrAddressNotLoopback reports a bind that would expose the graph beyond this
// host.
var ErrAddressNotLoopback = errors.New("daemon: the HTTP address is not loopback")

// LoopbackAddress reports whether address binds only to this host.
//
// An empty host means every interface, which is the case a reader most often
// gets wrong: `:7788` is not loopback. There is no branch for it because none is
// needed -- ParseIP("") is nil, so the last line already answers no -- and a
// branch that cannot change an answer reads as a check that does something.
func LoopbackAddress(address string) bool {
	host, _, err := net.SplitHostPort(address)
	if err != nil {
		return false
	}
	if host == "localhost" {
		return true
	}
	parsed := net.ParseIP(host)
	return parsed != nil && parsed.IsLoopback()
}

// HTTPOptions are what the daemon's HTTP listener needs.
type HTTPOptions struct {
	// Address is the bind. Empty means the persisted port, or DefaultAddress on
	// the first start, with an automatic loopback fallback if it is occupied.
	Address string
	// AllowRemote permits a bind outside loopback. Without it a non-loopback
	// address is refused rather than warned about: the socket's whole guarantee
	// is that the graph does not leave the machine, and a warning on a server
	// nobody watches is not a decision.
	AllowRemote bool
	// OnWarning, when set, receives operational notices.
	OnWarning func(message string)
}

// HTTPServer is a daemon's HTTP half.
type HTTPServer struct {
	listener net.Listener
	server   *http.Server
	endpoint Endpoint
	path     string
}

// Endpoint returns what a client needs to reach this server.
func (served *HTTPServer) Endpoint() Endpoint { return served.endpoint }

// Addr returns the address actually bound, which is what a caller logs: a port
// of zero resolves to a real one only after the bind.
func (served *HTTPServer) Addr() net.Addr { return served.listener.Addr() }

// Close releases the bind and withdraws the published endpoint without ever
// having served.
//
// It exists for the caller that binds and then fails at something else: the
// endpoint file claims a daemon is answering, and leaving it behind sends the
// next client to a closed port with no way to tell that from a bug. Serve does
// the same withdrawal on its own way out, so calling both is safe.
func (served *HTTPServer) Close() error {
	_ = os.Remove(served.path)
	return served.listener.Close()
}

// ListenHTTP binds the daemon's HTTP transport and publishes its endpoint.
//
// The endpoint file is written with the same private umask as the socket,
// because it holds the token that replaces the socket's mode as the key.
func ListenHTTP(options Options, httpOptions HTTPOptions) (*HTTPServer, error) {
	address := httpOptions.Address
	automaticPort := address == ""
	if address == "" {
		persistedPort, err := readPersistedPort(options.StateDirectory)
		if err != nil {
			return nil, err
		}
		if persistedPort != 0 {
			address = net.JoinHostPort("127.0.0.1", strconv.Itoa(persistedPort))
		} else {
			address = DefaultAddress
		}
	}
	if !LoopbackAddress(address) {
		if !httpOptions.AllowRemote {
			return nil, fmt.Errorf("%w: %s; the responses carry names, paths and source metadata, and there is no authentication in front of them",
				ErrAddressNotLoopback, address)
		}
		if httpOptions.OnWarning != nil {
			httpOptions.OnWarning(fmt.Sprintf(
				"serving the graph on %s, which is not loopback: names, paths and source metadata leave this host", address))
		}
	}

	if err := os.MkdirAll(options.StateDirectory, 0o700); err != nil {
		return nil, fmt.Errorf("create the state directory: %w", err)
	}
	token, err := loadOrCreateToken(options.StateDirectory, httpOptions.OnWarning)
	if err != nil {
		return nil, err
	}

	var listener net.Listener
	bind := func() error {
		return withPrivateUmask(func() error {
			bound, err := net.Listen("tcp", address)
			if err != nil {
				return fmt.Errorf("listen on %s: %w", address, err)
			}
			listener = bound
			return nil
		})
	}
	err = bind()
	if err != nil && automaticPort && errors.Is(err, syscall.EADDRINUSE) {
		// The historical port is a preference, not an identity. Bind port zero
		// only after the preferred port is unavailable, then persist the result
		// so the URL remains stable for every later supervisor restart.
		address = "127.0.0.1:0"
		err = bind()
	}
	if err != nil {
		return nil, err
	}
	if automaticPort {
		_, port, err := net.SplitHostPort(listener.Addr().String())
		if err != nil {
			_ = listener.Close()
			return nil, fmt.Errorf("read the bound HTTP port: %w", err)
		}
		if err := writePrivateFile(PortPath(options.StateDirectory), []byte(port+"\n")); err != nil {
			_ = listener.Close()
			return nil, fmt.Errorf("persist %s: %w", PortName, err)
		}
	}

	socket, socketErr := SocketPath(options.StateDirectory)
	if socketErr != nil {
		socket = ""
	}
	endpoint := Endpoint{
		URL:    (&url.URL{Scheme: "http", Host: listener.Addr().String(), Path: MCPPath}).String(),
		Token:  token,
		Socket: socket,
		PID:    os.Getpid(),
	}
	if err := writeEndpoint(options.StateDirectory, endpoint); err != nil {
		_ = listener.Close()
		return nil, err
	}

	served := &HTTPServer{
		listener: listener,
		endpoint: endpoint,
		path:     EndpointPath(options.StateDirectory),
	}
	served.server = &http.Server{
		Handler:           mcpHandler(options, token),
		ReadHeaderTimeout: 10 * time.Second,
	}
	return served, nil
}

// writeEndpoint publishes the endpoint privately and atomically.
func writeEndpoint(directory string, endpoint Endpoint) error {
	encoded, err := json.MarshalIndent(endpoint, "", "  ")
	if err != nil {
		return fmt.Errorf("encode the endpoint: %w", err)
	}
	return writePrivateFile(EndpointPath(directory), append(encoded, '\n'))
}

// writePrivateFile writes a secret where only its owner can read it.
//
// Atomically, because a partial file is worse than no file: a reader would take
// a truncated token for the real one and get 401s it cannot explain. And with
// the mode set on creation rather than after, for the reason the socket taught
// -- a chmod after the fact is a window, and on a bind mount it is an EINVAL.
func writePrivateFile(path string, content []byte) error {
	temporary := path + ".tmp"
	if err := os.WriteFile(temporary, content, 0o600); err != nil {
		return fmt.Errorf("write %s: %w", temporary, err)
	}
	// WriteFile honours the umask on an existing file's mode, so a file left by
	// an earlier run keeps whatever it had. Fix it before it is renamed into
	// place, not after: after is the window.
	if err := os.Chmod(temporary, 0o600); err != nil {
		_ = os.Remove(temporary)
		return fmt.Errorf("restrict %s: %w", temporary, err)
	}
	// The mode above is the whole answer on Unix and none of it where the
	// platform keeps an ACL instead. Narrowing the temporary rather than the
	// published name keeps the property this function is named for: the file
	// is private before it has the name a reader looks for, never after.
	if err := narrowSocket(temporary); err != nil {
		_ = os.Remove(temporary)
		return fmt.Errorf("restrict %s: %w", temporary, err)
	}
	if err := os.Rename(temporary, path); err != nil {
		_ = os.Remove(temporary)
		return fmt.Errorf("publish %s: %w", path, err)
	}
	return nil
}

// Serve answers until ctx is cancelled, then removes the published endpoint.
func (served *HTTPServer) Serve(ctx context.Context) error {
	done := make(chan struct{})
	defer close(done)
	go func() {
		select {
		case <-ctx.Done():
			// A bounded shutdown, so a hanging stream cannot keep the daemon
			// alive after its context ended.
			shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			_ = served.server.Shutdown(shutdownCtx)
		case <-done:
		}
	}()
	err := served.server.Serve(served.listener)
	// The endpoint names a token that no longer authorises anything, so leaving
	// it would send the next reader to a dead port with a dead secret.
	_ = os.Remove(served.path)
	if errors.Is(err, http.ErrServerClosed) {
		return nil
	}
	return err
}

// mcpHandler is the MCP surface behind the token and the origin check.
//
// The two guards defend different things and are therefore separate: the token
// keeps another local process out, and the origin check keeps a web page from
// making the user's own browser ask on its behalf.
func mcpHandler(options Options, token string) http.Handler {
	if options.IndexJobs == nil && options.Indexer != nil {
		// The SDK callback builds one server per HTTP session. Keep operation
		// state outside it so reconnecting does not lose an in-flight index.
		options.IndexJobs = kivmcp.NewIndexJobs(options.Indexer)
	}
	// A server per session, for the same reason the socket half builds one: the
	// tool surface is decided when a server is built, and a daemon outlives
	// generations.
	// The SDK enables net/http's CSRF protection by default, and it refuses a
	// cross-origin request -- which a page served from another loopback port is,
	// by definition. The viewer is exactly that page, and the ports it can sit
	// on -- the packaged bundle's, a dev server's -- are not knowable from here,
	// so there is no trusted-origin list to write.
	//
	// requireLocalOrigin below is the policy instead, and on the axis that
	// matters it is the stricter of the two: it refuses every remote origin
	// while allowing any loopback one. TestHTTPRefusesARemoteOrigin holds it.
	// The bypass is scoped to the one path this handler serves.
	crossOrigin := http.NewCrossOriginProtection()
	crossOrigin.AddInsecureBypassPattern(MCPPath)
	handler := sdkmcp.NewStreamableHTTPHandler(func(*http.Request) *sdkmcp.Server {
		return kivmcp.NewServerWithMetricsAndSnapshotStoreAndIndexerOptions(
			options.Registry, options.SnapshotStore, options.Indexer,
			kivmcp.ServerOptions{IndexJobs: options.IndexJobs})
	}, &sdkmcp.StreamableHTTPOptions{CrossOriginProtection: crossOrigin})

	// The comparison is constant-time, and no test in this package proves it:
	// only the timing differs, and a unit test cannot observe that reliably.
	// It is here because the alternative leaks the token one byte at a time to
	// a caller that can measure, which is any process on this machine.
	verifier := func(_ context.Context, presented string, _ *http.Request) (*sdkauth.TokenInfo, error) {
		if subtle.ConstantTimeCompare([]byte(presented), []byte(token)) != 1 {
			return nil, sdkauth.ErrInvalidToken
		}
		// No expiry: the token lives exactly as long as the process that minted
		// it, and a client that outlives the daemon gets a closed port.
		return &sdkauth.TokenInfo{Expiration: time.Now().Add(time.Hour)}, nil
	}

	mux := http.NewServeMux()
	mux.Handle(MCPPath, sdkauth.RequireBearerToken(verifier, nil)(handler))
	return requireLocalOrigin(mux)
}

// requireLocalOrigin refuses a request whose Origin is a remote page.
//
// A request with no Origin is not from a page and passes. This is what the spec
// asks of a local HTTP server, and it is not redundant with the token: a page
// cannot read the token, but a browser it drives would send cookies and could
// otherwise probe the port.
func requireLocalOrigin(next http.Handler) http.Handler {
	return http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		origin := request.Header.Get("Origin")
		if origin != "" && !localOrigin(origin) {
			http.Error(writer, "origin not allowed", http.StatusForbidden)
			return
		}
		next.ServeHTTP(writer, request)
	})
}

func localOrigin(origin string) bool {
	parsed, err := url.Parse(origin)
	if err != nil {
		return false
	}
	host := parsed.Hostname()
	if host == "localhost" {
		return true
	}
	address := net.ParseIP(host)
	return address != nil && address.IsLoopback()
}

// BearerHeader is the header value a client sends. It exists so a caller does
// not assemble the scheme by hand and get the spacing wrong.
func BearerHeader(token string) string {
	return "Bearer " + strings.TrimSpace(token)
}
