---
title: Workspace Code Intelligence for AI Coding Agents
description: Give Claude Code, Codex and other MCP clients structured context across a multi-repository workspace with Kivgraph.
---

**Workspace code intelligence** means giving an AI coding agent the relationships it needs across projects, not only the contents of the file currently open.

Kivgraph indexes named repositories into one canonical graph and serves local MCP queries for symbols, references, repository relationships and change impact.

## A workspace example

```text
workspace/
├── frontend/
├── api/
├── auth-service/
├── payments-service/
└── shared-types/
```

The graph keeps these repositories distinct while allowing supported relationships to cross their boundaries. An agent can start from a symbol in `shared-types`, find consumers in `frontend` and `api`, then inspect the bounded impact before editing.

## Why this is useful to an agent

Without a repository-aware index, an agent may need repeated search and file reads to discover:

- where a symbol is declared;
- which repository owns it;
- who references it;
- which package provides an import;
- what downstream code may be affected.

Kivgraph exposes those questions through MCP tools and preserves the evidence attached to each result. This can reduce exploratory tool calls while keeping the answer addressable by repository, path, symbol and line range.

## Supported workspace model

The workspace graph is strongest when repositories share analyzable code contracts, packages and symbols. It does not claim automatic discovery of every runtime relationship between independent services. HTTP, gRPC, event-bus and database edges require explicit supported evidence or remain outside the semantic graph.

Start with the [cross-repository code graph guide](/cross-repository-code-graph/) and then [register Kivgraph with an MCP client](/mcp/clients/).
