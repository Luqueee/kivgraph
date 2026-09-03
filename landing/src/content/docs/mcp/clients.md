---
title: Register with a client
description: Register the Kivgraph MCP server with a coding agent, and see exactly what Kivgraph writes into each client configuration file.
---

The release installer does not edit client configuration automatically. After
installing Kivgraph, run the integration commands without `--target` to detect
the coding agents present on this machine and select one or more of them:

```bash
kivgraph mcp install --scope user
kivgraph skill install --scope user
```

Neither command initialises Kivgraph nor indexes any repository. The skill
command is documented separately in [Agent Skill](/mcp/skills/).

Client integrations run on Linux and macOS only. On any other operating system
the command fails instead of guessing a path.

## The selector

Kivgraph checks each client's known local configuration or installation roots
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
| Claude Code | `claude-code` | `~/.claude.json` (user scope) | JSON | `mcpServers.kivgraph` |
| Claude Code | `claude-code` | `.mcp.json` in the project directory (project scope) | JSON | `mcpServers.kivgraph` |
| Claude Desktop | `claude-desktop` | `~/Library/Application Support/Claude/claude_desktop_config.json` on macOS, `~/.config/Claude/claude_desktop_config.json` on Linux; user scope only | JSON | `mcpServers.kivgraph` |
| Codex | `codex` | `~/.codex/config.toml` (user scope) | TOML | `[mcp_servers.kivgraph]` |
| Codex | `codex` | `.codex/config.toml` in the project directory (project scope) | TOML | `[mcp_servers.kivgraph]` |
| OpenCode | `opencode` | `~/.config/opencode/opencode.json` (user scope) | JSON | `mcp.kivgraph` |
| OpenCode | `opencode` | `opencode.json` in the project directory (project scope) | JSON | `mcp.kivgraph` |
| Oh My Pi | `oh-my-pi` | `~/.omp/agent/mcp.json` (user scope) | JSON | `mcpServers.kivgraph` |
| Oh My Pi | `oh-my-pi` | `.omp/mcp.json` in the project directory (project scope) | JSON | `mcpServers.kivgraph` |

Claude Desktop supports the `user` scope only; asking for `--scope project`
fails. It is also the one target with no local skill target, so it is absent
from the skill selector.

## Visible tool use

The server's connection instructions ask the agent to announce every Kivgraph
MCP call in chat, with the tool, target and purpose. The local skill carries
the same rule where supported. These notices are best effort, not messages
injected by Kivgraph into the client UI; see [Visible tool use](/mcp/skills/#visible-tool-use)
for examples, client limitations and upgrade behavior.

## What gets written

In every case the server is named `kivgraph`, and `command` is the absolute
path of the Kivgraph executable that ran the install command. The examples
below use `/usr/local/bin/kivgraph`; your path is whatever the running binary
resolves to.

Kivgraph adds only its own entry. Existing servers, keys and top-level settings
in the file are preserved. JSON files are re-encoded with two-space indentation.

### Claude Code

Written to `~/.claude.json` in the user scope and to `.mcp.json` in the project
directory in the project scope.

```json
{
  "mcpServers": {
    "kivgraph": {
      "command": "/usr/local/bin/kivgraph",
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
    "kivgraph": {
      "command": "/usr/local/bin/kivgraph",
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
[mcp_servers.kivgraph]
command = "/usr/local/bin/kivgraph"
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
    "kivgraph": {
      "type": "local",
      "command": ["/usr/local/bin/kivgraph", "serve"],
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
    "kivgraph": {
      "command": "/usr/local/bin/kivgraph",
      "args": ["serve"]
    }
  }
}
```

## One process for many clients

`kivgraph mcp install` registers a client against **one daemon** by default: it
writes a `url` entry, not a `command`. Every client shares that process instead
of spawning a `serve` of its own.

```bash
kivgraph mcp install --target claude-code
```

That installs the daemon's supervisor if it is missing, waits for the endpoint,
and reports both. What it saves, measured on a workspace of `108.737` symbols in
`benchmarks/daemon-cost`: at eight clients, `61 MB` against `328` when they are
asking questions, and `11` against `80` when they are not. The crossover is
around one client, so with a single editor open it is a coin flip; from the
second it is not close.

`--stdio` is the way out, and it writes exactly what the previous default wrote:

```bash
kivgraph mcp install --target claude-code --stdio
```

Three conditions write the stdio entry on their own, and each says so:
`--scope project`, because a url entry carries a token and that file gets
committed; a platform with no supported supervisor, because the daemon would
have no owner there; and a machine with no configuration yet, because there is
no state directory to point at. Passing `--daemon` explicitly refuses instead of
falling back, which is the point of passing it.

Reinstalling replaces the entry it finds, so switching transports leaves one
registration and not two. An entry naming a **different** kivgraph install still
needs `--force`: that one belongs to another installation.

`daemon install` gives the daemon an owner: a launchd agent on macOS, a systemd
user unit on Linux. Both start it with your session and bring it back if it dies,
which is what makes a `url` entry safe to depend on -- a `command` entry is owned
by the client that spawns it, but a url pointing at a daemon nobody restarts
takes every client down at once. `kivgraph daemon status` says whether one is
installed and where its unit lives, and `kivgraph daemon remove` takes it out.

The unit is keyed by the state directory and not by the configuration file, so
two configurations that keep their graph in different places get two units and
neither replaces the other -- but two that share a state directory share one
unit, and installing the second overwrites the first with its own `--config`.
The **address** is keyed by nothing at all: the daemon binds `127.0.0.1:7788`
unless you tell it otherwise, so the second one to start cannot bind and gives
up. Two supervised daemons on one machine need a free address given to at
least one of them, as in `kivgraph daemon install --addr 127.0.0.1:7789`. Nothing supervises a
daemon you start by hand with `kivgraph daemon &`; it dies with the shell that
launched it.

The url and its token come from `~/.local/state/kivgraph/daemon.json`, mode
`0600`. HTTP is the door that matters: no MCP client configuration dials a unix
socket -- it takes an executable or a url.

The daemon binds loopback. `--allow-remote` is required to bind anywhere else,
because the answers carry repository paths, file paths and symbol names.

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
  next to the file, with the suffix `.kivgraph.bak` appended to the full
  filename, for example `~/.claude.json.kivgraph.bak`. An existing backup is
  kept as it is, so the backup always holds the state from before the first
  Kivgraph write. A backup path that is a symlink or not a regular file is
  refused.
- An entry named `kivgraph` that does not match what Kivgraph writes is
  reported as `incompatible` and stops the command with an error. `--force` is
  required to replace or remove it.
- An entry that already matches is reported as `managed` and the file is not
  rewritten.
- `--dry-run` reports the plan as `would-install` or `would-remove` and writes
  nothing.

## Inspect and remove

Both `status` and `remove` require `--target`; there is no selector for them.

```bash
kivgraph mcp status --target claude-code --scope user
kivgraph mcp remove --target claude-code --scope user
```

`status` reads the file and reports `absent`, `managed` or `incompatible`
together with the path it inspected, and changes nothing.

`remove` withdraws only Kivgraph's own entry; every other server and setting in
the client configuration is left as it was. The file itself is never deleted. If
the entry is not there, the command reports `absent` and does nothing.

## After registering

The client launches `kivgraph serve` itself and speaks MCP over stdio. `serve`
listens on no port; the graph viewer is a separate opt-in command.

**`serve` is usually not the server.** It forwards the session to the daemon
and holds no graph of its own, which is what makes a stdio registration cost
what a shared one costs. Measured on a workspace of `186,159` symbols in
`benchmarks/relay-cost`: a `serve` answering questions holds `68.9`-`70.3 MB`
per client where a forwarding one holds `8.7`-`9.8`, and eight of them peak at
`2.5 GB` against `0.44`.

If this machine has no supervised daemon, `serve` installs one and says so on
stderr, naming `kivgraph daemon remove`. It installs nothing when a unit
already exists: a unit whose daemon is not running is a daemon you stopped, and
starting it again would leave `kivgraph stop` unable to stop anything.

Four things make it answer from its own graph instead, and none is an error:
no daemon is published, nothing answers where one says it is, a platform has no
supervisor to install, or `KIVGRAPH_SERVE_IN_PROCESS` is set. The last is the
way out if the forwarding path misbehaves and you do not want to stop the
daemon other clients are using.

One case fails loudly rather than falling back: a daemon running a **different
kivgraph release**. The `.mcpb` bundle carries its own binary while the state
directory comes from the configuration, so two installations share a daemon by
construction, and restarting it would take the graph away from whoever else is
using it. The message names both versions; `kivgraph update` restarts a
supervised daemon onto the installed release.

With no published generation, `serve` still completes the handshake. It exposes
only `index_project`, and its `instructions` tell the agent to run
`kivgraph index --full` and restart the server. Nothing is broken: there is
simply no graph to answer from yet.

`serve` also writes the default configuration when none exists, and continues,
because a client spawns the server itself and exiting because nobody ran `init`
reads as a crash. That default registers no repository and indexes nothing. An
existing configuration that cannot be read is a failure and is never
overwritten.

Next: [Quickstart](/quickstart/) to build the first graph, or
[Troubleshooting](/mcp/troubleshooting/) if the client reports no tools.
