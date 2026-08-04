# Benchmark de interning del HotSnapshot

- Commit medido: `a9893e2-dirty`
- Fecha: `2026-08-04T22:00:00Z`
- Plataforma: `linux/amd64`, `go1.24.4`
- Comando: `go test -run '^$' -bench 'Benchmark(StringInterner|DuplicatedStrings)$' -benchmem -count=5 ./internal/hotsnapshot`

## Carga

100.000 entradas: 1.000 strings distintos, cada uno repetido 100 veces.

## Medianas

| Estrategia | ns/op | B/op | allocs/op |
| --- | ---: | ---: | ---: |
| Tabla internada | 773.404 | 144.250 | 34 |
| Strings duplicados | 1.551.387 | 6.405.634 | 100.001 |

La tabla internada reduce la memoria por operación aproximadamente un 97,7 % y
las asignaciones aproximadamente un 99,97 % en este corpus repetitivo. El
resultado mide construcción; lookup concurrente y serialización se verifican en
la suite de tests, no se infieren de este benchmark.
