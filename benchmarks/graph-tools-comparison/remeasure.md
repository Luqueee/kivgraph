# Re-measurement question set, selected blind

Three fixes landed after the seven questions in `report.md` were measured. Those
seven cannot judge them: a benchmark you tune against stops measuring. This set
exists to be the second opinion, and it was chosen **before any fix was run
against it**, by a rule rather than by hand.

## The selection rule, fixed in advance

Over the corpus `/path/to/workspace`, excluding
`node_modules`, `dist`, `build`, `target`, `.next`, `graphify-out` and any
`vendor` tree -- vendored third-party source is not the project's code:

1. Parse every `.go`, `.ts` and `.rs` file for a top-level named function
   declaration: `func Name(` for Go, `export function name(` for TypeScript,
   `pub fn name(` for Rust.
2. Keep the names declared **exactly once** in the whole corpus, so no answer
   can hide behind a homonym, and at least five characters long.
3. Keep those whose bare name occurs in between four and eight distinct files:
   enough that the answer is not trivial, few enough that every occurrence can
   be read and classified by hand.
4. Take the **alphabetically first** survivor. One subject per language.

The rule left 133 candidates in Go, 408 in TypeScript and 22 in Rust. The three
it selected are below, with every occurrence read and attributed.

## The three subjects

### `N1_go` -- `Capture`

Declared at `services/go-svc-a/internal/infrastructure/glitchtip/glitchtip.go:84`.
Occurs in four files; **one** is a caller.

|file|line|what it is|
|---|---|---|
|`glitchtip.go`|`84`|the declaration|
|`glitchtip_test.go`|`39`, `40`|**calls**|
|`mongo/global_repository.go`|`103`|a comment: "Capture the raw version BEFORE defaulting"|
|`mongo/premium_repository.go`|`416`|a comment: "Capture the OLD cache keys BEFORE the patch"|

Ground truth: `services/go-svc-a/internal/infrastructure/glitchtip/glitchtip_test.go`.

Two of the three non-declaring files are English prose that happens to start a
sentence with the verb. A name-matching tool answers three files and scores
`P=0.33`; the only caller is in a test file, so a tool that does not load tests
answers nothing and scores `R=0.00`.

### `N2_ts` -- `__resetAllVoiceRescueState`

Declared at `modules/music-module/src/shared/voice-rescue-state.ts:346`.
Occurs in four files; **three** are callers, each importing it and calling it.

Ground truth:

- `modules/music-module/src/events/lavalink/PlayerUpdate.test.ts`
- `modules/music-module/src/events/lavalink/PlayerVoiceStuck.test.ts`
- `modules/music-module/src/shared/voice-rescue-state.test.ts`

All three live under `src/`, so a project whose `include` is `src/**` claims
them. This question asks nothing of the unclaimed-sources work; it is here to
catch a regression in the ordinary path.

### `N3_rust` -- `build_router`

Declared at `services/rs-svc-a/src/http/routes.rs:28`.
Occurs in four files; **two** are callers.

|file|line|what it is|
|---|---|---|
|`src/http/routes.rs`|`28`|the declaration|
|`src/app.rs`|`25`, `162`|imports it and calls it|
|`src/http/openapi.rs`|`4`|a doc comment naming `routes::build_router`|
|`tests/http_surface.rs`|`16`, `133`|imports it and calls it|

Ground truth: `services/rs-svc-a/src/app.rs` and
`services/rs-svc-a/tests/http_surface.rs`.

One occurrence is a doc comment, and one caller is an integration test outside
`src/`, which is a different inclusion question from Go's.

## What this set can and cannot show

It can show that a fix did not break the ordinary path, and it can show whether
comment occurrences are still correctly refused. Three questions cannot show
that a tool is good; they are a second opinion on three fixes, not a verdict.

### The defaults that decide answerability

Read from `internal/config/config.go` rather than assumed, because each one
decides whether a question has a reachable answer at all:

|lever|default|consequence here|
|---|---|---|
|`go.include_tests`|`false`|`N1`'s only caller is a `_test.go`, so the honest default answer is an absence|
|`rust.include_tests`|`true`|`N3`'s `tests/http_surface.rs` caller is in scope|
|`typescript.include_unclaimed_sources`|`false`|`N2`'s three callers live under `src/`, which the project's `include` claims, so this lever does not apply to them|

`N1` is therefore the one question whose result must be read twice: once with
the shipped default, where an empty answer is correct, and once with tests
enabled, where a wrong answer would be a real failure.

`grep` plus reading answers all three, as it did the original seven, and costs
what reading four files costs. The interesting number is not whether we beat it
but whether we still agree with it after changing the indexer.

## What it measured

Generation `000001` of an isolated `HOME`, `kivgraph 0.3.6` with the three
fixes, `go.include_tests`, `rust.include_tests` and
`typescript.include_unclaimed_sources` all on. `find_references` with
`view: "files"`, scoring file sets and excluding the declaring file.

|question|`P`|`R`|exact|before|
|---|---|---|---|---|
|`R1_ts_xrepo`|`0.56`|**`1.00`**|no|`0.00` / `0.00`|
|`R3_ts_intra`|`1.00`|`0.89`|no|`1.00` / `0.89`, unchanged|
|`N1_go`|`1.00`|`1.00`|**yes**|new|
|`N2_ts`|`0.00`|`0.00`|no|new|
|`N3_rust`|`1.00`|`1.00`|**yes**|new|

The corpus-wide numbers moved too: symbols `96.482` -> `125.821`, edges
`367.725` -> `468.391`, and TypeScript unresolved references `15.198` ->
`5.800`.

### What the fixes bought

`R1` went from naming none of the five call sites to naming **all five**. The
installed-copy bridge resolves `804` of `804` `@private/shared` imports that
previously had no target at all. Precision is `0.56` because four re-export
barrels still rank as callers, which is a separate defect: an incoming
references answer mixes `REEXPORTS` with `CALLS_DIRECT`, so a barrel
forwarding a name looks like a site using it.

`N1` and `N3` are exact, and both refused occurrences a name matcher would
have claimed: two English comments beginning "Capture the ..." in Go, and a doc
comment naming `routes::build_router` in Rust.

### What the fixes did not buy, and why

`R3` did not move, and `N2` \u2014 an ordinary intra-repository question, nothing to
do with either new feature \u2014 answers nothing at all. One defect explains both,
and neither the seven original questions nor the fixtures of the two features
could have found it.

Both files **are** indexed: `get_file_outline` returns declarations for
`core/tests/.../ipcCase.test.ts` (so the unclaimed-sources work did land) and
for `music-module/src/shared/voice-rescue-state.test.ts` (which its project
always claimed). What is missing is not the file but its calls.

Every missing call sits inside an **anonymous callback**:

|file|line|enclosing scope|
|---|---|---|
|`core/tests/.../ipcCase.test.ts`|`44`, `50`|`it(...)` callback|
|`music-module/.../voice-rescue-state.test.ts`|`18`|`afterEach(...)` callback|
|`music-module/.../voice-rescue-state.test.ts`|`156`|`it(...)` callback|
|`music-module/.../PlayerUpdate.test.ts`|`424`|`beforeEach(...)` callback|

`LocalReference.source` in `ts-worker/src/reference-extractor.ts` is documented
as "undefined for a top-level use without a containing local declaration", and
`internal/facts/typescript.go:397` drops exactly those into
`EdgesWithoutSource`. An arrow function passed as an argument to `it` is not a
named local declaration, so a call in its body has no source and its edge is
never emitted.

That erases the calls of **every** `vitest` and `jest` test file, which is the
same population the two test-inclusion levers and the unclaimed-sources work
exist to reveal. The files arrive; their calls do not. Fixing it is a decision
about the shape of the graph \u2014 today an edge runs symbol to symbol, and a use
in an anonymous callback has no symbol to start from \u2014 so it wants an ADR
rather than a patch.

The fixture of the unclaimed-sources work missed this because its call sat in a
named function (`readsTheRequiredField`). That is the warning in
`ts-worker/AGENTS.md`, earned again: a fixture proves the real case or it
proves nothing.

## The original seven, re-run on `0.3.6`

Same harness, same corpus, same five rivals, commit `b4afce9`. Raw numbers in
`results-0.3.6.json`, kivgraph's literal answers in `raw-0.3.6/`. `report.md`
keeps the `0.3.2` measurement it is dated for.

|arm|tokens|`P`|`R`|exact|
|---|---|---|---|---|
|**kivgraph** `0.3.2` -> `0.3.6`|`4.449` -> `6.295`|`0,81` -> **`0,94`**|`0,84` -> **`0,98`**|`4/7` -> **`5/7`**|
|graft `0.10.1`|`8.942` -> `8.770`|`0,14`|`0,14`|`1/7`|
|code-review-graph `2.3.7`|`109.298`|`0,67`|`0,85`|`3/7`|
|graphify `0.8.31`|`2.469`|`0,54`|`0,35`|`1/7`|
|codebase-memory-mcp `0.8.1`|`25.961` -> `26.599`|`0,67`|`0,81`|`3/7`|
|`grep` + reading|`63.531`|`1,00`|`1,00`|`7/7`|

The four rivals did not move, which is what validates the pass: same versions,
same corpus, only our arm changed.

Per question, ours:

|question|before|after|
|---|---|---|
|`R1_ts_xrepo`|`0,00` / `0,00`|**`0,56` / `1,00`**|
|`R2_go`|`1,00` / `1,00`|`1,00` / `1,00`|
|`R3_ts_intra`|`1,00` / `0,89`|`1,00` / `0,89`|
|`R4_rust`|`1,00` / `1,00`|`1,00` / `1,00`|
|`I1_go_depth2`|`0,67` / `1,00`|**`1,00` / `1,00`, exact**|
|`O1_ts_large`|`1,00` / `1,00`|`1,00` / `1,00`|
|`O2_go_small`|`1,00` / `1,00`|`1,00` / `1,00`|

`I1` became exact because the spurious row it used to claim was the build-cache
file. `R1` went from naming none of the five call sites to naming all five, and
its four remaining false positives are the re-export barrels. Nothing regressed.

That pass is superseded by the one at the end of this file, which adds the
module symbol and closes `R3`.

**The answer got dearer, not cheaper: `4.449` -> `6.295` tokens.** `R1` now
returns ten rows where it used to return three, and `I1` names a real second
hop instead of a cache entry. That is the trade, and it is the honest direction
of it: a wrong short answer was replaced by a right longer one.

### A harness bug this uncovered

The first re-run scored `R1` at `0,00` / `0,00` even though the answer already
carried all five call sites, and the summary said `5/7` for the wrong reason.
`arm_kivgraph.go` read a group's repository from the answer's header, and the
server hoists that header **only when every row shares one repository**. While
`R1` was wrong, its three claimed files all sat in `library-shared`, so the
header existed and the parser worked. The moment the fix made the answer span
four repositories, the header disappeared, each row carried its own `repo`, and
the parser -- which never read that field -- scored an empty set.

A benchmark that under-reports its own subject is still a broken benchmark, so
the row parser now prefers the row's repository and falls back to the header.
The numbers above are from the corrected parser, against captures on disk.

## The module symbol: `R3` closed

The third defect above is fixed. ADR 0052 has the decision; this is what it
measured. `results-0.3.6.json` and `raw-0.3.6/` now hold this pass.

|question|`0.3.2`|+ the two fixes|+ the module symbol|
|---|---|---|---|
|`R1_ts_xrepo`|`0,00` / `0,00`|`0,56` / `1,00`|`0,56` / `1,00`|
|`R2_go`|`1,00` / `1,00`|`1,00` / `1,00`|`1,00` / `1,00`|
|`R3_ts_intra`|`1,00` / `0,89`|`1,00` / `0,89`|**`1,00` / `1,00`, exact**|
|`R4_rust`|`1,00` / `1,00`|`1,00` / `1,00`|`1,00` / `1,00`|
|`I1_go_depth2`|`0,67` / `1,00`|`1,00` / `1,00`|`1,00` / `1,00`|
|`O1_ts_large`|`1,00` / `1,00`|`1,00` / `1,00`|`1,00` / `1,00`|
|`O2_go_small`|`1,00` / `1,00`|`1,00` / `1,00`|`1,00` / `1,00`|
|**aggregate**|`0,81` / `0,84`, `4/7`|`0,94` / `0,98`, `5/7`|**`0,94` / `1,00`, `6/7`**|

Recall `1,00` is the sentence worth reading: across the seven questions no
caller is missed any more. The one question that is not exact is `R1`, and its
four false positives are the re-export barrels -- a separate defect, named in
the losses table and not fixed here.

Of the three blind questions, `N2_ts` also went from `0,00` / `0,00` to exact,
measured on the same index. `N1_go` and `N3_rust` were exact before and after.

### The harness had to change with it

`R3` did not move on the first run with the module symbol, and the reason was
the harness, not the graph: it enabled `go.include_tests` but not
`typescript.include_unclaimed_sources`, so the test file was never indexed and
the module symbol had nothing to own. Both levers are off by default and both
are what make our arm see the file set the four tree-sitter arms see on disk, so
`prepareKivgraphHome` now enables both and fails loudly if the key is absent.

The two are a pair, and that is worth stating plainly: **the unclaimed-sources
work alone bought nothing measurable.** It indexed the file; without an owner
for a call in an anonymous callback, every call it made was still dropped.

### Two numbers that moved the wrong way

`52,0 s` to index and `1.607 MB` on disk, against `50,3 s` and `1.542 MB`
before, for `126.720` symbols against `122.583`. And `codebase-memory-mcp`
reports `4/7` here against `3/7` in the previous pass -- that is its own
measured variance, `3` to `4` exact answers across three passes, recorded in
`report.md`, and not something this change caused.

## Forwarding is not a use: `R1` closed, and grep matched

The fourth defect is fixed. ADR 0053 has the decision; this is what it measured.
`results-0.3.6.json` and `raw-0.3.6/` hold this pass, on commit `71e6c57`.

`find_references` was answering with the `EXPORTS` and `REEXPORTS` edges beside
the ones that use the symbol, so four re-export barrels ranked next to the five
real call sites of `withRetry`. Those four were **every** false positive Kivgraph
had: four of the forty-eight files it claimed across the seven questions, and
the only reason its precision was not `1,00`.

|question|`0.3.2`|+ module symbol|+ forwarding excluded|
|---|---|---|---|
|`R1_ts_xrepo`|`0,00` / `0,00`|`0,56` / `1,00`|**`1,00` / `1,00`**|
|`R2_go`|`1,00` / `1,00`|`1,00` / `1,00`|`1,00` / `1,00`|
|`R3_ts_intra`|`1,00` / `0,89`|`1,00` / `1,00`|`1,00` / `1,00`|
|`R4_rust`|`1,00` / `1,00`|`1,00` / `1,00`|`1,00` / `1,00`|
|`I1_go_depth2`|`0,67` / `1,00`|`1,00` / `1,00`|`1,00` / `1,00`|
|`O1_ts_large`|`1,00` / `1,00`|`1,00` / `1,00`|`1,00` / `1,00`|
|`O2_go_small`|`1,00` / `1,00`|`1,00` / `1,00`|`1,00` / `1,00`|
|**aggregate**|`0,81` / `0,84`, `4/7`|`0,94` / `1,00`, `6/7`|**`1,00` / `1,00`, `7/7`**|

Counted in files rather than in means: `44` claimed, `0` false, `0` missed of
`44` true. That is the same score `grep` plus reading gets, for `6.200` tokens
against `63.531` -- `10,2x` -- and `886` tokens per correct answer.

Being right for the first time on all seven is the moment to be most careful
about the denominator. **Seven questions on one corpus is a small set**, chosen
for what it could discriminate, and three of them were written blind and
re-scored only after. A tool that answers seven of seven here is not a tool that
answers everything; it is a tool with no known miss on this set. The set is in
`questions.go` and adding a question that fails is the useful contribution.

### It also got cheaper, and not only by four rows

`6.381` to `6.200` tokens overall, and `R1` itself from `526` to `332`. Both
passes spent the same two calls on it -- one ambiguous-name refusal, one answer
-- so this is not a page saved: the answer call went from `397` tokens to `203`.

The captures say why. Before, the page carried **four groups and an empty
header**: the five `IMPORTS_SYMBOL` rows and the four forwarding rows disagreed
on `kind`, `edge_kind`, `confidence` and `provenance`, so none of the four could
hoist and every group restated its tuple. After, the five rows that remain agree
on all four, the header hoists all of them and there are no groups at all.
Removing rows that dissent makes the rows that stay cheaper to state, which is
more than the cost of the rows themselves.

### What did not change, and why

`get_blast_radius` still reports the barrels. It answers "what breaks if I
change this", and a rename does break an `export { withRetry } from …`. So does
`trace_dependencies`, which walks outward and would truncate a real reach.
`I1_go_depth2`, the impact question, is unmoved at `1,00` / `1,00`, which is the
evidence that the change stayed inside the tool it was aimed at.

### Machine load, not a regression

Every arm indexed slower on this pass -- kivgraph `52,0` to `63,1 s`, graft
`26,4` to `29,1`, graphify `11,7` to `13,8`, codebase-memory `5,1` to `6,1`,
code-review-graph `7,7` to `9,2`. Five out of five moving together by about a
fifth is the machine, not five regressions; the symbol count is identical at
`126.720`. `codebase-memory-mcp` reports `3/7` again after `4/7` last pass,
which is the `3`-to-`4` variance already recorded in `report.md`.
