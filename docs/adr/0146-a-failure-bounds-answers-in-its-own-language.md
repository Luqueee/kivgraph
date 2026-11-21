# ADR 0146: A failure bounds answers in its own language

## Status

Accepted. Changes when a tool answers `COMPLETE`; no schema, no stable key and
no format changes. It narrows the rule of ADR 0063 and replaces none of it.

## Context

ADR 0063 gave every answer a verdict. `LOWER_BOUND` means the index recorded
something that could add to the answer, and for a question about a declaration
the first such thing is a reference the resolver could not follow that asked
for the declaration's name.

That lookup read the name and nothing else, over the whole snapshot. A snapshot
holds every repository of the installation and every language they are written
in, so an unresolved `Command` in a Rust file -- `std::process::Command` --
bounded the answer about the `Command` type of a Go repository.

Measured on two repositories at their benchmark commits, `find_references` for
`cobra`'s `Command`:

| indexed | verdict | blind spots |
|---|---|---|
| `cobra` alone | `COMPLETE` | none |
| `cobra` and `fd` | `LOWER_BOUND` | `src/exec/mod.rs`, `Command` |

The answer was the same `803` references both times. The verdict of a question
about one repository depended on which other repositories were registered, and
for a common name on a mixed installation it was `LOWER_BOUND` nearly always --
which is the constant ADR 0063 was written to avoid.

## Decision

A failure bounds an answer about a declaration only when it could have been
looking for one of that declaration's language.

A reference is resolved by its own language's analyzer and can only land on a
declaration of that language: `go/types` never resolves to a Rust item, and
`rust-analyzer` never to a Go type. The snapshot already stores the language of
every failure. `UnresolvedNamingSymbol` takes the language of the declaration
and skips failures recorded in another.

- A failure with no language recorded is kept. It cannot be ruled out.
- A failure of the same language in another repository is kept. That is a
  consumer that may not have resolved, which is what the check is for.
- `find_symbol` asks about a name and not about one declaration, so it passes
  no language: an unresolved `Command` in any language is a `Command` the graph
  may not hold.
- The scope half -- packages the index could not read -- is unchanged.

## Consequences

- `find_references`, `get_blast_radius` and `find_cross_repo_consumers` answer
  `COMPLETE` in cases where they answered `LOWER_BOUND` because of another
  language. `coverage.unresolved_related` falls by the same failures.
- The verdict of a question about a repository no longer moves when a
  repository in another language is registered or removed.
- No answer that was `COMPLETE` becomes `LOWER_BOUND`.
- On the recall benchmark, whose default corpus indexes four languages
  together, five of `600` answers turn from `LOWER_BOUND` to `COMPLETE`: one for
  Go, two for TypeScript, two for Dart. None of the five had missed a file:
  the count of `COMPLETE` answers that missed one is unchanged, and so is every
  recall and precision figure.
- A graph that ever holds edges across languages -- a contract edge, phase 8 of
  the roadmap -- has to revisit this: the premise is that none exists.
