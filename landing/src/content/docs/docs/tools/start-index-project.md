---
title: start_index_project
description: >-
  Starts a consent-gated graph rebuild and returns immediately with an operation
  ID that any MCP client can poll.
---

> Starts a graph rebuild without holding one MCP call open for the duration of
> the index. Prefer this tool over synchronous `index_project`.

## Arguments

It accepts the same `profile`, `projects`, single-project fields, and
`confirmed` fallback as [`index_project`](/docs/tools/index-project/). Pass all
projects in one `projects` array so Kivgraph rebuilds the complete graph once.

Both indexing tools use the same consent contract. A form-capable client asks
the user through MCP elicitation; other clients must obtain approval before
sending `confirmed: true`.

## Response

The call returns before analysis finishes:

```json
{
  "operation_id": "5e15bc5e34e84f1d9ff8da70ee33425f",
  "status": "working",
  "poll_after_ms": 1000
}
```

Treat `operation_id` as opaque. Poll
[`get_index_status`](/docs/tools/get-index-status/) at the suggested interval
until it reports `completed` or `failed`. Do not repeat `start_index_project`
after an ordinary terminal failure: that would request another complete
rebuild. A failure that explicitly names temporary cross-process store-lock
contention may be retried after the winning rebuild releases the lock.

Only one asynchronous index can run at a time. A second start returns
`INDEXING_IN_PROGRESS`, includes the active `operation_id`, and directs the
caller back to that operation. This also recovers when the original start
response was lost.

When this operation publishes the first generation, reconnect the MCP client
before calling graph-query tools; the current session retains the surface it
advertised during its handshake.

## Lifetime

The hosting process owns the operation rather than the short `tools/call`
request. Cancelling that completed request does not cancel the rebuild. A daemon
shares operation state across its HTTP and socket sessions; a standalone stdio
server retains it for that process lifetime.

The latest 32 completed operations are retained. A daemon or standalone-server
restart does not preserve operation state.
