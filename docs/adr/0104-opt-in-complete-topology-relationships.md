# ADR 0104: allow complete topology relationship responses

- Status: Accepted
- Date: 2026-09-05
- Issue: local viewer completeness follow-up

## Context

ADR 0101 bounds `GET /api/v1/topology` at 10,000 relationships so an
unqualified client cannot accidentally request an arbitrarily large JSON
document. The topology viewer used that default response, which meant a graph
with more relationships always opened with an incomplete map even when every
selected generation was available.

Raising the shared default would remove the safety property for every API
consumer. Treating the deterministic prefix as the whole graph would remain
dishonest.

## Decision

Keep the existing 10,000-relationship default and add two explicit modes.
`relationships=all` removes the relationship count bound and preserves every
evidence row. `relationships=grouped` also scans the complete graph, but
coalesces code dependencies with the same profile, generation, endpoints,
kind, status, confidence, provenance and reason. Each row reports its exact
`occurrences`; structural and unresolved relationships remain individual. All
profile selection, generation pinning, source completeness and generation
change checks remain unchanged. Any other value, or more than one
`relationships` value, returns `400 INVALID_ARGUMENT`.

The bundled topology viewer always requests `relationships=grouped`, including
generation-pinned refreshes and the read-only API link it exposes. Its counters
and visual edge labels sum `occurrences`, so every scanned dependency is
represented without materialising hundreds of thousands of equivalent browser
objects. Clients that need each evidence row use `relationships=all`. Clients
that omit the parameter retain the bounded ADR 0101 response and its honest
`completeness.truncated` marker.

Complete and grouped responses bypass the per-profile relationship cache so
their scans do not remain resident for the lifetime of the server. Bounded
requests retain the generation-owned cache. Request order does not alter any
response contract. Temporary heap reclamation remains owned by the Go runtime.

## Consequences

- The local viewer can display every relationship in the selected generations.
- Existing API consumers keep their bounded response unless they opt in.
- Exhaustive responses can be large and intentionally trade memory and
  transfer size for per-evidence detail.
- Grouped responses preserve dependency semantics and exact totals, but omit
  individual evidence keys represented by a group.

## Rejected alternatives

- Raising or removing the default bound would make an existing safe request
  unexpectedly expensive.
- Replacing the default response with grouped rows would silently change the
  meaning of an existing API request.
- Returning a partial prefix without the truncation marker would assert an
  absence the server did not establish.

## Verification

`internal/webapi/handler_test.go` requests a graph larger than the default
bound in bounded, complete, grouped and bounded-again order. It verifies that
complete mode returns every relationship, grouped occurrences equal that
complete total, malformed modes fail closed and request order does not alter
bounded responses. Web client and browser tests verify that initial and
generation-pinned viewer requests use grouped mode and sum occurrences.
