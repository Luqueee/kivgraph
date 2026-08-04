# Esquema sintético de LadybugDB

**Versión:** `001`

**DDL:** [`schemas/ladybug/001-synthetic.cypher`](../../schemas/ladybug/001-synthetic.cypher)

Este esquema es deliberadamente pequeño. Sirve para medir carga y consultas
sin adelantar el modelo persistente completo descrito en `PLAN.md`. Los nodos y
aristas representan hechos producidos por un analizador; una coincidencia de
nombre nunca crea por sí sola una arista exacta.

## Nodos

| Tabla | Clave primaria | Propiedades relevantes |
| --- | --- | --- |
| `Repository` | `stable_key` | `name`, `path`, `language` |
| `File` | `stable_key` | `repository_key`, `path`, `content_hash`, `language` |
| `Symbol` | `stable_key` | `repository_key`, `file_key`, `name`, `qualified_name`, `kind`, `signature`, `start_line`, `end_line` |

`stable_key` es la identidad persistente del nodo. `name`, `path` y
`qualified_name` son datos legibles o atributos de resolución; no sustituyen a
la clave y no se usan como identidad global.

## Relaciones

| Tabla | Origen → destino | Significado |
| --- | --- | --- |
| `CONTAINS` | `Repository → File` | El fichero pertenece al repositorio registrado. |
| `DEFINES` | `File → Symbol` | El fichero declara el símbolo. |
| `REFERENCES` | `Symbol → Symbol` | El analizador obtuvo evidencia de una referencia a un símbolo concreto. |
| `CALLS_DIRECT` | `Symbol → Symbol` | El analizador obtuvo evidencia de una llamada directa concreta. |

Las relaciones de resolución guardan `evidence_kind` y las claves de fichero
implicadas para que los benchmarks puedan conservar el origen de la arista.
Estos campos no convierten una relación nominal en exacta: el productor debe
haber clasificado la evidencia antes de insertar la arista.

## Invariantes de la versión 001

1. Todas las tablas de nodos tienen una clave primaria explícita.
2. Cada `File` y `Symbol` conserva la identidad del repositorio de origen.
3. `CONTAINS` y `DEFINES` son relaciones estructurales; `REFERENCES` y
   `CALLS_DIRECT` requieren evidencia del analizador.
4. Las relaciones no tienen multiplicidad `ONE`; el corpus sintético puede
   contener varios símbolos, referencias o llamadas entre el mismo par.
5. El DDL usa `IF NOT EXISTS` para permitir que una prueba reconstruya el
   esquema en una base vacía sin fallar por una segunda ejecución.

## Fuera de alcance

`Package`, `Evidence`, `Snapshot`, referencias no resueltas, imports,
callbacks, tipos e implementaciones pertenecen al esquema canónico posterior.
Se incorporarán solo después de medir el layout y las multiplicidades con el
corpus real; no se simulan en esta versión.
