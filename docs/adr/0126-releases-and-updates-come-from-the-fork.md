# ADR 0126: releases and updates come from `Tutitoos/kivgraph_official`

## Status

Accepted. Supersedes the distribution source named in ADR 0118; what that ADR
says about reporting nothing still holds.

## Context

ADR 0118 pointed `kivgraph update`, both installers and the build scripts at
`Tutitoos/kivgraph`, where the work was developed. That work now lives in this
repository, a fork of the official project, and it is where releases are cut.
A binary that kept reading releases from the other repository would update
into builds this repository does not publish.

## Decision

`internal/update.Repository`, `scripts/install.sh`, `scripts/install.ps1`,
`scripts/downloads.sh`, `scripts/build-mcpb.sh`, `scripts/build-server-json.sh`,
`server.json` and its test, the README, the installation docs, the landing
site and the publishing skill name `Tutitoos/kivgraph_official`.

What is not changed, on purpose:

- **The MCP Registry name** stays `io.github.Tutitoos/kivgraph`. It is an
  identifier published under an owner namespace, not a repository name, and
  changing it would make a new server. Only the repository and website URLs
  inside it move. Publishing to the registry is still off.
- **The Go module path** stays `github.com/Luqueee/kivgraph`.
- **Published notes** (`landing/src/content/releases/v0.10.0-dev.1.md`) and the
  text of ADR 0118 keep naming the repository they were written for.

## Consequences

- A binary installed from `Tutitoos/kivgraph`, including `v0.10.0-dev.1`, keeps
  looking for updates there and does not know about this repository; it has to
  be reinstalled once. Nothing redirects it from here.
- The first release published from this repository is `v0.10.0`, tagged on the
  commit that makes this change, so the binaries it builds update from here.
