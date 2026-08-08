# Benchmark end-to-end del visor web

- **Fecha:** 2026-08-08
- **Commit:** `fc2a808671cff0f86fc66b1af157cdccce55204c-dirty`
- **Host:** Linux `amd64`, AMD Ryzen 7 9700X, 16 CPUs, 23.4 GiB RAM
- **Go/Node/pnpm:** `go1.24.4` / `v25.9.0` / `11.5.1`
- **Navegador:** Chrome `150.0.7871.24`, viewport `1440 × 1000`, DPR `1`
- **Renderer:** ANGLE Intel UHD 620 en el adaptador headless; no representa una GPU discreta
- **Snapshot:** `~/kena`, `snapshot_id=2`, publicado por `ladygraph ui`

## Comando de verificación

El resultado estructurado queda en `results.json`. El harness valida cada límite y
falla cerrado ante una regresión:

```bash
node benchmarks/web-viewer/harness.mjs \
  --results benchmarks/web-viewer/results.json \
  --allow-limitations
```

`--allow-limitations` solo permite terminar con `WEB_VIEWER_PERFORMANCE_PASS_WITH_LIMITS`
cuando una limitación documentada impide emitir el gate. Sin esa opción, el mismo
resultado termina con código `1`.

La captura se hizo con Chromium contra:

```bash
ladygraph ui --config /home/devlabs/.config/ladygraph/config.yaml \
  --addr 127.0.0.1:7777
```

Se comprobaron también búsqueda con debounce, selección de símbolo, expansión de
vecindad, filtro `CANDIDATE`, cambio 2D/3D, slider y ausencia de `pageerror`.

## Dataset observado

| Recurso | Cantidad |
| --- | ---: |
| Repositories | 35 |
| Packages | 105 |
| Files | 4.261 |
| Symbols | 83.293 |
| Edges | 204.630 |
| Hubs | presente |

El SLO de `docs/performance/slo.md` define como referencia `100.000` símbolos y
`1.000.000` aristas. El snapshot publicado disponible para esta ejecución no
alcanza esa escala; el harness lo marca explícitamente y no emite el gate.

## Métricas

| Métrica | Resultado | Límite | Estado |
| --- | ---: | ---: | --- |
| `meta` HTTP p95 | `1,255 ms` | `50 ms` | PASS |
| payload de topología p95 | `18,31 ms` | `500 ms` | PASS |
| primer frame p95 | `84,53 ms` | `1.000 ms` | PASS |
| pan/zoom p95 | `11,30 ms` | `33,3 ms` | PASS |
| hover/picking p95 | `4,90 ms` | `5 ms` | PASS |
| vecindad depth 3 p95 | `1,51 ms` | `5 ms` | PASS |
| payload máximo | `788.786 B` | `32 MiB` | PASS |
| heap JavaScript | `41.534.874 B` | `512 MiB` | PASS |
| errores | `0` | `0` | PASS |

Se midieron 30 respuestas de `meta`, 10 tiles por nivel (`lod=1..3`), 8 cargas
para primer frame, 61 eventos de pan, 20 de zoom, 21 de hover y 20 vecindades.
Las respuestas HTTP fueron `200`; el worker decodificó payload `LGVB` v2 con
`snapshot_id=2` y la escena se renderizó.

## Gate

`WEB_VIEWER_PERFORMANCE_PASS`: **false**.

La única razón es la diferencia entre el corpus medido (`83.293` símbolos,
`204.630` aristas) y el corpus de referencia contractual (`100.000` símbolos,
`1.000.000` aristas). Las métricas observadas sí están dentro de sus límites,
pero no se extrapolan silenciosamente a una escala que no fue ejecutada.
