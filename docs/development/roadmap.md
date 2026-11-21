# Kivgraph: improvement plan (rev. 2, 2026-11-15)

This is the working note the plan was run from, kept here as a record. It was
written in Spanish outside the repository while the work was done in a
separate development repository (PR #32 to #100 there), and translated when
that work moved to a fork of the official project. It is a log, not a
specification: it
was appended to as each lot closed, so an early section can describe a state
a later one changed, and where two disagree the later one holds. The status
paragraph below and "Migration to the fork" at the end of phase 6 are the
current state. The private repositories used as a measurement corpus are not
named. The ADRs and the code are the reference for behavior.

ADRs 0118, 0126 and 0127 describe the fork's own distribution and exist only
there, and the pull request numbers are the fork's unless a repository is
named.

Base when the plan was written: `main` at `200c6db`, ADRs up to 0124.
Upstream at that time: the development repository. `Luqueee/kivgraph` was
then no
longer a reference: nothing was proposed there and its releases were not
pulled in. That rule ended on 2026-11-17, see "Pull request to the original
project" at the end of phase 6.

## Goal

1. What already exists must not give false answers, hang or lose data.
2. An agent with Kivgraph must solve complete tasks better or more cheaply
   than without it, and that must be demonstrated. Today it does not happen:
   `benchmarks/agent-e2e/report.md` gives +15% cost, +20% time and 4/12
   exact against 5/12 without the graph.

## Rules for all phases

- Each lot is one `type/kebab-description` branch and one PR that works on
  its own. Before it is executed, its detailed implementation plan is written.
- No change exceeds 800 lines (500 if it is complex logic).
- Each bug starts with a test that fails on `main` and passes with the fix.
  The phase 2 findings come from reading code: only three were re-read by
  the author and none is reproduced. The failing test is the confirmation; if
  it does not fail, the finding is discarded and noted.
- An `EXACT` edge never comes from a name, text, path or single candidate.
- What was valid yesterday does not fail today: retired keys are accepted,
  ignored and named.
- Delivered text (docs, ADRs, commits, PRs) is in English; a file in Spanish
  stays in Spanish.
- Gates per PR: `gofmt -l`, `go vet ./...`, `go test ./...`, `make build`;
  `scripts/check-docs.sh` if it touches `docs/`; `make test-ladybug` and
  `make lint-ladybug` if it touches LadybugDB.
- Accepted ADRs stop being an external limit: since the upstream is our own,
  revising them is a local decision that is documented with a new ADR.


Status (2026-11-17): phases 0, 1, 3, 5 (analyzed) and 7 done; 2 (29 of 31
closed or evaluated; D3 and D6 untouched), 4 (partial) and 6 (PRs #50 to #99,
ADRs 0121 to 0125). Stable release `v0.10.0` published. Pending, blocked on
the "Checklist lot": D3, D6, T1, T3, the `signature` decision and four
phase 6 candidates; phase 8 has no demand. Closed by decision of the
maintainer: 1d (the module path stays) and the report to `typescript-go`
(dropped).

## Phase 0. Documentation that lies (1 PR, `docs/`)

- `README.md:94` and `landing/src/content`: list the seven languages (Java
  and C# are missing).
- `README.md:85-87` and `landing/.../mcp/usage.md:603-628`: `mcp-token-cost`
  figures brought to the current report (`1,76x`, `1,24x`, floor `2,50x`).
- `docs/development/storage-benchmarks.md:78`: remove the disk-full `FAIL`;
  leave it written that the tested case is a late `ENOSPC` at close.
- ADR 0057 (appendix) and `benchmarks/edit-frequency/report.md:221-240`: the
  watcher already has a caller (`cmd/kivgraph/main.go:754`, ADR 0099).
- `benchmarks/agent-e2e/README.md`: name the six-arm pilot and the asymmetry
  of the Graphify arm (`arms.go:34`).

Done when: `scripts/check-docs.sh` passes and each figure matches its
`report.md`.

## Phase 1. The upstream becomes the development repository

Status: 470 files name `Luqueee`. The binary updates itself from there
(`internal/update/update.go:34`), and the development repository is private
and has
no release at all (it has the tags; Actions is enabled).

Done on 2026-11-15 (PRs #32 and #34): distribution repointed to
the development repository, telemetry removed (ADR 0118), MCP registry not
published, release `v0.10.0-dev.1` (prerelease, `requires_reindex`). Checked
on macOS arm64 with a temporary `HOME`: `version` and `update` correct;
`index --full` and `doctor` not. Linux and Windows only through the
workflow's smoke jobs.
The stable release `v0.10.0` was made on 2026-11-17 (`releases/latest` exists
and the README's versionless one-liner works). 1d closed without being done
(2026-11-17): the module path stays as it is.

- 1a. Distribution (what breaks if it is not changed): `Repository` in
  `internal/update/update.go:34`; `scripts/install.sh`, `install.ps1`,
  `downloads.sh`, `build-bundle.sh`, `build-mcpb.sh`,
  `build-server-json.sh`; `server.json` and
  `internal/version/registry_test.go` (MCP registry name
  `io.github.Luqueee/kivgraph`, which is a published identity: changing it
  is a new server in the registry); `.github/workflows/release.yml` and
  `download-metrics.yml`; `.claude/skills/publishing-releases/SKILL.md`.
- 1b. Links: `README.md`, `docs/installation.md`,
  `docs/development/mcp-registry.md`, `landing/src/site.mjs`, components and
  pages of `landing/`. The notes of already published releases are not
  rewritten.
- 1c. First release of our own: new tag, release workflow, and check that
  `install.sh` and `kivgraph update` find it with a temporary `HOME`.
- 1d. Go module path `github.com/Luqueee/kivgraph`: 367 `.go` files plus the
  `BUILD.bazel` files. It is mechanical and does not change behavior; it goes
  in a PR of its own, alone, or it does not go. It does not touch the
  `luque-stable-key` namespace or the `LUQUE-####` identifiers, which are not
  renamed.
- Compatibility: an already installed binary will keep looking for updates
  in `Luqueee/kivgraph`; there is no way to redirect it from here.

Done when: a clean installation from the development repository passes `init`,
`doctor`, `index --full` and `update` reports "up to date".

## Phase 2. Bugs found (4 PRs, one per lot)

Confidence: C = traced in code end to end; S = suspicion, what is indicated
is missing. Size in lines of fix, without tests.

Merged into `main` on 2026-11-15 (id, PR, evidence):

- **A1** (PR #2): reproduced: panic `slice bounds out of range [7:4]` on
  `main`
- **B1** (PR #3): reproduced: `Materialise` does not return within 30 s on
  `main`
- **C2** (PR #4): reproduced: the pruning deletes every old file in the
  directory
- **A8** (PR #5): keys retired by the ADR 0062 route; ADR 0117
- **C3** (PR #6): warning in `rollback`; the follower still does not accept
  an earlier generation
- **CI** (PRs #7, #8): Go `1.26.11`; `verify` job pinned to `ubuntu-24.04`
- **A2** (PR #9): reproduced: `verdict = "COMPLETE", guidance = ""` with the
  traversal cut short
- **C1** (PR #10): reproduced: the four operations return `nil` with the
  lock taken; includes the `clean --keep-active` suspicion
- **D1** (PR #11): reproduced in a test and with the binary: the second
  `daemon` exits with `1` and touches neither `daemon.json` nor `daemon.port`
- **B3** (PR #12): only the replacement of watchers is tested; the full path
  with a real daemon is not
- **B2** (PR #13): reproduced: the old name of a rename survives and the new
  directory is missing; in addition, the overlay's `git status` no longer
  rewrites `.git/index`
- **A3** (PR #14): reproduced: `err=<nil>, bodies=1` with a nonexistent
  symbol
- **A4** (PR #15): reproduced in the four tools
- **C5** (PR #16): reproduced: `remove previous bundle: unlinkat ...` with
  the bundle already installed. Part (a) only
- **B7** (PR #17): reproduced: observing the repository rewrites
  `.git/index`. Goes from suspicion to confirmed
- **B6** (PR #18): reproduced: `src/vendor/...` ignored. `vendor` is only
  ignored next to `go.mod`, `Cargo.toml` or `composer.json`
- **B5** (PR #19): reproduced with the two state transitions in the order of
  the race; the real race with a pass is not
- **C7** (PR #20): reproduced in `Prune`, `Discard` and `DiscardExcept`
- **B4** (PR #21): reproduced end to end: `WatchSources` stopped because of
  one unreadable file
- **A6** (PR #22): reproduced: `reached = 4` with two dependencies
- **A7** (PR #23): reproduced in three cases
- **A5** (PR #26): reproduced: `total = 0` blaming the filter; the symbol of
  the rare term outside 4000 rows
- **C4** (PR #27): reproduced: the pass following the serving of an old
  entry gives `Hits:0 Misses:1`. Half (a) only
- **D4** (PR #28): reproduced in five cases and with real commands through
  the classifier
- **D7** (PR #29): reproduced in three cases with two writers on the same
  path

No open PRs for phase 2. 24 of the 31 bugs are closed.

Parked by decision of the maintainer (2026-11-15):

- D3 (HTTP sessions of the daemon): the SDK has `SessionTimeout`, but its
  clock only pauses during a `POST`; an open stream does not count. A client
  connected and idle for longer than the timeout would lose the session, and
  it is not known whether the relay or the real clients restart on their own
  after the `404`. Options: (1) a long timeout, of days; (2) the server's
  `KeepAlive`, which closes the session if the client does not answer a
  ping; (3) leave it. The first two have to be tested with the relay and
  with a real client beforehand.

Discarded with a reason:

- C4, half (b), accumulation of old entries of an edited unit: an entry is
  addressed by content (ADR 0100) so that going back to an earlier content
  is a hit; deleting the earlier ones on commit would evict exactly those.
  A size cap would be new behavior.

Still open from what is already done:

- C1: `make test-ladybug` was not run locally; the CI job covers it.
- D1: a second start in the instant between publishing HTTP and binding the
  socket is not rejected.
- B3: `refreshInvalidationForProfile` keeps using the configuration the
  watcher started with until the next registration. No end-to-end test.
- C5: the bundle swap is still not safe against an interruption between the
  two `rename` calls nor against two `update` runs at once.
- B4: an unreadable directory still makes the reconciler's sweep fail;
  inotify's `ENOSPC` is still not mapped; a watcher that does not start for
  another reason is still not retried.
- B6: a Ruby `vendor/bundle`, or any vendored tree without one of the three
  manifests next to it, becomes watched.
- A5: the two responses captured with `total: 4000` on the tool's page were
  not recaptured; the original corpus is needed.
- D7: two processes that decide to rotate at the same instant can still
  rotate twice; there is no lock between processes by design.

Pending from phase 2 (7): C6 and C8 are unconfirmed suspicions; D2 needs
Linux with systemd; D6, Windows; D8, Bun; D5 and D3 need a real agent or
client session.

ADRs 0117 (A8), 0118 (distribution without telemetry) and 0119 (viewer on
loopback) are already used.

Tooling warning: the editor of this session reformats `.go` files on save
with a style that is not that of the `gofmt` of the `go.mod`. Formatting has
to be done with that `gofmt`, and the diff has to be checked so that it does
not bring in unrelated hunks.

Known flaky tests, unrelated to these changes:

- `TestBatcherFlushesAtMaximumWhileEventsContinue` (`internal/watcher`):
  fails sometimes on macOS because of timing.
- `TestGetIndexStatusReportsProgressAndFailure` (`internal/mcp`): failed once
  in the CI of #28 and fails locally on `main` when repeated 300 times; it
  reads the progress before the job publishes it.

`MODULE.bazel.lock` still names Go `1.26.6`; it is regenerated with Bazel.

### Lot A. False answers and crashes in the tools (`fix/mcp-`)

- **A1** (confidence: C, re-read; lines: 5): `get_source` returns the code
  of another repository, with `fresh: true`, and can panic. Where:
  `internal/mcp/tools/get_source.go:367-370`, `:506-515`. Trigger: one call
  with symbols from two repos that share a relative path (`main.go`,
  `src/index.ts`); panic if the line falls outside the cached file.
- **A2** (confidence: C; lines: 10): `COMPLETE` and `truncated: false` when
  `max_nodes` cut the traversal. Where: `blast_radius.go:656` versus
  `:712-716`; `trace_dependencies.go:744-753`. Trigger: a central symbol
  with more than 5000 reached.
- **A3** (confidence: C; lines: 12): multi-profile `get_source` omits,
  without an error, the symbols it does not resolve. Where: `get_source.go`,
  `getSourceAcrossProfiles`. Trigger: `profile: ["*"]` with a misspelled
  name.
- **A4** (confidence: C; lines: 8): the cursor does not include
  `include_derived`: rows skipped or repeated. Where:
  `find_references.go:377-390` and the other three query structs. Trigger:
  changing the flag between pages.
- **A5** (confidence: C in code, impact not measured; lines: 25):
  `find_by_intent` spends the cap of 4000 candidates before applying `repo`,
  `kind` and `path_prefix`, and says that the filter excluded them. Where:
  `find_by_intent.go:25`, `:602-608`. Trigger: a word that matches more than
  4000 symbols.
- **A6** (confidence: C; lines: 4): `reached` counts the members of the root
  container; a `max_nodes` smaller than them gives `INVALID_ARGUMENT`.
  Where: `trace_dependencies.go:752`. Trigger: a class or struct root with
  methods.
- **A7** (confidence: C; lines: 6): the selector's `path` is a string
  prefix, not a file. Where: `root_symbol.go`, `hotsnapshot/search.go`.
  Trigger: `src/util.ts` next to `src/util.tsx`.
- **A8** (confidence: C; lines: 40): `mcp.default_limit`, `maximum_limit`,
  `maximum_depth` and `maximum_visited_nodes` are validated and documented
  and nothing reads them. Where: `internal/config/config.go:1339-1353`.
  Trigger: an operator limits a shared daemon and limits nothing.

A8 requires a choice: wire them up or retire them as ADR 0062 did.
Recommended: retire them, which is the precedent.

### Lot B. Indexing and watching (`fix/indexing-`)

- **B1** (confidence: C, re-read; lines: 6): Java/C# indexing hangs forever
  if the extraction fails. Where:
  `internal/scratchtree/scratchtree.go:147-150`. Trigger: an `extractTar`
  error past ~64 KiB: a path with `:`, a case collision on APFS, a full
  disk.
- **B2** (confidence: C; lines: 20): the Java/C# temporary tree is not the
  working tree. Where: `scratchtree.go:343-381`. Trigger: a staged `git mv`,
  a new untracked directory, an edit under a package named `build` or `bin`.
- **B3** (confidence: C; lines: 30): repos and profiles registered after
  startup are never watched. Where: `cmd/kivgraph/main.go:742-750`,
  `invalidation.go:284-286`. Trigger: `index_project` against a running
  daemon.
- **B4** (confidence: C, narrow trigger; lines: 30): one unreadable path
  discards the whole batch of changes; a failure when starting the watcher
  is definitive. Where: `internal/watcher/hash.go:121`, `:219-244`;
  `sourcewatch.go:94`. Trigger: `EACCES` or `ENOTDIR` after indexing; the
  inotify limit.
- **B5** (confidence: S, window not reproduced; lines: 10): an edit during
  the end of the pass is marked and then erased: the graph stays old and the
  state says fresh. Where: `internal/invalidation/state.go:196`. Trigger:
  saving between `VerifySources` and `RecordPublished`.
- **B6** (confidence: C; lines: 5): the watcher ignores `vendor/` in every
  language. Where: `internal/watcher/ignore.go:10-17`. Trigger:
  `src/vendor/` in TypeScript, Python, Java, C# or Dart.
- **B7** (confidence: S, standard git behavior, not reproduced; lines: 2):
  `git status` without `GIT_OPTIONAL_LOCKS=0` can make a user's `git add`
  fail. Where: `internal/workspace/registry.go:324-341`. Trigger: an
  observation concurrent with a git command.

### Lot C. Storage and update (`fix/storage-`)

- **C1** (confidence: C; lines: 20): a `rollback` during a rebuild is undone
  silently after reporting `PASS`. Where:
  `internal/storage/generation/store.go:160-213`; `backup.go:47`, `:94`,
  `:146`. Trigger: `Restore`, `Prune` and `Discard` do not take
  `publish.lock`.
- **C2** (confidence: C, re-read; lines: 6): the pruning of the fact cache
  deletes any file older than 30 days in the directory. Where:
  `internal/indexer/factcache.go:511-522`. Trigger:
  `indexing.fact_cache_path` pointing to a non-dedicated directory.
- **C3** (confidence: C; lines: 5 to 40): a `rollback` does not reach the
  running servers. Where: `internal/hotsnapshot/publication.go:437-439`;
  `indexing/follow.go:139-141`. Trigger: `rollback` with `serve`, `ui` or
  the daemon active: they keep serving the new generation.
- **C4** (confidence: C for the mechanism, size not measured; lines: 25):
  the cache expires by write date, not by use date, and accumulates old
  versions without a cap. Where: `factcache.go:511-516`, `:494-497`.
  Trigger: a unit stable at 30 days; frequent edits.
- **C5** (confidence: C; lines: 21): `update` reports a failure after
  installing correctly; the swap does not survive an interruption nor two
  updates at once. Where: `internal/update/update.go:785-823`. Trigger: a
  subdirectory of the old bundle that cannot be deleted; a kill between the
  two `rename` calls.
- **C6** (confidence: S, no call site identified that loses the reference;
  lines: 10 to 20): possible `SIGSEGV`: the snapshot mapping is freed by GC
  reachability while there are tables handed out by value. Where:
  `internal/rebuild/snapshot_file.go:138`; `hotsnapshot/snapshot.go:331`,
  `:431`. Trigger: a publication during a query.
- **C7** (confidence: C; lines: 10): the `<id>.tmp` files of a killed
  rebuild are never cleaned up. Where: `backup.go:260-280`. Trigger: a
  process killed midway; `clean` says it deleted and the disk usage does not
  go down.
- **C8** (confidence: S, not tested on a real system; lines: 15): the space
  reservation has no alternative to `fallocate`. Where:
  `platform_linux.go:33`, `:16-17`. Trigger: state on NFS, 9p or FUSE.

C3 requires a choice: that the follower accepts an earlier generation (40
lines) or that `rollback` says a restart is needed (5). Recommended: the
latter for now.

### Lot D. Daemon, gate and installers (`fix/daemon-`)

- **D1** (confidence: C; lines: 20): a rejected second `kivgraph daemon`
  rewrites `daemon.port` and deletes the `daemon.json` of the one that
  works. Where: `internal/daemon/http.go:310-346`, `:241`;
  `cmd/kivgraph/daemon.go:77-88`. Trigger: starting it by hand, or
  `daemon install` with one running.
- **D2** (confidence: C; lines: not estimated): `daemon install` over an
  active unit on Linux leaves the old process serving. Where:
  `internal/supervisor` (Linux path). Trigger: reinstalling after changing
  `PATH`, `--addr` or the binary.
- **D3** (confidence: C for the mechanism, slow growth; lines: not
  estimated): the daemon's HTTP sessions do not expire. Where:
  `internal/daemon/http.go`, `mcpHandler`. Trigger: a client that exits
  without `DELETE`.
- **D4** (confidence: C; lines: not estimated): the gate denies a `grep` for
  a homonymous word even though it searches files that are not code. Where:
  `internal/agenthook/gate.go`, `Question.Paths` without a reader. Trigger:
  `grep -n version package.json`.
- **D5** (confidence: C in code, client behavior not executed; lines: not
  estimated): the `general-purpose` and `plan` subagents are always denied,
  and the escape that is printed cannot be applied. Where:
  `internal/agenthook/gate.go:250`. Trigger: any `Task` of that type in a
  registered repo.
- **D6** (confidence: S, it still has to be run on Windows; lines: not
  estimated): `skill install` on Windows creates a symlink and first deletes
  the existing skill. Where: `internal/integrations/skilllink.go`. Trigger:
  a user without the symlink privilege.
- **D7** (confidence: C; lines: not estimated): the rotation of the event
  log is not safe across processes: lost events. Where:
  `internal/eventlog/eventlog.go`. Trigger: the daemon and relays with the
  log open during a rotation.
- **D8** (confidence: S, it still has to be run on Bun; lines: not
  estimated): the OpenCode plugin does not handle the child's `stdin` error.
  Where: JS asset of `internal/integrations`. Trigger: arguments larger than
  the pipe buffer with the gate disabled.

D4 and D5 touch behavior that ADR 0077 fixes in part; review the ADR before
deciding how much is opened up.

### Untraced leads

- `internal/retrieval/split.go`, `AppendSpans`: byte arithmetic that assumes
  one-byte runes.
- `config.Initialize` (~`config.go:855`): checks `err` instead of `statErr`
  after `os.Stat`.
- Java/C#: `CombinedOutput` without `WaitDelay` and `RunDetached` without a
  process group; orphaned analyzers and `<state>/trees/tree-*` not swept.

### Order within the phase

A1, B1 and C2 first: false answer, hang and deletion of unrelated files.
Then C1, B3, D1 and A2. The rest by lot.

## Phase 3. Viewer safe by default (`fix/viewer-loopback-default`)

`internal/config/config.go:569` sets `0.0.0.0:7777` without authentication;
`internal/webapi/handler.go:722-740` does not validate `Host`.

- ADR 0117 that revises 0032: default `127.0.0.1:7777`; exposing requires an
  explicit `--addr` or `web.address`, like the daemon (ADRs 0066, 0069).
- Validate `Host` against the listen address and the loopback names; `403`.
- An already written `web.address: 0.0.0.0:7777` remains valid and keeps
  warning.
- Negatives first: foreign `Host` with and without `Origin`; a different
  port; an explicit non-loopback bind. Update `config_test.go:61-62` and
  `main_test.go:625-627`, and the six pages that document the default.

Done when: `kivgraph ui` without flags does not respond from another machine
and `Host: evil.example` receives `403`.

Done: PR #35, ADR 0119. Default `127.0.0.1:7777` and `403` for a foreign
`Host` on a loopback bind. There is no authentication. A configuration with
an explicit `0.0.0.0:7777` keeps its behavior. `kivgraph ui` not tested end
to end: a normal `go build` does not carry the web bundle.

## Phase 4. Measure properly before building (`bench/agent-e2e-self-repo`)

Status: partly done, PR #48 (2026-11-15). The original corpus is private and
is not available, so the six-arm sweep was not repeated. A different
measurement was made: 12 fixes of this repo's own (Go), `cold` versus
`kivgraph`, 2 repetitions, 48 runs, `$37,30`. Report and findings in
`benchmarks/agent-e2e/self-repo/`.

Result: `kivgraph` `-18 %` cost per run (9 of 12 tasks cheaper), `-5,3`
calls, `-42 s`, same recall (`0,63` versus `0,65`), exact 4 versus 5 of 24.
Without the two most favorable tasks it drops to `-10,9 %`. Per-task sign
test `p = 0,146`: the direction is consistent, the size is not established.
The agent made about 2 calls to the graph per run and none to
`get_blast_radius` or `trace_dependencies`.

Still not done:

- The 6 tasks of `tasks-locate.json` (private corpus).
- Review the 12 prompts by hand (T1) and see why each pair loses one file
  out of two (T3).
- Tasks that are neither Go nor from this repo; more repetitions.
- Tokens of graph responses versus later reads, per run.

Gate: the graph does not lose in accuracy and costs less, so the product is
not stopped. The improvement is in how it gets there (fewer reads), not in
hitting more files: that is what phase 5 has to move.

## Phase 5. Make the graph subtract reads (depends on phase 4)

Adjust responses, guidance, skill and `internal/mcp/instructions.go`
according to what phase 4 points out. No new tool: ceiling of `1900` and
`2400` bytes.

Status (2026-11-15): the 24 `kivgraph` transcripts analyzed. The gate ("cost
less than or equal to `cold` with accuracy not lower") is already met on
this corpus: `-18 %`, same recall. Findings, in `FINDINGS.md` (P3, S5, S6):

- 71% of the graph's response bytes was a constant block of
  `invisible_scopes`; 19 of 20 rows were the synthetic main `<pkg>.test`
  with `include_tests: true`. Fixed in `internal/goloader` (PR #49). On this
  corpus it does not reduce the response (13 real `testdata` scopes remain),
  so it does not move the benchmark; it corrects the verdict for repos
  without fixtures.
- Whole-file reads are 65% of the `Read` bytes (`get_source` was used once).
  It is a lever of 2-3% of tokens, below the noise (21-27%): it cannot be
  demonstrated with this benchmark and only 8 bytes fit in the `serve`
  instructions. Untouched.
- The measured saving comes from fewer turns (`-5,3` calls), not from
  shorter responses.

Pending to consider the phase closed: decide whether another measurement
with more tasks and repetitions is needed before touching the reading
guidance.

Done when: cost less than or equal to `cold` with accuracy not lower.
## Phase 6. Cheaper publication (`perf/`, several PRs)

`14,191 s` of the `17,150 s` after an edit are publication.

6a done (PR #50, `benchmarks/publication-profile/`): a Go repo of `12.465`
symbols, pass after an edit `3,247 s`, publication `59,2 %`. Largest stage:
`bulk load` `27,0 %`, inside LadybugDB (3.74 of 5.02 s of CPU under
`rebuild.Run` are `cgocall`): the Go profile does not see it. `GOGC` does not
move publication. `0,351 s` (`10,8 %`) are inside `publish` without a stage of
their own. It does not say whether `8 s` is reachable: the corpus is 14 times
smaller than the one behind the `17,150 s`. A toolchain comparison (`1,62x`)
was an artifact of an empty graph; the harness now rejects it.

Before 6b: a native profile of `liblbug` during the load (macOS `sample` or
`perf` on Linux), naming the missing stage, and a corpus of a size comparable
to the one behind the `17,150 s`; without it, 6b would optimize blind.

6b measured (PR #51): five private Go+TS repos, `35.297` symbols (0.20x of the
53-repo corpus), pass `6,913 s`, analysis `47 %`, publication `51,5 %`.
Findings, from most to least evidence:

- Directory walks in the analysis half: ~`1,5 s` of CPU per pass (`22 %`) in
  `freshness.Capture`, `TreeDigest`, `DiscoverGo`, `DiscoverTypeScript`,
  `BuildPlan`. Probable cause read from the code, not instrumented: they only
  skip `.git` and `node_modules`, and one repo has ~100,000 ignored entries.
  Changing it is a contract decision (the digest over-approximates on
  purpose), not a tuning one: it calls for an ADR.
- `PARALLEL=false` on every COPY: the serial CSV read is the top native
  function. Uncommitted experiment: parallel where there are no line breaks
  saves `0,16 s` (`2,4 %` of the pass). Logical equality against a clean
  rebuild is still missing.
- `0,843 s` (`12,2 %`) inside `publish` without a stage of their own.
- A pnpm repo (5 packages) fails to publish with `CONTAINS_FILE` bwd; not
  investigated.

No single lever exceeds `22 %`; the `8 s` target is already met on this
corpus, so it decides nothing.

Done afterwards (2026-11-15/16):

- PR #53: six repos, interleaved runs. Excluding a `target/` of ~47,000
  entries saves `0,9`-`1,1 s` of analysis; absolute seconds do not reproduce
  under load, only interleaved comparisons count.
- PR #52: a root package with a `tsconfig.base.json` and no sources no longer
  claims the files of its nested packages; `facts.Validate` rejects a file
  claimed by two packages. Effect: 80 files of a nested package outside its
  `include` stop being indexed. Separate defect left untouched:
  `UnclaimedTypeScriptSources` counts base configs as claimants.
- PR #54: an entry that disappears during a walk is skipped (the root still
  fails). With real churn in `target/`: `main` 31 of 40 passes, with the fix
  40 of 40. Not covered: the TypeScript glob walk.
- PR #55: `Publish` times its steps (`Report.PublishSteps`). What had no label
  in `publish` was source verification, `1,25`-`1,39 s` per pass
  (`10`-`11 %`); the macOS `fsync` is not there.
- PR #56 and #57, ADR 0121 (born as 0120): source verification can use a
  `stat` signature (`indexing.source_verification: signature`, default
  `full`). Measured in a pass, interleaved, six repos: `1,40 s` to `0,71 s`
  (`-49 %`) in the two undisturbed rounds, pass `~0,7 s` shorter (`~6 %`); 5
  of 6 trees not re-read (the sixth, edited seconds earlier, falls within the
  2 s window). The ADR's initial figure (`75 %`) was only digest and git
  state: the analyzer fingerprint and the per-repo `git` are not touched. Not
  tested on network file systems.
- PR #58: two ADRs were named 0120 (the sysroot one and mine on the `stat`
  signature); the second becomes 0121. My mistake: I did not check which
  number was free.
- PR #59 and #60, ADR 0122: the walks that decide the sources (inventory,
  `TreeDigest`, Go discovery, TypeScript and unclaimed TS) skip a directory
  with a valid `CACHEDIR.TAG`, with a single predicate
  (`workspace.IsCacheDirectory`). No name list and no `.gitignore`: both
  fail the over-approximation contract. `DiscoverCargo` and TypeScript glob
  expansion do not change (ADR). Measured, three interleaved arms, 24 passes
  each, six repos with a `target/` of `49.976` entries: pass `16,91 s` to
  `14,12 s` (`-2,79 s`, `-16,5 %`); with `exclusions: [target]` by hand,
  `15,14 s`. A first batch under load 22 gave `18,36` to `13,83 s`. A single
  marked directory, one repo, macOS. The digest of a repo with a marked
  directory changes once on upgrade (`stale`, tested in `freshness.Check`;
  `graph_status` and `doctor` not tested).
- PR #61: `doctor` lists in `walk.cache_tag` the directories skipped by tag
  (`workspace.CacheDirectories`): per repo, relative to its root, five at
  most and the rest counted; it says so when there are none; a tree that
  cannot be walked is `FAIL` and not "none". On the corpus: `target` in one
  private repository, the whole `doctor` in `0,95 s`. The PR got no CI from
  the `pull_request` event (GitHub left mergeability at `UNKNOWN`, closing
  and reopening did not help); `ci.yml` was launched with `workflow_dispatch`
  on the same commit and passed in full. The evidence is in that run, not in
  the PR's checks.
- PR #62: `benchmarks/walk-entries` counts the entries examined by each walk
  of the process, with an instrumented throwaway worktree (`instrument.py`,
  without touching production code) and a switch to turn off the ADR 0122
  rule. Pass after an edit, six repos, identical across the six passes of
  each arm: `803.626` entries without the rule and `446.857` with it
  (`-44 %`), about `7,0` per tree entry (`114.516` with the marked
  directories, `63.548` without them) with and without the rule: the rule
  shrinks the tree, not the multiple. Digest and inventory enter the marked
  `target/` twice, Go discovery twice and TypeScript discovery once;
  `treeDigest` is 73 walks (12 are the two observations, 61 match the
  per-unit fingerprint of the fact cache, not isolated). One walk was missing
  from the ADR's list: `indexer.countSourceFiles`. Entries, not time; without
  `go list`, the TypeScript worker or `rust-analyzer`.
- Investigated (no PR): Go discovery runs twice per pass because
  `indexer.FullWithRepositories` calls `goworkspace.BuildPlan`
  (`full.go:487`) and thirteen lines later `goloader.NewModuleRegistry`
  (`full.go:500`), and each one calls `workspace.DiscoverGo` per Go repo. The
  stack was dumped in a throwaway worktree: six calls per pass with three Go
  repos, the same in the warm-up and in the passes with an edit. Measured
  cost, 3 runs and 9 passes with an edit: `78 ms` per call (`58`, `19` and
  `1 ms` per repo), `156 ms` for the two out of a median of `6,93 s`
  (`2,3 %`); the second is redundant and is worth `78 ms` (`~1,1 %`), below
  the e2e noise (`21`-`27 %`). Warm page cache, loaded machine, not tested
  cold. The `Plan` cannot be reused: `BuildPlan` discards ambiguous modules
  and leaves a `Conflict` with repos and manifests sorted separately, and the
  registry keeps all providers by contract. The discoveries already made
  have to be passed to both functions: about `15` sites (`full.go`,
  `precision.go` with different repo sets, `doctor` and about ten test
  files). Decision: not to do it on its own; it goes, if at all, into the
  shared-walk ADR.
- Investigated (no PR): the per-unit fingerprints of the fact cache. They are
  63 distinct roots per pass (120 requests that the exact-path memo reduces),
  computed in parallel inside each unit's workers: `0,99 s` of CPU, `0,39 s`
  of wall clock with any in flight and `0,44 s` from first to last, `6 %` of
  a `7,16 s` pass and `15 %` of the analysis (`2,88 s`); maximum concurrency
  6-10. 51 of the 63 roots are inside another one, but the overlap is
  x`1,27` in files and x`1,04` in bytes (`152,7` MB summing roots, `147,0` MB
  the union). Hashing dominates, not walking: one root (`128` MB) costs
  `0,30`-`0,39 s`, about `59 µs` per entry against `1,4 µs` for the Go walk.
  Sharing the listing saves almost nothing; sharing the hash requires a
  Merkle-style digest that changes the identity of `TreeDigest` (ADR 0097)
  for <=`4 %` of bytes: no. The real duplication: 4 roots are the root of a
  registered repo and repeat the `TreeDigest` of the first observation
  (`0,62 s` of CPU out of `1,0`); reusing it would be correct (the cache
  commits its entries at `full.go:639`, after `VerifySources`). Measured by
  emulating the reuse for 3 of them, interleaved: pass `7,15` to `7,04 s` and
  analysis `2,84` to `2,82 s`, within the noise between rounds (up to
  ~`1 s`); same `33` cache hits and `53.395` symbols. They are off the
  critical path. Limits: `16` passes per arm (one round was aborted because
  my working repo changed during the pass), without emulating the edited copy
  (`0,167 s`), one corpus, load 4-5. Decision: closed; reopen only if a
  benchmark puts the fingerprints on the critical path.
- Investigated (no PR): why `go list` and the TypeScript worker walk
  directories and how much it costs, with a `DYLD_INSERT_LIBRARIES`
  interposer (outside the repo) that counts `getdirentries64` entries and the
  time of `getdirentries`, `stat` and `open` per process; validated against
  #62 (`~448 k` entries per pass versus `446.857`). A real
  `kivgraph index --full`, with Go and TypeScript edits. `go list`: three
  processes per Go module, `18` per pass with `6` modules; two are probes
  (`~0,02 s` in total) and the load takes as patterns the packages that
  `DiscoverGo` already found and `NeedDeps`, so it reads every directory,
  opens every `.go` for its constraints and imports, does `stat` upwards
  looking for `go.mod`/`go.work` and also loads the standard library and the
  module cache. Pass after editing Go: `23.771` entries, `34.171` `stat`,
  `0,67 s` of summed process time and `0,59 s` of it in file-system calls
  (`88 %`; `~7 %` of the `9,0 s` pass, summed across processes and threads,
  not critical path). Cold: `42` processes, `63.991` entries and `2,2 s` of
  `7,8 s` (`28 %`). TypeScript worker: one process per package; pass after
  editing TS (`16,9 s` versus `8,5 s` unchanged) one `8,75 s` process that
  reads `520` entries, `395` `stat` and spends `0,009 s` in FS; cold `25`
  processes, `34,7 s` and `0,19 s` of FS (`0,5 %`): it compiles, it does not
  walk, so there is no walk to take away from it. In the kivgraph process:
  `open` `0,69`-`0,71 s` (`~33-35 k` calls, `~20 µs`, hashing files),
  `getdirentries` `0,18`-`0,24 s`, `stat` `0,08`-`0,14 s`: `~11 %` of the
  pass. Correction: Go units do hit the cache (`9` of `14` on the corpus);
  the five loading `go list` runs in an unchanged pass (`0,51 s`) are the
  five modules that do not load (see the next investigation). Limits: system
  calls, not saveable time; no `read`; one corpus; load 4-5; no
  `rust-analyzer`. Decision: there is no walk to optimize; the lever, if it
  matters, is loading fewer packages per unit.
- Investigated (no PR): why five Go units miss the cache on every pass.
  `index --full` prints `index.go: modules=14 not_loaded=5` and
  `index.cache: hits=34 misses=5` (`14` Go and `25` TypeScript): the five
  misses are the five `MODULE_NOT_LOADED`; with a Go edit, `6` (the five plus
  the edited unit). Intentional rule at `factcache.go:348`: a module the
  loader could not read is not stored, because whether it reads depends on
  the module cache, which no source fingerprint describes, and storing it
  would turn "download the dependencies and reindex" into a no-op. The five:
  three `testdata/` fixtures of `kivgraph`
  (`could not import … invalid package name`, `~0,1 s`), a private Go
  repository (`module lookup disabled by GOPROXY=off`, a dependency is
  missing from the cache and `go.allow_network` is `false`, `~0,7 s`) and a
  module of another private repository (`undefined: application`, generated
  code absent, `~0,7 s`). Cost, `7` passes: `~15` `go list` and `~0,5 s` of
  summed process time per pass; critical path `0 s` in the `6` passes with an
  edit (the edited unit takes `1,3`-`1,6 s` in Go and `8`-`18 s` in
  TypeScript) and `+0,20 s` of `8,5 s` (`2 %`) in the unchanged pass
  (analysis `0,74 s` versus `0,54 s`). Decision: do not touch the rule;
  caching what did not load would give at most `~0,2 s` in a case that is not
  that of an agent editing, and would reintroduce the no-op the comment
  avoids. Closed.
- PR #63 and #64, ADR 0123 (no separate investigation PR): the question
  "should Go discovery skip `testdata/`" turned up two failures that stopped
  all indexing. (1) `DiscoverGo` turned every directory with `.go` into a
  package, and a file in `testdata/` that is not Go (or a directory with two
  package names) made the whole `index --full` fail; reproduced with the real
  binary, and `golang.org/x/tools@v0.41.0` (`1.254` Go files, `414` under
  `testdata`) stopped at the first bad file. `go help packages` says that
  `...` does not enter `testdata`, `_x` or `.x`. Skipping the directory
  entirely (4 lines) fixed it but dropped a module that lives in `testdata/`:
  with `replace example.com/fixture => ./testdata/mod` the call went to
  `unresolved=1` (definitions `2` to `1`), so it was discarded. #63 applies
  the rule to packages, from the module root, and a module inside is still a
  module (`replace` into `testdata/` intact). (2) `parseGoPackage` failed
  with several package names or a file that does not parse, which rejected
  the `//go:build ignore` idiom (a `package main` generator next to the
  library): `10` of `60` modules in the module cache have such a directory,
  `8` with `ignore`. #64 never fails: it ignores files with `ignore` before
  `package`, picks the name with the most files and lets `go` decide; the
  module that `go` rejects stays `MODULE_NOT_LOADED` with its reason and the
  rest of the repos are indexed. With the real binary: `ignore` indexes like
  the control; two package names and a non-Go file used to exit with `1` and
  now exit with `0` and `not_loaded=1`. The six-repo corpus does not change
  in either of the two (`14` modules, `12.482` definitions, `116` packages).
  Limits: a module rejected by `go` loses all its packages; `x/tools` now
  gets past discovery and stops at a dependency missing from its cache
  without network (`GOPROXY=off`), environmental; Go only.
- PR #65: why `TestResyncCoalescesAWholeWorkspaceIntoOneRebuild` failed
  (`batch = ["alpha"], want alpha and beta`). It is a race of the test with
  its fake clock, not of the `Resync` loop. The test moves the HEAD of
  `alpha` and that of `beta` and waits for `awaitMoved(t, 1)`, which counts
  reports and not repositories; the first tick can satisfy it having seen
  only `alpha`, the test advances the clock one minute and the advance lands
  inside a tick whose `poll` already read the old HEAD of `beta`: that tick
  sees `60 s` of silence and fires `alpha` alone. In production the wait is
  on a real clock and it would take a `30 s` `poll` to split a batch.
  Confirmed with in-memory traces (the `stderr` ones change the timing and
  hide the failure; two failures captured under load with the same sequence)
  and with a forced interleaving through a gate in `Now()`, not committed:
  the old flow gives `["alpha"]` `3` of `3` times and the new one passes the
  same interleaving `3` of `3`. Fix, test only: `awaitMovedRepositories`
  waits until the loop has reported every name (each report carries the full
  pending set); the old comment on `startResync`, which promised that
  counting observations was enough, is corrected. It is the only test in the
  file that moves two repositories in a row. Frequency does not separate
  before from after: `2` of `900` with 12 processes burning CPU and `0` of
  `3000` on both binaries in another batch; the proof is the deterministic
  one, not a permanent test.
- PR #66, ADR 0124: why the corpus's TypeScript symbol count alternated
  between `72.723` (`6` of `8` cold runs) and `68.298` (`2`). Only one unit
  of `25` moved, `@kivgraph/ts-worker` (`31.756` or `27.331` symbols); it was
  not a live repo, as had been suspected. What changed was the compiler's
  default library (`lib.*.d.ts`; `lib.es5.d.ts` alone is `2.517` symbols),
  published as symbols of the repository in some runs and not in others. The
  cause is in the native engine: with only its API (`typescript@7.0.2`) and
  the same project opened `80` times, `isSourceFileDefaultLibrary` always
  gives the same `65` files and `isSourceFileFromExternalLibrary` changes for
  `41` of them (external in `17` of `80` runs); for the rest of the files it
  never changes. It only affects a repository that contains its own copy of
  the engine, like this one (`ts-worker/node_modules`): in the others the
  library stays outside the root. Of that unit's `31.756` symbols, `28.255`
  (`39 %` of the corpus's TypeScript ones) were language declarations.
  `isDependencySourceFile` treats as foreign whatever the engine calls
  external or default library, at the four sites that decide which files are
  read. With the fixed build installed as the worker of the corpus itself:
  `44.468` symbols, `98.926` references and `1.239` unresolved in `12` of
  `12` cold runs; `@kivgraph/ts-worker` drops from `31.756` to `3.501`
  symbols. My first verification was not valid: with a launcher pointing at
  the repo's `dist` the engine stayed outside the indexed root and gave
  `44.468` with or without the fix; the fixed worker has to be put inside the
  corpus. Tests: one unit test of the helper and one integration test on the
  worker's own project (fails without the fix, `3` of `3`). There is no test
  that the other three sites use the helper: none has a fixture where the
  default library falls under the root and can be told apart. Limit:
  rebuilding only `ts-worker/dist` does not invalidate the fact cache
  (measured: `33` of `39` units served from cache and `72.723` symbols with
  the fixed build put in place; closed in #67 for a checkout). CI failed
  twice before passing (my test exceeded `5 s`, three lines of the ADR went
  over `84` columns) and once because of a rate limit of `repo1.maven.org` in
  the `Verify Go` job, unrelated to the change.
- PR #67, ADR 0100 (amendment): the TypeScript worker fingerprint
  (`typeScriptWorkerFingerprint`) hashed the entry script, and a
  `ts-worker/dist` rebuilt with different modules left the analyzer
  fingerprint unchanged: the fact cache served the old worker's facts. When
  the worker is a script in a `dist` directory, its identity becomes the
  script plus all the `.js`/`.mjs`/`.cjs` modules under it, by relative path
  and content; declarations, maps and compiled tests (`*.test.js`), which the
  analysis does not load, are left out; an absent or unreadable `dist` gives
  `absent` or `unreadable`; a script in a directory not named `dist` is still
  identified by its bytes, because its directory could be anything and
  walking it on every observation would cost more than the cache saves.
  Measured with the real binary from the checkout (the worker is
  `node ts-worker/dist/facts-cli.js`), same `HOME`, with the fixed build put
  in place: `main` gave `hits=33 misses=6` and the old `72.723` symbols; the
  branch, `hits=0 misses=39` and `44.468`, and the next pass with the same
  build went back to `hits=34`. Tests with a fixture that stands in for
  `node dist/facts-cli.js`: five of the six module changes fail on `main`;
  the cases that must not move the fingerprint (same content, another
  directory, `.d.ts`, maps, compiled tests, a directory that is not `dist`)
  do not move it. Limits: a worker launched through a shim is still
  identified by the shim and the executable, and a shim pointing at a `dist`
  rebuilt in place is not covered (that is how I produced the stale cache in
  #66); and the TypeScript engine, which the worker loads from outside its
  build, is not seen: a new version under the same `package.json` range goes
  unnoticed. CI green at the first attempt.
- PR #68: investigation of why a shim is not covered. The fingerprint only
  resolves the `dist` when the command is `node <script>` or the checkout
  fallback; with a launcher the path lives inside the script and the Go side
  does not read it (five configurations measured: launcher on the `PATH`,
  `worker_command` with a launcher: they do not follow an edit; checkout,
  `node <dist>/facts-cli.js`: they do). Finding: `node <dist>/facts-cli.js`
  did not work for `facts`, because the indexer passes `facts` and
  `facts-cli.js` takes it for the repository name (it prints the usage and
  exits 0); only the checkout fallback removed the word, so outside a
  checkout the launcher was mandatory and was exactly what the fingerprint
  did not see. Options discarded: asking the launcher (43 ms of node startup
  per call, 3 calls per pass, versus 0.46 ms of hashing in process) and
  looking at `<shim>/../worker/dist` (the bundle is already covered by the
  binary). Change: `factsCommand` removes the word `facts` when the
  configured command runs a script named `facts-cli.js` (the name only, in
  any directory; a `--require=…/facts-cli.js` option does not count);
  `docs/installation.md` documents
  `node <checkout>/ts-worker/dist/facts-cli.js` without a launcher, which #67
  already covers. Tests in `full_worker_command_test.go`: with the rule
  turned off 3 fail, applied to any command another 3 fail. Not tested
  against a real node worker (the fixture is a test program). A shim that
  hides its `dist` remains uncovered: it is avoided, not fixed. CI green at
  the first attempt.
- PR #69, amendment to ADR 0100: the TypeScript engine enters the
  fingerprint. The `dist` modules import the `typescript` package, which
  starts the native executable of the sibling platform package
  (`@typescript/typescript-<os>-<arch>`, `23,6 MB` plus the `lib.*.d.ts`,
  `27,4 MB` in total); a `pnpm install` that changes either of the two does
  not touch `dist`. Demonstrated on `main` with the worker copied to the
  bundle layout and `worker_command = node <wA>/dist/facts-cli.js` (possible
  since #68): first pass `misses=1`, second `hits=1`; with the engine swapped
  in place for `typescript@7.0.1-rc`, `hits=1` and `27` symbols from the
  cache, for a repository that engine does not load
  (`project … is not part of snapshot 1`). Change: `workerEngineFingerprint`
  looks for `node_modules/typescript` going up from the `dist` directory
  (links resolved), hashes the package without `.map` or
  `.d.ts`/`.d.cts`/`.d.mts`, and every package under `@typescript/` in full
  (there is no platform table: what is installed gets hashed); no
  `typescript`, no platform and unreadable platform are distinct values;
  `workerModulesFingerprint` and the new code share `hashTree`. With the
  change, the same swap runs the worker (which fails visibly) and restoring
  the engine gives a hit again. Same value in the pnpm checkout (links) and
  in the bundle copy. Cost: ~`15 ms` per computation with a warm disk cache
  (`26 MB`), three per pass (~`0,6 %` of a `7 s` pass); cold, not measured.
  Tests in `factcache_worker_engine_test.go` (flat and pnpm layouts):
  removing the call (= `main`) fails `15`, not hashing the platform `8`,
  hashing declarations `3`, not resolving links `5`. Only an engine that
  fails was measured; not one that gives different facts without failing. The
  fingerprint changes once on deploy (one cache miss per entry). Left out: a
  worker outside `dist` and a shim that hides its `dist`. CI green at the
  first attempt. Process note: I exported `HOME` to a temporary directory in
  the persistent shell and `gh` lost authentication until it was restored; it
  does not affect the repo.
- Investigation after #69 (no PR; limit documented by decision): can a worker
  outside a `dist` directory enter the fingerprint? Yes. Failure on `main`:
  worker copied to `wC/build/` with
  `worker_command = node …/build/facts-cli.js`; editing
  `symbol-extractor.js` (imported) gave `hits=1`, the cache served the old
  worker's facts (outside `dist` the value is `entry-only`). Walking the
  directory was discarded for the same reason as in #67 (it can be `$HOME`).
  Prototype in a throwaway worktree: import graph from the entry
  (`import`/`export … from`/`require` with a relative specifier, regex) and
  engine looked up from the entry's directory. Recall: the real worker loads
  `20` modules of its own (measured with a `module.register` hook) and the
  scan found all `20`; `2` false positives (paths in comments, nonexistent);
  walking `dist` already over-approximates `5` of `25` modules. With the
  prototype, editing an imported module gives `misses=1` and editing one that
  `facts-cli` does not import (`precision-report.js`) keeps `hits=1`
  (precision that no walk gives); changing the engine in `wC` runs the
  worker. Cost: ~`8,4 ms` for the regex graph (`0,4 ms` to hash `dist`),
  ~`14,6 ms` for the engine. Limit of the scan: `import(variable)`,
  `createRequire`, `new Worker(ruta)` and files read by computed path; our
  worker uses none. If a dynamic form were detected, the ways out would be to
  fall back to a bounded walk of the entry's directory or not to cache that
  worker (the latter repeats the silently disabled cache that
  `javaIndexerFingerprint` describes; only with a warning in `doctor`).
  Estimate: ~`120` lines plus tests and an amendment to ADR 0100. Decision:
  not to do it; today it only shows up with a custom `worker_command` and the
  documented route uses `dist`. The limit is already recorded in the ADR 0100
  amendment ("Not covered: a worker outside a `dist` directory");
  `docs/installation.md` does not mention it. Separate finding, investigated
  next (following entry).
- Investigation of `.cjs` and `.ts` entries (no PR; limit documented by
  decision). Cause: the loop in `typeScriptWorkerFingerprint`
  (`factcache.go:1147-1148`) only treats as a script an argument ending in
  `.js` or `.mjs`, and the entry's bytes, the module walk of #67 and the
  engine of #69 all hang on that condition; the condition was a stand-in for
  "the script node runs" written for `node dist/facts-cli.js`. Measured with
  a test `node` and the entry in `dist`: editing the entry is seen with `.js`
  and `.mjs` and is not seen with `.cjs`, `.ts`, `.mts`, `.cts` or
  `--import tsx x.ts`; a `.cjs` in `dist` gets neither its bytes, nor the
  `dist` walk, nor the engine (identity = the `node` binary). A
  `--require=…/preload.cjs` argument does not contribute its content either;
  if it ends in `.js` it is treated as a script of that name and gives a
  constant value. Latent: the script's relative path is read from the process
  directory and the worker runs in `options.WorkingDirectory`; in production
  they coincide (`os.Getwd()` at `main.go:1692`, `main.go:1901` and
  `full.go:426`), so it only fails if the service is built with another
  directory. Not a failure: the `pnpm --dir … facts` fallback passes no `.js`
  (identity = the `pnpm` binary) but builds `dist` and the next pass already
  sees the checkout rule (from reading the code, not tested). Options: (1)
  extend the list to `.cjs`, `.cts`, `.mts`, `.ts` (3 lines; `.cjs` in `dist`
  recovers everything, `.ts` only its bytes); (2) resolve relative paths
  against `WorkingDirectory` (1 line); (3) hash every argument that is an
  existing regular file (covers `--import ./hook.mjs`, over-approximates).
  Recommended: 1 and 2, ~`15` lines plus tests and one sentence in ADR 0100.
  Decision: not to do it; the project's own worker uses `.js` and it only
  matters with a custom `worker_command`. The limit is not recorded in ADR
  0100 or in `docs/installation.md` (the amendment only says "not covered"
  for a worker outside `dist`).
- Investigation of the `pnpm` fallback (no PR; limit documented by decision):
  does it leave the cache serving stale facts on the first pass? No. The
  fallback is reached with the default command off the `PATH`, without
  `ts-worker/dist/facts-cli.js` under the working directory and without a
  worker sibling of the executable (`full.go:1738-1765`): a fresh clone with
  `kivgraph` compiled by hand; it runs `pnpm --dir ts-worker facts …`. With
  no `.js` argument, its fingerprint is only the `pnpm` binary, the same in
  every checkout and blind to the source code. It does not serve old facts
  for four observed reasons: the fingerprint is computed once when the cache
  is created (with `dist` absent); `pnpm facts` builds `dist` during the
  pass; on publishing, `observeSources` recomputes the fingerprint and now
  gets the one of the `node` path, so it is rejected with
  `source observations changed during indexing: analyzer configuration changed`;
  and entries are only admitted after publishing (`CommitCache`,
  `indexing/full.go:639`), so none remains under the fallback's fingerprint.
  Measured with the `main` binary and two copies of the worker without
  `dist`: pass 1 `rc=1` with that error (although it had already produced
  `27` symbols) and `dist` built; pass 2 (checkout rule) `rc=0`, `misses=1`,
  new facts; pass 3 with `dist` deleted and the worker broken on purpose: no
  `index.cache` line and the worker ran (`SABOTAGE`), nothing was served from
  cache; same on the other copy. A real defect of another kind: on a new
  clone the first pass fails with a message that does not point at the cause
  and the second works. Not tested: the watchers of `invalidation.go` in the
  same scenario, a `pnpm build` that fails, and whoever calls `indexer.Full`
  directly (it admits entries at `full.go:401`). Options: (1) document it
  ("the first pass in a checkout without `dist` fails once", one sentence in
  `docs/installation.md`); (2) build `dist` before observing if the fallback
  is going to be used (touches the order of the phases); (3) detect the
  fingerprint change caused by the fallback and retry once within the pass.
  Decision: documented limit; option 1 recommended. The text is not yet in
  `docs/installation.md`.
- Investigation of the watchers in `invalidation.go` (origin of #70): do they
  compute the analyzer fingerprint in the `pnpm` fallback scenario? Yes. The
  initial watcher and `refreshInvalidationForProfile` call
  `indexing.ObserveSources` (`invalidation.go:270` and `:334`), which calls
  `indexer.AnalyzerFingerprint` (`indexing/full.go:337`) and leaves it in the
  manifest; `Invalidate` compares it with the one of the published generation
  and any fingerprint difference marks the profile stale. There are no other
  callers outside the pass. Measured with the `main` binary: two passes from a
  checkout (the first rejected, the second publishes `000001`), `dist` deleted
  and `kivgraph daemon` started in that directory: the watcher computes the
  fallback fingerprint (different from the published one), marks the profile
  stale and reindexes; `pnpm facts` builds `dist` and `000002` is rejected with
  `analyzer configuration changed` (`could not rebuild stale profile`, attempt
  1); the retry after `2 s` uses the `node` path and publishes `000002`. It
  converges with one retry (~`3 s`), within the budget of `5` attempts
  (`profileInvalidationAttempts`); it does not serve old facts and the error
  shows in the daemon log. Finding: the watchers' options come from
  `indexing.OptionsFromConfig`, which does not set `WorkingDirectory`, while
  the indexing service does (`service.go:360`); with the directory empty the
  checkout rule yields the relative path `ts-worker/dist/facts-cli.js`, and the
  upward walk of `resolveEnginePackage` ends at `.`, so it does not reach an
  engine above the working directory (monorepo, npm or yarn install with the
  engine hoisted). Tested at function level: with the engine inside `ts-worker`
  or in the working directory itself, watcher and pass agree; with the engine
  above, they differ. From reading the code the profile would become stale
  after every publication and would be reindexed until `5` attempts are
  exhausted (`gave up rebuilding a stale profile`), again on every event or
  reconciliation (every `10 min`); not reproduced end to end. The layout of
  this repository is not affected (pnpm links `typescript` inside
  `ts-worker/node_modules`). The missing `WorkingDirectory` in the watcher
  predates #69; #69 made it observable by adding a lookup that walks up the
  parents.
- PR #70, amendment to ADR 0100: `workerEngineFingerprint` converts the `dist`
  directory into an absolute path before walking up in search of
  `node_modules/typescript` (`filepath.Abs`; if it fails, `unreadable`). Option
  1 of three: `WorkingDirectory` was not set in `OptionsFromConfig` because it
  would touch other fields of the fingerprint. Test
  `TestWorkerIdentityOfAnEngineAboveTheWorkingDirectoryIsTheSameForARelativePath`:
  engine above the working directory, a relative and an absolute script must
  yield a single identity and editing the engine must change it (so that they
  do not coincide through a shared absence); it fails with `filepath.Abs`
  disabled and passes with it. The fingerprint of a worker with the engine
  above the working directory changes once. CI green on the first run. I did
  not run `make build`, `make test-ladybug` or Bazel locally (no new file).
- Investigation of the empty `WorkingDirectory` in `OptionsFromConfig` (closed,
  no PR): does it affect other fields of the fingerprint besides the engine of
  #70? No. Inside `AnalyzerFingerprint` it is only used in two places:
  `factsCommand` (`full.go:1738`, the checkout rule; the case of #70) and
  `pythonProducerFingerprint`, via `pythonloader.ProducerFile`, which looks for
  `python-worker/index.py` or `pyright_index.py` with `filepath.Join`
  (`loader.go:122` and `:149`). Outside the fingerprint: `DiscoverSysroot`
  (`rust_unit.go:490`), `semantic.go:113` and `commandContext.Dir`. `go env`,
  `rustc --version` and `rust-analyzer` run in the process cwd; Java, C# and
  the executable digest do not read relative paths. Python does not diverge
  because the relative and the absolute path point to the same file (the
  working directory is the process cwd on every production path: `NewService`
  and `indexer.Full` use `os.Getwd()`) and `fileFingerprint` hashes the
  content, not the path string; there is no lookup that walks up the parents.
  Measured in a throwaway worktree with `t.Chdir`: `AnalyzerFingerprint` with
  an empty `WorkingDirectory` against the absolute one, `4` layouts (full
  checkout, only `python-worker`, only `ts-worker`, nothing in the cwd) times
  `4` option sets (default, Python `auto`/`exact`, plain Python, Rust+Java+C#
  with explicit commands): the `16` combinations give an identical fingerprint
  and no errors; editing `python-worker/index.py` changes the fingerprint with
  both values, so the probe is not vacuous. Limits: only the real case (working
  directory = process cwd; if someone builds the service with a different one,
  Python would diverge, and that path does not exist today); the engine above
  the cwd only agrees thanks to the `filepath.Abs` of #70; measured at function
  level, not with a daemon. Option discarded for now: a permanent test
  comparing `AnalyzerFingerprint` with and without `WorkingDirectory` on a
  layout with `ts-worker`, an engine above and `python-worker` (~`40` lines),
  which would keep a future field from reading the working directory without
  anyone noticing; not needed today.
- Investigation of `go env` and `rustc --version` in the fingerprint (origin of
  #71): do they depend on the process cwd? Yes. `goEnvironmentFingerprint` ran
  `go env GOVERSION GOROOT …` without `Dir`, and `rustAnalysisFingerprint`
  called `ToolchainVersion(ctx, binary)`, which passed `""` as the directory,
  in addition to `DiscoverSysroot(ctx, "")`: everything ran in the cwd. Go,
  measured with the `go1.26.11` of the module cache as the baseline:
  `go env GOVERSION GOROOT` gives `go1.26.11` in a module with `go 1.21` and in
  one with `toolchain go1.26.11`, and `go1.27.4` with a different `GOROOT` in
  one with `go 1.27.4`; moreover that call **downloaded** the toolchain
  (`go: downloading go1.27.4`, now in the Go module cache of this machine,
  outside the repo). The loaders do not set `GOTOOLCHAIN` and `packages.Load`
  runs with `Dir` = the module directory, so the toolchain that analyzes each
  repo is decided by its `go.mod`, not by the cwd. Rust: there is no `rustup`
  on this machine (only Homebrew's `rustc`); with a stand-in `rustc` that
  answers according to the `rust-toolchain.toml` of the directory, like the
  proxy, the fingerprint changes with the cwd (it demonstrates the dependency,
  not the real behavior of rustup; the rest comes from the code and from the
  comments at `sysroot.go:72` and `scip_run.go:385`, and `cargo metadata` runs
  with `Dir` = workspace). Watcher and pass share a process, so they do not
  diverge from each other and there are no loops; the risk is a blind
  fingerprint (Go: a cwd in a module that requires a toolchain newer than the
  local one leaves the fingerprint fixed while the `go` on the `PATH` used by
  the other repos is updated; Rust, from reading the code, `[INFERENCE]`: a
  `rustup update` of a `stable` pinned by a repo with a different default
  toolchain) and two identities for one machine if two processes start in
  different places (only a cost). Finding: `rust-toolchain` and
  `rust-toolchain.toml` were not in `analyzedManifests`
  (`observation.go:891-904`) and `.toml` is not a code extension, so editing a
  repo's pin did not change any digest. Options: (1) add them as analyzed
  sources; (2) run the probes in a neutral directory; (3) run them with `Dir`
  in each repo or module (more faithful, one `go env` and one `rustc --version`
  per unit, a redesign). 1 and 2 were done.
- PR #71, amendment to ADR 0100: `probeDirectory()` (the temporary directory if
  it is absolute and usable; otherwise the process directory, as before) for
  `go env` and the four Rust probes (`rustc --version`, sysroot,
  `cargo --version`, analyzer version); `rustloader.ToolchainVersion` receives
  the directory and its three callers pass it; `rust-toolchain` and
  `rust-toolchain.toml` become analyzed sources. The pin of each repo is not
  asked for: changing it means changing `go.mod`, `go.work` or
  `rust-toolchain`, which the tree of its unit already records. Tests:
  `TestGoEnvironmentIsAskedFromADirectoryThatPinsNothing` and
  `TestRustToolchainIsAskedFromADirectoryThatPinsNothing` (stand-ins for `go`,
  `rustc` and `cargo` that answer according to their directory and record it;
  the fingerprint matches between a cwd with a pin and one without, it answers
  with the default toolchain and all the probes ran in the temporary
  directory),
  `TestProbesFallBackToTheProcessDirectoryWhenTheTemporaryOneIsUnusable`
  (`TMPDIR` nonexistent and relative),
  `TestProbeDirectoryIsTheTemporaryDirectory`, extensions of
  `TestIsAnalyzedSourceRecognizesSourceAndBuildInputs` and
  `TestEveryLanguagesManifestsInvalidateAnEntry`, and
  `TestFactCacheMissesWhenTheRustToolchainPinChanges`. Mutations: without the
  pin files as sources `6` subtests fail; `go env` back in the process
  directory `1`; the Rust probes back `1`; only the `cargo` one `1`. The
  fingerprint changes once if the startup directory pinned a toolchain, and the
  units of a repo with `rust-toolchain` are rebuilt once. It does not cover a
  `rustup update` of a pinned `stable` without touching files. The Unix test
  file is not in `BUILD.bazel`, like `rust_fingerprint_unix_test.go`. CI:
  `Classify change` ended `cancelled` after `5 min` (it was not the code) and
  the rest was skipped; `gh run rerun --failed` for that job and green on the
  second run. I did not run `make build`, `make test-ladybug` or Bazel locally.
- Investigation of the digest of the kivgraph executable in the fingerprint (no
  PR; limit documented by decision): does it depend on how the process is
  launched? On how it is referenced, no; on what is at the path while it runs,
  yes. `AnalyzerFingerprint` does `os.Executable()` and `FileDigest` on that
  path (`factcache.go:1076-1084`); `FileDigest` hashes `size=` and the content,
  without the path: it digests the file that is at the path, not the code being
  executed. Way of launching it (a probe binary that prints `os.Executable()`
  and the fingerprint): `9` ways give the same fingerprint (`6c2c0d5f9d39`):
  absolute path, relative `./` and `../`, symlink, hardlink, byte-for-byte copy
  in another directory, via `PATH`, `argv[0]` forged with `exec -a` and a
  different working directory; `os.Executable()` returns different strings
  (even `…/sub/../real` uncleaned) and the digest is of content only; two
  identical copies share the cache. Replacement with the process running
  (macOS): by `rename` (another inode) and by in-place overwrite (same inode)
  the fingerprint becomes that of the new file while the old code keeps
  running; deletion without replacement gives
  `digest Kivgraph executable: source input is absent`. On Linux
  `os.Executable()` reads `/proc/self/exe` and trims ` (deleted)` (read in
  `executable_procfs.go`, not tested). With a real daemon (binary copied to
  another path): replaced by another binary, on touching a source file the
  watcher sees the new fingerprint, marks the profile stale and reindexes with
  a child (`RunDetached` executes `os.Executable()`, that is, the new binary)
  and publishes `000002`: it converges and that is the desirable outcome;
  deleted: `could not observe profile sources` (`digest Kivgraph executable`),
  the reindex fails `5` times with `fork/exec … no such file or directory` and
  `gave up rebuilding a stale profile`; the daemon stays alive and serving,
  without indexing again until it is restarted. Why the replacement does not
  produce facts from old code with a new fingerprint: the passes run in a child
  that executes whatever is at the path, so the code that generates the entries
  and the digest that names them agree; the old code only computes fingerprints
  in-process for the watchers, and a direct `kivgraph index --full` with the
  binary swapped mid-pass is rejected with `analyzer configuration changed`.
  Cost: `31,8 MB` in ~`26 ms` (Python's `hashlib`, `32 KiB` reads; `FileDigest`
  in Go not measured), three per pass plus one per watcher observation. Real
  defect: deletion without replacement (a package manager that removes the old
  version) leaves a live daemon unable to observe or reindex, and the `gave up`
  message talks about fixing the source or the analyzer and about
  `kivgraph index --full`, not about restarting the daemon; `update` does
  restart the supervised daemons (there is a test), a `brew upgrade` or a
  manual `rm` does not. macOS only; no Windows and no deletion on Linux.
  Options: (1) document the deletion as a limit and adjust the `gave up`
  message to mention the restart; (2) digest the binary once at startup and
  cache it in the process (closes the deletion case, but a replacement would no
  longer mark profiles stale until a restart and the child would keep using the
  new one); (3) treat `ErrAbsent` of the executable as an analyzer change and
  stop retrying with a clear message. Decision: documented limit; option 1
  recommended. Not yet recorded in the repo.
- Investigation of the cost of the executable digest (origin of #72): can it
  cost less with a cache keyed by file signature? Yes, but the saving is small.
  Cost of the fingerprint in a real pass (corpus of `6` repos, `main` binary
  with a temporary `AFP_TRACE` trace): `3` calls to `AnalyzerFingerprint` per
  pass; cold pass `24,5 s` (`252`, `79`, `118 ms`), warm `7,4` and `7,5 s`
  (`81`, `80`, `121 ms` and `97`, `89`, `116 ms`): ~`0,28-0,32 s`, ~`4 %` of a
  warm pass. Breakdown of one call (separate probe): digest of the kivgraph
  executable (`31,8 MB`) `20 ms`; digest of `node` (`133 MB`, only with a
  `node <script>` worker) `69 ms`; TypeScript engine `20 ms`; Rust probes
  (`rustc`, `cargo`, sysroot, processes) ~`96 ms`; `go env` (process) `8 ms`;
  the rest `< 1 ms`; the whole call `183 ms` with a `node` worker. In the
  corpus the worker is a shim, so `node` is not hashed and the executable is
  ~`20` of ~`80-120 ms`. Prototype of an in-process signature cache (keyed by
  path; signature = size, mtime, ctime, inode and device; `2 s` window as in
  ADR 0121): hits go from `51 ms` to `0,6 µs` for `node` and from `14 ms` to
  `0,6 µs` for the executable; an mtime restored with the same size is detected
  by ctime; a `rename` is detected by the inode; a file written less than `2 s`
  ago is not cached and at `2,2 s` it is, and a later edit is detected. Saving:
  within a pass it only helps calls `2` and `3`; the executable alone ~`40 ms`
  (~`0,5 %`), with a `node` worker ~`220 ms` (~`3 %`); in a daemon, the digest
  of each watcher observation; a new process pays for the first call anyway and
  persisting it to disk would add state. Limits: the same open problem as ADR
  0121 (network file systems or ones with coarse ctime, untested); the Rust
  probes are processes and cannot be cached by signature without risk (a
  `rustup default` changes the answer without changing the proxy binary);
  `FileDigest` in Go within the corpus not measured. Cheaper alternative: call
  `2` is the one from `newFactCache`, with the same options as call `1`;
  reusing the value of call `1` saves a whole call (`80-180 ms`, `1-2,4 %`)
  without new trust in file statistics. Options: (1) reuse the fingerprint of
  call `1` (done in #72); (2) a signature cache for the file digests, only with
  `source_verification: signature` (~`100` lines, race and ctime tests, an ADR
  amendment; it pays off only with `node` workers, ~`3 %`); (3) nothing. Option
  `1` was done; option `2` remains open (candidate).
- PR #72: `indexer.FullOptions.AnalyzerFingerprint` (the value the caller has
  just observed) and `newFactCache` uses it as the key of the entries if it is
  not blank (with `strings.TrimSpace`) and computes it if it is;
  `indexing.Full` fills it with `manifest.AnalyzerFingerprint`, so a pass
  computes the fingerprint twice (observe and verify) and not three times.
  Verification still computes its own, which is the one that detects a change
  mid-pass: if the analyzer changes between the observation and the unit, the
  entries are stored with the observed value and the final verification rejects
  the pass without committing anything. Test
  `TestFactCacheKeysEntriesWithTheFingerprintTheCallerObserved`: without an
  observed value another resolver version is a miss; blank or whitespace-only
  is computed (two passes with different resolvers do not hit each other); the
  same observed value hits across versions and a different one misses.
  Mutations: value ignored fails (`analyzer_changed` rejection); blank taken as
  an observation fails on the second pass with blank (that pass had to be
  added: the first version of the test did not detect it). Real binary, corpus
  of `6` repos: `3` calls per pass (`~80`, `~80`, `~107 ms`) before and `2`
  (`~79`, `~108 ms`) after; warm pass `7,14` and `7,15 s` before, `7,03`,
  `7,04` and `7,10 s` after, `hits=34 misses=5` in both; consecutive runs, not
  interleaved: ~`0,1 s` (~`1,3 %`) is the order of magnitude. It does not
  change the value of the fingerprint, so it does not invalidate any cache. CI
  green on the first run. I did not run `make build`, `make test-ladybug` or
  Bazel locally.
- Investigation of the Rust probes in the fingerprint (origin of #73): can they
  cost less without risk? Yes. With a resolvable `rust-analyzer`, one call
  launched `5` processes in series, counted with stand-ins for `rustc`, `cargo`
  and `rust-analyzer` that record every call: `rust-analyzer --version`,
  `rustc --version` (the `rustc=` block), `rustc --version` again inside
  `DiscoverSysroot` (`probeToolchain`), `rustc --print sysroot` and
  `cargo --version`; the second `rustc --version` repeats the first and nobody
  reuses its result; a pass makes `2` calls since #72, so that is `10`
  processes per pass. Measured (Homebrew, no `rustup`; there is no
  `rust-analyzer` here, so that process was not included): `rustc --version`
  `17,9 ms`, `cargo --version` `9,0 ms`, `DiscoverSysroot` `35,6 ms`; today
  `60,4 ms` (`4` processes in series), `rustc --version` only once `43,6 ms`
  (`-28 %`), only once and the three in parallel `18,5 ms` (`-69 %`). With
  `rustup` proxies each process costs more (`[INFERENCE]`, not measured). Why
  those two are safe: the duplicate gives an identical result by construction
  and the fingerprint only uses the reason (`library=`) from `DiscoverSysroot`;
  the three probes are independent and are composed into the hash in a fixed
  order after they finish, and `RUSTUP_AUTO_INSTALL=0` already prevents a proxy
  from installing in a race. No safe way to skip the probes: a cache keyed by
  the binary's signature is false confidence (a `rustup default` changes the
  answer without touching the proxy); skipping them when the pass has no Rust
  repos makes the value depend on whether there is Rust, and the fact cache is
  shared between profiles (`TestFactCacheKeepsAlternatingProfilesWarm`), so
  alternating a profile with Rust and one without would go cold on every switch
  (~`20 s` versus `7 s` in this corpus); a per-language fingerprint is the
  right thing (and would avoid the over-invalidation: today a `rustup update`
  also makes Go and TypeScript go cold) but it changes the entry format
  (`cacheEntryVersion`), the observation manifest, and requires an ADR.
  Options: (1) remove the second `rustc --version` and launch the probes in
  parallel (done in #73); (2) per-language fingerprint (redesign with an ADR);
  (3) nothing. Option `1` was done; option `2` remains open (candidate).
- PR #73: `rustloader.ProbeToolchainIdentity(ctx, dir)` answers the `--version`
  line, whether it answered and why the library is missing (the answer of
  `DiscoverSysroot`) with a single `--version` and a single `--print sysroot`;
  `DiscoverSysroot` and `probeToolchain` are rebuilt on top of `askToolchain`
  and `sysrootFrom` without changing behavior; `rustAnalysisFingerprint` asks
  the analyzer, the toolchain identity and `cargo` at the same time and writes
  the answers in the usual order. Value unchanged: with `7` fixed stand-in
  configurations (complete, no library, no analyzer, no `cargo`, a `--version`
  that is not a version, another release and no `rustc`) the `7` values come
  out identical on `main` and on the branch (throwaway test, outside the diff).
  Tests: `TestProbeToolchainIdentityAnswersWhatDiscoverSysrootDoes` (four
  states, which questions it received and the same library answer as
  `DiscoverSysroot`),
  `TestProbeToolchainIdentityWithoutAToolchainAnswersNothing`,
  `TestProbeToolchainIdentityStopsWhenTheContextIsCancelled` and
  `TestRustFingerprintAsksEachToolchainOnceAndTogether` (the stand-ins wait for
  the other two to start). Mutations: sequential fails
  (`rust-analyzer never saw the other two running`, after a `17,7 s` wait);
  `rustc --version` twice again fails; losing the library reason fails in the
  identity tests and in
  `TestRustFingerprintChangesWhenTheLibrarySourcesArrive`. Cost of
  `rustAnalysisFingerprint`, best of `15` and alternating `main` and the
  branch: `98,5` and `99,6 ms` against `50,2` and `51,2 ms` (~`48 ms` per call,
  `2` per pass); over a warm pass (~`0,1 s`, ~`1,3 %`) it stays within the
  noise and is not claimed on that basis; real binary, corpus of `6` repos:
  cold `21,98 s`, warm `7,03` and `7,06 s`, `hits=34 misses=5`. Finding in my
  own test: the stand-ins' `PATH` contained only them, `sleep` did not exist
  and the loop spun without waiting, so the race passed by time window and not
  by design; fixed with `/bin/sleep` (~`0,5 s` for the positive case). It does
  not invalidate any cache. CI green on the first run. The two Unix test files
  are not in `BUILD.bazel`, like the other `_unix_test.go` files. I did not run
  `make build`, `make test-ladybug` or Bazel locally.
- Investigation of the per-language fingerprint of the fact cache (origin of
  #74): is it worth it? Only halfway, and not now. Today the fingerprint is a
  single value; each entry stores `entry.Analyzer` and `load` compares it
  (`factcache.go:380`); the entry path is `sha256(identity + local address)`
  and does not include the fingerprint, so there is a single entry per unit and
  content: any difference rejects all the units (`analyzer_changed`) and the
  next pass overwrites them. Cost of an irrelevant change, measured by
  invalidating selectively (deleting entries by language, `2` rounds) on the
  corpus of `6` repos (`25` TypeScript units, `159 MB` of entries, `72.723`
  symbols; `9` Go entries, `48 MB`; `5` Go modules that are never stored): warm
  `7,1-7,4 s`; only TypeScript invalidated `19,1` and `19,3 s`; only Go `8,1`
  and `8,7 s`; everything invalidated (what any change costs today) `21,6` and
  `22,8 s`. Saving per event with a per-language fingerprint: a `go` change
  ~`13 s`; Rust toolchain with no Rust units ~`14,5 s`; worker, engine or
  `node` ~`3 s`; kivgraph binary, resolver version or entry format, nothing
  (they remain global). Alternation demonstrated: one `PATH` with `rustc` and
  `cargo` and another without them, with a corpus without Rust: A `hits=34`
  `7,1` and `7,0 s`; B `hits=0 misses=39` `20,8 s`; B again `hits=34` `7,1 s`;
  A again `hits=0 misses=39` `23,9 s` (one warm pass of `14,7 s` unexplained,
  treated as noise): every environment change is a full cold pass (terminal,
  agent host, daemon or IDE with different `PATH` values). A much smaller
  alternative: put the fingerprint into the entry address
  (`sha256(identity + local address + analyzer)`, one line); with it the same
  experiment gives A cold `21,0 s`, A warm, B cold `20,4 s`, B warm and A warm
  again (`7,19 s`, `hits=34`): the alternation stops going cold, for any
  component, including two builds of the binary. Price: disk (`204 MB` with one
  variant and `407 MB` with two in this corpus) and retention: every build of
  the binary changes the digest and would create a new variant, which today is
  overwritten; with `prune` at `30` days a contributor who builds `50` times a
  day would accumulate hundreds of GB, so it requires a shorter retention or a
  limit on variants. Cost of the per-language fingerprint: ~`250-350` lines and
  an ADR; split `AnalyzerFingerprint` into a common part plus per-language
  parts, adapt the value that #72 passes to the cache, bump `cacheEntryVersion`
  from `5` to `6` (one cold pass for everyone on rollout), the observation
  manifest as a sum; the independence between languages has support (unit
  identity per language, records per type, no cross entries) but it is an
  assumption to review unit by unit; as a side effect it would skip the Rust
  probes when there are no Rust repos (~`0,1 s` per pass). Frequency: `25` tags
  from `22` August to `5` September `2026` and then none until `v0.10.0-dev.1`
  on `15` November; in active periods and for anyone who builds the binary the
  executable digest dominates, global in any design; `go` ~monthly and `rustup`
  every six weeks, ~`13-15 s` per event, a few minutes a year. Verdict: the
  per-language fingerprint is not worth it now; environment alternation is the
  valuable case and is better fixed with the fingerprint-based address plus
  bounded retention; before deciding it is worth knowing which component
  actually changes, and `analyzer_changed` did not say. Options: (1) record why
  the fingerprint changes (done in #74); (2) fingerprint-based address with
  bounded retention (for example the last N fingerprints per unit); (3)
  per-language fingerprint, only if (1) shows that a single language changes
  often; (4) nothing.
- PR #74, amendment to ADR 0100: `AnalyzerFingerprint` is built in `9` parts
  (`resolver`, `binary`, `go`, `typescript`, `python`, `dart`, `java`,
  `csharp`, `rust`) with an `analyzerBuilder` that writes each line into the
  total hash and into the hash of its part; the value is the hash of the same
  bytes in the same order, so it does not invalidate any cache and
  `cacheEntryVersion` does not change. Each entry stores the hash of each part
  (`16` digits) in `analyzer_parts` (optional, `omitempty`). When an entry is
  rejected because of an analyzer change, `CacheReport.AnalyzerChanged` counts
  the parts that differ and `index --full` appends it to the `index.cache` line
  (`analyzer_changed=rust:34`); an entry that differs in several parts counts
  in each one; an earlier entry or a value the process did not compute counts
  as `unrecorded`. The process remembers the parts of the last `8` fingerprints
  it computed, by value (so the cache, to which #72 hands the observed value,
  names them without recomputing and there is no plumbing in `indexing`). Value
  unchanged: `AnalyzerFingerprint` against a copy of the previous
  implementation inside the same test binary, `5` option sets, all equal
  (throwaway test). Tests: `TestAnalyzerPartsNameTheOptionThatMoved` (`11`
  cases: resolver, Go tag, Go tests, unclaimed TypeScript, worker, Python path,
  Dart SDK, Java tool, C# project, Rust feature, two at once),
  `TestChangedAnalyzerPartsSaysNothingItCannotKnow`,
  `TestAnalyzerChangedNoteIsEmptyUnlessAnEntryWasRefusedForIt`,
  `TestRememberedAnalyzerPartsAreBounded` and
  `TestCacheReportNamesThePartsOfTheAnalyzerThatRefusedEntries`. Mutations:
  Rust folded into the Go part fails `2`; not remembering the parts, the
  entries not storing them and not noting the rejection fail the report test;
  unbounded memory fails its test. Real binary, corpus of `6` repos, `PATH`
  with and without `rustc` and `cargo`:
  `hits=0 misses=39 analyzer_changed=rust:34` on every switch (`20,2` and
  `20,8 s`) and `hits=34` in `7,0 s` in between. Additive: an optional field in
  the entry and a suffix on an informational line; `--json` and the manifest
  untouched. CI: the first run failed in
  `Verify the worker, web and landing packages` because of a `5000 ms` timeout
  of `imported-symbol-identity.test.ts > is credited to the repository that
  declares it`, in a PR that only touches Go (the second timeout of a test of
  that worker in CI, after mine in #66); `gh run rerun --failed` for that job
  and green. I did not run `make build`, `make test-ladybug` or Bazel locally.
- Investigation of fingerprint-addressed cache entries with bounded retention
  (origin of #75): are they worth it? Retention yes, on its own;
  fingerprint-based addressing not yet. Prior finding, measured: the cache
  already grew without a practical bound on every edit, because the entry path
  includes the local content, so editing a file writes a new entry and leaves
  the previous one, which only expires by age (`30` days since its last use).
  Editing a TypeScript source file of a copy of the corpus (the maintainer's
  repositories were not touched), `5` times: warm `34` entries `213 MB`
  `8,8 s`; after `1` edit `36` `318 MB` `14,0 s`; `2` `38` `433 MB` `16,1 s`;
  `3` `40` `538 MB` `14,6 s`; `4` `42` `652 MB` `16,2 s`; `5` `44` `757 MB`
  `15,1 s`; with the file restored `44` `757 MB` `9,2 s` with `hits=34` (the
  old entry still serves). Each edit adds `2` entries and `105` to `115 MB`.
  Extrapolation, not measured: `30` edits a day in a large package would
  accumulate ~`3 GB` a day and ~`90 GB` in steady state. Per-unit retention
  (keep the `K` most recent per unit identity): the unit is in the first bytes
  because of the field order of `cacheEntry`, so a `4 KiB` header is enough; on
  that cache of `44` entries and `758 MB`, `K=1` leaves `213 MB`, `K=2`
  `318 MB`, `K=3` `433 MB`, `K=5` `652 MB`; cost: `5.000` synthetic entries of
  ~`20 KB` in `191 ms` (deletion included), the `44` large ones of the corpus
  in `6 ms`; with `K=2`, `8` entries and `439 MB` were pruned and the next pass
  gave `hits=34 misses=5` in `7,7 s`. Retention changes neither format nor
  addresses and requires no cold pass; the entries are disposable; the full
  identity includes the resolution context, so profile alternation
  (`TestFactCacheKeepsAlternatingProfilesWarm`) is not affected.
  Fingerprint-based addressing: it fixes environment alternation (`13-15 s` per
  switch, measured) but there is no real usage data (the real home of this
  machine has no kivgraph configuration or `factcache`), it changes the
  addresses (bumps `cacheEntryVersion`, one cold pass for everyone), the
  variants (analyzer by content) share the `K` quota (`K` between `3` and `4`
  would be advisable) and `analyzer_changed` (#74) would start saying
  `no_entry` (the diagnosis would have to be redone, for example with the
  header of the most recent entry of the unit). Detail: `prune` runs before the
  commit of the pass, so with `K` in `prune` there would be `K+1` transient
  entries; that is why retention goes after the commit. Verdict: retention yes,
  and now; fingerprint-based addressing not yet, to be decided with data from
  #74. Options: (1) retention by unit identity, `K=3` (done in #75); (2)
  retention plus fingerprint-based addressing with a new diagnosis, only if #74
  shows real alternation; (3) nothing.
- PR #75, amendment to ADR 0100: after admitting entries (`factCache.commit`),
  `factCache.retain` keeps the `3` most recent of each unit and removes the
  rest (`cacheEntriesKept`, a constant, no new configuration key; in
  `factcache_retain.go`). Most recent = the last time it was written or served
  (a hit already touches the file). Unit = the complete identity with the
  resolution; it is read from the first `4 KiB`. Files the cache did not
  write, names that are not its own and entries whose unit is not in the header
  are neither counted nor removed (age rule); the ones admitted in that pass
  are never removed; `keep < 1` removes nothing; a pass that wrote nothing
  reads no headers. Tests: seven for `retain` (garbage in the directory, a unit
  longer than the header, different resolutions, `keep < 1`, the entry served
  last wins) and `TestFactCacheDoesNotGrowWithEveryEdit` (six edits through
  `Full`: never more than three entries, going back to the previous version
  hits and going back beyond the three misses). Mutations: no retention in
  the commit, ignoring the admitted ones, keeping the oldest, removing the
  resolution from the unit, allowing `keep < 1` and counting an unreadable
  header as a unit each fail their test. Real binary, corpus of `6`
  repositories, five edits: `34` -> `36` -> `38` entries and `213` -> `318` ->
  `433 MB`, then `38` and `433-442 MB` for the rest (without retention `44` and
  `757 MB`); the passes after an edit take `14,0-15,6 s` in both cases.
  Accepted cost: going back to the content before the last three versions is
  now a miss (`14,1 s`, `hits=32 misses=7`) and was a hit before (`9,2 s`).
  Limits: the `90 GB` are arithmetic and the cost of the headers came from a
  prototype, not from the final code; entries without a readable header are
  left to the `30` day age rule (investigated later: in practice they do not
  grow, see the investigation of the entries without a readable header). CI
  green on the first run. `make build`, `make test-ladybug` and Bazel were not
  run locally; the two new files are in `BUILD.bazel`.
- Investigation of the cache entries without a readable header (origin of
  #76): can they keep growing up to the `30` days? In practice no. The
  retention of #75 does not attribute an entry if its `unit` does not fit in
  the first `4 KiB`, and there are only three possible classes. Old formats:
  none, because `Version`, `Unit` and `Analyzer` are the first three fields of
  `cacheEntry` from the first commit (`2e99505`) until today (`git log -L` on
  the structure), so inherited entries are read, grouped and trimmed by `K`
  like the new ones. Unit identity of more than ~`4 KB`: the `unit` ends
  between byte `219` and byte `331` of `4096` in the `38` entries of the corpus
  (a margin of `12-19` times) and all `38` are read; a Go unit carries the
  root path twice, so it would take a root of more than ~`1,9 KB`: impossible
  on macOS (`PATH_MAX` `1024`), possible only on Linux and Windows with
  pathological paths. Foreign files with an entry name: the cache never writes
  them and the age rule applies to them. A new write always puts the `unit` in
  the header, so the class does not grow, at most it is a finite residue; the
  only real risk is that a future format change moves `unit` beyond the first
  KiB, and `TestFactCacheDoesNotGrowWithEveryEdit` would detect it (it uses
  real entries). Larger finding, measured: the identity of a unit includes the
  resolution context and the TypeScript registry includes the version of each
  package, so bumping the version of one package gives a new identity to all
  TypeScript units, and the rule of #75 (which counts an identity apart on
  purpose) does not trim the old ones. Bumping `ts-worker/package.json` (corpus
  copy) from `0.1.0` to `0.1.1`, `0.1.2`, `0.1.3`: warm `8,0 s` `34/5` `38`
  entries `433 MB` `34` identities; `0.1.1` `22,9 s` `8/31` `63` `596 MB` `59`;
  `0.1.2` `20,4 s` `8/31` `88` `760 MB` `84`; `0.1.3` `20,9 s` `8/31` `113`
  `923 MB` `109`; restored `7,8 s` `33/6` (the old entry serves). Each bump
  adds `25` entries and ~`164 MB` and costs a cold pass of ~`21 s`; the `34`
  logical units are still `34` but the identities go from `34` to `109`.
  Simulation over those `113` entries (`923 MB`) with a cap per logical unit:
  `3` leaves `86` entries and `630 MB`, `4` `111` and `793 MB`, `6` `113` and
  `923 MB` (maximum observed `6`; the `mtime` did not distinguish use well, so
  it was not used as evidence). Tradeoff: with a cap of `3`, restoring a
  version from `3` bumps ago would already be a miss; the choice is between
  alternating profiles and versions and a disk limit. Options: (1) a cap per
  logical unit of `N` entries counting all resolutions, on top of the `K=3`
  per identity (done in #76 with `N=6`); (2) leave it (age bounds it to `30`
  days); (3) a test that pins that `unit` stays in the header with a real
  entry (covered indirectly).
- PR #76, amendment to ADR 0100: `factCache.retain` applies a second rule after
  the one of three per identity: at most `cacheEntriesPerUnitKept` (`6`)
  entries per unit, counted across all its resolutions, from the most recent to
  the oldest. Unit = the identity without its resolution (`logicalUnit`, with a
  new `cacheResolutionSeparator` that `identity()` shares); the cap never goes
  below what one identity keeps and the entries admitted in that pass are not
  removed. No new configuration key and nothing invalidated. Tests:
  `TestRetainCapsAUnitAcrossItsResolutions` (four resolutions of two entries:
  the two oldest of the eight go and another unit stays intact),
  `TestRetainAppliesTheRuleOfAnIdentityBeforeTheCapOfTheUnit`,
  `TestRetainNeverCapsAUnitBelowWhatOneIdentityKeeps` and
  `TestRetainNeverRemovesAnAdmittedEntryToHonourTheCap`; the ones from #75 pass
  unchanged with the new signature. Mutations: no cap pass, a cap counting
  identities separately, a cap below one identity, admitted ones unprotected
  and the identity rule skipped each fail their test; the first version of the
  "no cap" mutation did not compile and ran nothing, and redone as a cap that
  never removes it fails `2` tests. Real binary, corpus of `6` repositories,
  eight version bumps: `34` -> `60` -> `86` -> `111` -> `136` -> `161` entries
  and `213` -> `1129 MB` up to the fifth, and then `161` entries and `1111 MB`
  stable on the sixth, seventh and eighth; without a cap it was `25` entries
  and `164 MB` per bump. Accepted cost: going back to a version older than the
  six most recent resolutions is a miss; after eight bumps restoring the
  original took `20,4 s` (`hits=8 misses=31`) where with three bumps and no cap
  it was a hit of `7,8 s`. Limits: a single corpus and a single series; the
  plateau of `1,1 GB` is the one of this corpus. CI green on the first run.
  `make build`, `make test-ladybug` and Bazel were not run locally (no new
  file).
- Investigation of the cache cap per language (origin of #77): does the growth
  from registry changes of Go and of the other languages need a cap too? No:
  the one from #76 already covers them. `identity()` appends
  `cacheResolutionSeparator` plus the registry context to every unit (for
  `go`, `rust`, `semantic` and `typescript` their registry, for any other
  `"none"`) and `logicalUnit` only cuts at that separator, so the cap of `6`
  entries per unit applies to all of them alike. What changes the identity in
  each language (read in the code): Go, module path -> repository@manifest, on
  adding, removing or moving a repository or module; Rust,
  crate@version=repo@manifest, on changing the version or the provider of any
  crate; TypeScript, name, version and paths of each package, on bumping the
  version of any package (measured in #76); semantic ones (Python, Dart, Java,
  C#), repository=root@digest of the tree, with any edit in any semantic
  repository. Measured with the binary of #76 in a temporary HOME with small
  repositories: Python (two repositories and a Go one), eight edits of `py1`
  only: each gave `hits=1 misses=2`, that is, the untouched `py2` is reanalyzed
  too; the entries of each Python unit go from `1` to `6` and settle at `6`
  from the fifth edit (`13` entries in total). Go (`go1` and `8` new Go
  repositories registered one by one): each registration changes the identity
  of all Go units; the entries of `go1` go from `1` to `6` and stay at `6`; the
  total grows as `6 x` Go units (`41` entries with `9`), not without bound.
  Rust: not run, there is no `rust-analyzer` on the development machine; by
  the code the path is the same as TypeScript (`crateRegistryName` includes
  the version of each crate). Limits: the cap is on the number of entries and
  not on bytes (a package of `65 MB` times `6` is `390 MB` per unit; in the
  corpus the plateau was `1,1 GB` with `161` entries); the test repositories
  are tiny (`0,13 MB` in total) and confirm the mechanism, not the size; there
  is no large Python, Dart, Java or C# corpus. Finding about the hit, not the
  disk: the semantic registry includes the digest of the tree of all semantic
  repositories, so any edit in one reanalyzes all semantic units, even those of
  untouched repositories; probably deliberate (`indexSemantic` passes
  `Providers: options.Repositories` to the loader, so the facts of one unit
  can depend on the content of another repository) but neither a test nor an
  ADR pins it; tuning it (tracking only the providers each unit read, as
  TypeScript does with `inputProvider`) is a redesign with a risk of stale
  facts. Options: (1) close it; (2) a test that pins that the identity of each
  kind of unit carries the separator (done in #77); (3) investigate the
  granularity of the semantic registry, only if there are users with several
  Python or Dart repositories and the pass after an edit is noticeable; (4) a
  cap in bytes as well as in count, only if the `1,1 GB` of the corpus is a
  problem.
- PR #77 (test only, no ADR amendment):
  `TestEveryUnitKindCarriesItsResolutionInItsIdentity` in
  `factcache_retain_test.go`. For Go, TypeScript, Rust, Python, Dart (they
  share the semantic kind) and the unspecified kind: the identity carries the
  separator exactly once and ends in the registry of its own kind and not in
  that of another; the unit of an identity is the identity of the unit;
  another registry gives another identity of the same unit (what the cap
  groups); and two different kinds or languages are not the same unit, so they
  are bounded separately. Mutations: the semantic kind losing its resolution
  fails; the unit of an identity being the whole identity fails; Rust reading
  the Go registry fails, but the first version of the test did not detect it
  (both registries changed at once and "another registry changes the identity"
  kept passing), and it was corrected with a check of the own registry every
  identity must end in. The unspecified kind has no registry (`analyseUnit`
  rejects it) and its resolution is the constant `none`. The file was already
  in `BUILD.bazel` and its dependencies (`facts`, `goworkspace`) were already
  listed. Nothing in the cache, the fingerprint or any output changes. CI green
  on the first run. `make build`, `make test-ladybug` and Bazel were not run
  locally; the Rust check is from the code.
- Investigation of the semantic registry with the tree digest (origin of #78):
  can it be tuned without a risk of stale facts? No, and it was demonstrated.
  What each loader reads outside its repository (`semantic.go:100-150` and the
  loaders): Dart reads files of other repositories (it receives
  `Providers: options.Repositories` and `externalDartTarget` asks the analysis
  server for the symbol at the provider's path; its facts carry `composed`
  pointing at that repository); Python (AST worker), Java and C# receive only
  `unit.repository`, and in the AST worker `targetId` are internal
  identifiers, with no targets in another repository; but Python with pyright,
  Java and C# can read siblings through the environment (editable installs,
  `ProjectReference`, `includeBuild`) without the loader knowing, and it could
  not be tested (there was no `pyright`, `scip-java` or `dotnet` on the
  development machine). Demonstration with Dart (`d1` consumes `d2` through a
  path dependency; the provider renames `addOne` to `addTwo` and the consumer
  is not touched): with the current registry `d1` is reanalyzed
  (`hits=0 misses=2`) and its facts change (`references 8 -> 7`, composed
  target `addOne -> addTwo`, correct); with a prototype that keeps name and
  root and removes the digest, `d1` is served from cache (`hits=1 misses=1`)
  with `references=8` and the target `addOne` (stale). The digest is needed
  for Dart. Python, AST worker: eight edits of `py1` and `6` entries of `py2`
  with a different identity each give identical facts (`1` distinct out of
  `6`): there the digest is superfluous, but that only holds for the weakest
  mode. A gap that already existed, reproduced with the current binary: the
  registry only covers repositories with a semantic language and Dart looks at
  all registered ones; with the provider registered only as Rust, the same
  rename left `d1` served from cache (`hits=1`) with the stale target `addOne`
  (contrived, but incorrect facts and not just cost). Why a safe tuning is not
  trivial: the exact dependency of a Dart unit is the set of repositories its
  imports name, not only those in `composed`; an unresolved reference stops
  being one if the provider adds the symbol and then there is no `composed` to
  give it away; TypeScript solves it with `inputProvider` per requested
  package, including the unresolved ones; replicating it in four ecosystems
  plus the reads through the environment is a redesign with a risk of stale
  facts. Cost paid today: an edit in one semantic repository reanalyzes all the
  others even if they do not reference it; a few seconds in the test
  repositories, and by inference much more in Java with `scip-java` (not
  measured). Limits: Dart and Python AST were measured, Java, C# and pyright
  are reasoned from the code; the case of the provider as Rust uses a minimal
  `pubspec` and one consumer. Options: (1) leave it and document why with a
  comment in `semanticRegistryName` and a regression test (done in #78); (2)
  cover the gap by making the registry of the Dart units include all
  repositories that can be providers, at the cost of more invalidation for
  Dart; (3) design the dependency by imports only if a real case with a high
  cost shows up.
- PR #78 (test and comment, no ADR amendment):
  `TestDartConsumerIsAnalysedAgainWhenItsProviderChanges` in
  `factcache_dart_provider_test.go`: a Dart consumer with a path dependency on
  a Dart provider, with the cache enabled; cold both are analyzed, warm both
  are served (so the last check does not pass because nothing was ever served)
  and after renaming a symbol in the provider the consumer is reanalyzed too.
  The `package_config.json` is written by hand so it does not need `pub`; it
  is skipped without the `dart` SDK, like `TestFullBuildsDartSemanticFacts`. A
  comment in `semanticRegistryName` explains why each tree is digested, which
  loaders could do without it and for which it cannot be demonstrated, and
  records the known gap. Mutations: without the digest it fails with
  `Hits:1 Misses:1` (the stale serve of the demonstration) and with a constant
  digest it fails too. Nothing in the cache, the fingerprint or any output
  changes; the gap is not fixed here. In CI it is covered by the semantic
  coverage job (which has `dart`). CI green on the first run. `make build`,
  `make test-ladybug` and Bazel were not run locally; the file is in
  `BUILD.bazel`.
- Investigation of the gap of the Dart provider registered without a semantic
  language (origin of #79): can it be covered without invalidating too much?
  Yes. The Dart loader can only read the packages named by the
  `package_config.json` of the unit plus its own root, which gives the exact
  dependency: (a) the registered repositories, of any language, that contain
  the root of a package of the `package_config` (`inputTree` in the local
  address of the unit) and (b) the repositories named in its composed targets
  (entries recorded and validated on load, like `inputProvider` in
  TypeScript). Scenario A (provider `d2` registered as Rust; units `d1` and
  `d3` Dart and `x` Go): current, edit of an unrelated Go repository
  `hits=2 misses=1`, renaming the symbol `hits=3` served stale, `d1` sees
  `addOne`; (a)+(b) `2/1`, `2/1` and `addTwo` (correct); digest of all
  repositories in the registry `0/3` (reanalyzes the three units on an
  unrelated edit), `1/2` and `addTwo`. Scenario B (the provider is a Dart
  repository; `d1`, `d2`, `d3` and `x`): current `hits=1 misses=3` on the
  unrelated edit and on the rename; (a)+(b) added to the current registry the
  same; (a)+(b) plus a membership-only Dart registry `hits=3 misses=1` and
  `hits=2 misses=2` (`d2` and `d1`), with `d1` seeing `addTwo` (from `4,1` to
  `2,6 s`). Scenario C (provider registered after analyzing the consumer):
  current, `d1` served with `[]` (also stale); (a)+(b), reanalyzed and sees
  `addOne`; membership of all repositories, correct but it also reanalyzes
  `d3`. A consumer with a relative import that leaves its package (`d3`) gave
  `references=0`: the analysis server does not resolve outside the context
  root, so there is no observable dependency (it could change with another
  SDK). Python, Java and C# keep the global digest (they can read siblings
  through the environment). Limits: only Dart `3.12.2`, four small
  repositories; `include_external` and `include_sdk` were not exercised; the
  prototype exported `dartloader.PackageRoots` and added code in
  `describeInputs`, and the diff had to be reviewed and not only its results.
  Options: (1) close the gap with (a)+(b), additive (done in #79); (2) reduce
  the Dart registry to membership, in a separate PR after `1` and only safe on
  top of it; (3) leave it as a known gap.
- PR #79, amendment to ADR 0100: `dartloader.PackageRoots` lists the roots of
  the packages of the `package_config` (except the repository itself and only
  if they exist); `dartProviderRoots` (new `factcache_dart.go`) returns the
  registered repositories that contain one of those packages (a directory that
  only shares the first characters of a root does not count; a package reached
  through a link is the repository it points at) and the ones a target of the
  facts names; `describeInputs` adds their trees to the inputs of the unit. The
  former are known before the analysis and go into the address (a provider
  that gets registered is another address); the latter are recorded with the
  entry and checked when serving it. The comment of `semanticRegistryName` no
  longer lists the gap. Real binary, one consumer, a provider registered as
  Rust and an unrelated Go repository: the edit of the Go repository serves the
  consumer (`hits=2 misses=1`); the rename reanalyzes it and it sees `addTwo`
  (`hits=2 misses=1`; before `hits=3` and `addOne`); registering the provider
  afterwards reanalyzes it and it sees `addOne` (before, served without a
  target). Tests: the ones from #78, `...IsRegisteredUnderAnotherLanguage`,
  `...IsRegisteredLater`,
  `TestDartConsumerIsNotAnalysedAgainWhenARepositoryItDoesNotReadChanges`
  (guard against invalidating too much),
  `TestDartProviderRootsAreTheRegisteredRepositoriesThePackageConfigReaches`
  (no `package_config`, a root that shares a prefix, a package inside another
  repository, a link, targets of the facts, a target in the unit itself or in
  a repository nobody registered) and
  `TestPackageRootsListsTheOtherPackagesThatExist`. The three regression ones
  fall without the fix or with the digest of all repositories. Mutations:
  without provider inputs the two registration ones fail; taking all
  repositories fails the guard and the roots one; a prefix without a separator,
  links not followed, the unit not excluded and composed targets ignored each
  fail the roots one; two of the first did not compile and ran nothing, and
  were redone using the variable. `check-docs` failed once because of `2`
  lines of the ADR over `84` columns (rewrapped). Effect on deploy: a Dart unit
  with a provider among the registered ones changes address once and is
  reanalyzed once. The registry digest is not removed. CI green on the first
  run. `make build`, `make test-ladybug` and Bazel were not run locally; the
  new files are in `BUILD.bazel`.
- Investigation of reducing the registry of the Dart units to name and root
  (closed, no PR): can it be done without a risk of stale facts? No, and there
  is a counterexample. An oracle compares, for each change, the facts of the
  consumer unit served by the cache with those of a cold analysis with the
  change already applied (if they differ, the cache is stale), with the binary
  of `main` (#79, current registry) and with a prototype with the Dart
  registry reduced to name and root (membership of the semantic repositories,
  no digests; the Dart repositories record a `dart` registry of their own)
  relying only on the inputs of #79. Results. The provider renames a symbol:
  the change moves the facts; both reanalyze and give the same facts. An
  unresolved reference becomes resolved: it moves the facts; both equal. A
  transitive provider changes a type: it does not move the facts; the reduced
  one reanalyzes anyway (through `package_config`) and is equal. The included
  options of a sibling change: they do not move the facts; the reduced one
  serves from cache (`hits=1 misses=1`) and is equal. A sibling reached through
  a symbolic link inside the consumer (`d1/lib/shared` -> `d2/lib`, the
  consumer imports `shared/p.dart`): it moves the facts; `main` reanalyzes and
  is correct; the reduced one serves `d1` from cache (`hits=1 misses=1`) with
  facts different from the fresh analysis (STALE). The analysis server follows
  the link but that path is not in the `package_config` and does not appear as
  a composed target because it hangs from the consumer's root; only the global
  digest of the sibling covered it. Why it is not an isolated case: the inputs
  of #79 cover what is declared (`package_config` and composed targets); any
  other way of reading files of another repository is left out, the global
  digest covers everything by construction at the cost of invalidating too
  much, and there is no closed list of ways to reach a sibling. Saving that
  would be lost: an unrelated edit would reanalyze `1` unit instead of `3` in
  the prototype (from `4,1` to `2,6 s`). Additional findings, outside the
  registry: `dart.include_external_packages: true` makes the whole pass fail
  with Dart `3.12.2` (`failed Dart analyzer request ... INVALID_PARAMETER
  'params.packageRoots'. Expected to be Map`), and it fails the same on
  `main`, so it is not from the prototype; and by reading the code (not
  measured) the fingerprint only includes the strings of `dart_analyzer` and
  `dart_sdk`, not the content of the SDK, so an SDK change would invalidate
  nothing. Limits: Dart `3.12.2`, tiny repositories, one consumer per
  scenario; `include_external_packages` (broken), `include_sdk` and pub
  workspaces were not exercised; four forms of dependency passing does not
  prove that others do not exist. Options: (1) leave the registry as it is,
  because the saving (`2` Dart units per unrelated edit) is not worth the risk
  of stale facts with no known bound; (2) cover symbolic links by making the
  tree digest of the consumer record where they point and taking the
  repositories that contain them (a change in `TreeDigest`, with its risk, and
  it closes only that form); (3) fix `include_external_packages`, an
  independent failure. Decision: closed with `1`, no PR.
- Investigation of whether the fingerprint includes the content of the Dart
  SDK, and PR #80 (`60fa77e`, amendment to ADR 0100). It did not: the `dart`
  part only hashed the strings of the options. With the default command
  `dart`, a different `PATH` (terminal versus agent host, SDK `3.12.2` and
  `3.13.1`) or an in-place upgrade served the entries of the other SDK
  (`hits=2 misses=0`, no `analyzer_changed`; with the same command and the
  link changed, `hits=3` different from a fresh analysis). The facts do depend
  on the SDK: unresolved references name the file of their library; with
  `dart.include_sdk` the SDK is one more repository (`rich` `109` versus `52`
  references). Between `3.12.2` and `3.13.1` the facts of four repositories
  were identical except for that absolute path. Flutter's `dart` is a script,
  so hashing the file does not cover the SDK. Fix: `dartAnalyzerFingerprint`
  (`factcache_dart.go`) resolves the command like the loader (options of the
  command, otherwise `dart_sdk`); a program called `dart` is identified by its
  resolved path (links followed) and the output of `dart --version`, once, in
  parallel with the rest of the fingerprint and from the neutral directory of
  #71; another command, by its content; `absent`, `failed` and `unconfigured`
  are stable values. A change shows up as `analyzer_changed=dart:N`. Measured
  cost, best of 15 calls, interleaved with `main`: `46,6` versus `47,6 ms` and
  `45,6` versus `47,1 ms` (~+1 ms, the query runs concurrently). Real binary,
  two Dart repositories: `main` serves `hits=2` with the other SDK; with the
  fix `hits=0 misses=2 analyzer_changed=dart:2` and then `hits=2`. Seven tests
  with fake interpreters (it follows the SDK behind the command, the `PATH`
  and only the `dart` part, links, absent/fails/unconfigured, a single
  question from the neutral directory without the arguments of the command,
  another command by content and without asking it for the version, falling
  back to `dart_sdk`); seven mutations each fail (the "ask every command for
  the version" one did not compile at first and ran nothing: redone with
  `&& false`). CI green on the first run. Effect on deploy: once, the Dart
  entries are reanalyzed (and the rest of the languages too, because the
  fingerprint is a single one). Limits: only two Flutter SDKs on macOS;
  `include_sdk` and `include_external_packages` were not exercised with the
  fix; `dart --version` of a script that downloads its SDK can take up to the
  `30 s` deadline and then the value is `failed`; `make build`,
  `make test-ladybug` and Bazel were not run locally.
- Investigation of `dart.include_external_packages: true` and PR #81
  (`1e427aa`). Does it break the pass with other SDKs? Yes, with both:
  `3.12.2` and `3.13.1` fail the same (`INVALID_PARAMETER
  'params.packageRoots'. Expected to be Map; found [".../d2"]`, a repository
  `d1` with a path dependency on `d2`; with the option at `false` the pass
  passes). Cause: `appendNavigationReferences` sent `packageRoots` in
  `analysis.setAnalysisRoots` as a list and the protocol defines it as
  `Map<source directory, package root>` (an override of the `package:`
  resolution), so no SDK could accept it. It was redundant: the server
  resolves `package:` through the `package_config` without it. The option
  does do something useful: `ExternalPackageRepositories` publishes the
  external packages as providers. Binary with the parameter removed, `d1` as
  the only repository: option `false` `refs=0 unresolved=10`; `true`
  `refs=5 unresolved=5` plus the unit `dart-package:provider_pkg`; with `d2`
  also registered the facts of `d1`, `d2` and `rich` are identical with `true`
  and with `false` (only the unit is added). Fix: remove the parameter,
  `dartPackageRoots` and the argument that reached
  `appendNavigationReferences` (about `12` net lines). Test
  `TestRunResolvesAPackageDependencyWhenExternalPackagesAreIncluded` (real
  `dart`, skipped without an SDK; consumer and provider in a temporary
  directory with a hand-written `package_config`; both values of the option;
  it requires a call with a target): it fails on `main` with the error above
  and passes with the fix. The assertion on `dartPackageRoots` was removed
  from `TestDartPackageRootsReadsPubPackageConfig`. Mutation: the only
  possible one is putting the parameter back, which is the test that fails on
  `main`. A `dart-external=true` was already in the fingerprint, no cache
  changes. CI green on the first run. Limits: only repositories with a path
  dependency; real `pub-cache` dependencies, pub workspaces and the number of
  symbols the external package publishes (`0` in the fixture) were not tested;
  `make build`, `make test-ladybug` and Bazel were not run locally.
- Investigation of the signature cache for the file digests of the
  fingerprint, repeated after #80 (closed, no PR, documented limit). Best of
  10 calls, binary of `31 MB`, `node` of `133 MB` and the TypeScript engine of
  the machine: digest of the binary `11 ms`, of `node` `48 ms`, of the engine
  `14,6 ms`, `dist` modules `0,4 ms`; sum `~74 ms` of the `~113 ms` of
  `AnalyzerFingerprint` (the rest are process probes: Rust, `go env`,
  `dart --version`, which the signature does not touch). How many times it is
  paid: `indexing.Full` computes the fingerprint twice per pass (observe and
  verify) and `observeSources` calls it on every observation of the watcher.
  With a direct `node` worker it is `~150 ms` of digests per pass against
  `~7 s` warm (`~2 %`); with the binary alone `~22 ms` (`~0,3 %`); with the
  published bundle the worker goes through a shim that does not hash `node`,
  so the possible saving is the digest of the binary, `11 ms` per call.
  Options: (1) an in-process memo keyed by `(path, size, mtime, ctime,
  inode)`, `~40` lines: it saves one digest on the second call of a CLI pass
  and all of them after the first in a daemon; the mid-pass verification still
  detects changes because the signature changes; (2) a memo across processes:
  a state file, invalidation rules and an ADR, it saves `~150 ms` to whoever
  launches `node` directly and `~22 ms` to the rest; (3) leave it. Decision:
  `3`; the benefit is not worth an ADR or persistent state. `1` is reasonable
  if it is measured that the event latency of the daemon matters (not
  measured). Limits: the `typescript` of this machine weighs `~1 MB` and a
  `@typescript/native` engine was not measured; the corpus could not be used
  because its configuration calls `kivgraph-ts-worker`, which is not on the
  `PATH`, so the `~2 %` comes from the warm pass of `7 s` measured earlier;
  timings from a single machine with variable load.

### Checklist lot (2026-11-17, PR #82 to #98)

Done, all with CI green and merged (without `make test-ladybug` or a full Bazel
build locally; the Bazel tree does not build on the development machine
because of the tree-sitter C code):

- **#82**: `opentelemetry-go` 1.40.0 to 1.47.0: closes the 5 `go.mod` alerts;
  `govulncheck` with nothing reachable.
- **#83**: landing: `astro` 7.2.1 to 7.4.12 (RCE through AVIF),
  `@astrojs/node`, `zod`, transitive ones; `pnpm audit` clean. Starlight stays
  at `0.41.7` (`0.43` removes the type of `virtual:starlight/user-config`); an
  empty `i18n` collection so the build stays free of warnings.
- **#84**: web: `vite`, `vitest` 4.1.12, `shadcn`, `tailwindcss` and
  transitive ones, `development` only; `three`, `react`, biome `2.6` and
  playwright untouched (with `three` `0.187` one renderer test fails). 1
  moderate `minimatch` alert remains through `shadcn`.
- **#85**: ts-worker: `vitest` 4.1.12 and `vite` 8.3.8 as a direct dependency
  (the transitive one was `8.2.0`, below the `8.2.4` patch).
- **#86**: `.gitignore` for the `mcp-*.json` and `raw/` of `self-repo`.
- **#87**: **C6 confirmed and fixed**: `Strings()` and `StableKeys()` hand out
  the table by value and the `AddCleanup` hung from the snapshot; when a
  `Publish` replaced it, the GC unmapped under a reader (`"Helper"` read as
  `\x00` x6). The anchor (`mapping`) is shared by the snapshot and both
  tables; `MapSnapshot` receives the release function. Mutations: anchor out
  of each table, and cleanup on the snapshot.
- **#88**: **C8 confirmed and fixed**: with FUSE (ntfs-3g in Docker)
  `fallocate` answers `Not supported` and the 512 MiB reservation was not
  created, so the store did not open. `EOPNOTSUPP`/`ENOSYS` fall back to
  writing zeros (`writeZeros`, which already existed in `platform_other.go`);
  `ENOSPC` is still an error.
- **#89**: **Defect of #52**: `UnclaimedTypeScriptSources` counted the
  sourceless `tsconfig.base.json` as claiming the whole tree. With the binary:
  `x.ts` outside the `include` of its package was not indexed even with
  `include_unclaimed_sources: true`; now it is (`symbols=2` to `4`).
- **#90**: `MODULE.bazel.lock` regenerated (Go `1.26.11`);
  `bazel build //internal/storage/generation:all` and the `hotsnapshot` and
  `generation` tests pass.
- **#91**: **H7**: the `agent-e2e` harness passes `GOMODCACHE` to `kivgraph`
  and fails if it does not exist. `FINDINGS.md`: P1 is by design
  (`indexer.Full` aborts on a load diagnostic so it does not publish an
  incomplete graph); T1 and T3 cannot be fixed from the repository (the
  prompts are the input of the published sweep, `raw/` is not there).
- **#92**: limits of the fact cache fingerprint written in ADR 0100
  (amendment) and `docs/installation.md`.
- **#93**: **D8 reproduced and fixed** with Bun `1.4.4` and Node: the OpenCode
  plugin did not listen for the `error` of `stdin`; with a gate that exits
  before reading, the `EPIPE` killed the host process. The Oh My Pi one
  already listened.
- **#94**: `DiscoverGo` ignores a `go.mod` that does not parse under
  `testdata/`, `_x` or `.x` (the go tool never enters them); anywhere else it
  is still an error.
- **#95**: **D5, the half that is a defect**: the refusal of a native tool
  (`Grep`, `Glob`, subagent) no longer sends a `KIVGRAPH_DISABLE_HOOK=1` "in
  front of the command" that does not exist; it says the user should start the
  session with the variable. A research subagent is still always refused (ADR
  0077).
- **#96**: **D2**: on Linux `daemon install` ran `enable --now`, which does
  not restart a live unit; now `enable` and `restart`. The test with a fake
  `systemctl` runs in Docker (Alpine, linux/arm64) and fails on `main`.
- **#97, #98**: **Phase 7**: `internal/gitdiff` and `kivgraph impact` (ADR
  0125).

Blocked or not done, with the reason:

- Stable release: it is the next step (see below); needs an explicit decision
  by the maintainer. 1d (renaming the Go module): an outward-facing action,
  not ordered.
- D3 (HTTP sessions of the daemon): parked by the maintainer; needs a relay
  and a real client.
- D6 (`skill install` on Windows): no Windows machine.
- T1 and T3 (prompts and transcripts of `self-repo`): they would change the
  published experiment; a new sweep is needed (~`$37`).
- Decision on `signature` as the default: ADR 0121 asks for a test on a
  network filesystem; there is none.
- Report to `typescript-go`: **dropped** by the maintainer (2026-11-17).
  `microsoft/typescript-go` is archived and reports go to
  `microsoft/TypeScript`; Kivgraph does not need it (ADR 0124). The
  self-contained repro is in a draft kept outside the repository: it needs
  `@types/node` in the program and the frequency grows with the number of
  sources (49/51 over 100 loads with 300 files, 0 without `@types/node`). This
  replaces the earlier mentions of the report as pending.
- Phase 6 candidates: excluding only the directory that `go` rejects (needs a
  decision on a partial module load and a field from discovery to the facts),
  the repeated `-deps`, `PARALLEL` in COPY, the pnpm `CONTAINS_FILE` and the
  `0,20 s` of `build` (micro-gains; the `8 s` target is already met),
  `fuzz/target` in `exclusions` (it lives in the maintainer's local
  configuration) and the garbage `doctor` leaves in `t.TempDir` (a race with a
  macOS service in the tests).
- Memo of the fingerprint digests, per-language fingerprint, entries addressed
  by fingerprint and a cap in bytes: conditional on data that has not shown up.

Method findings: long reads in the editor (`read` without an end of range)
dumped whole files three times; `gh pr checks --watch` can return before all
the checks are registered, so the automatic merge uses the exit code of
`gh pr checks` (`8` is pending) and retries; the `grep` of `rtk` does not accept
`-q`, and a version of the merge loop that depended on it would have merged
with CI red (it was killed before it reached any PR).

#### Next step: the stable release

`main` is at `457548b` with PR #82 to #98 merged and CI green. It is what
unblocks the external part that is still open: `releases/latest` does not exist
until there is a stable one (the last one published is the prerelease
`v0.10.0-dev.1`, and the README one-liner without a version fails), and 1c and
the "Done when" of phase 1 ask that `install.sh` and `kivgraph update` find it.

Order of work, following `.claude/skills/publishing-releases/SKILL.md` (which
overrides this note):

1. Decide the number. There is a new command (`impact`), which the table in
   the skill counts as MINOR; check it against `git fetch origin --tags`,
   `gh api repos/<owner>/<repo>/releases/latest` and the list of tags, not
   against what is remembered here. The sentence on what whoever updates gains:
   `impact`, the space reservation on filesystems without `fallocate`, the fix
   of the snapshot mapping, TypeScript indexing outside the `include` of a
   package, the restart of the systemd unit, the OpenCode plugin that no longer
   kills the host and the patched dependencies.
2. Prepare the sources: `internal/version/version.go`, the note
   `landing/src/content/releases/vX.Y.Z.md` (in English; `requires_reindex` if
   the change to the snapshot anchor or to the fingerprint forces a reindex:
   the Dart fingerprint changes once and reanalyzes), install examples and
   `server.json` if applicable.
3. Gates of section 3 of the skill (including `make test-ladybug` and
   `make lint-ladybug`, which were not run locally in these PRs) and bundle
   reproducibility.
4. Commit, integration and tag `vX.Y.Z`; the workflow builds, verifies and
   publishes.
5. Check with a temporary `HOME`: `install.sh`, `kivgraph version`,
   `kivgraph update` ("up to date") and `init`, `doctor`, `index --full`.

Published (2026-11-17): PR #99 merged (`f6986b3`) and annotated tag `v0.10.0`
on that SHA. Number `v0.10.0` (there was no `releases/latest`; the highest tag
was `v0.10.0-dev.1`; it is the stable form of the series the prerelease
opened). Note `landing/src/content/releases/v0.10.0.md` with
`requires_reindex: true`; three install examples pinned to `v0.10.0`. Local
gates all green (`make coverage` `81,2 %` over the floor of `79,5 %`,
`semantic-coverage` with scip-java, scip-dotnet, pyright and rust-analyzer
installed, `-race`, node, reproducible macOS bundle with `780` identical
files); running them needed `pnpm` pinned outside corepack
(`npm_config_pm_on_fail=ignore`).

The tag run (`44088567636`) passed entirely: verify, the three build rows,
`Publish GitHub release` and the three published smoke rows (Linux, Windows and
macOS); the MCP Registry job was skipped, as expected. The release is public,
not a draft, not a prerelease, it is the `latest` of the API, and it has the
`11` assets; `SHA256SUMS` verifies the `10` files it lists from outside the
workspace.

Checked with a temporary `HOME`: `install.sh` with `KIVGRAPH_VERSION=v0.10.0`
installs `0.10.0`; the one-liner without a version
(`releases/latest/download/install.sh`) now works and also installs `0.10.0`;
`kivgraph version` says `0.10.0` and `kivgraph update` "is up to date
(0.10.0)"; with a copy of a real Go repository, `init`, `doctor` (`PASS`),
`index --full` (publishes generation `000001`) and `doctor` again (`PASS`). A
first attempt failed because of the preparation, not the product: the
temporary `HOME` of macOS hangs from `/var`, a symbolic link, and `doctor`
refuses to register paths with symbolic components (by design); it was repeated
with the resolved path. Only the macOS arm64 binary was tested from outside;
Linux and Windows, through the smoke jobs of the workflow.

With this, phase 1 only has 1d open. Still not ordered: 1d, the report to
`typescript-go` and the decision on `signature`.
- 6a. Per-stage profile of `rebuild.Run`; a new report in `benchmarks/`.
- 6b. Attack the largest stage without changing the schema or the layout of
  generations.
- 6c. Only if that is not enough: an ADR for an updatable HotSnapshot, with the
  contract of ADR 0056 and with `integrity` and the probes running.
- Open candidates: `0,20 s` of `build` outside its stages; `PARALLEL` in the
  COPY statements; the pnpm `CONTAINS_FILE` failure; whether the `-deps`
  repeated per Go unit can be avoided; excluding only the directory that `go`
  rejects (today a module with one loses all its packages: a field from
  discovery to the unresolved facts is needed); a malformed `go.mod` inside
  `testdata/` still fails discovery; the worker fingerprint does not cover a
  worker launched by a shim that hides its `dist` (the one on the `PATH` and
  the one of a bundle: only shim and executable; in the bundle that is enough,
  the binary changes with it) nor a worker outside a `dist` directory
  (documented limit, see the investigation after #69; the TypeScript engine
  is already covered in #69); `.cjs`, `.cts`, `.mts` or `.ts` entries (also
  with the `tsx` loader) contribute neither their bytes, nor `dist`, nor the
  engine, and a `--require`/`--import` argument does not contribute its
  content (documented limit, see the investigation of the `.cjs` and `.ts`
  entries); the first pass with the `pnpm` fallback fails once with
  `analyzer configuration changed` and the second one works (documented limit,
  see its investigation; no stale facts); a live daemon whose binary is deleted
  (package manager, `rm`) fails `5` times to observe and to reindex and gives
  up (`gave up`) until it is restarted; a replacement converges (documented
  limit, see its investigation); a signature cache for the file digests of the
  fingerprint (documented limit, not done: ~`0,3 %` of a pass with the binary
  alone and ~`2 %` with a direct `node` worker; see the investigation after
  #81); cache entries addressed by fingerprint (it would fix the `PATH`
  alternation between processes; per-unit retention is already in #75, so
  only the address is missing, which changes `cacheEntryVersion`, makes
  `analyzer_changed` say `no_entry` and waits for data from #74; see the
  investigation of fingerprint-addressed entries); a per-language fingerprint
  of the fact cache (it would keep a `rustup update` from cooling Go and
  TypeScript and would allow skipping the Rust probes in passes without Rust;
  it changes `cacheEntryVersion`, the manifest and requires an ADR; only if
  `analyzer_changed` from #74 shows that a single language changes often); a
  semantic registry with the digest of the tree of all semantic repositories
  (any edit reanalyzes all semantic units; needed for Dart and not tunable
  without risk, see the investigation of the semantic registry; the gap of a
  Dart provider registered without a semantic language is already covered in
  #79); reducing the registry of the Dart units to name and root (investigated
  and rejected: it is not safe, a sibling reached through a symbolic link
  would be served stale, see its investigation);
  `dart.include_external_packages: true` (solved in #81); the Dart
  fingerprint already includes the SDK (#80); a cap on the cache in bytes as
  well as in count (only if the `1,1 GB` of the corpus is a nuisance);
  reporting to `typescript-go` that `isSourceFileFromExternalLibrary` changes
  between loads of the same program for the default library files; deciding
  whether `signature` (ADR 0121) becomes the default after testing it on a
  network filesystem; an ADR for a shared traversal (bound: ~`86 %` fewer
  entries per pass, not less time; without measuring how much traversing
  weighs against hashing in a pass; see the investigation of the
  fingerprints: hashing dominates, so the value of the shared traversal is
  low); `fuzz/target/` and a test output directory of `49.057` entries are
  still unmarked and in `exclusions`; a `doctor` with a registered Go
  repository leaves garbage that prevents cleaning `t.TempDir` on macOS (a
  system service writes into the temporary `HOME`), so its tests do not run
  it.

Done when: a pass after an edit below `8 s` (proposed threshold) with content
equal to that of a clean rebuild.

### Migration to the fork (2026-11-17)

Done:

- PR #1 of the fork: all of `main` of the development repository (170 commits,
  history preserved), merge commit `56dc1a3`.
- PR #2 of the fork: `update`, installers, build scripts, `server.json`,
  README, docs, landing and skill read from the fork; ADR 0126; merge commit
  `db209bc`. The MCP Registry name, the module path and ADR 0118 do not
  change.
- Annotated tag `v0.10.0` on `db209bc` (not on `f6986b3`, which is where it is
  in the other repository). Release published in the fork with 11 assets,
  `releases/latest` = `v0.10.0`. The macOS jobs were cancelled twice for lack
  of a hosted runner and were rerun; the published smoke of `darwin/arm64` was
  checked by hand with a temporary `HOME`: `install.sh`, `version` (commit
  `db209bc`), `update` up to date, `init`, `doctor` PASS, `index --full`
  published.

- The development repository: PR #100 (`1b2cabb`) puts the notice of the move
  in the
  README with the install commands of the fork; repository archived. Its
  binaries keep looking for updates there: reinstall once.
- Working clone: `main` tracks `official/main` (`db209bc`),
  `remote.pushDefault=official`; `origin` is kept, read-only.

- Release run `44106777659` green on the third attempt, with the three
  published smokes (`linux/amd64`, `windows/amd64`, `darwin/arm64`); `origin`
  removed from the working clone.
- Branches inherited from upstream deleted from the fork (14, all on the same
  commit as their namesake in `Luqueee/kivgraph`); only `main` remains.
- PR #3 of the fork: this document, translated from the Spanish working note,
  which no longer exists; from here on it is updated in the repository.
- PR #4 of the fork: the moderate `minimatch` alert left open by #84 is closed
  with a scoped override in `web/pnpm-workspace.yaml`
  (`@ts-morph/common>minimatch` to `^11.0.1`); `shadcn` `4.23.0` is still the
  latest and still pins `ts-morph` `26`. `pnpm audit` in `web` is clean.
- The Bazel launcher installed for #90 was removed from the development
  machine; the other gate tooling had been removed after the release.

Remaining (maintainer's decision):

- Dependabot alerts is disabled in the fork (in the development repository it
  was
  what warned about dependencies); issues too. It is enabled in the repository
  settings.

Closed without doing, by the maintainer's decision (2026-11-17):

- `v0.10.0-dev.1` is not tagged in the fork: its commit predates the repoint.
- 1d: the Go module path is still `github.com/Luqueee/kivgraph`. This replaces
  the earlier mentions of 1d as pending.

#### Pull request to the original project (2026-11-17)

The rule that nothing is proposed to `Luqueee/kivgraph` was lifted by the
maintainer: the work goes back as `Luqueee/kivgraph#221`, from the branch
`feat/upstream-the-fork-work` of the fork. Its `main` had not moved from
`259fb00`, so there was nothing to resolve. Open, CI green there (12 checks),
waiting for its owner.

- It carries the whole history (178 commits, 283 files) plus one commit,
  `907e245`, that reverts `b64fb8b` and `7bf39cd`: update source, installers,
  bundle and registry metadata, install commands and the publishing skill
  name the original project again, the MCP Registry job is ungated, and the
  first-run telemetry is back. Those files are byte-identical to `259fb00`.
- Left out of it: ADR 0118 and ADR 0126, this document, and the
  `v0.10.0-dev.1` note, whose content is folded into the `v0.10.0` one.
- Not exercised: the telemetry request against the real collector, on
  purpose. `make test-ladybug` and `make coverage` were not rerun on the last
  commit.
- Not checked, outside both repositories: whether the collector accepts
  events from `0.10.0`, and the redeploy of the site with its new
  dependencies.
- The fork does not change: its `main` keeps publishing from itself and
  reporting nothing. The branch stays until the pull request closes.
- A second commit on that branch only, `60edc5e`: `main` of the original
  project requires a check named `Verify Go, web and landing packages`, and
  `7c3984d` had split that job in three (`Lint Go and run the race tests`,
  `Verify Go`, `Verify the worker, web and landing packages`), so the pull
  request waited for a status that never arrived with every real check green.
  The `verify` job carries the old name again there, and the six required
  checks report success. The name is no longer accurate (that job runs only
  the Go suite); a comment on the pull request gives the three names and
  suggests requiring them, or `CI result` alone, after merging. The pull
  request still shows as blocked by a rule that cannot be read from outside.

#### The site catches up, and the tree names the official project (2026-11-17)

- PR #7 and #8 of the fork (`c4c7a99`, `983da65`): the site and the reference
  say fifteen tools, seven languages, `find_implementations`, `impact`,
  `configure`, `hook`, and the configuration tables of every language. Both
  were carried to the branch of `Luqueee/kivgraph#221`.
- PR #10 (`bf5a9617`): the guides and the tool reference follow the daemon,
  the Windows integrations, the linked skill, `include_derived` in the cursor
  identity, the views and the completeness block, and gain answer shapes for
  `find_implementations`, `index_project` and `get_index_status` and an
  "Uninstalling" section. Statements were read from the code; no server was
  run. Carried to the branch of `Luqueee/kivgraph#221` (head `764dc79`), CI
  green there.
- PR #9 (`bf09e4d8`): the site says what its token figures do not show: they
  are costs per question, and the two end-to-end benchmarks did not show a
  reliable saving over a whole task. Four loosely quoted figures were
  corrected and the cold indexing cost carries its version. Not carried to
  the pull request, by decision.
- PR #11 (`ed9e4160`), ADR 0127: `update`, installers, build scripts,
  `server.json`, `glama.json`, README, docs, landing and skill name
  `Luqueee/kivgraph` again. Telemetry
  stays off and the MCP Registry job stays gated. The documented install
  commands therefore install the official project's latest release, not this
  tree, and no release is cut from the fork while this holds. This document
  and ADR 0118 and 0126 no longer name the repositories the work passed
  through. The attribution in the `v0.8.0` note stays: a changelog records
  who did what.
- Not done: the benchmarks were not rerun on `0.10.0` (the corpus is not
  available) and the captured answers in the tool reference that predate
  `completeness` were not recaptured. The figures the site quotes keep the
  version they were measured with: `0.5.0` for the 29 questions, `0.3.2` for
  the cold index, `0.8.0` for the MCP token cost, `0.3.0` for the first
  end-to-end run.

#### CI: scans, CodeQL and Windows tests (2026-11-17)

- PR #14 (`9e12fda6`): `.github/dependabot.yml` (weekly, grouped: `gomod`, the
  three pnpm projects, the actions); `security.yml` (weekly `govulncheck`,
  reachable only, and `pnpm audit` per project, also by hand); `codeql.yml`
  (Go with an untagged `go build ./...`, because the pinned CodeQL has no
  `none` mode for Go, and JavaScript/TypeScript; not part of `CI result`); a
  `windows` job in `ci.yml` that runs the 11 packages carrying a `_windows.go`
  file on `windows-latest`, which `CI result` requires.
- Running those tests on Windows for the first time found two production
  defects. `internal/daemon`: a busy `127.0.0.1:7788` stopped the daemon
  instead of falling back, because `syscall.EADDRINUSE` is not what a Windows
  bind returns (`addrinuse_windows.go` checks `WSAEADDRINUSE`).
  `internal/workspace`: a manifest deleted after the directory listing reached
  the parse and aborted the pass, because `DirEntry.Info` answers from the
  listing on Windows (`visitUnlessVanished`). Three tests also assumed Unix
  (`/tmp`, an unescaped path in JSON, a float sum with no slack).
- Both fixes and the test changes were carried to the branch of
  `Luqueee/kivgraph#221` (head `86947516`); the workflows were not.
- PR #15: the audit passes `--ignore-unfixable`. `pnpm audit` reports one high
  advisory today in all three projects, `picomatch` (GHSA-fc5q-39xc-vjpf,
  patched in `>=4.0.8`), and 4.0.8 is not on npm (latest `4.0.7`; an override
  to `^4.0.8` fails to install). It needs an override once the version exists.
- Not exercised: Dependabot has not opened a pull request yet, so its
  handling of the `pnpm-workspace.yaml` overrides and of the `patchedDependencies`
  in `web` is unknown. CodeQL findings have not been read.

#### Recall on real code, per language: pilot (2026-11-18)

`benchmarks/language-recall/`. There was no per-language figure for how much
of real code `find_references` finds, only fixture benchmarks. Method: rename a
declaration, type-check again, and the files that gain an error are the files
that use it; compare with what Kivgraph reports. Public repositories pinned by
commit (`spf13/cobra` for Go, `sindresorhus/ky` for TypeScript), 100
declarations per language, seed 1, stratified by kind.

- The oracle and Kivgraph share each language's type checker, so this measures
  everything around it (which files and variants load, how uses become edges,
  what is filtered), not type resolution. The unit is the file. Errors count
  only when their line names the original identifier (a rename cascades), and
  implementation relations and errors on the declaration's own line are left
  out. All of this is in the report.
- Go, file recall: `65.9 %` with the default configuration and `99.2 %` with
  `go.include_tests: true`. On non-test files it is `100 %` in both. The whole
  gap of the default run is `_test.go` files, which the default does not read;
  two files are still missed with tests on. Precision `100 %`.
- TypeScript, file recall `100.0 %`, precision `90.9 %` (unaudited: a reported
  file the compiler does not flag can be a use in a comment). It was `84.0 %`
  and `89.4 %` on the first run, before the three fixes below.
- **Finding, fixed: ES private names (`#x`) were never found.** The first run
  measured `84.0 %` and `89.4 %`: `0 of 13` private declarations, `0 of 13`
  dependent files, and every one of those answers carried the verdict
  `COMPLETE`. For `#parseJson`, called three times in `Ky.ts`, the answer was
  empty and said "this is an absence rather than a miss". Cause:
  `reference-extractor.ts` only visited `Identifier` nodes, and a private name
  is a `PrivateIdentifier`, so no use of one was ever resolved. Now `13 of 13`.
  The figures recorded here are from the run after all three fixes.
- **Finding, fixed: properties used as object literal keys and through
  destructuring were not found.** `{maxResponseSize: true}` and
  `const {throwHttpErrors} = options` name a property without the checker
  pointing at it: `getSymbolAtLocation` answers with the literal's own member
  or with the new local variable. Those keys are now resolved through the type
  the literal is checked against (`getContextualType`) or the value being
  destructured (`getTypeAtLocation` of the pattern), after removing `null` and
  `undefined`, and only for names some local property or method has. A
  `{name: alias}` pattern already worked. File recall went from `92.7 %` to
  `98.0 %` and the answers that said `COMPLETE` and missed a file from `8 of
  100` to `2 of 100`.
- **Finding, fixed: properties named by a string literal were not found.**
  `Options['onDownloadProgress']` in a type and `options['retries']` in an
  expression name a property through a string. They are now resolved through
  the indexed type (`getTypeFromTypeNode`) or the indexed value, including
  unions such as `T['a' | 'b']`, and only for names some local property has.
  File recall `98.0 %` to `100.0 %`, precision `90.9 %`, and no answer says
  `COMPLETE` while missing a file. Not tested: a key whose contextual type is a
  union probably points at the first local member only, because the resolver
  takes the first declaration that is local.
- Pilot limits: one repository per language, and the TypeScript dependencies
  are not pinned by a lockfile. Not done: Rust, Dart, Java, C# and Python
  (their toolchains are not installed here, and Python has no compiler to use
  as an oracle), a second repository per language, and an audit of the
  TypeScript extras.

#### Recall on real code: Rust added (2026-11-18)

`sharkdp/fd` at `b80cd53e6561`, 100 declarations, seed 1, same method. The
oracle is `rustc` through `cargo check --workspace --all-targets --locked`;
Kivgraph reads `rust-analyzer`, so unlike Go and TypeScript the two front ends
are different. `rust-analyzer` and `cargo` are needed on the machine
(`scripts/fetch-rust-analyzer.sh`; a sibling binary next to `kivgraph` is
used).

- First run: file recall `98.5 %`, precision `97.7 %`; every kind is `100 %`
  except functions (7 of 8 files) and trait methods (2 of 3).
- **Finding, fixed: a name used inside a `macro_rules!` body was never found.**
  `fd`'s `sanitize` (a trait method called as
  `$crate::error::SanitizeErr::sanitize($arg)` in `src/error.rs`) and
  `correct` (called inside a macro in `src/fmt/input.rs`) were the two files
  missed, and both answers said `COMPLETE` (2 of 96). `rust-analyzer scip`
  emits no occurrence for tokens in a macro definition. The loader now reads
  those paths with the grammar and publishes a `CANDIDATE` edge
  (`TREE_SITTER_SYNTAX`) when exactly one declaration of the macro's crate
  ends with the written path (ADR 0128). File recall `98.5 %` to `100 %`,
  precision `97.8 %`, no `COMPLETE` answer misses a file.
- Still absent: a name that fits two declarations, a method called on a
  receiver inside a macro, a name passed through a metavariable, and the
  expansion of a macro at each call site. Existing graphs need
  `kivgraph index --full`.
- Still limited to one repository. Java, C# and Python are not measured.

#### Recall on real code: Dart added (2026-11-18)

`dart-lang/collection` at `96afcc2fce62`, 100 declarations, seed 1. The oracle
is `dart analyze --format=machine` (errors, plus the two warnings an
`export ... show`/`hide` clause becomes when the name goes), Kivgraph reads the
Analysis Server. Dart is measured in two arms like Go: tests are read only with
`dart.include_tests: true`. Errors that come from renaming a class (its
constructors' own declarations, an `implements` clause) are not uses and are
left out of the truth.

- First run: file recall `49.0 %` (`64.9 %` on non-test files), precision
  `76.9 %`, and `25` answers that said `COMPLETE` and missed a file.
- **Finding, fixed: a file with a non-ASCII character published no
  references.** The navigation request carried the length in bytes and the
  server counts UTF-16 code units; an accent in a comment made the request run
  past the end of the file and the server answered an empty navigation, with no
  error. Two of the twenty files in `lib/src` were silent.
- **Finding, fixed: instantiating a class was not a use of it.** With an
  explicit constructor the call resolves to the constructor, so the class had no
  reference from the file that builds it. The class now gets a `CALLS_DIRECT`
  from the same evidence. Classes `0.37` to `0.63` file recall before the next
  fix.
- **Finding, fixed: doc links were published as relations.** `[name]` in a `///`
  comment is navigated like code and sourced at the module; "compatible with
  [hash]" became a module `EMBEDS` the function. Comment lines now publish
  nothing. Precision `76.4 %` to `92.5 %`.
- Result (ADR 0129): non-test file recall `98.7 %`, with `dart.include_tests`
  `99.0 %` overall and precision `88.4 %`; default overall `74.0 %` because
  the test files are not read. `COMPLETE` answers missing a non-test file:
  `25` to `1`.
- Still missed, not fixed: an explicit extension application
  (`IterableExtension(this)`), for which the server gives no navigation
  target; and `/** */` doc links. Not audited: the remaining extras
  (`UnionSet`, `equals`, `where` in their own files).
- Still limited to one repository. Java, C# and Python are not measured, and
  the first two need `scip-java` and `scip-dotnet`, which are not installed.

#### Go: several `init` functions in one package (2026-11-18)

Found while testing whether `go.include_tests` could become the default.
Indexing public Go repositories failed with `symbol "init" is defined by two
files, so two declarations share one identity`: Go allows any number of `init`
functions per package and they all got one stable key. It does not depend on
`include_tests`: `caddy` and `prometheus` failed without tests, `gin` only with
them. `cobra`, the benchmark corpus, has none, so nothing had seen it.

- Fixed (ADR 0130): an `init` is named `init#<file>`, plus `#<n>` when a file has
  several. Every Go `init` changes key; nothing can reference one.
- Test: `TestExtractDefinitionsGivesEveryInitFunctionItsOwnIdentity`, red on
  `main`. It also checks that a use inside each `init` is sourced at its name.
- After the fix `gin`, `caddy` and `prometheus` index with and without tests.
  `include_tests: true` on those three: files `52 to 92`, `208 to 400`,
  `403 to 697`; edges `3212 to 9744`, `27423 to 41543`, `93921 to 209969`;
  index time `3 to 3 s`, `5 to 6 s`, `14 to 20 s`; state `531 to 544 MB`,
  `571 to 602 MB`, `670 to 872 MB`.
- Not fixed in that change: `etcd` aborts the pass on a symlink in the source
  inventory. Fixed afterwards, see below.
- Still open: whether `go.include_tests` becomes the default. The cost on these
  three is small; recall with tests read is `99.2 %` on `cobra` against `65.9 %`.
  Decision parked until `find_references` can say it did not read tests.

#### Source symlinks no longer abort the index (2026-11-18)

`etcd` links integration examples into the package that documents them
(`client/v3/concurrency/example_election_test.go`). The source inventory refused
any symlink with a source extension with an error, and one was enough to abort
`index --full`. Not following symlinks is the policy; failing the pass was only
how it was enforced (ADR 0131).

- The inventory digest now covers the link's text, so a retargeted, added or
  removed link still makes a generation stale. An explicit `manifests` entry
  that is a symlink is still refused.
- Tests: `TestSourceSymlinksAreNotFollowedAndNotFatal` (red on `main`; the
  digest does not change with the content behind the link, does change with its
  target, and changes when it is removed) and
  `TestExplicitManifestSymlinksAreRefused`.
- A throwaway module with a linked `_test.go` indexes and publishes with the
  rebuilt binary. `etcd` now gets past the inventory and stops at its Go 1.27
  requirement, which this build (type-checks up to 1.26) does not meet. Not
  measured: `etcd` itself.

#### Go and Dart read their tests by default (2026-11-18)

Decision taken after measuring it (ADR 0132). With `include_tests: false` the
graph lost every file that used a symbol only from its tests, and
`find_references` still said `COMPLETE`. File recall, tests not read against
tests read, on 40 declarations per repository (100 for `cobra` and
`collection`):

|repository|tests not read|tests read|non-test files, both|
|---|---|---|---|
|`cobra`|`65.9 %`|`99.2 %`|`100 %`|
|`gin`|`50.0 %`|`98.9 %`|`97.8 %`|
|`caddy`|`77.5 %`|`95.8 %`|`94.8 %`|
|`prometheus`|`53.5 %`|`97.1 %`|`98.9 %`|
|`collection` (Dart)|`74.0 %`|`99.0 %`|`98.7 %`|

- Cost of reading tests, after the `init` and symlink fixes: edges `x3.0`, `x1.5`
  and `x2.2` on `gin`, `caddy` and `prometheus`; indexing `3 to 3 s`, `5 to 6 s`,
  `14 to 20 s`; state `+13`, `+31`, `+202 MB`. Measured later (ADR 0132): serving
  RSS `+1` to `+35 MB` and query p95 under `5 ms`; the default answer grows
  `5 %` to `53 %`; the indexing peak grows `+0.1` to `+1.1 GB`, from a baseline
  of `5.5 GB` on `prometheus` (see the next item: it is the Go heap).
- Where the indexing memory goes (measured on `prometheus`, with tests): the
  peak is the `kivgraph` process itself; its children (`go list`, `clang`) stay
  under `0.3 GB`. It is Go heap: `gctrace` shows `3.9 GB` live and `6.1 GB`
  in use at the maximum, which is why `GOMEMLIMIT` could not lower it. A heap
  profile at that point is `go/parser` ASTs and `go/types` `Info` maps
  (`recordTypeAndValue` alone `1.0 GB`, `recordUse`, scopes): `LoadMode` has
  `NeedDeps` with `NeedSyntax` and `NeedTypesInfo`, so `go/packages` type-checks
  every dependency from source and keeps it while `roots` lives. The writable
  buffer pool is not it: capping it at `256`, `512` and `1024 MB` moved neither
  `prometheus` nor `caddy` outside the run-to-run spread (`4.9` to `8.2 GB` on
  the same configuration). `GOGC` `50` and `25` did not lower it either.
- Dropping `NeedDeps` does lower it (`prometheus` `8.2` to `2.9 GB`, `caddy`
  `2.9` to `1.1 GB`) but dependencies then come from export data and indexing
  is `5x` to `11x` slower with a warm build cache (`caddy` `7` to `38 s`,
  `prometheus` `19` to `212 s`). Not adopted. Open: type-check dependencies
  from source but release their `Syntax` and `TypesInfo` once checked, which
  `go/packages` does not offer, so it would mean owning the loading.
- Done: both defaults are `true`. `find_references` takes `exclude_tests`, which
  uses the path rule of `impact` and applies before the total and the page.
- `find_cross_repo_consumers` takes `exclude_tests` too (second amendment of
  ADR 0132): it drops consumer and unresolved rows in a test file and keeps
  package dependencies, which name no file.
- Existing installations keep `false`: the `config.yaml` an earlier `init` wrote
  spells the key out. A test pins that an explicit `false` is still read as
  `false`.
- Found on the way, not fixed: non-test misses on `gin` (`Redirect`, 1 file) and
  `caddy` (`ClientCertificateVerifier`, `Warning`), each answered `COMPLETE`;
  and the extras of `gin` (`Use`), unaudited.
- The benchmark's second configuration is now `go.tests_excluded` and
  `dart.tests_excluded`; `-repos` measures extra repositories by name.

#### Trust in `COMPLETE`: the misses, the extras and the filter (2026-11-18)

The three answers that said `COMPLETE` and left out a non-test file, the extras
of the precision column, and `exclude_tests` on the traversal tools.

- **Go, three misses fixed (ADR 0133).** A use inside a local `var`/`type` was
  sourced at a name nobody published, and so was the blank `_` of
  `var _ I = T{}`. Two defects behind the second only showed once it was
  reached: the module symbol had an empty discriminator (one assertion aborted
  the index) and was appended to a list `NormalizeGo` rebuilt at the end.
  Tests read, `gin`, `caddy`, `prometheus`, `cobra`: file recall `98.9 %`,
  `95.8 %`, `97.1 %`, `99.2 %` to `100 %` each; `COMPLETE` answers missing a
  non-test file `3` to `0`.
- **Audit of the extras.** `gin` `Use` (`11` files): `Engine` embeds
  `RouterGroup` and has its own `Use`; renaming it leaves `router.Use(...)`
  compiling through the promoted one, so the oracle cannot see the calls. Real
  uses. Dart (`13`): overrides and implementations of interface members
  (`CanonicalizedMap implements Map`, `HeapPriorityQueue implements
  PriorityQueue`), a method defined in two extensions (`forEachWhile`) and
  `UnionSet` constructors in its own file. Checked on four, the rest by the same
  pattern. TypeScript (`15`): default imports (`delay`, `timeout`,
  `isRawNetworkError`) and re-export chains (`KyRequest`, `ShouldRetryState`)
  are not errors after a rename; `9` were ours (ADR 0134).
- **TypeScript, fixed (ADR 0134).** A class that merely carried a field of a
  data type or an options bag was its "only implementation", and the references
  to the type were forwarded to it. A structural implementation now needs a
  required method. `ky`: precision `90.9 %` to `96.2 %`, recall `100 %`.
- **`value` traced and fixed (ADR 0135).** The method `value` of
  `source/utils/body.ts:149` reported the uses of four other `value`s of the file:
  references travelled as a qualified name and the consumer kept the last symbol
  of each name. Each reference now carries the offsets of its two declarations.
  `ky` precision `96.2 %` to `96.8 %`. The benchmark's own-file `EXPORTS` rule hid
  a real `export default ky;` that the bug had been covering; it now keeps an
  export written on another line than the declaration.
- **Identical declarations numbered (ADR 0136).** Two symbols of a file with the
  same name, kind and signature (two `(value: string)` parameters) shared a
  stable key and the graph kept one. On `ky`: `72` declarations in `36` groups
  (`28` parameters, `38` variables, `6` type parameters); the graph went from
  `1062` to `1134` symbols. The first keeps its key and each repeat is numbered.
  The recall figures did not move: the benchmark cannot tell these apart.

- **`exclude_tests`** on `find_implementations`, `trace_dependencies` and
  `get_blast_radius` (ADR 0132, amendment). In the blast radius it leaves the
  axes too. `trace_dependencies` refuses it with `to`. Surface `18.128` to
  `18.494` bytes, ceiling `18.600`.

- **A second repository per language** (`benchmarks/language-recall/
  second-repository/`; `got`, `ripgrep`, `args`; 100 declarations each, same
  seed). The first repository's figures do not carry over:

  |language|repository|recall|precision|
  |---|---|---|---|
  |TypeScript|`ky` / `got`|`100 %` / `97.8 %`|`96.8 %` / `70.3 %`|
  |Rust|`fd` / `ripgrep`|`100 %` / `100 %`|`97.8 %` / `81.1 %`|
  |Dart|`collection` / `args`|`99.0 %` / `100 %`|`88.4 %` / `91.3 %`|

  Rust: the oracle ran `cargo check` without `--keep-going`, so one failing
  unit stopped the others. On `ripgrep` it left `53` of `87` declarations with
  dependents and precision at `45.1 %`; with the flag, `87` and `81.1 %`. `fd`
  did not move (`100 %`, `97.8 %`). The remaining extras are files in crates
  that depend on the renamed one, which do not compile after the rename: the
  truth is a lower bound on a workspace.
  TypeScript `got`: `4` declarations missed (all answers `LOWER_BOUND`, none
  `COMPLETE`). Two were keys of an object literal passed to a generic call
  (`https: {rejectUnauthorized}`, `beforeRedirect`): the contextual type there
  is what the checker inferred from the literal, so the key resolved to the
  literal's own property. Fixed (ADR 0137): the key is now looked up in the
  constraint of the parameter; `got` recall `97.8 %` to `98.7 %`, `ky` unchanged.
  The other two are accessor pairs (`dnsCache`, `maxHeaderSize`): the oracle
  renames the setter only, which makes the getter read-only and cascades errors
  in the same file (checked on `maxHeaderSize`). Not a miss of the worker.
  The precision is largely the oracle's. Of `92` extra files, `31` have no
  mention of the name (`export *` in `source/index.ts`), `2` only export or
  import lines and `59` mention it in code. Two causes were checked: an aliased
  default export (`export {default as Options}`, so renaming the class breaks
  nothing) and a merged `interface ClientRequest` that augments `node:http`. The
  rest of the `59` were not audited. Every `got` answer is `LOWER_BOUND` because
  one package (`expect-type`) is not found anywhere in the index.
  Dart `args`: `20` extra files, `6` of them callers of the factory
  `ArgParser.allowAnything()`, attributed to the class it constructs.
  Dart without tests: recall `65.1 %` (`collection`: `74.0 %`).
  No `COMPLETE` answer missed a non-test file in any of the three.
- **Indexing memory, fixed in part (ADR 0138).** The earlier item said the
  open idea was to own the load and release dependency syntax. It was cheaper:
  `go/packages` accepts a parser, and a dependency's function bodies can be
  dropped there. A first try that left the errors in blocked every module (the
  checker reports the imports that only bodies used, and a generic function
  without a body); the proxy that ignored them showed a halving of the live
  heap, and the version that keeps generic bodies and `init` and forgives only
  those errors is the one measured. `prometheus`: live heap `3.9` to `1.7`/`1.8
  GB`, peak heap `6.0`/`6.5` to `3.1 GB`, peak RSS `7.0`/`7.7` to `4.7`/`4.9
  GB`, time about `5 %` less; `caddy` RSS `2.85` to `1.88 GB`. Same graph on
  `cobra`, `gin`, `caddy`, `prometheus` and the same files and verdicts for
  `280` declarations. A root outside the load directory forces a second load.
- **The rest of the indexing memory, attributed (ADR 0139).** It was the
  collector's headroom while the pass holds every package (`3.7 GB` in use for
  `1.9 GB` live at `GOGC=100`), and on macOS pages Go had already released that
  `maximum resident set size` still counts; `peak memory footprint` is the
  figure that does not. Forcing the release before staging changed nothing.
  The pass now sets the GC percent to `50`: `prometheus` footprint `4.2`/`4.4`
  to `3.4`/`3.2 GB` for about `1.5 s` more, `gin` `0.86` to `0.45 GB`, `cobra`
  unchanged; `GOGC` in the environment still wins. About `0.7 GB` above the
  heap at the peak is not broken down.
- **Indexing memory on Linux (ADR 0140).** The two changes hold where the
  resident set is truthful: `prometheus` `7.3`/`7.7 GB` to `4.7`/`4.5 GB` (ADR
  0138) to `3.5`/`3.8 GB` (ADR 0139); `caddy` `2.8` to `1.3 GB`. What Linux
  adds is the container's limit, which Go does not read: with `--memory=3g` the
  pass was killed at `3.06 GB`. A pass now sets the Go soft limit to four
  fifths of the cgroup limit: it passes at `3 GB` (`2.46 GB`, `45 s`) and at
  `2.5 GB` (`56 s`), and is still killed at `2 GB`, where what is live does not
  fit. `GOMEMLIMIT` wins; without a cgroup limit nothing is set. Measured on
  `linux/arm64` in a container, not on `linux/amd64`.
- **Java and C#, measured for the first time (ADR 0141).** `scip-java` and
  `scip-dotnet` were installed and the benchmark got an arm for each (`jsoup`,
  `GuardClauses`; `benchmarks/language-recall/java-csharp/`). Three product
  defects surfaced before a figure did, all in the shared converter: a C#
  `partial` type aborted the whole index; every Java and C# line was one too
  low (SCIP counts from zero); and `new T(...)` was not a use of `T`. Fixed.
  Java with tests read: recall `100 %`, precision `96.8 %`. Java by default
  (`java.include_tests: false`): `61.1 %`, every miss a test file. C#: `49.1 %`
  with tests, `47.4 %` without, on `53` declarations, all methods; the cause is
  `scip-dotnet`, which resolves an extension-method call to the first overload
  and records nothing for a generic call with explicit type arguments, so the
  other overloads are answered `COMPLETE` with no caller. Two oracle defects of
  the benchmark were fixed on the way: `javac` stopped at the file-name error
  of a renamed public class (`-XDshould-stop.ifError=FLOW`), and `init` gave
  every repository every language, which Java and C# do not tolerate.
  Open: a registered repository with no project for its language
  (C# with no solution, Java when `scip-java` writes no index) aborts the whole
  pass instead of being isolated; and the C# overloads.
- **Java and C# read their tests by default (ADR 0142).** `jsoup` recall
  `61.1 %` to `100 %`, every miss having been a test file; symbols `3528` to
  `6673`, no slower to index. An explicit `false` in an existing `config.yaml`
  is kept. C# gains `7` symbols on `GuardClauses`: its loader's rule for a test
  (`tests/`, `*Test.cs`, `*Tests.cs`) does not match that repository's layout,
  so its tests were already read, and it differs from the rule `exclude_tests`
  uses. Not reconciled. Python stays as it was, unmeasured.
- **Doctor sees a blocked profile migration; tests leave the real home
  alone.** A state directory recreated beside an older `.pre-profiles`
  recovery point made `init`, `serve` and `index` refuse while `doctor`
  answered `PASS`. `doctor` now fails on `profiles` and names the directory to
  move, from a read-only digest of the legacy layout that a test holds equal
  to the digest of the migrated copy. What recreated the directory was this
  repository's own suite: `54` tests in `internal/config` and `cmd/kivgraph`
  initialise a configuration at a temporary path, whose synthetic Go workspace
  lives in the user's state directory. Both packages now run under
  `testsupport.IsolateHome`.
- **Three leftovers of that work, closed.** `serve` no longer installs a
  daemon when it runs under a home that is not the account's: the supervisor
  would start it under the account's, against another installation. A Dart
  analysis server now dies with the process that started it; it does not exit
  when its input closes, so one whose parent was killed stayed for good
  (unix only: a watcher on a pipe; a declared limitation elsewhere). And one
  rule says what a C# test is, in `internal/testpath`, for the loader and for
  `exclude_tests` (ADR 0142, amended): `csharp.tests_excluded` on
  `GuardClauses` goes from `47.4 %` to `14.0 %` recall, which is the arm
  leaving the tests out at last.
- **An indexer's process tree does not outlive it (ADR 0143).** The `dotnet`
  processes found after test sessions were reproduced: a time limit killed the
  launcher of `scip-dotnet` or `scip-java` and left its children, a killed
  kivgraph left the whole tree, and a successful C# index left an MSBuild
  worker node idling at `117 MB`. `internal/proctree` runs both tools in a
  process group, kills the group on cancellation, on this process's death and
  on return. Unix only. The Java loader now applies the shared test rule too
  (ADR 0142, second amendment), which gained `src/it/` and reads the capital
  in `*Test`, so `Latest.java` is no longer a test. The other two analyzers
  were then measured the same way: `rust-analyzer` leaves nothing behind,
  killed alone or with kivgraph, and is not changed; the Python worker, and
  the Pyright server under it, kept running when kivgraph was killed and now
  go through `proctree` too. Running the exact Python mode on real code for
  that measurement found it could not index any: the Pyright adapter crashed
  on the first plain `import x`, which no fixture had. Fixed. Open: no Windows
  counterpart for the guard; the exact Python mode has still only been run on
  one corpus.
- **Python measured on real code** (`benchmarks/language-recall/python/`,
  `click` at `934813e4`, `100` declarations per configuration). The oracle is
  mypy, which Kivgraph does not use, so the truth is a lower bound: it sees
  nothing through `Any`.

  | configuration | file recall | file precision |
  |---|---|---|
  | default (syntactic worker, no tests) | `43.0 %` | `86.1 %` |
  | `python.tests_included` | `43.0 %` | `86.1 %` |
  | `python.exact` (Pyright) | `66.1 %` | `95.3 %` |
  | `python.exact.tests_included` | `99.5 %` | `95.9 %` |

  The exact mode with tests misses one file in `83` declarations with
  dependents: a `ParamSpec` used in quoted annotations, answered `COMPLETE`.
  Without tests every other miss is a test file. What the run also showed,
  none of it fixed:
  - The default mode answers `COMPLETE` and misses a non-test file in `15` of
    `75` such answers, and recalls no use of a method (`0` of `10`).
  - Reading tests buys the default mode nothing here: it does not resolve
    `import click` from `tests/` to `src/click`, so no test reaches the package.
  - `exclusions` in the registry did not keep `docs` and `examples` out of the
    Python graph. The comparison leaves those files out on both sides instead.
  - `python.include_tests` is `false` by default and `analyzer_mode` is
    `fallback`: the measured gap is `43.0 %` to `99.5 %`. Changing either
    default is a decision, as it was for Java and C# (ADR 0142).

  Three of these were then fixed (ADR 0144), and the table above is the
  measurement before them. `python.include_tests` defaults to `true`; the
  syntactic worker records each member access it does not resolve, which takes
  its wrong `COMPLETE` answers from `15` of `75` to `1` of `43` without making
  it more complete (`43.7 %`); and both workers read names inside quoted
  annotations, which is the `ParamSpec`. The exact mode now measures `100.0 %`
  recall and `95.9 %` precision by default and `66.7 %` with tests excluded.
  Then the rest (ADR 0144, amended): the syntactic worker finds a package by
  the name it is imported as, follows re-exports and resolves `module.member`,
  which takes the default from `43.7 %` to `82.9 %` recall at `85.6 %`
  precision; both workers apply `internal/testpath`; and `exclusions` reach
  the Python graph. The last wrong `COMPLETE` went after that (ADR 0144, second
  amendment): a name one module binds to two declarations, an import under a
  platform test and a `def` in the other branch, was attributed to the import.
  The worker now leaves such a use unresolved, in that module and in whoever
  imports the name from it: `1` of `60` to `0` of `59`, recall and precision
  unchanged. Still open: the default mode recalls no use of a method, which
  needs a type; `analyzer_mode` stays `fallback`.
- **`exclusions` mean one thing in every loader.** Checked for the four that
  were left: Rust already applied the registry's matcher. Java and C# did not
  read the key at all -- the indexer builds the whole project and every document
  it wrote entered the graph -- and now leave excluded files out as they leave
  tests out. Dart read it with a matcher of its own, which took a bare name at
  any depth and did not read `./x`, an absolute path or `a/**/b`; it now asks
  the registry's (`workspace.ExclusionMatcher`). For Dart that is a change of
  meaning: `gen` excludes the root's `gen` and no longer one nested below, which
  is written `**/gen`, as it is for every other language.

  Two things about the oracle are worth keeping. mypy binds a name to `Any`
  after its import fails, which hides every use behind a re-export in
  `__init__`; the oracle rewrites each failing import and checks again. And
  mypy's cache takes a file for unchanged when its size and its modification
  second are: every rename here adds three bytes, and a first run read one
  declaration's errors for the next one in the same file. It runs without a
  cache, and the audit that caught it -- the same declaration must get the same
  truth in every configuration -- found `0` differences in `179` afterwards.
- **A failure bounds answers in its own language (ADR 0146).** The lookup
  behind every `completeness` verdict read a failure's name and not its
  language, over the whole snapshot: `find_references` for `Command` in `cobra`
  answered `COMPLETE` alone and `LOWER_BOUND` with `fd` indexed beside it,
  because of `std::process::Command` in a Rust file. A failure now bounds an
  answer about a declaration only if it is in that declaration's language, or
  recorded none. `find_symbol`, which asks about a name, is unchanged.
- **One repository that cannot be read does not fail the pass (ADR 0147).**
  A C# repository with no solution, a Java build that fails or a Python
  producer that exits ended the pass, and every other repository lost its graph
  to it. Python, Dart, Java and C# now do what Go and Rust did: the repository
  publishes a scope-level `REPOSITORY_NOT_LOADED`, the report names it on an
  `index.<language>.not_read` line, and the rest is indexed. A cancelled pass is
  still an error. TypeScript followed (ADR 0147, amended): a `tsconfig.json`
  that does not parse ended the pass at discovery, and so did a pass whose only
  TypeScript repository has no project -- a Go repository registered with
  `--languages go,typescript`. Both are isolated, and so is a package the
  worker fails on. A worker that is not there is still an error: it ships with
  Kivgraph.
- **Open after the language-recall work** (none has an owner or a date):
  - Dart: explicit extension application (`IterableExtension(this)`) and `/** */`
    doc links. Rust: methods on a receiver, names passed through a metavariable
    and per-call-site expansion in `macro_rules!`. TypeScript: property keys of
    a contextual union outside a generic call (the generic-call case is fixed,
    ADR 0137), and function-typed properties under the structural
    implementation rule.
  - The benchmark measures one repository per language apart from Go, and one
    each for Java, C# and Python; `etcd` needs a Go `1.27` build.
  - Python: the default mode recalls no use of a method, which needs a type,
    and `analyzer_mode` stays `fallback` because `exact` needs Pyright
    installed. Pyright does not navigate from the string of a `cast`. The
    syntactic worker's bindings are per file, not per scope: an import inside
    one function binds the name in the whole file. A name bound twice to an
    import outside the repository and a local `def` is attributed to the
    `def`.
  - C#: recall on overloads is `49.1 %` and comes from `scip-dotnet`, which
    resolves an extension-method call to the first overload.
  - Indexing: the Java, C# and Python tools still build or read excluded
    files. Whether the Dart loader's own rule for a test agrees with
    `internal/testpath` was not checked. TypeScript isolation: the worker
    failing on one package was exercised with a stand-in, never with the real
    worker, which tolerated every broken configuration it was given; the
    branch that refuses a worker which disappears between being found and
    being started has no test; and neither `index.<language>.not_read` nor
    `index.typescript.no_package` is asserted from the CLI.
  - Processes: `internal/proctree` is Unix only, and its runs with the real
    tools were on macOS. `ensureDaemon`, the explicit path, does not apply the
    foreign-home check that `serve` does.
  - Memory: about `0.7 GB` above the heap at the peak of a large Go index is
    not broken down; no `linux/amd64` measurement; the collector setting is
    process-wide and was not measured with a daemon serving while it indexes.
  - Benchmarks: `agent-e2e` was not rerun after its harness stopped relaying.
    The `BUILD.bazel` files do not list the packages and files added since
    `internal/goloader/dependency_bodies.go`; CI does not check them.
  - CI: `internal/daemon` reported `setup failed` once on Windows and passed
    on the next run; `Set up the semantic indexers` and the macOS runners fail
    to start often enough to need a rerun.
  - Site, done: `/github` answers `302` (the earlier `404` is gone, and the
    redirect lives in the edge Worker, not in this repository). The compare link
    of a release with no tag (`v0.10.0`, `v0.10.0-dev.1`) is hidden by
    `tagged: false` in its note, to be removed when the tag exists. The
    examples of `find_references`, `trace_dependencies` and
    `find_cross_repo_consumers` carry `completeness`, and `graph_status` shows
    schema `5`. Examples were reconciled by envelope keys against real answers
    on `cobra`; the fields inside `results` were not compared one by one.

## Phase 7. Impact from a diff (`feat/blast-radius-from-diff`)

`kivgraph impact --base <ref>`: symbols touched by `git diff`, aggregated
radius and affected tests apart (`by_test_file`), with `--json`. Negatives:
nonexistent ref, empty diff, file outside the index, graph older than the
tree, range without a symbol.

Done (2026-11-17): PR #97 (`internal/gitdiff`) and #98 (`impact`, ADR 0125).
See "Checklist lot" at the end of phase 6. Limits: a Rust test module inside
a source file is not seen as a test; a language whose tests the index leaves
out (Go with `go.include_tests: false`) shows none; a deleted symbol is not in
a graph built from the new tree.

## Phase 8. Languages and contracts (only on concrete demand)

- Kotlin through the `internal/scip` bridge; first check that there is a
  usable indexer.
- Swift: a complete new loader.
- Prototype of a contract edge, a single format, with an ADR and a schema
  change; `EXACT` only if both ends are resolved by an analyzer.

## Out of the plan

Tree-sitter structural level (ADR 0006, 0033), connected documentation (ADR
0073), embeddings without a prior experiment, agent memory, export,
differential verification and remote collaboration.

## Order

0 and 1a-1c first (1 needs the visibility decision). 2 in four PRs, in
parallel with 3. 4 after 2. 5 after 4. 6 and 7 independent. 8 waits for
demand.
