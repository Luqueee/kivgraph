# ADR 0073: la prosa no paga el esquema

- **Estado:** aceptada
- **Fecha:** 2026-08-25
- **Cambia el protocolo MCP:** no
- **Cambia el schema persistente:** **no, y ésa es la decisión**
- **Obliga a reconstruir:** no
- **Cambia la superficie del CLI:** no
- **Relaja un contrato de la raíz:** no

## Contexto: un techo con nombre y tamaño

El ADR 0072 dejó una decisión abierta y nombró su disparador. `benchmarks/intent-token-cost`
la disparó: sobre `24` preguntas y tres repositorios, `14` de los `18` ceros de
`find_by_intent` no son un fallo de ranking sino un **fichero inalcanzable** --
preguntado con la búsqueda apuntada directamente a él, no devuelve nada, porque
ni su nombre, ni su nombre cualificado, ni su kind, ni su ruta llevan una palabra
de la pregunta.

Persistir docstrings arreglaría eso, y cuesta: `CanonicalSchemaVersion` `4` -> `5`,
`SnapshotFileFormatVersion` `2` -> `3`, un `schemas/ladybug/005-canonical.cypher`,
los cinco loaders emitiendo prosa, un ADR de migración y una **reconstrucción
completa en cada instalación**, porque el único camino de indexado es el pase
completo (ADR 0057).

Antes de pagarlo se construyó el índice que ese cambio produciría, fuera del
producto: `benchmarks/docstring-simulation`. Mismo `Fold`, mismo `Score`, mismo
paseo de vecinos, misma vista, misma generación publicada. Una sola fuente de
texto más por símbolo.

## La medición

El brazo de control reproduce el producto exactamente -- `1.924` términos,
`194.181` postings, `6` de `24` con `2` primeros puestos--, así que el simulador
mide el producto y no a sí mismo.

|brazo|términos|postings|acierta|primeros|
|---|---|---|---|---|
|sin prosa (control)|`1.924`|`194.181`|`6` de `24`|`2`|
|con prosa, sin pesar|`5.295`|`358.305`|`6` de `24`|`2`|
|con prosa, un acierto de prosa vale `0,70`|`5.295`|`358.305`|`6` de `24`|`2`|
|con prosa, vale `0,50`|`5.295`|`358.305`|`7` de `24`|`2`|
|con prosa, vale `0,30`|`5.295`|`358.305`|`8` de `24`|`3`|
|con prosa, vale `0,15`|`5.295`|`358.305`|`7` de `24`|`3`|

`6.536` de `22.299` símbolos llevan docstring: `1,61 MB` de prosa, e índice
**x1,8** en postings y **x2,8** en vocabulario.

## Decisión

**No se sube el esquema.** Los docstrings no entran en el grafo canónico.

Tres razones, todas de la tabla:

1. **Sin pesar, la prosa es un empate exacto.** `6` de `24` antes y después. Gana
   cuatro preguntas y pierde otras cuatro: un término del comentario pesa igual
   que uno del nombre, así que el fichero que **menciona** las palabras desbanca
   al que las **implementa**.
2. **Pesada, el mejor caso compra `2` preguntas** de `24`, y el peso lo elegí
   mirando la respuesta. No es monótono -- `0,15` es peor que `0,30`--, que es la
   firma de un parámetro ajustado a un conjunto pequeño y no de un efecto.
3. **Y desestabiliza lo que ya funcionaba.** «qué código se niega a publicar
   cuando el disco está lleno» va del primer puesto a no ofrecerse a los pesos
   `1`, `0,7` y `0,5`, y vuelve al cuarto en `0,3`. Cuatro respuestas buenas
   pasan a depender de dónde caiga una constante.

Sólo **una** pregunta gana de forma estable en todos los pesos: la de `mole`,
cuyo comentario dice «comments» y «disk» donde el código dice `updateMapping` y
`yaml.Node`. Una pregunta no paga cinco loaders.

## Consecuencias

- El techo de `14` de `18` **sigue en pie y sigue nombrado**. Lo que este ADR
  retira es la solución, no el problema: la prosa está disponible, es barata de
  leer y no ordena.
- Lo que queda abierto no es más peso sobre el mismo texto. Los `14`
  inalcanzables son ficheros cuyo vocabulario vive en cadenas y comentarios
  porque **su código no deletrea el comportamiento en los nombres**, y eso es una
  propiedad del corpus y no del índice: `find_by_intent` acierta `5` de `8` en el
  repositorio que escribió sus nombres así, `1` de `8` y `0` de `8` en los otros
  dos.
- El simulador se queda en el árbol. Es la puerta de medición para cualquier
  propuesta futura sobre el texto indexado: se corre antes de tocar el esquema,
  no después.

## Dónde estaba la palanca

El mismo simulador encontró la respuesta que este ADR buscaba, y no está en el
texto indexado sino en **cómo se llama a la tool**. `keywords` es el parámetro que
sustituyó a los embeddings en la `v4.0`, y ningún benchmark lo había pasado nunca.

|índice|el llamante adivina identificadores|acierta|primeros|
|---|---|---|---|
|sólo nombres (el que se publica)|no|`6` de `24`|`2`|
|**sólo nombres (el que se publica)**|**sí**|**`11` de `24`**|**`4`**|
|literales, un acierto prestado vale `0,30`|sí|`16` de `24`|`4`|
|prosa y literales, `0,30`|sí|`12` de `24`|`7`|

Las palabras las adivinó un modelo a partir de la pregunta y sin ver ningún
fichero respuesta -- que es exactamente lo que un agente llamante puede hacer.
`11` contra `6` **sin tocar nada**, y contra los `7` de `grep`.

Y falta la otra mitad del consejo, que tampoco cuesta nada. El corpus es una
ventana de diez filas compartida por tres repositorios donde el mayor tiene el
`65 %` de los símbolos -- `kivgraph` `14.490`, `api-db-go` `7.393`, `mole` `416`--,
así que un `api-db-go` correcto pierde el hueco contra un `kivgraph` plausible.
Nombrando el repositorio, que es un parámetro que el llamante suele saber:
**`17` de `24`**, y `api-db-go` pasa de `1` de `8` a `4`.

Y nada enrutaba hacia ahí: la `guidance` nombraba `keywords` sólo cuando ninguna
palabra de la pregunta matcheaba algo, caso que en estas `24` preguntas **no
ocurre nunca**. El consejo no se emitía jamás.

Diagnosticado sobre el brazo que ahora se recomienda, el reparto de los ceros
cambia de sitio: los inalcanzables caen de `14` a `3`, y `10` de los `13` fallos
restantes son pesos o un parámetro. **La factura de esquema se divide por casi
cinco por documentar una llamada.**

Esto no reabre la decisión de arriba: la refuerza. Con la tool llamada como debe,
el techo que un `CanonicalSchemaVersion` 5 compraría son `3` preguntas de `24`.

## El residuo, y por qué no es de pesos

Llamada como se debe -- `17` de `24`--, los `7` fallos que quedan se clasificaron
uno a uno preguntando con la búsqueda apuntada al propio fichero respuesta:

|fallo|qué le pasa|
|---|---|
|`mole/cmd/mole/daemon.go`, `mole/cmd/mole/logs.go`, `api-db-go/.../headers.go`|**techo**: ni un símbolo del fichero alcanza un término|
|`kivgraph/internal/mcp/server.go`, `api-db-go` `main.go`, `locker.go`, `bots_guilds_handler.go`|alcanzan por **un único símbolo** con un término genérico|

Los cuatro segundos parecen ranking y no lo son en la práctica: `replyWithStatus`
lleva `1` término donde `ValidationWithMeta` lleva `3`, y un peso que ponga el de
uno por delante del de tres rompe los `17` que hoy funcionan.

Sólo uno de los siete es una pregunta de forma legítima -- «dónde se registran
todas las tools»: las doce funciones `Register*` llevan la palabra en el nombre y
viven cada una en su fichero, mientras que el fichero que contesta es el único
que **las llama a todas**, con `near=4` contra `near=1`. La señal correcta existe
y está topada en `1,9`, y subir ese tope por una pregunta de `24` es ajustar una
constante a un caso.

Y las dos de `mole` son exactamente las dos que `grep` sí acierta, porque su
vocabulario vive en un mensaje -- «already running», una línea de log sobre un
fichero que se encogió--. Eso vuelve a señalar los **literales** y no los
comentarios: la factura de esquema, si algún día se paga, son `3` preguntas de
`24` y `2` de ellas son cadenas.

## Cuarta señal rechazada: ordenar un fichero por lo que acumula

La vista `files` ordena cada fichero por su **mejor** símbolo. Sumar los de todo
el fichero parecía la forma natural de encontrar el punto de agregación del caso
anterior, y se midió: **`11` de `24` baja a `7`, y los `4` primeros puestos a
`0`**. Premia al fichero grande con muchos matches flojos por encima del que
tiene un match fuerte, que es la definición de un god file ganando una búsqueda.
Código retirado; queda el número.

## Confirmado sobre un segundo corpus

La generación `000194` -- la primera después de que el loader de TypeScript
empezara a leer un `jsconfig` como proyecto-- creció el corpus un `45 %`:
`32.419` símbolos contra `22.299`, y `kivgraph` pasó del `65 %` al `76 %` del
total. El índice de nombres pasó de `1.924` términos y `8,71` postings por
símbolo a `2.456` y `10,73`.

Las mismas `24` preguntas sobre ese corpus dan **exactamente los mismos
aciertos**: `6` de `24` en prosa sola, `11` con las palabras adivinadas, `17`
nombrando el repositorio, y el mismo reparto de causas -- `6` de ventana, `4`
desbancadas, `3` inalcanzables--. La superficie no se degrada porque el corpus
crezca.

Y la decisión de este ADR sale más fuerte, no más débil:

|texto indexado|generación `000191`|generación `000194`|
|---|---|---|
|prosa, mejor caso|`11` de `24`|**`9` de `24`**|
|literales, mejor caso|`16` de `24`|**`16` de `24`**|

**La prosa empeora cuando el corpus crece y los literales aguantan.** Es lo
esperable de una señal que añade vocabulario en todas partes frente a una que
añade el vocabulario de un comportamiento concreto, y es la segunda medición
independiente que dice que si algún día se paga un esquema, se paga por los
literales.

## Lo que este ADR no cierra

Que un **parser** en vez de una regla de texto ataría el docstring al símbolo con
más precisión. El arnés declara esa limitación y por eso su brazo de prosa es un
suelo. Pero la diferencia tendría que convertir dos preguntas en diez para mover
esta decisión, y nada en la tabla sugiere esa pendiente.
