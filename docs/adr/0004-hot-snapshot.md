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

### Tabla de strings

El builder interna strings y al congelarse transfiere la tabla ordenada por
`InternedString` al snapshot. La tabla congelada admite lectura concurrente sin
bloqueos; su serialización conserva ese orden y rechaza datos truncados o
valores duplicados. Las cadenas, no sus IDs, son la representación durable.

### Identidad estable

La identidad externa de un símbolo se codifica canónicamente con la versión de
formato, lenguaje, repositorio, paquete o módulo, nombre cualificado, clase y
discriminador. Cada campo está prefijado por su longitud, de modo que valores
con separadores no pueden alterar sus límites. La `StableKey` es el BLAKE3-256
de esa representación, expresado como base32 sin padding; la identidad canónica
se conserva para auditoría. La posición de fuente no participa, por lo que mover
una declaración no cambia su identidad.

### Envelope del snapshot

`GraphSnapshot` encapsula metadatos con versión y timestamp, tablas densas,
ambas adyacencias, y los índices exactos por stable key, nombre, nombre
cualificado y ruta de repositorio. Su constructor copia slices y mapas, valida
que los índices cubran exactamente sus tablas y no expone colecciones mutables.

### Adyacencia forward

El constructor CSR forward agrupa las aristas salientes por `SymbolID` con un
vector de offsets de longitud `símbolos + 1`. Conserva el orden de entrada para
aristas del mismo origen y rechaza orígenes o destinos fuera de la tabla; el
snapshot valida los offsets y las referencias a evidencia antes de publicarse.

### Adyacencia reverse

La CSR reverse se deriva de la forward validada: cada arista entrante conserva
evidencia, tipo, confianza, procedencia y flags, y cambia únicamente su destino
por el símbolo origen. La publicación compara ambas CSR como multisets exactos,
incluyendo duplicados, para impedir aristas colgantes o contrapartes inventadas.

### Construcción desde LadybugDB

El builder recibe filas canónicas de LadybugDB, copia y ordena cada colección
por su stable key, asigna IDs densos, interna strings, deriva ambas CSR,
construye índices exactos y entrega el snapshot únicamente tras la validación.
Los IDs de almacenamiento nunca se filtran a las filas de entrada ni sustituyen
las claves durables.

### Publicación atómica

`SnapshotStore` mantiene un `atomic.Pointer[GraphSnapshot]`. `Load` devuelve la
referencia completa que el lector usará sin bloqueos; `Publish` solo acepta un
snapshot no nulo con un ID estrictamente superior y usa CAS para que
publicadores concurrentes no puedan retroceder de generación. `Close` limpia el
puntero activo e impide publicaciones posteriores, sin invalidar referencias que
lectores ya conservaron. La construcción fallida nunca alcanza el puntero activo.

### Búsquedas exactas

Las tools consultan los mapas de stable key, nombre, nombre cualificado y
repo/path sin recorrer tablas ni aplicar coincidencias nominales. Los resultados
de nombre se devuelven en páginas acotadas (`limit` máximo 500), copiadas para
que el slice del lector no exponga almacenamiento del snapshot.

### Recorridos acotados

`Traverse` ejecuta BFS sobre CSR forward o reverse usando un array denso de
visitados indexado por `SymbolID`, no un mapa por nodo. El resultado incluye el
origen en profundidad cero, agrupa repositorios, filtra tipos de arista y
declara truncamiento por límite de nodos; una fecha límite vencida devuelve un
resultado parcial con error de timeout.

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
