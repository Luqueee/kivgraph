---
title: Telemetry
description: This distribution of Kivgraph reports nothing. What that covers, and how to check it.
---

**This distribution reports nothing.** No event is sent when Kivgraph is
installed, when it first runs, or when the daemon is registered with the
platform. There is no collector to send one to.

## What changed, and from when

Kivgraph began as another project, whose releases `v0.9.3` to `v0.9.10`
reported three events to a collector that project operates: one from the
installers, one when `serve` or the daemon first ran, and one when
`kivgraph daemon install` registered a supervisor entry.

Releases published from this repository, from `v0.10.0-dev.1` onward, send
none of them:

- the installers no longer make the request, and no longer print the notice
  that announced it;
- the binary is built with no endpoint, so the first-run and supervisor events
  are never sent, no marker is written for them, and nothing is printed.

A release installed from the original project keeps the behaviour that
project documents. This page describes only what is published here.

## What never leaves your machine

Nothing about your code, in any release: no repository name, path, file name,
symbol, query or result. The graph is built and served locally.

## Checking it

`KIVGRAPH_TELEMETRY=0` is still accepted, so a configuration that sets it
keeps working. It is no longer needed.

To see for yourself that a binary sends nothing, the endpoint is a constant
you can read: `Endpoint` in `internal/telemetry/firstrun.go` is the empty
string, and `TestABuildWithNoCollectorReportsNothing` fails if a build with no
endpoint makes any request at all.

## Requests Kivgraph does make

Three, all of them to GitHub and none carrying anything about you or your
code:

- an installer fetches the release archive and its `SHA256SUMS`;
- `kivgraph update` asks the GitHub API for the latest release and downloads
  it;
- running `kivgraph` with no command asks the same API, at most once a day,
  whether a newer release exists, so it can say so.

GitHub records those as it records any request.

## When it changes

This page changes in the same commit as the code, or the change does not ship.
If anything is ever reported again, it is described here first.
