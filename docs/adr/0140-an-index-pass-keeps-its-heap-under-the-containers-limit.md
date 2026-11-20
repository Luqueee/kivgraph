# ADR 0140: an index pass keeps its heap under the container's limit

## Status

Accepted. No change to any fact. Linux only; other platforms run as before.

## Context

ADR 0138 and ADR 0139 were measured on macOS, where the resident set counts
pages Go has already released. The same three builds were measured on Linux
(`linux/arm64`, a container with 2 CPUs and 7.8 GB, `/usr/bin/time -v`, two
runs each and one for `cobra`), where `Maximum resident set size` is what the
kernel charges:

| repository | before | ADR 0138 | ADR 0139 |
|---|---|---|---|
| `prometheus` | `7.3`/`7.7 GB` | `4.7`/`4.5 GB` | `3.5`/`3.8 GB` |
| `caddy` | `2.8`/`2.9 GB` | `1.9`/`1.6 GB` | `1.3`/`1.4 GB` |
| `gin` | `1.27`/`1.23 GB` | `0.56`/`0.81 GB` | `0.44`/`0.46 GB` |
| `cobra` | `0.58 GB` | `0.32 GB` | `0.27 GB` |

The two changes hold there, by about a half on `prometheus`. Time on that
machine: `50`/`49 s`, `39`/`38 s` and `44`/`42 s`.

What Linux adds is the limit. A container has a memory limit and the Go runtime
does not read it: the collector lets the heap grow to what the GC percent
allows and the kernel kills the process at the limit. With `--memory=3g` the
`prometheus` pass was killed at `3.06 GB`, although `2.4 GB` is enough for it
when the collector is told so.

## Decision

For the length of a pass, when the process is in a cgroup with a memory limit,
the Go soft memory limit is set to four fifths of it (`gcpolicy.go`,
`cgroup_linux.go`). The fifth left is for what Go does not count: the engine's
memory, the go command, the analyzers.

- The limit is read from `memory.max` (unified hierarchy) or
  `memory.limit_in_bytes`. `max`, and the page-rounded maximum an old kernel
  reports for no limit, are no limit.
- `GOMEMLIMIT` in the environment wins, and so does a limit already set from
  code. Overlapping passes share it: the first sets it and the last puts it
  back.
- Without a cgroup limit nothing is set. The file is read in a Linux-only
  source file; other platforms answer that there is none.

## Consequences

`prometheus`, same container image, by memory limit:

| limit | before | after |
|---|---|---|
| `3 GB` | killed at `3.06 GB` | passes, `2.46 GB`, `45 s` |
| `2.5 GB` | not run | passes, `2.45 GB`, `56 s` |
| `2 GB` | not run | killed at `2.04 GB` |
| none | `3.5 GB`, `47 s` | `3.8 GB`, `52 s` (no limit to read; run-to-run spread) |

- The soft limit is not a guarantee. When what is live does not fit, the
  collector runs up to half the CPU and the process is still killed, as at
  `2 GB`.
- Near the limit the pass is slower, because the collector runs more: `56 s`
  at `2.5 GB` against `45 s` at `3 GB`.
- A machine without a cgroup limit, a laptop or a bare server, gets nothing
  from this: its limit is the machine.
