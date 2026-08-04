# Calificación de LadybugDB

- **Fecha:** 2026-08-04
- **Decisión:** `ACCEPT_LADYBUGDB_WITH_LIMITS`
- **Gate:** `LADYBUG_STORAGE_PASS` **no emitido**
- **Estado para la fase 3:** bloqueada

## Decisión

LadybugDB queda aceptada como motor candidato para el almacenamiento canónico de Luque, pero no queda autorizada todavía para continuar a la construcción de HotSnapshot. La carga masiva, las mutaciones transaccionales, la integridad y la recuperación ante terminación de proceso cumplen el contrato medido. La recuperación ante agotamiento de disco no lo cumple.

El plan exige simultáneamente `LADYBUG_SCHEMA_PASS`, `LADYBUG_BULK_LOAD_PASS`, `LADYBUG_INCREMENTAL_PASS` y `LADYBUG_RECOVERY_PASS`. El último permanece en `FAIL`; por tanto, emitir `LADYBUG_STORAGE_PASS` ocultaría un gate fallido.

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
| `LADYBUG_RECOVERY_PASS` | `FAIL` | Seis casos pasaron; `simulated_disk_full` dejó la copia sin posibilidad de reapertura. |
| `LADYBUG_STORAGE_PASS` | **BLOQUEADO** | No se puede derivar mientras `LADYBUG_RECOVERY_PASS` siga fallando. |

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

### Actualización incremental

| Probe | Resultado |
| --- | ---: |
| añadir 1 símbolo | 14,040 ms |
| añadir 1.000 símbolos | 4.748,766 ms |
| añadir 3 aristas | 878,656 ms |
| borrar 1 arista | 7,748 ms |
| cambiar propiedades | 7,181 ms |
| sustituir relaciones salientes | 822,885 ms |
| borrar 1 símbolo | 113,111 ms |
| rollback tras fallo tardío | 403,419 ms |

Se verificaron rechazo de duplicados, ausencia de aristas fantasma, atomicidad y rollback. La medición no incluye construir ni publicar HotSnapshot.

### Recuperación

Pasaron:

- `SIGKILL` durante una inserción;
- `SIGKILL` inmediatamente antes de `COMMIT`;
- `SIGKILL` durante `COPY`;
- reapertura y nueva escritura durable después de la caída;
- fichero truncado como error controlado;
- directorio sin permisos como error controlado.

Falló `simulated_disk_full`. `Writer.Apply` devolvió éxito; el primer `ENOSPC` interceptado apareció durante el cierre y la API nativa de cierre no pudo propagarlo. La copia resultante no volvió a abrirse. `luque doctor storage` detecta una base ya dañada, pero no previene el daño ni recupera la base activa.

## Límites obligatorios

Hasta cerrar el gate:

1. No se permiten mutaciones incrementales in-place sobre la única copia canónica.
2. Ningún resultado de `Writer.Apply` se considera durable por sí solo ante `ENOSPC`.
3. La base viva solo puede abrirla un proceso Luque; un lock externo es fallo operativo.
4. El despliegue calificado requiere Linux amd64, CGO, `liblbug` fijada y verificación de checksum.
5. Los backups y una restauración verificada siguen siendo obligatorios; el doctor no los sustituye.
6. Las consultas MCP no se servirán directamente desde LadybugDB.

## Condiciones para emitir `LADYBUG_STORAGE_PASS`

LUQUE-0213 debe:

1. aplicar cambios sobre `graph.next.lbdb` o una copia privada, nunca sobre la única base activa;
2. cerrar, reabrir y ejecutar integridad/doctor sobre la candidata antes de publicarla;
3. publicar mediante reemplazo atómico y conservar una copia anterior recuperable;
4. definir reserva y umbral de espacio, sin tratarlos como sustituto de la publicación atómica;
5. inyectar `ENOSPC` durante aplicación, cierre y publicación;
6. demostrar que cada fallo conserva intactos el checksum y la reapertura de la base activa;
7. demostrar el camino exitoso de publicación y restauración;
8. repetir la suite de recuperación con `all_passed: true`.

Solo entonces se podrá emitir `LADYBUG_RECOVERY_PASS`, derivar `LADYBUG_STORAGE_PASS` y desbloquear LUQUE-0301.

## Evidencia

- [`benchmarks/ladybug-bulk/full-scale/results.json`](../../benchmarks/ladybug-bulk/full-scale/results.json)
- [`benchmarks/ladybug-bulk/full-scale/report.md`](../../benchmarks/ladybug-bulk/full-scale/report.md)
- [`benchmarks/ladybug-queries/results.json`](../../benchmarks/ladybug-queries/results.json)
- [`benchmarks/ladybug-queries/report.md`](../../benchmarks/ladybug-queries/report.md)
- [`benchmarks/ladybug-incremental/results.json`](../../benchmarks/ladybug-incremental/results.json)
- [`benchmarks/ladybug-incremental/report.md`](../../benchmarks/ladybug-incremental/report.md)
- [`benchmarks/ladybug-recovery/results.json`](../../benchmarks/ladybug-recovery/results.json)
- [`docs/testing/ladybug-recovery.md`](../testing/ladybug-recovery.md)
- [`docs/storage/synthetic-schema.md`](../storage/synthetic-schema.md)
- [`docs/adr/0003-ladybugdb-storage.md`](../adr/0003-ladybugdb-storage.md)
