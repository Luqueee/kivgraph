package telemetry

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"sync"
	"sync/atomic"
	"testing"
)

// collector answers like the endpoint does -- 204 to everything -- and keeps
// what it received.
func collector(t *testing.T) (*httptest.Server, *[]ping, *atomic.Int64) {
	t.Helper()
	var mu sync.Mutex
	received := make([]ping, 0, 8)
	var count atomic.Int64
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		var body ping
		_ = json.NewDecoder(request.Body).Decode(&body)
		mu.Lock()
		received = append(received, body)
		mu.Unlock()
		count.Add(1)
		writer.WriteHeader(http.StatusNoContent)
	}))
	t.Cleanup(server.Close)
	return server, &received, &count
}

// releasedBinary builds the layout a release archive unpacks into and answers
// the path of the executable inside it. Nothing reports from anywhere else.
func releasedBinary(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	binary := filepath.Join(root, "bin", "kivgraph")
	if err := os.MkdirAll(filepath.Dir(binary), 0o755); err != nil {
		t.Fatal(err)
	}
	for path, content := range map[string]string{
		binary:                               "",
		filepath.Join(root, "manifest.json"): `{"release":"0.9.1"}`,
	} {
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return binary
}

func options(t *testing.T, endpoint string) Options {
	t.Helper()
	return Options{
		StateDirectory: t.TempDir(),
		Version:        "0.9.1",
		Transport:      "stdio",
		Executable:     releasedBinary(t),
		Notice:         &bytes.Buffer{},
		Endpoint:       endpoint,
		Getenv:         func(string) string { return "" },
	}
}

func TestAnnounceSendsOnePingWithTheDeclaredFields(t *testing.T) {
	server, received, _ := collector(t)
	opts := options(t, server.URL)

	if !Announce(context.Background(), opts) {
		t.Fatal("Announce() = false, want a ping on the first run of a version")
	}
	if len(*received) != 1 {
		t.Fatalf("received %d pings, want 1", len(*received))
	}
	got := (*received)[0]
	want := ping{
		Emitter:   "binary",
		Version:   "0.9.1",
		Platform:  runtime.GOOS + "-" + runtime.GOARCH,
		Channel:   "archive",
		Transport: "stdio",
	}
	if got != want {
		t.Fatalf("received %+v, want %+v", got, want)
	}
}

// The gate the task named. `serve` runs the MCP surface over stdio, so a byte
// written here corrupts a session, and nothing else in the process would
// report it: the client would see a protocol error with no cause.
func TestAnnounceWritesNothingToStdout(t *testing.T) {
	server, _, _ := collector(t)
	opts := options(t, server.URL)
	opts.Notice = os.Stderr

	read, write, err := os.Pipe()
	if err != nil {
		t.Fatalf("os.Pipe() error = %v", err)
	}
	original := os.Stdout
	os.Stdout = write
	Announce(context.Background(), opts)
	os.Stdout = original
	if err := write.Close(); err != nil {
		t.Fatalf("close the pipe: %v", err)
	}

	var captured bytes.Buffer
	if _, err := captured.ReadFrom(read); err != nil {
		t.Fatalf("read the pipe: %v", err)
	}
	if captured.Len() != 0 {
		t.Fatalf("stdout received %q, and one byte there corrupts an MCP session", captured.String())
	}
}

// stdio starts bursts. Reading the marker and then writing it would let every
// process in one find it absent, and one install would become eight pings.
func TestAnnounceReportsOnceUnderABurst(t *testing.T) {
	server, _, count := collector(t)
	opts := options(t, server.URL)

	const starts = 16
	var sent atomic.Int64
	var wait sync.WaitGroup
	wait.Add(starts)
	for range starts {
		go func() {
			defer wait.Done()
			if Announce(context.Background(), opts) {
				sent.Add(1)
			}
		}()
	}
	wait.Wait()

	if sent.Load() != 1 {
		t.Fatalf("%d of %d concurrent starts reported, want exactly 1", sent.Load(), starts)
	}
	if count.Load() != 1 {
		t.Fatalf("the endpoint received %d pings, want 1", count.Load())
	}
}

func TestAnnounceReportsAgainForANewVersion(t *testing.T) {
	server, _, count := collector(t)
	opts := options(t, server.URL)

	Announce(context.Background(), opts)
	Announce(context.Background(), opts)
	opts.Version = "0.9.2"
	Announce(context.Background(), opts)

	if count.Load() != 2 {
		t.Fatalf("the endpoint received %d pings, want 2: one per version", count.Load())
	}
}

func TestAnnounceLeavesNothingBehindWhenItIsTurnedOff(t *testing.T) {
	server, _, count := collector(t)
	opts := options(t, server.URL)
	opts.Getenv = func(name string) string {
		if name == DisableEnv {
			return "0"
		}
		return ""
	}

	if Announce(context.Background(), opts) {
		t.Fatal("Announce() = true with telemetry disabled")
	}
	if count.Load() != 0 {
		t.Fatalf("the endpoint received %d pings with telemetry disabled", count.Load())
	}
	// No marker either. A machine that opted out and later opts back in should
	// report the version it is running, not skip it because a run it never
	// reported claimed the name.
	if entries, err := os.ReadDir(opts.StateDirectory); err != nil || len(entries) != 0 {
		t.Fatalf("the state directory holds %v (err %v), want nothing", entries, err)
	}
	if buffer, ok := opts.Notice.(*bytes.Buffer); ok && buffer.Len() != 0 {
		t.Fatalf("the notice printed %q with telemetry disabled", buffer.String())
	}
}

func TestNoticeNamesTheVariableThatStopsIt(t *testing.T) {
	server, _, _ := collector(t)
	opts := options(t, server.URL)
	Announce(context.Background(), opts)

	printed := opts.Notice.(*bytes.Buffer).String()
	for _, want := range []string{DisableEnv + "=0", "0.9.1", "https://kivgraph.dev/telemetry/"} {
		if !bytes.Contains([]byte(printed), []byte(want)) {
			t.Fatalf("the notice %q does not mention %q", printed, want)
		}
	}
}

// An endpoint that is down must cost nothing, and must not be retried: a
// machine with no network would otherwise report on every start forever.
func TestAnnounceSurvivesAnEndpointThatIsNotThere(t *testing.T) {
	server, _, _ := collector(t)
	closed := server.URL
	server.Close()

	opts := options(t, closed)
	if Announce(context.Background(), opts) {
		t.Fatal("Announce() = true against a closed endpoint")
	}
	marker := filepath.Join(opts.StateDirectory, "first-run", opts.Version)
	if _, err := os.Stat(marker); err != nil {
		t.Fatalf("the marker was not kept after a failed send: %v", err)
	}
}

func TestAnnounceDoesNothingWithoutAStateDirectory(t *testing.T) {
	server, _, count := collector(t)
	opts := options(t, server.URL)
	opts.StateDirectory = ""

	if sent := Announce(context.Background(), opts); sent || count.Load() != 0 {
		t.Fatalf("Announce() = %v with %d pings received, want false and 0: "+
			"there is no state directory to mark, so nothing can be reported once",
			sent, count.Load())
	}
}

func TestChannelOfReadsTheLayoutAroundTheExecutable(t *testing.T) {
	extension := t.TempDir()
	bundle := filepath.Join(extension, "server")
	binary := filepath.Join(bundle, "bin", "kivgraph")
	if err := os.MkdirAll(filepath.Dir(binary), 0o755); err != nil {
		t.Fatal(err)
	}
	write := func(path, content string) {
		t.Helper()
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write(binary, "")
	write(filepath.Join(bundle, "manifest.json"), `{"release":"0.9.1"}`)

	// A released binary with no extension around it.
	if got := channelOf(binary); got != "archive" {
		t.Fatalf("channelOf(a bundle) = %q, want archive", got)
	}

	// The same bundle inside an unpacked MCP extension.
	write(filepath.Join(extension, "manifest.json"),
		`{"manifest_version":"0.3","server":{"type":"binary","mcp_config":{"command":"x"}}}`)
	if got := channelOf(binary); got != "mcpb" {
		t.Fatalf("channelOf(an extension) = %q, want mcpb", got)
	}

	// A `manifest.json` that is not an extension's must not be read as one:
	// an archive extracted next to an unrelated file is still an archive.
	write(filepath.Join(extension, "manifest.json"), `{"name":"something else"}`)
	if got := channelOf(binary); got != "archive" {
		t.Fatalf("channelOf(an unrelated manifest) = %q, want archive", got)
	}

	// A `go build` output, with nothing around it, is not a release and has no
	// channel to report.
	loose := filepath.Join(t.TempDir(), "kivgraph")
	write(loose, "")
	if got := channelOf(loose); got != "" {
		t.Fatalf("channelOf(a bare binary) = %q, want no channel", got)
	}
	if got := channelOf(""); got != "" {
		t.Fatalf("channelOf(no path) = %q, want no channel", got)
	}
}

// The one that keeps this repository's own CI out of its own numbers: five
// platforms build and run the binary on every push, and every one of those
// runs is a `go build` output with nothing around it.
func TestAnnounceStaysSilentForABinaryThatIsNotARelease(t *testing.T) {
	server, _, count := collector(t)
	opts := options(t, server.URL)
	opts.Executable = filepath.Join(t.TempDir(), "kivgraph")

	if sent := Announce(context.Background(), opts); sent || count.Load() != 0 {
		t.Fatalf("Announce(%q) = %v with %d pings received, want false and 0: "+
			"a binary outside a release layout has no channel to report",
			opts.Executable, sent, count.Load())
	}
	// And it did not claim the marker: installing a release afterwards has to
	// be able to report the version it is running.
	if entries, err := os.ReadDir(opts.StateDirectory); err != nil || len(entries) != 0 {
		t.Fatalf("the state directory holds %v (err %v), want nothing", entries, err)
	}
}
