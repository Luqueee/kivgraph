# ADR 0058: Una travesía declara los miembros que no sigue

- **Estado:** aceptada
- **Fecha:** 2026-08-21

## Contexto

`LUQUE-2004`. Preguntar el alcance de una clase devolvía lo que la declaración de
la clase referencia y **no** lo que sus propios métodos alcanzan, sin decir que se
había parado ahí.

Medido en `benchmarks/graph-tools-comparison/reach.md`, pregunta `X4`, sobre
`RecommendationsCache` en `library-shared` del corpus `kena`:

|pregunta|alcanza|
|---|---|
|la clase, profundidad `1`|sólo `src/redis/cache/base-cache.ts`|
|la clase, profundidad `2`|miembros de `BaseCache` y `RedisCacheClient.ts`, nunca el tipo|
|el método `getResults`, profundidad `1`|`src/redis/cache/music/types.ts`, vía `TYPE_USES`|

`P=1,00`, `R=0,50`.

La causa no es una arista que falte. La arista está bien puesta: cuelga del
**método**, porque es el método quien nombra `ChipbotRecommendationsResponse` en
su firma. Lo que no existe es una arista clase -> método. La contención no es una
relación entre símbolos en este grafo: `DEFINES` va de un fichero a un símbolo y
`PART_OF` es la relación `part`/`library` de Dart. Una travesía camina aristas, y
no había ninguna que caminar.

Así que el defecto nunca fue el conjunto pequeño -- fue que **parecía completo**.
Es la misma forma que el `H2` que cerró el ADR 0054 y el `H3` que cerró el ADR
0055: una respuesta coherente con su propio modelo, contestando una pregunta
distinta de la que se hizo, en silencio.

## Decisión

`trace_dependencies` **no cambia su travesía**. Cuando la raíz declara miembros
cuyas aristas salientes esta respuesta no ha caminado, los nombra: en
`members_not_followed` y en la `guidance`, con la instrucción de cómo pedirlos.

Un miembro se nombra sólo si cumple las tres condiciones:

1. **Está dentro del span de la raíz**, en su mismo fichero. La contención se
   deriva del rango de líneas, que es un hecho estructural que vale en los tres
   lenguajes, y no de una convención de nombres punteados.
2. **Alcanza algo fuera del span de la raíz.** Un miembro cuya única arista se
   queda dentro no aporta alcance nuevo y nombrarlo no diría nada.
3. **Es de la capa más externa.** Un parámetro vive dentro de su método, y su
   tipo es alcance que el método ya explica. Sin esta condición, la respuesta
   sobre `RecommendationsCache` nombraba `setResults.data` -- un argumento, que no
   es una pregunta que esta tool contesta.

La lista está acotada a `12` nombres: una clase de trescientos métodos
convertiría el aviso en lo más grande de la página, que es lo contrario de lo que
se busca.

El campo viaja en las **dos** vistas. La compacta es el valor por defecto, así que
un aviso que sólo llevara la vista completa es un aviso que nadie lee.

## Consecuencias

- La exhaustividad medida **no se mueve**: `X4` sigue en `R=0,50`, porque se mide
  sobre ficheros alcanzados y ésos son los mismos. Lo que se va es el silencio.
  Esa es la decisión: convertir un error invisible en un borde declarado, que es
  lo que el ADR 0046 hizo con la ambigüedad.
- Cuesta `70` tokens en la respuesta que lo necesita: `163` antes, `233` ahora.
  Las otras tres preguntas del conjunto no se movieron ni un token, porque una
  función y un método no declaran miembros con alcance propio.
- El esquema de una tool MCP crece con un campo opcional. `members_not_followed`
  se omite cuando está vacío, que es toda raíz hoja.
- `get_blast_radius` tiene el mismo borde en la dirección entrante y **no** se
  toca aquí. Queda declarado como límite conocido, no arreglado en silencio.

## Alternativas descartadas

- **Descender: incluir en la travesía lo que alcanzan los miembros declarados.**
  Es la respuesta que un lector quiere, y cambia respuestas ya publicadas. Habría
  que decidir si un miembro consume un salto de profundidad o es parte de la
  raíz, y qué se hace con un contenedor de trescientos miembros bajo el mismo
  `max_nodes`. Ninguna de las dos cosas se puede decidir sin medir su coste, y
  este ADR no las mide. Sigue abierta, y ahora se puede medir contra un borde que
  se declara en vez de contra uno que no se veía.
- **Sintetizar una arista de contención símbolo -> símbolo.** Metería en el grafo
  canónico una arista que ningún checker afirmó, contra el contrato de la raíz:
  una arista `EXACT` exige evidencia observada, y la contención se deriva de un
  rango de líneas. Un `CANDIDATE` no arregla nada y ensucia toda consulta de
  aristas.
- **Nombrar todos los miembros, sin la condición de alcance.** Más simple y peor:
  llenaría la respuesta de nombres que no llevan a ninguna parte, y el lector
  aprendería a ignorar el aviso.
- **Deducirlo por el nombre cualificado punteado.** `RecommendationsCache.getResults`
  parece decirlo, pero es una convención del generador de nombres y no un hecho
  observado. El span sí lo es.
