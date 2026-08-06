//go:build ladybug && cgo

package main

import (
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
	"strings"
	"time"

	"github.com/Luqueee/ladygraph/internal/storage/ladybug"
)

const (
	defaultDatabase   = "benchmarks/ladybug-bulk/graph.db"
	defaultCorpus     = "testdata/generated/synthetic"
	defaultOutput     = "benchmarks/ladybug-queries"
	defaultIterations = 100
	defaultWarmup     = 5
	queryResultLimit  = 100
)

type config struct {
	DatabasePath string
	CorpusDir    string
	OutputDir    string
	Iterations   int
	Warmup       int
}

type results struct {
	Benchmark   string            `json:"benchmark"`
	Command     string            `json:"command"`
	Commit      string            `json:"commit"`
	GeneratedAt time.Time         `json:"generated_at"`
	GoVersion   string            `json:"go_version"`
	GOOS        string            `json:"goos"`
	GOARCH      string            `json:"goarch"`
	Corpus      manifest          `json:"corpus"`
	Iterations  int               `json:"iterations"`
	Warmup      int               `json:"warmup"`
	Operations  []operationResult `json:"operations"`
}

type operationResult struct {
	Operation           string  `json:"operation"`
	Calls               int     `json:"calls"`
	Errors              int     `json:"errors"`
	AverageReturned     float64 `json:"average_returned"`
	P50Microseconds     float64 `json:"p50_us"`
	P95Microseconds     float64 `json:"p95_us"`
	P99Microseconds     float64 `json:"p99_us"`
	MaximumMicroseconds float64 `json:"max_us"`
	CallsPerSecond      float64 `json:"calls_per_s"`
}

type manifest struct {
	SchemaVersion  string   `json:"schema_version"`
	Seed           int64    `json:"seed"`
	Repositories   int      `json:"repositories"`
	Files          int      `json:"files"`
	Symbols        int      `json:"symbols"`
	Edges          int      `json:"edges"`
	HubSymbols     []string `json:"hub_symbols"`
	DepthFiveChain []string `json:"depth_five_chain"`
}

type workload struct {
	name string
	run  func() (int, error)
}

func main() {
	cfg := config{}
	flag.StringVar(&cfg.DatabasePath, "database", defaultDatabase, "loaded LadybugDB database path")
	flag.StringVar(&cfg.CorpusDir, "corpus", defaultCorpus, "synthetic corpus directory")
	flag.StringVar(&cfg.OutputDir, "output", defaultOutput, "results output directory")
	flag.IntVar(&cfg.Iterations, "iterations", defaultIterations, "measured calls per operation")
	flag.IntVar(&cfg.Warmup, "warmup", defaultWarmup, "warm-up calls per operation")
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
	for _, operation := range result.Operations {
		fmt.Printf("%s: p50 %.1f us, p95 %.1f us, p99 %.1f us\n", operation.Operation, operation.P50Microseconds, operation.P95Microseconds, operation.P99Microseconds)
	}
}

func run(ctx context.Context, cfg config) (results, error) {
	if cfg.Iterations < 1 {
		return results{}, errors.New("iterations must be positive")
	}
	if cfg.Warmup < 0 {
		return results{}, errors.New("warmup must not be negative")
	}
	corpus, err := readManifest(filepath.Join(cfg.CorpusDir, "manifest.json"))
	if err != nil {
		return results{}, err
	}
	if len(corpus.HubSymbols) == 0 || len(corpus.DepthFiveChain) != 6 {
		return results{}, errors.New("manifest lacks required hub and depth-five probes")
	}
	storageConfig := ladybug.DefaultConfig()
	storageConfig.ReadOnly = true
	database, err := ladybug.Open(ctx, cfg.DatabasePath, storageConfig)
	if err != nil {
		return results{}, fmt.Errorf("open database: %w", err)
	}
	defer database.Close()
	reader, err := database.OpenReader(ctx)
	if err != nil {
		return results{}, fmt.Errorf("open reader: %w", err)
	}
	defer reader.Close()
	if err := verifyGoldenQueries(ctx, reader, corpus); err != nil {
		return results{}, fmt.Errorf("verify golden queries: %w", err)
	}

	hub := corpus.HubSymbols[0]
	chainStart := corpus.DepthFiveChain[0]
	chainEnd := corpus.DepthFiveChain[len(corpus.DepthFiveChain)-1]
	lookupIndex := 0
	workloads := []workload{
		{name: "get_symbol", run: func() (int, error) {
			key := fmt.Sprintf("symbol-%08d", lookupIndex%corpus.Symbols)
			lookupIndex++
			_, found, err := reader.GetSymbol(ctx, key)
			if err != nil {
				return 0, err
			}
			if !found {
				return 0, fmt.Errorf("symbol %s not found", key)
			}
			return 1, nil
		}},
		{name: "incoming_references_100", run: func() (int, error) {
			values, err := reader.IncomingReferences(ctx, hub, queryResultLimit)
			return len(values), err
		}},
		{name: "outgoing_references_100", run: func() (int, error) {
			values, err := reader.OutgoingReferences(ctx, hub, queryResultLimit)
			return len(values), err
		}},
		{name: "traverse_depth_3_100", run: func() (int, error) {
			values, err := reader.TraverseOutgoing(ctx, chainStart, 3, queryResultLimit)
			return len(values), err
		}},
		{name: "traverse_depth_5_100", run: func() (int, error) {
			values, err := reader.TraverseOutgoing(ctx, chainStart, 5, queryResultLimit)
			return len(values), err
		}},
		{name: "shortest_path_depth_5", run: func() (int, error) {
			path, found, err := reader.ShortestPath(ctx, chainStart, chainEnd, 5)
			if err != nil {
				return 0, err
			}
			if !found {
				return 0, errors.New("depth-five chain path not found")
			}
			return len(path.StableKeys), nil
		}},
		{name: "incoming_by_repository", run: func() (int, error) {
			values, err := reader.IncomingReferencesByRepository(ctx, hub)
			return len(values), err
		}},
	}
	result := results{
		Benchmark:   "ladybug-direct-queries",
		Command:     strings.Join(os.Args, " "),
		Commit:      gitState(),
		GeneratedAt: time.Now().UTC(),
		GoVersion:   runtime.Version(),
		GOOS:        runtime.GOOS,
		GOARCH:      runtime.GOARCH,
		Corpus:      corpus,
		Iterations:  cfg.Iterations,
		Warmup:      cfg.Warmup,
		Operations:  make([]operationResult, 0, len(workloads)),
	}
	for _, item := range workloads {
		measured, err := measure(ctx, item, cfg.Warmup, cfg.Iterations)
		if err != nil {
			return results{}, err
		}
		result.Operations = append(result.Operations, measured)
	}
	return result, nil
}

func verifyGoldenQueries(ctx context.Context, reader ladybug.Reader, corpus manifest) error {
	start := corpus.DepthFiveChain[0]
	end := corpus.DepthFiveChain[len(corpus.DepthFiveChain)-1]
	symbol, found, err := reader.GetSymbol(ctx, start)
	if err != nil {
		return err
	}
	if !found || symbol.StableKey != start {
		return fmt.Errorf("stable-key lookup returned %#v, found=%t", symbol, found)
	}
	incoming, err := reader.IncomingReferences(ctx, corpus.HubSymbols[0], queryResultLimit)
	if err != nil {
		return err
	}
	outgoing, err := reader.OutgoingReferences(ctx, corpus.HubSymbols[0], queryResultLimit)
	if err != nil {
		return err
	}
	if len(incoming) == 0 || len(outgoing) == 0 {
		return fmt.Errorf("hub references incoming=%d outgoing=%d", len(incoming), len(outgoing))
	}
	depthThree, err := reader.TraverseOutgoing(ctx, start, 3, ladybug.MaxTraversalResults)
	if err != nil {
		return err
	}
	if err := validateTraversalResult(depthThree, 3, ladybug.MaxTraversalResults); err != nil {
		return fmt.Errorf("depth three: %w", err)
	}
	depthFive, err := reader.TraverseOutgoing(ctx, start, 5, ladybug.MaxTraversalResults)
	if err != nil {
		return err
	}
	if err := validateTraversalResult(depthFive, 5, ladybug.MaxTraversalResults); err != nil {
		return fmt.Errorf("depth five: %w", err)
	}
	path, found, err := reader.ShortestPath(ctx, start, end, 5)
	if err != nil {
		return err
	}
	if !found || path.Length < 1 || path.Length > 5 || len(path.StableKeys) != path.Length+1 || path.StableKeys[0] != start || path.StableKeys[len(path.StableKeys)-1] != end {
		return fmt.Errorf("shortest path = %#v, found=%t", path, found)
	}
	groups, err := reader.IncomingReferencesByRepository(ctx, corpus.HubSymbols[0])
	if err != nil {
		return err
	}
	if len(groups) == 0 {
		return errors.New("incoming repository grouping is empty")
	}
	for index := 1; index < len(groups); index++ {
		if groups[index-1].RepositoryKey >= groups[index].RepositoryKey {
			return errors.New("incoming repository grouping is not strictly ordered")
		}
	}
	return nil
}

func validateTraversalResult(nodes []ladybug.TraversalNode, maximumDepth, limit int) error {
	if len(nodes) == 0 {
		return errors.New("traversal result is empty")
	}
	if len(nodes) > limit {
		return fmt.Errorf("traversal returned %d nodes, limit is %d", len(nodes), limit)
	}
	seen := make(map[string]struct{}, len(nodes))
	for index, node := range nodes {
		if node.Depth < 1 || node.Depth > maximumDepth {
			return fmt.Errorf("symbol %s has invalid depth %d", node.StableKey, node.Depth)
		}
		if _, exists := seen[node.StableKey]; exists {
			return fmt.Errorf("symbol %s occurs more than once", node.StableKey)
		}
		seen[node.StableKey] = struct{}{}
		if index > 0 {
			previous := nodes[index-1]
			if previous.Depth > node.Depth || (previous.Depth == node.Depth && previous.StableKey >= node.StableKey) {
				return fmt.Errorf("traversal result is not strictly ordered at index %d", index)
			}
		}
	}
	return nil
}

func measure(ctx context.Context, item workload, warmup, iterations int) (operationResult, error) {
	for range warmup {
		if err := ctx.Err(); err != nil {
			return operationResult{}, err
		}
		if _, err := item.run(); err != nil {
			return operationResult{}, fmt.Errorf("warm up %s: %w", item.name, err)
		}
	}
	durations := make([]int64, iterations)
	returned := 0
	totalStart := time.Now()
	for index := range iterations {
		if err := ctx.Err(); err != nil {
			return operationResult{}, err
		}
		start := time.Now()
		count, err := item.run()
		durations[index] = time.Since(start).Nanoseconds()
		if err != nil {
			return operationResult{}, fmt.Errorf("measure %s call %d: %w", item.name, index, err)
		}
		returned += count
	}
	totalDuration := time.Since(totalStart)
	sort.Slice(durations, func(left, right int) bool { return durations[left] < durations[right] })
	return operationResult{
		Operation:           item.name,
		Calls:               iterations,
		Errors:              0,
		AverageReturned:     float64(returned) / float64(iterations),
		P50Microseconds:     percentile(durations, 0.50) / 1_000,
		P95Microseconds:     percentile(durations, 0.95) / 1_000,
		P99Microseconds:     percentile(durations, 0.99) / 1_000,
		MaximumMicroseconds: float64(durations[len(durations)-1]) / 1_000,
		CallsPerSecond:      float64(iterations) / totalDuration.Seconds(),
	}, nil
}

func percentile(sortedNanoseconds []int64, quantile float64) float64 {
	index := int(float64(len(sortedNanoseconds))*quantile+0.999999999) - 1
	if index < 0 {
		index = 0
	}
	if index >= len(sortedNanoseconds) {
		index = len(sortedNanoseconds) - 1
	}
	return float64(sortedNanoseconds[index])
}

func readManifest(path string) (manifest, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return manifest{}, fmt.Errorf("read manifest: %w", err)
	}
	var value manifest
	if err := json.Unmarshal(data, &value); err != nil {
		return manifest{}, fmt.Errorf("decode manifest: %w", err)
	}
	return value, nil
}

func writeOutputs(outputDir string, result results) error {
	if err := os.MkdirAll(outputDir, 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(result, "", "  ")
	if err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(outputDir, "results.json"), append(data, '\n'), 0o644); err != nil {
		return err
	}
	var report strings.Builder
	fmt.Fprintln(&report, "# LadybugDB direct query benchmark")
	fmt.Fprintln(&report)
	fmt.Fprintf(&report, "- Command: `%s`\n", result.Command)
	fmt.Fprintf(&report, "- Commit: `%s`\n", result.Commit)
	fmt.Fprintf(&report, "- Generated at: `%s`\n", result.GeneratedAt.Format(time.RFC3339))
	fmt.Fprintf(&report, "- Platform: `%s/%s`, `%s`\n", result.GOOS, result.GOARCH, result.GoVersion)
	fmt.Fprintf(&report, "- Corpus: seed %d, %d repositories, %d symbols, %d edges\n", result.Corpus.Seed, result.Corpus.Repositories, result.Corpus.Symbols, result.Corpus.Edges)
	fmt.Fprintf(&report, "- Calls: %d measured + %d warm-up per operation\n", result.Iterations, result.Warmup)
	fmt.Fprintln(&report)
	fmt.Fprintln(&report, "| Operation | p50 us | p95 us | p99 us | max us | calls/s | avg returned | errors |")
	fmt.Fprintln(&report, "| --- | ---: | ---: | ---: | ---: | ---: | ---: | ---: |")
	for _, operation := range result.Operations {
		fmt.Fprintf(&report, "| %s | %.1f | %.1f | %.1f | %.1f | %.1f | %.1f | %d |\n", operation.Operation, operation.P50Microseconds, operation.P95Microseconds, operation.P99Microseconds, operation.MaximumMicroseconds, operation.CallsPerSecond, operation.AverageReturned, operation.Errors)
	}
	fmt.Fprintln(&report)
	fmt.Fprintln(&report, "Golden probes validate stable-key lookup, both reference directions, depth-3 and depth-5 reachability, shortest path endpoints, and deterministic repository grouping before measurement.")
	fmt.Fprintln(&report)
	fmt.Fprintln(&report, "These are direct LadybugDB measurements. They do not qualify the HotSnapshot MCP SLOs, which are measured in later phases.")
	return os.WriteFile(filepath.Join(outputDir, "report.md"), []byte(report.String()), 0o644)
}

func gitState() string {
	output, err := exec.Command("git", "rev-parse", "HEAD").Output()
	if err != nil {
		return "unknown"
	}
	commit := strings.TrimSpace(string(output))
	status, err := exec.Command("git", "status", "--porcelain").Output()
	if err == nil && len(status) > 0 {
		commit += "-dirty"
	}
	return commit
}
