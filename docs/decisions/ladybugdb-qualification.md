# Calificación de LadybugDB

- **Fecha:** 2026-08-04
- **Decisión:** `ACCEPT_LADYBUGDB_WITH_LIMITS`
- **Gate:** `LADYBUG_STORAGE_PASS` **no emitido**
- **Estado para la fase 3:** bloqueada

## Decisión

LadybugDB queda aceptada como motor candidato para el almacenamiento canónico
de Luque, pero todavía no se autoriza la construcción de HotSnapshot. La carga
masiva, las mutaciones transaccionales, la integridad, la recuperación ante
terminación de proceso y la publicación ante agotamiento de disco cumplen el
contrato medido. El perfil de deltas falló el límite de 1.000 relaciones.

El plan exige `LADYBUG_SCHEMA_PASS`, `LADYBUG_BULK_LOAD_PASS`,
`LADYBUG_INCREMENTAL_PASS`, `LADYBUG_RECOVERY_PASS` y
`LADYBUG_DELTA_PERFORMANCE_PASS` antes de derivar `LADYBUG_STORAGE_PASS`.
Recuperación está en `PASS`, pero el perfil de LUQUE-0214 deja el gate de
rendimiento en `FAIL`; emitir el gate global ocultaría ese bloqueo.

## Configuración calificada

| Elemento | Valor |
| --- | --- |
| LadybugDB core | `v0.19.0`, commit `c934f673b6b1c5b680bdae3295cbd909b5855cef` |
| Binding Go | `github.com/LadybugDB/go-ladybug v0.13.1`, commit `14a9f84900d0a8295c59419d91461c5430c692b5` |
| Toolchain medido | Go `1.24.4`, CGO habilitado |
| Plataforma calificada | `linux/amd64` |
| Corpus | semilla 42; 40 repositorios, 100.000 archivos, 100.000 símbolos y 1.000.000 de aristas |
| Esquema sintético | `001` |

Las versiones, assets y checksums están fijados en [`docs/dependencies/ladybugdb.md`](../dependencies/ladybugdb.md). Esta decisión no amplía la calificación a otras plataformas.

## Evaluación de gates

| Gate | Estado | Evidencia |
| --- | --- | --- |
| `LADYBUG_SCHEMA_PASS` | `PASS` | El doctor validó las siete tablas, claves, tipos y extremos de relaciones; los conteos e invariantes dieron cero violaciones. |
| `LADYBUG_BULK_LOAD_PASS` | `PASS` | `COPY` cargó y verificó la escala completa con RSS inferior a 2 GiB. |
| `LADYBUG_INCREMENTAL_PASS` | `PASS` | Altas, bajas, cambios, sustitución de relaciones, duplicados, atomicidad y rollback pasaron sobre una copia del corpus completo. |
| `LADYBUG_RECOVERY_PASS` | `PASS` | Ocho casos pasan; `CURRENT`, checksum y reapertura permanecen intactos ante fallos de la candidata y de publicación. |
| `LADYBUG_DELTA_PERFORMANCE_PASS` | **FAIL** | 1–10 relaciones cumplen el p95 tolerable (123,5 ms); 1.000 relaciones con el camino seguro tardan 19.249,3 ms p95 frente al límite de 500 ms. |
| `LADYBUG_STORAGE_PASS` | **BLOQUEADO** | Recuperación pasa; rendimiento de deltas no. |

## Resultados reproducidos

La calificación se reprodujo sobre `e902dd0d56563cd3b4d71c2ac19ca28caf955824`. Los generadores que se ejecutaron después del primer benchmark registran el sufijo `-dirty` porque los artefactos del benchmark anterior ya habían cambiado en el árbol; el código ejecutado siguió correspondiendo a ese commit.

### Carga y espacio

`COPY` procesó 1.200.040 registros en 1.793,5 ms de carga:

- 669.100,3 registros/s durante `COPY`;
- 391.195,8 registros/s incluyendo exportación CSV;
- 542.978.048 bytes de pico RSS;
- 43.290.624 bytes de base resultante;
- conteos almacenados verificados.

La estrategia recomendada para full rebuild es `COPY`. El batch de 10.000 queda como referencia incremental, no como alternativa de carga completa: en la comparación previa consumió 1.270.525.952 bytes de pico RSS y fue unas 179 veces más lento que `COPY` sobre el corpus comparable.

### Consultas directas

Las golden probes pasaron sin errores. Los p95 medidos fueron:

| Operación | p95 |
| --- | ---: |
| lookup por stable key | 13,591 ms |
| 100 referencias entrantes | 158,276 ms |
| 100 referencias salientes | 158,395 ms |
| recorrido depth 3 | 46,671 ms |
| recorrido depth 5 | 46,802 ms |
| shortest path depth 5 | 85,385 ms |
| entrantes agrupadas por repositorio | 31,351 ms |

Estas latencias no satisfacen los SLO del MCP y confirman la separación prevista: LadybugDB conserva la verdad persistente y HotSnapshot deberá atender las consultas online.

La latencia puntual no permite inferir el rendimiento de un scan columnar
completo. Antes de validar a LadybugDB como fuente de construcción de
HotSnapshot se medirán por separado la lectura de todos los símbolos, la
lectura de todas las aristas, la normalización de IDs, ambos CSR, los índices y
la validación. También se comparará con construir LadybugDB y HotSnapshot desde
los mismos facts normalizados.

### Actualización incremental

LUQUE-0214 perfiló `BEGIN`, lookup de extremos, borrado, creación, integridad,
`COMMIT` y cierre sobre cinco copias del corpus completo. También registró
throughput, RSS y allocations por batch.

| Estrategia | Relaciones | p95 ms | Resultado |
| --- | ---: | ---: | --- |
| prepared individual | 10 | 123,5 | segura para deltas pequeños; cumple el máximo de 150 ms, no el objetivo de 50 ms |
| prepared batch | 1.000 | 19.249,3 | rechazada: `UNWIND` ligado desde Go concentra el coste en creación |
| staging COPY | 1.000 | 177,9 | rápida, pero no preserva por sí sola la detección atómica de duplicados |
| COPY de 10 deltas agregados | 10.000 | 475,0 | excluye la espera de cola y alcanzó 1.876.275.200 bytes RSS |

El writer ahora ejecuta referencias individuales hasta 10 y agrupa las
eliminaciones por tipo de relación. Para 11 o más mantiene un `UNWIND` por
tipo: no emite una llamada nativa por fact, pero no alcanza el límite para
1.000 relaciones. `COPY` no se habilita en un delta genérico porque la tabla
admite multiplicidad; publicar filas sin una comprobación exacta previa puede
introducir duplicados.

La corrección de duplicados, ausencia de aristas fantasma, atomicidad y rollback
siguen cubiertas por la suite del writer. El benchmark de perfil deja
`LADYBUG_DELTA_PERFORMANCE_PASS` en `FAIL`; por tanto
`LADYBUG_STORAGE_PASS` no se deriva y la fase 3 permanece bloqueada.

La siguiente remediación debe construir un bulk path sobre la candidata privada
que conserve una prevalidación exacta de duplicados antes de usar `COPY`, o
corregir el coste de serialización de `UNWIND` en el binding/engine. No se
acepta retrasar el coste tras una ventana de 150–500 ms: la espera debe contarse
en la latencia end-to-end.

### Recuperación

Pasaron:

- `SIGKILL` durante una inserción;
- `SIGKILL` inmediatamente antes de `COMMIT`;
- `SIGKILL` durante `COPY`;
- reapertura y nueva escritura durable después de la caída;
- fichero truncado como error controlado;
- directorio sin permisos como error controlado;
- `ENOSPC` tardío durante el cierre de una candidata privada;
- `ENOSPC` durante rename de generación, escritura/fsync/rename de `CURRENT` y
  fsync del directorio de estado.

La publicación usa `state/generations/<id>.tmp/`, valida la candidata cerrada,
la renombra a `<id>/` y solo después cambia `CURRENT`. Ante fallo libera la
reserva para abortar y registrar, elimina la candidata y conserva la generación
activa. El benchmark verificó además una publicación posterior y la restauración
de la generación anterior.

## Límites obligatorios

1. No se permiten mutaciones incrementales in-place sobre la generación activa.
2. Ningún resultado de `Writer.Apply` se considera durable por sí solo ante
   `ENOSPC`; la durabilidad empieza al publicar `CURRENT`.
3. La base viva solo puede abrirla un proceso Luque; un lock externo es fallo operativo.
4. El despliegue calificado requiere Linux amd64, CGO, `liblbug` fijada y verificación de checksum.
5. Los backups y una restauración verificada siguen siendo obligatorios; el doctor no los sustituye.
6. Las consultas MCP no se servirán directamente desde LadybugDB.

## Resultado de LUQUE-0213

LUQUE-0213 cumplió las doce condiciones de recuperación:

1. almacena bases en `state/generations/<id>/` y candidatas en `.tmp`;
2. usa `CURRENT` como autoridad única;
3. aplica el mayor entre `2 × base + snapshot + 1 GiB` y el 15 % del filesystem;
4. preasigna una reserva configurable de al menos 512 MiB;
5. exige un validador sobre la candidata cerrada y sincronizada;
6. sincroniza ficheros y directorios candidatos;
7. publica la generación antes de cambiar el manifiesto;
8. sincroniza `CURRENT.next`, lo renombra y sincroniza el directorio padre;
9. conserva y restaura la generación anterior;
10. inyecta `ENOSPC` durante mutación y publicación;
11. conserva `CURRENT`, checksum, reapertura y snapshot validado ante cada fallo;
12. obtiene `all_passed: true` en la suite de recuperación.

LUQUE-0214 localizó el coste, demostró batching real y registró el bloqueo
explícito de rendimiento. No emitió `LADYBUG_DELTA_PERFORMANCE_PASS`: el
camino seguro de 1.000 relaciones alcanzó 19.249,3 ms p95.

Una remediación que conserve semántica exacta de duplicados debe aprobar el
gate de deltas antes de derivar `LADYBUG_STORAGE_PASS` y desbloquear
LUQUE-0301.

## Evidencia

- [`benchmarks/ladybug-bulk/full-scale/results.json`](../../benchmarks/ladybug-bulk/full-scale/results.json)
- [`benchmarks/ladybug-bulk/full-scale/report.md`](../../benchmarks/ladybug-bulk/full-scale/report.md)
- [`benchmarks/ladybug-queries/results.json`](../../benchmarks/ladybug-queries/results.json)
- [`benchmarks/ladybug-queries/report.md`](../../benchmarks/ladybug-queries/report.md)
- [`benchmarks/ladybug-incremental/results.json`](../../benchmarks/ladybug-incremental/results.json)
- [`benchmarks/ladybug-incremental/report.md`](../../benchmarks/ladybug-incremental/report.md)
- [`benchmarks/ladybug-delta-profile/results.json`](../../benchmarks/ladybug-delta-profile/results.json)
- [`benchmarks/ladybug-delta-profile/report.md`](../../benchmarks/ladybug-delta-profile/report.md)
- [`benchmarks/ladybug-recovery/results.json`](../../benchmarks/ladybug-recovery/results.json)
- [`docs/testing/ladybug-recovery.md`](../testing/ladybug-recovery.md)
- [`docs/storage/synthetic-schema.md`](../storage/synthetic-schema.md)
- [`docs/adr/0003-ladybugdb-storage.md`](../adr/0003-ladybugdb-storage.md)
