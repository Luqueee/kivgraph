# Kivgraph

Kivgraph is a local **cross-repository code intelligence MCP server for AI
coding agents**. It builds a canonical semantic code graph across multiple
registered repositories and answers questions about symbols, repository
relationships, callers, dependencies and change impact.

It indexes a corpus once and serves an immutable graph: the edges are resolved
by `go/types`, the TypeScript checker and `rust-analyzer`, not by matching
names. That is the difference from a search tool, and it is what makes an empty
answer worth something — an empty reference list means **nobody calls it**, not
that nothing was found, and `grep` cannot tell those apart.

Kivgraph is focused on semantic code relationships, not automatic discovery of
every HTTP, gRPC, Kafka or database runtime flow between services.

## Documentation

Read the [Kivgraph user documentation](https://github.com/Luqueee/kivgraph/tree/main/landing/src/content/docs)
for installation, MCP clients, code intelligence, repository relationships and
workspace code graphs. The published site is configured separately from the
release bundle; this link remains valid from every checkout.

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
absence. Measured over 29 questions against a 37-repository corpus
(`benchmarks/graph-tools-comparison/results-all.json`, commit `954b9eb`,
tokenizer `o200k_base`): `35,961` tokens for Kivgraph against `267,980` for
`grep` plus reading, both exact on 28 of the 29, median `5.95x` per question in
Kivgraph's favour. `grep` is cheaper on 5 of those 29, all of them at full
recall on both sides: `T1_go_trivial` asks for a name the corpus declares
twice, and there `grep` costs `0.53x` what Kivgraph does.

A second harness, `benchmarks/mcp-token-cost`, compares against the host's own
tool output captured verbatim, but it runs on Kivgraph's own single repository
of 13,222 symbols: `7.64x` on the answers themselves and `1.60x` over a whole
session, against a `2.41x` floor set by the source bodies both arms pay for.

## Status

Released and in use. `kivgraph version` reports the published release; the
backlog and the acceptance gate of every phase are in [`TASKS.md`](TASKS.md).

- **Languages:** Go, TypeScript, Rust, Python and Dart. Python uses the
  bundled AST worker in fallback mode; those inferred references are
  `CANDIDATE`, never `EXACT`. Exact Python mode uses the bundled Pyright LSP
  adapter with an installed Pyright/BasedPyright server. Dart uses the Dart
  Analysis Server supplied by the Dart or Flutter SDK.
- **Semantic dependencies:** Python and Dart imports can publish a package
  dependency when exactly one registered provider owns the requested package;
  symbol-level cross-repository edges require an explicit provider identity.
- **Surface:** ten read-only tools over STDIO, plus one consent-gated
  mutation (`index_project`). The contract is
  [docs/protocol/mcp-surface-v3.md](docs/protocol/mcp-surface-v3.md).
- **Storage:** LadybugDB is canonical; queries are served from an immutable
  HotSnapshot published atomically, never from the database.
- **Platforms:** `linux/amd64` and `darwin/arm64`.
- **Viewer:** `kivgraph ui` serves a read-only 3D view of the published graph.

## Requirements

- Go 1.26 or later to build from source. The indexer type-checks with the
  `go/types` linked into the binary, so it can only read repositories and
  dependencies written for its own language version or older; `kivgraph doctor`
  reports that ceiling.
- Indexing Rust needs `cargo` and `rust-analyzer`. The release bundle carries
  the analyzer; it does not carry a Rust toolchain.
- Indexing TypeScript needs Node.js 22 or later for the worker.
- Indexing Python needs Python 3.10 or later for the bundled worker. It is a
  syntax-aware fallback and reports dynamic or unresolved names explicitly;
  exact mode additionally requires a Pyright-compatible language server.
- Indexing Dart needs the `dart` executable; a Flutter installation supplies
  it. The loader uses the Analysis Server protocol and does not modify the
  Flutter project.

## Installation

### Install the MCP with one script

The installer detects the platform, downloads the latest published MCP release
for it, verifies both the release archive and the bundle checksums, and
installs it without requiring Go or pnpm. The release contains the Go server,
the pinned LadybugDB library, the TypeScript worker, the bundled Python AST
worker, the pinned `rust-analyzer`, the grammar manifest and the web viewer,
whose assets are 2.3 MB of the bundle. `scripts/build-bundle.sh --mcp-only`
produces a bundle without the viewer for anyone who wants one.

Published bundles: Linux `amd64` and macOS `arm64`.

Runtime requirements: Bash, Node.js `22` or later, Python 3.10 or later when
indexing Python, `curl`, `tar`, and `sha256sum` or `shasum`. The bundle carries
its own `rust-analyzer`; indexing Rust repositories additionally needs `cargo`
on the `PATH`, and indexing Dart needs the Dart or Flutter SDK.

On macOS the binaries are not notarized. A release downloaded with `curl` is
not quarantined and runs; a copy downloaded with a browser needs `xattr -dr
com.apple.quarantine`. See
[docs/development/macos.md](docs/development/macos.md).

Install the latest release in one command:

```bash
curl -fsSL https://github.com/Luqueee/kivgraph/releases/latest/download/install.sh | bash
```

From a checkout, the same installer can be run directly:

```bash
./scripts/install.sh
```

To install a specific release instead of the latest one:

```bash
KIVGRAPH_VERSION=v0.8.0 ./scripts/install.sh
```

The script installs the bundle in `~/.local/opt/kivgraph` and puts launchers
in `~/.local/bin`. It never modifies a registered repository, creates an index,
or replaces configuration files. To use a different location, set
`KIVGRAPH_INSTALL_ROOT` and `KIVGRAPH_BIN_DIR`.

Add the launcher directory to the current shell and verify both runtimes:

```bash
export PATH="$HOME/.local/bin:$PATH"
kivgraph version
kivgraph-ts-worker <<'EOF'
hello
EOF
```
Check for a newer release or update the installed bundle:

```bash
kivgraph update --check
kivgraph update
```

The update is atomic, preserves the configuration and graph state, verifies
the release and bundle checksums, and replaces only the installed bundle.
Restart the MCP client after updating so it launches the new binary.

When `kivgraph` is invoked without a command from an interactive terminal, it
checks for a newer release with an 800 ms timeout and a 24-hour cache in the
platform cache directory (`$XDG_CACHE_HOME` on Linux and
`$HOME/Library/Caches` on macOS), under `kivgraph/update-check.json`.
The optional check never blocks the command when the network is unavailable.

Interactive command output uses semantic ANSI colors when the destination is a
terminal. Set `NO_COLOR` or redirect output to keep it plain.

### Configure an MCP client and install the skill

The release installer does not edit client configuration automatically. After
installing Kivgraph, run the integration commands without `--target` to detect
the coding agents present on this machine and select one or more of them:

```bash
kivgraph mcp install --scope user
kivgraph skill install --scope user
```

Kivgraph checks each client's known local configuration or installation roots
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
`*.kivgraph.bak` backup before replacement or removal.

Inspect or remove a registration explicitly:

```bash
kivgraph mcp status --target claude-code --scope user
kivgraph mcp remove --target claude-code --scope user
kivgraph skill status --target claude-code --scope user
kivgraph skill remove --target claude-code --scope user
```

Initialize and publish a graph before starting the MCP server:

```bash
kivgraph init \
  --repository project=/absolute/path/to/project \
  --languages go,typescript,rust
kivgraph doctor
kivgraph index --full
```

`init` writes a self-contained configuration: with `--config` pointing
elsewhere, its state, cache and registry hang off that directory, so a throwaway
index never touches the real one. `index --full` republishes atomically — a
failure at any stage leaves the previous generation serving. A server already
running follows the new generation on its own.

Day to day:

```bash
kivgraph graph status      # what is published, and whether a tree has moved
kivgraph doctor            # toolchains, storage, and the type-checking ceiling
kivgraph ui                # read-only 3D viewer, default 0.0.0.0:7777
kivgraph logs --follow     # what it indexed, served and answered, as it happens
kivgraph tool-stats        # per-tool cost, calls, and failures
kivgraph stop              # terminate this user's serve and ui, never an index
kivgraph clean --keep-active
```

`kivgraph ui` binds a non-loopback address by default, because the graph is
indexed where the repositories are and looked at from elsewhere; there is no
authentication, so it logs exactly what it exposes and `--addr` restricts it.

`logs` and `tool-stats` read an append-only record in the state directory
rather than asking a server, which is why they can answer at all: the per-tool
counters a `serve` keeps are minted when it starts and gone when it stops.
Reading the file also makes the answer span every server that ever ran.

Configure any MCP client to start the server over STDIO:

```json
{
  "mcpServers": {
    "kivgraph": {
      "command": "/home/user/.local/bin/kivgraph",
      "args": [
        "serve",
        "--config",
        "/home/user/.config/kivgraph/config.yaml"
      ]
    }
  }
}
```

`kivgraph serve` starts before a graph exists: with no published generation it
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

The providers Kivgraph derives from the machine — today the Rust standard
library, named `rust:1.96.1` after the toolchain — are withheld from read
results by default: one toolchain is around twenty thousand symbols, and a
search for `Clone` would answer with `core`. `include_derived` asks for them, and
`graph_status` breaks out what they contribute so the totals stay readable.

## Development

```bash
make build
make test
make semantic-coverage
make test-ladybug
```

`make test-ladybug` is the only supported way to run the tag that links the
pinned native library. Contributing conventions are in
[AGENTS.md](AGENTS.md), which `CLAUDE.md` links to.

`make semantic-coverage` is the release gate for Go, TypeScript, Python and
Dart. It validates the machine-readable matrix in
`testdata/semantic-coverage/manifest.json`, runs the exact TypeScript, Go and
Dart suites, and requires a Pyright-compatible language server for the exact
Python suite. A language is not considered complete when a capability has a
fixture but no executable regression test.

### Storage and graph benchmarks

The LadybugDB qualification, the synthetic corpus generator, the load and query
benchmarks, and the `doctor`, `rebuild`, `rollback` and `snapshot` commands are
documented in
[docs/development/storage-benchmarks.md](docs/development/storage-benchmarks.md).
It concludes with `ACCEPT_LADYBUGDB_WITH_LIMITS`.

### The public site

`landing/` carries the landing page and the user documentation. It ships in no
release bundle, is verified with `make landing-check` and `make landing-build`,
and is served on port `6767`. What it publishes, how the MCP reference was
captured, and what is still open are recorded in
[docs/development/landing-site.md](docs/development/landing-site.md).

## Structure

```text
cmd/kivgraph/   Main executable.
internal/        Kivgraph internal packages.
ts-worker/        TypeScript worker.
web/              Graph viewer served by `kivgraph ui`.
landing/          Landing page and documentation site (not part of any release).
testdata/         Test fixtures and corpora.
benchmarks/       Benchmark results.
docs/             Documentation and ADRs.
scripts/          Auxiliary automation.
```

## License

Kivgraph is distributed under the [Apache License 2.0](LICENSE).

## Third-party licenses

Notices and licenses for dependencies distributed with Kivgraph are recorded in [`THIRD_PARTY_NOTICES.md`](THIRD_PARTY_NOTICES.md). The list is updated whenever a dependency is added to the distributable product.
