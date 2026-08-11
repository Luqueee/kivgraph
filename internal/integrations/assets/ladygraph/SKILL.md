---
name: ladygraph
description: Use Ladygraph's local MCP server to search symbols, references, dependencies, cross-repository consumers, graph status, and unresolved references in the indexed code graph.
license: Apache-2.0
compatibility: Requires the Ladygraph MCP server registered as `ladygraph`.
---

# Ladygraph

Use the `ladygraph` MCP server when the task needs evidence from the indexed
Go or TypeScript repositories rather than a text-only search.

## Workflow

1. Start with `graph_status` to check that a published snapshot exists, its
   age, repository coverage, and whether the index is ready.
2. Use `list_repositories` to identify the repository and language before
   narrowing a query. Repository names are case sensitive: two repositories
   whose names differ only in case are two repositories.
3. Use `find_symbol` for names and qualified names; use `get_symbol` after a
   stable key is known.
4. Use `find_references` for direct incoming or outgoing references and
   `trace_dependencies` for bounded dependency paths.
5. Use `find_cross_repo_consumers` for consumers in another repository and
   `get_blast_radius` for bounded impact analysis.
6. Use `get_unresolved_references` when a missing provider, candidate, or
   unresolved import is relevant to the answer.

## Reading `graph_status`

`status`, `snapshot_id` and `snapshot_age_ms` are what say whether a graph is
being served. Read those.

`storage` and `worker` answer `not_applicable` in a `serve` process, with the
reason: the server responds from the published snapshot and never opens the
database or runs the TypeScript worker. That is not a misconfiguration.

`metrics` reports only what this process measured. `metrics.queries` is
present; the index, snapshot, worker and storage sections are absent unless
the same process performed that work, because a section full of zeros reads
exactly like an empty graph.

## Evidence rules

- Treat the snapshot as a projection of the canonical graph, not as a reason
  to invent facts.
- Distinguish `EXACT`, `CANDIDATE`, and `UNRESOLVED`; never upgrade a candidate
  or unresolved result based only on a matching name, path, alias, or text.
- Preserve repository, language, file, position, reason, and detail when
  reporting an unresolved result.
- Include the relevant stable key, repository, file path, and snapshot age in
  explanations when they make the result auditable.
- Respect result limits and cursors. Narrow the query instead of pretending a
  truncated response is complete.
- If the snapshot is missing or stale for the requested question, say so and
  ask for a local re-index when appropriate; do not read LadybugDB directly
  through MCP.

## Reading an unresolved reason

Unresolved references are facts about the workspace, not defects of the tool.
Report the reason rather than concluding that coverage is broken:

- `MODULE_PROVIDER_NOT_FOUND`, `PACKAGE_PROVIDER_NOT_FOUND`: nothing
  registered provides that module or package. Either the provider is not in
  the registry, or the consumer reads it from a build output that does not
  exist yet.
- `PROVIDER_SOURCE_UNAVAILABLE`, `DECLARATION_SOURCE_NOT_MAPPED`: the provider
  is known but its sources or declaration maps are not, which is what a
  package consumed from `dist` without source maps looks like.
- `MODULE_NOT_LOADED`: the Go loader could not read that module, usually
  because its dependencies were never downloaded. The repository's facts are
  absent on purpose. `go mod download` in it and reindexing is the fix.
- `PACKAGE_NOT_BUILDABLE`: build constraints selected no file. A tag the index
  does not set is a configuration answer, not a missing symbol.

A repository registered as TypeScript that declares no package contributes
nothing to the graph; the index reports it by name.

## Indexing

`index_project` is the only mutating tool. It requires explicit user approval,
registers projects and rebuilds the whole graph. Ask for confirmation first,
and never claim success until the new generation and snapshot are published.

- Pass every project in one call through `projects`. A rebuild resolves
  cross-repository edges over the complete fact set, so it costs the whole
  corpus whatever was added: eleven separate calls build eleven graphs and
  keep the last one.
- The call reports `notifications/progress` per unit of work. A full rebuild
  can outlive the timeout a client applies to a single call; if the client
  gives up, the work still completes and publishes. Verify with `graph_status`
  -- a `snapshot_id` that advanced means the pass finished -- rather than
  retrying, which starts the whole rebuild again.
- Reindexing after a change is much cheaper than the first pass: unindexed
  work is served from the fact cache, and only the units whose inputs changed
  are analysed again.

Read-only tools must not register projects, edit source files, or change graph
state.
