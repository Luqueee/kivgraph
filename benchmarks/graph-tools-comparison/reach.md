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
|commit|`132ce20`|
|corpus|`kena`, 37 repositorios git, `4.683` ficheros, `120.461` símbolos|
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
|`X1` consumidores, TS, enum con cinco rivales|`3`|`566` tok, `P=1,00` `R=1,00`|`12.200` tok, `P=1,00` **`R=0,67`**|
|`X2` consumidores, Go, la respuesta es nada|`0`|`112` tok, `P=1,00` `R=1,00`|`4.271` tok, `P=1,00` `R=1,00`|
|`X3` alcance, Go, nueve declaraciones|`1`|`305` tok, `P=1,00` `R=1,00`|`26.481` tok, `P=1,00` `R=1,00`|
|`X4` alcance, TS, herencia + tipo puro|`2`|`233` tok, `P=1,00` **`R=0,50`**|`4.185` tok, `P=1,00` `R=1,00`|

Coste: entre `18,0x` y `86,8x` más barato que la búsqueda más la lectura. La
comparación de exactitud queda `3/4` a `3/4`, y **no en las mismas preguntas** --
que es lo interesante.

## Dónde pierde cada uno, y por qué

**El nativo pierde `X1` por construcción, no por descuido.** Busca el nombre en
`5.330` ficheros de código, lee los seis que lo declaran, y aun así no puede
nombrar el reexport por estrella: no hay nada que buscar en ese fichero. Es el
único sitio de este conjunto donde el brazo nativo no es correcto por
construcción, y el harness lo dice en su nota en vez de regalarle la fila.

**`trace_dependencies` pierde `X4`: no baja a los métodos de una clase.**
`RecommendationsCache` extiende `BaseCache` y sus dos métodos nombran
`ChipbotRecommendationsResponse` por un `import type`. La respuesta trae
`BaseCache` y nada más. Diagnosticado, no supuesto:

|pregunta|alcanza|
|---|---|
|la clase, profundidad `1`|sólo `base-cache.ts`|
|la clase, profundidad `2`|miembros de `BaseCache` y `RedisCacheClient.ts`, **nunca** el tipo|
|el método `getResults`, profundidad `1`|**sí** `types.ts`, vía `TYPE_USES`|

La arista existe y está bien: cuelga del **método**. Lo que no hay es arista
clase -> método, porque la contención no es una dependencia en este grafo. La
consecuencia sí es un defecto: preguntar por una clase **subestima su alcance a
cualquier profundidad y no avisa de que se ha parado**. Un lector que pregunta
«de qué depende esta clase» quiere las dos aristas.

Era la misma forma que el `H2` del conjunto duro: una respuesta coherente con su
propio modelo que contesta una pregunta distinta de la que se hizo, y devuelve un
conjunto más pequeño sin decirlo.

**Cerrado por el ADR 0058, y la fila no se movió.** La travesía no cambió -- sigue
alcanzando `base-cache.ts` y `R` sigue en `0,50`, porque la exhaustividad se mide
sobre ficheros alcanzados y ésos son los mismos. Lo que se fue es el silencio: la
respuesta nombra ahora los miembros cuyas dependencias no forman parte de ella
-`RecommendationsCache.getResults` y `setResults`- y dice cómo preguntarlas. El
`R=0,50` de esta tabla ya no mide una respuesta que se creía completa, sino una
que declara su borde, y eso cuesta `70` tokens: `163` antes, `233` ahora. Ninguna
de las otras tres preguntas se movió ni un token, que es la comprobación de que
el aviso no aparece donde no hace falta.

## Los cuatro rivales no están medidos aquí, y no es un cero

|herramienta|consumidores|alcance|
|---|---|---|
|`graft`|un contexto es un árbol: `--dir` construye un grafo por directorio y ninguna respuesta lleva repositorio|`graft callers` es la dirección entrante|
|`graphify`|el grafo se construye por ruta de repositorio y sus respuestas no lo llevan|alcanzable por su `query` en lenguaje natural: **no implementado**, no ausente|
|`codebase-memory-mcp`|indexa un repositorio por llamada y sus filas no llevan repositorio|`search_graph` responde llamantes|
|`code-review-graph`|se construye con `--repo`, un repositorio por grafo|su grafo se organiza alrededor del blast radius, que es la dirección entrante|

Ninguno tiene dimensión de repositorio, así que la familia de consumidores no es
una pregunta que puedan contestar mal: no la pueden formular. Mapearlos a la
fuerza habría medido nuestra ventaja contra una capacidad inventada. La única
casilla que es deuda nuestra y no límite suyo está marcada: el `query` de
`graphify` es agnóstico de dirección y se puede preguntar.

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
