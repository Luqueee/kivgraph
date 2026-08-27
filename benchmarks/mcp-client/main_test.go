package main

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
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

// reportSLOChecks reports the run's latency checks and gates on them only when
// `KIVGRAPH_BENCH_SLO` asks, which is what `benchmarks/AGENTS.md` requires of
// every environment-dependent gate.
//
// The SLO limits are a property of the machine, not of this code, and these
// tests run wherever `go test ./...` runs -- including a shared CI runner with
// three jobs on it. Asserting them made the v0.2.0 release fail on a p95 of
// 11,6 ms against a 5 ms limit, on the same commit whose CI run had just passed
// on another runner: the numbers were the runner's.
//
// It then happened a second time, because that reasoning was written into one
// of the two benchmark tests and not the other: the one-client test still
// asserted unconditionally and failed a documentation-only pull request at
// 5,6 ms against the same 5 ms limit, on `find_cross_repo_consumers` -- the same
// operation as v0.2.0. Both tests share this function so the two cannot drift
// apart again.
//
// What is asserted every time is that the measurement happened. A harness that
// silently stopped evaluating its checks would pass either way, and that is
// worse than an unmet limit.
func reportSLOChecks(t *testing.T, checks []sloCheck) {
	t.Helper()
	if len(checks) == 0 {
		t.Fatal("the run evaluated no SLO check, so there is nothing to report or to gate")
	}
	gate := os.Getenv("KIVGRAPH_BENCH_SLO") != ""
	for _, check := range unmetSLOChecks(checks) {
		if gate {
			t.Fatalf("SLO check failed: %#v", check)
		}
		t.Logf("SLO not met on this machine, which this test does not gate: %#v", check)
	}
}

// unmetSLOChecks is the decision reportSLOChecks acts on, split out because it
// is the half that can be tested: `testing.TB` carries an unexported method, so
// no fake can be handed to reportSLOChecks to observe what it does with a check
// that did not pass.
func unmetSLOChecks(checks []sloCheck) []sloCheck {
	var unmet []sloCheck
	for _, check := range checks {
		if !check.Passed {
			unmet = append(unmet, check)
		}
	}
	return unmet
}

func TestUnmetSLOChecks(t *testing.T) {
	failed := sloCheck{Operation: "find_cross_repo_consumers", P95LimitMS: 5, P95MS: 5.563282}
	passed := sloCheck{Operation: "find_references", P95LimitMS: 5, P95MS: 1.2, Passed: true}

	for _, testCase := range []struct {
		name   string
		checks []sloCheck
		want   []sloCheck
	}{
		{name: "no checks at all", checks: nil, want: nil},
		{name: "every check passed", checks: []sloCheck{passed, passed}, want: nil},
		{name: "one check missed its limit", checks: []sloCheck{passed, failed}, want: []sloCheck{failed}},
		{name: "every check missed its limit", checks: []sloCheck{failed, failed}, want: []sloCheck{failed, failed}},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			if got := unmetSLOChecks(testCase.checks); !reflect.DeepEqual(got, testCase.want) {
				t.Fatalf("unmetSLOChecks() = %#v, want %#v", got, testCase.want)
			}
		})
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
	reportSLOChecks(t, result.SLOChecks)
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
	reportSLOChecks(t, result.SLOChecks)
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
