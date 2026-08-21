# `unclaimed-sources`

One package whose `tsconfig.json` declares `include: ["src/**/*.ts"]`, so
every file outside `src` belongs to no TypeScript program and is invisible to
the graph by construction. `tests/case.test.ts` calls `getRequiredField` from
`src/case.ts`: without `typescript.include_unclaimed_sources` that call is not
an edge, and nothing reports it missing.

What the fixture is shaped to discriminate:

| path | unclaimed | why |
| --- | --- | --- |
| `src/case.ts`, `src/index.ts` | no | the project's `include` claims them |
| `src/generated/machine.ts` | no | the project's own `exclude` names it |
| `tests/case.test.ts`, `tests/helpers/fixture.ts`, `tests/widget.tsx` | yes | no project claims them |
| `tests/globals.d.ts` | no | a declaration file declares nothing a caller needs |
| `scripts/release.ts` | yes | no project claims it |
| `build/generate.ts`, `dist/vendored.ts`, `dist/case.d.ts` | no | generated-output directory |
| `node_modules/**` | no | an installed dependency belongs to whoever published it |
