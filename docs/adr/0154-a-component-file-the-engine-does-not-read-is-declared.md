# ADR 0154: A component file the engine does not read is declared

## Status

Accepted. Adds file rows and unresolved rows to a TypeScript graph; existing
graphs gain them on the next pass. Changes no schema, stable key or tool.

## Context

A TypeScript project is not only `.ts`. An Astro page, a Vue or a Svelte
component imports from `src/lib` like any module, and the TypeScript engine
does not read it: there is a template around the script. Its uses were in no
edge and in no failure. On a real site `find_references` for a helper every
page calls answered `COMPLETE` with one caller; twelve `.astro` files import
it.

ADR 0150 met the same shape in Rust, a file the build left out, and declared
what can be said without reading it.

## Decision

After a TypeScript package is normalised, every `.astro`, `.vue` and `.svelte`
file under its root is read for words. For each name the package **exports**
that the file writes, one unresolved row is recorded: the file, the first line
that writes the name, the name, and `COMPONENT_FILE_NOT_READ`. The file is
published, because a row with no file is a scope and would bound every answer
in the repository.

That makes an answer about that name `LOWER_BOUND`, with the files named, and
leaves every other answer alone.

- A file that writes no exported name is not listed.
- A directory that holds another `package.json` is that package's. A hidden
  directory, `node_modules`, a directory no walk enters and what the
  repository excludes are not searched.
- A file above `1 MiB` is not read.
- Only exported names: a declaration a module keeps to itself cannot be
  imported by a page.

It reads words, not the template. A name in the markup counts, which errs
towards saying an answer is bounded: an export called `title` is bounded by
every page with a `title` attribute.

## Also: a destructured dynamic import

`const { load } = await import("./x")` binds a new local, and nothing pointed
it at the export of `./x`: what a route or a command loads this way had no
use. The worker now resolves the name through the type of the module, as it
does for any destructured property, when the pattern destructures an
`import()` and some declaration of the program exports that name. The use is
recorded from the binding. `(await import("./x")).load()` and
`lib.load()` were already resolved.

## Rejected

- **Compiling the component.** Each framework has its own compiler and its
  own version; the graph would say `EXACT` on the word of a tool it does not
  pin.
- **Extracting the script block and handing it to the engine.** Astro's
  frontmatter is not a module the engine can place in a program without the
  framework's types and its virtual imports.

## Verification

Unit tests for the listing, with what is not searched and what is not listed;
a worker test for the three ways of using a dynamic import. On the site above
the helper answers `LOWER_BOUND` with the twelve files named.

Not measured: how many answers on a large application the common names cost.

## Amendment: a component file is an observed source

The rows this decision records are facts about a file no analyzer reads, and
nothing watched that file. Editing, adding or removing an `.astro`, `.vue` or
`.svelte` file left the fact cache serving the unit as it was and the
generation reporting itself fresh, with rows about a file that no longer said
that. The three extensions are analysed sources now
(`sourceobservation.IsComponentFile`): they enter the tree digest of a unit,
the digest a generation records, and what the watcher reacts to.

A generation published before this records a digest without them, so a
repository that holds such files reads as changed until its next pass.
