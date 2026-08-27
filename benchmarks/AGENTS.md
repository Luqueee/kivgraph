# Instrucciones de los benchmarks (`benchmarks/`)

Estas reglas se suman a las de `AGENTS.md` en la raíz del repositorio, que se
leen siempre. Una instrucción de este archivo puede añadir restricciones; nunca
puede relajar un contrato de integridad, compatibilidad o verificación
declarado en la raíz.

- Los benchmarks viven en `benchmarks/<nombre>/`, con `results.json` y
  `report.md`. Deben conservar comando, commit, entorno, dataset, semilla,
  métricas y limitaciones.
- Los benchmarks de observabilidad deben separar la ruta local, el proveedor
  `noop` y cualquier proveedor SDK configurado explícitamente; no se deben
  presentar como un único coste de producción.
- El benchmark end-to-end del visor se versiona en
  `benchmarks/web-viewer/`; el harness falla cerrado ante una métrica fuera de
  límite y no emite `WEB_VIEWER_PERFORMANCE_PASS` si el corpus o GPU no
  coinciden con la referencia declarada.
- `benchmarks/tool-honesty/` no mide aristas: mide **qué afirma una tool cuando
  su respuesta está vacía**, conduciendo el binario real por MCP contra un
  corpus con puntos ciegos a propósito. Los repositorios limpios son la mitad
  del diseño: sin ellos, un veredicto constante `LOWER_BOUND` pasaría todas las
  comprobaciones. Los dos lenguajes van en un solo corpus para poder comprobar
  que el veredicto no se contagia entre ellos, y cada brazo declara su propio
  ámbito ciego: la pasada se niega si alguno perdió el suyo. El ámbito se lee
  de `graph_status`, no del fixture. El brazo Rust se salta declarándose
  cuando falta su toolchain, y preserva `RUSTUP_HOME` porque un `HOME` aislado
  deja a `rustup` sin toolchains.
- `benchmarks/snapshot-heap` tampoco mide páginas residentes: separa, en lo que
  cuesta cargar un snapshot publicado, los bytes que un lector **conserva** de
  los que la carga asigna y tira. Toma el perfil con el snapshot **vivo**, que
  es la única forma de atribuirlo: el benchmark del paquete escribe el suyo
  cuando ya es inalcanzable y no atribuye ni un byte.
- Y las dos mitades **no son la misma cifra en `Private_Dirty`**, que es lo que
  este archivo decía. Sólo la que se conserva lo es en régimen estacionario:
  `benchmarks/load-cost-resident` retiró `60,5 MB` de la mitad transitoria y el
  residente por servidor no se movió (`71,76 MB` contra `71,22`, tres pares de
  tres). Bajar lo asignado compra tiempo hasta la primera respuesta; los bytes
  por proceso se bajan moviendo una estructura al fichero mapeado, y sólo eso.
  Quien escriba una cifra de `snapshot-heap` en una ficha de memoria residente
  está citando la magnitud equivocada.
- `benchmarks/load-cost-resident` corre en un contenedor Linux y **no es el host
  de referencia**: lo que hace comparables sus unidades es el page size de
  `4096` bytes, y lo que hace comparables sus dos brazos es que corrieron en la
  misma VM contra el mismo fichero. No sobrescribe los artefactos de
  `shared-snapshot`, y no afirma ningún límite de latencia.
- `benchmarks/daemon-cost` responde qué cuesta un proceso sirviendo a N clientes
  contra N procesos sirviendo a uno. Lo que publica como respuesta es la
  **pendiente por cliente**, no ningún total: un brazo que ahorrara a dos
  clientes y no a ocho parecería una victoria en cualquier fila suelta. Mide el
  recuento de **un** cliente aunque un demonio no comparta nada allí, porque es
  donde su coste fijo sería visible sin nada que amortizarlo -- y ahí resultó que
  empata, desmintiendo la predicción del ADR 0065.
- **`daemon-cost` mide las dos puertas del demonio a tres cargas y publica un
  artefacto por combinación**: `results-idle.json` y `results-http-idle.json` sin
  ninguna llamada, `results.json` y `results-http.json` con `8`, y
  `results-http-sustained.json` con `2.000`. El transporte y los recuentos entran
  en el digest; sin eso las corridas colisionan en una identidad y una cifra de
  socket puede citarse como si fuera alcanzable. El esquema es `daemon-cost-v3`.
- **La carga cero no es un extremo teórico: es la mediana.** `48` de `51`
  servidores reales no reciben ninguna llamada, así que `-calls 0` mide el caso
  que predomina. Ahí el arranque resultó ser el coste -- `33 MB` por cliente sin
  contestar nada, contra `40` contestando-- y eso se arregló: el ADR 0067 movió la
  lectura del grafo a la primera consulta, y la cifra ociosa vigente es `10 MB`
  por cliente contra `39` contestando y `66` bajo tráfico sostenido. Es también la
  única carga en la que los cuatro puntos del barrido miden lo mismo por cliente,
  porque `-calls N` reparte N llamadas entre los clientes que haya.
- **Un árbol sucio no publica un commit a secas.** `commit` lleva `-dirty` cuando
  hay cambios sin commitear y `-unknown` cuando no se pudo saber, y las dos
  variantes se declaran en `limitations`. Es el caso normal -- las cifras que
  justifican un cambio se miden antes de commitearlo-- y sin el sufijo el
  artefacto atribuye sus números a un código que no ejecutó. Las corridas
  publicadas se hacen desde un árbol limpio.
- **Un guardia no puede ser carga.** El `graph_status` que prueba que los dos
  brazos sirven la misma generación corre **después** del muestreo, no antes: nada
  obliga a que preceda a los bytes, y preguntando primero la carga cero era
  imposible de medir. Falla y descarta igual. Ese movimiento es lo que subió el
  esquema de `v2` a `v3`: un fichero `v2` incluye esa llamada en sus bytes.
- **Lo que no se midió no se publica como cero.** Los percentiles, el
  `new_client_ms` y los ratios de latencia son punteros y desaparecen del fichero
  cuando la corrida no preguntó nada; el resumen imprime `--`. Un `p50_ms: 0` se
  lee como una respuesta instantánea y un `p99_ratio: 0` como un demonio
  infinitamente más rápido. Lo que sí se mide a toda carga es
  `new_client_connect_ms`, que es el campo con el que dos cargas se comparan.
- Un probe que sólo existe en el camino de un servidor real -- los de
  `startServer` y `connect`-- no lo caza ningún test local: borrarlo no rompe nada
  en un portátil. Por eso la corrida se niega a publicar un fichero ocioso que
  haya cronometrado algún primer answer, y ese rechazo sí tiene test.
- **La carga se cuenta, no se elige, y es la variable que decidió el resultado
  dos veces.** El event log de un `serve` registra cada llamada de tool, así que
  la carga de una sesión real se recuenta de un log de uso -- la orden está en el
  informe. Medido: mediana de **una** llamada por sesión y `48` de `51`
  servidores sin ninguna. Las `2.000` llamadas del caso sostenido son tres
  órdenes de magnitud por encima de eso, y ahí HTTP parecía costar `12,5 MB` por
  cliente cuando a carga real cuesta menos de `1 MB`, igual que el socket. Un
  benchmark que mide la carga equivocada no es impreciso: **contesta otra
  pregunta**, y en este caso subestimaba el ahorro.
- El caso sostenido se conserva en `results-http-sustained.json` porque es un
  techo útil, y **su cifra no se transporta entre corpus**: `4,9`–`5,9 MB` por
  cliente sobre `117.499` símbolos contra `12,8` sobre `108.737`, reproducido dos
  veces en corpus de ese tamaño. Esa dependencia del corpus es la firma de un
  coste en bytes retenidos -- el buffer de reanudación de `10 MiB` que el SDK da a
  cada sesión-- y no de un coste por sesión. Citar el techo como si fuera el coste
  es el error que este benchmark ya cometió.
- No es un brazo de `shared-snapshot` y no debe convertirse en uno: los brazos de
  aquél se definen por si el fichero de snapshot está, y su gate mide mapear
  contra derivar. Un tercer brazo dejaría su comparación sin significado.
- Una cifra por símbolo se lee de la pasada que la produjo, nunca cruzada entre
  corpus. Las corridas vigentes de `daemon-cost` usan los `108.737` símbolos de
  `workspace` en su generación `000001`; las de `117.499` están en el historial, no en
  estas tablas. `workspace` es un workspace en uso, así que un reindexado posterior no
  reproduce el recuento anterior: el corpus se declara por pasada.

## Corpus y auditorías

- Los corpus sintéticos de aceptación de gran escala se generan en una ruta
  privada y nunca sustituyen ni modifican repositorios indexados. Para
  LadybugDB, la reproducibilidad debe distinguir entre hechos lógicos
  (conteos, schema e integridad) y bytes físicos del archivo nativo.
- Una auditoría de exactitud debe separar `false exact edges` de aristas
  colgantes: compara fixtures con ground truth para las primeras y ejecuta las
  invariantes canónicas de extremos, evidencia y procedencia para las segundas.
- Un informe `ACCEPT_KIVGRAPH_WITH_LIMITS` debe enumerar plataforma,
  toolchains, corpus, transporte, garantías, métricas y riesgos residuales;
  no puede convertir una limitación conocida en un PASS implícito.

`dist/` y los repositorios indexados nunca se usan como entrada: se generan
copias o fixtures privados.

## Un límite de latencia no se afirma en `go test ./...`

Un SLO es una propiedad de la máquina que lo mide, y `go test ./...` corre donde
sea: en un runner compartido con dos trabajos más al lado. Afirmarlo ahí produce
un fallo que no describe el código.

Ocurrió: la release `v0.2.0` cayó con `find_cross_repo_consumers` en `p95` de
`11,6 ms` contra un límite de `5 ms`, **sobre el mismo commit cuyo CI acababa de
pasar** en otro runner, y con cinco de cinco pasadas locales por debajo del
límite. El código no había cambiado entre las dos observaciones.

Así que el límite se comprueba cuando alguien lo pide -`KIVGRAPH_BENCH_SLO=1`,
que es lo que significa una puerta de benchmark- y se informa el resto del
tiempo. Lo que sí se afirma siempre es que la medición ocurrió: un harness que
dejara de evaluar sus comprobaciones pasaría igual, y eso es peor que un límite
incumplido.

La misma regla vale para cualquier gate que dependa del entorno -`rust-src`
instalado, una GPU, un corpus- y por el mismo motivo: la ausencia se declara y se
salta, nunca se convierte en un `FAIL` que apunta al sitio equivocado.
