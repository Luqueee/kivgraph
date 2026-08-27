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

// benchmarkCalls is the workload both benchmark tests measure, and it is a
// sample size before it is a duration. The gate reads `p95` and `p99` per
// operation, and nearest-rank makes each of them the maximum until there are
// twenty and one hundred calls respectively. The workload's smallest share is
// `get_blast_radius` at five per cent, so two thousand is the first count --
// and a multiple of twenty, which is what makes the shares exact -- that gives
// every one of the five operations enough calls for *both* its percentiles to
// discard at least one sample.
//
// Measured on the reference machine (Ryzen 7 9700X, 16 threads, Go 1.26.6),
// fifty repeats per count while thirty-two spinners held the load average
// near 37, which is the contended runner these limits kept failing on. Worst
// `p95` over fifty runs for `find_cross_repo_consumers`, against its 5 ms
// limit:
//
//	calls  n    worst p95   worst p99 (limit 15 ms)
//	   20  2      13,249       -
//	  200  20      5,029       -
//	  800  80      0,126       9,946
//	 2000  200     0,067       8,689
//
// The limits were never the problem: the median `p95` is 0,048 ms, a hundred
// times under the limit, and it does not move when the machine is loaded. Only
// the tail of a two-sample maximum moves. Raising the limit to cover 13,2 ms
// would have bought a green run by giving up the ability to see a real
// regression; this buys it by measuring. At two thousand calls no operation's
// `p95` or `p99` came within half its limit and no run failed the gate, in one
// hundred loaded runs across the two counts.
//
//	KIVGRAPH_BENCH_SLO=1 go test ./benchmarks/mcp-client/ -run TestRunSmall -count=1
//
// The cost is 1,5 s per run against 0,4 s at twenty calls. The corpus stays at
// a hundred symbols: what was too small was the number of calls, not the graph
// they run against.
const benchmarkCalls = 2_000

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
//
// Both failures had a second cause underneath the runner, and removing the
// unconditional assertion hid it rather than fixing it. `percentileMS` is
// nearest-rank, so `p95` over `n` calls reads index `ceil(0.95n)-1`: below
// twenty calls that index **is** `n-1` and the check compares the *slowest*
// call against the limit, not a percentile. These tests asked for twenty calls
// in total, and the workload gives `find_cross_repo_consumers` ten per cent of
// them -- so the number that failed the release at 11,6 ms and a pull request
// at 5,6 ms was the worse of *two* calls, and `get_blast_radius` gated on
// exactly *one*. `benchmarkCalls` is what makes the statistic a statistic; see
// its own comment for the measurements that chose it.
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
	result, err := run(context.Background(), config{Calls: benchmarkCalls, Warmup: 1, Symbols: 100, Edges: 1_000, Seed: 42})
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
	result, err := run(context.Background(), config{Calls: benchmarkCalls, Warmup: 1, Clients: 4, Symbols: 100, Edges: 1_000, Seed: 42})
	if err != nil {
		t.Fatalf("run() error = %v", err)
	}
	if result.Clients != 4 || result.Workload.Calls != benchmarkCalls {
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
	if totalCalls != benchmarkCalls {
		t.Fatalf("operation calls = %d, want %d", totalCalls, benchmarkCalls)
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
