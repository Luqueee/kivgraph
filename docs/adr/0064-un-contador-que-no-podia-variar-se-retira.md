# ADR 0064: Un contador que no podía variar se retira

- **Estado:** aceptada
- **Fecha:** 2026-08-23
- **Cambia el protocolo MCP:** sí -- `find_symbol` y `get_file_outline` dejan de
  publicar `coverage.exact`
- **Cambia el schema persistente:** no
- **Obliga a reconstruir:** no

## Contexto

`LUQUE-2215`. El ADR 0063 corrigió uno de los cuatro contadores de `coverage`.
Quedaban tres sin auditar, y la auditoría encontró que `exact` significaba **dos
cosas distintas según la tool**. Medido con el binario real sobre este
repositorio:

|tool|`total`|`returned`|`exact`|ámbito|
|---|---|---|---|---|
|`find_references`|`3`|`2`|`3`|toda la respuesta|
|`find_symbol`|`52`|`2`|`2`|la página|
|`get_file_outline`|`448`|`2`|`2`|la página|

Un cliente no podía escribir una sola regla. «Compara `exact` con `total` para
saber si te fías» funciona en `find_references` y en `find_symbol` da `2` de `52`
-- que parece que el `96 %` de la respuesta es dudosa cuando no lo es ninguna.

El defecto estaba publicado en la documentación: la página de
`get_file_outline` mostraba una captura con `total: 32`, `returned: 19`,
`exact: 19`.

Y el ámbito de página no era el defecto de fondo, sino su síntoma. Los cuatro
contadores clasifican **relaciones resueltas** por su confianza. `find_symbol` y
`get_file_outline` no devuelven relaciones: devuelven declaraciones de un
repositorio, y una declaración no tiene confianza que informar. Su `exact` era
por construcción igual al número de filas.

## Decisión

Retirarlo de esas dos tools. De las dos salidas posibles:

* **Hacerlo de respuesta**, como el resto, arregla la incoherencia y deja un
  campo **idéntico a `total`** en las dos tools. Un contador que no puede variar
  no es evidencia: repite un número que ya viaja dos posiciones antes, y se paga
  en cada respuesta. Es exactamente lo que `mcp/usage.md` da como razón de qué
  omite la vista compacta -- «una categoría que no contó nada».
* **Retirarlo** deja `total` y `returned` diciendo lo único que había que decir
  sobre una lista de declaraciones.

Es la misma decisión que el ADR 0063 tomó para `unresolved_related` en
`get_file_outline`, y por el mismo motivo: para una pregunta sobre un sitio, ese
contador no tenía nada que contar.

**No es global, y ahí está el matiz.** `get_source` parece el mismo caso y no lo
es: su cuenta dice cuántos cuerpos pudo servir de verdad, que es genuinamente
menor que `returned` cuando un fichero se movió bajo el índice -- y no viaja en
`coverage`, sino en la línea de cabecera de su respuesta en prosa. Y en
`find_cross_repo_consumers` los cuatro informan de cuatro cosas distintas
(`exact=1, candidate=1, unresolved_related=1, package_level=1`).

Después de esto `exact` tiene **un solo significado** en toda la superficie: las
cuatro tools de relaciones lo acumulan sobre la respuesta completa, con el
acumulador compartido `addReferenceCoverage`.

## La migración: la ausencia ya era legal

No hace falta ninguna, y no por suerte:

* Los contadores de la vista compacta llevan `omitempty`, y `Coverage.compact()`
  devuelve `nil` cuando los cuatro valen cero. La documentación ya decía que
  `coverage` está «absent entirely when all four are zero». Un cliente ya tenía
  que saber tratar la ausencia.
* La vista `full` sigue escribiendo los cuatro campos con sus ceros, que es su
  contrato -- «writes every field, `false`, `null` and zeros included». Un
  cliente de esa vista recibe `exact: 0`, y el cero dice la verdad: ninguna de
  las cuatro categorías aplica.
* Ningún consumidor interno leía el valor de esas dos tools. `observer.go` lee
  `UnresolvedRelated`, y `benchmarks/mcp-token-cost` lee `Exact` de
  `find_cross_repo_consumers`, que no se toca.

## Consecuencias

* `get_file_outline` en vista compacta no lleva `coverage`. En `full` lleva
  cuatro ceros.
* `find_symbol` en vista compacta lleva `coverage` sólo cuando
  `unresolved_related` no es cero; en `full`, los cuatro campos con `exact: 0`.
* `mcp/usage.md` dice ahora que los contadores son sobre **la respuesta y no la
  página**, y usa la captura de `trace_dependencies` -- `exact: 37` con
  `returned: 3`-- como la prueba de que el par merece leerse.
* Las capturas de las dos páginas de referencia se corrigieron: cuatro en cada
  una. Ninguna otra cifra se tocó.
* Una frase de `find-symbol.md` documentaba el defecto literalmente
  -- «`coverage.exact` counts the rows returned»-- y desaparece con él.

## Verificación

* Cuatro tests de payload dorado por tool caen si se repone el contador,
  comprobado revirtiendo cada mitad por separado: `TestFindSymbol*` para una y
  `TestGetFileOutline*` para la otra. Un payload dorado compara la cadena
  entera, así que una clave de más falla.
* `TestGetFileOutlineCountsOnlyWhatItReturns` sigue defendiendo lo suyo: su
  aserción es sobre `total`, y el `coverage` de su cadena era incidental.
* El binario real por MCP sobre este repositorio, pidiendo menos de lo que la
  respuesta tiene para que un contador de página y uno de respuesta no puedan
  coincidir: `find_symbol` compacta con `total=52 returned=2` responde
  `coverage={"unresolved_related":5}`; en `full`, los cuatro con `exact: 0`.
  `get_file_outline` compacta con `total=448 returned=2` no lleva `coverage`.
  `find_references` sigue en `exact=3` sobre una página de `2`.
* Las `21` capturas JSON de las dos páginas se parsean, y `coverage` sólo
  sobrevive en las de vista `full` con los cuatro contadores a cero.
