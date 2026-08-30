# ADR 0088: Bazel as an optional Go build proof of concept

- **Status:** proposed
- **Date:** 2026-08-30
- **Changes the MCP protocol:** no
- **Changes the persistent schema:** no
- **Requires a rebuild:** no
- **Changes the CLI surface:** no

## Context

Kivgraph has one Go module, three independent pnpm projects, native LadybugDB
linking, pinned Tree-sitter C grammars, a bundled Rust analyzer, and
platform-native release bundles. The current Make targets and release workflow
encode those boundaries deliberately. Replacing them all with one build graph
would make the build tool the new owner of platform and packaging contracts
before its value has been measured.

Bazel is still a useful candidate for the Go Module and its ordinary cgo
grammar dependencies. Its Module, Interface, and Implementation boundaries can
provide a smaller build graph, sandboxed actions, and a cacheable seam for
incremental Go builds. The POC must not make the analyzer build inside an
indexed repository or turn generated distribution files into source inputs.

## Decision

Add an optional, Go-only Bazel graph using Bzlmod, `rules_go`, and Gazelle.
`MODULE.bazel` takes dependency versions from `go.mod`; generated `BUILD.bazel`
files describe the production Go packages and tests. The Tree-sitter grammar
bindings are adapted at the external-repository seam with reproducible patches
so their generated C sources and headers are visible to Bazel's sandbox.

The supported proof-of-concept commands are:

```bash
make bazel-build
make bazel-poc-test
```

The first builds `//cmd/kivgraph:kivgraph`. The second runs Gazelle validation,
the Tree-sitter syntax test, selected fixture-backed loader tests, release and
version metadata tests, and the command smoke tests. The normal `test` target,
Ladybug targets, bundle scripts, and the three pnpm projects remain the source
of truth for their existing surfaces.

The POC excludes `landing/`, `web/`, `ts-worker/`, `python-worker/`, release
bundle generation, Rust analyzer acquisition, and the native LadybugDB tag.
It builds Kivgraph itself only; it never builds a user's indexed repository.

## Consequences

The Go implementation now has a real Bazel Module and a cacheable build graph,
while Make and GitHub Actions keep their current behavior. Bazel's sandbox also
exposed which tests depend on a checkout layout or on host tools: the full Go
suite is not claimed as a Bazel suite yet because several tests walk relative
`testdata` and source paths, follow runfile symlinks, or invoke `go` and workers
from the host `PATH`. Making those tests runfile-aware is a separate refactor,
not a reason to weaken their existing contracts.

The initial measurements were completed locally on macOS arm64 with Bazel
`9.2.0` and the Go toolchain required by `go.mod`, using the Kivgraph checkout as
the corpus. A clean `make bazel-poc-test` run took `65.03 s` and performed `395`
actions while setting up the Bazel SDK and resolving the external dependency
graph; setup and dependency download are included in that wall-clock result but
were not separately instrumented. A warm no-op run of the final 12-target smoke
suite took `0.59 s`. After an isolated Go source edit, `bazel build
//cmd/kivgraph:kivgraph` took `1.65 s`, compared with `4.63 s` for the equivalent
`go build ./cmd/kivgraph`. These are developer-machine measurements, not CI
promises, and they do not justify moving Bazel into CI or replacing Make yet.

The follow-up benchmark and adoption decision are tracked in [issue
#124](https://github.com/Luqueee/kivgraph/issues/124). It must repeat the clean,
warm, and one-file-edit comparisons on a representative CI runner and a
developer machine, record setup and external dependency download time
separately, and decide whether Bazel should enter CI, expand its smoke suite,
remain an optional POC, or be abandoned.

## Alternatives considered

- Keep Make as the only build interface. This has the lowest migration cost but
  cannot provide Bazel's action graph or remote-cache seam.
- Move the whole repository to Bazel now. This would duplicate the pnpm and
  native release logic and expand the change beyond a reviewable seam.
- Add `rules_js` in this POC. The three independent pnpm lockfiles and the
  release bundle's explicit payload make that a separate decision with its own
  reproducibility and artifact-size measurements.
