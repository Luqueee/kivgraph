// Package mcpworkload generates deterministic MCP query workloads.
package mcpworkload

import (
	"context"
	"fmt"
	"math/rand/v2"
)

const (
	SchemaVersion          = "mcp-workload-v1"
	DefaultCallCount       = 10_000
	DefaultSeed      int64 = 42
)

// Operation is one read-only MCP tool included in the performance workload.
type Operation string

const (
	FindSymbol             Operation = "find_symbol"
	GetSymbol              Operation = "get_symbol"
	FindReferences         Operation = "find_references"
	FindCrossRepoConsumers Operation = "find_cross_repo_consumers"
	GetBlastRadius         Operation = "get_blast_radius"
)

// Probe identifies a symbol that is valid in the snapshot under test. Name is
// used by find_symbol; StableKey is used by the other operations.
type Probe struct {
	Name      string `json:"name"`
	StableKey string `json:"stable_key"`
}

// Corpus contains the valid query probes for one snapshot.
type Corpus struct {
	Probes []Probe `json:"probes"`
}

// DefaultCorpus uses the probe names emitted by internal/synthetic for its
// deterministic corpus. A benchmark can replace it with probes from another
// corpus without changing the workload distribution.
func DefaultCorpus() Corpus {
	return Corpus{Probes: []Probe{
		{Name: "symbol_00000000", StableKey: "symbol-00000000"},
		{Name: "symbol_00000001", StableKey: "symbol-00000001"},
	}}
}

// Config controls one generated workload.
type Config struct {
	Calls  int
	Seed   int64
	Corpus Corpus
}

// DefaultConfig returns the standard workload used by the MCP performance
// phase. Ten thousand calls makes the percentages exact while remaining small
// enough for a smoke run.
func DefaultConfig() Config {
	return Config{Calls: DefaultCallCount, Seed: DefaultSeed, Corpus: DefaultCorpus()}
}

// Request is one MCP CallTool request and can be passed directly to the SDK.
type Request struct {
	Sequence  int            `json:"sequence"`
	Operation Operation      `json:"operation"`
	Arguments map[string]any `json:"arguments"`
}

// Workload is the self-describing output consumed by later benchmark tasks.
type Workload struct {
	SchemaVersion string            `json:"schema_version"`
	Seed          int64             `json:"seed"`
	Calls         int               `json:"calls"`
	Distribution  map[Operation]int `json:"distribution"`
	Requests      []Request         `json:"requests"`
}

type weightedOperation struct {
	operation Operation
	weight    int
}

var operationWeights = [...]weightedOperation{
	{operation: FindSymbol, weight: 40},
	{operation: GetSymbol, weight: 25},
	{operation: FindReferences, weight: 20},
	{operation: FindCrossRepoConsumers, weight: 10},
	{operation: GetBlastRadius, weight: 5},
}

// Generate creates a deterministic workload. The operation counts are the
// largest-remainder allocation of the declared weights, so the required
// percentages are exact when Calls is divisible by 20 and remain balanced for
// smaller smoke runs.
func Generate(ctx context.Context, config Config) (Workload, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if config.Calls == 0 {
		config.Calls = DefaultCallCount
	}
	if config.Seed == 0 {
		config.Seed = DefaultSeed
	}
	if len(config.Corpus.Probes) == 0 {
		config.Corpus = DefaultCorpus()
	}
	if err := validateConfig(config); err != nil {
		return Workload{}, err
	}

	counts := allocateCounts(config.Calls)
	operations := make([]Operation, 0, config.Calls)
	for _, item := range operationWeights {
		for range counts[item.operation] {
			operations = append(operations, item.operation)
		}
	}

	rng := rand.New(rand.NewPCG(uint64(config.Seed), uint64(config.Seed)^0x9e3779b97f4a7c15))
	for index := len(operations) - 1; index > 0; index-- {
		if err := contextErr(ctx); err != nil {
			return Workload{}, err
		}
		swap := rng.IntN(index + 1)
		operations[index], operations[swap] = operations[swap], operations[index]
	}

	requests := make([]Request, config.Calls)
	distribution := make(map[Operation]int, len(operationWeights))
	for sequence, operation := range operations {
		if err := contextErr(ctx); err != nil {
			return Workload{}, err
		}
		probe := config.Corpus.Probes[rng.IntN(len(config.Corpus.Probes))]
		requests[sequence] = Request{
			Sequence:  sequence,
			Operation: operation,
			Arguments: argumentsFor(operation, probe),
		}
		distribution[operation]++
	}

	return Workload{
		SchemaVersion: SchemaVersion,
		Seed:          config.Seed,
		Calls:         config.Calls,
		Distribution:  distribution,
		Requests:      requests,
	}, nil
}

func validateConfig(config Config) error {
	if config.Calls < 1 {
		return fmt.Errorf("calls must be positive: %d", config.Calls)
	}
	if len(config.Corpus.Probes) == 0 {
		return fmt.Errorf("corpus must contain at least one probe")
	}
	seen := make(map[string]struct{}, len(config.Corpus.Probes))
	for index, probe := range config.Corpus.Probes {
		if probe.Name == "" || probe.StableKey == "" {
			return fmt.Errorf("corpus probe %d must contain name and stable_key", index)
		}
		if _, exists := seen[probe.StableKey]; exists {
			return fmt.Errorf("corpus contains duplicate stable_key %q", probe.StableKey)
		}
		seen[probe.StableKey] = struct{}{}
	}
	return nil
}

func allocateCounts(calls int) map[Operation]int {
	counts := make(map[Operation]int, len(operationWeights))
	remainders := make([]int, len(operationWeights))
	allocated := 0
	for index, item := range operationWeights {
		product := calls * item.weight
		counts[item.operation] = product / 100
		remainders[index] = product % 100
		allocated += counts[item.operation]
	}
	for index := 0; allocated < calls; index++ {
		best := index % len(operationWeights)
		for candidate := 0; candidate < len(operationWeights); candidate++ {
			if remainders[candidate] > remainders[best] {
				best = candidate
			}
		}
		counts[operationWeights[best].operation]++
		remainders[best] = -1
		allocated++
	}
	return counts
}

func argumentsFor(operation Operation, probe Probe) map[string]any {
	switch operation {
	case FindSymbol:
		return map[string]any{"name": probe.Name, "mode": "exact", "limit": 50}
	case GetSymbol:
		return map[string]any{"stable_key": probe.StableKey}
	case FindReferences:
		return map[string]any{"stable_key": probe.StableKey, "direction": "incoming", "limit": 50}
	case FindCrossRepoConsumers:
		return map[string]any{"stable_key": probe.StableKey, "limit": 50}
	case GetBlastRadius:
		return map[string]any{"stable_key": probe.StableKey, "depth": 3, "max_nodes": 5000, "limit": 50}
	default:
		panic(fmt.Sprintf("unsupported operation %q", operation))
	}
}

func contextErr(ctx context.Context) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
		return nil
	}
}
