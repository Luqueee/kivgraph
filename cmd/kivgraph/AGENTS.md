# Instrucciones del CLI (`cmd/kivgraph/`)

Estas reglas se suman a las de `AGENTS.md` en la raíz del repositorio, que se
leen siempre. Una instrucción de este archivo puede añadir restricciones; nunca
puede relajar un contrato de integridad, compatibilidad o verificación
declarado en la raíz.

Lo que estos comandos ejecutan vive en `internal/`; aquí sólo está su
superficie observable.

## Ayuda, salidas y registro

- La ayuda no es un error: `--help`, `-h` y `help` escriben en `stdout` y
  terminan con código `0`; una invocación sin comando o con uno desconocido
  escribe una línea en `stderr` y apunta a la ayuda, nunca vuelca la superficie
  entera. Un comando puntual informa en texto plano cuando `stderr` es una
  terminal y como registro JSON cuando no lo es; `serve` y `ui` registran
  siempre en JSON, porque un cliente lee su `stderr`.
- La ayuda marca el comando que esta build no puede ejecutar. El bundle MCP
  publicado no lleva `webassets`, así que anunciar `ui` sin decirlo describe
  una capacidad que no existe.
- Un comando puntual clasifica lo que escribe en `stderr`: el progreso es
  `INFO` y sólo un fallo es `ERROR`, y el texto de la línea es el `msg` del
  registro, nunca un campo dentro de un mensaje fijo. Un registro que un
  lector no puede filtrar por nivel ni encontrar por texto no informa de nada.
- `doctor` informa de `snapshot.published`, y las dos respuestas que no son un
  fichero utilizable no valen lo mismo: **ausente es `PASS` y se declara** -- una
  generación publicada antes de que el fichero existiera no lo lleva, y derivar
  es lo que siempre se hizo-- mientras que **presente y no utilizable es `FAIL`**,
  porque algo del store está mal. Una sola respuesta de «no disponible» las
  contaría como lo mismo, y la segunda es la que merece despertar a alguien.
- `serve` dice al arrancar si leyó el snapshot publicado o lo derivó, y con qué
  razón. Nada más las distingue: un servidor que deriva contesta exactamente
  igual que uno que leyó, y cuesta un gigabyte más de pico al nacer. Mientras
  nadie lo dijo, esa confusión costó dos veces en una tarde -- el arranque
  derivando y un fichero de formato anterior sin cargar-- y ninguna suite lo vio,
  porque las dos rutas producen el mismo grafo.
- `stats` encabeza con la **memoria proporcional**, no con la residente, y esa
  elección es el motivo de que el comando exista: tres servidores que leen el
  mismo snapshot mapeado cuentan cada uno todas sus páginas, así que sumar
  residentes informa de una máquina gastando el triple de un fichero que está
  una vez. Medido: el RSS subió de 114 a 141 MB por servidor mientras el coste
  bajaba de 117 a 79. Donde la plataforma no sabe dividir páginas compartidas
  -macOS no sabe- no se ofrece ninguna aproximación: se encabeza con la
  residente y la nota dice que lo compartido se cuenta una vez por proceso.
  Adivinar qué páginas son compartidas es cómo un fichero mapeado empieza a
  parecer una fuga.
- El pico es columna propia porque es lo que dimensiona una máquina: un servidor
  que llegó a un gigabyte necesita ese gigabyte aunque aparque en la décima
  parte. Una pasada de indexado se colorea distinto justo porque explica un pico
  que un servidor no explicaría.
- Una vista viva que nadie mira es un comando que no termina: con `stdout`
  redirigido, `stats` imprime **una** observación y sale, y `--json` es esa misma
  observación para un script. La vista interactiva respeta `NO_COLOR` y
  `TERM=dumb` como el resto de la superficie.
- `doctor` informa del techo de versión con el que este binario comprueba
  tipos, no solo del `go` del PATH: son números distintos y el que decide si un
  repositorio se puede indexar es el primero.
- `logs` y `tool-stats` leen el registro durable de `internal/eventlog`, no
  preguntan a un servidor, y ese es el motivo de que puedan responder: los
  contadores por tool que un `serve` mantiene se acuñan al arrancar y se
  descartan al salir, así que una pregunta hecha desde otro proceso -- que es lo
  que es un comando -- encontraría siempre un registro vacío. Leer el fichero
  hace además que la respuesta abarque todos los servidores que corrieron, que
  es el intervalo que la pregunta implica. Ver ADR 0049.
- La insignia de `logs` nombra **qué** pasó, no sólo lo mal que fue: un fallo es
  `ERROR` y una respuesta degradada es `WARN`, pero una llamada que contestó es
  `TOOL` y una pasada es `INDEX`. Un registro donde toda línea rutinaria dijera
  `INFO` dejaría al lector haciendo la clasificación que el escritor ya sabía.
  Es la única superficie con fondo sólido: el resto de la salida no-TUI sólo
  tiene color de primer plano, y un `styleFor` que devuelve el estilo vacío
  sigue siendo lo que mantiene la vista redirigida en texto plano.
- La duración queda **fuera** de la identidad que colapsa líneas repetidas.
  Cada llamada tiene su tiempo propio, así que incluirla significaría que nada
  con duración se colapsa nunca -- y doce `find_references` seguidos son
  exactamente lo que un lector quiere ver como una fila con su media, no como
  doce filas que pasar. La fila lleva el instante de la ocurrencia más reciente,
  porque la pregunta que una línea repetida plantea es si sigue pasando.
- Un campo renderizado se pliega a una línea y se acota. Un fallo del cargador
  de Go llega con saltos de línea y una transcripción de `stderr` dentro:
  imprimirlo tal cual rompe el contrato de un registro por línea del que
  dependen todos los filtros y todo lector de esta salida. Observado en la
  primera pasada que falló de verdad, no supuesto.
- Los percentiles viven en el lector. `internal/metrics` retiene la latencia
  como cuenta, suma y máximo a propósito; el fichero guarda cada llamada, así
  que `tool-stats` puede contestar lo que una media esconde -- una tool cuya
  mediana es rápida y cuya cola no -- y sólo colorea la fila que falló, porque
  una tabla donde toda línea está pintada no dice qué línea leer.

## Protocolo de `index --full --json`

- El flujo de `index --full --json` es un protocolo, no una bitácora: `stdout`
  lleva sólo eventos JSON por línea -`progress`, y un único `result` al final- y
  el informe que leería una persona no se escribe en ese modo. Los contadores de
  los tres lenguajes viajan en el `result`; derivarlos del `msg` de un registro
  convertiría texto para humanos en una API. Un lector ignora una clase de
  evento que no conoce.

## Comandos que destruyen o terminan estado

- `kivgraph clean` retira generaciones publicadas: enumera y no toca nada sin
  `--yes`, porque no hay deshacer -- también se lleva el backup del que vive
  `rollback`. Sin flags deja el store vacío y libera la reserva de espacio;
  con `--keep-active` conserva exactamente la generación publicada. Nunca toca
  la configuración ni el registro de repositorios.
- `kivgraph stop` termina los procesos largos de este usuario -- `serve` y
  `ui` -- y nada más. Selecciona por invocación, no por ejecutable: una
  indexación en curso son minutos de análisis y no se tira, y el propio `stop`
  no se mata a sí mismo. Manda `SIGTERM`, espera el cierre graceful acotado y
  sólo entonces `SIGKILL`, y antes de escalar vuelve a comprobar que el pid
  sigue siendo la misma invocación: un pid liberado durante la espera puede ya
  pertenecer a otro proceso. `--dry-run` enumera sin señalar.
- `kivgraph update` ofrece parar lo que sobrevivió al bundle que reemplazó. Un
  `serve` o un `ui` que ya estaba corriendo sigue respondiendo desde la imagen
  que se intercambió -- con las tools viejas, las descripciones viejas y los
  bugs viejos -- y nada en su salida lo dice; el cliente que lo lanzó no lo
  reinicia por su cuenta. **No parar nada es el valor por defecto** siempre que
  la respuesta no se pueda preguntar: son procesos que un cliente posee, y
  terminar uno en silencio se le parece exactamente a una caída. `--stop` es la
  respuesta scriptable, y la escalada es la de `stop` -- una sola copia, en
  `stopTargets` -- no una segunda parecida. Una instalación que funcionó nunca
  se reporta como fallo porque la lista de procesos no se pudiera leer.

## `kivgraph ui`

- `kivgraph ui` registra la dirección que ha enlazado, incluida la que
  resuelve un puerto `0`, y se niega a arrancar cuando el binario no lleva el
  tag `webassets`: el bundle MCP publicado no lo lleva, así que solo podría
  servir la página de «bundle no disponible».

## Verificación

Un cambio de la superficie del CLI es un cambio de compatibilidad: revisar
`cmd/kivgraph/help.go`, la documentación de `landing/` y `scripts/install.sh`.

```bash
go test ./cmd/...
make build
```
