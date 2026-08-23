---
title: MCP tools
description: The eleven tools Kivgraph registers over stdio, and which question each one answers.
---

`kivgraph serve` registers eleven tools over stdio. Ten are read-only.
`index_project` is the only one that mutates anything, and only after explicit
consent.

Every tool answers from the published HotSnapshot. `serve` does not open the
database and does not run the TypeScript worker.

## Pick the tool by the question

| The question | The tool |
| --- | --- |
| Who calls this, what references this | [`find_references`](/reference/tools/find-references/) |
| What breaks if I change this | [`get_blast_radius`](/reference/tools/get-blast-radius/) |
| What does this reach outwards | [`trace_dependencies`](/reference/tools/trace-dependencies/) |
| Who uses it from another repository | [`find_cross_repo_consumers`](/reference/tools/find-cross-repo-consumers/) |
| Where is it declared | [`find_symbol`](/reference/tools/find-symbol/) |
| What is declared in this package | [`get_file_outline`](/reference/tools/get-file-outline/) |
| Give me the code of these symbols | [`get_source`](/reference/tools/get-source/) |
| Everything known about one symbol | [`get_symbol`](/reference/tools/get-symbol/) |
| What is indexed, and is it current | [`graph_status`](/reference/tools/graph-status/) |
| Which repositories are registered | [`list_repositories`](/reference/tools/list-repositories/) |
| Index a project and rebuild | [`index_project`](/reference/tools/index-project/) |

Edges are resolved by `go/types`, the TypeScript checker and `rust-analyzer`,
not by matching names. An empty reference list means nobody calls it, and two
homonymous methods are two different symbols. Grep can say neither.

See [Using it from an agent](/mcp/usage/) for the response envelope, the cursor
contract, and how to read `coverage`, `guidance` and `completeness`.

## Reference

Read-only, symbols and source:

- [`find_symbol`](/reference/tools/find-symbol/)
- [`get_symbol`](/reference/tools/get-symbol/)
- [`get_source`](/reference/tools/get-source/)
- [`get_file_outline`](/reference/tools/get-file-outline/)

Read-only, graph traversal:

- [`find_references`](/reference/tools/find-references/)
- [`find_cross_repo_consumers`](/reference/tools/find-cross-repo-consumers/)
- [`trace_dependencies`](/reference/tools/trace-dependencies/)
- [`get_blast_radius`](/reference/tools/get-blast-radius/)

Read-only, the index itself:

- [`list_repositories`](/reference/tools/list-repositories/)
- [`graph_status`](/reference/tools/graph-status/)

Mutating:

- [`index_project`](/reference/tools/index-project/)

## Addressing a symbol

Every tool that takes a symbol accepts either a `stable_key` or the triple
`repository`, repository-relative `path` and `qualified_name` — exactly one of
the two. Supplying both is rejected rather than resolved quietly, because two
selectors can disagree and answering one of them answers a question nobody
asked.

Every row a tool returns already carries a repository, a path, a qualified name
and a line range, so the next call is built from the answer just received and
stable keys never have to enter the conversation.

## Before a generation is published

With no published generation there is no query surface. The server completes
the handshake, publishes only `index_project`, and puts the rebuild command in
its `instructions`.

A client launches the process itself, so exiting would read as a crash, and
publishing tools that answer `INDEX_NOT_READY` to everything would teach the
agent that the tools do not work. See
[Troubleshooting](/mcp/troubleshooting/).

## One channel per response

No tool publishes an `outputSchema`. With one, the SDK marshals the typed
result into `structuredContent` and repeats the same JSON in the text block, so
the response is paid for twice.

[`get_source`](/reference/tools/get-source/) goes further and answers in prose:
source inside a JSON string costs more than the same source as text.
