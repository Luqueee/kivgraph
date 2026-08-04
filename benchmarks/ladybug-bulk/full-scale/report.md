# LadybugDB bulk load benchmark

- Command: `/home/devlabs/.cache/go-build/4e/4e7089d945d45446fdcdbb8c7d0e4a6f20f0be3e556fab46e503e44e0165f673-d/ladybug-bulk --corpus /tmp/luque-synthetic-42 --database /tmp/luque-ladybug-qualification.db --output benchmarks/ladybug-bulk/full-scale --individual-results benchmarks/ladybug-individual/results.json --batch-results benchmarks/ladybug-batch/results.json`
- Commit: `e902dd0d56563cd3b4d71c2ac19ca28caf955824`
- Generated at: `2026-08-04T20:19:50Z`
- Corpus seed: `42`
- Full initial scale: `true`

## COPY result

| Nodes | Edges | CSV export ms | COPY ms | COPY records/s | End-to-end records/s | Peak RSS | Database bytes |
| ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: |
| 200040 | 1000000 | 1274.1 | 1793.5 | 669100.3 | 391195.8 | 542978048 | 43290624 |

## Strategy comparison

| Strategy | Records | Records/s | Peak RSS | Comparable corpus |
| --- | ---: | ---: | ---: | :---: |
| COPY | 1200040 | 669100.3 | 542978048 | true |
| CREATE | 120040 | 1254.9 | 107200512 | false |
| batch transaction (10000) | 120040 | 3729.8 | 1270525952 | false |

## Gate `LADYBUG_BULK_LOAD_PASS`

- Full initial scale: `true`
- Stored counts verified: `true`
- RSS within 2 GiB: `true`
- Result: `true`

COPY throughput excludes deterministic JSONL-to-CSV export. End-to-end throughput includes it. Stored node and relationship counts are verified before results are written.
