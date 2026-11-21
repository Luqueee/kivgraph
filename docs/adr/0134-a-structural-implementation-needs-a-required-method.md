# ADR 0134: a structural TypeScript implementation needs a required method

## Status

Accepted.

## Context

TypeScript has no `implements` requirement: a class is assignable to any type
whose members it carries. The worker reports that as a structural
`IMPLEMENTS`, and `find_references` uses it to forward the references to an
interface to its **only** implementation. That is useful for a contract with
behaviour and wrong for a bag of data.

On `ky`, `find_references` on the error class `ResponseSizeError` returned
dozens of rows from `options: Options` parameters in files that never name the
class. Each row carried `via: "Options"`: the class had a `maxResponseSize`
field, `Options` declares `maxResponseSize?`, so the class was assignable to it
and became the only "implementation" of `Options`. `SchemaValidationError` and
`HTTPError` were reported the same way through a result type with an `issues`
field and an update type with optional members. Of 165 reported files, 15 were
extras the compiler did not flag; this accounted for 9 of them.

## Decision

A **structural** implementation requires the target to declare at least one
required **method**. A type with only fields, or only optional members, asks
nothing of a class's behaviour and is skipped. A **declared** `implements` is a
statement and keeps its edge, whatever the shape.

The same rule applies to the exhaustive path, so the optimised selection still
equals the compiler oracle.

A property whose type is a function (`on: () => void`) is not a method for this
rule, so a structural match to an interface made only of those is not reported.
This is a limit and not a goal.

## Consequences

- On `ky`, file precision went from `90.9 %` to `96.2 %` and file recall stays at
  `100 %`.
- `find_implementations` on an options bag or a data type no longer lists every
  class that happens to carry one of its fields.
- The previous test, which asserted that a class with no members implements an
  empty interface and one with all-optional members, is replaced: it pinned the
  trivial matches this ADR removes.
- Existing graphs need `kivgraph index --full`.
