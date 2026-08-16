---
title: Graph viewer
description: kivgraph ui, what it exposes, and how to restrict it.
---

```bash
kivgraph ui
```

The viewer is opt-in and serves only the published HotSnapshot over read-only
HTTP. It never mutates the snapshot and never opens the canonical database for
writing.

## The default bind is every interface

`web.address` defaults to `0.0.0.0:7777`. The graph is indexed where the
repositories are and the viewer is looked at from another machine, so a
loopback default would mean editing the configuration in the normal case.

That makes the warning the guard, and the warning is not decoration. Every bind
that is not loopback logs exactly what is being exposed:

- repository and file paths
- symbol names and signatures

**There is no authentication.** Restricting the endpoint is `--addr` or
`web.address`:

```bash
kivgraph ui --addr 127.0.0.1:7777
```

The command logs the address it actually bound, including the one a port `0`
resolves to.

## Builds without the viewer

`ui` refuses to start when the binary was not built with the `webassets` tag,
rather than serving a "bundle not available" page on every route. The published
release bundle carries the viewer.

## What it draws

The viewer derives its own 3D layout from the structure of each tile —
containment, dependencies, communities and hierarchical depth — computed once
per tile in a Web Worker. It is deterministic: directions come from the hash of
a node's identity, so the same tile always draws the same world.

Nodes are coloured by rank: repository, package, file, symbol. Containment is
drawn as segments; local, cross-repository and exact dependencies each have
their own colour and weight. Only repositories and hubs carry permanent labels;
everything else is captioned on hover.

Rendering is on demand: the loop stops when the graph is still and wakes on
pointer events. A published graph does not move, and redrawing it sixty times a
second costs a core for nothing.

## `serve` and `stop`

`kivgraph serve` stays on stdio and opens no HTTP listener. The two commands
are independent.

```bash
kivgraph stop --dry-run
kivgraph stop
```

`stop` terminates this user's long-running `serve` and `ui` processes and
nothing else. It selects by invocation, not by executable: an indexing pass is
minutes of analysis and is never killed, and `stop` does not kill itself. It
sends `SIGTERM`, waits for the bounded graceful shutdown, re-checks that the
pid is still the same invocation, and only then escalates to `SIGKILL`.
`--dry-run` enumerates without signalling.
