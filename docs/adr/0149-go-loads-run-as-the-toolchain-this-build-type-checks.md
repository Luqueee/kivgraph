# ADR 0149: Go loads run as the toolchain this build type-checks

## Status

Accepted. Changes no schema, stable key or tool. Changes which toolchain a pass
asks the `go` command to run as.

## Context

`go/types` is linked into the binary, so the language it reads is the one of
the toolchain that built it. `go list` runs as whatever `go` is installed, and
the packages it reports include that toolchain's standard library.

With a `go` newer than the build -- `1.27.4` installed, `1.26.11` built -- the
standard library is written in a language this build does not parse:

```text
math/rand/v2/rand.go:213:17: method must have no type parameters
vendor/golang.org/x/net/idna/tables17.0.0.go:5:9: file requires newer Go version
```

The module that imports it does not load. Since ADR 0147 a module that does not
load no longer fails the pass, so the pass exits `0`. On a real installation a
daemon that rebuilds on its own replaced a graph of `31110` symbols with one of
`14398`, with every Go declaration gone, `31` times, and nothing said so.
`kivgraph doctor` passed: it compares the build with the `go` directive of the
registered modules, which was `1.26`.

## Decision

When the installed `go` is newer than the toolchain this binary was built with,
a load sets `GOTOOLCHAIN` to that toolchain. The `go` command finds it in the
module cache and runs as it. A `GOTOOLCHAIN` already in the environment is the
user's and is left alone, and an installed `go` that is older or equal is not
pinned: it reads a standard library this build parses, and forcing a toolchain
it does not have would break a pass that works.

`kivgraph doctor` says so on `toolchain.go`.

## Consequences

Measured without `GOTOOLCHAIN` in the environment, over two repositories:
`1293` Go definitions before, the module of this repository not loaded, and
`29339` after.

- The pass is offline. If the toolchain is not in the module cache the load
  fails and says the toolchain is not available; the module is not loaded, as
  before, for a reason that can be acted on.
- A module whose `go` directive is above the build is refused by the `go`
  command with both versions named. It could not be type-checked before either.
- The fingerprint of the fact cache still asks the installed `go` for
  `GOVERSION`. It does not change between passes, so nothing is invalidated.

Whether a pass run by the daemon should publish a generation that lost a
module the active one had was not decided here. ADR 0152 decides it: it does
not.
