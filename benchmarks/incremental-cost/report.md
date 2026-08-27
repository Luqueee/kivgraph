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
|commit|`2808ea9`|
|máquina|`Mac17,2` (Apple M5), 10 CPU, macOS `26.6`|
|toolchains|`go1.26.4`, LadybugDB `v0.13.1`, `node v25.2.1`|
|corpus|`workspace`, 37 repositorios git, 35 indexados, **los tres lenguajes cargados**|
|tamaño|`4.768` ficheros, `123.524` símbolos, `493.521` aristas|
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
|arranque hasta el primer evento|`0,870`|8,7 %|no|
|motores de lenguaje (`ts` + `go` + `rust`)|`0,625`|6,2 %|no|
|`merge`|`0,611`|6,1 %|no|
|`facts`|`0,259`|2,6 %|no|
|`staging` -- escribir el grafo canónico entero|`3,719`|37,1 %|**sí**|
|`snapshot`|`2,048`|20,4 %|no|
|`integrity`|`1,853`|18,5 %|**sí**|
|`golden probes`|`0,051`|0,5 %|**sí**|
|**total**|**`10,036`**|100 %||

## Los costes fijos del delta, medidos

`applyDeltaRoute` corre estos tres pasos en **cada** delta, por pequeña que sea
la edición, y los tres escalan con el corpus y no con la edición. Medidos contra
la base real de `493.521` aristas, el mejor de tres:

|paso|segundos|
|---|---|
|`CanonicalTableCounts`|`0,034`|
|`RefreshSnapshotDigest`|`0,000`|
|`BuildSnapshot` -- el `HotSnapshot` **completo**|`1,913`|
|**total fijo por delta**|**`1,947`**|

`ApplyCanonicalDelta` no se cronometra aquí: escala con la edición, y construir
un delta a escala de `workspace` exige salida real del cargador para `workspace`. Los
costes fijos ya deciden la pregunta, y hay una cota: leer las dieciocho tablas de
relación cuesta `0,034 s`, así que el trabajo por tabla en esta base está en el
orden de `2 ms`.

## El resultado

|ruta|segundos|contra el full|
|---|---|---|
|pase completo|`10,036`|`1,00x`|
|delta **tal como está escrito**|`4,312`|`2,33x`|
|delta **si también verificara**|`6,165`|`1,63x`|

Dos cosas hacen ese techo, y las dos son de diseño, no de implementación:

1. **`Update` exige el set de hechos `Next` completo.** `UpdateOptions.Previous`
   y `Next` son `facts.Set`, y `Plans` sólo alimenta la elección de ruta. Así que
   el delta no ahorra ni el arranque, ni los motores, ni el `merge`, ni la fase
   `facts`: `2,37 s` de los `10,04 s` los paga igual.
2. **Reconstruye el `HotSnapshot` entero** desde la base mutada. Otro `1,91 s`
   que paga igual.

Lo único que ahorra de verdad es `staging`, y `staging` es el `37 %`. El resto de
su ventaja aparente -`1,85 s`- **es que no verifica**.

## La asimetría de integridad

La ruta completa corre etapas `integrity` y `golden probes`. `applyDeltaRoute`
hace **cero** llamadas a cualquiera de las dos: aplica, cuenta tablas, refresca
el digest, reconstruye el snapshot y publica. Así que la comparación honesta es
la fila de `1,63x`, y la de `2,33x` mide en parte una verificación ausente en la
ruta que ya demostró tener un defecto de corrupción silenciosa (`LUQUE-2002`).

## Cómo escala

`staging`, `merge`, `snapshot` e `integrity` escalan todos con el corpus; sólo la
mutación escala con la edición. Un corpus diez veces mayor multiplica por diez lo
que el delta ahorra **y** lo que sigue pagando, así que **la razón se queda donde
está**: `1,63x`, no un orden de magnitud. Un camino incremental que de verdad
pagara necesitaría un `HotSnapshot` actualizable y un set `Next` acotado -- otro
diseño, no este código.

## Corrección: la primera medición no llevaba Rust

La primera pasada de este benchmark indexó `workspace` **sin Rust**. El `PATH` del
harness llevaba `CARGO_HOME` pero no `cargo`, así que `rust-analyzer` rechazó los
dos workspaces Cargo y el pase publicó el resto. Kivgraph lo dijo -- `not_loaded=2`
en su JSON y en su salida humana, que es exactamente lo que su contrato promete
hacer con una ausencia-- y el harness lo ignoró. El error es del harness, dos
veces: por no poner `cargo` en el `PATH` y por no leer el contador que él mismo
imprimía.

|dato|primera medición|esta|
|---|---|---|
|corpus|`4.683` ficheros, `120.461` símbolos, `477.027` aristas|`4.768`, `123.524`, `493.521`|
|pase completo|`9,174 s`|`10,036 s`|
|`staging`|`3,529 s`, `38,5 %`|`3,719 s`, `37,1 %`|
|motores|`0,570 s`, `6,2 %`|`0,625 s`, `6,2 %`|
|delta tal como estaba|`3,811 s`, `2,41x`|`4,312 s`, `2,33x`|
|delta verificando|`5,507 s`, `1,67x`|`6,165 s`, `1,63x`|

**La conclusión no se mueve, y de hecho se refuerza.** Los hechos de Rust se
cachean como los de cualquier otro lenguaje, así que un pase caliente crece
`0,861 s` y ninguna proporción se mueve más de un punto y medio. El ADR 0057
retiró el camino incremental por un techo de `1,67x`; el techo real era `1,63x`.

## Reproducir

```bash
export HOME=/private/tmp/costhome
# cargo en el PATH, o rust-analyzer rechaza los workspaces y el corpus sale sin Rust
export PATH="$HOME/.cargo/bin:$PATH"
kivgraph init --languages go,typescript,rust --repository <nombre>=<ruta> ...
kivgraph index --full --json                       # poblar la caché de hechos
kivgraph index --full --json | ts -s '%.s'         # el pase medido, con marcas

go run -tags ladybug ./benchmarks/incremental-cost \
  -database "$HOME/.local/state/kivgraph/generations/$(cat "$HOME/.local/state/kivgraph/CURRENT")/graph.db" -repeats 3
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
