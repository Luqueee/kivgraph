---
title: find_symbol
description: Find where a symbol is declared, by name, qualified name, prefix or substring, narrowed by kind, repository and path.
---

> Where a symbol is declared, by name, qualified name, prefix or substring. Narrow with kind, repo and path_prefix.

## Arguments

| Argument | Type | Default | Meaning |
| --- | --- | --- | --- |
| `profile` | array of strings | configured default | Profiles to query. `["*"]` alone selects all profiles. |
| `name` | string | none, required | The text to search for. Rejected with `INVALID_ARGUMENT` when it is empty or carries surrounding whitespace. |
| `mode` | string | `exact` | One of `exact`, `qualified_exact`, `prefix`, `substring`. Any other value is rejected with `INVALID_ARGUMENT`. `exact` and `qualified_exact` match a whole interned string; `prefix` and `substring` walk every symbol name in the snapshot. |
| `kind` | string | unset | Keeps only symbols whose `kind` is exactly this string. |
| `repo` | string | unset | Keeps only symbols whose repository name is exactly this string. Naming a derived repository here is a request for it and overrides the default of `include_derived`. |
| `path_prefix` | string | unset | Keeps only symbols whose repository-relative file path starts with this string. Plain prefix matching, not a glob. |
| `include_derived` | boolean | `false` | When false, rows from the derived Rust standard-library repository, whose name starts with `rust:`, are withheld. |
| `limit` | integer | `50` | Rows per page. Must be between 1 and 500; anything else is rejected with `INVALID_ARGUMENT`. |
| `cursor` | string | unset | The opaque `next_cursor` of a previous page of the same query. |
| `response_format` | string | `concise` | `concise` or `detailed`. In the compact view `detailed` adds `stable_key` and `canonical_identity` to each row, and it is the only way a stable key reaches a compact page; in the full view the key is always there and `detailed` adds `canonical_identity`. Any other value is rejected with `INVALID_ARGUMENT`. |
| `view` | string | `compact` | The granularity of the answer, never a different answer. `compact` states what every row shares once and addresses each declaration by `path:line`. `full` is the field-per-row array. This tool answers about declarations and not about files, so `files` is rejected with `INVALID_ARGUMENT`, as is any other value. |

## Answers

This is the entry point when you have a name and nothing else.

By default the answer is the `compact` view: `results` is an object whose header
carries whatever the whole page agrees on -- `name`, `kind`, `exported`,
`repository` -- and whose `symbols` array holds one entry per declaration. An
entry addresses itself with `at`, which is `path:line` under a header that names
the repository and the whole `repository:path:line` triple when the rows come
from more than one. `end` appears only when the declaration does not start and
finish on the same line, `qn` only when the qualified name differs from the name,
`name` only when the header does not carry it and the qualified name does not end
with it, and `sig` only for the kinds a signature tells you how to call:
`function`, `func`, `method` and `class`.

With `view: "full"`, `results` is an array of rows, each carrying `stable_key`,
`name`, `qualified_name`, `kind`, `signature`, `exported`, `repository`,
`file_path`, `start_line` and `end_line`.

Either way every row is addressable: `get_symbol`, `get_source`,
`find_references` and the traversal tools all accept the `repository`, `path`
and `qualified_name` of a row you already have -- which is why the compact view
drops `stable_key`. It was `885` of the `2.293` tokens of one 22-row page over
the private benchmark corpus; the compact page cost `901` before the second grouping tier below and
`773` after it. `find_symbol` publishes no `exact` counter: a declaration lookup
returns declarations rather than resolved relations, so that count could only
repeat `returned`. `coverage` on this tool carries `unresolved_related` alone --
unresolved references that name the same string -- and it is absent entirely
when that counter is zero.

A search that found nothing and reports no uncertainty is claiming the name does
not exist, when it may only mean that whatever declares it was never indexed. So
the answer also carries a `completeness` object, whose `verdict` is `COMPLETE` or
`LOWER_BOUND`; see [the completeness verdict](/mcp/usage/#read-the-answer). What
bounds a declaration lookup is the scopes the index could not read, and the scope
follows the question -- `repo` narrows it to that repository, because a lookup
charged for one unreadable package anywhere in the graph would read `LOWER_BOUND`
on every call of the corpus and the verdict would carry no information. This is
the most frequent call in the surface, so the block is spent where the answer
could be mistaken for proof -- an empty or a truncated page -- and on every lower
bound; a full page of declarations claims no absence and does not carry it.

When `kind` and `exported` do not both hoist to the header -- a page mixing
methods, functions and variables in various combinations of visibility --
`results.groups` replaces `symbols`: each entry states its own `kind` and
`exported` and holds the declarations that share that pair. See
[when a page groups](/mcp/usage/#when-a-page-groups) for the mechanism shared
by six tools, and the [example](#example-grouped) below for a captured page.

## Example

```json
{
  "name": "MergeAll",
  "repo": "kivgraph",
  "limit": 3
}
```

```json
{"snapshot_id":30,"total":1,"returned":1,"results":{"name":"MergeAll","kind":"func","exported":true,"repository":"kivgraph","symbols":[{"at":"internal/facts/facts.go:516","end":542,"sig":"func(sets []github.com/Luqueee/kivgraph/internal/facts.Set) github.com/Luqueee/kivgraph/internal/facts.Set"}]}}
```

One row, so everything except the location is in the header and the entry is the
location plus how to call it. The same call with `"view": "full"` is the
field-per-row shape:

```json
{
  "name": "MergeAll",
  "repo": "kivgraph",
  "limit": 3,
  "view": "full"
}
```

```json
{"snapshot_id":30,"snapshot_age_ms":9019,"total":1,"returned":1,"truncated":false,"next_cursor":null,"coverage":{"exact":0,"candidate":0,"unresolved_related":0,"package_level":0},"results":[{"stable_key":"KHXAWFM5ED2YEIFIEMB5NALA7L7YXNHSUJEBLU7SDAMLGKX24UAA","name":"MergeAll","qualified_name":"MergeAll","kind":"func","signature":"func(sets []github.com/Luqueee/kivgraph/internal/facts.Set) github.com/Luqueee/kivgraph/internal/facts.Set","exported":true,"repository":"kivgraph","file_path":"internal/facts/facts.go","start_line":516,"end_line":542}]}
```

The same query in `substring` mode, which matches anywhere in the unqualified
name:

```json
{
  "name": "Merge",
  "mode": "substring",
  "repo": "kivgraph",
  "limit": 5
}
```

```json
{"snapshot_id":30,"total":3,"returned":3,"results":{"exported":true,"repository":"kivgraph","symbols":[{"at":"internal/facts/facts.go:516","end":542,"name":"MergeAll","kind":"func","sig":"func(sets []github.com/Luqueee/kivgraph/internal/facts.Set) github.com/Luqueee/kivgraph/internal/facts.Set"},{"at":"internal/indexer/full.go:50","name":"PhaseMerge","kind":"const"},{"at":"internal/facts/facts.go:505","end":507,"qn":"Set.Merge","kind":"method","sig":"func(other github.com/Luqueee/kivgraph/internal/facts.Set)"}]}}
```

Three declarations that agree only on their repository and their visibility, so
`name` and `kind` came back down to the rows. `PhaseMerge` carries no `sig`: a
constant's type does not tell you how to call it, and the compact view keeps the
signature for the four kinds where it does. `Set.Merge` carries `qn` instead of
`name`, because the qualified name already ends with the name. The same query in
the `full` view:

```json
{
  "name": "Merge",
  "mode": "substring",
  "repo": "kivgraph",
  "limit": 5,
  "view": "full"
}
```

```json
{"snapshot_id":30,"snapshot_age_ms":21769,"total":3,"returned":3,"truncated":false,"next_cursor":null,"coverage":{"exact":0,"candidate":0,"unresolved_related":0,"package_level":0},"results":[{"stable_key":"KHXAWFM5ED2YEIFIEMB5NALA7L7YXNHSUJEBLU7SDAMLGKX24UAA","name":"MergeAll","qualified_name":"MergeAll","kind":"func","signature":"func(sets []github.com/Luqueee/kivgraph/internal/facts.Set) github.com/Luqueee/kivgraph/internal/facts.Set","exported":true,"repository":"kivgraph","file_path":"internal/facts/facts.go","start_line":516,"end_line":542},{"stable_key":"VTNOLFOCZDSMBNROBRVHV2NM5MKA37B72M2K5LPCUPR3UJG2GGIA","name":"PhaseMerge","qualified_name":"PhaseMerge","kind":"const","signature":"github.com/Luqueee/kivgraph/internal/indexer.ProgressPhase","exported":true,"repository":"kivgraph","file_path":"internal/indexer/full.go","start_line":50,"end_line":50},{"stable_key":"XKK3NUCVCH57YKL36U4SUIL3NB7FLCJ2DTSTUH3YV4Q7EW7E5ZWA","name":"Merge","qualified_name":"Set.Merge","kind":"method","signature":"func(other github.com/Luqueee/kivgraph/internal/facts.Set)","exported":true,"repository":"kivgraph","file_path":"internal/facts/facts.go","start_line":505,"end_line":507}]}
```

A name nobody declares is an empty `symbols` list -- an empty array in the `full`
view -- and not an error. With every counter at zero, `coverage` is absent too:

```json
{
  "name": "ThisSymbolDoesNotExistAnywhere"
}
```

```json
{"snapshot_id":30,"total":0,"returned":0,"results":{"symbols":[]}}
```

The same absence in the `full` view keeps the counters and the nulls:

```json
{"snapshot_id":30,"snapshot_age_ms":21768,"total":0,"returned":0,"truncated":false,"next_cursor":null,"coverage":{"exact":0,"candidate":0,"unresolved_related":0,"package_level":0},"results":[]}
```

These responses come from snapshot `30` of two repositories, `kivgraph`
and `go-svc-e`.


## Example: grouped

A search across two repositories with no name and no view requested, so `mode`
defaults to `exact` -- here widened to `substring` on purpose to produce more
than one kind:

```json
{
  "name": "handle",
  "mode": "substring",
  "limit": 80
}
```

```json
{
  "snapshot_id": 1,
  "total": 8,
  "returned": 8,
  "results": {
    "exported": false,
    "groups": [
      {
        "kind": "method",
        "symbols": [
          { "at": "go-svc-e:internal/clip/server.go:147", "end": 174, "qn": "Server.handleLatest", "sig": "func(w net/http.ResponseWriter, r *net/http.Request)" },
          { "at": "go-svc-e:internal/admin/admin.go:106", "end": 129, "qn": "Server.handleStatus", "sig": "func(w net/http.ResponseWriter, _ *net/http.Request)" },
          { "at": "go-svc-e:internal/admin/admin.go:150", "end": 169, "qn": "Server.handlePortAdd", "sig": "func(w net/http.ResponseWriter, r *net/http.Request)" },
          { "at": "go-svc-e:internal/clip/server.go:89", "end": 145, "qn": "Server.handlePut", "sig": "func(w net/http.ResponseWriter, r *net/http.Request)" },
          { "at": "go-svc-e:internal/admin/admin.go:131", "end": 134, "qn": "Server.handleHealth", "sig": "func(w net/http.ResponseWriter, _ *net/http.Request)" },
          { "at": "go-svc-e:internal/admin/admin.go:175", "end": 188, "qn": "Server.handlePortDelete", "sig": "func(w net/http.ResponseWriter, r *net/http.Request)" }
        ]
      },
      {
        "kind": "variable",
        "symbols": [
          { "at": "kivgraph:web/src/hooks/useFrameRate.ts:19", "end": 28, "qn": "useFrameRate.handle" }
        ]
      },
      {
        "kind": "func",
        "symbols": [
          { "at": "go-svc-e:internal/proxy/proxy.go:44", "end": 88, "name": "handle", "sig": "func(local net.Conn, dial github.com/Luqueee/go-svc-e/internal/proxy.Dialer, remoteAddr string, hooks github.com/Luqueee/go-svc-e/internal/proxy.Hooks, log *log/slog.Logger)" }
        ]
      }
    ]
  }
}
```

All eight declarations are unexported, so `exported` stays in the header and
never repeats on a group or a row. `kind` is the opposite: three values, so it
drops out of the header and each group states its own instead. The `func` group
carries `name` because `handle` is the whole qualified name of a closure with no
receiver to prefix it, while every method above spells `qn` instead, since
`Server.handlePut` is not implied by `handle` alone. `repository` never reaches
the page header at all here, because the page spans both `go-svc-e` and `kivgraph`
and a group carries no `repository` field of its own to hoist into -- so every
row of every group, six-strong or alone, spells its own `repo:` prefix in `at`.

## Limits

`limit` defaults to 50 and cannot exceed 500. A value outside 1 to 500 is
rejected rather than clamped.

`truncated` is true and `next_cursor` is a token whenever the page did not
exhaust `total`; the compact view omits both when it does not. The cursor is an
opaque, checksummed base64url token about 31 characters long -- a binary body,
not the 314 characters of base64-wrapped JSON the previous version spelled -- and
it is bound to the snapshot identifier, to the sorting version `stable-key-v1`
and to the query identity: the tool name, `name`, `mode`, `kind`, `repo` and
`path_prefix`. Change any of those and the token no longer matches, which fails
as `CURSOR_INVALID`, and so does a token edited, truncated, re-encoded or left
with trailing bytes after its checksum. A token minted by a server of the
previous cursor version fails closed the same way: the body declares version 2
and version 1 is never reinterpreted under the new layout. A token issued
against an older generation fails as `CURSOR_SNAPSHOT_EXPIRED`, because the rows
it indexed into no longer exist. All of them mean the same thing for a caller:
restart the pagination. `view` is not part of the identity, so one cursor can
continue a query in another view.

`find_symbol` emits `guidance` where a count alone would mislead -- an empty
page or a truncated one -- and stays silent on a full page of rows, because
fifteen tokens of advice on the most frequent call of the surface is how a
saving becomes a cost. The other signal is `coverage.unresolved_related`: it
counts unresolved
references naming the same string, so a zero-row answer with
`unresolved_related` at zero means the published graph declares no symbol of
that name inside your filters, and a non-zero value means something names it
that the indexer could not resolve to a declaration. It is the only counter
this tool reports. In the compact view a zero counter is not written at all, so
when `unresolved_related` is zero the whole `coverage` object is omitted.

`response_format` accepts `concise` and `detailed`. Concise omits
`canonical_identity`, which is the concatenation of language, repository,
package, qualified name, kind and discriminator, all of which the row already
spells out. In the compact view it also omits `stable_key`, for the same reason:
the `at` of a row plus the header's repository is the triple every tool accepts.

Withholding derived rows is a page decision and never a claim about what was
observed. The edges into `rust:<release>` stay published with their exact
confidence; `include_derived: true` only stops the page from hiding them.

## Where it loses

A rare, unique name in one small repository is cheaper to grep: the answer is
one process and no snapshot has to be current. `find_symbol` earns its cost on
common names, where grep cannot separate the homonyms, and on `prefix` and
`substring` searches across several repositories at once. If you already hold a
row from another tool, you do not need this one at all.
