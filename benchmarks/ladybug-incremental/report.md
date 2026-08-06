# Benchmark de indexación incremental

- Commit medido: `3fbd7a4347cbabb9add1332af0c5047bcd7e5758`
- Fecha: `2026-08-06T16:27:55Z`
- Plataforma: `linux/amd64`, `go1.24.4`
- Muestras por escenario: `5`
- Corpus: `1` repositorio, `1` paquete, `1000` archivos, `10000` símbolos, `10000` evidencias, `21001` aristas

## Resultados

Los tiempos son la llamada completa a `indexer.Update`: cálculo del delta, decisión de ruta, mutación o republicación, digest, reconstrucción del HotSnapshot cuando corresponde y publicación atómica.

| Escenario | Ruta | p50 ms | p95 ms | mínimo ms | máximo ms | setup base p50 ms | integridad |
| --- | --- | ---: | ---: | ---: | ---: | ---: | --- |
| `simple_file` | `DELTA` | 560.9 | 571.6 | 558.4 | 571.6 | 885.6 | `true`, 0 violaciones |
| `imports_exports` | `DELTA` | 592.7 | 617.8 | 576.1 | 617.8 | 867.3 | `true`, 0 violaciones |
| `manifest` | `REPUBLISH` | 830.2 | 845.3 | 826.0 | 845.3 | 880.4 | `true`, 0 violaciones |

## Gate `INCREMENTAL_INDEXING_PASS`

- archivo simple p95: `571.6 ms` (límite `≤ 750 ms`) — `true`
- imports/exports p95: `617.8 ms` (límite `≤ 2 s`) — `true`
- manifest p95: `845.3 ms` (límite `≤ 5 s`) — `true`
- ghost edges: `0` — `true`
- Resultado: `true`

## Limitaciones

- The corpus is deterministic and generated in memory; source parsing and language-engine normalization are not part of this executable because indexer.Update accepts canonical facts after those stages.
- Each sample builds an isolated baseline generation before timing one complete Update call. Baseline construction is reported separately and is excluded from the update latency gate.
- DELTA samples include canonical mutation, row-count digest refresh, complete HotSnapshot rebuild and atomic in-memory publication. The manifest sample measures the forced full REPUBLISH path.
- Results describe the pinned LadybugDB build, Linux amd64 and the configured corpus size; they are not a guarantee for another filesystem or repository shape.
