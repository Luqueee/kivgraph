## Kivgraph code intelligence

When the `kivgraph` MCP server is available, prefer it for semantic code
navigation:

- use `find_by_intent` when you do not know the symbol name;
- use `find_symbol` to locate declarations and `get_source` to read their code;
- use `find_references` for callers and references, `trace_dependencies` for
  outgoing dependencies, and `get_blast_radius` for bounded impact;
- use `find_cross_repo_consumers` when the question crosses repositories;
- use `get_file_outline` when you need a file's declarations without reading
  the whole file.

Treat `EXACT`, `CANDIDATE`, and `UNRESOLVED` as different results. Read the
response's `completeness` before concluding that an absence is authoritative.
Use a text search for literal content or when Kivgraph is unavailable.
