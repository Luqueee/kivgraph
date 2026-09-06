# Superficie MCP, versión 3

- Estado: aceptado
- Fecha: 2026-08-13
- Tareas: LUQUE-1113, LUQUE-1114 y la fase 19 (`LUQUE-1901` a `LUQUE-1905`)
- Sustituye a: PLAN.md 17.1 y 17.3 en lo que este documento contradice

Este documento describe la superficie que sirve `kivgraph serve`. Es el
contrato observable: lo que un agente recibe y en qué puede confiar.

Las cifras que aparecen aquí las mide `benchmarks/mcp-token-cost`, con su digest
y su generación. Ninguna se declara a mano.

## 1. Las doce tools

```text
list_repositories       find_symbol            find_by_intent
get_symbol              get_file_outline       find_references
find_cross_repo_consumers                      trace_dependencies
get_blast_radius        get_source             graph_status
find_implementations
```

Una `serve` configurada añade tres controles de indexado:
`index_project`, `start_index_project` y `get_index_status`. Las dos primeras
mutan tras la misma puerta de consentimiento; la tercera sólo consulta el
estado de una operación.

Todas las consultas aceptan `profile` como lista y omitirla usa el perfil por
defecto. Todas aceptan varios nombres o `["*"]`;
`graph_status` y `list_repositories` descubren todos cuando se omite. Una
selección múltiple sustituye `snapshot_id` por `profiles`, fija
`cross_profile_edges: "not_resolved"`, añade el perfil a cada fila y conserva
la completitud más débil. Las filas idénticas se deduplican por `stable_key` y
payload y enumeran los perfiles que las aportaron. El cursor queda ligado al
conjunto canónico de nombres y generaciones. Una `stable_key` exige exactamente
un perfil cuando la instalación contiene varios.

`index_project` y `start_index_project` aceptan en cambio un `profile` string:
omitirlo escribe en el perfil por defecto y un nombre inexistente crea su
directorio antes de la reconstrucción.

`start_index_project` devuelve un `operation_id` sin sostener la llamada durante
la reconstrucción. `get_index_status` lo consulta y responde `working`,
`completed` o `failed`, con el último progreso observado y el resultado o fallo
terminal. El estado se comparte entre sesiones de un mismo daemon, conserva
como máximo 32 operaciones terminadas y no sobrevive un reinicio. Ver ADR 0114.

`get_unresolved_references` **salió** de la superficie del modelo: responde una
pregunta sobre el índice y no sobre el código, y cada tool de la lista cuesta
descripción residente en cada petición de cada sesión. Sigue disponible desde el
CLI.

**Sin generación publicada no hay superficie.** El servidor completa el
handshake, publica cero tools de consulta y pone el comando de reconstrucción en
`instructions`. Un cliente lanza este proceso él mismo, así que salir se lee como
una caída; y publicar doce tools que contestan `INDEX_NOT_READY` a todo enseña al
agente que las tools no funcionan.

**Salvo que se pida lo contrario.** `kivgraph serve --introspection` publica el
catálogo entero sin generación. No es para un cliente: el argumento de arriba
sigue siendo cierto para el agente que va a llamar. Es para el inspector, el
registro o la herramienta de desarrollo que sólo puede leer lo que devuelve
`tools/list`, y que ante un handshake vacío no tiene nada que puntuar.

Lo que la opción cambia es qué se **lista**, y nada más. No crea un índice, no
fabrica un grafo vacío, no relaja ninguna comprobación de espacio en disco y no
toca la puerta de consentimiento de las dos mutaciones. Las doce tools de
consulta del grafo que expone siguen contestando `INDEX_NOT_READY` hasta que
haya generación -- `graph_status`
es la excepción de siempre, porque la tool que explica por qué las demás se
niegan no puede negarse ella-- y el handshake sigue llevando las instrucciones
de reparación: decirle al cliente que hay grafo cuando no lo hay sería la única
mentira que esta opción no puede permitirse. Las definiciones son las mismas que
sirve una `serve` con generación, no una copia sólo de esquema: es lo que hace
que lo que se puntúa sea lo que se sirve.

Y no la hereda nadie: `serve` es la única superficie que la tiene. El demonio
construye un servidor por sesión aceptada y nada pidió introspección ahí. Una
`serve` que se relaya al demonio tampoco la aplica, porque no construye servidor.

## 2. Qué lleva una fila

Una fila lleva **lo que el agente puede usar y con qué pedir la siguiente**: un
nombre cualificado, un repositorio, una ruta y un rango de líneas. Con eso abre
el fichero, y con eso nombra el símbolo en la llamada siguiente.

El campo se llama `repository` en todas las tools y lleva el **nombre** del
repositorio, que es lo que acepta el selector de la sección 4. Ese es el punto de
que una fila sea direccionable: se copia tal cual a la llamada siguiente. Un
`repository_key` -`repository:kivgraph`- obligaría a quitar un prefijo que
ninguna respuesta explica, y `repository_name` obligaría a renombrar la clave; las
dos formas existieron en el código y ninguna cumplía este documento.

```json
{
  "name": "MergeAll", "qualified_name": "MergeAll", "kind": "func",
  "signature": "func(sets []Set) Set", "exported": true,
  "repository": "kivgraph", "file_path": "internal/facts/facts.go",
  "start_line": 484, "end_line": 509
}
```

Lo que **no** lleva por defecto:

- la `stable_key`. Son 35 tokens de base32 que sólo este servidor lee, y la mitad
  del coste de un outline de 155 declaraciones. La tripleta de la sección 4 la
  sustituye;
- `canonical_identity`, que es la concatenación con `\0` de language, repository,
  package, qualified_name, kind y discriminator — todos ya campos de la fila;
- los `*_key` cuyo valor deletrea la ruta o el nombre que tienen al lado;
- el camino del paquete en una firma, cuando el símbolo pertenece a ese paquete:
  dentro de `internal/facts` se publica `func(sets []Set) Set`, que es como se lee
  el fuente que lo declara.

`response_format: "detailed"` devuelve todo eso, incluida la firma completamente
cualificada. El valor por defecto es `"concise"`.

**`end_line` viaja siempre.** Sin él una fila obliga a un `get_symbol` antes de
poder abrirse, que son quince llamadas de apoyo en las seis preguntas del arnés.

**Un solo canal por respuesta.** Ninguna tool publica `outputSchema` ni rellena
`structuredContent`: el SDK, en cuanto hay esquema, marshala el resultado tipado
y además repite el mismo JSON en el bloque de texto. Eran 24.066 bytes duplicados
en una pasada del arnés. Un cliente que renderice los dos paga la respuesta dos
veces; Oh My Pi tira el estructurado.

**Una respuesta dice lo que su recuento significa.** El campo `guidance` aparece
sólo cuando la cifra engaña: cero filas se lee como «no existe» salvo que algo
diga que es una ausencia comprobada, y una página truncada no dice si contiene lo
que importaba. Con filas y sin truncar, calla.

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

## 4. Nombrar un símbolo sin su clave

Toda tool que recibe un símbolo acepta **una de dos cosas, nunca las dos**:

- `stable_key`, la identidad canónica y durable;
- `qualified_name`, opcionalmente estrechado con `repository` y `path`. Es la
  tripleta que toda fila de esta superficie lleva, así que la llamada siguiente se
  construye con la respuesta que ya se tiene.

`path` es relativo al repositorio y por tanto exige `repository`. Pasar una clave
junto a cualquiera de los otros tres es un error: pueden discrepar, y contestar a
uno es contestar una pregunta que nadie hizo.

Un nombre que resuelve a varios símbolos **no se resuelve en silencio**, y lo que
el error ofrece depende de lo que quede por estrechar:

```text
qualified name "pkg.Merge" names 2 symbols; narrow with repository and path:
  alpha src/set.go:10-20, beta src/set.go:5-9
```

Una vez dados repositorio y ruta, si el nombre sigue apareciendo dos veces sólo la
clave los separa y el error las lista. Y un nombre que la restricción excluyó no
se lee igual que uno que nadie declara: el mensaje remite a buscar en todo el
grafo.

`get_file_outline{repository, path}` acepta un fichero **o un directorio**, agrupa
por fichero y excluye miembros -campos, propiedades, variantes- salvo
`include_members: true`: no son declaraciones entre las que se elige, son la forma
del tipo de encima.

## 5. `get_source`, la única tool que contesta en prosa

Devuelve el código de varios símbolos en una llamada. Y lo devuelve como texto,
no dentro del envoltorio JSON, porque medido sobre una declaración de 26 líneas
302 tokens de fuente son 374 como cadena JSON -cada salto de línea y cada
tabulador se pagan dos veces- y 430 como fila completa, que es exactamente lo que
cuesta la lectura de rango del anfitrión. Servir código a través del envoltorio no
compra nada.

```text
snapshot 24  2 bodies  context 0
@ kivgraph internal/facts/facts.go:484-509 func MergeAll
func MergeAll(sets []Set) Set {
…
@ kivgraph internal/indexer/full.go:649-671 func mergeSets [file changed, re-anchored +3]
…
! kivgraph src/other.go other.Other — the file changed and no declaration of "Other" remains in it
```

La frescura viaja con los bytes. Cada `File` del grafo lleva el SHA-256 de lo que
se analizó: si coincide, el rango del grafo es autoridad; si no, **el fichero es
autoridad** -es lo que el agente va a editar-, la declaración se reancla por
nombre y el desplazamiento se declara. Si no queda o queda dos veces, esa fila no
da bytes y dice por qué, y las demás se sirven igual. Reanclar no crea ninguna
arista: ver ADR 0040.

`context_lines` es opcional, por defecto `0` y como máximo `100`. Nunca se lee
fuera de la ruta del repositorio ni a través de un componente symlink, con la
misma función que usa la capa de indexación.

## 6. Hasta dónde llega una respuesta

Ésta es la parte que no tiene equivalente en otros servidores de este tipo.

Kivgraph se niega a resolver por coincidencia de nombre, así que sabe dónde
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

## 7. Frescura

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

## 8. Coste del esquema

Ninguna tool publica `outputSchema`. Al principio eran `30.334` de `34.932`
caracteres de la superficie contra `2.530` de los `inputSchema`, y se sustituyeron
por `{"type":"object"}`; después se retiraron del todo, porque el esquema es lo
que hace al SDK duplicar cada respuesta en `structuredContent`. Un esquema que se
anuncia y no se rellena describe una respuesta que no se envía.

**Lo que un anfitrión mantiene residente no es el esquema.** Oh My Pi monta cada
tool como un dispositivo cuya documentación se lee bajo demanda; Claude Code
difiere los esquemas detrás de su búsqueda de tools e inyecta `instructions` al
abrir la sesión. Lo residente es el nombre, dos veces, y la descripción: `716`
tokens -- `220` de enrutado y `496` de descripciones-- para las once de consulta
más `index_project`, medido por el arnés sobre la generación `000206`, frente a
`2.104` de esquema diferido.

Ahí es donde vive el enrutado, y por eso cada descripción dice contra qué
alternativa nativa compite y **dónde pierde**. Nada de eso puede llevar un número
derivado del grafo: reescribiría bytes del prompt de sistema de un cliente en cada
reindexado e invalidaría su caché.

`TestServerSurfaceStaysCheapToLoad` fija el techo del esquema en `8.000`
caracteres y falla si una tool vuelve a publicar `outputSchema`;
`TestServerSurfaceStaysCheapToKeepResident` fija el residente en `1.900` bytes y
falla si una descripción contiene un dígito.

## 9. Códigos de error

Los de PLAN.md 17.5, sin cambios. `get_file_outline` usa
`REPOSITORY_NOT_FOUND` para un repositorio que no está en el grafo y
`SYMBOL_NOT_FOUND` para una ruta que no existe bajo él: una página vacía se
leería como «aquí no hay nada declarado», que es una respuesta distinta.

## Implementaciones tipadas

`find_implementations` consulta relaciones `IMPLEMENTS` y `OVERRIDES`, con evidencia
declarada o estructural de TypeScript. Devuelve `results.subject` e
`results.implementations`, identidades canónicas, generación, cursor y
completitud. Los filtros `repo`, `language`, `paths` y `detection` se aplican
antes de paginar. Una generación anterior al esquema `5` devuelve `LOWER_BOUND`,
y una del esquema `5` también lo devuelve cuando quedan ámbitos sin resolver
registrados. Un `COMPLETE` vacío sólo demuestra ausencia dentro de ese corpus.
Véase ADR `0116` para el ámbito de tipos y la compatibilidad del protocolo.
