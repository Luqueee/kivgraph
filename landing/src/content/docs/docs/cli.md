---
title: CLI
description: Kivgraph help lists every command in six groups.
---

`kivgraph --help`, `-h` and `help` write to `stdout` and exit `0`. So does a
bare `kivgraph`: an invocation with no command asks what the program does, and
it is answered with this same table. An **unknown** command writes a single
line to `stderr` and exits `2`, rather than dumping the whole surface at
someone who mistyped one word.

## Getting started

| Invocation | Summary |
| --- | --- |
| `init [--repository NAME=PATH] [--languages LIST]` | Write the configuration and register repositories |
| `index --full [--profile NAME] [--json]` | Index every repository in one profile and publish a generation |
| `serve [--introspection]` | Run the MCP server over stdio |
| `daemon [--addr HOST:PORT] [--allow-remote]` | Serve MCP to many clients from one process, over HTTP and a unix socket |
| `daemon install [--addr HOST:PORT] [--allow-remote]` | Give the daemon an owner, so the platform starts it and restarts it |
| `daemon remove` | Stop the daemon and take its supervisor entry out |
| `ui [--addr HOST:PORT] [--profile NAME]` | Serve one profile in the read-only graph viewer |
| `stop [--dry-run]` | Stop every running serve, daemon and ui of this user |

## Profiles

| Invocation | Summary |
| --- | --- |
| `profile create NAME` | Create an empty independent graph profile |
| `profile list` | List profiles and mark the configured default with `*` |
| `profile use NAME` | Move the default pointer without changing the profile |
| `profile remove NAME --yes` | Permanently remove a non-default profile and its indexed state |

Profiles share one installation, daemon and analyzer toolchain, but each owns
its repository registry, fact cache, canonical database and generations.
`index --full` and `ui` use `profiles.default` when `--profile` is omitted.

## Toolchains

- `toolchain status [--config PATH] [--json]`: Report optional analyzers
  managed by this installation.
- `toolchain install pyright [--config PATH] [--version VERSION] [--json]`:
  Install and activate the pinned Pyright analyzer.
- `toolchain remove pyright [--config PATH] --yes [--json]`: Remove the managed
  Pyright analyzer and restore fallback mode when the selected configuration
  uses the managed analyzer.

The family is explicit: indexing never installs a host dependency. The first
managed analyzer is Pyright, installed under Kivgraph state and enabled by
updating only the Python analyzer settings in the selected configuration.

## Diagnostics

| Invocation | Summary |
| --- | --- |
| `doctor` | Check configuration, toolchains and the published graph |
| `doctor repositories [--repository NAME] [--json]` | Audit whether every registered repository can be indexed, and say what to change |
| `doctor storage --database PATH` | Inspect one LadybugDB database file |
| `doctor graph --database PATH` | Validate the canonical graph of a database |
| `graph status --root PATH` | Report the active and backup generations |
| `daemon status` | Report whether the daemon has an owner, and where its unit lives |
| `stats [--interval D] [--once] [--json]` | Watch what every kivgraph process on this machine costs |
| `logs [--follow] [--kind K] [--tool NAME] [--since D] [--limit N] [--failures] [--json]` | Read aligned machine history with query summaries and neutral NOT_FOUND results |
| `tool-stats [--tool NAME] [--since D] [--json]` | Report the cost and the failures of every tool |
| `version [--json]` | Print the release, with --json for full provenance |

## Maintenance

| Invocation | Summary |
| --- | --- |
| `upgrade` | Rebuild the graph after a schema change |
| `clean [--keep-active] [--yes]` | Remove published graph generations |
| `rollback --root PATH [--generation ID]` | Return to the previous generation |
| `snapshot --root PATH [--generation ID]` | Rebuild the hot snapshot of a generation |
| `update [--check] [--stop]` | Install the latest release and refresh managed runtime integrations |

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

## Inspecting the tool catalog with no index

`serve --introspection` publishes the complete MCP tool catalog even when no
generation exists, for the inspectors and registries that read tool definitions
before anything has been indexed. It creates no index and relaxes no check:
graph-dependent query tools answer `INDEX_NOT_READY` until a generation is
published, while `graph_status` reports an empty graph status. `index_project`
stays behind its consent gate. Plain `serve` is unchanged, and this is not the
configuration to give a client. See
[MCP tools](/docs/mcp-tools/).

## Builds without the viewer

`ui` needs the web bundle, which is only linked in when the binary is built
with the `webassets` tag. A build without it marks the command as unavailable
in the help and refuses to start rather than serving a "bundle not available"
page. The published release bundle carries the viewer.

## Logging

A one-shot command reports in plain text when `stderr` is a terminal and as a
JSON record when it is not; `serve` and `ui` always log JSON, because a client
reads their `stderr`. Progress is `INFO` and only a failure is `ERROR`.
