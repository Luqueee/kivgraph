# Ladygraph

Ladygraph será un servidor MCP autónomo y local para inteligencia de código cross-repository en TypeScript y Go.

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
go build ./cmd/ladygraph
./ladygraph version
```

### Corpus sintético de LadybugDB

El generador crea un corpus JSON Lines reproducible para los benchmarks de
almacenamiento:

```bash
go run ./cmd/ladygraph benchmark generate-graph \
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
  --database /tmp/ladygraph-individual.db \
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
  --database-dir /tmp/ladygraph-batch \
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
  --database /tmp/ladygraph-copy.db \
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
  --database /tmp/ladygraph-copy.db \
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
  --database /tmp/ladygraph-copy.db \
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
  --database /tmp/ladygraph-copy.db
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
go run -tags ladybug ./cmd/ladygraph doctor storage \
  --database /tmp/ladygraph-copy.db
```

El comando informa ubicación, tamaño, permisos efectivos, locks externos,
versiones del motor, almacenamiento y binding Go, esquema, rollback, conteos e
integridad referencial. Devuelve `0` solo cuando todos los checks están en
`PASS`; una base bloqueada, incompleta o compilada sin el tag `ladybug` devuelve
`1`. La base indicada no se modifica.

La verificación de integridad semántica comprueba los seis invariantes del
grafo canónico sobre una base ya publicada, sin reconstruirla:

```bash
go run -tags ladybug ./cmd/ladygraph doctor graph \
  --database /var/lib/ladygraph/graph/CURRENT/graph.db
```

El comando imprime una línea por regla con su estado (`PASS`/`FAIL`) y su
número de violaciones y, bajo cada regla incumplida, hasta
`ladybug.MaxIntegritySamples` (20) muestras con la tabla, la clave y el
detalle de la fila que la rompe. Los seis invariantes, todos a cero en un
grafo sano:

- `exact_edge_without_source`: una arista semántica con `confidence` exacta
  cuyo nodo origen no está declarado, por ejemplo un `Symbol` sin `DEFINES`
  entrante desde ningún `File`.
- `exact_edge_without_target`: lo mismo para el nodo destino.
- `missing_evidence_file`: una arista con `evidence_key` cuya `Evidence` no
  existe, o que existe pero no tiene `OBSERVED_IN` hacia un `File`.
- `duplicate_stable_key`: una misma `stable_key` usada por dos tablas de
  nodo distintas.
- `unknown_confidence`: una `confidence` o `provenance` fuera del
  vocabulario de `facts.Confidence`/`facts.Provenance`, o una arista que
  declara exactitud respaldada por una procedencia no exacta.
- `invalid_repository_ownership`: un nodo cuyo `repository_key` no coincide
  con el repositorio alcanzable por contención (`Package` vía
  `CONTAINS_PACKAGE`, `File` vía `CONTAINS_FILE`, `Symbol` vía `DEFINES`,
  `Evidence` vía `OBSERVED_IN`), o que apunta a un `Repository` inexistente.

LadybugDB garantiza que toda relación tiene sus dos extremos, así que "sin
origen" nunca significa que el nodo no exista: significa que ningún hecho lo
declaró. Una arista exacta anclada a un símbolo que ningún archivo declara
es un fallo del invariante correspondiente, no una degradación aceptable.
El comando devuelve `0` solo si las seis reglas pasan; la base indicada no
se modifica.

La reconstrucción completa conecta facts, staging, `graph.next`, carga bulk,
integridad, snapshot, golden probes y publicación en una sola operación sobre
un `facts.Set` serializado:

```bash
go run -tags ladybug ./cmd/ladygraph rebuild \
  --facts facts.json \
  --root /var/lib/ladygraph/graph \
  --generation 000123 \
  --resolver-version go-tsserver-1.0.0
```

`--facts` apunta a un JSON con un `facts.Set` (`Repositories`, `Packages`,
`Files`, `Symbols`, `Evidence`, `Edges`, `Unresolved`) ya normalizado con
`Set.Sort()` y `Set.Validate()`. `--root` es la raíz del `generation.Store`
que recibirá la nueva generación; `--generation` son sus seis dígitos y
`--resolver-version` queda grabado en cada arista semántica junto con
`--snapshot-id` (por defecto `0`). El comando imprime en la salida estándar
una línea por etapa con su estado y duración, las discrepancias de conteo y
las violaciones de invariantes que encontró la etapa de integridad, las
sondas fallidas si las hay, el digest del snapshot y la generación
publicada.

La publicación es atómica: `rebuild` construye y valida el candidato en
`--root/generations/<generación>/graph.db` y solo actualiza `--root/CURRENT`
para apuntarlo si la carga, la integridad —conteos por tabla e invariantes
semánticos— y las golden probes pasan. Un
fallo en cualquier etapa deja `CURRENT` y la generación anterior intactas y
sirviéndose; el comando termina con estado distinto de cero y explica en la
salida de error qué etapa falló.

La consulta de estado resuelve los tres roles que debe mantener el
`generation.Store` para backup y rollback, sin reconstruir nada:

```bash
go run -tags ladybug ./cmd/ladygraph graph status \
  --root /var/lib/ladygraph/graph
```

El comando imprime `graph.active`, `graph.next` y `graph.backup` con la ruta
que cada uno nombra en disco, y la lista completa de generaciones retenidas.
Los tres son una lectura sobre el mismo layout descrito más arriba, no un
layout nuevo: `graph.active` es la generación que apunta `--root/CURRENT`,
`graph.next` es el candidato `--root/generations/<id>.tmp` que construiría
la próxima `rebuild`, y `graph.backup` es la generación que un `rollback`
restauraría, registrada en `--root/BACKUP` con la misma disciplina atómica
que `CURRENT` (`BACKUP.next`, `fsync`, `rename`). Un store sin generación
activa se reporta como `graph.active: none`, no como error; el comando solo
devuelve estado distinto de cero si no puede abrir el `generation.Store`.

`CURRENT` y `BACKUP` no pueden actualizarse en un único `rename`: cada
publicación o rollback escribe primero `BACKUP` —apuntando a la generación
que va a dejar de estar activa— y solo después `CURRENT`. Si el proceso
muere entre ambas escrituras, `BACKUP` puede quedar apuntando a la misma
generación que `CURRENT`; la regla de recuperación, autoconsistente y sin
reparación manual, es que `BACKUP == CURRENT` significa que no hay backup.
La retención tras cada `rebuild` conserva exactamente `graph.active` y
`graph.backup`: cualquier otra generación publicada se poda, y un fallo de
poda no invalida la publicación —el grafo activo ya es correcto y sigue
sirviéndose— pero queda anotado en la etapa `publish` del informe.

El rollback revierte `CURRENT` a una generación ya publicada, revalidándola
antes de conmutar:

```bash
go run -tags ladybug ./cmd/ladygraph rollback \
  --root /var/lib/ladygraph/graph \
  --generation 000123
```

`--generation` es opcional: sin él, `rollback` usa el `graph.backup`
registrado, y si no hay ninguno y tampoco se dio `--generation` explícito,
el comando falla explicando que no hay a dónde volver. Antes de conmutar
`CURRENT`, `rollback` recalcula el digest de la generación destino a partir
de sus conteos por tabla —la misma fórmula que la etapa `snapshot` de
`rebuild` ya escribió en su `snapshot.sha256`— y exige que los seis
invariantes del grafo canónico sigan pasando; una generación sin
`snapshot.sha256` nunca se reactiva a ciegas. Si cualquiera de las dos
comprobaciones falla, `CURRENT` queda intacto: el propio `generation.Store`
revierte el cambio cuando la validación no pasa, antes de que `rollback`
necesite un mecanismo de deshacer propio. El comando imprime la transición,
el digest esperado y el observado, y el veredicto de integridad, y termina
en `0` solo si la generación quedó activa. Un rollback exitoso invierte los
roles: la generación que antes era `graph.active` pasa a ser el nuevo
`graph.backup`, así que siempre se puede volver a avanzar con otro
rollback.

La construcción del HotSnapshot lee la generación ya publicada y produce,
en memoria, el índice denso que servirán las consultas MCP:

```bash
go run -tags ladybug ./cmd/ladygraph snapshot \
  --root /var/lib/ladygraph/graph \
  --generation 000123
```

`--generation` es opcional: sin él, `snapshot` construye desde el
`graph.active` registrado. El snapshot se deriva del grafo canónico ya
publicado en LadybugDB, nunca del `facts.Set` que lo originó: lee
`Repository`, `Package`, `File`, `Symbol`, `Evidence` y las relaciones
semánticas directamente de la base indicada —la misma fuente que verifica
`doctor graph`—, que no se modifica. Las aristas estructurales
(`CONTAINS_PACKAGE`, `CONTAINS_FILE`, `DEFINES`, `OBSERVED_IN`,
`REPORTS_UNRESOLVED`) y las de dependencia entre paquetes
(`PACKAGE_DEPENDS_ON`, `MODULE_DEPENDS_ON`) no entran en el CSR del
HotSnapshot, que solo indexa `Symbol` por su `stable_key` y solo conserva
adyacencias símbolo a símbolo; eso no pierde información, porque la
contención ya vive en los propios nodos (`File.package_key`,
`Package.repository_key`, `Symbol.file_key`) y la dependencia entre
paquetes sigue consultable directamente en LadybugDB, que continúa siendo
la fuente de verdad para ella. El comando imprime el identificador y la
versión del snapshot, su digest de contenido, las cuentas por tabla y el
número de aristas no representadas en el CSR, y termina en `0` solo si el
grafo pudo convertirse en snapshot; una tabla de arista fuera del
vocabulario canónico o un `confidence`/`provenance` desconocido hacen
fallar la construcción en vez de admitir un snapshot silenciosamente
incompleto.

La [calificación de LadybugDB](docs/decisions/ladybugdb-qualification.md)
concluye `ACCEPT_LADYBUGDB_WITH_LIMITS`. `LADYBUG_RECOVERY_PASS` está emitido:
las generaciones inmutables y la publicación durable de `CURRENT` protegen la
base activa ante `ENOSPC`. LUQUE-0214 obtuvo p95 `Apply` de 271,9 ms para
1.000 relaciones con staging transaccional exacto; se emitieron
`LADYBUG_DELTA_PERFORMANCE_PASS` y `LADYBUG_STORAGE_PASS`.



## Estructura

```text
cmd/ladygraph/   Ejecutable principal.
internal/    Paquetes internos de Ladygraph.
ts-worker/   Worker TypeScript.
testdata/    Fixtures y corpus de pruebas.
benchmarks/  Resultados de benchmarks.
docs/        Documentación y ADR.
scripts/     Automatización auxiliar.
```

## Licencia

Ladygraph se distribuye bajo [Apache License 2.0](LICENSE).

## Licencias de terceros

Los avisos y las licencias de las dependencias distribuidas con Ladygraph se
registran en [`THIRD_PARTY_NOTICES.md`](THIRD_PARTY_NOTICES.md). La lista se
actualiza al incorporar cada dependencia al producto distribuible.
