# ADR 0038: Identidad de proveedor sin declaration map

- **Estado:** aceptada
- **Fecha:** 2026-08-12

## Contexto

Una arista `IMPORTS_SYMBOL`, `REEXPORTS` o `EXTENDS` que cruza repositorios
necesita la identidad que el proveedor se da a sí mismo. El consumidor la
resuelve en tres pasos: su checker resuelve el import hasta un `.d.ts`, ese
artefacto se puentea a la fuente del proveedor, y el proyecto del proveedor
clasifica la declaración con el mismo código que usa al indexarse.

El segundo paso tenía dos implementaciones con alcances distintos:

- `declaration-source-resolver.ts` nombra el **archivo** fuente por cuatro
  vías: el `.d.ts.map`, las raíces del proyecto del proveedor, las del
  registro y la transformada `rootDir`/`outDir`.
- `resolveTargetIdentities` exigía además una **posición** dentro de ese
  archivo, y la única vía que la produce es el `.d.ts.map`.

El resultado era que un proveedor que compila con `declaration: true` y sin
`declarationMap` -la configuración por defecto de `tsc`- no producía ninguna
arista de símbolo, aunque su fuente estuviera nombrada y su proyecto abierto.
Todo consumidor quedaba como `PROVIDER_SOURCE_UNAVAILABLE` con el detalle «no
declaration map places this symbol in the provider's source».

En un monorepo pnpm real -40 repositorios, `@private/shared` consumido por 17-
eso son 89 ficheros en 14 repositorios sin una sola arista de símbolo, y el
grafo respondiendo con las dependencias de paquete como si fueran consumidores.

Ninguno de los seis fixtures TypeScript lo detectaba: todos resuelven al
proveedor con `paths` apuntando a su propio `dist/`, y el caso `nomap` -sin
declaration map- se apoyaba en `resolveProviderSourcePositions`, un resolutor
escrito y probado que **la ruta de producción nunca llamaba**: su único
llamador era el arnés de precisión.

## Decisión

Cuando ningún `.d.ts.map` coloca el símbolo pero el puente sí nombró la
fuente, se le pregunta al checker **del propio proveedor** qué declaración
exporta ese módulo bajo el nombre solicitado.

La respuesta viene del compilador del repositorio que posee el código, sobre
un archivo que la configuración de compilación de ese repositorio ya asoció a
su artefacto. No es coincidencia de nombre entre paquetes: el nombre es el que
el consumidor importó, la búsqueda es `getMemberInModuleExports` en el
programa del proveedor, y el archivo no se adivina -si el puente no nombró
ninguno, no hay nada que preguntar y la referencia sigue siendo `UNRESOLVED`.

### Las dos pruebas no valen lo mismo

El paso que cambia es el de artefacto a fuente. Con `.d.ts.map` lo afirma el
propio artefacto; sin él, lo afirma la configuración de compilación del
proveedor -que `dist/x.d.ts` salió de `src/x.ts`-. Es una premisa razonable y
verificable, pero es una premisa.

Por eso el grafo las distingue en vez de igualarlas:

| Posición obtenida de | `Confidence` | `Provenance` |
| --- | --- | --- |
| el `.d.ts.map` del artefacto | `EXACT_TYPECHECKED` | `TYPESCRIPT_CHECKER` |
| el checker del proveedor | `EXACT_PACKAGE_MAPPED` | `TYPESCRIPT_PROJECT_REFERENCE` |

Las dos son exactas -`Confidence.Exact()` es cierto para ambas- porque en las
dos la clave destino la compone `typeScriptSymbolIdentity` sobre la clase y la
firma leídas de la fuente del proveedor. Se diferencian en lo que un consumidor
del grafo tiene que dar por bueno, y el grafo lo dice.

Ambos códigos ya existían en el vocabulario y en el catálogo canónico
(`EXACT_PACKAGE_MAPPED` = 3, `TYPESCRIPT_PROJECT_REFERENCE` = 4): esta decisión
los usa, no los introduce.

### Transporte

`TypeScriptImportTarget` gana un campo opcional `source`, con valores
`DECLARATION_MAP` y `PROVIDER_EXPORT`. El payload sigue siendo `ts-facts-v4`:
el campo es aditivo y su ausencia significa `DECLARATION_MAP`, que es lo que
lleva todo payload grabado antes de que esta ruta existiera. No hay migración
de datos: el grafo se reconstruye desde los repositorios fuente, y la caché de
hechos ya invalida por contenido del worker.

## Alternativas descartadas

- **Exigir `declarationMap` en los proveedores.** No es de Kivgraph la
  configuración de los repositorios que indexa, y medido sobre el corpus no
  habría funcionado: un paquete instalado desde el registro no lleva el mapa
  hasta que se publica una versión nueva, y aunque lo llevara, sus `sources`
  apuntan a un `src/` que el tarball no publica. Publicándolo también, la
  posición cae dentro de la copia instalada y el proyecto del proveedor no
  reconoce ese archivo.
- **Componer la clave del destino desde el `.d.ts`.** El texto de una
  declaración emitida no es el de la fuente, así que la firma -y con ella la
  clave- diferiría de la que el proveedor se asigna. Sería una arista colgante
  con aspecto de correcta.
- **Emitir `CANDIDATE`.** Convierte una resolución del compilador del
  proveedor en una sugerencia, y deja al consumidor sin la única respuesta que
  pidió.
- **Llamar a `resolveProviderSourcePositions` como una segunda pasada.**
  Abriría el proyecto del proveedor dos veces por unidad de análisis:
  `resolveTargetIdentities` ya lo tiene abierto y agrupado por proyecto. La
  primitiva se comparte (`locateProviderExport`); el arnés de precisión sigue
  usando la API por lotes.

## Consecuencias

- Un proveedor con la configuración por defecto de `tsc` produce aristas de
  símbolo. Sobre el corpus de referencia eso son 89 ficheros en 14
  repositorios que antes no tenían ninguna.
- El grafo publica dos grados exactos donde antes publicaba uno. Un consumidor
  que solo acepte prueba de `.d.ts.map` filtra por `EXACT_TYPECHECKED`.
- Los re-exports de barril cruzando repositorios (`export * from "pkg"`) se
  benefician igual: comparten la misma maquinaria de identidad.

## Riesgos

- **Deriva entre el artefacto y la fuente.** Si el `.d.ts` se compiló desde un
  árbol distinto al indexado, la firma leída de la fuente no describe lo que
  el consumidor compiló. El caso tiene ya su propio motivo declarado
  (`VERSION_MISMATCH`) cuando las versiones no coinciden, pero dos árboles con
  la misma versión no se distinguen. Es exactamente el riesgo que separa
  `EXACT_PACKAGE_MAPPED` de `EXACT_TYPECHECKED`.
- **Un export reexportado dentro del proveedor.** `locateProviderExport` sigue
  el alias hasta su declaración, así que la identidad es la de la declaración
  real y no la del barril. Un módulo que exporta dos declaraciones con el
  mismo nombre no existe en TypeScript.

## Verificación

`imported-symbol-identity.test.ts` defiende los tres estados sobre fixtures
reales: identidad con `DECLARATION_MAP` sobre `shared-library`, identidad con
`PROVIDER_EXPORT` sobre `nomap` -que no publica mapa-, y **ninguna** identidad
sobre `unmapped`, que no publica fuente que nombrar.

`consumer-linked/` es el fixture que faltaba: resuelve al proveedor por un
symlink de `node_modules`, la forma que instala un gestor de paquetes, en vez
de por `paths`. Es la única vía por la que el motor devuelve la ruta del
destino del enlace, y es donde el agujero vivía.

`TestNormalizeTypeScriptGradesCrossRepositoryTargetsByEvidence` fija los dos
grados y el significado del campo ausente.
