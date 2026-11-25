# ADR 0163: A Rust `Type::method` resolves only through METHOD_OF

## Status

Accepted. Adds a fallback to the qualified-name selector for Rust methods. No
input field and no response field changes. An exact canonical match is unchanged.

## Context

A Rust method's canonical qualified name is its SCIP descriptor path, and that
path spells the impl block: `shapes::impl::Circle::area`. A caller who reads
`Circle::area` off the source gets `SYMBOL_NOT_FOUND` from the exact lookup,
even though the method is in the graph.

Two shortcuts would make the name match, and both are wrong. Stripping
`::impl::` from the rendered name relies on a renderer convention, not a fact
of the index. Matching only the trailing segments would resolve `area` on every
type that has one.

The index does state the relation that matters. The loader publishes a
`METHOD_OF` edge from a method to its impl's type only when that type is a
declared symbol of the same crate (ADR 0033 for the engine, ADR 0035 for what
stays unresolved). The edge carries `STRUCTURAL_CERTAIN` confidence and
`rust-analyzer-definition` provenance. A syntax-only pass produces neither.

## Decision

The selector consults the Rust fallback only when the exact `qualified_name`
lookup returns no symbol. That miss has two causes: the name was never interned
in the snapshot, or it was interned and matches zero symbols. A name that was
never interned can still be a method, so a miss is not proof that the method
is absent. Both causes run the fallback. A single exact match resolves, and two
or more are `AMBIGUOUS_SYMBOL`, as before.

The fallback splits the qualified name at its last `::` into a receiver and a
method. It finds the symbols named by the method, narrowed by `repository` and
`path` as the exact lookup is. Each one is classified against the receiver:

- **Proven**: the symbol has an outgoing `METHOD_OF` edge with provenance
  `rust-analyzer-definition` whose target is a declared symbol matching the
  receiver. The match is the target's canonical name equal to the whole receiver,
  or, when the receiver is one identifier, the target's own name.
- **Unproven**: the symbol is a Rust symbol whose canonical name carries the
  receiver's last step before its method step, which is what an impl block the
  index did not pair with a declared type looks like.
- **Unrelated**: anything else. It is never named.

The outcomes follow the proven count:

- **None proven**: `SYMBOL_NOT_FOUND`. The message names up to five unproven
  candidates by canonical name and location, and says how many more there are.
  Each candidate is a suggestion. Nothing resolves through it.
- **One proven, no unproven**: that method resolves.
- **One proven, at least one unproven**: `AMBIGUOUS_SYMBOL`. The message names
  the verified method and the unproven candidates, and asks for the exact
  `qualified_name`. The fallback does not pick the proven one, because the
  unproven candidates might be the method the caller meant.
- **Two or more proven**: `AMBIGUOUS_SYMBOL`, naming each method by canonical name
  so the next call can resolve it exactly. The fallback never picks the first,
  prefers an inherent method over a trait one, or prefers the nearer type. Two
  traits that define `name` are two methods, and only the caller knows which
  one was meant.

Nothing else resolves a Rust method. `crate::`, `self::`, `super::` and
`<T as Trait>::method` are not parsed as receivers, and a receiver with generic
arguments, references or `dyn` does not match a declared type.

## Limits

- Only impls whose type is a declared symbol of the same crate are paired. An
  impl on a foreign type, or on a type the index never declared, pairs with
  nothing, so its methods are at most suggestions.
- When one method is proven and unproven same-named methods also exist, the
  call is `AMBIGUOUS_SYMBOL`, not a resolution. A proven method alone resolves
  only when no unproven candidate is left.

## Consequences

- A caller who writes `Type::method` from source gets the canonical method when
  the index proves it, and a named list of candidates when it cannot.
- A miss never guesses. The worst outcome is `SYMBOL_NOT_FOUND` with suggestions,
  which costs one more call with an exact `qualified_name`.
