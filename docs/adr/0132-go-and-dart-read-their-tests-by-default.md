# ADR 0132: Go and Dart read their tests by default, and find_references filters

## Status

Accepted. Changes two configuration defaults and adds an argument to an MCP
tool.

## Context

`go.include_tests` and `dart.include_tests` defaulted to `false`. With the
default, a file that uses a symbol only from its tests was not in the graph, and
`find_references` answered `COMPLETE` without it. The language-recall benchmark
measured how often:

| Repository | File recall, tests not read | Tests read |
|---|---|---|
| `cobra` (Go) | 65.9 % | 99.2 % |
| `gin` (Go) | 50.0 % | 98.9 % |
| `caddy` (Go) | 77.5 % | 95.8 % |
| `prometheus` (Go) | 53.5 % | 97.1 % |
| `collection` (Dart) | 74.0 % | 99.0 % |

Recall on non-test files is the same in both columns. Everything the default
lost was a test file.

The reasons the default was `false` no longer hold. Packages with several
`init` functions (ADR 0130) and symlinked sources (ADR 0131) made repositories
such as `gin`, `caddy` and `prometheus` impossible to index, and the identity
collisions between test files of one package are fixed. Measured after those
fixes, reading tests on those three costs 1.5x to 3x the edges, 0 to 6 seconds
of indexing and 13 to 202 MB of state.

The cost to the reader is that tests become rows. `find_references` had no way
to ask for production uses only, which is a fair question, so the default could
not change without one.

## Decision

- `go.include_tests` and `dart.include_tests` default to `true`. The other
  languages are unchanged: Rust already read its tests, and Python, Java and C#
  are not measured.
- `find_references` takes `exclude_tests` (boolean, default `false`). It drops the
  rows declared in a test file, recognised by path with the same rule `impact`
  uses (`IsTestPath`). It applies before the total and the page, and it is part of
  the query hash, so a cursor minted without it is refused with it.
- The pagination guidance of `find_references` names it.

## Consequences

- A `config.yaml` written by an earlier `kivgraph init` spells every key out, so
  it keeps `include_tests: false` and the old behaviour until edited. Only a
  new installation, or a deleted key, gets the new default. Nothing rewrites an
  existing file.
- The next `kivgraph index --full` of a configuration that reads tests produces
  a larger graph. A new graph also has more answers in `find_references` and
  `get_blast_radius`, which is the purpose.
- The language-recall benchmark's second configuration is now the one that
  switches tests off (`go.tests_excluded`, `dart.tests_excluded`).
- Only `find_references` has the filter. `find_implementations`,
  `trace_dependencies`, `get_blast_radius` and `find_cross_repo_consumers` return
  test rows when the graph holds them.
- Test files are recognised by path. A Rust `#[cfg(test)]` module inside a source
  file is not seen.
