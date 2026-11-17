# ADR 0123: Go discovery follows the go tool's package rules

Status: accepted, 2026-11-16. Refines ADR 0122's rule that a walk may skip only
what no provider reads.

Changes the configuration: no.

Changes the schema: no. It changes which Go packages a repository contributes to
the graph, so a repository with Go code under `testdata` will have fewer symbols.

## Context

`workspace.DiscoverGo` walks a registered repository, finds `go.mod` and `go.work`
files, and groups every `.go` file by directory into packages. Each package
becomes a pattern for `go list`, and a directory it cannot read as a package
stops the whole index: `build synthetic Go workspace: … parse Go package file`.

`go help packages` says what the tool itself treats as a package:

> File system patterns expanded with the "..." wildcard exclude … directories
> named "vendor", directories named "testdata", files and directories with names
> beginning with "_" or ".", directories that contain a go.mod file …

Discovery already skips `vendor`. It did not skip the others, and a `testdata`
directory is where a Go repository keeps files that are not valid Go on purpose.
Reproduced with the real binary, one registered repository each, against a
control that indexes:

|tree|`index --full`|
|---|---|
|`testdata/bad.go` holding text|exit `1`|
|`testdata/multi/` with two package names|exit `1`|
|`testdata/ok/` with a valid package|passes; its symbol is in the graph|
|`_scratch/` and `.hidden/` with valid packages|pass; both are in the graph|

A real repository shows it is not hypothetical: `golang.org/x/tools@v0.41.0` has
`1.254` Go files, `414` of them under `testdata`, and stops at the first
unparsable file there.

## Decision

A directory is not a package of the module it sits in when any path component
**below that module's root** is `testdata` or begins with `_` or `.`. Its files
are not parsed and cannot fail discovery. For a tree with no module the
repository root is the root.

- **The root is never judged.** A checkout under `/work/testdata/repo` is indexed.
- **A module inside such a directory is still a module.** Its own root is where
  the rule starts again, so its packages are reported and a `testdata` below
  *its* root is skipped. A go.mod that `replace`s a dependency with a fixture in
  `testdata` therefore keeps resolving.
- **Nothing else changes.** `go.mod` and `go.work` files below those directories
  are still found and parsed, and the digest and the inventory walks still hash
  `testdata` (ADR 0122: they may skip only what every provider ignores, and the
  TypeScript providers do read it).

## Alternatives considered

- **Skip the directory while walking, as `vendor` is.** Four lines instead of
  twenty, and every case above passes. It also discards a module that lives in
  `testdata` altogether, and that is not free: with
  `replace example.com/fixture => ./testdata/mod` the fixture stops being
  discovered, the call into it becomes `unresolved=1` and the definitions fall
  from `2` to `1`. Reproduced with the real binary; rejected.
- **Do nothing and tell users to add `testdata` to `exclusions`.** It works for a
  person who knows, and an index that stops for a fixture is the failure it
  leaves everyone else.
- **Index `testdata` packages but tolerate the ones that do not parse.** Keeps
  fixture symbols in a graph that answers questions about the product's code, and
  needs a second mechanism for "tolerate". The go tool already decided these are
  not packages.

## Consequences

- A repository whose `testdata` holds valid Go code in the module's own tree
  contributes fewer symbols. Anything that referred to a symbol there is now an
  unresolved reference, which is what the go tool would say.
- Kivgraph's own corpus is unchanged: its `testdata` Go code lives in nested
  modules with their own `go.mod`, which the rule leaves alone (`14` modules,
  `12.482` definitions and `116` packages before and after).
- A malformed `go.mod` inside `testdata` still fails discovery. Whether it should
  be read only when something refers to it is a separate decision.

## Verification

- `internal/workspace/go_discovery_test.go`: a tree with text, two-package, valid
  and nested `testdata`, and `_`- and `.`-prefixed directories reports only the
  real packages and does not fail; the same files under `fixtures`, `testdata2`,
  `my_testdata`, `a_b` and `test.data` are packages and a file that does not parse
  there is still an error; names above the module root do not hide it; a module
  inside `testdata` keeps its packages, and a `testdata` below its root is
  skipped; a tree with no module is judged from the repository root. Turning the
  rule off makes the first and fourth fail.
- The real binary: all of the cases above and the `replace` case index with the
  graph of the control. `go test ./...` passes.

Not established: other languages (TypeScript discovery has no equivalent
convention).

## Amendment: a directory that is not one package does not stop the index

Skipping `testdata` left the other way a directory can stop the index. A
directory whose files name several packages, or include one that does not parse,
made `parseGoPackage` return an error and the whole pass failed with it. The
`x/tools` checkout showed it once `testdata` was out of the way: `go/analysis/
passes/copylock` holds `copylock.go` and a `main.go` marked `//go:build ignore`,
the generator idiom, and stopped the index at `package main`. In the Go module
cache `10` of `60` modules have such a directory, `8` of them with an `ignore`
file.

Decision, added to the one above:

- **Discovery never fails on a directory.** `parseGoPackage` names the package
  and lists its files, and the go command, which `go list -e` already runs on
  the package pattern, is what says whether it is valid.
- **A file with `//go:build ignore` or `// +build ignore` before the package
  clause is not part of the package** and does not name it, even when it sorts
  first. If every file is ignored, the directory is reported with what it has.
  Any other constraint is left to go, which evaluates it with the build's tags.
- **When several names remain, the one with most files is reported**, ties to the
  first by name; a file that does not parse stays in the list so that the error
  go gives can be attributed to it.
- **The module is declared not loaded.** For a directory go does reject, the
  module that holds it is `MODULE_NOT_LOADED` with go's own reason (`found
  packages a (a.go) and b (b.go) in …`, `expected 'package'`), as every module
  go cannot load already is, and every other repository in the pass is indexed.

Reproduced with the real binary, one repository each: before, `index --full`
exits `1` in all three; after, `ignore` beside a package indexes both
definitions of the control, and two package names or a file that is not Go
exit `0` with `not_loaded=1` and the reason on the `index.go.not_read` line.

Limit: a module go rejects loses all its packages, not only the broken one,
because the loader treats a list error as a module that did not load. Leaving
the broken directory out of the patterns and declaring only it would keep the
rest; it needs a new field from discovery to the unresolved facts and is not
done. `x/tools` itself now gets past discovery and stops at a dependency its
offline module cache does not hold (`module lookup disabled by GOPROXY=off`),
which is environmental.
