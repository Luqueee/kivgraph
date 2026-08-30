---
title: Limits
description: What Kivgraph does not do, measured and stated without softening.
---

A declared hole is a fact. Everything on this page is a known limitation, not a
bug waiting to be discovered.

## `grep` is cheaper on five of the 29 benchmark questions

On the current run — 29 questions, 37 repositories, Kivgraph `0.5.0`, tokenizer
`o200k_base` — plain text search costs fewer tokens than the graph on five
questions, and both arms answer all five at recall 1.00:

| Question | `grep` cost, relative to Kivgraph |
| --- | --- |
| `A1_go_absent` | 0.26x |
| `A2_ts_absent` | 0.38x |
| `I1_go_depth2` | 0.38x |
| `A3_rs_absent` | 0.47x |
| `T1_go_trivial` | 0.53x |

They share a shape: a rare name, one repository, few files to open. A graph
query has a fixed price that a two-hit `grep` does not, and no amount of
indexing removes it. See the [comparison](/comparison/) for the whole run.

The same run records one recall miss of our own: `R3_ts_intra`, where the
answer omitted a single TypeScript test file (recall 0.889). Every other
question in the set is exact.

## Python resolves exactly only with a configured analyzer

The five languages are not resolved to the same standard. Go, TypeScript and
Rust edges are type-checked; Dart edges are resolved by Dart Analysis Server;
Python uses exact semantic facts when a configured analyzer provides them and
`CANDIDATE` facts in its bundled AST fallback.

A `CANDIDATE` Python edge is not a proven call. Treating an empty Python answer
as proof of absence is only safe when the response reports the confidence and
completeness to back it.

## Nothing answers before a generation is published

`serve` with no published graph registers `index_project` and nothing else.
That is deliberate — publishing ten tools that all answer `INDEX_NOT_READY`
teaches an agent that the tools do not work — but it does mean a fresh install
cannot answer a single question until `kivgraph index --full` has completed.

There is no incremental path: a rebuild is a full pass over every registered
repository.

## Rust does not index `core`, `std` or `alloc`

That single absence explains four measured silences:

- `#[derive(...)]` produces no relation.
- Operator overloading does not reach its local `impl`: `a + b` is attributed
  to `core::ops::Add::add`.
- The `?` operator lands in `Try::branch`.
- Every call into the standard library disappears.

Fabricating those edges against a target nobody publishes is forbidden by the
graph contract, and emitting an `UNRESOLVED` for every `derive` would be worse
than the current silence. Indexing the sysroot changes the size and the
versioning of the graph and is tracked as its own task.

Related Rust limits: a symbol behind an inactive Cargo feature is absent from
the graph and reported as unresolved; a crate version the analyzer does not
know (`.`) identifies no code and never resolves; local symbols (`local N`) are
a per-document counter, are not addressable, and never enter the graph.

## Go type-checking has a version ceiling

`go/types` travels linked into the binary, so Kivgraph type-checks only up to
the language version of the toolchain that compiled it. A registered module
above that ceiling is rejected by name — repository, module and version — rather
than being allowed to escalate the synthetic `go.work` toolchain and break the
load of every other repository inside the standard library.

`kivgraph doctor` reports that ceiling. It is not the `go` on your `PATH`, and
it is the number that decides whether a repository can be indexed.

## Published platforms

Linux `amd64`, macOS `arm64` and Windows `amd64`. `darwin/amd64` and
`windows/arm64` are out of scope because no pinned native library is published
for them; the installers say so when they refuse.

A bundle is always built on a host of its own platform: cgo links the native
library and there is no cross-compilation.

## macOS artifacts are not notarized

The project uses no Developer ID. The binary carries an ad-hoc signature, which
is what Apple Silicon requires in order to execute. Gatekeeper only blocks a
file carrying `com.apple.quarantine`, which neither `curl` nor `tar` writes.

## A full disk is a recorded `FAIL`

The LadybugDB recovery suite passes the crash, reopen, truncation and
permissions scenarios. It retains an explicit `FAIL` for a full disk:
`Writer.Apply` returned success and the first intercepted `ENOSPC` appeared
during shutdown, leaving the copy unable to reopen. The recovery command
returns a nonzero status while this limitation exists.

Immutable generations and durable `CURRENT` publication protect the *active*
database against `ENOSPC`; the qualification is recorded as
`ACCEPT_LADYBUGDB_WITH_LIMITS`.

## The viewer is unauthenticated

`kivgraph ui` binds `0.0.0.0:7777` by default and carries no authentication.
Its responses contain repository and file paths, symbol names and signatures.
The bind is a deliberate default — the graph is built where the repositories
are and the viewer is usually opened from another machine — but it is not a
safe one on a shared network. Restrict it with `--addr 127.0.0.1:7777` or
`web.address`. See the [viewer guide](/guides/viewer/).
