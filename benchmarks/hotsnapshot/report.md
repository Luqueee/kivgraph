# Benchmark de HotSnapshot

- Fecha: `2026-08-04T22:46:51Z`
- Plataforma: `linux/amd64`, `AMD Ryzen 7 9700X 8-Core Processor`
- Go: `go1.24.4`
- Corpus: `100.000` símbolos, `1.000.000` aristas, 10 repositorios, 100 paquetes y 1.000 archivos.
- Comandos:
  - `go test -run '^$' -bench '^BenchmarkHotSnapshotBuild$' -benchtime=1x -benchmem -count=5 ./internal/hotsnapshot`
  - `go test -run '^$' -bench '^BenchmarkHotSnapshotBuildPublish$' -benchtime=1x -benchmem -count=5 ./internal/hotsnapshot`
  - `go test -run '^$' -bench '^BenchmarkHotSnapshot(FindExact|References|Depth3|Depth5|ConcurrentFind)$' -benchtime=100x -benchmem -count=5 ./internal/hotsnapshot`

## Resultados

Los valores `p95 estimate` son el máximo de cinco muestras independientes, no un
percentil de una distribución de latencias de producción.

| Operación | p95 estimate | Memoria | Allocs |
| --- | ---: | ---: | ---: |
| Build completo desde filas canónicas | 712,804 ms | 365.939.952 B/op | 408.660/op |
| Build + publish atómico | 725,769 ms | 365.934.720 B/op | 408.656/op |
| Find exacto por stable key | 0,0000306 ms | 0 B/op | 0/op |
| References outgoing | 0,0001429 ms | 128 B/op | 1/op |
| Depth 3 | 0,015158 ms | 402.864 B/op | 12/op |
| Depth 5 | 0,014242 ms | 402.864 B/op | 12/op |
| Find concurrente | 0,0003777 ms | 40 B/op | 0/op |

`VmHWM` observado en un build: `419.598.336` bytes.

## Gate

Los límites medidos del builder, publicación, find, references y depth 3 pasan
sus objetivos (`2 s`, `3 s`, `2 ms`, `5 ms` y `20 ms`). `HOT_SNAPSHOT_PASS` no se
emite: falta medir el requisito independiente de full scan LadybugDB ≤ 1 s.

El repositorio todavía no expone una API de scan completo ordenado en
`internal/storage/ladybug`; sus operaciones actuales son búsquedas puntuales y
referencias. El benchmark mide por tanto filas canónicas ya extraídas y no
puede presentar ese proxy como un scan de LadybugDB. `0401` permanece bloqueada
por `HOT_SNAPSHOT_PASS`.
