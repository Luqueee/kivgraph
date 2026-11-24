---
name: running-tests
description: How Kivgraph's tests are run and written - the three suites, the `ladybug` tag that is never invoked by hand, what gets skipped and why, and the binary smoke test. Use when running tests, when adding them, on a `library 'lbug' not found` link failure, on an unexpected `SKIP`, or before closing a task.
---

# Running Kivgraph's tests

## The rule that is hardest to discover

**Never** `go test -tags ladybug`. It fails at link time:

```
ld: library 'lbug' not found
```

The library is not missing: the linker is not being told where it is.
`make test-ladybug` exports the `CGO_*` variables that point at
`.tooling/ladybug/<version>`, passes the rpath through `-ldflags` and is the only
supported way to run that tag.

```bash
make test-ladybug                                   # full suite with the native layer
make test-ladybug PKGS=./internal/storage/ladybug   # one package while working
```

`scripts/fetch-ladybug.sh` downloads and verifies the library the first time; the
target invokes it by itself. **One** warning per binary remains, and it is the
binding's: the `-L` pointing at its own module directory does not exist in a
pinned build. Silencing it would require patching a module pinned by digest.

The `duplicate -rpath` and `ignoring duplicate libraries` warnings no longer
appear. There were `221` in a full pass and none was a defect: `CGO_LDFLAGS` is
applied to every cgo package instead of once at link time, so naming `-llbug` or
the rpath there -- which the binding already declares -- made the linker see ten
copies of each. If they come back, somebody put them back in `CGO_LDFLAGS`.

## The three suites

| Command | Covers | Running | Cached |
| --- | --- | --- | --- |
| `go test ./...` | 34 packages; all the logic that does not touch native LadybugDB | `20 s` | `0.6 s` |
| `make test-ladybug` | 39 packages with `-tags ladybug` | `45 s` | `47 s` |
| `pnpm check` in `ts-worker/` and in `web/` | format, lint, types and `vitest` (87 and 69 tests) | `3 s` each | same |

The first `make test-ladybug` takes about `150 s`: it compiles cgo from scratch.
After that the cost is that of running, and the test cache does not lower it
because linking against the native library is redone anyway.

`make test` runs `go test -p 2 ./...`. By default Go tests one package per core,
and on a 10-core Mac that left `0 %` CPU free in four of eleven samples. With
`-p 2` the minimum free was `15 %` and the suite took `70 s` instead of `61 s`.
CI does not use `make test`, so it is not affected; by hand, `go test ./...`
is still fine if the machine is doing nothing else.

`go test ./...` **does not compile** the files with `//go:build ladybug`: there
are 22 files with **75 tests** that the ordinary suite does not see, spread
across `internal/storage/ladybug`, `internal/indexer`, `internal/app` and
`internal/resilience`, plus five benchmark packages that only exist under the
tag. Those packages come out `ok` without the tag -they also have tests that do
not need it-, so a green says nothing about them. A change to storage, indexing,
rebuild, snapshots or shutdown **is not tested** until it passes
`make test-ladybug`.

## What to run depending on what you touch

| Change | Commands |
| --- | --- |
| Go, not touching storage | `gofmt -l <files>`, `go vet ./...`, `go test ./...` |
| Storage, indexer, rebuild, snapshots, upgrade | the above **plus** `make test-ladybug` |
| Rust path | also `go test ./internal/rustloader/ ./internal/indexer/ -run Rust` with `rust-analyzer` and `cargo` on the `PATH` |
| `ts-worker/` | `cd ts-worker && pnpm check && pnpm build` |
| `web/` | `cd web && pnpm check` |
| CLI, MCP surface, commands | add `go test -race ./cmd/kivgraph/...` (`6 s`), which is what CI runs |
| Distribution, bundle, manifest | `make build-darwin-arm64` and `(cd dist/kivgraph-darwin-arm64 && shasum -a 256 -c SHA256SUMS)` |

## What gets skipped, and how to stop skipping it

A `SKIP` here is not a broken test: it is a host dependency that is not there.

- `rust-analyzer is not installed` -- declared by `internal/rustloader/analyze_test.go`,
  `internal/rustloader/scip_run_test.go` and `internal/indexer/rust_unit_test.go`.
  Resolved with `rustup component add rust-analyzer`. Without it, the Rust path
  stays **untested** even if the suite comes out green.
- `host filesystem cannot satisfy the default space policy` -- publishing a
  generation rejects a disk more than 85 % full
  (`internal/testsupport.RequireSpaceOrSkip`). It is product policy, not a
  failure; free up disk if you need to cover that path.

Before claiming a suite passed, check that it did not skip what you were going to
test: `go test ./internal/rustloader/ -v 2>&1 | grep SKIP`.

## Narrowing a failure

```bash
go test ./internal/indexer/ -run TestClassifyRustChange -v   # one test
go test ./internal/indexer/ -count=1                          # no cache
go test ./internal/indexer/ -count=10 -run TestX              # is it flaky?
make test-ladybug PKGS=./internal/rebuild                     # with the native layer
```

The test cache is aggressive and `ok (cached)` runs nothing: if you changed a
fixture, a testdata file or an environment variable, use `-count=1`.

## Writing tests here

- A test defends an **observable behaviour** and fails on a plausible regression.
  Negatives, invariants and comparison against a clean rebuild are part of the
  contract, not an extra.
- **`internal/testsupport.TempDir(t)`, never `t.TempDir()`** when the path feeds
  the workspace layer: on macOS `/var` and `/tmp` are symlinks, validation
  rejects paths with a symlink ancestor, and the test would end up exercising the
  rejection instead of what it intends.
- **Do not modify indexed repositories** or the benchmarks' inputs. Fixtures live
  in `testdata/`; copy, do not edit in place. The Rust fixture is
  `testdata/rust/workspace` and indexing passes must leave it intact -no
  `target/`, no `Cargo.lock`-, which is worth checking in the test itself.
- A fixture with an external toolchain -Cargo, `rust-analyzer`- requires a real
  `HOME`: a temporary `HOME` breaks rustup's shims. If you isolate `HOME`, also
  pass the real `RUSTUP_HOME` and `CARGO_HOME`.

## Binary smoke test

The tests do not cover the installation, and **the binary from `make build` is
no good for this**: without the `ladybug` tag there is no native layer, and the
pass runs all the way to die at publishing.

```
stage.staging: FAIL (ladybug load canonical: LadybugDB native support is unavailable)
index.full: FAIL
```

Build one with the native layer -the same flags as `make test-ladybug`- or use
the bundle's, `dist/kivgraph-darwin-arm64/bin/kivgraph`:

```bash
LIB="$(scripts/fetch-ladybug.sh)"
CGO_ENABLED=1 CGO_CFLAGS="-I$LIB" CGO_LDFLAGS="-L$LIB" \
  go build -tags ladybug -ldflags="-extldflags=-Wl,-rpath,$LIB" \
  -o /tmp/kivgraph-native ./cmd/kivgraph
```

The full flow, with a disposable `HOME` and without touching any real
repository:

```bash
export RUSTUP_HOME="$HOME/.rustup" CARGO_HOME="$HOME/.cargo"   # the real HOME
home=$(cd "$(mktemp -d)" && pwd -P); repo="$home/fixture"
cp -r testdata/rust/workspace "$repo"
(cd "$repo" && git init -q && git add -A && git -c user.email=a@b -c user.name=t commit -qm fixture)
env HOME="$home" /tmp/kivgraph-native init --repository "fixture=$repo" --languages rust
env HOME="$home" /tmp/kivgraph-native doctor
env HOME="$home" /tmp/kivgraph-native index --full
```

`RUSTUP_HOME` and `CARGO_HOME` are exported **first** and point at the real
`HOME`: `env HOME=` only isolates Kivgraph's configuration. Without that,
rustup's shim does not find its toolchain and `doctor` declares
`toolchain.cargo: FAIL` because of an artefact of the test, not of the state of
the machine.

A healthy `index --full` publishes `stage.integrity: PASS (0 invariant violations)`,
`stage.golden probes: PASS` and a generation. `doctor` names each toolchain
separately: `toolchain.rust` can be PASS with the bundled analyzer while
`toolchain.cargo` fails, and then indexing isolates the workspace
(`not_loaded=1`) instead of aborting.

## Benchmarks: they are not tests

They live in `benchmarks/<name>/` and are run by hand:

```bash
go run ./benchmarks/rust-semantic
```

Those that measure the native layer carry `//go:build ladybug` and without the
tag they do not even compile -`build constraints exclude all Go files`-. They
need the same flags as `make test-ladybug`:

```bash
LIB="$(scripts/fetch-ladybug.sh)"
CGO_ENABLED=1 CGO_CFLAGS="-I$LIB" CGO_LDFLAGS="-L$LIB" \
  go run -tags ladybug -ldflags="-extldflags=-Wl,-rpath,$LIB" \
  ./benchmarks/ladybug-bulk
```

They write versioned `results.json` and `report.md` and emit a gate on `stdout`
(`GO_SEMANTIC_PASS`, `RUST_SEMANTIC_PASS_WITH_LIMITS`, `HOT_SNAPSHOT_PASS`...).
A `_WITH_LIMITS` gate must list its limitations; it is never presented as a
clean PASS.

**They overwrite their versioned artifacts.** Running a benchmark out of
curiosity leaves a diff in `benchmarks/`; either it is reviewed and justified, or
it is reverted. They are not run to "check they still work".

## Before closing a task

```bash
gofmt -l <modified-go-files>
go vet ./...
go test ./...
make build
make test-ladybug     # if you touched the native layer
git diff --check
```

And in `ts-worker/`, `pnpm check && pnpm build`.

If the change touches `docs/` or a `CLAUDE.md`, also `scripts/check-docs.sh` --
which verifies the symlink and the column width, **not** the language. The
language is decided by whoever writes: a new document, in English; a document
that already exists in Spanish stays in Spanish until somebody translates it
whole. The rule is in *The language of what is written*, in the root `AGENTS.md`,
and it also reaches the commit and the pull request that close the task.

And a trap of that gate that costs a red CI: `check-docs.sh` measures width with
`git diff`, so it **does not see an untracked file**. On a newly created document
it answers `ok` without having looked at anything. Run it after `git add`.

## What this machine cannot test

The `linux/amd64` bundle is not built here: cgo links the native library and the
project does not cross-compile. The Linux CI job verifies it, or a native Linux
host over SSH -copy the tree with `git archive HEAD` plus the unversioned files;
macOS `tar` with `--exclude` drops directories along the way-. An emulated
container serves to test scripts and downloads, not to sign the artifact that
gets published.
