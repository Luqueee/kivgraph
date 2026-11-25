# ADR 0162: find_references can show the source around a caller

## Status

Accepted. Adds an optional input to `find_references` and an optional result
field. The default answer is byte-identical to today's.

## Context

An incoming `find_references` row names the symbol that holds the reference and
bounds that symbol's declaration. It does not show the code near the reference.
The snapshot records which symbol contains a reference, never the token position
inside it (see `ReferenceSummary`), so reading the callers takes a second
`get_source` call that names each of them.

## Decision

`find_references` accepts `include_evidence`, a boolean, default `false`.

- It is valid only for incoming queries in the `compact` and `full` views.
  `direction=outgoing` and `view=files` with `include_evidence` are rejected as
  `INVALID_ARGUMENT`. They are not silently ignored.
- When set, and when the page holds at least one distinct exact incoming row,
  the result gains `results.evidence`: an array of at most 5 entries. The same
  key is used in `compact` and `full`.
- The first 5 distinct exact locations in page order each get one entry.
  Profile variants remain distinct. Candidates and unresolved rows get none.
- Each entry carries:
  - `repository`, `path`, `start_line`: the row's declaration anchor.
  - `qualified_name`: the enclosing callable, only when verified.
  - `shown_start_line`, `shown_end_line`: 1-based inclusive bounds of `code`.
  - `code`: those lines joined by `\n`.
  - `profile`: an array, present only in multi-profile responses, naming the
    profile whose snapshot verified the source.
  - `unavailable`: a short reason string, given instead of the shown bounds and
    `code`. It covers: the file differs from the snapshot or cannot be read; no
    enclosing callable, or an ambiguous one; or the anchor exceeds 1,536 bytes.
    A profile generation changing during the union fails the whole query.
- Return the entire callable when its lines joined by `\n` fit in 1,536 bytes.
  Otherwise keep the bounded window around `start_line` (up to four whole
  lines on each side) and report `callable_start_line` and `callable_end_line`
  so a reader can distinguish the excerpt from the full declaration.
- Alongside the caller previews, `results.definition` gives the resolved
  subject's complete indexed declaration if it independently fits the same
  byte limit and its file still matches the snapshot. It is never truncated.
  A profile union labels the verifying profile and omits the definition if
  the selector resolves to different subjects.
- `results.evidence_trimmed` is an integer counting distinct exact rows on the
  page that got no entry because 5 were already used. It is omitted when zero.

The window is anchored at the row's declaration start, not at the reference
token, and clamped to the enclosing callable. It need not contain the token.

Nothing is guessed. A row that cannot be verified gets `unavailable` and no
code. No edge is added or inferred, and the evidence never changes `total`,
rows, coverage or cursors.

## Consequences

- One call answers "who calls this, and what does the surrounding code look
  like" for the first five distinct callers on the page. Later callers still
  need `get_source`, and `evidence_trimmed` says how many.
- Default responses are unchanged, byte for byte. The cost is paid only by a
  caller who sets the flag.
- Invalid combinations fail loudly, so an agent cannot believe it received
  previews it did not ask for.
