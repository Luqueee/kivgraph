# Semantic facts v1

Kivgraph's Python, Dart and Java adapters accept a JSON document with
`version: 1`,
the language, a package, files, symbols, references, imports, Dart parts and
unresolved entries. The Go type is the executable contract:
`internal/facts.SemanticPayload`.

`authoritative: true` is reserved for a producer that resolved references with
the language's semantic analyzer. When it is absent or false, definitions are
still structural facts but reference and call edges are `CANDIDATE`. Every
reference carries source and target symbol IDs plus a file range; an import
without a local target becomes `UNRESOLVED`.

Producers may identify themselves with `analyzer`, `analyzerVersion` and a
build `variant`. A producer may also attach structured `diagnostics`; these do
not become graph edges, but allow callers to distinguish an excluded file,
unsupported runtime construct and unavailable dependency.

References and imports may carry an optional `target` object when the
declaration belongs to another registered repository. Its `repository`,
`package`, `file`, `qualifiedName`, `kind` and `signature` fields must describe
the provider identity exactly. Kivgraph merges that target after all units
finish and turns a missing or ambiguous provider into an unresolved fact.

Dart imports may additionally carry `alternatives`, `prefix` and `deferred`.
Dart `part` declarations are represented by `parts`; normalization publishes
one `PART_OF` relation between the synthetic module symbols of the part and
the library. Duplicate `part`/`part of` observations are deduplicated by their
module endpoints.

The bundled `python-worker/index.py` is intentionally a standard-library AST
fallback. It supports `.py` and `.pyi`, module/class/function/method/variable
declarations, common imports, type positions and statically visible calls.
Dynamic dispatch, runtime imports, decorators that rewrite names, generated
code and ambiguous redefinitions remain unresolved or candidate-only.
`python-worker/pyright_index.py` is the exact-mode adapter: it delegates
definition resolution to a Pyright-compatible LSP server and refuses to claim
an exact target outside the indexed provider set.

The optional target shape is:

```json
{
  "repository": "shared-widgets",
  "package": "shared_widgets",
  "file": "lib/widgets.dart",
  "qualifiedName": "Widget",
  "kind": "class",
  "signature": "class Widget",
  "source": "PROVIDER_SOURCE"
}
```

`source: PROVIDER_SOURCE` permits `EXACT_PACKAGE_MAPPED`; other analyzer-backed
targets remain `EXACT_TYPECHECKED`. A package dependency may be published even
when its individual symbol is unresolved, but that edge proves only package
dependency, not symbol usage.

Java and C# do not have an adapter that writes this JSON. They have a bridge:
`internal/scip` converts the SCIP index `scip-java` emits into the same
`facts.SemanticPayload` in process, so the payload is the contract even where
nothing serialises it. SCIP is one format with many producers -- scip-python,
scip-ruby, scip-dotnet, scip-clang and `rust-analyzer scip` all emit it -- and
the bridge is written against the format, not against Java. C# was added on it
through `scip-dotnet` and is a loader that runs an indexer and names a package.

The two producers agree on the format and on very little else, and the bridge
carries what that costs: scip-dotnet sets no symbol kind, no signature and no
`enclosing_range`, so C# kinds fall back to the descriptor suffix, its
stable-key discriminator falls back to the descriptor path, and its declaration
spans are reconstructed from definition positions and descriptor nesting. See
ADR 0081.

The bridge publishes `REFERENCES` for every use. SCIP records where a symbol
occurs and not what the occurrence was, so a call, a type position and a field
read are the same row; deriving a narrower edge kind from a descriptor suffix
would be a guess. That is a declared hole rather than an approximation.

SCIP `relationships` are read, and they are where the type hierarchy comes
from: `IMPLEMENTS`, `EXTENDS` and `OVERRIDES`. A relationship carries no
position, so its evidence is the declaring name's range, and a supertype
outside the graph produces no edge at all. See ADR 0080.

The release coverage gate is `make semantic-coverage`. Its manifest maps every
required capability to a fixture and an executable test for Go, TypeScript,
Python, Dart, Java and C#. Exact Python coverage requires the Pyright-compatible LSP
server; the AST worker is intentionally a candidate-only fallback.
