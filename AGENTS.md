# Instrucciones de desarrollo de Ladygraph

Estas reglas aplican a todo el repositorio. Una instrucción más cercana a un
archivo puede añadir restricciones, pero no puede relajar los contratos de
integridad, compatibilidad o verificación descritos aquí.

## Identidad del proyecto

- Proyecto: `Ladygraph`.
- Módulo Go: `github.com/Luqueee/ladygraph`.
- Ejecutable principal: `cmd/ladygraph`.
- Worker TypeScript: `ts-worker/`, paquete privado `@ladygraph/ts-worker`.
- LadybugDB es el almacenamiento canónico; el HotSnapshot es una proyección
  derivada y no una fuente alternativa de hechos.
- Los identificadores históricos `LUQUE-####` del backlog no se renombran.

## Qué pregunta contesta cada tool de Ladygraph

Este bloque es el canal de enrutado portable: `CLAUDE.md` es un enlace a este
fichero, así que Oh My Pi y Claude Code lo cargan los dos sin que nadie lo pida.
El campo `instructions` del servidor dice lo mismo, y Zed no lo lee.

| la pregunta | la tool |
| --- | --- |
| quién llama a esto, qué referencia a esto | `find_references` |
| qué se rompe si lo cambio | `get_blast_radius` |
| qué alcanza esto hacia fuera | `trace_dependencies` |
| quién lo usa desde otro repositorio | `find_cross_repo_consumers` |
| dónde está declarado | `find_symbol` |
| qué hay declarado en este paquete | `get_file_outline` |
| dame el código de estos símbolos | `get_source` |
| ¿está el grafo al día? | `graph_status` |

Las aristas las resuelven `go/types`, el checker de TypeScript y
`rust-analyzer`, no la coincidencia de nombres: una lista de referencias vacía
significa que **nadie lo llama**, no que no se encontró. `grep` no puede decir
eso, y tampoco distingue dos métodos homónimos.

Toda fila trae repositorio, ruta, nombre cualificado y rango de líneas, y toda
tool acepta esa tripleta en vez de una clave estable: la llamada siguiente se
construye con la respuesta que ya se tiene.

**Dónde pierde, y conviene no gastar la llamada:** un nombre raro en un solo
repositorio pequeño lo resuelve `grep` más barato -medido: `1,08x` contra
`7,69x` de un nombre común-, y el índice de un fichero pequeño cuesta más que
leerlo. Gana en nombres comunes, en impacto transitivo, en consumidores de otro
repositorio y en demostrar una ausencia.

## Herramientas MCP en Oh My Pi

- Las rutas `xd://` se descubren consultando `xd://`; nunca se construyen
  concatenando prefijos a partir del nombre visible de una herramienta.
- Las herramientas directas de Ladygraph usan
  `xd://mcp__ladygraph_<operación>`; a través de 1MCP el nombre agregado es
  `ladygraph_1mcp_<operación>`.
- No se debe inventar una forma `xd://mcp__mcp_ladygraph_<operación>`.
- Una respuesta MCP `tools/list` puede estar paginada: seguir `nextCursor`
  hasta `null` antes de concluir que una herramienta no está montada.

- `ladygraph_1mcp_index_project` es la única herramienta MCP mutante de
  Ladygraph; solo se registra en la ruta `serve` configurada y exige
  consentimiento explícito del cliente antes de cambiar el registro de
  repositorios o publicar una generación.

## Antes de editar

1. Leer `TASKS.md`, sus dependencias, el gate aplicable y la documentación del
   subsistema afectado.
2. Inspeccionar implementaciones, tests y consumidores existentes; reutilizar
   la convención vigente en vez de crear una segunda.
3. Definir el comportamiento observable y sus casos negativos antes de tocar
   código.
4. No modificar repositorios indexados ni artefactos de entrada usados por los
   benchmarks. Las pruebas deben usar copias o fixtures privados.
5. No ocultar warnings, errores, referencias no resueltas, limitaciones ni
   resultados `FAIL`.

## Contratos semánticos que no se pueden relajar

- Una arista `EXACT` requiere evidencia suficiente y la procedencia correcta.
  Nunca se crea por coincidencia de nombre, texto, path, alias o candidato
  único.
- `CANDIDATE` y `UNRESOLVED` son resultados distintos de `EXACT`.
- Cada arista canónica tiene `confidence`, `provenance` y, cuando corresponde,
  `evidence_key`; la evidencia debe estar observada en un `File`.
- Las stable keys son persistentes. No cambiar su algoritmo, identidad
  canónica ni el namespace histórico `luque-stable-key` sin migración de datos,
  ADR y actualización explícita del contrato.
- El schema LadybugDB es versionado. Un cambio incompatible requiere full
  rebuild o migración documentada; nunca se modifica una base existente en
  silencio.
- En un delta incremental, todo hecho afirmado por un archivo se retira y se
  vuelve a afirmar junto con ese archivo. Las aristas de paquete también se
  retiran por su evidencia aunque sobrevivan sus dos extremos.
- Cada `UNRESOLVED` conserva motivo, repositorio y lenguaje; cuando existe una
  ocurrencia concreta conserva su archivo, posición y detalle observados.
  Los fallos de módulo a nivel de repositorio pueden no tener archivo y nunca
  se les fabrica evidencia ni una arista `EXACT`.

## Go

- Ejecutar `gofmt` en cada archivo Go modificado.
- Propagar errores con contexto usando `%w`; no descartar errores.
- Las funciones bloqueantes reciben `context.Context` como primer argumento
  después del receptor.
- Toda goroutine tiene propietario y cancelación claros.
- Los datos se validan antes de construir nodos o aristas.
- Los identificadores que mezclan dominios usan tipos definidos.
- Los paquetes bajo `internal/` no son API externa estable.
- Cambiar un símbolo exportado exige revisar todos sus consumidores y tests.
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
- `go/types` viaja enlazado en el binario: Ladygraph solo comprueba tipos hasta
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
- `ladygraph serve` y `ladygraph ui` siguen la generación publicada: cargan el
  HotSnapshot al arrancar y republican cuando el puntero `CURRENT` avanza, sin
  coordinarse con nadie -- `SnapshotStore.Publish` solo acepta una generación
  estrictamente más nueva. Un `index --full` en otra terminal no puede dejar a
  un servidor sirviendo un grafo que ya no existe en disco.
- `doctor` informa del techo de versión con el que este binario comprueba
  tipos, no solo del `go` del PATH: son números distintos y el que decide si un
  repositorio se puede indexar es el primero.
- Un nombre de paquete TypeScript declarado por varios manifests es una
  ambigüedad, no un repositorio roto: ningún manifest lo provee, ambos salen
  del registro y se declara `AMBIGUOUS_PACKAGE_PROVIDER`. Es el mismo trato que
  recibe un módulo Go con varios proveedores.
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
- `ladygraph clean` retira generaciones publicadas: enumera y no toca nada sin
  `--yes`, porque no hay deshacer -- también se lleva el backup del que vive
  `rollback`. Sin flags deja el store vacío y libera la reserva de espacio;
  con `--keep-active` conserva exactamente la generación publicada. Nunca toca
  la configuración ni el registro de repositorios.
- Tras un `clean` completo la numeración vuelve a `000001`, y
  `SnapshotStore.Publish` solo acepta una generación estrictamente más nueva:
  un servidor vivo conserva el grafo que ya no existe y no instalará ninguno
  más. El seguidor lo declara una vez y el comando avisa de reiniciar.
- `index_project` es idempotente: un proyecto ya registrado con el mismo
  directorio se reindexa sin tocar el registro, y un cambio de lenguajes
  conserva las `exclusions` que la petición no puede expresar. Solo un nombre
  ocupado por otro directorio es conflicto, y el error nombra el registrado.
  `clean` nunca retira repositorios: reconstruir lo registrado es
  `index --full`.
- Un cliente MCP lanza el servidor él mismo, así que `serve` y `ui` escriben la
  configuración por defecto cuando no existe y siguen adelante: salir porque
  nadie ejecutó `init` convierte instalar la integración en una sesión de
  terminal y el cliente solo informa de que el servidor falló. No registran
  ningún repositorio ni indexan nada. Una configuración existente que no se
  puede leer es un fallo, nunca algo que sobrescribir.
- `index_project` emite `notifications/progress` cuando la petición trae
  `progressToken`: un rebuild completo dura minutos y un cliente MCP aplica su
  propio timeout a la llamada. Sin token no se instala callback alguno.
- `index_project` acepta un lote (`projects`) y reconstruye **una sola vez**.
  Un rebuild resuelve las aristas cross-repository sobre el conjunto completo
  de hechos, así que cuesta el corpus entero se añada lo que se añada: llamar
  una vez por proyecto paga ese coste una vez por proyecto y tira todos los
  grafos menos el último. La forma de un solo proyecto se conserva; mezclar
  ambas en una petición se rechaza.
- Una petición de indexación se construye en un solo sitio,
  `indexing.OptionsFromConfig`: el llamador sólo añade lo que la configuración
  no decide -repositorios, directorio de trabajo, versión del resolver y los
  sinks de progreso-. Construirla dos veces es cómo `index_project` acabó sin
  nombrar ningún campo de Rust y fallando con «the Rust analyzer command is
  required» sobre la misma configuración que el CLI indexaba sin problema.
  Su resultado informa de los contadores de los tres lenguajes: un lenguaje
  ausente del informe se lee como un lenguaje sin código.
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
- `ladygraph ui` registra la dirección que ha enlazado, incluida la que
  resuelve un puerto `0`, y se niega a arrancar cuando el binario no lleva el
  tag `webassets`: el bundle MCP publicado no lo lleva, así que solo podría
  servir la página de «bundle no disponible».
- Un comando puntual clasifica lo que escribe en `stderr`: el progreso es
  `INFO` y sólo un fallo es `ERROR`, y el texto de la línea es el `msg` del
  registro, nunca un campo dentro de un mensaje fijo. Un registro que un
  lector no puede filtrar por nivel ni encontrar por texto no informa de nada.
- La ayuda marca el comando que esta build no puede ejecutar. El bundle MCP
  publicado no lleva `webassets`, así que anunciar `ui` sin decirlo describe
  una capacidad que no existe.
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
- `graph_status` no informa de lo que este proceso no usa ni midió. `serve`
  responde desde el HotSnapshot publicado: no abre la base ni ejecuta el
  worker, así que los declara `not_applicable` diciendo por qué, y las
  secciones de métricas que nadie observó se omiten en vez de valer cero.
- Una consulta por símbolo solo cuenta como consumidor lo que se observó sobre
  ese símbolo. `find_cross_repo_consumers` devuelve las dependencias de
  paquete, que prueban que el consumidor depende del proveedor y nunca que use
  el símbolo, en su propio contador `coverage.package_level`; sumarlas a
  `exact` informa de un uso que nadie vio. Por lo mismo, un fallo de
  resolución que no nombró símbolo -un módulo ilegible, un proveedor ausente-
  pertenece al paquete y se declara con `requested_package`, nunca atribuido a
  cada símbolo que ese paquete exporta. Esa lista salió de la superficie del
  modelo en la fase 19 -es una pregunta sobre el índice, no sobre el código- y
  vive en el CLI.
- Un diagnóstico del cargador que no tumba la pasada se imprime, no sólo se
  cuenta; un repositorio TypeScript que no declara ningún paquete se nombra.
  Un contador sin detalle y una entrada de registro que no aporta nada son
  dos formas de callar.
- Una respuesta viaja por **un solo canal**. Ninguna tool publica `outputSchema`,
  porque el SDK entonces marshala el resultado tipado a `structuredContent` y
  repite el mismo JSON en el bloque de texto: la respuesta se paga dos veces.
  `get_source` va más allá y contesta en prosa: 302 tokens de fuente son 374
  dentro de una cadena JSON y 430 como fila, que es lo que cuesta la lectura de
  rango del anfitrión, así que servir código por el envoltorio no compra nada.
- Toda fila que nombra un símbolo se puede abrir sin otra llamada: lleva
  repositorio, ruta, nombre cualificado y **el rango completo**. Y toda tool
  acepta esa tripleta en lugar de la clave estable, que son 35 tokens de base32
  que sólo este servidor lee. Un nombre ambiguo no se resuelve en silencio: el
  error nombra los candidatos por dónde están, y si repositorio y ruta ya no los
  separan, ofrece las claves.
- Sin generación publicada no hay superficie: el servidor completa el handshake,
  publica cero tools de consulta y pone el comando de reconstrucción en
  `instructions`. Un cliente lanza el proceso él mismo, así que salir se lee como
  una caída, y publicar tools que contestan `INDEX_NOT_READY` a todo enseña al
  agente que las tools no funcionan.
- Lo que un anfitrión mantiene residente es el nombre de la tool y su
  descripción, no su esquema: Oh My Pi lee el esquema bajo demanda y Claude Code
  lo difiere. Ahí vive el enrutado -contra qué alternativa nativa compite cada
  tool y **dónde pierde**- y por eso ninguna descripción ni `instructions` puede
  llevar un número derivado del grafo: reescribiría el prompt de sistema del
  cliente en cada reindexado. Los datos volátiles se piden con `graph_status`.
- Una respuesta declara lo que su recuento significa sólo cuando la cifra
  engaña. Cero filas se lee como «no existe» salvo que algo diga que es una
  ausencia comprobada; una página truncada no dice si contiene lo que importaba.
  Con filas y sin truncar, `guidance` calla: quince tokens de consejo en cada
  llamada es cómo un ahorro se convierte en un coste.
- `serve` puede leer los ficheros de los repositorios registrados **sólo para
  entregar bytes**. Falla cerrado en lo que afirma y degrada declarando en lo que
  entrega: si el fichero ya no cuadra con el `ContentDigest` de la generación, el
  fichero es la autoridad, la declaración se reancla por nombre y el
  desplazamiento se declara; si no queda o queda dos veces, esa fila no da bytes
  y las demás sí. Reanclar no crea ninguna arista. Ver ADR 0040.
- El coste en tokens de la superficie se mide, no se opina:
  `benchmarks/mcp-token-cost` compara cada pregunta contra la vía nativa del
  anfitrión con sus salidas **capturadas literalmente**, publica el factor de
  responder y el de la sesión completa -uno solo engaña- y declara el gate desde
  su digest.

## TypeScript

- El worker usa TypeScript estricto y módulos ESM.
- Los límites de proceso, protocolo y adaptadores tienen tipos explícitos; `any`
  requiere una justificación local.
- `stdout` contiene únicamente framing/protocolo. Los logs van a `stderr`.
- Todo recurso persistente se cierra al cancelar o terminar el proceso.
- Las promesas rechazadas se clasifican en el límite adecuado; no se ocultan
  con aserciones.
- No editar `ts-worker/dist` manualmente: regenerarlo con `pnpm build`.
- La indexación TypeScript procesa providers `package.json` nombrados con
  `ProjectPath` resuelto; pasa ese path al worker en cada invocación y omite
  manifests sin proyecto en vez de adivinar el `tsconfig` de la raíz.
- La identidad de un símbolo de otro repositorio sale siempre del proyecto del
  proveedor. Si su `.d.ts.map` coloca el símbolo, esa es la posición; si no lo
  hay pero el puente nombró la fuente, se le pregunta a su checker qué
  declaración exporta ese módulo bajo el nombre pedido. Las dos aristas son
  exactas y **no valen lo mismo**: la primera es
  `EXACT_TYPECHECKED`/`TYPESCRIPT_CHECKER` y la segunda
  `EXACT_PACKAGE_MAPPED`/`TYPESCRIPT_PROJECT_REFERENCE`, porque el paso de
  artefacto a fuente lo afirma la configuración de compilación del proveedor y
  no un mapa que emitiera. Sin fuente nombrada no se pregunta nada: la
  referencia queda `UNRESOLVED`. Ver ADR 0038.
- Un fixture cross-repository que resuelve al proveedor con `paths` no prueba
  nada sobre la forma que instala un gestor de paquetes. El consumidor llega
  al proveedor por un symlink de `node_modules` y el motor devuelve la ruta
  del destino del enlace, así que `consumer-linked` es el fixture que defiende
  el caso real; exigir `declarationMap` en el proveedor no lo arregla.
- El proyecto que resuelve la identidad de un símbolo importado es el que
  **posee el fichero** que nombra el mapa de declaraciones, no el paquete del
  que se importó. Un paquete fachada existe para reexportar los de su
  workspace, así que su mapa apunta al repositorio que los declara; preguntar
  al programa de la fachada por ese fichero no devuelve nada y la referencia se
  abandonaba como `PROVIDER_SOURCE_UNAVAILABLE` aunque la declaración estuviera
  indexada. La identidad acredita al dueño: componerla contra la fachada
  produce una clave que ese repositorio no publica y deja la arista colgando.
  La pertenencia es la raíz registrada más larga que contiene el fichero, y
  nadie posee lo que está bajo un `node_modules` -la copia instalada de un
  paquete cuelga de la raíz de quien lo instaló-.
- El worker que ejecuta una pasada es el del checkout cuando existe
  `ts-worker/dist` y nadie configuró otro: un shim instalado de una release
  anterior ocupa el mismo nombre en el `PATH` y ganaba, de modo que `pnpm
  build` no cambiaba nada observable y la medición salía mal con el aspecto de
  estar bien.
- La aplicación web en `web/` mantiene TypeScript estricto, ESM, Biome y
  Vitest; los payloads binarios grandes permanecen fuera del estado React y
  `web/dist` se regenera con el build de Vite.
- `web/` es solo el previsualizador del grafo: `App` monta el visor a pantalla
  completa y no contiene landing, cabecera ni secciones de presentación. El
  visor es oscuro por construcción (`class="dark"` y `darkTheme` de Reagraph).
- Los componentes UI que se necesiten deben añadirse con
  `pnpm dlx shadcn@latest init`/`add` y Tailwind CSS, no a mano ni con una
  segunda librería de estilos; el paquete no vendoriza primitives sin uso.
- `web/src/renderer` recibe el payload `LGVB` versionado como `ArrayBuffer`,
  conserva sus vistas fuera de React y solo materializa el límite visible que
  consume `reagraph`; el adaptador rechaza payloads que excedan el límite en
  vez de truncar silenciosamente la topología.
- `ladygraph ui` es opt-in y sirve solo el `HotSnapshot` publicado por HTTP
  read-only. Su bind por defecto es `0.0.0.0:7777`: el grafo se indexa donde
  están los repositorios y el visor se mira desde otra máquina, así que un
  default loopback obligaba a editar la configuración en el caso normal. La
  guarda es entonces la advertencia, y por eso no es decorativa: todo bind que
  no sea loopback registra qué se expone -rutas de repositorio y de fichero,
  nombres y firmas de símbolos- y con qué se cierra. El endpoint no lleva
  autenticación; restringirlo es `--addr` o `web.address`.
- `ladygraph serve` permanece STDIO y no abre HTTP; `webapi.Run` es dueño del
  listener y ejecuta un cierre graceful acotado al cancelar el contexto.
- `ladygraph stop` termina los procesos largos de este usuario -- `serve` y
  `ui` -- y nada más. Selecciona por invocación, no por ejecutable: una
  indexación en curso son minutos de análisis y no se tira, y el propio `stop`
  no se mata a sí mismo. Manda `SIGTERM`, espera el cierre graceful acotado y
  sólo entonces `SIGKILL`, y antes de escalar vuelve a comprobar que el pid
  sigue siendo la misma invocación: un pid liberado durante la espera puede ya
  pertenecer a otro proceso. `--dry-run` enumera sin señalar.
- La enumeración de procesos vive en `internal/procstat`, con un fichero por
  plataforma: `/proc` en Linux y `kern.proc.all` más `kern.procargs2` en
  macOS. Sólo se ven los procesos de este usuario, que son exactamente los que
  se podrían señalar, y una plataforma que no sabe enumerar devuelve error, no
  una lista vacía.
- `internal/webassets` sirve solo la copia generada de `web/dist` cuando la
  distribución se construye con el tag `webassets`; los binarios sin tag
  devuelven un fallback visible `503` en vez de servir archivos no declarados.
- El visor consulta `navigator.gpu` antes de montar WebGPU; sin un adaptador
  utilizable muestra el motivo y conserva WebGL. La ruta WebGPU usa materiales
  nodo y nunca se declara activa por una mera propiedad de configuración.
- La fábrica WebGL del visor desactiva antialiasing; `FrameGovernor` limita el
  DPR a `1` mientras el puntero se mueve - también al pasar por encima, no sólo
  al arrastrar - y durante un gesto sostenido pausa además el picking y oculta
  las etiquetas con `visible = false`. Todo se restaura al quedar inactivo, y
  cada cambio del drawing buffer se repinta sincrónicamente antes de exponerse
  al navegador, para no mostrar un frame negro.
- Las etiquetas no se desmontan durante un gesto: `labelType` permanece fijo
  porque conmutarlo reconstruye todas las mallas de glifos y detiene el
  arrastre más de `130 ms` en cada extremo del gesto.
- El rótulo del nodo apuntado no vive en el estado del visor: viaja por un
  canal propio (`createStatusChannel`) porque un `setState` por nodo rozado
  reconstruye el árbol de elementos de todo el grafo. La selección y su
  vecindario son un único estado y se aplican dentro de `startTransition`.
- Las geometrías instanciadas de Troika parten con `instanceCount: 0` hasta
  que llega el atlas de glifos asíncrono y no llaman a `dispose()` al sustituir
  un atributo: `troika-three-text@0.52.5` permanece fijado y parcheado porque
  WebGPU rechaza el valor por defecto `Infinity` y porque liberar los buffers
  de una geometría viva deja al backend enlazando un índice inexistente.
- Antes de construir el `WebGPURenderer`, el visor resuelve `instanceCount` en
  `InstancedBufferGeometry` con la misma regla que WebGL - el mínimo de
  `meshPerAttribute * count` entre los atributos instanciados, `0` sin
  ninguno - para que ninguna librería pueda llevar un `Infinity` a
  `drawIndexed`.
- `site/` es la landing pública y la documentación de usuario, en Astro con
  Starlight, y es su propio proyecto pnpm como `ts-worker/` y `web/`. No entra
  en ningún bundle: el payload de `scripts/build-bundle.sh` es una lista blanca
  y el workflow de release comprueba que ni el directorio ni una línea de
  `SHA256SUMS` lo nombran. Se verifica con `make site-check` y `make
  site-build`, y se sirve con `pm2 start site/ecosystem.config.cjs` en el
  puerto `6767`.
- La documentación de `site/` está escrita para quien usa Ladygraph y sale de
  las fuentes del repositorio -`cmd/ladygraph/help.go`, `internal/config`,
  `internal/mcp/tools`, `README.md`-, copiadas literalmente cuando son un
  contrato. `docs/` sigue siendo material interno de ingeniería -ADRs,
  informes de cualificación- y no se publica.
- El lienzo del hero (`site/src/lib/hero-graph.ts`) es un grafo sintético en
  2D, sin three.js ni reagraph, y no importa nada de `web/`. Toma la paleta de
  las propiedades `--color-graph-*` de `global.css`, que son los literales de
  `web/src/renderer/reagraph.ts`, y dibuja bajo demanda: un
  `IntersectionObserver` detiene el bucle fuera de pantalla y
  `prefers-reduced-motion: reduce` pinta un solo fotograma sin instalar bucle
  alguno.

## Rust

- Rust no se analiza en proceso: la autoridad es `rust-analyzer scip`,
  invocado como proceso externo una vez por workspace Cargo. Tree-sitter no
  produce identidad ni resolución; aporta la clase sintáctica del uso y la
  visibilidad declarada, igual que el AST de Go aporta `GO_AST_CALL` sobre una
  resolución de `go/types`.
- El bundle lleva `bin/rust-analyzer`, fijado en `tools/manifest.json` por
  versión, URL y digest, y descargado por `scripts/fetch-rust-analyzer.sh`. En
  ejecución gana el binario que viaja junto al ejecutable, después una ruta
  explícita de la configuración y por último el `PATH`; `doctor` dice cuál
  respondió y `ladygraph version --json` publica su release. Lo que el bundle
  no lleva es un toolchain de Rust: sin `cargo` el analizador no carga el
  workspace, así que `doctor` lo comprueba aparte y falla nombrándolo.
- El catálogo de `canonical_integrity.go` es la lista que la capa nativa usa
  para validar una arista. Una procedencia nueva que no entre ahí pasa todos
  los tests sin el tag `ladybug` y revienta la integridad al publicar; el
  guardia que lo impide vive en `canonical_catalog_test.go`, sin tag.
- El subcomando `scip` ejecuta build scripts siempre, así que la hermeticidad
  se impone desde fuera: `CARGO_TARGET_DIR` a un directorio de estado externo
  -`cargo.targetDir` no sirve, su valor es relativo al workspace-,
  `--offline --locked`, y una comprobación posterior de que el repositorio no
  ganó `target/` ni un `Cargo.lock` nuevo. `rust.allow_network` es la única
  salida declarada.
- La unidad de análisis es el workspace Cargo, no el crate: el analizador
  carga el workspace entero en cada invocación. El techo de concurrencia es
  bajo por memoria.
- La identidad estable de un símbolo Rust es su cadena SCIP -crate, camino de
  descriptores y sufijo-, nunca su firma: rust-analyzer no emite
  `SymbolInformation` para una declaración fuera de la raíz del workspace, así
  que un consumidor que dependiera de la firma no podría nombrar la clave que
  su proveedor publica. El `Discriminator` sale del disambiguador del
  descriptor.
- Los símbolos locales (`local N`) son un contador por documento: no son
  direccionables y nunca entran al grafo.
- Una referencia solo se convierte en arista si alguien publica su destino: el
  propio pase, o el repositorio proveedor registrado. Un símbolo del propio
  repositorio que el índice no define -el bloque `impl` al que apunta
  `-> Self`, que SCIP menciona y nunca define- se declara
  `DEFINITION_NOT_INDEXED`. Componer su clave y confiar en que otro la publique
  aborta la pasada entera con una arista colgante.
- El símbolo que contiene una referencia se obtiene del `enclosing_range` de
  las ocurrencias de definición del mismo documento, la más interna que la
  contiene. SCIP dice a qué símbolo resuelve un uso, nunca qué declaración lo
  contiene.
- `IMPLEMENTS`, `EXTENDS` y `OVERRIDES` de Rust no salen de
  `SymbolInformation.relationships`, que viaja siempre vacío: salen de la forma
  del `impl` y del bound, con los dos extremos resueltos por el analizador. El
  destino de un `OVERRIDES` se compone desde el símbolo del trait y solo se
  emite si el índice lo observó; una implementación que la gramática no ve
  -generada por una macro- queda ausente y no se adivina.
- Nombrar una función no es llamarla, y las tres formas en que Rust la mueve
  son tres relaciones distintas: argumento de una llamada
  (`PASSES_AS_CALLBACK`, con procedencia propia `RUST_SYNTAX_CALLBACK`, el
  espejo de `GO_AST_CALLBACK`), valor de un `let`, `const`, `static` o campo
  de un literal (`ASSIGNS_FUNCTION`), y resultado de un cuerpo, con `return` o
  como expresión final (`RETURNS_FUNCTION`). La gramática decide la clase y el
  analizador el destino, como en una llamada.
- Una clase de posición de valor exige además que el destino sea invocable y
  que esta pasada lo haya indexado: `takes(LIMIT)` es un argumento que no es
  un callback, y un destino de otro repositorio llega sin `Kind`. En ambos
  casos la arista degrada a `REFERENCES` en vez de afirmar lo que nadie leyó.
  El ascenso por la expresión atraviesa lo que no cambia lo nombrado -un
  camino, un préstamo, un literal de array o tupla- y nunca un acceso a
  campo: devolver `objeto.campo` no devuelve el objeto.
- `core`, `std` y `alloc` entran al grafo con `rust.index_sysroot`, apagado por
  defecto. Sin ellos, cuatro silencios medidos: `#[derive(...)]` no produce
  ninguna relación, la sobrecarga de operadores no alcanza su trait, el operador
  `?` no alcanza `Try::branch`, y toda llamada a la biblioteca estándar
  desaparece. Con ellos, los cuatro son aristas exactas. La ausencia sigue
  siendo una carencia declarada y nunca un fallo: una máquina sin toolchain o
  sin `rust-src` indexa sus repositorios y dice por qué no indexó la
  biblioteca. Ver ADR 0041.
- La unidad del sysroot es el workspace `library` y nada por debajo. Un
  toolchain vendoriza ahí dentro `backtrace`, `compiler-builtins`,
  `portable-simd` y `stdarch`, que no son la biblioteca estándar: seis de esos
  workspaces no cargan, aportan crates que nadie puede nombrar y sus build
  scripts hacen que dos pasadas del mismo toolchain publiquen distinto número de
  aristas. Se nombran en los diagnósticos y sus crates no se registran, porque
  registrar un proveedor que la pasada no indexa compone una clave que nadie
  publica.
- Un crate de la biblioteca se atribuye por **origen**, nunca por versión ni por
  nombre. Los dos lados de la frontera escriben cosas distintas en el campo de
  versión del moniker -el consumidor una URL, la biblioteca indexada como
  workspace `0.0.0`-, y la clave estable no lleva la versión, así que ambos
  componen la misma clave. Una referencia que nombra `core` con una release es
  `CRATE_VERSION_MISMATCH`; un repositorio registrado que declara un crate de la
  biblioteca, o dos toolchains a la vez, es `AMBIGUOUS_CRATE_PROVIDER`.
- El repositorio sintético se llama `rust:<release>` y ese namespace está
  reservado en el registro: `init` rechaza un nombre de usuario que lo tome. Eso
  es lo que hace del nombre la autoridad y evita una columna en el esquema
  canónico para responder si una fila es derivada. No lleva `commit`, `branch`
  ni `dirty`: nadie lo clona y nadie puede moverlo.
- La huella de la caché de hechos incluye `rustc --version`, así que un cambio
  de toolchain invalida cada hecho tomado de él. La biblioteca cuesta una pasada
  fría por toolchain y se sirve del caché en las siguientes.
- Una unidad acepta a crédito un destino atribuido a otro repositorio, porque no
  puede ver los hechos del proveedor. Cuando el proveedor no lo publica -`impl
  Add for u32` la genera `add_impl!` y no existe en ningún rango de código- el
  merge retira la arista y declara el uso como `PROVIDER_DEFINITION_NOT_INDEXED`
  con su crate, su símbolo y su posición; se cuenta en `EdgesWithoutProvider`.
  La descripción viaja con la unidad que compuso la clave, también en su entrada
  de caché: sin eso una pasada caliente no puede cerrar lo que la fría publicó.
- El índice de claves estables se construye sobre la lista **ordenada** de
  definiciones, primero gana. Dos símbolos del analizador pueden compartir una
  clave -no lleva la versión del crate, y rust-analyzer emite duplicados-, y
  recorriendo un mapa el orden de iteración decidía el ganador: dos pasadas del
  mismo corpus publicaban 170 aristas de diferencia en la raíz de `std`.
- El proveedor derivado se retira por defecto de las cuatro tools servidas que
  pueden devolver una de sus filas -`find_symbol`, `find_references`,
  `trace_dependencies` y `get_blast_radius`-, y lo anulan `include_derived` o
  nombrarlo en `repo`. Retirar una fila es una decisión de página y nunca una afirmación
  sobre lo observado: la arista sigue publicada con su confianza exacta.
  `graph_status` lo desglosa en `derived`, sus no resueltos incluidos -una arista
  pertenece al repositorio de su símbolo origen, que es el lado que observó- y
  `list_repositories` marca la fila. Sin ese desglose, un repositorio de diez
  símbolos responde `24.704`.
- La visibilidad de Rust no es solo `pub`: un miembro de un `trait` es tan
  visible como el trait, y un método de una implementación de trait es
  alcanzable a través de él. Leer únicamente el modificador publicaría una API
  pública falsa.
- El `Kind` publicado de un símbolo Rust es el fino de SCIP -`struct`,
  `trait`, `field`, `trait_method`-, no el sufijo del descriptor que decide la
  clave: con el sufijo, un struct, un enum y un alias son todos `type`.
- Los no resueltos de Rust se derivan de tres fuentes observadas -el registro
  de crates, el diff entre el inventario Tree-sitter y las definiciones SCIP
  del mismo archivo, y el fallo de carga del workspace-, porque el índice
  descarta en silencio los tokens sin moniker.
- Un nombre de crate declarado por varios repositorios es una ambigüedad:
  ninguno lo provee y se declara `AMBIGUOUS_CRATE_PROVIDER`. Una versión que
  el analizador no conoce (`.`) no identifica código y nunca resuelve.
- El descubrimiento Cargo no ejecuta `cargo`: lee los manifests con
  `BurntSushi/toml` y resuelve la pertenencia por directorio, como hace Cargo.
  Un crate sin workspace por encima es un workspace de uno.
- Un manifest que declara su propio `[workspace]` dentro del directorio de otro
  es una raíz legítima, no un error: así se vendoriza un árbol independiente y
  Cargo lo acepta -la biblioteca estándar lleva `library/backtrace` justo así-.
  Lo que Cargo rechaza es un manifest que sea raíz **y** miembro del workspace
  de encima, que llama «multiple workspace roots found in the same workspace», y
  los `members` se comparan como globs.
- La frontera de un repositorio Rust es una sola decisión, y la responde
  `workspace.CargoExcludes`: lo que el descubrimiento no camina -`.git`,
  `target`, `vendor`, `node_modules` y las `exclusions` configuradas- tampoco
  entra al análisis. Un crate vendorizado con `[patch.crates-io]` es código
  que Cargo compila, que el analizador indexa y que ningún manifest de este
  repositorio declara: publicar sus usos mientras se descartan sus
  declaraciones deja una arista colgante por cada uno y aborta la pasada. Sus
  usos se declaran `CRATE_PROVIDER_NOT_FOUND`, que es lo que son.
- Un paquete Cargo compila varios crates -biblioteca, binarios, build script,
  uno por test de integración- y el moniker SCIP nombra el paquete, así que
  sus módulos raíz llegan como un símbolo definido en varios documentos. Lo
  publica el target más alcanzable, con la ruta como desempate: sólo la
  biblioteca es enlazable y es la única que otro repositorio puede nombrar.
  Eso decide dónde vive el nodo y nunca cómo se llama; la clave no cambia.
- Un uso pertenece a una declaración de su propio documento. Si la más interna
  que lo contiene se publica en otro archivo, se sube al siguiente contenedor
  y, si no queda ninguno, el uso no tiene fuente: acreditarlo sitúa la
  observación en un archivo donde no ocurrió y el snapshot lo rechaza. Un
  fallo de resolución no necesita fuente y se declara igual, con su archivo y
  su posición.
- Ninguna arista se publica hacia un destino de este repositorio que la pasada
  no publica; se cuenta en `EdgesWithoutTarget`. Un destino de otro
  repositorio es otra cosa: está ausente a propósito y lo cierra el merge.
- Un símbolo que el analizador define en más de un documento se nombra en los
  diagnósticos, con el documento que lo publica y los que no. Ver ADR 0039.

## LadybugDB y snapshots

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
- El visor no muestra IDs densos: cada nodo se rotula con el nombre que el
  snapshot conoce, acortado en el lienzo y completo al pasar el cursor. Un ID
  denso solo es único dentro de su tipo de nodo, así que los extremos de una
  arista se resuelven por `(tipo, id)`.
- El visor no dibuja las coordenadas publicadas: deriva su propio layout 3D de
  la estructura del tile - contención, dependencias, comunidades y profundidad
  jerárquica - y lo calcula en el worker, una vez por tile. Una tile es una
  muestra del mundo, y la rejilla que empaquetó el servidor no dice nada sobre
  qué paquetes van juntos cuando falta la mayoría.
- Primero estructura y después física: los clusters se colocan y se relajan
  entre sí, cada nodo cuelga de su contenedor y sólo al final una relajación
  local corrige lo que la estructura no decidió. No hay simulación por frame.
- El layout es determinista: las direcciones salen del hash de la identidad
  del nodo, no de su posición en el tile ni de un generador aleatorio. El mismo
  tile dibuja siempre el mismo mundo.
- El radio de una concha es la suma cuadrática de los radios de sus hijos,
  nunca el mayor por la raíz del número: eso manda los hijos pequeños a la
  órbita del grande y deja el volumen intermedio vacío.
- Todas las distancias del layout son proporciones del radio con el que se
  dibuja un nodo. Reservar espacio en unidades ajenas a lo que se pinta produce
  o una maraña o un campo de puntos invisibles.
- Ningún eje puede colapsar: si uno queda más estrecho que la mitad del más
  ancho se reescala en torno al centroide. La profundidad que sólo existe en
  los números no vale nada.
- El visor dibuja la contención declarada por `parent_kind`/`parent_id` cuando
  el contenedor viaja en la misma tile, y una leyenda nombra cada color y cada
  trazo. La paleta se declara una sola vez en el adaptador.
- Ninguna arista del visor es discontinua: Reagraph construye una curva y un
  tubo por guion, y con una arista por nodo eso domina el frame. La distinción
  se hace con color y grosor; las aristas entre clusters se curvan para no
  atravesar el centro en línea recta.
- Sólo los repositorios y los hubs llevan rótulo permanente; el resto sale al
  pasar el cursor. Reagraph dibuja cada etiqueta a tamaño fijo en unidades de
  mundo, así que rotularlo todo sólo produce una mancha gris.
- Al posar el cursor sobre un nodo se ilumina su vecindario y se atenúa el
  resto; al salir se apaga. Reagraph sólo atenúa cuando hay selección, así que
  el nodo apuntado va en `selections` y lo que toca en `actives`. Encender y
  apagar esperan a que el cursor se pose: cada cambio del conjunto activo
  reconstruye las mallas de arista.
- La cámara encuadra la extensión proyectada sobre sus propios ejes, no la
  esfera envolvente, y abre fuera de eje. Un layout estructural nunca es una
  bola, y encuadrar una deja el grafo en un tercio de la pantalla.
- El visor dibuja bajo demanda: el bucle de render se detiene con el grafo
  quieto y despierta con los eventos de puntero del lienzo. Un grafo publicado
  no se mueve, y redibujarlo sesenta veces por segundo cuesta un núcleo a
  cambio de nada. Cualquier componente que refresque por su cuenta -el
  contador de FPS- vive fuera del lienzo: cada commit reaplica su `frameloop`.
- La jerarquía se lee por tamaño: el rango dibujado va de `4` a `22` unidades y
  el lienzo recibe esos mismos límites como `minNodeSize`/`maxNodeSize`, porque
  Reagraph reescala los tamaños que recibe. El extremo pequeño no baja de `4`:
  por debajo un símbolo deja de ser un punto y no es nada.
- La contención se atenúa según lo que sostiene y la separación entre hermanos
  la fija el nodo típico del tile, no el mayor. Un contenedor con todos sus
  trazos al mismo brillo es un erizo, y espaciar símbolos con la medida de un
  repositorio infla el mundo hasta hacerlos invisibles.
- Los nodos se dibujan con una geometría de esfera compartida y materiales
  compartidos por color y opacidad. Una esfera de `25 × 25` por nodo son mil
  quinientos triángulos para un punto de cinco píxeles.
- La contención no es una arista: viaja como pares de índices y se dibuja como
  una única malla de segmentos con color por vértice. Hay una por nodo, nada
  la selecciona, y como arista obligaría a reconstruir toda la geometría del
  grafo cada vez que se mueve el resaltado.
- El visor pide las tiles desde un Web Worker con `AbortController` por
  petición; el render permanece en el hilo principal. El número de nodos por
  vista es ajustable desde la interfaz, una tile recortada se declara como tal
  y un contador de FPS expone el coste de la elección.
- El benchmark end-to-end del visor se versiona en
  `benchmarks/web-viewer/`; el harness falla cerrado ante una métrica fuera de
  límite y no emite `WEB_VIEWER_PERFORMANCE_PASS` si el corpus o GPU no
  coinciden con la referencia declarada.
- El visor elige el nivel de detalle según los píxeles proyectados de la
  cámara, conserva histéresis entre `1.1` y `1` píxeles y eleva dependencias
  hacia contenedores visibles; cambiar de nivel sólo reproyecta durante un
  frame de interacción y nunca modifica el tile.
- Un upgrade de schema incompatible debe detectar la versión, respaldar y
  verificar la generación activa antes de reconstruir desde repositorios fuente.
  Solo una generación candidata que pase integridad y validación puede cambiar
  `CURRENT`; un rollback debe comprobar los digests del backup.

- Un rollback de versión debe cubrir restauración válida y rechazo fail-closed
  ante digest ausente o divergente; si falla la validación, `CURRENT` no cambia.

- La documentación de instalación debe reflejar el layout generado, el
  `RUNPATH`, el runtime Node requerido y la verificación `SHA256SUMS`; no
  presentar un bundle como autocontenido si faltan dependencias del sistema.
- La release publicada lleva el visor. `ladygraph ui` se anuncia en la ayuda de
  toda build, así que un binario publicado que responda «this build carries no
  web bundle» ofrece un comando que nadie puede ejecutar; los assets web son
  `2.3 MB` de un bundle de `90 MB`. El workflow de release construye sin
  `--mcp-only` y verifica las dos mitades: que `web/index.html` está en el
  payload y que la ayuda del binario no marca `ui` como no disponible -- un
  bundle con assets enlazado sin el tag `webassets` serviría la página de
  «bundle no disponible» en todas las rutas.
- `--mcp-only` sigue existiendo para quien quiera un bundle sin visor.
  `scripts/install.sh` no inicializa la configuración ni indexa repositorios.
- Las releases publicadas usan tags `vX.Y.Z`; `scripts/install.sh` detecta la
  plataforma, descarga la última release publicada para ella, verifica el
  checksum externo e interno, y `ladygraph update` solo sustituye el bundle
  después de validar manifest, versión y checksums. El `SHA256SUMS` de la
  release lista todos los archivos publicados, así que se verifica la línea
  del propio archivo, no el fichero entero.

- Un build de distribución limpio debe ser reproducible entre checkouts del
  mismo commit, toolchain y plataforma; compara el payload completo y no solo
  `manifest.json`.
- Los corpus sintéticos de aceptación de gran escala se generan en una ruta
  privada y nunca sustituyen ni modifican repositorios indexados. Para
  LadybugDB, la reproducibilidad debe distinguir entre hechos lógicos
  (conteos, schema e integridad) y bytes físicos del archivo nativo.
- Una auditoría de exactitud debe separar `false exact edges` de aristas
  colgantes: compara fixtures con ground truth para las primeras y ejecuta las
  invariantes canónicas de extremos, evidencia y procedencia para las segundas.

- Un informe `ACCEPT_LADYGRAPH_WITH_LIMITS` debe enumerar plataforma,
  toolchains, corpus, transporte, garantías, métricas y riesgos residuales;
  no puede convertir una limitación conocida en un PASS implícito.

## Plataformas

- Los objetivos de distribución son `linux/amd64` y `darwin/arm64`, y sólo
  esos. En macOS se publica únicamente Apple Silicon; `darwin/amd64` está
  fuera de alcance por decisión, no por coste, y el instalador lo dice al
  rechazarlo. La nomenclatura es `ladygraph-<os>-<arch>` para el directorio,
  la raíz del tar y el archivo publicado. Un bundle se construye siempre en un
  host de su propia plataforma: cgo enlaza la biblioteca nativa y no hay
  cross-compilation.
- `scripts/build-bundle.sh` es el único generador de bundles; los objetivos
  `make build-linux-amd64` y `make build-darwin-arm64` delegan en él. El
  manifest, `ladygraph version --json` y `ladygraph update` validan contra la
  plataforma en ejecución, nunca contra literales.
- Los scripts eligen la herramienta de digest por disponibilidad -`sha256sum`,
  si no `shasum -a 256`- y fallan cerrado sin ninguna. `--no-overwrite-dir` no
  existe en el `tar` de macOS.
- Una ruta que el motor TypeScript resuelve llega con su grafía canónica, en
  minúsculas cuando el filesystem pliega mayúsculas. Se corrige en la frontera
  con `enginePath` antes de indexarla, compararla o emitirla; nunca con
  `realpath`, que resolvería los enlaces de `node_modules` y cambiaría los
  hechos.
- El ejecutable del bundle declara exactamente un `RUNPATH`, el relativo. El
  que añade el binding hacia su directorio de módulo se retira después de
  enlazar y el build falla si sobrevive alguno más.
- Los artefactos macOS no se notarizan y el proyecto no usa un Developer ID.
  El binario lleva firma ad-hoc, que es lo que exige Apple Silicon para
  ejecutar, y el script la rehace después de editar sus load commands.
  Gatekeeper sólo bloquea un archivo con `com.apple.quarantine`, que no
  escriben `curl` ni `tar`.
- La ayuda no es un error: `--help`, `-h` y `help` escriben en `stdout` y
  terminan con código `0`; una invocación sin comando o con uno desconocido
  escribe una línea en `stderr` y apunta a la ayuda, nunca vuelca la superficie
  entera. Un comando puntual informa en texto plano cuando `stderr` es una
  terminal y como registro JSON cuando no lo es; `serve` y `ui` registran
  siempre en JSON, porque un cliente lee su `stderr`.
- El código específico de plataforma vive en archivos con build tag, no en
  ramas `runtime.GOOS` dentro de la lógica. Un fallback `!linux` que devuelve
  error o cero es una limitación declarada, nunca un silencio.
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
- `internal/procstat` es el único lugar que lee estadísticas de proceso del
  sistema operativo; `0` significa desconocido.

## Integraciones de clientes

- `ladygraph mcp install` y `ladygraph skill install` detectan raíces locales
  conocidas cuando falta `--target`, muestran una selección interactiva de uno o
  varios clientes y conservan `--target` para automatización; nunca inicializan
  Ladygraph ni indexan repositorios.
- La selección interactiva usa Bubble Tea y Lip Gloss: `↑`/`↓` o `j`/`k`
  mueven, `space` alterna, `a`/`n` seleccionan todos/ninguno, `Enter`
  confirma y `q`/`Esc` cancela; respeta `NO_COLOR` y no emite ANSI al
  redirigir la salida.
- Los adaptadores externos validan JSON/TOML antes de modificarlo, rechazan
  destinos symlink, escriben atómicamente con `0600` y exigen `--force` para
  sustituir o retirar una entrada incompatible. Las entradas idempotentes no
  reescriben el archivo.
- La skill canónica vive en
  `internal/integrations/assets/ladygraph/SKILL.md`; los bundles deben
  incluirla bajo `skills/ladygraph/SKILL.md` y en `SHA256SUMS`.

## Verificación obligatoria

Antes de cerrar una tarea, ejecutar según el alcance. El procedimiento -qué
cubre cada suite, qué se salta, cómo acotar un fallo y cómo hacer el smoke test
del binario- está en la skill `.claude/skills/running-tests/SKILL.md`.

```bash
gofmt -l <archivos-go-modificados>
go vet ./...
go test ./...
make build
```

Si el cambio afecta LadybugDB nativo:

```bash
make test-ladybug
```

`make test-ladybug` es el único modo soportado de ejecutar ese tag: exporta las
variables `CGO_*` que apuntan a la biblioteca fijada. `go test -tags ladybug`
por su cuenta no enlaza. `PKGS` acota la pasada a un paquete.

Si afecta el camino Rust, los tests que ejecutan el analizador se saltan
cuando `rust-analyzer` no está instalado, así que la verificación exige
tenerlo:

```bash
rustup component add rust-analyzer
go test ./internal/rustloader/... ./internal/indexer/ -run Rust
```

Estar en el `PATH` no es estar instalado: rustup deja un proxy llamado
`rust-analyzer` para cada toolchain, exista o no el componente, y ese proxy
falla con `Unknown binary 'rust-analyzer' in official toolchain`. El guardia
de los tests (`testsupport.RequireRustAnalyzer`) ejecuta `--version` y se
salta la prueba si no responde; un guardia que sólo busque el nombre da por
instalado el analizador en cualquier máquina con rustup y convierte un
`SKIP` en un `FAIL`. CI instala el componente: saltarse la suite entera
escondería el camino Rust en vez de verificarlo.

Si afecta `ts-worker/`:

```bash
cd ts-worker
pnpm check
pnpm build
```

Si afecta `site/`:

```bash
make site-check
make site-build
```

Para cambios de instalación local, ejecutar el flujo con un `HOME` temporal y
sin modificar repositorios indexados:

```bash
ladygraph init
ladygraph doctor
ladygraph index --full
ladygraph serve
```

`ladygraph serve` debe cargar el `HotSnapshot` publicado antes de abrir el
transporte MCP; sin una generación publicada debe fallar cada consulta que
requiera snapshot de forma explícita.

Los tests nuevos deben defender contratos observables y fallar ante una
regresión plausible. Para cambios de almacenamiento o resolución, incluir
pruebas negativas, invariantes y comparación contra una reconstrucción limpia
cuando sea aplicable.

## Documentación, ADRs y benchmarks

- La documentación técnica vive en `docs/`.
- Cambios de arquitectura, protocolo MCP, framing, schema persistente,
  compatibilidad o migración requieren un ADR.
- Los benchmarks viven en `benchmarks/<nombre>/`, con `results.json` y
  `report.md`. Deben conservar comando, commit, entorno, dataset, semilla,
  métricas y limitaciones.
- La documentación describe el comportamiento observado, no promesas futuras.
- La integración OpenTelemetry de métricas es opcional; los exporters y
  collectors permanecen desactivados por defecto y el proveedor configurado
  pertenece al llamador.
- Los benchmarks de observabilidad deben separar la ruta local, el proveedor
  `noop` y cualquier proveedor SDK configurado explícitamente; no se deben
  presentar como un único coste de producción.
- Los comandos, códigos, campos JSON y gates se escriben entre backticks.
- Los bundles se generan con `make build-linux-amd64` y
  `make build-darwin-arm64`; el directorio `dist/` es generado y no se usa como
  entrada indexada ni de benchmark.
- `ladygraph version --json` debe conservar salida JSON exclusiva en `stdout`;
  el bundle obtiene provenance del `manifest.json` y valida el digest de
  `grammars/manifest.json`; los valores no observables se representan como
  `null`.
- `SHA256SUMS` lista hashes SHA-256 de `manifest.json` y del payload en orden
  lexicográfico; se verifica con `sha256sum -c` o `shasum -a 256 -c` y no se
  incluye a sí mismo.

## Entrega

- Revisar el diff completo y `git diff --check`.
- Confirmar que no quedan imports, rutas, nombres de paquete o comandos
  antiguos del proyecto.
- Confirmar que tests, documentación y consumidores fueron migrados o que la
  excepción está documentada.
- Entregar con estado Git limpio y evidencia concreta de los comandos
  ejecutados.
- Editar siempre `AGENTS.md`; `CLAUDE.md` es un symlink deliberado hacia este
  archivo.
