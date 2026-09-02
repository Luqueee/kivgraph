# ADR 0098: invalidate profiles from shared mutable sources

- Status: Accepted
- Date: 2026-09-02
- Issue: #150

## Context

ADR 0097 records the source state that produced a published generation and
verifies it again before publication. That protects one pass, but a daemon
also needs to notice a dirty edit or a moved worktree after publication. The
same worktree may be selected by several profiles, so observing it in one
profile must invalidate every dependent generation.

## Decision

`sourceobservation.Tracker` keeps the reverse relationship from `WorktreeID` to
profiles and records each profile's last valid generation and manifest. A
source observation compares complete manifests by repository identity, reports
all changes in stable order, and marks the affected profiles stale with a
source-specific reason. The old generation remains the diagnostic and serving
baseline until `Commit` records a successful replacement.

The resynchroniser receives filesystem wake-ups and performs a slower periodic
observation as a dropped-event backstop. Events are only hints; `Capture` is
the authority for bytes, Git state, policy, analyzer configuration and source
availability. Changes are coalesced by repository while a debounce window is
quiet. A complete rebuild is attempted under the existing profile and shared
analyzer locks. Source verification before publication remains mandatory.

An unavailable or moved source marks its dependent profiles stale and retains
the last valid generation. A failed rebuild uses the existing bounded retry
policy and never commits its observation. After the bound is exhausted, the
source state is accepted as the new observation baseline only to avoid an
infinite retry; the diagnostic stays stale and names the remedy. A later source
movement creates new work.

A changed analyzer or resolver fingerprint creates a profile-scoped invalidation
so an old generation is rebuilt under the configuration that will serve it. It
does not invalidate another profile merely because that profile shares a
worktree.

The reverse index is process-local and rebuilt from published manifests when a
server starts. It is a diagnostic and scheduling aid, not a second graph or a
new persistence schema. Published generations remain immutable and no
incremental graph mutation is introduced.

## Consequences

- Dirty edits invalidate profiles even when Git `HEAD` does not move.
- A shared worktree identifies every dependent profile and generation.
- Dropped events are recovered by periodic complete observations.
- A source changing during analysis fails the candidate verification and leaves
  `CURRENT` untouched.
- Existing commit-only resynchronisation can still skip a rebuild when Git
  proves the trees are identical.

## Rejected alternatives

- Comparing only commits misses dirty bytes in a worktree.
- Treating a filesystem event as proof would publish before reading the actual
  source state and would lose events on backend overflow.
- Mutating one published graph for all profiles would violate profile
  isolation and the full-rebuild publication contract.
- Retrying forever would repeatedly run a broken analyzer or unavailable
  source and hide the fact that the profile remains stale.

## Verification

`internal/sourceobservation` tests deterministic diffs, reverse dependencies,
shared worktrees, unavailable inputs and failed publication state. The
resynchroniser tests dirty events, debounce, retries and invalid initial
manifests. The existing generation verification tests cover source movement
during a full pass.
