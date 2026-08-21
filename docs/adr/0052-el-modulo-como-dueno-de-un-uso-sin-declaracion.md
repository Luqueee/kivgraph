# ADR 0052: El módulo como dueño de un uso sin declaración

- **Estado:** aceptada
- **Fecha:** 2026-08-21

## Contexto

Una arista necesita una declaración en los dos extremos. El checker resuelve
perfectamente el destino de estos usos; lo que faltaba era el origen:

```ts
describe("ipcCase utils", () => {
  it("throws when the field is missing", () => {
    expect(() => getRequiredField({}, "userId")).toThrow(/Missing/);
  });
});
```

La llamada vive dentro de una función anónima pasada como argumento. Una
función anónima no es una declaración, así que `findOwner` en
`reference-extractor.ts` -que busca el símbolo extraído cuyo rango contiene el
uso, el más estrecho- no encontraba ninguno y el worker emitía `source:
undefined`. `internal/facts/typescript.go` descartaba la referencia y la contaba
en `EdgesWithoutSource`.

El mismo agujero existía en Go, donde `goloader/uses.go` documenta
`SourceQualifiedName` como «the enclosing declaration, **empty at file
level**»: el uso en el inicializador de una variable de paquete no tiene dueño.

**Medido** sobre `packages/core` del monorepo `kena`, con `facts-cli`:

|población|usos perdidos|
|---|---|
|código ordinario (`src/**`)|`98` de `14.100` -- `0,7 %`|
|un fichero de test|`38` de `38` -- `100 %`|

Los 98 no eran tests: estaban en `src/cluster/worker/index.ts` (54),
`src/cluster/master/index.ts` (24) e `src/index.ts` (16), sentencias de arranque
de nivel superior. Los 38 sí lo eran, y son **todos** los que hace ese fichero:
el idioma de `vitest` y `jest` mete cada llamada dentro de un callback.

Eso convertía las dos palancas de inclusión de tests -`go.include_tests` y
`typescript.include_unclaimed_sources`- en promesas a medias: el fichero entraba
al grafo con sus declaraciones, y sus llamadas no.

## Decisión

El ámbito de nivel superior de un fichero es una declaración como cualquier
otra, y es la dueña de un uso que nada más estrecho contiene.

- Un fichero recibe un símbolo sintético de clase `module` cuando -y sólo
  cuando- un uso suyo lo necesita. La creación es perezosa: el símbolo existe
  donde hace falta y en ningún otro sitio.
- Su nombre cualificado es la ruta relativa al repositorio con los separadores
  vueltos puntos y la extensión quitada: `tests/case.test.ts` es
  `tests.case.test`. Su nombre es el nombre base del fichero.
- Su clave estable sale del mecanismo que ya existía,
  `hotsnapshot.StableKeyIdentity`, con `Kind: "module"` y `Module` puesto a la
  ruta del fichero: dos ficheros que sólo difieren en la extensión no colisionan.
- La arista `DEFINES` del fichero a su módulo es `StructuralCertain` /
  `PackageManifest`, la misma pareja que `CONTAINS_FILE`. El ámbito existe
  porque el fichero existe; ningún checker declaró este símbolo y a ninguno se
  le acredita.
- La arista del uso conserva su propia confianza y procedencia. El checker sí
  resolvió el destino, y eso no cambia porque el origen sea el módulo.

La convención no es nueva: `internal/dartloader` ya emite un símbolo `module`
por fichero para una biblioteca Dart, `module` ya estaba en el vocabulario de
clases de `blast_radius.go` y `root_symbol.go`, y `facts.PartOf` ya relaciona
dos de ellos. Esto lo extiende a los lenguajes que no tenían nombre para eso.

### Lo que separa los dos casos

Un origen **vacío** significa que no hay declaración que encierre el uso, y su
módulo lo posee. Un origen **con nombre** que este payload no declara es una
inconsistencia, y fabricarle un dueño taparía una pérdida real: sigue contando
en `EdgesWithoutSource`. Hay un test negativo por cada mitad.

## Consecuencias

- `R3_ts_intra` pasó de `P=1,00 R=0,89` a **exacta**, y `N2_ts` -una pregunta
  ordinaria de la remedición ciega, ajena a este trabajo- de `0,00/0,00` a
  **exacta**. Ninguna otra se movió.
- Una respuesta de referencias puede nombrar ahora un símbolo `module`. A la
  granularidad de archivo, que es la que pide `view: "files"`, la respuesta es
  idéntica a la de un llamante nombrado.
- **La etiqueta de una fila compacta pierde valor cuando el dueño es el
  módulo.** `at` es `nombre_cualificado@línea` de la declaración que sostiene la
  referencia, así que un llamante nombrado da `handleFetch.guild_id@42` y el
  módulo da su propio nombre en la línea `1`, repetido una vez por uso: cuatro
  llamadas en `ipcCase.test.ts` producen cuatro etiquetas idénticas. Repetir
  etiqueta no es nuevo -`handleUserIPC@85-121` ya salía dos veces para un
  llamante nombrado-, pero esto lo vuelve frecuente. Colapsar las repetidas en
  un recuento es lo que la propia documentación de `files()` dice que quiere
  («a repeated caller in one file is a count and not a row») y es un cambio de
  la superficie MCP, así que no se hace aquí. Queda escrito.
- Un `get_file_outline` puede mostrar una fila `module`. Es lo que ya hacía Dart
  y no se le añade una excepción: la creación perezosa la vuelve poco frecuente.
- Un radio de impacto puede alcanzar un `module`. Es un consumidor legítimo -el
  código de nivel superior de ese fichero llega al símbolo- y reportarlo es más
  cierto que omitirlo.
- El grafo crece. Sobre `kena` con las dos palancas activas, un `+0,4 %` de
  símbolos: el símbolo sólo se crea donde un uso lo pidió.
- **Exige reconstrucción completa.** No cambia el schema ni el algoritmo de las
  claves estables, pero añade símbolos y aristas que un índice anterior no
  tiene.

## Alternativas descartadas

**Extraer el callback anónimo como símbolo.** Daría una respuesta mejor -«este
*test* lo llama», no «este fichero»- pero el problema es la identidad. El
literal de `describe`/`it` es estable y legible, y es una heurística de
`vitest`/`jest`; un nombre posicional se mueve con cada edición, y las claves
estables son superficie de compatibilidad. Compone con esta decisión: esto es el
suelo, aquello un refinamiento del nombre.

**Que una arista pueda empezar en un `File`.** Es lo más fiel, y rompe el
invariante de que una arista va de símbolo a símbolo, más todo consumidor que
resuelve `SourceKey`. El mayor radio de impacto por la menor ganancia.

**Dejarlo documentado.** Es lo que estaba, y costaba el `100 %` de las llamadas
de cada fichero de test.

## Lo que queda fuera

Rust. Su cargador SCIP descarta el uso sin origen **dentro** de
`analyze_references.go` y lo cuenta en `Analysis.ReferencesWithoutSource`, así
que el normalizador nunca lo ve: el arreglo vive en otro subsistema. Su pérdida
es sólo de nivel de fichero -un test de Rust vive en `#[test] fn nombre`, que sí
es una declaración-, y su pregunta de referencias ya era exacta antes y después.
Se declara aquí para que la ausencia esté escrita y no descubierta.
