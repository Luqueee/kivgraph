# CI review, November 2026

A review of `.github/workflows/ci.yml`: why a run took as long as it did, what
was changed, and what the change measured. Written on 2026-11-15 and closed the
same day. It describes that day; later changes to the workflow are not tracked
here.

## Conclusion

The workflow was slow because of what it waited for, not because of what it
ran. The longest job ran for 6.8 minutes, while a green run took 10.4 minutes
at the median and up to 23.8. The difference was the queue for macOS runners.

After the changes a green run takes 5.0 minutes at the median, over six runs.
That sample is small and saw no burst of open pull requests, so the macOS
queue has not been put to the test since.

A second pass later the same day took one run on `main` to 4.2 minutes, and a
third took a pull request run to 3.2. Both are described at the end.

## Before

Sample: the 54 runs of `ci.yml` between 2026-11-14 23:16Z and 2026-11-15
14:04Z. Job and step times are the median of the last 20 green runs.

| metric | value |
|---|---|
| run duration, median / max | 10.4 / 23.8 min |
| longest job, no queue (`macos`) | 6.8 min |
| runner time per run | ~24 min |
| Linux queue, median / p90 / max | 3 / 4 / 165 s |
| macOS queue, median / p90 / max | 61 / 432 / 1000 s |

| job | runner | median (min) | max (min) |
|---|---|---|---|
| `macos` | `macos-15` | 6.8 | 8.7 |
| `verify` | `ubuntu-24.04` | 5.5 | 6.4 |
| `ladybug` | `ubuntu-latest` | 2.9 | 3.8 |
| `semantic-coverage` | `ubuntu-latest` | 2.6 | 3.3 |
| `darwin-bundle` | `macos-15` | 2.5 | 3.6 |
| `linux-bundle` | `ubuntu-latest` | 1.9 | 2.4 |
| `smoke`, after `linux-bundle` | `ubuntu-latest` | 1.0 | 1.2 |
| `windows-cross` | `ubuntu-latest` | 0.8 | 1.2 |

The steps that weighed most:

| job | step | median |
|---|---|---|
| `macos` | `make test-ladybug` | 164 s |
| `macos` | `go test ./...` | 132 s |
| `verify` | `make coverage` | 120 s |
| `semantic-coverage` | the gate | 104 s |
| `ladybug` | `make test-ladybug` | 96 s |
| `ladybug` | `make lint-ladybug` | 63 s |
| `macos` | `go vet ./...` | 40 s |

One package dominates every suite: `internal/indexer`, at 74 s on macOS
without the tag, 106 s with it, and 77 s instrumented on Linux. After it come
`javaloader` at 49 s, `csharploader` at 41 s and `rustloader` at 27 to 34 s.

## Findings

### 1. Duplicate runs through `workflow_dispatch`

10 of 44 commits had two complete runs, a `pull_request` one and a
`workflow_dispatch` one, started about a second apart on the same SHA. Nothing
in the repository started the second: the agent that opened the pull request
ran `gh workflow run` beside it. The concurrency group was
`ci-${{ github.ref }}`, and the two runs have different refs, `refs/heads/x`
and `refs/pull/N/merge`, so neither cancelled the other.

Each duplicate added two macOS jobs. The worst queue times measured, 570 to
1000 s, all belong to one burst of five simultaneous runs.

### 2. The `macos` job ran the Go suite twice

`go test ./...` and then `make test-ladybug`. The second covers the same 64
packages plus four tagged ones. What only the untagged pass reads are the
`!ladybug` fallback files, which are not platform specific and already run in
`verify`.

### 3. The Go cache was written by the wrong job

`setup-go` keys its cache on the runner image, the Go version and `go.sum`,
with no job in the key, and never rewrites an entry that exists.

- Five Linux jobs shared one entry, written by whichever finished first. By
  its creation time that was probably `windows-cross`, whose build cache is
  for another `GOOS`; this is an inference.
- Observed: `make lint-ladybug` downloaded and compiled staticcheck on every
  run, 63 s against 23 s for the same tool in `verify`.
- Observed: `go vet` took 40 s on macOS and 8 s on Linux.

### 4. The cache was over quota

14.55 GB in 128 entries, against a limit of 10 GB per repository. Entries were
repeated per branch, partly because of the duplicate runs of finding 1.

### 5. macOS downloads Go in every job

`Set up Go` takes about 25 s on macOS against 4 to 6 s on Linux, because the
`macos-15` image does not carry the pinned Go release. There is no cheap fix.

### 6. Consecutive pushes to `main` lost their verdict

Eight `push` runs were cancelled after six seconds. `cancel-in-progress` was
`false` on `main`, but a concurrency group keeps one pending run, and a newer
one replaces it. Four merges in a row left the middle two without a verdict.

`main` has no branch protection, so `CI result` is not required to merge.
This review did not change that.

### 7. Nothing was skipped by path

A change to `docs/` or `README.md` paid for the whole workflow, two macOS jobs
included.

### 8. The failures in the sample were not flakiness

| failure | times | cause |
|---|---|---|
| `govulncheck` | 5, same hour | a new toolchain advisory |
| installing Chromium | 1 | `ubuntu-latest` moved to 26.04 |
| Go tests on macOS | 1 | a real failure in the pull request |

`govulncheck` is a design risk all the same: a published advisory turns every
open pull request red for something none of them changed.

### 9. Minor

- coursier, scip-dotnet, pyright and Dart were installed at whatever upstream
  published last, in a repository that pins everything else by digest.
- The Dart, JDK, scip-java, .NET and scip-dotnet setup was copied into two
  jobs.
- Playwright's Chromium is not cached: 21 s at the median. Left as it is; a
  cache would save about 10 s and add one more entry per branch.
- `release.yml` runs all of `ci.yml` again on the tagged commit. That is
  deliberate.

## What was changed

| change | pull request |
|---|---|
| the agent no longer dispatches a second run | #24 |
| one Go pass in `macos` | #24 |
| a Go cache per job, saved only from `main` | #24 |
| documentation-only changes skip the code jobs | #25 |
| analyzer versions pinned; one setup action | #25 |
| one concurrency group per commit on `main` | #25 |
| `govulncheck` reports without blocking a PR | #25 |
| `darwin-bundle` inside `macos` | #37, closed unmerged |

The old `setup-go-*` cache entries, 7.4 GB, were deleted by hand.

A flaky test surfaced while this was under way, and its cause was in the
product rather than in the workflow. `TestFullIndexesARustRepository` failed
once in `ladybug` and passed on a rerun. The hosted image has no `rust-src`,
and rust-analyzer installs the component itself when it finds it missing, so
two test binaries starting together raced the install.

| change | pull request |
|---|---|
| `rust-src` installed before the Rust suites | #31 |
| `Run` no longer passes `discover` to the analyzer | #33 |
| no `rustc` means `none`; the cache sees `rust-src` | #36 |

ADR 0120 records that decision.

## After

Sample: the six complete green runs between 15:33Z and 18:58Z on 2026-11-15,
with #24, #25 and #31 on `main`.

| metric | before | after |
|---|---|---|
| run duration, median | 10.4 min | 5.0 min |
| run duration, max | 23.8 min | 5.6 min |
| macOS queue, median / max | 61 / 1000 s | 3.5 / 5 s |
| Linux queue, median / max | 3 / 165 s | 3 / 4 s |
| Actions cache | 14.55 GB | 8.39 GB |

| job | before (min) | after (min) |
|---|---|---|
| `verify` | 5.5 | 4.8 |
| `macos` | 6.8 | 4.1 |
| `ladybug` | 2.9 | 2.5 |
| `darwin-bundle` | 2.5 | 2.5 |
| `semantic-coverage` | 2.6 | 2.2 |
| `linux-bundle` | 1.9 | 1.6 |
| `smoke` | 1.0 | 1.0 |
| `windows-cross` | 0.8 | 0.9 |

The drop in run duration mixes two things this sample cannot separate: the
jobs are shorter, and there were no duplicate runs and no burst to queue
behind.

## One macOS job instead of two

#37 moves the bundle steps into the `macos` job. One run of it, without a Go
cache because only `main` saves one:

| | two jobs, median on `main` | one job, #37, cold |
|---|---|---|
| macOS jobs per run | 2 | 1 |
| longest macOS job | 4.1 min | 6.4 min |
| run duration, no queue | 5.0 min | 6.8 min |

Merging the jobs makes a run longer when there is no queue: `macos` becomes
the longest job, ahead of `verify`. What it buys is half the macOS slots,
which only shows with several pull requests open at once.

Decision, 2026-11-15: #37 stays open. It is merged if the macOS queue comes
back with a real burst of pull requests, and closed otherwise.

## Open at closing

- **#37.** Measure the macOS queue again after several simultaneous pull
  requests. Above a one-minute median, merge it.
- **The Go cache.** An entry is not rewritten until `go.mod` or `go.sum`
  changes, so first-party build output ages in it. Marked as a shortcut in
  `.github/actions/setup-go/action.yml`.
- **`internal/indexer`.** Still the package that sets the length of every
  suite, and the next thing to split to go below five minutes.
- **Branch protection.** `main` has none.

## Not verified

- That `windows-cross` was the author of the shared Linux cache entry.
- The warm duration of the merged macOS job; it cannot be measured before
  #37 is on `main`.
- `release.yml`, `download-metrics.yml` and `bazel-benchmark.yml` were read
  only as far as their triggers.

## Second pass

Done after this review was closed, the same day. Everything above describes
the workflow before it.

The goal was a run under five minutes. `verify` was the job that decided the
duration, at 4.8 minutes, with `macos` at 4.1.

### What one package cost

`internal/indexer` was the slowest package of every suite, and one test was
most of it. `TestFullIndexesTheStandardLibraryAsASyntheticProvider` indexes
the whole Rust standard library and took 84 s of the 138 s the package needed,
measured locally with `go test -json`. A test binary runs its tests one after
another, so that test set the length of the package.

#39 moves it, unchanged, to a test-only package of its own,
`internal/indexer/rustsysroot`, where it runs beside the rest.

| job | before | with #39 |
|---|---|---|
| `ladybug` | 169 s | 94 s |
| `macos` | 250 s | 201 s |
| `verify` | 299 s | 353 s, then 300 s |

`verify` did not move. There the suite runs instrumented for coverage and is
bound by CPU, not by its slowest package: the step took 136 s and 93 s in the
two samples, against 96 s before. Splitting the package was not enough to
meet the goal.

### What `verify` was

Three unrelated things on one runner, one after another.

| change | pull request |
|---|---|
| the worker, web and landing checks run in `packages` | #40 |
| the linters and the race tests run in `lint` | #41 |

`lint` needs none of the analyzers `verify` installs, and no test in the five
raced packages skips for a missing one.

### After the second pass

One run on `main` with #39, #40 and #41, and no queue:

| job | duration |
|---|---|
| `macos` | 229 s |
| `verify` | 152 s |
| `darwin-bundle` | 152 s |
| `semantic-coverage` | 151 s |
| `lint` | 143 s |
| `packages` | 117 s |
| `ladybug` | 103 s |
| `linux-bundle` | 101 s |

The run took 252 s, 4.2 minutes. `macos` is the job that decides it now, and
a queue for its runner adds to it directly: a pull request run the same
afternoon waited 66 s for one and took 5.2 minutes.

This is a single run. The suite step alone has ranged from 93 s to 136 s.

### #37 again

Rebased on the three changes and run once, still without a Go cache:

| | two jobs, on `main` | one job, #37, cold |
|---|---|---|
| longest macOS job | 229 s | 376 s |
| run duration, no queue | 252 s | 396 s |

With `macos` already the longest job, moving the bundle into it adds its whole
length to the run: about two and a half minutes. The earlier comparison, made
while `verify` was longer, understated that.

#37 was closed without merging after this measurement. The queue it would
have relieved showed up once that afternoon, for 66 s; the change cost more
than twice that on every run.

### Open after the second pass

- **The macOS queue.** Two macOS jobs per run remain. If the queue comes back
  at more than about two and a half minutes per run, #37 is the change to
  bring back.
- **`macos`.** Its suite step, `make test-ladybug`, is 144 s of it, with
  `internal/indexer/rustsysroot` at 106 s and `cmd/kivgraph` at 110 s as the
  two slowest packages.
- **The suite under coverage.** CPU-bound in `verify`; a larger runner or
  fewer instrumented packages are the only levers left there.

## Third pass

Also the same day. After the second pass `macos` decided how long a run took,
and its suite step, `make test-ladybug`, was most of it. The step was timed
test by test on the `macos-15` runner.

### What was in the step

**A stall in `cmd/kivgraph`, on macOS only.** The package took 4 to 7 s in
some runs and 49 to 110 s in others. A goroutine dump put it inside
`rustc --version`, called by the fact-cache fingerprint. On that image `rustc`
is rustup's proxy, and under the temporary `HOME` those tests set it has no
toolchain, so it downloads one before answering.

| `rustc --version`, empty `HOME` | time | left behind |
|---|---|---|
| before | 15 to 19 s | a 1.4 GB toolchain |
| with `RUSTUP_AUTO_INSTALL=0` | 1 s, exit 1 | nothing |

It was not only a test problem: a workspace whose `rust-toolchain.toml` names
a release nobody installed made the same probe download it.

**Eight analyses of one fixture.** Each `TestAnalyze*` in
`internal/rustloader` ran rust-analyzer over the same recorded workspace to
read the result, 5 to 7 s apiece.

**The standard library pass.** The test #39 gave a package of its own is
what decides the step, and how long it takes depends on the runner:

| | suite step | that one test |
|---|---|---|
| whole suite, five jobs | 80 to 191 s | 62 to 159 s |
| without it, two jobs | 57 and 79 s | -- |

The runner has three cores and the analyzer is CPU-bound.

### What was changed

| change | pull request |
|---|---|
| no question to the toolchain lets rustup install | #44 |
| `internal/rustloader` analyses its fixture once | #44 |
| on a pull request, `macos` skips the standard library pass | #45 |

#44 covers the sysroot probe, the three version lines of the fingerprint and
the two of `doctor`; ADR 0120 has the amendment. `internal/rustloader` went
from 68 s to between 12 and 31 s on the runner.

#45 is a trade. Both Linux jobs still run that test on every pull request,
and `macos` still runs it on every push to `main` and on a release. A
regression in it that only shows on darwin is found after the merge.

### After the third pass

One run each, no queue:

| | run | `macos` | its suite step |
|---|---|---|---|
| `main` after the second pass | 252 s | 229 s | 144 s |
| pull request, #45 | 194 s | 176 s | 97 s |
| `main` with #44 and #45 | 237 s | 215 s | 136 s |

The pull request row predates #44 on its branch. No pull request has run
with both yet.

On `main` the step still carries the standard library pass, so it moves with
the runner: 136 s here, 193 s on the pull request that carried #44.

`lint`, measured cold at 143 s when it was added, took 51 s on `main` once
its cache existed.

### Open after the third pass

- **The analyzer and `cargo metadata`.** They can still make rustup install
  a toolchain during a pass. ADR 0120 leaves that as its own decision.
- **The standard library pass on `main`.** Still 62 to 160 s of the `macos`
  job there, by design.
- **Pull requests.** `macos` at about three minutes and `verify` at about two
  and a half are close; the next saving is no longer in one place.
- **The macOS queue** and **the suite under coverage**, as after the second
  pass.
