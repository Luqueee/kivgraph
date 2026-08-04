# LadybugDB bulk load benchmark

- Command: `/tmp/go-build1913071936/b001/exe/ladybug-bulk --corpus /tmp/luque-synthetic-42 --database /tmp/luque-ladybug-copy-full-gated.db --output benchmarks/ladybug-bulk/full-scale --individual-results benchmarks/ladybug-individual/results.json --batch-results benchmarks/ladybug-batch/results.json`
- Commit: `d599f721ff740e039c8af105d806a437bae209d7-dirty`
- Generated at: `2026-08-04T18:29:15Z`
- Corpus seed: `42`
- Full initial scale: `true`

## COPY result

| Nodes | Edges | CSV export ms | COPY ms | COPY records/s | End-to-end records/s | Peak RSS | Database bytes |
| ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: |
| 200040 | 1000000 | 1277.6 | 1800.2 | 666615.5 | 389908.1 | 532602880 | 43065344 |

## Strategy comparison

| Strategy | Records | Records/s | Peak RSS | Comparable corpus |
| --- | ---: | ---: | ---: | :---: |
| COPY | 1200040 | 666615.5 | 532602880 | true |
| CREATE | 120040 | 1254.9 | 107200512 | false |
| batch transaction (10000) | 120040 | 3729.8 | 1270525952 | false |

## Gate `LADYBUG_BULK_LOAD_PASS`

- Full initial scale: `true`
- Stored counts verified: `true`
- RSS within 2 GiB: `true`
- Result: `true`

COPY throughput excludes deterministic JSONL-to-CSV export. End-to-end throughput includes it. Stored node and relationship counts are verified before results are written.
