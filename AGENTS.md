# Kivgraph development instructions

These rules apply to the whole repository. An instruction closer to a file may
add restrictions, but it cannot relax the integrity, compatibility or
verification contracts described here.

## Instruction map

This file is always read. The others cover one directory and are read when
working in it; none repeats what is already here or contradicts it.

|directory|file|what it covers|
|---|---|---|
|`internal/`|`internal/AGENTS.md`|Go loading, the pass, fact cache, canonical graph, generations, configuration, processes|
|`internal/mcp/`|`internal/mcp/AGENTS.md`|tool surface, `index_project`, the skill, token cost|
|`internal/rustloader/`|`internal/rustloader/AGENTS.md`|`rust-analyzer scip`, SCIP identity, sysroot, Cargo discovery|
|`cmd/kivgraph/`|`cmd/kivgraph/AGENTS.md`|help, registry, `index [--full]`, `clean`, `stop`, `ui`|
|`ts-worker/`|`ts-worker/AGENTS.md`|TypeScript worker and cross-repository identity|
|`web/`|`web/AGENTS.md`|the viewer: layout, drawing, cost per frame|
|`landing/`|`landing/AGENTS.md`|landing and user documentation: layers, palette, SEO, icons|
|`benchmarks/`|`benchmarks/AGENTS.md`|reports, corpora and audits|

An invariant that is violated from another directory lives here, not in the file
of the directory it names: that `landing/` enters no bundle is broken by editing
`scripts/build-bundle.sh`.

## Project identity

- Project: `Kivgraph`.
- Go module: `github.com/Luqueee/kivgraph`.
- Main executable: `cmd/kivgraph`.
- TypeScript worker: `ts-worker/`, private package `@kivgraph/ts-worker`.
- LadybugDB is the canonical storage; the HotSnapshot is a derived projection
  and not an alternative source of facts.
- The historical `LUQUE-####` identifiers of the backlog are not renamed.

## Which question each Kivgraph tool answers

This block is the portable routing channel: `CLAUDE.md` is a link to this file,
so Oh My Pi and Claude Code both load it without anybody asking. The server's
`instructions` field says the same, and Zed does not read it.

| the question | the tool |
| --- | --- |
| who calls this, what references this | `find_references` |
| who implements a type or method | `find_implementations` |
| what breaks if I change it | `get_blast_radius` |
| what does this reach outward | `trace_dependencies` |
| who uses it from another repository | `find_cross_repo_consumers` |
| where is it declared | `find_symbol` |
| I do not know what it is called, which files do I open | `find_by_intent`, with `keywords` |
| what is declared in this package | `get_file_outline` |
| give me the code of these symbols -- up to `20` per call | `get_source` |
| the code at one file line, from a stack frame or a diff | `get_source` |
| is the graph up to date? | `graph_status` |
| start an index without holding a long call | `start_index_project` |
| how is that index going | `get_index_status` |

The edges are resolved by `go/types`, the TypeScript checker and
`rust-analyzer`, not by name matching: an empty reference list means **nobody
calls it**, not that it was not found. `grep` cannot say that, and it does not
tell two homonymous methods apart either.

Every row brings repository, path, qualified name and line range, and every tool
accepts that triple instead of a stable key: the next call is built with the
response already in hand.

A references question does not need to resolve the symbol first: a bare `name`
is enough, and when several declarations share the name the response refuses to
choose and **names the candidates** with that same triple, so narrowing is
copying one. Over `workspace` the refusal costs `129` tokens where the previous
`find_symbol` cost `750`; measured in `benchmarks/graft-comparison/report.md`.

And it is asked at the granularity of the question: `view: "files"` answers which
files, without the line of each reference. The four references questions of
`workspace` cost `2,480` tokens with the line and `912` without it, with the same
precision and the same exhaustiveness -- and one page instead of two where 66
references fit in 9 files. Whoever needs the line asks for the compact rows,
which are the default.

**Where it loses, and the call is best not spent:** a rare name in a single small
repository is resolved more cheaply by `grep` -one call, no schema, no symbol to
resolve first-, and the index of a small file costs more than reading it. It
wins on common names, on transitive impact, on consumers from another repository
and on proving an absence.

Measured with `benchmarks/mcp-token-cost` on `0.10.1`, over the six references
questions of this same repository, indexed as documented -with the `ladybug` tag
and without `testdata/`- (generation `000001`, `26,231` symbols): **there `grep`
comes out cheaper on five of six**. Answering costs `0.82x` -between `0.55x`,
`DiscoverGo`, and `1.22x`, `Publish`, which is the only one that is won-, and
the whole session -including the bodies the agent opens afterwards, which cost
the same on both sides- `0.98x`, with a floor of `1.50x` set by the cost of
those bodies and not by that of the answer. Indexed without those two things it
was `0.47x` and `0.89x`, and before ADR 0156 `0.24x` and `0.74x`: the
`completeness` block was most of the response. The earlier measurement, on
`0.8.0`, gave `1.76x` and `1.24x`; the agent's `grep` stopped bringing context
lines, and that alone lowered the native arm from `1,082` to `429` tokens on
`MergeAll`. It is the least representative corpus we have: small, with its own
fixtures as blind spots. The `29` cases below remain the headline of where it is
won.

The genuinely trivial case **is already a measured row**, and it confirms the
disadvantage: `benchmarks/graph-tools-comparison/trivial.md`, on `newGMCClient`
-- two occurrences across all of `workspace`, one declaration and one call, no
homonym in five languages. All six arms are right and `grep` is the cheapest of
all: `65` tokens against our `123`, that is, we cost `1.9x`. The other four
graphs also lose against that `grep` line, and the reason is structural: the
envelope of a response -- `snapshot_id`, `coverage`, `total`, the row with
repository and range-- costs more than two lines of text when the answer **is**
two lines of text.

Over the `29` questions of the six sets, `grep` comes out cheaper on five and the
median stays at `5.95x` in our favour. Three of those five are absence
questions, and there cost and evidence do not say the same thing: a `grep` with
no results is cheap and is not proof, because it does not distinguish "nobody
calls it" from "the callers write it another way". Measured in the same corpus by
`X1`, where a real consumer never spells the symbol -- a `*` re-export that
crosses it between repositories-- and no text search reaches it.

## The gate in front of `grep`

`kivgraph hook install` registers a hook that runs before each of the agent's
tools and denies the search when the graph answers it better. It is hosted by
`claude-code`, `claude-desktop`, `codex`, `opencode` and `oh-my-pi`. Claude
Desktop bundles a Claude Code and launches it without giving it a configuration
of its own, so it reads `~/.claude/settings.json`: it shares a file with
`claude-code` and installing one leaves the other `managed`. Oh My Pi receives a
native extension in `~/.omp/agent/extensions/` or `.omp/extensions/`; the
contract is in ADR 0089.

For a search that already spells a name, it closes on **a single fact**: the name
is declared by two or more things, so a text search cannot separate what it
finds. A name with no homonym is let through for many places that use it, because
there `grep` is cheaper and that is measured -- see ADR 0077 for the table and for
what would need measuring to close it over high counts too. And the search's
repository has to declare the name: the count is for the whole graph, and denying
a search in a repository the graph has nothing about leaves no tool at all.

A search that does not yet know the name takes the intent route. A regex shaped
like code, or four or more alternative identifiers such as
`http|route|handler|serve`, queries `find_by_intent` and is only denied when the
graph returns candidates. Three exact terms such as `error|warning|failed` remain
a textual search.

Neither route closes when the search says where it looks and only looks at files
the graph does not cover: `grep -n version package.json`, a `.log`, a glob
`*.yaml`. It takes a path with an extension that is not code and none that is; a
bare directory proves nothing, and a type filter that names a language
(`--type rust`) is still a search over code.

Every failure of the gate is a permission, and a permission writes nothing: in
that contract an explicit `allow` skips the agent's permission prompt.

To skip it once: `KIVGRAPH_DISABLE_HOOK=1` in front of the command. A call of the
native tools (`Grep`, `Glob`, a subagent) has no command, and for them the denial
says that the user should start the session with the variable.

The output wrappers `rtk rg ...` and `rtk proxy rg ...` are classified by the
inner command. `rtk gain` and the other RTK-specific commands are not considered
searches.

And likewise the wrappers that only change how a command runs -- `time`,
`timeout`, `nice`, `nohup`, `env`, `command`, `exec`, `sh -c "..."` and a
subshell--, and a redirection is not a file the search reads: `rg Name >
out.txt` named a `.txt` that the graph does not cover and let an ambiguous name
through. The target of a redirection is discarded when tokenizing; a quoted `>`
is still part of the pattern or the path.

The gate also does one thing that is not denying. The matcher covers Kivgraph's
MCP tools -- by server name, `mcp__kivgraph_.*`, never by a list of operations
that would have to be extended with every new tool-- and to the **first one of
each session** it attaches the call budget. It never denies it: a call to the
graph is already the result the other branches defend, so demanding the skill
before letting it through would put friction exactly on the behaviour the gate
exists to encourage, and would leave the tools unusable in the session where the
notice failed.

It goes as `additionalContext` **without** `permissionDecision`, and that is not
a formatting detail: the short form -- `allow` with the text in
`permissionDecisionReason`-- delivers the same words and in passing removes from
the user the permission prompt of every call it travels with. The notice adds
context and does not vote.

It does not repeat the server's `instructions` field, which already arrives in
the handshake. It carries what that field leaves out on purpose: the cost per
tool, which cannot live in a system prompt that is rewritten on every reindex --
`internal/mcp/instructions.go` explains why, and cites issue #260 of tokensave,
where a budget there cost more than the calls it advised against.

It is remembered by `session_id` with a marker in `<state>/briefs`, created with
`O_EXCL` so that two simultaneous calls do not both notify, and it is pruned at
24 hours. A host that sends no `session_id` -- the OpenCode and Oh My Pi
modules-- receives no notice at all: without an identity, notifying on every call
would be worse than not notifying.

The matcher is written at install time, so an installation earlier than this does
not see the MCP calls until `kivgraph hook install` is run again.

In Codex the matcher is the literal `Bash`, even though the transcript presents
the execution as `Shell`. Its payload adds `turn_id` and its denial is not Claude
Code's JSON: it requires exit code `2` and the reason on `stderr`.

A linked worktree is covered when its Git common dir identifies a single
registered repository. The physical path does not have to hang from the checkout
that was indexed: Codex creates its worktrees outside it. If two registrations
share that identity and the path belongs directly to neither, the gate does not
choose. Intent searches are bounded to the repository so identified and read the
candidates inside `results.symbols`, the profile-aware envelope of the response.

A repository indexed with `kivgraph index` keeps its graph in its own
`.kivgraph/`, and the user's registry does not know it. When the working
directory falls in no repository of that registry, the gate looks for the
checkout's `.kivgraph/config.yaml` and reads it only if nothing in it could have
been committed to say otherwise: neither `.kivgraph` nor its configuration are
links, the state it names stays inside `.kivgraph`, and the endpoint published
there is this machine's. Without a daemon for that graph, the gate steps aside as
always.

## MCP tools in Oh My Pi

- `xd://` paths are discovered by querying `xd://`; they are never built by
  concatenating prefixes from a tool's visible name.
- Kivgraph's direct tools use `xd://mcp__kivgraph_<operation>`; through 1MCP the
  aggregated name is `kivgraph_1mcp_<operation>`.
- A form `xd://mcp__mcp_kivgraph_<operation>` must not be invented.
- An MCP `tools/list` response may be paginated: follow `nextCursor` until `null`
  before concluding that a tool is not mounted.

- `kivgraph_1mcp_index_project` and `kivgraph_1mcp_start_index_project` are
  Kivgraph's two mutating MCP tools; they are only registered on the configured
  `serve` path and require the client's explicit consent before changing the
  repository registry or publishing a generation.

## The skill is edited in one place

`kivgraph skill install` leaves, at user scope, a canonical file at
`~/.config/kivgraph/skills/kivgraph/SKILL.md` and links each client's path to
it. Editing it once reaches all of them, and an upgrade does **not** take the
change away: `install` only writes the canonical one if it is missing or if what
is there is the skill the build brings, and `--force` recovers that one. Project
scope copies, because an absolute link would be committed broken. See ADR 0078.

## Getting started

- The Go toolchain is the one in `go.mod`. `go/types` travels linked into the
  binary, so the language-version ceiling is that of the toolchain that compiled
  it; `kivgraph doctor` reports that number and not the `go` on the `PATH`.
- The pinned native library is downloaded and verified with `make ladybug-lib`.
  `make test-ladybug` resolves it by itself and exports the `CGO_*` variables.
- **`make build` does not produce a binary able to touch the canonical graph.** A
  `go build` without the `ladybug` tag leaves inside the fallback that declares
  `LadybugDB native support is unavailable`, so that binary **serves** an
  already published snapshot -- which is mapped and needs no cgo-- and in
  exchange fails everything that crosses LadybugDB: `index --full` dies at
  `stage.staging`, and its own `doctor` says `graph.store: FAIL`. It is a
  declared limitation and not a silence, but it is worth knowing which of the two
  binaries is in hand. The one that can do it is compiled the way the native
  suite does:

  ```bash
  LIB="$(scripts/fetch-ladybug.sh)"
  CGO_ENABLED=1 CGO_CFLAGS="-I$LIB" CGO_LDFLAGS="-L$LIB" \
    go build -tags ladybug -ldflags="-extldflags=-Wl,-rpath,$LIB" ./cmd/kivgraph
  ```

  With it, `doctor` ends in `PASS`. A published bundle already carries it linked,
  so to reindex the installed binary will do.
- The pinned Rust analyzer is downloaded with `scripts/fetch-rust-analyzer.sh`.
  The Rust suite also requires `rustup component add rust-analyzer rust-src` and a
  `cargo` on the `PATH`: without a toolchain the analyzer does not load the
  workspace, and without `rust-src` it installs it itself in the middle of the
  pass, which is a race when two test packages start at the same time.
- The three pnpm projects are independent and have their own lockfile:
  `pnpm --dir ts-worker install --frozen-lockfile`, and likewise for `web` and
  `landing`.
- If a command these instructions use is not installed, install it before going
  on. Do not substitute another one or skip the step.

## Before editing

1. Read `TASKS.md`, its dependencies, the applicable gate and the documentation
   of the affected subsystem.
2. Inspect existing implementations, tests and consumers; reuse the current
   convention instead of creating a second one.
3. Define the observable behaviour and its negative cases before touching code.
4. Do not hide warnings, errors, unresolved references, limitations or `FAIL`
   results.

## Never modify

- The indexed repositories and the benchmarks' input artifacts. Tests use copies
  or private fixtures.
- The generated directories: `ts-worker/dist`, `web/dist`, `landing/dist`,
  `dist/`, `.tooling/` and `landing/.astro`. They are regenerated with their
  build.
- What is pinned by digest: `tools/manifest.json`, `grammars/manifest.json` and
  the lockfiles. They are changed with the script that produces them, never by
  hand.
- The historical `LUQUE-####` identifiers of the backlog.
- The `CLAUDE.md` files: they are deliberate symlinks to the `AGENTS.md` of their
  directory.

## Semantic contracts that cannot be relaxed

- An `EXACT` edge requires sufficient evidence and the right provenance. It is
  never created by name, text, path, alias or single-candidate coincidence.
- `CANDIDATE` and `UNRESOLVED` are results distinct from `EXACT`.
- Every canonical edge has `confidence`, `provenance` and, when it applies,
  `evidence_key`; the evidence must be observed in a `File`.
- Stable keys are persistent. Do not change their algorithm, canonical identity
  or the historical namespace `luque-stable-key` without a data migration, an ADR
  and an explicit update of the contract.
- The LadybugDB schema is versioned. An incompatible change requires a full
  rebuild or a documented migration; an existing database is never modified
  silently.
- **The only indexing path is the full rebuild.** There is no incremental path:
  it was withdrawn in ADR 0057, measured at `1.67x` of a full pass and with no
  caller in production. `kivgraph index` autodetects the current project and runs
  that rebuild; `kivgraph index --full` keeps the explicit form over the
  configured registry. See ADR 0086.
- If an incremental path ever comes back, the withdrawal contract of ADR 0056 is
  the starting point and **cannot be relaxed**: every fact a file asserts is
  withdrawn and asserted again together with that file, and what a file asserts
  are the edges that **leave** its symbols -- an edge another file points at it
  was asserted by that other one, and withdrawing this one does not touch it. A
  symbol the `Upsert` asserts again keeps its node for that very reason; one that
  does not, goes with its edges, incoming ones included. Package edges are also
  withdrawn by their evidence even if both their ends survive. Relaxing it is
  defect `LUQUE-2002`: an edited file silently losing every incoming edge from
  another file.
- An incremental path cannot publish without verifying either. The withdrawn
  route made zero calls to `integrity` and to `golden probes`, and part of its
  measured advantage was that missing verification. And it would not produce a
  graph byte-identical to a clean rebuild: a row nobody reset would keep the
  `source_snapshot` and the `resolver_version` of the generation that observed
  it. That is provenance and no query filters by it; what must match is the
  content. See ADR 0056 and ADR 0057.
- Every `UNRESOLVED` keeps reason, repository and language; when a concrete
  occurrence exists it keeps its observed file, position and detail. Module
  failures at repository level may have no file and evidence or an `EXACT` edge
  is never fabricated for them.

## Surfaces that break compatibility

Before closing, check whether the change touches any of these. None is changed
silently: each requires an ADR, a documented migration or a full rebuild.

- The stable keys: algorithm, canonical identity and the historical namespace
  `luque-stable-key`.
- The LadybugDB schema and the layout of the generation store, its backup and its
  rollback.
- The viewer's `LGVB` payload, its version and its error codes.
- The names, descriptions and schemas of the MCP tools, and the skill that names
  them.
- The CLI's parameters and outputs, including the `index --full --json` protocol
  and `version --json`.
- The configuration: keys, accepted vocabularies and default location.
- The published bundle: the `kivgraph-<os>-<arch>` names, `manifest.json`,
  `SHA256SUMS` and the `RUNPATH`.

And a rule about **how** any of them is withdrawn: what was valid yesterday
cannot become a failure today. A configuration key that is withdrawn is
accepted, ignored and **named** -- `retiredConfigKeys` and `doctor`'s
`config.retired` line--, because the decoder rejects unknown keys and simply
deleting the field would turn every file written by an earlier `init` into a
startup error. The same holds for a format that goes up a version: a file of the
previous version is an update, not a damaged store, and counting it as a failure
turns `doctor` red on every installation -- which is exactly how a real failure
stops being noticed. Both forms have already been violated once each. See ADR
0062 and ADR 0061.

## Go

- Run `gofmt` on every modified Go file.
- Propagate errors with context using `%w`; do not discard errors.
- Blocking functions receive `context.Context` as the first argument after the
  receiver.
- Every goroutine has a clear owner and cancellation.
- Data is validated before building nodes or edges.
- Identifiers that mix domains use defined types.
- The packages under `internal/` are not a stable external API.
- Changing an exported symbol requires reviewing all its consumers and tests.

## Platforms and distribution

- The distribution targets are `linux/amd64`, `darwin/arm64` and
  `windows/amd64`, and only those. On macOS only Apple Silicon is published;
  `darwin/amd64` is out of scope by decision, not by cost, and the installer says
  so when rejecting it. The naming is `kivgraph-<os>-<arch>` for the directory,
  the archive root and the published archive. The container does change: Windows
  publishes a `.zip` because it is what its own tools read, and the rest a
  `.tar.gz`. A bundle is always built on a host of its own platform: cgo links the
  native library and there is no cross-compilation.
- `scripts/build-bundle.sh` is the only bundle generator; the targets
  `make build-linux-amd64` and `make build-darwin-arm64` delegate to it. The
  manifest, `kivgraph version --json` and `kivgraph update` validate against the
  running platform, never against literals.
- The scripts choose the digest tool by availability -`sha256sum`, otherwise
  `shasum -a 256`- and fail closed with neither. `--no-overwrite-dir` does not
  exist in macOS's `tar`.
- The bundle's executable declares exactly one `RUNPATH`, the relative one. The
  one the binding adds towards its module directory is removed after linking and
  the build fails if any other survives.
- macOS artifacts are not notarized and the project uses no Developer ID. The
  binary carries an ad-hoc signature, which is what Apple Silicon requires to
  run, and the script redoes it after editing its load commands. Gatekeeper only
  blocks a file with `com.apple.quarantine`, which neither `curl` nor `tar`
  write.
- Platform-specific code lives in files with a build tag, not in `runtime.GOOS`
  branches inside the logic. A `!linux` fallback that returns an error or zero is
  a declared limitation, never a silence.
- Installation documentation must reflect the generated layout, the `RUNPATH`,
  the required Node runtime and the `SHA256SUMS` verification; do not present a
  bundle as self-contained if system dependencies are missing.
- The published release carries the viewer. `kivgraph ui` is advertised in the
  help of every build, so a published binary that answers "this build carries no
  web bundle" offers a command nobody can run; the web assets are `2.3 MB` and
  the published bundle of `v0.5.0` weighs `43 MB` compressed. The release
  workflow builds without `--mcp-only` and verifies both halves: that
  `web/index.html` is in the payload and that the binary's help does not mark
  `ui` as unavailable -- a bundle with assets linked without the `webassets` tag
  would serve the "bundle not available" page on every route.
- `--mcp-only` still exists for whoever wants a bundle without the viewer.
  `scripts/install.sh` does not initialize the configuration or index
  repositories.
- `--slim` exists for the `.mcpb`, which has to fit under `25 MB`, and makes four
  cuts: it removes the Rust engine -- `bin/rust-analyzer`, `tools/manifest.json`
  and their licences, with `"tools": null` in the manifest, which is what
  `version --json` turns into a null `rust_analyzer`--, it compiles with
  `-s -w`, it `strip`s the native library it has just installed and it leaves of
  the worker's `dist/` only what `node` loads. They add up to `46.3 MB` down to
  `24.9 MB` of `.mcpb`: `14.9` the analyzer, `5.4` the executable's symbols,
  `0.9` those of the library and `0.15` the tests, `.d.ts` and source maps that
  travelled compiled.
- And what `--slim` does **not** do is download afterwards what it does not carry.
  MCPB defines a `binary` server as a self-contained package, so
  `ResolveAnalyzer` finds no sibling next to the executable, falls back to the
  `PATH` and `doctor` says which one answered: a slim bundle indexes Rust only if
  whoever installs it supplies their own. The library's `strip` is Linux-only --
  macOS's dylib carries an ad-hoc signature it would invalidate-- and leaves
  `ladybugdb.library_sha256` describing the installed file, which is no longer
  byte for byte that of the pinned archive; `archive_sha256` still names that one.
  The published release does not use `--slim`: it keeps analyzer, symbols and
  DWARF.
- Published releases use `vX.Y.Z` tags; `scripts/install.sh` detects the
  platform, downloads the latest published release for it, verifies the external
  and internal checksum, and `kivgraph update` only replaces the bundle after
  validating manifest, version and checksums. The release's `SHA256SUMS` lists
  all the published files, so the archive's own line is verified, not the whole
  file.
- A clean distribution build must be reproducible between checkouts of the same
  commit, toolchain and platform; it compares the full payload and not only
  `manifest.json`.
- Bundles are generated with `make build-linux-amd64` and
  `make build-darwin-arm64`; the `dist/` directory is generated and is not used
  as indexed or benchmark input.
- `kivgraph version --json` must keep exclusively JSON output on `stdout`; the
  bundle obtains provenance from `manifest.json` and validates the digest of
  `grammars/manifest.json`; values that cannot be observed are represented as
  `null`.
- `SHA256SUMS` lists SHA-256 hashes of `manifest.json` and of the payload in
  lexicographic order; it is verified with `sha256sum -c` or `shasum -a 256 -c`
  and does not include itself.

## Documentation and ADRs

- Technical documentation lives in `docs/`.
- Changes to architecture, MCP protocol, framing, persistent schema,
  compatibility or migration require an ADR.
- Documentation describes observed behaviour, not future promises.
- The OpenTelemetry metrics integration is optional; the exporters and collectors
  stay disabled by default and the configured provider belongs to the caller.
- Commands, codes, JSON fields and gates are written between backticks.

## The language of what is written

Every text this repository **delivers or publishes** is written in English:

- the documentation of `docs/` and that of `landing/`;
- the `AGENTS.md` files and the skills of `.claude/skills/`;
- the notes of a release -- which are the body of
  `landing/src/content/releases/vX.Y.Z.md`, and already were;
- the body and the title of a pull request;
- the message of a commit, which the delivery section already required;
- the code comments, which already are and do not change.

The reason is not preference. This repository's audience reads English -- the
`README`, the landing, the tool reference and the releases are already published
that way -- and a project that documents its contracts in one language and its
code in another forces every new reader to choose which of the two is the one
that rules.

**It is a ratchet, not a migration.** The rule holds for what is written from now
on; documents that predate it stay as they are until somebody translates them. It
is the same treatment as the column rule of `scripts/check-docs.sh`: a gate that
is born red is a gate one learns to ignore.

And from that comes the only part that is not obvious: **a file does not become
bilingual**. If a change adds a sentence to a document that is in Spanish, that
sentence goes in Spanish. Translating the file is a change of its own, whole, with
its own commit -- because half a page in each language reads worse than either,
and because a diff that translates and edits at the same time cannot be reviewed.

What is not delivered does not enter here: a conversation, a working note or the
text of a task go in whatever language the writer wants.

## Size and shape of a change

- A change that is not mechanical should not go over 800 lines, and 500 if it is
  complex logic. If it does, propose splitting it into reviewable stages from the
  real diff, its dependencies and its consumers, and identify the first coherent
  stage that can be delivered alone.
- Prefer a new package to fattening a large one. `internal/indexing`,
  `internal/mcp` and `internal/hotsnapshot` are the ones that attract unrelated
  changes; when extracting code from one, move its tests and the documentation of
  its invariants with it.
- This repository's commands are slow by construction: `index --full` lasts
  minutes, `make test-ladybug` links cgo and the Rust analyzer loads a whole
  workspace on every invocation. Wait for them. Do not kill them by PID or give
  them up as hung.

## Tests

- New tests must defend observable contracts and fail on a plausible regression.
  For storage or resolution changes, include negative tests, invariants and
  comparison against a clean rebuild when applicable.
- Do not add tests of statically defined values, or negative tests of logic that
  has just been withdrawn.
- Prefer comparing the whole object to checking field by field.
- Production code carries no functions that exist only for a test; the helper
  lives with the test.
- A fixture demonstrates the real case or demonstrates nothing: check what shape
  the tool being imitated really installs.

### Start with the negatives

The order is not a matter of style: the happy path writes itself and is checked
by looking at the response, whereas rejections, limits and empties are the ones
nobody runs until a user runs them. So they are written **first**, before the test
that shows the thing works.

For every new unit, before taking the good case for granted:

- **The input that is extra and the one that is missing.** The empty argument,
  the one that arrives twice, the one that is combined with another that
  contradicts it. A rejection is tested by its message as well as by its error: an
  `INVALID_ARGUMENT` that does not say which narrowing fixes it is a surface
  failure, not a validation.
- **The three empties, which do not mean the same.** There is nothing, there is
  nothing *within these limits*, and it could not be looked at. A test that
  confuses them blesses the worst answer this project can give: an asserted
  absence nobody established.
- **The limit from both sides.** Zero, one, exactly the maximum, the maximum plus
  one. And the right unit: counting bytes where the contract counts characters
  already let a one-character term through for every non-ASCII alphabet.
- **The corrupt shape nobody can build today.** A guard that only protects from
  what the neighbouring constructor never produces is an untested guard, and it
  stops protecting the day the data comes from a file.

### Coverage is measured and what is missing is named

The highest coverage the code admits is pursued, and it is measured instead of
estimated:

```bash
go test ./<package>/ -coverprofile=/tmp/cov.out
go tool cover -func=/tmp/cov.out
```

On new code the reference is **100 % of statements**, and reaching it is usually
a sign that the unit is the right size. When it is not reached, the branch left
out **is named and justified** in the test's comment or in the commit message.
Only two justifications count:

1. Reaching it would require the clock, the filesystem or the scheduler to behave
   in a way the test cannot pin down.
2. Reaching it would require a parameter, an injected clock or a function in
   production code that would exist only for the test -- which is already
   forbidden above.

An uncovered and unjustified branch is half-done work, not a decision. And
raising a percentage by adding tests of statically defined values is forbidden
above: coverage is the consequence of having tested the contracts, never the goal
that replaces them.

### An index or a cache is compared against brute force

Every structure that exists only to answer faster what another path already
answers carries a test that compares the two over a generated corpus: the index
returns exactly what the linear sweep returns, term by term. It is the only test
able to detect that it stopped saying the same, because a derived structure that
is wrong does not fail -- it answers.

## Mandatory verification

Before closing a task, run according to scope. The procedure -what each suite
covers, what gets skipped, how to narrow a failure and how to do the binary's
smoke test- is in the skill `.claude/skills/running-tests/SKILL.md`.

```bash
gofmt -l <modified-go-files>
go vet ./...
go test ./...
make build
```

If the change touches documentation under `docs/` or a `CLAUDE.md`:

```bash
scripts/check-docs.sh
```

It checks that every `CLAUDE.md` is still a symlink to its `AGENTS.md` and that no
**new** line under `docs/` goes past 84 columns. The second is a ratchet over the
lines that change, not over the files: forty documents predate the rule and
reflowing them is not the price of adding a sentence.

If the change affects native LadybugDB:

```bash
make test-ladybug
make lint-ladybug
```

`make test-ladybug` is the only supported way to run that tag: it exports the
`CGO_*` variables that point at the pinned library and passes the rpath through
`-ldflags`. `go test -tags ladybug` on its own does not link. `PKGS` bounds the
pass to one package.

`make lint-ladybug` answers the dead-code question, and it can only be answered
there. Under the default configuration the files behind the tag are not analysed,
so every symbol whose caller lives in one of them looks unreferenced: measured in
`20` findings, all `20` false. With the tag the answer is zero, and that is what
turns it into a gate instead of a wish.

What those flags do **not** repeat is part of the contract. `CGO_LDFLAGS` is
applied to every cgo package and not once at link time, so naming `-llbug` or the
rpath there -- which the pinned binding already declares -- printed `221`
`duplicate -rpath` and `ignoring duplicate libraries` warnings in a full pass.
None was a defect and together they hid the only one that does inform. Putting
them back in `CGO_LDFLAGS` brings them back; the rpath is declared once, where it
is linked once.

The rest of the gates live next to the code they verify: Rust in
`internal/rustloader/AGENTS.md`, the worker in `ts-worker/AGENTS.md`, the viewer
in `web/AGENTS.md` and the landing in `landing/AGENTS.md`.

And there are five that only CI runs, because they ask for network or several
minutes, but that can be reproduced by hand when one of them fails:

|gate|command|what it requires|
|---|---|---|
|correctness|`staticcheck -checks='all,-U1000' ./...`|every class of `staticcheck`. `U1000` is the only exclusion and not for being noisy: under this build it cannot be answered, and it lives in `make lint-ladybug`|
|vulnerabilities|`govulncheck ./...`|zero **reachable** ones; one in a module that is not called does not fail|
|reproducibility|`scripts/check-reproducible-bundle.sh`|two builds of the same checkout, identical payload|
|coverage|`make coverage`|the whole Go suite, instrumented, above the statement floor|
|bundle smoke|`init` · `doctor` · `index --full` · `--smoke`|that the published binary indexes and that the fourteen tools answer|

`make coverage` **is** the `go test ./...` of the `verify` job, not a separate
gate: it measures with `-coverpkg=./...` while the tests run, so it cannot follow
a clean pass without being the same work twice. It measures across packages
because this repository tests across boundaries on purpose -- `index_project.go`
is exercised from `internal/mcp`, not from `internal/mcp/tools`--, and the
per-package number that `go test ./...` produces comes out almost three points
low and flags as untested files that are tested. `benchmarks/` and
`internal/testsupport` are left out: they are harnesses without tests of their
own and cost fifteen points that say nothing about the product.

It refuses to run if `dart`, `pyright-langserver` or `rust-analyzer` is missing,
because an absent analyzer skips its suite instead of failing it: without Dart the
total falls from `80.0 %` to `77.9 %`, below the floor, and the failure would read
as lost tests. `KIVGRAPH_COVERAGE_ALLOW_PARTIAL=1` measures anyway and does not
apply the floor, which is what a workstation without the five installed wants.

The floor is a ratchet: it goes up when the suite earns it, and lowering it is a
regression or a decision that goes in the commit message.

The smoke is the only one that tests the product against a **published**
generation, and that is why it exists: the two defects `v0.8.0` took with it
passed every test with fixtures and only failed there. It runs with a temporary
`HOME`, and exports `GOMODCACHE` to the real cache -- without that the main module
does not type-check and the graph comes out hollow with nothing saying so.

For local installation changes, run the flow with a temporary `HOME` and without
modifying indexed repositories:

```bash
kivgraph init
kivgraph doctor
kivgraph index --full
kivgraph serve
```

`kivgraph serve` must **resolve** the published generation before opening the MCP
transport and say whether it has one; the graph is read by the first query that
needs it, not by startup. Without a published generation every query that
requires a snapshot must fail explicitly, and a generation that cannot be mapped
cannot be confused with the absence of a generation: `graph_status` names it. See
ADR 0067, which measured it -- `48` of `51` real servers receive no call, and
loading just in case cost them `33` of their `40 MB`.

What decides the tool surface and the handshake's instructions is
**availability**, never the graph: the daemon builds one MCP server per accepted
session, so asking for the snapshot there maps it once per client. For the same
reason, comparing generations -- the reconciler, the startup log, the race arm at
publish time-- is done by identifier.

## Commits and delivery

- Conventional Commits in English, in the imperative and lowercase after the
  colon: `type(scope): subject`. It is what 95 % of the history meets; the median
  subject is 41 characters and none goes past 80.
- Types in use: `feat`, `fix`, `docs`, `chore`, `test`, `perf`, `build`,
  `refactor`, `ci`, plus `bench` and `audit` for measurements and qualifications.
- The scope is the subsystem -`indexing`, `mcp`, `rustloader`, `typescript`,
  `cli`, `web`, `landing`, `release`- and is omitted when the change is
  cross-cutting.
- The body explains the why when it is not obvious; it never narrates the diff.
- The title and body of a pull request go in English, like the commit. A PR is
  what the reviewer reads and what stays in the branch's history when the commit
  is no longer opened, so it cannot be in a language different from the commit it
  summarizes.
- Review the complete diff and `git diff --check`.
- Confirm that no old imports, paths, package names or commands of the project
  remain.
- Confirm that tests, documentation and consumers were migrated or that the
  exception is documented.
- Deliver with a clean Git state and concrete evidence of the commands run.
- Always edit the `AGENTS.md` that corresponds to the directory; the `CLAUDE.md`
  files are deliberate symlinks to them.
