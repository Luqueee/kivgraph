# Kivgraph contra NanoNets `graft` sobre el workspace `kena`

Comparación de coste en tokens, latencia y exactitud entre `kivgraph 0.3.1` y
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
|commit del repositorio|`9195855`|
|kivgraph medido|`0.3.1` más la descripción de `find_references` de este commit|
|graft medido|`0.10.1`, sin cambios desde la primera medición|
|máquina|Apple M5, macOS, `arm64`|
|toolchain|`go1.26.4`|
|corpus|37 repositorios git, `5.330` archivos de código|
|tokenizador|`tiktoken` `o200k_base`|
|transporte|MCP sobre `stdio`, cliente JSON-RPC del harness|

El estado de Kivgraph vive en un `HOME` aislado (`/tmp/kivbench-graft-home`) con
los 37 repositorios registrados y `include_tests: true`; el contexto de graft
vive fuera del corpus (`/private/tmp/graft-kena-ctx`), así que no se escribe nada
dentro de `kena`. Ninguno de los 37 repositorios se modificó.

## Las cuatro preguntas medidas

Cada pregunta es «qué llama a esta declaración concreta». Los tres brazos parten
del **nombre desnudo** que tiene quien pregunta: `withRetry` existe siete veces
en `kena` y `now_ms` cuatro.

|pregunta|kivgraph|graft|graft `--lsp`|`grep`+lectura|
|---|---|---|---|---|
|`Q1_ts_xrepo`|`288` tok, 2 llamadas, `P=0,00 R=0,00`|`659`, 1, `0,00/0,00`|`659`, 1, `0,00/0,00`|`10.054`|
|`Q2_go`|`311`, 2, **`1,00/1,00`**|`659`, 1, `0,00/0,00`|`659`, 1, `0,00/0,00`|`10.054`|
|`Q3_ts_intra`|`1.021`, 2, `1,00/0,89`|`1.082`, 1, **`1,00/1,00`**|`1.082`, 1, **`1,00/1,00`**|`3.237`|
|`Q4_rust`|`859`, 2, **`1,00/1,00`**|`524`, 1, `0,00/0,00`|`524`, 1, `0,00/0,00`|`34.274`|
|**total**|**`2.479`, 8, `0,75/0,72`, 2/4**|`2.924`, 4, `0,25/0,25`, 1/4|`2.924`, 4, `0,25/0,25`, 1/4|`57.619`, 23, `1,00/1,00`, 4/4|

`P` es precisión -- de lo que la herramienta dijo, qué fracción era cierta -- y
`R` exhaustividad -- de lo que existía, qué fracción encontró. Se calculan a
nivel de archivo contra una verdad de referencia manual, excluyendo el archivo
que declara el símbolo.

**Kivgraph responde estas cuatro preguntas en `0,85x` los tokens de graft y con
el doble de respuestas exactas.** Es el resultado de dos cambios sucesivos, y el
segundo no fue código.

## Cómo bajó de `14.788` a `2.479`

|qué se midió|tokens|contra graft|
|---|---|---|
|`0.2.1`, resolviendo el símbolo antes|`14.788`|`5,06x`|
|`0.3.1`, resolviendo el símbolo antes|`3.991`|`1,33x`|
|`0.3.1`, preguntando por el nombre|**`2.479`**|**`0,85x`**|

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

La diferencia es lo que cuesta desambiguar: la negativa son `129` tokens donde
la página de `find_symbol` eran `750` -- 22 filas para `withRetry`, imports y
re-exports incluidos, con la firma de cada declaración.

Lo que faltaba no era la capacidad, era decirlo **donde se elige la llamada**.
Dos de las tres superficies ya lo decían: la referencia de la landing en su
sección «One call instead of two», y la skill publicada
(`internal/integrations/assets/kivgraph/SKILL.md`), que lo enuncia entero --
«takes `name` on its own [...] so the usual question costs one call and not
two». La que no lo decía era la descripción de la tool.

La distinción importa para saber a quién le costaba: un anfitrión que instala la
skill ya tenía la información; uno que sólo monta el servidor MCP veía nada más
que la descripción, y el camino que enseña es la búsqueda previa. El harness es
de los segundos, y por eso medía `3.991` donde había `2.479`. La descripción
ahora lo dice también, en 19 tokens de superficie residente que se amortizan en
la primera pregunta ambigua.

## Las dos contabilidades

Por respuesta Kivgraph gana. Contando la superficie que se paga una vez por
sesión, no:

|concepto|kivgraph|graft|
|---|---|---|
|descripciones + instrucciones|`550`|`520`|
|esquemas|`926`|`420`|
|las cuatro respuestas|`2.479`|`2.924`|
|**sesión completa**|**`3.955`**|**`3.864`**|

`1,024x`: **pierde por `91` tokens**. La diferencia no está en las respuestas,
está en la superficie: 11 tools contra 6, y `926` tokens de esquema contra `420`.

Una sesión de sólo lectura paga además `143` tokens por `index_project`, la única
tool mutante, que hoy se registra siempre que hay grafo publicado. Sin ella la
sesión serían `3.812` contra `3.864` -- `0,987x`, gana --, pero no hay forma de
no registrarla, así que la cifra que cuenta es `1,024x`.

## Coste y latencia

|brazo|tokens|llamadas|ms/llamada|
|---|---|---|---|
|kivgraph|`2.479`|8|**`0,25`**|
|graft|`2.924`|4|`107,56`|
|graft `--lsp`|`2.924`|4|`106,17`|
|`grep`+lectura|`57.619`|23|--|

Las dos herramientas de grafo son entre `20x` y `23x` más baratas que leer y
`grep`ear, que es el resultado que importa si la alternativa real es no usar
ninguna.

La diferencia de latencia -- `430x` -- no es una optimización nuestra: Kivgraph
responde desde el `HotSnapshot` en memoria, y graft refresca el grafo contra el
árbol de trabajo antes de contestar, lo que le da una frescura que Kivgraph no
tiene sin reindexar. Una pasada por medición: sirve para el orden de magnitud, no
para afirmar un SLO.

### El banner

Cada respuesta de graft empieza por una línea que no es la respuesta: `84`
tokens por llamada, `336` de los `2.924` del brazo -- el **`11 %`**. Es una
estimación de ahorro contra leer los archivos enteros, más una instrucción de
repetir esa cifra al usuario. Dos observaciones, las dos de diseño y no de
rendimiento: el ahorro que declara se mide contra un brazo que nadie iba a
ejecutar, y se paga en cada llamada, incluidas las que no contestan nada.

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
|graft|`24,6 s`|`2,5 s`|`34.523`|`77.356`|`181 MB`|
|graft `--lsp`|`114,4 s`|--|`34.523`|`77.281`|`181 MB`|
|kivgraph|`39,5 s`|`8,3 s`|`96.482`|`367.725`|`1.694 MB`|

«Frío» no significa lo mismo en los dos: en graft es un contexto vacío, en
Kivgraph la caché de hechos y el target de Rust borrados. Cada fila dice qué
midió en `results.json`.

Kivgraph indexa `2,8x` más nodos y `4,8x` más aristas, cuesta `1,6x` más tiempo y
`9,4x` más disco, y **necesita un toolchain**: la caché de módulos de Go por
módulo indexado y `cargo` por cada workspace de Rust. Sin ellos el load falla y
sus símbolos quedan ausentes -- por eso `indexing.languages` registra qué aportó
cada front end (`go: 19.166`, `typescript: 90.729`, `rust: 3.063`): un total
agregado no distingue una medición completa de una que perdió un lenguaje. graft
no necesita clave ni toolchain para su tier estructural.

El tier `--lsp`, que promete «compiler-grade call edges», costó `4,6x` más tiempo
y produjo **cero** aristas `lsp_resolved`: `77.281` contra `77.356`, dentro de la
banda de no-determinismo de graft entre builds. El tier `--deep`, que es el que
usa un modelo, no se midió por falta de clave de proveedor: es la única ausencia
declarada.

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
  contabilidad por sesión.

## Dónde gana cada uno

**graft**: la sesión completa por `91` tokens, indexación (`24,6 s` contra
`39,5 s`), disco (`181 MB` contra `1.694 MB`), cero dependencias -- ni clave ni
toolchain --, el censo de declaraciones, frescura contra el árbol de trabajo sin
reindexar, y una política honesta ante la ambigüedad: avisa en vez de inventar.

**kivgraph**: coste por respuesta (`0,85x`), exactitud (`2/4` contra `1/4`,
`P=0,75` contra `0,25`), inmunidad al scope -- resuelve homónimos con `go/types` y
el checker, no con nombres --, consumidores cross-repository (`13,7x`), el outline
de un directorio en una llamada, latencia por llamada (`430x`), y no resueltos
retenidos con su motivo.

## Limitaciones

- El tokenizador es `o200k_base`, un proxy del de Claude. Los cocientes entre
  columnas son estables; los valores absolutos no son exactos.
- Sólo se midió el tier estructural de graft. `--deep` no se ejecutó por falta de
  clave de proveedor.
- Cuatro preguntas de referencias, un censo, tres auxiliares y tres sondas de
  scope, sobre un corpus y una máquina. No es una medida de calidad general de
  ninguna de las dos herramientas.
- Una sola pasada por medición: las latencias no llevan intervalo, y el
  no-determinismo de graft entre builds -- cuatro builds en frío del mismo árbol
  dieron `77.264`, `77.281`, `77.310` y `77.356` aristas -- es del orden de las
  diferencias que se le atribuyen al tier `--lsp`.
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
