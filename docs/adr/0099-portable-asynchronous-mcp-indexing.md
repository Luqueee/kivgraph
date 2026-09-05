# ADR 0099: portable asynchronous MCP indexing

- **Status:** accepted
- **Date:** 2026-09-05
- **Changes the MCP protocol:** yes -- two tools are added
- **Changes the persistent schema:** no
- **Requires a rebuild:** no
- **Changes the CLI surface:** no

## Context

A complete rebuild can take minutes. MCP progress notifications help only when
the client supplies a `progressToken`, displays the notifications, and treats
them as activity for its timeout policy. None of those behaviours is mandatory.
Several coding agents instead apply a fixed per-tool deadline, so a healthy
`index_project` can be cancelled before it returns.

Increasing a host-specific timeout does not solve the protocol problem. Every
client names the setting differently, some do not expose one, and an installed
setting can drift independently of the Kivgraph server.

MCP Tasks model this operation directly, but they are experimental in protocol
version `2025-11-25` and require both peers to advertise the capability. Making
them the only route would exclude clients that support ordinary tools but not
Tasks.

## Decision

Kivgraph keeps the synchronous `index_project` contract and adds two ordinary
MCP tools:

- `start_index_project` obtains the same explicit consent, accepts the same
  single-project and batch input, starts one background rebuild, and returns an
  opaque `operation_id` immediately;
- `get_index_status` accepts that ID and returns `working`, `completed`, or
  `failed`, the latest observed progress, and either the published result or a
  classified failure.

The operation uses a context owned by the hosting process, not the completed
`tools/call` request. Cancelling the short request after it returned therefore
does not cancel the rebuild it accepted. A daemon creates one operation registry
and shares it across its socket and HTTP sessions. A standalone stdio server
owns one registry for its process lifetime.

Only one asynchronous operation is accepted at a time. Completed history is
bounded to the latest 32 operations. IDs contain 128 bits from the operating
system random source and are treated as opaque lowercase hexadecimal strings.
An unknown, malformed, or expired ID fails with `INVALID_ARGUMENT` and tells the
caller to copy the value returned by `start_index_project`.

A second start fails with `INDEXING_IN_PROGRESS` and includes the active
`operation_id`. That makes a lost start response recoverable without repeating
or abandoning the mutation.

`notifications/progress` remains on synchronous calls and may later accompany
the asynchronous route. It is a user-interface enhancement, not a correctness
or liveness dependency.

## Compatibility

The change is additive. Existing `index_project` requests, results, consent,
batching, rollback, and progress behaviour are unchanged. Clients that know only
that tool continue to work as before.

The two new tools use ordinary `tools/call`, so they require no optional MCP
capability. The status response uses the existing single text channel and does
not add an `outputSchema`; `index_project` remains the only measured exception
that returns its fixed report through both channels.

## Consequences

An agent can keep every individual MCP call below its deadline by starting once
and polling at the returned interval. Polling never starts a second rebuild, so
a client timeout cannot tempt the agent into repeating the mutation.

Operation state is durable across sessions of one daemon process, but not across
a daemon or standalone-server restart. A host restart loses the status record
and terminates work owned by that process; making jobs restart-durable would need
an on-disk state machine and recovery contract of its own.

The configured surface grows from twelve to fourteen tools: eleven graph
queries, one read-only operation-status query, and two consent-gated mutations.
The installed skill and user documentation route new work through the
asynchronous pair while retaining the synchronous tool as compatibility
fallback.
