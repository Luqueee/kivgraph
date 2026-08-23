# ADR 0063: El veredicto de `completeness`, y el contador que infló

- **Estado:** aceptada
- **Fecha:** 2026-08-23
- **Cambia el protocolo MCP:** sí -- añade `completeness` a seis tools y corrige
  el valor de `coverage.unresolved_related` en cinco
- **Cambia el schema persistente:** no
- **Obliga a reconstruir:** no -- el veredicto se calcula desde filas que la
  generación ya guarda

## Contexto

`LUQUE-2206`, `LUQUE-2212`. `internal/mcp/instructions.go` le dice a todo
agente: «Read confidence and **completeness** before treating an empty or
partial answer as proof of absence». Hasta la fase 23 sólo una tool de once
emitía ese campo, así que la instrucción pedía leer algo que casi nunca estaba.

Una respuesta vacía es la más cara del producto: es la que se lee como prueba.
`find_symbol` sin filas dice «ese nombre no existe» y manda a un agente a
inventarse el símbolo; `find_references` sin filas dice «nadie lo llama» y
autoriza a borrar código vivo. Ninguna de las dos podía saberlo.

## Decisión

Seis tools publican `completeness`, y **lo que acota una respuesta no es la
misma pregunta para todas**:

|tool|qué puede acotarla|cuándo se emite|
|---|---|---|
|`find_references`|fallos que pidieron ese nombre, más ámbitos ilegibles del repositorio del sujeto|siempre|
|`get_blast_radius`|el mismo par, desde el repositorio donde arranca el recorrido|siempre|
|`trace_dependencies`|fallos que **el símbolo mismo** hizo, más ámbitos de su repositorio|siempre|
|`find_cross_repo_consumers`|ámbitos ilegibles de **todo** el grafo, a propósito|siempre|
|`find_symbol`|ámbitos ilegibles; `repo` acota a los de ese repositorio|vacía, truncada o `LOWER_BOUND`|
|`get_file_outline`|ámbitos ilegibles del repositorio preguntado|vacía, truncada o `LOWER_BOUND`|

Tres decisiones que no eran obvias:

* **El ámbito sigue a la pregunta.** Un veredicto cobrado por cada punto ciego
  del grafo diría `LOWER_BOUND` en toda respuesta de un corpus con un solo
  paquete ilegible, y un veredicto que nunca dice `COMPLETE` no informa de nada.
  `find_cross_repo_consumers` es la excepción deliberada: un paquete que nadie
  pudo leer en **ningún** repositorio es exactamente donde se esconde un
  consumidor de fuera.
* **La pregunta hacia fuera necesita otros fallos.** «¿Quién llama a esto?» está
  acotada por los que pidieron el nombre; «¿qué alcanza esto?», por lo que el
  símbolo no pudo resolver. Una llamada ilegible de otro esconde un llamante, no
  una dependencia.
* **Las cinco tools que no comprueban no afirman ninguna ausencia.**
  `get_symbol` y `get_source` rechazan un símbolo que no encuentran en vez de
  devolver lista vacía; `graph_status`, `list_repositories` e `index_project`
  contestan sobre el índice.

## El defecto que traía desde el nacimiento

`completenessFor` devolvía `namingTotal + scopeTotal`, y ese segundo valor es
`coverage.unresolved_related`. Nació así en `5960312` (`2026-08-12`), sobre
`find_references` y `get_blast_radius`; la fase 23 (`308e802`) extendió el mismo
helper a cuatro tools más y en `find_symbol` **sustituyó** un contador que sólo
contaba nombres -- `snapshot.UnresolvedNamingSymbol(name, 0)`.

Medido con el binario real sobre este repositorio: `find_symbol` de un nombre
que nadie referencia informaba de `unresolved_related: 29`, y los `29` eran
entradas `DECLARATION_OUTSIDE_REPOSITORY` de cgo que no nombran nada parecido.

Los cuatro contadores de `coverage` están documentados como **disjuntos y sobre
la propia consulta**. Un ámbito ilegible no toca la consulta: es *por qué* la
respuesta puede ser corta, no evidencia sobre lo que se preguntó, y ya se
informa entero en `completeness.invisible_scopes` y `more_invisible_scopes`.

Lo delator es que ya estaba visto en un sitio: `find_cross_repo_consumers`
descartaba el valor con su propio comentario -- «adding them twice would inflate
the only number a caller can audit»-- mientras las otras cinco lo sumaban.

**Cambiar lo que un contador cuenta es un cambio de esquema aunque el campo no
cambie de nombre ni de tipo, y el compilador no lo ve.** Las cuatro puertas
seguían en verde porque ningún test fijaba el contador sobre un fallo que
**sólo** fuese de ámbito.

## La corrección, y qué ve un cliente

* `completenessFor` devuelve `namingTotal`; `completenessOutwardFor`, el
  `sourcedTotal` del símbolo. Los ámbitos siguen en el veredicto.
* `completenessScopes` **no devuelve contador**: todo lo que encuentra es
  ámbito.
  `get_file_outline` vuelve a `Coverage{Exact: kept}`, que es lo que publicaba
  antes de la fase 23.
* En las cinco tools afectadas `unresolved_related` **baja**. Nada se pierde: en
  la medición, los `29` aparecen donde corresponde -- `20` listados más `9` en
  `more_invisible_scopes`.
* La vista `full` sigue escribiendo el campo con su cero, así que ninguna
  captura de esa vista deja de ser exacta.

No hay migración que hacer y el schema persistente no se toca: el veredicto se
calcula al responder, desde las filas `UNRESOLVED` que la generación ya guarda.
Un cliente que leyera `unresolved_related` como «cosas que nombran mi símbolo»
recibe ahora ese número y antes recibía uno mayor; ningún cliente podía usar el
anterior para nada correcto.

## Consecuencias

* La instrucción del servidor deja de pedir un campo que casi nunca viajaba.
* `guidance` gana una rama por veredicto en las tools que comprueban: una
  respuesta vacía con `LOWER_BOUND` manda a `blind_spots` y a
  `invisible_scopes` en vez de afirmar la ausencia.
* La documentación de la superficie deja de tener el bloque copiado: la
  semántica compartida vive en `mcp/usage.md` con la tabla de arriba, y las
  páginas de tool enlazan. Tres páginas afirmaban lo contrario de lo que hacía
  el código y se corrigieron.
* Queda un hueco declarado, de la fase 24: `hotsnapshot.EvidenceRecord` no
  proyecta la posición de una evidencia, así que ninguna tool puede abrir el
  texto de una evidencia hoy. No lo pide ningún consumidor.

## Verificación

* `TestBlastRadiusVerdictTurnsOnOneRecordedFailure`: el mismo grafo contesta
  `COMPLETE` sin nada registrado y `LOWER_BOUND` con un solo fallo, para que el
  veredicto no pueda ser una constante.
* `TestCompletenessSeparatesAFailedReferenceFromAnUnreadableScope`: un fallo que
  **sólo** es de ámbito da `LOWER_BOUND`, no aparece como referencia fallida y
  deja `unresolved_related` en `0`. Falsificado volviendo a sumar los ámbitos:
  falla con `unresolved_related = 1, want 0`.
* `TestFindSymbolScopesTheVerdictToTheRepositoryAsked`: una búsqueda acotada no
  hereda el punto ciego de otro repositorio, que es lo que impide que el
  veredicto sea constante.
* `benchmarks/tool-honesty`: conduce el binario real por MCP contra un corpus
  con puntos ciegos a propósito, en dos lenguajes, y comprueba que el ámbito se
  lee del grafo servido y no del fixture. `18` comprobaciones.
* El binario real sobre este repositorio, antes y después:
  `unresolved_related: 29` -> `coverage` vacío, con los `29` ámbitos en el
  veredicto.
