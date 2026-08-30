---
title: get_source
description: Read the code of up to twenty symbols in one call, as plain text, with the range re-anchored when the file changed.
---

> The code of several symbols in one call. Prefer it to reading each range: no line numbers, one call across files and repositories.

## Arguments

| Argument | Type | Default | Meaning |
| --- | --- | --- | --- |
| `profile` | array of strings | configured default | Profiles to query. `["*"]` alone selects all. A stable key requires exactly one named profile when several profiles exist. |
| `symbols` | array | none, required | The symbols to read. At least one and at most 20; an empty array or more than 20 is rejected with `INVALID_ARGUMENT`. |
| `symbols[].stable_key` | string | unset | The durable key of one symbol. Exactly one of `stable_key` or `qualified_name` per entry; both is rejected, and a key together with `repository` or `path` is rejected. |
| `symbols[].qualified_name` | string | unset | The qualified name of the symbol, as every row of this surface returns it. |
| `symbols[].repository` | string | unset | Narrows that entry's `qualified_name` to one repository, matched by name. |
| `symbols[].path` | string | unset | Narrows that entry's `qualified_name` to a repository-relative path prefix. It requires `repository`. An absolute path or any `..` segment is rejected. |
| `context_lines` | integer | `0` | Lines kept before and after the declaration. Must be between 0 and 100; anything else is rejected with `INVALID_ARGUMENT`. The window is clamped at the first and last line of the file. |
| `response_format` | string | `concise` | `concise` or `detailed`. `detailed` appends the stable key to each header line. Any other value is rejected with `INVALID_ARGUMENT`. |

## Answers

This is the only tool that answers in prose rather than JSON, and the reason is
a measurement: a 26-line declaration worth 302 tokens of source costs 374
tokens inside a JSON string and 430 as a full row, which is what the host's own
range read costs. Serving code through the envelope buys nothing, so the code
travels as code and the counters travel in the header line.

The first line is `snapshot <id>  <n> bodies  context <n>`. Each body then
opens with `@ <repository> <path>:<start>-<end> <kind> <qualified_name>`,
followed by the bytes, unescaped and unnumbered. A row that has no bytes opens
with `!` instead and names the reason. Files shared by several requested
symbols are read once.

## Example

```json
{
  "symbols": [
    {
      "repository": "kivgraph",
      "path": "internal/facts/facts.go",
      "qualified_name": "MergeAll"
    }
  ],
  "context_lines": 0
}
```

```text
snapshot 30  1 bodies  context 0
@ kivgraph internal/facts/facts.go:516-542 func MergeAll [file changed, re-anchored +0]
func MergeAll(sets []Set) Set {
	merged := Set{
		Repositories: mergeAllBy(sets, func(set Set) []Repository { return set.Repositories },
			func(value Repository) string { return value.Key }),
		Packages: mergeAllBy(sets, func(set Set) []Package { return set.Packages },
			func(value Package) string { return value.Key }),
		Files: mergeAllBy(sets, func(set Set) []File { return set.Files },
			func(value File) string { return value.Key }),
		Symbols: mergeAllBy(sets, func(set Set) []Symbol { return set.Symbols },
			func(value Symbol) string { return value.Key }),
		Evidence: mergeAllBy(sets, func(set Set) []Evidence { return set.Evidence },
			func(value Evidence) string { return value.Key }),
		Edges: mergeAllBy(sets, func(set Set) []Edge { return set.Edges }, edgeIdentityOf),
		Unresolved: mergeAllBy(sets, func(set Set) []UnresolvedReference { return set.Unresolved },
			func(value UnresolvedReference) unresolvedIdentity {
				return unresolvedIdentity{
					repository: value.RepositoryKey,
					file:       value.FileKey, reason: value.Reason,
					requestedPackage: value.RequestedPackage,
					requestedSymbol:  value.RequestedSymbol,
					offset:           value.Start.Offset,
				}
			}),
	}
	merged.Sort()
	return merged
}

```

The response comes from snapshot `30` of two repositories, `kivgraph` and
`go-svc-e`. The `[file changed, re-anchored +0]` marker on the header is the
freshness contract at work: the file on disk no longer hashed to what the
generation recorded, the declaration was found again by name, and it had not
moved.

## Limits

One call reads at most 20 symbols. `context_lines` is capped at 100; above that
you want the file, and the host reads files.

There is no `limit` and no `cursor`, so there is no paging. The size bound is a
262144-byte ceiling on the assembled bodies. When it stops the response short,
the header says so, as `trimmed <n> at the 262144 byte ceiling`, and the
envelope's `truncated` is set. A response that quietly stopped halfway would be
worse than one that says it stopped. `guidance` is never emitted by this tool.

`serve` may read the files of registered repositories only in order to deliver
bytes. Nothing outside a repository is read, and no component of a path may be
a symbolic link; a path that escapes its repository or crosses a symlink yields
no bytes for that row.

Freshness fails open on bytes and closed on claims. When the file still hashes
to the generation's `ContentDigest`, the range served is the graph's own. When
it does not, the file is the authority, because the file is what you will edit:
the declaration is re-anchored by name to the nearest matching line, the served
range is corrected, and the offset is declared in the header as
`[file changed, re-anchored +N]`. Re-anchoring creates no edge and asserts no
graph fact; it only answers "these lines" with the lines the declaration now
occupies.

The re-anchoring can refuse, and it refuses one row at a time. A declaration
that no longer exists in the file, one that now appears twice equally far from
its recorded position, or one that no longer spans as many lines, yields no
bytes for that row. So does a symbol for which the generation records no line
range. Those rows are rendered with the leading `!` and their reason, and every
other row of the same answer still carries its code.

## Where it loses

For one small file, reading the file is cheaper than naming its declarations.
`get_source` wins when the symbols are scattered: several declarations across
several files, or across repositories, arrive in one call and without line
numbers to strip. If you only need to know where something is, the range is
already in the `find_symbol` or `get_file_outline` row and no bytes are needed.
