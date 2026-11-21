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
