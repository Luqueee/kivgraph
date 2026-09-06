# ADR 0111: durable bounded tool query summaries

- Date: 2026-09-05
- Status: accepted
- Compatibility: `query` is an optional JSONL event field. Readers of older
  records show an empty cell; older readers ignore the new field.

## Context

`kivgraph logs` could identify a tool, duration, result count, and process, but
not what the tool was asked. This made distinct `find_by_intent` calls look
alike. It also made an operator's durable history less useful than a chat
preamble, which is best effort and disappears with the conversation.

Persisting the complete MCP arguments would create a second, long-lived copy of
unbounded input. It can include opaque cursors, consent state, opaque stable
keys, and fields that a future tool adds without intending to expose them.

## Decision

Every completed MCP tool call may add an optional `query` field to its event.
The observer derives it server-side from a per-tool, ordered allow-list. A new
tool argument is absent until an implementation deliberately admits it.

`find_by_intent` records the exact `intent` value first, followed by supplied
`keywords` and scope. The source tool replaces a stable key with a label rather
than writing the key. The renderer uses compact JSON values, truncates the
whole summary to 320 runes, and does not record cursors, confirmations, unknown
arguments, or absolute paths.

The human view renders aligned `TIME`, `TYPE`, `EVENT`, `QUERY`, `TOOK`,
`RESULTS`, `PID`, and `DETAIL` columns. The query is part of both the following
watermark identity and adjacent-run identity, so distinct questions never
disappear through deduplication or folding. JSON output remains the original
event sequence and includes the optional field.

## Consequences

The additive field needs neither a graph rebuild nor migration. JSON parsers
that know no `query` field already ignore it, and records created before this
decision remain readable with an empty query cell. The event log is still a
bounded, local operator record rather than an audit trail or MCP transcript.

An allow-list must be updated intentionally whenever a new tool needs a query
summary. That is deliberate review work: a generic argument serializer would
silently expand what the durable record retains.
