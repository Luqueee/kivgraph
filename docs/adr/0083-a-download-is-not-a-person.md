# ADR 0083: a download is not a person

- **Status:** accepted; Layer 0 is implemented, Layer 1 is still a design
- **Date:** 2026-08-28
- **Implementation:** `LUQUE-2232`

## Context

`scripts/downloads.sh` is everything this repository has for the question
*how many people use Kivgraph*. It reads GitHub's `download_count` per
release asset and adds the numbers up. Three properties keep that sum from
answering the question, and one of them keeps it from answering any
question at all.

**It is cumulative and undated.** A total that only ever grows cannot say
whether last week was better than the week before, which is the only shape
of the question worth asking.

**One installation is several downloads, and how many depends on the
channel.** `curl … | bash` fetches `install.sh`, then the platform archive,
then `SHA256SUMS`: three increments for one install. A `.mcpb` is one. So
two channels are not comparable to each other, and a channel is not
comparable to itself across the release that added an asset to it.

**And `--clobber` takes it backwards.** The publish job ends by uploading
every asset with `--clobber`. Clobbering replaces the asset, and a replaced
asset is a new asset whose counter starts at zero. Re-running `publish` on
an existing tag therefore lowers a number that is documented everywhere as
monotonic.

### What the counter says today

Every release from `v0.1.0` to `v0.9.1`, by asset class. Every count below
is the `download_count` the releases API returned on **2026-08-28**,
grouped by the extension of each asset:

```bash
gh api repos/Luqueee/kivgraph/releases --paginate \
  --jq '.[] | .tag_name as $t | .assets[]
        | [$t, .name, .download_count] | @tsv'
```

| class | downloads | share |
| --- | ---: | ---: |
| `.mcpb` | `46` | `39 %` |
| `.tar.gz` / `.zip` | `36` | `31 %` |
| `SHA256SUMS` | `29` | `25 %` |
| `install.sh` / `install.ps1` | `6` | `5 %` |

And `v0.9.1`, hours after publication:

| asset | count | asset | count |
| --- | ---: | --- | ---: |
| `kivgraph-linux-amd64.mcpb` | `10` | `kivgraph-linux-amd64.tar.gz` | `0` |
| `kivgraph-windows-amd64.mcpb` | `9` | `kivgraph-windows-amd64.zip` | `0` |
| `kivgraph-darwin-arm64.mcpb` | `8` | `kivgraph-darwin-arm64.tar.gz` | `0` |
| `install.sh` | `0` | `SHA256SUMS` | `0` |

Three readings, and none of them is the one the README assumes.

**The channel the README leads with is the one nobody uses.** Six installer
downloads across nine releases. Any work that instruments `install.sh`
instruments `5 %` of the volume.

**`.mcpb` carries the volume and is the most contaminated number on the
page.** `27` downloads in a day, split near-evenly across three platforms,
is the signature of a machine and not of people: people concentrate on the
platform they are on. At least three per release are ours, because the
`registry` job publishes the three `.mcpb` files as packages with a
`fileSha256` and the registry fetches each URL to verify it. CI never
downloads Kivgraph's own assets -- the only `releases/download` URLs in the
workflows are coursier's and `mcp-publisher`'s -- so the rest is external,
and nothing on the release page distinguishes external-and-a-person from
external-and-a-directory mirroring the registry.

**`bundle` ≈ `checksums` is consistent with the verified path.** `36`
against `29`: every automated install takes both. The seven-unit gap is
**not** evidence of seven manual downloads that skipped verification. A
retry, a checksum fetched on its own, and the `--clobber` reset above all
produce the same gap, and an aggregate of two totals cannot separate them.
It is a hypothesis this counter can raise and cannot settle -- which is the
next paragraph in miniature.

That last line is the shape of the whole problem. The counter supports
arithmetic, and every quantity the arithmetic yields is a *download*. No
operation over it produces a person, because nothing in it separates one
machine fetching an asset seven times from seven machines fetching it once.

## Decision

Two layers. They answer different questions and neither is a step towards
the other.

**Nothing below is built.** The present tense states what each layer
**must** do, not what it does; `LUQUE-2232` carries the work and its gates.
A reader asking whether a control is deployed should read the task, not
this section.

### Layer 0 -- the series, with no client involvement

A scheduled workflow snapshots the releases API daily and writes one JSON
document per day to an orphan `metrics` branch. `main` stays a record of
changes; a daily commit of data on it is noise in every history listing
forever.

Two invariants decide whether the store is usable:

**Snapshots are cumulative; deltas are derived when read.** GitHub disables
scheduled workflows on a repository with sixty days of no activity, so a
missed day is expected rather than exceptional. A cumulative snapshot
survives it, because the next one still carries the total. A stored delta
loses that day permanently.

**A decrease is a counter reset, never negative traffic.** That is
`--clobber`, above. The reader clamps the delta at zero and records the
reset as its own fact, so a re-published tag reads as a gap in the series
and not as people un-downloading a file.

Building it found the sharper form of that invariant. The snapshot stores
the asset id next to the count, and `--clobber` changes it: a replaced
asset is a *different* asset whose counter starts at zero. So a
replacement is not clamped -- its whole total is traffic, because that
counter started after the previous photograph, and only what the old asset
took between the photograph and its replacement is lost. Clamping is left
for a count that fell with the id unchanged, which nothing here explains;
that one contributes zero and is recorded as `unexplained`, because
silently clamping an anomaly is how a broken source keeps looking healthy.

The classification and the derivation live in `scripts/downloads.jq`,
included by both readers, so the workflow that writes the series and the
command that shows today cannot come to disagree about what a `bundle` is.

Layer 0 also fixes the classification the table above uses -- `bundle`,
`checksums`, `installer`, `mcpb` -- and names one KPI: downloads of the
`bundle` class, per platform, per day. `scripts/downloads.sh` becomes the
live view over that same classification instead of a second opinion.

### Layer 1 -- one ping, two emitters, one endpoint

`POST https://kivgraph.dev/api/telemetry/first-run`, carrying `emitter`,
`version`, `platform`, `channel` and `transport`, from:

- `install.sh` and `install.ps1`, after the archive is verified and the
  install has succeeded, so a ping means a working installation and not an
  attempt;
- the binary, on the first run of each version it is installed as.

The endpoint lives in `landing/server.mjs` and forwards to Umami, because
the reporter, the header finding it depends on and the fail-closed
configuration pair are already there; an Astro route would need a second
copy of all three.

**Identity is Umami's, and this repository mints none.** Umami derives a
visitor from a daily-rotating hash of website id, hostname, address and
user agent. *Unique visitors per day* is therefore distinct machines that
reported that day, and the address itself is never stored. An identifier of
our own would answer better and would also have to be explained, stored and
defended; this one is answered by software already deployed.

The load-bearing consequence is that **the endpoint must forward the
caller's address to the collector**. Without it every install on earth
collapses into one visitor -- the landing server. And `REPORTER_HEADERS`
forces `User-Agent: ""` to survive the collector's `isbot` filter, so the
address is the *only* discriminator left: a corporate NAT counts as one
person. That is the bias every web analytics carries, and it is written
here rather than discovered in a report.

**A third property, `kivgraph FIRST RUNS`,** for the reason the AI crawlers
property exists: an install is not a visit, and mixing them moves
visitors, bounce rate and the conversion rate that describes people.

**`emitter` is why the two sources do not become one number.** An installer
that finished and a binary that started are different facts, and the second
does not follow from the first: a bundle can be installed and never
launched. Without the field the property would report an installer's
success as a first run, which is the claim this ADR spends its length
refusing to make. It is `installer` or `binary`, the two are aggregated
separately, and only the `binary` rows answer *how many machines ran it*.

**The endpoint is public, so the number is worth exactly its bounds.**
Strict validation against the closed sets of platform, channel and
transport and the published version pattern, an in-process dedupe window
per address, version **and `emitter`**, and `204` on every path so probing
it teaches nothing.

The `emitter` in that key is load-bearing and easy to leave out. An
installer that has just finished and the first run that follows it carry
the same address and the same version, seconds apart: a window keyed on
those two alone would discard the second, which is precisely the `binary`
row the property exists to collect. The field that separates the two
aggregates has to separate their deduplication too.

What the dedupe window does **not** buy is the headline number, and saying
otherwise would misread the identity above: under a daily-rotating hash one
address reinstalling in a loop is already **one** unique visitor, so
repetition inflates the event count and never the visitor count. The window
bounds events and write volume; validation is what stops a forged `version`
or `platform` from inventing a row no release ever produced.

### Why the binary reports and not only the installer

A `.mcpb` never runs `install.sh`. The MCP client unpacks the bundle and
launches the binary, so instrumenting the installer alone would give clean
data about `5 %` of the volume and nothing about `39 %`. It is also the
only thing that separates a robot from a person on that channel: a
directory downloads a `.mcpb` and never executes it.

**One ping per machine and version, and the metric says so.** The marker
lives under the user's state directory, not the bundle root: an update
replaces the bundle, so a marker there would fire again every time. What
is measured is therefore *the first run of a version*. Calling it
*installations* would be a claim the marker cannot support.

**The marker is a check-and-set, because one of the two transports starts
many processes at once.** ADR 0069 measured a machine in use at `69`
starts of `serve` with `8` alive simultaneously. Reading the marker and
then writing it lets every process of a burst find it absent and report
before any of them has created it, turning one installation into as many
pings as the client happened to launch -- the exact inflation this ADR
exists to remove. The marker is created with `O_CREATE|O_EXCL` and only
the process that created it sends anything.

**The transport travels as a field, because nothing can answer that
question today.** ADR 0069 made the daemon the default, and
`resolveTransport` still writes a stdio entry in four declared cases:
`--stdio`, project scope, a platform with no supervisor, and a machine
with no configuration yet. Three of those are silent. And
`scripts/build-mcpb.sh` declares `serve` in the bundle manifest, so the
`.mcpb` channel -- `39 %` of the volume -- is stdio in its entirety.
Whether the new default is what people actually run is a fact that
changes a decision, which is what `docs/development/analytics.md` demands
of an event before it is added.

**Nothing may reach stdout, and the stricter of the two transports
decides that.** `kivgraph serve` runs the MCP surface over
`sdkmcp.StdioTransport`, where a stray byte on stdout corrupts the
session; the daemon serves the same surface over Streamable HTTP, where
it would not. The ping is shared code, so it obeys the stdio rule
everywhere: a goroutine with a short timeout, every failure dropped, and
the first-run notice on stderr. `KIVGRAPH_TELEMETRY=0` disables both
emitters.

## Consequences

- The question splits in two and both halves become answerable.
  *How many downloads* is Layer 0, exact and unattributable. *How many
  machines reported a first run of a version, per day* is Layer 1 -- not
  how many installed, and **not comparable to a count of people** in either
  direction: one person on three machines reports three, while an opt-out,
  a bundle never launched and a dropped request each report none.
- The gap between the two is itself the answer about `.mcpb`. A channel
  whose download count is `39 %` of the total and whose first-run count is
  near zero is a channel being mirrored, not installed.
- The two transports stop being invisible. ADR 0069 changed the default
  and no measurement followed it; `transport` on the ping is the first
  thing that can say whether the stdio fallback is rare or normal, and
  three of the four ways into it say nothing when they are taken.
- `install_copy` stops being the end of the funnel. It is the primary
  conversion in `docs/development/analytics.md` and today it is the last
  event the site can see; the ping is the first one that says the command
  actually ran.
- Kivgraph starts making a network request it did not make before, on a
  path a user did not ask for. That cost is paid once, in public: an
  opt-out variable, a first-run notice, and a page that lists the fields
  sent.
- `docs/development/analytics.md` gains the new events before any code
  emits them, which is the rule that document states about itself.

## Alternatives considered

**A minted installation id.** It answers retention and version migration,
which the daily hash cannot. Rejected for now because it is a persistent
identifier in a developer tool: it has to be documented, justified and
kept, and the questions it adds are not the ones being asked today.

**A periodic heartbeat from the update check.** The check already goes to
`api.github.com` every 24 hours with a cache, so routing it through
`kivgraph.dev` would cost almost nothing technically and would yield
monthly actives. Deferred, not refused: it is the difference between a
tool that reports once and a tool that reports forever, and that is a
decision to take on its own evidence rather than as a side effect.

**Modelling the robot baseline instead of pinging.** A workflow could
download each `.mcpb` from a known address and subtract a per-release
automatic baseline. It needs no telemetry at all and produces an estimate
that can never become a count -- and the residual would still mix people
with every directory that is not modelled.

## Limitations

- Anyone behind the same address on the same day is one machine, and
  anyone who opts out is nobody. The reported number is a floor.
- Layer 1 sees a first run, not a user: one person on three machines is
  three, and three people sharing a workstation are one.
- Whether Umami honours the forwarded address is a wire behaviour, not a
  documented contract, and it fails the way the `User-Agent` finding
  failed -- `200`, `{"beep":"boop"}`, nothing written. It is verified
  against the instance on a throwaway property before the endpoint ships,
  and not assumed.
- The historical counts predate the classification, so the series starts
  the day the workflow does. Everything before it is one cumulative number
  per asset, and stays that way.
