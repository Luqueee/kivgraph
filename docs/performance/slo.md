# SLO de rendimiento

- **Estado:** aceptado como contrato inicial
- **Fecha:** 2026-08-04
- **Alcance:** backend de Kivgraph, sin overhead del cliente MCP

Este documento convierte los objetivos de rendimiento del plan en límites
medibles. Un resultado de benchmark debe conservar el comando, el commit, el
hardware, el corpus, la semilla y las condiciones de ejecución. Las métricas
se expresan en milisegundos salvo que indiquen otra unidad.

## Definiciones

- **p50:** percentil 50 de la latencia observada.
- **p95:** percentil 95; representa la latencia que no debe superar el 95 % de
  las operaciones.
- **p99:** percentil 99; se usa para controlar la cola larga.
- **RSS:** máximo de memoria residente del proceso durante la ejecución.
- **Visible:** tiempo desde que el watcher recibe el cambio hasta que el
  snapshot que lo contiene queda disponible para consultas.
- **Error:** respuesta fallida, proceso abortado, timeout o resultado no
  clasificable. `UNRESOLVED` y `CANDIDATE` son resultados semánticos válidos,
  no errores de rendimiento.

## SLO de consultas MCP

Los objetivos se miden con el HotSnapshot activo y cache caliente, en el
backend, sin transporte ni serialización del cliente.

| Operación | p50 | p95 | p99 | Parámetros |
| --- | ---: | ---: | ---: | --- |
| `graph_status` | ≤ 0,25 ms | ≤ 1 ms | ≤ 2 ms | Sin parámetros |
| `list_repositories` | — | ≤ 1 ms | — | Página por defecto |
| `get_symbol` | ≤ 0,25 ms | ≤ 1 ms | ≤ 2 ms | Stable key exacta |
| `find_symbol` exacto | ≤ 0,5 ms | ≤ 2 ms | ≤ 5 ms | Coincidencia exacta |
| `find_references` | ≤ 1 ms | ≤ 5 ms | ≤ 10 ms | Hasta 100 referencias |
| `find_cross_repo_consumers` | — | ≤ 5 ms | ≤ 15 ms | Página por defecto |
| `trace_dependencies` | — | ≤ 12 ms | ≤ 25 ms | Profundidad 3 |
| `get_blast_radius` | — | ≤ 20 ms | ≤ 40 ms | Profundidad 3 |
| `get_blast_radius` | — | ≤ 50 ms | ≤ 100 ms | Profundidad 5 |

Una operación que alcance `TRAVERSAL_LIMIT_REACHED` por superar sus límites
configurados no se considera una violación de latencia por sí misma, pero el
resultado debe declarar `truncated` y el código de error correspondiente.

## SLO de reindexación

No hay SLO de actualización incremental porque no hay camino incremental: el
delta se retiró en el [ADR 0057](../adr/0057-el-camino-incremental-se-retira.md).
Toda reindexación es una pasada completa que publica una generación nueva.

| Operación | Disponibilidad visible |
| --- | ---: |
| Reindexación con caché de hechos caliente | objetivo ≤ 15 s |
| Reconstrucción inicial, caché en frío | objetivo ≤ 60 s; límite provisional ≤ 120 s |

El tiempo de indexación incluye detección, análisis, escritura del grafo
canónico, integridad, construcción y validación del snapshot hasta que este
queda visible. El tiempo de consulta se mide por separado.
Durante una pasada válida, las consultas existentes continúan leyendo el
snapshot anterior. No se publica un snapshot parcial aunque una fase falle: la
generación candidata sólo pasa a `CURRENT` si supera integridad y validación.

Lo que abarata una reindexación es la caché de hechos, no un delta: medido sobre
el corpus `kena` (`4.683` ficheros, `477.027` aristas) los motores de lenguaje
con caché caliente son `0,57 s` de un pase de `9,17 s`. Las cifras están en
`benchmarks/incremental-cost/report.md`.

## Memoria y almacenamiento

| Componente | Objetivo | Límite provisional |
| --- | ---: | ---: |
| Servidor en reposo | ≤ 500 MiB RSS | — |
| HotSnapshot | ≤ 400 MiB | — |
| Worker TypeScript | ≤ 1,5 GiB RSS | ≤ 3 GiB |
| Indexación completa agregada | — | ≤ 4 GiB RSS |
| LadybugDB para corpus inicial | ≤ 2 GiB | — |

El RSS se mide como máximo de la ejecución completa, incluyendo los workers
hijos. Las mediciones deben indicar si el proceso se ejecutó en frío o con
cache caliente. Los logs tienen rotación y límite configurables; no se acepta
crecimiento ilimitado como comportamiento normal.

Por defecto se retienen como máximo tres snapshots binarios. Una limpieza no
puede borrar el snapshot activo ni el último snapshot válido requerido para
recuperación.

## Límites de recorrido y paginación

Los límites son parte del contrato y no se aumentan para maquillar un
benchmark:

```yaml
mcp:
  default_limit: 50
  maximum_limit: 500
  maximum_depth: 5
  maximum_visited_nodes: 25000
```

Las consultas de referencias usan un límite de 100 en el workload de SLO. Cada
respuesta relevante declara `snapshot_id`, `snapshot_age`, `total`, `returned`,
`truncated`, `next_cursor`, `exact_results`, `unresolved_related` y `coverage`
cuando esos campos apliquen.

Un cursor está ligado al snapshot y a la versión de ordenación. Si cambia el
snapshot, la consulta devuelve `CURSOR_SNAPSHOT_EXPIRED` en vez de mezclar
resultados de dos versiones.

## Procedimiento de benchmark

1. Ejecutar desde un checkout limpio y registrar el commit o indicar `dirty`.
2. Registrar OS, arquitectura, CPU, memoria, versión de Go, Node.js, pnpm,
   TypeScript, LadybugDB y el worker.
3. Registrar corpus, número de símbolos y aristas, repositorios, seed y todos
   los parámetros de generación.
4. Separar warm-up de mediciones. El benchmark de consultas usa al menos
   10.000 llamadas por operación y repite con 1, 4, 16 y 32 clientes.
5. Medir p50, p95, p99, throughput, allocations/op, bytes/op, RSS máximo,
   goroutines y errores.
6. Medir por separado consulta puntual, recorridos de profundidad 3 y 5,
   full rebuild, delta e invalidación. No mezclar latencia de indexación con
   latencia del HotSnapshot.
7. Guardar el resultado estructurado en
   `benchmarks/<nombre>/results.json` y el análisis en
   `benchmarks/<nombre>/report.md`.
8. El informe debe comparar contra este documento, indicar si todos los SLO se
   cumplen y listar limitaciones, regresiones y resultados no concluyentes.

El JSON mínimo sigue este esquema:

```json
{
  "benchmark": "nombre-estable",
  "command": "comando reproducible",
  "commit": "sha o dirty",
  "environment": {
    "os": "...",
    "arch": "...",
    "cpu": "...",
    "memory": "..."
  },
  "dataset": {
    "name": "...",
    "seed": 0,
    "parameters": {}
  },
  "metrics": {
    "p50_ms": 0,
    "p95_ms": 0,
    "p99_ms": 0,
    "throughput_per_s": 0,
    "allocations_per_op": 0,
    "bytes_per_op": 0,
    "rss_bytes": 0,
    "goroutines": 0,
    "errors": 0
  }
}
```

## SLO del visor web

Estos límites aplican a la aplicación React/Vite/Reagraph/Three.js y a su API
HTTP read-only. No modifican los SLO de las tools MCP ni convierten STDIO en
un transporte web. El corpus de referencia es el fixture sintético versionado
de 100.000 símbolos y 1.000.000 de aristas; cada medición debe registrar
también GPU, navegador, commit, seed y modo de nivel de detalle.

| Métrica | Objetivo | Límite | Condiciones |
| --- | ---: | ---: | --- |
| `meta` HTTP p95 | ≤ 25 ms | 50 ms | snapshot caliente, localhost |
| payload de topología p95 | ≤ 250 ms | 500 ms | viewport/LOD declarado |
| primer frame p95 | ≤ 500 ms | 1 s | navegador frío, bundle local |
| pan/zoom p95 | ≤ 16,6 ms | 33,3 ms | interacción sostenida |
| hover p95 | ≤ 2 ms | 5 ms | raycast de nodo Reagraph |
| vecindad p95 | ≤ 2 ms | 5 ms | depth 3, presupuesto declarado |
| payload máximo | ≤ 16 MiB | 32 MiB | una respuesta, sin descarga completa |
| heap JavaScript | ≤ 384 MiB | 512 MiB | después de warm-up |
| errores del workload | 0 | 0 | incluyendo cancelaciones mal clasificadas |

El visor debe ocultar o agregar aristas de símbolo cuando el nivel de detalle
lo requiera. Medir una topología completa no autoriza a ignorar el presupuesto
de interacción: pan, zoom y hover se evalúan por separado.

### Criterio `WEB_VIEWER_PERFORMANCE_PASS`

El gate solo puede emitirse cuando:

- todas las métricas requeridas están presentes y tienen unidades conocidas;
- el workload completa sin errores y valida `snapshot_id` y versión de payload;
- primer frame, interacción, picking y memoria respetan sus límites;
- el informe conserva comando, commit, entorno, navegador, GPU, corpus, seed y
  parámetros de LOD;
- se ejecuta al menos una repetición con un corpus que contenga hubs o una
  limitación explícita impide generalizar el resultado del fixture uniforme.

## Criterio de cumplimiento

Un benchmark pasa este contrato cuando:

- todas las métricas requeridas están presentes y tienen unidades conocidas;
- los errores son cero en workloads de éxito;
- cada operación está dentro de sus percentiles objetivo;
- los límites de memoria, almacenamiento y retención se respetan;
- no existe crecimiento continuo de RSS o goroutines después del warm-up;
- la actualización no publica estados parciales;
- el informe y el JSON son reproducibles con el comando registrado.

Una desviación puede registrarse como limitación durante desarrollo, pero no se
emite `PROJECT_FOUNDATION_PASS` mientras los criterios de la fase no estén
satisfechos o exista una excepción documentada y autorizada.
