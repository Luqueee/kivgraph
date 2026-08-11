# Ladygraph

Ladygraph will be a standalone, local MCP server for cross-repository code intelligence in TypeScript and Go.

## Status

The repository contains the initial project foundation. Indexing, storage, and MCP query functionality will be added following the order defined in [`TASKS.md`](TASKS.md).

## Requirements

- Go 1.26 or later. The indexer type-checks with the `go/types` linked into
  the binary, so it can only read repositories and dependencies written for
  its own language version or older.

## Installation

### Install the MCP with one script

The installer detects the platform, downloads the latest published MCP release
for it, verifies both the release archive and the bundle checksums, and
installs it without requiring Go, Node.js, or pnpm. The release contains the Go
server, the pinned LadybugDB library, the TypeScript worker, and the grammar
manifest; the web viewer is intentionally omitted.

Published bundles: Linux `amd64` and macOS `arm64`.

Runtime requirements: Bash, Node.js `22` or later, `curl`, `tar`, and
`sha256sum` or `shasum`.

On macOS the binaries are not notarized. A release downloaded with `curl` is
not quarantined and runs; a copy downloaded with a browser needs `xattr -dr
com.apple.quarantine`. See
[docs/development/macos.md](docs/development/macos.md).

Install the latest release in one command:

```bash
curl -fsSL https://github.com/Luqueee/ladygraph/releases/latest/download/install.sh | bash
```

From a checkout, the same installer can be run directly:

```bash
./scripts/install.sh
```

To install a specific release instead of the latest one:

```bash
LADYGRAPH_VERSION=v0.3.1 ./scripts/install.sh
```

The script installs the bundle in `~/.local/opt/ladygraph` and puts launchers
in `~/.local/bin`. It never modifies a registered repository, creates an index,
or replaces configuration files. To use a different location, set
`LADYGRAPH_INSTALL_ROOT` and `LADYGRAPH_BIN_DIR`.

Add the launcher directory to the current shell and verify both runtimes:

```bash
export PATH="$HOME/.local/bin:$PATH"
ladygraph version
ladygraph-ts-worker <<'EOF'
hello
EOF
```
Check for a newer release or update the installed bundle:

```bash
ladygraph update --check
ladygraph update
```

The update is atomic, preserves the configuration and graph state, verifies
the release and bundle checksums, and replaces only the installed bundle.
Restart the MCP client after updating so it launches the new binary.

When `ladygraph` is invoked without a command from an interactive terminal, it
checks for a newer release with an 800 ms timeout and a 24-hour cache in the
platform cache directory (`$XDG_CACHE_HOME` on Linux and
`$HOME/Library/Caches` on macOS), under `ladygraph/update-check.json`.
The optional check never blocks the command when the network is unavailable.

Interactive command output uses semantic ANSI colors when the destination is a
terminal. Set `NO_COLOR` or redirect output to keep it plain.

### Configure an MCP client and install the skill

The release installer does not edit client configuration automatically. After
installing Ladygraph, run the integration commands without `--target` to detect
the coding agents present on this machine and select one or more of them:

```bash
ladygraph mcp install --scope user
ladygraph skill install --scope user
```

Ladygraph checks each client's known local configuration or installation roots
and marks detected agents. Use `↑`/`↓` (or `j`/`k`) to move, `space` to toggle
an agent, `a` to select all, `n` to select none, `Enter` to confirm, and `q` or
`Esc` to cancel. If none is detected, the selector starts with no agents
selected. Use `--target` only for scripted, non-interactive installation.

Supported MCP targets are `claude-code`, `claude-desktop`, `codex`, `opencode`,
and `oh-my-pi`. Supported skill targets are `claude-code`, `codex`, `opencode`,
and `oh-my-pi`; Claude Desktop has no local skill target. The default scope is
`user`; use `--scope project` for project-local configuration. Use `--dry-run`
to inspect a plan without writing. Existing incompatible entries stop with an
error; `--force` is required to replace or remove one. Existing files are
written atomically with mode `0600` and receive a
`*.ladygraph.bak` backup before replacement or removal.

Inspect or remove a registration explicitly:

```bash
ladygraph mcp status --target claude-code --scope user
ladygraph mcp remove --target claude-code --scope user
ladygraph skill status --target claude-code --scope user
ladygraph skill remove --target claude-code --scope user
```

Initialize and publish a graph before starting the MCP server:

```bash
ladygraph init \
  --repository project=/absolute/path/to/project \
  --languages go,typescript
ladygraph doctor
ladygraph index --full
```

Configure any MCP client to start the server over STDIO:

```json
{
  "mcpServers": {
    "ladygraph": {
      "command": "/home/user/.local/bin/ladygraph",
      "args": [
        "serve",
        "--config",
        "/home/user/.config/ladygraph/config.yaml"
      ]
    }
  }
}
```

`ladygraph serve` must start only after a validated generation has been
published. The process writes MCP framing exclusively to `stdout` and logs to
`stderr`.

## Development

```bash
make build
make test
make version
```

The provisional version command can also be run directly:

```bash
go build ./cmd/ladygraph
./ladygraph version
```

### LadybugDB synthetic corpus

The generator creates a reproducible JSON Lines corpus for storage benchmarks:

```bash
go run ./cmd/ladygraph benchmark generate-graph \
  --symbols 100000 \
  --edges 1000000 \
  --seed 42
```

By default, it generates 40 repositories, 100,000 files, 100,000 symbols, and 1,000,000 edges in `testdata/generated/synthetic`. `--repositories`, `--files`, and `--output` can override these values. The directory contains `repositories.jsonl`, `files.jsonl`, `symbols.jsonl`, `edges.jsonl`, and a `manifest.json` with the graph counts and controlled structures.

The individual reference load requires the native LadybugDB library and executes one prepared statement per node or edge:

```bash
go run -tags ladybug ./benchmarks/ladybug-individual \
  --corpus testdata/generated/synthetic \
  --database /tmp/ladygraph-individual.db \
  --transaction-size 1000
```

The transaction size only controls commits; it does not group records into a single statement. Results are written to `benchmarks/ladybug-individual`.

The batched variant uses an `UNWIND $rows` statement and one transaction per batch. It compares the batch sizes required by the plan:

```bash
go run -tags ladybug ./benchmarks/ladybug-batch \
  --corpus testdata/generated/synthetic \
  --database-dir /tmp/ladygraph-batch \
  --batch-sizes 100,1000,10000,50000
```

Each scenario uses a new database and verifies the stored counts before recording throughput, commit time, peak RSS, and disk size. The recorded comparison recommends 10,000 records per batch under the 2 GiB RSS limit. Scenarios run in separate processes so their memory measurements do not contaminate one another.

The bulk load through `COPY` exports the corpus to temporary CSV files and executes one `COPY` operation per table. At the initial full scale, 200,040 nodes and 1,000,000 edges were verified:

```bash
go run -tags ladybug ./benchmarks/ladybug-bulk \
  --corpus testdata/generated/synthetic \
  --database /tmp/ladygraph-copy.db \
  --output benchmarks/ladybug-bulk/full-scale
```

The comparable comparison with `CREATE` and batched transactions is recorded in `benchmarks/ladybug-bulk/report.md`; the full-scale measurement is stored in `benchmarks/ladybug-bulk/full-scale/`.

Direct queries reuse one connection and prepared statements for lookup by stable key, incoming and outgoing references, bounded traversals, shortest path, and repository grouping:

```bash
go run -tags ladybug ./benchmarks/ladybug-queries \
  --database /tmp/ladygraph-copy.db \
  --corpus testdata/generated/synthetic \
  --output benchmarks/ladybug-queries
```

The benchmark runs golden probes before measuring. Its results characterize LadybugDB as the canonical source; they do not qualify the HotSnapshot SLOs.

The incremental update uses a single logical writer, validates the complete delta before mutating, and applies symbols and relationships in one transaction. The benchmark copies an already-loaded database, so it never modifies the input artifact:

```bash
go run -tags ladybug ./benchmarks/ladybug-incremental \
  --database /tmp/ladygraph-copy.db \
  --corpus testdata/generated/synthetic \
  --output benchmarks/ladybug-incremental
```

The sequence measures individual and batched inserts, edge additions and removals, property changes, outgoing relationship replacement, and symbol deletion. It then checks duplicate rejection, the absence of ghost edges, and rollback after a late failure. The timings cover only transactional LadybugDB mutation; HotSnapshot construction and publication belong to later phases.

Recovery is tested with isolated workers, `SIGKILL`, corruption, permissions, and a Linux `ENOSPC` injector. Each scenario modifies only a private copy:

```bash
go run -tags ladybug ./benchmarks/ladybug-recovery \
  --database /tmp/ladygraph-copy.db
```

The crash, reopen, truncation, and permissions cases pass. The recorded result retains an explicit `FAIL` for a full disk: `Writer.Apply` returned success and the first intercepted `ENOSPC` appeared during shutdown, leaving the copy unable to reopen. The command returns a nonzero status while this limitation exists. The complete methodology and evidence are in `docs/testing/ladybug-recovery.md`.

The operational diagnostic opens the original database in read-only mode and runs the transaction test on a temporary copy:

```bash
go run -tags ladybug ./cmd/ladygraph doctor storage \
  --database /tmp/ladygraph-copy.db
```

The command reports location, size, effective permissions, external locks, engine versions, storage and Go binding, schema, rollback, counts, and referential integrity. It returns `0` only when every check is `PASS`; a locked or incomplete database, or a binary built without the `ladybug` tag, returns `1`. The specified database is not modified.

Semantic integrity verification checks the six canonical graph invariants on an already-published database without rebuilding it:

```bash
go run -tags ladybug ./cmd/ladygraph doctor graph \
  --database /var/lib/ladygraph/graph/CURRENT/graph.db
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
go run -tags ladybug ./cmd/ladygraph rebuild \
  --facts facts.json \
  --root /var/lib/ladygraph/graph \
  --generation 000123 \
  --resolver-version go-tsserver-1.0.0
```

`--facts` points to a JSON file containing a `facts.Set` (`Repositories`, `Packages`, `Files`, `Symbols`, `Evidence`, `Edges`, `Unresolved`) already normalized with `Set.Sort()` and `Set.Validate()`. `--root` is the root of the `generation.Store` that will receive the new generation; `--generation` is its six-digit identifier, and `--resolver-version` is recorded on every semantic edge together with `--snapshot-id` (default `0`). The command prints one line per stage to standard output with its status and duration, count discrepancies and invariant violations found by the integrity stage, failed probes if any, the snapshot digest, and the published generation.

Publication is atomic: `rebuild` builds and validates the candidate at `--root/generations/<generation>/graph.db` and updates `--root/CURRENT` to point to it only if loading, integrity checks—table counts and semantic invariants—and golden probes pass. A failure at any stage leaves `CURRENT` and the previous generation intact and serving; the command exits with a nonzero status and explains on standard error which stage failed.

The status query resolves the three roles that `generation.Store` must maintain for backup and rollback without rebuilding anything:

```bash
go run -tags ladybug ./cmd/ladygraph graph status \
  --root /var/lib/ladygraph/graph
```

The command prints `graph.active`, `graph.next`, and `graph.backup` with the path named by each on disk, together with the complete list of retained generations. All three are reads over the same layout described above, not a new layout: `graph.active` is the generation pointed to by `--root/CURRENT`, `graph.next` is the candidate at `--root/generations/<id>.tmp` that the next `rebuild` would construct, and `graph.backup` is the generation that a `rollback` would restore, registered at `--root/BACKUP` with the same atomic discipline as `CURRENT` (`BACKUP.next`, `fsync`, `rename`). A store with no active generation reports `graph.active: none`, not an error; the command returns a nonzero status only if it cannot open the `generation.Store`.

`CURRENT` and `BACKUP` cannot be updated in a single `rename`: every publication or rollback writes `BACKUP` first—pointing to the generation that is about to stop being active—and only then writes `CURRENT`. If the process dies between those writes, `BACKUP` may point to the same generation as `CURRENT`; the self-consistent recovery rule, which requires no manual repair, is that `BACKUP == CURRENT` means there is no backup. After each `rebuild`, retention keeps exactly `graph.active` and `graph.backup`: any other published generation is pruned, and a pruning failure does not invalidate publication—the active graph is already correct and continues to be served—but it is recorded in the `publish` stage of the report.

Rollback switches `CURRENT` back to an already-published generation and revalidates it before switching:

```bash
go run -tags ladybug ./cmd/ladygraph rollback \
  --root /var/lib/ladygraph/graph \
  --generation 000123
```

`--generation` is optional: without it, `rollback` uses the registered `graph.backup`, and if there is no backup and no explicit `--generation` was provided, the command fails explaining that there is nowhere to roll back to. Before switching `CURRENT`, `rollback` recalculates the destination generation's digest from its per-table counts—the same formula already written to `snapshot.sha256` by the `rebuild` snapshot stage—and requires all six canonical graph invariants to pass; a generation without `snapshot.sha256` is never reactivated blindly. If either check fails, `CURRENT` remains unchanged: `generation.Store` itself reverts the change when validation fails, before `rollback` needs its own undo mechanism. The command prints the transition, expected and observed digests, and the integrity verdict, and exits with `0` only if the generation became active. A successful rollback swaps the roles: the generation that was previously `graph.active` becomes the new `graph.backup`, so it is always possible to move forward again with another `rollback`.

HotSnapshot construction reads the already-published generation and produces, in memory, the dense index served by MCP queries:

```bash
go run -tags ladybug ./cmd/ladygraph snapshot \
  --root /var/lib/ladygraph/graph \
  --generation 000123
```

`--generation` is optional: without it, `snapshot` builds from the registered `graph.active`. The snapshot is derived from the canonical graph already published in LadybugDB, never from the `facts.Set` that originated it: it reads `Repository`, `Package`, `File`, `Symbol`, `Evidence`, and the semantic relationships directly from the specified database—the same source verified by `doctor graph`—which is not modified. Structural edges (`CONTAINS_PACKAGE`, `CONTAINS_FILE`, `DEFINES`, `OBSERVED_IN`, `REPORTS_UNRESOLVED`) and package dependency edges (`PACKAGE_DEPENDS_ON`, `MODULE_DEPENDS_ON`) do not enter the HotSnapshot CSR, which indexes only `Symbol` by its `stable_key` and preserves only symbol-to-symbol adjacencies; this does not lose information, because containment already lives in the nodes themselves (`File.package_key`, `Package.repository_key`, `Symbol.file_key`) and package dependencies remain directly queryable in LadybugDB, which continues to be their source of truth. The command prints the snapshot identifier and version, its content digest, per-table counts, and the number of edges not represented in the CSR, and exits with `0` only if the graph could be converted into a snapshot; an edge table outside the canonical vocabulary or an unknown `confidence`/`provenance` causes construction to fail instead of silently accepting an incomplete snapshot.

The [LadybugDB qualification](docs/decisions/ladybugdb-qualification.md) concludes with `ACCEPT_LADYBUGDB_WITH_LIMITS`. `LADYBUG_RECOVERY_PASS` has been emitted: immutable generations and durable `CURRENT` publication protect the active database against `ENOSPC`. LUQUE-0214 achieved a p95 `Apply` time of 271.9 ms for 1,000 relationships with exact transactional staging; `LADYBUG_DELTA_PERFORMANCE_PASS` and `LADYBUG_STORAGE_PASS` were emitted.

## Structure

```text
cmd/ladygraph/   Main executable.
internal/        Ladygraph internal packages.
ts-worker/        TypeScript worker.
testdata/         Test fixtures and corpora.
benchmarks/       Benchmark results.
docs/             Documentation and ADRs.
scripts/          Auxiliary automation.
```

## License

Ladygraph is distributed under the [Apache License 2.0](LICENSE).

## Third-party licenses

Notices and licenses for dependencies distributed with Ladygraph are recorded in [`THIRD_PARTY_NOTICES.md`](THIRD_PARTY_NOTICES.md). The list is updated whenever a dependency is added to the distributable product.
