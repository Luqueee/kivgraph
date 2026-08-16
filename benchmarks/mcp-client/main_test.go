package main

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/Luqueee/kivgraph/internal/mcpworkload"
)

func TestPercentileUsesNearestRank(t *testing.T) {
	values := []int64{1_000_000, 2_000_000, 3_000_000, 4_000_000}
	if got := percentileMS(values, 0.50); got != 2 {
		t.Fatalf("p50 = %v, want 2", got)
	}
	if got := percentileMS(values, 0.95); got != 4 {
		t.Fatalf("p95 = %v, want 4", got)
	}
}

func TestBuildCorpusHasCrossRepositoryProbeAndEdges(t *testing.T) {
	rows, corpus, dataset := buildCorpus(100, 1_000)
	if dataset.Symbols != 100 || dataset.Edges != 1_000 {
		t.Fatalf("dataset = %#v", dataset)
	}
	if len(corpus.Probes) != 1 || corpus.Probes[0].Name != "name-50" || corpus.Probes[0].StableKey != "s-50" {
		t.Fatalf("probe = %#v", corpus.Probes)
	}
	if len(rows.Edges) != 1_000 || rows.Edges[0].TargetKey != "s-50" {
		t.Fatalf("first edge = %#v", rows.Edges[0])
	}
}

func TestRunSmallOneClientBenchmark(t *testing.T) {
	result, err := run(context.Background(), config{Calls: 20, Warmup: 1, Symbols: 100, Edges: 1_000, Seed: 42})
	if err != nil {
		t.Fatalf("run() error = %v", err)
	}
	if result.Metrics.Errors != 0 {
		t.Fatalf("metrics = %#v", result.Metrics)
	}
	for _, check := range result.SLOChecks {
		if !check.Passed {
			t.Fatalf("SLO check failed: %#v", check)
		}
	}
	if len(result.Operations) != 5 {
		t.Fatalf("operations = %d, want 5", len(result.Operations))
	}
}

func TestPartitionRequestsPreservesOrderAcrossClients(t *testing.T) {
	requests := make([]mcpworkload.Request, 7)
	for index := range requests {
		requests[index].Sequence = index
	}
	batches := partitionRequests(requests, 4)
	if len(batches) != 4 {
		t.Fatalf("batches = %d, want 4", len(batches))
	}
	for clientIndex, batch := range batches {
		for itemIndex, request := range batch {
			wantSequence := clientIndex + itemIndex*len(batches)
			if request.Sequence != wantSequence {
				t.Fatalf("batch %d item %d sequence = %d, want %d", clientIndex, itemIndex, request.Sequence, wantSequence)
			}
		}
	}
	if got := len(batches[0]) + len(batches[1]) + len(batches[2]) + len(batches[3]); got != len(requests) {
		t.Fatalf("partitioned requests = %d, want %d", got, len(requests))
	}
}

func TestRunSmallFourClientBenchmark(t *testing.T) {
	result, err := run(context.Background(), config{Calls: 20, Warmup: 1, Clients: 4, Symbols: 100, Edges: 1_000, Seed: 42})
	if err != nil {
		t.Fatalf("run() error = %v", err)
	}
	if result.Clients != 4 || result.Workload.Calls != 20 {
		t.Fatalf("result = %#v", result)
	}
	if result.Metrics.Errors != 0 {
		t.Fatalf("metrics = %#v", result.Metrics)
	}
	for _, check := range result.SLOChecks {
		if !check.Passed {
			t.Fatalf("SLO check failed: %#v", check)
		}
	}
	totalCalls := 0
	for _, operation := range result.Operations {
		totalCalls += operation.Calls
	}
	if totalCalls != 20 {
		t.Fatalf("operation calls = %d, want 20", totalCalls)
	}
}

func TestRunWritesAllProfiles(t *testing.T) {
	profileDir := t.TempDir()
	_, err := run(context.Background(), config{
		Calls: 20, Warmup: 1, Clients: 4, Symbols: 100, Edges: 1_000, Seed: 42,
		ProfileDir: profileDir,
	})
	if err != nil {
		t.Fatalf("run() with profiles error = %v", err)
	}
	for _, name := range []string{"cpu.pprof", "heap.pprof", "allocs.pprof", "mutex.pprof", "block.pprof", "trace.out"} {
		info, err := os.Stat(filepath.Join(profileDir, name))
		if err != nil {
			t.Fatalf("profile %s: %v", name, err)
		}
		if info.Size() == 0 {
			t.Fatalf("profile %s is empty", name)
		}
	}
}
