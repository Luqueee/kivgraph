# ADR 0003: LadybugDB como almacenamiento canónico

- **Estado:** aceptada con límites; gate de almacenamiento bloqueado
- **Fecha:** 2026-08-04

## Context

Luque necesita persistir un grafo semántico cross-repository con nodos,
relaciones, hechos de evidencia y referencias no resueltas. El almacenamiento
debe ser embebido, transaccional y capaz de reconstruir snapshots sin depender
de un servidor externo.

## Decision

LadybugDB será el almacenamiento canónico embebido y persistente en disco.

Cada versión durable del grafo vivirá en una generación inmutable. `CURRENT`
seleccionará la generación activa y solo una candidata privada se abrirá en
modo `READ_WRITE`. Las conexiones de una misma base se derivarán de su única
instancia `Database` y las escrituras se serializarán según las restricciones
de LadybugDB.

La candidata se cerrará, sincronizará, reabrirá y validará antes de publicar
`CURRENT` mediante rename atómico y fsync del directorio. La generación
anterior permanecerá disponible para restauración.

El grafo persistente seguirá siendo la fuente durable para full rebuild,
integridad, auditoría y recuperación. En el camino incremental se medirá si
LadybugDB y HotSnapshot deben construirse en paralelo desde los mismos facts
normalizados para evitar una lectura completa innecesaria.

## Alternatives

- **SQLite:** queda descartado porque el modelo requerido es un grafo y el plan
  fija `sqlite.enabled: false`.
- **Servidor de base de datos externo:** añadiría despliegue, red y dependencia
  operativa innecesarios para un servidor local autónomo.
- **Archivos propios o JSON:** simplificarían el arranque, pero no proporcionan
  transacciones, recuperación WAL ni consultas de relaciones con garantías
  suficientes.
- **Base en memoria como fuente canónica:** reduciría latencia, pero perdería
  persistencia y obligaría a reconstruir todo tras cada reinicio.

## Consequences

- Luque debe gestionar explícitamente el ciclo de vida de la base y sus
  conexiones.
- El schema y las versiones de LadybugDB pasan a ser dependencias de
  distribución que deben fijarse y auditarse.
- Las transacciones de escritura solo mutan una generación candidata y se
  coordinan para respetar una única escritura activa por base.
- El HotSnapshot atiende el fast path; debe poder reconstruirse desde el grafo
  persistente durante arranque y recuperación.
- La publicación requiere fsync de la candidata, rename atómico de `CURRENT`,
  fsync del directorio, una generación anterior y una reserva de emergencia.
- Los backups, rollback e integridad forman parte del camino de recuperación.
- `LADYBUG_RECOVERY_PASS` está emitido. El gate de almacenamiento permanece
  bloqueado únicamente hasta medir y aceptar el rendimiento de deltas.

## Risks

- Cambios de API o compatibilidad nativa de LadybugDB pueden bloquear builds en
  una plataforma concreta.
- Un schema prematuro puede obligar a migraciones costosas; por eso se difiere
  el congelado hasta LUQUE-0201 y los benchmarks posteriores.
- Corrupción de la base o recuperación incompleta puede dejar el índice sin una
  fuente válida; cada publicación deberá tener una ruta de rollback.
- Compartir una instancia entre componentes sin respetar su modelo de
  concurrencia puede producir errores de transacción.

## Status

Aceptada con límites tras LUQUE-0213. Carga, integridad, mutaciones,
recuperación ante terminación de proceso y publicación ante `ENOSPC` pasan.
La base activa ya no se muta: `internal/storage/generation` construye y valida
una candidata, publica `CURRENT` de forma durable y conserva una generación
restaurable.

La decisión completa y la evidencia están en
[`docs/decisions/ladybugdb-qualification.md`](../decisions/ladybugdb-qualification.md).
`LADYBUG_RECOVERY_PASS` está emitido. `LADYBUG_STORAGE_PASS` sigue bloqueado
hasta que LUQUE-0214 emita `LADYBUG_DELTA_PERFORMANCE_PASS`.
