---
title: Quickstart
description: Register a repository, publish a graph, and point an MCP client at it.
---

Kivgraph serves queries from a published generation. Nothing answers until one
exists, so the order below is the whole setup: register, check, index, connect.

## 1. Register a repository

```bash
kivgraph init \
  --repository project=/absolute/path/to/project \
  --languages go,typescript,rust
```

`--repository NAME=PATH` may be repeated. The name is an identifier, compared
exactly — two repositories differing only in case are two repositories — and it
travels inside the stable keys of everything the repository declares.

`--languages` accepts ten tokens: `go`, `typescript`, `javascript`, `ts`, `js`,
`rust`, `rs`, `python`, `py` and `dart`. The five languages are not resolved to
the same standard. Go, TypeScript and Rust edges are type-checked; Dart edges
are resolved by Dart Analysis Server; Python uses exact semantic facts when a
configured analyzer provides them and `CANDIDATE` facts in its bundled AST
fallback.

## 2. Check the machine

```bash
kivgraph doctor
```

`doctor` reports the configuration, the toolchains it found and the state of
the published graph. It names the language version ceiling this binary
type-checks Go with, which is not the `go` on your `PATH` and is the number
that decides whether a module can be indexed at all.

If you want exact Python edges, install the optional managed analyzer before
indexing:

```bash
kivgraph toolchain install pyright
```

The command installs the pinned Pyright release under Kivgraph's state and
activates it in the selected configuration. Use
`--config .kivgraph/config.yaml` for a project-local configuration. The
bundled Python worker remains available as a `CANDIDATE`-only fallback, and
`kivgraph toolchain status` reports which mode is active.

## 3. Index and publish

```bash
kivgraph index --full
```

The pass analyses every registered repository, validates the canonical graph
and publishes it as a new generation. Publication is atomic: a candidate that
fails integrity or validation never becomes `CURRENT`, and the previous
generation keeps serving.

## 4. Point a client at it

Configure any MCP client to start the server over stdio:

```json
{
  "mcpServers": {
    "kivgraph": {
      "command": "/home/user/.local/bin/kivgraph",
      "args": [
        "serve",
        "--config",
        "/home/user/.config/kivgraph/config.yaml"
      ]
    }
  }
}
```

Most clients can be wired automatically. `kivgraph mcp install` has five
targets — `claude-code`, `claude-desktop`, `codex`, `opencode` and `oh-my-pi` —
and takes `--scope user|project` (default `user`), `--dry-run` and `--force`.
Claude Desktop is user-scope only and is the one target that installs no local
skill. See [Clients](/mcp/clients/).

## What `serve` guarantees

With no published generation there is no graph-query surface. `serve` completes
the handshake and publishes the three indexing controls — which is how a client
with no graph builds its first one — and puts the rebuild path in its
`instructions`. It does not exit: a client launches the process itself, so
exiting reads as a crash.

The process writes MCP framing exclusively to `stdout` and logs to `stderr`. It
follows the published generation: it loads the HotSnapshot at start and
republishes when the `CURRENT` pointer advances, so an `index --full` in
another terminal cannot leave a server answering from a graph that no longer
exists on disk.
