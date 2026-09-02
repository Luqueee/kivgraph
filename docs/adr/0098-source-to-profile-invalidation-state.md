# ADR 0098: retain the reverse source-to-profile invalidation state

- Status: Accepted
- Date: 2026-09-02
- Issue: #150

## Context

Each published generation now records the source observations that produced
it, but that manifest is scoped to one profile. A worktree may be selected by
several profiles, and a source change must identify every affected profile
without treating a repository name or path as the source identity.

The invalidation record must also survive a daemon restart and concurrent
profile indexers. It is diagnostic state, not a second graph store: a stale
marker must never mutate `CURRENT` or make a partially built generation
readable.

## Decision

Kivgraph keeps one `source-invalidation.json` file at installation scope. Each
profile record contains its current generation and the source manifest copied
from that generation. The state also stores an explicit reverse index from
`WorktreeID` to sorted dependent profile names.

Comparing a tracked manifest with a newly observed manifest reports source
changes by stable worktree identity. It distinguishes content digest, commit,
dirty state, branch, provider policy, source membership and analyzer/resolver
configuration changes. If a source is unavailable before a complete manifest
can be captured, the caller records the reason and the observed detail rather
than fabricating an `EXACT` source state.

Marking a source stale fans out to every dependent profile and deduplicates the
diagnostic by worktree and provider. Recording a successful full generation
replaces only that profile's record and clears only its stale marker. The
manager serializes read-modify-write operations with the installation lock,
writes a validated temporary file, flushes it, and atomically replaces the
state file.

This stage provides the reverse index, durable stale state and comparison
contract. Rebuild scheduling and watcher ownership are defined in ADR 0099.

## Consequences

- Shared worktrees have one authoritative dependent-profile list.
- Dirty edits and commit movement are distinguishable even when their content
  digests happen to match.
- An unavailable source remains an explicit diagnostic with its reason.
- A failed or in-progress rebuild cannot replace the last valid generation
  through this state file.
- The state can be reconstructed and inspected after a daemon restart.

## Rejected alternatives

- Indexing by path or repository name would conflate moved worktrees and
  profiles that use different source instances.
- Keeping state only in process memory would lose stale diagnostics when the
  daemon restarts and would miss updates made by another indexer process.
- Mutating the graph or `CURRENT` when marking stale would make a diagnostic
  side effect destroy the stable generation readers already hold.

## Verification

`internal/invalidation` tests reverse-index construction, shared-profile fan
out, commit and dirty distinctions, unavailable-source diagnostics, selective
stale clearing after publication and refresh after an external writer.
