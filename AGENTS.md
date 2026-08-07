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

## LadybugDB y snapshots

- El código que usa la biblioteca nativa se compila con el tag `ladybug`.
- La pareja de versiones de LadybugDB y del binding Go debe provenir de la
  fijación versionada y verificarse mediante `scripts/fetch-ladybug.sh`.
- Validar el grafo canónico antes de publicarlo. Una generación inválida nunca
  puede convertirse en `CURRENT`.
- No leer ni servir consultas MCP directamente desde LadybugDB cuando el
  contrato exige el HotSnapshot publicado.
- No mezclar el esquema experimental `001-synthetic` con el canónico `002`.

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
