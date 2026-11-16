# What proving that the sources did not change costs, and what a stat signature would miss

> **Result.** On six repositories one observation of the
> source tree costs `786 ms` warm: `613 ms` for the digest and `173 ms` for the
> git state. A stat signature of the same tree verifies in `25 ms`, which leaves
> `198 ms` and saves `588 ms` (`75 %`) of the observation. Over a change matrix of
> `14` scenarios on a local APFS volume the signature reported nothing for a
> changed tree **zero** times; the same signature without the inode change time
> misses the one scenario that restores a file's modification time. This
> supports [ADR 0120](../../docs/adr/0120-verify-the-sources-with-a-stat-signature.md)
> as a proposal. It is not a measurement of a pass: nothing here is wired into one.

[`benchmarks/publication-profile`](../publication-profile/report.md) found that
the verification of the sources inside `validate` costs `1,25`–`1,39 s` of a
pass, `10`–`11 %` of it, and that most of what directory walks spend is opening
and reading directories rather than hashing. This harness splits that cost and
prices a cheaper proof. Raw metrics: `results.json`.

## Environment and provenance

|field|value|
|---|---|
|date|2026-11-16|
|commit|`af6dec2aa1588e7aeb66b0ed7627195e0b8652cd`, clean tree|
|machine|macOS, `arm64`, Apple M5, `10` CPUs; load average `3,3` when it ran|
|toolchain|`go1.27.4`|
|corpus|six repositories of the maintainer, Go and TypeScript, private and not named|
|size|`7.017` directories read, `113.555` entries listed, `4.185` analysed files (`150,7 MB`)|
|samples|median of `7`, after one warm-up of each operation|

The corpus is the one `publication-profile` used for its six-repository runs. The
pass that measured `1,25`–`1,39 s` ran with a load average of `16`; this run
did not, which is why the sum below (`786 ms`) is smaller than that figure.

## Where one observation goes

An observation is, per repository, `TreeDigest` (walk the tree, hash every
analysed file) and `RefreshRepositoryState` (ask git for commit, branch and
dirty state). The traversal-only column walks the same tree and opens no file.

|repository|dirs read|entries|analysed files|digest|traversal only|git state|
|---|---|---|---|---|---|---|
|1|`358`|`2.524`|`1.168`|`46,1 ms`|`10,4 ms`|`36,3 ms`|
|2|`6.189`|`105.029`|`497`|`368,1 ms`|`347,5 ms`|`33,3 ms`|
|3|`90`|`577`|`348`|`17,6 ms`|`2,9 ms`|`27,0 ms`|
|4|`319`|`5.031`|`1.874`|`171,8 ms`|`15,6 ms`|`27,4 ms`|
|5|`52`|`358`|`281`|`9,3 ms`|`1,5 ms`|`25,4 ms`|
|6|`9`|`36`|`17`|`0,6 ms`|`0,2 ms`|`23,3 ms`|
|**all**|`7.017`|`113.555`|`4.185`|**`613,5 ms`**|**`378,2 ms`**|**`172,8 ms`**|

- **The traversal is `62 %` of the digest** (`378` of `613 ms`) and hashing is
  `38 %` (`235 ms`). Which one dominates depends on the repository: repository 2
  holds `105.029` entries for `497` analysed files (a build-output directory and a
  test-output directory the walk enters) and spends `94 %` of its digest walking;
  repository 4 holds `1.874` analysed files of `128 MB` and spends `91 %` hashing.
- **The git state is `173 ms`, `22 %` of an observation, and almost all of it is
  process start**: `23`–`36 ms` for each repository, including the one with `17`
  analysed files. It is a separate cost and this report does not touch it.

## What the signature costs

The signature records, for the traversal `TreeDigest` already makes, the stat of
every directory entered and of every analysed file hashed: size, modification
time, inode change time, inode and mode. Verifying it stats each of them again
and reads no directory and opens no file.

|repository|stats|capture|verify|
|---|---|---|---|
|1|`1.526`|`13,2 ms`|`2,1 ms`|
|2|`6.686`|`360,1 ms`|`17,2 ms`|
|3|`438`|`3,9 ms`|`0,8 ms`|
|4|`2.193`|`21,6 ms`|`4,6 ms`|
|5|`333`|`2,3 ms`|`0,6 ms`|
|6|`26`|`0,3 ms`|`0,0 ms`|
|**all**|`11.202`|`401,4 ms`|**`25,3 ms`**|

Capture costs what a traversal costs, because it is one; in a pass the first
observation already walks and already stats the analysed files, so recording the
stamps adds a map insert per entry, not a second walk. Verification is `25 ms`
for `11.202` stats against `613 ms` for the digest.

|a pass's proof today|ms|with a signature|ms|
|---|---|---|---|
|digest|`613,5`|signature verify|`25,3`|
|git state|`172,8`|git state|`172,8`|
|**total**|**`786,3`**||**`198,1`**|

Saving: `588 ms`, `75 %` of the observation. On the loaded pass of
`publication-profile` the observation cost `1,25`–`1,39 s`, so the same ratio
would be about `1 s` there, `7 %` of a `13,8 s` pass. That is an extrapolation
from a ratio, not a measurement of a pass.

## What the signature misses

Fourteen modifications applied one at a time to a fresh synthetic tree whose
files had been aged an hour, so no entry was racy. For each, the digest the pass
uses today and the signature each say whether the tree changed.

|scenario|digest|signature|outcome|
|---|---|---|---|
|edit an analysed file, new size|changed|changed|agree|
|edit an analysed file, same size, new mtime|changed|changed|agree|
|same-size edit, then the file's own mtime restored|changed|changed|agree|
|replace an analysed file by rename over it|changed|changed|agree|
|add an analysed file|changed|changed|agree|
|delete an analysed file|changed|changed|agree|
|rename an analysed file|changed|changed|agree|
|add a file in a new analysed directory|changed|changed|agree|
|edit a file that is not analysed, in place|same|same|agree|
|add a file that is not analysed|same|changed|false positive|
|build output churn in an ignored directory|same|changed|false positive|
|touch an analysed file, content unchanged|same|changed|false positive|
|chmod an analysed file|same|changed|false positive|
|nothing at all|same|same|agree|
|**ablation:** same-size edit, mtime restored, **ctime not compared**|changed|same|**missed**|

- **No scenario was missed.** The harness exits non-zero if one is.
- **The four false positives are the price.** Each makes the signature say
  "changed" for a tree whose digest is unchanged, so a verification would fall
  back to a full observation and cost what it costs today. Directory stamps are
  what make an added file or a rename visible without reading the directory, and
  they are also what a build writing temporary files in an ignored directory
  moves.
- **The inode change time is a requirement.** Without it the scenario that
  restores a file's own modification time after a same-size edit is missed. A
  program can set a modification time; it cannot set a change time.

## Limitations

- **The matrix ran on a local APFS volume**, where timestamps have nanosecond
  granularity and the attribute cache is the kernel's own. It says nothing about
  a filesystem with coarse timestamps (FAT, `2 s`) or stale client attribute
  caches (NFS, SMB). There the signature has no guarantee, and the ADR has to
  make it opt-in or fall back; this run cannot show either.
- **A write in the capture tick is argued, not measured.** A file modified within
  the same timestamp tick as the capture can be modified again in that tick
  without its stat moving. The prototype re-reads any entry modified within `2 s`
  of the capture (the rule git applies to "racily clean" entries); a hardware
  clock tick cannot be reproduced here. In this corpus `0` entries were racy at
  capture.
- **Timings are warm medians.** The pass that verifies has just analysed the
  tree, so its directories are cached; a cold tree costs more for both methods.
- **The signature is a prototype in this harness.** It is not part of a pass, and
  nothing here changes what a pass publishes.
- **One corpus, one machine.** The per-repository split of traversal against
  hashing depends on the repository, as the two extremes above show.

## Reproduce

```bash
go run ./benchmarks/source-verification \
  -config <kivgraph config naming the repositories> -repeats 7 \
  -output benchmarks/source-verification/results.json
```

Without `-config` it runs the change matrix alone. The corpus is private, so the
timings are not reproducible from this repository; the matrix is.
