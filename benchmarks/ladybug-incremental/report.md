# LadybugDB incremental update probes

- Command: `/tmp/go-build956227063/b001/exe/ladybug-incremental --database /tmp/luque-ladybug-qualification.db --corpus /tmp/luque-synthetic-42 --output benchmarks/ladybug-incremental`
- Commit: `e902dd0d56563cd3b4d71c2ac19ca28caf955824-dirty`
- Generated at: `2026-08-04T20:21:04Z`
- Platform: `linux/amd64`, `go1.24.4`
- Corpus: seed 42, 100000 symbols, 1000000 edges
- Base database bytes: `43290624`

| Probe | Duration ms | Added symbols | Updated symbols | Deleted symbols | Added references | Deleted references | Replaced sources | Expected failure |
| --- | ---: | ---: | ---: | ---: | ---: | ---: | ---: | --- |
| add_1_symbol | 14.040 | 1 | 0 | 0 | 0 | 0 | 0 |  |
| add_1000_symbols | 4748.766 | 1000 | 0 | 0 | 0 | 0 | 0 |  |
| add_edges | 878.656 | 0 | 0 | 0 | 3 | 0 | 0 |  |
| delete_edges | 7.748 | 0 | 0 | 0 | 0 | 1 | 0 |  |
| update_properties | 7.181 | 0 | 1 | 0 | 0 | 0 | 0 |  |
| replace_outgoing | 822.885 | 0 | 0 | 0 | 2 | 2 | 1 |  |
| delete_symbol | 113.111 | 0 | 0 | 1 | 0 | 1 | 0 |  |
| rollback_after_late_failure | 403.419 | 0 | 0 | 0 | 0 | 0 | 0 | ErrNotFound |

## Integrity

- Duplicate symbols rejected: `true`
- Duplicate references rejected: `true`
- No ghost edges: `true`
- Atomicity verified: `true`
- Rollback verified: `true`
- Result: `true`

The probe sequence runs against one temporary copy of the full synthetic LadybugDB database. Timings cover the transactional database mutation only; HotSnapshot construction and publication are not implemented in this phase.
