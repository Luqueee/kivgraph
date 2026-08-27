# ADR 0079: windows/amd64 is a published platform

- **Status:** accepted
- **Date:** 2026-08-27
- **Revises:** ADR 0015, ADR 0026, ADR 0068
- **Changes the MCP protocol:** no
- **Changes the persistent schema:** no
- **Forces a rebuild:** no
- **Changes a tool's output:** no

## What was happening

Kivgraph published `linux/amd64` and `darwin/arm64`. Nothing refused Windows
on purpose; it had simply never been built there, and what had accumulated in
its absence was not the large port the repository's shape suggested.

Measured on a Windows Server 2022 host: the tree cross-compiled after two
missing build-tag fallbacks, and then failed 184 of its own tests. Six rounds
of fixes later it fails 31, and a real `kivgraph.exe` indexes a repository,
publishes a generation, passes every `doctor` check and serves MCP over both
its transports. `docs/development/windows.md` holds the measurements.

What the failures turned out to be matters more than the count. Almost none
of them were hard: a primitive Windows has that nobody wired, a POSIX
assumption written out by hand in four or five copies, a path separator, and a
test that did not know Windows exists. What is left is not more of the same --
it is eight decisions about what Windows should be promised, which this ADR
takes so that nobody has to take them one at a time while implementing.

## Decision

`windows/amd64` becomes a published platform: a bundle, an installer, an entry
in the release matrix, and a line in `SHA256SUMS` beside the other two.

### The eight

**1. `stop` gets a real process table.** `internal/procstat` is implemented
with `CreateToolhelp32Snapshot` for the PIDs, and the command line is read out
of each process's own memory: `NtQueryInformationProcess` for the PEB address,
then three `ReadProcessMemory` calls, then `DecomposeCommandLine`. Toolhelp
alone would leave `procstat.Process.Args` empty, and `Args` is what tells a
`serve` from an `index`: a `stop` that cannot tell them apart is a `stop` that
ends the wrong process.

*Amended after writing.* This first said `Win32_Process.CommandLine` through
WMI, on the reasoning that WMI is what answering the whole question costs
here. It is not: `golang.org/x/sys/windows` already binds every call the PEB
route needs, including the `PEB` and `RTL_USER_PROCESS_PARAMETERS` layouts and
`DecomposeCommandLine`, which applies the same rules `CommandLineToArgvW` does
-- the split the process itself used. So the reachable route costs no COM
dependency, no subprocess and no per-call marshalling, and the decision that
rested on the cost had the cost wrong. The choice stands; its reason changed.

A process this cannot open is skipped rather than reported, which is the trade
the Linux implementation already makes with an unreadable `cmdline`: a process
it cannot read is not one it could have signalled.

**2. Worker framing moves to a named pipe on Windows.** *Done, and it cost
one thing this decision did not price.* A named pipe is not only a transport
with different polling behaviour; it is an object in a namespace every local
process can enumerate, where the anonymous pipe it replaces was reachable only
through an inherited handle. The frames carry the graph, so the implementation
narrows it back -- first-instance, no remote clients, one instance, and a DACL
naming this user by SID -- which is most of what decision 3 asks for on the
socket, written here first and testable there.

The original reasoning follows and still holds.

 Go does not associate
the handles from `os.Pipe` with its I/O completion port, so a read deadline
cannot interrupt a blocked read and `ReadFrame`'s cancellation -- which is how
the supervisor gives up on a worker that stopped answering -- is
unimplementable as written. A named pipe *is* polled, so the contract stays
the same on both platforms. Closing the handle from another goroutine was the
cheaper alternative and was refused: it cannot distinguish "cancelled" from
"the worker died", and every caller would have to.

**3. The daemon sets its own DACL on the socket.** Measured, the socket is
already private -- `SYSTEM`, `Administrators`, the owner -- but only because
it is inherited from a state directory that sits in the user's profile. On
Unix the socket is `0600` wherever it is told to live. Privacy becomes a
property of the daemon rather than of where it was pointed.

**4. The supervisor is a Task Scheduler logon task.** The existing backends
are a systemd *user* unit and a launchd *user* agent: both per-user, neither
needing root. A logon task is the faithful analogue and keeps the installer
unelevated. An SCM service starts at boot and survives logout, which is what
Windows users expect of a daemon -- and it runs under a different identity,
which changes where it looks for its configuration, for a daemon whose whole
state is one user's.

**5. Client integrations open to Windows.** `integrations.NewManager` refused
anything but darwin and linux. Claude Code, Codex and OpenCode all run on
Windows, so the refusal costs real users. What it needs is discovery rather
than design: where each client keeps its configuration there, which is not
`$HOME/.config` for any of them.

**6. The installer installs the Visual C++ redistributable.** `kivgraph.exe`
does not start without it -- `STATUS_DLL_NOT_FOUND`, because `lbug_shared.dll`
is MSVC-built and Server 2022 ships none of its runtime. Carrying the three
DLLs in the bundle would be more consistent with a bundle that carries
everything else, and was refused for one reason: Windows Update services the
redistributable and services no copy of it. A security fix that reaches every
other installation and not ours is not a trade a self-contained bundle wins.

**7. The installer is PowerShell.** `scripts/install.sh` cannot run where
there is no POSIX shell, and the pre-extraction checks it performs -- entry
listing, prefix, member types, symlinks -- are the reason it exists. They are
reimplemented rather than dropped, and the known cost is that two
implementations of one set of checks drift. Tests pin them against each other.

**8. The bundle layout gains a third shape.** ADR 0026's table of what differs
per platform gets a Windows row, and one entry in it is not a value but an
absence: there is no `RUNPATH`. Windows resolves a DLL from the directory of
the executable, so `lbug_shared.dll` sits beside `kivgraph.exe` and the
`lib/` directory does not apply. The archives are zips, because both
`liblbug-windows-x86_64.zip` and `rust-analyzer-x86_64-pc-windows-msvc.zip`
are, so `tools/manifest.json` grows a per-platform `archive_format`.

## Consequences

### What this costs

Decisions 1, 2 and 3 are the ones with teeth. The named pipe touches the
transport between the indexer and the TypeScript worker on every platform's
code path even if only one platform's behaviour changes, and it is the change
most likely to need its own ADR once written. Decision 5 is the largest in
hours and the smallest in risk: it is finding out where five clients keep
their files.

### What it does not promise

`windows/arm64` is out of scope, and by availability rather than by choice:
LadybugDB publishes no asset for it. The installer names the limit rather
than leaving a user to discover it, which is what ADR 0026 asked of the same
question on Intel Macs.

`RunDetached` is unverified on Windows. Its test fixture is a shell script and
porting it means writing every test body twice; the code under test is not
what is unportable. It is named here so that "the suite passes" is not read as
covering it.

### The order

1. ~~`procstat`, and the advice `doctor` gives about a supervisor it cannot
   install.~~ **Done.** `stop` finds its targets, and two things fell out of
   doing it. `procstat.Invocation` compared an observed program against a
   literal `"kivgraph"`, which a process table reporting `kivgraph.exe` never
   matches, so `stop` quietly stopped nothing; `executable.BaseName` is the
   inverse of `executable.Name` and both live in one place now. And there is
   no polite stage: no console control event reaches a daemon with no console
   and no window message reaches one with no window, so `stop` skips straight
   to termination and reports `stop.terminated` rather than the line it prints
   when a process was asked and agreed. An operator who cannot tell those
   apart reads a truncated answer as a bug in the server.
2. The named pipe, because nothing downstream of a worker is trustworthy
   while a stuck one cannot be abandoned.
3. The DACL, the supervisor, the integrations.
4. Distribution, which is the largest piece and depends on all of the above
   only through the supervisor.
