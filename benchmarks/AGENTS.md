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
