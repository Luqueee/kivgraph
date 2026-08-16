package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"
)

type config struct {
	Server      string
	ConfigPath  string
	Calls       int
	Warmup      int
	OutputDir   string
	CallTimeout time.Duration
}

type results struct {
	Benchmark        string      `json:"benchmark"`
	Command          string      `json:"command"`
	Commit           string      `json:"commit"`
	GeneratedAt      time.Time   `json:"generated_at"`
	Environment      environment `json:"environment"`
	Transport        string      `json:"transport"`
	ServerConfig     string      `json:"server_config"`
	ProtocolVersion  string      `json:"protocol_version"`
	ToolCount        int         `json:"tool_count"`
	WarmupCalls      int         `json:"warmup_calls"`
	Calls            int         `json:"calls"`
	Metrics          metrics     `json:"metrics"`
	Errors           int         `json:"errors"`
	ServerExitCode   int         `json:"server_exit_code"`
	ServerStderr     string      `json:"server_stderr,omitempty"`
	CloseError       string      `json:"close_error,omitempty"`
	TransportSLONote string      `json:"transport_slo_note"`
}

type environment struct {
	OS     string `json:"os"`
	Arch   string `json:"arch"`
	CPU    string `json:"cpu"`
	Memory string `json:"memory"`
	Go     string `json:"go"`
}

type metrics struct {
	P50MS        float64 `json:"p50_ms"`
	P95MS        float64 `json:"p95_ms"`
	P99MS        float64 `json:"p99_ms"`
	MinMS        float64 `json:"min_ms"`
	MaxMS        float64 `json:"max_ms"`
	ThroughputPS float64 `json:"throughput_per_s"`
	RSSBytes     int64   `json:"rss_bytes"`
}

type rssSampler struct {
	stop chan struct{}
	done chan struct{}
	max  atomic.Int64
}

func main() {
	cfg := parseConfig()
	result, err := run(context.Background(), cfg)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	if err := writeOutputs(cfg.OutputDir, result); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	fmt.Printf("MCP STDIO benchmark: p50 %.3f ms, p95 %.3f ms, p99 %.3f ms, throughput %.1f/s, errors %d\n",
		result.Metrics.P50MS, result.Metrics.P95MS, result.Metrics.P99MS,
		result.Metrics.ThroughputPS, result.Errors)
}

func parseConfig() config {
	cfg := config{
		Server:      "./kivgraph",
		ConfigPath:  "benchmarks/mcp-stdio/testdata/config.yaml",
		Calls:       10_000,
		Warmup:      100,
		OutputDir:   "benchmarks/mcp-stdio",
		CallTimeout: 20 * time.Second,
	}
	flag.StringVar(&cfg.Server, "server", cfg.Server, "Kivgraph server executable")
	flag.StringVar(&cfg.ConfigPath, "config", cfg.ConfigPath, "server configuration path")
	flag.IntVar(&cfg.Calls, "calls", cfg.Calls, "measured graph_status calls")
	flag.IntVar(&cfg.Warmup, "warmup", cfg.Warmup, "warm-up graph_status calls")
	flag.StringVar(&cfg.OutputDir, "output", cfg.OutputDir, "results output directory")
	flag.DurationVar(&cfg.CallTimeout, "call-timeout", cfg.CallTimeout, "timeout for each client call")
	flag.Parse()
	return cfg
}

func run(ctx context.Context, cfg config) (results, error) {
	if cfg.Calls < 1 {
		return results{}, errors.New("calls must be positive")
	}
	if cfg.Warmup < 0 {
		return results{}, errors.New("warmup must not be negative")
	}
	if cfg.CallTimeout <= 0 {
		return results{}, errors.New("call-timeout must be positive")
	}
	if _, err := os.Stat(cfg.Server); err != nil {
		return results{}, fmt.Errorf("stat server executable %q: %w", cfg.Server, err)
	}
	if _, err := os.Stat(cfg.ConfigPath); err != nil {
		return results{}, fmt.Errorf("stat server config %q: %w", cfg.ConfigPath, err)
	}
	command := fmt.Sprintf("go run ./benchmarks/mcp-stdio --server %s --config %s --calls %d --warmup %d --output %s",
		cfg.Server, cfg.ConfigPath, cfg.Calls, cfg.Warmup, cfg.OutputDir)
	commit, err := currentCommit()
	if err != nil {
		return results{}, err
	}
	result := results{
		Benchmark:        "mcp-stdio",
		Command:          command,
		Commit:           commit,
		GeneratedAt:      time.Now().UTC(),
		Environment:      currentEnvironment(),
		Transport:        "stdio-subprocess",
		ServerConfig:     cfg.ConfigPath,
		WarmupCalls:      cfg.Warmup,
		Calls:            cfg.Calls,
		TransportSLONote: "The SLO document defines backend limits, not a STDIO transport limit; this result is evidence, not a transport gate.",
	}

	serverCommand := exec.Command(cfg.Server, "serve", "--config", cfg.ConfigPath)
	var stderr bytes.Buffer
	serverCommand.Stderr = &stderr
	transport := &sdkmcp.CommandTransport{Command: serverCommand, TerminateDuration: 5 * time.Second}
	client := sdkmcp.NewClient(&sdkmcp.Implementation{Name: "mcp-stdio-benchmark", Version: "1.0.0"}, nil)
	callContext, cancel := context.WithCancel(ctx)
	defer cancel()
	session, err := client.Connect(callContext, transport, nil)
	if err != nil {
		return results{}, fmt.Errorf("connect MCP STDIO client: %w", err)
	}
	closed := false
	defer func() {
		if !closed {
			_ = session.Close()
		}
	}()

	initResult := session.InitializeResult()
	if initResult == nil {
		return results{}, errors.New("MCP initialize returned no result")
	}
	result.ProtocolVersion = initResult.ProtocolVersion
	tools, err := session.ListTools(callContext, nil)
	if err != nil {
		return results{}, fmt.Errorf("list MCP tools: %w", err)
	}
	result.ToolCount = len(tools.Tools)
	if err := assertQueryTools(tools.Tools); err != nil {
		return results{}, err
	}

	sampler := startRSSSampler(serverCommand.Process.Pid)
	for call := 0; call < cfg.Warmup; call++ {
		if err := callGraphStatus(callContext, session, cfg.CallTimeout); err != nil {
			stopRSSSampler(sampler)
			return results{}, fmt.Errorf("warm-up call %d: %w", call, err)
		}
	}

	latencies := make([]float64, 0, cfg.Calls)
	start := time.Now()
	for call := 0; call < cfg.Calls; call++ {
		callStart := time.Now()
		callErr := callGraphStatus(callContext, session, cfg.CallTimeout)
		latencies = append(latencies, float64(time.Since(callStart).Nanoseconds())/1e6)
		if callErr != nil {
			result.Errors++
		}
	}
	elapsed := time.Since(start)
	result.Metrics = metricsFromLatencies(latencies, elapsed, stopRSSSampler(sampler))

	closeErr := session.Close()
	closed = true
	if closeErr != nil {
		result.CloseError = closeErr.Error()
	}
	result.ServerStderr = stderr.String()
	if serverCommand.ProcessState != nil {
		result.ServerExitCode = serverCommand.ProcessState.ExitCode()
	}
	return result, nil
}

// queryTools is the read-only surface this benchmark measures. The count is
// not asserted: the `serve` path also registers the mutating `index_project`,
// and a new tool is not a benchmark failure. A missing one is, because the
// numbers would then describe a different server.
var queryTools = []string{
	"graph_status",
	"list_repositories",
	"find_symbol",
	"get_symbol",
	"find_references",
	"find_cross_repo_consumers",
	"trace_dependencies",
	"get_blast_radius",
	"get_unresolved_references",
}

func assertQueryTools(listed []*sdkmcp.Tool) error {
	present := make(map[string]struct{}, len(listed))
	for _, tool := range listed {
		if tool != nil {
			present[tool.Name] = struct{}{}
		}
	}
	missing := make([]string, 0, len(queryTools))
	for _, name := range queryTools {
		if _, found := present[name]; !found {
			missing = append(missing, name)
		}
	}
	if len(missing) > 0 {
		return fmt.Errorf("MCP server does not expose %s", strings.Join(missing, ", "))
	}
	return nil
}

func callGraphStatus(ctx context.Context, session *sdkmcp.ClientSession, timeout time.Duration) error {
	callCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	response, err := session.CallTool(callCtx, &sdkmcp.CallToolParams{
		Name:      "graph_status",
		Arguments: map[string]any{},
	})
	if err != nil {
		return err
	}
	if response == nil {
		return errors.New("nil graph_status response")
	}
	if response.IsError {
		return errors.New("graph_status returned an MCP error result")
	}
	return nil
}

func metricsFromLatencies(latencies []float64, elapsed time.Duration, rssBytes int64) metrics {
	sort.Float64s(latencies)
	return metrics{
		P50MS:        percentile(latencies, 0.50),
		P95MS:        percentile(latencies, 0.95),
		P99MS:        percentile(latencies, 0.99),
		MinMS:        latencies[0],
		MaxMS:        latencies[len(latencies)-1],
		ThroughputPS: float64(len(latencies)) / elapsed.Seconds(),
		RSSBytes:     rssBytes,
	}
}

func percentile(values []float64, fraction float64) float64 {
	index := int(fraction * float64(len(values)))
	if index >= len(values) {
		index = len(values) - 1
	}
	return values[index]
}

func startRSSSampler(pid int) *rssSampler {
	sampler := &rssSampler{stop: make(chan struct{}), done: make(chan struct{})}
	go func() {
		defer close(sampler.done)
		ticker := time.NewTicker(10 * time.Millisecond)
		defer ticker.Stop()
		for {
			select {
			case <-sampler.stop:
				return
			case <-ticker.C:
				if rss, err := processRSSBytes(pid); err == nil {
					updateMax(&sampler.max, rss)
				}
			}
		}
	}()
	return sampler
}

func stopRSSSampler(sampler *rssSampler) int64 {
	if sampler == nil {
		return 0
	}
	close(sampler.stop)
	<-sampler.done
	return sampler.max.Load()
}

func updateMax(target *atomic.Int64, value int64) {
	for {
		current := target.Load()
		if value <= current || target.CompareAndSwap(current, value) {
			return
		}
	}
}

func processRSSBytes(pid int) (int64, error) {
	data, err := os.ReadFile(filepath.Join("/proc", strconv.Itoa(pid), "status"))
	if err != nil {
		return 0, err
	}
	for _, line := range strings.Split(string(data), "\n") {
		if !strings.HasPrefix(line, "VmRSS:") {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) < 2 {
			return 0, fmt.Errorf("invalid VmRSS line %q", line)
		}
		kilobytes, err := strconv.ParseInt(fields[1], 10, 64)
		if err != nil {
			return 0, fmt.Errorf("parse VmRSS %q: %w", line, err)
		}
		return kilobytes * 1024, nil
	}
	return 0, errors.New("VmRSS not found")
}

func currentCommit() (string, error) {
	output, err := exec.Command("git", "rev-parse", "HEAD").Output()
	if err != nil {
		return "", fmt.Errorf("read git commit: %w", err)
	}
	commit := strings.TrimSpace(string(output))
	if commit == "" {
		return "", errors.New("git rev-parse HEAD returned an empty commit")
	}
	return commit, nil
}

func currentEnvironment() environment {
	return environment{
		OS:     runtime.GOOS,
		Arch:   runtime.GOARCH,
		CPU:    cpuModel(),
		Memory: memoryTotal(),
		Go:     runtime.Version(),
	}
}

func cpuModel() string {
	data, err := os.ReadFile("/proc/cpuinfo")
	if err == nil {
		for _, line := range strings.Split(string(data), "\n") {
			if strings.HasPrefix(line, "model name") {
				if _, value, ok := strings.Cut(line, ":"); ok {
					return strings.TrimSpace(value)
				}
			}
		}
	}
	return runtime.GOARCH
}

func memoryTotal() string {
	data, err := os.ReadFile("/proc/meminfo")
	if err == nil {
		for _, line := range strings.Split(string(data), "\n") {
			if strings.HasPrefix(line, "MemTotal:") {
				return strings.TrimSpace(strings.TrimPrefix(line, "MemTotal:"))
			}
		}
	}
	return "unknown"
}

func writeOutputs(outputDir string, result results) error {
	if err := os.MkdirAll(outputDir, 0o755); err != nil {
		return fmt.Errorf("create output directory: %w", err)
	}
	data, err := json.MarshalIndent(result, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal results: %w", err)
	}
	if err := os.WriteFile(filepath.Join(outputDir, "results.json"), append(data, '\n'), 0o644); err != nil {
		return fmt.Errorf("write results: %w", err)
	}
	report := fmt.Sprintf(`# MCP STDIO transport benchmark

- Command: %s
- Commit: %s
- Generated at: %s
- Environment: %s/%s, %s, %s, Go %s
- Transport: subprocess over newline-delimited JSON on stdin/stdout
- Server config: %s
- Workload: %d warm-up calls and %d measured graph_status calls

## Results

| Metric | Result |
| --- | ---: |
| Protocol version | %s |
| Registered tools | %d |
| p50 round-trip | %.6f ms |
| p95 round-trip | %.6f ms |
| p99 round-trip | %.6f ms |
| Minimum | %.6f ms |
| Maximum | %.6f ms |
| Throughput | %.3f calls/s |
| Errors | %d |
| Server RSS sampled maximum | %d bytes |
| Server exit code | %d |

graph_status was called against an empty published snapshot, so every measured
call was a successful status response. The client completed initialization,
tools/list, 100 warm-ups and the measured workload over the real Kivgraph STDIO
transport. Server logs remained on stderr; no protocol bytes were written there.

## SLO interpretation

docs/performance/slo.md defines limits for backend query handlers and does not
define a transport limit. This artifact is therefore evidence for the STDIO
path, not a new PASS gate. It excludes sockets and network transports, which are
not configured by the current Kivgraph server.
`, result.Command, result.Commit, result.GeneratedAt.UTC().Format(time.RFC3339),
		result.Environment.OS, result.Environment.Arch, result.Environment.CPU, result.Environment.Memory,
		result.Environment.Go, result.ServerConfig, result.WarmupCalls, result.Calls,
		result.ProtocolVersion, result.ToolCount, result.Metrics.P50MS, result.Metrics.P95MS,
		result.Metrics.P99MS, result.Metrics.MinMS, result.Metrics.MaxMS, result.Metrics.ThroughputPS,
		result.Errors, result.Metrics.RSSBytes, result.ServerExitCode)
	if err := os.WriteFile(filepath.Join(outputDir, "report.md"), []byte(report), 0o644); err != nil {
		return fmt.Errorf("write report: %w", err)
	}
	return nil
}
