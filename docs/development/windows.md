# Windows support: what it would take

`windows/amd64` is a published platform as of
[ADR 0079](../adr/0079-windows-is-a-published-platform.md), and nothing ships
it yet. That ADR takes the eight decisions this document kept arriving at;
what is here is the measurement behind them, so that whoever finishes knows
what is left and what it is made of.

It has been rewritten once. The first version answered the question by
cross-compiling, concluded that the port was small, and said in as many words
that a real host had to confirm it. A real host did, and disagreed with the
optimism while confirming the shape: the code compiled clean and then failed
184 of its own tests. Everything below is measured on that host.

Thirteen of those failures are left, and what they are made of matters more
than the count -- roughly half are tests exercising features the product
refuses on Windows on purpose. The section on what is left says which.

The count is not monotonic and that is worth reading rather than smoothing.
It rose from 31 to 34 when `stop` stopped pretending it could ask a process to
exit, because four tests asserted an escalation that does not happen there;
and `procstat` turned red the run after it was implemented, because a test
that spawned `/bin/sh` had been skipping for a different reason and started
running. Each fix reached the next thing wrong, which is what a suite is
supposed to do and what a falling number would have hidden.

## What this rests on

A Windows Server 2022 virtual machine running the repository's own suite,
fourteen times, with the fixes between the runs. What it needed installed,
beyond Go 1.26.6 and mingw-w64 GCC 16.2, is worth writing down: `git`, a
`python3`, Node and pnpm for the worker, `jq`, and a Git for Windows shell
rather than the minimal one, because `bash` is what builds a bundle. Without
`git` and `python3` in particular, eighteen failures are the machine rather
than the port -- the kind of number that gets quoted at a planning meeting and
should not be.

|run|what changed before it|packages ok|tests failed|
|---|---|---|---|
|1|nothing; the tree as it was|25|184|
|2|`fsync`|25|146|
|3|`filesystemCapacity`, `go.work` paths|28|61|
|4|program names, `HOME`, path order|31|38|
|5|manifest containment, `doctor`|32|33|
|6|assertions a platform cannot answer|34|31|
|7|`procstat`, and an honest `stop`|35|34|
|8|closing the event log|35|28|
|9|the worker's pipe, `ResidentBytes`|35|28|
|10|a child both platforms have|36|27|
|11|client integrations opened|39|21|
|12|fixtures that speak the platform|39|18|
|13|the distribution, and what testing it found|40|**13**|

Before any of that, the compile results the first version of this document
reported still hold, and one of them was strengthened: the tree builds for
`windows/amd64` with mingw-w64 as well as with Zig, so the CI job's toolchain
is the one that was verified, and `go build`, `go vet` and `staticcheck` are
all clean for that GOOS.

## What the product does there

The suite is not the product, so the product was run: a real `kivgraph.exe`,
built on the host with the `ladybug` tag against the pinned Windows library.

|step|result|
|---|---|
|`version`|`0.8.1`|
|`init --repository demo=... --languages go`|writes config and registry|
|`index --full`|**PASS**, generation `000001` published|
|`doctor`|**PASS**, every check|
|`daemon --addr 127.0.0.1:7777`|listens on HTTP *and* on the socket|
|MCP `initialize` + `tools/list` over HTTP|200, twelve tools|
|`find_symbol` over HTTP|`greet/greet.go:4`|
|MCP `initialize` over the `AF_UNIX` socket|answered|
|`stop`|**fails**: `procstat: this platform cannot enumerate processes`|

So the whole indexing path works -- Go loading, fact normalisation, staging,
the native bulk load, the integrity check, the hot snapshot, the golden probes
and the atomic publish -- and so does serving, over both transports. The
symbol came back as `greet/greet.go:4`, with the slash the canonical form
wants, which is the normalisation fix confirmed from the outside.

`stop` is the one user-facing command that is broken rather than unsupported,
and `doctor` reports the missing supervisor as `PASS (unsupported: install one
with kivgraph daemon install)` -- advice that cannot work there, which is worth
fixing before anyone reads it as a suggestion.

### The runtime the bundle will have to carry

`kivgraph.exe` would not start at all until the Microsoft Visual C++
redistributable was installed: exit `0xC0000135`, `STATUS_DLL_NOT_FOUND`.
`lbug_shared.dll` is built with MSVC and Windows Server 2022 ships none of
`vcruntime140.dll`, `vcruntime140_1.dll` or `msvcp140.dll`.

This has no analogue on the platforms that ship today -- the C++ runtime a
Linux or macOS host needs is already there -- so a Windows bundle has to
either carry those three DLLs beside `lbug_shared.dll` or state the
redistributable as a prerequisite and check for it. ADR 0026 records that the
bundle carries no system standard libraries; on Windows that sentence stops
being free.

## The shape of the work

The single most useful thing measured is that **almost none of this was
hard**. Sorted by what they actually were, the root causes fixed so far are:

- **A primitive Windows has that nobody wired.** The two cross-process locks,
  `filesystemCapacity`, `preallocate`. Each had a Unix implementation and a
  fallback that refused; each has a direct Windows equivalent -- `LockFileEx`,
  `GetDiskFreeSpaceEx`, `SetEndOfFile` -- reachable through
  `golang.org/x/sys/windows` or the standard library.
- **A POSIX assumption written out by hand in several places.** `fsync` on a
  read-only handle and on a directory, in four copies. "A program is a file
  with an execute bit", in five. Each copy agreed with the others, which is
  what made them dangerous: the same defect had to be found once per copy, and
  the reason for the difference had nowhere to live. Both now have one.
- **A path separator.** The `go.work` `use` directives, the file table in
  `facts.NormalizeSemantic`, the containment check on a bundle's grammar
  manifest.
- **A test that did not know Windows exists.** `HOME` where the platform reads
  `USERPROFILE`, mode bits where there are none, and assertions that the slash
  form of a path is the stored form.

The third and fourth categories are worth dwelling on, because they are the
ones that will keep happening.

### The failures that read as neutral

`facts.NormalizeSemantic` filled its file table with
`filepath.Clean(filepath.ToSlash(p))` and looked into it with
`filepath.ToSlash(filepath.Clean(p))`. The same two operations, opposite
order. `filepath.Clean` produces the separator of the host, so on Windows one
of them ends in a backslash and the other does not, and every Dart and Python
symbol was reported as referencing a file nobody had declared. Twelve other
normalisations in the same file already had it the right way round.

`loadGrammarProvenance` decided whether a bundle's manifest reference stayed
inside the bundle by testing `filepath.Clean(ref) != ref`. The reference comes
out of JSON and is slash-separated by contract, so on Windows a correctly
written bundle was reported as trying to escape its own root -- a security
check that fails closed against nothing but itself.

`doctor` asked whether each state directory's mode bits were narrower than
`0700`. Go reports every directory on Windows as `0777`, so the check could
not come out any other way. A permanent red is not a check; it is a line an
operator learns to skip.

Neither of the first two looks wrong. Both are the kind of line that gets
written once, reads as careful, and is a no-op on every platform the project
ships to -- which is exactly why a compile gate cannot find them and why the
`windows-cross` job in CI is worth having but is not worth trusting.

## What the last thirteen are

|cause|count|
|---|---|
|the host's Python, not the port|3|
|`update`, refused because no release has been published yet|2|
|POSIX mode bits|2|
|everything else, one apiece|6|

Only the last row is work. The first is the machine the suite ran on -- an
embeddable interpreter without the standard library a real install has. The
second is correct and stops being true the day a release goes out, which this
branch makes possible and does not perform. The third is a claim no platform
without mode bits can answer.

The deadlock is gone: `internal/tsworker` was costing whatever the test timeout
was -- 1500 seconds on the first run -- and passes in three.

## What is still open

### The deadlock, and what fixing it cost

~~`internal/tsworker` deadlocks.~~ **Fixed**, and the shape of it is worth
keeping. `*os.File` carries `SetReadDeadline` on every platform and answers
`os.ErrNoDeadline` for a handle the runtime cannot poll; `armDeadline`
discarded that error and `NewReader` decided it could interrupt a read by
asserting that the method existed. The reader claimed a capability it did not
have, and a blocked read never returned. `NewReader` probes now.

The transport is a named pipe there, because Go cannot associate the handles
`os.Pipe` returns on Windows with its completion port and a pipe opened
overlapped is pollable. ADR 0079 called this a transport change; it is also an
exposure change, and that was not in the decision. An anonymous pipe is
reachable only through a handle somebody inherited. A named one is an object
every local process can enumerate, and these frames carry the graph. So it
carries `FILE_FLAG_FIRST_PIPE_INSTANCE` against a squatted name,
`PIPE_REJECT_REMOTE_CLIENTS` against SMB, a single instance, and a DACL naming
this user by SID, SYSTEM and the administrators -- which is most of the work
decision 3 asks for on the daemon's socket, done here first.

### One thing has still never been exercised

`RunDetached` is unverified on Windows. Its test fixture is a `#!/bin/sh`
script, and porting it means writing every test body twice; what is unportable
there is the fixture and not the code under test. It is named here so that "the
suite passes" is not read as covering it.

The socket, which this document once listed beside it, has been reached. The
daemon starts, serves MCP over HTTP and over `AF_UNIX`, and the socket now
carries a DACL the daemon sets rather than one it inherits -- so the guarantee
is a property of the daemon rather than of where somebody pointed its state
directory. `internal/privateobject` states the rule once for both the socket
and the worker's named pipe, which needed the same sentence.

### The one prediction that came true, and the one that did not

The first version of this document named the generation store's use of
`os.Rename` and `os.RemoveAll` over an open database as the item most likely
to pass on a quiet machine and fail on a real one. It has not fired in the
publish path, and the store's suite is now green except for a mode-bit
assertion. It *has* fired twice, in `t.TempDir` cleanup, which is the same
Windows rule -- a file held open cannot be deleted -- reached from the test
harness rather than from the store. It is still worth reproducing deliberately
with a concurrent reader before believing the store.

What the same document got wrong was the ordering: it put the daemon's owner
and the socket first and treated the Go code as nearly done. The Go code was
where the work was.

## The daemon has an owner

`internal/supervisor` installs a Task Scheduler logon task. That is the
faithful translation of the other two backends rather than the obvious one: a
systemd *user* unit and a launchd *user* agent are both per-user and neither
needs root, so a logon task is the analogue and a service through the SCM is
not. A service starts at boot and outlives the logout, which is what a Windows
operator expects of a daemon -- and it runs under another identity, which
changes where the daemon looks for a configuration whose whole content is one
user's paths, and forces the installer to ask for elevation it otherwise never
needs.

Four settings in the task definition are load-bearing and each has a default
that breaks a daemon quietly: `ExecutionTimeLimit`, because the default ends a
task after 72 hours; `MultipleInstancesPolicy`, so a second logon does not put
a second daemon on one state directory; and the two battery settings, without
which a laptop never starts it. They are asserted by value, because the failure
each prevents looks like something else entirely when it happens.

Task Scheduler is not a directory of files the way systemd and launchd are:
registering copies the definition into a store of its own. So `status` has two
facts to check rather than one, and a definition on disk with no task in the
scheduler -- what an operator who deleted the task by hand leaves behind --
is reported rather than read as installed.

## Distribution

Done, and verified on a Windows host rather than reasoned about. The bundle
builds, the installer installs it, and the release matrix has a third row.

Four differences turned out to be structural rather than cosmetic, and ADR 0026
gains a row for each:

- **The archives are zips**, so `tools/manifest.json` carries a per-platform
  `archive_format` and the fetch scripts choose an extractor by what the
  archive is. The tar in a Git for Windows shell is GNU tar and cannot read a
  zip; the one in System32 is bsdtar and can, so neither is assumed.
- **The library name changes**, and with it the object the linker resolves:
  `lbug_shared.dll` with an import library that names it, which is why the
  binding's `-llbug_shared` finds it.
- **There is no `RUNPATH`**, and that is a change to the bundle layout rather
  than a flag. `lbug_shared.dll` sits beside `kivgraph.exe`, `lib/` does not
  exist there at all -- it was created empty for one build, which is a claim
  about a layout and was the wrong one -- and the check that guards the RUNPATH
  elsewhere asserts adjacency here.
- **The installer is a different language.** `scripts/install.ps1` reimplements
  one set of pre-extraction checks, and `internal/release/install_parity_test.go`
  makes the drift ADR 0079 predicted into a failure: both scripts mark each
  check, the sets must match, and the set itself is pinned so that losing a
  check from both is also a failure.

### What running it found that reading it would not

Three defects, one shape: a tool on Windows formats its output the way that
platform does, and something downstream compares it for equality.

- `jq` ends its lines with a carriage return, so the last field of each line
  carried one. An archive format matched no `case` while printing as though it
  did, and a licence digest failed to equal itself -- the message read
  `expected X, got X` with the two strings identical on screen.
- `sha256sum` defaults to binary mode and marks the file name with an asterisk,
  so a release manifest named `*kivgraph-windows-amd64.zip` and matched
  nothing.
- .NET's `ZipFile.CreateFromDirectory` writes the host separator into entry
  names, where a zip stores forward slashes by specification. The installer
  refused the archive, which is correct, and said only "unsafe path" -- which
  sends a maintainer looking for an attacker rather than for their packer.

None of the three is subtle once seen. All three were invisible until the code
ran on the platform, and the first took a `bash -x` trace to find at all.

### The Visual C++ runtime

`kivgraph.exe` does not start without it: exit `0xC0000135`,
`STATUS_DLL_NOT_FOUND`, because `lbug_shared.dll` is built with MSVC and
Windows Server 2022 ships none of its runtime. The installer installs the
redistributable rather than the bundle carrying the DLLs, because Windows
Update services the redistributable and services no copy of it -- a security
fix that reaches every other installation and not ours is not a trade a
self-contained bundle wins.

## What is left

ADR 0079's four steps are done. What remains is smaller than any of them and
mostly not defects:

1. **The long tail.** A handful of failures, and about half are not defects:
   `update` refuses Windows because no release has been published yet, which
   this branch makes possible and does not perform; and the Python analyzer
   failures are the embeddable interpreter on the machine that ran the suite,
   not the port.
2. **Two claims that want a real installation rather than more code.** The
   Claude Desktop detection markers are a guess about where the installer puts
   things -- the first is the directory the application itself creates, which
   is a fact; the second is not. And `RunDetached` above.
3. **A first release.** Everything here has been verified on one Windows host
   with a served archive standing in for a release. The matrix row has never
   run on a GitHub runner.

That third one is the honest limit of this branch. It builds, installs and runs
on a machine; it has not yet been published from CI, and a release job has ways
to differ from a laptop that no amount of local verification anticipates.

## Reproducing this

Nothing here needs the virtual machine to compile-check a change:

```bash
CC='zig cc -target x86_64-windows-gnu' \
GOOS=windows GOARCH=amd64 CGO_ENABLED=1 go vet ./...
```

Running the suite does need a host. What that one had installed, beyond Go and
mingw-w64, was `git` and a `python3` on `PATH` -- without them eighteen
failures are the machine rather than the port, which is the kind of number
that gets quoted at a planning meeting and should not be.
