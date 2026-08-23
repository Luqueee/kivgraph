# Lo que un proceso para muchos clientes cuesta de verdad

`LUQUE-2222`. `kivgraph daemon` se escribió sobre una aritmética: si un `serve`
conserva `71 MB` de páginas privadas y esa cifra es plana en el número de
clientes, N de ellos deberían convertirse en uno. Esto lo mide.

Las métricas crudas están en `results.json`, que es la primera de las tres
pasadas. Este informe no emite ningún veredicto de aceptación: mide dos formas de
servir el mismo grafo, en un entorno concreto.

## Entorno y procedencia

|dato|valor|
|---|---|
|fecha|2026-08-23|
|commit|`0343d3a`|
|plataforma|VM `linux/arm64` de Docker Desktop, imagen `golang:1.26-trixie`|
|kernel|`Linux 6.12.54-linuxkit`, `10` CPU, page size `4096`|
|corpus|`kena`, `37` repositorios, **`108.737` símbolos**|
|snapshot|`77,6 MB`, generación `000001`|
|digest|`123b08f2d86e1c66`|
|carga|`2.000` llamadas medidas, `4.000` descartadas, semilla `42`|

La generación se publicó en el host (darwin/arm64, donde hay `node 25` y el
TypeScript de `kena` carga entero) y **se lee** en Linux. Los dos brazos leen ese
mismo fichero, byte a byte: ninguno deriva, así que LadybugDB no participa. El
workspace se monta en sólo lectura, que es lo que hace imposible tocar un
repositorio indexado.

`108.737` símbolos **no es el corpus de `LUQUE-2221`**, que midió `117.499`.
Cualquier comparación por símbolo con esa ficha tiene que usar la cifra de su
propia pasada; aquí un `serve` sale a `635 B` por símbolo contra los `647` de
allí, que es la misma magnitud y no el mismo número.

## La respuesta

`Private_Dirty` total, las tres pasadas:

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

* **La lectura del demonio a ocho clientes es la menos estable**: `68,2`, `71,1` y
  `82,1 MB` en tres pasadas. El signo del resultado no está en duda -- el brazo de
  procesos está en `530`-- pero la pendiente del demonio se estima entre `0,2` y
  `2,3 MB` por cliente según la pasada. Lo que se puede afirmar es el orden de
  magnitud, no la cifra.
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

# En Linux, los dos brazos contra ese fichero.
docker run --rm \
  -v "$PWD":/src -v /ruta/a/kena:/ruta/a/kena:ro -v "$HOME":"$HOME" \
  -e HOME="$HOME" golang:1.26-trixie bash -c '
    cd /src && go build -o /out/kivgraph ./cmd/kivgraph
    go build -o /out/daemon-cost ./benchmarks/daemon-cost
    /out/daemon-cost -server /out/kivgraph \
      -config "$HOME/.config/kivgraph/config.yaml" \
      -generation-dir "$HOME/.local/state/kivgraph/generations/000001" \
      -state-dir "$HOME/.local/state/kivgraph" \
      -clients 1,2,4,8 -calls 2000 -warmup 4000 -output /out/run'
```

El `digest` es la identidad de las entradas -- corpus, generación, recuentos,
semilla-- y no de las medidas: dos corridas del mismo experimento comparten
cadena, y una cifra distinta no puede pasar por el mismo experimento.
