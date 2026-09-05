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

The result contains `subject` and `implementations`. Each row carries its
canonical location, stable key, relationship kind, confidence, provenance and
detection. The envelope includes generation, totals, completeness and a
`next_cursor`. Keep filters unchanged on the next page. A changed generation
requires a fresh first page.

Read `completeness` before interpreting zero rows. `LOWER_BOUND` identifies an
incomplete analysis or a legacy generation; it cannot establish absence.
`COMPLETE` applies only to the analyzed corpus and observed type instances.
Unknown generic arguments are never replaced with `any`. Inferred sources and
provider members without canonical source identity carry explicit limitations.
