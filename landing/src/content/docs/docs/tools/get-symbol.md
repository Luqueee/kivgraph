---
title: get_symbol
description: Read one symbol's package, module, signature, visibility and line range, by stable key or by repository, path and qualified name.
---

> One symbol's package, signature, visibility and line range, by stable key or by repository, path and qualified name.

## Arguments

| Argument | Type | Default | Meaning |
| --- | --- | --- | --- |
| `stable_key` | string | unset | The durable key of one symbol. Exactly one of `stable_key` or `qualified_name` is required; passing both is rejected. Passing it together with `repository` or `path` is also rejected, because a key already names one symbol. |
| `qualified_name` | string | unset | The qualified name of the symbol, as every row of this surface returns it. Exactly one of `stable_key` or `qualified_name` is required. |
| `repository` | string | unset | Narrows a `qualified_name` to one repository, matched by name. A repository the published graph does not hold is rejected with `REPOSITORY_NOT_FOUND`. |
| `path` | string | unset | Narrows a `qualified_name` to a repository-relative path prefix. It requires `repository`. An absolute path or any `..` segment is rejected; a trailing `/` is trimmed. |
| `response_format` | string | `concise` | `concise` or `detailed`. `detailed` adds `canonical_identity` and `repository_key`. Any other value is rejected with `INVALID_ARGUMENT`. |

Any of the four selector fields carrying surrounding whitespace is rejected
with `INVALID_ARGUMENT` naming the field.

## Answers

This is the detail view of one symbol. `results` is a single object, not an
array: `stable_key`, `repository`, `repository_path`, `package_name`,
`module_path`, `file_path`, `name`, `qualified_name`, `kind`, `signature`,
`exported`, `start_line` and `end_line`. Compared with a `find_symbol` row it
adds the package, the module path and the absolute repository path, which is
what you need to tell two same-named declarations apart by where they live.
`total` and `returned` are always 1.

## Example

```json
{
  "repository": "kivgraph",
  "path": "internal/facts/facts.go",
  "qualified_name": "MergeAll"
}
```

```json
{"snapshot_id":30,"snapshot_age_ms":9019,"total":1,"returned":1,"truncated":false,"next_cursor":null,"coverage":{"exact":0,"candidate":0,"unresolved_related":0,"package_level":0},"results":{"stable_key":"KHXAWFM5ED2YEIFIEMB5NALA7L7YXNHSUJEBLU7SDAMLGKX24UAA","repository":"kivgraph","repository_path":"/Users/adria/Documents/programacion/projects/kivgraph","package_name":"github.com/Luqueee/kivgraph/internal/facts","module_path":"github.com/Luqueee/kivgraph","file_path":"internal/facts/facts.go","name":"MergeAll","qualified_name":"MergeAll","kind":"func","signature":"func(sets []github.com/Luqueee/kivgraph/internal/facts.Set) github.com/Luqueee/kivgraph/internal/facts.Set","exported":true,"start_line":516,"end_line":542}}
```

A qualified name on its own searches the whole graph. Here `Set.Merge` names
exactly one symbol, so it resolves without narrowing:

```json
{
  "qualified_name": "Set.Merge"
}
```

```json
{"snapshot_id":30,"snapshot_age_ms":21768,"total":1,"returned":1,"truncated":false,"next_cursor":null,"coverage":{"exact":0,"candidate":0,"unresolved_related":0,"package_level":0},"results":{"stable_key":"XKK3NUCVCH57YKL36U4SUIL3NB7FLCJ2DTSTUH3YV4Q7EW7E5ZWA","repository":"kivgraph","repository_path":"/Users/adria/Documents/programacion/projects/kivgraph","package_name":"github.com/Luqueee/kivgraph/internal/facts","module_path":"github.com/Luqueee/kivgraph","file_path":"internal/facts/facts.go","name":"Merge","qualified_name":"Set.Merge","kind":"method","signature":"func(other github.com/Luqueee/kivgraph/internal/facts.Set)","exported":true,"start_line":505,"end_line":507}}
```

A name the narrowing excluded fails, and the message says how to widen it:

```json
{
  "repository": "kivgraph",
  "qualified_name": "NoSuchThing"
}
```

```text
SYMBOL_NOT_FOUND: qualified name "NoSuchThing" was not found under kivgraph; call it without repository and path to search the whole graph
```

These three responses come from snapshot `30` of two repositories, `kivgraph`
and `mole`.

## Limits

There is no `limit` and no `cursor`: the answer is one symbol, so `total` and
`returned` are 1, `truncated` is false and `next_cursor` is null. `guidance` is
never emitted; it belongs to the tools whose counts can mislead.

`response_format` accepts `concise` and `detailed`. Concise omits the two
derived identifiers, `canonical_identity` and `repository_key`, whose value the
name and the path beside them already spell out.

`SYMBOL_NOT_FOUND` has two shapes, because a name nobody declares and a name
the narrowing excluded need different fixes. With no `repository` and no
`path`, the message is only that the qualified name was not found. With either
of them set, it names where it looked and tells you to call again without them,
as the capture above shows. A `stable_key` that resolves to nothing reports the
key itself.

A qualified name that matches more than one symbol is `AMBIGUOUS_SYMBOL`, never
a silent pick. What the message offers depends on what is left to narrow. With
no `repository` and no `path`, it names each candidate by where it is, as
`repository path:start-end`, because that is a narrowing you can express in the
next call. Once both `repository` and `path` were given and the name still
matches twice, only the key separates them, and the message lists the stable
keys.

Other codes this tool can return: `INVALID_ARGUMENT` for a malformed or
double selector, and `REPOSITORY_NOT_FOUND` for a repository outside the
published graph.

## Where it loses

If you got the row from `find_symbol` or `get_file_outline`, you already have
the name, the kind, the signature and the range; `get_symbol` adds only the
package, the module path and the absolute repository path, and a second call
for those three fields is rarely worth it. For a single small file, opening the
file is cheaper than resolving a selector against the graph.
