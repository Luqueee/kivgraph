---
title: "Cross-Repository Code Intelligence: Monorepos & Polyrepos"
description: Use a cross-repository code graph to show which repositories consume shared symbols, dependencies and contracts before an AI coding agent changes them.
pubDate: 2026-08-30
author: Kivgraph
category: Workspaces
tags:
  - cross-repository dependencies
  - monorepo
  - polyrepo
  - code intelligence
featured: false
---

Cross-repository code intelligence lets an AI coding agent follow a symbol or dependency across project boundaries. That matters when a shared library, service contract or generated type has consumers outside the repository being edited.

## Why repository boundaries matter

A repository boundary is an ownership boundary, not necessarily a dependency boundary. A service can import a library maintained elsewhere, and a shared contract can affect several deployable projects.

A directory search inside one repository cannot prove that the relationship ends there. The missing consumer may be in another checkout, with a different package path and a different source of truth. GitHub's [dependency graph](https://docs.github.com/en/code-security/concepts/supply-chain-security/dependency-graph) makes repository-level dependencies and dependents visible; a code graph preserves the symbol-level relationships inside that boundary.

## What a workspace graph should preserve

A useful cross-repository graph should preserve:

- each repository identity;
- package and file ownership;
- the symbol at each end of a relationship;
- the direction and kind of the edge;
- evidence, confidence and unresolved cases.

Flattening all paths into one list loses the information needed to coordinate a change safely.

## Questions an agent can ask

In a registered workspace, ask:

- Which repository consumes this exported symbol?
- What calls this interface across the workspace?
- Which dependency path reaches the shared package?
- What is the bounded impact of changing this type?

Kivgraph documents this workflow in the [cross-repository code graph guide](/cross-repository-code-graph/) and exposes the relevant [cross-repository consumer tool](/docs/tools/find-cross-repo-consumers/).

## Monorepo or polyrepo

The same reasoning applies to both layouts. A monorepo makes files easy to browse but can still contain independent packages and ownership boundaries. A polyrepo makes the boundary explicit but requires a workspace index to see consumers together.

The important question is not which layout is better. It is whether the agent can see the relationships that cross the boundary before it proposes a change.
