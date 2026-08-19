---
title: get_file_outline
description: List the declarations under a file or a directory, grouped by file, with the kind and the line range of each and the signature on request.
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
| `response_format` | string | `concise` | `concise` or `detailed`. `detailed` adds `stable_key` and `canonical_identity`, and restores the fully qualified `signature`; in the compact view it is also what puts a signature on a row at all. Any other value is rejected with `INVALID_ARGUMENT`. |
| `view` | string | `compact` | The granularity of the answer, never a different answer. `compact` states the repository, the package and whatever every declaration shares once, and lists the declarations by file as `name@start-end` entries. `full` is the field-per-row shape. `files` answers only which files hold the page's declarations and how many each holds. Any other value is rejected with `INVALID_ARGUMENT`. |

## Answers

This is how to read the shape of code without opening it: what is declared
under a path, of what kind and on which lines.

By default the answer is the `compact` view. `results` is one object stating the
`repository`, the `path` asked about, the `package` when the whole page sits in
one -- `packages` when it does not -- and then whatever every declaration of the
page shares: `kind` when they are all of one kind, `exported` when they are all
visible or all not. `files` groups the declarations by file, and an entry of a
group's `at` is `name@start`, or `name@start-end` when the declaration spans
more than one line, using the qualified name when the row has one. An entry
becomes an array when the page could not hoist a column: the elements after the
label are appended in a fixed order -- the name, the kind, `exported` or
`unexported`, the signature, the stable key, the canonical identity -- skipping
everything the header already states and everything `concise` withholds. There is
no `languages` field: a path's extension says the language.

With `view: "full"`, `results` carries `repository`, `path`, `packages`,
`languages` and `files`, each entry of `files` having a `path` and its `symbols`,
and each symbol row carrying `name`, `kind`, `signature`, `exported`,
`start_line` and `end_line`, plus `qualified_name` only when it differs from
`name`.

Those rows are what the rest of the surface accepts: the repository, the path
of the group and the qualified name are exactly the triple `get_symbol`,
`get_source`, `find_references` and the traversal tools take, so the next call
is built out of the answer you just got. Under
`response_format: "detailed"` each row also carries its stable key. Measured over
`kena`, one directory outline went from `633` to `248` tokens with a single
shared `kind`; a larger directory with no single shared `kind` or `exported`
went from `3.667` to `3.184` once the second grouping tier below applied.

When `kind` and `exported` do not both hoist to the header, `results.groups`
replaces `files`: each entry states its own `kind` and `exported` pair and
holds the declarations that share it, grouped by file exactly like the flat
page. See [when a page groups](/mcp/usage/#when-a-page-groups) for the
mechanism shared by six tools, and the [grouped example](#example-grouped)
below for a captured page.

## Example

```json
{
  "repository": "kivgraph",
  "path": "internal/mcp/instructions.go"
}
```

```json
{"snapshot_id":30,"total":2,"returned":2,"coverage":{"exact":2},"results":{"repository":"kivgraph","path":"internal/mcp/instructions.go","package":"github.com/Luqueee/kivgraph/internal/mcp","kind":"const","exported":false,"files":[{"file":"internal/mcp/instructions.go","at":["staleServerInstructions@36","serverInstructions@21-27"]}]}}
```

Both declarations are unexported constants of the same package, so the header
carries the package, the kind and the visibility and each entry is the bare
label: one line for `staleServerInstructions`, lines 21 to 27 for
`serverInstructions`. Nothing was dropped -- there was nothing left to say per
row.

The same call in the `full` view, the field-per-row shape:

```json
{
  "repository": "kivgraph",
  "path": "internal/mcp/instructions.go",
  "view": "full"
}
```

```json
{"snapshot_id":30,"snapshot_age_ms":9020,"total":2,"returned":2,"truncated":false,"next_cursor":null,"coverage":{"exact":2,"candidate":0,"unresolved_related":0,"package_level":0},"results":{"repository":"kivgraph","path":"internal/mcp/instructions.go","packages":["github.com/Luqueee/kivgraph/internal/mcp"],"languages":["go"],"files":[{"path":"internal/mcp/instructions.go","symbols":[{"name":"staleServerInstructions","kind":"const","signature":"untyped string","exported":false,"start_line":36,"end_line":36},{"name":"serverInstructions","kind":"const","signature":"untyped string","exported":false,"start_line":21,"end_line":27}]}]}}
```

### The `files` view

When the question is which files declare anything, `view: "files"` answers with
the file list and a count each, and nothing about what is declared:

```json
{
  "repository": "kivgraph",
  "path": "internal/mcp/instructions.go",
  "view": "files"
}
```

```json
{"snapshot_id":30,"total":2,"returned":2,"coverage":{"exact":2},"results":{"repository":"kivgraph","path":"internal/mcp/instructions.go","files":[{"file":"internal/mcp/instructions.go","declarations":2}]}}
```

It is the shape of the question over a directory, where the answer is a dozen
files rather than one. `declarations` counts what this page holds for the file,
so on a truncated page it is the page's count and not the file's total.

These responses come from snapshot `30` of two repositories, `kivgraph` and
`mole`.

## Example: grouped

A directory whose declarations do not share one `kind` or one `exported`:

```json
{
  "repository": "mole",
  "path": "internal/admin"
}
```

```json
{
  "snapshot_id": 1,
  "total": 32,
  "returned": 19,
  "coverage": { "exact": 19 },
  "results": {
    "repository": "mole",
    "path": "internal/admin",
    "package": "github.com/Luqueee/mole/internal/admin",
    "groups": [
      {
        "kind": "method",
        "exported": true,
        "files": [{ "file": "internal/admin/admin.go", "at": [
          "Stats.OnConnect@30-33", "Server.WithPortController@89-92",
          "PortController.RemoveDiscover@74", "Server.Handler@95-104",
          "Server.WithPorts@81-84", "Stats.OnDialFail@41-43",
          "Stats.OnDisconnect@36-38", "PortController.AddDiscover@73"
        ] }]
      },
      {
        "kind": "method",
        "exported": false,
        "files": [{ "file": "internal/admin/admin.go", "at": [
          "Server.handleStatus@106-129", "Server.handlePortAdd@150-169",
          "Server.handleHealth@131-134", "Server.handlePortDelete@175-188"
        ] }]
      },
      {
        "kind": "func",
        "exported": true,
        "files": [{ "file": "internal/admin/admin.go", "at": ["New@54-56", "NewStats@25-27"] }]
      },
      {
        "kind": "type",
        "exported": true,
        "files": [{ "file": "internal/admin/admin.go", "at": ["PortController@72-75", "Server@58-63", "Stats@16-22"] }]
      },
      {
        "kind": "type",
        "exported": false,
        "files": [{ "file": "internal/admin/admin.go", "at": ["portRequest@139-141", "snapshot@45-50"] }]
      }
    ]
  }
}
```

Five groups over one file: `repository`, `path` and `package` cover the whole
page and stay in the header as always, but `kind` and `exported` each take four
or more values between them, so neither hoists and every group states its own
pair. Nothing here needed a column beyond the pair -- no group mixed two
signatures or two visibilities inside itself -- so every entry is still the bare
`name@start-end` label; a group with a residual disagreement of its own would
turn an entry into an array exactly as the flat page does.

## Limits

`limit` defaults to 200 and cannot exceed 500. A value outside 1 to 500 is
rejected rather than clamped.

`truncated` is true and `next_cursor` is a token whenever the page did not
exhaust `total`; the compact view omits both when it did. The cursor is an
opaque, checksummed base64url token about 31 characters long -- a binary body,
where the previous version spelled 314 characters of base64-wrapped JSON -- and
it is bound to the snapshot identifier, to the sorting version `stable-key-v1`
and to the query identity: the tool name, `repository`, `path`, `kind` and
`include_members`. Change any of those and the token no longer matches, which
fails as `CURSOR_INVALID`, and so does a token that was edited, truncated,
re-encoded or decodes with trailing bytes after its checksum. A token minted by a
server of the previous cursor version fails closed the same way: the body
declares version 2 and version 1 is never reinterpreted under the new layout. A
token issued against an older generation fails as `CURSOR_SNAPSHOT_EXPIRED`. All
of them mean the pagination has to restart. `view` is not part of the identity,
so one cursor can continue the outline in another view.

`kind` and `include_members` filter the page after it is taken, so `total`
counts every symbol under the path and `returned` counts what survived the
filter. A page can therefore return fewer rows than `limit` and still be
truncated.

A path the graph does not know is an error naming both the repository and the
path, `SYMBOL_NOT_FOUND`, never an empty page. An empty page would read as
"nothing is declared here", which is a different and more misleading answer.

`guidance` is never emitted by this tool. Groups follow the order the page
first mentions each file, and `packages` is sorted, so two calls over the same
page of the same snapshot produce byte-identical responses.

`response_format` accepts `concise` and `detailed`. Concise drops the stable
key, which on a 155-declaration file was half the tokens, and prints the
signature the way the declaring source reads it, with the symbol's own package
path removed. Types from other packages keep their path. `detailed` restores
the key, the full signature and `canonical_identity`. In the compact view the
signature is on a row only under `detailed`: it is the largest field a row can
carry, and a reader choosing between declarations is choosing between names.

## Where it loses

One small file is cheaper to read than to outline: the outline is a second
round trip and it gives you names where the file gives you the code.
`get_file_outline` earns its cost on a directory or a package, where it
replaces a dozen reads with one answer, and when you need the ranges before
deciding which body to fetch with `get_source`.
