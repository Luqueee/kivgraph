---
title: Register with a client
description: Register the Ladygraph MCP server with a coding agent, and see exactly what Ladygraph writes into each client configuration file.
---

The release installer does not edit client configuration automatically. After
installing Ladygraph, run the integration commands without `--target` to detect
the coding agents present on this machine and select one or more of them:

```bash
ladygraph mcp install --scope user
ladygraph skill install --scope user
```

Neither command initialises Ladygraph nor indexes any repository. The skill
command is documented separately in [Agent Skill](/mcp/skills/).

Client integrations run on Linux and macOS only. On any other operating system
the command fails instead of guessing a path.

## The selector

Ladygraph checks each client's known local configuration or installation roots
and marks the agents it detected.

| Key | Action |
| --- | --- |
| `↑` / `↓`, or `j` / `k` | Move |
| `space` | Toggle an agent |
| `a` | Select all |
| `n` | Select none |
| `Enter` | Confirm |
| `q` or `Esc` | Cancel |

Detected agents start selected. If none is detected, the selector starts with no
agents selected, and confirming with an empty selection is refused. It respects
`NO_COLOR` and emits no ANSI when the output is redirected. When neither the
output nor standard input is a terminal, the selector refuses to run and tells
you to pass `--target`. Use `--target` for scripted, non-interactive
installation.

## Supported clients

Five targets are supported. `~` is the user's home directory; the project scope
resolves against the current working directory.

| Client | `--target` value | Config file | Format | Where the entry goes |
| --- | --- | --- | --- | --- |
| Claude Code | `claude-code` | `~/.claude.json` (user scope) | JSON | `mcpServers.ladygraph` |
| Claude Code | `claude-code` | `.mcp.json` in the project directory (project scope) | JSON | `mcpServers.ladygraph` |
| Claude Desktop | `claude-desktop` | `~/Library/Application Support/Claude/claude_desktop_config.json` on macOS, `~/.config/Claude/claude_desktop_config.json` on Linux; user scope only | JSON | `mcpServers.ladygraph` |
| Codex | `codex` | `~/.codex/config.toml` (user scope) | TOML | `[mcp_servers.ladygraph]` |
| Codex | `codex` | `.codex/config.toml` in the project directory (project scope) | TOML | `[mcp_servers.ladygraph]` |
| OpenCode | `opencode` | `~/.config/opencode/opencode.json` (user scope) | JSON | `mcp.ladygraph` |
| OpenCode | `opencode` | `opencode.json` in the project directory (project scope) | JSON | `mcp.ladygraph` |
| Oh My Pi | `oh-my-pi` | `~/.omp/agent/mcp.json` (user scope) | JSON | `mcpServers.ladygraph` |
| Oh My Pi | `oh-my-pi` | `.omp/mcp.json` in the project directory (project scope) | JSON | `mcpServers.ladygraph` |

Claude Desktop supports the `user` scope only; asking for `--scope project`
fails. It is also the one target with no local skill target, so it is absent
from the skill selector.

## What gets written

In every case the server is named `ladygraph`, and `command` is the absolute
path of the Ladygraph executable that ran the install command. The examples
below use `/usr/local/bin/ladygraph`; your path is whatever the running binary
resolves to.

Ladygraph adds only its own entry. Existing servers, keys and top-level settings
in the file are preserved. JSON files are re-encoded with two-space indentation.

### Claude Code

Written to `~/.claude.json` in the user scope and to `.mcp.json` in the project
directory in the project scope.

```json
{
  "mcpServers": {
    "ladygraph": {
      "command": "/usr/local/bin/ladygraph",
      "args": ["serve"]
    }
  }
}
```

### Claude Desktop

Same shape as Claude Code. The file lives at
`~/Library/Application Support/Claude/claude_desktop_config.json` on macOS and
`~/.config/Claude/claude_desktop_config.json` on Linux.

```json
{
  "mcpServers": {
    "ladygraph": {
      "command": "/usr/local/bin/ladygraph",
      "args": ["serve"]
    }
  }
}
```

### Codex

Appended to `~/.codex/config.toml` in the user scope, or to
`.codex/config.toml` in the project directory in the project scope. The table is
appended at the end of the file; the rest of the TOML is untouched.

```toml
[mcp_servers.ladygraph]
command = "/usr/local/bin/ladygraph"
args = ["serve"]
```

### OpenCode

Written to `~/.config/opencode/opencode.json` in the user scope and to
`opencode.json` in the project directory in the project scope. OpenCode's
document shape differs: the section key is `mcp`, `command` is an array holding
the executable and its arguments, and the entry carries `type` and `enabled`.

```json
{
  "mcp": {
    "ladygraph": {
      "type": "local",
      "command": ["/usr/local/bin/ladygraph", "serve"],
      "enabled": true
    }
  }
}
```

### Oh My Pi

Written to `~/.omp/agent/mcp.json` in the user scope and to `.omp/mcp.json` in
the project directory in the project scope.

```json
{
  "mcpServers": {
    "ladygraph": {
      "command": "/usr/local/bin/ladygraph",
      "args": ["serve"]
    }
  }
}
```

## Scope

`--scope user` is the default. It writes into the client's own configuration
under your home directory, so the registration applies to every project you open
with that agent.

`--scope project` writes into a file inside the current working directory, so
the registration travels with the repository and applies only there. Claude
Desktop has no project-scoped configuration file and rejects this scope.

## Safety

- The file is parsed as JSON or TOML before it is modified. A file that does not
  parse, a top-level value that is not an object, or a section (`mcpServers`,
  `mcp`, `mcp_servers`) that is not an object or table, is an error and the file
  is left alone.
- A destination that is a symlink is refused, as is a destination that exists but
  is not a regular file.
- Writes are atomic: the new content goes to a temporary file in the same
  directory with mode `0600`, is synced, and is then renamed over the
  destination. Missing parent directories are created with mode `0700`.
- Before a replacement or a removal, the previous content is copied to a backup
  next to the file, with the suffix `.ladygraph.bak` appended to the full
  filename, for example `~/.claude.json.ladygraph.bak`. An existing backup is
  kept as it is, so the backup always holds the state from before the first
  Ladygraph write. A backup path that is a symlink or not a regular file is
  refused.
- An entry named `ladygraph` that does not match what Ladygraph writes is
  reported as `incompatible` and stops the command with an error. `--force` is
  required to replace or remove it.
- An entry that already matches is reported as `managed` and the file is not
  rewritten.
- `--dry-run` reports the plan as `would-install` or `would-remove` and writes
  nothing.

## Inspect and remove

Both `status` and `remove` require `--target`; there is no selector for them.

```bash
ladygraph mcp status --target claude-code --scope user
ladygraph mcp remove --target claude-code --scope user
```

`status` reads the file and reports `absent`, `managed` or `incompatible`
together with the path it inspected, and changes nothing.

`remove` withdraws only Ladygraph's own entry; every other server and setting in
the client configuration is left as it was. The file itself is never deleted. If
the entry is not there, the command reports `absent` and does nothing.

## After registering

The client launches `ladygraph serve` itself and speaks MCP over stdio. `serve`
opens no HTTP port; the graph viewer is a separate opt-in command.

With no published generation, `serve` still completes the handshake. It exposes
only `index_project`, and its `instructions` tell the agent to run
`ladygraph index --full` and restart the server. Nothing is broken: there is
simply no graph to answer from yet.

`serve` also writes the default configuration when none exists, and continues,
because a client spawns the server itself and exiting because nobody ran `init`
reads as a crash. That default registers no repository and indexes nothing. An
existing configuration that cannot be read is a failure and is never
overwritten.

Next: [Quickstart](/quickstart/) to build the first graph, or
[Troubleshooting](/mcp/troubleshooting/) if the client reports no tools.
