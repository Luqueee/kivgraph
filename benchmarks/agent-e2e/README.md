# End-to-end: ¿ayuda una capa de contexto a entregar un cambio real?

El benchmark de `benchmarks/graft-comparison/` mide preguntas sueltas. Este mide
lo único que le importa a quien usa una: **si el agente entrega el cambio**.

Tres brazos del mismo agente, mismo modelo, mismas herramientas de archivo, sobre
los mismos commits reales de `workspace`:

- `cold` — sin capa de contexto.
- `kivgraph` — con el MCP de Kivgraph montado.
- `graft` — con el MCP de graft montado.

Se puntúa contra los archivos que tocó el autor del commit. Sin modelo juez y sin
similitud: `git` dice qué escribió el brazo y el commit dice qué debía escribir.

## Estado

Barrido corrido: **36 ejecuciones** (6 tareas × 3 brazos × 2 trials),
`claude-sonnet-5`, tope de `$3,00` por ejecución, `$47,62` y 2 h 1 min.
El resultado y su lectura están en `report.md`; el resumen es que **ninguna de las
dos capas de contexto mejora a un agente sin capa sobre estas tareas**, y las dos
cuestan más.

Dos condiciones anteriores se conservan aparte porque miden otra cosa:
`uncapped-pilot/` (sin tope: 54-75 turnos por ejecución, ninguno converge) y
`starved-1.50/` (tope de `$1,50`: ahoga la tarea Go de 6 archivos en los tres
brazos).

```bash
go run ./benchmarks/agent-e2e --kivgraph /private/tmp/kivgraph-e2e \
  --model claude-sonnet-5 --trials 2 --budget-usd 3.0 --only <ids>
```

El agente necesita credencial de suscripción (`claude auth login --claudeai`); no
hay soporte de API key en esta configuración.

## Qué garantiza el harness

- **El corpus no se toca.** Todo ocurre en una copia privada
  (`/private/tmp/e2e-workspace`); del original sólo se lee con `git archive`.
- **La respuesta no está al alcance.** El repositorio de la tarea se reconstruye
  en el padre del commit como un repositorio de **un solo commit**: el commit que
  se está reimplementando es inalcanzable, y `assertNoLeak` lo comprueba en cada
  tarea en vez de suponerlo.
- **La copia arranca limpia.** `normalize` descarta lo que estuviera sin
  commitear en el original -- este corpus tiene dos `go.sum` sucios -- para que el
  scorer no los lea como ediciones del agente.
- **Sin shell.** `Bash` está en `--disallowedTools`: con él, un `git log` es una
  ruta a la respuesta en vez de al código.
- **Los prompts no nombran archivos.** Se construyen del asunto y el primer
  párrafo del commit, con todo token con pinta de ruta sustituido; un test lo
  verifica sobre el set congelado.
- **Auditoría de fuga** por ejecución: el transcripto se guarda entero en `raw/`
  y se revisa por el hash del commit y por accesos a `.git`.
- **Resultado incremental.** `results.json` se reescribe tras cada ejecución, así
  que un barrido interrumpido conserva lo medido.

## Coste medido del andamiaje (sin agente)

|paso|coste|
|---|---|
|copia privada del corpus|`3 s`, `315 MB`|
|índice de Kivgraph por estado de tarea|`32,3 s` el primero, `8,7 s` con caché de hechos caliente|
|build de graft por estado de tarea|`2,5 s` / `2,1 s`|

El índice se cobra **una vez por tarea**, no por brazo: los tres consultan la
misma generación. Para las 13 tareas son unos `3`–`7` min de andamiaje.

Lo que no está medido es el coste en tokens del agente, porque requiere la
credencial. La matriz es de `13 × 3 × trials` ejecuciones.
