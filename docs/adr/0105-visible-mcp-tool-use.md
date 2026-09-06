# ADR 0105: announce Kivgraph tool use in the agent's chat

- Date: 2026-09-03
- Status: accepted
- Compatibility: connection instructions and the shipped skill change; tool
  names, schemas, results, consent gates and event logs do not.

## Decision

Before every Kivgraph MCP call, ask the agent for a brief user-visible preamble
in the conversation's language. It names the exact tool, its target (symbol,
file, repository or scope), and the question it will answer. For example:

`Kivgraph · find_references — NewServer: check who calls it.`

Every repeated call is announced. Parallel calls may share a preamble with a
separate line for each call. The notice describes intent, not success, avoids
dumping arguments or secrets, and never substitutes for indexing consent.
There is no required completion notice or new configuration setting. CLI
commands are outside this contract.

Put the policy first in both healthy and cold MCP connection instructions.
The cold case matters because an indexing server can expose `index_project`
without a graph. Reuse one constant for those two states, retain the existing
routing guidance, and stay below the existing 2 KB instruction budget.

The shipped skill carries the same paragraph. Its installed form and the
actual MCP handshake are compared in an integration test, across all skill
targets and both installation scopes. Claude Desktop has no local skill and
depends on the connection instructions alone.

## Limits and alternatives

This is best effort, not a new protocol event. The client decides whether to
pass instructions to its model and whether to display the resulting preamble.
MCP cannot force an assistant message into an arbitrary client's chat. Logging
or progress notifications do not provide that guarantee either; emitting them
for this purpose would also fail to express the agent's reason before the call.
No model-compliance claim follows from the integration test.

## Rollout and validation

Update the running server and reconnect clients to obtain the new instructions.
No graph rebuild or data migration is needed. Fresh skill installations include
the rule; existing canonical skills and project copies remain untouched. Users
can merge the section into an existing skill without discarding local edits.
Do not force-reinstall customized skills as part of this change.

Run the MCP and integration suites, CLI race suite, `go vet ./...`, `make build`,
the documentation gate, and the landing check/build. Preserve the existing
permission-denial tests: a preamble does not authorize `index_project`.

For client UI qualification, start a fresh chat connected to the updated server
and use an isolated indexed fixture. Request callers of a symbol, then source
for a returned declaration (sequential calls); request two independent symbol
queries in parallel; repeat a query. Check that each actual call has a preceding
notice with tool, target and purpose in the user's language, including each
parallel call. Compare notices against the client's call trace, not an agent's
claim that it followed the rule. Record client/model versions and any omissions.
Test Desktop without a skill; test the other clients with and without it.
These UI checks must be reported separately from automated protocol tests.

### Verification after branch reconciliation on 2026-09-06

The delivery and protocol paths passed with:

```bash
go test ./internal/mcp/... ./internal/integrations
go test -race ./cmd/kivgraph/...
go vet ./...
make build
pnpm --dir landing check
pnpm --dir landing build
```

The landing build emitted no route warning after `/404` moved to the framework
route. Actual chat rendering and model compliance have not been qualified in
Codex, Claude Code, Desktop, OpenCode or Oh My Pi.
