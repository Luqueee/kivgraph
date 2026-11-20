# ADR 0137: keys of a literal in a generic call follow the constraint

## Status

Accepted. Adds references the TypeScript worker did not emit; no change to the
wire format or to any identity. Needs `kivgraph index --full`.

## Context

The key of an object literal names a property of the type the literal is
checked against. The worker asks the checker for the contextual type of the
literal and looks the key up in it. For an argument of a generic call such as

```ts
extend<T extends Array<Got | ExtendOptions>>(...options: T): Got<...>
got.extend({https: {rejectUnauthorized: false}})
```

the contextual type is `T` already instantiated, which is the type inferred from
the literal itself. The key then resolves to a property the literal declares,
which is not a declaration of the project, and the reference was dropped. Nothing
was reported as unresolved: the use was simply absent from the answer.

Found by the second-repository run of the language-recall benchmark: two of the
four declarations `got` missed (`HttpsOptions.rejectUnauthorized`,
`OptionsInit.beforeRedirect`) were keys of a literal passed to `got.extend`.

## Decision

When the member a key resolves to is not a declaration of the project and the
only declarations it has are the literal's own (property assignment, shorthand,
method or accessor of an object literal), the worker looks again in the declared
type:

1. Climb from the literal through property values to the literal that is an
   argument of a call or `new`.
2. Take the parameter type of the generic signature the call resolved from, at
   that argument's position, and its base constraint. For a rest parameter
   `...options: T` that is the constraint of `T[0]`.
3. Walk down the keys that led to the literal. A union is walked member by
   member, as the checker does for a contextual type: a property is on a union
   only when every member has it, and `Got | ExtendOptions` has `https` on one
   of them only.
4. Look the key up in the types reached, and take the first member that is a
   project declaration.

A member that is declared by a property signature, a class or any other
declaration is never looked up again, so keys of external types cost nothing
extra.

## Consequences

- On `got` the two declarations are now found: recall `97.8 %` to `98.7 %`,
  precision unchanged. `ky` does not move (`100 %`, `96.8 %`).
- Measured on `got` by the worker alone: fact collection `5.7 s` to `6.1 s`
  (about `8 %`).
- The remaining two `got` misses are not the worker's: `dnsCache` and
  `maxHeaderSize` are getter and setter pairs, and the oracle renames only the
  setter, which turns the getter read-only and cascades errors in the same file.
- Not exercised: a literal behind `as`, `satisfies` or parentheses (not
  climbed), and a call whose resolved signature is overloaded.
