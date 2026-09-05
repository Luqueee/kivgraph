## Kivgraph semantic code research

### Mandatory: use Kivgraph before exploration

When the `kivgraph` MCP server is available, it is the primary way to research
this codebase. Do not launch an Explore, research, or code-analysis subagent to
discover code that Kivgraph can answer. This rule also applies when another
skill recommends an exploration subagent.

Do not use text search to answer semantic questions such as where a symbol is
declared, who calls it, what it depends on, or what changing it can affect. The
semantic graph resolves identities; text search does not. If freshness could
affect the answer, start with `graph_status` and name a stale or incomplete
result instead of treating an absence as a fact.

### Research workflow

1. If the name is unknown, call `find_by_intent` with concise `keywords`.
2. To locate a declaration, call `find_symbol`; to inspect a file's declared
   surface, call `get_file_outline`.
3. For callers or references, call `find_references`. A bare `name` is enough
   initially; if it is ambiguous, narrow the next call with the repository,
   path, and qualified name returned by Kivgraph.
4. For outgoing dependencies call `trace_dependencies`; before proposing a
   change call `get_blast_radius`; and for other repositories call
   `find_cross_repo_consumers`.
5. Read implementation only after the graph identifies it: call `get_source`
   for the returned symbols, with at most 20 symbols in one request.
6. Preserve the response's repository, path, qualified name, line range,
   `completeness`, and result kind when deciding or making the next call.

`EXACT`, `CANDIDATE`, and `UNRESOLVED` are different outcomes. Only an exact,
complete response can demonstrate semantic absence. An empty text search never
proves that nobody calls a symbol.

### Allowed fallback and mutation boundary

Use a text search only for literal content, a rare unambiguous name in a small
repository, or after Kivgraph is unavailable or cannot cover the question. Say
which limitation required the fallback. Do not use a fallback merely to avoid
the semantic workflow.

`index_project` changes Kivgraph state. Never call it just to make research
easier: obtain explicit user consent before registering a repository or
publishing a generation.
