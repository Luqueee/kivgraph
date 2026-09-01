# ADR 0093: Composition profiles select an effective registry

- Status: Accepted
- Date: 2026-09-01
- Issue: #149

## Context

Issue #148 defined the identities of logical repositories, worktrees, profiles,
source observations and generations. The next step is to make a profile's
multi-repository resolution universe explicit. Kivgraph already runs a full
pass over the repositories in the selected profile registry, but the topology
model needs a deterministic operation that can explain which worktrees formed
that registry.

A profile boundary is a resolution boundary. Selecting two worktrees in one
profile permits the existing language providers to resolve source-backed
dependencies between them. Selecting the same worktrees in separate profiles
does not create a cross-profile join.

## Decision

`Topology.Compose` materialises one `ProfileComposition` from a validated
topology. It returns the selected profile, its worktrees and the corresponding
logical repositories in declaration order. It validates the complete topology
before selecting anything, so duplicate ownership, unknown worktrees and
conflicting variants remain errors rather than being hidden by a narrow query.

The composition is membership and provenance metadata only. It does not create
dependency edges. Imports, module paths, package metadata, project references,
workspace files and other language-specific facts remain the only sources of
code relationships. The existing Go, TypeScript, Rust, Java and C# indexers
consume the selected registry and retain their fail-closed provider rules.

## Consequences

- Diagnostics can name the profile, logical repositories and worktrees that
  formed one effective resolution universe.
- A multi-repository profile has an explicit, inspectable input set.
- Separate profiles remain isolated even when they select variants of the same
  logical repository.
- Composition does not yet change the on-disk profile registry or publish a
  topology payload; those integrations belong to the later lifecycle and API
  issues.

## Rejected alternatives

- Joining independent profile snapshots at query time would claim edges that no
  single provider saw in one effective registry.
- Inferring membership from paths, parent directories, repository names or
  visual proximity would turn metadata into source evidence.
- Choosing one provider when several repositories declare the same package or
  module would violate the existing ambiguity contract.
