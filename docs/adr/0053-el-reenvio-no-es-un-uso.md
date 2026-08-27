# ADR 0053: El reenvío no es un uso

- **Estado:** aceptada
- **Fecha:** 2026-08-21

## Contexto

`find_references` respondía con las aristas `EXPORTS` y `REEXPORTS` junto a las
de uso. Sobre el monorepo `workspace`, la pregunta «qué call sites usan el
`withRetry`
declarado en `libraries/library-shared/src/utils/retry.ts`» devolvía nueve
archivos: los cinco llamantes reales y cuatro barrels de re-export.

```
[export / EXPORTS / EXACT_TYPECHECKED]
  library-shared  src/utils/retry.ts          <- el que declara
[import / IMPORTS_SYMBOL / EXACT_PACKAGE_MAPPED]
  core            src/cluster/worker/BotWorker.ts
  gateway         src/grpc/server.ts
  core            src/cluster/master/index.ts
  sdk-module-ts   src/sdk/client/PrivateModule.ts
  core            src/shared/utils/sharding.ts
[export / REEXPORTS / EXACT_TYPECHECKED]
  library-shared  src/index.ts
  library-shared  src/client/index.ts
  library-shared  src/utils/index.ts
[export / REEXPORTS / EXACT_PACKAGE_MAPPED]
  sdk-module-ts   src/index.ts
```

Los cuatro `REEXPORTS` eran **la totalidad** de los falsos positivos que la
medición de `benchmarks/graph-tools-comparison` atribuía a Kivgraph: los únicos
cuatro de cuarenta y ocho archivos afirmados sobre las siete preguntas, y la
única razón por la que su precisión no era `1,00`.

Dos hechos hacen que la decisión no sea una preferencia de gusto.

El primero es que **la propia herramienta ya declaraba que un re-export no es el
símbolo.** Pedirle un barrel como sujeto devuelve un error, no una respuesta:

```
SYMBOL_NOT_FOUND: name "withRetry" is only imported or re-exported here,
never declared; pass the repository and path that declares it
```

Así que un barrel no podía ser sujeto y sí podía ser respuesta. Eso no es una
elección de granularidad; es una contradicción interna.

El segundo es que **el reenvío no aporta alcance.** El checker de TypeScript
resuelve un import a través de cuantos barrels haya y nombra la declaración, de
modo que cada consumidor trae su propia arista `IMPORTS_SYMBOL`. En el caso
medido, los cinco consumidores están detrás de los cuatro barrels y los cinco
aparecen en la respuesta sin ellos. La arista de reenvío no nombraba a ningún
consumidor que la respuesta no tuviera ya.

## Decisión

Una respuesta de `find_references` que nadie filtró **no incluye las aristas de
reenvío** -- `EXPORTS` y `REEXPORTS` -- y **lo declara** en
`edge_kinds_default_excluded`.

- El filtro actúa en el único punto donde se admite una arista candidata, así
  que `total`, la página y los recuentos de `coverage` describen el mismo
  conjunto. Un total filtrado que no dijera que lo está entendería a la baja las
  referencias a un símbolo.
- `edge_kinds` las recupera para la pregunta que sí contestan: un renombrado
  tiene que editar el barrel. `edge_kinds: ["*"]` desactiva el filtro entero, con
  la misma grafía que ya usa `get_blast_radius` para su comodín.
- Una selección explícita no declara nada, porque la escribió quien llama.
- `get_blast_radius` **no cambia**: es la herramienta de «qué se rompe si lo
  cambio», y un renombrado sí rompe el barrel.
- `trace_dependencies` **no cambia**: recorre el grafo hacia fuera y excluir una
  arista de reenvío truncaría un alcance real.

El mecanismo no es nuevo: es el de `blastRadiusKindFilter`, que ya excluye
`field` y `variable` de un impacto que nadie acotó y lo declara en
`kinds_default_excluded`. Se reutiliza la convención en vez de crear una segunda;
la única diferencia es el eje, aristas en vez de clases de símbolo, y por eso la
clave lo nombra.

### Por qué el eje son las aristas y no las clases de símbolo

Las filas de reenvío llevan `kind: "export"` y las de uso cruzado llevan
`kind: "import"`. Excluir por clase de símbolo habría sido igual de fácil de
escribir y habría destruido la exhaustividad: los cinco llamantes reales de
`withRetry` llegan por `IMPORTS_SYMBOL`, con `kind: "import"`. El eje correcto es
el que distingue reenviar de usar, y ese es la arista.

## Consecuencias

- `R1_ts_xrepo` pasa de `P=0,56` a `P=1,00` sin perder exhaustividad, y el
  agregado de las siete preguntas a `1,00`/`1,00`. Las cifras medidas están en
  `benchmarks/graph-tools-comparison/remeasure.md`.
- **Es un cambio de superficie MCP.** El esquema gana el valor `"*"` en
  `edge_kinds` y la respuesta gana `edge_kinds_default_excluded`; el conjunto que
  devuelve una llamada sin `edge_kinds` es más pequeño que antes. Un cliente que
  contaba con las aristas de reenvío por defecto tiene que pedirlas.
- Un fixture de `find_references` afirmaba una arista `EXPORTS` **saliente de una
  función**, forma que ningún cargador produce: un `EXPORTS` real nace en el
  binding de export y muere en la declaración, así que sólo alcanza una
  declaración desde fuera. El fixture pasa a declarar la llamada que sí sería
  saliente. Lo destapó este cambio, y es la regla de `AGENTS.md` sobre fixtures.
- La comprobación de la vista `files` buscaba la subcadena `edge_kind` para
  impedir columnas por fila; ahora busca la clave exacta `"edge_kind"`, porque
  un campo de página sobre la consulta no es un dato por arista.

## Alternativas descartadas

- **Dejarlo y que quien llama filtre.** La información estaba: la respuesta
  agrupa por `edge_kind`. Pero el coste que la herramienta existe para evitar es
  precisamente abrir archivos que no hacía falta abrir, y el valor por defecto es
  lo que paga un agente que no sabe que debe acotar.
- **Filtrar sólo en `direction: "incoming"`.** Más quirúrgico sobre el papel y
  sin efecto real: una arista de reenvío siempre apunta *a* la declaración, así
  que sólo aparece entrante. Habría añadido una asimetría que describir a cambio
  de nada.
- **Ajustar el harness para que no puntuara los barrels.** Habría movido la cifra
  sin mejorar ninguna respuesta, que es la definición de afinar un benchmark
  contra sí mismo.
- **Excluir por clase de símbolo `export`.** Ver arriba: mismo resultado en este
  caso y una trampa a un paso.
