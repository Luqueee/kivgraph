---
title: list_repositories
description: The registered repositories a published graph covers, each with the commit it was indexed at and the commit its tree holds now.
---

> The repositories the published graph covers, with the commit each was indexed at and which one is the derived provider.

## Arguments

| Argument | Type | Default | Meaning |
| --- | --- | --- | --- |
| `profile` | array of strings | all profiles | Profiles to list. `["*"]` alone also selects all profiles. |
| `limit` | integer | `50` | Rows to return in one page. Accepted range is 1 to 500; anything else is `INVALID_ARGUMENT`. |
| `cursor` | string | none | Opaque token from `next_cursor` of a previous call, to continue the same listing. |

## Answers

Which repositories the published graph actually covers, and whether each one
still holds the code the graph describes. Every row carries the commit and
branch the graph was built from next to the commit and branch the working tree
holds right now, so one call is enough to decide whether a path or a line
number from any other tool can still be trusted. Reading the current position
costs two small file reads per repository and is done inline, because a caller
that needs a second call to learn the first one is stale will not make it.

## Example

```json
{
  "name": "list_repositories",
  "arguments": {}
}
```

```json
{
  "snapshot_id": 30,
  "snapshot_age_ms": 9019,
  "total": 2,
  "returned": 2,
  "truncated": false,
  "next_cursor": null,
  "coverage": {
    "exact": 0,
    "candidate": 0,
    "unresolved_related": 0,
    "package_level": 0
  },
  "results": [
    {
      "name": "kivgraph",
      "path": "/path/to/kivgraph",
      "languages": [
        "go",
        "typescript"
      ],
      "indexed_commit": "d67bc0ebfb3b002f7c52fb9b048b688bd24bd28b",
      "indexed_branch": "main",
      "current_commit": "d67bc0ebfb3b002f7c52fb9b048b688bd24bd28b",
      "current_branch": "main",
      "moved": false
    },
    {
      "name": "go-svc-e",
      "path": "/path/to/home/Documents/programacion/projects/go-svc-e",
      "languages": [
        "go",
        "typescript"
      ],
      "indexed_commit": "4cc05cdc2c73cb7111b7b38447639c1444ab8410",
      "indexed_branch": "main",
      "indexed_dirty": true,
      "current_commit": "4cc05cdc2c73cb7111b7b38447639c1444ab8410",
      "current_branch": "main",
      "moved": false
    }
  ]
}
```

Corpus: snapshot `30` of two repositories, `kivgraph` and `go-svc-e`.

## The row

| Field | Meaning |
| --- | --- |
| `profile` | The profile that owns the repository; present only in a multi-profile answer. |
| `name` | The registered identifier of the repository. |
| `path` | The absolute directory the repository was registered at. |
| `languages` | The languages the repository declares, lowercased. |
| `indexed_commit` | The commit the graph was built from. |
| `indexed_branch` | The branch that commit was on. Absent means a detached HEAD. |
| `current_commit` | The commit the working tree holds now. Absent means HEAD could not be read. |
| `current_branch` | The branch the working tree is on now. |
| `indexed_dirty` | Present and `true` when the tree had uncommitted changes at the time it was indexed. |
| `moved` | `true` when `current_commit` differs from `indexed_commit`. |

With several profiles, the response replaces `snapshot_id` and
`snapshot_age_ms` with `profiles`, one entry per selected profile and its
generation, and declares `cross_profile_edges` as `not_resolved`.

Two more fields appear only when they say something. `moved_detail` carries the
reason in prose: the two commits the tree moved between, or why the comparison
could not be made at all. `derived` marks a provider Kivgraph built from the
machine rather than from the registry.

Movement is decided by the commit alone. A branch renamed or recreated over the
same commit leaves every path and every line exactly where the graph says they
are, and reporting that as a move would train a caller to ignore the field.

## Names are identifiers

Repository names are compared exactly. A name is an identifier, never a path
component, and the stable keys that carry it are case-sensitive: two
repositories differing only in case are two repositories and both must be able
to register under their real name.

The `rust:` namespace is reserved. The Rust standard library enters the graph as
a synthetic repository named `rust:<release>`, its row is marked `derived`, and
a user-registered name that takes the namespace is rejected. That reservation is
what makes the name authoritative: no extra column is needed to tell a derived
provider from an indexed repository.

A derived row carries no `indexed_commit`, no `indexed_branch` and no
`indexed_dirty`. Nothing clones it and nothing can move it, so its
`moved_detail` says that it has no commit to compare rather than reporting a
freshness problem that does not exist.

## Limits

- With no published generation the tool is not registered at all. See
  [Troubleshooting](/mcp/troubleshooting/).
- A published store with no active snapshot answers `INDEX_NOT_READY`.
- A `cursor` is bound to the snapshot it was issued from. After a rebuild it
  answers `CURSOR_SNAPSHOT_EXPIRED` and pagination has to restart, because the
  page it named no longer exists.
- A `cursor` from a different tool or a malformed one answers `CURSOR_INVALID`.
- A HEAD that cannot be read is never reported as agreement. The `current_*`
  fields stay empty, `moved` stays `false`, and `moved_detail` carries the
  reason. An unknown answer must not read as a good one.
- The row describes registration and position, not size. For counts, breakdowns
  and per-repository freshness in one answer, call
  [`graph_status`](/docs/tools/graph-status/).
