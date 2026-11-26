# Engine instructions (`internal/`)

These rules add to the `AGENTS.md` at the repository root, which is always
read. An instruction in this file may add restrictions; it can never relax an
integrity, compatibility or verification contract declared at the root.

The Rust path has its own file in `internal/rustloader/AGENTS.md` and the MCP
surface has its own in `internal/mcp/AGENTS.md`.

## Go loading and workspaces

- A method whose receiver has no name -that of an anonymous interface, whether
  the one in `var _ interface{ ... } = x` or in an assertion
  `y.(interface{ M() })`- is not addressable and never enters the graph, like a
  Rust `local N`. There is no path to it from the package scope, so it gets no
  objectpath and its identity falls back to the qualified name, which without an
  owner is the bare method name: two such declarations in a package derive one
  key from two files, and a `Symbol` with two `File`s that declare it is what the
  multiplicity of `DEFINES` forbids. `go.include_tests` made it appear because a
  test file is the second place where that interface is written.
- `internal/indexer.Full` loads Go through the package patterns produced by
  `DiscoverGo`; do not replace them with `./...`, because the configured
  `exclusions` must stay effective during `go/packages`.
- The build tags Go is loaded with come from `go.build_tags`. A directory whose
  files that configuration excludes is not an index failure: it is declared as
  `UNRESOLVED` with reason `PACKAGE_NOT_BUILDABLE` and the pass continues. Any
  other loader diagnostic still aborts it. Indexing this repository requires
  the `ladybug` tag.
- When an **entire repository** fails there are two contracts, and which one
  applies depends on the language. Go, Rust, Python, Dart, Java and C# record an
  `UNRESOLVED` row **with no file** -`MODULE_NOT_LOADED`,
  `WORKSPACE_NOT_LOADED`, `ANALYZER_UNAVAILABLE`, `REPOSITORY_NOT_LOADED`- and
  the pass continues: the graph is published declaring the hole, and every answer
  about that repository comes out as `LOWER_BOUND`. The last four have done so
  since ADR 0147: before, they returned an error and a C# repository with no
  solution left every other one without a graph. A cancelled pass is not an
  unreadable repository and is still an error. TypeScript does the same since
  the amendment to that ADR, in two places: a repository whose projects cannot
  be discovered -a `tsconfig.json` that does not parse- comes out as
  `REPOSITORY_NOT_LOADED`, and a package in which the worker fails as
  `PACKAGE_NOT_LOADED`. A worker that is not there is another matter: it travels
  with Kivgraph, so it is a broken installation and is still an error. Both
  forms are honest and neither asserts an absence; only the first is measurable
  from a served graph, and that is why it is the only one `benchmarks/tool-honesty`
  exercises. That the row carries no file is the contract, not a detail:
  `hotsnapshot.UnresolvedScopes` selects exactly the rows whose file is not set,
  so one that gained one would stop bounding its repository and questions about
  it would go back to saying `COMPLETE` over a hole.
- `go/types` travels linked into the binary: Kivgraph only type-checks up to the
  language version of the toolchain that compiled it. A module registered above
  that ceiling is left out of `goworkspace.BuildPlan` and declared
  `MODULE_NOT_LOADED` naming repository, module and version, and the pass goes
  on (ADR 0147, amended); it is never allowed to make the synthetic `go.work`
  escalate the toolchain and break the load of every repository inside the
  standard library. The ceiling is `major.minor`.
- The synthetic `go.work` resolves a single build list for the modules it uses,
  so the plan is split into independent groups: two modules only share a
  workspace when one reaches the other through `require`, through `replace` or
  through a `go.work` of the indexed repository. A module that reaches no other
  is loaded in module mode, with no workspace, and the leftover synthetic files
  are removed together with their `.sum`. Indexing stays hermetic and
  `go.allow_network` is the only declared way out when a group's MVS selects a
  module absent from the local cache.

## The indexing pass

- **Every pass is a full rebuild.** There is no incremental path: `indexer.Full`
  is the only entry, `kivgraph index` prepares the current project and
  `kivgraph index --full` walks the configured registry; each pass publishes a
  new generation instead of mutating the current one. The delta
  -- `Update`, `Diff`, routing by invalidation plans, the semantic change
  classes and incremental canonical mutation -- was withdrawn in ADR 0057: nobody
  called it, its measured ceiling was `1.67x` over a `9.17 s` pass, and it
  published without running `integrity` or `golden probes`. What makes a
  reindex cheap is the fact cache, not a delta.
- The analysis of the pass is concurrent: every Go module and every TypeScript
  package is an independent unit. The merge follows the order of the units,
  never that of completion, so the published graph does not depend on how the
  work was scheduled -- two passes over the same corpus produce byte-identical
  facts, and that is what to verify when touching this area. The budgets differ
  by kind: `go.maximum_loads` bounds the Go loads because each one holds a
  complete type universe, and `typescript.maximum_workers` bounds the worker
  processes. The first failure cancels the rest.
- A Go module the loader cannot read does not bring down the pass: it publishes
  no facts -- they would not be trustworthy -- and is declared as
  `MODULE_NOT_LOADED` with the diagnostics observed. A repository whose
  dependencies nobody downloaded does not decide whether the others have a
  graph.
- Each kind of unit drains its own queue, the heaviest first, with as many
  workers as its budget allows and never more than it has units. The weight is
  an estimate over the files the unit will read, never a fact of the graph: a
  pass ends when its slowest unit ends.
- The facts of all the units are merged in a single pass (`facts.MergeAll`), not
  one unit against the accumulated result: a pairwise merge deduplicates, copies
  and reorders the whole graph again at every step, which is quadratic in its
  size. The identity used to deduplicate is a comparable tuple, not a string
  joined by separators: a string makes a name containing the separator
  ambiguous, and here deduplication is the only thing that decides whether two
  facts are the same.
- An indexing request is built in a single place, `indexing.OptionsFromConfig`:
  the caller only adds what the configuration does not decide -repositories,
  working directory, resolver version and the progress sinks-. Building it twice
  is how `index_project` ended up naming no Rust field and failing with "the Rust
  analyzer command is required" on the same configuration the CLI indexed without
  trouble. Its result reports the counters of the six languages: a language
  absent from the report reads as a language with no code.
- A TypeScript package name declared by several manifests is an ambiguity, not a
  broken repository: no manifest provides it, both leave the registry and
  `AMBIGUOUS_PACKAGE_PROVIDER` is declared. It is the same treatment a Go module
  with several providers gets.
- TypeScript indexing processes named `package.json` providers with a resolved
  `ProjectPath`; it passes that path to the worker on every invocation and omits
  manifests with no project instead of guessing the root's `tsconfig`.
- A path the TypeScript engine resolves arrives in its canonical spelling,
  lowercase when the filesystem folds case. It is corrected at the boundary with
  `enginePath` before indexing, comparing or emitting it; never with
  `realpath`, which would resolve the `node_modules` links and change the facts.

## Fact cache

- The fact cache (`indexing.fact_cache`) stores one entry per analysis unit with
  **the list of everything the unit read and its fingerprint**; serving it
  requires revalidating that entire list. An entry is never served to another
  analyzer: its identity is the content of the executable, the answer of
  `go env`, the content of the TypeScript worker, the build tags,
  `include_tests` and `go.allow_network`. A module the loader could not read is
  never stored, because its failure depends on the module cache and no
  fingerprint of the code describes it. See ADR 0030.
- The provenance of a repository -commit, branch, dirty- is not a fact any
  analysis produces: the pass stamps it when merging, from the registry it was
  given, and no unit writes it. Inside the cache entry it went stale on every
  hit, made the published commit depend on which unit failed, and aborted
  `verify` over a commit that touched no file.
- The lockfile is searched from the root of the registered repository upwards and
  the entire chain is recorded. `node_modules` is not walked -it is the
  lockfile's job-, so a lockfile that is not found leaves that dependency with no
  control at all: in a pnpm monorepo it lives above the registered repositories.
- An entry records the name of what has to be measured again, never the measured
  value: an entry whose name carries its own value is compared with itself and
  cannot invalidate anything.
- A package name that nobody provides today is a dependency with fingerprint
  `absent`, not the absence of a dependency. Without that an `UNRESOLVED` would
  be served forever and would never become the edge it is due.
- When touching the cache, verify with `fact_cache: verify`, which analyses
  everything and aborts the pass if an entry disagrees with the analysis.
  Comparing two passes with a cache proves nothing.

## Canonical graph and LadybugDB

- Code that uses the native library is compiled with the `ladybug` tag.
- The pair of LadybugDB and Go binding versions must come from the versioned pin
  and be verified through `scripts/fetch-ladybug.sh`.
- Validate the canonical graph before publishing it. An invalid generation can
  never become `CURRENT`.
- Do not read or serve MCP queries directly from LadybugDB when the contract
  requires the published HotSnapshot.
- Do not mix the experimental schema `001-synthetic` with the canonical `002`.
- The iterators `VisitRepositories`, `VisitPackages`, `VisitFiles`,
  `VisitSymbols`, `VisitEvidence` and `VisitEdges` deliver copies by value and
  expose no internal slices; their ranges are half-open and cancellable.
- `NewGraphSnapshot` copies every mutable input and that is its contract: the
  caller may keep mutating what it passed, which is what the constructor
  guards. The **reader** of a file does not need that and does not do it:
  `readSnapshot` calls the internal constructor ceding ownership, because its
  `decode*` functions allocated each slice from the mapped bytes one statement
  earlier and nobody else can name them. Copying them produced a verbatim twin
  and left the original as garbage -- `20.8 MB` on `workspace`, measured in
  `benchmarks/snapshot-heap`, with identical live bytes. What makes the cession
  safe is that **no decoder aliases the map**: all five use `make` and copy field
  by field. Whoever writes one that returns a view over `data` breaks this
  silently.
- The evidence of an edge travels to the HotSnapshot **without its position**:
  `hotsnapshot.EvidenceRecord` carries key, the two files, class and provenance,
  and the observed span stays in the canonical graph. So no tool can open the
  text that justifies an edge, in any of the six languages: a `find_references`
  row uses the symbol's range and its `evidence_kind` comes from the provenance.
  It is a declared hole, not a defect to fix in passing: projecting it raises the
  version of the snapshot's row format -- a compatibility surface-- and no
  consumer asks for it today. Whoever adds an `evidence_key` to a language must
  know that it completes the canonical contract and changes no answer.
- `CanonicalColumns` rebuilds the complete canonical schema on every call, so a
  table's columns are resolved once per table and never per row. The graph has
  one edge per reference in the corpus.
- The canonical scan reads in Arrow chunks, not value by value: a `Symbol` is
  sixteen cgo crossings per row and the graph has one row per declaration of the
  corpus. `scanCanonicalTuples` stays as the reference implementation and the
  test compares the two readers field by field -- a wrong pointer decoder
  produces a graph that looks correct, so the columnar path is not touched
  without that oracle.
- The engine writes a string `NULL` with offsets `0..0`, which are not
  monotonic: the data range of a column is the widest over its rows, never the
  one running from the start of the first to the end of the last.
- A read-only opening of the canonical graph receives a buffer pool proportional
  to that graph -twice its size, between 256 MiB and 2 GiB-, never the 80 % of
  system memory the engine gives by default: a scan reads each page once and
  closes. A write opening keeps the default, which is the cache a `COPY` of
  millions of rows needs.
- The catalogue in `canonical_integrity.go` is the list the native layer uses to
  validate an edge. A new provenance that does not enter there passes every test
  without the `ladybug` tag and breaks integrity at publish time; the guard that
  prevents it lives in `canonical_catalog_test.go`, with no tag.
- An incompatible schema upgrade must detect the version, back up and verify the
  active generation before rebuilding from source repositories. Only a candidate
  generation that passes integrity and validation may change `CURRENT`; a
  rollback must check the backup's digests.
- A version rollback must cover a valid restore and fail-closed rejection on a
  missing or divergent digest; if validation fails, `CURRENT` does not change.

## Generations, publication and snapshots

- `kivgraph ui`, and `kivgraph serve` **when it answers itself**, follow the
  published generation and republish when the `CURRENT` pointer advances,
  without coordinating with anybody -- `SnapshotStore.Publish` only accepts a
  strictly newer generation. An `index --full` in another terminal cannot leave
  a server serving a graph that no longer exists on disk. A `serve` acting as a
  relay opens no store, no follower and no resync: it has no graph to follow,
  and the one that answers is the daemon.
- Publishing a generation takes a `flock` on the store itself. The `Store`'s
  mutex only orders the goroutines of one process, and a state directory is
  shared by an `index --full`, a client's `index_project` and a server's
  resyncer: without it two passes trample each other. A `<id>.tmp` candidate that
  exists while the lock is held is garbage by construction -the kernel would
  keep holding it for its writer if it were alive- and is removed. Rejecting it
  turned a dead pass -an OOM, a closed terminal- into a store unusable forever,
  because every later attempt derives the same identifier and collides with it.
  The lock does not wait: a rebuild lasts minutes and blocking would look like a
  hang, so the loser gets `ErrPublishInProgress`. For the follower that is not a
  failure, it is exactly what the lock exists to produce.
- After a full `clean` the numbering goes back to `000001`, and
  `SnapshotStore.Publish` only accepts a strictly newer generation: a live
  server keeps the graph that no longer exists and will install no other. The
  follower declares it once and the command warns to restart.
- After publishing a snapshot `rebuild.ReturnBuildMemory` is called: it is the
  only moment the build's transient is dead -published, or discarded because
  another publisher won- and a server has nothing to do until the next request.
  It returns **both** halves: the Go arena with `debug.FreeOSMemory` and the
  native allocator with `nativeheap.Return`. The second is not a detail: the Go
  heap parks between 2.4 and 4 MB above the live snapshot on every build, so what
  made a server grow was not its own. `GOMEMLIMIT` is not set, because the
  pass's child would inherit it from the environment -`indexing/subprocess.go`
  does not set `command.Env`- and because on the Go side it would have no gap to
  close. See ADR 0044.
- The engine's memory is libc's, not Go's, and no `runtime` metric sees it. glibc
  gives one arena per competing thread and makes it grow before reusing
  anything, so every rebuild keeps another arena until they saturate. Measured on
  `devlabs` over a 189 MB graph, four builds: with only the Go scavenge the RSS
  goes from 309.5 to 511.1 MB -67 MB per build-, and returning the native one as
  well it stays at 241.5-249.6 MB, at no cost in time. Bounding the arenas from
  the process does not work and is measured: `mallopt` after startup does not
  move what already lives in secondary arenas.
- A generation carries its HotSnapshot in `snapshot.kvsnap`, and a server reads
  it instead of deriving it from the canonical graph: `rebuild.LoadOrBuildSnapshot`
  is the door, and the three paths that install a generation -`serve`'s startup,
  the follower and the parent of a pass- go through it. What proves the file
  belongs to that generation is `snapshot.content.sha256`, the digest of the
  graph the file contains, repeated in its header. **It is not
  `snapshot.sha256`**, which digests the counters per table and therefore does
  not tell two graphs of the same shape apart: measured on `workspace`, two
  indexings whose graphs differed in 288 rows gave a byte-identical
  `snapshot.sha256`, so a file derived from one was accepted as the other's. The
  counters digest keeps its job, which is `Rollback`'s cheap check. No pass
  mutates a generation in place, so a digest that does not match means the file
  is foreign, stale or corrupt. Writing it is an economy and never a
  precondition -- an absent, foreign, stale or corrupt file costs a derivation,
  is declared in the report and is never an answer. A generation published before
  the record existed carries none: that is `ErrNoRecordedGraphDigest`, of the same
  class as the absence, and counting it as a failure would turn `doctor` red on
  every installation when updating. Measured on `devlabs`: 253-255 MB of RSS and
  787-836 MB of peak deriving, against 150-152 MB and 264-269 MB reading. See ADR
  0045 and ADR 0061.
- Position tokens use the canonical graph digest and generation marker on
  both the mapped and derived paths, never the header of a refused cache.
  See ADR 0164.
- `doctor` asks **both** questions, and they are different. `snapshot` derives
  the snapshot from the canonical graph and never reads the file: it reports
  whether **this graph** can still become a snapshot, and reading a file written
  when the graph was healthy would answer another question, and would answer it
  reassuringly. `snapshot.published` is the other half: it says whether the file
  can be used, or why a server would have to derive. Without it, a server that
  derives the whole graph on every startup is indistinguishable from one that
  reads it, and that cost twice while this was being built.
- **Two servers over the same generation share the file.** Measured with two
  `serve` over `workspace` -- `123,531` symbols, `98,773,720` bytes: `94 MB` of
  clean `mapped file`, a single copy, and `44.5 MB` dirty per process. With four
  clients it is `272 MB` against the `692 MB` of the unshared baseline, a
  `39.3 %`. What stays private are the decoded tables; what is shared is the
  string arena, which is the large part. `internal/procstat` splits `Pss` and
  `Shared_Clean` on Linux and **declares** that darwin cannot: there the split
  comes from `footprint`, which is not the same quantity, so the two figures are
  not compared with each other. On Linux, measured by `benchmarks/shared-snapshot`
  over `161,819` symbols: the `Pss` share of the mapped arm over the derived one
  is `0.498` with two servers, `0.416` with four and `0.372` with eight --
  **it falls at every step, which is the property**, because the shared part is
  divided among more processes. What is private comes out flat at `614 B` per
  symbol, `~95 MB` per process in that corpus.

  The two original thresholds of `LUQUE-2006` were **not** met -- `0.416` against
  `≤0.40` and `95 MB` against `≤60 MB`--, and what the measurement showed is that
  the problem was the thresholds: one was a property of the number of clients and
  the other a number from a smaller corpus. They are rewritten as properties.
  Mapping the tables too is still asked for by no figure: what it would save is
  those `614 B` per symbol, and startup -- `13x`-- is already given by mapping
  the arena.
- The published file is **mapped and the mapping is kept**, because the string
  table reads its values in place instead of copying them -- some fifty megabytes
  on the real corpus. The other tables are decoded to structs and are copied.
  That means the mapping has to live as long as the snapshot **and as any table
  taken from it**: `Strings()` and `StableKeys()` deliver the table by value and a
  reader holds it without holding the snapshot. Its release is tied to the
  **unreachability** of an anchor (`mapping`) that the snapshot and its two
  tables point to, with `runtime.AddCleanup` inside `hotsnapshot.MapSnapshot`,
  never to a public `Close`: `Publish` leaves the previous snapshot as garbage
  while somebody reads it, and unmapping there gives not an error but a `SIGSEGV`
  or strings read from foreign pages.
  What makes all this safe and **has to stay true**: a table with a borrowed
  arena **copies every string it delivers**, so no response can outlive the
  pages it was read from. There are two guards, and both fail if the copy is
  removed: one compares **addresses** -- comparing the bytes would pass
  anyway--, and the other releases the mapping by hand with a reader holding what
  it read, and reports `SIGSEGV` instead of `ok`. A third test, the one that
  deletes the file and walks the whole graph, covers that a mapping survives its
  path.
- The string table resolves `Lookup` by binary search over the ids sorted by
  their value, not with a map: it was 20.45 MB per process against 1.9 MB, and
  `Lookup` is called once or twice per query. The order is carried by the file in
  a section of its own -it is derived and deterministic, so the writer sorts
  once-, is optional in both directions, and is validated by requiring strictly
  increasing values: it is the only section whose corruption would be silent,
  because a valid order of another table still answers with the id of another
  value.
- The viewer's layout is a projection derived from the `HotSnapshot`: it uses
  deterministic integer coordinates, repository/package/file/symbol containment
  and a spatial grid with bounded overflow; it never mutates the snapshot or
  runs a global force simulation.
- The layout balances each grid by default (`Columns: 0`) so that the published
  world ends up roughly square; a fixed width turns it into an unreadable strip
  when the number of repositories grows.
- The viewer's binary `LGVB` payload is versioned (`v2`), little-endian and
  limited to `32 MiB`; it carries a labels section with the name of each node,
  the server validates magic, version, offsets, lengths and `snapshot_id` before
  serving it, and the incompatibility errors are stable codes.

## Configuration and registry

- The vocabulary of languages is `config.SupportedLanguages` and it is validated
  when the registry is written, not only when indexing: `init` does not accept
  what the pass rejects.
- A configuration setting is worth exactly what the code implements.
  `indexing.generated_files` only accepts `include` and
  `indexing.unresolved_references` only `retain`, because that is what the pass
  does: accepting another word promises a behaviour that does not exist and
  turns a typo into a configuration silently different from the one that was
  read.
- A configuration written outside the default location is self-contained: its
  state, its cache and its registry hang from its own directory. A `--config` in
  `/tmp` that pointed at the real state would publish generations over the graph
  it came to leave alone.
- Repository names are compared exactly. The name is an identifier, never a path
  component, and the stable keys that carry it distinguish case: two
  repositories that differ only in case are two repositories and must be
  registrable under their real name.
- Every new state path is relocated in `stateBesideConfig`, not just added to
  `DefaultConfig`. It is what makes the rule above self-contained: a path
  forgotten there sends its content to the real installation from a test under
  `/tmp`. `logging.event_log_path` is the last one that entered.

## HTTP servers, assets and processes

- `kivgraph ui` is opt-in and serves only the published `HotSnapshot` over
  read-only HTTP. Its default bind is `127.0.0.1:7777` (ADR 0119, which revises
  0032) and a loopback bind rejects with `403` a `Host` that is not this machine,
  against DNS rebinding. Viewing it from another machine is a decision (`--addr`
  or `web.address`); a configuration that an earlier `init` wrote with
  `0.0.0.0:7777` keeps it. The guard of an exposed bind is the warning, and that
  is why it is not decorative: every bind that is not loopback logs what is
  exposed -repository and file paths, symbol names and signatures- and with what
  it is closed. The endpoint carries no authentication; restricting it is
  `--addr` or `web.address`.
- `kivgraph serve` **listens on no port**, and that is the part that does not
  change: `webapi.Run` owns the only listener and runs a bounded graceful
  shutdown when the context is cancelled. What it does since ADR 0084 is
  **connect** as a client to the daemon when there is one: its entry is still
  STDIO -- the `.mcpb` manifest has no field for a url and a `url` entry in a
  committed file would carry the token--, but the server that answers becomes the
  daemon. With no reachable daemon it answers itself, as before, and
  `KIVGRAPH_SERVE_IN_PROCESS` forces that path. The relay lives in
  `internal/relay` and not in `internal/mcp`, which is where the ADR expected it:
  `internal/daemon` imports `internal/mcp`, so a relay that needs an `Endpoint`
  does not fit there without a cycle.
- `internal/webassets` serves only the generated copy of `web/dist` when the
  distribution is built with the `webassets` tag; binaries without the tag
  return a visible `503` fallback instead of serving undeclared files.
- Process enumeration lives in `internal/procstat`, the only place that reads
  operating-system process statistics, with one file per platform: `/proc` on
  Linux and `kern.proc.all` plus `kern.procargs2` on macOS. Only this user's
  processes are seen, which are exactly the ones that could be signalled; a
  platform that cannot enumerate returns an error, not an empty list, and `0`
  means unknown.

- `internal/eventlog` is the durable record of what happened: a pass, a tool
  call, a server's lifecycle. It is an append-only JSON-lines file, and that
  shape is what makes it safe for the several processes that write it at once: a
  record is a single write of much less than a pipe buffer on an `O_APPEND`
  descriptor, which POSIX keeps whole. There is no lock between processes and
  nothing is read before writing; adding either would buy nothing. See ADR 0049.
- **It is not an API.** It is derived state and carries no schema version:
  deleting it loses history and nothing else. A reader skips a line that does
  not parse instead of failing the read, because the file is written by several
  processes and one may be of a version that knows fields another does not.
- A sink that cannot be opened **degrades with a warning**, never fails the work
  it describes. A nil `*eventlog.Writer` discards, so no producer carries a
  branch for that case; and `Append` returns no error for the same reason.
- Per-tool measurement already existed and stays where it was: `tools.observe`
  is the only point the nine query tools pass through, and `metrics.Registry`
  accumulates its atomics so that `graph_status` reads them back. What was added
  is a second side channel, `metrics.QueryRecorder`, symmetric with the
  OpenTelemetry bridge: the registry cannot be the source of a command because it
  dies with the process. `QueryRecorder` runs inside the hot path of a call, so
  it cannot block.
- `index_project` receives variadic observers like every `Register*` of
  `internal/mcp/tools`. It got them late, and while it did not have them it was
  the only call a client could make that no counter saw -- precisely the one
  that costs minutes.

## Platform

- On macOS, LadybugDB's lock inspection never opens the database: POSIX releases
  all of a process's record locks when any descriptor on the file is closed, so
  an `fcntl(F_GETLK)` would unlock the engine. Descriptors are enumerated with
  `libproc`.
- The watcher declares its contract per backend. Under `kqueue` a file created
  and written at once arrives as a single `Create`, and the backend consumes one
  descriptor per watched file: a tree that exceeds the limit fails in `New`
  naming the limit, and never silently watches a subset.
- The policy that rejects paths with symlink components is not relaxed to
  accommodate macOS's `/var` and `/tmp`. The tests that feed the workspace layer
  use `internal/testsupport.TempDir`.

## Client integrations

- `kivgraph mcp install` and `kivgraph skill install` detect known local roots
  when `--target` is missing, show an interactive selection of one or several
  clients and keep `--target` for automation; they never initialize Kivgraph or
  index repositories.
- The interactive selection uses Bubble Tea and Lip Gloss: `↑`/`↓` or `j`/`k`
  move, `space` toggles, `a`/`n` select all/none, `Enter` confirms and `q`/`Esc`
  cancels; it honours `NO_COLOR` and emits no ANSI when output is redirected.
- The external adapters validate JSON/TOML before modifying it, reject symlink
  destinations, write atomically with `0600` and require `--force` to replace or
  remove an incompatible entry. Idempotent entries do not rewrite the file.
- The canonical skill lives in
  `internal/integrations/assets/kivgraph/SKILL.md`; bundles must include it
  under `skills/kivgraph/SKILL.md` and in `SHA256SUMS`.

## Verification

```bash
gofmt -l <modified-go-files>
go vet ./...
go test ./...
```

If the change touches native LadybugDB, `make test-ladybug`; `PKGS` bounds the
pass to one package. The full procedure is in the skill
`.claude/skills/running-tests/SKILL.md`.
