---
title: find_cross_repo_consumers
description: Consumers of one symbol in other repositories, with exact uses counted apart from package-level dependencies.
---

> Consumers of a symbol in other repositories, exact uses kept apart from package-level dependencies that prove no use. A language server stops at its own workspace and cannot answer this.

## Arguments

| Argument | Type | Default | Meaning |
| --- | --- | --- | --- |
| `profile` | array of strings | configured default | Profiles to query. `["*"]` alone selects all. A stable key requires exactly one named profile when several profiles exist. |
| `cursor` | string | none | Opaque token taken from `next_cursor`. Resumes the same query at the next offset. |
| `language` | string | none | Keeps only consumers carrying this language: `go`, `typescript` or `rust`. Compared exactly; surrounding whitespace is rejected. |
| `limit` | integer | `50` | Rows in this page. Must be between 1 and 500. |
| `path` | string | none | Repository-relative path narrowing `qualified_name`. Requires `repository`. Rejected together with `stable_key`. |
| `qualified_name` | string | none | Names the symbol to ask about. Either this or `stable_key` is required, never both. A name matching more than one symbol is rejected with `AMBIGUOUS_SYMBOL` instead of being resolved silently. |
| `repo` | string | none | Keeps only consumers in this repository, by name, compared exactly. It filters the answer; `repository` selects the subject. |
| `repository` | string | none | Repository of the symbol being asked about. Narrows `qualified_name`. |
| `response_format` | string | `concise` | `concise` or `detailed`. Detailed adds the derived identifiers, such as consumer symbol, repository, package and file keys. Anything else is rejected with `INVALID_ARGUMENT`. |
| `stable_key` | string | none | Names the symbol directly. Cannot be combined with `qualified_name`, `repository` or `path`. |
| `view` | string | `compact` | The granularity of the answer, never a different answer. `compact` lifts the columns every consumer repeats into the header and gives a repository one entry for its package dependencies. `full` is the field-per-row shape. The answer is a set of consuming repositories and not a set of files, so `files` is rejected with `INVALID_ARGUMENT`, as is any other value. |

## Answers

Who, outside the symbol's own repository, uses it or depends on the package that
declares it. A language server stops at its workspace, so this is the question
no editor answers. The response states the subject once and returns a
`consumers` list where every row carries a `category`, so an exact use is never
mixed with a dependency between packages. Rows inside the symbol's own
repository are excluded by construction; ask
[`find_references`](/docs/tools/find-references/) for those.

By default the page arrives in the `compact` view: `category`, `edge_kind`,
`confidence`, `provenance`, `evidence_kind` and `reason` rise into the header
whenever every row that has them agrees, and `requested_package` and
`requested_symbol` do too, because the request is a property of the call and
not of the consumer. A row keeps only what the header does not state. Measured
over a 35-row page on the benchmark corpus, the compact view alone
brought `2.456` tokens down to `2.202`; grouping the rows that still disagreed
took it to `926` -- see [reading a grouped page](#reading-a-grouped-page) below.
That corpus is private, so the repository and package names quoted from it below
are substituted; the counts, the edge kinds and the token figures are the
measured ones.

## Example

```json
{
  "repository": "kivgraph",
  "path": "internal/facts/facts.go",
  "qualified_name": "MergeAll"
}
```

```json
{
  "snapshot_id": 30,
  "total": 0,
  "returned": 0,
  "guidance": "no repository in the published graph consumes this symbol. Check graph_status if a consumer is registered but was not indexed, and find_references for uses inside its own repository",
  "results": {
    "subject": {
      "qualified_name": "MergeAll",
      "at": "kivgraph:internal/facts/facts.go:516",
      "pkg": "github.com/Luqueee/kivgraph/internal/facts",
      "module_path": "github.com/Luqueee/kivgraph",
      "end_line": 542
    },
    "consumers": []
  }
}
```

The subject is the `repository:path:line` triple in `at` with its package in
`pkg`, and `end_line` appears only because the declaration spans more than one
line. There is no `coverage` at all: all four counters were zero, and four zeros
say only that the tool has four counters. `truncated` and `next_cursor` are
absent for the same reason.

The same answer in the `full` view:

```json
{
  "repository": "kivgraph",
  "path": "internal/facts/facts.go",
  "qualified_name": "MergeAll",
  "view": "full"
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
      "repository": "kivgraph",
      "package_name": "github.com/Luqueee/kivgraph/internal/facts",
      "module_path": "github.com/Luqueee/kivgraph",
      "file_path": "internal/facts/facts.go",
      "start_line": 516,
      "end_line": 542
    },
    "consumers": null
  }
}
```

This answer comes from snapshot `30` of two repositories, `kivgraph` and
`go-svc-e`.

## Reading the result

The captured answer has no consumers, and that is the interesting case. `total`
is `0`, `consumers` is an empty list -- `null` in the full view -- and `guidance`
says what the zero means: no
repository in the published graph consumes this symbol, plus the two ways the
answer could still be wrong, both of which are about the index and not about the
code. That sentence is how the surface distinguishes a checked absence from an
empty page. Without it, zero rows read as "no such thing" and the session goes
back to grep.

When there are consumers, every row carries a `category`, in the header when the
whole page shares one and on the row when it does not:

| `category` | What it means |
| --- | --- |
| `exact_symbol` | A use of the queried symbol, resolved by a type checker. |
| `candidate` | A relation to the symbol that is plausible and not proven. |
| `package` | A `PACKAGE_DEPENDS_ON` dependency on the package that declares it. |
| `unresolved` | A recorded reference the resolver could not follow, with the strings it asked for. |

An `exact_symbol` or `candidate` row is a reference row and reads like one from
[`find_references`](/docs/tools/find-references/): `edge_kind` is the
relation, `confidence` is how well it is proven, `provenance` is the mechanism
that observed it. It also carries the consumer's repository, package, file and
line range, so it can be opened without another call. In the compact view the
consumer's repository is `repo`, its package is `pkg`, and the file and line are
one `at` of the form `path:line`, with `end_line` beside it only when the
declaration spans more than one line -- `repo` plus the path in `at` plus
`qualified_name` is the triple the next call takes.

A `package` row carries no symbol, no file and no range, and it is not an
omission. The evidence is a dependency between packages: it has an `edge_kind`,
a `confidence` and a `provenance` of its own, and it names a consuming
repository and package, because that is all anybody observed. The compact view
gives a repository one entry for those dependencies, with `pkg` holding the bare
package name while one package of the repository carries the fact and the list of
names when several do; `total`, `returned` and `coverage` still count every
dependency, so one entry can stand for two package-level facts.

An `unresolved` row is evidence about a failed request, not a relationship. It
carries `reason`, `requested_package`, `requested_symbol` and `detail`, which
are the strings the resolver used and never graph keys. `reason` rises into the
header, or a group's own header, whenever every row that carries it agrees;
`detail` never reaches the page header -- across the whole answer it is
usually several distinct sentences -- but a group built on a shared `reason`
often turns out to share one `detail` too, because the sentence is a template
keyed by the failure and not prose composed per file. `requested_package` and
`requested_symbol` rise into the page header whenever every row that names
them names the same one, because the request is a property of the call and
not of the consumer.

### Reading a grouped page

`category`, `edge_kind`, `confidence`, `provenance`, `evidence_kind` and
`reason` are a page-wide hoist or nothing: mix one package dependency into a
page of exact uses and every one of those six columns drops back onto every
row. `results.groups` is the second tier that catches it, grouping rows by
whichever exact tuple of those six they still share -- never `detail`, which
gets its own hoist attempt once a group is otherwise fixed, exactly as
`reached_from` does for [`get_blast_radius`](/docs/tools/get-blast-radius/).
See [when a page groups](/mcp/usage/#when-a-page-groups) for the mechanism
shared by six tools.

Measured over the real 35-consumer page on the private benchmark corpus that motivated this tier:
`22` package-level dependencies on `@workspace/platform`, each its own repository, all
sharing `category: "package"`, `edge_kind: "PACKAGE_DEPENDS_ON"` and the same
confidence and provenance -- one group, `22` bare `{ "repo", "pkg" }` entries.
The other `13` rows are unresolved imports of the same package, but for two
different reasons: `7` files across one repository fail with
`DECLARATION_SOURCE_NOT_MAPPED`, all naming the identical `.d.ts` path as
`detail`; the other `6` fail with `PROVIDER_SOURCE_UNAVAILABLE` and the
identical sentence "no declaration map places this symbol in the provider's
source". Three groups, one per `(category, reason)` pair actually present,
and both `detail` sentences hoist to their group instead of repeating on `7`
and `6` rows -- the assumption that `detail` was each row's own prose held
for a page small enough that it never had to be tested, and cost `2.202`
tokens more than the `926` the grouped page now spends on the same evidence.

### Why `package_level` is counted separately

`coverage` reports four counters, and the split is the point of this page. A
query about a symbol only counts as a consumer what was observed about that
symbol. `exact` counts uses proven by a type checker; `candidate` the plausible
ones; `unresolved_related` the references that named the symbol but could not be
followed. Package dependencies land in `package_level` and are never summed into
`exact`. The compact view writes only the counters that counted something, and
drops `coverage` entirely when none did, so an absent counter is a zero and never
a category the tool forgot.

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

Every answer carries a `completeness` object whose `verdict` is `COMPLETE` when
nothing the index recorded could add to it and `LOWER_BOUND` when the answer is
a floor, with `blind_spots` for the individual references the resolver could not
follow and `invisible_scopes` for the packages it could not read at all. What
bounds this tool is deliberately wider than the rest: the scope half of the
check is global, so an unreadable package in any repository counts and not only
in the one the question names, because a package nobody could read anywhere is
exactly where an outside consumer hides. That matters most here, because this is
the tool with no native `grep` competitor and its empty answer gets read as a
finding -- "nobody outside uses this" -- so on `LOWER_BOUND` the zero-row
`guidance` refuses that reading and sends you to those two lists first. See
[the completeness verdict](/mcp/usage/#read-the-answer) for the shape shared by
the six tools that check.

## Limits

`truncated` is `true` when rows remain after this page, and `next_cursor` then
carries the token to continue; otherwise the full view says `false` and `null`
and the compact view omits both. This tool walks no graph, so it has no
`traversal_truncated` field: that one belongs to
[`trace_dependencies`](/docs/tools/trace-dependencies/) and
[`get_blast_radius`](/docs/tools/get-blast-radius/), where the bound is on
the walk rather than on the page.

The cursor is an opaque base64url token over a binary body: the format version,
the snapshot id, the offset, a digest of the query identity, a digest of the
sorting contract `consumers-v1` and a checksum over all of it. It is about 31
characters, where the previous format spent 314 of base64-wrapped JSON. Pass it
back unchanged. It fails closed:

| What changed | What you get |
| --- | --- |
| A new generation was published | `CURSOR_SNAPSHOT_EXPIRED`; restart pagination |
| The selector, `repo` or `language` differ from the call that produced it | `CURSOR_INVALID` |
| The token was edited, re-encoded, truncated, or decodes with trailing bytes after the checksum | `CURSOR_INVALID` |
| The token was minted by a server of the previous cursor version | `CURSOR_INVALID`; the body declares version 2 and version 1 is not reinterpreted under the new layout |

`limit`, `view` and `response_format` are not part of the cursor identity, so
changing one of them mid-pagination is accepted: the same cursor continues the
page in another view, and changing `limit` shifts what the remaining pages
contain.

`guidance` appears only when the count alone would mislead: on zero rows, with
the sentence quoted above, and on a truncated page, where it names `repo` and
`language` as the way to narrow. It stays absent on a complete non-empty answer.

This tool takes no `include_derived` argument. Providers derived from the
machine, the ones in the `rust:` namespace, are withheld by default only from
[`find_symbol`](/docs/tools/find-symbol/),
[`find_references`](/docs/tools/find-references/),
[`trace_dependencies`](/docs/tools/trace-dependencies/) and
[`get_blast_radius`](/docs/tools/get-blast-radius/).

## Where it loses

The answer is bounded by what is registered. A consumer nobody indexed produces
no row, and the zero-consumer guidance says so rather than pretending otherwise;
[`graph_status`](/docs/tools/graph-status/) and
[`list_repositories`](/docs/tools/list-repositories/) are where you check.
It also says nothing about uses inside the symbol's own repository, which are
the common case and belong to
[`find_references`](/docs/tools/find-references/). For a single-repository
corpus this tool has nothing to add.
