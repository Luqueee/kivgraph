# Instrucciones del CLI (`cmd/kivgraph/`)

Estas reglas se suman a las de `AGENTS.md` en la raíz del repositorio, que se
leen siempre. Una instrucción de este archivo puede añadir restricciones; nunca
puede relajar un contrato de integridad, compatibilidad o verificación
declarado en la raíz.

Lo que estos comandos ejecutan vive en `internal/`; aquí sólo está su
superficie observable.

## La tabla de comandos

- La línea de comandos se declara **una vez**, en `cmd/kivgraph/commands.go`, y
  se lee tres veces: el despacho que ejecuta, la ayuda que lista y el
  completado que sugiere. Antes se declaraba dos veces -- una cadena `if` para
  despachar y una tabla aparte para la ayuda -- y las dos ya habían divergido:
  diez comandos llevaban flags que la ayuda no nombraba nunca. Una tercera
  copia para el completado habría divergido igual, y un completado que omite un
  tercio de la superficie es peor que ninguno, porque el lector se lo cree.
- **Los flags no se repiten en la tabla.** Cada comando tiene un constructor
  `<nombre>FlagSet` y ese es el único sitio donde un flag se escribe;
  `writeFlagList` ya recorre un `flag.FlagSet` real. Un comando nuevo añade su
  `<nombre>Options` y su `<nombre>FlagSet` junto a su `runX`, como ya hacían
  `uiFlagSet` y `serveFlagSet`, y una entrada en la tabla.
- El campo `usage` es un **resumen curado para una persona**: puede nombrar
  menos flags de los que el comando acepta -- listar los ocho de `logs` en una
  línea de ayuda la empeora -- pero nunca uno que no exista, y
  `TestUsageNamesOnlyRealFlags` lo comprueba. El completado no depende de él:
  lee el `FlagSet`, así que es exhaustivo.
- Las palabras se emparejan **de más larga a más corta**, que es lo que impide
  leer `doctor storage` como `doctor` con un argumento suelto, o `index --full`
  como `index`.
- `serve` y `ui` están en la tabla con `run: nil`. `main` los intercepta antes
  de `run` porque son los dos que poseen un manejador de señales y registran
  estructuradamente toda su vida; están declarados para que la ayuda y el
  completado los describan igual.

## Completado

- El script de cada shell es un **stub fijo**: sin nombres de comando, sin
  flags y sin vocabularios. Reenvía las palabras a `kivgraph __complete` y todo
  candidato sale de la tabla. Un script que hay que regenerar al añadir un flag
  es un script que estará desactualizado, y `TestCompletionScriptEmitsOnePerShell`
  falla si un stub codifica un nombre.
- El stub de bash está escrito para **bash 3.2**, que es el que trae macOS: sin
  `mapfile` y sin `compopt`. Verificado ejecutándolo ahí, no supuesto -- la
  primera versión usaba `mapfile` y fallaba en toda invocación.
- `COMP_WORDBREAKS` de bash contiene `=`, así que entrega `--kind=s` como tres
  palabras donde zsh y fish entregan una. El motor reconoce las dos formas, lo
  que mantiene los stubs idénticos y sin reensamblado propio de cada shell.
- Un flag que toma una ruta responde `:file` y el shell usa su propio
  completado: el entrecomillado, los enlaces y `~` son su trabajo, y una lista
  generada aquí los haría mal.
- Los candidatos que un script estático no podría dar son los que justifican el
  diseño: `--generation` lee las generaciones en disco, `--target` los clientes
  de esta máquina y `--tool` las tools que esta instalación ha recibido de
  verdad, desde el registro durable. Un comando que esta build no puede
  ejecutar no se ofrece.

## Ayuda, salidas y registro

- La ayuda no es un error: `--help`, `-h` y `help` escriben en `stdout` y
  terminan con código `0`. **Una invocación sin comando es la misma pregunta**
  -- «qué hace esto» -- así que escribe esa misma tabla en `stdout` y termina en
  `0`; antes escribía una línea en `stderr` apuntando a una ayuda que el lector
  todavía no había visto. Un comando **desconocido** sí es un error: una línea
  en `stderr`, código `2`, y nunca la superficie entera -- quien se equivoca
  escribiendo `inedx` no necesita leerse el catálogo. Un comando puntual informa
  en texto plano cuando `stderr` es una terminal y como registro JSON cuando no
  lo es; `serve` y `ui` registran siempre en JSON, porque un cliente lee su
  `stderr`.
- La ayuda marca el comando que esta build no puede ejecutar. El bundle MCP
  publicado no lleva `webassets`, así que anunciar `ui` sin decirlo describe
  una capacidad que no existe.
- Un comando puntual clasifica lo que escribe en `stderr`: el progreso es
  `INFO` y sólo un fallo es `ERROR`, y el texto de la línea es el `msg` del
  registro, nunca un campo dentro de un mensaje fijo. Un registro que un
  lector no puede filtrar por nivel ni encontrar por texto no informa de nada.
- `doctor` informa de `snapshot.published`, y las respuestas que no son un
  fichero utilizable **no valen lo mismo**. Son tres, no dos:
  **ausente es `PASS` y se declara** -- una generación publicada antes de que el
  fichero existiera no lo lleva, y derivar es lo que siempre se hizo--;
  **escrito por un formato anterior es `PASS` y se declara**, porque no hay nada
  mal en el store, se movió el layout, y el siguiente indexado lo sustituye;
  y **presente, de este formato y no utilizable es `FAIL`**, porque entonces sí
  algo del store está mal. Una sola respuesta de «no disponible» las contaría
  como lo mismo, y sólo la tercera merece despertar a alguien. Contar la segunda
  como fallo es peor que no informar: pone el `doctor` en rojo en cada
  actualización, que es exactamente como un fallo de verdad deja de notarse.
  Lo distingue `hotsnapshot.ErrSnapshotFileVersion`, que envuelve
  `ErrInvalidSnapshotFile` para que el resto de llamantes no cambie.
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

## `kivgraph doctor repositories`

- Contesta si un repositorio registrado está estructurado para que una pasada
  pueda leerlo, **sin indexarlo**, y dice qué cambiar cuando no lo está. Es la
  única forma de saberlo que no cuesta minutos: hasta aquí un agujero de
  cobertura sólo aparecía como un aviso entre cientos al final de
  `index --full`.
- **Toda comprobación pregunta al código que pregunta la pasada**: el mismo
  registro de paquetes, la misma resolución de fuentes -- por eso existe
  `workspace.ClaimedTypeScriptSources`--, el mismo `go list` con los mismos
  patrones -- por eso `goworkspace.PackagePatternsForModule` está exportada-- y
  el mismo `cargo metadata`. Una comprobación que reimplementara cualquiera de
  ellos contestaría por una pasada que nadie ejecuta, y eso es peor que no
  contestar: daría por bueno un repositorio que la pasada descarta.
- Los patrones de Go se nombran uno a uno **porque `./...` no basta**: un
  paquete cuyos ficheros excluye la configuración de build no lo matchea, así
  que `go list -e ./...` no informa de la exclusión. Medido: cero hallazgos con
  `./...`, los dos reales al nombrar los paquetes.
- **Un remedio se propone, nunca se aplica.** Kivgraph no escribe dentro del
  código que indexa, y el `Remedy` está pensado para que lo aplique quien lee:
  ruta, contenido exacto, clave de configuración o comando. Un remedio que no
  puede nombrar el tag que pide -- Go-- lo lee de los propios ficheros con el
  parser del compilador, porque «concede el tag» sin decir cuál no lo puede
  aplicar nadie.
- Sale con código `1` **sólo** si algo es `blocking`, es decir si un
  repositorio o un paquete no aporta nada. Un hallazgo `partial` es un agujero
  que su dueño puede haber elegido, y un gate que fallara por esos sería un
  gate que nadie puede pasar.
- `--json` es la salida para un agente: el informe entero, con el `code`
  estable de cada hallazgo y su remedio. La salida de texto es la misma
  información para una persona.

## Protocolo de `index --full --json`

- El flujo de `index --full --json` es un protocolo, no una bitácora: `stdout`
  lleva sólo eventos JSON por línea -`progress`, y un único `result` al final- y
  el informe que leería una persona no se escribe en ese modo. Los contadores de
  los cinco lenguajes viajan en el `result`; derivarlos del `msg` de un registro
  convertiría texto para humanos en una API. Un lector ignora una clase de
  evento que no conoce.
- **Lo que no se cargó viaja con lo que sí.** `go_modules_not_loaded`,
  `rust_workspaces_not_loaded`, `python_repositories_not_loaded` y
  `dart_repositories_not_loaded` están en el `result` y en las líneas del informe
  humano, los cuatro. Sin ellos un lector no puede distinguir un lenguaje sin
  código de uno que esta máquina no pudo leer: los dos suman cero símbolos. La
  razón difiere -- un workspace que el analizador no leyó, un módulo que no
  cargó, un analizador que no está instalado-- y la consecuencia no.
- **Y el contador nombra a sus módulos.** Cada módulo Go que no se leyó sale
  como `index.go.not_read: <repositorio>: <módulo>: <motivo>: <detalle>` en el
  informe humano, con el mismo tope de veinte líneas que los diagnósticos. El
  contador solo dejaba una pregunta sin respuesta: `not_loaded=4` con
  `diagnostics=1` no dice cuáles son los cuatro, y el motivo vivía únicamente en
  las filas `MODULE_NOT_LOADED` del grafo publicado, que ningún comando lee.
  `index.go.diagnostic` es otra clase -- lo que el cargador dijo de un módulo que
  **sí** leyó-- y mezclarlas perdería justo esa distinción.
- **`counts` y `index` del `result` no miden lo mismo, y hay que decirlo.**
  `counts` es lo que el grafo publicado guarda: hechos canónicos distintos.
  `index` es lo que cada pasada de lenguaje **observó**. Un fichero que
  pertenece a dos paquetes -- `pkg` y `pkg.test` -- se observa dos veces y se
  guarda una, así que los dos bloques del mismo evento divergen sin que ninguno
  esté mal. Medido sobre `workspace` con `include_tests: true`: `index` suma `146.600`
  símbolos y `counts.symbols` dice `124.073`; `go_definitions` va `1,63x` por
  encima de los símbolos Go del grafo y `go_unresolved` `1,58x`, mientras que
  Rust -- una pasada por workspace -- coincide exacto. Con `include_tests: false`
  los no resueltos de Go coinciden: `4.397` y `4.397`.
  Quien compare las dos cifras y espere que cuadren está comparando trabajo
  hecho con hechos guardados. No se deduplican los contadores del cargador: el
  número de observaciones dice cuánto trabajó la pasada, y es la única cifra que
  lo dice.

## Comandos que destruyen o terminan estado

- `kivgraph clean` retira generaciones publicadas: enumera y no toca nada sin
  `--yes`, porque no hay deshacer -- también se lleva el backup del que vive
  `rollback`. Sin flags deja el store vacío y libera la reserva de espacio;
  con `--keep-active` conserva exactamente la generación publicada. Nunca toca
  la configuración ni el registro de repositorios.
- `kivgraph stop` termina los procesos largos de este usuario y nada más. La
  lista es `longRunningCommands` -- `serve`, `daemon` y `ui` -- y es una lista y no
  una regla porque la regla sería falsa: `index --full` también dura minutos, y
  matar uno a mitad de publicación no es lo que pide quien manda parar un
  servidor. **Un comando largo nuevo se añade ahí y al mensaje de «nada
  corriendo», que lo nombra:** un lector que dejó un demonio y sólo oye hablar de
  `serve` concluye que `stop` no lo gestiona. Selecciona por invocación, no por
  ejecutable: el propio `stop` no se mata a sí mismo. Manda `SIGTERM`, espera el
  cierre graceful acotado y sólo entonces `SIGKILL`, y antes de escalar vuelve a
  comprobar que el pid sigue siendo la misma invocación: un pid liberado durante
  la espera puede ya pertenecer a otro proceso. `--dry-run` enumera sin señalar.
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

## `kivgraph daemon`

- Sirve MCP a varios clientes desde un proceso, por **dos puertas a la vez**: un
  socket unix en el directorio de estado y el transporte Streamable HTTP en
  loopback. Comparte
  con `serve` todo el montaje -- config, store, seguidor de generación, resync,
  event log-- a través de `runConfiguredServe`, que recibe el nombre del comando
  para que sus rótulos digan la verdad: dos comandos con un `serve` fijo en los
  logs son indistinguibles en el mismo event log.
- **El socket vive dentro del directorio de estado, y esa es la clave.** Dos
  configuraciones apuntando a directorios distintos obtienen demonios distintos,
  así que un cliente nunca alcanza un grafo construido a partir de los
  repositorios de otro. Un socket por máquina o por usuario habría sido más
  simple y habría cruzado exactamente eso.
- Una dirección unix es un campo de tamaño fijo -- `104` bytes en darwin, `108`
  en linux-- y `bind` **trunca** en vez de rechazar, lo que dejaría dos
  directorios compartiendo socket. Se comprueba contra el menor de los dos
  límites, para que un directorio que funciona en una plataforma funcione en la
  otra, y el error nombra la cifra. Es una limitación declarada: quien no cabe
  usa `serve`.
- **Un servidor MCP por sesión aceptada, no uno por proceso.** La superficie de
  tools se decide al construir un servidor -- un proceso sin generación publicada
  publica sólo `index_project`-- y un demonio sobrevive a las generaciones, así
  que uno construido al arrancar le diría a todo cliente futuro que no hay grafo.
  El store, el registro de métricas y el indexador sí se comparten: el snapshot
  se mapea una vez y lo que `graph_status` informa es del proceso.
- Un socket obsoleto y un demonio vivo no son el mismo estado, y la única forma
  de distinguirlos es intentar hablarle: si contesta, no se sustituye.
- El ahorro está medido en `benchmarks/daemon-cost` **a la carga que un editor
  produce de verdad**, contada del event log: `48` de `51` procesos servidores no
  recibieron **ninguna** llamada, así que la mediana de una sesión real es cero.
  Medir `2.000` llamadas por sesión no describía a nadie.

  |carga|pendiente del demonio|N procesos|8 clientes|1 cliente|
  |---|---|---|---|---|
  |ninguna|indistinguible de cero|`10` MB/cli|`10`–`13` contra `77`–`81 MB`|el demonio `+2`–`3 MB`|
  |`8` llamadas|`0,6`–`0,9` MB/cli|`39` MB/cli|`60`–`62` contra `323`–`330 MB`|empata|

  **El arranque ya no es el coste, y eso es lo que hace que este comando valga
  menos de lo que valía**: desde el ADR 0067 el grafo lo lee la primera consulta,
  así que un servidor ocioso cuesta `10 MB` y no `33`. Lo que un demonio sigue
  ahorrando es el resto -- y a un cliente ahora **pierde** por un par de megabytes,
  que es el proceso extra.

  **Las dos puertas son indistinguibles a las dos cargas**, así que la advertencia
  anterior -- que HTTP costaba `12,5 MB` por cliente y perdía a un cliente-- queda
  retirada: era el buffer de reanudación del SDK llenándose con tráfico sintético.
  Bajo carga sostenida sí cuesta más (`11`–`13`), y eso es un techo que ninguna
  sesión real alcanza y que depende del corpus.

  La diferencia más grande no es la pendiente: es el **pico**, `179`–`186` contra
  `26`–`29 MB` a ocho clientes, sin una sola consulta. Lo que no es el ahorro en
  ninguna puerta es el snapshot: ya se comparte y esas páginas están limpias.
- `kivgraph mcp install --daemon` es lo que hace usable todo lo anterior: lee
  `daemon.json` del directorio de estado y escribe una entrada `url` con el
  token. Sin ese flag se escribe `serve`, y es deliberado -- detectar un demonio
  y cambiar la entrada en silencio haría que el mismo comando escribiera dos
  ficheros distintos según si había un proceso arrancado. En ámbito `project` se
  niega: ese fichero se commitea.
- **El demonio publica HTTP antes de enlazar el socket, y el orden es contrato.**
  Un socket unix acepta en cuanto está enlazado, antes de que nadie llame a
  `Accept`, así que alcanzar el socket tiene que implicar que `daemon.json` ya
  existe. Invertirlo devolvió «no such file or directory» sobre un demonio vivo en
  una de cada tres pasadas del benchmark. Y si el `bind` del socket falla después,
  el endpoint se retira: un fichero que afirma que hay un demonio manda al
  cliente siguiente a un puerto cerrado.

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
