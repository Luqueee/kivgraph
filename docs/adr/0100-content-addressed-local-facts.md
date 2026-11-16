# ADR 0100: reuse local facts across compatible profiles

- **Status:** Accepted
- **Date:** 2026-09-02
- **Issue:** #151
- **Changes the MCP protocol:** no
- **Changes the persistent graph schema:** no
- **Changes derived state:** yes, the fact-cache layout and entry format
- **Requires a graph rebuild:** no; old fact entries are disposable and are
  discarded by the cache version

## Context

Profiles are independent resolution universes, but they may select the same
worktree. The source and analyzer facts for that worktree do not change merely
because the profile name changed. The previous cache made the profile part of
the unit identity and placed the cache below each profile, so the same unit was
analysed once per profile.

The opposite shortcut is unsafe. A provider selected by one profile can be
absent, ambiguous or replaced in another profile. A cached cross-repository
edge or unresolved result must not cross that boundary. A worktree can also be
dirty or move to a different analyzer configuration between passes.

This ADR supersedes the fact-cache key and placement choice in ADR 0087. The
profile isolation, publication and query decisions in that ADR remain in
force.

## Decision

The cache uses two addresses for a normalized analysis fragment:

1. The local address identifies the source content and configuration inputs of
   the unit. It includes the unit's source identity, language-specific unit
   identity, and the fingerprints of the source trees and relevant manifests.
   The analyzer fingerprint remains a separate compatibility check.
2. The resolution address identifies the effective provider context. Go,
   Rust, semantic languages and TypeScript each use the deterministic provider
   registry that their loader consumes. The profile name is not part of this
   address.

The stored fragment is served only when both addresses, the analyzer
fingerprint, and every recorded input still match. Provider and registry inputs
remain in the recorded input list even though they are not part of the local
address. This means a dependency or registry mutation is still detected by the
same fail-closed validation used by a cold pass.

The cache directory is installation-scoped. Profiles with the same source,
analyzer and effective registry therefore share one content-addressed entry.
Profiles with different provider contexts use different entries and cannot
overwrite one another. A worktree path and its declared `WorktreeID` are both
part of the source identity because normalized facts contain absolute source
paths and cannot be relocated without rewriting them.

Cache reports expose refusal counts by reason, including missing, malformed,
incompatible, analyzer, dependency and registry entries. A normal source or
registry change selects a new address and is reported as `no_entry`; the more
specific reasons describe an entry found at that address but rejected during
validation. A miss is therefore observable without parsing loader diagnostics.
Cache entries remain derived state: a failure to write one never invalidates an
otherwise valid full pass. A full pass stages newly analysed entries in memory
until source verification and the publication gates pass. `Full` commits them
when it returns a valid result; `FullWithRepositories` leaves the commit to the
caller that owns publication. An unpublishable analysis therefore cannot admit
facts under the source address captured before it ran.

## Consequences

- Alternating compatible profiles reuse the same local analysis.
- A registry change gets a separate resolution address, while a provider
  content change is caught by the recorded dependency input.
- A dirty source creates a new local address and leaves the previous entry
  available if that content is restored.
- A source change during analysis cannot leave a cache entry behind when the
  publication-time observation rejects the pass.
- These behaviors are pinned by `TestFactCacheKeepsAlternatingProfilesWarm`,
  `TestFactCacheMissesWhenTheProviderRepositoryChanges` and
  `TestFactCacheMissesWhenASourceFileChanges`; the directed suite reports
  `176 passed in 2 packages` with `go test ./internal/indexer ./internal/config`.
- Existing profile-local fact caches are not migrated into the new format.
  They are retained as user state but are ignored by the new cache version;
  the first pass after this change is cold and does not require a graph
  rebuild.
- The installation-level fact cache is outside the profile graph stores. The
  graph, generation counters and publication locks remain profile-scoped.

## Alternatives rejected

**Keep the profile in the cache key.** This preserves isolation but repeats
identical source analysis for every profile and makes shared worktrees pay for
the same facts indefinitely.

**Key only by source content.** This would reuse profile-dependent provider
selection, replacements, cross-repository edges and unresolved results in the
wrong universe.

**Keep one mutable entry and overwrite it for every profile.** Alternating
profiles would make valid entries evict one another and could serve the last
profile's resolution facts to the next one.

**Cache only the post-merge canonical graph.** The canonical graph is already
profile-scoped published state. Sharing it would bypass the full-pass provider
and integrity checks and would collapse the profile boundary.

## Verification

`internal/indexer` tests cover compatible profile reuse, distinct resolution
contexts, non-overwriting restoration of an older context, dirty source
invalidation, analyzer invalidation, and refusal reason reporting.
`internal/config` tests cover retaining a legacy installation cache at the
installation scope during profile migration.

```bash
go test ./internal/indexer ./internal/config
go vet ./...
go test ./...
```

## Amendment: the worker's identity covers its whole build

The analyzer fingerprint hashed the TypeScript worker's entry script. The worker is
a directory of modules that script imports, and a rebuild that changes only those
leaves the entry's bytes alone, so facts produced by the old worker kept being
served. Measured on the six-repository corpus with a fixed build swapped in place:
the next pass answered `72.723` symbols, the old count, from the cache for `33` of
`39` units.

When the worker is a script in a directory named `dist`, its identity is now the
entry script plus every JavaScript module below that directory, by relative path
and content. Declarations, source maps and compiled tests are left out because the
analysis never loads them. A worker configured as a script elsewhere keeps being
identified by its own bytes: its directory can be anything, and walking it on every
observation would cost more than the cache saves. A worker started through a shim
is identified by the shim and by the executable; a bundle ships both together, so a
release replaces them at once.

Measured with the real binary run from the checkout, so that the worker is
`node ts-worker/dist/facts-cli.js`: swapping the fixed build in place served
`33` of `39` units from the cache before the change and `0` after it, and the
symbol count moved from `72.723` to `44.468`. The next pass with the same build hit
`34` again.

Not covered by that change: what the worker loads from outside its build, the
TypeScript engine among it.

## Amendment: the worker's identity covers its TypeScript engine

The modules of the build import the `typescript` package, which starts the native
executable of the platform package that sits beside it. A `pnpm install` that
changes either leaves every byte of `dist` alone. Measured on a fixture with the
worker copied into a bundle layout and the same command throughout: with the engine
replaced in place by `typescript@7.0.1-rc`, the next pass reported `hits=1` and
`27` symbols from the cache, for a repository the new engine fails to load with
`project ... is not part of snapshot 1`.

When the worker is a script in a directory named `dist`, its identity now also
covers the engine. The `typescript` package is found the way node finds it, by
walking up from the directory that holds `dist`, with links resolved; it is hashed
by relative path and content, leaving out declarations and source maps. Every
package under the `@typescript` directory beside it is hashed whole, because the
executable reads the default library files next to it and this side does not know
which platform node runs on. No `typescript` package, no `@typescript` directory
and an unreadable one are three different values, and none of them is the value of
a worker that has no engine to look for. The hash is of content, so a pnpm checkout
and the copy a bundle carries agree.

With the change, the same swap ran the worker, which failed visibly, and putting the
first engine back hit the cache again. The calculation takes about `15 ms` with a
warm page cache (`26 MB`), three times per pass. A bundle was already covered, as
the engine travels with the executable the fingerprint already hashes; the change
matters for a checkout, where `pnpm install` replaces it independently.

Not covered: a worker outside a `dist` directory, and one started through a shim
that hides its `dist`.
