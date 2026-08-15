---
title: Indexing
description: What a pass does, what bounds it, and why two passes over the same corpus produce the same graph.
---

```bash
ladygraph index --full
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

The budgets are separate because the costs are different. A Go load holds a
complete type universe; a TypeScript worker is a process; a `rust-analyzer`
invocation holds a whole Cargo workspace and its sysroot. Each queue is drained
heaviest-first, with no more workers than it has units, and the first failure
cancels the rest.

## Where a pass runs

A pass never runs inside a process that answers queries. It holds the type
universe of every Go module, every TypeScript worker and every SCIP index at
once, and a Go heap that has grown to that peak keeps the arena for as long as
the process lives. Measured on a 41-repository corpus, a server that indexed in
its own process parked at `1.68 GB` of resident memory while holding a `173 MB`
snapshot.

So when a server indexes — because a client called
[`index_project`](/reference/tools/index-project/), or because `HEAD` moved in a
registered repository — it runs `index --full --json` as a child process and
reads the result. The peak dies with the child, and the server pays only for the
snapshot it then loads.

That flag is public. With `--json`, `stdout` carries only newline-delimited JSON
events — any number of `progress`, then exactly one `result` — and the report a
person reads is not written at all:

```bash
ladygraph index --full --json
```

```text
{"event":"progress","progress":{"phase":"go","repository":"api-db-go","completed":3,"total":41}}
{"event":"result","result":{"passed":true,"generation_id":"000054","counts":{"symbols":102385},"index":{"go_definitions":41230}}}
```

A reader ignores an event kind it does not know, so a new one is not a breaking
change. Without the flag, nothing changes: the report goes to `stdout` and
progress to `stderr`.

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
to a different analyzer: its identity includes the content of the executable,
the answer of `go env`, the content of the TypeScript worker, the build tags,
`include_tests` and `go.allow_network`.

A module the loader could not read is never cached, because its failure depends
on the module cache and no fingerprint of the code describes it.

Comparing two cached passes proves nothing. When touching this area, run with
`fact_cache: verify`.

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

Indexing the Ladygraph repository itself requires the `ladybug` tag.

## Failures that do not stop the pass

A Go module the loader cannot read publishes no facts — they would not be
trustworthy — and is declared `MODULE_NOT_LOADED` with the diagnostics
observed. One repository whose dependencies nobody downloaded does not decide
whether the others have a graph.

A package name declared by several manifests is an ambiguity, not a broken
repository: nobody provides it, every manifest leaves the registry, and it is
declared `AMBIGUOUS_PACKAGE_PROVIDER`. Go modules and Rust crates get the same
treatment.

## Indexing from an MCP client

`index_project` registers one or more projects and rebuilds once. Pass every
project in a single call: a rebuild resolves cross-repository edges over the
complete set of facts, so it costs the whole corpus whatever is added. Calling
it once per project pays that cost once per project and keeps only the last
graph.
