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
| `kinds` | array of string | none, meaning every kind except `variable` and `field` | Which symbol kinds count as affected. The default leaves out the local bindings a backwards walk crosses on its way to the consumers a reviewer is looking for, and the response says so under `kinds_default_excluded`. A list replaces that default and is echoed, sorted, under `kinds`; `["*"]` reports every kind the snapshot holds and is echoed as `kinds: ["*"]`. The wildcard stands alone -- mixing it with names asks for a filter and for no filter at once -- and a kind outside the accepted vocabulary is rejected with `INVALID_ARGUMENT` rather than silently matching nothing. Accepted: `alias`, `associated_type`, `attribute`, `class`, `const`, `constant`, `enum`, `enum_member`, `export`, `field`, `func`, `function`, `implementation`, `import`, `interface`, `macro`, `method`, `module`, `namespace`, `parameter`, `property`, `self_parameter`, `static`, `static_method`, `struct`, `trait`, `trait_method`, `type`, `type_alias`, `type_parameter`, `union`, `var`, `variable`. |
| `limit` | integer | `50` | Rows of `symbols` in this page. Must be between 1 and 500. The aggregates are never paged. |
| `max_nodes` | integer | `5000` | Ceiling on how many symbols the walk may discover, the root included. Must be between 1 and 25000. Hitting it sets `traversal_truncated`. |
| `path` | string | none | Repository-relative path narrowing `qualified_name`. Requires `repository`. Rejected together with `stable_key`. |
| `qualified_name` | string | none | Names the symbol to ask about. Either this or `stable_key` is required, never both. A name matching more than one symbol is rejected with `AMBIGUOUS_SYMBOL` instead of being resolved silently. |
| `repository` | string | none | Repository of the symbol being asked about. Narrows `qualified_name`. |
| `response_format` | string | `concise` | `concise` or `detailed`. Detailed adds the derived identifiers: `stable_key`, `file_key` and `reached_from_key` in the `full` view, and the stable key alone in the compact one, which is the only way a key reaches a compact row. Anything else is rejected with `INVALID_ARGUMENT`. |
| `stable_key` | string | none | Names the symbol directly. Cannot be combined with `qualified_name`, `repository` or `path`. |
| `view` | string | `compact` | The granularity of the answer, never a different answer. `compact` leads with the filter and the four axes, states what every affected symbol shares once and groups the rows by file. `full` is the field-per-row shape. The answer is a set of affected symbols and not a set of files, so `files` is rejected with `INVALID_ARGUMENT`, as is any other value. |

## Answers

What a change to one symbol reaches: the same bounded walk as
[`trace_dependencies`](/docs/tools/trace-dependencies/), run backwards over
incoming edges. The response is the affected symbols plus four aggregations a
reviewer acts on, by repository, by package, by depth and by relation kind. The
root is excluded everywhere, because a symbol is not affected by its own change.
It states how far its answer reaches in a `completeness` object, as the five
other tools whose empty answer could be read as proof now do.

Two things shape the answer before you read it. The kind filter decides what
counts as affected, and the response always states which one ran. The `view`
decides how it is spelled: by default the `compact` one, which leads with the
filter and the four axes -- "how far does this reach" is the question, and the
page behind them only names what the axes counted -- states what every affected
symbol shares once, and groups the rows by file. Measured over the private
benchmark corpus, one
depth-2 answer went from `5.102` tokens to `921`.

That page-wide hoist is unanimous or nothing: one row that disagrees on `kind`,
`hop_depth`, `reached_from`, `via_kind`, `via_confidence` or `via_provenance`
pushes the whole column back down to every row. `results.groups` is the second
tier that catches it, grouping the rows by whatever exact tuple of those
columns they still share instead of repeating it on each one. A real `29`-row
answer went from `921` to `821` tokens this way; it gains less than the other
five tools because most of its rows carry their own distinct `reached_from`, the
one column excluded from the grouping tuple on purpose -- folding it in would
fragment every group back down to one row. See
[when a page groups](/mcp/usage/#when-a-page-groups) for the mechanism shared
by six tools, and [reading a grouped page](#reading-a-grouped-page) below for a
captured page.

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
  "total": 5,
  "returned": 5,
  "coverage": {
    "exact": 5,
    "unresolved_related": 7
  },
  "completeness": { … },
  "results": {
    "root": "kivgraph:internal/facts/facts.go:516",
    "depth": 2,
    "max_nodes": 5000,
    "kinds_default_excluded": ["field", "variable"],
    "affected": 5,
    "deepest_depth": 2,
    "by_depth": { "1": 3, "2": 2 },
    "by_kind": { "CALLS_DIRECT": 5 },
    "by_repository": { "kivgraph": 5 },
    "by_package": [
      { "package": "github.com/Luqueee/kivgraph/internal/facts", "count": 2 },
      { "package": "github.com/Luqueee/kivgraph/internal/indexer", "count": 3 }
    ],
    "repository": "kivgraph",
    "via_kind": "CALLS_DIRECT",
    "via_confidence": "EXACT_TYPECHECKED",
    "via_provenance": "GO_AST_CALL",
    "files": [
      {
        "file": "internal/indexer/full.go",
        "at": [
          ["mergeSets@681-712", "func", "1", "MergeAll"],
          ["closeCrossRepositoryEdges@735-783", "func", "1", "MergeAll"],
          ["Full@199-363", "func", "2", "mergeSets"]
        ]
      },
      {
        "file": "internal/facts/facts.go",
        "at": [
          ["Set.Merge@505-507", "method", "1", "MergeAll"],
          ["Diff@240-317", "func", "2", "Set.Merge"]
        ]
      }
    ]
  }
}
```

The `completeness` block is cut here with `{ … }` only to keep the payload short:
the compact view spells it exactly as the full view below does, and it is not a
field either view abbreviates.

The root is the `repository:path:line` triple every tool accepts as a selector,
not the stable key the full view carries. Every row was reached by a call a type
checker resolved, so `via_kind`, `via_confidence` and `via_provenance` are stated
once; the kinds, the depths and the symbol each row was reached from disagree, so
those three travel on the rows, in a tail
after the label, in the fixed order name, kind, depth, `reached_from`, `via_kind`,
`via_confidence`, `via_provenance`, stable key -- skipping whatever the header
already states. The name is absent from all five tails because each qualified name
already ends with it. Reading `["Diff@240-317", "func", "2", "Set.Merge"]`: a
function declared on lines 240 to 317 of the group's file, two hops out, reached
from `Set.Merge`, and its next call is `repository` `kivgraph`, `path`
`internal/facts/facts.go`, `qualified_name` `Diff`.

The same answer in the `full` view -- the field-per-row shape, with the stable
key as the root and every column spelled on every row:

```json
{
  "repository": "kivgraph",
  "path": "internal/facts/facts.go",
  "qualified_name": "MergeAll",
  "depth": 2,
  "view": "full"
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
    "kinds_default_excluded": [
      "field",
      "variable"
    ],
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

The root is named once: `root` as `repository:path:line` in the compact view,
`root_key` plus `root_repository` in the full one, so a stored answer still says
what it was about. `depth` and `max_nodes` echo the bounds, including the
defaults you did not pass: the request set `depth` to `2` and left `max_nodes` at
`5000`.

### What the answer says about its own filter

Exactly one of two fields is present, in both views, and reading it is how you
know what `affected` counted:

| Field | What it means |
| --- | --- |
| `kinds_default_excluded` | No `kinds` was passed. Its value, `["field", "variable"]`, is what the answer left out: the local bindings a backwards walk crosses on its way to real consumers. |
| `kinds` | A `kinds` was passed. Its value is that selection, sorted and canonical, or `["*"]` for the wildcard that reports everything. |

A filtered count that did not say it was filtered would be a lie about the size
of an impact, which is why one of the two is always there.

The filter applies to the page, to `affected`, to `total` and to all four
aggregates alike, so every number in the response describes the same set. It
never applies to the traversal itself: a consumer reached *through* an excluded
binding is still reported behind it, at its own depth.

It is also part of the cursor identity. A page taken under one filter cannot be
resumed under another -- the token fails as `CURSOR_INVALID` rather than
continuing into a set that no longer means the same thing -- and a cursor minted
before the filter existed fails the same way.

The default is the one that changes answers rather than shapes. Measured over
the private benchmark corpus, `48` of the first `50` rows of a `get_blast_radius` page were local
variables, and of the `118` symbols the same walk reached at depth 2, `29` are
invocable and the rest were local bindings. `kinds: ["*"]` restores every one of
them; a list such as `["func", "method"]` narrows further; a kind the loaders
never publish is `INVALID_ARGUMENT`, because a typo that silently matched nothing
would understate an impact.

`affected` is how many symbols the change reaches, root excluded.
`deepest_depth` is the deepest level any of them sits at, here `2`.

Each affected symbol is a row plus the edge the walk first arrived
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

### Reading a compact row

The header above `files` states what every row of the page shares, out of
`repository`, `kind`, `hop_depth`, `reached_from`, `via_kind`, `via_confidence`
and `via_provenance`; a field absent from it means the rows disagreed and carry
their own. Inside a group, an entry of `at` is `qualified_name@start`, or
`qualified_name@start-end` when the declaration spans more than one line, and it
becomes an array whose elements after the label are the columns the header could
not hoist, appended in the order name, kind, depth, `reached_from`, `via_kind`,
`via_confidence`, `via_provenance`, stable key. Numbers in that tail are written
as strings: it is a list of columns, not a record. `language` is never on a row --
the file extension says it -- and `name` is absent whenever the qualified name
already ends with it.

The triple for the next call is the header's `repository` (or a group's own
`repo`, present only when its file sits outside the hoisted repository), the
group's `file`, and the qualified name from the label.

`hop_depth` in the header is the depth of the rows; `depth` beside `max_nodes` is
the bound the walk was given. They are different facts that share a name in the
full view.

### Reading a grouped page

A type used from both methods and a top-level function, so `kind` cannot hoist
to the page header:

```json
{
  "repository": "mole",
  "path": "internal/admin/admin.go",
  "qualified_name": "Server",
  "depth": 2
}
```

```json
{
  "snapshot_id": 1,
  "total": 9,
  "returned": 9,
  "coverage": { "exact": 9 },
  "completeness": { … },
  "results": {
    "root": "mole:internal/admin/admin.go:58",
    "depth": 2,
    "max_nodes": 5000,
    "kinds_default_excluded": ["field", "variable"],
    "affected": 9,
    "deepest_depth": 2,
    "by_depth": { "1": 8, "2": 1 },
    "by_kind": { "CALLS_DIRECT": 1, "PASSES_AS_CALLBACK": 1, "TYPE_USES": 8 },
    "by_repository": { "mole": 9 },
    "by_package": [
      { "package": "github.com/Luqueee/mole/cmd/mole", "count": 1 },
      { "package": "github.com/Luqueee/mole/internal/admin", "count": 8 }
    ],
    "repository": "mole",
    "via_confidence": "EXACT_TYPECHECKED",
    "groups": [
      {
        "kind": "method",
        "hop_depth": 1,
        "reached_from": "Server",
        "via_kind": "TYPE_USES",
        "via_provenance": "GO_TYPES_USE",
        "files": [{ "file": "internal/admin/admin.go", "at": [
          "Server.WithPortController@89-92", "Server.handleStatus@106-129",
          "Server.Handler@95-104", "Server.WithPorts@81-84",
          "Server.handlePortAdd@150-169", "Server.handleHealth@131-134",
          "Server.handlePortDelete@175-188"
        ] }]
      },
      {
        "kind": "func",
        "hop_depth": 1,
        "reached_from": "Server",
        "via_kind": "TYPE_USES",
        "via_provenance": "GO_TYPES_USE",
        "files": [{ "file": "internal/admin/admin.go", "at": ["New@54-56"] }]
      },
      {
        "kind": "func",
        "hop_depth": 2,
        "reached_from": "Server.WithPortController",
        "via_kind": "CALLS_DIRECT",
        "via_provenance": "GO_AST_CALL",
        "files": [{ "file": "cmd/mole/main.go", "at": ["runUp@117-321"] }]
      }
    ]
  }
}
```

The `completeness` block is cut here with `{ … }` for the same reason the first
example cuts it: it is not the field this page is illustrating, and
[`completeness`](#completeness) below spells it in full elsewhere. `repository`
and `via_confidence` still hoist to the page header -- everything affected sits
in `mole`, reached by an edge a type checker resolved -- but `kind` splits three
ways and `hop_depth` with it, so every group states its own pair instead of
forcing one row, `Server.WithPortController@89-92`, to carry a residual tail
the other eight rows do not need. The third group is alone because it is the
only symbol reached at depth `2`; grouping still applies to a group of one when
the alternative is a page that agrees on nothing.

### The four axes

Every axis is computed over the whole traversal, not over the page, so the
numbers stay stable while you page through the rows. All four count the same
filtered set as `affected`.

The compact view writes the three counting axes as one object each -- `by_depth`
keyed by the depth, `by_kind` by the relation kind, `by_repository` by the
repository name -- and keeps `by_package` as rows, because two packages of the
same name in different repositories are two facts an object keyed by name would
add together. A compact package row carries `package` and `count`, plus
`repository` only when the package is outside the hoisted one; the package key is
`package:<language>:<repository>:<name>`, which the row already spells, and the
full view keeps it. An axis that counted nothing is absent rather than empty.

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
this tool produces none, so the full view says `0` and the compact view leaves
the counter out, as it does any counter at zero. Only
[`find_cross_repo_consumers`](/docs/tools/find-cross-repo-consumers/) fills
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

- `truncated` is about the page of rows. It is `true` when rows remain, and
  `next_cursor` then carries the token to continue. The aggregates do not page
  and do not change between pages. On a complete page the compact view omits both
  fields rather than writing `false` and `null`.
- `traversal_truncated` is about the walk. It is `true` when the walk hit
  `max_nodes` and stopped discovering symbols, so the impact itself is
  understated and no cursor will recover it. Raise `max_nodes`, lower `depth`,
  or narrow with `edge_kinds` or `confidence`. In the captured answer both are
  `false`, which is why the compact payload carries neither.

To take the next page, repeat the same call and add `cursor` set to the
`next_cursor` value, unchanged. The token is an opaque base64url wrapper around a
binary body: the format version, the snapshot id, the offset, a digest of the
query identity, a digest of the sorting contract `blast-radius-v1` and a checksum
over all of it -- about 31 characters, where the previous format spent 314
characters of base64-wrapped JSON. It fails closed:

| What changed | What you get |
| --- | --- |
| A new generation was published | `CURSOR_SNAPSHOT_EXPIRED`; restart pagination |
| The selector, `depth`, `max_nodes`, `edge_kinds`, `confidence` or `kinds` differ from the call that produced it | `CURSOR_INVALID` |
| The token was edited, re-encoded, truncated, or decodes with trailing bytes after the checksum | `CURSOR_INVALID` |
| The token was minted by a server of the previous cursor version, or before `kinds` existed | `CURSOR_INVALID`; the body declares version 2 and the default filter is part of the identity, so an older page fails closed instead of resuming a different set |

`limit`, `view`, `response_format` and `include_derived` are not part of the
cursor identity, so changing one of them mid-pagination is accepted: the same
cursor continues the impact in another view, and changing `limit` shifts what the
remaining pages contain. `kinds` is the opposite: it decides which rows exist, so
it is in the identity.

`guidance` appears only when the count alone would mislead: on a truncated page,
naming `depth`, `max_nodes`, `edge_kinds` and `confidence` as the ways to
narrow, and on zero rows, where it says the traversal reached nothing within its
bounds and suggests raising `depth` or asking
[`find_references`](/docs/tools/find-references/) for the direct relations
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
