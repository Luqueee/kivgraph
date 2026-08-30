# What the graph costs an agent that edits

> **Result.** This measurement answers issue `#106`, which asked whether
> [ADR 0057](../../docs/adr/0057-el-camino-incremental-se-retira.md) had measured
> the wrong workload. It had not. Publication is **`82,7 %`** of a pass after a
> one-file edit, the delta route's ceiling on this workload is **`1,62x`** --
> the same ceiling ADR 0057 measured on the other one -- and the number that
> actually moves the session is **how often a rebuild is triggered**, not what
> one costs. ADR 0057 stands.

ADR 0057 retired the delta route on a ceiling of `1,63x`, measured against a
corpus-level scenario: one rebuild against another, on a corpus that had just
been pulled. Issue `#106` did not dispute that measurement. It questioned
whether the workload it was taken against is the one that now matters, and named
a different one -- **an agent editing files across steps within a single task**,
where the question is not what one rebuild costs against another but how often a
rebuild is triggered and what the session pays while it runs.

This benchmark measures that workload. The raw metrics are in `results.json`.
No acceptance verdict is emitted and no latency gate is asserted.

## Environment and provenance

|field|value|
|---|---|
|date|2026-08-30|
|commit|`c66642f-dirty`, harness committed as `e8d59ec`|
|machine|Linux `6.12.94`, `x86_64`, 16 CPU|
|toolchains|`go1.26.6`, LadybugDB `v0.13.1`, `node v24.18.0`, `rust-analyzer` from the pinned build|
|corpus|53 registered repositories, `go` + `typescript` + `rust`|
|size|`6.473` files, `178.851` symbols, `719.022` edges|
|edited repository|a private copy of `kivgraph` at `53055a4`, `356` Go files|
|edits|`10`, one appended declaration each, one file per step|
|fact cache|**cold for the warm-up pass, warm for every edit pass**|
|search arm|`grep` with the exclusions ripgrep gets from `.gitignore`, best of `5`|

The corpus is larger than the one ADR 0057 measured -- `6.473` files against
`4.768`, `719.022` edges against `493.521` -- which matters for one claim below
and for no other: a ratio that survives `1,46x` more edges is not an artefact of
corpus size.

The warm-up pass is measured, published and **excluded from every statistic
here**. It is the pass that fills the fact cache, and an editing agent never
runs it: mixing a cold pass into the median reports a cost nobody pays.

## The workload

A session is `N` file edits interleaved with `M` questions. The graph arm has to
republish before a question can be answered against the edit; the search arm
pays nothing for an edit and pays per question. So the two arms meet in one
quantity -- **how many questions one rebuild buys** -- and everything below
reduces to that.

Each step appends one exported declaration to one Go file of the copy and then
runs a full pass. An addition rather than a rewrite, because the effect on the
graph is then unambiguous: one new symbol, and every fact the file already
asserted still asserted. The symbol count rises by exactly one per step --
`178.852` through `178.861` -- which is the check that the edits landed.

## The pass after one edit

Ten passes, warm cache, one edited file each. Median, with the range:

|half|median seconds|share|
|---|---|---|
|**analysis** -- start, language engines, merge|`2,861`|`16,7 %`|
|**publication** -- staging, bulk load, integrity, snapshot, probes, swap|`14,191`|`82,7 %`|
|**total**|**`17,150`**|100 %|

The spread across the ten is `16,662`–`17,797 s`: the pass after an edit is a
constant, not a distribution with a tail.

The boundary between the halves is not an estimate. `RunFull` calls the rebuild
progress sink with the name of each stage as it starts, so the first call is the
instant the analysis half ended.

### Publication, by stage

|stage|median seconds|share of the pass|
|---|---|---|
|`facts`|`0,263`|`1,5 %`|
|`staging`|`1,092`|`6,4 %`|
|`graph.next`|`0,000`|`0,0 %`|
|`bulk load`|`5,461`|`31,8 %`|
|`integrity`|`2,889`|`16,8 %`|
|`snapshot`|`3,802`|`22,2 %`|
|`golden probes`|`0,017`|`0,1 %`|
|`publish`|`13,593`|`79,3 %`|

`publish` is the **enclosing** stage: the generation store runs staging through
the golden probes inside its build callback, so its `13,593 s` covers the rows
above it -- which sum to `13,524` -- and is not another cost. The `0,598 s` between it and the `14,191 s`
of the publication half is resolving the next generation, writing the digest and
pruning.

`staging` and `bulk load` are the two reported halves of **one** canonical load,
and together they are `6,553 s`, `38,2 %` of the pass. That is the same fraction
ADR 0057 reported for writing the canonical graph whole (`37,1 %`) on a corpus
two thirds the size.

### Analysis is already incremental, and it is incremental per module

Every one of the ten edit passes reports the same cache line: **`55` hits and
`1` miss** out of `56` analysis units. The one miss is the Go module that owns
the edited file. Nothing else in the corpus was re-analysed, including the
TypeScript packages of the same repository.

So the fact cache delivers what the incremental path promised for the analysis
half, and the cold pass says how much: `108,609 s` of analysis cold against
`2,861 s` warm, a factor of `38`. What it does not deliver is **file**
granularity. A cache unit for Go is one module (`unitIdentity` keys it on
repository and module path), so a one-line edit re-loads that whole module. The
`2,861 s` is the cost of re-loading `kivgraph`'s Go module in full, and it would
be larger for a larger module. That is where the analysis half's remaining
headroom is -- and it is `16,7 %` of the pass.

## What a delta would have saved on this workload

The delta route no longer exists, so this is a projection from the stages ADR
0057 documents it skipping, exactly as ADR 0057's own ceiling was. It skipped
writing the canonical graph -- `staging` and `bulk load` here -- and it ran
**zero** integrity checks and golden probes. It kept the analysis half, the
`facts` stage, and a complete rebuild of the `HotSnapshot`.

|route|seconds|against the full pass|
|---|---|---|
|full pass|`17,150`|`1,00x`|
|delta **as it was written**|`7,691`|`2,23x`|
|delta **if it also verified**|`10,597`|`1,62x`|

ADR 0057 measured `2,33x` and `1,63x`. **The ceiling did not move.** It could
not: both of the things that fixed it -- a `HotSnapshot` rebuilt whole
(`3,802 s`, `22,2 %` of the pass here) and a set of facts that is not bounded by
the edit -- scale with the corpus and not with the edit, so changing the workload
from a corpus-wide pull to a single-file edit changes neither term. A corpus with `1,46x`
more edges returned a ratio within a hundredth of the old one, which is what a
corpus-scaling cost looks like.

## The crossover

The search arm answers each question the way a session without the graph does:
one word-boundary search across all 53 repositories, then reading every file it
matched. Reading is not padding -- a list of line numbers is not an answer to
*who calls this*, and an arm that skipped it would be answering a different
question from the one the graph answers.

|question|seconds|matches|files|bytes read|
|---|---|---|---|---|
|`NewServer`|`0,082`|`192`|`68`|`1,7 MB`|
|`Run`|`0,118`|`816`|`333`|`9,8 MB`|
|`Load`|`0,100`|`466`|`157`|`9,0 MB`|
|`Close`|`0,112`|`1.320`|`352`|`14,4 MB`|
|`Options`|`0,103`|`419`|`134`|`13,9 MB`|
|`Report`|`0,096`|`228`|`46`|`2,5 MB`|
|**median**|**`0,101`**||||

So, in wall clock:

|route|seconds per rebuild|questions one rebuild buys|
|---|---|---|
|full pass today|`17,150`|**`169,5`**|
|delta at its measured ceiling|`10,597`|`104,7`|
|a rebuild that cost nothing|`0`|`0`|

Read as an **edit rate**, the crossover is at zero: above zero edits the graph
is behind the search arm in wall clock, and it stays behind. Read as a **question
count**, it is `169,5` -- a session would have to ask a hundred and seventy
reference questions between two edits for one rebuild to pay for itself in
seconds. Both readings are the same fact from opposite ends, and neither sits
far outside realistic agent behaviour in the direction the issue hoped: one sits
at zero, and the other two orders of magnitude above any session observed here.

### And that is the wrong currency

The graph's advantage was never wall clock, and this is where the two arms stop
being comparable in one number.

An edit costs the graph **`17,150` seconds and roughly zero tokens** -- a rebuild
is one tool call whose result is a summary the session does not read files from.
A question costs the search arm tokens in proportion to what it had to read:
`14,4 MB` of source for `Close`, of which the answer is a handful of lines.

Measured elsewhere in this repository, on the corpus that arm actually paid for:

- `benchmarks/graph-tools-comparison/results-all.json`, 29 questions over a
  37-repository workspace: `267.980` tokens for the search-and-read arm against
  `35.961` for Kivgraph, **`7,45x`**, at the same precision and marginally
  higher recall.
- `benchmarks/mcp-token-cost/results.json`, six reference questions: `1,76x` on
  the answer alone, `1,24x` on the whole session including the bodies both arms
  open.

So the two currencies do not cross in the same place, and **neither crossing is
moved by a delta**:

- In seconds, the graph is behind from the first edit and a delta at `1,62x`
  leaves it behind by `104,7` questions instead of `169,5`.
- In tokens, the graph is ahead on the workloads those two benchmarks measured,
  and a rebuild adds one tool call rather than a body to read. That an edit
  therefore costs the graph nothing in tokens is an **inference** from the shape
  of a rebuild, not a measurement taken here: no arm of this harness counts
  tokens. What follows from it -- that no edit rate flips the token result --
  inherits that status.

## What does move the session: the trigger, not the route

Issue `#106` was right that the relevant number is *how often a rebuild is
triggered*. Here is what that is worth, on this run's own workload of ten edits:

|policy|seconds spent indexing|
|---|---|
|reindex after every edit|`10 x 17,150` = `171,5`|
|reindex after every edit, with a verifying delta|`10 x 10,597` = `106,0`|
|reindex once, after the last edit|`1 x 17,150` = `17,2`|

**Batching the ten edits into one rebuild saves `154,4 s`. A verifying delta on
ten rebuilds saves `65,5 s`.** Batching is worth `2,36x` the delta, it is a
scheduling decision rather than a new write path, and it carries none of the
provenance burden ADR 0056 imposes on a fact an incremental pass asserts.

### What actually triggers a rebuild today

Worth recording, because issue `#106` describes this differently and the code
says otherwise:

- **The file watcher has no production caller.** `internal/watcher.New` is
  referenced only by `internal/resilience/shutdown_test.go`, `NewBatcher` by
  nothing that resolves, and `config.Watcher.Enabled` -- whose default is `true`,
  not off -- is read by no code outside `internal/config`. Nothing watches files.
- What triggers a rebuild is an explicit `kivgraph index --full` or the
  `index_project` tool, and the daemon's HEAD resync when a repository's git HEAD
  moves, debounced per workspace.
- So **an uncommitted edit triggers nothing.** The graph goes stale silently
  until someone asks for a pass. The cost of an edit today is not `17,150 s` of
  indexing; it is a graph that no longer describes the file the agent is editing.

That is the finding worth carrying out of this issue, and it is not a cost
problem.

## The three questions the issue asked

- [x] **Define an edit-frequency workload.** `N` appended declarations across `N`
      files of one repository, interleaved with reference questions, on a live
      53-repository corpus with a warm fact cache. It is in `main.go` and it is
      reproducible.
- [x] **Measure the current full-rebuild path against it, and find the
      crossover.** `17,150 s` per rebuild against `0,101 s` per search-and-read
      question. The crossover is at `169,5` questions between two edits, which is
      to say there is no crossover: the graph is behind in wall clock from the
      first edit, and ahead in tokens at every edit rate.
- [x] **Establish how much of the cost is analysis versus publication.**
      Publication is `82,7 %`. Analysis is `16,7 %` and is already incremental
      at module granularity -- `55` cache hits to `1` miss on every edit pass.

The issue's own gate reads: *if publication dominates, the delta route addresses
the wrong half and this issue should close again.* Publication dominates, at
`82,7 %`. The delta route did address most of that half -- but it kept the
single largest cost inside it, the whole-`HotSnapshot` rebuild, and it skipped
the verification. Which is exactly why its ceiling is `1,62x` here and was
`1,63x` there.

**ADR 0057 stands.**

## Reproduce

The harness needs the native LadybugDB build to publish a generation, and it
must run as a **built binary**: the fact cache is keyed on the fingerprint of the
indexing executable, so a run driven by `go run` recompiles to a new path and can
never hit its own warm-up.

```bash
LIB="$(scripts/fetch-ladybug.sh)"
CGO_ENABLED=1 CGO_CFLAGS="-I$LIB" CGO_LDFLAGS="-L$LIB" \
  go build -tags ladybug -ldflags="-extldflags=-Wl,-rpath,$LIB" \
  -o /tmp/edit-frequency ./benchmarks/edit-frequency

# cargo and the ts-worker on the PATH, or the corpus loses Rust and TypeScript
export PATH="$HOME/.cargo/bin:$HOME/.local/opt/kivgraph/bin:$PATH"

/tmp/edit-frequency -edits 10 -grep-repeats 5 \
  -root      "$HOME/.cache/edit-frequency/root" \
  -fact-cache "$HOME/.cache/edit-frequency/factcache" \
  -scratch   "$HOME/.cache/edit-frequency/scratch" \
  -output benchmarks/edit-frequency/results.json
```

The run never writes inside a registered repository and never publishes into the
machine's own generation root: it refuses both, and the refusals have tests.

## Limitations

- **The edits land in a private copy of one repository.** The other 52 are read
  where they are and never written to. That is the workload the issue names --
  an agent inside one repository -- and not a corpus-wide pull.
- **Every step is one appended declaration.** A larger edit changes what the
  analysis half costs. Publication was **not observed to move with the edit** --
  `13,891`–`14,942 s` across the ten, and `14,611 s` on the cold pass whose
  analysis half was thirty-eight times larger -- which is what rewriting the
  whole graph either way predicts. But one edit shape was measured, so that is
  an observation over this workload and not a proof.
- **The delta figures are a projection**, from the stages ADR 0057 documents the
  retired route skipping. There is no delta to measure and there has never been
  one; this is the same class of estimate as ADR 0057's, on the same stages.
- **The search arm ran `grep`, not ripgrep.** The agent hosts on this machine
  ship ripgrep inside their own executable rather than on the `PATH`, so
  `exec.LookPath("rg")` fails and the harness falls back to `grep` with the
  exclusions ripgrep would get from `.gitignore`. ripgrep is the faster of the
  two, so `169,5` questions per rebuild is a **lower bound**: the real searcher
  makes the graph's wall-clock deficit larger, not smaller.
- **The token figures are cited, not measured here.** They come from
  `benchmarks/graph-tools-comparison` and `benchmarks/mcp-token-cost`, on their
  own corpora, and are quoted as orders of magnitude rather than transported.
- **Memory is not measured.** A pass on this corpus is visibly larger than the
  `1,4 GB` the issue quotes for a smaller one, but this harness records no
  memory and no number from it should be cited for one.
- **The run was taken from a dirty tree**, so `commit` carries the `-dirty`
  suffix: the harness existed as uncommitted work when it measured. No Go source
  changed between the run and the commit that carries it, so `e8d59ec` is the
  code that produced these numbers -- but the suffix stays, because an artefact
  that dropped it would attribute them to a parent commit that has no harness in
  it at all.
- **The published `results.json` is `edit-frequency-v1`; the harness now emits
  `v2`.** `v2` makes the analysis/publication split absent rather than zero when
  a pass never reports where the rebuild began, and adds
  `passes_with_boundary`. Every pass in this file reported one, so `v2` would
  have written the same measured values plus that count -- but the file is left
  exactly as the run produced it rather than migrated, because an artefact
  edited to look like the output of code that never ran is the provenance defect
  this directory's rules exist to prevent.
- **Wall clock on a shared machine is not an SLO.** A daemon and a log follower
  were running throughout. No gate is asserted and no acceptance verdict is
  emitted.
