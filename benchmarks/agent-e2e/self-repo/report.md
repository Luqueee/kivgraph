# Does the graph help an agent ship a real change? Self-repo sweep

Run of `benchmarks/agent-e2e` over this repository's own history, `cold`
against `kivgraph`. Per-run numbers: `results.json`. The 48 agent transcripts
(27 MB) are not committed. Problems found along the way: `FINDINGS.md`.

## Setup

|||
|---|---|
|corpus|clone of this repository at `d79c483`, Go only|
|tasks|12 `fix(...)` commits from 2026-10, 2 to 5 Go files each, at least one test file|
|state given to the agent|the commit's parent as a one-commit repository; the commit is unreachable (asserted by the harness)|
|prompt|the commit message with path-like tokens removed|
|arms|`cold`: `Read`/`Grep`/`Glob`/edit. `kivgraph`: the same plus the graph tools over MCP|
|model|`claude-sonnet-5`, headless, no shell, no builds or tests|
|trials|2 per task per arm: 48 runs, `$37.30` total, 0 errors|
|cap|`$1.50` per run, same for both arms|
|score|files the agent wrote against files the commit changed; no judge model|

The instruction files of the corpus (`AGENTS.md`, `CLAUDE.md`, `.mcp.json`) were
removed from the prepared copy. Checked in the transcripts: the `cold` arm had no
`mcp__` tool and no run read an instruction file.

## Result

|measure|`cold`|`kivgraph`|paired difference (95 % bootstrap)|
|---|---|---|---|
|cost, 24 runs|`$20.54`|`$16.76`|`-$0.16` per run (`-0.27` to `-0.05`), `-18 %`|
|tool calls per run|28.0|22.7|`-5.3` (`-9.0` to `-1.8`)|
|seconds per run|214|172|`-42` (`-69` to `-16`)|
|precision|0.65|0.70|`+0.05` (`-0.03` to `+0.13`)|
|recall|0.65|0.63|`-0.02` (`-0.09` to `+0.03`)|
|exact file set|5 of 24|4 of 24||
|runs that hit the cap|1|0||

`kivgraph` was cheaper in 17 of the 24 pairs and in 9 of the 12 tasks.

## What this supports

- **It cost less on this corpus.** About 18 % less per run, with a paired interval
  that excludes zero, and fewer calls and less time.
- **It did not find more of the right files.** Recall is the same within noise
  and the exact-set count is not higher. Precision is higher by `0.05`, inside
  the interval. The saving is in how it gets there, not in a better answer.

## What it does not support

- **The size of the effect.** With 12 tasks the per-task sign test is
  `p = 0.146` (9 of 12). The interval on cost is a bootstrap over 12 tasks and
  is optimistic about a different repository.
- **A claim about other code.** One repository, Go only, and it is the project
  the tools were built against.
- **Correctness of the code.** Only files were scored. Nothing was compiled or
  tested.

## Where the number comes from

The two tasks with the largest gap carry a lot of it:

|excluded|cost difference|
|---|---|
|none|`-18.4 %`|
|`511901e`|`-13.9 %`|
|`511901e` and `4eea720`|`-10.9 %`|

`511901e` is the task whose prompt came out most garbled (`FINDINGS.md`, T1).
There `cold` made 43 to 44 calls and hit the cap once, while `kivgraph` made 21 to
27. Whether the graph or the broken prompt explains that is not separable here.
The direction survives removing both, with a smaller size.

## Run-to-run noise

The same arm on the same task differed between its two trials by 21 % of the mean
cost for `cold` and 27 % for `kivgraph` (mean of absolute differences). A
single-run gap of that size is not evidence. Accuracy moved too: on `ce38264`
`cold` scored P=0.50 and then P=1.00; on `02dd835` both arms swung between recall
0.20 and 0.60 across trials.

## How the graph was used

In the 24 `kivgraph` runs the agent made 210 `Read`, 147 `Grep`, 120 `Edit` and
only 52 graph calls: `find_symbol` 21, `find_by_intent` 20, `find_references` 10,
`get_source` 1. About 2 per run. It never called `get_blast_radius` or
`trace_dependencies`. The saving comes from a few lookups replacing searches, not
from the graph driving the work.

## Caveats on validity

- Prompts were not reviewed one by one (T1) and some commit bodies state the fix
  (T2); both help the two arms equally but dilute the find-the-files part.
- Two smoke runs on `4eea720` that preceded the sweep are not included.
- The earlier six-arm sweep could not be repeated: its corpus is private (H1).

## Reproduce

```bash
git clone --no-local . /private/tmp/e2e-corpus/kivgraph   # at d79c483
go run ./benchmarks/agent-e2e --dir benchmarks/agent-e2e/self-repo \
  --corpus /private/tmp/e2e-corpus --kivgraph <binary built with -tags ladybug> \
  --languages go --arms cold,kivgraph --trials 2
```

It writes `results.json` and `raw/` next to the task set, so rerunning
overwrites this evidence. Cost was about `$37` on a subscription login.
