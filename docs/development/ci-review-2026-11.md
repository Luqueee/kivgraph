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
| `darwin-bundle` inside `macos` | #37, open |

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
