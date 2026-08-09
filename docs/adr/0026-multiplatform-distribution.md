# ADR 0026: Distribución multiplataforma

- **Estado:** aceptada
- **Fecha:** 2026-08-09
- **Revisa:** ADR 0015, ADR 0025

## Contexto

ADR 0015 fijó un único bundle `linux/amd64` producido por
`scripts/build-linux-amd64.sh`. ADR 0025 dejó `darwin/arm64` compilando y
verificándose desde un checkout, pero sin artefacto instalable.

Añadir macOS duplicando el script habría creado una segunda convención para el
mismo contrato -layout, manifest, `SHA256SUMS`, provenance- y dos sitios donde
divergir. Las diferencias reales entre plataformas son pocas y concretas.

## Decisión

### Un solo script parametrizado

`scripts/build-bundle.sh` sustituye a `scripts/build-linux-amd64.sh`. Acepta
`--target OS/ARCH` y sólo admite el objetivo que coincide con el host: cgo
enlaza la biblioteca nativa fijada, así que no hay ruta de cross-compilation.
El `Makefile` conserva nombres explícitos, `make build-linux-amd64` y
`make build-darwin-arm64`, que delegan en el script.

Lo que cambia por plataforma, y nada más:

| Aspecto | `linux/amd64` | `darwin/arm64` |
| --- | --- | --- |
| Biblioteca | `lib/liblbug.so` | `lib/liblbug.dylib` |
| `RUNPATH` relativo | `$ORIGIN/../lib` | `@loader_path/../lib` |
| Digest | `sha256sum` | `shasum -a 256` |
| Tamaño de archivo | `stat -c '%s'` | `stat -f '%z'` |

El script elige la herramienta de digest y de `stat` por disponibilidad, no por
nombre de sistema, así que un host con coreutils instalado se comporta igual en
ambos casos.

La comprobación de que el `RUNPATH` relativo basta ya no usa
`LD_LIBRARY_PATH`: el script ejecuta `bin/ladygraph version` sin ninguna
variable de búsqueda de bibliotecas. Si el enlazado fuera incorrecto, el build
falla ahí.

### Nomenclatura de release

- Directorio del bundle y raíz del tar: `ladygraph-<os>-<arch>`.
- Archivo publicado: `ladygraph-<os>-<arch>.tar.gz`.
- `SHA256SUMS` de la release: un único fichero que lista **todos** los archivos
  publicados, en orden lexicográfico. El instalador extrae la línea de su
  propio archivo y verifica sólo esa; si no existe, la release no publica esa
  plataforma y el instalador lo dice.
- `SHA256SUMS` dentro del bundle: sin cambios; sigue listando `manifest.json` y
  cada archivo del payload, y se verifica entero.

`manifest.json` registra el `target.os`/`target.arch` real. `ladygraph version
--json` y `ladygraph update` validan contra la plataforma en ejecución en vez
de contra literales.

### Un solo `RUNPATH`

El binding fijado declara su propio `RUNPATH` hacia su directorio de módulo,
que no contiene ninguna biblioteca y nombra la máquina que construyó el
bundle. Después de enlazar, el script deja exactamente la entrada relativa
-`install_name_tool -delete_rpath` y una firma ad-hoc nueva en macOS,
`patchelf --set-rpath` en Linux- y falla si sobrevive cualquier otra. Un
bundle deja así de depender de dónde guardaba su caché de módulos quien lo
generó.

### Instalador y CI

`scripts/install.sh` deduce la pareja `<os>-<arch>` de `uname`, rechaza
cualquier otra nombrando lo observado, y usa sólo opciones de `tar` que aceptan
GNU tar y bsdtar: `--no-overwrite-dir` no existe en macOS. Las validaciones de
seguridad previas a la extracción -listado de entradas, prefijo, tipos,
symlinks- se conservan intactas.

El workflow de release construye una matriz `ubuntu-24.04` → `linux/amd64` y
`macos-15` → `darwin/arm64`, y un job final compone el `SHA256SUMS` común.

## Consecuencias

### Verificado

Bundle `darwin/arm64` generado con `scripts/build-bundle.sh --target
darwin/arm64 --mcp-only`:

- `bin/ladygraph version` arranca **sin** variables de entorno; el `RUNPATH`
  relativo resuelve la `dylib`.
- `otool -l` declara un único `LC_RPATH`, `@loader_path/../lib`.
- `shasum -a 256 -c SHA256SUMS` verifica los `703` archivos del bundle.
- La `dylib` conserva su firma ad-hoc al copiarse y el ejecutable se vuelve a
  firmar ad-hoc tras editar sus load commands: `Signature=adhoc` en ambos.
- Dos builds consecutivos producen payload y `manifest.json` idénticos byte a
  byte, también con la firma rehecha.

### Firma y Gatekeeper: no se notariza

El proyecto **no** usa un Developer ID y no notariza sus artefactos. No es una
tarea pendiente: la ruta de instalación soportada no lo necesita.

Dos mecanismos distintos que se confunden a menudo:

1. **Ejecutar en Apple Silicon** exige que el binario lleve *alguna* firma
   válida. La aporta el enlazador de Go, y el script la rehace tras editar los
   load commands. Verificado sobre el artefacto final: `codesign --verify
   --strict` responde `valid on disk` y `satisfies its Designated Requirement`,
   con `Signature=adhoc` y `TeamIdentifier=not set`.
2. **Gatekeeper** sólo interviene cuando el archivo lleva el atributo
   `com.apple.quarantine`, que escriben los navegadores y otros clientes de
   LaunchServices, no `curl` ni `tar`.

Medido en macOS `26.6` sobre `bin/ladygraph` del bundle:

| Situación | Resultado |
| --- | --- |
| Descarga con `curl` y extracción con `tar` | Sólo `com.apple.provenance`; ejecuta |
| `com.apple.quarantine` con flags `0001`, `0081` u `0083` | `SIGKILL`, `exit=137` |
| Tras `xattr -dr com.apple.quarantine` | Ejecuta |

`spctl -a -t exec` responde `rejected`, que es lo esperado sin Developer ID.
Esa evaluación sólo se consulta para un archivo en cuarentena, así que no
afecta a la instalación por `curl ... | bash`.

Un Developer ID haría falta únicamente para que un artefacto **descargado con
un navegador**, o un `.pkg`/`.dmg`, arrancase sin que el usuario retire la
cuarentena. Mientras la distribución sea el instalador, la documentación indica
`xattr -dr com.apple.quarantine` para quien descargue el `.tar.gz` a mano.
Conviene saber además que un `.tar.gz` de binarios sueltos no admite
`stapler`: la notarización sólo se puede grapar a un bundle de aplicación, un
`.dmg` o un `.pkg`, de modo que ni siquiera comprando el certificado
desaparecería la comprobación en línea para este formato.

### Límites

- La rama Linux del build exige `patchelf`, que no viene preinstalado en el
  runner; el workflow lo instala. macOS usa herramientas de las Command Line
  Tools.
- `darwin/amd64` **no entra en el alcance**. Los Mac Intel no se publican, y no
  por coste técnico: el asset nativo es universal y añadirlo sería un runner
  más en la matriz. Es una decisión de producto, así que el instalador la
  nombra en vez de dejar al usuario adivinando: `unsupported host
  Darwin/x86_64 (supported: ... Darwin/arm64 for Apple Silicon; Intel Macs are
  not published)`.
- El bundle sigue sin incluir Node.js ni las bibliotecas estándar del sistema.
