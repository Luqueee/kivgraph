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
  hasta la siguiente petición. No se fija `GOMEMLIMIT` de entorno, que el hijo
  de la pasada hereda -`indexing/subprocess.go` no fija `command.Env`- y cuyo
  pico es el trabajo mismo. Un límite fijado en proceso sí sería sólo del
  servidor, pero tendría que dejar pasar el pico de esta construcción, así que
  el pico es la palanca y no el techo.
- Cada servidor construye su propio HotSnapshot, también el que sólo sigue una
  generación que publicó otro: una generación en disco es `graph.db` y su
  digest, nunca el snapshot. Medido en `devlabs` sobre 42 repositorios y un
  grafo de 189 MB -102.881 símbolos-: un servidor recién cargado son 344 MB de
  RSS con un pico de 768 MB, cada reconstrucción de seguidor retiene 61 MB más,
  y uno que lleva diez encima se estabiliza en 1,07-1,13 GB con el 100 % en
  `Private_Dirty`. Con tres clientes son 2,6 GB para servir el mismo grafo
  inmutable, y diez publicaciones en ochenta minutos son diez reconstrucciones
  por servidor.
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
