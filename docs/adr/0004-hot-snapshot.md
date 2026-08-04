# ADR 0004: HotSnapshot inmutable en memoria

- **Estado:** aceptada
- **Fecha:** 2026-08-04

## Context

Las consultas MCP deben responder con latencia baja y predecible. Consultar
LadybugDB, ejecutar compiladores o recorrer el filesystem en cada petición
introduciría trabajo variable en el fast path. A la vez, la indexación
incremental debe poder construir una versión nueva sin bloquear las lecturas
en curso.

## Decision

Luque construirá un `HotSnapshot` inmutable en memoria a partir del grafo
canónico. Las consultas solo leerán el snapshot publicado y usarán sus índices
densos, stable keys, CSR forward y CSR reverse.

La publicación se hará mediante un intercambio atómico de una referencia al
snapshot completo. Un lector conserva la referencia que obtuvo al comenzar la
petición; un builder prepara la siguiente versión fuera del fast path y la
publica solo cuando la verificación de integridad ha pasado.

Cada snapshot incluye un identificador, edad, contadores, tablas de símbolos,
relaciones, índices y metadatos suficientes para paginar y declarar límites.
Los IDs densos pueden cambiar entre snapshots; las stable keys permanecen como
identidad externa.

### Ciclo de vida de IDs densos

`RepositoryID`, `PackageID`, `FileID`, `SymbolID` y `EvidenceID` son `uint32`;
`EdgeID` es `uint64`. Son índices zero-based de tablas pertenecientes a un único
snapshot. El valor máximo de cada representación queda reservado como centinela
inválido, y el builder rechaza el overflow antes de truncar.

El builder posee un asignador privado durante la construcción y lo descarta al
publicar o abortar. Una reconstrucción reinicia la numeración en cero. Ninguna
tool, cursor, archivo durable ni protocolo intercambia estos IDs: las stable
keys son la única identidad externa persistente.

## Alternatives

- **Consultar LadybugDB directamente en cada tool:** reduciría duplicación de
  índices, pero haría la latencia dependiente del almacenamiento y complicaría
  los límites de recorrido.
- **Un snapshot mutable compartido:** ahorraría reconstrucciones, pero expone a
  los lectores a estados parciales y requiere bloqueos en cada consulta.
- **Bloquear consultas mientras se reconstruye:** simplificaría la publicación,
  pero viola el objetivo de disponibilidad y latencia del fast path.

## Consequences

- El consumo de memoria del snapshot debe medirse y limitarse.
- La reconstrucción y la publicación son operaciones separadas de las tools
  MCP.
- La inmutabilidad simplifica la concurrencia de lectura y elimina carreras
  durante una consulta.
- Un snapshot nuevo solo puede publicarse después de comprobar referencias,
  stable keys, ownership y ausencia de aristas colgantes.
- Los cursores deben incluir la identidad del snapshot y rechazar snapshots
  expirados.

## Risks

- Un snapshot completo puede ser demasiado grande para corpus de producción;
  los benchmarks fijarán límites y estrategias de compactación.
- Una publicación incorrecta puede exponer un grafo parcialmente construido;
  por eso la referencia pública se cambia una sola vez tras validar.
- Retener referencias durante consultas largas puede retrasar la liberación de
  memoria; las herramientas tendrán límites de recorrido y paginación.

## Status

Aceptada. La implementación y los benchmarks se realizarán en la fase de
HotSnapshot.
