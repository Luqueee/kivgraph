# ADR 0065: Un proceso sirve a muchos clientes

- **Estado:** aceptada
- **Fecha:** 2026-08-23
- **Cambia el protocolo MCP:** no -- la misma superficie de tools, el mismo JSON
  delimitado por saltos de línea, sobre otro transporte
- **Cambia el schema persistente:** no
- **Obliga a reconstruir:** no
- **Cambia la superficie del CLI:** sí -- añade el comando `daemon`

## Contexto

`LUQUE-2008`, abierta y aplazada dos veces. Cada cliente MCP arranca su propio
`kivgraph serve`, y cada uno de esos procesos paga su propia mitad privada del
coste de cargar un snapshot. La cifra está medida en
`benchmarks/load-cost-resident`, en Linux y con el binario real: **`71,2 MB` de
`Private_Dirty` por servidor**, con cuatro servidores arrancando contra el mismo
fichero mapeado.

Lo que esa medición también dice es que **bajar lo que la carga asigna no baja
esa cifra**. Las cuatro fases `LUQUE-2216` a `LUQUE-2220` retiraron `60,5 MB` de
asignación -- el gemelo de las tablas, el mapa de bits del CSR inverso, los tres
mapas de índices, la copia de claves-- y el residente por servidor se movió de
`71,8` a `71,2 MB`: **menos del 1 %, ruido**. Son bytes que el asignador recicla,
no bytes que un proceso conserva.

Así que quedaba una sola vía hacia esos bytes, y no es de formato ni de
asignación: **dejar de tener N procesos**.

## Decisión

Un comando `daemon` que sirve MCP a muchos clientes desde un proceso, sobre un
socket unix dentro del directorio de estado.

Lo que decide el diseño no es el ahorro, sino tres contratos que un demonio
rompe si se escribe a la ligera.

### El directorio de estado es la clave, y no la máquina ni el usuario

`daemon.sock` vive **dentro** del directorio de estado. Dos configuraciones
apuntando a directorios distintos obtienen demonios distintos, así que un cliente
nunca alcanza un grafo construido a partir de los repositorios de otro. Un socket
por máquina o por usuario habría sido más simple y habría cruzado exactamente
eso.

De ahí sale un límite que hay que declarar: una dirección unix es un campo de
tamaño fijo en el kernel -- `104` bytes en darwin, `108` en linux-- y `bind`
**trunca** en vez de rechazar. Un truncado dejaría dos directorios de estado
compartiendo un socket, que es el fallo que este diseño existe para no tener. Así
que la longitud se comprueba y se nombra: `ErrSocketPathTooLong` dice cuántos
bytes hay y cuál es el límite. Se usa `104` en las dos plataformas, para que un
directorio que funciona en una funcione en la otra.

El directorio por defecto cabe de sobra (`~/.local/state/kivgraph/daemon.sock`,
`44` bytes). El que no cabe es el temporal de darwin, y eso salió al escribir los
tests: `t.TempDir()` devuelve rutas de `100` bytes antes de añadir el socket.

### Un servidor por sesión, no uno por proceso

La superficie de tools **se decide cuando se construye un servidor**: un proceso
sin generación publicada publica sólo `index_project` y lo dice en sus
instructions. Un demonio sobrevive a las generaciones -- alguien corre
`index --full` en otra terminal-- así que un servidor construido al arrancar
seguiría diciéndole a **todo cliente futuro** que no hay grafo.

Construir uno por sesión cuesta once registros de tool y contesta con la
generación que existe ahora. El store, el registro de métricas y el indexador sí
son compartidos, y eso es lo que se quería: el snapshot se mapea una vez y lo que
`graph_status` informa es del proceso, no de un cliente.

### Un socket obsoleto y un demonio vivo no son el mismo estado

Un socket es un fichero, y `bind` rechaza una ruta que existe. Un proceso muerto
a señal deja el fichero detrás. La única forma de distinguirlo de un demonio vivo
es **intentar hablarle**: si contesta, no se sustituye; si no, el fichero se
borra y se sigue.

## El transporte, y por qué no se reutiliza el del SDK

El SDK trae dos transportes y ninguno sirve. El de `stdio` fija los descriptores
del propio proceso; el de memoria no cruza un socket. Un demonio tiene un
`net.Conn` por cliente.

`internal/mcp.StreamTransport` sirve una sesión sobre un `io.ReadWriteCloser`. El
cable es el mismo JSON delimitado por saltos de línea, así que lo único nuevo es
de dónde vienen los bytes. Tres detalles que sí son decisiones:

* **La cancelación llega cerrando el stream, no por el contexto.** Un `Decode` ya
  bloqueado en una lectura no observa un contexto, y el contrato de `Connection`
  nombra `Close` como la forma de desbloquear una lectura pendiente. Por eso el
  demonio cierra la conexión cuando su contexto se cancela: sin eso el bucle de
  `accept` termina y luego espera para siempre a sesiones cuyos clientes no
  habían colgado. El contexto **sí** se comprueba antes de decodificar, y no es
  redundante: una sesión ya terminada que leyera se llevaría el mensaje siguiente
  de la cola sin contestarlo.
* **Un batch JSON-RPC se rechaza.** La revisión `2025-06-18` del protocolo lo
  retiró y aquí no hay nada que desempaquete un array: contestar un batch como si
  fuera un mensaje correlacionaría la respuesta equivocada con la petición
  equivocada. Lo rechaza el decodificador, no una comprobación propia.
* **`Write` toma un lock.** Los handlers producen respuestas y notificaciones a
  la vez, y un `io.Writer` no promete atomicidad: una escritura corta es legal.

`SessionID` es vacío, la misma respuesta que da la conexión de `stdio` del SDK:
un id pertenece a los transportes que multiplexan sesiones sobre un endpoint, y
este lleva exactamente una.

## Lo que no se mide todavía

El ahorro **no está observado**. La cifra medida es la del arreglo que esto
sustituye (`71,2 MB` por servidor, cuatro servidores, Linux). Lo que un proceso
para muchos clientes cuesta en su lugar es una medición pendiente, `LUQUE-2222`,
y hay una razón concreta para no darla por hecha: el snapshot ya se comparte -- es
el mismo fichero mapeado en todos los servidores y esas páginas están limpias--,
así que los bytes en juego son los privados, y la mitad privada de un demonio
crece con cada sesión para la que construye un servidor.

Hasta esa medición, el ahorro que este paquete afirma es **aritmética sobre una
cifra medida por proceso**, y su documentación lo dice así.

`serve` no se retira ni se marca obsoleto. Un cliente que arranca su propio
proceso sigue siendo el camino soportado, y es el único que funciona cuando el
directorio de estado no admite un socket.

## Verificación

* Trece decisiones falsificadas una a una, sabotando el código y comprobando qué
  test cae: el servidor por sesión, el cierre de la conexión al cancelar, el
  borrado del socket obsoleto, el límite de longitud, el socket por directorio de
  estado, el límite nombrado en el error, el store compartido, el apagado que no
  es un fallo, el lock de `Write`, el salto de línea, la comprobación del
  contexto antes de leer, el stream nulo, el `SessionID` inventado y el error de
  escritura tragado. Trece cazados; cada sabotaje se restauró y se comprobó con
  `cmp`.
* Dos huecos reales aparecieron ahí y se cerraron: `net.Pipe` entrega cada
  `Write` de una pieza, así que el test de concurrencia no veía el desgarro hasta
  escribirlo contra un `io.Writer` que parte cada escritura en dos; y ningún test
  leía los bytes crudos, así que el salto de línea no estaba fijado.
* Un comentario afirmaba un rechazo de batch que no estaba en este código -- lo
  hace el decodificador del SDK-- y se corrigió la atribución.
* `isDisconnect` parecía código muerto: el SDK ya se come `io.EOF`, así que ni un
  cierre limpio ni una caída abrupta llegan como error. Vive en el apagado, donde
  la lectura falla con `net.ErrClosed` o un contexto cancelado, y ese es el test
  que lo defiende.
* Humo con el binario real y una generación publicada: tres clientes
  concurrentes sobre un demonio, `11` tools cada uno, `find_references` y
  `get_file_outline` pedidos a la vez por clientes distintos y respondidos sin
  cruzarse, socket desvinculado al parar, cero errores en `stderr`.
* `gofmt`, `go vet ./...`, `go test ./...` y `make build` en verde.
