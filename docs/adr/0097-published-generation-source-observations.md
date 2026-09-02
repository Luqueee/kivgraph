# ADR 0097: bind published generations to observed source inputs

- Status: Accepted
- Date: 2026-09-02
- Issue: #150

## Context

A profile can compose mutable worktrees. A full rebuild already publishes a
candidate only after its canonical graph, integrity checks and golden probes
pass, but it previously had no durable record of the source state that supplied
those facts. A worktree could change after analysis and before `CURRENT` moved,
leaving a valid graph described as if it were current.

Git commit comparison alone is insufficient: uncommitted source bytes can change
without moving `HEAD`. Conversely, the source path is not an identity because a
worktree can move while still representing the same mutable instance.

## Decision

Each new generation may carry `source-observations.json` beside its database and
snapshot. The versioned manifest records, for every effective provider:

- the configured repository name and selected worktree identity;
- commit, branch, dirty state and a deterministic digest of analysable files;
- manifests, roots, exclusions and language provider policy; and
- the resolver version and analyzer fingerprint used by the pass.

An analyzer-discovered provider such as an opted-in Dart package or SDK has no
Git worktree. Its record declares it as derived, leaves `branch` empty, and
uses a `content-<sha256>` revision token. This records the state actually read
without fabricating a Git commit.

The digest deliberately covers every supported source file and build manifest
under a repository, except `.git` and `node_modules`. It over-approximates
language-specific source selection: an unnecessary rebuild is safe, whereas
omitting bytes a provider could read would permit an unreproducible graph.

`indexing.RunFull` resolves the effective provider set and captures this
manifest before analysis. `rebuild.Run` writes it only into the candidate
generation, then resolves and captures the same inputs again from inside the
generation-store validation closure, after integrity and probes and before the
atomic `CURRENT` update. A changed, missing or unreadable input rejects the
candidate; the prior published generation stays active.

Topology-backed registries preserve their declared `WorktreeID` in runtime
provider metadata. A legacy registry has no topology declaration, so it uses a
repository-name compatibility identity only for the observation record. This
does not invent profile membership or a dependency edge.

Existing generations without the optional manifest remain valid and readable.
They are not rewritten; the next successful full rebuild writes the new file.

## Consequences

- A generation never becomes current after a source changes during its pass.
- A dirty edit and a commit movement produce different source observations.
- Failed source verification cannot replace a graph that readers already use.
- The persisted manifest is the input for later stale-profile diagnostics and
  reverse source-to-profile invalidation; this decision does not yet schedule
  those rebuilds.

## Rejected alternatives

### Compare only Git commits

Rejected because dirty bytes do not require a commit movement.

### Trust file timestamps

Rejected because timestamps do not identify bytes and can move without a
semantic change or remain unchanged across coarse filesystem clocks.

### Publish first and mark stale afterwards

Rejected because it exposes a graph as current even though the source state it
claims to describe was already gone before publication.

## Verification

`internal/sourceobservation` tests source availability, dirty edits, commit
movement, policy persistence, deterministic source digests and manifest
round-tripping. `internal/rebuild` verifies manifest publication and that a
source mismatch leaves the prior generation current.
