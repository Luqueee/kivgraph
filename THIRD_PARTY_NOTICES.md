# Avisos y licencias de terceros

Este archivo es el inventario de avisos, copyright y licencias de las
dependencias que se distribuyan junto con Kivgraph.

## Dependencias registradas

### LadybugDB core y binding Go

- `LadybugDB/ladybug` `v0.13.1`.
- `LadybugDB/go-ladybug` `v0.13.1`, commit
  `14a9f84900d0a8295c59419d91461c5430c692b5`.
- Licencia: MIT License.
- Los textos completos están disponibles en los tags fijados:
  [core](https://raw.githubusercontent.com/LadybugDB/ladybug/v0.13.1/LICENSE)
  y
  [binding](https://raw.githubusercontent.com/LadybugDB/go-ladybug/v0.13.1/LICENSE).
- Los SHA-256 de los assets nativos y sus URLs versionadas están en
  [`docs/dependencies/ladybugdb.md`](docs/dependencies/ladybugdb.md).


### Go MCP SDK

- `github.com/modelcontextprotocol/go-sdk` `v0.8.0`.
- Licencia: MIT License.
- El texto se incluye en el bundle bajo
  `licenses/third-party/`.

### fsnotify

- `github.com/fsnotify/fsnotify` `v1.9.0`.
- Licencia: BSD-3-Clause.
- El texto se incluye en el bundle bajo
  `licenses/third-party/`.

### BLAKE3

- `github.com/zeebo/blake3` `v0.2.4`.
- Licencia: CC0 1.0.
- El texto se incluye en el bundle bajo
  `licenses/third-party/`.

### YAML

- `gopkg.in/yaml.v3` `v3.0.1`.
- Licencias: MIT y Apache-2.0, según el archivo fuente.
- El texto se incluye en el bundle bajo
  `licenses/third-party/`.

### Tree-sitter y grammars

- `github.com/tree-sitter/go-tree-sitter` `v0.25.0`.
- `github.com/tree-sitter/tree-sitter-go` `v0.25.0`.
- `github.com/tree-sitter/tree-sitter-javascript` `v0.25.0`.
- `github.com/tree-sitter/tree-sitter-rust` `v0.23.2`.
- `github.com/tree-sitter/tree-sitter-typescript` `v0.23.2`.
- Licencia: MIT License.
- El inventario de commits, URLs, SHA-256 y licencias de las grammars está
  en [`grammars/manifest.json`](grammars/manifest.json). Los textos de los
  módulos se incluyen en el bundle bajo `licenses/third-party/`.

### rust-analyzer

- `rust-analyzer` `2026-08-10.1` (`0.3.3008-standalone`), commit
  `f938641be53c2e4bacd7dc46bddb74825a3e9d28`, distribuido como
  `bin/rust-analyzer` porque es el motor que lee Rust.
- Licencia: MIT o Apache License 2.0, a elección.
- La versión, las URLs y los SHA-256 del binario y de sus licencias están en
  [`tools/manifest.json`](tools/manifest.json); los textos se distribuyen en
  `licenses/third-party/rust-analyzer/`.

### OpenTelemetry

- `go.opentelemetry.io/otel`, `metric` y `sdk/metric` `v1.40.0`.
- Licencia: Apache License 2.0.
- El texto se incluye en el bundle bajo
  `licenses/third-party/`.

### Módulos `golang.org/x`

- `golang.org/x/mod` `v0.31.0`.
- `golang.org/x/tools`
  `v0.40.1-0.20260108161641-ca281cf95054`.
- Licencia: BSD-3-Clause.
- Los textos disponibles se incluyen en el bundle bajo
  `licenses/third-party/`.

El script de distribución copia los archivos de licencia presentes en el grafo
de módulos Go bajo `licenses/third-party/`. El aviso del core nativo de
LadybugDB se conserva mediante el enlace oficial fijado arriba y la
procedencia local en `docs/dependencies/ladybugdb.md`.

### TypeScript runtime

- `typescript` `7.0.2`, incluido porque el worker consume la API de
  TypeScript en runtime.
- Licencia: Apache License 2.0, con avisos adicionales de Microsoft.
- Los textos `LICENSE` y `NOTICE.txt` se distribuyen dentro de
  `worker/node_modules/typescript/` y quedan incluidos en los hashes del
  manifest.