---
title: Compared
description: Five code-graph tools and grep, on the same seven questions over a 37-repository monorepo, with the ground truth, the losses and the raw responses published.
---

Five tools that call themselves code graphs, asked the same seven questions over
the same 37-repository monorepo, against a manual ground truth. Plus a sixth arm:
`grep` and reading the files, which is what an agent already has.

Everything below is one run of `benchmarks/graph-tools-comparison`, on
2026-08-21, commit `b4afce9`, tokenised with `o200k_base`. The raw response of
every call is published next to the harness, so any number here can be checked
against the bytes it came from. An earlier pass on `kivgraph 0.3.2` is kept in
`report.md`; what changed between them, and the third defect the re-measurement
found, is in `remeasure.md`.

## The result

| tool | version | tokens | calls | precision | recall | exact |
| --- | --- | --- | --- | --- | --- | --- |
| **kivgraph** | `0.3.6` | `6,295` | 11 | **`0.94`** | **`0.98`** | **`5/7`** |
| graphify | `0.8.31` | **`2,469`** | 9 | `0.54` | `0.35` | `1/7` |
| graft | `0.10.1` | `8,770` | 7 | `0.14` | `0.14` | `1/7` |
| codebase-memory-mcp | `0.8.1` | `26,599` | 21 | `0.67` | `0.81` | `3/7` |
| code-review-graph | `2.3.7` | `109,298` | 10 | `0.67` | `0.85` | `3/7` |
| `grep` + reading | — | `63,531` | 27 | `1.00` | `1.00` | `7/7` |

Two readings, and they should not be mixed.

**`grep` plus reading answers all seven.** It is the honest denominator: the real
alternative to these tools is not being wrong, it is spending `63,531` tokens.
None of the five matches its accuracy, ours included.

**Among the five, the cheapest is not the most accurate.** graphify costs `2,469`
tokens and answers one of seven; Kivgraph costs `6,295` and answers five. Per
correct answer that is `1,259` tokens against `2,469` and against code-review-
graph's `36,433`.

## Why three families of question

Five tools that share a category do not share a question. Asking only "who calls
this" would have put two of them at zero for being outside their purpose rather
than for being wrong: graphify is a BFS over an extracted graph, and
code-review-graph is built around blast radius.

So there are three families, each one the question some tool's own documentation
says it answers, and **each tool is asked in its own vocabulary** — `callers_of`
in one, `affected` in another, `trace_path` or Cypher in a third. What is
compared is the answer, never the spelling.

| family | questions | truth |
| --- | --- | --- |
| references | 4 — one per language, one cross-package | the files holding a call or reference |
| impact | 1 — transitive, two hops | the files that reach the subject |
| outline | 2 — one large file, one small | the names declared at the top level |

The small outline is there on purpose: three declarations in 78 lines is where
[Limits](/limits/) already says an index costs more than reading the file, and a
benchmark that only asked the flattering size would be measuring its own question
selection.

## What separates them: a name is not a symbol

`withRetry` is declared **seven times** in this corpus, in three languages.
`now_ms`, four times. That is the case that splits the five, and three of them
fail the same way.

- **codebase-memory-mcp** points its `CALLS` edges at the name, so the callers of
  all seven `withRetry` collapse onto one node. On the Go question it recovers
  both correct files and drags in four TypeScript ones — `P=0.33`. On the
  TypeScript one the answer is empty: every caller attached to the Go homonym, so
  the TypeScript node has in-degree zero.
- **code-review-graph** disambiguates the *subject* — it refuses to choose and
  names both candidates with file and line, exactly as Kivgraph does — but not
  the call sites. Narrowed to the `postgres` declaration it still returns
  `internal/shared/infisical/infisical_test.go`, which calls the other one:
  impossible in Go, different packages, unexported function. `P=0.67`.
- **graft**, given an ambiguous name, **drops the cross-file callers and warns
  that it may undercount**. That is the opposite of inventing an edge and it is
  honest — and it leaves the Go and Rust questions at zero.

Kivgraph answers those two exactly because its edges come from `go/types`, the
TypeScript checker and `rust-analyzer`, not from matching names.

## Two numbers that are not mistakes

**code-review-graph's `82,057` tokens on impact are a different question, not an
error.** Its `impact` takes changed *files*, not declarations: asking about
`retry.go::expBackoffJitter` answers "0 nodes changed". Asked about the file, two
hops reach `390` nodes in `255` files, and against a two-file truth the precision
is `0.01`. It finds both — recall `1.00`. That `0.01` measures granularity.

**graphify's `affected` is not reverse reachability.** Its `graph.json` is written
`"directed": false`, so networkx loads an undirected graph with no in-edges and
the fallback walks edges whose stored orientation ends at the seed — and that
orientation is node insertion order, which is file walk order, not call
direction. Its own output shows it: `affected` on `withRetry` returns its
*callees*, on `expBackoffJitter` it returns nothing despite three incoming call
edges, and the same command in another repository returns 43 genuine callers.

## Where Kivgraph loses

| | |
| --- | --- |
| indexing | `50.3 s` cold, the slowest of the five, against graphify's `12.1 s` and codebase-memory's `5.3 s` |
| disk | `1,542 MB`, `7x` the next |
| dependencies | the only one that needs a toolchain: without the Go module cache or `cargo`, a load fails and those symbols are absent |
| the TypeScript question | recall `0.89`: the missing caller's only calls sit inside an `it(...)` callback, and a use with no enclosing named declaration has no source symbol for an edge to start from, so it is dropped. The file itself is indexed |
| re-export barrels | precision `0.56` on the cross-package question: an incoming-references answer mixes `REEXPORTS` with `CALLS_DIRECT`, so a barrel forwarding a name ranks beside the five sites that use it |
| accuracy overall | `5/7` against `grep`'s `7/7` |

## Entry cost

Every index was timed cold, with the derived state deleted first, because "cold"
has to mean the same thing in every row: graft took `2.6 s` over a context it had
already built and `26.1 s` over none. Every arm was a little slower this pass
than the one `report.md` records; the machine, not the code.

| tool | cold | disk | scope | needs |
| --- | --- | --- | --- | --- |
| codebase-memory-mcp | `5.3 s` | `221 MB` | whole corpus | nothing |
| code-review-graph | `8.0 s` | `201 MB` | one graph per repository | nothing |
| graphify | `12.1 s` | — | one graph per repository | nothing for the structural pass |
| graft | `26.1 s` | `181 MB` | whole corpus | nothing for the structural tier |
| kivgraph | `50.3 s` | `1,542 MB` | whole corpus, `122,583` symbols | Go module cache, `cargo` |

A per-repository graph has a consequence beyond cost: a cross-package reference
is structurally invisible to it, which is why code-review-graph and graphify both
answer zero on that question. Their `build` is also a full build that truncates
the data directory it is given, so each repository needs its own.

## Nothing was written inside the corpus

Checked with `git status` across all 37 repositories before and after: only the
two `go.sum` files that were already dirty. Each tool's state lives outside.

One of them needs saying plainly: **graphify writes `graphify-out/` beside the
code it reads**, so it only ever ran against a private copy. Anyone pointing it
at their own repository gets a new directory inside it.

## Limitations

- Seven questions, one corpus, one machine. Not a general measure of quality for
  any of the five.
- `o200k_base` is a proxy for the Claude tokenizer: the ratios between rows are
  the claim, the absolute values are not.
- Three full cold passes, and the spread was measured rather than assumed:
  kivgraph, graft, graphify and `grep` returned **the same number all three
  times**; code-review-graph moved by `0.1%`; codebase-memory-mcp by `1.8%` in
  tokens **and between `3` and `4` exact answers out of seven**, so it is the one
  row whose accuracy depends on the pass. The figures above are the first pass.
- Only the free, model-free tiers. `graft --deep`, graphify's semantic pass and
  code-review-graph's embeddings need a provider key and were not measured.
- code-review-graph and graphify indexed the four repositories the questions name
  rather than all 37, because their graph is per-repository.
- Kivgraph was measured with its default view, not the `files` view that answers
  the same reference questions for a third of the tokens. Taking the discount
  only we have would have compared our summary against everyone else's detail.

The harness, the ground truth and every raw response are in
`benchmarks/graph-tools-comparison/` in the repository.
