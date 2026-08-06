# MCP benchmark with 16 clients

- Command: `go run ./benchmarks/mcp-client --clients 16 --calls 10000 --warmup 100 --symbols 100000 --edges 1000000 --seed 42 --output benchmarks/mcp-client-16`
- Commit: `a2b1923b62800bc36a13e877b09438eb33bcd168`
- Generated at: `2026-08-06T21:34:40Z`
- Environment: `linux/amd64`, `AMD Ryzen 7 9700X 8-Core Processor`, `24543904 kB`, `go1.24.4`
- Clients: 16 concurrent SDK sessions
- Dataset: `synthetic-mcp-client`, 100000 symbols, 1000000 edges, seed 42
- Workload: 10000 total calls, warm-up 100 calls per operation and client

## Overall metrics

| p50 round-trip ms | p95 round-trip ms | p99 round-trip ms | Throughput/s | Allocs/op | Bytes/op | RSS bytes | Goroutines | Errors | Memory growth |
| ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: | --- |
| 0.191 | 2.271 | 4.760 | 24287.8 | 2018.9 | 128828.2 | 500953088 | 65 | 0 | false |

## Per-operation metrics

| Operation | Calls | Backend p50 ms | Backend p95 ms | Backend p99 ms | Round-trip p50 ms | Round-trip p95 ms | Round-trip p99 ms | Errors |
| --- | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: |
| `find_symbol` | 4000 | 0.002 | 0.004 | 0.007 | 0.126 | 0.403 | 1.825 | 0 |
| `get_symbol` | 2500 | 0.001 | 0.002 | 0.002 | 0.153 | 0.445 | 2.636 | 0 |
| `find_references` | 2000 | 0.008 | 0.014 | 0.029 | 0.742 | 2.101 | 4.932 | 0 |
| `find_cross_repo_consumers` | 1000 | 0.012 | 0.033 | 0.058 | 0.856 | 2.557 | 5.678 | 0 |
| `get_blast_radius` | 500 | 0.056 | 0.203 | 0.561 | 2.307 | 5.690 | 8.651 | 0 |

## SLO comparison

The SLO comparison uses backend observer timings, excluding MCP transport and client serialization, as required by `docs/performance/slo.md`.

| Operation | Backend p95 ms | Limit | Backend p99 ms | Limit | Pass |
| --- | ---: | ---: | ---: | ---: | --- |
| `find_symbol` | 0.004 | 2.000 | 0.007 | 5.000 | true |
| `get_symbol` | 0.002 | 1.000 | 0.002 | 2.000 | true |
| `find_references` | 0.014 | 5.000 | 0.029 | 10.000 | true |
| `find_cross_repo_consumers` | 0.033 | 5.000 | 0.058 | 15.000 | true |
| `get_blast_radius` | 0.203 | 20.000 | 0.561 | 40.000 | true |

Overall SLO result: `true`.

Allocations/op and bytes/op are process-wide deltas over the measured mixed workload after warm-up. The 16 clients share one MCP server and snapshot. Repeat with the same command on target hardware before treating this result as a release gate.
The clients use the SDK in-memory transport; this result excludes stdio, socket and network overhead.
RSS is the process peak and includes synthetic corpus and HotSnapshot construction before measurement.
