# ADR 0125: `kivgraph impact` maps a diff onto the graph

## Status

Accepted.

## Context

An agent or a reviewer about to commit asks what a change reaches. The tools
answer it one symbol at a time (`get_blast_radius`), and getting from a diff to
the symbols takes reading hunks by hand. `kivgraph impact --base REF` does that
step and then asks the same question of all of them at once.

## Decision

The command reads `git diff --unified=0 REF` against the working tree, plus the
untracked files git does not ignore, and takes the line ranges of the **new**
side of each hunk. They are lines of the working tree, which is what the graph
indexed while it is fresh, so no mapping between old and new numbering is needed.

**A change belongs to the innermost symbol that holds it.** For each run of
changed lines the symbols that overlap it are taken, and one that merely
contains another overlapping symbol is dropped: a hunk inside a method touches
the method, not its type; a hunk across two methods touches both. A removal adds
no line, so it is a gap between two neighbouring lines and belongs to a symbol
only when that symbol holds both: a deletion between two functions touches
neither, between two methods it touches the type. Removed symbols are not in a
graph built from the new tree, so a deleted file is reported as such and not
mapped.

**One traversal from all the touched symbols.** It runs `TraverseFrom` with the
incoming direction, the default reference edge kinds, the same depth and node
bounds, the same withheld kinds (`field`, `variable`) and the same withheld
derived providers as `get_blast_radius`. The engine lives in `internal/mcp/tools`
because those are unexported there on purpose; no tool, input or description
changes. A symbol that depends on several touched ones is counted once, and a
touched symbol is never reported as affected.

**Tests are separated by path.** The graph carries no test flag, so a file is a
test by the convention of its language: `_test.go`, `*.test.*` and `*.spec.*`,
`test_*.py`, `Test`/`Tests` Java and C# classes, and a `test`, `tests` or
`__tests__` directory. `by_test_file` groups the affected symbols declared in
them; the rest is `production`, capped by `--limit` with the whole count kept. A
Rust test module inside a source file carries no mark and is not seen, and a
language whose tests the index leaves out by default (Go with
`go.include_tests: false`) shows none.

**A stale graph refuses.** The content freshness the server already keeps is
checked first. `stale` is an error that says to reindex, because changed lines on
an older graph land on the wrong symbols; `--allow-stale` answers anyway and says
so. `unverified` and `unavailable` answer with a warning.

**What it reports it could not place.** `not_indexed` lists changed tracked
files with no file record, and deleted ones; `no_symbol` lists changed runs that
no symbol of an indexed file holds (a comment, an import). Untracked files the
graph does not hold are not reported: nobody changed them on purpose.

## Consequences

- A new command and no change to an existing surface: the CLI output and flags
  of other commands, the MCP tools and the skill are untouched.
- The answer is only as good as the graph: it reads the published generation and
  does not index.
- A ref that names no commit is refused before the graph is opened, and a
  checkout that is not a registered repository is refused with that sentence.
