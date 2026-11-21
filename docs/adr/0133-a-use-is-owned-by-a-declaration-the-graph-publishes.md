# ADR 0133: a Go use is owned by a declaration the graph publishes

## Status

Accepted.

## Context

`find_references` answered `COMPLETE` and left out a file that used the symbol.
The language-recall benchmark found three such answers on non-test files of
`gin` and `caddy`, and traced them to two shapes of use that were sourced at a
name nobody published, so the edge was dropped as having no source:

- **A local declaration.** `var warnings []caddyconfig.Warning` inside a
  function. The loader looked for the declaration around the use, found the
  local `var` first and answered its name. A local is not a symbol, so the file
  that declared the alias had no reference to it.
- **A blank package declaration.** `var _ Render = (*Redirect)(nil)`, the
  interface assertion. Its name is `_`, which is not a symbol either.

Fixing the first exposed two defects behind the second, because nothing had ever
reached them with a real file. A use that no declaration encloses belongs to the
file's own scope, the synthetic module symbol:

- the identity of that module symbol left its discriminator empty, which the
  stable key refuses, so one assertion in a file aborted the whole index;
- the symbol was appended to a list that `NormalizeGo` rebuilt from the
  declarations at the end, so even with a valid identity the symbol vanished and
  its edges pointed at nothing.

## Decision

- The enclosing declaration of a use is the nearest one **the graph publishes**.
  A `var`, `const` or `type` whose object is not a package-level symbol -- local
  to a function, or the blank `_` -- is skipped, and the walk continues outwards:
  to the function around it, or to the file when there is none. A declaration
  with several names uses the first one the graph publishes.
- The module symbol carries the same discriminator the other synthetic
  identities do and is registered with the symbols of the pass, so it is
  published and its edges resolve.

## Consequences

- With tests read, file recall went from `98.9 %`, `95.8 %`, `97.1 %` and
  `99.2 %` to `100 %` on `gin`, `caddy`, `prometheus` and `cobra`, and the
  `COMPLETE` answers that missed a non-test file from `3` to `0`.
- A Go file with an interface assertion now has a module symbol, and the
  assertion is a reference from it. This is new rows in `find_references` and a
  new symbol per such file in the graph.
- A use inside a local `type` belongs to the function around it.
- Existing graphs need `kivgraph index --full`.
