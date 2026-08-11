# ADR 0036: El analizador de Rust viaja dentro del bundle

- **Estado:** aceptada
- **Fecha:** 2026-08-11
- **Revisa:** ADR 0033

## Contexto

El ADR 0033 decidió que `rust-analyzer` fuese un prerrequisito externo, como
el runtime Node del worker TypeScript, con dos argumentos: catorce megabytes
comprimidos por plataforma y una cadencia de releases diaria que este proyecto
no controla.

Medido, el argumento del tamaño es real pero acotado, y el de la cadencia se
resuelve fijando la versión igual que se fijan las gramáticas:

| Artefacto | Tamaño en el bundle |
| --- | --- |
| `bin/rust-analyzer` | `36 MB` (`darwin/arm64`), `40 MB` (`linux/amd64`) |
| `bin/ladygraph` | `24 MB` |
| `lib/liblbug.dylib` | `34 MB` |
| `worker/` | `31 MB` |
| Bundle completo | `127 MB` |

El argumento en contra que sí importaba era otro, y no estaba escrito: sin
fijar la versión, dos instalaciones del mismo release indexarían el mismo
repositorio con analizadores distintos y publicarían grafos distintos.

## Decisión

El bundle incluye `bin/rust-analyzer`, fijado en `tools/manifest.json` por
versión, URL y digest, descargado y verificado por
`scripts/fetch-rust-analyzer.sh`.

- El binario y sus dos licencias entran en el payload, en `SHA256SUMS` y en
  `manifest.json`, de modo que `ladygraph update` los valida como cualquier
  otro artefacto y `ladygraph version --json` publica la release del
  analizador.
- En ejecución, el analizador que viaja **junto al ejecutable** gana al del
  `PATH`. Una configuración que escribe una ruta explícita siempre manda.
  `doctor` dice cuál de los tres respondió.
- La misma regla se aplicó al worker TypeScript, que el bundle ya llevaba y
  el ejecutable sólo encontraba por `PATH`.

### Lo que el bundle sigue sin resolver

`rust-analyzer` no puede cargar un workspace sin `cargo`: sin él aborta con
`Failed to load the project`, verificado con un entorno sin toolchain. El
bundle no lleva un toolchain de Rust -son cientos de megabytes por plataforma
y su propia gestión de versiones-, así que **`cargo` sigue siendo requisito de
la máquina**. `doctor` lo comprueba por separado y falla nombrándolo.

## Alternativas descartadas

- **Seguir sin empaquetarlo.** Deja la reproducibilidad en manos del `PATH`
  de cada máquina, que es justo lo que un bundle existe para evitar.
- **Empaquetar el toolchain completo (rustc, cargo, std).** Resolvería el
  requisito entero, multiplicaría el tamaño del bundle por varias veces y
  pondría a Ladygraph a gestionar versiones de Rust. Si algún día se hace,
  será su propia decisión, no un efecto lateral de esta.
- **Descargar el analizador en el primer uso.** Convierte la primera
  indexación en una descarga silenciosa y rompe la instalación hermética.

## Consecuencias

- El bundle pasa de `91 MB` a `127 MB` en `darwin/arm64`.
- Un `index --full` de Rust funciona en una instalación recién descomprimida
  con sólo `cargo` presente.
- Actualizar el analizador es un cambio versionado en `tools/manifest.json`,
  visible en el diff y verificado por digest.

## Verificación cruzada de plataforma

El artefacto `linux/amd64` fijado se ejecutó en un contenedor `linux/amd64`:
descarga con digest correcto, `ELF 64-bit LSB pie executable, x86-64`, y una
indexación completa del fixture con `cargo 1.97.1` que dejó el workspace
intacto. El índice resultante se conserva en
`testdata/protocol/scip-v0.9/engine-linux-amd64.scip` y un test lo compara
contra el grabado en `darwin/arm64`: los símbolos, sus kinds, sus firmas y los
rangos de cada ocurrencia coinciden uno a uno. La identidad estable de un
símbolo no depende de la plataforma que lo indexó, que es lo que un bundle por
plataforma tiene que garantizar.

## Verificación en la plataforma que no se construye aquí

El bundle `linux/amd64` se construyó en un host Debian 13 x86_64: `123 MB`, 741
archivos con checksum correcto, `RUNPATH` `$ORIGIN/../lib` y arranque sin
variables de búsqueda. Su `index --full` del fixture Rust publicó el mismo
grafo que el bundle `darwin/arm64` -18 símbolos, 12 referencias, 2 no
resueltas- **con el mismo digest de HotSnapshot**,
`ad42e8d682f9ef5d23fe98acebaeea79f32345fb67b00dbbd5a4327017590a4f`.

En ese host también corrió `make test-ladybug`: 40 paquetes en verde, incluidos
`internal/storage/ladybug`, `internal/rebuild` e `internal/indexer`. La misma
suite pasa en `darwin/arm64` -39 paquetes, `internal/storage/ladybug` en
`40.4 s` y `internal/indexer` en `35.5 s`-, así que la capa nativa está
verificada en las dos plataformas de distribución.

## Riesgos

- **Deriva con el toolchain de la máquina.** El analizador empaquetado y el
  `rustc` del usuario pueden diferir en versión de lenguaje. Es el mismo
  riesgo que ya corre cualquier instalación de rust-analyzer, y `doctor`
  publica ambas versiones para que sea visible.
- **Tamaño.** Un bundle de 127 MB pesa lo que pesa; el instalador descarga un
  `tar.gz`, no el árbol descomprimido.
