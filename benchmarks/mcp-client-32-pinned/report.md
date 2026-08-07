# MCP benchmark with 32 clients — rerun pinned

- Command: `go run ./benchmarks/mcp-client --clients 32 --calls 10000 --warmup 100 --symbols 100000 --edges 1000000 --seed 42 --output benchmarks/mcp-client-32-pinned`
- Commit: `45220d30c17d4521568dde6e1f8ae2aa4e367356`
- Generated at: `2026-08-07T13:52:02Z`
- Environment: `linux/amd64`, `AMD Ryzen 7 9700X 8-Core Processor`, `24543908 kB`, `go1.24.4`
- Clients: 32 concurrent SDK sessions
- Dataset: `synthetic-mcp-client`, 100000 symbols, 1000000 edges, seed 42
- Workload: 10000 total calls, warm-up 100 calls per operation and client

## Overall metrics

| p50 round-trip ms | p95 round-trip ms | p99 round-trip ms | Throughput/s | Allocs/op | Bytes/op | RSS bytes | Goroutines | Errors | Memory growth |
| ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: | --- |
| 0.509 | 3.352 | 8.027 | 26267.3 | 2018.6 | 110009.4 | 537911296 | 129 | 0 | false |

## Per-operation metrics

| Operation | Calls | Backend p50 ms | Backend p95 ms | Backend p99 ms | Round-trip p50 ms | Round-trip p95 ms | Round-trip p99 ms | Errors |
| --- | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: |
| `find_symbol` | 4000 | 0.002 | 0.004 | 0.007 | 0.183 | 1.915 | 4.713 | 0 |
| `get_symbol` | 2500 | 0.001 | 0.001 | 0.002 | 0.227 | 2.074 | 4.385 | 0 |
| `find_references` | 2000 | 0.008 | 0.014 | 0.032 | 0.985 | 4.085 | 9.731 | 0 |
| `find_cross_repo_consumers` | 1000 | 0.012 | 0.030 | 0.052 | 1.105 | 4.117 | 8.078 | 0 |
| `get_blast_radius` | 500 | 0.043 | 0.090 | 0.786 | 2.747 | 8.865 | 18.176 | 0 |

## SLO comparison

The SLO comparison uses backend observer timings, excluding MCP transport and
client serialization, as required by `docs/performance/slo.md`.

| Operation | Backend p95 ms | Limit | Backend p99 ms | Limit | Pass |
| --- | ---: | ---: | ---: | ---: | --- |
| `find_symbol` | 0.004 | 2.000 | 0.007 | 5.000 | true |
| `get_symbol` | 0.001 | 1.000 | 0.002 | 2.000 | true |
| `find_references` | 0.014 | 5.000 | 0.032 | 10.000 | true |
| `find_cross_repo_consumers` | 0.030 | 5.000 | 0.052 | 15.000 | true |
| `get_blast_radius` | 0.090 | 20.000 | 0.786 | 40.000 | true |

Overall SLO result: `true`.

Allocations/op and bytes/op are process-wide deltas over the measured mixed
workload after warm-up. The 32 clients share one MCP server and snapshot. The
clients use the SDK in-memory transport; this result excludes STDIO, socket and
network overhead. RSS is the process peak and includes synthetic corpus and
HotSnapshot construction before measurement. This rerun validates the pinned
source commit; it does not establish a network-transport SLO.
