# ADR 0156: A compact answer groups what a completeness block repeats

## Status

Accepted. Changes the output of every tool that carries `completeness`, under
the default `compact` view only: `invisible_scopes` and `blind_spots` are
grouped. `view: "full"` is byte for byte what it was. Changes no tool name,
input schema, protocol version, persistent schema or stable key; no rebuild
is needed.

## Context

Measured on this repository with a binary of `main` (`0.10.1`), the cost of
`find_references` was no longer what the published figures say. The answer to
who calls `MergeAll`, five references, was `6,089` bytes; `completeness` was
`5,510` of them. For `NewServer` it was `11,805` of `12,257`. The same block
comes back on every call of a session about a repository, so a question that
takes two calls pays for it twice. `benchmarks/mcp-token-cost` read it as
`0.24x` of what `grep` costs for the answer, against `1.76x` recorded at
`0.8.0`.

ADR 0076 already made an invisible scope one row per package and not one per
failure. What was left in the rows is repetition of another kind. Thirteen rows
of `invisible_scopes` say `"repository":"kivgraph"` thirteen times, six of them
say `"reason":"PACKAGE_NOT_BUILDABLE"`, and each carries a `detail` that is
either a path the `fallback` lists again or a compiler message about a fixture.
Twenty rows of `blind_spots` repeat the repository, the reason, the requested
symbol and a seventy-character requested package, and differ only in a file and
a line.

The block is the reason `COMPLETE` means something (ADR 0059, ADR 0146), so
removing it was never an option. What can go is what the rows repeat.

## Decision

Under `view: "compact"`, the default, a `completeness` block is spelled so that
every fact is said once.

**`invisible_scopes`** are grouped by `(repository, reason)` and, when a row
names a file, by that too:

```json
{"reason":"PACKAGE_NOT_BUILDABLE",
 "packages":["pkg/a","pkg/b"],"occurrences":2}
```

`packages` lists each requested package once, in the order the rows came.
`occurrences` is the sum of the recorded failures behind the group, which ADR
0076 defined per scope. `detail` is kept when every row of the group carries
the same text, which is when it says something about the reason; with one row
it is always kept. When rows disagree, or only some have one, it is left out
and the block says `"details":"view=full"`, once, where to read it.

**`blind_spots`** are grouped by `(reason, repository, requested_symbol,
requested_package, detail)`. Each failure stays as one `path:line` in `at`; a
failure with no file is counted in `unlocated`. A blind spot is evidence about
one place, and every place is still named.

**`repository`** moves to the block when every group of both lists names the
same one, and stays on the groups otherwise.

`more_invisible_scopes`, `more_blind_spots` and `fallback` are untouched: the
cut that makes a list a lower bound, and the recovery action, are not spelling.
The multi-profile envelope spells each profile's verdict the same way.

`view: "full"` keeps one row per scope and per failure, with every field, as
ADR 0076 left it.

## What is not lost

Compared with the full rows over generated corpora
(`TestCompactScopesLoseNoPackageReasonOrOccurrence`,
`TestCompactBlindSpotsLoseNoPlace`): every `(repository, reason, package)` is
in exactly one group, the occurrences add up, and every `path:line` and every
unlocated failure is accounted for. The only text that can leave a compact
answer is a `detail` the rows of a group did not share, and the answer says it
did.

## Measured

Same repository, same generation, `find_references`, bytes of the answer.

| name | full | compact |
| --- | ---: | ---: |
| `MergeAll` | `6,270` | `2,792` |
| `NewServer` | `13,326` | `4,681` |

What remains of the block is mostly what is not repeated: the `detail` of the
scopes that are alone in their group, and the `fallback` paths, which are the
directories to search.

## Compatibility and migration

A client that reads `completeness.verdict`, `more_invisible_scopes`,
`more_blind_spots` or `fallback` is unaffected.

A client that reads the rows of `invisible_scopes` or `blind_spots` has two
ways to continue:

1. Ask for `view: "full"`. The rows are exactly as before.
2. Read the groups. A scope was `{reason, repository, requested_package,
   detail, occurrences}`; it is now an entry of `packages` in a group with that
   `reason`, in the block's `repository` (or the group's), under the group's
   `occurrences`. A blind spot was `{reason, repository, file_path,
   start_line, requested_symbol, requested_package, detail}`; it is now an
   entry of `at`, `file_path:start_line`, in a group that holds the rest.

To count scopes, count `packages`, and count a group with none as one. The
`benchmarks/tool-honesty` harness did this and was changed with it; nothing
else in the repository read the rows.

The skill and the reference pages say where the full spelling is. A cached
copy of the skill from before this change describes rows that a compact answer
no longer has; `kivgraph skill install --force` refreshes it.

## Not done

The `fallback.paths` are one entry per directory and are the largest part of
what is left. Collapsing them under their common parent would change what a
client can run them over, and was not decided here.

The groups do not carry a per-package `occurrences`: a reader who needs
`18` for one package of a group of five asks for the full view.
