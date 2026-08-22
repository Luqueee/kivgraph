# ADR 0060: El receptor de un método es un hecho del grafo

- **Estado:** aceptada
- **Fecha:** 2026-08-22
- **Completa:** el borde declarado por el [ADR 0059](0059-el-alcance-de-un-contenedor-incluye-el-de-sus-miembros.md)
- **Cambia el schema:** `CanonicalSchemaVersion` `3` -> `4`

## Contexto

`LUQUE-2010`. El ADR 0059 hizo que el alcance de un contenedor incluyera el de
sus miembros, y derivó la contención del **rango de líneas** porque es un hecho
estructural. Ese mecanismo cubre lo que se escribe *dentro* de la declaración y
no puede cubrir lo que se escribe fuera:

|forma|¿los miembros caen en el span?|
|---|---|
|clase de TypeScript|**sí** -- los métodos van entre sus llaves|
|`struct` de Go|sus campos sí; sus métodos no -- `func (h *T) M()` se declara fuera|
|`struct` de Rust|sus campos sí; sus métodos viven en un `impl`, que no se publica|

Así que el alcance de un tipo Go o Rust excluía el de sus métodos **mientras la
respuesta de TypeScript parecía idéntica y sí era completa**. Eso es lo que el
ADR 0059 declaró y no arregló, y es exactamente el defecto que su propia nota
describía como inaceptable: la diferencia estaba escrita en un ADR y no en la
respuesta.

Medido sobre `kena`, con `depth: 1`:

|sujeto|antes|después|
|---|---|---|
|`GuildsHandler` (Go, 9 métodos)|`3` filas, `167` tok|`53` filas, `1.613` tok|
|`state::memory::MemoryStateStore` (Rust, dos `impl`)|`2` filas, `151` tok|`18` filas, `606` tok|

## Decisión

Se añade `METHOD_OF` al vocabulario de aristas: del método al tipo que lo
declara como receptor. Es contención, no un uso.

La pareja se **observa**, nunca se deletrea. En Go la emite el receptor que
`go/types` resolvió, que el cargador ya calculaba en `Definition.Owner` y nadie
consumía. En Rust es el parámetro de tipo del descriptor `impl` que emite
`rust-analyzer`, cualificado en el cargador -- donde están los descriptores-- y
no recortando `::impl::` del nombre ya renderizado. El nombre punteado
`GuildsHandler.Get` **no** es evidencia: es una convención del generador de
nombres, y es el terreno que el ADR 0059 se negó a pisar.

Cada arista lleva evidencia observada en un `File`, que es la declaración del
propio método: es donde el receptor está escrito. El constructor del snapshot
rechaza una arista sin observación, y esta no tenía ninguna excusa para ser una
excepción.

En Rust el miembro se empareja **con el tipo, no con el bloque**. El bloque es
sintaxis real y no un nodo del grafo desde LUQUE-2008, y saltárselo evita
revertir esa decisión: el tipo es lo que un llamante pregunta.

`METHOD_OF` queda **fuera del vocabulario de referencias**. Un tipo no está
referenciado por sus propios métodos, así que `find_references` lo ignora por
construcción -- su lista de kinds es blanca, no negra.

## Consecuencias

`CanonicalSchemaVersion` pasa de `3` a `4`: cada kind semántico tiene
exactamente una tabla de relación, así que un kind nuevo es una tabla nueva. El
DDL `schemas/ladybug/004-canonical.cypher` y `docs/storage/canonical-schema.md`
se generan desde la misma metadata. El único camino de indexado es la
reconstrucción completa (ADR 0057), así que no hay migración que escribir: una
base con `schema_version=3` se rechaza al abrirse, que es el comportamiento ya
documentado.

`containedMembers` suma ahora las dos fuentes y deduplica. Las lee las dos
siempre: un `type T struct{}` de una línea no tiene interior que buscar y sí
tiene métodos, así que la guarda del span dejó de cortar el camino de aristas.

## Verificación

La verdad se construyó **sin preguntar al contenedor**: la unión de lo que
alcanza la declaración del `struct` y lo que alcanza cada uno de sus nueve
métodos, medida con el binario anterior, menos la raíz y sus propios miembros
-- que nunca son filas, por contrato. Son `53`. La respuesta del contenedor son
`53`, sin faltar ni sobrar ninguna: `P=1,00`, `R=1,00`.

`TestTraceDependenciesFollowsMethodsDeclaredOutsideTheType` fija el contrato
sobre la forma que derrota al span dos veces: un `struct` de una línea, sin
interior, cuyos métodos se declaran después.

## Limitaciones

`get_blast_radius` no cambia. La pregunta entrante -- «qué se rompe si cambio
esto» -- no se contesta con contención: los métodos de un tipo no son sus
consumidores. Si algún día lo pide una medición, será otra decisión y no un
efecto lateral de esta.

Un miembro cuyo tipo no es un símbolo publicado no se empareja con nada en vez
de emparejarse con una suposición. En Go el caso no existe -- el receptor vive
en el paquete del método-- y en Rust cubre un `impl` sobre un tipo ajeno.
