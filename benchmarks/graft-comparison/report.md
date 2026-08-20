# Kivgraph contra NanoNets `graft` sobre el workspace `kena`

Comparación de coste en tokens, latencia y exactitud entre `kivgraph 0.3.2` y
`graft 0.10.1` (`@nanonets/graft`), respondiendo las mismas preguntas
estructurales sobre `/Users/adria/Documents/programacion/projects/kena`, más un
tercer brazo: las herramientas que cualquier agente ya tiene, `grep` y lectura
de archivos.

Las métricas crudas están en `results.json` y las respuestas literales de cada
llamada en `raw/`, de modo que cualquier afirmación de parseo de este informe se
puede comprobar contra los bytes de los que salió. Este informe no emite ningún
veredicto de aceptación: mide dos herramientas sobre un corpus concreto y en un
estado concreto de ese corpus.

## Entorno y provenance

|dato|valor|
|---|---|
|fecha|2026-08-20|
|commit del repositorio|`93ebfc3` más la descripción de `find_references` de este commit|
|graft medido|`0.10.1`, sin cambios en ninguna pasada|
|máquina|Apple M5, macOS, `arm64`|
|toolchain|`go1.26.4`|
|corpus|37 repositorios git, `5.330` archivos de código|
|tokenizador|`tiktoken` `o200k_base`|
|transporte|MCP sobre `stdio`, cliente JSON-RPC del harness|

El estado de Kivgraph vive en un `HOME` aislado (`/tmp/kivbench-graft-home`) con
los 37 repositorios registrados y `include_tests: true`; el contexto de graft
vive fuera del corpus (`/private/tmp/graft-kena-ctx`), así que no se escribe nada
dentro de `kena`. Ninguno de los 37 repositorios se modificó.

## Antes de los cocientes: graft no es determinista

Cuatro pasadas de este harness sobre el mismo corpus y el mismo `graft 0.10.1`,
sin tocar nada entre ellas:

|brazo|totales observados|
|---|---|
|graft|`2.924`, `2.999`, `2.999`, **`3.179`**|
|kivgraph, filas compactas|`2.479`, `2.480`, `2.480`, `2.478`|
|kivgraph, vista `files`|`912`, `912`|

graft varía un `9 %` porque reconstruye su grafo en cada pasada y el resultado no
es reproducible: ocho builds en frío del mismo árbol dieron `77.198`, `77.251`,
`77.264`, `77.281`, `77.310`, `77.333`, `77.339` y `77.356` aristas. Su `Q3` pasó
de `1.157` a `1.337` tokens entre dos pasadas.

Así que este informe da **bandas**, no cocientes de tres decimales, y las cifras
por pregunta son las de la última pasada. Lo que es estable es que Kivgraph no se
mueve y graft sí.

## Las cuatro preguntas medidas

Cada pregunta es «qué archivos llaman a esta declaración concreta». Los brazos
parten del **nombre desnudo** que tiene quien pregunta: `withRetry` existe siete
veces en `kena` y `now_ms` cuatro.

|pregunta|kivgraph|kivgraph vista `files`|graft|graft `--lsp`|`grep`+lectura|
|---|---|---|---|---|---|
|`Q1_ts_xrepo`|`288` tok, 2 llamadas, `P=0,00 R=0,00`|`234`, 2, `0,00/0,00`|`659`, 1, `0,00/0,00`|`659`, 1, `0,00/0,00`|`10.054`|
|`Q2_go`|`311`, 2, **`1,00/1,00`**|`217`, 2, **`1,00/1,00`**|`659`, 1, `0,00/0,00`|`659`, 1, `0,00/0,00`|`10.054`|
|`Q3_ts_intra`|`1.020`, 2, `1,00/0,89`|`230`, **1**, `1,00/0,89`|`1.337`, 1, **`1,00/1,00`**|`1.337`, 1, **`1,00/1,00`**|`3.237`|
|`Q4_rust`|`859`, 2, **`1,00/1,00`**|`231`, 2, **`1,00/1,00`**|`524`, 1, `0,00/0,00`|`524`, 1, `0,00/0,00`|`34.274`|
|**total**|**`2.478`, 8, `0,75/0,72`, 2/4**|**`912`, 7, `0,75/0,72`, 2/4**|`3.179`, 4, `0,25/0,25`, 1/4|`3.179`, 4, `0,25/0,25`, 1/4|`57.619`, 23, `1,00/1,00`, 4/4|

`P` es precisión -- de lo que la herramienta dijo, qué fracción era cierta -- y
`R` exhaustividad -- de lo que existía, qué fracción encontró. Se calculan a
nivel de archivo contra una verdad de referencia manual, excluyendo el archivo
que declara el símbolo.

Hay dos columnas de Kivgraph porque hay dos granularidades, y conviene no
confundirlas:

- **`kivgraph`** son las filas compactas, que además de los archivos dan la línea
  de cada referencia. Es lo comparable con graft, que responde siempre con
  bloques de llamante y rango de líneas.
- **`kivgraph` vista `files`** responde sólo qué archivos, que es literalmente lo
  que piden estas cuatro preguntas y la granularidad a la que se puntúa. **Mismos
  `P` y `R`, mismas dos exactas, `2,7x` menos tokens** y una llamada menos, porque
  `Q3` deja de necesitar segunda página: los 66 hechos caben en 9 filas.

`graft` no tiene un modo equivalente: `graft callers` acepta `--direction`,
`--depth`, `--in`, `--json` y `--no-refresh`, y siempre responde con las líneas.
Por eso esta columna se informa **al lado** de la comparable y no en su lugar.

|granularidad|banda contra graft|
|---|---|
|filas compactas, con línea|`0,78x` -- `0,85x`|
|vista `files`, sin línea|**`0,29x` -- `0,31x`**|

## Cómo bajó de `14.788` a `912`

|qué se midió|tokens|
|---|---|
|`0.2.1`, resolviendo el símbolo antes|`14.788`|
|`0.3.1`, resolviendo el símbolo antes|`3.991`|
|`0.3.2`, preguntando por el nombre|`2.478`|
|`0.3.2`, y a la granularidad preguntada|**`912`**|

`16,2x` en total, y **ninguno de los tres tramos cambió una respuesta**: las
mismas dos exactas de cuatro, la misma precisión, la misma exhaustividad, en las
cuatro filas. Sólo el primero cambió código.

**Primer tramo, `3,71x`: el payload.** Es el ADR 0046 -- izar todo campo que se
repite por fila -- comprobado aquí por un harness que puntúa archivos contra una
verdad de referencia: mismas llamadas, misma precisión, misma exhaustividad,
mismas dos respuestas exactas, `3,71x` menos tokens.

**Segundo tramo, `1,61x`: la llamada que no hacía falta.** El brazo resolvía el
símbolo con `find_symbol` y después preguntaba por sus referencias. Pero
`find_references` **ya aceptaba un nombre desnudo**: cuando es único contesta en
una llamada, y cuando no, se niega a elegir y nombra los candidatos con la misma
tripleta que aceptan todas las tools, así que acotar es copiar uno.

|camino|`Q1`|`Q2`|
|---|---|---|
|`find_symbol` y luego referencias|`909`|`932`|
|preguntar por el nombre|**`288`**|**`311`**|

La diferencia es lo que cuesta desambiguar: la negativa son `129` tokens donde la
página de `find_symbol` eran `750` -- 22 filas para `withRetry`, imports y
re-exports incluidos, con la firma de cada declaración.

**Tercer tramo, `2,7x`: contestar lo que se pregunta.** Las cuatro preguntas
dicen «qué archivos llaman a esto» y se puntúan sobre archivos, pero el brazo
pedía la línea de cada referencia. La vista `files` responde el conjunto de
archivos con su recuento, y el harness verifica que el resultado es el mismo: `P`
y `R` idénticos en las cuatro. En `Q3` son `1.020` -> `230` tokens y dos llamadas
-> una.

No es gratis en información: la línea de cada referencia deja de venir, y quien
la necesite pide las filas compactas, que son el valor por defecto.

### Las tres veces, el mismo patrón

Ninguno de los dos últimos tramos añadió una capacidad. Las dos existían, las dos
estaban documentadas -- la referencia de la landing en «One call instead of two»,
y la skill publicada, que enuncia el nombre desnudo y dedica una sección entera a
«Choosing a view» -- y las dos faltaban en **la descripción de la tool**, que es
lo que lee un agente al elegir sus argumentos.

Eso decide a quién le costaba: un anfitrión que instala la skill ya tenía la
información; uno que sólo monta el servidor MCP veía nada más que la descripción,
y el camino que enseñaba era el caro. El harness es de los segundos, y por eso
medía `3.991` donde había `912`. La descripción ahora dice las dos cosas, en 33
tokens de superficie residente.

## Las dos contabilidades

|concepto|kivgraph|kivgraph `files`|graft|
|---|---|---|---|
|descripciones + instrucciones|`564`|`564`|`520`|
|esquemas|`926`|`926`|`420`|
|las cuatro respuestas|`2.478`|`912`|`3.179`|
|**sesión completa**|`3.968`|**`2.402`**|`4.119`|

Con filas compactas la sesión cae **a caballo del empate**: `0,96x` con el graft
de esta pasada, `1,03x` con el más barato que le medí. Con `17` a `151` tokens de
diferencia sobre casi cuatro mil, no hay ganador; decirlo de otra forma sería
elegir la pasada que conviene.

A la granularidad preguntada es `0,58x` -- `0,62x`, y ahí la superficie deja de
decidir.

Lo que no depende de la pasada: **la superficie residente de Kivgraph cuesta
`1.490` contra `940`**, 11 tools contra 6 y `926` tokens de esquema contra `420`.
Ahí seguimos detrás pase lo que pase con las respuestas. Una sesión de sólo
lectura paga además `143` tokens por `index_project`, la única tool mutante, que
hoy se registra siempre que hay grafo publicado y no se puede omitir.

## Coste y latencia

|brazo|tokens|llamadas|ms/llamada|
|---|---|---|---|
|kivgraph|`2.478`|8|`0,44`|
|kivgraph vista `files`|**`912`**|7|**`0,15`**|
|graft|`3.179`|4|`113,71`|
|`grep`+lectura|`57.619`|23|--|

Las dos herramientas de grafo son entre `18x` y `23x` más baratas que leer y
`grep`ear -- y `63x` si sólo se piden los archivos --, que es el resultado que
importa si la alternativa real es no usar ninguna.

La diferencia de latencia -- `257x` -- no es una optimización nuestra: Kivgraph
responde desde el `HotSnapshot` en memoria, y graft refresca el grafo contra el
árbol de trabajo antes de contestar, lo que le da una frescura que Kivgraph no
tiene sin reindexar. Una pasada por medición: sirve para el orden de magnitud, no
para afirmar un SLO.

### El banner

Cada respuesta de graft empieza por una línea que no es la respuesta: `84` tokens
por llamada, `338` de los `3.179` del brazo -- el **`11 %`**. Es una estimación de
ahorro contra leer los archivos enteros, más una instrucción de repetir esa cifra
al usuario. Dos observaciones, las dos de diseño y no de rendimiento: el ahorro
que declara se mide contra un brazo que nadie iba a ejecutar, y se paga en cada
llamada, incluidas las que no contestan nada.

## Lo que decide: exactitud

Los ceros de `Q2` y `Q4` no miden lo que graft sabe extraer. Miden que se le
apuntó al árbol entero: ante un nombre ambiguo **descarta el llamante cross-file
y avisa de que puede infracontar** en vez de adivinar. Es la política opuesta a
inventar una arista, y en `Q1` -- donde nadie acierta -- es mejor comportamiento
que el nuestro: graft dice «puede infracontar», mientras Kivgraph devuelve cuatro
`REEXPORTS` correctas que no responden la pregunta.

La frontera está en el **build**, no en el paquete:

|scope de la construcción|tokens|resultado|
|---|---|---|
|`Q2` desde la carpeta `kena` completa|`659`|`P=0,00 R=0,00`, 2 homónimos, «sin llamantes»|
|`Q2` desde `services/api-db-go`|`232`|`P=0,00 R=0,00`, 2 homónimos, «sin llamantes»|
|`Q2` desde `.../infrastructure/postgres`|`206`|**`P=1,00 R=1,00`**|
|`Q4` desde `services/kenalink-rs`|`532`|**`P=1,00 R=1,00`**|

Con el repositorio Rust solo, graft resuelve los seis llamantes de `now_ms`. Con
el repositorio Go solo **sigue** en cero, porque `api-db-go` declara dos
`withRetry` en dos paquetes: hay que bajar al paquete. Kivgraph separa esos dos
símbolos porque los resuelve `go/types`, no la coincidencia de nombres, y el
scope no entra en la pregunta.

Y donde Kivgraph pierde: `Q3`. Falta
`packages/core/tests/.../ipcCase.test.ts` porque el `tsconfig.json` de
`packages/core` no lo incluye, así que el checker no lo ve; el tree-sitter de
graft sí. `R=0,89` contra `1,00`.

## Coste de indexación y dependencias

|brazo|frío|caliente|nodos|aristas|estado|
|---|---|---|---|---|---|
|graft|`26,6 s`|`2,7 s`|`34.523`|`77.251`|`181 MB`|
|graft `--lsp`|`115,6 s`|--|`34.523`|`77.339`|`181 MB`|
|kivgraph|`41,8 s`|`8,6 s`|`96.482`|`367.725`|`1.694 MB`|

«Frío» no significa lo mismo en los dos: en graft es un contexto vacío, en
Kivgraph la caché de hechos y el target de Rust borrados. Cada fila dice qué midió
en `results.json`.

Kivgraph indexa `2,8x` más nodos y `4,8x` más aristas, cuesta `1,6x` más tiempo y
`9,4x` más disco, y **necesita un toolchain**: la caché de módulos de Go por
módulo indexado y `cargo` por cada workspace de Rust. Sin ellos el load falla y
sus símbolos quedan ausentes -- por eso `indexing.languages` registra qué aportó
cada front end (`go: 19.166`, `typescript: 90.729`, `rust: 3.063`): un total
agregado no distingue una medición completa de una que perdió un lenguaje. graft
no necesita clave ni toolchain para su tier estructural.

El tier `--lsp`, que promete «compiler-grade call edges», costó `4,3x` más tiempo
y produjo **cero** aristas `lsp_resolved`: `77.339` contra `77.251`, dentro de la
banda de no-determinismo. El tier `--deep`, que es el que usa un modelo, no se
midió por falta de clave de proveedor: es la única ausencia declarada.

## Las preguntas auxiliares

|pregunta|kivgraph|graft|
|---|---|---|
|qué hay declarado en este directorio|`3.284` tok, **1 llamada**, 619 filas|`3.636` tok, **10 llamadas**, 84 filas|
|qué se rompe si cambio esto (profundidad 2)|`818` tok, 29 filas|`1.556` tok, 47 filas|
|quién lo consume desde otro repositorio|**`878`** tok|`11.992` tok|
|cuántas declaraciones distintas se llaman así|`750` tok, `P=1,00 R=1,00`|`659` tok, `P=1,00 R=1,00`|

La forma de la diferencia importa más que los tokens. El outline de graft es una
llamada por archivo -- diez, y una de ellas falla con «no definitions indexed for
this file» -- porque su modelo es la tarjeta por archivo; Kivgraph contesta el
directorio en una. Los consumidores cross-repository son `13,7x` más baratos
porque graft no modela la dimensión de paquete y hay que caer a un grep sobre el
especificador, que devuelve texto sin resolver.

Las 29 filas del blast radius contra las 47 de graft no son una respuesta más
pobre por accidente: `0.3.x` excluye `field` y `variable` por defecto, lo declara
en `kinds_default_excluded` y marca su veredicto de completitud como
`LOWER_BOUND` con los puntos ciegos nombrados. Es otra respuesta, no la misma más
barata.

## Palancas medidas y no aplicadas

Quedan tres, con su número, para que nadie tenga que volver a medirlas:

- **`find_symbol` manda la cadena de re-exports.** De las 22 filas de
  `withRetry`, 15 son de kind `import` o `export`, que no son declaraciones: el
  worker las emite como símbolos aparte que alcanzan la declaración por una
  arista `EXPORTS` (`ts-worker/src/facts-cli.ts`). Omitirlas dejaría la página en
  `1.170` tokens contra `1.855`. Ya no está en el camino de este benchmark, pero
  sí en el de quien pregunta «dónde está declarado».
- **Las firmas son el campo más grande de la fila.** `get_file_outline` ya las
  deja fuera de su vista compacta y las devuelve bajo `response_format:
  "detailed"`; `find_symbol` las manda para los callables. Aplicar el mismo
  criterio ahorraría otros `426` tokens en esa página -- pero recuperar una firma
  cuesta `428` tokens de `get_source`, así que quitarla sin una vía barata de
  vuelta cambia tokens por un viaje de ida y vuelta, y sale peor.
- **Los esquemas son `926` tokens contra `420`.** Están repartidos (`133` el
  mayor), así que recortarlos de verdad significa menos tools o menos argumentos
  aceptados, y las dos cosas rompen compatibilidad. Es la partida que decide la
  contabilidad por sesión con filas compactas.

## Dónde gana cada uno

**graft**: indexación (`26,6 s` contra `41,8 s`), disco (`181 MB` contra
`1.694 MB`), cero dependencias -- ni clave ni toolchain --, el censo de
declaraciones, frescura contra el árbol de trabajo sin reindexar, y una política
honesta ante la ambigüedad: avisa en vez de inventar. La sesión completa con filas
compactas es un empate que puede caer de su lado.

**kivgraph**: coste por respuesta (`0,78x` -- `0,85x` con línea, `0,29x` --
`0,31x` sin ella), exactitud (`2/4` contra `1/4`, `P=0,75` contra `0,25`),
inmunidad al scope -- resuelve homónimos con `go/types` y el checker, no con
nombres --, consumidores cross-repository (`13,7x`), el outline de un directorio
en una llamada, latencia por llamada (`257x`), determinismo entre pasadas, y no
resueltos retenidos con su motivo.

## Limitaciones

- El tokenizador es `o200k_base`, un proxy del de Claude. Los cocientes entre
  columnas son estables; los valores absolutos no son exactos.
- Sólo se midió el tier estructural de graft. `--deep` no se ejecutó por falta de
  clave de proveedor.
- Cuatro preguntas de referencias, un censo, tres auxiliares y tres sondas de
  scope, sobre un corpus y una máquina. No es una medida de calidad general de
  ninguna de las dos herramientas.
- Una sola pasada por medición para las latencias, que no llevan intervalo. Para
  los tokens hay cuatro pasadas y la variación de graft está declarada arriba.
- Las secuencias las fijó el harness. La de Kivgraph es la más barata que
  contesta bien, y la de graft es la que su superficie documenta; otro agente con
  otras llamadas mediría otro coste. En particular, un agente que elija entre
  homónimos por su firma y no por su ruta pagará la página de `find_symbol`.
- El desacuerdo de `Q3` es manual y a granularidad de archivo.
- El corpus no tiene consumo por fuente entre repositorios, así que `Q1` no tiene
  respuesta alcanzable para ninguna de las dos.

## Reproducir

```bash
# graft: el contexto vive fuera del corpus, así que nada se escribe en kena
npm install -g @nanonets/graft
graft --dir /private/tmp/graft-kena-ctx build /ruta/a/kena

# el tier opt-in: los servidores tienen que estar en el PATH
export PATH="$PATH:$(go env GOPATH)/bin"
graft --dir /private/tmp/graft-kena-lsp build /ruta/a/kena --lsp

# kivgraph: HOME aislado, y las rutas reales de los cachés que exige el índice
export HOME=/tmp/kivbench-graft-home
kivgraph init --languages go,typescript,rust --repository <nombre>=<ruta> ...
sed -i '' 's/include_tests: false/include_tests: true/' \
  "$HOME/.config/kivgraph/config.yaml"
kivgraph index --full --json

# el harness; --skip-indexing reutiliza lo ya indexado
go run ./benchmarks/graft-comparison
```

El harness escribe `results.json` y `raw/`, y su salida por pantalla es la tabla
de este informe.
