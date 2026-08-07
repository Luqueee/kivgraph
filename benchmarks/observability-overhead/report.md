# Benchmark: overhead de observabilidad

## Comando

```bash
go test ./internal/metrics -run '^$' -bench 'BenchmarkObserve(All|Query)' -benchmem -count=5
```

Commit de referencia: `4d85d52` más el benchmark de LUQUE-1405 en el working
tree. Entorno: Linux amd64, Go `1.24.4`, AMD Ryzen 7 9700X 8-Core Processor.

## Método

`BenchmarkObserveAll` ejecuta una iteración completa de observabilidad:
`ObserveQuery`, `ObserveSnapshot`, `ObserveIndex`, `ObserveWorker`,
`RecordWorkerRestart` y `ObserveLadybug`. Se comparan el registro local, el
puente con el proveedor oficial `noop` y el puente con `sdk/metric.ManualReader`.
Cada variante tiene cinco muestras y reporta allocations.

## Resultados

| Variante | Media ns/op | Rango ns/op | B/op | allocs/op |
| --- | ---: | ---: | ---: | ---: |
| Registro local | 138.66 | 138.3–138.9 | 0 | 0 |
| OpenTelemetry `noop` | 157.84 | 157.4–158.3 | 0 | 0 |
| SDK `ManualReader` | 636.68 | 630.0–641.7 | 0 | 0 |

El puente `noop` añade `13.83%` sobre la ruta base en esta carga. El proveedor
SDK explícito añade `359.22%`; ese coste corresponde a sus agregaciones y no a
la ruta por defecto.

## Gate

`OBSERVABILITY_PASS` queda justificado para el alcance de LUQUE-1405:

- las rutas base, `noop` y SDK fueron medidas con cinco muestras;
- no hubo asignaciones por observación en ninguna variante;
- la ruta por defecto sigue siendo local y sin exporter;
- el coste del proveedor SDK se presenta separado y reproducible.

`TASKS.md` y `PLAN.md` no fijan un umbral numérico de overhead; estos resultados
son evidencia del gate, no un SLO de release.

## Limitaciones

La medición excluye framing MCP, recorrido del grafo, serialización, exporter de
red y collector. `ManualReader` no representa el coste de un exporter concreto.
Es una ejecución en Linux amd64 sobre el CPU registrado; debe repetirse en el
hardware objetivo antes de usarla como decisión de release.
