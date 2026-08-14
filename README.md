# Ladygraph

A local MCP server that answers questions about code across repositories, for
Go, TypeScript and Rust.

It indexes a corpus once and serves an immutable graph: the edges are resolved
by `go/types`, the TypeScript checker and `rust-analyzer`, not by matching
names. That is the difference from a search tool, and it is what makes an empty
answer worth something — an empty reference list means **nobody calls it**, not
that nothing was found, and `grep` cannot tell those apart.

## What each tool answers

| the question | the tool |
| --- | --- |
| who calls this, what references this | `find_references` |
| what breaks if I change it | `get_blast_radius` |
| what does this reach outward | `trace_dependencies` |
| who uses it from another repository | `find_cross_repo_consumers` |
| where is it declared | `find_symbol` |
| what is declared in this package | `get_file_outline` |
| give me the code of these symbols | `get_source` |
| everything about this one symbol | `get_symbol` |
| what is indexed, and is the graph current | `list_repositories`, `graph_status` |

Ten read-only tools, plus one consent-gated mutation (`index_project`) that a
client has to authorize before it can register a repository or publish a
generation.

Every row that names a symbol carries its repository, path, qualified name and
line range, so it can be opened without a second call, and every tool accepts
that triple in place of an opaque key.

**Where it loses.** A rare name in one small repository is cheaper with `grep`,
and indexing a small file costs more than reading it. It wins on common names,
on transitive impact, on consumers in another repository, and on proving an
absence. Measured, on a private 41-repository monorepo of 100,118 symbols:
`28.3x` cheaper than `grep` for a name the corpus declares 126 times, `0.2x` for
one that appears twice, `2.1x` over a whole session. The harness that measures
it is `benchmarks/mcp-token-cost`, and it compares against the host's own tool
output captured verbatim.

## Status

Released and in use. `ladygraph version` reports the published release; the
backlog and the acceptance gate of every phase are in [`TASKS.md`](TASKS.md).

- **Languages:** Go, TypeScript, Rust. The Rust standard library enters the
  graph as a synthetic provider with `rust.index_sysroot`, off by default.
- **Surface:** ten read-only tools over STDIO, plus one consent-gated
  mutation (`index_project`). The contract is
  [docs/protocol/mcp-surface-v3.md](docs/protocol/mcp-surface-v3.md).
- **Storage:** LadybugDB is canonical; queries are served from an immutable
  HotSnapshot published atomically, never from the database.
- **Platforms:** `linux/amd64` and `darwin/arm64`.
- **Viewer:** `ladygraph ui` serves a read-only 3D view of the published graph.

## Requirements

- Go 1.26 or later to build from source. The indexer type-checks with the
  `go/types` linked into the binary, so it can only read repositories and
  dependencies written for its own language version or older; `ladygraph doctor`
  reports that ceiling.
- Indexing Rust needs `cargo` and `rust-analyzer`. The release bundle carries
  the analyzer; it does not carry a Rust toolchain.
- Indexing TypeScript needs Node.js 22 or later for the worker.

## Installation

### Install the MCP with one script

The installer detects the platform, downloads the latest published MCP release
for it, verifies both the release archive and the bundle checksums, and
installs it without requiring Go, Node.js, or pnpm. The release contains the Go
server, the pinned LadybugDB library, the TypeScript worker, the pinned
`rust-analyzer`, and the grammar manifest; the web viewer is intentionally
omitted.

Published bundles: Linux `amd64` and macOS `arm64`.

Runtime requirements: Bash, Node.js `22` or later, `curl`, `tar`, and
`sha256sum` or `shasum`. The bundle carries its own `rust-analyzer`; indexing
Rust repositories additionally needs `cargo` on the `PATH`, because the
analyzer cannot load a Cargo workspace without it.

On macOS the binaries are not notarized. A release downloaded with `curl` is
not quarantined and runs; a copy downloaded with a browser needs `xattr -dr
com.apple.quarantine`. See
[docs/development/macos.md](docs/development/macos.md).

Install the latest release in one command:

```bash
curl -fsSL https://github.com/Luqueee/ladygraph/releases/latest/download/install.sh | bash
```

From a checkout, the same installer can be run directly:

```bash
./scripts/install.sh
```

To install a specific release instead of the latest one:

```bash
LADYGRAPH_VERSION=v0.6.0 ./scripts/install.sh
```

The script installs the bundle in `~/.local/opt/ladygraph` and puts launchers
in `~/.local/bin`. It never modifies a registered repository, creates an index,
or replaces configuration files. To use a different location, set
`LADYGRAPH_INSTALL_ROOT` and `LADYGRAPH_BIN_DIR`.

Add the launcher directory to the current shell and verify both runtimes:

```bash
export PATH="$HOME/.local/bin:$PATH"
ladygraph version
ladygraph-ts-worker <<'EOF'
hello
EOF
```
Check for a newer release or update the installed bundle:

```bash
ladygraph update --check
ladygraph update
```

The update is atomic, preserves the configuration and graph state, verifies
the release and bundle checksums, and replaces only the installed bundle.
Restart the MCP client after updating so it launches the new binary.

When `ladygraph` is invoked without a command from an interactive terminal, it
checks for a newer release with an 800 ms timeout and a 24-hour cache in the
platform cache directory (`$XDG_CACHE_HOME` on Linux and
`$HOME/Library/Caches` on macOS), under `ladygraph/update-check.json`.
The optional check never blocks the command when the network is unavailable.

Interactive command output uses semantic ANSI colors when the destination is a
terminal. Set `NO_COLOR` or redirect output to keep it plain.

### Configure an MCP client and install the skill

The release installer does not edit client configuration automatically. After
installing Ladygraph, run the integration commands without `--target` to detect
the coding agents present on this machine and select one or more of them:

```bash
ladygraph mcp install --scope user
ladygraph skill install --scope user
```

Ladygraph checks each client's known local configuration or installation roots
and marks detected agents. Use `↑`/`↓` (or `j`/`k`) to move, `space` to toggle
an agent, `a` to select all, `n` to select none, `Enter` to confirm, and `q` or
`Esc` to cancel. If none is detected, the selector starts with no agents
selected. Use `--target` only for scripted, non-interactive installation.

Supported MCP targets are `claude-code`, `claude-desktop`, `codex`, `opencode`,
and `oh-my-pi`. Supported skill targets are `claude-code`, `codex`, `opencode`,
and `oh-my-pi`; Claude Desktop has no local skill target. The default scope is
`user`; use `--scope project` for project-local configuration. Use `--dry-run`
to inspect a plan without writing. Existing incompatible entries stop with an
error; `--force` is required to replace or remove one. Existing files are
written atomically with mode `0600` and receive a
`*.ladygraph.bak` backup before replacement or removal.

Inspect or remove a registration explicitly:

```bash
ladygraph mcp status --target claude-code --scope user
ladygraph mcp remove --target claude-code --scope user
ladygraph skill status --target claude-code --scope user
ladygraph skill remove --target claude-code --scope user
```

Initialize and publish a graph before starting the MCP server:

```bash
ladygraph init \
  --repository project=/absolute/path/to/project \
  --languages go,typescript,rust
ladygraph doctor
ladygraph index --full
```

`init` writes a self-contained configuration: with `--config` pointing
elsewhere, its state, cache and registry hang off that directory, so a throwaway
index never touches the real one. `index --full` republishes atomically — a
failure at any stage leaves the previous generation serving. A server already
running follows the new generation on its own.

Day to day:

```bash
ladygraph graph status      # what is published, and whether a tree has moved
ladygraph doctor            # toolchains, storage, and the type-checking ceiling
ladygraph ui                # read-only 3D viewer, default 0.0.0.0:7777
ladygraph stop              # terminate this user's serve and ui, never an index
ladygraph clean --keep-active
```

`ladygraph ui` binds a non-loopback address by default, because the graph is
indexed where the repositories are and looked at from elsewhere; there is no
authentication, so it logs exactly what it exposes and `--addr` restricts it.

Configure any MCP client to start the server over STDIO:

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

`ladygraph serve` starts before a graph exists: with no published generation it
completes the handshake, publishes no query tool and puts the rebuild command in
`instructions`. A client launches the process itself, so exiting would read as a
crash. It writes MCP framing exclusively to `stdout` and logs to `stderr`.

## What the graph carries, and what it refuses to

An edge is `EXACT` only with sufficient evidence and the right provenance. It is
never created from a name, a path, an alias or a single candidate, and a
reference that cannot be resolved is published as `UNRESOLVED` with its reason,
repository and language rather than dropped. `graph_status` reports both, broken
down.

That is why some answers are absences rather than edges. With the Rust standard
library indexed, `impl Add for u32` is generated by a macro and exists in no
source range, so every use of it is declared `PROVIDER_DEFINITION_NOT_INDEXED`
once per symbol instead of becoming an edge nobody could open.

The providers Ladygraph derives from the machine — today the Rust standard
library, named `rust:1.96.1` after the toolchain — are withheld from read
results by default: one toolchain is around twenty thousand symbols, and a
search for `Clone` would answer with `core`. `include_derived` asks for them, and
`graph_status` breaks out what they contribute so the totals stay readable.

## Development

```bash
make build
make test
make test-ladybug
```

`make test-ladybug` is the only supported way to run the tag that links the
pinned native library. Contributing conventions are in
[AGENTS.md](AGENTS.md), which `CLAUDE.md` links to.

### Storage and graph benchmarks

The LadybugDB qualification, the synthetic corpus generator, the load and query
benchmarks, and the `doctor`, `rebuild`, `rollback` and `snapshot` commands are
documented in
[docs/development/storage-benchmarks.md](docs/development/storage-benchmarks.md).
It concludes with `ACCEPT_LADYBUGDB_WITH_LIMITS`.

## Structure

```text
cmd/ladygraph/   Main executable.
internal/        Ladygraph internal packages.
ts-worker/        TypeScript worker.
web/              Graph viewer served by `ladygraph ui`.
site/             Landing page and documentation site (not part of any release).
testdata/         Test fixtures and corpora.
benchmarks/       Benchmark results.
docs/             Documentation and ADRs.
scripts/          Auxiliary automation.
```

## License

Ladygraph is distributed under the [Apache License 2.0](LICENSE).

## Third-party licenses

Notices and licenses for dependencies distributed with Ladygraph are recorded in [`THIRD_PARTY_NOTICES.md`](THIRD_PARTY_NOTICES.md). The list is updated whenever a dependency is added to the distributable product.
