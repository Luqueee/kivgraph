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
   narrowing a query.
3. Use `find_symbol` for names and qualified names; use `get_symbol` after a
   stable key is known.
4. Use `find_references` for direct incoming or outgoing references and
   `trace_dependencies` for bounded dependency paths.
5. Use `find_cross_repo_consumers` for consumers in another repository and
   `get_blast_radius` for bounded impact analysis.
6. Use `get_unresolved_references` when a missing provider, candidate, or
   unresolved import is relevant to the answer.

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

## Indexing safety

The configured `serve` process may expose `index_project`, which is the only
mutating tool. It requires explicit user approval and can run a full index and
rebuild. Ask for confirmation before using it, preserve the existing project
registry, and never claim success until the new generation and snapshot are
published. Read-only tools must not register projects, edit source files, run
arbitrary queries, or change graph state.
