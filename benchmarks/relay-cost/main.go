// Command relay-cost measures the three numbers ADR 0084 gated itself on before
// the relay it describes may be built.
//
// ADR 0069 made the daemon the default and measured why, but its tables are the
// daemon's case and not the relay's. The difference is the whole question. A
// relay is the same binary with the same runtime, so it pays a process floor
// too, and against an idle `serve` -- which ADR 0067 already made cheap, from
// `33.9 MB` per client to `9.8`-`10.7` -- a floor of `8 MB` would save four and
// buy nothing. Against a client that answers on a real workspace it competes
// with something two orders of magnitude larger. Nobody had measured either
// end on today's corpus, so nobody could tell those two worlds apart.
//
// Three arms, at each client count:
//
//   - serve: one `kivgraph serve` process per client, over stdio. The
//     arrangement every `.mcpb` installation runs today.
//   - relay: one daemon, plus one relay process per client holding stdio on one
//     side and Streamable HTTP on the other. The proposal.
//   - daemon: one daemon and N direct HTTP sessions, no per-client process.
//     Not shippable for the installations that carry the volume -- the `.mcpb`
//     manifest has no field for a url -- but it is the cheapest a client could
//     ever be, so it says how much of the relay's cost is the relay itself.
//
// What it publishes is the per-client slope, not any total: an arrangement that
// saved at two clients and not at eight would look like a win in any single row.
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
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/Luqueee/kivgraph/internal/daemon"
	"github.com/Luqueee/kivgraph/internal/mcpworkload"
	"github.com/Luqueee/kivgraph/internal/procstat"
	"github.com/Luqueee/kivgraph/internal/rebuild"
)

const (
	benchmarkName    = "relay-cost"
	defaultDirectory = "benchmarks/relay-cost"
	defaultClients   = "1,2,4,8"
	defaultCalls     = 8
	callTimeout      = 60 * time.Second
	// probeCount is how many real symbols the workload draws from, matching
	// daemon-cost so the two sets of figures can be read together.
	probeCount = 64
	// socketWait bounds how long a daemon is given to bind. A readiness wait
	// and not a sleep: a fixed sleep either wastes the difference or races.
	socketWait = 30 * time.Second
	// defaultThreshold is what the saving has to clear per client for commit 2
	// of LUQUE-2233 to exist. One megabyte: below that the relay trades three
	// commits of work for a rounding error on the load that dominates.
	defaultThreshold = 1 << 20
)

const (
	armServe  = "serve"
	armRelay  = "relay"
	armDaemon = "daemon"
)

// armNames fixes the order the arms appear in, in the file and in the digest.
var armNames = []string{armServe, armRelay, armDaemon}

type config struct {
	Server        string
	Relay         string
	ConfigPath    string
	GenerationDir string
	StateDir      string
	Clients       []int
	Calls         int
	Warmup        int
	Seed          int64
	Directory     string
	Threshold     float64
}

func main() {
	var cfg config
	flag.StringVar(&cfg.Server, "server", "kivgraph", "kivgraph binary under test")
	flag.StringVar(&cfg.Relay, "relay", "", "relay prototype binary (benchmarks/relay-cost/prototype)")
	flag.StringVar(&cfg.ConfigPath, "config", "", "configuration file the servers load")
	flag.StringVar(&cfg.GenerationDir, "generation-dir", "", "published generation directory holding snapshot.kvsnap")
	flag.StringVar(&cfg.StateDir, "state-dir", "", "state directory the daemon puts its socket and endpoint in")
	clientList := flag.String("clients", defaultClients, "comma-separated client counts to measure")
	flag.IntVar(&cfg.Calls, "calls", defaultCalls, "tool calls per arm, split across the clients; 0 for an idle run")
	flag.IntVar(&cfg.Warmup, "warmup", 0, "tool calls per arm to discard before measuring")
	flag.Int64Var(&cfg.Seed, "seed", mcpworkload.DefaultSeed, "workload seed")
	flag.StringVar(&cfg.Directory, "output", defaultDirectory, "directory for results.json")
	flag.Float64Var(&cfg.Threshold, "threshold-bytes", defaultThreshold,
		"bytes per client the relay must save against a serve for the gate to pass")
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
			return nil, fmt.Errorf("client count %q: %w", trimmed, err)
		}
		if parsed < 1 {
			return nil, fmt.Errorf("client count %d must be at least 1", parsed)
		}
		counts = append(counts, parsed)
	}
	if len(counts) == 0 {
		return nil, errors.New("no client counts given")
	}
	return counts, nil
}

func run(ctx context.Context, cfg config) error {
	if strings.TrimSpace(cfg.Relay) == "" {
		return errors.New("-relay is required: it is the prototype whose floor this benchmark exists to measure")
	}
	if strings.TrimSpace(cfg.GenerationDir) == "" {
		return errors.New("-generation-dir is required: every arm must be shown to read the same generation")
	}
	if strings.TrimSpace(cfg.StateDir) == "" {
		return errors.New("-state-dir is required: it is where the daemon publishes the endpoint the relay reads")
	}
	if cfg.Calls > 0 {
		for _, clients := range cfg.Clients {
			if cfg.Calls < clients {
				return fmt.Errorf("calls (%d) must be at least clients (%d), or 0 for an idle run", cfg.Calls, clients)
			}
		}
	}
	snapshotPath := filepath.Join(cfg.GenerationDir, rebuild.PublishedSnapshotFileName)
	info, err := os.Stat(snapshotPath)
	if err != nil {
		return fmt.Errorf("the generation carries no published snapshot: %w", err)
	}
	if _, err := daemon.SocketPath(cfg.StateDir); err != nil {
		return err
	}

	out := results{
		Benchmark:     benchmarkName,
		Date:          time.Now().UTC().Format("2006-01-02"),
		SchemaVersion: "relay-cost-v1",
		Commit:        currentCommit(),
		Command:       strings.Join(os.Args, " "),
		Calls:         cfg.Calls,
		Warmup:        cfg.Warmup,
		Seed:          cfg.Seed,
		Environment:   observeEnvironment(cfg.Server),
		Snapshot:      snapshotFile{Path: snapshotPath, Bytes: info.Size()},
	}

	for _, clients := range cfg.Clients {
		measured := point{Clients: clients}
		for _, name := range armNames {
			one, err := measureArm(ctx, cfg, name, clients)
			if err != nil {
				return fmt.Errorf("%d clients, %s arm: %w", clients, name, err)
			}
			measured.Arms = append(measured.Arms, one)
		}
		if err := checkGenerations(measured); err != nil {
			return err
		}
		if err := checkIdle(cfg.Calls, measured); err != nil {
			return err
		}
		serve, _ := armByName(measured, armServe)
		relay, _ := armByName(measured, armRelay)
		served, _ := armByName(measured, armDaemon)
		measured.RelayVSServ = compare(relay, serve)
		measured.RelayTax = compare(relay, served)
		measured.Waits = waitSummary{
			ServeConnectMS:  serve.NewClientConnectMS,
			RelayConnectMS:  relay.NewClientConnectMS,
			DaemonConnectMS: served.NewClientConnectMS,
			ServeAnswerMS:   serve.NewClientMS,
			RelayAnswerMS:   relay.NewClientMS,
			DaemonAnswerMS:  served.NewClientMS,
		}
		out.SnapshotID = serve.SnapshotID
		out.Points = append(out.Points, measured)
	}

	out.Slopes = slopesOf(out)
	out.Verdict = verdictOf(out.Slopes, cfg.Threshold)
	out.Limitations = limitations(out, cfg)
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

// checkGenerations refuses a point whose arms served different graphs. The
// environment given to one of them would resolve to another state directory,
// and the three columns would be three corpora rather than three arrangements.
func checkGenerations(measured point) error {
	var first *arm
	for index := range measured.Arms {
		one := measured.Arms[index]
		if first == nil {
			first = &measured.Arms[index]
			continue
		}
		if one.SnapshotID != first.SnapshotID {
			return fmt.Errorf("the %s and %s arms served generations %d and %d",
				first.Name, one.Name, first.SnapshotID, one.SnapshotID)
		}
	}
	return nil
}

// checkIdle refuses to publish an idle run that answered something. Such a run
// is not idle: some path asked a question the load did not order, and the probes
// inside the process starters are the ones that would.
//
// Those probes only run against a live server, so no unit test reaches them --
// deleting one breaks nothing on a laptop. This turns that silence into a failed
// run instead of a file whose name is the one thing about it that is wrong.
func checkIdle(calls int, measured point) error {
	if calls != 0 {
		return nil
	}
	for _, one := range measured.Arms {
		if len(one.FirstAnswersMS) > 0 || one.Latency.Calls > 0 || one.NewClientMS != nil {
			return fmt.Errorf(
				"%d clients, %s arm: an idle run answered %d calls and timed %d first answers; a probe survived on the no-call path",
				measured.Clients, one.Name, one.Latency.Calls, len(one.FirstAnswersMS))
		}
	}
	return nil
}

func measureArm(ctx context.Context, cfg config, name string, clients int) (arm, error) {
	switch name {
	case armServe:
		return measureStdio(ctx, cfg, armServe, clients)
	case armRelay:
		return measureStdio(ctx, cfg, armRelay, clients)
	case armDaemon:
		return measureDaemon(ctx, cfg, clients)
	}
	return arm{}, fmt.Errorf("unknown arm %q", name)
}

// measureStdio runs the two arms that put a process in front of every client:
// `serve`, which is that process answering, and the relay, which is that process
// forwarding. They differ only in what gets spawned and in whether a daemon is
// running behind it, so measuring them apart would have been two copies of the
// same function disagreeing later.
func measureStdio(ctx context.Context, cfg config, name string, clients int) (arm, error) {
	var behind *daemonProcess
	if name == armRelay {
		started, err := startDaemon(ctx, cfg)
		if err != nil {
			return arm{}, err
		}
		behind = started
		defer behind.stop()
	}

	live := make([]*stdioProcess, 0, clients)
	defer func() {
		for _, one := range live {
			one.stop()
		}
	}()
	for index := range clients {
		started, err := startStdio(ctx, cfg, name)
		if err != nil {
			return arm{}, fmt.Errorf("start client %d: %w", index+1, err)
		}
		live = append(live, started)
	}

	sessions := make([]*sdkmcp.ClientSession, 0, clients)
	firstAnswers := make([]float64, 0, clients)
	for _, one := range live {
		sessions = append(sessions, one.session)
		if one.waited.firstAnswerMS != nil {
			firstAnswers = append(firstAnswers, *one.waited.firstAnswerMS)
		}
	}

	measured, err := driveAndSample(ctx, cfg, sessions, func() []int {
		pids := make([]int, 0, len(live))
		for _, one := range live {
			pids = append(pids, one.pid())
		}
		return pids
	}, behind.pidOrZero())
	if err != nil {
		return arm{}, err
	}
	measured.Name = name
	measured.FirstAnswersMS = firstAnswers

	// What one more client waits for. Started after the sample, so it never
	// counts towards the arm's bytes.
	extra, err := startStdio(ctx, cfg, name)
	if err != nil {
		return arm{}, fmt.Errorf("start the extra client: %w", err)
	}
	measured.NewClientMS = extra.waited.firstAnswerMS
	measured.NewClientConnectMS = extra.waited.connectMS
	extra.stop()
	return measured, nil
}

// measureDaemon is one process with N sessions over its HTTP door and no
// per-client process at all.
func measureDaemon(ctx context.Context, cfg config, clients int) (arm, error) {
	served, err := startDaemon(ctx, cfg)
	if err != nil {
		return arm{}, err
	}
	defer served.stop()

	sessions := make([]*sdkmcp.ClientSession, 0, clients)
	firstAnswers := make([]float64, 0, clients)
	for index := range clients {
		session, waited, err := served.connect(ctx, cfg.Calls > 0)
		if err != nil {
			return arm{}, fmt.Errorf("connect client %d: %w", index+1, err)
		}
		sessions = append(sessions, session)
		if waited.firstAnswerMS != nil {
			firstAnswers = append(firstAnswers, *waited.firstAnswerMS)
		}
	}

	measured, err := driveAndSample(ctx, cfg, sessions, func() []int { return nil }, served.pid())
	if err != nil {
		return arm{}, err
	}
	measured.Name = armDaemon
	measured.FirstAnswersMS = firstAnswers

	_, waited, err := served.connect(ctx, cfg.Calls > 0)
	if err != nil {
		return arm{}, fmt.Errorf("connect the extra client: %w", err)
	}
	measured.NewClientMS = waited.firstAnswerMS
	measured.NewClientConnectMS = waited.connectMS
	return measured, nil
}

// driveAndSample runs the shared workload over the given sessions and samples
// the per-client processes and, when there is one, the server behind them.
//
// Sampling is after the workload, because a server that has answered is the one
// whose cost matters: pages it never touched cost nothing. The generation guard
// runs after the sample too -- `graph_status` is a call, and a server that
// answered one is not a server nobody asked anything.
func driveAndSample(
	ctx context.Context,
	cfg config,
	sessions []*sdkmcp.ClientSession,
	clientPIDs func() []int,
	serverPID int,
) (arm, error) {
	if len(sessions) == 0 {
		return arm{}, errors.New("no sessions to drive")
	}
	want, parseErr := strconv.ParseUint(filepath.Base(filepath.Clean(cfg.GenerationDir)), 10, 64)
	if parseErr != nil {
		return arm{}, fmt.Errorf("-generation-dir must be named after its generation number: %w", parseErr)
	}

	var latencies []int64
	var callErrors int
	if cfg.Calls > 0 {
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
		latencies, callErrors, err = driveAll(ctx, sessions, workload.Requests)
		if err != nil {
			return arm{}, err
		}
	}

	clientSamples := make([]processSample, 0, 8)
	for index, pid := range clientPIDs() {
		clientSamples = append(clientSamples, sampleOf(index+1, pid, "client"))
	}
	all := append([]processSample(nil), clientSamples...)
	var server *processSample
	if serverPID != 0 {
		observed := sampleOf(0, serverPID, "daemon")
		server = &observed
		all = append(all, observed)
	}

	status, err := readStatus(ctx, sessions[0])
	if err != nil {
		return arm{}, err
	}
	if status.SnapshotID != want {
		return arm{}, fmt.Errorf(
			"the servers serve snapshot %d but -generation-dir names generation %d: "+
				"the environment given to the server resolves to another state directory "+
				"(a configuration written by `init` stores `~` paths, so HOME decides)",
			status.SnapshotID, want)
	}
	return arm{
		SnapshotID:   status.SnapshotID,
		Symbols:      status.Symbols,
		Sessions:     len(sessions),
		Clients:      clientSamples,
		Server:       server,
		ClientTotals: totalsOf(clientSamples),
		Totals:       totalsOf(all),
		Latency:      latencyOf(latencies),
		CallErrors:   callErrors,
	}, nil
}

func sampleOf(index, pid int, role string) processSample {
	observed := procstat.Observe(pid)
	return processSample{
		Index:            index,
		PID:              pid,
		Role:             role,
		ResidentBytes:    observed.Resident,
		ProportionalByte: observed.Proportional,
		SharedCleanByte:  observed.SharedClean,
		PrivateDirtyByte: observed.PrivateDirty,
		PeakBytes:        observed.Peak,
	}
}

// safeBuffer collects a process's stderr behind a mutex.
//
// `exec.Cmd` copies a child's stderr on a goroutine of its own, and every
// error path here reads the buffer back to quote what the process said. A
// plain bytes.Buffer written and read in that arrangement is a race, and the
// moment it would bite is the one where an arm is already failing -- which is
// the least useful moment for the harness to start misbehaving too.
type safeBuffer struct {
	mu     sync.Mutex
	buffer bytes.Buffer
}

func (safe *safeBuffer) Write(data []byte) (int, error) {
	safe.mu.Lock()
	defer safe.mu.Unlock()
	return safe.buffer.Write(data)
}

func (safe *safeBuffer) String() string {
	safe.mu.Lock()
	defer safe.mu.Unlock()
	return safe.buffer.String()
}

// clientWait is what one client waited for. The connection is measured under
// every load; the first answer exists only when the run asks for one, and a zero
// there would read as an instant answer rather than as no answer at all.
type clientWait struct {
	connectMS     float64
	firstAnswerMS *float64
}

type stdioProcess struct {
	command *exec.Cmd
	session *sdkmcp.ClientSession
	cancel  func()
	waited  clientWait
}

func (live *stdioProcess) pid() int {
	if live == nil || live.command == nil || live.command.Process == nil {
		return 0
	}
	return live.command.Process.Pid
}

func (live *stdioProcess) stop() {
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

// startStdio launches one per-client process -- a `serve` or a relay -- and times
// what a client actually waits for: the process, the handshake and, when this
// run asks anything, the first answered call.
//
// The probe is skipped under an idle load rather than kept "because it is only
// one call". One call is the entire load being measured there.
func startStdio(ctx context.Context, cfg config, name string) (*stdioProcess, error) {
	var command *exec.Cmd
	if name == armRelay {
		command = exec.Command(cfg.Relay, "-state-dir", cfg.StateDir)
	} else {
		arguments := []string{"serve"}
		if cfg.ConfigPath != "" {
			arguments = append(arguments, "--config", cfg.ConfigPath)
		}
		command = exec.Command(cfg.Server, arguments...)
	}
	stderr := &safeBuffer{}
	command.Stderr = stderr
	transport := &sdkmcp.CommandTransport{Command: command, TerminateDuration: 5 * time.Second}
	client := sdkmcp.NewClient(&sdkmcp.Implementation{Name: benchmarkName, Version: "1.0.0"}, nil)
	callContext, cancel := context.WithCancel(ctx)

	started := time.Now()
	session, err := client.Connect(callContext, transport, nil)
	if err != nil {
		cancel()
		return nil, fmt.Errorf("connect: %w (it said: %s)", err, clip(strings.TrimSpace(stderr.String()), 400))
	}
	live := &stdioProcess{command: command, session: session, cancel: cancel}
	live.waited.connectMS = float64(time.Since(started).Microseconds()) / 1000
	if cfg.Calls == 0 {
		return live, nil
	}
	if _, err := callTool(ctx, session, "graph_status", map[string]any{}); err != nil {
		live.stop()
		return nil, fmt.Errorf("%w (it said: %s)", err, clip(strings.TrimSpace(stderr.String()), 400))
	}
	answered := float64(time.Since(started).Microseconds()) / 1000
	live.waited.firstAnswerMS = &answered
	return live, nil
}

type daemonProcess struct {
	command  *exec.Cmd
	stderr   *safeBuffer
	socket   string
	sessions []*sdkmcp.ClientSession
	endpoint daemon.Endpoint
}

func (live *daemonProcess) pid() int {
	if live == nil || live.command == nil || live.command.Process == nil {
		return 0
	}
	return live.command.Process.Pid
}

// pidOrZero answers for an arm that has no daemon behind it. A nil receiver is
// the honest shape for the serve arm: there is no server there, rather than one
// whose pid happens to be unknown.
func (live *daemonProcess) pidOrZero() int {
	if live == nil {
		return 0
	}
	return live.pid()
}

func (live *daemonProcess) stop() {
	if live == nil {
		return
	}
	for _, session := range live.sessions {
		_ = session.Close()
	}
	if live.command != nil && live.command.Process != nil {
		_ = live.command.Process.Signal(syscall.SIGTERM)
		_ = live.command.Wait()
	}
}

// startDaemon launches one daemon and waits for its socket to accept and its
// endpoint file to appear, which are the only honest readiness signals: the
// process exists before it has bound, and the relay reads the endpoint file.
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
	stderr := &safeBuffer{}
	command.Stderr = stderr
	if err := command.Start(); err != nil {
		return nil, fmt.Errorf("start daemon: %w", err)
	}
	live := &daemonProcess{command: command, stderr: stderr, socket: socket}

	deadline := time.Now().Add(socketWait)
	for {
		connection, dialErr := net.Dial("unix", socket)
		if dialErr == nil {
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
	// Waited for, not inferred from the socket. The daemon publishes HTTP
	// before it binds, so reaching the socket implies this file exists -- but a
	// harness that relied on the ordering would break silently the day it
	// changed, and the relay cannot start without it.
	for {
		endpoint, readErr := daemon.ReadEndpoint(cfg.StateDir)
		if readErr == nil {
			live.endpoint = endpoint
			return live, nil
		}
		if time.Now().After(deadline) || ctx.Err() != nil {
			live.stop()
			return nil, fmt.Errorf("the daemon published no endpoint within %s: %w (it said: %s)",
				socketWait, readErr, clip(strings.TrimSpace(stderr.String()), 400))
		}
		time.Sleep(20 * time.Millisecond)
	}
}

// connect opens one HTTP session and times the connection plus, when this run
// asks anything, the first answered call.
func (live *daemonProcess) connect(ctx context.Context, probe bool) (*sdkmcp.ClientSession, clientWait, error) {
	client := sdkmcp.NewClient(&sdkmcp.Implementation{Name: benchmarkName, Version: "1.0.0"}, nil)
	transport := &sdkmcp.StreamableClientTransport{
		Endpoint: live.endpoint.URL,
		HTTPClient: &http.Client{
			Transport: bearerRoundTripper{token: live.endpoint.Token, next: http.DefaultTransport},
		},
	}
	started := time.Now()
	session, err := client.Connect(ctx, transport, nil)
	if err != nil {
		return nil, clientWait{}, fmt.Errorf("connect %s: %w (daemon said: %s)",
			live.endpoint.URL, err, clip(strings.TrimSpace(live.stderr.String()), 400))
	}
	waited := clientWait{connectMS: float64(time.Since(started).Microseconds()) / 1000}
	if probe {
		if _, err := callTool(ctx, session, "graph_status", map[string]any{}); err != nil {
			_ = session.Close()
			return nil, clientWait{}, fmt.Errorf("%w (daemon said: %s)", err, clip(strings.TrimSpace(live.stderr.String()), 400))
		}
		answered := float64(time.Since(started).Microseconds()) / 1000
		waited.firstAnswerMS = &answered
	}
	live.sessions = append(live.sessions, session)
	return session, waited, nil
}

// bearerRoundTripper attaches the token the daemon published to every request,
// because the transport reconnects on its own and a token sent only on the first
// request would fail the second.
type bearerRoundTripper struct {
	token string
	next  http.RoundTripper
}

func (attach bearerRoundTripper) RoundTrip(request *http.Request) (*http.Response, error) {
	cloned := request.Clone(request.Context())
	cloned.Header.Set("Authorization", daemon.BearerHeader(attach.token))
	return attach.next.RoundTrip(cloned)
}
