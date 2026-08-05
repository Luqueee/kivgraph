# Calificación de LadybugDB

- **Fecha:** 2026-08-04
- **Decisión:** `ACCEPT_LADYBUGDB_WITH_LIMITS`
- **Gate:** `LADYBUG_STORAGE_PASS` **emitido**
- **Estado para la fase 3:** desbloqueada

## Decisión

LadybugDB queda aceptada como motor candidato para el almacenamiento canónico
de Luque y se autoriza la construcción de HotSnapshot. La carga masiva, las
mutaciones transaccionales, la integridad, la recuperación ante terminación de
proceso, la publicación ante agotamiento de disco y el rendimiento de deltas
cumplen el contrato medido.

El plan exige `LADYBUG_SCHEMA_PASS`, `LADYBUG_BULK_LOAD_PASS`,
`LADYBUG_INCREMENTAL_PASS`, `LADYBUG_RECOVERY_PASS` y
`LADYBUG_DELTA_PERFORMANCE_PASS` antes de derivar `LADYBUG_STORAGE_PASS`.
Todos están en `PASS`; se deriva el gate global.

## Configuración calificada

| Elemento | Valor |
| --- | --- |
| LadybugDB core | `v0.13.1` (corregido el 2026-08-05; ver la nota de abajo) |
| Binding Go | `github.com/LadybugDB/go-ladybug v0.13.1`, commit `14a9f84900d0a8295c59419d91461c5430c692b5` |
| Toolchain medido | Go `1.24.4`, CGO habilitado |
| Plataforma calificada | `linux/amd64` |
| Corpus | semilla 42; 40 repositorios, 100.000 archivos, 100.000 símbolos y 1.000.000 de aristas |
| Esquema sintético | `001` |

Las versiones, assets y checksums están fijados en [`docs/dependencies/ladybugdb.md`](../dependencies/ladybugdb.md). Esta decisión no amplía la calificación a otras plataformas.

### Corrección del 2026-08-05

Esta decisión registraba el core `v0.19.0` junto al binding `v0.13.1`. Ese par
**no es compatible a nivel de ABI**: el core `v0.19.0` amplía
`lbug_system_config` con cuatro campos y el binding la devuelve por valor, de
modo que la primera llamada C provoca `SIGSEGV`. El par correcto es core y
binding en `v0.13.1`, con el que la suite `-tags ladybug` completa pasa.

La calificación de rendimiento y recuperación se midió antes de esa
corrección; sus cifras quedan como registro histórico y deben repetirse sobre
el par fijado cuando la fase de rendimiento vuelva a medirse. Los gates de
almacenamiento **no se rebajan aquí**: lo que cambia es la biblioteca con la
que se reproducen, y `scripts/fetch-ladybug.sh` la deja disponible.

## Evaluación de gates

| Gate | Estado | Evidencia |
| --- | --- | --- |
| `LADYBUG_SCHEMA_PASS` | `PASS` | El doctor validó las siete tablas, claves, tipos y extremos de relaciones; los conteos e invariantes dieron cero violaciones. |
| `LADYBUG_BULK_LOAD_PASS` | `PASS` | `COPY` cargó y verificó la escala completa con RSS inferior a 2 GiB. |
| `LADYBUG_INCREMENTAL_PASS` | `PASS` | Altas, bajas, cambios, sustitución de relaciones, duplicados, atomicidad y rollback pasaron sobre una copia del corpus completo. |
| `LADYBUG_RECOVERY_PASS` | `PASS` | Ocho casos pasan; `CURRENT`, checksum y reapertura permanecen intactos ante fallos de la candidata y de publicación. |
| `LADYBUG_DELTA_PERFORMANCE_PASS` | `PASS` | p95 `Apply`: 115,7 ms para 1–10 relaciones y 271,9 ms para 1.000; staging transaccional conserva el rechazo exacto de duplicados. |
| `LADYBUG_STORAGE_PASS` | `PASS` | Todos los gates de almacenamiento están aprobados. |

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
completo. La API de lectura completa usa el Arrow C Data Interface de LadybugDB,
cuatro conexiones de solo lectura y ordenación determinista en Go; omite
`ORDER BY` nativo para evitar materializar un ordenamiento columnar costoso.
Sobre el corpus de 1.200.040 filas, el benchmark p95 estimate fue `946,599 ms`,
por debajo del límite de `1 s`, con `692.665.776 B/op` de materialización. El
margen de `53,401 ms` exige repetir la medición en el hardware objetivo.

### Actualización incremental

LUQUE-0214 perfiló `BEGIN`, lookup de extremos, borrado, creación, integridad,
`COMMIT` y cierre sobre cinco copias del corpus completo. También registró
throughput, RSS y allocations por batch. El gate mide `Writer.Apply`; `Close`
se conserva como fase observada, no como latencia de una mutación ya que el
writer persistente no se cierra tras cada delta.

| Estrategia | Relaciones | p95 Apply ms | Resultado |
| --- | ---: | ---: | --- |
| prepared individual | 10 | 115,7 | cumple el máximo contractual de 150 ms; no alcanza el objetivo aspiracional de 50 ms |
| prepared batch | 1.000 | 19.211,5 | rechazada: el binding `UNWIND` concentra el coste en creación |
| staging COPY | 1.000 | 271,9 | elegida: validación exacta y carga masiva dentro de una transacción |
| COPY de 10 deltas agregados | 10.000 | 771,3 | excluye la espera de cola y no es el camino online |

El writer valida 1–10 relaciones con una consulta exacta única. Para 11 o más,
valida primero todos los endpoints, importa pares en una tabla relacional de
staging con `COPY`, cruza staging con las relaciones canónicas para detectar
duplicados exactos y limpia staging antes de importar las relaciones canónicas.
Todo ocurre en la transacción del writer: cualquier fallo revierte ambas tablas.

La corrección de duplicados, ausencia de aristas fantasma, atomicidad y rollback
siguen cubiertas por la suite del writer. Los límites contractuales pasan, por
lo que `LADYBUG_DELTA_PERFORMANCE_PASS` y `LADYBUG_STORAGE_PASS` se emiten y la
fase 3 queda desbloqueada.

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

LUQUE-0214 localizó el coste, demostró batching real y emitió
`LADYBUG_DELTA_PERFORMANCE_PASS`: el staging transaccional obtuvo 271,9 ms p95
para 1.000 relaciones y mantiene la semántica exacta de duplicados.

Con este gate y `LADYBUG_RECOVERY_PASS` se deriva `LADYBUG_STORAGE_PASS` y se
desbloquea LUQUE-0301.

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
