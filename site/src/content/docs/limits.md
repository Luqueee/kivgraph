---
title: Limits
description: What Ladygraph does not do, measured and stated without softening.
---

A declared hole is a fact. Everything on this page is a known limitation, not a
bug waiting to be discovered.

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

`go/types` travels linked into the binary, so Ladygraph type-checks only up to
the language version of the toolchain that compiled it. A registered module
above that ceiling is rejected by name — repository, module and version — rather
than being allowed to escalate the synthetic `go.work` toolchain and break the
load of every other repository inside the standard library.

`ladygraph doctor` reports that ceiling. It is not the `go` on your `PATH`, and
it is the number that decides whether a repository can be indexed.

## Published platforms

Linux `amd64` and macOS `arm64`, and only those. `darwin/amd64` is out of scope
by decision, not by cost, and the installer says so when it refuses.

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

`ladygraph ui` binds every interface by default and carries no authentication.
Its responses contain repository and file paths, symbol names and signatures.
Restricting it is `--addr` or `web.address`. See the
[viewer guide](/guides/viewer/).
