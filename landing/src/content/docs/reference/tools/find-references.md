---
title: find_references
description: Type-checked incoming or outgoing references for one symbol, each row carrying its edge kind, confidence and provenance.
---

> Who calls or references a symbol. Type-checked, not name-matched: grep cannot separate homonyms, and an empty answer means nobody calls it. A rare name in one repository is cheaper to grep.

## Arguments

| Argument | Type | Default | Meaning |
| --- | --- | --- | --- |
| `confidence` | string | none | Keeps only edges carrying exactly this confidence. Accepted: `EXACT_TYPECHECKED`, `EXACT_DECLARATION_MAPPED`, `EXACT_PACKAGE_MAPPED`, `STRUCTURAL_CERTAIN`, `CANDIDATE`, `UNRESOLVED`. One value, not a list. Anything else is rejected with `INVALID_ARGUMENT`. |
| `cursor` | string | none | Opaque token taken from `next_cursor`. Resumes the same query at the next offset. |
| `direction` | string | `incoming` | `incoming` returns the symbols that reference this one. `outgoing` returns the ones it reaches. Any other value is rejected with `INVALID_ARGUMENT`. |
| `edge_kinds` | array of string | none, meaning every reference kind | Restricts rows to these relations. Accepted: `IMPORTS_SYMBOL`, `EXPORTS`, `REEXPORTS`, `REFERENCES`, `CALLS_DIRECT`, `PASSES_AS_CALLBACK`, `ASSIGNS_FUNCTION`, `RETURNS_FUNCTION`, `TYPE_USES`, `IMPLEMENTS`, `EXTENDS`, `EMBEDS`, `OVERRIDES`. Containment and package kinds are rejected. Duplicates collapse; an empty or space-padded entry is rejected. |
| `include_derived` | boolean | `false` | Includes rows from providers Kivgraph derives from the machine, which take the `rust:` namespace, such as a Rust toolchain's standard library. Naming one of them in `repo` has the same effect. |
| `language` | string | none | Keeps rows whose symbol carries this language: `go`, `typescript` or `rust`. Compared exactly; surrounding whitespace is rejected. |
| `limit` | integer | `50` | Rows in this page. Must be between 1 and 500. |
| `path` | string | none | Repository-relative path narrowing `qualified_name`. Requires `repository`. Rejected together with `stable_key`. |
| `qualified_name` | string | none | Names the symbol to ask about. Either this or `stable_key` is required, never both. A name matching more than one symbol is rejected with `AMBIGUOUS_SYMBOL` instead of being resolved silently. |
| `repo` | string | none | Keeps only rows belonging to this repository, by name, compared exactly. It filters the answer; `repository` selects the subject. |
| `repository` | string | none | Repository of the symbol being asked about. Narrows `qualified_name`. |
| `response_format` | string | `concise` | `concise` or `detailed`. Detailed adds the derived identifiers, such as stable keys and file keys. Anything else is rejected with `INVALID_ARGUMENT`. |
| `stable_key` | string | none | Names the symbol directly. Cannot be combined with `qualified_name`, `repository` or `path`. |

## Answers

Who calls or references one symbol, and what that symbol reaches directly. It is
a single hop, not a walk: for a chain use
[`trace_dependencies`](/reference/tools/trace-dependencies/) or
[`get_blast_radius`](/reference/tools/get-blast-radius/). The response states the
subject once, echoes the `direction` it answered in, and lists one row per
matching edge. Each row names a symbol with its repository, repository-relative
path, qualified name and line range, so the next call is built from the answer
just received.

## Example

Incoming references, three at a time:

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
  "snapshot_age_ms": 21769,
  "total": 66,
  "returned": 2,
  "truncated": true,
  "next_cursor": "eyJ2ZXJzaW9uIjoxLCJzbmFwc2hvdF9pZCI6MzAsInF1ZXJ5X2hhc2giOiI3NTQ1NDMwNjBmMGY5YzU0NTY5YjYxZDM1YTFlYjA0ZmJlODUzYmYwYmRkZjUwNjhjNjM3N2Q5NjE5ZGVkODg0Iiwib2Zmc2V0IjoyLCJzb3J0aW5nX3ZlcnNpb24iOiJyZWZlcmVuY2VzLXYxIiwiY2hlY2tzdW0iOiJlNzJjMzllNjZlNWI5MWUzNmQ5M2YwZWQyYTkzZGE0YTBkMzQ2MzI2NmI2NjFhNTg5NjRlNWM0ZmZiODU1MTkxIn0",
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

Both answers come from snapshot `30` of two repositories, `kivgraph` and
`mole`.

## Reading the result

`subject` is the symbol the query resolved to. It is stated once, not repeated
on every row, and it is worth checking: it is the proof that
`repository` + `path` + `qualified_name` picked the symbol you meant.
`direction` echoes which question was answered, so a cached response cannot be
misread later.

Each row carries three fields describing the relationship, not the symbol:

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

`start_line` and `end_line` bound the declaration of the symbol holding the
reference, never the position of the token. The snapshot records which symbol
contains a reference and not where inside it, and publishing a line nobody
observed would be inventing evidence. The range is there so the row can be
opened with [`get_source`](/reference/tools/get-source/) without a second
lookup.

`coverage` classifies every matching edge, including the ones this page did not
return: `exact` counts edges whose confidence is exact, `candidate` the
plausible ones, `unresolved_related` the related references that carry no
target identity. `package_level` counts facts about a package rather than about
the symbol asked for; this tool produces none, so it stays `0`. Only
[`find_cross_repo_consumers`](/reference/tools/find-cross-repo-consumers/) fills
it, because a package dependency proves a dependency on the provider and never a
use of the symbol.

No `completeness` object appears on this tool. Absent means it did not check how
far the answer reaches, which is not the same as checking and finding nothing.
[`get_blast_radius`](/reference/tools/get-blast-radius/) is the tool that
checks.

An empty `references` list with `total` at `0` is a proven absence, not a miss.
The edges came from `go/types`, the TypeScript checker and `rust-analyzer`, not
from matching names, so nothing referencing the symbol was left out by a
spelling. Grep cannot make that claim. The response says so itself, in
`guidance`.

## Limits

`truncated` is `true` when rows remain after this page, and `next_cursor` then
carries the token to continue. When the page holds everything, `truncated` is
`false` and `next_cursor` is `null`. This tool walks no graph, so it has no
`traversal_truncated` field; that one belongs to
[`trace_dependencies`](/reference/tools/trace-dependencies/) and
[`get_blast_radius`](/reference/tools/get-blast-radius/), where the bound is on
the walk rather than on the page.

The cursor is an opaque base64url token wrapping a versioned body: the format
version, the snapshot id, a hash of the query, the offset, the sorting version
`references-v1` and a checksum over all of it. Pass it back unchanged. It fails
closed:

| What changed | What you get |
| --- | --- |
| A new generation was published | `CURSOR_SNAPSHOT_EXPIRED`; restart pagination |
| The selector, `direction`, `repo`, `language`, `edge_kinds` or `confidence` differ from the call that produced it | `CURSOR_INVALID` |
| The token was edited, re-encoded, truncated or carries an unknown field | `CURSOR_INVALID` |

`limit`, `response_format` and `include_derived` are not part of the cursor
identity, so changing one of them mid-pagination is accepted and shifts what the
remaining pages contain.

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
