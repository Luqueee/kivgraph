---
title: Kivgraph MCP Server for Oh My Pi
description: Connect Oh My Pi to Kivgraph for local code intelligence, repository relationships and token-efficient code navigation.
---

Kivgraph integrates with **Oh My Pi** as a local code intelligence MCP server. Pi can query symbols, callers, dependencies and change impact through the graph instead of exploring every repository file first.

## Register Kivgraph with Oh My Pi

After installing the Kivgraph binary, register the user-scope MCP server:

```bash
kivgraph mcp install --scope user --target oh-my-pi
```

This writes the `kivgraph` entry to:

```text
~/.omp/agent/mcp.json
```

For one project only, run the command from the project directory:

```bash
kivgraph mcp install --scope project --target oh-my-pi
```

That writes `.omp/mcp.json` in the current repository.

## Build the graph first

Register and publish the repository before asking Pi structural questions:

```bash
kivgraph init \
  --repository project=/absolute/path/to/project \
  --languages go,typescript,rust
kivgraph index --full
```

Then use Oh My Pi to ask questions such as:

```text
Who calls this function across the workspace?
What repository relationships and dependencies are affected by this change?
```

Kivgraph returns repository, file, symbol and line-range evidence where the analyzer can prove the relationship. Read [code intelligence](/code-intelligence/) and [token-efficient code understanding](/token-efficient-code-understanding/) for the query model.
