# ADR 0136: TypeScript declarations that share an identity are numbered

## Status

Accepted. Changes the stable key of the second and later declaration of an
identity in one file; the first keeps its key. Needs `kivgraph index --full`.

## Context

The stable key of a TypeScript declaration is built from the repository, the
package, the file, the qualified name, the kind and the signature. Two
declarations of one file can agree on every component: the parameter
`value: string` of two functions, or a local `key` in two methods. Nothing in
the name says which function they belong to, because a parameter's qualified
name is its own name.

Such declarations had one key, so the graph held one symbol for both and every
use of either was an edge to that node. A reference answer for one parameter
listed the uses of the other. ADR 0135 made the worker say which declaration a
use reaches, by offset, and that could only select between declarations that
have different keys; these had the same one.

Measured on `ky` by the worker's own payload: `1133` symbols, `36` identities
shared by `72` extra declarations (`28` parameters, `38` variables and `6` type
parameters), all in one file each. The published graph held `1062` symbols for
the repository.

## Decision

`NormalizeTypeScript` counts the declarations of a file by identity, in payload
order, which is by position. The first keeps its key. The n-th (n from two) takes
the signature `<signature>#<n>` as its discriminator, so a use sourced at or
targeting it by offset (ADR 0135) reaches a node of its own.

Nothing else changes: the displayed `signature` of the symbol is the real one,
and the key algorithm, the format version and the wire format are untouched.

## Consequences

- A graph built before this keeps valid keys for every first declaration, and
  only declarations that were being merged get a key they never had. There is
  no migration; a reindex produces the new symbols, `1134` for `ky`, and the
  extra `72` are the ones that were lost.
- The number is the position among the declarations that share the identity. A
  declaration added before one of them in the same file renumbers the later
  ones, like a Go `init` function (ADR 0130). Only declarations that cannot be
  told apart by anything else carry a number, and they are locals and
  parameters, which nothing outside the file refers to.
- A consumer in another repository derives the provider's key from the
  provider's file, name, kind and signature alone, so for an exported
  declaration that shares its identity with another it still reaches the first.
  The kinds that collided on `ky` are parameters, variables and type
  parameters, none of them exported; whether an exported pair can occur was not
  measured.
- The language-recall figures for `ky` did not move (`100 %` recall, `96.8 %`
  precision): the benchmark samples one declaration per name and file, and these
  are the ones it cannot tell apart.
