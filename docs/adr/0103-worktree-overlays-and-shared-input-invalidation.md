# ADR 0103: represent worktree overlays and shared-input invalidation

- Status: Accepted
- Date: 2026-09-05
- Issue: #200

## Context

A profile selects one worktree for each logical repository, but a feature
worktree can deliberately replace a shared input for that profile. The prior
topology view showed only generic shared ownership. It could not explain that
replacement or connect a changed shared input to every stale profile that
published against it.

Representing either fact as a code dependency would be false: neither a
worktree replacement nor invalidation is evidence that one source symbol calls
or imports another.

## Decision

Topology schema version 2 adds `worktrees[].overlays`. A selection may replace
one declared base worktree only when both represent the same logical repository.
The target cannot be selected by the same profile, cannot be an overlay itself,
and each overlay worktree has one owning profile. Validation rejects every
ambiguous or cyclic form.

Version 1 topology documents remain readable. An explicit topology save writes
version 2. A version 1 document cannot declare the new field, so an old
configuration never silently gains overlay semantics.

Source-observations schema version 3 persists the selected overlay reference
and the target worktree metadata with the effective composition. Versions 1 and
2 remain readable: version 1 is incomplete as before, and version 2 denotes a
composition that has no overlay declarations.

`GET /api/v1/topology` emits structural `shared_input_usage`,
`worktree_overlay`, and `shared_input_invalidation` relationships. Overlay
targets and changed inputs are represented as shared-input nodes even if a
query selects only the overriding profile. Invalidation relationships carry the
affected profile and its pinned generation, use `SOURCE_INVALIDATION`
provenance, and do not create a code edge. Shared-input node status aggregates
the persisted source changes and exposes their reason.

The client renders usages from the API rather than synthesizing them. Overlay
and invalidation edges have their own labelled visual treatment, while the
accessible relationship list displays profile generation, reason, and
provenance together.

## Consequences

- Logical repository identity is unchanged when a feature worktree overlays a
  shared input.
- A shared change visibly reaches every selected stale profile/generation.
- Exact, candidate, unresolved, and conflict code relationships remain
  semantically separate from topology and invalidation relationships.
- A pinned historical topology retains the overlay definition that selected its
  sources instead of reading a live configuration.

## Rejected alternatives

- Infer overlays from matching repository paths or branch names: those are
  mutable metadata and do not prove an ownership decision.
- Encode invalidation as `CANDIDATE` or `UNRESOLVED` code evidence: those
  confidence values describe resolver outcomes, not lifecycle state.
- Keep client-generated shared usage edges: the accessible view and API would
  describe different topology contracts.

## Verification

`internal/topology` tests reject invalid overlay declarations and preserve a
valid overlay composition. `internal/sourceobservation` covers version 2
compatibility and version 3 overlay round trips. `internal/webapi` asserts the
typed usage, overlay, and invalidation relationships with their profile and
generation provenance. The web unit and end-to-end tests verify the API decoder,
canvas labels, colours, and accessible relationship table.
