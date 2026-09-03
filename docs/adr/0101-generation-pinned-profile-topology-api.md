# ADR 0101: expose a generation-pinned profile topology API

- Status: Accepted
- Date: 2026-09-03
- Issue: #152

## Context

The viewer needs to explain how profiles, logical repositories and mutable
worktrees relate to the published graph. The existing HTTP endpoints answer
symbol-level queries and the `LGVB` tile payload is intentionally not a
topology configuration format. Joining those responses by filesystem path
would lose worktree identity and could invent relationships between profiles.

A profile can also publish a newer generation while a viewer is paging or
refreshing its topology. A response that silently combines profile metadata
from different generations is not a truthful view of the graph.

## Decision

Add `GET /api/v1/topology` as a JSON read model independent of the `LGVB`
payload. The response carries `topology_version`, selected profiles and their
generation IDs, logical repository and worktree records, indexed and current
source observations, shared-worktree ownership, typed relationships, status
reasons and a `completeness` object.

`profile` may be repeated to select profiles. A single-profile continuation
pins `generation_id=000042`; a multi-profile continuation supplies one
`generation=profile:000042` value for every selected profile. Unpinned requests
are accepted; when a continuation supplies a pin, a missing or invalid pin is
rejected, and a pin that no longer matches the active generation returns
`409 GENERATION_CHANGED` with refresh guidance. The server
captures immutable snapshot pointers for all selected profiles and checks
their generation IDs again before returning, so it never assembles one
response from a changing profile store.

Relationships are bounded at 10,000 unique emitted relationships per response.
Relationships with distinct evidence remain distinct. When the bound is
reached, the server keeps the deterministic prefix and sets
`completeness.truncated`; it never presents a partial relationship set as
complete. When source metadata is also incomplete, `completeness.reason` names
both the incompleteness and truncation reasons.

Malformed requests return `400 INVALID_ARGUMENT`; stale continuations return
`409 GENERATION_CHANGED`; ambiguous declarations return
`409 TOPOLOGY_AMBIGUOUS`. Unavailable generations return
`503 TOPOLOGY_UNAVAILABLE`; unexpected assembly failures return `500 INTERNAL`.

Structural membership is emitted separately from source-backed code
dependencies. Exact and candidate relationships retain their confidence,
provenance and evidence key. Unresolved references have no fabricated target;
ambiguous or conflicting diagnostics are marked `conflict`. Profiles remain
separate in the response, so selecting several profiles does not create
cross-profile code edges.

Indexed observations come from the generation's
`source-observations.json`, with the persisted invalidation state as the
diagnostic fallback. The latter supplies the last current observation and
names stale, missing or unavailable sources without claiming a filesystem
state that was not observed.

The existing `NewHandler` constructor, symbol endpoints and `LGVB` payload
remain unchanged. The topology route is opt-in through
`NewHandlerWithTopology`. The configured UI uses the profile aggregate for its
unscoped view and follows each child generation independently; an explicit
`--profile` keeps the single-profile path.

## Consequences

- The visualizer has one stable, typed source for profile/worktree topology.
- A client can distinguish a stable generation from stale or unavailable
  mutable inputs and can recover from a concurrent publication by refreshing.
- JSON payload size is observable before deciding whether a binary topology
  representation is warranted.
- Legacy profiles without `topology.yaml` remain readable through conservative
  `legacy:<repository>` worktree identities, while missing source manifests
  are reported as incomplete rather than inferred as current.

## Rejected alternatives

- Extending `LGVB` would couple a configuration/topology contract to the
  symbol tile format and break its compatibility boundary.
- Scraping MCP responses would duplicate a server contract and would not
  provide generation pinning or typed node identities.
- Inferring relationships from paths, names or profiles would violate the
  source-evidence and no-fabricated-cross-profile-edge rules.

## Verification

`internal/webapi/handler_test.go` covers missing generations, malformed pins,
single- and multi-profile generation identities, shared ownership, exact,
candidate, structural, conflict and unresolved relationships, unavailable
sources and stale continuations. Existing symbol and binary viewer tests
continue to exercise the unchanged endpoints and `LGVB` payload.
