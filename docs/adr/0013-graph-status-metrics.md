# ADR 0013: Exponer métricas internas mediante `graph_status`

**Estado:** aceptada

**Fecha:** 2026-08-07

## Contexto

LUQUE-1402 añadió un registro de métricas protegido por proceso, pero el
estado no era observable por los clientes MCP. `graph_status` ya es la lectura
administrativa read-only del snapshot publicado y de la salud del host; no se
debe crear otro endpoint ni hacer que la consulta vuelva a abrir LadybugDB.

## Decisión

`GraphStatus` incorpora un campo opcional `metrics` con el `metrics.Report`
consistente del registro configurado:

- el campo se omite cuando el servidor no recibe un registro;
- un snapshot ausente sigue respondiendo `status: "empty"` y puede incluir el
  reporte disponible, sin inventar identidad ni edad de snapshot;
- `Run` y `RunWithSnapshotStore` crean un registro por proceso para que el
  servidor STDIO publicado exponga consultas MCP; los hosts que coordinan
  indexación y rebuild pueden usar `RunWithMetricsAndSnapshotStore` con un
  registro compartido;
- los constructores y registradores anteriores conservan sus firmas y delegan
  en la variante con métricas, manteniendo el campo ausente cuando no se
  configura el registro;
- el reporte reutiliza los nombres y tipos del contrato interno. Las duraciones
  `time.Duration` conservan la serialización numérica estándar de Go en
  nanosegundos; no se hace una conversión silenciosa ni se pierde precisión.

## Consecuencias

El cliente puede consultar snapshot, estado del host y métricas en una sola
llamada MCP. La ruta no abre bases de datos ni retiene payloads de consultas.
El coste adicional es el snapshot consistente del registro y la serialización
de sus nueve nombres de herramienta.

La compatibilidad de servidores sin registro se conserva: no aparece un objeto
`metrics` vacío que pudiera confundirse con observabilidad activa.

## Limitaciones

El registro sigue siendo local al proceso y contiene los valores observados
hasta la llamada; la coordinación de lifecycle, indexador, rebuild y worker
debe compartir explícitamente el registro. `graph_status` no ofrece todavía
exportación Prometheus ni OpenTelemetry; LUQUE-1404 mantiene esa integración
separada.
