# Lo que el camino incremental podría ahorrar

> **Resultado.** Esta medición decidió `LUQUE-2003`: el camino incremental se
> **retiró** ([ADR 0057](../../docs/adr/0057-el-camino-incremental-se-retira.md)).
> Lo que sigue describe en presente un código que ya se borró; se conserva
> intacto porque es la evidencia que sostiene la decisión.

`LUQUE-2003` pregunta si el camino del delta se cablea o se retira. Este
benchmark mide la única cifra que decide eso: **el techo**. No mide un delta
-nada lo llama, así que no hay tal medición que tomar-, mide qué pasos de un
pase completo se salta y cuáles sigue pagando, y esos se miden hoy contra una
base real.

Las métricas crudas están en `results.json`. No se emite ningún veredicto de
aceptación.

## Entorno y provenance

|dato|valor|
|---|---|
|fecha|2026-08-21|
|commit|`e78490e`|
|máquina|`Mac17,2` (Apple M5), 10 CPU, macOS `26.6`|
|toolchains|`go1.26.4`, LadybugDB `v0.13.1`, `node v25.2.1`|
|corpus|`kena`, 37 repositorios git, 35 indexados|
|tamaño|`4.683` ficheros, `120.461` símbolos, `477.027` aristas, base de `318 MB`|
|caché de hechos|**caliente**|

La caché caliente es deliberada y es la condición honesta: la pregunta es qué
cuesta **reindexar después de editar**, no qué cuesta una construcción en frío.
Con la caché caliente los motores de lenguaje ya sólo reparsean lo que cambió,
que es precisamente lo que un camino incremental prometía.

## El pase completo, por fase

Los eventos de progreso marcan el **inicio** de cada paso, así que la duración
de un paso es la distancia hasta el siguiente evento.

|fase|segundos|% del pase|¿la salta el delta?|
|---|---|---|---|
|arranque hasta el primer evento|`0,717`|7,8 %|no|
|motores de lenguaje (`ts` + `go` + `rust`)|`0,570`|6,2 %|no|
|`merge`|`0,488`|5,3 %|no|
|`facts`|`0,218`|2,4 %|no|
|`staging` -- escribir el grafo canónico entero|`3,529`|38,5 %|**sí**|
|`snapshot`|`1,909`|20,8 %|no|
|`integrity`|`1,696`|18,5 %|**sí**|
|`golden probes`|`0,047`|0,5 %|**sí**|
|**total**|**`9,174`**|100 %||

## Los costes fijos del delta, medidos

`applyDeltaRoute` corre estos tres pasos en **cada** delta, por pequeña que sea
la edición, y los tres escalan con el corpus y no con la edición. Medidos contra
la base real de `477.027` aristas, el mejor de tres:

|paso|segundos|
|---|---|
|`CanonicalTableCounts`|`0,030`|
|`RefreshSnapshotDigest`|`0,000`|
|`BuildSnapshot` -- el `HotSnapshot` **completo**|`1,788`|
|**total fijo por delta**|**`1,818`**|

`ApplyCanonicalDelta` no se cronometra aquí: escala con la edición, y construir
un delta a escala de `kena` exige salida real del cargador para `kena`. Los
costes fijos ya deciden la pregunta, y hay una cota: leer las dieciocho tablas de
relación cuesta `0,030 s`, así que el trabajo por tabla en esta base está en el
orden de `2 ms`.

## El resultado

|ruta|segundos|contra el full|
|---|---|---|
|pase completo|`9,174`|`1,00x`|
|delta **tal como está escrito**|`3,811`|`2,41x`|
|delta **si también verificara**|`5,507`|`1,67x`|

Dos cosas hacen ese techo, y las dos son de diseño, no de implementación:

1. **`Update` exige el set de hechos `Next` completo.** `UpdateOptions.Previous`
   y `Next` son `facts.Set`, y `Plans` sólo alimenta la elección de ruta. Así que
   el delta no ahorra ni el arranque, ni los motores, ni el `merge`, ni la fase
   `facts`: `2,00 s` de los `9,17 s` los paga igual.
2. **Reconstruye el `HotSnapshot` entero** desde la base mutada. Otro `1,79 s`
   que paga igual.

Lo único que ahorra de verdad es `staging`, y `staging` es el `38 %`. El resto de
su ventaja aparente -`1,70 s`- **es que no verifica**.

## La asimetría de integridad

La ruta completa corre etapas `integrity` y `golden probes`. `applyDeltaRoute`
hace **cero** llamadas a cualquiera de las dos: aplica, cuenta tablas, refresca
el digest, reconstruye el snapshot y publica. Así que la comparación honesta es
la fila de `1,67x`, y la de `2,41x` mide en parte una verificación ausente en la
ruta que ya demostró tener un defecto de corrupción silenciosa (`LUQUE-2002`).

## Cómo escala

`staging`, `merge`, `snapshot` e `integrity` escalan todos con el corpus; sólo la
mutación escala con la edición. Un corpus diez veces mayor multiplica por diez lo
que el delta ahorra **y** lo que sigue pagando, así que **la razón se queda donde
está**: `1,67x`, no un orden de magnitud. Un camino incremental que de verdad
pagara necesitaría un `HotSnapshot` actualizable y un set `Next` acotado -- otro
diseño, no este código.

## Reproducir

```bash
export HOME=/private/tmp/costhome
kivgraph init --languages go,typescript,rust --repository <nombre>=<ruta> ...
kivgraph index --full --json                       # poblar la caché de hechos
kivgraph index --full --json | ts -s '%.s'         # el pase medido, con marcas

go run -tags ladybug ./benchmarks/incremental-cost \
  -database "$HOME/.local/state/kivgraph/generations/000002/graph.db" -repeats 3
```

## Limitaciones

- El camino del delta **no tiene llamante en producción**, así que su coste está
  proyectado a partir de los pasos que `applyDeltaRoute` corre demostrablemente.
  Nunca se ha medido de extremo a extremo, y no se puede hasta que se cablee.
- `ApplyCanonicalDelta` no está cronometrado; queda acotado, no medido.
- Un corpus, una máquina, una caché caliente. En frío los motores de lenguaje
  pasan de `0,57 s` a minutos, y entonces **todas** las razones de aquí quedan
  mejor de lo que son -- el ahorro relativo del delta caería aún más.
- Las cifras del pase completo salen de una ejecución con marcas de tiempo por
  evento, no de una distribución.
