# ADR 0091: Stable and development release channels

- **Status:** accepted
- **Date:** 2026-08-31
- **Changes the CLI surface:** yes -- `update --channel` is added
- **Changes the release protocol:** yes

## Context

GitHub's `/releases/latest` endpoint intentionally excludes prereleases.
Using it for development builds makes `kivgraph update` report that a test
installation is current or silently jump it to a stable release. A development
build needs an opt-in stream that cannot affect stable installations.

## Decision

Stable releases use tags such as `v0.10.0` and remain the GitHub latest
release. Development releases use SemVer prerelease tags such as
`v0.10.0-dev.1` and are published as GitHub prereleases. They are not
published to the MCP Registry, whose package is the stable discovery channel.
The existing release workflow builds and smoke-tests both forms; only the
publication flags and registry job differ.

Update selection has two channels:

- `stable` queries `/releases/latest`. This is the default for a
  stable binary.
- `dev` queries published, non-draft prereleases and selects the highest
  valid SemVer tag. A prerelease binary follows this channel when no explicit
  channel is supplied.

The channel can be selected with `kivgraph update --channel stable|dev` or
`KIVGRAPH_UPDATE_CHANNEL=dev`. The same environment value controls the
interactive update notice. A command-line channel takes precedence over the
environment. An invalid channel is rejected before any network request. The
cache records the channel, so a stable lookup can never be reused for a dev
lookup; cache files written before this ADR are treated as stable.

Both channels retain the same safety checks: supported platform, SemVer tag,
outer `SHA256SUMS`, archive layout, inner checksums, manifest target and
release, and the executable's reported version. An update replaces the bundle
only after all checks pass.

Installing a particular development build remains explicit through
`KIVGRAPH_VERSION=vX.Y.Z-dev.N` with the installer. After that installation
the prerelease version makes the default `update` follow `dev`. Stable
users are never moved to a prerelease unless they select it.

## Consequences

Developers can test a release candidate with the same update command and can
return to stable with `kivgraph update --channel stable`. Stable clients keep
the existing behaviour and endpoint. The development endpoint makes one API
request per check and considers only the first 100 releases returned by GitHub.
Releases beyond that first page are not selected by the current implementation.

A prerelease may be lower than the corresponding final SemVer, so selecting
`stable` from a dev installation is the explicit operation that moves
forward to the stable stream. The release number still follows the repository
rule that versions only increase within a channel.
