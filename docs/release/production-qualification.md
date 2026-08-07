# Calificación de producción de Ladygraph

**Fecha de verificación:** 2026-08-07
**Decisión:** `ACCEPT_LADYGRAPH_WITH_LIMITS`

## Decisión

Ladygraph queda aceptada para producción dentro del entorno calificado y con
los límites operativos de este documento. Los 16 gates globales definidos en
`TASKS.md` están emitidos; no se aprueba una ampliación silenciosa de plataforma,
corpus, transporte o durabilidad.

La calificación cubre el pipeline real: extracción, normalización de hechos,
almacenamiento canónico en LadybugDB, validación, publicación de generación,
construcción del `HotSnapshot` y consultas MCP read-only. Una generación inválida
no se publica como `CURRENT`, y el servidor consulta el snapshot publicado, no
LadybugDB directamente.

## Gates globales

| Gate | Estado | Evidencia principal |
| --- | --- | --- |
| `PROJECT_FOUNDATION_PASS` | `PASS` | Fase 0 completada; convenciones, ADRs, SLO y CI registrados en `TASKS.md`. |
| `EMPTY_MCP_PERFORMANCE_PASS` | `PASS` | `benchmarks/mcp-empty/report.md`: cero errores, sin crecimiento continuo y gate aprobado según el p95 del handler. |
| `LADYBUG_STORAGE_PASS` | `PASS` | `docs/decisions/ladybugdb-qualification.md` y `benchmarks/ladybug-bulk/full-scale/report.md`. |
| `HOT_SNAPSHOT_PASS` | `PASS` | `benchmarks/hotsnapshot/report.md`: scan completo bajo 1 s y consultas dentro de sus límites. |
| `REPOSITORY_REGISTRY_PASS` | `PASS` | Detección de providers Go/TypeScript, conflictos y registries en `TASKS.md`. |
| `TREE_SITTER_ACCELERATOR_PASS` | `PASS` | Grammars fijadas, parser manager, inventario y suite de fase en `TASKS.md`. |
| `TYPESCRIPT_LOCAL_PASS` | `PASS` | Suite local con 45 tests, `pnpm check` y `pnpm build`. |
| `TYPESCRIPT_CROSS_REPO_PASS` | `PASS` | `benchmarks/typescript-cross-repo/report.md`: 11/11 true positives, 0 false exact edges, 4/4 unresolved. |
| `GO_SEMANTIC_PASS` | `PASS` | `benchmarks/go-semantic/report.md`: 16/16 true positives, 0 false exact edges, 2/2 unresolved. |
| `CANONICAL_GRAPH_PASS` | `PASS` | `docs/decisions/canonical-graph-qualification.md` y `doctor graph` con invariantes a cero. |
| `INCREMENTAL_INDEXING_PASS` | `PASS` | `benchmarks/ladybug-incremental/report.md`: p95 de 571,6 ms, 617,8 ms y 845,3 ms; 0 ghost edges. |
| `MCP_SURFACE_PASS` | `PASS` | Superficie read-only de nueve tools y rechazo de tools prohibidas documentados en `TASKS.md`. |
| `RESILIENCE_PASS` | `PASS` | Ocho escenarios de recuperación con `all_passed: true` en `benchmarks/ladybug-recovery/results.json`. |
| `PERFORMANCE_PASS` | `PASS` | Benchmark MCP de 32 clientes y regresión semántica posterior a la optimización. |
| `OBSERVABILITY_PASS` | `PASS` | `benchmarks/observability-overhead/report.md`: 0 B/op y 0 allocs/op en las tres variantes. |
| `DISTRIBUTION_PASS` | `PASS` | Dos checkouts limpios: payload, `SHA256SUMS` y `manifest.json` idénticos. |

## Evidencia de exactitud y seguridad semántica

- Go: `GO_SEMANTIC_PASS`, 16/16 true positives, precisión y recall `1.0000`,
  0 false positives, 0 false negatives, 0 false exact edges y 2/2 referencias
  no resueltas correctamente clasificadas.
- TypeScript: `TYPESCRIPT_CROSS_REPO_PASS`, 11/11 true positives, precisión y
  recall `1.0000`, 0 false positives, 0 false negatives, 0 false exact edges,
  4/4 referencias no resueltas y 10/10 posiciones fuente mapeadas.
- La auditoría de la generación canónica publicada `000003` devolvió cero en
  `exact_edge_without_source`, `exact_edge_without_target`,
  `missing_evidence_file`, `unknown_confidence`, `duplicate_stable_key` e
  `invalid_repository_ownership`.
- Los fixtures negativos conservan motivos, repositorio, lenguaje y evidencia
  observada. Providers ambiguos, replacements conflictivos, declaration maps
  ausentes y destinos TypeScript no demostrados permanecen como `UNRESOLVED`;
  nunca se convierten en aristas `EXACT` por coincidencia nominal, textual o de
  path.

## Evidencia de escala, almacenamiento y publicación

El corpus sintético privado de aceptación se ejecutó con semilla `42`:

```text
40 repositorios
100.000 archivos
1.000.000 símbolos
1.100.040 nodos
10.000.000 aristas
```

La carga `COPY` tardó `9.141,3 ms`, con `1.214.272,8 registros/s`,
`2.079.531.008` bytes de RSS máximo y una base de `432.570.368` bytes. Dos
cargas independientes conservaron los mismos conteos, schema e integridad:

```text
Repository=40
File=100000
Symbol=1000000
CONTAINS=100000
DEFINES=1000000
REFERENCES=4450001
CALLS_DIRECT=4449999
integrity_violations=0
```

El corpus y los resúmenes lógicos fueron byte a byte idénticos. Los archivos
nativos `graph.db` no lo fueron: `432.570.368` frente a `433.037.312` bytes.
La garantía es de reproducibilidad lógica, no de bytes físicos de LadybugDB.

LadybugDB y el binding Go están fijados conjuntamente en `v0.13.1`. La carga
full usa `COPY`; las mutaciones incrementales validan extremos y duplicados en
transacciones, con staging `COPY` para lotes grandes. La publicación usa
candidatas privadas, valida antes de cambiar `CURRENT`, conserva backups y
rechaza rollback ante digest ausente o divergente.

El `HotSnapshot` pasó el scan de `1.200.040` filas con p95 estimate de
`946,599 ms`, build completo en `712,804 ms` y publicación en `725,769 ms`.
Las consultas exactas y las travesías quedaron dentro de sus límites medidos.

## Evidencia MCP, resiliencia y observabilidad

- La superficie real registra exactamente nueve tools read-only. Las tools
  prohibidas responden `unknown tool`.
- El benchmark MCP de 32 clientes, con 100.000 símbolos y 1.000.000 de aristas,
  obtuvo p50 `0,600691 ms`, p95 `3,780575 ms`, p99 `9,775364 ms`, throughput
  `25.351,8 calls/s`, cero errores y sin crecimiento continuo de RSS.
- Las pruebas de resiliencia cubren `SIGKILL` durante inserción, antes de
  `COMMIT` y durante `COPY`, reapertura, truncamiento, permisos y fallos
  `ENOSPC` durante mutación y publicación. `CURRENT`, checksum y la generación
  activa permanecen protegidos.
- El benchmark de observabilidad midió `138,66 ns/op` para el registro local,
  `157,84 ns/op` para OpenTelemetry `noop` y `636,68 ns/op` para `ManualReader`;
  las tres variantes produjeron `0 B/op` y `0 allocs/op`.

## Repetición con el par fijado

El 2026-08-07 se repitieron recuperación y rendimiento sobre el commit
`45220d30c17d4521568dde6e1f8ae2aa4e367356`, Linux `amd64`, Go `1.24.4` y el
par LadybugDB core/binding `v0.13.1`. Los resultados versionados están en
`benchmarks/ladybug-recovery-pinned/` y `benchmarks/mcp-client-32-pinned/`.

- Recuperación: 8/8 casos correctos, `source_unchanged: true` y
  `all_passed: true`. El SHA-256 de la base privada fue idéntico antes y
  después.
- MCP con 32 clientes, 10.000 llamadas, 100 warm-ups por cliente, 100.000
  símbolos y 1.000.000 de aristas: p50 `0,509040 ms`, p95 `3,351542 ms`, p99
  `8,026664 ms`, throughput `26.267,3 calls/s`, cero errores y sin crecimiento
  continuo de memoria.
- Las cinco comprobaciones SLO de backend MCP pasaron. El resultado sigue
  excluyendo STDIO, sockets y red; no se convierte en un SLO de transporte.

## Distribución y operación

El bundle `linux/amd64` incluye el binario Go, el worker TypeScript, LadybugDB,
grammars, licencias, `manifest.json` y `SHA256SUMS`. Dos checkouts limpios del
mismo commit, toolchain y plataforma produjeron payload idéntico y pasaron
`sha256sum -c SHA256SUMS`, `version --json`, el worker `hello` y extracción
TypeScript.

Requisitos obligatorios del despliegue:

- Linux `amd64` y bibliotecas estándar compatibles;
- Node.js `22` o posterior para el bundle;
- core y binding LadybugDB fijados y verificados por checksum;
- no modificar repositorios indexados;
- no mutar generaciones activas in-place;
- conservar backups y verificar su restauración;
- servir consultas MCP desde el `HotSnapshot` publicado.

## Límites y riesgos residuales

1. La exactitud semántica se midió contra fixtures versionadas. No demuestra
   precisión sobre cualquier repositorio externo no indexado.
2. Los benchmarks MCP usan transporte en memoria y no cubren STDIO, sockets ni
   red. El rerun fijado obtuvo p95 round-trip `3,351542 ms` con 32 clientes;
   todavía debe medirse el transporte objetivo antes de ampliar su SLO.
3. El `HotSnapshot` vive en memoria y se reconstruye en un arranque en frío; no
   existe snapshot serializado independiente.
4. La recuperación cubre Linux, terminación de procesos y fallos de syscalls,
   no pérdida eléctrica ni cachés de controladores de almacenamiento.
5. El smoke de fuzz pasó el harness, pero el repositorio no contiene funciones
   `Fuzz*`; no se ejecutaron mutaciones reales.
6. La reproducibilidad binaria del archivo nativo LadybugDB no está garantizada.
7. Algunos informes históricos registran árboles `-dirty` y mediciones previas
   a la corrección del par core/binding. El rerun del 2026-08-07 sobre
   `v0.13.1` pasó recuperación y MCP en el host calificado; no cubre pérdida
   eléctrica ni permite convertir la medición en un SLO de red.
8. El benchmark reducido `benchmarks/ladybug-bulk/report.md` registra
   `full_initial_scale=false` y gate `false`; no se usa como evidencia de
   producción. La evidencia válida de escala es
   `benchmarks/ladybug-bulk/full-scale/report.md` y
   `benchmarks/ladybug-large/report.md`.

## Verificación final de esta calificación

La suite de aceptación registrada en `TASKS.md` pasó:

```text
go test ./... -count=1
go test -race ./... -count=1
go vet ./...
make build
make test-ladybug
cd ts-worker && pnpm check
cd ts-worker && pnpm build
```

También pasaron los benchmarks semánticos, incrementales, de recuperación,
HotSnapshot, MCP, observabilidad y el build reproducible. Las limitaciones
anteriores forman parte de la decisión y no son warnings ocultos.

**Siguiente acción:** medir STDIO, sockets y red, y ampliar la recuperación a
fallos de alimentación o almacenamiento si el entorno de despliegue lo exige,
antes de ampliar los SLO de producción.
