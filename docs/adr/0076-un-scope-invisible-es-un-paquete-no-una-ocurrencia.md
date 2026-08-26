# ADR 0076: un scope invisible es un paquete, no una ocurrencia

- **Estado:** aceptada
- **Fecha:** 2026-08-26
- **Cambia el protocolo MCP:** no
- **Cambia el schema persistente:** no
- **Obliga a reconstruir:** no
- **Cambia una salida de tool:** sí -- `completeness.invisible_scopes` pasa a
  traer una fila por scope, con un campo `occurrences` nuevo, y esas filas dejan
  de llevar `requested_symbol` y `start_line`

## Lo que pasaba

La primera corrida completa de `benchmarks/mcp-token-cost` tras recapturar sus
lecturas -- `LUQUE-2227`, primer punto-- midió el titular en `0,63x`: nuestro
brazo costaba `1,6x` **más** que `grep` más las lecturas. Contra la generación
`000001` ese mismo titular era `7,64x` a favor.

La respuesta no había engordado. Medido sobre `find_references` de `MergeAll`,
en tokens `cl100k_base` de las dos llamadas que una sesión hace:

|                        | `find_symbol` | `find_references` | total |
| ---------------------- | ------------: | ----------------: | ----: |
| con el bloque          |       `1.580` |           `1.639` | `3.219` |
| sin el bloque          |          `89` |             `148` |  `237` |
| publicado en `000001`  |               |                   |  `228` |

`237` contra `228`: la respuesta cuesta hoy lo que costaba. Todo lo demás era
`completeness`, y era **el mismo bloque en las dos llamadas** -- se pagaba dos
veces por pregunta, unos `3.000` tokens de los que ninguno hablaba del símbolo
preguntado. Con `NewServer`, que no tiene ninguna referencia, el `98 %` de lo que
el agente pagaba era texto que no hablaba de `NewServer`.

La causa no era el tamaño sino la repetición. `UnresolvedScopes` devolvía una
fila **por ocurrencia**, y las veinte que caben en una respuesta se reducían a
**dos** tuplas `(reason, requested_package, detail)`: todas
`DECLARATION_OUTSIDE_REPOSITORY` sobre `internal/procstat`, cada una arrastrando
la misma frase en prosa una vez por símbolo cgo. Sobre este repositorio hay `165`
fallas de ese tipo, así que la lista se llenaba con veinte y declaraba
`more_invisible_scopes: 145`.

Y el propio fichero ya decía cuál era la unidad, antes de este cambio:

> a row without [a file] is a **scope** it could not read at all -- a package
> excluded by build tags, a module the loader never loaded.

## La decisión

Una fila por scope distinto, con `occurrences` diciendo cuántas fallas hay
detrás. `UnresolvedScopes` se retira y en su lugar queda
`UnresolvedScopeGroups`, que agrupa por
`(repository, reason, requested_package, detail)` -- identificadores internados,
así que agrupar cuesta una consulta de mapa por falla y nunca construye una
cadena.

Las filas agrupadas **no** llevan `requested_symbol` ni `start_line`. Pertenecen
a una de las fallas agrupadas, y publicar una de veinte se lee como si esa
importara; el contador es lo que un lector puede usar.

**Lo que no se toca, y es el motivo de que el bloque exista:** una lista vacía
significa «nadie lo llama», y `LOWER_BOUND` con sus scopes es lo que impide
confundir eso con «no se encontró». Agrupar conserva la afirmación entera --
qué paquetes no se pudieron leer, y cuánto hay en cada uno--; borrar el bloque la
habría destruido. Ver ADR 0059 y el caso `X1` de
`benchmarks/graph-tools-comparison`, donde un consumidor real nunca deletrea el
símbolo.

## Lo medido después

Las mismas dos llamadas, contra un binario con el cambio:

|                        | scopes | `more` | tokens |
| ---------------------- | -----: | -----: | -----: |
| antes                  |   `20` |  `145` | `3.219` |
| después                |    `5` |    `0` |   `991` |

Las `165` fallas son **cinco** paquetes. La respuesta no sólo baja a un tercio:
deja de truncarse, porque la verdad entera cabe. El titular del arnés pasa de
`0,63x` a `1,53x`, y `Publish` -- la pregunta con más referencias-- de `2,00x` a
`4,98x`.

## Un defecto contiguo, arreglado con él

`scopeDirectory` recuperaba el directorio de un scope cortando el `detail` por el
último `" in "`. Con «declared **in** a Go build cache entry: the package is
built from generated or cgo sources» eso devolvía prosa, y esa prosa se publicaba
como una ruta del `fallback`. El `fallback` es una acción de recuperación: una
ruta que no existe es peor que ninguna, porque manda a barrer algo que no está.
Sólo se acepta una ruta absoluta, que es lo que escribe el loader.

## Compatibilidad

Un cliente que leyera `requested_symbol` o `start_line` de una fila de
`invisible_scopes` ya estaba leyendo una ocurrencia arbitraria. `occurrences` es
`omitempty` y ausente en un `blind_spot`, que es una ocurrencia por definición.
Ningún consumidor del repositorio los leía: los tres llamantes son
`completenessFor`, `completenessOutwardFor` y su variante de recorrido, todos en
`internal/mcp/tools/completeness.go`.

Cierra `LUQUE-2229`.
