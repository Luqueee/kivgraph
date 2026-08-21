# ADR 0056: La retirada alcanza lo que un fichero afirmó

- **Estado:** aceptada
- **Fecha:** 2026-08-21

## Contexto

El primer test que comparó un delta incremental contra una reconstrucción limpia
**sobre salida del cargador real** -- no sobre sets construidos a mano --
encontró que editar un fichero perdía todas las aristas que le apuntaban desde
otros ficheros.

Sobre el fixture `type-relations`, hacer crecer un cuerpo de método en
`geometry.go` **una línea** reemplazaba ese fichero y ninguno más. Las seis
aristas que entran desde `units/` están ancladas en ficheros de `units/`, cuyos
hechos no cambiaron, así que `Diff` no las restablecía. Y la retirada las
borraba de todos modos.

El mecanismo no era una cascada implícita del motor. Era explícito:

```go
// LadybugDB requiere las aristas de un nodo fuera antes de borrar el nodo.
deleteCanonicalEdgesTouching(ctx, native, symbolKeys)
```

que por cada tabla de relación hacía

```cypher
WHERE source.stable_key IN $keys OR target.stable_key IN $keys
```

Ese `OR target` borraba las entrantes. Y se borraban porque había que borrar el
nodo -- pero **el nodo no hacía falta borrarlo**: `Circle.Area` sigue existiendo
después de la edición, con la misma clave estable. Se borraba y se recreaba, y
las entrantes morían en el hueco.

Producción toma esa ruta: `internal/indexer/delta.go` llama a `facts.Diff` con
los dos sets completos, que es exactamente la forma del test.

Esto contradecía el invariante que `AGENTS.md` ya declaraba: «todo hecho afirmado
por **un archivo** se retira y se vuelve a afirmar junto con ese archivo». La
arista de `units/handlers.go` a `geometry.go` la afirma `units/handlers.go`. No
era del fichero editado, y se retiraba con él.

## Decisión

La retirada distingue **lo que un fichero afirmó** de **lo que hay que despejar
para poder borrar un nodo**, que hasta ahora eran la misma consulta.

1. **Lo afirmado son las aristas que salen de sus símbolos.** Se borran todas y
   el `Upsert` restablece las vigentes. Es
   `deleteCanonicalEdgesAssertedBy`: `deleteCanonicalEdgesTouching` sin la mitad
   `OR target`.
2. **Un símbolo que el `Upsert` vuelve a afirmar conserva su nodo.** El upsert le
   escribe encima. Por tanto las aristas que otros ficheros le apuntan no se
   tocan nunca.
3. **Un símbolo que el `Upsert` no afirma se va**, y sus aristas -- entrantes
   incluidas -- se despejan antes, porque el motor lo exige.
4. La evidencia y los no resueltos de un fichero retirado se van siempre: una
   clave de evidencia lleva sus offsets, así que todas se mueven cuando el
   fichero cambia. `deleteCanonicalEdgesEvidencedBy` no cambia -- es el caso de
   las aristas de paquete, cuyos dos extremos sobreviven y cuyo testigo no.
5. Un fichero **eliminado** pierde también su propio nodo, así que las aristas
   que le apuntan se van. Uno **reemplazado** lo conserva.

### Por qué no cuelga nada

El caso que da miedo es «el símbolo destino desapareció de verdad». Entonces está
en el conjunto condenado y el punto 3 borra sus entrantes. Y el fichero que le
apuntaba **también fue reemplazado, necesariamente**: `next` pasó `Validate`, que
exige que los dos extremos de cada arista existan, así que si el destino se fue
`next` no puede contener la arista, el fragmento del origen encogió, y `Diff` lo
reemplazó. Es el razonamiento que el comentario largo de `Diff` ya hacía; este
ADR lo completa en vez de contradecirlo.

## Consecuencias

- **Un grafo incremental no es idéntido byte a byte a una reconstrucción
  limpia**, y la diferencia es exactamente una: una fila que nadie restableció
  conserva el `source_snapshot` y el `resolver_version` de la generación que la
  observó, donde una carga limpia sella todo con la nueva. Es el registro
  honesto -- la llamada se observó en la generación 1 y nadie la volvió a
  observar -- y ninguna consulta filtra por esa columna: es procedencia, no
  identidad. El test lo fija: contenido idéntico, y el sello divergente sólo en
  las filas intactas.
- El test de viaje redondo que ya existía,
  `TestApplyCanonicalDeltaMatchesFreshLoadOfFinalState`, comparaba con
  `reflect.DeepEqual` y pasaba porque en su fixture toda fila se borra o se
  restablece. El caso nuevo es el primero con una fila **superviviente**, y por
  eso es el primero que ve divergir el sello.
- `RemovedSymbols` y `RemovedEdges` cuentan menos en un reemplazo donde un
  símbolo sobrevive, porque se borra menos. Los fixtures existentes no cambian:
  en ellos las claves del fichero reemplazado son todas nuevas
  (`OldReplace` -> `NewReplace`), así que ninguna sobrevive.
- El modelo de referencia del test en `internal/facts` se corrigió con el mismo
  contrato. Era el que codificaba la regla vieja, y por eso el defecto podía
  existir con la suite en verde.

## Alternativas descartadas

- **Restablecer las aristas entrantes desde `Diff`.** Se implementó y se
  revirtió. `Delta.Validate` exige un fragmento autoconsistente -- una arista
  necesita su evidencia, la evidencia su fichero, el fichero su paquete -- así
  que restablecer una arista arrastra su fichero ancla entero. Y entonces las
  entrantes de **ese** fichero también se retiran, y la restitución cascadea
  hacia atrás por el grafo de llamadas: editar una utilidad hoja reemplazaría a
  todos sus llamantes transitivos. El indexador lo absorbería enrutando a
  republicación completa, así que sería correcto y lento -- el incremental deja
  de ser incremental justo en los ficheros que más se editan.
- **Volver a sellar las filas supervivientes** para que el grafo sea idéntico a
  una reconstrucción. Obligaría a tocar cada arista superviviente en cada delta,
  que es precisamente el trabajo que un delta existe para no hacer, y mentiría
  sobre cuándo se observó el hecho.
