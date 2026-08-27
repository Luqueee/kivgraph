# El caso trivial: donde `grep` gana, medido

Hasta hoy este proyecto afirmaba perder en el caso fácil y no lo había medido
nunca. El `AGENTS.md` de la raíz decía que «un nombre raro en un solo
repositorio pequeño lo resuelve `grep` más barato — una llamada, sin esquema,
sin resolver un símbolo primero» y a continuación confesaba que el harness «no
incluye todavía el caso genuinamente trivial», así que esa desventaja «sigue
siendo estructural y no una fila medida».

Ésta es la fila.

## Provenance

|dato|valor|
|---|---|
|fecha|`2026-08-22`|
|versión|`kivgraph 0.5.0`, commit `c1bde5d`|
|corpus|`workspace`, 37 repositorios registrados, `5.330` ficheros de código|
|generación|`000001`, un pase completo con el binario de `v0.5.0`|
|tokenizador|`tiktoken` `o200k_base`|
|datos crudos|`results-all.json` y `raw-all/`, una sola pasada `--set all` de las `29` preguntas|

`results-all.json` es el fichero autoritativo para la columna del `grep` nativo:
los `results-*.json` por conjunto son anteriores a la corrección de cobro que se
describe más abajo y su columna nativa está sobrevalorada.

## Elegir el sujeto **es** la pregunta

Dos candidatos que parecían triviales se rechazaron por ser nuestro terreno
disfrazado de fichero pequeño, y merece la pena decir cuáles:

- **`userMetaKey`** aparece dos veces, y las dos dentro de su propio fichero
  declarante. La verdad de una pregunta de referencias excluye el fichero que
  declara, así que sería el conjunto vacío: eso mide **ausencia**, que ya es
  `A1`, no trivialidad.
- **`jsonMarshal`** parecía inmejorable — vive en un fichero de **nueve
  líneas**— y es un **homónimo**: dos funciones no exportadas con ese nombre en
  dos paquetes de dos repositorios, con firmas distintas, y nueve líneas de
  `grep` que un lector tiene que separar. Ése es el caso que **ganamos**.

`newGMCClient` sí lo es: **dos apariciones en todo el corpus**, una declaración
y una llamada, el mismo paquete, ningún homónimo en cinco lenguajes. La línea
del `grep` muestra `func newGMCClient(` frente a `l.mc = newGMCClient(...)`, así
que un lector termina en la búsqueda sin abrir nada.

## El resultado

`T1_go_trivial` — «¿qué ficheros llaman a `newGMCClient` en
`services/go-svc-a`?». Verdad: un fichero.

|brazo|tokens|llamadas|`P`|`R`|
|---|---|---|---|---|
|`graphify`|`108`|`1`|`1,00`|`1,00`|
|**`kivgraph`**|**`123`**|**`1`**|`1,00`|`1,00`|
|`graft`|`210`|`1`|`1,00`|`1,00`|
|`codebase-memory-mcp`|`282`|`3`|`1,00`|`1,00`|
|`code-review-graph`|`397`|`1`|`1,00`|`1,00`|
|**`grep` nativo**|**`65`**|**`1`**|`1,00`|`1,00`|

**Los seis aciertan, y `grep` es el más barato de todos.** Costamos `1,9x` lo
que cuesta la búsqueda: `0,53x` en la dirección en que este harness publica sus
ratios. La afirmación del `AGENTS.md` queda **confirmada**, y por primera vez con
un número detrás.

Que ganemos a los otros cuatro grafos aquí no es el titular. El titular es que
los cinco grafos pierden contra una línea de `grep`, y la razón es estructural:
el sobre de una respuesta MCP — `snapshot_id`, `coverage`, `total`, la fila con
repositorio y rango— cuesta más que dos líneas de texto cuando la respuesta
**son** dos líneas de texto.

## Una corrección al harness que salió de aquí

Medir esto obligó a arreglar cómo se cobra al brazo nativo, y el arreglo mueve
cifras ya publicadas.

La primera pasada dio `native` a `401` tokens: `65` de `grep` y **`336` de leer
entero el fichero que declara**. Ese cobro tenía una única justificación escrita
en el propio harness — el campo `Declarations` dice que el brazo nativo lee todas
las declaraciones porque «es el mínimo que un lector necesita para distinguir
homónimos, y es por eso que `grep` solo no puede responder». Con **una sola**
declaración no hay nada que distinguir, y la rama `familyLocate` ya hacía
exactamente ese juicio: cobra sólo la búsqueda porque «la línea del `grep` ya
muestra `func withRetry(`».

Así que la lectura se cobra sólo cuando hay más de una declaración, y sólo en la
familia de referencias. Lo segundo importa: `familyImpact` comparte esa rama y
sus lecturas **no** son sobre homónimos — una pregunta transitiva se responde
encontrando la declaración que **encierra** cada hit para dar el salto
siguiente, así que el fichero hay que abrirlo pase lo que pase. Saltárselo ahí
bajó una pregunta de profundidad tres a `94` tokens, una respuesta que ningún
`grep` produce. Ése es el error opuesto y es igual de deshonesto; está acotado
en el código y comentado.

Seis preguntas ya publicadas cambian de ratio con la corrección, todas a favor
del rival:

|pregunta|kivgraph|nativo|ratio|
|---|---|---|---|
|`R3_ts_intra`|`1.090`|`2.825`|`2,59x`|
|`H3_ts_type`|`366`|`455`|`1,24x`|
|`H4_ts_alias`|`132`|`138`|`1,05x`|
|`A1_go_absent`|`113`|`29`|`0,26x`|
|`A2_ts_absent`|`110`|`42`|`0,38x`|
|`A3_rs_absent`|`145`|`68`|`0,47x`|

`H4` queda en `1,05x`, que es un empate a efectos prácticos.

## Las cinco filas donde perdemos, y qué significan

Sobre las `29` preguntas de los seis conjuntos, el `grep` nativo sale más barato
en cinco, todas con la **misma** exhaustividad `R=1,00`:

|pregunta|familia|ratio|
|---|---|---|
|`A1_go_absent`|`references`|`0,26x`|
|`A2_ts_absent`|`references`|`0,38x`|
|`I1_go_depth2`|`impact`|`0,39x`|
|`A3_rs_absent`|`references`|`0,47x`|
|`T1_go_trivial`|`references`|`0,53x`|

La mediana del conjunto entero sigue en `5,95x` a nuestro favor, con máximos de
`86,8x` (`X3`) y `83,4x` (`H5`).

**Las tres de ausencia piden una distinción que un ratio de tokens no captura, y
esta vez está medida y no argumentada.** Un `grep` sin resultados es barato, y no
es una prueba: no distingue «nadie lo llama» de «los llamantes lo escriben de
otra forma». En este mismo corpus, `X1` lo demuestra — un consumidor real
**nunca deletrea el símbolo**, porque un fichero lo reexporta con `*` y lo cruza
de repositorio sin nombrarlo. El brazo nativo se queda en `R=0,67` ahí y lo
declara en su propia nota. Así que las tres filas de ausencia son a la vez
ciertas en el coste y no equivalentes en la evidencia: `29` tokens de silencio no
son lo mismo que `113` de un «no hay ninguna» que un checker sostiene.

`I1_go_depth2` es una pérdida limpia y sin matices: `2.322` contra `897`, misma
exhaustividad. Nuestra respuesta de impacto es verbosa para ese sujeto.

## Limitaciones

- **Un sujeto.** `newGMCClient` es un caso trivial, no *el* caso trivial. Un
  segundo sujeto en TypeScript o Rust diría si `1,9x` es el orden de magnitud o
  una casualidad de este fichero de 53 líneas.
- **El sobre no está desglosado.** Sabemos que costamos `123` tokens y no cuánto
  de eso es `snapshot_id`, `coverage` y `total` frente a la fila misma. Medirlo
  diría si la desventaja se puede cerrar o es el precio de ser direccionable.
- **`P` y `R` son ciegos al valor de la respuesta.** Los seis brazos aciertan;
  ninguna columna dice que nuestra fila trae repositorio, ruta, nombre
  cualificado y rango, y que la del `grep` trae una línea de texto.

## Reproducir

```bash
go run ./benchmarks/graph-tools-comparison --set trivial \
  --dir /tmp/bench-trivial --state-root /tmp/st --kivgraph-home /tmp/home \
  --kivgraph /ruta/a/kivgraph --graft-context /tmp/st/graft-ctx
```
