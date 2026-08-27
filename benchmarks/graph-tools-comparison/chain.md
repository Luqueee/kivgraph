# Chain: las tres tools que se llaman *después* de una respuesta

Un agente no termina en «quién llama a esto». Sigue: dónde está declarado, dame
su código, qué es. El enrutado del `AGENTS.md` nombra las tres -`find_symbol`,
`get_source`, `get_symbol`- y **ninguna pregunta de ningún conjunto había llamado
a una sola de ellas**. Éste son tres preguntas sobre esas tres, medidas igual que
el resto.

Las métricas crudas están en `results-chain.json` y las respuestas literales en
`raw-chain/`.

## Provenance

|dato|valor|
|---|---|
|fecha|2026-08-21|
|commit|`559e1bf`|
|corpus|`workspace`, 37 repositorios git, `4.683` ficheros, `120.461` símbolos -- **sin Rust**, ver abajo|
|kivgraph|`0.3.6`|
|tokenizador|`tiktoken` `o200k_base`|

## El resultado, y el titular incómodo primero

|pregunta|verdad|kivgraph|nativo|razón|
|---|---|---|---|---|
|`X5` dónde se declara `withRetry`|`7` ficheros|`744` tok, `P=1,00` `R=1,00`|`1.699` tok, `P=1,00` `R=1,00`|**`2,3x`**|
|`X6` el código de tres declaraciones, en una llamada|`3`|`674` tok, `P=1,00` `R=1,00`|`2.071` tok, 3 lecturas|`3,1x`|
|`X7` qué es el `HttpStatus` de `library-shared`|`1`|`176` tok, `P=1,00` `R=1,00`|`1.516` tok|`8,6x`|
|`X8` el código de **veinte** declaraciones, en una llamada|`20`|`19.328` tok, `P=1,00` `R=1,00`|`80.788` tok, `18` lecturas|`4,2x`|

**El nativo acierta las tres.** A diferencia de la familia de consumidores -donde
una búsqueda de texto es estructuralmente ciega ante un reexport por estrella-,
aquí estas tres tools **no compran exactitud: compran tokens**. Conviene decirlo
así, porque es lo que un informe interesado omitiría.

Y `X5` es **el margen más estrecho medido en todo el proyecto**: `2,3x`. Un
`grep` sobre `5.330` ficheros de código devuelve líneas donde se lee
`func withRetry(` o `const withRetryMock`, y un lector separa las declaraciones
de los usos sin abrir nada. El `AGENTS.md` ya avisa de que un nombre raro en un
repositorio pequeño lo resuelve `grep` más barato; esto lo afila: aquí el nombre
es **común** -22 ficheros lo mencionan, 7 lo declaran- el corpus es **grande**, y
`grep` sigue quedándose a `2,3x`. Para la pregunta «dónde está declarado esto» la
ventaja es real pero pequeña.

## La verdad, y el binding que no declara nada

`withRetry` está declarado `7` veces: dos copias Go del mismo fichero, una
tercera en otro paquete Go, tres funciones exportadas de TypeScript y un método
privado de una clase. Las siete se leyeron de los ficheros. El octavo candidato
que ofrece una expresión regular -`const withRetryMock = vi.fn()`- es **otro
identificador** que casó por faltarle el límite de palabra al final.

Pero el grafo tiene `22` símbolos con ese nombre, no `7`. Los `15` restantes son
**bindings**: cada barrel de TypeScript que republica el nombre recibe un símbolo
propio, de kind `export` o `import`. No declaran nada. La verdad no los incluye y
el brazo los aparta contándolos -- «15 forwarding symbol(s) set aside» viaja en la
nota, no se cae en silencio. Es la misma decisión que el ADR 0046 tomó para
`find_references`, donde las aristas de reenvío se retiran salvo que se pidan.

## `get_source` no responde JSON, y eso es correcto

La respuesta es la fuente: una línea de cabecera por cuerpo
-`@ <repo> <ruta>:<inicio>-<fin> <kind> <nombre>`- y el código debajo. Envolver
código en JSON pagaría el escapado de cada comilla y cada salto de línea que
lleva dentro. El brazo lee lo que lee un agente.

Un cuerpo cuenta **sólo si está entero**: abre en la primera línea de la
declaración y cierra en la última, comparadas contra las líneas copiadas del
fichero. Contar líneas habría dado por bueno un cuerpo de la longitud correcta y
el contenido equivocado. Un cuerpo que se corta es exactamente el fallo que esta
familia existe para cazar, y en una página parece un acierto.

## `X8`: la ganancia es el **rango**, no el lote

La tabla de enrutado dice «dame el código de estos símbolos» y añade que se
prefiera a leer cada rango: «sin números de línea, una llamada entre ficheros y
repositorios». Con tres sujetos eso no se puede separar de leer tres ficheros.
Con veinte sí, y el resultado no es el que la frase sugiere.

|medida|una llamada|leer y buscar|
|---|---|---|
|llamadas|`1`|`18`|
|tokens|`19.328`|`80.788`|

`4,2x`, contra `3,1x` con tres sujetos. Siete veces más cuerpos mueven la razón
`1,1x`: **el lote apenas contribuye**. Lo que ahorra es devolver la declaración y
no el fichero -- de `80.788` a `19.328`--, y de esos `19.328` la mayoría es el
código en sí, que se paga igual por cualquier vía. Lo que el lote sí compra es el
número de llamadas, `18` a `1`, que es latencia y no tokens.

Y la frase tiene un techo que no menciona: **`MaximumSourceSymbols = 20`**. La
primera versión de esta pregunta pidió treinta y la tool se negó, correctamente y
en once tokens -- `INVALID_ARGUMENT: symbols must name at most 20 symbols`--. La
cota está en el código y no estaba en la tabla que hace la promesa; ahora sí.

**Y un defecto de este harness, que casi publicó una acusación falsa.** En la
primera pasada `X8` marcó `R=0,90` con la nota «2 cuerpos llegaron sin su línea de
cierre». Los veinte llegaban enteros: `subjectFor` emparejaba el cuerpo devuelto
con su expectativa por **repositorio y ruta**, y veinte declaraciones caen en
dieciocho ficheros -- dos ficheros tienen dos--, así que el segundo cuerpo de esos
dos se comparaba contra las líneas del primero. El nombre entra ahora en el
emparejamiento. Sin ese arreglo, este informe habría dicho que la tool trunca
cuerpos.

## La verdad de `X8` no se copió a mano

Veinte declaraciones con su primera y su última línea son cuarenta líneas de
verdad. Copiarlas a mano invita al error que este proyecto ya cometió tres veces
hoy, y preguntárselas al grafo haría la verdad circular. Salen de un oráculo
aparte: `go/parser` recorriendo el repositorio y quedándose con las funciones de
nivel superior más largas -- `1.780` líneas en dieciocho ficheros--. Es la misma
biblioteca que usa el cargador, y aquí sólo lee spans de fichero.

## Los rivales, ya sin casillas a medias

Tres casillas decían «no implementado, no ausente». Eso es una promesa a medias y
se pagó: las tres se implementaron. `X5` es la única familia que las cuatro
herramientas pueden formular, y con el estado de cada una construido responde así:

|herramienta|tokens|`P`|`R`|qué explica el número|
|---|---|---|---|---|
|**kivgraph**|`744`|**`1,00`**|**`1,00`**|aparta los `15` bindings de reenvío y los cuenta|
|nativo|`1.699`|`1,00`|`1,00`|las líneas de `grep` separan declaración de uso|
|`codebase-memory-mcp`|`762`|`0,70`|`1,00`|encuentra las siete y trae `3` de más|
|`graft`|`2.766`|`0,30`|`1,00`|encuentra las siete y trae `16` de más|
|`graphify`|`611`|`0,00`|`0,00`|vecindario BFS no dirigido sembrado por etiquetas|
|`code-review-graph`|--|--|--|`callers_of` es su única entrada por símbolo|

Lo interesante no es el `1,00` nuestro: es que **`codebase-memory-mcp` y `graft`
encuentran las siete declaraciones**. Su exhaustividad es perfecta y su precisión
no, porque ninguno separa una declaración de un uso -- `search_graph` casa un
nombre y devuelve los nodos que lo llevan; `graft grep` busca en sus tarjetas de
wiring. Para «dónde está declarado esto» eso significa `3` y `16` ficheros de más
que el llamante tiene que descartar leyendo.

Las dos familias restantes siguen fuera y por motivos reales, no por deuda:
ninguna de las cuatro devuelve texto fuente para `X6` y `X8`, y sus nodos no
llevan kind ni span para `X7`.

**Y dos ceros que fueron míos antes de ser suyos.** La primera pasada dio `graft`
a `0` tokens y `graphify` a `30`: con `--skip-indexing` sobre un `state-root`
nuevo, el contexto de `graft` nunca se construyó -- `✗ no graph — run graft build
first`-- y la copia privada del corpus no tenía el repositorio sujeto. Publicar
eso habría sido medir mi montaje y llamarlo herramienta. Es el mismo error que la
regla de corpus incompleto ya vigila en el otro sentido.

## Cobertura, ahora declarada en vez de deducida

El servidor sirve `11` tools. Con este conjunto, las ejercitadas por preguntas
son `8`, y `X8` añade una segunda llamada a `get_source`:

|tool|llamadas en preguntas|
|---|---|
|`find_references`|`19`|
|`get_file_outline`|`3`|
|`get_blast_radius`|`3`|
|`find_cross_repo_consumers`|`2`|
|`trace_dependencies`|`2`|
|`find_symbol`|`1`|
|`get_source`|`2`|
|`get_symbol`|`1`|
|`graph_status`|`0`|
|`list_repositories`|`0` en preguntas; el harness la llama una vez al arrancar|
|`index_project`|`0`|

Las tres a cero no son un hueco del mismo tipo: `graph_status` y
`list_repositories` responden estado y no una pregunta sobre el código, e
`index_project` muta. Quedan fuera por decisión declarada, no por olvido.

## Reproducir

```bash
export HOME=/private/tmp/chainhome
kivgraph init --languages go,typescript,rust --repository <nombre>=<ruta> ...
kivgraph index --full --json

go run ./benchmarks/graph-tools-comparison --set chain \
  --dir /private/tmp/bench-chain --state-root /private/tmp/5way-chain \
  --kivgraph-home /private/tmp/chainhome --skip-indexing
```

## El corpus medido no llevaba Rust

El índice de estas preguntas se construyó sin `cargo` en el `PATH` del harness,
así que `rust-analyzer` rechazó los dos workspaces Cargo de `workspace` y el pase
publicó el resto, declarándolo como `rust_workspaces_not_loaded=2`. El corpus
real son `4.768` ficheros y `123.524` símbolos; el medido aquí, `4.683` y
`120.461`.

No afecta a ninguna cifra de esta tabla: las preguntas son de Go y TypeScript, y
sus verdades se construyeron leyendo los ficheros, no consultando el índice. Lo
que sí queda dicho es que **ninguna de estas preguntas es de Rust**, y que no
podría haberlo sido con este índice.

## Limitaciones

- Tres preguntas, un corpus, una máquina. Dos de las tres verdades tienen uno y
  tres elementos, así que la precisión y la exhaustividad son muy gruesas: lo que
  esta familia mide de verdad es coste.
- El brazo nativo de `X5` se cobra sólo la búsqueda, porque las líneas de `grep`
  bastan para separar declaración de uso en este caso. En un nombre donde no
  bastaran, tendría que abrir ficheros y su coste subiría: la razón `2,3x` es la
  **más favorable al `grep`** que este conjunto puede producir, no un promedio.
- `X6` mide una llamada con tres símbolos. No hay una pregunta con treinta, que
  es donde el batching debería separarse de verdad de leer ficheros.
- Ninguna de las tres preguntas tiene una respuesta que el nativo no pueda
  alcanzar, así que esta familia no dice nada sobre exactitud. La familia
  `reach` sí, y ahí es donde está la única ceguera estructural medida.
