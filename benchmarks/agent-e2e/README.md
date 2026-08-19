# End-to-end: ¿ayuda una capa de contexto a entregar un cambio real?

El benchmark de `benchmarks/graft-comparison/` mide preguntas sueltas. Este mide
lo único que le importa a quien usa una: **si el agente entrega el cambio**.

Tres brazos del mismo agente, mismo modelo, mismas herramientas de archivo, sobre
los mismos commits reales de `kena`:

- `cold` — sin capa de contexto.
- `kivgraph` — con el MCP de Kivgraph montado.
- `graft` — con el MCP de graft montado.

Se puntúa contra los archivos que tocó el autor del commit. Sin modelo juez y sin
similitud: `git` dice qué escribió el brazo y el commit dice qué debía escribir.

## Estado

El harness está completo y verificado con un agente stub. **Falta la credencial
del agente**: `claude` responde `OAuth session expired and could not be
refreshed`. Desbloquea con una de las dos:

```bash
claude /login                      # suscripción, flujo interactivo
export ANTHROPIC_API_KEY=sk-...    # por tokens
```

Y entonces:

```bash
go run ./benchmarks/agent-e2e                      # 13 tareas x 3 brazos x 1 trial
go run ./benchmarks/agent-e2e --trials 3           # con repetición
go run ./benchmarks/agent-e2e --only core-6ad7d65  # una tarea
```

## Qué garantiza el harness

- **El corpus no se toca.** Todo ocurre en una copia privada
  (`/private/tmp/e2e-kena`); del original sólo se lee con `git archive`.
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
