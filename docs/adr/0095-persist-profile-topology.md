# ADR 0095: persist and apply profile topology

- Status: Accepted
- Date: 2026-09-01
- Issue: #149

## Context

`topology.ProfileComposition` can select several worktrees, but a profile
previously had no durable declaration that the indexer could load. Applying a
composition only in an in-memory test would leave `index --full` indexing the
ordinary repository registry and would not support a worktree from another
logical repository.

## Decision

Each profile may contain `topology.yaml` beside its `repositories.yaml`:

```yaml
version: 1
repositories:
  - id: frontend
worktrees:
  - id: frontend-main
    repository: frontend
    path: ../../../worktrees/frontend
profiles:
  - id: feature-login
    worktrees:
      - repository: frontend
        worktree: frontend-main
```

The document requires `version: 1`, uses strict YAML decoding, expands paths
relative to the document, validates the complete topology, and requires the
loaded profile to be selected. `config.SaveProfileTopology` validates the same
rules before atomically writing the document.

The document is optional. When it is absent, `index --full` keeps using the
existing profile repository registry unchanged. When it is present, the CLI
composes the selected profile and calls `workspace.NewComposedRegistry`; the
ordinary repository registry supplies provider metadata and the topology
supplies selected worktree paths.

## Consequences

- A profile can make several repository worktrees one effective indexing
  universe without relying on filesystem layout or repository aliases.
- A missing topology file remains compatible with installations created before
  profile compositions existed.
- A malformed, unsupported or incomplete topology fails before indexing or
  publishing a generation.
- Relative topology paths are portable within the configuration layout, while
  logical repository and worktree identities remain independent of those paths.
- The topology file is configuration metadata; it does not change the canonical
  graph schema or turn profile membership into dependency evidence.
- This decision covers configuration persistence and registry construction; it
  does not define MCP lifecycle commands or topology diagnostics.

## Rejected alternatives

- Embedding topology in `repositories.yaml` would mix provider metadata with
  worktree identity and make existing repository documents ambiguous.
- Inferring a topology from repository paths or names would violate the
  fail-closed identity contract.
- Treating a missing topology as an empty composition would make old profiles
  index nothing, so absence retains the legacy registry path.

## Verification

`internal/config/profile_topology_test.go` covers optional absence, strict and
versioned decoding, invalid selections, relative path expansion and durable
round trips. `cmd/kivgraph/profile_registry_test.go` verifies both composed
worktree selection and the legacy fallback used by `index --full`.
