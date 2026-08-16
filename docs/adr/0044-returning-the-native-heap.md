# ADR 0044: Devolver el heap nativo tras construir un snapshot

- **Estado:** aceptada
- **Fecha:** 2026-08-16
- **Revisa:** el coste residente de `serve` y `ui`, y el contrato de `rebuild.ReturnBuildMemory`

## Contexto

Tres servidores vivos en `devlabs` ocupaban 2,6 GB para responder preguntas
sobre el mismo grafo inmutable de 189 MB. El que llevaba tres minutos ocupaba
405 MB; los que llevaban hora y veinte, 1,07 y 1,13 GB, con el 100 % en
`Private_Dirty` y sin una sola página compartida entre ellos.

El ADR 0042 ya había sacado la pasada de indexación del proceso que sirve, y
funciona: la pasada que se forzó para medir tardó `37,5 s` de reloj contra `4m02`
de CPU y su pico murió con el hijo. Lo que quedaba era otra cosa. La generación
avanzó de `000074` a `000085` en ochenta minutos -42 repositorios cuyo HEAD se
mueve-, y **cada publicación obliga a cada servidor vivo a reconstruir su
HotSnapshot**, porque una generación en disco es `graph.db` y su digest, nunca el
snapshot.

La hipótesis obvia era el heap de Go: `BuildSnapshot` materializa el grafo tres
veces y `rebuild/memory.go` ya lo tenía medido. Era falsa, y hubo que instrumentar
para saberlo. `BenchmarkBuildSnapshotMemory` mide lo que un servidor conserva, no
lo que una construcción asigna:

```text
grafo de 189 MB, 102.894 símbolos, cuatro construcciones en un proceso
alloc      1.003 MB por construcción
vivo         173 MB
aparcado     176-180 MB   <- tras debug.FreeOSMemory, cada vez
RSS          309,5 -> 400,7 -> 453,7 -> 511,1 MB
```

El heap de Go se devuelve entero: aparca entre 2,4 y 4 MB por encima de lo vivo,
en todas las iteraciones. Y el RSS sube 67 MB por construcción de todas formas.
La memoria que se queda no es de Go, y ninguna métrica de `runtime` la ve.

Es del asignador de libc, del que asigna el motor C++. glibc da a cada hilo que
compite por el asignador su propia arena y la hace crecer antes de reutilizar
nada, así que un servidor que reconstruye sobre un juego nuevo de hilos conserva
otra arena por construcción hasta que saturan, que es la meseta del gigabyte.
Nada se filtra: cada arena es alcanzable, se reutiliza y el asignador la cuenta
como libre. Por eso exactamente el kernel la cuenta como residente y el scavenge
de Go no la alcanza.

## Decisión

`rebuild.ReturnBuildMemory` devuelve también el heap nativo, con
`malloc_trim(0)` en `internal/nativeheap`. Se llama donde ya estaba el scavenge
de Go: el único momento en que los insumos de la construcción están probadamente
muertos y el proceso no tiene nada que hacer hasta la petición siguiente.

Medido en la misma máquina con la caché de página caliente, cuatro
construcciones:

```text
sólo scavenge de Go   RSS 309,5 -> 400,7 -> 453,7 -> 511,1 MB   (+67 MB cada una)
y malloc_trim         RSS 241,5 -> 243,6 -> 244,5 -> 249,6 MB   (+2,7 MB cada una)
```

−68 MB en la primera construcción, −261 MB en la cuarta, y el crecimiento por
reconstrucción cae un 96 %. El tiempo no se mueve: `1,63-1,77 s` sin la llamada,
`1,67-1,84 s` con ella.

## Alternativas descartadas

**Acotar las arenas desde el proceso.** `mallopt(M_ARENA_MAX, 1)` después de
arrancar no sirve, y la medición es lo que lo dice: el RSS siguió subiendo
`334 -> 396 -> 467 -> 553 MB`. glibc lee `arena_max` cuando crea una arena, así
que lo que ya está repartido en arenas secundarias -montones `mmap`- se queda
donde está. Se implementó, se midió y se retiró: un ajuste que no hace lo que su
nombre promete es peor que no tenerlo.

**La misma cota en el entorno.** `MALLOC_ARENA_MAX=1`, leído antes de la primera
asignación, sí aplana el RSS, en 256-259 MB. Sigue siendo peor que recortar
-240-250 MB- y no está en nuestras manos: un servidor MCP lo lanza su cliente,
que no va a exportar nada.

**`GOMEMLIMIT` sólo en `serve`.** No había hueco que cerrar en el lado de Go: el
heap aparca 2,4 MB por encima de lo vivo. Habría costado GC para no recuperar
nada.

**Construir el snapshot una vez en vez de tres.** Sigue siendo una mejora real
-1.003 MB asignados y 1,7 s por construcción-, pero de CPU y de pico transitorio,
no de lo que un servidor conserva. Este ADR no la descarta; le quita la urgencia
que parecía tener cuando el gigabyte se atribuía al heap de Go.

**Persistir el snapshot por generación y mapearlo.** Es el arreglo estructural:
el publicador construiría una vez y los demás servidores mapearían páginas de
fichero compartidas, en vez de reconstruir cada uno en su propio heap. Ataca la
duplicación N× y la reconstrucción del seguidor, que este ADR no toca. Queda
pendiente, con su propio formato versionado e integridad.

## Consecuencias

Un servidor deja de crecer con cada generación publicada. Con la frecuencia
observada -diez publicaciones en ochenta minutos- eso es la diferencia entre
aparcar en 250 MB y aparcar en un gigabyte.

La llamada es de glibc. En macOS y en un build sin cgo, `nativeheap.Return`
informa de que no hay asignador nativo al que devolver nada y el proceso sigue:
la devolución es una economía, nunca una precondición. El coste es una llamada
por publicación, no por consulta.

Lo que no cambia: cada servidor sigue construyendo su propio snapshot, y tres
servidores siguen costando tres veces el snapshot vivo. Eso lo arregla la
alternativa que queda pendiente, no esta.

## Verificación

```bash
KIVGRAPH_SNAPSHOT_BUILD_DB=<generación>/graph.db \
  make test-ladybug PKGS=./internal/rebuild \
  ARGS='-run NONE -bench BenchmarkBuildSnapshotMemory -benchtime=1x -count=4'
```

`KIVGRAPH_SNAPSHOT_BUILD_UNBOUNDED_HEAP=1` reproduce el camino anterior en la
misma máquina y con la misma caché caliente, que es la única forma de distinguir
un efecto del asignador de una lectura en frío de un fichero de 189 MB.

El banco mide la construcción; lo que decide es un servidor. Tres servidores
reales en `devlabs`, con este bundle instalado y dos publicaciones forzadas de
las 42 repositorios:

```text
                    antes    1ª publicación   2ª publicación
pid 313694         256 MB          256 MB           256 MB
pid 313703         259 MB          261 MB           269 MB
pid 313840         259 MB          259 MB           263 MB
```

Entre 0 y 5 MB por publicación y por servidor, donde antes eran entre 61 y
80 MB. Los tres juntos ocupan 788 MB, contra los 2,6 GB que ocupaban tres
servidores que además seguían subiendo.

Lo que no baja es el pico: el `VmHWM` de cada uno sigue en torno a `1,05 GB`,
que es el transitorio de una construcción. Esta decisión no lo toca, y es
exactamente lo que queda para las dos alternativas pendientes.
