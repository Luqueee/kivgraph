package main

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"os"
	"os/exec"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"

	kivgraphmcp "github.com/Luqueee/kivgraph/internal/mcp"
)

const (
	callsPerScenario          = 10_000
	warmupCallsPerClient      = 100
	growthProbeBatches        = 3
	growthProbeCallsPerClient = 100
	benchmarkCommand          = "go run ./benchmarks/mcp-empty"
)

var tools = []string{"graph_status", "list_repositories"}
var clientCounts = []int{1, 4, 16, 32}

type Results struct {
	Benchmark      string           `json:"benchmark"`
	Command        string           `json:"command"`
	Commit         string           `json:"commit"`
	GeneratedAt    time.Time        `json:"generated_at"`
	CallsPerTool   int              `json:"calls_per_tool"`
	ClientCounts   []int            `json:"client_counts"`
	Scenarios      []ScenarioResult `json:"scenarios"`
	GateAssessment GateAssessment   `json:"gate_assessment"`
}

type ScenarioResult struct {
	Tool                    string  `json:"tool"`
	Clients                 int     `json:"clients"`
	Calls                   int     `json:"calls"`
	DurationMS              float64 `json:"duration_ms"`
	P50MS                   float64 `json:"p50_ms"`
	P95MS                   float64 `json:"p95_ms"`
	P99MS                   float64 `json:"p99_ms"`
	RoundTripP50MS          float64 `json:"round_trip_p50_ms"`
	RoundTripP95MS          float64 `json:"round_trip_p95_ms"`
	RoundTripP99MS          float64 `json:"round_trip_p99_ms"`
	ThroughputPerSecond     float64 `json:"throughput_per_s"`
	AllocationsPerOperation float64 `json:"allocations_per_op"`
	BytesPerOperation       float64 `json:"bytes_per_op"`
	RSSBytes                int64   `json:"rss_bytes"`
	Goroutines              int     `json:"goroutines"`
	Errors                  int     `json:"errors"`
	MemoryGrowthDetected    bool    `json:"memory_growth_detected"`
}

type GateAssessment struct {
	P95WithinTwoMilliseconds bool `json:"p95_within_2ms"`
	ZeroErrors               bool `json:"zero_errors"`
	NoContinuousMemoryGrowth bool `json:"no_continuous_memory_growth"`
	Passed                   bool `json:"passed"`
}

type clientSession struct {
	server *sdkmcp.ServerSession
	client *sdkmcp.ClientSession
}

func main() {
	results, err := run(context.Background())
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	if err := writeOutputs(results); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run(ctx context.Context) (Results, error) {
	results := Results{
		Benchmark:    "mcp-empty",
		Command:      benchmarkCommand,
		Commit:       gitState(),
		GeneratedAt:  time.Now().UTC(),
		CallsPerTool: callsPerScenario,
		ClientCounts: append([]int(nil), clientCounts...),
	}

	for _, tool := range tools {
		for _, clients := range clientCounts {
			scenario, err := benchmarkTool(ctx, tool, clients)
			if err != nil {
				return Results{}, fmt.Errorf("benchmark %s/%d: %w", tool, clients, err)
			}
			results.Scenarios = append(results.Scenarios, scenario)
		}
	}
	results.GateAssessment = assessGate(results.Scenarios)
	return results, nil
}

func benchmarkTool(ctx context.Context, tool string, clientCount int) (ScenarioResult, error) {
	var backendDurationsMu sync.Mutex
	backendDurations := make([]int64, 0, callsPerScenario)
	observer := func(_ string, elapsed time.Duration) {
		backendDurationsMu.Lock()
		backendDurations = append(backendDurations, elapsed.Nanoseconds())
		backendDurationsMu.Unlock()
	}
	sessions, closeSessions, err := newSessions(ctx, clientCount, observer)
	if err != nil {
		return ScenarioResult{}, err
	}
	defer closeSessions()

	if warmupErrors := exercise(ctx, tool, sessions, warmupCallsPerClient); warmupErrors != 0 {
		return ScenarioResult{}, fmt.Errorf("warm-up recorded %d errors", warmupErrors)
	}
	backendDurationsMu.Lock()
	backendDurations = backendDurations[:0]
	backendDurationsMu.Unlock()

	runtime.GC()
	var before, after runtime.MemStats
	runtime.ReadMemStats(&before)
	startRSS := readRSSBytes()
	startGoroutines := runtime.NumGoroutine()
	start := time.Now()

	durations := make([]int64, 0, callsPerScenario)
	var durationsMu sync.Mutex
	var errorsCount atomic.Int64
	var waitGroup sync.WaitGroup

	for clientIndex, session := range sessions {
		calls := callsPerScenario / clientCount
		if clientIndex < callsPerScenario%clientCount {
			calls++
		}
		waitGroup.Add(1)
		go func(session *sdkmcp.ClientSession, calls int) {
			defer waitGroup.Done()
			localDurations := make([]int64, 0, calls)
			localErrors := int64(0)
			for range calls {
				callStart := time.Now()
				result, callErr := session.CallTool(ctx, &sdkmcp.CallToolParams{Name: tool})
				localDurations = append(localDurations, time.Since(callStart).Nanoseconds())
				if callErr != nil || result == nil || result.IsError {
					localErrors++
				}
			}
			durationsMu.Lock()
			durations = append(durations, localDurations...)
			durationsMu.Unlock()
			errorsCount.Add(localErrors)
		}(session.client, calls)
	}
	waitGroup.Wait()
	elapsed := time.Since(start)

	runtime.GC()
	runtime.ReadMemStats(&after)
	endRSS := readRSSBytes()
	backendDurationsMu.Lock()
	measuredBackendDurations := append([]int64(nil), backendDurations...)
	backendDurationsMu.Unlock()
	sort.Slice(measuredBackendDurations, func(i, j int) bool { return measuredBackendDurations[i] < measuredBackendDurations[j] })
	endGoroutines := runtime.NumGoroutine()
	if startGoroutines > endGoroutines {
		endGoroutines = startGoroutines
	}

	growthSamples := []int64{startRSS, endRSS}
	for range growthProbeBatches {
		if probeErrors := exercise(ctx, tool, sessions, growthProbeCallsPerClient); probeErrors != 0 {
			return ScenarioResult{}, fmt.Errorf("growth probe recorded %d errors", probeErrors)
		}
		runtime.GC()
		growthSamples = append(growthSamples, readRSSBytes())
	}

	sort.Slice(durations, func(i, j int) bool { return durations[i] < durations[j] })
	if len(durations) != callsPerScenario {
		return ScenarioResult{}, fmt.Errorf("recorded %d round-trip calls, want %d", len(durations), callsPerScenario)
	}
	if len(measuredBackendDurations) != callsPerScenario {
		return ScenarioResult{}, fmt.Errorf("recorded %d backend calls, want %d", len(measuredBackendDurations), callsPerScenario)
	}

	return ScenarioResult{
		Tool:                    tool,
		Clients:                 clientCount,
		Calls:                   len(durations),
		DurationMS:              elapsed.Seconds() * 1000,
		P50MS:                   percentileMS(measuredBackendDurations, 0.50),
		P95MS:                   percentileMS(measuredBackendDurations, 0.95),
		P99MS:                   percentileMS(measuredBackendDurations, 0.99),
		RoundTripP50MS:          percentileMS(durations, 0.50),
		RoundTripP95MS:          percentileMS(durations, 0.95),
		RoundTripP99MS:          percentileMS(durations, 0.99),
		ThroughputPerSecond:     float64(len(durations)) / elapsed.Seconds(),
		AllocationsPerOperation: float64(after.Mallocs-before.Mallocs) / float64(len(durations)),
		BytesPerOperation:       float64(after.TotalAlloc-before.TotalAlloc) / float64(len(durations)),
		RSSBytes:                maxRSS(growthSamples),
		Goroutines:              endGoroutines,
		Errors:                  int(errorsCount.Load()),
		MemoryGrowthDetected:    continuousGrowth(growthSamples),
	}, nil
}

func exercise(ctx context.Context, tool string, sessions []clientSession, callsPerClient int) int {
	var errorsCount atomic.Int64
	var waitGroup sync.WaitGroup
	for _, session := range sessions {
		waitGroup.Add(1)
		go func(session *sdkmcp.ClientSession) {
			defer waitGroup.Done()
			for range callsPerClient {
				result, err := session.CallTool(ctx, &sdkmcp.CallToolParams{Name: tool})
				if err != nil || result == nil || result.IsError {
					errorsCount.Add(1)
				}
			}
		}(session.client)
	}
	waitGroup.Wait()
	return int(errorsCount.Load())
}

func newSessions(ctx context.Context, count int, observer func(string, time.Duration)) ([]clientSession, func(), error) {
	server := kivgraphmcp.NewServerWithObserver(observer)
	sessions := make([]clientSession, 0, count)
	closeSessions := func() {
		for i := len(sessions) - 1; i >= 0; i-- {
			_ = sessions[i].client.Close()
			_ = sessions[i].server.Close()
		}
	}

	for range count {
		serverTransport, clientTransport := sdkmcp.NewInMemoryTransports()
		serverSession, err := server.Connect(ctx, serverTransport, nil)
		if err != nil {
			closeSessions()
			return nil, func() {}, fmt.Errorf("server.Connect(): %w", err)
		}
		client := sdkmcp.NewClient(&sdkmcp.Implementation{Name: "mcp-empty-benchmark", Version: "0.1.0"}, nil)
		session, err := client.Connect(ctx, clientTransport, nil)
		if err != nil {
			_ = serverSession.Close()
			closeSessions()
			return nil, func() {}, fmt.Errorf("client.Connect(): %w", err)
		}
		sessions = append(sessions, clientSession{server: serverSession, client: session})
	}
	return sessions, closeSessions, nil
}

func percentileMS(values []int64, percentile float64) float64 {
	index := int(math.Ceil(float64(len(values))*percentile)) - 1
	if index < 0 {
		index = 0
	}
	if index >= len(values) {
		index = len(values) - 1
	}
	return float64(values[index]) / float64(time.Millisecond)
}

func continuousGrowth(samples []int64) bool {
	if len(samples) < 3 || samples[0] == 0 {
		return false
	}
	for index := 1; index < len(samples); index++ {
		if samples[index] <= samples[index-1] {
			return false
		}
	}
	return true
}

func maxRSS(samples []int64) int64 {
	var maximum int64
	for _, sample := range samples {
		maximum = max(maximum, sample)
	}
	return maximum
}

func assessGate(scenarios []ScenarioResult) GateAssessment {
	assessment := GateAssessment{
		P95WithinTwoMilliseconds: true,
		ZeroErrors:               true,
		NoContinuousMemoryGrowth: true,
	}
	for _, scenario := range scenarios {
		assessment.P95WithinTwoMilliseconds = assessment.P95WithinTwoMilliseconds && scenario.P95MS <= 2
		assessment.ZeroErrors = assessment.ZeroErrors && scenario.Errors == 0
		assessment.NoContinuousMemoryGrowth = assessment.NoContinuousMemoryGrowth && !scenario.MemoryGrowthDetected
	}
	assessment.Passed = assessment.P95WithinTwoMilliseconds && assessment.ZeroErrors && assessment.NoContinuousMemoryGrowth
	return assessment
}

func writeOutputs(results Results) error {
	data, err := json.MarshalIndent(results, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal results: %w", err)
	}
	if err := os.WriteFile("benchmarks/mcp-empty/results.json", append(data, '\n'), 0o644); err != nil {
		return fmt.Errorf("write results: %w", err)
	}

	var report strings.Builder
	fmt.Fprintf(&report, "# MCP empty benchmark\n\n")
	fmt.Fprintf(&report, "- Command: `%s`\n", results.Command)
	fmt.Fprintf(&report, "- Commit: `%s`\n", results.Commit)
	fmt.Fprintf(&report, "- Calls per tool and scenario: %d\n", results.CallsPerTool)
	fmt.Fprintf(&report, "- Generated at: `%s`\n\n", results.GeneratedAt.Format(time.RFC3339))
	fmt.Fprintln(&report, "## Results")
	fmt.Fprintln(&report)
	fmt.Fprintln(&report, "| Tool | Clients | Backend p50 ms | Backend p95 ms | Backend p99 ms | Round-trip p95 ms | Throughput/s | Allocs/op | Bytes/op | RSS | Goroutines | Errors |")
	fmt.Fprintln(&report, "| --- | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: |")
	for _, scenario := range results.Scenarios {
		fmt.Fprintf(&report, "| `%s` | %d | %.3f | %.3f | %.3f | %.3f | %.1f | %.1f | %.1f | %d | %d | %d |\n",
			scenario.Tool,
			scenario.Clients,
			scenario.P50MS,
			scenario.P95MS,
			scenario.P99MS,
			scenario.RoundTripP95MS,
			scenario.ThroughputPerSecond,
			scenario.AllocationsPerOperation,
			scenario.BytesPerOperation,
			scenario.RSSBytes,
			scenario.Goroutines,
			scenario.Errors,
		)
	}
	fmt.Fprintln(&report)
	fmt.Fprintln(&report, "## Gate `EMPTY_MCP_PERFORMANCE_PASS`")
	fmt.Fprintln(&report)
	fmt.Fprintf(&report, "- p95 ≤ 2 ms: `%t`\n", results.GateAssessment.P95WithinTwoMilliseconds)
	fmt.Fprintf(&report, "- 0 errores: `%t`\n", results.GateAssessment.ZeroErrors)
	fmt.Fprintf(&report, "- Sin crecimiento continuo de memoria detectado: `%t`\n", results.GateAssessment.NoContinuousMemoryGrowth)
	fmt.Fprintf(&report, "- Resultado: `%t`\n", results.GateAssessment.Passed)
	fmt.Fprintln(&report)
	fmt.Fprintln(&report, "Backend p50/p95/p99 measure only the registered handler body through an observer; round-trip p95 includes the in-memory MCP transport and JSON-RPC path. RSS and goroutines correspond to the benchmark process. Repeat the benchmark on target hardware before treating the gate as production evidence.")

	if err := os.WriteFile("benchmarks/mcp-empty/report.md", []byte(report.String()), 0o644); err != nil {
		return fmt.Errorf("write report: %w", err)
	}
	return nil
}

func gitState() string {
	commit := "unknown"
	if output, err := exec.Command("git", "rev-parse", "HEAD").Output(); err == nil {
		commit = strings.TrimSpace(string(output))
	}
	if output, err := exec.Command("git", "status", "--porcelain").Output(); err == nil && len(output) > 0 {
		commit += "-dirty"
	}
	return commit
}

func readRSSBytes() int64 {
	data, err := os.ReadFile("/proc/self/status")
	if err != nil {
		return 0
	}
	for _, line := range strings.Split(string(data), "\n") {
		fields := strings.Fields(line)
		if len(fields) >= 2 && fields[0] == "VmRSS:" {
			kilobytes, err := strconv.ParseInt(fields[1], 10, 64)
			if err == nil {
				return kilobytes * 1024
			}
		}
	}
	return 0
}
