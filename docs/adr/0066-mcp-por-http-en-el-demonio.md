# ADR 0066: MCP por HTTP en el demonio, con el token como clave

- **Estado:** aceptada
- **Fecha:** 2026-08-23
- **Cambia el protocolo MCP:** no -- la misma superficie de tools sobre el
  transporte Streamable HTTP del propio spec
- **Cambia el schema persistente:** no
- **Obliga a reconstruir:** no
- **Cambia la superficie del CLI:** sí -- `daemon` acepta `--addr`, y publica
  `daemon.json` en el directorio de estado

## Contexto: el demonio no tenía consumidor

El ADR 0065 entregó `kivgraph daemon` sobre un socket unix, y el
`benchmarks/daemon-cost` midió lo que ahorra: `66`–`67 MB` de páginas privadas
por cliente contra `0,2`–`2,3`, y `533 MB` contra `68`–`82` a ocho clientes.

Esa cifra describe el socket bajo carga sostenida, y no es la que un usuario ve.
Medido después a la carga real -- mediana de *una* llamada por sesión, contada de
un event log de uso, donde `48` de `51` servidores no recibieron ninguna-- las dos
puertas cuestan lo mismo: `1,0`–`1,3 MB` por cliente por HTTP contra `1,1`–`1,6`
por socket, y N procesos `43` en vez de `66`. Ver la sección de consecuencias.

Y no lo podía usar nadie. Todas las integraciones que este proyecto escribe
-- Claude Code, Claude Desktop, Cursor, Oh My Pi-- ponen la misma entrada, en
`internal/integrations`:

```json
{"command": "kivgraph", "args": ["serve"]}
```

Eso es stdio, un proceso por cliente. **Un cliente MCP no marca un socket unix**:
su configuración acepta un ejecutable con argumentos o una `url`. El ahorro estaba
medido sobre código inalcanzable.

El ADR 0065 descartó por escrito la salida que el diseño original proponía -- que
`serve` detectara el socket y hiciera de relé-- con un argumento que era falso:
«un relé añadiría un proceso por cliente para ahorrar procesos por cliente». Un
relé no carga el snapshot, y el snapshot es lo que cuesta: los `63`–`67 MB` del
intercepto medido. Pero la conclusión correcta no es construir el relé, porque
**MCP ya tiene un transporte HTTP** y los clientes ya lo hablan.

## La decisión

El demonio sirve la superficie MCP por HTTP además del socket, con el
`StreamableHTTPHandler` del SDK, ligado a loopback y **protegido por un token**.

### Por qué el rechazo del ADR 0017 no aplica

El ADR 0017 descartó «abrir HTTP dentro de `kivgraph serve`», y la razón que dio
es exacta: «cambia el contrato de un proceso que actualmente es STDIO puro y
complica el aislamiento de sesiones». Ninguna de las dos mitades le toca a un
demonio: no es STDIO puro, y existe precisamente para aislar N sesiones -- ya
construye un servidor MCP por sesión aceptada, y ése es su contrato desde el ADR
0065.

El otro descarte de aquel ADR -- «usar el transporte HTTP del SDK sin una API
específica»-- se refería al **visor**, que necesita subgrafos inducidos, tiles y
buffers binarios. Un cliente MCP no necesita nada de eso: necesita exactamente lo
que el transporte del spec hace.

`serve` sigue siendo STDIO puro y no abre ningún puerto. Ese contrato no se toca.

### El token, y por qué no es opcional

Aquí está el precio, y no se hereda gratis del ADR 0017.

Un socket unix vive **dentro** del directorio de estado con modo `0600`: en Linux
el kernel comprueba permiso de escritura al conectar, y en las dos plataformas hay
que atravesar el directorio para llegar a la ruta. Eso era la clave del ADR 0065:
dos configuraciones apuntando a directorios distintos obtienen demonios distintos,
y un cliente nunca alcanza un grafo construido a partir de los repositorios de
otro.

**Un puerto no tiene ruta.** Cualquier proceso local, de cualquier usuario de la
máquina, puede conectar a `127.0.0.1:7788` y pedir el grafo entero: nombres,
rutas, firmas, código. El ADR 0017 ya declaró que «el bind loopback no es
autenticación», y para el visor se aceptó como riesgo. Para MCP es el mismo dato
por otra puerta, así que la decisión se toma otra vez y sale distinta.

El token conserva la propiedad que el modo del socket compraba, por otro medio:

* Se genera al arrancar, `32` bytes de `crypto/rand`.
* Se escribe en `daemon.json`, **dentro del directorio de estado**, con el mismo
  `umask` privado que ya usa el socket. Quien puede leer ese fichero es
  exactamente quien podía conectar al socket.
* Se exige en cada petición como `Authorization: Bearer`, con el middleware
  `auth.RequireBearerToken` del SDK y una comparación de tiempo constante.

Así que la frase del ADR 0065 sigue siendo verdad y por el mismo motivo: **el
directorio de estado es la clave**. Lo que cambia es qué la hace cumplir -- el modo
de un fichero en un caso, el contenido de un fichero en el otro.

### `Origin`, que es una puerta distinta

Un token no defiende de un navegador. Una página web puede hacer que el navegador
del usuario mande peticiones a `127.0.0.1` -- DNS rebinding-- y el spec de MCP
exige por eso validar `Origin` en un servidor HTTP local. Se valida: una petición
sin `Origin` pasa (no viene de una página), y una con `Origin` sólo pasa si es
loopback. Es una comprobación aparte del token porque defiende de otra cosa.

### La dirección, y un fallo que debe ser ruidoso

`--addr` con defecto `127.0.0.1:7788` -- loopback, y un puerto distinto del `7777`
del visor. Un bind fuera de loopback avisa, igual que hace `ui`.

Dos demonios de dos directorios de estado distintos **colisionan** en ese puerto,
y eso tiene que fallar: `bind` devuelve `EADDRINUSE` y el error lo nombra. La
alternativa -- un puerto efímero-- haría que cada arranque publicara una URL
distinta, y una entrada de `mcpServers` es un fichero estático: el cliente
quedaría apuntando a un puerto muerto. Un puerto derivado del directorio de estado
sería estable y silenciosamente colisionable, que es peor.

### Las integraciones

`internal/integrations` escribe hoy `command` + `args` fijos. Aprende a escribir
una entrada `url` leyendo `daemon.json`, para los clientes que la aceptan, y
conserva `serve` para los que no. Un cliente sin soporte HTTP no pierde nada.

## Alternativas descartadas

* **El relé de `serve`.** Sigue costando un proceso por cliente -- pequeño, pero
  uno-- y hay que escribir y mantener un puente de bytes que el spec ya resuelve.
  Su única ventaja es no exigir soporte HTTP en el cliente, y el socket ya cubre
  ese caso.
* **HTTP loopback sin token.** Es lo que el ADR 0017 aceptó para el visor, y
  aquí tiraría la única propiedad de aislamiento que el ADR 0065 compró.
* **Reutilizar el puerto del visor.** Mezcla dos contratos versionados
  independientemente -- la API del visor y la superficie MCP-- en un proceso que
  hoy son dos comandos distintos.
* **`config.mcp.transport: http`.** El vocabulario de esa clave es superficie de
  compatibilidad y hoy sólo acepta `stdio`, que es lo que `serve` habla. La
  dirección del demonio es una propiedad de una invocación, no de la
  configuración del transporte de `serve`; va en un flag, como `ui`.

## Consecuencias

* El demonio escucha en dos sitios a la vez y sirve el mismo grafo por los dos.
  El socket no se retira: es la puerta más estrecha que existe y no cuesta
  mantenerla.
* `daemon.json` es un fichero nuevo en el directorio de estado, con modo `0600`.
  Se borra al parar, como el socket.
* Un token en un fichero es un secreto en reposo. Quien pueda leer el directorio
  de estado ya podía leer el grafo entero, así que no añade una superficie nueva;
  lo que sí añade es que ese fichero no debe aparecer en un log ni en una
  captura de `doctor`.
* `kivgraph stop` ya reconoce `daemon`; nada cambia ahí.
* **Elegir HTTP no cuesta nada, y eso está medido a dos cargas.**
  `benchmarks/daemon-cost` corre las dos puertas con el transporte y los
  recuentos dentro del digest, sobre `108.737` símbolos de `kena` en Linux. La
  carga se contó de un event log de uso, donde `48` de `51` servidores no
  recibieron **ninguna** llamada:

  |carga|pendiente del demonio|N procesos|8 clientes|1 cliente|
  |---|---|---|---|---|
  |ninguna|indistinguible de cero|`10 MB`/cli|`10`–`13` contra `77`–`81 MB`|el demonio `+2`–`3 MB`|
  |`8` llamadas|`0,6`–`0,9 MB`/cli|`39 MB`/cli|`60`–`62` contra `323`–`330 MB`|empata|

  Por puerta y sin carga: `9,8`–`10,6 MB` por cliente por HTTP contra
  `10,0`–`10,7` por socket. Los rangos se solapan a las dos cargas, así que el
  transporte no se paga, y el cruce está entre `0,96` y `1,41` clientes.

  Estas cifras son posteriores al ADR 0067, que movió la lectura del grafo a la
  primera consulta: cuando este ADR se aceptó, un servidor ocioso costaba `33 MB`
  por cliente en vez de `10`. La comparación entre puertas no cambió con aquello,
  que es lo que este punto afirma.
* **Una advertencia anterior de este ADR queda retirada.** Decía que HTTP costaba
  `12,5 MB` por cliente y que a un cliente el demonio perdía. Era el
  `MemoryEventStore` de `10 MiB` que el SDK da a cada sesión (`event.go:255`)
  llenándose con `2.000` llamadas sintéticas. A carga real no aparece; bajo carga
  sostenida sí -- `12,8 MB` por cliente aquí, `4,9`–`5,9` sobre un corpus mayor, y
  esa dependencia del corpus es la firma de un coste en bytes y no en sesiones.
  Sigue sin poder acotarse
  desde aquí: `NewStreamableHTTPHandler` no acepta un `EventStore` y
  `StreamableHTTPOptions` sólo expone `Stateless` y `JSONResponse`. Si el SDK
  cierra su `TODO(#148)`, el techo baja sin cambiar nada más.
* **El orden de arranque es contrato:** HTTP se publica antes de enlazar el
  socket. Un socket unix acepta en cuanto está enlazado, antes de que nadie llame
  a `Accept`, así que alcanzar el socket tiene que implicar que `daemon.json` ya
  existe -- al revés, una de cada tres pasadas del benchmark moría con «no such
  file or directory» sobre un demonio vivo. Y si el `bind` falla después, el
  endpoint se retira.
* Una entrada con token **no se escribe en ámbito `project`**: `.mcp.json` se
  commitea, y un secreto en git no se retira borrándolo. `integrations` lo
  rechaza con `ErrEndpointNeedsUserScope` en vez de degradar a stdio en silencio.
