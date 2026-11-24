# Rust path instructions (`internal/rustloader/`)

These rules add to the `AGENTS.md` at the repository root, which is always
read. An instruction in this file may add restrictions; it can never relax an
integrity, compatibility or verification contract declared at the root.

The engine rules are in `internal/AGENTS.md`.

## Authority and running the analyzer

- Rust is not analyzed in process: the authority is `rust-analyzer scip`,
  invoked as an external process once per Cargo workspace. Tree-sitter produces
  neither identity nor resolution; it contributes the syntactic class of the use
  and the declared visibility, just as Go's AST contributes `GO_AST_CALL` over a
  `go/types` resolution.
- The bundle carries `bin/rust-analyzer`, pinned in `tools/manifest.json` by
  version, URL and digest, and downloaded by `scripts/fetch-rust-analyzer.sh`.
  At run time the binary that travels next to the executable wins, then an
  explicit path from the configuration and last the `PATH`; `doctor` says which
  one answered and `kivgraph version --json` publishes its release. What the
  bundle does not carry is a Rust toolchain: without `cargo` the analyzer does
  not load the workspace, so `doctor` checks it separately and fails naming it.
- An isolated `HOME` leaves `rustup` with no toolchains, and with that no
  workspace loads. `RUSTUP_HOME` hangs from `$HOME/.rustup`, so a harness that
  points `HOME` at a temporary directory -which is what must be done so as not
  to touch the user's generation- has to preserve `RUSTUP_HOME` and `CARGO_HOME`
  separately. Without that `rustc` stops answering, every Rust repository comes
  out `WORKSPACE_NOT_LOADED`, and a measurement whose clean arm depends on
  something loading passes green having measured nothing. Reproduced and fixed
  in `benchmarks/tool-honesty`, which separates the two things in
  `indexEnvironment`.
- The `scip` subcommand always runs build scripts, so hermeticity is imposed
  from outside: `CARGO_TARGET_DIR` at an external state directory
  -`cargo.targetDir` does not do, its value is relative to the workspace-,
  `--offline --locked`, and a later check that the repository did not gain a
  `target/` or a new `Cargo.lock`. `rust.allow_network` is the only declared
  way out.
- `sysroot: "discover"` does not reach the analyzer as is. An analyzer that
  discovers a sysroot without `rust-src` runs
  `rustup component add rust-src` itself: a download and a write into the
  toolchain from an offline pass. Two simultaneous passes competed for that
  installation and the one that lost indexed without a standard library. `Run`
  asks `rustc --print sysroot` from the workspace and passes it the path, with
  which the analyzer loads whatever is there and installs nothing. The `rustc`
  is looked up where the analyzer looks it up -`$RUSTC`, `$CARGO_HOME/bin`, and
  only afterwards the `PATH`-, because a process with a minimal `PATH` saw no
  toolchain and the analyzer did. If none answers it is passed `none` with a
  diagnostic; `discover` only reaches it with `rust.allow_network`. The fact
  cache fingerprint records whether the toolchain carries `rust-src`. See ADR
  0120.
- Asking the toolchain does not install either. `rustc` and `cargo` are usually
  rustup's proxies, and a proxy without its active toolchain downloads it before
  answering: `rustc --version` with an empty `HOME` took 15 to 19 s on the macOS
  runner and left 1.4 GB each time. Every question goes with
  `rustloader.ProbeEnvironment()`, which carries `RUSTUP_AUTO_INSTALL=0`. The
  analyzer and `cargo metadata` carry it too when the network is off. The
  pinned analyzer already puts it on everything it launches -twelve distinct
  invocations of `cargo` and `rustc` on the fixture, measured-, but the analyzer
  is whichever one `rust.analyzer_command` names.
- The unit of analysis is the Cargo workspace, not the crate: the analyzer loads
  the whole workspace on every invocation. The concurrency ceiling is low
  because of memory.

## Identity and resolution

- The stable identity of a Rust symbol is its SCIP string -crate, descriptor
  path and suffix-, never its signature: rust-analyzer emits no
  `SymbolInformation` for a declaration outside the workspace root, so a
  consumer that depended on the signature could not name the key its provider
  publishes. The `Discriminator` comes from the descriptor's disambiguator.
- Local symbols (`local N`) are a per-document counter: they are not
  addressable and never enter the graph.
- A reference becomes an edge only if somebody publishes its target: the pass
  itself, or the registered provider repository. A symbol of the repository's
  own that the index does not define -the `impl` block that `-> Self` points to,
  which SCIP mentions and never defines- is declared `DEFINITION_NOT_INDEXED`.
  Composing its key and trusting that another will publish it aborts the whole
  pass with a dangling edge.
- The symbol that contains a reference is obtained from the `enclosing_range` of
  the definition occurrences in the same document, the innermost one that
  contains it. SCIP says which symbol a use resolves to, never which
  declaration contains it.
- Rust's `IMPLEMENTS`, `EXTENDS` and `OVERRIDES` do not come from
  `SymbolInformation.relationships`, which always travels empty: they come from
  the shape of the `impl` and of the bound, with both ends resolved by the
  analyzer. The target of an `OVERRIDES` is composed from the trait's symbol and
  is emitted only if the index observed it; an implementation the grammar does
  not see -generated by a macro- stays absent and is not guessed.
- A name written inside the body of a `macro_rules!` has no occurrence in the
  index: the analyzer resolves nothing there. The grammar reads those paths
  (`MacroPaths`) and a `CANDIDATE` reference with provenance
  `TREE_SITTER_SYNTAX` is published only when exactly one declaration of the
  macro's crate ends with that path. Never `EXACT`; see ADR 0128.
- Naming a function is not calling it, and the three ways Rust moves it are three
  distinct relations: argument of a call (`PASSES_AS_CALLBACK`, with its own
  provenance `RUST_SYNTAX_CALLBACK`, the mirror of `GO_AST_CALLBACK`), value of a
  `let`, `const`, `static` or a literal's field (`ASSIGNS_FUNCTION`), and result
  of a body, with `return` or as the final expression (`RETURNS_FUNCTION`). The
  grammar decides the class and the analyzer the target, as in a call.
- A value-position class also requires the target to be callable and this pass
  to have indexed it: `takes(LIMIT)` is an argument that is not a callback, and
  a target from another repository arrives without `Kind`. In both cases the
  edge degrades to `REFERENCES` instead of asserting what nobody read. The
  climb through the expression crosses what does not change what is named -a
  path, a borrow, an array or tuple literal- and never a field access: returning
  `object.field` does not return the object.
- The index of stable keys is built over the **sorted** list of definitions,
  first one wins. Two analyzer symbols can share a key -it does not carry the
  crate version, and rust-analyzer emits duplicates-, and walking a map the
  iteration order decided the winner: two passes over the same corpus published
  170 edges of difference at the root of `std`.
- Rust visibility is not just `pub`: a member of a `trait` is as visible as the
  trait, and a method of a trait implementation is reachable through it.
  Reading only the modifier would publish a false public API.
- The published `Kind` of a Rust symbol is SCIP's fine one -`struct`, `trait`,
  `field`, `trait_method`-, not the descriptor suffix that decides the key: with
  the suffix, a struct, an enum and an alias are all `type`.
- A use belongs to a declaration of its own document. If the innermost one that
  contains it is published in another file, it climbs to the next container and,
  if none is left, the use has no source: crediting it places the observation
  in a file where it did not happen and the snapshot rejects it. A resolution
  failure needs no source and is declared all the same, with its file and its
  position.
- No edge is published toward a target of this repository that the pass does
  not publish; it is counted in `EdgesWithoutTarget`. A target from another
  repository is a different matter: it is absent on purpose and the merge
  closes it.
- A symbol the analyzer defines in more than one document is named in the
  diagnostics, with the document that publishes it and those that do not. See
  ADR 0039.

## Standard library and providers

- `core`, `std` and `alloc` enter the graph with `rust.index_sysroot`, off by
  default. Without them, four measured silences: `#[derive(...)]` produces no
  relation, operator overloading does not reach its trait, the `?` operator does
  not reach `Try::branch`, and every call to the standard library disappears.
  With them, all four are exact edges. The absence remains a declared lack and
  never a failure: a machine without a toolchain or without `rust-src` indexes
  its repositories and says why it did not index the library. See ADR 0041.
- The sysroot's unit is the `library` workspace and nothing below it. A
  toolchain vendors in there `backtrace`, `compiler-builtins`, `portable-simd`
  and `stdarch`, which are not the standard library: six of those workspaces do
  not load, contribute crates nobody can name, and their build scripts make two
  passes of the same toolchain publish a different number of edges. They are
  named in the diagnostics and their crates are not registered, because
  registering a provider the pass does not index composes a key nobody
  publishes.
- A library crate is attributed by **origin**, never by version or by name. The
  two sides of the boundary write different things in the moniker's version
  field -the consumer a URL, the library, indexed as a workspace, `0.0.0`-, and
  the stable key carries no version, so both compose the same key. A reference
  that names `core` with a release is `CRATE_VERSION_MISMATCH`; a registered
  repository that declares a library crate, or two toolchains at once, is
  `AMBIGUOUS_CRATE_PROVIDER`.
- The synthetic repository is called `rust:<release>` and that namespace is
  reserved in the registry: `init` rejects a user name that takes it. That is
  what makes the name the authority and avoids a column in the canonical schema
  to answer whether a row is derived. It carries no `commit`, `branch` or
  `dirty`: nobody clones it and nobody can move it.
- The fact cache fingerprint includes `rustc --version`, so a toolchain change
  invalidates every fact taken from it. The library costs one cold pass per
  toolchain and is served from the cache in the following ones.
- A unit accepts on credit a target attributed to another repository, because it
  cannot see the provider's facts. When the provider does not publish it -`impl
  Add for u32` is generated by `add_impl!` and exists in no range of code- the
  merge withdraws the edge and declares the use as
  `PROVIDER_DEFINITION_NOT_INDEXED` with its crate, its symbol and its position;
  it is counted in `EdgesWithoutProvider`. The description travels with the unit
  that composed the key, also in its cache entry: without that a warm pass cannot
  close what the cold one published.
- Rust's unresolved references are derived from three observed sources -the
  crate registry, the diff between the Tree-sitter inventory and the SCIP
  definitions of the same file, and the workspace load failure-, because the
  index silently discards the tokens without a moniker.
- Two workspaces of **the same** repository that declare a crate of the same
  name -a vendored copy, an example, a fixture- would give two declarations the
  same identity, and the published set did not validate: the pass died for
  everybody. The first by path keeps the name; the other stays out entirely and
  is declared `AMBIGUOUS_CRATE_PROVIDER` (`markShadowedWorkspaces`). It is not
  discriminated by workspace because the identity has no such field and adding
  it changes every stable key.
- An `.rs` file under the `src` of an indexed crate that has no document -a
  module behind a `cfg` that is false on this machine- is read by words, and
  every name the workspace declares and the file writes is declared as
  `TARGET_NOT_BUILDABLE` with that file (`collectUnselectedFiles`, ADR 0150).
  Without that, its uses were in no edge and in no failure, and the answer said
  `COMPLETE`. It resolves nothing: it never produces an edge.
- A crate name declared by several repositories is an ambiguity: none provides it
  and it is declared `AMBIGUOUS_CRATE_PROVIDER`. A version the analyzer does not
  know (`.`) identifies no code and never resolves.

## Cargo discovery

- Cargo discovery does not run `cargo`: it reads the manifests with
  `BurntSushi/toml` and resolves membership by directory, as Cargo does. A crate
  with no workspace above it is a workspace of one.
- A manifest that declares its own `[workspace]` inside another's directory is
  a legitimate root, not an error: that is how an independent tree is vendored
  and Cargo accepts it -the standard library carries `library/backtrace` exactly
  like that-. What Cargo rejects is a manifest that is a root **and** a member
  of the workspace above, which it calls "multiple workspace roots found in the
  same workspace", and the `members` are compared as globs.
- The boundary of a Rust repository is a single decision, answered by
  `workspace.CargoExcludes`: what discovery does not walk -`.git`, `target`,
  `vendor`, `node_modules` and the configured `exclusions`- does not enter the
  analysis either. A crate vendored with `[patch.crates-io]` is code that Cargo
  compiles, that the analyzer indexes and that no manifest of this repository
  declares: publishing its uses while its declarations are discarded leaves one
  dangling edge per use and aborts the pass. Its uses are declared
  `CRATE_PROVIDER_NOT_FOUND`, which is what they are.
- A Cargo package compiles several crates -library, binaries, build script, one
  per integration test- and the SCIP moniker names the package, so its root
  modules arrive as a symbol defined in several documents. The most reachable
  target publishes it, with the path as tie-break: only the library is linkable
  and it is the only one another repository can name. That decides where the
  node lives and never what it is called; the key does not change.

## Verification

The tests that run the analyzer are skipped when `rust-analyzer` is not
installed, so verification requires having it:

```bash
rustup component add rust-analyzer rust-src
go test ./internal/rustloader/... ./internal/indexer/ -run Rust
```

Being on the `PATH` is not being installed: rustup leaves a proxy called
`rust-analyzer` for every toolchain, whether or not the component exists, and
that proxy fails with `Unknown binary 'rust-analyzer' in official toolchain`.
The tests' guard (`testsupport.RequireRustAnalyzer`) runs `--version` and skips
the test if it does not answer; a guard that only looks for the name takes the
analyzer as installed on any machine with rustup and turns a `SKIP` into a
`FAIL`. CI installs the component: skipping the whole suite would hide the Rust
path instead of verifying it.
