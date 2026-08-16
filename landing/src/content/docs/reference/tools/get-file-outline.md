---
title: get_file_outline
description: List the declarations under a file or a directory, grouped by file, with kind, signature and line range.
---

> Declarations under a path, grouped by file, with kind, signature and range. Use it for a package; one small file is cheaper to read.

## Arguments

| Argument | Type | Default | Meaning |
| --- | --- | --- | --- |
| `repository` | string | none, required | The repository name. Rejected with `INVALID_ARGUMENT` when empty or padded with whitespace, and with `REPOSITORY_NOT_FOUND` when the published graph does not hold it. |
| `path` | string | none, required | A repository-relative file, or a directory whose files are all wanted. An absolute path is rejected, and so is any `..` segment; a trailing `/` is trimmed. |
| `kind` | string | unset | Keeps only rows whose `kind` is exactly this string. |
| `include_members` | boolean | `false` | Adds the rows that belong to the declaration above them: `field`, `property`, `enum_member` and `variant`. They are off by default because on a real file they are about half the payload. |
| `limit` | integer | `200` | Symbols per page. Must be between 1 and 500; anything else is rejected with `INVALID_ARGUMENT`. |
| `cursor` | string | unset | The opaque `next_cursor` of a previous page of the same query. |
| `response_format` | string | `concise` | `concise` or `detailed`. `detailed` adds `stable_key` and `canonical_identity`, and restores the fully qualified `signature`. Any other value is rejected with `INVALID_ARGUMENT`. |

## Answers

This is how to read the shape of code without opening it: what is declared
under a path, of what kind, with what signature and on which lines. `results`
is one object with `repository`, `path`, `packages`, `languages` and `files`.
Each entry of `files` has a `path` and its `symbols`, so the path is written
once per group instead of once per row. Each symbol row carries `name`, `kind`,
`signature`, `exported`, `start_line` and `end_line`, plus `qualified_name`
only when it differs from `name`.

Those rows are what the rest of the surface accepts: the repository, the path
of the group and the qualified name are exactly the triple `get_symbol`,
`get_source`, `find_references` and the traversal tools take, so the next call
is built out of the answer you just got. Under
`response_format: "detailed"` each row also carries its stable key.

## Example

```json
{
  "repository": "kivgraph",
  "path": "internal/mcp/instructions.go"
}
```

```json
{"snapshot_id":30,"snapshot_age_ms":9020,"total":2,"returned":2,"truncated":false,"next_cursor":null,"coverage":{"exact":2,"candidate":0,"unresolved_related":0,"package_level":0},"results":{"repository":"kivgraph","path":"internal/mcp/instructions.go","packages":["github.com/Luqueee/kivgraph/internal/mcp"],"languages":["go"],"files":[{"path":"internal/mcp/instructions.go","symbols":[{"name":"staleServerInstructions","kind":"const","signature":"untyped string","exported":false,"start_line":36,"end_line":36},{"name":"serverInstructions","kind":"const","signature":"untyped string","exported":false,"start_line":21,"end_line":27}]}]}}
```

The response comes from snapshot `30` of two repositories, `kivgraph` and
`mole`.

## Limits

`limit` defaults to 200 and cannot exceed 500. A value outside 1 to 500 is
rejected rather than clamped.

`truncated` is true and `next_cursor` is a token whenever the page did not
exhaust `total`. The cursor is opaque and checksummed, and it is bound to the
snapshot identifier, to the sorting version `stable-key-v1` and to the query
identity: the tool name, `repository`, `path`, `kind` and `include_members`.
Change any of those and the token no longer matches, which fails as
`CURSOR_INVALID`. A token issued against an older generation fails as
`CURSOR_SNAPSHOT_EXPIRED`. Both mean the pagination has to restart.

`kind` and `include_members` filter the page after it is taken, so `total`
counts every symbol under the path and `returned` counts what survived the
filter. A page can therefore return fewer rows than `limit` and still be
truncated.

A path the graph does not know is an error naming both the repository and the
path, `SYMBOL_NOT_FOUND`, never an empty page. An empty page would read as
"nothing is declared here", which is a different and more misleading answer.

`guidance` is never emitted by this tool. Groups follow the order the page
first mentions each file, and `packages` and `languages` are sorted, so two
calls over the same page of the same snapshot produce byte-identical responses.

`response_format` accepts `concise` and `detailed`. Concise drops the stable
key, which on a 155-declaration file was half the tokens, and prints the
signature the way the declaring source reads it, with the symbol's own package
path removed. Types from other packages keep their path. `detailed` restores
the key, the full signature and `canonical_identity`.

## Where it loses

One small file is cheaper to read than to outline: the outline is a second
round trip and it gives you names where the file gives you the code.
`get_file_outline` earns its cost on a directory or a package, where it
replaces a dozen reads with one answer, and when you need the ranges before
deciding which body to fetch with `get_source`.
