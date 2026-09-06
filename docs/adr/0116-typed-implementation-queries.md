# ADR 0116: Typed implementation queries and coverage

Status: accepted for local implementation, 2026-09-05.

## Decision

Expose `find_implementations` as a read-only, single-text MCP query. It reads
`IMPLEMENTS` and `OVERRIDES` edges of one immutable published generation. It does
not reinterpret calls or name matches as implementations. Go retains its
existing `go/types` proofs. TypeScript emits declared and structural proofs
at index time through the installed native compiler's `isTypeAssignableTo`.

The worker examines concrete class instance types, interfaces, abstract types,
object type aliases, and concrete type instances observed in type references,
heritage clauses and construction. It never substitutes `any` for unknown type
parameters. Required member names are a conservative prefilter; the compiler
makes the final decision. Native calls are serial, and property/assignability
caches are confined to a project generation. Tests compare this selection to
an exhaustive compiler evaluation.

Interface method/property declarations now use the same classifier as provider
source lookup. Inherited implementation methods point to the actual declaring
symbol. External type identities reuse resolved imports. External method
identities require the provider's own project and canonical source declaration;
missing source identity is recorded as a coverage limitation.

## Wire and storage

`ts-facts-v5` adds `implementations` and `implementationLimitations`. Its relation
rows reuse the proven local/provider target identity shape. The Go decoder also
reads v4 for historical fixtures, with an explicit missing-analysis scope.
Provenance codes `30` and `31` append `TYPESCRIPT_IMPL_DECLARED` and
`TYPESCRIPT_IMPL_STRUCTURAL`; existing codes and stable-key derivation do not
change. Canonical schema `5` requires a full rebuild and attests the new analysis
pass. The previous database and matching executable remain the rollback pair.

Query pages carry canonical locations, provenance, detection, confidence,
generation, total, cursor and completeness. Cursors bind the query, filters,
profiles and generation. Result path filters run before counting/pagination.
Multi-profile queries retain independently indexed scopes. Legacy generations
return `LOWER_BOUND`, including when empty. New generations also return
`LOWER_BOUND` for recorded unresolved scopes across the analyzed corpus. An empty
`COMPLETE` result establishes absence only within that corpus and type-instance
universe. Inferred files currently contribute symbols/references and explicitly
do not attest implementation coverage. Compiler-error declarations and unknown
provider method identities cannot become exact edges.

Atenea binds its existing `symbol.implementations` contract to this tool,
preserves local `locations`, and adds optional evidence and pagination fields.
Atenea's maintenance coordinator owns rebuilding; a normal query cannot inherit
an indexing timeout.

## Validation and activation

Negative type fixtures, declared/structural/method/generic fixtures, actual v5
worker output, canonical normalization, scoped pagination and generation/filter
cursor rejection are required. The complete native Ladybug gate and a real
local full rebuild must pass before activation is considered validated.
