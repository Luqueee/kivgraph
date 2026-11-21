# ADR 0144: Python reads its tests and says what it did not resolve

## Status

Accepted. Changes one configuration default; an existing `config.yaml` keeps
what it says. No change to the payload format.

## Context

Python was measured on real code for the first time (`click`, `100`
declarations per configuration, mypy as the oracle). Three things in that
measurement were defects and not limits:

- **Tests were not read by default.** In `exact` mode recall was `66.1 %`
  without them and `99.5 %` with them, and every file missed without them but
  one was a test. Go, Dart, Java and C# already read theirs (ADR 0132, 0142).
- **The syntactic worker answered `COMPLETE` and was wrong.** In `15` of `75`
  such answers it had missed a non-test file. It reads bare names only: a use
  through a member, `ctx.scope()` or `termui.prompt`, left no edge and no
  unresolved row, so nothing bounded the answer. The exact adapter had the same
  defect once and records an unresolved row for it.
- **A name inside a quoted annotation was not an occurrence** in either worker.
  `def f(ctx: "Context")` uses `Context`; read as a string constant it uses
  nothing. A `ParamSpec` used only that way had no reference at all in `exact`
  mode and was answered `COMPLETE`.

## Decision

1. `python.include_tests` defaults to `true`.
2. The syntactic worker records every member access as an unresolved
   occurrence naming the member. It does not guess what the member is.
3. Both workers read quoted annotations: a string in an annotation position, or
   the first argument of `cast`. The syntactic worker resolves the names inside
   as it resolves bare names; the exact adapter asks Pyright at each name's own
   offset inside the quotes. The strings of `Literal[...]` and the metadata of
   `Annotated[...]` are values and are not read. Only a plain one-line ASCII
   literal is read, so the evidence of each occurrence is where the name is.

`analyzer_mode` stays `fallback`: `exact` needs Pyright installed, which a
default installation does not have.

## Consequences

Measured again on the same sample:

| configuration | file recall | file precision |
|---|---|---|
| default (syntactic, reads tests) | `43.7 %` | `86.2 %` |
| `python.tests_excluded` | `43.7 %` | `86.2 %` |
| `python.exact` | `100.0 %` | `95.9 %` |
| `python.exact.tests_excluded` | `66.7 %` | `95.4 %` |

Answers that said `COMPLETE` and missed a non-test file: `1` of `43` by
default, where it was `15` of `75`; `0` of `87` in `exact` mode, where it was
`1`.

- The syntactic worker is no more complete than it was: `43.7 %`. What changed
  is that it says so. `57` of its `100` answers are now `LOWER_BOUND`, against
  `25`, and each names the occurrences it could not resolve.
- The price is rows: `7768` to `11876` unresolved occurrences on `click`, one
  per member access. A name that is also a member somewhere in the graph gets
  `LOWER_BOUND`, which for a common name such as `get` is nearly always.
- The one `COMPLETE` that still misses is a function defined twice under a
  platform test, an import in one branch and a `def` in the other: the worker
  binds the name to the import and attributes the uses to that.
- Reading tests does not help the syntactic worker on this repository: it does
  not resolve `import click` from `tests/` to `src/click`. It is what takes the
  exact mode from `66.7 %` to `100 %`.
- Pyright does not navigate from the string of a `cast`. The adapter records
  that as an unresolved occurrence, so the answer is bounded and not silent.
- A `config.yaml` written by an earlier `init` spells `include_tests: false`
  out and keeps it; a test pins that for Python as for the others.
- What the workers call a test (a `test`, `tests` or `integration_test`
  directory) is not the rule `exclude_tests` applies (`internal/testpath`).
  Not reconciled here.
- Existing graphs need `kivgraph index --full` to gain the occurrences.

## Amendment: the package by its own name, one rule for a test, exclusions

Three of the things left open above, closed.

**A package is found by the name it is imported as.** The syntactic worker
named `src/click/core.py` as `src.click.core`, so `import click` resolved to
nothing and no test reached the package. A module is now also reachable by its
package name -- the path from the first directory above it that is not a
package -- unless two modules would claim that name. The stable identity of a
declaration is unchanged: only how an import finds it. With it:

- a relative import in an `__init__` is read against the package itself, which
  it was not: `from .core import X` in `click/__init__.py` looked in `src`;
- what a module imports is what it makes available, followed until nothing new
  resolves, so `click.Command` is the class `__init__` re-exports;
- `module.member` is resolved when the module is one the file imported. That is
  reading an import. A member of anything else is still an unresolved
  occurrence.

| configuration | file recall, before | file recall, now | file precision, now |
|---|---|---|---|
| default | `43.7 %` | `82.9 %` | `85.6 %` |
| `python.tests_excluded` | `43.7 %` | `58.2 %` | `88.5 %` |

Reading tests is now worth `24.7` points to the default mode. It still recalls
no use of a method (`0` of `10`): that needs a type, and it says `LOWER_BOUND`.
One `COMPLETE` answer in `60` misses a non-test file, the same one as before.
The exact mode is unchanged at `100.0 %` and `66.7 %`.

**One rule says what a Python test is.** Both workers apply
`internal/testpath`, written in them a second time because they decide what to
read before a graph exists; a test holds the two copies to the same answer over
`380` paths. The rule gains the `integration_test` directory the workers had.
With `python.include_tests: false` a `test_*.py`, a `*_test.py` or a
`conftest.py` outside a test directory is now left out, as `exclude_tests`
already filtered it.

**`exclusions` reach the Python graph.** The producers walk the repository
themselves and were never told. The filter is applied to the payload with the
registry's own matcher, so an external producer is covered and there is no
second grammar. A use that stays of a declaration that goes becomes an
unresolved occurrence, `TARGET_NOT_INDEXED`. The producer still reads the
excluded files; only the graph loses them.

## Amendment: a name bound to two declarations

The one `COMPLETE` left above that missed a file, closed. `click/_compat.py`
imports `_get_windows_console_stream` from `_winconsole` under a platform
test and defines a function of that name in the other branch. The syntactic
worker bound the name to the import, so the three calls in that file went to
the Windows function and the local one was answered as used by nobody.

Which of the two a use means is decided when the program runs. The worker now
calls a name **contested** when one module binds it to more than one
declaration of the repository -- by `from` imports, or by an import and a
declaration of its own -- and resolves no use of it: not in that module, not
in a module that imports the name from it, not through a package that
re-exports it. Each use is an unresolved occurrence, so both declarations
answer `LOWER_BOUND`.

| arm | wrong `COMPLETE`, before | now | recall | precision |
|---|---|---|---|---|
| default | `1` of `60` | `0` of `59` | `82.9 %` | `85.6 %` |
| `python.tests_excluded` | `1` of `71` | `0` of `70` | `58.2 %` | `88.5 %` |

Recall and precision are unchanged: the uses it gives up were attributed to
the wrong declaration, and mypy has no truth for the Windows one here.

Not contested, and unchanged: a name imported from outside the repository and
also declared (`try: from tomllib import loads` / `except: def loads`) goes to
the local declaration, the only one the graph has; the same declaration
imported twice; two declarations of one name and kind in one module, which are
one symbol. Bindings are still read per file and not per scope. The exact mode
had no such miss and is untouched.
