# LadybugDB batch insert benchmark

- Command: `/home/devlabs/.cache/go-build/5f/5f66bb20e5127104195eea41c98693406959dd04c51b9708e2c076ddccc0d28f-d/ladybug-batch --corpus /tmp/luque-synthetic-reduced --database-dir /tmp/luque-ladybug-batch-isolated-final --output benchmarks/ladybug-batch --batch-sizes 100,1000,10000,50000`
- Commit: `cf5e17a108422f06e1a9cf5fc1475642ebab6d80-dirty`
- Generated at: `2026-08-04T18:00:23Z`
- Corpus seed: `42`
- Full initial scale: `false`
- Recommended batch size under the 2 GiB RSS limit: `10000`

| Batch | Nodes/s | Edges/s | Records/s | Transactions | Commit ms | Peak RSS | Database bytes |
| ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: |
| 100 | 3818.7 | 2499.8 | 2652.7 | 1202 | 2189.5 | 181788672 | 7639040 |
| 1000 | 4462.1 | 3034.2 | 3205.4 | 122 | 1878.4 | 303104000 | 7639040 |
| 10000 | 25253.2 | 3185.7 | 3729.8 | 14 | 1424.6 | 1270525952 | 7716864 |
| 50000 | 24670.5 | 3332.0 | 3894.3 | 7 | 118.3 | 2166947840 | 4767744 |

Batch 10,000 is the best measured throughput under the 2 GiB RSS qualification limit. Batch 50,000 improves aggregate throughput by 4.4% but reaches 2.17 GB RSS. Its full-scale attempt exceeded the 600-second limit and produced no qualification result.

Each batch is one `UNWIND $rows` prepared statement inside one explicit transaction. Database counts are verified after every scenario.
