# Windows support: what it would take

Nothing here ships. This is the investigation behind the question "how much
work is `windows/amd64`", measured rather than estimated, so that whoever
decides to take it on knows what they are agreeing to.

The headline is that the answer is much smaller than the shape of the
repository suggests. A complete `windows/amd64` binary, with the native
LadybugDB storage linked and not the pure-Go stub, cross-compiles today after
adding **two** missing build-tag fallbacks. The cost of Windows is not in the
Go code. It is in the distribution, in the daemon's owner, and in three
subsystems that compile and then answer "unsupported".

## How this was measured

There is no Windows host and no MSVC here, so the C compiler was Zig, which
ships a complete `x86_64-windows-gnu` cross toolchain in one archive:

```bash
CC='zig cc -target x86_64-windows-gnu'
GOOS=windows GOARCH=amd64 CGO_ENABLED=1 go build ./...
```

The native library is the Windows asset of the pinned LadybugDB release,
`liblbug-windows-x86_64.zip` for `v0.13.1`, which unpacks to
`lbug_shared.dll`, `lbug_shared.lib`, `lbug.h` and `lbug.hpp`.

```bash
CGO_CFLAGS="-I$LIB" CGO_LDFLAGS="-L$LIB" \
go build -tags ladybug -o kivgraph.exe ./cmd/kivgraph
```

That produced a `PE32+` executable whose import table names
`lbug_shared.dll`. The whole native path links; it was not stubbed out.

This is a compile-and-link result, not a run. Nothing below has been executed
on Windows, and the section on what compiles but does not work is exactly the
part that a real host has to confirm.

## What already works, unchanged

- **The pinned binding is already ported.** `go-ladybug` v0.13.1 declares
  `#cgo windows LDFLAGS: -L${SRCDIR}/lib/dynamic/windows -llbug_shared`. No
  patch, no fork, no vendored shim.
- **The upstream publishes the library.** LadybugDB `v0.13.1` ships a Windows
  asset alongside the Linux and macOS ones.
- **The tree-sitter grammars build.** They are portable C compiled through
  cgo, and they compiled for `windows/amd64` without a change.
- **The platform split is nearly complete already.** Every package that
  reaches for a Unix facility -- `supervisor`, `procstat`, `daemon`'s umask,
  `rebuild`'s mmap, `tsworker`'s process group, `generation`'s filesystem
  capacity -- already carries a `!unix` or `!linux && !darwin` fallback,
  written when macOS was added and when the distribution targets were named.
  The two gaps below are the exception, not the rule.

## What fails to compile

Two files in the shipped tree, and that is the entire list:

|package|missing|
|---|---|
|`internal/storage/generation`|`acquirePublishLock`, `publishLock`|
|`internal/indexing`|`acquireWriterLock`, `writerLock`|

Both are `flock` advisory locks in `*_unix.go` files with no counterpart, and
both are called from files with no build tag, so the package does not build.

Outside the shipped tree:

- `benchmarks/snapshot-heap` uses `syscall.Mmap` directly. It is a benchmark,
  not a distributed artifact, and it can simply stay off Windows.
- Three test packages use calls that do not exist on Windows:
  `internal/tsworker` and `internal/resilience` use `syscall.Kill`,
  `internal/daemon` uses `syscall.Umask`.

## What compiles and then does not work

This is the real content of the port. Each row compiles cleanly and would
mislead anyone who read the build as a verdict.

|subsystem|behaviour on Windows|
|---|---|
|writer and publish locks|the two files above|
|`internal/supervisor`|`unsupported`; the daemon has no owner|
|`internal/procstat`|`ErrProcessListUnsupported`|
|`withPrivateUmask`|no-op; the socket keeps the default ACL|
|`internal/rebuild` mmap|falls back to reading the whole file|
|`internal/watcher`|the `EMFILE`/`ENFILE` branch is unreachable|

Four of them deserve more than a row.

### The locks must be real, not stubs

The obvious fix for the two missing files is a stub that returns an error, and
it is the wrong one. The writer lock is what stops two Kivgraph processes over
the same state directory from rebuilding the same graph at once, and the
publish lock is what makes a leftover candidate directory safe to treat as
debris. A stub that fails turns `index` into a command that cannot run; a stub
that succeeds unconditionally turns concurrent indexing into corruption that
appears only under load.

Windows has the primitive: `LockFileEx` with `LOCKFILE_EXCLUSIVE_LOCK |
LOCKFILE_FAIL_IMMEDIATELY`, through `golang.org/x/sys/windows`, which the
module tree already carries. It gives the same non-blocking exclusive
semantics and the same release-on-process-death that the comments in both Unix
files rely on. So this is an implementation, not a fallback, and it is the one
piece of the port that cannot be deferred.

### The daemon would have no owner

`internal/supervisor` writes a systemd user unit on Linux and a launchd plist
on macOS, and ADR 0068 makes that ownership the reason a client may be
registered against a daemon at all. On Windows the honest answer today is
`unsupported`, which the package already returns.

Giving Windows an owner means a third backend: a Task Scheduler logon task is
the cheap version and needs no privileges; a real service via the SCM is the
faithful one and needs an installer running elevated. Until one exists,
Windows gets a daemon that nothing restarts, and `kivgraph doctor` should say
so rather than let the absence read as health.

### The socket is a security decision, not a portability one

`internal/daemon` listens on `AF_UNIX`, which Windows 10 1803 and later
support, so `net.Listen("unix", path)` works. What does not carry over is the
protection around it: `withPrivateUmask` is a documented no-op off Unix, and
the comment beside it -- "the socket carries the whole graph, so it is the
owner's alone" -- stops being true. The socket file inherits the default ACL
of its directory.

So a Windows daemon needs either an explicit DACL on the socket's directory or
a named pipe with a security descriptor. Shipping the `AF_UNIX` path as-is
would publish the entire graph to any local account, quietly, on a platform
where the code still claims otherwise.

### Windows will not delete or rename an open file

`internal/storage/generation` publishes by `os.Rename` of a candidate
directory and cleans up with `os.RemoveAll`. On Unix an open database file
does not obstruct either. On Windows a file held open by any handle cannot be
renamed or deleted, and the failure surfaces as `ERROR_SHARING_VIOLATION` from
the middle of a publish rather than at the point the handle was taken.

This is the item most likely to pass a test suite on a quiet machine and fail
on a real one, because it needs a *concurrent reader* to reproduce: the
publisher renaming a generation while a follower still has its `.lbdb` mapped.
It is worth reproducing deliberately before believing the port.

## Distribution

None of this exists yet, and it is the larger half of the work.

|file|what it assumes|
|---|---|
|`scripts/build-bundle.sh`|`uname`; `linux/amd64` or `darwin/arm64` only|
|`scripts/install.sh`|`uname`, `tar`, POSIX shell|
|`scripts/fetch-ladybug.sh`|`.tar.gz`; `liblbug.so` / `liblbug.dylib`|
|`scripts/fetch-rust-analyzer.sh`|`uname`; `.gz`|
|`tools/manifest.json`|one `archive_format` for every platform|
|`.github/workflows/release.yml`|two runners, `.tar.gz` assets|
|`.github/workflows/ci.yml`|`ubuntu-latest` and `macos-15`|

Four differences are structural rather than cosmetic:

- **The archives are zips.** Both `liblbug-windows-x86_64.zip` and
  `rust-analyzer-x86_64-pc-windows-msvc.zip` are zips where every other
  platform ships a tarball. `tools/manifest.json` has a single
  `archive_format` field per tool, so it needs a per-platform one.
- **The library name changes.** `lbug_shared.dll` with an import library, not
  `liblbug.so` or `liblbug.dylib`, so the fetch script's naming and the
  bundle layout both need a Windows branch.
- **There is no `RUNPATH`.** ADR 0026's per-platform table -- `$ORIGIN/../lib`
  against `@loader_path/../lib` -- has no third row. Windows resolves a DLL
  from the directory of the executable, so `lbug_shared.dll` must sit beside
  `kivgraph.exe`, and the `lib/` directory of the bundle layout does not
  apply. That is a change to the bundle contract, not a flag.
- **The installer is a different language.** `scripts/install.sh` cannot run
  where there is no POSIX shell. A PowerShell installer is a second
  implementation of the same security checks -- entry listing, prefix, member
  types, symlinks -- and duplicated checks drift. Whether that is acceptable
  is a product decision and belongs in an ADR, not in a script.

## Two things that are already handled

- **Path casing.** ADR 0027 settled how the engine treats a filesystem that
  folds case, for macOS. Windows raises the same question and inherits the
  same answer.
- **Symlinks.** The tree reads and *skips* symlinks in a dozen walkers, which
  is portable. Exactly one place creates one, `internal/integrations`
  linking the installed skill, and `os.Symlink` on Windows needs Developer
  Mode or an elevated process. A copy is the obvious fallback there.

## A staged plan

The stages are ordered so that each one is worth landing on its own, and so
that no stage claims support the one below it has not earned.

1. **Make it compile.** The two lock files, as real `LockFileEx`
   implementations. Guard the three test packages and `benchmarks/snapshot-heap`
   behind build tags. Add a `windows/amd64` cross-compile job to `ci.yml` --
   cheap, and it stops the next Unix-only call from landing unnoticed.
2. **Make it honest.** `doctor` and `status` name the absent supervisor and
   the unreadable process table instead of returning a shape that reads as
   healthy. This is ADR 0031's rule applied to a new platform.
3. **Make it safe.** The socket ACL or the named pipe, and the open-handle
   behaviour of the generation store reproduced with a concurrent reader on a
   real Windows host. Nothing should be published before this stage passes.
4. **Make it installable.** The manifest's per-platform archive format, the
   zip and DLL-beside-the-binary bundle layout, the PowerShell installer, the
   `windows-2022` runner in the release matrix, and an ADR that records what
   `windows/amd64` support does and does not include.

Stage 1 is small and mechanical. Stage 3 is the one that decides whether this
is a supported platform or a build that happens to succeed.

## Open questions

- **Does the supervisor get a Task Scheduler task or a real service?** It
  changes whether the installer needs elevation, and that changes the
  installer.
- **Is `AF_UNIX` with an explicit DACL enough, or does the daemon need a named
  pipe on Windows?** The second is more faithful to the platform and is a
  second transport to maintain.
- **Is `windows/arm64` in scope?** LadybugDB does not publish an asset for it
  today, so the answer is currently no by availability rather than by choice
  -- which is the kind of limit ADR 0026 says to name rather than leave the
  user to discover.
