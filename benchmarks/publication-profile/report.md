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
