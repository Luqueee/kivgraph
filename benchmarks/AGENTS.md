# Benchmark instructions (`benchmarks/`)

These rules add to the `AGENTS.md` at the repository root, which is always
read. An instruction in this file may add restrictions; it can never relax an
integrity, compatibility or verification contract declared at the root.

- Benchmarks live in `benchmarks/<name>/`, with `results.json` and
  `report.md`. They must keep the command, commit, environment, dataset, seed,
  metrics and limitations.
- Observability benchmarks must separate the local path, the `noop` provider
  and any explicitly configured SDK provider; they must not be presented as a
  single production cost.
- The viewer's end-to-end benchmark is versioned in
  `benchmarks/web-viewer/`; the harness fails closed on a metric out of bounds
  and does not emit `WEB_VIEWER_PERFORMANCE_PASS` if the corpus or GPU do not
  match the declared reference.
- `benchmarks/tool-honesty/` does not measure edges: it measures **what a tool
  claims when its answer is empty**, driving the real binary over MCP against a
  corpus with deliberate blind spots. The clean repositories are half of the
  design: without them, a constant `LOWER_BOUND` verdict would pass every
  check. The two languages go in a single corpus so that it can be checked that
  the verdict does not leak between them, and each arm declares its own blind
  scope: the pass refuses to run if any lost its own. The scope is read from
  `graph_status`, not from the fixture. The Rust arm skips itself, declaring
  so, when its toolchain is missing, and preserves `RUSTUP_HOME` because an
  isolated `HOME` leaves `rustup` with no toolchains.
- `benchmarks/language-recall/` measures, per language, what share of the files
  that depend on a declaration `find_references` returns. The ground truth
  comes from renaming the declaration and type-checking again: the files that
  gain an error are the ones that use it. **In Go, TypeScript and Dart the
  oracle and Kivgraph share the type checker** (`dart analyze` and the Analysis
  Server), so what is measured is not type resolution but everything around it.
  Not in Rust: the oracle is `rustc` through `cargo check` and Kivgraph reads
  `rust-analyzer`. What is measured in the others is which files and variants
  are loaded, how a resolved use becomes an edge and what is filtered. The
  unit is the file, not the reference, because after a broken import the
  compiler does not report the uses of the imported name. An error counts only
  if its line names the original identifier (cascading errors do not) and
  implementation relations and errors on the declaration's own line are
  excluded. Public corpus pinned by commit; one repository per language in the
  pilot, so the figures describe those repositories and not the language. Go and
  Dart read their tests by default (ADR 0132) and are measured with the default
  configuration and with `include_tests: false` (`go.tests_excluded`), because
  the difference between the two is what reading them buys. `-repos` measures
  extra repositories by name -- `gin`, `caddy`, `prometheus`, and `got`,
  `ripgrep` and `args` as the second repository for TypeScript, Rust and Dart
  (`second-repository/`) -- which are not part of the default run. Java and C#
  (`jsoup`, `GuardClauses`) are measured only with `-languages java,csharp`,
  because their indexers are not part of a default installation; they read
  their tests by default (ADR 0142) and their second arm is `tests_excluded`,
  as in Go and Dart. Their results live in `java-csharp/`. Python (`click`) is
  measured only with `-languages python` and with `mypy` and
  `pyright-langserver` on the `PATH`: the oracle is `mypy`, which Kivgraph does
  not use, and its four arms cross `analyzer_mode: exact` with
  `include_tests: false`, because the default mode reads tests (ADR 0144) and is
  not exact. `mypy` runs without a cache on purpose: it reports a file as
  unchanged when its size and modification second match, and here every
  rename adds the same three bytes. Its results live in `python/`. A result
  without the list of what could not be sampled is not published.
- `benchmarks/edit-frequency` does not measure one rebuild against another: it
  measures what the graph costs **an agent that edits**, which is the load for
  which issue `#106` reopened ADR 0057. Its two arms meet in a single figure --
  how many questions a rebuild buys -- because it is the only unit they share:
  editing costs the search arm nothing, and asking costs the graph almost
  nothing. Measured: `17.150 s` per pass after editing one file against `0.101 s`
  per question searched and read, that is `169.5` questions per rebuild, and
  `82.7 %` of the pass is publishing.
- **The warm-up pass is measured, published and enters no median.** It is the
  one that fills the fact cache, and an agent that edits never runs it: mixing
  it into the median reports a cost nobody pays. The difference is not
  marginal -- `123.220 s` cold against `17.150` warm.
- **And the fact cache is pinned to the fingerprint of the binary that
  indexes**, so a run launched with `go run` recompiles to another path and
  cannot hit even its own warm-up. Published runs are made with a built binary,
  and the report says so in its reproduction order. A harness that did not
  would measure a cold pass and call it warm.
- The search arm declares **which searcher ran**. `rg` is not always on the
  `PATH` -- agent hosts bundle it inside their own executable -- and the
  `grep` alternative needs its exclusions written by hand, because ripgrep does
  not descend into a tree of installed dependencies and a `grep` that did would
  time a search that no session makes. The slower searcher **favours the
  graph**, so the published figure is a lower bound and the report names it as
  such.
- `benchmarks/snapshot-heap` does not measure resident pages either: it
  separates, in what loading a published snapshot costs, the bytes a reader
  **keeps** from those the load allocates and throws away. It takes the profile
  with the snapshot **live**, which is the only way to attribute it: the
  package benchmark writes its own when it is already unreachable and
  attributes not a single byte.
- And the two halves **are not the same figure in `Private_Dirty`**, which is
  what this file used to say. Only the one that is kept is, in steady state:
  `benchmarks/load-cost-resident` removed `60.5 MB` of the transient half and
  the resident per server did not move (`71.76 MB` against `71.22`, three pairs
  of three). Lowering what is allocated buys time to first answer; bytes per
  process are lowered by moving a structure to the mapped file, and only that.
  Whoever writes a `snapshot-heap` figure into a resident-memory sheet is
  quoting the wrong magnitude.
- `benchmarks/load-cost-resident` runs in a Linux container and **is not the
  reference host**: what makes its units comparable is the `4096`-byte page
  size, and what makes its two arms comparable is that they ran in the same VM
  against the same file. It does not overwrite the artifacts of
  `shared-snapshot`, and it asserts no latency bound.
- `benchmarks/daemon-cost` answers what one process serving N clients costs
  against N processes serving one each. What it publishes as the answer is the
  **slope per client**, not any total: an arm that saved for two clients and
  not for eight would look like a victory in any single row. It measures the
  count of **one** client even though a daemon shares nothing there, because
  that is where its fixed cost would be visible with nothing to amortize it --
  and there it turned out to tie, disproving the prediction of ADR 0065.
- **`daemon-cost` measures the daemon's two doors at three loads and publishes
  one artifact per combination**: `results-idle.json` and
  `results-http-idle.json` with no call at all, `results.json` and
  `results-http.json` with `8`, and `results-http-sustained.json` with `2,000`.
  The transport and the counts enter the digest; without that, runs collide on
  one identity and a socket figure can be quoted as if it were reachable. The
  schema is `daemon-cost-v3`.
- **Zero load is not a theoretical extreme: it is the median.** `48` of `51`
  real servers receive no call at all, so `-calls 0` measures the case that
  predominates. There the startup turned out to be the cost -- `33 MB` per
  client without answering anything, against `40` answering -- and that was
  fixed: ADR 0067 moved the reading of the graph to the first query, and the
  current idle figure is `10 MB` per client against `39` answering and `66`
  under sustained traffic. It is also the only load at which the four points of
  the sweep measure the same per client, because `-calls N` spreads N calls
  among however many clients there are.
- **A dirty tree does not publish a bare commit.** `commit` carries `-dirty`
  when there are uncommitted changes and `-unknown` when it could not be known,
  and both variants are declared in `limitations`. It is the normal case -- the
  figures that justify a change are measured before committing it -- and
  without the suffix the artifact attributes its numbers to code it did not
  run. Published runs are made from a clean tree.
- **A guard cannot be load.** The `graph_status` that proves both arms serve the
  same generation runs **after** the sampling, not before: nothing requires it
  to precede the bytes, and asking first made zero load impossible to measure.
  It fails and discards just the same. That move is what raised the schema from
  `v2` to `v3`: a `v2` file includes that call in its bytes.
- **What was not measured is not published as zero.** Percentiles,
  `new_client_ms` and the latency ratios are pointers and disappear from the
  file when the run asked nothing; the summary prints `--`. A `p50_ms: 0` reads
  as an instantaneous answer and a `p99_ratio: 0` as an infinitely faster
  daemon. What is measured at every load is `new_client_connect_ms`, which is
  the field two loads are compared with.
- A probe that exists only on the path of a real server -- those of
  `startServer` and `connect` -- is caught by no local test: deleting it breaks
  nothing on a laptop. That is why the run refuses to publish an idle file that
  timed any first answer, and that refusal does have a test.
- **Load is counted, not chosen, and it is the variable that decided the result
  twice.** The event log of a `serve` records every tool call, so the load of a
  real session is recounted from a usage log -- the command is in the report.
  Measured: median of **one** call per session and `48` of `51` servers with
  none. The `2,000` calls of the sustained case are three orders of magnitude
  above that, and there HTTP seemed to cost `12.5 MB` per client when under real
  load it costs less than `1 MB`, like the socket. A benchmark that measures the
  wrong load is not imprecise: **it answers another question**, and in this case
  it underestimated the saving.
- The sustained case is kept in `results-http-sustained.json` because it is a
  useful ceiling, and **its figure does not carry across corpora**: `4.9`–`5.9 MB`
  per client over `117,499` symbols against `12.8` over `108,737`, reproduced
  twice on corpora of that size. That dependence on the corpus is the signature
  of a cost in retained bytes -- the `10 MiB` resumption buffer the SDK gives
  every session -- and not of a cost per session. Quoting the ceiling as if it
  were the cost is the mistake this benchmark already made.
- It is not an arm of `shared-snapshot` and must not become one: that
  benchmark's arms are defined by whether the snapshot file is present, and its
  gate measures mapping against deriving. A third arm would leave its
  comparison meaningless.
- A per-symbol figure is read from the pass that produced it, never crossed
  between corpora. The current `daemon-cost` runs use the `108,737` symbols of
  `workspace` at its generation `000001`; those of `117,499` are in the
  history, not in these tables. `workspace` is a workspace in use, so a later
  reindex does not reproduce the earlier count: the corpus is declared per pass.
- `benchmarks/mcp-token-cost/corpora/` freezes the corpus on third-party
  repositories at a **full commit** (a tag can move, and the hash of an
  annotated tag is not a commit): so the native arm's captures are taken once
  and a figure that changes is the server's. `prepare` refuses to measure a
  checkout that is not at that commit or is dirty. `--check` is the ratchet: it
  fails if an answer costs more than its `results.json` by more than the
  tolerance or if a question finds **fewer references**, and it refuses to
  compare what is not comparable (another commit, another question set, another
  capture). A corpus without `results.json` is declared and skipped in CI. The
  native arm remains the literal output of the host's tool, so it is recaptured
  with `recapture.py` in a session with `KIVGRAPH_DISABLE_HOOK=1`: the gate
  denies that very `grep`.

## Corpora and audits

- Large-scale synthetic acceptance corpora are generated at a private path and
  never replace or modify indexed repositories. For LadybugDB, reproducibility
  must distinguish logical facts (counts, schema and integrity) from the
  physical bytes of the native file.
- An accuracy audit must separate `false exact edges` from dangling edges:
  compare fixtures with ground truth for the former and run the canonical
  invariants of endpoints, evidence and provenance for the latter.
- An `ACCEPT_KIVGRAPH_WITH_LIMITS` report must list platform, toolchains,
  corpus, transport, guarantees, metrics and residual risks; it cannot turn a
  known limitation into an implicit PASS.

`dist/` and the indexed repositories are never used as input: private copies or
fixtures are generated.

## A latency bound is not asserted in `go test ./...`

An SLO is a property of the machine that measures it, and `go test ./...` runs
anywhere: on a shared runner with two more jobs next to it. Asserting it there
produces a failure that does not describe the code.

It happened: the `v0.2.0` release fell over with `find_cross_repo_consumers` at
a `p95` of `11.6 ms` against a bound of `5 ms`, **on the same commit whose CI
had just passed** on another runner, and with five of five local passes below
the bound. The code had not changed between the two observations.

So the bound is checked when someone asks for it -`KIVGRAPH_BENCH_SLO=1`, which
is what a benchmark gate means- and reported the rest of the time. What is
always asserted is that the measurement took place: a harness that stopped
evaluating its checks would pass anyway, and that is worse than a missed bound.

The same rule holds for any gate that depends on the environment -`rust-src`
installed, a GPU, a corpus- and for the same reason: the absence is declared
and skipped, never turned into a `FAIL` that points at the wrong place.
