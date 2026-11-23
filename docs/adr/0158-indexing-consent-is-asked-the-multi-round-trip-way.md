# ADR 0158: Indexing consent is asked the multi round-trip way

## Status

Accepted. Raises `github.com/modelcontextprotocol/go-sdk` from `1.4.1` to
`1.8.0` and `github.com/google/jsonschema-go` from `0.4.2` to `0.4.3`, and
changes how `index_project` and `start_index_project` ask for approval. Changes
no tool name, input schema or persistent schema. Raises the surface ceiling by
`300` bytes.

## Context

The SDK was held at `1.4.1` because `1.8.0` failed three tests of this
repository, and two causes lay behind them.

**The protocol.** `1.8.0` speaks `2026-07-28`, in which a server may not send
`elicitation/create` while it serves a request. It answers with the questions
in `inputRequests`, and the client asks the user and retries the call with the
answers (SEP-2322, multi round-trip requests). `index_project` and
`start_index_project` asked by calling `Elicit` from inside the handler, which
is exactly what a client of the new version refuses: the call came back as
`PERMISSION_REQUIRED: the client could not obtain indexing permission`, and
would have for every client that moves to the new version, however willing its
user.

**The listing.** From `1.7.0` the SDK writes `readOnlyHint` and
`idempotentHint` on every annotation even when they are false, which is the
protocol default and reads the same. It is `"idempotentHint":false` on each of
the twelve tools, `23` bytes each: `18,599` to `18,875`, over the ceiling
of `18,600` that `TestServerSurfaceStaysCheapToLoad` guards. The SDK keeps the
old spelling behind `MCPGODEBUG=hintomitempty=1`, read from the environment when
the process starts, which a server cannot set for the client that launches it.

## Decision

The consent is returned as a question and not asked. `requireIndexConsent`
returns a result carrying `inputRequests` with one elicitation, `consent`, when
the client advertises form elicitation, and reads the answer from
`inputResponses` when the call comes back. The SDK serves both ends of it: a
client of `2026-07-28` fulfils the question and retries by itself, and for a
client on an earlier version the server's middleware asks through `Elicit` and
calls the handler again with the response. One spelling holds for both, and a
test runs every case on `2025-11-25` and on `2026-07-28`, for both tools.

The state echoed with the answer is a digest of the batch -- each project's
name, path and languages, in order. An answer that comes back with another
digest, or with none, is not an answer to this batch and the question is asked
again, so an approval kept from one call does not approve another set of
projects. A client that does not advertise form elicitation still needs
`confirmed: true`, as before, and the flag is never assumed.

The ceiling goes to `18,900`: the cost of the SDK's annotations, with `25`
bytes of room, and no description or route a session pays for in every request
changed: the resident surface of `benchmarks/mcp-token-cost --smoke` stays at
`812` tokens, and the deferred schemas go from `5,174` to `5,289`. `addTextTool` leaves the text block off a result that asks a question,
because content and `inputRequests` are exclusive on the wire.

## Compatibility

A client on an earlier protocol version sees the same question, from the same
tool, and the same answers: approved, declined, cancelled.

Two things differ at the edge. A client that advertises form elicitation and
then fails to answer -- the dialog closed, a transport error -- used to receive
`PERMISSION_REQUIRED` as the result of the tool, and now receives an error of the
call, `multi round-trip: fulfilling input request "consent": ...`, on either
version. Neither runs the indexer, and a refusal is still a refusal; a client
that matched the old code in the text of the result will not find it. And the
listing carries `"idempotentHint":false` and `"readOnlyHint":false` where it
omitted them, which a client reads as the default it already assumed.

`jsonschema-go` `0.4.3` changes nothing observable here: the tool schemas
compare equal to the ones `0.4.2` produced, apart from the annotations above.

## Not done

`ttlMs` and `cacheScope` on the listings, which `1.8.0` fills in with defaults,
were not looked at beyond the tests passing. The `SDK` is the version that
carries `2026-07-28`, a protocol revision that was a draft when this was
written; the earlier versions stay served.
