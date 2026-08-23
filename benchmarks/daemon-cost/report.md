# Lo que un proceso para muchos clientes cuesta de verdad

`LUQUE-2222`, corregido en `LUQUE-2223`, otra vez en `LUQUE-2224` y ampliado en
`LUQUE-2225`. `kivgraph daemon` se escribió sobre una aritmética: si un `serve`
conserva `71 MB` de páginas privadas y esa cifra es plana en el número de
clientes, N de ellos deberían convertirse en uno. Esto lo mide.

Se ha corregido dos veces, y las dos por el mismo motivo: **la cifra publicada
describía una situación que no era la del usuario.** Primero la del socket unix,
que ningún cliente MCP puede marcar. Luego la de HTTP bajo `2.000` llamadas por
sesión, que ninguna sesión real hace. Esta versión añade el extremo opuesto, que
es el caso que **de verdad predomina**: el servidor al que nadie pregunta nada.

|artefacto|puerta|carga|
|---|---|---|
|`results-idle.json`|socket unix|ninguna llamada|
|`results-http-idle.json`|Streamable HTTP|ninguna llamada|
|`results.json`|socket unix|`8` llamadas por brazo|
|`results-http.json`|Streamable HTTP|`8` llamadas por brazo|
|`results-http-sustained.json`|Streamable HTTP|`2.000` llamadas: el techo|

Este informe no emite ningún veredicto de aceptación: mide dos formas de servir
el mismo grafo, por dos transportes y a tres cargas, en un entorno concreto.

## Lo que cuesta un servidor al que nadie pregunta

`33,1`–`34,4 MB` de páginas privadas por cliente. Sin haber contestado nada.

|carga|pendiente de N procesos|pendiente del demonio|proporción|
|---|---|---|---|
|ninguna llamada|`33,1`–`34,4` MB/cli|`0,8`–`1,2` MB/cli|`0,023`–`0,032`|
|`8` llamadas|`39,9`–`40,7` MB/cli|`0,4`–`0,9` MB/cli|`0,009`–`0,013`|
|`2.000` llamadas|`67,6` MB/cli|`12,8` MB/cli|`0,189`|

$$\frac{33}{40} = 83\,\%\ \text{de lo que cuesta un servidor consultado, y}\ \frac{33}{68} = 49\,\%\ \text{del techo}$$

**El arranque es el coste.** Contestar una pregunta añade unos `7 MB` sobre los
`33` que ya se pagaron al abrir el proceso, y hacen falta `2.000` llamadas para
que la carga iguale a lo que costó estar listo. Esto importa porque `48` de cada
`51` servidores reales **no reciben ninguna llamada**: la fila de arriba no es un
extremo teórico, es la mediana de lo que ocurre.

En el brazo del demonio esa cifra es `1 MB` por cliente, que está en el ruido de
este método. La lectura honesta no es «el demonio ahorra el 97 %», es que **el
coste por cliente de un demonio es indistinguible de cero hasta que llega tráfico
sostenido**, y sólo ahí se vuelve medible (`12,8`).

A ocho clientes ociosos: `263`–`273 MB` contra `40 MB`. Un editor solo empata
(`31` contra `31`–`34`), y el cruce está en `1,04`–`1,07` clientes.

Y la corrida ociosa es, además, **la única medición limpia del benchmark**: es la
única en la que los cuatro puntos del barrido miden lo mismo por cliente. Con
`-calls 8` el brazo reparte ocho llamadas entre los clientes que haya, así que la
fila de un cliente contesta ocho preguntas y la de ocho contesta una cada uno; sin
llamadas no hay nada que repartir.

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
les preguntó nada.** La mediana de una sesión real es **cero** llamadas; el
máximo observado, tres. El benchmark medía `2.000`, tres órdenes de magnitud por
encima de lo que ocurre.

Eso no es un detalle de calibración: era la variable que decidía el resultado. La
penalización de HTTP que una versión anterior publicó -- `12,5 MB` por cliente-- era
el buffer de reanudación del SDK llenándose con respuestas que una sesión real
nunca produce.

## Entorno y procedencia

|dato|valor|
|---|---|
|fecha|2026-08-23|
|commit|`ef31f35`|
|plataforma|VM `linux/arm64` de Docker Desktop, imagen `golang:1.26-trixie`|
|kernel|`Linux 6.12.54-linuxkit`, `10` CPU, page size `4096`|
|corpus|`kena`, `37` repositorios, **`108.737` símbolos**|
|snapshot|`77,6 MB`, generación `000001`|
|esquema|`daemon-cost-v3`|
|digest ocioso socket|`043a0f42bcdc`|
|digest ocioso http|`45f78ccf76e1`|
|digest socket `8` llamadas|`84e07f9513b6`|
|digest http `8` llamadas|`122cf7c27892`|
|digest http sostenida|`b96fc53295d0`|
|semilla|`42`|

**Las cinco corridas comparten corpus y generación.** Es una condición, no un
detalle: cruzar una cifra por símbolo entre corpus es el error que este mismo
informe prohíbe más abajo. Las cifras de la versión anterior salieron de `117.499`
símbolos y **no se reproducen aquí**; están en el historial.

El esquema subió a `v3` porque **el punto de medición se movió**. Hasta ahora el
guardia de generación -- la llamada a `graph_status` que prueba que los dos brazos
sirven el mismo fichero-- corría *antes* de leer la memoria, así que cada brazo
había contestado una pregunta antes de ser medido. Con cargas de miles de llamadas
eso era invisible; con una carga de cero **es la carga entera**. Ahora el guardia
corre después del muestreo: falla igual y descarta la corrida igual, por un camino
que no contamina lo que vigila. Un fichero `v2` incluye esa llamada en sus bytes,
así que no es la misma medición.

La generación se publicó en el host (darwin/arm64, donde hay `node 25` y el
TypeScript de `kena` carga entero) y **se lee** en Linux. Los dos brazos leen ese
mismo fichero, byte a byte: ninguno deriva, así que LadybugDB no participa. El
workspace se monta en sólo lectura, que es lo que hace imposible tocar un
repositorio indexado.

## Las dos puertas cuestan lo mismo, y también sin carga

|puerta|carga|pendiente del demonio|pendiente de N procesos|8 clientes|cruce|
|---|---|---|---|---|---|
|socket|ninguna|`1,1`–`1,2` MB/cli|`33,1`–`34,4` MB/cli|`40`–`42` vs `263`–`273` MB|`1,04`|
|HTTP|ninguna|`0,8`–`1,0` MB/cli|`33,4`–`33,7` MB/cli|`40` vs `265`–`267` MB|`1,07`|
|socket|`8`|`0,5`–`0,9` MB/cli|`39,9`–`40,6` MB/cli|`60`–`62` vs `334`–`337` MB|`1,01`–`1,06`|
|HTTP|`8`|`0,4`–`0,5` MB/cli|`40,0`–`40,7` MB/cli|`60`–`62` vs `337` MB|`1,06`–`1,11`|

Los rangos se solapan en las dos cargas: **elegir HTTP no se paga**, y HTTP es la
única puerta que un cliente MCP puede configurar. Eso confirma sin carga lo que
`LUQUE-2224` midió con ella, y retira definitivamente la advertencia de que a un
cliente el demonio perdía por HTTP.

## El techo, y por qué es un techo

`results-http-sustained.json` es la misma pregunta con `2.000` llamadas medidas y
`4.000` descartadas:

|carga|pendiente del demonio|8 clientes|cruce|
|---|---|---|---|
|ninguna|`0,8`–`1,2` MB/cli|`40` vs `265` MB|`1,04`–`1,07`|
|`8` llamadas|`0,4`–`0,9` MB/cli|`61` vs `336` MB|`1,01`–`1,11`|
|`2.000` llamadas|`12,8` MB/cli|`168` vs `540` MB|`1,31`|

**El ahorro sigue existiendo** -- `168` contra `540 MB`, la tercera parte-- pero el
coste por sesión se vuelve real y conviene saber de dónde sale.

### De dónde sale, verificado en la fuente

No es el grafo: es **tráfico retenido**. El SDK da a cada sesión su propio
`MemoryEventStore` para poder reanudar un stream cortado, y ese store guarda
hasta `10 MiB` por defecto -- `NewMemoryEventStore(nil)` en `streamable.go:391`,
`defaultMaxBytes = 10 << 20` en `event.go:255`. Con `2.000` llamadas cada sesión
empuja respuestas del grafo hacia ese techo; con una, no; con ninguna, tampoco
existe la sesión que las pediría.

Y la cifra sostenida **depende del corpus**, que es la firma de un coste medido en
bytes y no en sesiones: `4,9`–`5,9 MB` por cliente sobre `117.499` símbolos, y
`12,8` aquí sobre `108.737` -- que reproduce los `12,1`–`12,8` medidos en un corpus
del mismo tamaño en el commit `56c49d7`. Un coste estructural por sesión no haría
eso.

**No se puede acotar desde aquí.** `NewStreamableHTTPHandler` no acepta un
`EventStore`: el campo existe en `StreamableServerTransport`, pero el handler
construye el transporte por su cuenta y `StreamableHTTPOptions` sólo expone
`Stateless` y `JSONResponse` (`streamable.go:53-71`, con un `TODO(#148)` sobre
retención de sesión justo en medio). Las salidas serían reimplementar el
enrutado de sesiones del SDK o servir en modo `Stateless`, que prohíbe la GET
colgada y las peticiones del servidor al cliente. Ninguna se tomó: la primera
duplica la capa HTTP del SDK y la segunda cambia el contrato del transporte para
acotar memoria que ya está acotada y que ninguna sesión real alcanza.

## Lo que la medición añade y no se buscaba

**El pico es la diferencia más grande de todo el benchmark, y no depende de que
nadie pregunte nada.** Sin ninguna llamada:

|clientes|pico N procesos|pico 1 demonio|
|---|---|---|
|`1`|`124`–`126` MB|`125`–`128` MB|
|`2`|`250`–`251` MB|`128`–`130` MB|
|`4`|`497`–`503` MB|`129`–`132` MB|
|`8`|**`994`–`1.000` MB**|**`134`–`135` MB**|

Una máquina que arranca ocho editores a la vez paga **un gigabyte** de pico contra
`134 MB`, y ninguno de esos ocho ha hecho una sola consulta. Con `8` llamadas la
cifra sube a `1.053`–`1.057` contra `154`–`156`: el pico es el arranque, no el
trabajo.

**Un cliente nuevo se conecta unas cien veces antes**, y esto también es sin
carga:

|clientes ya conectados|N procesos|1 demonio|
|---|---|---|
|`1`|`96`–`107` ms|`1,4`–`1,7` ms|
|`8`|`130`–`151` ms|`1,5`–`1,6` ms|

Es lo que ve quien abre una segunda ventana del editor: un proceso nuevo tiene que
cargar el snapshot, una sesión nueva no. La comparación es de conexión contra
conexión -- `new_client_connect_ms`, que se mide a todas las cargas-- y no mezcla la
espera de una respuesta, que a carga cero no existe.

**La latencia empata** a la carga real: `p99` entre `4` y `17 ms` en el brazo de
procesos y entre `4` y `29` en el del demonio, rangos que se solapan y se cruzan.
La ventaja de latencia que el demonio mostraba bajo carga sostenida es **un efecto
de la carga sintética**.

## Limitaciones

* **La corrida ociosa mide un arranque, no una sesión de trabajo.** Dice lo que
  cuesta estar listo, que es lo que `48` de `51` servidores reales hacen y nada
  más; no dice nada sobre lo que cuesta contestar.
* **De los `33 MB` no se sabe qué parte es qué.** Son páginas privadas que existen
  antes de la primera pregunta; este benchmark no separa la construcción de
  índices del resto del arranque, y afirmar cuál domina sería inventar el
  mecanismo. El fichero mapeado no está ahí: son `6,1 MB` por proceso de
  `shared_clean`, sobre un snapshot de `77,6`.
* **La forma de la sesión es real; su ritmo no.** Las llamadas por sesión salen
  de un log de uso real, pero se emiten seguidas y desde sondas del propio
  snapshot. Un editor intercala pausas de minutos.
* **`-calls 8` no es «una llamada por cliente» en todo el barrido.** El brazo
  reparte ocho llamadas entre los clientes que haya: a ocho clientes es una cada
  uno, a uno son ocho. La fila de un cliente de esa carga no es comparable con la
  de ocho.
* **La mezcla de tools no es la observada.** El log real es `80 %`
  `find_references`; el arnés reparte entre las sondas que cosecha del snapshot.
  Importa porque el coste sostenido se mide en bytes de respuesta.
* **El log real son dos días de una máquina.** `51` procesos y `5` llamadas es una
  muestra pequeña y de un solo usuario. Sostiene el orden de magnitud -- unidades
  de llamadas por sesión, no miles-- y no una distribución.
* **La carga sostenida es una sola pasada** por HTTP, y su pendiente depende del
  corpus: la cifra del techo no se transporta entre corpus.
* **No es bare metal.** Es la VM de Docker Desktop sobre Apple Silicon. El
  `page size` es `4096`, el mismo que amd64, que es lo que hace comparables las
  unidades; el resto del entorno es el de un contenedor.
* **La generación se publicó en darwin y se leyó en Linux.** Mismo arco, formato
  de anchura fija y little-endian por diseño. Es una dependencia declarada del
  método.
* **No se midió por encima de ocho clientes.** La pendiente medida extrapola a
  que el demonio siga plano, y una extrapolación no es una medición.

## Defectos del arnés que estas pasadas destaparon

El de esta pasada es el que impedía la medición: **el guardia de generación
contestaba antes de muestrear**, así que ningún brazo podía estar realmente
ocioso. Movido detrás del muestreo, el guardia conserva su fuerza -- una corrida
cuyo servidor sirve otra generación se descarta igual-- y deja de ser carga.

Y una trampa que iba con él: `latencyOf` sin llamadas devolvía `latency{}`, así
que un fichero ocioso habría publicado `p50_ms: 0`. Un cero ahí se lee como una
respuesta instantánea, y `p99_ratio: 0` como un demonio infinitamente más rápido.
Los percentiles, el `new_client_ms` y los dos ratios de latencia son ahora
punteros: **lo que no se midió no aparece en el fichero**, y el resumen imprime
`--`. Es el mismo modo de fallo que este benchmark ya cometió dos veces por otra
vía: publicar un número que un lector toma por lo que no es.

Queda una comprobación que ningún test de portátil puede hacer. Los probes que
`startServer` y `connect` se saltan bajo carga cero necesitan un servidor real, y
borrar ese salto no rompe nada que corra en local -- lo verifiqué. Por eso la
corrida se niega a publicar un fichero ocioso que haya cronometrado algún primer
answer (`checkIdle`), y eso sí está defendido por un test.

De pasadas anteriores: la primera versión publicó `symbols: 0` sin fallar porque
`readStatus` adivinó la forma de `graph_status`; y `commit` se rellenaba dentro de
`writeResults`, después de calcular las limitaciones, así que un commit ausente
nunca podía declararse.

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
orden.

## Reproducir

```bash
# En el host, donde hay node: publicar la generación.
export HOME=/ruta/aislada
kivgraph init --languages go,typescript,rust --repository <nombre>=<ruta> ...
kivgraph index --full

# En Linux, las tres cargas por las dos puertas. El cwd tiene que ser el
# checkout, o el commit no se lee y la corrida lo declara como limitación.
docker run --rm -w /src \
  -v "$PWD":/src:ro -v /ruta/a/kena:/ruta/a/kena:ro -v "$HOME":"$HOME" \
  -e HOME="$HOME" golang:1.26-trixie bash -c '
    git config --global --add safe.directory /src
    go build -tags ladybug -o /out/kivgraph ./cmd/kivgraph
    go build -o /out/daemon-cost ./benchmarks/daemon-cost
    for t in socket http; do
      for c in 0 8; do
        /out/daemon-cost -server /out/kivgraph \
          -config "$HOME/.config/kivgraph/config.yaml" \
          -generation-dir "$HOME/.local/state/kivgraph/generations/000001" \
          -state-dir "$HOME/.local/state/kivgraph" \
          -clients 1,2,4,8 -calls $c -warmup 0 \
          -transport $t -output /out/$t-$c
      done
    done'
```

`-calls 0` es la corrida ociosa: los clientes se conectan, negocian la sesión y no
preguntan nada. El techo es la misma orden con `-calls 2000 -warmup 4000`.

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
semilla, transporte-- y no de las medidas: dos corridas del mismo experimento
comparten cadena, y una cifra distinta no puede pasar por el mismo experimento.
