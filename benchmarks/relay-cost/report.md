# Lo que cuesta un relé, antes de escribir el relé

`LUQUE-2233`, commit 1. El ADR 0084 se puso una condición a sí mismo: su
estado es «accepted in shape, gated on the relay's own process floor being
measured first», y su plan dice que **el primer commit puede cancelar los
otros tres**. Esto es ese commit. No hay código de producto: hay un prototipo
de cuarenta líneas y el arnés que lo mide.

La pregunta no era si un demonio ahorra -- eso ya lo midió `daemon-cost` --
sino si un **relé** ahorra, que no es lo mismo. Un relé es el mismo binario
con el mismo runtime, así que paga suelo de proceso igual que un `serve`. Y
contra un `serve` ocioso, que el ADR 0067 ya dejó barato, un suelo de `8 MB`
habría ahorrado cuatro y no habría nada que construir.

## El veredicto

**Pasa, y por dos márgenes muy distintos.** El umbral escrito en el arnés es
`1 MB` por cliente; el `results.json` lo lleva junto a la medida para que una
pasada posterior se juzgue igual y no contra una cifra que alguien recuerde.

|carga|`serve`|relé|ahorro|umbral|
|---|---:|---:|---:|---:|
|ninguna llamada|`8,76`–`9,15` MB/cli|`4,63`–`5,83`|`2,93`–`4,24`|`1`|
|`8` llamadas|`68,94`–`70,28` MB/cli|`8,74`–`9,78`|`59,26`–`61,54`|`1`|

Tres pasadas por carga; el artefacto publicado es la mediana por pendiente
de `serve`. Lo que se publica es la **pendiente por cliente** y no ningún
total, por el motivo que da `benchmarks/AGENTS.md`: un arreglo que ahorrara a
dos clientes y no a ocho parecería una victoria en la fila que cada lector
eligiera.

## Los tres números que pedía el ADR

**Uno: el suelo del relé.** Aislado del demonio que tiene detrás, un proceso
relé cuesta `4,21`–`5,59` MB por cliente ocioso y `4,72`–`5,54` contestando.
Es plano en la carga, que es exactamente lo que debe ser un proceso que no
guarda grafo: lo que hay ahí es runtime de Go, el decodificador y los búferes
de las dos conexiones.

No es gratis. Contra una sesión HTTP directa -- `0,04`–`0,13` MB por cliente
ocioso -- ese suelo **es el precio íntegro de conservar la entrada stdio** que
el formato `.mcpb` obliga a mantener. El brazo `daemon` no se mide como
alternativa: ninguna instalación por `.mcpb` puede configurarse así, porque
el manifiesto no tiene campo para una url. Está para decir qué parte del
coste del relé es el relé.

**Dos: un `serve` que contesta sobre el corpus de hoy.** `68,94`–`70,28` MB
por cliente. Nadie lo había medido: `daemon-cost` dio `38,4`–`39,5` sobre
`108.737` símbolos, y esta generación tiene `186.159`.

**Y las dos mitades de la predicción del ADR se cumplen.** El corpus es
`1,71` veces mayor y la fila que contesta subió `1,77` veces; la fila ociosa
no subió: `9,8`–`10,7` MB/cli entonces contra `8,76`–`9,15` ahora. Lo que
escala con el grafo es contestar, no arrancar, que es lo que el ADR 0067
dejó montado y esto confirma sobre otro corpus.

**Tres: arranque más handshake.** Aquí la premisa de la pregunta estaba
caducada, y conviene decirlo antes que la cifra.

|espera|`serve`|relé|demonio|
|---|---:|---:|---:|
|conectar|`6,7`–`8,2 ms`|`3,8`–`5,0`|`1,6`–`2,6`|
|primera respuesta|`525`–`543 ms`|`14,5`–`17,7`|`11,5`–`15,5`|

El ADR pedía comparar contra «los `38`–`55 ms` que sustituye». Ese número ya
no existe: un `serve` de hoy conecta en `6,7`–`8,2 ms` porque el ADR 0067
sacó la carga del arranque. **La espera no desapareció, se mudó.** Ahora está
entera en la primera respuesta, y ahí un `serve` cuesta medio segundo contra
los `15 ms` del relé -- un factor de `34`. En la conexión el relé gana `3 ms`,
que no es un argumento para nada.

## La carga que manda es la ociosa, y ahí el margen es el pequeño

`48` de cada `51` servidores reales no reciben ninguna llamada. Esa es la
fila de arriba de la primera tabla, y es la que menos ahorra.

Peor: **a pocos clientes el relé pierde.** Tiene un coste fijo que un `serve`
no tiene -- el demonio detrás -- y hace falta repartirlo.

|cruce de las rectas ajustadas|pasada 1|pasada 2|pasada 3|
|---|---:|---:|---:|
|ocioso|`4,83` clientes|`6,02`|`4,91`|
|`8` llamadas|`1,42` clientes|`1,48`|`1,49`|

Contestando gana desde el segundo cliente. **Ocioso no gana hasta el quinto o
el sexto.** El event log del ADR 0069 contó `8` servidores vivos a la vez, así
que el caso observado cae por encima del cruce -- pero por poco, y por encima
de un cruce que se mueve `1,2` clientes entre pasadas.

Los totales a ocho clientes dicen lo mismo sin recta de por medio:

|a `8` clientes|`serve`|relé|demonio|
|---|---:|---:|---:|
|ocioso, privado|`81,3`–`85,7 MB`|`67,9`–`75,9`|`26,8`–`27,0`|
|ocioso, pico|`229`–`234 MB`|`145`–`154`|`32,0`–`32,1`|
|`8` llamadas, privado|`575`–`584 MB`|`175`–`183`|`129`–`130`|
|`8` llamadas, pico|`2.531`–`2.540 MB`|`437`–`445`|`319`–`320`|

Ocioso a ocho clientes el relé ahorra unos `10`–`15 MB`, que es real y es
poco. Contestando ahorra `400 MB` de residente y **`2,1 GB` de pico**, y esa
es la fila por la que existe la ficha.

## Lo que la pendiente ociosa no resuelve

Entre uno y dos clientes ociosos el residente total **baja** en las tres
pasadas de `serve` (`-1,45`, `-5,25` y `-3,47` MB/cliente) y en dos de las
tres del relé. Un cliente de más no devuelve memoria: significa que a esta
carga el coste por cliente está por debajo de lo que este método resuelve, y
el escalón se publica crudo en `results-idle.json` en vez de recortarlo,
porque recortarlo escondería justo eso. Los escalones de `2`→`4` y `4`→`8`,
donde sí hay señal, son los que sostienen la recta.

Bajo carga no pasa: los tres escalones de `serve` van de `55` a `73` MB por
cliente y ninguno cambia de signo.

## Lo que esto no contesta

- **Una máquina, un kernel, un corpus.** Las cifras no se transportan a otro
  workspace: lo que un cliente le cuesta a un servidor crece con el grafo, y
  este está declarado arriba. Es la misma regla que ya se rompió una vez en
  `daemon-cost`, citando un techo de un corpus como si fuera un coste.
- **El prototipo no es el relé.** No tiene fallback, ni provisionado, ni la
  comprobación de skew de versión que el ADR decidió. Un relé terminado no
  será más barato que éste, así que el suelo medido es una **cota inferior**
  del real.
- **El brazo `daemon` no es una alternativa desplegable** para las
  instalaciones que llevan el volumen, y no debe citarse como tal.
- **No hay carga sostenida.** `daemon-cost` conserva un techo de `2.000`
  llamadas; aquí no se midió, porque la ficha se decide entre la carga
  mediana y la real, y un techo habría dado el número más favorable.
- **Nadie ha medido el fallback.** Es el camino de una plataforma sin
  supervisor, y por construcción cuesta lo que la columna `serve`.

## Reproducir

```bash
# Con el demonio del usuario parado: el arnés levanta el suyo en el mismo
# state dir, y dos demonios no comparten socket.
systemctl --user stop com.kivgraph.daemon.<id>.service

LIB="$(scripts/fetch-ladybug.sh | tail -1)"
export CGO_CFLAGS="-I$LIB" CGO_LDFLAGS="-L$LIB -llbug -Wl,-rpath,$LIB"
go build -tags ladybug -o /tmp/kivgraph ./cmd/kivgraph
go build -o /tmp/relay ./benchmarks/relay-cost/prototype
go build -tags ladybug -o /tmp/relay-cost ./benchmarks/relay-cost

for c in 0 8; do for n in 1 2 3; do
  /tmp/relay-cost -server /tmp/kivgraph -relay /tmp/relay \
    -config "$HOME/.config/kivgraph/config.yaml" \
    -generation-dir "$HOME/.local/state/kivgraph/generations/000091" \
    -state-dir "$HOME/.local/state/kivgraph" \
    -clients 1,2,4,8 -calls $c -warmup 0 -output /tmp/relay-$c-$n
done; done

systemctl --user start com.kivgraph.daemon.<id>.service
```

El árbol tiene que estar limpio: si no, `commit` sale con `-dirty` y la
corrida lo declara como limitación, porque un artefacto que nombra una
revisión que no contiene su código atribuye sus números al sitio equivocado.

## Artefactos

|fichero|carga|pendiente de `serve`|veredicto|
|---|---|---:|---|
|`results-idle.json`|ninguna llamada|`8,87` MB/cli|`proceed: true`|
|`results.json`|`8` llamadas|`69,12` MB/cli|`proceed: true`|

Los dos son la pasada mediana de tres. Commit `34cbe1a`, generación `000091`,
`186.159` símbolos, snapshot de `146.891.848` bytes, semilla `42`, sin
warmup, Linux `6.12.94+deb13-amd64` sobre `16` CPUs, `kivgraph 0.9.1`.

El `digest` es la identidad de las entradas -- corpus, generación, recuentos,
semilla, brazos -- y no de las medidas: dos corridas del mismo experimento
comparten cadena, y una cifra distinta no puede pasar por el mismo
experimento.

## Qué desbloquea

El commit 2 del `LUQUE-2233`: el relé de verdad y el fallback, con el test de
la SSE en solitario y el de skew de versión. Y con una advertencia que sale
de aquí y no estaba en el ADR: **el argumento no es el ocioso.** Quien
defienda el relé por el caso que predomina está defendiendo `10`–`15 MB` a
ocho clientes y una derrota por debajo de cinco. El argumento es la sesión
que contesta, donde son `400 MB` de residente y `2,1 GB` de pico.
