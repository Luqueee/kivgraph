# Instrucciones de la superficie MCP (`internal/mcp/`)

Estas reglas se suman a las de `AGENTS.md` en la raíz del repositorio, que se
leen siempre. Una instrucción de este archivo puede añadir restricciones; nunca
puede relajar un contrato de integridad, compatibilidad o verificación
declarado en la raíz.

`internal/AGENTS.md` cubre el motor que responde estas tools.

## Forma de una respuesta

- Una respuesta viaja por **un solo canal**. Ninguna tool publica `outputSchema`,
  porque el SDK entonces marshala el resultado tipado a `structuredContent` y
  repite el mismo JSON en el bloque de texto: la respuesta se paga dos veces.
  `get_source` va más allá y contesta en prosa: 302 tokens de fuente son 374
  dentro de una cadena JSON y 430 como fila, que es lo que cuesta la lectura de
  rango del anfitrión, así que servir código por el envoltorio no compra nada.
- La única tool que publica `outputSchema` es `index_project`, y es una
  excepción declarada, no un descuido: su respuesta es un informe de forma fija
  cuyo peso son los nombres de campo -- `1.180` bytes en cada canal-- y se emite
  una vez por reconstrucción, no una por pregunta. La regla se midió sobre una
  página de cincuenta filas de `find_references`, donde el duplicado eran
  `24.066` bytes en una pasada. Pagarla dos veces aquí compra un cliente que lee
  los contadores sin parsearlos. `index_project` no pasa por `addQueryTool`, así
  que ni el `panic` ni el test de la superficie servida la alcanzan; quien lo
  fija es `TestIndexProjectIsTheOnlyToolWithAnOutputSchema`, y lo que ese test
  vigila es la segunda tool que lo adquiera.

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
- La única excepción se pide a mano y no la pide un cliente:
  `ServerOptions{ExposeUnavailableTools: true}` -- que es lo que enciende
  `kivgraph serve --introspection`-- lista el catálogo entero sin generación,
  para el inspector o el registro que sólo puede puntuar lo que devuelve
  `tools/list`. Cambia qué se lista y nada más: no crea índice, no fabrica un
  grafo, las tools de consulta que dependen del grafo siguen contestando
  `INDEX_NOT_READY` y `graph_status` informa de un estado de grafo vacío;
  `index_project` sigue tras su puerta de consentimiento y el handshake sigue
  llevando las instrucciones de reparación, porque la disponibilidad -- y no la
  opción-- es lo que decide qué se le dice al cliente. `ServerOptions{}` es el
  comportamiento de siempre, así que el valor cero no puede convertirse en la
  excepción por descuido; lo fija `TestColdStartStillPublishesOnlyTheRepair`.
- Las registraciones de las tools de consulta viven en `registerQueryTools` y
  sólo ahí. Un catálogo que se puntúa desde fuera y otro que se sirve serían dos
  listas que derivan, y lo que se puntuaría no sería lo que se sirve; lo fija
  `TestIntrospectionServesTheSameToolDefinitions`, que compara descripción y
  esquema tool a tool entre las dos formas.
- Lo que un anfitrión mantiene residente es el nombre de la tool y su
  descripción, no su esquema: Oh My Pi lee el esquema bajo demanda y Claude Code
  lo difiere. Ahí vive el enrutado -contra qué alternativa nativa compite cada
  tool y **dónde pierde**- y por eso ninguna descripción ni `instructions` puede
  llevar un número derivado del grafo: reescribiría el prompt de sistema del
  cliente en cada reindexado. Los datos volátiles se piden con `graph_status`.
- Las dos mitades de una definición se pagan en sitios distintos y por eso se
  escriben con presupuestos distintos. La descripción es lo residente y compite
  contra el techo de `MaximumResidentSurfaceBytes`: ahí sólo cabe el enrutado.
  El esquema no lo mantiene nadie en memoria, así que **todo argumento publicado
  lleva su descripción** -- lo fija
  `TestEveryPublishedArgumentDescribesItself`, que también entra en los objetos
  de un array-- y ahí es donde se explica qué distingue `repo` de `repository`,
  cuáles acotan lo alcanzable y cuáles sólo filtran la página. Una descripción
  que ya no cabe residente no se compensa subiendo el techo: se mueve al
  argumento al que pertenecía. Así se recuperó el presupuesto de
  `find_references`, cuya frase sobre el nombre ambiguo vive hoy en `name`.
- Toda tool de consulta anota `readOnlyClosedWorld()`, nunca un
  `ToolAnnotations` a mano. `OpenWorldHint` vale `true` cuando falta, así que
  omitirlo declara lo contrario de lo que este servidor hace: una respuesta sale
  de la generación publicada y de nada más.
- Una respuesta declara lo que su recuento significa sólo cuando la cifra
  engaña. Cero filas se lee como «no existe» salvo que algo diga que es una
  ausencia comprobada; una página truncada no dice si contiene lo que importaba.
  Con filas y sin truncar, `guidance` calla: quince tokens de consejo en cada
  llamada es cómo un ahorro se convierte en un coste.

## Qué puede afirmar una tool

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
- Los cuatro contadores de `coverage` son **disjuntos y sobre la propia
  consulta**. Un fallo que acota la respuesta sin ser evidencia sobre ella -un
  ámbito ilegible del repositorio- no entra en ninguno: se informa en
  `completeness.invisible_scopes` y en `more_invisible_scopes`, que existen
  para eso. Cambiar lo que un contador cuenta es un cambio de esquema aunque
  el campo no cambie de nombre ni de tipo, y el compilador no lo ve: el helper
  del veredicto nació sumando los ámbitos a `unresolved_related` y se propagó
  a cinco tools, hasta que `find_symbol` informó de `29` registros
  relacionados para un nombre que nadie referencia. Todas las puertas seguían
  en verde porque ningún test fijaba el contador sobre un fallo que **sólo**
  fuese de ámbito; el que lo fija ahora está en
  `TestCompletenessSeparatesAFailedReferenceFromAnUnreadableScope`. Ver ADR
  0063.
- Un diagnóstico del cargador que no tumba la pasada se imprime, no sólo se
  cuenta; un repositorio TypeScript que no declara ningún paquete se nombra.
  Un contador sin detalle y una entrada de registro que no aporta nada son
  dos formas de callar.
- `serve` puede leer los ficheros de los repositorios registrados **sólo para
  entregar bytes**. Falla cerrado en lo que afirma y degrada declarando en lo que
  entrega: si el fichero ya no cuadra con el `ContentDigest` de la generación, el
  fichero es la autoridad, la declaración se reancla por nombre y el
  desplazamiento se declara; si no queda o queda dos veces, esa fila no da bytes
  y las demás sí. Reanclar no crea ninguna arista. Ver ADR 0040.
- El proveedor derivado se retira por defecto de las cuatro tools servidas que
  pueden devolver una de sus filas -`find_symbol`, `find_references`,
  `trace_dependencies` y `get_blast_radius`-, y lo anulan `include_derived` o
  nombrarlo en `repo`. Retirar una fila es una decisión de página y nunca una afirmación
  sobre lo observado: la arista sigue publicada con su confianza exacta.
  `graph_status` lo desglosa en `derived`, sus no resueltos incluidos -una arista
  pertenece al repositorio de su símbolo origen, que es el lado que observó- y
  `list_repositories` marca la fila. Sin ese desglose, un repositorio de diez
  símbolos responde `24.704`.
- Una respuesta entrante de `find_references` sobre un método incluye las
  referencias a los métodos de interfaz que implementa, **sólo donde es la única
  implementación**: ahí una llamada por la interfaz no puede alcanzar otra cosa.
  Con dos no se puentea -sería cambiar una ausencia falsa por una presencia
  falsa- y `get_blast_radius` sigue cruzando `IMPLEMENTS` en los dos sentidos.
  Cada fila puenteada lleva `via` y la página declara `dispatch_through`, así que
  ninguna se lee como llamada directa. Añadir una fila es una afirmación, no una
  decisión de página: la correspondencia método a método la emite el cargador con
  `types.LookupFieldOrMethod`, nunca la consulta por nombre. Rust todavía no la
  emite. Ver ADR 0054.
- Las aristas de reenvío -`EXPORTS` y `REEXPORTS`- se retiran por defecto de
  `find_references`, y lo anula `edge_kinds`, con `["*"]` para desactivar el
  filtro entero. Es la misma decisión de página que la anterior: la arista sigue
  publicada con su confianza exacta, `get_blast_radius` no la retira -un
  renombrado sí rompe el barrel- y `trace_dependencies` tampoco, porque recorre
  hacia fuera y truncaría un alcance real. No se pierde ningún consumidor: el
  checker resuelve un import a través de cuantos barrels haya, así que cada uno
  trae su propia `IMPORTS_SYMBOL`. Se declara en `edge_kinds_default_excluded`
  y el `total` cuenta lo que la respuesta tiene. Ver ADR 0053.

## `index_project`

- Una pasada de indexación nunca ocurre en el proceso que responde consultas.
  `Service.IndexProjects` y `Service.Reindex` ejecutan `index --full --json`
  como proceso hijo -este mismo ejecutable, con `--config`, `--repositories` y
  `--resolver-version`- y el pico muere con el hijo. Una pasada sostiene el
  universo de tipos de cada módulo Go, cada worker TypeScript y cada índice SCIP
  a la vez, y el heap de Go conserva la arena: un servidor que indexó en su
  propio proceso se quedaba en 1,68 GB de RSS mientras viviera, con el heap vivo
  en 173 MB. El padre conserva el registro y su rollback; el hijo lee el registro
  del disco y publica la generación; el padre construye el `HotSnapshot`, que no
  se puede delegar porque sirve desde su propio heap. Ver ADR 0042.
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
- `start_index_project` es la ruta portable para clientes con un timeout fijo:
  devuelve un `operation_id` inmediatamente y `get_index_status` lo consulta
  hasta `completed` o `failed`. El daemon comparte el estado entre sus sesiones;
  un `serve` por stdio lo conserva durante la vida de ese proceso. El estado no
  sobrevive un reinicio y conserva como máximo 32 operaciones terminadas. Ver
  ADR 0114.
- `index_project` acepta un lote (`projects`) y reconstruye **una sola vez**.
  Un rebuild resuelve las aristas cross-repository sobre el conjunto completo
  de hechos, así que cuesta el corpus entero se añada lo que se añada: llamar
  una vez por proyecto paga ese coste una vez por proyecto y tira todos los
  grafos menos el último. La forma de un solo proyecto se conserva; mezclar
  ambas en una petición se rechaza.

## La skill publicada

- La skill sólo puede nombrar tools que `internal/mcp/server.go` registra. Es lo
  que un agente lee antes de decidir, así que una tool que el servidor no
  publica no es una imprecisión de documentación: enruta la pregunta a una
  llamada que falla. Enviaba a `get_unresolved_references`, que no está
  registrada, y no mencionaba `get_source` ni `get_file_outline`, que sí.

## Coste en tokens

- El coste en tokens de la superficie se mide, no se opina:
  `benchmarks/mcp-token-cost` compara cada pregunta contra la vía nativa del
  anfitrión con sus salidas **capturadas literalmente**, publica el factor de
  responder y el de la sesión completa -uno solo engaña- y declara el gate desde
  su digest.

## Verificación

```bash
go test ./internal/mcp/...
```

Un cambio de nombre, descripción o esquema de una tool es un cambio de
superficie: exige actualizar
`internal/integrations/assets/kivgraph/SKILL.md`, la referencia de tools de
`landing/` y la tabla de enrutado de la raíz.
