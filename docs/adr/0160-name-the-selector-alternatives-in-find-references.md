# ADR 0160: Name the selector alternatives in find_references

## Status

Accepted. Changes MCP descriptions only. No client migration or rebuild.

## Decision

Say in the `find_references` tool description that `name`, `stable_key` and
`qualified_name` are alternatives. Name the subject `repository` next to `path`;
`repo` filters result rows and cannot locate the subject. Keep rejecting
conflicting selectors rather than guessing which symbol the caller meant.

## Evidence

On six frozen questions asked twice, the original descriptions yielded six
invalid calls. Changing field descriptions alone yielded five; also naming the
choice in the tool description yielded one. Both the original and final runs
answered all twelve questions correctly on the required facts. The small,
non-randomized final comparison does not establish a general cost advantage.
See `benchmarks/mcp-token-cost/selector-schema-ab.json` for all three variants.

## Amendment: redundant short name

`find_references` also accepts `name` alongside `qualified_name` as a
check, not an alternative lookup. Resolve the qualified selector first.
Return its unchanged result only if it selects a declaration with the exact
short name supplied. A mismatch is `INVALID_ARGUMENT`; an ambiguous or
missing qualified selector keeps its original error even when the short
name alone would resolve. `stable_key` remains exclusive. No rebuild or
client migration is required.
