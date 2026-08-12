---
title: Quickstart
description: Register a repository, publish a graph, and point an MCP client at it.
---

Ladygraph serves queries from a published generation. Nothing answers until one
exists, so the order below is the whole setup: register, check, index, connect.

## 1. Register a repository

```bash
ladygraph init \
  --repository project=/absolute/path/to/project \
  --languages go,typescript,rust
```

`--repository NAME=PATH` may be repeated. The name is an identifier, compared
exactly — two repositories differing only in case are two repositories — and it
travels inside the stable keys of everything the repository declares.

## 2. Check the machine

```bash
ladygraph doctor
```

`doctor` reports the configuration, the toolchains it found and the state of
the published graph. It names the language version ceiling this binary
type-checks Go with, which is not the `go` on your `PATH` and is the number
that decides whether a module can be indexed at all.

## 3. Index and publish

```bash
ladygraph index --full
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
    "ladygraph": {
      "command": "/home/user/.local/bin/ladygraph",
      "args": [
        "serve",
        "--config",
        "/home/user/.config/ladygraph/config.yaml"
      ]
    }
  }
}
```

Most clients can be wired automatically — see [MCP clients](/mcp-clients/).

## What `serve` guarantees

`ladygraph serve` must start only after a validated generation has been
published; without one, every query that needs a snapshot fails explicitly
rather than answering from nothing.

The process writes MCP framing exclusively to `stdout` and logs to `stderr`. It
follows the published generation: it loads the HotSnapshot at start and
republishes when the `CURRENT` pointer advances, so an `index --full` in
another terminal cannot leave a server answering from a graph that no longer
exists on disk.
