---
title: Using the MCP server
description: Route a question to the right Kivgraph tool, address a symbol without a stable key, and read the envelope, guidance and completeness of an answer.
---

`kivgraph serve` speaks MCP over stdio and, once a generation is published,
registers twelve tools; before that it registers `index_project` alone. This page
is about using them: which tool answers which question, how to name a symbol, and
how to read what comes back. Per-tool arguments live under
[`/docs/mcp-tools/`](/docs/mcp-tools/) and on each tool's own page.

Which process answers does not change any of that. `serve` normally forwards
the session to a daemon and holds no graph itself, so the tools, the arguments
and the envelopes below are the same either way; what changes is what it costs.
[Registering a client](/mcp/clients/) has that part.

## Select a profile

One server can expose several independent graphs. Every read tool accepts a
`profile` array and uses the configured default when it is omitted.
Every query accepts one or more profile names, or `["*"]` alone for all.
`graph_status` and `list_repositories` are the discovery calls, so omitting
`profile` there lists every profile instead.

A multi-profile answer replaces scalar `snapshot_id` with a `profiles` array,
marks `cross_profile_edges` as `not_resolved`, and identifies which profiles
supplied each row. Identical rows are returned once with all their profile
names; completeness is the weakest selected profile. A cursor is valid only
for the same canonical profile names and generations. When more than one
profile exists, a stable key must name exactly one profile: the default is a
movable pointer, so omitting `profile` is rejected.

`index_project` uses a single string `profile`, not an array. Omitting it writes
the default graph; naming a missing profile creates it before the full rebuild.

## What the server tells your agent

The `initialize` result carries an `instructions` string. This is it, literally:

```text
Kivgraph answers "what breaks if I change this" from a published code graph over
Go, TypeScript, Rust, Python and Dart. If you can describe what the code does
but do not know a symbol name, call find_by_intent first; then use the file or
symbol it returns with the exact graph tools. Before grepping or reading files
to find callers, references or impact, call find_references or get_blast_radius;
to read the code they name, call get_source. Check confidence and completeness:
Python fallback facts can be CANDIDATE and external Dart packages can be
UNRESOLVED.

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
| I can describe the behavior but do not know its name; which files do I open | [`find_by_intent`](/docs/tools/find-by-intent/) |
| Who calls this, what references this | [`find_references`](/docs/tools/find-references/) |
| What breaks if I change this | [`get_blast_radius`](/docs/tools/get-blast-radius/) |
| What does this reach outward | [`trace_dependencies`](/docs/tools/trace-dependencies/) |
| Who uses this from another repository | [`find_cross_repo_consumers`](/docs/tools/find-cross-repo-consumers/) |
| Where is this declared | [`find_symbol`](/docs/tools/find-symbol/) |
| What are this symbol's package, signature, visibility and line range | [`get_symbol`](/docs/tools/get-symbol/) |
| What is declared under this path | [`get_file_outline`](/docs/tools/get-file-outline/) |
| Give me the code of these symbols | [`get_source`](/docs/tools/get-source/) |
| Which repositories does the graph cover, at which commit | [`list_repositories`](/docs/tools/list-repositories/) |
| Is the published graph current | [`graph_status`](/docs/tools/graph-status/) |
| Register projects and rebuild the graph | [`index_project`](/docs/tools/index-project/) |

`index_project` is the only tool that changes anything. Every other tool is
annotated `readOnlyHint`.

Start with [`find_by_intent`](/docs/tools/find-by-intent/) when the question is
about behavior rather than an identifier. It returns ranked candidates and the
matching mode, not a type-checked edge; use its answer to enter the exact graph
queries.

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
because every row a tool returns already carries the triple -- which is why the
default view of every query tool leaves the key out. Find the symbol, then feed
the answer straight into the next call.

Step one, locate it:

```json
{
  "name": "MergeAll",
  "repo": "kivgraph",
  "limit": 3
}
```

`find_symbol` answers with the row you need. This is the default `compact` view:
the header states what the whole page shares, the row states where it is:

```json
{
  "name": "MergeAll",
  "kind": "func",
  "exported": true,
  "repository": "kivgraph",
  "symbols": [
    {
      "at": "internal/facts/facts.go:516",
      "end": 542,
      "sig": "func(sets []github.com/Luqueee/kivgraph/internal/facts.Set) github.com/Luqueee/kivgraph/internal/facts.Set"
    }
  ]
}
```

`at` is `path:line` under a header that names the repository, and the full
`repository:path:line` triple when the page's rows come from more than one. Under
`"view": "full"` the same row is spelled field by field, key included:

```json
{
  "stable_key": "KHXAWFM5ED2YEIFIEMB5NALA7L7YXNHSUJEBLU7SDAMLGKX24UAA",
  "name": "MergeAll",
  "qualified_name": "MergeAll",
  "kind": "func",
  "exported": true,
  "repository": "kivgraph",
  "file_path": "internal/facts/facts.go",
  "start_line": 516,
  "end_line": 542
}
```

Step two, ask the real question. The header's `repository`, the row's path and
its qualified name become the call's `repository`, `path` and `qualified_name`:

```json
{
  "repository": "kivgraph",
  "path": "internal/facts/facts.go",
  "qualified_name": "MergeAll",
  "direction": "incoming",
  "limit": 3
}
```

Note the one rename: a compact row reports the file inside `at`, a full row as
`file_path`, and arguments call it `path`. All of them are repository-relative.

For a reference question the two steps collapse into one:
[`find_references`](/docs/tools/find-references/) takes `name` on its own,
answers directly when a single declaration carries that name, and returns the
candidates as `repository:path:line` under `AMBIGUOUS_SYMBOL` when several do.

[`get_file_outline`](/docs/tools/get-file-outline/) is the other way in
when you know the file or the package but not the name. It takes `repository`
and `path` and returns the declarations grouped by file, each as a
`name@start-end` entry with its kind, and those rows address the next call the
same way. The signature comes with `response_format: "detailed"`.

If a qualified name matches more than one symbol, the call fails with
`AMBIGUOUS_SYMBOL` instead of a silent pick. What the error offers depends on
what is left to narrow. With no `repository` and no `path` it names the
candidates by where they are, repository, file and line range, because that is a
narrowing you can express in the next call. Once both were given and the name
still matches twice, only the key separates them, and then, and only then, the
error lists the stable keys.

A name that is absent says so distinctly. This is the captured answer to
`get_symbol` with `repository` `kivgraph` and `qualified_name` `NoSuchThing`:

```text
SYMBOL_NOT_FOUND: qualified name "NoSuchThing" was not found under kivgraph; call it without repository and path to search the whole graph
```

## Choose a view

Every query tool takes a `view`, and it changes the granularity of an answer,
never the answer. The same edges, with the same confidence and the same
provenance, spelled with or without the parts a row shares with every other row.

| `view` | What you get | Where |
| --- | --- | --- |
| `compact` | The default. Whatever every row of the page shares is stated once in a header, and a row carries only what the header could not state for it. | Every query tool |
| `full` | The field-per-row shape: every field on every row, including the stable keys and the `language` the compact view drops. Ask for it when a client was written against that shape. | Every query tool |
| `files` | Only which files hold the facts, with a count each. The answer to "which files do I open". | [`find_references`](/docs/tools/find-references/) and [`get_file_outline`](/docs/tools/get-file-outline/) |

An unsupported value is `INVALID_ARGUMENT`, and so is `files` on a tool whose
answer is not a set of files -- it fails rather than quietly returning something
else.

Pick `compact` unless you have a reason, which is why it is the default: over the
four reference questions of `benchmarks/codebase-memory-comparison`, measured on
the private benchmark corpus, they cost `13.594` tokens in the previous shape,
`2.883` in the compact view and `963` in the files view, with precision and
recall unchanged.
`view` is never part of a cursor's identity, so a page taken in one view can be
continued in another.

Four tools group their rows by file and spell each row as a label:
[`find_references`](/docs/tools/find-references/),
[`trace_dependencies`](/docs/tools/trace-dependencies/),
[`get_blast_radius`](/docs/tools/get-blast-radius/) and
[`get_file_outline`](/docs/tools/get-file-outline/). Three habits make their
answers readable:

- **Read the header first.** A field there applies to every row. A field missing
  from it means the rows disagreed and each carries its own.
- **A row is a label.** `qualified_name@start`, or `qualified_name@start-end`
  when the declaration spans lines -- the qualified name when the row has one and
  the bare name when it does not. It becomes an array when the row had to carry
  a column the header could not hoist, and the elements after the label are that
  tool's columns in a fixed order, skipping whatever the header states -- so the
  header tells you which columns a tail can hold and the order tells you which is
  which. Numbers in a tail are written as strings.
- **The triple is assembled, not printed.** The repository comes from the header
  (or a group's own `repo`), the path from the group's `file`, the qualified name
  and line from the label. That is the selector every tool accepts, so no row
  needs a stable key.

The other two spell a row as an object, for the same reason: their answer is not
a set of files. [`find_symbol`](/docs/tools/find-symbol/) addresses a
declaration with `at`, which is `path:line` under a header that names the
repository and the whole `repository:path:line` triple when the rows come from
more than one. [`find_cross_repo_consumers`](/docs/tools/find-cross-repo-consumers/)
keeps a field per consumer -- `repo`, `pkg`, `at` -- because a package dependency
has a repository and no file at all.

## When a page groups

A header only states a column when **every** row of the page agrees on it. One
dissenting row is enough to knock a column back down to the rows -- a `65`-row
page sharing `kind` and `edge_kind` pays for both on every row if row `66` is a
re-export the other `65` are not. `groups` is the second tier that catches this:
the six query tools that carry a `compact` view -- `find_symbol`,
`find_references`, `trace_dependencies`, `get_blast_radius`,
`get_file_outline` and `find_cross_repo_consumers` -- also try grouping the page
by whatever exact tuple of the remaining columns each row still shares, instead
of stating that tuple over and over on every row.

`results.groups` replaces the flat field -- `symbols`, `files` or `consumers`,
depending on the tool -- it never sits beside it. Each entry is a small header of
its own, holding the columns that tuple fixed, followed by the tool's normal rows
in their normal shape: a label list under `files` for the four that group by
file, a `symbols` array for `find_symbol`, a `consumers` array for
`find_cross_repo_consumers`. A column already stated on the page header is never
repeated on a group; a column that still disagrees within a group stays on the
row, exactly as it would on a flat page.

Grouping is a bet, and the response never assumes it pays off: building both the
flat page and the grouped one and keeping whichever serializes smaller is cheap
next to a snapshot lookup, and it is the only way to guarantee grouping never
costs more than not grouping. A page where every row disagrees with every other
-- three hops down three different edges is the ordinary shape of
`trace_dependencies` -- stays flat, because a group is an object with its own
keys and a row with nothing to share only had to pay for its own values. Neither
shape changes `total`, `returned` or `coverage`; it only changes how the same
rows are spelled.

A `find_symbol` search for `handle` across two repositories, three kinds and no
column the whole page agrees on:

```json
{
  "exported": false,
  "groups": [
    { "kind": "method", "symbols": [ /* 6 rows */ ] },
    { "kind": "variable", "symbols": [ /* 1 row */ ] },
    { "kind": "func", "symbols": [ /* 1 row */ ] }
  ]
}
```

`exported` is `false` for the whole page -- nothing here is exported -- so it
stays in the header; `kind` disagrees, so each group states its own and never
repeats it on a row. Every tool's own reference page shows the full, unabridged
capture:
[`find_symbol`](/docs/tools/find-symbol/#example-grouped),
[`find_references`](/docs/tools/find-references/#reading-a-grouped-page),
[`trace_dependencies`](/docs/tools/trace-dependencies/#reading-a-grouped-page),
[`get_blast_radius`](/docs/tools/get-blast-radius/#reading-a-grouped-page),
[`get_file_outline`](/docs/tools/get-file-outline/#example-grouped) and
[`find_cross_repo_consumers`](/docs/tools/find-cross-repo-consumers/#reading-a-grouped-page).

## Read the answer

Every query tool returns the same envelope around its `results`. No tool
publishes an `outputSchema`, so the answer arrives once, in the text block, and
is not repeated as structured content.

| Field | What it says | In a compact envelope |
| --- | --- | --- |
| `snapshot_id` | The published generation that answered. `null` means none is published. | Always |
| `total` | How many rows the query matched. | Always |
| `returned` | How many are in this page. | Always |
| `results` | The answer itself. | Always |
| `truncated` | Whether `returned` is less than `total`. | Only when it is `true` |
| `next_cursor` | The opaque token for the next page. | Only when there is one |
| `coverage` | Four disjoint counters over the **whole answer**, not this page: `exact`, `candidate`, `unresolved_related`, `package_level`. | Only the counters above zero, and absent entirely when all four are zero |
| `snapshot_age_ms` | How long ago that generation was built. | Never; ask [`graph_status`](/docs/tools/graph-status/) for the age |
| `guidance` | Present only when the count alone would mislead. | Unchanged |
| `completeness` | Present only when the tool checked how far its answer reaches. | Unchanged |

What the compact envelope leaves out is what carried no information: an age
nobody asked for, a `truncated` that is `false`, a cursor that does not exist and
a category that counted nothing. Read an absence accordingly -- a missing
`truncated` is `false`, a missing `next_cursor` is `null`, a missing
`unresolved_related` is `0` -- and note that `snapshot_id`, `total`, `returned`
and `results` are always there, so the four numbers that say how much of the
answer you are holding never move. The `full` view writes every field, `false`,
`null` and zeros included.

`coverage.package_level` is counted apart from `exact` on purpose: a package
dependency proves the consumer depends on the provider package, never that it
uses the symbol you asked about, so folding it into `exact` would report a use
nobody observed.

**The counters describe resolved relations, so a tool that returns no relations
carries none of them.** `exact` and `candidate` are edge confidences, and the
scope is the answer rather than the page: the `trace_dependencies` envelope
below reports `exact` at `37` while `returned` is `3`. That is what makes the
pair worth reading -- a counter equal to `returned` could not tell you anything
`returned` had not already said.

So `get_file_outline` publishes no counter at all, and `find_symbol` publishes
only `unresolved_related`: both answer with declarations of one repository, and
a declaration has no confidence to report. `get_source` keeps a count of its
own and it is not one of these four: it answers in prose, and its header line
says how many bodies it could actually serve, which is genuinely less than
`returned` when a file moved under the index.

Three rules matter more than the rest.

**A zero-result answer means "nobody", not "not found" -- but only when the
surface says so.** That is what `guidance` is for. This is the captured answer
to `find_cross_repo_consumers` on `MergeAll`, `total` 0:

```text
no repository in the published graph consumes this symbol. Check graph_status if a consumer is registered but was not indexed, and find_references for uses inside its own repository
```

An empty `find_references` page carries the same kind of sentence, and which one
depends on its verdict: with `COMPLETE` it says the edges are type-checked and
this is an absence rather than a miss, and with `LOWER_BOUND` it says the index
recorded places it could not read that ask for this name, and sends you to
`completeness.blind_spots` and the fallback pattern instead. The two are never
interchangeable: the first is a claim about the code, the second about the index.
Without either sentence, treat a zero only as far as the tool's own description
takes you.

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

Six tools check, and **what bounds an answer is not the same question for all of
them**, so each one looks at a different set of recorded failures:

| Tool | What can bound its answer |
| --- | --- |
| `find_references` | Failures that asked for this name, plus unreadable scopes of the subject's repository. `direction` does not change the verdict: it is charged with the naming question either way. |
| `get_blast_radius` | The same pair, taken from the repository the walk starts in. |
| `trace_dependencies` | Failures **this symbol itself** made, plus unreadable scopes of its repository. "What does this reach" is never bounded by who asked for its name -- somebody else's unreadable call hides a caller, not a dependency. |
| `find_symbol` | Unreadable scopes. Narrowed with `repo`, only that repository's; unnarrowed, every one in the graph. |
| `get_file_outline` | Unreadable scopes of the repository asked for. There is no symbol name here, so nothing else can bound it. |
| `find_cross_repo_consumers` | Unreadable scopes **anywhere in the graph**, deliberately. A package nobody could read in any repository is exactly where an outside consumer hides. |

The scope follows the question for a reason: a verdict charged for every blind
spot in the graph would read `LOWER_BOUND` on every answer of a corpus with one
bad package, and a verdict that never says `COMPLETE` carries no information.

The other five tools do not check, and none of them claims an absence.
`get_symbol` and `get_source` refuse a symbol they cannot find instead of
answering an empty list; `graph_status`, `list_repositories` and `index_project`
answer about the index itself.

This is the `completeness` block captured from `get_blast_radius` on `MergeAll`
at depth 2:

```json
{
  "verdict": "LOWER_BOUND",
  "invisible_scopes": [
    {
      "reason": "PACKAGE_NOT_BUILDABLE",
      "repository": "kivgraph",
      "requested_package": "github.com/Luqueee/kivgraph/benchmarks/ladybug-delta-profile",
      "detail": "LIST: build constraints exclude all Go files in /path/to/kivgraph/benchmarks/ladybug-delta-profile"
    },
    {
      "reason": "PACKAGE_NOT_BUILDABLE",
      "repository": "kivgraph",
      "requested_package": "github.com/Luqueee/kivgraph/benchmarks/ladybug-recovery",
      "detail": "LIST: build constraints exclude all Go files in /path/to/kivgraph/benchmarks/ladybug-recovery"
    },
    {
      "reason": "PACKAGE_PROVIDER_NOT_FOUND",
      "repository": "kivgraph",
      "requested_package": "@astrojs/node"
    },
    {
      "reason": "PACKAGE_PROVIDER_NOT_FOUND",
      "repository": "kivgraph",
      "requested_package": "@astrojs/starlight"
    },
    {
      "reason": "PACKAGE_PROVIDER_NOT_FOUND",
      "repository": "kivgraph",
      "requested_package": "@tailwindcss/vite"
    },
    {
      "reason": "PACKAGE_PROVIDER_NOT_FOUND",
      "repository": "kivgraph",
      "requested_package": "astro"
    },
    {
      "reason": "PACKAGE_PROVIDER_NOT_FOUND",
      "repository": "kivgraph",
      "requested_package": "vitest"
    }
  ],
  "fallback": {
    "pattern": "\\bMergeAll\\b",
    "paths": [
      "/path/to/kivgraph/benchmarks/ladybug-delta-profile",
      "/path/to/kivgraph/benchmarks/ladybug-recovery"
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
  "total": 37,
  "returned": 3,
  "truncated": true,
  "next_cursor": "Ah4DYbLgOYhUrECElsmz0oFlyLUGpgE",
  "coverage": { "exact": 37 },
  "guidance": "showing 3 of 37; narrow with depth, max_nodes, edge_kinds or confidence, or pass the cursor for the next page"
}
```

Three of thirty-seven. Concluding anything about the other thirty-four from this
page is a mistake the envelope told you not to make.

The token is base64url without padding, and it is opaque: do not parse it, do
not build one. It is about 31 characters over a binary body -- a format version,
the snapshot id it was taken against, the row offset, a digest of the query
identity, a digest of the sorting contract and a checksum over all of it -- where
the previous version spent 314 characters of base64-wrapped JSON, `221` tokens on
every truncated page. The query identity covers the tool name and every argument
that can affect which rows match or in what order, so the next call must repeat
the original arguments unchanged and add `cursor`. `view`, `limit` and
`response_format` are not among them.

| What changed | What happens |
| --- | --- |
| A newer generation was published | `CURSOR_SNAPSHOT_EXPIRED`; start the query again from page one. |
| Any argument affecting membership or ordering | `CURSOR_INVALID`, the cursor does not match the active query. |
| The tool's sorting contract | `CURSOR_INVALID`. |
| The cursor was edited, truncated or re-encoded, or decodes with trailing bytes after the checksum | `CURSOR_INVALID`; the checksum covers the body. |
| The cursor was minted by a server of the previous cursor version | `CURSOR_INVALID`; the body declares version 2, and a version-1 token fails closed rather than being read under the new layout. |

The guidance names narrowing before paging, and in that order for a reason: a
second page of rows you did not want is a second payload. Cut with `depth`,
`max_nodes`, `edge_kinds`, `confidence`, `repo` or `language` first, and page
only when the narrowed answer is still too large.

## Confidence and provenance

Every edge carries a `confidence` and a `provenance`, and neither view ever drops
them: in the compact one they sit in the header when the whole page agrees and on
the row when it does not, and in the full one they are on every row. Traversal
rows name the edge they arrived by as `via_confidence` and `via_provenance`. This
is a captured `find_references` row on `MergeAll` in the `full` view; the compact
page that carries the same edge states the last three fields once above it, and
spells the row as `["mergeSets@681-712", "func"]`:

```json
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

- [`find_symbol`](/docs/tools/find-symbol/)
- [`find_references`](/docs/tools/find-references/)
- [`trace_dependencies`](/docs/tools/trace-dependencies/)
- [`get_blast_radius`](/docs/tools/get-blast-radius/)

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
costs more than reading it. Kivgraph wins on common names, on transitive
impact, on consumers in another repository and on proving an absence. Spending a
call where grep would have done means paying for the graph twice: once for the
call, once for the read you still make.

The repository carries a token-cost harness under `benchmarks/mcp-token-cost`,
and its numbers say the same thing. It compares two arms on the question "who
calls this symbol, and what do those callers look like": the host's own captured
answer, grep plus the file reads that follow it, against the MCP calls a session
needs against the published generation plus the same reads. Tokens are counted
with `cl100k_base`, over the `kivgraph` corpus at generation `000026`, 14424
symbols across 363 files.

Measured on the answer alone, the part a graph server owns. These factors were
taken against the previous output shape, before the compact view became the
default, so they are a ceiling on what a session pays today:

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

[`graph_status`](/docs/tools/graph-status/) is how you find out. Its
`repository_freshness` block lists each repository with the commit it was
indexed at and the commit its working tree is on now, and `repositories_moved`
counts the ones that left the indexed commit. A repository whose HEAD could not
be read is not counted as moved and not silently counted as fresh either.

Rebuilding is `kivgraph index --full` from the CLI, or
[`index_project`](/docs/tools/index-project/) from the client. The tool
requires explicit user approval before it runs: called without `confirmed`, it
returns `PERMISSION_REQUIRED`. A rebuild costs the whole corpus, so pass every
project in one call. See [`/guides/indexing/`](/guides/indexing/) for the full
procedure.
