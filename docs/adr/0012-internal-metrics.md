# ADR 0012: Métricas internas del proceso

**Estado:** aceptada

**Fecha:** 2026-08-06

## Contexto

Ladygraph necesita medir consultas MCP, indexación incremental, rebuilds,
HotSnapshots, el worker TypeScript y transacciones de LadybugDB antes de
exponer ese estado mediante `graph_status`. Las métricas no deben introducir
una dependencia de red ni mezclar datos de usuario con etiquetas de alta
cardinalidad.

## Decisión

Se implementa un registro de métricas en `internal/metrics` respaldado por la
biblioteca estándar:

- los contadores, histogramas de latencia y gauges viven en un estado protegido
  por un mutex;
- las observaciones se integran mediante callbacks de consultas MCP y opciones
  explícitas en indexación, rebuild, snapshots y worker TypeScript;
- cada informe devuelve una copia consistente del estado, sin exponer el
  registro mutable;
- las consultas se identifican por el nombre de herramienta registrado y no
  almacenan payloads, argumentos ni etiquetas derivadas de entrada del usuario;
- las métricas mantienen contadores monótonos, gauges del último valor válido y
  máximos de latencia;
- no se añade todavía un exportador Prometheus, endpoint HTTP ni serialización
  pública: la exposición pertenece a LUQUE-1403.

Los nombres canónicos son `ladygraph_query_*`, `ladygraph_snapshot_*`,
`ladygraph_index_*`, `ladygraph_unresolved_references`,
`ladygraph_ts_worker_*` y `ladygraph_ladybug_*`. El informe Go usa esos mismos
nombres para que la futura capa `graph_status` no tenga que traducir el
contrato.

## Consecuencias

El camino de observación no reserva memoria por llamada y el benchmark de
`Registry.ObserveQuery` mide 0 allocaciones por operación. Las integraciones
pueden activar métricas sin cambiar los contratos de las herramientas ni del
worker. La lectura de memoria del worker usa el RSS de `/proc/<pid>/statm` en
Linux y devuelve cero en sistemas sin esa fuente portable.

La ausencia de exportador mantiene el alcance interno y evita fijar un formato
de transporte antes de `graph_status`; hasta esa tarea, el registro debe ser
conservado por el componente que vaya a servir el estado.
