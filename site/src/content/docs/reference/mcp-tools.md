---
title: MCP tools
description: The eleven tools Ladygraph registers over stdio, and what each one answers.
---

Ten tools are read-only. `index_project` is the only one that mutates
anything, and only after explicit consent.

Every tool answers from the published HotSnapshot. `serve` does not open the
database and does not run the TypeScript worker.

## `find_symbol`

> Finds symbols by exact name, qualified name, prefix or substring, and returns
> where each one is. Narrow with kind, repo and path_prefix.

The entry point: a name goes in, stable keys come out, and every other tool
takes a stable key.

## `get_symbol`

> Returns one symbol by its stable key.

## `get_file_outline`

> Lists the declarations of one file or of a directory, with their kind,
> signature and line range. Use it to read the shape of code without opening
> it, and to get the stable keys the other tools need.

## `find_references`

> Finds the symbols that reference one symbol, or the ones it reaches. Each row
> names the other end and where it is declared.

Direction is a parameter: incoming references, or outgoing ones.

## `find_cross_repo_consumers`

> Finds exact, package-level, candidate, and unresolved consumers of a symbol
> in other repositories.

A query about a symbol counts as a consumer only what was observed about that
symbol. Package dependencies prove that a consumer depends on the provider and
never that it uses the symbol, so they are reported in their own
`coverage.package_level` counter and are never summed into `exact`: adding them
would report a use nobody saw.

By the same rule, a resolution failure that named no symbol — an unreadable
module, an absent provider — belongs to the package. It is served by
`get_unresolved_references` with `requested_package`, not attributed to every
symbol that package exports.

## `trace_dependencies`

> Traces the bounded outgoing dependency graph of a symbol.

## `get_blast_radius`

> Groups the bounded incoming impact of a symbol by repository, package, depth,
> and relation kind.

## `get_unresolved_references`

> Lists references that could not be resolved to an exact symbol.

Each unresolved entry keeps its reason, repository and language; where there is
a concrete occurrence it keeps the file, position and detail observed.
Repository-level module failures may have no file, and evidence is never
fabricated for them.

## `list_repositories`

> Lists repositories registered with Ladygraph.

## `graph_status`

> Returns the published snapshot, its provenance, its counts, dependency
> health, and internal metrics.

It never reports what this process did not use or measure. `serve` answers from
the published HotSnapshot, so the database and worker sections are declared
`not_applicable` with the reason, and metric sections nobody observed are
omitted rather than reported as zero.

## `index_project`

> Registers one or more projects and rebuilds Ladygraph once, after explicit
> user approval. Pass every project in a single call: the rebuild costs the
> whole corpus, so calling this per project multiplies that cost and keeps only
> the last result. It never writes inside the source projects.

The only mutating tool, and the only one registered on a configured `serve`
route. It requires explicit client consent before changing the repository
registry or publishing a generation.

A full rebuild takes minutes and MCP clients apply their own timeout, so
`index_project` emits `notifications/progress` when the request carries a
`progressToken`. Without a token no progress callback is installed.

Registration is idempotent: a project already registered with the same
directory is reindexed without touching the registry, and a change of languages
preserves the `exclusions` the request cannot express. Only a name already
occupied by a different directory is a conflict, and the error names the
registered one.
