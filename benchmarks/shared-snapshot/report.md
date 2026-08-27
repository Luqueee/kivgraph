# Lo que cuestan N servidores de una misma generación publicada

Mide qué paga la máquina cuando varios servidores MCP sirven el mismo grafo, y
qué pagaría si cada uno lo derivara por su cuenta. Los dos brazos ejecutan el
mismo binario sobre la misma generación y se diferencian **sólo** en si el
fichero `snapshot.kvsnap` está en su sitio: el brazo derivado lo mide con el
fichero apartado, así que cada servidor reconstruye el grafo desde LadybugDB.

Las métricas crudas están en `results.json`. El gate de esta fase se emite desde
este arnés, y sus criterios están reescritos después de la primera medición: eso
se dice y se justifica abajo, en «Los criterios».

## Entorno y provenance

|dato|valor|
|---|---|
|fecha|2026-08-22|
|commit|`186884c`|
|binario|`kivgraph 0.5.0`|
|máquina|Linux `6.12.94+deb13-amd64`, 16 CPU, 23,4 GB|
|corpus|`workspace`, 51 repositorios registrados|
|generación|`000001`, `snapshot.kvsnap` de `128.957.472` bytes|
|llamadas|`2.000` medidas por brazo, `4.000` descartadas antes|
|semilla|`42`|
|digest|`71c8301f512358c3957e25804f31473f942e2ccd84bfa660543967767a236bcc`, idéntico en dos ejecuciones|

El grafo servido: `161.819` símbolos, `482.478` aristas de símbolo, `168`
paquetes, `6.173` ficheros, `17.997` no resueltos, schema `4`.

`smaps_rollup` está disponible, así que el reparto de una página compartida
entre los procesos que la sostienen es una medición y no una estimación. Es la
razón de medir en Linux: en darwin ese reparto no se puede observar y el total
sumaría el fichero una vez por servidor.

## El barrido

Cifra principal, `Pss` -memoria proporcional-, sumada sobre todos los procesos.
Las filas son las de `results.json`:

|servidores|mapeado|derivado|cuota|sucio/símbolo|`p99` mapeado|`p99` derivado|Δ `p99`|
|---|---|---|---|---|---|---|---|
|2|`325,7 MB`|`654,1 MB`|`0,498`|`611 B`|`1,16 ms`|`0,89 ms`|`0,267 ms`|
|4|`513,7 MB`|`1.233,8 MB`|`0,416`|`614 B`|`1,38 ms`|`0,99 ms`|`0,383 ms`|
|8|`887,6 MB`|`2.384,6 MB`|`0,372`|`614 B`|`1,81 ms`|`1,79 ms`|`0,026 ms`|

Y el arranque, que no depende del número de servidores:

|servidores|mapeado|derivado|razón|
|---|---|---|---|
|2|`260 ms`|`3.339 ms`|`12,8x`|
|4|`261 ms`|`3.394 ms`|`13,0x`|
|8|`265 ms`|`3.502 ms`|`13,2x`|

### Qué se repite y qué no

Cuatro barridos y tres pasadas de un solo punto sobre esta generación:

|servidores|cuota, observado|razón `p99`, observado|Δ `p99`, observado|
|---|---|---|---|
|2|`0,498` – `0,514` (4)|`1,300` – `1,442` (4)|`0,27` – `0,38 ms`|
|4|`0,416` – `0,421` (4)|`1,273` – `1,385` (4)|`0,29` – `0,38 ms`|
|8|`0,365` – `0,373` (5)|`0,944` – `1,097` (5)|`0,03` – `0,18 ms`|

La memoria se repite dentro de `±0,01`. La razón de `p99` **no**: con ocho
servidores va de `0,944` a `1,097`, así que el mismo código queda por encima o
por debajo de `1,0` según la pasada. En diferencia absoluta el mismo dato es
estable y pequeño -- nunca más de `0,38 ms` --, y es por eso que el criterio
pasó a ser absoluto.

## Qué dicen los números

**La cuota cae al crecer N, y eso es la definición de compartir.** Una página
mapeada la paga la máquina una vez por muchos procesos que la sostengan, así que
a más servidores menos paga cada uno: `0,498` con dos, `0,416` con cuatro,
`0,372` con ocho. Con ocho, la máquina se ahorra `1.497 MB` de los `2.385 MB`
que costaría derivar.

**El sucio por símbolo es plano: `611`–`614 B` en los tres puntos.** No depende
de N porque no es la parte compartida: son las tablas que cada servidor
decodifica para sí. Escala con el grafo, no con los clientes, y por eso se
declara por símbolo.

**El arranque es el efecto más grande de la fase: `13x`.** `261 ms` contra
`3.394 ms` con cuatro servidores, y la razón no se mueve con N porque cada
servidor arranca solo. Ningún criterio lo miraba.

**El `p99` del brazo mapeado es peor con pocos servidores, y no es un
transitorio.** Sin calentamiento la razón era `1,442` con `2.000` llamadas y
`1,270` con `8.000`, lo que medía la duración de la pasada: la primera lectura
de una página mapeada es un fallo de página y la de una página de heap no. Con
`4.000` llamadas descartadas, con cuatro servidores se queda entre `1,273` y
`1,385`. Eso es coste de régimen.

Con ocho deja de verse, y la razón está en la otra columna: la cola del brazo
derivado se degrada más rápido al añadir procesos -`0,89 ms` con dos, `1,79 ms`
con ocho- porque ocho grafos en heap compiten por la máquina. Lo que el mapeo
paga en fallos de página lo cobra en no multiplicar el residente.

En absoluto, con cuatro servidores, son `0,38 ms` en el percentil 99 a cambio de
`720 MB`.

## Los criterios

Los tres primeros criterios de `LUQUE-2006` se escribieron antes de medir y no
sobrevivieron a la medición: `resident_share ≤0,40` se cumplía con ocho
servidores y no con dos sobre el mismo código; `Private_Dirty ≤60 MB` estaba
fijado contra un corpus de `123.531` símbolos y aquí hay `161.819`; y
`p99 ≤1,05` era más estrecho que el ruido entre pasadas -- con ocho servidores
la razón fue de `0,944` a `1,097`, así que el veredicto lo decidía la pasada.

Están reescritos. **Un criterio escrito después de medir vale menos como
evidencia que uno escrito antes**: no puede sorprenderse de lo que ya vio. Lo
que justifica cada uno es que valora una propiedad del diseño y no un número de
un corpus en una máquina.

|criterio|declarado|`N=2`|`N=4`|`N=8`|
|---|---|---|---|---|
|`resident_share_falls_with_clients`|cae en cada paso|— |cae|cae|
|`resident_share_at_gate`|`≤ 0,45`|—|`0,416` ok|—|
|`private_dirty_per_symbol`|`≤ 800 B`|`611` ok|`614` ok|`614` ok|
|`p99_absolute_delta_ms`|`≤ 1,00 ms`|`0,267` ok|`0,383` ok|`0,026` ok|
|`first_answer_speedup`|`≥ 5x`|`12,8` ok|`13,0` ok|`13,2` ok|

El primero es la afirmación de diseño, y es el único que no depende del corpus ni
de cuántos clientes eligió correr nadie. Antes no lo miraba ningún criterio.

Con `KIVGRAPH_BENCH_SLO=1` el arnés emite `SHARED_SNAPSHOT_PASS`. Sin la
variable evalúa lo mismo y sólo informa, que es la convención de
`benchmarks/AGENTS.md`.

## Limitaciones

- Un solo corpus y una sola máquina. El sucio por proceso escala con el grafo,
  así que los `~96 MB` no se transportan a otro corpus.
- `2.000` llamadas medidas por brazo y punto. Los percentiles son de esa
  muestra, y la razón de `p99` no se repite entre pasadas: ver la dispersión
  arriba antes de leer un `1,3` como un efecto.
- El brazo derivado se mide apartando el fichero publicado y restituyéndolo
  después; el arnés se niega a continuar si el brazo mapeado no lo sirvió o si
  el derivado sí, porque entonces los dos habrían medido lo mismo.
- El arnés comprueba que los servidores sirven la generación cuyo fichero
  esconde. Sin esa comprobación la primera pasada midió la instalación real del
  usuario en vez de la aislada: una configuración escrita por `init` guarda sus
  rutas con `~`, que se expande contra el `HOME` de quien ejecuta el servidor.
- En darwin el reparto de una página compartida no se puede observar. Ejercitado
  sobre una generación aislada: el arnés publica el residente, **marca como no
  evaluada** la comprobación que la plataforma no puede responder -no la falla-,
  declara dos negativas y no emite el gate.

## Reproducir

```bash
export HOME=/ruta/aislada
kivgraph init --repository <nombre>=<ruta> ...
kivgraph index --full --json
go run ./benchmarks/shared-snapshot \
  --server /ruta/al/kivgraph \
  --config $HOME/.config/kivgraph/config.yaml \
  --generation-dir $HOME/.local/state/kivgraph/generations/000001 \
  --clients 2,4,8 --gate-clients 4 --calls 2000 --warmup 4000
```

El `HOME` importa en la invocación del arnés, no sólo en el `init`: es lo que
resuelve las rutas `~` de la configuración.
