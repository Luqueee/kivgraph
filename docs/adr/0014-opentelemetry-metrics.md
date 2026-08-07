# ADR 0014: Métricas OpenTelemetry opcionales

**Estado:** aceptada  
**Fecha:** 2026-08-07

## Contexto

Ladygraph ya mantiene un `metrics.Registry` local para consultas, snapshots,
indexación, el worker TypeScript y LadybugDB. El registro no debe depender de un
collector ni forzar una exportación en procesos embebidos, pruebas o el servidor
MCP por STDIO.

El plan del proyecto habilita métricas y mantiene trazas deshabilitadas. La
integración necesaria es, por tanto, una proyección opcional de las
observaciones existentes, no una segunda fuente de hechos ni un rediseño del
registro local.

## Decisión

`internal/metrics` expone `NewRegistryWithOpenTelemetry` y
`NewOpenTelemetry`. El llamador puede suministrar un
`go.opentelemetry.io/otel/metric.MeterProvider`; el llamador conserva la
propiedad y el cierre del proveedor.

Si el proveedor es `nil`, la integración usa el proveedor `noop` oficial de
OpenTelemetry. Ladygraph no crea exporters, readers periódicos, collectors,
conexiones de red ni goroutines de telemetría. Un exporter sólo existe cuando
el proceso consumidor construye y suministra explícitamente un proveedor.

El puente reutiliza las mismas observaciones que `metrics.Registry`:

- contadores e histograma de consultas con el atributo acotado `tool.name`;
- gauges del snapshot, indexación y worker;
- histograma de transacciones y gauge de tamaño de LadybugDB.

Los nombres de instrumentos son las constantes históricas `ladygraph_*` del
registro. Las duraciones se publican en milisegundos y los tamaños en bytes.
Los nombres de herramientas desconocidos se agrupan como `other` para evitar
cardinalidad no acotada.

La construcción normal con `NewRegistry` permanece sin puente y sin llamadas a
OpenTelemetry. La API existente de observación y `Report` no cambia.

## Consecuencias

- Los consumidores pueden conectar un exporter elegido por ellos sin cambiar
  handlers MCP ni observadores de indexación.
- El comportamiento por defecto no produce telemetría externa y no requiere un
  collector.
- Un proveedor configurado puede añadir coste a cada observación; el paquete
  conserva benchmarks comparables para el camino base, el proveedor `noop` y el
  SDK de OpenTelemetry.
- La integración cubre métricas. Las trazas continúan deshabilitadas, de
  acuerdo con el plan vigente.
