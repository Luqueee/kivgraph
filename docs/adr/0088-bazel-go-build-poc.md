# ADR 0088: Bazel as an optional Go build proof of concept

- **Status:** accepted
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

The follow-up benchmark from [issue
#124](https://github.com/Luqueee/kivgraph/issues/124) measured three isolated
trials on macOS arm64 and a GitHub-hosted Ubuntu 24.04 amd64 runner. Every trial
used private Bazelisk, Bazel action, Bazel repository, Go build, and Go module
caches. It also ignored host Bazel rc files after two excluded CI runs exposed a
shared `output_base`. The
[full method and report](../../benchmarks/build-system-cost/report.md) and
[raw samples](../../benchmarks/build-system-cost/results.json) are versioned with
this decision.

On macOS, median clean totals were `16.84 s` for Go and `59.40 s` for Bazel;
warm no-op builds were `0.127 s` and `0.143 s`, and one-file rebuilds were
`0.147 s` and `0.154 s`. On GitHub Actions, clean totals were `31.38 s` and
`86.86 s`; warm no-op builds were `0.088 s` and `0.314 s`, and one-file
rebuilds were `0.116 s` and `0.490 s`.

Bazel therefore remains an optional POC. It does not enter ordinary CI, replace
any Make target, or expand its smoke suite: it was slower in every measured
scenario on both environments, and the manual benchmark itself occupied a
GitHub runner for `7m42s`. The benchmark workflow remains available through
`workflow_dispatch`; it is manual, has no timing threshold, and fails only when
an arm cannot be measured. Make, the existing CI, native LadybugDB, release
packaging, Rust tooling, and pnpm remain the source of truth.

## Follow-up

Repeat the benchmark and revisit this decision only after a material change,
such as deploying a remote cache or substantially enlarging the Go graph.

## Alternatives considered

- Keep Make as the only build interface. This has the lowest migration cost but
  cannot provide Bazel's action graph or remote-cache seam.
- Move the whole repository to Bazel now. This would duplicate the pnpm and
  native release logic and expand the change beyond a reviewable seam.
- Add `rules_js` in this POC. The three independent pnpm lockfiles and the
  release bundle's explicit payload make that a separate decision with its own
  reproducibility and artifact-size measurements.
