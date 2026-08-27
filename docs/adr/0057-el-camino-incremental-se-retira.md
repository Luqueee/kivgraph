# ADR 0057: El camino incremental se retira

- **Estado:** aceptada
- **Fecha:** 2026-08-21

> **Corrección de 2026-08-21.** Las cifras de este ADR se midieron sobre un
> `workspace` **sin Rust**: al harness le faltaba `cargo` en el `PATH`, así que
> `rust-analyzer` rechazó los dos workspaces Cargo y el pase publicó el resto --
> declarándolo como `not_loaded=2`, que el harness no leyó. Con los tres lenguajes
> cargados el corpus es `4.768` ficheros y `123.524` símbolos, el pase completo
> `10,036 s`, `staging` el `37,1 %`, y el techo del delta **`1,63x`** en vez de
> `1,67x`. La decisión no cambia: se refuerza. Las cifras corregidas y el detalle
> de lo que se movió están en `benchmarks/incremental-cost/report.md`.

## Contexto

El camino del delta estaba construido y probado -- `facts.Diff`,
`indexer.Decide`, `indexer.Update`, `ladybug.ApplyCanonicalDelta`, los planes de
invalidación y las clases de cambio semántico, con tests propios incluidos los
nativos con el tag `ladybug` -- y **no tenía ningún llamante en producción**. Los
consumidores de `internal/indexer` usan `Full`, el servicio de indexado
reconstruye completo, y el CLI responde `index: only --full is supported`. Todo
indexado que un usuario ha podido provocar es una reconstrucción completa.

`LUQUE-2003` preguntaba si se cablea o se retira, y la única cifra que decide eso
es el techo: qué pasos de un pase completo se saltaría el delta y cuáles seguiría
pagando. Está medido en `benchmarks/incremental-cost/report.md`, commit
`e78490e`, corpus `workspace` con `4.683` ficheros y `477.027` aristas, caché de
hechos **caliente** -- que es la condición honesta, porque la pregunta es qué
cuesta reindexar después de editar.

**El pase completo cuesta `9,174 s`.** De esos, la fase `staging` -- escribir el
grafo canónico entero -- es `3,529 s`, el `38 %`, y es **lo único** que el delta
evitaba.

**Los costes fijos del delta, medidos contra la base real de `318 MB`,** son
`1,818 s`, que `applyDeltaRoute` paga en cada delta por pequeña que sea la
edición:

|paso|segundos|
|---|---|
|`CanonicalTableCounts`|`0,030`|
|`RefreshSnapshotDigest`|`0,000`|
|`BuildSnapshot` -- el `HotSnapshot` **completo**|`1,788`|
|**total fijo por delta**|**`1,818`**|

**El resultado:**

|ruta|segundos|contra el full|
|---|---|---|
|pase completo|`9,174`|`1,00x`|
|delta **tal como está escrito**|`3,811`|`2,41x`|
|delta **si también verificara**|`5,507`|`1,67x`|

Dos hechos de diseño, no de implementación, fijaban ese techo:

1. **`Update` exigía el set de hechos `Next` completo.** `UpdateOptions.Previous`
   y `Next` son `facts.Set`, y `Plans` sólo alimentaba la elección de ruta. Así
   que el delta no ahorraba ni el arranque, ni los motores de lenguaje, ni el
   `merge`, ni la fase `facts`: `2,00 s` de los `9,17 s` los pagaba igual.
2. **Reconstruía el `HotSnapshot` entero** desde la base mutada. Otro `1,79 s`
   que pagaba igual.

**No verificaba.** `applyDeltaRoute` hacía **cero** llamadas a `integrity` y a
`golden probes`, que la ruta completa sí corre. Aplicaba, contaba tablas,
refrescaba el digest, reconstruía el snapshot y publicaba. Así que la comparación
honesta es la fila de `1,67x`, y parte del `2,41x` medía una verificación
ausente.

**Y no mejoraba con la escala.** `staging`, `merge`, `snapshot` e `integrity`
escalan todos con el corpus; sólo la mutación escalaba con la edición. Un corpus
diez veces mayor multiplica por diez lo que el delta ahorraba **y** lo que
seguía pagando, así que la razón se queda en `1,67x` en cualquier tamaño.

**Ya había cobrado un defecto de corrupción silenciosa.** `LUQUE-2002` y el ADR
0056: un fichero editado perdía **toda arista entrante desde otro fichero**,
porque la retirada borraba el nodo para recrearlo y el motor exige despejar sus
aristas antes. Se arregló, y nadie lo había notado precisamente porque ninguna
ruta de producción llegaba ahí.

**La caché de hechos ya entrega lo que el incremental prometía.** Con ella
caliente los motores de lenguaje son `0,57 s` de los `9,17 s`: ya sólo reparsean
lo que cambió.

## Decisión

El único camino de indexado es la reconstrucción completa. El subsistema del
delta se borra.

Se retira de producción `internal/indexer/delta.go`, `invalidation.go`, `go.go`,
`typescript.go`, `rust.go`, `semantic_changes.go`, `internal/facts/delta.go`,
`internal/syntax/changes.go` y los tres `canonical_mutation*.go` de
`internal/storage/ladybug`; con sus tests, y con el benchmark
`benchmarks/ladybug-incremental`. Son `2.732` líneas de producción, `3.976` de
test y `953` de benchmark.

`1,67x` sobre nueve segundos no paga un subsistema con un modo de fallo de
corrupción silenciosa ni una ruta de publicación que no verifica. Y mantener
código inalcanzable no cuesta cero: cuesta la confianza falsa de que existe una
propiedad del producto que no existe.

Los invariantes que **no** dependían del delta sobreviven con otro vehículo de
escritura. `internal/storage/ladybug/corruption_native_test.go` y
`duplicate_process_linux_test.go` usaban `ApplyCanonicalDelta` para escribir, no
para probar el delta: una base corrupta rechaza una escritura sin añadir daño al
daño, y una base retenida por otro proceso da `ErrDatabaseLocked` nombrando los
PIDs. Los dos invariantes se sostienen, pero no con la misma llamada, y el matiz
importa: `LoadCanonical` se niega **antes** de abrir -- una guarda de `os.Stat`
devuelve `ErrAlreadyExists` sobre una base que ya existe--, así que sostiene la
negativa de escritura y el «no toca el archivo», y por construcción no puede
clasificar un cerrojo ni llegar al motor de una base dañada. La clasificación del
cerrojo la sostiene `Open`, que es donde vive `classifyOpenFailure` y la única
puerta de escritura que llega al motor.

## Consecuencias

- `kivgraph index` sigue aceptando sólo `--full`, y ahora eso es toda la verdad
  del subsistema y no una limitación temporal de la superficie.
- El contrato de retirada que el ADR 0056 estableció -- lo que un fichero afirma
  son las aristas que **salen** de sus símbolos -- deja de describir código
  existente y pasa a ser la condición de partida de cualquier camino incremental
  futuro. No se relaja. `AGENTS.md` lo dice en esos términos.
- Los documentos que citaban el coste del incremental citaban **proyecciones**,
  nunca mediciones de extremo a extremo: nunca hubo un delta que medir. Los que
  siguen vivos se corrigieron; los registros históricos -- las cualificaciones de
  LadybugDB y de release, los ADR 0002, 0003, 0004 y 0006 -- se dejan como el
  registro de lo que se decidió entonces.

### Qué se pierde

- **El benchmark `ladybug-incremental`.** Medía `ApplyCanonicalDelta` y el
  enrutado del delta: altas, bajas, cambios de propiedades, sustitución de
  relaciones salientes, rechazo de duplicados, ausencia de aristas fantasma y
  rollback tras un fallo tardío. Con el código retirado es un benchmark de código
  que no existe. Su evidencia -- `LADYBUG_INCREMENTAL_PASS`,
  `LADYBUG_DELTA_PERFORMANCE_PASS` y los p95 que registraron -- sigue en
  `docs/decisions/ladybugdb-qualification.md`, y el harness en el historial de
  git.
- **La posibilidad de un delta sin rediseñar.** No queda una ruta apagada que
  encender: queda un ADR. Volver a tenerlo es volver a escribirlo.

### Qué haría falta para que un incremental pagara

Camino futuro, no promesa. Las dos condiciones son exactamente las dos que
fijaban el techo:

1. **Un `HotSnapshot` actualizable**, que reciba la mutación en vez de
   reconstruirse entero desde la base.
2. **Un set `Next` acotado**, para no volver a pagar arranque, motores, `merge` y
   `facts` en cada edición.

Con las dos, el ahorro dejaría de estar limitado por lo que el delta conserva. Es
otro diseño, y necesita su propio ADR y su propia medición -- más el contrato de
retirada del ADR 0056 y la verificación (`integrity`, `golden probes`) que esta
ruta se saltaba, que en un incremental nuevo no son opcionales.

## Alternativas descartadas

- **Cablearlo.** Un `kivgraph index` sin `--full` que tomase la ruta, más un test
  end-to-end que indexe, edite, reindexe y compare contra una reconstrucción
  limpia. Es lo que haría falta para saber si la maquinaria hace lo que sus tests
  con dobles dicen -- y el techo medido es `1,67x`, así que el trabajo compra una
  reducción de nueve segundos a cinco y medio, sobre una ruta cuyo modo de fallo
  ya se materializó una vez.
- **Dejarlo sin cablear y no citarlo.** Cuesta cero hoy, y es la alternativa que
  más se parece a no hacer nada. Se descarta porque mantiene un subsistema que da
  confianza falsa: al arreglar `LUQUE-2002` se estuvo a punto de publicar que
  «producción pierde aristas en cada edición», que era **falso**, y sólo se
  descubrió al buscar el llamante. Un subsistema inalcanzable no es neutro; es
  una afirmación implícita sobre el producto que alguien acaba citando.
- **Conservar sólo `ApplyCanonicalDelta` como primitiva de escritura.** Los dos
  tests nativos que la usaban prueban invariantes de la base, no del delta, y
  `LoadCanonical` y `Open` los sostienen entre los dos. Guardar el aplicador por
  ellos sería guardar la mutación canónica incremental entera -- validación,
  retirada, upsert -- por dos escrituras que ya tienen ruta viva.
