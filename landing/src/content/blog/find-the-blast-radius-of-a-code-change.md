---
title: How to Find the Blast Radius of a Code Change
description: Find a code change's blast radius by tracing callers, dependencies and cross-repository consumers before editing a shared symbol.
pubDate: 2026-08-30
author: Kivgraph
category: Change impact
tags:
  - blast radius
  - change impact
  - dependency graph
  - code review
featured: true
---

The blast radius of a code change is the set of code and repositories that may depend on the changed symbol. Finding it means following proven references and bounded dependency paths before editing the implementation.

## Start with the symbol, not the file

First identify the declaration and its fully qualified identity. A filename is only a location; the same short name can be declared in multiple packages.

With an MCP code intelligence server, the starting question can be:

What breaks if I change the Payment interface?

If the symbol name is unknown, use [find_by_intent](/docs/tools/find-by-intent/) to locate likely declarations and source files.

## Find direct references

Use [find_references](/docs/tools/find-references/) to inspect direct incoming and outgoing relationships. Incoming references show callers and consumers that may need an update. Outgoing references show what the implementation itself depends on.

Keep the evidence attached to each result. An exact, type-checked reference is a stronger basis for a change than a candidate produced by name matching.

## Trace bounded dependencies

Direct references are not the whole impact. A changed package can affect another package through several edges. Use [trace_dependencies](/docs/tools/trace-dependencies/) with a bounded depth to see the paths that lead outward.

Bounded traversal keeps the result actionable. It also makes the question explicit: a depth-one review asks a different question from a repository-wide dependency audit.

## Check cross-repository consumers

Shared libraries and services often have consumers outside the repository being edited. [find_cross_repo_consumers](/docs/tools/find-cross-repo-consumers/) makes that boundary visible when the workspace contains registered repositories. GitHub's [dependency graph](https://docs.github.com/en/code-security/concepts/supply-chain-security/dependency-graph) provides a package-level view of dependencies and dependents; this workflow follows the symbols and callers behind that view.

Cross-repository results should name both repositories and preserve their identities. A single flattened list of matching paths is not enough to plan a safe change.

## Use a blast-radius query

Finally, use [get_blast_radius](/docs/tools/get-blast-radius/) for a bounded incoming impact view. Review the returned files, symbols, relation kinds and confidence before deciding which tests and consumers to update.

The result is a change plan, not an automatic edit. The agent still needs to inspect the relevant source and run the affected tests.

## A repeatable checklist

1. Resolve the intended declaration.
2. Find direct incoming references.
3. Trace dependencies to a stated depth.
4. Check consumers in other repositories.
5. Inspect the affected source and tests.
6. Make the smallest compatible change.
7. Run tests for every confirmed consumer.

This workflow combines fast text search with semantic evidence. It gives the agent a smaller, more defensible context for the change without pretending that every search query needs a graph.
