# ADR 0122: Skip the directories a tool tags as caches, in every walk

Status: proposed, 2026-11-16. Refines ADR 0097 (what the source digest covers).

Changes the configuration: no.

Changes the schema: no. The manifest keeps its shape; the digest it carries
changes for a repository that holds a tagged directory (see Consequences).

## Context

A pass walks each repository's directory tree several times, and each walk has
its own idea of what to enter:

|walk|skips|
|---|---|
|`freshness.Capture` (inventory digest)|`.git`, `node_modules`, `exclusions`|
|`sourceobservation.TreeDigest` (twice per pass)|`.git`, `node_modules`|
|`DiscoverGo`|`.git`, `vendor`, `node_modules`, `.pnpm`, `.yarn`, `exclusions`|
|`DiscoverTypeScript`|`.git`, `node_modules`, `.pnpm`, `.yarn`, `bower_components`|
|the unclaimed-TypeScript walk|that set plus `dist` and `build`|
|`DiscoverCargo`|`.git`, `target`, `vendor`, `node_modules`|

None of the digests or the Go walk skips a build-output directory, and the
Cargo walk skips `target` by name. The unclaimed walk prunes a list of
conventional output names, which is the one precedent for names, and it is
limited to files nobody claims. So a Rust workspace's `target/` is entered by
five of the six.

`benchmarks/publication-profile` measured what that costs. Six repositories,
`113.525` entries by the digest's own rule (`.git` and `node_modules` skipped),
`105.022` of them in one repository. Under that repository there are two
directories named `target` beside a `Cargo.toml`, `49.976` and `1.926` entries,
and a test-output directory of `49.057`. Excluding the first with the
repository's `exclusions` setting saved `0,9`-`1,1 s` of a `12`-`14 s` pass, all
of it in analysis and none in publication, about `8 %`. A CPU profile put the
directory walks at about `1,5 s` of CPU per pass (`22 %`); it did not say how
much of that is this one directory. Absolute seconds did not reproduce under
load; only the interleaved comparison above is evidence.

The same report records why this is not tuning. `TreeDigest` over-approximates on
purpose (ADR 0097): skipping a source a provider reads could publish a graph that
cannot be reproduced from the sources the manifest names. Skipping a directory is
safe only where **no** provider reads it, and all the walks must agree, because a
file a discovery walk finds and the digest omits is exactly that failure. Two
shortcuts fail that test:

- **Honouring `.gitignore`.** Generated sources are ignored and are read.
- **A default list of names** (`target`, `dist`, `build`, `bin`). A name is not
  evidence: a Go package called `build` or `bin` is source, and the overlay tree
  of the Java and C# loaders was already wrong once for exactly that.

What does assert "this directory is not source" is the directory itself. The
[Cache Directory Tagging Specification](https://bford.info/cachedir/) defines a
`CACHEDIR.TAG` file whose first line is the signature
`Signature: 8a477f597d28d172789f06886806bc55`; a tool writes it into a directory
to say it holds only reproducible cache. Cargo writes one in every `target/` it
creates (the `target/` measured above has it, `fuzz/target/` does not), and so do
pytest, mypy and other tools. It is the tool's claim, made at the moment it makes
the directory, and it needs no list of names.

The Rust provider does not read an in-repository `target/` either: the SCIP run
refuses to run with its target directory inside the workspace
(`internal/rustloader/scip_run.go`, guarded and tested), so generated sources
land outside the tree.

## Decision

1. **A directory that holds a valid `CACHEDIR.TAG` is not entered by any walk
   that decides what a repository's sources are.** That covers the inventory
   digest, the source digest, and the Go, TypeScript, Cargo and unclaimed
   TypeScript discovery walks. They share one predicate, in a package all of them
   already import, so they cannot disagree.
2. **"Valid" is narrow.** The tag is a regular file (an `Lstat` that is not a
   symlink) directly inside the directory, whose first bytes are the
   specification's signature. A missing file, a symlink, a wrong signature, an
   unreadable file or a short read is not a tag, and the directory is walked as
   before. The repository root is never skipped by this rule.
3. **The check costs one `Lstat` per directory entered**, plus a read of at most
   the signature length when the file exists. ADR 0121 measured `11.202` stats at
   `25 ms`; the directories are a fraction of those.
4. **Names are not added.** `exclusions` stays the tool for a directory nothing
   tags, such as the `49.057`-entry test-output directory above, and
   `fuzz/target/`. This decision does not claim to remove the cost of all of
   them.
5. **The watcher is out of scope.** Its ignore set is a separate list with its
   own reason (event volume), and changing it needs its own evidence.

## Alternatives considered

- **Do nothing and document `exclusions`.** It works and was measured. It asks
  every user to find a `target/` that the build tool already labelled, and the
  default stays eight percent slower on Rust workspaces.
- **Skip `target` when a `Cargo.toml` is beside it** (the precedent of `vendor`
  beside a manifest). It also covers `fuzz/target/`, but it is still a name: a Go
  package called `target` next to a `Cargo.toml` is source, and the walk would drop
  it from the digest while the Go loader reads it. The tag has no such case unless
  someone tags a directory of sources.
- **One shared traversal that produces a listing the walks consume.** It removes
  the divergence by construction and cuts the walks from five to one, but it
  changes how six components get their input. This decision is compatible with it
  and is the smaller step: the predicate becomes that traversal's only filter.
- **Cache the listing by directory mtime.** ADR 0121 does this for verification
  only, because it depends on a second observation of the same tree; a pass that
  starts cold gains nothing from it.

## Consequences

- A repository that contains a tagged directory (every built Rust workspace)
  produces a different digest from the one an earlier version stored, because the
  tagged files stop being hashed. [INFERENCE] the pass that follows an upgrade
  reads such a repository as changed and rebuilds once. A repository without a
  tagged directory keeps its digest. The implementation must test the upgrade
  path: a generation published before the change must be reported stale, not
  corrupt, by `graph_status` and `doctor`.
- Analysis loses the entries under tagged directories, and nothing else. A file
  under a tagged directory is no longer an input, so it can no longer reject a
  pass because a build rewrote it between the two observations, which is the
  failure the walk-vanish fix (#54) papered over for a missing entry.
- ADR 0121's stat signature is recorded inside `TreeDigest`, so a skipped
  directory is not stamped either.
- Misuse is possible and has a limit: a tool or a person who tags a directory of
  real sources makes them invisible to the graph. That is the same class as an
  `exclusions` entry, but implicit; `doctor` should say which directories were
  skipped for a tag so that it is never silent.

## Verification

Required before this moves to accepted:

- for each walk, a test that a tagged directory containing a source file is not
  entered, and that the file does not reach the digest or the discovery;
- a test that the walks agree: over a generated tree with tagged, untagged,
  symlinked-tag, wrong-signature and nested tagged directories, the set of source
  files each walk reports is the same, and equals a brute-force walk that applies
  the rule by hand;
- negatives first: no tag, tag as a symlink, signature one byte off, empty tag
  file, unreadable tag, a tag at the repository root, a tag in a directory named
  like source (`src`, `target`);
- the upgrade test above, against a generation written by the previous digest;
- an interleaved pass measurement with the rule on and off and `exclusions` empty,
  on the six-repository corpus, reporting the entries each walk visited. Until
  then the saving is the `0,9`-`1,1 s` of the exclusion measurement, which is the
  upper bound for one directory, not a result of this change.

Not established by this record:

- the number of entries each of the five walks visits per pass (the profile
  counted CPU, not entries);
- the saving on a repository whose `target/` is small, and on Linux;
- whether another tool's tag, such as a Python cache, is worth the same walk
  change. The rule applies to them by construction and has not been measured.
