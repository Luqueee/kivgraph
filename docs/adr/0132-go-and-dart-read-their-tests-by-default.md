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

Measured on the same repositories and `cobra`, with the binary serving the
published graph:

| Repository | Index peak RSS | Serving RSS | Query p95 | Default answer |
|---|---|---|---|---|
| `cobra` | 0.61 to 0.74 GB | 78 to 80 MB | 0.6 to 0.5 ms | 544 to 707 B |
| `gin` | 1.15 to 1.36 GB | 82 to 83 MB | 0.2 to 0.7 ms | 432 to 662 B |
| `caddy` | 2.23 to 2.77 GB | 98 to 104 MB | 0.6 to 0.6 ms | 484 to 589 B |
| `prometheus` | 5.50 to 6.62 GB | 111 to 146 MB | 1.9 to 4.0 ms | 1388 to 1458 B |

Serving is cheap: at most `35 MB` more, and a query stays under `5 ms`. A
default `find_references` answer grows by `5 %` to `53 %` because the compact view
groups rows by file; with `view: full` and every edge kind the mean answer grows
`1.4x` to `3.7x`. The peak while indexing is the cost that matters: `+0.1` to
`+1.1 GB`, on top of a baseline that is already `5.5 GB` for `prometheus`. It is
Go heap, mostly the syntax trees and type-checker tables of every dependency
(`GOMEMLIMIT` cannot go below the `3.9 GB` that is live), and no cheap setting
lowers it: see the roadmap.

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
- `find_implementations`, `trace_dependencies`, `get_blast_radius` and
  `find_cross_repo_consumers` returned test rows with no way to leave them out;
  see the amendments below.
- Test files are recognised by path. A Rust `#[cfg(test)]` module inside a source
  file is not seen.

## Amendment

`exclude_tests` is also an argument of `find_implementations`,
`trace_dependencies` and `get_blast_radius`, with the same path rule. In
`get_blast_radius` it is applied where the row filters are, before the axes are
counted, so the page and `affected`, `by_repository`, `by_package`, `by_depth`
and `by_kind` describe the same symbols; the walk itself still crosses a test
file, which is where a test is a link and not a leaf. A cursor is bound to it,
like to every other filter. `trace_dependencies` refuses it together with `to`,
as it does `repo` and `language`: a route with a link left out is not a route.
`find_cross_repo_consumers` did not take it; the second amendment adds it.

The schema ceiling went to `18.600` bytes for the three arguments, `122` each.

## Second amendment

`find_cross_repo_consumers` takes `exclude_tests`. It drops a consumer symbol
or an unresolved failure whose file is a test, before the coverage is counted,
so `total` and `coverage` describe the page returned. A package dependency
names no file and stays: it proves a repository depends on the provider, and
that a test file alone caused it is not something the row records. `completeness`
still counts every failure the index could not read, tests included, because
that figure says whether the empty answer can be trusted. A cursor is bound to
the argument. The description is `46` characters and the surface stays under
the `18.600` ceiling.
