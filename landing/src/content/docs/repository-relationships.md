---
title: Repository Relationships and Code Dependencies
description: Explore callers, callees, code dependencies and cross-repository relationships with Kivgraph's local semantic code graph.
---

A codebase is more than a directory tree. Its important structure is the set of **repository relationships** between symbols, files, packages and projects.

Kivgraph indexes those relationships into a canonical code graph so an AI coding agent can navigate dependencies without reading every file first.

## Relationships Kivgraph can answer

The graph represents questions such as:

- Which symbols call this function?
- Which declarations does this implementation reference?
- What reaches this symbol within a bounded depth?
- Which repository consumes a symbol from another repository?
- Which packages and files are affected by a change?

Use [`find_references`](/docs/tools/find-references/) for direct incoming and outgoing references, [`trace_dependencies`](/docs/tools/trace-dependencies/) for paths outward, and [`get_blast_radius`](/docs/tools/get-blast-radius/) for bounded incoming impact.

## Cross-repository dependencies

Each registered repository keeps its identity in the graph. Cross-repository consumers are reported separately from local callers, so an agent can tell whether a change affects one project or a wider workspace.

The [`find_cross_repo_consumers`](/docs/tools/find-cross-repo-consumers/) tool reports consumers in another repository when the index has enough evidence. A package dependency alone is not presented as a symbol use.

## Evidence is part of the relationship

Kivgraph does not promote a plausible name match to an exact edge. Results carry confidence and provenance. Facts that cannot be resolved remain `UNRESOLVED` with their reason, while weaker analyzer results remain `CANDIDATE` instead of being presented as exact.

How strong that evidence is depends on the language. Go, TypeScript and Rust edges are type-checked; Dart edges are resolved by Dart Analysis Server; Python uses exact semantic facts when a configured analyzer provides them and `CANDIDATE` facts in its bundled AST fallback. A Python relationship read from the fallback is a candidate, and the response says so.

That distinction is what makes repository relationship queries useful in code review and impact analysis: the agent can see what is proven, what is uncertain and what the index could not load. It is not a reason to route every question through the graph — on five of the 29 benchmark questions plain `grep` answered correctly for fewer tokens, and the [comparison](/comparison/) names them.

Read the [resolution vocabulary](/docs/resolution/) before relying on a result in an automated workflow.
