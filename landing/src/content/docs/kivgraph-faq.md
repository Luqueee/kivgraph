---
title: "Kivgraph FAQ: Code Graphs, MCP and Workspaces"
description: Answers about Kivgraph, cross-repository code graphs, MCP clients, token-efficient context and architectural relationships.
---

## What is Kivgraph?

Kivgraph is a local **cross-repository code intelligence MCP server** for AI coding agents. It indexes Go, TypeScript, Rust, Python and Dart repositories into a canonical semantic code graph.

## Is Kivgraph a multi-repository code graph?

Yes. Kivgraph registers repositories explicitly, preserves repository identity and can report supported symbol and dependency relationships across repository boundaries. See [cross-repository code graphs](/cross-repository-code-graph/).

## Is Kivgraph an architecture graph for microservices?

Not primarily. Kivgraph focuses on semantic code relationships: symbols, declarations, references, calls, packages and analyzer-backed dependencies. It does not claim automatic discovery of every HTTP, gRPC, Kafka or database runtime relationship.

## Does Kivgraph replace grep?

No. A rare text lookup in a small repository can be cheaper with `grep`. Kivgraph is useful when the question requires typed references, repository relationships, transitive impact or evidence that an empty result means nobody calls the symbol.

## Does Kivgraph save tokens?

It can reduce exploratory file reads and MCP tool calls by returning structured graph facts before the agent opens source files. The saving depends on the repository and query, so Kivgraph measures comparisons instead of promising one universal percentage. See [token-efficient code understanding](/token-efficient-code-understanding/).

## Which MCP clients does Kivgraph support?

The installer has targets for Claude Code, Claude Desktop, Codex, OpenCode and Oh My Pi. See the [client registration guide](/mcp/clients/), [Claude Code integration](/mcp/claude-code/), [Codex integration](/mcp/codex/) and [Oh My Pi integration](/mcp/oh-my-pi/).

## Is Kivgraph local and private?

The index, canonical store, snapshot and MCP server run locally. Kivgraph does not require a hosted code-search service to answer graph queries.

## Is the project called Kivgraph or KiroGraph?

The project name is **Kivgraph**. It is a distinct project and is not KiroGraph.
