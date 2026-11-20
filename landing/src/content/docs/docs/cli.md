---
title: CLI
description: Kivgraph help lists every command in six groups.
---

`kivgraph --help`, `-h` and `help` write to `stdout` and exit `0`. So does a
bare `kivgraph`: an invocation with no command asks what the program does, and
it is answered with this same table. An **unknown** command writes a single
line to `stderr` and exits `2`, rather than dumping the whole surface at
someone who mistyped one word.

## Getting started

| Invocation | Summary |
| --- | --- |
| `init [--repository NAME=PATH] [--languages LIST]` | Write the configuration and register repositories |
| `index [--config PATH] [--repositories PATH] [--json]` | Detect the current project languages and publish a local full index |
| `index --full [--profile NAME] [--json]` | Index every repository in one profile and publish a generation |
| `serve [--introspection]` | Run the MCP server over stdio |
| `daemon [--addr HOST:PORT] [--allow-remote]` | Serve MCP to many clients from one process, over HTTP and a unix socket |
| `daemon install [--addr HOST:PORT] [--allow-remote]` | Give the daemon an owner, so the platform starts it and restarts it |
| `daemon remove` | Stop the daemon and take its supervisor entry out |
| `ui [--addr HOST:PORT] [--profile NAME]` | Serve every profile in the read-only graph viewer, or pin one by name |
| `stop [--dry-run]` | Stop every running serve, daemon and ui of this user |

`index` without `--full` indexes the project the shell is standing in. It
resolves the nearest containing Git repository as the project root, or uses the
current directory when there is none, detects the supported languages from
source extensions and project manifests, creates or reuses
`.kivgraph/config.yaml` and `.kivgraph/repositories.yaml` in that project, adds
or updates a repository named `project` there, and runs the same complete
rebuild as `index --full`. The shared user registry is not touched. An entry
that already exists keeps its manifests, roots and exclusions; only its
languages are updated. A project in which no supported language is detected is
an error and publishes nothing. With `--json`, `stdout` carries only the event
stream and the detection messages go to `stderr`. See ADR 0086.

## Profiles

| Invocation | Summary |
| --- | --- |
| `profile create NAME` | Create an empty independent graph profile |
| `profile list` | List profiles and mark the configured default with `*` |
| `profile use NAME` | Move the default pointer without changing the profile |
| `profile remove NAME --yes` | Permanently remove a non-default profile and its indexed state |

Profiles share one installation, daemon and analyzer toolchain, but each owns
its repository registry, fact cache, canonical database and generations.
`index --full` uses `profiles.default` when no profile is named. `ui` opens every
published profile by default and represents every emitted relationship through
semantic groups with occurrence totals; `--profile` pins the viewer to one
profile.

The help has no group of its own for these commands: it lists `profile create`,
`profile list` and `profile use` under Getting started and `profile remove`
under Maintenance.

## Toolchains

- `toolchain status [--config PATH] [--json]`: Report optional analyzers
  managed by this installation.
- `toolchain install pyright [--config PATH] [--version VERSION] [--json]`:
  Install and activate the pinned Pyright analyzer.
- `toolchain remove pyright [--config PATH] --yes [--json]`: Remove the managed
  Pyright analyzer and restore fallback mode when the selected configuration
  uses the managed analyzer.

The family is explicit: indexing never installs a host dependency. The first
managed analyzer is Pyright, installed under Kivgraph state and enabled by
updating only the Python analyzer settings in the selected configuration.

## Diagnostics

| Invocation | Summary |
| --- | --- |
| `doctor` | Check configuration, toolchains and the published graph |
| `doctor repositories [--repository NAME] [--json]` | Audit whether every registered repository can be indexed, and say what to change |
| `doctor storage --database PATH` | Inspect one LadybugDB database file |
| `doctor graph --database PATH` | Validate the canonical graph of a database |
| `graph status --root PATH` | Report the active and backup generations |
| `daemon status` | Report whether the daemon has an owner, and where its unit lives |
| `stats [--interval D] [--once] [--json]` | Watch what every kivgraph process on this machine costs |
| `logs [--follow] [--kind K] [--since D] [--json]` | Read what this machine indexed, served and answered |
| `tool-stats [--tool NAME] [--since D] [--json]` | Report the cost and the failures of every tool |
| `impact --base REF [--depth N] [--max-nodes N] [--limit N] [--allow-stale] [--json]` | Show what a diff reaches: the symbols it touches, what depends on them, and the tests |
| `version [--json]` | Print the release, with --json for full provenance |

`logs` shows the newest `200` records by default. Beyond the flags in its row
it takes `--limit N`, `--tool NAME`, `--failures` to keep only what did not
answer, and `--config PATH`. `--kind` accepts a comma-separated list of
`serve`, `tool` and `index`.

### What a diff reaches

`impact --base REF` reads `git diff --unified=0 REF` against the working tree,
plus the untracked files git does not ignore, and maps each run of changed
lines onto the innermost symbol that holds it: a hunk inside a method touches
the method, not its type, and a hunk across two methods touches both. It then
runs one traversal from all the touched symbols with the bounds, edge kinds and
withheld kinds of [`get_blast_radius`](/docs/tools/get-blast-radius/):
`--depth` defaults to `3` and accepts `1` to `5`, `--max-nodes` defaults to
`5000` and accepts `1` to `25000`, and `field` and `variable` symbols are left
out. A symbol that depends on several touched ones is counted once, and a
touched symbol is never reported as affected.

The affected symbols are split in two. Tests are listed apart, grouped by file
(`by_test_file` with `--json`); the rest is `production`, capped by `--limit`
(default `50`) with the whole count kept. `--json` writes the report together
with `base`, `base_commit` and `freshness`. `--config PATH` reads that
configuration instead of the project's `.kivgraph/config.yaml` or the default
one.

It reads the graph the last index published and does not index. Its limits:

- A graph older than the working tree refuses to answer, because the changed
  lines would land on the wrong symbols. Run `kivgraph index` first, or pass
  `--allow-stale` to answer anyway with a warning. A graph whose freshness
  could not be established answers with a warning.
- Tests are recognised by path convention, because the graph carries no test
  flag: `_test.go`, `_test.py`, `_test.dart`, `test_*.py`, `conftest.py`,
  `*.test.*` and `*.spec.*`, Java and C# files whose name ends in `Test` or
  `Tests`, a `test`, `tests`, `__tests__`, `spec`, `specs` or
  `integration_test` directory, and Maven's `src/it/`. Go
  tests therefore appear unless `go.include_tests` is `false`, and a Rust test
  module inside a source file is not seen.
- A removed symbol is not in a graph built from the new tree, so a deleted
  file is reported under `not_indexed` and not mapped.
- `not_indexed` also lists the changed tracked files the graph holds no record
  of, and `no_symbol` the changed lines no symbol holds, such as a comment or
  an import. Untracked files that are not in the graph are not reported.
- A ref that names no commit is refused, and so is a checkout that is not a
  registered repository.

## Maintenance

| Invocation | Summary |
| --- | --- |
| `upgrade` | Rebuild the graph after a schema change |
| `clean [--keep-active] [--yes]` | Remove published graph generations |
| `rollback --root PATH [--generation ID]` | Return to the previous generation |
| `snapshot --root PATH [--generation ID]` | Rebuild the hot snapshot of a generation |
| `update [--check] [--channel stable\|dev] [--stop]` | Install the latest release from a selected channel |

`update --channel` selects the release stream, `stable` or `dev`. Without the
flag it reads `KIVGRAPH_UPDATE_CHANNEL`, and with neither it follows the
installed version: a prerelease binary stays on `dev` and a stable one on
`stable`. The development channel offers only prereleases, so going back from
a development build to stable releases is `kivgraph update --channel stable`.

## Integrations

| Invocation | Summary |
| --- | --- |
| `configure [--target TARGET]... [--daemon] [--stdio] [--dry-run] [--force]` | Configure MCP, skills, hooks, daemon and user instructions |
| `mcp install [--scope user\|project] [--stdio]` | Detect and register one or more MCP clients, against the daemon by default |
| `mcp status --target TARGET [--scope user\|project] [--stdio]` | Inspect a client MCP registration |
| `mcp remove --target TARGET [--scope user\|project] [--stdio]` | Remove only Kivgraph's MCP registration |
| `skill install [--scope user\|project]` | Detect and install the Agent Skill in one or more clients |
| `skill status --target TARGET [--scope user\|project]` | Inspect the installed Agent Skill |
| `skill remove --target TARGET [--scope user\|project]` | Remove only Kivgraph's Agent Skill |
| `hook install [--scope user\|project]` | Detect and install the pre-tool-use gate in one or more clients |
| `hook status --target TARGET [--scope user\|project]` | Inspect the installed pre-tool-use gate |
| `hook remove --target TARGET [--scope user\|project]` | Remove only Kivgraph's pre-tool-use gate |
| `instructions install [--agent AGENT] [--file AGENTS.md\|CLAUDE.md\|.omp/AGENTS.md] [--dry-run] [--force]` | Add Kivgraph instructions to a user agent context file |
| `completion bash\|zsh\|fish` | Print the shell completion script for one shell |

`configure` is the guided setup the installer offers. For each selected client
it registers the MCP server, installs the Agent Skill and the pre-tool-use gate
where the client supports them, and adds the user instructions, all in user
scope. It writes the empty Kivgraph configuration when there is none, and it
neither registers a repository nor indexes. `--target` is repeatable and skips
the selector, `--stdio` writes per-client `serve` entries instead of using the
daemon, `--daemon` requires and installs the supervised daemon without asking,
`--dry-run` shows the changes without writing and `--force` replaces all
Kivgraph-owned entries. `--stdio` and `--daemon` cannot be combined.

`instructions install` writes a managed block into a user context file. Without
`--agent` it asks which clients to write for. `--file` is the deprecated
selector and cannot be combined with `--agent`.

### Shell completion

```bash
kivgraph completion bash > /usr/local/etc/bash_completion.d/kivgraph   # bash 3.2 and newer
kivgraph completion zsh  > "${fpath[1]}/_kivgraph"
kivgraph completion fish > ~/.config/fish/completions/kivgraph.fish
```

The script is a fixed stub: it carries no command name, no flag and no
vocabulary, and forwards the words typed so far to `kivgraph __complete`. That
is what keeps it from going out of date when a flag is added, and it is what
lets completion answer the questions a static script cannot: `--generation`
completes the generations on disk, `--target` the clients this machine has, and
`--tool` the tools this installation has actually been called with. A flag that
takes a path defers to the shell's own file completion.

## Pipeline

| Invocation | Summary |
| --- | --- |
| `rebuild --facts PATH --root PATH ...` | Publish a generation from a fact set |
| `benchmark generate-graph` | Generate a synthetic corpus |

## Inspecting the tool catalog with no index

`serve --introspection` publishes the complete MCP tool catalog even when no
generation exists, for the inspectors and registries that read tool definitions
before anything has been indexed. It creates no index and relaxes no check:
graph-dependent query tools answer `INDEX_NOT_READY` until a generation is
published, while `graph_status` reports an empty graph status. `index_project`
stays behind its consent gate. Plain `serve` is unchanged, and this is not the
configuration to give a client. See
[MCP tools](/docs/mcp-tools/).

## Builds without the viewer

`ui` needs the web bundle, which is only linked in when the binary is built
with the `webassets` tag. A build without it marks the command as unavailable
in the help and refuses to start rather than serving a "bundle not available"
page. The published release bundle carries the viewer.

## Logging

A one-shot command reports in plain text when `stderr` is a terminal and as a
JSON record when it is not; `serve` and `ui` always log JSON, because a client
reads their `stderr`. Progress is `INFO` and only a failure is `ERROR`.
