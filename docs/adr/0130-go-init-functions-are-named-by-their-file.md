# ADR 0130: Go `init` functions are named by their file

## Status

Accepted. Changes the identity of every Go `init` function; see Consequences.

## Context

Go allows any number of `init` functions in a package, even in one file. They
are the one kind of function a name cannot tell apart: they have no receiver,
are not in the package scope so they have no object path, and share one
signature. Every `init` of a package therefore got the same stable key, and the
generation failed to publish on the `DEFINES` multiplicity check:

```text
symbol "init" is defined by two files, "caddyconfig/httpcaddyfile/builtins.go"
and "caddyconfig/httpcaddyfile/httptype.go", so two declarations share one
identity
```

The defect was independent of `go.include_tests`. It was found while measuring
that setting on public repositories: `caddy` and `prometheus` could not be
indexed at all, and `gin` could not be indexed with tests read. The benchmark
corpus (`cobra`) has no package with two `init` functions, so nothing had
exercised it.

## Decision

The qualified name of an `init` function without a receiver is
`init#<file base name>`, with `#<n>` appended, counting from one, when the file
holds more than one. A method called `init` is not affected.

The name depends only on the file, never on the rest of the package, so the
build variants of a package -- with and without its tests -- name the same
declaration the same way, and adding an `init` to another file moves no
identity. A use is sourced at the qualified name of its enclosing declaration,
so definitions and uses share one helper, `declarationQualifiedName`.

## Consequences

- Every Go `init` function gets a new stable key, including a package that had
  a single one. Nothing can reference an `init`, so no edge into one is lost,
  but a client that pinned one of those keys will not find it. The next
  `kivgraph index --full` rebuilds the graph, which is the only indexing path.
- Measured on public repositories, index succeeds where it failed:

  | Repository | Go files | Without tests | With `go.include_tests` |
  |---|---|---|---|
  | `gin` | 99 | ok (was ok) | ok (was a failure) |
  | `caddy` | 429 | ok (was a failure) | ok (was a failure) |
  | `prometheus` | 768 | ok (was a failure) | ok (was a failure) |

- Not fixed here: `etcd` still fails, because the inventory refuses a symlink
  (`client/v3/concurrency/example_election_test.go`) and aborts the pass.
