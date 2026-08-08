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

## TypeScript

- El worker usa TypeScript estricto y módulos ESM.
- Los límites de proceso, protocolo y adaptadores tienen tipos explícitos; `any`
  requiere una justificación local.
- `stdout` contiene únicamente framing/protocolo. Los logs van a `stderr`.
- Todo recurso persistente se cierra al cancelar o terminar el proceso.
- Las promesas rechazadas se clasifican en el límite adecuado; no se ocultan
  con aserciones.
- No editar `ts-worker/dist` manualmente: regenerarlo con `pnpm build`.
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
- `ladygraph ui` es opt-in, sirve solo el `HotSnapshot` publicado por HTTP
  read-only y mantiene `127.0.0.1:7777` como bind por defecto; un bind no
  loopback debe emitir una advertencia de seguridad.
- `ladygraph serve` permanece STDIO y no abre HTTP; `webapi.Run` es dueño del
  listener y ejecuta un cierre graceful acotado al cancelar el contexto.
- `internal/webassets` sirve solo la copia generada de `web/dist` cuando la
  distribución se construye con el tag `webassets`; los binarios sin tag
  devuelven un fallback visible `503` en vez de servir archivos no declarados.
- El visor consulta `navigator.gpu` antes de montar WebGPU; sin un adaptador
  utilizable muestra el motivo y conserva WebGL. La ruta WebGPU usa materiales
  nodo y nunca se declara activa por una mera propiedad de configuración.

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

## Verificación obligatoria

Antes de cerrar una tarea, ejecutar según el alcance:

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

Si afecta `ts-worker/`:

```bash
cd ts-worker
pnpm check
pnpm build
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
- El bundle Linux amd64 se genera con `make build-linux-amd64`; el directorio
  `dist/` es generado y no se usa como entrada indexada ni de benchmark.
- `ladygraph version --json` debe conservar salida JSON exclusiva en `stdout`;
  el bundle obtiene provenance del `manifest.json` y valida el digest de
  `grammars/manifest.json`; los valores no observables se representan como
  `null`.
- `SHA256SUMS` lista hashes SHA-256 de `manifest.json` y del payload en orden
  lexicográfico; se verifica con `sha256sum -c` y no se incluye a sí mismo.

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
