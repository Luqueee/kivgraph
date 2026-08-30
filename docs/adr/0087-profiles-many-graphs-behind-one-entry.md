# ADR 0087: profiles, many graphs behind one entry

- **Status:** proposed
- **Date:** 2026-08-30
- **Changes the MCP protocol:** yes
- **Changes the persistent schema:** no, the canonical schema is untouched; the
  state layout and the fact-cache key change
- **Requires a rebuild:** no, the published generation migrates into a default
  profile
- **Changes the CLI surface:** yes
- **Relaxes a root contract:** yes, the closed-world claim becomes
  profile-scoped. See "The claim that changes".

## Context

One installation holds one graph. Every figure below is from one installation on
`devlabs`, whose corpus is the `53` repositories its `repositories.yaml`
registered at generation `000094` -- the `kena-workspace` monorepo across Go,
TypeScript and Rust, plus this repository. Two commands produced all of it:

```bash
du -sh ~/.local/state/kivgraph/*        # sizes
grep 'index --full finished' ~/.local/state/kivgraph/events.jsonl
```

Counts, from `graph_status` at that generation: `53` repositories, `184,315`
symbols, `549,970` edges. Sizes: `476 MB` per generation with two retained, a
`145 MB` snapshot file, `568 MB` of fact cache, `1.1 GB` of `rust-target`.

Since ADR 0057 there is no incremental path: every pass is a full
reconstruction. The three passes the event log holds for that corpus took
`35.6 s` and `16.9 s` warm, and `178.9 s` for the one whose entries went cold --
generations `000092`, `000094` and `000093` respectively. So editing one
TypeScript library reconstructs the Rust and Go repositories nobody touched, and
the watcher does it on every debounce.

Both figures are one machine and one corpus, not a benchmark. They are enough to
justify a shape and not enough to promise a speedup, which is why no section
below claims one.

Isolation already exists and has no name. `config.stateBesideConfig` relocates
the database, the backups, the fact cache, the synthetic `go.work`, the three
analyzer target directories, the event log and the registry for any
configuration written outside the default location, and `internal/daemon`
already keys a daemon by its state directory. What is missing is that a person
can name one, list them, switch between them, and reach them from one client
entry. The gap is visible on disk: `~/.config/kivgraph/` carries a
`repositories.yaml.pre-workspace-switch`, which is this feature done by hand and
without a record of which corpus answered.

The name `workspace` is not available. `internal/workspace` is the repository
registry and its discovery, and `go.work`, Cargo and pnpm each call something
else by that word. A fourth meaning in one binary is a permanent reading hazard.

## Decision

A **profile** is a named repository registry together with the state derived
from it. Profiles are the unit of indexing, publication and query scope.

### One entry, one daemon, many stores

`mcp install` keeps writing one entry, unchanged, and it stays `kivgraph serve`.
One daemon owns the socket, the token and the endpoint at the default state
directory, and holds a map from profile to `hotsnapshot.SnapshotStore`.

A store opens on the first **operation** that names its profile, not on the
first query. Deferring to queries alone would leave `index_project` and
`index --full` with nothing to register or publish into for a profile that has
never been asked about, which is exactly the profile a first indexing run
targets. Indexing, watching and the CLI open the store the same way a query
does, and a profile that no operation names is what costs nothing -- which is
what ADR 0067 already does for one.

Two problems disappear with this choice rather than being solved. There is one
supervisor unit, not one per profile. And a client configuration never learns
that profiles exist.

The `104` byte ceiling of `daemon.SocketPath` is **not** one of them, and this
ADR does not touch it. Profiles stop multiplying socket paths, because there is
one socket for the installation, but `--config` still relocates a whole state
directory and a socket under it can still be too long. The check stays where
ADR 0065 put it.

The cost is that a tool can no longer capture a store when it registers.
`snapshotStore.Load()` runs inside every handler over a value bound at
registration, across `37` of the `48` `Register*` functions in
`internal/mcp/tools`. That parameter becomes a resolver from profile to
snapshot. This is the bulk of the work and it is mechanical.

### The profile is an argument, never a tool and never session state

The resident surface is `len(name)*2 + len(description)` per tool against
`MaximumResidentSurfaceBytes`, which is the formula
`TestServerSurfaceStaysCheapToKeepResident` applies. Summing it over the eleven
tools `registerQueryTools` registers, at commit `6f37f4a`, the query catalogue
spends `1,864` of `1,900` -- `98.1 %` -- and `index_project` spends `213` of
the `236` its own line allows -- `90.3 %`. The two budgets are separate lines
and neither is close to its own ceiling by accident: the query catalogue is the
tighter of the two.

A schema is not resident: neither target host keeps it. So `profile` on all
eleven tools and on `index_project` costs **zero** resident bytes, and two new
tools would cost about `270` and force the ceiling up by `14 %` for every
session of every user, including the ones with a single profile.

The ceiling is a chosen regression guard and could be raised. It is not raised
here because the two tools that would be added already have owners:

- `list_profiles` asks what `graph_status` and `list_repositories` already
  answer. Profiles become fields of those **responses**, which cost only when
  someone calls them.
- `use_profile` is refused on a stronger ground than its bytes. Every query tool
  annotates `readOnlyClosedWorld()`, which asserts that an answer comes from the
  published generation and nothing else. Session state makes the same call with
  the same arguments answer differently depending on what happened earlier, and
  it fails invisibly: an agent whose context was compacted keeps querying the
  wrong corpus, and under overlap the answer is not empty, only incomplete.

Managing profiles needs no tool either. `index_project` already registers
projects behind a consent gate and gains `profile` regardless, so indexing into
a name that does not exist yet is how a profile is created. Removing one is CLI,
like `clean`.

Rejecting `list_profiles` only works if something else enumerates, so this is
stated rather than implied: **`graph_status` and `list_repositories` return
every profile, not only the one that answered.** `graph_status` gains a
`profiles` array -- name, published generation, repository count, and which one
the pointer names -- and `list_repositories` gains a `profile` column and
returns the repositories of all of them. Both take an optional `profile` to
narrow. A client that knows no name calls either and has them all.

Both follow the same rule as every other response: with one profile in the
installation the `profiles` array and the `profile` column are **absent**, and
the two answers are what they are today. There is nothing to enumerate when
there is one, and the single-profile envelope has to stay byte-identical or
the compatibility promise below is not one.

That is the whole discovery path, and it costs no resident bytes because it
lives in responses. The rest of the routing is paid where the ceiling does not
apply: one generic sentence in the handshake `instructions`, which may not name
or count profiles because a volatile fact there rewrites a cached system prompt;
the published `SKILL.md`; and `guidance` on an empty answer whose repository
also lives in another profile.

### A repository may belong to more than one profile

Overlap is allowed. It is not free, and the price is paid in one place.

A fact-cache entry is keyed by `unitIdentity`, which is kind, repository name
and module, with no discriminator for the registry it was produced under. The
composition of the registry is a genuine input to the analysis: `goRegistryName`
answers "does this reference leave the repository", so the same repository
analysed beside different siblings produces different facts, an edge in one case
and an `UNRESOLVED` in the other. Two profiles sharing a repository therefore
write the same key with different fingerprints and overwrite each other, and no
pass ever hits: the measured `16.9 s` becomes the measured `178.9 s`, for every
profile, forever.

`unitIdentity` gains the profile name. Keying by the registry fingerprint would
share more entries, but the number of entries would stop being bounded by
anything; by profile name the cost is units times profiles, and `prune` already
exists. A shared repository is analysed once per profile, which is irreducible.

### What is shared, and what a shared thing costs

The synthetic `go.work` is **not** shared. It is built from the module set of
the pass, which is the registry, and `stateBesideConfig` already relocates
`go.synthetic_work_file` with the rest of the state. It moves under the profile.

The three analyzer output directories -- `rust-target` at `1.1 GB`, `java-target`
and `csharp-target` -- do not depend on the registry, and only they stay shared
per machine. That sharing is what makes profiles affordable, and it is also the
one place where two profiles can corrupt each other, so it needs a lock it does
not have today: `indexing.Service.gate` is process-local and the publish lock is
per profile, so two passes over two profiles are serialised by neither. Sharing
is therefore conditional on a cross-profile lock over the shared targets, taken
for the length of a pass and named in the failure when it is not free.

If that lock proves to cost more than the disk it saves, the fallback is
per-profile targets: `1.1 GB` per profile, and no coordination.

`generations`, `CURRENT`, `BACKUP`, the publish lock, `backups`, `factcache`,
the registry and the synthetic `go.work` move under the profile.

### A multi-profile query is a declared union

A query may name several profiles. Three things follow.

`Cursor` pins one `SnapshotID` and rejects a cursor from another. A page across
profiles carries a position **and a `SnapshotID` per profile**, plus a digest of
the profile set, and is rejected when the set changed or when **any** profile
published since. Carrying only the set digest would be a regression on what the
scalar cursor already guarantees: profiles publish independently, so a second
page would silently mix rows from two generations of the same profile. The order
of profiles within a page is canonical, never the order of the request, or two
equivalent calls paginate differently.

The envelope's `snapshot_id` stops being a scalar and becomes one entry per
profile carrying its own generation and completeness. The merged
`completeness` verdict is the **weakest** of the profiles, never a sum:
`COMPLETE` only when every profile said `COMPLETE`.

### The two envelope shapes, written out

The compatibility promise and the shape change are only compatible if both are
stated exactly, so both are.

**One profile in the installation.** The envelope is what it is today, byte for
byte: `snapshot_id` is the scalar it has always been, and there is no `profile`
field in the envelope or on any row. There is one corpus, so there is nothing to
scope and no declaration to make.

**More than one profile in the installation, one named in the query.**
`snapshot_id` stays scalar and the envelope gains `profile`. Rows carry no
`profile`, because they all came from the one named.

**More than one named in the query.** `snapshot_id` is replaced by `profiles`,
one entry per profile with its generation and completeness; the envelope carries
the merged verdict and `cross_profile_edges`; rows carry `profile`.

The second and third shapes are a row-format version bump. The first is not a
new shape at all, which is the whole point: an installation that never creates a
second profile never sees a changed response.

A `StableKey` is a BLAKE3 digest over language, package, module, qualified name
and discriminator, and is documented as independent of snapshot ids and source
locations. Under overlap the same declaration in two profiles produces a byte
identical key in two snapshots. That is the deduplicator: two rows with one key
are one declaration, returned once, declaring the profiles it was found in. It
is also the hazard, and it is not answered by declaring it. A stable key
arriving as an **input** no longer names a snapshot, and resolving it through
the default pointer means a key handed out before the pointer moved resolves
later against a different graph -- different symbol data, or `not found`, with
nothing wrong on either side. So once a second profile exists, a call that
carries a stable key **requires** `profile`; it is refused rather than resolved
by a pointer that is allowed to move. With one profile the requirement does not
exist, like every other part of this.

When one key is reached through several profiles with **different** payloads --
the same declaration at two commits -- the row is not merged. Each profile's
payload is its own row, both declaring their profile, because merging would
invent a symbol that exists in neither generation.

Rows carry `profile` only when more than one profile was asked for. One profile
costs nothing, which is the rule `view: "files"` already follows.

Deduplication crosses pages or it is not deduplication. A key emitted on page
one has to stay suppressed on page two, so the set of emitted keys travels in
the cursor rather than living for the length of one response -- otherwise a
declaration reached through two profiles appears once on a page and again on
the next, and `total` describes neither.

What travels is not the bare key: it is the pair of the key and a digest of the
payload emitted for it. Two rows can share a key and still differ -- the same
declaration reached through profiles at different generations is exactly the
case above that refuses to merge them. Suppressing on the bare key would
silently drop the second variant across a page boundary, which is a worse
defect than the one this is meant to fix: it would look like deduplication and
be data loss. Both variants are emitted, each on the page that first reaches
it, and each pair reappears in the set exactly once. The set is bounded by the
page size times the pages taken.

Which also means the merge needs a global order across profiles, not a merge
of independently ordered pages. The order is the canonical profile order and
then each profile's own, so the position a cursor stores means the same thing
on the next call.

### The union is not a join

Asking two profiles returns two independent answers side by side. The edges
between them are not withheld; they do not exist, because no pass ever resolved
over both registries. `find_cross_repo_consumers` across two profiles cannot
find a consumer in one of a provider in the other.

So a response to a multi-profile query always carries
`cross_profile_edges: "not_resolved"`. Not through `guidance`, which speaks only
when a count misleads: here what misleads is the operation, so the declaration
is unconditional.

### There is always a default profile, and it is ordinary

`init` creates a profile named `default`, and a configuration key names which
profile is the default, initialised to `default`. An installation is never in a
state with no profiles and never in a state where the pointer names one that
does not exist: `profile remove` refuses the profile the pointer names until it
is repointed, and refuses to leave zero.

Every operation that takes a profile and is not given one uses that pointer.
`index --full` indexes it, `index_project` registers into it, the watcher
watches it, and a query answers from it.

`default` is a name and not a keyword. It is addressable like any other --
`--profile default`, `profile: "default"` -- it appears in the `graph_status`
and `list_repositories` responses beside the rest, and nothing special-cases it.
The pointer can be moved to another profile, at which point `default` is simply
a profile that is no longer the default. Two mechanisms, so that "which profile
is used when none is named" stays a question with an answer even after somebody
renames or repoints things.

The consequence that matters most: **profiles are invisible until a second one
exists.** After the migration below, an installation with one profile behaves
exactly as it does today -- no flag on any command, no argument on any call --
and the surface argument above holds, because the cost of the feature is zero
for the people who never adopt it. That extends to the envelope: the `profile`
field is omitted from a response when the installation has exactly one profile,
because with one corpus there is nothing to scope and the declaration would be
fifteen bytes of noise on every call.

Absent `profile`, a query answers from one profile and never from all of them.
`profile: ["*"]` exists and is never the default. This is what decides whether
the whole design is worth building: if the normal case is querying every profile
at once, the partition has been paid for -- shared repositories analysed once per
profile, fact cache multiplied, cross-profile edges gone -- to receive the union
of the parts, which is strictly worse than today's single graph.

### Migration

The `53` repositories of an existing installation become the profile `default`,
with the pointer set to it. No pass runs, no command grows a flag, and no call
grows an argument: the installation answers afterwards exactly as it did before.
The alternative -- migrating by reindexing into the new layout -- would cost the
measured `178.9 s` cold pass on every installation that upgrades, for no reason
a user would recognise as their own; that avoided cost is what makes a no-pass
migration worth building rather than a convenience.

Every artifact the decision scopes to a profile moves, and the list is the
migration rather than an example of it: `generations/`, `CURRENT`, `BACKUP`,
`backups/`, `publish.lock`, `resync.lock`, `factcache/`, the synthetic
`go.work` and `repositories.yaml`. Anything left above
`profiles/default/` is state the default profile would then not find, and the
cost of not finding `factcache` is the difference between the measured
`16.9 s` and the measured `178.9 s` on the next pass.

What stays above `profiles/` is the installation, not a profile: the socket,
the token, the endpoint, `daemon.port`, the event log and the three shared
analyzer target directories.

The move is one transaction or it does not happen, because a half-migrated
state directory has a `CURRENT` pointing at generations no longer beside it.
Renaming every artifact straight into `profiles/default/` would leave exactly
that half-migrated state if the process died between two of them, so nothing
of the old layout is touched until the new one is proven to work.

The new layout is built first, entirely alongside the old, under a temporary
name -- copied, not moved, so the old layout is untouched while this runs no
matter how long it takes or how it fails. It is then validated: every named
artifact present, and the migrated `CURRENT` naming a generation that opens.
Only then do two renames run, and only the second one is allowed to fail
loudly: the old top-level layout is renamed to a retained backup name, and the
validated temporary layout is renamed into `profiles/default/`. Both are
renames within the same state directory, so each is atomic on its own; the
gap between them is where a crash is possible, and it leaves the backup
present and `profiles/default/` absent, which is a state a restart recognises
and completes rather than one that mismatches `CURRENT` against its
generations.

The daemon's own startup is the second validation, not a formality: it opens
the migrated store before anything depends on it, and a failure there rolls
back by renaming the backup over `profiles/default/` again. The backup is
retained -- not deleted -- until a startup succeeds against the new layout, so
rollback is always a rename and never a reconstruction. The migration refuses
to start while a daemon holds the socket, rather than racing it.

## The claim that changes

`readOnlyClosedWorld()` and the routing table promise that an empty reference
list means nobody calls it, and that this is what `grep` cannot say. With
profiles that claim becomes true of a corpus rather than of a machine: zero
references in `oss` is not zero references on disk.

The claim is scoped rather than weakened, and three declarations do the scoping.
Every response carries the profile it answered from. A multi-profile response
carries `cross_profile_edges: "not_resolved"`. And an empty answer whose symbol's
repository also lives in another profile gets a `guidance` sentence naming it,
which is exactly the case that comment describes: an empty answer read as "no
such thing" is the moment a session defects.

Without all three, overlap converts an honest absence into false certainty,
which is the one outcome this surface refuses everywhere else.

## Consequences

- The state layout gains a level. `~/.local/state/kivgraph/profiles/<name>/`
  holds generations, `CURRENT`, the publish lock and the fact cache; the socket,
  the token, the endpoint and the analyzer target directories stay above it.
- Disk grows with profiles for the fact cache and for retained generations. The
  `1.1 GB` of analyzer output does not grow **while the shared targets keep
  their cross-profile lock**; if that lock is dropped for the per-profile
  fallback, it grows by `1.1 GB` per profile like everything else.
- Memory improves in the common case and is bounded rather than free in the
  worst. A profile no operation names is not mapped, and mapping every profile
  costs roughly what the single graph costs today -- but only because the
  profiles partition one corpus. Nothing stops someone creating twenty
  overlapping profiles, so a store is not held forever: the daemon retains a
  bounded number of open stores and closes the least recently used, which is
  safe because a mapping is released by the unreachability of its
  `*GraphSnapshot` and never by a `Close` that a reader could outlive. The
  bound is configuration, and the default is small.
- The watcher's blast radius becomes the profile rather than the installation.
- Three response shapes change: `snapshot_id`, the completeness verdict and the
  optional per-row `profile`. That is an MCP compatibility surface and it moves
  the row format version.
- A profile name is an identifier compared exactly, like a repository name, and
  is bounded so it can appear in a path. `*` is reserved: it is the all-profiles
  selector, so a profile literally named `*` would be either unreachable or
  ambiguous, and the registry refuses it at creation rather than defining an
  escape.

## Alternatives rejected

**One daemon per profile, one client entry per profile.** Rejected by the
constraint that profiles are reached through a single MCP entry, and separately
by three costs it carries: `daemon.SocketPath` refuses a path of `104` bytes or
more, so a long profile name fails at bind; `daemon install` writes one
supervisor unit per profile; and every client file lists every profile.

**Profiles as views over one graph.** A repository would carry group labels and
tools would filter by a set. It preserves every cross-repository edge and costs
no disk, and it answers none of the question: there is still one graph of
`476 MB` per generation whose every pass reconstructs `53` repositories.

**A `workspace_id` column in the canonical schema.** One store, one snapshot,
publication per profile. It touches schema `002`, `canonical_integrity.go`, the
snapshot row format and `SnapshotStore.Publish`, whose contract that a
generation is strictly newer stops being expressible as a scalar. No measurement
taken here asks for it. It is the right shape only if the goal becomes
multi-tenancy rather than isolation.

**Session state through `use_profile`.** Covered above: it is refused for
breaking the closed-world annotation, not for its bytes.

## Verification

Nothing is implemented, so nothing here reports a result. These are the
acceptance criteria an implementation has to meet, and the ADR gets its
verification section filled in with recorded output when it does:

```bash
go test ./internal/mcp/... ./internal/daemon ./internal/indexer ./internal/config
```

- That the resident surface still fits, under
  `TestServerSurfaceStaysCheapToKeepResident` unchanged -- the expectation being
  that a `profile` argument is schema and not description, and so costs nothing
  it measures -- and that `TestEveryPublishedArgumentDescribesItself` covers the
  new argument.
- That two profiles indexing concurrently cannot corrupt the shared analyzer
  targets, with a test that runs two passes over two profiles at once. Today
  neither `indexing.Service.gate`, which is process-local, nor the publish lock,
  which is per profile, would serialise them.
- That a page taken across profiles is refused when any one of those profiles
  publishes between pages, not only when the profile set changes.
- That a stable key without a profile is refused once a second profile exists,
  and accepted while there is one.
- That two profiles sharing a repository both stay warm across alternating
  passes. This is the regression the fact-cache key exists to prevent, and it is
  invisible to any test that runs one profile.
- That a cursor from one profile set is refused by another, the way
  `cursor.go` already refuses one from another generation.
- That a multi-profile response carries `cross_profile_edges` unconditionally
  and that its completeness verdict is the weakest of its profiles, with a test
  that pairs a `COMPLETE` profile with a `LOWER_BOUND` one.
- That one declaration reachable through two profiles returns one row, and that
  `total` counts what the response contains.
- That the migration moves an existing generation into `default` without
  running a pass, and that a daemon started afterwards serves it.
- That an installation with one profile is byte-identical to today on the
  surface: no `profile` field in any envelope, and every command and call
  behaving as it does now. This is the test that keeps the feature free for
  whoever never adopts it.
- That the pointer cannot be left dangling: `profile remove` refuses the
  profile the pointer names and refuses to leave zero profiles, and a
  configuration naming a profile that does not exist fails to load rather than
  silently falling back.
- That an empty answer whose repository lives in another profile says so.
