---
title: get_index_status
description: >-
  Returns progress and the terminal result for an asynchronous Kivgraph index
  operation.
---

> Status or result for a start_index_project operation.

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

`completed` carries the outcome under `result`, which is the full result of
synchronous `index_project`: `project`, `projects`, `generation_id`,
`snapshot_id`, `counts` and `index`. `failed` carries `failure`, an object with
`code`, always `INDEXING_FAILED`, and `message`, which names the observed cause.
`started_at` and `updated_at` are RFC 3339 timestamps with nanoseconds, and
`progress` is present once the operation has reported a step and holds the last
`phase`, `repository`, `detail`, `completed` and `total` it reported, each
omitted when empty or zero. A failed job is a successful status lookup, so the
failure is carried in the response rather than turning the lookup itself into an
MCP error.

The tool is read-only. Polling it never registers a repository, starts another
index, or changes the published generation.
