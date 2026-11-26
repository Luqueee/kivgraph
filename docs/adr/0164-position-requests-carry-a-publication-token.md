# ADR 0164: Position requests carry a publication token, not a snapshot ID

## Status

Accepted. Supersedes the `snapshot_id` field of ADR 0161. The rest of ADR 0161
(selector rules, innermost-callable resolution, digest check, byte ceiling)
is unchanged.

## Context

A position entry (`repository`, `path`, `line`) is meaningful only in the
generation it was read from. ADR 0161 carried that generation as the numeric
`snapshot_id`.

A numeric ID is not a publication identity. Rebuilding the graph can reuse an
ID, so an old position read before a rebuild would match a new snapshot and be
served against the wrong line. A process-local identity fails the other way: a
remap of the same publication in a new process would be refused although the
graph did not change.

The cache header preserves `CreatedAt` across remaps, but derivation from the
canonical graph sets a new build time. Build time cannot identify the stored
generation when the cache is missing or refused.

## Decision

A single-profile `find_references` answer carries `publication` when
`response_format` is `detailed` or `include_evidence` is true. Ordinary
reference lists omit it to avoid charging callers for a position lookup they
did not request. Position entries copy it back as `publication`:

```json
{
  "repository": "kivgraph",
  "path": "internal/facts/facts.go",
  "line": 530,
  "publication": "3-1dk8sq0g9x5k"
}
```

The token is opaque. For a stored generation it combines its numeric ID, the
canonical graph digest and the modification time of its immutable generation
marker (`snapshot.sha256`). The marker predates the optional snapshot file.
Its contents are table counts and are **not** evidence of graph identity.

- Mapping uses the verified content digest; derivation computes that same
  digest from the canonical rows. Neither trusts a refused cache header.
- The same publication keeps its token across processes, even without a cache.
  Replacing the marker can invalidate a position without changing the graph.
  Without a marker, identical graphs with the same ID are indistinguishable;
  different content still invalidates the position.
- A missing token is `INVALID_ARGUMENT`. A supplied token that does not match
  is `SNAPSHOT_UNAVAILABLE`: repeat the reference query for a fresh position.
- `snapshot_id` remains on responses for all tools. It is no longer accepted in
  a position entry. No compatibility shim exists: old-format position requests
  are invalid.
- Multi-profile answers carry no `publication`, because a position selects one
  profile.

## Consequences

- Callers copy `publication` and `line` from the same reference answer, as
  before with `snapshot_id`.
- A changed graph invalidates the token even when its numeric ID is reused.
- Evidence previews use the snapshot retained by the query, even if the store
  publishes another one before rendering. The source bytes must still match
  the retained snapshot's indexed digest.
