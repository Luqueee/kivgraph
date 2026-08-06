package main

import (
	"context"
	"testing"
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
