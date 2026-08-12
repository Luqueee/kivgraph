# Superficie MCP, versión 2

- Estado: aceptado
- Fecha: 2026-08-12
- Tareas: LUQUE-1113 y LUQUE-1114
- Sustituye a: PLAN.md 17.1 y 17.3 en lo que este documento contradice

Este documento describe la superficie que sirve `ladygraph serve`. Es el
contrato observable: lo que un agente recibe y en qué puede confiar.

## 1. Las diez tools

```text
list_repositories       find_symbol            get_symbol
get_file_outline        find_references        find_cross_repo_consumers
trace_dependencies      get_blast_radius       get_unresolved_references
graph_status
```

Una `serve` configurada añade `index_project`, la única mutación, con su puerta
de consentimiento. Diez es el techo de lectura y la razón está en PLAN.md 17.1:
un agente que carga esquemas bajo demanda deja de mirar una superficie grande.

## 2. Qué lleva una fila

Una fila lleva **lo que el agente puede usar**: un nombre, una ruta y una
línea. Con eso abre el fichero sin una segunda llamada.

```json
{
  "stable_key": "KHXAWFM5…", "name": "MergeAll", "qualified_name": "MergeAll",
  "kind": "func", "signature": "func(sets []facts.Set) facts.Set",
  "exported": true, "repository_name": "ladygraph",
  "file_path": "internal/facts/facts.go", "start_line": 459, "end_line": 484
}
```

Lo que **no** lleva por defecto:

- `canonical_identity`, que es la concatenación con `\0` de language,
  repository, package, qualified_name, kind y discriminator — todos ellos ya
  campos de la fila o la propia `signature`;
- los `*_key` cuyo valor deletrea la ruta o el nombre que tienen al lado.

`response_format: "detailed"` los devuelve. El valor por defecto es `"concise"`.

## 3. El sujeto se enuncia una vez

`find_references` devuelve un objeto, no una lista:

```json
{"subject": {…}, "direction": "incoming", "references": [{…}]}
```

El símbolo consultado es el argumento, no un resultado. Cada fila describe **el
otro extremo**: quien contiene la referencia en `incoming`, quien es alcanzado
en `outgoing`.

`start_line` de una fila es **la línea de declaración de ese símbolo**, no la
posición del token. El snapshot registra qué símbolo contiene una referencia y
nunca en qué punto: publicar una línea que nadie observó sería inventar
evidencia.

## 4. Entrar sin una clave estable

Dos puertas, para dos cosas que un agente ya tiene:

- `get_file_outline{repository, path}` acepta un fichero **o un directorio**.
  Es la misma pregunta a dos granularidades y la única forma de obtener claves
  estables sin acertar un nombre. Excluye miembros -campos, propiedades,
  variantes- salvo `include_members: true`: no son declaraciones entre las que
  se elige, son la forma del tipo de encima.
- `get_blast_radius` y `trace_dependencies` aceptan `qualified_name` en lugar
  de `stable_key`. Pasar los dos es un error; un nombre ambiguo devuelve
  `AMBIGUOUS_SYMBOL` con las claves entre las que elegir.

## 5. Hasta dónde llega una respuesta

Ésta es la parte que no tiene equivalente en otros servidores de este tipo.

Ladygraph se niega a resolver por coincidencia de nombre, así que sabe dónde
falló: cada fallo queda con su fichero, su línea y su motivo. Un índice que
adivina siempre devuelve algo y por eso no puede decir esto.

`get_blast_radius` publica un bloque `completeness`:

```json
{"verdict": "LOWER_BOUND",
 "blind_spots": [{"reason": "MODULE_PROVIDER_NOT_FOUND",
                  "file_path": "benchmarks/x/main.go", "start_line": 328,
                  "requested_symbol": "Connection.Close"}],
 "invisible_scopes": [{"reason": "PACKAGE_NOT_BUILDABLE", "detail": "…"}],
 "fallback": {"pattern": "\\bMergeAll\\b", "paths": ["benchmarks/x"]}}
```

- `COMPLETE` sólo cuando ningún fallo registrado intersecta la consulta por sus
  tres vías observables: el nombre solicitado, el paquete y el repositorio.
- `LOWER_BOUND` en cualquier otro caso, con las coordenadas.
- Un **blind spot** es una referencia que falló; un **invisible scope** es algo
  que el índice no pudo leer entero. Se distinguen por si el fallo nombra un
  fichero, no por su motivo.
- `fallback` es el `grep` acotado que cierra el hueco. Un aviso sin acción de
  recuperación obliga a un barrido completo, que cuesta más que no avisar.

`coverage.unresolved_related` se une de verdad a la tabla de no resueltos en
todas las tools. Un `find_symbol` que no encuentra nada y no declara
incertidumbre está afirmando que el nombre no existe.

## 6. Frescura

`list_repositories` y `graph_status` dicen, por repositorio, de qué commit y
rama se construyó el grafo y dónde está el árbol de trabajo ahora:

```json
{"indexed_commit": "e13b9ad…", "indexed_branch": "main",
 "current_commit": "91f13bf…", "current_branch": "main", "moved": true,
 "moved_detail": "indexed at commit e13b9ad on main, the tree is now at 91f13bf on main"}
```

`graph_status` añade `repositories_moved`. Un `HEAD` ilegible no se lee como
acuerdo: los campos `current_*` quedan vacíos, `moved` es `false` y el motivo
viaja en `moved_detail`.

`serve` resincroniza solo: observa `HEAD` y reconstruye cuando se mueve. Un
`push` no dispara nada -no toca el árbol ni mueve un ref local-; un commit
mueve `HEAD` sin cambiar contenido.

## 7. Coste del esquema

Ninguna tool publica el `outputSchema` que el SDK deriva de su tipo de retorno:
eran `30.334` de `34.932` caracteres de la superficie, contra `2.530` de los
`inputSchema`. Todas publican `{"type":"object"}`. El resultado sigue tipado y
sigue viajando en `structuredContent`; sólo se recorta la descripción previa.

`TestServerSurfaceStaysCheapToLoad` fija el techo en `8.000` caracteres.

## 8. Códigos de error

Los de PLAN.md 17.5, sin cambios. `get_file_outline` usa
`REPOSITORY_NOT_FOUND` para un repositorio que no está en el grafo y
`SYMBOL_NOT_FOUND` para una ruta que no existe bajo él: una página vacía se
leería como «aquí no hay nada declarado», que es una respuesta distinta.
