# Corpus sintético grande de Ladygraph

- Commit: `f968d16e4bf19b45b571e6571ce999ba45ed73cb`
- Plataforma: Linux `amd64`
- CPU: AMD Ryzen 7 9700X 8-Core Processor
- Go: `go1.24.4`
- LadybugDB core/binding: `v0.13.1`
- Semilla: `42`
- Entrada privada: `/tmp/ladygraph-large-corpus`

## Escala

| Recurso | Cantidad |
| --- | ---: |
| Repositorios | 40 |
| Archivos | 100.000 |
| Símbolos | 1.000.000 |
| Nodos totales | 1.100.040 |
| Aristas | 10.000.000 |

## Generación

El comando `go run ./cmd/ladygraph benchmark generate-graph --repositories 40 --files 100000 --symbols 1000000 --edges 10000000 --seed 42` terminó en `7,756 s`.

Una segunda generación con la misma semilla produjo byte a byte los cinco
archivos del corpus. Los SHA-256 están en `results.json`.

## Carga COPY

| Métrica | Resultado |
| --- | ---: |
| Exportación CSV | 11.922,2 ms |
| Carga de nodos | 2.424,6 ms |
| Carga de aristas | 6.716,7 ms |
| Carga COPY | 9.141,3 ms |
| End-to-end | 21.063,5 ms |
| COPY | 1.214.272,8 registros/s |
| End-to-end | 526.980,2 registros/s |
| RSS máximo | 2.079.531.008 bytes |
| Base generada | 432.570.368 bytes |

El gate de escala completa pasó: `full_initial_scale`, `counts_verified` y
`rss_within_2_gib` fueron `true`.

## Validación lógica

`doctor storage` pasó en dos cargas independientes. Ambas conservaron:

- `Repository=40`;
- `File=100000`;
- `Symbol=1000000`;
- `CONTAINS=100000`;
- `DEFINES=1000000`;
- `REFERENCES=4450001`;
- `CALLS_DIRECT=4449999`;
- cero violaciones de integridad.

Los resúmenes lógicos de las dos cargas fueron idénticos.

## Limitación

Los archivos nativos `graph.db` no fueron byte a byte idénticos: la primera
carga produjo `432.570.368` bytes y la segunda `433.037.312` bytes, con SHA-256
distintos. Por tanto, este benchmark certifica reproducibilidad byte a byte del
corpus y reproducibilidad lógica de los hechos almacenados, no reproducibilidad
binaria del archivo nativo LadybugDB.
