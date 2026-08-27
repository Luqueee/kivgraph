# ADR 0046: El presupuesto en tokens del payload MCP

- **Estado:** aceptado e implementado; las cinco etapas medidas sobre `workspace`
- **Fecha:** 2026-08-18
- **Revisa:** qué paga un agente por cada respuesta de las tools, y qué se
  puede quitar sin perder una sola arista

## Contexto

`benchmarks/codebase-memory-comparison/` midió las mismas cuatro preguntas de
referencias contra `codebase-memory-mcp 0.8.1` sobre `workspace`. Kivgraph ganó en
exactitud -2 de 4 respuestas exactas contra 1, sin una sola arista falsa- y
perdió en coste: `13.586` tokens contra `3.217`, `4,2x` más caro. En dos de las
cuatro preguntas ni siquiera bajó del coste de leer y `grep`ear.

El motivo no es la cantidad de hechos, es cómo se serializan. Desglose por
campo, tokenizando cada `"clave":valor` de las respuestas reales
(`tiktoken` `o200k_base`):

```text
find_references getRequiredField      4.236 tok / 50 filas
  file_path 930 (22%)   provenance 650 (15%)   qualified_name 590 (14%)
  confidence 550 (13%)  edge_kind 499 (12%)    repository 408 (10%)
  name 363               kind 357               start_line 357
  end_line 350           language 350           next_cursor 223

find_symbol withRetry                 2.293 tok / 22 filas
  stable_key 885 (39%)  signature 360 (16%)    file_path 284 (12%)

get_blast_radius getRequiredField     5.103 tok / 50 filas
  file_path 912 (18%)   via_provenance 700     via_confidence 650
  qualified_name 583    reached_from 550       via_kind 499
```

Tres patrones, y ninguno aporta información:

1. **Constantes repetidas por fila.** `confidence` y `provenance` son `1.200`
   tokens en una sola respuesta y valen `EXACT_TYPECHECKED` /
   `TYPESCRIPT_CHECKER` en las 50 filas. Igual `repository` y `language`.
2. **Claves que el agente no usa.** `stable_key` es el `39 %` de `find_symbol`
   y todas las tools aceptan la tripleta `repository` + `path` +
   `qualified_name` en su lugar. `language` se deduce de la extensión.
   `next_cursor` son `221` tokens de base64 -`314` caracteres- por respuesta
   truncada.
3. **Una fila por ocurrencia.** Siete llamadas en la misma línea de
   `routes_players.rs` son siete filas con el mismo `repository`, `file_path`,
   `confidence` y `provenance`.

Y un cuarto, propio de `get_blast_radius`: de las `50` filas de la primera
página, `48` son **variables locales** (`handleBan.userId` y compañía). El
agente paga `5.103` tokens para ver asignaciones, y los llamantes reales caen
a la página siguiente.

## Decisión

Tres etapas. La primera no añade parámetros y se lleva la mayor parte; la
segunda cambia la superficie de entrada; la tercera cambia qué se considera
afectado. Las cifras son de aplicar cada transformación a las respuestas reales
capturadas en el benchmark, no estimaciones.

**Etapa 1 -- serializar sin repetirse.** Sobre la misma información:

- Los campos con un único valor en toda la respuesta suben a la cabecera:
  `confidence`, `provenance`, `repository`, `kind`, `edge_kind`.
- `stable_key` desaparece del payload por defecto y vuelve con
  `include_stable_keys: true`.
- `start_line` y `end_line` se colapsan en una línea cuando coinciden;
  `language` se retira; `name` se emite sólo cuando no es el último segmento de
  `qualified_name`.
- Las referencias se agrupan por archivo, con las ocurrencias como
  `[línea, arista, kind]` y `xN` para las repetidas.
- Nada nulo, nada `false`, `coverage` sólo con las categorías no vacías, y el
  cursor pasa a ser un identificador corto del snapshot en vez de un blob
  base64.

```text
find_references getRequiredField   4.236 -> 874 tok   (4,8x)
find_references now_ms             2.983 -> 354 tok   (8,4x)
find_symbol withRetry              2.293 -> 664 tok   (3,5x)
get_blast_radius                   5.103 -> 1.447 tok (3,5x)
find_cross_repo_consumers          2.457 -> 1.697 tok (1,4x)

las cuatro preguntas del benchmark: 13.594 -> 3.058 tok
```

`3.058` contra los `3.217` de `codebase-memory-mcp`, con la exactitud de hoy
intacta. `find_cross_repo_consumers` es el que menos baja porque su `detail` de
los no resueltos es texto que sí se lee.

**Etapa 2 -- no cobrar dos llamadas por una pregunta.** Hoy toda pregunta de
referencias cuesta `find_symbol` y luego `find_references`. `find_references`
acepta `name` a secas:

- una sola declaración con ese nombre: contesta directo;
- varias: devuelve un aviso con las declaraciones -`ruta:línea` por candidata,
  `49` a `144` tokens- en vez del listado completo de `find_symbol`, que
  incluye imports, re-exportaciones y firmas.

Y un `view` que declara la granularidad pedida: `full` es el payload de hoy,
`compact` el de la etapa 1, `files` sólo los archivos con su recuento.

```text
las cuatro preguntas, name-first + compact:   1.868 tok   (0,58x del rival)
las cuatro preguntas, name-first + files:       684 tok   (0,21x)
```

**Etapa 3 -- qué cuenta como afectado.** `get_blast_radius` excluye por defecto
las variables y los campos locales, que son ruido de traversal, y acepta
`kinds` para recuperarlos. La respuesta empieza por el resumen que ya calcula
-`by_kind`, `by_depth`, `by_repository`, `by_package`- y las filas se paginan
detrás.

```text
get_blast_radius, sólo símbolos invocables:   5.103 -> 172 tok
```

## Lo medido después de implementarlo

Las proyecciones de arriba se calcularon transformando las respuestas
capturadas. Esto es el mismo corpus preguntado al servidor con el código ya
dentro, misma generación `000003` de `workspace` y misma verdad de referencia
manual:

```text
las cuatro preguntas de referencias        13.594 -> 2.883 tok   (4,7x)
  con view=files                                     963 tok
`codebase-memory-mcp 0.8.1` sobre las mismas          3.217 tok

find_symbol withRetry, 22 filas            2.292 ->   901 tok   (2,5x)
get_blast_radius depth=2                   5.102 ->   921 tok   (5,5x)
get_file_outline de un directorio            633 ->   248 tok   (2,6x)
find_cross_repo_consumers, 35 filas        2.456 -> 2.202 tok   (1,1x)
get_source de dos símbolos                   201 ->   201 tok   (prosa, sin envoltorio)
cursor de una página truncada            314 car. ->    31 car.
```

`P` y `R` de las cuatro preguntas no se movieron: `Q1` sigue en `0,00`/`0,00`
-el corpus no tiene consumo por fuente entre repositorios-, `Q2` y `Q4` en
`1,00`/`1,00`, `Q3` en `1,00`/`0,89`. El ahorro salió de la serialización, no
de contestar menos.

Dos cifras se movieron en contra y se declaran: el coste fijo de `tools/list`
más `instructions` subió de `2.568` a `2.651` tokens -los tres parámetros
nuevos- y sigue por debajo de los `2.956` del rival; y
`find_cross_repo_consumers` apenas baja, porque el `detail` de sus filas no
resueltas es prosa que el agente lee.

`get_blast_radius` cambió de respuesta, no sólo de forma: de los `118`
afectados a profundidad 2, `29` son invocables y el resto eran bindings
locales. El filtro se declara en `kinds_default_excluded` y entra en la
identidad del cursor, así que una página vieja falla cerrado.

**Etapa 4 -- agrupar lo que aún se repite.** Medido después de la etapa 1:
`Q3` y `Q4` de `benchmarks/codebase-memory-comparison` seguían sin bajar del
coste de `grep`. La cabecera de página hoistea una columna sólo si **todas**
las filas coinciden; en `Q3`, `65` de `66` filas comparten `kind`+`edge_kind`
y la fila `66` -una re-exportación- bastaba para devolver esas dos columnas a
las `66` filas.

El arreglo es un segundo nivel: `groupByResidual` (`view.go`) agrupa las
filas por la tupla exacta de columnas que la página no pudo hoistear, y cada
grupo hoistea esa tupla una vez en vez de repetirla por fila. `find_references`
y el `compactReachedSymbols` compartido de `get_blast_radius` y
`trace_dependencies` lo usan. Una página cuyas filas no comparten nada -tres
saltos por tres aristas distintas es el caso normal de `trace_dependencies`,
no la excepción- paga más agrupada que sin agrupar: cada grupo es un objeto
con sus propias claves, y una fila sin nada que compartir sólo tenía que
pagar sus valores. Por eso `compactReachedSymbols` y `ReferenceResult.compact`
construyen las dos formas y serializan ambas para quedarse con la más barata,
en vez de asumir que agrupar gana siempre.

`reached_from` -qué símbolo llevó hasta esta fila- queda fuera de la tupla que
decide el agrupamiento: en una página de `29` filas, `27` nombraban `27`
símbolos distintos, y meterlo en la tupla fragmentaba cada grupo de vuelta a
una fila. Sigue intentando hoistear -a la página primero, al grupo después,
una vez fijado por las demás columnas- pero nunca decide a qué grupo
pertenece una fila.

```text
Q3 find_references (66 filas)     1.205 -> 788 tok   (1,5x adicional)
Q4 find_references (34 filas)     1.143 -> 779 tok   (1,5x adicional)
get_blast_radius depth=2 (29→9)     921 -> 821 tok   (1,1x adicional)
```

`get_blast_radius` gana menos: a diferencia del fixture sintético que probó el
diseño, la mayoría de sus filas reales sí tienen un `reached_from` distinto
cada una, así que ese campo se queda en la fila aunque el resto de la tupla
-`kind`, `via_kind`, `via_confidence`, `via_provenance`, profundidad- sí
hoistea al grupo.

**El bug que esta etapa encontró y corrigió.** La primera versión pasaba el
valor ya vaciado del grupo -`""` porque ya está en la cabecera de página- como
si nada se hubiera hoisteado, y `reachedFileGroups` devolvía `kind` a las
ocho filas de una prueba de `4+4`: agrupar costaba más que no agrupar, y el
comparador de bytes silenciosamente elegía la forma plana en cada
ejecución -sin fallar nunca, porque las dos formas eran válidas, sólo una
de ellas cara. Sólo apareció al escribir
`TestGetBlastRadiusGroupsFanInDespiteDivergingReachedFrom` con datos que
un `go/types`/checker de verdad produciría; ningún fixture anterior tenía
cuatro filas que compartieran página *y* grupo en la misma columna. El
arreglo distingue el valor que se muestra en el grupo -vacío cuando ya está
en la página- del valor contra el que se compara cada fila -que es el de la
página si hoisteó, si no el del grupo-.

**Etapa 5 -- el mismo segundo nivel en `find_symbol`, `get_file_outline` y
`find_cross_repo_consumers`.** La etapa 4 agrupó `find_references` y
`compactReachedSymbols` -compartido por `get_blast_radius` y
`trace_dependencies`-, pero el mismo defecto -una página que pierde el hoist
de cabecera por una minoría de filas disidentes- vive en las otras tres tools
compactas:

- `find_symbol` hoistea `kind` y `exported` por página hoy; una fila
  `interface` entre variables y funciones basta para que las `500` filas de
  un prefijo común paguen ambas columnas sin necesidad.
- `get_file_outline` hoistea `kind`, `visibility` y `signature`; un único
  método exportado entre variables no exportadas rompe el hoist para todo el
  directorio.
- `find_cross_repo_consumers` hoistea `category`, `edge_kind`, `confidence`,
  `provenance`, `evidence_kind` y `reason`; sobre `workspace`, una página de `35`
  consumidores mezcla dependencias de paquete (`PACKAGE_DEPENDS_ON`) con
  fallos de resolución (`UNRESOLVED`), así que ninguna columna hoistea y las
  `35` filas repiten las seis.

Mismo mecanismo que la etapa 4: `groupByResidual` agrupa por la tupla exacta
que la página no pudo hoistear y cada tool sirve la forma más barata entre
agrupada y plana, medida en bytes, nunca asumida. Tres implementaciones
nuevas porque cada tool hoistea columnas distintas -- no hay una firma común
que compartir como `compactReachedSymbols`.

`find_cross_repo_consumers` tenía además una asunción incorrecta: `detail` en
una fila `UNRESOLVED` se trataba como prosa propia de esa fila. Sobre `workspace`,
las filas que fallan por el mismo `reason` casi siempre comparten también el
mismo `detail` palabra por palabra -es la ruta al `.d.ts` que no se pudo
mapear, no una frase compuesta por fila-, así que agruparlo no pierde
información: expone que era una plantilla, no prosa.

```text
find_symbol, censo de 7 declaraciones            901 ->   773 tok
find_symbol, prefijo handle* (500 filas)      22.657 -> 18.678 tok
get_file_outline, 13 declaraciones               248 ->   248 tok (sin cambio)
get_file_outline, directorio completo          3.667 -> 3.184 tok
find_cross_repo_consumers, 35 filas            2.202 ->   926 tok
```

`get_file_outline` de `13` declaraciones no cambia: muy pocas tuplas
`(kind, visibility, signature)` se repiten, así que agrupar cuesta más que la
página plana y el comparador de bytes la deja como estaba -el mismo mecanismo
que ya protegía `Q1`/`Q2` de agrupar sin ganancia, aplicado aquí a una tercera
tool.

## Consecuencias

- Es un cambio incompatible en la forma de salida de `find_references`,
  `find_symbol`, `get_blast_radius`, `trace_dependencies`,
  `find_cross_repo_consumers` y `get_file_outline`: la raíz de `AGENTS.md` lo
  clasifica como superficie que rompe compatibilidad, y por eso vive aquí. Los
  nombres de las tools y sus entradas no cambian en la etapa 1.
- `view: "full"` conserva el payload actual, así que un cliente que dependa de
  la forma de hoy tiene una salida declarada y no un cambio silencioso.
- El coste fijo de contexto -`2.370` tokens de `tools/list` más `196` de
  `instructions`- no lo toca ninguna etapa. `index_project` es `796` de esos
  `2.370`, un tercio, y sólo se registra en la ruta `serve` configurada;
  acortar su descripción es la única ganancia disponible ahí y es pequeña.
- Lo que no cambia: qué aristas existen, con qué confianza y con qué
  procedencia. Ninguna etapa relaja el contrato de `EXACT`, y `confidence` y
  `provenance` siguen en la respuesta -en la cabecera cuando son homogéneas y
  en la fila cuando no-, porque son lo que distingue una respuesta de Kivgraph
  de una coincidencia de nombre.
- Riesgo: agrupar por archivo y deduplicar por `(línea, arista)` pierde el
  recuento exacto de ocurrencias dentro de una línea si se omite el `xN`. Se
  emite siempre que `N > 1`.
- Verificación: las mismas cuatro preguntas del benchmark, con la misma verdad
  de referencia manual, deben conservar `P` y `R` y bajar de `3.217` tokens.
  El harness ya está en `benchmarks/codebase-memory-comparison/`.
- `benchmarks/mcp-token-cost` se volvió a ejecutar sobre este mismo
  repositorio (generación `000001`, commit `f8a952d6` con el ADR aplicado).
  Responder cuesta entre `3,29x` y `11,95x` menos que `grep` más la lectura
  según la pregunta, `7,64x` en conjunto, contra el `3,46x` de antes; la
  sesión completa, con los cuerpos que ambos lados pagan igual, pasa de
  `1,29x`-`6,00x` a `1,26x`-`8,05x`. El propio harness confirma que `today` y
  `projected` ya coinciden: la fila compacta lleva su rango de líneas, así
  que la llamada extra a `get_symbol` que proyectaba desapareció, no se
  proyecta más. La tabla de enrutado de la raíz cita estas cifras.
- Indexar este repositorio para esa medición encontró un defecto de
  `internal/goloader` anterior al ADR y sin relación con él: dos campos de un
  struct anónimo escrito en línea dentro de una función -dos ramas de un
  `MarshalJSON`, o dos literales seguidos- derivan una sola identidad, y
  `LadybugDB` rechaza publicar con un `Node` y un desplazamiento, sin nombrar
  el símbolo. `internal/goloader/definitions.go` ya no emite esos campos
  -tan inalcanzables como el método de una interfaz sin nombre-, y
  `Set.Validate` rechaza ahora dos declaraciones de un mismo símbolo por su
  cuenta, nombrando el símbolo y los dos archivos, antes de llegar al
  storage. Sin este arreglo, indexar el propio `kivgraph` con el ADR 0046
  aplicado fallaba: es lo que hizo posible medir esta sección.
- La revancha de `benchmarks/codebase-memory-comparison` con la etapa 4
  dentro, misma generación `000003` de `workspace`, mismo rival sin tocar: las
  cuatro preguntas de referencias bajan de `2.883` a `2.214` tokens
  (`6,1x` contra el corpus sin el ADR, `0,69x` de los `3.217` del rival, antes
  `0,90x`); las nueve preguntas -las cuatro más el censo de declaraciones, el
  outline, el código de dos símbolos, el impacto y los consumidores
  cross-repo- de `7.356` a `6.587` tokens contra los `29.633` del rival,
  `4,50x` (antes `4,03x`). `P` y `R` no cambiaron.
- La revancha de `benchmarks/codebase-memory-comparison` con la etapa 5
  dentro, misma generación `000003` de `workspace`, mismo rival sin tocar: las
  cuatro preguntas de referencias bajan de `2.883` a `2.214` tokens
  (`6,1x` contra el corpus sin el ADR, `0,69x` de los `3.217` del rival); las
  nueve preguntas -las cuatro más el censo de declaraciones, el outline, el
  código de dos símbolos, el impacto y los consumidores cross-repo- de
  `7.356` a `5.183` tokens contra los `29.633` del rival, `5,72x` (antes
  `4,50x`); la sesión completa, con el coste fijo de cada lado, de `7.834`
  contra `32.589`, `4,16x`. `P` y `R` no cambiaron: agrupar decide cómo se
  escribe la respuesta, nunca qué arista existe.
