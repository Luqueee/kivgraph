# Lo que cuesta en latencia quitarle los mapas al snapshot

`LUQUE-2003` retira los cuatro mapas de `GraphSnapshot` y los sustituye por
arrays planos con búsqueda binaria. La pregunta que este banco responde es la que
puede hundir el cambio: **¿se paga en latencia de consulta?**

La puerta de la tarea es `p50` y `p99` no más de un `5 %` peor, con el mismo
corpus y la misma semilla. Las cifras crudas de los dos brazos están en
`results.json`.

## Entorno y provenance

|dato|valor|
|---|---|
|fecha|`2026-08-22`|
|comando|`go run ./benchmarks/mcp-client --clients 4`|
|commit (después)|`485efc8` más este cambio|
|corpus|el sintético que genera `mcp-client`, semilla por defecto|
|clientes|`4`, sobre `stdio`|

Los dos brazos comparten `dataset` y `workload` byte a byte; el arnés lo verifica
antes de comparar.

## El resultado

|métrica|antes (mapas)|después (planos)|delta|puerta `5 %`|
|---|---|---|---|---|
|`p50` ida y vuelta|`0,0350 ms`|`0,0349 ms`|`-0,24 %`|pasa|
|`p95` ida y vuelta|`0,2185 ms`|`0,2181 ms`|`-0,15 %`|pasa|
|`p99` ida y vuelta|`0,2323 ms`|`0,2321 ms`|`-0,09 %`|pasa|

**No cuesta nada, y la cola mejora.** Los percentiles de ida y vuelta no se
mueven -las tres diferencias son ruido- pero eso es porque están dominados por el
transporte `stdio`: un cambio de decenas de nanosegundos en el backend está cerca
del suelo de lo que pueden mostrar.

Donde se ve es en el backend, que es el trabajo del snapshot y nada más:

|operación|`p50` antes|`p50` después|`p99` antes|`p99` después|
|---|---|---|---|---|
|`find_symbol`|`709 ns`|`709 ns`|`2.417 ns`|`1.708 ns` (`-29 %`)|
|`get_symbol`|`500 ns`|`500 ns`|`1.291 ns`|`791 ns` (`-39 %`)|
|`find_references`|`4.791 ns`|`4.792 ns`|`12.583 ns`|`9.375 ns` (`-25 %`)|
|`find_cross_repo_consumers`|`4.958 ns`|`4.875 ns`|`16.584 ns`|`10.417 ns` (`-37 %`)|
|`get_blast_radius`|`19.083 ns`|`19.083 ns`|`69.125 ns`|`55.458 ns` (`-20 %`)|

El `p50` es idéntico y el `p99` baja entre un `20 %` y un `39 %` en las cinco
operaciones. Ése es el patrón que cabía esperar y conviene decir por qué: un mapa
tiene un caso medio muy bueno y una cola que depende del sondeo -cuántos buckets
hay que recorrer y cuántas claves comparar- mientras que una búsqueda binaria
sobre enteros contiguos no tiene hash, ni bucket, ni comparación de clave, y su
peor caso está acotado por `log₂ n`. Se cambia una media buena por una cola
acotada, y a este tamaño la media no empeora.

El caudal sube un `1,9 %` (`69.704` → `71.042` llamadas por segundo), que es del
mismo orden que el ruido y no se reclama como resultado.

## Limitaciones

- **Una máquina, una semilla, cuatro clientes**, y el corpus es el sintético que
  genera el arnés, no `workspace`. Un corpus con muchas más claves distintas mueve el
  `log₂ n` de la búsqueda binaria y este banco no lo mide.
- **Los percentiles de ida y vuelta no son sensibles a este cambio.** Se publican
  porque son la puerta declarada en la tarea, pero la evidencia real son los
  percentiles de backend por operación.
- Las asignaciones por operación no cambian dentro del ruido (`283,6` → `283,7`);
  los bytes por operación suben un `0,7 %` (`31.633` → `31.849`) y esta pasada
  **no lo atribuye**.

## Reproducir

```bash
go run ./benchmarks/mcp-client --clients 4 --output /tmp/mcp-arm
```

Con el mismo `--seed` en los dos brazos, que es el valor por defecto.
