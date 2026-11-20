# ADR 0128: names inside macro bodies are candidates

## Status

Accepted.

## Context

`rust-analyzer scip` emits no occurrence for a token inside the body of a
`macro_rules!` definition: the body is a token tree and nothing in it is
resolved. A declaration used only there had no reference edge, and
`find_references` answered `COMPLETE` without the file that names it.

The language-recall benchmark found it on `sharkdp/fd`: the trait method
`sanitize`, called as `$crate::error::SanitizeErr::sanitize($arg)` in a macro,
and the function `correct`, called inside another. Two files missed in 96
complete answers (file recall `98.5 %`).

## Decision

The loader reads the transcriber of every `macro_rules!` rule with the grammar
and lists the paths in it. It keeps a path only when it names a declaration:
two or more segments, or one segment followed by an argument list. A path
after `.`, before `!`, or with a metavariable in it is left out, and so is
the leading `$crate`, `crate`, `self` or `super`.

A path is resolved against the declarations of the macro's own crate whose
descriptor path ends with the written one. Exactly one match makes an edge; two
or more make none, and a bare called name only matches a free function. The
source is the macro, the evidence is the last segment.

The edge is `CANDIDATE` with provenance `TREE_SITTER_SYNTAX`, never `EXACT`:
nothing proved which declaration a token tree reaches, only that one fits.
No new provenance code is added.

## Consequences

- A declaration used only inside a macro body gains a candidate reference.
  Measured on `fd`: file recall `98.5 %` to `100 %`, answers that said
  `COMPLETE` and missed a file `2` to `0`, precision `97.7 %` to `97.8 %`.
- `find_references` reports these rows with `confidence: CANDIDATE`; a caller
  that filters to exact edges does not see them.
- A name that fits two declarations, a method called on a receiver and a
  name passed through a metavariable are still absent. Macro invocations are
  not resolved either: this covers names written in a body, not the macro's
  expansion at each call site.
- Graphs built before this change need `kivgraph index --full`.
