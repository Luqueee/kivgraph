---
title: Configuration
description: Every key of config.yaml and repositories.yaml, with its default and what it accepts.
---

`kivgraph init` writes `~/.config/kivgraph/config.yaml` and
`~/.config/kivgraph/repositories.yaml`. Paths use the `~` notation until they
are expanded at load time; after expansion every path key must be absolute.

A configuration written outside the default location is self-contained: its
state, its cache and its registry hang from its own directory. A `--config` in
`/tmp` never publishes over the real graph.

## `version`

| Key | Default | Notes |
| --- | --- | --- |
| `version` | `1` | Configuration schema version. |

## `workspace`

| Key | Default | Notes |
| --- | --- | --- |
| `repositories_file` | `~/.config/kivgraph/repositories.yaml` | The repository registry document. |

## `profiles`

| Key | Default | Notes |
| --- | --- | --- |
| `default` | `default` | Profile used when an operation does not name one. |
| `max_open` | `3` | Maximum number of profile snapshots kept mapped by one server. Must be at least `1`; the least recently used mapping is released when the bound is crossed. |

## `storage`

| Key | Default | Notes |
| --- | --- | --- |
| `database_path` | `~/.local/state/kivgraph/graph.lbdb` | The canonical LadybugDB database. A published generation lives at `generations/<id>/graph.db` in the directory that holds this path, beside the `CURRENT` and `BACKUP` pointer files, and carries the graph, the snapshot and the digests that prove they belong together. With profiles each profile derives its own `graph.lbdb` path under `<state>/profiles/<name>/`, and its generations live there, not beside this key. |
| `backups_path` | `~/.local/state/kivgraph/backups` | What `rollback` restores from. |

### Retired keys

`snapshots_path` and `retain_snapshots` no longer exist. Neither ever did
anything: nothing was written to the first, and nothing read the second.

A configuration that still carries them **loads normally** — they are accepted,
ignored, and named by `kivgraph doctor` as `config.retired` so you can delete
them. Rejecting a file that was valid when it was written would be a worse
answer than the keys were a mistake.

## `web`

| Key | Default | Notes |
| --- | --- | --- |
| `address` | `127.0.0.1:7777` | Bind of `kivgraph ui`. Must be a valid `host:port`. A loopback bind refuses a request whose `Host` is not this machine. Every non-loopback bind logs what it exposes; there is no authentication. A configuration written by an earlier `init` carries `0.0.0.0:7777` and keeps it. |

## `mcp`

| Key | Default | Notes |
| --- | --- | --- |
| `transport` | `stdio` | The only accepted value, and a decision rather than a gap. The reason to want a shared transport was memory: every client launching its own server used to mean another private copy of the graph. A server now maps the published snapshot, so the largest part of it is one copy however many are running — see [what a second server costs](/guides/indexing/#what-a-second-server-costs). A client launching its own server is not something to negotiate with the user. |

### Retired limit keys

`default_limit`, `maximum_limit`, `maximum_depth` and `maximum_visited_nodes`
no longer exist. Nothing read them: a value set here never limited a call.
The bounds of a query are fixed and each tool declares them in its schema —
at most `500` rows a page, a depth of `5` and `25000` visited nodes.

A configuration that still carries them loads normally, as with the retired
`storage` keys: they are ignored and named by `kivgraph doctor` as
`config.retired`.

## `indexing`

| Key | Default | Notes |
| --- | --- | --- |
| `generated_files` | `include` | Accepts only `include`. |
| `unresolved_references` | `retain` | Accepts only `retain`. |
| `syntax_acceleration` | `true` | |
| `full_rebuild_on_schema_change` | `true` | |
| `fact_cache` | `on` | `off`, `on` or `verify`. `verify` analyses everything and fails the pass when a servable entry disagrees with the analysis. |
| `fact_cache_path` | `~/.local/state/kivgraph/factcache` | One entry per analysis unit, outside every indexed repository. Must not be empty unless the cache is `off`. |
| `source_verification` | `full` | `full` or `signature`. A pass proves its sources did not change while it ran by observing the tree again before `CURRENT` moves. `full` hashes every analysed file again. `signature` first compares the stat of every directory it read and every analysed file with the stat taken at the first observation, and reads a tree again only when it cannot prove it would read the same: a differing stat, a missing entry, an entry modified within two seconds of the capture, a symlinked source, a platform without an inode change time. It is opt-in because the proof rests on the filesystem's timestamps: it was measured on a local APFS volume only, and a filesystem with coarse timestamps or stale attribute caches (FAT, NFS, SMB) gives it no guarantee. `index --full` prints `index.source_verification` with how many trees it did not read. See ADR 0121. |

The two words are not decoration. `generated_files` and
`unresolved_references` accept exactly one value each because that is exactly
what the pass does: it indexes generated files, and it retains every unresolved
reference. Accepting another word would promise behaviour no code implements.

## `watcher`

| Key | Default | Notes |
| --- | --- | --- |
| `enabled` | `true` | |
| `debounce_ms` | `150` | Must be positive. |
| `maximum_batch_ms` | `500` | Must be at least `debounce_ms`. |
| `reconciliation_interval` | `10m` | A Go duration string; must be positive. |

## `typescript`

| Key | Default | Notes |
| --- | --- | --- |
| `worker_command` | `kivgraph-ts-worker` | Must not be empty. |
| `maximum_workers` | `3` | Must be positive. Bounds concurrent worker processes. |
| `project_idle_timeout` | `30m` | Must be positive. |
| `include_unclaimed_sources` | `false` | Indexes the `.ts`/`.tsx`/`.mts`/`.cts` files no project claims, through TypeScript's inferred project. Those files belong to no program, so they are invisible by construction; enabling this checks them under compiler options Kivgraph chose rather than any the project declared. JavaScript is excluded: whether a `.mjs` is a source is the project's `allowJs` to answer, and this walk resolves no project. See ADR 0050. |

## `go`

| Key | Default | Notes |
| --- | --- | --- |
| `synthetic_work_file` | `~/.local/state/kivgraph/go.work` | The synthetic workspace, outside every indexed repository. |
| `include_tests` | `true` | Whether the Go tests are indexed. Tests are recognised by path (`_test.go`), so `kivgraph impact` lists a Go test among the affected symbols and `find_references` includes the tests that use a symbol only while this is `true`. A `config.yaml` written by an earlier `init` spells the key out as `false` and keeps it until edited. `find_references`, `find_implementations`, `trace_dependencies`, `get_blast_radius` and `find_cross_repo_consumers` take `exclude_tests` to leave them out of one answer. |
| `goos` | *(empty)* | The build platform's operating system, passed to every Go load as `GOOS`. Empty uses the platform of the configured Go toolchain. |
| `goarch` | *(empty)* | The build platform's architecture, passed to every Go load as `GOARCH`. Empty uses the platform of the configured Go toolchain. |
| `cgo_enabled` | *(unset)* | When set, passed to every Go load as `CGO_ENABLED`: `true` is `1` and `false` is `0`. Unset leaves the variable as the process environment has it. |
| `build_tags` | *(empty)* | The constraints every Go load satisfies. No tag may be empty or contain a comma or whitespace. Indexing the Kivgraph repository itself requires `ladybug`. |
| `allow_network` | `false` | The one declared escape from a hermetic pass: lets the go command reach a module proxy. |
| `maximum_loads` | `0` | Bounds concurrent Go loads; each holds a complete type universe. `0` uses the processor count, capped. Must not be negative. |

## `python`

| Key | Default | Notes |
| --- | --- | --- |
| `indexer_command` | `kivgraph-python-worker` | Must not be empty. |
| `analyzer_command` | `kivgraph-python-pyright` | Must not be empty. An optional type-aware producer that must emit the versioned semantic payload Kivgraph understands. |
| `analyzer_mode` | `fallback` | `fallback` or `exact`. In `fallback` the bundled AST worker reads names and imports: it resolves `module.member` and what a package re-exports, and records a use through `object.member` as unresolved, so an answer about that member says `LOWER_BOUND`. `exact` asks Pyright and resolves them. |
| `maximum_workers` | `3` | Must be positive. |
| `python_path` | `python3` | Must not be empty. |
| `include_tests` | `true` | Whether the Python tests are indexed: `test_*.py`, `*_test.py`, `conftest.py` and files under a `test`, `tests` or `integration_test` directory, the same rule `exclude_tests` applies. A `config.yaml` written by an earlier `init` spells the key out as `false` and keeps it until edited. |
| `include_generated` | `false` | |
| `include_external_packages` | `false` | |

## `dart`

| Key | Default | Notes |
| --- | --- | --- |
| `analyzer_command` | `dart` | Must not be empty. The default resolves through the `PATH`, so the fact cache fingerprint identifies the SDK it finds. |
| `maximum_workers` | `2` | Must be positive. |
| `sdk_path` | `dart` | Must not be empty. |
| `include_tests` | `true` | Whether the Dart tests are indexed, recognised by path (`test/`, `_test.dart`). |
| `include_generated` | `false` | |
| `include_external_packages` | `false` | Adds the packages the package config resolves as provider repositories of their own, so references into them have a target. Off, the registered repository is the only unit and package-cache source never enters the graph. |
| `include_sdk` | `false` | Publishes the Dart SDK itself. |
| `package_config` | `auto` | Must not be empty. `auto` uses `.dart_tool/package_config.json`. |
| `wait_for_analysis` | `true` | |
| `maximum_analysis_time` | `5m` | A Go duration string; must be positive. |

## `java`

| Key | Default | Notes |
| --- | --- | --- |
| `indexer_command` | `scip-java` | Must not be empty. Java is indexed by building it: `scip-java` drives the repository's own build tool. |
| `build_tool` | *(empty)* | Empty lets `scip-java` detect Maven, Gradle, sbt and mill on its own; name one for a repository that carries two. Accepts `maven`, `gradle`, `sbt`, `mill` or `bazel`. |
| `maximum_workers` | `1` | Must be positive. |
| `include_tests` | `true` | Whether the Java tests are indexed. A test is a file under a `test` or `tests` directory or `src/it/`, or named `*Test.java` or `*Tests.java`, the same rule `exclude_tests` applies. A `config.yaml` written by an earlier `init` spells the key out as `false` and keeps it until edited. |
| `include_generated` | `false` | |
| `target_directory` | `~/.local/state/kivgraph/java-target` | Where the indexer writes its SemanticDB output and its index, outside every indexed repository. Must be absolute after expansion. |
| `maximum_index_time` | `20m` | A Go duration string; must be positive. |

## `csharp`

| Key | Default | Notes |
| --- | --- | --- |
| `indexer_command` | `scip-dotnet` | Must not be empty. C# is indexed by building it: `scip-dotnet` runs `dotnet restore` and drives Roslyn. |
| `project` | *(empty)* | The `.sln` or `.csproj` to index. Empty discovers one, and a solution wins over a project. |
| `maximum_workers` | `1` | Must be positive. |
| `include_tests` | `true` | Whether the C# tests are indexed. A test is a file under a `test` or `tests` directory or named `*Test.cs` or `*Tests.cs`, the same rule `exclude_tests` applies. A `config.yaml` written by an earlier `init` spells the key out as `false` and keeps it until edited. |
| `include_generated` | `false` | |
| `skip_restore` | `false` | An index built without a restore loses every symbol that comes from a package. |
| `target_directory` | `~/.local/state/kivgraph/csharp-target` | Where the indexer writes its output, outside every indexed repository. Must be absolute after expansion. |
| `maximum_index_time` | `20m` | A Go duration string; must be positive. |

## `rust`

| Key | Default | Notes |
| --- | --- | --- |
| `analyzer_command` | `rust-analyzer` | Must not be empty. The bundled binary beside the executable wins, then this path, then the `PATH`. |
| `maximum_workspaces` | `0` | Bounds concurrent `rust-analyzer` invocations; each holds a whole Cargo workspace in memory. `0` uses the processor count, capped. |
| `features` | *(empty)* | Cargo features to activate. Cannot be combined with `all_features`. |
| `all_features` | `false` | |
| `no_default_features` | `false` | |
| `cfgs` | *(empty)* | Additional `--cfg` values the analysis assumes. |
| `build_scripts` | `true` | |
| `proc_macros` | `true` | |
| `include_tests` | `true` | Sets `cfg(test)`. Turning it off removes every test item from the graph, and the grammar then reports each one as a declaration the index does not carry. |
| `allow_network` | `false` | Lets cargo reach a registry while the analyzer loads a workspace. |
| `target_directory` | `~/.local/state/kivgraph/rust-target` | Build artifacts of the analysis, outside every indexed repository. |
| `sysroot` | `discover` | `discover`, `none`, or a path. Where the standard library is, never whether it enters the graph: loading it is what lets the analyzer resolve `Vec` at all. |
| `index_sysroot` | `false` | Publishes the standard library as a synthetic provider repository named after the toolchain release, such as `rust:1.96.1`. |

A symbol behind an inactive feature is absent from the graph and reported as
unresolved. Feature selection is therefore part of what the graph *is*, not a
performance knob.

### The standard library

With `index_sysroot` off, four things leave no edge at all: `#[derive(...)]`, an
overloaded operator, the `?` operator, and every call into the standard library.
Each one resolves to a symbol of `core`, `alloc` or `std`, and nothing in the
graph declares it. The pass says so, per crate, as `CRATE_PROVIDER_NOT_FOUND`.

With it on, they become exact edges. It costs one extra analysis unit — around
`19.500` symbols and one cold pass per toolchain — and the fact cache serves it
afterwards, because the cache fingerprint includes `rustc --version`. A machine
with no toolchain, or one without the `rust-src` component, indexes its
repositories and reports why the standard library is absent; it is never a
failure. Kivgraph does not install the component either: with `sysroot` set to
`discover` it asks `rustc --print sysroot` and hands the analyzer that path, so
a missing `rust-src` stays missing unless `allow_network` is on. It looks for
`rustc` where the analyzer does: `$RUSTC`, then `$CARGO_HOME/bin`, then the
`PATH`. Run `rustup component add rust-src` to have the standard library
resolved; the next index notices and re-analyses.

Nor does a pass install a toolchain. A workspace whose `rust-toolchain.toml`
names a release that is not installed is reported as not loaded; run
`rustup toolchain install` in that directory.

Read tools withhold it by default. `find_symbol`, `find_references`,
`trace_dependencies` and `get_blast_radius` accept `include_derived: true` to ask
for it, and naming the repository in `repo` is a request for it too.
`graph_status` reports what it contributes under `derived` — including its own
unresolved references, which the standard library declares by the thousand — and
`list_repositories` marks the row.

## `telemetry`

| Key | Default | Notes |
| --- | --- | --- |
| `metrics` | `true` | |
| `traces` | `false` | |

OpenTelemetry integration is optional; exporters and collectors stay disabled
by default and the configured provider belongs to the caller.

## `logging`

| Key | Default | Notes |
| --- | --- | --- |
| `format` | `json` | `json` or `text`. |
| `level` | `info` | `debug`, `info`, `warn` or `error`. |
| `event_log_path` | `~/.local/state/kivgraph/events.jsonl` | The append-only record `kivgraph logs` and `kivgraph tool-stats` read. |

`event_log_path` is state, not configuration: it holds one JSON object per line
describing an indexing pass, a tool call or a server's lifecycle. It rotates at
8 MiB and keeps one rotation, so the history costs at most 16 MiB and a store
that outgrows that drops its oldest records. Deleting it loses history and
nothing else. An empty value is refused rather than defaulted, because the
default lives in the shared state directory and substituting it would make an
isolated configuration write into the real installation.

## The repository registry

`repositories.yaml` carries a `version` and a list of `repositories`:

| Key | Notes |
| --- | --- |
| `name` | The identifier. Compared exactly, case included, and carried inside stable keys. |
| `path` | Absolute path to the checkout. |
| `languages` | At least one of `go`, `typescript`, `javascript`, `ts`, `js`, `rust`, `rs`, `python`, `py`, `dart`, `java`, `csharp`, `cs`. Compared without regard to case or surrounding space; a duplicate is refused. Validated when the registry is written, not only when a pass runs. |
| `manifests` | Optional. Explicit manifest paths. |
| `roots` | Optional. Explicit analysis roots. |
| `exclusions` | Optional. Directories and files left out of the graph, for every language of the repository. A pattern is relative to the repository root: `vendor` is the root's `vendor`, `**/vendor` is one at any depth, and `./x`, an absolute path inside the repository, `*` and `**` are read. Go, TypeScript, Rust and Dart never walk them; Java, C# and Python are analysed whole by their tools and lose the excluded files afterwards. |

```yaml
version: 1
repositories:
  - name: project
    path: /absolute/path/to/project
    languages: [go, typescript, rust]
    exclusions: [vendor, third_party]
```

Independently of `exclusions`, a directory that holds a valid `CACHEDIR.TAG` is
never walked: it is the build tool's own statement that the directory is a
reproducible cache (Cargo writes one in every `target/`). The tag must be a
regular file, not a symlink, and start with the signature of the
[Cache Directory Tagging Specification](https://bford.info/cachedir/). A
directory with no tag, or a malformed one, is walked as before, and so is the
repository root. A directory of real sources that carries such a tag is not
indexed; remove the tag to index it. `kivgraph doctor` lists the skipped
directories on its `walk.cache_tag` line. See ADR 0122.

For Go, discovery reads the packages the `go` tool would. A directory named
`testdata`, or beginning with `_` or `.`, is not a package of the module it sits
in and is not parsed, so a fixture that is not valid Go cannot stop the index. A
module that lives inside one (a `go.mod` there, or a `replace` pointing at it) is
still a module. A file marked `//go:build ignore` is not part of its package. A
directory that is not one valid package, two package names or a file that is not
Go, does not stop the index either: `go` reports it and the module that holds it
is declared not loaded, with go's reason, while every other repository is
indexed. See ADR 0123.
