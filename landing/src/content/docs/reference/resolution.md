---
title: Resolution vocabulary
description: What EXACT means, what it never means, and the six invariants that hold the graph together.
---

The vocabulary is the product. A graph that calls a guess a fact is worse than
no graph, because nothing downstream can tell the difference.

## The three outcomes

**`EXACT`** requires sufficient observed evidence and the correct provenance.
It is never created from a coincidence of name, text, path or alias, and never
from being the only remaining candidate.

**`CANDIDATE`** is a plausible resolution that was not proven. It is a distinct
outcome, not a weaker exactness.

**`UNRESOLVED`** is a reference the pass could not resolve at all. It is
retained, not discarded: a declared hole is a fact the graph owes its readers.
Each one keeps its reason, repository and language, and where there is a
concrete occurrence, its file, position and observed detail. A repository-level
module failure may have no file at all, and evidence is never invented for it.

## What every edge carries

Each canonical edge carries `confidence`, `provenance` and, where applicable,
an `evidence_key`. The evidence must have been observed in a `File`.

Two edges that are both exact are not necessarily equal. In TypeScript, a
symbol placed by the provider's own `.d.ts.map` is
`EXACT_TYPECHECKED` / `TYPESCRIPT_CHECKER`; a symbol placed by asking the
provider's checker what its module exports is
`EXACT_PACKAGE_MAPPED` / `TYPESCRIPT_PROJECT_REFERENCE`, because the step from
artifact to source is asserted by the provider's build configuration rather
than by a map it emitted. With no named source, the reference stays
`UNRESOLVED`.

## Stable keys

Stable keys are persistent. The algorithm, the canonical identity and the
historical `luque-stable-key` namespace do not change without a data migration
and an ADR.

A Rust symbol's stable identity is its SCIP string — crate, descriptor path and
suffix — never its signature: rust-analyzer emits no `SymbolInformation` for a
declaration outside the workspace root, so a consumer keyed on the signature
could not name the key its provider publishes.

## The six invariants

A healthy graph has zero of each. `ladygraph doctor graph` prints one line per
rule with its status and violation count, and up to twenty samples beneath each
failure with the table, key and row detail that breaks it.

```bash
ladygraph doctor graph --database /var/lib/ladygraph/graph/CURRENT/graph.db
```

- **`exact_edge_without_source`** — a semantic edge with exact `confidence`
  whose source node is not declared, for example a `Symbol` with no incoming
  `DEFINES` edge from any `File`.
- **`exact_edge_without_target`** — the same condition for the target node.
- **`missing_evidence_file`** — an edge with an `evidence_key` whose `Evidence`
  does not exist, or exists without an `OBSERVED_IN` edge to a `File`.
- **`duplicate_stable_key`** — the same `stable_key` used by two different node
  tables.
- **`unknown_confidence`** — a `confidence` or `provenance` outside the
  `facts.Confidence` / `facts.Provenance` vocabulary, or an edge that declares
  exactness backed by non-exact provenance.
- **`invalid_repository_ownership`** — a node whose `repository_key` does not
  match the repository reachable through containment (`Package` via
  `CONTAINS_PACKAGE`, `File` via `CONTAINS_FILE`, `Symbol` via `DEFINES`,
  `Evidence` via `OBSERVED_IN`), or points at a nonexistent `Repository`.

LadybugDB guarantees that every relationship has both endpoints, so "missing
source" never means the node does not exist: it means no fact declared it. An
exact edge anchored to a symbol that no file declares is a failure of the
corresponding invariant, not an acceptable degradation.

The command returns `0` only if all six rules pass, and never modifies the
database it inspects. A candidate generation that fails these checks never
becomes `CURRENT`.

## Incremental deltas

Every fact asserted by a file is withdrawn and re-asserted together with that
file. Package edges are withdrawn by their evidence too, even when both of
their endpoints survive.
