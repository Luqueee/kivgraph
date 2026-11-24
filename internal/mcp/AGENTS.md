# MCP surface instructions (`internal/mcp/`)

These rules add to the `AGENTS.md` at the repository root, which is always
read. An instruction in this file may add restrictions; it can never relax an
integrity, compatibility or verification contract declared at the root.

`internal/AGENTS.md` covers the engine that answers these tools.

## Shape of a response

- A response travels over **a single channel**. No tool publishes
  `outputSchema`, because the SDK would then marshal the typed result into
  `structuredContent` and repeat the same JSON in the text block: the response
  would be paid for twice. `get_source` goes further and answers in prose: 302
  tokens of source are 374 inside a JSON string and 430 as a row, which is what
  the host's range read costs, so serving code through the envelope buys
  nothing.
- The only tool that publishes `outputSchema` is `index_project`, and it is a
  declared exception, not an oversight: its response is a fixed-shape report
  whose weight is the field names -- `1,180` bytes in each channel -- and it is
  emitted once per rebuild, not once per question. The rule was measured on a
  fifty-row page of `find_references`, where the duplicate was `24,066` bytes
  in one pass. Paying twice here buys a client that reads the counters without
  parsing them. `index_project` does not go through `addQueryTool`, so neither
  the `panic` nor the served-surface test reaches it; what pins it is
  `TestIndexProjectIsTheOnlyToolWithAnOutputSchema`, and what that test watches
  for is the second tool that acquires one.

- Every row that names a symbol can be opened without another call: it carries
  repository, path, qualified name and **the full range**. And every tool
  accepts that triple instead of the stable key, which is 35 tokens of base32
  that only this server reads. An ambiguous name is not resolved silently: the
  error names the candidates by where they are, and if repository and path no
  longer separate them, it offers the keys.
- Without a published generation there is no surface: the server completes the
  handshake, publishes zero query tools and puts the rebuild command in
  `instructions`. A client launches the process itself, so exiting reads as a
  crash, and publishing tools that answer `INDEX_NOT_READY` to everything
  teaches the agent that the tools do not work.
- The only exception is requested by hand and not by a client:
  `ServerOptions{ExposeUnavailableTools: true}` -- which is what
  `kivgraph serve --introspection` turns on -- lists the whole catalogue
  without a generation, for the inspector or registry that can only score what
  `tools/list` returns. It changes what is listed and nothing else: it creates
  no index, fabricates no graph, the query tools that depend on the graph keep
  answering `INDEX_NOT_READY` and `graph_status` reports an empty graph state;
  `index_project` stays behind its consent gate and the handshake keeps
  carrying the repair instructions, because availability -- and not the
  option -- is what decides what the client is told. `ServerOptions{}` is the
  behaviour as always, so the zero value cannot become the exception by
  accident; `TestColdStartStillPublishesOnlyTheRepair` pins it.
- The registrations of the query tools live in `registerQueryTools` and only
  there. A catalogue scored from outside and another one served would be two
  lists that drift, and what would be scored would not be what is served;
  `TestIntrospectionServesTheSameToolDefinitions` pins it, comparing
  description and schema tool by tool between the two forms.
- What a host keeps resident is the name of the tool and its description, not
  its schema: Oh My Pi reads the schema on demand and Claude Code defers it.
  That is where the routing lives -against which native alternative each tool
  competes and **where it loses**- and that is why no description or
  `instructions` may carry a number derived from the graph: it would rewrite the
  client's system prompt on every reindex. Volatile data is requested with
  `graph_status`.
- The two halves of a definition are paid for in different places and are
  therefore written to different budgets. The description is what is resident
  and competes against the `MaximumResidentSurfaceBytes` ceiling: only the
  routing fits there. The schema is held in memory by nobody, so **every
  published argument carries its description** -- pinned by
  `TestEveryPublishedArgumentDescribesItself`, which also enters the objects of
  an array -- and that is where what distinguishes `repo` from `repository` is
  explained, which ones bound what is reachable and which only filter the page.
  A description that no longer fits resident is not compensated by raising the
  ceiling: it moves to the argument it belonged to. That is how the budget of
  `find_references` was recovered, whose sentence about the ambiguous name
  lives today in `name`.
- Every query tool annotates `readOnlyClosedWorld()`, never a hand-written
  `ToolAnnotations`. `OpenWorldHint` is `true` when missing, so omitting it
  declares the opposite of what this server does: a response comes from the
  published generation and from nothing else.
- A response declares what its count means only when the figure misleads. Zero
  rows reads as "does not exist" unless something says it is a verified
  absence; a truncated page does not say whether it contains what mattered.
  With rows and not truncated, `guidance` is silent: fifteen tokens of advice on
  every call is how a saving turns into a cost.

## What a tool may claim

- `graph_status` does not report what this process does not use or did not
  measure. `serve` answers from the published HotSnapshot: it does not open the
  database or run the worker, so it declares them `not_applicable` saying why,
  and the metrics sections that nobody observed are omitted instead of being
  zero.
- A per-symbol query counts as a consumer only what was observed on that symbol.
  `find_cross_repo_consumers` returns the package dependencies, which prove that
  the consumer depends on the provider and never that it uses the symbol, in
  their own counter `coverage.package_level`; adding them to `exact` reports a
  use that nobody saw. For the same reason, a resolution failure that named no
  symbol -an unreadable module, an absent provider- belongs to the package and
  is declared with `requested_package`, never attributed to every symbol that
  package exports. That list left the model's surface in phase 19 -it is a
  question about the index, not about the code- and lives in the CLI.
- The four `coverage` counters are **disjoint and about the query itself**. A
  failure that bounds the answer without being evidence about it -an unreadable
  scope of the repository- enters none of them: it is reported in
  `completeness.invisible_scopes` and in `more_invisible_scopes`, which exist
  for that. Changing what a counter counts is a schema change even if the field
  keeps its name and type, and the compiler does not see it: the verdict helper
  was born adding the scopes to `unresolved_related` and spread to five tools,
  until `find_symbol` reported `29` related records for a name nobody
  references. Every gate stayed green because no test pinned the counter on a
  failure that was **only** a scope's; the one that pins it now is
  `TestCompletenessSeparatesAFailedReferenceFromAnUnreadableScope`. See ADR
  0063.
- **Indexing consent is returned as a question, not made.** With protocol
  `2026-07-28` a server cannot send `elicitation/create` during a request:
  `requireIndexConsent` returns `inputRequests` and reads the answer from
  `inputResponses`, and the SDK does the round trip for earlier clients. The
  state that comes back is a digest of the batch, so an old approval is not
  valid for another. An `Elicit` inside a handler breaks every new client
  again. See ADR 0158.
- The `completeness` block is grouped in the `compact` view and is one row per
  scope and per failure in `full`: `completeness_compact.go` is the only
  translation, and it is done when serializing, never when building the
  verdict. A client or harness that counts rows of `invisible_scopes` in a
  compact response counts groups; count the `packages`. It happened with
  `benchmarks/tool-honesty`. See ADR 0156.
- A loader diagnostic that does not bring down the pass is printed, not only
  counted; a TypeScript repository that declares no package is named. A counter
  with no detail and a log entry that contributes nothing are two ways of
  staying silent.
- `serve` may read the files of the registered repositories **only to deliver
  bytes**. It fails closed in what it asserts and degrades declaring in what it
  delivers: if the file no longer matches the generation's `ContentDigest`, the
  file is the authority, the declaration is re-anchored by name and the offset
  is declared; if none is left or two are left, that row gives no bytes and the
  others do. Re-anchoring creates no edge. See ADR 0040.
- The derived provider is withdrawn by default from the four served tools that
  can return one of its rows -`find_symbol`, `find_references`,
  `trace_dependencies` and `get_blast_radius`-, and `include_derived` or naming
  it in `repo` overrides that. Withdrawing a row is a page decision and never a
  claim about what was observed: the edge stays published with its exact
  confidence. `graph_status` breaks it down in `derived`, its unresolved
  included -an edge belongs to the repository of its source symbol, which is the
  side that observed- and `list_repositories` marks the row. Without that
  breakdown, a repository of ten symbols answers `24,704`.
- An incoming `find_references` response on a method includes the references to
  the interface methods it implements, **only where it is the sole
  implementation**: there a call through the interface cannot reach anything
  else. With two it does not bridge -that would trade a false absence for a
  false presence- and `get_blast_radius` keeps crossing `IMPLEMENTS` in both
  directions. Every bridged row carries `via` and the page declares
  `dispatch_through`, so none reads as a direct call. Adding a row is a claim,
  not a page decision: the method-to-method correspondence is emitted by the
  loader with `types.LookupFieldOrMethod`, never by the query by name. Rust does
  not emit it yet. See ADR 0054.
- The forwarding edges -`EXPORTS` and `REEXPORTS`- are withdrawn by default from
  `find_references`, and `edge_kinds` overrides it, with `["*"]` to switch off
  the whole filter. It is the same page decision as the previous one: the edge
  stays published with its exact confidence, `get_blast_radius` does not
  withdraw it -a rename does break the barrel- and `trace_dependencies` does not
  either, because it walks outward and would truncate a real reach. No consumer
  is lost: the checker resolves an import through however many barrels there
  are, so each one brings its own `IMPORTS_SYMBOL`. It is declared in
  `edge_kinds_default_excluded` and the `total` counts what the response has.
  See ADR 0053.

## `index_project`

- An indexing pass never happens in the process that answers queries.
  `Service.IndexProjects` and `Service.Reindex` run `index --full --json` as a
  child process -this same executable, with `--config`, `--repositories` and
  `--resolver-version`- and the peak dies with the child. A pass holds the type
  universe of every Go module, every TypeScript worker and every SCIP index at
  once, and the Go heap keeps the arena: a server that indexed in its own
  process stayed at 1.68 GB of RSS for as long as it lived, with the live heap
  at 173 MB. The parent keeps the registry and its rollback; the child reads the
  registry from disk and publishes the generation; the parent builds the
  `HotSnapshot`, which cannot be delegated because it serves from its own heap.
  See ADR 0042.
- `index_project` is idempotent: a project already registered with the same
  directory is reindexed without touching the registry, and a change of
  languages keeps the `exclusions` the request cannot express. Only a name taken
  by another directory is a conflict, and the error names the registered one.
  `clean` never removes repositories: rebuilding what is registered is
  `index --full`.
- An MCP client launches the server itself, so `serve` and `ui` write the
  default configuration when it does not exist and carry on: exiting because
  nobody ran `init` turns installing the integration into a terminal session and
  the client only reports that the server failed. They register no repository
  and index nothing. An existing configuration that cannot be read is a failure,
  never something to overwrite.
- `index_project` emits `notifications/progress` when the request brings a
  `progressToken`: a full rebuild takes minutes and an MCP client applies its
  own timeout to the call. Without a token no callback is installed.
- `start_index_project` is the portable path for clients with a fixed timeout:
  it returns an `operation_id` immediately and `get_index_status` queries it
  until `completed` or `failed`. The daemon shares the state among its
  sessions; a `serve` over stdio keeps it for the life of that process. The
  state does not survive a restart and keeps at most 32 finished operations. See
  ADR 0114.
- `index_project` accepts a batch (`projects`) and rebuilds **only once**. A
  rebuild resolves the cross-repository edges over the complete set of facts, so
  it costs the entire corpus whatever is added: calling once per project pays
  that cost once per project and throws away every graph but the last. The
  single-project form is kept; mixing both in one request is rejected.

## The published skill

- The skill may only name tools that `internal/mcp/server.go` registers. It is
  what an agent reads before deciding, so a tool the server does not publish is
  not a documentation imprecision: it routes the question to a call that fails.
  It used to send to `get_unresolved_references`, which is not registered, and
  did not mention `get_source` or `get_file_outline`, which are.

## Token cost

- The token cost of the surface is measured, not opined: `benchmarks/mcp-token-cost`
  compares each question against the host's native route with its outputs
  **captured literally**, publishes the factor of answering and that of the
  whole session -one alone misleads- and declares the gate from its digest.

## Verification

```bash
go test ./internal/mcp/...
```

A change to the name, description or schema of a tool is a change of surface:
it requires updating `internal/integrations/assets/kivgraph/SKILL.md`, the
tool reference in `landing/` and the routing table at the root.
