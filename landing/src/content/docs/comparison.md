---
title: Code Intelligence MCP Benchmark
description: Five code-graph tools and grep over one 37-repository corpus, with the ground truth, the raw responses and each arm's losses published.
---

Five tools that call themselves code graphs, plus a sixth arm that is not a tool
at all — `grep` and reading the files, which is what an agent already has —
asked the same questions over the same corpus, against a hand-written ground
truth.

This page is a benchmark report, not a product page. Two passes are published
below. They were run a day apart against different versions of one arm and a
different number of questions, so they are kept in **two separate tables and
never averaged together**. Every figure names the JSON file and the commit it
came from, and the raw response of every call is committed next to the harness,
so any number here can be checked against the bytes it came from.

## Methodology

Each arm is driven by the harness in `benchmarks/graph-tools-comparison/`, one
Go file per arm. A question is put to every arm **in that arm's own vocabulary**
— `callers_of` in one, `affected` in another, `trace_path`, `impact` or Cypher
in a third. What is compared is the answer, never the spelling.

Scored per question:

- **tokens** — every byte the arm returned, counted with `o200k_base`, including
  the calls that were wrong or empty.
- **calls** — how many round trips the answer took.
- **precision** and **recall** against the ground truth for that question.
- **exact** — precision and recall both `1.00`.

### Why three families of question

Five tools that share a category do not share a question. Asking only "who calls
this" would have put two of them at zero for being outside their purpose rather
than for being wrong: graphify is a BFS over an extracted graph, and
code-review-graph is built around blast radius.

So the seven-question `measured` set is three families, each one the question
some tool's own documentation says it answers:

| family | questions | truth |
| --- | --- | --- |
| references | 4 — one per language, one cross-package | the files holding a call or reference |
| impact | 1 — transitive, two hops | the files that reach the subject |
| outline | 2 — one large file, one small | the names declared at the top level |

The small outline is there on purpose: three declarations in 78 lines is where
[Limits](/limits/) already says an index costs more than reading the file, and a
benchmark that only asked the flattering size would be measuring its own
question selection.

The 29-question `all` set unions that seven-question set with six further sets
(`hard`, `impact`, `reach`, `chain`, `rust`, `trivial`) and spans eight
families, adding cross-repository consumers, outward dependencies, symbol
location, fact lookup and source-body retrieval.

### Isolation

Nothing was written inside the corpus. Checked with `git status` across all 37
repositories before and after: only the two `go.sum` files that were already
dirty. Each arm's state lives outside the corpus — a `--data-dir`, an isolated
`HOME`, or a context directory under `/private/tmp`.

One arm needs saying plainly: **graphify writes `graphify-out/` beside the code
it reads**, so it only ever ran against a private copy of the corpus. Anyone
pointing it at their own repository gets a new directory inside it.

Every index was timed cold, with the derived state deleted first, because "cold"
has to mean the same thing in every row.

## Corpus

37 git repositories in one private monorepo, in Go, TypeScript, Rust, Python and
Dart. On Pass A, indexing the whole corpus published `96,482` symbols.

The corpus is private. The questions, the ground truth and the captured
responses are all published; the code they run over is not. That is the single
largest thing a reader cannot independently re-run.

Because it is private, the repository, file, package and symbol names on this
page are substituted throughout. Every count, every hop, every edge kind, every
precision figure and every token figure is the measured one; only the names were
changed. Where a token figure depends on the length of the identifiers
themselves, that is noted at the figure: it was measured against the real names,
not the substitutes printed here.

## Results

### Pass A — five code graphs and grep, seven questions

Source: `benchmarks/graph-tools-comparison/results.json`, commit `4c1bfae`,
generated 2026-08-21, tokenizer `o200k_base`, kivgraph `0.3.2`, question set
`measured`. This is the only pass in which all six arms produced real
measurements.

| tool | version | tokens | calls | precision | recall | exact |
| --- | --- | --- | --- | --- | --- | --- |
| kivgraph | `0.3.2` | `4,449` | 11 | `0.81` | `0.84` | `4/7` |
| graphify | `0.8.31` | `2,469` | 9 | `0.54` | `0.35` | `1/7` |
| graft | `0.10.1` | `8,942` | 7 | `0.14` | `0.14` | `1/7` |
| codebase-memory-mcp | `0.8.1` | `25,961` | 21 | `0.67` | `0.81` | `3/7` |
| code-review-graph | `2.3.7` | `109,298` | 10 | `0.67` | `0.85` | `3/7` |
| `grep` + reading | — | `63,531` | 27 | `1.00` | `1.00` | `7/7` |

Two readings, and they should not be mixed. **`grep` plus reading answers all
seven**, and it is the honest denominator: the real alternative to these tools is
not being wrong, it is spending `63,531` tokens. **Among the five graphs, the
cheapest is not the most accurate**: graphify is the cheapest row on the table
and answers one of seven. At `0.3.2` Kivgraph answered four of seven — the three
it missed are what the next pass was run to check.

### Pass B — the released version, twenty-nine questions

Source: `benchmarks/graph-tools-comparison/results-all.json`, commit `954b9eb`,
generated 2026-08-22, tokenizer `o200k_base`, kivgraph `0.5.0`, question set
`all`, same 37-repository corpus.

| tool | version | tokens | calls | precision | recall | exact |
| --- | --- | --- | --- | --- | --- | --- |
| kivgraph | `0.5.0` | `35,961` | 36 | `1.00` | `0.9962` | `28/29` |
| `grep` + reading | — | `267,980` | 101 | `1.00` | `0.9885` | `28/29` |

**Only two arms are published here, because only two arms ran.** In this pass the
other four failed to start, and their token columns in `results-all.json` hold
error strings rather than measurements:

- code-review-graph — `exit status 1`
- codebase-memory-mcp — `project not found or not indexed`
- graft — `chdir /private/tmp/st11/graft-ctx: no such file or directory`
- graphify started, but answered only 4 of the 29.

A failure to start is an operational fact about one machine on one afternoon, not
a measurement of a tool's accuracy, so those columns are not published in any
form. That is why Pass A above is still the five-way table even though it
measures an older Kivgraph: it is the last pass in which every arm answered.

On totals the ratio is `7.45x` in Kivgraph's favour; the median per-question
ratio is `5.95x`. Both arms reach precision `1.00`, and both miss exactly one
question. Kivgraph's miss is `R3_ts_intra`, recall `0.889` — one TypeScript test
file. `grep`'s miss is `X1_ts_shared_enum`, recall `0.667`, where the harness
note reads: "searched 5330 code files, read 6 declaring file(s); 1 true
consumer(s) never spell the symbol, so no text search reaches them".

The widest per-question margins are `X3_go_reach_depth1` at `86.8x`,
`H5_rs_trait` at `83.4x` and `X9_go_reach_depth1` at `55.4x`.

### Five questions in detail

All from Pass B, `results-all.json`.

| question | truth | kivgraph `0.5.0` | `grep` + reading |
| --- | --- | --- | --- |
| `R1_ts_xrepo` — call sites of the `withBackoff` declared in `platform-lib/src/util/retry.ts` | 5 files in 3 repositories | `332` tokens, 2 calls, exact | `10,054` tokens, 8 calls, exact |
| `X1_ts_shared_enum` — files outside `platform-lib` consuming the `StatusCode` it declares | 3 files | `530` tokens, 1 call, 3 of 3 | `12,200` tokens, 7 calls, 2 of 3 |
| `I1_go_depth2` — files holding something that reaches `jitterFor` within two hops | 2 files | `2,380` tokens, 1 call, exact | `897` tokens, 2 calls, exact |
| `H5_rs_trait` — call sites of the Rust trait method `remove_entry` | 3 files | `264` tokens, 2 calls, exact | `22,016` tokens, 4 calls, exact |
| `T1_go_trivial` — `newMetricsClient`, two occurrences in the whole corpus | 2 files | `123` tokens, exact | `65` tokens, exact |

`R1_ts_xrepo`'s truth is `client-sdk:src/client/session.ts`,
`runtime-core:src/queue/dispatcher.ts`, `runtime-core:src/queue/worker.ts`,
`runtime-core:src/queue/partitioning.ts` and `edge-service:src/rpc/server.ts`.

On `X1_ts_shared_enum` the single call was `find_cross_repo_consumers`, which
also reported 22 package-level rows separately and did not count them as uses;
`grep` missed `client-sdk:src/index.ts`. On `I1_go_depth2` `grep` is the
cheaper arm and both are exact. On `T1_go_trivial` the graph costs `1.9x` what
reading costs.

### A name is not a symbol

`withBackoff` is declared **seven times** in this corpus — four of them
TypeScript and three of them Go, spread across five repositories. `epoch_ms`,
four times, all of them Rust. That case is what splits the five graphs in Pass
A, and three of them fail it the same way.

- **codebase-memory-mcp** points its `CALLS` edges at the name, so the callers of
  all seven `withBackoff` collapse onto one node. On the Go question it recovers
  both correct files and drags in four TypeScript ones — `P=0.33`. On the
  TypeScript one the answer is empty: every caller attached to the Go homonym, so
  the TypeScript node has in-degree zero.
- **code-review-graph** disambiguates the *subject* — it refuses to choose and
  names both candidates with file and line — but not the call sites. Narrowed to
  the `storage` declaration it still returns
  `internal/secrets/provider_test.go`, which calls the other one:
  impossible in Go, different packages, unexported function. `P=0.67`.
- **graft**, given an ambiguous name, **drops the cross-file callers and warns
  that it may undercount**. That is the opposite of inventing an edge, and it is
  honest — and it leaves the Go and Rust questions at zero.

Kivgraph's edges come from `go/types`, the TypeScript checker and
`rust-analyzer` rather than from matching names, so the homonyms stay apart. The
graph holds 22 symbols named `withBackoff` — the other 15 are TypeScript barrel
`import`/`export` symbols — and the name appears in 22 files. Asked by bare name
the server refuses rather than guessing:

```text
AMBIGUOUS_SYMBOL: name "withBackoff" declares 7 symbols; repeat with the repository and path of the one you mean: config-lib:src/retry.ts:41, media-service:internal/secrets/provider.go:240, data-service:internal/secrets/provider.go:143, client-sdk:src/managers/command.ts:19, data-service:internal/storage/retry.go:49, platform-lib:src/util/retry.ts:135, client-sdk:src/types/result.ts:91
```

That refusal is not free. It costs `129` tokens — measured against the real
identifiers rather than the substituted ones printed above — and it is charged to
Kivgraph in every table on this page.

### Two numbers that are not mistakes

**code-review-graph's `82,057` tokens on impact in Pass A are a different
question, not an error.** Its `impact` takes changed *files*, not declarations:
asking about `retry.go::jitterFor` answers "0 nodes changed". Asked about
the file, two hops reach `390` nodes in `255` files, and against a two-file truth
the precision is `0.01`. It finds both — recall `1.00`. That `0.01` measures
granularity, not correctness.

**graphify's `affected` is not reverse reachability.** Its `graph.json` is written
`"directed": false`, so networkx loads an undirected graph with no in-edges and
the fallback walks edges whose stored orientation ends at the seed — and that
orientation is node insertion order, which is file walk order, not call
direction. Its own output shows it: `affected` on `withBackoff` returns its
*callees*, on `jitterFor` it returns nothing despite three incoming call
edges, and the same command in another repository returns 43 genuine callers.

## Where each tool loses

**kivgraph.** It is the slowest arm to index and the heaviest on disk: `37.6 s`
cold and `1,423 MB` of state, both measured on Pass A at `0.3.2`, not on Pass B —
Pass B recorded no index times at all. It is the only arm that needs a toolchain:
without the Go module cache or `cargo`, a load fails and those symbols are simply
absent. `grep` is cheaper than it on 5 of the 29 Pass B questions, with both arms
at recall `1.00` on all five — `A1_go_absent` `0.26x`, `A2_ts_absent` `0.38x`,
`I1_go_depth2` `0.38x`, `A3_rs_absent` `0.47x`, `T1_go_trivial` `0.53x`. Its
compact label for a module-owned use is lossy: `at` names the declaration holding
the reference, and a use with no enclosing declaration is held by its module, so
four calls in one test file produce four identical `module@1` labels instead of
four call lines. And 28 of 29 is **no known miss on twenty-nine questions, not an
absence of misses**; the row that would belong here is the question nobody has
written yet.

**`grep` + reading.** It is the most expensive arm in both passes — `63,531`
tokens on seven questions, `267,980` on twenty-nine — and in Pass B it took 101
calls to do what the graph did in 36. Its accuracy failure is narrow but real:
`X1_ts_shared_enum`, where a true consumer never spells the symbol, so no text
search reaches it. Text search cannot distinguish "no result" from "no occurrence
of this spelling".

**codebase-memory-mcp.** Name-keyed `CALLS` edges, so homonyms merge and the
callers of one declaration are attributed to another: `0.67` precision, `0.81`
recall, 3 of 7 exact in Pass A. It is also the one arm whose score moved between
identical cold passes. In Pass B it did not run at all.

**code-review-graph.** The most expensive graph in Pass A by an order of
magnitude — `109,298` tokens on seven questions, most of it the file-granularity
impact answer above. It disambiguates subjects but not call sites, and its graph
is per-repository, so a cross-package reference is structurally invisible to it.
In Pass B it did not run at all.

**graft.** The lowest accuracy in Pass A, `0.14` precision and `0.14` recall, 1 of
7 exact, because it declines ambiguous names rather than answering them. Only the
free structural tier was measured; `graft --deep` needs a provider key. In Pass B
it did not run at all.

**graphify.** `0.54` precision, `0.35` recall, 1 of 7 exact in Pass A. Its
`affected` walks an undirected graph, so the answer is not reverse reachability at
all. Its graph is per-repository, and its `build` truncates the data directory it
is given, so each repository needs its own. It writes `graphify-out/` inside the
tree it indexes. In Pass B it answered 4 of 29.

## Entry cost

From the `indexing` block of `results.json` — Pass A only, commit `4c1bfae`,
kivgraph `0.3.2`. **Pass B measured no index times**: `results-all.json` carries
an empty `indexing` block, so nothing on this table describes kivgraph `0.5.0`.

| tool | cold | disk | scope | needs |
| --- | --- | --- | --- | --- |
| codebase-memory-mcp | `5.1 s` | `221 MB` | whole corpus, 37 repositories | nothing beyond the binary |
| code-review-graph | `7.2 s` | `201 MB` | one graph per repository; built the 4 the questions name | nothing beyond the binary |
| graphify | `11.1 s` | — | one graph per repository; built the 4 the questions name | nothing for the structural pass |
| graft | `24.6 s` | `181 MB` | whole corpus, 37 repositories | nothing for the structural tier |
| kivgraph | `37.6 s` | `1,423 MB` | whole corpus, `96,482` symbols published | Go module cache, `cargo` |
| `grep` + reading | `0` | — | nothing is indexed | nothing |

A per-repository graph has a consequence beyond cost: a cross-package reference
is structurally invisible to it, which is why code-review-graph and graphify both
answer zero on that question.

## Raw outputs

Everything the tables are computed from is committed in
`benchmarks/graph-tools-comparison/`.

- **Harness** — `main.go` drives the run, `questions.go` holds every question,
  and one file per arm (`arm_kivgraph.go`, `arm_graft.go`, `arm_graphify.go`,
  `arm_cmm.go`, `arm_crg.go`) translates a question into that arm's vocabulary.
- **Ground truth** — written by hand in `questions.go` and mirrored verbatim into
  the `ground_truth` field of every question in every results JSON, so the
  scoring can be checked without reading the harness.
- **Results** — `results.json` backs the Pass A table and the Entry cost table.
  `results-all.json` backs the Pass B table and the per-question detail. The
  individual sets that `all` unions are also committed on their own:
  `results-hard.json`, `results-impact.json`, `results-reach.json`,
  `results-chain.json`, `results-rust.json` and `results-trivial.json`.
- **Captures** — the verbatim stdout of every call, one file per question and
  arm: `raw/` (the `measured` set), `raw-all/`, `raw-trivial/`, `raw-hard/`,
  `raw-impact/`, `raw-reach/`, `raw-chain/`, `raw-rust/` and `raw-0.3.6/`. That
  last directory and its `results-0.3.6.json` are misnamed: the pass they hold
  records kivgraph `0.5.0`, not the version in the filename, and no number on
  this page is attributed to the version that filename suggests.

## Reproduction

```sh
go run ./benchmarks/graph-tools-comparison --set all
```

`--set` selects the question set: `measured` (the seven of Pass A, and the
default), `hard`, `impact`, `reach`, `chain`, `rust`, `trivial`, or `all` (the 29
of Pass B). An unknown name is a failure rather than a fallback, so a run cannot
silently measure the wrong set. The other flags point each arm at its executable,
the corpus at its root, and every arm's state at an isolated directory;
`--skip-indexing` reuses existing indexes instead of rebuilding them cold.

The corpus is private, so the command reproduces the method rather than the
numbers. Pointing it at another corpus needs new ground truth in `questions.go`.

## Limitations

- 29 questions, one corpus, one machine. Not a general measure of quality for any
  of the arms.
- `o200k_base` is a proxy for the Claude tokenizer: the ratios between rows are
  the claim, the absolute values are not.
- Three full cold passes, and the spread was measured rather than assumed:
  kivgraph, graft, graphify and `grep` returned **the same number all three
  times**; code-review-graph moved by `0.1%`; codebase-memory-mcp by `1.8%` in
  tokens **and between `3` and `4` exact answers out of seven**, so it is the one
  row whose accuracy depends on the pass.
- Only the free, model-free tiers. `graft --deep`, graphify's semantic pass and
  code-review-graph's embeddings need a provider key and were not measured.
- code-review-graph and graphify indexed the four repositories the questions name
  rather than all 37, because their graph is per-repository.
- Kivgraph was measured with its default view, not the `files` view that answers
  the same reference questions for a fraction of the tokens. Taking the discount
  only one arm has would have compared our summary against everyone else's
  detail.
- The corpus is private. The questions, the ground truth and the captured
  responses are publishable; the code is not, so nobody outside this machine can
  re-run the measurement itself.
