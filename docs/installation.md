# Instalación y primera ejecución

Esta guía instala Kivgraph como un servidor MCP local y prepara su primer
índice. El flujo recomendado usa el bundle publicado para la plataforma, que
incluye el binario Go, la biblioteca nativa fijada de LadybugDB, el worker
TypeScript, las grammars, los avisos de licencia y el visor web bajo `web/`.

El visor viene en la release: tras instalar, `kivgraph ui` sirve el grafo
publicado en `0.0.0.0:7777` sin construir nada, porque lo normal es indexar
donde están los repositorios y mirar el grafo desde otra máquina. Un bundle
generado con `--mcp-only` no lo lleva, y en ese caso la ayuda marca `ui` como
no disponible en lugar de ofrecer un comando que termina en error.

**El visor no lleva autenticación** y sus respuestas contienen rutas de
repositorio y de fichero, nombres de símbolo y firmas. Con el bind por defecto
lo ve cualquiera que alcance el puerto: `ui` lo advierte en cada arranque. Para
restringirlo, `kivgraph ui --addr 127.0.0.1:7777` o `web.address` en la
configuración. Una configuración ya escrita conserva su valor: cambiarlo es
editar `web.address`.

El visor web empaquetado usa Reagraph `4.32.0` sobre el payload binario `LGVB`.
Cada vista materializada está limitada a `10.000` nodos por tile y `32 MiB`
por respuesta; el LOD oculta detalle lejano y una vista mayor debe solicitar
tiles o una neighborhood acotada en vez de truncarse en el navegador.

Kivgraph no copia ni modifica los repositorios registrados. El grafo canónico,
los snapshots y los backups se escriben en el directorio de estado configurado.

## Compatibilidad y requisitos

### Bundles publicados

La release publica un bundle por plataforma soportada:

| Plataforma | Archivo | Biblioteca nativa |
| --- | --- | --- |
| Linux `x86_64`/`amd64` | `kivgraph-linux-amd64.tar.gz` | `lib/liblbug.so` |
| macOS `arm64` (Apple Silicon) | `kivgraph-darwin-arm64.tar.gz` | `lib/liblbug.dylib` |

Cada bundle lleva dentro los tres motores que Kivgraph ejecuta: la biblioteca
nativa de LadybugDB en `lib/`, el worker TypeScript en `worker/` y
`bin/rust-analyzer`. Todos entran en `SHA256SUMS` y en `manifest.json`, y
`kivgraph version --json` publica sus versiones.

Cada bundle necesita:

- Node.js `22` o posterior para ejecutar el worker TypeScript;
- `cargo` en el `PATH` **solo** si se indexan repositorios Rust. El bundle
  lleva su propio `bin/rust-analyzer`, fijado y verificado por digest, y el
  ejecutable lo prefiere al del `PATH`; lo que no lleva es un toolchain de
  Rust, y sin `cargo` el analizador no puede cargar un workspace. Sin él, el
  repositorio Rust se declara `WORKSPACE_NOT_LOADED` y el resto del grafo se
  publica igual;
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
curl -fsSL https://github.com/Luqueee/kivgraph/releases/latest/download/install.sh | bash
```

El instalador no requiere Go, pnpm ni un compilador C. Para fijar una versión:

```bash
KIVGRAPH_VERSION=v0.7.0 ./scripts/install.sh
```

Si el repositorio de releases es privado, proporciona
`KIVGRAPH_GITHUB_TOKEN` al instalador.

Comprueba y aplica actualizaciones desde el bundle instalado:

```bash
kivgraph update --check
kivgraph update
```

`kivgraph update` descarga la última release, rechaza tags que no sean SemVer,
verifica el checksum externo, el `manifest.json`, todos los checksums internos y
la versión del ejecutable. Solo después reemplaza atómicamente el bundle
instalado; la configuración y el estado del grafo permanecen fuera del bundle.
Reinicia el cliente MCP después de actualizar.

## Instalar un bundle

El artefacto es el directorio `kivgraph-<os>-<arch>/` generado por el build de
distribución. Antes de instalarlo, comprueba sus hashes desde la raíz del
bundle, con `sha256sum -c SHA256SUMS` o, en macOS, `shasum -a 256 -c
SHA256SUMS`:

```bash
bundle=/ruta/al/kivgraph-darwin-arm64
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
install_root="$HOME/.local/opt/kivgraph"
mkdir -p "$install_root"
cp -a "$bundle/." "$install_root/"
export PATH="$install_root/bin:$PATH"
```

Para persistir el `PATH`, añade la exportación a la configuración de la shell
que utilice el cliente MCP. No crees un symlink separado para
`kivgraph-ts-worker`: su launcher calcula la raíz del bundle a partir de su
propia ubicación.

Verifica el runtime y la procedencia antes de inicializar datos:

```bash
node --version
kivgraph version
kivgraph version --json
kivgraph-ts-worker <<'EOF'
hello
EOF
```

`kivgraph version --json` debe mostrar el `target.os` y `target.arch` de la
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
hay cross-compilation. El comando recrea `dist/kivgraph-<os>-<arch>/`,
descarga y verifica el asset nativo, instala las dependencias del worker con
`pnpm install --frozen`, construye `web/` con su lockfile cuando el paquete
existe, copia únicamente `web/dist` al bundle y valida `SHA256SUMS`. `dist/` es
generado e ignorado por Git.

En un checkout limpio, el `buildid` Go se deriva del commit y del estado
`dirty`, no de la ruta temporal del checkout. Dos builds sobre el mismo commit,
toolchain y plataforma producen el mismo payload; un árbol modificado se marca
`source.dirty: true` en el manifest.

El comando `kivgraph ui` sirve `web/index.html` y los demás archivos generados
del bundle. Un build sin el tag `webassets` o sin `web/` no sirve archivos del
checkout: responde `503` con un fallback que indica que el bundle web no está
construido.

## Compilar desde el código fuente

Este flujo es útil para desarrollo o para una plataforma para la que no exista
un bundle de distribución:

```bash
git clone https://github.com/Luqueee/kivgraph.git
cd kivgraph

scripts/fetch-ladybug.sh
pnpm --dir ts-worker install --frozen-lockfile
pnpm --dir ts-worker build

native_dir="$(scripts/fetch-ladybug.sh)"
mkdir -p "$HOME/.local/bin"
CGO_ENABLED=1 \
CGO_CFLAGS="-I$native_dir" \
CGO_LDFLAGS="-L$native_dir -llbug -Wl,-rpath,$native_dir" \
go build -tags ladybug -trimpath \
  -o "$HOME/.local/bin/kivgraph" \
  ./cmd/kivgraph
```

Para una ejecución desde el checkout, el indexador puede usar
`ts-worker/dist/facts-cli.js` como fallback cuando el comando por defecto
`kivgraph-ts-worker` no está en `PATH`. Para instalar el binario fuera del
checkout, crea un launcher que conserve esa resolución:

```bash
repo_root=/ruta/al/checkout/de/kivgraph
cat > "$HOME/.local/bin/kivgraph-ts-worker" <<EOF
#!/usr/bin/env bash
set -euo pipefail
if [[ "\${1:-}" == "facts" ]]; then
  shift
  exec node "$repo_root/ts-worker/dist/facts-cli.js" "\$@"
fi
exec node "$repo_root/ts-worker/dist/index.js" "\$@"
EOF
chmod 0755 "$HOME/.local/bin/kivgraph-ts-worker"
```

Alternativamente, configura `typescript.worker_command` con un comando
ejecutable que acepte la forma
`facts REPOSITORY PATH OUTPUT [--provider NAME=PATH]...`.

Comprueba el binario compilado con:

```bash
export PATH="$HOME/.local/bin:$PATH"
kivgraph version
```

Un binario compilado sin `-tags ladybug` puede servir para comandos que no abren
LadybugDB, pero no es una instalación funcional para indexar, diagnosticar o
publicar el grafo canónico.

`kivgraph --help` lista los comandos agrupados y `kivgraph <comando> --help`
describe sus banderas; ambos escriben en `stdout` y terminan con código `0`.
Los errores de un comando puntual se escriben en texto plano cuando `stderr` es
una terminal, y como registro JSON cuando es una tubería o un archivo, que es
lo que consume otra herramienta. `serve` y `ui` registran siempre en JSON.

## Inicializar la configuración

Genera los dos documentos de configuración y los directorios de estado:

```bash
kivgraph init
```

Por defecto se crean, con permisos restrictivos:

```text
~/.config/kivgraph/config.yaml
~/.config/kivgraph/repositories.yaml
~/.local/state/kivgraph/
```

`init` es no destructivo: si los archivos existen, los conserva y devuelve
`existing`. Para usar otras ubicaciones, pasa ambas rutas explícitamente:

```bash
kivgraph init \
  --config "$HOME/.config/kivgraph/config.yaml" \
  --repositories "$HOME/.config/kivgraph/repositories.yaml"
```

`--force` reemplaza los documentos de configuración y registro. Úsalo solo
cuando quieras descartar esos archivos locales; no modifica los repositorios
fuente, pero puede eliminar registros que todavía necesites.

### Registrar repositorios

Registra un repositorio Go y uno TypeScript en invocaciones separadas para que
cada entrada conserve sus lenguajes correctos:

```bash
kivgraph init \
  --repository backend=/ruta/absoluta/al/backend \
  --languages go

kivgraph init \
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
kivgraph init \
  --repository shared=/ruta/absoluta/al/shared \
  --repository tools=/ruta/absoluta/al/tools \
  --languages go,typescript
```

La sección `go` permite seleccionar el contexto de compilación que resolverá
`go/packages`. `goos`, `goarch` y `cgo_enabled` vacíos conservan los valores del
toolchain; los tags y los tests forman parte del mismo contexto:

```yaml
go:
  synthetic_work_file: ~/.local/state/kivgraph/go.work
  include_tests: false
  goos: linux
  goarch: amd64
  cgo_enabled: false
  build_tags: [integration]
  allow_network: false
```

El grafo describe esa variante, no todos los binarios posibles. Para cubrir
varias plataformas hay que ejecutar una pasada por variante y conservar la
configuración de cada generación.


### Monorepos y exclusiones

El registro acepta `exclusions` por repositorio en `repositories.yaml`. Los
patrones se aplican tanto durante el descubrimiento como durante la carga
semántica; por ejemplo, para no indexar fixtures o benchmarks:

```yaml
repositories:
  - name: backend
    path: /ruta/absoluta/al/backend
    languages: [go, typescript, python, dart]
    exclusions:
      - '**/testdata'
      - '**/benchmarks'
      - dist
```

En un monorepo TypeScript, Kivgraph procesa cada `package.json` nombrado que
tenga un `tsconfig` aplicable de forma independiente. El `ProjectPath` elegido
es el `tsconfig` más profundo que contiene ese paquete; no se reutiliza
silenciosamente el `tsconfig` de la raíz para otro paquete. Un manifest sin
nombre o sin proyecto se conserva como configuración del workspace, pero no
genera hechos semánticos por sí solo.

Un paquete que envía JavaScript declara su proyecto con un `jsconfig.json`,
que Kivgraph lee como un `tsconfig` cuyo `allowJs` está implícito -- un valor
declarado en el fichero gana, `false` incluido. Donde conviven los dos en un
mismo directorio, el proyecto del paquete es el `tsconfig.json`. Ver el ADR
0070.

Un `include` con comodín reclama las extensiones que lee el compilador:
`.ts`, `.tsx`, `.mts` y `.cts` siempre, más `.js`, `.jsx`, `.mjs` y `.cjs`
cuando el proyecto declara `allowJs`.

Si un repositorio registrado como TypeScript no tiene ningún provider nombrado
con proyecto aplicable, `index --full` termina con error explícito; no publica
una generación vacía.

Un fichero `.ts` al que no llega el `files`/`include` de ningún `tsconfig` no
pertenece a ningún programa: el compilador no lo comprueba, el grafo no puede
verlo y nada lo declara ausente. Un árbol `tests/` junto a un proyecto que
declara `include: ["src/**/*.ts"]` es el caso típico. Para indexarlos:

```yaml
typescript:
  include_unclaimed_sources: true
```

Está apagado por defecto. Esos ficheros se cargan en el proyecto **inferido**
de TypeScript, cuyas opciones de compilación las elige Kivgraph y no el
proyecto que los habría declarado -- no hay ninguno --, y de ellos se recogen
sólo sus declaraciones y sus usos: un uso cuyo destino vive en otro paquete no
produce arista. Las condiciones exactas están en el ADR 0050.

Antes de pagar una pasada completa, `kivgraph doctor repositories` contesta si
cada repositorio registrado está estructurado para que se pueda leer, sin
indexar nada, y propone qué cambiar donde no lo está: el fichero de proyecto
que falta con su contenido, la clave de configuración, o el comando que hay
que ejecutar. Termina con código `1` sólo cuando algún hallazgo es `blocking`,
es decir cuando un repositorio o un paquete no aporta nada. `--json` emite el
informe entero con el `code` estable de cada hallazgo.

Python y Dart se activan igual que los demás lenguajes:

```yaml
python:
  indexer_command: kivgraph-python-worker
  analyzer_command: kivgraph-python-pyright
  analyzer_mode: fallback
  python_path: python3
  maximum_workers: 3
  include_tests: false
  include_generated: false
  include_external_packages: false
dart:
  analyzer_command: dart
  sdk_path: dart
  maximum_workers: 2
  include_tests: false
  include_generated: false
  include_external_packages: false
  include_sdk: false
  package_config: auto
  wait_for_analysis: true
  maximum_analysis_time: 5m
```

El worker Python incluido recorre `.py` y `.pyi`, conserva símbolos e imports
y clasifica sus inferencias como `CANDIDATE`; no inventa aristas exactas para
código dinámico. Para exactitud semántica configura `analyzer_mode: exact` y
un productor compatible con el payload de hechos. `kivgraph-python-pyright`
usa un servidor Pyright/BasedPyright LSP instalado en el sistema y falla de
forma explícita si no está disponible. Dart se resuelve con el Analysis Server del SDK y sus
referencias resueltas se publican como `EXACT_TYPECHECKED`. `package_config`
usa `.dart_tool/package_config.json` cuando vale `auto`; si se habilita
`include_external_packages`, sus raíces se entregan al Analysis Server para
resolver dependencias de Pub, pero Kivgraph solo publica los ficheros de los
repositorios registrados y, si se activa durante `index --full`, registra los
paquetes de Pub descubiertos como proveedores sintéticos. Las directivas
`export` se conservan como
`REEXPORTS`, y las declaraciones `part`/`part of` generan `PART_OF` entre los
módulos sintéticos de ambos ficheros. Los imports condicionales conservan sus
alternativas, prefijo y modo diferido en el payload. Por defecto se excluyen
`test/`, `integration_test/` y nombres generados (`.g.dart`, `.freezed.dart`,
etc.); se pueden incluir con las opciones correspondientes.

Cuando una importación Python o Dart nombra exactamente un único paquete de
otro repositorio registrado, la pasada añade `PACKAGE_DEPENDS_ON`. Esa arista
demuestra dependencia de paquete, no uso de un símbolo concreto. Para una
arista de símbolo cross-repository el proveedor semántico debe entregar una
identidad explícita del destino.

La matriz verificable de capacidades está en
`testdata/semantic-coverage/manifest.json`. Para validar que las cuatro rutas
semánticas tienen fixtures y tests ejecutables usa:

```bash
make semantic-coverage
```

Este gate exige el servidor `pyright-langserver` para Python exacto y el SDK
`dart` para Dart. El fallback de Python se mantiene para desarrollo, pero no
puede declarar compatibilidad semántica completa.

## Validar e indexar

Ejecuta el diagnóstico antes de abrir el servidor:

```bash
kivgraph doctor
```

El comando comprueba la configuración, los permisos de los directorios de
estado, el registro de repositorios, los toolchains requeridos, el layout de
`generation.Store`, el grafo activo y el snapshot. Devuelve `0` solo con
`doctor: PASS`; un `FAIL` se imprime sin ocultarlo y debe corregirse antes de
continuar.

Construye el índice completo y publica una generación validada:

```bash
kivgraph index --full
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
kivgraph index --full --resolver-version mi-resolver-go-ts-v1
```

Usa `--config PATH` si la configuración no está en la ruta predeterminada, y
`--repositories PATH` solo cuando quieras sustituir el registro indicado por
`config.yaml` para esa ejecución.

Después de indexar, vuelve a ejecutar:

```bash
kivgraph doctor
```

Un resultado `PASS` debe incluir una generación activa, un digest de snapshot y
el conteo de referencias no resueltas retenidas. `UNRESOLVED` no es un error de
instalación: son hechos que el resolver no pudo demostrar exactamente y que el
contrato conserva como resultado distinto de `EXACT`.

## Ejecutar como servidor MCP

El transporte configurado por defecto es STDIO. Arranca el servidor con:

```bash
kivgraph serve
```

Para un cliente MCP que ejecute procesos directamente, usa el binario y sus
argumentos sin envolverlos en `sh -c`:

```json
{
  "mcpServers": {
    "kivgraph": {
      "command": "/home/usuario/.local/opt/kivgraph/bin/kivgraph",
      "args": [
        "serve",
        "--config",
        "/home/usuario/.config/kivgraph/config.yaml"
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

### Un proceso para varios clientes

`serve` es un proceso por cliente, y cada uno paga su propia mitad privada del
coste de cargar el snapshot: `71,2 MB` medidos en Linux con cuatro servidores
contra el mismo fichero mapeado (`benchmarks/load-cost-resident`).

`daemon` sirve a varios clientes desde un proceso, por dos puertas a la vez: un
socket unix dentro del directorio de estado y el transporte Streamable HTTP en
loopback.

```bash
kivgraph daemon                      # 127.0.0.1:7788 y el socket
kivgraph daemon --addr 127.0.0.1:9000
```

El socket es `~/.local/state/kivgraph/daemon.sock`. **El directorio de estado es
la clave**: dos configuraciones apuntando a directorios distintos obtienen
demonios distintos, así que un cliente nunca alcanza un grafo construido a partir
de los repositorios de otro.

**Ningún cliente MCP marca un socket unix**, así que el socket no es el camino de
un editor: su configuración acepta un ejecutable o una `url`. Por eso el demonio
publica su endpoint y hay un comando que lo escribe:

```bash
kivgraph daemon install                 # launchd o systemd lo arranca y lo repone
kivgraph mcp install --daemon --target claude-code
```

`daemon install` es lo que le da **dueño** al proceso: un LaunchAgent en macOS,
una unit de usuario de systemd en Linux. Los dos lo arrancan con la sesión y lo
reponen si muere, y eso es lo que hace fiable una entrada `url` -- una entrada
`command` la arranca el cliente que la lee, pero una `url` apuntando a un demonio
que nadie repone deja a **todos** los clientes sin tools a la vez. `kivgraph
daemon status` dice si hay uno instalado y dónde vive su unit; `kivgraph daemon
remove` lo retira.

La unit es por directorio de estado, con un digest del directorio en su etiqueta,
así que dos configuraciones pueden tener dos demonios supervisados sin que uno
sustituya al otro. Un `kivgraph daemon &` a mano no tiene dueño: muere con la
terminal que lo lanzó. Y en una plataforma sin supervisor soportado el comando lo
dice y falla, en vez de dejar creer que instaló algo.

Eso lee `~/.local/state/kivgraph/daemon.json` -- modo `0600`, con la `url` y el
token-- y escribe la entrada que ese cliente entiende: `type: http` con una
cabecera `Authorization` para Claude Code, Claude Desktop y Oh My Pi, `type:
remote` para OpenCode, y `url` con `http_headers` para Codex.

**Ésa es la entrada por defecto**: `mcp install` apunta al demonio sin que se lo
pidan, y `--daemon` sólo cambia el fallo -- pedirlo a mano se niega donde el
defecto caería a `serve`. La salida explícita es `--stdio`, que escribe
exactamente lo que escribía el defecto anterior: un proceso por cliente.

Hay tres condiciones que escriben `stdio` por su cuenta, y las tres lo dicen:
ámbito `project`, porque la `url` lleva un token y ese fichero se commitea; una
plataforma sin supervisor soportado, porque ahí el demonio no tendría dueño; y
una máquina sin configuración todavía, porque no hay directorio de estado al que
apuntar. Nada de esto depende de que haya un proceso arrancado: son condiciones
declaradas, y por eso el mismo comando en la misma máquina escribe el mismo
fichero.

Reinstalar **sustituye** la entrada que encuentra, así que cambiar de transporte
deja un registro y no dos -- `command` junto a `url` bajo una clave son dos
registros, y el cliente elige transporte por la forma. Una entrada que nombra
**otra** instalación de `kivgraph` sigue exigiendo `--force`: ésa no es nuestra.

El token se guarda aparte, en `daemon.token`, y **sobrevive al reinicio**: una
entrada escrita una vez sigue valiendo cuando el demonio vuelve. El endpoint no
sobrevive, porque es liveness: se borra al parar.

En ámbito `project` la instalación con `--daemon` **se niega**. Un `.mcp.json` se
commitea, y un token en git no se retira borrándolo.

El bind se comprueba: una dirección que no sea loopback se rechaza nombrando lo
que se escaparía, y sólo `--allow-remote` la acepta, con un aviso. El servidor
valida además la cabecera `Origin`, que es lo que impide que una página web use
el navegador del propio usuario para leer el grafo.

El demonio corre en primer plano hasta recibir `SIGINT` o `SIGTERM`, igual que
`serve`; nadie lo arranca solo y nadie lo para solo. `kivgraph stop` lo reconoce
junto a `serve` y `ui`. Al parar desvincula su socket, así que el siguiente
arranque no encuentra el fichero ocupado; si un proceso muere a señal y deja el
socket detrás, el arranque siguiente comprueba si alguien contesta antes de
borrarlo -- un demonio vivo nunca se sustituye.

Cada sesión obtiene su propio servidor MCP sobre el store compartido, y eso es
deliberado: la superficie de tools se decide al construir un servidor, así que un
cliente que conecta después de un `index --full` ve la generación nueva. Un
servidor construido al arrancar seguiría diciendo que no hay grafo.

Una dirección unix es un campo de tamaño fijo en el kernel -- `104` bytes en
darwin, `108` en linux-- y `bind` trunca en vez de rechazar. Un directorio de
estado cuya ruta de socket no quepa se rechaza nombrando el límite, en vez de
dejar dos directorios compartiendo un socket. El directorio por defecto cabe de
sobra; uno muy profundo puede no caber, y en ese caso `serve` sigue siendo el
camino.

El ahorro está medido, y **a la carga que un editor produce de verdad**: contando
el event log de una máquina en uso, `48` de `51` servidores no recibieron
**ninguna** llamada, así que la mediana de una sesión real es cero y el máximo
observado, tres.

| carga | pendiente del demonio | N procesos | 8 clientes | 1 cliente |
| --- | --- | --- | --- | --- |
| ninguna llamada | indistinguible de cero | `10 MB` por cliente | `10`–`13` contra `77`–`81 MB` | el demonio cuesta `2`–`3 MB` más |
| `8` llamadas | `0,6`–`0,9 MB` por cliente | `39 MB` por cliente | `60`–`62` contra `323`–`330 MB` | empata |

Un servidor al que nadie pregunta cuesta `10 MB` y uno consultado `39`: el grafo
se lee cuando alguien pregunta, no al arrancar. A ocho clientes el demonio cuesta
**siete veces menos** sin consultas y cinco y media contestando; a uno empata o
pierde por un par de megabytes, así que la razón para instalarlo empieza en el
segundo cliente.

**Las dos puertas cuestan lo mismo** -- `9,8`–`10,6 MB` por cliente por HTTP contra
`10,0`–`10,7` por socket--, así que elegir HTTP, la única que un cliente MCP puede
configurar, no se paga.

La diferencia más grande no es ésa. Es el **pico**, y no depende de que nadie
pregunte nada:

| clientes | pico N procesos | pico 1 demonio |
| --- | --- | --- |
| `1` | `22`–`24 MB` | `26`–`28 MB` |
| `8` | **`179`–`186 MB`** | **`26`–`29 MB`** |

Ocho editores arrancando a la vez pagan siete veces más pico que un demonio; a uno
solo, el demonio pica algo más alto. Y un cliente nuevo se conecta antes --
`1,6`–`2,0 ms` contra `38`–`55` a ocho clientes-- porque una
sesión nueva no arranca nada. Sobre `108.737` símbolos de `kena`, en Linux:
`benchmarks/daemon-cost`.

Bajo tráfico sostenido -- `2.000` llamadas por sesión, que ninguna sesión real
hace-- HTTP sube a `11`–`13 MB` por cliente: el SDK de MCP da a cada sesión un buffer
de reanudación de `10 MiB` y las respuestas lo llenan. Es un techo, está en
`results-http-sustained.json`, y no es lo que cuesta un editor.

Lo que **no** es el ahorro en ninguna puerta es el snapshot: es el mismo fichero
mapeado en todos los servidores y esas páginas están limpias. Lo que está en
juego son las privadas.

## Rutas y mantenimiento

Con la configuración por defecto, las rutas principales son:

| Ruta | Contenido |
| --- | --- |
| `~/.config/kivgraph/config.yaml` | configuración versionada (`version: 1`) |
| `~/.config/kivgraph/repositories.yaml` | registro de repositorios |
| `~/.local/state/kivgraph/graph.lbdb` | ruta base configurada del almacenamiento |
| `~/.local/state/kivgraph/generations/` | generaciones publicadas y candidatas |
| `~/.local/state/kivgraph/snapshots/` | snapshots derivados retenidos |
| `~/.local/state/kivgraph/backups/` | backups de upgrades de schema |
| `~/.local/state/kivgraph/go.work` | workspace sintético temporal de Go |
| `~/.local/state/kivgraph/daemon.sock` | socket del demonio, mientras corre |
| `~/.local/state/kivgraph/daemon.json` | url y token publicados, mientras corre |
| `~/.local/state/kivgraph/daemon.token` | el token, `0600`, sobrevive al reinicio |

El snapshot es una proyección derivada. LadybugDB en la generación publicada es
la fuente canónica; no edites sus archivos a mano ni reemplaces un snapshot sin
reconstruirlo desde esa generación.

Si una actualización detecta un schema canónico incompatible, usa el flujo
seguro de upgrade:

```bash
kivgraph upgrade
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
`bin/kivgraph`. Reinstala el directorio completo y añade `bundle/bin` al
`PATH`; el ejecutable ya contiene el `RUNPATH` relativo a `../lib`.

En un build fuente, confirma que `scripts/fetch-ladybug.sh` devuelve la
biblioteca correcta y que el binario se compiló con `CGO_ENABLED=1`, los flags
CGO mostrados arriba y `-tags ladybug`.

### `toolchain.typescript: FAIL`

Comprueba `node --version` (debe ser `22` o posterior) y que
`kivgraph-ts-worker` sea ejecutable desde el mismo entorno que ejecutará
`kivgraph`. En un bundle, el launcher debe permanecer junto a `kivgraph` y
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
Después repite `kivgraph doctor`.

### No hay generación publicada

`doctor` puede mostrar `no published generation` después de un `init` limpio.
Registra al menos un repositorio accesible y ejecuta `kivgraph index --full`.
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
  instalador; `kivgraph update` lo rechaza nombrando la plataforma. Ver
  [Desarrollo en macOS](development/macos.md).
- La configuración se valida sintácticamente y por permisos durante `doctor`;
  comprobaciones adicionales de políticas de repositorio pertenecen a la
  indexación.
- **Todo índice es una operación completa.** No hay actualización incremental:
  `kivgraph index` acepta sólo `--full` y cada pasada publica una generación
  nueva, validada, en vez de mutar la vigente. El camino del delta se retiró en
  el [ADR 0057](adr/0057-el-camino-incremental-se-retira.md). Lo que abarata una
  reindexación es la caché de hechos, que sólo reanaliza lo que cambió.
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
