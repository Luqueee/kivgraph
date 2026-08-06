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
