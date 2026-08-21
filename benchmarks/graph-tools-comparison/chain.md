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
|corpus|`kena`, 37 repositorios git, `4.683` ficheros, `120.461` símbolos -- **sin Rust**, ver abajo|
|kivgraph|`0.3.6`|
|tokenizador|`tiktoken` `o200k_base`|

## El resultado, y el titular incómodo primero

|pregunta|verdad|kivgraph|nativo|razón|
|---|---|---|---|---|
|`X5` dónde se declara `withRetry`|`7` ficheros|`744` tok, `P=1,00` `R=1,00`|`1.699` tok, `P=1,00` `R=1,00`|**`2,3x`**|
|`X6` el código de tres declaraciones, en una llamada|`3`|`674` tok, `P=1,00` `R=1,00`|`2.071` tok, 3 lecturas|`3,1x`|
|`X7` qué es el `HttpStatus` de `library-shared`|`1`|`176` tok, `P=1,00` `R=1,00`|`1.516` tok|`8,6x`|

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

## Los cuatro rivales, otra vez no medidos y otra vez no un cero

|herramienta|localizar|cuerpos|hechos|
|---|---|---|---|
|`graft`|`grep` y `skeleton` son lo más cercano y ninguno enumera las declaraciones de un nombre: **no implementado**|`show` devuelve una tarjeta, no un cuerpo por nombre cualificado|informa símbolos dentro de una tarjeta, sin kind y span por declaración|
|`graphify`|alcanzable por su `query`: **no implementado**|devuelve nodos y aristas, no texto fuente|sus nodos no llevan kind y span por declaración|
|`codebase-memory-mcp`|`search_graph` casa nombres pero no separa declaración de uso: **no implementado**|devuelve nodos, no fuente|sus nodos no llevan kind y span|
|`code-review-graph`|`callers_of` es su única entrada por símbolo|devuelve ficheros impactados, no fuente|informa impacto, no un registro de declaración|

Tres casillas dicen **no implementado** y no «ausente»: son deuda de este harness,
no límite de la herramienta. Están marcadas para que nadie lea la tabla como una
victoria que no se ha disputado.

## Cobertura, ahora declarada en vez de deducida

El servidor sirve `11` tools. Con este conjunto, las ejercitadas por preguntas
son `8`:

|tool|llamadas en preguntas|
|---|---|
|`find_references`|`19`|
|`get_file_outline`|`3`|
|`get_blast_radius`|`3`|
|`find_cross_repo_consumers`|`2`|
|`trace_dependencies`|`2`|
|`find_symbol`|`1`|
|`get_source`|`1`|
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
así que `rust-analyzer` rechazó los dos workspaces Cargo de `kena` y el pase
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
