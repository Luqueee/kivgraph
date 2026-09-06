---
title: Kivgraph MCP Server for Codex
description: Connect Codex to Kivgraph for local semantic code search, repository relationships and impact analysis.
---

Kivgraph can serve as a local **code intelligence MCP server for Codex**. It exposes repository structure, symbols, references and bounded impact through the MCP configuration used by Codex.

## Register Kivgraph with Codex

After installing Kivgraph, run:

```bash
kivgraph mcp install --scope user --target codex
```

This writes the `kivgraph` server entry to `~/.codex/config.toml`. For a project-only configuration:

```bash
kivgraph mcp install --scope project --target codex
```

The project entry is written to `.codex/config.toml` in the current repository.

## Build the repository graph

Register the repository and publish a full graph:

```bash
kivgraph init \
  --repository project=/absolute/path/to/project \
  --languages go,typescript,rust
kivgraph index --full
```

Codex can then use Kivgraph for semantic code navigation and questions about repository relationships, callers, dependencies and change impact.

Start with [`find_references`](/docs/tools/find-references/) for direct references or [`get_blast_radius`](/docs/tools/get-blast-radius/) before a risky change.

## See when Kivgraph is used

Kivgraph asks Codex to post a short preamble before every MCP call, naming
the tool, target and question it will answer. For example:

`Kivgraph · find_references — NewServer: check who calls it.`

This is a best-effort instruction to the agent, not a guaranteed UI event.
Reconnect Codex to the updated server to receive the rule. The optional
[Agent Skill](/mcp/skills/#visible-tool-use) carries the same instruction and
explains how to preserve customizations in existing skill installations.
