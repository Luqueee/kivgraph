# MCP empty benchmark

- Command: `go run ./benchmarks/mcp-empty`
- Commit: `59a4c122fefc78f04dd7a572f5a2b392d03046c7-dirty`
- Calls per tool and scenario: 10000
- Generated at: `2026-08-04T16:57:56Z`

## Results

| Tool | Clients | Backend p50 ms | Backend p95 ms | Backend p99 ms | Round-trip p95 ms | Throughput/s | Allocs/op | Bytes/op | RSS | Goroutines | Errors |
| --- | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: |
| `graph_status` | 1 | 0.000 | 0.000 | 0.000 | 0.081 | 23787.2 | 239.5 | 10195.2 | 14610432 | 5 | 0 |
| `graph_status` | 4 | 0.000 | 0.000 | 0.000 | 0.095 | 81103.6 | 239.7 | 10251.2 | 15749120 | 17 | 0 |
| `graph_status` | 16 | 0.000 | 0.000 | 0.000 | 1.390 | 42521.4 | 242.9 | 10686.7 | 15773696 | 65 | 0 |
| `graph_status` | 32 | 0.000 | 0.000 | 0.000 | 3.534 | 36019.6 | 242.8 | 10751.6 | 17285120 | 129 | 0 |
| `list_repositories` | 1 | 0.000 | 0.000 | 0.000 | 0.080 | 25129.1 | 224.5 | 9679.8 | 16457728 | 5 | 0 |
| `list_repositories` | 4 | 0.000 | 0.000 | 0.000 | 0.108 | 75613.2 | 224.8 | 9814.9 | 16687104 | 17 | 0 |
| `list_repositories` | 16 | 0.000 | 0.000 | 0.000 | 1.617 | 37251.8 | 227.9 | 10382.8 | 16502784 | 65 | 0 |
| `list_repositories` | 32 | 0.000 | 0.000 | 0.000 | 3.678 | 33897.8 | 227.8 | 10450.5 | 16777216 | 129 | 0 |

## Gate `EMPTY_MCP_PERFORMANCE_PASS`

- p95 ≤ 2 ms: `true`
- 0 errores: `true`
- Sin crecimiento continuo de memoria detectado: `true`
- Resultado: `true`

Backend p50/p95/p99 measure only the registered handler body through an observer; round-trip p95 includes the in-memory MCP transport and JSON-RPC path. RSS and goroutines correspond to the benchmark process. Repeat the benchmark on target hardware before treating the gate as production evidence.
