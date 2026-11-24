# CLI instructions (`cmd/kivgraph/`)

These rules add to the `AGENTS.md` at the repository root, which is always
read. An instruction in this file may add restrictions; it can never relax an
integrity, compatibility or verification contract declared at the root.

What these commands run lives in `internal/`; only their observable surface is
here.

## The command table

- The command line is declared **once**, in `cmd/kivgraph/commands.go`, and
  read three times: the dispatch that runs, the help that lists and the
  completion that suggests. It used to be declared twice -- an `if` chain to
  dispatch and a separate table for the help -- and the two had already
  diverged: ten commands carried flags the help never named. A third copy for
  completion would have diverged just the same, and a completion that omits a
  third of the surface is worse than none, because the reader believes it.
- **Flags are not repeated in the table.** Each command has a
  `<name>FlagSet` constructor and that is the only place a flag is written;
  `writeFlagList` already walks a real `flag.FlagSet`. A new command adds its
  `<name>Options` and its `<name>FlagSet` next to its `runX`, as `uiFlagSet`
  and `serveFlagSet` already did, and an entry in the table.
- The `usage` field is a **summary curated for a person**: it may name fewer
  flags than the command accepts -- listing the eight of `logs` on one help
  line makes it worse -- but never one that does not exist, and
  `TestUsageNamesOnlyRealFlags` checks it. Completion does not depend on it: it
  reads the `FlagSet`, so it is exhaustive.
- Words are matched **from longest to shortest**, which is what prevents
  reading `doctor storage` as `doctor` with a loose argument, or `index --full`
  as `index`.
- `serve` and `ui` are in the table with `run: nil`. `main` intercepts them
  before `run` because they are the two that own a signal handler and log
  structurally for their whole life; they are declared so that the help and the
  completion describe them the same.

## Completion

- The script of each shell is a **fixed stub**: no command names, no flags and
  no vocabularies. It forwards the words to `kivgraph __complete` and every
  candidate comes from the table. A script that has to be regenerated when a
  flag is added is a script that will be out of date, and
  `TestCompletionScriptEmitsOnePerShell` fails if a stub hard-codes a name.
- The bash stub is written for **bash 3.2**, which is what macOS ships: no
  `mapfile` and no `compopt`. Verified by running it there, not assumed -- the
  first version used `mapfile` and failed on every invocation.
- bash's `COMP_WORDBREAKS` contains `=`, so it delivers `--kind=s` as three
  words where zsh and fish deliver one. The engine recognises both forms, which
  keeps the stubs identical and without a reassembly of their own for each
  shell.
- A flag that takes a path answers `:file` and the shell uses its own
  completion: quoting, links and `~` are its job, and a list generated here
  would get them wrong.
- The candidates a static script could not give are what justify the design:
  `--generation` reads the generations on disk, `--target` the clients on this
  machine and `--tool` the tools this installation has really received, from the
  durable registry. A command this build cannot run is not offered.

## Help, outputs and logging

- Help is not an error: `--help`, `-h` and `help` write to `stdout` and end with
  code `0`. **An invocation with no command is the same question** -- "what does
  this do" -- so it writes that same table to `stdout` and ends at `0`; it used
  to write one line to `stderr` pointing at a help the reader had not yet seen.
  An **unknown** command is an error: one line on `stderr`, code `2`, and never
  the whole surface -- whoever mistypes `inedx` does not need to read the
  catalogue. A one-shot command reports in plain text when `stderr` is a
  terminal and as a JSON log when it is not; `serve` and `ui` always log in
  JSON, because a client reads their `stderr`.
- Help marks the command this build cannot run. The published MCP bundle does
  not carry `webassets`, so advertising `ui` without saying so describes a
  capability that does not exist.
- A one-shot command classifies what it writes to `stderr`: progress is `INFO`
  and only a failure is `ERROR`, and the text of the line is the log's `msg`,
  never a field inside a fixed message. A log that a reader can neither filter
  by level nor find by text reports nothing.
- `doctor` reports `snapshot.published`, and the answers that are not a usable
  file **are not worth the same**. There are three, not two: **absent is `PASS`
  and is declared** -- a generation published before the file existed does not
  carry it, and deriving is what was always done --; **written by an earlier
  format is `PASS` and is declared**, because there is nothing wrong with the
  store, the layout moved, and the next indexing replaces it; and **present, of
  this format and not usable is `FAIL`**, because then something in the store is
  wrong. A single "unavailable" answer would count them as the same, and only
  the third deserves to wake anyone. Counting the second as a failure is worse
  than not reporting: it turns `doctor` red on every update, which is exactly
  how a real failure stops being noticed. `hotsnapshot.ErrSnapshotFileVersion`
  tells them apart, wrapping `ErrInvalidSnapshotFile` so that the other callers
  do not change.
- `serve` says on startup whether it read the published snapshot or derived it,
  and for what reason. Nothing else tells them apart: a server that derives
  answers exactly like one that read, and costs a gigabyte more of peak at
  birth. While nobody said it, that confusion cost twice in one afternoon -- the
  start deriving and a file of an earlier format not loading -- and no suite
  saw it, because both paths produce the same graph.
- `stats` leads with **proportional memory**, not resident, and that choice is
  the reason the command exists: three servers that read the same mapped
  snapshot each count all their pages, so adding up the residents reports a
  machine spending three times a file that is there once. Measured: RSS rose
  from 114 to 141 MB per server while the cost fell from 117 to 79. Where the
  platform does not know how to split shared pages -macOS does not- no
  approximation is offered: it leads with the resident and the note says that
  what is shared is counted once per process. Guessing which pages are shared is
  how a mapped file starts to look like a leak.
- The peak is a column of its own because it is what sizes a machine: a server
  that reached a gigabyte needs that gigabyte even if it parks at a tenth of
  it. An indexing pass is coloured differently precisely because it explains a
  peak a server would not.
- A live view nobody watches is a command that does not end: with `stdout`
  redirected, `stats` prints **one** observation and exits, and `--json` is that
  same observation for a script. The interactive view honours `NO_COLOR` and
  `TERM=dumb` like the rest of the surface.
- `doctor` reports the version ceiling with which this binary type-checks, not
  only the `go` on the PATH: they are different numbers and the one that decides
  whether a repository can be indexed is the first.
- `logs` and `tool-stats` read the durable log of `internal/eventlog`, they do
  not ask a server, and that is why they can answer: the per-tool counters a
  `serve` keeps are minted on startup and discarded on exit, so a question asked
  from another process -- which is what a command is -- would always find an
  empty record. Reading the file also makes the answer span every server that
  ran, which is the interval the question implies. See ADR 0049.
- The `logs` badge names **what** happened, not just how bad it was: a failure
  is `ERROR` and a degraded response is `WARN`, but a call that answered is
  `TOOL` and a pass is `INDEX`. A log in which every routine line said `INFO`
  would leave the reader doing the classification the writer already knew. It is
  the only surface with a solid background: the rest of the non-TUI output has
  only foreground colour, and a `styleFor` that returns the empty style is still
  what keeps the redirected view in plain text.
- Duration stays **out of** the identity that collapses repeated lines. Each
  call has its own time, so including it would mean nothing with a duration ever
  collapses -- and twelve `find_references` in a row are exactly what a reader
  wants to see as one row with its mean, not as twelve rows to get through. The
  row carries the instant of the most recent occurrence, because the question a
  repeated line raises is whether it is still happening.
- A rendered field is folded to one line and bounded. A Go loader failure
  arrives with line breaks and a transcript of `stderr` inside: printing it as
  is breaks the one-record-per-line contract that every filter and every reader
  of this output depends on. Observed in the first pass that really failed, not
  assumed.
- Percentiles live in the reader. `internal/metrics` retains latency as count,
  sum and maximum on purpose; the file stores every call, so `tool-stats` can
  answer what a mean hides -- a tool whose median is fast and whose tail is
  not -- and only colours the row that failed, because a table where every line
  is painted does not say which line to read.

## `kivgraph doctor repositories`

- It answers whether a registered repository is structured so that a pass can
  read it, **without indexing it**, and says what to change when it is not. It
  is the only way to know that does not cost minutes: until now a coverage hole
  only showed up as one warning among hundreds at the end of `index --full`.
- **Every check asks the code the pass asks**: the same package registry, the
  same source resolution -which is why `workspace.ClaimedTypeScriptSources`
  exists-, the same `go list` with the same patterns -which is why
  `goworkspace.PackagePatternsForModule` is exported- and the same
  `cargo metadata`. A check that reimplemented any of them would be answering
  for a pass nobody runs, and that is worse than not answering: it would
  approve a repository the pass discards.
- The Go patterns are named one by one **because `./...` is not enough**: a
  package whose files the build configuration excludes does not match it, so
  `go list -e ./...` does not report the exclusion. Measured: zero findings with
  `./...`, the two real ones when naming the packages.
- **A remedy is proposed, never applied.** Kivgraph does not write inside the
  code it indexes, and the `Remedy` is meant to be applied by whoever reads it:
  path, exact content, configuration key or command. A remedy that cannot name
  the tag it asks for -Go- reads it from the files themselves with the
  compiler's parser, because "grant the tag" without saying which cannot be
  applied by anyone.
- It exits with code `1` **only** if something is `blocking`, that is, if a
  repository or a package contributes nothing. A `partial` finding is a hole
  its owner may have chosen, and a gate that failed for those would be a gate
  nobody can pass.
- `--json` is the output for an agent: the whole report, with the stable `code`
  of each finding and its remedy. The text output is the same information for a
  person.

## The `index --full --json` protocol

- A registry entry that cannot be registered -a deleted directory, one that is
  not a checkout, a path with a symbolic link- no longer brings down
  `index --full`: it is left out and named in `index.repository.not_read`, on
  `stderr` with `--json`. If none is registered it is still an error. `clean`
  reads the profile, which is where a pass publishes.
- `kivgraph index` is the local form of the same pass: it resolves the current
  directory, detects the supported languages, prepares `.kivgraph/` and
  registers the `project` repository before delegating to `runIndexFull`. If
  neither `--config` nor `--repositories` is passed, it never writes the user's
  shared registry. `kivgraph index --full` keeps the explicit form over the
  configured registry; neither path is incremental.
- The `index --full --json` flow is a protocol, not a logbook: `stdout` carries
  only JSON events per line -`progress`, and a single `result` at the end- and
  the report a person would read is not written in that mode. The counters of
  the six languages travel in the `result`; deriving them from the `msg` of a
  log would turn text for humans into an API. A reader ignores a class of event
  it does not know.
- `--unattended` is the pass nobody asked for: the one the watcher starts. It
  does not publish if a file that the active generation reads, and that is still
  on disk, is left without declarations in a repository and language where the
  pass records a `_NOT_LOADED` failure. The `result` then carries `lost`, up to
  twenty `repository:path`, and the daemon does not retry on a timer: the same
  sources give the same refusal. A requested pass publishes as always. See ADR
  0152.
- **What was not loaded travels with what was.** `go_modules_not_loaded`,
  `rust_workspaces_not_loaded`, `python_repositories_not_loaded`,
  `dart_repositories_not_loaded` and `java_repositories_not_loaded` are in the
  `result` and in the lines of the human report, all five. Without them a reader
  cannot tell a language with no code from one this machine could not read: both
  add up to zero symbols. The reason differs -- a workspace the analyzer did not
  read, a module that did not load, an analyzer that is not installed-- and the
  consequence does not.
- **And the counter names its modules.** Each Go module that was not read comes
  out as `index.go.not_read: <repository>: <module>: <reason>: <detail>` in the
  human report, with the same cap of twenty lines as the diagnostics. The counter
  alone left one question unanswered: `not_loaded=4` with `diagnostics=1` does
  not say which four, and the reason lived only in the `MODULE_NOT_LOADED` rows
  of the published graph, which no command reads. `index.go.diagnostic` is
  another class -- what the loader said about a module it **did** read-- and
  mixing them would lose exactly that distinction.
  Python, Dart, Java and C# have the same line since ADR 0147:
  `index.<language>.not_read: <repository>: REPOSITORY_NOT_LOADED: <detail>`,
  one per repository that its analyzer ran and could not read. It does not come
  out for an analyzer that is not installed: `doctor` says that, once, about the
  machine. TypeScript has its own, `index.typescript.not_read`, for a repository
  whose projects could not be discovered and for a package in which the worker
  failed, and its summary line carries `not_loaded` like the others.
- **`counts` and `index` of the `result` do not measure the same thing, and that
  must be said.** `counts` is what the published graph stores: distinct
  canonical facts. `index` is what each language pass **observed**. A file that
  belongs to two packages -- `pkg` and `pkg.test` -- is observed twice and
  stored once, so the two blocks of the same event diverge without either being
  wrong. Measured on `workspace` with `include_tests: true`: `index` adds up
  `146,600` symbols and `counts.symbols` says `124,073`; `go_definitions` runs
  `1.63x` above the graph's Go symbols and `go_unresolved` `1.58x`, while Rust --
  one pass per workspace -- matches exactly. With `include_tests: false` the Go
  unresolved match: `4,397` and `4,397`.
  Whoever compares the two figures and expects them to add up is comparing work
  done with facts stored. The loader's counters are not deduplicated: the number
  of observations says how much the pass worked, and it is the only figure that
  says so.

## Commands that destroy or end state

- `kivgraph clean` removes published generations: it enumerates and touches
  nothing without `--yes`, because there is no undo -- it also takes the backup
  that `rollback` lives on. With no flags it leaves the store empty and frees
  the reserved space; with `--keep-active` it keeps exactly the published
  generation. It never touches the configuration or the repository registry.
- `kivgraph stop` ends this user's long-running processes and nothing else. The
  list is `longRunningCommands` -- `serve`, `daemon` and `ui` -- and it is a list
  and not a rule because the rule would be false: `index --full` also lasts
  minutes, and killing one halfway through publishing is not what whoever orders
  a server stopped asks for. **A new long command is added there and to the
  "nothing running" message, which names it:** a reader who left a daemon and
  only hears about `serve` concludes that `stop` does not manage it. It selects
  by invocation, not by executable: `stop` itself does not kill itself. It sends
  `SIGTERM`, waits for the bounded graceful shutdown and only then `SIGKILL`,
  and before escalating it checks again that the pid is still the same
  invocation: a pid freed during the wait may already belong to another process.
  `--dry-run` enumerates without signalling.
- `kivgraph update` offers to stop what survived the bundle it replaced. A
  `serve` or a `ui` that was already running keeps answering from the image that
  was swapped out -- with the old tools, the old descriptions and the old bugs --
  and nothing in its output says so; the client that launched it does not
  restart it on its own. **Stopping nothing is the default** whenever the
  question cannot be asked: they are processes a client owns, and ending one
  silently looks exactly like a crash. `--stop` is the scriptable answer, and
  the escalation is that of `stop` -- a single copy, in `stopTargets` -- not a
  second similar one. An installation that worked is never reported as a failure
  because the process list could not be read.

## `kivgraph serve`

- The **entry** is STDIO and is permanent; the **server** is not. Since ADR 0084
  `serve` forwards the session to a daemon when one is reachable and loads no
  graph: the decision is taken **before** the store, the generation follower and
  the resync, because a relay needs none of the three and paying for them would
  give back exactly the per-client cost the relay exists to remove.
- The two reasons the entry cannot stop being STDIO: the `.mcpb` manifest
  describes a local process and **has no field for a url**, and a `url` entry
  carries the literal token into a file that gets committed. Measured in ADR
  0083, the `.mcpb` is `39 %` of downloads.
- What it costs, measured in `benchmarks/relay-cost` on generation `000091`: a
  `serve` that answers is `68.9`-`70.3 MB` per client against `8.7`-`9.8` for the
  relay, and at eight clients the peak is `2.5 GB` against `0.44`. **Idle, the
  margin is much smaller** -- the relay does not win until the fifth client --
  and that is the load of `48` out of every `51` sessions: whoever defends this
  for the case that predominates is defending `10`-`15 MB`.
- **Declining is not failing.** With no published endpoint, with an endpoint
  nobody answers, or with `KIVGRAPH_SERVE_IN_PROCESS` set, `serve` answers
  itself and on those paths nothing ends up worse than before. Nor does it
  install a daemon when it runs under a `HOME` that is not the account's
  (`foreignHome`): the supervisor would start it with the account's, against
  another installation, and the unit would stay registered when the directory
  it was installed from no longer exists. The explicit path -`ensureDaemon`,
  behind `configure` and `mcp install`- applies the same check: it writes the
  stdio entry and says why, and an explicit `--daemon` is rejected instead of
  degraded. The environment variable exists because the relay is the default in
  a process an unsupervised client launches: without an escape, a machine where
  the relay misbehaves has no way back that does not go through stopping the
  daemon the others use.
- **Version skew is refused, and it is the only failure this design adds.** The
  `.mcpb` brings its binary but `stateDirectory` comes from the configuration, so
  two installations share a daemon by construction; restarting it would take the
  graph away from whoever is using it. The relay compares the version the
  daemon's `initialize` carries and refuses **before** handing the handshake to
  the agent, which is what avoids leaving anybody with a session this process
  had already decided not to serve. The message names both versions and the way
  out.
- A daemon that does **not** name a version is served anyway: it is a release
  earlier than this check and has the same right to answer it always had.

## The first-run notice

- The first time a version serves MCP on a machine, `serve` and `daemon` write
  three lines to `stderr` saying what is reported and how to switch it off, and
  send a ping. **Never to `stdout`**: `serve` speaks MCP over that descriptor and
  a stray byte corrupts the session, so `internal/telemetry` tests it by
  replacing `os.Stdout` with a pipe. What is sent is in
  `docs/development/analytics.md` and published at `/telemetry/`.
- **The transport comes from the command, not from a literal.**
  `runConfiguredServe` runs `serve` and `daemon`, and its call passed a fixed
  `"stdio"`: the daemon reported its first start as stdio. The marker is created
  once per version, so that wrong row would have been the only one that version
  would ever produce. A `serve` that forwards reports `daemon`, which is the only
  case where the command and whoever answers disagree.
- It does not report a binary that does not run from a release layout: nothing
  tells apart a `go build` of our own from one in a CI job, and counting them
  would make the number mostly us. `KIVGRAPH_TELEMETRY=0` switches it off
  entirely.

## `kivgraph daemon`

- It serves MCP to several clients from one process, through **two doors at
  once**: a unix socket in the state directory and the Streamable HTTP transport
  on loopback. It shares with `serve` the whole assembly -- config, store,
  generation follower, resync, event log -- through `runConfiguredServe`, which
  receives the name of the command so that its labels tell the truth: two
  commands with a fixed `serve` in the logs are indistinguishable in the same
  event log.
- **The socket lives inside the state directory, and that is the key.** Two
  configurations pointing at different directories get different daemons, so a
  client never reaches a graph built from another's repositories. A socket per
  machine or per user would have been simpler and would have crossed exactly
  that.
- A unix address is a fixed-size field -- `104` bytes on darwin, `108` on
  linux -- and `bind` **truncates** instead of rejecting, which would leave two
  directories sharing a socket. It is checked against the smaller of the two
  limits, so that a directory that works on one platform works on the other,
  and the error names the figure. It is a declared limitation: whoever does not
  fit uses `serve`.
- **One MCP server per accepted session, not one per process.** The surface of
  tools is decided when a server is built -- a process with no published
  generation publishes only `index_project` -- and a daemon outlives
  generations, so one built at startup would tell every future client there is
  no graph. The store, the metrics registry and the indexer are shared: the
  snapshot is mapped once and what `graph_status` reports belongs to the
  process.
- A stale socket and a live daemon are not the same state, and the only way to
  tell them apart is to try talking to it: if it answers, it is not replaced.
- The saving is measured in `benchmarks/daemon-cost` **at the load an editor
  really produces**, counted from the event log: `48` of `51` server processes
  received **no** call at all, so the median of a real session is zero.
  Measuring `2,000` calls per session described nobody.

  |load|daemon's slope|N processes|8 clients|1 client|
  |---|---|---|---|---|
  |none|indistinguishable from zero|`10` MB/cli|`10`–`13` against `77`–`81 MB`|the daemon `+2`–`3 MB`|
  |`8` calls|`0.6`–`0.9` MB/cli|`39` MB/cli|`60`–`62` against `323`–`330 MB`|ties|

  **Startup is no longer the cost, and that is what makes this command worth
  less than it was**: since ADR 0067 the graph is read by the first query, so an
  idle server costs `10 MB` and not `33`. What a daemon still saves is the rest
  -- and for one client it now **loses** by a couple of megabytes, which is the
  extra process.

  **The two doors are indistinguishable at both loads**, so the earlier warning
  -- that HTTP cost `12.5 MB` per client and lost with one client-- is withdrawn:
  it was the SDK's resumption buffer filling up with synthetic traffic. Under
  sustained load it does cost more (`11`–`13`), and that is a ceiling that no
  real session reaches and that depends on the corpus.

  The biggest difference is not the slope: it is the **peak**, `179`–`186`
  against `26`–`29 MB` at eight clients, without a single query. What is not the
  saving at either door is the snapshot: it is already shared and those pages
  are clean.
- **The unit records the `PATH` of the terminal that installed it**, and without
  that the daemon runs with the supervisor's: neither systemd nor launchd reads
  a shell profile, so nvm's node and Homebrew's are not there,
  `kivgraph-ts-worker` dies at `exec node` with a `127`, and the Go in
  `~/.local/go/bin` silently loses to the one in `/usr/bin` -- which is worse,
  because it does not fail: it publishes another graph. None of this is visible
  from the terminal, where the same `index --full` works. `status` compares the
  unit **without** the recorded `PATH`, because it belongs to the terminal that
  installed it and not to the daemon: comparing it would say `stale` from any
  other shell. What it does compare is whether there is one recorded, and that
  is why every installation earlier than this says `stale` once. See ADR 0085.
- **`daemon install` refuses a `HOME` that is not the account's**: the unit is
  written under `HOME` and registered in the account's session manager, so a
  throwaway `HOME` leaves an agent whose directory takes the directory with it.
  It is compared by identity, not by text, and it does not refuse if it cannot
  compare. A test that reaches the real `Install` installs on the machine of
  whoever runs it: those of this package stop before it. See ADR 0157.
- The resyncer **gives up** after `ResyncAttempts` consecutive failures of the
  same unchanged batch -- five, like the unit's `StartLimitBurst` -- and says so
  once through `OnGaveUp`. The batch is identified by its content, so a new move
  recovers the whole count and the bound can never suppress work nobody has
  tried. Giving up does not rewind: the tracker stays at the commit where the
  tree is, which is what prevents the batch from being proposed again.
- `kivgraph mcp install` is what makes everything above usable: it reads
  `daemon.json` from the state directory and writes a `url` entry with the
  token. If there is no supervisor, it asks before installing one; a negative
  answer or a non-interactive exit keeps `stdio`. `--daemon` expresses consent
  without a prompt. In `project` scope it refuses: that file gets committed.
- Human output reuses the ANSI colour of `styleFor`: it is only active on a
  terminal, honours `NO_COLOR` and `TERM=dumb`, and adds no escapes to a pipe.
  `daemon status`, `graph status`, `rollback` and `snapshot` present a table on a
  terminal; redirected text keeps the linear form scripts consume.
- **The daemon publishes HTTP before binding the socket, and the order is a
  contract.** A unix socket accepts as soon as it is bound, before anybody calls
  `Accept`, so reaching the socket has to imply that `daemon.json` already
  exists. Reversing it returned "no such file or directory" on a live daemon in
  one of every three passes of the benchmark. And if the socket's `bind` fails
  afterwards, the endpoint is withdrawn: a file that claims a daemon is there
  sends the next client to a closed port.

## `kivgraph ui`

- `kivgraph ui` records the address it bound, including the one a port `0`
  resolves to, and refuses to start when the binary does not carry the
  `webassets` tag: the published MCP bundle does not carry it, so it could only
  serve the "bundle not available" page.

## Verification

A change to the CLI surface is a compatibility change: review
`cmd/kivgraph/help.go`, the `landing/` documentation and `scripts/install.sh`.

```bash
go test ./cmd/...
make build
```
