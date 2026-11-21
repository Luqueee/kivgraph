# ADR 0138: Go loads read no bodies of dependencies

## Status

Accepted. No change to any fact, identity or wire format.

## Context

A Go load type-checks every dependency from source, and keeps its syntax and
its type information for as long as the roots live (`NeedDeps` with
`NeedSyntax` and `NeedTypesInfo`). On `prometheus` a heap profile at the peak
was `go/parser` trees and `go/types` tables, and the live heap was `3.9 GB`.
Capping the engine's buffer pool, `GOGC` and `GOMEMLIMIT` did not lower it
(roadmap, indexing memory); dropping `NeedDeps` did, at `5x` to `11x` the time.

Nothing reads a dependency's function bodies. What a root needs from a
dependency is the types of its declarations, and no body changes them.

## Decision

`goloader.Load` gives `go/packages` a parser that drops the body of every
function of a file outside the load directory (`dependency_bodies.go`):

- The checker asks for a body in two cases, so those are kept: a generic
  function or a method of a generic type, and `init`.
- The files cgo writes into the build cache are kept whole. They are named by
  hash with no `.go` extension, and a root's own files are among them.
- Object resolution is skipped for the files that lost their bodies; nothing
  reads `ast.Object` of a dependency. Roots are parsed exactly as before.
- The checker reports an import that only the bodies used. Those errors are
  removed from the dependencies, and only those: an `imported and not used`
  error in a file that lost its bodies. When they were all of a package's
  errors, the toolchain-skew advisory go/packages attached beside them goes too.
  Any other error of a dependency stays and keeps blocking.
- If a root turns out to have lost a body (a root outside the load directory),
  the load is redone whole, and that is what is returned.

## Consequences

Measured with `GODEBUG=gctrace=1` and `/usr/bin/time -l`, before and after, same
machine; two runs on `prometheus` (with tests) and one on `caddy`:

- `prometheus`: live heap `3.9 GB` to `1.7`/`1.8 GB`; peak heap `6.0`/`6.5 GB`
  to `3.1 GB`; peak RSS `7.7`/`7.0 GB` to `4.7`/`4.9 GB`; time `15.9`/`16.4 s`
  to `14.9`/`15.7 s`.
- `caddy`: live heap `1.35 GB` to `0.71 GB`; peak heap `2.0 GB` to `1.2 GB`;
  peak RSS `2.85 GB` to `1.88 GB`; time `5.7 s` to `4.8 s`.

The Go heap halves. The resident peak falls by about a third. What is left above
the Go heap was attributed afterwards (ADR 0139).

The graph is the same. Symbols, edges, evidence, package edges, unresolved by
reason and files are equal on `cobra`, `gin`, `caddy` and `prometheus`; and the
`find_references` files and completeness verdicts of `280` sampled declarations
(`cobra` `100`, `gin` and `caddy` `40` each, both with and without tests) are
identical.

- An unused import that is a real error of a dependency is hidden too. A
  dependency that does not compile cannot be built either, and the roots' own
  files are never touched.
- A bug in the dependency's function bodies no longer shows as an error of the
  dependency. It never reached the graph: only the roots' bodies are read.
- Tests: `dependency_bodies_test.go`.
