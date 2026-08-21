# Semantic facts v1

Kivgraph's Python and Dart adapters accept a JSON document with `version: 1`,
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

The release coverage gate is `make semantic-coverage`. Its manifest maps every
required capability to a fixture and an executable test for Go, TypeScript,
Python and Dart. Exact Python coverage requires the Pyright-compatible LSP
server; the AST worker is intentionally a candidate-only fallback.
