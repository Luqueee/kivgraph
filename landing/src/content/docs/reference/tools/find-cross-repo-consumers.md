---
title: find_cross_repo_consumers
description: Consumers of one symbol in other repositories, with exact uses counted apart from package-level dependencies.
---

> Consumers of a symbol in other repositories, exact uses kept apart from package-level dependencies. A language server stops at its workspace.

## Arguments

| Argument | Type | Default | Meaning |
| --- | --- | --- | --- |
| `cursor` | string | none | Opaque token taken from `next_cursor`. Resumes the same query at the next offset. |
| `language` | string | none | Keeps only consumers carrying this language: `go`, `typescript` or `rust`. Compared exactly; surrounding whitespace is rejected. |
| `limit` | integer | `50` | Rows in this page. Must be between 1 and 500. |
| `path` | string | none | Repository-relative path narrowing `qualified_name`. Requires `repository`. Rejected together with `stable_key`. |
| `qualified_name` | string | none | Names the symbol to ask about. Either this or `stable_key` is required, never both. A name matching more than one symbol is rejected with `AMBIGUOUS_SYMBOL` instead of being resolved silently. |
| `repo` | string | none | Keeps only consumers in this repository, by name, compared exactly. It filters the answer; `repository` selects the subject. |
| `repository` | string | none | Repository of the symbol being asked about. Narrows `qualified_name`. |
| `response_format` | string | `concise` | `concise` or `detailed`. Detailed adds the derived identifiers, such as consumer symbol, repository, package and file keys. Anything else is rejected with `INVALID_ARGUMENT`. |
| `stable_key` | string | none | Names the symbol directly. Cannot be combined with `qualified_name`, `repository` or `path`. |

## Answers

Who, outside the symbol's own repository, uses it or depends on the package that
declares it. A language server stops at its workspace, so this is the question
no editor answers. The response states the subject once and returns a
`consumers` list where every row carries a `category`, so an exact use is never
mixed with a dependency between packages. Rows inside the symbol's own
repository are excluded by construction; ask
[`find_references`](/reference/tools/find-references/) for those.

## Example

```json
{
  "repository": "ladygraph",
  "path": "internal/facts/facts.go",
  "qualified_name": "MergeAll"
}
```

```json
{
  "snapshot_id": 30,
  "snapshot_age_ms": 9020,
  "total": 0,
  "returned": 0,
  "truncated": false,
  "next_cursor": null,
  "coverage": {
    "exact": 0,
    "candidate": 0,
    "unresolved_related": 0,
    "package_level": 0
  },
  "guidance": "no repository in the published graph consumes this symbol. Check graph_status if a consumer is registered but was not indexed, and find_references for uses inside its own repository",
  "results": {
    "subject": {
      "qualified_name": "MergeAll",
      "repository": "ladygraph",
      "package_name": "github.com/Luqueee/ladygraph/internal/facts",
      "module_path": "github.com/Luqueee/ladygraph",
      "file_path": "internal/facts/facts.go",
      "start_line": 516,
      "end_line": 542
    },
    "consumers": null
  }
}
```

This answer comes from snapshot `30` of two repositories, `ladygraph` and
`mole`.

## Reading the result

The captured answer has no consumers, and that is the interesting case. `total`
is `0`, `consumers` is `null`, and `guidance` says what the zero means: no
repository in the published graph consumes this symbol, plus the two ways the
answer could still be wrong, both of which are about the index and not about the
code. That sentence is how the surface distinguishes a checked absence from an
empty page. Without it, zero rows read as "no such thing" and the session goes
back to grep.

When there are consumers, every row carries a `category`:

| `category` | What it means |
| --- | --- |
| `exact_symbol` | A use of the queried symbol, resolved by a type checker. |
| `candidate` | A relation to the symbol that is plausible and not proven. |
| `package` | A `PACKAGE_DEPENDS_ON` dependency on the package that declares it. |
| `unresolved` | A recorded reference the resolver could not follow, with the strings it asked for. |

An `exact_symbol` or `candidate` row is a reference row and reads like one from
[`find_references`](/reference/tools/find-references/): `edge_kind` is the
relation, `confidence` is how well it is proven, `provenance` is the mechanism
that observed it. It also carries the consumer's `repository`, `package_name`,
`file_path` and line range, so it can be opened without another call.

A `package` row carries no symbol, no file and no range, and it is not an
omission. The evidence is a dependency between packages: it has an `edge_kind`,
a `confidence` and a `provenance` of its own, and it names a consuming
repository and package, because that is all anybody observed.

An `unresolved` row is evidence about a failed request, not a relationship. It
carries `reason`, `requested_package`, `requested_symbol` and `detail`, which
are the strings the resolver used and never graph keys.

### Why `package_level` is counted separately

`coverage` reports four counters, and the split is the point of this page. A
query about a symbol only counts as a consumer what was observed about that
symbol. `exact` counts uses proven by a type checker; `candidate` the plausible
ones; `unresolved_related` the references that named the symbol but could not be
followed. Package dependencies land in `package_level` and are never summed into
`exact`.

The rule behind it is short: a package dependency proves that the consumer
depends on the provider and never that it uses the symbol. Adding it to `exact`
would report a use nobody saw. By the same rule, a resolution failure that named
no symbol at all, an unreadable module or an absent provider, belongs to the
package: it is declared with `requested_package` and never attributed to every
symbol that package exports.

So `package_level` is not a weaker `exact`. It answers a different question:
"could this repository be reaching me at all", where `exact` answers "does it".
A migration plan built on `exact` is a list of call sites; a migration plan built
on `package_level` is a list of places to go and look.

No `completeness` object appears on this tool. Absent means it did not check how
far its answer reaches, which is not the same as checking and finding nothing.
[`get_blast_radius`](/reference/tools/get-blast-radius/) is the tool that
checks.

## Limits

`truncated` is `true` when rows remain after this page, and `next_cursor` then
carries the token to continue; otherwise `truncated` is `false` and
`next_cursor` is `null`. This tool walks no graph, so it has no
`traversal_truncated` field: that one belongs to
[`trace_dependencies`](/reference/tools/trace-dependencies/) and
[`get_blast_radius`](/reference/tools/get-blast-radius/), where the bound is on
the walk rather than on the page.

The cursor is an opaque base64url token wrapping a versioned body: the format
version, the snapshot id, a hash of the query, the offset, the sorting version
`consumers-v1` and a checksum over all of it. Pass it back unchanged. It fails
closed:

| What changed | What you get |
| --- | --- |
| A new generation was published | `CURSOR_SNAPSHOT_EXPIRED`; restart pagination |
| The selector, `repo` or `language` differ from the call that produced it | `CURSOR_INVALID` |
| The token was edited, re-encoded, truncated or carries an unknown field | `CURSOR_INVALID` |

`limit` and `response_format` are not part of the cursor identity, so changing
one of them mid-pagination is accepted and shifts what the remaining pages
contain.

`guidance` appears only when the count alone would mislead: on zero rows, with
the sentence quoted above, and on a truncated page, where it names `repo` and
`language` as the way to narrow. It stays absent on a complete non-empty answer.

This tool takes no `include_derived` argument. Providers derived from the
machine, the ones in the `rust:` namespace, are withheld by default only from
[`find_symbol`](/reference/tools/find-symbol/),
[`find_references`](/reference/tools/find-references/),
[`trace_dependencies`](/reference/tools/trace-dependencies/) and
[`get_blast_radius`](/reference/tools/get-blast-radius/).

## Where it loses

The answer is bounded by what is registered. A consumer nobody indexed produces
no row, and the zero-consumer guidance says so rather than pretending otherwise;
[`graph_status`](/reference/tools/graph-status/) and
[`list_repositories`](/reference/tools/list-repositories/) are where you check.
It also says nothing about uses inside the symbol's own repository, which are
the common case and belong to
[`find_references`](/reference/tools/find-references/). For a single-repository
corpus this tool has nothing to add.
