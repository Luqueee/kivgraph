# LADYGRAPH

## MCP ultrarrápido de inteligencia de código cross-repository para TypeScript y Go

---

# 1. Resumen ejecutivo

**Ladygraph** será un servidor MCP autónomo, local y de solo lectura especializado en construir y consultar un grafo semántico entre múltiples repositorios.

Su objetivo será responder con precisión y muy baja latencia preguntas como:

* ¿Dónde se define exactamente este símbolo?
* ¿Qué archivos, paquetes y repositorios lo consumen?
* ¿Qué funciones llaman directamente a esta función?
* ¿Dónde se pasa una función como callback?
* ¿Qué paquete proporciona un determinado import?
* ¿Qué símbolos resultarían afectados por un cambio?
* ¿Qué referencias no ha podido resolver el indexador?
* ¿Qué camino de dependencias conecta dos símbolos?
* ¿Qué repositorios dependen directa o transitivamente de un paquete?

Ladygraph no será:

* un editor;
* un agente;
* un sistema RAG;
* un buscador vectorial;
* un sustituto del compilador;
* un servidor LSP;
* una herramienta vinculada a una estructura de repositorios concreta.

La arquitectura se dividirá en dos planos:

```text
PLANO DE INDEXACIÓN

Repositorios
    │
    ├── TypeScript Compiler API
    ├── go/packages + go/types
    └── Tree-sitter
          │
          ▼
Resolver semántico cross-repository
          │
          ▼
LadybugDB
Grafo persistente canónico
          │
          ▼
Construcción de HotSnapshot
          │
          ▼
Publicación atómica


PLANO DE CONSULTA

Cliente MCP
    │
    ▼
Ladygraph MCP
    │
    ▼
HotSnapshot inmutable en RAM
    │
    ▼
Respuesta estructurada
```

La regla principal será:

> El análisis puede ser costoso. Las consultas MCP no.

---

# 2. Decisiones cerradas

```yaml
project:
  name: Ladygraph
  type: autonomous_mcp_server
  primary_language: Go
  initial_languages:
    - TypeScript
    - JavaScript
    - Go

mcp:
  initial_transport: stdio
  mode: read_only
  sdk: official_go_sdk

graph:
  canonical_store:
    engine: LadybugDB
    mode: embedded
    persistence: on_disk

  query_fast_path:
    engine: custom_hot_snapshot
    location: memory
    mutability: immutable
    publication: atomic_swap

typescript:
  semantic_engine: TypeScript Compiler API
  execution: persistent_node_worker

go:
  loader: go/packages
  semantic_engine: go/types
  syntax_engine: go/ast
  stable_identity: objectpath

syntax_acceleration:
  engine: Tree-sitter
  authority: false

incremental_updates:
  watcher: fsnotify
  verification: content_hash
  reconciliation: periodic_scan

worker_protocol:
  initial: length_prefixed_json
  future_candidate: protobuf

database_servers:
  external: false

sqlite:
  enabled: false

vectors:
  enabled: false

llm:
  required: false
```

LadybugDB es una base de grafos embebida, por lo que se ejecuta dentro del proceso de la aplicación. Ofrece API oficial para Go, persistencia on-disk, WAL y transacciones ACID. El diseño de Ladygraph respetará su modelo de concurrencia: una única instancia `Database` será propietaria del fichero, y todas las conexiones procederán de esa misma instancia.

---

# 3. Principios no negociables

## 3.1 Ningún compilador en el fast path

Una llamada MCP nunca debe provocar:

```text
packages.Load
creación de TypeScript Program
creación de TypeChecker
resolución de módulos
lectura global del filesystem
consulta al worker TypeScript
consulta a un LSP
reconstrucción del grafo
análisis Tree-sitter
ejecución de Cypher compleja
```

La ruta de una consulta habitual será:

```text
petición
→ validación
→ snapshot.Load()
→ lookup
→ recorrido acotado
→ paginación
→ serialización
→ respuesta
```

## 3.2 Exactitud antes que cobertura aparente

Ladygraph nunca creará una arista exacta únicamente porque:

* dos símbolos tengan el mismo nombre;
* exista un único candidato global;
* dos archivos tengan nombres similares;
* un import y un export parezcan coincidir;
* un resultado textual contenga el identificador;
* un símbolo esté próximo en el árbol sintáctico.

Cuando no exista evidencia suficiente, se registrará:

```text
UNRESOLVED
```

o:

```text
CANDIDATE
```

Nunca:

```text
EXACT
```

## 3.3 Las omisiones son aceptables; los enlaces falsos no

```text
referencia omitida:
  degradación conocida y medible

arista exacta hacia símbolo incorrecto:
  fallo de integridad
```

Una sola arista falsa en un fixture negativo bloquea producción.

## 3.4 Toda respuesta debe declarar sus límites

Cada respuesta relevante incluirá:

```text
snapshot_id
snapshot_age
total
returned
truncated
next_cursor
exact_results
unresolved_related
coverage
```

## 3.5 Los repositorios analizados son de solo lectura

Ladygraph no escribirá:

```text
archivos de configuración
índices
cachés
go.work
tsbuildinfo
metadatos
```

dentro de los repositorios.

Todo su estado vivirá en:

```text
~/.config/ladygraph/
~/.local/state/ladygraph/
~/.cache/ladygraph/
```

---

# 4. Objetivos de rendimiento

## 4.1 Consultas MCP internas

Objetivos del backend, sin contar el overhead del cliente:

```text
graph_status:
  p50 ≤ 0,25 ms
  p95 ≤ 1 ms
  p99 ≤ 2 ms

list_repositories:
  p95 ≤ 1 ms

get_symbol:
  p50 ≤ 0,25 ms
  p95 ≤ 1 ms
  p99 ≤ 2 ms

find_symbol exacto:
  p50 ≤ 0,5 ms
  p95 ≤ 2 ms
  p99 ≤ 5 ms

find_references, hasta 100:
  p50 ≤ 1 ms
  p95 ≤ 5 ms
  p99 ≤ 10 ms

find_cross_repo_consumers:
  p95 ≤ 5 ms
  p99 ≤ 15 ms

trace_dependencies depth 3:
  p95 ≤ 12 ms
  p99 ≤ 25 ms

get_blast_radius depth 3:
  p95 ≤ 20 ms
  p99 ≤ 40 ms

get_blast_radius depth 5:
  p95 ≤ 50 ms
  p99 ≤ 100 ms
```

## 4.2 Rendimiento incremental

```text
cambio en un archivo sin alterar exports:
  visible p95 ≤ 750 ms

cambio en exports o imports:
  visible p95 ≤ 2 s

cambio en package.json, tsconfig o go.mod:
  visible p95 ≤ 5 s

actualización de un repositorio completo:
  objetivo ≤ 15 s

reconstrucción inicial:
  objetivo ≤ 60 s
  límite provisional ≤ 120 s
```

## 4.3 Memoria

```text
servidor en reposo:
  objetivo ≤ 500 MiB

HotSnapshot:
  objetivo inicial ≤ 400 MiB

worker TypeScript:
  objetivo ≤ 1,5 GiB
  límite provisional ≤ 3 GiB

indexación completa agregada:
  límite provisional ≤ 4 GiB
```

## 4.4 Almacenamiento

```text
LadybugDB:
  objetivo ≤ 2 GiB para corpus inicial

snapshots binarios:
  máximo 3 snapshots retenidos por defecto

logs:
  rotación y límite configurables
```

---

# 5. Arquitectura de procesos

## 5.1 Proceso principal

```text
ladygraph
```

Implementado en Go.

Responsabilidades:

* servidor MCP;
* propiedad exclusiva de LadybugDB;
* analizador Go;
* coordinador de indexación;
* resolución cross-repository;
* construcción del HotSnapshot;
* watcher;
* CLI;
* métricas;
* diagnóstico;
* recuperación;
* supervisión del worker TypeScript.

## 5.2 Worker TypeScript

```text
ladygraph-ts-worker
```

Proceso Node.js persistente.

Responsabilidades:

* cargar la versión correcta de TypeScript;
* mantener proyectos y Language Services;
* resolver módulos;
* obtener símbolos y aliases;
* seguir exports y reexports;
* encontrar referencias;
* clasificar llamadas y callbacks;
* mapear declaraciones `.d.ts` hacia fuente;
* devolver hechos normalizados.

El worker no podrá:

* abrir LadybugDB;
* escribir el grafo;
* publicar snapshots;
* responder directamente al MCP;
* modificar repositorios.

## 5.3 Tree-sitter

Tree-sitter se utilizará como parser sintáctico rápido e incremental. Puede actualizar árboles al cambiar el contenido de un archivo y conservar resultados útiles incluso cuando el código contiene errores sintácticos.

Responsabilidades:

* inventario rápido;
* clasificación de archivos;
* extracción de imports y exports candidatos;
* detección de regiones modificadas;
* identificación de declaraciones candidatas;
* detección de call sites;
* cálculo del alcance probable de una invalidación.

No será autoridad semántica.

```text
Tree-sitter:
  encuentra

TypeScript Checker / go/types:
  certifican
```

---

# 6. Estructura del repositorio

```text
ladygraph/
├── cmd/
│   └── ladygraph/
│       └── main.go
│
├── internal/
│   ├── app/
│   │   ├── app.go
│   │   ├── lifecycle.go
│   │   └── shutdown.go
│   │
│   ├── config/
│   │   ├── config.go
│   │   ├── validation.go
│   │   └── defaults.go
│   │
│   ├── workspace/
│   │   ├── registry.go
│   │   ├── repository.go
│   │   ├── discovery.go
│   │   ├── manifests.go
│   │   └── git.go
│   │
│   ├── model/
│   │   ├── ids.go
│   │   ├── symbol.go
│   │   ├── edge.go
│   │   ├── evidence.go
│   │   ├── unresolved.go
│   │   └── enums.go
│   │
│   ├── storage/
│   │   └── ladybug/
│   │       ├── database.go
│   │       ├── connection.go
│   │       ├── schema.go
│   │       ├── migrations.go
│   │       ├── transaction.go
│   │       ├── bulk.go
│   │       ├── delta.go
│   │       ├── queries.go
│   │       ├── integrity.go
│   │       └── backup.go
│   │
│   ├── graph/
│   │   ├── build.go
│   │   ├── resolver.go
│   │   ├── package_resolver.go
│   │   ├── symbol_resolver.go
│   │   ├── integrity.go
│   │   └── delta.go
│   │
│   ├── snapshot/
│   │   ├── snapshot.go
│   │   ├── builder.go
│   │   ├── interner.go
│   │   ├── indexes.go
│   │   ├── csr.go
│   │   ├── traversal.go
│   │   ├── cursor.go
│   │   ├── serialization.go
│   │   └── publication.go
│   │
│   ├── indexer/
│   │   ├── coordinator.go
│   │   ├── full.go
│   │   ├── incremental.go
│   │   ├── invalidation.go
│   │   ├── batch.go
│   │   └── result.go
│   │
│   ├── languages/
│   │   ├── typescript/
│   │   │   ├── client.go
│   │   │   ├── protocol.go
│   │   │   ├── normalize.go
│   │   │   └── project.go
│   │   │
│   │   ├── golang/
│   │   │   ├── loader.go
│   │   │   ├── definitions.go
│   │   │   ├── references.go
│   │   │   ├── calls.go
│   │   │   ├── callbacks.go
│   │   │   ├── methods.go
│   │   │   └── identity.go
│   │   │
│   │   └── treesitter/
│   │       ├── parser.go
│   │       ├── inventory.go
│   │       ├── diff.go
│   │       └── queries.go
│   │
│   ├── watcher/
│   │   ├── watcher.go
│   │   ├── debounce.go
│   │   ├── reconcile.go
│   │   └── ignore.go
│   │
│   ├── mcp/
│   │   ├── server.go
│   │   ├── errors.go
│   │   ├── response.go
│   │   └── tools/
│   │       ├── list_repositories.go
│   │       ├── find_symbol.go
│   │       ├── get_symbol.go
│   │       ├── find_references.go
│   │       ├── find_consumers.go
│   │       ├── trace_dependencies.go
│   │       ├── blast_radius.go
│   │       ├── unresolved.go
│   │       └── status.go
│   │
│   ├── doctor/
│   ├── benchmark/
│   ├── telemetry/
│   └── version/
│
├── ts-worker/
│   ├── src/
│   │   ├── main.ts
│   │   ├── protocol.ts
│   │   ├── workspace.ts
│   │   ├── projects.ts
│   │   ├── module-resolution.ts
│   │   ├── symbols.ts
│   │   ├── references.ts
│   │   ├── exports.ts
│   │   ├── declarations.ts
│   │   ├── callbacks.ts
│   │   └── diagnostics.ts
│   ├── package.json
│   ├── tsconfig.json
│   └── pnpm-lock.yaml
│
├── schemas/
│   ├── ladybug/
│   └── protocol/
│
├── testdata/
│   ├── synthetic/
│   ├── typescript/
│   ├── go/
│   ├── cross-repo/
│   ├── negative/
│   └── corruption/
│
├── benchmarks/
├── docs/
├── scripts/
├── go.mod
├── go.sum
├── Makefile
├── LICENSE
└── README.md
```

---

# 7. Configuración

## 7.1 Archivo principal

```text
~/.config/ladygraph/config.yaml
```

Ejemplo:

```yaml
version: 1

workspace:
  repositories_file: ~/.config/ladygraph/repositories.yaml

storage:
  database_path: ~/.local/state/ladygraph/graph.lbdb
  snapshots_path: ~/.local/state/ladygraph/snapshots
  backups_path: ~/.local/state/ladygraph/backups
  retain_snapshots: 3

mcp:
  transport: stdio
  default_limit: 50
  maximum_limit: 500
  maximum_depth: 5
  maximum_visited_nodes: 25000

indexing:
  generated_files: include
  unresolved_references: retain
  syntax_acceleration: true
  full_rebuild_on_schema_change: true

watcher:
  enabled: true
  debounce_ms: 150
  maximum_batch_ms: 500
  reconciliation_interval: 10m

typescript:
  worker_command: ladygraph-ts-worker
  maximum_workers: 3
  project_idle_timeout: 30m

go:
  synthetic_work_file: ~/.local/state/ladygraph/go.work
  include_tests: false

telemetry:
  metrics: true
  traces: false

logging:
  format: json
  level: info
```

## 7.2 Registro de repositorios

```text
~/.config/ladygraph/repositories.yaml
```

```yaml
version: 1

repositories:
  - name: shared-library
    path: /srv/code/shared-library
    languages:
      - typescript

  - name: service-a
    path: /srv/code/service-a
    languages:
      - typescript
      - go

  - name: service-b
    path: /srv/code/service-b
    languages:
      - go
```

Ladygraph no debe depender de convenciones de nombres de carpetas.

---

# 8. Modelo semántico

## 8.1 Nodos

### Repository

```text
id
stable_key
name
root_path
commit_sha
branch
dirty
indexed_at
```

### Package

```text
id
stable_key
repository_id
language
package_name
package_version
root_path
manifest_path
```

### File

```text
id
stable_key
repository_id
relative_path
language
content_hash
size
generated
parse_status
```

### Symbol

```text
id
stable_key
repository_id
package_id
file_id
language
name
qualified_name
kind
visibility
signature_hash
start_byte
end_byte
start_line
start_column
end_line
end_column
```

### Evidence

```text
id
repository_id
file_id
start_byte
end_byte
start_line
start_column
snippet_hash
metadata
```

No será obligatorio guardar el texto completo.

### UnresolvedReference

```text
id
repository_id
file_id
source_symbol_id
language
requested_package
requested_symbol
reason
evidence_id
```

### Snapshot

```text
id
created_at
schema_version
resolver_version
repositories
files
symbols
edges
valid
checksum
```

## 8.2 Aristas

```text
CONTAINS_PACKAGE
CONTAINS_FILE
DEFINES
PACKAGE_DEPENDS_ON
MODULE_DEPENDS_ON

IMPORTS_SYMBOL
EXPORTS
REEXPORTS

REFERENCES
CALLS_DIRECT
PASSES_AS_CALLBACK
ASSIGNS_FUNCTION
RETURNS_FUNCTION

TYPE_USES
IMPLEMENTS
EXTENDS
EMBEDS
OVERRIDES
```

Futuras, fuera del MVP:

```text
HTTP_PROVIDES
HTTP_CONSUMES
GRPC_PROVIDES
GRPC_CONSUMES
PUBLISHES_TOPIC
SUBSCRIBES_TOPIC
READS_DATASET
WRITES_DATASET
```

## 8.3 Propiedades de las aristas

```text
kind
confidence
provenance
evidence_id
source_snapshot
resolver_version
```

## 8.4 Niveles de confianza

```text
EXACT_TYPECHECKED
EXACT_DECLARATION_MAPPED
EXACT_PACKAGE_MAPPED
STRUCTURAL_CERTAIN
CANDIDATE
UNRESOLVED
```

Solo las cuatro primeras podrán participar en resultados exactos.

`CANDIDATE` y `UNRESOLVED` se expondrán por separado.

## 8.5 Procedencia

```text
TYPESCRIPT_CHECKER
TYPESCRIPT_MODULE_RESOLUTION
TYPESCRIPT_DECLARATION_MAP
TYPESCRIPT_PROJECT_REFERENCE

GO_TYPES_DEF
GO_TYPES_USE
GO_TYPES_SELECTION
GO_AST_CALL
GO_AST_CALLBACK
GO_OBJECT_PATH

TREE_SITTER_SYNTAX
PACKAGE_MANIFEST
```

Una relación producida únicamente por Tree-sitter no será exacta.

---

# 9. Identidad estable

## 9.1 IDs internos

```go
type RepositoryID uint32
type PackageID uint32
type FileID uint32
type SymbolID uint32
type EdgeID uint64
```

Los IDs internos pueden cambiar entre snapshots.

## 9.2 Stable key

Las respuestas MCP utilizarán una stable key persistente.

### TypeScript

```text
language
repository identity
package name
source module
qualified container
exported/local symbol name
symbol kind
signature discriminator
```

### Go

```text
language
repository identity
module path
package import path
object path
symbol kind
```

`objectpath` define una forma de nombrar objetos de `go/types` respecto a su paquete y evita depender de punteros válidos únicamente dentro de un proceso.

## 9.3 Hash

```text
stable_key = BLAKE3(canonical_identity)
```

Se conservará también la identidad canónica legible para auditoría.

No se utilizará la línea como componente principal, porque mover una declaración no debe cambiar necesariamente su identidad.

---

# 10. LadybugDB

## 10.1 Propiedad

El proceso principal mantendrá:

```text
1 Database READ_WRITE
N conexiones derivadas de esa Database
```

LadybugDB permite transacciones concurrentes mediante conexiones de una misma instancia, pero solo admite una transacción de escritura activa en cada momento.

Consecuencia:

```text
un único writer lógico
batches grandes
transacciones breves
consultas MCP sin depender de la base
```

## 10.2 Esquema conceptual

```text
Repository
Package
File
Symbol
Evidence
UnresolvedReference
Snapshot
```

Relaciones:

```text
Repository-CONTAINS_PACKAGE->Package
Repository-CONTAINS_FILE->File
Package-CONTAINS_FILE->File
File-DEFINES->Symbol

Package-PACKAGE_DEPENDS_ON->Package

Symbol-IMPORTS_SYMBOL->Symbol
Symbol-REFERENCES->Symbol
Symbol-CALLS_DIRECT->Symbol
Symbol-PASSES_AS_CALLBACK->Symbol
Symbol-TYPE_USES->Symbol
Symbol-IMPLEMENTS->Symbol
```

El DDL exacto se fijará después del benchmark de LadybugDB, porque el layout físico y las multiplicidades de las tablas de relaciones deben medirse con el corpus real.

## 10.3 Full rebuild

```text
repositorios
→ facts normalizados
→ archivos CSV/Parquet de staging
→ generations/<id>.tmp/graph.lbdb
→ carga bulk
→ cierre y fsync
→ reapertura
→ doctor, integridad y golden probes
→ HotSnapshot validado
→ publicación atómica de CURRENT
```

La base activa no se sobrescribirá. Una generación solo pasa a ser visible
cuando la base y el snapshot candidatos están cerrados, sincronizados,
reabiertos y validados.

```text
state/
├── generations/
│   ├── 000041/
│   └── 000042.tmp/
├── CURRENT
└── space-reserve
```

`CURRENT` contiene únicamente el identificador de la generación activa. Tras
sincronizar la candidata, la publicación renombra
`generations/<id>.tmp/` a `generations/<id>/` y sincroniza `generations/`.
Después escribe y sincroniza `CURRENT.next`, lo renombra atómicamente sobre
`CURRENT` y sincroniza el directorio padre. Se conserva al menos la generación
anterior. `space-reserve` se preasigna para registrar y limpiar después de
`ENOSPC`, pero no sustituye la publicación generacional.

Antes de crear una candidata debe cumplirse:

```text
espacio requerido =
  2 × tamaño de la base activa
  + tamaño estimado del snapshot
  + 1 GiB de margen

umbral efectivo =
  max(espacio requerido, 15 % del filesystem)
```

## 10.4 Incremental

```text
cambios
→ facts eliminados y añadidos
→ clonar la generación activa como candidata
→ aplicar un delta agregado en una transacción
→ cerrar, sincronizar y reabrir
→ doctor, integridad y golden probes
→ construir y validar HotSnapshot
→ publicar CURRENT
```

Los eventos del filesystem se agruparán antes de escribir. El writer no
emitirá una consulta por fact salvo que un benchmark lo justifique. Se medirá
si conviene alimentar LadybugDB y HotSnapshot desde los mismos facts
normalizados; el arranque y la recuperación siempre deben poder reconstruir el
snapshot desde LadybugDB.

## 10.5 Extensiones

Ladygraph no descargará ni instalará automáticamente extensiones de LadybugDB.

Toda extensión deberá:

* declararse;
* fijarse por versión;
* tener checksum;
* instalarse durante el build o provisioning;
* permanecer fuera del camino de arranque normal.

---

# 11. HotSnapshot

## 11.1 Objetivo

LadybugDB es la fuente canónica. El HotSnapshot es el motor de consultas.

```go
var activeSnapshot atomic.Pointer[GraphSnapshot]
```

## 11.2 Estructura

```go
type GraphSnapshot struct {
    ID        uint64
    CreatedAt int64

    Strings StringTable

    Repositories []RepositoryRecord
    Packages     []PackageRecord
    Files        []FileRecord
    Symbols      []SymbolRecord
    Evidence     []EvidenceRecord

    ForwardOffsets []uint32
    ForwardEdges   []PackedEdge

    ReverseOffsets []uint32
    ReverseEdges   []PackedEdge

    SymbolByStableKey map[StableKey]SymbolID
    SymbolsByName     map[InternedString][]SymbolID
    SymbolsByQName    map[InternedString][]SymbolID
    FileByRepoPath    map[RepoPathKey]FileID

    UnresolvedByPackage map[InternedString][]UnresolvedID
}
```

## 11.3 CSR

```text
ForwardOffsets[symbol]
ForwardOffsets[symbol+1]
```

definen el rango de aristas salientes.

Lo mismo para referencias entrantes.

```go
start := snapshot.ReverseOffsets[id]
end := snapshot.ReverseOffsets[id+1]
edges := snapshot.ReverseEdges[start:end]
```

## 11.4 Arista compacta

```go
type PackedEdge struct {
    Target     uint32
    Evidence   uint32
    Kind       uint8
    Confidence uint8
    Provenance uint8
    Flags      uint8
}
```

Objetivo:

```text
12–16 bytes por arista
```

## 11.5 Strings

Se aplicará interning a:

* nombres de repositorios;
* paquetes;
* paths;
* nombres;
* qualified names;
* module specifiers;
* commits.

## 11.6 Recorridos

BFS y DFS acotados mediante:

* slices preasignados;
* IDs densos;
* visited-generation arrays;
* buffers reutilizados;
* filtros por tipo de arista;
* límite de profundidad;
* límite de nodos;
* timeout lógico.

No se utilizará `map[SymbolID]bool` en el camino crítico salvo que un benchmark lo justifique.

---

# 12. TypeScript

## 12.1 Versión

Cada proyecto podrá usar su propia versión de TypeScript.

Orden de selección:

```text
1. dependencia del repositorio;
2. dependencia del workspace;
3. versión fallback fijada por Ladygraph.
```

Se agruparán proyectos compatibles para reducir workers.

## 12.2 Descubrimiento

```text
package.json
tsconfig.json
tsconfig.*.json
project references
workspace manifests
```

## 12.3 Resolución de módulos

Ladygraph deberá respetar:

```text
moduleResolution
package.json type
exports
imports
types
typings
typesVersions
paths
baseUrl
project references
declaration maps
```

TypeScript adapta su resolución según la configuración del proyecto y el formato de módulos, por lo que Ladygraph debe utilizar las opciones reales del proyecto y no un resolvedor nominal propio.

## 12.4 Resolución de símbolos

Pipeline:

```text
import syntax
→ module resolution
→ package provider registry
→ source/declaration target
→ symbolAtLocation
→ aliasedSymbol
→ original declaration
→ stable key
```

## 12.5 Reexports

Resolver:

```ts
export { value } from "./module";
export { value as alias } from "./module";
export * from "./module";
```

Se conservará:

```text
consumer
→ imported alias
→ reexport
→ original symbol
```

## 12.6 Declaration maps

Orden:

```text
declaration map
project reference
package source metadata
rootDir/outDir mapping
provider export registry
unresolved
```

Nunca se enlazará un `.d.ts` a un `.ts` solo por nombre.

## 12.7 Clasificación de referencias

```ts
target()
```

```text
CALLS_DIRECT
```

```ts
register(target)
```

```text
PASSES_AS_CALLBACK
```

```ts
const handler = target
```

```text
ASSIGNS_FUNCTION
```

```ts
return target
```

```text
RETURNS_FUNCTION
```

Otros usos:

```text
REFERENCES
```

---

# 13. Go

## 13.1 Carga

Se empleará `go/packages` con:

```go
packages.NeedName |
packages.NeedFiles |
packages.NeedCompiledGoFiles |
packages.NeedSyntax |
packages.NeedTypes |
packages.NeedTypesInfo |
packages.NeedImports |
packages.NeedDeps |
packages.NeedModule
```

Cada ejecución de `packages.Load` crea un universo nuevo de identidad de tipos, por lo que no se mezclarán objetos `go/types` de diferentes cargas. Todos los resultados se normalizarán antes de abandonar la sesión de análisis.

## 13.2 Workspace sintético

Ladygraph generará:

```text
~/.local/state/ladygraph/go.work
```

Nunca escribirá un `go.work` en los repositorios.

## 13.3 Extracción

```text
TypesInfo.Defs
TypesInfo.Uses
TypesInfo.Selections
TypesInfo.Types
TypesInfo.Implicits
```

## 13.4 Llamadas

Para cada `ast.CallExpr`:

```text
resolver Fun
→ objeto exacto
→ CALLS_DIRECT
```

## 13.5 Callbacks

Para cada argumento:

```text
si el argumento resuelve a función o método
→ PASSES_AS_CALLBACK
```

No se clasificará como llamada directa.

## 13.6 Métodos

Utilizar:

```text
TypesInfo.Selections
receiver type
method object
objectpath
```

## 13.7 SSA

Fuera del MVP.

Se añadirá únicamente para:

* funciones almacenadas;
* interfaces;
* closures;
* llamadas indirectas;
* análisis de flujo.

Toda arista SSA deberá declarar el algoritmo y la confianza.

---

# 14. Registro cross-repository

## 14.1 TypeScript package registry

```text
package name
→ repository
→ package root
→ manifest
→ exports
→ source roots
→ declaration roots
→ TypeScript project
```

## 14.2 Go module registry

```text
module path
→ repository
→ module root
→ packages
```

## 14.3 Ambigüedad

Si dos repositorios declaran el mismo provider:

```text
AMBIGUOUS_PACKAGE_PROVIDER
```

No se elegirá automáticamente uno.

## 14.4 Incompatibilidad

```text
PACKAGE_VERSION_MISMATCH
MODULE_REPLACE_CONFLICT
EXPORT_NOT_FOUND
DECLARATION_SOURCE_NOT_MAPPED
PACKAGE_PROVIDER_NOT_FOUND
```

Todo fallo debe quedar clasificado.

---

# 15. Actualización incremental

## 15.1 Watcher

`fsnotify` proporciona notificaciones de filesystem multiplataforma para Go. Ladygraph lo utilizará como señal de cambio, no como única fuente de verdad.

```text
evento
→ debounce
→ stat
→ hash
→ comparación
→ clasificación
→ invalidación
```

## 15.2 Reconciliación

Cada diez minutos, por defecto:

```text
recorrer manifests y fuentes
→ comparar paths, tamaños y hashes
→ detectar eventos perdidos
```

## 15.3 Clasificación

### Implementación interna

```text
cambia cuerpo sin cambiar firma/imports/exports
→ reindexar archivo
→ reemplazar aristas originadas allí
```

### API pública

```text
cambia firma/export
→ reindexar provider
→ invalidar consumidores
→ resolver referencias afectadas
```

### Manifest

```text
package.json
tsconfig
go.mod
```

```text
→ reconstruir registry afectado
→ invalidar module resolution
→ reindexar proyecto o módulo
```

### Eliminación

```text
eliminar nodos y aristas salientes
→ convertir referencias entrantes afectadas a unresolved
→ reindexar consumidores
```

## 15.4 Publicación

```text
actualización LadybugDB
→ validación
→ HotSnapshot nuevo
→ probes mínimos
→ atomic swap
```

Las consultas activas continúan con el snapshot anterior.

---

# 16. Protocolo Go–TypeScript

## 16.1 Primera versión

```text
stdin/stdout
length prefix de 32 bits
JSON UTF-8
```

## 16.2 Mensajes

```text
HELLO
OPEN_WORKSPACE
INDEX_PROJECT
UPDATE_FILES
REMOVE_FILES
GET_STATUS
SHUTDOWN
```

## 16.3 Resultados

```text
RepositoryFact
PackageFact
FileFact
SymbolFact
EdgeFact
EvidenceFact
UnresolvedFact
DiagnosticFact
```

## 16.4 Migración a Protobuf

Solo se realizará si:

```text
serialización > 5 % del tiempo total
o
payload medio > 10 MiB
o
JSON provoca presión de memoria significativa
```

No se utilizará gRPC.

---

# 17. Superficie MCP

Ladygraph utilizará el SDK oficial de MCP para Go, que proporciona las APIs para construir servidores, registrar tools y gestionar el protocolo.

## 17.1 Tools

```text
list_repositories
find_symbol
get_symbol
get_file_outline
find_references
find_cross_repo_consumers
trace_dependencies
get_blast_radius
get_unresolved_references
graph_status
```

Total:

```text
10 tools
```

Diez es el techo. Repowise midió en Claude Code cuántas veces un agente llega
a llamar a cada servidor MCP y sale un acantilado por tamaño de superficie:
un servidor de 1 tool y `1.567` caracteres de esquema fue llamado 13 de 15
veces; uno de 10 tools y `17.561` caracteres, 15 de 15; uno de 29 y `29.050`,
4 de 15; y uno de 30 y `28.118`, ninguna. Claude Code carga los esquemas bajo
demanda, así que una superficie grande es una superficie que el agente no
llega a mirar. La tool once exige retirar una.

## 17.2 No se expondrá

```text
execute_cypher
execute_query
index
update
refresh
rebuild
register_repository
remove_repository
edit_file
run_command
```

## 17.3 Respuesta estándar

```json
{
  "snapshot_id": 42,
  "snapshot_age_ms": 512,
  "total": 128,
  "returned": 50,
  "truncated": true,
  "next_cursor": "opaque-cursor",
  "coverage": {
    "exact": 117,
    "candidate": 4,
    "unresolved_related": 7
  },
  "results": []
}
```

## 17.4 Cursores

Contendrán:

```text
snapshot_id
query_hash
offset
sorting_version
checksum
```

Si cambia el snapshot:

```text
CURSOR_SNAPSHOT_EXPIRED
```

## 17.5 Errores

```text
INVALID_ARGUMENT
SYMBOL_NOT_FOUND
AMBIGUOUS_SYMBOL
REPOSITORY_NOT_FOUND
CURSOR_INVALID
CURSOR_SNAPSHOT_EXPIRED
TRAVERSAL_LIMIT_REACHED
SNAPSHOT_UNAVAILABLE
INDEX_NOT_READY
```

---

# 18. CLI

```bash
ladygraph init
ladygraph serve
ladygraph index
ladygraph update
ladygraph status
ladygraph doctor
ladygraph benchmark
ladygraph inspect
ladygraph export
ladygraph version
```

## `ladygraph init`

* crea configuración;
* crea directorios;
* valida LadybugDB;
* registra repositorios iniciales.

## `ladygraph index`

```bash
ladygraph index --full
ladygraph index --repository service-a
```

## `ladygraph doctor`

Comprueba:

* configuración;
* rutas;
* permisos;
* versión del esquema;
* integridad de LadybugDB;
* stable keys duplicadas;
* aristas colgantes;
* referencias no clasificadas;
* snapshot;
* worker TypeScript;
* toolchain Go;
* repositorios inaccesibles.

## `ladygraph inspect`

Permite consultas administrativas seguras y predefinidas.

Cypher arbitrario solo podrá habilitarse explícitamente en modo desarrollo.

---

# 19. Observabilidad

## 19.1 Logs

JSON estructurado:

```json
{
  "level": "info",
  "component": "indexer",
  "event": "incremental_update",
  "repository": "service-a",
  "changed_files": 3,
  "duration_ms": 221
}
```

No registrar contenido fuente por defecto.

## 19.2 Métricas

```text
ladygraph_query_duration
ladygraph_query_total
ladygraph_query_errors
ladygraph_query_results
ladygraph_query_truncated

ladygraph_snapshot_id
ladygraph_snapshot_age
ladygraph_snapshot_build_duration
ladygraph_snapshot_bytes

ladygraph_index_duration
ladygraph_index_files
ladygraph_index_symbols
ladygraph_index_edges
ladygraph_index_unresolved

ladygraph_ladybug_transaction_duration
ladygraph_ladybug_database_bytes

ladygraph_ts_worker_restarts
ladygraph_ts_worker_memory
```

OpenTelemetry Go permite instrumentar métricas y trazas; se añadirá después de estabilizar el MVP, manteniendo exportadores desactivados por defecto.

## 19.3 Profiling

Desarrollo:

```text
go test -bench
go test -benchmem
pprof
go tool trace
```

Producción:

```text
pprof desactivado
activación explícita
solo loopback
```

---

# 20. Seguridad

## 20.1 Solo lectura MCP

Todas las tools:

```text
readOnlyHint = true
```

## 20.2 Paths

* canonicalización con `realpath`;
* bloqueo de escapes;
* bloqueo de symlinks fuera del repo, configurable;
* allowlist explícita de repositorios;
* ninguna ruta arbitraria desde una llamada MCP.

## 20.3 Worker

* argumentos controlados;
* sin shell;
* protocolo tipado;
* tamaño máximo de mensaje;
* timeout;
* reinicio limitado;
* stderr separado.

## 20.4 Permisos

```text
directorios: 0700
archivos:    0600
umask:       0077
```

## 20.5 Base

LadybugDB solo será abierta por Ladygraph.

No se permitirá simultáneamente:

```text
otro proceso Ladygraph
CLI de LadybugDB
explorador
script externo
```

sobre la base viva.

---

# 21. Build y distribución

LadybugDB introduce una biblioteca nativa en la integración Go, por lo que la distribución debe tratarse como una parte del producto, no como un detalle posterior.

## 21.1 Plataformas iniciales

```text
Linux amd64
Linux arm64, después
macOS arm64, después
Windows, fuera del primer MVP
```

## 21.2 Build reproducible

Registrar:

```text
Go version
Node version
TypeScript fallback version
LadybugDB version
go-ladybug version
Tree-sitter grammars
checksums
commit
compiler flags
CGO flags
```

## 21.3 Artefacto

```text
ladygraph/
├── bin/ladygraph
├── bin/ladygraph-ts-worker
├── lib/libladybug.*
├── grammars/
├── licenses/
└── manifest.json
```

## 21.4 Proveniencia

`ladygraph version --json`:

```json
{
  "ladygraph": "0.1.0",
  "commit": "...",
  "go": "...",
  "ladybug": "...",
  "go_ladybug": "...",
  "typescript_fallback": "...",
  "schema": 1,
  "resolver": 1
}
```

La misma versión deberá aparecer en `serverInfo.version`.

---

# 22. Suite de pruebas

## 22.1 TypeScript

```text
relative imports
package imports
default exports
named exports
aliases
barrels
export *
namespace imports
dynamic imports
require
paths
baseUrl
Node16
NodeNext
Bundler
typesVersions
project references
declaration maps
overloads
generics
methods
local shadowing
homonyms
generated declarations
```

## 22.2 Go

```text
direct calls
callbacks
function assignment
method calls
method expressions
interfaces
embedding
generics
closures
package aliases
dot imports
go.work
replace
test packages
homonyms across modules
```

## 22.3 Cross-repository

```text
consumer package → provider package
imported alias → source declaration
reexport chain
version mismatch
duplicate provider
missing provider
provider removed
consumer moved
```

## 22.4 Integridad

```text
0 exact edges with missing source
0 exact edges with missing target
0 evidence pointing to missing file
0 duplicate stable keys
0 invalid repository ownership
0 unclassified exact confidence
0 stale edges after deletion
```

## 22.5 Fuzzing

```text
MCP inputs
cursor decoding
worker frames
config parsing
manifest parsing
stable key generation
graph traversal limits
snapshot loading
```

---

# 23. Fases de implementación

## Fase 0 — Constitución

### Pasos

1. Crear repositorio.
2. Elegir licencia.
3. Crear ADRs.
4. Crear estructura.
5. Configurar CI.
6. Definir políticas de versiones.
7. Congelar objetivos de rendimiento.
8. Crear corpus de fixtures.

### Gate

```text
PROJECT_FOUNDATION_PASS
```

---

## Fase 1 — MCP vacío

### Implementar

```text
graph_status
list_repositories
```

Sin grafo real.

### Benchmark

```text
10.000 llamadas
1, 4, 16 y 32 clientes
```

### Gate

```text
p95 backend ≤ 2 ms
0 errores
0 fugas
```

---

## Fase 2 — Calificación de LadybugDB

### Corpus

```text
100.000 símbolos
1.000.000 aristas
40 repositorios
```

### Medir

* bulk load;
* incremental inserts;
* deletes;
* desglose de latencia de deltas;
* reverse references;
* depth-3;
* depth-5;
* reapertura;
* cierre forzado;
* recuperación;
* `ENOSPC` durante escritura, cierre y publicación;
* espacio;
* RSS.

### Gate

```text
LADYBUG_SCHEMA_PASS
LADYBUG_BULK_LOAD_PASS
LADYBUG_INCREMENTAL_PASS
LADYBUG_RECOVERY_PASS
LADYBUG_DELTA_PERFORMANCE_PASS
LADYBUG_STORAGE_PASS
```

Si falla, se detiene la secuencia oficial antes de construir indexadores. Las
estructuras puras de HotSnapshot podrán explorarse de forma aislada con
fixtures, pero no se marcarán como completadas ni se integrarán con LadybugDB.

---

## Fase 3 — HotSnapshot

### Implementar

* IDs densos;
* string interning;
* CSR;
* índices;
* BFS;
* cursores;
* atomic swap;
* extracción secuencial completa desde LadybugDB;
* comparación con construcción directa desde facts normalizados.

### Gate

```text
full scan LadybugDB ≤ 1 s
1M edges snapshot build ≤ 2 s
commit → snapshot publicado ≤ 3 s
point lookup p95 ≤ 2 ms
references p95 ≤ 5 ms
depth-3 p95 ≤ 20 ms
```

---

## Fase 4 — Repository Registry

### Implementar

* repositorios;
* Git;
* package.json;
* tsconfig;
* go.mod;
* providers;
* conflictos.

### Gate

```text
REPOSITORY_REGISTRY_PASS
PACKAGE_PROVIDER_PASS
MODULE_PROVIDER_PASS
```

---

## Fase 5 — Tree-sitter

### Implementar

* inventario;
* parsing;
* queries;
* changed ranges;
* clasificación de invalidación.

### Gate

```text
SYNTAX_INVENTORY_PASS
INCREMENTAL_PARSE_PASS
NO_SEMANTIC_AUTHORITY_PASS
```

---

## Fase 6 — TypeScript intrarrepositorio

### Implementar

* worker;
* proyectos;
* símbolos;
* imports;
* exports;
* aliases;
* referencias;
* callbacks.

### Gate

```text
TS_LOCAL_IDENTITY_PASS
TS_LOCAL_REFERENCE_PASS
TS_HOMONYM_NEGATIVE_PASS
```

---

## Fase 7 — TypeScript cross-repository

### Implementar

* package registry;
* package exports;
* reexports;
* declaration mapping;
* source reconciliation.

### Gate

```text
TS_CROSS_REPO_EXACT_PASS
TS_REEXPORT_PASS
TS_DECLARATION_MAP_PASS
TS_NO_FALSE_PROVIDER_PASS
```

No se inicia Go hasta superar esta fase.

---

## Fase 8 — Go intramódulo

### Implementar

* packages.Load;
* defs;
* uses;
* calls;
* callbacks;
* methods;
* objectpath.

### Gate

```text
GO_LOCAL_IDENTITY_PASS
GO_DIRECT_CALL_PASS
GO_CALLBACK_PASS
GO_HOMONYM_NEGATIVE_PASS
```

---

## Fase 9 — Go cross-module

### Implementar

* module registry;
* synthetic go.work;
* replaces;
* consumers;
* identity global.

### Gate

```text
GO_CROSS_MODULE_PASS
GO_REPLACE_PASS
GO_NO_FALSE_PROVIDER_PASS
```

---

## Fase 10 — Incrementalidad

### Probes

```text
add
modify
rename
move
delete
change export
change package.json
change tsconfig
change go.mod
remove provider
```

### Gate

```text
0 ghost symbols
0 ghost edges
0 dangling exact edges
single-file p95 ≤ 750 ms
queries available during update
```

---

## Fase 11 — Superficie MCP completa

Implementar las nueve tools con:

* validación;
* paginación;
* truncamiento;
* cobertura;
* typed errors;
* stable sorting.

### Gate

```text
MCP_CONTRACT_PASS
MCP_READ_ONLY_PASS
MCP_PAGINATION_PASS
```

---

## Fase 12 — Resiliencia

### Fallos

```text
SIGKILL en worker
SIGKILL en commit
disco lleno
snapshot corrupto
base staging corrupta
config inválida
repo desaparecido
tsconfig inválido
go.mod inválido
proceso duplicado
```

### Gate

```text
LAST_VALID_SNAPSHOT_AVAILABLE
NO_PARTIAL_PUBLICATION
RECOVERY_PASS
DOCTOR_DIAGNOSTICS_PASS
```

---

## Fase 13 — Rendimiento

### Workload

```text
40 % find_symbol
25 % get_symbol
20 % find_references
10 % find_cross_repo_consumers
5 % blast_radius
```

### Concurrencia

```text
1
4
16
32
```

### Gate

```text
0 errores
p95 point queries ≤ 5 ms
p99 point queries ≤ 15 ms
p95 depth-3 ≤ 20 ms
RSS estable
sin contención global
```

---

## Fase 14 — Observabilidad

* métricas;
* logs;
* health;
* perfiles;
* counters;
* snapshot age.

### Gate

```text
OBSERVABILITY_PASS
```

---

## Fase 15 — Distribución

* builds;
* checksums;
* licencias;
* manifest;
* instalación;
* upgrade;
* rollback.

### Gate

```text
REPRODUCIBLE_BUILD_PASS
VERSION_PROVENANCE_PASS
UPGRADE_PASS
ROLLBACK_PASS
```

---

# 24. Criterios de producción

Para aprobar:

```text
ACCEPT_LADYGRAPH_FOR_PRODUCTION
```

deben pasar:

```text
PROJECT_FOUNDATION_PASS
LADYBUG_RECOVERY_PASS
HOT_QUERY_PERFORMANCE_PASS
TS_CROSS_REPO_EXACT_PASS
GO_CALLBACK_PASS
GO_CROSS_MODULE_PASS
NOMINAL_COLLISION_PASS
INTEGRITY_PASS
INCREMENTAL_PASS
MCP_CONTRACT_PASS
MCP_READ_ONLY_PASS
RESILIENCE_PASS
PERFORMANCE_PASS
VERSION_PROVENANCE_PASS
```

Bloqueos:

```text
BLOCKED_FALSE_EXACT_EDGE
BLOCKED_DANGLING_EDGE
BLOCKED_CROSS_REPO_RESOLUTION
BLOCKED_INCREMENTAL_CORRUPTION
BLOCKED_SNAPSHOT_INCONSISTENCY
BLOCKED_LIFECYCLE
BLOCKED_PERFORMANCE
```

Regla final:

> Una relación exacta falsa bloquea producción. Una referencia no resuelta correctamente declarada no.

---

# 25. Tecnologías aplazadas

No entran en el MVP:

```text
Protocol Buffers
OpenTelemetry exporters
Go PGO
Roaring bitmaps
mmap
SSA
HTTP transport
Arrow/Parquet export
visualización
HTTP/gRPC/topic contracts
SCIP import
```

Se incorporarán únicamente si un benchmark o una necesidad funcional concreta lo justifica.

PGO podrá evaluarse cuando exista un workload estable; Go permite alimentar al compilador con perfiles representativos para optimizar rutas calientes.

---

# 26. Orden de ejecución definitivo

```text
01. Crear repositorio y ADRs.
02. Congelar SLOs.
03. Crear MCP vacío.
04. Medir overhead.
05. Calificar LadybugDB.
06. Construir HotSnapshot.
07. Alcanzar objetivos sintéticos.
08. Implementar Repository Registry.
09. Implementar Tree-sitter.
10. Implementar TypeScript local.
11. Resolver TypeScript cross-repository.
12. Pasar fixtures negativos.
13. Implementar Go local.
14. Resolver Go cross-module.
15. Implementar incrementalidad.
16. Implementar las nueve tools.
17. Ejecutar integridad completa.
18. Ejecutar resiliencia.
19. Perfilar.
20. Optimizar.
21. Ejecutar carga concurrente.
22. Añadir observabilidad.
23. Construir distribución reproducible.
24. Ejecutar aceptación final.
```

---

# 27. Resultado esperado

Ladygraph deberá terminar con esta arquitectura:

```text
                     INDEXACIÓN

                 Repository Registry
                         │
       ┌─────────────────┼─────────────────┐
       │                 │                 │
       ▼                 ▼                 ▼
  Tree-sitter      TS Compiler API    go/packages
       │                 │             go/types
       └─────────────────┼─────────────────┘
                         ▼
                Semantic Normalizer
                         │
                         ▼
                Cross-Repo Resolver
                         │
                         ▼
                    LadybugDB
              canonical persistent graph
                         │
                         ▼
               HotSnapshot Builder
                         │
                         ▼
                   atomic.Pointer


                     CONSULTA

Cliente MCP
    │
    ▼
Ladygraph MCP
    │
    ▼
HotSnapshot
    │
    ├── exact indexes
    ├── forward CSR
    ├── reverse CSR
    └── bounded traversal
    │
    ▼
Respuesta estructurada
```

Contrato final:

```text
LadybugDB:
  verdad persistente

HotSnapshot:
  velocidad

TypeScript y Go:
  identidad semántica

Tree-sitter:
  aceleración sintáctica

Unresolved references:
  honestidad

MCP:
  interfaz de consulta cerrada y segura
```

Ladygraph será rápido porque no analizará código cuando recibe una pregunta. Recibirá los resultados de análisis previamente certificados y consultará estructuras compactas en memoria.
