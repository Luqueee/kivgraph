# ADR 0007: Superficie MCP de solo lectura

- **Estado:** aceptada
- **Fecha:** 2026-08-04

## Context

Kivgraph analiza repositorios que pertenecen al usuario y expone consultas de
inteligencia de código. Una petición MCP no debe modificar el código analizado,
sus configuraciones ni metadatos, y debe ofrecer respuestas reproducibles con
límites explícitos. El indexador sí necesita actualizar el almacenamiento
interno de Kivgraph, pero esa escritura no puede convertirse en una operación
arbitraria del cliente MCP.

## Decision

La superficie MCP inicial será estrictamente de solo lectura. Las tools podrán
consultar el HotSnapshot, el estado del índice, repositorios registrados,
símbolos, referencias, dependencias y referencias no resueltas.

No habrá tools MCP para escribir archivos del repositorio analizado, modificar
su configuración, crear índices dentro de ese repositorio, ejecutar comandos
arbitrarios ni mutar directamente el grafo canónico. Las actualizaciones del
índice serán operaciones internas controladas por el ciclo de indexación.

Cada respuesta relevante declara `snapshot_id`, `snapshot_age`, `total`,
`returned`, `truncated`, `next_cursor`, `exact_results`, `unresolved_related` y
`coverage` cuando el contrato de la tool lo requiera. Los cursores están ligados
al snapshot y se rechazan cuando dejan de ser válidos.

## Alternatives

- **MCP con operaciones de escritura:** ampliaría la superficie funcional, pero
  convertiría a Kivgraph en un agente con riesgo de modificar código o estado del
  usuario.
- **Permitir SQL/Cypher arbitrario:** dificultaría límites de recursos,
  estabilidad del contrato y aislamiento de datos.
- **Responder sin metadatos de límites:** simplificaría respuestas, pero
  ocultaría truncamientos, edad del snapshot y cobertura real.

## Consequences

- El cliente puede tratar las respuestas como consultas sin efectos laterales
  sobre los repositorios analizados.
- La superficie MCP debe validar argumentos, limitar recorridos y clasificar
  errores sin filtrar detalles internos innecesarios.
- El indexador y el almacenamiento interno requieren controles separados del
  servidor MCP.
- Añadir una operación mutante exigiría cambiar esta decisión mediante un ADR y
  una revisión de seguridad.

## Risks

- Aunque la tool sea read-only, una dependencia con efectos secundarios podría
  escribir en el filesystem; los tests deben verificar el comportamiento y las
  rutas permitidas.
- Consultas sin límites pueden agotar CPU o memoria; cada recorrido tendrá
  límites y paginación.
- Un snapshot viejo puede parecer correcto; las respuestas deben declarar su
  edad y disponibilidad.

## Status

Aceptada para la superficie MCP inicial.
