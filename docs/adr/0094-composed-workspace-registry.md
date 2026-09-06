# ADR 0094: compose the effective workspace registry

- Status: Accepted
- Date: 2026-09-01
- Issue: #149

## Context

`Topology.Compose` identifies the repositories and worktrees in one profile,
but the indexers consume `workspace.Registry`. Selecting a topology entry only
in diagnostics would leave the actual provider universe unchanged and would
make the composition contract impossible to exercise at the indexing boundary.

The existing repository registry also carries provider configuration that does
not belong to the topology model: languages, manifests, roots and exclusions.
Those values must follow the logical repository while the selected worktree
path comes from the composition.

## Decision

`workspace.NewComposedRegistry` builds an observed registry from two validated
inputs:

1. the ordinary repository registry, which supplies provider metadata; and
2. one `topology.ProfileComposition`, which supplies the ordered logical
   repositories and selected worktree paths.

The configured repository `Name` is the compatibility identity for the
logical repository ID. The adapter requires an exact ID match and fails when
provider metadata is missing; it does not infer an alias from a display name,
path or repository order.

Only selected repositories are passed to `NewRegistry`. Their configured
metadata is copied, and their path is replaced with the selected worktree path.
The existing path, Git and provider validation therefore remains the single
registration gate. A malformed composition is rejected before any provider is
read.

The resulting registry retains a copy of the composition. `Registry.Composition`
returns that membership and worktree provenance for diagnostics without
exposing mutable registry state. This provenance describes the resolution
universe; it does not create dependency edges. Language providers still need
source evidence to emit an exact relationship.

## Consequences

- A profile can now provide the indexer with multiple selected worktrees from
  different logical repositories.
- A worktree variant changes the observed registry path without changing the
  provider metadata attached to its logical repository.
- Unselected repositories cannot become providers through co-membership.
- Separate compositions produce separate registries and retain their variant
  identities.
- Existing callers of `NewRegistry` and existing configuration files are
  unchanged. Profile topology persistence and CLI wiring are defined in ADR
  0095; this ADR covers registry construction and its validation boundary.

## Rejected alternatives

- Joining independently observed registries after indexing would not give the
  language providers one effective resolution universe.
- Reusing the configured path would silently index the wrong worktree.
- Mapping by path, display name or position would turn metadata into an
  unstable identity and could select the wrong provider.
- Storing the composition only in the canonical graph would mix membership
  metadata with source-backed code relationships.

## Verification

`internal/workspace/composition_test.go` covers malformed input rejection,
missing provider metadata, duplicate metadata, selected worktree paths,
preserved provider configuration, copied provenance and isolated variants.
