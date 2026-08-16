---
title: find_symbol
description: Find where a symbol is declared, by name, qualified name, prefix or substring, narrowed by kind, repository and path.
---

> Where a symbol is declared, by name, qualified name, prefix or substring. Narrow with kind, repo and path_prefix. A unique name in one repository is cheaper to grep.

## Arguments

| Argument | Type | Default | Meaning |
| --- | --- | --- | --- |
| `name` | string | none, required | The text to search for. Rejected with `INVALID_ARGUMENT` when it is empty or carries surrounding whitespace. |
| `mode` | string | `exact` | One of `exact`, `qualified_exact`, `prefix`, `substring`. Any other value is rejected with `INVALID_ARGUMENT`. `exact` and `qualified_exact` match a whole interned string; `prefix` and `substring` walk every symbol name in the snapshot. |
| `kind` | string | unset | Keeps only symbols whose `kind` is exactly this string. |
| `repo` | string | unset | Keeps only symbols whose repository name is exactly this string. Naming a derived repository here is a request for it and overrides the default of `include_derived`. |
| `path_prefix` | string | unset | Keeps only symbols whose repository-relative file path starts with this string. Plain prefix matching, not a glob. |
| `include_derived` | boolean | `false` | When false, rows from the derived Rust standard-library repository, whose name starts with `rust:`, are withheld. |
| `limit` | integer | `50` | Rows per page. Must be between 1 and 500; anything else is rejected with `INVALID_ARGUMENT`. |
| `cursor` | string | unset | The opaque `next_cursor` of a previous page of the same query. |
| `response_format` | string | `concise` | `concise` or `detailed`. `detailed` adds `canonical_identity` to each row. Any other value is rejected with `INVALID_ARGUMENT`. |

## Answers

This is the entry point when you have a name and nothing else. `results` is an
array of rows, each carrying `stable_key`, `name`, `qualified_name`, `kind`,
`signature`, `exported`, `repository`, `file_path`, `start_line` and
`end_line`. Every row is therefore addressable: `get_symbol`, `get_source`,
`find_references` and the traversal tools all accept the `repository`, `path`
and `qualified_name` of a row you already have. `coverage.exact` counts the
rows returned, and `coverage.unresolved_related` counts unresolved references
that name the same string.

## Example

```json
{
  "name": "MergeAll",
  "repo": "kivgraph",
  "limit": 3
}
```

```json
{"snapshot_id":30,"snapshot_age_ms":9019,"total":1,"returned":1,"truncated":false,"next_cursor":null,"coverage":{"exact":1,"candidate":0,"unresolved_related":0,"package_level":0},"results":[{"stable_key":"KHXAWFM5ED2YEIFIEMB5NALA7L7YXNHSUJEBLU7SDAMLGKX24UAA","name":"MergeAll","qualified_name":"MergeAll","kind":"func","signature":"func(sets []github.com/Luqueee/kivgraph/internal/facts.Set) github.com/Luqueee/kivgraph/internal/facts.Set","exported":true,"repository":"kivgraph","file_path":"internal/facts/facts.go","start_line":516,"end_line":542}]}
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
{"snapshot_id":30,"snapshot_age_ms":21769,"total":3,"returned":3,"truncated":false,"next_cursor":null,"coverage":{"exact":3,"candidate":0,"unresolved_related":0,"package_level":0},"results":[{"stable_key":"KHXAWFM5ED2YEIFIEMB5NALA7L7YXNHSUJEBLU7SDAMLGKX24UAA","name":"MergeAll","qualified_name":"MergeAll","kind":"func","signature":"func(sets []github.com/Luqueee/kivgraph/internal/facts.Set) github.com/Luqueee/kivgraph/internal/facts.Set","exported":true,"repository":"kivgraph","file_path":"internal/facts/facts.go","start_line":516,"end_line":542},{"stable_key":"VTNOLFOCZDSMBNROBRVHV2NM5MKA37B72M2K5LPCUPR3UJG2GGIA","name":"PhaseMerge","qualified_name":"PhaseMerge","kind":"const","signature":"github.com/Luqueee/kivgraph/internal/indexer.ProgressPhase","exported":true,"repository":"kivgraph","file_path":"internal/indexer/full.go","start_line":50,"end_line":50},{"stable_key":"XKK3NUCVCH57YKL36U4SUIL3NB7FLCJ2DTSTUH3YV4Q7EW7E5ZWA","name":"Merge","qualified_name":"Set.Merge","kind":"method","signature":"func(other github.com/Luqueee/kivgraph/internal/facts.Set)","exported":true,"repository":"kivgraph","file_path":"internal/facts/facts.go","start_line":505,"end_line":507}]}
```

A name nobody declares is an empty array, not an error:

```json
{
  "name": "ThisSymbolDoesNotExistAnywhere"
}
```

```json
{"snapshot_id":30,"snapshot_age_ms":21768,"total":0,"returned":0,"truncated":false,"next_cursor":null,"coverage":{"exact":0,"candidate":0,"unresolved_related":0,"package_level":0},"results":[]}
```

These three responses come from snapshot `30` of two repositories, `kivgraph`
and `mole`.

## Limits

`limit` defaults to 50 and cannot exceed 500. A value outside 1 to 500 is
rejected rather than clamped.

`truncated` is true and `next_cursor` is a token whenever the page did not
exhaust `total`. The cursor is opaque and checksummed, and it is bound to the
snapshot identifier, to the sorting version `stable-key-v1` and to the query
identity: the tool name, `name`, `mode`, `kind`, `repo` and `path_prefix`.
Change any of those and the token no longer matches, which fails as
`CURSOR_INVALID`. A token issued against an older generation fails as
`CURSOR_SNAPSHOT_EXPIRED`, because the rows it indexed into no longer exist.
Both mean the same thing for a caller: restart the pagination.

`find_symbol` never emits `guidance`. That field belongs to the reference and
traversal tools, where a zero count reads as an absence and needs a sentence to
say so. Here the signal is `coverage.unresolved_related`: it counts unresolved
references naming the same string, so a zero-row answer with
`unresolved_related` at zero means the published graph declares no symbol of
that name inside your filters, and a non-zero value means something names it
that the indexer could not resolve to a declaration.

`response_format` accepts `concise` and `detailed`. Concise omits
`canonical_identity`, which is the concatenation of language, repository,
package, qualified name, kind and discriminator, all of which the row already
spells out.

Withholding derived rows is a page decision and never a claim about what was
observed. The edges into `rust:<release>` stay published with their exact
confidence; `include_derived: true` only stops the page from hiding them.

## Where it loses

A rare, unique name in one small repository is cheaper to grep: the answer is
one process and no snapshot has to be current. `find_symbol` earns its cost on
common names, where grep cannot separate the homonyms, and on `prefix` and
`substring` searches across several repositories at once. If you already hold a
row from another tool, you do not need this one at all.
