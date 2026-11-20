# ADR 0139: an index pass collects garbage at half the default headroom

## Status

Accepted. No change to any fact; the pass takes a little longer and holds less.

## Context

ADR 0138 left `1.6 GB` of the resident peak above the Go heap on `prometheus`
unattributed. Sampling `runtime.MemStats` beside the resident set of the whole
process tree, every `250 ms`, attributes it:

- The heap in use reached `3.7 GB` with `1.9 GB` live. The collector runs when
  the heap is twice what was live at the last cycle (`GOGC=100`), and the live
  heap is largest exactly when the syntax and type information of every
  package is held. The rest was garbage waiting for its cycle.
- After the analysis the heap falls to `0.45 GB` in use, and Go reports `3.3 GB`
  of the idle remainder as released. On macOS the process still counts those
  pages in its resident set (they are freed lazily), so `maximum resident set
  size` overstates what the machine holds; `peak memory footprint` from
  `/usr/bin/time -l` does not. Forcing the release at the start of the
  staging stage (`debug.FreeOSMemory()`) changed neither: `4.75` and `4.94 GB`
  before and `4.85` and `4.99 GB` after.
- The peak footprint was about `0.7 GB` above the heap in use. It was not
  broken down further.

So the lever is the collector's headroom while the pass runs, not what is
returned afterwards.

## Decision

`indexing.RunFull` sets the GC percent to `50` for the length of the pass
(`gcpolicy.go`). Passes of one process can overlap, so the first sets it and the
last puts it back. A `GOGC` in the environment is the operator's decision and
wins; a collector that is off, or already below `50`, is left alone.

## Consequences

Same machine, two runs each, `/usr/bin/time -l` peak footprint and wall time,
before and after (ADR 0138 binary against this one):

- `prometheus`: `4.22`/`4.40 GB` to `3.42`/`3.19 GB`; `13.0`/`14.3 s` to
  `14.5`/`15.6 s`.
- `caddy`: `1.87`/`1.33 GB` to `1.24`/`1.13 GB`; `5.0`/`4.7 s` to `4.7`/`4.9 s`.
- `gin`: `0.86 GB` to `0.45 GB`; `2.4`/`2.3 s` to `2.3`/`2.2 s`.
- `cobra`: `0.39`/`0.40 GB` to `0.39 GB`; no change in time.

`GOGC=25`, set from the environment on `prometheus`, took the footprint to
`2.9 GB` for `16.9 s`, against `3.5 GB` and `14.5 s` for `50`; fifty is the
point where the time did not grow past a tenth. A smaller percent is the
operator's to choose with `GOGC`.

The setting is process-wide, so an indexing pass of a daemon that is also
serving collects more often while it runs.
