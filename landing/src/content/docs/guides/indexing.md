---
title: Indexing
description: What a pass does, what bounds it, and why two passes over the same corpus produce the same graph.
---

```bash
kivgraph index --full
```

A pass analyses every registered repository, merges the facts, validates the
canonical graph and publishes it as a new generation. Publication is atomic: a
candidate that fails integrity or validation never becomes `CURRENT`, and the
generation already being served is untouched.

## The unit of analysis

Analysis is concurrent, and the unit differs per language:

| Language | Unit | Budget |
| --- | --- | --- |
| Go | Module | `go.maximum_loads` |
| TypeScript | Package | `typescript.maximum_workers` |
| Rust | Cargo workspace | `rust.maximum_workspaces` |
| Python | Repository | `python.maximum_workers` |
| Dart | Repository | `dart.maximum_workers` |
| Java | Repository | `java.maximum_workers` |
| C# | Repository | `csharp.maximum_workers` |

The budgets are separate because the costs are different. A Go load holds a
complete type universe; a TypeScript worker is a process; a `rust-analyzer`
invocation holds a whole Cargo workspace and its sysroot. Each queue is drained
heaviest-first, with no more workers than it has units, and the first failure
cancels the rest.

Python, Dart, Java and C# are scheduled the same way, one unit per registered
repository. Java and C# default to one worker: `scip-java` drives the project's
own build tool and `scip-dotnet` runs `dotnet restore`, and two of either on one
machine compete for the same package cache and heap.

## Where a pass runs

A pass never runs inside a process that answers queries. It holds the type
universe of every Go module, every TypeScript worker and every SCIP index at
once, and a Go heap that has grown to that peak keeps the arena for as long as
the process lives. Measured on a 41-repository, 102,385-symbol corpus, a server
that indexed in its own process parked at `1.68 GB` of resident memory against
a live heap that stayed flat at 173 MB — a memory measurement on that corpus,
not one of the token figures the [benchmark](/comparison/) reports.

So when a server indexes — because a client called
[`index_project`](/docs/tools/index-project/), because `HEAD` moved in a
registered repository, or because a source file changed while `watcher.enabled`
is on, which is the default — it runs `index --full --json` as a child process
and reads the result. The peak dies with the child, and the server pays only for
the snapshot it then loads. A `HEAD` move that leaves every indexed file as the
graph recorded it, such as a plain commit, rebuilds nothing. The last two
triggers belong to a `serve` that answers from its own graph and to the
`daemon`; a `serve` that relays to the daemon holds no graph and watches
nothing. The source watcher reconciles the whole tree every
`watcher.reconciliation_interval`, so a filesystem event the platform dropped
still reaches a rebuild.

That flag is public. With `--json`, `stdout` carries only newline-delimited JSON
events — any number of `progress`, then exactly one `result` — and the report a
person reads is not written at all:

```bash
kivgraph index --full --json
```

```text
{"event":"progress","progress":{"phase":"go","repository":"data-service","completed":3,"total":41}}
{"event":"result","result":{"passed":true,"generation_id":"000054","counts":{"symbols":102385},"index":{"go_definitions":41230}}}
```

The corpus this was captured on is private, so the repository name in that event
is substituted. The phase, the counts and the generation are the captured ones.

A reader ignores an event kind it does not know, so a new one is not a breaking
change. Without the flag, nothing changes: the report goes to `stdout` and
progress to `stderr`.

## What a second server costs

A generation carries its snapshot as a file, and a server maps it read-only
instead of deriving the graph. So the largest part of it — the string arena — is
one copy in physical memory however many servers read the same generation.

Measured on Linux with `benchmarks/shared-snapshot`, one generation of a
51-repository corpus — `161,819` symbols in a `129 MB` file — against the same
binary made to derive the graph instead, `Pss` summed over every server:

| servers | mapping the file | deriving the graph | share |
| --- | --- | --- | --- |
| 2 | `326 MB` | `654 MB` | `50%` |
| 4 | `514 MB` | `1,234 MB` | `42%` |
| 8 | `888 MB` | `2,385 MB` | `37%` |

The share falls as servers are added, which is the whole point: a mapped page is
paid for once by the machine however many processes hold it, so each new server
adds only what it decodes for itself — `614 B` per symbol, flat across all three
counts. At eight servers the machine keeps `1.5 GB` it would otherwise spend.

The other half is startup. A server that maps the file answers its first query
in `261 ms`; one that derives the graph takes `3,394 ms`, and that ratio does
not improve with more servers because each one starts alone.

On Linux, `Pss` and `Shared_Clean` in `/proc/<pid>/smaps_rollup` split shared
from private directly. macOS reports a footprint per process and no such split,
so there the numbers come from `footprint`, which separates dirty from clean and
names the mapped region — a different quantity, not comparable to the table
above. Measured that way with two servers on `123,531` symbols: `94 MB` of clean
mapped file in one shared copy, `44.5 MB` dirty per process.

If the file is absent, foreign, stale or corrupt, the server derives the graph
from the canonical store exactly as it always did, says so, and answers. It is
an economy, never a precondition. `kivgraph doctor` tells the two apart on the
`snapshot.published` line.

## Determinism

The merge follows the order of the units, never the order in which they
finished. Two passes over the same corpus produce byte-identical facts, whatever
the scheduler did.

## The fact cache

`indexing.fact_cache` decides whether an analysis unit may be served from what
a previous pass stored for it:

- `off` — analyse everything.
- `on` — serve an entry whose recorded inputs all still match.
- `verify` — analyse everything and fail the pass when a servable entry
  disagrees with the analysis.

An entry records the whole list of what the unit read and the fingerprint of
each item; serving it revalidates that list in full. An entry is never served
to a different analyzer. The analyzer fingerprint is the hash of nine parts —
`resolver`, `binary`, `go`, `typescript`, `python`, `dart`, `java`, `csharp` and
`rust` — and covers:

- the content of the Kivgraph executable and the resolver version;
- the answer of `go env`, the build tags, `include_tests`, `go.goos`,
  `go.goarch`, `go.cgo_enabled` and `go.allow_network`;
- the TypeScript worker. When the worker is a script in a directory named
  `dist`, that is the entry script plus every JavaScript module below the
  directory, and the `typescript` package it imports with the `@typescript`
  platform packages beside it, by relative path and content. A worker started
  through a shim is identified by the shim and the executable, which a bundle
  replaces together;
- the Dart SDK. A `dart` command is identified by its resolved path and what
  `dart --version` prints, so a different SDK on the `PATH` is a different
  analyzer;
- the Rust toolchain: the `rustc`, `cargo` and sysroot probes;
- the configuration of the Python, Dart, Java and C# loaders.

The toolchain probes run from the temporary directory, so they describe the
machine's default toolchain and not the pin of whichever directory the process
started in. A repository's own pin is a change of `go.mod`, `go.work` or
`rust-toolchain`, which the tree of its unit records.

When a pass refuses entries because the analyzer changed, `index --full` names
the part that moved on the `index.cache` line, for example
`analyzer_changed=rust:34`. The number is how many refused entries differ in
that part, and an entry that differs in several parts counts in each. An entry
written before the parts were recorded counts as `unrecorded`. The parts are a
diagnosis: an entry is served or refused by the fingerprint as a whole.

A module the loader could not read is never cached, because its failure depends
on the module cache and no fingerprint of the code describes it.

An entry is a binary file ending in `.entry`: a header with a fingerprint of the
structure of what it stores, the unit in the clear, a table of its distinct
strings and then the facts. Reading is strict, so a damaged file or one written
by a build whose facts have another shape is a miss and the unit is analysed
again, never decoded into the wrong fields. Versions before `0.10.3` stored JSON
(`.json`); those files are never served and the next pass removes them. On a
registry of sixty repositories the entries went from `1,516 MB` to `367 MB` and a
warm pass spent about `7 s` less CPU.

Comparing two cached passes proves nothing. When touching this area, run with
`fact_cache: verify`.

### What the cache keeps

The address of an entry holds the content of its unit, so every edit writes a
new entry. After a pass admits entries, the cache keeps the newest three of each
unit and at most six across all the resolutions of that unit, and removes the
rest. Newest is the last time an entry was written or served. The entries the
pass has just admitted are never removed by it, and an entry that has not been
used for thirty days expires. The three and the six are constants, not
configuration keys. The price is that going back to content older than the
entries kept is a miss.

### What the fingerprint does not look at

These are what the cache leaves unwatched on purpose, not failures:

- A worker script outside a directory named `dist`, or a launcher that hides
  its `dist`, is identified only by its own bytes. A `.cjs`, `.cts`, `.mts` or
  `.ts` entry, with or without a loader such as `tsx`, contributes neither its
  bytes, nor a `dist`, nor the engine, and the content of a `--require` or
  `--import` argument is not hashed. Only `.js` and `.mjs` scripts are
  followed. In a bundle none of this matters: the binary changes with the
  worker.
- Without `ts-worker/dist/facts-cli.js` and without a worker on the `PATH`, the
  pass runs `pnpm --dir ts-worker facts`, whose fingerprint is the `pnpm` binary
  alone. `pnpm` builds `dist` during the pass, so the first pass fails once with
  `analyzer configuration changed` and the second works. It never serves old
  facts, but the message does not point at the cause: run
  `pnpm --dir ts-worker build` first.
- An update of the toolchain a repository selects without touching its files,
  such as `rustup update` on a pinned `stable` channel, when the machine's
  default toolchain is another one.
- A live daemon whose binary is deleted fails five times while observing and
  reindexing and gives up (`gave up rebuilding a stale profile`) until it is
  restarted. A replaced binary converges on its own.

## Verifying the sources

A pass proves its sources did not change while it ran: it observes the tree
before analysis and again before `CURRENT` moves, and a changed, missing or
unreadable input rejects the candidate. By default that second observation
hashes every analysed file again. ADR 0121 measured it at `10`-`11 %` of a
`12`-`14 s` pass on six repositories.

`indexing.source_verification: signature` is the opt-in alternative. It first
compares the stat of every directory it read and every analysed file with the
stat taken at the first observation, and reads a tree again only when it cannot
prove it would read the same. The default stays `full`, because the proof rests
on the filesystem's timestamps: it was measured on a local APFS volume only,
and a filesystem with coarse timestamps or stale attribute caches (FAT, NFS,
SMB) gives it no guarantee. Neither mode changes what a generation records.
`index --full` prints `index.source_verification` with how many trees it did
not read. See [`indexing`](/docs/configuration/#indexing) for the key.

## Hermetic by default

A pass never writes inside the code it indexes, and it does not reach the
network. There are exactly two declared escapes:

- `go.allow_network` — lets the go command reach a module proxy while loading.
  A multi-repository workspace resolves one shared build list, so its selection
  can need a version no member downloaded on its own.
- `rust.allow_network` — lets cargo reach a registry while the analyzer loads a
  workspace.

Without them, a module or crate the local cache does not hold is *reported*,
not fetched.

## Build tags

The tags Go is loaded with come from `go.build_tags`. A directory whose files
that configuration excludes is not an index failure: it is declared
`UNRESOLVED` with reason `PACKAGE_NOT_BUILDABLE` and the pass continues. Any
other loader diagnostic still aborts it.

Indexing the Kivgraph repository itself requires the `ladybug` tag.

## Failures that do not stop the pass

A Go module the loader cannot read publishes no facts — they would not be
trustworthy — and is declared `MODULE_NOT_LOADED` with the diagnostics
observed. One repository whose dependencies nobody downloaded does not decide
whether the others have a graph.

The same holds for a repository an analyzer ran on and could not read: a C#
repository with no solution, a Java build that fails, a Python or Dart producer
that exits. It publishes no facts and is declared `REPOSITORY_NOT_LOADED`, the
report names it on an `index.<language>.not_read` line with the reason, and
every answer about that repository says `LOWER_BOUND`. A Rust workspace the
analyzer cannot load is declared `WORKSPACE_NOT_LOADED` the same way. So is a
TypeScript repository whose projects cannot be discovered -- a `tsconfig.json`
that does not parse -- and a package the worker fails on, as
`PACKAGE_NOT_LOADED`. A repository registered as TypeScript with no package in
it contributes nothing and is named on an `index.typescript.no_package` line. Fixing the
repository and indexing again restores it: a failure to read is never cached.

## What a pass does not read, and says so

Some source is in the tree and outside what an analyzer reads. A use in it
is in no edge, so the pass records that it did not look. Each of these is an
unresolved row on the file, naming the declaration it writes, and makes an
answer about that declaration `LOWER_BOUND` with the file listed. Other
answers are not affected.

| Reason | What was not read |
| --- | --- |
| `COMPONENT_FILE_NOT_READ` | An `.astro`, `.vue` or `.svelte` file that writes the name of a declaration its package exports. The TypeScript engine does not read a template. |
| `TARGET_NOT_BUILDABLE` | A Rust file of an indexed crate that the build configuration left out, such as a module behind a `cfg` that is false on this machine. |
| `DEFINITION_NOT_INDEXED` | A Rust declaration the grammar sees and the analyzer did not index, or a use of one. |
| `DART_TARGET_NOT_INDEXED` | A Dart use of a library nobody registered. It is recorded once for each declaration that uses the library, named as an import names it: `dart:core`, `package:collection`. |

These are read as words, not as code: a name in a comment or in markup
counts, which errs towards saying an answer is bounded.

Three more things are named on the report of the pass:

- `index.typescript.inferred` -- a package with a `package.json` and no
  `tsconfig.json`. Its TypeScript is read anyway, through a project written
  for the pass outside the repository, under compiler options Kivgraph chose.
  A package that needs its own options declares a project.
- `index.typescript.unclaimed_not_read` -- a TypeScript file in no package at
  all. A file is read as part of a package, so nothing reads it.
- `index.repository.not_read` -- a registry entry that could not be opened.

## Packages of one repository

An import of a package of the same repository is an edge to the declaration
it reaches. In a workspace whose packages publish their source --
`"main": "src/index.ts"` behind a `workspace:*` link -- the compiler resolves
the import to that source, and the edge is `IMPORTS_SYMBOL`,
`EXACT_TYPECHECKED`, whether or not the imported package declares a project.
A dependency installed under `node_modules` is still a package nobody
provides.

## Top-level code

A use that no declaration contains belongs to the scope of its file: a
symbol of kind `module`, created only where a use needs it. That is where a
call in a test callback, a top-level statement, or the body of a Rust macro
invoked at the top of a file -- a fuzz target -- is recorded. It applies to
Go, TypeScript and Rust.

## Checkouts and ignored paths

Every walk skips three kinds of directory, and
[`kivgraph doctor`](/guides/maintenance/#inspect) lists each:

- one that carries a `CACHEDIR.TAG` (`walk.cache_tag`);
- another Git checkout below the repository, a clone or a linked worktree
  (`walk.checkout`). Nothing of it is read with the parent, so it can be
  registered beside it as a repository of its own. A submodule is read with
  its parent;
- a path Git ignores that holds source of a registered language
  (`walk.ignored`): the types a framework generates, a build that copies
  `src`. `include_git_ignored: true` on the registry entry keeps it.

## Java builds

With no build at the repository root, the outermost build of each branch is
run and their indexes are merged, with paths relative to the repository. One
build that fails still fails the repository.

A package name declared by several manifests is an ambiguity, not a broken
repository: nobody provides it, every manifest leaves the registry, and it is
declared `AMBIGUOUS_PACKAGE_PROVIDER`. Go modules and Rust crates get the same
treatment.

## Indexing from an MCP client

`start_index_project` registers one or more projects and starts one rebuild.
Pass every project in a single call: a rebuild resolves cross-repository edges
over the complete set of facts, so it costs the whole corpus whatever is added.
Poll `get_index_status` until it reports `completed` or `failed`. On failure,
surface the returned `INDEXING_FAILED` message. Retry only when its cause is
explicitly temporary, such as contention with another rebuild after that
rebuild releases the store lock. The synchronous `index_project` remains
available for compatible clients, but it holds the call open for the whole
pass.
