# The `metrics` branch

This branch holds one file per day under `downloads/`, each a cumulative
photograph of the download counters of every release asset, taken by
`.github/workflows/download-metrics.yml`. It carries no code and shares no
history with `main`: it is an orphan so that the history of the measurement
never appears in the history a release tag is cut from.

## What a file says

```json
{
  "schema": 1,
  "repository": "Luqueee/kivgraph",
  "captured_at": "2026-08-28T03:17:04Z",
  "assets": [
    {
      "tag": "v0.9.1",
      "asset": "kivgraph-linux-amd64.mcpb",
      "id": 123456789,
      "class": "mcpb",
      "platform": "linux-amd64",
      "downloads": 10
    }
  ]
}
```

`downloads` is the running total GitHub reports, not the traffic of that day.
A scheduled workflow is disabled after sixty days without repository activity
and a run can be delayed or dropped, so a missing day is expected: a total
survives the gap, and a stored delta would have lost it.

`id` is stored because it is what tells a counter reset from a lie. Publishing
an asset with `--clobber` replaces it, and a replaced asset is a different
asset whose counter starts at zero.

## No branch may be named `metrics/anything`

Git stores a branch as a file under `refs/heads/`, so `refs/heads/metrics` and
`refs/heads/metrics/download-series` cannot both exist: one is a file where the
other needs a directory. A branch under `metrics/` fetches with

```
cannot lock ref 'refs/remotes/origin/metrics': 'refs/remotes/origin/metrics/x'
exists; cannot create 'refs/remotes/origin/metrics'
```

and the fix is `git remote prune origin` once the offending branch is gone.
This branch is the data, so the namespace is spent: name working branches
something else.

## Reading it

From a checkout of `main`, with this branch cloned anywhere:

```sh
scripts/downloads.sh series path/to/metrics/downloads
```

That derives the daily traffic, marks the first row as the baseline rather
than a day, says how many days each row covers, and lists the counter resets
it had to account for. `scripts/downloads.jq` holds the classification and the
derivation; nothing else should reimplement either.
