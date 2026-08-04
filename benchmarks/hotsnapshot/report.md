# Benchmark de HotSnapshot

- Fecha: `2026-08-04T23:39:34Z`
- Plataforma: `linux/amd64`, `AMD Ryzen 7 9700X 8-Core Processor`
- Go: `go1.24.4`
- Corpus builder: `100.000` símbolos, `1.000.000` aristas, 10 repositorios, 100 paquetes y 1.000 archivos.
- Corpus full scan: `40` repositorios, `100.000` archivos, `100.000` símbolos y `1.000.000` aristas.
- Comandos:
  - `go test -run '^$' -bench '^BenchmarkReaderScanAll$' -benchtime=1x -benchmem -count=5 -tags ladybug ./internal/storage/ladybug`
  - `go test -run '^$' -bench '^BenchmarkHotSnapshotBuild$' -benchtime=1x -benchmem -count=5 ./internal/hotsnapshot`
  - `go test -run '^$' -bench '^BenchmarkHotSnapshotBuildPublish$' -benchtime=1x -benchmem -count=5 ./internal/hotsnapshot`
  - `go test -run '^$' -bench '^BenchmarkHotSnapshot(FindExact|References|Depth3|Depth5|ConcurrentFind)$' -benchtime=100x -benchmem -count=5 ./internal/hotsnapshot`

## Resultados

Los valores `p95 estimate` son el máximo de cinco muestras independientes, no un
percentil de una distribución de latencias de producción.

| Operación | p95 estimate | Memoria | Allocs |
| --- | ---: | ---: | ---: |
| Scan completo LadybugDB (1.200.040 filas) | 946,599 ms | 692.665.776 B/op | 294/op |
| Build completo desde filas canónicas | 712,804 ms | 365.939.952 B/op | 408.660/op |
| Build + publish atómico | 725,769 ms | 365.934.720 B/op | 408.656/op |
| Find exacto por stable key | 0,0000306 ms | 0 B/op | 0/op |
| References outgoing | 0,0001429 ms | 128 B/op | 1/op |
| Depth 3 | 0,015158 ms | 402.864 B/op | 12/op |
| Depth 5 | 0,014242 ms | 402.864 B/op | 12/op |
| Find concurrente | 0,0003777 ms | 40 B/op | 0/op |

`VmHWM` observado en un build: `419.598.336` bytes.

## Gate

El scan completo ordenado usa Arrow C Data Interface, cuatro conexiones de
lectura y ordenación determinista en Go. El máximo de cinco muestras fue
`946,599 ms`, por debajo del límite de `1 s` por `53,401 ms`. El scan no usa
`ORDER BY` en LadybugDB: copia los buffers columnar y ordena solo si la salida
física no está ya ordenada.

Los límites medidos del scan, builder, publicación, find, references y depth 3
pasan sus objetivos (`1 s`, `2 s`, `3 s`, `2 ms`, `5 ms` y `20 ms`).
Se emite `HOT_SNAPSHOT_PASS`, con la limitación de que la medición debe repetirse
en el hardware objetivo.

El scan materializa 1.200.040 filas; sus `692.665.776 B/op` no constituyen una
medición RSS de proceso persistente. La implementación requiere el ABI CGO de
LadybugDB calificado en Linux amd64.
