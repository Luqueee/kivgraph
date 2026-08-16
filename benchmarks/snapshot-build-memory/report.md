# Benchmark de la memoria que deja una construcción de snapshot

- Commit medido: `5521ced`
- Fecha: `2026-08-16T13:50:00Z`
- Máquina: `devlabs`, AMD Ryzen 7 9700X, 23 GiB, `linux/amd64`, `go1.24.4`
- Corpus: el grafo canónico real de esa máquina — `189.579.264` bytes, `102.894`
  símbolos, 42 repositorios
- Comando:

```bash
KIVGRAPH_SNAPSHOT_BUILD_DB=<generación>/graph.db \
  make test-ladybug PKGS=./internal/rebuild \
  ARGS='-run NONE -bench BenchmarkBuildSnapshotMemory -benchtime=1x -count=4'
```

## Qué se mide y por qué no es `B/op`

Un servidor no cuesta lo que asigna, cuesta lo que conserva. Las cuatro
construcciones ocurren **en un mismo proceso**, que es lo que hace un servidor a
lo largo de las generaciones que se publican: una sola construcción en un
proceso nuevo no muestra el crecimiento. Las métricas son el arena de Go tras la
construcción, el arena tras `ReturnBuildMemory` -donde un servidor aparca hasta
la petición siguiente-, el conjunto vivo y el RSS del proceso.

## Resultado

Caché de página caliente en las dos filas, misma máquina, cuatro construcciones
seguidas:

| | RSS 1ª | 2ª | 3ª | 4ª | por construcción | s/construcción |
| --- | ---: | ---: | ---: | ---: | ---: | ---: |
| sólo scavenge de Go | 309,5 MB | 400,7 MB | 453,7 MB | 511,1 MB | **+67 MB** | 1,63-1,77 |
| y `malloc_trim` | 241,5 MB | 243,6 MB | 244,5 MB | 249,6 MB | **+2,7 MB** | 1,67-1,84 |

−68 MB en la primera construcción, −261 MB en la cuarta, y el crecimiento cae un
96 % sin coste medible en tiempo.

El dato que descarta la explicación obvia: el heap de Go **aparca entre 176,6 y
179,9 MB en las dos filas**, contra 173 MB de snapshot vivo. Se devuelve entero,
siempre, y aun así el RSS subía 67 MB por construcción. La memoria que se quedaba
es la del asignador de libc del que asigna el motor C++, que ninguna métrica de
`runtime` ve.

## Alternativas medidas y descartadas

| estrategia | RSS | crecimiento | por qué no |
| --- | --- | ---: | --- |
| `mallopt(M_ARENA_MAX,1)` en proceso | 334 → 396 → 467 → 553 MB | +73 MB | glibc lee `arena_max` al crear una arena; lo ya repartido en arenas `mmap` no se mueve |
| `MALLOC_ARENA_MAX=1` en el entorno | 259 → 259 → 256 MB | plano | efectivo, pero peor que recortar y no está en nuestras manos: al servidor lo lanza su cliente |
| `MALLOC_TRIM_THRESHOLD_` | 259 → 267 → 317 MB | +29 MB | sólo parcial |
| `MALLOC_ARENA_MAX=2` | 291 → 314 → 313 MB | +11 MB | sólo parcial |

## Limitaciones

- El RSS sale de `/proc/self/statm`, así que en macOS el banco no publica esa
  métrica y la comparación existe sólo en Linux.
- Las primeras muestras de la investigación corrieron en frío y daban 2,7-3,3 s
  por construcción; se leyeron como un 45 % de mejora del asignador y **no lo
  eran**. Todas las cifras de aquí son en caliente.
- `KIVGRAPH_SNAPSHOT_BUILD_UNBOUNDED_HEAP=1` reproduce el camino anterior en la
  misma máquina, que es la única forma de separar un efecto del asignador de una
  lectura en frío de un fichero de 189 MB.
- Esto no toca la duplicación entre servidores: cada uno sigue construyendo su
  propio snapshot y tres siguen costando tres veces los 173 MB vivos. Ver
  ADR 0044.

## Lo que pasó después

Este banco mide **derivar** un snapshot, que desde el ADR 0045 ya no es lo que
hace un servidor: una generación lleva el suyo y el servidor lo lee. Las cifras
de arriba siguen describiendo el coste de la derivación -- que es lo que ocurre
cuando el fichero falta, es ajeno, está rancio o está corrupto, y lo que `doctor`
hace siempre a propósito-, pero el coste de instalar una generación ya no es
éste. Medido sobre la misma máquina y el mismo grafo, un servidor pasó de
253-255 MB de RSS y 787-836 MB de pico a 150-152 MB y 264-269 MB.
