# Instalación y primera ejecución

Esta guía instala Ladygraph como un servidor MCP local y prepara su primer
índice. El flujo recomendado usa el bundle `linux/amd64`; el bundle incluye el
binario Go, la biblioteca nativa fijada de LadybugDB, el worker TypeScript, las
grammars y los avisos de licencia.

Ladygraph no copia ni modifica los repositorios registrados. El grafo canónico,
los snapshots y los backups se escriben en el directorio de estado configurado.

## Compatibilidad y requisitos

### Bundle `linux/amd64`

El bundle publicado por `scripts/build-linux-amd64.sh` requiere:

- Linux `x86_64`/`amd64`;
- `bash` para el launcher `ladygraph-ts-worker`;
- Node.js `22` o posterior para ejecutar el worker TypeScript;
- las bibliotecas estándar del sistema Linux, incluida `glibc` compatible con el
  entorno donde se compiló el bundle.

`pnpm`, Go y un compilador C no son necesarios para ejecutar un bundle ya
construido. LadybugDB se carga desde `lib/liblbug.so` mediante el `RUNPATH`
relativo del ejecutable; no se debe separar `bin/`, `lib/`, `worker/` ni
`grammars/`.

El runtime del worker contiene `typescript` y su paquete nativo de plataforma
`@typescript/typescript-linux-x64`; por eso no hace falta ejecutar una
instalación de pnpm en la máquina destino.

El bundle no incluye Node.js ni las bibliotecas estándar del sistema. No es un
artefacto portable a Windows, macOS o `arm64`.

### Build desde el código fuente

Para compilar desde un checkout se necesitan:

- Go `1.24` o posterior, con la versión de toolchain fijada por `go.mod`;
- Node.js `22` o posterior;
- pnpm `11.5.1` (la versión declarada en `ts-worker/package.json`);
- un compilador C y las herramientas de enlace de la plataforma;
- `git`, `curl`, `make`, `sha256sum` y `tar`.

El build nativo debe usar el tag Go `ladybug` y el par LadybugDB core/binding
fijado por `scripts/fetch-ladybug.sh`. No se debe sustituir la biblioteca por
una versión `latest` ni mezclar core y binding de versiones distintas.

## Instalar un bundle

El artefacto es el directorio `ladygraph-linux-amd64/` generado por el build de
distribución. Antes de instalarlo, comprueba sus hashes desde la raíz del
bundle:

```bash
bundle=/ruta/al/ladygraph-linux-amd64
(
  cd "$bundle"
  sha256sum -c SHA256SUMS
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

`ladygraph version --json` debe mostrar `target.os` `linux`, `target.arch`
`amd64`, la versión de LadybugDB y el digest de la biblioteca. El launcher del
worker debe imprimir `hello`; también enruta el subcomando `facts` que usa el
indexador TypeScript.

El bundle se genera desde el repositorio con:

```bash
make build-linux-amd64
```

Ese comando recrea `dist/ladygraph-linux-amd64/`, descarga y verifica el asset
nativo, instala las dependencias del worker con `pnpm install --frozen-lockfile`,
compila el worker y valida `SHA256SUMS`. `dist/` es generado e ignorado por Git.

En un checkout limpio, el `buildid` Go se deriva del commit y del estado
`dirty`, no de la ruta temporal del checkout. Dos builds sobre el mismo commit,
toolchain y plataforma producen el mismo payload; un árbol modificado se marca
`source.dirty: true` en el manifest.

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

### `sha256sum -c` falla

El bundle está incompleto o fue alterado. No continúes; consigue de nuevo el
artefacto y conserva el `manifest.json`, `SHA256SUMS` y el mensaje de error para
la auditoría.

### `error while loading shared libraries: liblbug.so`

Se ejecutó el binario fuera de la estructura del bundle o se copió solo
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
  de las bibliotecas estándar del sistema y de Node.js.
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
- [Schema canónico](storage/canonical-schema.md)
- [SLO de rendimiento](performance/slo.md)
- [Backlog de implementación](../TASKS.md)
