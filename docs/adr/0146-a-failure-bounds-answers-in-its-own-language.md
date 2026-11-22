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

## Amendment: a scope is read by language too

The decision above filtered the failures that name a symbol and left the other
half of the verdict as it was: the scopes the index could not read at all,
`invisible_scopes`. They were taken for the whole repository, so in a
repository of two languages no answer was ever `COMPLETE`. Found on a real
one: every answer about a Rust declaration was `LOWER_BOUND` because of npm
packages nobody registered, `PACKAGE_PROVIDER_NOT_FOUND`.

A scope now bounds an answer about a declaration under the same rule: if it
was recorded in that declaration's language, or in none.
`trace_dependencies` reads the language of its root, and `get_file_outline`
that of the file when the path names one file.

Unchanged: `find_symbol`, which asks about a name, and the outline of a
directory, under which what is missing can be in any language.

On that repository the outline of a Rust file goes from `LOWER_BOUND` to
`COMPLETE`, and its answer from `1835` to `1018` bytes.

Removing the scopes showed what they had been covering. A Rust failure records
the path it tried, `replay::ReplayWindow`, and the lookup matched a name only
bare or after a dot: a failed use of anything below the root of a crate asked
for no name. With the scopes gone, `find_references` for a struct that a
second Cargo workspace of the same repository uses answered `COMPLETE` and
missed that file. A name now also matches after `::`. The one shape left out
is `module::impl::Name`, the `impl` block SCIP mentions for `-> Self` and never
defines: a type naming itself in its own block, which would bound every type
that has one.

So that struct answers `LOWER_BOUND` again, now for the reason that is true,
with the file named.

Matching after `::` alone was too much: on `fd` it turned `16` complete answers
of `100` into `LOWER_BOUND`, because `new`, `push` and `parse` are the last name
of `std::path::impl::PathBuf::new` and the like. A failure whose crate no
registered repository provides, `CRATE_PROVIDER_NOT_FOUND`, was not looking for
a declaration the graph holds, and no longer bounds an answer about one. It
still bounds `find_symbol`.

Measured again, `100` declarations each, recall and precision unchanged and no
`COMPLETE` answer missing a non-test file before or after:

| repository | `COMPLETE`, before | `COMPLETE`, now |
|---|---|---|
| `fd` | `96` | `98` |
| `ripgrep` | `97` | `96` |

The Go, TypeScript and Dart verdicts did not move.

## Amendment: a dependency nobody registered bounds no declaration

The same holds for a scope, and in every language: a package, crate or module
that no registered repository provides (`PACKAGE_PROVIDER_NOT_FOUND`,
`CRATE_PROVIDER_NOT_FOUND`, `MODULE_PROVIDER_NOT_FOUND`) was not looking for a
declaration the graph holds. It no longer makes an answer about one
`LOWER_BOUND`. It still bounds `find_symbol`. A provider failure recorded on a
file still bounds, except `CRATE_PROVIDER_NOT_FOUND`.

Measured, `100` declarations each, precision unchanged:

| repository | `COMPLETE`, before | `COMPLETE`, now | wrong, now |
|---|---|---|---|
| `got` | `0` | `100` | `0` |
| `ky` | `100` | `100` | `0` |
| `fd` | `96` | `98` | `0` |
| `ripgrep` | `97` | `93` | `0` |

On `got` every answer had been `LOWER_BOUND` because of npm packages, which
said nothing. Without them two answers were wrong, and had always missed the
same files: `dnsCache` and `maxHeaderSize` are `set` accessors. The checker
has one symbol for a `get`/`set` pair, and the worker attached every use to
whichever was declared first. An assignment now reaches the `set`, a read the
`get`, and `+=` or `++` both; file recall on `got` went from `220` of `223`
to `223` of `223`. The key of an object literal still reaches the `get`.

## Amendment: incomplete implementation relations bound what they could answer

`IMPLEMENTATION_COVERAGE_PARTIAL` is a scope a TypeScript package records when
some relation of implementing, extending or overriding could not be proven: a
declaration with compiler errors, an imported target that is not an interface
or a type alias. Everything else in the package was read. As a scope it
bounded every TypeScript answer of the repository, and ADR 0155 made it
common: one class imported from a sibling package, and who calls a function
was `LOWER_BOUND` for good.

It now bounds an answer such a relation could be part of:

| question | bounded |
|---|---|
| `find_implementations` | always |
| `get_blast_radius`, `trace_dependencies` | always: a walk crosses any relation |
| `find_references`, `find_cross_repo_consumers` | by the kind, below |
| `find_symbol`, `get_file_outline` | never: neither is a relation |

The kinds nothing implements, extends or overrides are listed -- `function`,
`variable`, `enum`, `enum_member`, `namespace`, `parameter`, `type_parameter`,
`import`, `export`, `module` -- and a kind the list does not know keeps the
bound. A class, an interface, a type, a method and a property keep it.

On the monorepo of ADR 0155 the function every page imports answers
`COMPLETE` with its `29` files; the class beside it answers `LOWER_BOUND`
with the scope named, and so does the walk from either.

Not narrowed: the scope names the package that recorded it, not the
declarations it could not relate, so a class nobody in that package touches
is still bounded.
