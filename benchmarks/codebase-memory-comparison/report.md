# Kivgraph contra `codebase-memory-mcp` sobre el workspace `kena`

Comparación de coste en tokens y de exactitud entre `kivgraph 0.2.1`
(commit `67338a42b0d652db1c3edce47d8b262cb07cc06f`) y
`codebase-memory-mcp 0.8.1`, respondiendo las mismas preguntas estructurales
sobre `/Users/adria/Documents/programacion/projects/kena`.

Las métricas crudas están en `results.json`. Este informe no emite ningún
veredicto de aceptación: mide dos herramientas sobre un corpus concreto y en
un estado concreto de ese corpus.

## Entorno y provenance

|dato|valor|
|---|---|
|fecha|2026-08-18|
|commit del repositorio|`f8a952d`|
|máquina|`Mac17,2` (Apple M5), 10 CPU, macOS `26.6`|
|toolchains|`go1.26.4`, `cargo 1.96.1`, `rust-analyzer 0.3.3008` (bundled), `node v25.2.1`|
|tokenizador|`tiktoken` `o200k_base`|
|transporte|MCP sobre `stdio`, cliente JSON-RPC propio del harness|

El estado de Kivgraph se creó en un `HOME` aislado (`/tmp/kivbench-home`): la
generación `000088` del usuario y su registro de repositorios no se tocaron.
`codebase-memory-mcp` guarda su índice en `~/.cache/codebase-memory-mcp/`. Ni
uno ni otro escribe dentro de `kena`.

## Dataset

`kena` es un monorepo poliglota de 37 repositorios git independientes,
`5.302` archivos de código y ~`840k` líneas: TypeScript `640k`
(`.ts` + `.tsx`), Go `134k`, Rust `53k`.

Estado relevante: las dependencias pnpm están instaladas y los repositorios de
`kena` **no se consumen entre sí por fuente**. Cada uno declara
`"@kena/shared": "0.0.1"`, un rango de registro, y pnpm materializa una copia
del paquete publicado en `node_modules/.pnpm/`, con su `dist` y sin `src`. Es
una propiedad del corpus, no de las herramientas, y es lo que explica `Q1` y
la sección de cross-repository.

Kivgraph se registró con los 36 repositorios que contienen código y los
lenguajes `go`, `typescript`, `rust`, con `go.include_tests=true` y
`rust.include_tests=true` para igualar a `codebase-memory-mcp`, que indexa los
tests siempre.

## Coste de indexación

|métrica|Kivgraph|`codebase-memory-mcp`|
|---|---|---|
|full index en frío|`41,8 s`|`11,4 s`|
|reindexado con caché|`26,5 s`|`5,3 s`|
|símbolos / nodos|`96.923`|`67.120`|
|aristas|`368.166`|`235.905`|
|aristas de llamada|-- (`260.489` evidencias)|`63.021` `CALLS`|
|almacenamiento|`1,4 GB` de estado (`487 MB` generaciones, `267 MB` caché de hechos, `307 MB` target de Rust, `512 MB` de reserva)|`210 MB` de SQLite|
|referencias no resueltas|`22.929` (retenidas y consultables)|no se reportan|

Tres reconstrucciones completas dieron conteos idénticos
(`96.923` símbolos, `368.166` aristas) en Kivgraph.

`codebase-memory-mcp` indexa `3,7x` más rápido y ocupa `6,7x` menos disco;
Kivgraph carga `go/types`, el checker de TypeScript y `rust-analyzer`, y por
eso paga esa diferencia.

## Coste fijo de contexto

|          |herramientas|tokens de `tools/list` + `instructions`|
|---|---|---|
|Kivgraph|11|`2.568`|
|`codebase-memory-mcp`|14|`2.956`|

Es el coste que se paga en cada sesión antes de preguntar nada.

## Las cuatro preguntas medidas

Cada pregunta es «qué llama a este símbolo concreto», con la declaración
identificada por repositorio, ruta y línea. La secuencia por sistema es la
que haría un agente: buscar el símbolo, y sobre la fila que coincide con el
archivo, pedir las referencias.

- Kivgraph: `find_symbol` → `find_references`.
- `codebase-memory-mcp`: `search_graph` → `trace_path`; `trace_path` devolvió
  vacío en las cuatro preguntas, así que el harness añade el fallback
  `query_graph` (Cypher sobre `CALLS`), y su coste se cuenta.
- Base de comparación: salida de `rg -n "\bNOMBRE\b"` sobre el corpus más la
  lectura completa de cada archivo que declara ese nombre, que es lo mínimo
  para desambiguar homónimos leyendo.

La verdad de referencia se construyó a mano por pregunta: ocurrencias de
`grep` clasificadas una a una, comprobando de qué módulo importa cada archivo.
Se compara el conjunto de archivos llamantes, excluyendo el archivo que
declara.

|pregunta|lenguaje|`grep`+lectura|Kivgraph|`codebase-memory-mcp`|
|---|---|---|---|---|
|`Q1` llamantes de `withRetry` de `@kena/shared`|TS cross-package|`16.004` tok|`2.708` tok, `P=0,00` `R=0,00`|`830` tok, `P=0,00` `R=0,00`|
|`Q2` llamantes de `withRetry` en `postgres/retry.go`|Go|`16.004` tok|`2.918` tok, `P=1,00` `R=1,00`|`961` tok, `P=0,33` `R=1,00`|
|`Q3` llamantes de `getRequiredField`|TS intra-repo|`4.210` tok|`4.505` tok, `P=1,00` `R=0,89`|`830` tok, `P=1,00` `R=1,00`|
|`Q4` llamantes de `now_ms()`|Rust|`3.394` tok|`3.455` tok, `P=1,00` `R=1,00`|`596` tok, `P=1,00` `R=0,67`|
|**total**| |`39.612` tok|`13.586` tok (8 llamadas, `5,6 ms`)|`3.217` tok (12 llamadas, `1.750 ms`)|
|**respuestas exactas**| |4/4 por construcción|2/4|1/4|

`withRetry` es un homónimo de siete declaraciones en tres lenguajes
(`library-shared`, `library-env`, `sdk-module-ts` como función y como método
privado, y tres funciones Go en `api-db-go` y `api-music`). Es el caso donde
`grep` no puede contestar y donde se ve la diferencia entre las dos
herramientas.

### Qué falló, exactamente

- **`Q2`, los cuatro falsos llamantes.** `codebase-memory-mcp` atribuye a la
  función Go `withRetry` de `postgres/retry.go` cuatro llamantes TypeScript:
  `packages/core/src/cluster/master/index.ts`,
  `packages/core/src/cluster/worker/BotWorker.ts`,
  `packages/core/src/shared/utils/sharding.ts` y
  `packages/gateway/src/grpc/server.ts`. Esos archivos importan `withRetry`
  de `@kena/shared`; la arista cruza lenguaje por coincidencia de nombre. Es
  el mismo mecanismo que deja `Q1` en cero: los llamantes reales del símbolo
  de `@kena/shared` acabaron colgados de un archivo `.go`.
- **`Q4`, dos llamantes perdidos.** `codebase-memory-mcp` no reporta
  `api_rest/routes_players.rs` ni `api_ws/mod.rs`, que llaman `now_ms()` por
  ruta importada (`use crate::util::now_ms`) y cualificada
  (`crate::util::now_ms()`).
- **`Q1`, Kivgraph.** Devuelve tres re-exportaciones dentro de
  `library-shared` y ninguna llamada. La causa no es que falte construir el
  paquete: en `kena` **ningún** repositorio consume a otro por fuente. Todos
  declaran `"@kena/shared": "0.0.1"`, un rango de registro, y pnpm coloca en
  `node_modules/.pnpm/@kena+shared@0.0.1_…/` una copia del paquete publicado
  con su propio `dist` y sin `src`. Se comprobó construyendo
  `library-shared` y `library-env` y reindexando con la caché de hechos
  borrada: `96.923` símbolos, `368.166` aristas y `22.929` no resueltos,
  idénticos con y sin `dist`; `codebase-memory-mcp` ni detectó cambios
  (`changed_files: 0`, `dist/` está en `.gitignore`). Las construcciones se
  revirtieron y ambos repos quedaron con `git status` limpio. La única
  respuesta con evidencia es de nivel paquete, y es la que da
  `find_cross_repo_consumers`.
- **`Q1`, `codebase-memory-mcp`.** Devuelve vacío porque ya había colgado esas
  llamadas del archivo `.go` homónimo (ver `Q2`).
- **`Q3`, Kivgraph.** Falta `packages/core/tests/.../ipcCase.test.ts`: el
  `tsconfig.json` de `packages/core` sólo incluye `src/**/*`, así que el
  checker no ve el test. `codebase-memory-mcp`, que parsea archivos sueltos,
  sí lo ve.

## Cross-repository

`kena` no tiene una sola dependencia `workspace:` o `link:` entre sus 37
repositorios: el grafo de consumo entre repos existe a nivel de paquete
publicado, no de símbolo con fuente en disco. Eso acota lo que se puede medir
sobre este corpus y obliga a un fixture aparte para el resto.

### Nivel paquete, sobre `kena`

Pregunta: qué repositorios consumen `@kena/shared`. La verdad son los `22`
repositorios que lo importan en código; se excluye `services/api-metrics`, que
lo declara en `dependencies` y no lo importa en ningún `.ts`, y se excluyen
`libraries/library-env` y `web/logs.kena.bot`, que sólo lo nombran en
comentarios.

|          |llamadas|tokens|resultado|
|---|---|---|---|
|Kivgraph `find_cross_repo_consumers`|1|`2.456`|`22`/`22`, `P=1,00` `R=1,00`, `22` aristas `PACKAGE_DEPENDS_ON` y `13` relacionados no resueltos|
|`codebase-memory-mcp`|3|`13.847`|sin respuesta|

`codebase-memory-mcp` no modela paquetes npm: su label `Package` tiene `100`
nodos y todos son módulos Go. `get_architecture` cuesta `13.548` tokens y no
contiene la dependencia. La ausencia de `api-metrics` en la respuesta de
Kivgraph es correcta: su arista de paquete nace de un import observado en un
`File`, no del manifiesto.

### Identidad de símbolo, sobre un fixture de dos repositorios

Como `kena` no permite medirlo, se construyó `/private/tmp/xrepo-bench` con
dos repositorios git: `lib-a` exporta `withRetry` y se consume desde `app-b`
por `node_modules/@bench/lib-a` -> `lib-a`, resuelto a `dist/*.d.ts`; `app-b`
declara además un **homónimo local** `withRetry` en `src/local.ts` que sólo
llama `src/unused.ts`. `app-b` type-checkea sin errores, así que la
resolución cross-repository existe de verdad.

Índice: Kivgraph `2,8 s`, `18` símbolos, `45` aristas, `0` no resueltos;
`codebase-memory-mcp` `0,04 s`, `21` nodos, `25` aristas.

|pregunta|verdad|Kivgraph|`codebase-memory-mcp`|
|---|---|---|---|
|`X1` quién llama al `withRetry` de `lib-a/src/retry.ts`|`app-b/src/main.ts`|`962` tok, `P=1,00` `R=1,00` — `IMPORTS_SYMBOL`, `EXACT_PACKAGE_MAPPED`, `TYPESCRIPT_PROJECT_REFERENCE`|`218` tok, `P=0,00` `R=0,00` — cero filas|
|`X2` quién llama al `withRetry` local de `app-b/src/local.ts`|`app-b/src/unused.ts`|`270` tok, `P=1,00` `R=1,00` — `CALLS_DIRECT`, `EXACT_TYPECHECKED`|`63` tok, `P=0,33` `R=1,00`|

El fallo de `codebase-memory-mcp` es el mismo mecanismo de `Q1`/`Q2` en
miniatura y aquí se ve entero: atribuye las tres llamadas -`main.ts:3`,
`main.ts:7` y `unused.ts:3`- al homónimo `app-b/src/local.ts`. Las dos de
`main.ts` pertenecen a `lib-a`, que se importa explícitamente en la línea 1
del archivo. Kivgraph separa los dos símbolos sin mezclar una sola fila.

Límite de Kivgraph en `X1`: la respuesta cross-repository llega con
granularidad de import (`src/main.ts:1`), no de los dos call sites (`:5` y
`:9`). Sabe qué archivo consume el símbolo de otro repositorio; no dónde lo
llama.

### El modo cross-repo de `codebase-memory-mcp`

`index_repository` con `mode: "cross-repo-intelligence"` sólo enlaza `Route` y
`Channel` entre proyectos: HTTP, async, gRPC, GraphQL y tRPC. No proyecta
identidad de símbolo importado, así que no puede contestar `X1` por diseño.
Medido: `total_cross_edges: 0` con los dos proyectos del fixture y `21`
proyectos escaneados, y `total_cross_edges: 0` entre cinco repositorios de
`kena` registrados por separado (`api-gateway`, `api-db-go`, `api-premium`,
`api-translations`, `packages/core`), aunque `api-gateway` habla gRPC con
`env.grpc.host` y consume rutas de `api-db`. Con la dirección en una variable
de entorno no hay literal que casar; Kivgraph no modela rutas HTTP en
absoluto, así que ese eje no tiene comparación, sólo dos ausencias distintas.

### Nota operativa

Kivgraph rechaza registrar una ruta con un componente symlink: en macOS
`/tmp/...` falla con `contains symlink component "/tmp"` y hay que usar
`/private/tmp/...`.

## Preguntas auxiliares

|pregunta|Kivgraph|`codebase-memory-mcp`|
|---|---|---|
|declaraciones llamadas `withRetry`|`find_symbol`, 1 llamada, `2.292` tok, `P=1,00` `R=1,00` (7/7)|`search_graph` `name_pattern`, 1 llamada, `4.392` tok, `P=0,70` `R=1,00` (3 fábricas `vi.mock` como declaraciones)|
|qué hay declarado en `ipc/utils/`|`get_file_outline`, 1 llamada, `633` tok, 13 declaraciones|`search_graph` con `file_pattern`, 3 intentos hasta acertar la sintaxis, `8.815` tok acumulados, 7 filas|
|código de dos símbolos|`get_source`, 1 llamada, `201` tok|`get_code_snippet` ×2, `1.231` tok|
|impacto de `getRequiredField`|`get_blast_radius` `depth=2`, 1 llamada, `5.102` tok, `118` afectados (`50` en página)|`trace_path` vacío + Cypher `CALLS*1..2`, 2 llamadas, `2.071` tok, `47` filas|

`trace_path` no contestó ninguna de las cinco veces que se llamó, ni con
nombre corto ni con `qualified_name`, ni con `include_tests`, mientras que las
aristas `CALLS` existen y Cypher las devuelve. En `0.8.1` la ruta de
consulta anunciada como sustituta de `grep` para llamantes no funciona sobre
este corpus; el agente que la use se queda sin respuesta y sin aviso.

## Latencia

Kivgraph responde desde el `HotSnapshot` en memoria: `5,6 ms` para las ocho
llamadas de la tabla principal. `codebase-memory-mcp` gastó `1.750 ms` en sus
doce llamadas, con `~430 ms` de coste dominante en la primera consulta de cada
secuencia (`search_graph`). Una pasada por medición, sin repeticiones: sirve
para ver el orden de magnitud, no para afirmar un SLO.

## Qué ahorra más

- **Por token de respuesta**: `codebase-memory-mcp`, `3.217` contra `13.586`
  en las cuatro preguntas, `4,2x` menos. Contra leer y `grep`ear ahorra
  `12,3x`; Kivgraph, `2,9x`.
- **Por respuesta correcta**: Kivgraph, 2 de 4 exactas contra 1 de 4, y sin
  ninguna arista falsa en las cuatro preguntas. Los `3.217` tokens baratos
  incluyen cuatro llamantes TypeScript inventados para una función Go: un
  agente que los crea se pone a editar archivos que no participan.
- **En las preguntas auxiliares** el orden se invierte: Kivgraph es más barato
  en outline (`633` contra `8.815`), en código (`201` contra `1.231`) y en el
  censo de declaraciones (`2.292` contra `4.392`), porque contesta a la
  primera y sin campos de métricas por nodo.
- El coste en tokens de Kivgraph en `Q3` y `Q4` no baja del de `grep`, y por
  una razón concreta: devuelve una fila por ocurrencia con `stable_key`,
  `confidence` y `provenance`, no una lista de archivos. La ventaja ahí no es
  el ahorro, es que `P=1,00`.
- **En cross-repository no hay competencia**: Kivgraph contesta a nivel
  paquete sobre `kena` (`22`/`22`, 1 llamada, `2.456` tok) y a nivel símbolo
  sobre el fixture (`P=1,00` `R=1,00`); `codebase-memory-mcp` no modela
  paquetes npm, y su modo cross-repo sólo casa rutas HTTP y RPC, que en este
  corpus dan `0` aristas. Sus tokens ahí son baratos porque no hay respuesta.

## Limitaciones

- El tokenizador es `o200k_base`, un proxy del de Claude; los cocientes entre
  columnas son estables, los valores absolutos no son exactos.
- El corpus no tiene consumo por fuente entre repositorios, así que la
  identidad de símbolo cross-repository se midió en un fixture de dos
  repositorios construido para eso. Construir `library-shared` y
  `library-env` no cambió ningún conteo -se comprobó y se revirtió-, porque
  los consumidores usan el paquete del registro y no el repo del workspace.
- Cuatro preguntas de referencias y cuatro auxiliares sobre un solo corpus y
  una sola máquina. No es una medida de calidad general de ninguna de las dos
  herramientas.
- Una sola pasada por medición: las latencias no llevan intervalo.
- La verdad de referencia es manual y se limita a granularidad de archivo. Un
  desacuerdo a nivel de línea no se contabiliza.
- Las secuencias de llamadas las fijó el harness. Otro agente, con otras
  llamadas -por ejemplo Cypher directo en `codebase-memory-mcp` o `limit` más
  bajo en Kivgraph- obtendría otros costes.
- `codebase-memory-mcp` cubre 158 lenguajes y trae análisis que Kivgraph no
  tiene (`get_architecture`, clusters, ADRs, complejidad por nodo); Kivgraph
  cubre tres lenguajes con aristas del checker, dimensión de repositorio y no
  resueltos retenidos. Esta comparación sólo mide el solape.

## Reproducir

```bash
codebase-memory-mcp cli index_repository '{"repo_path":"/ruta/a/kena"}'

export HOME=/tmp/kivbench-home
kivgraph init --repository <nombre>=<ruta> ...   # un repositorio git por entrada
kivgraph index --full --json
kivgraph serve
```

Fixture cross-repository:

```bash
export HOME=/private/tmp/kivfix-home        # nunca /tmp: hay un symlink
kivgraph init --repository lib-a=/private/tmp/xrepo-bench/lib-a \
             --repository app-b=/private/tmp/xrepo-bench/app-b --languages typescript
kivgraph index --full --json

codebase-memory-mcp cli index_repository '{"repo_path":"/private/tmp/xrepo-bench"}'
```

Con `HOME` aislado hay que exportar además `CARGO_HOME`, `RUSTUP_HOME` y
`GOMODCACHE` apuntando a las cachés reales: sin `GOMODCACHE` la pasada Go
falla con `module lookup disabled by GOPROXY=off` y publica una generación sin
un solo símbolo Go, y sin `CARGO_HOME` `doctor` marca `toolchain.cargo` como
`FAIL`.

## Lo que cambió después: ADR 0046

Este informe midió la superficie de `kivgraph 0.2.1`. El desglose por campo que
produjo -`confidence` y `provenance` repetidos en las cincuenta filas de una
página, `stable_key` como el `39 %` de `find_symbol`, `48` de las primeras `50`
filas de un blast radius siendo variables locales- se convirtió en el
ADR 0046, ya implementado. Vuelto a medir sobre la **misma** generación
`000003`, con la misma verdad de referencia manual y sin reindexar:

|                                   |antes|después|
|---|---|---|
|las cuatro preguntas de referencias|`13.594` tok, 8 llamadas|`2.883` tok, 7 llamadas|
|las mismas en `view: "files"`|--|`963` tok|
|`codebase-memory-mcp` sobre las mismas|`3.217` tok|`3.217` tok|
|`find_symbol` de un homónimo de 22 filas|`2.292`|`901`|
|`get_blast_radius` a profundidad 2|`5.102`|`921`|
|`get_file_outline` de un directorio|`633`|`248`|
|`find_cross_repo_consumers`, 35 filas|`2.456`|`2.202`|
|cursor de una página truncada|`314` caracteres|`31`|
|coste fijo de `tools/list` + `instructions`|`2.568`|`2.651`|

`P` y `R` no se movieron en ninguna de las cuatro preguntas: `Q1` sigue en
`0,00`/`0,00`, `Q2` y `Q4` en `1,00`/`1,00`, `Q3` en `1,00`/`0,89`. El ahorro
salió de dejar de repetir columnas, de resolver un nombre sin una llamada
previa y de no paginar bindings locales; no de contestar menos.

Con eso la comparación cambia de signo en el eje donde perdía: `2.883` contra
`3.217` tokens, `0,90x`, y `0,30x` pidiendo la vista de archivos, con 2 de 4
respuestas exactas contra 1 de 4 y sin una arista falsa. Lo que sigue en contra
es el coste fijo, que subió `83` tokens por los tres parámetros nuevos, y
`find_cross_repo_consumers`, que apenas baja porque el `detail` de sus filas no
resueltas es prosa que se lee.

Las cifras de esta sección están en `results.json` bajo `after_adr_0046`.

## La revancha completa

Las nueve preguntas del informe, otra vez, misma generación y misma verdad de
referencia. Kivgraph con el ADR 0046 dentro; `codebase-memory-mcp 0.8.1` sin
cambios.

|pregunta|Kivgraph|`codebase-memory-mcp`|
|---|---|---|
|`Q1` `withRetry` de `@kena/shared`|`297` tok, 2 llamadas, `P=0,00` `R=0,00`|`830` tok, 3 llamadas, `P=0,00` `R=0,00`|
|`Q2` `withRetry` de `postgres/retry.go`|`320` tok, 2, `P=1,00` `R=1,00`|`961` tok, 3, `P=0,33` `R=1,00`|
|`Q3` `getRequiredField`|`1.034` tok, 1, `P=1,00` `R=0,89`|`830` tok, 3, `P=1,00` `R=1,00`|
|`Q4` `now_ms()`|`1.232` tok, 2, `P=1,00` `R=1,00`|`596` tok, 3, `P=1,00` `R=0,67`|
|**subtotal referencias**|**`2.883`** tok, 7 llamadas, `4,6 ms`|`3.217` tok, 12 llamadas, `2.329 ms`|
|las mismas en `view: "files"`|`963` tok|no tiene equivalente|
|censo de declaraciones de `withRetry`|`901` tok, `P=1,00` `R=1,00` (7/7)|`4.392` tok, `P=0,70` `R=1,00`|
|outline de un directorio|`248` tok, 1 llamada|`4.875` tok, 2 intentos|
|código de dos símbolos|`201` tok, 1|`1.231` tok, 2|
|impacto de `getRequiredField`|`921` tok, 1, `29` afectados invocables|`2.071` tok, 2 (`trace_path` vacío + Cypher)|
|consumidores cross-repo de `@kena/shared`|`2.202` tok, 1, `22`/`22`|`13.847` tok, 3, sin respuesta|
|**total nueve preguntas**|**`7.356`** tok|`29.633` tok|
|**sesión, con el coste fijo**|**`10.007`** tok|`32.589` tok|

`4,03x` más barato en las nueve preguntas y `3,26x` contando el coste fijo de
`tools/list`, que es el que se paga sin preguntar nada: `2.651` contra `2.956`.

Donde sigue perdiendo por token es en dos preguntas concretas, y por el mismo
motivo que antes: `Q3` y `Q4` devuelven una fila por ocurrencia con su línea y
su tipo de arista, mientras `codebase-memory-mcp` devuelve el llamante
deduplicado. Pedir `view: "files"` invierte también eso -`963` contra `3.217`,
`3,3x`- a cambio de no ver las líneas.

Lo que no cambió es lo único que decide: 2 de 4 respuestas exactas contra 1 de
4, sin una sola arista falsa contra los cuatro llamantes TypeScript que el
rival cuelga de una función Go, y `2.329 ms` contra `4,6 ms`.

## La revancha, con el segundo nivel de hoisting

El ADR 0046 ganó una etapa 4 después de esta revancha: agrupar las filas que
no hoistean a la página por la tupla que sí comparten, en vez de repetirla en
cada una. Exactamente el motivo por el que `Q3` y `Q4` no bajaban del coste de
`grep` -una fila de re-exportación entre 66 rompía el hoist de página para
las 66-. Misma generación `000003`, mismo rival sin tocar:

|pregunta|antes (etapa 1-3)|con etapa 4|
|---|---|---|
|`Q3` `getRequiredField`|`1.034` tok, `P=1,00` `R=0,89`|`729` tok, `P=1,00` `R=0,89`|
|`Q4` `now_ms()`|`1.232` tok, `P=1,00` `R=1,00`|`868` tok, `P=1,00` `R=1,00`|
|**subtotal referencias (4 preguntas)**|`2.883` tok|**`2.214`** tok|
|impacto de `getRequiredField`|`921` tok, `29` afectados|`821` tok, `29` afectados|
|**total nueve preguntas**|`7.356` tok|**`6.587`** tok|

`P` y `R` no se movieron: agrupar cambia cómo se escribe la respuesta, nunca
qué arista afirma. `Q1` y `Q2` no aparecen en la tabla porque no cambiaron -sus
páginas son demasiado pequeñas para que agrupar compre nada, y el propio
código lo comprueba: mide las dos formas y sirve la más barata en vez de
asumir que agrupar gana siempre.

```
cuatro preguntas de referencias:  2.214 tok  ->  0,69x de codebase-memory (3.217)
                                              ->  6,1x más barato que antes del ADR (13.594)
nueve preguntas:                  6.587 tok  ->  4,50x más barato que codebase-memory (29.633)
```

Encontrar esto exigió un fixture que un checker de verdad produciría -cuatro
llamantes directos de una raíz y, para cada uno, un consumidor propio, con
`reached_from` distinto en cada consumidor pero el resto de la tupla igual- y
no uno de los fixtures anteriores, ninguno con cuatro filas que coincidieran
en página *y* grupo en la misma columna a la vez. La primera versión de
`get_blast_radius` agrupaba y perdía por eso: pasaba el valor ya vaciado del
grupo como si nada se hubiera hoisteado, y la comparación de bytes elegía en
silencio la forma plana en cada ejecución real, sin fallar nunca porque las
dos formas eran válidas JSON, sólo una más cara. `TestGetBlastRadiusGroupsFanInDespiteDivergingReachedFrom`
en `internal/mcp/tools/blast_radius_test.go` es el resultado, y queda como
guardia de regresión.

## La revancha final: `find_symbol`, `get_file_outline`, `find_cross_repo_consumers`

La etapa 4 sólo tocó `find_references` y `get_blast_radius`/`trace_dependencies`,
las dos tools que comparten `compactReachedSymbols`. El mismo patrón --una
página que no hoistea nada a la cabecera porque una minoría de filas rompe la
unanimidad-- estaba también en `find_symbol` (agrupar por `kind`+`exported`),
`get_file_outline` (agrupar por `kind`+`visibility`+`signature`) y
`find_cross_repo_consumers` (agrupar por `category`+`edge_kind`+`confidence`+
`provenance`+`evidence_kind`+`reason`). Mismo mecanismo -`groupByResidual`
construye ambas formas y `compact()` sirve la más barata-, tres
implementaciones nuevas porque cada tool hoistea columnas distintas. Misma
generación `000003`, mismo rival sin tocar:

|pregunta|tool|antes|con el segundo hoist|
|---|---|---|---|
|censo de declaraciones de `withRetry` (7 filas)|`find_symbol`|`901` tok|`773` tok|
|outline de `ipc/utils/` (13 declaraciones)|`get_file_outline`|`248` tok|`248` tok (sin cambio)|
|consumidores cross-repo de `@kena/shared` (35 filas)|`find_cross_repo_consumers`|`2.202` tok|`926` tok|
|**total nueve preguntas**| |`6.587` tok|**`5.183`** tok|

```text
nueve preguntas:  5.183 tok  ->  5,72x más barato que codebase-memory (29.633)
sesión completa:  7.834 tok  ->  4,16x más barato que codebase-memory (32.589)
```

`get_file_outline` de `ipc/utils/` no baja: `13` declaraciones sobre muy pocas
tuplas `(kind, exported)` repetidas no le ganan a la página plana, y el
comparador de bytes -el mismo mecanismo que ya evitó agrupar en `Q1`/`Q2`- la
deja tal cual. `find_symbol` baja poco en proporción porque `7` filas es una
página pequeña; la ganancia real está en páginas grandes con una minoría
disidente, como `find_symbol` sobre el prefijo `handle*` en `packages-core`
-`500` filas, `22.657 -> 18.678` tok- o `get_file_outline` sobre un directorio
entero -`3.667 -> 3.184` tok-, ninguna de las dos parte del cuestionario de
nueve preguntas pero medidas sobre el mismo `kena`.

`find_cross_repo_consumers` es donde estaba el coste real: `35` filas con
`22` dependencias de paquete que comparten una tupla y `13` no resueltas que
colapsan a dos pares `(reason, detail)`. La sección "Qué falló, exactamente"
de este mismo informe -y el ADR 0046- asumían que `detail` era prosa propia
de cada fila no resuelta; sobre `kena` es una plantilla repetida palabra por
palabra en las filas que fallan por el mismo motivo, y agrupar la expone
como lo que es: una propiedad del grupo, no de la fila.

Ninguna pregunta cambió de `P` o `R`: agrupar sigue sin decidir qué arista
existe, sólo cómo se escribe. El coste fijo de `tools/list` +
`instructions` tampoco se movió -`2.651` tokens-, porque ninguna de las tres
tools ganó un parámetro de entrada nuevo; sólo cambió cómo serializan la
respuesta que ya daban. Las cifras de esta sección están en `results.json`
bajo `stage5_second_tier_hoisting_symbol_outline_xrepo`.
