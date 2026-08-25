---
title: Cross-Repository Code Graph MCP Server
description: Build a local semantic code graph across multiple repositories and query cross-repository consumers, dependencies and change impact with Kivgraph.
---

**Kivgraph is a local cross-repository code intelligence MCP server for AI coding agents.** Register multiple repositories in one workspace and query their proven symbol and dependency relationships without collapsing their identities.

This is a **semantic code graph across repositories**. It answers relationships observed by the language analyzers: symbols, references, callers, callees, packages and consumers in another repository.

## What the workspace graph answers

Use Kivgraph to ask:

- Which repository consumes this symbol?
- Who calls this function across the workspace?
- What dependencies reach this package?
- What breaks if I change this shared type?
- Which files and repositories are in the bounded impact radius?

The relevant MCP tools are [`find_cross_repo_consumers`](/docs/tools/find-cross-repo-consumers/), [`find_references`](/docs/tools/find-references/), [`trace_dependencies`](/docs/tools/trace-dependencies/) and [`get_blast_radius`](/docs/tools/get-blast-radius/).

## Register repositories explicitly

Each repository gets an explicit name and path:

```bash
kivgraph init \
  --repository frontend=/workspace/frontend \
  --repository api=/workspace/api \
  --repository shared=/workspace/shared \
  --languages go,typescript,rust
kivgraph index --full
```

The repository identity travels with the graph facts. A cross-repository consumer is not confused with a local caller, and every returned row carries the repository and repository-relative path needed for the next query.

## What that is worth, measured

One question from the published benchmark asks which files outside
`platform-lib` consume the `StatusCode` enum it declares. Three files do.
`find_cross_repo_consumers` returned all three in one call for 530 tokens, and
reported the 22 package-level rows separately rather than counting them as
uses. `grep` spent 12,200 tokens over seven calls and found two of the three:
the consumer in `client-sdk:src/index.ts` never spells the symbol, so no
text search reaches it.

The benchmark corpus is private, so the repository, file and symbol names here
are substituted. The counts, the calls and the token figures are the measured
ones.

That margin is not universal. On five of the 29 questions `grep` is the cheaper
arm, all of them single-repository lookups of a rare name. Read the
[comparison](/comparison/) before assuming the graph is always the shorter
path.

## Semantic code graph versus architecture graph

Kivgraph focuses on **code relationships**: declarations, references, calls, imports, packages and analyzer-backed dependencies. It is not an architecture-observability product that automatically promises to infer every HTTP, gRPC, Kafka or database runtime flow.

That boundary is deliberate. Kivgraph publishes `EXACT`, `CANDIDATE` and `UNRESOLVED` results with confidence and provenance instead of turning a plausible service name into a proven edge.

For the broader query model, read [repository relationships](/repository-relationships/) and [code intelligence](/code-intelligence/).
