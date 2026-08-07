# Benchmark de iteración del HotSnapshot

- **Commit medido:** `7f74bdc7b5f39915f7e46cf3707e10a62d1cac20`
- **Fecha:** `2026-08-07T15:24:13Z`
- **Comando:**
  `go test ./internal/hotsnapshot -run '^$' -bench 'BenchmarkHotSnapshot(OutgoingAllSymbols|VisitCSRBySymbol)$' -benchmem -count=1`
- **Entorno:** Linux `6.12.94+deb13-amd64`, Go `1.24.4`, AMD Ryzen 7 9700X, 23 GiB RAM.
- **Dataset:** `hotBenchmarkRowsFixture`, 10 repositories, 100 packages, 1.000 files, 100.000 símbolos y 1.000.000 aristas. La fixture es determinista y no usa PRNG; no hay seed aplicable.

## Resultado

| Recorrido | ns/op | B/op | allocs/op | símbolos/op |
| --- | ---: | ---: | ---: | ---: |
| `Outgoing` por símbolo, anterior | 2.606.721 | 12.800.000 | 100.000 | 100.000 |
| `CSRRange` + `VisitEdges`, nuevo | 2.329.939 | 0 | 0 | 100.000 |

El recorrido nuevo conserva el acceso a los 1.000.000 de edges sin crear una slice por símbolo ni asignaciones por operación. La comparación mide la misma fixture y el mismo número de símbolos; los tiempos son una sola ejecución y pueden variar con la carga del host.

## Interpretación

- El accessor anterior materializa una copia de cada rango `Outgoing`.
- Los nuevos `Visit*` entregan registros por valor mediante callbacks y mantienen privadas las slices internas.
- `VisitSymbols` y `VisitEdges` comprueban `context.Context` antes de cada callback.
- Este resultado califica la iteración del `HotSnapshot`; no emite `WEB_VIEWER_PERFORMANCE_PASS`, que requiere el benchmark end-to-end del visor con Chromium.
