---
title: MCP troubleshooting
description: The failure modes of the Kivgraph MCP server, each with the message you see, what causes it, and the command that fixes it.
---

Every failure the tool surface returns carries a stable code and a human-readable message. Branch on the code; the message text is free to change.

| Code | Meaning |
| --- | --- |
| `INVALID_ARGUMENT` | The arguments cannot name a symbol, a page or a limit. |
| `SYMBOL_NOT_FOUND` | Nothing in the published graph answers to that selector. |
| `AMBIGUOUS_SYMBOL` | The qualified name matches more than one symbol. |
| `REPOSITORY_NOT_FOUND` | The `repository` given is not in the published graph. |
| `CURSOR_INVALID` | The `cursor` is malformed, or does not match the active query and sorting. |
| `CURSOR_SNAPSHOT_EXPIRED` | The cursor belongs to an older snapshot. |
| `TRAVERSAL_LIMIT_REACHED` | The traversal exceeded its deadline. |
| `SNAPSHOT_UNAVAILABLE` | The published snapshot is inconsistent or could not be read. |
| `INDEX_NOT_READY` | No graph is published. |
| `PERMISSION_REQUIRED` | `index_project` was called without approval. |
| `PERMISSION_DENIED` | The user refused the `index_project` elicitation. |
| `INDEXING_FAILED` | `index_project` ran and the pass failed. |

## The client sees no tools

**Symptom**

The client lists one tool, `index_project`, and nothing else. The session instructions read:

```text
Kivgraph has no published graph to answer from, so it exposes no query tools. Run "kivgraph index --full" to build one, then restart this server. Until then, use the host's own search and file tools.
```

**Cause**

`serve` checks for a published generation before it registers anything. With none, it registers only `index_project` and returns. `index_project` is the exception because it is how a client without a graph builds one, and it needs no graph to run.

The handshake still completes. The client spawns this process itself, so exiting reads as a crash and says nothing; and publishing ten tools that would answer `INDEX_NOT_READY` to everything teaches the agent that the tools do not work.

**Fix**

Build a generation, then restart the server so it loads it:

```bash
kivgraph index --full
```

While the store is empty, `kivgraph doctor` says so without failing:

```text
graph.store: PASS (no published generation)
snapshot: PASS (no published generation)
unresolved: PASS (no published generation)
```

## A query says the index is not ready

**Symptom**

```text
INDEX_NOT_READY: no graph is published yet: index a project with index_project, or run "kivgraph index --full"
```

**Cause**

No graph is published, so nothing can answer. `serve` registers the query tools only when a generation exists, so this is what a query tool returns whenever the snapshot store it was given holds nothing — the state a freshly installed client is in. The code is stable; only the message carries the guidance.

**Fix**

Either of the two the message names. From a shell:

```bash
kivgraph index --full
```

Or, from the client, call `index_project` with the project and `confirmed` set. A configuration written outside the default location is self-contained: its state, its cache and its registry hang from its own directory, so a `serve` started with `--config` pointing elsewhere does not see the graph built against `~/.config/kivgraph/config.yaml`.

## A tool refuses the arguments

Every tool that takes a symbol accepts either a `stable_key` or the triple `repository` + `path` + `qualified_name`, and exactly one of the two. `path` is repository-relative. Two selectors can disagree, so passing both is refused instead of resolved quietly.

### The selector is malformed

**Symptom**

One of these, with code `INVALID_ARGUMENT`:

```text
one of stable_key or qualified_name is required
pass either stable_key or qualified_name, not both
repository and path narrow a qualified_name; a stable_key already names one symbol
path is repository-relative, so it requires repository
stable_key must not carry surrounding whitespace
```

**Cause**

The arguments do not name exactly one symbol.

**Fix**

Build the call out of the answer you already have. Every row the surface returns carries `repository`, `file_path`, `qualified_name` and a line range, so the next call is the triple copied from the previous response. Stable keys never have to enter the conversation.

### The name matches more than one symbol

**Symptom**

With code `AMBIGUOUS_SYMBOL`. With no `repository` or `path` yet, the message names each candidate by where it is, because that is a narrowing you can express in the next call:

```text
qualified name "<name>" names <n> symbols; narrow with repository and path: <repository> <path>:<start>-<end>, ...
```

Once `repository` and `path` were both given and the name still matches more than once, only the key separates them, so the keys are listed instead:

```text
qualified name "<name>" names <n> symbols in <repository> <path>, so only a stable_key separates them: <key>, <key>
```

**Cause**

Kivgraph does not pick one for you. Choosing by name is the coincidence its edges exist to avoid.

**Fix**

Add `repository`, then `path`. When the message offers keys, pass one as `stable_key` and drop the triple.

### The symbol is not found

**Symptom**

A name the narrowing excluded says where it looked and how to widen:

```text
SYMBOL_NOT_FOUND: qualified name "NoSuchThing" was not found under kivgraph; call it without repository and path to search the whole graph
```

A name nobody declares says only that:

```text
SYMBOL_NOT_FOUND: qualified name "<name>" was not found
```

An unknown repository is its own code:

```text
REPOSITORY_NOT_FOUND: repository "<name>" is not in the published graph
```

**Cause**

The two not-found cases need different fixes, so they read differently. The first is a narrowing problem; the second means the graph has no such qualified name.

**Fix**

Drop `repository` and `path` and call again. If it is still absent, search by name instead: `find_symbol` returns an empty result set rather than an error, so `{"name": "ThisSymbolDoesNotExistAnywhere"}` answers `"total": 0` and proves the absence. Use `list_repositories` for the registered names.

## index_project refuses to run

**Symptom**

```text
PERMISSION_REQUIRED: user approval is required; confirm the operation before setting confirmed=true
```

Or, when the client supports elicitation and the user declines:

```text
PERMISSION_DENIED: project indexing was not approved
```

**Cause**

`index_project` registers projects and rebuilds the whole corpus. It is gated on explicit user approval, and it never infers approval from the call.

**Fix**

A client that declares the `elicitation` capability is prompted by the server, naming the projects the approval covers, and needs no argument. A client without it must ask the user itself and then send `confirmed: true`. Pass every project in one call: a rebuild costs the whole corpus.

If the pass itself fails, the code is `INDEXING_FAILED` and the message carries the reason the indexer gave.

## Two rebuilds at once

**Symptom**

```text
another process is publishing into this generation store
```

Reaching a client through `index_project`, this arrives as `INDEXING_FAILED` with that text inside the message.

**Cause**

Publishing a generation takes a `flock` on the store. One state directory is shared by an `index --full`, a client's `index_project` and a server's resynchroniser, so without the lock two passes overwrite each other.

The lock does not wait. A rebuild takes minutes and blocking would look like a hang, so the loser is told instead.

**Fix**

Let the winner finish, then retry. For the follower inside a running `serve` or `ui` this is not a failure and is not reported as one: it is exactly what the lock exists to produce, and the generation already published keeps answering.

## The graph is behind the working tree

**Symptom**

Paths and line ranges name code that has moved. `graph_status` reports it in `repository_freshness`, and `repositories_moved` counts the entries whose working tree left the indexed commit. One entry, with `path` omitted:

```json
{
  "name": "go-svc-e",
  "languages": ["go", "typescript"],
  "indexed_commit": "4cc05cdc2c73cb7111b7b38447639c1444ab8410",
  "indexed_branch": "main",
  "indexed_dirty": true,
  "current_commit": "4cc05cdc2c73cb7111b7b38447639c1444ab8410",
  "current_branch": "main",
  "moved": false
}
```

**Cause**

A snapshot is immutable. It describes the commit it was built from, whatever the tree holds now.

**Fix**

Call `graph_status` first and read the entry for each repository:

| Field | What it says |
| --- | --- |
| `indexed_commit`, `indexed_branch` | The tree the graph was built from. |
| `current_commit`, `current_branch` | The tree on disk now. |
| `indexed_dirty` | The indexed tree carried uncommitted changes, so matching commits do not prove matching files. |
| `moved` | The two disagree. `moved_detail` names both positions. |

A repository whose `HEAD` could not be read is not counted as moved and is not counted as fresh either; its entry says why. A path that is not a checkout, and a graph built before the commit was recorded, are also not `moved` — and only a tree still holding the indexed commit means the results can be trusted. A `derived` entry is a provider Kivgraph built from the machine rather than from the registry; nothing checks it out and nothing can move it.

`serve` and `ui` follow the published generation and republish when the `CURRENT` pointer advances, so a rebuild in another terminal reaches a running server. To force the corpus back onto the current code:

```bash
kivgraph index --full
```

One thing the fact cache cannot see: a lockfile is searched from the registered repository root upwards, and `node_modules` is never walked because that is the lockfile's job. A lockfile that is not found leaves that dependency with no control at all — in a pnpm monorepo it lives above the registered repositories. This is a declared limitation. Where it applies, rebuild with the cache off or set `indexing.fact_cache: verify`, which analyses everything and aborts the pass when an entry disagrees with the analysis.

## After a full clean the server serves nothing new

**Symptom**

`clean` prints the warning:

```text
clean: restart any running serve or ui before the next index --full
```

Ignored, a running server keeps answering from the graph that was removed, and its log says so once:

```text
generation store was rewound to <id> while serving <id>: restart to follow it again
```

**Cause**

After a full `clean` the numbering returns to `000001`, and `SnapshotStore.Publish` only accepts a strictly newer generation. A live server keeps the graph that no longer exists and installs no further one. The follower declares it once, and `clean` warns about it in advance.

**Fix**

Stop the long-running processes, rebuild, start them again:

```bash
kivgraph stop
kivgraph index --full
```

`kivgraph stop --dry-run` lists what it would stop and stops nothing. It matches `serve` and `ui` only: an index in flight is left alone, because killing one throws away minutes of analysis. A client launches `serve` itself, so restarting the server means restarting that client, or reloading its MCP connection.

`clean` never removes registered repositories, only published generations, so rebuilding what is registered is `kivgraph index --full` and nothing has to be registered again. Without `--yes` it lists and changes nothing; with `--keep-active` it keeps exactly the generation currently published, and then `rollback` has nothing to restore.

## Rust is not indexed

Rust is the one language Kivgraph does not analyse itself. `rust-analyzer scip` is the authority, invoked as an external process once per Cargo workspace, and it is a prerequisite like the Node runtime of the TypeScript worker.

### The analyzer is not there, or is the wrong one

**Symptom**

```text
toolchain.rust: FAIL (command "rust-analyzer" is unavailable)
```

Or the binary resolves and fails to run:

```text
toolchain.rust: FAIL (<path to rust-analyzer> --version failed: <error>)
```

Run by hand, that binary prints:

```text
Unknown binary 'rust-analyzer' in official toolchain
```

**Cause**

Being on the `PATH` is not being installed. rustup leaves a proxy named `rust-analyzer` for every toolchain whether or not the component exists, and the proxy fails with the line above.

Resolution is fixed. A `rust.analyzer_command` that contains a path separator is honoured exactly as written. A bare command name resolves first to the binary sitting beside the Kivgraph executable, and only then through the `PATH`: an installation that ships its own engine must use it, or two machines with the same bundle would index the same repository with different analyzers.

**Fix**

```bash
rustup component add rust-analyzer
```

`kivgraph doctor` reports which binary answered and its source — `bundled`, `explicit` or `path` — and `kivgraph version --json` publishes its release.

### cargo is missing

**Symptom**

```text
toolchain.cargo: FAIL (cargo is unavailable, so no workspace can be loaded)
```

**Cause**

The bundle carries `bin/rust-analyzer` but no Rust toolchain. Without `cargo` the analyzer cannot load a Cargo workspace, so the analyzer travelling inside the installation is not enough. `doctor` checks it separately and fails naming it.

**Fix**

Install a Rust toolchain on the machine that indexes.

### derive macros, operators and `?` produce no edges

**Symptom**

`find_references` on a trait reached through `#[derive(...)]`, an overloaded operator or the `?` operator returns nothing, and calls into the standard library are absent from the graph.

**Cause**

`core`, `std` and `alloc` enter the graph with `rust.index_sysroot`, which is off by default. Four silences follow, all measured: `#[derive(...)]` produces no relation, operator overloading does not reach its trait, `?` does not reach `Try::branch`, and every call into the standard library disappears.

**Fix**

Turn the standard library on:

```yaml
rust:
  index_sysroot: true
```

With it, all four become exact edges. It costs an order of magnitude in graph size — one toolchain is around 350.000 monikers and half a minute of indexing — and one cold pass per toolchain, since the fact-cache fingerprint includes `rustc --version` and a toolchain change invalidates every fact taken from it.

This is a declared limitation, never a failure: a machine with no toolchain, or without `rust-src`, indexes its own repositories and says why it did not index the library. `rust.sysroot` is `discover`, `none`, or a path; it says where the standard library is, not whether it enters the graph.

## A Go package or module is missing

### The package is not buildable

**Symptom**

The package contributes nothing, `graph_status` counts it under `unresolved_by_reason`, and a walk that would have crossed it downgrades its verdict. From a real `get_blast_radius` response, with the absolute path abridged:

```json
{"completeness":{"verdict":"LOWER_BOUND","invisible_scopes":[{"reason":"PACKAGE_NOT_BUILDABLE","repository":"kivgraph","requested_package":"github.com/Luqueee/kivgraph/benchmarks/ladybug-recovery","detail":"LIST: build constraints exclude all Go files in /path/to/benchmarks/ladybug-recovery"}]}}
```

**Cause**

Go is loaded with the tags in `go.build_tags`. A directory whose files those tags exclude is not an index failure: it is declared `UNRESOLVED` with reason `PACKAGE_NOT_BUILDABLE` and the pass continues. Every other loader diagnostic still aborts it.

**Fix**

Add the tag the package needs to `go.build_tags` and rebuild. Indexing the Kivgraph repository itself requires the `ladybug` tag.

The other reasons seen alongside it in a real graph are `DECLARATION_NOT_RESOLVED`, `MODULE_PROVIDER_NOT_FOUND` and `PACKAGE_PROVIDER_NOT_FOUND`. A package name nobody provides today is a dependency with an `absent` fingerprint, not the absence of a dependency, so it becomes the edge it should be once a provider appears.

### The module was not loaded

**Symptom**

The module's symbols are absent and its references are declared `MODULE_NOT_LOADED`, with the diagnostics observed.

**Cause**

A module the loader cannot read publishes no facts, because they would not be reliable, and it does not bring down the pass. A repository whose dependencies nobody downloaded does not decide whether the others have a graph.

**Fix**

Download the module's dependencies in its own checkout, then rebuild. Indexing is hermetic by default: a module the local cache does not hold is reported, not fetched. `go.allow_network` is the one declared way out, and a multi-repository workspace resolves one shared build list, so its selection can need a version no member downloaded on its own.

### The module is above the language-version ceiling

**Symptom**

```text
toolchain.typecheck: FAIL (registered Go module requires a newer Go language version than this build supports (this build type-checks with go <ceiling>): repository "<name>" module "<path>" requires go <version>; rebuild Kivgraph with that toolchain or drop "go" from the languages of that repository)
```

A passing run reports both numbers, the ceiling and the highest registered module:

```text
toolchain.typecheck: PASS (go <ceiling> (highest registered module: go <version>))
```

**Cause**

`go/types` travels linked inside the binary, so Kivgraph type checks only up to the language version of the toolchain that compiled it. The `go` on your `PATH` is a different number and is not the one that decides whether a repository can be indexed. A module above the ceiling is rejected by name — repository, module and version — rather than being allowed to escalate the synthetic `go.work` and break the load of every other repository. The ceiling is `major.minor`.

**Fix**

The message names both ways out: rebuild Kivgraph with that toolchain, or drop `go` from the languages of that repository. `kivgraph doctor` is where the ceiling is stated.

## A provider is ambiguous or absent

These three codes say something about the registry, not about your code. A name has to have exactly one owner for a stable key to mean one thing.

| Code | What it means |
| --- | --- |
| `AMBIGUOUS_PACKAGE_PROVIDER` | A TypeScript package name is declared by several manifests. No manifest provides it, and both leave the registry. A Go module with several providers gets the same treatment. |
| `AMBIGUOUS_CRATE_PROVIDER` | A crate name is declared by several registered repositories, or a registered repository declares a crate of the standard library, or two toolchains are present at once. None of them provides it. |
| `CRATE_PROVIDER_NOT_FOUND` | Code the analyzer indexed that no manifest of the repository declares — a crate vendored through `[patch.crates-io]`, for example. Its uses are declared as such rather than published against declarations that were discarded. |

**Fix**

Make the ownership unambiguous: remove the duplicate declaration, or register only one of the repositories that claims the name. A repository name is compared exactly and is an identifier, never a path component, so two names differing only in case are two repositories.

A reference that names `core` with a release is `CRATE_VERSION_MISMATCH`, and a version the analyzer does not know (`.`) identifies no code and never resolves.

## The viewer will not start

**Symptom**

```text
ui: this binary carries no web bundle; build one with scripts/build-bundle.sh (without --mcp-only), or run the viewer from a source checkout with the webassets build tag
```

`kivgraph --help` marks the command in the same build:

```text
  ui [--addr HOST:PORT]  Serve the read-only graph viewer, every interface by default (unavailable: this build carries no web bundle)
```

**Cause**

`kivgraph ui` refuses to start when the binary lacks the `webassets` build tag. Without it, every route would serve the page that says the bundle is missing, so the refusal costs one line instead of a browser tab.

**Fix**

The published release carries the viewer: the release workflow builds without `--mcp-only` and verifies both halves, that `web/index.html` is in the payload and that the help does not mark `ui` unavailable. If your binary refuses, it was built with `--mcp-only` or from a source checkout without the tag. Install the published release, or rebuild the bundle without `--mcp-only`.

The viewer is opt-in and serves only the published `HotSnapshot` over read-only HTTP. Its default bind is `0.0.0.0:7777`, and it logs the address it bound, including the one a port `0` resolves to.

## Where to look next

**`kivgraph doctor`** answers the machine questions in one pass, one line per check: `config`, the `state.*` directories, `repositories`, the `toolchain.*` checks, `graph.store`, `snapshot.digest`, `snapshot` and `unresolved`, then `doctor: PASS` or `doctor: FAIL`. It is where the Go type-checking ceiling is stated and where you find out which `rust-analyzer` answered.

**`graph_status`, through the client**, answers the graph questions: `status`, which is `ready` or `empty`; `snapshot_id`, `snapshot_built_at` and `snapshot_age_ms`; the counts; `edges_by_kind` and `unresolved_by_reason`; and `repository_freshness` with `repositories_moved`. It reports nothing this process did not use or measure: `serve` answers from the published `HotSnapshot`, so it never opens the database and never runs the TypeScript worker, and declares them `not_applicable` saying why. Metrics nobody observed are omitted rather than reported as zero.

**The logs.** A one-shot command reports plain text when its `stderr` is a terminal and a JSON log record when it is not, so a pipeline gets records and a person gets sentences. `serve` and `ui` always log JSON, because a client reads their `stderr`. In both, progress is `INFO` and only a failure is `ERROR`, and the text of the line is the record's `msg`, never a field buried inside a fixed message — a record you cannot filter by level or find by text reports nothing. On `serve`, `stdout` carries protocol framing and nothing else.
