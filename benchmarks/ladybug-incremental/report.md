# LadybugDB incremental update probes

- Command: `/tmp/go-build1461275480/b001/exe/ladybug-incremental --database /tmp/luque-ladybug-copy-full-gated.db --corpus /tmp/luque-synthetic-42 --output benchmarks/ladybug-incremental`
- Commit: `23de693271f3a9e354b488785e687e8b0d21007d-dirty`
- Generated at: `2026-08-04T19:09:02Z`
- Platform: `linux/amd64`, `go1.24.4`
- Corpus: seed 42, 100000 symbols, 1000000 edges
- Base database bytes: `43065344`

| Probe | Duration ms | Added symbols | Updated symbols | Deleted symbols | Added references | Deleted references | Replaced sources | Expected failure |
| --- | ---: | ---: | ---: | ---: | ---: | ---: | ---: | --- |
| add_1_symbol | 13.979 | 1 | 0 | 0 | 0 | 0 | 0 |  |
| add_1000_symbols | 4731.657 | 1000 | 0 | 0 | 0 | 0 | 0 |  |
| add_edges | 864.600 | 0 | 0 | 0 | 3 | 0 | 0 |  |
| delete_edges | 7.128 | 0 | 0 | 0 | 0 | 1 | 0 |  |
| update_properties | 7.299 | 0 | 1 | 0 | 0 | 0 | 0 |  |
| replace_outgoing | 799.309 | 0 | 0 | 0 | 2 | 2 | 1 |  |
| delete_symbol | 109.805 | 0 | 0 | 1 | 0 | 1 | 0 |  |
| rollback_after_late_failure | 400.355 | 0 | 0 | 0 | 0 | 0 | 0 | ErrNotFound |

## Integrity

- Duplicate symbols rejected: `true`
- Duplicate references rejected: `true`
- No ghost edges: `true`
- Atomicity verified: `true`
- Rollback verified: `true`
- Result: `true`

The probe sequence runs against one temporary copy of the full synthetic LadybugDB database. Timings cover the transactional database mutation only; HotSnapshot construction and publication are not implemented in this phase.
