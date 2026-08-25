---
title: CLI
description: Every Kivgraph command, in the five groups the help prints them.
---

`kivgraph --help`, `-h` and `help` write to `stdout` and exit `0`. An
invocation with no command, or with an unknown one, writes a single line to
`stderr` and points at the help rather than dumping the whole surface.

## Getting started

| Invocation | Summary |
| --- | --- |
| `init [--repository NAME=PATH] [--languages LIST]` | Write the configuration and register repositories |
| `index --full [--json]` | Index every registered repository and publish a generation |
| `serve` | Run the MCP server over stdio |
| `daemon [--addr HOST:PORT] [--allow-remote]` | Serve MCP to many clients from one process, over HTTP and a unix socket |
| `daemon install [--addr HOST:PORT] [--allow-remote]` | Give the daemon an owner, so the platform starts it and restarts it |
| `daemon remove` | Stop the daemon and take its supervisor entry out |
| `ui [--addr HOST:PORT]` | Serve the read-only graph viewer, every interface by default |
| `stop [--dry-run]` | Stop every running serve, daemon and ui of this user |

## Diagnostics

| Invocation | Summary |
| --- | --- |
| `doctor` | Check configuration, toolchains and the published graph |
| `doctor storage --database PATH` | Inspect one LadybugDB database file |
| `doctor graph --database PATH` | Validate the canonical graph of a database |
| `graph status --root PATH` | Report the active and backup generations |
| `daemon status` | Report whether the daemon has an owner, and where its unit lives |
| `stats [--interval D] [--once] [--json]` | Watch what every kivgraph process on this machine costs |
| `logs [--follow] [--kind K] [--tool NAME] [--since D] [--limit N] [--failures] [--json]` | Read what this machine indexed, served and answered |
| `tool-stats [--tool NAME] [--since D] [--json]` | Report the cost and the failures of every tool |
| `version [--json]` | Print the release, with --json for full provenance |

## Maintenance

| Invocation | Summary |
| --- | --- |
| `upgrade` | Rebuild the graph after a schema change |
| `clean [--keep-active] [--yes]` | Remove published graph generations |
| `rollback --root PATH [--generation ID]` | Return to the previous generation |
| `snapshot --root PATH [--generation ID]` | Rebuild the hot snapshot of a generation |
| `update [--check] [--stop]` | Install the latest published release |

## Integrations

| Invocation | Summary |
| --- | --- |
| `mcp install [--scope user\|project] [--stdio]` | Detect and register one or more MCP clients, against the daemon by default |
| `mcp status --target TARGET [--scope user\|project] [--stdio]` | Inspect a client MCP registration |
| `mcp remove --target TARGET [--scope user\|project] [--stdio]` | Remove only Kivgraph's MCP registration |
| `skill install [--scope user\|project]` | Detect and install the Agent Skill in one or more clients |
| `skill status --target TARGET [--scope user\|project]` | Inspect the installed Agent Skill |
| `skill remove --target TARGET [--scope user\|project]` | Remove only Kivgraph's Agent Skill |
| `completion bash\|zsh\|fish` | Print the shell completion script for one shell |

### Shell completion

```bash
kivgraph completion bash > /usr/local/etc/bash_completion.d/kivgraph   # bash 3.2 and newer
kivgraph completion zsh  > "${fpath[1]}/_kivgraph"
kivgraph completion fish > ~/.config/fish/completions/kivgraph.fish
```

The script is a fixed stub: it carries no command name, no flag and no
vocabulary, and forwards the words typed so far to `kivgraph __complete`. That
is what keeps it from going out of date when a flag is added, and it is what
lets completion answer the questions a static script cannot: `--generation`
completes the generations on disk, `--target` the clients this machine has, and
`--tool` the tools this installation has actually been called with. A flag that
takes a path defers to the shell's own file completion.

## Pipeline

| Invocation | Summary |
| --- | --- |
| `rebuild --facts PATH --root PATH ...` | Publish a generation from a fact set |
| `benchmark generate-graph` | Generate a synthetic corpus |

## Builds without the viewer

`ui` needs the web bundle, which is only linked in when the binary is built
with the `webassets` tag. A build without it marks the command as unavailable
in the help and refuses to start rather than serving a "bundle not available"
page. The published release bundle carries the viewer.

## Logging

A one-shot command reports in plain text when `stderr` is a terminal and as a
JSON record when it is not; `serve` and `ui` always log JSON, because a client
reads their `stderr`. Progress is `INFO` and only a failure is `ERROR`.
