# Instalación y primera ejecución

Esta guía instala Ladygraph como un servidor MCP local y prepara su primer
índice. El flujo recomendado usa el bundle publicado para la plataforma, que
incluye el binario Go, la biblioteca nativa fijada de LadybugDB, el worker
TypeScript, las grammars, los avisos de licencia y el visor web bajo `web/`.

El visor viene en la release: tras instalar, `ladygraph ui` sirve el grafo
publicado en `0.0.0.0:7777` sin construir nada, porque lo normal es indexar
donde están los repositorios y mirar el grafo desde otra máquina. Un bundle
generado con `--mcp-only` no lo lleva, y en ese caso la ayuda marca `ui` como
no disponible en lugar de ofrecer un comando que termina en error.

**El visor no lleva autenticación** y sus respuestas contienen rutas de
repositorio y de fichero, nombres de símbolo y firmas. Con el bind por defecto
lo ve cualquiera que alcance el puerto: `ui` lo advierte en cada arranque. Para
restringirlo, `ladygraph ui --addr 127.0.0.1:7777` o `web.address` en la
configuración. Una configuración ya escrita conserva su valor: cambiarlo es
editar `web.address`.

El visor web empaquetado usa Reagraph `4.32.0` sobre el payload binario `LGVB`.
Cada vista materializada está limitada a `10.000` nodos por tile y `32 MiB`
por respuesta; el LOD oculta detalle lejano y una vista mayor debe solicitar
tiles o una neighborhood acotada en vez de truncarse en el navegador.

Ladygraph no copia ni modifica los repositorios registrados. El grafo canónico,
los snapshots y los backups se escriben en el directorio de estado configurado.

## Compatibilidad y requisitos

### Bundles publicados

La release publica un bundle por plataforma soportada:

| Plataforma | Archivo | Biblioteca nativa |
| --- | --- | --- |
| Linux `x86_64`/`amd64` | `ladygraph-linux-amd64.tar.gz` | `lib/liblbug.so` |
| macOS `arm64` (Apple Silicon) | `ladygraph-darwin-arm64.tar.gz` | `lib/liblbug.dylib` |

Cada bundle necesita:

- Node.js `22` o posterior para ejecutar el worker TypeScript;
- las bibliotecas estándar del sistema. En Linux eso incluye una `glibc`
  compatible con el entorno donde se compiló el bundle.

El instalador de releases requiere además Bash, `curl`, `tar` y una herramienta
SHA-256: usa `sha256sum` si existe y `shasum -a 256` en su lugar, que es lo que
trae macOS. Go, pnpm y un compilador C no son necesarios para instalar o
ejecutar un bundle publicado.

LadybugDB se carga desde `lib/` mediante el `RUNPATH` relativo del ejecutable
-`$ORIGIN/../lib` en Linux, `@loader_path/../lib` en macOS-; no se debe separar
`bin/`, `lib/`, `worker/`, `grammars/` ni el directorio `web/` si está
presente.

El runtime del worker contiene `typescript` y su paquete nativo de plataforma;
por eso no hace falta ejecutar una instalación de pnpm en la máquina destino.

El bundle no incluye Node.js ni las bibliotecas estándar del sistema, y no es
portable entre plataformas.

En macOS, el archivo descargado con `curl` no recibe `com.apple.quarantine` y
el ejecutable arranca con normalidad; los binarios no están notarizados, así
que una copia descargada con un navegador sí queda en cuarentena y hay que
retirarla con `xattr -dr com.apple.quarantine <ruta>`.

### Build desde el código fuente

Para compilar desde un checkout se necesitan:

- Go `1.26` o posterior, con la versión de toolchain fijada por `go.mod`. El
  indexador comprueba tipos con el `go/types` con el que fue compilado, así
  que solo puede leer repositorios y dependencias escritos para su versión del
  lenguaje o anterior;
- Node.js `22` o posterior;
- pnpm `11.5.1`, fijado en `ts-worker/package.json` y `web/package.json`;
- un compilador C y las herramientas de enlace de la plataforma;
- `git`, `curl`, `make`, `tar` y `sha256sum` o `shasum`.

El build nativo debe usar el tag Go `ladybug` y el par LadybugDB core/binding
fijado por `scripts/fetch-ladybug.sh`. No se debe sustituir la biblioteca por
una versión `latest` ni mezclar core y binding de versiones distintas.

Las plataformas verificadas para compilar desde fuente son `linux/amd64` y
`darwin/arm64`. En macOS, ver [Desarrollo en macOS](development/macos.md).

## Instalar la última release

El instalador detecta la plataforma, descarga la release publicada más
reciente para ella, verifica el checksum del archivo y después los checksums
internos del bundle:

```bash
curl -fsSL https://github.com/Luqueee/ladygraph/releases/latest/download/install.sh | bash
```

El instalador no requiere Go, pnpm ni un compilador C. Para fijar una versión:

```bash
LADYGRAPH_VERSION=v0.3.4 ./scripts/install.sh
```

Si el repositorio de releases es privado, proporciona
`LADYGRAPH_GITHUB_TOKEN` al instalador.

Comprueba y aplica actualizaciones desde el bundle instalado:

```bash
ladygraph update --check
ladygraph update
```

`ladygraph update` descarga la última release, rechaza tags que no sean SemVer,
verifica el checksum externo, el `manifest.json`, todos los checksums internos y
la versión del ejecutable. Solo después reemplaza atómicamente el bundle
instalado; la configuración y el estado del grafo permanecen fuera del bundle.
Reinicia el cliente MCP después de actualizar.

## Instalar un bundle

El artefacto es el directorio `ladygraph-<os>-<arch>/` generado por el build de
distribución. Antes de instalarlo, comprueba sus hashes desde la raíz del
bundle, con `sha256sum -c SHA256SUMS` o, en macOS, `shasum -a 256 -c
SHA256SUMS`:

```bash
bundle=/ruta/al/ladygraph-darwin-arm64
(
  cd "$bundle"
  shasum -a 256 -c SHA256SUMS
)
```

La orden debe terminar con `OK` para cada entrada. Si falla cualquier hash, no
ejecutes el binario: conserva el artefacto para investigación y vuelve a
obtener una copia íntegra.

Una instalación por usuario conserva la estructura del bundle y no necesita
permisos de administrador:

```bash
install_root="$HOME/.local/opt/ladygraph"
mkdir -p "$install_root"
cp -a "$bundle/." "$install_root/"
export PATH="$install_root/bin:$PATH"
```

Para persistir el `PATH`, añade la exportación a la configuración de la shell
que utilice el cliente MCP. No crees un symlink separado para
`ladygraph-ts-worker`: su launcher calcula la raíz del bundle a partir de su
propia ubicación.

Verifica el runtime y la procedencia antes de inicializar datos:

```bash
node --version
ladygraph version
ladygraph version --json
ladygraph-ts-worker <<'EOF'
hello
EOF
```

`ladygraph version --json` debe mostrar el `target.os` y `target.arch` de la
plataforma, la versión de LadybugDB y el digest de la biblioteca. El launcher
del worker debe imprimir `hello`; también enruta el subcomando `facts` que usa
el indexador TypeScript.

El bundle se genera desde el repositorio con el objetivo de su plataforma:

```bash
make build-linux-amd64
make build-darwin-arm64
```

Ambos delegan en `scripts/build-bundle.sh --target <os>/<arch>`, que sólo
acepta el objetivo del propio host: cgo enlaza la biblioteca nativa, así que no
hay cross-compilation. El comando recrea `dist/ladygraph-<os>-<arch>/`,
descarga y verifica el asset nativo, instala las dependencias del worker con
`pnpm install --frozen`, construye `web/` con su lockfile cuando el paquete
existe, copia únicamente `web/dist` al bundle y valida `SHA256SUMS`. `dist/` es
generado e ignorado por Git.

En un checkout limpio, el `buildid` Go se deriva del commit y del estado
`dirty`, no de la ruta temporal del checkout. Dos builds sobre el mismo commit,
toolchain y plataforma producen el mismo payload; un árbol modificado se marca
`source.dirty: true` en el manifest.

El comando `ladygraph ui` sirve `web/index.html` y los demás archivos generados
del bundle. Un build sin el tag `webassets` o sin `web/` no sirve archivos del
checkout: responde `503` con un fallback que indica que el bundle web no está
construido.

## Compilar desde el código fuente

Este flujo es útil para desarrollo o para una plataforma para la que no exista
un bundle de distribución:

```bash
git clone https://github.com/Luqueee/ladygraph.git
cd ladygraph

scripts/fetch-ladybug.sh
pnpm --dir ts-worker install --frozen-lockfile
pnpm --dir ts-worker build

native_dir="$(scripts/fetch-ladybug.sh)"
mkdir -p "$HOME/.local/bin"
CGO_ENABLED=1 \
CGO_CFLAGS="-I$native_dir" \
CGO_LDFLAGS="-L$native_dir -llbug -Wl,-rpath,$native_dir" \
go build -tags ladybug -trimpath \
  -o "$HOME/.local/bin/ladygraph" \
  ./cmd/ladygraph
```

Para una ejecución desde el checkout, el indexador puede usar
`ts-worker/dist/facts-cli.js` como fallback cuando el comando por defecto
`ladygraph-ts-worker` no está en `PATH`. Para instalar el binario fuera del
checkout, crea un launcher que conserve esa resolución:

```bash
repo_root=/ruta/al/checkout/de/ladygraph
cat > "$HOME/.local/bin/ladygraph-ts-worker" <<EOF
#!/usr/bin/env bash
set -euo pipefail
if [[ "\${1:-}" == "facts" ]]; then
  shift
  exec node "$repo_root/ts-worker/dist/facts-cli.js" "\$@"
fi
exec node "$repo_root/ts-worker/dist/index.js" "\$@"
EOF
chmod 0755 "$HOME/.local/bin/ladygraph-ts-worker"
```

Alternativamente, configura `typescript.worker_command` con un comando
ejecutable que acepte la forma
`facts REPOSITORY PATH OUTPUT [--provider NAME=PATH]...`.

Comprueba el binario compilado con:

```bash
export PATH="$HOME/.local/bin:$PATH"
ladygraph version
```

Un binario compilado sin `-tags ladybug` puede servir para comandos que no abren
LadybugDB, pero no es una instalación funcional para indexar, diagnosticar o
publicar el grafo canónico.

`ladygraph --help` lista los comandos agrupados y `ladygraph <comando> --help`
describe sus banderas; ambos escriben en `stdout` y terminan con código `0`.
Los errores de un comando puntual se escriben en texto plano cuando `stderr` es
una terminal, y como registro JSON cuando es una tubería o un archivo, que es
lo que consume otra herramienta. `serve` y `ui` registran siempre en JSON.

## Inicializar la configuración

Genera los dos documentos de configuración y los directorios de estado:

```bash
ladygraph init
```

Por defecto se crean, con permisos restrictivos:

```text
~/.config/ladygraph/config.yaml
~/.config/ladygraph/repositories.yaml
~/.local/state/ladygraph/
```

`init` es no destructivo: si los archivos existen, los conserva y devuelve
`existing`. Para usar otras ubicaciones, pasa ambas rutas explícitamente:

```bash
ladygraph init \
  --config "$HOME/.config/ladygraph/config.yaml" \
  --repositories "$HOME/.config/ladygraph/repositories.yaml"
```

`--force` reemplaza los documentos de configuración y registro. Úsalo solo
cuando quieras descartar esos archivos locales; no modifica los repositorios
fuente, pero puede eliminar registros que todavía necesites.

### Registrar repositorios

Registra un repositorio Go y uno TypeScript en invocaciones separadas para que
cada entrada conserve sus lenguajes correctos:

```bash
ladygraph init \
  --repository backend=/ruta/absoluta/al/backend \
  --languages go

ladygraph init \
  --repository frontend=/ruta/absoluta/al/frontend \
  --languages typescript
```

Se pueden repetir `--repository` y `--languages` se aplica a todas las
entradas de esa invocación. Los nombres deben ser únicos y las rutas deben
apuntar a directorios accesibles. Una ruta relativa se resuelve respecto al
archivo `repositories.yaml`; se recomiendan rutas absolutas en instalaciones
que se ejecutan como servicio.

Para registrar más repositorios con la misma combinación de lenguajes:

```bash
ladygraph init \
  --repository shared=/ruta/absoluta/al/shared \
  --repository tools=/ruta/absoluta/al/tools \
  --languages go,typescript
```


### Monorepos y exclusiones

El registro acepta `exclusions` por repositorio en `repositories.yaml`. Los
patrones se aplican tanto durante el descubrimiento como durante la carga
semántica; por ejemplo, para no indexar fixtures o benchmarks:

```yaml
repositories:
  - name: backend
    path: /ruta/absoluta/al/backend
    languages: [go, typescript]
    exclusions:
      - '**/testdata'
      - '**/benchmarks'
      - dist
```

En un monorepo TypeScript, Ladygraph procesa cada `package.json` nombrado que
tenga un `tsconfig` aplicable de forma independiente. El `ProjectPath` elegido
es el `tsconfig` más profundo que contiene ese paquete; no se reutiliza
silenciosamente el `tsconfig` de la raíz para otro paquete. Un manifest sin
nombre o sin proyecto se conserva como configuración del workspace, pero no
genera hechos semánticos por sí solo.

Si un repositorio registrado como TypeScript no tiene ningún provider nombrado
con proyecto aplicable, `index --full` termina con error explícito; no publica
una generación vacía.

## Validar e indexar

Ejecuta el diagnóstico antes de abrir el servidor:

```bash
ladygraph doctor
```

El comando comprueba la configuración, los permisos de los directorios de
estado, el registro de repositorios, los toolchains requeridos, el layout de
`generation.Store`, el grafo activo y el snapshot. Devuelve `0` solo con
`doctor: PASS`; un `FAIL` se imprime sin ocultarlo y debe corregirse antes de
continuar.

Construye el índice completo y publica una generación validada:

```bash
ladygraph index --full
```

La operación:

1. extrae hechos de los repositorios registrados;
2. crea una generación candidata en el directorio de estado;
3. carga y valida el esquema canónico de LadybugDB;
4. construye el HotSnapshot desde la generación canónica;
5. actualiza `CURRENT` solo después de que pasen integridad, conteos y probes.

Un fallo deja intacta la generación activa anterior. Para registrar una versión
de resolver estable de la instalación, puede pasarse explícitamente:

```bash
ladygraph index --full --resolver-version mi-resolver-go-ts-v1
```

Usa `--config PATH` si la configuración no está en la ruta predeterminada, y
`--repositories PATH` solo cuando quieras sustituir el registro indicado por
`config.yaml` para esa ejecución.

Después de indexar, vuelve a ejecutar:

```bash
ladygraph doctor
```

Un resultado `PASS` debe incluir una generación activa, un digest de snapshot y
el conteo de referencias no resueltas retenidas. `UNRESOLVED` no es un error de
instalación: son hechos que el resolver no pudo demostrar exactamente y que el
contrato conserva como resultado distinto de `EXACT`.

## Ejecutar como servidor MCP

El transporte configurado por defecto es STDIO. Arranca el servidor con:

```bash
ladygraph serve
```

Para un cliente MCP que ejecute procesos directamente, usa el binario y sus
argumentos sin envolverlos en `sh -c`:

```json
{
  "mcpServers": {
    "ladygraph": {
      "command": "/home/usuario/.local/opt/ladygraph/bin/ladygraph",
      "args": [
        "serve",
        "--config",
        "/home/usuario/.config/ladygraph/config.yaml"
      ]
    }
  }
}
```

El proceso escribe exclusivamente el framing MCP en `stdout`; los logs van a
`stderr`. El cliente debe conservar `stderr` para diagnóstico y reenviar
`SIGINT` o `SIGTERM` para permitir el cierre limpio. No arranques `serve` antes
de publicar una generación: sin una generación activa no hay símbolos que
servir.

## Rutas y mantenimiento

Con la configuración por defecto, las rutas principales son:

| Ruta | Contenido |
| --- | --- |
| `~/.config/ladygraph/config.yaml` | configuración versionada (`version: 1`) |
| `~/.config/ladygraph/repositories.yaml` | registro de repositorios |
| `~/.local/state/ladygraph/graph.lbdb` | ruta base configurada del almacenamiento |
| `~/.local/state/ladygraph/generations/` | generaciones publicadas y candidatas |
| `~/.local/state/ladygraph/snapshots/` | snapshots derivados retenidos |
| `~/.local/state/ladygraph/backups/` | backups de upgrades de schema |
| `~/.local/state/ladygraph/go.work` | workspace sintético temporal de Go |

El snapshot es una proyección derivada. LadybugDB en la generación publicada es
la fuente canónica; no edites sus archivos a mano ni reemplaces un snapshot sin
reconstruirlo desde esa generación.

Si una actualización detecta un schema canónico incompatible, usa el flujo
seguro de upgrade:

```bash
ladygraph upgrade
```

`upgrade` crea un backup verificable, reconstruye desde los repositorios
registrados y publica atómicamente solo una generación que pase validación. No
borres `CURRENT`, `BACKUP` ni una generación retenida para intentar reparar un
upgrade fallido.

## Diagnóstico de fallos

### La verificación de `SHA256SUMS` falla

El bundle está incompleto o fue alterado. No continúes; consigue de nuevo el
artefacto y conserva el `manifest.json`, `SHA256SUMS` y el mensaje de error para
la auditoría. En macOS la orden es `shasum -a 256 -c SHA256SUMS`.

### `liblbug` no se carga

En Linux el mensaje es `error while loading shared libraries: liblbug.so`; en
macOS, `dyld: Library not loaded: @rpath/liblbug.dylib`. En ambos casos se
ejecutó el binario fuera de la estructura del bundle o se copió solo
`bin/ladygraph`. Reinstala el directorio completo y añade `bundle/bin` al
`PATH`; el ejecutable ya contiene el `RUNPATH` relativo a `../lib`.

En un build fuente, confirma que `scripts/fetch-ladybug.sh` devuelve la
biblioteca correcta y que el binario se compiló con `CGO_ENABLED=1`, los flags
CGO mostrados arriba y `-tags ladybug`.

### `toolchain.typescript: FAIL`

Comprueba `node --version` (debe ser `22` o posterior) y que
`ladygraph-ts-worker` sea ejecutable desde el mismo entorno que ejecutará
`ladygraph`. En un bundle, el launcher debe permanecer junto a `ladygraph` y
`worker/`. En un checkout fuente, ejecuta `pnpm --dir ts-worker build` o usa el
fallback documentado en la sección de compilación.

### El proceso muere al arrancar en macOS

El archivo se descargó con un navegador y quedó en cuarentena. Retírala con
`xattr -dr com.apple.quarantine <ruta>`; los binarios no están notarizados.

### `config: FAIL` o `unsupported schema version`

Revisa que `config.yaml` declare `version: 1`, que no tenga claves desconocidas
y que `workspace.repositories_file` apunte a un registro existente. No copies
campos de una versión futura: una configuración incompatible debe rechazarse,
no reinterpretarse silenciosamente.

### `doctor` informa un directorio inseguro

Los directorios de estado deben ser privados (`0700`). Corrige el propietario
y los permisos del directorio indicado sin hacerlos globalmente escribibles.
Después repite `ladygraph doctor`.

### No hay generación publicada

`doctor` puede mostrar `no published generation` después de un `init` limpio.
Registra al menos un repositorio accesible y ejecuta `ladygraph index --full`.
No intentes crear `CURRENT` manualmente.

### El índice falla por un repositorio

Confirma que la ruta existe, que el proceso puede leerla y que el repositorio
conserva sus metadatos (`go.mod`, `package.json` o `tsconfig.json`, según el
lenguaje). El indexador escribe el detalle en `stderr` y devuelve un código
no cero; corrige el repositorio o su registro y repite la operación.

## Limitaciones observadas

- El artefacto distribuible documentado es únicamente `linux/amd64` y depende
  de las bibliotecas estándar del sistema y de Node.js. `darwin/arm64` se
  compila y se verifica desde un checkout, pero no tiene bundle publicado ni
  instalador; `ladygraph update` lo rechaza nombrando la plataforma. Ver
  [Desarrollo en macOS](development/macos.md).
- La configuración se valida sintácticamente y por permisos durante `doctor`;
  comprobaciones adicionales de políticas de repositorio pertenecen a la
  indexación.
- El primer índice es una operación completa. El watcher incremental y las
  actualizaciones posteriores deben seguir la configuración y los gates del
  proyecto; no sustituyen la validación de una generación canónica.
- El bundle generado desde un árbol modificado conserva `source.dirty: true` en
  `manifest.json`; para distribuir una versión reproducible usa un checkout
  limpio y conserva `SHA256SUMS` junto al artefacto.

## Referencias

- [Configuración](adr/0008-configuration.md)
- [Distribución Linux amd64](adr/0015-linux-amd64-distribution.md)
- [Desarrollo en macOS](adr/0025-macos-development-support.md)
- [Schema canónico](storage/canonical-schema.md)
- [SLO de rendimiento](performance/slo.md)
- [Backlog de implementación](../TASKS.md)
