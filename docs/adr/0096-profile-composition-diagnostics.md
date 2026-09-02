# ADR 0096: report the effective profile composition

- Status: Accepted
- Date: 2026-09-02
- Issue: #149

## Context

`index --full` can now select repository worktrees from a persisted profile
topology. A report that only contains language counts cannot show which source
instances formed the provider universe, so an operator cannot distinguish an
edge-resolution problem from an incorrectly selected worktree.

The current implementation treats composition as membership and provenance
metadata. It is not evidence for a dependency edge; dependency evidence is
stored separately in the canonical fact set.

## Decision

The human-readable `index --full` report emits an `index.profile` line. For a
topology-backed profile it also emits one `index.profile.worktree` line per
selected repository, naming the logical repository, worktree and resolved path.
For a profile without topology it reports `composition=legacy` and the number
of repositories in the ordinary registry.

These lines are emitted after the effective registry has been built and before
the indexing pass starts, so they are available even when the pass later fails.
They are informational and do not change provider selection or create edges.

`index --full --json` remains the existing JSON event protocol: composition
diagnostics are not written to its `stdout`, which continues to contain only
progress events and one final result event.

## Consequences

- Operators can audit the exact profile inputs used by a full index.
- A selected path can be compared directly with the worktree expected by the
  profile, without inspecting internal registry state.
- Existing profiles without topology remain explicit and compatible.
- The JSON child-process protocol and canonical graph schema are unchanged.

## Rejected alternatives

- Adding worktree membership to dependency edges would turn provenance into
  source evidence and fabricate relationships from co-membership.
- Printing the diagnostics in the JSON stream would make the human report an
  undocumented protocol event and break readers that expect the final result.
- Reporting only the repository count would hide which worktree was indexed.

## Verification

`cmd/kivgraph/profile_registry_test.go` verifies that selected worktrees are
reported and that the same selection reaches the full Go indexer with its
source-backed cross-repository edge rules.
