# ADR 0120: The analyzer is never told to discover a sysroot

Status: accepted, 2026-11-15. Refines ADR 0041.

Changes the configuration: no key and no vocabulary. What `rust.sysroot:
discover` does on a machine without `rust-src` changes; see Compatibility.

Changes the schema: no.

## Context

`rust.sysroot` defaults to `discover`, and the value used to reach
`rust-analyzer` verbatim. Discovery is not a read. An analyzer that discovers a
sysroot without the `rust-src` component runs `rustup component add rust-src`
itself: a download, and a write into the toolchain.

Three things were wrong with that.

- **The pass is documented as offline.** Cargo runs `--offline --locked`, and
  `rust.allow_network` is the only declared way out. The install went around
  both.
- **It is a race.** Two passes that start together both find the component
  missing and one of them installs it. The other indexes with
  `can't load standard library` and resolves nothing from `core`, `std` or
  `alloc` -- on a machine where the sources exist a second later.
- **Nothing said so.** The loser published a graph with fewer edges and a
  diagnostic nobody reads.

It surfaced as a flaky test. `TestFullIndexesARustRepository` failed once in CI
and passed on a rerun of the same commit. On `ubuntu-latest`, eight copies of
the test started at once failed 7 of 8 in the first round without `rust-src`
and 0 of 24 with it preinstalled; run one after another, 240 of 240 passed.

## Decision

1. **`Run` resolves `discover` itself.** It asks `rustc --print sysroot` from
   the workspace -- which is what honours a `rust-toolchain.toml` -- and passes
   the path. Given a path, the analyzer loads what is under it and installs
   nothing.
2. **The probe looks where the analyzer looks.** `$RUSTC`, then
   `$CARGO_HOME/bin` (by default `~/.cargo/bin`), then the `PATH`. A probe that
   read the `PATH` alone found no toolchain in a process started with a minimal
   one, and the analyzer it then started found `~/.cargo/bin/rustc` and
   installed anyway.
3. **No `rustc`, no discovery.** When nothing answers, the analyzer is told
   `none` and the pass carries a diagnostic naming the reason. `discover` is
   not passed on in any branch that has the network off.
4. **`rust.allow_network: true` leaves discovery to the analyzer.** It is the
   declared way out and it stays one.
5. **The fact cache sees the library sources.** The Rust fingerprint records
   whether the toolchain carries them. Installing `rust-src` changes neither
   the release nor a file of any repository, and without this the cache would
   go on serving the facts produced without it.

## Compatibility

A machine without `rust-src` used to get it installed, silently, on its first
Rust index. It now indexes its repositories without the standard library and
says so, which is what ADR 0041 and the configuration reference already
described. To get the previous result, run `rustup component add rust-src`
once, or set `rust.allow_network: true`.

Every Rust fingerprint changes once, because it gains a field. The next pass
re-analyses Rust repositories instead of reading them from the fact cache.
Nothing that was valid becomes an error.

A machine that has both a `rustc` on the `PATH` and another in `~/.cargo/bin`
now has its sysroot read from the second, which is the one the analyzer was
already running.

## Not done

- `cargo` is still found through the `PATH` by `cargo metadata` and by the
  fingerprint. It installs nothing, so it is outside this decision.
- The order in decision 2 is the one rust-analyzer documents. What was
  observed here is the part that matters: with no `rustc` on the `PATH`, the
  pinned analyzer ran the one in `$CARGO_HOME/bin`.

## Verification

- `TestRunDoesNotInstallTheStandardLibrarySources` drives the real analyzer
  against a sysroot without `rust-src` and a `rustup` that records its calls.
  Before decision 1 it failed with `component add rust-src`.
- `TestRunDoesNotInstallForAToolchainThatIsOffThePath` does the same with the
  toolchain only in `$CARGO_HOME/bin`. Before decision 2 it failed the same
  way.
- `TestHermeticSysrootNeverPassesDiscoveryOn` pins decisions 3 and 4, and
  `TestRunLeavesDiscoveryToTheAnalyzerWhenTheNetworkIsAllowed` pins 4 against
  the analyzer.
- `TestLookToolchainReadsWhereTheAnalyzerReads` pins the order.
- `TestRustFingerprintChangesWhenTheLibrarySourcesArrive` pins decision 5.
