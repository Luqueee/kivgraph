# Storage and graph benchmarks

Every command here reads or copies an artifact; none of them modifies an indexed
repository or a published generation. They were the qualification of LadybugDB as
the canonical store, and they are kept because the invocations are not recorded
anywhere else: a benchmark report carries its numbers, its environment and its
limitations, not the command line that produced it.

Results live in `benchmarks/<name>/`, each with its own `results.json` and
`report.md`.

## The synthetic corpus

The generator creates a reproducible JSON Lines corpus for storage benchmarks:

```bash
go run ./cmd/kivgraph benchmark generate-graph \
  --symbols 100000 \
  --edges 1000000 \
  --seed 42
```

By default, it generates 40 repositories, 100,000 files, 100,000 symbols, and 1,000,000 edges in `testdata/generated/synthetic`. `--repositories`, `--files`, and `--output` can override these values. The directory contains `repositories.jsonl`, `files.jsonl`, `symbols.jsonl`, `edges.jsonl`, and a `manifest.json` with the graph counts and controlled structures.

The individual reference load requires the native LadybugDB library and executes one prepared statement per node or edge:

```bash
go run -tags ladybug ./benchmarks/ladybug-individual \
  --corpus testdata/generated/synthetic \
  --database /tmp/kivgraph-individual.db \
  --transaction-size 1000
```

The transaction size only controls commits; it does not group records into a single statement. Results are written to `benchmarks/ladybug-individual`.

The batched variant uses an `UNWIND $rows` statement and one transaction per batch. It compares the batch sizes required by the plan:

```bash
go run -tags ladybug ./benchmarks/ladybug-batch \
  --corpus testdata/generated/synthetic \
  --database-dir /tmp/kivgraph-batch \
  --batch-sizes 100,1000,10000,50000
```

Each scenario uses a new database and verifies the stored counts before recording throughput, commit time, peak RSS, and disk size. The recorded comparison recommends 10,000 records per batch under the 2 GiB RSS limit. Scenarios run in separate processes so their memory measurements do not contaminate one another.

The bulk load through `COPY` exports the corpus to temporary CSV files and executes one `COPY` operation per table. At the initial full scale, 200,040 nodes and 1,000,000 edges were verified:

```bash
go run -tags ladybug ./benchmarks/ladybug-bulk \
  --corpus testdata/generated/synthetic \
  --database /tmp/kivgraph-copy.db \
  --output benchmarks/ladybug-bulk/full-scale
```

The comparable comparison with `CREATE` and batched transactions is recorded in `benchmarks/ladybug-bulk/report.md`; the full-scale measurement is stored in `benchmarks/ladybug-bulk/full-scale/`.

Direct queries reuse one connection and prepared statements for lookup by stable key, incoming and outgoing references, bounded traversals, shortest path, and repository grouping:

```bash
go run -tags ladybug ./benchmarks/ladybug-queries \
  --database /tmp/kivgraph-copy.db \
  --corpus testdata/generated/synthetic \
  --output benchmarks/ladybug-queries
```

The benchmark runs golden probes before measuring. Its results characterize LadybugDB as the canonical source; they do not qualify the HotSnapshot SLOs.

The incremental update benchmark, `benchmarks/ladybug-incremental`, was **deleted** along with the delta path itself ([ADR 0057](../adr/0057-el-camino-incremental-se-retira.md)). It measured `ApplyCanonicalDelta` -- individual and batched inserts, edge additions and removals, property changes, outgoing relationship replacement, symbol deletion, duplicate rejection, absence of ghost edges, and rollback after a late failure -- and that code no longer exists. Its recorded numbers survive in `docs/decisions/ladybugdb-qualification.md` (`LADYBUG_INCREMENTAL_PASS`, `LADYBUG_DELTA_PERFORMANCE_PASS`) and the harness in git history. There is no incremental indexing path to benchmark: every pass is a full rebuild.

Recovery is tested with isolated workers, `SIGKILL`, corruption, permissions, and a Linux `ENOSPC` injector. Each scenario modifies only a private copy:

```bash
go run -tags ladybug ./benchmarks/ladybug-recovery \
  --database /tmp/kivgraph-copy.db
```

The crash, reopen, truncation, and permissions cases pass. The recorded result retains an explicit `FAIL` for a full disk: `Writer.Apply` returned success and the first intercepted `ENOSPC` appeared during shutdown, leaving the copy unable to reopen. The command returns a nonzero status while this limitation exists. The complete methodology and evidence are in `docs/testing/ladybug-recovery.md`.

The operational diagnostic opens the original database in read-only mode and runs the transaction test on a temporary copy:

```bash
go run -tags ladybug ./cmd/kivgraph doctor storage \
  --database /tmp/kivgraph-copy.db
```

The command reports location, size, effective permissions, external locks, engine versions, storage and Go binding, schema, rollback, counts, and referential integrity. It returns `0` only when every check is `PASS`; a locked or incomplete database, or a binary built without the `ladybug` tag, returns `1`. The specified database is not modified.

Semantic integrity verification checks the six canonical graph invariants on an already-published database without rebuilding it:

```bash
go run -tags ladybug ./cmd/kivgraph doctor graph \
  --database /var/lib/kivgraph/graph/CURRENT/graph.db
```

The command prints one line per rule with its status (`PASS`/`FAIL`) and violation count and, beneath each failed rule, up to `ladybug.MaxIntegritySamples` (20) samples with the table, key, and row detail that breaks it. The six invariants, all zero in a healthy graph, are:

- `exact_edge_without_source`: a semantic edge with exact `confidence` whose source node is not declared, for example a `Symbol` with no incoming `DEFINES` edge from any `File`.
- `exact_edge_without_target`: the same condition for the target node.
- `missing_evidence_file`: an edge with an `evidence_key` whose `Evidence` does not exist, or exists without an `OBSERVED_IN` edge to a `File`.
- `duplicate_stable_key`: the same `stable_key` used by two different node tables.
- `unknown_confidence`: a `confidence` or `provenance` outside the `facts.Confidence`/`facts.Provenance` vocabulary, or an edge that declares exactness backed by non-exact provenance.
- `invalid_repository_ownership`: a node whose `repository_key` does not match the repository reachable through containment (`Package` via `CONTAINS_PACKAGE`, `File` via `CONTAINS_FILE`, `Symbol` via `DEFINES`, `Evidence` via `OBSERVED_IN`), or points to a nonexistent `Repository`.

LadybugDB guarantees that every relationship has both endpoints, so “missing source” never means that the node does not exist: it means that no fact declared it. An exact edge anchored to a symbol that no file declares is a failure of the corresponding invariant, not an acceptable degradation. The command returns `0` only if all six rules pass; the specified database is not modified.

The full rebuild connects facts, staging, `graph.next`, bulk loading, integrity, snapshot, golden probes, and publication in a single operation over a serialized `facts.Set`:

```bash
go run -tags ladybug ./cmd/kivgraph rebuild \
  --facts facts.json \
  --root /var/lib/kivgraph/graph \
  --generation 000123 \
  --resolver-version go-tsserver-1.0.0
```

`--facts` points to a JSON file containing a `facts.Set` (`Repositories`, `Packages`, `Files`, `Symbols`, `Evidence`, `Edges`, `Unresolved`) already normalized with `Set.Sort()` and `Set.Validate()`. `--root` is the root of the `generation.Store` that will receive the new generation; `--generation` is its six-digit identifier, and `--resolver-version` is recorded on every semantic edge together with `--snapshot-id` (default `0`). The command prints one line per stage to standard output with its status and duration, count discrepancies and invariant violations found by the integrity stage, failed probes if any, the snapshot digest, and the published generation.

Publication is atomic: `rebuild` builds and validates the candidate at `--root/generations/<generation>/graph.db` and updates `--root/CURRENT` to point to it only if loading, integrity checks—table counts and semantic invariants—and golden probes pass. A failure at any stage leaves `CURRENT` and the previous generation intact and serving; the command exits with a nonzero status and explains on standard error which stage failed.

The status query resolves the three roles that `generation.Store` must maintain for backup and rollback without rebuilding anything:

```bash
go run -tags ladybug ./cmd/kivgraph graph status \
  --root /var/lib/kivgraph/graph
```

The command prints `graph.active`, `graph.next`, and `graph.backup` with the path named by each on disk, together with the complete list of retained generations. All three are reads over the same layout described above, not a new layout: `graph.active` is the generation pointed to by `--root/CURRENT`, `graph.next` is the candidate at `--root/generations/<id>.tmp` that the next `rebuild` would construct, and `graph.backup` is the generation that a `rollback` would restore, registered at `--root/BACKUP` with the same atomic discipline as `CURRENT` (`BACKUP.next`, `fsync`, `rename`). A store with no active generation reports `graph.active: none`, not an error; the command returns a nonzero status only if it cannot open the `generation.Store`.

`CURRENT` and `BACKUP` cannot be updated in a single `rename`: every publication or rollback writes `BACKUP` first—pointing to the generation that is about to stop being active—and only then writes `CURRENT`. If the process dies between those writes, `BACKUP` may point to the same generation as `CURRENT`; the self-consistent recovery rule, which requires no manual repair, is that `BACKUP == CURRENT` means there is no backup. After each `rebuild`, retention keeps exactly `graph.active` and `graph.backup`: any other published generation is pruned, and a pruning failure does not invalidate publication—the active graph is already correct and continues to be served—but it is recorded in the `publish` stage of the report.

Rollback switches `CURRENT` back to an already-published generation and revalidates it before switching:

```bash
go run -tags ladybug ./cmd/kivgraph rollback \
  --root /var/lib/kivgraph/graph \
  --generation 000123
```

`--generation` is optional: without it, `rollback` uses the registered `graph.backup`, and if there is no backup and no explicit `--generation` was provided, the command fails explaining that there is nowhere to roll back to. Before switching `CURRENT`, `rollback` recalculates the destination generation's digest from its per-table counts—the same formula already written to `snapshot.sha256` by the `rebuild` snapshot stage—and requires all six canonical graph invariants to pass; a generation without `snapshot.sha256` is never reactivated blindly. If either check fails, `CURRENT` remains unchanged: `generation.Store` itself reverts the change when validation fails, before `rollback` needs its own undo mechanism. The command prints the transition, expected and observed digests, and the integrity verdict, and exits with `0` only if the generation became active. A successful rollback swaps the roles: the generation that was previously `graph.active` becomes the new `graph.backup`, so it is always possible to move forward again with another `rollback`.

HotSnapshot construction reads the already-published generation and produces, in memory, the dense index served by MCP queries:

```bash
go run -tags ladybug ./cmd/kivgraph snapshot \
  --root /var/lib/kivgraph/graph \
  --generation 000123
```

`--generation` is optional: without it, `snapshot` builds from the registered `graph.active`. The snapshot is derived from the canonical graph already published in LadybugDB, never from the `facts.Set` that originated it: it reads `Repository`, `Package`, `File`, `Symbol`, `Evidence`, and the semantic relationships directly from the specified database—the same source verified by `doctor graph`—which is not modified. Structural edges (`CONTAINS_PACKAGE`, `CONTAINS_FILE`, `DEFINES`, `OBSERVED_IN`, `REPORTS_UNRESOLVED`) and package dependency edges (`PACKAGE_DEPENDS_ON`, `MODULE_DEPENDS_ON`) do not enter the HotSnapshot CSR, which indexes only `Symbol` by its `stable_key` and preserves only symbol-to-symbol adjacencies; this does not lose information, because containment already lives in the nodes themselves (`File.package_key`, `Package.repository_key`, `Symbol.file_key`) and package dependencies remain directly queryable in LadybugDB, which continues to be their source of truth. The command prints the snapshot identifier and version, its content digest, per-table counts, and the number of edges not represented in the CSR, and exits with `0` only if the graph could be converted into a snapshot; an edge table outside the canonical vocabulary or an unknown `confidence`/`provenance` causes construction to fail instead of silently accepting an incomplete snapshot.

The [LadybugDB qualification](../decisions/ladybugdb-qualification.md) concludes with `ACCEPT_LADYBUGDB_WITH_LIMITS`. `LADYBUG_RECOVERY_PASS` has been emitted: immutable generations and durable `CURRENT` publication protect the active database against `ENOSPC`. LUQUE-0214 achieved a p95 `Apply` time of 271.9 ms for 1,000 relationships with exact transactional staging; `LADYBUG_DELTA_PERFORMANCE_PASS` and `LADYBUG_STORAGE_PASS` were emitted.
