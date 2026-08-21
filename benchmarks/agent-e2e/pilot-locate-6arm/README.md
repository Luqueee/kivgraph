# Pilot: six arms, one task where locating is the work

One task, six arms, one trial, `$3` cap, `claude-sonnet-5`. It exists to answer
two questions before a full sweep is worth paying for: does the plumbing of four
new arms work, and does a task set built around *finding* the call sites separate
them at all.

The frozen 36-run sweep next door answered neither, and said so: its own
conclusion was that its tasks did not require locating anything, so it measured
three arms doing the same easy thing and found no effect.

## What ran

`api-db-go-821977c` — «add a requirements block to the levelling module's
configuration, with its validation and its reflection in the cache». Four files
across four directories in `services/api-db-go`. The intent is written as a user
request, not lifted from the commit message, which named helpers that did not
exist yet.

|arm|`P`|`R`|files|graph calls|cost|seconds|
|---|---|---|---|---|---|---|
|**kivgraph**|**`0.75`**|`0.75`|`4`|`4`|**`$1.86`**|`223`|
|cold|`0.50`|`0.75`|`6`|`0`|`$3.05` capped|`272`|
|code-review-graph|`0.50`|`0.75`|`6`|`4`|`$3.02` capped|`348`|
|graphify|`0.43`|`0.75`|`7`|**`0`**|`$2.68`|`286`|
|graft|`0.33`|`0.25`|`3`|`13`|`$1.55`|`185`|
|codebase-memory|`0.25`|`0.25`|`4`|`5`|`$2.79`|`271`|

Total `$14.95` over 26 min 51 s. Nothing errored, nothing leaked, two runs hit
the cap: cold and code-review-graph.

## What it establishes

**The task set separates the arms.** Six different scores where the previous set
produced a wall of zeros and exacts. That was the blocker, and it is cleared.

**kivgraph is the only arm that beat cold**, on the axis the tools claim: the same
recall with a precision of `0.75` against `0.50` — four files touched where the
cold agent touched six — for `39%` less money, without exhausting the cap that
cold and code-review-graph both hit.

**graphify made zero graph calls**, with a shell and a directive naming its exact
commands. Its arm is a cold agent next to a directory it does not open. graft made
thirteen and still finished at `R=0.25`.

## What it does not establish

`n=1` per arm. One task, one trial, one language. Nothing here is a result about
any of these tools; it is a measurement that the instrument now works and a
direction worth paying to test.

## The cap is biting

Two of six runs spent the whole `$3`, and a third did in the earlier unscored
pilot. A run that stops because the money ran
out reports where its budget ended, not what its context layer knew, so a sweep
at this cap would publish a precision that partly measures the cap. The next run
should raise it until the arms finish on their own.

## Reproduce

```bash
go run ./benchmarks/agent-e2e \
  --tasks tasks-locate.json \
  --dir benchmarks/agent-e2e/pilot-locate-6arm \
  --only api-db-go-821977c --trials 1 --budget-usd 3.00
```

The task set is `benchmarks/agent-e2e/tasks-locate.json`; a set other than the
frozen one refuses to run in the default directory, because that is how this
pilot overwrote the 36-run sweep's `results.json` the first time.
