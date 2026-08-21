---
title: find_references
description: Type-checked incoming or outgoing references for one symbol, with the edge kind, confidence and provenance in the header when the page agrees on them and on the row when it does not.
---

> Who calls or references a symbol. Type-checked, not name-matched: grep cannot separate homonyms, and an empty answer means nobody calls it. A rare name in one repository is cheaper to grep. A bare name suffices: an ambiguous one returns its candidates, so no lookup call first. `view: "files"` answers which files without a line each.

## Arguments

| Argument | Type | Default | Meaning |
| --- | --- | --- | --- |
| `confidence` | string | none | Keeps only edges carrying exactly this confidence. Accepted: `EXACT_TYPECHECKED`, `EXACT_DECLARATION_MAPPED`, `EXACT_PACKAGE_MAPPED`, `STRUCTURAL_CERTAIN`, `CANDIDATE`, `UNRESOLVED`. One value, not a list. Anything else is rejected with `INVALID_ARGUMENT`. |
| `cursor` | string | none | Opaque token taken from `next_cursor`. Resumes the same query at the next offset. |
| `direction` | string | `incoming` | `incoming` returns the symbols that reference this one. `outgoing` returns the ones it reaches. Any other value is rejected with `INVALID_ARGUMENT`. |
| `edge_kinds` | array of string | none, meaning every reference kind except `EXPORTS` and `REEXPORTS` | Restricts rows to these relations. The default leaves out the forwarding bindings: an `export { x }` or `export { x } from "./y"` names a path to the declaration rather than a use of it, and the response says so under `edge_kinds_default_excluded`. Nothing becomes unreachable -- the checker resolves an import through however many barrels stand in the way, so every consumer behind one carries its own `IMPORTS_SYMBOL` edge and is listed without them. A list replaces that default; `["*"]` reports every reference kind, and the wildcard stands alone since mixing it with names asks for a filter and for no filter at once. Accepted: `IMPORTS_SYMBOL`, `EXPORTS`, `REEXPORTS`, `REFERENCES`, `CALLS_DIRECT`, `PASSES_AS_CALLBACK`, `ASSIGNS_FUNCTION`, `RETURNS_FUNCTION`, `TYPE_USES`, `IMPLEMENTS`, `EXTENDS`, `EMBEDS`, `OVERRIDES`. Containment and package kinds are rejected. Duplicates collapse; an empty or space-padded entry is rejected. |
| `include_derived` | boolean | `false` | Includes rows from providers Kivgraph derives from the machine, which take the `rust:` namespace, such as a Rust toolchain's standard library. Naming one of them in `repo` has the same effect. |
| `language` | string | none | Keeps rows whose symbol carries this language: `go`, `typescript` or `rust`. Compared exactly; surrounding whitespace is rejected. |
| `limit` | integer | `50` | Rows in this page. Must be between 1 and 500. |
| `name` | string | none | The unqualified name of the declaration to ask about, so the common question costs one call instead of two. One declaration carrying that name is answered directly; several are rejected with `AMBIGUOUS_SYMBOL` naming the candidates as `repository:path:line`. Cannot be combined with `stable_key` or `qualified_name`; `repository`, or `repository` plus `path`, narrows it. A name that is only imported or re-exported and never declared is `SYMBOL_NOT_FOUND`, because the declaration is what a reference question is about. |
| `path` | string | none | Repository-relative path narrowing `qualified_name`. Requires `repository`. Rejected together with `stable_key`. |
| `qualified_name` | string | none | Names the symbol to ask about. One of `qualified_name`, `stable_key` or `name` is required, never two of them. A name matching more than one symbol is rejected with `AMBIGUOUS_SYMBOL` instead of being resolved silently. |
| `repo` | string | none | Keeps only rows belonging to this repository, by name, compared exactly. It filters the answer; `repository` selects the subject. |
| `repository` | string | none | Repository of the symbol being asked about. Narrows `qualified_name`. |
| `response_format` | string | `concise` | `concise` or `detailed`. Detailed adds the derived identifiers -- `stable_key`, `file_key`, `repository_key` and `evidence_kind` -- to the rows of the `full` view. The compact and files views carry none of them under either format: a row is addressed by the header's repository plus its own path and qualified name. Anything else is rejected with `INVALID_ARGUMENT`. |
| `stable_key` | string | none | Names the symbol directly. Cannot be combined with `qualified_name`, `name`, `repository` or `path`. |
| `view` | string | `compact` | The granularity of the answer, never a different answer. `compact` lifts into a header what every row shares and groups the rows by file. `full` is the field-per-row shape. `files` answers only which files hold references and how many each holds, and raises the default `limit` to 500 so a file list is never cut by a page. Any other value is rejected with `INVALID_ARGUMENT`. |

## Answers

Who calls or references one symbol, and what that symbol reaches directly. It is
a single hop, not a walk: for a chain use
[`trace_dependencies`](/reference/tools/trace-dependencies/) or
[`get_blast_radius`](/reference/tools/get-blast-radius/). The response states the
subject once, echoes the `direction` it answered in, and returns one page of the
matching edges. Each row names a symbol with its repository, repository-relative
path, qualified name and line range, so the next call is built from the answer just
received.

By default the answer arrives in the `compact` view: what every row shares is
stated once in the header and the rows are grouped by file. It is the same
edges, with the same confidence and the same provenance, as the `full` view --
`confidence` and `provenance` alone were `1.200` of the `4.236` tokens of one
fifty-row page over `kena`, the same pair on all fifty rows. Measured over that
corpus, the page went from `4.236` to `874` tokens.

That page-wide hoist is unanimous or nothing: on a real `66`-row page, `65`
rows shared `kind` and `edge_kind` and the `66`th, a re-export, was enough to
push both columns back down to every row. `results.groups` is the second tier
that catches it, grouping the rows by whatever exact tuple of the remaining
columns they still share instead of `files` repeating it row by row. Measured
over the same two `kena` questions, `1.205` and `1.143` tokens fell to `788`
and `779`. See [when a page groups](/mcp/usage/#when-a-page-groups) for the
mechanism shared by six tools, and [reading a grouped page](#reading-a-grouped-page)
below for a captured page.

## Example

Incoming references, three at a time. Nothing asks for a `view`, so this is the
`compact` answer a caller gets by default:

```json
{
  "repository": "kivgraph",
  "path": "internal/facts/facts.go",
  "qualified_name": "MergeAll",
  "direction": "incoming",
  "limit": 3
}
```

```json
{
  "snapshot_id": 30,
  "total": 3,
  "returned": 3,
  "coverage": {
    "exact": 3
  },
  "results": {
    "subject": "kivgraph:internal/facts/facts.go:516",
    "qn": "MergeAll",
    "direction": "incoming",
    "repository": "kivgraph",
    "edge_kind": "CALLS_DIRECT",
    "confidence": "EXACT_TYPECHECKED",
    "provenance": "GO_AST_CALL",
    "files": [
      {
        "file": "internal/indexer/full.go",
        "at": [
          ["mergeSets@681-712", "func"],
          ["closeCrossRepositoryEdges@735-783", "func"]
        ]
      },
      {
        "file": "internal/facts/facts.go",
        "at": [["Set.Merge@505-507", "method"]]
      }
    ]
  }
}
```

All three rows are calls a type checker resolved, so `edge_kind`, `confidence`
and `provenance` are in the header rather than on each row. `kind` is not: two
of the callers are functions and one is a method, so each row carries its own.

The same question, the same three edges, in the `full` view -- the field-per-row
shape, which is what a client written against the previous output should ask for
explicitly:

```json
{
  "repository": "kivgraph",
  "path": "internal/facts/facts.go",
  "qualified_name": "MergeAll",
  "direction": "incoming",
  "limit": 3,
  "view": "full"
}
```

```json
{
  "snapshot_id": 30,
  "snapshot_age_ms": 9020,
  "total": 3,
  "returned": 3,
  "truncated": false,
  "next_cursor": null,
  "coverage": {
    "exact": 3,
    "candidate": 0,
    "unresolved_related": 0,
    "package_level": 0
  },
  "results": {
    "subject": {
      "stable_key": "KHXAWFM5ED2YEIFIEMB5NALA7L7YXNHSUJEBLU7SDAMLGKX24UAA",
      "name": "MergeAll",
      "qualified_name": "MergeAll",
      "kind": "func",
      "repository": "kivgraph",
      "file_path": "internal/facts/facts.go",
      "start_line": 516
    },
    "direction": "incoming",
    "references": [
      {
        "name": "mergeSets",
        "qualified_name": "mergeSets",
        "kind": "func",
        "repository": "kivgraph",
        "file_path": "internal/indexer/full.go",
        "start_line": 681,
        "end_line": 712,
        "language": "go",
        "edge_kind": "CALLS_DIRECT",
        "confidence": "EXACT_TYPECHECKED",
        "provenance": "GO_AST_CALL"
      },
      {
        "name": "closeCrossRepositoryEdges",
        "qualified_name": "closeCrossRepositoryEdges",
        "kind": "func",
        "repository": "kivgraph",
        "file_path": "internal/indexer/full.go",
        "start_line": 735,
        "end_line": 783,
        "language": "go",
        "edge_kind": "CALLS_DIRECT",
        "confidence": "EXACT_TYPECHECKED",
        "provenance": "GO_AST_CALL"
      },
      {
        "name": "Merge",
        "qualified_name": "Set.Merge",
        "kind": "method",
        "repository": "kivgraph",
        "file_path": "internal/facts/facts.go",
        "start_line": 505,
        "end_line": 507,
        "language": "go",
        "edge_kind": "CALLS_DIRECT",
        "confidence": "EXACT_TYPECHECKED",
        "provenance": "GO_AST_CALL"
      }
    ]
  }
}
```

The same symbol, the other direction, with a page smaller than the answer:

```json
{
  "repository": "kivgraph",
  "path": "internal/facts/facts.go",
  "qualified_name": "MergeAll",
  "direction": "outgoing",
  "limit": 2
}
```

```json
{
  "snapshot_id": 30,
  "total": 66,
  "returned": 2,
  "truncated": true,
  "next_cursor": "Ah4CAHjbgtIy3kG-GzdYiDefgr2UKDg",
  "coverage": {
    "exact": 66
  },
  "guidance": "showing 2 of 66; narrow with edge_kinds, confidence, repo or language, or pass the cursor for the next page",
  "results": {
    "subject": "kivgraph:internal/facts/facts.go:516",
    "qn": "MergeAll",
    "direction": "outgoing",
    "repository": "kivgraph",
    "kind": "field",
    "edge_kind": "REFERENCES",
    "confidence": "EXACT_TYPECHECKED",
    "files": [
      {
        "file": "internal/facts/facts.go",
        "at": [
          ["Set.Symbols@251", "GO_TYPES_USE"],
          ["Set.Symbols@251", "GO_TYPES_SELECTION"]
        ]
      }
    ]
  }
}
```

Here `provenance` stayed on the rows. The two entries are the same symbol on the
same line under the same relation, observed twice by two different mechanisms,
so no single value covered the page and each row carries its own as the tail of
an array. `truncated` and `next_cursor` are present because rows remain; on the
complete page above, both were absent rather than `false` and `null`.

The same page in the `full` view:

```json
{
  "repository": "kivgraph",
  "path": "internal/facts/facts.go",
  "qualified_name": "MergeAll",
  "direction": "outgoing",
  "limit": 2,
  "view": "full"
}
```

```json
{
  "snapshot_id": 30,
  "snapshot_age_ms": 21769,
  "total": 66,
  "returned": 2,
  "truncated": true,
  "next_cursor": "Ah4CAHjbgtIy3kG-GzdYiDefgr2UKDg",
  "coverage": {
    "exact": 66,
    "candidate": 0,
    "unresolved_related": 0,
    "package_level": 0
  },
  "guidance": "showing 2 of 66; narrow with edge_kinds, confidence, repo or language, or pass the cursor for the next page",
  "results": {
    "subject": {
      "stable_key": "KHXAWFM5ED2YEIFIEMB5NALA7L7YXNHSUJEBLU7SDAMLGKX24UAA",
      "name": "MergeAll",
      "qualified_name": "MergeAll",
      "kind": "func",
      "repository": "kivgraph",
      "file_path": "internal/facts/facts.go",
      "start_line": 516
    },
    "direction": "outgoing",
    "references": [
      {
        "name": "Symbols",
        "qualified_name": "Set.Symbols",
        "kind": "field",
        "repository": "kivgraph",
        "file_path": "internal/facts/facts.go",
        "start_line": 251,
        "end_line": 251,
        "language": "go",
        "edge_kind": "REFERENCES",
        "confidence": "EXACT_TYPECHECKED",
        "provenance": "GO_TYPES_USE"
      },
      {
        "name": "Symbols",
        "qualified_name": "Set.Symbols",
        "kind": "field",
        "repository": "kivgraph",
        "file_path": "internal/facts/facts.go",
        "start_line": 251,
        "end_line": 251,
        "language": "go",
        "edge_kind": "REFERENCES",
        "confidence": "EXACT_TYPECHECKED",
        "provenance": "GO_TYPES_SELECTION"
      }
    ]
  }
}
```

### The `files` view

When the question is which files to open, the rows are noise: seven calls in one
file are one file. `view: "files"` answers with the file list and a count each,
and it pages at 500 by default so the list is whole:

```json
{
  "repository": "kivgraph",
  "path": "internal/facts/facts.go",
  "qualified_name": "MergeAll",
  "direction": "incoming",
  "view": "files"
}
```

```json
{
  "snapshot_id": 30,
  "total": 3,
  "returned": 3,
  "coverage": {
    "exact": 3
  },
  "results": {
    "subject": "kivgraph:internal/facts/facts.go:516",
    "qn": "MergeAll",
    "direction": "incoming",
    "files": [
      { "file": "kivgraph/internal/indexer/full.go", "count": 2 },
      { "file": "kivgraph/internal/facts/facts.go", "count": 1 }
    ]
  }
}
```

A `file` here is prefixed with its repository, because a file list spanning two
repositories has no header to hoist the repository into. Over the four reference
questions of `benchmarks/codebase-memory-comparison`, this view answered in `963`
tokens against the `2.883` of the compact rows and the `13.594` of the previous
shape.

### One call instead of two

`name` resolves the declaration itself, so a reference question does not have to
be preceded by a [`find_symbol`](/reference/tools/find-symbol/) call:

```json
{
  "name": "MergeAll",
  "direction": "incoming"
}
```

That answers exactly the page above, because one symbol in the graph declares
`MergeAll`. When several do, the tool refuses to pick and says which ones it
refused between:

```text
AMBIGUOUS_SYMBOL: name "withRetry" declares 7 symbols; repeat with the repository and path of the one you mean: kena:apps/api/src/retry.ts:14, kena:apps/web/src/lib/retry.ts:22, …
```

The message lists all seven; the two above are the shape, the rest are cut here
with the `…`. Those candidates are the same `repository:path:line` triple every
tool accepts, so narrowing is a matter of copying one of them into `repository`
and `path`. The
refusal costs `49` to `144` tokens over `kena`, against the `2.293` of listing
every `find_symbol` row for the name, imports and re-exports included. A name
the graph knows only as an import or a re-export is `SYMBOL_NOT_FOUND` with the
same instruction: name the repository and path that declares it.

All the answers above come from snapshot `30` of two repositories, `kivgraph`
and `mole`.

## Reading the result

`subject` is the symbol the query resolved to, stated once rather than on every
row, and it is worth checking: it is the proof that the selector picked the
symbol you meant. The compact view spells it as the `repository:path:line`
triple in one string, with the resolved qualified name beside it in `qn`; the
full view spells the same facts as an object of fields. `direction` echoes which
question was answered, so a cached response cannot be misread later.

`dispatch_through` is present when the subject is the one implementation of an
interface method and the answer therefore also holds the references to it: a call
through the interface can reach nothing else, so leaving them out answered that
nothing called the implementation. Every row that arrived that way repeats the
interface method in `via`, which hoists and groups like the other columns, so a
bridged row is never read as a direct call. With two implementations nothing is
bridged -- a call reaches one of them, and naming both would trade a false
absence for a false presence -- and
[`get_blast_radius`](/reference/tools/get-blast-radius/) crosses `IMPLEMENTS` in
either direction regardless.

`edge_kinds_default_excluded` is present only when a filter you did not ask for
ran. Its value, `["EXPORTS", "REEXPORTS"]`, is what the answer left out: the
export bindings that forward the name rather than use it. It is on the page and
not on a row because it describes the query, and `total` counts what the answer
holds, so a filtered page never reports a larger number than it can show. Pass
`edge_kinds: ["*"]` to turn the filter off, or name `REEXPORTS` to ask only for
the barrels -- which is the question a rename has, since a rename must edit
them.

### Reading a compact row

The header holds what every row of the page shares: any of `repository`, `kind`,
`edge_kind`, `confidence` and `provenance`. A field missing from the header is
not a field nobody knows -- it means the rows disagreed and each carries its
own. `confidence` and `provenance` are therefore always readable in one of the
two places, never in neither.

`files` groups the rows by the file holding them. Inside a group, an entry of
`at` is one of two shapes:

- `qualified_name@start`, or `qualified_name@start-end` when the declaration
  spans more than one line. Nothing was left over: the header answers the rest.
- An array whose first element is that same label, when the row had to carry a
  column the header could not hoist. The remaining elements are appended in a
  fixed order -- `edge_kind`, `kind`, `confidence`, `provenance` -- skipping
  every column the header already states. That is why
  `["mergeSets@681-712", "func"]` above is a kind and
  `["Set.Symbols@251", "GO_TYPES_USE"]` is a provenance: the header says which
  columns are still on the rows, and the order says which is which.

A group carries a `repo` of its own only when its file is not in the header's
repository, which on a single-repository answer never happens.

The triple for the next call is assembled from both levels: `repository` from
the header (or the group's `repo` when it has one), `path` from the group's
`file`, and the qualified name and line from the label. That is the same
`repository` + `path` + `qualified_name` every other tool accepts, so no row
needs a stable key to be followed.

Two things a compact row never carries. `language` is gone, because the file
extension beside it says the same thing. `name` is gone whenever the qualified
name already ends with it, which for a Go or TypeScript declaration it does.

Each row carries three facts describing the relationship and not the symbol --
in the header when the whole page agrees on them, on the row when it does not:

- `edge_kind` is the relation. `CALLS_DIRECT` above is a call site;
  `REFERENCES` is a use that is not a call. The full accepted vocabulary is the
  `edge_kinds` list in the table above.
- `confidence` is how well the relation is proven. `EXACT_TYPECHECKED` means a
  type checker resolved it. `CANDIDATE` is plausible and not proven, and
  `UNRESOLVED` carries no target identity at all; they are distinct results
  from an exact edge and are never promoted into one.
- `provenance` is the mechanism that produced it. `GO_AST_CALL` is a call
  expression over a `go/types` resolution; `GO_TYPES_USE` and
  `GO_TYPES_SELECTION` are two different observations of a use. Two rows for
  the same symbol pair with different provenance, as in the outgoing example
  above, are two observed facts, not a duplicate.

The line range -- `start_line` and `end_line` in the full view, the `@start-end`
of a compact label -- bounds the declaration of the symbol holding the reference,
never the position of the token. The snapshot records which symbol contains a
reference and not where inside it, and publishing a line nobody observed would
be inventing evidence. The range is there so the row can be opened with
[`get_source`](/reference/tools/get-source/) without a second lookup. A label
with a single line, `Set.Symbols@251`, is a declaration that starts and ends
there, not a range somebody dropped.

`coverage` classifies every matching edge, including the ones this page did not
return: `exact` counts edges whose confidence is exact, `candidate` the
plausible ones, `unresolved_related` the related references that carry no
target identity. `package_level` counts facts about a package rather than about
the symbol asked for; this tool produces none. In the full view it stays `0`; in
the compact view a category that counted nothing is absent, and `coverage` as a
whole is absent when all four are, because four zeros only say that the tool has
four counters. Only
[`find_cross_repo_consumers`](/reference/tools/find-cross-repo-consumers/) fills
it, because a package dependency proves a dependency on the provider and never a
use of the symbol.

No `completeness` object appears on this tool. Absent means it did not check how
far the answer reaches, which is not the same as checking and finding nothing.
[`get_blast_radius`](/reference/tools/get-blast-radius/) is the tool that
checks.

An empty answer -- an empty `files` list in the compact view, an empty
`references` list in the full one -- with `total` at `0` is a proven absence, not
a miss.
The edges came from `go/types`, the TypeScript checker and `rust-analyzer`, not
from matching names, so nothing referencing the symbol was left out by a
spelling. Grep cannot make that claim. The response says so itself, in
`guidance`.

### Reading a grouped page

A homonym with declarations of two kinds in two repositories, so no single
`kind` covers the page:

```json
{
  "name": "NewRegistry",
  "repository": "kivgraph",
  "path": "internal/workspace/registry.go",
  "direction": "incoming",
  "limit": 100
}
```

```json
{
  "snapshot_id": 1,
  "total": 6,
  "returned": 6,
  "coverage": { "exact": 6 },
  "results": {
    "subject": "kivgraph:internal/workspace/registry.go:68",
    "qn": "NewRegistry",
    "direction": "incoming",
    "repository": "kivgraph",
    "edge_kind": "CALLS_DIRECT",
    "confidence": "EXACT_TYPECHECKED",
    "provenance": "GO_AST_CALL",
    "groups": [
      {
        "kind": "method",
        "files": [
          { "file": "internal/indexing/service.go", "at": ["Service.IndexProjects@140-232", "Service.Reindex@241-277"] }
        ]
      },
      {
        "kind": "func",
        "files": [
          { "file": "cmd/kivgraph/main.go", "at": ["runDoctor@1164-1289", "resyncOnBranchChange@432-490", "runUpgrade@973-1050", "runIndexFull@786-903"] }
        ]
      }
    ]
  }
}
```

All six calls are the same relation with the same confidence from the same
mechanism, so `edge_kind`, `confidence` and `provenance` still hoist to the
page header exactly as on a flat answer. `kind` is what breaks the unanimous
vote -- two methods against four functions -- so `results.groups` replaces
`results.files`, and each group states its own `kind` once instead of
repeating it on six rows. Inside a group, `files` reads exactly like the flat
view: a `file` and a label per declaration, becoming an array only if a row
still disagrees with its own group. Neither `total`, `returned` nor `coverage`
moved for grouping; six rows are six rows either way.

## Limits

`truncated` is `true` when rows remain after this page, and `next_cursor` then
carries the token to continue. When the page holds everything the full view says
`truncated: false` and `next_cursor: null`, and the compact view omits both
fields: a false flag and a cursor that does not exist are not facts worth a
line. This tool walks no graph, so it has no
`traversal_truncated` field; that one belongs to
[`trace_dependencies`](/reference/tools/trace-dependencies/) and
[`get_blast_radius`](/reference/tools/get-blast-radius/), where the bound is on
the walk rather than on the page.

The cursor is an opaque base64url token over a binary body: the format version,
the snapshot id, the offset, a digest of the query identity, a digest of the
sorting contract `references-v1` and a checksum over all of it. It is about 31
characters -- `Ah4CAHjbgtIy3kG-GzdYiDefgr2UKDg` above -- where the previous
format spent 314 characters of base64-wrapped JSON, `221` tokens on every
truncated page. Pass it back unchanged. It fails closed:

| What changed | What you get |
| --- | --- |
| A new generation was published | `CURSOR_SNAPSHOT_EXPIRED`; restart pagination |
| The selector, `direction`, `repo`, `language`, `edge_kinds` or `confidence` differ from the call that produced it | `CURSOR_INVALID` |
| The token was edited, re-encoded, truncated, or decodes with trailing bytes after the checksum | `CURSOR_INVALID` |
| The token was minted by a server of the previous cursor version | `CURSOR_INVALID`; the body is version 2 and version 1 is not reinterpreted under the new layout |

`limit`, `view`, `response_format` and `include_derived` are not part of the
cursor identity, so changing one of them mid-pagination is accepted: the same
cursor can continue a query in a different view, and changing `limit` shifts what
the remaining pages contain. A page taken through `name` is bound to the
qualified name the name resolved to, not to the name, so it keeps working while
the snapshot does.

`guidance` is present only when the count alone would mislead: when nothing was
found, and when the page is truncated. It stays absent on a complete non-empty
answer. The zero-row sentence differs by direction, and both are worth reading
literally: incoming says the absence is type-checked and points at
[`find_cross_repo_consumers`](/reference/tools/find-cross-repo-consumers/) and
[`graph_status`](/reference/tools/graph-status/); outgoing says the symbol
reaches nothing and suggests asking the other direction.

`include_derived` is `false` by default and that default is load-bearing. With a
Rust toolchain in the graph, references to a name like `Clone` reach most of the
corpus, and a page of the standard library is not what a question about your own
code wants. Withholding those rows is a decision about the page and never a
claim about the graph: the edge stays published with its exact confidence, and
`include_derived: true`, or naming the provider in `repo`, brings it back.

## Where it loses

A rare name in one small repository is cheaper to grep, and this tool costs a
symbol resolution before it answers. It is a single hop, so it cannot tell you
what happens two calls away. It also depends on the snapshot being current: if
the tree moved since it was indexed, the answer describes the code that was
indexed, not the code on disk. [`graph_status`](/reference/tools/graph-status/)
reports that.
