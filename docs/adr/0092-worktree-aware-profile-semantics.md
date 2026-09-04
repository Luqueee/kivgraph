# ADR 0092: worktree-aware profile semantics

- **Status:** accepted for the domain model
- **Date:** 2026-09-01
- **Changes the MCP protocol:** no
- **Changes the persistent graph schema:** no
- **Requires a rebuild:** no; this delivery adds no migration and does not
  change an existing published generation
- **Changes the CLI surface:** no

## Context

Profiles currently own independently published repository registries. That
model is sufficient for isolated work, but it cannot describe one resolution
universe containing, for example, a frontend worktree and a backend worktree.
It also leaves the identity of a mutable checkout implicit in its path.

Issue #147 requires those concepts to be separated before composition, shared
source invalidation or topology visualisation is implemented. A filesystem
path is unsuitable as identity: worktrees move, linked worktrees have distinct
Git layouts, and two branches of one repository may be active at once.

## Decision

The domain model in `internal/topology` uses five dedicated identities:

- `LogicalRepositoryID` names the source repository represented by one or more
  worktrees.
- `WorktreeID` names one mutable checkout. Its path and Git layout are
  replaceable metadata.
- `SourceObservationID` names the state actually analysed. It is derived from
  the worktree ID, commit, branch, dirty state and content digest.
- `ProfileID` names an effective dependency-resolution universe.
- `GenerationID` names an independently published profile generation and keeps
  the existing six-digit persistent representation.

The configuration model has explicit `WorktreeSelection` entries. A `Profile`
selects at most one worktree for each logical repository. Different worktrees that
represent the same logical repository are therefore variants that may coexist
in the installation, but they must be selected by different profiles.

`Topology.Validate` fails closed. It rejects unknown references, duplicate
ownership, a logical repository selected through conflicting worktrees in one
profile and mismatched repository/worktree declarations. The same worktree may
be referenced by more than one profile; the ownership constraint is per
effective profile.

A profile is therefore a resolution universe, not a physical worktree. Two
worktrees from different logical repositories can be selected together, while
two variants of one logical repository can be represented in different
profiles without being silently merged.

## Alternatives

### Use the worktree path as the identity

Rejected. Moving a checkout would look like removing one source and adding a
different one, and a dirty worktree could not be distinguished from its clean
commit.

### Use the Git common directory as the identity

Rejected. Linked worktrees share Git storage but are different mutable source
instances. Common storage is an implementation detail, not source identity.

### Keep one repository entry per profile and infer variants from paths

Rejected. The profile boundary would continue to conflate source identity,
checkout state and dependency-resolution scope. It would also reintroduce the
path inference that this model is intended to remove.

## Consequences

- `WorktreeID` remains stable and does not depend on `Worktree.Path`; moving a
  checkout therefore does not change its source identity.
- Dirty bytes are part of source freshness through `SourceObservationID`; a
  commit comparison alone is insufficient.
- A profile can compose repositories explicitly while retaining the existing
  fail-closed provider rules.
- Shared source ownership and profile-specific overrides remain outside this
  first model slice and belong to the dependent shared-input issue.
- This delivery is declarative only. Existing `repositories.yaml`, profile
  stores, query routing and publication code are not bound to this model in
  this delivery.
- No existing wire format, canonical schema, state layout or published
  generation changes in this delivery.

## Risks

The content digest is required but this delivery does not choose how a future
indexing pass captures the complete analysed file set. That observation policy
must be defined before source invalidation is implemented. Shared source
ownership and profile-specific overrides also need an explicit contract before
they can be added without making invalidation ambiguous.

## Verification

`internal/topology/model_test.go` covers rejected identity and configuration
inputs first, then verifies composition across two repositories, isolated
variants across profiles, path-independent worktree identity, content-aware
source observations and YAML round-tripping.
