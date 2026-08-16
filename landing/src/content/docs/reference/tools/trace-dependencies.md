---
title: trace_dependencies
description: Bounded outgoing traversal from one symbol, with the edge each reached symbol was first arrived by.
---

> What this symbol reaches outward, bounded by depth. Grep does not follow a chain.

## Arguments

| Argument | Type | Default | Meaning |
| --- | --- | --- | --- |
| `confidence` | string | none | Gates which edges the traversal may follow, so it changes what is reachable. Accepted: `EXACT_TYPECHECKED`, `EXACT_DECLARATION_MAPPED`, `EXACT_PACKAGE_MAPPED`, `STRUCTURAL_CERTAIN`, `CANDIDATE`, `UNRESOLVED`. One value, not a list. Anything else is rejected with `INVALID_ARGUMENT`. |
| `cursor` | string | none | Opaque token taken from `next_cursor`. Resumes the same query at the next offset. |
| `depth` | integer | `3` | How many hops the walk may take. Must be between 1 and 5. |
| `edge_kinds` | array of string | none, meaning every reference kind | Gates which relations the traversal may follow, so it also changes what is reachable. Accepted: `IMPORTS_SYMBOL`, `EXPORTS`, `REEXPORTS`, `REFERENCES`, `CALLS_DIRECT`, `PASSES_AS_CALLBACK`, `ASSIGNS_FUNCTION`, `RETURNS_FUNCTION`, `TYPE_USES`, `IMPLEMENTS`, `EXTENDS`, `EMBEDS`, `OVERRIDES`. Containment and package kinds are rejected. Duplicates collapse; an empty or space-padded entry is rejected. |
| `include_derived` | boolean | `false` | Includes rows from providers Kivgraph derives from the machine, which take the `rust:` namespace, such as a Rust toolchain's standard library. Naming one of them in `repo` has the same effect. |
| `language` | string | none | Selects which reached symbols are returned: `go`, `typescript` or `rust`. It filters rows after reachability and never changes the walk. |
| `limit` | integer | `50` | Rows in this page. Must be between 1 and 500. |
| `max_nodes` | integer | `5000` | Ceiling on how many symbols the walk may discover, the root included. Must be between 1 and 25000. Hitting it sets `traversal_truncated`. |
| `path` | string | none | Repository-relative path narrowing `qualified_name`. Requires `repository`. Rejected together with `stable_key`. |
| `qualified_name` | string | none | Names the symbol to start from. Either this or `stable_key` is required, never both. A name matching more than one symbol is rejected with `AMBIGUOUS_SYMBOL` instead of being resolved silently. |
| `repo` | string | none | Selects which reached symbols are returned, by repository name, compared exactly. A dependency found through a symbol in another repository is still reported. |
| `repository` | string | none | Repository of the symbol being asked about. Narrows `qualified_name`. |
| `response_format` | string | `concise` | `concise` or `detailed`. Detailed adds the derived identifiers: `stable_key`, `file_key` and `reached_from_key`. Anything else is rejected with `INVALID_ARGUMENT`. |
| `stable_key` | string | none | Names the starting symbol directly. Cannot be combined with `qualified_name`, `repository` or `path`. |

## Answers

What one symbol reaches outward, following the chain rather than stopping at the
first hop. The walk is breadth-first and bounded twice, by `depth` and by
`max_nodes`. The response echoes those bounds, reports how many symbols the walk
reached and how deep it got, and returns one page of `nodes`. The start symbol
is the root and is never listed as its own dependency.

## Example

```json
{
  "repository": "kivgraph",
  "path": "internal/facts/facts.go",
  "qualified_name": "MergeAll",
  "depth": 1,
  "limit": 3
}
```

```json
{
  "snapshot_id": 30,
  "snapshot_age_ms": 9020,
  "total": 37,
  "returned": 3,
  "truncated": true,
  "next_cursor": "eyJ2ZXJzaW9uIjoxLCJzbmFwc2hvdF9pZCI6MzAsInF1ZXJ5X2hhc2giOiIyZDNiNDY2YmY5MmIxOWFiOTA2Yjg5YjczZmI1YWViODk5YWMyYWRjN2U3OGFmZGUwYmMwNmE3OTg4ZTQyOWQwIiwib2Zmc2V0IjozLCJzb3J0aW5nX3ZlcnNpb24iOiJkZXBlbmRlbmNpZXMtdjEiLCJjaGVja3N1bSI6ImU4MGU0YTViZTEwNWIwM2VhOGEzOWQ0MzVkZWZmODZjMjU5ZTNmMDY4OGM5Yzc3ZWU0YTAzMGRjZDk0YmRiNDgifQ",
  "coverage": {
    "exact": 37,
    "candidate": 0,
    "unresolved_related": 0,
    "package_level": 0
  },
  "guidance": "showing 3 of 37; narrow with depth, max_nodes, edge_kinds or confidence, or pass the cursor for the next page",
  "results": {
    "root_key": "KHXAWFM5ED2YEIFIEMB5NALA7L7YXNHSUJEBLU7SDAMLGKX24UAA",
    "root_repository": "kivgraph",
    "depth": 1,
    "max_nodes": 5000,
    "reached": 37,
    "deepest_depth": 1,
    "traversal_truncated": false,
    "nodes": [
      {
        "name": "Symbols",
        "qualified_name": "Set.Symbols",
        "kind": "field",
        "depth": 1,
        "repository": "kivgraph",
        "language": "go",
        "file_path": "internal/facts/facts.go",
        "start_line": 251,
        "end_line": 251,
        "reached_from": "MergeAll",
        "via_kind": "REFERENCES",
        "via_confidence": "EXACT_TYPECHECKED",
        "via_provenance": "GO_TYPES_USE"
      },
      {
        "name": "file",
        "qualified_name": "unresolvedIdentity.file",
        "kind": "field",
        "depth": 1,
        "repository": "kivgraph",
        "language": "go",
        "file_path": "internal/facts/facts.go",
        "start_line": 568,
        "end_line": 568,
        "reached_from": "MergeAll",
        "via_kind": "REFERENCES",
        "via_confidence": "EXACT_TYPECHECKED",
        "via_provenance": "GO_TYPES_USE"
      },
      {
        "name": "reason",
        "qualified_name": "unresolvedIdentity.reason",
        "kind": "field",
        "depth": 1,
        "repository": "kivgraph",
        "language": "go",
        "file_path": "internal/facts/facts.go",
        "start_line": 569,
        "end_line": 569,
        "reached_from": "MergeAll",
        "via_kind": "REFERENCES",
        "via_confidence": "EXACT_TYPECHECKED",
        "via_provenance": "GO_TYPES_USE"
      }
    ]
  }
}
```

This answer comes from snapshot `30` of two repositories, `kivgraph` and
`mole`.

## Reading the result

`root_key` and `root_repository` name the symbol the walk started from, so a
stored answer still says what it was about. `depth` and `max_nodes` echo the
bounds that produced it, including the defaults you did not pass: the request
above set `depth` to `1` and left `max_nodes` at `5000`.

`reached` is how many symbols the traversal visited, root excluded.
`deepest_depth` is the deepest level among the rows that survived the filters,
not only the ones on this page; here the walk was bounded at one hop, so it is
`1`. `total` is how many rows survived the `repo`, `language` and
derived-provider filters, and `returned` is how many of them fit in this page:
3 of 37.

Every row is a reached symbol plus the edge it was first arrived by:

- `depth` is how many hops from the root it sits at.
- `reached_from` is the qualified name of the already-reached symbol it was
  discovered from. It is a name, not a key, because the symbol it names is
  another row of the same walk.
- `via_kind` is that edge's relation: `REFERENCES` above is a use that is not a
  call, `CALLS_DIRECT` is a call site.
- `via_confidence` is how well that edge is proven. `EXACT_TYPECHECKED` means a
  type checker resolved it. `CANDIDATE` is plausible and not proven, and
  `UNRESOLVED` carries no target identity; neither is ever promoted into an
  exact edge.
- `via_provenance` is the mechanism that observed it, such as `GO_TYPES_USE` for
  a use `go/types` resolved.

The `via_*` triple describes one route and not the only one. A breadth-first
frontier records the shortest edge it found to each symbol, so a symbol reachable
by both a call and a type use is reported once, by whichever edge arrived first.
For the full set of relations into a symbol, ask
[`find_references`](/reference/tools/find-references/), or read `by_kind` in
[`get_blast_radius`](/reference/tools/get-blast-radius/), which counts every
relation instead of only the discovering one.

`start_line` and `end_line` bound the declaration of the reached symbol, so any
row can be opened with [`get_source`](/reference/tools/get-source/) without a
second lookup.

`coverage` classifies the edges the rows were reached by: `exact` for exact
confidences, `candidate` for the plausible ones, `unresolved_related` for
related references with no target identity. `package_level` counts facts about a
package rather than about a symbol; this tool produces none, so it stays `0`.
Only [`find_cross_repo_consumers`](/reference/tools/find-cross-repo-consumers/)
fills it, because a package dependency proves a dependency on the provider and
never a use of the symbol, and summing the two would report a use nobody saw.

No `completeness` object appears on this tool. Absent means it did not check how
far the answer reaches, which is not the same as checking and finding nothing.
[`get_blast_radius`](/reference/tools/get-blast-radius/) is the tool that
checks.

## Limits

There are two different truncations and they mean different things:

- `truncated` is about the page. It is `true` when rows remain after this one,
  and `next_cursor` then carries the token to continue. The captured answer
  shows it: 3 rows returned out of 37, `truncated: true`, a token present.
- `traversal_truncated` is about the walk. It is `true` when the walk hit
  `max_nodes` and stopped discovering new symbols, so rows are missing from the
  answer itself and no cursor will produce them. Raise `max_nodes`, lower
  `depth`, or narrow with `edge_kinds` or `confidence`. In the captured answer
  it is `false`: the walk was complete within its bounds, only the page was
  short.

To take the next page, repeat the same call and add `cursor` set to the
`next_cursor` value, unchanged. The token is an opaque base64url wrapper around
a versioned body: the format version, the snapshot id, a hash of the query, the
offset, the sorting version `dependencies-v1` and a checksum over all of it. It
fails closed:

| What changed | What you get |
| --- | --- |
| A new generation was published | `CURSOR_SNAPSHOT_EXPIRED`; restart pagination |
| The selector, `depth`, `max_nodes`, `repo`, `language`, `edge_kinds` or `confidence` differ from the call that produced it | `CURSOR_INVALID` |
| The token was edited, re-encoded, truncated or carries an unknown field | `CURSOR_INVALID` |

`limit`, `response_format` and `include_derived` are not part of the cursor
identity, so changing one of them mid-pagination is accepted and shifts what the
remaining pages contain.

`guidance` appears only when the count alone would mislead. On a truncated page
it names the bounds to narrow and the cursor, exactly as in the capture above.
On zero rows it says the traversal reached nothing within its bounds and
suggests raising `depth` or asking
[`find_references`](/reference/tools/find-references/) for the direct relations
only. On a complete non-empty answer it is absent.

`include_derived` is `false` by default. With a Rust toolchain in the graph a
walk over common trait methods reaches most of the corpus, and a page of the
standard library is not what a question about your own code wants. Withholding
those rows is a decision about the page and never a claim about the graph: the
edge stays published with its exact confidence, and `include_derived: true`, or
naming the provider in `repo`, brings it back.

A walk that exceeds the deadline the client set on the request fails with
`TRAVERSAL_LIMIT_REACHED` rather than returning a partial answer as if it were
whole.

## Where it loses

Five hops is the ceiling and three is the default, so this is not a
whole-program closure. It answers what one symbol reaches, which is the less
common question: for a change you are about to make, the question is who reaches
it, and that is [`get_blast_radius`](/reference/tools/get-blast-radius/). On a
symbol with wide fan-out the answer is large and mostly uninteresting, and the
first thing to do is narrow `edge_kinds` rather than page through it.
