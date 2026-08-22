# Reach: las dos preguntas que el enrutado recomienda y nadie medía

El `AGENTS.md` de la raíz enruta cuatro preguntas a cuatro tools. Dos de ellas
-«quién lo usa desde otro repositorio» y «qué alcanza esto hacia fuera»- no
tenían **ninguna** pregunta en los tres conjuntos publicados: `find_references`
acumulaba 19 llamadas y `find_cross_repo_consumers` y `trace_dependencies`
exactamente `0`. Este conjunto son cuatro preguntas sobre esas dos, medidas igual
que el resto.

Las métricas crudas están en `results-reach.json` y las respuestas literales en
`raw-reach/`. No se emite ningún veredicto de aceptación.

## Provenance

|dato|valor|
|---|---|
|fecha|2026-08-21|
|commit|`d862705`, más el descenso del ADR 0059|
|corpus|`kena`, 37 repositorios git, `4.768` ficheros, `123.531` símbolos -- **con Rust**|
|kivgraph|`0.3.6`|
|tokenizador|`tiktoken` `o200k_base`|

## La verdad se construyó leyendo, y estaba mal dos veces

Cada verdad se levantó resolviendo, para cada fichero que menciona el nombre,
**qué import lo liga**. Y el primer intento falló en las dos direcciones:

1. **Por defecto.** Un `grep` de una línea por `^import .*HttpStatus` perdió un
   consumidor real, porque el import era **multilínea** y el nombre estaba en la
   línea 9 de un bloque que abre en la 2.
2. **Por exceso de confianza en el texto.** `modules/sdk-module-ts/src/index.ts`
   reexporta el sujeto con `export * from "@kena/shared"`. El símbolo cruza ahí
   la frontera de repositorio **en un fichero cuyo texto no lo nombra jamás**. Lo
   encontró el grafo; se verificó aparte enumerando todos los reexports por
   estrella del paquete en el corpus -- hay exactamente uno, y ninguno nombrado.

Ese segundo caso es la única fila de este conjunto que **ninguna** búsqueda de
texto puede alcanzar, y por eso está dentro.

## El resultado

|pregunta|verdad|kivgraph|nativo|
|---|---|---|---|
|`X1` consumidores, TS, enum con cinco rivales|`3`|`530` tok, `P=1,00` `R=1,00`|`12.200` tok, `P=1,00` **`R=0,67`**|
|`X2` consumidores, Go, la respuesta es nada|`0`|`112` tok, `P=1,00` `R=1,00`|`4.271` tok, `P=1,00` `R=1,00`|
|`X3` alcance, Go, nueve declaraciones|`1`|`305` tok, `P=1,00` `R=1,00`|`26.481` tok, `P=1,00` `R=1,00`|
|`X4` alcance, TS, herencia + tipo puro|`2`|`403` tok, `P=1,00` `R=1,00`|`4.185` tok, `P=1,00` `R=1,00`|

Coste: entre `10,4x` y `86,8x` más barato que la búsqueda más la lectura. La
exactitud queda **`4/4`** contra `3/4`: la única que fallaba la cerró el ADR 0059,
y la que el nativo falla no la puede ganar.

## Dónde pierde cada uno, y por qué

**El nativo pierde `X1` por construcción, no por descuido.** Busca el nombre en
`5.330` ficheros de código, lee los seis que lo declaran, y aun así no puede
nombrar el reexport por estrella: no hay nada que buscar en ese fichero. Es el
único sitio de este conjunto donde el brazo nativo no es correcto por
construcción, y el harness lo dice en su nota en vez de regalarle la fila.

**`X4` la perdíamos, y el ADR 0059 la cerró.** `RecommendationsCache` extiende
`BaseCache` y sus dos métodos nombran `ChipbotRecommendationsResponse` por un
`import type`. La respuesta traía `BaseCache` y nada más, porque la contención no
es una arista y una travesía enraizada sólo en la clase no tenía nada que caminar.

Ahora la travesía siembra el contenedor **y sus miembros a profundidad cero**, así
que el tipo aparece a profundidad `1` -- es contenido, no cuesta un salto-- y los
miembros no salen como filas. Medido con el mismo corpus y el mismo estado,
cambiando sólo el binario: `X4` pasa de `233` a `403` tokens y de `R=0,50` a
`R=1,00`, y las otras tres preguntas **no se mueven ni un token**, porque una
función y un método no declaran miembros. El conjunto sube `14,4 %` y pasa de
`3/4` a `4/4`.

Queda una limitación que el ADR declara y que esta tabla no mide: la contención se
deriva del **rango de líneas**, así que sólo cubre miembros que viven dentro de la
declaración. En una clase de TypeScript los métodos van entre sus llaves; en Go
`func (h *T) M()` se declara **fuera** del `struct`, y en Rust viven en un `impl`
que además no se publica. El alcance de un tipo Go o Rust sigue excluyendo el de
sus métodos: `GuildsHandler`, con nueve, responde `3` nodos -- los tipos de sus
tres campos. Es `LUQUE-2010`.

## Los cuatro rivales no están medidos aquí, y no es un cero

|herramienta|consumidores|alcance|
|---|---|---|
|`graft`|un contexto es un árbol: `--dir` construye un grafo por directorio y ninguna respuesta lleva repositorio|`graft callers` es la dirección entrante|
|`graphify`|el grafo se construye por ruta de repositorio y sus respuestas no lo llevan|**medido**: `29`-`30` tok, `P=0,00` `R=0,00` -- ver abajo|
|`codebase-memory-mcp`|indexa un repositorio por llamada y sus filas no llevan repositorio|`search_graph` responde llamantes|
|`code-review-graph`|se construye con `--repo`, un repositorio por grafo|su grafo se organiza alrededor del blast radius, que es la dirección entrante|

Ninguno tiene dimensión de repositorio, así que la familia de consumidores no es
una pregunta que puedan contestar mal: no la pueden formular. Mapearlos a la
fuerza habría medido nuestra ventaja contra una capacidad inventada.

**La casilla de `graphify` en alcance ya no es deuda.** Estaba marcada «no
implementado, no ausente», que es una promesa a medias, y se implementó: su
`query` toma una frase, así que se le pregunta la del enunciado. Responde `29` y
`30` tokens con `P=0,00` y `R=0,00`, y el motivo viaja en la nota de cada fila --
`query` es un vecindario BFS de profundidad 2 sobre un grafo **no dirigido**,
sembrado por coincidencia de etiquetas: no tiene dirección, así que una pregunta
hacia fuera alcanza los mismos vecinos que una hacia dentro, y no tiene noción de
declaración. Puntuarlo es justo; leer el `0,00` sin esa frase, no.

## Reproducir

```bash
export HOME=/private/tmp/reachhome
kivgraph init --languages go,typescript,rust --repository <nombre>=<ruta> ...
kivgraph index --full --json

go run ./benchmarks/graph-tools-comparison --set reach \
  --dir /private/tmp/bench-reach --state-root /private/tmp/5way-reach \
  --kivgraph-home /private/tmp/reachhome
```

## Limitaciones

- Cuatro preguntas, un corpus, una máquina. Dos de las cuatro verdades tienen
  cero o un fichero: son deliberadas -- demostrar una ausencia es lo que el
  enrutado vende-- pero hacen la precisión y la exhaustividad muy gruesas.
- La familia de alcance sólo se mide a profundidad `1`. La transitividad, que es
  la propiedad que `grep` no puede seguir, queda sin medir aquí: el conjunto
  `impact` la mide en la dirección entrante y nadie la mide en la saliente.
- El brazo nativo de la familia de alcance recibe los nombres que el sujeto
  alcanza en lugar de descubrirlos, así que su coste es un **suelo**: un lector
  real los extrae leyendo, y eso cuesta más de lo que aquí se le cobra.
- `X2` demuestra una ausencia entre dos módulos Go que no se importan. No hay una
  pregunta equivalente donde la ausencia sea entre dos repositorios que **sí** se
  importan pero no por este símbolo, que es el caso más difícil.
