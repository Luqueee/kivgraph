# Lo que un proceso para muchos clientes cuesta de verdad

`LUQUE-2222`, corregido en `LUQUE-2223` y otra vez en `LUQUE-2224`.
`kivgraph daemon` se escribió sobre una aritmética: si un `serve` conserva
`71 MB` de páginas privadas y esa cifra es plana en el número de clientes, N de
ellos deberían convertirse en uno. Esto lo mide.

Se ha corregido dos veces, y las dos por el mismo motivo: **la cifra publicada
describía una situación que no era la del usuario.** Primero se publicó la del
socket unix, que ningún cliente MCP puede marcar. Luego la de HTTP bajo `2.000`
llamadas por sesión, que **ninguna sesión real hace**. Esta versión mide la carga
que un editor produce de verdad, contada del event log de una máquina en uso.

|artefacto|puerta|carga|
|---|---|---|
|`results.json`|socket unix|real|
|`results-http.json`|Streamable HTTP|real|
|`results-http-sustained.json`|Streamable HTTP|`2.000` llamadas: el techo|

Este informe no emite ningún veredicto de aceptación: mide dos formas de servir
el mismo grafo, por dos transportes y a dos cargas, en un entorno concreto.

## Qué carga produce un cliente real

La carga no se eligió: se contó. Un `kivgraph serve` escribe cada llamada de tool
en su event log, así que el log de una máquina en uso durante dos días
-- `2026-08-21` a `2026-08-23`, editores reales, sin instrumentación añadida-- dice
exactamente qué le piden:

|observación|valor|
|---|---|
|procesos `serve` arrancados|`51`|
|procesos que hicieron **cero** llamadas|**`48` de `51`**|
|llamadas de tool en total|`5`|
|llamadas por proceso que sí llamó|`3`, `1`, `1`|
|mezcla|`find_references` `80 %`, `find_symbol` `20 %`|

**Cuarenta y ocho de cincuenta y un servidores cargaron el grafo entero y nadie
les preguntó nada.** La mediana de una sesión real es **una** llamada; el máximo
observado, tres. El benchmark medía `2.000`, que son tres órdenes de magnitud por
encima de lo que ocurre.

Eso no es un detalle de calibración: es la variable que decidía el resultado. La
penalización de HTTP que la versión anterior publicó -- `12,5 MB` por cliente-- era
el buffer de reanudación del SDK llenándose con respuestas que una sesión real
nunca produce.

## Entorno y procedencia

|dato|valor|
|---|---|
|fecha|2026-08-23|
|commit|`f2333a9`|
|plataforma|VM `linux/arm64` de Docker Desktop, imagen `golang:1.26-trixie`|
|kernel|`Linux 6.12.54-linuxkit`, `10` CPU, page size `4096`|
|corpus|`kena`, `37` repositorios, **`117.499` símbolos**|
|snapshot|`86,7 MB`, generación `000001`|
|esquema|`daemon-cost-v2`|
|digest socket real|`41b87caa6a47`|
|digest http real|`7e072cf239a6`|
|digest http sostenida|`534a650f77f6`|
|carga real|`1` llamada por cliente, `0` descartadas|
|carga sostenida|`2.000` llamadas, `4.000` descartadas|
|semilla|`42`|

**Las cuatro corridas de este informe comparten corpus y generación.** Es una
condición, no un detalle: las cifras de la versión anterior salieron de `108.737`
símbolos y cruzarlas con éstas sería exactamente el error que este mismo informe
prohíbe más abajo. Los digests son distintos entre sí porque el transporte y los
recuentos entran en la identidad de la corrida.

El esquema subió a `v2` cuando el transporte entró en esa identidad. Un fichero
`v1` calculó su digest sin él, así que no se puede comparar por digest con uno de
éstos: `v1` nombraba un experimento que podía haber sido cualquiera de las dos
puertas.

La generación se publicó en el host (darwin/arm64, donde hay `node 25` y el
TypeScript de `kena` carga entero) y **se lee** en Linux. Los dos brazos leen ese
mismo fichero, byte a byte: ninguno deriva, así que LadybugDB no participa. El
workspace se monta en sólo lectura, que es lo que hace imposible tocar un
repositorio indexado.

## La respuesta

A la carga que un editor produce de verdad, tres pasadas por puerta:

|puerta|pendiente del demonio|pendiente de N procesos|1 cliente|8 clientes|cruce|
|---|---|---|---|---|---|
|socket|`1,1`–`1,6` MB/cli|`42,8`–`43,0` MB/cli|`57` vs `57` MB|`66`–`68` vs `355`–`357` MB|`1,06`–`1,10`|
|HTTP|`1,0`–`1,3` MB/cli|`42,6`–`43,5` MB/cli|`57` vs `54`–`57` MB|`66`–`68` vs `354`–`361` MB|`1,06`–`1,10`|

**Las dos puertas son indistinguibles.** `1,0`–`1,3` contra `1,1`–`1,6 MB` por
cliente, con los rangos solapados: a esta carga el transporte no se paga. Eso
**retira la advertencia** que la versión anterior publicaba -- que HTTP costaba
`12,5 MB` por cliente y que a un cliente el demonio perdía.

A ocho clientes el demonio cuesta **la quinta parte**: `67` contra `356 MB`. Y el
cruce está en `1,06`–`1,10` clientes por las dos puertas, así que gana desde el
segundo cliente y a uno empata.

### Y N procesos cuestan menos de lo que se publicó

`42,6`–`43,5 MB` por cliente, no `66`–`67`. La razón es la misma variable: el
snapshot se mapea y sus páginas se ensucian **al consultarse**. Un `serve` al que
nadie pregunta -- que son `48` de cada `51`-- nunca toca la mayor parte del fichero,
así que su mitad privada se queda en `43 MB` en vez de `67`.

Los dos brazos bajan, y el del demonio baja más: la proporción a ocho clientes
pasa de `0,25` a `0,19`. Medir la carga equivocada **subestimaba** el ahorro.

## El techo, y por qué es un techo

`results-http-sustained.json` es la misma pregunta con `2.000` llamadas medidas y
`4.000` descartadas, sobre este mismo corpus y tres pasadas:

|carga|puerta|pendiente del demonio|8 clientes|cruce|
|---|---|---|---|---|
|real|socket|`1,1`–`1,6` MB/cli|`66` vs `356` MB|`1,06`–`1,10`|
|real|HTTP|`1,0`–`1,3` MB/cli|`66` vs `354` MB|`1,06`–`1,10`|
|sostenida|HTTP|`4,9`–`5,9` MB/cli|`136`–`141` vs `561`–`569` MB|`1,48`–`1,52`|

Bajo tráfico sostenido la pendiente por HTTP se multiplica por cinco y el cruce
se mueve a `1,5` clientes. **El ahorro sigue existiendo** -- `138` contra `565 MB` a
ocho clientes, la cuarta parte-- pero el coste por sesión es real y conviene saber
de dónde sale.

### De dónde sale, verificado en la fuente

No es el grafo: es **tráfico retenido**. El SDK da a cada sesión su propio
`MemoryEventStore` para poder reanudar un stream cortado, y ese store guarda
hasta `10 MiB` por defecto -- `NewMemoryEventStore(nil)` en `streamable.go:391`,
`defaultMaxBytes = 10 << 20` en `event.go:255`. Con `2.000` llamadas cada sesión
empuja respuestas del grafo hacia ese techo; con una, no.

Y la cifra sostenida **depende del corpus**, que es la firma de un coste medido en
bytes y no en sesiones: `4,9`–`5,9 MB` por cliente sobre `117.499` símbolos, y
`12,1`–`12,8` sobre los `108.737` de la corrida anterior. Un coste estructural por
sesión no haría eso.

**No se puede acotar desde aquí.** `NewStreamableHTTPHandler` no acepta un
`EventStore`: el campo existe en `StreamableServerTransport`, pero el handler
construye el transporte por su cuenta y `StreamableHTTPOptions` sólo expone
`Stateless` y `JSONResponse` (`streamable.go:53-71`, con un `TODO(#148)` sobre
retención de sesión justo en medio). Las salidas serían reimplementar el
enrutado de sesiones del SDK o servir en modo `Stateless`, que prohíbe la GET
colgada y las peticiones del servidor al cliente. Ninguna se tomó: la primera
duplica la capa HTTP del SDK y la segunda cambia el contrato del transporte para
acotar memoria que ya está acotada y que ninguna sesión real alcanza.

## Las tres pasadas del socket bajo carga sostenida

Están en el historial, en el commit `56c49d7`, sobre el corpus de `108.737`
símbolos: `0,2`–`2,3 MB` por cliente, `533 MB` contra `68`–`82` a ocho clientes.
No se reproducen aquí porque cruzarlas con las de arriba sería comparar corpus
distintos.


## Lo que la medición añade y no se buscaba

**El pico casi no baja con la carga real, y sigue siendo la diferencia más
grande de todo el benchmark.** Cifras de la corrida por HTTP; la del socket
coincide dentro de un megabyte:

|clientes|pico N procesos|pico 1 demonio|
|---|---|---|
|`1`|`157` MB|`160` MB|
|`2`|`293` MB|`167` MB|
|`4`|`576` MB|`165` MB|
|`8`|**`1.152` MB**|**`169` MB**|

Una máquina que arranca ocho editores a la vez paga **más de un gigabyte** de
pico contra `169 MB`. Y esto es a la carga real: el pico de ocho procesos son
ocho cargas simultáneas, no ocho consultas. Es el sitio donde el demonio gana por
más, y no depende de que nadie pregunte nada.

**Un cliente nuevo se responde entre `5` y `12` veces antes:**

|clientes ya conectados|N procesos|1 demonio|
|---|---|---|
|`1`|`126`–`143` ms|`23`–`25` ms|
|`2`|`145`–`155` ms|`17` ms|
|`4`|`148`–`179` ms|`18`–`20` ms|
|`8`|`202`–`205` ms|`16` ms|

Es lo que ve quien abre una segunda ventana del editor: un proceso nuevo tiene
que cargar el snapshot, una sesión nueva no. Y con la carga real la ventaja del
demonio **crece** con los clientes mientras la de los procesos empeora, porque
ocho arranques simultáneos compiten por las mismas diez CPU.

**La latencia empata**, y aquí sí cambia la conclusión anterior: `p99` entre
`1,2` y `1,9 ms` en los dos brazos, contra los `13`–`20 ms` que se medían bajo
carga sostenida. Con una llamada por cliente no hay contención que repartir, así
que la ventaja de latencia que el demonio mostraba a ocho clientes **es un efecto
de la carga sintética** y no algo que un usuario vea.

## Limitaciones

* **La forma de la sesión es real; su ritmo no.** Las llamadas por sesión salen
  de un log de uso real, pero se emiten seguidas y desde sondas del propio
  snapshot. Un editor intercala pausas de minutos, y `48` de cada `51` sesiones
  no preguntan nada en absoluto -- ésas están *mejor* representadas por el brazo
  del demonio de lo que este arnés puede medir, porque el arnés obliga a cada
  cliente a hacer al menos una llamada.
* **La mezcla de tools no es la observada.** El log real es `80 %`
  `find_references`; el arnés reparte entre las sondas que cosecha del snapshot.
  Importa porque el coste sostenido se mide en bytes de respuesta, y dos tools no
  devuelven lo mismo.
* **El log real son dos días de una máquina.** `51` procesos y `5` llamadas es una
  muestra pequeña y de un solo usuario. Lo que sostiene es el orden de magnitud
  -- unidades de llamadas por sesión, no miles-- y no una distribución.
* **La pendiente bajo carga sostenida depende del corpus**, y eso está medido:
  `4,9`–`5,9 MB` por cliente sobre `117.499` símbolos contra `12,1`–`12,8` sobre
  `108.737`. Es coherente con un coste en bytes retenidos, y significa que la
  cifra del techo no se transporta entre corpus.
* **No es bare metal.** Es la VM de Docker Desktop sobre Apple Silicon. El
  `page size` es `4096`, el mismo que amd64, que es lo que hace comparables las
  unidades; el resto del entorno es el de un contenedor.
* **La generación se publicó en darwin y se leyó en Linux.** Mismo arco, formato
  de anchura fija y little-endian por diseño. Es lo que permite que los dos
  brazos lean el mismo fichero, y es una dependencia declarada del método.
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

## Y dos defectos de producción

`Listen` fijaba el modo del socket con `chmod` **después** de crearlo, y `chmod`
sobre un socket devuelve `EINVAL` en un bind mount de virtiofs -- que es
exactamente donde corre este benchmark. El demonio no arrancaba. Ahora el socket
nace con el modo puesto, vía `umask`, y no hay `chmod` que pueda fallar.

De paso quedó dicho qué compra ese modo, que es distinto por plataforma: Linux
comprueba permiso de escritura sobre el socket al conectar, así que `0600` es una
puerta real; darwin **ignora** los permisos del socket para conectar, y allí la
puerta es el directorio de estado, que hay que atravesar para llegar a la ruta.

El segundo lo destapó el brazo HTTP, fallando: **un socket unix acepta en cuanto
está enlazado**, antes de que nadie llame a `Accept`, así que el arnés -- que
trataba un `dial` con éxito como señal de arranque-- alcanzaba a un demonio cuyo
`daemon.json` todavía no existía. Una de cada tres pasadas moría con «no such
file or directory» sobre un demonio que estaba perfectamente vivo.

Dos arreglos, porque son dos problemas. En producción el demonio publica HTTP
**antes** de enlazar el socket, así que alcanzar el socket implica que el endpoint
está; y si el `bind` falla después, el endpoint se retira, porque un fichero que
afirma que hay un demonio contestando manda al cliente siguiente a un puerto
cerrado. En el arnés, la espera del endpoint es explícita en vez de deducida del
orden: un arnés que dependiera de ese orden se rompería en silencio el día que
cambiara, y éste ya se rompió una vez con el orden contrario.

## Reproducir

```bash
# En el host, donde hay node: publicar la generación.
export HOME=/ruta/aislada
kivgraph init --languages go,typescript,rust --repository <nombre>=<ruta> ...
kivgraph index --full

# En Linux, las dos puertas a la carga real: una llamada por cliente, la mediana
# medida de una sesión. El cwd tiene que ser el checkout, o el commit no se lee y
# la corrida lo declara como limitación.
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
        -clients 1,2,4,8 -calls 8 -warmup 0 \
        -transport $t -output /out/real-$t
    done'
```

El techo es la misma orden con la carga sintética, y la diferencia entre las dos
filas de la tabla es **sólo** ese par de números:

```bash
/out/daemon-cost ... -clients 1,2,4,8 -calls 2000 -warmup 4000 \
  -transport http -output /out/sostenida
```

Y la carga real se recuenta de un log de uso, no se estima:

```bash
python3 - <<'EOF'
import json, collections, os
pids, calls = set(), collections.Counter()
for name in ('events.jsonl.1', 'events.jsonl'):
    path = os.path.expanduser(f'~/.local/state/kivgraph/{name}')
    if not os.path.exists(path):
        continue
    for line in open(path, errors='replace'):
        try:
            event = json.loads(line)
        except ValueError:
            continue
        if event.get('kind') == 'serve' and 'started' in event.get('msg', ''):
            pids.add(event.get('pid'))
        if event.get('kind') == 'tool' and event.get('tool'):
            calls[event.get('pid')] += 1
print(f'{len(pids)} servidores, {sum(calls.values())} llamadas, '
      f'{len(pids) - len(calls)} sin ninguna')
EOF
```

El `digest` es la identidad de las entradas -- corpus, generación, recuentos,
semilla-- y no de las medidas: dos corridas del mismo experimento comparten
cadena, y una cifra distinta no puede pasar por el mismo experimento.
