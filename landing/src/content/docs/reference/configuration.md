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

## `storage`

| Key | Default | Notes |
| --- | --- | --- |
| `database_path` | `~/.local/state/kivgraph/graph.lbdb` | The canonical LadybugDB database. |
| `snapshots_path` | `~/.local/state/kivgraph/snapshots` | Published generations. |
| `backups_path` | `~/.local/state/kivgraph/backups` | What `rollback` restores from. |
| `retain_snapshots` | `3` | Must be positive. |

## `web`

| Key | Default | Notes |
| --- | --- | --- |
| `address` | `0.0.0.0:7777` | Bind of `kivgraph ui`. Must be a valid `host:port`. Every non-loopback bind logs what it exposes; there is no authentication. |

## `mcp`

| Key | Default | Notes |
| --- | --- | --- |
| `transport` | `stdio` | The only supported value. |
| `default_limit` | `50` | Must be positive. |
| `maximum_limit` | `500` | Must be at least `default_limit`. |
| `maximum_depth` | `5` | Must be between `1` and `5`. |
| `maximum_visited_nodes` | `25000` | Must be positive. |

## `indexing`

| Key | Default | Notes |
| --- | --- | --- |
| `generated_files` | `include` | Accepts only `include`. |
| `unresolved_references` | `retain` | Accepts only `retain`. |
| `syntax_acceleration` | `true` | |
| `full_rebuild_on_schema_change` | `true` | |
| `fact_cache` | `on` | `off`, `on` or `verify`. `verify` analyses everything and fails the pass when a servable entry disagrees with the analysis. |
| `fact_cache_path` | `~/.local/state/kivgraph/factcache` | One entry per analysis unit, outside every indexed repository. Must not be empty unless the cache is `off`. |

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

## `go`

| Key | Default | Notes |
| --- | --- | --- |
| `synthetic_work_file` | `~/.local/state/kivgraph/go.work` | The synthetic workspace, outside every indexed repository. |
| `include_tests` | `false` | |
| `build_tags` | *(empty)* | The constraints every Go load satisfies. No tag may be empty or contain a comma or whitespace. Indexing the Kivgraph repository itself requires `ladybug`. |
| `allow_network` | `false` | The one declared escape from a hermetic pass: lets the go command reach a module proxy. |
| `maximum_loads` | `0` | Bounds concurrent Go loads; each holds a complete type universe. `0` uses the processor count, capped. Must not be negative. |

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
failure.

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
| `languages` | Any of `go`, `typescript`, `rust`. Validated when the registry is written, not only when a pass runs. |
| `manifests` | Optional. Explicit manifest paths. |
| `roots` | Optional. Explicit analysis roots. |
| `exclusions` | Optional. Directories the discovery never walks, and therefore never analyses. |

```yaml
version: 1
repositories:
  - name: project
    path: /absolute/path/to/project
    languages: [go, typescript, rust]
    exclusions: [vendor, third_party]
```
