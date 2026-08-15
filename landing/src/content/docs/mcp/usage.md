---
title: Using the MCP server
description: Route a question to the right Ladygraph tool, address a symbol without a stable key, and read the envelope, guidance and completeness of an answer.
---

`ladygraph serve` speaks MCP over stdio and registers eleven tools. This page is
about using them: which tool answers which question, how to name a symbol, and
how to read what comes back. Per-tool arguments live under
[`/reference/mcp-tools/`](/reference/mcp-tools/) and on each tool's own page.

## What the server tells your agent

The `initialize` result carries an `instructions` string. This is it, literally:

```text
Ladygraph answers "what breaks if I change this" from an exact, published code graph over Go, TypeScript and Rust. Before grepping or reading files to find callers, references or impact, call find_references or get_blast_radius; to read the code they name, call get_source.

Its edges are resolved by go/types, the TypeScript checker and rust-analyzer, not by matching names, so a reference list is complete for those languages and an empty one means nobody calls it. Grep cannot tell you that.

Rows are addressable: every one carries a repository, a repository-relative path, a qualified name and a line range, and every tool accepts that triple instead of a stable key.

Where it loses: a rare name in a single small repository is cheaper to grep, and one small file is cheaper to read than to outline. It wins on common names, on transitive impact, on cross-repository consumers and on proving an absence.
```

A client keeps the tool names and their one-line descriptions resident in the
model's context, and fetches the full input schemas only when a tool is about to
be called. That is why the routing advice lives in `instructions` and in the
descriptions rather than in the schemas: the schemas are not in front of the
model at the moment it decides whether to call anything.

## Pick the tool by the question

| The question | The tool |
| --- | --- |
| Who calls this, what references this | [`find_references`](/reference/tools/find-references/) |
| What breaks if I change this | [`get_blast_radius`](/reference/tools/get-blast-radius/) |
| What does this reach outward | [`trace_dependencies`](/reference/tools/trace-dependencies/) |
| Who uses this from another repository | [`find_cross_repo_consumers`](/reference/tools/find-cross-repo-consumers/) |
| Where is this declared | [`find_symbol`](/reference/tools/find-symbol/) |
| What are this symbol's package, signature, visibility and line range | [`get_symbol`](/reference/tools/get-symbol/) |
| What is declared under this path | [`get_file_outline`](/reference/tools/get-file-outline/) |
| Give me the code of these symbols | [`get_source`](/reference/tools/get-source/) |
| Which repositories does the graph cover, at which commit | [`list_repositories`](/reference/tools/list-repositories/) |
| Is the published graph current | [`graph_status`](/reference/tools/graph-status/) |
| Register projects and rebuild the graph | [`index_project`](/reference/tools/index-project/) |

`index_project` is the only tool that changes anything. Every other tool is
annotated `readOnlyHint`.

## Address a symbol without a stable key

Every tool that takes a symbol accepts one of two selectors:

- `stable_key`, or
- the triple `repository` plus repository-relative `path` plus `qualified_name`.

Exactly one of the two. Passing both is rejected with `INVALID_ARGUMENT` rather
than resolved quietly, because the two can disagree and answering one of them
answers a question nobody asked. `repository` and `path` narrow a
`qualified_name`; a `stable_key` already names one symbol, so combining them is
also rejected. `path` alone is rejected too: it is repository-relative, so it
requires `repository`.

A stable key is exact and durable, and it is also about thirty-five tokens of
opaque base32 that nothing outside the server can read. You never need one,
because every row a tool returns already carries the triple. Find the symbol,
then feed the answer straight into the next call.

Step one, locate it:

```json
{
  "name": "MergeAll",
  "repo": "ladygraph",
  "limit": 3
}
```

`find_symbol` answers with the row you need:

```json
{
  "stable_key": "KHXAWFM5ED2YEIFIEMB5NALA7L7YXNHSUJEBLU7SDAMLGKX24UAA",
  "name": "MergeAll",
  "qualified_name": "MergeAll",
  "kind": "func",
  "exported": true,
  "repository": "ladygraph",
  "file_path": "internal/facts/facts.go",
  "start_line": 516,
  "end_line": 542
}
```

Step two, ask the real question. The row's `repository`, `file_path` and
`qualified_name` become the call's `repository`, `path` and `qualified_name`:

```json
{
  "repository": "ladygraph",
  "path": "internal/facts/facts.go",
  "qualified_name": "MergeAll",
  "direction": "incoming",
  "limit": 3
}
```

Note the one rename: rows report the file as `file_path`, arguments call it
`path`. Both are repository-relative.

[`get_file_outline`](/reference/tools/get-file-outline/) is the other way in
when you know the file or the package but not the name. It takes `repository`
and `path` and returns the declarations grouped by file, each with kind,
signature and line range, and those rows address the next call the same way.

If a qualified name matches more than one symbol, the call fails with
`AMBIGUOUS_SYMBOL` instead of a silent pick. What the error offers depends on
what is left to narrow. With no `repository` and no `path` it names the
candidates by where they are, repository, file and line range, because that is a
narrowing you can express in the next call. Once both were given and the name
still matches twice, only the key separates them, and then, and only then, the
error lists the stable keys.

A name that is absent says so distinctly. This is the captured answer to
`get_symbol` with `repository` `ladygraph` and `qualified_name` `NoSuchThing`:

```text
SYMBOL_NOT_FOUND: qualified name "NoSuchThing" was not found under ladygraph; call it without repository and path to search the whole graph
```

## Read the answer

Every query tool returns the same envelope around its `results`. No tool
publishes an `outputSchema`, so the answer arrives once, in the text block, and
is not repeated as structured content.

| Field | What it says |
| --- | --- |
| `total` | How many rows the query matched. |
| `returned` | How many are in this page. |
| `truncated` | Whether `returned` is less than `total`. |
| `next_cursor` | The opaque token for the next page, or `null`. |
| `coverage` | Four disjoint counters: `exact`, `candidate`, `unresolved_related`, `package_level`. |
| `snapshot_id` | The published generation that answered. `null` means none is published. |
| `snapshot_age_ms` | How long ago that generation was built. |
| `guidance` | Present only when the count alone would mislead. |
| `completeness` | Present only when the tool checked how far its answer reaches. |

`coverage.package_level` is counted apart from `exact` on purpose: a package
dependency proves the consumer depends on the provider package, never that it
uses the symbol you asked about, so folding it into `exact` would report a use
nobody observed.

Three rules matter more than the rest.

**A zero-result answer means "nobody", not "not found" -- but only when the
surface says so.** That is what `guidance` is for. This is the captured answer
to `find_cross_repo_consumers` on `MergeAll`, `total` 0:

```text
no repository in the published graph consumes this symbol. Check graph_status if a consumer is registered but was not indexed, and find_references for uses inside its own repository
```

An empty `find_references` page carries the same kind of sentence, saying that
the edges are type-checked and this is an absence rather than a miss. Without
that sentence, treat a zero only as far as the tool's own description takes you.

**`guidance` stays silent when there are rows and no truncation.** It costs
about fifteen tokens, and fifteen tokens of advice on every call is how a saving
becomes a cost. Its absence is not a warning that something is wrong; it means
the numbers speak for themselves.

**A `completeness` verdict of `LOWER_BOUND` means the answer is a floor.**
`COMPLETE` means nothing the index recorded could add to it. `LOWER_BOUND` means
the index recorded places it could not read that this query reaches, and it
names them: `invisible_scopes` for whole packages or modules that could not be
read, `blind_spots` for individual references the resolver could not follow, and
`fallback` for the recovery action, a regular expression and the paths to run it
over. Absence of the whole block means the tool did not check, which is not the
same as checking and finding nothing.

This is the `completeness` block captured from `get_blast_radius` on `MergeAll`
at depth 2:

```json
{
  "verdict": "LOWER_BOUND",
  "invisible_scopes": [
    {
      "reason": "PACKAGE_NOT_BUILDABLE",
      "repository": "ladygraph",
      "requested_package": "github.com/Luqueee/ladygraph/benchmarks/ladybug-delta-profile",
      "detail": "LIST: build constraints exclude all Go files in /Users/adria/Documents/programacion/projects/ladygraph/benchmarks/ladybug-delta-profile"
    },
    {
      "reason": "PACKAGE_NOT_BUILDABLE",
      "repository": "ladygraph",
      "requested_package": "github.com/Luqueee/ladygraph/benchmarks/ladybug-recovery",
      "detail": "LIST: build constraints exclude all Go files in /Users/adria/Documents/programacion/projects/ladygraph/benchmarks/ladybug-recovery"
    },
    {
      "reason": "PACKAGE_PROVIDER_NOT_FOUND",
      "repository": "ladygraph",
      "requested_package": "@astrojs/node"
    },
    {
      "reason": "PACKAGE_PROVIDER_NOT_FOUND",
      "repository": "ladygraph",
      "requested_package": "@astrojs/starlight"
    },
    {
      "reason": "PACKAGE_PROVIDER_NOT_FOUND",
      "repository": "ladygraph",
      "requested_package": "@tailwindcss/vite"
    },
    {
      "reason": "PACKAGE_PROVIDER_NOT_FOUND",
      "repository": "ladygraph",
      "requested_package": "astro"
    },
    {
      "reason": "PACKAGE_PROVIDER_NOT_FOUND",
      "repository": "ladygraph",
      "requested_package": "vitest"
    }
  ],
  "fallback": {
    "pattern": "\\bMergeAll\\b",
    "paths": [
      "/Users/adria/Documents/programacion/projects/ladygraph/benchmarks/ladybug-delta-profile",
      "/Users/adria/Documents/programacion/projects/ladygraph/benchmarks/ladybug-recovery"
    ]
  }
}
```

Read that as: the five rows are real, two Go directories were excluded by build
constraints and five TypeScript package names had no provider in the graph, and
if the answer has to be airtight, grep `\bMergeAll\b` over the two paths named.
One response enumerates at most twenty entries per list; when the list is cut,
`more_invisible_scopes` and `more_blind_spots` carry the exact remainder.

## Paginate

A page is not the whole answer. When `truncated` is `true`, `total` is the real
count and `next_cursor` is how you get the rest.

This is `trace_dependencies` on `MergeAll` at depth 1 with `limit` 3, envelope
only:

```json
{
  "snapshot_id": 30,
  "snapshot_age_ms": 9020,
  "total": 37,
  "returned": 3,
  "truncated": true,
  "next_cursor": "eyJ2ZXJzaW9uIjoxLCJzbmFwc2hvdF9pZCI6MzAsInF1ZXJ5X2hhc2giOiIyZDNiNDY2YmY5MmIxOWFiOTA2Yjg5YjczZmI1YWViODk5YWMyYWRjN2U3OGFmZGUwYmMwNmE3OTg4ZTQyOWQwIiwib2Zmc2V0IjozLCJzb3J0aW5nX3ZlcnNpb24iOiJkZXBlbmRlbmNpZXMtdjEiLCJjaGVja3N1bSI6ImU4MGU0YTViZTEwNWIwM2VhOGEzOWQ0MzVkZWZmODZjMjU5ZTNmMDY4OGM5Yzc3ZWU0YTAzMGRjZDk0YmRiNDgifQ",
  "coverage": { "exact": 37, "candidate": 0, "unresolved_related": 0, "package_level": 0 },
  "guidance": "showing 3 of 37; narrow with depth, max_nodes, edge_kinds or confidence, or pass the cursor for the next page"
}
```

Three of thirty-seven. Concluding anything about the other thirty-four from this
page is a mistake the envelope told you not to make.

The token is base64url without padding, and it is opaque: do not parse it, do
not build one. It encodes a format version, the snapshot id it was taken
against, a hash of the query identity, the row offset, a sorting version and a
checksum over all of it. The query identity covers the tool name and every
argument that can affect which rows match or in what order, so the next call
must repeat the original arguments unchanged and add `cursor`.

| What changed | What happens |
| --- | --- |
| A newer generation was published | `CURSOR_SNAPSHOT_EXPIRED`; start the query again from page one. |
| Any argument affecting membership or ordering | `CURSOR_INVALID`, the cursor does not match the active query. |
| The tool's sorting contract | `CURSOR_INVALID`. |
| The cursor was edited, truncated or re-encoded | `CURSOR_INVALID`; the checksum covers the body. |

The guidance names narrowing before paging, and in that order for a reason: a
second page of rows you did not want is a second payload. Cut with `depth`,
`max_nodes`, `edge_kinds`, `confidence`, `repo` or `language` first, and page
only when the narrowed answer is still too large.

## Confidence and provenance

Every edge row carries a `confidence` and a `provenance`. Traversal rows name
the edge they arrived by as `via_confidence` and `via_provenance`. A captured
`find_references` row on `MergeAll`:

```json
{
  "name": "mergeSets",
  "qualified_name": "mergeSets",
  "kind": "func",
  "repository": "ladygraph",
  "file_path": "internal/indexer/full.go",
  "start_line": 681,
  "end_line": 712,
  "language": "go",
  "edge_kind": "CALLS_DIRECT",
  "confidence": "EXACT_TYPECHECKED",
  "provenance": "GO_AST_CALL"
}
```

`confidence` is one of `EXACT_TYPECHECKED`, `EXACT_DECLARATION_MAPPED`,
`EXACT_PACKAGE_MAPPED`, `STRUCTURAL_CERTAIN`, `CANDIDATE` or `UNRESOLVED`.
`provenance` says which analysis observed it: `GO_AST_CALL` and `GO_TYPES_USE`
come from the Go analysis, `TYPESCRIPT_CHECKER` from the TypeScript checker,
`TYPESCRIPT_PROJECT_REFERENCE` from a provider's own build configuration.

An `EXACT` edge requires sufficient evidence and the correct provenance. It is
never created by a coincidence of name, text, path or alias, and never because
one candidate happened to be the only one left. That is the property that makes
an empty `find_references` answer worth acting on.

`CANDIDATE` and `UNRESOLVED` are different results from `EXACT`, not weaker
versions of it. `CANDIDATE` is plausible and unproven. `UNRESOLVED` carries no
target identity at all: it records that a reference was seen and could not be
followed, with its reason, repository and language, and its file and position
when there is a concrete occurrence. Neither is an edge you can treat as a use.
`coverage.unresolved_related` counts the unresolved records that touch your
query, which is the number that tells you whether the exact ones are the whole
story.

## The Rust standard library

The standard library of the toolchain that indexed the graph enters as a
synthetic derived repository named `rust:<release>`. That namespace is reserved,
so no registered repository can take the name, and the name is the only
authority needed to tell a derived row from yours. It has no commit, no branch
and no dirty flag: nothing clones it and nothing can move it.

It is withheld by default from the four served tools that could return one of
its rows:

- [`find_symbol`](/reference/tools/find-symbol/)
- [`find_references`](/reference/tools/find-references/)
- [`trace_dependencies`](/reference/tools/trace-dependencies/)
- [`get_blast_radius`](/reference/tools/get-blast-radius/)

The default is the difference between a usable answer and an unusable one: with
the standard library in the graph, `find_references` on `Clone` or `Debug`
reaches most of the corpus, and a page of `core` is not what someone asking
about their own code wants.

Two things override it. `include_derived: true`, on any of the four. And naming
the derived provider in `repo`, on the three that take a `repo` filter, because
an explicit filter is a request and not an accident. `get_blast_radius` has no
`repo` argument, so there `include_derived` is the only way.

Withholding a row is a page decision and never a claim about what was observed.
The edge stays published with its exact confidence; it was simply not put on
this page. The counts stay honest too: `graph_status` breaks the derived
provider out under `derived`, with its own packages, files, symbols, inbound and
internal edges and unresolved references, and `list_repositories` marks its row
`derived`.

## When not to use it

A rare name in one small repository is cheaper to grep. Indexing a small file
costs more than reading it. Ladygraph wins on common names, on transitive
impact, on consumers in another repository and on proving an absence. Spending a
call where grep would have done means paying for the graph twice: once for the
call, once for the read you still make.

The repository carries a token-cost harness under `benchmarks/mcp-token-cost`,
and its numbers say the same thing. It compares two arms on the question "who
calls this symbol, and what do those callers look like": the host's own captured
answer, grep plus the file reads that follow it, against the MCP calls a session
needs against the published generation plus the same reads. Tokens are counted
with `cl100k_base`, over the `ladygraph` corpus at generation `000026`, 14424
symbols across 363 files.

Measured on the answer alone, the part a graph server owns:

| Symbol | Class | Factor against the native arm |
| --- | --- | ---: |
| `MergeAll` | rare name | 1.20x |
| `CanonicalColumns` | rare name | 2.24x |
| `DiscoverGo` | rare name | 1.74x |
| `BuildPlan` | shared name | 4.35x |
| `NewServer` | common name | 6.00x |
| `Publish` | common name | 5.52x |
| Total | | 3.46x |

Counting the whole session, the answer plus the bodies the agent then opens,
which both arms pay identically, the total factor is 1.42x, and 15102 of the
25768 native tokens are source bodies. The report publishes both figures on
purpose: the answer factor flatters the graph and the session factor flatters
grep.

The cross-repository question was measured separately, on `Compute` in a
three-file `shared-library` corpus, and there the MCP arm cost more: 0.53x on
the answer and 0.70x on the session. The harness records why that column is a
floor rather than a ceiling for the native side: a grep finds the name but
cannot tell whether the hit is the same symbol, and says nothing about a
consumer that depends on the provider package without using the symbol. Neither
latency nor money is measured anywhere in the harness.

## Keep the graph fresh

Answers come from the published generation, not from your working tree. A file
you edited a minute ago is not in the graph until a rebuild publishes it.

[`graph_status`](/reference/tools/graph-status/) is how you find out. Its
`repository_freshness` block lists each repository with the commit it was
indexed at and the commit its working tree is on now, and `repositories_moved`
counts the ones that left the indexed commit. A repository whose HEAD could not
be read is not counted as moved and not silently counted as fresh either.

Rebuilding is `ladygraph index --full` from the CLI, or
[`index_project`](/reference/tools/index-project/) from the client. The tool
requires explicit user approval before it runs: called without `confirmed`, it
returns `PERMISSION_REQUIRED`. A rebuild costs the whole corpus, so pass every
project in one call. See [`/guides/indexing/`](/guides/indexing/) for the full
procedure.
