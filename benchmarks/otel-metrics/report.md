# Benchmark: métricas OpenTelemetry opcionales

## Comando

```bash
go test ./internal/metrics -run '^$' -bench 'BenchmarkObserveQuery' -benchmem -count=3
```

Commit de referencia: `f3984c4` más los cambios no confirmados de LUQUE-1404.
Entorno: Linux amd64, Go `1.24.4`, AMD Ryzen 7 9700X 8-Core Processor.

## Carga

Cada variante ejecuta `ObserveQuery` con `find_symbol`, `2us` de duración y
cinco resultados. Se comparan el registro local, el puente con el proveedor
oficial `noop` y el puente con `sdk/metric.ManualReader`.

| Variante | ns/op | B/op | allocs/op |
| --- | ---: | ---: | ---: |
| Registro local | 30.46–30.56 | 0 | 0 |
| OpenTelemetry `noop` | 33.14–33.25 | 0 | 0 |
| SDK `ManualReader` | 125.7–126.4 | 0 | 0 |

## Interpretación

El camino base no cambia: `NewRegistry` no crea instrumentos ni llama al
puente. El constructor opcional con `noop` añade aproximadamente 2.7 ns/op en
esta carga y no añade asignaciones. Un proveedor SDK configurado explícitamente
incurre en el coste de sus agregaciones, también sin asignaciones por
observación en esta carga.

## Limitaciones

El benchmark mide el handler de métricas y excluye framing MCP, exportación de
red y collector. `ManualReader` no representa el coste de un exporter concreto.
Es una medición puntual en el CPU registrado, no una afirmación SLO
multi-host.
