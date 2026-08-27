# ADR 0051: De la copia instalada a la fuente del workspace

- **Estado:** aceptada
- **Fecha:** 2026-08-21

## Contexto

El ADR 0038 cerró el caso del proveedor que compila sin `declarationMap`:
cuando el puente nombra la fuente, se le pregunta al checker del proveedor qué
declaración exporta ese módulo bajo el nombre pedido. Su premisa es «cuando el
puente nombra la fuente». Este ADR es el caso en el que no la nombraba.

`declaration-source-resolver.ts` relacionaba artefacto y fuente por cuatro
vías: el `.d.ts.map`, las raíces del proyecto del proveedor, las del registro y
la transformada `rootDir`/`outDir`. Las tres últimas exigen que el `.d.ts` esté
**bajo la raíz del proveedor**, porque calculan una ruta relativa desde ella.

Un consumidor que escribe `import { withRetry } from "@private/shared"` no
resuelve ahí. `pnpm` instala:

```
workspace/modules/sdk-module-ts/node_modules/@private/shared
  -> ../../../../node_modules/.pnpm/@workspace+shared@0.0.1_<peers>/
     node_modules/@private/shared
```

El destino es un **directorio real**, no un enlace al workspace: el tarball
publicado, con `dist/**/*.d.ts`, sin `src`, sin `tsconfig.json` y sin ningún
`.d.ts.map` -publicar un mapa cuyos `sources` nombran archivos que el tarball
omite no coloca nada-. Su identidad de `File` no es la de
`libraries/library-shared/src/utils/retry.ts`, y ninguna de las cuatro vías
podía relacionarlas: el artefacto no está bajo la raíz del proveedor, está bajo
el `node_modules` de quien lo instaló.

El resultado medido sobre el corpus de referencia, en los tres repositorios que
la pregunta `R1_ts_xrepo` de `benchmarks/graph-tools-comparison` cubre: `804`
imports de `@private/shared` -`408` en `sdk-module-ts`, `310` en `packages/core`,
`86` en `packages/gateway`- y **ninguno** con destino. Los cinco sitios de uso
de `withRetry` que la ground truth nombra no existían como arista, y la
respuesta a «quién usa `withRetry`» quedaba en los tres barriles de
reexportación de `library-shared`: precisión `0.00`, exhaustividad `0.00`.

El propio grafo lo declaraba, en `completeness.blind_spots`:

```
reason            DECLARATION_SOURCE_NOT_MAPPED
requested_symbol  expBackoffJitter
requested_package @private/shared
detail            .../node_modules/.pnpm/@workspace+shared@0.0.1_...
                  /dist/utils/retry.d.ts
```

El ADR 0038 ya había descartado «exigir `declarationMap` en los proveedores»
por esta misma razón, sin resolver todavía la vía positiva.

## Decisión

Se añade una quinta vía al puente, la última que se intenta antes de
`UNRESOLVED`: dado un `.d.ts` bajo un `node_modules`, se lee el `package.json`
**más cercano**, se toma su `name` y se busca ese nombre en el registro de
proveedores que el CLI construye con `--provider` / `--provider-project`.
Cuando un repositorio registrado declara ese nombre exacto, la transformada
`rootDir`/`outDir` de ese repositorio se aplica con la raíz de declaración
**re-anclada sobre la copia instalada**, y la fuente que resulta se le pasa al
checker de ese proveedor exactamente como hace la vía sin mapa del ADR 0038.

El estado del mapeo es `INSTALLED_PACKAGE`; la identidad resultante lleva
`source: "PROVIDER_EXPORT"`, y por tanto la arista es
`EXACT_PACKAGE_MAPPED`/`TYPESCRIPT_PROJECT_REFERENCE`. Nunca
`EXACT_TYPECHECKED`: el paso de artefacto a fuente lo afirma la configuración
de compilación del proveedor, y aquí además cruza una publicación.

### El nombre lo dice el artefacto, no el import

Se pregunta por el `name` del `package.json` más cercano, no por el paquete que
el consumidor escribió. No son el mismo:

- `@private/shared` reexporta `@workspace/env`, `@workspace/http` y
  `@workspace/logger`, que
  `pnpm` cuelga como hermanas en el almacén. Un `vendoredHelper` importado
  «de» `@private/shared` está declarado en el `.d.ts` de otro paquete.
- Acreditar al paquete que el consumidor escribió compondría una clave contra
  un repositorio que no publica ese símbolo: una arista colgante con aspecto
  de correcta, que es lo que el ADR 0038 ya prohíbe para las fachadas.

La búsqueda **se detiene en el `node_modules`** en el que la copia fue
instalada. El primer manifest por encima de ese directorio es el de quien
ejecutó el install, y ese repositorio no declara nada de lo que hay debajo.
Es la misma regla que `createPackageProviderRegistry.owning` aplica por ruta:
nadie posee lo que está bajo un `node_modules`. Precisamente porque la
pertenencia por ruta no existe aquí, el dueño se busca por nombre; y la
identidad se acredita al repositorio que `owning` devuelve para el fichero
**fuente** que el puente nombró, no a la raíz instalada ni al consumidor.

### La deriva de versión no se convierte en una arista

La copia instalada puede ser una publicación anterior a la fuente del
workspace. Si el nombre pedido no está exportado por la fuente,
`locateProviderExport` no devuelve posición y **no hay caída hacia el
artefacto**: la referencia conserva `UNRESOLVED` con el motivo
`PROVIDER_SOURCE_UNAVAILABLE` y el detalle «provider project exports no
`<nombre>` in `<fichero>`», y `target` sale `null`. Componer la identidad desde
el `.d.ts` instalado daría una firma que el proveedor no se asigna, que es la
alternativa que el ADR 0038 ya descartó.

## Alternativas descartadas

- **Registrar la copia instalada como su propio repositorio.** Duplicaría cada
  paquete tantas veces como instalaciones haya en el corpus, con una clave
  estable por copia, y las aristas apuntarían a un árbol que nadie edita.
- **Emparejar por versión antes de puentear.** La versión ya tiene su propio
  motivo declarado (`VERSION_MISMATCH`, decidido en el registro de Go). Repetir
  la comparación aquí duplicaría la política, y no es la comparación que
  protege: dos árboles con la misma versión tampoco son el mismo árbol -el
  riesgo que separa `EXACT_PACKAGE_MAPPED` de `EXACT_TYPECHECKED`-. Lo que
  protege es exigir que la fuente exporte el nombre.
- **Puentear por similitud de ruta (`dist/x.d.ts` → cualquier `src/x.ts`).**
  Es coincidencia de path, que es exactamente lo que un `EXACT` no puede usar
  como evidencia.
- **Leer el `.d.ts.map` publicado cuando exista.** No cambia nada: sus
  `sources` nombran un `src/` que el tarball no publica, y si lo publicara la
  posición caería dentro de la copia instalada, que el proyecto del proveedor
  no reconoce como suya. Ya estaba descartado en el ADR 0038.

## Consecuencias

- Los `804` imports de `@private/shared` de los tres repositorios medidos pasan de
  `0` a `804` con destino, y los cinco sitios de uso de `withRetry` de la
  ground truth resuelven a `library-shared:src/utils/retry.ts:135` con
  `PROVIDER_EXPORT`.
- Un paquete instalado que ningún repositorio registrado declara -una
  dependencia de terceros, o una transitiva fuera del corpus- sigue igual:
  `UNRESOLVED`, con el `.d.ts` como detalle. La cobertura no se infla.
- El grafo gana aristas `EXACT_PACKAGE_MAPPED` donde antes no tenía ninguna;
  un consumidor que sólo acepte prueba de `.d.ts.map` sigue filtrando por
  `EXACT_TYPECHECKED`.

## Riesgos

- **La copia instalada y la fuente son dos árboles.** Es el riesgo del ADR
  0038 agravado por una publicación de por medio: la firma que se lee es la de
  la fuente indexada, no la del artefacto que el consumidor compiló. Está
  acotado por lo que la decisión exige -que la fuente exporte el nombre- y
  declarado por el grado de la arista, que es el más débil de los dos exactos.
- **Un paquete cuyo `dist` no sale de su `src`.** Si el proveedor emite con un
  bundler que reorganiza el árbol, la transformada re-anclada no encuentra
  fichero y no se inventa ninguno: el mapeo queda `UNRESOLVED`.

## Los barriles siguen apareciendo, y es otro defecto

La respuesta equivocada que motivó este ADR nombraba tres barriles de
`library-shared` (`src/index.ts`, `src/utils/index.ts`, `src/client/index.ts`).
Esas filas **no son falsas**: cada `export * from "./retry.js"` produce un hecho
`REEXPORTS` con `qualifiedName: "withRetry"`, verificado por el checker, y
seguirá produciéndolo después de este cambio.

Lo que falla es la pregunta, no el hecho: una respuesta de referencias
entrantes mezcla `REEXPORTS` -un barril que reenvía un nombre- con
`CALLS_DIRECT` e `IMPORTS_SYMBOL` -un sitio que lo usa-, y con cero aristas
cross-repository los barriles eran lo único que quedaba. Con este cambio los
cinco sitios reales aparecen; los tres barriles también. Filtrar o graduar por
tipo de arista en la respuesta de referencias es un cambio distinto, en la
superficie MCP, y no se hace aquí.

## Verificación

`installed-package-identity.test.ts` sobre el fixture
`testdata/typescript/installed-package`, que reproduce la forma que `pnpm`
instala de verdad -almacén `.pnpm`, `dist` sin mapas, fuente en otro
repositorio registrado- y no una que resuelva por `paths`:

- `withRetry` de una copia instalada resuelve a `provider-shared/src/retry.ts`
  con `source: "PROVIDER_EXPORT"`;
- `legacyRetry`, que la copia `1.0.0` exporta y la fuente `1.1.0` renombró,
  queda sin identidad y con motivo;
- `vendoredHelper`, declarado por la dependencia transitiva que ningún
  repositorio registrado declara, queda `UNRESOLVED` sin cambio.

Con el puente desactivado los dos primeros fallan con
`expected 'UNRESOLVED' to be 'INSTALLED_PACKAGE'`.
