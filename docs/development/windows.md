# Windows support: what it would take

`windows/amd64` is not a supported platform and nothing here ships it. This
document is what was learned trying, so that whoever decides to finish knows
what is left and what it is made of.

It has been rewritten once. The first version answered the question by
cross-compiling, concluded that the port was small, and said in as many words
that a real host had to confirm it. A real host did, and disagreed with the
optimism while confirming the shape: the code compiled clean and then failed
184 of its own tests. Everything below is measured on that host.

Thirty-one of those failures are left, and what they are made of matters more
than the count -- roughly half are tests exercising features the product
refuses on Windows on purpose. The section on what is left says which.

## What this rests on

A Windows Server 2022 virtual machine -- Go 1.26.6, mingw-w64 GCC 16.2, the
pinned LadybugDB Windows library -- running the repository's own suite, five
times, with the fixes between the runs.

|run|what changed before it|packages ok|tests failed|
|---|---|---|---|
|1|nothing; the tree as it was|25|184|
|2|`fsync`|25|146|
|3|`filesystemCapacity`, `go.work` paths|28|61|
|4|program names, `HOME`, path order|31|38|
|5|manifest containment, `doctor`|32|33|
|6|assertions a platform cannot answer|34|31|

Before any of that, the compile results the first version of this document
reported still hold, and one of them was strengthened: the tree builds for
`windows/amd64` with mingw-w64 as well as with Zig, so the CI job's toolchain
is the one that was verified, and `go build`, `go vet` and `staticcheck` are
all clean for that GOOS.

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

## What the last thirty-one are

|cause|count|
|---|---|
|client integrations, refused by `NewManager` on purpose|13|
|the host's Python, not the port|3|
|`update`, refused because no Windows release exists|2|
|`procstat` and signal-0 liveness, unimplemented|2|
|deleting a file something still holds open|2|
|everything else, one apiece|9|

The first two rows are not defects and the third follows from a decision
nobody has made yet, so a little over half of what is left is waiting on
somebody to say what Windows should be promised rather than on somebody to
write something.

## What is still open

### One thing needs a decision, not a primitive

`internal/tsworker` deadlocks. `TestReadFrameHonoursDeadlineAndCancellationOnAPipe`
blocks in `syscall.readFile` and never returns; the package costs whatever the
test timeout is set to. The name states the cause: Go on Windows does not
associate the handles from `os.Pipe` with its I/O completion port, so a read
deadline cannot interrupt a blocked read there.

This is not a test defect. `ReadFrame`'s cancellation is what the supervisor
uses to give up on a worker that has stopped answering, so the contract itself
is unimplementable on Windows as written. The options are a named pipe, which
Windows *does* poll, or closing the handle from another goroutine, which is
cruder and changes what a caller can retry. Both are design changes and belong
in an ADR.

### Two things have never been exercised

- **The socket ACL.** `withPrivateUmask` is a documented no-op off Unix, so
  the daemon's socket inherits its directory's ACL and the comment beside it --
  "the socket carries the whole graph, so it is the owner's alone" -- stops
  being true. No run has reached it: the daemon has not started on Windows.
- **`RunDetached`.** Its test fixture is a `#!/bin/sh` script, and Windows has
  no interpreter for a `#!` line. The file is Unix-only now, so the code under
  test is unverified there. What is unportable is the fixture, not the code.

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

## The daemon still has no owner

`internal/supervisor` writes a systemd user unit on Linux and a launchd plist
on macOS, and ADR 0068 makes that ownership the reason a client may be
registered against a daemon at all. On Windows it answers `unsupported`, which
is honest and is not support. A third backend -- a Task Scheduler logon task,
which needs no privileges, or a real service through the SCM, which needs an
elevated installer -- is a decision about the installer as much as about the
daemon.

## Distribution

Unchanged from the first version of this document, and still the larger half
of the remaining work.

|file|what it assumes|
|---|---|
|`scripts/build-bundle.sh`|`uname`; `linux/amd64` or `darwin/arm64` only|
|`scripts/install.sh`|`uname`, `tar`, POSIX shell|
|`scripts/fetch-ladybug.sh`|`.tar.gz`; `liblbug.so` / `liblbug.dylib`|
|`scripts/fetch-rust-analyzer.sh`|`uname`; `.gz`|
|`tools/manifest.json`|one `archive_format` for every platform|
|`.github/workflows/release.yml`|two runners, `.tar.gz` assets|

Four differences are structural rather than cosmetic:

- **The archives are zips.** Both `liblbug-windows-x86_64.zip` and
  `rust-analyzer-x86_64-pc-windows-msvc.zip` are zips where every other
  platform ships a tarball, and `tools/manifest.json` has one
  `archive_format` per tool rather than per platform.
- **The library name changes.** `lbug_shared.dll` with an import library.
- **There is no `RUNPATH`.** ADR 0026's per-platform table -- `$ORIGIN/../lib`
  against `@loader_path/../lib` -- has no third row, because Windows resolves
  a DLL from the directory of the executable. `lbug_shared.dll` sits beside
  `kivgraph.exe`, and the `lib/` directory of the bundle layout does not
  apply. That is a change to the bundle contract, not a flag.
- **The installer is a different language.** A PowerShell installer is a
  second implementation of the same pre-extraction security checks, and
  duplicated checks drift. Whether that is acceptable is a product decision
  and belongs in an ADR.

## What is left, in order

1. **Finish the long tail,** knowing that most of what is left is not a
   defect. Roughly a third of the remaining failures are tests exercising
   client integrations, which `integrations.NewManager` refuses outright on
   anything but darwin and linux, and updates, which are refused because no
   Windows release exists to update to. Both refusals are correct today and
   both are product decisions, so those tests cannot be fixed -- only decided.

   The one mechanical piece left is `internal/procstat`, which reports
   `ErrProcessListUnsupported` and takes `kivgraph stop` and part of `doctor`
   with it. Windows has the primitive, `CreateToolhelp32Snapshot`, already
   bound in `golang.org/x/sys/windows`. It is not quite the free win the
   others were: Toolhelp yields a process's image name and not its argv, and
   `procstat.Process` carries `Args` because callers use it to tell a `serve`
   from an `index`. Whether an image name is enough to stop the right process
   is a question worth answering before writing it.
2. **Decide the framing cancellation.** The pipe deadlock, in an ADR. Nothing
   downstream is trustworthy while a stuck worker cannot be given up on.
3. **Decide the socket, and start the daemon.** Explicit DACL or named pipe,
   and then a run that actually reaches it.
4. **Decide the supervisor,** which decides the installer.
5. **Then distribution,** and an ADR recording what `windows/amd64` support
   does and does not include.

Steps 2 through 4 are three decisions, and every one of them is about what
Windows should be promised rather than about how to write it.

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
