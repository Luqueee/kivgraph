# Desarrollo en macOS

Kivgraph se compila, se verifica y se distribuye en `darwin/arm64`. El bundle
se genera con `make build-darwin-arm64` y la release publica
`kivgraph-darwin-arm64.tar.gz`, que instala `scripts/install.sh`. Las
decisiones y sus límites están en
[ADR 0025](../adr/0025-macos-development-support.md) y
[ADR 0026](../adr/0026-multiplatform-distribution.md).

## Requisitos

- macOS con las Command Line Tools de Xcode (compilador C y enlazador).
- Go con la versión de toolchain fijada por `go.mod`.
- Node.js `22` o posterior y pnpm `11.5.1` para `ts-worker/` y `web/`.
- `curl`, `tar`, `git` y `shasum` -incluido en el sistema-. No hace falta
  instalar `coreutils`: `scripts/fetch-ladybug.sh` usa `sha256sum` si existe y
  `shasum -a 256` en su lugar.

## Biblioteca nativa

`scripts/fetch-ladybug.sh` descarga y verifica `liblbug-osx-universal.tar.gz`
para la versión fijada y escribe `.tooling/ladybug/<versión>/liblbug.dylib`. El
asset es universal (`x86_64` y `arm64`), está firmado ad-hoc y declara
`@rpath/liblbug.dylib` como `install_name`.

Un build nativo debe añadir su propio `RUNPATH`. El binding fijado enlaza con
un `rpath` hacia su directorio de módulo, que no contiene ninguna biblioteca;
sin `-Wl,-rpath` propio, `dyld` aborta el arranque con `Library not loaded:
@rpath/liblbug.dylib`.

```bash
LIB="$(scripts/fetch-ladybug.sh)"
CGO_ENABLED=1 \
CGO_CFLAGS="-I$LIB" \
CGO_LDFLAGS="-L$LIB -llbug -Wl,-rpath,$LIB" \
go build -tags ladybug -o kivgraph ./cmd/kivgraph
```

El enlazador emite avisos `duplicate -rpath` y `ignoring duplicate libraries`
porque el binding ya declara los suyos. Son inocuos.

## Verificación

```bash
gofmt -l <archivos-go-modificados>
go vet ./...
go test ./...
make build
make test-ladybug
```

El bundle se genera y se comprueba con:

```bash
make build-darwin-arm64
(cd dist/kivgraph-darwin-arm64 && shasum -a 256 -c SHA256SUMS)
```

`make test-ladybug` exporta las mismas variables `CGO_*` del ejemplo anterior;
sin ellas, `go test -tags ladybug` no enlaza y aborta con
`ld: library 'lbug' not found`. Para trabajar sobre un solo paquete:

```bash
make test-ladybug PKGS=./internal/storage/ladybug
```

## Diferencias observables frente a Linux

- **Watcher.** El backend `kqueue` mantiene un descriptor por archivo y por
  directorio vigilado -`787` para este checkout- contra un techo
  `kern.maxfilesperproc` de `92160`. Un árbol mayor falla en `New` nombrando el
  límite. Además, un archivo creado y escrito de una vez llega como un único
  `Create`: la escritura ocurre antes de que el archivo esté vigilado.
- **Doctor.** El check `lock` enumera descriptores con `libproc` en lugar de
  leer `/proc/locks`. Un holder de otro usuario, que exigiría privilegios, no
  es visible.
- **Rutas.** La validación rechaza cualquier ruta con un componente symlink en
  su ascendencia, y en macOS `/tmp` y `/var` lo son. Un repositorio bajo
  `/Users/...` no se ve afectado; un directorio temporal sí. Los tests que
  alimentan la capa de workspace usan `internal/testsupport.TempDir`, que
  resuelve el realpath, en lugar de `t.TempDir()`.

## Filesystem que pliega mayúsculas

APFS es insensible a mayúsculas por defecto. El motor TypeScript canonicaliza
a minúsculas las rutas que resuelve él mismo -las de un módulo importado, no
las del propio proyecto-, y esas rutas entran en las stable keys y en la
evidencia. Además dejan de casar con los índices que el worker construye a
partir de rutas reales, de modo que las declaration maps se perdían y el
fixture negativo producía un `UNRESOLVED` de más.

`ts-worker/src/engine-path.ts` corrige ese plegado en la frontera: recorre la
ruta componente a componente y devuelve la grafía del disco. No usa
`realpath`, que resolvería un enlace de `node_modules` hacia el almacén
`.pnpm` y cambiaría los hechos; corrige sólo mayúsculas y minúsculas. Las
listas de directorio se memorizan, y un fallo sólo se acepta después de releer
el directorio, porque un índice memorizado es anterior a lo que escribe una
indexación en curso.

La corrección se aplica donde una ruta del motor entra por primera vez en los
datos del worker: las declaraciones de un símbolo importado, las de un export
de proveedor, el mapa de posiciones y las posiciones de proveedor sin
declaration map. Con eso, `pnpm check` pasa entero en macOS y el índice
TypeScript emite las mismas rutas que en un volumen sensible a mayúsculas.

El indexado Go nunca estuvo afectado.

## Punto de entrada del worker

`worker/dist/index.js` decide si se ejecuta como programa comparando
`process.argv[1]` con `import.meta.url`. Node resuelve el módulo principal por
realpath, así que una instalación bajo una ruta con symlink -cualquiera bajo
`/tmp` o `/var`- hacía que el worker terminara en silencio con código `0` en
vez de hablar el protocolo. El guard compara ahora ambas formas y el lanzador
del bundle calcula su raíz con `pwd -P`.
