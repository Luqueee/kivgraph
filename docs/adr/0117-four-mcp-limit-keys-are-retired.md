# ADR 0117: Four `mcp` limit keys are retired, and still accepted

Status: accepted, 2026-11-15.

Changes the configuration: retires `mcp.default_limit`, `mcp.maximum_limit`,
`mcp.maximum_depth` and `mcp.maximum_visited_nodes`.

Changes the schema: no. `config.version` stays at `1`.

## Context

The four keys were declared, had defaults, were validated on load and were
documented as the limits of a query. Nothing read them. The bounds a call is
held to are constants beside the code that walks the graph:
`hotsnapshot.MaxExactResults` (`500`), `hotsnapshot.MaxTraversalDepth` (`5`)
and `hotsnapshot.MaxTraversalNodes` (`25000`), plus the defaults each tool
declares in its own schema.

So an operator who set `maximum_visited_nodes: 1000` to protect a shared
daemon loaded a valid configuration, saw `doctor` pass, and limited nothing: a
caller could still send `max_nodes: 25000`. A key that promises a limit and
does not apply it is worse than no key.

This is the same defect ADR 0062 found in `storage.snapshots_path` and
`storage.retain_snapshots`.

## Decision

Retire them, through the path ADR 0062 built.

Wiring them was the alternative. It was not taken because it changes what a
running installation answers: every configuration written by `kivgraph init`
carries the four defaults, and a file where someone lowered one would start
truncating answers on upgrade, with no change on their side. The bounds are
also part of each tool's published schema, and a per-installation override
would make that schema describe a limit the server no longer honours.

## The migration

The keys are added to `retiredConfigKeys`. A document that carries them loads,
the values are ignored, and `kivgraph doctor` names them under
`config.retired` as accepted, ignored and safe to delete.

Ignored includes not validated. `maximum_limit: 10` under the default
`default_limit` was a load error yesterday; it loads today. That direction is
the safe one: nothing that was valid becomes a failure.

Strictness is unchanged. A key in `mcp` that is neither known nor retired
still fails the load.

## Consequences

- `kivgraph init` no longer writes the four keys.
- The four validation errors disappear with the keys.
- The configuration reference documents them as retired and names where the
  real bounds live.
- `docs/performance/slo.md` states the bounds as fixed values instead of as
  configuration.

## Verification

- `TestARetiredKeyLoadsAndIsReported` loads a document carrying the four keys
  with values the old validation rejected, and expects them reported in order.
