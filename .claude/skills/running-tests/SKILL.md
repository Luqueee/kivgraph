---
name: running-tests
description: Cómo se ejecutan y se escriben los tests de Kivgraph - las tres suites, el tag `ladybug` que no se invoca a mano, qué se salta y por qué, y el smoke test del binario. Usar al correr tests, al añadirlos, ante un fallo de enlazado `library 'lbug' not found`, ante un `SKIP` inesperado, o antes de cerrar una tarea.
---

# Ejecutar los tests de Kivgraph

## La regla que más cuesta descubrir

**Nunca** `go test -tags ladybug`. Falla al enlazar:

```
ld: library 'lbug' not found
```

No falta la biblioteca: falta decirle al enlazador dónde está. `make test-ladybug`
exporta las tres variables `CGO_*` que apuntan a `.tooling/ladybug/<versión>` y es
el único modo soportado de ejecutar ese tag.

```bash
make test-ladybug                                   # suite completa con la capa nativa
make test-ladybug PKGS=./internal/storage/ladybug   # un paquete mientras se trabaja
```

`scripts/fetch-ladybug.sh` descarga y verifica la biblioteca la primera vez; el
target lo invoca solo. Los avisos `duplicate -rpath` y
`search path '.../lib/dynamic/darwin' not found` son inocuos: el binding declara
los suyos y el build fijado no puebla ese directorio.

## Las tres suites

| Comando | Cubre | Ejecutando | Con caché |
| --- | --- | --- | --- |
| `go test ./...` | 34 paquetes; toda la lógica que no toca LadybugDB nativo | `20 s` | `0.6 s` |
| `make test-ladybug` | 39 paquetes con `-tags ladybug` | `45 s` | `47 s` |
| `pnpm check` en `ts-worker/` y en `web/` | formato, lint, tipos y `vitest` (87 y 69 tests) | `3 s` cada uno | igual |

La primera invocación de `make test-ladybug` tarda unos `150 s`: compila cgo
desde cero. Después el coste es el de ejecutar, y la caché de test no lo baja
porque el enlazado con la biblioteca nativa se rehace igual.

`go test ./...` **no compila** los archivos con `//go:build ladybug`: son 22
archivos con **75 tests** que la suite corriente no ve, repartidos en
`internal/storage/ladybug`, `internal/indexer`, `internal/app` e
`internal/resilience`, más cinco paquetes de benchmark que solo existen bajo el
tag. Esos paquetes salen `ok` sin el tag -tienen además tests que no lo
necesitan-, así que un verde no dice nada sobre ellos. Un cambio en
almacenamiento, indexación, rebuild, snapshots o apagado **no está probado**
hasta que pasa `make test-ladybug`.

## Qué correr según lo que tocas

| Cambio | Comandos |
| --- | --- |
| Go, sin tocar almacenamiento | `gofmt -l <archivos>`, `go vet ./...`, `go test ./...` |
| Almacenamiento, indexer, rebuild, snapshots, upgrade | lo anterior **más** `make test-ladybug` |
| Camino Rust | además `go test ./internal/rustloader/ ./internal/indexer/ -run Rust` con `rust-analyzer` y `cargo` en el `PATH` |
| `ts-worker/` | `cd ts-worker && pnpm check && pnpm build` |
| `web/` | `cd web && pnpm check` |
| CLI, superficie MCP, comandos | añadir `go test -race ./cmd/kivgraph/...` (`6 s`), que es lo que corre CI |
| Distribución, bundle, manifest | `make build-darwin-arm64` y `(cd dist/kivgraph-darwin-arm64 && shasum -a 256 -c SHA256SUMS)` |

## Lo que se salta, y cómo dejar de saltarlo

Un `SKIP` aquí no es un test roto: es una dependencia del host que no está.

- `rust-analyzer is not installed` -- lo declaran `internal/rustloader/analyze_test.go`,
  `internal/rustloader/scip_run_test.go` e `internal/indexer/rust_unit_test.go`.
  Se resuelve con `rustup component add rust-analyzer`. Sin él, el camino Rust
  queda **sin probar** aunque la suite salga verde.
- `host filesystem cannot satisfy the default space policy` -- publicar una
  generación rechaza un disco con más del 85 % ocupado
  (`internal/testsupport.RequireSpaceOrSkip`). Es política de producto, no un
  fallo; libera disco si necesitas cubrir esa ruta.

Antes de afirmar que una suite pasó, comprueba que no se saltó lo que ibas a
probar: `go test ./internal/rustloader/ -v 2>&1 | grep SKIP`.

## Acotar un fallo

```bash
go test ./internal/indexer/ -run TestClassifyRustChange -v   # un test
go test ./internal/indexer/ -count=1                          # sin caché
go test ./internal/indexer/ -count=10 -run TestX              # ¿es flaky?
make test-ladybug PKGS=./internal/rebuild                     # con la capa nativa
```

La caché de test es agresiva y `ok (cached)` no ejecuta nada: si cambiaste un
fixture, un testdata o una variable de entorno, usa `-count=1`.

## Escribir tests aquí

- Un test defiende un **comportamiento observable** y falla ante una regresión
  plausible. Los negativos, las invariantes y la comparación contra una
  reconstrucción limpia son parte del contrato, no un extra.
- **`internal/testsupport.TempDir(t)`, nunca `t.TempDir()`** cuando el path
  alimenta la capa de workspace: en macOS `/var` y `/tmp` son symlinks, la
  validación rechaza rutas con ancestro symlink, y el test acabaría ejercitando
  el rechazo en vez de lo que pretende.
- **No modificar repositorios indexados** ni las entradas de los benchmarks. Los
  fixtures viven en `testdata/`; copia, no edites en sitio. El fixture Rust es
  `testdata/rust/workspace` y las pasadas de indexación deben dejarlo intacto -sin
  `target/`, sin `Cargo.lock`-, cosa que conviene comprobar en el propio test.
- Un fixture con toolchain externo -Cargo, `rust-analyzer`- exige `HOME` real: un
  `HOME` temporal rompe los shims de rustup. Si aíslas `HOME`, pasa también
  `RUSTUP_HOME` y `CARGO_HOME` reales.

## Smoke test del binario

Los tests no cubren la instalación, y **el binario de `make build` no sirve para
esto**: sin el tag `ladybug` no hay capa nativa, y la pasada llega hasta el final
para morir al publicar.

```
stage.staging: FAIL (ladybug load canonical: LadybugDB native support is unavailable)
index.full: FAIL
```

Construye uno con la capa nativa -las mismas flags que `make test-ladybug`- o usa
el del bundle, `dist/kivgraph-darwin-arm64/bin/kivgraph`:

```bash
LIB="$(scripts/fetch-ladybug.sh)"
CGO_ENABLED=1 CGO_CFLAGS="-I$LIB" CGO_LDFLAGS="-L$LIB -llbug -Wl,-rpath,$LIB" \
  go build -tags ladybug -o /tmp/kivgraph-native ./cmd/kivgraph
```

El flujo completo, con un `HOME` desechable y sin tocar ningún repositorio real:

```bash
export RUSTUP_HOME="$HOME/.rustup" CARGO_HOME="$HOME/.cargo"   # el HOME de verdad
home=$(cd "$(mktemp -d)" && pwd -P); repo="$home/fixture"
cp -r testdata/rust/workspace "$repo"
(cd "$repo" && git init -q && git add -A && git -c user.email=a@b -c user.name=t commit -qm fixture)
env HOME="$home" /tmp/kivgraph-native init --repository "fixture=$repo" --languages rust
env HOME="$home" /tmp/kivgraph-native doctor
env HOME="$home" /tmp/kivgraph-native index --full
```

`RUSTUP_HOME` y `CARGO_HOME` se exportan **antes** y apuntan al `HOME` real: el
`env HOME=` solo aísla la configuración de Kivgraph. Sin eso, el shim de rustup
no encuentra su toolchain y `doctor` declara `toolchain.cargo: FAIL` por un
artefacto de la prueba, no por el estado de la máquina.

Un `index --full` sano publica `stage.integrity: PASS (0 invariant violations)`,
`stage.golden probes: PASS` y una generación. `doctor` nombra cada toolchain por
separado: `toolchain.rust` puede ir en PASS con el analizador empaquetado
mientras `toolchain.cargo` falla, y entonces la indexación aísla el workspace
(`not_loaded=1`) en vez de abortar.

## Benchmarks: no son tests

Viven en `benchmarks/<nombre>/` y se ejecutan a mano:

```bash
go run ./benchmarks/rust-semantic
```

Los que miden la capa nativa llevan `//go:build ladybug` y sin el tag ni siquiera
se compilan -`build constraints exclude all Go files`-. Necesitan las mismas
flags que `make test-ladybug`:

```bash
LIB="$(scripts/fetch-ladybug.sh)"
CGO_ENABLED=1 CGO_CFLAGS="-I$LIB" CGO_LDFLAGS="-L$LIB -llbug -Wl,-rpath,$LIB" \
  go run -tags ladybug ./benchmarks/ladybug-bulk
```

Escriben `results.json` y `report.md` versionados y emiten un gate en `stdout`
(`GO_SEMANTIC_PASS`, `RUST_SEMANTIC_PASS_WITH_LIMITS`, `HOT_SNAPSHOT_PASS`...).
Un gate `_WITH_LIMITS` debe enumerar sus limitaciones; nunca se presenta como
PASS limpio.

**Sobrescriben sus artefactos versionados.** Ejecutar un benchmark por curiosidad
deja un diff en `benchmarks/`; o se revisa y se justifica, o se revierte. No se
corren para "comprobar que siguen funcionando".

## Antes de cerrar una tarea

```bash
gofmt -l <archivos-go-modificados>
go vet ./...
go test ./...
make build
make test-ladybug     # si tocaste la capa nativa
git diff --check
```

Y en `ts-worker/`, `pnpm check && pnpm build`.

## Lo que esta máquina no puede probar

El bundle `linux/amd64` no se construye aquí: cgo enlaza la biblioteca nativa y
el proyecto no cross-compila. Lo verifica el job Linux de CI, o un host Linux
nativo por SSH -copia el árbol con `git archive HEAD` más los ficheros sin
versionar; `tar` de macOS con `--exclude` se deja directorios por el camino-.
Un contenedor emulado sirve para probar scripts y descargas, no para firmar el
artefacto que se publica.
