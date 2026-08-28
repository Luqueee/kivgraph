# ADR 0080: Java through a shared SCIP bridge

- **Status:** accepted with declared limitations
- **Date:** 2026-08-28
- **Revises:** ADR 0006, ADR 0048

## Context

Kivgraph indexed five languages through three different routes. Go, TypeScript
and Rust each carry a normalizer of their own in `internal/facts`; Python and
Dart share `facts.SemanticPayload` and the one normalizer behind it. Adding a
sixth language meant choosing which of those two shapes it takes.

Java's mature open indexer is `scip-java`, which emits SCIP. So does
`rust-analyzer scip`, which this project already reads: `scipwire` decodes the
format and lived inside `internal/rustloader`. SCIP is also what the published
indexers for C#, Ruby, Scala, Kotlin and C/C++ emit, so the question was never
only about Java.

## Decision

**The SCIP decoder is promoted out of the Rust loader** to `internal/scip`, and
a conversion from a SCIP index to `facts.SemanticPayload` is written once,
beside it. Java is the first language on that bridge; Rust keeps its own
normalizer and only follows the moved import.

A SCIP occurrence with the definition role becomes a symbol, and its
`enclosing_range` -- not its selection range -- is the symbol's span. Every
other occurrence becomes a reference sourced at the innermost declaration whose
enclosing range contains it, falling back to the file's module symbol. A target
this repository does not declare never becomes a local symbol; it becomes an
unresolved fact.

**Java symbols are scoped by package, not by file.** A SCIP descriptor path
already carries the full nesting and the overload disambiguator, so two files of
one package cannot collide and `Module` stays empty in the stable key.

**Java payloads are authoritative.** `scip-java` attaches the SemanticDB plugin
to `javac`, so its targets are resolved by the compiler that would compile the
code, and its edges are `EXACT_TYPECHECKED`.

`scip-java` writes where it is told: the loader passes `--targetroot` and
`--output` under `java.target_directory`, which defaults outside every indexed
repository for the same reason `rust.target_directory` does.

## Consequences

- `internal/scip` is reusable: a second SCIP language is a loader that runs an
  indexer and names a package, not a second copy of the graph model.
- Java enters the language vocabulary, the fact cache, the report counters, the
  progress phases, `doctor` and the coverage gate.
- The provenances `JAVA_SCIP_DEF` and `JAVA_SCIP_USE` are appended to the frozen
  numbering as codes `26` and `27`.
- Indexing a Java repository **builds it**. That is the cost of type-checked
  Java facts, and it is why `java.maximum_workers` defaults to `1` and
  `java.maximum_index_time` exists.

## Limitations and risks

- **The build tool still writes into the repository it builds.** `--targetroot`
  moves SemanticDB output out, but Maven's own `target/` and Gradle's `build/`
  are the build's, not Kivgraph's. A pass over a registered Java repository
  leaves them behind. The fixtures are copied before indexing so the repository
  stays clean; a user's repository will not be.
- **Relationships are not read.** SCIP carries `relationships`, which is where
  `IMPLEMENTS` and `OVERRIDES` would come from, and `scipwire` does not decode
  them. Java publishes `REFERENCES` and no type hierarchy. This is a declared
  hole, not an approximation: no edge is invented from a name.
- **Every use is `REFERENCES`.** SCIP says where a symbol occurs, not what the
  occurrence was, so a call, a type position and a field read are
  indistinguishable. Deriving `CALLS_DIRECT` from a descriptor suffix would be a
  guess.
- A member the compiler synthesises -- a `record` accessor -- is referenced and
  never declared. It is reported `DEFINITION_NOT_INDEXED`, distinct from the
  `IMPORT_NOT_RESOLVED` that drives package dependencies.
- `scip-java` classifies a `record` as unspecified, so its kind falls back to
  `type` rather than `class`.
- Position columns are UTF-16 code units even though `scip-java` declares no
  position encoding. This is measured, not assumed, and pinned by a test over a
  fixture with a non-ASCII character before a symbol on the same line.
- A multi-module build reports one package identity per module; the loader names
  the package after the repository's manifest directory instead, so every symbol
  of a repository shares one package. A finer identity would change stable keys
  and needs its own migration.
