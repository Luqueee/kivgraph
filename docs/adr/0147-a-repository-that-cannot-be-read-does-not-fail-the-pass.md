# ADR 0147: A repository that cannot be read does not fail the pass

## Status

Accepted. Changes when `kivgraph index` fails; adds one unresolved reason and
one line to its report. No schema or stable key changes.

## Context

A pass indexes every registered repository in every language it declares, and
publishes one generation. When an analyzer cannot read one of them there are two
things a pass can do, and the languages did not agree:

- **Go and Rust** record a scope-level failure -- `MODULE_NOT_LOADED`,
  `WORKSPACE_NOT_LOADED` -- with no file, publish the rest, and every answer
  about that repository says `LOWER_BOUND`.
- **Python, Dart, Java and C#** returned the error, and the pass ended without
  publishing.

Java and C# are indexed by building them, which is the most ways a repository
has of not being readable: no solution, a build that fails, an indexer that
writes no index. With a C# repository that has no solution registered beside
`cobra`:

| | exit | `cobra` |
|---|---|---|
| before | `1`, `index.full: FAIL`, no generation | not indexed |
| now | `0`, generation published | `1230` definitions |

Nothing about `cobra` had changed. The same held for a repository given a
language it has no project for by `init --languages`, which registers every
language for every repository it is passed.

A missing analyzer was already isolated (`analyzerNotInstalled`): that is a fact
about the machine. This is the other case: the analyzer is installed, ran, and
failed on this repository.

## Decision

A semantic loader's failure belongs to its repository. The repository publishes
a scope-level unresolved row with reason `REPOSITORY_NOT_LOADED`, its language,
and a bounded one-line detail; it is counted in `not_loaded`; and the report
names it:

```text
index.csharp.not_read: nosln: REPOSITORY_NOT_LOADED: csharp indexer: no .sln ...
```

Three things stay errors:

- **A cancelled pass.** It learned nothing about the repository it was reading.
- **A payload that does not normalise.** That is a producer breaking the
  contract or a defect here, and publishing around it would hide it.
- **A TypeScript package.** Its worker is one process for the whole pass and is
  not changed here.

## Consequences

- One repository that cannot be read no longer decides whether the others have a
  graph.
- **A repository that was indexed and stops building loses its facts in the next
  generation**, where before the pass failed and the previous generation went on
  being served. The generation says so -- `LOWER_BOUND`, with the reason -- and
  the report prints the line, but a caller that only reads the exit code sees
  success. This is the trade Go and Rust already made.
- A failure to read is never stored in the fact cache, so indexing again after
  fixing the repository reads it.
- The detail is cut at `600` bytes: the loaders quote the tail of a build log.
- The C# loader's "no solution" message names the repository and not the scratch
  copy it built in.

## Amendment: TypeScript

The third item above was wrong about the worker -- it runs once per package,
not once per pass -- and TypeScript had three ways of ending a pass. Indexed
beside `cobra`, with the real worker:

| shape | before | now |
|---|---|---|
| a `tsconfig.json` that does not parse | exit `1` | `REPOSITORY_NOT_LOADED` |
| the only TypeScript repository has no project | exit `1` | `no_package` line |
| the worker fails on a package | exit `1` | `PACKAGE_NOT_LOADED` |

- **Discovery.** Kivgraph reads every project of a repository before any
  worker starts, to know what its packages are, and a project that does not
  parse failed there. The repository is now declared `REPOSITORY_NOT_LOADED`;
  it is the whole repository because the project graph is what could not be
  read.
- **No project anywhere.** A pass refused to run when no TypeScript repository
  had a named package with a project. `init --languages go,typescript`
  registers both languages for every repository it is given, so a Go
  repository was enough. Such a repository was already named as contributing
  nothing when another one had packages; it is now named in every case.
- **The worker.** A failure of the worker process on one package is declared
  `PACKAGE_NOT_LOADED`, with the package as the scope.

`index.typescript` gains `not_loaded`, and `index.typescript.not_read` names
each one.

Still errors: a cancelled pass, a worker command that cannot be prepared, an
answer that cannot be read or does not normalise, and **a worker that is not
there**. That last one is not the analyzer of another language that a machine
may not have: it ships with Kivgraph, and a pass that counted every package as
unread and exited `0` would hide an installation that cannot index TypeScript
at all.

Not measured with the real worker: the third row. The worker tolerated every
broken configuration it was given, so that path is exercised with a stand-in
that exits non-zero.

## Amendment: two Cargo workspaces that declare the same crate

One more way a single repository ended the pass. A Rust symbol is named by its
repository, its crate and its path, so two workspaces of one repository that
declare a crate of the same name -- a vendored copy, an example, a fixture --
give two declarations one identity, and the published set did not validate.

The first workspace by path is indexed. The other is left out whole and
declared with a scope-level `AMBIGUOUS_CRATE_PROVIDER`, the reason two
repositories declaring one crate already have, and an `index.rust.diagnostic`
line naming the workspace, the crate and the workspace that keeps it.

Not done: telling the two apart. The identity has no field for the workspace,
and adding one changes every stable key. Leaving out only the shared crate
instead of the workspace would keep more, and is not done either.

### A use that crosses two workspaces of one repository

Two workspaces that do not share a crate are both indexed, each alone. A use
in one of a crate the other declares -- an application depending by path on
the workspace of its libraries -- found the crate in its own repository and no
declaration in its own index, and was declared `DEFINITION_NOT_INDEXED`: no
edge, to a declaration the same pass publishes.

It is resolved now the way a use that crosses repositories is. The key is
composed from the identity the analyzer gave, the edge is `EXACT` with the
provenance of a moniker, and the merge keeps it only if the other workspace
publishes that key; otherwise it is `PROVIDER_DEFINITION_NOT_INDEXED`.
`DEFINITION_NOT_INDEXED` stays for a crate of the workspace being analysed.

Checked on a repository with a fuzzing workspace beside its main one: a
function of a library crate gained the fuzz target that calls it. Not measured
on the benchmark corpus, which has no such repository.

Seen there and open: a Rust file that declares nothing -- its whole body is
one macro invocation -- has no symbol to be the source of a use, so its uses
are in no edge and no failure.

## Amendment: an entry that cannot be registered

The same failure one step earlier. Before any loader runs, `index --full`
builds the registry, and one entry that could not be registered failed it: a
directory that was deleted, one that is not a Git checkout, a path with a
symlink in it. Every other repository lost its graph to it.

Such an entry is now left out and named on an
`index.repository.not_read: <name>: <reason>` line -- on `stderr` with
`--json`, whose `stdout` is a protocol. The pass indexes the rest and exits
`0`. A registry where nothing registers is still an error, and so is what
involves two entries: a duplicate name or path.

Unlike the cases above there is no row in the graph: the repository is not in
it at all, and a question about it answers `REPOSITORY_NOT_FOUND`. The same
trade-off holds, and is sharper here: a repository on a disk that is not
mounted leaves the next generation, with exit `0`.

`init` also writes the path of a directory that is there as the directory it
is -- symlinks resolved, a relative path taken from where the command ran --
because the registry refuses the first and read the second against the
configuration's directory. `/tmp` is a symlink on macOS.

Only `index --full` does this. `serve`, `doctor` and the other readers of the
registry still fail on such an entry.
