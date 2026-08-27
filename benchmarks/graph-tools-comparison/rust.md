# Rust: la primera pregunta sobre código real

Hasta hoy la única dimensión Rust medida en este proyecto era `H5_rs_trait`, y
vivía sobre un **fixture sintético**. Peor: los tres conjuntos medidos sobre
`workspace` -`reach`, `chain` y el desglose de `incremental-cost`- se construyeron
sobre un índice **sin Rust**, porque al `PATH` del harness le faltaba `cargo`.
Ésta es la primera pregunta de Rust sobre código que alguien envió a producción.

Las métricas crudas están en `results-rust.json` y las respuestas literales en
`raw-rust/`.

## Provenance

|dato|valor|
|---|---|
|fecha|2026-08-21|
|commit|`11016ce`|
|corpus|`workspace`, 37 repositorios, `126.934` símbolos publicados|
|por lenguaje|`go` `19.166`, `typescript` `128.985`, `rust` `3.063`|
|kivgraph|`0.3.6`|

Ese desglose por lenguaje viaja ahora en `results.json` y el harness **se niega a
medir** si a un lenguaje registrado le salen cero símbolos. Ésa es la regla que
faltaba cuando tres conjuntos publicados midieron un corpus sin Rust.

## El sujeto: la forma que arregló el ADR 0054, sobre código real

`StateStore` tiene **exactamente una** implementación, `MemoryStateStore`, y el
único llamante de su `delete_session` llega por `Arc<dyn StateStore>` --
**dispatch dinámico**. Verdad construida leyendo:

|fichero|qué es|
|---|---|
|`src/state/mod.rs:15`|declara el método en el trait|
|`src/state/memory.rs:38`|lo implementa -- fichero declarante, nunca se cuenta|
|`src/state/memory.rs:546`|lo llama dentro del propio fichero declarante|
|**`src/api_ws/mod.rs:436`**|**`state.store.delete_session(...)`, con `state.store: Arc<dyn StateStore>`**|

El tipo del receptor está en `src/app_state.rs:25`, que es el fichero que un
lector tiene que abrir para saber que esa llamada llega aquí. Verdad: **un
fichero**.

## El resultado

|herramienta|tokens|`P`|`R`|
|---|---|---|---|
|`codebase-memory-mcp`|`788`|`1,00`|`1,00`|
|**kivgraph**|**`186`**|`1,00`|`1,00`|
|nativo (`grep` + leer)|`6.006`|`1,00`|`1,00`|
|`code-review-graph`|`1.000`|`0,00`|`0,00`|
|`graft`|`394`|`0,00`|`0,00`|
|`graphify`|`44`|`0,00`|`0,00`|

**Un rival acierta, y hay que decirlo primero.** `codebase-memory-mcp` responde
bien: encadena `trace_path` y una Cypher sobre las mismas aristas `CALLS`. Somos
`4,2x` más baratos que él y `32,3x` más baratos que buscar y leer -- pero no más
exactos. Es la primera vez en todo el conjunto de comparaciones que un rival
empata en una forma difícil, y el puente del ADR 0054 no es, por tanto, una
capacidad exclusiva.

## Los tres ceros, cada uno por su motivo

- **`code-review-graph`** respondió cero llamantes, y **todo lo que la respuesta
  necesita está dentro de `rs-svc-b`**, que es exactamente el repositorio sobre
  el que construyó su grafo. No es un problema de alcance: es un fallo.

  Esta fila obligó a arreglar el harness. La nota de este brazo estaba
  **enlatada** y afirmaba, siempre, que el llamante había quedado fuera de su
  grafo por construirse un repositorio a la vez. Aquí eso es falso, y la nota
  excusaba un fallo con una causa que no aplica. Ahora la explicación de alcance
  sólo se emite cuando algún fichero de la verdad vive de verdad fuera del
  repositorio del sujeto.
- **`graft`** se negó: no resuelve los llamantes cross-file de un nombre ambiguo,
  y **lo dice** -- avisa de que puede quedarse corto. Una negativa declarada es
  mejor que un cero silencioso, y aquí cuesta `394` tokens.
- **`graphify`** escribe su `graph.json` con `directed: false`, así que `networkx`
  lo carga no dirigido y su `affected.py` cae a un barrido de `graph.edges()`. Es
  un defecto de su propio formato, no de la pregunta.

## Reproducir

```bash
# cargo en el PATH, o rust-analyzer rechaza los workspaces y el corpus sale sin Rust
export PATH="$HOME/.cargo/bin:$PATH"
go run ./benchmarks/graph-tools-comparison --set rust \
  --dir /private/tmp/bench-rust --state-root /private/tmp/5way-rust \
  --kivgraph-home /private/tmp/rshome
```

La copia privada del corpus tiene que contener `services/rs-svc-b`: `graphify`
escribe al lado del código que lee y nunca se le apunta al repositorio real.

## Limitaciones

- **Una pregunta.** Es una dimensión estrenada, no una dimensión cubierta. Con
  `3.063` símbolos Rust indexados hay corpus para varias más: un `impl` entre
  crates del mismo workspace, un trait con tres implementaciones -`AudioSource`
  las tiene-, o un consumidor entre los dos workspaces Cargo, que no se importan.
- La verdad tiene **un** fichero, así que la precisión y la exhaustividad son
  gruesas: distinguen acertar de no acertar, y poco más.
- `rust_unresolved` es `1.969` sobre `3.063` símbolos y este conjunto no pregunta
  nada sobre esa proporción. Es el número que más merece una pregunta propia y
  aquí no la tiene.
- El brazo nativo se cobra la búsqueda más la lectura de los dos ficheros que
  declaran el nombre. Un lector real también abriría `src/app_state.rs` para
  tipar el receptor, así que su coste es un **suelo**.
