# Luque

Luque será un servidor MCP autónomo y local para inteligencia de código cross-repository en TypeScript y Go.

## Estado

El repositorio contiene la base inicial del proyecto. La funcionalidad de indexación, almacenamiento y consultas MCP se incorporará siguiendo el orden definido en [`TASKS.md`](TASKS.md).

## Requisitos

- Go 1.24 o posterior.

## Desarrollo

```bash
make build
make test
make version
```

El comando provisional de versión también puede ejecutarse directamente:

```bash
go build ./cmd/luque
./luque version
```

### Corpus sintético de LadybugDB

El generador crea un corpus JSON Lines reproducible para los benchmarks de
almacenamiento:

```bash
go run ./cmd/luque benchmark generate-graph \
  --symbols 100000 \
  --edges 1000000 \
  --seed 42
```

Por defecto genera 40 repositorios, 100.000 archivos, 100.000 símbolos y
1.000.000 de aristas en `testdata/generated/synthetic`. `--repositories`,
`--files` y `--output` permiten sustituir esos valores. El directorio contiene
`repositories.jsonl`, `files.jsonl`, `symbols.jsonl`, `edges.jsonl` y un
`manifest.json` con los recuentos y las estructuras controladas del grafo.

La carga individual de referencia requiere la biblioteca nativa de LadybugDB y
ejecuta una sentencia preparada por nodo o arista:

```bash
go run -tags ladybug ./benchmarks/ladybug-individual \
  --corpus testdata/generated/synthetic \
  --database /tmp/luque-individual.db \
  --transaction-size 1000
```

El tamaño de transacción solo controla los commits; no agrupa registros en una
misma sentencia. Los resultados se escriben en
`benchmarks/ladybug-individual`.

La variante por lotes usa una sentencia `UNWIND $rows` y una transacción por
lote. Compara los tamaños exigidos por el plan:

```bash
go run -tags ladybug ./benchmarks/ladybug-batch \
  --corpus testdata/generated/synthetic \
  --database-dir /tmp/luque-batch \
  --batch-sizes 100,1000,10000,50000
```

Cada escenario usa una base nueva y verifica los recuentos almacenados antes de
registrar throughput, tiempo de commit, pico de RSS y tamaño en disco.
La comparación registrada recomienda 10.000 registros por lote bajo el límite
RSS de 2 GiB. Los escenarios se ejecutan en procesos separados para que sus
mediciones de memoria no se contaminen entre sí.

La carga bulk mediante `COPY` exporta el corpus a CSV temporal y ejecuta una
operación `COPY` por tabla. En la escala inicial completa se verificaron
200.040 nodos y 1.000.000 de aristas:

```bash
go run -tags ladybug ./benchmarks/ladybug-bulk \
  --corpus testdata/generated/synthetic \
  --database /tmp/luque-copy.db \
  --output benchmarks/ladybug-bulk/full-scale
```

La comparación comparable con `CREATE` y transacciones por lotes se registra
en `benchmarks/ladybug-bulk/report.md`; la medición full-scale queda en
`benchmarks/ladybug-bulk/full-scale/`.

Las consultas directas reutilizan una conexión y sentencias preparadas para
lookup por stable key, referencias entrantes y salientes, recorridos acotados,
shortest path y agrupación por repositorio:

```bash
go run -tags ladybug ./benchmarks/ladybug-queries \
  --database /tmp/luque-copy.db \
  --corpus testdata/generated/synthetic \
  --output benchmarks/ladybug-queries
```

El benchmark ejecuta golden probes antes de medir. Sus resultados caracterizan
LadybugDB como fuente canónica; no califican los SLO del HotSnapshot.

La actualización incremental usa un único escritor lógico, valida el delta
completo antes de mutar y aplica símbolos y relaciones en una transacción. El
benchmark copia una base ya cargada, por lo que nunca modifica el artefacto de
entrada:

```bash
go run -tags ladybug ./benchmarks/ladybug-incremental \
  --database /tmp/luque-copy.db \
  --corpus testdata/generated/synthetic \
  --output benchmarks/ladybug-incremental
```

La secuencia mide altas individuales y por lote, altas y bajas de aristas,
cambios de propiedades, sustitución de relaciones salientes y borrado de
símbolos. Después comprueba rechazo de duplicados, ausencia de aristas
fantasma y rollback de un fallo tardío. Los tiempos solo cubren la mutación
transaccional de LadybugDB; la construcción y publicación de HotSnapshot
pertenecen a fases posteriores.

La recuperación se prueba con workers aislados, `SIGKILL`, corrupción,
permisos y un inyector Linux de `ENOSPC`. Cada escenario modifica únicamente
una copia privada:

```bash
go run -tags ladybug ./benchmarks/ladybug-recovery \
  --database /tmp/luque-copy.db
```

Los casos de caída, reapertura, truncado y permisos pasan. El resultado
registrado conserva un `FAIL` explícito para disco lleno: `Writer.Apply`
devolvió éxito y el primer `ENOSPC` interceptado apareció durante el cierre,
dejando la copia sin posibilidad de reapertura. El comando devuelve estado no
cero mientras exista esta limitación. La metodología y la evidencia completas
están en `docs/testing/ladybug-recovery.md`.

El diagnóstico operativo abre la base original en modo de solo lectura y
ejecuta la prueba de transacciones sobre una copia temporal:

```bash
go run -tags ladybug ./cmd/luque doctor storage \
  --database /tmp/luque-copy.db
```

El comando informa ubicación, tamaño, permisos efectivos, locks externos,
versiones del motor, almacenamiento y binding Go, esquema, rollback, conteos e
integridad referencial. Devuelve `0` solo cuando todos los checks están en
`PASS`; una base bloqueada, incompleta o compilada sin el tag `ladybug` devuelve
`1`. La base indicada no se modifica.

La reconstrucción completa conecta facts, staging, `graph.next`, carga bulk,
integridad, snapshot, golden probes y publicación en una sola operación sobre
un `facts.Set` serializado:

```bash
go run -tags ladybug ./cmd/luque rebuild \
  --facts facts.json \
  --root /var/lib/luque/graph \
  --generation 000123 \
  --resolver-version go-tsserver-1.0.0
```

`--facts` apunta a un JSON con un `facts.Set` (`Repositories`, `Packages`,
`Files`, `Symbols`, `Evidence`, `Edges`, `Unresolved`) ya normalizado con
`Set.Sort()` y `Set.Validate()`. `--root` es la raíz del `generation.Store`
que recibirá la nueva generación; `--generation` son sus seis dígitos y
`--resolver-version` queda grabado en cada arista semántica junto con
`--snapshot-id` (por defecto `0`). El comando imprime en la salida estándar
una línea por etapa con su estado y duración, las discrepancias de
integridad y las sondas fallidas si las hay, el digest del snapshot y la
generación publicada.

La publicación es atómica: `rebuild` construye y valida el candidato en
`--root/generations/<generación>/graph.db` y solo actualiza `--root/CURRENT`
para apuntarlo si la carga, la integridad y las golden probes pasan. Un
fallo en cualquier etapa deja `CURRENT` y la generación anterior intactas y
sirviéndose; el comando termina con estado distinto de cero y explica en la
salida de error qué etapa falló.

La [calificación de LadybugDB](docs/decisions/ladybugdb-qualification.md)
concluye `ACCEPT_LADYBUGDB_WITH_LIMITS`. `LADYBUG_RECOVERY_PASS` está emitido:
las generaciones inmutables y la publicación durable de `CURRENT` protegen la
base activa ante `ENOSPC`. LUQUE-0214 obtuvo p95 `Apply` de 271,9 ms para
1.000 relaciones con staging transaccional exacto; se emitieron
`LADYBUG_DELTA_PERFORMANCE_PASS` y `LADYBUG_STORAGE_PASS`.



## Estructura

```text
cmd/luque/   Ejecutable principal.
internal/    Paquetes internos de Luque.
ts-worker/   Worker TypeScript.
testdata/    Fixtures y corpus de pruebas.
benchmarks/  Resultados de benchmarks.
docs/        Documentación y ADR.
scripts/     Automatización auxiliar.
```

## Licencia

Luque se distribuye bajo [Apache License 2.0](LICENSE).

## Licencias de terceros

Los avisos y las licencias de las dependencias distribuidas con Luque se
registran en [`THIRD_PARTY_NOTICES.md`](THIRD_PARTY_NOTICES.md). La lista se
actualiza al incorporar cada dependencia al producto distribuible.
