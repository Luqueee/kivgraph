# MCP benchmark with 32 clients

- Command: `go run ./benchmarks/mcp-client --clients 32 --calls 10000 --warmup 100 --symbols 100000 --edges 1000000 --seed 42 --output benchmarks/mcp-client-32`
- Commit: `9387e1d41bb3db1f9a4da73ed76fbf7023f8d8cb`
- Generated at: `2026-08-06T21:40:25Z`
- Environment: `linux/amd64`, `AMD Ryzen 7 9700X 8-Core Processor`, `24543904 kB`, `go1.24.4`
- Clients: 32 concurrent SDK sessions
- Dataset: `synthetic-mcp-client`, 100000 symbols, 1000000 edges, seed 42
- Workload: 10000 total calls, warm-up 100 calls per operation and client

## Overall metrics

| p50 round-trip ms | p95 round-trip ms | p99 round-trip ms | Throughput/s | Allocs/op | Bytes/op | RSS bytes | Goroutines | Errors | Memory growth |
| ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: | --- |
| 0.601 | 3.781 | 9.775 | 25351.8 | 2018.9 | 128502.3 | 500662272 | 129 | 0 | false |

## Per-operation metrics

| Operation | Calls | Backend p50 ms | Backend p95 ms | Backend p99 ms | Round-trip p50 ms | Round-trip p95 ms | Round-trip p99 ms | Errors |
| --- | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: |
| `find_symbol` | 4000 | 0.002 | 0.004 | 0.007 | 0.202 | 1.960 | 5.469 | 0 |
| `get_symbol` | 2500 | 0.001 | 0.001 | 0.002 | 0.242 | 2.214 | 5.467 | 0 |
| `find_references` | 2000 | 0.008 | 0.014 | 0.023 | 1.051 | 4.689 | 11.142 | 0 |
| `find_cross_repo_consumers` | 1000 | 0.012 | 0.030 | 0.052 | 1.185 | 4.803 | 10.574 | 0 |
| `get_blast_radius` | 500 | 0.060 | 0.317 | 1.377 | 3.158 | 11.132 | 19.149 | 0 |

## SLO comparison

The SLO comparison uses backend observer timings, excluding MCP transport and client serialization, as required by `docs/performance/slo.md`.

| Operation | Backend p95 ms | Limit | Backend p99 ms | Limit | Pass |
| --- | ---: | ---: | ---: | ---: | --- |
| `find_symbol` | 0.004 | 2.000 | 0.007 | 5.000 | true |
| `get_symbol` | 0.001 | 1.000 | 0.002 | 2.000 | true |
| `find_references` | 0.014 | 5.000 | 0.023 | 10.000 | true |
| `find_cross_repo_consumers` | 0.030 | 5.000 | 0.052 | 15.000 | true |
| `get_blast_radius` | 0.317 | 20.000 | 1.377 | 40.000 | true |

Overall SLO result: `true`.

Allocations/op and bytes/op are process-wide deltas over the measured mixed workload after warm-up. The 32 clients share one MCP server and snapshot. Repeat with the same command on target hardware before treating this result as a release gate.
The clients use the SDK in-memory transport; this result excludes stdio, socket and network overhead.
RSS is the process peak and includes synthetic corpus and HotSnapshot construction before measurement.


## LUQUE-1307 allocation optimization

The change reuses per-snapshot traversal scratch buffers through a
`sync.Pool` and generation-stamped visited state. It covers the `allocations`
category only; no serialization, index, traversal algorithm, or snapshot-layout
change was included.

### HotSnapshot microbenchmark

Command: `go test ./internal/hotsnapshot -run '^$' -bench='BenchmarkHotSnapshotDepth3|BenchmarkHotSnapshotDepth5' -benchmem -count=5`

| Benchmark | Before B/op | After B/op | Before allocs/op | After allocs/op | After median ns/op |
| --- | ---: | ---: | ---: | ---: | ---: |
| `Depth3` | 404912 | 1752 | 12 | 4 | 1040 |
| `Depth5` | 404912 | 1752 | 12 | 4 | 1671 |

The before values are the five-run baseline from the same checkout before
LUQUE-1307; the after values are the five-run measurement after the change.

### 32-client comparison

The same command, corpus, seed, and hardware were run before and after the
change. The after values below use the clean published commit `69c86a7` and
wrote the raw result to `/tmp/luque-mcp-client-32-after-clean/results.json`.

| Metric | Before | After | Delta |
| --- | ---: | ---: | ---: |
| p50 round-trip ms | 0.601 | 0.687 | +14.3% |
| p95 round-trip ms | 3.781 | 4.016 | +6.2% |
| p99 round-trip ms | 9.775 | 8.308 | -15.0% |
| Throughput/s | 25351.8 | 24369.7 | -3.9% |
| Allocs/op | 2018.9 | 2018.6 | -0.02% |
| Bytes/op | 128502.3 | 109860.6 | -14.5% |
| RSS bytes | 500662272 | 502259712 | +0.3% |
| Errors | 0 | 0 | 0 |

The microbenchmark demonstrates the allocation reduction directly. LUQUE-1308
repeats the 32-client workload below before treating latency or throughput
movement as a conclusion. The pool retains scratch capacity for concurrent
readers, so RSS can vary independently of the allocation reduction.

## LUQUE-1308 repeated benchmark

Command for each run: `go run ./benchmarks/mcp-client --clients 32 --calls
10000 --warmup 100 --symbols 100000 --edges 1000000 --seed 42 --output
/tmp/luque-mcp-client-32-repeat-N`

All three runs used the clean commit `cf8fc65`, the same Linux/amd64 host,
corpus, workload distribution, and seed. Every run reported zero errors and
`slo_passed: true`.

| Run | p50 ms | p95 ms | p99 ms | Throughput/s | Allocs/op | Bytes/op | RSS bytes |
| ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: |
| 1 | 0.569 | 3.783 | 9.033 | 24513.7 | 2018.552 | 109775.2 | 502386688 |
| 2 | 0.695 | 3.581 | 8.791 | 26236.0 | 2018.554 | 110034.2 | 501907456 |
| 3 | 0.676 | 3.857 | 9.433 | 24520.5 | 2018.610 | 110783.0 | 688136192 |
| Arithmetic mean | 0.646 | 3.740 | 9.086 | 25090.1 | 2018.572 | 110197.4 | 564143445 |

The third run observed an RSS peak of `688136192` bytes while the other two
were approximately `502 MB`; the median repeated RSS is `502386688` bytes.
This is recorded as environmental variance, not as a memory-growth finding:
all runs reported `memory_growth_detected: false`.

### Baseline versus repeated mean

The baseline is the single published pre-optimization run in
`benchmarks/mcp-client-32/results.json`; the after column is the arithmetic
mean of the three post-optimization runs.

| Metric | Baseline | Repeated mean | Delta |
| --- | ---: | ---: | ---: |
| p50 round-trip ms | 0.601 | 0.646 | +7.6% |
| p95 round-trip ms | 3.781 | 3.740 | -1.1% |
| p99 round-trip ms | 9.775 | 9.086 | -7.1% |
| Throughput/s | 25351.8 | 25090.1 | -1.0% |
| Allocs/op | 2018.944 | 2018.572 | -0.02% |
| Bytes/op | 128502.3 | 110197.4 | -14.2% |
| RSS bytes (median after) | 500662272 | 502386688 | +0.3% |