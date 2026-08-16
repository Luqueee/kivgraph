# ADR 0040: Servir código desde el grafo publicado

- **Estado:** aceptada
- **Fecha:** 2026-08-13
- **Revisa:** el contrato de `serve` fijado en la fase MCP

## Contexto

`kivgraph serve` responde desde el `HotSnapshot` publicado y **no abre la base
de datos**. Ese contrato está escrito, tiene tests y es la razón por la que un
`index --full` en otra terminal no puede dejar a un servidor sirviendo un grafo
que ya no existe.

Lo que el servidor nunca ha hecho es entregar código. Devuelve dónde está: un
repositorio, una ruta y un rango de líneas. El agente abre el fichero él mismo
con la herramienta de su anfitrión, y ahí está la mitad del coste de la sesión.

Medido por `benchmarks/mcp-token-cost` sobre la generación `000024`, seis
preguntas del tipo «quién llama a este símbolo»:

```text
vía nativa del anfitrión         25.144 tokens
Kivgraph hoy                    17.464 tokens   1,44x
  llamadas MCP                    3.059
  veinte lecturas de rango        14.405
los mismos bytes, sin envoltorio 10.934 tokens
```

Los 3.471 tokens de diferencia entre leer y servir no son un detalle de
formato: el `read` de rango de Oh My Pi antepone la cabecera del snapshot y el
número de línea a **cada línea**, y eso mide un **38 % sobre los bytes** -427
tokens donde el cuerpo son 302-. Servirlos desde el grafo los cobra una vez, sin
prefijos, y en una sola llamada en vez de una por cuerpo.

Para hacerlo, `serve` tiene que leer del sistema de archivos. Eso amplía su
contrato y es lo que este ADR decide.

## Decisión

**`serve` puede leer los ficheros de los repositorios registrados, sólo para
entregar bytes, y nunca para afirmar un hecho del grafo.**

La distinción es el corazón de la decisión y se aplica campo a campo.

### Falla cerrado en lo que afirma

Una arista, una posición o una relación siguen saliendo únicamente del snapshot.
`get_source` no resuelve nada, no busca por texto y no completa lo que el grafo
no publicó. Si el snapshot no conoce el símbolo, la respuesta es un error, no una
búsqueda en el fichero.

### Degrada declarando en lo que entrega

Cada `File` del grafo lleva el `ContentHash` -SHA-256 en hexadecimal- de los
bytes que se analizaron. Al servir un cuerpo se recalcula el digest del fichero
en disco y se compara:

- **Coincide.** El rango del grafo es autoridad y se sirve tal cual.
- **No coincide.** El **fichero** es autoridad, porque es lo que el agente va a
  editar. Se reancla la declaración por su nombre en el fichero actual, se
  sirven sus bytes y **se declara el desplazamiento**: `el grafo dice 484-509; el
  fichero cambió; la declaración está en 512`.
- **No se encuentra allí, o se encuentra dos veces.** Esa fila no devuelve bytes
  y dice por qué. Las demás filas de la misma respuesta sí se sirven: un fichero
  editado no invalida las otras cinco respuestas.
- **El hash está vacío o el fichero no se puede leer.** Esa fila no devuelve
  bytes y lo dice.

**Reanclar no crea ninguna arista.** Entrega bytes de un fichero que el llamante
nombró; no afirma que un símbolo llame a otro, ni cambia una posición del grafo,
ni convierte un `UNRESOLVED` en nada. El contrato que prohíbe la coincidencia
nominal prohíbe fabricar hechos, y aquí no se fabrica ninguno: se responde a
«dame estas líneas» con las líneas que ahora ocupan esa declaración, diciendo que
se movieron. Un lector que quiera la posición canónica la tiene en el mismo
snapshot que le dio la fila.

### Por qué no falla cerrado del todo

La primera redacción de la tarea decía «si el digest no coincide, no se sirve
nada». Es la opción tentadora y hace la tool inútil: el árbol de trabajo cambia
entre generaciones -en la sesión que produjo estas cifras el snapshot se
reconstruyó dos veces en una hora- y una tool que se niega a responder casi
siempre enseña al agente a no llamarla. Serena midió esa deserción: el 18,4 % de
sus consultas resueltas acabó en un `Read` del mismo fichero, y el 80,8 % de
esos ya tenía el cuerpo. La forma segura de perder el 20 % de ahorro es dárselo
a una tool que el agente deja de usar.

### Límites de lectura

- Nunca se lee fuera de `Repository.Path`. La ruta se resuelve contra él y se
  rechaza si escapa.
- Ningún componente de la ruta puede ser un enlace simbólico, la misma política
  que ya aplica la capa de workspace.
- Sólo se leen ficheros que el grafo publica. Una ruta que el snapshot no conoce
  es un error, no una lectura.
- La respuesta tiene techo de bytes y lo declara al recortar. No se trunca en
  silencio.
- Sigue siendo read-only: esta tool devuelve código, no lo escribe.

### `context_lines`

Opcional, por defecto `0`, tope `100`. Existe porque la telemetría de Serena lo
midió: de sus lecturas de rescate, el **83,3 %** usó `offset`/`limit`. El agente
no quería el cuerpo, quería lo que lo rodea. Los valores por defecto siguen a
`probe` -que sirve el cuerpo desde el AST y usa `0`- y no a `octocode`, que
parte de `5` porque no tiene un rango exacto que servir.

## Consecuencias

- `graph_status` sigue declarando `storage: not_applicable`: este servidor no
  abre la base. Leer un fichero fuente no es abrir el almacenamiento.
- Un servidor con una generación vieja y un árbol muy editado sirve casi todo
  reanclado. Eso es visible en la respuesta y es información, no un fallo.
- El reanclaje por nombre puede encontrar dos declaraciones homónimas en un
  fichero muy editado. No se elige: se declara y esa fila no da bytes.
- La superficie gana una tool. La fase la compensa retirando `graph_status`,
  `list_repositories` y `get_unresolved_references` de la superficie del modelo,
  con lo que cierra en nueve.

## Alternativas descartadas

**No servir código.** Es el estado que mide el arnés: `1,44x` contra la vía
nativa en vez de `1,80x`, con la sesión pagando un 38 % de recargo en cada
cuerpo.

**Servir rangos arbitrarios de fichero.** `read <path>:<a>-<b>` ya existe en los
dos anfitriones, es más barato que una llamada MCP y está siempre fresco.
Competir con eso no aporta nada. La entrada de `get_source` son símbolos, y su
valor es una sola llamada que reúne cuerpos de varios ficheros y varios
repositorios.

**Fallar cerrado ante cualquier divergencia.** Descartado arriba: convierte el
ahorro en una tool que nadie llama.

**Guardar los cuerpos en el snapshot.** Duplicaría el corpus en el grafo
publicado, y el fichero seguiría siendo la autoridad para editar. El disco ya
tiene los bytes.
