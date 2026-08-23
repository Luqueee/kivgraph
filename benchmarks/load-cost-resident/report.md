# Lo que asignar menos en la carga no cambia

Cuatro fases retiraron `60,5 MB` de lo que la carga de un snapshot asigna, de
`89,7 MB` a `29,2 MB`, y dejaron los bytes vivos idénticos. La pregunta que
ninguna de ellas midió es la que decide sus dos gates: cuántas páginas conserva
residentes un proceso que sirve.

La respuesta es **ninguna menos**.

Las métricas crudas están en `results.json`. Este informe no emite ningún
veredicto de aceptación: mide dos binarios sobre un fichero concreto.

## Entorno y provenance

|dato|valor|
|---|---|
|fecha|`2026-08-23`|
|commit antes|`c420490`|
|commit después|`0ad501a`|
|anfitrión|`Mac17,2` (Apple M5), macOS `26.6`|
|runtime|Docker Desktop `29.1.3`, VM `linux/arm64`, 10 CPU, `8,2 GB`|
|imagen|`golang:1.26-trixie`, glibc `2.41`|
|page size|`4096` bytes|
|corpus|generación `000001` de `kena`, `117.499` símbolos, `35` repositorios|

La generación es una sola y es la misma para todos los brazos: el formato del
fichero no cambió entre los dos commits, así que los dos binarios leen los
mismos bytes.

## Lo medido

Brazo `mapped`, cuatro servidores, `2.000` llamadas medidas tras `4.000` de
calentamiento, semilla `42`.

|pasada|`private_dirty` antes|después|primera respuesta antes|después|
|---|---|---|---|---|
|1|`288,0 MB`|`285,6 MB`|`292 ms`|`147 ms`|
|2|`286,1 MB`|`285,2 MB`|`191 ms`|`156 ms`|
|3|`287,0 MB`|`283,8 MB`|`222 ms`|`138 ms`|

Por servidor son `71,76 MB` antes y `71,22 MB` después: **`0,75 %` de
diferencia**, donde la aritmética de la asignación retirada predeciría unos
treinta megabytes. Por símbolo, `647,348 B` contra `647,104 B`, y el gate de
`LUQUE-2006` está en `800 B`.

## Por qué

Es lo que «transitorio» significa para un asignador. Las páginas que se ensucian
decodificando se devuelven al heap y las reutiliza el trabajo que viene después,
así que **nunca estuvieron residentes en régimen estacionario**. `Private_Dirty`
tomado tras `6.000` llamadas informa del heap de servicio ya asentado, no del
pico de la carga.

`benchmarks/snapshot-heap` ya declaraba que sus cifras son contabilidad del
runtime de Go y no páginas residentes, y que `Private_Dirty` es mayor que
cualquiera de las dos. Lo que no decía -- y esta medición lo dice-- es que bajar
lo asignado no baja `Private_Dirty`.

## Lo que sí compró el trabajo

La primera respuesta: `138-156 ms` contra `191-292 ms`, tres pares de tres. Es
consistente con los `123,6 ms` contra `134,5` que el benchmark de carga mide
directamente en darwin, y es la cifra que ve un cliente que arranca un servidor.

## Lo que esto corrige

`LUQUE-2008` -- el demonio-- se mantiene aplazada porque su condición de
reapertura es un `Private_Dirty` por proceso por encima de `100 MB`. Sobre este
corpus estamos en `72 MB`, y la razón es **el tamaño del corpus, no este
trabajo**: los `94-98 MB` registrados antes eran sobre `161.819` símbolos, o sea
`609 B` por símbolo contra estos `647`. La ficha de `LUQUE-2217` decía «`20,8 MB`
menos de páginas ensuciadas por proceso»; eso no se observa.

## Limitaciones

- No es el host de referencia. El objetivo de distribución declarado es
  `linux/amd64` y esto corrió en una VM `linux/arm64`. Lo que hace comparables
  las cifras residentes en unidades es el page size de `4096` bytes, que
  coincide; lo que las hace comparables entre sí es que los dos brazos corrieron
  en la misma VM contra el mismo fichero.
- La latencia de un contenedor no es comparable con el entorno de referencia y
  aquí no se afirma ningún límite de latencia. Las cifras de primera respuesta se
  usan sólo como razón antes/después dentro de esta VM. En el barrido completo
  una comprobación `p99` falló a 4 clientes (`1,064 ms` contra `1,000`) y el
  arnés no emitió su gate; un contenedor con montajes virtualizados no es donde
  se decide ese límite.
- No se midió el pico residente durante la carga, sólo el valor asentado. Una
  máquina que arranca varios servidores a la vez paga un pico que este benchmark
  no informa, y es el único sitio donde la asignación retirada podría aparecer.
- Tres pares. Bastan para decir que la diferencia residente está por debajo del
  uno por ciento y que la primera respuesta es consistentemente más rápida; no
  bastan para un percentil.

## Reproducir

```bash
kivgraph init --languages go,typescript,rust --repository <nombre>=<ruta> ...
kivgraph index --full

docker run --rm \
  -v "$PWD":/src:ro -v "$HOME/go/pkg/mod":/gomod:ro -v /tmp/linout:/out \
  -e GOMODCACHE=/gomod -e GOPATH=/gopath -e GOCACHE=/out/gocache \
  golang:1.26-trixie bash -c '
    cd /src
    LIB="$(scripts/fetch-ladybug.sh /out/ladybug)"
    CGO_ENABLED=1 CGO_CFLAGS="-I$LIB" CGO_LDFLAGS="-L$LIB -llbug -Wl,-rpath,$LIB" \
      go build -tags ladybug -o /out/kivgraph ./cmd/kivgraph
    CGO_ENABLED=1 go build -o /out/shared-snapshot ./benchmarks/shared-snapshot'

docker run --rm \
  -v "$STATE_HOME":"$STATE_HOME" -v "$WORKSPACE":"$WORKSPACE":ro -v /tmp/linout:/out \
  -e HOME="$STATE_HOME" golang:1.26-trixie \
  /out/shared-snapshot -server /out/kivgraph \
    -config "$STATE_HOME/.config/kivgraph/config.yaml" \
    -generation-dir "$STATE_HOME/.local/state/kivgraph/generations/000001" \
    -output /out/res -clients 4
```

El home de estado y el workspace se montan en su ruta absoluta porque la
configuración las lleva expandidas; el workspace va en sólo lectura, que es lo
que hace imposible tocar un repositorio indexado. La biblioteca nativa fijada
exige glibc `≥ 2.38`: sobre `bookworm` (`2.36`) el enlazado falla con
`GLIBCXX_3.4.31` y `__isoc23_strtol` sin resolver.
