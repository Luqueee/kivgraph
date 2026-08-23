// Command daemon-cost measures what one process serving N clients costs against
// N processes serving one each.
//
// It exists because the saving was never observed. `benchmarks/load-cost-resident`
// measured what a `serve` keeps -- `71,2 MB` of private dirty pages, flat in the
// number of clients -- and `kivgraph daemon` was written on the arithmetic that N
// of those become one. Arithmetic is not a measurement, and two things could make
// it wrong:
//
//   - The snapshot is already shared. It is the same mapped file in every server
//     and those pages are clean, so the bytes at stake are only the private ones.
//     A reader who expects the file's size to disappear is measuring the wrong
//     thing.
//   - A daemon's private half is not constant. It builds an MCP server per
//     accepted session -- eleven tool registrations, a decoder, buffers -- so its
//     cost grows with the number of clients. The question is not whether it
//     saves, it is the slope: an arm that saves at two clients and not at eight
//     would not be worth the command.
//
// Both arms therefore read the same published generation from its file, and the
// sweep is what answers the question. A single client count cannot, so the
// one-client point is measured rather than skipped: it is where a daemon's own
// overhead would be visible with nothing to amortise it over.
//
// This comment predicted that a daemon must be *worse* there -- same load, plus a
// session. Measured, it is neither: the proportion at one client is 0,966 to
// 1,015 over three runs, a megabyte of noise. The per-session server does not
// show up against the 66 MB the load costs. See report.md.
package main

import (
	"bytes"
	"context"
	"errors"
	"flag"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"syscall"
	"time"

	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/Luqueee/kivgraph/internal/daemon"
	kivmcp "github.com/Luqueee/kivgraph/internal/mcp"
	"github.com/Luqueee/kivgraph/internal/mcpworkload"
	"github.com/Luqueee/kivgraph/internal/procstat"
	"github.com/Luqueee/kivgraph/internal/rebuild"
)

const (
	benchmarkName    = "daemon-cost"
	defaultDirectory = "benchmarks/daemon-cost"
	defaultClients   = "1,2,4,8"
	defaultCalls     = 2000
	defaultWarmup    = 4000
	callTimeout      = 60 * time.Second
	// probeCount is how many real symbols the workload draws from. The
	// workload's distribution is what makes the latency comparable between
	// arms, so the probes have to come from the snapshot under test and not
	// from a synthetic corpus.
	probeCount = 64
	// socketWait bounds how long a daemon is given to bind. It is a readiness
	// wait and not a sleep: a fixed sleep either wastes the difference or races.
	socketWait = 20 * time.Second
)

type config struct {
	Server        string
	ConfigPath    string
	GenerationDir string
	StateDir      string
	Clients       []int
	Calls         int
	Warmup        int
	Seed          int64
	Directory     string
	// Transport is which of the daemon's two doors the clients use.
	//
	// It exists because the saving was measured over a unix socket and no
	// editor can be configured for one. If HTTP costs materially more, the
	// reachable saving is not the measured saving, and the number that gets
	// quoted has to be this one.
	Transport string
}

const (
	transportSocket = "socket"
	transportHTTP   = "http"
)

func main() {
	var cfg config
	flag.StringVar(&cfg.Server, "server", "kivgraph", "kivgraph binary under test")
	flag.StringVar(&cfg.ConfigPath, "config", "", "configuration file the servers load")
	flag.StringVar(&cfg.GenerationDir, "generation-dir", "", "published generation directory holding snapshot.kvsnap")
	flag.StringVar(&cfg.StateDir, "state-dir", "", "state directory the daemon puts its socket in")
	clientList := flag.String("clients", defaultClients, "comma-separated client counts to measure")
	flag.IntVar(&cfg.Calls, "calls", defaultCalls, "tool calls per arm, split across the clients")
	flag.IntVar(&cfg.Warmup, "warmup", defaultWarmup, "tool calls per arm to discard before measuring")
	flag.Int64Var(&cfg.Seed, "seed", mcpworkload.DefaultSeed, "workload seed")
	flag.StringVar(&cfg.Directory, "output", defaultDirectory, "directory for results.json and report.md")
	flag.StringVar(&cfg.Transport, "transport", transportSocket,
		"which daemon door the clients use: socket or http")
	flag.Parse()

	parsed, err := parseClientCounts(*clientList)
	if err != nil {
		fmt.Fprintf(os.Stderr, "%s: %v\n", benchmarkName, err)
		os.Exit(1)
	}
	cfg.Clients = parsed

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if err := run(ctx, cfg); err != nil {
		fmt.Fprintf(os.Stderr, "%s: %v\n", benchmarkName, err)
		os.Exit(1)
	}
}

// parseClientCounts reads the sweep. A malformed entry fails rather than being
// skipped: a run that quietly measured fewer counts than it was asked for would
// report a slope fitted to a curve with a hole in it.
func parseClientCounts(value string) ([]int, error) {
	fields := strings.Split(value, ",")
	counts := make([]int, 0, len(fields))
	for _, field := range fields {
		trimmed := strings.TrimSpace(field)
		if trimmed == "" {
			continue
		}
		parsed, err := strconv.Atoi(trimmed)
		if err != nil {
			return nil, fmt.Errorf("-clients %q: %w", trimmed, err)
		}
		if parsed < 1 {
			return nil, fmt.Errorf("-clients %q: a count below one measures nothing", trimmed)
		}
		if !slices.Contains(counts, parsed) {
			counts = append(counts, parsed)
		}
	}
	slices.Sort(counts)
	if len(counts) == 0 {
		return nil, errors.New("-clients named no counts")
	}
	return counts, nil
}

func run(ctx context.Context, cfg config) error {
	if strings.TrimSpace(cfg.GenerationDir) == "" {
		return errors.New("-generation-dir is required: both arms must be shown to read the same generation")
	}
	if strings.TrimSpace(cfg.StateDir) == "" {
		return errors.New("-state-dir is required: it is where the daemon's socket goes, and it is the key that separates daemons")
	}
	if cfg.Transport != transportSocket && cfg.Transport != transportHTTP {
		return fmt.Errorf("-transport %q: want %q or %q", cfg.Transport, transportSocket, transportHTTP)
	}
	for _, clients := range cfg.Clients {
		if cfg.Calls < clients {
			return fmt.Errorf("calls (%d) must be at least clients (%d)", cfg.Calls, clients)
		}
	}
	snapshotPath := filepath.Join(cfg.GenerationDir, rebuild.PublishedSnapshotFileName)
	info, err := os.Stat(snapshotPath)
	if err != nil {
		return fmt.Errorf("the generation carries no published snapshot: %w", err)
	}
	// Failing here rather than at the first sample: a socket path the kernel
	// would truncate is the one failure that would otherwise look like a
	// daemon that never started.
	if _, err := daemon.SocketPath(cfg.StateDir); err != nil {
		return err
	}

	out := results{
		Benchmark: benchmarkName,
		Date:      time.Now().UTC().Format("2006-01-02"),
		// v2 adds the transport to the run identity. A v1 file's digest was
		// computed without it, so the two cannot be compared by digest: v1 named
		// an experiment that could have been either door.
		SchemaVersion: "daemon-cost-v2",
		// Read before the run rather than at write time, so a missing commit
		// reaches the limitations instead of only the JSON field.
		Commit:      currentCommit(),
		Calls:       cfg.Calls,
		Warmup:      cfg.Warmup,
		Seed:        cfg.Seed,
		Transport:   cfg.Transport,
		Environment: observeEnvironment(cfg.Server),
		Snapshot:    snapshotFile{Path: snapshotPath, Bytes: info.Size()},
	}

	for _, clients := range cfg.Clients {
		processes, err := measureProcesses(ctx, cfg, clients)
		if err != nil {
			return fmt.Errorf("%d clients, processes arm: %w", clients, err)
		}
		served, err := measureDaemon(ctx, cfg, clients)
		if err != nil {
			return fmt.Errorf("%d clients, daemon arm: %w", clients, err)
		}
		if processes.SnapshotID != served.SnapshotID {
			return fmt.Errorf(
				"the arms served different generations (%d and %d): the environment given to one of them resolves to another state directory",
				processes.SnapshotID, served.SnapshotID)
		}
		out.SnapshotID = processes.SnapshotID
		measured := point{Clients: clients, Arms: []arm{processes, served}}
		measured.Comparison = compare(processes, served)
		out.Points = append(out.Points, measured)
	}

	out.Slopes = slopesOf(out)
	out.Limitations = limitations(out)
	digest, err := computeDigest(out)
	if err != nil {
		return err
	}
	out.Digest = digest

	if err := writeResults(cfg.Directory, out); err != nil {
		return err
	}
	printSummary(out)
	return nil
}

// measureProcesses is the arrangement the daemon replaces: one server process
// per client, each mapping the same file.
func measureProcesses(ctx context.Context, cfg config, clients int) (arm, error) {
	live := make([]*serverProcess, 0, clients)
	defer func() {
		for _, one := range live {
			one.stop()
		}
	}()
	for index := range clients {
		started, err := startServer(ctx, cfg)
		if err != nil {
			return arm{}, fmt.Errorf("start server %d: %w", index+1, err)
		}
		live = append(live, started)
	}

	sessions := make([]*sdkmcp.ClientSession, 0, clients)
	firstAnswers := make([]float64, 0, clients)
	for _, one := range live {
		sessions = append(sessions, one.session)
		firstAnswers = append(firstAnswers, one.firstAnswerMS)
	}

	measured, err := driveAndSample(ctx, cfg, sessions, func() []int {
		pids := make([]int, 0, len(live))
		for _, one := range live {
			pids = append(pids, one.pid())
		}
		return pids
	})
	if err != nil {
		return arm{}, err
	}
	measured.Name = "processes"
	measured.FirstAnswersMS = firstAnswers

	// What a new client waits for in this arm: another whole process.
	extra, err := startServer(ctx, cfg)
	if err != nil {
		return arm{}, fmt.Errorf("start the extra client's server: %w", err)
	}
	measured.NewClientMS = extra.firstAnswerMS
	extra.stop()
	return measured, nil
}

// measureDaemon is one process with N sessions over its socket.
func measureDaemon(ctx context.Context, cfg config, clients int) (arm, error) {
	served, err := startDaemon(ctx, cfg)
	if err != nil {
		return arm{}, err
	}
	defer served.stop()

	sessions := make([]*sdkmcp.ClientSession, 0, clients)
	firstAnswers := make([]float64, 0, clients)
	for index := range clients {
		session, elapsed, err := served.connect(ctx)
		if err != nil {
			return arm{}, fmt.Errorf("connect client %d: %w", index+1, err)
		}
		sessions = append(sessions, session)
		firstAnswers = append(firstAnswers, elapsed)
	}

	measured, err := driveAndSample(ctx, cfg, sessions, func() []int { return []int{served.pid()} })
	if err != nil {
		return arm{}, err
	}
	measured.Name = "daemon"
	measured.FirstAnswersMS = firstAnswers

	// What a new client waits for here: a connection to a process that has
	// already mapped the file. This is the number a second editor window sees,
	// and it is the one place the daemon should win outright.
	_, elapsed, err := served.connect(ctx)
	if err != nil {
		return arm{}, fmt.Errorf("connect the extra client: %w", err)
	}
	measured.NewClientMS = elapsed
	return measured, nil
}

// driveAndSample runs the shared workload over the given sessions and samples
// whatever processes the caller names.
//
// Sampling is after the workload because a server that has answered is the one
// whose cost matters: pages it never touched cost nothing. The peak is read from
// the same sample, and it is the high-water mark over the process's whole life,
// so it includes the load -- which is the one place the retired allocations of
// LUQUE-2216 to LUQUE-2220 could still show up.
func driveAndSample(
	ctx context.Context,
	cfg config,
	sessions []*sdkmcp.ClientSession,
	pids func() []int,
) (arm, error) {
	if len(sessions) == 0 {
		return arm{}, errors.New("no sessions to drive")
	}
	status, err := readStatus(ctx, sessions[0])
	if err != nil {
		return arm{}, err
	}
	want, parseErr := strconv.ParseUint(filepath.Base(filepath.Clean(cfg.GenerationDir)), 10, 64)
	if parseErr != nil {
		return arm{}, fmt.Errorf("-generation-dir must be named after its generation number: %w", parseErr)
	}
	if status.SnapshotID != want {
		return arm{}, fmt.Errorf(
			"the servers serve snapshot %d but -generation-dir names generation %d: "+
				"the environment given to the server resolves to another state directory "+
				"(a configuration written by `init` stores `~` paths, so HOME decides)",
			status.SnapshotID, want)
	}

	probes, err := harvestProbes(ctx, sessions[0])
	if err != nil {
		return arm{}, err
	}
	workload, err := mcpworkload.Generate(ctx, mcpworkload.Config{
		Calls:  cfg.Calls,
		Seed:   cfg.Seed,
		Corpus: mcpworkload.Corpus{Probes: probes},
	})
	if err != nil {
		return arm{}, fmt.Errorf("generate workload: %w", err)
	}
	if cfg.Warmup > 0 {
		warmup, err := mcpworkload.Generate(ctx, mcpworkload.Config{
			Calls:  cfg.Warmup,
			Seed:   cfg.Seed + 1,
			Corpus: mcpworkload.Corpus{Probes: probes},
		})
		if err != nil {
			return arm{}, fmt.Errorf("generate warmup: %w", err)
		}
		if _, _, err := driveAll(ctx, sessions, warmup.Requests); err != nil {
			return arm{}, fmt.Errorf("warmup: %w", err)
		}
	}

	latencies, callErrors, err := driveAll(ctx, sessions, workload.Requests)
	if err != nil {
		return arm{}, err
	}

	samples := make([]processSample, 0, 4)
	for index, pid := range pids() {
		sample := procstat.Observe(pid)
		samples = append(samples, processSample{
			Index:            index + 1,
			PID:              pid,
			ResidentBytes:    sample.Resident,
			ProportionalByte: sample.Proportional,
			SharedCleanByte:  sample.SharedClean,
			PrivateDirtyByte: sample.PrivateDirty,
			PeakBytes:        sample.Peak,
		})
	}
	return arm{
		SnapshotID: status.SnapshotID,
		Symbols:    status.Symbols,
		Sessions:   len(sessions),
		Processes:  samples,
		Totals:     totalsOf(samples),
		Latency:    latencyOf(latencies),
		CallErrors: callErrors,
	}, nil
}

type serverProcess struct {
	command       *exec.Cmd
	session       *sdkmcp.ClientSession
	cancel        func()
	firstAnswerMS float64
}

func (live *serverProcess) pid() int {
	if live == nil || live.command == nil || live.command.Process == nil {
		return 0
	}
	return live.command.Process.Pid
}

func (live *serverProcess) stop() {
	if live == nil {
		return
	}
	if live.session != nil {
		_ = live.session.Close()
	}
	if live.cancel != nil {
		live.cancel()
	}
}

// startServer launches one `serve` and times what a client actually waits for:
// the process, the connection and the first answered call.
func startServer(ctx context.Context, cfg config) (*serverProcess, error) {
	arguments := []string{"serve"}
	if cfg.ConfigPath != "" {
		arguments = append(arguments, "--config", cfg.ConfigPath)
	}
	command := exec.Command(cfg.Server, arguments...)
	stderr := &bytes.Buffer{}
	command.Stderr = stderr
	transport := &sdkmcp.CommandTransport{Command: command, TerminateDuration: 5 * time.Second}
	client := sdkmcp.NewClient(&sdkmcp.Implementation{Name: benchmarkName, Version: "1.0.0"}, nil)
	callContext, cancel := context.WithCancel(ctx)

	started := time.Now()
	session, err := client.Connect(callContext, transport, nil)
	if err != nil {
		cancel()
		return nil, fmt.Errorf("connect: %w (server said: %s)", err, clip(strings.TrimSpace(stderr.String()), 400))
	}
	live := &serverProcess{command: command, session: session, cancel: cancel}
	if _, err := callTool(ctx, session, "graph_status", map[string]any{}); err != nil {
		live.stop()
		return nil, fmt.Errorf("%w (server said: %s)", err, clip(strings.TrimSpace(stderr.String()), 400))
	}
	live.firstAnswerMS = float64(time.Since(started).Microseconds()) / 1000
	return live, nil
}

type daemonProcess struct {
	command  *exec.Cmd
	stderr   *bytes.Buffer
	socket   string
	sessions []*sdkmcp.ClientSession
	streams  []net.Conn
	// transport and endpoint are how a client reaches this daemon. The socket
	// is always bound -- it is the readiness signal for both arms -- and the
	// endpoint is only read when the clients are going to use it.
	transport string
	endpoint  daemon.Endpoint
}

func (live *daemonProcess) pid() int {
	if live == nil || live.command == nil || live.command.Process == nil {
		return 0
	}
	return live.command.Process.Pid
}

func (live *daemonProcess) stop() {
	if live == nil {
		return
	}
	for _, session := range live.sessions {
		_ = session.Close()
	}
	for _, stream := range live.streams {
		_ = stream.Close()
	}
	if live.command != nil && live.command.Process != nil {
		_ = live.command.Process.Signal(syscall.SIGTERM)
		_ = live.command.Wait()
	}
}

// startDaemon launches one daemon and waits for its socket to accept, which is
// the only honest readiness signal: the process exists before it has bound.
func startDaemon(ctx context.Context, cfg config) (*daemonProcess, error) {
	socket, err := daemon.SocketPath(cfg.StateDir)
	if err != nil {
		return nil, err
	}
	arguments := []string{"daemon"}
	if cfg.ConfigPath != "" {
		arguments = append(arguments, "--config", cfg.ConfigPath)
	}
	command := exec.Command(cfg.Server, arguments...)
	stderr := &bytes.Buffer{}
	command.Stderr = stderr
	if err := command.Start(); err != nil {
		return nil, fmt.Errorf("start daemon: %w", err)
	}
	live := &daemonProcess{command: command, stderr: stderr, socket: socket, transport: cfg.Transport}

	deadline := time.Now().Add(socketWait)
	for {
		connection, err := net.Dial("unix", socket)
		if err == nil {
			_ = connection.Close()
			break
		}
		if time.Now().After(deadline) || ctx.Err() != nil {
			live.stop()
			return nil, fmt.Errorf("the daemon did not accept on %s within %s (it said: %s)",
				socket, socketWait, clip(strings.TrimSpace(stderr.String()), 400))
		}
		time.Sleep(20 * time.Millisecond)
	}
	if live.transport != transportHTTP {
		return live, nil
	}
	// The socket accepted, so both listeners are up: the daemon publishes the
	// endpoint before it serves either. Reading it here rather than per client
	// keeps the endpoint out of the timed section.
	endpoint, err := daemon.ReadEndpoint(cfg.StateDir)
	if err != nil {
		live.stop()
		return nil, fmt.Errorf("read the published endpoint: %w (it said: %s)",
			err, clip(strings.TrimSpace(stderr.String()), 400))
	}
	live.endpoint = endpoint
	return live, nil
}

// connect opens one session and times the connection plus the first answered
// call, so the two arms measure the same wait.
func (live *daemonProcess) connect(ctx context.Context) (*sdkmcp.ClientSession, float64, error) {
	started := time.Now()
	session, stream, err := live.dial(ctx)
	if err != nil {
		return nil, 0, err
	}
	if _, err := callTool(ctx, session, "graph_status", map[string]any{}); err != nil {
		_ = session.Close()
		if stream != nil {
			_ = stream.Close()
		}
		return nil, 0, fmt.Errorf("%w (daemon said: %s)", err, clip(strings.TrimSpace(live.stderr.String()), 400))
	}
	live.sessions = append(live.sessions, session)
	if stream != nil {
		live.streams = append(live.streams, stream)
	}
	return session, float64(time.Since(started).Microseconds()) / 1000, nil
}

// dial opens the session over whichever door this arm is measuring.
//
// The HTTP transport owns its own connections, so it returns no stream to close:
// a nil there is the honest answer rather than a placeholder the caller would
// close and wonder about.
func (live *daemonProcess) dial(ctx context.Context) (*sdkmcp.ClientSession, net.Conn, error) {
	client := sdkmcp.NewClient(&sdkmcp.Implementation{Name: benchmarkName, Version: "1.0.0"}, nil)
	if live.transport == transportHTTP {
		transport := &sdkmcp.StreamableClientTransport{
			Endpoint: live.endpoint.URL,
			HTTPClient: &http.Client{
				Transport: bearerRoundTripper{
					token: live.endpoint.Token,
					next:  http.DefaultTransport,
				},
			},
		}
		session, err := client.Connect(ctx, transport, nil)
		if err != nil {
			return nil, nil, fmt.Errorf("connect %s: %w (daemon said: %s)",
				live.endpoint.URL, err, clip(strings.TrimSpace(live.stderr.String()), 400))
		}
		return session, nil, nil
	}
	stream, err := net.Dial("unix", live.socket)
	if err != nil {
		return nil, nil, fmt.Errorf("dial %s: %w", live.socket, err)
	}
	session, err := client.Connect(ctx, &kivmcp.StreamTransport{Stream: stream}, nil)
	if err != nil {
		_ = stream.Close()
		return nil, nil, fmt.Errorf("connect: %w (daemon said: %s)", err, clip(strings.TrimSpace(live.stderr.String()), 400))
	}
	return session, stream, nil
}

// bearerRoundTripper attaches the token the daemon published. Every request
// carries it, because the transport reconnects on its own and a token sent only
// on the first request would fail the second.
type bearerRoundTripper struct {
	token string
	next  http.RoundTripper
}

func (attach bearerRoundTripper) RoundTrip(request *http.Request) (*http.Response, error) {
	cloned := request.Clone(request.Context())
	cloned.Header.Set("Authorization", daemon.BearerHeader(attach.token))
	return attach.next.RoundTrip(cloned)
}

func callTool(ctx context.Context, session *sdkmcp.ClientSession, name string, arguments map[string]any) (string, error) {
	callCtx, cancel := context.WithTimeout(ctx, callTimeout)
	defer cancel()
	response, err := session.CallTool(callCtx, &sdkmcp.CallToolParams{Name: name, Arguments: arguments})
	if err != nil {
		return "", fmt.Errorf("call %s: %w", name, err)
	}
	if response.IsError {
		return "", fmt.Errorf("call %s returned an error result: %s", name, firstText(response))
	}
	return firstText(response), nil
}

func firstText(response *sdkmcp.CallToolResult) string {
	for _, content := range response.Content {
		if text, ok := content.(*sdkmcp.TextContent); ok {
			return text.Text
		}
	}
	return ""
}

func clip(value string, limit int) string {
	if len(value) <= limit {
		return value
	}
	return value[:limit] + "…"
}
