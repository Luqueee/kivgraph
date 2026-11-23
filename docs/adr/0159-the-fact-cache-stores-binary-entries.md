# ADR 0159: The fact cache stores binary entries

## Status

Accepted. Changes the files of the fact cache; no schema, stable key, tool or
configuration key changes. The first pass after an update analyses everything
once.

## Context

A pass served from the fact cache spent most of its CPU decoding it. On a
registry of sixty repositories (`543,631` symbols, `2,020,464` edges) the entries
were `1,516 MB` of JSON: `7.1 s` of CPU to decode, `3.8 GB` allocated, and `1.6 GB`
of strings alive once decoded -- the largest thing a pass held when it merged
them. `encoding/json` was `12%` of the CPU of a warm pass and the directory was
the size of the graph several times over.

Measured on those entries, decoded sequentially:

| format | size | decode CPU | allocated | alive |
|---|---:|---:|---:|---:|
| `encoding/json` | `1,516 MB` | `7.10 s` | `3,765 MB` | `1,641 MB` |
| json, `GOEXPERIMENT=jsonv2` | `1,516 MB` | `3.20 s` | `3,275 MB` | `1,155 MB` |
| `encoding/gob` | `1,046 MB` | `1.08 s` | `6,575 MB` | `579 MB` |
| dictionary and varints | `384 MB` | `0.97 s` | `858 MB` | `385 MB` |

## Decision

An entry is a binary file: a magic, a fingerprint of its structure, the unit in
the clear, a table of every distinct string of the entry, and the entry field by
field with numbers as varints. A string costs its table slot once and a varint
each time it is used; the table is decoded as one allocation and every string is
a piece of it.

The format follows the Go type of `cacheEntry` by reflection, so a field added to
a fact is stored without anybody remembering to say so. That is what the
fingerprint is for: it hashes the names, order and kinds of every exported field
the entry reaches, and an entry whose fingerprint is not the build's is refused as
incompatible instead of decoded into the wrong fields. A type the format cannot
store -- a pointer, an interface, a float, a struct of no byte -- panics when the
plan is built, which a test does, and not on the first hit that reaches it.

A nil slice and an empty one are stored apart, as JSON told them apart: the
verify mode compares an entry with a fresh analysis by what they marshal to.

Decoding is strict. Every length is checked against the bytes that remain divided
by the least a value of its type can take, so a damaged length cannot make the
decoder allocate what the file could not have filled; every string index is in the
table; a bool is `0` or `1`; a number fits its type; bytes left over are an error.
A refusal is a cache miss, as a malformed JSON entry was.

The files end in `.entry`. The JSON entries ended in `.json` and can never be
served again, so `prune` removes a file named as one -- a lower case SHA-256 and
the suffix -- at once, and not when it ages out in thirty days.

Retention reads the unit of an entry from the header, which holds it in the clear,
where it used to match the first `4 KiB` of the JSON.

## Consequences

- The cache of that registry is `367 MB`, not `1.5 GB`.
- A warm pass is `7%` less CPU (`93.4` to `87.1 s`) and `4%` less wall time
  (`43.8` to `41.9 s`); the footprint of the cache loading falls by `1` to `2 GB`
  while it runs. The peak of the whole pass does not move: it is later, in the
  snapshot build and the engine, after the rebuild has let go of the facts.
- The first pass after an update misses every entry and analyses the registry
  once. A cold pass costs what it did (`175 s` against `175 s` here).
- The published graph is the same: its content digest is identical.
- A change to the facts needs no version bump to invalidate the cache, and a
  change that keeps their shape does not invalidate it. What the facts mean is
  still `cacheEntryVersion`.
- Two builds of one source tree write the same bytes: strings are numbered in
  order of first use and maps are written in key order.

## Verification

Round trip of every kind of value, byte determinism over map order, every
truncation and every flipped byte of an entry, lengths the file could not hold
(checked against the memory a refusal allocates), strings outside the table, a bool
of `2`, trailing bytes, another structure, the plan of types the format refuses,
and `5` million executions of a fuzz target. Mutations of each check fail a test.
By hand, over the registry that measured it: `index --full` twice, the second served
`48` entries from the cache (the other `16` units never store, because their module
did not load, and were analysed as before) and published a graph with the digest of
the one the JSON format published.

Not measured: the cost of writing an entry, which is on the cold path only
(`1.8 s` of CPU for the `48` entries in an experiment); another operating system.
