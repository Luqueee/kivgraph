# Perfil de deltas LadybugDB

- Commit medido: `c943b7249ebcac000a918b2de1dac4631136759e-dirty`
- Fecha: `2026-08-04T21:34:51Z`
- Plataforma: `linux/amd64`, `go1.24.4`
- Base: `43290624` bytes; muestras por caso: `5`

## Resultados

| Estrategia | Relaciones | Deltas agregados | p50 ms | p95 ms | Relaciones/s | RSS pico bytes | Alloc/batch bytes |
| --- | ---: | ---: | ---: | ---: | ---: | ---: | ---: |
| `prepared_individual` | 1 | 1 | 82.0 | 86.5 | 12.2 | 190779392 | 4024 |
| `prepared_individual` | 10 | 1 | 123.4 | 123.5 | 81.1 | 190730240 | 7048 |
| `prepared_individual` | 1000 | 1 | — | — | — | — | — |
| `prepared_batch` | 1 | 1 | 499.0 | 502.4 | 2.0 | 210423808 | 5048 |
| `prepared_batch` | 10 | 1 | 645.3 | 654.9 | 15.5 | 218136576 | 13088 |
| `prepared_batch` | 1000 | 1 | 19167.7 | 19249.3 | 52.2 | 407334912 | 979200 |
| `staging_copy` | 1 | 1 | 142.7 | 144.3 | 7.0 | 408879104 | 4320 |
| `staging_copy` | 10 | 1 | 144.4 | 149.7 | 69.2 | 411299840 | 5624 |
| `staging_copy` | 1000 | 1 | 171.6 | 177.9 | 5827.6 | 446902272 | 179160 |
| `aggregate_10_deltas` | 1 | 10 | 143.9 | 144.8 | 69.5 | 395309056 | 6040 |
| `aggregate_10_deltas` | 10 | 10 | 148.7 | 150.6 | 672.6 | 360783872 | 19480 |
| `aggregate_10_deltas` | 1000 | 10 | 465.4 | 475.0 | 21489.0 | 1876275200 | 1521928 |

## Fases p50

| Estrategia | Relaciones | Stage | BEGIN | Lookups | Deletes | Creates | Integrity | COMMIT | Close | Total |
| --- | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: |
| `prepared_individual` | 1 | 0.0 | 0.1 | 5.1 | 3.2 | 4.7 | 2.8 | 1.4 | 64.4 | 82.0 |
| `prepared_individual` | 10 | 0.0 | 0.1 | 5.3 | 3.3 | 47.5 | 2.9 | 0.6 | 63.0 | 123.4 |
| `prepared_batch` | 1 | 0.0 | 0.1 | 5.2 | 3.3 | 410.4 | 3.3 | 0.9 | 74.7 | 499.0 |
| `prepared_batch` | 10 | 0.0 | 0.2 | 5.1 | 3.2 | 558.2 | 3.3 | 0.7 | 74.3 | 645.3 |
| `prepared_batch` | 1000 | 0.0 | 0.1 | 19.6 | 9.5 | 19050.4 | 3.3 | 1.4 | 81.4 | 19167.7 |
| `staging_copy` | 1 | 0.0 | 0.2 | 5.1 | 3.3 | 63.8 | 3.2 | 63.2 | 4.7 | 142.7 |
| `staging_copy` | 10 | 0.0 | 0.1 | 5.5 | 3.5 | 63.5 | 3.0 | 64.6 | 4.6 | 144.4 |
| `staging_copy` | 1000 | 0.1 | 0.2 | 19.0 | 9.9 | 65.9 | 3.4 | 69.2 | 6.1 | 171.6 |
| `aggregate_10_deltas` | 1 | 0.0 | 0.2 | 5.3 | 3.3 | 62.8 | 3.4 | 63.6 | 4.6 | 143.9 |
| `aggregate_10_deltas` | 10 | 0.0 | 0.2 | 6.1 | 3.9 | 65.3 | 3.4 | 64.5 | 5.1 | 148.7 |
| `aggregate_10_deltas` | 1000 | 1.2 | 0.2 | 151.2 | 102.5 | 73.1 | 3.3 | 127.9 | 7.8 | 465.4 |

## Gate

`LADYBUG_DELTA_PERFORMANCE_PASS`: **false**. Estrategia segura elegida: `prepared_individual (≤10); prepared_batch (>10)`; p95 1–10 relaciones: 123.5 ms (límite < 150 ms); p95 1.000 relaciones: 19249.3 ms (límite < 500 ms).

El writer usa sentencias preparadas individuales para 1–10 relaciones y un `UNWIND` por tipo a partir de 11; también borra por batch. `staging_copy` es más rápido en el corpus, pero no se adopta para un delta genérico: el esquema permite multiplicidad y `COPY` no preserva por sí solo la detección atómica de duplicados.

La estrategia agregada agrupa diez deltas antes de mutar. Su medición excluye la espera de cola, por lo que una ventana de 150–500 ms no puede declarar el objetivo end-to-end de 1–10 relaciones.

## Límites

- Each sample copies the qualified Linux amd64 database and measures one transaction; results do not generalize to other LadybugDB builds or filesystems.
- The individual strategy is measured only for 1–10 relationships because 1,000 per-row native calls violate the production batching policy by design.
- RSS is the process resident set observed after each sample; it is not a cgroup limit or a storage-controller measurement.
- The aggregate result excludes its 150–500 ms queue wait, which must be added before claiming end-to-end latency.
