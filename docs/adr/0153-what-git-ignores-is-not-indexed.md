# ADR 0153: What Git ignores is not indexed

## Status

Accepted. Changes what a graph holds: the next pass of a repository with
ignored sources publishes fewer files. Adds an optional key to an entry of
`repositories.yaml`. Changes no schema, stable key or tool.

## Context

A repository ignores what a build writes. Some of it is source: the route
types React Router generates under `.react-router/`, the `.next/types` of a
Next application, a `build/` that is a copy of `src/`.

Those files were indexed. A TypeScript project includes its generated types in
`tsconfig.json` -- it must, to type-check -- and the engine loads whatever the
project includes. On one application `58` of `495` indexed files were
generated or built: `3244` declarations, each a second declaration of a name
the repository writes once, which is what makes a bare name ambiguous. A pass
after a build changed the graph and no source had been edited.

Two things were wrong, and the second hid behind the first.

- Nothing read `.gitignore`.
- An `exclusions` entry did not keep a TypeScript file out. It kept a package
  from being discovered; a file of a project that was discovered was published
  whatever the registry said.

## Decision

1. **A path Git ignores that holds source is an exclusion.** When a repository
   is registered, `git ls-files --others --ignored --exclude-standard
   --directory` is asked, and every path it names that is, or holds, a file of
   a registered language is added to the repository's exclusions. Every
   loader, the watcher and the freshness check already read those.
2. **A TypeScript file an exclusion names is retired.** After the worker
   answers, a file the exclusions match is dropped with the declarations, uses
   and failures it holds, the way a file outside the repository already is.
   A use of such a declaration from a file that stays is dropped with it.
3. **`include_git_ignored: true`** on the entry indexes them as before, for a
   repository whose generated code is ignored and wanted.
4. **`kivgraph doctor` names them**, on `walk.ignored`.

A path that holds no source is not listed. The exclusions are part of what a
pass compares to know that the sources moved, and a log directory that comes
and goes would make every pass stale. A directory is read until a source file
is found, or `4000` entries; a larger one is taken to hold some.

Not listed either: a path with `node_modules` or `.git` in it, which no walk
enters, and a name with a pattern character in it, which an exclusion cannot
spell.

## Rejected

- **Marking the files generated and keeping them.** The copies would still
  make every name ambiguous.
- **Reading `.gitignore` in each walk.** Git's rules are Git's: nested files,
  negations, the global file. Git is asked.
- **A key in the configuration instead of the registry.** What a repository
  generates is a property of that repository.

## Consequences

- The first pass after an upgrade is a full one for a repository with ignored
  sources: its exclusions changed.
- A build that creates a new ignored directory of sources makes the next pass
  stale once.
- A use of a generated declaration is in no edge and no failure. The
  declaration is not in the graph to be asked about.
- Generated code that is ignored and that the rest of the code depends on --
  a generated client -- leaves the graph until the key is set.
- `index.typescript: symbols=` still counts what the worker answered;
  `stage.facts` counts what was published.

## Verification

On the application above, indexed with and without the key: `495` files and
`40595` symbols with it, `437` and `37351` without; `.react-router/` and
`build/` on the `walk.ignored` line. Unit tests cover the listing against a
real Git repository, and the retirement of a TypeScript file with a use in each
direction and a failure.

Not measured: the benchmark corpus, whose repositories ignore no source. Go,
Rust and the other loaders were not checked on a repository that ignores
generated code of theirs.
