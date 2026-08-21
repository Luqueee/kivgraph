# ADR 0054: El puente a la implementación única

- **Estado:** aceptada
- **Fecha:** 2026-08-21

## Contexto

El conjunto de preguntas duras de `benchmarks/graph-tools-comparison` -- el que
existe para encontrar fallos y no para confirmar la tabla -- destapó el peor que
teníamos. `H2_go_iface` pregunta qué archivos llaman al `FindPendingGuilds` que
implementa `NotifierSubRepository`, la **única** implementación del método que
`pgrepo/repos.go:329` declara en una interfaz. La respuesta era:

```
total: 0
"nothing references this symbol in the published graph; the edges are
 type-checked, so this is an absence rather than a miss"
```

Hay un llamante: `guilds_handler.go:349` hace
`h.Notifier.FindPendingGuilds(...)`. El checker resuelve esa llamada al método
**de la interfaz**, que es donde el sitio de llamada la nombra, así que la
implementación no tenía ninguna arista entrante. `H5_rs_trait` daba lo mismo en
Rust sobre `Arc<dyn StateStore>`.

Dos cosas lo convierten en el fallo más grave posible y no en una limitación:

- **La frase es falsa, y es la misma que usa una ausencia real.** La página de
  Kivgraph afirma que una lista de referencias vacía significa que nadie lo
  llama. Eso vale para una llamada estática y no valía para una dinámica, sin
  que nada lo dijera.
- **`code-review-graph` contestaba `H2` exacta.** No era una frontera del
  problema; era nuestra.

La causa raíz no estaba en la consulta: **el grafo no tenía la arista**.
`IMPLEMENTS` relacionaba tipo con interfaz -- `types.Implements` decide eso --
y nada relacionaba el método concreto con el método de la interfaz, que es el
único hecho que dice a qué declaración llega la llamada.

## Decisión

### 1. `IMPLEMENTS` se emite también a granularidad de método

El cargador de Go empareja cada método de una interfaz satisfecha con el método
concreto que la llamada alcanza. El emparejamiento lo decide
`types.LookupFieldOrMethod` sobre el receptor que satisfizo la interfaz: es la
selección del propio checker, **nunca una coincidencia de nombres**. Un fixture
lo prueba por el lado negativo -- `Blob.Area` se escribe igual que `Shape.Area`,
devuelve `int`, y no se empareja.

No es una convención nueva. `TypeRelation` ya se documenta como «entre dos tipos
**o métodos**» y `OVERRIDES` ya era método a método, así que la tubería
-- hechos, delta, snapshot, MCP -- no cambia. Tampoco cambia el vocabulario:
ninguna clase de arista ni código nuevo, y por tanto ninguna migración de
schema.

Un método que el receptor sólo tiene por promoción desde **otro paquete** se
omite: la relación se ancla en la declaración concreta y una pasada no puede
anclar evidencia en un fichero que no cargó. `EMBEDS` y `OVERRIDES` ya describen
la cadena de promoción que lleva hasta ahí.

### 2. `find_references` cruza esa arista sólo con implementación única

Una respuesta entrante sobre un método incluye además las referencias a los
métodos de interfaz que implementa, **y sólo donde es la única
implementación**, que es el caso en el que una llamada por la interfaz no puede
alcanzar otra cosa.

- **Con dos implementaciones no se puentea.** Una llamada por la interfaz llega
  a una de ellas, y nombrar las dos como llamadas cambiaría una ausencia falsa
  por una presencia falsa. `get_blast_radius` -- que es la herramienta de «qué
  alcanza un cambio» -- sigue cruzando `IMPLEMENTS` en los dos sentidos.
- **Cada fila puenteada dice por dónde llegó.** Lleva `via` con el método de
  interfaz, y la página declara `dispatch_through`. `via` iza y agrupa como las
  demás columnas de la vista compacta, así que una fila puenteada no se puede
  leer como una llamada directa en ninguna vista.
- **Una arista `IMPLEMENTS` entrante en el método de interfaz no es un uso.** Se
  excluye del conjunto puenteado; la única que hay es la del propio sujeto, y
  reportarla haría que el sujeto se referenciara a sí mismo.
- El puente se construye antes de recorrer la página, así que `total`, las filas
  y `coverage` describen un solo conjunto y el cursor sigue direccionando un
  orden estable.

## Consecuencias

- `H2_go_iface` pasa de `0,00`/`0,00` a exacta. Las cifras medidas están en
  `benchmarks/graph-tools-comparison/harder.md`.
- **Rust queda abierto.** Su `IMPLEMENTS` sale de `impl Trait for Type` en
  `internal/rustloader/relations.go` y sigue siendo tipo con trait, así que
  `H5_rs_trait` no se mueve. Es la etapa dos, con este ADR como precedente: el
  contrato de consulta ya está escrito y probado, y lo que falta es que el
  cargador de Rust empareje los métodos como lo hace el de Go.
- **Es un cambio de superficie MCP.** La respuesta gana `via` por fila y
  `dispatch_through` por página, y una pregunta que antes devolvía cero filas
  puede devolver filas. Ningún cliente pierde nada.
- El grafo crece: una arista más por método de cada interfaz satisfecha.
- **Requiere reindexar.** Las aristas nuevas las produce una pasada completa; una
  generación anterior no las tiene y el puente no encontrará nada en ella.

## Alternativas descartadas

- **Puentear siempre, declarando el grado.** Cierra también el caso de `N`
  implementaciones, y a cambio una interfaz con veinte implementaciones
  convierte una llamada en veinte llamantes. La precisión es lo único que hace
  útil una respuesta de referencias, y esto la habría hundido justo en los
  grafos grandes, que es donde la herramienta gana.
- **Emparejar en la capa de consulta por nombre o por firma.** Habría evitado
  tocar el cargador y habría sido una inferencia por coincidencia en la capa que
  no puede probar nada: `AGENTS.md` prohíbe crear una arista `EXACT` así, y
  añadir una fila a una respuesta es una afirmación sobre el mundo, no una
  decisión de página como retirar una.
- **Una clase de arista nueva.** Habría añadido un código al schema y una
  segunda convención para lo que `IMPLEMENTS` ya nombra, con `TypeRelation`
  documentando desde el principio que sus extremos pueden ser métodos.
- **Sólo arreglar la frase.** Cambiar la `guidance` para que nombre la interfaz
  en vez de afirmar una ausencia es barato y honesto, pero deja la pregunta sin
  contestar y la derrota contra `code-review-graph` intacta.
