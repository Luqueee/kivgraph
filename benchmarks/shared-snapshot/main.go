// Command shared-snapshot measures what N servers of the same published
// generation cost together, and what they would cost without the file.
//
// The comparison is the whole point. A harness that measured only the mapped arm
// would report a resident size per process and prove nothing: a process counts
// every mapped page it touched as resident, so three servers reading one
// generation each report all of it, and the number goes up while the machine
// gets cheaper. Both arms therefore run the same binary over the same
// generation, and the only difference is whether the snapshot file is there.
//
// The arm without it is not an older build. Hiding the file makes every server
// derive the graph from the canonical store, which is exactly what a server did
// before ADR 0045, and it isolates the one variable.
package main

import (
	"bytes"
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"

	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/Luqueee/kivgraph/internal/mcpworkload"
	"github.com/Luqueee/kivgraph/internal/procstat"
	"github.com/Luqueee/kivgraph/internal/rebuild"
)

const (
	benchmarkName    = "shared-snapshot"
	defaultDirectory = "benchmarks/shared-snapshot"
	defaultClients   = 4
	defaultCalls     = 2_000
	// defaultWarmup is discarded work. A mapped snapshot answers its first
	// queries by faulting pages in, and a heap one never does, so a measured
	// window that includes those faults reports the transient as the cost.
	defaultWarmup = 4_000
	callTimeout   = 60 * time.Second
	// probeCount is how many real symbols the workload draws from. The
	// workload's distribution is what makes the latency comparable between
	// arms, so the probes have to come from the snapshot under test and not
	// from a synthetic corpus.
	probeCount = 64
)

// Gate thresholds. They are the acceptance criteria of LUQUE-2006 and they are
// declared here so a reader sees what the verdict is measured against.
const (
	maximumResidentShare  = 0.40
	maximumPrivateDirty   = 60 << 20
	maximumP99Regression  = 0.05
	gatePassSentinel      = "SHARED_SNAPSHOT_PASS"
	gateEnvironmentSwitch = "KIVGRAPH_BENCH_SLO"
)

const (
	// defaultClientList is the sweep. The gate is decided at one count, but the
	// answer to "what does sharing buy" is the curve: below the gate count the
	// shared part is spread over fewer processes, above it over more.
	defaultClientList  = "2,4,8"
	defaultGateClients = 4
)

// parseClientCounts reads the sweep. A malformed entry fails rather than being
// skipped: a run that quietly measured fewer counts than it was asked for would
// report a curve with a hole in it.
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
		if slices.Contains(counts, parsed) {
			return nil, fmt.Errorf("client count %d is repeated", parsed)
		}
		counts = append(counts, parsed)
	}
	if len(counts) == 0 {
		return nil, errors.New("no client counts given")
	}
	slices.Sort(counts)
	return counts, nil
}

type config struct {
	Server        string
	ConfigPath    string
	GenerationDir string
	// Clients is every server count to measure. The shared part of a mapped
	// snapshot is amortised over the processes holding it, so the ratio moves
	// with the count and one point cannot say what sharing buys.
	Clients     []int
	GateClients int
	Calls       int
	Warmup      int
	Seed        int64
	Directory   string
}

func main() {
	var cfg config
	flag.StringVar(&cfg.Server, "server", "kivgraph", "kivgraph binary under test")
	flag.StringVar(&cfg.ConfigPath, "config", "", "configuration file the servers load")
	flag.StringVar(&cfg.GenerationDir, "generation-dir", "", "published generation directory holding snapshot.kvsnap")
	clientList := flag.String("clients", defaultClientList, "comma-separated server counts to measure")
	flag.IntVar(&cfg.GateClients, "gate-clients", defaultGateClients, "the server count the gate is decided on")
	flag.IntVar(&cfg.Calls, "calls", defaultCalls, "tool calls per arm, split across the servers")
	flag.IntVar(&cfg.Warmup, "warmup", defaultWarmup, "tool calls per arm to discard before measuring, so a mapped page is faulted in before its latency counts")
	flag.Int64Var(&cfg.Seed, "seed", mcpworkload.DefaultSeed, "workload seed")
	flag.StringVar(&cfg.Directory, "output", defaultDirectory, "directory for results.json and report.md")
	flag.Parse()
	parsed, err := parseClientCounts(*clientList)
	if err != nil {
		fmt.Fprintf(os.Stderr, "%s: %v\n", benchmarkName, err)
		os.Exit(1)
	}
	cfg.Clients = parsed

	if err := run(context.Background(), cfg); err != nil {
		fmt.Fprintf(os.Stderr, "%s: %v\n", benchmarkName, err)
		os.Exit(1)
	}
}

func run(ctx context.Context, cfg config) error {
	if len(cfg.Clients) == 0 {
		return errors.New("-clients is required")
	}
	for _, clients := range cfg.Clients {
		if clients < 2 {
			return fmt.Errorf("clients must be at least 2, got %d: one server shares nothing", clients)
		}
		if cfg.Calls < clients {
			return fmt.Errorf("calls (%d) must be at least clients (%d)", cfg.Calls, clients)
		}
	}
	if !slices.Contains(cfg.Clients, cfg.GateClients) {
		return fmt.Errorf("-gate-clients %d is not among the measured counts %v", cfg.GateClients, cfg.Clients)
	}
	if strings.TrimSpace(cfg.GenerationDir) == "" {
		return errors.New("-generation-dir is required: the arms are defined by whether its snapshot file is there")
	}
	snapshotPath := filepath.Join(cfg.GenerationDir, rebuild.PublishedSnapshotFileName)
	info, err := os.Stat(snapshotPath)
	if err != nil {
		return fmt.Errorf("the generation carries no published snapshot to hide: %w", err)
	}

	out := results{
		Benchmark:     benchmarkName,
		Date:          time.Now().UTC().Format("2006-01-02"),
		SchemaVersion: "shared-snapshot-v1",
		GateClients:   cfg.GateClients,
		Calls:         cfg.Calls,
		Warmup:        cfg.Warmup,
		Seed:          cfg.Seed,
		Environment:   observeEnvironment(cfg.Server),
		Snapshot: snapshotFile{
			Path:  snapshotPath,
			Bytes: info.Size(),
		},
		Thresholds: thresholds{
			ResidentShare:  maximumResidentShare,
			PrivateDirty:   maximumPrivateDirty,
			P99Regression:  maximumP99Regression,
			MeasuredOnly:   !procstat.ProportionalSupported(),
			GateSwitchName: gateEnvironmentSwitch,
		},
	}

	for _, clients := range cfg.Clients {
		// The mapped arm first, because it is the one that has to answer which
		// snapshot is being served: the derived arm cannot be asked, its file
		// is not there.
		mapped, err := measureArm(ctx, cfg, clients, "mapped")
		if err != nil {
			return fmt.Errorf("%d clients, mapped arm: %w", clients, err)
		}
		out.SnapshotID = mapped.SnapshotID

		derived, err := withHiddenSnapshot(snapshotPath, func() (arm, error) {
			return measureArm(ctx, cfg, clients, "derived")
		})
		if err != nil {
			return fmt.Errorf("%d clients, derived arm: %w", clients, err)
		}

		measured := point{Clients: clients, Arms: []arm{mapped, derived}}
		measured.Comparison = compare(mapped, derived)
		measured.Checks = checksFor(measured)
		out.Points = append(out.Points, measured)
	}

	out.Limitations = limitations(out)
	out.Gate = decide(out)
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

// withHiddenSnapshot runs body with the published snapshot renamed out of the
// way, and puts it back whatever happens. Losing it would leave the generation
// deriving forever with nothing saying why.
func withHiddenSnapshot(path string, body func() (arm, error)) (arm, error) {
	hidden := path + ".hidden-by-" + benchmarkName
	if err := os.Rename(path, hidden); err != nil {
		return arm{}, fmt.Errorf("hide %s: %w", filepath.Base(path), err)
	}
	defer func() {
		if err := os.Rename(hidden, path); err != nil {
			// Nothing here can recover it, so it has to be loud.
			fmt.Fprintf(os.Stderr,
				"%s: RESTORE FAILED: %v\n  the generation's snapshot is at %s and must be renamed back to %s\n",
				benchmarkName, err, hidden, path)
		}
	}()
	return body()
}

// measureArm starts the servers, drives the workload and samples every process.
func measureArm(ctx context.Context, cfg config, clients int, name string) (arm, error) {
	servers := make([]*server, 0, clients)
	defer func() {
		for _, live := range servers {
			live.stop()
		}
	}()

	for index := range clients {
		live, err := startServer(ctx, cfg)
		if err != nil {
			return arm{}, fmt.Errorf("start server %d: %w", index+1, err)
		}
		servers = append(servers, live)
	}

	status, err := readStatus(ctx, servers[0].session)
	if err != nil {
		return arm{}, err
	}
	// The generation the servers actually serve has to be the one whose file
	// this harness hides, or the two arms measure different graphs and the
	// comparison is meaningless.
	//
	// This is not hypothetical. A configuration written by `init` stores its
	// paths with a literal `~`, expanded against the HOME of whoever runs the
	// server, so passing -config alone does not isolate anything: the first run
	// of this harness pointed at an isolated generation and measured the real
	// installation's, and nothing said so.
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
	loaded, err := readLoadedFromFile(cfg.GenerationDir)
	if err != nil {
		return arm{}, err
	}

	probes, err := harvestProbes(ctx, servers[0].session)
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

	// A warmup whose latencies are thrown away, because the first touch of a
	// mapped page is a fault and the first touch of a heap page is not. Without
	// it the two arms are compared over different amounts of work: at 2000
	// calls the mapped arm's p99 came out 1.42x the derived one's and at 8000
	// calls 1.27x, which measures the length of the run and not the design.
	if cfg.Warmup > 0 {
		warmup, err := mcpworkload.Generate(ctx, mcpworkload.Config{
			Calls:  cfg.Warmup,
			Seed:   cfg.Seed + 1,
			Corpus: mcpworkload.Corpus{Probes: probes},
		})
		if err != nil {
			return arm{}, fmt.Errorf("generate warmup: %w", err)
		}
		if _, _, err := driveAll(ctx, servers, warmup.Requests); err != nil {
			return arm{}, fmt.Errorf("warmup: %w", err)
		}
	}

	latencies, callErrors, err := driveAll(ctx, servers, workload.Requests)
	if err != nil {
		return arm{}, err
	}

	// Sampled after the workload, because a server that has answered is the
	// one whose cost matters: the pages it never touched cost nothing.
	samples := make([]processSample, 0, len(servers))
	for index, live := range servers {
		sample := procstat.Observe(live.pid())
		samples = append(samples, processSample{
			Index:            index + 1,
			PID:              live.pid(),
			ResidentBytes:    sample.Resident,
			ProportionalByte: sample.Proportional,
			SharedCleanByte:  sample.SharedClean,
			PrivateDirtyByte: sample.PrivateDirty,
			PeakBytes:        sample.Peak,
			FirstAnswerMS:    live.firstAnswerMS,
		})
	}

	return arm{
		Name:           name,
		SnapshotID:     status.SnapshotID,
		Symbols:        status.Symbols,
		ServedFromFile: loaded,
		Processes:      samples,
		Totals:         totalsOf(samples),
		Latency:        latencyOf(latencies),
		CallErrors:     callErrors,
	}, nil
}

// readLoadedFromFile answers whether this arm served the file or derived the
// graph, from the generation rather than from a log line. An arm that claimed to
// be derived while reading the file would compare a thing against itself.
func readLoadedFromFile(directory string) (bool, error) {
	_, err := rebuild.InspectPublishedSnapshot(directory)
	switch {
	case err == nil:
		return true, nil
	case errors.Is(err, rebuild.ErrNoPublishedSnapshot):
		return false, nil
	default:
		return false, fmt.Errorf("inspect the published snapshot: %w", err)
	}
}

type server struct {
	command       *exec.Cmd
	session       *sdkmcp.ClientSession
	cancel        func()
	firstAnswerMS float64
}

func (live *server) pid() int {
	if live == nil || live.command == nil || live.command.Process == nil {
		return 0
	}
	return live.command.Process.Pid
}

func (live *server) stop() {
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

// startServer launches one server and times what a client actually waits for:
// the connection plus the first answered call. A server that mapped its
// snapshot in a millisecond and then spent a second on its first query has not
// saved anybody anything.
func startServer(ctx context.Context, cfg config) (*server, error) {
	arguments := []string{"serve"}
	if cfg.ConfigPath != "" {
		arguments = append(arguments, "--config", cfg.ConfigPath)
	}
	command := exec.Command(cfg.Server, arguments...)
	// The server's own diagnostics, kept so a failure names its cause. Writing
	// this to os.NewFile(0, os.DevNull) would alias standard input, which is a
	// different file descriptor with the same name.
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
	live := &server{command: command, session: session, cancel: cancel}
	if _, err := callTool(ctx, session, "graph_status", map[string]any{}); err != nil {
		live.stop()
		return nil, fmt.Errorf("%w (server said: %s)", err, clip(strings.TrimSpace(stderr.String()), 400))
	}
	live.firstAnswerMS = float64(time.Since(started).Microseconds()) / 1000
	return live, nil
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
