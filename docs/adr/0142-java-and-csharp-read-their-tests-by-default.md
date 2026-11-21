# ADR 0142: Java and C# read their tests by default

## Status

Accepted. Changes two configuration defaults; an existing `config.yaml` keeps
what it says.

## Context

ADR 0132 made Go and Dart read their tests by default and left Java, C# and
Python as they were, because they had not been measured. Java and C# now are
(ADR 0141). On `jsoup`, `100` sampled declarations:

| `java.include_tests` | file recall | file precision |
|---|---|---|
| `false` (the default until now) | `61.1 %` | `96.6 %` |
| `true` | `100.0 %` | `96.8 %` |

Every file the default missed is a test. A declaration that only its tests use
was answered as used by nobody, and `kivgraph impact` could not list the tests a
change reaches. That is the case ADR 0132 was written for.

The cost, measured on the same repositories with one pass each:

- `jsoup`: symbols `3528` to `6673`, references `17674` to `47305`, index time
  `67 s` to `57 s`, peak footprint `0.43` to `0.48 GB`, generation `542` to
  `564 MB`.
- `GuardClauses`: symbols `909` to `916`, references `2121` to `2136`, index
  time `38 s` to `34 s`, peak footprint `0.38` to `0.35 GB`, generation `531`
  to `532 MB`.

The time is the build the indexer drives, and it did not grow: one pass each,
so the two times are within the spread of a run.

## Decision

`java.include_tests` and `csharp.include_tests` default to `true`. Python is
unchanged: it has no oracle independent of its own analyzer and is still not
measured.

Nothing else is needed. `exclude_tests` on the traversal tools already
recognises a Java and a C# test by path (`src/test/`, a `test` or `tests`
directory, a `*Test` or `*Tests` file name), so production-only answers stay one
argument away.

## Consequences

- A `config.yaml` written by an earlier `kivgraph init` spells the key out as
  `false` and keeps it. Only a new installation, or a deleted key, gets the new
  default. A test pins that an explicit `false` is still read as `false`.
- The next `kivgraph index --full` of a configuration that reads tests publishes
  a larger graph for a Java repository: about twice the symbols on `jsoup`.
- C# gains little from the setting on the measured repository. The loader
  treats as a test a file under `tests/` or named `*Test.cs` or `*Tests.cs`,
  and `GuardClauses` keeps its tests under `test/GuardClauses.UnitTests/` with
  other names, so they were already read: `7` symbols differ. Its recall is
  `49.1 %` with the setting and `47.4 %` without, and the cause is the indexer's
  handling of overloads (ADR 0141), which this does not touch.
- The loader's rule for a C# test and the rule `exclude_tests` uses are not the
  same one. `exclude_tests` drops the files of `test/GuardClauses.UnitTests/`
  although the loader does not call them tests. Not reconciled here.
- The benchmark's second configuration for Java and C# is now
  `java.tests_excluded` and `csharp.tests_excluded`, as for Go and Dart.

## Amendment: one rule says what a C# test is

The two rules named above are now one. `internal/testpath` holds it, the C#
loader applies it when `csharp.include_tests` is `false`, and the tools apply
it under `exclude_tests`. A file can no longer be in the graph as production
code and be filtered from an answer as a test.

For C# the rule is wider than the loader's was: it adds a `test`, `spec`,
`specs` or `__tests__` directory to `tests/` and to the `*Test.cs` and
`*Tests.cs` names. So a configuration that sets `csharp.include_tests: false`
indexes fewer files than before on a repository laid out like `GuardClauses`,
which is what the key says it does. The default configuration reads tests and
is not affected.

Measured again on `GuardClauses`, `53` declarations:

| configuration | file recall | file precision |
|---|---|---|
| default | `49.1 %` | `54.9 %` |
| `csharp.tests_excluded`, before | `47.4 %` | `54.0 %` |
| `csharp.tests_excluded`, now | `14.0 %` | `72.7 %` |

The excluded arm now leaves the tests out, as the same arm does for the other
languages. The Java loader keeps its own narrower rule (`src/test/`,
`src/it/`) and is not changed here.

## Second amendment: the Java loader applies it too

The Java loader kept `src/test/` and `src/it/` as its own rule. It now calls
`internal/testpath` as well, and the rule changed in two ways to take it in:

- `src/it/`, Maven's integration tests, is a test directory. `it` alone is not:
  it is also a locale.
- The class-name convention reads the capital. `ServiceTest.java` and
  `ServiceTests.cs` are tests; `Latest.java` and `Contest.cs`, which the
  case-folding comparison took for tests, are not.

So `exclude_tests` no longer drops a production class whose name happens to end
in "test", and drops the files under `src/it/`. With
`java.include_tests: false` a `*Test.java` outside `src/test/` and a file under
a `test` or `tests` directory are left out, which they were not. `jsoup` keeps
all its tests under `src/test/`: `java.tests_excluded` is `61.1 %` recall and
`96.6 %` precision before and after, and every other figure of the Java and C#
results is unchanged.
