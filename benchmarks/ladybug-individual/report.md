# LadybugDB individual insert benchmark

- Command: `/tmp/go-build1029130918/b001/exe/ladybug-individual --corpus /tmp/kivgraph-synthetic-reduced --database /tmp/kivgraph-ladybug-individual-reduced.db --output benchmarks/ladybug-individual --transaction-size 1000`
- Commit: `6d2edb5a28ffda42b564f5b2d02e135e30b44864-dirty`
- Generated at: `2026-08-04T17:37:48Z`
- Corpus seed: `42`
- Transaction size: `1000` records
- Full initial scale: `false`

## Results

| Nodes | Edges | Nodes/s | Edges/s | Records/s | Transactions | Commit ms | RSS | Database bytes |
| ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: |
| 20040 | 100000 | 2648.7 | 1135.2 | 1254.9 | 121 | 1900.4 | 107200512 | 7639040 |


This recorded run uses the agreed reduced corpus. The full 100,000-symbol/1,000,000-edge qualification is deferred to the batched and bulk loaders.

Each node and edge is executed as one prepared statement. The transaction size only controls commit boundaries; it does not batch records into a statement. This is a baseline for comparison with batched and bulk loading.
