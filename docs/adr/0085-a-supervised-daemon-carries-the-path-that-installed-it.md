# ADR 0085: a supervised daemon carries the PATH that installed it

- **Status:** accepted -- the two defects are reproduced and fixed; the
  field report they come from is issue `#105`
- **Date:** 2026-08-30
- **Revises:** ADR 0068
- **Changes the MCP protocol:** no
- **Changes the CLI surface:** yes -- `daemon status` reports an existing
  unit as `stale` once, and `daemon install` may warn about node
- **Implementation:** `internal/supervisor`, `internal/indexing/resync.go`

## Where the numbers come from

Nothing here is measured by this ADR. Two sources carry it:

- **The failure loop.** The field report in issue `#105`: kivgraph
  `0.9.2` against a snapshot of generation `91` built with resolver
  `0.8.1`, `53` registered repositories, node `v24.18.0` through nvm, Go
  `1.26.6` in `~/.local/go/bin` beside the distribution's `1.24.4` in
  `/usr/bin`. Roughly ten failed `index --full` attempts a minute, for
  about twenty minutes, ended by its owner noticing the CPU rather than
  by anything in the loop.
- **The start limit one layer up.** ADR 0068 and
  `restart_limit_linux_test.go`, measured `2026-08-28`: a unit whose
  `ExecStart` named a deleted binary reached `NRestarts=140` under the
  shipped defaults and stopped at `5` once the window could trip.

## Context

`unit()` wrote `Description`, `Type`, `ExecStart`, `WorkingDirectory`,
`Restart` and `RestartSec`, and no environment. A systemd user unit that
declares none inherits systemd's own `PATH`:

```
/home/<user>/.cargo/bin:/home/<user>/.local/bin:/usr/local/bin:/usr/bin:/bin:/usr/local/games:/usr/games
```

`kivgraph-ts-worker` ends in `exec node`. Node installed through nvm lives
under `~/.nvm/versions/node/<version>/bin` and reaches `PATH` through
`.bashrc`; systemd sources no shell profile, so the worker exits `127` and
every TypeScript repository fails to index. Go fails more quietly: a
current toolchain in `~/.local/go/bin` loses to the distribution's in
`/usr/bin`, the pass succeeds, and the graph is a different one with
nothing to report. launchd is the same shape with a shorter list --
`/usr/bin:/bin:/usr/sbin:/sbin`, which holds neither Homebrew nor nvm.

What makes it expensive to diagnose is that it is invisible from the
shell. `kivgraph index --full` typed by hand works, because the shell has
the `PATH` the unit lacks, and the logs name the indexer rather than the
environment.

The second half is what turned a broken toolchain into a permanent load.
`resync.go` absorbed the failure, rewound the tracker so the movement
would not be forgotten, and tried again. The rewind is right: the tree
really did move. Nothing bounded the retries, so at `ResyncInterval` `2s`,
`ResyncDebounce` `3s` and a rebuild that fails in about `0.9s`, the loop
settles at one full attempt every six seconds and does not stop.

The argument against that already existed one layer up. The comment above
`unit()` reasons about `StartLimitIntervalSec` and `StartLimitBurst` and
concludes that a daemon which failed to start five times in half a minute
will not be fixed by a sixth attempt. That holds just as well for a
rebuild of a tree that has not moved again.

## Decision

**The unit records the PATH of the shell that installed it.** Both
backends write it -- `Environment="PATH=..."` on systemd, an
`EnvironmentVariables` dict on launchd -- from `os.Getenv("PATH")` at
install time. That shell is the one place where the user's toolchains
demonstrably resolve: they typed the command in it. It also makes the unit
self-describing, where ambient state describes nothing.

`status` compares the daemon and not the shell. The recorded `PATH`
belongs to the terminal that ran `daemon install`, so comparing it would
report a working daemon `stale` from any other shell and teach its
operator that `stale` means nothing. What is compared is whether a `PATH`
is recorded at all, which is what puts every existing installation through
one `stale` and one reinstall -- the daemon under a unit that records none
is exactly the daemon that cannot resolve node.

**The resync loop gives up after `ResyncAttempts` consecutive failures of
one unchanged batch.** Five, to match `StartLimitBurst`. A batch is
identified by its content -- which repositories moved, and between which
commits -- so a retry carries the same fingerprint and adds to the count
while any genuinely new movement carries a different one and starts over.
Giving up rewinds nothing: the tracker keeps the commit the tree is
actually on, so the batch is never proposed again and only a new movement
can produce work. `OnGaveUp` reports it once, and the daemon logs it at
error level with the remedy.

`daemon install` additionally warns when the `PATH` it just recorded
cannot resolve `node` and a registered repository declares TypeScript or
JavaScript. It is a warning and not a refusal: a workspace that never runs
the worker does not need node, and refusing to supervise a daemon that
would have worked is a worse failure than the one being reported.

## Consequences

Every installed unit reports `stale` once after this ships, and the remedy
`daemon status` already prints -- `kivgraph daemon install` -- is the right
one. That is the intended migration and not a side effect: a unit written
before this change carries the defect.

The recorded `PATH` is a snapshot. An nvm upgrade moves node to a new
versioned directory and the unit goes on naming the old one, so the remedy
is `daemon install` again. It is the same remedy the workaround in issue
`#105` needs, and unlike a hand-written drop-in it is not silently pinned
to a version nobody will remember to update.

A resync that gives up leaves the published graph on the commit it was
built from until somebody fixes the failure and runs `index --full`, or
until the tree moves again. That is a graph which is behind and says so,
against a machine that rebuilds forever and never gets ahead.

## Alternatives considered

**Have `kivgraph-ts-worker` resolve node itself** -- `KIVGRAPH_NODE`, then
a node recorded at install, then `PATH`. It fixes the loud half and leaves
the silent one: the Go toolchain the daemon picks is still the wrong one,
and that failure produces a different graph rather than an error.

**Exponential backoff instead of a bound.** It softens the symptom and
keeps the loop. A loop that cannot succeed should end, not slow down.

**Refusing `daemon install` without node.** It would block every install
on a machine with no TypeScript registered, which is a regression for
those users in exchange for a warning they do not need.

## Not decided here

`index --full` aborts on the first repository that fails, so one
permanently broken toolchain costs the whole graph -- `52` of the `53`
repositories in the field report would have indexed fine. Changing that
means publishing a generation that declares a hole for one language, which
is a contract question about `UNRESOLVED` and completeness rather than a
bug in either of these two paths. It needs its own ADR.
