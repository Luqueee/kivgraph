# TypeScript worker instructions (`ts-worker/`)

These rules add to the `AGENTS.md` at the repository root, which is always
read. An instruction in this file may add restrictions; it can never relax an
integrity, compatibility or verification contract declared at the root.

The Go orchestrator that invokes this worker lives in `internal/`.

- The worker uses strict TypeScript and ESM modules.
- Process, protocol and adapter boundaries have explicit types; `any` needs a
  local justification.
- `stdout` carries framing and protocol only. Logs go to `stderr`.
- Every persistent resource is closed when the process is cancelled or ends.
- Rejected promises are classified at the right boundary; they are not hidden
  behind assertions.
- Do not edit `ts-worker/dist` by hand: regenerate it with `pnpm build`.

## Cross-repository identity and resolution

- The identity of a symbol from another repository always comes from the
  provider's project. If its `.d.ts.map` places the symbol, that is the
  position; if there is none but the bridge named the source, its checker is
  asked which declaration that module exports under the requested name. Both
  edges are exact and **are not worth the same**: the first is
  `EXACT_TYPECHECKED`/`TYPESCRIPT_CHECKER` and the second is
  `EXACT_PACKAGE_MAPPED`/`TYPESCRIPT_PROJECT_REFERENCE`, because the step from
  artifact to source is asserted by the provider's build configuration and not
  by a map it emitted. With no named source nothing is asked: the reference
  stays `UNRESOLVED`. See ADR 0038.
- The identity of a symbol imported by a **relative path** can still belong to
  another repository: a local barrel -`export { x } from "pkg"`- puts a foreign
  declaration behind a path that names no package. That import creates a
  binding with the provider the barrel already resolved, and the match is by
  the identity of the declaration -the checker collapses the alias chain in one
  step and lands on it, not on the intermediate binding-, never by name. See
  ADR 0055.
- The walk over bindings covers every file of the project **except those under
  a `node_modules`**: the program contains the `.d.ts` of every dependency, and
  that is the provider's source, not a use this repository makes of it. Binding
  them named the installed copy of a package as a consumer of a type that the
  same package declares, in two repositories at once.
- A cross-repository fixture that resolves to the provider with `paths` proves
  nothing about the shape a package manager installs. The consumer reaches the
  provider through a `node_modules` symlink and the engine returns the path of
  the link's target, so `consumer-linked` is the fixture that defends the real
  case; requiring `declarationMap` in the provider does not fix it.
- A copy installed by a package manager is a different `File` from the source
  that produced it, and the published tarball carries neither `src` nor
  `.d.ts.map`: no transform anchored at the provider's root relates the two
  paths, because the artifact is not under that root. The source is named by
  the `name` of the `package.json` nearest the artifact, looked up in the
  provider registry; never by the name the consumer wrote, which in a
  transitive dependency belongs to another repository. The edge is
  `EXACT_PACKAGE_MAPPED`/`TYPESCRIPT_PROJECT_REFERENCE`, and if the workspace
  source does not export the requested name -version drift- **it does not fall
  back to the artifact**: the reference stays `UNRESOLVED`. See ADR 0051 and the
  `installed-package` fixture.
- The project that resolves the identity of an imported symbol is the one that
  **owns the file** the declaration map names, not the package it was imported
  from. A facade package exists to re-export those of its workspace, so its map
  points at the repository that declares them; asking the facade's program for
  that file returns nothing, and the reference used to be abandoned as
  `PROVIDER_SOURCE_UNAVAILABLE` even though the declaration was indexed. The
  identity credits the owner: composing it against the facade produces a key
  that repository does not publish and leaves the edge dangling. Ownership is
  the longest registered root that contains the file, and nobody owns what is
  under a `node_modules` -the installed copy of a package hangs from the root
  of whoever installed it-.
- The worker does not publish the declarations of the default library
  (`lib.es5.d.ts` and the others) as symbols of a repository. The engine
  answers "external" for those files depending on the path by which it first
  reached them when loading the program in parallel, and that changes between
  loads of the same program; what does not change is that they are the default
  library, and that decides (`isDependencySourceFile`). It only matters when
  the engine sits under the project root, which is the case for this
  repository: indexing itself gave `72,723` or `68,298` TypeScript symbols
  depending on the pass. See ADR 0124.
- The worker that runs a pass is the checkout's when `ts-worker/dist` exists
  and nobody configured another: a shim installed from an earlier release takes
  the same name on the `PATH` and used to win, so `pnpm build` changed nothing
  observable and the measurement came out wrong while looking right.

## Verification

```bash
cd ts-worker
pnpm check
pnpm build
```

`pnpm check` is `format:check`, `lint`, `typecheck` and `test`. `dist/` is
generated: it is regenerated with `pnpm build` and never edited by hand.
