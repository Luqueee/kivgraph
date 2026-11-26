# ADR 0161: get_source reads the callable at a file position

## Status

Accepted. Adds an optional entry shape to `get_source`. The existing selectors,
the 20-symbol limit and the 262144-byte ceiling are unchanged. The
`snapshot_id` field of this entry is replaced by `publication` (ADR 0164).

## Context

`get_source` names a symbol by stable key, or by `repository`, `path` and
`qualified_name`. Those are the identifiers the graph returns, so they serve a
body the graph already showed.

The compact incoming references to Ky's `getContentLength` identify local
variables: `totalBytes@210` and `totalBytes@231` in `source/utils/body.ts`.
Even the fully qualified variable name and file do not identify one binding.
The question is about the functions around the two lines, not the variables.

## Decision

A `symbols[]` entry may name a position instead of a symbol:

```json
{
  "repository": "kivgraph",
  "path": "internal/facts/facts.go",
  "line": 530,
  "publication": "3-1dk8sq0g9x5k"
}
```

- `repository`, `path`, `line` and `publication` are required together in each
  entry. `line` is one-based and positive. `publication` is the token the
  reference answer carried (ADR 0164), per entry.
- A position entry is exclusive with `stable_key` and `qualified_name`. Mixing
  them in one entry is rejected, not resolved by precedence.
- A position entry selects one profile, as a stable key does when several
  profiles exist.
- The entry resolves to the innermost indexed callable whose recorded range
  contains the line, and serves its body with the same header, `context_lines`
  and byte ceiling as any named symbol.

The selector fails closed. It never guesses:

- A `publication` other than the published snapshot's token is refused. A line
  only means something in the publication it was read from.
- A line that no indexed callable contains, or that two equally innermost
  callables both contain, is refused. Choosing between them would be a guess.
- A file that no longer hashes to the generation's `ContentDigest` is refused
  for a position. Re-anchoring (ADR 0040) works from a declaration's name; a
  position has no name, and the line itself is what drifted.

A selector error aborts the batch. For TypeScript and JavaScript variables,
the digest-verified syntax tree must show a direct arrow or function
initializer matching the indexed binding's name and range. An arbitrary
variable that happens to hold a callable value does not qualify.

## Consequences

- The caller copies the position and publication from the same reference
  answer. No extra symbol discovery is needed.
- The answer is as fresh as the publication. After a re-index, the caller takes
  the new `publication` and the new line. An old pair is refused, not silently
  remapped.
- Existing requests keep their meaning. Only entries carrying `line` take the
  new path.
- The header keeps the declaration range after the path. When the returned
  excerpt differs, `[shown first-last]` gives its actual inclusive range,
  clamped at the file boundaries. Do not count overlapping context twice.
