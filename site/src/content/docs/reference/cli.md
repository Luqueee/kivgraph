---
title: CLI
description: Every Ladygraph command, in the five groups the help prints them.
---

`ladygraph --help`, `-h` and `help` write to `stdout` and exit `0`. An
invocation with no command, or with an unknown one, writes a single line to
`stderr` and points at the help rather than dumping the whole surface.

## Getting started

| Invocation | Summary |
| --- | --- |
| `init [--repository NAME=PATH] [--languages LIST]` | Write the configuration and register repositories |
| `index --full` | Index every registered repository and publish a generation |
| `serve` | Run the MCP server over stdio |
| `ui [--addr HOST:PORT]` | Serve the read-only graph viewer, every interface by default |
| `stop [--dry-run]` | Stop every running serve and ui of this user |

## Diagnostics

| Invocation | Summary |
| --- | --- |
| `doctor` | Check configuration, toolchains and the published graph |
| `doctor storage --database PATH` | Inspect one LadybugDB database file |
| `doctor graph --database PATH` | Validate the canonical graph of a database |
| `graph status --root PATH` | Report the active and backup generations |
| `version [--json]` | Print the release, with --json for full provenance |

## Maintenance

| Invocation | Summary |
| --- | --- |
| `upgrade` | Rebuild the graph after a schema change |
| `clean [--keep-active] [--yes]` | Remove published graph generations |
| `rollback --root PATH [--generation ID]` | Return to the previous generation |
| `snapshot --root PATH [--generation ID]` | Rebuild the hot snapshot of a generation |
| `update [--check]` | Install the latest published release |

## Integrations

| Invocation | Summary |
| --- | --- |
| `mcp install [--scope user\|project]` | Detect and register one or more MCP clients |
| `mcp status --target TARGET [--scope user\|project]` | Inspect a client MCP registration |
| `mcp remove --target TARGET [--scope user\|project]` | Remove only Ladygraph's MCP registration |
| `skill install [--scope user\|project]` | Detect and install the Agent Skill in one or more clients |
| `skill status --target TARGET [--scope user\|project]` | Inspect the installed Agent Skill |
| `skill remove --target TARGET [--scope user\|project]` | Remove only Ladygraph's Agent Skill |

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
