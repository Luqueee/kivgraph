# Where a pass after one edit spends its time, per stage (phase 6a)

> **Result.** On a one-repository Go corpus (343 files, 12,465 symbols), a pass
> after a one-file edit takes **`3,247 s`**, and publication is **`59,2 %`** of
> it. The largest stage is the bulk load of the canonical graph (`0,878 s`,
> `27,0 %`), which runs inside LadybugDB where a Go profile cannot see. Garbage
> collector settings do not move publication. The corpus is about 14 times
> smaller than the one behind the `17,150 s` figure, so this does **not** say
> whether the `8 s` target of phase 6 is reachable on that corpus.

This is a re-measurement of the stage split in
[`benchmarks/edit-frequency`](../edit-frequency/report.md) with the same
harness, plus a CPU profile of the edit passes. Raw metrics: `results.json`.
No gate is asserted and no acceptance verdict is emitted.

## Environment and provenance

|field|value|
|---|---|
|date|2026-11-15|
|commit|`81de018406f855f5ceb0453c194c108eb2121222`, clean tree|
|machine|macOS, `arm64`, Apple M5, 10 CPUs|
|toolchain|`go1.27.4` (the one on the `PATH`); CI and releases build with `go1.26.11`|
|corpus|1 registered repository, `go` only, a clone of this repository at `19dd106`|
|size|`343` files, `12.465` symbols, `73.598` edges|
|edits|`10`, one appended declaration each, one file per step, `393` Go files to choose from|
|fact cache|cold for the warm-up, warm for every edit pass|
|search arm|`ripgrep`, best of `5`|

The warm-up (`3,417 s`: analysis `1,346`, publication `2,071`, `12` units
missed) is published and excluded from every statistic below.

## The pass after one edit

Ten passes, median with range:

|half|median seconds|share|
|---|---|---|
|analysis|`1,327`|`40,9 %`|
|publication|`1,921`|`59,2 %`|
|**total**|**`3,247`** (`3,210`–`3,289`)|100 %|

### Publication, by stage

|stage|median seconds|share of the pass|
|---|---|---|
|`facts`|`0,006`|`0,2 %`|
|`staging`|`0,078`|`2,4 %`|
|`graph.next`|`0,000`|`0,0 %`|
|`bulk load`|`0,878`|`27,0 %`|
|`integrity`|`0,260`|`8,0 %`|
|`snapshot`|`0,308`|`9,5 %`|
|`golden probes`|`0,024`|`0,7 %`|
|`publish` (encloses the rows above)|`1,905`|`58,7 %`|

The rows inside `publish` sum to `1,555 s`. The other **`0,351 s` (`10,8 %` of
the pass) is inside `publish` and in no listed stage**: resolving the next
generation, the digest, the swap and the pruning. It is as large as the
snapshot build and nothing reports it. `[INFERENCE]` from the stage names the
harness reads; I did not instrument it.

Against the earlier Linux run on the 53-repository corpus, the shares moved with
corpus size the way a cost that scales with the corpus would:

|stage|this run|53 repositories, Linux|
|---|---|---|
|`snapshot`|`9,5 %`|`22,2 %`|
|`bulk load` + `staging`|`29,4 %`|`38,2 %`|
|`integrity`|`8,0 %`|`16,8 %`|
|publication, in total|`59,2 %`|`82,7 %`|

The two runs differ in operating system, CPU, toolchain and corpus, so only the
shape is comparable, not a single figure.

### Analysis

Each edit pass reports `8` cache hits and `4` misses of `12` units; the
53-repository run reported `1` of `56`. One edited file therefore re-analyses
four units here. This repository holds `12` Go modules, most of them test
fixtures, and `[INFERENCE]` the other three depend on the one that was edited.
Not checked: `unitIdentity` was not read for this. It is `40,9 %` of the pass
and the only half the fact cache can still shrink.

## What the Go profile shows

`-cpuprofile` covers the ten edit passes only: `32,6 s` of wall time and `48,8 s`
of CPU samples (`150 %`).

|where|CPU seconds|of samples|
|---|---|---|
|under `rebuild.Run`|`5,1`|`10,4 %`|
|`ladybug.LoadCanonical` (cgo into LadybugDB)|`2,9`|`6,0 %`|
|`ladybug.VerifyCanonicalIntegrity`|`0,4`|`0,8 %`|
|`<unknown>` and `_ExternalCode` (native code)|`7,7` and `8,7`|`15,7 %`, `17,8 %`|
|`runtime.madvise`|`12,0`|`24,6 %`|

Two things follow, and one does not.

- **The bulk load is mostly invisible to this profile.** `LoadCanonical` holds
  `2,9 s` of CPU over ten passes, `0,29 s` a pass, against `0,878 + 0,078 s` of
  wall time per pass for the load. The rest is LadybugDB's own threads, shown
  as `<unknown>`. A Go profile cannot say which part of the load is slow; that
  needs a native profile of `liblbug`.
- **`runtime.madvise` is a quarter of the samples and carries no user frame**
  (every stack ends in `runtime.systemstack`), so the profile cannot attribute
  it. Whether it matters is a separate question, answered next.
- Under `rebuild.Run`, `5,02 s` of the samples are flat in only three places:
  `runtime.cgocall` `3,74 s` (a Go thread blocked inside native code),
  `syscall.rawsyscalln` `0,74 s`, and everything else together `0,54 s`. This
  repository's own Go code under `rebuild.Run` is therefore about a tenth of
  that; its largest items are `BuildSnapshot` (`1,06 s` cumulative over ten
  passes) and `ObserveSources` (`0,68 s`), and no function in it is hot.

## Garbage collector settings do not move publication

A side experiment, with the harness binary built from the same source and no
profile, four edits per run, same corpus. These runs came from a dirty working
tree and are **not** in `results.json`; the numbers are a record, not an artefact.

|setting|median pass|analysis|publication|
|---|---|---|---|
|default (two runs)|`3,243`, `3,237`|`1,315`, `1,319`|`1,927`, `1,921`|
|`GOGC=400`|`3,157`|`1,221`|`1,932`|
|`GOGC=off`|`3,274`|`1,293`|`1,968`|

Publication is flat within `0,05 s`; `GOGC=400` saves `0,09 s` in analysis only.
So the `madvise` samples are CPU burned off the critical path, and tuning the Go
runtime is not a way to shorten the publication half.

## A measurement that was wrong, and what the harness does about it

My first comparison of toolchains reported `2,0 s` for a binary built with
`go1.26.11` against `3,2 s` for `go1.27.4`, a `1,62x` gap in both halves,
reproduced in two alternating rounds. **It is an artefact.** The `go1.26.11`
binary indexed `11` files and `63` symbols instead of `343` and `12.465`:
`index --full` succeeded on a graph that held almost nothing, and the pass over
that graph was shorter. The `go1.27.4` binary, run the same way, indexed the full
graph. I do not know why the same command line produced a hollow graph with the
other binary, and I make **no claim about the speed of either toolchain**.

The same hollow graph appears when the Go module cache is not found (`9`
packages and `63` symbols without `GOMODCACHE`, `97` and `15.597` with it), and
`index --full` reports `passed: true` both times. The harness now stops when the
warm-up indexed under a quarter of the Go files it is about to edit
(`graphLooksHollow`, with a boundary test), and that guard fired on the
`go1.26.11` binary. Every run quoted above has `343` files.

## Reading it against the phase 6 target

Phase 6 proposed `8 s` for a pass after an edit. This corpus already takes
`3,247 s`, but it has `12.465` symbols against `178.851`, so the figure says
nothing about the large corpus. The profile does say where a reduction would
have to come from:

- publication is `59,2 %` here and was `82,7 %` there, so it is the half to
  shorten on any corpus large enough to matter;
- within it, the bulk load is the largest named stage, and it runs in native
  code, so 6b starts with a native profile and not with Go changes;
- `0,351 s` of publication is unaccounted for and worth a stage name before
  anything is optimised;
- the snapshot build grows fastest with the corpus (`9,5 %` to `22,2 %`).

## A second corpus: five repositories (phase 6b)

The single-repository corpus is too small to say anything about where a larger
one spends its time, so the same harness ran over five repositories of the
maintainer, Go and TypeScript, one of them this repository's clone. They are
private and are not named here; the run is not reproducible from this
repository. Raw metrics: `results-workspace.json`.

|field|value|
|---|---|
|commit|`79ab9ca3baddfb0c3d28c335288d9e5b304adef7`, clean tree|
|size|`1.070` files, `35.297` symbols, `149.570` edges (`2,8x` the symbols of the first corpus, `0,20x` those of the 53-repository one)|
|fact cache|warm-up `34` units missed; each edit pass `30` hits and `4` misses|
|edits|`10`, appended to a private copy of the Go repository|

Median of ten passes: **`6,913 s`** (`6,540`–`8,539`). One pass, `8,539 s`, ran
while the native sampler of the next section was attached; without it the
median is `6,859 s`.

|half|median seconds|share|
|---|---|---|
|analysis|`3,249`|`47,0 %`|
|publication|`3,563`|`51,5 %`|

|stage|median seconds|share of the pass|
|---|---|---|
|`staging`|`0,164`|`2,4 %`|
|`bulk load`|`1,305`|`18,9 %`|
|`integrity`|`0,532`|`7,7 %`|
|`snapshot`|`0,631`|`9,1 %`|
|`golden probes`|`0,026`|`0,4 %`|
|in `publish`, in no listed stage|`0,843`|`12,2 %`|

The first corpus' ordering holds (`bulk load` is the largest named stage), and
two things changed with size: analysis is now as large as publication, and the
unlabelled part of `publish` is larger than `integrity`.

### What the walks cost

The Go profile, ten edit passes, `70,5 s` of wall time. Under `RunFull` and
outside `rebuild.Run`:

|where|CPU seconds over ten passes|per pass|
|---|---|---|
|`os.ReadDir`, inside `filepath.WalkDir`|`10,05`|`1,0`|
|`freshness.Capture`|`6,14`|`0,6`|
|`sourceobservation.TreeDigest`|`3,05`|`0,3`|
|`workspace.DiscoverGo`|`3,12`|`0,3`|
|`workspace.DiscoverTypeScript`|`1,72`|`0,2`|
|`goworkspace.BuildPlan`|`1,58`|`0,2`|
|all of `RunFull` outside publication|`15,35`|`1,5`|

The rows overlap (they are cumulative and the walks nest), so they do not add.
What the profile does say is that about `1,5 s` of CPU per pass, `22 %` of it,
is directories being read, and that it is Go code in this repository. If those
walks are sequential, which I did not check, that is about a fifth of the pass.

A likely cause, read from the code and not instrumented: `freshness.Capture` and
`TreeDigest` skip only `.git` and `node_modules`. One of the five repositories
tracks `830` files and holds about `100.000` untracked or ignored entries
(`47.000` in a Rust `target/` and `49.000` in a test-output directory), which by
that code both walks enter every pass. I did not count the entries each walk
visits.
`TreeDigest` documents that it over-approximates on purpose: skipping a source a
provider reads could publish a graph that cannot be reproduced. So skipping a
directory is only safe where **every** provider ignores it, and honouring
`.gitignore` is not: generated sources are ignored and are read. That is a
contract question, not a tuning one.

### What the native profile shows

macOS `sample` at `1 ms` for `8 s` during one edit pass. Of the `618` top-of-stack
samples inside `liblbug`, the largest single function is
`BaseCSVReader::parseCSV<SerialParsingDriver>` (`134`, `22 %`). The load forces
`PARALLEL=false` on every table (`canonicalCopyOptions`) because the engine's
parallel reader rejects a quoted newline, and only some canonical text carries
one (`Evidence.text`, `Symbol.signature`).

An experiment, **not committed**: `PARALLEL=true` for each table in which no field
holds `\r` or `\n`, serial for the rest, same harness, four edits, alternating
with the unchanged build:

|build|bulk load median|pass median|symbols, edges|
|---|---|---|---|
|unchanged (two runs)|`1,300`, `1,306`; `1,317`|`7,033`, `7,020`; `7,051`|`35.297`, `149.570`|
|parallel where there is no newline (two runs)|`1,146`, `1,155`|`6,940`, `7,005`|`35.297`, `149.570`|

It saves about `0,16 s` of the bulk load, `12 %` of the stage and `2,4 %` of the
pass, with the same counts and with the integrity stage passing. Not checked:
that the logical content stays identical to a clean rebuild when row order
changes, which `AGENTS.md` requires before a change to the load is kept.

### What it means for the `8 s` target

On this corpus a pass already takes `6,9 s`. The target is not the question;
the corpus is `0,20x` the size of the one the number came from, and the `53`
repository run is the only one that can say. What the two corpora agree on is
the order of the costs: the bulk load, then the snapshot, with publication
the larger half. What this one adds is that analysis is no longer the small half:
it holds the directory walks, which were never visible at `12.465` symbols.

### Six repositories, after the fix (phase 6b, repeated)

The repository above joins the corpus: `1.290` files, `53.711` symbols, `219.891`
edges (`1,52x` the five-repository corpus). Raw metrics:
`results-workspace-6repos.json`; the interleaved runs below are in
`interleaved/`. Commit `9ae5efdf8dea36902e24969ed34c90b6942acd68`, clean tree.

**The absolute seconds of the previous section do not reproduce.** The same five
repositories, same configuration, took `6,913 s` there and `12,3`–`12,5 s` here,
on code that differs only by #52. The grep arm moved from `0,043` to `0,062 s`
per question and the load average was `4`–`7` on `10` CPUs, from a `cargo
clippy` of the maintainer's running in the background. Only comparisons made
inside one window mean anything, so these were interleaved: four
configurations, run in turn, twice, five edits each.

|configuration|symbols|pass, two runs|analysis|publication|
|---|---|---|---|---|
|five repositories|`35.297`|`12,28`, `12,53`|`6,15`, `6,30`|`6,12`, `6,33`|
|five, `target/` of one excluded|`35.297`|`11,37`, `11,43`|`5,29`, `5,26`|`6,08`, `6,17`|
|six repositories|`53.711`|`14,87`, `14,58`|`6,50`, `6,52`|`8,12`, `8,03`|
|six, `target/` of one excluded|`53.711`|`13,53`, `13,70`|`5,56`, `5,78`|`8,01`, `8,07`|

- **Excluding one `target/` of about `47.000` entries saves `0,9`–`1,1 s` of the
  pass, all of it in analysis**, and publication does not move. That is the
  first direct measurement of what the walks cost, for one directory: about `8 %`
  of the pass under this load. The exclusion is a repository setting the product
  already has; the full ten-edit runs below use it.
- **The sixth repository adds `2,3 s` (`+19 %`) for `+52 %` symbols**, and
  `2,0 s` of that is publication (`+33 %`); analysis hardly moves, because the
  fact cache serves `35` of `39` units.

Full run, ten edits, `target/` excluded (`results-workspace-6repos.json`):

|measure|six repositories|five repositories (same window)|
|---|---|---|
|pass, median (range)|`14,122 s` (`12,945`–`17,937`)|`11,419 s` (`11,342`–`11,533`)|
|analysis|`5,864 s`, `41,5 %`|`5,291 s`, `46,3 %`|
|publication|`8,224 s`, `58,2 %`|`6,149 s`, `53,9 %`|
|`bulk load`|`2,641 s`, `18,7 %`|`2,052 s`, `18,0 %`|
|`snapshot`|`1,790 s`, `12,7 %`|`1,160 s`, `10,2 %`|
|`integrity`|`1,592 s`, `11,3 %`|`1,020 s`, `8,9 %`|
|`staging`|`0,427 s`|`0,288 s`|
|in `publish`, in no listed stage|`1,541 s`, `10,9 %`|`1,459 s`, `12,8 %`|

`integrity` (`+56 %`) and `snapshot` (`+54 %`) grow with the symbols; `bulk load`
grows less (`+29 %`); **the unlabelled part of `publish` does not grow at all**
(`1,46` to `1,54 s` for `+52 %` symbols). A cost that does not follow the size of
the graph is a fixed cost of every pass, which makes it the cheapest thing to
find: it needs a name and a timer, not an optimisation. It was `0,843 s` in the
lighter window of the previous section, so it also moves with load.

One ten-edit run is not a controlled comparison with another taken minutes
apart; the range of the six-repository run (`12,9`–`17,9 s`) shows the machine
moving under it.

### The unlabelled cost of `publish` has a name (phase 6b, step b)

The previous section ended on a cost inside `publish` that no stage owns and that
did not grow with the graph (`1,46` to `1,54 s`). The generation store and
`rebuild.Run` now time the steps of `publish` one after another
(`Report.PublishSteps`, #55); the harness records them per pass
(`publish_steps`). Commit `fce9afd25b81d716dc9c6977c6c24a0bed918536`, clean tree,
ten edits each, `target/` of one repository excluded as in the full runs above.
Raw metrics: `results-publish-steps-6repos.json` and `...-5repos.json`.

|step|six repositories|share of the pass|five repositories|share of the pass|
|---|---|---|---|---|
|`setup`|`0,000 s`|`0,0 %`|`0,000 s`|`0,0 %`|
|`build`|`4,913 s`|`35,6 %`|`3,742 s`|`32,1 %`|
|`sync candidate`|`0,017 s`|`0,1 %`|`0,016 s`|`0,1 %`|
|`validate`|`1,624 s`|`11,8 %`|`1,088 s`|`9,3 %`|
|**`verify sources`**|**`1,387 s`**|**`10,0 %`**|**`1,250 s`**|**`10,7 %`**|
|`sync validated`|`0,006 s`|`0,0 %`|`0,005 s`|`0,0 %`|
|`commit`|`0,017 s`|`0,1 %`|`0,016 s`|`0,1 %`|
|`prune`|`0,008 s`|`0,1 %`|`0,007 s`|`0,1 %`|
|steps added up / the `publish` stage|`7,972` / `7,992 s`||`6,126` / `6,143 s`||
|pass, median|`13,801 s`||`11,656 s`||

The steps add up to the stage within `0,02 s`, so nothing is left unnamed.

What was unlabelled, then, is **the verification of the sources**: `1,25`–`1,39 s`
per pass, `10`–`11 %` of it. It runs inside `validate`, immediately before
`CURRENT` can move, and re-observes the source tree to prove it did not change
during the pass. It is `ObserveSources` called a second time, the function that
produced the manifest at the start of the pass (`internal/indexing/full.go`), and a
pass already runs `freshness.Capture` twice and the discovery walks besides, so
the tree is read from the root at least four times before the discovery walks
are counted. It grows `11 %` for `+52 %`
symbols, which is what a walk of the tree (the same repositories, mostly the same
files) predicts and what a cost of the graph does not.

The rest of the old unlabelled cost is smaller: `0,20`–`0,22 s` is in `build`
outside its own stages (the published snapshot file and the source manifest are
written there), and every filesystem step together -- both `fsync` passes over the
candidate, the rename, the pointers, the pruning -- is under `0,06 s`. **The
`fsync` cost I expected on macOS is not there.**

This changes what 6b can attack, ordered by what was measured:

|candidate|per pass, six repositories|
|---|---|
|`bulk load`|`2,57 s`, native, serial CSV reader (above)|
|`snapshot` build|`1,72 s`|
|`integrity`|`1,58 s`|
|**`verify sources`**|**`1,39 s`**|
|the walks inside analysis|about `1 s` (above)|

`verify sources` and the walks of analysis are the same kind of work: both are
directory walks that read every analysed file to hash it, so one design decision
covers both -- whether the verification may use something cheaper than a second
full read of the tree (a stat signature to find the files that changed, hashing
only those). That weakens a fail-closed check and is a contract question, so it
needs an ADR, not a patch.

The load at the start of this run was `16` (average over one minute, `10` CPUs); the pass
medians are `13,8` and `11,7 s`, so the shares above are the result and the
seconds are not comparable with another window.

### Problems found while building this corpus

- **One repository could not be published** (fixed in #52, which is why it is in
  the corpus of the section above). A pnpm workspace of five packages analysed fully
  and then failed at publish with `copy CONTAINS_FILE: ... more than one
  neighbour ... bwd direction`. Its named root package took a `tsconfig.base.json`
  without `include` as its project, which claims every file below it, so 220 of
  its files sat under two packages.
- **A pass can fail because a build is running in a registered repository.** The
  first six-repository run stopped at edit 9 with `verify source inventory:
  inventory <repository>: walk ".../target/debug/deps/rustc...": open ...: no
  such file or directory`. A `cargo check` of the maintainer's was writing in that
  repository's `target/`, a temporary directory existed when the walk listed it
  and not when it opened it, and `freshness.Capture` returns the error of any
  entry it fails to open. It also shows that the walk enters `target/`. Not fixed.
- **The TypeScript worker cannot be built here without a flag.** `pnpm install`
  stops with a corepack version mismatch unless `--pm-on-fail=ignore` is passed.
- **Without a built worker a TypeScript repository fails the whole index** (P1 in
  `benchmarks/agent-e2e/self-repo/FINDINGS.md`); here the worker was built in the
  clone and put on the `PATH` with the launcher `build-bundle.sh` writes.
- Rust and Java repositories of the maintainer were left out: no
  `rust-analyzer` on the `PATH`, and no dependencies installed for the
  TypeScript repositories that lacked them, which would have meant writing into
  them.

## Reproduce

Build the harness with the native tag and run the built binary, as in
[`edit-frequency`](../edit-frequency/report.md), with the module cache exported
because the run isolates `HOME`:

```bash
HOME=/tmp/p6/home /path/to/kivgraph init --languages go --repository kivgraph=/tmp/p6/corpus
HOME=/tmp/p6/home GOMODCACHE="$(go env GOMODCACHE)" /tmp/edit-frequency \
  -config /tmp/p6/home/.config/kivgraph/config.yaml -edits 10 -grep-repeats 5 \
  -root /tmp/p6/root -fact-cache /tmp/p6/factcache -scratch /tmp/p6/scratch \
  -output benchmarks/publication-profile/results.json -cpuprofile /tmp/p6/cpu.pprof
```

`/tmp/p6/corpus` is a `git clone --no-local` of the repository; macOS paths
under `/tmp` must be spelled `/private/tmp`.

## Limitations

- **One repository, one language, `12.465` symbols.** Not the corpus behind the
  `17,150 s` figure, and not a substitute for it.
- **Different platform from the earlier run**, so shares are compared and
  seconds are not.
- **`go1.27.4`, not the `go1.26.11` that CI and releases use.** No speed claim
  about either toolchain survives (see above).
- **The CPU profile cannot see inside LadybugDB** and cannot attribute the
  `madvise` samples.
- **The unaccounted `0,351 s` and the four-unit cache miss were not
  investigated.**
- **Wall clock on a shared machine is not an SLO.** The ten passes span
  `3,210`–`3,289 s`.
