---
title: graph_status
description: What the server is serving from, how large it is, how it resolved, and whether any repository has left the commit the graph was built from.
---

> The published generation: counts, provenance, and whether a repository moved since it was indexed. Call it when an answer looks stale.

## Arguments

| Argument | Type | Default | Meaning |
| --- | --- | --- | --- |
| `profile` | array of strings | all profiles | Profiles to report. `["*"]` alone also selects all profiles. |

With several profiles, each entry identifies its default status, generation
and repository count. The outer response carries the selected profile
generations and declares cross-profile edges as unresolved.

## Answers

Whether there is a graph, what it was built from, how big it is, how much of it
failed to resolve, and whether the repositories it describes still hold the code
it describes. It is the tool a client calls when another tool answers
`INDEX_NOT_READY` or returns something that looks stale, so it never fails on a
missing snapshot: reporting that the index is empty is its job.

## Example

```json
{
  "name": "graph_status",
  "arguments": {}
}
```

```json
{
  "snapshot_id": 30,
  "snapshot_age_ms": 9018,
  "total": 1,
  "returned": 1,
  "truncated": false,
  "next_cursor": null,
  "coverage": {
    "exact": 0,
    "candidate": 0,
    "unresolved_related": 0,
    "package_level": 0
  },
  "results": {
    "status": "ready",
    "snapshot_id": 30,
    "snapshot_built_at": "2026-08-15T11:14:10Z",
    "snapshot_age_ms": 9016,
    "snapshot_row_format_version": 3,
    "schema_version": 2,
    "resolver_version": "0.5.0",
    "repositories": 2,
    "packages": 57,
    "files": 311,
    "symbols": 10957,
    "evidence": 40125,
    "edges": 40125,
    "package_edges": 123,
    "unresolved": 1642,
    "edges_by_kind": [
      { "key": "ASSIGNS_FUNCTION", "count": 22 },
      { "key": "CALLS_DIRECT", "count": 6198 },
      { "key": "EMBEDS", "count": 8 },
      { "key": "EXPORTS", "count": 312 },
      …
      { "key": "REFERENCES", "count": 25335 },
      { "key": "RETURNS_FUNCTION", "count": 62 },
      { "key": "TYPE_USES", "count": 8036 }
    ],
    "unresolved_by_reason": [
      { "key": "DECLARATION_NOT_RESOLVED", "count": 4 },
      { "key": "MODULE_PROVIDER_NOT_FOUND", "count": 1487 },
      { "key": "PACKAGE_NOT_BUILDABLE", "count": 2 },
      { "key": "PACKAGE_PROVIDER_NOT_FOUND", "count": 149 }
    ],
    "repository_freshness": [
      {
        "name": "kivgraph",
        "path": "/path/to/kivgraph",
        "languages": ["go", "typescript"],
        "indexed_commit": "d67bc0ebfb3b002f7c52fb9b048b688bd24bd28b",
        "indexed_branch": "main",
        "current_commit": "d67bc0ebfb3b002f7c52fb9b048b688bd24bd28b",
        "current_branch": "main",
        "moved": false
      },
      {
        "name": "go-svc-e",
        "path": "/path/to/home/Documents/programacion/projects/go-svc-e",
        "languages": ["go", "typescript"],
        "indexed_commit": "4cc05cdc2c73cb7111b7b38447639c1444ab8410",
        "indexed_branch": "main",
        "indexed_dirty": true,
        "current_commit": "4cc05cdc2c73cb7111b7b38447639c1444ab8410",
        "current_branch": "main",
        "moved": false
      }
    ],
    "repositories_moved": 0,
    "worker": {
      "state": "not_applicable",
      "detail": "the TypeScript worker runs during indexing, not in this server"
    },
    "storage": {
      "state": "not_applicable",
      "detail": "this server answers from the published snapshot and never opens the database"
    },
    "metrics": {
      "queries": {},
      "snapshot": {
        "id": 30,
        "created_at": "2026-08-15T11:14:10.983894Z",
        "age": 9018245000,
        "build_duration": 0,
        "bytes": 0
      }
    }
  }
}
```

Corpus: snapshot `30` of two repositories, `kivgraph` and `go-svc-e`.

## Identity and freshness

| Field | Meaning |
| --- | --- |
| `status` | `ready` when a snapshot is published and queryable, `empty` when none is. With `empty`, every query tool answers `INDEX_NOT_READY`. |
| `snapshot_id` | The published generation being served. Publication accepts only a strictly newer generation, so a lower id than an earlier answer means the store was rebuilt from empty. |
| `snapshot_built_at` | When that generation was built. |
| `snapshot_age_ms` | How long ago, in milliseconds. This is the number that says whether an answer is minutes or days old. |
| `schema_version` | The canonical graph schema the generation was written with. |
| `schema_version_expected` | The canonical schema this binary builds. Present only when it differs. |
| `schema_outdated` | The comparison between the two above, stated rather than left for the reader to make. |
| `resolver_version` | The resolver that produced its edges. |
| `snapshot_row_format_version` | The format version of the hot snapshot itself, as distinct from the graph schema it was derived from. |
| `snapshot_unreadable` | Why the generation this server holds could not be mapped, when that is what happened. |
| `last_rebuild_at` | When a full rebuild last completed in this deployment. |
| `last_update_at` | When the graph was last updated, rebuild or reconciliation. |

A `schema_version` older than the binary expects is what `kivgraph upgrade`
exists for. A generation published by an older binary **stays readable** -- the
snapshot is a projection with its own row format, so every query answers -- but it
cannot carry facts its resolver never emitted. That answer looks complete and is
not, which is why the comparison is reported instead of inferred.

`snapshot_unreadable` distinguishes two states that share a `status` of `empty`.
The graph is read by the first query that needs it rather than at startup, so a
snapshot that cannot be mapped reaches a caller instead of killing the process.
Without this field, «could not be read» would look like «never indexed», which has
a different fix. Absent when nothing was refused.

Two answers that disagree on `snapshot_id` came from two different graphs.

## Counts

| Field | Meaning |
| --- | --- |
| `repositories` | Registered repositories in the generation, derived providers included. |
| `packages` | Packages, modules or crates across them. |
| `files` | Indexed files. |
| `symbols` | Indexed symbols. |
| `edges` | Resolved symbol edges. |
| `evidence` | Evidence records. Every edge is attributed to one, which is what lets a row name the position it was observed at. |
| `package_edges` | Edges between packages rather than symbols. |

`repositories` is the count. The per-repository array is called
`repository_freshness`, and the two are deliberately not the same key.

## Breakdowns

`edges_by_kind` counts every resolved symbol edge once, under its kind. Every
edge is held in the forward adjacency under its source, so walking it counts
each one exactly once; the reverse adjacency is the same multiset seen from the
other end.

`unresolved` is the total of references the passes could not attribute to a
declaration, and `unresolved_by_reason` says why:

| Reason | Meaning |
| --- | --- |
| `MODULE_PROVIDER_NOT_FOUND` | No registered repository declares the module that owns the target. |
| `PACKAGE_PROVIDER_NOT_FOUND` | Nothing registered provides that package specifier. |
| `PACKAGE_NOT_BUILDABLE` | Build constraints selected no file in the package, so there was nothing to type-check. |
| `DECLARATION_NOT_RESOLVED` | The checker resolved no declaration for the reference. |

The vocabulary is open. Each language pass emits its own reasons and the server
does not validate them against a fixed list, because rejecting a reason it had
not heard of would discard a real fact. A large `unresolved` beside a healthy
`edges` count is normal for a corpus whose dependencies are outside the
registry: nothing registered provides them, which is exactly what the reason
says.

## Repository freshness

`repository_freshness` carries one row per repository, in the same shape
[`list_repositories`](/docs/tools/list-repositories/) returns. It answers,
in the one call an agent makes before trusting anything else, whether what it is
about to be told is stale.

| Reading | What it means |
| --- | --- |
| `indexed_commit` equals `current_commit` | The graph describes the tree on disk. Paths and line ranges are current. |
| `indexed_commit` differs from `current_commit` | The graph is behind the working tree. `moved` is `true` and `moved_detail` names both commits. Line numbers may be wrong and symbols may be gone. |
| `indexed_dirty` is `true` | The tree had uncommitted changes when it was indexed, so the commit alone does not identify what was read. |
| `moved` is `true` and `path` no longer exists | The registered directory is gone. Nothing under it can be read. |
| `current_commit` absent | HEAD could not be read. `moved_detail` carries the reason. |

`repositories_moved` counts the rows that left their indexed commit. A
repository whose HEAD could not be read is not one of them and is not counted as
fresh either; its own row says why. Reading each HEAD is what makes the answer
worth anything: a status that only repeated what the snapshot remembers could
not tell a caller that the snapshot is no longer true.

## It reports only what was used and measured

`worker` and `storage` are both `not_applicable` in the captured response, each
with a reason:

```text
the TypeScript worker runs during indexing, not in this server
```

```text
this server answers from the published snapshot and never opens the database
```

That is the rule this tool follows, and it is the point of the page. `serve`
answers from the published hot snapshot. It never opens the database and never
runs the TypeScript worker, so reporting those two as unconfigured would suggest
a misconfiguration where there is none, and reporting them as healthy would
claim a probe nobody ran. `not_applicable` with a reason is the only honest
answer. What is or is not being served is `status`, `snapshot_id` and
`snapshot_age_ms`.

The same rule governs the optional sections. `metrics` is present only when the
hosting process wires a metrics registry; without one the key is absent rather
than an object of zeros. `derived` is absent when the graph holds no derived
provider. A section reporting zeros would claim a measurement nobody made.

## The derived breakdown

The Rust standard library enters the graph as a synthetic repository named
`rust:<release>`. It is large, and folded into the totals silently it would
answer "how big is my code" with a number about Rust: without a separate
breakdown a ten-symbol repository appears to answer for tens of thousands of
symbols.

So when the graph holds one, `derived` appears beside the counts:

| Field | Meaning |
| --- | --- |
| `repositories` | The name of every derived provider in the snapshot. |
| `packages` | Packages belonging to them. |
| `files` | Files belonging to them. |
| `symbols` | Symbols declared in those files. |
| `edges_within` | Edges whose source is a derived symbol: the standard library referring to itself. |
| `edges_inbound` | Edges that leave a registered repository and land in a derived provider. This is the number the feature exists for. |
| `unresolved` | Gaps the derived provider declares about its own code, none of them the caller's. |

An edge is attributed to the repository of its source symbol, the side that made
the observation. Counting a registered repository's use of `core` as the
standard library's own edge would hide exactly what indexing it was for.

## Limits

- It reports the published generation, not the one being built. A rebuild in
  another process becomes visible when its generation is published.
- The tool is registered only once a generation exists. Before that, `serve`
  registers `index_project` alone; see
  [`index_project`](/docs/tools/index-project/).
- Counts come from the snapshot metadata. They describe what the passes indexed,
  not what exists on disk: a reference the passes could not attribute appears in
  `unresolved` and `unresolved_by_reason`, never as an edge.
- Freshness is per repository and per commit. It cannot tell you that a single
  file changed under an unchanged commit; `indexed_dirty` is the only signal
  that uncommitted work was involved.
