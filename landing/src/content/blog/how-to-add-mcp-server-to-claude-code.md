---
title: "How to Add an MCP Server to Claude Code: Step by Step"
description: Use claude mcp add to add an MCP server to Claude Code, then verify it with /mcp. This guide covers HTTP, stdio and Kivgraph setup.
pubDate: 2026-08-31
author: Kivgraph
category: MCP setup
tags:
  - MCP server
  - Claude Code
  - claude mcp add
  - coding agents
  - developer tools
featured: true
faq:
  - question: Does installing an MCP server index my code?
    answer: >-
      No. Registering an MCP server only makes its process available to Claude
      Code. With Kivgraph, kivgraph init registers repositories and
      kivgraph index --full analyzes and publishes the graph.
  - question: Can one MCP server work with several coding agents?
    answer: >-
      Yes, when each client supports the same MCP transport and configuration
      shape. Kivgraph provides targeted installation for Claude Code, Claude
      Desktop, Codex, OpenCode and Oh My Pi; see the client registration guide
      for the supported scopes and files.
  - question: How is an MCP server different from a coding agent?
    answer: >-
      The agent decides what to do and communicates with the user. The MCP
      server exposes tools and structured context that the agent can call.
      Kivgraph is the code-intelligence layer: it answers questions about
      symbols, references, dependencies and change impact while the agent
      remains in control of the coding workflow.
  - question: What should I ask first after connecting a code MCP server?
    answer: >-
      Ask one question that has a verifiable answer, such as where this symbol
      is declared or who calls this function. Then inspect the returned source
      and evidence before asking for an edit. A small structural query is a
      better connection test than asking the agent to change a large feature
      immediately.
---

Use `claude mcp add` to register an MCP server in Claude Code. Choose `--transport http` for a remote server or `--transport stdio` for a local process. For Kivgraph, install the binary, run `kivgraph mcp install --scope user --target claude-code`, index your repository, and verify the connection with `/mcp`.

## How do you add an MCP server to Claude Code?

Claude Code supports MCP servers that run locally over stdio and remote servers that use HTTP. The [official Claude Code MCP documentation](https://code.claude.com/docs/en/mcp) recommends choosing the transport that matches the server. The [MCP architecture specification](https://modelcontextprotocol.io/specification/2025-06-18/architecture) explains the client-server boundary: use stdio for a local process with direct access to a workspace, and HTTP for a hosted service.

For a generic server, the Claude Code CLI uses one of these forms:

```bash
# Remote HTTP server
claude mcp add --transport http <name> <url>

# Local stdio server
claude mcp add --transport stdio <name> -- <command> [args...]
```

The options go before the server name. The `--` separates Claude Code options from the command and arguments that start the local server.

## How do you add Kivgraph to Claude Code?

Kivgraph includes an installer that writes the correct MCP entry for Claude Code. After installing Kivgraph, run:

```bash
kivgraph mcp install --scope user --target claude-code
kivgraph skill install --scope user --target claude-code
```

The first command registers Kivgraph with the client. The second installs the instructions that tell an agent when to use its semantic graph. Neither command initializes a repository or indexes source code.

If Kivgraph is not on your `PATH`, run the commands from the directory that contains the installed launcher or use the full path to `kivgraph`.

## Should you use user scope or project scope?

Use **user scope** when the server should be available in every project for your account. Claude Code stores that registration in `~/.claude.json`.

Use **project scope** when the configuration belongs to one repository and should be shared with the team:

```bash
kivgraph mcp install --scope project --target claude-code
kivgraph skill install --scope project --target claude-code
```

Project scope writes `.mcp.json` in the current repository. Review that file before committing it, especially if a server needs credentials or accesses systems outside the workspace. A local code-intelligence server is often a good fit for project scope when every contributor needs the same tool, but user scope keeps personal tooling out of the repository.

## How do you prepare Kivgraph before asking questions?

An MCP connection does not create a code graph by itself. Register the repository and publish a generation first:

```bash
kivgraph init \
  --repository project=/absolute/path/to/project \
  --languages go,typescript,rust

kivgraph doctor
kivgraph index --full
```

You can register more than one repository by repeating `--repository`. The full index is what makes cross-repository questions possible. If there is no published generation, the server can connect but has no graph to query.

## How do you verify that the MCP server is connected?

Inside Claude Code, run:

```text
/mcp
```

The MCP panel shows the configured server and the tools it exposes. With Kivgraph connected and indexed, ask a narrow structural question such as:

```text
Who calls this function?
What breaks if I change this interface?
Which repository consumes this exported type?
```

These questions test more than whether the process started. They check that the client can reach the server, that the graph is published and that the returned context is useful for the task.

## What should you do when Claude Code cannot see the server?

Start with the smallest distinction that explains the failure:

1. Run `claude mcp list` to check that the server is registered.
2. Run `/mcp` in Claude Code to check its connection and tools.
3. Run `kivgraph doctor` to check the executable, configuration and graph state.
4. Run `kivgraph mcp install --dry-run --scope user --target claude-code` to inspect what the installer would write.
5. If the server is connected but has no useful answers, run `kivgraph index --full` and retry a narrow query.

For a project-scoped server, confirm that `.mcp.json` is at the repository root and that Claude Code has approved the project configuration. For a stdio server, confirm that the command is executable from the client environment; a command that works in an interactive shell may not be on the `PATH` used by a desktop application.

## Is a local or remote MCP server better for code?

For source code, a local stdio server is usually the simpler security boundary: the process can read the workspace without uploading the repository to a hosted service. A remote HTTP server can be the right choice when a team needs a shared service, centralized authentication or access to data that is already hosted.

The important question is what the server can prove. Before connecting one, verify its source, transport, permissions and data handling. Do not add a server merely because it advertises many tools; every tool becomes part of the context and trust boundary your coding agent uses.

## Frequently asked questions

### Does installing an MCP server index my code?

No. Registering an MCP server only makes its process available to Claude Code. With Kivgraph, `kivgraph init` registers repositories and `kivgraph index --full` analyzes and publishes the graph.

### Can one MCP server work with several coding agents?

Yes, when each client supports the same MCP transport and configuration shape. Kivgraph provides targeted installation for Claude Code, Claude Desktop, Codex, OpenCode and Oh My Pi; see the [client registration guide](/mcp/clients/) for the supported scopes and files.

### How is an MCP server different from a coding agent?

The agent decides what to do and communicates with the user. The MCP server exposes tools and structured context that the agent can call. Kivgraph is the code-intelligence layer: it answers questions about symbols, references, dependencies and change impact while the agent remains in control of the coding workflow.

### What should I ask first after connecting a code MCP server?

Ask one question that has a verifiable answer, such as where this symbol is declared or who calls this function. Then inspect the returned source and evidence before asking for an edit. A small structural query is a better connection test than asking the agent to change a large feature immediately.

## Further reading

- [Kivgraph MCP server for Claude Code](/mcp/claude-code/)
- [Register Kivgraph with a client](/mcp/clients/)
- [Kivgraph quickstart](/quickstart/)
- [Claude Code MCP documentation](https://code.claude.com/docs/en/mcp)
