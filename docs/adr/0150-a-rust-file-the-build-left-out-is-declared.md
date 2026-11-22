# ADR 0150: A Rust file the build left out is declared

## Status

Accepted. Changes no schema, stable key or tool. Adds file rows and unresolved
rows to a Rust graph; existing graphs gain them on the next `kivgraph index`.

## Context

`rust-analyzer` indexes a workspace for the host. A module behind a `cfg` that
is false there -- `#[cfg(target_os = "linux")] mod linux;` on macOS -- gets no
document: its declarations are not in the graph, and neither are its uses.
Nothing recorded that. On a real repository `find_references` for a struct
answered `COMPLETE` with seven files, and five more used it: the Linux and
Windows halves of the same module.

A crate with no file selected at all was already declared,
`TARGET_NOT_BUILDABLE` (`collectEmptyCrates`). A crate with some files selected
and some not said nothing about the rest.

## Decision

After the analysis of a workspace, every `.rs` file under the `src` of a crate
that has at least one indexed file, and that has no document itself, is read
for words. For each name the workspace declares that the file writes outside
its comments, one unresolved row is recorded: the file, the first line that
writes the name, the name, and `TARGET_NOT_BUILDABLE`. The file is published,
because a row with no file is a scope and bounds every answer in the
repository.

That is all that can be said without the analyzer: the file was not read, and
it writes that name. It is what makes an answer about that name `LOWER_BOUND`,
with the file named, and leaves every other answer alone.

- A file that writes no name the workspace declares is not listed.
- A test file (`internal/testpath`) is not one of them: it is left out by what
  `include_tests` asks for, and what a configuration excludes does not bound
  an answer.
- A file above `1 MiB` is not read.

## Consequences

On that repository: the struct answers `LOWER_BOUND` and names the five files;
`285` files instead of `280`, and `1580` more rows on `14885` symbols.

On the benchmark, `100` declarations each, recall and precision unchanged and
no `COMPLETE` answer missing a non-test file before or after: `fd` keeps `98`
`COMPLETE`; `ripgrep` goes from `96` to `93`. The three are `Result`,
`HashMap` and `build`: names a file that was not read also writes, on answers
that were complete. That is the cost, and it is paid by common names.

- The words are read, not Rust: a name inside a string counts, and so does a
  local variable of the same name. Both err towards saying an answer is
  bounded.
- A name the file reaches through a glob import or a macro is not seen.
- Nothing is resolved: these rows never become an edge.
