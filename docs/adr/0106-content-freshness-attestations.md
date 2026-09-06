# ADR 0106: Content freshness belongs to the published generation

Status: accepted.

Commit equality cannot detect edits, additions or deletions in a working tree.
Graph consumers that require freshness need a separate filesystem observation.

A full pass captures a deterministic inventory before and after analysis.
The inventory covers registered roots, source files and build configuration,
including untracked files, while respecting repository exclusions. Unreadable
roots and source symlinks fail closed. Derived providers are not registered
source trees and are not included.

When both inventories match and publication passes its existing integrity
gates, the pass writes a versioned attestation under the configured state root:
`freshness/<generation>.json`. This is additional derived state, not a
Ladybug schema or snapshot-row migration. It does not alter stable identities.

The configured MCP service adds `content_freshness` to `graph_status`.
It reloads the registry and compares the current inventory with the
attestation for the generation being served. States are `fresh`, `stale`,
`unverified` and `unavailable`. Older generations remain readable and are
`unverified`, not corrupt. A full rebuild supplies the missing attestation.

The inventory is conservative: configuration files may invalidate a graph
even when a particular analyzer does not consume them. Source freshness does
not prove analyzer completeness, external dependency freshness, test coverage
or absence of consumers. The existing completeness envelope remains mandatory.

Consumers must compare the attestation generation with the response generation
and verify again after composite queries. They must not interpret source
reanchoring as a repair of graph relationships.

With profile discovery, the service attestation describes only its default
profile. Other selected profiles and aggregate discovery omit that attestation;
profile-local generation numbers cannot establish freshness across profiles.

Verification uses filesystem fixtures for edits, additions, deletions,
exclusions, unreadable roots and cancellation, plus the full native index
tests and a locally installed bundle.
