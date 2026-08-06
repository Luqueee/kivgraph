package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	sdkmcp "github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/Luqueee/ladygraph/internal/facts"
	"github.com/Luqueee/ladygraph/internal/hotsnapshot"
	ladygraphmcp "github.com/Luqueee/ladygraph/internal/mcp"
	"github.com/Luqueee/ladygraph/internal/mcpworkload"
)

const (
	defaultCalls           = mcpworkload.DefaultCallCount
	defaultWarmup          = 100
	defaultSymbols         = 100_000
	defaultEdges           = 1_000_000
	defaultSeed      int64 = mcpworkload.DefaultSeed
	defaultOutputDir       = "benchmarks/mcp-client"
	growthBatches          = 3
	growthCalls            = 100
)

type config struct {
	Calls     int
	Warmup    int
	Symbols   int
	Edges     int
	Seed      int64
	OutputDir string
}

type results struct {
	Benchmark   string            `json:"benchmark"`
	Command     string            `json:"command"`
	Commit      string            `json:"commit"`
	GeneratedAt time.Time         `json:"generated_at"`
	Environment environment       `json:"environment"`
	Dataset     dataset           `json:"dataset"`
	Workload    workloadSummary   `json:"workload"`
	WarmupCalls int               `json:"warmup_calls"`
	Metrics     metrics           `json:"metrics"`
	Operations  []operationResult `json:"operations"`
	SLOChecks   []sloCheck        `json:"slo_checks"`
	SLOPassed   bool              `json:"slo_passed"`
}

type environment struct {
	OS     string `json:"os"`
	Arch   string `json:"arch"`
	CPU    string `json:"cpu"`
	Memory string `json:"memory"`
	Go     string `json:"go"`
}

type dataset struct {
	Name         string `json:"name"`
	Seed         int64  `json:"seed"`
	Repositories int    `json:"repositories"`
	Packages     int    `json:"packages"`
	Files        int    `json:"files"`
	Symbols      int    `json:"symbols"`
	Edges        int    `json:"edges"`
}

type workloadSummary struct {
	SchemaVersion string                        `json:"schema_version"`
	Calls         int                           `json:"calls"`
	Distribution  map[mcpworkload.Operation]int `json:"distribution"`
}

type metrics struct {
	P50MS                   float64 `json:"p50_ms"`
	P95MS                   float64 `json:"p95_ms"`
	P99MS                   float64 `json:"p99_ms"`
	ThroughputPerSecond     float64 `json:"throughput_per_s"`
	AllocationsPerOperation float64 `json:"allocations_per_op"`
	BytesPerOperation       float64 `json:"bytes_per_op"`
	RSSBytes                int64   `json:"rss_bytes"`
	Goroutines              int     `json:"goroutines"`
	Errors                  int     `json:"errors"`
	MemoryGrowthDetected    bool    `json:"memory_growth_detected"`
}

type operationResult struct {
	Operation      mcpworkload.Operation `json:"operation"`
	Calls          int                   `json:"calls"`
	BackendP50MS   float64               `json:"backend_p50_ms"`
	BackendP95MS   float64               `json:"backend_p95_ms"`
	BackendP99MS   float64               `json:"backend_p99_ms"`
	RoundTripP50MS float64               `json:"round_trip_p50_ms"`
	RoundTripP95MS float64               `json:"round_trip_p95_ms"`
	RoundTripP99MS float64               `json:"round_trip_p99_ms"`
	Errors         int                   `json:"errors"`
}

type sloCheck struct {
	Operation  mcpworkload.Operation `json:"operation"`
	P95LimitMS float64               `json:"p95_limit_ms"`
	P99LimitMS float64               `json:"p99_limit_ms"`
	P95MS      float64               `json:"backend_p95_ms"`
	P99MS      float64               `json:"backend_p99_ms"`
	Passed     bool                  `json:"passed"`
}

type latencyObserver struct {
	mu        sync.Mutex
	durations map[mcpworkload.Operation][]int64
}

func main() {
	cfg := config{}
	flag.IntVar(&cfg.Calls, "calls", defaultCalls, "measured MCP calls")
	flag.IntVar(&cfg.Warmup, "warmup", defaultWarmup, "warm-up calls per operation")
	flag.IntVar(&cfg.Symbols, "symbols", defaultSymbols, "synthetic snapshot symbols")
	flag.IntVar(&cfg.Edges, "edges", defaultEdges, "synthetic snapshot semantic edges")
	flag.Int64Var(&cfg.Seed, "seed", defaultSeed, "workload seed")
	flag.StringVar(&cfg.OutputDir, "output", defaultOutputDir, "results output directory")
	flag.Parse()

	result, err := run(context.Background(), cfg)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	if err := writeOutputs(cfg.OutputDir, result); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	fmt.Printf("one-client MCP benchmark: p50 %.3f ms, p95 %.3f ms, p99 %.3f ms, allocations/op %.1f, errors %d\n",
		result.Metrics.P50MS, result.Metrics.P95MS, result.Metrics.P99MS,
		result.Metrics.AllocationsPerOperation, result.Metrics.Errors)
}

func run(ctx context.Context, cfg config) (results, error) {
	if cfg.Calls < 1 {
		return results{}, errors.New("calls must be positive")
	}
	if cfg.Warmup < 0 {
		return results{}, errors.New("warmup must not be negative")
	}
	if cfg.Symbols < 20 {
		return results{}, errors.New("symbols must be at least 20")
	}
	if cfg.Edges < cfg.Symbols {
		return results{}, fmt.Errorf("edges must be at least symbols (%d)", cfg.Symbols)
	}

	rows, corpus, data := buildCorpus(cfg.Symbols, cfg.Edges)
	data.Seed = cfg.Seed
	snapshot, err := hotsnapshot.BuildGraphSnapshot(rows, 1, time.Unix(1_700_000_000, 0).UTC(), 1)
	if err != nil {
		return results{}, fmt.Errorf("build snapshot: %w", err)
	}
	store := hotsnapshot.NewSnapshotStore(snapshot)
	observer := &latencyObserver{durations: make(map[mcpworkload.Operation][]int64)}
	server := ladygraphmcp.NewServerWithObserverAndSnapshotStore(observer.observe, store)
	serverTransport, clientTransport := sdkmcp.NewInMemoryTransports()
	serverSession, err := server.Connect(ctx, serverTransport, nil)
	if err != nil {
		return results{}, fmt.Errorf("server.Connect(): %w", err)
	}
	defer serverSession.Close()
	client := sdkmcp.NewClient(&sdkmcp.Implementation{Name: "mcp-client-benchmark", Version: "0.1.0"}, nil)
	clientSession, err := client.Connect(ctx, clientTransport, nil)
	if err != nil {
		return results{}, fmt.Errorf("client.Connect(): %w", err)
	}
	defer clientSession.Close()

	workload, err := mcpworkload.Generate(ctx, mcpworkload.Config{
		Calls:  cfg.Calls,
		Seed:   cfg.Seed,
		Corpus: corpus,
	})
	if err != nil {
		return results{}, fmt.Errorf("generate workload: %w", err)
	}
	if err := warmup(ctx, clientSession, workload.Requests, cfg.Warmup); err != nil {
		return results{}, fmt.Errorf("warm-up: %w", err)
	}
	observer.reset()
	runtime.GC()
	var before, after runtime.MemStats
	runtime.ReadMemStats(&before)
	startRSS := readRSSBytes()
	startGoroutines := runtime.NumGoroutine()
	start := time.Now()

	roundTrip := make(map[mcpworkload.Operation][]int64, len(operationOrder()))
	allRoundTrip := make([]int64, 0, len(workload.Requests))
	errorCounts := make(map[mcpworkload.Operation]int, len(operationOrder()))
	for _, request := range workload.Requests {
		callStart := time.Now()
		result, callErr := clientSession.CallTool(ctx, &sdkmcp.CallToolParams{
			Name: string(request.Operation), Arguments: request.Arguments,
		})
		elapsed := time.Since(callStart).Nanoseconds()
		roundTrip[request.Operation] = append(roundTrip[request.Operation], elapsed)
		allRoundTrip = append(allRoundTrip, elapsed)
		if callErr != nil || result == nil || result.IsError {
			errorCounts[request.Operation]++
		}
	}
	elapsed := time.Since(start)
	observerDurations := observer.snapshot()

	runtime.GC()
	runtime.ReadMemStats(&after)
	growthSamples := []int64{startRSS, readRSSBytes()}
	for range growthBatches {
		if err := exercise(ctx, clientSession, workload.Requests, growthCalls); err != nil {
			return results{}, fmt.Errorf("growth probe: %w", err)
		}
		runtime.GC()
		growthSamples = append(growthSamples, readRSSBytes())
	}
	endGoroutines := runtime.NumGoroutine()
	if endGoroutines < startGoroutines {
		endGoroutines = startGoroutines
	}

	for _, operation := range operationOrder() {
		if len(roundTrip[operation]) != workload.Distribution[operation] {
			return results{}, fmt.Errorf("recorded %d round-trip calls for %s, want %d", len(roundTrip[operation]), operation, workload.Distribution[operation])
		}
		if len(observerDurations[operation]) != workload.Distribution[operation] {
			return results{}, fmt.Errorf("recorded %d backend calls for %s, want %d", len(observerDurations[operation]), operation, workload.Distribution[operation])
		}
	}

	sort.Slice(allRoundTrip, func(i, j int) bool { return allRoundTrip[i] < allRoundTrip[j] })
	operations := make([]operationResult, 0, len(operationOrder()))
	checks := make([]sloCheck, 0, len(operationOrder()))
	allSLOPassed := true
	for _, operation := range operationOrder() {
		sort.Slice(roundTrip[operation], func(i, j int) bool { return roundTrip[operation][i] < roundTrip[operation][j] })
		sort.Slice(observerDurations[operation], func(i, j int) bool { return observerDurations[operation][i] < observerDurations[operation][j] })
		backendP95, backendP99 := percentileMS(observerDurations[operation], 0.95), percentileMS(observerDurations[operation], 0.99)
		p95Limit, p99Limit := sloLimits(operation)
		passed := backendP95 <= p95Limit && backendP99 <= p99Limit && errorCounts[operation] == 0
		allSLOPassed = allSLOPassed && passed
		operations = append(operations, operationResult{
			Operation:      operation,
			Calls:          len(roundTrip[operation]),
			BackendP50MS:   percentileMS(observerDurations[operation], 0.50),
			BackendP95MS:   backendP95,
			BackendP99MS:   backendP99,
			RoundTripP50MS: percentileMS(roundTrip[operation], 0.50),
			RoundTripP95MS: percentileMS(roundTrip[operation], 0.95),
			RoundTripP99MS: percentileMS(roundTrip[operation], 0.99),
			Errors:         errorCounts[operation],
		})
		checks = append(checks, sloCheck{
			Operation: operation, P95LimitMS: p95Limit, P99LimitMS: p99Limit,
			P95MS: backendP95, P99MS: backendP99, Passed: passed,
		})
	}

	totalErrors := 0
	for _, count := range errorCounts {
		totalErrors += count
	}
	memoryGrowth := continuousGrowth(growthSamples)
	metricsResult := metrics{
		P50MS:                   percentileMS(allRoundTrip, 0.50),
		P95MS:                   percentileMS(allRoundTrip, 0.95),
		P99MS:                   percentileMS(allRoundTrip, 0.99),
		ThroughputPerSecond:     float64(len(allRoundTrip)) / elapsed.Seconds(),
		AllocationsPerOperation: float64(after.Mallocs-before.Mallocs) / float64(len(allRoundTrip)),
		BytesPerOperation:       float64(after.TotalAlloc-before.TotalAlloc) / float64(len(allRoundTrip)),
		RSSBytes:                maxRSS(append(growthSamples, readPeakRSSBytes())),
		Goroutines:              endGoroutines,
		Errors:                  totalErrors,
		MemoryGrowthDetected:    memoryGrowth,
	}
	return results{
		Benchmark:   "mcp-client-one",
		Command:     commandForConfig(cfg),
		Commit:      gitState(),
		GeneratedAt: time.Now().UTC(),
		Environment: environment{OS: runtime.GOOS, Arch: runtime.GOARCH, CPU: cpuModel(), Memory: memoryTotal(), Go: runtime.Version()},
		Dataset:     data,
		Workload:    workloadSummary{SchemaVersion: workload.SchemaVersion, Calls: workload.Calls, Distribution: workload.Distribution},
		WarmupCalls: cfg.Warmup,
		Metrics:     metricsResult,
		Operations:  operations,
		SLOChecks:   checks,
		SLOPassed:   allSLOPassed && totalErrors == 0 && !memoryGrowth,
	}, nil
}

func commandForConfig(cfg config) string {
	output := cfg.OutputDir
	if output == "" {
		output = defaultOutputDir
	}
	return fmt.Sprintf(
		"go run ./benchmarks/mcp-client --calls %d --warmup %d --symbols %d --edges %d --seed %d --output %s",
		cfg.Calls, cfg.Warmup, cfg.Symbols, cfg.Edges, cfg.Seed, output,
	)
}

func buildCorpus(symbols, edges int) (hotsnapshot.LadybugSnapshotRows, mcpworkload.Corpus, dataset) {
	repositories := 10
	packages := 100
	files := 1_000
	if symbols < repositories {
		repositories = symbols
	}
	if symbols < packages {
		packages = symbols
	}
	if symbols < files {
		files = symbols
	}
	rows := hotsnapshot.LadybugSnapshotRows{
		Repositories: make([]hotsnapshot.RepositoryRow, repositories),
		Packages:     make([]hotsnapshot.PackageRow, packages),
		Files:        make([]hotsnapshot.FileRow, files),
		Symbols:      make([]hotsnapshot.SymbolRow, symbols),
		Edges:        make([]hotsnapshot.EdgeRow, edges),
	}
	for index := range rows.Repositories {
		key := "repo-" + strconv.Itoa(index)
		rows.Repositories[index] = hotsnapshot.RepositoryRow{Key: key, Name: key, Path: "/synthetic/" + key, Languages: "go"}
	}
	for index := range rows.Packages {
		key := "pkg-" + strconv.Itoa(index)
		rows.Packages[index] = hotsnapshot.PackageRow{Key: key, RepositoryKey: "repo-" + strconv.Itoa(index%repositories), Name: "package-" + strconv.Itoa(index), ModulePath: "example.com/module-" + strconv.Itoa(index)}
	}
	for index := range rows.Files {
		key := "file-" + strconv.Itoa(index)
		rows.Files[index] = hotsnapshot.FileRow{Key: key, RepositoryKey: "repo-" + strconv.Itoa(index%repositories), PackageKey: "pkg-" + strconv.Itoa(index%packages), Path: "src/" + key + ".go"}
	}
	for index := range rows.Symbols {
		key := "s-" + strconv.Itoa(index)
		rows.Symbols[index] = hotsnapshot.SymbolRow{
			StableKey: hotsnapshot.StableKey(key), CanonicalIdentity: "go:" + key,
			FileKey: "file-" + strconv.Itoa(index%files), Name: "name-" + strconv.Itoa(index),
			QualifiedName: "module." + key, Kind: "function", Signature: "func " + key + "()",
		}
	}
	target := symbols / 2
	for index := range rows.Edges {
		source := index % symbols
		edgeTarget := (source + 1 + (index / symbols)) % symbols
		if index == 0 {
			source = (target + 1) % symbols
			edgeTarget = target
		}
		rows.Edges[index] = hotsnapshot.EdgeRow{
			SourceKey: rows.Symbols[source].StableKey, TargetKey: rows.Symbols[edgeTarget].StableKey,
			Kind: facts.CodeReferences, Confidence: facts.CodeExactTypechecked, Provenance: facts.CodeGoTypesUse,
			EvidenceKind: "types", EvidenceSourceFileKey: rows.Symbols[source].FileKey, EvidenceTargetFileKey: rows.Symbols[edgeTarget].FileKey,
		}
	}
	corpus := mcpworkload.Corpus{Probes: []mcpworkload.Probe{{
		Name: "name-" + strconv.Itoa(target), StableKey: "s-" + strconv.Itoa(target),
	}}}
	return rows, corpus, dataset{Name: "synthetic-mcp-client", Seed: defaultSeed, Repositories: repositories, Packages: packages, Files: files, Symbols: symbols, Edges: edges}
}

func operationOrder() []mcpworkload.Operation {
	return []mcpworkload.Operation{
		mcpworkload.FindSymbol,
		mcpworkload.GetSymbol,
		mcpworkload.FindReferences,
		mcpworkload.FindCrossRepoConsumers,
		mcpworkload.GetBlastRadius,
	}
}

func warmup(ctx context.Context, client *sdkmcp.ClientSession, requests []mcpworkload.Request, callsPerOperation int) error {
	if callsPerOperation == 0 {
		return nil
	}
	seen := make(map[mcpworkload.Operation]int, len(operationOrder()))
	for _, request := range requests {
		if seen[request.Operation] >= callsPerOperation {
			continue
		}
		if err := call(ctx, client, request); err != nil {
			return err
		}
		seen[request.Operation]++
	}
	return nil
}

func exercise(ctx context.Context, client *sdkmcp.ClientSession, requests []mcpworkload.Request, count int) error {
	if len(requests) == 0 {
		return nil
	}
	for index := 0; index < count; index++ {
		if err := call(ctx, client, requests[index%len(requests)]); err != nil {
			return err
		}
	}
	return nil
}

func call(ctx context.Context, client *sdkmcp.ClientSession, request mcpworkload.Request) error {
	result, err := client.CallTool(ctx, &sdkmcp.CallToolParams{Name: string(request.Operation), Arguments: request.Arguments})
	if err != nil {
		return err
	}
	if result == nil || result.IsError {
		return fmt.Errorf("%s returned an error result", request.Operation)
	}
	return nil
}

func (observer *latencyObserver) observe(tool string, elapsed time.Duration) {
	observer.mu.Lock()
	defer observer.mu.Unlock()
	observer.durations[mcpworkload.Operation(tool)] = append(observer.durations[mcpworkload.Operation(tool)], elapsed.Nanoseconds())
}

func (observer *latencyObserver) reset() {
	observer.mu.Lock()
	defer observer.mu.Unlock()
	observer.durations = make(map[mcpworkload.Operation][]int64)
}

func (observer *latencyObserver) snapshot() map[mcpworkload.Operation][]int64 {
	observer.mu.Lock()
	defer observer.mu.Unlock()
	result := make(map[mcpworkload.Operation][]int64, len(observer.durations))
	for operation, durations := range observer.durations {
		result[operation] = append([]int64(nil), durations...)
	}
	return result
}

func sloLimits(operation mcpworkload.Operation) (float64, float64) {
	switch operation {
	case mcpworkload.FindSymbol:
		return 2, 5
	case mcpworkload.GetSymbol:
		return 1, 2
	case mcpworkload.FindReferences:
		return 5, 10
	case mcpworkload.FindCrossRepoConsumers:
		return 5, 15
	case mcpworkload.GetBlastRadius:
		return 20, 40
	default:
		return math.Inf(1), math.Inf(1)
	}
}

func percentileMS(values []int64, percentile float64) float64 {
	if len(values) == 0 {
		return 0
	}
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
	var result int64
	for _, sample := range samples {
		if sample > result {
			result = sample
		}
	}
	return result
}

func writeOutputs(outputDir string, result results) error {
	data, err := json.MarshalIndent(result, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal results: %w", err)
	}
	if err := os.MkdirAll(outputDir, 0o755); err != nil {
		return fmt.Errorf("create output directory: %w", err)
	}
	if err := os.WriteFile(filepath.Join(outputDir, "results.json"), append(data, '\n'), 0o644); err != nil {
		return fmt.Errorf("write results: %w", err)
	}

	var report strings.Builder
	fmt.Fprintln(&report, "# MCP one-client benchmark")
	fmt.Fprintln(&report)
	fmt.Fprintf(&report, "- Command: `%s`\n", result.Command)
	fmt.Fprintf(&report, "- Commit: `%s`\n", result.Commit)
	fmt.Fprintf(&report, "- Generated at: `%s`\n", result.GeneratedAt.Format(time.RFC3339))
	fmt.Fprintf(&report, "- Environment: `%s/%s`, `%s`, `%s`, `%s`\n", result.Environment.OS, result.Environment.Arch, result.Environment.CPU, result.Environment.Memory, result.Environment.Go)
	fmt.Fprintf(&report, "- Dataset: `%s`, %d symbols, %d edges, seed %d\n", result.Dataset.Name, result.Dataset.Symbols, result.Dataset.Edges, result.Dataset.Seed)
	fmt.Fprintf(&report, "- Workload: %d calls, warm-up %d calls per operation\n\n", result.Workload.Calls, result.WarmupCalls)
	fmt.Fprintln(&report, "## Overall metrics")
	fmt.Fprintln(&report)
	fmt.Fprintln(&report, "| p50 round-trip ms | p95 round-trip ms | p99 round-trip ms | Throughput/s | Allocs/op | Bytes/op | RSS bytes | Goroutines | Errors | Memory growth |")
	fmt.Fprintln(&report, "| ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: | --- |")
	fmt.Fprintf(&report, "| %.3f | %.3f | %.3f | %.1f | %.1f | %.1f | %d | %d | %d | %t |\n\n", result.Metrics.P50MS, result.Metrics.P95MS, result.Metrics.P99MS, result.Metrics.ThroughputPerSecond, result.Metrics.AllocationsPerOperation, result.Metrics.BytesPerOperation, result.Metrics.RSSBytes, result.Metrics.Goroutines, result.Metrics.Errors, result.Metrics.MemoryGrowthDetected)
	fmt.Fprintln(&report, "## Per-operation metrics")
	fmt.Fprintln(&report)
	fmt.Fprintln(&report, "| Operation | Calls | Backend p50 ms | Backend p95 ms | Backend p99 ms | Round-trip p50 ms | Round-trip p95 ms | Round-trip p99 ms | Errors |")
	fmt.Fprintln(&report, "| --- | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: |")
	for _, operation := range result.Operations {
		fmt.Fprintf(&report, "| `%s` | %d | %.3f | %.3f | %.3f | %.3f | %.3f | %.3f | %d |\n", operation.Operation, operation.Calls, operation.BackendP50MS, operation.BackendP95MS, operation.BackendP99MS, operation.RoundTripP50MS, operation.RoundTripP95MS, operation.RoundTripP99MS, operation.Errors)
	}
	fmt.Fprintln(&report)
	fmt.Fprintln(&report, "## SLO comparison")
	fmt.Fprintln(&report)
	fmt.Fprintln(&report, "The SLO comparison uses backend observer timings, excluding MCP transport and client serialization, as required by `docs/performance/slo.md`.")
	fmt.Fprintln(&report)
	fmt.Fprintln(&report, "| Operation | Backend p95 ms | Limit | Backend p99 ms | Limit | Pass |")
	fmt.Fprintln(&report, "| --- | ---: | ---: | ---: | ---: | --- |")
	for _, check := range result.SLOChecks {
		fmt.Fprintf(&report, "| `%s` | %.3f | %.3f | %.3f | %.3f | %t |\n", check.Operation, check.P95MS, check.P95LimitMS, check.P99MS, check.P99LimitMS, check.Passed)
	}
	fmt.Fprintf(&report, "\nOverall SLO result: `%t`.\n\n", result.SLOPassed)
	fmt.Fprintln(&report, "Allocations/op and bytes/op are process-wide deltas over the measured mixed workload after warm-up. Repeat with the same command on target hardware before treating this one-client result as a release gate.")
	fmt.Fprintln(&report, "The client uses the SDK in-memory transport; this result excludes stdio, socket and network overhead.")
	fmt.Fprintln(&report, "RSS is the process peak and includes synthetic corpus and HotSnapshot construction before measurement.")
	if err := os.WriteFile(filepath.Join(outputDir, "report.md"), []byte(report.String()), 0o644); err != nil {
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
			value, err := strconv.ParseInt(fields[1], 10, 64)
			if err == nil {
				return value * 1024
			}
		}
	}
	return 0
}

func readPeakRSSBytes() int64 {
	data, err := os.ReadFile("/proc/self/status")
	if err != nil {
		return 0
	}
	for _, line := range strings.Split(string(data), "\n") {
		fields := strings.Fields(line)
		if len(fields) >= 2 && fields[0] == "VmHWM:" {
			value, err := strconv.ParseInt(fields[1], 10, 64)
			if err == nil {
				return value * 1024
			}
		}
	}
	return 0
}

func cpuModel() string {
	data, err := os.ReadFile("/proc/cpuinfo")
	if err != nil {
		return "unknown"
	}
	for _, line := range strings.Split(string(data), "\n") {
		if strings.HasPrefix(line, "model name") {
			if _, value, ok := strings.Cut(line, ":"); ok {
				return strings.TrimSpace(value)
			}
		}
	}
	return "unknown"
}

func memoryTotal() string {
	data, err := os.ReadFile("/proc/meminfo")
	if err != nil {
		return "unknown"
	}
	for _, line := range strings.Split(string(data), "\n") {
		if strings.HasPrefix(line, "MemTotal:") {
			return strings.TrimSpace(strings.TrimPrefix(line, "MemTotal:"))
		}
	}
	return "unknown"
}
