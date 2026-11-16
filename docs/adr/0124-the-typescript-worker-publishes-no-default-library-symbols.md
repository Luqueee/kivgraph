# ADR 0124: The TypeScript worker publishes no symbol of the default library

Status: accepted, 2026-11-16. Refines the rule that the files of a dependency are
not the repository's own code.

Changes the configuration: no.

Changes the schema: no. The `ts-facts-v5` payload keeps its shape; a repository
that holds its own copy of the TypeScript engine publishes far fewer symbols and
references, and the same number on every pass.

## Context

The worker emits, for each TypeScript project, the symbols of every file of the
program that lies under the project root and that the engine does not call
"from an external library" (`isSourceFileFromExternalLibrary`). Files of a
dependency are meant to be left out by that second test.

The same corpus indexed eight times from a cold cache gave `72.723` TypeScript
symbols six times and `68.298` twice. Only one of the twenty-five units moved,
`@kivgraph/ts-worker`: `31.756` symbols in one run and `27.331` in the other, the
same package, the same sources. Running the worker's CLI on it twenty times gave
three different outputs (`31.756`, `27.331`, `27.323`).

What differs is the default library. The `lib.*.d.ts` files of the compiler
(`lib.es5.d.ts` alone is `2.517` symbols) are emitted in some runs and not in
others. They are under the project root only for a repository that contains the
engine: the worker's own native engine is installed under
`ts-worker/node_modules`, so indexing this repository finds them there. For any
other repository they are outside the root and never reach the filter.

The engine alone reproduces it, with no code of this repository between the
script and its API (`typescript@7.0.2`, the same program opened eighty times):

|question|answer|
|---|---|
|`isSourceFileDefaultLibrary`|the same `65` files every time|
|`isSourceFileFromExternalLibrary` on those files|`41` of them change between runs|
|`isSourceFileFromExternalLibrary` on every other file|never changes|

The default library files are called external in `17` of the `80` runs, `42` or `43`
of them at once; in the other `63` they are called local, bar two. The engine loads
the program in parallel and the answer follows which route reached each file first.
So the count of symbols alternated, and `28.255` of the `31.756` of that unit,
`39 %` of all TypeScript symbols of the corpus, were declarations of the language
itself.

## Decision

A file is a dependency's, and not the repository's own, when the engine calls it
external **or** a default library file. `isDependencySourceFile` asks both and is
used at the four places that decide which files of a program are read:
`extractLocalSymbols`, `extractLocalReferences`, the heritage resolver and the
package-import resolver.

The second question is the one that is stable. Nothing else about how a file is
selected changes, and a repository whose default library is outside its root, which
is every repository but this one, is unaffected.

## Alternatives considered

- **Treat the default library as local every time.** Also deterministic, and it
  keeps what the majority of runs did. It keeps the noise: tens of thousands of
  declarations of `Array` and `Promise` as symbols of this repository, answers to
  `find_symbol` that are not its code, and references that never leave the language.
- **Do nothing.** The published generation of the repository differs from one pass
  to the next with the same sources, which the fact cache cannot repair: a
  `verify` pass would report a divergence at random, and an unlucky run is served
  from the cache until the sources change.
- **Report it to the engine.** The flag changing between loads of the same program
  is theirs to fix, and is worth reporting. A fix there would not tell this
  repository whether it wants the default library, and it would reach it after a
  release.

## Consequences

- The corpus' TypeScript symbols are `44.468` and its references `98.926` on every
  one of twelve cold runs, against `72.723` and `116.661` (and `68.298` and
  `112.800`) before. `@kivgraph/ts-worker` goes from `31.756` symbols to
  `3.501`; the
  difference, `28.255`, is exactly the default library. Unresolved references move
  from `1.237` to `1.239`.
- A reference from this repository's code to a declaration of the language is no
  longer a reference to a symbol: it is outside the graph, like a reference to any
  other dependency.
- Anyone who indexed this repository sees every generation published after the
  change differ from the ones before in those symbols.
- A release invalidates the fact cache, because the analyzer fingerprint hashes
  the executable. Rebuilding only `ts-worker/dist` in a checkout does not:
  `typeScriptWorkerFingerprint` hashes the entry script and not the modules it
  imports, so a cache filled by the old worker keeps serving its facts, default
  library included, until the unit's sources change or the cache is cleared.
  Measured: with the fixed build swapped into the corpus in place, the next pass
  served `33` of `39` units from the cache and still reported `72.723` symbols.
  That gap was closed afterwards for a checkout; see the amendment of ADR 0100.

## Verification

- `dependency-source-file.test.ts`: the four combinations of the two answers,
  and a program whose "external" answer alternates between calls still gives the
  same result when the file is a default library file.
- `symbol-extractor.test.ts`: the worker's own project, loaded eight times, has the
  default library under its root every time and publishes no symbol from it. Before
  the change the test fails, because the files are called local in about four loads
  of five.
- `pnpm check` (format, lint, typecheck, `110` tests) passes. The real binary on the
  six-repository corpus, twelve cold runs with the fixed build installed as the
  corpus' own worker: `44.468` symbols and `98.926` references in all twelve. The
  engine has to lie under the indexed root for the case to exist: a worker installed
  elsewhere gives `44.468` with or without the change.

Not covered by a test: that `extractLocalReferences`, the heritage resolver and the
package-import resolver use the helper. They were changed by the same edit and the
corpus figures above include the reference extractor; none has a fixture in which
the default library lies under the project root and their answer can be told apart.
