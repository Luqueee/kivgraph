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
- **`daemon-cost` mide las dos puertas del demonio y publica un artefacto por
  cada una**: `results.json` para el socket unix y `results-http.json` para
  Streamable HTTP, con `-transport` seleccionando la puerta y el transporte
  dentro del digest. Sin eso las dos corridas colisionan en una identidad y una
  cifra de socket puede citarse como si fuera alcanzable. El esquema es
  `daemon-cost-v2`; un fichero `v1` calculó su digest sin el transporte y no se
  puede comparar con uno nuevo.
- **La carga se cuenta, no se elige, y es la variable que decidió el resultado
  dos veces.** El event log de un `serve` registra cada llamada de tool, así que
  la carga de una sesión real se recuenta de un log de uso -- la orden está en el
  informe. Medido: mediana de **una** llamada por sesión y `48` de `51`
  servidores sin ninguna. Las `2.000` llamadas del caso sostenido son tres
  órdenes de magnitud por encima de eso, y ahí HTTP parecía costar `12,5 MB` por
  cliente cuando a carga real cuesta `1,0`–`1,3`, igual que el socket. Un
  benchmark que mide la carga equivocada no es impreciso: **contesta otra
  pregunta**, y en este caso subestimaba el ahorro.
- El caso sostenido se conserva en `results-http-sustained.json` porque es un
  techo útil, y **su cifra no se transporta entre corpus**: `4,9`–`5,9 MB` por
  cliente sobre `117.499` símbolos contra `12,1`–`12,8` sobre `108.737`. Esa
  dependencia del corpus es la firma de un coste en bytes retenidos -- el buffer
  de reanudación de `10 MiB` que el SDK da a cada sesión-- y no de un coste por
  sesión. Citar el techo como si fuera el coste es el error que este benchmark ya
  cometió.
- No es un brazo de `shared-snapshot` y no debe convertirse en uno: los brazos de
  aquél se definen por si el fichero de snapshot está, y su gate mide mapear
  contra derivar. Un tercer brazo dejaría su comparación sin significado.
- Una cifra por símbolo se lee de la pasada que la produjo, nunca cruzada entre
  corpus. Las corridas vigentes usan los `117.499` símbolos de `kena`, los mismos
  que `load-cost-resident`; las anteriores usaban `108.737` y sus cifras están en
  el historial, no en estas tablas.

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
