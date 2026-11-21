# Findings while running the self-repo sweep

Working log, appended as things turn up. Each entry says whether it was
observed or only suspected, and whether it is fixed. Nothing here is a
conclusion about the arms; results go in the report once the sweep ends.

Setup: corpus is a clone of this repository at `d79c483`; 12 `fix(...)` commits
from 2026-10 onward, Go only; arms `cold` and `kivgraph`; model
`claude-sonnet-5`; `--budget-usd 1.5` per run; 2 trials.

## Product

### P1. `index --full` fails the whole repository when the TypeScript worker cannot start
- **Observed.** `kivgraph index --full` against the benchmark copy, with
  `--languages go,typescript,rust`, exits 1:
  `run TypeScript facts for "kivgraph" package "@kivgraph/ts-worker": exit
  status 1: ! Corepack is about to download https://reg...` (message truncated
  in the harness output).
- Cause: the copy holds `ts-worker/package.json` with no `node_modules`, and
  Corepack wants to fetch pnpm. No network prompt can be answered headless.
- Open question: a Go-only graph is still useful. Today a TypeScript package
  that cannot load aborts the entire index instead of surfacing one failing
  package.
- **By design, checked.** `indexer.Full` documents it: "A loader diagnostic
  aborts the pass instead of publishing a graph that is known to be
  incomplete", and the all-or-nothing publication of the generation store
  depends on it. Not a gap, so not changed. Worked around in the harness with
  `--languages go`.

### P2. Native build prints a linker warning on every build
- **Observed.** `go build -tags ladybug` ends with
  `ld: warning: search path '.../go-ladybug@v0.13.1/lib/dynamic/darwin' not found`.
  The binary builds and runs. `scripts/fetch-ladybug.sh` also prints
  `fetching ...liblbug-osx-universal.tar.gz`, so the library was not cached
  on this machine at that point.
- Probably harmless (AGENTS.md says the binding declares its own rpath), but
  it is exactly the kind of noise AGENTS.md warns hides the one warning that
  matters. Not investigated.

## Harness

### H1. The benchmark cannot be reproduced from the repository
- **Observed.** `tasks.json` and `tasks-locate.json` name a private,
  anonymised workspace (`corpus: "/path/to/workspace"`). `--corpus` defaults to
  that placeholder. No binary of graft, code-review-graph, graphify or
  codebase-memory is installed here. Without the original corpus the earlier
  sweep cannot be repeated, only the self-repo one exists.
- Not fixed; documented.

### H2. Language list was hardcoded
- **Observed and fixed.** `register()` always passed `go,typescript,rust`.
  Added `--languages` (default unchanged).

### H3. Every surface was indexed regardless of `--arms`
- **Observed and fixed.** `reindex()` ran graft, codebase-memory,
  code-review-graph and graphify builds even for `--arms cold,kivgraph`; with
  those tools absent the run would fail before any agent started. It now
  returns after kivgraph when none of the four is selected.

### H4. A corpus that ships agent instructions contaminates both arms
- **Observed risk, fixed.** This repository has `AGENTS.md`/`CLAUDE.md` files
  that name the kivgraph tools. Claude Code loads nested ones on demand, so
  `cold` would read instructions for tools it does not have. `prepare()` now
  deletes `AGENTS.md`, `CLAUDE.md`, `GEMINI.md` and `.mcp.json` from the
  prepared copy.
- **Not verified** that the transcripts of `cold` runs never mention the tools.
  Check the `raw/*.jsonl` of the `cold` arm for `find_references` etc.

### H5. Smoke evidence was thrown away
- **Mine.** I deleted `self-repo/raw` before the full run, so the transcripts of
  the two smoke runs (`4eea720`, both arms) no longer exist. Their numbers
  (cold $1.14 / 21 calls / 335 s; kivgraph $0.84 / 17 calls / 195 s, both
  P=R=0.50) exist only in this session's terminal output, not on disk.
  They are not part of the results.

### H6. Progress is invisible until a run ends
- **Observed.** `results.json` is written after each run; an in-flight run
  only shows as a growing `raw/*.jsonl`. Trials are the outer loop, so trial 2
  of a task starts before the next task: a crash leaves uneven coverage.

## Tasks

### T1. Path stripping in the generator mangles prompts
- **Observed.** My first regex left fragments like
  ``A bare word that two symbols declare was refused whatever the target:
  `grep -n version , a grep of a log``. The second pass replaces them with `…`
  but the text is still broken in places (`…, a grep of a log, a glob over
  YAML`). Task `511901e` is the worst; prompts in `tasks.json` were not
  reviewed one by one.
- Fix pending: read all 12 prompts and repair or drop the tasks.

### T2. Commit messages describe the solution, not only the problem
- **Observed.** `511901e`: "The caller was told to call find_references ..."
  and the later paragraphs say what the gate now does. Bodies of fix commits
  often state the mechanism, so the prompt can hand the approach to both arms.
  Weakens the "find the files" part of the measurement. Not fixed.

### T3. Ground truth includes test files
- **Observed.** Every selected task has at least one `_test.go` in
  `truth_files` (that was the filter). The prompt says "including its tests if
  the change warrants one" but does not require one. Both finished tasks score
  P=0.50, R=0.50 for both arms: each wrote 2 files and hit 1.
  `[INFERENCE]` it is the code file or the test file that is missing; the
  transcripts were not opened.

### T4. The corpus is the project the arms were built for
- **Limit, not a bug.** One repository, Go only, 760 files, and it is
  Kivgraph itself. The agent may know the project, and the graph tools were
  designed against this code. Results do not transfer to other repos.

## Statistical

### S1. Sample size
- 12 tasks x 2 arms x 2 trials = 48 runs. Per-task cost differences seen so far
  (17% and 27% in favour of `kivgraph`) come from one trial per arm, so
  run-to-run noise is unmeasured. The second trial of each task exists to
  estimate it. Do not read them as an effect.

## Added after the sweep finished

### H4 closed
- **Verified.** In the 24 `cold` transcripts the init event lists no `mcp__`
  tool, no run called one, and no run read `AGENTS.md`/`CLAUDE.md` (one `Grep`
  pattern contains the word "gate", unrelated). The matches for `find_references`
  in those files come from the agent reading Kivgraph's own source.

### S2. One run hit the budget cap
- **Observed.** `511901e`, `cold`, trial 1: 44 calls, `$1.51`, cut by the cap.
  Its cost is a floor, not a full cost, so the cost gap on that task is
  understated for `cold`.

### S3. Accuracy is noisy across trials
- **Observed.** `ce38264` `cold`: P=0.50 then 1.00. `02dd835`: recall 0.60 then
  0.20 for `cold`, 0.20 then 0.60 for `kivgraph`. Two trials per arm cannot
  support an accuracy claim in either direction.

### S4. The graph tools are barely used
- **Observed.** 52 graph calls in 24 runs (about 2 per run), none of them
  `get_blast_radius` or `trace_dependencies`. The cost saving comes from this
  small use; the agent mostly still reads and greps.

### T3 and T1: not fixable from this repository
- **T1.** The 12 prompts are the input of the published sweep: `results.json`
  was measured with exactly these texts, and AGENTS.md forbids editing a
  benchmark's input artifacts. Repairing the broken ones would describe a
  different experiment, so it needs a new task set and a rerun (about `$37`),
  not an edit. Left as read: `511901e` is the one with visible fragments.
- **T3.** Needs the transcripts, and `self-repo/raw/` is not committed (it is
  ignored now); this machine no longer has them. A rerun regenerates them.

## Phase 5 analysis of the 24 `kivgraph` transcripts

### P3. With `include_tests: true`, every Go answer is `LOWER_BOUND` and carries a constant 5.4 KB block
- **Observed.** All 28 responses that carry a verdict (21 `find_symbol`, 7
  `find_references`) say `LOWER_BOUND`. `completeness.invisible_scopes` is the
  same 20 rows on every call (`more_invisible_scopes: 57`, so 77 in the
  repository) and is 5,455 bytes of a 5.6 KB response. Of the 20 listed, 19 are
  `<package>.test` with reason `DECLARATION_OUTSIDE_REPOSITORY` ("declared in a
  Go build cache entry"), the other one is a test fixture
  (`testdata/.../twin-a`, `AMBIGUOUS_MODULE_PROVIDER`).
- Total across the 24 runs: 152,780 of 215,792 bytes of graph responses (71 %).
- `[INFERENCE]` the `.test` rows are the synthetic test main that `go test`
  generates, not code anyone can read, so listing them as unreadable scopes says
  nothing about the user's code. Not confirmed in the loader; the benchmark
  forces `include_tests: true`, so with the default (`false`) this may not occur.
  Not checked.
- Effect: the verdict cannot discriminate (it is always `LOWER_BOUND`), and an
  empty `find_symbol` answer gets guidance pointing at these rows.

### S5. What the graph changed in the agent's behaviour
- Tool-result bytes per run, `cold` against `kivgraph`: `Read` 1,637 KB against
  1,377 KB; `Grep` 328 KB against 182 KB; graph tools 236 KB. First call of the
  `kivgraph` arm was always a graph call (24 of 24).
- Reads of files that are later edited are 852 KB of the 1,377 KB; those are
  required (Edit needs a prior Read). Reads of files never edited are 525 KB
  against 490 KB for `cold`: the graph did not reduce exploration reads.
- Whole-file `Read`s are 888 KB of the 1,377 KB; 8 of them (over 20 KB) are
  273 KB. The graph rows carry a line range, and `get_source` was called once.
- 10 of 52 graph calls returned nothing (`returned: 0`, 56 KB), mostly
  `find_symbol` on a name that is a string (a tool name) rather than a Go
  symbol; the agent then ran `Grep` for the same name.

### S6. Size of the two levers, against the noise
- Both are about 2 to 3 % of the 1.47 M tokens a `kivgraph` run uses (cache
  reads are 94 % of them): 6.4 KB per run of repeated scope rows, and 11 KB per
  run of whole-file reads over 20 KB, each re-read on later turns
  `[INFERENCE: about 15 turns]`.
- Run-to-run cost noise is 21 to 27 %. A change worth 2 to 3 % cannot be shown
  with this benchmark; only the byte counts are measurable.

### P3 resolved in `internal/goloader` (phase 5)
- **Confirmed.** `packages.Config.Tests` adds, for each tested package, a
  generated main named `<path>.test` whose single file is in the build cache.
  `ExtractDefinitions` read its declarations, the facts marked them
  `DECLARATION_OUTSIDE_REPOSITORY`, and each one became an invisible scope.
- **Fix.** `Load` drops that package from the roots when the package it tests
  is also loaded. A real main whose own path ends in `.test` is kept
  (`TestLoadWithTestsKeepsAPackageWhoseImportPathEndsInTest`). Test files stay
  indexed. The fact cache is keyed by the executable, so no stale entry is served.
- **Smoke, two-package module with tests, `include_tests: true`, real binaries.**
  Before: `unresolved: 12`, `LOWER_BOUND`, two `.test` scopes, 710 bytes for
  `find_symbol`. After: `unresolved: 0`, no completeness block, 173 bytes.
- **On this corpus it does not shrink the response.** Same repository, same
  query: invisible scopes 77 to 13, `unresolved` 3,448 to 3,064, but
  `find_symbol` goes from 5,647 to 5,716 bytes and the verdict stays
  `LOWER_BOUND`. The 13 left are real: 7 `PACKAGE_NOT_BUILDABLE` and the
  ambiguous and conflicting modules of this repository's own `testdata`
  fixtures. My estimate in S6 (about 4 KB saved per call) was wrong for this
  corpus; the benchmark was not rerun because it would measure no difference.
- The fix helps repositories with `include_tests: true` and no fixtures like
  these; here the fixtures keep the verdict at `LOWER_BOUND` anyway.

### H7. The harness does not pass `GOMODCACHE` (fixed)
- **Observed.** `kivgraph index --full` under `HOME=<isolated>` with no
  `GOMODCACHE` indexes 97 packages and 15,597 symbols only if the module cache
  is found; without it the same command gave 9 packages and 63 symbols
  (`MODULE_NOT_LOADED`), with `passed: true`. The sweep's graph was complete
  (3,448 unresolved, 15,597 symbols), so its results stand. A rerun from a shell
  that lacks the variable would silently index a hollow graph.
- **Fixed.** The harness resolves the module cache of its own environment
  before swapping `HOME`, passes it as `GOMODCACHE` to `kivgraph init` and
  `index --full`, and refuses to start when that directory does not exist.
