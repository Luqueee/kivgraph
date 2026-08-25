---
title: Kivgraph MCP Server for Claude Code
description: Connect Claude Code to Kivgraph for local code intelligence, repository relationships and change impact analysis.
---

Kivgraph gives **Claude Code** a local semantic code graph for repository navigation. Claude can ask for symbols, references, callers, dependencies and change impact through MCP instead of scanning the repository from scratch.

## Install the integration

After installing the Kivgraph binary, register it in the user configuration:

```bash
kivgraph mcp install --scope user --target claude-code
kivgraph skill install --scope user --target claude-code
```

The user-scope MCP entry is written to `~/.claude.json`. For a project-only setup:

```bash
kivgraph mcp install --scope project --target claude-code
kivgraph skill install --scope project --target claude-code
```

The project-scope entry is written to `.mcp.json` in the current repository.

## Index before asking questions

Register and publish the graph before starting a structural query:

```bash
kivgraph init \
  --repository project=/absolute/path/to/project \
  --languages go,typescript,rust
kivgraph index --full
```

Then ask Claude Code questions such as:

```text
Who calls this symbol, including consumers in another repository?
What breaks if I change this interface?
```

See [code intelligence](/code-intelligence/) and [repository relationships](/repository-relationships/) for the query model.
