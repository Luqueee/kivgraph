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
coste de esos cuerpos y no por el de la respuesta. El harness no incluye
todavía el caso genuinamente trivial -un nombre raro en una sola línea de un
archivo pequeño-, así que la ventaja de `grep` ahí sigue siendo estructural y
no una fila medida.

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

## Puesta en marcha

- El toolchain Go es el del `go.mod`. `go/types` viaja enlazado en el binario,
  así que el techo de versión del lenguaje es el del toolchain que lo compiló;
  `kivgraph doctor` informa de ese número y no del `go` del `PATH`.
- La biblioteca nativa fijada se descarga y se verifica con `make ladybug-lib`.
  `make test-ladybug` la resuelve por su cuenta y exporta las variables `CGO_*`.
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
- En un delta incremental, todo hecho afirmado por un archivo se retira y se
  vuelve a afirmar junto con ese archivo. Las aristas de paquete también se
  retiran por su evidencia aunque sobrevivan sus dos extremos.
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
  `2.3 MB` de un bundle de `90 MB`. El workflow de release construye sin
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

El resto de gates vive junto al código que verifica: Rust en
`internal/rustloader/AGENTS.md`, el worker en `ts-worker/AGENTS.md`, el visor en
`web/AGENTS.md` y la landing en `landing/AGENTS.md`.

Para cambios de instalación local, ejecutar el flujo con un `HOME` temporal y
sin modificar repositorios indexados:

```bash
kivgraph init
kivgraph doctor
kivgraph index --full
kivgraph serve
```

`kivgraph serve` debe cargar el `HotSnapshot` publicado antes de abrir el
transporte MCP; sin una generación publicada debe fallar cada consulta que
requiera snapshot de forma explícita.

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
- Revisar el diff completo y `git diff --check`.
- Confirmar que no quedan imports, rutas, nombres de paquete o comandos
  antiguos del proyecto.
- Confirmar que tests, documentación y consumidores fueron migrados o que la
  excepción está documentada.
- Entregar con estado Git limpio y evidencia concreta de los comandos
  ejecutados.
- Editar siempre el `AGENTS.md` que corresponda al directorio; los `CLAUDE.md`
  son symlinks deliberados hacia ellos.
