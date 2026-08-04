# LadybugDB direct query benchmark

- Command: `/tmp/go-build2190362627/b001/exe/ladybug-queries --database /tmp/luque-ladybug-copy-full-gated.db --corpus /tmp/luque-synthetic-42 --output benchmarks/ladybug-queries --iterations 100 --warmup 5`
- Commit: `a1454bc7f3ca3481233b38d64067305b8c2bf933-dirty`
- Generated at: `2026-08-04T18:48:47Z`
- Platform: `linux/amd64`, `go1.24.4`
- Corpus: seed 42, 40 repositories, 100000 symbols, 1000000 edges
- Calls: 100 measured + 5 warm-up per operation

| Operation | p50 us | p95 us | p99 us | max us | calls/s | avg returned | errors |
| --- | ---: | ---: | ---: | ---: | ---: | ---: | ---: |
| get_symbol | 12658.0 | 13305.0 | 13711.9 | 14229.1 | 78.7 | 1.0 | 0 |
| incoming_references_100 | 142397.0 | 145724.8 | 148109.6 | 151557.4 | 7.0 | 100.0 | 0 |
| outgoing_references_100 | 141971.0 | 146744.9 | 147466.8 | 150958.4 | 7.0 | 100.0 | 0 |
| traverse_depth_3_100 | 45425.2 | 46657.7 | 47371.1 | 48526.4 | 21.9 | 100.0 | 0 |
| traverse_depth_5_100 | 45582.0 | 47088.8 | 47335.3 | 47507.0 | 21.9 | 100.0 | 0 |
| shortest_path_depth_5 | 83154.5 | 85451.1 | 86297.2 | 87478.1 | 12.0 | 2.0 | 0 |
| incoming_by_repository | 30522.4 | 33590.3 | 39003.9 | 39526.6 | 32.4 | 10.0 | 0 |

Golden probes validate stable-key lookup, both reference directions, depth-3 and depth-5 reachability, shortest path endpoints, and deterministic repository grouping before measurement.

These are direct LadybugDB measurements. They do not qualify the HotSnapshot MCP SLOs, which are measured in later phases.
