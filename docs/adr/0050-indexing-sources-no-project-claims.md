# ADR 0050: Las fuentes TypeScript que ningún proyecto reclama

- **Estado:** aceptada con limitaciones declaradas
- **Fecha:** 2026-08-21
- **Revisa:** ADR 0010, ADR 0030, ADR 0038

## Contexto

Un fichero `.ts` al que no llega ningún `files`/`include` de ningún `tsconfig`
del repositorio **no pertenece a ningún programa**. El compilador no lo
comprueba, así que el grafo no puede verlo y -esto es lo grave- nada lo declara
ausente: no es un `UNRESOLVED`, no es un diagnóstico, no es una fila. Es
invisible por construcción.

Medido sobre el monorepo `workspace`: de `3.247` ficheros `.ts`, **`186` (5,7 %) no
los reclama ningún proyecto**, y `177` de ellos viven en
`packages/core/tests/`, porque `packages/core/tsconfig.json` declara
`include: ["src/**/*.ts", ...]` y su árbol de tests queda fuera.

La consecuencia es directa y está medida: la pregunta `R3_ts_intra` de
`benchmarks/graph-tools-comparison` puntúa `0,89` de recall porque
`packages/core/tests/cluster/worker/ipc/utils/ipcCase.test.ts` llama a
`getRequiredField` y el grafo no lo ve.

Auditados los `376` especificadores de import de esos `186` ficheros: `111`
relativos, `201` de paquete, `40` de paquete con scope, `24` builtins `node:` y
**cero alias de `paths`**. No hace falta ningún mapeo que no tengamos.

## Decisión

Esos ficheros entran al grafo mediante el **proyecto inferido** de TypeScript,
opt-in con `typescript.include_unclaimed_sources`, **apagado por defecto**.

### Alcance: lo que ningún proyecto reclama, y nada más

`workspace.UnclaimedTypeScriptSources` los nombra resolviendo cada proyecto
descubierto con la misma maquinaria que usa el grafo de proyectos
-`resolveTypeScriptConfig` y luego `resolveTypeScriptSources`-, así que un
fichero es «no reclamado» aquí exactamente cuando el compilador lo dejaría
fuera de todos sus programas. Quedan fuera del conjunto:

- Los ficheros de declaración `.d.ts`. Declaran la forma de un artefacto, no el
  código que hay detrás, así que no hay declaración a la que un llamante
  llegue.
- `node_modules`, `dist`, `build` y lo que ya poda el descubrimiento -`.git`,
  `.pnpm`, `.yarn`, `bower_components`- más las `exclusions` del repositorio
  registrado, que siguen siendo efectivas por este camino igual que por el
  otro.
- Lo que un proyecto **excluye**. Un `exclude` es una declaración sobre ese
  árbol, no un hueco del índice; devolverlo por la puerta de atrás haría que
  dos lecturas de la misma configuración discreparan.

Cada fichero se atribuye a la **unidad de paquete que lo contiene**, la de raíz
más larga: en un monorepo el manifest de la raíz contiene a todos, así que la
coincidencia más profunda es la única atribución que pone el fichero en el
paquete que un lector nombraría. Un fichero que no contiene ningún paquete no
se puede indexar -el payload de una unidad declara su paquete y no hay ninguno-
y se nombra en `FullReport.TypeScriptUnclaimedWithoutPackage` en vez de
descartarse en silencio.

### Las opciones del compilador son nuestras, no del proyecto

El worker pasa esos ficheros a `updateSnapshot({ openFiles })`. Para cada uno,
el motor busca en sus directorios ancestros un `tsconfig` que lo contenga; como
no hay ninguno -es la definición del conjunto-, el fichero se carga en el
**proyecto inferido**, cuyo `configFileName` es `/dev/null/inferred` y cuyas
opciones son los valores por defecto del motor. Observadas sobre el fixture:

```text
allowJs, allowImportingTsExtensions, allowNonTsExtensions, resolveJsonModule,
jsx: 4, module: 99, moduleResolution: 100, strictFunctionTypes,
strictNullChecks, target: 12
```

Eso es lo que un llamante **no** puede concluir de una arista que salga de un
fichero no reclamado: que se comprobó bajo las opciones que su repositorio
declara. No hay proyecto que las declare. Lo que sí puede concluir es lo
mismo que de cualquier otra arista `EXACT_TYPECHECKED`/`TYPESCRIPT_CHECKER`:
que el checker de TypeScript resolvió el uso hasta esa declaración, con su
evidencia en un rango de código observado. La confianza y la procedencia son
las de un fichero configurado porque el mecanismo es el mismo checker; lo que
cambia es la configuración bajo la que corrió, y esta ADR es dónde eso está
escrito.

Por eso el defecto es `false`. Lo que añade es código real con llamantes
reales, y lo añade bajo una autoridad más débil que la de un proyecto
configurado. Quien la quiera, la pide.

### La tabla de símbolos abarca el programa; lo emitido, sólo los ficheros

El programa inferido trae su propia copia de todo lo que esos ficheros
importan, incluido `src/**`, que el proyecto configurado ya declaró. Las dos
mitades del filtro son necesarias y no son la misma:

- `extractLocalSymbols` corre **sin filtro** sobre el proyecto propietario: una
  referencia sólo resuelve contra una declaración que esa extracción vio, y el
  sentido entero de un fichero no reclamado es que llama al código del
  repositorio. Medido sobre el fixture: restringir también la tabla deja `6`
  referencias en vez de `7` y pierde exactamente
  `tests/case.test.ts CALLS_DIRECT -> src/case.ts#getRequiredField`, la arista
  por la que existe la función.
- De esa tabla se **emite** sólo lo que vive en los ficheros abiertos, y nunca
  un fichero que el pase configurado ya reportó. Eso es lo que impide que un
  símbolo se declare dos veces, que es lo que hace que un grafo afirme que una
  declaración vive en dos ficheros.

`extractLocalReferences` sí recibe `files:` con los ficheros abiertos: acota el
lado **origen** de la referencia, no el destino.

### Sólo símbolos y referencias

De un fichero no reclamado se recogen sus declaraciones y sus usos. No se
recogen `imports`, `exports`, `extends` ni `dependencies`, porque las cuatro
gradúan su identidad contra la **configuración del proveedor** -ADR 0038- y un
proyecto inferido no tiene ninguna que acreditar.

Eso deja una limitación declarada: un uso cuyo destino vive en otro paquete
-`describe` de `vitest`, un símbolo de `@discordjs/core`- **no es arista y
tampoco es una fila `UNRESOLVED`**. Medido sobre `workspace/packages/core` con el
fichero real: las filas no resueltas son idénticas con y sin el pase
-`DECLARATION_NOT_RESOLVED=7`, `PACKAGE_PROVIDER_NOT_FOUND=236`, las mismas que
produce el proyecto configurado-, así que el pase **no añade ni una**. No hay
avalancha de no resueltos, y tampoco hay contabilidad de lo que un fichero no
reclamado pide fuera de su paquete.

Lo único que el pase puede declarar por su cuenta es
`UNCLAIMED_FILE_WITHOUT_PROJECT`: el motor no resolvió proyecto alguno para un
fichero, ni siquiera el inferido. No se ha observado nunca; existe porque un
fichero que se pidió por nombre no se descarta en silencio.

### Invalidación

Un fichero no reclamado está, por definición, fuera de todas las raíces de
fuente que declara el paquete, así que nada de lo que la caché de hechos ya
medía lo huella. Entra como `inputFile` de la entrada de su unidad, y el propio
`include_unclaimed_sources` entra en la huella del analizador: una entrada
escrita con la clave apagada no se sirve a una pasada que la tiene encendida.

## Consecuencias

Sobre el fixture `testdata/typescript/unclaimed-sources`, con la clave apagada
el payload es **idéntico byte a byte** al de antes del cambio (`md5
17f1ad9dd080e84ff498073d9960d0ff`). Con la clave encendida:

```text
                símbolos  referencias  ficheros
apagada                6            5         2
encendida             10            7         6
```

Y la arista que faltaba, con `sourceQualifiedName` propio y evidencia:

```text
tests/case.test.ts  CALLS_DIRECT  -> src/case.ts#getRequiredField
tests/case.test.ts  REFERENCES    -> tests/helpers/fixture.ts#record
```

Sobre el caso real, `workspace/packages/core` con
`tests/cluster/worker/ipc/utils/ipcCase.test.ts`: `4.835 -> 4.845` símbolos,
`14.100 -> 14.138` referencias, `0` no resueltos nuevos, y el test aparece como
llamante de `getField`, `getRequiredField` y `normalizeMessageData`, que es
justamente lo que `R3_ts_intra` no encontraba. El efecto sobre la puntuación
del benchmark no se ha medido: haría falta una pasada completa del corpus.

## Alternativas descartadas

- **Reescribir los `tsconfig` de los repositorios indexados.** Kivgraph no
  escribe dentro del código que indexa, y el `include` de un proyecto es una
  decisión de su dueño.
- **Un `tsconfig` sintético fuera del repositorio que incluya esos ficheros.**
  Habría que inventarle opciones de compilador igual que el proyecto inferido,
  con el coste añadido de un fichero que mantener y de decidir sus
  `paths` -- que la auditoría dice que nadie necesita.
- **Indexarlos con el árbol sintáctico en vez del checker.** Produciría
  `CANDIDATE` por coincidencia de nombre donde el checker da `EXACT`, y el
  motor ya sabe resolverlos de verdad.
- **Encenderlo por defecto.** Añade aristas ciertas bajo opciones de
  compilación que el proyecto no declaró; quien las publica debería haberlo
  pedido.
