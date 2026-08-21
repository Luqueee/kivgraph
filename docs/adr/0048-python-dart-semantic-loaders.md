# ADR 0048: Python and Dart semantic loaders

- **Estado:** aceptada con limitaciones declaradas
- **Fecha:** 2026-08-21
- **Revisa:** ADR 0006, ADR 0030

## Contexto

Kivgraph ya tiene adaptadores de Go, TypeScript y Rust, pero el registro de
lenguajes y la pasada completa no podían indexar Python ni Dart. Tree-sitter
sirve para inventario y clasificación sintáctica, pero no debe convertir una
coincidencia nominal en una arista exacta.

## Decisión

Python entra mediante un límite JSON versionado (`facts.SemanticPayload`). El
worker incluido usa la AST de la biblioteca estándar para ofrecer una ruta
hermética sin dependencias de npm o pip. Sus referencias son `CANDIDATE` porque
la AST no resuelve tipos, imports dinámicos, sobrecargas ni despacho dinámico.
Un productor externo puede emitir el mismo payload con `authoritative: true`.

Dart entra mediante el Dart Analysis Server del SDK, usando su protocolo
`analysis.getNavigation`. La extracción consulta todas las regiones de
navegación de un archivo en una petición por archivo, en lugar de pedir
referencias una por símbolo. El servidor resuelve imports, clases, funciones,
métodos y llamadas; esas aristas se publican como `EXACT_TYPECHECKED`.

Tree-sitter Python se registra para aceleración sintáctica local. No se añade
una gramática Dart no oficial: el servidor oficial del SDK es la autoridad y
evita fijar una dependencia comunitaria sin contrato de mantenimiento.

Los objetivos externos pueden declararse con identidad de proveedor y se
resuelven después de combinar los facts de todos los repositorios. Si el
proveedor es ambiguo o no publica la declaración, la relación queda
`UNRESOLVED`. Las importaciones que identifican de forma única un paquete
registrado producen `PACKAGE_DEPENDS_ON`, sin convertir esa dependencia en un
uso de símbolo.

## Consecuencias

- La normalización común crea repositorio, paquete, fichero, símbolos,
  `DEFINES`, evidencia, referencias, llamadas, imports, `PART_OF` para partes
  Dart y `UNRESOLVED`.
- `dart.include_tests` e `include_generated` controlan el alcance; por defecto
  se omiten tests y artefactos generados.
- La caché incluye árbol de fuentes, manifests, opciones del analizador y el
  worker Python; el formato de entrada se incrementa a la versión 4.
- Los contadores Python y Dart recorren la configuración, el indexador, el
  proceso hijo y las salidas CLI/MCP.

## Limitaciones y riesgos

- El worker Python no es un type checker: un grafo Python completo con aristas
  exactas requiere un productor externo compatible con el payload.
- Las relaciones de símbolo cross-repository requieren que el productor
  entregue explícitamente la identidad del proveedor; una importación por sí
  sola solo puede producir una dependencia de paquete.
- La invalidación específica Python/Dart se clasifica de forma conservadora,
  pero la ejecución actual sigue usando la pasada semántica completa como
  unidad segura de publicación.
- El rango completo de un símbolo Dart no siempre lo devuelve LSP como
  `DocumentSymbol`; el loader aplica un fallback de símbolo contenedor por
  posición. Se conserva como caso de regresión en fixtures.
- Los offsets que devuelve LSP/Analysis Server se convierten de UTF-16 a
  offsets de bytes antes de llegar al payload canónico.
- El Analysis Server consume el SDK y puede analizar dependencias Flutter
  transitivas, pero el loader filtra sus targets externos y nunca escribe en
  el proyecto fuente.
