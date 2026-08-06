# Esquema canónico de LadybugDB

Versión del esquema: `002`. DDL versionado: `schemas/ladybug/002-canonical.cypher`.

Este documento se genera desde `internal/storage/ladybug.CanonicalSchemaDocumentation`; no se edita a mano.

## Reglas

- Toda clave primaria es una clave durable de Ladygraph. Ninguna se deriva de un nombre visible ni la genera la base.
- `GraphMetadata` guarda la versión del esquema y la del resolutor: una base con otra versión se reconstruye, no se migra en caliente.
- Las rutas de `File` son relativas al repositorio, de modo que una clave nunca incrusta la máquina que la produjo.
- Toda relación semántica transporta `confidence`, `provenance` y `evidence_key`; una arista sin procedencia no puede ser exacta.
- LadybugDB indexa la clave primaria de cada tabla. No se declaran índices secundarios: las búsquedas exactas por repositorio, archivo o nombre las sirve el HotSnapshot, no la base.

## Nodos

### GraphMetadata

schema version and resolver identity of the stored graph.

| Propiedad | Tipo | Nota |
| --- | --- | --- |
| `key` | `STRING` | clave primaria |
| `value` | `STRING` | — |

### Repository

one indexed repository.

| Propiedad | Tipo | Nota |
| --- | --- | --- |
| `stable_key` | `STRING` | clave primaria |
| `name` | `STRING` | — |
| `root_path` | `STRING` | — |
| `commit` | `STRING` | — |
| `branch` | `STRING` | — |
| `dirty` | `BOOL` | — |
| `languages` | `STRING` | comma separated, sorted |

### Package

npm package or Go package; container holds the Go module.

| Propiedad | Tipo | Nota |
| --- | --- | --- |
| `stable_key` | `STRING` | clave primaria |
| `repository_key` | `STRING` | — |
| `language` | `STRING` | — |
| `name` | `STRING` | — |
| `version` | `STRING` | — |
| `root_path` | `STRING` | — |
| `manifest_path` | `STRING` | — |
| `container` | `STRING` | — |

### File

path is repository relative so a key never embeds a machine.

| Propiedad | Tipo | Nota |
| --- | --- | --- |
| `stable_key` | `STRING` | clave primaria |
| `repository_key` | `STRING` | — |
| `package_key` | `STRING` | — |
| `path` | `STRING` | — |
| `language` | `STRING` | — |
| `content_hash` | `STRING` | — |
| `generated` | `BOOL` | — |

### Symbol

canonical_identity is the auditable text the key derives from.

| Propiedad | Tipo | Nota |
| --- | --- | --- |
| `stable_key` | `STRING` | clave primaria |
| `canonical_identity` | `STRING` | — |
| `repository_key` | `STRING` | — |
| `package_key` | `STRING` | — |
| `file_key` | `STRING` | — |
| `language` | `STRING` | — |
| `name` | `STRING` | — |
| `qualified_name` | `STRING` | — |
| `kind` | `STRING` | — |
| `exported` | `BOOL` | — |
| `signature` | `STRING` | — |
| `start_line` | `INT64` | — |
| `start_column` | `INT64` | — |
| `start_offset` | `INT64` | — |
| `end_line` | `INT64` | — |
| `end_offset` | `INT64` | — |

### Evidence

observation backing an edge; text is a short excerpt.

| Propiedad | Tipo | Nota |
| --- | --- | --- |
| `stable_key` | `STRING` | clave primaria |
| `repository_key` | `STRING` | — |
| `file_key` | `STRING` | — |
| `start_line` | `INT64` | — |
| `start_column` | `INT64` | — |
| `start_offset` | `INT64` | — |
| `end_offset` | `INT64` | — |
| `text` | `STRING` | — |

### UnresolvedReference

a fact that could not become an exact edge, with its reason.

| Propiedad | Tipo | Nota |
| --- | --- | --- |
| `stable_key` | `STRING` | clave primaria |
| `repository_key` | `STRING` | — |
| `file_key` | `STRING` | — |
| `language` | `STRING` | — |
| `source_symbol_key` | `STRING` | — |
| `requested_package` | `STRING` | — |
| `requested_symbol` | `STRING` | — |
| `reason` | `STRING` | — |
| `detail` | `STRING` | — |
| `start_line` | `INT64` | — |
| `start_column` | `INT64` | — |
| `start_offset` | `INT64` | — |

## Relaciones

| Relación | Origen | Destino | Multiplicidad | Propiedades |
| --- | --- | --- | --- | --- |
| `CONTAINS_PACKAGE` | Repository | Package | `ONE_MANY` | `confidence`, `provenance` |
| `CONTAINS_FILE` | Package | File | `ONE_MANY` | `confidence`, `provenance` |
| `DEFINES` | File | Symbol | `ONE_MANY` | `confidence`, `provenance` |
| `OBSERVED_IN` | Evidence | File | `MANY_ONE` | — |
| `REPORTS_UNRESOLVED` | Repository | UnresolvedReference | `ONE_MANY` | — |
| `PACKAGE_DEPENDS_ON` | Package | Package | `MANY_MANY` | `confidence`, `provenance`, `evidence_key`, `source_snapshot`, `resolver_version` |
| `MODULE_DEPENDS_ON` | Package | Package | `MANY_MANY` | `confidence`, `provenance`, `evidence_key`, `source_snapshot`, `resolver_version` |
| `IMPORTS_SYMBOL` | Symbol | Symbol | `MANY_MANY` | `confidence`, `provenance`, `evidence_key`, `source_snapshot`, `resolver_version` |
| `EXPORTS` | Symbol | Symbol | `MANY_MANY` | `confidence`, `provenance`, `evidence_key`, `source_snapshot`, `resolver_version` |
| `REEXPORTS` | Symbol | Symbol | `MANY_MANY` | `confidence`, `provenance`, `evidence_key`, `source_snapshot`, `resolver_version` |
| `REFERENCES` | Symbol | Symbol | `MANY_MANY` | `confidence`, `provenance`, `evidence_key`, `source_snapshot`, `resolver_version` |
| `CALLS_DIRECT` | Symbol | Symbol | `MANY_MANY` | `confidence`, `provenance`, `evidence_key`, `source_snapshot`, `resolver_version` |
| `PASSES_AS_CALLBACK` | Symbol | Symbol | `MANY_MANY` | `confidence`, `provenance`, `evidence_key`, `source_snapshot`, `resolver_version` |
| `ASSIGNS_FUNCTION` | Symbol | Symbol | `MANY_MANY` | `confidence`, `provenance`, `evidence_key`, `source_snapshot`, `resolver_version` |
| `RETURNS_FUNCTION` | Symbol | Symbol | `MANY_MANY` | `confidence`, `provenance`, `evidence_key`, `source_snapshot`, `resolver_version` |
| `TYPE_USES` | Symbol | Symbol | `MANY_MANY` | `confidence`, `provenance`, `evidence_key`, `source_snapshot`, `resolver_version` |
| `IMPLEMENTS` | Symbol | Symbol | `MANY_MANY` | `confidence`, `provenance`, `evidence_key`, `source_snapshot`, `resolver_version` |
| `EXTENDS` | Symbol | Symbol | `MANY_MANY` | `confidence`, `provenance`, `evidence_key`, `source_snapshot`, `resolver_version` |
| `EMBEDS` | Symbol | Symbol | `MANY_MANY` | `confidence`, `provenance`, `evidence_key`, `source_snapshot`, `resolver_version` |
| `OVERRIDES` | Symbol | Symbol | `MANY_MANY` | `confidence`, `provenance`, `evidence_key`, `source_snapshot`, `resolver_version` |

## Restricciones

- `CONTAINS_PACKAGE`, `CONTAINS_FILE` y `DEFINES` son `ONE_MANY`: un paquete pertenece a un repositorio, un archivo a un paquete y un símbolo se declara en un archivo.
- `OBSERVED_IN` es `MANY_ONE`: muchas evidencias por archivo.
- `REPORTS_UNRESOLVED` cuelga del repositorio porque un fallo de módulo no tiene archivo.
- Las relaciones semánticas son `MANY_MANY`: un símbolo puede llamar al mismo destino desde varios sitios, y cada ocurrencia lleva su evidencia.
