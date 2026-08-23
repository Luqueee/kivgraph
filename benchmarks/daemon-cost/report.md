# Lo que un proceso para muchos clientes cuesta de verdad

`LUQUE-2222`. `kivgraph daemon` se escribió sobre una aritmética: si un `serve`
conserva `71 MB` de páginas privadas y esa cifra es plana en el número de
clientes, N de ellos deberían convertirse en uno. Esto lo mide.

El demonio tiene **dos puertas** y la medición ya no es una: `results.json` es la
corrida por socket unix y `results-http.json` la del transporte Streamable HTTP,
sobre la misma generación y con la misma carga. Las dos hacen falta porque
**ninguna configuración de editor acepta una ruta de socket**: la cifra que se
puede prometer es la de HTTP, y no es la misma.

Este informe no emite ningún veredicto de aceptación: mide dos formas de servir
el mismo grafo, por dos transportes, en un entorno concreto.

## Entorno y procedencia

|dato|valor|
|---|---|
|fecha|2026-08-23|
|commit|`65c3921`|
|plataforma|VM `linux/arm64` de Docker Desktop, imagen `golang:1.26-trixie`|
|kernel|`Linux 6.12.54-linuxkit`, `10` CPU, page size `4096`|
|corpus|`kena`, `37` repositorios, **`108.737` símbolos**|
|snapshot|`77,6 MB`, generación `000001`|
|esquema|`daemon-cost-v2`|
|digest socket|`c5ea75119906`|
|digest http|`55ad786d94d1`|
|carga|`2.000` llamadas medidas, `4.000` descartadas, semilla `42`|

El esquema subió a `v2` porque el transporte entró en la identidad de la corrida.
Un fichero `v1` calculó su digest sin él, así que no se puede comparar por digest
con uno de éstos: `v1` nombraba un experimento que podía haber sido cualquiera de
las dos puertas. Los digests de arriba son distintos entre sí por eso mismo, y es
lo que impide que una cifra de socket pase por una de HTTP.

La generación se publicó en el host (darwin/arm64, donde hay `node 25` y el
TypeScript de `kena` carga entero) y **se lee** en Linux. Los dos brazos leen ese
mismo fichero, byte a byte: ninguno deriva, así que LadybugDB no participa. El
workspace se monta en sólo lectura, que es lo que hace imposible tocar un
repositorio indexado.

`108.737` símbolos **no es el corpus de `LUQUE-2221`**, que midió `117.499`.
Cualquier comparación por símbolo con esa ficha tiene que usar la cifra de su
propia pasada; aquí un `serve` sale a `635 B` por símbolo contra los `647` de
allí, que es la misma magnitud y no el mismo número.

## La respuesta por socket

`Private_Dirty` total, las tres pasadas del socket:

|clientes|N procesos|1 demonio|proporción|ahorro|
|---|---|---|---|---|
|`1`|`65,9` / `67,4` / `66,1` MB|`66,9` / `65,1` / `66,6` MB|`0,966`–`1,015`|`-1` a `+2` MB|
|`2`|`129,6` / `133,4` / `129,9` MB|`67,5` / `68,4` / `67,3` MB|`0,513`–`0,521`|`62`–`65` MB|
|`4`|`264,0` / `263,0` / `263,9` MB|`69,2` / `69,6` / `70,5` MB|`0,262`–`0,267`|`193`–`195` MB|
|`8`|`533,9` / `529,0` / `533,0` MB|`68,2` / `71,1` / `82,1` MB|`0,128`–`0,154`|`451`–`466` MB|

**A ocho clientes el demonio cuesta la séptima parte.** Y la cifra que decide no
es ninguna de esas filas, es la pendiente:

|pasada|N procesos|1 demonio|ratio|
|---|---|---|---|
|1|`67,0` MB por cliente (fijo `-3,0`)|`0,2` MB por cliente (fijo `67,3`)|`0,003`|
|2|`65,9` MB por cliente (fijo `1,0`)|`0,7` MB por cliente (fijo `65,8`)|`0,011`|
|3|`66,9` MB por cliente (fijo `-2,5`)|`2,3` MB por cliente (fijo `63,1`)|`0,034`|

El brazo de procesos sube **un servidor entero por cliente**, y su intercepto es
cero dentro del ruido: no hay nada compartido que amortizar, porque las páginas
privadas son por definición lo que no se comparte. El demonio sube entre `0,3 %` y
`3,4 %` de eso, y todo su coste está en el intercepto -- una carga.

La corrida `v2` publicada en `results.json` cae dentro de eso: `0,5 MB` por
cliente, fijo `66,6`.

## La respuesta por HTTP, que es la que se puede prometer

Ninguna configuración de cliente MCP acepta un socket unix -- `integrations.go`
escribe `command` + `args` para los cinco targets, y las formas remotas que los
cinco aceptan son `url`. Así que la cifra de arriba describe una puerta que un
editor no puede cruzar, y la que importa es ésta:

|clientes|N procesos|1 demonio|proporción|
|---|---|---|---|
|`1`|`66,7` MB|`75,8` MB|`1,137`|
|`2`|`135,5` MB|`96,0` MB|`0,709`|
|`4`|`266,5` MB|`125,6` MB|`0,471`|
|`8`|`535,5` MB|`165,9` MB|`0,310`|

**La pendiente por HTTP es `12,5 MB` por cliente, no `0,5`.** Cuatro pasadas dan
`12,1`, `12,5`, `12,7` y `12,8`, con el intercepto entre `68,6` y `72,3`: es la
medición más estable de este benchmark y no deja margen para leerla como ruido.

El ahorro **sigue existiendo y es grande** -- a ocho clientes `166` contra `536 MB`,
la tercera parte-- pero ya no es «una carga y nada más». Y el cruce se mueve: por
socket estaba en `1,00` clientes y por HTTP está en `1,26`, porque **a un cliente
el demonio por HTTP es peor**, `75,8` contra `66,7 MB`. Un usuario con un solo
editor abierto no gana nada instalando el demonio; gana desde el segundo.

### De dónde salen esos `12 MB`, y por qué no son el grafo

No son estructura por sesión: son **tráfico retenido**. El SDK da a cada sesión
su propio `MemoryEventStore` para poder reanudar un stream cortado, y ese store
guarda hasta `10 MiB` por defecto -- `NewMemoryEventStore(nil)` en
`streamable.go:391`, `defaultMaxBytes = 10 << 20` en `event.go:255`. Con `2.000`
llamadas por brazo cada sesión llena ese techo con respuestas del grafo.

La medición lo separa. Bajando la carga a `64` llamadas y `8` de calentamiento,
sobre el mismo corpus y los mismos recuentos de clientes, la pendiente del
demonio cae a `2,7` / `2,7` / `2,1 MB` por cliente en tres pasadas. Es el mismo
código y el mismo transporte: lo único que cambió es cuántos bytes hay que
recordar.

Así que la lectura honesta son dos cifras, no una:

|carga|pendiente del demonio por HTTP|
|---|---|
|`2.000` llamadas por brazo|`12,1`–`12,8` MB por cliente|
|`64` llamadas por brazo|`2,1`–`2,7` MB por cliente|

`12,5 MB` es un **techo bajo tráfico sostenido**, no lo que cuesta un editor
abierto sin usar. Cuál de las dos describe a un usuario real no lo dice este
benchmark, porque no mide clientes reales.

**No se puede quitar desde aquí.** `NewStreamableHTTPHandler` no acepta un
`EventStore`: el campo existe en `StreamableServerTransport`, pero el handler
construye el transporte por su cuenta y `StreamableHTTPOptions` sólo expone
`Stateless` y `JSONResponse` (`streamable.go:53-71`, con un `TODO(#148)` sobre
retención de sesión justo en medio). Las salidas serían reimplementar el
enrutado de sesiones del SDK o servir en modo `Stateless`, que prohíbe la GET
colgada y las peticiones del servidor al cliente. Ninguna se tomó: la primera
duplica la capa HTTP del SDK y la segunda cambia el contrato del transporte para
ahorrar memoria que está acotada.

## Una predicción mía que la medición desmiente

Escribí en el arnés que a un cliente el demonio tenía que salir **peor**: misma
carga más una sesión. Medido, la proporción a un cliente es `0,966`–`1,015`: no es
peor ni mejor, es la misma cifra con ruido de un megabyte. El servidor MCP por
sesión -- once registros de tool, un decodificador, buffers-- **no aparece** contra
los `66 MB` que cuesta la carga.

Eso también responde por qué el cruce sale en `0,99`–`1,05` clientes: no hay un
tramo donde el demonio pierda. Gana desde el primer cliente, empatando.

## Lo que la medición añade y no se buscaba

**El pico también baja**, y es la mitad que `LUQUE-2221` declaró como hueco:

|clientes|pico N procesos|pico 1 demonio|
|---|---|---|
|`1`|`156`–`157` MB|`155`–`159` MB|
|`2`|`281`–`285` MB|`158`–`161` MB|
|`4`|`536`–`538` MB|`167`–`169` MB|
|`8`|`1.046`–`1.049` MB|`187`–`189` MB|

Una máquina que arranca ocho servidores a la vez paga un gigabyte de pico. Un
demonio paga `188 MB`. Ése era el único sitio donde los `60,5 MB` de asignación
retirada en `LUQUE-2216`–`LUQUE-2220` podían aparecer residentes, y lo que se ve
es que el pico de ocho procesos es ocho cargas simultáneas -- no una carga más
gorda.

**Un cliente nuevo se responde entre `8` y `15` veces antes:**

|clientes ya conectados|N procesos|1 demonio|
|---|---|---|
|`1`|`107`–`124` ms|`12`–`13` ms|
|`2`|`102`–`117` ms|`12`–`42` ms|
|`4`|`110`–`112` ms|`13` ms|
|`8`|`185`–`263` ms|`13`–`17` ms|

Es lo que ve quien abre una segunda ventana del editor: un proceso nuevo tiene
que cargar el snapshot, una sesión nueva no.

**Y a ocho clientes el demonio contesta más rápido**, que no era el objetivo:
`p99` de `19,0`–`20,3 ms` contra `12,8`–`17,7`. Ocho procesos compiten por diez
CPU y cada uno arrastra su propio runtime de Go; uno solo reparte las mismas
llamadas sin esa competencia. A uno, dos y cuatro clientes los dos brazos empatan
dentro del ruido, así que la ventaja aparece justo donde aparece la contención.

## Limitaciones

* **La lectura del demonio por socket a ocho clientes es la menos estable**:
  `68,2`, `71,1` y `82,1 MB` en tres pasadas. El signo del resultado no está en
  duda -- el brazo de procesos está en `530`-- pero la pendiente del demonio por
  socket se estima entre `0,2` y `2,3 MB` por cliente según la pasada. Lo que se
  puede afirmar ahí es el orden de magnitud, no la cifra. Por HTTP no pasa: las
  cuatro pasadas caen en `12,1`–`12,8`.
* **La pendiente por HTTP depende de la carga y no se mapeó su forma.** Se midieron
  dos puntos -- `2.000` y `64` llamadas por brazo-- y dan `12,5` y `2,4 MB` por
  cliente. Entre ellos no hay curva medida: un intento intermedio salió con
  filas vacías y se descartó en vez de publicarse. Lo que está establecido es que
  la pendiente crece con el tráfico y que su techo lo fija un límite del SDK, no
  cuál es la cifra a una carga cualquiera.
* **No es bare metal.** Es la VM de Docker Desktop sobre Apple Silicon. El
  `page size` es `4096`, el mismo que amd64, que es lo que hace comparables las
  unidades; el resto del entorno es el de un contenedor.
* **La generación se publicó en darwin y se leyó en Linux.** Mismo arco, formato
  de anchura fija y little-endian por diseño. Es lo que permite que los dos
  brazos lean el mismo fichero, y es una dependencia declarada del método.
* **No se midió con clientes reales.** La carga son llamadas de tool generadas
  desde sondas del propio snapshot; un editor intercala pausas, y una sesión
  ociosa no ensucia páginas.
* **No se midió por encima de ocho clientes.** La pendiente medida extrapola a
  que el demonio siga plano, y una extrapolación no es una medición.

## Un defecto del arnés que la primera pasada destapó

La primera versión publicó `symbols: 0` sin fallar. `readStatus` adivinó la forma
de `graph_status` -- el conteo está anidado bajo `results`-- y sólo comprobaba el
`snapshot_id`, así que el corpus quedaba sin nombre en un artefacto que existe
para ser comparado. Ahora se niega cuando el conteo es cero, como ya hacía el
arnés de `shared-snapshot`.

El mismo arreglo destapó el otro: `commit` se rellenaba dentro de `writeResults`,
después de calcular las limitaciones, así que un commit ausente nunca podía
declararse. Se lee al construir la corrida.

## Y un defecto de producción

`Listen` fijaba el modo del socket con `chmod` **después** de crearlo, y `chmod`
sobre un socket devuelve `EINVAL` en un bind mount de virtiofs -- que es
exactamente donde corre este benchmark. El demonio no arrancaba. Ahora el socket
nace con el modo puesto, vía `umask`, y no hay `chmod` que pueda fallar.

De paso quedó dicho qué compra ese modo, que es distinto por plataforma: Linux
comprueba permiso de escritura sobre el socket al conectar, así que `0600` es una
puerta real; darwin **ignora** los permisos del socket para conectar, y allí la
puerta es el directorio de estado, que hay que atravesar para llegar a la ruta.

## Reproducir

```bash
# En el host, donde hay node: publicar la generación.
export HOME=/ruta/aislada
kivgraph init --languages go,typescript,rust --repository <nombre>=<ruta> ...
kivgraph index --full

# En Linux, los dos brazos contra ese fichero, una vez por puerta.
# El cwd tiene que ser el checkout, o el commit no se lee y la corrida lo declara
# como limitación.
docker run --rm -w /src \
  -v "$PWD":/src:ro -v /ruta/a/kena:/ruta/a/kena:ro -v "$HOME":"$HOME" \
  -e HOME="$HOME" golang:1.26-trixie bash -c '
    git config --global --add safe.directory /src
    go build -o /out/kivgraph ./cmd/kivgraph
    go build -o /out/daemon-cost ./benchmarks/daemon-cost
    for t in socket http; do
      /out/daemon-cost -server /out/kivgraph \
        -config "$HOME/.config/kivgraph/config.yaml" \
        -generation-dir "$HOME/.local/state/kivgraph/generations/000001" \
        -state-dir "$HOME/.local/state/kivgraph" \
        -clients 1,2,4,8 -calls 2000 -warmup 4000 \
        -transport $t -output /out/run-$t
    done'
```

La contribución del tráfico se aísla bajando la carga sobre la misma generación,
que es la única diferencia entre las dos filas de la tabla de arriba:

```bash
/out/daemon-cost ... -clients 1,8 -calls 64 -warmup 8 -transport http -output /out/light
```

El `digest` es la identidad de las entradas -- corpus, generación, recuentos,
semilla-- y no de las medidas: dos corridas del mismo experimento comparten
cadena, y una cifra distinta no puede pasar por el mismo experimento.
