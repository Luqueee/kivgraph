# Perfil de deltas LadybugDB

- Commit medido: `e7472c0f135df2e6152d96420a4f86223aa0b338-dirty`
- Fecha: `2026-08-05T18:43:32Z`
- Plataforma: `linux/amd64`, `go1.24.4`
- Base: `66936832` bytes; muestras por caso: `5`

## Resultados

| Estrategia | Relaciones | Deltas agregados | p50 Apply ms | p95 Apply ms | Relaciones/s | RSS pico bytes | Alloc/batch bytes |
| --- | ---: | ---: | ---: | ---: | ---: | ---: | ---: |
| `prepared_individual` | 1 | 1 | 11.7 | 20.2 | 85.5 | 147464192 | 4440 |
| `prepared_individual` | 10 | 1 | 66.7 | 70.4 | 150.0 | 204828672 | 10368 |
| `prepared_individual` | 1000 | 1 | — | — | — | — | — |
| `prepared_batch` | 1 | 1 | 425.0 | 427.8 | 2.4 | 211308544 | 5048 |
| `prepared_batch` | 10 | 1 | 571.2 | 572.6 | 17.5 | 245620736 | 13520 |
| `prepared_batch` | 1000 | 1 | 19010.1 | 19050.7 | 52.6 | 324894720 | 979200 |
| `staging_copy` | 1 | 1 | 106.6 | 107.1 | 9.4 | 346296320 | 4328 |
| `staging_copy` | 10 | 1 | 104.4 | 105.7 | 95.7 | 336474112 | 6080 |
| `staging_copy` | 1000 | 1 | 153.7 | 159.2 | 6506.0 | 384081920 | 179376 |
| `aggregate_10_deltas` | 1 | 10 | 104.4 | 106.8 | 95.8 | 396107776 | 6496 |
| `aggregate_10_deltas` | 10 | 10 | 115.7 | 116.9 | 864.7 | 382115840 | 19936 |
| `aggregate_10_deltas` | 1000 | 10 | 647.3 | 668.3 | 15447.9 | 1733173248 | 1522904 |

## Fases p50

| Estrategia | Relaciones | Stage | BEGIN | Lookups | Deletes | Creates | Integrity | COMMIT | Close | Total |
| --- | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: |
| `prepared_individual` | 1 | 0.0 | 0.2 | 7.9 | 1.0 | 0.7 | 0.6 | 1.1 | 62.9 | 73.5 |
| `prepared_individual` | 10 | 0.0 | 0.1 | 58.4 | 1.2 | 6.0 | 0.7 | 0.9 | 63.3 | 130.5 |
| `prepared_batch` | 1 | 0.0 | 0.2 | 7.0 | 1.1 | 414.0 | 1.0 | 1.1 | 77.6 | 503.1 |
| `prepared_batch` | 10 | 0.0 | 0.1 | 6.9 | 1.1 | 561.1 | 1.0 | 1.1 | 78.7 | 650.3 |
| `prepared_batch` | 1000 | 0.0 | 0.1 | 19.4 | 7.5 | 18982.1 | 1.2 | 2.0 | 97.3 | 19106.6 |
| `staging_copy` | 1 | 0.1 | 0.2 | 32.0 | 1.0 | 7.1 | 1.2 | 64.2 | 5.7 | 111.7 |
| `staging_copy` | 10 | 0.1 | 0.2 | 30.5 | 1.1 | 7.7 | 1.0 | 63.6 | 6.4 | 110.6 |
| `staging_copy` | 1000 | 0.2 | 0.2 | 51.3 | 7.3 | 11.6 | 1.1 | 82.3 | 6.4 | 160.7 |
| `aggregate_10_deltas` | 1 | 0.0 | 0.2 | 30.7 | 1.1 | 7.4 | 1.0 | 64.4 | 5.2 | 110.1 |
| `aggregate_10_deltas` | 10 | 0.1 | 0.2 | 32.6 | 1.5 | 9.3 | 1.0 | 71.0 | 5.7 | 121.4 |
| `aggregate_10_deltas` | 1000 | 1.7 | 0.2 | 377.2 | 101.8 | 16.4 | 1.1 | 152.6 | 8.6 | 655.7 |

## Gate

`LADYBUG_DELTA_PERFORMANCE_PASS`: **true**. Estrategia segura elegida: `prepared_individual (≤10); staged_copy (>10)`; p95 `Apply` 1–10 relaciones: 70.4 ms (límite < 150 ms); p95 `Apply` 1.000 relaciones: 159.2 ms (límite < 500 ms).

`Close` se mide por separado como coste de flush y cierre de la muestra; no forma parte de `Writer.Apply`, por lo que no participa en el gate de aplicación.

El writer valida 1–10 relaciones en una consulta exacta y usa staging transaccional con `COPY` a partir de 11: importa endpoints en una tabla efímera, rechaza solapamientos exactos, la vacía y sólo entonces copia la relación canónica. También borra por batch.

La estrategia agregada agrupa diez deltas antes de mutar. Su medición excluye la espera de cola, por lo que una ventana de 150–500 ms no puede declarar el objetivo end-to-end de 1–10 relaciones.

## Límites

- Each sample copies the qualified Linux amd64 database and measures one transaction; results do not generalize to other LadybugDB builds or filesystems.
- The individual strategy is measured only for 1–10 relationships because 1,000 per-row native calls violate the production batching policy by design.
- RSS is the process resident set observed after each sample; it is not a cgroup limit or a storage-controller measurement.
- The aggregate result excludes its 150–500 ms queue wait, which must be added before claiming end-to-end latency.
