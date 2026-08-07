# LadybugDB recovery benchmark — pinned rerun

- Command: `CGO_ENABLED=1 CGO_CFLAGS="-I$PWD/.tooling/ladybug/v0.13.1" CGO_LDFLAGS="-L$PWD/.tooling/ladybug/v0.13.1 -Wl,-rpath,$PWD/.tooling/ladybug/v0.13.1 -llbug" go run -tags ladybug ./benchmarks/ladybug-recovery --database /tmp/ladygraph-acceptance-bulk/graph.db --output benchmarks/ladybug-recovery-pinned --documentation benchmarks/ladybug-recovery-pinned/report.md`
- Commit: `45220d30c17d4521568dde6e1f8ae2aa4e367356`
- Generated at: `2026-08-07T13:51:37Z`
- Environment: `linux/amd64`, Go `1.24.4`, LadybugDB core and binding `v0.13.1`
- Input: private copy `/tmp/ladygraph-acceptance-bulk/graph.db`, `67.059.712` bytes
- Input SHA-256 before/after: `9d4964e299688657369a89212d00a23903dadd1d432710d4e4da8c64162b526c`

## Results

| Case | Duration ms | Result |
| --- | ---: | --- |
| `sigkill_during_insert` | 89.1 | pass |
| `sigkill_before_commit` | 61.2 | pass |
| `sigkill_during_bulk_load` | 252.2 | pass |
| `reopen_after_crash` | 111.0 | pass |
| `truncated_file` | 63.5 | pass |
| `permission_denied_directory` | 10.4 | pass |
| `simulated_disk_full` | 2528.7 | pass |
| `generation_publication_enospc` | 6110.1 | pass |

`source_unchanged: true` and `all_passed: true`.

The rerun verifies rollback of interrupted transactions and `COPY`, controlled
rejection of truncation and unwritable directories, preservation of `CURRENT`
and its digest under `ENOSPC`, and successful restoration after publication
faults. The input database and its source repository were not modified.

## Limitations

The probes cover Linux process termination and deterministic filesystem-call
faults, not machine power loss or storage-controller cache loss. The full-disk
case injects `ENOSPC` at the libc boundary for the copied database file. The
permission case assumes a non-root process.
