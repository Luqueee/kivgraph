---
title: Install
description: Install the published Kivgraph bundle, choose where it lands, and keep it up to date.
---

Kivgraph is distributed as a self-contained bundle. The installer detects the
platform, downloads the latest published release for it, verifies both the
release archive and the bundle checksums, and installs it without requiring Go,
Node.js or pnpm on the build side.

The release contains the Go server, the pinned LadybugDB library, the
TypeScript worker, the pinned `rust-analyzer` and the grammar manifest.

## Published platforms

Linux `amd64` and macOS `arm64`. Those are the only two. On macOS only Apple
Silicon is published; `darwin/amd64` is out of scope by decision, and the
installer says so when it refuses.

## Runtime requirements

- Bash
- Node.js `22` or later — the TypeScript worker is a Node process
- `curl`, `tar`
- `sha256sum` or `shasum`

The bundle carries its own `rust-analyzer`. Indexing Rust repositories
additionally needs `cargo` on the `PATH`: the analyzer cannot load a Cargo
workspace without it.

## One command

```bash
curl -fsSL https://github.com/Luqueee/kivgraph/releases/latest/download/install.sh | bash
```

From a checkout, the same installer runs directly:

```bash
./scripts/install.sh
```

To install a specific release instead of the latest one:

```bash
KIVGRAPH_VERSION=v0.3.0 ./scripts/install.sh
```

## Where it lands

The script installs the bundle in `~/.local/opt/kivgraph` and puts launchers
in `~/.local/bin`. Override both with `KIVGRAPH_INSTALL_ROOT` and
`KIVGRAPH_BIN_DIR`.

It never modifies a registered repository, creates an index or replaces
configuration files. Installing Kivgraph and initialising it are two separate
acts.

Add the launcher directory to the current shell and verify both runtimes:

```bash
export PATH="$HOME/.local/bin:$PATH"
kivgraph version
kivgraph-ts-worker <<'EOF'
hello
EOF
```

## macOS and quarantine

The binaries are not notarized and the project uses no Developer ID. The
executable carries an ad-hoc signature, which is what Apple Silicon requires in
order to run at all.

Gatekeeper only blocks a file carrying the `com.apple.quarantine` attribute,
and neither `curl` nor `tar` writes it: a release downloaded with the installer
runs. A copy downloaded with a browser needs:

```bash
xattr -dr com.apple.quarantine ~/.local/opt/kivgraph
```

## Updating

```bash
kivgraph update --check
kivgraph update
```

The update is atomic, preserves the configuration and graph state, verifies the
release and bundle checksums, and replaces only the installed bundle. Restart
the MCP client afterwards so it launches the new binary.

When `kivgraph` is invoked without a command from an interactive terminal, it
checks for a newer release with an 800 ms timeout and a 24-hour cache in the
platform cache directory (`$XDG_CACHE_HOME` on Linux, `$HOME/Library/Caches` on
macOS), under `kivgraph/update-check.json`. The check never blocks the command
when the network is unavailable.

Interactive command output uses semantic ANSI colours when the destination is a
terminal. Set `NO_COLOR`, or redirect the output, to keep it plain.
