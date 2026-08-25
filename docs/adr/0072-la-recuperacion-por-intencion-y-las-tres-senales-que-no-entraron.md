# ADR 0072: la recuperación por intención, y las tres señales que no entraron

- **Estado:** aceptada
- **Fecha:** 2026-08-25
- **Cambia el protocolo MCP:** sí, de forma aditiva — una tool nueva,
  `find_by_intent`, y ningún cambio en las doce existentes
- **Cambia el schema persistente:** no
- **Obliga a reconstruir:** no — el índice se deriva al leer el snapshot
- **Cambia la superficie del CLI:** no
- **Relaja un contrato de la raíz:** no

## Contexto: la pregunta que la superficie no admitía

Las doce tools exigían un nombre. `find_symbol` toma uno, `get_source` toma una
terna, las travesías toman una raíz. Ninguna admite «no sé cómo se llama», que es
con lo que empieza cualquier sesión sobre código ajeno.

Lo que se construyó es un índice invertido derivado —`0,76 MB` sobre `22.299`
símbolos, `8,71` postings por símbolo, `22 ms` de derivar contra `68` de leer el
fichero— y una tool encima. No hay BM25, ni embeddings, ni stemmer, ni lista de
stopwords: `PLAN.md:27-28` excluye el buscador vectorial, y la lingüística se
sustituyó por lo que el índice ya sabe medir.

## Decisión: qué señales ordenan, y en qué orden

Cinco, todas derivables del snapshot:

|señal|qué dice|techo|
|---|---|---|
|términos llevados|el símbolo lleva la palabra|base|
|rareza|descontada por la porción del corpus que el término matchea|×1|
|longitud|cuánto del término era la palabra, al cuadrado|×1|
|`kind`, visibilidad, ruta, fan-in|estructura que el grafo ya afirma|×2, ×1,5, ×1, ×2|
|**aristas salientes**|los términos que el símbolo **alcanza** y no lleva|**×1,9**|

Y el orden entre las dos últimas es la parte que se puede infringir: **llevar un
término gana a alcanzarlo, siempre.** El techo del crédito por aristas es `1,9` y
no `2,0` justamente por eso — a `2,0` un símbolo que lleva un término y alcanza
otro **empata** con uno que lleva los dos, y un empate lo decide la stable key,
que es alfabética y pone el fixture primero. Lo cazó un test, no una revisión.

Dos consecuencias que no se pueden relajar:

- **El grafo nunca inventa un candidato.** Un símbolo que no lleva ningún término
  no es respuesta por muchas respuestas que llame. El crédito es un peso sobre
  evidencia léxica, nunca un sustituto de ella.
- **Una fila que usó una arista no se llama `lexical`.** Dice `lexical+calls`, y
  la vista `compact` no iza un `match` en el que las filas discrepan.

### La longitud sustituye a la lista de stopwords

Medido sobre la generación publicada: `is` lo llevan `178` de `22.299` símbolos,
o sea `df` del `0,8 %`, así que el descuento por rareza lo leía como señal casi
pura. `IsValidModuleKey`, `isEntryPoint`, `IsSyntheticRepository` ocupaban el
primer puesto de tres preguntas sin relación. **La frecuencia no distingue la
gramática de una pregunta de su vocabulario**: `library` son `7` símbolos y `own`
son `2`, más raros que `is`.

La longitud sí, y sin lista inglesa: un término son cinco runas de un token, así
que una palabra de dos runas matcheó un segmento de dos runas de un
identificador, y en código eso es una partícula — `Is`, `To`, `Of`, `At`, `In`.
El peso es `(runas/5)²`, saturando en el pliegue: al cuadrado y no lineal porque
a `0,4` dos partículas todavía ganaban a una palabra real, y a `0,16` no.

Esto **no es la lista de stopwords que el diseño rechazó**, y la distinción
importa: aquélla era una lista inglesa aplicada al **índice**, con `himself` y
sin `get`, `new` ni `data`. Ésta es una medida sobre la **consulta**, no toca el
índice y por eso no vuelve monolingüe un producto de cinco lenguajes.

## Lo que se midió y se quedó fuera

Tres señales plausibles se implementaron enteras, se midieron sobre las ocho
preguntas de `benchmarks/intent-token-cost` y **se borraron**. Se escriben aquí
para que la revisión siguiente tenga un disparador y no una discusión.

|señal|hipótesis|resultado|
|---|---|---|
|rango de prefijo|`rust` debería alcanzar `rustloader`|**5/8 → 4/8**|
|términos del fichero|la respuesta vive en un fichero que trata de la pregunta|**5/8 → 4/8**|
|adyacencia|dos palabras juntas en un nombre son la respuesta|**a techo alto daña, a techo bajo no hace nada**|

- **Rango de prefijo.** Las claves empaquetan runas big-endian con relleno de
  ceros, así que los términos con prefijo `rust` son un rango contiguo y
  resolverlo es exacto, sin heurística. Contestó una pregunta más y perdió dos:
  `tool` se ensancha a `tools` y `toolstats` y entierra el fichero que las
  registra del octavo puesto al decimocuarto. Acotarlo a palabras de cuatro runas
  no lo arregló. **La brecha se queda**, y lo que la cierra para quien la
  necesite es el parámetro `keywords`.
- **Términos del fichero.** Se aplicó al símbolo y no a la página, deliberadamente:
  `view` es granularidad y **no una segunda respuesta**, y rankear ficheros aparte
  habría roto ese invariante. Compró tokens —`1.857` a `1.701`— y costó un punto
  de precisión. Precisión antes que coste.
- **Adyacencia.** La única señal que leía una secuencia en vez de un conjunto.
  A techo `2,0` la pregunta sobre el guardia de espacio libre cayó del primer
  puesto al tercero: premia el **nombre literal de la frase**, y esa pregunta
  trata de un guardia *dentro* del camino de publicación, no del camino. A techo
  `1,4` los ocho rangos salen idénticos a no tenerla.

## Consecuencias

- La tool contesta `5` de `8` preguntas donde `grep` acotado al fuente contesta
  `3`, con `2` primeros puestos contra `0`.
- **El coste de sesión es `1,00x`**: los `22.358` tokens de cuerpo que el agente
  abre después dominan, y los ~`300` de la respuesta se pierden en el redondeo.
  El `1,9x` sobre `grep` es real y se paga sobre el `2 %` del total.
- Es la tool número trece y deja el presupuesto residente a `4` bytes de su
  techo. Ése es el precio real: la siguiente no cabe sin retirar algo.
- **El techo que queda es de esquema, no de ranking.** Dos de los tres fallos son
  ficheros que no llevan ni una palabra de su pregunta en nombre, ruta o kind —
  el índice no tiene prosa. Subirlo cuesta `CanonicalSchemaVersion` 5, un ADR,
  los cinco loaders emitiendo docstrings y una reconstrucción completa, y compra
  dos de ocho preguntas **en este conjunto**.
- Y ocho preguntas sobre un repositorio marcan una dirección, no una tasa: antes
  de pagar un cambio de esquema, el mismo arnés sobre otro corpus es más barato
  y decide mejor.

## La medición que esta decisión pidió, y lo que corrigió

El conjunto pasó a `24` preguntas sobre los tres repositorios registrados —
`kivgraph`, `mole` y el servicio `api-db-go` de `kena`—, con la verdad de
terreno leída del código y la regla de sesgo comprobada a máquina: ninguna
palabra de una frase es un identificador declarado en su fichero respuesta.

|repositorio|preguntas|`grep` acierta|la tool acierta|
|---|---|---|---|
|`kivgraph`|`8`|`3`|`5`|
|`api-db-go`|`8`|`2`|`1`|
|`mole`|`8`|`2`|`0`|

**El `5` de `8` era del repositorio, no de la tool.** Sobre dos codebases que no
escribí, `grep` va por delante: `7` de `24` contra `6` de `24`, al mismo coste
de sesión.

Y de los `18` ceros, la causa se separa en tres facturas distintas:

|causa|preguntas|qué costaría arreglarla|
|---|---|---|
|competencia entre repositorios|`0`|un parámetro que el llamante ya tiene|
|desbancada dentro de su repositorio|`4`|pesos, y nada persistido|
|inalcanzable: el fichero no lleva ningún término|`14`|esquema, cinco loaders y reconstrucción|

Las dos primeras hipótesis mueren con un número. Nombrar el repositorio **no
mueve nada** -`6` de `24` con filtro y sin él-, así que la ventana compartida no
era el problema. Y el ranking sólo es dueño de `4` fallos: en los otros `14`, una
búsqueda apuntada **directamente** al fichero respuesta no devuelve nada, y
ningún peso levanta una puntuación de cero.

Eso confirma el techo que este ADR ya nombraba y le pone tamaño: `14` de `18`, y
no `2` de `3`. La conclusión que cambia es la otra — **seguir puliendo el ranking
no está capado en `6` de `8` sino en `4` preguntas de `24`**, y la diferencia
entre un repositorio que contesta y uno que no es si su código deletrea el
comportamiento en los nombres o lo guarda en cadenas y comentarios.
