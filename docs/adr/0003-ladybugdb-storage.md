# ADR 0003: LadybugDB como almacenamiento canónico

- **Estado:** aceptada
- **Fecha:** 2026-08-04

## Context

Luque necesita persistir un grafo semántico cross-repository con nodos,
relaciones, hechos de evidencia y referencias no resueltas. El almacenamiento
debe ser embebido, transaccional y capaz de reconstruir snapshots sin depender
de un servidor externo.

## Decision

LadybugDB será el almacenamiento canónico embebido y persistente en disco.

El proceso principal será propietario de una única instancia `Database` en modo
`READ_WRITE`. Las conexiones se derivarán de esa instancia y las transacciones
de escritura se serializarán según las restricciones de LadybugDB. Las
consultas de lectura podrán usar conexiones concurrentes de la misma instancia.

El grafo persistente será la fuente de verdad para full rebuild, deltas,
verificación de integridad y recuperación. El layout exacto, las versiones y
los límites de capacidad se fijarán con benchmarks antes de congelar el
schema.

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
- Las escrituras se realizan dentro de transacciones y pueden requerir
  coordinación para respetar una única escritura activa.
- El HotSnapshot se construye desde el grafo persistente, pero no lo sustituye.
- Los backups, rollback e integridad forman parte del camino de recuperación.

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

Aceptada como dirección arquitectónica. La versión concreta de LadybugDB y el
schema definitivo quedan pendientes de las tareas de calificación.
