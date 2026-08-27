# ADR 0067: el grafo se lee cuando alguien pregunta

- **Estado:** aceptada
- **Fecha:** 2026-08-23
- **Cambia el protocolo MCP:** sí, de forma aditiva -- `graph_status` gana un
  campo opcional `snapshot_unreadable`
- **Cambia el schema persistente:** no
- **Obliga a reconstruir:** no
- **Cambia la superficie del CLI:** no -- ningún comando, flag ni salida cambia
- **Relaja un contrato de la raíz:** sí, y esa relajación es la decisión

## Contexto: casi nadie pregunta

`benchmarks/daemon-cost` midió qué cuesta un servidor al que **nadie consulta**,
que resultó ser el caso normal y no un extremo: contando el event log de una
máquina en uso, `48` de `51` procesos `serve` se arrancaron y no recibieron
ninguna llamada de tool. La mediana de una sesión real es cero.

Y ese servidor costaba casi lo mismo que uno consultado:

|carga|N procesos|
|---|---|
|ninguna llamada|`33,1`-`34,4 MB` por cliente|
|`8` llamadas|`39,9`-`40,7 MB` por cliente|
|`2.000` llamadas|`67,6 MB` por cliente|

`33` de los `40 MB` -- el `83 %` -- se pagaban antes de la primera pregunta. El
fichero del snapshot no es lo que cuesta: se mapea, sus páginas están limpias y
son compartidas (`6,1 MB` por proceso sobre un fichero de `77,6`). Lo privado son
las **tablas que los decodificadores construyen al mapear**: `evidence` (`5,6 MB`),
los dos arrays CSR de aristas (`9,5`), `symbols` (`4,6`), `unresolved` (`2,4`) y
los offsets, contra `0,84 MB` de los tres índices de lookup que
`newGraphSnapshot` deriva -- `23,9 MB` en total por aritmética sobre los
recuentos de la generación `000001` y las anchuras de `file.go`. Las cadenas no
están ahí: se leen del mapa, y son justo los `6,1 MB` compartidos.

Un servidor que nunca contesta no necesita ninguna de ellas.

## Lo que impedía diferirlo

El contrato de la raíz decía: «`kivgraph serve` debe cargar el `HotSnapshot`
publicado **antes** de abrir el transporte MCP». Existe por una razón buena: un
servidor que se anuncia listo y luego no contesta es peor que uno que no arranca,
porque el cliente lo lanzó y una salida se lee como un cierre inesperado.

Y había un obstáculo que no era de contrato sino de código: `internal/mcp`
decidía **la superficie de tools** preguntando `snapshotStore.Load() != nil`. El
demonio construye un servidor MCP por sesión aceptada, así que el handshake
mapeaba el grafo para cada cliente -- incluidos los que no van a preguntar nada.

## La decisión

El store recibe el trabajo en vez del resultado. `NewDeferredSnapshotStore`
guarda un `SnapshotLoader`, y el primer lector que necesita el grafo lo
materializa, una vez, bajo un mutex; los demás lo encuentran publicado.

Lo que cambia el contrato es esto: **la disponibilidad se decide sin el grafo.**

- `Available()` responde si una consulta tendría algo que leer. Es lo que usa el
  handshake, y es lo que separa «trabajo pendiente» de «trabajo ausente».
- `ActiveID()` y `GenerationID()` nombran la generación sin mapearla, para el log
  de arranque y para el reconciliador.
- `Load()` sigue devolviendo el snapshot inmutable, y sólo aquí ocurre la carga.

Dentro del loader corre **exactamente** lo que corría en el arranque, fallback
incluido: un snapshot ausente, ajeno, viejo o corrupto sigue costando una
derivación del grafo canónico y no una respuesta. Sólo se movió el momento. Y esa
derivación cuesta `1,7 s`, no minutos, así que caber dentro de una consulta es
aceptable -- se midió antes de decidirlo.

El contrato de la raíz queda reescrito: lo que `serve` debe hacer antes de abrir
el transporte es **resolver** la generación publicada y decir si la tiene, no
leerla.

## Lo medido

Seis pasadas ociosas por las dos puertas, sobre `108.737` símbolos de
`workspace` en
Linux, desde un árbol limpio en el commit `68da6dc`:

|ocioso|antes|ahora|
|---|---|---|
|pendiente de N procesos|`33,9 MB`/cli|`9,8`-`10,7 MB`/cli|
|un cliente|`31,2 MB`|`7,1`-`9,2 MB`|
|ocho clientes|`268,6 MB`|`77`-`81 MB`|
|pico a ocho clientes|`994,3 MB`|`179`-`186 MB`|
|demonio, ocho clientes|`40,4 MB`|`10`-`13 MB`|

Un servidor ocioso cuesta **la tercera parte**, y el pico de ocho editores
arrancando a la vez baja de un gigabyte a `183 MB`. Y un cliente nuevo se conecta
en `14`-`23 ms` en vez de `96`-`107`, porque arrancar un proceso ya no mapea nada.

Con carga la cifra **no se mueve**: `38,4`-`39,5 MB` por cliente contra los `39,9`
de antes, y `66,1`-`66,2` contra `67,6` con `2.000` llamadas. El ahorro es
exactamente el de las sesiones que no preguntan, y no se pagó con nada.

La pendiente del demonio a carga cero pasa a cruzar el cero entre pasadas
(`-0,28` a `0,32 MB`), así que el cruce queda entre `0,96` y `1,41` clientes: los
dos brazos son ahora tan baratos en reposo que el proceso del demonio pesa
relativamente más. **A un cliente ocioso el demonio ahora pierde** -- `9,9`-`12,0`
contra `7,1`-`9,2 MB`--; gana desde el segundo.

## Consecuencias

* **Un fallo de carga ya no mata el proceso: llega a una consulta.** Antes un
  snapshot ilegible impedía arrancar; ahora `Load()` devuelve nil y toda tool
  responde `INDEX_NOT_READY`, que es el mismo código que responde un servidor
  jamás indexado. Son dos problemas distintos con arreglos distintos, así que
  `graph_status` gana `snapshot_unreadable` con el motivo. Es aditivo: un cliente
  que lo ignore no ve ningún cambio.
* **La negativa no se reintenta**, porque re-mapear un fichero roto en cada
  llamada convierte un fallo en un coste permanente. Se retira cuando alguien
  **publica** una generación, así que una reconstrucción recupera el servidor sin
  reiniciarlo.
* **Comparar generaciones no puede materializarlas.** `followOnce` corría
  `store.Load()` en cada tick de reconciliación: con la carga diferida eso habría
  cargado cada servidor ocioso al primer intervalo, deshaciendo todo en silencio.
  Ahora compara por identificador, igual que el arm de carrera de
  `publishActiveSnapshot`. Hay un test por cada uno de esos dos sitios, porque
  ninguno de los dos falla de forma visible cuando se rompe.
* **La primera consulta paga la carga.** En las medidas de este benchmark
  conectarse cuesta `1,5 ms` contra `96`-`151` de arrancar un proceso; la primera
  respuesta de un servidor diferido incluye el mapeo. No se midió como latencia
  aislada, y está declarado como límite.
* **El loader cierra sobre el contexto del arranque**, no sobre el de la consulta
  que lo dispara. Una consulta que expira no cancela una carga a medias: la
  siguiente la encuentra publicada. Es deliberado; cancelarla dejaría al servidor
  repitiendo el trabajo.
* **El watcher sigue siendo el caso que no ahorra.** Un proceso con
  reconciliación activa que publique una generación nueva la mapea, porque
  publicarla es materializarla. La configuración por defecto no lo activa.

## Alternativas descartadas

* **Validar el fichero al arrancar y sólo diferir si es de fiar.** Habría que
  hacer lo que hace la carga -- `InspectPublishedSnapshot` mapea el grafo entero--
  así que la comprobación cuesta lo que la cosa comprobada. Se descartó al medirlo,
  no al pensarlo.
* **Cambiar `Load()` por un `Resolve()` con error en los once handlers.** Nombra
  mejor el fallo, y son 18 sitios de producción en 16 ficheros para una lógica
  concurrente: por encima del techo que la raíz fija para un cambio así. El
  fallo queda nombrado en `graph_status`, que es donde un agente lo mira.
* **Descargar el snapshot tras un rato sin consultas.** Devolvería la memoria de
  un servidor que ya preguntó, y no es el caso medido: el caso medido es el que
  no pregunta **nunca**. Sin una medición de sesiones que preguntan y luego callan
  durante horas, esto sería una corazonada.
