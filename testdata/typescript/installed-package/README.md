# Fixture de copia instalada por un gestor de paquetes

Reproduce la forma que `pnpm` instala de verdad, medida sobre el monorepo de
referencia (`@private/shared@0.0.1`), no una que resuelva por `paths`:

- el consumidor llega al proveedor por un symlink de `node_modules` hacia el
  almacén `node_modules/.pnpm/<nombre>@<versión>/node_modules/<nombre>`;
- esa copia trae `dist/*.d.ts` y **ningún** `.d.ts.map`, ni `src/`, ni
  `tsconfig.json`: es un tarball publicado, no un enlace al workspace;
- la fuente del proveedor vive en **otro repositorio registrado**
  (`provider-shared/`), con su propio `tsconfig.json` y su `src/`.

Es la única forma en la que el `.d.ts` que resuelve el checker tiene una
identidad de archivo distinta de la fuente que lo produce, y por eso el puente
de `declaration-source-resolver.ts` tiene que llegar al repositorio del
workspace por el `name` del `package.json` más cercano. Ver ADR 0051.

## Los tres casos

| import del consumidor | copia instalada | repositorio registrado | resultado |
| --- | --- | --- | --- |
| `withRetry` de `@kivgraph-fixture/installed` | `dist/retry.d.ts` | `provider-shared` declara ese paquete | identidad en `provider-shared/src/retry.ts`, `EXACT_PACKAGE_MAPPED`/`TYPESCRIPT_PROJECT_REFERENCE` |
| `legacyRetry` de `@kivgraph-fixture/drifted` | `dist/legacy.d.ts` (publicado en `1.0.0`) | `provider-drifted` en `1.1.0`, que renombró el export a `renamedRetry` | `UNRESOLVED`: la fuente no exporta ese nombre y no se cae hacia el artefacto |
| `vendoredHelper`, reexportado por `@kivgraph-fixture/installed` | `@kivgraph-fixture/vendored/dist/index.d.ts` | ninguno declara `@kivgraph-fixture/vendored` | `UNRESOLVED`, sin cambio |

El tercero es la dependencia transitiva que `pnpm` cuelga como hermana en el
almacén: `@private/shared` depende de `@workspace/env`, `@workspace/http` y `@workspace/logger`
igual, y sólo algunas están registradas. El `package.json` más cercano al
`.d.ts` nombra al dueño real, que no es el paquete que el consumidor importó.
