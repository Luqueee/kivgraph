---
title: MCP clients
description: Register Ladygraph with a coding agent and install its Agent Skill.
---

The release installer does not edit client configuration automatically. After
installing Ladygraph, run the integration commands without `--target` to detect
the coding agents present on this machine and select one or more of them:

```bash
ladygraph mcp install --scope user
ladygraph skill install --scope user
```

Neither command initialises Ladygraph nor indexes any repository.

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

If none is detected, the selector starts with no agents selected. It respects
`NO_COLOR` and emits no ANSI when the output is redirected. Use `--target` only
for scripted, non-interactive installation.

## Supported targets

- **MCP**: `claude-code`, `claude-desktop`, `codex`, `opencode`, `oh-my-pi`
- **Skill**: `claude-code`, `codex`, `opencode`, `oh-my-pi` — Claude Desktop has
  no local skill target

## Scope and safety

The default scope is `user`; use `--scope project` for project-local
configuration. `--dry-run` prints the plan without writing anything.

Adapters validate JSON or TOML before modifying it and reject a destination
that is a symlink. Existing incompatible entries stop with an error; `--force`
is required to replace or remove one. Files are written atomically with mode
`0600` and receive a `*.ladygraph.bak` backup before replacement or removal. An
entry that is already correct does not rewrite the file.

## Inspect and remove

```bash
ladygraph mcp status --target claude-code --scope user
ladygraph mcp remove --target claude-code --scope user
ladygraph skill status --target claude-code --scope user
ladygraph skill remove --target claude-code --scope user
```

`remove` withdraws only Ladygraph's own entry; everything else in the client
configuration is left as it was.
