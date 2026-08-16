---
title: Maintenance
description: Inspect, repair, roll back and clean a published graph.
---

## Inspect

```bash
kivgraph doctor
kivgraph graph status --root PATH
```

`doctor` checks the configuration, the toolchains and the published graph. It
reports the language version ceiling this binary type-checks with, which is a
different number from the `go` on the `PATH` and is the one that decides
whether a repository can be indexed. It also checks `cargo` separately: the
bundle carries `rust-analyzer` but no Rust toolchain, and the analyzer cannot
load a workspace without cargo.

`graph status` prints `graph.active`, `graph.next` and `graph.backup` with the
path each names on disk, plus the full list of retained generations. A store
with no active generation reports `graph.active: none`; that is not an error.

## Validate a database

```bash
kivgraph doctor storage --database PATH
kivgraph doctor graph --database PATH
```

`doctor storage` opens the database read-only and runs its transaction test on
a temporary copy. It reports location, size, effective permissions, external
locks, engine versions, storage and Go binding, schema, rollback, counts and
referential integrity, and returns `0` only when every check is `PASS`.

`doctor graph` checks the [six canonical invariants](/reference/resolution/) on
an already-published database without rebuilding it. Neither command modifies
the database it is given.

## Roll back

```bash
kivgraph rollback --root PATH --generation 000123
```

`--generation` is optional: without it, `rollback` uses the registered
`graph.backup`, and fails explaining there is nowhere to go if there is neither
a backup nor an explicit generation.

Before switching `CURRENT`, it recomputes the destination generation's digest
from its per-table counts — the same formula written to `snapshot.sha256` when
the generation was published — and requires all six invariants to pass. A
generation without `snapshot.sha256` is never reactivated blindly. If either
check fails, `CURRENT` does not change.

## Rebuild the hot snapshot

```bash
kivgraph snapshot --root PATH --generation 000123
```

Without `--generation` it builds from the registered `graph.active`. The
snapshot is derived from the canonical graph already published in LadybugDB,
never from the fact set that originated it, and the database is not modified.

## Upgrade after a schema change

```bash
kivgraph upgrade
```

An incompatible schema change requires detecting the version, backing up and
verifying the active generation, and rebuilding from the source repositories.
Only a candidate that passes integrity and validation may change `CURRENT`.

## Clean

```bash
kivgraph clean
kivgraph clean --keep-active --yes
```

`clean` removes published generations. Without `--yes` it enumerates and
touches nothing, because there is no undo — it also removes the backup that
`rollback` depends on. With no flags it leaves the store empty and releases the
reserved space; `--keep-active` preserves exactly the published generation.

It never touches the configuration or the repository registry. Rebuilding what
is registered is `index --full`.

After a full clean the numbering restarts at `000001`, and a snapshot store
only accepts a strictly newer generation: **a running server keeps the graph
that no longer exists and will install no further one.** The command says to
restart it.

## Update the installation

```bash
kivgraph update --check
kivgraph update
```

The update validates the manifest, the version and the checksums before
replacing the bundle, and preserves the configuration and graph state. Restart
the MCP client afterwards.
