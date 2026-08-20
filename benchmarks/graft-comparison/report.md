# Kivgraph contra NanoNets `graft` sobre el workspace `kena`

Comparación de coste en tokens, latencia y exactitud entre `kivgraph 0.3.1` y
`graft 0.10.1` (`@nanonets/graft`), respondiendo las mismas preguntas
estructurales sobre `/Users/adria/Documents/programacion/projects/kena`, más un
tercer brazo: las herramientas que cualquier agente ya tiene, `grep` y lectura de
archivos.

Las métricas crudas están en `results.json` y las respuestas literales de cada
llamada en `raw/`, de modo que cualquier afirmación de parseo de este informe se
puede comprobar contra los bytes de los que salió. Este informe no emite ningún
veredicto de aceptación: mide dos herramientas sobre un corpus concreto y en un
estado concreto de ese corpus.

## Entorno y provenance

|dato|valor|
|---|---|
|fecha|2026-08-20|
|commit del repositorio|`fcc3871`|
|kivgraph medido|`0.3.1`, el binario publicado e instalado en el `PATH`|
|graft medido|`0.10.1`, sin cambios desde la medición anterior|
|máquina|Apple M5, macOS, `arm64`|
|toolchain|`go1.26.4`|
|tokenizador|`tiktoken` `o200k_base`|
|transporte|MCP sobre `stdio`, cliente JSON-RPC del harness|

El estado de Kivgraph vive en un `HOME` aislado (`/tmp/kivbench-graft-home`) con
los 37 repositorios registrados y `include_tests: true`; el contexto de graft vive
fuera del corpus (`/private/tmp/graft-kena-ctx`), así que no se escribe nada dentro
de `kena`. Ninguno de los 37 repositorios se modificó: siguen en los mismos commits
que la medición anterior.

## Qué cambió desde la medición anterior

La versión anterior de este informe midió `kivgraph 0.2.1`. La comparación se
repitió sin tocar el rival, sobre el mismo corpus en los mismos commits, con la
misma verdad de referencia y las mismas cuatro preguntas.

|brazo kivgraph|`0.2.1`|`0.3.1`| |
|---|---|---|---|
|tokens en las 4 preguntas|`14.788`|**`3.991`**|`3,71x` menos|
|llamadas|`9`|`9`|iguales|
|precisión / exhaustividad|`0,75` / `0,72`|`0,75` / `0,72`|iguales|
|respuestas exactas|`2/4`|`2/4`|iguales|

El payload adelgazó `3,71x` **sin perder una sola respuesta**: mismas llamadas,
misma precisión, misma exhaustividad, mismas dos respuestas exactas. Es lo que
proponía el ADR 0046 -izar todo campo que se repite por fila- comprobado por un
harness que puntúa archivos contra una verdad de referencia, no bytes contra sí
mismos. Por pregunta: `2,96x`, `3,11x`, `5,09x` y `3,36x`.

El mismo efecto en las auxiliares: el outline de un directorio `9.111` -> `3.282`,
el censo de declaraciones `2.277` -> `750`, los consumidores cross-repository
`2.408` -> `878`.

Y una diferencia que **no** es codificación y conviene no vender como ahorro:
`get_blast_radius` pasó de `118` filas a `29` porque `0.3.x` excluye `field` y
`variable` por defecto. No es información perdida en silencio -- la respuesta
declara `kinds_default_excluded` y su veredicto de completitud (`LOWER_BOUND`,
con los puntos ciegos nombrados)-, pero es una respuesta distinta, no la misma
más barata.

### Por qué el brazo anterior no se puede recomponer

Al repetir la medición, el binario previo al arreglo `f449c71` **no puede indexar
este corpus**: falla al publicar con

```text
invalid fact set: symbol "KY2XF76JIM4ACS5Q5G5K6NCP2R33LJQVYHZRC25VSZAPRIDCN4SA"
is defined by two files, "file:api-db-go:...bots_mock_test.go" and
"file:api-db-go:...command_mock_test.go"
```

El binario `0.3.1` indexa el mismo `HOME` y publica: `96.482` símbolos, `367.725`
aristas, con `19.166` definiciones Go, `90.729` símbolos TypeScript y `3.063`
Rust. Como el brazo anterior sí completó, su índice **no** pudo contener esos
archivos de test Go, y el `results.json` de entonces no registraba la composición
por lenguaje, así que no se puede decir cuál era.

Eso es un defecto del harness, no del rival, y está corregido: `indexing` ahora
registra `languages`. Un índice que pierde un toolchain publica `passed: true` con
cero símbolos de ese lenguaje -- ya pasó en el primer intento de este benchmark-,
y un total agregado no distingue eso de una medición completa.

## Las cuatro preguntas medidas

Cada pregunta es «qué llama a esta declaración concreta», con el sujeto
identificado por repositorio, ruta y nombre. Un nombre desnudo no identifica una
declaración: `withRetry` existe siete veces en `kena` y `now_ms` cuatro.

|pregunta|kivgraph|graft|graft `--lsp`|`grep`+lectura|
|---|---|---|---|---|
|`Q1_ts_xrepo`|`909` tok, 2 llamadas, `P=0,00 R=0,00`|`659`, 1, `0,00/0,00`|`659`, 1, `0,00/0,00`|`10.054`, `1,00/1,00`|
|`Q2_go`|`932`, 2, **`1,00/1,00`**|`659`, 1, `0,00/0,00`|`659`, 1, `0,00/0,00`|`10.054`, `1,00/1,00`|
|`Q3_ts_intra`|`1.144`, 3, `1,00/0,89`|`1.157`, 1, **`1,00/1,00`**|`1.178`, 1, **`1,00/1,00`**|`3.237`, `1,00/1,00`|
|`Q4_rust`|`1.006`, 2, **`1,00/1,00`**|`524`, 1, `0,00/0,00`|`524`, 1, `0,00/0,00`|`34.274`, `1,00/1,00`|
|**total**|**`3.991`, 9, `0,75/0,72`, 2/4**|`2.999`, 4, `0,25/0,25`, 1/4|`3.020`, 4, `0,25/0,25`, 1/4|`57.619`, 23, `1,00/1,00`, 4/4|

`P` es precisión -- de lo que la herramienta dijo, qué fracción era cierta -- y `R`
exhaustividad -- de lo que existía, qué fracción encontró. Se calculan a nivel de
archivo contra la verdad de referencia, excluyendo el archivo que declara el
símbolo.

## Lo que decide: exactitud

Sobre estas cuatro preguntas, **graft es `1,33x` más barato por respuesta y
contesta una cuarta parte de lo que contesta Kivgraph**: `1` respuesta exacta
contra `2`, `P=0,25` contra `0,75`.

Los ceros de `Q2` y `Q4` no miden lo que graft sabe extraer. Miden que se le
apuntó al árbol entero: `withRetry` está declarada dos veces dentro de
`api-db-go` y `now_ms` cuatro dentro de `kena`, y graft, ante un nombre ambiguo,
**descarta el llamante cross-file y avisa de que puede infracontar** en vez de
adivinar. Es la política opuesta a inventar una arista, y en `Q1` -- donde nadie
acierta -- es mejor comportamiento que el nuestro: graft dice «puede
infracontar», mientras Kivgraph devuelve cuatro `REEXPORTS` correctas que no
responden la pregunta.

La frontera está en el **build**, no en el paquete. Con el scope acotado, graft
resuelve:

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

La consecuencia práctica: sobre un monorepo, un nombre común pierde sus llamantes
cross-file salvo que quien pregunta ya sepa en qué paquete vive el símbolo -- que
es justo parte de lo que la pregunta quería averiguar.

Y donde Kivgraph pierde: `Q3`. Falta
`packages/core/tests/.../ipcCase.test.ts` porque el `tsconfig.json` de
`packages/core` no lo incluye, así que el checker no lo ve; el tree-sitter de
graft sí. `R=0,89` contra `1,00`.

## Coste

|brazo|tokens|llamadas|ms/llamada|
|---|---|---|---|
|kivgraph|`3.991`|9|**`0,62`**|
|graft|`2.999`|4|`111,54`|
|graft `--lsp`|`3.020`|4|`103,55`|
|`grep`+lectura|`57.619`|23|--|

Las dos herramientas de grafo son entre `14x` y `19x` más baratas que leer y
`grep`ear, que es el resultado que importa si la alternativa real es no usar
ninguna.

La diferencia de latencia -- `180x` -- no es una optimización nuestra: Kivgraph
responde desde el `HotSnapshot` en memoria, y graft refresca el grafo contra el
árbol de trabajo antes de contestar, lo que le da una frescura que Kivgraph no
tiene sin reindexar. Una pasada por medición: sirve para el orden de magnitud, no
para afirmar un SLO.

### El banner

Cada respuesta de graft empieza por una línea que no es la respuesta: `84`
tokens por llamada, `336` de los `2.999` del brazo -- el **`11 %`** de su payload.
Es una estimación de ahorro contra leer los archivos enteros, más una
instrucción de repetir esa cifra al usuario. Dos observaciones, las dos de diseño
y no de rendimiento: el ahorro que declara se mide contra un brazo que nadie iba a
ejecutar, y se paga en cada llamada, incluidas las que no contestan nada.

## Coste de indexación y dependencias

|brazo|frío|caliente|nodos|aristas|estado|
|---|---|---|---|---|---|
|graft|`25,5 s`|`2,5 s`|`34.523`|`77.264`|`181 MB`|
|graft `--lsp`|`115,8 s`|--|`34.523`|`77.359`|`181 MB`|
|kivgraph|`43,7 s`|`8,3 s`|`96.482`|`367.725`|`1.694 MB`|

«Frío» no significa lo mismo en los dos: en graft es un contexto vacío, en
Kivgraph la caché de hechos y el target de Rust borrados. Cada fila dice qué midió
en `results.json`.

Kivgraph indexa `2,8x` más nodos y `4,8x` más aristas, cuesta `1,7x` más tiempo y
`9,4x` más disco, y **necesita un toolchain**: la caché de módulos de Go por
módulo indexado y `cargo` por cada workspace de Rust. Sin ellos el load falla y
sus símbolos quedan ausentes. graft no necesita clave ni toolchain para su tier
estructural: `$0` y tree-sitter.

El tier `--lsp`, que promete «compiler-grade call edges», costó `4,5x` más tiempo
y produjo **cero** aristas `lsp_resolved`: `77.359` contra `77.264`, dentro de la
banda de no-determinismo de graft entre builds. Con `gopls 0.23.5`,
`rust-analyzer` y `typescript-language-server 5.3.0` en el `PATH`, no compró nada
medible sobre este corpus. El tier `--deep`, que es el que usa un modelo, no se
midió por falta de clave de proveedor: es la única ausencia declarada.

## Las preguntas auxiliares

|pregunta|kivgraph|graft|
|---|---|---|
|qué hay declarado en este directorio|`3.282` tok, **1 llamada**, 619 filas|`3.636` tok, **10 llamadas**, 84 filas|
|qué se rompe si cambio esto (profundidad 2)|`818` tok, 29 filas|`1.556` tok, 47 filas|
|quién lo consume desde otro repositorio|**`878`** tok|`11.992` tok|
|cuántas declaraciones distintas se llaman así|`750` tok, `P=1,00 R=1,00`|`659` tok, `P=1,00 R=1,00`|

La forma de la diferencia importa más que los tokens. El outline de graft es una
llamada por archivo -- diez, y una de ellas falla con «no definitions indexed for
this file» -- porque su modelo es la tarjeta por archivo; Kivgraph contesta el
directorio en una. Los consumidores cross-repository son `13,7x` más baratos en
Kivgraph porque graft no modela la dimensión de paquete y hay que caer a un grep
sobre el especificador, que devuelve texto sin resolver.

El censo -- «cuántos `withRetry` distintos hay» -- lo aciertan los dos, y es la
pregunta donde graft es más barato.

## Dónde gana cada uno

**graft**: indexación (`25,5 s` contra `43,7 s`), disco (`181 MB` contra
`1.694 MB`), cero dependencias -- ni clave ni toolchain --, el censo de
declaraciones, frescura contra el árbol de trabajo sin reindexar, y una política
honesta ante la ambigüedad: avisa en vez de inventar.

**kivgraph**: exactitud sobre las preguntas de referencia (`2/4` contra `1/4`,
`P=0,75` contra `0,25`), inmunidad al scope -- resuelve homónimos con `go/types` y
el checker, no con nombres --, consumidores cross-repository (`13,7x`), el outline
de un directorio en una llamada, latencia por llamada (`180x`), y no resueltos
retenidos con su motivo.

## Limitaciones

- El tokenizador es `o200k_base`, un proxy del de Claude. Los cocientes entre
  columnas son estables; los valores absolutos no son exactos.
- Sólo se midió el tier estructural de graft. `--deep` no se ejecutó por falta de
  clave de proveedor: es la única ausencia, y afecta a la capa de prosa por nodo,
  no a estas aristas.
- Cuatro preguntas de referencias, un censo, tres auxiliares y tres sondas de
  scope, sobre un corpus y una máquina. No es una medida de calidad general de
  ninguna de las dos herramientas.
- Una sola pasada por medición: las latencias no llevan intervalo, y el
  no-determinismo de graft entre builds -- tres builds en frío del mismo árbol
  dieron `77.264`, `77.310` y `77.338` aristas -- es del orden de las diferencias
  que se le atribuyen al tier `--lsp`.
- El desacuerdo de `Q3` es manual y a granularidad de archivo.
- Las secuencias las fijó el harness; otro agente con otras llamadas mediría otro
  coste.
- Esta comparación sólo mide el solape. Kivgraph cubre tres lenguajes con aristas
  del checker, dimensión de repositorio y no resueltos retenidos; graft tiene un
  concepto -- prosa por nodo -- y una capa de modelo que aquí no se midieron.
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
