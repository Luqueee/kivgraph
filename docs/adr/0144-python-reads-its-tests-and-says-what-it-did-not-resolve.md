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
