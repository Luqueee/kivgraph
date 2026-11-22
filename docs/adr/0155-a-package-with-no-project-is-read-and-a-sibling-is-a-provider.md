# ADR 0155: A package with no project is read, and a sibling is a provider

## Status

Accepted. Adds symbols, files and edges to a TypeScript graph; existing graphs
gain them on the next pass. Adds two worker arguments and one line of `index`
output. Changes no schema, stable key or tool.

## Context

Two things kept a module every page imports out of the graph of a real
monorepo.

**A package with a manifest and no `tsconfig.json` was not indexed.** A
package is read through its project, and a shared package that ships its
source -- `"main": "src/index.ts"`, consumed through `workspace:*` and
compiled by whoever imports it -- often declares none. ADR 0050 reads a file no
project claims through an inferred project, but only beside a declared one and
only with `typescript.include_unclaimed_sources` on. The audit named these
files and the pass read none of them.

**An import of the package next door was never an edge.** A provider was a
package of another repository: the registry a consumer is handed left out its
own repository, so `import { api } from "@scope/client"` was
`PACKAGE_PROVIDER_NOT_FOUND` with a declared sibling as with an undeclared
one. Asked who uses `api`, the graph answered `COMPLETE` with the package's
own files while twenty-nine others import it. Naming the sibling alone did not
help either: across repositories a consumer reads a built `.d.ts` and maps it
back, and here the checker resolves the import to the source, where there is
nothing to map.

## Decision

### A package that declares no project is a unit

Every named package of a repository is a unit when TypeScript sources lie
under it. With no project of its own it is read through one Kivgraph writes
for the pass: its files listed by name, `strict`, `ES2022`, `ESNext` modules
resolved as a bundler does, `jsx` preserved, library checks skipped.

- The project is written to a temporary directory and removed after the
  package is read. Nothing is created in a repository that is only being
  read, so the worker takes the package by `--package-root` and the project
  from outside the repository root, which it refuses for a declared one.
- Its files are the ones no project claims whose deepest enclosing package it
  is. A file of a package with a project still waits for the key; a file in
  no package is read by nobody and is named, key or no key.
- The pass says so: `index.typescript.inferred: <repository>: <package>:
  declares no project, so its N file(s) are read under compiler options
  Kivgraph chose`.
- The audit keeps `TS_NO_PROJECT` for a repository none of whose packages
  declares one, as `partial` when their TypeScript is read this way and
  `blocking` when there is only JavaScript, which is a source only if a
  project says so.

The options are a choice, and the graph does not hide it: the edges are the
checker's under those options, not under ones the repository declared. A
package that needs others -- path aliases, a `types` list -- declares a
project, and then it is read through that.

### A package of the same repository is a provider

The worker is told the other packages of the repository it is reading, each
by its root: `--sibling <absolute path>`. Each is a registry entry with its
manifest and no project.

When the checker resolves an imported name to a source file -- not a
declaration artifact -- that a registered package owns, the identity of the
target is composed there: the declaration at the position the checker gave,
classified by the functions that package's own pass runs over the same bytes.
No other project is opened. The edge is `IMPORTS_SYMBOL`, `EXACT_TYPECHECKED`.

- Ownership is the registered package whose manifest directory contains the
  file, the deepest first. Packages of one repository share its root, and the
  first of them is not the one that declares the file.
- A file under `node_modules` is owned by nobody. A dependency that ships its
  source is still a package nobody provides; a `workspace:*` link is not under
  `node_modules` once resolved, which is what tells them apart.
- An import that resolves to a built artifact of a sibling takes the path it
  always took, and without the sibling's project that path ends in
  `PROVIDER_SOURCE_UNAVAILABLE`. This is unchanged.
- Without the argument nothing changes: a sibling is never inferred from a
  path.

## Consequences

- `find_references` for an export of a shared package lists the files that
  import it. On the monorepo above `api` went from no symbol to `29` files.
- A repository whose packages import a sibling's **class** now carries
  `IMPLEMENTATION_COVERAGE_PARTIAL`, the scope already recorded for an
  imported target that is not an interface or a type alias. It bounded every
  TypeScript answer in the repository; ADR 0146, as amended with this change,
  keeps it to the answers an implementation relation could be part of.
- The fact cache already fingerprints a requested package by its tree, so a
  consumer is read again when a sibling changes.

## Rejected

- **Writing a `tsconfig.json` into the package.** A pass does not write into
  what it reads.
- **Reading every sibling's project from the consumer.** It is what crossing
  repositories needs, and here it would open a program per sibling to learn
  what the consumer's own program already parsed.
- **Refusing the answer instead.** Recording the unprovided sibling as a
  failure naming each import would make the answer `LOWER_BOUND` and still
  list no file.

## Verification

Worker tests: an import of two siblings, one with a project and one without,
resolves to each one's own declaration with the identity that package gives
itself; an installed dependency that ships source stays unprovided; nothing
resolves when no sibling is named; a package is read through a project outside
the repository, and the three malformed invocations are refused. Go tests for
the units, the arguments, the attribution and the audit. Each fails with its
change reverted.

Not measured: the options chosen against a package that needs path aliases.
Not run: a published bundle, which carries the worker these arguments need.
