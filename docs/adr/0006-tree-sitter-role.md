# ADR 0006: Tree-sitter como acelerador sintáctico

- **Estado:** aceptada
- **Fecha:** 2026-08-04

## Context

Luque necesita detectar cambios y mantener inventarios sintácticos con rapidez,
incluso cuando un archivo está temporalmente incompleto o contiene errores de
sintaxis. Tree-sitter ofrece parsing incremental y árboles tolerantes, pero la
precisión cross-repository requiere la semántica del compilador de cada
lenguaje.

## Decision

Tree-sitter se utilizará como acelerador sintáctico e incremental, no como
autoridad semántica. Podrá producir inventarios, rangos, candidatos y señales
de invalidación. No podrá crear por sí solo aristas `EXACT`, resolver providers,
identificar definiciones cross-repository ni convertir una coincidencia textual
en una relación semántica.

Las relaciones exactas deben estar respaldadas por evidencia de la
TypeScript Compiler API, `go/types`/`go/packages` u otra fuente semántica
explícita aprobada. Cuando la evidencia no sea suficiente, el resultado se
clasifica como `CANDIDATE` o `UNRESOLVED`.

## Alternatives

- **Usar Tree-sitter como único parser:** sería rápido y tolerante, pero no
  resuelve tipos, aliases, módulos ni overloads con la precisión requerida.
- **Eliminar el parser sintáctico auxiliar:** reduciría dependencias, pero
  perdería actualización incremental y capacidad de trabajar con código
  incompleto.
- **Usar solo compiladores semánticos:** produciría resultados autoritativos,
  pero aumentaría el coste de detectar cambios y de procesar archivos con
  errores sintácticos.

## Consequences

- El pipeline debe conservar la procedencia y autoridad de cada hecho.
- Los resultados de Tree-sitter se pueden invalidar o reemplazar sin migrar
  identidad semántica.
- Los fixtures negativos deben comprobar que no aparecen aristas exactas por
  nombre, texto, path o proximidad en el árbol.
- La gramática y la versión de Tree-sitter se fijan y se benchmarkean.

## Risks

- Un consumidor puede tratar accidentalmente un candidato sintáctico como
  relación exacta; los tipos y estados del modelo deben impedir esa conversión.
- Cambios de gramática pueden alterar rangos o nodos; el inventario debe
  registrar la versión y mantener fixtures reproducibles.
- El parser puede aceptar sintaxis inválida parcialmente; esa tolerancia no se
  interpreta como evidencia de compilación.

## Versionado reproducible

`grammars/manifest.json` es la fuente versionada de las grammars iniciales:
TypeScript, TSX, JavaScript y Go. Cada entrada fija el tag, el commit de 40
caracteres, la ruta dentro del archivo fuente, la URL del archivo `tar.gz`, su
SHA-256 y la licencia MIT. TypeScript y TSX comparten deliberadamente el mismo
repositorio, commit y checksum. `internal/syntax.LoadManifest` rechaza
manifiestos incompletos, URLs no fijadas, checksums con formato incorrecto y
grammars faltantes antes de que un parser pueda consumirlos.

El runtime oficial `github.com/tree-sitter/go-tree-sitter` se fija en
`v0.25.0`. Las grammars seleccionadas generan ABI 15; el runtime `v0.24.0`
rechaza ese ABI, por lo que no se permite reducir el runtime de forma
independiente de las grammars.

## Status

Aceptada. Tree-sitter queda limitado a aceleración y clasificación sintáctica.
