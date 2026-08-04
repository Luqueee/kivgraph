//go:build ladybug && cgo

package main

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/Luqueee/luque/internal/synthetic"
)

func TestRunLoadsAndVerifiesEveryBatchSize(t *testing.T) {
	corpusDir := filepath.Join(t.TempDir(), "corpus")
	generated, err := synthetic.Generate(context.Background(), synthetic.Config{
		Repositories: 2,
		Files:        10,
		Symbols:      20,
		Edges:        100,
		Seed:         42,
		OutputDir:    corpusDir,
	})
	if err != nil {
		t.Fatalf("Generate() error = %v", err)
	}

	result, err := run(context.Background(), config{
		CorpusDir:   corpusDir,
		DatabaseDir: filepath.Join(t.TempDir(), "databases"),
		SchemaPath:  filepath.Join("..", "..", "schemas", "ladybug", "001-synthetic.cypher"),
		OutputDir:   t.TempDir(),
		BatchSizes:  []int{10, 50},
	})
	if err != nil {
		t.Fatalf("run() error = %v", err)
	}
	if len(result.Scenarios) != 2 {
		t.Fatalf("scenario count = %d, want 2", len(result.Scenarios))
	}
	wantNodes := generated.Repositories + generated.Files + generated.Symbols
	for _, scenario := range result.Scenarios {
		if scenario.Nodes != wantNodes || scenario.Edges != generated.Edges {
			t.Fatalf("batch %d loaded nodes=%d edges=%d, want nodes=%d edges=%d", scenario.BatchSize, scenario.Nodes, scenario.Edges, wantNodes, generated.Edges)
		}
		if scenario.Transactions != expectedTransactions(generated, scenario.BatchSize) {
			t.Fatalf("batch %d transactions=%d, want %d", scenario.BatchSize, scenario.Transactions, expectedTransactions(generated, scenario.BatchSize))
		}
		if scenario.NodesPerSecond <= 0 || scenario.EdgesPerSecond <= 0 || scenario.PeakRSSBytes <= 0 || scenario.DatabaseBytes <= 0 {
			t.Fatalf("batch %d has invalid metrics: %#v", scenario.BatchSize, scenario)
		}
	}
}

func TestParseBatchSizesRejectsInvalidInputAndDeduplicates(t *testing.T) {
	sizes, err := parseBatchSizes("100, 1000,100")
	if err != nil {
		t.Fatalf("parseBatchSizes() error = %v", err)
	}
	if len(sizes) != 2 || sizes[0] != 100 || sizes[1] != 1000 {
		t.Fatalf("parseBatchSizes() = %v, want [100 1000]", sizes)
	}
	if _, err := parseBatchSizes("100,0"); err == nil {
		t.Fatal("parseBatchSizes() error = nil, want invalid-size error")
	}
}

func TestRecommendBatchSizeMaximizesThroughputUnderRSSLimit(t *testing.T) {
	scenarios := []scenarioResult{
		{BatchSize: 100, RecordsPerSecond: 2_500, PeakRSSBytes: 200_000_000},
		{BatchSize: 10_000, RecordsPerSecond: 3_700, PeakRSSBytes: 1_300_000_000},
		{BatchSize: 50_000, RecordsPerSecond: 3_900, PeakRSSBytes: maxQualificationRSS + 1},
	}
	if got := recommendBatchSize(scenarios); got != 10_000 {
		t.Fatalf("recommendBatchSize() = %d, want 10000", got)
	}
}

func expectedTransactions(generated synthetic.Manifest, batchSize int) int {
	transactions := ceilDiv(generated.Repositories, batchSize) + ceilDiv(generated.Files, batchSize) + ceilDiv(generated.Symbols, batchSize)
	for _, count := range generated.EdgeCounts {
		transactions += ceilDiv(count, batchSize)
	}
	return transactions
}

func ceilDiv(value, divisor int) int {
	return (value + divisor - 1) / divisor
}
