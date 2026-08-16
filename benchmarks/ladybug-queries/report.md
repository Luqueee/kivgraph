# LadybugDB direct query benchmark

- Command: `/tmp/go-build1319215104/b001/exe/ladybug-queries --database /tmp/kivgraph-ladybug-qualification.db --corpus /tmp/kivgraph-synthetic-42 --output benchmarks/ladybug-queries --iterations 100 --warmup 5`
- Commit: `e902dd0d56563cd3b4d71c2ac19ca28caf955824-dirty`
- Generated at: `2026-08-04T20:20:00Z`
- Platform: `linux/amd64`, `go1.24.4`
- Corpus: seed 42, 40 repositories, 100000 symbols, 1000000 edges
- Calls: 100 measured + 5 warm-up per operation

| Operation | p50 us | p95 us | p99 us | max us | calls/s | avg returned | errors |
| --- | ---: | ---: | ---: | ---: | ---: | ---: | ---: |
| get_symbol | 12723.3 | 13591.0 | 13852.7 | 13988.2 | 78.2 | 1.0 | 0 |
| incoming_references_100 | 153948.8 | 158275.5 | 160652.4 | 166901.9 | 6.5 | 100.0 | 0 |
| outgoing_references_100 | 153810.5 | 158395.1 | 161524.0 | 166639.3 | 6.5 | 100.0 | 0 |
| traverse_depth_3_100 | 45219.1 | 46671.1 | 48360.3 | 48852.7 | 22.1 | 100.0 | 0 |
| traverse_depth_5_100 | 45442.8 | 46802.3 | 47077.4 | 47294.4 | 22.0 | 100.0 | 0 |
| shortest_path_depth_5 | 83634.6 | 85385.1 | 86813.7 | 87204.4 | 11.9 | 2.0 | 0 |
| incoming_by_repository | 29573.3 | 31350.8 | 33288.8 | 33354.3 | 33.6 | 10.0 | 0 |

Golden probes validate stable-key lookup, both reference directions, depth-3 and depth-5 reachability, shortest path endpoints, and deterministic repository grouping before measurement.

These are direct LadybugDB measurements. They do not qualify the HotSnapshot MCP SLOs, which are measured in later phases.
