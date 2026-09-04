# ADR 0094: Codex MCP consent compatibility

**Status:** accepted
**Date:** 2026-09-04

## Context

`index_project` changes the persistent project registry and can publish a new
graph generation. Kivgraph therefore asks MCP clients with form elicitation
support for an interactive approval. Clients without that support must send
`confirmed: true` only after obtaining approval through their own UI.

Codex Desktop currently exposes its native tool-approval flow but does not
complete Kivgraph's server-side form elicitation. The server consequently
receives a declined elicitation and returns `PERMISSION_DENIED`, even when the
tool call already carries `confirmed: true`.

## Decision

Consent selection follows these rules:

1. A client identified as `codex` (including a name with a `codex` prefix) uses
   its native tool approval together with the explicit `confirmed: true`
   fallback.
2. A client that advertises only URL elicitation uses the same fallback because
   `index_project` requests a form.
3. Other clients that advertise form elicitation must return `accept` with
   `confirmed: true` from the elicitation request.
4. Clients without elicitation continue to use the explicit fallback.

The `confirmed` field remains mandatory for every fallback path. Client
identity is an interoperability hint, not an authentication boundary: an MCP
client can spoof its initialization name. The fallback is a user-interface
contract and must not be treated as authorization for an untrusted client.

## Alternatives considered

- Always accepting `confirmed: true` would make the current form-consent
  contract ineffective for clients that support elicitation.
- Rejecting Codex would leave the mutating tool unusable from Codex Desktop
  until its elicitation support changes.
- Requiring URL elicitation would not work for this approval because the
  requested schema is a simple form and the operation has no external URL flow.

## Consequences

- Codex users can approve `index_project` through the normal tool-approval
  prompt and the call can proceed without a second unsupported MCP prompt.
- MCP clients with working form elicitation retain the stronger interactive
  server-side path.
- If Codex adds working form elicitation, this compatibility branch can be
  removed after the client behavior is verified and the ADR is superseded.
