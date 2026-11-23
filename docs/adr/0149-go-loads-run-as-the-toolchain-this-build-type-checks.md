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

## Amendment: a toolchain saved with `go env -w`

The variable was read from the environment only, so a `GOTOOLCHAIN` saved
with `go env -w` was overridden by the pin. The file `go env -w` writes --
`GOENV`, or `go/env` in the user's configuration directory -- is read, and a
`GOTOOLCHAIN` there is left alone.

`go env GOTOOLCHAIN` is not asked. It answers with the default a distribution
ships in `GOROOT/go.env` as readily as with the user's choice, and honouring
a distribution's `local` would leave every load on a toolchain this build
cannot parse.

## Amendment: `golang.org/x/tools` stays at 0.52

`go/packages` 0.53 type-checks a package that belongs to no module -- the
standard library -- with the Go version of the `go` command, which it reads
from `go list -f {{context.ReleaseTags}} unsafe` run with `GO111MODULE=off`. In
that mode `go` does not select a toolchain, so `GOTOOLCHAIN`, the pin this ADR
sets, is ignored for that one call: on a machine whose `go` is `1.27` and a
binary built with `1.26`, every standard package read as written for `1.27`, and
`go/types` refused it as too new. No Go module of three real repositories was
loaded (`package requires newer Go version go1.27 (application built with
go1.26)`), where `0.52` loaded them. The release before it used that version
only for an external driver.

It was found by indexing those repositories after the bump had been merged on
the strength of CI, which runs with `GOTOOLCHAIN=local` and a `go` as old as the
binary and so cannot see it. `x/tools` stays at `0.52.0` and Dependabot is told
to leave it. Moving on needs the probe to run as the pinned toolchain, for
example by putting that toolchain's `bin` first on the `PATH` of the process
before a load, which is a change of its own.
