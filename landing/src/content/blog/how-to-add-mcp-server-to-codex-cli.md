---
title: How to Add an MCP Server to OpenAI Codex CLI
description: Use codex mcp add to connect an MCP server to Codex CLI, then verify it with codex mcp list. Includes stdio, HTTP and Kivgraph setup.
pubDate: 2026-08-31
author: Kivgraph
category: MCP setup
tags:
  - MCP server
  - Codex CLI
  - codex mcp add
  - coding agents
  - developer tools
featured: false
faq:
  - question: Where does Codex save MCP server configuration?
    answer: >-
      Codex CLI saves user-level MCP server entries in ~/.codex/config.toml.
      Kivgraph can also write a project-level entry to .codex/config.toml.
  - question: Does adding an MCP server index my repository?
    answer: >-
      No. Adding an MCP server only registers the process or URL with Codex.
      Kivgraph indexing is a separate operation that publishes a graph for
      queries.
  - question: Should I use stdio or HTTP for an MCP server in Codex?
    answer: >-
      Use stdio for a local process and HTTP for a server that is already
      hosted. For source code that should stay on the machine, a local stdio
      server is usually the simpler boundary.
  - question: What should I do if Codex cannot see the server?
    answer: >-
      Run codex mcp list and codex mcp get <name>, check the command or URL,
      and start a fresh Codex session so it reloads the configuration. For
      Kivgraph, also run kivgraph doctor and confirm that a graph is published.
---

Use `codex mcp add` to connect an MCP server to Codex CLI, then run `codex mcp list` to confirm that it is registered. For Kivgraph, use `kivgraph mcp install --scope user --target codex`, publish a repository graph, and start a new Codex session.

## How do you add an MCP server to Codex CLI?

Codex CLI can connect to local MCP servers over stdio and remote servers over
Streamable HTTP. The [official Codex MCP documentation](https://developers.openai.com/codex/mcp)
documents both transports and the `codex mcp` commands used to manage them.

For a local stdio server, pass the server name followed by `--` and the command
that launches it:

```bash
codex mcp add <name> -- <command> [args...]
```

For a remote HTTP server, provide its URL:

```bash
codex mcp add <name> --url https://example.com/mcp
```

If the remote server uses a bearer token, keep the secret in an environment
variable instead of putting it in a command or repository file:

```bash
codex mcp add <name> \
  --url https://example.com/mcp \
  --bearer-token-env-var MCP_TOKEN
```

The [Model Context Protocol architecture](https://modelcontextprotocol.io/specification/2025-06-18/architecture)
defines the client-server boundary behind these commands. The command you
choose should match where the server runs and where its data can safely live.

## How do you verify an MCP server in Codex?

List the configured servers and inspect the entry you just added:

```bash
codex mcp list
codex mcp get <name>
```

Check the command, arguments, URL and enabled state. If the server does not
appear in the current Codex session after it is registered, start a new session
so the client can load the updated configuration.

The configuration for user-level servers lives in `~/.codex/config.toml`. A
typical stdio entry looks like this:

```toml
[mcp_servers.example]
command = "npx"
args = ["-y", "@example/mcp-server"]
enabled = true
```

Codex manages this file through its CLI. Hand-editing it is useful for advanced
configuration, but `codex mcp add`, `codex mcp get` and `codex mcp remove` make
the intended operation easier to inspect.

## How do you connect Kivgraph to Codex?

Kivgraph is a local code-intelligence MCP server for Codex. Its installer writes
the client-specific configuration, including the correct TOML shape:

```bash
kivgraph mcp install --scope user --target codex
```

For a repository-only setup, write the project configuration instead:

```bash
kivgraph mcp install --scope project --target codex
```

User scope writes `~/.codex/config.toml`; project scope writes
`.codex/config.toml` in the current repository. See the [Kivgraph Codex
integration guide](/mcp/codex/) for the exact files and the [client registration
guide](/mcp/clients/) for the other supported coding agents.

## Does Kivgraph need an indexed graph before Codex can use it?

Yes. Connecting the MCP server and preparing its code graph are separate steps.
Register a repository, check the local toolchains, and publish a full graph:

```bash
kivgraph init \
  --repository project=/absolute/path/to/project \
  --languages go,typescript,rust

kivgraph doctor
kivgraph index --full
```

After the index finishes, Codex can ask Kivgraph structural questions about the
workspace. Start with a narrow request such as who calls a function, where a
symbol is declared, or what breaks if an interface changes. The [Kivgraph tool
reference](/docs/mcp-tools/) explains the available queries and their evidence.

## What should you do when Codex cannot see the MCP server?

Separate registration problems from graph problems before changing the setup:

1. Run `codex mcp list` and confirm that the server name is present.
2. Run `codex mcp get <name>` and check its command or URL.
3. For a local server, confirm that the executable is on the `PATH` available to
   Codex and that the command can start without an interactive shell.
4. Start a new Codex session after changing the configuration.
5. For Kivgraph, run `kivgraph doctor` and `kivgraph graph status --root PATH` to check the
   executable, configuration and published graph.

If the server is connected but returns no useful code answers, the MCP
registration is probably working and the graph is the next thing to inspect.
Kivgraph serves queries from a published generation; adding the server alone
does not create one.

## Should you use a local or remote MCP server for code?

Use a local stdio server when it needs direct access to source code that should
remain on the machine. Use remote HTTP when a team needs a shared service,
centralized authentication or data that is already hosted elsewhere.

Whichever transport you choose, inspect the server's source, permissions and
data handling before connecting it to a coding agent. Every MCP tool becomes
part of the context and trust boundary used during development.

## Frequently asked questions

### Where does Codex save MCP server configuration?

Codex CLI saves user-level MCP server entries in `~/.codex/config.toml`.
Kivgraph can also write a project-level entry to `.codex/config.toml`.

### Does adding an MCP server index my repository?

No. Adding an MCP server only registers the process or URL with Codex. Kivgraph
indexing is a separate operation that publishes a graph for queries.

### Should I use stdio or HTTP for an MCP server in Codex?

Use stdio for a local process and HTTP for a server that is already hosted. For
source code that should stay on the machine, a local stdio server is usually
the simpler boundary.

### What should I do if Codex cannot see the server?

Run `codex mcp list` and `codex mcp get <name>`, check the command or URL, and
start a fresh Codex session so it reloads the configuration. For Kivgraph, also
run `kivgraph doctor` and confirm that a graph is published.

## Further reading

- [Kivgraph MCP server for Codex](/mcp/codex/)
- [Register Kivgraph with a client](/mcp/clients/)
- [Kivgraph quickstart](/quickstart/)
- [How to add an MCP server to Claude Code](/blog/how-to-add-mcp-server-to-claude-code/)
- [Codex MCP documentation](https://developers.openai.com/codex/mcp)
