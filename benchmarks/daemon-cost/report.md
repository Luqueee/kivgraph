# Lo que un proceso para muchos clientes cuesta de verdad

`LUQUE-2222`, corregido en `LUQUE-2223`, otra vez en `LUQUE-2224`, ampliado en
`LUQUE-2225` y **cobrado** en `LUQUE-2226`. `kivgraph daemon` se escribió sobre
una aritmética: si un `serve` conserva `71 MB` de páginas privadas y esa cifra es
plana en el número de clientes, N de ellos deberían convertirse en uno. Esto lo
mide.

Se corrigió dos veces, y las dos por el mismo motivo: **la cifra publicada
describía una situación que no era la del usuario.** Primero la del socket unix,
que ningún cliente MCP puede marcar. Luego la de HTTP bajo `2.000` llamadas por
sesión, que ninguna sesión real hace. Después se midió el extremo opuesto -- el
servidor al que nadie pregunta, que es `48` de cada `51`-- y ahí el benchmark dejó
de describir un reparto y encontró un defecto: **el arranque era casi todo el
coste, y se pagaba siempre**. El ADR 0067 lo arregló, y este informe mide el antes
y el después.

|artefacto|puerta|carga|
|---|---|---|
|`results-idle.json`|socket unix|ninguna llamada|
|`results-http-idle.json`|Streamable HTTP|ninguna llamada|
|`results.json`|socket unix|`8` llamadas por brazo|
|`results-http.json`|Streamable HTTP|`8` llamadas por brazo|
|`results-http-sustained.json`|Streamable HTTP|`2.000` llamadas: el techo|

Este informe no emite ningún veredicto de aceptación: mide dos formas de servir
el mismo grafo, por dos transportes y a tres cargas, en un entorno concreto.

## Lo que costaba un servidor al que nadie pregunta, y lo que cuesta ahora

Esta medición encontró que el arranque **era** el coste, y eso se arregló: el
grafo lo lee ahora la primera consulta que lo necesita, no el arranque
(ADR 0067). Las dos columnas son la misma pregunta antes y después.

|ocioso, sin ninguna llamada|antes|ahora|
|---|---|---|
|pendiente de N procesos|`33,9` MB/cli|`9,8`–`11,3` MB/cli|
|un cliente|`31,2 MB`|`7,1`–`9,2 MB`|
|ocho clientes|`268,6 MB`|`75,9`–`85,7 MB`|
|pico a ocho clientes|`994,3 MB`|`181,6`–`187,0 MB`|
|demonio a ocho clientes|`40,4 MB`|`10,4`–`10,8 MB`|

Las cifras «antes» se midieron en el commit `d990a6c` sobre esta misma
generación; están en el historial y no en los artefactos, porque un artefacto
describe el código que lo produjo.

**Qué lo hacía caro.** El fichero del snapshot nunca fue el problema: se mapea,
sus páginas están limpias y compartidas -- `6,1 MB` por proceso sobre un fichero
de `77,6`. Lo privado son los índices que el mapeo deriva, y un servidor que
nunca contesta no necesita ninguno.

|carga|pendiente de N procesos|pendiente del demonio|proporción|
|---|---|---|---|
|ninguna llamada|`9,8`–`11,3` MB/cli|`-0,2`–`0,2` MB/cli|indistinguible de cero|
|`8` llamadas|`38,9`–`40,1` MB/cli|`0,4`–`1,0` MB/cli|`0,010`–`0,025`|
|`2.000` llamadas|`66,9` MB/cli|`10,5` MB/cli|`0,157`|

**Con carga la cifra no se movió**, que es lo que hace creíble el ahorro:
`38,9`–`40,1` contra los `39,9` de antes, y `66,9` contra `67,6` en el caso
sostenido. Lo que desapareció es exactamente lo que las sesiones que no preguntan
estaban pagando.

La pendiente ociosa del demonio sale **negativa por unas décimas de megabyte**.
No significa que un cliente devuelva memoria: significa que a esta carga su coste
por cliente está por debajo de lo que este método resuelve. Se publica el ajuste
crudo en vez de recortarlo a cero, porque recortarlo escondería justo eso.

A ocho clientes ociosos: `76`–`86 MB` contra `10,4`–`10,8`. Un editor solo ya no
empata -- `7,1`–`9,2` contra `10,3`–`10,8`-- así que el cruce se mueve a
`1,26`–`1,54` clientes: los dos brazos son ahora tan baratos en reposo que el
proceso del demonio pesa relativamente más. Gana desde el segundo.

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
|commit|`638c6de` (antes) y el de este árbol (ahora)|
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

Y comparten algo más: **el mismo código**. Las columnas «antes» y «ahora» de la
primera tabla son dos códigos distintos sobre esta misma generación, y por eso son
lo único de este informe que cruza esa frontera. Todo lo demás describe el árbol
actual, con la carga diferida del ADR 0067.

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
|socket|ninguna|`-0,1`–`0,2` MB/cli|`10,2`–`10,5` MB/cli|`10`–`11` vs `78`–`80` MB|`1,26`–`1,48`|
|HTTP|ninguna|`-0,2` MB/cli|`9,8`–`11,3` MB/cli|`11` vs `76`–`86` MB|`1,41`–`1,54`|
|socket|`8`|`0,9`–`1,4` MB/cli|`38,9`–`40,1` MB/cli|`60`–`62` vs `325`–`332` MB|`0,96`–`1,08`|
|HTTP|`8`|`0,6`–`1,0` MB/cli|`39,2`–`39,7` MB/cli|`62` vs `329`–`331` MB|`1,03`–`1,10`|

Los rangos se solapan en las dos cargas: **elegir HTTP no se paga**, y HTTP es la
única puerta que un cliente MCP puede configurar. Eso confirma sin carga lo que
`LUQUE-2224` midió con ella, y retira definitivamente la advertencia de que a un
cliente el demonio perdía por HTTP.

## El techo, y por qué es un techo

`results-http-sustained.json` es la misma pregunta con `2.000` llamadas medidas y
`4.000` descartadas:

|carga|pendiente del demonio|8 clientes|cruce|
|---|---|---|---|
|ninguna|`-0,2`–`0,2` MB/cli|`11` vs `76`–`86` MB|`1,26`–`1,54`|
|`8` llamadas|`0,6`–`1,4` MB/cli|`61` vs `328` MB|`0,96`–`1,10`|
|`2.000` llamadas|`10,5` MB/cli|`155` vs `536` MB|`1,38`|

**El ahorro sigue existiendo** -- `155` contra `536 MB`, la tercera parte-- pero el
coste por sesión se vuelve real y conviene saber de dónde sale. Y es el único caso
que la carga diferida no mejora, por construcción: dos mil llamadas mapean el
grafo igual, así que la pendiente de procesos apenas cambió (`66,9` contra `67,6`).

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

**El pico sigue siendo la diferencia más grande, y ya no es un gigabyte.** Sin
ninguna llamada:

|clientes|pico N procesos|pico 1 demonio|
|---|---|---|
|`1`|`22`–`24` MB|`26`–`28` MB|
|`2`|`44`–`46` MB|`25`–`31` MB|
|`4`|`92`–`96` MB|`26`–`28` MB|
|`8`|**`182`–`187` MB**|**`26`–`27` MB**|

Ocho editores arrancando a la vez pagaban `994 MB` y ahora pagan `184`: la carga
diferida se lleva el pico junto con el resto, porque el pico *era* el mapeo. Con
`8` llamadas vuelve a `1.043`–`1.050` contra `154`–`156`, que es la prueba de que
lo que se movió es el momento y no la cifra.

**Y un cliente nuevo se conecta antes que antes de este cambio.** Arrancar un
proceso ya no mapea nada:

|clientes ya conectados|N procesos|antes|1 demonio|
|---|---|---|---|
|`1`|`13`–`18` ms|`96`–`107` ms|`1,6`–`2,1` ms|
|`8`|`23`–`45` ms|`130`–`151` ms|`1,5`–`1,7` ms|

Es lo que ve quien abre una segunda ventana del editor. El demonio sigue ganando
por un orden de magnitud, pero la brecha se cerró de `70x` a unos `20x`, y esa
parte del ahorro **ya no hace falta instalar nada** para tenerla. La comparación es
de conexión contra conexión -- `new_client_connect_ms`, que se mide a todas las
cargas-- y no incluye la primera respuesta, que en un servidor diferido paga el
mapeo.

**La latencia empata** a la carga real: `p99` entre `4` y `17 ms` en el brazo de
procesos y entre `4` y `29` en el del demonio, rangos que se solapan y se cruzan.
La ventaja de latencia que el demonio mostraba bajo carga sostenida es **un efecto
de la carga sintética**.

## Limitaciones

* **La corrida ociosa mide un arranque, no una sesión de trabajo.** Dice lo que
  cuesta estar listo, que es lo que `48` de `51` servidores reales hacen y nada
  más; no dice nada sobre lo que cuesta contestar.
* **La primera consulta de un servidor diferido paga el mapeo, y eso no está
  medido como latencia aislada.** Las tablas de conexión comparan conexiones. Lo
  que sí se midió es que ese mapeo no desapareció: con `8` llamadas las cifras
  vuelven a las de antes.
* **La pendiente ociosa del demonio sale negativa por décimas de megabyte.** Está
  por debajo de lo que este método resuelve, así que la lectura es «indistinguible
  de cero», no «devuelve memoria». Se publica el ajuste crudo.
* **De los `10 MB` que quedan en un servidor ocioso no se sabe qué parte es qué.**
  Son un proceso de Go con su servidor MCP; este benchmark no los desglosa. Del
  fichero mapeado siguen siendo `6,1 MB` por proceso de `shared_clean` sobre un
  snapshot de `77,6`, y esas páginas están limpias.
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
