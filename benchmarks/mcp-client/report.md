# MCP one-client benchmark

- Command: `go run ./benchmarks/mcp-client --calls 10000 --warmup 100 --symbols 100000 --edges 1000000 --seed 42 --output benchmarks/mcp-client`
- Commit: `039645de0d527f661b803a5589cb89be14ac2f37`
- Generated at: `2026-08-06T21:22:17Z`
- Environment: `linux/amd64`, `AMD Ryzen 7 9700X 8-Core Processor`, `24543904 kB`, `go1.24.4`
- Dataset: `synthetic-mcp-client`, 100000 symbols, 1000000 edges, seed 42
- Workload: 10000 calls, warm-up 100 calls per operation

## Overall metrics

| p50 round-trip ms | p95 round-trip ms | p99 round-trip ms | Throughput/s | Allocs/op | Bytes/op | RSS bytes | Goroutines | Errors | Memory growth |
| ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: | --- |
| 0.127 | 1.186 | 1.327 | 3532.9 | 2018.8 | 128477.3 | 500506624 | 5 | 0 | false |

## Per-operation metrics

| Operation | Calls | Backend p50 ms | Backend p95 ms | Backend p99 ms | Round-trip p50 ms | Round-trip p95 ms | Round-trip p99 ms | Errors |
| --- | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: |
| `find_symbol` | 4000 | 0.002 | 0.003 | 0.004 | 0.098 | 0.154 | 0.186 | 0 |
| `get_symbol` | 2500 | 0.001 | 0.001 | 0.001 | 0.118 | 0.174 | 0.203 | 0 |
| `find_references` | 2000 | 0.005 | 0.008 | 0.013 | 0.444 | 0.629 | 0.707 | 0 |
| `find_cross_repo_consumers` | 1000 | 0.008 | 0.014 | 0.022 | 0.509 | 0.681 | 0.837 | 0 |
| `get_blast_radius` | 500 | 0.036 | 0.052 | 0.063 | 1.251 | 1.545 | 2.100 | 0 |

## SLO comparison

The SLO comparison uses backend observer timings, excluding MCP transport and client serialization, as required by `docs/performance/slo.md`.

| Operation | Backend p95 ms | Limit | Backend p99 ms | Limit | Pass |
| --- | ---: | ---: | ---: | ---: | --- |
| `find_symbol` | 0.003 | 2.000 | 0.004 | 5.000 | true |
| `get_symbol` | 0.001 | 1.000 | 0.001 | 2.000 | true |
| `find_references` | 0.008 | 5.000 | 0.013 | 10.000 | true |
| `find_cross_repo_consumers` | 0.014 | 5.000 | 0.022 | 15.000 | true |
| `get_blast_radius` | 0.052 | 20.000 | 0.063 | 40.000 | true |

Overall SLO result: `true`.

Allocations/op and bytes/op are process-wide deltas over the measured mixed workload after warm-up. Repeat with the same command on target hardware before treating this one-client result as a release gate.
The client uses the SDK in-memory transport; this result excludes stdio, socket and network overhead.
RSS is the process peak and includes synthetic corpus and HotSnapshot construction before measurement.
