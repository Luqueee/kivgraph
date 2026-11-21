# ADR 0148: `get_test_health` separates runtime coverage from static test support

## Status

Proposed. Adds a read-only MCP tool. Changes the tool surface, the skill and the
surface byte budget; changes no schema, stable key or canonical edge.

Issue: #145.

## Context

An agent asks whether production code is protected by tests. Two different
facts answer it, and they must never be merged:

- **Runtime coverage** is an observation: a test run executed these lines and a
  tool wrote a report. It exists only if someone ran the tests.
- **Static test support** is a derivation: the published graph has a call path
  from a test symbol to this symbol. It proves the code may be reached from a
  test, not that any branch executed.

A missing report is not `0%`. Treating it so is the one wrong answer this tool
can give that looks like a measurement. Requiring an upload step makes the
signal vanish in ordinary local workflows.

What the repository already has:

- Test files are recognised by path with `testpath.Is` (`internal/testpath`),
  the one rule the loaders and `exclude_tests` share. A Rust test module inside
  a source file has no such mark and is not seen.
- Go, Dart, Java, C# and Python read their tests by default (ADRs 0132, 0142
  and 0144), so test symbols and their `CALLS` edges are in the graph for those
  languages.
- `CALLS` edges already carry confidence and provenance: `EXACT*`,
  `CANDIDATE` and `UNRESOLVED` are distinct results.
- `trace_dependencies` walks reachability with a `max_nodes` cut reported as a
  lower bound. Responses carry `snapshot_id`, `coverage` and generation-bound
  cursors.
- The tool surface has a byte budget enforced by
  `internal/mcp/surface_test.go`. A new tool moves it.

## Decision

### The tool

`get_test_health` is read-only. It runs no command, writes no file and mutates
no repository.

```text
get_test_health(
  repo?, path?, name? | stable_key?,   target: a file or a symbol
  mode: auto | runtime | static,       auto returns both, in separate fields
  limit?, cursor?
)
```

The target uses the same repository, path and qualified-name triple as the other
tools. The response carries `snapshot_id` and `coverage`, bounded rows and a
cursor bound to the served generation.

### Two fields, never one number

The response has a `runtime` object and a `static` object. No field combines
them and no percentage is derived from both. A caller that wants one answer
reads two.

### States

One vocabulary, reported per object:

|state|object|meaning|
|---|---|---|
|`MEASURED`|runtime|observed report, bound to the current tree|
|`STALE`|runtime|report commit or file digest no longer matches|
|`UNVERIFIED`|runtime|report paths map to no file, or to several|
|`UNAVAILABLE`|runtime|no report was found|
|`ESTIMATED`|static|derived from graph edges; not an execution|
|`TESTS_NOT_INDEXED`|static|no test files of this scope are in the graph|

`UNAVAILABLE` is never rendered as `0%`. `TESTS_NOT_INDEXED` is a different
answer from "indexed, and no test reaches this": the first says the question
could not be asked, the second says it was asked and the answer is none. An
`UNVERIFIED` or `STALE` report fails closed: its numbers are withheld, and the
reason and report path are returned.

### Runtime evidence

- Artifacts are looked for inside registered repositories only, to a bounded
  depth, by well-known names: Go cover profiles (`coverage.out`, `cover.out`),
  LCOV (`lcov.info`), Cobertura (`coverage.xml`, `cobertura.xml`) and JaCoCo
  (`jacoco.xml`). A file over a size cap is not read.
- A measured result names its source report, its format, its `metric_basis`
  (`statement`, `line`, `function` or `branch`) and its freshness binding: the
  repository commit when the report records one, otherwise the file content
  digest. A report with neither is `UNVERIFIED`, never `MEASURED`.
- Go profiles are statement-level. Branch counts exist only in formats that
  record them. `metric_basis` is what stops a statement ratio being compared with
  a branch ratio.
- A report path maps to exactly one indexed file or the report is `UNVERIFIED`.
  Two repositories sharing a suffix are ambiguous, not a tie broken by order.
- A file with zero measurable statements is reported as such, not as `0%` or
  `100%`.

### Static evidence

Derived from the published generation at query time. No new canonical edge, and
in particular no `TESTED_BY`: the evidence and freshness contract of such an edge
is not defined, and adding one needs a schema decision this ADR does not take.

Three tiers, returned in three separate arrays, each with its own confidence:

|tier|rule|confidence|
|---|---|---|
|`direct`|test symbol has a resolved `EXACT*` `CALLS` edge|the edge's|
|`transitive`|bounded `CALLS` path reaches the target|weakest edge|
|`convention`|package and name convention only|candidate, lowest|

- An unresolved call is never `direct`. An import without a call is not support.
- The transitive walk reuses the existing traversal and its lower-bound cut.
- `convention` is never merged into the other two.
- Every row names the production symbol and the test symbol with repository,
  path and line range.

### Surface cost

The argument list is the minimum above. The byte budget in `surface_test.go`
moves by the measured size of the new tool, and descriptions are trimmed before
the number is raised. The cost of a response is measured with
`benchmarks/mcp-token-cost` and recorded with the change that registers the tool.

## Consequences

- A repository with no report gets `UNAVAILABLE` plus static support. That is
  the normal local case and it is honest about what it knows.
- A language whose tests live in source files, such as a Rust inline test
  module, gets `TESTS_NOT_INDEXED`. This is a declared limit, not an absence.
- A report written before the last edit is `STALE`. The tool never runs tests to
  refresh it.
- Parsing happens per call. A cache keyed by path, mtime, size and digest is
  added only if the response cost measurement shows parsing dominates.
- Adding the tool changes the MCP surface, the skill and the user-facing tool
  reference. Hosts with the hook installed before the tool existed need no
  re-install: the matcher covers the server by name.

## Rejected alternatives

- Requiring a manual report upload: the signal disappears in normal workflows.
- Counting imports, file names or test naming conventions as coverage.
- Running test commands from the MCP server: it executes arbitrary code from a
  read-only tool.
- A canonical `TESTED_BY` edge now: the evidence and freshness contract comes
  first, and this ADR is that contract.
- One blended percentage: a call path is not an execution.

## Implementation stages

Each is one reviewable change, shippable alone.

1. This ADR.
2. `internal/coverage`: parsers for the four formats into one normalised shape,
   with size caps. Compared against `golang.org/x/tools/cover` for the Go format.
3. Discovery and freshness binding: `MEASURED`, `STALE`, `UNVERIFIED`,
   `UNAVAILABLE`.
4. Static test support over the snapshot, with the brute-force oracle.
5. The MCP tool, and the surface budget.
6. Protocol doc, skill, tool reference, `AGENTS.md` routing row, cost
   measurement.
7. Optional report cache, only if measured to matter.

Stages 2 and 4 are independent. Negative tests are written first: missing,
truncated, duplicated and oversized reports; stale reports; ambiguous paths;
unresolved calls; imports mistaken for support; unindexed tests.
