# ADR 0084: the stdio entry outlives the stdio server

- **Status:** accepted in shape, gated on the relay's own process floor
  being measured first
- **Date:** 2026-08-28
- **Revises:** ADR 0066, ADR 0069
- **Changes the MCP protocol:** no -- the same surface, reached over the
  same two transports
- **Changes the CLI surface:** yes -- `serve` stops loading a graph in the
  common case, and may install a supervisor
- **Implementation:** `LUQUE-2233`; nothing in *Decision* is built

## Where the numbers come from

Every figure below is quoted rather than produced here, and each has one
source:

- **The eight-client tables, idle and answering.**
  `benchmarks/daemon-cost/report.md`, schema `daemon-cost-v3`, measured
  `2026-08-23` on the `workspace` corpus at `108,737` symbols with a
  `77.6 MB` snapshot.
- **`69` starts of `serve` with `8` alive, and `48` of `51` silent.** The
  event log of a machine in use, counted by ADR 0067 and ADR 0069.
- **The release-page shares, `39 %` and `5 %`.** ADR 0083, from
  `gh api repos/Luqueee/kivgraph/releases --paginate` on `2026-08-28`.
- **The workspace of today and the live daemon.** `graph_status` and
  `/proc/<pid>/smaps_rollup` of the running daemon, both read
  `2026-08-28`. This is one machine on one day, not a corpus: it bounds
  the prize, and the prototype in `LUQUE-2233` is what measures it.

## Context

ADR 0069 made the daemon the default and measured why: at eight clients,
`77`-`81 MB` of private pages against `10`-`13`, peaks of `179`-`186`
against `26`-`29`, and `38`-`55 ms` to connect against `1.6`-`2.0`.

Those are ADR 0069's case for the daemon and they are **not** this ADR's
case for the relay. ADR 0067 takes most of the idle half of them back, for
reasons in *What the relay is worth* below.

What it could not do is stop the stdio server from running, and the reason
is not that anyone prefers it. `resolveTransport` and
`integrationManagerOptions` still write a command entry in four declared
cases: `--stdio`, project scope, a platform with no supervisor, and a
machine with no configuration yet. Three of them are silent.

And the case that carries the volume is not in that list at all.
`scripts/build-mcpb.sh` declares `serve` in the bundle manifest, so every
`.mcpb` installation is stdio. ADR 0083 measured the release page: `.mcpb`
is `39 %` of all downloads and the installer channel is `5 %`. **The
transport ADR 0069 replaced is the transport most installations run.**

Two of those cases cannot be fixed by kivgraph changing its mind:

- the `.mcpb` manifest describes a local process and its runtime. There is
  no field for a url, so the format cannot express a remote server;
- a url entry carries the token literally -- `Endpoint.Token`, "written
  literally, because no client reads a token out of a file for us" -- and a
  project file is committed. `integrations.New` refuses the combination.

So the stdio **entry** is permanent. The stdio **server** is not, and it is
the one that costs.

## Decision

**Nothing in this section is built, and the status above is gated.** The
present tense states what the relay, the provisioning, the skew refusal and
the fallback **must** do if the prototype clears the floor; `LUQUE-2233`
carries the work, and until its first commit reports a number none of this
describes a running system.

`serve` stops serving. It becomes a relay between the client's stdio and
the daemon's Streamable HTTP endpoint, and it loads no graph.

The SDK already exposes the piece this needs. A `Connection` is
`Read`/`Write`/`Close` of a `jsonrpc.Message`, so the relay uses neither
`Server` nor `Client`: it connects both sides and copies messages.

```go
agent, _ := (&sdkmcp.StdioTransport{}).Connect(ctx)
daemon, _ := (&sdkmcp.StreamableClientTransport{
    Endpoint:             endpoint.URL,
    HTTPClient:           bearer(endpoint.Token),
    DisableStandaloneSSE: true,
}).Connect(ctx)

go pipe(ctx, agent, daemon)
pipe(ctx, daemon, agent)
```

Messages pass opaquely, so a tool added to the daemon works without the
relay being touched. The endpoint and its token come from `daemon.json` in
the state directory, which is why a committed `.mcp.json` stops carrying a
secret: the stdio entry names a command, and the command finds the token
on disk.

### Why this is not ADR 0066 re-litigated

ADR 0066 set the relay aside with "MCP already has an HTTP transport and
the clients already speak it". That is true of a configuration file this
project writes. It is not true of an `.mcpb` manifest, which has nowhere
to put a url. The argument that retired the relay never covered the
channel that carries `39 %` of the downloads.

### `serve` may install the supervisor

`ensureConfiguration` already decided the principle and wrote down why:

> An MCP client starts its servers itself: it spawns `kivgraph serve` […]
> A server that exits because nobody ran `init` first turns installing the
> integration into a terminal session, and the client only reports that
> the server failed.

The reader/writer separation ADR 0069 defends is about the `mcp` commands,
not about `serve`, which has provisioned on the client's behalf since it
existed. Installing the supervisor is a larger step than writing two files
under `~/.config`, and it is the same step: without it the `.mcpb`
installation -- the one that never sees a terminal -- can never have a
daemon at all, which is the condition ADR 0069's default was measured
against.

The daemon it installs **lives as a service**. It outlives the client and
the reboot, and `mcp remove` is what takes it away. That is what pays the
saving across sessions rather than within one, and it is the ownership ADR
0068 required.

### Version skew refuses

Today one process makes skew impossible. With a relay, a `serve` from one
installation can reach a daemon another installation started -- the `.mcpb`
carries its own binary in the extension directory while `stateDirectory`
comes from the configuration, so two installations share one daemon by
construction. The relay compares versions on connect and **refuses with a
message** rather than restarting a daemon other clients are using.

### The in-process fallback stays, and it is measured

A platform with no supervisor has no daemon to reach, and provisioning can
fail. In both cases `serve` does what it does today: it serves the surface
in its own process, and **on those two paths** nothing gets worse than the
current state.

The claim stops there, because one path does get a failure it did not have:
a `serve` and a daemon at different versions are refused where today they
cannot meet, since today there is only one process. That is the trade in
*Version skew refuses* above, taken deliberately, and it is the one place
this design is not a superset of the current behaviour.

`transport` on the ADR 0083 ping is what says how often that happens, and
the ordering is what makes the number mean anything. **Shipping the
fallback without the provisioning would have measured nothing**: no
`.mcpb` installation ever runs `mcp install`, so the fallback would fire
every time by construction, and the field would report this project's own
decision rather than anyone's behaviour. With provisioning shipped, the
same field reports the failure rate of provisioning, which is a number
that decides whether the next step is worth taking.

### What the relay is worth, and the number nobody has

ADR 0067 already took the snapshot off the idle path, and that changes the
size of this prize. `benchmarks/daemon-cost/report.md` records the before
and after: an idle `serve` fell from `33.9 MB` per client to `9.8`-`10.7`.
So the `77`-`81 MB` that eight idle clients cost is **not** a snapshot. It
is a per-process floor of about `10 MB`, eight times over -- and a relay is
the same binary with the same Go runtime, so it pays a floor of its own.

| load | `serve` per client | daemon per client | share |
| --- | ---: | ---: | --- |
| no calls | `9.8`-`10.7 MB` | ~`0 MB` | `48` of `51` |
| `8` calls | `38.4`-`39.5 MB` | `0.63`-`0.95 MB` | |
| `2,000` calls | `66.1`-`66.2 MB` | `11.1`-`13.3 MB` | |

The relay turns the middle row into its own floor, which is a large
saving, and the top row into the difference between two floors, which may
be nothing. The top row is where `48` of `51` sessions live.

**And that table is measured on a corpus that no longer exists.**
`daemon-cost` ran on `37` repositories and `108,737` symbols with a
`77.6 MB` snapshot. A workspace in use today, read from `graph_status`,
is `53` repositories, `229` packages, `6,418` files, **`177,790` symbols**
and `528,603` edges: `1.6` times the symbols the benchmark measured.

The daemon serving that workspace, after `23` hours and `136` tool calls,
reads:

| metric | value | what it is |
| --- | ---: | --- |
| `Rss` | `294,484 kB` | ~`287 MB` |
| `Private_Dirty` | `224,316 kB` | anonymous heap, not reclaimable |
| `Private_Clean` | `67,152 kB` | the mapped snapshot, clean |
| `Shared_Clean` | `3,016 kB` | |
| `Swap` | `17,668 kB` | |

The snapshot behaves exactly as `daemon-cost` described it: mapped, clean,
cheap. The `219 MB` is heap, and it is **not** a leak -- scaling the
benchmark's `163`-`174 MB` daemon at `2,000` calls by the same `1.6` gives
`260`-`280 MB`, which is where this process sits after `136` calls. The
daemon's cost tracks the size of the graph, not the number of questions.

Which is the finding: **the rows below the first scale with the corpus,
and the first one does not.** A `serve` that answers on this workspace
builds the same tables the daemon builds, so the same arithmetic predicts
a working client near the daemon's own footprint rather than near the
`38`-`39 MB` the benchmark recorded -- eight of them being gigabytes, not
the `323`-`330 MB` in the table. Nobody has measured an answering `serve`
on this corpus, and that prediction is half of what the prototype checks.

**The case still rests on one unmeasured number: what a relay process
costs at rest.** What changed is the gap it has to clear. Against an idle
`serve` at `9.8`-`10.7 MB` a floor of `8 MB` would save `4 MB` across
eight clients and there would be nothing here worth building. Against a
working client on a real workspace the same floor is competing with
something two orders of magnitude larger.

That is why the status above is gated, and why the work starts with a
forty-line prototype that connects and does nothing, measured by
`benchmarks/daemon-cost` **on a corpus this size** and not on the one from
August. It reports two numbers: the relay's resident floor, and an
answering `serve` beside it. If the floor does not clear the gap, this
decision is withdrawn and the stdio server stays.

## Consequences

- The `.mcpb` channel gets the daemon for the first time. It is `39 %` of
  downloads and it has never had one.
- A project-scoped `.mcp.json` stops being the reason to accept a second
  graph in memory. The entry stays stdio and the token stays on disk.
- One process per client remains, and what it saves is bounded by its own
  floor rather than by the snapshot -- ADR 0067 removed that from the idle
  path already. The saving is large under load and may be negligible at
  rest.
- A machine that installs the `.mcpb` acquires a supervised background
  service it did not ask for in a terminal. That is stated in the
  first-run notice ADR 0083 already introduces, and `mcp remove` takes it
  away.
- Two installations at different versions now fail loudly instead of
  silently sharing a daemon. That failure did not exist before because the
  situation could not arise.

## Limitations

- **The standalone SSE stream does not open.** The persistent GET that
  carries server-initiated messages with no request in flight is started
  by `sessionUpdated`, which only the SDK's `Client` calls, and the relay
  uses none. Today nothing is lost: the two messages kivgraph initiates --
  `NotifyProgress` and `Elicit`, both in
  `internal/mcp/tools/index_project.go` -- happen inside a call in flight
  and travel down that POST's own stream. The day the daemon sends
  anything with no request open, the relay swallows it in silence. Hence
  `DisableStandaloneSSE: true` written explicitly, and a test that pins
  it.
- A burst of clients starting at once all find no daemon and all try to
  provision. The provisioning is behind a lock, and the losers wait for
  `endpointDeadline` rather than each installing a supervisor.
- The relay adds a process spawn and an HTTP handshake to every session.
  Whether that lands nearer `1.6`-`2.0 ms` or nearer the `38`-`55` it
  replaces is measured with the floor, by the same prototype, before any
  of this ships.
- A platform with no supervisor keeps the cost ADR 0069 measured, in full.
  None of the three published platforms is one.
