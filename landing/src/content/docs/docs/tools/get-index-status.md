---
title: get_index_status
description: >-
  Returns progress and the terminal result for an asynchronous Kivgraph index
  operation.
---

> Polls an index started by
> [`start_index_project`](/docs/tools/start-index-project/) without starting or
> changing any graph operation.

## Arguments

| Argument | Type | Meaning |
| --- | --- | --- |
| `operation_id` | string | The exact opaque ID returned by `start_index_project`. |

An empty, malformed, unknown, or expired value returns `INVALID_ARGUMENT`.

## Working response

```json
{
  "operation_id": "5e15bc5e34e84f1d9ff8da70ee33425f",
  "status": "working",
  "started_at": "2026-09-05T16:00:00Z",
  "updated_at": "2026-09-05T16:00:08Z",
  "progress": {
    "phase": "go",
    "repository": "kivgraph",
    "completed": 1,
    "total": 3
  }
}
```

## Terminal responses

`completed` includes the same generation, snapshot, and language counts returned
by synchronous `index_project`. `failed` includes a stable `INDEXING_FAILED`
code and the observed cause. A failed job is a successful status lookup, so the
failure is carried in the response rather than turning the lookup itself into an
MCP error.

The tool is read-only. Polling it never registers a repository, starts another
index, or changes the published generation.
