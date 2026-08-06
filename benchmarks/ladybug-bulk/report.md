# LadybugDB bulk load benchmark

- Command: `/home/devlabs/.cache/go-build/4e/4e7089d945d45446fdcdbb8c7d0e4a6f20f0be3e556fab46e503e44e0165f673-d/ladybug-bulk --corpus /tmp/ladygraph-synthetic-reduced --database /tmp/ladygraph-ladybug-copy-reduced-gated.db --output benchmarks/ladybug-bulk --individual-results benchmarks/ladybug-individual/results.json --batch-results benchmarks/ladybug-batch/results.json`
- Commit: `d599f721ff740e039c8af105d806a437bae209d7-dirty`
- Generated at: `2026-08-04T18:29:26Z`
- Corpus seed: `42`
- Full initial scale: `false`

## COPY result

| Nodes | Edges | CSV export ms | COPY ms | COPY records/s | End-to-end records/s | Peak RSS | Database bytes |
| ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: |
| 20040 | 100000 | 130.1 | 178.7 | 671567.7 | 388655.3 | 154759168 | 4276224 |

## Strategy comparison

| Strategy | Records | Records/s | Peak RSS | Comparable corpus |
| --- | ---: | ---: | ---: | :---: |
| COPY | 120040 | 671567.7 | 154759168 | true |
| CREATE | 120040 | 1254.9 | 107200512 | true |
| batch transaction (10000) | 120040 | 3729.8 | 1270525952 | true |

## Gate `LADYBUG_BULK_LOAD_PASS`

- Full initial scale: `false`
- Stored counts verified: `true`
- RSS within 2 GiB: `true`
- Result: `false`

COPY throughput excludes deterministic JSONL-to-CSV export. End-to-end throughput includes it. Stored node and relationship counts are verified before results are written.
