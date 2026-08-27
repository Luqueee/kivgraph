# Instrucciones de desarrollo de Kivgraph

Estas reglas aplican a todo el repositorio. Una instrucción más cercana a un
archivo puede añadir restricciones, pero no puede relajar los contratos de
integridad, compatibilidad o verificación descritos aquí.

## Mapa de instrucciones

Este archivo se lee siempre. Los demás cubren un directorio y se leen al
trabajar en él; ninguno repite lo que ya está aquí ni lo contradice.

|directorio|archivo|qué cubre|
|---|---|---|
|`internal/`|`internal/AGENTS.md`|carga de Go, la pasada, caché de hechos, grafo canónico, generaciones, configuración, procesos|
|`internal/mcp/`|`internal/mcp/AGENTS.md`|superficie de tools, `index_project`, la skill, coste en tokens|
|`internal/rustloader/`|`internal/rustloader/AGENTS.md`|`rust-analyzer scip`, identidad SCIP, sysroot, descubrimiento Cargo|
|`cmd/kivgraph/`|`cmd/kivgraph/AGENTS.md`|ayuda, registro, `index --full --json`, `clean`, `stop`, `ui`|
|`ts-worker/`|`ts-worker/AGENTS.md`|worker TypeScript e identidad cross-repository|
|`web/`|`web/AGENTS.md`|el visor: layout, dibujo, coste por fotograma|
|`landing/`|`landing/AGENTS.md`|landing y documentación de usuario: capas, paleta, SEO, iconos|
|`benchmarks/`|`benchmarks/AGENTS.md`|informes, corpus y auditorías|

Un invariante que se infringe desde otro directorio vive aquí, no en el archivo
del directorio que nombra: que `landing/` no entre en ningún bundle se rompe
editando `scripts/build-bundle.sh`.

## Identidad del proyecto

- Proyecto: `Kivgraph`.
- Módulo Go: `github.com/Luqueee/kivgraph`.
- Ejecutable principal: `cmd/kivgraph`.
- Worker TypeScript: `ts-worker/`, paquete privado `@kivgraph/ts-worker`.
- LadybugDB es el almacenamiento canónico; el HotSnapshot es una proyección
  derivada y no una fuente alternativa de hechos.
- Los identificadores históricos `LUQUE-####` del backlog no se renombran.

## Qué pregunta contesta cada tool de Kivgraph

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
| no sé cómo se llama, qué archivos abro | `find_by_intent`, con `keywords` |
| qué hay declarado en este paquete | `get_file_outline` |
| dame el código de estos símbolos -- hasta `20` por llamada | `get_source` |
| ¿está el grafo al día? | `graph_status` |

Las aristas las resuelven `go/types`, el checker de TypeScript y
`rust-analyzer`, no la coincidencia de nombres: una lista de referencias vacía
significa que **nadie lo llama**, no que no se encontró. `grep` no puede decir
eso, y tampoco distingue dos métodos homónimos.

Toda fila trae repositorio, ruta, nombre cualificado y rango de líneas, y toda
tool acepta esa tripleta en vez de una clave estable: la llamada siguiente se
construye con la respuesta que ya se tiene.

Una pregunta de referencias no necesita resolver el símbolo antes: `name` a
secas basta, y cuando varias declaraciones comparten el nombre la respuesta se
niega a elegir y **nombra los candidatos** con esa misma tripleta, así que
acotar es copiar uno. Sobre `kena` la negativa cuesta `129` tokens donde el
`find_symbol` previo costaba `750`; medido en
`benchmarks/graft-comparison/report.md`.

Y se pide a la granularidad que se pregunta: `view: "files"` responde qué
archivos sin la línea de cada referencia. Las cuatro preguntas de referencias de
`kena` cuestan `2.480` tokens con línea y `912` sin ella, con la misma precisión
y la misma exhaustividad -- y una página en vez de dos donde 66 referencias caben
en 9 archivos. Quien necesite la línea pide las filas compactas, que son el
valor por defecto.

**Dónde pierde, y conviene no gastar la llamada:** un nombre raro en un solo
repositorio pequeño lo resuelve `grep` más barato -una llamada, sin esquema,
sin resolver un símbolo primero-, y el índice de un fichero pequeño cuesta más
que leerlo. Gana en nombres comunes, en impacto transitivo, en consumidores de
otro repositorio y en demostrar una ausencia.

Medido con `benchmarks/mcp-token-cost` después del ADR 0046, sobre las seis
preguntas de referencias de este mismo repositorio (generación `000001`,
commit `f8a952d6`): responder cuesta entre `3,29x` (`MergeAll`, nombre raro) y
`11,95x` (`NewServer`, nombre común) menos que `grep` más la lectura; la sesión
completa -incluidos los cuerpos que el agente abre después, que pagan igual en
los dos lados- entre `1,26x` y `8,05x`, con un suelo de `2,41x` fijado por el
coste de esos cuerpos y no por el de la respuesta.

El caso genuinamente trivial **ya es una fila medida**, y confirma la
desventaja: `benchmarks/graph-tools-comparison/trivial.md`, sobre
`newGMCClient` -- dos apariciones en todo `kena`, una declaración y una llamada,
sin homónimo en cinco lenguajes. Los seis brazos aciertan y `grep` es el más
barato de todos: `65` tokens contra nuestros `123`, o sea que costamos `1,9x`.
Los otros cuatro grafos también pierden contra esa línea de `grep`, y la razón
es estructural: el sobre de una respuesta -- `snapshot_id`, `coverage`, `total`,
la fila con repositorio y rango-- cuesta más que dos líneas de texto cuando la
respuesta **son** dos líneas de texto.

Sobre las `29` preguntas de los seis conjuntos, `grep` sale más barato en cinco
y la mediana queda en `5,95x` a nuestro favor. Tres de esas cinco son preguntas
de ausencia, y ahí el coste y la evidencia no dicen lo mismo: un `grep` sin
resultados es barato y no es una prueba, porque no distingue «nadie lo llama» de
«los llamantes lo escriben de otra forma». Medido en el mismo corpus por `X1`,
donde un consumidor real nunca deletrea el símbolo -- un reexport con `*` que lo
cruza de repositorio-- y ninguna búsqueda de texto lo alcanza.

## La puerta delante de `grep`

`kivgraph hook install` registra un gancho que se ejecuta antes de cada tool del
agente y niega la búsqueda cuando el grafo la contesta mejor. Lo alojan
`claude-code`, `claude-desktop`, `codex` y `opencode`. Claude Desktop empaqueta
un Claude Code y lo lanza sin darle configuración propia, así que lee
`~/.claude/settings.json`: comparte fichero con `claude-code` e instalar uno
deja el otro `managed`. Oh My Pi es el único que no puede alojarlo.

Se cierra por **un solo hecho**: el nombre lo declaran dos cosas o más, así que
una búsqueda de texto no puede separar lo que encuentra. Un nombre sin homónimo
se deja pasar por muchos sitios que lo usen, porque ahí `grep` es más barato y
está medido -- ver ADR 0077 para la tabla y para lo que haría falta medir para
cerrarla también sobre recuentos altos.

Todo fallo de la puerta es un permiso, y un permiso no escribe nada: en ese
contrato un `allow` explícito se salta la petición de permiso del agente.

Para saltársela una vez: `KIVGRAPH_DISABLE_HOOK=1` delante del comando.

## Herramientas MCP en Oh My Pi

- Las rutas `xd://` se descubren consultando `xd://`; nunca se construyen
  concatenando prefijos a partir del nombre visible de una herramienta.
- Las herramientas directas de Kivgraph usan
  `xd://mcp__kivgraph_<operación>`; a través de 1MCP el nombre agregado es
  `kivgraph_1mcp_<operación>`.
- No se debe inventar una forma `xd://mcp__mcp_kivgraph_<operación>`.
- Una respuesta MCP `tools/list` puede estar paginada: seguir `nextCursor`
  hasta `null` antes de concluir que una herramienta no está montada.

- `kivgraph_1mcp_index_project` es la única herramienta MCP mutante de
  Kivgraph; solo se registra en la ruta `serve` configurada y exige
  consentimiento explícito del cliente antes de cambiar el registro de
  repositorios o publicar una generación.

## La skill se edita en un solo sitio

`kivgraph skill install` deja, en alcance de usuario, un fichero canónico en
`~/.config/kivgraph/skills/kivgraph/SKILL.md` y enlaza ahí la ruta de cada
cliente. Editarlo una vez alcanza a todos, y un upgrade **no** se lleva el
cambio: `install` sólo escribe el canónico si falta o si lo que hay es la skill
que el build trae, y `--force` recupera esa. El alcance de proyecto copia, porque
un enlace absoluto se commitearía roto. Ver ADR 0078.

## Puesta en marcha

- El toolchain Go es el del `go.mod`. `go/types` viaja enlazado en el binario,
  así que el techo de versión del lenguaje es el del toolchain que lo compiló;
  `kivgraph doctor` informa de ese número y no del `go` del `PATH`.
- La biblioteca nativa fijada se descarga y se verifica con `make ladybug-lib`.
  `make test-ladybug` la resuelve por su cuenta y exporta las variables `CGO_*`.
- **`make build` no produce un binario capaz de tocar el grafo canónico.** Un
  `go build` sin el tag `ladybug` deja dentro el fallback que declara
  `LadybugDB native support is unavailable`, así que ese binario **sirve** un
  snapshot ya publicado -- que se mapea y no necesita cgo-- y en cambio falla
  todo lo que cruza LadybugDB: `index --full` muere en `stage.staging`, y su
  propio `doctor` dice `graph.store: FAIL`. Es una limitación declarada y no un
  silencio, pero conviene saber cuál de los dos binarios se tiene en la mano.
  El que sí lo puede hacer se compila como lo hace la suite nativa:

  ```bash
  LIB="$(scripts/fetch-ladybug.sh)"
  CGO_ENABLED=1 CGO_CFLAGS="-I$LIB" CGO_LDFLAGS="-L$LIB" \
    go build -tags ladybug -ldflags="-extldflags=-Wl,-rpath,$LIB" ./cmd/kivgraph
  ```

  Con él, `doctor` termina en `PASS`. Un bundle publicado lo lleva ya enlazado,
  así que para reindexar sirve el binario instalado.
- El analizador Rust fijado se descarga con `scripts/fetch-rust-analyzer.sh`. La
  suite de Rust exige además `rustup component add rust-analyzer` y un `cargo`
  en el `PATH`: sin toolchain el analizador no carga el workspace.
- Los tres proyectos pnpm son independientes y tienen su propio lockfile:
  `pnpm --dir ts-worker install --frozen-lockfile`, y lo mismo para `web` y
  `landing`.
- Si un comando que estas instrucciones usan no está instalado, instalarlo antes
  de seguir. No sustituirlo por otro ni saltarse el paso.

## Antes de editar

1. Leer `TASKS.md`, sus dependencias, el gate aplicable y la documentación del
   subsistema afectado.
2. Inspeccionar implementaciones, tests y consumidores existentes; reutilizar
   la convención vigente en vez de crear una segunda.
3. Definir el comportamiento observable y sus casos negativos antes de tocar
   código.
4. No ocultar warnings, errores, referencias no resueltas, limitaciones ni
   resultados `FAIL`.

## Nunca modificar

- Los repositorios indexados y los artefactos de entrada de los benchmarks. Las
  pruebas usan copias o fixtures privados.
- Los directorios generados: `ts-worker/dist`, `web/dist`, `landing/dist`,
  `dist/`, `.tooling/` y `landing/.astro`. Se regeneran con su build.
- Lo que está fijado por digest: `tools/manifest.json`,
  `grammars/manifest.json` y los lockfiles. Se cambian con el script que los
  produce, nunca a mano.
- Los identificadores históricos `LUQUE-####` del backlog.
- Los `CLAUDE.md`: son symlinks deliberados hacia el `AGENTS.md` de su
  directorio.

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
- **El único camino de indexado es la reconstrucción completa.** No existe un
  camino incremental: se retiró en el ADR 0057, medido a `1,67x` de un pase
  completo y sin ningún llamante en producción. `kivgraph index` acepta sólo
  `--full`, y eso es el diseño, no una limitación de la superficie.
- Si alguna vez vuelve un camino incremental, el contrato de retirada del ADR
  0056 es el punto de partida y **no se puede relajar**: todo hecho afirmado por
  un archivo se retira y se vuelve a afirmar junto con ese archivo, y lo que un
  archivo afirma son las aristas que **salen** de sus símbolos -- una arista que
  otro archivo le apunta la afirmó ese otro, y retirar este no la toca. Un
  símbolo que el `Upsert` vuelve a afirmar conserva su nodo por eso mismo; uno
  que no, se va con sus aristas, entrantes incluidas. Las aristas de paquete
  también se retiran por su evidencia aunque sobrevivan sus dos extremos.
  Relajarlo es el defecto `LUQUE-2002`: un fichero editado perdiendo en silencio
  toda arista entrante desde otro fichero.
- Un camino incremental tampoco puede publicar sin verificar. La ruta retirada
  hacía cero llamadas a `integrity` y a `golden probes`, y parte de su ventaja
  medida era esa verificación ausente. Y no produciría un grafo idéntico byte a
  byte a una reconstrucción limpia: una fila que nadie restableció conservaría el
  `source_snapshot` y el `resolver_version` de la generación que la observó. Eso
  es procedencia y ninguna consulta filtra por ella; lo que sí debe coincidir es
  el contenido. Ver ADR 0056 y ADR 0057.
- Cada `UNRESOLVED` conserva motivo, repositorio y lenguaje; cuando existe una
  ocurrencia concreta conserva su archivo, posición y detalle observados.
  Los fallos de módulo a nivel de repositorio pueden no tener archivo y nunca
  se les fabrica evidencia ni una arista `EXACT`.

## Superficies que rompen compatibilidad

Antes de cerrar, comprobar si el cambio toca alguna de estas. Ninguna se cambia
en silencio: cada una exige ADR, migración documentada o full rebuild.

- Las stable keys: algoritmo, identidad canónica y el namespace histórico
  `luque-stable-key`.
- El schema LadybugDB y el layout del store de generaciones, su backup y su
  rollback.
- El payload `LGVB` del visor, su versión y sus códigos de error.
- Los nombres, descripciones y esquemas de las tools MCP, y la skill que los
  nombra.
- Los parámetros y las salidas del CLI, incluidos el protocolo de
  `index --full --json` y `version --json`.
- La configuración: claves, vocabularios aceptados y ubicación por defecto.
- El bundle publicado: los nombres `kivgraph-<os>-<arch>`, `manifest.json`,
  `SHA256SUMS` y el `RUNPATH`.

Y una regla sobre **cómo** se retira cualquiera de ellas: lo que era válido ayer
no puede convertirse hoy en un fallo. Una clave de configuración que se retira se
acepta, se ignora y se **nombra** -- `retiredConfigKeys` y la línea
`config.retired` de `doctor`--, porque el decodificador rechaza claves
desconocidas y borrar el campo sin más convertiría en error de arranque cada
fichero escrito por un `init` anterior. Lo mismo vale para un formato que sube de
versión: un fichero de la versión anterior es una actualización, no un store
dañado, y contarlo como fallo pone el `doctor` en rojo en cada instalación --
que es exactamente cómo un fallo de verdad deja de notarse. Las dos formas ya se
infringieron una vez cada una. Ver ADR 0062 y ADR 0061.

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

## Plataformas y distribución

- Los objetivos de distribución son `linux/amd64` y `darwin/arm64`, y sólo
  esos. En macOS se publica únicamente Apple Silicon; `darwin/amd64` está
  fuera de alcance por decisión, no por coste, y el instalador lo dice al
  rechazarlo. La nomenclatura es `kivgraph-<os>-<arch>` para el directorio,
  la raíz del tar y el archivo publicado. Un bundle se construye siempre en un
  host de su propia plataforma: cgo enlaza la biblioteca nativa y no hay
  cross-compilation.
- `scripts/build-bundle.sh` es el único generador de bundles; los objetivos
  `make build-linux-amd64` y `make build-darwin-arm64` delegan en él. El
  manifest, `kivgraph version --json` y `kivgraph update` validan contra la
  plataforma en ejecución, nunca contra literales.
- Los scripts eligen la herramienta de digest por disponibilidad -`sha256sum`,
  si no `shasum -a 256`- y fallan cerrado sin ninguna. `--no-overwrite-dir` no
  existe en el `tar` de macOS.
- El ejecutable del bundle declara exactamente un `RUNPATH`, el relativo. El
  que añade el binding hacia su directorio de módulo se retira después de
  enlazar y el build falla si sobrevive alguno más.
- Los artefactos macOS no se notarizan y el proyecto no usa un Developer ID.
  El binario lleva firma ad-hoc, que es lo que exige Apple Silicon para
  ejecutar, y el script la rehace después de editar sus load commands.
  Gatekeeper sólo bloquea un archivo con `com.apple.quarantine`, que no
  escriben `curl` ni `tar`.
- El código específico de plataforma vive en archivos con build tag, no en
  ramas `runtime.GOOS` dentro de la lógica. Un fallback `!linux` que devuelve
  error o cero es una limitación declarada, nunca un silencio.
- La documentación de instalación debe reflejar el layout generado, el
  `RUNPATH`, el runtime Node requerido y la verificación `SHA256SUMS`; no
  presentar un bundle como autocontenido si faltan dependencias del sistema.
- La release publicada lleva el visor. `kivgraph ui` se anuncia en la ayuda de
  toda build, así que un binario publicado que responda «this build carries no
  web bundle» ofrece un comando que nadie puede ejecutar; los assets web son
  `2.3 MB` y el bundle publicado de la `v0.5.0` pesa `43 MB` comprimido. El
  workflow de release construye sin
  `--mcp-only` y verifica las dos mitades: que `web/index.html` está en el
  payload y que la ayuda del binario no marca `ui` como no disponible -- un
  bundle con assets enlazado sin el tag `webassets` serviría la página de
  «bundle no disponible» en todas las rutas.
- `--mcp-only` sigue existiendo para quien quiera un bundle sin visor.
  `scripts/install.sh` no inicializa la configuración ni indexa repositorios.
- Las releases publicadas usan tags `vX.Y.Z`; `scripts/install.sh` detecta la
  plataforma, descarga la última release publicada para ella, verifica el
  checksum externo e interno, y `kivgraph update` solo sustituye el bundle
  después de validar manifest, versión y checksums. El `SHA256SUMS` de la
  release lista todos los archivos publicados, así que se verifica la línea
  del propio archivo, no el fichero entero.
- Un build de distribución limpio debe ser reproducible entre checkouts del
  mismo commit, toolchain y plataforma; compara el payload completo y no solo
  `manifest.json`.
- Los bundles se generan con `make build-linux-amd64` y
  `make build-darwin-arm64`; el directorio `dist/` es generado y no se usa como
  entrada indexada ni de benchmark.
- `kivgraph version --json` debe conservar salida JSON exclusiva en `stdout`;
  el bundle obtiene provenance del `manifest.json` y valida el digest de
  `grammars/manifest.json`; los valores no observables se representan como
  `null`.
- `SHA256SUMS` lista hashes SHA-256 de `manifest.json` y del payload en orden
  lexicográfico; se verifica con `sha256sum -c` o `shasum -a 256 -c` y no se
  incluye a sí mismo.

## Documentación y ADRs

- La documentación técnica vive en `docs/`.
- Cambios de arquitectura, protocolo MCP, framing, schema persistente,
  compatibilidad o migración requieren un ADR.
- La documentación describe el comportamiento observado, no promesas futuras.
- La integración OpenTelemetry de métricas es opcional; los exporters y
  collectors permanecen desactivados por defecto y el proveedor configurado
  pertenece al llamador.
- Los comandos, códigos, campos JSON y gates se escriben entre backticks.

## El idioma de lo que se escribe

Todo texto que este repositorio **entrega o publica** se escribe en inglés:

- la documentación de `docs/` y la de `landing/`;
- los `AGENTS.md` y las skills de `.claude/skills/`;
- las notas de una release -- que son el cuerpo de
  `landing/src/content/releases/vX.Y.Z.md`, y ya lo estaban;
- el cuerpo y el título de un pull request;
- el mensaje de un commit, que la sección de entrega ya exigía;
- los comentarios del código, que ya lo están y no cambian.

La razón no es preferencia. El público de este repositorio lee inglés -- el
`README`, la landing, la referencia de tools y las releases ya se publican
así -- y un proyecto que documenta sus contratos en un idioma y su código en
otro obliga a cada lector nuevo a elegir cuál de los dos es el que manda.

**Es un ratchet, no una migración.** La regla vale para lo que se escribe a
partir de ahora; los documentos que la preceden se quedan como están hasta que
alguien los traduzca. Es el mismo trato que la regla de columnas de
`scripts/check-docs.sh`: un gate que nace en rojo es un gate que se aprende a
ignorar.

Y de ahí sale la única parte que no es obvia: **un fichero no se vuelve
bilingüe**. Si un cambio añade una frase a un documento que está en castellano,
esa frase va en castellano. Traducir el fichero es un cambio propio, entero, con
su propio commit -- porque media página en cada idioma se lee peor que cualquiera
de los dos, y porque un diff que traduce y edita a la vez no se puede revisar.

Lo que no se entrega no entra aquí: una conversación, una nota de trabajo o el
texto de una tarea van en el idioma que quiera quien escribe.

## Tamaño y forma de un cambio

- Un cambio que no sea mecánico no debería pasar de 800 líneas, y de 500 si es
  lógica compleja. Si pasa, proponer el troceado en etapas revisables a partir
  del diff real, sus dependencias y sus consumidores, e identificar la primera
  etapa coherente que se puede entregar sola.
- Preferir un paquete nuevo a engordar uno grande. `internal/indexing`,
  `internal/mcp` e `internal/hotsnapshot` son los que atraen cambios ajenos; al
  extraer código de uno, mover con él sus tests y la documentación de sus
  invariantes.
- Los comandos de este repositorio son lentos por construcción: `index --full`
  dura minutos, `make test-ladybug` enlaza cgo y el analizador Rust carga un
  workspace entero en cada invocación. Esperarlos. No matarlos por PID ni darlos
  por colgados.

## Tests

- Los tests nuevos deben defender contratos observables y fallar ante una
  regresión plausible. Para cambios de almacenamiento o resolución, incluir
  pruebas negativas, invariantes y comparación contra una reconstrucción limpia
  cuando sea aplicable.
- No añadir tests de valores definidos estáticamente, ni tests negativos de
  lógica que se acaba de retirar.
- Preferir comparar el objeto completo a comprobar campo por campo.
- El código de producción no lleva funciones que sólo existen para un test; el
  helper vive con el test.
- Un fixture demuestra el caso real o no demuestra nada: comprobar qué forma
  instala de verdad la herramienta que se está imitando.

### Se empieza por los negativos

El orden no es una preferencia de estilo: el camino feliz se escribe solo y se
comprueba mirando la respuesta, mientras que los rechazos, los límites y los
vacíos son los que nadie ejecuta hasta que un usuario los ejecuta. Así que se
escriben **primero**, antes del test que demuestra que la cosa funciona.

Por cada unidad nueva, antes de dar por hecho el caso bueno:

- **La entrada que sobra y la que falta.** El argumento vacío, el que llega dos
  veces, el que se combina con otro que lo contradice. Un rechazo se prueba por
  su mensaje además de por su error: un `INVALID_ARGUMENT` que no dice qué
  narrowing arreglarlo es un fallo de superficie, no una validación.
- **Los tres vacíos, que no significan lo mismo.** No hay nada, no hay nada
  *dentro de estos límites*, y no se pudo mirar. Un test que los confunde
  bendice la peor respuesta que este proyecto puede dar: una ausencia afirmada
  que nadie estableció.
- **El límite por los dos lados.** Cero, uno, exactamente el máximo, el máximo
  más uno. Y la unidad correcta: contar bytes donde el contrato cuenta
  caracteres ya dejó pasar un término de un solo carácter por cada alfabeto no
  ASCII.
- **La forma corrupta que hoy nadie puede construir.** Un guardia que sólo
  protege de lo que el constructor de al lado nunca produce es un guardia sin
  probar, y deja de proteger el día que los datos llegan de un fichero.

### La cobertura se mide y lo que falta se nombra

Se persigue la cobertura más alta que el código admita, y se mide en vez de
estimarse:

```bash
go test ./<paquete>/ -coverprofile=/tmp/cov.out
go tool cover -func=/tmp/cov.out
```

Sobre el código nuevo la referencia es el **100 % de sentencias**, y llegar
suele ser señal de que la unidad tiene el tamaño correcto. Cuando no se llega,
la rama que queda fuera **se nombra y se justifica** en el comentario del test o
en el mensaje del commit. Sólo dos justificaciones valen:

1. Alcanzarla exigiría que el reloj, el sistema de ficheros o el planificador se
   comporten de una forma que el test no puede fijar.
2. Alcanzarla exigiría un parámetro, un reloj inyectado o una función en el
   código de producción que sólo existiría para el test -- que ya está
   prohibido más arriba.

Una rama sin cubrir y sin justificar es trabajo a medias, no una decisión. Y
subir un porcentaje añadiendo tests de valores definidos estáticamente está
prohibido más arriba: la cobertura es la consecuencia de haber probado los
contratos, nunca el objetivo que los sustituye.

### Un índice o una caché se compara contra la fuerza bruta

Toda estructura que sólo existe para contestar más rápido lo que otro camino ya
contesta lleva un test que compara los dos sobre un corpus generado: el índice
devuelve exactamente lo que devuelve el barrido lineal, término a término. Es el
único test capaz de detectar que dejó de decir lo mismo, porque una estructura
derivada que se equivoca no falla -- contesta.

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

Si el cambio toca documentación bajo `docs/` o un `CLAUDE.md`:

```bash
scripts/check-docs.sh
```

Comprueba que todo `CLAUDE.md` sigue siendo un symlink a su `AGENTS.md` y que
ninguna línea **nueva** bajo `docs/` pasa de 84 columnas. Lo segundo es un
ratchet sobre las líneas que cambian, no sobre los ficheros: cuarenta documentos
preceden a la regla y reflowarlos no es el precio de añadir una frase.

Si el cambio afecta LadybugDB nativo:

```bash
make test-ladybug
make lint-ladybug
```

`make test-ladybug` es el único modo soportado de ejecutar ese tag: exporta las
variables `CGO_*` que apuntan a la biblioteca fijada y pasa el rpath por
`-ldflags`. `go test -tags ladybug` por su cuenta no enlaza. `PKGS` acota la
pasada a un paquete.

`make lint-ladybug` contesta la pregunta de código muerto, y sólo se puede
contestar ahí. Bajo la configuración por defecto los ficheros tras el tag no se
analizan, así que todo símbolo cuyo llamante vive en uno de ellos parece sin
referencias: medido en `20` hallazgos, los `20` falsos. Con el tag la respuesta es
cero, y eso es lo que lo convierte en un gate en vez de un deseo.

Lo que esas flags **no** repiten es parte del contrato. `CGO_LDFLAGS` se aplica a
cada paquete cgo y no una vez al enlazar, así que nombrar ahí `-llbug` o el rpath
-- que el binding fijado ya declara -- imprimía `221` avisos `duplicate -rpath` e
`ignoring duplicate libraries` en una pasada completa. Ninguno era un defecto y
entre todos tapaban el único que sí informa. Devolverlos a `CGO_LDFLAGS` los trae
de vuelta; el rpath se declara una vez, donde se enlaza una vez.

El resto de gates vive junto al código que verifica: Rust en
`internal/rustloader/AGENTS.md`, el worker en `ts-worker/AGENTS.md`, el visor en
`web/AGENTS.md` y la landing en `landing/AGENTS.md`.

Y hay cinco que sólo corre CI, porque piden red o varios minutos, pero que se
pueden reproducir a mano cuando uno de ellos falla:

|gate|comando|qué exige|
|---|---|---|
|corrección|`staticcheck -checks='all,-U1000' ./...`|toda clase de `staticcheck`. `U1000` es la única exclusión y no por ruidosa: bajo esta build no se puede contestar, y vive en `make lint-ladybug`|
|vulnerabilidades|`govulncheck ./...`|cero **alcanzables**; una en un módulo que no se llama no falla|
|reproducibilidad|`scripts/check-reproducible-bundle.sh`|dos builds del mismo checkout, payload idéntico|
|cobertura|`make coverage`|la suite Go entera, instrumentada, por encima del suelo de sentencias|
|humo del bundle|`init` · `doctor` · `index --full` · `--smoke`|que el binario publicado indexe y que las doce tools contesten|

`make coverage` **es** el `go test ./...` del job `verify`, no un gate aparte:
mide con `-coverpkg=./...` mientras los tests corren, así que no puede seguir a
una pasada limpia sin ser el mismo trabajo dos veces. Mide cruzando paquetes
porque este repositorio prueba cruzando fronteras a propósito -- `index_project.go`
se ejercita desde `internal/mcp`, no desde `internal/mcp/tools`--, y el número por
paquete que `go test ./...` produce sale casi tres puntos bajo y señala como sin
tests ficheros que sí lo están. `benchmarks/` e `internal/testsupport` quedan
fuera: son arneses sin tests propios y cuestan quince puntos que no dicen nada
del producto.

Se niega a correr si falta `dart`, `pyright-langserver` o `rust-analyzer`, porque
un analizador ausente salta su suite en vez de fallarla: sin Dart el total cae de
`80,0 %` a `77,9 %`, por debajo del suelo, y el fallo se leería como tests
perdidos. `KIVGRAPH_COVERAGE_ALLOW_PARTIAL=1` mide igual y no aplica el suelo,
que es lo que quiere una estación de trabajo sin los cinco instalados.

El suelo es un trinquete: se sube cuando la suite se lo gana, y bajarlo es una
regresión o una decisión que va en el mensaje del commit.

El humo es el único que prueba el producto contra una generación **publicada**, y
por eso existe: los dos defectos que la `v0.8.0` se llevó pasaban todos los tests
con fixtures y sólo fallaban ahí. Corre con un `HOME` temporal, y exporta
`GOMODCACHE` a la caché real -- sin eso el módulo principal no type-checkea y el
grafo sale hueco sin que nada lo diga.

Para cambios de instalación local, ejecutar el flujo con un `HOME` temporal y
sin modificar repositorios indexados:

```bash
kivgraph init
kivgraph doctor
kivgraph index --full
kivgraph serve
```

`kivgraph serve` debe **resolver** la generación publicada antes de abrir el
transporte MCP y decir si la tiene; el grafo lo lee la primera consulta que lo
necesita, no el arranque. Sin una generación publicada debe fallar cada consulta
que requiera snapshot de forma explícita, y una generación que no se pueda mapear
no puede confundirse con la ausencia de generación: `graph_status` la nombra.
Ver ADR 0067, que lo midió -- `48` de `51` servidores reales no reciben ninguna
llamada, y cargar por si acaso les costaba `33` de sus `40 MB`.

Lo que decide la superficie de tools y las instrucciones del handshake es la
**disponibilidad**, nunca el grafo: el demonio construye un servidor MCP por
sesión aceptada, así que preguntar por el snapshot ahí lo mapea una vez por
cliente. Por el mismo motivo, comparar generaciones -- el reconciliador, el log de
arranque, el arm de carrera al publicar-- se hace por identificador.

## Commits y entrega

- Conventional Commits en inglés, en imperativo y en minúscula tras los dos
  puntos: `tipo(scope): asunto`. Es lo que cumple el 95 % del historial; la
  mediana del asunto son 41 caracteres y ninguno pasa de 80.
- Tipos en uso: `feat`, `fix`, `docs`, `chore`, `test`, `perf`, `build`,
  `refactor`, `ci`, más `bench` y `audit` para mediciones y cualificaciones.
- El scope es el subsistema -`indexing`, `mcp`, `rustloader`, `typescript`,
  `cli`, `web`, `landing`, `release`- y se omite cuando el cambio es
  transversal.
- El cuerpo explica el por qué cuando no es obvio; nunca narra el diff.
- El título y el cuerpo de un pull request van en inglés, como el commit. Un PR
  es lo que lee quien revisa y lo que queda en el historial de la rama cuando el
  commit ya no se abre, así que no puede estar en un idioma distinto del commit
  que resume.
- Revisar el diff completo y `git diff --check`.
- Confirmar que no quedan imports, rutas, nombres de paquete o comandos
  antiguos del proyecto.
- Confirmar que tests, documentación y consumidores fueron migrados o que la
  excepción está documentada.
- Entregar con estado Git limpio y evidencia concreta de los comandos
  ejecutados.
- Editar siempre el `AGENTS.md` que corresponda al directorio; los `CLAUDE.md`
  son symlinks deliberados hacia ellos.
