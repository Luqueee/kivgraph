# LadybugDB fijado

**Estado:** seleccionado para la integración inicial

**Fecha de verificación:** 2026-08-05

Este documento fija la combinación de LadybugDB que puede entrar en Ladygraph. No se
usarán ramas, alias `latest` ni scripts que resuelvan una versión en tiempo de
build.

## Selección

| Componente | Versión/release | Evidencia |
| --- | --- | --- |
| LadybugDB core | `v0.13.1` | [release v0.13.1](https://github.com/LadybugDB/ladybug/releases/tag/v0.13.1), publicada el 2025-12-16 |
| Binding Go oficial | `v0.13.1` | [release v0.13.1](https://github.com/LadybugDB/go-ladybug/releases/tag/v0.13.1), publicada el 2025-12-18 |

El binding `v0.13.1` declara `go 1.20` y es compatible con el toolchain Go
1.24 del proyecto.

### Corrección del 2026-08-05: el core y el binding comparten versión

La selección anterior fijaba el core `v0.19.0` con el binding `v0.13.1`. **Ese
par no es compatible a nivel de ABI.** El core `v0.19.0` añade cuatro campos a
`lbug_system_config`:

```text
throw_on_wal_replay_failure
enable_checksums
enable_multi_writes
enable_default_hash_index
```

El binding devuelve esa estructura **por valor** desde
`lbug_default_system_config`, compilando contra su propia cabecera, más
pequeña. La primera llamada corrompe la pila:

```text
SIGSEGV: segmentation violation
signal arrived during cgo execution
github.com/LadybugDB/go-ladybug._Cfunc_lbug_default_system_config()
```

El core `v0.13.1` declara exactamente la misma estructura que la cabecera del
binding, y con ese par la suite `-tags ladybug` completa pasa. La regla que se
deriva: **el core y el binding se fijan siempre con la misma versión**; subir
uno obliga a subir el otro y a repetir esta comprobación.

La descarga verificada la realiza `scripts/fetch-ladybug.sh` y la CI la ejecuta
en el job `ladybug`.

## Checksums

### Módulo Go

El módulo `github.com/LadybugDB/go-ladybug@v0.13.1` se obtuvo desde el proxy
Go y tiene estos valores de `go mod download -json`:

```text
Sum:      h1:X11ch5sIsHHY2wqKx5phmvXi5aES9zMjRj3qkpUWTgU=
GoModSum: h1:f5RET9iUFgH+gLI6l/uJxAE4tXdYRdsDP9dN0Gr3M1M=
```

El commit y la referencia de origen devueltos por Go son, respectivamente,
`14a9f84900d0a8295c59419d91461c5430c692b5` y `refs/tags/v0.13.1`.

### Biblioteca nativa del core

Los SHA-256 siguientes son los `digest` publicados por la API de releases de
GitHub para el core `v0.13.1`. La URL contiene el tag inmutable y no `latest`.

| Plataforma | Asset | SHA-256 |
| --- | --- | --- |
| Linux amd64 | [`liblbug-linux-x86_64.tar.gz`](https://github.com/LadybugDB/ladybug/releases/download/v0.13.1/liblbug-linux-x86_64.tar.gz) | `ce6387bd46a5bcecbf7d59608694c540274b6bfb690ea8e5c92d9cb373d73439` |
| Linux arm64 | [`liblbug-linux-aarch64.tar.gz`](https://github.com/LadybugDB/ladybug/releases/download/v0.13.1/liblbug-linux-aarch64.tar.gz) | `f037a9e237cd0f9182b08fdabd73569b5afdcc3098e4d0b92687e678ce7736e4` |
| macOS universal | [`liblbug-osx-universal.tar.gz`](https://github.com/LadybugDB/ladybug/releases/download/v0.13.1/liblbug-osx-universal.tar.gz) | `4195a05e42671e5f8d036c5d035617ca05d25a1813b2ed8b46ab6cf9d8f0c426` |
| Windows amd64 | [`liblbug-windows-x86_64.zip`](https://github.com/LadybugDB/ladybug/releases/download/v0.13.1/liblbug-windows-x86_64.zip) | `68e0dffb16cf54158ca5780e0e62042e03873eb7287d10bdc2bf79f105998caa` |

El core `v0.13.1` publica un asset **universal** para macOS, no uno por
arquitectura como hacía `v0.19.0`.

La verificación de un asset debe ser equivalente a:

```bash
sha256sum liblbug-linux-x86_64.tar.gz
```

y debe compararse con esta tabla antes de extraerlo. Los artefactos nativos no
se suben al repositorio de Ladygraph: `scripts/fetch-ladybug.sh` los descarga desde
la URL versionada, comprueba el digest antes de extraer y deja la biblioteca
en `.tooling/ladybug/<versión>`, que está en `.gitignore`.

## Licencia

El core y el binding declaran **MIT License**. Las fuentes verificadas son
[`ladybug/LICENSE` en v0.13.1](https://raw.githubusercontent.com/LadybugDB/ladybug/v0.13.1/LICENSE)
y [`go-ladybug/LICENSE` en v0.13.1](https://raw.githubusercontent.com/LadybugDB/go-ladybug/v0.13.1/LICENSE).
Los avisos de redistribución quedan registrados en
[`THIRD_PARTY_NOTICES.md`](../../THIRD_PARTY_NOTICES.md).

## Biblioteca, CGO y empaquetado

El binding oficial es un wrapper CGO sobre la C API de LadybugDB (`lbug.h`).
La configuración `cgo_shared.go` del binding usa estas bibliotecas y flags:

| Plataforma | Biblioteca enlazada | Flags relevantes |
| --- | --- | --- |
| Linux amd64/arm64 | `liblbug.so` | `CGO_ENABLED=1`, `-L<lib-dir> -llbug -Wl,-rpath,<lib-dir>` |
| macOS amd64/arm64 | `liblbug.dylib` | `CGO_ENABLED=1`, `-lc++ -L<lib-dir> -llbug -Wl,-rpath,<lib-dir>` |
| Windows amd64 | `lbug_shared.dll`/import library | `CGO_ENABLED=1`, `-L<lib-dir> -llbug_shared` |

El asset de Windows arm64 queda registrado para trazabilidad del release, pero
no se declara soportado por el binding `v0.13.1`: su documentación exige MSYS2
UCRT64 y describe Windows x86_64. Se debe reabrir esta decisión si una versión
posterior del binding añade soporte Windows arm64.

El binding incluye un script `download_lbug.sh` que sigue
`releases/latest`; ese script **no forma parte del build reproducible de Ladygraph**.
El empaquetado posterior colocará el asset fijo de cada arquitectura en el
layout esperado por CGO y verificará su SHA-256 antes de compilar.

## Plataformas soportadas por Ladygraph en esta selección

- Linux amd64 (`linux/amd64`).
- Linux arm64 (`linux/arm64`).
- macOS amd64 (`darwin/amd64`).
- macOS arm64 (`darwin/arm64`).
- Windows amd64 (`windows/amd64`, con MSYS2 UCRT64 y CGO).

No se promete compilación con `CGO_ENABLED=0`, WebAssembly, BSD ni Windows
arm64. La matriz real queda sujeta a una prueba de compilación y apertura de
base en LUQUE-0202 y a la CI de distribución.
