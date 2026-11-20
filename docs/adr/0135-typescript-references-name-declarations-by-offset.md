# ADR 0135: TypeScript references name the declarations they reach by offset

## Status

Accepted. Adds two optional fields to the `ts-facts-v5` references.

## Context

A TypeScript reference travelled from the worker to the graph as a qualified
name and a file: `sourceQualifiedName`, `targetQualifiedName`, `targetFile`. The
consumer resolved each pair with a map from `file + qualified name` to the
symbol, and a map keeps one value per key.

A qualified name is not unique in a file. The parameter of an arrow function
assigned to a `const` is just its own name, a local variable has no function
prefix, and a method of an object literal may share it. `ky`'s
`source/utils/body.ts` has five symbols called `value`: two parameters, a loop
variable and two methods. The worker resolved every use correctly, and the
consumer then linked all of them to whichever was declared last, a method with
no callers:

```text
find_references value (method, body.ts:149)
  getBodySize        REFERENCES  body.ts:29
  normalizeLineBreaks REFERENCES body.ts:25
  body               REFERENCES  body.ts:16
```

None of those uses was of the method. The wrong target also means the right one
lost the reference, so this costs recall as well as precision.

## Decision

Each reference carries `sourceStart` and `targetStart`: the offset where the
declaration holding the use starts and where the declaration it reaches starts.
The consumer indexes the payload's symbols by `file + qualified name + start`
as well, and resolves by offset when the reference has one. `sourceStart` is
null for a use at file level, which has no declaring symbol.

The fields are optional on the consumer, as pointers: a payload from a worker
that predates them has neither, and falls back to the name alone, which is the
previous behaviour. A declaration can start at offset 0, so absent is not zero.

The version stays at 5. A new optional field does not change what a consumer of
the old shape reads.

## Consequences

- On `ky`, the method `value` of `body.ts` no longer reports the uses of other
  `value`s, and `find_references` on a parameter or a local no longer loses its
  references to the last symbol of the same name. File precision `96.2 %` to
  `96.8 %`.
- It also shows how much the old behaviour held up. `ky` recall was `100 %` in
  part because the uses of a local `ky` in `createInstance` were attributed to the
  top-level `ky`, covering `index.ts`. The real use there is `export default ky;`.
  The benchmark discarded every `EXPORTS` edge of the declaring file, which hid
  it; it now discards only the one on the line of the declaration
  (`export const x = ...`) and keeps one written elsewhere. Recall is `100 %` again,
  from the right row.
- Not changed: two symbols of one file with the same name, kind and signature,
  such as two `(value: string)` parameters, have the same stable key, and the
  graph keeps one of them. Nothing fails; the second is not a symbol of its own.
- Existing graphs need `kivgraph index --full`.
