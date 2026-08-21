# Cinco grafos de código sobre el workspace `kena`

Comparación de coste en tokens y exactitud entre cinco herramientas que se
describen como grafos de código, más un sexto brazo: `grep` y leer los archivos,
que es lo que un agente ya tiene.

|herramienta|versión|
|---|---|
|kivgraph|`0.3.2`|
|graft (`@nanonets/graft`)|`0.10.1`|
|code-review-graph|`2.3.7`|
|graphify|`0.8.31`|
|codebase-memory-mcp|`0.8.1`|

Las métricas crudas están en `results.json` y las respuestas literales de cada
llamada en `raw/`, así que cualquier afirmación de parseo se puede comprobar
contra los bytes de los que salió. Este informe no emite un veredicto de
aceptación: mide seis maneras de contestar tres preguntas sobre un corpus
concreto, en un estado concreto de ese corpus.

## Entorno

|dato|valor|
|---|---|
|fecha|2026-08-21|
|commit|`4c1bfae`|
|corpus|`/Users/adria/Documents/programacion/projects/kena`, 37 repositorios git|
|máquina|Apple M5, macOS, `arm64`|
|toolchain|`go1.26.4`|
|tokenizador|`tiktoken` `o200k_base`|

## Por qué tres familias de preguntas

Cinco herramientas que comparten categoría no comparten pregunta. Preguntar sólo
«quién llama a esto» habría puesto a dos de ellas en cero por estar fuera de su
propósito y no por equivocarse: graphify es un BFS sobre un grafo extraído y
code-review-graph está construido alrededor del blast radius.

Así que hay tres familias, cada una es la pregunta que la documentación de alguna
de las cinco dice que contesta, y **cada herramienta se pregunta en su propio
vocabulario**: `callers_of` en una, `affected` en otra, `trace_path` o Cypher en
la tercera. Lo que se compara es la respuesta, nunca la ortografía: un archivo se
canonicaliza a `repositorio:ruta` antes de puntuar y un outline se compara como
conjunto de nombres.

|familia|preguntas|verdad|
|---|---|---|
|referencias|4, una por lenguaje y una cross-package|los archivos con una llamada o referencia|
|impacto|1, transitiva a 2 saltos|los archivos que alcanzan el sujeto|
|outline|2, un archivo grande y uno pequeño|los nombres declarados en el nivel superior|

La verdad de referencia es manual y comprobable a mano. Las cuatro de referencias
vienen de `benchmarks/codebase-memory-comparison` y se revalidaron en
`benchmarks/graft-comparison`. La de impacto usa una función Go **no exportada**,
cuyo alcance lo cierra el lenguaje: nada fuera de su paquete puede nombrarla, así
que dos saltos se enumeran leyendo tres archivos. Los dos outlines se enumeraron
leyendo el archivo.

El outline pequeño -- 3 declaraciones en 78 líneas -- está ahí a propósito: es
donde el `AGENTS.md` de este repositorio ya afirma que un índice cuesta más que
leer el archivo, y un benchmark que sólo preguntara el tamaño que le favorece
estaría midiendo su propia selección de preguntas.

## Aislamiento

Ninguna herramienta escribió dentro del corpus. Se verificó con `git status` en
los 37 repositorios antes y después: sólo siguen sucios los dos `go.sum` que ya
lo estaban.

|herramienta|dónde vive su estado|
|---|---|
|kivgraph|`HOME` aislado, reconstruido desde cero en cada pasada|
|graft|directorio de contexto fuera del corpus (`--dir`)|
|code-review-graph|base de datos en `--data-dir` fuera del corpus; su registro en un `HOME` aislado|
|graphify|**escribe `graphify-out/` junto al código que lee**, así que sólo corre contra una copia privada|
|codebase-memory-mcp|índice bajo un `HOME` aislado, así que el índice propio del usuario no se lee ni se sustituye|

Que graphify escriba dentro del árbol que analiza es la razón de que exista la
copia. No es un detalle de montaje: cualquiera que lo apunte a su repositorio se
encuentra un directorio nuevo dentro.

## El resultado

|herramienta|tokens|llamadas|`P`|`R`|exactas|
|---|---|---|---|---|---|
|**kivgraph**|`4.449`|11|**`0,81`**|`0,84`|**`4/7`**|
|graphify|**`2.469`**|9|`0,54`|`0,35`|`1/7`|
|graft|`8.942`|7|`0,14`|`0,14`|`1/7`|
|codebase-memory-mcp|`25.961`|21|`0,67`|`0,81`|`3/7`|
|code-review-graph|`109.298`|10|`0,67`|`0,85`|`3/7`|
|`grep` + lectura|`63.531`|27|`1,00`|`1,00`|`7/7`|

`P` es precisión -- de lo que dijo, qué fracción era cierta -- y `R`
exhaustividad -- de lo que existía, qué fracción encontró --, a nivel de archivo
para referencias e impacto y de nombre para outline, excluyendo el archivo que
declara el sujeto.

Dos lecturas que conviene no mezclar:

- **`grep` más leer acierta las siete.** Es el denominador honesto: la
  alternativa real a estas herramientas no es equivocarse, es gastar `63.531`
  tokens. Ninguna de las cinco iguala su exactitud.
- **Entre las cinco, la más barata no es la más exacta.** graphify cuesta
  `2.469` tokens y contesta una de siete; kivgraph cuesta `4.449` y contesta
  cuatro. El token por respuesta correcta favorece a kivgraph por `2,2x` sobre
  graphify y por `33x` sobre code-review-graph: `1.112` por acierto contra
  `2.469` y `36.433`.

### Por familia

**Referencias** -- «qué archivos llaman a esta declaración»:

|herramienta|tokens|`P`|`R`|exactas|
|---|---|---|---|---|
|kivgraph|`2.479`|`0,75`|`0,72`|`2/4`|
|graphify|`1.993`|`0,44`|`0,28`|`0/4`|
|graft|`7.736`|`0,25`|`0,25`|`1/4`|
|codebase-memory-mcp|`13.768`|`0,58`|`0,67`|`1/4`|
|code-review-graph|`24.284`|`0,67`|`0,75`|`2/4`|
|`grep` + lectura|`57.619`|`1,00`|`1,00`|`4/4`|

**Impacto** -- «qué alcanza a `expBackoffJitter` en dos saltos»:

|herramienta|tokens|`P`|`R`|
|---|---|---|---|
|kivgraph|`1.097`|`0,67`|`1,00`|
|graphify|`57`|`0,00`|`0,00`|
|graft|`371`|`0,00`|`0,00`|
|codebase-memory-mcp|`1.768`|`0,33`|`1,00`|
|code-review-graph|`82.057`|`0,01`|`1,00`|
|`grep` + lectura|`897`|`1,00`|`1,00`|

**Outline** -- «qué declara este archivo»:

|herramienta|tokens|`P`|`R`|exactas|
|---|---|---|---|---|
|kivgraph|`873`|`1,00`|`1,00`|`2/2`|
|graphify|`419`|`1,00`|`0,68`|`1/2`|
|codebase-memory-mcp|`10.425`|`1,00`|`1,00`|`2/2`|
|code-review-graph|`2.957`|`1,00`|`0,97`|`1/2`|
|graft|`835`|`0,00`|`0,00`|`0/2`|
|`grep` + lectura|`5.015`|`1,00`|`1,00`|`2/2`|

## El hallazgo: tres de cinco resuelven por nombre

`withRetry` se declara **siete veces** en `kena`, en tres lenguajes. `now_ms`,
cuatro. Ese es el caso que separa las cinco herramientas, y tres fallan igual:

- **codebase-memory-mcp**: sus aristas `CALLS` apuntan al nombre, no al símbolo,
  así que los llamantes de los siete `withRetry` colapsan en un nodo. En `R2` (Go)
  recupera los dos archivos correctos y arrastra cuatro de TypeScript: `P=0,33`.
  Peor en `R1`: el nodo TypeScript de `library-shared` queda con grado de entrada
  cero porque todos los llamantes se ataron al homónimo Go, y la respuesta es
  vacía.
- **code-review-graph**: desambigua el **sujeto** -- se niega a elegir y nombra
  los dos candidatos con archivo y línea, igual que kivgraph -- pero no los
  sitios de llamada. Acotado al `withRetry` de `postgres` devuelve además
  `internal/shared/infisical/infisical_test.go`, que llama al otro: imposible en
  Go, son paquetes distintos y la función no está exportada. `P=0,67`.
- **graft**: ante un nombre ambiguo **descarta el llamante cross-file y avisa de
  que puede infracontar**. Es la política opuesta a inventar una arista, y es
  honesta, pero deja `R2` y `R4` en cero.

kivgraph resuelve `R2` y `R4` exactas porque las aristas las produce `go/types`,
el checker de TypeScript y `rust-analyzer`, no la coincidencia de nombres.

## Dos cosas que las cifras dicen y no son un error

**Los `82.057` tokens de code-review-graph en impacto no son un fallo, son otra
pregunta.** Su `impact` toma archivos cambiados, no declaraciones: `--files
retry.go::expBackoffJitter` responde «0 nodos cambiados», así que hay que
preguntarle por el archivo. A dos saltos eso son `390` nodos en `255` archivos,
y contra una verdad de dos archivos la precisión sale `0,01`. Encuentra los dos
-- `R=1,00` --; lo que mide ese `0,01` es la granularidad, no el acierto.

**El `affected` de graphify no es alcanzabilidad inversa.** Su `graph.json` se
escribe `"directed": false`, así que networkx carga un grafo no dirigido, sin
`in_edges`, y el fallback recorre las aristas cuya orientación almacenada acaba
en el nodo semilla -- y esa orientación es el orden de inserción de nodos, que es
el orden de recorrido de ficheros, no la dirección de llamada. La evidencia es
suya: `affected` sobre `withRetry` devuelve sus *llamados*, sobre
`expBackoffJitter` no devuelve nada pese a tres llamadas entrantes, y el mismo
comando en `packages/core` devuelve 43 llamantes genuinos. Eso explica su `R=0,28`
en referencias y su cero en impacto.

Además, cuando un nombre es ambiguo `affected` responde `No unique node match` y
**no nombra los candidatos**; el único identificador que acota es el id interno
de graphify (`{directorio}_{fichero}[_{nombre}]`), que su propia skill no
documenta.

## Coste de entrada

Todas las indexaciones se midieron en frío, borrando el estado derivado antes de
cada una, porque «frío» tiene que significar lo mismo en cada fila: graft tardó
`2,6 s` sobre un contexto ya construido y `24,6 s` sobre ninguno.

|herramienta|frío|disco|alcance|necesita|
|---|---|---|---|---|
|codebase-memory-mcp|`5,1 s`|`221 MB`|corpus entero|nada|
|code-review-graph|`7,2 s`|`201 MB`|un grafo por repositorio|nada|
|graphify|`11,1 s`|--|un grafo por repositorio|nada para la pasada estructural|
|graft|`24,6 s`|`181 MB`|corpus entero|nada para el tier estructural|
|kivgraph|`37,6 s`|`1.423 MB`|corpus entero, `96.482` símbolos|caché de módulos de Go y `cargo`|

kivgraph es el más caro de las cinco por los dos lados: `1,5x` el tiempo del
siguiente y `6,5x` el disco. Y es el único que **exige un toolchain**: sin la caché
de módulos de Go o sin `cargo` el load falla y sus símbolos quedan ausentes.

Dos consecuencias del alcance por repositorio, que afectan a crg y a graphify: su
`build` es completo y **trunca** el directorio de datos que recibe, así que hace
falta uno por repositorio; y una referencia cross-package es estructuralmente
invisible, que es el cero de `R1` para las dos.

## Dónde pierde kivgraph

- **Indexación y disco**: el más caro de los cinco, y el único con dependencia de
  toolchain.
- **`R1`, cross-package**: cero, como las otras cuatro. `kena` consume sus
  repositorios como paquetes publicados y no como fuente, así que la respuesta no
  es alcanzable para nadie; la pregunta se conserva justamente por eso.
- **`R3`**: `R=0,89`. Falta
  `packages/core/tests/.../ipcCase.test.ts` porque el `tsconfig.json` de
  `packages/core` no lo incluye y el checker no lo ve. El tree-sitter de graft y
  de crg sí, y ahí ganan.
- **Impacto**: `P=0,67`, un archivo de más frente al `1,00` de leer.
- **Ninguna herramienta iguala a `grep` más leer en exactitud**, y la nuestra
  tampoco: `4/7` contra `7/7`.

## Limitaciones

- Siete preguntas sobre un corpus y una máquina. No es una medida de calidad
  general de ninguna de las cinco.
- El tokenizador es `o200k_base`, un proxy del de Claude: los cocientes entre
  filas son la afirmación, los valores absolutos no.
- Una sola pasada por medición. graft varía hasta un `9 %` en tokens entre
  builds del mismo árbol, medido en `benchmarks/graft-comparison`; las demás no
  se probaron para no-determinismo.
- Sólo los tiers gratuitos y sin modelo. `graft --deep`, la pasada semántica de
  graphify y los embeddings de crg necesitan clave de proveedor y no se midieron.
  Es la ausencia declarada de este informe.
- crg y graphify indexaron los 4 repositorios que las preguntas nombran, no los
  37, porque su grafo es por repositorio y construir el resto habría tarifado un
  índice que ninguna pregunta lee.
- Las secuencias las fijó el harness. Cada una es la más barata que contesta bien
  con la superficie que la herramienta documenta; otro agente con otras llamadas
  mediría otro coste.
- kivgraph se midió con su vista por defecto, no con la vista `files` que
  contesta las mismas preguntas de referencias por un tercio de los tokens
  (`benchmarks/graft-comparison`). Tomar el descuento que sólo nosotros tenemos
  habría comparado nuestro resumen contra el detalle de los demás.

## Reproducir

```bash
pip install code-review-graph        # 2.3.7, en un venv aislado
npm install -g @nanonets/graft       # 0.10.1

# graphify escribe dentro del árbol que lee: copia privada, nunca el corpus
rsync -a --delete --exclude=node_modules /ruta/a/kena/ /private/tmp/5way/kena-copy/

go run ./benchmarks/graph-tools-comparison
```

El harness registra los 37 repositorios en un `HOME` aislado, indexa las cinco
herramientas en frío, hace las siete preguntas a las seis, escribe `results.json`
y `raw/`, y su salida por pantalla son las tablas de este informe.
