# LadybugDB fijado

**Estado:** seleccionado para la integración inicial

**Fecha de verificación:** 2026-08-04

Este documento fija la combinación de LadybugDB que puede entrar en Luque. No se
usarán ramas, alias `latest` ni scripts que resuelvan una versión en tiempo de
build.

## Selección

| Componente | Versión/release | Commit exacto | Evidencia |
| --- | --- | --- | --- |
| LadybugDB core | `v0.19.0` | `c934f673b6b1c5b680bdae3295cbd909b5855cef` | [release v0.19.0](https://github.com/LadybugDB/ladybug/releases/tag/v0.19.0), publicada el 2026-07-30 |
| Binding Go oficial | `v0.13.1` | `14a9f84900d0a8295c59419d91461c5430c692b5` | [release v0.13.1](https://github.com/LadybugDB/go-ladybug/releases/tag/v0.13.1), publicada el 2025-12-18 |

El binding `v0.13.1` declara `go 1.20` y es compatible con el toolchain Go
1.24 del proyecto. El `master` observado el 2026-08-04 estaba en el commit
`42bbf464c74c59088f59691dbd9f204015be3463` y declara `go 1.26`; no se fija
esa rama porque introduciría un requisito de toolchain no disponible en Luque.

La compatibilidad completa entre el binding y la biblioteca nativa se valida
con el wrapper y sus pruebas en LUQUE-0202. Esta tarea fija las entradas; no
presenta todavía una garantía de ABI.

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
GitHub para `v0.19.0`. La URL contiene el tag inmutable y no `latest`.

| Plataforma | Asset | SHA-256 |
| --- | --- | --- |
| Linux amd64 | [`liblbug-linux-x86_64.tar.gz`](https://github.com/LadybugDB/ladybug/releases/download/v0.19.0/liblbug-linux-x86_64.tar.gz) | `907ff68f1df703704ed61b618dd9502ba589c5f8983a96de6be2282c6a1c11db` |
| Linux arm64 | [`liblbug-linux-aarch64.tar.gz`](https://github.com/LadybugDB/ladybug/releases/download/v0.19.0/liblbug-linux-aarch64.tar.gz) | `b2b4aeb3cb4405ea773b42accecba4c8aa7f6179c198898b962a98058ed94d2a` |
| macOS arm64 | [`liblbug-osx-arm64.tar.gz`](https://github.com/LadybugDB/ladybug/releases/download/v0.19.0/liblbug-osx-arm64.tar.gz) | `625e52ab05051b938e8841798ce57dcee5b6625e9537c6be3f3425e76974e734` |
| macOS amd64 | [`liblbug-osx-x86_64.tar.gz`](https://github.com/LadybugDB/ladybug/releases/download/v0.19.0/liblbug-osx-x86_64.tar.gz) | `a9cb505f580a70ce37a2a1f4996650bd212212939e6a43f874e411fb94fc3334` |
| Windows amd64 | [`liblbug-windows-x86_64.zip`](https://github.com/LadybugDB/ladybug/releases/download/v0.19.0/liblbug-windows-x86_64.zip) | `8ecb60bb5b3bec7ff263cd23a622c0892d7f605c42af19b4e06d75b1604bf612` |
| Windows arm64 | [`liblbug-windows-arm64.zip`](https://github.com/LadybugDB/ladybug/releases/download/v0.19.0/liblbug-windows-arm64.zip) | `eaefdeaa1686a4697fe3053f6330774387ffb0b5d6b9b87a26e47be827791916` |

La verificación de un asset debe ser equivalente a:

```bash
sha256sum liblbug-linux-x86_64.tar.gz
```

y debe compararse con esta tabla antes de extraerlo. Los artefactos nativos no
se subirán al repositorio de Luque; se descargarán en un paso de build que use
la URL versionada y compruebe el digest esperado.

## Licencia

El core y el binding declaran **MIT License**. Las fuentes verificadas son
[`ladybug/LICENSE` en v0.19.0](https://raw.githubusercontent.com/LadybugDB/ladybug/v0.19.0/LICENSE)
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
`releases/latest`; ese script **no forma parte del build reproducible de Luque**.
El empaquetado posterior colocará el asset fijo de cada arquitectura en el
layout esperado por CGO y verificará su SHA-256 antes de compilar.

## Plataformas soportadas por Luque en esta selección

- Linux amd64 (`linux/amd64`).
- Linux arm64 (`linux/arm64`).
- macOS amd64 (`darwin/amd64`).
- macOS arm64 (`darwin/arm64`).
- Windows amd64 (`windows/amd64`, con MSYS2 UCRT64 y CGO).

No se promete compilación con `CGO_ENABLED=0`, WebAssembly, BSD ni Windows
arm64. La matriz real queda sujeta a una prueba de compilación y apertura de
base en LUQUE-0202 y a la CI de distribución.
