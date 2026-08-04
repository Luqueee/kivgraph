# Perfil de deltas LadybugDB

- Commit medido: `0a3b11ddd70d8d85102a26a61fabad7e16a65ef5-dirty`
- Fecha: `2026-08-04T21:57:00Z`
- Plataforma: `linux/amd64`, `go1.24.4`
- Base: `43290624` bytes; muestras por caso: `5`

## Resultados

| Estrategia | Relaciones | Deltas agregados | p50 Apply ms | p95 Apply ms | Relaciones/s | RSS pico bytes | Alloc/batch bytes |
| --- | ---: | ---: | ---: | ---: | ---: | ---: | ---: |
| `prepared_individual` | 1 | 1 | 20.8 | 30.7 | 48.1 | 177467392 | 4888 |
| `prepared_individual` | 10 | 1 | 112.1 | 115.7 | 89.2 | 195977216 | 11344 |
| `prepared_individual` | 1000 | 1 | — | — | — | — | — |
| `prepared_batch` | 1 | 1 | 422.6 | 428.6 | 2.4 | 201486336 | 4568 |
| `prepared_batch` | 10 | 1 | 573.8 | 613.8 | 17.4 | 215343104 | 13424 |
| `prepared_batch` | 1000 | 1 | 19113.9 | 19211.5 | 52.3 | 392540160 | 979072 |
| `staging_copy` | 1 | 1 | 223.5 | 224.2 | 4.5 | 394235904 | 4848 |
| `staging_copy` | 10 | 1 | 221.7 | 223.1 | 45.1 | 358240256 | 6024 |
| `staging_copy` | 1000 | 1 | 263.3 | 271.9 | 3797.8 | 413667328 | 136264 |
| `aggregate_10_deltas` | 1 | 10 | 221.9 | 224.3 | 45.1 | 388845568 | 5976 |
| `aggregate_10_deltas` | 10 | 10 | 227.0 | 228.0 | 440.5 | 353529856 | 19936 |
| `aggregate_10_deltas` | 1000 | 10 | 758.4 | 771.3 | 13186.3 | 1518698496 | 1522696 |

## Fases p50

| Estrategia | Relaciones | Stage | BEGIN | Lookups | Deletes | Creates | Integrity | COMMIT | Close | Total |
| --- | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: | ---: |
| `prepared_individual` | 1 | 0.0 | 0.1 | 9.0 | 3.6 | 4.7 | 2.9 | 1.0 | 64.3 | 85.7 |
| `prepared_individual` | 10 | 0.0 | 0.1 | 55.9 | 3.1 | 48.2 | 3.2 | 0.7 | 64.1 | 176.2 |
| `prepared_batch` | 1 | 0.0 | 0.2 | 5.1 | 3.4 | 410.4 | 3.2 | 1.3 | 74.5 | 496.8 |
| `prepared_batch` | 10 | 0.0 | 0.1 | 5.3 | 3.4 | 560.9 | 3.1 | 1.0 | 75.3 | 650.0 |
| `prepared_batch` | 1000 | 0.0 | 0.1 | 19.3 | 9.5 | 19078.7 | 3.5 | 2.2 | 81.7 | 19196.2 |
| `staging_copy` | 1 | 0.1 | 0.2 | 86.6 | 3.0 | 62.7 | 3.3 | 67.0 | 5.0 | 228.6 |
| `staging_copy` | 10 | 0.1 | 0.2 | 84.2 | 3.4 | 63.7 | 3.6 | 66.3 | 4.9 | 226.6 |
| `staging_copy` | 1000 | 0.2 | 0.2 | 110.1 | 10.0 | 66.8 | 3.2 | 72.5 | 5.8 | 269.5 |
| `aggregate_10_deltas` | 1 | 0.1 | 0.2 | 85.8 | 3.4 | 62.8 | 3.3 | 67.0 | 4.6 | 226.6 |
| `aggregate_10_deltas` | 10 | 0.1 | 0.2 | 87.2 | 3.8 | 64.7 | 3.5 | 67.1 | 4.6 | 232.2 |
| `aggregate_10_deltas` | 1000 | 1.6 | 0.2 | 449.6 | 101.8 | 72.4 | 3.6 | 128.3 | 8.6 | 767.0 |

## Gate

`LADYBUG_DELTA_PERFORMANCE_PASS`: **true**. Estrategia segura elegida: `prepared_individual (≤10); staged_copy (>10)`; p95 `Apply` 1–10 relaciones: 115.7 ms (límite < 150 ms); p95 `Apply` 1.000 relaciones: 271.9 ms (límite < 500 ms).

`Close` se mide por separado como coste de flush y cierre de la muestra; no forma parte de `Writer.Apply`, por lo que no participa en el gate de aplicación.

El writer valida 1–10 relaciones en una consulta exacta y usa staging transaccional con `COPY` a partir de 11: importa endpoints en una tabla efímera, rechaza solapamientos exactos, la vacía y sólo entonces copia la relación canónica. También borra por batch.

La estrategia agregada agrupa diez deltas antes de mutar. Su medición excluye la espera de cola, por lo que una ventana de 150–500 ms no puede declarar el objetivo end-to-end de 1–10 relaciones.

## Límites

- Each sample copies the qualified Linux amd64 database and measures one transaction; results do not generalize to other LadybugDB builds or filesystems.
- The individual strategy is measured only for 1–10 relationships because 1,000 per-row native calls violate the production batching policy by design.
- RSS is the process resident set observed after each sample; it is not a cgroup limit or a storage-controller measurement.
- The aggregate result excludes its 150–500 ms queue wait, which must be added before claiming end-to-end latency.
