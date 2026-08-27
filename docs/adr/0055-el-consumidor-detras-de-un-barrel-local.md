# ADR 0055: El consumidor detrás de un barrel local

- **Estado:** aceptada
- **Fecha:** 2026-08-21

## Contexto

`H3_ts_type` del conjunto duro preguntaba qué archivos usan el `ApiRuntimeState`
declarado en `libraries/library-shared/src/types/gateway-registry.ts`. La
respuesta tenía siete filas `TYPE_USES`, la página entera izaba
`repository: library-shared`, y **no había ni un uso de otro repositorio**.
`packages/gateway/src/grpc/manager/RegistryGrpcManager.ts` anota cuatro
posiciones con ese tipo y no aparecía. `P=1,00`, `R=0,50`.

El síntoma sugería un problema con los tipos y no lo era. En ese mismo fichero,
`import type { RedisAdapter } from "@private/shared"` resuelve perfectamente. Lo
que difiere es **la ruta**:

```ts
import type { ApiRuntimeState, … } from "../../types/registry.js";
```

y `src/types/registry.ts` es, entero,
`export type { ApiRuntimeState, … } from "@private/shared"`.

Sólo se creaba vinculación para un import que **nombra un paquete**. Con una ruta
relativa no se creaba ninguna, así que los cuatro usos no tenían a qué apuntar y
se caían enteros. Medido en el payload del worker:

|nombre|`imports`|`references`|
|---|---|---|
|`RedisAdapter`, import de paquete|`2`|`3`|
|`ApiRuntimeState`, import relativo|`0`|**`0`**|

El payload deja claro por qué esto no se arregla en `references`: una entrada de
`imports` lleva un `target` con `repository`, `package`, `file` y `source`; una
de `references` lleva `targetFile` y `targetQualifiedName`, **locales**. Los
destinos de otro repositorio viajan por `imports` por diseño, y el consumidor
apunta a su vinculación local. Faltaba la vinculación, no el canal.

Y había un agujero más ancho detrás: el recorrido sólo visitaba ficheros que ya
tenían un import de paquete resuelto, así que un fichero que llega a una
declaración ajena **sólo** por barrels locales no se miraba nunca.

## Decisión

Un import de ruta relativa cuyo nombre resuelve a una declaración que algún
barrel local de este proyecto ya resolvió **crea su vinculación**, con el
proveedor que ese barrel nombró.

- **La correspondencia es de identidad, no de nombre.** El checker colapsa una
  cadena de alias de una vez y aterriza en la declaración, no en la vinculación
  intermedia, así que los barrels se indexan por la declaración a la que
  resolvieron y el candidato se compara contra ella por `symbol.id`. El proveedor
  no se infiere aquí: es el que el resolutor de imports de paquete ya resolvió
  para el especificador del barrel.
- El consumidor queda **a un salto**, con su propia `IMPORTS_SYMBOL`, que es la
  forma que ya tienen los cinco consumidores de `R1`. Nada cambia en la consulta.
- El recorrido pasa a cubrir todos los ficheros del proyecto, porque un fichero
  así no nombra ningún paquete en su propio texto. `getSymbolAtLocation` acepta
  un lote, así que el checker se pregunta una vez por todos los nombres
  relativos encontrados.
- **Los ficheros bajo un `node_modules` no se recorren.** Un programa contiene
  el `.d.ts` de cada dependencia, y eso es la fuente del proveedor, no el uso que
  este repositorio hace de ella.

## Consecuencias

- `H3_ts_type` pasa de `1,00`/`0,50` a exacta, y el conjunto duro a `9/9` con
  `P=R=1,00`: `15` archivos afirmados, `0` falsos, `0` perdidos. Los siete
  publicados se remidieron con el mismo binario y siguen en `7/7`. Las cifras
  están en `benchmarks/graph-tools-comparison/harder.md`.
- **Un `9/9` no es una afirmación general.** Son nueve preguntas sobre un corpus,
  tres escritas a ciegas; es la ausencia de un fallo conocido en ese conjunto y
  nada más.
- El grafo crece: `126.934` símbolos contra `126.720`, y una `IMPORTS_SYMBOL` por
  cada nombre que un barrel local traía de otro repositorio.
- **Requiere reindexar, y con el worker correcto.** Las vinculaciones nuevas las
  produce el worker, no el binario, así que una medición con el shim instalado de
  una release anterior no ve nada. Pasó aquí: la primera pasada no movió `H3`
  aunque el worker del checkout ya lo arreglaba. Es la trampa que
  `ts-worker/AGENTS.md` ya documenta.

## Alternativas descartadas

- **Enseñar a `references` a llevar un destino de otro repositorio.** Duplicaría
  el canal de `imports` y exigiría subir la versión del protocolo, para expresar
  lo que ya se expresa.
- **Que el uso apunte a la vinculación del barrel y que la consulta lo
  atraviese.** Dos cambios pequeños en vez de uno, y el grafo diría algo más
  débil que en el caso de paquete: dos caminos distintos para el mismo hecho, y
  una respuesta que depende de un salto extra.
- **Emparejar por nombre o por firma en la capa de consulta.** Añadir una fila a
  una respuesta es una afirmación sobre el mundo, no una decisión de página como
  retirar una, y `AGENTS.md` prohíbe crear una arista `EXACT` por coincidencia.

## Lo que la corrección se dejó por el camino, y se arregló

Ensanchar el recorrido a todos los ficheros del programa metió los `.d.ts` de las
dependencias. Vincularlos nombró la **copia instalada** de `@private/shared` como
consumidora de un tipo que `@private/shared` declara, en dos repositorios a la vez,
con una ruta que se sale de la raíz del repositorio (`../../node_modules/…`).
`H3` marcó `0,50`/`1,00` durante una pasada. Es el mismo invariante que el ADR de
`internal/facts/golang.go` ya defiende: un hecho es evidencia del repositorio que
contiene su fichero. El conjunto duro lo detectó antes de publicarse.
