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

It does not win every question. On the 29-question benchmark plain `grep` costs fewer tokens on five of them, all single-repository lookups of a rare name where both approaches answer correctly. The workspace graph earns its cost on common names, on transitive impact and on consumers in another repository; the split is in the [comparison](/comparison/).

## Supported workspace model

The workspace graph is strongest when repositories share analyzable code contracts, packages and symbols. It does not claim automatic discovery of every runtime relationship between independent services. HTTP, gRPC, event-bus and database edges require explicit supported evidence or remain outside the semantic graph.

Start with the [cross-repository code graph guide](/cross-repository-code-graph/) and then [register Kivgraph with an MCP client](/mcp/clients/).

`kivgraph mcp install` has five targets: `claude-code`, `claude-desktop`, `codex`, `opencode` and `oh-my-pi`. Claude Desktop is the exception in two ways — it is user-scope only, and it is the one target that installs no local skill.
