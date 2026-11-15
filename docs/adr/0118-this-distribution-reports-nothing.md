# ADR 0118: This distribution reports nothing and publishes from its own repository

Status: accepted, 2026-11-15.

Changes the distribution: releases, `kivgraph update` and the installers use
`Tutitoos/kivgraph`. Changes telemetry: nothing is sent.

Changes the schema: no.

## Context

This repository began as `Luqueee/kivgraph` and is now developed on its own.
Everything a release touches still pointed at the original project:

- `kivgraph update` and both installers downloaded from its releases, so a
  binary built here would have replaced itself with one built there;
- the binary and the installers reported three telemetry events to
  `https://kivgraph.dev/api/telemetry/first-run`, a collector the original
  project operates (ADR 0083, 0090, 0092). A release cut here would have
  reported its installs to somebody else's dashboard;
- the release workflow published to the MCP Registry as
  `io.github.Luqueee/kivgraph`, a namespace this repository cannot prove it
  owns: the registry grants the namespace of the repository that
  authenticates;
- the install commands used `kivgraph.dev`, a domain this distribution does not
  have.

## Decision

1. **Repository.** `update.Repository`, the installers, the bundle and
   registry metadata and the documented install commands name
   `Tutitoos/kivgraph`. Install commands use the GitHub release URLs directly.
2. **Telemetry.** Nothing is sent. `telemetry.Endpoint` is empty, and a build
   with no endpoint returns before it claims a marker or prints a notice. The
   installers lose the request and its notice.
3. **MCP Registry.** Not published. The job stays in the workflow, gated on
   the repository variable `PUBLISH_MCP_REGISTRY`, and `server.json` names
   `io.github.Tutitoos/kivgraph` so that enabling it is one variable.
4. **Website.** None. `landing/` stays as the source of the user
   documentation and is not deployed.

## What was not changed

- **The Go module path** stays `github.com/Luqueee/kivgraph`. Renaming it
  touches every Go file and changes nothing a user can observe.
- **Stable keys**, the `luque-stable-key` namespace and the `LUQUE-####`
  identifiers are persistent identities and are untouched.
- **The telemetry package** keeps its code and its tests. Deleting it would
  also delete the proof that a build with an endpoint behaves; with an empty
  endpoint it is unreachable, and one constant re-enables it.
- **`KIVGRAPH_TELEMETRY=0`** is still accepted, so a configuration or a unit
  file that sets it keeps working.
- **`download-metrics.yml`** runs only in the original repository and does
  nothing here.

## Consequences

- A binary already installed from the original project keeps updating from
  it. Nothing here can redirect it; it has to be reinstalled from this
  repository.
- `releases/latest` answers only once a stable release exists here. Until
  then an install pins a version with `KIVGRAPH_VERSION`.
- `docs/development/analytics.md`, ADR 0083, 0090 and 0092 describe the
  original project's collector and site. They are history, not a description
  of this distribution.

## Verification

- `TestABuildWithNoCollectorReportsNothing`: with no endpoint, neither emitter
  makes a request, writes a marker or prints a notice.
- `TestPublishedServerMetadata`: `server.json` names this repository.
