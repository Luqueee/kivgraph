---
title: Code Intelligence MCP Server for AI Coding Agents
description: Build a local semantic code graph that gives AI coding agents symbols, references, callers and change impact instead of raw file scans.
---

Kivgraph is a local **code intelligence MCP server** for AI coding agents. It indexes Go, TypeScript, Rust, Python and Dart repositories into a canonical knowledge graph, then answers structural questions about the codebase.

This is **semantic code navigation**, not a text search wrapper. Kivgraph preserves declarations, symbols, callers, callees, repository relationships and unresolved facts with the evidence that produced them.

## What code intelligence answers

An agent can ask:

- Where is this symbol declared?
- Who calls or references it?
- What dependencies does it reach?
- Which repositories consume it?
- What breaks if I change it?
- What source code belongs to the returned symbols?

The MCP tools expose these questions directly through [`find_symbol`](/docs/tools/find-symbol/), [`find_references`](/docs/tools/find-references/), [`trace_dependencies`](/docs/tools/trace-dependencies/), [`get_blast_radius`](/docs/tools/get-blast-radius/) and [`get_source`](/docs/tools/get-source/).

## Code intelligence versus text search

`grep` can find matching text. It cannot distinguish two homonymous methods, prove that a reference is a call to a particular declaration, or show a dependency that crosses repository boundaries. Kivgraph uses the configured language analyzers and keeps `EXACT`, `CANDIDATE` and `UNRESOLVED` results distinct.

An empty result is therefore meaningful only when the response reports sufficient confidence and completeness. Unresolved facts remain visible instead of being silently discarded.

## Local and repository-aware

Indexing, graph queries and MCP serving run locally. A repository is identified explicitly, so the graph can answer cross-repository questions without merging unrelated projects into one anonymous namespace.

Start with the [Quickstart](/quickstart/), then register the server with a [supported MCP client](/mcp/clients/).
