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
| `response_format` | string | `concise` | `concise` or `detailed`. Detailed adds the derived identifiers: `stable_key`, `file_key` and `reached_from_key` in the `full` view, and the stable key alone in the compact one, which is the only way a key reaches a compact row. Anything else is rejected with `INVALID_ARGUMENT`. |
| `stable_key` | string | none | Names the starting symbol directly. Cannot be combined with `qualified_name`, `repository` or `path`. |
| `view` | string | `compact` | The granularity of the answer, never a different answer. `compact` lifts into a header what every reached symbol shares and groups the rows by file. `full` is the field-per-row shape. The answer is a set of reached symbols and not a set of files, so `files` is rejected with `INVALID_ARGUMENT`, as is any other value. |

## Answers

What one symbol reaches outward, following the chain rather than stopping at the
first hop. The walk is breadth-first and bounded twice, by `depth` and by
`max_nodes`. The response echoes those bounds, reports how many symbols the walk
reached and how deep it got, and returns one page of them. The start symbol
is the root and is never listed as its own dependency.

By default the page arrives in the `compact` view: whatever every reached symbol
shares -- `repository`, `kind`, `hop_depth`, `reached_from`, `via_kind`,
`via_confidence`, `via_provenance` -- is stated once above the rows, and the rows
are grouped by file under `files`. It is the same edges with the same confidence
and the same provenance as `full`; what leaves the rows is only what every row
repeated. `view: "full"` keeps the field-per-row `nodes` array.

That page-wide hoist needs every row to agree: one row with its own `kind`,
`hop_depth`, `reached_from`, `via_kind`, `via_confidence` or `via_provenance`
pushes the whole column back down onto every row. `results.groups` is the
second tier that catches it, sharing its mechanism and its `compactReachedGroup`
shape with [`get_blast_radius`](/docs/tools/get-blast-radius/): the rows
group by whatever exact tuple of those columns they still share, instead of
repeating it once per row. See
[when a page groups](/mcp/usage/#when-a-page-groups) for the mechanism shared
by six tools, and [reading a grouped page](#reading-a-grouped-page) below for
a captured page.

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
  "total": 37,
  "returned": 3,
  "truncated": true,
  "next_cursor": "Ah4DYbLgOYhUrECElsmz0oFlyLUGpgE",
  "coverage": {
    "exact": 37
  },
  "guidance": "showing 3 of 37; narrow with depth, max_nodes, edge_kinds or confidence, or pass the cursor for the next page",
  "results": {
    "root_key": "KHXAWFM5ED2YEIFIEMB5NALA7L7YXNHSUJEBLU7SDAMLGKX24UAA",
    "root_repository": "kivgraph",
    "depth": 1,
    "max_nodes": 5000,
    "reached": 37,
    "deepest_depth": 1,
    "repository": "kivgraph",
    "kind": "field",
    "hop_depth": 1,
    "reached_from": "MergeAll",
    "via_kind": "REFERENCES",
    "via_confidence": "EXACT_TYPECHECKED",
    "via_provenance": "GO_TYPES_USE",
    "files": [
      {
        "file": "internal/facts/facts.go",
        "at": [
          "Set.Symbols@251",
          "unresolvedIdentity.file@568",
          "unresolvedIdentity.reason@569"
        ]
      }
    ]
  }
}
```

All three rows are fields of the same repository, in the same file, one hop from
the root, reached from `MergeAll` by the same kind of edge with the same
confidence and the same provenance. Every column was therefore hoisted and each
entry is the bare `qualified_name@line` label -- nothing was left to say per row.
`hop_depth` in the header is the depth of the rows, which is a different fact
from `depth`, the bound the walk was given; they only share a name in the full
view. `traversal_truncated` is absent because it is false.

The same page in the `full` view:

```json
{
  "repository": "kivgraph",
  "path": "internal/facts/facts.go",
  "qualified_name": "MergeAll",
  "depth": 1,
  "limit": 3,
  "view": "full"
}
```

```json
{
  "snapshot_id": 30,
  "snapshot_age_ms": 9020,
  "total": 37,
  "returned": 3,
  "truncated": true,
  "next_cursor": "Ah4DYbLgOYhUrECElsmz0oFlyLUGpgE",
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
`go-svc-e`.

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

### Reading a compact row

The header states what the whole page agrees on; a field absent from it means
the rows disagreed and carry their own. Inside `files`, an entry of `at` is
`qualified_name@start`, or `qualified_name@start-end` when the declaration spans
more than one line, and it becomes an array when the row had to carry a column
the page could not hoist. The elements after the label are appended in one fixed
order -- the name, the kind, the depth, the symbol it was reached from, the
edge's kind, its confidence, its provenance, and the stable key -- skipping
every column the header already states, so on a page whose kind was hoisted but
whose rows sit at different depths, `["pkg.Middle@40-58", "2", "pkg.Entry"]` is a
depth of `2` reached from `pkg.Entry`. Numbers in a tail are written as strings,
because a tail is a list of columns and not a record.
A group carries a `repo` of its own only when its file is outside the header's
repository.

The triple for the next call comes from both levels: the header's `repository`
(or the group's `repo`), the group's `file`, and the qualified name from the
label. `language` is never on a row, because the file extension says it, and
`name` is absent whenever the qualified name already ends with it.

Every row is a reached symbol plus the edge it was first arrived by -- in the
header when the page agrees, on the row when it does not:

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
[`find_references`](/docs/tools/find-references/), or read `by_kind` in
[`get_blast_radius`](/docs/tools/get-blast-radius/), which counts every
relation instead of only the discovering one.

The line range -- `start_line` and `end_line` in the full view, the `@start-end`
of a compact label -- bounds the declaration of the reached symbol, so any row
can be opened with [`get_source`](/docs/tools/get-source/) without a second
lookup. A label naming one line is a declaration that starts and ends there.

`coverage` classifies the edges the rows were reached by: `exact` for exact
confidences, `candidate` for the plausible ones, `unresolved_related` for
related references with no target identity. `package_level` counts facts about a
package rather than about a symbol; this tool produces none, so in the full view
it stays `0` and in the compact view it is simply absent, as is any other counter
at zero and `coverage` itself when all four are.
Only [`find_cross_repo_consumers`](/docs/tools/find-cross-repo-consumers/)
fills it, because a package dependency proves a dependency on the provider and
never a use of the symbol, and summing the two would report a use nobody saw.

`completeness` states how far the answer reaches, and its `verdict` is either
`COMPLETE` -- nothing the index recorded could add to this walk -- or
`LOWER_BOUND`, meaning the page is a floor. What bounds an outward answer is not
what bounds an inward one: this verdict is charged with the failures the symbol
itself made -- the references it makes that the resolver could not follow, in
`blind_spots` -- and never with the failures that asked for its name. Somebody
else's unreadable call to this symbol hides a caller, not a dependency. A scope
of its own repository that the index could not read counts too, in
`invisible_scopes`: a package nobody could open may hold anything this symbol
reaches. The distinction matters because
a walk bounded at `depth` or `max_nodes` that reached nothing reads exactly like
a symbol that depends on nothing, and the verdict is what separates the bound
from the graph. See [the completeness verdict](/mcp/usage/#read-the-answer) for
the shared shape and what each tool is charged with.

### Reading a grouped page

A private struct reached by a field selection and then by its own type, and
that type reached again from a second file -- three different `(kind, hop_depth,
via_kind)` tuples out of a walk of seven:

```json
{
  "repository": "kivgraph",
  "path": "internal/app/lifecycle.go",
  "qualified_name": "Lifecycle.waitRunners",
  "depth": 2,
  "limit": 100
}
```

```json
{
  "snapshot_id": 1,
  "total": 7,
  "returned": 7,
  "coverage": { "exact": 7 },
  "results": {
    "root_key": "EOVNCHGUQS43STPNTZICMD4MI35UUDJQFFRFGLKXRPQFOCTHBUJA",
    "root_repository": "kivgraph",
    "depth": 2,
    "max_nodes": 5000,
    "reached": 7,
    "deepest_depth": 2,
    "repository": "kivgraph",
    "via_confidence": "EXACT_TYPECHECKED",
    "groups": [
      {
        "kind": "field",
        "hop_depth": 1,
        "reached_from": "Lifecycle.waitRunners",
        "via_kind": "REFERENCES",
        "via_provenance": "GO_TYPES_SELECTION",
        "files": [{ "file": "internal/app/lifecycle.go", "at": [
          "Lifecycle.runErrs@37", "Lifecycle.runners@35",
          "Lifecycle.runDone@36", "Lifecycle.waiting@33", "Lifecycle.mu@30"
        ] }]
      },
      {
        "kind": "type",
        "hop_depth": 1,
        "reached_from": "Lifecycle.waitRunners",
        "via_kind": "TYPE_USES",
        "via_provenance": "GO_TYPES_USE",
        "files": [{ "file": "internal/app/lifecycle.go", "at": ["Lifecycle@26-42"] }]
      },
      {
        "kind": "type",
        "hop_depth": 2,
        "reached_from": "Lifecycle",
        "via_kind": "TYPE_USES",
        "via_provenance": "GO_TYPES_USE",
        "files": [{ "file": "internal/app/shutdown.go", "at": ["Resource@18-21"] }]
      }
    ]
  }
}
```

`repository` and `via_confidence` still hoist to the page header -- the whole
walk stays inside `kivgraph` on edges a type checker resolved -- but `kind`,
`hop_depth` and the edge that reached each symbol split three ways between five
struct fields, the struct itself, and the one type it in turn exposes, so every
group states its own four columns instead of stamping a residual tail onto
seven rows. `reached_from` still hoists inside the first two groups even though
it is not part of the tuple that built them: both share `Lifecycle.waitRunners`
for a different reason, one because it is the field's container and one because
it is the type the field returns. The third group, alone at depth `2`, is
`Resource` reached through `Lifecycle` itself -- the qualified name a compact
row never needs to repeat, because it is right there in `reached_from`.

## Limits

There are two different truncations and they mean different things:

- `truncated` is about the page. It is `true` when rows remain after this one,
  and `next_cursor` then carries the token to continue. The captured answer
  shows it: 3 rows returned out of 37, `truncated: true`, a token present. On a
  complete page the compact view omits both fields instead of writing `false`
  and `null`.
- `traversal_truncated` is about the walk. It is `true` when the walk hit
  `max_nodes` and stopped discovering new symbols, so rows are missing from the
  answer itself and no cursor will produce them. Raise `max_nodes`, lower
  `depth`, or narrow with `edge_kinds` or `confidence`. In the captured answer
  it is `false`: the walk was complete within its bounds, only the page was
  short -- which is why the compact payload above does not carry the field at
  all, while the full one says `false`.

To take the next page, repeat the same call and add `cursor` set to the
`next_cursor` value, unchanged. The token is an opaque base64url wrapper around a
binary body: the format version, the snapshot id, the offset, a digest of the
query identity, a digest of the sorting contract `dependencies-v1` and a checksum
over all of it. It is about 31 characters --
`Ah4DYbLgOYhUrECElsmz0oFlyLUGpgE` above -- where the previous format spent 314
characters of base64-wrapped JSON. It fails closed:

| What changed | What you get |
| --- | --- |
| A new generation was published | `CURSOR_SNAPSHOT_EXPIRED`; restart pagination |
| The selector, `depth`, `max_nodes`, `repo`, `language`, `edge_kinds` or `confidence` differ from the call that produced it | `CURSOR_INVALID` |
| The token was edited, re-encoded, truncated, or decodes with trailing bytes after the checksum | `CURSOR_INVALID` |
| The token was minted by a server of the previous cursor version | `CURSOR_INVALID`; the body declares version 2 and version 1 is not reinterpreted under the new layout |

`limit`, `view`, `response_format` and `include_derived` are not part of the
cursor identity, so changing one of them mid-pagination is accepted: the same
cursor continues the walk in another view, and changing `limit` shifts what the
remaining pages contain.

`guidance` appears only when the count alone would mislead. On a truncated page
it names the bounds to narrow and the cursor, exactly as in the capture above.
On zero rows it says the traversal reached nothing within its bounds and
suggests raising `depth` or asking
[`find_references`](/docs/tools/find-references/) for the direct relations
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
it, and that is [`get_blast_radius`](/docs/tools/get-blast-radius/). On a
symbol with wide fan-out the answer is large and mostly uninteresting, and the
first thing to do is narrow `edge_kinds` rather than page through it.
