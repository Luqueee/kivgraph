package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"

	"github.com/Luqueee/ladygraph/internal/mcpworkload"
)

const defaultOutput = "benchmarks/mcp-workload/workload.json"

func main() {
	calls := flag.Int("calls", mcpworkload.DefaultCallCount, "number of MCP calls to generate")
	seed := flag.Int64("seed", mcpworkload.DefaultSeed, "deterministic workload seed")
	output := flag.String("output", defaultOutput, "JSON workload output path")
	symbolName := flag.String("symbol-name", "symbol_00000000", "find_symbol probe name")
	stableKey := flag.String("stable-key", "symbol-00000000", "stable key used by symbol queries")
	flag.Parse()

	config := mcpworkload.Config{
		Calls: *calls,
		Seed:  *seed,
		Corpus: mcpworkload.Corpus{Probes: []mcpworkload.Probe{{
			Name: *symbolName, StableKey: *stableKey,
		}}},
	}
	workload, err := mcpworkload.Generate(context.Background(), config)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	if err := writeWorkload(*output, workload); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	fmt.Printf("generated %d MCP calls at %s\n", workload.Calls, *output)
	for _, operation := range []mcpworkload.Operation{
		mcpworkload.FindSymbol,
		mcpworkload.GetSymbol,
		mcpworkload.FindReferences,
		mcpworkload.FindCrossRepoConsumers,
		mcpworkload.GetBlastRadius,
	} {
		fmt.Printf("%s: %d\n", operation, workload.Distribution[operation])
	}
}

func writeWorkload(path string, workload mcpworkload.Workload) error {
	data, err := json.MarshalIndent(workload, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal workload: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("create output directory: %w", err)
	}
	if err := os.WriteFile(path, append(data, '\n'), 0o644); err != nil {
		return fmt.Errorf("write workload: %w", err)
	}
	return nil
}
