# Instrucciones del motor (`internal/`)

Estas reglas se suman a las de `AGENTS.md` en la raíz del repositorio, que se
leen siempre. Una instrucción de este archivo puede añadir restricciones; nunca
puede relajar un contrato de integridad, compatibilidad o verificación
declarado en la raíz.

El camino Rust tiene su propio archivo en `internal/rustloader/AGENTS.md` y la
superficie MCP el suyo en `internal/mcp/AGENTS.md`.

## Carga de Go y workspaces

- Un método cuyo receptor no tiene nombre -el de una interfaz anónima, sea la
  de `var _ interface{ ... } = x` o la de una aserción `y.(interface{ M() })`-
  no es direccionable y nunca entra al grafo, igual que un `local N` de Rust.
  No hay camino hasta él desde el scope del paquete, así que no obtiene
  objectpath y su identidad cae al nombre cualificado, que sin dueño es el
  nombre del método a secas: dos declaraciones así en un paquete derivan una
  sola clave desde dos ficheros, y un `Symbol` con dos `File` que lo declaran
  es lo que prohíbe la multiplicidad de `DEFINES`. `go.include_tests` lo hacía
  aparecer porque un fichero de test es el segundo sitio donde se escribe esa
  interfaz.
- `internal/indexer.Full` carga Go mediante los patrones de paquetes producidos
  por `DiscoverGo`; no sustituirlos por `./...`, porque las `exclusions`
  configuradas deben seguir siendo efectivas durante `go/packages`.
- Los tags de build con los que se carga Go vienen de `go.build_tags`. Un
  directorio cuyos archivos excluye esa configuración no es un fallo del
  índice: se declara como `UNRESOLVED` con razón `PACKAGE_NOT_BUILDABLE` y la
  pasada continúa. Cualquier otro diagnóstico del cargador sigue abortándola.
  Indexar este repositorio exige el tag `ladybug`.
- `go/types` viaja enlazado en el binario: Kivgraph solo comprueba tipos hasta
  la versión del lenguaje del toolchain que lo compiló. Un módulo registrado
  por encima de ese techo se rechaza en `goworkspace.BuildPlan` nombrando
  repositorio, módulo y versión; nunca se deja que el `go.work` sintético
  escale el toolchain y reviente la carga de todos los repositorios dentro de
  la biblioteca estándar. El techo es `major.minor`.
- El `go.work` sintético resuelve una sola lista de build para los módulos que
  usa, así que el plan se parte en grupos independientes: dos módulos solo
  comparten workspace cuando uno alcanza al otro por `require`, por `replace`
  o por un `go.work` del repositorio indexado. Un módulo que no alcanza a
  ningún otro se carga en modo módulo, sin workspace, y los ficheros
  sintéticos que sobran se retiran con su `.sum`. La indexación sigue siendo
  hermética y `go.allow_network` es la única salida declarada cuando la MVS de
  un grupo selecciona un módulo ausente del caché local.

## La pasada de indexación

- El análisis de la pasada es concurrente: cada módulo Go y cada paquete
  TypeScript es una unidad independiente. El merge sigue el orden de las
  unidades, nunca el de finalización, así que el grafo publicado no depende de
  cómo se planificó el trabajo -- dos pasadas del mismo corpus producen hechos
  idénticos byte a byte, y eso es lo que hay que verificar al tocar esta zona.
  Los presupuestos son distintos por tipo: `go.maximum_loads` acota las cargas
  Go porque cada una sostiene un universo de tipos completo, y
  `typescript.maximum_workers` acota los procesos del worker. El primer fallo
  cancela el resto.
- Un módulo Go que el cargador no puede leer no tumba la pasada: no publica
  hechos -- no serían de fiar -- y se declara como `MODULE_NOT_LOADED` con los
  diagnósticos observados. Un repositorio cuyas dependencias nadie descargó no
  decide si los demás tienen grafo.
- Cada tipo de unidad drena su propia cola, la más pesada primero, con tantos
  trabajadores como permita su presupuesto y nunca más que unidades tenga. El
  peso es una estimación sobre los archivos que la unidad leerá, nunca un
  hecho del grafo: una pasada termina cuando termina su unidad más lenta.
- Los hechos de todas las unidades se fusionan en una sola pasada
  (`facts.MergeAll`), no una unidad contra el acumulado: una fusión par a par
  vuelve a deduplicar, copiar y reordenar el grafo entero en cada paso, que es
  cuadrático en su tamaño. La identidad con la que se deduplica es una tupla
  comparable, no una cadena unida por separadores, y es la misma que usa
  `Diff` para detectar un duplicado.
- Una petición de indexación se construye en un solo sitio,
  `indexing.OptionsFromConfig`: el llamador sólo añade lo que la configuración
  no decide -repositorios, directorio de trabajo, versión del resolver y los
  sinks de progreso-. Construirla dos veces es cómo `index_project` acabó sin
  nombrar ningún campo de Rust y fallando con «the Rust analyzer command is
  required» sobre la misma configuración que el CLI indexaba sin problema.
  Su resultado informa de los contadores de los tres lenguajes: un lenguaje
  ausente del informe se lee como un lenguaje sin código.
- Un nombre de paquete TypeScript declarado por varios manifests es una
  ambigüedad, no un repositorio roto: ningún manifest lo provee, ambos salen
  del registro y se declara `AMBIGUOUS_PACKAGE_PROVIDER`. Es el mismo trato que
  recibe un módulo Go con varios proveedores.
- La indexación TypeScript procesa providers `package.json` nombrados con
  `ProjectPath` resuelto; pasa ese path al worker en cada invocación y omite
  manifests sin proyecto en vez de adivinar el `tsconfig` de la raíz.
- Una ruta que el motor TypeScript resuelve llega con su grafía canónica, en
  minúsculas cuando el filesystem pliega mayúsculas. Se corrige en la frontera
  con `enginePath` antes de indexarla, compararla o emitirla; nunca con
  `realpath`, que resolvería los enlaces de `node_modules` y cambiaría los
  hechos.

## Caché de hechos

- La caché de hechos (`indexing.fact_cache`) guarda una entrada por unidad de
  análisis con **la lista de todo lo que la unidad leyó y su huella**; servirla
  exige revalidar esa lista entera. Una entrada nunca se sirve a otro
  analizador: su identidad es el contenido del ejecutable, la respuesta de
  `go env`, el contenido del worker TypeScript, los build tags,
  `include_tests` y `go.allow_network`. Un módulo que el cargador no pudo leer
  no se guarda jamás, porque su fallo depende del caché de módulos y ninguna
  huella del código lo describe. Ver ADR 0030.
- La procedencia de un repositorio -commit, rama, sucio- no es un hecho que
  produzca ningún análisis: la estampa la pasada al fusionar, desde el registro
  que se le dio, y ninguna unidad la escribe. Dentro de la entrada de caché se
  volvía rancia en cada acierto, hacía que el commit publicado dependiera de
  qué unidad fallara, y abortaba `verify` por un commit que no tocó un fichero.
- El lockfile se busca desde la raíz del repositorio registrado hacia arriba y
  se registra la cadena entera. `node_modules` no se recorre -es función del
  lockfile-, así que un lockfile que no se encuentra deja esa dependencia sin
  ningún control: en un monorepo pnpm vive por encima de los repositorios
  registrados.
- Una entrada registra el nombre de lo que hay que volver a medir, nunca el
  valor medido: una entrada cuyo nombre lleva su propio valor se compara
  consigo misma y no puede invalidar nada.
- Un nombre de paquete que hoy no provee nadie es una dependencia con huella
  `absent`, no la ausencia de una dependencia. Sin eso un `UNRESOLVED` se
  serviría para siempre y nunca se convertiría en la arista que le toca.
- Al tocar la caché se verifica con `fact_cache: verify`, que analiza todo y
  aborta la pasada si una entrada discrepa del análisis. Comparar dos pasadas
  con caché no demuestra nada.

## Grafo canónico y LadybugDB

- El código que usa la biblioteca nativa se compila con el tag `ladybug`.
- La pareja de versiones de LadybugDB y del binding Go debe provenir de la
  fijación versionada y verificarse mediante `scripts/fetch-ladybug.sh`.
- Validar el grafo canónico antes de publicarlo. Una generación inválida nunca
  puede convertirse en `CURRENT`.
- No leer ni servir consultas MCP directamente desde LadybugDB cuando el
  contrato exige el HotSnapshot publicado.
- No mezclar el esquema experimental `001-synthetic` con el canónico `002`.
- Los iteradores `VisitRepositories`, `VisitPackages`, `VisitFiles`,
  `VisitSymbols`, `VisitEvidence` y `VisitEdges` entregan copias por valor y
  no exponen slices internas; sus rangos son half-open y cancelables.
- `CanonicalColumns` reconstruye el esquema canónico completo en cada llamada,
  así que las columnas de una tabla se resuelven una vez por tabla y nunca por
  fila. El grafo tiene una arista por referencia del corpus.
- El scan canónico lee en chunks de Arrow, no valor a valor: un `Symbol` son
  dieciséis cruces de cgo por fila y el grafo tiene una fila por declaración
  del corpus. `scanCanonicalTuples` permanece como implementación de
  referencia y el test compara los dos lectores campo a campo -- un decoder
  de punteros equivocado produce un grafo que parece correcto, así que no se
  toca la ruta columnar sin ese oráculo.
- El motor escribe un `NULL` de cadena con los offsets `0..0`, que no son
  monótonos: el rango de datos de una columna es el más ancho sobre sus
  filas, nunca el que va del inicio de la primera al final de la última.
- Una apertura de sólo lectura del grafo canónico recibe un buffer pool
  proporcional a ese grafo -el doble de su tamaño, entre 256 MiB y 2 GiB-, nunca
  el 80 % de la memoria del sistema que da el motor por defecto: un scan lee cada
  página una vez y cierra. Una apertura de escritura conserva el defecto, que es
  la caché que un `COPY` de millones de filas necesita.
- El catálogo de `canonical_integrity.go` es la lista que la capa nativa usa
  para validar una arista. Una procedencia nueva que no entre ahí pasa todos
  los tests sin el tag `ladybug` y revienta la integridad al publicar; el
  guardia que lo impide vive en `canonical_catalog_test.go`, sin tag.
- Un upgrade de schema incompatible debe detectar la versión, respaldar y
  verificar la generación activa antes de reconstruir desde repositorios fuente.
  Solo una generación candidata que pase integridad y validación puede cambiar
  `CURRENT`; un rollback debe comprobar los digests del backup.
- Un rollback de versión debe cubrir restauración válida y rechazo fail-closed
  ante digest ausente o divergente; si falla la validación, `CURRENT` no cambia.

## Generaciones, publicación y snapshots

- `kivgraph serve` y `kivgraph ui` siguen la generación publicada: cargan el
  HotSnapshot al arrancar y republican cuando el puntero `CURRENT` avanza, sin
  coordinarse con nadie -- `SnapshotStore.Publish` solo acepta una generación
  estrictamente más nueva. Un `index --full` en otra terminal no puede dejar a
  un servidor sirviendo un grafo que ya no existe en disco.
- La publicación de una generación toma un `flock` sobre el propio store. El
  mutex del `Store` sólo ordena las goroutines de un proceso, y un directorio
  de estado lo comparten un `index --full`, un `index_project` de un cliente y
  el resincronizador de un servidor: sin él dos pasadas se pisan. Un candidato
  `<id>.tmp` que exista mientras se sostiene el lock es basura por
  construcción -el kernel seguiría sosteniéndolo por su escritor si viviera- y
  se retira. Rechazarlo convertía una pasada muerta -un OOM, una terminal
  cerrada- en un store inservible para siempre, porque todo intento posterior
  deriva el mismo identificador y choca con él. El lock no espera: una
  reconstrucción dura minutos y bloquear parecería un cuelgue, así que el
  perdedor recibe `ErrPublishInProgress`. Para el seguidor eso no es un fallo,
  es exactamente lo que el lock existe para producir.
- Tras un `clean` completo la numeración vuelve a `000001`, y
  `SnapshotStore.Publish` solo acepta una generación estrictamente más nueva:
  un servidor vivo conserva el grafo que ya no existe y no instalará ninguno
  más. El seguidor lo declara una vez y el comando avisa de reiniciar.
- Tras publicar un snapshot se llama a `rebuild.ReturnBuildMemory`: es el único
  momento en que el transitorio de la construcción está muerto -publicado, o
  descartado porque otro publicador ganó- y un servidor no tiene nada que hacer
  hasta la siguiente petición. Devuelve **las dos** mitades: el arena de Go con
  `debug.FreeOSMemory` y el asignador nativo con `nativeheap.Return`. La segunda
  no es un detalle: el heap de Go aparca entre 2,4 y 4 MB por encima del
  snapshot vivo en cada construcción, así que lo que hacía crecer a un servidor
  no era suyo. No se fija `GOMEMLIMIT`, que el hijo de la pasada heredaría del
  entorno -`indexing/subprocess.go` no fija `command.Env`- y que en el lado de
  Go no tendría hueco que cerrar. Ver ADR 0044.
- La memoria del motor es de libc, no de Go, y ninguna métrica de `runtime` la
  ve. glibc da una arena por hilo que compite y la hace crecer antes de
  reutilizar nada, así que cada reconstrucción conserva otra arena hasta que
  saturan. Medido en `devlabs` sobre un grafo de 189 MB, cuatro construcciones:
  con sólo el scavenge de Go el RSS va de 309,5 a 511,1 MB -67 MB por
  construcción-, y devolviendo también el nativo se queda en 241,5-249,6 MB, sin
  coste en tiempo. Acotar las arenas desde el proceso no sirve y está medido:
  `mallopt` después de arrancar no mueve lo que ya vive en arenas secundarias.
- Una generación lleva su HotSnapshot en `snapshot.kvsnap`, y un servidor lo lee
  en vez de derivarlo del grafo canónico: `rebuild.LoadOrBuildSnapshot` es la
  puerta, y las tres rutas que instalan una generación -el arranque de `serve`,
  el seguidor y el padre de una pasada- pasan por ella. Lo que prueba que el
  fichero pertenece a esa generación es su propio `snapshot.sha256`, repetido en
  la cabecera: un delta incremental muta la generación en sitio y refresca ese
  digest, así que un snapshot del grafo anterior deja de cuadrar sin que nadie
  tenga que acordarse de borrarlo. Escribirlo es una economía y nunca una
  precondición -- un fichero ausente, ajeno, rancio o corrupto cuesta una
  derivación, se declara en el informe y jamás una respuesta. Medido en
  `devlabs`: 253-255 MB de RSS y 787-836 MB de pico derivando, contra 150-152 MB
  y 264-269 MB leyendo. Ver ADR 0045.
- `doctor` deriva el snapshot y nunca lee el publicado. No es un descuido que
  optimizar: informa de si **este grafo** todavía puede convertirse en snapshot,
  y leer un fichero escrito cuando el grafo estaba sano contestaría otra
  pregunta, y la contestaría tranquilizando.
- Cada servidor sigue guardando su propia copia de lo que leyó: `Private_Dirty`
  es el 100 % del RSS y tres clientes son tres copias. Compartir páginas es
  mapear el fichero, no leerlo, y eso es la fase 2 del ADR 0045; su condición es
  que `SymbolRecord.StableKey` deje de ser una `string`.
- El fichero publicado se **mapea** para decodificarlo y el mapeo se libera en
  cuanto acaba la decodificación, en vez de leerlo al heap: eran 73 MB asignados
  por carga sobre el corpus real, y ahora dos procesos que cargan la misma
  generación leen las mismas páginas físicas mientras lo hacen. Eso es seguro
  por una razón que **tiene que seguir siendo verdad**: todo decodificador del
  formato copia -un registro a un struct, una cadena por una conversión que
  asigna-, así que el snapshot no comparte nada con esos bytes. Un decodificador
  que empezara a entregar una vista convertiría esto en un use-after-free que
  contesta consultas en vez de romperse. El guardia es el test que carga, borra
  el fichero y luego recorre cada símbolo, cada clave estable, cada cadena y
  cada arista.
- La tabla de cadenas resuelve `Lookup` por búsqueda binaria sobre los ids
  ordenados por su valor, no con un mapa: eran 20,45 MB por proceso contra 1,9 MB,
  y `Lookup` se llama una o dos veces por consulta. El orden lo lleva el fichero
  en su propia sección -es derivado y determinista, así que el escritor ordena
  una vez-, es opcional en las dos direcciones, y se valida exigiendo valores
  estrictamente crecientes: es la única sección cuya corrupción sería silenciosa,
  porque un orden válido de otra tabla sigue contestando con el id de otro valor.
- El layout del visor es una proyección derivada del `HotSnapshot`: usa
  coordenadas enteras deterministas, contención repository/package/file/symbol
  y un grid espacial con overflow acotado; nunca muta el snapshot ni ejecuta
  force simulation global.
- El layout equilibra cada rejilla por defecto (`Columns: 0`) para que el mundo
  publicado quede aproximadamente cuadrado; una anchura fija lo convierte en
  una tira ilegible cuando crece el número de repositorios.
- El payload binario `LGVB` del visor es versionado (`v2`), little-endian y
  limitado a `32 MiB`; lleva una sección de etiquetas con el nombre de cada
  nodo, el servidor valida magic, versión, offsets, longitudes y `snapshot_id`
  antes de servirlo, y los errores de incompatibilidad son códigos estables.

## Configuración y registro

- El vocabulario de lenguajes es `config.SupportedLanguages` y se valida al
  escribir el registro, no sólo al indexar: `init` no acepta lo que la pasada
  rechaza.
- Un ajuste de la configuración vale exactamente lo que el código implementa.
  `indexing.generated_files` sólo acepta `include` y
  `indexing.unresolved_references` sólo `retain`, porque eso es lo que la
  pasada hace: aceptar otra palabra promete un comportamiento que no existe y
  convierte una errata en una configuración silenciosamente distinta de la que
  se leyó.
- Una configuración escrita fuera de la ubicación por defecto es autocontenida:
  su estado, su caché y su registro cuelgan de su propio directorio. Un
  `--config` en `/tmp` que apuntase al estado real publicaría generaciones
  sobre el grafo que venía a dejar en paz.
- Los nombres de repositorio se comparan exactos. El nombre es un
  identificador, nunca un componente de ruta, y las stable keys que lo llevan
  distinguen mayúsculas: dos repositorios que sólo difieren en el caso son dos
  repositorios y tienen que poder registrarse con su nombre real.
- Toda ruta de estado nueva se reubica en `stateBesideConfig`, no sólo se añade
  a `DefaultConfig`. Es lo que hace autocontenida la regla de arriba: una ruta
  que se olvide ahí manda su contenido a la instalación real desde una prueba
  bajo `/tmp`. `logging.event_log_path` es la última que entró.

## Servidores HTTP, assets y procesos

- `kivgraph ui` es opt-in y sirve solo el `HotSnapshot` publicado por HTTP
  read-only. Su bind por defecto es `0.0.0.0:7777`: el grafo se indexa donde
  están los repositorios y el visor se mira desde otra máquina, así que un
  default loopback obligaba a editar la configuración en el caso normal. La
  guarda es entonces la advertencia, y por eso no es decorativa: todo bind que
  no sea loopback registra qué se expone -rutas de repositorio y de fichero,
  nombres y firmas de símbolos- y con qué se cierra. El endpoint no lleva
  autenticación; restringirlo es `--addr` o `web.address`.
- `kivgraph serve` permanece STDIO y no abre HTTP; `webapi.Run` es dueño del
  listener y ejecuta un cierre graceful acotado al cancelar el contexto.
- `internal/webassets` sirve solo la copia generada de `web/dist` cuando la
  distribución se construye con el tag `webassets`; los binarios sin tag
  devuelven un fallback visible `503` en vez de servir archivos no declarados.
- La enumeración de procesos vive en `internal/procstat`, el único lugar que lee
  estadísticas de proceso del sistema operativo, con un fichero por plataforma:
  `/proc` en Linux y `kern.proc.all` más `kern.procargs2` en macOS. Sólo se ven
  los procesos de este usuario, que son exactamente los que se podrían señalar;
  una plataforma que no sabe enumerar devuelve error, no una lista vacía, y `0`
  significa desconocido.

- `internal/eventlog` es el registro durable de lo que pasó: una pasada, una
  llamada de tool, el ciclo de vida de un servidor. Es un fichero JSON-lines de
  solo-añadir, y esa forma es la que lo hace seguro para los varios procesos que
  lo escriben a la vez: un registro es una sola escritura de mucho menos que un
  buffer de tubería sobre un descriptor `O_APPEND`, que POSIX mantiene entera.
  No hay cerrojo entre procesos y no se lee antes de escribir; añadir cualquiera
  de las dos cosas no compraría nada. Ver ADR 0049.
- **No es una API.** Es estado derivado y no lleva versión de esquema: borrarlo
  pierde historia y nada más. Un lector salta una línea que no parsea en vez de
  fallar la lectura, porque el fichero lo escriben varios procesos y uno puede
  ser de una versión que conoce campos que otra no.
- Un sumidero que no se puede abrir **degrada con un aviso**, nunca falla el
  trabajo que describe. Un `*eventlog.Writer` nulo descarta, así que ningún
  productor lleva una rama para ese caso; y `Append` no devuelve error por la
  misma razón.
- La medición por tool ya existía y sigue donde estaba: `tools.observe` es el
  único punto por el que pasan las nueve tools de consulta, y `metrics.Registry`
  acumula sus atómicos para que `graph_status` los lea de vuelta. Lo que se
  añadió es un segundo canal lateral, `metrics.QueryRecorder`, simétrico con el
  puente OpenTelemetry: el registro no puede ser la fuente de un comando porque
  muere con el proceso. `QueryRecorder` corre dentro del camino caliente de una
  llamada, así que no puede bloquear.
- `index_project` recibe observadores variádicos como todo `Register*` de
  `internal/mcp/tools`. Los tuvo tarde, y mientras no los tuvo era la única
  llamada que un cliente podía hacer que ningún contador veía -- justo la que
  cuesta minutos.

## Plataforma

- En macOS, la inspección de locks de LadybugDB nunca abre la base: POSIX
  libera todos los locks de registro de un proceso al cerrar cualquier
  descriptor sobre el archivo, así que un `fcntl(F_GETLK)` desbloquearía el
  motor. Se enumeran descriptores con `libproc`.
- El watcher declara su contrato por backend. Bajo `kqueue` un archivo creado
  y escrito de una vez llega como un único `Create`, y el backend consume un
  descriptor por archivo vigilado: un árbol que excede el límite falla en
  `New` nombrando el límite, nunca vigila un subconjunto en silencio.
- La política que rechaza rutas con componentes symlink no se relaja para
  acomodar `/var` ni `/tmp` de macOS. Los tests que alimentan la capa de
  workspace usan `internal/testsupport.TempDir`.

## Integraciones de clientes

- `kivgraph mcp install` y `kivgraph skill install` detectan raíces locales
  conocidas cuando falta `--target`, muestran una selección interactiva de uno o
  varios clientes y conservan `--target` para automatización; nunca inicializan
  Kivgraph ni indexan repositorios.
- La selección interactiva usa Bubble Tea y Lip Gloss: `↑`/`↓` o `j`/`k`
  mueven, `space` alterna, `a`/`n` seleccionan todos/ninguno, `Enter`
  confirma y `q`/`Esc` cancela; respeta `NO_COLOR` y no emite ANSI al
  redirigir la salida.
- Los adaptadores externos validan JSON/TOML antes de modificarlo, rechazan
  destinos symlink, escriben atómicamente con `0600` y exigen `--force` para
  sustituir o retirar una entrada incompatible. Las entradas idempotentes no
  reescriben el archivo.
- La skill canónica vive en
  `internal/integrations/assets/kivgraph/SKILL.md`; los bundles deben
  incluirla bajo `skills/kivgraph/SKILL.md` y en `SHA256SUMS`.

## Verificación

```bash
gofmt -l <archivos-go-modificados>
go vet ./...
go test ./...
```

Si el cambio toca LadybugDB nativo, `make test-ladybug`; `PKGS` acota la pasada
a un paquete. El procedimiento completo está en la skill
`.claude/skills/running-tests/SKILL.md`.
