---
title: find_implementations
description: Find compiler-proven implementations of types and methods, with generation and coverage.
---

`find_implementations` returns typed implementations of an interface, abstract
type or method. Go uses existing `go/types` relations. TypeScript includes both
declared relationships and structurally compatible concrete class instances.
The compiler decides compatibility during indexing.

```json
{"name":"Reader","repository":"my-library","limit":50}
```

Use `stable_key`, a bare `name`, or `repository`, `path` and `qualified_name` to
select the subject. `repo`, `language`, `paths` and `detection` filter result
rows. Detection accepts `declared` or `structural`; omitting it includes every
supported typed mechanism. `paths` contains repository-relative files or
directories. `profile` selects one or more independently indexed graphs.

It reads the typed `IMPLEMENTS`, `EXTENDS` and `OVERRIDES` relations of the selected
generation, incoming to the subject.

## Arguments

| Argument | Type | Default | Meaning |
| --- | --- | --- | --- |
| `profile` | array of strings | configured default | Profiles to query. `["*"]` alone selects all. A stable key requires exactly one named profile when several profiles exist. |
| `cursor` | string | none | Opaque token taken from `next_cursor`. Keep the filters unchanged; it is bound to the served generation. |
| `detection` | string | none | Filters the evidence: `declared` or `structural`. Omitted, it includes both and the other typed mechanisms. Anything else is rejected with `INVALID_ARGUMENT`. |
| `exclude_tests` | boolean | `false` | Drops the rows declared in a test file, matched by path as `find_references` does. |
| `language` | string | none | Filters rows by language, as stored for the symbol: `go`, `typescript`, `rust`, `python`, `dart`, `java` or `csharp`. |
| `limit` | integer | `50` | Rows in this page. Must be between 1 and 500. |
| `name` | string | none | The subject's name. Ambiguity returns candidates. Alternatively use `stable_key` or `qualified_name`. |
| `path` | string | none | Repository-relative file declaring the subject. |
| `paths` | array of strings | none | Repository-relative files or directories to include in the rows. Each must be a clean relative path: an empty entry, `.`, an absolute path, `..` or one that leaves the repository is rejected with `INVALID_ARGUMENT`. |
| `qualified_name` | string | none | The subject's fully qualified name. |
| `repo` | string | none | Filters rows by repository. It filters the answer; `repository` selects the subject. |
| `repository` | string | none | Repository declaring the subject. |
| `stable_key` | string | none | The subject's canonical key. Alternatively use `name` or `qualified_name`. |

## Answers

The answer is always the `full` view with the detailed identifiers: the tool has
no `view`, `response_format` or `include_derived` argument. `results` holds
`subject`, `implementations` and `scope`, a constant sentence naming what was
searched: `Typed IMPLEMENTS and OVERRIDES relations in the selected published
generation(s).` Each row carries its canonical location, `language`, `edge_kind`,
`confidence`, `provenance`, `evidence_kind`, `stable_key`, `file_key`,
`repository_key` and `detection`, and `via` when the row was reached through an
interface method. Only edges with an exact confidence are returned. `detection`
is `declared` for a TypeScript declared relationship, `structural` for a
TypeScript structural match or a Go `go/types` relation, and `typed` for every
other typed mechanism. The `detection` argument filters on the first two.
The envelope includes generation, totals, completeness and a
`next_cursor`. Keep filters unchanged on the next page. A changed generation
requires a fresh first page.

`guidance` is present on every answer, rows or not, and is a constant sentence:
`Absence is established only for COMPLETE coverage within the analyzed corpus.
Concrete classes and observed type instances; no unknown generic arguments are
replaced with any.` Rows of the provider Kivgraph derives from the machine, in
the `rust:` namespace, are withheld unless `repo` names it.

Read `completeness` before interpreting zero rows. `LOWER_BOUND` identifies an
incomplete analysis or a legacy generation; it cannot establish absence. On a
generation older than schema 5 the verdict is `LOWER_BOUND` with an invisible
scope whose reason is `IMPLEMENTATIONS_NOT_ANALYZED`, because that generation
never ran implementation analysis. The scope half of the check is global, not
limited to the subject's repository.
`COMPLETE` applies only to the analyzed corpus and observed type instances.
Unknown generic arguments are never replaced with `any`. Inferred sources and
provider members without canonical source identity carry explicit limitations.
