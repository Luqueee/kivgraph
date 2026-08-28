# ADR 0082: an analyzer never builds in the repository

- **Status:** accepted
- **Date:** 2026-08-28
- **Revises:** ADR 0080, ADR 0081

## Context

`AGENTS.md` states it under *Nunca modificar*, with no exception:

> Los repositorios indexados y los artefactos de entrada de los benchmarks.

Every analyzer honoured it until Java and C#. Those two are indexed by
**building** the code: `scip-java` drives Maven, Gradle, sbt or mill, and
`scip-dotnet` runs `dotnet restore` and then Roslyn. A build writes into the
directory it builds. `--targetroot` moves the SemanticDB output out, but
Maven's `target/`, Gradle's `build/` and the SDK's `obj/` and `bin/` belong to
the build tool, and no flag relocates them.

Both ADRs recorded this as an accepted limitation. It is not a limitation of
the format or the analyzer; it is a rule the two languages broke, and the
smoke tests said so in one line each: `?? target/`, `?? obj/`.

## Decision

**The analyzer is never pointed at the repository.** `internal/scratchtree`
materialises the repository's working tree somewhere else, the build runs
there, and the tree is removed afterwards with everything it produced.

Three strategies were measured on this repository -- `1652` tracked files, a
`3.8 GB` working tree once `node_modules`, `.tooling` and `dist` are counted:

| strategy | time | size | files | writes in the repo |
| --- | --- | --- | --- | --- |
| copy of the working tree | `8154 ms` | `4.5 GB` | `55862` | none |
| `git worktree add` | `107 ms` | `16 MB` | `1638` | **`.git/worktrees/`** |
| **`git archive` + overlay** | **`76 ms`** | **`16 MB`** | **`1637`** | **none** |

The archive wins on every axis. It is also the only one of the three that
writes nothing inside the repository at all: `git worktree add` registers
metadata a dead pass leaves behind, which is the rule this ADR is about. The
copy is honest and costs two orders of magnitude more, because it carries every
build output and vendored dependency that happens to be on disk.

**It reproduces the working tree, not `HEAD`.** `git archive HEAD` is followed
by an overlay of everything `git status` reports: a modified file is copied
over, a deleted one is removed, an untracked one is carried. A user editing
code expects the graph to describe what is on disk, and the registry already
records whether a repository is dirty; indexing `HEAD` would answer about code
nobody has.

A repository git cannot describe -- not a repository, or one with no commit --
falls back to a copy with the same exclusions. The tree is identical either
way; only the cost differs.

## Consequences

- `internal/javaloader` and `internal/csharploader` run the indexer in the
  scratch tree. Both keep pointing `--targetroot` and `--output` outside it,
  because the index has to outlive the tree.
- The tar stream is read in process rather than piped to a `tar` binary:
  Windows is a published platform and does not reliably have one.
- The archive is third-party content -- it is `git archive` over a registered
  repository -- and three escapes are refused, not one. A traversal name is
  the obvious shape. A symlink with an **absolute** target is the one that got
  through: the first version joined the link target onto the entry's
  directory to test it, and `filepath.Join("a", "/etc")` is `"a/etc"`, which
  passes every containment check. And a write **through** a symlinked parent
  has a clean name, so no test on the name can see it; the parent is resolved
  with `EvalSymlinks` before anything is written. And nothing creates a
  symlink at all: a link that stays inside the tree is materialised as a copy
  of its content, so the class cannot occur rather than being defended
  against. A link is legitimate content -- this repository's `CLAUDE.md` is
  one -- and a build reads the bytes behind a path, so the only thing that
  costs is a build that inspects link-ness.
- `TestRunLeavesTheRepositoryUntouched` indexes a fixture **in place** and
  compares the tree before and after. Every other end-to-end test copies first,
  which is what a test may do and also what hid the defect.

## The defect this introduced, and the test that holds it down

The scratch tree is a fresh temporary directory on every pass. The Java loader
derived the package name from the directory it read the sources from, and the
package name reaches **every stable key**. Left alone, the same unchanged code
would have been published under a different identity on each pass -- a graph
that looks correct and that nothing downstream compares against the previous
one.

So a loader now carries two roots and they are not interchangeable: `sources`
is where files are read, `repository` is what the facts are about.
`TestRunKeepsOneIdentityAcrossPasses` indexes twice and compares the derived
keys.

## Limitations

- The exclusion list is by directory name -- `target`, `obj`, `bin`, `build`,
  `node_modules` and the rest. A repository whose *source* lives in a directory
  with one of those names loses it. That is a real trade and the alternative is
  worse: carrying them costs the `4.5 GB` in the table above and lets an
  analyzer index its own previous output.
- Submodules are not materialised: `git archive` does not descend into them.
- The measurement is one repository on one machine. The shape of the result --
  archive beats copy by two orders of magnitude because it materialises tracked
  files only -- is what generalises, not the milliseconds.
