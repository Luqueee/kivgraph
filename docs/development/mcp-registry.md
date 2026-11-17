# Publishing to the official MCP Registry

Kivgraph is listed at `io.github.Luqueee/kivgraph` in the registry at
`registry.modelcontextprotocol.io`. This document is what the release workflow
does on a tag and, more usefully, **why the shape is what it is** -- most of it
is forced by constraints that are not obvious and that cost a release to
discover.

The registry stores metadata only. The artifact stays on the GitHub release.

## Why there is a second archive

The registry accepts `npm`, `pypi`, `nuget`, `cargo`, `oci` and `mcpb`
packages. Kivgraph ships `kivgraph-<os>-<arch>.tar.gz` from a GitHub release,
which is **none of them**, and four of the five alternatives would mean
distributing Kivgraph a second way rather than describing the way it already is.

`mcpb` is the one that fits: a prebuilt binary that needs no toolchain on the
reader's machine. So each release publishes the same bundle in two containers.
`scripts/build-mcpb.sh` builds the second one.

|asset|who reads it|
|---|---|
|`kivgraph-<os>-<arch>.tar.gz`|`install.sh`, and anyone installing by hand|
|`kivgraph-<os>-<arch>.mcpb`|the registry, and clients that install bundles|

Nothing is built twice: the `.mcpb` packages the bundle directory the release
job already produced, so the binary inside it is the same file, byte for byte.

## The layout, and the collision it avoids

```text
manifest.json      the MCPB manifest, which MUST be at the archive root
server/            the Kivgraph bundle, verbatim
```

The bundle is **nested rather than flattened**, and that is not a style choice.
The bundle already carries its own `manifest.json` -- the provenance one, with
the release, the commit and the schema versions that the release job asserts.
Flattening would put two different files at one path and one would win in
silence. Nested, both survive: `manifest.json` is MCPB's, `server/manifest.json`
is ours.

It also keeps the executable's relative `RUNPATH` intact. The binary finds
`lib/liblbug.so` relative to itself, so the tree has to move as a unit; any
layout that separates `bin/` from `lib/` produces an archive that installs and
then cannot start.

The MCPB manifest runs `server/bin/kivgraph serve` -- the same command
`kivgraph mcp install` writes into a client configuration, so a reader who
installs the bundle and a reader who installs the binary run the identical
process. The command is written as `${__dirname}/server/bin/kivgraph`, because
a relative one is resolved against the client's working directory and not the
extension's.

## Reproducible, for the same reason the tarball is

Two builds of one tag must not differ, or the `fileSha256` published to the
registry stops describing anything.

The archive is written by `python3`'s `zipfile` rather than by `zip`, and both
halves of that matter. `zip` stores whatever mtime a file happens to carry with
no flag to override it, so reproducibility would depend on a `touch` pass having
run first -- a precondition nothing checks. `zipfile` sets the timestamp per
entry (1980-01-01, the earliest a zip entry can express) so it cannot be
forgotten, and it writes the permission bits deliberately: `bin/kivgraph`,
`bin/rust-analyzer` and the native library have to come out executable or the
bundle installs and cannot start.

Verified: two runs over the same bundle produce the same SHA-256, the extracted
binary reports its version -- which proves the relative `RUNPATH` survived
packaging -- and answers `initialize` and `tools/list` with its twelve tools
over stdio.

## `server.json` carries no packages, on purpose

The committed `server.json` has no `packages` array. A package entry requires a
`fileSha256`, and a committed one would be the checksum of an artifact that
either does not exist yet or has been rebuilt since -- a claim nobody verified.

`scripts/build-server-json.sh` adds the packages at release time, hashing the
artifacts **that are being uploaded in that run**, and refuses to run against a
template that already has an array. `internal/version/registry_test.go` asserts
the committed file stays that way.

## The 100-character description

The registry rejects a description longer than 100 characters with a `422`, and
**this limit is not in the published JSON schema**, so nothing local catches it
by validating. The first integration run found it by being rejected.

Both `scripts/build-server-json.sh` and `internal/version/registry_test.go`
check it, so it fails in a second rather than in the last step of a release.

## What the schema cannot express

A `Package` has `registryType`, `identifier`, `fileSha256` and `transport`, and
**no field saying which platform it is for**. Both platforms are therefore
sibling packages, and what distinguishes them is inside each archive --
`compatibility.platforms` in its MCPB manifest -- plus the target in the file
name.

A client that picks the wrong one gets a binary that will not execute. There is
nowhere in the current schema to prevent that, and shipping only one platform to
sidestep it would be worse. This is a limitation to carry, not to hide.

## Authentication

The release job authenticates with `mcp-publisher login github-oidc`, which
proves the identity of the repository itself -- the same repository that owns
the `io.github.Luqueee/` namespace being published to.

The alternative, `mcp-publisher login github`, opens an interactive device flow
that no unattended run can complete, and a personal access token in the
repository secrets is a long-lived credential this needs no part of.

## The order of the jobs

`registry` runs after `publish`, not beside it. The registry fetches the package
URL to check the `fileSha256` it was given, so publishing before the assets are
attached to the release fails on a `404` that describes nothing.

## Doing it by hand

```bash
# The published metadata, from artifacts you have in front of you.
scripts/build-server-json.sh \
  --version 0.8.1 --tag v0.8.1 \
  --package "linux/amd64=$(sha256sum kivgraph-linux-amd64.mcpb | cut -d' ' -f1)" \
  --package "darwin/arm64=$(sha256sum kivgraph-darwin-arm64.mcpb | cut -d' ' -f1)" \
  --output server.json

mcp-publisher validate    # checks against the live registry, not a local schema
mcp-publisher login github
mcp-publisher publish
```

`validate` talks to the registry, so it catches the rules the schema does not
state. Run it before a tag, never after.
