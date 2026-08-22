# ADR 0059: El alcance de un contenedor incluye el de sus miembros

- **Estado:** aceptada
- **Fecha:** 2026-08-21
- **Reemplaza:** el mecanismo del [ADR 0058](0058-una-travesia-declara-los-miembros-que-no-sigue.md)

## Contexto

`LUQUE-2004`. Preguntar el alcance de una clase devolvía lo que la declaración de
la clase referencia y **no** lo que sus propios métodos alcanzan. Medido como `X4`
del conjunto `reach`, sobre `RecommendationsCache` en `library-shared`: la clase
llegaba a `base-cache.ts` y paraba, mientras que el tipo que sus dos métodos
nombran por un `import type` sólo se alcanzaba preguntando por el método.
`P=1,00`, `R=0,50`.

La causa no es una arista que falte: la arista cuelga del método, porque es el
método quien nombra el tipo. Lo que no existe es una arista clase -> método. La
contención no es una relación entre símbolos en este grafo -- `DEFINES` va de un
fichero a un símbolo, y `PART_OF` es la relación `part`/`library` de Dart-- así
que una travesía enraizada sólo en la clase no tenía nada que caminar.

El ADR 0058 respondió **declarándolo**: la respuesta nombraba los miembros que no
podía seguir. Eso convirtió un error invisible en un borde visible, que era el
paso correcto, y dejó la pregunta abierta: seguirlos o no.

## Decisión

Se siguen. **El alcance de una declaración es el alcance de lo que su propia
fuente nombra**, y el cuerpo de un método está dentro de esa fuente. Un lector que
pregunta de qué depende una clase quiere las dos aristas.

La travesía siembra el contenedor **y sus miembros a profundidad cero**, en un
único BFS:

1. **A profundidad cero, no uno.** Un miembro es contenido, no una dependencia, así
   que no puede costar un salto: el tipo que nombra un método está a profundidad
   `1` de la clase, no a `2`.
2. **Un solo BFS, no uno por miembro.** `TraverseFrom` acepta varias semillas.
   Correr una travesía por miembro y fusionar contaría dos veces un nodo que dos
   miembros comparten, y le pondría la profundidad del miembro que corriera
   primero.
3. **Las semillas no son filas.** `dependencyNodes` ya descarta la visita cuya
   `Source` es `InvalidSymbolID`, que es exactamente toda semilla. La clase y sus
   miembros no aparecen como dependencias de sí mismos.
4. **Sólo siembra la capa más externa.** Un parámetro vive dentro de su método y su
   tipo es alcance que el método ya explica. Sin esta condición la respuesta
   sobre `RecommendationsCache` sembraba también `setResults.data`, un argumento.

`members_not_followed` y su `guidance` se retiran: la respuesta ahora **lleva** lo
que antes sólo nombraba. El campo vivió unas horas y su motivo desapareció con
este cambio; dejarlo vacío para siempre sería peor que retirarlo.

## Consecuencias

Medido con el mismo corpus y el mismo estado, cambiando sólo el binario:

|pregunta|tokens|`R`|
|---|---|---|
|`X1` consumidores TS|`530` -> `530`|`1,00` -> `1,00`|
|`X2` consumidores Go, ausencia|`112` -> `112`|`1,00` -> `1,00`|
|`X3` alcance de una función Go|`305` -> `305`|`1,00` -> `1,00`|
|**`X4` alcance de una clase TS**|**`233` -> `403`**|**`0,50` -> `1,00`**|

`+170` tokens en la única pregunta que cambia y **cero** en las otras tres: una
función y un método no declaran miembros, así que no hay nada que sembrar. El
conjunto entero sube `14,4 %` y pasa de `3/4` a `4/4` exactas.

La cota de `max_nodes` sigue siendo la que protege de un contenedor enorme, y
`traversal_truncated` sigue diciéndolo. Un `struct` Go con nueve métodos medido
sobre `kena` -- `GuildsHandler` -- responde `3` nodos y `167` tokens, no una
explosión, y por un motivo que conviene leer entero en la limitación de abajo.

## Limitación declarada: en Go y en Rust un método no está dentro del tipo

La contención se deriva del **rango de líneas**, que es un hecho estructural. Y
eso sólo cubre a los miembros que viven léxicamente dentro de la declaración:

|forma|¿los miembros caen en el span?|
|---|---|
|clase de TypeScript|**sí** -- los métodos van entre sus llaves|
|`struct` de Go|sus **campos** sí; sus **métodos** no -- `func (h *T) M()` se declara fuera|
|`struct`/`enum` de Rust|sus campos sí; sus métodos viven en un `impl`, que además no se publica (ADR 0058, LUQUE-2008)|

Así que el alcance de un tipo Go o Rust **sigue excluyendo el de sus métodos**, y
este ADR no lo arregla. `GuildsHandler` alcanza los tipos de sus tres campos y
nada más. Cubrirlo exige otra cosa -- una arista de contención, o emparejar por
receptor-- y es una decisión aparte con su propia medición: queda en
`LUQUE-2010`.

Se declara aquí en vez de dejarlo implícito porque una respuesta que parece
completa y no lo está es exactamente el defecto que este ADR viene a cerrar.

## Alternativas descartadas

- **Dejarlo declarado, como el ADR 0058.** Es honesto y era el paso correcto para
  no publicar un error en silencio, pero le da al llamante trabajo -- una llamada
  por miembro-- para obtener lo que la pregunta ya pedía. `+170` tokens en una
  respuesta cuestan menos que dos llamadas más.
- **Cobrar un salto por el miembro.** A profundidad `1` la respuesta serían los
  métodos de la clase, que no son dependencias sino contenido, y el tipo quedaría
  a `2`. Contesta una pregunta que nadie hizo.
- **Sintetizar una arista clase -> método.** Metería en el grafo canónico una
  arista que ningún checker afirmó, contra el contrato de la raíz. La contención
  se deriva de un rango de líneas, no se observa.
- **Emparejar por nombre cualificado punteado.** `RecommendationsCache.getResults`
  parece decirlo, pero es una convención del generador de nombres. El span es un
  hecho.
