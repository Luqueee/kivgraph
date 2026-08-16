# ADR 0034: Indexación Rust hermética y sin escrituras en el repositorio

- **Estado:** aceptada
- **Fecha:** 2026-08-11
- **Revisa:** ADR 0033

## Contexto

Dos reglas del proyecto chocan con la forma en que `rust-analyzer` carga un
proyecto Cargo:

1. La pasada nunca escribe dentro de un repositorio registrado
   (`internal/indexer/full.go:61-63`).
2. La indexación es hermética por defecto; la red es una salida explícita,
   como `go.allow_network`.

El subcomando `scip` fija en su propio código `load_out_dirs_from_check: true`
y `ProcMacroServerChoice::Sysroot`. Es decir: **ejecuta build scripts**, y para
eso invoca `cargo check`, que escribe en `target/` del workspace y puede tocar
`Cargo.lock`. Ninguna opción del subcomando lo desactiva.

`rust-analyzer.cargo.targetDir` no resuelve el problema: su valor es `true`
—un subdirectorio del `target` existente— o una ruta **relativa al
workspace**. En ambos casos la escritura cae dentro del repositorio indexado.

## Decisión

La hermeticidad se impone desde fuera del analizador, en la invocación, y se
verifica después.

### Configuración generada por invocación

Kivgraph escribe un JSON temporal y lo pasa con `--config-path`:

| Clave | Valor | Por qué |
| --- | --- | --- |
| `cargo.extraEnv.CARGO_TARGET_DIR` | directorio de estado, absoluto y externo | única forma de sacar los artefactos del repositorio |
| `cargo.extraEnv.CARGO_NET_OFFLINE` | `"true"` salvo `rust.allow_network` | hermético por defecto |
| `cargo.extraArgs` | `["--offline", "--locked"]` | `--locked` falla cerrado si habría que reescribir el lockfile |
| `cargo.features`, `cargo.noDefaultFeatures`, `cargo.cfgs` | configuración del usuario | deciden qué código existe |
| `cargo.buildScripts.enable`, `procMacro.enable` | configurables, activos por defecto | su desactivación es una limitación declarada, no un silencio |

La línea de órdenes añade `--exclude-vendored-libraries`: sin ella, el código
vendorizado bajo la raíz del workspace entraría al grafo como archivos del
repositorio indexado.

### Verificación posterior

Tras cada invocación se comprueba que el árbol del repositorio no cambió. Si
cambió, la unidad falla y no publica hechos. Una indexación que modifica lo que
indexa no es una indexación.

### Aislamiento de fallos

`rust-analyzer scip` aborta la invocación completa cuando el workspace no
carga. Eso limita el daño a una unidad: el workspace afectado se declara
`WORKSPACE_NOT_LOADED` con el diagnóstico observado y el resto de repositorios
se publica, exactamente como un módulo Go que no carga.

## Alternativas descartadas

- **`rust-analyzer.cargo.targetDir`.** Escribe dentro del workspace.
- **Copiar el repositorio a un directorio temporal antes de indexar.** Duplica
  el árbol y las rutas dejan de ser las del repositorio registrado; además
  invalidaría las huellas de la caché de hechos.
- **`cargo.noDeps: true` como modo único.** Es genuinamente offline, pero deja
  sin metadatos a las dependencias y con ello sin destino a buena parte de las
  referencias. Queda como opción del usuario, no como norma.
- **Desactivar build scripts y proc-macros por defecto.** Se pierde el código
  generado y los `derive`, que es donde vive una parte considerable de la API
  real de un crate. Con el target dir redirigido y `--offline --locked` no hace
  falta pagar ese precio.

## Consecuencias

- Indexar Rust exige un caché de cargo caliente y un `Cargo.lock` coherente.
  Un workspace cuyas dependencias nunca se descargaron falla de forma visible
  en lugar de descargarlas por su cuenta.
- Los artefactos de compilación de la indexación viven bajo el directorio de
  estado y son desechables.
- Un repositorio en solo lectura es indexable.

## Riesgos

- **`--locked` frente a lockfiles desactualizados.** Un repositorio cuyo
  `Cargo.lock` no corresponde a sus manifests no se indexa. Es el
  comportamiento correcto: la alternativa es que Kivgraph modifique el
  repositorio.
- **Build scripts arbitrarios.** Indexar un repositorio Rust ejecuta código de
  ese repositorio. Es inherente a cualquier análisis de Cargo con precisión y
  debe quedar dicho en la documentación de instalación, junto con la opción de
  desactivarlo.
- **Caché compartida de cargo.** Dos indexaciones simultáneas comparten el
  caché del usuario; el target dir propio evita que se bloqueen entre sí.

## Verificación

`TestRunIndexesAWorkspaceWithoutWritingToIt` indexa una copia del fixture y
comprueba que el workspace no ganó `target/` ni `Cargo.lock`. El guardián de
escrituras corre además en cada invocación real: si el árbol cambia, la unidad
falla con `REPOSITORY_WRITTEN` en lugar de publicar hechos obtenidos de un
repositorio que la propia pasada modificó.

Observado durante la medición: con `CARGO_NET_OFFLINE` y `--offline`, la
resolución de metadatos del sysroot puede degradarse a `--no-deps` y el
analizador lo dice por `stderr`. Ese aviso se conserva como diagnóstico de la
pasada; no se silencia.
