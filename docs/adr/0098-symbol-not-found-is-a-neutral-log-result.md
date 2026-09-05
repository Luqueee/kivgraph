# ADR 0098: a missing symbol is a neutral log result

- Date: 2026-09-05
- Status: accepted
- Compatibility: MCP continues to return `SYMBOL_NOT_FOUND` as an error code.
  Event logs add `status: "not_found"`; readers also recognise older rows.

## Context

The MCP surface deliberately returns `SYMBOL_NOT_FOUND` when a selector names
no declaration. A client needs that stable code to distinguish no match from an
empty reference list. The durable operator log copied the MCP error path
directly, so a normal exploratory query appeared as a red operational failure.

That made `logs --failures` and `tool-stats` ask an operator to investigate
ordinary absence. Existing append-only rows have the old `error` status, so
changing only new writers would leave the misleading history in place.

## Decision

The tool recorder writes a completed missing-symbol lookup as `status:
"not_found"`, with a zero result count and no error detail. The table renders a
neutral `NOT_FOUND` badge. `SYMBOL_NOT_FOUND` remains unchanged at the MCP
boundary, where clients use it for control flow.

The event reader maps historical tool rows whose stable error code is
`SYMBOL_NOT_FOUND` to the same neutral outcome. They are excluded from
`--failures` and contribute to the non-failing count in `tool-stats`; no file
rewrite or graph rebuild occurs. Other error codes, including invalid arguments,
missing repositories, unavailable snapshots, and index failures, remain errors.

## Consequences

`not_found` is an additive event-status vocabulary value. Readers that do not
know it continue to ignore unknown JSON fields and retain the old file shape;
current readers provide a backward-compatible rendering for historical rows.
There is no separate `NOT_FOUND` aggregate column in `tool-stats`: it is a
completed, non-failing call, so the existing `OK` count remains the compatible
aggregate.
