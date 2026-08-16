# MCP STDIO transport benchmark

- Command: go run ./benchmarks/mcp-stdio --server ./kivgraph --config benchmarks/mcp-stdio/testdata/config.yaml --calls 10000 --warmup 100 --output benchmarks/mcp-stdio
- Commit: 4580240a155940cadf9e230626d42e8879a3e337
- Generated at: 2026-08-07T14:26:07Z
- Environment: linux/amd64, AMD Ryzen 7 9700X 8-Core Processor, 24543908 kB, Go go1.24.4
- Transport: subprocess over newline-delimited JSON on stdin/stdout
- Server config: benchmarks/mcp-stdio/testdata/config.yaml
- Workload: 100 warm-up calls and 10000 measured graph_status calls

## Results

| Metric | Result |
| --- | ---: |
| Protocol version | 2025-06-18 |
| Registered tools | 9 |
| p50 round-trip | 0.269231 ms |
| p95 round-trip | 0.362711 ms |
| p99 round-trip | 0.573922 ms |
| Minimum | 0.214761 ms |
| Maximum | 1.996267 ms |
| Throughput | 3520.665 calls/s |
| Errors | 0 |
| Server RSS sampled maximum | 19075072 bytes |
| Server exit code | 0 |

graph_status was called against an empty published snapshot, so every measured
call was a successful status response. The client completed initialization,
tools/list, 100 warm-ups and the measured workload over the real Kivgraph STDIO
transport. Server logs remained on stderr; no protocol bytes were written there.

## SLO interpretation

docs/performance/slo.md defines limits for backend query handlers and does not
define a transport limit. This artifact is therefore evidence for the STDIO
path, not a new PASS gate. It excludes sockets and network transports, which are
not configured by the current Kivgraph server.
