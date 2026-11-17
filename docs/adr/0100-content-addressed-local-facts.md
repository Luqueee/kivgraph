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

The walk starts from an absolute path. The source watchers observe a worker through
the relative path the configuration or the checkout rule gives them and a pass
through an absolute one; a relative walk stops at the working directory, so an
engine above it, in a monorepo root, gave the two observations different identities
and left the profile stale after every publication.

Not covered: a worker outside a `dist` directory, and one started through a shim
that hides its `dist`.

## Amendment: the toolchain probes do not depend on the working directory

`go env` and the `rustc`, `cargo` and sysroot probes of the fingerprint ran in
the directory the process started in. The go command switches to the toolchain a
`go.mod` or `go.work` names, and rustup's proxies to the one a
`rust-toolchain.toml` names, both from the working directory, so the fingerprint
described that directory's pin and not the machine's toolchain. Measured with a
`go1.26.11` base: `go env GOVERSION GOROOT` from a module that requires
`go 1.27.4` answered `go1.27.4` with another `GOROOT`, and downloaded the
toolchain to answer. A `rustc` that answers by directory, as the proxy does, gave
a different Rust fingerprint from a directory with a pin; rustup itself was not
available to measure. Two processes started in different places gave one machine
two identities.

The probes now run from the temporary directory, which pins nothing, so they
answer with the default toolchain whatever started the process. When that
directory is not absolute or not usable the probes keep the process directory, as
before. A repository's own pin is not asked: the toolchain a repository selects
is a change of `go.mod`, `go.work` or `rust-toolchain`, which the tree of its unit
records. `rust-toolchain` and `rust-toolchain.toml` were not analyzed sources, so
editing the pin of a repository changed no digest; they are now, and the units of
a repository that has one are rebuilt once.

Not covered: an update of the toolchain a repository selects without touching
its files, such as `rustup update` on a pinned `stable` channel, when the default
toolchain of the machine is another one.

## Amendment: a refused entry says which part of the analyzer moved

A difference in the analyzer fingerprint refused every entry and said nothing
about where it was. The fingerprint is now built in parts -- `resolver`, `binary`,
`go`, `typescript`, `python`, `dart`, `java`, `csharp` and `rust` -- and each
entry records the hash of each part, truncated to `16` digits, as
`analyzer_parts`. The value is the hash of the same bytes in the same order it
always was, so no entry is invalidated and the entry version stays. The parts are
a diagnosis: an entry is served or refused by the value alone.

When an entry is refused because the analyzer changed, the pass counts the parts
that differ between what the entry recorded and what the pass has, and
`index --full` appends them to the `index.cache` line, as in
`analyzer_changed=rust:34`. An entry that differs in several parts counts in each.
An entry written before the parts were kept, or a value that this process did not
compute, counts as `unrecorded`. A process remembers the parts of the last eight
fingerprints it computed, by value, so the cache can name them without computing
the fingerprint again.

Measured on the six-repository corpus, which has no Rust repository: with a `PATH`
that has `rustc` and `cargo` and then one that has neither, the pass reports
`hits=0 misses=39` and `analyzer_changed=rust:34`, and again on the way back. The
cost of the switch, `19.8` to `20.8 s` against `7.0 s` warm, is what a
per-language fingerprint or an analyzer-addressed entry would have to remove; the
line is what tells which of them is worth building.

## Amendment: the cache keeps the newest entries of each unit

The address of an entry holds the content of its unit, so every edit writes a new
entry and leaves the one it supersedes, and only age, thirty days after it was
last used, expired it. Measured on the six-repository corpus, editing one
TypeScript source file added two entries and `105` to `115 MB` each time: `213 MB`
became `757 MB` after five edits.

After a pass admits entries, the cache now keeps the newest three entries of each
unit and removes the rest. Newest is the last time an entry was written or served,
which a hit sets, so an old entry that was just served outranks a newer one that
nobody asked for. A unit is its identity including the resolution it was analysed
in, so profiles that share a directory do not spend each other's entries. The
unit is read from the first `4 KiB` of an entry, which holds the version and the
unit before the facts; an entry whose unit is not there is neither counted nor
removed and is left to the age rule, as is anything the cache did not write. The
entries a pass has just admitted are never removed by it, and a pass that admitted
nothing reads no headers. Three is the one in use, the one before it and one more,
which is what going back to another branch or reverting an edit needs. It is a
constant and not a configuration key.

With the change the same five edits leave `38` entries and `442 MB`, where they
left `44` and `757 MB`, and the passes take the same time. Reverting the file to
the content it had before the five edits is now a miss, where it was a hit, because
that entry is beyond the three kept: `14.1 s` instead of `9.2 s` on this corpus.
Reading the headers of `5000` entries took `191 ms`, and of the corpus' `44` large
ones `6 ms`.

## Amendment: the cache caps the entries of a unit across resolutions

The identity of a unit includes its resolution, the map of providers it was
analysed against, and the TypeScript registry includes the version of every
package. Raising the version of one package gave every TypeScript unit a new
identity, and the rule of the previous amendment counts an identity apart, so each
old identity kept its entries until age expired them. On the six-repository corpus
each version bump added `25` entries and about `164 MB`: `433 MB` became `923 MB`
after three bumps, with the same `34` units.

After a pass admits entries, the cache now also keeps at most six entries of each
unit, the unit being the identity without its resolution, counted across all of
its resolutions and newest first. The rule of three per identity applies first.
Six are two or three resolutions that are in use together, as the profiles that
share a directory are, with two contents each. The cap is never below what one
identity keeps, and the entries a pass has just admitted are not removed by it.

With the change eight version bumps of the same package leave `161` entries and
`1.11 GB`, a level the cache reached after the fifth bump, where the unbounded
growth was `25` entries and `164 MB` per bump. The price is that going back to a
version older than the six most recent resolutions is a miss: restoring the
original version after eight bumps took `20.4 s` where, after three bumps and
without the cap, it was a hit at `7.8 s`.

## Amendment: a Dart unit takes the trees its package config reaches

The semantic registry digests the tree of every repository that declares a semantic
language, which is what stops a Dart consumer from being served facts its provider
has outgrown: the loader is handed every registered repository as a provider, reads
the file a reference lands in, and the facts of the consumer name its symbols. The
registry knows only the repositories of a semantic language, so a provider
registered under another one could change, or be registered after the consumer was
analysed, without the consumer's entry noticing. Measured with the provider
registered as Rust, after it renamed `addOne` to `addTwo` the consumer was served
with `addOne`; and with the provider registered after the consumer, the consumer
kept no destination into it.

A Dart unit now also takes among its inputs the tree of every registered repository
that holds a package its package config names, which is all the analysis server can
open besides the repository itself, whatever language it was registered under, and
that of every repository its facts name a target in. The first are known before the
unit is analysed and are part of its address, so a provider that is registered is
another address; the second are recorded with the entry and checked when it is
served. A directory that only shares its first characters with a repository's root
is not inside it, and a package reached through a link is the repository it points
into.

Measured on a consumer, a provider registered as Rust and an unrelated Go
repository: editing the Go repository still serves the consumer (`hits=2 misses=1`),
renaming the symbol analyses it again and it sees `addTwo` (`hits=2 misses=1`), and
registering the provider afterwards analyses it again and it sees `addOne`.
Registering a digest of every repository in the registry instead closes the gap too,
and analyses the three units again on the edit of the unrelated one (`hits=0
misses=3`). A consumer that imports a file of another repository by a relative path
that leaves its package got no resolved reference from the analysis server
(`references=0`), so nothing depends on it; that may change with another SDK.

The digest of the registry is not removed. A Python unit with pyright, a Java
and a C# unit can read a sibling through the environment, which nothing here
can see. For Dart alone, the registry could name only the repositories and
leave their content to these inputs, which on a consumer, its provider and an
unrelated Dart repository left the unrelated edit analysing one unit instead
of three; that is not done here.
