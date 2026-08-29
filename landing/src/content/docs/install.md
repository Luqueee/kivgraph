---
title: Install
description: Install the published Kivgraph bundle, choose where it lands, and keep it up to date.
---

Kivgraph is distributed as a self-contained bundle. The installer detects the
platform, downloads the latest published release for it, verifies both the
release archive and the bundle checksums, and installs it without requiring Go,
Node.js or pnpm on the build side.

The release contains the Go server, the pinned LadybugDB library, the
TypeScript worker, the bundled Python AST worker, the pinned `rust-analyzer`,
the grammar manifest and the web viewer served by `kivgraph ui`.

## Published platforms

Linux `amd64`, macOS `arm64` and Windows `amd64`. Those are the three, and each
publishes exactly one architecture. On macOS only Apple Silicon is published;
`darwin/amd64` is out of scope by decision, and the installer says so when it
refuses rather than downloading something that will not run.

## Runtime requirements

- Bash on Linux and macOS; PowerShell `5.1` or later on Windows — the two
  installers are two programs, and the Windows one exists because `install.sh`
  cannot run where there is no POSIX shell
- Node.js `22` or later — the TypeScript worker is a Node process
- Python `3.10` or later when indexing Python — the bundled worker is a Python
  process
- The Dart or Flutter SDK when indexing Dart — the loader drives the Dart
  Analysis Server it supplies
- `curl`, `tar` on Linux and macOS
- `sha256sum` or `shasum` on Linux and macOS

The bundle carries its own `rust-analyzer`. Indexing Rust repositories
additionally needs `cargo` on the `PATH`: the analyzer cannot load a Cargo
workspace without it.

On Windows the installer also installs the Visual C++ redistributable, because
`kivgraph.exe` does not start without it — `STATUS_DLL_NOT_FOUND`, since the
LadybugDB DLL is MSVC-built. It is installed rather than carried in the bundle
so that Windows Update services it: a security fix that reaches every other
installation and not this one is not a trade a self-contained bundle wins.

## One command

On Linux and macOS, where the same line covers both because the installer reads
`uname` and picks its own archive:

```bash
curl -fsSL https://github.com/Luqueee/kivgraph/releases/latest/download/install.sh | bash
```

On Windows:

```powershell
irm https://github.com/Luqueee/kivgraph/releases/latest/download/install.ps1 | iex
```

That is the PowerShell shape of the line above it, and it gives up one thing in
the trade: `install.ps1` opens with `#Requires -Version 5.1`, which is a comment
rather than a guard when the text is piped into `Invoke-Expression` instead of
being run as a file. Every Windows version still receiving updates ships a newer
PowerShell than that, so what it costs is a clearer error on a machine that
would have failed anyway. Download it and run it as a file to keep the guard:

```powershell
$installer = "$env:TEMP\kivgraph-install.ps1"
irm https://github.com/Luqueee/kivgraph/releases/latest/download/install.ps1 -OutFile $installer
& $installer
```

From a checkout, either installer runs directly:

```bash
./scripts/install.sh
```

To install a specific release instead of the latest one:

```bash
KIVGRAPH_VERSION=v0.9.2 ./scripts/install.sh
```

`KIVGRAPH_VERSION` is read by both installers, and so are
`KIVGRAPH_INSTALL_ROOT`, `KIVGRAPH_BIN_DIR` and `KIVGRAPH_RELEASE_BASE_URL`.

## Where it lands

On Linux and macOS the script installs the bundle in `~/.local/opt/kivgraph`
and puts launchers in `~/.local/bin`. On Windows it is
`%LOCALAPPDATA%\Programs\kivgraph` and `%LOCALAPPDATA%\Programs\kivgraph-bin`.
Override both with `KIVGRAPH_INSTALL_ROOT` and `KIVGRAPH_BIN_DIR`.

Neither installer edits your `PATH` — an installer whose effects outlive an
uninstall is not one this project ships — and both say so when the launcher
directory is not on it. The Windows one also prints the `setx` line that would
add it for the current account.

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
