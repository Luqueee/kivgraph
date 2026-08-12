---
title: Configuration
description: Every key of config.yaml and repositories.yaml, with its default and what it accepts.
---

`ladygraph init` writes `~/.config/ladygraph/config.yaml` and
`~/.config/ladygraph/repositories.yaml`. Paths use the `~` notation until they
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
| `repositories_file` | `~/.config/ladygraph/repositories.yaml` | The repository registry document. |

## `storage`

| Key | Default | Notes |
| --- | --- | --- |
| `database_path` | `~/.local/state/ladygraph/graph.lbdb` | The canonical LadybugDB database. |
| `snapshots_path` | `~/.local/state/ladygraph/snapshots` | Published generations. |
| `backups_path` | `~/.local/state/ladygraph/backups` | What `rollback` restores from. |
| `retain_snapshots` | `3` | Must be positive. |

## `web`

| Key | Default | Notes |
| --- | --- | --- |
| `address` | `0.0.0.0:7777` | Bind of `ladygraph ui`. Must be a valid `host:port`. Every non-loopback bind logs what it exposes; there is no authentication. |

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
| `fact_cache_path` | `~/.local/state/ladygraph/factcache` | One entry per analysis unit, outside every indexed repository. Must not be empty unless the cache is `off`. |

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
| `worker_command` | `ladygraph-ts-worker` | Must not be empty. |
| `maximum_workers` | `3` | Must be positive. Bounds concurrent worker processes. |
| `project_idle_timeout` | `30m` | Must be positive. |

## `go`

| Key | Default | Notes |
| --- | --- | --- |
| `synthetic_work_file` | `~/.local/state/ladygraph/go.work` | The synthetic workspace, outside every indexed repository. |
| `include_tests` | `false` | |
| `build_tags` | *(empty)* | The constraints every Go load satisfies. No tag may be empty or contain a comma or whitespace. Indexing the Ladygraph repository itself requires `ladybug`. |
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
| `target_directory` | `~/.local/state/ladygraph/rust-target` | Build artifacts of the analysis, outside every indexed repository. |
| `sysroot` | `discover` | `discover`, `none`, or a path. Without a sysroot the standard library is absent and the proc-macro server cannot start. |

A symbol behind an inactive feature is absent from the graph and reported as
unresolved. Feature selection is therefore part of what the graph *is*, not a
performance knob.

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
