---
title: Token-Efficient Code Understanding for AI Agents
description: Reduce codebase exploration and MCP tool calls with a local semantic knowledge graph for AI coding agents.
---

AI coding agents often build context by scanning files with `grep`, `glob` and repeated reads. On a large repository that can mean many tool calls before the agent has identified the relevant symbols and relationships.

Kivgraph provides **token-efficient code understanding** by indexing the repository first and returning structured graph facts: declarations, callers, callees, dependencies, impact and source code.

## Fewer exploratory calls

A typical structural question can begin with one graph query instead of a sequence of text searches and file reads:

```text
What breaks if I change MergeAll?
```

The agent can use [`get_blast_radius`](/docs/tools/get-blast-radius/) to receive bounded impact, grouped by repository, package, depth and relation kind. It can then call [`get_source`](/docs/tools/get-source/) only for the symbols that matter.

Fewer calls do not mean less evidence. Each result keeps its repository, path, symbol, line range, confidence and provenance where applicable.

## Token saving should be measured

The amount saved depends on the question, repository, language and response view. A broad exploratory task and a rare symbol lookup do not have the same baseline. Kivgraph's benchmark reports the comparison rather than promising one universal percentage.

The useful comparison is:

| Approach | First step | Common cost |
| --- | --- | --- |
| Text exploration | Search, open files, follow names manually | Repeated context and tool calls |
| Code graph query | Ask for symbols or relationships | Structured rows, then targeted source |

Kivgraph also exposes compact and file-oriented response views where supported, so an agent can request the amount of detail the task needs.

## Local context and privacy

The index and MCP server run locally. Repository contents are not sent to a hosted search service by Kivgraph. This makes token-efficient retrieval useful for private codebases as well as public projects.

See the [comparison](/comparison/) for measured tradeoffs and the [Quickstart](/quickstart/) to build the first published graph.
