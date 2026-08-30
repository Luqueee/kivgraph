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

One installation holds one graph. Measured on `devlabs` at generation `000094`:
`53` repositories, `184.315` symbols, `549.970` edges, `476 MB` per generation
with two retained, a `145 MB` snapshot file, `568 MB` of fact cache and `1,1 GB`
of `rust-target`.

Since ADR 0057 there is no incremental path: every pass is a full
reconstruction. A warm pass over that corpus takes `16,9 s` and `35,6 s`; a pass
whose entries went cold takes `178,9 s`. So editing one TypeScript library
reconstructs the Rust and Go repositories nobody touched, and the watcher does
it on every debounce.

Isolation already exists and has no name. `config.stateBesideConfig` relocates
the database, the backups, the fact cache, the synthetic `go.work`, the four
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
directory, and holds a map from profile to `hotsnapshot.SnapshotStore`. Each
store opens on the first query that names its profile, which is what ADR 0067
already does for one: a profile nobody asks about costs nothing.

Three problems disappear with this choice rather than being solved. The `104`
byte ceiling of `daemon.SocketPath` never applies, because there is one socket
at a path that already fits. There is one supervisor unit, not one per profile.
And a client configuration never learns that profiles exist.

The cost is that a tool can no longer capture a store when it registers.
`snapshotStore.Load()` runs inside every handler over a value bound at
registration, across `37` of the `48` `Register*` functions in
`internal/mcp/tools`. That parameter becomes a resolver from profile to
snapshot. This is the bulk of the work and it is mechanical.

### The profile is an argument, never a tool and never session state

The resident surface is `len(name)*2 + len(description)` per tool against
`MaximumResidentSurfaceBytes`. Measured over the served catalogue, the eleven
query tools spend `1.864` of `1.900`, and `index_project` spends `213` of the
`236` its own line allows. Both budgets sit at `98 %`.

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

Discovery is paid where routing already lives and where the ceiling does not
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
pass ever hits: the measured `16,9 s` becomes the measured `178,9 s`, for every
profile, forever.

`unitIdentity` gains the profile name. Keying by the registry fingerprint would
share more entries, but the number of entries would stop being bounded by
anything; by profile name the cost is units times profiles, and `prune` already
exists. A shared repository is analysed once per profile, which is irreducible.

The four analyzer target directories -- `rust-target` at `1,1 GB`, and the Java,
C# and Go synthetic files -- do not depend on the registry and stay shared per
machine. `generations`, `CURRENT`, the publish lock and `factcache` move under
the profile.

### A multi-profile query is a declared union

A query may name several profiles. Three things follow.

`Cursor` pins one `SnapshotID` and rejects a cursor from another. A page across
profiles is a vector of positions plus a digest of the profile set, and a cursor
whose set changed is rejected the way one from another generation already is.
The order of profiles within a page is canonical, never the order of the
request, or two equivalent calls paginate differently.

The envelope's `snapshot_id` stops being a scalar and becomes one entry per
profile carrying its own generation and completeness. The merged
`completeness` verdict is the **weakest** of the profiles, never a sum:
`COMPLETE` only when every profile said `COMPLETE`.

A `StableKey` is a BLAKE3 digest over language, package, module, qualified name
and discriminator, and is documented as independent of snapshot ids and source
locations. Under overlap the same declaration in two profiles produces a byte
identical key in two snapshots. That is the deduplicator: two rows with one key
are one declaration, returned once, declaring the profiles it was found in. It
is also the hazard: a stable key arriving as an **input** no longer names a
snapshot, so a call that carries one and no profile resolves in the default and
must say so.

Rows carry `profile` only when more than one profile was asked for. One profile
costs nothing, which is the rule `view: "files"` already follows.

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

The `53` repositories of an existing installation become the profile `default`
by moving `generations/` and `CURRENT` under `profiles/default/`, with the
pointer set to `default`. No pass runs, no command grows a flag, and no call
grows an argument: the installation answers afterwards exactly as it did
before.
An upgrade that costs `178,9 s` is the difference between this being adopted and
not.

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
- Disk grows with profiles for the fact cache and for retained generations, and
  does not grow for the `1,1 GB` of analyzer output.
- Memory improves in the common case and does not regress in the worst. A
  profile nobody queries is not mapped; querying every profile maps roughly what
  the single graph maps today.
- The watcher's blast radius becomes the profile rather than the installation.
- Three response shapes change: `snapshot_id`, the completeness verdict and the
  optional per-row `profile`. That is an MCP compatibility surface and it moves
  the row format version.
- A profile name is an identifier compared exactly, like a repository name, and
  is bounded so it can appear in a path.

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

Nothing is implemented. An implementation has to prove, at least:

```bash
go test ./internal/mcp/... ./internal/daemon ./internal/indexer ./internal/config
```

- That the resident surface still fits. `TestServerSurfaceStaysCheapToKeepResident`
  passes unchanged, because a `profile` argument is schema and not description,
  and `TestEveryPublishedArgumentDescribesItself` covers the new argument.
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
