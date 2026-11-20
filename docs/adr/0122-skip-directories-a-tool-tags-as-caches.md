# ADR 0122: Skip the directories a tool tags as caches, in every walk

Status: accepted, 2026-11-16, implemented. Refines ADR 0097 (what the source
digest covers).

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
five of the six. `benchmarks/walk-entries` counted them in a pass: `803.626`
entries examined without the rule and `446.857` with it, seven examinations per
entry in the tree either way, and one more walk than the table lists
(`indexer.countSourceFiles`, unaffected by the rule).

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
  tagged files stop being hashed. The pass that follows an upgrade reads such a
  repository as changed and rebuilds once: `freshness.Check` reports the earlier
  generation `stale`, tested below. A repository without a tagged directory keeps
  its digest.
- Analysis loses the entries under tagged directories, and nothing else. A file
  under a tagged directory is no longer an input, so it can no longer reject a
  pass because a build rewrote it between the two observations, which is the
  failure the walk-vanish fix (#54) papered over for a missing entry.
- ADR 0121's stat signature is recorded inside `TreeDigest`. A skipped directory
  is still stamped, so a tag that appears or goes is a change the signature sees.
- Misuse is possible and has a limit: a tool or a person who tags a directory of
  real sources makes them invisible to the graph. That is the same class as an
  `exclusions` entry, but implicit; `doctor` names every skipped directory.

## Implementation

`workspace.IsCacheDirectory` is the one predicate. It is asked by the inventory
walk (`freshness.Capture`), `TreeDigest`, and the Go, TypeScript and unclaimed
TypeScript discovery walks, through their existing `isDiscoveryExcluded` helpers.
Two deviations from the decision as proposed, both on purpose:

- **`DiscoverCargo` is unchanged.** It already skips `target` by name, which is
  what a Cargo tag marks, and its path-only companion `CargoExcludes` cannot ask
  the filesystem. Tagging another directory inside a Rust tree is not covered.
- **The TypeScript glob expansion is unchanged.** `walkTypeScriptGlobDirectory`
  implements a project's own `include` semantics, the compiler's. A project whose
  `include` reaches into a tagged directory still claims it while the digest does
  not hash it. That needs a TypeScript source inside a directory a tool tagged as
  a cache, which no tool the corpus holds produces; it is the same limit as
  tagging a directory of sources.

`TreeDigest` stamps a tagged directory before it skips it, so ADR 0121's
signature notices a tag that appears or goes and stops vouching for a digest taken
under the other answer. `doctor` lists the skipped directories as `walk.cache_tag`:
per repository, relative to its root, at most five each with the count of the rest,
and `no directory carries a cache tag` when there are none. It walks each
registered tree once, which took under a second for the six-repository corpus in
a whole `doctor` run. A tree it cannot walk is a failure, not an empty list.

## Verification

Done, with the tests in `internal/workspace/cachedir_test.go`,
`internal/freshness/freshness_cachetag_test.go` and
`internal/sourceobservation/cachetag_test.go` and `cachetag_signature_test.go`,
over the fixture `testsupport.WriteCacheTagTree`:

- the predicate against no tag, a tag a byte off or a byte short, an empty tag, a
  directory named like a tag, an absent directory, a file, a tag as a symlink, an
  unreadable tag, and a tag with text after the signature;
- for each walk, the result over a tree with tagged directories equals the result
  after deleting those directories by hand, and directories without a valid tag
  are all still walked. Each of those tests fails when the rule is switched off;
- the control: with the tags removed the same directories are reached;
- a tag at the root of a walk does not hide the tree;
- the upgrade: a generation stored with the digest of the tree before it was
  tagged is reported `stale` with the ordinary changed-inventory detail by
  `freshness.Check`, never `unavailable` or `unverified`, and `fresh` before the
  tag appears. The inference in Consequences holds at that level; `graph_status`
  and `doctor` were not run against a published generation;
- a tag that appears or goes after the capture makes the stat signature refuse to
  vouch. Moving the skip before the stamping makes that test fail.

Measured, in `benchmarks/cache-tag-walks/report.md`: six repositories, one
tagged `target/` of `49.976` entries, three arms interleaved, `24` passes each. The
pass after an edit is `14,12 s` with the rule and `16,91 s` without (`-2,79 s`,
`-16,5 %`), against `15,14 s` with the manual `exclusions: [target]`. The saving
is larger than the `0,9`-`1,1 s` of the exclusion measurement, and the rule beats
the exclusion itself by about a second. `TreeDigest` takes no exclusions, so
that is consistent with the exclusion leaving it to walk `target/`; the run does
not isolate it.

Not established by this record:

- the entries each walk looks at is now counted in `benchmarks/walk-entries`, but
  only for walks inside the kivgraph process: `go list`, the TypeScript worker and
  `rust-analyzer` read directories too;
- the saving on a repository whose `target/` is small, with several tagged
  directories, or on Linux;
- whether another tool's tag, such as a Python cache, is worth the same walk
  change. The rule applies to them by construction and has not been measured.
