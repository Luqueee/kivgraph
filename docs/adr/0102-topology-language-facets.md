# ADR 0102: expose repository language facets in topology data

- Status: Accepted
- Date: 2026-09-03
- Issue: #153

## Context

The topology visualizer needs a language filter, but profile composition
records identify repositories and worktrees rather than individual symbols.
The published snapshot already records the languages observed for each logical
repository. Inferring a language from a path, file extension or repository
name would make the filter disagree with the graph's source evidence.

## Decision

Add a sorted, duplicate-free `languages` array to every repository in
`GET /api/v1/topology`. The values are copied from the repository metadata in
the selected generation. A repository declared by configuration but absent
from the snapshot receives an empty array.

The web client treats an empty language set as unavailable data. It does not
invent an `unknown` language or infer one from another field.

## Alternatives

- Infer languages from paths or names: rejected because those are not graph
  evidence.
- Return one language per worktree: rejected because a repository may contain
  several indexed languages and worktrees are source instances, not language
  identities.
- Load symbol-level graph data for filtering: rejected because topology mode
  must remain a compact read model.

## Consequences

- Existing clients can ignore the additive field.
- Topology filtering can be performed locally without loading `LGVB` tiles.
- Empty language metadata remains visibly unavailable rather than misleading.

## Risks

Language strings are snapshot metadata and may lag a mutable checkout until a
new generation is published. The response's generation IDs and refresh state
make that boundary visible.
