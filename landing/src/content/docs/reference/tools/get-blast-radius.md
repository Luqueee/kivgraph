---
title: get_blast_radius
description: Bounded incoming impact of one symbol, grouped by repository, package, depth and relation kind, with a completeness verdict.
---

> What a change to this symbol reaches, by repository, package, depth and relation kind. Grep does not follow a chain.

## Arguments

| Argument | Type | Default | Meaning |
| --- | --- | --- | --- |
| `confidence` | string | none | Gates which incoming edges the traversal may follow, so it changes what counts as affected. Accepted: `EXACT_TYPECHECKED`, `EXACT_DECLARATION_MAPPED`, `EXACT_PACKAGE_MAPPED`, `STRUCTURAL_CERTAIN`, `CANDIDATE`, `UNRESOLVED`. One value, not a list. Anything else is rejected with `INVALID_ARGUMENT`. |
| `cursor` | string | none | Opaque token taken from `next_cursor`. Resumes the same query at the next offset. |
| `depth` | integer | `3` | How many hops the walk may take backwards. Must be between 1 and 5. |
| `edge_kinds` | array of string | none, meaning every reference kind | Gates which relations may be followed, so it also changes what counts as affected. Accepted: `IMPORTS_SYMBOL`, `EXPORTS`, `REEXPORTS`, `REFERENCES`, `CALLS_DIRECT`, `PASSES_AS_CALLBACK`, `ASSIGNS_FUNCTION`, `RETURNS_FUNCTION`, `TYPE_USES`, `IMPLEMENTS`, `EXTENDS`, `EMBEDS`, `OVERRIDES`. Containment and package kinds are rejected. Duplicates collapse; an empty or space-padded entry is rejected. |
| `include_derived` | boolean | `false` | Includes rows from providers Kivgraph derives from the machine, which take the `rust:` namespace, such as a Rust toolchain's standard library. This tool takes no `repo` argument, so the flag is the only way to ask for them. |
| `limit` | integer | `50` | Rows of `symbols` in this page. Must be between 1 and 500. The aggregates are never paged. |
| `max_nodes` | integer | `5000` | Ceiling on how many symbols the walk may discover, the root included. Must be between 1 and 25000. Hitting it sets `traversal_truncated`. |
| `path` | string | none | Repository-relative path narrowing `qualified_name`. Requires `repository`. Rejected together with `stable_key`. |
| `qualified_name` | string | none | Names the symbol to ask about. Either this or `stable_key` is required, never both. A name matching more than one symbol is rejected with `AMBIGUOUS_SYMBOL` instead of being resolved silently. |
| `repository` | string | none | Repository of the symbol being asked about. Narrows `qualified_name`. |
| `response_format` | string | `concise` | `concise` or `detailed`. Detailed adds the derived identifiers: `stable_key`, `file_key` and `reached_from_key`. Anything else is rejected with `INVALID_ARGUMENT`. |
| `stable_key` | string | none | Names the symbol directly. Cannot be combined with `qualified_name`, `repository` or `path`. |

## Answers

What a change to one symbol reaches: the same bounded walk as
[`trace_dependencies`](/reference/tools/trace-dependencies/), run backwards over
incoming edges. The response is the affected symbols plus four aggregations a
reviewer acts on, by repository, by package, by depth and by relation kind. The
root is excluded everywhere, because a symbol is not affected by its own change.
It is also the one tool that states how far its answer reaches, in a
`completeness` object.

## Example

```json
{
  "repository": "kivgraph",
  "path": "internal/facts/facts.go",
  "qualified_name": "MergeAll",
  "depth": 2
}
```

```json
{
  "snapshot_id": 30,
  "snapshot_age_ms": 9021,
  "total": 5,
  "returned": 5,
  "truncated": false,
  "next_cursor": null,
  "coverage": {
    "exact": 5,
    "candidate": 0,
    "unresolved_related": 7,
    "package_level": 0
  },
  "completeness": {
    "verdict": "LOWER_BOUND",
    "invisible_scopes": [
      {
        "reason": "PACKAGE_NOT_BUILDABLE",
        "repository": "kivgraph",
        "requested_package": "github.com/Luqueee/kivgraph/benchmarks/ladybug-delta-profile",
        "detail": "LIST: build constraints exclude all Go files in /Users/adria/Documents/programacion/projects/kivgraph/benchmarks/ladybug-delta-profile"
      },
      {
        "reason": "PACKAGE_NOT_BUILDABLE",
        "repository": "kivgraph",
        "requested_package": "github.com/Luqueee/kivgraph/benchmarks/ladybug-recovery",
        "detail": "LIST: build constraints exclude all Go files in /Users/adria/Documents/programacion/projects/kivgraph/benchmarks/ladybug-recovery"
      },
      {
        "reason": "PACKAGE_PROVIDER_NOT_FOUND",
        "repository": "kivgraph",
        "requested_package": "@astrojs/node"
      },
      …
      {
        "reason": "PACKAGE_PROVIDER_NOT_FOUND",
        "repository": "kivgraph",
        "requested_package": "vitest"
      }
    ],
    "fallback": {
      "pattern": "\\bMergeAll\\b",
      "paths": [
        "/Users/adria/Documents/programacion/projects/kivgraph/benchmarks/ladybug-delta-profile",
        "/Users/adria/Documents/programacion/projects/kivgraph/benchmarks/ladybug-recovery"
      ]
    }
  },
  "results": {
    "root_key": "KHXAWFM5ED2YEIFIEMB5NALA7L7YXNHSUJEBLU7SDAMLGKX24UAA",
    "root_repository": "kivgraph",
    "depth": 2,
    "max_nodes": 5000,
    "affected": 5,
    "deepest_depth": 2,
    "traversal_truncated": false,
    "symbols": [
      {
        "name": "mergeSets",
        "qualified_name": "mergeSets",
        "kind": "func",
        "depth": 1,
        "repository": "kivgraph",
        "language": "go",
        "file_path": "internal/indexer/full.go",
        "start_line": 681,
        "end_line": 712,
        "reached_from": "MergeAll",
        "via_kind": "CALLS_DIRECT",
        "via_confidence": "EXACT_TYPECHECKED",
        "via_provenance": "GO_AST_CALL"
      },
      {
        "name": "closeCrossRepositoryEdges",
        "qualified_name": "closeCrossRepositoryEdges",
        "kind": "func",
        "depth": 1,
        "repository": "kivgraph",
        "language": "go",
        "file_path": "internal/indexer/full.go",
        "start_line": 735,
        "end_line": 783,
        "reached_from": "MergeAll",
        "via_kind": "CALLS_DIRECT",
        "via_confidence": "EXACT_TYPECHECKED",
        "via_provenance": "GO_AST_CALL"
      },
      {
        "name": "Merge",
        "qualified_name": "Set.Merge",
        "kind": "method",
        "depth": 1,
        "repository": "kivgraph",
        "language": "go",
        "file_path": "internal/facts/facts.go",
        "start_line": 505,
        "end_line": 507,
        "reached_from": "MergeAll",
        "via_kind": "CALLS_DIRECT",
        "via_confidence": "EXACT_TYPECHECKED",
        "via_provenance": "GO_AST_CALL"
      },
      {
        "name": "Full",
        "qualified_name": "Full",
        "kind": "func",
        "depth": 2,
        "repository": "kivgraph",
        "language": "go",
        "file_path": "internal/indexer/full.go",
        "start_line": 199,
        "end_line": 363,
        "reached_from": "mergeSets",
        "via_kind": "CALLS_DIRECT",
        "via_confidence": "EXACT_TYPECHECKED",
        "via_provenance": "GO_AST_CALL"
      },
      {
        "name": "Diff",
        "qualified_name": "Diff",
        "kind": "func",
        "depth": 2,
        "repository": "kivgraph",
        "language": "go",
        "file_path": "internal/facts/delta.go",
        "start_line": 240,
        "end_line": 317,
        "reached_from": "Set.Merge",
        "via_kind": "CALLS_DIRECT",
        "via_confidence": "EXACT_TYPECHECKED",
        "via_provenance": "GO_AST_CALL"
      }
    ],
    "by_repository": [
      {
        "key": "kivgraph",
        "count": 5
      }
    ],
    "by_depth": [
      {
        "depth": 1,
        "count": 3
      },
      {
        "depth": 2,
        "count": 2
      }
    ],
    "by_kind": [
      {
        "key": "CALLS_DIRECT",
        "count": 5
      }
    ],
    "by_package": [
      {
        "package_key": "package:go:kivgraph:github.com/Luqueee/kivgraph/internal/facts",
        "package_name": "github.com/Luqueee/kivgraph/internal/facts",
        "repository": "kivgraph",
        "count": 2
      },
      {
        "package_key": "package:go:kivgraph:github.com/Luqueee/kivgraph/internal/indexer",
        "package_name": "github.com/Luqueee/kivgraph/internal/indexer",
        "repository": "kivgraph",
        "count": 3
      }
    ]
  }
}
```

Three of the seven `invisible_scopes` entries are elided above, marked with the
`…` line; the response carries them all. This answer comes from snapshot `30` of
two repositories, `kivgraph` and `mole`.

## Reading the result

`root_key` and `root_repository` name the symbol the impact is about, so a
stored answer still says what it was about. `depth` and `max_nodes` echo the
bounds, including the defaults you did not pass: the request set `depth` to `2`
and left `max_nodes` at `5000`.

`affected` is how many symbols the change reaches, root excluded.
`deepest_depth` is the deepest level any of them sits at, here `2`.

Each row of `symbols` is an affected symbol plus the edge the walk first arrived
by. `depth` is its distance from the root. `reached_from` is the qualified name
of the symbol it was discovered from, and it composes into a chain you can read
off the page: `Diff` at depth 2 was reached from `Set.Merge`, which was reached
from `MergeAll`. `via_kind`, `via_confidence` and `via_provenance` describe that
one edge: the relation, how well it is proven, and the mechanism that observed
it. Every row above is `CALLS_DIRECT` / `EXACT_TYPECHECKED` / `GO_AST_CALL`, a
call expression resolved by `go/types`. `CANDIDATE` and `UNRESOLVED` are
distinct results from an exact edge and are never promoted into one.

The `via_*` triple is the route the breadth-first frontier took, not the only
one. `by_kind` is the field that does not have that limitation.

### The four axes

Every axis is computed over the whole traversal, not over the page, so the
numbers stay stable while you page through `symbols`.

| Field | What it groups |
| --- | --- |
| `by_repository` | Affected symbols per repository `key`. Partitions `affected`. |
| `by_package` | Affected symbols per package, with `package_key`, `package_name`, `repository` and `count`. Partitions `affected`. |
| `by_depth` | Affected symbols per hop distance. Partitions `affected`. |
| `by_kind` | Distinct relation kinds through which each affected symbol touches the traversed subgraph. |

`by_kind` is counted differently on purpose. A consumer can reach the changed
code through several relations at once, calling a function and using its type,
and reporting only the edge the walk happened to take first would hide the
others. So each affected symbol is counted once per distinct kind, and `by_kind`
can therefore sum to more than `affected`. `by_repository` and `by_package`
always partition it exactly.

### `coverage`

`exact` counts the edges with an exact confidence, `candidate` the plausible
ones. `package_level` counts facts about a package rather than about a symbol;
this tool produces none, so it stays `0`. Only
[`find_cross_repo_consumers`](/reference/tools/find-cross-repo-consumers/) fills
it, because a package dependency proves a dependency on the provider and never a
use of the symbol, and summing the two would report a use nobody saw.

`unresolved_related` is `7` in the answer above while `exact` is `5`. That is not
an inconsistency: it counts the recorded resolution failures that could belong to
this question, and it is exactly the number of entries `completeness` lists.

### `completeness`

This is the field that makes the answer safe to act on. Its `verdict` is one of
two values:

| `verdict` | What it means |
| --- | --- |
| `COMPLETE` | Nothing the index recorded could add to this answer. The list is the whole list. |
| `LOWER_BOUND` | The answer is a floor. The index recorded places it could not read that this question reaches, and they are named. |

The captured answer is `LOWER_BOUND`. It does not say "something might be
missing"; it says exactly what it could not see, in `invisible_scopes`. Each
entry is evidence about a failed request, never an inferred relationship, and
carries a `reason`, the `repository` it belongs to, the `requested_package` the
resolver asked for, and a `detail` when the loader wrote one. Two reasons appear
above:

- `PACKAGE_NOT_BUILDABLE`: the package exists in the tree and the Go build
  configuration excludes all of its files, so the index never type-checked it.
  The `detail` quotes the loader verbatim.
- `PACKAGE_PROVIDER_NOT_FOUND`: no repository in the corpus provides that
  package name, so nothing the resolver asked it for could be followed.

A failure that named no symbol belongs to the package, and that is why these
entries carry `requested_package` rather than a symbol: attributing an
unreadable module to every symbol that package exports would invent uses nobody
observed.

`fallback` closes the gap instead of leaving you with a warning. `pattern` is a
literal-word regular expression for the root symbol name, `\bMergeAll\b`, and
`paths` are the absolute directories the graph could not read. Grep that pattern
in those paths and you have covered the difference between the floor and the
answer. A warning without the recovery action would force a whole-repository
sweep, which costs more than not warning at all.

Two more fields appear when there is more to say than one response should carry.
`blind_spots` lists recorded references that named this same symbol and could not
be followed, with the same shape as an invisible scope plus a `file_path` and a
`start_line`. Each list is capped at 20 entries, and `more_blind_spots` and
`more_invisible_scopes` carry the remainder: the count is always exact even when
the list is cut, because a truncated warning must not read as a smaller problem
than it is.

## Limits

There are two different truncations:

- `truncated` is about the page of `symbols`. It is `true` when rows remain, and
  `next_cursor` then carries the token to continue. The aggregates do not page
  and do not change between pages.
- `traversal_truncated` is about the walk. It is `true` when the walk hit
  `max_nodes` and stopped discovering symbols, so the impact itself is
  understated and no cursor will recover it. Raise `max_nodes`, lower `depth`,
  or narrow with `edge_kinds` or `confidence`. In the captured answer both are
  `false`.

To take the next page, repeat the same call and add `cursor` set to the
`next_cursor` value, unchanged. The token is an opaque base64url wrapper around
a versioned body: the format version, the snapshot id, a hash of the query, the
offset, the sorting version `blast-radius-v1` and a checksum over all of it. It
fails closed:

| What changed | What you get |
| --- | --- |
| A new generation was published | `CURSOR_SNAPSHOT_EXPIRED`; restart pagination |
| The selector, `depth`, `max_nodes`, `edge_kinds` or `confidence` differ from the call that produced it | `CURSOR_INVALID` |
| The token was edited, re-encoded, truncated or carries an unknown field | `CURSOR_INVALID` |

`limit`, `response_format` and `include_derived` are not part of the cursor
identity, so changing one of them mid-pagination is accepted and shifts what the
remaining pages contain.

`guidance` appears only when the count alone would mislead: on a truncated page,
naming `depth`, `max_nodes`, `edge_kinds` and `confidence` as the ways to
narrow, and on zero rows, where it says the traversal reached nothing within its
bounds and suggests raising `depth` or asking
[`find_references`](/reference/tools/find-references/) for the direct relations
only. On a complete non-empty answer it is absent.

`include_derived` is `false` by default. With a Rust toolchain in the graph, the
impact of a common trait method reaches most of the corpus. Withholding those
rows is a decision about the page and never a claim about the graph: the edge
stays published with its exact confidence.

A walk that exceeds the deadline the client set on the request fails with
`TRAVERSAL_LIMIT_REACHED` rather than returning a partial answer as if it were
whole.

## Where it loses

The impact stops at five hops and at `max_nodes`, so on a widely used symbol the
honest answer is a bounded one and the `completeness` verdict is what tells you
so. It reports symbols, not behaviour: a caller that is reached may still be
unaffected by your particular change, and that judgement is yours. And an
unindexed or unbuildable package produces no rows at all, which is why the
`LOWER_BOUND` verdict and its `fallback` pattern exist rather than a number
presented as the whole truth.
