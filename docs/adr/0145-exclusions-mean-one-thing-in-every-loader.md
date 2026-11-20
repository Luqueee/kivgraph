# ADR 0145: `exclusions` mean one thing in every loader

## Status

Accepted. Changes what an `exclusions` pattern matches for Dart, and makes the
key effective for Java, C# and Python. No change to the registry's format.

## Context

`exclusions` is a key of a repository in `repositories.yaml`. It names what is
left out of the graph, and the repository is indexed in every language it
declares. `workspace.MatchesExclusion` is the matcher: segment-aware, relative
to the repository root, reading `**`, `./x` and absolute paths inside the
repository. Go, TypeScript and Rust ask it.

The other four loaders were checked one by one:

- **Python** did not read the key. Fixed in the amendment to ADR 0144, on the
  payload.
- **Java and C#** did not read the key. Their indexers build the whole project,
  and every document they wrote entered the graph.
- **Dart** read it with a matcher of its own. Compared with the registry's over
  the same patterns and paths, it disagreed in thirteen of sixty cases, in both
  directions: a bare `gen` excluded a `gen` directory at any depth, `gen/**` and
  `*.g.dart` matched below the root, and `./gen`, an absolute path and
  `lib/**/gen` excluded nothing.
- **Rust** already asked the registry's matcher.

One list therefore excluded different files for Dart than for the Go and
TypeScript files of the same repository, and nothing at all for three languages.

## Decision

Every loader asks the registry's matcher. `workspace.ExclusionMatcher` validates
the patterns once and answers for a path relative to the repository, so a loader
that decides file by file does not carry an error through each one.

- Dart's own matcher is removed.
- Java and C# leave an excluded document out where they already leave out a test
  or a generated file, so a use of a declaration that went is handled as a use of
  a test's declaration is when tests are not read.

## Consequences

- **A Dart repository can index more than it did.** A bare name in its
  `exclusions` used to match at any depth and now matches at the root, as it
  does for every other language. `**/gen` is how any depth is written. The
  patterns Dart ignored -- `./gen`, an absolute path, `lib/**/gen` -- now
  exclude what they say.
- A Java or C# repository with `exclusions` indexes fewer files than it did.
- The Java, C# and Python tools still build or read the excluded files; the
  graph loses them afterwards. Only the loaders that walk the repository
  themselves skip the work.
- A pattern the matcher refuses is an error for these loaders, where Dart read
  it as matching nothing.
- Existing graphs need `kivgraph index --full`.
