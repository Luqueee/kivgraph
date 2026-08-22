# Lo que cuestan N servidores de una misma generación publicada

Mide qué paga la máquina cuando varios servidores MCP sirven el mismo grafo, y
qué pagaría si cada uno lo derivara por su cuenta. Los dos brazos ejecutan el
mismo binario sobre la misma generación y se diferencian **sólo** en si el
fichero `snapshot.kvsnap` está en su sitio: el brazo derivado lo mide con el
fichero apartado, así que cada servidor reconstruye el grafo desde LadybugDB.

Las métricas crudas están en `results.json`. Este informe no emite ningún
veredicto de aceptación: mide una decisión de diseño sobre un corpus concreto.

## Entorno y provenance

|dato|valor|
|---|---|
|fecha|2026-08-22|
|commit|`2713fe5`|
|binario|`kivgraph 0.5.0`|
|máquina|Linux `6.12.94+deb13-amd64`, 16 CPU, 23,4 GB|
|corpus|`kena-workspace`, 51 repositorios registrados|
|generación|`000001`, `snapshot.kvsnap` de `128.957.472` bytes|
|llamadas|`2.000` medidas por brazo, `4.000` descartadas antes|
|semilla|`42`|
|digest|`be4ccde29d04588f36efb5fbade5bb90219f29722cd2e6507fa34f68e883770a`|

El grafo servido: `161.819` símbolos, `482.478` aristas de símbolo, `168`
paquetes, `6.173` ficheros, `17.997` no resueltos, schema `4`.

`smaps_rollup` está disponible, así que el reparto de una página compartida
entre los procesos que la sostienen es una medición y no una estimación. Es la
razón de medir en Linux: en darwin ese reparto no se puede observar y el total
sumaría el fichero una vez por servidor.

## El barrido

Cifra principal, `Pss` -memoria proporcional-, sumada sobre todos los procesos.
Las filas son las de `results.json`, que es la tercera de tres pasadas:

|servidores|mapeado|derivado|cuota|sucio por proceso|`p99` mapeado|`p99` derivado|razón `p99`|
|---|---|---|---|---|---|---|---|
|2|`327,8 MB`|`655,7 MB`|`0,500`|`96,2 MB`|`1,15 ms`|`0,85 ms`|`1,347`|
|4|`518,1 MB`|`1.229,3 MB`|`0,421`|`97,7 MB`|`1,41 ms`|`1,04 ms`|`1,354`|
|8|`884,7 MB`|`2.383,0 MB`|`0,371`|`95,0 MB`|`1,98 ms`|`2,10 ms`|`0,944`|

Y el arranque, que no depende del número de servidores:

|servidores|primera respuesta mapeado|primera respuesta derivado|
|---|---|---|
|2|`259 ms`|`3.551 ms`|
|4|`286 ms`|`3.464 ms`|
|8|`276 ms`|`3.532 ms`|

### Qué se repite y qué no

Tres pasadas del mismo barrido, mismo digest de entradas:

|servidores|cuota|razón `p99`|
|---|---|---|
|2|`0,509` · `0,500`|`1,442` · `1,347`|
|4|`0,420` · `0,421` · `0,421`|`1,273` · `1,273` · `1,354`|
|8|`0,365` · `0,371` · `0,371`|`1,097` · `1,095` · `0,944`|

La memoria se repite dentro de `±0,01`. El `p99` **no**: con ocho servidores va
de `0,944` a `1,097`, así que ahí la diferencia es ruido de pasada y no un
efecto. Con dos y cuatro sí se repite el signo.

## Qué dicen los números

**La cuota mejora al crecer N, y eso es la definición de compartir.** La parte
compartida se reparte entre los procesos que la sostienen, así que a más
servidores menos paga cada uno: `0,500` con dos, `0,421` con cuatro, `0,371` con
ocho. Con ocho servidores la máquina se ahorra `1.498 MB` de los `2.383 MB` que
costaría derivar.

**El sucio por proceso es plano: ~`96 MB` en los tres puntos.** No depende de N
porque no es la parte compartida: son las tablas que cada servidor decodifica
para sí. Es una propiedad del corpus, no del número de clientes.

**El `p99` del brazo mapeado es peor con pocos servidores, y eso no es un
transitorio.** Sin calentamiento la razón era `1,442` con `2.000` llamadas y
`1,270` con `8.000`, lo que medía la duración de la pasada: la primera lectura
de una página mapeada es un fallo de página y la de una página de heap no. Con
`4.000` llamadas descartadas antes de medir, con cuatro servidores se queda en
`1,273`, `1,273` y `1,354`. Eso es coste de régimen.

Con ocho deja de verse -`1,097`, `1,095`, `0,944`-, y la razón está en la otra
columna: la cola del brazo derivado se degrada más rápido al añadir procesos
(`0,85 ms` con dos, `2,10 ms` con ocho) porque ocho grafos en heap compiten por
la máquina. Lo que el mapeo paga en fallos de página lo cobra en no multiplicar
el residente.

En absoluto, con cuatro servidores, son `0,37 ms` en el percentil 99 a cambio de
`711 MB`.

## Los tres criterios declarados, contra lo medido

|criterio|declarado|`N=2`|`N=4`|`N=8`|
|---|---|---|---|---|
|`resident_share`|`≤ 0,40`|`0,500`|`0,421`|`0,371` ok|
|`private_dirty_per_process`|`≤ 60 MB`|`96,2`|`97,7`|`95,0`|
|`p99_not_worse`|`≤ 1,05`|`1,347`|`1,354`|`0,944` ok|

El gate se decide con cuatro servidores y **no se emite**. Los tres criterios se
evalúan siempre y sólo se exigen con `KIVGRAPH_BENCH_SLO=1`, que es la
convención de `benchmarks/AGENTS.md`.

Lo que la medición dice de los criterios mismos:

- `resident_share ≤ 0,40` no es una propiedad del diseño, es una propiedad de N.
  Se cumple con ocho servidores y no con dos, sobre exactamente el mismo código.
- `private_dirty ≤ 60 MB` estaba fijado contra un corpus más pequeño
  (`123.531` símbolos) y aquí hay `161.819`. Es un número por corpus, no un
  límite.
- `p99 ≤ 1,05` es más estrecho que el ruido de la propia medición: con ocho
  servidores las tres pasadas dan `1,097`, `1,095` y `0,944`, así que el mismo
  código pasa o no pasa según la pasada. Y con cuatro rechazaría un diseño que
  cuesta `0,37 ms` en la cola y ahorra `711 MB`.

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
