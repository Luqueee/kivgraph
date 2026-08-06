# MCP benchmark with 4 clients

- Command: `go run ./benchmarks/mcp-client --clients 4 --calls 10000 --warmup 100 --symbols 100000 --edges 1000000 --seed 42 --output benchmarks/mcp-client-4`
- Commit: `1e23735b43f441af0a89897ecf074d55f9efd34b`
- Generated at: `2026-08-06T21:31:40Z`
- Environment: `linux/amd64`, `AMD Ryzen 7 9700X 8-Core Processor`, `24543904 kB`, `go1.24.4`
- Clients: 4 concurrent SDK sessions
- Dataset: `synthetic-mcp-client`, 100000 symbols, 1000000 edges, seed 42
- Workload: 10000 total calls, warm-up 100 calls per operation and client

## Overall metrics

| p50 round-trip ms | p95 round-trip ms | p99 round-trip ms | Throughput/s | Allocs/op | Bytes/op | RSS bytes | Goroutines | Errors | Memory growth |
| ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: | --- |
| 0.130 | 1.173 | 1.588 | 13359.9 | 2018.8 | 128898.5 | 500944896 | 17 | 0 | false |

## Per-operation metrics

| Operation | Calls | Backend p50 ms | Backend p95 ms | Backend p99 ms | Round-trip p50 ms | Round-trip p95 ms | Round-trip p99 ms | Errors |
| --- | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: |
| `find_symbol` | 4000 | 0.002 | 0.003 | 0.005 | 0.087 | 0.161 | 0.237 | 0 |
| `get_symbol` | 2500 | 0.001 | 0.001 | 0.001 | 0.104 | 0.185 | 0.247 | 0 |
| `find_references` | 2000 | 0.005 | 0.009 | 0.013 | 0.429 | 0.687 | 1.058 | 0 |
| `find_cross_repo_consumers` | 1000 | 0.008 | 0.016 | 0.027 | 0.496 | 0.771 | 1.169 | 0 |
| `get_blast_radius` | 500 | 0.037 | 0.079 | 0.191 | 1.274 | 2.032 | 3.109 | 0 |

## SLO comparison

The SLO comparison uses backend observer timings, excluding MCP transport and client serialization, as required by `docs/performance/slo.md`.

| Operation | Backend p95 ms | Limit | Backend p99 ms | Limit | Pass |
| --- | ---: | ---: | ---: | ---: | --- |
| `find_symbol` | 0.003 | 2.000 | 0.005 | 5.000 | true |
| `get_symbol` | 0.001 | 1.000 | 0.001 | 2.000 | true |
| `find_references` | 0.009 | 5.000 | 0.013 | 10.000 | true |
| `find_cross_repo_consumers` | 0.016 | 5.000 | 0.027 | 15.000 | true |
| `get_blast_radius` | 0.079 | 20.000 | 0.191 | 40.000 | true |

Overall SLO result: `true`.

Allocations/op and bytes/op are process-wide deltas over the measured mixed workload after warm-up. The 4 clients share one MCP server and snapshot. Repeat with the same command on target hardware before treating this result as a release gate.
The clients use the SDK in-memory transport; this result excludes stdio, socket and network overhead.
RSS is the process peak and includes synthetic corpus and HotSnapshot construction before measurement.
