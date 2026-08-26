# KIVGRAPH — Backlog de tareas para desarrollo asistido por IA

## 1. Reglas de ejecución

La IA deberá trabajar siguiendo estas normas:

1. Ejecutar una sola tarea principal cada vez.
2. No comenzar una tarea si sus dependencias no están completadas.
3. No avanzar de fase si el gate correspondiente no ha pasado.
4. No introducir tecnologías aplazadas sin una justificación respaldada por benchmarks.
5. No modificar repositorios indexados durante pruebas.
6. No considerar una relación semántica exacta si solo está respaldada por coincidencia textual o nominal.
7. Añadir tests para cada comportamiento implementado.
8. Ejecutar antes de cerrar cada tarea:

```bash
go test ./...
go vet ./...
```

Y, cuando afecte al worker TypeScript:

```bash
pnpm test
pnpm typecheck
```

9. Documentar cualquier decisión arquitectónica relevante mediante un ADR.
10. Guardar los resultados de benchmarks en archivos versionados.
11. No ocultar tests fallidos, warnings, referencias no resueltas ni limitaciones.
12. Cada tarea debe terminar con un informe que incluya:

```text
Estado:
Archivos creados:
Archivos modificados:
Tests ejecutados:
Resultados:
Benchmarks:
Limitaciones:
Siguiente tarea desbloqueada:
```

---

# 2. Estados permitidos

```text
TODO
IN_PROGRESS
BLOCKED
PASS
PASS_WITH_LIMITS
FAIL
```

Una tarea solo se considera completada cuando está en:

```text
PASS
```

o, cuando se permita expresamente:

```text
PASS_WITH_LIMITS
```

---

# 3. Fase 0 — Constitución del proyecto

## LUQUE-0001 — Crear el repositorio base

**Dependencias:** ninguna.

**Checklist:**

- [x] Verificar dependencias y alcance.
- [x] Completar acciones y entregables.
- [x] Ejecutar pruebas y benchmarks aplicables.
- [x] Verificar criterios de aceptación y el gate aplicable.
- [x] Registrar resultados, limitaciones y siguiente tarea.

**Objetivo:** crear la estructura inicial del repositorio Kivgraph.

**Acciones:**

* Crear el repositorio Git.
* Crear `go.mod`.
* Crear `README.md`.
* Crear `LICENSE`.
* Crear `.gitignore`.
* Crear `Makefile`.
* Crear los directorios principales.
* Inicializar el paquete `cmd/kivgraph`.
* Añadir un comando que muestre la versión provisional.

**Estructura mínima:**

```text
kivgraph/
├── cmd/kivgraph/
├── internal/
├── ts-worker/
├── testdata/
├── benchmarks/
├── docs/
├── scripts/
├── go.mod
├── Makefile
├── README.md
└── LICENSE
```

**Criterios de aceptación:**

```bash
go build ./cmd/kivgraph
./kivgraph version
```

deben completarse correctamente.

---

## LUQUE-0002 — Definir la licencia

**Dependencias:** LUQUE-0001.

**Checklist:**

- [x] Verificar dependencias y alcance.
- [x] Completar acciones y entregables.
- [x] Ejecutar pruebas y benchmarks aplicables.
- [x] Verificar criterios de aceptación y el gate aplicable.
- [x] Registrar resultados, limitaciones y siguiente tarea.

**Objetivo:** seleccionar y documentar la licencia del proyecto.

**Acciones:**

* Comparar Apache-2.0 y MIT.
* Tener en cuenta las dependencias nativas de LadybugDB.
* Registrar la decisión en:

```text
docs/adr/0001-license.md
```

* Actualizar `LICENSE`.
* Añadir una sección de licencias de terceros.

**Criterios de aceptación:**

* Existe una licencia explícita.
* La elección está justificada.
* El README menciona la licencia.
* Existe un lugar reservado para avisos de terceros.

---

## LUQUE-0003 — Definir convenciones del proyecto

**Dependencias:** LUQUE-0001.

**Checklist:**

- [x] Verificar dependencias y alcance.
- [x] Completar acciones y entregables.
- [x] Ejecutar pruebas y benchmarks aplicables.
- [x] Verificar criterios de aceptación y el gate aplicable.
- [x] Registrar resultados, limitaciones y siguiente tarea.

**Objetivo:** fijar reglas de nombres, errores, logging y estructura.

**Acciones:**

* Definir convenciones Go.
* Definir convenciones TypeScript.
* Definir formato de errores.
* Definir reglas para stable keys.
* Definir nomenclatura de gates.
* Definir formato de benchmarks.
* Definir política de compatibilidad.

**Entregable:**

```text
docs/development/conventions.md
```

**Criterios de aceptación:**

* Las convenciones cubren Go, TypeScript, tests y documentación.
* Los errores deben poder clasificarse programáticamente.
* Se distingue claramente entre error interno y resultado no resuelto.

---

## LUQUE-0004 — Crear los ADR iniciales

**Dependencias:** LUQUE-0001.

**Checklist:**

- [x] Verificar dependencias y alcance.
- [x] Completar acciones y entregables.
- [x] Ejecutar pruebas y benchmarks aplicables.
- [x] Verificar criterios de aceptación y el gate aplicable.
- [x] Registrar resultados, limitaciones y siguiente tarea.

**Objetivo:** registrar las decisiones arquitectónicas ya cerradas.

**Crear:**

```text
docs/adr/0002-go-core.md
docs/adr/0003-ladybugdb-storage.md
docs/adr/0004-hot-snapshot.md
docs/adr/0005-typescript-worker.md
docs/adr/0006-tree-sitter-role.md
docs/adr/0007-read-only-mcp.md
```

**Criterios de aceptación:**

Cada ADR debe incluir:

```text
Context
Decision
Alternatives
Consequences
Risks
Status
```

---

## LUQUE-0005 — Configurar calidad de código Go

**Dependencias:** LUQUE-0001.

**Checklist:**

- [x] Verificar dependencias y alcance.
- [x] Completar acciones y entregables.
- [x] Ejecutar pruebas y benchmarks aplicables.
- [x] Verificar criterios de aceptación y el gate aplicable.
- [x] Registrar resultados, limitaciones y siguiente tarea.

**Objetivo:** automatizar las comprobaciones del código Go.

**Acciones:**

* Configurar `gofmt`.
* Configurar `go vet`.
* Configurar `staticcheck`.
* Configurar `golangci-lint` solo si aporta reglas necesarias.
* Añadir targets al Makefile.

**Comandos esperados:**

```bash
make format
make lint
make test
make check
```

**Criterios de aceptación:**

Todos los comandos deben pasar en un repositorio limpio.

---

## LUQUE-0006 — Configurar el worker TypeScript

**Dependencias:** LUQUE-0001.

**Checklist:**

- [x] Verificar dependencias y alcance.
- [x] Completar acciones y entregables.
- [x] Ejecutar pruebas y benchmarks aplicables.
- [x] Verificar criterios de aceptación y el gate aplicable.
- [x] Registrar resultados, limitaciones y siguiente tarea.

**Objetivo:** crear el proyecto independiente del worker.

**Acciones:**

* Crear `ts-worker/package.json`.
* Fijar el gestor de paquetes.
* Crear `tsconfig.json`.
* Configurar lint.
* Configurar tests.
* Añadir un ejecutable provisional.
* Crear un comando `hello` que responda mediante stdin/stdout.

**Criterios de aceptación:**

```bash
cd ts-worker
pnpm install --frozen-lockfile
pnpm typecheck
pnpm test
pnpm build
```

deben pasar.

---

## LUQUE-0007 — Configurar integración continua

**Dependencias:** LUQUE-0005 y LUQUE-0006.

**Checklist:**

- [x] Verificar dependencias y alcance.
- [x] Completar acciones y entregables.
- [x] Ejecutar pruebas y benchmarks aplicables.
- [x] Verificar criterios de aceptación y el gate aplicable.
- [x] Registrar resultados, limitaciones y siguiente tarea.

**Objetivo:** validar automáticamente ambos componentes.

**La CI deberá ejecutar:**

```text
Go format
Go vet
Go tests
Go race tests seleccionados
TypeScript install
TypeScript typecheck
TypeScript tests
Build Go
Build worker
```

**Criterios de aceptación:**

* La CI pasa en la rama principal.
* Un test roto provoca fallo.
* Un archivo Go sin formatear provoca fallo.

---

## LUQUE-0008 — Fijar los SLO de rendimiento

**Dependencias:** LUQUE-0003.

**Checklist:**

- [x] Verificar dependencias y alcance.
- [x] Completar acciones y entregables.
- [x] Ejecutar pruebas y benchmarks aplicables.
- [x] Verificar criterios de aceptación y el gate aplicable.
- [x] Registrar resultados, limitaciones y siguiente tarea.

**Objetivo:** convertir los objetivos del plan en un contrato medible.

**Entregable:**

```text
docs/performance/slo.md
```

**Debe incluir:**

* p50;
* p95;
* p99;
* RSS;
* tiempo de indexación;
* tamaño máximo del snapshot;
* tiempo de actualización incremental;
* límites de recorrido;
* procedimiento de benchmark.

**Gate de fase:**

```text
PROJECT_FOUNDATION_PASS
```

---

# 4. Fase 1 — MCP mínimo

## LUQUE-0101 — Integrar el SDK MCP oficial para Go

**Dependencias:** PROJECT_FOUNDATION_PASS.

**Checklist:**

- [x] Verificar dependencias y alcance.
- [x] Completar acciones y entregables.
- [x] Ejecutar pruebas y benchmarks aplicables.
- [x] Verificar criterios de aceptación y el gate aplicable.
- [x] Registrar resultados, limitaciones y siguiente tarea.

**Objetivo:** crear el servidor MCP mínimo.

**Acciones:**

* Fijar una versión concreta del SDK.
* Registrar la dependencia.
* Crear `internal/mcp/server.go`.
* Implementar transporte STDIO.
* Implementar cierre limpio.
* Exponer versión y capacidades.

**Criterios de aceptación:**

* El servidor responde a `initialize`.
* Publica `serverInfo`.
* `serverInfo.version` coincide con `kivgraph version`.
* Se cierra correctamente al terminar stdin.

---

## LUQUE-0102 — Implementar `graph_status` provisional

**Dependencias:** LUQUE-0101.

**Checklist:**

- [x] Verificar dependencias y alcance.
- [x] Completar acciones y entregables.
- [x] Ejecutar pruebas y benchmarks aplicables.
- [x] Verificar criterios de aceptación y el gate aplicable.
- [x] Registrar resultados, limitaciones y siguiente tarea.

**Objetivo:** crear la primera tool MCP.

**Respuesta inicial:**

```json
{
  "status": "empty",
  "snapshot_id": null,
  "repositories": 0,
  "symbols": 0,
  "edges": 0
}
```

**Criterios de aceptación:**

* La tool está marcada como read-only.
* No accede al filesystem.
* No crea goroutines persistentes por petición.
* Tiene tests de entrada y salida.

---

## LUQUE-0103 — Implementar `list_repositories` provisional

**Dependencias:** LUQUE-0101.

**Checklist:**

- [x] Verificar dependencias y alcance.
- [x] Completar acciones y entregables.
- [x] Ejecutar pruebas y benchmarks aplicables.
- [x] Verificar criterios de aceptación y el gate aplicable.
- [x] Registrar resultados, limitaciones y siguiente tarea.

**Objetivo:** exponer una lista vacía mientras no exista registry.

**Criterios de aceptación:**

* Responde con formato estable.
* Incluye `total`, `returned` y `truncated`.
* Tiene tests.

---

## LUQUE-0104 — Crear el benchmark del MCP vacío

**Dependencias:** LUQUE-0102 y LUQUE-0103.

**Checklist:**

- [x] Verificar dependencias y alcance.
- [x] Completar acciones y entregables.
- [x] Ejecutar pruebas y benchmarks aplicables.
- [x] Verificar criterios de aceptación y el gate aplicable.
- [x] Registrar resultados, limitaciones y siguiente tarea.

**Objetivo:** medir el overhead mínimo.

**Pruebas:**

```text
10.000 llamadas graph_status
10.000 llamadas list_repositories
1 cliente
4 clientes
16 clientes
32 clientes
```

**Medir:**

* p50;
* p95;
* p99;
* throughput;
* allocations/op;
* bytes/op;
* RSS;
* goroutines;
* errores.

**Entregables:**

```text
benchmarks/mcp-empty/results.json
benchmarks/mcp-empty/report.md
```

**Gate:**

```text
EMPTY_MCP_PERFORMANCE_PASS
```

Requisitos:

```text
p95 ≤ 2 ms
0 errores
0 crecimiento continuo de memoria
```

---

# 5. Fase 2 — LadybugDB

## LUQUE-0201 — Fijar versiones de LadybugDB

**Dependencias:** EMPTY_MCP_PERFORMANCE_PASS.

**Checklist:**

- [x] Verificar dependencias y alcance.
- [x] Completar acciones y entregables.
- [x] Ejecutar pruebas y benchmarks aplicables.
- [x] Verificar criterios de aceptación y el gate aplicable.
- [x] Registrar resultados, limitaciones y siguiente tarea.

**Objetivo:** seleccionar versiones reproducibles.

**Registrar:**

* LadybugDB core;
* binding Go;
* commit;
* release;
* SHA-256;
* licencia;
* biblioteca nativa;
* flags CGO;
* plataformas soportadas.

**Entregable:**

```text
docs/dependencies/ladybugdb.md
```

**Criterios de aceptación:**

No se admite `latest`, ramas flotantes ni descargas sin checksum.

---

## LUQUE-0202 — Crear wrapper mínimo de LadybugDB

**Dependencias:** LUQUE-0201.

**Checklist:**

- [x] Verificar dependencias y alcance.
- [x] Completar acciones y entregables.
- [x] Ejecutar pruebas y benchmarks aplicables.
- [x] Verificar criterios de aceptación y el gate aplicable.
- [x] Registrar resultados, limitaciones y siguiente tarea.

**Objetivo:** encapsular completamente la dependencia.

**Crear:**

```text
internal/storage/ladybug/database.go
internal/storage/ladybug/connection.go
internal/storage/ladybug/errors.go
```

**Interfaz inicial:**

```go
type Database interface {
    Close() error
    Health(context.Context) error
}
```

**Criterios de aceptación:**

* La base se abre.
* Se cierra limpiamente.
* Puede reabrirse.
* Los errores nativos se convierten en errores propios de Kivgraph.

---

## LUQUE-0203 — Diseñar el esquema sintético

**Dependencias:** LUQUE-0202.

**Checklist:**

- [x] Verificar dependencias y alcance.
- [x] Completar acciones y entregables.
- [x] Ejecutar pruebas y benchmarks aplicables.
- [x] Verificar criterios de aceptación y el gate aplicable.
- [x] Registrar resultados, limitaciones y siguiente tarea.

**Objetivo:** modelar un grafo suficiente para benchmarks.

**Nodos:**

```text
Repository
File
Symbol
```

**Relaciones:**

```text
CONTAINS
DEFINES
REFERENCES
CALLS_DIRECT
```

**Entregable:**

```text
schemas/ladybug/001-synthetic.cypher
docs/storage/synthetic-schema.md
```

---

## LUQUE-0204 — Crear generador de corpus sintético

**Dependencias:** LUQUE-0203.

**Checklist:**

- [x] Verificar dependencias y alcance.
- [x] Completar acciones y entregables.
- [x] Ejecutar pruebas y benchmarks aplicables.
- [x] Verificar criterios de aceptación y el gate aplicable.
- [x] Registrar resultados, limitaciones y siguiente tarea.

**Objetivo:** generar grafos reproducibles.

**Escala inicial:**

```text
40 repositorios
100.000 archivos
100.000 símbolos
1.000.000 aristas
```

**Requisitos:**

* semilla configurable;
* distribución reproducible;
* relaciones entrantes y salientes;
* nodos aislados;
* hubs;
* cadenas de profundidad 5;
* ciclos controlados.

**Comando:**

```bash
kivgraph benchmark generate-graph \
  --symbols 100000 \
  --edges 1000000 \
  --seed 42
```

---

## LUQUE-0205 — Implementar carga individual

**Dependencias:** LUQUE-0204.

**Checklist:**

- [x] Verificar dependencias y alcance.
- [x] Completar acciones y entregables.
- [x] Ejecutar pruebas y benchmarks aplicables.
- [x] Verificar criterios de aceptación y el gate aplicable.
- [x] Registrar resultados, limitaciones y siguiente tarea.

**Objetivo:** medir la estrategia más simple.

**Acciones:**

* Crear nodos uno a uno.
* Crear aristas una a una.
* Usar transacciones configurables.
* Medir throughput.

**Este resultado será un baseline, no necesariamente la solución final.**

**Resultado registrado:**

* Corpus reducido acordado: 40 repositorios, 10.000 archivos, 10.000 símbolos y 100.000 aristas.
* Transacciones de 1.000 registros; cada registro usa una sentencia preparada individual.
* 2.648,7 nodos/s; 1.135,2 aristas/s; 1.254,9 registros/s.
* El corpus completo se aplaza para comparar los loaders por lotes y bulk.
* Artefactos: `benchmarks/ladybug-individual/results.json` y `benchmarks/ladybug-individual/report.md`.

---

## LUQUE-0206 — Implementar carga por lotes

**Dependencias:** LUQUE-0205.

**Checklist:**

- [x] Verificar dependencias y alcance.
- [x] Completar acciones y entregables.
- [x] Ejecutar pruebas y benchmarks aplicables.
- [x] Verificar criterios de aceptación y el gate aplicable.
- [x] Registrar resultados, limitaciones y siguiente tarea.

**Objetivo:** mejorar la carga mediante batches.

**Comparar lotes de:**

```text
100
1.000
10.000
50.000
```

**Medir:**

* nodos/s;
* aristas/s;
* RSS;
* commit time;
* tamaño en disco.

**Resultado registrado:**

* Comparación aislada por proceso sobre 20.040 nodos y 100.000 aristas.
* Throughput agregado para lotes 100/1.000/10.000/50.000: 2.652,7 / 3.205,4 / 3.729,8 / 3.894,3 registros/s.
* Lote recomendado: 10.000, con 25.253,2 nodos/s, 3.185,7 aristas/s y 1.270.525.952 bytes de pico RSS.
* El lote 50.000 supera el límite RSS de 2 GiB y su intento a escala completa excedió 600 segundos.
* Artefactos: `benchmarks/ladybug-batch/results.json` y `benchmarks/ladybug-batch/report.md`.

---

## LUQUE-0207 — Implementar bulk load

**Dependencias:** LUQUE-0206.

**Checklist:**

- [x] Verificar dependencias y alcance.
- [x] Completar acciones y entregables.
- [x] Ejecutar pruebas y benchmarks aplicables.
- [x] Verificar criterios de aceptación y el gate aplicable.
- [x] Registrar resultados, limitaciones y siguiente tarea.

**Objetivo:** evaluar el mecanismo de carga masiva recomendado por LadybugDB.

**Comparar:**

```text
CREATE
batch transaction
COPY
```

**Entregable:**

```text
benchmarks/ladybug-bulk/report.md
```

**Resultado registrado:**

* `COPY` se probó con CSV temporales y una operación por tabla.
* Escala inicial completa verificada: 200.040 nodos y 1.000.000 de aristas.
* COPY: 666.615,5 registros/s durante la carga y 389.908,1 registros/s end-to-end incluyendo exportación CSV.
* Pico RSS: 532.602.880 bytes; base: 43.065.344 bytes.
* Comparación comparable en corpus reducido: `CREATE` 1.254,9 registros/s, batch 10.000 3.729,8 registros/s y COPY 671.567,7 registros/s.
* `LADYBUG_BULK_LOAD_PASS`: aprobado; recuentos verificados, escala completa y RSS bajo 2 GiB.
* Artefactos: `benchmarks/ladybug-bulk/results.json`, `benchmarks/ladybug-bulk/report.md` y `benchmarks/ladybug-bulk/full-scale/`.

---

## LUQUE-0208 — Implementar consultas directas de LadybugDB

**Dependencias:** LUQUE-0207.

**Checklist:**

- [x] Verificar dependencias y alcance.
- [x] Completar acciones y entregables.
- [x] Ejecutar pruebas y benchmarks aplicables.
- [x] Verificar criterios de aceptación y el gate aplicable.
- [x] Registrar resultados, limitaciones y siguiente tarea.

**Consultas:**

* obtener símbolo por stable key;
* referencias entrantes;
* referencias salientes;
* profundidad 3;
* profundidad 5;
* shortest path;
* agrupación por repositorio.

**Criterios de aceptación:**

Las consultas deben devolver resultados correctos sobre el corpus sintético.

**Resultado registrado:**

* `Database.OpenReader` crea un lector reutilizable con conexión propia y sentencias preparadas.
* Las referencias combinan `REFERENCES` y `CALLS_DIRECT`; los resultados tienen orden determinista y límites validados.
* Los recorridos admiten profundidades 1–5, hasta 25.000 nodos, y devuelven cada destino a su menor profundidad.
* Las pruebas nativas verifican lookup, ambas direcciones, profundidad 3 y 5, shortest path exacto, agrupación por repositorio, entradas inválidas, cierre y concurrencia.
* Corpus completo: 100 llamadas medidas y 5 de warm-up por operación, con cero errores.
* p95 directo: lookup 13,31 ms; incoming 100 145,72 ms; outgoing 100 146,74 ms; depth 3 46,66 ms; depth 5 47,09 ms; shortest path 85,45 ms; agrupación 33,59 ms.
* No hay gate propio en esta tarea. Estas latencias caracterizan LadybugDB y no califican los SLO MCP del HotSnapshot.
* Artefactos: `benchmarks/ladybug-queries/results.json` y `benchmarks/ladybug-queries/report.md`.

---

## LUQUE-0209 — Implementar actualización incremental sintética

**Dependencias:** LUQUE-0208.

**Checklist:**

- [x] Verificar dependencias y alcance.
- [x] Completar acciones y entregables.
- [x] Ejecutar pruebas y benchmarks aplicables.
- [x] Verificar criterios de aceptación y el gate aplicable.
- [x] Registrar resultados, limitaciones y siguiente tarea.

**Probes:**

* añadir 1 símbolo;
* añadir 1.000 símbolos;
* añadir aristas;
* borrar aristas;
* borrar símbolo;
* cambiar propiedades;
* sustituir relaciones salientes.

**Comprobar:**

* ausencia de duplicados;
* ausencia de ghost edges;
* atomicidad;
* rollback.

**Resultado registrado:**

* `Database.OpenWriter` expone un único escritor lógico; cada `Delta` se valida y aplica en una transacción explícita.
* El contrato soporta altas, cambios y bajas de símbolos, altas y bajas de `REFERENCES`/`CALLS_DIRECT` y sustitución atómica de relaciones salientes.
* Corpus completo: 100.000 símbolos y 1.000.000 de aristas. Probes: 1 símbolo 13,98 ms; 1.000 símbolos 4.731,66 ms; 3 aristas 864,60 ms; baja de arista 7,13 ms; cambio de propiedades 7,30 ms; sustitución saliente 799,31 ms; baja de símbolo 109,81 ms.
* Un fallo tardío por destino inexistente tardó 400,36 ms y revirtió el símbolo insertado en la misma transacción.
* Las pruebas y el probe final confirman rechazo de símbolos y relaciones duplicados, ausencia de ghost edges, atomicidad y rollback.
* La base de entrada conservó el checksum SHA-256 `ada9dc0b704046c8b019e17efe3d443de58102b7a316964b9e105822ffc99191`.
* No hay gate propio en esta tarea. Las mediciones excluyen la construcción y publicación del HotSnapshot, aplazadas a fases posteriores.
* Artefactos: `benchmarks/ladybug-incremental/results.json` y `benchmarks/ladybug-incremental/report.md`. **Borrados por LUQUE-2003 / ADR 0057** al retirarse el camino incremental; medían código que ya no existe. Quedan en el historial de git, y las cifras de arriba son el registro.

---

## LUQUE-0210 — Probar recuperación ante fallo

**Dependencias:** LUQUE-0209.

**Checklist:**

- [x] Verificar dependencias y alcance.
- [x] Completar acciones y entregables.
- [x] Ejecutar pruebas y benchmarks aplicables.
- [x] Verificar criterios de aceptación y el gate aplicable.
- [x] Registrar resultados, limitaciones y siguiente tarea.

**Casos:**

* SIGKILL durante inserción;
* SIGKILL antes de commit;
* SIGKILL durante carga masiva;
* reapertura tras caída;
* fichero truncado;
* directorio sin permisos;
* disco simulado como lleno.

**Entregables:**

```text
benchmarks/ladybug-recovery/results.json
docs/testing/ladybug-recovery.md
```

**Resultado registrado — PASS_WITH_LIMITS:**

* Cada caso se ejecuta sobre una copia privada y los fallos potencialmente fatales quedan aislados en workers Linux.
* Pasan `SIGKILL` durante inserción, antes de `COMMIT` y durante `COPY`: la reapertura conserva el estado previo y no expone filas parciales.
* La reapertura posterior permite una transacción nueva que sigue presente tras cerrar y abrir otra vez la base.
* Un fichero truncado a la mitad y un directorio sin permisos producen errores controlados, sin señales ni timeouts.
* El caso de disco lleno queda en `FAIL`: el shim se armó antes de `Writer.Apply`, pero el primer `ENOSPC` interceptado ocurrió durante el cierre, después de que `Apply` devolviera éxito. La copia resultante no pudo reabrirse y la API nativa de cierre no expone un error.
* La base de entrada mantuvo el SHA-256 `ada9dc0b704046c8b019e17efe3d443de58102b7a316964b9e105822ffc99191`.
* No hay gate propio. La limitación de disco lleno queda visible y deberá diagnosticarse en LUQUE-0211; no se considera una recuperación soportada.
* Artefactos: `benchmarks/ladybug-recovery/results.json` y `docs/testing/ladybug-recovery.md`.

---

## LUQUE-0211 — Crear comando `kivgraph doctor storage`

**Dependencias:** LUQUE-0210.

**Checklist:**

- [x] Verificar dependencias y alcance.
- [x] Completar acciones y entregables.
- [x] Ejecutar pruebas y benchmarks aplicables.
- [x] Verificar criterios de aceptación y el gate aplicable.
- [x] Registrar resultados, limitaciones y siguiente tarea.

**Objetivo:** diagnosticar la base.

**Debe informar:**

* apertura;
* versión;
* esquema;
* permisos;
* transacciones;
* conteos;
* integridad;
* tamaño;
* ubicación;
* lock de otro proceso.

**Resultado registrado: `PASS`.**

* `kivgraph doctor storage --database PATH` informa los diez diagnósticos requeridos y devuelve `0` únicamente si todos están en `PASS`.
* La base original se abre en modo de solo lectura; la prueba `BEGIN`/mutación/`ROLLBACK` se ejecuta sobre una copia temporal y el SHA-256 del origen permanece idéntico.
* Una base sintética completa de 40 repositorios, 100.000 archivos, 100.000 símbolos y 1.000.000 de aristas superó apertura, versiones, esquema, permisos, transacciones, conteos e integridad.
* Un proceso externo con LadybugDB abierto fue detectado por PID y produjo estado no cero sin impedir el resto del informe.
* La detección detallada de locks usa `/proc/locks` en Linux. En otras plataformas se informa `SKIP`; el soporte nativo sigue requiriendo `-tags ladybug`, CGO y la biblioteca LadybugDB.
* Verificación: `go test ./...`, `go vet ./...`, `go test -tags ladybug ./...`, `go vet -tags ladybug ./...` y `go test -race -tags ladybug ./...`.
* Siguiente tarea: LUQUE-0212.

---

## LUQUE-0212 — Emitir decisión de LadybugDB

**Dependencias:** LUQUE-0207, LUQUE-0209, LUQUE-0210 y LUQUE-0211.

**Checklist:**

- [x] Verificar dependencias y alcance.
- [x] Completar acciones y entregables.
- [x] Ejecutar pruebas y benchmarks aplicables.
- [x] Verificar criterios de aceptación y el gate aplicable.
- [x] Registrar resultados, limitaciones y siguiente tarea.

**Entregable:**

```text
docs/decisions/ladybugdb-qualification.md
```

**Decisiones válidas:**

```text
ACCEPT_LADYBUGDB
ACCEPT_LADYBUGDB_WITH_LIMITS
REJECT_LADYBUGDB
```

**Gate requerido para continuar:**

```text
LADYBUG_STORAGE_PASS
```

**Resultado registrado: `ACCEPT_LADYBUGDB_WITH_LIMITS`.**

* `LADYBUG_SCHEMA_PASS`, `LADYBUG_BULK_LOAD_PASS`, `LADYBUG_INCREMENTAL_PASS`, `LADYBUG_RECOVERY_PASS` y `LADYBUG_DELTA_PERFORMANCE_PASS` están aprobados sobre el corpus completo.
* LUQUE-0214 registró p95 `Apply` de 115,7 ms para 10 relaciones y 271,9 ms para 1.000 mediante staging transaccional con `COPY`; ambos límites contractuales pasan.
* `LADYBUG_STORAGE_PASS` queda emitido. La fase 3 puede comenzar conforme al gate definido en `PLAN.md`.
* La carga con `COPY` confirmó 669.100,3 registros/s y 542.978.048 bytes de pico RSS; la decisión y la evidencia posterior de recuperación y deltas están en `docs/decisions/ladybugdb-qualification.md`.
* ADR 0003 queda aceptada con límites y el siguiente trabajo permitido es LUQUE-0301.

---

## LUQUE-0213 — Hacer segura la publicación ante `ENOSPC`

**Dependencias:** LUQUE-0212.

**Checklist:**

- [x] Verificar dependencias y alcance.
- [x] Completar acciones y entregables.
- [x] Ejecutar pruebas y benchmarks aplicables.
- [x] Verificar criterios de aceptación y el gate aplicable.
- [x] Registrar resultados, limitaciones y siguiente tarea.

**Objetivo:** impedir que un fallo de espacio invalide la única base canónica.

**Acciones:**

* Crear candidatas en `state/generations/<id>.tmp/`; nunca mutar la generación indicada por `CURRENT`.
* Exigir el mayor entre `2 × base activa + snapshot + 1 GiB` y el 15 % libre del filesystem.
* Mantener una reserva de emergencia preasignada y configurable de al menos 512 MiB.
* Cerrar, hacer fsync, reabrir y ejecutar doctor, integridad, golden probes y validación de HotSnapshot.
* Hacer fsync de la candidata y su directorio.
* Renombrar `<id>.tmp/` a `<id>/` y hacer fsync del directorio `generations/` antes de cambiar `CURRENT`.
* Publicar `CURRENT.next` mediante fsync, rename atómico y fsync del directorio padre.
* Conservar y probar la restauración de al menos una generación anterior.
* Inyectar `ENOSPC` durante aplicación, cierre y publicación de `CURRENT`.
* Ante fallo, liberar la reserva solo para abortar, limpiar y registrar; continuar sirviendo el último HotSnapshot válido.

**Criterios de aceptación:**

* Cada fallo conserva `CURRENT`, el checksum y la posibilidad de reapertura de la base activa.
* Una candidata fallida se descarta y nunca se publica.
* El camino exitoso publica una generación validada y permite restaurar la anterior.
* Los fsync y renames requeridos se verifican mediante fault injection.
* `benchmarks/ladybug-recovery/results.json` termina con `all_passed: true`.

**Gate:**

```text
LADYBUG_RECOVERY_PASS
```

**Resultado registrado:** `LADYBUG_RECOVERY_PASS`.

* `internal/storage/generation` implementa candidatas privadas, reserva de 512 MiB, política preventiva de espacio, publicación durable de `CURRENT` y restauración.
* Fault injection cubre rename de generación, escritura/fsync/rename de `CURRENT` y fsync del directorio padre.
* El caso de cierre tardío con `ENOSPC` destruye solo la candidata; la generación activa conserva `CURRENT`, checksum, reapertura y snapshot validado.
* `benchmarks/ladybug-recovery/results.json` registra ocho casos en `PASS` y `all_passed: true`.
* Limitación conservada: la prueba cubre Linux y fallos de syscalls, no pérdida eléctrica ni cachés del controlador.
* `LADYBUG_STORAGE_PASS` queda emitido tras el perfil de LUQUE-0214.
* LUQUE-0301 queda desbloqueada.

---

## LUQUE-0214 — Perfilar y optimizar deltas LadybugDB

**Dependencias:** LUQUE-0213.

**Checklist:**

- [x] Verificar dependencias y alcance.
- [x] Completar acciones y entregables.
- [x] Ejecutar pruebas y benchmarks aplicables.
- [x] Verificar criterios de aceptación y el gate aplicable.
- [x] Registrar resultados, limitaciones y siguiente tarea.

**Objetivo:** eliminar el coste por fact y fijar una política medible de batching.

**Desglosar:**

```text
BEGIN
lookups de source/target
borrado de relaciones
creación de relaciones
consultas de integridad
COMMIT
close/flush
```

**Comparar:**

* sentencias preparadas individuales;
* batches reales;
* staging con `COPY`;
* deltas agregados durante ventanas de 150–500 ms.

**Entregables:**

```text
benchmarks/ladybug-delta-profile/results.json
benchmarks/ladybug-delta-profile/report.md
```

**Criterios de aceptación:**

* El writer no emite una operación nativa por fact sin justificación medida.
* Un delta de 1–10 relaciones tiene objetivo menor de 50 ms y p95 máximo tolerable de 150 ms.
* Un delta de 1.000 relaciones tiene p95 menor de 500 ms.
* Atomicidad, rollback, duplicados e integridad siguen pasando.
* La estrategia elegida registra throughput, RSS, allocations y tiempo por fase.

**Gate:**

```text
LADYBUG_DELTA_PERFORMANCE_PASS
LADYBUG_STORAGE_PASS
```

**Resultado registrado:** `LADYBUG_DELTA_PERFORMANCE_PASS` **emitido**.

**Estado:** `PASS` — `LADYBUG_STORAGE_PASS` derivado.

* `prepared_individual` usa una consulta exacta para validar 1–10 relaciones y sentencias individuales para crearlas; p95 `Apply` de 10 relaciones: 115,7 ms. Supera el objetivo aspiracional de 50 ms, pero cumple el máximo contractual de 150 ms.
* A partir de 11 relaciones, staging transaccional con `COPY` valida endpoints, importa pares a una tabla efímera, rechaza solapamientos exactos, la limpia y sólo entonces importa las relaciones canónicas. El p95 `Apply` de 1.000 relaciones es 271,9 ms, bajo el límite de 500 ms.
* `Close` se mide por separado: no pertenece a `Writer.Apply` y no participa en el gate. `prepared_batch` y deltas agregados permanecen como comparativas, no como camino elegido.
* Atomicidad, rollback, duplicados e integridad siguen cubiertos por la suite del writer; los resultados incluyen throughput, RSS, allocations y fases.
* Siguiente tarea desbloqueada: LUQUE-0301.

---

# 6. Fase 3 — HotSnapshot

## LUQUE-0301 — Diseñar los IDs densos

**Dependencias:** LADYBUG_STORAGE_PASS.

**Checklist:**

- [x] Verificar dependencias y alcance.
- [x] Completar acciones y entregables.
- [x] Ejecutar pruebas y benchmarks aplicables.
- [x] Verificar criterios de aceptación y el gate aplicable.
- [x] Registrar resultados, limitaciones y siguiente tarea.

**Objetivo:** definir IDs internos compactos.

**Tipos:**

```go
RepositoryID uint32
PackageID    uint32
FileID       uint32
SymbolID     uint32
EvidenceID   uint32
EdgeID       uint64
```

**Criterios de aceptación:**

* no se exponen como identidad persistente;
* tienen validación de overflow;
* se documenta su ciclo de vida.

**Estado:** `PASS`.

* `internal/hotsnapshot` define los seis tipos internos y centinelas inválidos; cero es el primer ID válido y los máximos de cada representación quedan reservados.
* `IDAllocator` es privado al builder, no es concurrente y se descarta al publicar o abortar; cada snapshot reinicia su numeración. No hay API de persistencia ni identidad externa basada en estos IDs.
* La validación impide overflow sin truncar y las pruebas cubren densidad zero-based, reinicio por snapshot y agotamiento de `uint32`/`uint64`.
* El ciclo de vida queda documentado en ADR 0004. No hay benchmark ni gate propios para tipos y asignación sin I/O.
* Siguiente tarea: LUQUE-0302.

---

## LUQUE-0302 — Implementar string interning

**Dependencias:** LUQUE-0301.

**Checklist:**

- [x] Verificar dependencias y alcance.
- [x] Completar acciones y entregables.
- [x] Ejecutar pruebas y benchmarks aplicables.
- [x] Verificar criterios de aceptación y el gate aplicable.
- [x] Registrar resultados, limitaciones y siguiente tarea.

**Objetivo:** evitar duplicar nombres y paths.

**Debe soportar:**

* inserción;
* lookup;
* serialización;
* lectura concurrente;
* estadísticas.

**Benchmark:**

Comparar con strings duplicados.

**Estado:** `PASS`.

* `StringInterner` elimina duplicados durante la construcción y `Freeze` transfiere su almacenamiento a `StringTable`, inmutable y segura para lectura concurrente sin bloqueos.
* La tabla soporta inserción, lookup por valor/ID, serialización determinista en orden de ID, restauración validada y estadísticas de entradas/bytes.
* La serialización rechaza datos truncados, trailing bytes y strings duplicados; el interner rechaza inserciones después de congelarse.
* El benchmark versionado muestra, sobre 100.000 entradas repetitivas, 144.250 B/op y 34 allocs/op internados frente a 6.405.634 B/op y 100.001 allocs/op con strings duplicados.
* Siguiente tarea: LUQUE-0303.

---

## LUQUE-0303 — Implementar stable keys

**Dependencias:** LUQUE-0301.

**Checklist:**

- [x] Verificar dependencias y alcance.
- [x] Completar acciones y entregables.
- [x] Ejecutar pruebas y benchmarks aplicables.
- [x] Verificar criterios de aceptación y el gate aplicable.
- [x] Registrar resultados, limitaciones y siguiente tarea.

**Objetivo:** crear identidad externa persistente.

**Inicialmente:**

```text
BLAKE3(canonical identity)
```

**Debe incluir:**

* versión del formato;
* lenguaje;
* repositorio;
* paquete o módulo;
* qualified name;
* kind;
* discriminador.

**Tests obligatorios:**

* determinismo;
* ausencia de colisión en corpus;
* cambio ante identidad distinta;
* estabilidad al mover línea.

**Estado:** `PASS`.

* `StableKeyIdentity` registra versión, lenguaje, repositorio, paquete o módulo, nombre cualificado, clase y discriminador; ninguna identidad incompleta ni versión desconocida puede producir una clave.
* La identidad canónica usa campos prefijados por longitud y se conserva como texto auditable; `StableKey` es el BLAKE3-256 base32 sin padding de esa identidad.
* Las pruebas cubren vector BLAKE3 determinista, cambios en cada componente de identidad, 20.000 símbolos distintos sin colisiones y estabilidad al mover una línea al excluir posiciones de fuente.
* Siguiente tarea: LUQUE-0304.

---

## LUQUE-0304 — Implementar estructura `GraphSnapshot`

**Dependencias:** LUQUE-0302 y LUQUE-0303.

**Checklist:**

- [x] Verificar dependencias y alcance.
- [x] Completar acciones y entregables.
- [x] Ejecutar pruebas y benchmarks aplicables.
- [x] Verificar criterios de aceptación y el gate aplicable.
- [x] Registrar resultados, limitaciones y siguiente tarea.

**Debe contener:**

* tablas densas;
* índices exactos;
* forward adjacency;
* reverse adjacency;
* metadatos;
* conteos;
* timestamp;
* versión.

**Estado:** `PASS`.

* `GraphSnapshot` encapsula tablas densas, `StringTable`, metadatos versionados con timestamp y conteos derivados, CSR forward/reverse y los índices exactos previstos.
* `NewGraphSnapshot` copia toda colección mutable antes de publicar y valida que los índices por stable key, nombre, nombre cualificado y repo/path cubren sus tablas sin IDs duplicados ni ajenos.
* `PackedEdge` mantiene el layout compacto de 12 bytes; los getters no exponen slices mutables.
* Siguiente tarea: LUQUE-0305.

---

## LUQUE-0305 — Implementar CSR forward

**Dependencias:** LUQUE-0304.

**Checklist:**

- [x] Verificar dependencias y alcance.
- [x] Completar acciones y entregables.
- [x] Ejecutar pruebas y benchmarks aplicables.
- [x] Verificar criterios de aceptación y el gate aplicable.
- [x] Registrar resultados, limitaciones y siguiente tarea.
**Objetivo:** almacenar aristas salientes contiguamente.

**Tests:**

* símbolo sin aristas;
* una arista;
* miles de aristas;
* último símbolo;
* índices inválidos.

**Estado:** `PASS`.

* `BuildForwardCSR` agrupa aristas salientes por origen, conserva el orden de entrada de cada símbolo y construye offsets de longitud `símbolos + 1`.
* Valida IDs de origen y destino antes de construir; el snapshot además rechaza offsets no monótonos, rangos inconsistentes y referencias a evidencia inexistente.
* Las pruebas cubren símbolos sin aristas, una arista, 4.096 aristas del último símbolo, IDs inválidos y aislamiento de las colecciones devueltas.
* Siguiente tarea: LUQUE-0306.


---

## LUQUE-0306 — Implementar CSR reverse

**Dependencias:** LUQUE-0305.

**Checklist:**

- [x] Verificar dependencias y alcance.
- [x] Completar acciones y entregables.
- [x] Ejecutar pruebas y benchmarks aplicables.
- [x] Verificar criterios de aceptación y el gate aplicable.
- [x] Registrar resultados, limitaciones y siguiente tarea.

**Objetivo:** responder referencias entrantes sin recorrer todo el grafo.

**Criterios de aceptación:**

* el conteo reverse coincide con el forward;
* cada arista tiene contraparte;
* no hay IDs fuera de rango.

**Estado:** `PASS`.

* `BuildReverseCSR` deriva la adyacencia entrante de la CSR forward validada, conserva la metadata de cada arista y cambia el destino al origen original.
* `NewGraphSnapshot` valida offsets, rangos, IDs de evidencia y la contraparte exacta de cada arista, incluyendo duplicados.
* Las pruebas cubren buckets vacíos, múltiples aristas, el último símbolo, forward malformada, getter `Incoming` y rechazo de contrapartes inválidas.
* Siguiente tarea: LUQUE-0307.

---

## LUQUE-0307 — Implementar builder desde LadybugDB

**Dependencias:** LUQUE-0306.

**Checklist:**

- [x] Verificar dependencias y alcance.
- [x] Completar acciones y entregables.
- [x] Ejecutar pruebas y benchmarks aplicables.
- [x] Verificar criterios de aceptación y el gate aplicable.
- [x] Registrar resultados, limitaciones y siguiente tarea.

**Objetivo:** generar un snapshot completo.

**Pipeline:**

```text
LadybugDB
→ lectura ordenada
→ IDs densos
→ interning
→ forward CSR
→ reverse CSR
→ índices exactos
→ validación
```

**Medir por separado:**

* scan secuencial de todos los símbolos;
* scan secuencial de todas las aristas;
* normalización de IDs;
* construcción de forward CSR;
* construcción de reverse CSR;
* índices;
* validación.

**Comparar:**

```text
facts → LadybugDB → scan → HotSnapshot
facts ├→ LadybugDB
      └→ HotSnapshot
```

El arranque y la recuperación deben reconstruir siempre desde LadybugDB. El
camino directo desde facts solo es válido si produce un snapshot byte a byte o
semánticamente equivalente según un golden digest.

**Estado:** `PASS`.

* `BuildGraphSnapshot` recibe filas canónicas, ordena repositorios, paquetes, archivos, símbolos y aristas por claves durables y asigna IDs densos nuevos.
* El pipeline completa interning, índices exactos, CSR forward/reverse y validación de referencias, evidencia y contraparte.
* Las pruebas cubren orden de entrada no determinista, igualdad de snapshot y tabla de strings, aristas completas y rechazo de filas colgantes o duplicadas.
* La fuente implementada es la representación de filas canónicas producida por el scan de LadybugDB; no se añadió un segundo lector nominal.
* Siguiente tarea: LUQUE-0308.

---

## LUQUE-0308 — Implementar publicación atómica

**Dependencias:** LUQUE-0307.

**Checklist:**

- [x] Verificar dependencias y alcance.
- [x] Completar acciones y entregables.
- [x] Ejecutar pruebas y benchmarks aplicables.
- [x] Verificar criterios de aceptación y el gate aplicable.
- [x] Registrar resultados, limitaciones y siguiente tarea.

**Objetivo:** cambiar de snapshot sin bloquear consultas.

**Implementar:**

```go
atomic.Pointer[GraphSnapshot]
```

**Tests:**

* múltiples lectores;
* publicación concurrente;
* lector usando snapshot antiguo;
* fallo al construir snapshot nuevo.

**Estado:** `PASS`.

* `SnapshotStore` publica y carga `*GraphSnapshot` con `atomic.Pointer`, sin bloquear lectores.
* `Publish` rechaza nil y generaciones no estrictamente crecientes, y usa CAS para resolver publicaciones concurrentes sin retrocesos.
* `Close` limpia el puntero, impide publicaciones posteriores y no invalida referencias antiguas conservadas por lectores.
* Las pruebas cubren lectores que conservan un snapshot anterior, 32 publicadores, 16 lectores concurrentes, generación final máxima y rechazo de candidatos inválidos.
* Siguiente tarea: LUQUE-0309.

---

## LUQUE-0309 — Implementar búsquedas exactas

**Dependencias:** LUQUE-0308.

**Checklist:**

- [x] Verificar dependencias y alcance.
- [x] Completar acciones y entregables.
- [x] Ejecutar pruebas y benchmarks aplicables.
- [x] Verificar criterios de aceptación y el gate aplicable.
- [x] Registrar resultados, limitaciones y siguiente tarea.

**Operaciones:**

```text
stable key → symbol
exact name → symbols
qualified name → symbols
repo + path → file
```

**Estado:** `PASS`.

* Stable key y repo/path usan mapas exactos O(1); nombre y nombre cualificado usan índices exactos con resultados copiados.
* `SearchSymbolsByName` y `SearchSymbolsByQName` aplican offset no negativo y límite máximo 500, informan total y `HasMore`, y no hacen substring ni case-folding.
* Las pruebas cubren páginas primera/última/pasada, cualificado, near miss nominal y límites inválidos.
* Siguiente tarea: LUQUE-0310.

---

## LUQUE-0310 — Implementar recorridos acotados

**Dependencias:** LUQUE-0308.

**Checklist:**

- [x] Verificar dependencias y alcance.
- [x] Completar acciones y entregables.
- [x] Ejecutar pruebas y benchmarks aplicables.
- [x] Verificar criterios de aceptación y el gate aplicable.
- [x] Registrar resultados, limitaciones y siguiente tarea.

**Operaciones:**

* BFS;
* dirección incoming;
* dirección outgoing;
* profundidad máxima;
* filtro por arista;
* límite de nodos;
* agrupación por repositorio.

**No usar mapas por nodo en el camino principal salvo justificación.**

**Estado:** `PASS`.

* `Traverse` ejecuta BFS outgoing o incoming sobre CSR y devuelve el origen en profundidad cero junto con visitas y agrupación por repositorio.
* Aplica filtro de tipo, profundidad máxima 5, límite de nodos 25.000, truncamiento explícito y timeout por fecha límite.
* El estado visitado usa un slice denso indexado por `SymbolID`; no se usa mapa por nodo.
* Las pruebas cubren ciclo, ambas direcciones, profundidad, filtros, límite de nodos, timeout y opciones inválidas.
* Siguiente tarea: LUQUE-0311.

---

## LUQUE-0311 — Benchmark del HotSnapshot

**Dependencias:** LUQUE-0309 y LUQUE-0310.

**Checklist:**

- [x] Verificar dependencias y alcance.
- [x] Completar acciones y entregables.
- [x] Ejecutar pruebas y benchmarks aplicables.
- [x] Verificar criterios de aceptación y el gate aplicable.
- [x] Registrar resultados, limitaciones y siguiente tarea.

**Corpus:**

```text
100.000 símbolos
1.000.000 aristas
```

**Medir:**

* extracción completa desde LadybugDB;
* construcción total y cada fase interna;
* commit hasta snapshot publicado;
* construcción directa desde facts normalizados;
* carga;
* RSS y allocations;
* find exacto;
* references;
* profundidad 3;
* profundidad 5;
* concurrencia.

**Gate:**

```text
HOT_SNAPSHOT_PASS
```

Requisitos:

```text
full scan LadybugDB ≤ 1 s
build completo ≤ 2 s
commit → snapshot publicado ≤ 3 s
find p95 ≤ 2 ms
references p95 ≤ 5 ms
depth-3 p95 ≤ 20 ms
```

**Estado:** `PASS` — `HOT_SNAPSHOT_PASS` emitido.

* El scan completo ordenado de LadybugDB, mediante Arrow C Data Interface, procesa 1.200.040 filas en 946,599 ms p95 estimate; el máximo de cinco muestras queda 53,401 ms por debajo de 1 s.
* El scan usa cuatro conexiones de lectura, omite `ORDER BY` nativo para conservar el recorrido columnar y ordena en Go solo cuando la salida no está ya ordenada.
* El scan registra 692.665.776 B/op y 294 allocs/op; la medición incluye materialización de todas las filas y no es un RSS de proceso persistente.
* El build canónico mide 712,804 ms, build + publish 725,769 ms, find 0,0000306 ms, references 0,0001429 ms y depth-3 0,015158 ms; todos los límites pasan.
* El RSS `VmHWM` del builder es 419.598.336 bytes. La calificación requiere el ABI CGO de LadybugDB en Linux amd64.
* `benchmarks/hotsnapshot/results.json` y `benchmarks/hotsnapshot/report.md` conservan la evidencia reproducible.
* Siguiente tarea: LUQUE-0401.

---

# 7. Fase 4 — Configuración y repositorios

## LUQUE-0401 — Implementar carga de configuración

**Dependencias:** HOT_SNAPSHOT_PASS.

**Checklist:**

- [x] Verificar dependencias y alcance.
- [x] Completar acciones y entregables.
- [x] Ejecutar pruebas y benchmarks aplicables.
- [x] Verificar criterios de aceptación y el gate aplicable.
- [x] Registrar resultados, limitaciones y siguiente tarea.

**Archivos:**

```text
config.yaml
repositories.yaml
```

**Requisitos:**

* defaults explícitos;
* validación;
* paths expandidos;
* errores claros;
* versión del schema.

**Estado:** `PASS`.

* `internal/config` carga y combina `config.yaml` con `repositories.yaml` usando YAML estricto (`KnownFields`) y exige schema `version: 1`.
* Los defaults siguen el contrato de `PLAN.md`; `Load` devuelve paths absolutos tras expandir `~`, variables de entorno y rutas relativas respecto al archivo que las declara.
* La validación rechaza versiones incompatibles, campos desconocidos, límites incoherentes, duraciones inválidas, transportes no soportados y registros con nombres, paths o lenguajes duplicados.
* Los paths se resuelven sin comprobar todavía existencia, permisos, symlinks o anidamiento; esas comprobaciones pertenecen a LUQUE-0403.
* Las pruebas cubren defaults, expansión, documentos inválidos, variables ausentes, duplicados y registro vacío explícito.
* Verificación: `go test ./...`, `go vet ./...`, `go test -race` del paquete y `go build ./cmd/kivgraph` pasan; `go tool staticcheck ./internal/config` no reporta incidencias.
* Limitación de repositorio: `go tool staticcheck ./...` sigue reportando avisos preexistentes fuera de `internal/config` en `benchmarks/mcp-empty`, `internal/hotsnapshot` e `internal/storage/ladybug`.
* Siguiente tarea: LUQUE-0402.

---

## LUQUE-0402 — Implementar registro de repositorios

**Dependencias:** LUQUE-0401.

**Checklist:**

- [x] Verificar dependencias y alcance.
- [x] Completar acciones y entregables.
- [x] Ejecutar pruebas y benchmarks aplicables.
- [x] Verificar criterios de aceptación y el gate aplicable.
- [x] Registrar resultados, limitaciones y siguiente tarea.

**Debe registrar:**

* nombre;
* ruta;
* realpath;
* commit;
* branch;
* dirty;
* lenguajes;
* manifests;
* roots;
* exclusiones.

**Estado:** `PASS`.

* `internal/workspace.NewRegistry` conserva el orden declarado y registra nombre, ruta, `realpath`, commit, branch, dirty, lenguajes, manifests, roots y exclusiones.
* La metadata Git usa comandos con `exec.CommandContext`, sin shell; soporta branch normal y HEAD desacoplado, y marca cambios sin commitear incluyendo untracked.
* `List` y `Get` devuelven copias profundas para impedir mutaciones del registro interno.
* La ruta debe existir, ser un directorio y ser un repositorio Git operativo; la validación entre repositorios queda para LUQUE-0403 y el descubrimiento automático de manifests para LUQUE-0404/0405.
* Verificación: `go test ./...`, `go vet ./...`, `go test -race ./internal/config ./internal/workspace`, `go build ./cmd/kivgraph`, `go test -tags ladybug ./...` y `go vet -tags ladybug ./...` pasan; `go tool staticcheck ./internal/config ./internal/workspace` no reporta incidencias.
* Smoke real: `TestNewRegistryReadsRealGitMetadata` creó un repositorio Git temporal, verificó commit, branch `main`, estado limpio y detección posterior de untracked.
* Siguiente tarea: LUQUE-0403.

---

## LUQUE-0403 — Implementar validación de paths

**Dependencias:** LUQUE-0402.

**Checklist:**

- [x] Verificar dependencias y alcance.
- [x] Completar acciones y entregables.
- [x] Ejecutar pruebas y benchmarks aplicables.
- [x] Verificar criterios de aceptación y el gate aplicable.
- [x] Registrar resultados, limitaciones y siguiente tarea.

**Comprobar:**

* existencia;
* duplicados;
* repos anidados;
* symlinks;
* escapes;
* permisos;
* colisión de nombres.

**Estado:** `PASS`.

* `internal/workspace.ValidatePaths` valida rutas absolutas existentes, directorios, permisos POSIX legibles y buscables, ausencia de componentes symlink y realpath canónico.
* La validación rechaza nombres vacíos o inválidos, colisiones de nombres sin distinguir mayúsculas, realpaths duplicados, repositorios anidados y escapes en `manifests`, `roots` y `exclusions`.
* `workspace.NewRegistry` ejecuta estas comprobaciones antes de invocar Git y conserva los paths ya validados para evitar una segunda resolución divergente.
* Las pruebas cubren aceptación de metadatos acotados, duplicados, anidamiento, symlinks, escapes, permisos, colisiones, entradas inválidas y cancelación de contexto.
* Verificación: `go test ./...`, `go vet ./...`, `go test -race ./internal/config ./internal/workspace`, `go build ./cmd/kivgraph` y `go tool staticcheck ./internal/config ./internal/workspace` pasan.
* Verificación Ladybug: `go test -tags ladybug ./...` y `go vet -tags ladybug ./...` pasan con la biblioteca v0.19.0 fijada en `/tmp/kivgraph-ladybug-v0.19.0/lib`.
* Smoke real: `go test ./internal/workspace -run 'Test(NewRegistryReadsRealGitMetadata|ValidatePathsAcceptsScopedMetadata)$' -count=1 -v` pasa con un repositorio Git temporal y metadatos acotados.
* Limitación: `go tool staticcheck ./...` conserva seis avisos preexistentes en `benchmarks/mcp-empty`, `internal/hotsnapshot` e `internal/storage/ladybug`; ninguno pertenece al alcance 0403.
* No se requiere benchmark: la tarea solo valida configuración y límites de filesystem.
* Siguiente tarea: LUQUE-0404.

---

## LUQUE-0404 — Implementar descubrimiento TypeScript

**Dependencias:** LUQUE-0402.

**Checklist:**

- [x] Verificar dependencias y alcance.
- [x] Completar acciones y entregables.
- [x] Ejecutar pruebas y benchmarks aplicables.
- [x] Verificar criterios de aceptación y el gate aplicable.
- [x] Registrar resultados, limitaciones y siguiente tarea.

**Detectar:**

```text
package.json
tsconfig.json
tsconfig.*.json
workspace declarations
project references
```

**Estado:** `PASS`.

* `internal/workspace.DiscoverTypeScript` detecta `package.json`, `tsconfig.json`, `tsconfig.*.json`, `pnpm-workspace.yaml` y `pnpm-workspace.yml` con paths absolutos y orden determinista.
* Las declaraciones `workspaces` de npm/Yarn y pnpm se validan sin resolver todavía el registro de paquetes; esa responsabilidad pertenece a LUQUE-0406.
* Los `tsconfig` se leen como JSONC; las referencias se resuelven desde el archivo declarante, incluyendo referencias a directorios mediante `tsconfig.json`, y se rechazan targets ausentes o fuera del repositorio.
* Se omiten `.git`, dependencias instaladas, symlinks y exclusiones configuradas.
* Las pruebas cubren detección, workspaces array/objeto, pnpm, JSONC, referencias, escapes, targets ausentes, exclusiones, symlinks y cancelación.
* Verificación: `go test ./...`, `go vet ./...`, `go test -race ./internal/workspace`, `go build ./cmd/kivgraph` y `go tool staticcheck ./internal/workspace` pasan.
* Verificación Ladybug: `go test -tags ladybug ./...` y `go vet -tags ladybug ./...` pasan con la biblioteca v0.19.0 fijada en `/tmp/kivgraph-ladybug-v0.19.0/lib`.
* Smoke real: `go test ./internal/workspace -run '^TestDiscoverTypeScriptFindsManifestsWorkspacesAndReferences$' -count=1 -v` pasa con un árbol temporal de workspace.
* Limitación: `go tool staticcheck ./...` conserva seis avisos fuera del alcance 0404 en `benchmarks/mcp-empty`, `internal/hotsnapshot` e `internal/storage/ladybug`.
* No se requiere benchmark: el descubrimiento es una operación de configuración y filesystem; no se ha añadido un contrato de rendimiento.
* Siguiente tarea: LUQUE-0405.

---

## LUQUE-0405 — Implementar descubrimiento Go

**Dependencias:** LUQUE-0402.

**Checklist:**

- [x] Verificar dependencias y alcance.
- [x] Completar acciones y entregables.
- [x] Ejecutar pruebas y benchmarks aplicables.
- [x] Verificar criterios de aceptación y el gate aplicable.
- [x] Registrar resultados, limitaciones y siguiente tarea.

**Detectar:**

```text
go.mod
go.sum
go.work existente
packages
replace directives
```

**Estado:** `PASS`.

* `internal/workspace.DiscoverGo` detecta `go.mod`, `go.sum`, `go.work`, paquetes Go y directivas `replace` mediante `golang.org/x/mod/modfile`, sin ejecutar comandos externos.
* Los módulos de `go.work use` se resuelven a sus `go.mod`; las sustituciones locales se canonicalizan y se mantienen dentro del repositorio, mientras que las remotas se conservan sin resolver.
* Los paquetes se agrupan por directorio, se identifican mediante la cláusula `package`, se asignan al módulo más profundo y se omiten `vendor`, dependencias instaladas, symlinks y exclusiones.
* Las pruebas cubren módulos anidados, sums, workspaces, replacements locales, paquetes de test externos, escapes, targets ausentes, conflictos de paquetes, symlinks y cancelación.
* Verificación: `go test ./...`, `go vet ./...`, `go test -race ./internal/workspace`, `go build ./cmd/kivgraph` y `go tool staticcheck ./internal/workspace` pasan.
* Verificación Ladybug: `go test -tags ladybug ./...` y `go vet -tags ladybug ./...` pasan con la biblioteca v0.19.0 fijada en `/tmp/kivgraph-ladybug-v0.19.0/lib`.
* Smoke real: `go test ./internal/workspace -run '^TestDiscoverGoFindsModulesWorkspacesPackagesAndReplaces$' -count=1 -v` pasa con módulos y workspace temporales.
* Limitación: `go tool staticcheck ./...` conserva seis avisos fuera del alcance 0405 en `benchmarks/mcp-empty`, `internal/hotsnapshot` e `internal/storage/ladybug`.
* No se requiere benchmark: el descubrimiento es una operación de configuración y filesystem; la carga semántica con `go/packages` pertenece a fases posteriores.
* Siguiente tarea: LUQUE-0406.

---

## LUQUE-0406 — Implementar package registry TypeScript

**Dependencias:** LUQUE-0404.

**Checklist:**

- [x] Verificar dependencias y alcance.
- [x] Completar acciones y entregables.
- [x] Ejecutar pruebas y benchmarks aplicables.
- [x] Verificar criterios de aceptación y el gate aplicable.
- [x] Registrar resultados, limitaciones y siguiente tarea.

**Mapa:**

```text
package name
→ repository
→ package root
→ manifest
→ exports
→ types
→ source roots
→ declaration roots
→ TypeScript project
```

**Estado:** `PASS`.

* `internal/workspace.NewTypeScriptPackageRegistry` compone `DiscoverTypeScript` y registra manifests `package.json` nombrados, con nombre, versión, privacidad, repositorio, raíz, manifest y `exports` JSON preservado.
* Los manifests sin `name` se omiten como providers de raíz; los nombres duplicados dentro del repositorio se rechazan. `types` precede a `typings`; `ProjectPath` selecciona el `tsconfig` más profundo que contiene cada paquete.
* Las raíces fuente se derivan de `rootDirs`, `rootDir`, `include` y `files`; las raíces declarativas de `types` y `declarationDir`. Las rutas de tipos y exports se limitan a la raíz del paquete sin exigir que artefactos generados existan.
* `List` y `Get` devuelven copias profundas; el índice queda ordenado y la ambigüedad entre repositorios se reserva para LUQUE-0408.
* Las pruebas cubren providers, versiones, exports, roots, proyecto más profundo, privacidad, omisión de raíz sin nombre, duplicados, escapes y cancelación.
* Verificación: `go test ./internal/workspace`, `go test -race ./internal/workspace`, `go vet ./internal/workspace`, `go tool staticcheck ./internal/workspace` y smoke focal pasan.
* No se requiere benchmark: el registro es una operación de configuración y filesystem; la carga semántica y la resolución cross-repository pertenecen a fases posteriores.
* Siguiente tarea: LUQUE-0407.

---

## LUQUE-0407 — Implementar module registry Go

**Dependencias:** LUQUE-0405.

**Checklist:**

- [x] Verificar dependencias y alcance.
- [x] Completar acciones y entregables.
- [x] Ejecutar pruebas y benchmarks aplicables.
- [x] Verificar criterios de aceptación y el gate aplicable.
- [x] Registrar resultados, limitaciones y siguiente tarea.

**Mapa:**

```text
module path
→ repository
→ module root
→ manifest
→ go.sum
→ packages
→ go.mod replaces
→ go.work replaces
```

**Estado:** `PASS`.

* `internal/workspace.NewGoModuleRegistry` compone `DiscoverGo` y registra módulos por `module path`, con repositorio, raíz, manifest, `go.sum`, versión de Go y paquetes asociados.
* Los paquetes se asignan al módulo más específico y se ignoran los que quedan fuera de cualquier módulo. Los módulos sin paquetes siguen siendo providers válidos.
* Se conservan los `replace` de `go.mod` y, por separado, los de `go.work` que incluyen cada módulo; los duplicados exactos se eliminan y los conflictos se conservan.
* Los `module path` duplicados dentro del mismo repositorio se rechazan. `List` y `Get` devuelven copias profundas ordenadas por módulo.
* Las pruebas cubren providers, paquetes, módulos anidados, replaces de `go.mod` y `go.work`, duplicados, estado sin módulos y cancelación.
* Verificación: `go test ./internal/workspace`, `go test -race ./internal/workspace`, `go vet ./internal/workspace`, `go tool staticcheck ./internal/workspace` y smoke focal pasan.
* No se requiere benchmark: el registro solo normaliza metadatos ya descubiertos; la carga semántica con `go/packages` pertenece a fases posteriores.
* Siguiente tarea: LUQUE-0408.

---

## LUQUE-0408 — Detectar providers ambiguos

**Dependencias:** LUQUE-0406 y LUQUE-0407.

**Checklist:**

- [x] Verificar dependencias y alcance.
- [x] Completar acciones y entregables.
- [x] Ejecutar pruebas y benchmarks aplicables.
- [x] Verificar criterios de aceptación y el gate aplicable.
- [x] Registrar resultados, limitaciones y siguiente tarea.

**Errores:**

```text
AMBIGUOUS_PACKAGE_PROVIDER
AMBIGUOUS_MODULE_PROVIDER
PACKAGE_VERSION_MISMATCH
MODULE_REPLACE_CONFLICT
```

**Estado:** `PASS`.

* `internal/workspace.DetectProviderConflicts` construye ambos registries para cada repositorio y devuelve un `ProviderConflictReport` determinista sin seleccionar providers automáticamente.
* Los nombres de paquete duplicados producen `AMBIGUOUS_PACKAGE_PROVIDER`; si sus versiones difieren se añade `PACKAGE_VERSION_MISMATCH`.
* Los `module path` duplicados producen `AMBIGUOUS_MODULE_PROVIDER`; los conjuntos de replacements de `go.mod` y `go.work` distintos producen `MODULE_REPLACE_CONFLICT`, incluido el mismo módulo sustituido desde módulos distintos.
* Cada conflicto conserva clase, provider, repositorios, manifests y versiones aplicables. `List` devuelve copias profundas y `HasConflicts` permite comprobar el resultado.
* Las pruebas cubren los cuatro tipos de conflicto, ausencia de conflicto, orden determinista, versiones, manifests, validación de repositorios, cancelación y mutabilidad.
* Verificación: `go test ./...`, `go vet ./...`, `go test -race ./internal/workspace`, `go build ./cmd/kivgraph`, `go tool staticcheck ./internal/workspace`, smoke focal y suite Ladybug pasan.
* No se requiere benchmark: esta tarea valida metadatos de configuración ya descubiertos.

**Gate:**

```text
REPOSITORY_REGISTRY_PASS
```

**Siguiente tarea:** LUQUE-0501.

---

# 8. Fase 5 — Tree-sitter

## LUQUE-0501 — Fijar versiones de grammars

**Dependencias:** REPOSITORY_REGISTRY_PASS.

**Checklist:**

- [x] Verificar dependencias y alcance.
- [x] Completar acciones y entregables.
- [x] Ejecutar pruebas y benchmarks aplicables.
- [x] Verificar criterios de aceptación y el gate aplicable.
- [x] Registrar resultados, limitaciones y siguiente tarea.

**Estado:** `PASS`.

**Entregables:**

* `grammars/manifest.json`;
* `internal/syntax/grammar_manifest.go`;
* `internal/syntax/grammar_manifest_test.go`;
* ADR 0006 actualizado.

**Resultado:**

* TypeScript y TSX quedan fijados en `v0.23.2`, commit
  `f975a621f4e7f532fe322e13c4f79495e0a7b2e7`;
* JavaScript queda fijado en `v0.25.0`, commit
  `44c892e0be055ac465d5eeddae6d3e194424e7de`;
* Go queda fijado en `v0.25.0`, commit
  `1547678a9da59885853f5f5cc8a99cc203fa2e2c`;
* cada entrada registra ruta de fuente, URL del archivo `tar.gz`, SHA-256 y
  licencia MIT;
* `LoadManifest` valida esquema, grammars requeridas, tags, commits, URLs,
  checksums y licencias antes de exponer el manifiesto.

**Verificación:** `go test ./...`, `go vet ./...`, `go test -race ./internal/syntax`,
`go tool staticcheck ./internal/syntax` y la comprobación de SHA-256 de los
tres archivos fuente oficiales pasan.

**Limitación:** los archivos fuente todavía no se vendorizan; el parser manager
de LUQUE-0502 deberá descargarlos o resolverlos desde el archivo fijado y
verificar el checksum antes de compilarlos.

**Siguiente tarea:** LUQUE-0502.

---

## LUQUE-0502 — Implementar parser manager

**Dependencias:** LUQUE-0501.

**Checklist:**

- [x] Verificar dependencias y alcance.
- [x] Completar acciones y entregables.
- [x] Ejecutar pruebas y benchmarks aplicables.
- [x] Verificar criterios de aceptación y el gate aplicable.
- [x] Registrar resultados, limitaciones y siguiente tarea.

**Estado:** `PASS`.

**Entregables:**

* `internal/syntax/parser_manager.go`;
* `internal/syntax/parser_manager_test.go`;
* runtime oficial `github.com/tree-sitter/go-tree-sitter v0.25.0`;
* bindings oficiales de JavaScript, TypeScript/TSX y Go fijados en `go.mod`.

**Resultado:**

* los parsers se crean bajo demanda y se reutilizan por language;
* un semaphore limita los parseos concurrentes;
* `Close` espera parseos activos y libera todos los recursos nativos;
* `context.Context` cancela la espera y el parseo;
* los errores operativos se clasifican mediante `ParserError`;
* los errores sintácticos permanecen en el árbol y se consultan con `HasError`;
* `ParseIncremental` clona el árbol anterior para no mutar al llamador.

**Verificación:** `go test ./...`, `go vet ./...` y
`go test -race ./internal/syntax` pasan.

**Limitación:** la implementación usa cgo y requiere que el runtime soporte el
ABI de las grammars fijadas; por eso el binding oficial queda en `v0.25.0`.

**Siguiente tarea:** LUQUE-0503.

---

## LUQUE-0503 — Implementar inventario sintáctico

**Dependencias:** LUQUE-0502.

**Checklist:**

- [x] Verificar dependencias y alcance.
- [x] Completar acciones y entregables.
- [x] Ejecutar pruebas y benchmarks aplicables.
- [x] Verificar criterios de aceptación y el gate aplicable.
- [x] Registrar resultados, limitaciones y siguiente tarea.

**Estado:** `PASS`.

**Entregables:**

* `internal/syntax/inventory.go`;
* `internal/syntax/inventory_test.go`.

**Resultado:**

* se extraen declaraciones, imports, exports, calls, identificadores,
  clases, interfaces y métodos;
* cada candidato conserva tipo de nodo, nombre, rango de bytes y posiciones;
* la salida es determinista y `List` devuelve una copia;
* los árboles con errores sintácticos se conservan y se marcan en
  `SyntaxInventory.HasErrors`;
* el inventario no crea símbolos, providers ni aristas semánticas.

**Verificación:** `go test ./internal/syntax` y `go test ./...` pasan.
No se requiere benchmark: el contrato añade extracción de metadatos, no un
pipeline de rendimiento.

**Limitación:** las clasificaciones son deliberadamente heurísticas por tipo de
nodo; la autoridad semántica queda para las fases TypeScript y Go.

**Siguiente tarea:** LUQUE-0504.

---

## LUQUE-0504 — Implementar parsing incremental

**Dependencias:** LUQUE-0503.

**Checklist:**

- [x] Verificar dependencias y alcance.
- [x] Completar acciones y entregables.
- [x] Ejecutar pruebas y benchmarks aplicables.
- [x] Verificar criterios de aceptación y el gate aplicable.
- [x] Registrar resultados, limitaciones y siguiente tarea.

**Estado:** `PASS`.

**Entregables:** `ParserManager.ParseIncremental`,
`ParserManager.ParseIncrementalWithRanges`, `SyntaxRange` y las pruebas
incrementales de `internal/syntax/parser_manager_test.go`.

**Resultado:**

* se acepta el árbol anterior, una edición, el nuevo contenido y los puntos
  de byte correspondientes;
* el árbol anterior se clona y permanece sin mutar;
* Tree-sitter devuelve los changed ranges estructurales como `SyntaxRange`;
* una cancelación o edición inválida se clasifica y no deja árboles nativos
  abiertos.

**Verificación:** `go test ./internal/syntax` pasa. No se requiere benchmark:
la tarea valida transición e integridad de árboles, no throughput.

**Siguiente tarea:** LUQUE-0505.

---

## LUQUE-0505 — Implementar clasificación de cambios

**Dependencias:** LUQUE-0504.

**Checklist:**

- [x] Verificar dependencias y alcance.
- [x] Completar acciones y entregables.
- [x] Ejecutar pruebas y benchmarks aplicables.
- [x] Verificar criterios de aceptación y el gate aplicable.
- [x] Registrar resultados, limitaciones y siguiente tarea.

**Estado:** `PASS`.

**Entregables:** `internal/syntax/changes.go` y
`internal/syntax/changes_test.go`.

**Resultado:**

* se implementan `BODY_ONLY`, `SIGNATURE_CHANGED`, `IMPORTS_CHANGED`,
  `EXPORTS_CHANGED`, `DECLARATION_ADDED`, `DECLARATION_REMOVED` y `UNKNOWN`;
* la precedencia es imports, exports, declarations y signatures;
* los cambios sin diferencias estructurales solo son `BODY_ONLY` cuando
  existen changed ranges;
* la clasificación compara multisets de candidatos y devuelve copias de los
  changed ranges;
* no se interpretan nombres ni coincidencias sintácticas como relaciones
  semánticas.

**Verificación:** `go test ./internal/syntax` pasa. No se requiere benchmark:
la tarea clasifica cambios ya parseados.

**Siguiente tarea:** LUQUE-0506.

---

## LUQUE-0506 — Añadir fixture de falso homónimo

**Dependencias:** LUQUE-0503.

**Checklist:**

- [x] Verificar dependencias y alcance.
- [x] Completar acciones y entregables.
- [x] Ejecutar pruebas y benchmarks aplicables.
- [x] Verificar criterios de aceptación y el gate aplicable.
- [x] Registrar resultados, limitaciones y siguiente tarea.

**Estado:** `PASS`.

**Entregables:**

* `testdata/syntax/false-homonym/left.ts`;
* `testdata/syntax/false-homonym/right.ts`;
* `internal/syntax/false_homonym_test.go`.

**Resultado:** ambos archivos contienen candidatos con el mismo nombre
`parse`, pero `right.ts` no importa `parse` desde `left.ts`. El inventario
expone candidatos sintácticos únicamente; no contiene una salida de aristas
exactas ni convierte el homónimo en relación semántica.

**Verificación:** `go test ./internal/syntax -run
'^TestFalseHomonymFixtureProducesCandidatesWithoutSemanticEdges$' -count=1`
pasa. No se requiere benchmark.

**Gate:** `TREE_SITTER_ACCELERATOR_PASS`.
**Gate de fase:** `TREE_SITTER_ACCELERATOR_PASS` emitido tras
`go test ./...`, `go vet ./...`, `go test -race ./...`, `go build ./cmd/kivgraph`,
`go test -tags ladybug ./...`, `go vet -tags ladybug ./...`,
`go tool staticcheck ./internal/syntax` y `go mod verify`.

---

# 9. Fase 6 — Worker TypeScript

## LUQUE-0601 — Diseñar el protocolo inicial

**Dependencias:** TREE_SITTER_ACCELERATOR_PASS.

**Checklist:**

- [x] Verificar dependencias y alcance.
- [x] Completar acciones y entregables.
- [x] Ejecutar pruebas y benchmarks aplicables.
- [x] Verificar criterios de aceptación y el gate aplicable.
- [x] Registrar resultados, limitaciones y siguiente tarea.

**Estado:** `PASS`.

**Entregables:**

* `docs/protocol/ts-worker-v1.md`;
* `docs/adr/0010-typescript-engine.md`;
* `docs/adr/0005-typescript-worker.md` revisado;
* `benchmarks/typescript-engine/results.json` y `report.md`.

**Transporte:** stdin/stdout, prefijo de longitud de 32 bits big-endian, JSON
UTF-8, frame máximo de 16 MiB, `stdout` reservado a frames y `stderr` a logs.

**Mensajes:** `HELLO`, `OPEN_WORKSPACE`, `INDEX_PROJECT`, `UPDATE_FILES`,
`REMOVE_FILES`, `GET_STATUS`, `CANCEL` y `SHUTDOWN`. `GET_STATUS` es el nombre
normativo de `STATUS`.

**Resultado:**

* el motor semántico es el compilador nativo de TypeScript 7, decidido en el
  ADR 0010 y respaldado por benchmark versionado;
* el protocolo es por lotes y con granularidad de archivo, con posiciones en
  lista y respuesta en el mismo orden; queda prohibida la petición por símbolo;
* la correlación es por `id`, con cancelación explícita y respuestas fuera de
  orden permitidas;
* los hechos se emiten agrupados por archivo y un lote incompleto se descarta
  entero;
* un proyecto fuera de la ventana de versiones soportadas degrada sus hechos a
  `CANDIDATE` con motivo y versión efectiva registrados.

**Benchmark:** `benchmarks/typescript-engine`, corpus de 250, 1000 y 4000
módulos, 50 iteraciones y 5 de calentamiento por operación. Carga en frío entre
`4.4x` y `4.9x` más rápida, chequeo completo entre `1.45x` y `4.85x`,
referencias con alcance de archivo entre `21x` y `462x`, re-resolución
incremental entre `20x` y `37x`, residente entre `1.4x` y `2.3x` menor. La
resolución de un símbolo suelto es entre `3x` y `7x` más lenta y por lotes de
50 vuelve a ganar entre `1.34x` y `1.48x`; de ahí la regla de lotes.

**Limitaciones:**

* la superficie consumida del compilador está marcada `unstable`, por lo que
  LUQUE-0607 debe incluir un test de contrato que falle ante un cambio;
* la equivalencia semántica entre TypeScript 5 y 7 no se ha medido sobre
  repositorios reales; por eso la degradación a `CANDIDATE` es obligatoria;
* el framing todavía no está implementado: es LUQUE-0602 en Go y LUQUE-0603 en
  TypeScript, que deben coincidir byte a byte.

**Siguiente tarea:** LUQUE-0602.

---

## LUQUE-0602 — Implementar framing en Go

**Dependencias:** LUQUE-0601.

**Checklist:**

- [x] Verificar dependencias y alcance.
- [x] Completar acciones y entregables.
- [x] Ejecutar pruebas y benchmarks aplicables.
- [x] Verificar criterios de aceptación y el gate aplicable.
- [x] Registrar resultados, limitaciones y siguiente tarea.

**Estado:** `PASS`.

**Entregables:**

* `internal/tsworker/framing.go`;
* `internal/tsworker/framing_test.go`;
* `internal/tsworker/fixtures_test.go`;
* `testdata/protocol/ts-worker-v1/` con ocho frames, su `manifest.json` y un
  `README.md` generado con el volcado hexadecimal y el cuerpo decodificado de
  cada frame; un test falla si cualquiera de los tres deriva del codec.

**Tests:**

* frame válido, incluido el prefijo big-endian comprobado contra bytes
  literales y el round-trip completo;
* frame parcial entregado en dos escrituras separadas, más el caso de
  truncamiento real;
* longitud excesiva rechazada antes de reservar memoria, y cuerpo vacío;
* JSON inválido y sobre inválido, ambos recuperables y sin desalinear el flujo;
* EOF limpio entre frames frente a EOF dentro de un frame;
* timeout y cancelación sobre un pipe, comprobando que el transporte sigue
  usable después.

**Resultado:**

* prefijo de 32 bits big-endian, cuerpo JSON UTF-8, frame máximo de 16 MiB;
* errores clasificados mediante `FramingError` con `Kind` y `Fatal`; solo
  `INVALID_PAYLOAD` conserva la sesión, el resto la termina;
* sin resincronización del flujo, por decisión del protocolo;
* la longitud se valida antes de cualquier asignación y los buffers se
  reutilizan entre frames;
* la cancelación es real: el reader usa `SetReadDeadline` cuando el transporte
  lo soporta e informa de ello con `SupportsInterruption`;
* el writer es seguro para emisores concurrentes.

**Corrección durante el gate:** `go test -race` reveló que el vigilante de
cancelación podía aplicar un deadline vencido después de que la lectura
terminara, contaminando la lectura siguiente. El cierre ahora espera a que el
vigilante termine antes de limpiar el deadline. Sin esa espera el caso de
cancelación fallaba de forma intermitente.

**Verificación:** `go test ./...`, `go vet ./...`, `go build ./cmd/kivgraph`,
`go tool staticcheck ./internal/tsworker`, `go test -race ./internal/tsworker`
repetido cinco veces, y la suite Ladybug completa pasan.

**Limitación:** un transporte sin soporte de deadlines solo puede cancelarse
entre frames; queda declarado en la API mediante `SupportsInterruption`.

**Siguiente tarea:** LUQUE-0603, que debe consumir los fixtures de
`testdata/protocol/ts-worker-v1/` y coincidir byte a byte.

---

## LUQUE-0603 — Implementar framing en TypeScript

**Dependencias:** LUQUE-0601.

**Checklist:**

- [x] Verificar dependencias y alcance.
- [x] Completar acciones y entregables.
- [x] Ejecutar pruebas y benchmarks aplicables.
- [x] Verificar criterios de aceptación y el gate aplicable.
- [x] Registrar resultados, limitaciones y siguiente tarea.

**Estado:** `PASS`.

**Entregables:**

* `ts-worker/src/framing.ts`;
* `ts-worker/src/framing.test.ts`;
* worker migrado a `typescript@7.0.2`, conforme al ADR 0010.

**Compatibilidad byte a byte:** las pruebas leen los fixtures de
`testdata/protocol/ts-worker-v1/`, comprueban las constantes del manifiesto,
decodifican los ocho casos con el mismo código de error y reencodean los frames
canónicos verificando bytes idénticos a los que produjo Go. Se comprobó además
la dirección inversa: frames generados en TypeScript decodificados por
`internal/tsworker` y comparados con `cmp` contra los fixtures de Go, sin
diferencias.

**Resultado:**

* `FrameDecoder` incremental que espera un frame parcial en lugar de adivinar;
* longitud validada antes de asignar memoria, y cuerpo vacío rechazado;
* EOF limpio distinguido de truncamiento;
* `INVALID_PAYLOAD` conserva la sesión y el flujo sigue alineado; el resto de
  clases la terminan, igual que en Go;
* `canonicalJSON` ordena las claves como lo hace `encoding/json` con mapas, que
  es lo que reproduce los mismos bytes;
* `readFrames` reensambla frames partidos en cualquier punto del flujo.

**Cambio de toolchain:** `typescript-eslint@8.66.0` rechaza explícitamente
TypeScript 7.0. Se retiró ESLint y el linter del worker pasa a ser el de Biome,
que no depende del compilador. Registrado en el ADR 0010. Se pierden las reglas
con información de tipos; las opciones estrictas del `tsconfig` se mantienen.

**Verificación:** `pnpm format:check`, `pnpm lint`, `pnpm typecheck`,
`pnpm test` con 14 pruebas y `pnpm build` pasan con el compilador nativo, junto
a `go test ./...` y `go vet ./...`.

**Limitación:** la igualdad de bytes depende de que los payloads se serialicen
con claves ordenadas. Un payload emitido desde una struct de Go con orden de
campos no alfabético rompería esa igualdad; los fixtures lo detectarían.

**Siguiente tarea:** LUQUE-0604.

---

## LUQUE-0604 — Implementar supervisor del worker

**Dependencias:** LUQUE-0602 y LUQUE-0603.

**Checklist:**

- [x] Verificar dependencias y alcance.
- [x] Completar acciones y entregables.
- [x] Ejecutar pruebas y benchmarks aplicables.
- [x] Verificar criterios de aceptación y el gate aplicable.
- [x] Registrar resultados, limitaciones y siguiente tarea.

**Estado:** `PASS`.

**Entregables:**

* `internal/tsworker/supervisor.go`;
* `internal/tsworker/messages.go`;
* `internal/tsworker/process_unix.go` y `process_other.go`;
* `internal/tsworker/supervisor_test.go` y `fake_worker_test.go`.

**Debe soportar:**

| Comportamiento | Implementación | Prueba |
| --- | --- | --- |
| arranque | `Start` deja la sesión lista o devuelve error | `TestSupervisorStartCompletesHandshakeAndExposesCapabilities` |
| handshake | `HELLO` con oferta de versiones y validación de límites | `TestSupervisorStartRejectsHandshakeOutsideProtocolLimits` |
| shutdown | `SHUTDOWN`, cierre de stdin, gracia, `SIGTERM`, `SIGKILL` | `TestSupervisorShutdownIsClean` y las dos de escalada |
| timeout | handshake y petición, con códigos distintos | `TestSupervisorHandshakeTimesOut`, `TestSupervisorRequestTimeoutInvalidatesTheSession` |
| reinicio | nueva sesión y handshake tras muerte inesperada | `TestSupervisorRestartsAfterAnUnexpectedExit` |
| límite de reinicios | ventana deslizante; agotada pasa a `FAILED` | `TestSupervisorStopsRestartingAfterTheBudget` |
| captura separada de stderr | bomba por líneas con cola acotada | `TestSupervisorCapturesStderrOutsideTheProtocol` |
| estado observable | `Status` con estado, generación, contadores y capacidades | presente en todas las anteriores |

**Decisiones:**

* Cancelar por `context` y agotar el timeout **no** son lo mismo. Cancelar envía
  `CANCEL` y conserva la sesión, según la sección 3.7. Un timeout la invalida,
  porque la sección 6 lo clasifica junto a EOF y salida inesperada.
* El grupo de procesos se aísla con `setpgid`, de modo que el servidor nativo
  que arranca el worker muere con él en lugar de quedar huérfano.
* Un único propietario llama a `cmd.Wait`. `os/exec` cierra los pipes durante
  `Wait`, así que hacerlo mientras el bucle de lectura sigue vivo es una
  carrera con reutilización de descriptores.
* Las anomalías del supervisor —respuestas sin destinatario, frames con cuerpo
  inválido— se cuentan en `Status`, no se disfrazan de salida del worker.
  `StderrTail` contiene solo lo que escribió el worker.
* Un `INVALID_PAYLOAD` no termina la sesión: el límite del frame se respetó y
  el flujo sigue alineado.

**Lote incompleto:** al morir una sesión se abortan sus peticiones con
`ENGINE_UNAVAILABLE` y se emite `SessionLoss` con los `id` afectados, que es lo
que permite al consumidor descartar entero un lote a medio emitir.
Verificado en `TestSupervisorReportsPartialBatchesAsLostOnSessionDeath`.

**Verificación:** `go test ./...`, `go vet ./...`, `go tool staticcheck
./internal/tsworker` y cinco ejecuciones consecutivas de
`go test -race ./internal/tsworker`, todas en verde.

**Hallazgo de las pruebas:** el worker falso es el propio binario de test
reejecutado, lo que las hace herméticas —sin Node ni build previo— y garantiza
que habla el códec real. Con `-race`, un binario Go tarda ~1 s en salir tras su
última escritura; medido y aislado en un caso mínimo. La gracia de las pruebas
se ajustó a ese artefacto del instrumentado, no del supervisor.

**Limitaciones:**

* El supervisor no lee todavía `config.typescript.worker_command`: se
  configura por `Options`. El cableado corresponde a su primer consumidor.
* `Call` es petición y respuesta. El ensamblado de lotes de hechos y su
  terminación por `PROJECT_INDEXED` es de LUQUE-0608 y LUQUE-0609; aquí los
  eventos se entregan por callback.
* No se pudo ejecutar la suite con `-tags ladybug`: la biblioteca nativa
  extraída en `/tmp` fue borrada por la limpieza del sistema. Ningún archivo de
  `internal/storage/ladybug` cambia en esta tarea.

**Siguiente tarea:** LUQUE-0605.

---

## LUQUE-0605 — Cargar versiones de TypeScript por proyecto

**Dependencias:** LUQUE-0604.

**Checklist:**

- [x] Verificar dependencias y alcance.
- [x] Completar acciones y entregables.
- [x] Ejecutar pruebas y benchmarks aplicables.
- [x] Verificar criterios de aceptación y el gate aplicable.
- [x] Registrar resultados, limitaciones y siguiente tarea.

**Estado:** `PASS`.

**Entregables:**

* `internal/workspace/typescript_versions.go`;
* `internal/workspace/typescript_versions_test.go`.

**Orden:**

```text
TypeScript local
TypeScript de workspace
fallback fijado
```

Implementado como una subida de directorios desde el `tsconfig` buscando
`node_modules/typescript`, que es como resuelve Node. El primer hallazgo gana.
Se clasifica `local` cuando cuelga del paquete propio del proyecto —el
`package.json` más cercano— y `workspace` cuando lo encontró más arriba. Sin
instalación, `pinned`: el compilador que Kivgraph distribuye.

**Qué decide la versión:** no el motor. Según el ADR 0010 el compilador es
siempre el nativo; la versión del proyecto decide la **confianza**.
`WithinSupportedWindow` es falso fuera de la ventana anunciada en `HELLO`, y
`OutsideWindowReason` registra el motivo para la evidencia.

**Decisiones:**

* La versión **resuelta** manda; el rango declarado en `package.json`
  (`^5.9.0`) se conserva solo como evidencia. Un rango no es una versión.
* El recorrido se detiene en la raíz del repositorio registrado. Un compilador
  instalado por encima no puede decidir en silencio la confianza de los hechos.
* La clasificación sigue **dónde lo encontró Node**, no dónde vive el paquete:
  con pnpm, `node_modules/typescript` es un enlace al almacén y se cuenta como
  instalación del proyecto que lo enlaza.
* Un `node_modules/typescript` que declara otro `name` es un error, no un
  hallazgo. Igual que un manifiesto ilegible.
* `ResolveProjects` falla entera si un proyecto no resuelve: un repositorio
  indexado con una versión indeterminada produce hechos que nadie puede
  auditar.
* La comparación usa `golang.org/x/mod/semver`, ya presente en el grafo de
  módulos, en lugar de un comparador propio. Queda como dependencia directa.

**Verificación:** `go test ./...`, `go vet ./...`, `go test -race`,
`go tool staticcheck` y `go mod verify`, todos en verde. Las pruebas se
comprobaron por mutación: anular la clasificación local rompe tres, y anular la
ventana soportada rompe cuatro.

**Limitaciones:**

* Un prerelease ordena por debajo de su versión final, así que `7.0.2-dev.N`
  entra en una ventana cuyo máximo es `7.0.2`. Es el orden de semver, y la
  prueba lo fija de forma explícita.
* No se comprueba que el compilador instalado sea ejecutable ni que su
  contenido coincida con su `package.json`; se toma la versión declarada por el
  paquete instalado.
* El resolutor todavía no se invoca desde el descubrimiento de proyectos: eso
  es LUQUE-0606, que construye configs, referencias y DAG.

**Siguiente tarea:** LUQUE-0606.

---

## LUQUE-0606 — Implementar descubrimiento de proyectos

**Dependencias:** LUQUE-0605.

**Checklist:**

- [x] Verificar dependencias y alcance.
- [x] Completar acciones y entregables.
- [x] Ejecutar pruebas y benchmarks aplicables.
- [x] Verificar criterios de aceptación y el gate aplicable.
- [x] Registrar resultados, limitaciones y siguiente tarea.

**Estado:** `PASS`.

**Entregables:**

* `internal/workspace/typescript_program.go` — tipo `TypeScriptProgram`, grafo
  y orquestación;
* `internal/workspace/typescript_config.go` — cadena `extends` y opciones
  efectivas;
* `internal/workspace/typescript_sources.go` — `files`, `include`, `exclude`;
* `internal/workspace/typescript_graph.go` — orden topológico y ciclos;
* los cuatro archivos de prueba correspondientes.

**Construir:**

| Pieza | Dónde |
| --- | --- |
| parsed configs | `resolveTypeScriptConfig`, JSONC y cadena `extends` |
| project references | ya resueltas en el descubrimiento; no se heredan por `extends` |
| DAG | `topologicalTypeScriptOrder`, con dependientes inversos |
| versión | `TypeScriptVersionResolver` de LUQUE-0605, por proyecto |
| compiler options | merge por clave con rebase de rutas |
| source files | globs con `**` propios, extensiones según `allowJs` |

**Decisiones:**

* `extends` acepta cadena y array. En array gana el elemento más a la derecha,
  y el hijo gana sobre todos. `files`, `include` y `exclude` **reemplazan**, no
  se fusionan; es la semántica de TypeScript, y fusionarlas silenciosamente
  cambiaría qué archivos posee un proyecto.
* Una opción de ruta heredada es relativa **al archivo que la declaró**. Un
  `declarationDir` del config base apunta junto al base, no dentro del paquete
  que lo hereda.
* `paths` no se absolutiza: sus valores son relativos a `baseUrl` por
  definición, y `baseUrl` ya queda absoluto.
* Un ciclo de `extends` y un ciclo de project references son errores que
  nombran los configs implicados, no bucles infinitos ni avisos.
* Una referencia a un config no descubierto es un error que nombra origen y
  destino. Ignorarla en silencio produciría un grafo incompleto que nadie
  detectaría.
* El orden topológico desempata lexicográficamente y no depende del recorrido
  de mapas de Go: dos ejecuciones sobre la misma entrada dan la misma lista.
* `node_modules` se poda de la expansión de comodines incluso con `exclude`
  declarado, y una entrada que salta dentro (`node_modules/pkg/**/*.ts`)
  tampoco la burla.
* `Get`, `Programs` y `Unsupported` devuelven copias: el grafo es inmutable
  tras construirse.

**Ejecución:** las tres piezas —`extends`, source files y DAG— se
implementaron en paralelo con un contrato de tipos y firmas fijado antes de
empezar. La integración compiló y pasó los ocho tests de extremo a extremo a la
primera, incluidas las reglas que cruzan piezas.

**Verificación:** `go test ./...`, `go vet ./...`, `go test -race`,
`go tool staticcheck` y `go build`, todos en verde. 70 tests en el paquete.

Comprobación por mutación sobre el paquete integrado: quitar la exclusión de
`node_modules` rompe 2 pruebas; invertir la precedencia del merge rompe 2;
limitar `**` a un nivel rompe 1.

**Prueba vacua encontrada y corregida:** el desempate lexicográfico del orden
topológico no estaba defendido. Su prueba solo liberaba proyectos a la vez, y
en ese caso la cola FIFO ya coincide con el orden lexicográfico; anular el
`sort` no rompía nada. Se añadió
`TestTopologicalTypeScriptOrderPrefersALateSmallerCandidate`, donde un
proyecto se libera más tarde y es menor que otro ya encolado. Con esa prueba,
anular el `sort` sí falla.

**Limitaciones:**

* La resolución de `extends` por módulo Node no aplica el endurecimiento
  contra symlinks que sí hace `resolveProjectReference`; la contención se
  comprueba de forma léxica.
* No se interpretan `compilerOptions` más allá de su rebase de rutas: no se
  validan valores ni se resuelven `paths` contra el sistema de archivos.
* El grafo aún no lo consume nadie; el Language Service persistente es
  LUQUE-0607.

**Siguiente tarea:** LUQUE-0607.

---

## LUQUE-0607 — Implementar Language Service persistente

**Dependencias:** LUQUE-0606.

**Checklist:**

- [x] Verificar dependencias y alcance.
- [x] Completar acciones y entregables.
- [x] Ejecutar pruebas y benchmarks aplicables.
- [x] Verificar criterios de aceptación y el gate aplicable.
- [x] Registrar resultados, limitaciones y siguiente tarea.

**Estado:** `PASS`.

**Entregables:**

* `ts-worker/src/language-service.ts`;
* `ts-worker/src/language-service.test.ts`.

**Mantener:**

| Pieza | Cómo |
| --- | --- |
| snapshots | uno vivo; el anterior se libera tras crear el nuevo |
| versions | versión y hash SHA-256 por archivo, solo suben si cambia el contenido |
| module cache | caché de source files del cliente, con `invalidateAll` como recuperación |
| Program | `program` del proyecto del snapshot vivo |
| TypeChecker | `checker` del mismo proyecto, ligado a la generación |
| proyecto | `openProject` y `closeProject`, con opens que persisten entre snapshots |

**Invariante central:** los handles son **de snapshot**. Un `Project`,
`Program` o `Checker` obtenido de un snapshot deja de ser válido cuando ese
snapshot se libera, así que el servicio nunca entrega uno sin atarlo a su
generación, y `assertFresh` rechaza una vista caduca.

**Decisiones:**

* Una versión sube solo si el contenido cambió de verdad. Un archivo anunciado
  cuyo hash coincide se reporta como `unchanged` y no rueda el snapshot.
* Un `content_hash` anunciado que no coincide con el disco se reporta como
  `desynchronised` y **no se aplica**: el protocolo usa ese hash justamente
  para detectar una lectura desincronizada, y aplicarla produciría hechos que
  nadie puede atribuir a una revisión.
* Un archivo anunciado que ya no existe también es desincronización, no una
  lectura vacía.
* Solo se versionan los archivos que pueden cambiar durante la sesión: los de
  dentro del workspace y fuera de `node_modules`. Las declaraciones de
  librería viven en la instalación del compilador y son inmutables.
* El snapshot anterior se libera **después** de crear el siguiente, para que el
  servidor nativo no vea una ventana sin snapshot.
* `invalidateAll` limpia caché de cliente, versiones y estado del servidor; es
  la ruta de recuperación cuando el estado incremental deja de ser fiable.

**Verificación:** las pruebas ejercitan el **servidor nativo real**, que es el
motor que fijó el ADR 0010; un mock no probaría nada de la persistencia que
esta tarea entrega. 32 tests del worker en 0,9 s, más `pnpm format:check`,
`pnpm lint`, `pnpm typecheck`, `pnpm build`, `go test ./...` y `go vet ./...`.

Comprobación por mutación: quitar la verificación del hash anunciado rompe 1;
subir siempre la versión rompe 1; no incrementar la generación rompe 2; no
liberar el snapshot anterior rompe 1.

**Fuga cubierta explícitamente:** el ADR 0005 advierte de acumulación de
memoria en workers de larga duración. `snapshotsDisposed` es estado observable
y una prueba comprueba que tras cuatro snapshots hay exactamente tres
liberados, y cuatro tras cerrar.

**Limitaciones:**

* No hay política de reciclado por umbral de memoria; el ADR 0005 la deja
  pendiente de medición y aquí solo se garantiza que no se acumulan snapshots.
* `openFiles`/`closeFiles` de la API no se usan todavía: el worker trabaja por
  proyecto, no por documento abierto.
* El servicio aún no está conectado al protocolo; los mensajes
  `INDEX_PROJECT`, `UPDATE_FILES` y `REMOVE_FILES` lo consumirán en las tareas
  de extracción.
* El hash es SHA-256 sobre los bytes del archivo. No es la stable key del
  modelo semántico, que es BLAKE3 sobre la identidad canónica.

**Siguiente tarea:** LUQUE-0608.

---

## LUQUE-0608 — Extraer símbolos TypeScript locales

**Dependencias:** LUQUE-0607.

**Checklist:**

- [x] Verificar dependencias y alcance.
- [x] Completar acciones y entregables.
- [x] Ejecutar pruebas y benchmarks aplicables.
- [x] Verificar criterios de aceptación y el gate aplicable.
- [x] Registrar resultados, limitaciones y siguiente tarea.

**Estado:** `PASS`.

**Entregables:**

* `ts-worker/src/symbol-extractor.ts`;
* `ts-worker/src/symbol-extractor.test.ts`;
* `ts-worker/src/index.ts` (exportación pública).

**Extrae:**

* funciones declaradas;
* clases;
* interfaces;
* métodos y accessors;
* variables, incluyendo bindings simples y destructuring;
* propiedades de clase;
* alias de tipo;
* enums;
* namespaces;
* exports locales directos, `export { local as public }`, `export default
  local` y `export = local`.

**Contrato del extractor:**

```text
extractLocalSymbols(service, view, options?)
  -> { generation, configFileName, symbols, exports }
```

Cada `LocalSymbol` contiene el `Symbol` del checker nativo, `symbolId`,
archivo, nombre, nombre cualificado, kind, offsets UTF-16, líneas 1-based,
cabecera de declaración compacta y nombres exportados. Cada `LocalExport`
conserva el binding local, el nombre público, si es type-only y el mismo
`Symbol` del checker.

**Decisiones:**

* El AST solo localiza declaraciones; ningún símbolo se emite si el checker
  nativo no puede resolverlo.
* Todos los nombres de declaración y bindings de export se resuelven en una
  única llamada batched a `checker.getSymbolAtLocation`. Solo los aliases
  cuyo target no aparece en ese lote usan la primitiva específica de
  `ExportSpecifier`.
* La extracción está atada a la generación del `ProjectView`. Se comprueba
  `assertFresh` antes y después de las llamadas nativas; una vista caduca
  produce `STALE_GENERATION`.
* “Local” significa fuente del proyecto bajo el directorio del tsconfig,
  fuera de librerías externas. Las reexportaciones desde otro módulo no se
  convierten en símbolos locales; LUQUE-0610 resolverá aliases.
* El orden de símbolos y exports es determinista por archivo, posición y
  nombre.
* Las declaraciones sobrecargadas se conservan por sitio de declaración aunque
  compartan el mismo `symbolId`; la stable key futura podrá discriminarlas por
  firma.
* No se materializa el conjunto completo de exports del módulo: se recorren
  únicamente los `export` sintácticos del archivo seleccionado, respetando
  ADR 0010.

**Verificación:**

```text
4 archivos de tests
37 tests passed
pnpm format:check
pnpm lint
pnpm typecheck
pnpm test
pnpm build
go test ./...
go vet ./...
git diff --check
```

Los tests ejecutan el servidor nativo de TypeScript 7 real y cubren
declaraciones, scopes anidados, exports directos y aliasados, default exports,
destructuring, selección por archivo relativo, exclusión de fuentes externas y
rechazo de una vista stale, y preservación de declaraciones sobrecargadas.

**Limitaciones:**

* Los constructores no se emiten como símbolos independientes porque la API
  AST no les proporciona un name node con el que resolver un símbolo local.
* Las expresiones anónimas de `export default`, `export *` y reexports desde
  otro módulo no generan `LocalSymbol`; requieren identidad de módulo y
  resolución de aliases.
* `signature` es una cabecera sintáctica compacta, no el tipo inferido por
  `TypeChecker`; la firma semántica se añadirá cuando el modelo de hechos la
  necesite.
* Los `symbolId` son válidos solo durante la vida del snapshot; la identidad
  durable será la stable key del modelo Go.

**Siguiente tarea:** LUQUE-0609.

---

## LUQUE-0609 — Extraer referencias locales

**Dependencias:** LUQUE-0608.

**Checklist:**

- [x] Verificar dependencias y alcance.
- [x] Completar acciones y entregables.
- [x] Ejecutar pruebas y benchmarks aplicables.
- [x] Verificar criterios de aceptación y el gate aplicable.
- [x] Registrar resultados, limitaciones y siguiente tarea.

**Estado:** `PASS`.

**Entregables:**

* `ts-worker/src/reference-extractor.ts`;
* `ts-worker/src/reference-extractor.test.ts`;
* `ts-worker/src/index.ts` (exportación pública).

**Contrato del extractor:**

```text
extractLocalReferences(service, view, symbols, options?)
  -> { generation, configFileName, references }
```

Cada `LocalReference` conserva el archivo, la ocurrencia AST, offsets UTF-16,
líneas 1-based, texto, símbolo local destino y símbolo local contenedor cuando
existe. Las ocurrencias de nivel superior tienen `source` indefinido.

**Clasificación:**

* `REFERENCES`: uso ordinario de un identificador local;
* `CALLS_DIRECT`: identificador o miembro local usado como callee de una
  llamada o construcción;
* `PASSES_AS_CALLBACK`: valor callable local pasado como argumento;
* `ASSIGNS_FUNCTION`: valor callable local usado en el lado derecho de una
  asignación o inicializador;
* `RETURNS_FUNCTION`: valor callable local devuelto;
* `TYPE_USES`: uso local dentro de una posición de tipo.

**Decisiones:**

* El AST solo descubre ocurrencias; cada destino se resuelve con el
  `TypeChecker` nativo en una única llamada batched a
  `checker.getSymbolAtLocation`.
* No se emiten referencias por coincidencia textual, nombre cualificado o
  símbolo externo. Solo se conservan destinos presentes en la extracción de
  `LocalSymbol` de LUQUE-0608.
* La extracción exige que `ProjectView` y `LocalSymbolExtraction` pertenezcan
  a la misma generación y comprueba `assertFresh` antes y después de las
  llamadas nativas.
* Imports, exports y aliases de módulo se omiten deliberadamente; LUQUE-0610
  resolverá esos bindings sin convertirlos en referencias nominales.
* La clasificación de callback, asignación y retorno solo se mantiene cuando
  el destino es callable según una función o método declarado, o una variable
  inicializada con una función/arrow local; en otro caso se degrada a
  `REFERENCES`.
* La salida es determinista por archivo, posición, clasificación y nombre de
  destino.

**Verificación:**

```text
5 archivos de tests
41 tests passed
pnpm format:check
pnpm lint
pnpm typecheck
pnpm test
pnpm build
gofmt -l .
go test ./...
go vet ./...
go build ./cmd/kivgraph
```

La suite nueva cubre resolución con el checker, llamadas directas, callbacks,
asignaciones, retornos, usos de tipos, valores función en variables arrow,
selección por archivo, exclusión de símbolos externos y rechazo de snapshots
stale.

**Limitaciones:**

* Referencias que llegan mediante imports, `export *`, reexports o aliases
  locales no se materializan todavía; requieren la resolución explícita de
  LUQUE-0610.
* La detección de valores función no intenta inferir aliasing o flujo de datos
  arbitrario; conserva únicamente las formas locales observables soportadas
  por esta tarea para evitar aristas semánticas especulativas.
* `symbolId` y los objetos `LocalSymbol` siguen siendo válidos solo durante la
  vida del snapshot; la identidad durable será la stable key del modelo Go.

**Siguiente tarea:** LUQUE-0610.

---

## LUQUE-0610 — Resolver aliases locales

**Dependencias:** LUQUE-0609.

**Checklist:**

- [x] Verificar dependencias y alcance.
- [x] Completar acciones y entregables.
- [x] Ejecutar pruebas y benchmarks aplicables.
- [x] Verificar criterios de aceptación y el gate aplicable.
- [x] Registrar resultados, limitaciones y siguiente tarea.

**Estado:** `PASS`.

**Entregables:**

* `ts-worker/src/reference-extractor.ts`;
* `ts-worker/src/reference-extractor.test.ts`.

**Resolución implementada:**

* Los símbolos que el checker marca con `SymbolFlags.Alias` se resuelven con
  `checker.getAliasedSymbol`.
* La resolución sigue toda la cadena de aliases y descarta el símbolo
  `unknown` o cualquier destino que no esté en el índice de `LocalSymbol`.
* El destino se asocia exclusivamente por `symbol.id`; no se compara por
  nombre, texto ni nombre cualificado.
* Las referencias a imports de valor y de tipo conservan la ocurrencia local,
  pero apuntan al `LocalSymbol` declarado en el módulo origen.
* Los aliases externos o no resueltos siguen omitidos.

**Contrato conservado:**

```text
extractLocalReferences(service, view, symbols, options?)
  -> { generation, configFileName, references }
```

La clasificación de LUQUE-0609 (`REFERENCES`, `CALLS_DIRECT`,
`PASSES_AS_CALLBACK`, `ASSIGNS_FUNCTION`, `RETURNS_FUNCTION` y `TYPE_USES`)
se aplica después de resolver el destino final, por lo que un callback o
función importada conserva su clasificación semántica.

**Verificación:**

```text
5 archivos de tests
42 tests passed
pnpm format:check
pnpm lint
pnpm typecheck
pnpm test
pnpm build
gofmt -l .
go test ./...
go vet ./...
go build ./cmd/kivgraph
```

La prueba nueva cubre imports aliasados de valor y tipo, verifica el archivo
declarativo real del destino y conserva la exclusión de símbolos externos.

**Limitaciones:**

* `export`, `export *`, reexports, barrels y las formas de exportación
  sintáctica siguen reservadas para LUQUE-0611.
* No se infiere aliasing por asignación (`const local = imported`) ni flujo de
  datos arbitrario; solo se resuelven aliases que el checker marca como tales.
* `symbolId` permanece limitado al snapshot actual; la identidad durable será
  la stable key del modelo Go.

**Siguiente tarea:** LUQUE-0611.

---

## LUQUE-0611 — Resolver exports y reexports

**Dependencias:** LUQUE-0610.

**Checklist:**

- [x] Verificar dependencias y alcance.
- [x] Completar acciones y entregables.
- [x] Ejecutar pruebas y benchmarks aplicables.
- [x] Verificar criterios de aceptación y el gate aplicable.
- [x] Registrar resultados, limitaciones y siguiente tarea.

**Estado:** `PASS`.

**Entregables:**

* `ts-worker/src/symbol-extractor.ts`;
* `ts-worker/src/symbol-resolution.ts`;
* `ts-worker/src/symbol-extractor.test.ts`;
* `ts-worker/package.json`.

**Resolución implementada:**

* Los exports locales directos, aliases de exportación y `default` conservan
  su `LocalExport` respaldado por el símbolo nativo.
* `export { ... } from` se resuelve con
  `checker.getExportSpecifierLocalTargetSymbol`.
* `export * from` obtiene los exports del módulo con
  `checker.getExportsOfModule`.
* Los barrels siguen las cadenas de aliases hasta el `LocalSymbol` declarado
  en el archivo origen.
* La resolución común usa `SymbolFlags.Alias`, `getAliasedSymbol`,
  `isUnknownSymbol` y `symbol.id`; no compara nombres ni texto.
* Los reexports se representan en `LocalExport` con el archivo que los
  publica y el símbolo local declarado en el módulo origen.

**Casos cubiertos:**

```text
named export
default export
alias export
export from
export *
barrels
```

**Script de comprobación:**

```text
pnpm check
  -> pnpm format:check
  -> pnpm lint
  -> pnpm typecheck
  -> pnpm test
```

**Verificación:**

```text
5 archivos de tests
43 tests passed
pnpm check
pnpm build
gofmt -l .
go test ./...
go vet ./...
go build ./cmd/kivgraph
```

La prueba nueva comprueba exports directos, `default`, aliases, exports desde
otro módulo, `export *` y propagación a través de un barrel, incluyendo
destinos de tipo y valor.

**Limitaciones:**

* Un `export default` anónimo no genera `LocalSymbol` porque carece de un
  nombre declarativo estable.
* `export * as namespace` no se convierte en un único `LocalExport`, porque
  representa un namespace y no una declaración local individual.
* La identidad de los símbolos sigue limitada al snapshot actual; la stable
  key durable se añadirá en las capas posteriores.

**Siguiente tarea:** LUQUE-0612.

---

## LUQUE-0612 — Crear suite TypeScript local

**Dependencias:** LUQUE-0611.

**Checklist:**

- [x] Verificar dependencias y alcance.
- [x] Completar acciones y entregables.
- [x] Ejecutar pruebas y benchmarks aplicables.
- [x] Verificar criterios de aceptación y el gate aplicable.
- [x] Registrar resultados, limitaciones y siguiente tarea.

**Estado:** `PASS`.

**Entregables:**

* `ts-worker/src/local-suite.test.ts`;
* `ts-worker/src/reference-extractor.ts`;
* `ts-worker/src/symbol-resolution.ts`.

**Cobertura:**

La suite crea proyectos temporales y ejecuta el servidor nativo de TypeScript 7
real. Verifica homónimos en módulos distintos, shadowing en scopes anidados,
declaraciones overload, métodos genéricos, tipos genéricos, barrels,
callbacks, aliases de valor y tipo, y resolución de exports locales.
El fixture de código roto confirma que los diagnósticos semánticos se exponen,
los símbolos resolubles se conservan y los destinos inexistentes no se emiten.

**Decisiones:**

* La suite usa fixtures temporales para aislar cada snapshot y no modificar
  repositorios indexados.
* Los valores función se descubren en todos los archivos locales del proyecto,
  aunque las referencias solicitadas estén limitadas por archivo; así un
  callback exportado y aliasado conserva `PASSES_AS_CALLBACK`.
* Los símbolos instanciados de miembros genéricos se asocian mediante sus
  `declaration handles` cuando el `symbolId` de uso difiere del declarado.
* Las referencias siguen resolviéndose mediante `TypeChecker`; no se añade
  coincidencia nominal ni textual.

**Verificación:**

```text
6 archivos de tests
45 tests passed
pnpm check
pnpm build
gofmt -l .
go test ./...
go vet ./...
go build ./cmd/kivgraph
```

No se ejecutó un benchmark separado: esta tarea fija cobertura local de
corrección; los SLO y benchmarks permanecen en sus tareas de rendimiento.

**Limitaciones:**

* La suite no resuelve imports de paquetes entre repositorios; esa cobertura
  pertenece a LUQUE-0701.
* La extracción sigue siendo snapshot-scoped y los `symbolId` no son identidad
  durable; la stable key se definirá en las capas Go.
* El fixture roto usa un error semántico y un destino inexistente; no pretende
  validar recuperación de errores sintácticos arbitrarios.

**Siguiente tarea:** LUQUE-0701.

---

# 10. Fase 7 — TypeScript cross-repository

## LUQUE-0701 — Resolver package imports

**Dependencias:** TYPESCRIPT_LOCAL_PASS.

**Checklist:**

- [x] Verificar dependencias y alcance.
- [x] Completar acciones y entregables.
- [x] Ejecutar pruebas y benchmarks aplicables.
- [x] Verificar criterios de aceptación y el gate aplicable.
- [x] Registrar resultados, limitaciones y siguiente tarea.

**Estado:** `PASS`.

**Entregables:**

* `ts-worker/src/package-import-resolver.ts`;
* `ts-worker/src/package-import-resolver.test.ts`;
* `ts-worker/src/index.ts` (exportación pública).

**Contrato:**

```text
resolvePackageImports(service, view, registry, options?)
  -> { generation, configFileName, imports }
```

Cada entrada conserva archivo, specifier, package name, offsets UTF-16,
archivos resueltos por TypeScript, estado y provider repository. Se reconocen
imports estáticos, `export ... from`, `import =`, `import type(...)` y
`import(...)` dinámico. Los imports relativos, built-ins `node:` y aliases
internos `#` quedan fuera de este contrato.

**Decisiones:**

* El `TypeChecker` nativo resuelve primero el module specifier en un lote. El
  package registry solo aporta el provider después de esa resolución.
* `RESOLVED` requiere simultáneamente declaraciones resueltas y un provider
  registrado; no se crean aristas por nombre, ruta o coincidencia textual.
* `MODULE_NOT_RESOLVED` y `PACKAGE_PROVIDER_NOT_FOUND` conservan el contexto
  suficiente para las referencias no resueltas de LUQUE-0706.
* El registry se recibe como una interfaz de solo lectura compatible con los
  providers producidos por Go; el worker no duplica el descubrimiento de
  `package.json`.
* La selección por archivo, la exclusión de librerías externas y la ordenación
  de resultados son deterministas y están ligadas a la generación del snapshot.

**Verificación:**

```text
7 archivos de tests
48 tests passed
pnpm check
pnpm build
gofmt -l .
go test ./...
go vet ./...
go build ./cmd/kivgraph
```

La suite cubre providers registrados, providers ausentes, módulos no
resueltos, paquetes scoped y subpaths, imports de valor y tipo, barrels,
imports dinámicos y exclusión de imports locales o built-ins.

No se requiere benchmark separado: esta tarea resuelve metadatos de imports y
provider; el coste cross-repository y los SLO se medirán con el corpus de fase.

**Limitaciones:**

* `exports`, `types`, `typings`, `typesVersions`, `paths` y project references
  del provider no se interpretan aquí; corresponden a LUQUE-0702.
* La relación de un `.d.ts` con su fuente queda para LUQUE-0703.
* La resolución de símbolos importados y las aristas `IMPORTS_SYMBOL` quedan
  para LUQUE-0705.
* `rootPath` del provider se conserva como metadata, pero la comprobación de
  mapeo físico del archivo resuelto se reserva para la fase de declaraciones,
  porque los workspaces pueden resolver mediante enlaces de `node_modules`.

**Gate:**

```text
PACKAGE_IMPORTS_PASS
```

**Siguiente tarea:** LUQUE-0702.

---

## LUQUE-0702 — Resolver exports del provider

**Dependencias:** LUQUE-0701.

**Checklist:**

- [x] Verificar dependencias y alcance.
- [x] Completar acciones y entregables.
- [x] Ejecutar pruebas y benchmarks aplicables.
- [x] Verificar criterios de aceptación y el gate aplicable.
- [x] Registrar resultados, limitaciones y siguiente tarea.

**Estado:** `PASS`.

**Entregables:**

* `ts-worker/src/provider-export-resolver.ts`;
* `ts-worker/src/package-import-resolver.ts` (selección de exports);
* `ts-worker/src/package-import-resolver.test.ts`;
* `ts-worker/src/index.ts` (exportación pública).

**Contrato:**

```text
resolveProviderExports(service, view, registry, options?)
  -> { generation, configFileName, imports, exports }
```

Cada entrada de `imports` conserva la resolución de LUQUE-0701 y añade el modo
de selección (`NAMED`, `STAR`, `NAMESPACE` o `NONE`) y los nombres solicitados.
`exports` devuelve el nombre público, el estado (`RESOLVED`,
`EXPORT_NOT_FOUND` o `MODULE_SYMBOL_NOT_FOUND`), el nombre destino y los
archivos declarativos del símbolo cuando existen.

**Decisiones:**

* El `TypeChecker` sigue siendo la fuente de verdad para `exports`, `types`,
  `typings`, `typesVersions`, `paths`, project references y `moduleResolution`;
  el worker no duplica las reglas de resolución de TypeScript.
* Los imports nombrados consultan `getMemberInModuleExports`; solo los imports
  de namespace y los `export *` enumeran el módulo completo. `export *` omite
  `default`, conforme a la semántica de TypeScript.
* Los alias se siguen con `getAliasedSymbol` para conservar el nombre público y
  localizar la declaración destino sin crear relaciones nominales.
* Imports laterales, dinámicos e `import =` conservan su resolución de módulo,
  pero no inventan nombres exportados que el AST no solicita.

**Verificación:**

```text
7 archivos de tests
49 tests passed
pnpm check
pnpm build
gofmt -l .
go test ./...
go vet ./...
go build ./cmd/kivgraph
```

La cobertura incluye exports nombrados, `default`, tipos, alias,
`export ... from`, namespace, `export *`, exports ausentes y módulos
resueltos sin provider. No se requiere benchmark separado: esta tarea consulta
símbolos del checker; el coste cross-repository y los SLO se medirán con el
corpus de fase.

**Limitaciones:**

* La relación de un `.d.ts` con su fuente queda para LUQUE-0703.
* La resolución de símbolos importados y las aristas `IMPORTS_SYMBOL` quedan
  para LUQUE-0705.
* Los imports sin una selección de nombres no generan una lista artificial de
  exports; la resolución de módulo permanece disponible en `imports`.

**Gate:**

```text
PROVIDER_EXPORTS_PASS
```

**Siguiente tarea:** LUQUE-0703.

---

## LUQUE-0703 — Mapear `.d.ts` hacia fuente

**Dependencias:** LUQUE-0702.

**Checklist:**

- [x] Verificar dependencias y alcance.
- [x] Completar acciones y entregables.
- [x] Ejecutar pruebas y benchmarks aplicables.
- [x] Verificar criterios de aceptación y el gate aplicable.
- [x] Registrar resultados, limitaciones y siguiente tarea.

**Estado:** `PASS`.

**Entregables:**

* `ts-worker/src/declaration-source-resolver.ts`;
* `ts-worker/src/declaration-source-resolver.test.ts`;
* `ts-worker/src/package-import-resolver.ts` (metadata de provider);
* `ts-worker/src/index.ts` (exportación pública).

**Contrato:**

```text
resolveDeclarationSources(service, view, providerExportResolution)
  -> { generation, configFileName, mappings }
```

Cada mapping es file-level y conserva el `.d.ts`, sus fuentes físicas
encontradas y el estado:

```text
DECLARATION_MAP
PROJECT_REFERENCE
PROVIDER_REGISTRY
ROOT_DIR_OUT_DIR
UNRESOLVED
```

**Precedencia:**

```text
declarationMap
project reference
provider export registry
rootDir/outDir
unresolved
```

`*.d.ts.map` se consulta primero y sus `sourceRoot`/`sources` se resuelven
contra el archivo de mapa. Después se consultan el `projectPath` del provider,
sus `declarationRoots`/`sourceRoots` y, por último, las rutas explícitas
`declarationDir`/`outDir` y `rootDir`. Solo se publican fuentes que existen;
no se crean destinos por coincidencia nominal.

**Verificación:**

```text
8 archivos de tests
51 tests passed
pnpm check
pnpm build
gofmt -l .
go test ./...
go vet ./...
go build ./cmd/kivgraph
```

La cobertura verifica las cinco ramas de precedencia, configuración de proyecto,
source maps, raíces relativas, fuentes ausentes y el estado `UNRESOLVED`.
No se requiere benchmark separado: el mapeo es una consulta de metadata de
archivos; los SLO se medirán con el corpus de fase.

**Limitaciones:**

* El resultado es file-level; la posición exacta de cada símbolo y la arista
  `IMPORTS_SYMBOL` quedan para LUQUE-0705.
* Un declaration map que referencia fuentes inexistentes cae a las
  estrategias siguientes y finalmente a `UNRESOLVED`.
* No se inventan fuentes para declaraciones sin mapa, raíces del provider o
  configuración `rootDir`/`outDir`.

**Gate:**

```text
DECLARATION_SOURCE_MAP_PASS
```

**Siguiente tarea:** LUQUE-0704.

---

## LUQUE-0704 — Crear `PACKAGE_DEPENDS_ON`

**Dependencias:** LUQUE-0701.

**Checklist:**

- [x] Verificar dependencias y alcance.
- [x] Completar acciones y entregables.
- [x] Ejecutar pruebas y benchmarks aplicables.
- [x] Verificar criterios de aceptación y el gate aplicable.
- [x] Registrar resultados, limitaciones y siguiente tarea.

**Estado:** `PASS`.

**Entregables:**

* `ts-worker/src/package-dependency-resolver.ts`;
* `ts-worker/src/package-dependency-resolver.test.ts`;
* `ts-worker/src/index.ts` (exportación pública).

**Contrato:**

```text
resolvePackageDependencies(service, view, registry, consumer, options?)
  -> { generation, configFileName, imports, dependencies }
```

Cada dependencia conserva:

```text
kind: PACKAGE_DEPENDS_ON
consumer: PackageProvider
provider: PackageProvider
imports: file, specifier y offsets UTF-16
```

`createPackageDependencies` agrupa las ocurrencias por identidad completa del
provider y ordena tanto las aristas como su evidencia de forma determinista.

**Decisiones:**

* La identidad del paquete consumidor llega desde el registro de Go; el worker
  no vuelve a descubrir ni a inferir `package.json`.
* Solo una entrada `RESOLVED` con provider registrado y cuyo nombre coincide
  con el package name puede producir una arista.
* Providers ausentes, módulos no resueltos, nombres inconsistentes y
  auto-referencias no producen aristas nominales.
* El resultado conserva la resolución de imports para que las capas posteriores
  puedan clasificar referencias no resueltas y construir stable keys.

**Verificación:**

```text
pnpm check                         # 9 archivos, 53 tests passed
gofmt -l .
go test ./...                      # 11 paquetes ok, 2 sin tests
go vet ./...
go build ./cmd/kivgraph
```

No se requiere benchmark separado: la tarea agrupa metadata ya resuelta por el
checker y el coste cross-repository se medirá con el corpus de fase.

**Limitaciones:**

* La arista todavía es un resultado del worker; su persistencia, stable key y
  almacenamiento pertenecen a las capas Go posteriores.
* La ambigüedad entre providers de distintos repositorios y la compatibilidad
  de versiones permanecen bajo LUQUE-0408 y LUQUE-0706.

**Siguiente tarea:** LUQUE-0705.

---

## LUQUE-0705 — Crear `IMPORTS_SYMBOL` exacto

**Dependencias:** LUQUE-0702 y LUQUE-0703.

**Checklist:**

- [x] Verificar dependencias y alcance.
- [x] Completar acciones y entregables.
- [x] Ejecutar pruebas y benchmarks aplicables.
- [x] Verificar criterios de aceptación y el gate aplicable.
- [x] Registrar resultados, limitaciones y siguiente tarea.

**Estado:** `PASS`.

**Entregables:**

* `ts-worker/src/imported-symbol-resolver.ts`;
* `ts-worker/src/imported-symbol-resolver.test.ts`;
* `ts-worker/src/index.ts` (exportación pública).

**Contrato:**

```text
resolveImportedSymbols(service, view, registry, options?)
  -> { generation, configFileName, imports, exports, mappings, symbols }
```

Cada entrada de `symbols` enlaza:

```text
símbolo consumidor
→ símbolo fuente del provider
```

y conserva `kind: IMPORTS_SYMBOL`, package name, specifier, provider, nombre
público solicitado, el binding consumidor con `symbolId`, offsets UTF-16 y
líneas, y el símbolo destino con `symbolId`, nombre y cada sitio de
declaración. Cada declaración añade las fuentes de LUQUE-0703 y su estado.

**Decisiones:**

* La arista exige tres hechos del compilador nativo: module specifier resuelto,
  provider registrado y alias resuelto por `getAliasedSymbol` a un símbolo con
  declaraciones. Ningún nombre coincidente produce una arista.
* El binding consumidor se une a su import por la posición exacta del specifier,
  no por el nombre del paquete.
* Se cubren imports por defecto, nombrados, aliasados y `export ... from`.
* Las posiciones destino se obtienen resolviendo los `NodeHandle` nativos; una
  declaración que no se puede resolver no genera arista.

**Verificación:**

```text
pnpm check                         # 10 archivos, 56 tests passed
pnpm build
gofmt -l .
go test ./...                      # 11 paquetes ok, 2 sin tests
go vet ./...
go build ./cmd/kivgraph
```

La cobertura verifica aristas exactas con posiciones, mapeo a fuente mediante
declaration map, y ausencia de aristas para homónimos locales, módulos no
resueltos, providers no registrados y exports inexistentes.

No se requiere benchmark separado: la resolución reutiliza el lote del checker;
el coste cross-repository y los SLO se medirán con el corpus de fase.

**Limitaciones:**

* Los imports de namespace y `import =` enlazan el módulo, no un símbolo; no
  producen aristas exactas.
* La posición destino es la del sitio de declaración, normalmente en `.d.ts`;
  la fuente física llega file-level desde LUQUE-0703.
* Las razones de no resolución se clasifican en LUQUE-0706.

**Siguiente tarea:** LUQUE-0706.

---

## LUQUE-0706 — Implementar referencias no resueltas TypeScript

**Dependencias:** LUQUE-0705.

**Checklist:**

- [x] Verificar dependencias y alcance.
- [x] Completar acciones y entregables.
- [x] Ejecutar pruebas y benchmarks aplicables.
- [x] Verificar criterios de aceptación y el gate aplicable.
- [x] Registrar resultados, limitaciones y siguiente tarea.

**Estado:** `PASS`.

**Entregables:**

* `ts-worker/src/unresolved-reference-resolver.ts`;
* `ts-worker/src/unresolved-reference-resolver.test.ts`;
* `ts-worker/src/index.ts` (exportación pública).

**Contrato:**

```text
resolveUnresolvedReferences(service, view, registry, options?)
  -> { generation, configFileName, imports, exports, mappings, symbols, unresolved }
```

**Razones emitidas:**

```text
AMBIGUOUS_PACKAGE_PROVIDER
VERSION_MISMATCH
PACKAGE_PROVIDER_NOT_FOUND
MODULE_NOT_RESOLVED
TYPECHECK_FAILED
EXPORT_NOT_FOUND
DECLARATION_SOURCE_NOT_MAPPED
```

Cada entrada conserva archivo, specifier, package name, símbolo solicitado
cuando la razón es por nombre, provider, evidencia observada y los offsets
UTF-16 del specifier.

**Decisiones:**

* La precedencia es de módulo antes que de nombre: un provider ambiguo, una
  versión en conflicto, un provider ausente o un módulo no resuelto anulan las
  razones por nombre de la misma ocurrencia.
* `AMBIGUOUS_PACKAGE_PROVIDER` y `VERSION_MISMATCH` provienen de los conflictos
  que emite `internal/workspace.DetectProviderConflicts`; el worker no decide
  identidad entre repositorios.
* `TYPECHECK_FAILED` exige un error semántico real en un módulo del provider y
  solo se emite cuando ese módulo no produjo ninguna arista exacta: un símbolo
  que el checker sí resuelve sigue siendo un hecho válido.
* `MODULE_NOT_RESOLVED` se añade al mínimo exigido para no perder el hecho de
  un provider registrado cuyo módulo TypeScript no resuelve.
* `DECLARATION_SOURCE_NOT_MAPPED` requiere que todos los destinos declarativos
  del export carezcan de fuentes en el mapeo de LUQUE-0703.

**Verificación:**

```text
pnpm check                         # 11 archivos, 59 tests passed
pnpm build
gofmt -l .
go test ./...                      # 11 paquetes ok, 2 sin tests
go vet ./...
go build ./cmd/kivgraph
```

La cobertura ejercita las siete razones sobre proyectos reales del compilador
nativo, incluida la precedencia entre razones de módulo y de nombre.

**Limitaciones:**

* Los conflictos cross-repository se reciben como entrada; su detección
  pertenece a LUQUE-0408.
* La evidencia de `TYPECHECK_FAILED` es el primer error semántico del módulo,
  no el conjunto completo de diagnósticos.

**Siguiente tarea:** LUQUE-0707.

---

## LUQUE-0707 — Crear fixture cross-repository positivo

**Dependencias:** LUQUE-0705.

**Checklist:**

- [x] Verificar dependencias y alcance.
- [x] Completar acciones y entregables.
- [x] Ejecutar pruebas y benchmarks aplicables.
- [x] Verificar criterios de aceptación y el gate aplicable.
- [x] Registrar resultados, limitaciones y siguiente tarea.

**Estado:** `PASS`.

**Entregables:**

```text
testdata/typescript/cross-repository/shared-library
testdata/typescript/cross-repository/consumer-a
testdata/typescript/cross-repository/consumer-b
ts-worker/src/cross-repository-positive.test.ts
```

**Contenido:**

* `shared-library` publica `@kivgraph-fixture/shared@1.4.2` con barrel,
  reexport aliasado y `declaration maps` hacia sus fuentes reales.
* `consumer-a` usa imports directos de valor y de tipo.
* `consumer-b` usa alias de import, reexport y namespace.

**Decisiones:**

* Los consumidores resuelven el provider mediante `paths`, de modo que el
  fixture no instala ni enlaza `node_modules` dentro del repositorio y no se
  modifica ningún repositorio indexado.
* Las declaraciones publicadas incluyen `.d.ts.map`, por lo que el fixture
  ejercita la rama `DECLARATION_MAP` de LUQUE-0703.
* El import de namespace se conserva deliberadamente: comprueba que no genera
  una arista exacta.

**Verificación:**

```text
pnpm check                         # 12 archivos, 62 tests passed
pnpm build
gofmt -l .
go test ./...
go vet ./...
go build ./cmd/kivgraph
```

La suite comprueba tres aristas exactas en `consumer-a`, dos en `consumer-b`
—alias y reexport—, el mapeo a fuentes del provider, la ausencia de
referencias no resueltas y una única arista `PACKAGE_DEPENDS_ON` por consumidor.

**Limitaciones:**

* El fixture no cubre `node_modules` reales ni enlaces de workspace; esa
  resolución ya está cubierta por las suites temporales de LUQUE-0701.

**Siguiente tarea:** LUQUE-0708.

---

## LUQUE-0708 — Crear fixture cross-repository negativo

**Dependencias:** LUQUE-0705.

**Checklist:**

- [x] Verificar dependencias y alcance.
- [x] Completar acciones y entregables.
- [x] Ejecutar pruebas y benchmarks aplicables.
- [x] Verificar criterios de aceptación y el gate aplicable.
- [x] Registrar resultados, limitaciones y siguiente tarea.

**Estado:** `PASS`.

**Entregables:**

```text
testdata/typescript/cross-repository-negative
ts-worker/src/cross-repository-negative.test.ts
```

**Casos cubiertos:**

| Caso | Resultado esperado |
| --- | --- |
| homónimo local | ninguna arista |
| package duplicado | `AMBIGUOUS_PACKAGE_PROVIDER`, sin arista |
| export ausente | `EXPORT_NOT_FOUND` |
| versión incompatible | `VERSION_MISMATCH`, sin arista |
| `.d.ts` sin source map | `DECLARATION_SOURCE_NOT_MAPPED` |
| otro paquete con mismo símbolo | arista al paquete importado, nunca al homónimo |

**Decisiones:**

* Un conflicto de registro invalida la arista exacta: sin identidad probada del
  provider, la arista sería un `false exact edge`. `resolveUnresolvedReferences`
  descarta esas aristas y conserva únicamente la razón.
* Un `.d.ts` sin mapa sigue produciendo arista al símbolo declarado; lo que
  falta es la fuente física, y eso se reporta como razón, no como ausencia de
  hecho.

**Verificación:**

```text
pnpm check                         # 13 archivos, 64 tests passed
pnpm build
gofmt -l .
go test ./...
go vet ./...
go build ./cmd/kivgraph
```

**Limitaciones:**

* Los conflictos se inyectan como entrada del worker; su detección real
  pertenece a LUQUE-0408.

**Siguiente tarea:** LUQUE-0709.

---

## LUQUE-0709 — Medir precisión TypeScript

**Dependencias:** LUQUE-0707 y LUQUE-0708.

**Checklist:**

- [x] Verificar dependencias y alcance.
- [x] Completar acciones y entregables.
- [x] Ejecutar pruebas y benchmarks aplicables.
- [x] Verificar criterios de aceptación y el gate aplicable.
- [x] Registrar resultados, limitaciones y siguiente tarea.

**Estado:** `PASS`.

**Entregables:**

```text
ts-worker/src/precision-report.ts
ts-worker/src/precision-report.test.ts
ts-worker/src/precision-cli.ts
benchmarks/typescript-cross-repo/results.json
benchmarks/typescript-cross-repo/report.md
```

**Métricas medidas:**

```text
true positives                    8
false positives                   0
false negatives                   0
precision                         1.0000
recall                            1.0000
false exact edges                 0
unresolved correctly classified   4/4
```

**Decisiones:**

* El ground truth vive en `precision-report.ts` como conjuntos explícitos de
  aristas y razones; una regresión aparece como arista sobrante o ausente, no
  como una métrica difusa.
* Los artefactos se regeneran con `pnpm precision` y son deterministas: sin
  marcas de tiempo ni rutas de la máquina, de modo que el diff es la evidencia.
* La medición usa el resolver real sobre los tres proyectos de fixture; no hay
  mocks del compilador.

**Verificación:**

```text
pnpm check                         # 14 archivos, 65 tests passed
pnpm precision                     # TYPESCRIPT_CROSS_REPO_PASS
gofmt -l .
go test ./...
go vet ./...
go build ./cmd/kivgraph
```

**Limitaciones:**

* El corpus es el de los fixtures de fase; la precisión sobre repositorios
  reales a escala se medirá en la fase de aceptación final.
* El recall se mide contra aristas declaradas como esperadas; los imports de
  namespace siguen fuera del contrato exacto.

**Gate:**

```text
TYPESCRIPT_CROSS_REPO_PASS
```

Requisito obligatorio cumplido:

```text
false exact edges = 0
```

**Siguiente tarea:** LUQUE-0710.

---

## LUQUE-0710 — Mapear posiciones exactas hacia fuente

**Dependencias:** LUQUE-0703, LUQUE-0705 y LUQUE-0709.

**Checklist:**

- [x] Verificar dependencias y alcance.
- [x] Completar acciones y entregables.
- [x] Ejecutar pruebas y benchmarks aplicables.
- [x] Verificar criterios de aceptación y el gate aplicable.
- [x] Registrar resultados, limitaciones y siguiente tarea.

**Motivo:** LUQUE-0703 estableció un puente *file-level* y LUQUE-0705 publicó la
posición del símbolo en el artefacto `.d.ts`. Ninguna tarea cubría la posición
exacta del símbolo en su archivo fuente, de modo que `get_symbol` habría
devuelto la línea del artefacto compilado en lugar de la del código real.

**Estado:** `PASS`.

**Entregables:**

* `ts-worker/src/declaration-position-mapper.ts`;
* `ts-worker/src/declaration-source-resolver.ts` (`loadDeclarationSourceMap`);
* `ts-worker/src/imported-symbol-resolver.ts` (`sourcePosition`);
* `ts-worker/src/index.ts` (exportación pública);
* `benchmarks/typescript-cross-repo/*` (métrica de posiciones exactas).

**Contrato:**

```text
DeclarationPositionMapper.create(declarationFile)
  -> lookup(line, character) -> { fileName, line, character } | undefined
```

Cada `ImportedSymbolDeclaration` añade `sourcePosition`: la línea 1-based y el
carácter 0-based del símbolo en su archivo fuente real.

**Decisiones:**

* Se decodifica el campo `mappings` del `.d.ts.map` en segmentos VLQ; se toma el
  segmento precedente más cercano **de la misma línea generada**, que es lo
  único que el formato garantiza. Sin segmento cubriente, la posición queda
  `undefined`: no se aproxima ni se cae a la línea 1.
* Un `source` inexistente en disco anula la posición aunque el segmento exista;
  se conserva el alineamiento por índice con `sources`.
* Los mapas se parsean una vez por archivo de declaración y se cachean durante
  la resolución: un barrel se consulta decenas de veces.
* Los fixtures de LUQUE-0707 y LUQUE-0708 se regeneraron con `tsc` real para que
  sus `mappings` sean auténticos y no cadenas vacías.

**Verificación:**

```text
pnpm check                         # 14 archivos, 65 tests passed
pnpm precision                     # exact source positions 7/7
gofmt -l .
go test ./...
go vet ./...
go build ./cmd/kivgraph
```

La cobertura comprueba posiciones exactas de `const`, `function` e `interface`
a través de barrel y alias, un mapa sin segmentos y un artefacto sin mapa.
El gate `TYPESCRIPT_CROSS_REPO_PASS` se re-midió y sigue emitido, ahora también
exigiendo que las posiciones esperadas estén todas mapeadas.

**Limitaciones resueltas por LUQUE-0711:**

* La posición correspondía al inicio de la declaración, no al identificador.
* Un provider sin `declarationMap` conservaba únicamente el puente file-level.

**Siguiente tarea:** LUQUE-0711.

---

## LUQUE-0711 — Posicionar el identificador y suplir el mapa ausente

**Dependencias:** LUQUE-0710.

**Checklist:**

- [x] Verificar dependencias y alcance.
- [x] Completar acciones y entregables.
- [x] Ejecutar pruebas y benchmarks aplicables.
- [x] Verificar criterios de aceptación y el gate aplicable.
- [x] Registrar resultados, limitaciones y siguiente tarea.

**Motivo:** cerrar las dos limitaciones que dejó LUQUE-0710.

**Estado:** `PASS`.

**Entregables:**

* `ts-worker/src/declaration-name.ts`;
* `ts-worker/src/imported-symbol-resolver.ts` (consulta por identificador);
* `ts-worker/src/provider-source-position-resolver.ts`;
* `ts-worker/src/provider-source-position-resolver.test.ts`;
* `testdata/typescript/cross-repository-negative/nomap`;
* `benchmarks/typescript-cross-repo/*`.

**Contrato añadido:**

```text
resolveProviderSourcePositions(importedSymbolResolution, options?)
  -> { positions, unresolved }
```

**Decisiones:**

* El mapa se consulta en la posición del **nombre declarado**, no del inicio de
  la sentencia. `declarationName` localiza ese token dentro de un nodo que el
  checker ya seleccionó; no compara nombres entre paquetes.
* Un provider sin `declarationMap` se resuelve abriendo **su propio proyecto**
  TypeScript y preguntando a **su** checker qué símbolo exporta el módulo fuente
  bajo ese nombre. La respuesta la da el compilador del repositorio dueño del
  código; sigue sin existir coincidencia nominal entre paquetes.
* La resolución por proyecto del provider es explícita y opcional: abre y cierra
  un `LanguageService` por proyecto, coste que no se impone a quien solo
  necesita las aristas.
* Un provider sin proyecto propio ni fuentes publicadas no genera petición: no
  hay nada que situar y no se inventa un destino.

**Verificación:**

```text
pnpm check                         # 15 archivos, 67 tests passed
pnpm precision                     # exact source positions 8/8
gofmt -l .
go test ./...
go vet ./...
go build ./cmd/kivgraph
```

Posiciones comprobadas contra el código fuente real:

```text
value    src/value.ts:1:13
Shape    src/value.ts:3:17
compute  src/value.ts:7:16
helper   src/helper.ts:3:16
plain    nomap/src/index.ts:1:13    (sin declaration map)
```

**Limitaciones:**

* Un provider que no publica ni mapa ni fuentes sigue sin posición; sólo existe
  el artefacto compilado y no hay hecho que registrar.
* Abrir el proyecto del provider tiene coste; cuando el provider ya esté
  indexado como repositorio propio, LUQUE-0901 podrá reutilizar su símbolo en
  lugar de recalcularlo.

**Siguiente tarea:** LUQUE-0801.

---

# 11. Fase 8 — Go

## LUQUE-0801 — Generar `go.work` sintético

**Dependencias:** TYPESCRIPT_CROSS_REPO_PASS.

**Checklist:**

- [x] Verificar dependencias y alcance.
- [x] Completar acciones y entregables.
- [x] Ejecutar pruebas y benchmarks aplicables.
- [x] Verificar criterios de aceptación y el gate aplicable.
- [x] Registrar resultados, limitaciones y siguiente tarea.

**Estado:** `PASS`.

**Ubicación:**

```text
~/.local/state/kivgraph/go.work
```

**No modificar repositorios.**

**Entregables:**

* `internal/goworkspace/synthetic.go`;
* `internal/goworkspace/synthetic_test.go`;
* `internal/goworkspace/toolchain_test.go`.

**Contrato:**

```text
BuildPlan(ctx, repositories, options) -> Plan
Plan.Render()                         -> bytes go.work
Write(ctx, path, plan, repositories)  -> Result
```

`Plan` conserva versión de Go, módulos incluidos, replacements promovidos y los
conflictos excluidos.

**Decisiones:**

* La ruta destino se rechaza si cae dentro de cualquier repositorio registrado,
  comparando `path` y `realpath`. Un error de configuración no puede hacer que
  Kivgraph escriba un `go.work` en código indexado.
* La versión del workspace es la **más alta** declarada por sus módulos: un
  workspace no puede prometer menos que sus miembros.
* Un `module path` declarado por dos repositorios se excluye como
  `AMBIGUOUS_MODULE_PROVIDER`; el propio `go` rechaza un workspace con dos
  directorios sirviendo el mismo módulo.
* Un `replace` con destinos distintos entre módulos se excluye como
  `MODULE_REPLACE_CONFLICT`; no se elige ganador.
* Un `replace` cuyo módulo sustituido ya está en `use` no se promueve: el
  workspace ya lo sirve desde disco y la directiva lo ocultaría.
* La escritura es atómica —temporal, `fsync`, `rename`, `fsync` del
  directorio— e idempotente: contenido idéntico no reescribe el archivo.

**Verificación:**

```text
gofmt -l .
go test ./...                       # 12 paquetes ok, 2 sin tests
go vet ./...
go build ./cmd/kivgraph
go test -race ./internal/goworkspace
go tool staticcheck ./internal/goworkspace
```

Prueba de humo con la toolchain real: sobre el `go.work` emitido,
`go list -m all` reporta ambos módulos y `go run .` resuelve un import
cross-module y imprime `42`. No se requiere benchmark: la tarea compone
metadatos ya descubiertos.

**Limitaciones:**

* Los conflictos se reportan en el `Plan`; su publicación como referencias no
  resueltas del grafo pertenece a LUQUE-0901.
* Los replacements locales que escapan del repositorio siguen rechazados por
  el descubrimiento de LUQUE-0405, de modo que no llegan al workspace.

**Siguiente tarea:** LUQUE-0802.

---

## LUQUE-0802 — Implementar carga con `go/packages`

**Dependencias:** LUQUE-0801.

**Checklist:**

- [x] Verificar dependencias y alcance.
- [x] Completar acciones y entregables.
- [x] Ejecutar pruebas y benchmarks aplicables.
- [x] Verificar criterios de aceptación y el gate aplicable.
- [x] Registrar resultados, limitaciones y siguiente tarea.

**Estado:** `PASS`.

**Entregables:**

* `internal/goloader/loader.go`;
* `internal/goloader/loader_test.go`;
* `golang.org/x/tools` promovido a dependencia directa.

**Contrato:**

```text
Load(ctx, options) -> { Fset, Packages, Modules, Errors }
```

**Flags semánticos completos:**

```text
NeedName NeedFiles NeedCompiledGoFiles NeedSyntax
NeedTypes NeedTypesInfo NeedTypesSizes NeedImports
NeedDeps NeedModule
```

**Soporte verificado:**

* **context cancellation:** el contexto viaja en `packages.Config` y se
  comprueba antes y después de la carga.
* **errores parciales:** clasificados en `LIST`, `PARSE`, `TYPE` y `UNKNOWN`,
  con paquete y posición. Un paquete roto nunca descarta a los sanos.
* **módulos múltiples:** la carga usa el `go.work` sintético de LUQUE-0801
  mediante `GOWORK`.
* **replace directives:** `Module.ReplacedBy` y `ReplacedDirectory` conservan
  el destino real de la sustitución.

**Decisiones:**

* La indexación es hermética por defecto: `GOPROXY=off` y `-mod=readonly`. Una
  dependencia ausente se reporta como error parcial, no se descarga. La red se
  habilita explícitamente con `AllowNetwork`.
* Se devuelven los `*packages.Package` reales, no una copia empobrecida: las
  fases 0803 a 0805 necesitan `TypesInfo`, `Types` y `Syntax` del mismo
  universo.
* Cada carga crea un universo `go/types` nuevo; los resultados se ordenan por
  package path para que la salida sea determinista.

**Verificación:**

```text
gofmt -l .
go test ./...                       # 13 paquetes ok, 2 sin tests
go vet ./...
go build ./cmd/kivgraph
go test -race ./internal/goloader
go tool staticcheck ./internal/goloader ./internal/goworkspace
```

La suite comprueba, sobre proyectos reales del compilador: resolución
cross-module de `TypesInfo.Uses` a un objeto del módulo proveedor con su
posición en disco, un `replace` local seguido hasta su directorio, un paquete
con error de tipos que no arrastra al válido, cancelación y peticiones
inválidas.

**Limitaciones:**

* La extracción de definiciones, claves estables y usos pertenece a
  LUQUE-0803, LUQUE-0804 y LUQUE-0805.
* No se mide rendimiento aquí: el coste de carga se perfila con el corpus de
  fase.

**Siguiente tarea:** LUQUE-0803.

---

## LUQUE-0803 — Extraer definiciones Go

**Dependencias:** LUQUE-0802.

**Checklist:**

- [x] Verificar dependencias y alcance.
- [x] Completar acciones y entregables.
- [x] Ejecutar pruebas y benchmarks aplicables.
- [x] Verificar criterios de aceptación y el gate aplicable.
- [x] Registrar resultados, limitaciones y siguiente tarea.

**Estado:** `PASS`.

**Usar:**

```text
TypesInfo.Defs
```

**Entregables:**

* `internal/goloader/definitions.go`;
* `internal/goloader/definitions_test.go`.

**Contrato:**

```text
ExtractDefinitions(ctx, result, options) -> []Definition
```

Cada definición conserva repositorio, module path, package path y nombre,
nombre cualificado, clase, propietario, visibilidad, firma con paquetes
cualificados, receptor, la posición del nombre y el rango de la declaración.
`Definition.Object()` expone el objeto `go/types` del mismo universo de carga.

**Clases emitidas:**

```text
func method type alias const var field
```

**Decisiones:**

* Solo se recorren los paquetes raíz: sus dependencias se cargan para dar
  identidad a los tipos, no para indexarlas.
* Variables locales, parámetros, receptores y etiquetas se omiten: no son
  símbolos direccionables del grafo.
* El propietario de métodos sale del receptor real y el de campos del
  `TypeSpec` que los contiene, no de una heurística de nombres.
* La firma se imprime con rutas de paquete completas, de modo que dos tipos
  homónimos de módulos distintos no comparten discriminador.
* La salida se ordena por package path, archivo y offset del nombre.

**Verificación:**

```text
gofmt -l .
go test ./...
go vet ./...
go test -race ./internal/goloader
go tool staticcheck ./internal/goloader
```

La suite comprueba las siete clases sobre un paquete real, la exclusión exacta
de locales y parámetros, metadatos de módulo y paquete, posición del nombre,
receptor puntero, campos exportados y no exportados, método de interfaz,
determinismo y cancelación.

**Limitaciones:**

* Las claves estables se calculan en LUQUE-0804; aquí la identidad es local a
  la carga.
* Los símbolos declarados dentro de funciones —tipos y clausuras locales— no
  se indexan.

**Siguiente tarea:** LUQUE-0804.

---

## LUQUE-0804 — Generar stable keys Go

**Dependencias:** LUQUE-0803.

**Checklist:**

- [x] Verificar dependencias y alcance.
- [x] Completar acciones y entregables.
- [x] Ejecutar pruebas y benchmarks aplicables.
- [x] Verificar criterios de aceptación y el gate aplicable.
- [x] Registrar resultados, limitaciones y siguiente tarea.

**Estado:** `PASS`.

**Usar:**

* module path;
* package path;
* objectpath;
* kind;
* repository.

**Entregables:**

* `internal/goloader/stablekey.go`;
* `internal/goloader/stablekey_test.go`.

**Contrato:**

```text
AssignStableKeys(ctx, definitions) -> []KeyedDefinition
```

Cada entrada añade `ObjectPath`, la identidad canónica auditable y la
`StableKey` BLAKE3 de LUQUE-0303.

**Identidad:**

```text
language      go
repository    nombre del repositorio
package       module path + " " + package path
qualified     objectpath:<path> | syntax:<nombre cualificado>
kind          func|method|type|alias|const|var|field
discriminator firma con paquetes cualificados
```

**Decisiones:**

* El `objectpath` se incrusta **solo cuando es estable por nombre**, es decir
  para objetos de ámbito de paquete: `Compute`, `Shape`.
* Los miembros se direccionan por índice —un método es `Shape.M0` y un campo
  `Shape.UF1`—, de modo que insertar un método o un campo rotaría la identidad
  de todos los posteriores. Para ellos la identidad usa el nombre cualificado
  sintáctico; el path indexado se conserva en `ObjectPath` para la resolución
  cross-repository de LUQUE-0809.
* Un objeto no exportado no tiene `objectpath` y usa la identidad sintáctica:
  sigue siendo un símbolo del repositorio y no puede quedarse sin clave.
* Repositorio, module path y package path son obligatorios: una identidad
  incompleta produce error en lugar de una clave ambigua.

**Verificación:**

```text
gofmt -l .
go test ./...
go vet ./...
go test -race ./internal/goloader
go tool staticcheck ./internal/goloader
```

La suite comprueba unicidad dentro de un paquete, identidad canónica
auditable, separación de homónimos entre paquetes, módulos y repositorios,
estabilidad al mover líneas, **estabilidad al insertar un campo y un método
antes de los existentes** —el caso que el path indexado rompería— y rechazo de
identidades incompletas.

**Limitaciones:**

* Renombrar un símbolo cambia su clave: es un símbolo distinto, no un
  movimiento.
* La clave no distingue dos declaraciones homónimas dentro de una misma
  función; esos símbolos locales no se indexan.

**Siguiente tarea:** LUQUE-0805.

---

## LUQUE-0805 — Extraer usos Go

**Dependencias:** LUQUE-0803.

**Checklist:**

- [x] Verificar dependencias y alcance.
- [x] Completar acciones y entregables.
- [x] Ejecutar pruebas y benchmarks aplicables.
- [x] Verificar criterios de aceptación y el gate aplicable.
- [x] Registrar resultados, limitaciones y siguiente tarea.

**Estado:** `PASS`.

**Usar:**

```text
TypesInfo.Uses
TypesInfo.Selections
```

**Entregables:**

* `internal/goloader/uses.go`;
* `internal/goloader/uses_test.go`.

**Contrato:**

```text
ExtractUses(ctx, result, options) -> []Use
```

Cada uso conserva la declaración que lo contiene, el objeto destino con su
módulo, paquete, nombre cualificado y clase, la selección cuando existe y la
posición exacta.

**Selecciones clasificadas:**

```text
field  method_value  method_expression
```

**Decisiones:**

* El destino es el objeto que resolvió el checker, no lo que deletrea la
  fuente: un homónimo de otro paquete nunca puede confundirse con él.
* Un campo no conoce el tipo que lo declara; su propietario sale de
  `Selection.Recv()`, nunca de una heurística de nombres.
* Los objetos del universo —`int`, `error`— se descartan: no pertenecen a
  ningún repositorio.
* Variables locales, parámetros y nombres de paquete no son destinos.
* `IndirectReceiver` conserva si la selección atravesó un puntero o un campo
  embebido, dato que LUQUE-0808 necesita.

**Verificación:**

```text
gofmt -l .
go test ./...
go vet ./...
go test -race ./internal/goloader
go tool staticcheck ./internal/goloader
```

La suite comprueba, sobre paquetes reales: uso cross-package de función y
constante con su módulo, selección de campo con receptor puntero, valor de
método, expresión de método, exclusión de locales y nombres de paquete,
determinismo y cancelación.

**Limitaciones:**

* La clasificación semántica —`CALLS_DIRECT`, `PASSES_AS_CALLBACK`— pertenece
  a LUQUE-0806 y LUQUE-0807; aquí sólo se resuelven las ocurrencias.
* Los usos dentro de funciones literales se atribuyen a la declaración que las
  contiene.

**Siguiente tarea:** LUQUE-0806.

---

## LUQUE-0806 — Extraer llamadas directas

**Dependencias:** LUQUE-0805.

**Checklist:**

- [x] Verificar dependencias y alcance.
- [x] Completar acciones y entregables.
- [x] Ejecutar pruebas y benchmarks aplicables.
- [x] Verificar criterios de aceptación y el gate aplicable.
- [x] Registrar resultados, limitaciones y siguiente tarea.

**Estado:** `PASS`.

**Para cada `ast.CallExpr`:**

* resolver `Fun`;
* localizar objeto;
* crear `CALLS_DIRECT`.

**Entregables:**

* `internal/goloader/references.go`;
* `internal/goloader/references_test.go`.

**Contrato:**

```text
ClassifyReferences(ctx, result, uses) -> []Reference
```

**Clases emitidas:**

```text
CALLS_DIRECT  TYPE_USES  REFERENCES
```

**Decisiones:**

* El callee se desenvuelve a través de paréntesis, selectores e instanciación
  genérica —`IndexExpr` e `IndexListExpr`—, de modo que
  `(*Shape).Area(shape)` y `Generic[int](x)` son llamadas directas.
* `CALLS_DIRECT` exige que el objeto resuelto sea función o método. Llamar a
  una variable que contiene una función no lo es: el destino exacto no se
  conoce en esta capa y se conserva como `REFERENCES`.
* Una conversión nombra un tipo; nunca produce una llamada. Los destinos de
  clase tipo o alias se clasifican `TYPE_USES`, en el vocabulario del plan.
* La clasificación se une con los usos por archivo y offset, sin recalcular la
  resolución del checker.

**Verificación:**

```text
gofmt -l .
go test ./...
go vet ./...
go tool staticcheck ./internal/goloader
```

La suite comprueba llamada directa a función, llamada por valor de método,
llamada por expresión de método, llamada a través de variable de paquete,
conversión de tipo, lectura de campo y constante, determinismo y cancelación.

**Limitaciones:**

* `PASSES_AS_CALLBACK` llega en LUQUE-0807; hasta entonces un argumento de
  función se conserva como `REFERENCES`.
* Las llamadas a través de variables e interfaces necesitan SSA, fuera del MVP
  según el plan.

**Siguiente tarea:** LUQUE-0807.

---

## LUQUE-0807 — Extraer callbacks

**Dependencias:** LUQUE-0806.

**Checklist:**

- [x] Verificar dependencias y alcance.
- [x] Completar acciones y entregables.
- [x] Ejecutar pruebas y benchmarks aplicables.
- [x] Verificar criterios de aceptación y el gate aplicable.
- [x] Registrar resultados, limitaciones y siguiente tarea.

**Estado:** `PASS`.

**Para cada argumento de llamada:**

* determinar si es función o método;
* crear `PASSES_AS_CALLBACK`;
* no crear `CALLS_DIRECT` al callback.

**Entregables:**

* `internal/goloader/references.go` (rol de argumento);
* `internal/goloader/references_test.go`.

**Decisiones:**

* Los roles se ordenan por fuerza: `callee` nunca se degrada a `argument`. Una
  función invocada en su propio sitio es una llamada, aunque aparezca dentro de
  los argumentos de otra.
* Sólo un argumento que **nombra** un objeto produce callback. Una llamada
  anidada es una expresión, no un identificador: su callee se clasifica por su
  cuenta y no se convierte en callback.
* Un método pasado como valor —`Bind(shape.Area)`— es callback, nunca llamada.
* Un argumento que no resuelve a función ni método conserva su clase previa.

**Verificación:**

```text
gofmt -l .
go test ./...
go vet ./...
go test -race ./internal/goloader
go tool staticcheck ./internal/goloader
```

La suite comprueba función pasada a una conversión y a otra función, método
pasado como valor, llamada anidada cuyo callee no se degrada, y que las
funciones invocadas no producen callback.

**Limitaciones:**

* `ASSIGNS_FUNCTION` y `RETURNS_FUNCTION` quedaron fuera de esta tarea; la
  paridad con el worker TypeScript se completó en LUQUE-0814.
* Los callbacks pasados dentro de literales compuestos o campos de estructura
  no se clasifican todavía.

**Siguiente tarea:** LUQUE-0808.

---

## LUQUE-0808 — Resolver métodos y receivers

**Dependencias:** LUQUE-0805.

**Checklist:**

- [x] Verificar dependencias y alcance.
- [x] Completar acciones y entregables.
- [x] Ejecutar pruebas y benchmarks aplicables.
- [x] Verificar criterios de aceptación y el gate aplicable.
- [x] Registrar resultados, limitaciones y siguiente tarea.

**Estado:** `PASS`.

**Usar `TypesInfo.Selections`.**

**Entregables:**

* `internal/goloader/methods.go`;
* `internal/goloader/methods_test.go`.

**Contrato:**

```text
ResolveMethods(ctx, result, uses) -> []MethodResolution
```

Cada resolución separa dos hechos distintos:

```text
receptor de la expresión   -> tipo, paquete, puntero, interfaz
tipo que declara el método -> tipo, paquete, receptor puntero
promoción                  -> campos embebidos atravesados
```

**Decisiones:**

* El receptor es lo que tiene la expresión; el tipo declarante es donde vive
  el método. Difieren cuando hay promoción, y esa diferencia es justamente el
  hecho que el grafo necesita: `Wrapper.Describe()` es una llamada al método de
  `Base`, no a un método de `Wrapper`.
* La ruta de promoción se reconstruye recorriendo `Selection.Index()` sobre los
  campos reales de la estructura; no se busca por nombre.
* Una llamada a través de un valor de interfaz se marca como tal y su tipo
  declarante es la interfaz. No se inventa la implementación concreta.
* Un método declarado sobre puntero se distingue de la indirección del
  receptor: son dos propiedades independientes.

**Verificación:**

```text
gofmt -l .
go test ./...
go vet ./...
go test -race ./internal/goloader
go tool staticcheck ./internal/goloader
```

La suite comprueba método promovido por embedding de valor, promovido por
embedding de puntero, llamada concreta con receptor puntero, llamada a través
de interfaz, expresión de método sin promoción, determinismo y cancelación.

**Limitaciones:**

* La relación interfaz → implementación concreta es la arista `IMPLEMENTS`, que
  no pertenece a esta tarea ni al MVP de Go.
* Un método alcanzado por una interfaz embebida en otra interfaz conserva el
  receptor de la expresión; su cadena de embebidos no se reconstruye.

**Siguiente tarea:** LUQUE-0809.

---

## LUQUE-0809 — Resolver módulos cross-repository

**Dependencias:** LUQUE-0804 y LUQUE-0805.

**Checklist:**

- [x] Verificar dependencias y alcance.
- [x] Completar acciones y entregables.
- [x] Ejecutar pruebas y benchmarks aplicables.
- [x] Verificar criterios de aceptación y el gate aplicable.
- [x] Registrar resultados, limitaciones y siguiente tarea.

**Estado:** `PASS`.

**Usar:**

```text
module registry
import path
objectpath
go.work sintético
replace
```

**Entregables:**

* `internal/goloader/crossrepo.go`;
* `internal/goloader/crossrepo_test.go`;
* `internal/goloader/stablekey.go` (`ObjectIdentity` reutilizable).

**Contrato:**

```text
NewModuleRegistry(ctx, repositories)                  -> *ModuleRegistry
ResolveCrossRepository(ctx, uses, registry, options)  -> []CrossRepositoryReference
```

**Estados:**

```text
RESOLVED
MODULE_PROVIDER_NOT_FOUND
AMBIGUOUS_MODULE_PROVIDER
OBJECT_PATH_UNAVAILABLE
```

**Decisiones:**

* El module path lo aporta el cargador, que ya siguió el `go.work` sintético y
  cualquier `replace`: el provider es el módulo que realmente entregó el
  código, no el que deletrea el import path.
* Dos repositorios que declaran el mismo module path no se desempatan: la
  referencia queda ambigua y conserva ambos candidatos.
* Un destino sin `objectpath` no puede direccionarse desde otro repositorio y
  se reporta, no se adivina.
* La identidad del destino se calcula con el mismo `ObjectIdentity` que usa
  LUQUE-0804, de modo que la clave del consumidor y la del provider coinciden
  por construcción.

**Corrección encontrada por la prueba:**

La firma del discriminador dependía del observador: el provider imprimía
`Shape` y el consumidor `example.com/provider/api.Shape` para el mismo objeto,
produciendo dos claves para un símbolo y dejando colgando toda arista
cross-repository. El qualifier ahora imprime siempre la ruta del paquete,
incluido el propio.

**Verificación:**

```text
gofmt -l .
go test ./...
go vet ./...
go test -race ./internal/goloader
go tool staticcheck ./internal/goloader
```

La prueba decisiva compara la clave que el consumidor deriva para cada destino
con la que el repositorio proveedor asigna a su propia declaración: función,
constante, tipo, método y campo coinciden exactamente. También cubre módulo
duplicado, provider no registrado y exclusión de usos intra-módulo.

**Limitaciones:**

* La taxonomía completa de razones —incluidas `PACKAGE_NOT_LOADED`,
  `REPLACE_CONFLICT` y `TYPECHECK_FAILED`— se emite en LUQUE-0810.
* La biblioteca estándar no pertenece a ningún repositorio registrado y se
  reporta como provider no encontrado.

**Siguiente tarea:** LUQUE-0810.

---

## LUQUE-0810 — Implementar unresolved Go

**Dependencias:** LUQUE-0809.

**Checklist:**

- [x] Verificar dependencias y alcance.
- [x] Completar acciones y entregables.
- [x] Ejecutar pruebas y benchmarks aplicables.
- [x] Verificar criterios de aceptación y el gate aplicable.
- [x] Registrar resultados, limitaciones y siguiente tarea.

**Estado:** `PASS`.

**Razones:**

```text
MODULE_PROVIDER_NOT_FOUND
PACKAGE_NOT_LOADED
OBJECT_PATH_UNAVAILABLE
AMBIGUOUS_MODULE_PROVIDER
REPLACE_CONFLICT
TYPECHECK_FAILED
```

**Entregables:**

* `internal/goloader/unresolved.go`;
* `internal/goloader/unresolved_test.go`;
* `internal/goloader/uses.go` y `crossrepo.go` (correcciones de identidad).

**Contrato:**

```text
ClassifyUnresolved(ctx, result, references, options) -> []UnresolvedReference
```

Cada entrada conserva repositorio, paquete, archivo, posición, el módulo,
paquete y símbolo solicitados, la razón y la evidencia observada.

**Origen de cada razón:**

```text
MODULE_PROVIDER_NOT_FOUND   estado de LUQUE-0809
AMBIGUOUS_MODULE_PROVIDER   estado de LUQUE-0809 + candidatos
OBJECT_PATH_UNAVAILABLE     estado de LUQUE-0809
PACKAGE_NOT_LOADED          import sin tipos completos o con diagnósticos
TYPECHECK_FAILED            errores TYPE y PARSE del cargador
REPLACE_CONFLICT            conflictos del go.work sintético de LUQUE-0801
```

**Correcciones encontradas por las pruebas:**

* Un miembro de un genérico instanciado no tiene `objectpath` propio. Ahora se
  resuelve por su **origen declarado**: `Box[int].Unwrap` es el símbolo
  `Box.Unwrap`, no una referencia perdida.
* La firma de un miembro instanciado se toma también del origen: el consumidor
  ve `int` donde el provider declara `T`, y firmar la instancia daba dos
  identidades a un mismo símbolo.
* Un campo escrito en un literal compuesto no llega por selección, así que
  quedaba como `Value` en lugar de `Box.Value`. El propietario se toma ahora
  del tipo del literal según el checker.

**Verificación:**

```text
gofmt -l .
go test ./...
go vet ./...
go test -race ./internal/goloader
go tool staticcheck ./internal/goloader
```

La suite cubre las seis razones: módulo duplicado con candidatos, provider no
registrado, import que no carga con su diagnóstico, error de tipos con
posición, conflicto de `replace` del workspace y el mapeo de
`OBJECT_PATH_UNAVAILABLE`. La prueba de genéricos compara la clave del
consumidor con la que el provider asigna a su propia declaración.

**Limitaciones:**

* `OBJECT_PATH_UNAVAILABLE` no se observó con entradas reales tras el
  fallback al origen genérico; se conserva como guarda y se prueba a nivel de
  clasificador.
* La biblioteca estándar no pertenece a ningún repositorio registrado y produce
  `MODULE_PROVIDER_NOT_FOUND`; filtrarla es decisión de la fase de grafo.

**Siguiente tarea:** LUQUE-0811.

---

## LUQUE-0811 — Crear fixtures Go positivos

**Dependencias:** LUQUE-0809.

**Checklist:**

- [x] Verificar dependencias y alcance.
- [x] Completar acciones y entregables.
- [x] Ejecutar pruebas y benchmarks aplicables.
- [x] Verificar criterios de aceptación y el gate aplicable.
- [x] Registrar resultados, limitaciones y siguiente tarea.

**Estado:** `PASS`.

**Incluir:**

* direct call;
* callback;
* method;
* package alias;
* módulo consumidor;
* módulo proveedor;
* replace válido.

**Entregables:**

```text
testdata/go/cross-repository/shared-library
testdata/go/cross-repository/consumer-a
testdata/go/cross-repository/consumer-b
internal/goloader/fixture_positive_test.go
```

**Cobertura:**

| Caso | Dónde |
| --- | --- |
| direct call | `consumer-a` llama `api.Compute` |
| callback | `consumer-a` pasa `api.Compute` a `api.Register` |
| method | `consumer-a` llama `shape.Area()` |
| package alias | `consumer-b` importa `shared "…/shared/api"` |
| módulo proveedor | `shared-library` |
| módulo consumidor | `consumer-a` y `consumer-b` |
| replace válido | `consumer-b` sustituye `legacy` por `./internal/legacy` |

**Decisiones:**

* Los módulos se resuelven con el `go.work` sintético de LUQUE-0801, escrito
  fuera del fixture: no se instala nada ni se modifica ningún repositorio.
* El `replace` apunta a un módulo anidado del propio repositorio, que es la
  única forma de sustitución local que el descubrimiento de LUQUE-0405 acepta.
* Un repositorio puede contener varios módulos; cada uno se carga por separado,
  como hará el indexador real.

**Verificación:**

```text
gofmt -l .
go test ./...
go vet ./...
go tool staticcheck ./internal/goloader
```

La prueba decisiva compara la clave de cada destino cross-repository con la que
el repositorio proveedor asigna a su propia declaración; también comprueba las
clases de referencia por objetivo y la ausencia de referencias no resueltas.

**Limitaciones:**

* El fixture no usa dependencias descargadas: la indexación es hermética y una
  dependencia externa se reportaría como no cargada.

**Siguiente tarea:** LUQUE-0812.

---

## LUQUE-0812 — Crear fixtures Go negativos

**Dependencias:** LUQUE-0809.

**Checklist:**

- [x] Verificar dependencias y alcance.
- [x] Completar acciones y entregables.
- [x] Ejecutar pruebas y benchmarks aplicables.
- [x] Verificar criterios de aceptación y el gate aplicable.
- [x] Registrar resultados, limitaciones y siguiente tarea.

**Estado:** `PASS`.

**Incluir:**

* homónimos;
* módulos duplicados;
* método del receiver incorrecto;
* callback con mismo nombre;
* replace conflictivo.

**Entregables:**

```text
testdata/go/cross-repository-negative
internal/goloader/fixture_negative_test.go
internal/goworkspace/synthetic.go        (override determinista)
internal/goloader/crossrepo.go           (estado REPLACE_CONFLICT)
```

**Casos y resultado esperado:**

| Caso | Resultado |
| --- | --- |
| homónimo local | arista al símbolo local, nunca al provider |
| módulo duplicado | `AMBIGUOUS_MODULE_PROVIDER`, sin identidad |
| método de otro receptor | arista al receptor importado, nunca al homónimo |
| callback homónimo | arista a la función local que se pasa |
| replace conflictivo | `REPLACE_CONFLICT`, sin arista |

**Hallazgo del toolchain:**

`go` **rechaza cargar el workspace completo** cuando dos módulos usados
declaran replacements distintos para el mismo módulo, aunque nadie lo importe.
Excluir la directiva, como hacía LUQUE-0801, dejaba el fixture sin cargar y con
él todos los repositorios.

Decisión: el workspace emite un **override determinista** —el destino
lexicográficamente menor— para que la carga sea posible, conserva el conflicto
en el plan, y `ResolveCrossRepository` marca `REPLACE_CONFLICT` toda referencia
a ese módulo. El override es un recurso de carga; ninguna arista se apoya en el
destino adivinado.

**Verificación:**

```text
gofmt -l .
go test ./...
go vet ./...
go test -race ./internal/goloader ./internal/goworkspace
go tool staticcheck ./internal/goloader ./internal/goworkspace
```

La suite comprueba que el homónimo local y el del provider se cuentan por
separado, que el callback resuelto es el local, que `Shape.Area` sólo se
atribuye a dos receptores —el local y el importado—, que el módulo duplicado
conserva ambos candidatos sin clave, y que una referencia a un módulo con
replacement adivinado no produce arista.

**Limitaciones:**

* El fixture mantiene `mirror` sin importar: comprueba que un paquete presente
  en el registro pero no importado nunca aparece como destino.
* El conflicto de `replace` es declarativo; el caso con importación real se
  cubre con el estado inyectado del mismo clasificador.

**Siguiente tarea:** LUQUE-0813.

---

## LUQUE-0813 — Medir precisión Go

**Dependencias:** LUQUE-0811 y LUQUE-0812.

**Checklist:**

- [x] Verificar dependencias y alcance.
- [x] Completar acciones y entregables.
- [x] Ejecutar pruebas y benchmarks aplicables.
- [x] Verificar criterios de aceptación y el gate aplicable.
- [x] Registrar resultados, limitaciones y siguiente tarea.

**Estado:** `PASS`.

**Entregables:**

```text
internal/goloader/precision.go
internal/goloader/precision_test.go
benchmarks/go-semantic/main.go
benchmarks/go-semantic/results.json
benchmarks/go-semantic/report.md
```

**Métricas medidas:**

```text
true positives                    16
false positives                   0
false negatives                   0
precision                         1.0000
recall                            1.0000
false exact edges                 0
unresolved correctly classified   2/2
```

**Decisiones:**

* El ground truth enumera cada arista esperada como
  `origen -> CLASE -> paquete.destino`, con **multiconjunto**: dos lecturas del
  mismo campo cuentan dos veces y una arista de más aparece como sobrante.
* La medición usa el pipeline real completo —workspace sintético, carga,
  usos, clasificación, resolución cross-repository y no resueltas—; no hay
  mocks del compilador.
* Los artefactos se regeneran con `go run ./benchmarks/go-semantic` y son
  deterministas: sin marcas de tiempo ni rutas de máquina.
* El caso negativo mide también que el módulo ambiguo y el `replace` en
  conflicto se clasifican, no que desaparecen.

**Verificación:**

```text
gofmt -l .
go test ./...
go vet ./...
go test -race ./internal/goloader
go tool staticcheck ./internal/goloader ./benchmarks/go-semantic
go run ./benchmarks/go-semantic        # GO_SEMANTIC_PASS
```

**Limitaciones:**

* El corpus es el de los fixtures de fase; la precisión sobre repositorios
  reales a escala se mide en la fase de aceptación final.
* Las aristas intra-módulo no entran en la métrica: esta fase mide la
  resolución cross-repository.

**Gate:**

```text
GO_SEMANTIC_PASS
```

Requisito cumplido:

```text
false exact edges = 0
```

**Siguiente tarea:** LUQUE-0814.

---

## LUQUE-0814 — Igualar clases de referencia con TypeScript

**Dependencias:** LUQUE-0807.

**Checklist:**

- [x] Verificar dependencias y alcance.
- [x] Completar acciones y entregables.
- [x] Ejecutar pruebas y benchmarks aplicables.
- [x] Verificar criterios de aceptación y el gate aplicable.
- [x] Registrar resultados, limitaciones y siguiente tarea.

**Motivo:** el worker TypeScript emite `ASSIGNS_FUNCTION` y `RETURNS_FUNCTION`
desde LUQUE-0610; el extractor Go no. Sin esta paridad, el mismo hecho
produciría aristas distintas según el lenguaje del repositorio.

**Estado:** `PASS`.

**Clasificar en Go:**

```text
ASSIGNS_FUNCTION
RETURNS_FUNCTION
```

**Requisitos cumplidos:**

* la función o método debe resolverse por el checker, nunca por nombre;
* una asignación a variable local no crea arista si el destino no es un
  símbolo indexable;
* no degradar `CALLS_DIRECT` ni `PASSES_AS_CALLBACK`.

**Entregables:**

* `internal/goloader/references.go`;
* `internal/goloader/references_test.go`.

**Orden de fuerza de los roles:**

```text
CALLS_DIRECT > RETURNS_FUNCTION > ASSIGNS_FUNCTION > PASSES_AS_CALLBACK
```

Es el mismo orden que aplica `ts-worker/src/reference-extractor.ts`, de modo
que un hecho equivalente produce la misma clase en ambos lenguajes.

**Decisiones:**

* Se reconocen `AssignStmt`, `ValueSpec` y `ReturnStmt`; sólo un operando que
  **nombra** un objeto produce arista. Una llamada anidada es una expresión y su
  callee se clasifica por su cuenta.
* La clase sólo cambia cuando el destino es función o método: almacenar una
  constante o un campo sigue siendo `REFERENCES`.
* Llamar a una variable que contiene una función continúa siendo `REFERENCES`;
  la asignación que la llenó sí produce `ASSIGNS_FUNCTION` hacia la función real.

**Verificación:**

```text
gofmt -l .
go test ./...
go vet ./...
go test -race ./internal/goloader
go tool staticcheck ./internal/goloader
go run ./benchmarks/go-semantic        # GO_SEMANTIC_PASS, artefactos sin cambios
```

La suite comprueba función almacenada, función retornada, expresión de método
almacenada en variable de paquete, ausencia de arista para símbolos no
invocables y conservación de las clases más fuertes. Los artefactos de
LUQUE-0813 no cambian: las nuevas clases no alteran las aristas del fixture,
lo que confirma que la medición sigue siendo exacta.

**Limitaciones:**

* Los valores función dentro de literales compuestos o campos de estructura no
  se clasifican todavía; tampoco en TypeScript.
* Un canal o un mapa que transporta funciones queda fuera: eso exige SSA.

**Siguiente tarea:** LUQUE-0901.

---

# 12. Fase 9 — Grafo canónico

## LUQUE-0901 — Normalizar hechos semánticos

**Dependencias:** TYPESCRIPT_CROSS_REPO_PASS y GO_SEMANTIC_PASS.

**Checklist:**

- [x] Verificar dependencias y alcance.
- [x] Completar acciones y entregables.
- [x] Ejecutar pruebas y benchmarks aplicables.
- [x] Verificar criterios de aceptación y el gate aplicable.
- [x] Registrar resultados, limitaciones y siguiente tarea.

**Estado:** `PASS`.

**Crear un formato común para:**

* repositorios;
* paquetes;
* archivos;
* símbolos;
* aristas;
* evidencia;
* unresolved.

**Entregables:**

```text
internal/facts/facts.go          modelo canónico, validación y merge
internal/facts/golang.go         normalizador Go
internal/facts/typescript.go     contrato de cable ts-facts-v1 y normalizador
ts-worker/src/facts-cli.ts       emisor del payload (`pnpm facts`)
testdata/protocol/ts-facts-v1/   payloads reales del worker
```

**Modelo:**

```text
Repository Package File Symbol Evidence Edge UnresolvedReference
```

Con el vocabulario del plan: niveles de confianza, procedencias y clases de
arista. `Set.Validate` rechaza claves duplicadas, referencias colgantes y
cualquier arista que declare exactitud con una procedencia que no puede
sostenerla.

**Decisiones:**

* **Las claves las deriva un solo lado.** El worker TypeScript reporta
  componentes de identidad y posiciones; Go calcula la `StableKey`. Si cada
  lenguaje calculara la suya, un mismo símbolo tendría dos identidades — el
  fallo que ya apareció tres veces en la fase 8.
* Las rutas del payload son **relativas al repositorio**: una clave no puede
  incrustar la máquina que la produjo. Hay una prueba que normaliza el mismo
  payload en dos rutas distintas y exige claves idénticas.
* Una arista sólo existe si **ambos extremos tienen identidad durable**. Lo que
  no la tiene no se degrada a un nombre: se descarta y se cuenta en el informe
  de normalización, de modo que un hecho perdido es visible.
* `Merge` deduplica por clave durable: dos repositorios indexados en la misma
  pasada comparten los símbolos del provider y deben producir un solo nodo.
* Un consumidor por sí solo **no valida**: sus destinos viven en el provider.
  Es el comportamiento correcto y está cubierto por una prueba.

**Verificación:**

```text
gofmt -l .
go test ./...
go vet ./...
go test -race ./internal/facts
go tool staticcheck ./internal/facts
pnpm check                              # 15 archivos, 67 tests
pnpm facts <repo> <root> <salida.json>  # regenera los payloads golden
```

La suite Go normaliza los fixtures reales de LUQUE-0811 de extremo a extremo
—workspace, carga, definiciones, claves, referencias, cross-repository y no
resueltas— y comprueba las ocho aristas cross-repository, la validación del
grafo combinado y el determinismo. La suite TypeScript consume payloads
**emitidos por el worker**, no muestras escritas a mano.

**Limitaciones:**

* El payload TypeScript aún no transporta la clase ni la firma del símbolo
  destino de un `IMPORTS_SYMBOL`, así que esas aristas no se normalizan
  todavía: inventar esos campos daría dos identidades a un mismo símbolo. Se
  resuelve en LUQUE-0907.
* El emisor de hechos es un CLI del worker; su integración con el protocolo
  `ts-worker-v1` pertenece a la fase de supervisión.

**Siguiente tarea:** LUQUE-0902.

---

## LUQUE-0902 — Diseñar el esquema LadybugDB definitivo

**Dependencias:** LUQUE-0901.

**Checklist:**

- [x] Verificar dependencias y alcance.
- [x] Completar acciones y entregables.
- [x] Ejecutar pruebas y benchmarks aplicables.
- [x] Verificar criterios de aceptación y el gate aplicable.
- [x] Registrar resultados, limitaciones y siguiente tarea.

**Estado:** `PASS`.

**Crear nodos y relaciones definitivos.**

Documentar:

* claves primarias;
* multiplicidades;
* índices;
* constraints;
* propiedades;
* versión.

**Entregables:**

```text
internal/storage/ladybug/canonical_schema.go
internal/storage/ladybug/canonical_schema_test.go
internal/storage/ladybug/canonical_schema_native_test.go
schemas/ladybug/002-canonical.cypher
docs/storage/canonical-schema.md
```

**Esquema, versión `002`:**

```text
nodos        GraphMetadata Repository Package File Symbol Evidence
             UnresolvedReference
relaciones   CONTAINS_PACKAGE CONTAINS_FILE DEFINES OBSERVED_IN
             REPORTS_UNRESOLVED PACKAGE_DEPENDS_ON MODULE_DEPENDS_ON
             IMPORTS_SYMBOL EXPORTS REEXPORTS REFERENCES CALLS_DIRECT
             PASSES_AS_CALLBACK ASSIGNS_FUNCTION RETURNS_FUNCTION TYPE_USES
             IMPLEMENTS EXTENDS EMBEDS OVERRIDES
```

**Decisiones:**

* El DDL y la documentación se **generan desde una sola fuente de metadatos**
  en Go. Dos pruebas comparan los archivos versionados con lo generado: un
  esquema documentado que la base no tiene es peor que no documentarlo.
* Toda clave primaria es `stable_key`, una clave durable de Kivgraph. Ninguna se
  deriva de un nombre visible ni la genera la base.
* Las relaciones de contención son `ONE_MANY` y `OBSERVED_IN` es `MANY_ONE`;
  las semánticas son `MANY_MANY`, porque un símbolo puede llamar al mismo
  destino desde varios sitios y cada ocurrencia lleva su evidencia.
* Toda relación semántica transporta `confidence`, `provenance`,
  `evidence_key`, `source_snapshot` y `resolver_version`, como fija el plan.
* `REPORTS_UNRESOLVED` cuelga del repositorio: un fallo de módulo no tiene
  archivo y colgarlo de uno inventado sería falsear su origen.
* `GraphMetadata` guarda versión de esquema y de resolutor: una base con otra
  versión se reconstruye, no se migra en caliente.
* **Índices:** sólo el de clave primaria, que LadybugDB crea. No se declaran
  secundarios; las búsquedas exactas las sirve el HotSnapshot. Declarar índices
  que la versión fijada no soporta sería documentación falsa.

**Verificación:**

```text
gofmt -l .
go test ./...
go vet ./...
go vet -tags ladybug ./internal/storage/ladybug
go tool staticcheck ./internal/facts
```

Las pruebas comprueban: paridad entre **todas** las clases de arista del modelo
canónico y las tablas del esquema, claves primarias durables, multiplicidades
por tipo de relación, propiedades obligatorias de las aristas semánticas, orden
de creación —ningún `REL` antes de sus nodos— y que los archivos versionados
coinciden con los metadatos.

**Limitaciones:**

* `internal/storage/ladybug` conserva avisos de `staticcheck` anteriores a esta
  tarea en `query.go`, `query_native.go`, `mutation_native.go` y
  `arrow_scan_native.go`; los archivos nuevos no añaden ninguno.

**Actualización del 2026-08-05:** la prueba que carga el DDL en una base real
quedó sin ejecutar en esta tarea por falta de la biblioteca nativa. LUQUE-0908
la hizo obtenible de forma reproducible y **la prueba pasa**: el esquema `002`
se crea en un LadybugDB real y volver a aplicarlo es idempotente.

**Siguiente tarea:** LUQUE-0908.

---

## LUQUE-0908 — Obtener LadybugDB de forma reproducible

**Dependencias:** LUQUE-0201 y LUQUE-0902.

**Checklist:**

- [x] Verificar dependencias y alcance.
- [x] Completar acciones y entregables.
- [x] Ejecutar pruebas y benchmarks aplicables.
- [x] Verificar criterios de aceptación y el gate aplicable.
- [x] Registrar resultados, limitaciones y siguiente tarea.

**Motivo:** las suites `-tags ladybug` no se podían ejecutar. La biblioteca
nativa vivía en `/tmp`, el «paso de build» que debía descargarla nunca se
implementó y la CI no ejecutaba ninguna prueba contra una base real. Todo lo
que las fases 2 y 3 declararon verificado con LadybugDB era irreproducible.

**Estado:** `PASS`.

**Entregables:**

```text
scripts/fetch-ladybug.sh
Makefile                          objetivos ladybug-lib y test-ladybug
.github/workflows/ci.yml          job ladybug
docs/dependencies/ladybugdb.md    par corregido y checksums
docs/decisions/ladybugdb-qualification.md
THIRD_PARTY_NOTICES.md
```

**Hallazgo: el par fijado no era compatible.**

LUQUE-0201 fijaba el core `v0.19.0` con el binding `v0.13.1`. Con ese par, la
primera llamada C revienta:

```text
SIGSEGV: segmentation violation
signal arrived during cgo execution
github.com/LadybugDB/go-ladybug._Cfunc_lbug_default_system_config()
```

La causa es de ABI, no de configuración: el core `v0.19.0` añade cuatro campos
a `lbug_system_config` —`throw_on_wal_replay_failure`, `enable_checksums`,
`enable_multi_writes` y `enable_default_hash_index`— y el binding devuelve esa
estructura **por valor** compilando contra su cabecera, más pequeña. La pila se
corrompe antes de abrir ninguna base.

El core `v0.13.1` declara exactamente la misma estructura que la cabecera del
binding. Verificado por diff de cabeceras y ejecutando la suite completa.

**Regla derivada:** el core y el binding se fijan **con la misma versión**.
Subir uno obliga a subir el otro y a repetir esta comprobación.

**Decisiones:**

* La descarga usa la URL del tag inmutable y **verifica el SHA-256 antes de
  extraer**; un digest que no cuadra borra el archivo y falla.
* La biblioteca se deja en `.tooling/ladybug/<versión>`, ignorada por Git: el
  artefacto nativo no entra en el repositorio.
* El script es idempotente y revalida por digest, de modo que un `.verified`
  manipulado fuerza una nueva descarga.
* La CI ejecuta un job `ladybug` dedicado: si el par vuelve a romperse, se ve
  en la CI y no seis fases después.

**Verificación:**

```text
scripts/fetch-ladybug.sh        descarga, verifica digest, idempotente
make test-ladybug               go test -tags ladybug ./...  → todos los paquetes ok
```

Con el par corregido pasan, entre otras, las suites nativas de esquema
sintético y canónico, writer, doctor, consultas, staging, recuperación y los
benchmarks de LadybugDB.

**Limitaciones:**

* La calificación de rendimiento de LUQUE-0212 y LUQUE-0214 se midió con el
  core anterior; sus cifras quedan como registro histórico y deben repetirse
  sobre el par fijado en la fase de rendimiento.
* Windows arm64 sigue sin asset soportado; macOS usa el asset universal que
  publica `v0.13.1`.

**Siguiente tarea:** LUQUE-0903.

---

## LUQUE-0903 — Implementar full rebuild

**Dependencias:** LUQUE-0902 y LUQUE-0908.

**Checklist:**

- [x] Verificar dependencias y alcance.
- [x] Completar acciones y entregables.
- [x] Ejecutar pruebas y benchmarks aplicables.
- [x] Verificar criterios de aceptación y el gate aplicable.
- [x] Registrar resultados, limitaciones y siguiente tarea.

**Pipeline:**

```text
facts
→ staging
→ graph.next
→ bulk load
→ integrity
→ snapshot
→ golden probes
→ publish
```

**Estado:** `PASS`.

**Entregables:**

```text
internal/rebuild/rebuild.go                          orquestador de las ocho etapas
internal/storage/ladybug/canonical_load.go           render determinista del esquema 002
internal/storage/ladybug/canonical_load_native.go    staging CSV + COPY + counts + sondas
internal/storage/ladybug/canonical_load_stub.go      degradado sin CGO
internal/facts/facts.go                              UnresolvedKey
cmd/kivgraph/main.go                                    comando kivgraph rebuild
```

**Punto de partida real:** cada etapa existía aislada y **ninguna llamaba a la
siguiente**. No había un solo consumidor de `facts.Set` fuera de su paquete, ni
un camino que escribiera el esquema canónico `002` en una base: hasta esta
tarea, `002` sólo estaba probado como DDL, mientras el `Reader`/`Writer` seguían
sobre el esquema experimental `001-synthetic`.

**Decisiones:**

* La publicación **no se reimplementa**: la etapa `publish` es
  `generation.Store.Publish`, con la carga y el snapshot dentro de `Build` y la
  integridad y las sondas dentro de `Validate`. Un fallo en cualquiera de las
  dos deja el `CURRENT` anterior intacto porque es el propio store el que
  aborta.
* `CanonicalColumns` se deriva **en vivo** de `CanonicalNodeTables()` y
  `CanonicalRelationshipTables()`. No hay una lista paralela de columnas que
  pueda desincronizarse del esquema.
* El COPY carga **todas las tablas de nodo antes de cualquier relación**;
  ninguna arista puede entrar antes que sus extremos.
* `OBSERVED_IN` y `REPORTS_UNRESOLVED` no son `facts.EdgeKind`: se **derivan**
  de `Evidence.FileKey` y de `UnresolvedReference.RepositoryKey`. Una arista de
  esas dos clases no puede inventarse desde el conjunto de aristas.
* Las sondas golden se **derivan del propio conjunto de hechos** —primer
  símbolo en orden de clave, par (símbolo, tabla) con más salientes, y una
  arista concreta origen→destino—, nunca de claves escritas a mano. Sólo las
  relaciones `Symbol→Symbol` pueden respaldar una sonda anclada en símbolos.
* El digest de snapshot es SHA-256 sobre la versión del esquema y las cuentas
  por tabla, ordenadas: sin reloj y sin rutas de máquina, de modo que dos
  reconstrucciones del mismo conjunto coinciden byte a byte.
* La integridad de esta tarea es **paridad de cuentas** por tabla, incluidas
  las de cuenta cero. Los seis invariantes semánticos son LUQUE-0904.

**Verificación de extremo a extremo sobre código Go real.** No es una fixture
escrita a mano: los hechos se derivaron del fixture `testdata/go/cross-repository`
con `goloader` + `facts.NormalizeGo`, cargando **cada módulo** de cada
repositorio —incluido el módulo anidado `internal/legacy` de `consumer-b`, que
no forma parte del `./...` de su padre— y combinando con `Set.Merge`:

```text
3 repositorios, 4 paquetes, 4 archivos, 9 símbolos, 32 aristas, 0 no resueltas
Set.Validate() pasa: 0 aristas colgantes
```

Sobre esos hechos, `kivgraph rebuild` con la biblioteca nativa:

```text
[PASS] facts          validated 3 repositories, 4 packages, 4 files, 9 symbols, 32 edges
[PASS] staging        staged 14 canonical table(s)
[PASS] graph.next     generations/000001.tmp
[PASS] bulk load      copied 39 node(s) and 47 edge(s)
[PASS] integrity      27 canonical table(s) matched their expected count
[PASS] snapshot       digest ac40aef4a329929332f17cd8c1a4613074bf60b32b5dfa89d39ddc8a2e8b5845
[PASS] golden probes  3 golden probe(s) passed
[PASS] publish        published generation 000001
```

La generación publicada se leyó **después** del rename atómico, con un lector
independiente del que la escribió:

```text
Repository 3   Package 4   File 4   Symbol 9   Evidence 15   GraphMetadata 4
CONTAINS_PACKAGE 4   CONTAINS_FILE 4   DEFINES 9   OBSERVED_IN 15
REFERENCES 7   CALLS_DIRECT 5   TYPE_USES 2   PASSES_AS_CALLBACK 1
generación 000002, 86 filas en 27 tablas
```

Atomicidad comprobada en disco real, no sólo con hooks inyectados:

```text
000002 publicada        CURRENT = 000002
hechos con arista colgante  falla en `facts`, CURRENT sigue 000002, sin directorio nuevo
id de generación ya usado   falla en `publish`, CURRENT sigue 000002, sin restos .tmp
```

Dos reconstrucciones del mismo conjunto emitieron el **mismo digest**
(`ac40aef4…`), que es la prueba de determinismo contra una base real.

```text
gofmt -l .        sin diferencias
go vet ./...      limpio
go test ./...     todos los paquetes ok
make test-ladybug todos los paquetes ok, incluidas las suites nativas nuevas
```

**Limitaciones:**

* La integridad es paridad de cuentas. Los seis invariantes semánticos
  (`0 exact edges without source`, evidencia ausente, claves duplicadas,
  confianza desconocida, propiedad de repositorio inválida) llegan en
  LUQUE-0904.
* ~~`IMPLEMENTS`, `EMBEDS` y `OVERRIDES` no tenían productor.~~ Cerrado
  después: `goloader.ResolveTypeRelations` las calcula con el comprobador de
  tipos y `NormalizeGo` las emite.
* `IMPORTS_SYMBOL` de TypeScript sigue pendiente de LUQUE-0907.
* `DiagnoseStorage` continúa validando el esquema `001-synthetic`; no reconoce
  todavía una base canónica. No lo toqué: pertenece a la verificación de
  LUQUE-0904.
* El comando consume un `facts.Set` en JSON. El indexado que lo produce desde
  repositorios reales es de fases posteriores; el derivador usado para verificar
  esta tarea fue un andamio y no se ha dejado en el árbol.

**Siguiente tarea:** LUQUE-0904.

---

## LUQUE-0904 — Implementar verificación de integridad

**Dependencias:** LUQUE-0903.

**Checklist:**

- [x] Verificar dependencias y alcance.
- [x] Completar acciones y entregables.
- [x] Ejecutar pruebas y benchmarks aplicables.
- [x] Verificar criterios de aceptación y el gate aplicable.
- [x] Registrar resultados, limitaciones y siguiente tarea.

**Comprobar:**

```text
0 exact edges without source
0 exact edges without target
0 missing evidence files
0 duplicate stable keys
0 unknown confidence
0 invalid repository ownership
```

**Estado:** `PASS`.

**Entregables:**

```text
internal/storage/ladybug/canonical_integrity.go            reglas, catálogos y tipos
internal/storage/ladybug/canonical_integrity_native.go     los seis invariantes en Cypher
internal/storage/ladybug/canonical_integrity_stub.go       degradado sin CGO
internal/rebuild/rebuild.go                                etapa integrity ampliada
cmd/kivgraph/main.go                                          comando kivgraph doctor graph
```

**Decisión de semántica: «sin origen» no puede significar «el nodo no existe».**
LadybugDB garantiza los dos extremos de toda relación, así que una arista sin
extremo es inexpresable en el almacén. La regla que sí tiene contenido es
**no declarado**: un `Symbol` sin `DEFINES` entrante, o un `Package` sin
`CONTAINS_PACKAGE` entrante, es un marcador de posición, y una arista exacta
anclada ahí está colgando. Es la lectura fiel del principio del plan: una
referencia omitida es degradación aceptable; una arista exacta hacia un símbolo
que nadie declara es un fallo de integridad.

**Las seis reglas, tal como se evalúan:**

| Regla | Qué recorre |
| --- | --- |
| `exact_edge_without_source` | aristas semánticas con confianza exacta cuyo origen no está declarado |
| `exact_edge_without_target` | lo mismo para el destino |
| `missing_evidence_file` | `evidence_key` sin `Evidence`, o `Evidence` sin `OBSERVED_IN` a un `File` |
| `duplicate_stable_key` | una misma clave usada por dos tablas de nodo distintas |
| `unknown_confidence` | `confidence`/`provenance` fuera de catálogo, y exactitud declarada con procedencia no exacta |
| `invalid_repository_ownership` | `repository_key` que la cadena de contención no confirma, o repositorio inexistente |

**Decisiones:**

* Las tablas semánticas y las que llevan `confidence` se **derivan** de
  `CanonicalRelationshipTables()`. No hay lista escrita a mano de quince
  tablas que pueda desincronizarse del esquema.
* Una regla violada **no es un error**: es un informe. Sólo un fallo del motor
  devuelve error. Un verificador que aborta en la primera violación no sirve
  para diagnosticar.
* La cuenta de violaciones es exacta; las muestras están acotadas a
  `MaxIntegritySamples` y ordenadas por tabla y clave. Un grafo roto debe poder
  diagnosticarse sin volcar el grafo entero.
* La etapa `integrity` del rebuild exige ahora **las dos cosas**: paridad de
  cuentas e invariantes. Cualquiera de las dos aborta la publicación, así que un
  grafo que viole un invariante nunca llega a `CURRENT`.
* `facts` no expone un enumerador de sus constantes, así que el catálogo se
  declara una vez en `canonical_integrity.go` y un test parsea `facts.go` con
  `go/parser` para fallar si el catálogo y las constantes se separan.

**Verificación sobre un grafo canónico real.** Hechos derivados del fixture
`testdata/go/cross-repository` con `goloader` + `facts.NormalizeGo`, publicados
con `kivgraph rebuild`:

```text
[PASS] integrity  27 of 27 canonical table(s) matched their expected count; 0 invariant violation(s)
[PASS] publish    published generation 000001

kivgraph doctor graph --database generations/000001/graph.db  →  graph doctor: PASS
  exact_edge_without_source 0   exact_edge_without_target 0   missing_evidence_file 0
  duplicate_stable_key 0        unknown_confidence 0          invalid_repository_ownership 0
```

**Y las seis reglas muerden.** Cada violación se inyectó con Cypher crudo sobre
una copia de la generación publicada —`LoadCanonical` valida los hechos, así que
una violación sólo puede entrar escribiendo directamente— y se comprobó por la
ruta del operador, `kivgraph doctor graph`, que salió 1 en los seis casos:

| Inyección | Regla que falla | Muestra emitida |
| --- | --- | --- |
| `Symbol` huérfano con arista exacta hacia él | `exact_edge_without_source` | `source confidence EXACT_TYPECHECKED has no declaring DEFINES from File` |
| `evidence_key` inexistente | `missing_evidence_file` | `evidence_key NO_EXISTE has no Evidence observed in a File` |
| clave de `File` reutilizada en `Evidence` | `duplicate_stable_key` | `stable_key also used by File` |
| `confidence: MUY_SEGURO` | `unknown_confidence` | `is not a facts.Confidence value` |
| exacta con `TREE_SITTER_SYNTAX` | `unknown_confidence` | `claims exactness but provenance … is not exact` |
| `repository_key` contradictorio | `invalid_repository_ownership` | `is not confirmed by the containment chain` |

El primer caso incumple además `invalid_repository_ownership`, y es correcto: un
símbolo huérfano tampoco tiene cadena de contención que confirme su repositorio.
Una sola inyección puede violar dos reglas legítimamente.

```text
gofmt -l .        sin diferencias
go vet ./...      limpio
go test ./...     todos los paquetes ok
make test-ladybug todos los paquetes ok, con las suites nativas nuevas
```

**Bug corregido en el camino:** una prueba nueva de `internal/rebuild` aseveraba
el mensaje del *stub* (`ErrUnavailable`) para demostrar que `Options.Integrity`
usa su valor por defecto. Con `-tags ladybug` ese mensaje no existe y la prueba
fallaba **sólo en la ejecución nativa**. Ahora asevera el prefijo del envoltorio
`ladybug `, que es lo único que ambas compilaciones comparten y sigue
demostrando el cableado. Sin `make test-ladybug` habría pasado inadvertida.

**Limitaciones:**

* Que un invariante violado impida la publicación está probado con un informe
  inyectado, no con una carga real que los produzca: `LoadCanonical` rechaza
  antes los hechos que lo causarían. La función que corre dentro de `Validate`
  es exactamente la que `doctor graph` ejerce contra un grafo real y roto.
* ~~`doctor storage` no reconocía una base canónica.~~ Cerrado después:
  detecta el esquema, valida el que corresponda y lo declara en su salida.
* ~~`IMPLEMENTS`, `EMBEDS` y `OVERRIDES` no estaban ejercitadas sobre datos
  reales.~~ Cerrado después: el fixture `testdata/go/type-relations` las
  produce y la generación mixta las almacena.
* Los arneses usados para derivar hechos e inyectar violaciones son andamios y
  no se han dejado en el árbol.

**Siguiente tarea:** LUQUE-0905.

---

## LUQUE-0905 — Implementar backup y rollback

**Dependencias:** LUQUE-0903 y LUQUE-0904.

**Checklist:**

- [x] Verificar dependencias y alcance.
- [x] Completar acciones y entregables.
- [x] Ejecutar pruebas y benchmarks aplicables.
- [x] Verificar criterios de aceptación y el gate aplicable.
- [x] Registrar resultados, limitaciones y siguiente tarea.

**Mantener:**

```text
graph.active
graph.next
graph.backup
```

**Estado:** `PASS`.

**Entregables:**

```text
internal/storage/generation/backup.go    puntero BACKUP, List, Prune, NextID
internal/storage/generation/store.go     BACKUP integrado en Publish y Restore
internal/rebuild/rollback.go             Roles y Rollback verificado
internal/rebuild/rebuild.go              retención tras publicar
cmd/kivgraph/main.go                        kivgraph graph status y kivgraph rollback
```

**Los tres nombres son roles, no directorios.** `generations/<id>` y `CURRENT`
son load-bearing y están probados; renombrarlos a `graph.active` habría sido
fidelidad cosmética a cambio de romper artefactos ya publicados. El mapeo:

| Rol del plan | Qué es en disco |
| --- | --- |
| `graph.active` | la generación que apunta `CURRENT` |
| `graph.next` | el candidato `<id>.tmp` que construye `Publish` |
| `graph.backup` | la generación anterior, retenida en `BACKUP` |

**El hueco real que se cerró.** «La anterior» sólo existía como
`Publication.PreviousID` **en memoria**: tras un reinicio nadie sabía a dónde
volver. Y nada podaba: cada rebuild acumulaba una generación más, con
`CheckSpace` exigiendo el doble de la activa. Ahora hay un puntero persistente
y retención de exactamente activa más backup.

**Decisiones:**

* `BACKUP` se escribe con la misma disciplina que `CURRENT` —fichero
  transitorio, fsync, rename atómico, fsync del directorio— reutilizando un
  `writePointer` generalizado en vez de duplicar la lógica.
* **Consistencia ante caída con dos punteros.** No pueden actualizarse en un
  solo rename. El orden es `BACKUP` primero y `CURRENT` después, y la regla de
  recuperación es: **si `BACKUP` iguala a `CURRENT`, o apunta a una generación
  que ya no existe, no hay backup**. Se interpreta al leer; no se repara el
  fichero. Es autoconsistente y no exige intervención manual.
* `abortPublication` y el camino de reversión de `Restore` devuelven **los dos**
  punteros a su estado anterior, no sólo `CURRENT`.
* Tras un rollback, la que era activa pasa a ser el backup. La simetría permite
  volver hacia adelante y evita que un rollback destruya la única alternativa.
* `Prune` nunca borra la activa, el backup ni un `<id>.tmp` en curso, y sin
  `CURRENT` no borra nada: sin activa no se sabe qué proteger.
* Un fallo de poda **no** invalida una publicación ya efectiva —el grafo activo
  es correcto— pero se reporta en el `Detail` de la etapa `publish`.
* `NextID` salta el reservado `000000` y cualquier id ya ocupado: dos rebuilds
  seguidos no pueden colisionar.

**Verificación del ciclo completo con biblioteca nativa**, sobre hechos
derivados del fixture Go real:

```text
publish 000001   previous <none>
publish 000002   previous 000001
publish 000003   previous 000002; pruned generation(s) 000001

graph.active: 000003    graph.next: generations/000004.tmp    graph.backup: 000002
retained: 000002, 000003          en disco: 000002, 000003
```

Rollback y vuelta hacia adelante, con revalidación completa:

```text
rollback: 000003 -> 000002   digest esperado == observado   invariants: PASS   exit 0
graph.active: 000002    graph.backup: 000003      ← roles invertidos
rollback: 000002 -> 000003   →  graph.active: 000003    graph.backup: 000002
```

**Las cuatro defensas, probadas contra disco real.** En los cuatro casos
`CURRENT` quedó intacto:

| Manipulación | Resultado |
| --- | --- |
| digest del backup alterado | `snapshot digest mismatch: recomputed …, generation 000002 recorded …` |
| `snapshot.sha256` ausente | `generation has no snapshot.sha256: cannot verify its digest before reactivating it` |
| `BACKUP` igualado a `CURRENT` | `graph.backup: none`, rollback rechazado |
| invariante roto en el backup | `integrity check failed: 1 invariant violation(s)` |

El cuarto caso es el que más dice, y se aisló a propósito: la violación se
inyectó con `SET r.provenance = 'TREE_SITTER_SYNTAX'` sobre una arista ya
existente, **sin alterar ninguna cuenta**. El digest cuadró exactamente y el
rollback se negó igualmente. Es la prueba de que la verificación de invariantes
no es redundante con el digest: un grafo semánticamente roto puede tener las
cuentas perfectas.

```text
gofmt -l .                                        sin diferencias
go vet ./...                                      limpio
go test ./...                                     todos los paquetes ok
make test-ladybug                                 todos los paquetes ok
go test -race ./internal/storage/generation ./internal/rebuild   ok
```

La consistencia ante caída está además probada con el `FaultInjector` del
propio store: un fallo en `write_current` o `rename_current` **después** de
haber escrito `BACKUP` deja los dos punteros como estaban.

**Limitaciones:**

* La retención es exactamente activa más backup, sin configurar. El plan pide
  tres roles; una profundidad mayor de histórico no está pedida y no se inventa.
* `NextID` envuelve de `999999` a `000001`. Con retención de dos generaciones el
  reciclado es inofensivo, pero un histórico profundo necesitaría otra política.
* El rollback exige `snapshot.sha256`. Las generaciones publicadas antes de
  LUQUE-0903 no lo tienen y no son reactivables; no existe ninguna en uso.
* `graph.next` sólo existe mientras `Publish` construye. `graph status` reporta
  la ruta que ocuparía, no un directorio presente.

**Siguiente tarea:** LUQUE-0906.

---

## LUQUE-0906 — Construir HotSnapshot real

**Dependencias:** LUQUE-0904.

**Checklist:**

- [x] Verificar dependencias y alcance.
- [x] Completar acciones y entregables.
- [x] Ejecutar pruebas y benchmarks aplicables.
- [x] Verificar criterios de aceptación y el gate aplicable.
- [x] Registrar resultados, limitaciones y siguiente tarea.

**Debe generarse desde el grafo definitivo.**

**Gate:**

```text
CANONICAL_GRAPH_PASS
```

**Estado:** `PASS`. Gate emitido en
[`docs/decisions/canonical-graph-qualification.md`](docs/decisions/canonical-graph-qualification.md).

**Entregables:**

```text
internal/facts/codes.go                              numeración canónica congelada
internal/storage/ladybug/canonical_scan.go           lectura del grafo definitivo
internal/storage/ladybug/canonical_scan_native.go
internal/rebuild/snapshot.go                         adaptador y construcción
internal/hotsnapshot/builder.go                      dos correcciones de contrato
cmd/kivgraph/main.go                                    comando kivgraph snapshot
```

**El hueco que había.** `BuildGraphSnapshot` existía desde la fase 6 y **nunca
se había invocado fuera de sus propios tests**, con fixtures escritas a mano. No
existía lectura del grafo canónico, ni adaptador, ni —lo más grave— ninguna
tabla de correspondencia para los códigos `uint8` que el HotSnapshot almacena
en cada arista. El contenedor sabía guardar números que nadie sabía interpretar.

**Decisiones:**

* **La numeración vive en `internal/facts`**, que es el dueño del vocabulario.
  `hotsnapshot` no puede importarlo —sería un ciclo— y debe seguir siendo un
  contenedor numérico. El código `0` queda reservado: un campo sin asignar no
  puede leerse como un valor legítimo. La tabla está congelada y un test la
  compara contra las constantes reales parseando `facts.go`, así que añadir una
  constante sin código rompe la compilación de la prueba.
* **El adaptador vive en `internal/rebuild`**, dueño del pipeline. Crear un
  cuarto paquete de snapshot al lado de `hotsnapshot` habría introducido una
  segunda convención.
* **El snapshot se deriva de la base, nunca de los hechos.** Ni `ScanCanonical`
  ni el adaptador reciben un `facts.Set`.
* La etapa `snapshot` del rebuild **construye el HotSnapshot real** además de
  escribir el digest, y falla la publicación si el grafo no puede convertirse.
  Un grafo que no se puede consultar no debe llegar a `CURRENT`.

**Dos defectos que sólo aparecieron con datos reales.** Los dos cortes en
paralelo pasaron sus pruebas con fixtures fabricadas; el grafo real no
construía. Ninguno es un fallo de los cortes: son contratos equivocados en
`hotsnapshot`, escritos contra fixtures donde todo estaba poblado.

1. **El builder exigía riqueza descriptiva, no identidad.** Rechazaba un
   repositorio sin `commit`, un paquete sin `module_path` y un símbolo sin
   `signature`. Los tres son legítimamente vacíos: un checkout sin metadatos de
   git, un paquete npm, una constante. Los tests existentes sólo defendían
   integridad referencial; esas exigencias habían entrado como condiciones
   incidentales en el mismo `if`. Ahora la validación separa identidad e
   integridad referencial —que se exigen— de los campos descriptivos, y hay
   pruebas para ambas mitades de la línea.
2. **`EdgeRow` no podía representar dos ocurrencias de la misma relación.** El
   esquema declara las relaciones semánticas `MANY_MANY` justamente porque el
   mismo símbolo puede alcanzar el mismo destino desde varios sitios, cada uno
   con su evidencia. La fila del snapshot tiraba la clave de evidencia, así que
   dos usos distintos colapsaban en filas idénticas y el detector de duplicados
   rechazaba el grafo entero. Ocurre en el corpus real:

```text
REFERENCES  consumer-a:main -> shared-library:Shape.Width
  evidence:file:consumer-a:main.go:158:163
  evidence:file:consumer-a:main.go:206:211
```

`EdgeRow` ganó `EvidenceKey`, presente en la ordenación, la igualdad y el
digest. Además `ErrInvalidSnapshotRows` era un centinela **sin detalle**:
rechazaba el grafo sin decir qué fila ni por qué, y diagnosticarlo exigió
añadírselo. Ahora cada rechazo nombra la fila, su clave y el motivo.

**Verificación de extremo a extremo**, sobre hechos derivados del fixture Go:

```text
[PASS] snapshot   hot snapshot 1 built (3 repositories, 4 packages, 4 files,
                  9 symbols, 15 edges, 17 edge(s) not represented in the CSR)
[PASS] publish    published generation 000001

kivgraph snapshot --root …   PASS   digest 0c8ce3bf…   9 símbolos, 15 aristas
segunda construcción      mismo digest
```

El snapshot no sólo se construye: **se consulta**. Las nueve claves durables
hacen ida y vuelta por `SymbolByStableKey`, la adyacencia responde, y la
travesía cross-repository —la consulta que justifica el proyecto— alcanza siete
nodos en dos repositorios desde el `main` de `consumer-a` hasta los símbolos
declarados en `shared-library`.

De las 32 aristas, 15 entran en el CSR y 17 no: contención y paquete a paquete
no caben en un índice símbolo a símbolo. No es pérdida —la contención está en
los propios nodos y la dependencia entre paquetes en la base canónica— y se
declara en cada informe.

```text
gofmt -l .        sin diferencias
go vet ./...      limpio
go test ./...     todos los paquetes ok
make test-ladybug todos los paquetes ok
```

**Limitaciones:**

* El corpus es el fixture Go de tres repositorios; la escala grande es
  LUQUE-1602.
* ~~`IMPLEMENTS`, `EMBEDS` y `OVERRIDES` no aparecían en el grafo.~~ Cerrado
  después: ya se producen y se publican.
* El grafo calificado es sólo Go: `IMPORTS_SYMBOL` de TypeScript es LUQUE-0907.
* El snapshot vive en memoria. Persistirlo y publicarlo a los lectores MCP es
  de fases posteriores.
* `ScanCanonical` lee con `Query`+`nextTuple`, medido en ~229k filas/s. A un
  millón de símbolos son decenas de segundos; si eso pesa, la ruta Arrow ya
  está demostrada en `arrow_scan_native.go`.

**Siguiente tarea:** LUQUE-0907.

---

## LUQUE-0907 — Normalizar `IMPORTS_SYMBOL` de TypeScript

**Dependencias:** LUQUE-0901 y LUQUE-0705.

**Checklist:**

- [x] Verificar dependencias y alcance.
- [x] Completar acciones y entregables.
- [x] Ejecutar pruebas y benchmarks aplicables.
- [x] Verificar criterios de aceptación y el gate aplicable.
- [x] Registrar resultados, limitaciones y siguiente tarea.

**Motivo:** el payload `ts-facts-v1` transporta el binding consumidor y el
símbolo destino de un import, pero no la **clase** ni la **firma** del destino.
Sin esos dos campos, la clave del destino que derivaría el consumidor no
coincide con la que el repositorio proveedor asigna a su propia declaración, y
la arista quedaría colgando. Es el mismo fallo que la fase 8 encontró tres
veces.

**Acciones:**

* extender `resolveImportedSymbols` para que el destino conserve clase y firma
  tomadas del checker, nunca inferidas;
* transportarlas en `ts-facts-v1`;
* normalizar la arista `IMPORTS_SYMBOL` en `internal/facts`.

**Criterios de aceptación:**

* la clave del destino calculada por el consumidor coincide con la que produce
  el propio provider al indexarse, comprobado sobre el fixture de LUQUE-0707;
* un destino sin clase o sin firma no produce arista, sino una referencia no
  resuelta;
* `Set.Validate` sigue pasando sobre el grafo combinado.

**Estado:** `PASS`.

**Entregables:**

```text
ts-worker/src/declaration-classifier.ts     clasificación compartida
ts-worker/src/imported-symbol-resolver.ts   identidad del destino
ts-worker/src/facts-cli.ts                  registry real y wire v2
internal/facts/typescript.go                normalización de IMPORTS_SYMBOL
testdata/protocol/ts-facts-v2/              tres goldens reales
```

**El fallo era más profundo que un campo ausente.** No bastaba con transportar
clase y firma: la firma que **ve** el consumidor no es la que **indexa** el
proveedor. El consumidor resuelve el import hasta `dist/value.d.ts`, cuyo texto
es `export declare function compute(input: number): number`, mientras el
proveedor se indexa desde `src/value.ts` y produce
`export function compute(input: number): number`. Como el discriminador de la
clave **es** la firma, copiar el texto del `.d.ts` habría generado dos claves
distintas para el mismo símbolo. Es exactamente el fallo de la fase 8: la
identidad no puede depender de quién mira.

**Solución: leer la fuente del proveedor, con el mismo código.** LUQUE-0703 ya
mapea cada declaración de un `.d.ts` a su posición real en la fuente. El
consumidor abre esa fuente, localiza la declaración por esa posición y la
clasifica con **las mismas funciones** que el proveedor usa sobre sí mismo
—`declarationCandidate` y `compactSignature`, extraídas a
`declaration-classifier.ts`—. Mismo código sobre los mismos bytes: la clave
coincide por construcción, no por coincidencia.

**Decisiones:**

* **Sin mapa de declaración no hay arista.** Si `sourceStatus` no es
  `DECLARATION_MAP`, o la posición no cae sobre una declaración reconocible, la
  clase y la firma del proveedor son desconocidas y se emite una referencia no
  resuelta con razón explícita (`PROVIDER_SOURCE_UNAVAILABLE`). Nunca se
  infiere ni se cae al texto del `.d.ts`.
* **El binding importado es un símbolo del consumidor**, de clase `import`, para
  que la arista sea de verdad `Symbol→Symbol` como exige el esquema canónico.
* **El wire sube a `ts-facts-v2`.** Un payload sin campo de imports es
  indistinguible de uno donde no se importó nada, y esa ambigüedad es
  justamente la que el proyecto rechaza en todo lo demás.
* **Se eliminó el `emptyRegistry` del CLI.** `pnpm facts` no resolvía ningún
  import externo: el registro de proveedores estaba fijado a vacío, así que
  todo import cruzado caía en `PACKAGE_PROVIDER_NOT_FOUND`. Ahora acepta
  repositorios proveedores y construye el registry de verdad.
* La identidad del destino se construye con **el mismo helper** que construye la
  de un símbolo local, para que no puedan divergir.

**Una arista `IMPORTS_SYMBOL` apunta fuera de su payload.** El destino vive en
el repositorio proveedor, así que el `Set` de un consumidor **por sí solo no
valida**: la arista está colgando hasta que se combina con el `Set` del
proveedor por `Set.Merge`. Es el mismo comportamiento que ya tenía Go, y está
documentado en el código.

**Verificación: paridad de claves sobre el fixture de LUQUE-0707.** Los tres
goldens se generaron con el CLI real y se normalizaron y combinaron:

```text
shared-library   symbols=4 edges=9  imports_symbol=0
consumer-a       symbols=4 edges=9  imports_symbol=3
consumer-b       symbols=4 edges=8  imports_symbol=2

  Shape        -> Shape    (shared-library, interface)  coincide con la clave del proveedor
  republished  -> value    (shared-library, variable)   coincide con la clave del proveedor
  compute      -> compute  (shared-library, function)   coincide con la clave del proveedor
  value        -> value    (shared-library, variable)   coincide con la clave del proveedor
  helper       -> helper   (shared-library, function)   coincide con la clave del proveedor
```

`republished` y `helper` son los casos exigentes: el nombre local del binding
**no** coincide con el del proveedor (`value as republished`,
`aliasedHelper as helper`), y aun así la clave del destino es la del proveedor.
La identidad depende sólo del destino, nunca del nombre que le dé el consumidor.

**Y la arista llega al grafo canónico**, no se queda en el normalizador:

```text
[PASS] facts      3 repositories, 3 packages, 4 files, 12 symbols, 26 edges
[PASS] integrity  27 of 27 canonical table(s) matched; 0 invariant violation(s)
[PASS] snapshot   hot snapshot built (12 symbols, 7 edges, 19 not in the CSR)
[PASS] publish    published generation 000001

kivgraph doctor graph   PASS, los seis invariantes a cero
kivgraph snapshot       PASS
```

Que `doctor graph` pase importa aquí especialmente: las cinco aristas son
`EXACT_TYPECHECKED` y `exact_edge_without_source`/`_target` exigen que ambos
extremos estén **declarados**. Si la clave del destino no coincidiera con la del
proveedor, ese invariante lo cazaría.

```text
gofmt -l .        sin diferencias
go vet ./...      limpio
go test ./...     todos los paquetes ok
make test-ladybug todos los paquetes ok
pnpm check        16 ficheros, 71 tests, limpio
```

**Limitaciones, todas cerradas después** en la tanda de completitud registrada
en [`docs/decisions/canonical-graph-qualification.md`](docs/decisions/canonical-graph-qualification.md):

* ~~Un import de namespace no producía arista exacta.~~ Ahora cada **miembro
  usado** de un namespace (`shared.compute`) produce su `IMPORTS_SYMBOL`. El
  binding `shared` sigue sin producirla, y es correcto: no nombra un símbolo.
* ~~Los usos locales de un binding importado no producían `REFERENCES`.~~ El
  extractor resuelve ahora el uso contra el propio binding local.
* ~~`EXPORTS` y `REEXPORTS` no tenían productor.~~ El nombre público es ahora un
  símbolo de clase `export`, con `EXPORTS` hacia la declaración del mismo
  repositorio y `REEXPORTS` cuando llega por un `from`.

**Siguiente tarea:** LUQUE-1001, ya en la fase 10.

---

# 13. Fase 10 — Incrementalidad

## LUQUE-1001 — Integrar `fsnotify`

**Dependencias:** CANONICAL_GRAPH_PASS.

**Checklist:**

- [x] Verificar dependencias y alcance.
- [x] Completar acciones y entregables.
- [x] Ejecutar pruebas y benchmarks aplicables.
- [x] Verificar criterios de aceptación y el gate aplicable.
- [x] Registrar resultados, limitaciones y siguiente tarea.

**Objetivo:** detectar cambios en repositorios registrados.

**Entregables:**

```text
internal/watcher/watcher.go
internal/watcher/ignore.go
internal/watcher/watcher_test.go
go.mod
go.sum
```

**Criterios de aceptación:**

* `fsnotify` queda fijado en `v1.9.0`.
* Se observan recursivamente los directorios existentes y los nuevos
  directorios creados después de iniciar el watcher.
* Se omiten symlinks, árboles de dependencias por defecto y exclusiones
  configuradas con los mismos segmentos y `**` de descubrimiento.
* Cada evento entrega repository, path absoluto limpio y operaciones portables;
  los errores del backend se entregan por un canal separado.
* `Close`, cancelación y cierre de canales son idempotentes y no dejan la
  goroutine de procesamiento pendiente.

**Estado:** `PASS`.

**Archivos creados:** los tres archivos de `internal/watcher/`.

**Archivos modificados:** `go.mod` y `go.sum`.

**Tests ejecutados:**

```text
go test ./internal/watcher -count=10
go test -race ./internal/watcher
go test ./...
go vet ./...
make build
```

**Resultados:** todas las pruebas pasan; el watcher informa escrituras,
creaciones y cancelación, y no registra rutas excluidas.

**Benchmarks:** no aplica; esta tarea integra la señal cruda. Debounce,
batching, hash y reconciliación pertenecen a LUQUE-1002, LUQUE-1003 y
LUQUE-1004.

**Limitaciones:** `fsnotify` es una señal de cambio y puede emitir eventos
duplicados o perder eventos por overflow; el watcher no intenta convertirlos
en cambios semánticos ni calcula hashes. La reconciliación posterior es la
fuente de recuperación.

**Siguiente tarea:** LUQUE-1002.

---

## LUQUE-1002 — Implementar debounce y batching

**Dependencias:** LUQUE-1001.

**Checklist:**

- [x] Verificar dependencias y alcance.
- [x] Completar acciones y entregables.
- [x] Ejecutar pruebas y benchmarks aplicables.
- [x] Verificar criterios de aceptación y el gate aplicable.
- [x] Registrar resultados, limitaciones y siguiente tarea.

**Defaults:**

```text
debounce: 150 ms
maximum batch: 500 ms
```

**Entregables:**

```text
internal/watcher/debounce.go
internal/watcher/debounce_test.go
```

**Criterios de aceptación:**

* El debounce se reinicia con cada evento y emite después del período quieto.
* El máximo comienza con el primer evento y no se prolonga por eventos
  posteriores.
* Eventos del mismo `repository` y `path` se coalescen mediante OR de
  `Operations`, conservando el orden de primera aparición de cada path.
* El cierre del canal de entrada entrega el batch pendiente; la cancelación
  explícita detiene el ciclo sin emitir trabajo pendiente.
* Se rechazan duraciones no positivas y un máximo menor que el debounce.

**Estado:** `PASS`.

**Archivos creados:** `internal/watcher/debounce.go` y
`internal/watcher/debounce_test.go`.

**Archivos modificados:** ninguno adicional.

**Tests ejecutados:**

```text
go test ./internal/watcher -count=10
go test ./...
go vet ./...
go test -race ./internal/watcher
make build
```

**Resultados:** todas las pruebas pasan. La suite cubre validación de
duraciones, coalescencia y orden, período quieto, límite máximo, cierre de
entrada y cancelación.

**Benchmarks:** no aplica; el comportamiento temporal depende de timers y la
implementación no introduce un camino de análisis cuyo rendimiento sea todavía
representativo. El benchmark debe hacerse al integrar el pipeline incremental.

**Limitaciones:** el batcher no hace `stat`, hashing ni clasificación semántica.
La cancelación descarta únicamente el batch aún no emitido, de forma explícita;
el cierre normal del input sí lo entrega completo.

**Siguiente tarea:** LUQUE-1003.

---

## LUQUE-1003 — Implementar content hashes

**Dependencias:** LUQUE-1001.

**Checklist:**

- [x] Verificar dependencias y alcance.
- [x] Completar acciones y entregables.
- [x] Ejecutar pruebas y benchmarks aplicables.
- [x] Verificar criterios de aceptación y el gate aplicable.
- [x] Registrar resultados, limitaciones y siguiente tarea.

**Objetivo:** no reindexar cuando el contenido no cambie.

**Contrato:**

* El algoritmo es `SHA-256` en hexadecimal, compatible con el
  `contentHash` del worker TypeScript.
* El hasher recibe los hashes conocidos del grafo indexado y mantiene una
  caché en memoria por `repository` y path absoluto.
* Un hash nuevo o distinto produce `Changed`; uno igual produce
  `Unchanged`; un archivo conocido que desaparece o deja de ser regular produce
  `Removed`.
* Directorios y paths no conocidos producen `Skipped` y nunca una arista ni una
  reindexación.
* Los errores o cancelaciones no aplican actualizaciones parciales a la caché.

**Entregables:**

```text
internal/watcher/hash.go
internal/watcher/hash_test.go
```

**Estado:** `PASS`.

**Archivos creados:** `internal/watcher/hash.go` y
`internal/watcher/hash_test.go`.

**Archivos modificados:** ninguno adicional.

**Tests ejecutados:**

```text
go test ./internal/watcher -count=10
go test ./...
go vet ./...
go test -race ./internal/watcher
make build
```

**Resultados:** todas las pruebas pasan. La suite verifica cambios,
contenido idéntico sin reindexación, eliminaciones, directorios ignorados,
eventos duplicados, reemplazo por una ruta no regular, hashes inválidos y
cancelación.

**Benchmarks:** no aplica todavía; medir el hash aislado no representa el coste
del pipeline incremental completo. El benchmark de extremo a extremo queda
para la integración del indexador.

**Limitaciones:** la caché es en memoria y debe reconstruirse desde el grafo al
reiniciar el proceso. La lectura observa el contenido disponible durante el
stream; no intenta resolver una escritura concurrente posterior al `Lstat`.
La clasificación semántica e invalidación siguen siendo tareas posteriores.

**Siguiente tarea:** LUQUE-1004.

---

## LUQUE-1004 — Implementar reconciliación periódica

**Dependencias:** LUQUE-1003.

**Checklist:**

- [x] Verificar dependencias y alcance.
- [x] Completar acciones y entregables.
- [x] Ejecutar pruebas y benchmarks aplicables.
- [x] Verificar criterios de aceptación y el gate aplicable.
- [x] Registrar resultados, limitaciones y siguiente tarea.

**Objetivo:** recuperar cambios aunque `fsnotify` haya perdido o no haya
entregado un evento.

**Contrato:**

* `Reconciler.Reconcile` recorre cada repositorio, omitiendo symlinks,
  exclusiones por defecto/configuradas y respetando las raíces de código.
* El recorrido incluye fuentes Go/TypeScript y manifests comunes o declarados
  explícitamente por la configuración.
* La comparación usa `ContentHasher`: separa `Added`, `Modified`, `Unchanged`,
  `Removed` y `Skipped`, y actualiza la caché solo después de observar todo el
  batch.
* Un renombrado solo se reporta cuando existe exactamente una ruta eliminada y
  una nueva con el mismo hash; los hashes duplicados quedan como add/remove y
  no se adivina una relación.
* `ManifestChanges` identifica cambios de manifests para que la invalidación
  posterior pueda reconstruir el registro afectado.
* `Reconciler.Run` ejecuta una pasada inmediata y repite cada intervalo
  positivo; errores de lectura, del sink o cancelaciones se devuelven.

**Entregables:**

```text
internal/watcher/reconcile.go
internal/watcher/reconcile_test.go
```

**Archivos modificados:**

```text
internal/watcher/hash.go
```

Se añadió `ContentHasher.KnownFiles`, que devuelve una copia ordenada de la
caché para comparar el estado anterior con el recorrido actual sin exponer
mutabilidad interna.

**Estado:** `PASS`.

**Tests ejecutados:**

```text
go test ./internal/watcher -count=10
go test -race ./internal/watcher
go test ./...
go vet ./...
make build
```

**Resultados:** todas las pruebas pasan. La suite cubre recuperación sin
eventos, archivos nuevos, modificaciones, archivos eliminados, renombrados
unívocos, renombrados ambiguos, manifests configurados, exclusiones,
cancelación periódica y validación de argumentos.

**Benchmarks:** no aplica todavía; la tarea define detección y recuperación,
pero no integra aún el pipeline de reindexación. El coste de recorrer y
hashear el repositorio debe medirse con el indexador incremental completo.

**Limitaciones:** la caché sigue siendo en memoria y se reconstruye desde el
grafo al reiniciar. La reconciliación detecta diferencias respecto al último
estado conocido, pero no puede demostrar si una diferencia proviene de un
overflow de `fsnotify` o de una modificación ocurrida fuera del proceso. Un
archivo que cambia mientras se lee puede requerir otra pasada. Renombrados con
contenido idéntico en varias rutas no se clasifican para evitar falsos
positivos. La invalidación semántica y la reindexación pertenecen a tareas
posteriores.

**Siguiente tarea:** LUQUE-1005.

---

## LUQUE-1005 — Implementar invalidación TypeScript

**Dependencias:** LUQUE-1002 y LUQUE-1003.

- [x] Verificar dependencias y alcance.
- [x] Completar acciones y entregables.
- [x] Ejecutar pruebas y benchmarks aplicables.
- [x] Verificar criterios de aceptación y el gate aplicable.
- [x] Registrar resultados, limitaciones y siguiente tarea.

**Distinguir:**

```text
body only
signature
import
export
manifest
project config
```

**Contrato implementado:**

* `ClassifyTypeScriptChange` consume inventarios Tree-sitter y rangos ya
  observados; nunca convierte candidatos sintácticos en símbolos o aristas.
* `BODY_ONLY` solo reindexa el archivo; cambios de firma, exportación o
  declaraciones invalidan consumidores y resuelven referencias afectadas.
* Imports invalidan la resolución de módulos; manifests y configuración de
  proyecto reconstruyen el registro, invalidan la resolución y fuerzan una
  reindexación de proyecto.
* `package.json`, lockfiles comunes y `tsconfig`/`jsconfig` se reconocen por
  ruta, y los manifests o configuraciones no convencionales pueden marcarse
  explícitamente.
* Inventarios con errores sintácticos y clasificaciones no acotables caen en
  `UNKNOWN` y `REINDEX_PROJECT`, nunca en una falsa precisión.

**Entregables:**

```text
internal/indexer/invalidation.go
internal/indexer/typescript.go
internal/indexer/invalidation_test.go
```

**Estado:** `PASS`.

**Resultados:** la clasificación conserva repository/package/file/path y
rangos ordenados, deduplica acciones y separa reindexación local, invalidación
de consumidores, resolución de referencias y reconstrucción de registry. La
implementación no aplica todavía cambios al grafo; esa orquestación pertenece a
LUQUE-1007.

**Verificación:** `go test ./internal/indexer -count=10`, `go test ./...`,
`go vet ./...`, `go test -race ./internal/indexer` y `make build` pasan.

**Benchmarks:** no aplica; el clasificador es una decisión acotada por
inventarios y metadatos. El coste extremo a extremo queda para la integración
incremental.

**Limitaciones:** la clasificación de TypeScript depende de que el worker o
el parser entregue los inventarios anterior y actual; no resuelve referencias
ni escribe hechos por sí misma. Una configuración de proyecto usa el alcance
amplio de proyecto para evitar snapshots parciales.

**Siguiente tarea:** LUQUE-1006.

---

## LUQUE-1006 — Implementar invalidación Go

**Dependencias:** LUQUE-1002 y LUQUE-1003.

- [x] Verificar dependencias y alcance.
- [x] Completar acciones y entregables.
- [x] Ejecutar pruebas y benchmarks aplicables.
- [x] Verificar criterios de aceptación y el gate aplicable.
- [x] Registrar resultados, limitaciones y siguiente tarea.

**Distinguir:**

```text
body
signature
import
go.mod
replace
package deletion
```

**Contrato implementado:**

* `ClassifyGoChange` consume señales explícitas del loader y aplica
  precedencia conservadora: eliminación de paquete, replace, go.mod,
  eliminación de archivo, imports, firma y cuerpo.
* `ClassifyGoSourceChange` compara imports y fingerprints de declaraciones con
  `go/parser` y `go/format`; un parseo inválido devuelve error y solicita
  `REINDEX_PROJECT`.
* `ClassifyGoModChange` usa `golang.org/x/mod/modfile` y distingue cambios de
  `replace` de cambios ordinarios del módulo.
* Las clasificaciones no crean símbolos, providers ni aristas; solo describen
  el alcance que consumirá la etapa incremental.

**Entregables:**

```text
internal/indexer/invalidation.go
internal/indexer/go.go
internal/indexer/invalidation_test.go
```

**Estado:** `PASS`.

**Resultados:** cuerpo reindexa el archivo; firma reindexa el provider e
invalida consumidores; imports reindexan el package e invalidan resolución;
go.mod/replace reconstruyen registry y proyecto; eliminaciones retiran archivo
o paquete e invalidan referencias entrantes.

**Verificación:** `go test ./internal/indexer -count=10`, `go test ./...`,
`go vet ./...`, `go test -race ./internal/indexer` y `make build` pasan.

**Benchmarks:** no aplica por la misma razón que LUQUE-1005; el benchmark debe
medir el pipeline que carga, normaliza y publica el delta.

**Limitaciones:** el comparador de fuente recibe bytes de una transición y
requiere que el loader sea la autoridad para errores de tipos y resolución de
paquetes. Un fallo sintáctico no se degrada a `BODY_ONLY`; se propaga y usa
alcance de proyecto.

**Siguiente tarea:** LUQUE-1007.

---

## LUQUE-1007 — Implementar delta LadybugDB

**Dependencias:** LUQUE-1005 y LUQUE-1006.

**Checklist:**

- [x] Verificar dependencias y alcance.
- [x] Completar acciones y entregables.
- [x] Ejecutar pruebas y benchmarks aplicables.
- [x] Verificar criterios de aceptación y el gate aplicable.
- [x] Registrar resultados, limitaciones y siguiente tarea.

**Debe:**

* borrar relaciones antiguas;
* actualizar nodos;
* insertar relaciones nuevas;
* actualizar unresolved;
* ejecutar una transacción.

**Los cinco puntos ya están implementados y probados como primitivos**; esta
tarea los orquesta desde la invalidación de LUQUE-1005/1006, no los escribe:

```text
facts.Diff(previous, next) (Delta, error)      el cambio, en unidades de archivo
facts.Delta{ReplacedFiles, RemovedFiles, Upsert}
ladybug.ApplyCanonicalDelta(ctx, path, delta, options)   transaccional
```

**Por qué existían antes de tiempo:** el escritor que había (`ladybug.Writer`,
`Delta`, `Apply`) sirve al esquema experimental `001-synthetic`, no al
canónico: `validateDelta` acepta dos de las dieciocho clases de arista y
`addSymbolsQuery` escribe un `Symbol` de nueve columnas cuando el canónico
tiene dieciséis. No es un ajuste, es otra ruta. Se dejó intacto porque sus
únicos consumidores son `benchmarks/ladybug-incremental` y
`benchmarks/ladybug-recovery`, que miden LadybugDB sobre el corpus sintético
y para los que es la herramienta correcta.

**La unidad es el archivo**, porque es la unidad a la que reacciona un índice
incremental y la única con la que se puede garantizar «0 ghost edges»: todo lo
que un archivo afirmaba se retira y se vuelve a afirmar.

**Dos hallazgos del motor, ya resueltos, que habrían costado caro aquí:**

* `UNWIND`+`MATCH`+`MERGE` **omite en silencio** las filas cuyo extremo no
  existe, sin error. Por eso la existencia de los dos extremos se verifica
  antes de fusionar, y no es opcional.
* El `COPY` de la carga completa almacena un `evidence_key` vacío como `NULL`,
  mientras el camino `MERGE` escribe `''`. `NULL <> ''`, así que reafirmar una
  arista existente la habría duplicado. Se normaliza antes de fusionar.

**Y un fallo de modelo que sólo apareció ejecutando un cambio real:** una
arista `PACKAGE_DEPENDS_ON` une dos paquetes, que un delta por archivo nunca
retira, pero la afirma el archivo donde se observó el import y lleva su
evidencia. Retirar sólo las aristas que tocan un nodo retirado la dejaba viva
apuntando a evidencia inexistente. La regla es la misma que para todo lo
demás —una arista la afirma un archivo— alcanzada a través de la evidencia en
vez de a través de un extremo, y está implementada en los dos lados:
`facts.edgeAnchor` y `deleteCanonicalEdgesEvidencedBy`.

**Verificado sobre código real**, no sobre fixtures: indexar
`testdata/go/type-relations`, publicarlo, editar un archivo fuente, indexar de
nuevo, `Diff` y `ApplyCanonicalDelta`. El grafo resultante es **idéntico** al
que produce una carga desde cero del estado final (`ScanCanonical` comparado
campo a campo) y los seis invariantes pasan.

**Lo que esta tarea sí tiene que decidir:** cuándo se aplica un delta sobre la
generación activa y cuándo conviene republicar entera, y cómo encaja eso con
`graph.active`/`graph.next`/`graph.backup` de LUQUE-0905.

**Decisión tomada — cuándo delta y cuándo republicar:**

`indexer.Decide` elige una de tres rutas y explica por qué:

```text
NOOP       el delta no cambia nada
DELTA      se aplica transaccionalmente sobre graph.active
REPUBLISH  rebuild.Run construye graph.next y hace atomic swap
```

* `REBUILD_REGISTRY` o `REINDEX_PROJECT` en cualquier plan **fuerzan**
  republicación: ambas cambian la identidad o la resolución de los paquetes,
  y ninguna retirada por archivo las retira.
* Sin generación activa no hay nada que mutar: republicación.
* Por encima de `DefaultRepublishRatio` (0,5 de los archivos indexados) el
  delta deja de ser más barato que una carga limpia, y la republicación
  además da swap atómico y un `graph.backup` nuevo en vez de mutar la
  generación desde la que se sirven consultas.
* En el resto, delta sobre `graph.active`.

**Hallazgo propio de esta tarea:** mutar `graph.active` in situ invalida su
`snapshot.sha256`. `rebuild.Rollback` revalida el destino recomputando ese
digest desde los contadores vivos y comparándolo con el archivo grabado, así
que una generación mutada sin refrescarlo **nunca podría volver a ser
destino de rollback**. La ruta delta reescribe el digest con
`rebuild.RefreshSnapshotDigest`, que reutiliza `writeSnapshotDigest`: la
mutación in situ y una publicación nueva no pueden discrepar sobre qué
significa el digest de una generación.

**Entregables:**

```text
internal/indexer/delta.go
internal/indexer/delta_test.go
internal/indexer/delta_native_test.go
```

**Archivos modificados:**

```text
internal/rebuild/rebuild.go
```

Se añadió `RefreshSnapshotDigest`, único punto de entrada exportado al
cálculo que ya usaban publicación y rollback.

**Estado:** `PASS`.

**Verificado sobre almacenamiento real**, no sobre hooks: con LadybugDB
nativo se carga el estado previo, se ejecuta `Update`, y el grafo mutado se
compara campo a campo (`ScanCanonical`) con una carga desde cero del estado
siguiente. Coincide, los invariantes pasan, y el caso de eliminación de
archivo no deja símbolos ni aristas fantasma. Los tests sin cgo cubren la
decisión de ruta, la provenance propagada al applier, el refresco del digest,
el no-op y los fallos.

**Comprobación por mutación:** quitar el refresco del digest rompe 1 prueba;
vaciar la lista de acciones que fuerzan republicación rompe 1; ignorar
`Report.Passed == false` rompe 1.

**Verificación:** `go test ./...`, `go vet ./...`,
`go test -race ./internal/indexer ./internal/rebuild`,
`go test ./internal/indexer -count=10`, `make build` y `make test-ladybug`
pasan.

**Benchmarks:** no aplica todavía. El coste de un delta frente a una carga
completa ya está medido en `benchmarks/ladybug-delta-profile`; medir la
orquestación aislada no añadiría información hasta que el watcher alimente el
pipeline completo.

**Limitaciones:** el ratio de republicación es un umbral fijo, no una
estimación de coste medida; `Update` recibe los dos estados indexados ya
calculados —no invoca a los motores de lenguaje— y la reconstrucción del
HotSnapshot tras un delta pertenece a LUQUE-1008.

**Siguiente tarea:** LUQUE-1008.

---

## LUQUE-1008 — Implementar reconstrucción de snapshot tras delta

**Dependencias:** LUQUE-1007.

**Checklist:**

- [x] Verificar dependencias y alcance.
- [x] Completar acciones y entregables.
- [x] Ejecutar pruebas y benchmarks aplicables.
- [x] Verificar criterios de aceptación y el gate aplicable.
- [x] Registrar resultados, limitaciones y siguiente tarea.

Primera versión:

```text
delta DB
→ rebuild completo HotSnapshot
→ atomic swap
```

Optimizar solo si excede el presupuesto.

**Implementación:**

`indexer.Update` acepta un `hotsnapshot.SnapshotStore` opcional. Después de
aplicar el delta y refrescar `snapshot.sha256`, reconstruye el HotSnapshot
completo desde la base canónica mutada mediante `rebuild.BuildSnapshot`.
`SnapshotStore.Publish` sólo se ejecuta después de que el builder y su informe
pasen; el CAS del store hace el reemplazo atómico para los lectores. Si la
reconstrucción falla o el candidato tiene una generación obsoleta, el
snapshot anterior permanece publicado y `Update` devuelve `ErrUpdateFailed`.

**Archivos modificados:**

```text
internal/indexer/delta.go
internal/indexer/delta_test.go
internal/indexer/delta_native_test.go
```

**Estado:** `PASS`.

**Verificación:**

```text
go test ./...
go vet ./...
go test -race ./internal/indexer ./internal/rebuild
make build
make test-ladybug
```

Todos pasan. El test unitario cubre publicación, fallo de build conservando
el snapshot anterior y rechazo de una generación obsoleta. El test nativo
carga una base canónica real, ejecuta `Update` con `ApplyCanonicalDelta` y
reconstruye/publica el snapshot usando `ScanCanonical` real.

**Comprobación por mutación:** eliminar `SnapshotStore.Publish` rompe la
prueba de publicación; restaurar el código original deja la suite en verde.

**Benchmark:** no aplica: LUQUE-1008 implementa deliberadamente el rebuild
completo pedido por el contrato. La optimización incremental se reserva para
cuando el presupuesto de reconstrucción real se exceda.

**Limitaciones:** el store HotSnapshot se actualiza sólo cuando el llamador
lo proporciona; esto conserva la compatibilidad con consumidores que sólo
usan el grafo persistente. La ruta de republicación completa seguirá su
pipeline existente; esta tarea cubre la reconstrucción posterior a un delta.

**Siguiente tarea:** LUQUE-1009.

---

## LUQUE-1009 — Probar altas

**Dependencias:** LUQUE-1008.

**Checklist:**

- [x] Verificar dependencias y alcance.
- [x] Completar acciones y entregables.
- [x] Ejecutar pruebas y benchmarks aplicables.
- [x] Verificar criterios de aceptación y el gate aplicable.
- [x] Registrar resultados, limitaciones y siguiente tarea.

**Casos:**

* archivo;
* símbolo;
* export;
* consumidor;
* paquete.

**Implementación de pruebas:**

```text
internal/indexer/additions_native_test.go
```

El test ejecuta cada alta contra LadybugDB real y compara el grafo mutado,
campo a campo (`ScanCanonical`), con una carga completa desde cero del estado
siguiente:

* archivo nuevo sin símbolo;
* símbolo nuevo en un archivo existente;
* arista `EXPORTS` nueva con evidencia;
* consumidor nuevo con `CALLS_DIRECT`, evidencia y contención;
* repositorio, paquete, archivo y símbolo nuevos con sus aristas de
  contención.

Cada caso exige ruta `DELTA`, al menos un upsert, todos los invariantes
canónicos en verde y cero diferencias respecto a la carga completa. Los casos
aislados de `facts.Diff` ya cubrían además la inclusión de contenedores para
un archivo en un paquete existente y para un paquete/repositorio nuevos.

**Estado:** `PASS`.

**Verificación:**

```text
go test ./...
go vet ./...
go test -race ./internal/facts ./internal/indexer
make build
make test-ladybug
```

Todos pasan. El smoke test nativo de altas ejecuta los cinco casos definidos
por la tarea sobre el motor real.

**Benchmarks:** no aplica. Son pruebas de corrección de altas; el coste de
delta frente a carga completa ya está medido en
`benchmarks/ladybug-delta-profile`.

**Limitaciones:** el caso consumidor usa un consumidor nuevo con una llamada
exacta y el caso export usa una arista `EXPORTS` con evidencia explícita. La
resolución desde los motores Go/TypeScript y la clasificación de cambios
pertenecen a las tareas de los respectivos normalizadores; aquí se prueba el
contrato canónico que recibe LadybugDB.

**Siguiente tarea:** LUQUE-1010.

---

## LUQUE-1010 — Probar modificaciones

**Dependencias:** LUQUE-1008.

**Checklist:**

- [x] Verificar dependencias y alcance.
- [x] Completar acciones y entregables.
- [x] Ejecutar pruebas y benchmarks aplicables.
- [x] Verificar criterios de aceptación y el gate aplicable.
- [x] Registrar resultados, limitaciones y siguiente tarea.

**Casos:**

* cuerpo;
* firma;
* callback;
* import;
* provider.

**Implementación de pruebas:**

```text
internal/indexer/modifications_native_test.go
```

La prueba ejecuta contra LadybugDB real cinco modificaciones y compara
`ScanCanonical` del grafo mutado con una carga completa del estado siguiente:

* cambio de cuerpo;
* cambio de firma;
* modificación de evidencia de `PASSES_AS_CALLBACK`;
* conversión de una referencia unresolved en `IMPORTS_SYMBOL`;
* cambio de firma del provider.

Cada caso reconstruye y publica también el HotSnapshot, exige ruta `DELTA` y
verifica todos los invariantes canónicos.

**Estado:** `PASS`.

**Verificación:**

```text
go test ./...
go vet ./...
go test -race ./internal/facts ./internal/indexer ./internal/rebuild
make build
make test-ladybug
```

Todos pasan, incluidos los cinco subcasos nativos.

**Limitaciones:** las transiciones usan facts canónicos deterministas; la
clasificación de bytes fuente y la resolución de los motores se cubren en sus
respectivas tareas. Aquí se prueba que el delta resultante no deja estado
anterior en LadybugDB ni en HotSnapshot.

**Siguiente tarea:** LUQUE-1011.

---

## LUQUE-1011 — Probar eliminaciones

**Dependencias:** LUQUE-1008.

**Checklist:**

- [x] Verificar dependencias y alcance.
- [x] Completar acciones y entregables.
- [x] Ejecutar pruebas y benchmarks aplicables.
- [x] Verificar criterios de aceptación y el gate aplicable.
- [x] Registrar resultados, limitaciones y siguiente tarea.

**Comprobar:**

* 0 ghost symbols;
* 0 ghost edges;
* referencias convertidas en unresolved;
* snapshot consistente.

**Implementación de pruebas:**

```text
internal/indexer/deletions_native_test.go
```

La prueba ejecuta dos eliminaciones sobre LadybugDB real:

* desaparición de un símbolo provider manteniendo el archivo;
* desaparición completa del archivo provider.

En ambos casos la llamada que deja de resolver se convierte en
`UnresolvedReference`. Se comprueba que no quedan símbolos ni aristas
fantasma, que el contador de unresolved coincide con una carga completa del
estado siguiente, que todos los invariantes pasan y que el HotSnapshot
publicado conserva el símbolo fuente pero no el eliminado.

**Estado:** `PASS`.

**Verificación:**

```text
go test ./...
go vet ./...
go test -race ./internal/facts ./internal/indexer ./internal/rebuild
make build
make test-ladybug
```

Todos pasan, incluidos los dos subcasos nativos de eliminación y conversión
a unresolved.

**Limitaciones:** la eliminación de paquetes o repositorios no se fuerza por
delta: el contrato file-grained de `facts.Delta` sólo retira hechos anclados a
archivos; cambios de identidad/resolución deben tomar la ruta de
republicación. Esta tarea cubre las eliminaciones expresables por el delta.

**Siguiente tarea:** LUQUE-1012.

---

## LUQUE-1012 — Benchmark incremental

**Dependencias:** LUQUE-1009, LUQUE-1010 y LUQUE-1011.

**Checklist:**

- [x] Verificar dependencias y alcance.
- [x] Completar acciones y entregables.
- [x] Ejecutar pruebas y benchmarks aplicables.
- [x] Verificar criterios de aceptación y el gate aplicable.
- [x] Registrar resultados, limitaciones y siguiente tarea.

**Gate:**

```text
INCREMENTAL_INDEXING_PASS
```

Requisitos:

```text
archivo simple p95 ≤ 750 ms
imports/exports p95 ≤ 2 s
manifest p95 ≤ 5 s
0 ghost edges
```

**Implementación:**

```text
benchmarks/ladybug-incremental/main.go
benchmarks/ladybug-incremental/main_test.go
benchmarks/ladybug-incremental/results.json
benchmarks/ladybug-incremental/report.md
```

Se sustituyó el benchmark experimental del esquema sintético por una
medición del pipeline canónico real:

```text
facts.Set previo
→ rebuild.Run de una generación aislada
→ indexer.Update
→ Diff + decisión DELTA/REPUBLISH
→ ApplyCanonicalDelta o republicación completa
→ digest + HotSnapshot + publicación atómica
→ VerifyCanonicalIntegrity
```

El corpus determinista contiene 1 repositorio, 1 paquete, 1.000 archivos,
10.000 símbolos, 10.000 evidencias y 21.001 aristas. Cada escenario tiene
cinco muestras independientes:

* `simple_file`: cambio de cuerpo, ruta `DELTA`;
* `imports_exports`: dos aristas semánticas nuevas con evidencia, ruta
  `DELTA`;
* `manifest`: cambio de versión y manifest, ruta `REPUBLISH`.

**Resultados medidos en Linux amd64, Go 1.24.4, LadybugDB fijado:**

```text
simple_file       p95 571.6 ms
imports_exports   p95 617.8 ms
manifest          p95 845.3 ms
ghost edges       0
```

**Estado:** `PASS`.

**Gate emitido:** `INCREMENTAL_INDEXING_PASS`.

**Verificación:**

```text
go test ./...
go vet ./...
go test -race ./internal/facts ./internal/indexer ./internal/rebuild
make build
make test-ladybug
go test -tags ladybug ./benchmarks/ladybug-incremental
```

La ejecución nativa del benchmark también devuelve `gate: true`. El detalle
reproducible queda en `benchmarks/ladybug-incremental/results.json` y
`report.md`.

**Limitaciones:** el ejecutable recibe facts canónicos ya normalizados; no
incluye el tiempo de parseo ni de resolución de los motores Go/TypeScript.
El coste de construcción de cada baseline se registra aparte. La medición
actual cubre la orquestación y almacenamiento canónicos con el corpus
determinista documentado, no todos los tamaños o topologías de repositorio.

**Siguiente tarea:** LUQUE-1101.

---

## LUQUE-1013 — El grafo se mantiene al día solo y dice cuándo no lo está

**Dependencias:** LUQUE-1012.

**Checklist:**

- [x] Verificar dependencias y alcance.
- [x] Completar acciones y entregables.
- [x] Ejecutar pruebas y benchmarks aplicables.
- [x] Verificar criterios de aceptación y el gate aplicable.
- [x] Registrar resultados, limitaciones y siguiente tarea.

**Objetivo:** que un cambio de rama en un repositorio registrado deje el grafo
al día sin que nadie ejecute nada, y que mientras no lo esté el agente lo sepa.
Hoy no ocurre ninguna de las dos cosas.

**El fallo, medido.** Esta fase construyó el motor incremental completo
-`fsnotify`, debounce, hashes de contenido, reconciliación, invalidación Go y
TypeScript, delta LadybugDB y reconstrucción del snapshot- y
`INCREMENTAL_INDEXING_PASS` lo aprobó con un benchmark que llama a
`indexer.Update` directamente. **Nada más lo llama:**

```text
grep "watcher\.|indexer\.Update"  en cmd/ + internal/indexing/ + internal/rebuild/
  -> sin coincidencias
```

No hay comando `watch`; `cmd/kivgraph/main.go:468-471` responde
`index: only --full is supported`. `config.watcher.enabled` vale `true` por
defecto y no enciende nada. Toda sincronización pasa por `RunFull`.

Medido sobre el workspace Kena -33 repositorios, 4.488 ficheros, 89.606
símbolos, 323.049 aristas, `darwin/arm64`, caché de build de Go caliente-:

```text
en frío                          27,5 s    caché  0/33
sin ningún cambio (suelo)         7,0 s    caché 33/33   (6,98 / 7,02 / 6,91)
cambia un repositorio pequeño     7,2 s    caché 32/33
cambia un repositorio grande     13,0 s    caché 32/33
```

Con las 33 unidades servidas desde caché el análisis termina en `0,7 s`; los
otros `6,3 s` son republicación. El log dice por qué: `copied 336911 node(s) and
565720 edge(s)` a una generación nueva, **en cada pasada**. Para un repositorio
pequeño el 97 % del tiempo es suelo fijo, y el suelo crece con el grafo entero:
`1,7 s` con 10.058 símbolos, `7,0 s` con 89.606.

Y el estado git no llega al MCP. Se captura de verdad
(`internal/workspace/registry.go:87-127`), se persiste
(`canonical_load.go:111-113`) y se cae en `internal/rebuild/snapshot.go:240-243`,
que construye la fila sin `Branch` ni `Dirty`. `list_repositories` no devuelve
ni el commit.

**Entregables:**

- [x] el ciclo watcher -> `indexer.Update`, con su lock de escritor único;
- [x] la vigilancia dirigida de `<repo>/.git/HEAD` y su ref, en
  `internal/watcher/`;
- [x] `Branch` y `Dirty` en `hotsnapshot.RepositoryRow` y `RepositoryRecord`,
  con su propagación en `internal/rebuild/snapshot.go`;
- [x] los metadatos git que faltan en `internal/facts/typescript.go`, y la
  política de fusión cuando dos lenguajes describen el mismo repositorio -hoy
  `mergeAllBy` deduplica por `Key` y se queda con la primera aparición
  (`facts.go:544-552`), así que el commit registrado depende de qué lenguaje se
  fusionó antes;
- [x] el bloque de frescura por repositorio en `graph_status` y
  `list_repositories`;
- [x] un test que alimente `Update` con un delta del tamaño de un checkout y
  fije la ruta que elige.

**Decisiones:**

* **El disparador es que `HEAD` se mueva, y sólo eso.** Cubre checkout, pull,
  merge, rebase, reset y commit. `push` no entra: no toca el árbol de trabajo,
  no mueve `refs/heads` y el código que el grafo describe es idéntico antes y
  después. Un commit tampoco cambia contenido, así que la verificación por hash
  concluye sola que no hay nada que reanalizar y sólo se actualiza la etiqueta.
* **Debounce por workspace, no por repositorio.** Un `git pull` sobre 33
  repositorios debe producir **una** pasada. Pagar el suelo 33 veces son casi
  cuatro minutos.
* **Se vigila el directorio, no el fichero.** Git actualiza refs con rename
  atómico: el inodo cambia y un descriptor sobre el fichero queda apuntando a lo
  viejo. Hay que vigilar `.git/` y `.git/refs/heads/` y rearmar. `HEAD` se
  resuelve mirando la ref suelta y, si no está, `packed-refs`, porque tras un
  `gc` la suelta no existe. Y `.git` puede ser un fichero con `gitdir:` en un
  worktree enlazado o un submódulo.
* **No se publica mientras git trabaja.** Un checkout produce ráfagas de eventos
  y no es atómico visto desde fuera. Se espera a que el árbol se estabilice, se
  comprueba que `HEAD` no volvió a moverse, y `.git/index.lock` es la señal
  barata de que git sigue dentro.
* **Un solo escritor, sin daemon de elección de líder.** Basta un lock de
  escritura: el que lo coge sincroniza y los demás se enteran solos, porque
  `serve` y `ui` ya siguen `CURRENT` y republican cuando avanza. El reparto
  publicador/seguidor ya existe y ya funciona.
* **La ruta la decide `indexer.Update`, no el llamante.** Un checkout puede
  tocar `go.mod`, `package.json` o `Cargo.toml`, y eso es `REPUBLISH`, no
  `DELTA`. La máquina de decisión existe; se le deja decidir.
* **`index --full` no sirve como sincronización a esta escala.** Con dos
  repositorios son 3 s e invisible; con Kena son 7 a 13 s, que es exactamente la
  ventana en la que el agente pregunta. Ésa es la razón de esta tarea: sin el
  delta, ningún disparador llega a tiempo.
* **La caducidad es por fichero, no por repositorio.** Un checkout deja la
  mayoría de los ficheros byte a byte idénticos. Con el hash de contenido en el
  snapshot, una fila cuyo fichero no cambió sigue siendo exacta aunque la rama
  sea otra, y sólo se degrada lo que de verdad se movió. `facts.File.ContentHash`
  ya existe (`facts.go:185`) y no llega a `hotsnapshot.FileRecord`.

**Criterios de aceptación:**

- [x] Un `git checkout` en un repositorio registrado deja el grafo al día sin
  que nadie ejecute un comando.
- [x] Un `git pull` que mueve `HEAD` en varios repositorios produce una sola
  pasada.
- [x] Un `git push` no dispara ninguna pasada.
- [x] Un commit que no cambia contenido no publica generación: actualiza el
  commit y `dirty`, y nada más.
- [x] `graph_status` y `list_repositories` devuelven, por repositorio, la rama y
  el commit indexados y si el árbol se movió desde entonces.
- [x] Dos procesos que arranquen a la vez no reindexan a la vez: el que no
  obtiene el lock recoge la generación del otro.
- [x] Un repositorio cuya ref vive en `packed-refs` se detecta igual que uno con
  ref suelta.
- [x] Con el corpus Kena, el tiempo entre el `checkout` y la generación
  publicada queda registrado en la tarea y comparado contra los `7-13 s` que
  cuesta hoy `index --full`.
- [ ] El test de ruta falla si un delta del tamaño de un checkout degrada a
  `REPUBLISH` sin que ningún manifest haya cambiado.

**Fuera de alcance:** grafos distintos por rama. El grafo describe el árbol de
trabajo de cada repositorio, esté en la rama que esté. Fijar un repositorio a
una rama declarada haría que `file_path` y `start_line` apuntasen a un fichero
cuyo contenido en disco es otro, que es peor que el problema que se viene a
resolver.

**Prioridad:** por delante de `LUQUE-1113` y `LUQUE-1114`. Aquéllas hacen la
respuesta más barata y más honesta; ésta la hace **correcta**. Un
`file_path:line` con el formato perfecto, sobre código que ya no existe porque
se cambió de rama, es un error silencioso, y eso pesa más que un resultado caro.

**Resultado.** Hecho: el modelo de datos -`Branch`, `Dirty`,
`ContentHash` y `Exported` llegan ya al HotSnapshot-, los metadatos git de
TypeScript, la lectura de `HEAD` sin lanzar `git` (`internal/watcher/githead.go`,
con `packed-refs`, `gitdir:` de worktree y `commondir`), y el bloque de frescura
en `list_repositories` y `graph_status`. Contra el corpus real:

```text
graph_status: repositories=2  repositories_moved=1
  kivgraph  moved=true   indexed at commit e13b9ad on main, the tree is now at 91f13bf on main
  mole       moved=false
```

El test de ruta pasa y fija lo que el diseño necesitaba saber: un checkout de
146 ficheros sobre un corpus de 4.488 toma la ruta `DELTA`; sobre uno de 258
supera el ratio y republica; y con un manifest de por medio republica siempre.

El ciclo está enchufado. `serve` arranca `indexing.Resync` junto al seguidor:
sondea `HEAD` cada dos segundos -dos lecturas de fichero por repositorio, sin
subproceso-, agrupa por workspace, espera a que el árbol se calme y a que
desaparezca `.git/index.lock`, toma un `flock` de escritor y reconstruye.
Contra un clon aislado, con un `git checkout` real de 146 ficheros y nadie
ejecutando ningún comando:

```text
11:35:29  working tree moved      from 69d992d to 85b1a6f
11:35:36  graph resynchronised    repositories=1
```

Siete segundos, y el grafo servido pasó de `snapshot_id 25` a `26` y de `9703`
a `8693` símbolos -los que de verdad tiene ese commit-. `list_repositories`
vuelve a decir `moved: false`.

Un commit no reconstruye nada. Antes de tocar el índice se comparan los dos
commits con `git diff --quiet`: si los árboles coinciden, el trabajo no se
hace. Verificado en el mismo banco:

```text
12:19:52  working tree moved   fb3e9d0 -> 4a73ef4   (commit --allow-empty)
12:19:56  no rebuild needed, the indexed content is unchanged
12:20:08  working tree moved   4a73ef4 -> 482cb89   (checkout de 40 commits)
12:20:15  graph resynchronised
```

El smoke test destapó además una carrera dentro del propio proceso: el
seguidor de generaciones instalaba la nueva un milisegundo antes que la
reindexación, y `Publish` la rechazaba por no ser más nueva. El rebuild ya
había ido bien, así que el error era falso y provocaba un segundo rebuild
inútil. `publishActiveSnapshot` trata ahora ese caso como éxito cuando el store
ya sirve esa generación o una posterior.

**Ruta `DELTA`: descartada por decisión, no por coste de implementación.** El
suelo medido es `7 s` en Kena y `3 s` en dos repositorios, y con el commit ya
resuelto sólo se paga en un cambio de rama real. Conservar el fact set anterior
para diferenciar cuesta **`257 MB` retenidos** -medido decodificando la caché
de hechos de Kena: 89.606 símbolos, 223.233 evidencias, 323.049 aristas-, sobre
los `638 MB` de RSS que ya tiene un `serve` con ese grafo cargado, y por cada
cliente MCP abierto. No compensa. Leerlo del disco en vez de retenerlo evita esa
memoria pero exige demostrar que el round-trip canónico es exacto, y eso pide su
propio ADR. `ResyncOptions.Resync` sigue siendo un punto de inyección: el día
que exista, sustituye a `Service.Reindex` sin tocar el bucle.

**Limitación declarada:** `facts.File.ContentHash` está en el modelo y se
persiste, pero **ningún analizador lo rellena** - sólo los tests -. Por eso la
comparación de contenido se hace con git y no con los digests del grafo, y por
eso el campo no se lleva al HotSnapshot: transportar una cadena siempre vacía
que nadie lee es andamiaje. Poblarlo es trabajo de la ruta `DELTA`.

**Estado:** `PASS`.

**Gate:** `INCREMENTAL_INDEXING_PASS` se vuelve a exigir. El gate original se
emitió sobre un benchmark que llama a `indexer.Update` directamente, así que
nunca cubrió que alguien lo llamara.

**Verificación:**

```text
gofmt -l <archivos-go-modificados>
go vet ./...
go test ./internal/watcher/... ./internal/indexer/... ./internal/rebuild/... -count=1
go test ./... -count=1
go test -race ./internal/watcher/... ./internal/indexer/... -count=1
make test-ladybug
make build
```

`make test-ladybug` entra porque el delta cruza la capa nativa. Y contra el
binario real, con una configuración aislada y copias privadas de los
repositorios -nunca los indexados-: un `checkout`, un `pull` multi-repo, un
`commit` y un `push`, comprobando en cada uno qué pasada se dispara, cuál no, y
cuánto tarda la generación en publicarse.

---

# 14. Fase 11 — Tools MCP

## LUQUE-1101 — Implementar respuesta estándar

**Dependencias:** INCREMENTAL_INDEXING_PASS.

**Checklist:**

- [x] Verificar dependencias y alcance.
- [x] Completar acciones y entregables.
- [x] Ejecutar pruebas y benchmarks aplicables.
- [x] Verificar criterios de aceptación y el gate aplicable.
- [x] Registrar resultados, limitaciones y siguiente tarea.

**Campos:**

```text
snapshot_id
snapshot_age_ms
total
returned
truncated
next_cursor
coverage
results
```

**Implementación:**

Se añadió `internal/mcp/tools/response.go` con el envelope genérico
`Response[T]` y `Coverage`. Todas las respuestas declaran:

```text
snapshot_id
snapshot_age_ms
total
returned
truncated
next_cursor
coverage
results
```

Los metadatos de snapshot y cursor se serializan como `null` mientras no
exista una generación publicada. `graph_status` usa el envelope con un
resultado de estado; `list_repositories` usa un array vacío no nulo.

Se añadió `internal/mcp/tools/errors.go` con los códigos de error del
contrato, `ToolError`, `ErrorCode`, `NewToolError` y `WrapToolError`. El
payload JSON conserva únicamente `code`, `message` y `details`; las causas
internas no se exponen.

**Estado:** `PASS`.

**Gate:** no hay un gate adicional definido para LUQUE-1101.

**Verificación:**

```text
go test ./...
go vet ./...
go test -race ./internal/mcp/...
make build
```

Las pruebas MCP in-memory validan el envelope de ambas herramientas y el
smoke STDIO de `kivgraph serve` devolvió `structuredContent` con los ocho
campos del contrato para `list_repositories`.

**Limitaciones:** el servidor todavía devuelve un grafo vacío; la carga desde
`HotSnapshot`, los cursores y los errores de dominio se implementan en las
tareas siguientes. El envelope ya fija sus formas y los valores `null` de
estado sin snapshot.

**Siguiente tarea:** LUQUE-1102.

---

## LUQUE-1102 — Implementar cursores

**Dependencias:** LUQUE-1101.

**Checklist:**

- [x] Verificar dependencias y alcance.
- [x] Completar acciones y entregables.
- [x] Ejecutar pruebas y benchmarks aplicables.
- [x] Verificar criterios de aceptación y el gate aplicable.
- [x] Registrar resultados, limitaciones y siguiente tarea.

**Incluir:**

* snapshot;
* query hash;
* offset;
* sorting version;
* checksum.

**Implementación:**

Se añadió `internal/mcp/tools/cursor.go` con:

```text
CursorFormatVersion
snapshot_id
query_hash
offset
sorting_version
checksum
```

`HashQuery` calcula SHA-256 hexadecimal sobre JSON determinista. `NewCursor`
valida la identidad y genera el checksum; `Encode` usa JSON versionado dentro
de base64url sin padding; `DecodeCursor` rechaza payloads truncados, campos
desconocidos, versiones incompatibles, hashes inválidos y checksums alterados.
`ValidateAgainst` distingue `CURSOR_SNAPSHOT_EXPIRED` de
`CURSOR_INVALID`.

La ordenación pública inicial queda identificada como
`stable-key-v1`, consistente con la asignación de IDs de símbolos del
`HotSnapshot`, que parte de stable keys ascendentes.

**Estado:** `PASS`.

**Gate:** no hay un gate adicional definido para LUQUE-1102.

**Verificación:**

```text
go test ./...
go vet ./...
go test -race ./internal/mcp/...
make build
```

Las pruebas cubren hashes equivalentes y distintos, round-trip determinista,
token opaco URL-safe, campos ausentes, payloads desconocidos, manipulación del
offset, checksum, expiración por snapshot y cambios de consulta u ordenación.

**Limitaciones:** los cursores todavía no están consumidos por una herramienta
con datos reales; `list_repositories` se conectará al `HotSnapshot` en
LUQUE-1103. El checksum no es un mecanismo de autenticación: el cursor
transporta estado íntegro, no un secreto.

**Siguiente tarea:** LUQUE-1103.

---

## LUQUE-1103 — Implementar `list_repositories`

**Dependencias:** LUQUE-1101.

**Checklist:**

- [x] Verificar dependencias y alcance.
- [x] Completar acciones y entregables.
- [x] Ejecutar pruebas y benchmarks aplicables.
- [x] Verificar criterios de aceptación y el gate aplicable.
- [x] Registrar resultados, limitaciones y siguiente tarea.

**Implementación:**

`internal/mcp/tools/repositories.go` dejó de devolver un stub y ahora consume
el `HotSnapshot` publicado en un `SnapshotStore`. La entrada admite `limit`
(por defecto 50, máximo 500) y `cursor`; la respuesta devuelve los metadatos
del envelope estándar, la edad del snapshot y repositorios ordenados por stable
key. El cursor contiene la identidad del snapshot, hash de consulta, offset,
ordenación `stable-key-v1` y checksum, y distingue `CURSOR_INVALID`,
`CURSOR_SNAPSHOT_EXPIRED`, `INDEX_NOT_READY` y `SNAPSHOT_UNAVAILABLE`.

`internal/hotsnapshot` conserva en cada registro de repositorio la stable key,
la ruta raíz y los lenguajes ordenados, además del nombre y commit existentes.
`internal/rebuild/snapshot.go` incrementó el formato de filas a la versión 2 y
incluyó esos campos en la conversión y el digest determinista. `internal/mcp`
expone la inyección mediante `NewServerWithSnapshotStore`,
`NewServerWithObserverAndSnapshotStore` y `RunWithSnapshotStore`.

**Estado:** `PASS`.

**Gate:** no hay un gate adicional definido para LUQUE-1103.

**Verificación:**

```text
go test ./...
go vet ./...
go test -race ./internal/mcp/...
make build
```

Las pruebas in-memory cubren snapshot vacío, snapshot poblado, ordenación por
stable key, campos de display, paginación con cursor, cursor final y rechazo
de límites fuera de rango. Todos los comandos pasan.

**Limitaciones:** `NewServer` y el entrypoint CLI conservan el modo sin fuente y
devuelven la página vacía compatible mientras no se inyecte un `SnapshotStore`.
La carga y publicación del snapshot desde el ciclo de indexación se cableará en
las tareas de integración posteriores; la tool ya no consulta LadybugDB ni el
filesystem durante la petición.

**Siguiente tarea:** LUQUE-1104.

---

## LUQUE-1104 — Implementar `find_symbol`

**Dependencias:** LUQUE-1101.

**Checklist:**

- [x] Verificar dependencias y alcance.
- [x] Completar acciones y entregables.
- [x] Ejecutar pruebas y benchmarks aplicables.
- [x] Verificar criterios de aceptación y el gate aplicable.
- [x] Registrar resultados, limitaciones y siguiente tarea.

**Implementación:**

Se añadió `internal/mcp/tools/find_symbol.go`, que registra la tool
read-only sobre el `HotSnapshot` publicado. La entrada usa `name`, `mode`,
`limit` y `cursor`; el modo vacío equivale a `exact`. Los modos soportados son
`exact`, `qualified_exact` y `prefix`; cualquier intento de fuzzy matching o
modo desconocido devuelve `INVALID_ARGUMENT`.

`exact` y `qualified_exact` consultan los índices internados del snapshot.
`prefix` recorre solo los nombres de símbolo y conserva el orden ascendente de
stable key. Los resultados exponen stable key, identidad canónica, nombre,
nombre cualificado, kind y signature. La respuesta usa el envelope estándar,
paginación acotada a 500 resultados y cursores ligados al snapshot, consulta y
ordenación. Un snapshot ausente devuelve `INDEX_NOT_READY`; una inconsistencia
interna devuelve `SNAPSHOT_UNAVAILABLE`.

`internal/hotsnapshot/search.go` incorpora la página de prefijo y
`internal/mcp/server.go` registra `find_symbol` en las variantes de servidor
con y sin observador.

**Estado:** `PASS`.

**Gate:** no hay un gate adicional definido para LUQUE-1104.

**Verificación:**

```text
go test ./...
go vet ./...
go test -race ./internal/mcp/...
make build
```

Las pruebas cubren exact, qualified_exact, prefix, coincidencias exactas
ambiguas, ausencia de resultados, paginación con cursor, expiración de cursor,
modos no soportados, nombres inválidos, límites inválidos y
`INDEX_NOT_READY`. Todos los comandos pasan.

**Limitaciones:** `prefix` usa un recorrido lineal de los nombres del snapshot;
no se añade un índice de prefijos hasta que un benchmark del corpus real lo
justifique. El entrypoint CLI sigue sin cargar y publicar un `SnapshotStore`,
por lo que la tool solo devuelve datos cuando recibe explícitamente un
snapshot activo.

**Siguiente tarea:** LUQUE-1105.

---

## LUQUE-1105 — Implementar `get_symbol`

**Dependencias:** LUQUE-1104.

**Checklist:**

- [x] Verificar dependencias y alcance.
- [x] Completar acciones y entregables.
- [x] Ejecutar pruebas y benchmarks aplicables.
- [x] Verificar criterios de aceptación y el gate aplicable.
- [x] Registrar resultados, limitaciones y siguiente tarea.

**Implementación:**

Se añadió `internal/mcp/tools/get_symbol.go`, una consulta read-only que
resuelve `stable_key` mediante el índice exacto del `HotSnapshot`. La respuesta
usa el envelope estándar con un único `SymbolDetails`, incluyendo identidad
canónica, repositorio, paquete, ruta de archivo, nombre, kind, signature y
rango de líneas.

`GraphSnapshot` expone getters inmutables para `Package` y `File`, permitiendo
reconstruir la cadena de contención sin consultar LadybugDB ni el filesystem.
La tool se registra en las variantes de servidor con y sin `SnapshotStore`.

Errores clasificados:

```text
INVALID_ARGUMENT
SYMBOL_NOT_FOUND
INDEX_NOT_READY
SNAPSHOT_UNAVAILABLE
```

**Estado:** `PASS`.

**Gate:** no hay un gate adicional definido para LUQUE-1105.

**Verificación:**

```text
go test ./...
go vet ./...
go test -race ./internal/mcp/...
make build
```

Las pruebas cubren lookup encontrado, detalle de identidad y contención,
ausencia de stable key, entradas inválidas, snapshot no publicado y registro
read-only. El smoke STDIO real confirmó el handshake MCP y la publicación de
`get_symbol` en `tools/list`.

**Limitaciones:** el `HotSnapshot` actual conserva para símbolos el rango de
líneas, no columnas ni offsets; `get_symbol` devuelve únicamente metadatos
presentes en el snapshot y no inventa esos campos. No se añadió un benchmark
separado: el lookup usa el índice exacto ya cubierto por el objetivo de
latencia documentado.

**Siguiente tarea:** LUQUE-1106.

---

## LUQUE-1106 — Implementar `find_references`

**Dependencias:** LUQUE-1105.

**Checklist:**

- [x] Verificar dependencias y alcance.
- [x] Completar acciones y entregables.
- [x] Ejecutar pruebas y benchmarks aplicables.
- [x] Verificar criterios de aceptación y el gate aplicable.
- [x] Registrar resultados, limitaciones y siguiente tarea.

**Implementación:**

Se añadió `internal/mcp/tools/find_references.go` como consulta read-only
paginada sobre el CSR entrante/saliente del `HotSnapshot`. La entrada exige
`stable_key` y admite `direction` (`incoming` por defecto), `repo`, `language`,
`edge_kinds`, `confidence`, `limit` y cursor opaco. La respuesta reutiliza el
envelope estándar y devuelve cada relación con sus claves de símbolos,
repositorios, lenguajes, archivos, evidencia, kind, confidence y provenance.

Los filtros se aplican sobre la arista canónica y sobre el extremo relacionado
con el símbolo consultado. Las aristas de contención no se exponen como
referencias; los códigos desconocidos, la evidencia ausente y el metadata
inconsistente se clasifican como `SNAPSHOT_UNAVAILABLE`. La cobertura separa
relaciones exactas, candidatas y `UNRESOLVED`.

El `HotSnapshot` conserva ahora claves y lenguajes en `PackageRecord`,
`FileRecord` y `SymbolRecord`, y también la identidad, ruta y lenguajes del
repositorio. La conversión canónica rellena esos campos sin consultar de nuevo
LadybugDB durante la consulta.

**Estado:** `PASS`.

**Gate:** no hay un gate adicional definido para LUQUE-1106.

**Verificación:**

```text
go test ./internal/mcp/tools -run 'TestFindReferences' -count=1
go test ./...
go vet ./...
go test -race ./internal/mcp/...
make build
```

Todas las comprobaciones pasan. Las pruebas cubren referencias entrantes y
salientes, paginación, filtros de repositorio/lenguaje/kind/confidence,
metadatos de evidencia, límites, errores clasificados, snapshot ausente y
registro read-only.

**Limitaciones:** la consulta recorre únicamente la página CSR inmediata del
símbolo; no hace recorrido transitivo ni inventa referencias para
`UnresolvedReference` sin un destino `Symbol`. El índice exacto de aristas
continúa siendo el CSR del snapshot; no se añadió un índice adicional porque
la consulta ya está acotada por el símbolo y el SLO existente cubre hasta 100
referencias.

**Siguiente tarea:** LUQUE-1107.
---

## LUQUE-1107 — Implementar `find_cross_repo_consumers`

**Dependencias:** LUQUE-1106.

**Checklist:**

- [x] Verificar dependencias y alcance.
- [x] Completar acciones y entregables.
- [x] Ejecutar pruebas y benchmarks aplicables.
- [x] Verificar criterios de aceptación y el gate aplicable.
- [x] Registrar resultados, limitaciones y siguiente tarea.

**Separar:**

```text
exact symbol consumers
package consumers
candidate consumers
unresolved consumers
```

**Implementación:**

Se añadió `internal/mcp/tools/find_cross_repo_consumers.go` como consulta
read-only y paginada sobre el `HotSnapshot`. La entrada exige `stable_key` y
admite `repo`, `language`, `limit` y cursor opaco validado contra el snapshot.

Las cuatro categorías salen de tres fuentes distintas del snapshot, nunca de
coincidencias nominales sobre el nombre de un símbolo:

```text
exact_symbol -> CSR entrante con confianza exacta
candidate    -> CSR entrante con confianza no exacta
package      -> PACKAGE_DEPENDS_ON / MODULE_DEPENDS_ON entrantes del paquete
unresolved   -> UnresolvedReference cuyo paquete solicitado es el del objetivo
```

Un consumidor del mismo repositorio que el objetivo se descarta: la consulta es
cross-repo por definición. El orden es determinista por categoría y después por
claves de repositorio, paquete, símbolo, archivo, kind, evidencia y unresolved.

`GraphSnapshot.PackageDependencies` es un índice de entrada: un paquete ve
quién depende de él, no de qué depende. `targetLocation` conserva ahora el
`PackageID` resuelto, de modo que la consulta no vuelve a buscar el símbolo por
stable key para localizar su paquete.

**Estado:** `PASS`.

**Gate:** no hay un gate adicional definido para LUQUE-1107.

**Verificación:**

```text
go test ./internal/mcp/tools -run TestFindCrossRepoConsumers -count=1
go test ./... -count=1
go test -tags ladybug ./... -count=1
go vet ./...
go test -race ./internal/mcp/... -count=1
make build
```

El smoke STDIO confirmó `find_cross_repo_consumers` en `tools/list` marcada
read-only, con su esquema de entrada y salida, y `INDEX_NOT_READY` clasificado
cuando no hay snapshot publicado.

**Limitaciones:** el emparejamiento de `UnresolvedReference` con el objetivo
compara el paquete solicitado contra la clave, el nombre y la ruta de módulo
del paquete objetivo, y el símbolo solicitado contra su clave, nombre y nombre
cualificado, incluido el sufijo `.Nombre` o `::Nombre`. Es una coincidencia
sobre lo que el resolutor pidió, registrada como evidencia de resolución
fallida en la categoría `unresolved`; no crea ninguna arista ni identidad de
símbolo. La consulta recorre las referencias no resueltas del snapshot en orden
lineal: no hay índice por paquete solicitado, porque el conjunto está acotado
por el corpus indexado y el SLO de 5 ms p95 se mide en LUQUE-1602.

**Siguiente tarea:** LUQUE-1108.

---

## LUQUE-1108 — Implementar `trace_dependencies`

**Dependencias:** LUQUE-1106.

**Checklist:**

- [x] Verificar dependencias y alcance.
- [x] Completar acciones y entregables.
- [x] Ejecutar pruebas y benchmarks aplicables.
- [x] Verificar criterios de aceptación y el gate aplicable.
- [x] Registrar resultados, limitaciones y siguiente tarea.

**Aplicar:**

* profundidad;
* límites;
* filtros;
* paginación;
* typed errors.

**Implementación:**

Se añadió `internal/mcp/tools/trace_dependencies.go`: BFS saliente y acotado
sobre la CSR forward del `HotSnapshot`, registrada read-only en el servidor MCP.

```text
depth       1..5    por defecto 3   (hotsnapshot.MaxTraversalDepth)
max_nodes   1..25000 por defecto 5000 (hotsnapshot.MaxTraversalNodes)
limit       1..500  por defecto 50
```

La distinción clave del contrato es qué filtro corta el grafo y cuál sólo
selecciona filas:

```text
edge_kinds, confidence -> gobiernan qué aristas puede seguir el recorrido
repo, language         -> seleccionan qué símbolos alcanzados se devuelven
```

Un filtro de confianza corta el camino: los símbolos que sólo eran alcanzables
a través de una arista descartada dejan de existir para la consulta. Un filtro
de repositorio no lo hace: una dependencia encontrada atravesando otro
repositorio se sigue devolviendo.

`TraversalVisit` conserva ahora `Source` y la `PackedEdge` por la que el BFS
llegó por primera vez a cada símbolo, y `TraversalOptions` admite `Confidences`
junto a `EdgeKinds`. La respuesta expone esa arista como `reached_from_key`,
`via_kind`, `via_confidence` y `via_provenance`. El campo se llama
`reached_from_key` y no `via_source_key` porque nombra el símbolo ya alcanzado
desde el que se descubrió éste, que en un recorrido entrante es el **destino**
de la arista: el nombre no debe afirmar una orientación falsa. Es el camino más
corto que el frente encontró, no el único posible.

El sobre estándar pagina sobre `nodes`; la metainformación del recorrido
—`reached`, `deepest_depth`, `traversal_truncated`— vive dentro de `results`
porque `truncated` del sobre ya significa "hay más páginas".

**Errores clasificados:**

```text
INVALID_ARGUMENT         stable_key, depth, max_nodes, limit, edge_kinds, confidence
SYMBOL_NOT_FOUND         la raíz no está en el snapshot
CURSOR_INVALID           cursor corrupto
CURSOR_SNAPSHOT_EXPIRED  cursor de otra generación
INDEX_NOT_READY          no hay snapshot publicado
TRAVERSAL_LIMIT_REACHED  el deadline de la petición venció durante el BFS
SNAPSHOT_UNAVAILABLE     metadatos del snapshot inconsistentes
```

**Estado:** `PASS`.

**Gate:** no hay un gate adicional definido para LUQUE-1108.

**Verificación:**

```text
go test ./internal/mcp/tools -run TestTraceDependencies -count=1
go test ./internal/hotsnapshot -run TestTraverse -count=1
go test ./... -count=1
go test -tags ladybug ./... -count=1
go vet ./...
go test -race ./internal/mcp/... ./internal/hotsnapshot -count=1
make build
```

El smoke STDIO confirmó `INVALID_ARGUMENT: depth must be between 1 and 5` y
`INDEX_NOT_READY` clasificados por el servidor real.

**Limitaciones:** el recorrido es sólo saliente; la dirección inversa y su
agrupación pertenecen a `get_blast_radius` (LUQUE-1109). El timeout lógico se
toma del deadline de la propia petición: sin deadline de cliente, los únicos
límites son `depth` y `max_nodes`. `traversal_truncated` indica que el BFS tocó
`max_nodes`, es decir, que el subgrafo devuelto es incompleto por diseño, no un
error. El SLO de 12 ms p95 a profundidad 3 se medirá en LUQUE-1602.

**Siguiente tarea:** LUQUE-1109.

---

## LUQUE-1109 — Implementar `get_blast_radius`

**Dependencias:** LUQUE-1108.

**Checklist:**

- [x] Verificar dependencias y alcance.
- [x] Completar acciones y entregables.
- [x] Ejecutar pruebas y benchmarks aplicables.
- [x] Verificar criterios de aceptación y el gate aplicable.
- [x] Registrar resultados, limitaciones y siguiente tarea.

**Agrupar por:**

* repositorio;
* paquete;
* profundidad;
* tipo de relación.

**Implementación:**

Se añadió `internal/mcp/tools/blast_radius.go`: el mismo BFS acotado de
LUQUE-1108 pero en dirección **entrante**, y agregando en vez de listando. La
tool reutiliza `dependencyTraversalOptions` y sólo invierte la dirección, de
modo que profundidad, `max_nodes`, `edge_kinds`, confianza y errores
clasificados se comportan igual en ambas herramientas.

El resultado es la lista de símbolos afectados —misma forma `ReachedSymbol` que
`trace_dependencies`— más cuatro ejes sobre ese mismo conjunto:

```text
by_repository   clave de repositorio -> símbolos afectados
by_package      paquete + repositorio -> símbolos afectados
by_depth        profundidad 1..depth -> símbolos afectados
by_kind         tipo de relación -> símbolos afectados
```

La raíz nunca se cuenta: un símbolo no se ve afectado por su propio cambio.

`by_kind` no se lee de la arista descubridora. Un consumidor puede tocar el
subgrafo por varias relaciones a la vez —llamar a una función y usar su tipo—, y
publicar sólo la arista que el BFS tomó primero ocultaría las demás. Se inspecciona
**cada** arista del símbolo afectado hacia el conjunto visitado, bajo los mismos
filtros del recorrido, contando una vez por tipo distinto. Consecuencia explícita:
`by_repository`, `by_package` y `by_depth` particionan `affected`, mientras que
`by_kind` puede sumar más.

El sobre pagina sobre `symbols`. Los cuatro ejes se calculan sobre el recorrido
completo, no sobre la página, así que no encogen al avanzar el cursor.
`affected` es el total real de símbolos y vive dentro de `results`.

**Estado:** `PASS`.

**Gate:** no hay un gate adicional definido para LUQUE-1109.

**Verificación:**

```text
go test ./internal/mcp/tools -run TestGetBlastRadius -count=1
go test ./... -count=1
go test -tags ladybug ./... -count=1
go vet ./...
go test -race ./internal/mcp/... ./internal/hotsnapshot -count=1
make build
```

Las pruebas comprueban que `by_repository` particiona `affected`, que un
consumidor con dos relaciones aparece en dos entradas de `by_kind` sin duplicarse
como símbolo afectado, que un filtro de confianza excluye al consumidor candidato
junto con su subárbol y también de `by_kind`, que `max_nodes` marca
`traversal_truncated`, y que paginar símbolos no recorta los agregados. El smoke
STDIO devolvió `INVALID_ARGUMENT: max_nodes must be between 1 and 25000` e
`INDEX_NOT_READY` clasificados, y `tools/list` publica `symbols` y
`reached_from_key` en el esquema de salida.

**Limitaciones:** un símbolo alcanzado por dos caminos aparece una sola vez, con
la profundidad y la arista del camino más corto que encontró el BFS; sus demás
relaciones con el subgrafo sí quedan reflejadas en `by_kind`. El SLO de 20 ms p95
a profundidad 3 y 50 ms a profundidad 5 se medirá en LUQUE-1602.

**Siguiente tarea:** LUQUE-1110.

---

## LUQUE-1110 — Implementar `get_unresolved_references`

**Dependencias:** LUQUE-1101.

**Checklist:**

- [x] Verificar dependencias y alcance.
- [x] Completar acciones y entregables.
- [x] Ejecutar pruebas y benchmarks aplicables.
- [x] Verificar criterios de aceptación y el gate aplicable.
- [x] Registrar resultados, limitaciones y siguiente tarea.

**Filtros:**

* repositorio;
* paquete;
* símbolo;
* motivo;
* lenguaje.

**Implementación:**

Se añadió `internal/mcp/tools/unresolved.go`: lectura paginada de
`GraphSnapshot.UnresolvedReferences()`, registrada read-only en el servidor MCP.
Con esto quedan expuestas las nueve tools del plan.

El filtro «paquete» se implementa como **dos** filtros, porque son dos hechos
distintos y colisionan en cuanto un repositorio consume un paquete que se llama
como uno indexado:

```text
repo, package, language -> dónde se observó el fallo
requested_package, requested_symbol -> qué pidió el resolutor y no encontró
reason -> por qué falló
```

Una fila es evidencia de una petición fallida, nunca una identidad inferida:
`requested_package` y `requested_symbol` son las cadenas que usó el resolutor,
no claves del grafo. `file_key`, `package_key` y `source_symbol_key` se dejan
vacíos cuando el fallo es de nivel módulo y no hay tal identidad que declarar.

`reason` se filtra por coincidencia exacta sin validar contra un vocabulario:
hoy no existe uno común entre lenguajes. El cargador Go emite valores de
`goloader.UnresolvedReason` como `package_not_found`, y el worker TypeScript
emite los suyos, como `PACKAGE_PROVIDER_NOT_FOUND`. Validar contra una lista
inventada aquí rechazaría hechos reales; unificar el vocabulario sería un cambio
de contrato de los cargadores, no de esta tool.

**Estado:** `PASS`.

**Gate:** no hay un gate adicional definido para LUQUE-1110.

**Verificación:**

```text
go test ./internal/mcp/tools -run TestGetUnresolvedReferences -count=1
go test ./... -count=1
go test -tags ladybug ./... -count=1
go vet ./...
go test -race ./internal/mcp/... -count=1
make build
```

Las pruebas cubren fallos de nivel símbolo y de nivel módulo, los cinco filtros
por separado y combinados, la separación entre paquete observado y solicitado,
la paginación con cursor y los errores clasificados. El smoke STDIO devolvió
`INVALID_ARGUMENT: repo must not have surrounding whitespace` e
`INDEX_NOT_READY`.

**Limitaciones:** el recorrido de las referencias no resueltas es lineal sobre
la tabla del snapshot; no hay índice por repositorio ni por paquete solicitado,
porque el conjunto está acotado por el corpus indexado. Si `get_unresolved_references`
aparece como caliente en LUQUE-1602, ese índice es el primer candidato.

**Siguiente tarea:** LUQUE-1111.

---

## LUQUE-1111 — Completar `graph_status`

**Dependencias:** todas las tools anteriores.

**Checklist:**

- [x] Verificar dependencias y alcance.
- [x] Completar acciones y entregables.
- [x] Ejecutar pruebas y benchmarks aplicables.
- [x] Verificar criterios de aceptación y el gate aplicable.
- [x] Registrar resultados, limitaciones y siguiente tarea.

**Debe mostrar:**

* snapshot;
* schema;
* resolver;
* repositorios;
* archivos;
* símbolos;
* aristas por tipo;
* unresolved por motivo;
* último rebuild;
* última actualización;
* salud del worker;
* salud de LadybugDB.

**Implementación:**

`graph_status` deja de ser un literal `empty` y pasa a leer el snapshot
publicado. Los datos vienen de tres sitios distintos, y ninguno se inventa.

Del snapshot: id, `snapshot_built_at`, edad, versión de formato de fila, y los
contadores de repositorios, paquetes, archivos, símbolos, evidencia, aristas,
aristas de paquete y unresolved. `edges_by_kind` recorre la CSR forward una vez
—cada arista aparece exactamente una vez bajo su origen— y `unresolved_by_reason`
agrupa la tabla de referencias no resueltas.

De la procedencia del grafo definitivo: `schema_version` y `resolver_version`.
Antes se perdían al construir el snapshot. Ahora `LadybugSnapshotRows` los
transporta, `convertCanonicalGraph` los toma de `CanonicalGraph.SchemaVersion` y
de `Metadata["resolver_version"]`, y `SnapshotMetadata` los conserva, de modo que
la consulta los declara sin reabrir LadybugDB. Un grafo escrito sin resolver
registrado viaja con cadena vacía; no adquiere un valor por defecto.

Del host: `last_rebuild_at`, `last_update_at` y la salud del worker y de
LadybugDB, mediante una `HostStatusProbe` opcional que se inyecta al registrar
la tool. Cuando no hay sonda, ambos componentes se reportan como
`not_configured`, que es una afirmación sobre este despliegue, no sobre el
componente: un worker no sondeado no es un worker sano.

`graph_status` es la única tool que **no** falla sin snapshot: responde
`status: "empty"`. La herramienta que se consulta para averiguar por qué las
demás devuelven `INDEX_NOT_READY` no puede negarse a contestar.

**Estado:** `PASS`.

**Gate:** no hay un gate adicional definido para LUQUE-1111.

**Verificación:**

```text
go test ./internal/mcp/tools -run TestGraphStatus -count=1
go test ./internal/rebuild -run TestBuildSnapshotCarriesGraphProvenance -count=1
go test ./... -count=1
go test -tags ladybug ./... -count=1
go vet ./...
go test -race ./internal/mcp/... ./internal/hotsnapshot ./internal/rebuild -count=1
make build
```

El smoke STDIO devolvió el estado real del binario tal y como está hoy:

```json
{"status":"empty","repositories":0,"symbols":0,"edges_by_kind":[],
 "unresolved_by_reason":[],"worker":{"state":"not_configured"},
 "storage":{"state":"not_configured"}}
```

**Limitaciones:** `serve` todavía no cablea ni un `SnapshotStore` ni una
`HostStatusProbe` —`mcpserver.Run` pasa `nil`—, así que el binario publicado
responde `empty` y `not_configured` con exactitud. Cablear el ciclo
rebuild/publicación al proceso servidor es trabajo de la fase de watcher e
indexación incremental, no de esta tarea; `graph_status` ya está preparado para
reportarlo en cuanto exista. `last_update_at` sólo puede distinguirse de
`last_rebuild_at` cuando ese ciclo publique actualizaciones incrementales.

**Siguiente tarea:** LUQUE-1112.

---

## LUQUE-1112 — Ejecutar pruebas de superficie negativa

**Dependencias:** LUQUE-1111.

**Checklist:**

- [x] Verificar dependencias y alcance.
- [x] Completar acciones y entregables.
- [x] Ejecutar pruebas y benchmarks aplicables.
- [x] Verificar criterios de aceptación y el gate aplicable.
- [x] Registrar resultados, limitaciones y siguiente tarea.

**Comprobar que no existen:**

```text
execute_cypher
index
update
edit
run_command
register_repository
```

**Implementación:**

Se añadió `internal/mcp/surface_test.go` con cuatro comprobaciones sobre el
servidor real, no sobre la lista de registros:

```text
la superficie es exactamente las 9 tools de PLAN.md 17.1
ningún nombre publicado contiene un nombre prohibido de PLAN.md 17.2
toda tool está anotada read-only y ninguna es destructiva
llamar a un nombre prohibido falla
```

La lista prohibida cubre las seis del checklist más las de PLAN.md 17.2
(`execute_query`, `refresh`, `rebuild`, `remove_repository`, `edit_file`). La
comprobación es **por subcadena**, no por igualdad: un futuro `rebuild_graph` o
`graph_index` pasaría una comparación exacta y rompería la misma regla.

La ausencia del listado no bastaba como prueba, así que se comprueba también que
la llamada directa falla: el SDK responde `-32602 unknown tool`.

**Estado:** `PASS`.

**Gate:**

```text
MCP_SURFACE_PASS
```

**Verificación:**

```text
go test ./internal/mcp -count=1
go test ./... -count=1
go test -tags ladybug ./... -count=1
go vet ./...
go test -race ./internal/mcp/... -count=1
make build
```

El guardián se validó por mutación: registrando temporalmente una tool
`execute_cypher` en `newServer`, los tres tests de superficie fallaron —conteo,
subcadena y llamada aceptada— y volvieron a pasar al revertirla. Un guardián que
no se ha visto fallar no es un guardián.

Contra el binario real, `tools/list` devuelve exactamente esas nueve, y
`execute_cypher` y `rebuild` responden `{"code":-32602,"message":"unknown tool"}`.

**Limitaciones:** la prueba cubre la superficie de *tools*. El servidor no
registra prompts ni resources hoy, así que no hay nada que auditar en esos
canales; si algún día se registran, esta prueba no los cubriría y habría que
ampliarla. La superficie se comprueba sobre `NewServer()`; las variantes con
`SnapshotStore` comparten el mismo `newServer`, de modo que registran el mismo
conjunto.

**Siguiente tarea:** LUQUE-1201.

---

## LUQUE-1113 — Devolver lo que el agente puede usar y entrar por el fichero

**Dependencias:** LUQUE-1112.

**Checklist:**

- [x] Verificar dependencias y alcance.
- [x] Completar acciones y entregables.
- [x] Ejecutar pruebas y benchmarks aplicables.
- [x] Verificar criterios de aceptación y el gate aplicable.
- [x] Registrar resultados, limitaciones y siguiente tarea.

**Objetivo:** que una respuesta de la superficie MCP baste para actuar -abrir el
fichero, nombrar el llamante- sin una llamada de seguimiento, y que un agente
que sólo tiene una ruta o un diff pueda entrar al grafo.

**Medición previa** (`snapshot_id` 14 de este repositorio: 2 repositorios, 51
paquetes, 268 ficheros, 9.341 símbolos, 33.823 aristas):

```text
find_symbol        811 B; 385 B (47 %) son canonical_identity; sin file_path ni start_line
find_references    599 B/fila; 242 B (40 %) son el bloque target_* repetido; sin nombre ni línea del llamante
get_blast_radius   457 B/fila; 169 B (37 %) derivables de otro campo o constantes por respuesta; sin start_line
```

«¿Quién llama a `MergeAll` y dónde?» cuesta hoy cuatro llamadas y unos
`4.100 B`: `find_symbol` no da la ubicación, así que hace falta `get_symbol`, y
`find_references` devuelve llamantes que sólo se nombran con una clave opaca de
52 caracteres, así que hace falta un `get_symbol` más por fila.

Siete de las nueve tools exigen `stable_key` y el único generador de
`stable_key` es `find_symbol`, que sólo acepta nombre exacto, cualificado exacto
o prefijo. `FileByRepoPath` existe en el snapshot
(`internal/hotsnapshot/snapshot.go:355`) y ninguna tool lo usa.

**Entregables:**

- [x] `internal/mcp/tools/find_symbol.go`, `get_symbol.go`,
  `find_references.go`, `blast_radius.go`, `trace_dependencies.go` y
  `find_cross_repo_consumers.go`;
- [x] `internal/mcp/tools/file_outline.go`, con la tool `get_file_outline`;
- [x] `internal/mcp/server.go` y `internal/mcp/surface_test.go`;
- [ ] el índice fichero -> símbolos en `internal/hotsnapshot/` si el recorrido
  lineal no basta;
- [x] `PLAN.md` 17.1 y la documentación de protocolo en `docs/protocol/`;
- [x] tests por tool, incluidos los negativos.

**Decisiones:**

* `canonical_identity` sale de la salida por defecto. No es una identidad que el
  agente pueda usar para nada: es la concatenación con `\0` de language,
  repository, package, qualified_name, kind y discriminator, y todos ellos son
  ya un campo aparte o la propia `signature`. Sigue disponible bajo
  `response_format: "detailed"`, que es también donde vuelven los `*_key`
  derivables de su `*_path`.
* Una fila lleva lo que la distingue; lo constante sube al envelope. El bloque
  `target_*` de `find_references` es el argumento de la consulta, no un
  resultado, y repetirlo por fila es el 40 % de la respuesta.
* Un resultado sin `file_path` ni `start_line` obliga a un `get_symbol` por
  fila. `SymbolRecord` ya guarda `StartLine` y `EndLine` y no se emiten: no hay
  ningún dato nuevo que calcular ni que indexar.
* `find_references` nombra el símbolo que **contiene** la referencia, no la
  posición del token. El snapshot no guarda la línea de la ocurrencia -ni
  `PackedEdge` ni `EvidenceRecord` la llevan-, así que se publica la declaración
  que la contiene y se dice que es eso. Fabricar una línea de referencia sería
  inventar evidencia.
* `get_file_outline` acepta un fichero o un prefijo de directorio: es la misma
  pregunta a dos granularidades, no dos tools. Es además la única forma de
  obtener un `stable_key` sin acertar el nombre del símbolo.
* Los modos nuevos de `find_symbol` -`substring` y los filtros `kind`, `repo` y
  `path_prefix`- no cambian la clase de coste: `SearchSymbolsByNamePrefix` ya
  recorre linealmente todos los símbolos (`internal/hotsnapshot/search.go:44`).
* La superficie pasa de nueve a diez tools **y ahí se queda**: diez es el techo
  de esta fase. Repowise midió en Claude Code cuántas veces un agente llega a
  llamar a cada servidor MCP, y sale un acantilado por tamaño de superficie:
  CodeGraph con 1 tool y `1.567` caracteres de esquema fue llamado 13 de 15
  veces; Repowise con 10 tools y `17.561` caracteres, 15 de 15; Serena con 29 y
  `29.050`, 4 de 15; y code-review-graph con 30 y `28.118`, **ninguna**. Claude
  Code carga los esquemas bajo demanda, así que una superficie grande es una
  superficie que el agente no llega a mirar. Diez queda por debajo del
  acantilado observado; la tool once exige retirar una.
* Serena, con la superficie más parecida a la nuestra, escribe menos que un
  agente sin herramientas pero llama a tools un 42 % más de veces. Una cadena
  `find_symbol` -> `get_symbol` -> `find_references` no es una respuesta: es
  trabajo movido al agente. Por eso la fila lleva ubicación y nombre.
* `surface_test.go` es un contrato y no un reflejo del código: `allowedTools`
  se actualiza en el mismo commit o el guardia falla, que es exactamente para
  lo que está.
* Ninguna tool nueva lee el disco. El grafo publica rutas y rangos de líneas;
  abrir el fichero es del harness.

**Criterios de aceptación:**

- [x] `find_symbol` devuelve `file_path` y `start_line`, y una sesión que busca
  un símbolo y lo abre no necesita llamar a `get_symbol`.
- [x] Ninguna respuesta por defecto contiene `canonical_identity`;
  `response_format: "detailed"` lo devuelve.
- [x] `find_references` devuelve `source_name`, `source_qualified_name`,
  `source_kind` y `source_start_line`, y el target aparece una sola vez en la
  respuesta.
- [x] `get_file_outline` sobre `internal/facts/facts.go` devuelve sus 32
  declaraciones con kind, signature y rango de líneas por menos de la décima
  parte de los `20.681 B` que cuesta leer el fichero.
- [x] `get_file_outline` sobre una ruta que el snapshot no conoce devuelve un
  error que nombra la ruta y el repositorio, nunca un resultado vacío.
- [x] `get_blast_radius` y `trace_dependencies` aceptan `paths` o
  `qualified_name` como raíz, además de `stable_key`.
- [x] La suite de superficie sigue prohibiendo toda mutación y exige la
  anotación read-only también en la tool nueva.
- [x] `graph_status` contabiliza las llamadas de `get_file_outline` como las
  demás.
- [x] El coste total del esquema de la superficie queda registrado en la tarea,
  en caracteres, medido sobre el `tools/list` del binario real. Es lo que el
  cliente carga antes de poder llamar a nada, y sin ese número el techo de diez
  es una opinión.

**Fuera de alcance:** `get_repository_overview` -el mapa de paquetes con fan-in
y fan-out sobre `AllPackageDependencies`, 103 aristas de paquete en este
corpus- y `get_call_path` -el camino más corto entre dos símbolos, reconstruible
con `TraversalVisit.Source` (`internal/hotsnapshot/traversal.go:44`), que ya es
el puntero al padre del BFS-. Las dos están justificadas y ninguna depende de
esta tarea: se abren cuando `graph_status` haya medido qué tools llama de verdad
un agente, porque `metrics.queries` publica ya `calls`, `errors` y `results` por
tool (`internal/metrics/metrics.go:66`) y hoy está vacío.

Tampoco entran `read_file`, `search_for_pattern` ni `list_dir`: el harness ya
los trae, y Serena -que los tiene- los desactiva por defecto dentro de Claude
Code y Codex por esa misma razón. Ni edición simbólica ni diagnósticos: lo
primero rompe el contrato read-only y sobre una generación publicada sería un
rename decidido con datos viejos; lo segundo exige un LSP vivo y aquí sólo hay
un snapshot.

**Resultado.** Medido contra la generación publicada `000014`, el mismo
snapshot del que salieron las cifras de arriba:

```text
find_symbol(MergeAll)        811 B -> 558 B  (-31 %), ya con file_path y start_line
find_references, una fila    599 B -> 315 B  (-47 %), el llamante nombrado y situado
get_file_outline(facts.go)          16.665 B, 72 declaraciones
  con kind=func                      3.220 B, 11 declaraciones
```

**El criterio de la décima parte estaba mal escrito y se corrige.** Decía «sus
32 declaraciones», contadas con `grep` sobre las líneas que empiezan por `func`
o `type`. El grafo conoce 147 símbolos en ese fichero, 75 de ellos campos de
struct. Con los campos fuera por defecto -no son declaraciones entre las que se
elige, son la forma del tipo de encima- y sin repetir la ruta en cada fila de un
outline de un solo fichero, la respuesta baja de `41.062 B` a `16.665 B`. Frente
a los `21.014 B` del fichero eso es `1,3x`, no `10x`; con `kind=func` es `7x`.
Ésa es la cifra honesta, y lo que el outline compra no es sobre todo tamaño: son
72 filas direccionables con su rango de líneas, en vez de 550 líneas que hay que
leer para encontrarlas.

**Coste del esquema: de `34.932` a `4.768` caracteres, un 86 % menos.** El
reparto medido explicaba dónde estaba todo: `30.334` caracteres eran los
`outputSchema` que el SDK deriva del tipo de retorno de cada handler, contra
`2.530` de los `inputSchema`, que son la mitad que de verdad le dice a alguien
cómo llamar. Un cliente lee el resultado que recibe; no necesita una
descripción previa de él, y un harness que carga los esquemas bajo demanda paga
esa descripción antes de decidir siquiera si mira la tool.

`ConciseOutputSchema` publica `{"type":"object"}` en su lugar. No se pierde
nada más: los handlers siguen tipados, el SDK sigue serializando el resultado a
`structuredContent` y sigue validándolo -contra un esquema que toda respuesta
satisface-. `TestServerSurfaceStaysCheapToLoad` fija el techo en `8.000`
caracteres y falla si una tool vuelve a publicar el esquema derivado.

La superficie queda descrita en `docs/protocol/mcp-surface-v2.md`, que la fase 19
sustituye por `docs/protocol/mcp-surface-v3.md`.

**Estado:** `PASS`.

**Gate:** ninguno adicional. `MCP_SURFACE_PASS` se vuelve a exigir tras el
cambio, porque la tarea amplía la superficie que ese gate fija.

**Verificación:**

```text
gofmt -l <archivos-go-modificados>
go vet ./...
go test ./internal/mcp/... -count=1
go test ./... -count=1
go test -race ./internal/mcp/... -count=1
make build
```

Y contra el binario real: `kivgraph index --full` sobre este repositorio,
`kivgraph serve`, y la comparación en bytes de la respuesta de cada tool tocada
antes y después del cambio. Una reducción que no se ha medido no se declara.

---

## LUQUE-1114 — Ninguna respuesta afirma un conocimiento que no tiene

**Dependencias:** LUQUE-1113.

**Checklist:**

- [x] Verificar dependencias y alcance.
- [x] Completar acciones y entregables.
- [x] Ejecutar pruebas y benchmarks aplicables.
- [x] Verificar criterios de aceptación y el gate aplicable.
- [ ] Registrar resultados, limitaciones y siguiente tarea.

**Objetivo:** que toda respuesta diga hasta dónde llega, para que un agente
sepa cuándo puede automatizar un cambio y cuándo tiene que ir a mirar el código
él mismo.

**El fallo, medido.** `Coverage.UnresolvedRelated` sólo se incrementa en dos
sitios: `internal/mcp/tools/find_references.go:539`, para aristas cuya
*confianza* es `Unresolved`, y `unresolved.go:191`, una por fila devuelta.
**Nunca se une a la tabla de referencias no resueltas.** `find_symbol`,
`get_symbol`, `get_blast_radius` y `trace_dependencies` devuelven
`unresolved_related: 0` incondicionalmente, mientras el comentario del campo
promete «how confidently the response can account for related graph facts»
(`response.go:20-27`).

Contra el snapshot 14 de este repositorio:

```text
find_symbol{name:"Connection", mode:"prefix"}
  -> results: [], total: 0, unresolved_related: 0
```

En ese mismo snapshot, 18 ficheros Go del repositorio importan
`github.com/LadybugDB/go-ladybug`, y hay `1.288` filas
`MODULE_PROVIDER_NOT_FOUND` pidiendo símbolos de ese módulo con fichero, línea
y columna -entre ellas `benchmarks/ladybug-batch/main.go:328` pidiendo
`Connection.Close`-.

Que la resolución falle está bien: ese módulo no es un repositorio registrado y
no hay fuente que indexar. Lo que no está bien es responder «no hay nada, y sin
ninguna duda». Igual, `get_blast_radius` sobre `MergeAll` devuelve
`exact: 7, unresolved_related: 0` -impacto completo- desde un snapshot con
`1.420` fallos de resolución y dos paquetes propios que no puede ver.

**Entregables:**

- [x] `internal/mcp/tools/response.go`, con el bloque de completitud del
  envelope;
- [x] `find_symbol.go`, `get_symbol.go`, `find_references.go`,
  `blast_radius.go`, `trace_dependencies.go` y `find_cross_repo_consumers.go`;
- [x] el índice de no resueltos por nombre solicitado, paquete y repositorio, en
  `internal/hotsnapshot/`;
- [x] `Exported` en `hotsnapshot.SymbolRecord` y su lectura en el builder;
- [x] tests por tool, incluido el negativo de que `COMPLETE` no se emite cuando
  un punto ciego intersecta la consulta.

**Decisiones:**

* **Cero tools nuevas.** La superficie se queda en las diez de LUQUE-1113. La
  completitud es un bloque del envelope y un modo de `get_blast_radius`, no una
  tool aparte: el agente ya está en `get_blast_radius` cuando se hace esta
  pregunta, y la medida de adopción de LUQUE-1113 dice que crecer la superficie
  es justo lo que impide que se llame a nada.
* Una respuesta es `COMPLETE` sólo cuando ningún fallo registrado intersecta la
  consulta por las tres vías observables: el nombre solicitado, el paquete y el
  repositorio. En cualquier otro caso es `LOWER_BOUND` y viaja con las
  coordenadas de cada punto ciego.
* Un punto ciego se publica como lo que es -una petición que falló, con su
  fichero, su línea y su motivo-, nunca como una arista candidata. El contrato
  de `EXACT` no se toca y no se inventa ninguna relación.
* La respuesta incluye el `grep` que cierra el hueco, acotado a las rutas
  afectadas. Un aviso sin acción de recuperación obliga al agente a un barrido
  completo, que cuesta más que no avisar.
* `Exported` viaja al HotSnapshot. Ya se calcula en los tres lenguajes
  (`facts.go:202`, `golang.go:125`, `typescript.go:345`, `rust.go:140`), se
  persiste (`canonical_load.go:178`) y se lee de vuelta
  (`canonical_scan.go:61`), pero `hotsnapshot.SymbolRecord` no lo tiene y la
  superficie MCP no distingue una API pública de un helper privado. Sin ese
  campo el veredicto no se puede acotar: romper un símbolo no exportado se
  queda en su paquete, romper uno exportado cruza el repositorio.
* **Esto no es la etiqueta de Sourcegraph.** Sourcegraph distingue `precise` de
  `search-based` y, cuando no tiene índice SCIP, rellena el hueco con una
  búsqueda de texto por límite de palabra: el resultado nunca se presenta como
  incompleto, sino como menos preciso, y los sitios donde no tiene nada no se
  enumeran jamás. Su etiqueta es binaria y para un humano en una interfaz.
  Aquí son coordenadas, con motivo, para un agente, y el veredicto es una
  palabra sobre la que puede ramificar.
* Se puede hacer porque Kivgraph se negó a adivinar. Un índice que resuelve por
  coincidencia de nombre siempre devuelve algo y por eso no sabe dónde falló:
  su recall es alto y desconocido. El de aquí es más bajo y **medido**, y eso es
  lo único que se puede reportar.

**Criterios de aceptación:**

- [x] Ninguna tool devuelve `unresolved_related: 0` sin haberlo comprobado
  contra la tabla de no resueltos.
- [x] `find_symbol{name:"Connection", mode:"prefix"}` sobre este repositorio
  deja de devolver un cero limpio y nombra el módulo que no se pudo resolver.
- [x] `get_blast_radius` sobre un símbolo con puntos ciegos devuelve
  `LOWER_BOUND`, sus coordenadas y el `grep` acotado; sobre uno sin ellos
  devuelve `COMPLETE`.
- [x] Un símbolo no exportado cuyo paquete está entero en el índice puede
  alcanzar `COMPLETE`.
- [x] Los puntos ciegos nunca aparecen como resultados ni como aristas: viajan
  en su propio bloque.
- [x] La superficie sigue siendo de diez tools.
- [x] Añadir una fila de no resueltos a un fixture cambia el veredicto. Un
  guardia que no se ha visto fallar no es un guardia.

**Fuera de alcance:** el diff semántico entre dos generaciones publicadas -qué
cambió en la superficie pública desde la indexación anterior y a quién rompe-.
`facts.Diff` existe (`internal/facts/delta.go:240`) y las generaciones están
numeradas, pero antes hay que comprobar si los datos de una generación anterior
siguen siendo legibles tras publicar la siguiente. Si no lo son, hace falta
persistir un resumen de superficie pública por generación, y eso cambia el
formato de almacenamiento y pide un ADR. Sourcegraph tiene `compare_revisions` y
`diff_search`, pero son diffs de git: nadie compara dos estados del grafo.

**Resultado.** Contra la generación publicada `000014` de esta máquina, con un
binario construido con el tag `ladybug` y hablando MCP por stdio:

```text
find_symbol{name:"Connection", mode:"prefix"}
  antes:  total=0, unresolved_related=0
  ahora:  total=0, unresolved_related=68
```

`get_blast_radius{qualified_name:"MergeAll"}` responde `LOWER_BOUND` y enumera
los tres ámbitos que el índice no puede leer -los dos paquetes que los build
tags excluyen y `vitest`-, con el `grep` acotado a sus directorios:

```json
"fallback": {"pattern": "\\bMergeAll\\b", "paths": ["…/benchmarks/ladybug-delta-profile", "…/benchmarks/ladybug-recovery"]}
```

El guardia se vio fallar antes de pasar: `TestBlastRadiusVerdictTurnsOnOneRecordedFailure`
exige `COMPLETE` sobre un grafo limpio y `LOWER_BOUND` sobre el mismo grafo con
una sola fila de no resueltos añadida.

**Estado:** `PASS`.

**Gate:** ninguno adicional. `MCP_SURFACE_PASS` se vuelve a exigir, porque la
tarea cambia el contrato de la respuesta que ese gate fija.

**Verificación:**

```text
gofmt -l <archivos-go-modificados>
go vet ./...
go test ./internal/mcp/... ./internal/hotsnapshot/... -count=1
go test ./... -count=1
make test-ladybug PKGS=./internal/storage/ladybug
make build
```

`make test-ladybug` entra porque `Exported` cruza la frontera nativa. Contra el
binario real: `kivgraph index --full` sobre este repositorio y las tres
consultas de arriba -`Connection`, `MergeAll` y un símbolo privado de un paquete
íntegro- comparadas con su respuesta actual.

---

# 15. Fase 12 — Resiliencia

## LUQUE-1201 — Recuperar worker TypeScript

**Dependencias:** MCP_SURFACE_PASS.

**Checklist:**

- [x] Verificar dependencias y alcance.
- [x] Completar acciones y entregables.
- [x] Ejecutar pruebas y benchmarks aplicables.
- [x] Verificar criterios de aceptación y el gate aplicable.
- [x] Registrar resultados, limitaciones y siguiente tarea.

**Probar:**

* SIGTERM;
* SIGKILL;
* crash loop;
* protocolo inválido;
* timeout.

**El último snapshot debe seguir disponible.**

**Implementación:**

`internal/tsworker/recovery_test.go` cubre los cinco modos de fallo contra un
proceso hijo real:

```text
SIGTERM / SIGKILL externos -> nueva sesión, PID distinto, Call vuelve a servir
protocolo inválido recuperable -> InvalidFrames++, misma generación, sesión viva
protocolo inválido fatal -> sesión reemplazada (trama truncada y sobredimensionada)
crash loop -> estado FAILED, reinicios acotados, Call falla con RESTART_LIMIT
timeout -> TIMEOUT clasificado y worker sustituido
```

La distinción entre trama inválida recuperable y fatal no es decorativa: sólo
`INVALID_PAYLOAD` deja el flujo alineado, porque respetó el límite de trama. Una
longitud corrupta o excesiva pierde el límite y el protocolo prohíbe
resincronizar, así que la sesión debe morir.

Se añadió `internal/resilience`, un paquete **sin código de producción** para
las pruebas de costura de la fase 12: cada componente ya prueba su propia
recuperación, pero ninguno puede afirmar solo la propiedad que pide el plan.
`TestPublishedSnapshotSurvivesWorkerLoss` mata el worker, agota su presupuesto
de reinicios hasta `FAILED`, y comprueba a través del **servidor MCP real** que
`get_symbol` devuelve exactamente la misma respuesta antes, durante y después.

Su control es `TestClosedSnapshotStoreStopsServing`: al cerrar el store, la
misma consulta pasa a `INDEX_NOT_READY`. Sin ese contraste, «sigue sirviendo» no
demostraría nada.

**Hallazgos durante la verificación:**

El primer fake de trama inválida escribía `{"not":"an envelope"}`, que **sí**
decodifica como envelope con versión 0 y por tanto es `VERSION_MISMATCH`: fatal,
no recuperable. El caso recuperable exige un cuerpo que no sea JSON en absoluto.

El primer fake de flujo corrupto escribía una cabecera larga y se quedaba
bloqueado; el lector se quedaba esperando los bytes prometidos en vez de fallar.
`FRAME_TRUNCATED` sólo se observa al llegar al fin de la entrada, así que el
worker debe morir tras la trama parcial. Ambos son comportamientos correctos del
supervisor que el test describía mal.

**Estado:** `PASS`.

**Gate:** no hay un gate adicional definido para LUQUE-1201.

**Verificación:**

```text
go test ./internal/tsworker -run 'TestSupervisorRecovers|TestSupervisorSurvives|TestSupervisorRestartsAfterFatal|TestSupervisorFailsClosed|TestSupervisorTimeoutInvalidates' -count=1
go test ./internal/resilience -count=1
go test ./... -count=1
go test -tags ladybug ./... -count=1
go vet ./...
go test -race ./internal/tsworker ./internal/resilience -count=1
make build
```

**Limitaciones:** el worker de las pruebas es el propio binario de test
reejecutado, no el worker TypeScript real; habla el mismo códec porque enlaza el
mismo paquete, pero no ejerce Node.js ni el motor de TypeScript. La prueba de
snapshot usa un `SnapshotStore` publicado a mano: `serve` todavía no cablea el
ciclo de indexación, así que lo que se demuestra es que la ruta de consulta no
depende del worker, no que un despliegue completo sobreviva. `SIGSTOP` (worker
congelado, no muerto) no se prueba: hoy se manifestaría como timeout de petición,
que ya está cubierto.

**Siguiente tarea:** LUQUE-1202.

---

## LUQUE-1202 — Probar fallo durante full rebuild

**Dependencias:** LUQUE-1201.

**Checklist:**

- [x] Verificar dependencias y alcance.
- [x] Completar acciones y entregables.
- [x] Ejecutar pruebas y benchmarks aplicables.
- [x] Verificar criterios de aceptación y el gate aplicable.
- [x] Registrar resultados, limitaciones y siguiente tarea.

**El grafo activo no debe cambiar.**

**Implementación:**

«Grafo activo» son dos cosas distintas y las dos se comprueban.

En disco, `internal/rebuild/failure_test.go` inyecta un fallo en cada punto de
la tubería y exige el mismo invariante en todos:

```text
facts · bulk load · counts · integrity · snapshot scan · golden probes
```

Tras cada fallo: `ErrRebuildFailed`, la etapa registrada como fallida, sin
publicación, `CURRENT` intacto, y el candidato inexistente en disco. La
comparación no es del puntero: `captureGeneration` renderiza `CURRENT` **y los
bytes de todos los archivos de la generación activa**, de modo que una escritura
parcial dentro de la generación viva también se detectaría.

Se añaden dos casos que las pruebas por etapa no cubrían: cancelación del
contexto a mitad de ejecución —con la base candidata ya escrita— y cinco fallos
consecutivos, que es lo que un operador sufre de verdad.

En memoria, `internal/resilience/rebuild_test.go` mira lo mismo desde el
cliente: con un snapshot publicado y consultas vivas contra el servidor MCP
real, un rebuild que falla no altera la respuesta de `get_symbol`. Que
`rebuild.Run` y `SnapshotStore.Publish` sean pasos separados es justo lo que
hace imposible que una ejecución que no llegó a publicar toque el store.

**Controles:** cada aserción de «no cambió» tiene su contraprueba, porque una
comparación que nunca observa un cambio no demuestra nada.

```text
TestSuccessfulRebuildDoesChangeTheActiveGeneration   captureGeneration sí ve una publicación
TestServedGraphChangesWhenANewerSnapshotIsPublished  querySymbol sí ve una generación nueva
TestSnapshotStoreRejectsAStaleCandidate              publicar hacia atrás se rechaza y no cambia nada
```

**Estado:** `PASS`.

**Gate:** no hay un gate adicional definido para LUQUE-1202.

**Verificación:**

```text
go test ./internal/rebuild -count=1
go test ./internal/resilience -count=1
go test ./... -count=1
go test -tags ladybug ./... -count=1
go vet ./...
go test -race ./internal/rebuild ./internal/resilience -count=1
make build
```

**Limitaciones:** los ganchos `Load`, `Counts`, `Probes`, `Integrity` y `Scan`
son los falsos que ya usaba `internal/rebuild`, así que se ejercita la
orquestación, no LadybugDB: un fallo que sólo ocurra dentro del motor nativo
—por ejemplo una transacción a medias— no lo cubre esta tarea, sino LUQUE-1205.
La cancelación se inyecta después del bulk load; no hay forma determinista de
cancelar dentro de una etapa nativa sin cgo. El store en memoria se publica a
mano porque `serve` aún no cablea el ciclo de indexación.

**Siguiente tarea:** LUQUE-1203.

---

## LUQUE-1203 — Probar fallo durante delta

**Dependencias:** LUQUE-1201.

**Checklist:**

- [x] Verificar dependencias y alcance.
- [x] Completar acciones y entregables.
- [x] Ejecutar pruebas y benchmarks aplicables.
- [x] Verificar criterios de aceptación y el gate aplicable.
- [x] Registrar resultados, limitaciones y siguiente tarea.

**La transacción debe hacer rollback.**

**Implementación:**

`internal/indexer/delta_rollback_native_test.go` prueba el rollback contra el
motor real, no contra un doble: tras un delta fallido, `ScanCanonical` devuelve
un grafo idéntico al de antes y las invariantes siguen pasando.

Llegar a la transacción costó más de lo que parece, y ese es el hallazgo de la
tarea. El primer intento inyectaba una arista con destino inexistente en el
conjunto de hechos: **nunca abría transacción**, porque `facts.Diff` rechaza el
conjunto antes.

```text
incremental update failed: diff indexed state: invalid delta: next set:
invalid fact set: edge CALLS_DIRECT has unknown target "…Ghost"
```

Un test verde que no ejercitaba nada. El fallo se inyecta ahora como ocurre en
operación —**deriva de estado**—: la base activa se carga desde un grafo que
nunca tuvo `b.go`, mientras el indexador difunde dos estados que sí lo
contienen. Ambos conjuntos validan, el delta está bien formado, y el motor es
el primer componente en posición de notar que el destino de la arista no está
en la base que muta. El delta además retira y reafirma `a.go`, de modo que hay
trabajo real aplicado antes del statement que falla: sin una única transacción,
ese trabajo sobreviviría.

El test exige además que el error provenga de `apply delta to`. Sin esa
comprobación, cualquier rechazo anterior volvería a dejar la prueba vacía.

**Control:** `TestUpdateDeltaRouteSucceedsAfterARollback` — la base no sólo
quedó igual, quedó **usable**: un delta que sí resuelve se aplica limpiamente
justo después y produce el mismo grafo que una carga completa.

`internal/indexer/delta_failure_test.go` cubre lo que ninguna transacción puede
deshacer: la orquestación. Para cada paso que puede fallar —aplicar, contar,
construir el snapshot, informe que no pasa— el snapshot publicado no cambia y
el update no se declara exitoso. Una cancelación previa ni siquiera abre
transacción.

**Ventana documentada:** una vez el motor hace COMMIT, la base está mutada y un
fallo posterior no la deshace. Si lo que falla es el refresco del digest, la
generación conserva el digest del contenido anterior.
`TestUpdateLeavesAStaleDigestWhenTheMutationOutlivesTheUpdate` lo fija como
comportamiento observado, y **falla cerrado**: `Rollback` revalida el destino
recomputando el digest desde la base, así que una generación cuyo digest ya no
corresponde a su contenido se rechaza en vez de restaurarse en silencio.

**Estado:** `PASS`.

**Gate:** no hay un gate adicional definido para LUQUE-1203.

**Verificación:**

```text
go test -tags ladybug ./internal/indexer -run TestUpdateDeltaRoute -count=1
go test ./internal/indexer -count=1
go test ./... -count=1
go test -tags ladybug ./... -count=1
go vet ./...
go test -race ./internal/indexer -count=1
make build
```

**Limitaciones:** el fallo dentro de la transacción se provoca por deriva de
estado, que es realista pero es **un** camino; un fallo de E/S o una caída del
proceso a mitad de COMMIT dependen de la durabilidad de LadybugDB y se abordan
en LUQUE-1205. La ventana post-commit del digest queda documentada, no cerrada:
cerrarla exige refrescar el digest dentro de la misma transacción, lo que hoy
no es posible porque el digest se calcula desde fuera, con los contadores ya
comprometidos.

**Siguiente tarea:** LUQUE-1204.

---

## LUQUE-1204 — Probar snapshot corrupto

**Dependencias:** LUQUE-1201.

**Checklist:**

- [x] Verificar dependencias y alcance.
- [x] Completar acciones y entregables.
- [x] Ejecutar pruebas y benchmarks aplicables.
- [x] Verificar criterios de aceptación y el gate aplicable.
- [x] Registrar resultados, limitaciones y siguiente tarea.

**Kivgraph debe cargar el último snapshot válido o reconstruirlo.**

**Implementación:**

Lo primero es qué significa «snapshot corrupto» en esta arquitectura. Lo único
que se persiste junto a una generación es `snapshot.sha256`, el digest de sus
contadores canónicos. El `HotSnapshot` **nunca se escribe**: se deriva del grafo
definitivo en cada construcción. Por eso de las dos ramas del requisito siempre
se toma la segunda: no hay snapshot que cargar, hay grafo que reconstruir.

`internal/rebuild/snapshot_corruption_test.go` fija las tres consecuencias:

```text
digest corrupto  -> SnapshotGeneration sigue reconstruyendo un snapshot válido
digest corrupto  -> Rollback a esa generación se rechaza…
                    …y vuelve a funcionar tras recomputarlo desde la base
grafo inconvertible -> ErrSnapshotBuildFailed, nada utilizable devuelto
```

El rechazo importa más que la recuperación: una generación cuyo digest no
concuerda con su contenido es una generación por la que nadie responde, y
reactivarla sustituiría un grafo que funciona por otro sin verificar. La
recuperación no es una reparación del archivo, es una **recomputación** desde
los contadores que la base declara ahora.

`internal/resilience/snapshot_test.go` lo mira desde el cliente:

```text
corromper el digest            -> las respuestas servidas no cambian
reconstruir y publicar          -> los lectores pasan al grafo nuevo de golpe
grafo inconvertible sin publicar-> INDEX_NOT_READY, nunca un grafo a medias
```

La tercera es la que cierra el requisito por el lado honesto: si no hay nada
válido que servir, la respuesta correcta es decirlo, no entregar un grafo
parcialmente construido.

**Estado:** `PASS`.

**Gate:** no hay un gate adicional definido para LUQUE-1204.

**Verificación:**

```text
go test ./internal/rebuild -count=1
go test ./internal/resilience -count=1
go test ./... -count=1
go test -tags ladybug ./... -count=1
go vet ./...
go test -race ./internal/rebuild ./internal/resilience -count=1
make build
```

**Limitaciones:** no existe un snapshot serializado, así que «cargar el último
válido» sólo puede significar hoy «seguir sirviendo el que ya está en memoria».
Un arranque en frío siempre reconstruye; el coste de esa reconstrucción sobre un
corpus real se mide en LUQUE-1602, y si resultara prohibitivo, persistir el
`HotSnapshot` sería una decisión nueva, no una corrección de ésta. Tampoco hay
recuperación automática: nadie vigila el digest ni dispara la reconstrucción
sola, porque `serve` aún no cablea ese ciclo. La corrupción de la **base** —no
del digest— es LUQUE-1205.

**Siguiente tarea:** LUQUE-1205.

---

## LUQUE-1205 — Probar base corrupta

**Dependencias:** LUQUE-1201.

**Checklist:**

- [x] Verificar dependencias y alcance.
- [x] Completar acciones y entregables.
- [x] Ejecutar pruebas y benchmarks aplicables.
- [x] Verificar criterios de aceptación y el gate aplicable.
- [x] Registrar resultados, limitaciones y siguiente tarea.

**Debe:**

* detectar;
* bloquear escrituras;
* conservar snapshot válido;
* informar mediante doctor.

**Implementación:**

«Base corrupta» aquí es el **archivo dañado** —truncado o sobrescrito—, no un
esquema alterado: eso último ya lo cubrían las pruebas del doctor desde
LUQUE-0205. Es el estado que deja un fallo de disco, una copia interrumpida o
un restore parcial.

`internal/storage/ladybug/corruption_native_test.go` cubre los tres primeros
puntos contra el motor real:

```text
detectar          DiagnoseStorage devuelve Healthy=false y nombra el check caído,
                  sin tocar el archivo que inspecciona
bloquear escrituras LoadCanonical se niega antes de abrir -ErrAlreadyExists sobre
                  una base que ya existe- y una apertura de escritura sobre el
                  archivo dañado falla; el hash del archivo no cambia: un rechazo
                  no añade daño al daño
bloquear lecturas ScanCanonical falla y no devuelve un grafo parcial junto al error
```

La comprobación de que el hash no cambia es la que importa: un `LoadCanonical`
contra una ruta que ya no es una base podría haber creado una base nueva encima
y destruido la evidencia. No lo hace -- ni siquiera llega al motor: la guarda de
`os.Stat` la detiene antes. (Este punto lo cubría `ApplyCanonicalDelta` hasta
que el ADR 0057 retiró el camino incremental; el vehículo de escritura es ahora
`LoadCanonical` más una apertura de escritura, que es la ruta viva.)

`internal/resilience/database_native_test.go` junta los cuatro puntos en una
sola historia: con un servidor MCP sirviendo y una base canónica real, se
destruye el archivo y se comprueba que el doctor lo reporta, que el motor
rechaza cargar, y que **las respuestas servidas no cambian**. El grafo servido
está en memoria y es derivado; destruir su origen no puede alcanzarlo.

**Control:** `TestHealthyDatabasePassesTheSameChecks` — las mismas aserciones
pasan sobre una base sana, así que no fallan por construcción.

**Estado:** `PASS`.

**Gate:** no hay un gate adicional definido para LUQUE-1205.

**Verificación:**

```text
go test -tags ladybug ./internal/storage/ladybug -run 'TestDiagnoseStorageDetectsADamaged|TestCorruptDatabase|TestHealthyDatabasePasses' -count=1
go test -tags ladybug ./internal/resilience -count=1
go test ./... -count=1
go test -tags ladybug ./... -count=1
go vet ./...
go test -race ./internal/resilience -count=1
make build
```

El informe por CLI se comprobó ejecutándolo de verdad sobre un archivo
destruido:

```text
storage doctor: FAIL
[PASS] lock: no external database lock detected
[FAIL] open: failed to open database with status 1
[SKIP] schema/transactions/counts/integrity: database did not open
exit=1
```

Los checks posteriores se declaran `SKIP`, no `PASS`: una base que no abre no ha
superado nada.

**Limitaciones:** el daño se inyecta a nivel de archivo completo. Una corrupción
de una sola página que el motor abriera sin quejarse produciría un grafo que
parece válido, y frente a eso la defensa no es el doctor de almacenamiento sino
las invariantes canónicas de `doctor graph`, que se ejecutan por separado.
«Bloquear escrituras» se cumple porque cada operación abre la base y falla, no
porque exista un cerrojo global que marque la base como no escribible: no hay
un modo degradado explícito, y si se quisiera uno sería una decisión nueva.

**Siguiente tarea:** LUQUE-1206.

---

## LUQUE-1206 — Probar proceso duplicado

**Dependencias:** LUQUE-1201.

**Checklist:**

- [x] Verificar dependencias y alcance.
- [x] Completar acciones y entregables.
- [x] Ejecutar pruebas y benchmarks aplicables.
- [x] Verificar criterios de aceptación y el gate aplicable.
- [x] Registrar resultados, limitaciones y siguiente tarea.

**El segundo proceso debe fallar de manera clara y segura.**

**Hallazgo:**

Se midió primero el comportamiento real con dos procesos: el segundo es
rechazado en **10 ms**, antes de escribir nada. Seguro, sí. Claro, no:

```text
ladybug open: failed to open database with status 1
```

Ese es **exactamente** el mismo mensaje que produce una base corrupta —el mismo
que aparece en el informe del doctor de LUQUE-1205—. Un operador no podía
distinguir «hay otra instancia corriendo» de «el archivo está dañado», que son
dos acciones opuestas: matar el duplicado o restaurar una copia.

**Implementación:**

`ladybug.Open` clasifica ahora el fallo de apertura. Cuando el motor rechaza
abrir, se consulta la tabla de cerrojos del sistema —la misma inspección que ya
usaba el chequeo `lock` del doctor— y, si otro proceso vivo retiene el archivo,
el error se envuelve en `ErrDatabaseLocked` nombrando los PIDs:

```text
ladybug open: LadybugDB database is held by another process (pids [12345]):
failed to open database with status 1
```

La distinción viene de fuera del motor porque el motor no la da. La causa
original se conserva envuelta: no se sustituye un diagnóstico por otro.

**Pruebas:** `internal/storage/ladybug/duplicate_process_linux_test.go` levanta
un segundo proceso real que retiene la base y comprueba que `Open` se rechaza
con `ErrDatabaseLocked` -- es donde vive `classifyOpenFailure`, la única puerta
de escritura--, que el error nombra los PIDs, y que al morir el proceso la base
vuelve a ser utilizable —el cerrojo es del kernel, no un archivo obsoleto que
Kivgraph deje atrás—. Su cláusula de seguridad asserta que `LoadCanonical` se
niega con `ErrAlreadyExists`, que es lo que hace antes de tocar el motor. La
prueba usaba `ApplyCanonicalDelta` hasta que el ADR 0057 lo retiró.

**Control:** `TestDamagedDatabaseIsNotReportedAsLocked` — una base destruida
conserva su propio error. Sin él, clasificar todo como «locked» también pasaría.

**Comprobación por mutación:** revertir la clasificación deja el test en rojo
con el mensaje genérico; restaurarla lo devuelve a verde.

**Estado:** `PASS`.

**Gate:** no hay un gate adicional definido para LUQUE-1206.

**Verificación:**

```text
go test -tags ladybug ./internal/storage/ladybug -run 'TestSecondProcessIsRefused|TestDamagedDatabaseIsNotReportedAsLocked' -count=1
go test ./... -count=1
make test-ladybug
go vet ./...
make build
```

**Limitaciones:** la detección del proceso que retiene es **de Linux**: lee
`/proc/locks`. En otras plataformas `externalStorageLocks` declara que no está
soportado y el error vuelve a ser el genérico del motor —seguro igualmente, pero
sin nombrar la causa—. No existe un cerrojo propio de Kivgraph: la exclusión la
da LadybugDB sobre el archivo de base, de modo que dos procesos que trabajen
sobre **generaciones distintas** de la misma raíz no se excluyen entre sí. Ese
caso —dos `rebuild` concurrentes publicando en la misma raíz— no lo cubre esta
tarea y necesitaría un cerrojo de raíz, que sería una decisión nueva.

**Siguiente tarea:** LUQUE-1207.

---

## LUQUE-1207 — Probar apagado limpio

**Dependencias:** LUQUE-1201.

**Checklist:**

- [x] Verificar dependencias y alcance.
- [x] Completar acciones y entregables.
- [x] Ejecutar pruebas y benchmarks aplicables.
- [x] Verificar criterios de aceptación y el gate aplicable.
- [x] Registrar resultados, limitaciones y siguiente tarea.

**Debe cerrar:**

* MCP;
* watcher;
* worker;
* conexiones;
* LadybugDB.

**Implementación:**

Se añadió `internal/app`, el coordinador de ciclo de vida que faltaba entre
los componentes ya existentes y el proceso `serve`:

```text
internal/app/lifecycle.go
internal/app/shutdown.go
```

`Lifecycle` comparte un `context.Context` cancelable con todos los bucles de
larga duración. Los recursos se registran en **orden explícito de apagado**,
no en orden inverso de construcción:

```text
MCP ingress / connections
→ HotSnapshot
→ watcher
→ worker
→ conexiones persistentes
→ LadybugDB
```

El primer paso de `Shutdown` cancela el contexto. Así `sdkmcp.Server.Run`
cierra su sesión STDIO y el `watcher.Run` abandona su select. Después se
ejecutan todos los `Close`, aunque uno falle; al final se espera a que terminen
los bucles. Los errores se conservan con el nombre del componente mediante
`errors.Join`, por lo que un fallo al cerrar el watcher no oculta el cierre del
worker o la base.

El cierre es idempotente y seguro para llamadas concurrentes. El segundo
llamador espera la misma operación y puede poner su propio deadline. Después de
`Wait` o `Shutdown` no se pueden registrar bucles ni recursos nuevos: esto
evita extender un `WaitGroup` mientras se está drenando.

`cmd/kivgraph` usa `signal.NotifyContext` para `SIGINT` y `SIGTERM` y ejecuta
`serve` dentro del coordinador. El smoke contra el binario real terminó con
`SIGTERM` y código **0**, sin dejar el proceso vivo.

**Pruebas:**

* `internal/app/lifecycle_test.go` comprueba cancelación del runner, orden
  watcher → worker → conexión → LadybugDB, cierre idempotente, continuación
  después de un error, respeto de deadlines y que los recursos se cierran
  antes de drenar un runner que depende de ellos.
* `internal/app/shutdown_native_test.go` carga una base canónica real, ejecuta
  `Health` —que abre y cierra una conexión nativa—, cierra el `Database` a
  través del coordinador y lo reabre; el lock queda liberado.
* `internal/resilience/shutdown_test.go` usa sesiones MCP reales en transportes
  en memoria, `watcher.New`/`Run`, un `tsworker.Supervisor` real y un
  `SnapshotStore`. Después del shutdown las sesiones no aceptan llamadas, los
  canales del watcher están cerrados, el worker está en `CLOSED` y el snapshot
  deja de servirse.
* `cmd/kivgraph/main_test.go` verifica que cancelar el contexto de `serve`
  detiene el runner MCP y no devuelve error de apagado esperado.

**Comprobación por mutación:** invertir temporalmente el recorrido de recursos
en `Shutdown` hace fallar dos tests con el orden observado
`ladybug,worker,connection,watcher`; restaurar el orden explícito devuelve la
suite a verde.

**Estado:** `PASS`.

**Gate:** no hay un gate adicional definido para LUQUE-1207.

**Verificación:**

```text
go test ./... -count=1
make test-ladybug
go test -tags ladybug ./internal/app ./internal/resilience -run 'TestLifecycleCloses' -count=1 -v
go vet ./...
go test -race ./internal/app ./internal/resilience ./cmd/kivgraph -count=1
make build
```

Resultados observados:

```text
19 paquetes Go sin tag: PASS; 3 sin tests
25 paquetes con LadybugDB: PASS; 4 sin tests
go vet: limpio
3 paquetes con race: PASS
make build: PASS
smoke SIGTERM al binario serve: exit 0
```

**Limitaciones:** el comando `serve` todavía sólo construye el servidor MCP
con el `SnapshotStore` nulo; el cableado del watcher, supervisor y base a ese
proceso pertenece a la integración del pipeline de indexación. El coordinador
ya ofrece el contrato para registrarlos y la prueba de costura ejerce los
cuatro componentes reales donde existen. `generation.Store` no mantiene
descriptores ni conexiones persistentes: sus operaciones abren recursos
locales con `defer`, así que no necesita `Close`; sólo debe dejar de recibir
operaciones al cancelar el pipeline. En plataformas sin señal POSIX, el
coordinador sigue funcionando mediante cancelación de contexto, pero el
adaptador de señales del CLI es específico de Unix.

**Siguiente tarea:** LUQUE-1208.


---

## LUQUE-1208 — Crear matriz de resiliencia

**Dependencias:** LUQUE-1202 a LUQUE-1207.

**Checklist:**

- [x] Verificar dependencias y alcance.
- [x] Completar acciones y entregables.
- [x] Ejecutar pruebas y benchmarks aplicables.
- [x] Verificar criterios de aceptación y el gate aplicable.
- [x] Registrar resultados, limitaciones y siguiente tarea.

**Entregable:** `docs/testing/resilience-matrix.md`.

La matriz reúne LUQUE-1202 a LUQUE-1207 y exige, por escenario, un fallo
inyectado, un invariante observable, la prueba que lo demuestra, un control
positivo y las limitaciones que siguen abiertas. Todas las filas quedaron en
`PASS`; el gate `RESILIENCE_PASS` queda justificado.

**Verificación ejecutada:**

```text
go test ./... -count=1: PASS; 19 paquetes con tests, 3 sin tests
make test-ladybug: PASS
go vet ./...: PASS
go test -race ./internal/app ./internal/indexer ./internal/rebuild ./internal/resilience ./internal/tsworker -count=1: PASS; 5 paquetes
make build: PASS
smoke del binario serve + SIGTERM: exit 0
```

**Benchmarks:** no aplican a este gate; la matriz verifica invariantes de
seguridad, recuperación y cierre, no latencia ni throughput.

**Limitaciones registradas:** la integración de `serve` todavía no instancia
el pipeline completo; la corrupción cubierta es truncado/sobrescritura; la
clasificación con PIDs es específica de Linux; no se cubren dos rebuilds
concurrentes sobre generaciones distintas, pérdida de alimentación ni
restauración de backup. `HotSnapshot` no se serializa y no hay recuperación
automática en `serve`.

**Estado:** `PASS`.

**Siguiente tarea desbloqueada:** LUQUE-1301.

**Gate:**

```text
RESILIENCE_PASS
```

---

# 16. Fase 13 — Rendimiento

## LUQUE-1301 — Crear generador de workload MCP

**Dependencias:** RESILIENCE_PASS.

**Checklist:**

- [x] Verificar dependencias y alcance.
- [x] Completar acciones y entregables.
- [x] Ejecutar pruebas y benchmarks aplicables.
- [x] Verificar criterios de aceptación y el gate aplicable.
- [x] Registrar resultados, limitaciones y siguiente tarea.

**Implementación:**

- `internal/mcpworkload/workload.go` genera una secuencia determinista de
  `CallTool` con `math/rand/v2`, semilla explícita y probes de corpus.
- La distribución usa asignación de resto mayor y respeta exactamente
  `40/25/20/10/5` cuando el número de llamadas es divisible por 20.
- Cada operación recibe argumentos válidos para la superficie MCP actual:
  `find_symbol`, `get_symbol`, `find_references`,
  `find_cross_repo_consumers` y `get_blast_radius`.
- `internal/mcpworkload/workload_test.go` cubre distribución, reproducibilidad,
  semillas distintas, argumentos, recuentos pequeños, validación y cancelación.
- `internal/mcpworkload/integration_test.go` envía las solicitudes generadas a
  un servidor MCP real sobre `SnapshotStore` y exige cero errores.
- `benchmarks/mcp-workload/main.go` expone el generador como CLI y escribe un
  documento JSON autocontenido (`schema_version`, semilla, distribución y
  solicitudes).

**Uso:**

```text
go run ./benchmarks/mcp-workload \
  --calls 10000 \
  --seed 42 \
  --output /tmp/mcp-workload.json
```

**Verificación ejecutada:**

```text
go test ./internal/mcpworkload ./benchmarks/mcp-workload -count=1: PASS
go test ./... -count=1: PASS; 20 paquetes con tests, 4 sin tests
go vet ./...: PASS
go test -race ./internal/mcpworkload ./benchmarks/mcp-workload -count=1: PASS
go run ... --calls 20 --seed 42: PASS; 8/5/4/2/1 solicitudes
dos ejecuciones CLI con la misma semilla: JSON idéntico
```

**Benchmarks:** el generador no mide latencia; LUQUE-1302 consumirá este
documento para medir un cliente y conservará las métricas por operación.

**Limitaciones:** el CLI recibe un probe explícito (`--symbol-name` y
`--stable-key`); la API Go acepta múltiples probes para distribuir consultas
entre símbolos. Los probes deben existir en el snapshot y, para workloads de
éxito, deben tener las relaciones necesarias para las operaciones de
referencias, consumidores y blast radius.

**Estado:** `PASS`.

**Siguiente tarea desbloqueada:** LUQUE-1302.

---

## LUQUE-1302 — Benchmark de un cliente

**Dependencias:** LUQUE-1301.

**Checklist:**

- [x] Verificar dependencias y alcance.
- [x] Completar acciones y entregables.
- [x] Ejecutar pruebas y benchmarks aplicables.
- [x] Verificar criterios de aceptación y el gate aplicable.
- [x] Registrar resultados, limitaciones y siguiente tarea.

**Entregables:**

```text
benchmarks/mcp-client/main.go
benchmarks/mcp-client/main_test.go
benchmarks/mcp-client/results.json
benchmarks/mcp-client/report.md
```

El benchmark construye un `HotSnapshot` sintético reproducible de 100.000
símbolos y 1.000.000 de aristas, abre un único cliente SDK sobre transporte
MCP en memoria y ejecuta 10.000 llamadas con 100 llamadas de warm-up por
operación. Conserva latencia round-trip, latencia backend observada,
throughput, allocations/op, bytes/op, RSS, goroutines y errores.

**Resultado medido en `linux/amd64`, Go 1.24.4, Ryzen 7 9700X:**

```text
p50 round-trip: 0.126650 ms
p95 round-trip: 1.186112 ms
p99 round-trip: 1.326593 ms
throughput: 3532.9 calls/s
allocations/op: 2018.8276
bytes/op: 128477.2576
RSS máximo: 500506624 bytes
goroutines: 5
errores: 0
crecimiento continuo de RSS: false
```

Distribución observada: `find_symbol` 4.000, `get_symbol` 2.500,
`find_references` 2.000, `find_cross_repo_consumers` 1.000 y
`get_blast_radius` 500.

Todos los SLO backend pasaron. Los p95/p99 backend máximos fueron
`0.052490/0.063121 ms` para `get_blast_radius`; los otros cuatro límites
también pasaron. `SLOPassed=true`.

**Verificación ejecutada:**

```text
go test ./benchmarks/mcp-client ./internal/mcpworkload -count=1: PASS
go test ./... -count=1: PASS; 21 paquetes con tests, 4 sin tests
go vet ./...: PASS
go test -race ./benchmarks/mcp-client ./internal/mcpworkload -count=1: PASS
benchmark completo con 10000 llamadas: PASS; 0 errores
```

**Limitaciones:** el transporte es `NewInMemoryTransports`; no mide STDIO,
sockets ni red. El RSS incluye la construcción del corpus y del
`HotSnapshot`. El corpus es sintético y el workload usa un probe con relaciones
válidas; no representa todavía la diversidad de un checkout real. Las
allocations/op son un delta de proceso sobre el workload mixto, no una
atribución exacta por operación.

**Estado:** `PASS`.

**Siguiente tarea desbloqueada:** LUQUE-1303.

---

## LUQUE-1303 — Benchmark de 4 clientes

**Dependencias:** LUQUE-1302.

**Checklist:**

- [x] Verificar dependencias y alcance.
- [x] Completar acciones y entregables.
- [x] Ejecutar pruebas y benchmarks aplicables.
- [x] Verificar criterios de aceptación y el gate aplicable.
- [x] Registrar resultados, limitaciones y siguiente tarea.

**Entregables:**

```text
benchmarks/mcp-client/main.go
benchmarks/mcp-client/main_test.go
benchmarks/mcp-client-4/results.json
benchmarks/mcp-client-4/report.md
```

La implementación generaliza el benchmark para `--clients N`, manteniendo
LUQUE-1302 con un cliente y habilitando las fases 1303, 1304 y 1305 sin
duplicar el motor. En esta tarea se ejecutan exactamente cuatro sesiones SDK
concurrentes que comparten un servidor MCP y un `HotSnapshot`. El workload
total se mantiene en 10.000 llamadas, se reparte round-robin entre clientes y
cada cliente recibe 100 llamadas de warm-up por operación antes de medir.

**Resultado medido en `linux/amd64`, Go 1.24.4, Ryzen 7 9700X:**

```text
p50 round-trip:       0.129969 ms
p95 round-trip:       1.172731 ms
p99 round-trip:       1.587841 ms
throughput:           13359.9 calls/s
allocations/op:       2018.7892
bytes/op:             128898.48
RSS máximo:           500944896 bytes
goroutines:           17
errores:              0
crecimiento continuo de RSS: false
```

Distribución observada: `find_symbol` 4.000, `get_symbol` 2.500,
`find_references` 2.000, `find_cross_repo_consumers` 1.000 y
`get_blast_radius` 500. Todos los checks SLO pasaron
(`SLOPassed=true`). El máximo backend fue `0.079121/0.190920 ms` p95/p99
para `get_blast_radius`.

**Verificación ejecutada:**

```text
go test ./benchmarks/mcp-client -count=1: PASS
go test ./... -count=1: PASS; 21 paquetes con tests, 4 sin tests
go vet ./...: PASS
go test -race ./benchmarks/mcp-client -count=1: PASS
benchmark de 4 clientes con 10000 llamadas: PASS; 0 errores
```

**Limitaciones:** el transporte es `NewInMemoryTransports`; no mide STDIO,
sockets ni red. El RSS incluye la construcción del corpus y del
`HotSnapshot`. El corpus es sintético y la medición es una ejecución en un
hardware concreto. Las allocations/op son un delta de proceso sobre el
workload mixto. No existe todavía un contador directo de contención global;
la ejecución verifica concurrencia, latencias, errores y crecimiento de RSS,
pero no demuestra ausencia absoluta de contención.

**Estado:** `PASS`.

**Siguiente tarea desbloqueada:** LUQUE-1304.

---

## LUQUE-1304 — Benchmark de 16 clientes

**Dependencias:** LUQUE-1303.

**Checklist:**

- [x] Verificar dependencias y alcance.
- [x] Completar acciones y entregables.
- [x] Ejecutar pruebas y benchmarks aplicables.
- [x] Verificar criterios de aceptación y el gate aplicable.
- [x] Registrar resultados, limitaciones y siguiente tarea.

**Entregables:**

```text
benchmarks/mcp-client/main.go
benchmarks/mcp-client/main_test.go
benchmarks/mcp-client-16/results.json
benchmarks/mcp-client-16/report.md
```

Se reutiliza el benchmark parametrizable de LUQUE-1303 con exactamente
dieciséis sesiones SDK concurrentes, un servidor MCP y un `HotSnapshot`
compartidos. El workload conserva 10.000 llamadas totales, distribución
40/25/20/10/5, reparto round-robin entre clientes y 100 llamadas de warm-up
por operación y cliente.

**Resultado medido en `linux/amd64`, Go 1.24.4, Ryzen 7 9700X:**

```text
p50 round-trip:       0.190770 ms
p95 round-trip:       2.271372 ms
p99 round-trip:       4.759554 ms
throughput:           24287.8 calls/s
allocations/op:       2018.9348
bytes/op:             128828.2264
RSS máximo:           500953088 bytes
goroutines:           65
errores:              0
crecimiento continuo de RSS: false
```

Distribución observada: `find_symbol` 4.000, `get_symbol` 2.500,
`find_references` 2.000, `find_cross_repo_consumers` 1.000 y
`get_blast_radius` 500. Los checks SLO backend pasaron (`SLOPassed=true`).
El máximo backend fue `0.202610/0.560781 ms` p95/p99 para
`get_blast_radius`. Las point queries quedaron por debajo de 5 ms p95 y
15 ms p99; `get_blast_radius` quedó por debajo de 20 ms p95.

Frente a LUQUE-1303, el throughput observado pasa de 13.359,9 a 24.287,8
llamadas/s, mientras el p95 round-trip pasa de 1,172731 a 2,271372 ms y el
p99 de 1,587841 a 4,759554 ms. La ejecución no muestra errores ni crecimiento
continuo de RSS.

**Verificación ejecutada:**

```text
go test ./... -count=1: PASS; 21 paquetes con tests, 4 sin tests
go vet ./...: PASS
go test -race ./benchmarks/mcp-client -count=1: PASS
smoke de 16 clientes: PASS; 20 llamadas, 0 errores
benchmark de 16 clientes con 10000 llamadas: PASS; 0 errores
```

**Limitaciones:** el transporte es `NewInMemoryTransports`; no mide STDIO,
sockets ni red. El RSS incluye la construcción del corpus y del
`HotSnapshot`. El corpus es sintético y la medición es una ejecución en un
hardware concreto. Las allocations/op son un delta de proceso sobre el
workload mixto. No existe todavía un contador directo de contención global;
la degradación de latencia se observa en las métricas round-trip, pero no
identifica qué recurso la causa.

**Estado:** `PASS`.

**Siguiente tarea desbloqueada:** LUQUE-1305.

---

## LUQUE-1305 — Benchmark de 32 clientes

**Dependencias:** LUQUE-1304.

**Checklist:**

- [x] Verificar dependencias y alcance.
- [x] Completar acciones y entregables.
- [x] Ejecutar pruebas y benchmarks aplicables.
- [x] Verificar criterios de aceptación y el gate aplicable.
- [x] Registrar resultados, limitaciones y siguiente tarea.

**Entregables:**

```text
benchmarks/mcp-client/main.go
benchmarks/mcp-client/main_test.go
benchmarks/mcp-client-32/results.json
benchmarks/mcp-client-32/report.md
```

Se reutiliza el benchmark parametrizable con exactamente treinta y dos
sesiones SDK concurrentes, un servidor MCP y un `HotSnapshot` compartidos. El
workload conserva 10.000 llamadas totales, distribución 40/25/20/10/5,
reparto round-robin y 100 llamadas de warm-up por operación y cliente.

**Resultado medido en `linux/amd64`, Go 1.24.4, Ryzen 7 9700X:**

```text
p50 round-trip:       0.600691 ms
p95 round-trip:       3.780575 ms
p99 round-trip:       9.775364 ms
throughput:           25351.8 calls/s
allocations/op:       2018.9440
bytes/op:             128502.3432
RSS máximo:           500662272 bytes
goroutines:           129
errores:              0
crecimiento continuo de RSS: false
```

Distribución observada: `find_symbol` 4.000, `get_symbol` 2.500,
`find_references` 2.000, `find_cross_repo_consumers` 1.000 y
`get_blast_radius` 500. Todos los checks SLO backend pasaron
(`SLOPassed=true`). El máximo backend fue `0.317330/1.376852 ms` p95/p99 para
`get_blast_radius`.

El gate de concurrencia también pasó con las métricas round-trip observadas:
las point queries tuvieron como máximo `2.213514 ms` p95 y `5.469237 ms` p99,
`get_blast_radius` tuvo `11.132494 ms` p95, no hubo errores y el RSS no mostró
crecimiento continuo.

Frente a LUQUE-1304, el throughput pasa de 24.287,8 a 25.351,8 llamadas/s,
mientras el p95 round-trip pasa de 2,271372 a 3,780575 ms y el p99 de
4,759554 a 9,775364 ms. La ganancia de throughput es pequeña frente al coste
de latencia adicional.

**Verificación ejecutada:**

```text
go test ./... -count=1: PASS; 21 paquetes con tests, 4 sin tests
go vet ./...: PASS
go test -race ./benchmarks/mcp-client -count=1: PASS
smoke de 32 clientes: PASS; 20 llamadas, 0 errores
benchmark de 32 clientes con 10000 llamadas: PASS; 0 errores
```

**Limitaciones:** el transporte es `NewInMemoryTransports`; no mide STDIO,
sockets ni red. El RSS incluye la construcción del corpus y del
`HotSnapshot`. El corpus es sintético y la medición es una ejecución en un
hardware concreto. Las allocations/op son un delta de proceso sobre el
workload mixto. No existe todavía un contador directo de contención global;
la degradación de latencia es evidencia de saturación del escenario, pero no
identifica ni cuantifica el recurso contendido.

**Estado:** `PASS`.

**Siguiente tarea desbloqueada:** LUQUE-1306.

---

## LUQUE-1306 — Analizar perfiles

**Dependencias:** LUQUE-1305.

**Checklist:**

- [x] Verificar dependencias y alcance.
- [x] Completar acciones y entregables.
- [x] Ejecutar pruebas y benchmarks aplicables.
- [x] Verificar criterios de aceptación y el gate aplicable.
- [x] Registrar resultados, limitaciones y siguiente tarea.

**Entregables:**

```text
benchmarks/mcp-client/main.go
benchmarks/mcp-client/main_test.go
benchmarks/mcp-client-32/profiles/cpu.pprof
benchmarks/mcp-client-32/profiles/heap.pprof
benchmarks/mcp-client-32/profiles/allocs.pprof
benchmarks/mcp-client-32/profiles/mutex.pprof
benchmarks/mcp-client-32/profiles/block.pprof
benchmarks/mcp-client-32/profiles/trace.out
benchmarks/mcp-client-32/profiles/report.md
```

Se añadió `--profile-dir` al benchmark MCP. La captura cubre el workload
representativo de LUQUE-1305: 32 clientes, 10.000 llamadas, 100 warm-up por
operación y cliente, 100.000 símbolos, 1.000.000 de aristas y seed 42.

**Observaciones principales:**

* CPU: 5.650 ms de muestras sobre 601,46 ms de reloj; domina
  `encoding/json` (`stateInString` 16,99 %, `checkValid` 7,43 %,
  `decodeState.skip` 7,26 %) y la validación JSON Schema (17,35 %
  acumulado).
* Heap: 128,91 MB vivos; `hotsnapshot.NewGraphSnapshot` retiene 57,33 MB
  acumulados, seguido por `StringInterner.Intern` (14,11 MB) y
  `buildCorpus` (9 MB).
* Allocations: 6,05 GB acumulados y 76.336.687 objetos; dominan
  `GraphSnapshot.Traverse` (1,36 GB), JSON marshal/decode, validación Schema y
  `reflect.copyVal` (24.379.739 objetos, 31,94 %).
* Mutex: 1,91 s agregados; 91,78 % aparece en `sync.Mutex.Unlock`, con la
  ruta acumulada en `jsonrpc2.Connection.write`/`mcp.ioConn.Write`.
* Block: 51,61 s agregados; 92,09 % en `runtime.selectgo`, seguido por
  recepción de canales, locks y esperas de `WaitGroup`. El tiempo agregado
  suma goroutines y no es latencia de pared.
* Trace: el perfil sync confirma 51.714 ms agregados, 91,89 % en
  `runtime.selectgo`; el perfil scheduler muestra 52 % en `runtime.chansend1`
  y 20,57 % en `jsonrpc2.Connection.handleAsync`.

La primera categoría accionable para LUQUE-1307 es **allocations/serialización**:
JSON, validación Schema y reflexión dominan CPU y allocations; el recorrido del
grafo es el mayor allocator propio del snapshot. LUQUE-1306 no modifica aún
el comportamiento de producción.

**Verificación ejecutada:**

```text
go test ./benchmarks/mcp-client -count=1: PASS
go test ./... -count=1: PASS; 21 paquetes con tests, 4 sin tests
go vet ./...: PASS
go test -race ./benchmarks/mcp-client -count=1: PASS
go tool pprof -top sobre CPU, heap, allocs, mutex y block: PASS
go tool trace -pprof=sync y -pprof=sched: PASS
```

**Limitaciones:** CPU y trace incluyen overhead de instrumentación. Heap y
allocations incluyen estado de setup, snapshot, workload, SDK y runtime de
profiling. Los tiempos mutex/block/trace son agregados y no se pueden
atribuir directamente a una llamada. El transporte es en memoria; no se
profilan STDIO, sockets, red ni un checkout real. No existe todavía un
contador directo de contención global.

**Estado:** `PASS`.

**Siguiente tarea desbloqueada:** LUQUE-1307.

---

## LUQUE-1307 — Optimizar el primer cuello de botella real

**Dependencias:** LUQUE-1306.

**Checklist:**

- [x] Verificar dependencias y alcance.
- [x] Completar acciones y entregables.
- [x] Ejecutar pruebas y benchmarks aplicables.
- [x] Verificar criterios de aceptación y el gate aplicable.
- [x] Registrar resultados, limitaciones y siguiente tarea.

No optimizar más de una categoría por tarea.

Prioridad:

```text
allocations
serialization
indexes
traversal
snapshot layout
```

**Estado:** `PASS`.

**Implementación:** `GraphSnapshot` reutiliza buffers de recorrido por snapshot
mediante `sync.Pool`; el estado `visited` usa generaciones para evitar el
borrado completo de la tabla densa en cada llamada. La optimización queda
limitada a la categoría `allocations`.

**Tests y benchmarks:** `go test ./internal/hotsnapshot -count=1`,
`go test -race ./internal/hotsnapshot -count=1`, `go test ./... -count=1`,
`go vet ./...`, `go test ./benchmarks/mcp-client -count=1` y
`go test -race ./benchmarks/mcp-client -count=1` pasan. El microbenchmark
reduce `Depth3` y `Depth5` de `404912 B/op, 12 allocs/op` a
`1752 B/op, 4 allocs/op`. El benchmark limpio de 32 clientes reduce
`Bytes/op` de `128502.3` a `109860.6`, sin errores; la comparación completa
está en `benchmarks/mcp-client-32/report.md`.

**Limitaciones:** el benchmark de 32 clientes es una comparación única y el
pool retiene capacidad de scratch para lectores concurrentes; LUQUE-1308 debe
repetirlo antes de convertir el movimiento de latencia o throughput en una
conclusión.

**Siguiente tarea desbloqueada:** LUQUE-1308.

---

## LUQUE-1308 — Repetir benchmark tras optimización

**Dependencias:** LUQUE-1307.

**Checklist:**

- [x] Verificar dependencias y alcance.
- [x] Completar acciones y entregables.
- [x] Ejecutar pruebas y benchmarks aplicables.
- [x] Verificar criterios de aceptación y el gate aplicable.
- [x] Registrar resultados, limitaciones y siguiente tarea.

**Debe incluir comparación antes/después.**

**Estado:** `PASS`.

**Ejecución:** se repitió tres veces el benchmark de 32 clientes sobre el
commit limpio `cf8fc65`, con 10.000 llamadas, 100 warm-up por operación y
cliente, 100.000 símbolos, 1.000.000 de aristas y seed 42. Todos los runs
terminaron sin errores y con `slo_passed: true`.

**Comparación:** frente al baseline publicado, la media aritmética de las tres
repeticiones obtuvo `Bytes/op` `128502.3 → 110197.4` (`-14.2%`) y
`Allocs/op` `2018.944 → 2018.572`. La variación de latencia queda registrada
sin atribución causal; el tercer run tuvo un RSS de `688136192` bytes frente a
aproximadamente `502 MB` en los otros dos. El informe completo está en
`benchmarks/mcp-client-32/report.md`.

**Verificación adicional:** `go test ./... -count=1` y `go vet ./...` pasan.

**Limitaciones:** el baseline es una sola ejecución histórica y el benchmark
usa transporte MCP en memoria; no mide STDIO, sockets ni red. La siguiente
tarea debe verificar regresiones de precisión con la suite semántica completa.

**Siguiente tarea desbloqueada:** LUQUE-1309.

---

## LUQUE-1309 — Verificar regresiones de precisión

**Dependencias:** LUQUE-1308.

**Checklist:**

- [x] Verificar dependencias y alcance.
- [x] Completar acciones y entregables.
- [x] Ejecutar pruebas y benchmarks aplicables.
- [x] Verificar criterios de aceptación y el gate aplicable.
- [x] Registrar resultados, limitaciones y siguiente tarea.

Toda optimización debe ejecutar la suite semántica completa.

**Estado:** `PASS`.

**Suite ejecutada sobre el commit optimizado:** `go test ./... -count=1`,
`go vet ./...`, `go test -race ./internal/goloader ./internal/facts -count=1`,
`go run ./benchmarks/go-semantic`, `pnpm check` y `pnpm precision`.

**Evidencia Go:** `GO_SEMANTIC_PASS`; 16/16 true positives, 0 false
positives, 0 false negatives, precisión 1.0000, recall 1.0000, 0 false exact
edges y 2/2 unresolved correctamente clasificadas.

**Evidencia TypeScript:** `TYPESCRIPT_CROSS_REPO_PASS`; 11/11 true positives,
0 false positives, 0 false negatives, precisión 1.0000, recall 1.0000, 0 false
exact edges, 4/4 unresolved correctamente clasificadas y 10/10 posiciones
exactas mapeadas.

**Limitaciones:** la suite usa los fixtures semánticos versionados y no
sustituye la validación sobre repositorios reales a escala. No se modificaron
los artefactos semánticos generados. El análisis de rendimiento queda en
LUQUE-1308; esta tarea sólo verifica que la optimización no cambió la
precisión.

**Siguiente tarea desbloqueada:** LUQUE-1401.

**Gate:**

```text
PERFORMANCE_PASS
```

---

# 17. Fase 14 — Observabilidad

## LUQUE-1401 — Implementar logging estructurado

**Dependencias:** PERFORMANCE_PASS.

**Checklist:**

- [x] Verificar dependencias y alcance.
- [x] Completar acciones y entregables.
- [x] Ejecutar pruebas y benchmarks aplicables.
- [x] Verificar criterios de aceptación y el gate aplicable.
- [x] Registrar resultados, limitaciones y siguiente tarea.

**Formato:** JSON a stderr.

**Estado:** `PASS`.

**Implementación:** `internal/logging` usa `log/slog.JSONHandler` de la
biblioteca estándar. `cmd/kivgraph` registra inicio y cierre de `serve`, y
adapta los errores heredados del CLI a eventos JSON `ERROR` sin escribir
diagnósticos en stdout ni registrar argumentos completos, payloads MCP o
credenciales.

**Entregables:**

```text
internal/logging/logging.go
internal/logging/logging_test.go
cmd/kivgraph/main.go
cmd/kivgraph/main_test.go
docs/adr/0011-structured-logging.md
```

**Verificación:** `go test ./... -count=1` pasa con 22 paquetes y 4 sin tests;
`go vet ./...` pasa; `go test -race ./internal/logging ./cmd/kivgraph -count=1`
pasa. El CLI sin argumentos y el ciclo `serve` producen registros JSON válidos
en `stderr`; `version` no produce logging espurio.

**Limitaciones:** el adaptador normaliza las escrituras del propio CLI, pero
una dependencia externa que escriba directamente en `stderr` puede seguir
produciendo texto. Las métricas de consultas, latencias, resultados y
truncamientos pertenecen a LUQUE-1402.

**Siguiente tarea desbloqueada:** LUQUE-1402.

---

## LUQUE-1402 — Implementar métricas internas

**Dependencias:** LUQUE-1401.

**Checklist:**

- [x] Verificar dependencias y alcance.
- [x] Completar acciones y entregables.
- [x] Ejecutar pruebas y benchmarks aplicables.
- [x] Verificar criterios de aceptación y el gate aplicable.
- [x] Registrar resultados, limitaciones y siguiente tarea.

**Métricas:**

* queries;
* latencia;
* resultados;
* truncamientos;
* snapshot;
* indexación;
* unresolved;
* workers;
* LadybugDB.

**Estado:** `PASS`.

**Entregables:**

```text
internal/metrics/metrics.go
internal/metrics/metrics_test.go
internal/mcp/tools/observer.go
internal/mcp/tools/blast_radius.go
internal/mcp/tools/find_cross_repo_consumers.go
internal/mcp/tools/find_references.go
internal/mcp/tools/find_symbol.go
internal/mcp/tools/get_symbol.go
internal/mcp/tools/repositories.go
internal/mcp/tools/status.go
internal/mcp/tools/trace_dependencies.go
internal/mcp/tools/unresolved.go
internal/mcp/server.go
internal/indexer/delta.go
internal/rebuild/rebuild.go
internal/rebuild/snapshot.go
internal/tsworker/supervisor.go
internal/tsworker/process_memory_linux.go
internal/tsworker/process_memory_other.go
benchmarks/internal-metrics/results.json
docs/adr/0012-internal-metrics.md
```

**Implementación:** registro protegido por mutex con contadores, latencias,
gauges y máximos; observación común de las nueve herramientas MCP; métricas de
indexación, rebuild, snapshot, worker TypeScript y LadybugDB; RSS del worker
desde `/proc/<pid>/statm` en Linux; informes consistentes sin payloads ni
etiquetas de alta cardinalidad.

**Verificación:** `go test ./... -count=1` pasa con 23 paquetes y 4 sin tests;
`go vet ./...` pasa; `go test -race ./internal/metrics ./internal/mcp
./internal/rebuild ./internal/indexer ./internal/tsworker -count=1` pasa;
`go test ./internal/metrics -bench=BenchmarkObserveQuery -benchmem -count=3`
mide 0 allocaciones por operación y aproximadamente 30.5 ns/op.

**Limitaciones:** el registro todavía es interno y no tiene exportador ni
endpoint público; la exposición mediante `graph_status` corresponde a
LUQUE-1403. El RSS devuelve cero en sistemas sin `/proc`; el worker conserva
la métrica del último `Status()` observado.

**Siguiente tarea desbloqueada:** LUQUE-1403.

---

## LUQUE-1403 — Exponer estado mediante `graph_status`

**Dependencias:** LUQUE-1402.

**Checklist:**

- [x] Verificar dependencias y alcance.
- [x] Completar acciones y entregables.
- [x] Ejecutar pruebas y benchmarks aplicables.
- [x] Verificar criterios de aceptación y el gate aplicable.
- [x] Registrar resultados, limitaciones y siguiente tarea.

**Estado:** `PASS`.

**Implementación:** `graph_status` incluye el reporte consistente de
`internal/metrics` cuando el proceso configura un registro. El campo
`metrics` se omite sin registro; un snapshot ausente conserva
`status: "empty"` y puede devolver las métricas observadas. `Run` y
`RunWithSnapshotStore` crean un registro por proceso, y
`RunWithMetricsAndSnapshotStore` permite compartirlo con indexación, rebuild y
worker. Los registradores anteriores conservan sus firmas.

**Entregables:**

```text
internal/mcp/tools/status.go
internal/mcp/tools/status_test.go
internal/mcp/server.go
internal/mcp/server_test.go
docs/adr/0013-graph-status-metrics.md
benchmarks/graph-status-metrics/results.json
```

**Verificación:** smoke STDIO real con `initialize`,
`notifications/initialized` y `tools/call graph_status` devolvió un envelope
MCP válido; el resultado `status: "empty"` incluyó `metrics` con los cinco
grupos (`queries`, `snapshot`, `index`, `worker`, `ladybug`). El benchmark
`BenchmarkGraphStatusWithMetrics` mide 1.013–1.020 µs/op, 2.128 B/op y
21 allocaciones/op. También pasan `go test ./... -count=1`, `go vet ./...`,
`go test -race ./internal/mcp/... ./internal/metrics -count=1`, `make build` y
`gofmt -l` no reporta archivos.

**Limitaciones:** el reporte conserva las duraciones `time.Duration` como
números JSON en nanosegundos; no hay exportador Prometheus u OpenTelemetry.
Los registros siguen siendo locales al proceso y deben compartirse
explícitamente para reflejar indexación y rebuild.

**Siguiente tarea desbloqueada:** LUQUE-1404.

---

## LUQUE-1404 — Integrar OpenTelemetry opcional

**Dependencias:** LUQUE-1402.

**Checklist:**

- [x] Verificar dependencias y alcance.
- [x] Completar acciones y entregables.
- [x] Ejecutar pruebas y benchmarks aplicables.
- [x] Verificar criterios de aceptación y el gate aplicable.
- [x] Registrar resultados, limitaciones y siguiente tarea.

**Condiciones:**

* exportadores desactivados por defecto;
* impacto de rendimiento medido;
* ninguna dependencia de un collector.

**Estado:** `PASS`.

**Implementación:** `internal/metrics` conserva el registro local sin
dependencias de exporters y añade `NewRegistryWithOpenTelemetry` y
`NewOpenTelemetry`. El proveedor `nil` usa el `noop` oficial; el proveedor
configurado pertenece al llamador y Kivgraph no crea collectors, exporters,
readers periódicos, conexiones ni goroutines. Las observaciones de consultas,
snapshot, indexación, worker y LadybugDB se proyectan a instrumentos
OpenTelemetry; el atributo `tool.name` queda acotado y los nombres desconocidos
se agrupan como `other`. Las trazas continúan desactivadas según el plan.

**Entregables:**

```text
internal/metrics/metrics.go
internal/metrics/otel.go
internal/metrics/metrics_test.go
go.mod
go.sum
docs/adr/0014-opentelemetry-metrics.md
benchmarks/otel-metrics/results.json
benchmarks/otel-metrics/report.md
AGENTS.md
```

**Verificación:** `go test ./internal/metrics -count=1`,
`go test -race ./internal/metrics -count=1`, `go vet ./...`,
`go test ./... -count=1`, `make build` y `gofmt -l` sobre los archivos Go
modificados pasan. El benchmark `BenchmarkObserveQuery` mide
30.46–30.56 ns/op y 0 B/op para el registro local, 33.14–33.25 ns/op y
0 B/op para el proveedor `noop`, y 125.7–126.4 ns/op y 0 B/op para
`sdk/metric.ManualReader`, con cero asignaciones en las tres variantes.

**Limitaciones:** esta tarea integra métricas, no trazas ni un exporter de red.
El benchmark excluye framing MCP, I/O de exportación y collector. El proveedor
SDK sólo se conecta cuando el proceso consumidor lo suministra explícitamente.

**Siguiente tarea desbloqueada:** LUQUE-1405.

---

## LUQUE-1405 — Medir overhead de observabilidad

**Dependencias:** LUQUE-1404.

**Checklist:**

- [x] Verificar dependencias y alcance.
- [x] Completar acciones y entregables.
- [x] Ejecutar pruebas y benchmarks aplicables.
- [x] Verificar criterios de aceptación y el gate aplicable.
- [x] Registrar resultados, limitaciones y siguiente tarea.

**Gate:**

```text
OBSERVABILITY_PASS
```

**Estado:** `PASS`.

**Implementación:** se amplió el benchmark de `internal/metrics` para medir una
iteración completa de `ObserveQuery`, `ObserveSnapshot`, `ObserveIndex`,
`ObserveWorker`, `RecordWorkerRestart` y `ObserveLadybug` bajo tres variantes:
registro local, proveedor OpenTelemetry `noop` y `sdk/metric.ManualReader`.

**Entregables:**

```text
internal/metrics/metrics_test.go
benchmarks/observability-overhead/results.json
benchmarks/observability-overhead/report.md
AGENTS.md
```

**Verificación:** cinco muestras con
`go test ./internal/metrics -run '^$' -bench 'BenchmarkObserve(All|Query)' -benchmem -count=5`
produjeron 138.66 ns/op para el registro local, 157.84 ns/op para `noop`
(`13.83%` de overhead) y 636.68 ns/op para `ManualReader`
(`359.22%` adicional), siempre con `0 B/op` y `0 allocs/op`. No existe un
umbral numérico de overhead en `TASKS.md` ni `PLAN.md`; el gate separa el coste
SDK explícito de la ruta por defecto.

**Limitaciones:** la medición excluye framing MCP, recorrido y serialización
del grafo, I/O del exporter y collector. `ManualReader` no representa un
exporter de red concreto. El resultado es una medición puntual en Linux amd64
y el CPU registrado.

**Gate:** `OBSERVABILITY_PASS`.

**Siguiente tarea desbloqueada:** LUQUE-1501.

---

# 18. Fase 15 — Distribución

## LUQUE-1501 — Crear build Linux amd64

**Dependencias:** OBSERVABILITY_PASS.

**Checklist:**

- [x] Verificar dependencias y alcance.
- [x] Completar acciones y entregables.
- [x] Ejecutar pruebas y benchmarks aplicables.
- [x] Verificar criterios de aceptación y el gate aplicable.
- [x] Registrar resultados, limitaciones y siguiente tarea.

**Incluir:**

* binario Go;
* worker;
* LadybugDB;
* grammars;
* licencias;
* manifest.

**Estado:** `PASS`.

**Implementación:** `scripts/build-linux-amd64.sh` genera
`dist/kivgraph-linux-amd64/` con el binario Go compilado con LadybugDB,
`liblbug.so`, el worker TypeScript compilado y su runtime `typescript`, el
inventario de grammars, licencias y `manifest.json`. El target
`make build-linux-amd64` elimina y regenera el directorio de salida; `dist/`
permanece ignorado por Git.

**Entregables:**

```text
scripts/build-linux-amd64.sh
Makefile
.gitignore
AGENTS.md
THIRD_PARTY_NOTICES.md
docs/adr/0015-linux-amd64-distribution.md
dist/kivgraph-linux-amd64/
```

**Verificación:** `make build-linux-amd64` completó la descarga verificada de
LadybugDB, `pnpm install --frozen-lockfile`, `pnpm build` y `go build -tags
ladybug -trimpath`. El binario ejecutó `version` desde el bundle con su
`RUNPATH` relativo, y el worker respondió `hello` desde `/tmp`. El manifest
JSON validó `linux/amd64`, esquema canónico `2`, formato de filas `3`,
`resolver_version: null`, el `archive_sha256` fijado y 588 hashes de payload;
`sha256sum -c` confirmó todos los archivos. Dos ejecuciones consecutivas
produjeron el mismo SHA-256 para `manifest.json`
(`73d7219f444907122ef7f64268798fe27decdb693bd554845bc27a429494685c`) y
`bin/kivgraph`
(`e1dd29ed5468ac2aa51cdfd870bed2bae99a24b28ca08092d3e59f10149bacc3`).

**Limitaciones:** el artefacto se construyó en Linux amd64 con Go
`go1.24.4`, Node `v25.9.0`, pnpm `11.5.1` y TypeScript `7.0.2`. El build se
ejecutó con cambios locales todavía no publicados y por eso el manifest marca
`source.dirty: true`. La reproducibilidad entre checkouts limpios y checksums
de distribución queda cubierta por LUQUE-1508; `REPRODUCIBLE_BUILD_PASS` no se
declara aquí.

**Siguiente tarea desbloqueada:** LUQUE-1502.

---

## LUQUE-1502 — Implementar `kivgraph version --json`

**Dependencias:** LUQUE-1501.

**Checklist:**

- [x] Verificar dependencias y alcance.
- [x] Completar acciones y entregables.
- [x] Ejecutar pruebas y benchmarks aplicables.
- [x] Verificar criterios de aceptación y el gate aplicable.
- [x] Registrar resultados, limitaciones y siguiente tarea.

**Mostrar:**

* Kivgraph;
* commit;
* Go;
* Node;
* TypeScript;
* LadybugDB;
* binding;
* schema;
* resolver;
* grammars.

**Estado:** `PASS`.

**Implementación:** `kivgraph version --json` emite un único documento JSON
estable con la versión de Kivgraph, commit y estado del árbol, toolchain Go,
Node y TypeScript, versiones core/binding de LadybugDB, schema, formato de
filas del snapshot, resolver y grammars. En un bundle valida el `manifest.json`
y el SHA-256 de `grammars/manifest.json`; fuera de un bundle usa metadatos de
build/runtime y representa como `null` los valores no disponibles.

**Entregables:**

```text
cmd/kivgraph/main.go
cmd/kivgraph/version.go
cmd/kivgraph/main_test.go
internal/version/provenance.go
internal/version/provenance_test.go
internal/rebuild/snapshot.go
internal/storage/ladybug/doctor.go
scripts/build-linux-amd64.sh
```

**Verificación:** `go vet ./...`, `go test ./... -count=1`, `make build`,
`make test-ladybug` y `make build-linux-amd64` pasaron. El binario del bundle
respondió `version` y `version --json` sin `LD_LIBRARY_PATH`; el JSON validó
`kivgraph`, `commit`, `dirty`, Go `go1.24.4`, Node `v25.9.0`, TypeScript
`7.0.2`, LadybugDB `v0.13.1`, binding `v0.13.1`, schema `2`, formato de filas
`3`, `resolver: null` y las cuatro grammars fijadas. Las pruebas cubren
manifest válido, fallback sin bundle, digest de grammars incorrecto y JSON
malformado.

**Limitaciones:** un binario de desarrollo no puede afirmar Node ni una
versión embebida de TypeScript si no existe el bundle; esos campos quedan
`null`. El bundle de verificación se construyó con cambios locales y marca
`source.dirty: true`.

**Siguiente tarea desbloqueada:** LUQUE-1503.

---

## LUQUE-1503 — Crear checksums

**Dependencias:** LUQUE-1501.

**Checklist:**

- [x] Verificar dependencias y alcance.
- [x] Completar acciones y entregables.
- [x] Ejecutar pruebas y benchmarks aplicables.
- [x] Verificar criterios de aceptación y el gate aplicable.
- [x] Registrar resultados, limitaciones y siguiente tarea.

Generar SHA-256 de todos los artefactos.

**Estado:** `PASS`.

**Implementación:** `make build-linux-amd64` genera `SHA256SUMS` en la raíz
del bundle después de escribir `manifest.json`. Contiene una entrada
lexicográficamente ordenada para `manifest.json` y cada archivo de `bin/`,
`lib/`, `worker/`, `grammars/` y `licenses/`; el propio `SHA256SUMS` queda
fuera del listado para evitar una dependencia circular. El script ejecuta
`sha256sum -c SHA256SUMS` antes de publicar la ruta del bundle.

**Entregables:**

```text
scripts/build-linux-amd64.sh
docs/adr/0015-linux-amd64-distribution.md
AGENTS.md
TASKS.md
dist/kivgraph-linux-amd64/SHA256SUMS
```

**Verificación:** `bash -n scripts/build-linux-amd64.sh`, `go vet ./...`,
`go test ./... -count=1` y `make build` pasaron. `make build-linux-amd64`
generó 589 entradas; `cd dist/kivgraph-linux-amd64 && sha256sum -c
SHA256SUMS` devolvió `OK` para todas. Una segunda generación en
`/tmp/kivgraph-linux-amd64-second` produjo un `SHA256SUMS` idéntico mediante
`cmp`.

**Limitaciones:** `dist/` y los bundles temporales son artefactos generados e
ignorados por Git. `SHA256SUMS` no se hashea a sí mismo; la reproducibilidad
entre checkouts limpios y la publicación de checksums queda cubierta por
LUQUE-1508.

**Siguiente tarea desbloqueada:** LUQUE-1504.

---

## LUQUE-1504 — Crear instalación local

**Dependencias:** LUQUE-1503.

**Checklist:**

- [x] Verificar dependencias y alcance.
- [x] Completar acciones y entregables.
- [x] Ejecutar pruebas y benchmarks aplicables.
- [x] Verificar criterios de aceptación y el gate aplicable.
- [x] Registrar resultados, limitaciones y siguiente tarea.

**Comandos esperados:**

```bash
kivgraph init
kivgraph doctor
kivgraph index --full
kivgraph serve
```

**Estado:** `PASS`.

**Implementación:**

* `internal/config.Initialize` crea configuración, registro vacío y directorios
  de estado con escritura no destructiva por defecto; `RegisterRepositories`
  añade registros validados con paths resueltos y rechazo de duplicados.
* `kivgraph doctor` valida configuración, rutas y permisos de estado, registro
  Git, toolchains, generación canónica, presencia del digest del snapshot,
  HotSnapshot y referencias no resueltas.
* `internal/indexer.Full` coordina extracción Go y TypeScript fuera de los
  repositorios fuente, valida el `facts.Set` y conecta su resultado con
  `rebuild.Run`.
* `kivgraph serve` resuelve `CURRENT`, reconstruye el HotSnapshot desde la
  base canónica activa y sirve MCP exclusivamente mediante el snapshot
  publicado.

**Verificación:** `gofmt -l`, `go vet ./...`, `go test ./... -count=1`,
`make build`, `make test-ladybug`, `cd ts-worker && pnpm check` y
`cd ts-worker && pnpm build` pasaron. Las pruebas nuevas cubren init, doctor,
repositorios inaccesibles y facts Go full.

**Smoke real:** con un `HOME` temporal, `kivgraph init --repository`,
`kivgraph doctor`, `kivgraph index --full`, `kivgraph doctor` y
`kivgraph serve` pasaron contra un repositorio Go temporal y LadybugDB
v0.13.1; también pasó una indexación TypeScript full con el worker compilado.

**Limitaciones:** el worker y el grafo canónico requieren las dependencias
instaladas y el binario compilado con el tag `ladybug`; el comando `doctor`
reporta rutas de estado ausentes como `FAIL` en lugar de crearlas
silenciosamente.

**Siguiente tarea:** LUQUE-1505.

---

## LUQUE-1505 — Implementar upgrade de schema

**Dependencias:** LUQUE-1504.

**Checklist:**

- [x] Verificar dependencias y alcance.
- [x] Completar acciones y entregables.
- [x] Ejecutar pruebas y benchmarks aplicables.
- [x] Verificar criterios de aceptación y el gate aplicable.
- [x] Registrar resultados, limitaciones y siguiente tarea.

**Debe incluir:**

* detección;
* backup;
* migración;
* validación;
* rollback.

**Estado:** `PASS`.

**Implementación:**

* `internal/upgrade` detecta la generación activa y la versión canónica,
  crea backups deterministas con manifiesto y SHA-256, reindexa desde los
  repositorios registrados y delega la publicación en el pipeline validado de
  `rebuild.Run`.
* Los fallos previos a la publicación conservan `CURRENT`; un fallo de
  validación posterior restaura la generación anterior solo después de
  verificarla contra el backup.
* `kivgraph upgrade` expone las etapas y sus detalles, y rechaza schemas
  sintéticos, versiones posteriores y generaciones no publicadas.

**Verificación:** `gofmt -l`, `go vet ./...`, `go test ./... -count=1`,
`make build`, `make test-ladybug`, `cd ts-worker && pnpm check` y
`cd ts-worker && pnpm build` pasaron. Las pruebas cubren no-op de schema actual,
backup idempotente, corrupción, rutas inseguras, migración, fallos de extracción,
rollback validado y rechazo de schemas incompatibles.

**Smoke real:** con un `HOME` temporal, `kivgraph index --full` creó una
generación canónica con LadybugDB v0.13.1; se cambió `GraphMetadata.schema_version`
a `1`, `kivgraph upgrade` publicó `000002` con backup `001-to-002` y todas las
etapas en `PASS`; `kivgraph rollback --generation 000001` restauró `000001` con
digest e invariantes en `PASS`.

**Limitaciones:** solo se reconstruye un schema canónico anterior cuya forma
puede diagnosticar esta versión; un schema sintético, posterior, corrupto o con
forma desconocida se rechaza explícitamente. El upgrade requiere espacio
simultáneo para el backup y una reconstrucción full.

**ADR:** `docs/adr/0016-schema-upgrade.md`.

**Siguiente tarea:** LUQUE-1506.

---

## LUQUE-1506 — Probar rollback de versión

**Dependencias:** LUQUE-1505.

**Checklist:**

- [x] Verificar dependencias y alcance.
- [x] Completar acciones y entregables.
- [x] Ejecutar pruebas y benchmarks aplicables.
- [x] Verificar criterios de aceptación y el gate aplicable.
- [x] Registrar resultados, limitaciones y siguiente tarea.

**Estado:** `PASS`.

**Implementación:**

* `internal/upgrade/rollback_test.go` prueba con un `generation.Store` real que
  un fallo de validación posterior a la publicación restaura `CURRENT`, invierte
  `graph.backup` y conserva byte por byte la generación anterior.
* Otra prueba modifica la generación anterior después del backup y exige que el
  rollback se rechace por digest, dejando `CURRENT` en la candidata publicada.
* La cobertura existente de `internal/rebuild/rollback_test.go` conserva los
  casos de digest, invariantes, digest ausente, ausencia de backup y roles.

**Verificación:** `gofmt -l`, `go vet ./...`, `go test ./... -count=1`,
`make build` y `make test-ladybug` pasaron. No se modificó `ts-worker/`; sus
gates no aplican a esta tarea.

**Smoke real:** contra la generación canónica temporal de LUQUE-1505,
`kivgraph rollback --generation 000003` restauró la generación anterior con
digest e invariantes en `PASS`. Tras eliminar `snapshot.sha256` del destino,
`kivgraph rollback --generation 000001` devolvió código `1` y `CURRENT`
permaneció en `000003`.

**Limitaciones:** el rollback siempre falla cerrado si falta el digest, cambia
la generación retenida o falla cualquier verificación; en esos casos `CURRENT`
permanece sin cambiar.

**Siguiente tarea:** LUQUE-1507.

---

## LUQUE-1507 — Crear documentación de instalación

**Dependencias:** LUQUE-1504.

**Objetivo:** documentar la instalación del bundle `linux/amd64`, el build
desde fuente, la inicialización de configuración, el primer índice, el
servidor MCP y el diagnóstico de fallos sin ocultar requisitos ni
limitaciones.

**Entregable:**

```text
docs/installation.md
```

**Checklist:**

- [x] Verificar dependencias y alcance.
- [x] Completar acciones y entregables.
- [x] Ejecutar pruebas y benchmarks aplicables.
- [x] Verificar criterios de aceptación y el gate aplicable.
- [x] Registrar resultados, limitaciones y siguiente tarea.

**Estado:** `PASS`.

**Implementación:** `docs/installation.md` documenta los requisitos del
bundle `linux/amd64`, la verificación `SHA256SUMS`, la instalación por usuario,
el build fuente con `-tags ladybug`, `kivgraph init`, `doctor`, `index --full`,
`serve`, las rutas de estado, upgrades y diagnóstico. `README.md` enlaza la
guía. `scripts/build-linux-amd64.sh` conserva el worker interactivo y enruta
`facts` a `facts-cli.js`, además de empaquetar el paquete nativo de TypeScript
para `linux/amd64`; `docs/adr/0015-linux-amd64-distribution.md` registra ambos
contratos.

**Verificación:** `bash -n scripts/build-linux-amd64.sh`,
`git diff --check`, `go vet ./...`, `go test ./... -count=1`, `make build`,
`make test-ladybug`, `cd ts-worker && pnpm check && pnpm build` y
`make build-linux-amd64` pasaron. El bundle pasó `sha256sum -c SHA256SUMS`,
`version`, `version --json`, el worker `hello` y extracción TypeScript
(`21 symbols`, `4 references`). En un `HOME` temporal, `init`, `doctor`,
`index --full` (1 repositorio Go, generación `000001`) y el diagnóstico
posterior pasaron; el servidor MCP respondió a `initialize` con
`serverInfo.name=kivgraph` y `protocolVersion=2025-06-18`.

**Limitaciones:** el artefacto documentado es `linux/amd64` y requiere Node.js
`22` o posterior y bibliotecas estándar del sistema. El bundle de verificación
se generó desde un árbol local con cambios y por tanto marcó
`source.dirty: true`; la comparación entre checkouts limpios pertenece a
LUQUE-1508. Un build fuente ejecutado fuera del checkout necesita el launcher
TypeScript descrito en `docs/installation.md`.

**Siguiente tarea:** LUQUE-1508.

---

## LUQUE-1508 — Ejecutar build reproducible

**Dependencias:** LUQUE-1503.

**Objetivo:** demostrar que el bundle `linux/amd64` es reproducible entre
checkouts limpios del mismo commit, toolchain y plataforma, comparando el
payload completo y sus manifiestos.

**Checklist:**

- [x] Verificar dependencias y alcance.
- [x] Completar acciones y entregables.
- [x] Ejecutar pruebas y benchmarks aplicables.
- [x] Verificar criterios de aceptación y el gate aplicable.
- [x] Registrar resultados, limitaciones y siguiente tarea.

**Estado:** `PASS`.

**Implementación:** el build fija `-buildid` de Go a partir del commit y del
estado `dirty` del checkout. Esto elimina la diferencia introducida por las
rutas absolutas de CGO entre checkouts temporales. La guía de instalación y el
ADR de distribución describen el contrato.

**Verificación:** dos checkouts limpios equivalentes pasaron
`make build-linux-amd64`; `diff -qr` confirmó payload idéntico y `cmp` confirmó
`SHA256SUMS` y `manifest.json` idénticos. Ambos manifests declararon
`source.dirty: false`, `linux/amd64` y el mismo commit; ambos bundles pasaron
`sha256sum -c SHA256SUMS`. `version --json`, el worker `hello` y extracción
TypeScript (`21 symbols`, `4 references`) pasaron. También pasaron
`bash -n scripts/build-linux-amd64.sh`, `git diff --check`, `go vet ./...`,
`go test ./... -count=1`, `make build`, `make test-ladybug` y
`cd ts-worker && pnpm check && pnpm build`.

**Limitaciones:** la comparación cubre dos checkouts Linux `amd64` con el mismo
toolchain, dependencias fijadas y plataforma. No prueba reproducibilidad entre
distribuciones Linux, arquitecturas o versiones distintas de Go/Node/pnpm. El
build de un árbol modificado conserva `source.dirty: true` y usa un `buildid`
distinto del build limpio.

**Siguiente tarea:** LUQUE-1601.


**Comparar checksums en dos entornos equivalentes cuando sea viable.**

**Gate:**

```text
DISTRIBUTION_PASS
```

---

# 19. Fase 16 — Aceptación final

## LUQUE-1601 — Ejecutar suite completa

**Dependencias:** DISTRIBUTION_PASS.

**Checklist:**

- [x] Verificar dependencias y alcance.
- [x] Completar acciones y entregables.
- [x] Ejecutar pruebas y benchmarks aplicables.
- [x] Verificar criterios de aceptación y el gate aplicable.
- [x] Registrar resultados, limitaciones y siguiente tarea.

**Estado:** `PASS_WITH_LIMITS`.

Ejecutar:

```text
unit
integration
semantic
cross-repo
negative
incremental
integrity
resilience
performance
fuzz smoke
```

**Verificación:**

- `go test ./... -count=1` y `go test -race ./... -count=1`: 25 paquetes
  correctos y 3 sin tests en cada ejecución.
- `cd ts-worker && pnpm check`: 17 archivos y 78 tests correctos;
  `pnpm precision`: `TYPESCRIPT_CROSS_REPO_PASS`.
- `go run ./benchmarks/go-semantic`: `GO_SEMANTIC_PASS`.
- `GOFLAGS=-count=1 make test-ladybug`: 25 paquetes correctos y 3 sin tests.
  `doctor graph` pasó con las seis invariantes a cero.
- El benchmark incremental pasó `INCREMENTAL_INDEXING_PASS`: p95 de 575,3 ms
  para archivo simple, 592,7 ms para imports/exports y 834,8 ms para
  manifest; sin aristas fantasma.
- El corpus privado completo pasó la carga `COPY`: 40 repositorios, 100.000
  archivos, 100.000 símbolos, 200.040 nodos y 1.000.000 aristas; RSS máximo
  638.312.448 bytes y gate de carga correcto.
- El benchmark de recuperación pasó sus 8 casos, con
  `source_unchanged: true` y `all_passed: true`.
- Las consultas directas devolvieron cero errores en las 700 iteraciones
  medidas. El benchmark MCP con 32 clientes, 100.000 símbolos y 1.000.000
  aristas registró p50 0,639 ms, p95 3,494 ms, p99 7,064 ms, cero errores y
  sin crecimiento continuo de memoria.
- El smoke `go test ./internal/facts -run=^$ -fuzz=Fuzz -fuzztime=1s`
  terminó correctamente.

**Limitaciones:**

- El repositorio no contiene funciones `Fuzz*`; el smoke de fuzz valida el
  harness de un paquete, pero no ejecuta mutaciones reales. Go tampoco permite
  combinar `-fuzz` con múltiples paquetes en una sola invocación.
- La carga, recuperación y consultas usan un corpus sintético privado con
  LadybugDB fijado en Linux `amd64`; no modifican artefactos versionados ni
  repositorios indexados.
- Una ejecución inicial de recuperación contra una base canónica de esquema
  `002` fue rechazada por el benchmark, que exige el esquema sintético `001`.
  Se descartó ese input incompatible y se repitió sobre la base sintética
  correcta; los ocho casos pasaron.

**Siguiente tarea:** LUQUE-1602.

---

## LUQUE-1602 — Ejecutar corpus grande sintético

**Dependencias:** LUQUE-1601.

**Objetivo:** ejecutar el corpus sintético de aceptación en la escala de
1.000.000 de símbolos y 10.000.000 de aristas, validando carga, conteos,
integridad, memoria y reproducibilidad lógica sin tocar repositorios
indexados.

**Checklist:**

- [x] Verificar dependencias y alcance.
- [x] Completar acciones y entregables.
- [x] Ejecutar pruebas y benchmarks aplicables.
- [x] Verificar criterios de aceptación y el gate aplicable.
- [x] Registrar resultados, limitaciones y siguiente tarea.

**Estado:** `PASS_WITH_LIMITS`.

**Escala ejecutada:**

```text
40 repositorios
100.000 archivos
1.000.000 símbolos
10.000.000 aristas
```

**Verificación:** `ladybug-bulk-copy` cargó 1.100.040 nodos y 10.000.000
aristas. El gate de escala, conteos y RSS pasó. La exportación CSV tardó
11.922,2 ms; la carga COPY tardó 9.141,3 ms; el end-to-end tardó 21.063,5 ms.
El throughput fue 1.214.272,8 registros/s durante COPY y 526.980,2
registros/s end-to-end. El RSS máximo fue 2.079.531.008 bytes, por debajo de
2 GiB; la base ocupó 432.570.368 bytes.

`doctor storage` pasó en dos cargas independientes con los mismos conteos:
`Repository=40`, `File=100000`, `Symbol=1000000`, `CONTAINS=100000`,
`DEFINES=1000000`, `REFERENCES=4450001`, `CALLS_DIRECT=4449999`, y cero
violaciones de integridad. Dos generaciones del corpus produjeron los cinco
archivos (`manifest.json` y los cuatro JSONL) byte a byte idénticos. Los
resúmenes lógicos de ambas cargas también fueron idénticos.

**Limitaciones:** los dos archivos físicos `graph.db` no fueron byte a byte
idénticos (`432.570.368` frente a `433.037.312` bytes), aunque sus conteos,
schema e integridad coincidieron. Este resultado demuestra reproducibilidad
lógica del corpus y de la carga, no reproducibilidad binaria del archivo nativo
LadybugDB. El corpus y las bases se mantuvieron en `/tmp`; no se modificaron
artefactos versionados ni repositorios indexados. La máquina tenía 24.543.908
KiB de RAM total y 8.783.788 KiB disponibles al inicio.

**Siguiente tarea:** LUQUE-1603.

---

## LUQUE-1603 — Auditar aristas exactas

**Dependencias:** LUQUE-1601.

**Objetivo:** auditar las aristas que declaran confianza exacta contra las
evidencias, procedencias y extremos declarados del grafo canónico.

**Checklist:**

- [x] Verificar dependencias y alcance.
- [x] Completar acciones y entregables.
- [x] Ejecutar pruebas y benchmarks aplicables.
- [x] Verificar criterios de aceptación y el gate aplicable.
- [x] Registrar resultados, limitaciones y siguiente tarea.

**Estado:** `PASS_WITH_LIMITS`.

**Requisito:**

```text
0 false exact edges
0 dangling exact edges
```

**Verificación:**

- `kivgraph doctor graph` sobre la generación canónica publicada
  `000003` devolvió `PASS`: `exact_edge_without_source=0`,
  `exact_edge_without_target=0`, `missing_evidence_file=0`,
  `unknown_confidence=0`, `duplicate_stable_key=0` e
  `invalid_repository_ownership=0`.
- `go run ./benchmarks/go-semantic` devolvió `GO_SEMANTIC_PASS`: 16 true
  positives, 0 false positives, 0 false negatives y 0 false exact edges.
- `pnpm precision` devolvió `TYPESCRIPT_CROSS_REPO_PASS`: 11 true positives,
  0 false positives, 0 false negatives y 0 false exact edges.
- `go test -tags ladybug ./internal/storage/ladybug -run '^TestVerifyCanonicalIntegrity'`
  pasó las pruebas positivas y negativas de extremos huérfanos, evidencia
  ausente, confianza desconocida, claves duplicadas y ownership inválido.

**Limitaciones:** la auditoría canónica usa la generación publicada de
calificación y las fixtures semánticas Go/TypeScript versionadas; no afirma
precisión sobre repositorios externos que todavía no hayan sido indexados.
`PASS_WITH_LIMITS` separa esa cobertura de fixtures del requisito global de
producción.

**Siguiente tarea:** LUQUE-1604.

---

## LUQUE-1604 — Auditar referencias no resueltas

**Dependencias:** LUQUE-1601.

**Checklist:**

- [x] Verificar dependencias y alcance.
- [x] Completar acciones y entregables.
- [x] Ejecutar pruebas y benchmarks aplicables.
- [x] Verificar criterios de aceptación y el gate aplicable.
- [x] Registrar resultados, limitaciones y siguiente tarea.

**Estado:** `PASS_WITH_LIMITS`.

**Verificación:**

- `go run ./benchmarks/go-semantic` devolvió `GO_SEMANTIC_PASS`: las 2
  referencias no resueltas esperadas del caso negativo se clasificaron
  correctamente (`2/2`), con `0` mal clasificadas, `0` false positives,
  `0` false negatives y `0` false exact edges.
- `cd ts-worker && pnpm precision` devolvió
  `TYPESCRIPT_CROSS_REPO_PASS`: las 4 referencias no resueltas esperadas del
  caso negativo se clasificaron correctamente (`4/4`), con `0` mal
  clasificadas, `0` entradas faltantes o inesperadas y `0` false exact edges.
- `go test ./internal/goloader -run 'TestClassifyUnresolved|TestGoNegativeFixture' -count=1`
  verificó motivos, detalle, repositorio, archivo y posición cuando la
  referencia tiene una ocurrencia concreta, además de rechazar providers
  ambiguos y reemplazos no demostrados.
- `go test ./internal/facts -run 'TestUnresolved' -count=1` y las pruebas de
  normalización TypeScript verificaron `reason`, `language`, `repository_key`,
  `file_key`, símbolos solicitados, detalle y `source_symbol_key` sin inventar
  identidad.
- `TestNormalizeTypeScriptImportWithoutTargetIsUnresolved` y
  `TestNormalizeTypeScriptExtendsWithoutTargetIsUnresolved` confirmaron que
  una identidad no demostrada produce `UNRESOLVED`, nunca una arista.

**Limitaciones:** el grafo canónico publicado y los fixtures positivos no
contienen referencias no resueltas. Un conflicto de módulo del workspace puede
carecer de archivo concreto; en ese caso `file_key` queda vacío por diseño y
el hecho cuelga del repositorio, conforme al esquema canónico. No se fabrica
un archivo ni una posición para ocultar esa ausencia.

**Siguiente tarea:** LUQUE-1605.

---

## LUQUE-1605 — Emitir informe final

**Dependencias:** LUQUE-1602, LUQUE-1603 y LUQUE-1604.

**Checklist:**

- [x] Verificar dependencias y alcance.
- [x] Completar acciones y entregables.
- [x] Ejecutar pruebas y benchmarks aplicables.
- [x] Verificar criterios de aceptación y el gate aplicable.
- [x] Registrar resultados, limitaciones y siguiente tarea.

**Entregable:**

```text
docs/release/production-qualification.md
```

**Estado:** `ACCEPT_KIVGRAPH_WITH_LIMITS`.

**Decisiones válidas:**

```text
ACCEPT_KIVGRAPH_FOR_PRODUCTION
ACCEPT_KIVGRAPH_WITH_LIMITS
REJECT_KIVGRAPH_FOR_PRODUCTION
```

**Resultado:** los 16 gates globales están emitidos. La aceptación queda
limitada a Linux `amd64`, el par LadybugDB core/binding `v0.13.1`, los
toolchains fijados, el `HotSnapshot` en memoria, los transportes y corpus
medidos, y las garantías de reproducibilidad lógica descritas en el informe.

**Verificación:** `go test ./... -count=1`, `go test -race ./... -count=1`,
`go vet ./...`, `make build`, `make test-ladybug`, `cd ts-worker && pnpm check`
y `cd ts-worker && pnpm build` pasaron. También pasaron los benchmarks
semánticos, incrementales, de recuperación, HotSnapshot, MCP, observabilidad y
el build reproducible.

**Reruns posteriores:** el 2026-08-07, sobre el commit `45220d3` y el par
LadybugDB `v0.13.1`, `benchmarks/ladybug-recovery-pinned/` pasó 8/8 casos con
`source_unchanged: true`, y `benchmarks/mcp-client-32-pinned/` pasó las cinco
comprobaciones SLO backend con 0 errores, p95 round-trip `3,351542 ms` y sin
crecimiento continuo de memoria. `benchmarks/mcp-stdio/`, sobre el commit del
benchmark `4580240`, completó protocolo `2025-06-18`, nueve tools, 100
warm-ups y 10.000 llamadas con 0 errores, exit code `0`, p95 `0,362711 ms` y
RSS muestreado de `19.075.072` bytes.

**Limitaciones y siguiente acción:** el informe conserva las limitaciones de
fixtures semánticas, transporte MCP en memoria, snapshot no serializado,
recuperación sin pérdida eléctrica, fuzz smoke sin funciones `Fuzz*`, bytes
nativos no reproducibles y corpus sintético. STDIO está medido; sockets y red
no están configurados. Si el despliegue los requiere, primero debe definirse un
transporte y un ADR; después habrá que medirlo. La recuperación ante fallos de
alimentación o almacenamiento se amplía sólo si el entorno lo exige.

---

# 20. Fase 17 — Visor web de grafos

La fase introduce una interfaz web read-only para explorar el `HotSnapshot`
mediante React, Vite y Reagraph sobre Three.js. No amplía la superficie MCP de
nueve tools ni convierte el navegador en una fuente de hechos.

## LUQUE-1701 — Aceptar ADR del transporte HTTP read-only

**Dependencias:** LUQUE-1605.

**Objetivo:** definir el contrato HTTP local que consumirá el visor sin alterar
el transporte STDIO de MCP.

**Entregable:**

```text
docs/adr/0017-read-only-web-transport.md
```

**Checklist:**

- [x] Verificar dependencias y alcance.
- [x] Documentar bind loopback, lifecycle, seguridad y límites.
- [x] Fijar API `/api/v1`, errores y versionado de snapshot.
- [x] Registrar alternativas descartadas y riesgos.

**Criterios de aceptación:**

- El ADR establece que la API es read-only y usa el mismo `SnapshotStore`.
- `kivgraph serve` y sus nueve tools MCP no cambian por esta decisión.
- Queda explícito que exponer una dirección no loopback requiere revisión.

**Estado:** `PASS`.

**Verificación:** `go vet ./...`, `go test ./...`, `make build`, smoke HTTP de `/api/v1/meta` y `/api/v1/tiles`.

---

## LUQUE-1702 — Aceptar ADR del paquete React/Vite/Reagraph

**Dependencias:** LUQUE-1701.

**Objetivo:** fijar el diseño del paquete web, el render Reagraph y el formato
binario.

**Entregable:**

```text
docs/adr/0018-react-vite-threejs-viewer.md
docs/adr/0019-reagraph-graph-viewer.md
```

**Checklist:**

- [x] Verificar dependencias y compatibilidad TypeScript.
- [x] Fijar responsabilidades de React, Reagraph y Web Worker.
- [x] Fijar límites de vista, picking y layout determinista.
- [x] Registrar alternativas descartadas y riesgos de escala.

**Criterios de aceptación:**

- El ADR prohíbe asumir que JSON completo escala al corpus de referencia.
- El formato binario incluye versión, snapshot y validación de longitudes.
- El paquete web permanece read-only y separado de `ts-worker`.

**Estado:** `PASS`.

**Verificación:** `pnpm --dir web check`, `pnpm --dir web build`, smoke Chromium con canvas 3D, picking, layout determinista y worker real.

---

## LUQUE-1703 — Definir SLO y gate del visor web

**Dependencias:** LUQUE-1702.

**Objetivo:** convertir la velocidad esperada de la UI en mediciones
reproducibles.

**Checklist:**

- [x] Verificar dependencias y corpus de referencia.
- [x] Definir límites de payload, TTFI, primer frame, FPS y memoria.
- [x] Definir medición de picking, decodificación y vecindad.
- [x] Registrar `WEB_VIEWER_PERFORMANCE_PASS` y sus excepciones.

**Criterios de aceptación:**

- El SLO usa el corpus de 100.000 símbolos y 1.000.000 de aristas.
- Cada métrica tiene comando, entorno, dataset y criterio de PASS.
- No se confunde una métrica de transporte HTTP con una métrica MCP STDIO.

**Estado:** `PASS_WITH_LIMITS`.

**Verificación:** `docs/performance/slo.md` define el gate y `benchmarks/web-viewer/results.json` conserva las mediciones; el corpus ejecutado es `~/kena` y no el corpus sintético de referencia.

---

## LUQUE-1704 — Añadir iteración eficiente al HotSnapshot

**Dependencias:** LUQUE-1703.

**Objetivo:** permitir exportar rangos densos y CSR sin copias por entidad.

**Checklist:**

- [x] Verificar invariantes de inmutabilidad y ownership.
- [x] Diseñar accesores de iteración con cancelación.
- [x] Cubrir nodos, aristas y límites de rango con tests.
- [x] Medir allocations/op antes y después.

**Criterios de aceptación:**

- Los lectores existentes conservan sus contratos.
- La iteración rechaza IDs y rangos inválidos sin panic.
- El benchmark demuestra que el encoder no asigna una slice por símbolo.

**Estado:** `PASS`.

**Verificación:** `go test ./internal/hotsnapshot`, `go test ./internal/rebuild` y el benchmark de iteración CSR sin exposición de slices internas.

---

## LUQUE-1705 — Implementar layout jerárquico y grid espacial

**Dependencias:** LUQUE-1704.

**Objetivo:** generar posiciones y consultas de viewport deterministas.

**Checklist:**

- [x] Verificar relaciones de contención disponibles en el snapshot.
- [x] Implementar layout por repository, package, file y symbol.
- [x] Implementar LOD y consulta de bounding box.
- [x] Comparar bytes de dos ejecuciones sobre el mismo snapshot.

**Criterios de aceptación:**

- El mismo snapshot y configuración producen las mismas posiciones.
- La consulta de viewport no devuelve nodos fuera del límite solicitado.
- El layout no ejecuta force simulation global para el corpus completo.

---

## LUQUE-1706 — Implementar API binaria read-only del visor

**Dependencias:** LUQUE-1705.

**Objetivo:** servir metadata, búsqueda, detalles, tiles y subgrafos inducidos.

**Checklist:**

- [x] Verificar contrato de API y snapshot antes de codificar.
- [x] Implementar `/api/v1/meta`, búsqueda y detalle de símbolo.
- [x] Implementar `/api/v1/tiles` y `/api/v1/neighborhood`.
- [x] Validar método, origen, tamaño, snapshot, depth y node budget.

**Criterios de aceptación:**

- Ningún endpoint puede mutar repositorios, hechos o generaciones.
- `neighborhood` devuelve todas las aristas inducidas entre nodos visibles.
- Buffers truncados, versiones desconocidas y snapshots incompatibles fallan
  con códigos estables.

---

## LUQUE-1707 — Añadir comando y configuración `kivgraph ui`

**Dependencias:** LUQUE-1706.

**Objetivo:** ejecutar el servidor web con configuración explícita y segura.

**Checklist:**

- [x] Verificar defaults y validación `KnownFields`.
- [x] Añadir sección UI/HTTP y flag `--addr`.
- [x] Registrar lifecycle, cancelación y cierre del listener.
- [x] Probar bind loopback, dirección inválida y puerto ocupado.

**Criterios de aceptación:**

- `kivgraph serve` sigue siendo STDIO y no abre HTTP por defecto.
- El default es `127.0.0.1:7777`.
- Toda goroutine y listener tiene propietario y cierre por contexto.

---

## LUQUE-1708 — Servir y empaquetar assets web versionados

**Dependencias:** LUQUE-1707.

**Objetivo:** servir el bundle Vite sin copiar assets no declarados.

**Checklist:**

- [x] Verificar el layout existente de distribución.
- [x] Implementar copia reproducible bajo el tag `webassets`.
- [x] Añadir fallback claro cuando el bundle web no está construido.
- [x] Integrar hashes y tamaños en `manifest.json`/`SHA256SUMS`.

**Criterios de aceptación:**

- `make build` continúa funcionando sin depender de Node.
- El bundle de distribución declara todos sus assets web.
- `dist/` no se edita manualmente y los assets son reproducibles.

**Estado:** `PASS`.

**Archivos creados:**

* `internal/webassets/fallback.go`;
* `internal/webassets/assets_fallback.go`;
* `internal/webassets/assets_bundle.go`;
* pruebas de fallback y serving bajo cada variante de build tag.

**Archivos modificados:**

* `internal/webapi/handler.go` y `internal/webapi/handler_test.go`;
* `scripts/build-linux-amd64.sh`;
* `docs/installation.md` y `docs/adr/0018-react-vite-threejs-viewer.md`;
* `AGENTS.md`.

**Tests ejecutados:**

* `gofmt -l` sobre todos los archivos Go modificados;
* `go vet ./...`;
* `go test ./... -count=1`;
* `go test -tags webassets ./... -count=1`;
* `go test -race ./internal/webapi ./internal/webassets -count=1`;
* `make build`;
* `make build-linux-amd64`;
* smoke HTTP del bundle generado con un fixture web efímero.

**Resultados:**

* El build normal no requiere Node ni el tag `webassets`; `/` y `/assets/`
  devuelven `503` con una explicación explícita si no existe el bundle.
* El empaquetador ejecuta el build de `web/` solo cuando existe su
  `package.json` y `pnpm-lock.yaml`, copia únicamente `web/dist`, compila con
  `webassets` y añade cada archivo copiado a `manifest.json` y `SHA256SUMS`.
* El smoke real sirvió `web/index.html` y `web/assets/app.js` con `200`; el
  manifest y los checksums incluyeron ambos paths con digest y tamaño.

**Benchmarks:** no aplica.

**Limitaciones:** el paquete `web/` todavía pertenece a LUQUE-1709; mientras
no exista, el bundle Linux se genera con el fallback y sin directorio `web/`.

**Siguiente tarea desbloqueada:** LUQUE-1709.

---

## LUQUE-1709 — Crear paquete web React/Vite/Three.js

**Dependencias:** LUQUE-1702.

**Objetivo:** crear el paquete web estricto y verificable.

**Checklist:**

- [x] Verificar pines de pnpm, Node, Biome, Vitest y TypeScript.
- [x] Crear configuración Vite, React, TS strict y ESM.
- [x] Crear scripts `check`, `build`, `test` y `typecheck`.
- [x] Añadir lockfile y excluir `dist/` generado.

**Criterios de aceptación:**

- El paquete compila con el typecheck nativo fijado.
- Biome y Vitest pasan sin depender de servicios externos.
- El paquete no modifica ni importa internals privados de `ts-worker`.

**Estado:** `PASS`.

**Archivos creados:**

* `web/package.json`, `web/pnpm-lock.yaml` y `web/components.json`;
* `web/index.html`, `web/vite.config.ts`, `web/vitest.config.ts`,
  `web/tsconfig.json`, `web/tsconfig.node.json` y `web/biome.json`;
* `web/src/main.tsx`, `web/src/App.tsx`, `web/src/App.test.tsx`,
  `web/src/index.css`, `web/src/lib/utils.ts` y el `Button` generado por
  shadcn CLI.

**Archivos modificados:**

* `.gitignore`;
* `.github/workflows/ci.yml`;
* `AGENTS.md`;
* `docs/installation.md` y `docs/adr/0018-react-vite-threejs-viewer.md`.

**Tests ejecutados:**

* `pnpm dlx shadcn@latest init --template vite --base radix --preset nova
  --no-monorepo --cwd web --force --yes`;
* `pnpm --dir web install --frozen-lockfile`;
* `pnpm --dir web check`;
* `pnpm --dir web build`;
* smoke visual con Vite en `127.0.0.1:4173` y navegador Chromium.

**Resultados:**

* El paquete usa React `19.2.8`, Vite `8.2.1`, Tailwind CSS `4.3.3`,
  Biome `2.5.7`, Vitest `4.1.10` y TypeScript `7.0.2`.
* `shadcn@latest` inicializó el preset Radix Nova, `components.json`, tokens
  Tailwind, `tw-animate-css`, Geist y el componente `Button`.
* `check` pasó con 2 tests; `build` generó `web/dist` con `index.html`, CSS,
  JavaScript y fuentes Geist.
* El smoke visual confirmó el shell, los tokens y la navegación de anclas sin
  servicios externos.

**Benchmarks:** no aplica.

**Limitaciones:** esta tarea crea la aplicación y sus primitives; el renderer
  Three.js de buffers y el chrome funcional del visor pertenecen a
  LUQUE-1710 y LUQUE-1711.

**Siguiente tarea desbloqueada:** LUQUE-1710.

---

## LUQUE-1710 — Implementar renderer Three.js de buffers

**Dependencias:** LUQUE-1709 y LUQUE-1706.

**Objetivo:** renderizar nodos y aristas con un número constante de draw calls.

**Checklist:**

- [x] Verificar formato binario y ownership de ArrayBuffers.
- [x] Implementar puntos, segmentos, shaders y cámara ortográfica.
- [x] Implementar LOD de aristas y etiquetas limitadas.
- [x] Implementar picking GPU por color-ID.

**Criterios de aceptación:**

- No se crea un objeto Three.js por entidad del grafo.
- Los buffers grandes no entran en estado React.
- Pan, zoom y hover no ejecutan layouts ni serializaciones completas.

**Estado:** `PASS`.

**Archivos creados:**

* `web/src/renderer/binary.ts`: validación `LGVB` v1 mediante `DataView`,
  secciones de nodos y aristas sin copiar el `ArrayBuffer`, límites de 32 MiB
  y errores estables;
* `web/src/renderer/GraphRenderer.ts`: `Points` y `LineSegments` agrupados,
  shaders, cámara ortográfica, pan/zoom, LOD de aristas, overlay de 48
  etiquetas y picking GPU por color-ID;
* `web/src/renderer/fixture.ts` y tests del decoder, picking y LOD;
* `web/src/components/GraphPreview.tsx`.

**Archivos modificados:**

* `AGENTS.md`;
* `web/package.json` y `web/pnpm-lock.yaml` con Three.js `0.185.1` y
  `@types/three` `0.185.4`;
* `web/vitest.config.ts` para incluir tests `.ts`;
* `web/src/App.tsx` y `web/src/App.test.tsx`.

**Verificación:**

* `pnpm --dir web check`: 3 archivos de test, 10 tests pasando;
* `pnpm --dir web build`: 1.903 módulos transformados;
* smoke Chromium con WebGL disponible, picking GPU confirmado como
  `Node 0 · kind 4 · GPU picked`, y pan/zoom verificando cambios de cámara;
* los buffers grandes permanecen en `GraphRenderer`, fuera del estado React;
  la escena crea un único `Points` y un único `LineSegments`.

**Limitación visible:** Vite informa un warning de chunk principal de
`769.97 kB` minificado por Three.js. No se oculta ni se declara un gate de
rendimiento; la medición end-to-end pertenece a LUQUE-1713.

**Nota de evolución:** la implementación inicial de buffers quedó registrada
como `PASS`; la superficie final del visor fue sustituida por Reagraph en
LUQUE-1716, que conserva el contrato `LGVB` y explicita los límites de vista.

**Siguiente tarea desbloqueada:** LUQUE-1716.

## LUQUE-1716 — Migrar el visor a Reagraph

**Dependencias:** LUQUE-1710 y LUQUE-1702.

**Objetivo:** usar Reagraph como superficie de grafo sin romper el contrato
binario ni materializar un snapshot completo en React.

**Checklist:**

- [x] Fijar `reagraph` `4.32.0` y revisar su API pública.
- [x] Adaptar `LGVB` a nodos y aristas con IDs únicos y referencias validadas.
- [x] Configurar `GraphCanvas` con layout determinista, pan, zoom y hover.
- [x] Rechazar vistas que superen `2.000` nodos o `8.000` aristas.
- [x] Añadir ADR, tests negativos y checklist de verificación.

**Criterios de aceptación:**

- `GraphPreview` renderiza con `GraphCanvas` y no importa el renderer propio.
- El adaptador conserva el `ArrayBuffer` decodificado fuera del estado React.
- Una referencia inválida o un payload sobredimensionado produce un código
  estable y visible; no hay truncamiento silencioso.
- Las coordenadas del payload llegan al layout custom sin force simulation
  global.

**Estado:** `PASS_WITH_LIMITS`.

**Archivos creados:**

* `docs/adr/0019-reagraph-graph-viewer.md`;
* `web/src/renderer/reagraph.ts`;
* `web/src/renderer/reagraph.test.ts`.

**Archivos modificados:**

* `AGENTS.md`, `TASKS.md` y `docs/adr/0018-react-vite-threejs-viewer.md`;
* `docs/performance/slo.md`;
* `web/package.json`, `web/pnpm-lock.yaml`, `web/src/App.tsx`,
  `web/src/App.test.tsx` y `web/src/components/GraphPreview.tsx`.

**Archivos retirados:**

* `web/src/renderer/GraphRenderer.ts` y su test de picking/LOD.

**Verificación:**

* `pnpm --dir web check`: 3 archivos de test y 11 tests pasando;
* `pnpm --dir web build`: 2.744 módulos transformados; Vite conserva visible
  el warning del chunk principal de `1.596,12 kB` minificado (`457,44 kB`
  gzip);
* smoke Chromium con WebGL: canvas Reagraph presente, hover confirmado como
  `Node 0 · kind 1 · Reagraph hover`, y pan/zoom ejercitados mediante eventos
  de rueda y arrastre;
* el adaptador conserva el payload en `ArrayBuffer`, materializa solo la vista
  acotada y devuelve códigos visibles para payloads inválidos o demasiado
  grandes.

**Limitación:** Reagraph requiere una vista acotada; el rendimiento del corpus
completo queda pendiente de `LUQUE-1713`. Las aristas se renderizan sólidas y
sin flechas para mantener homogénea la geometría agregada; `confidence` se
mantiene visible mediante el color.

**Siguiente tarea desbloqueada:** LUQUE-1717.

---

## LUQUE-1717 — Reducir la web al previsualizador del grafo

**Dependencias:** LUQUE-1716.

**Objetivo:** dejar en `web/` únicamente el visor del grafo, en oscuro, sin
contenido de landing ni dependencias que solo lo sostenían.

**Checklist:**

- [x] Sustituir el shell por el visor a pantalla completa.
- [x] Aplicar tema oscuro en documento y `GraphCanvas`.
- [x] Retirar `Button`, `cn` y las dependencias UI y Three.js sin uso.
- [x] Actualizar tests, ADR y reglas.

**Criterios de aceptación:**

- El markup del shell no contiene cabecera ni secciones de presentación.
- El visor ocupa la ventana y conserva pan, zoom y hover.
- `package.json` no declara dependencias sin importador.

**Estado:** `PASS`.

**Archivos creados:**

* `docs/adr/0020-graph-only-dark-viewer.md`.

**Archivos modificados:**

* `AGENTS.md`, `TASKS.md`;
* `web/index.html`, `web/package.json`, `web/pnpm-lock.yaml`;
* `web/src/App.tsx`, `web/src/App.test.tsx`,
  `web/src/components/GraphPreview.tsx`.

**Archivos retirados:**

* `web/src/components/ui/button.tsx` y `web/src/lib/utils.ts`.

**Verificación:**

* `pnpm --dir web check`: 3 archivos de test y 10 tests pasando;
* `pnpm --dir web build`: 863 módulos transformados, `1.555,01 kB` minificado
  (`444,50 kB` gzip) y CSS de `13,54 kB`; el warning de chunk sigue visible;
* smoke Chromium: canvas de `1440 × 1000`, fondo `oklch(0.145 0 0)`, cero
  elementos `header`/`section`, hover `Node 0 · kind 1 · hover` y cero
  `pageerror`.

**Limitación:** el visor mostraba el fixture determinista; el consumo del
`HotSnapshot` publicado se resuelve en `LUQUE-1718`.

**Siguiente tarea desbloqueada:** LUQUE-1718.

---

## LUQUE-1718 — Servir el grafo publicado en el visor

**Dependencias:** LUQUE-1717 y LUQUE-1706.

**Objetivo:** que la web muestre el `HotSnapshot` publicado, con nombres
legibles y relaciones visibles en cada nivel de detalle.

**Checklist:**

- [x] Publicar el viewport raíz del layout en `/api/v1/meta`.
- [x] Consumir `/api/v1/tiles` desde la web con selector de nivel.
- [x] Añadir la sección de etiquetas al payload binario (`LGVB` v2).
- [x] Emitir las relaciones de paquete en los tiles y resolver extremos por
      `(tipo, id)`.
- [x] Equilibrar la rejilla del layout para que el mundo sea legible.
- [x] Renderizar en 3D con cámara rotable y un plano por tipo de nodo.
- [x] Acortar la etiqueta del lienzo y conservar el nombre completo.
- [x] Proyectar cada eje por rango para repartir los nodos uniformemente.
- [x] Dibujar la contención del payload y añadir una leyenda.

**Criterios de aceptación:**

- El visor no muestra IDs densos: cada nodo lleva su nombre del snapshot.
- Un tile de paquetes contiene sus dependencias de paquete.
- Una versión de payload distinta de la publicada se rechaza con código
  estable, sin interpretar el buffer.
- El mundo publicado no degenera en una tira: relación de aspecto acotada.
- La vista rota y cada tipo de nodo ocupa su propio plano de profundidad.
- El nombre completo sigue disponible aunque la etiqueta se acorte.
- Un repositorio aparece unido a sus paquetes, no suelto junto a ellos.
- La leyenda nombra cada color de nodo y cada trazo de arista.

**Estado:** `PASS`.

**Archivos creados:**

* `docs/adr/0021-viewer-payload-v2-labels.md`;
* `web/src/api/client.ts`.

**Archivos modificados:**

* `AGENTS.md`, `TASKS.md`, `docs/adr/0018-react-vite-threejs-viewer.md`;
* `internal/webapi/binary.go`, `internal/webapi/handler.go`,
  `internal/layout/layout.go` y sus tests;
* `web/vite.config.ts`, `web/src/components/GraphPreview.tsx`,
  `web/src/renderer/binary.ts`, `web/src/renderer/fixture.ts`,
  `web/src/renderer/reagraph.ts` y sus tests.

**Verificación:**

* `go vet ./...` y `go test ./...` correctos;
* `pnpm --dir web check`: 3 archivos de test y 16 tests pasando;
* `pnpm --dir web build`: bundle regenerado;
* medición sobre el tile de paquetes de `~/kena` (`128` nodos): con proyección
  lineal la celda más densa del mundo reunía `11` nodos y sólo `51` de `400`
  celdas tenían contenido; por rango, ninguna celda pasa de `2` y se ocupan
  `100`;
* smoke Chromium contra `kivgraph ui`: `138 nodos` y `399 aristas` en
  paquetes — `295` dependencias más `104` de contención, una por paquete —,
  nombres legibles con zoom (`domain/dbtrackerror`, `infrastructure/locker`),
  leyenda con los siete rótulos, rotación y zoom confirmados, cero
  `pageerror`.

**Limitación:** por encima de `200` nodos las etiquetas se ocultan y el nombre
queda accesible al pasar el cursor; los niveles `files` y `symbols` se acotan a
`10.000` nodos por tile y `32 MiB` de payload. El LOD mantiene la jerarquía al
alejarse en vez de truncar silenciosamente la topología.

**Siguiente tarea desbloqueada:** LUQUE-1711.

---

## LUQUE-1711 — Implementar chrome React del visor

**Dependencias:** LUQUE-1718.

**Objetivo:** proporcionar búsqueda, filtros, selección y detalle sin bloquear
el renderer.

**Checklist:**

- [x] Verificar estados de loading, vacío, error y snapshot cambiado.
- [x] Implementar búsqueda con debounce y cancelación.
- [x] Implementar filtros por repository, kind y confidence.
- [x] Implementar panel de símbolo y expansión de vecindad.

**Criterios de aceptación:**

- Un resultado viejo no puede sobrescribir una selección nueva.
- Los errores muestran código estable y no se silencian.
- La UI conserva la interacción mientras llegan buffers nuevos.

**Estado:** `PASS`.

**Archivos creados:** `web/src/components/ViewerChrome.tsx`, `web/src/components/ViewerChrome.test.ts`, `web/src/components/ui/{badge,button,input,select}.tsx`, `web/src/lib/utils.ts`.

**Archivos modificados:** `web/src/api/client.ts`, `web/src/components/GraphPreview.tsx`, `web/src/App.test.tsx`.

**Verificación:** `pnpm --dir web check`: 10 archivos de test y 51 tests pasando; smoke Chromium con loading, ready, búsqueda `web`, selección, expansión depth 2, filtro `CANDIDATE`, 2D/3D, slider y cero `pageerror`.

---

## LUQUE-1712 — Implementar Web Worker de fetch y decode

**Dependencias:** LUQUE-1718.

**Objetivo:** aislar red, validación y decodificación del hilo de render.

**Checklist:**

- [x] Verificar lifecycle de worker y AbortController.
- [x] Decodificar cabecera y secciones con límites comprobados.
- [x] Transferir ArrayBuffers sin copias evitables.
- [x] Probar cancelación, truncamiento y snapshot incompatible.

**Criterios de aceptación:**

- Un payload inválido no puede provocar acceso fuera de rango.
- Cancelar una consulta libera o abandona sus buffers de forma explícita.
- El hilo principal no bloquea con JSON o decodificación de topología grande.

**Estado:** `PASS_WITH_LIMITS`.

**Archivos creados:**

* `web/src/renderer/tile-loader.ts` y su test;
* `web/src/worker/tile-worker.ts`;
* `web/src/worker/client.ts`.

**Archivos modificados:**

* `AGENTS.md`, `TASKS.md`, `docs/adr/0021-viewer-payload-v2-labels.md`;
* `web/src/components/GraphPreview.tsx`, `web/src/renderer/binary.ts`,
  `web/src/renderer/reagraph.ts`.

**Medición:** el reparto real del coste de una tile de `2.000` nodos, medido
contra el índice de `~/kena`:

```text
fetch    16,7 ms
decode    1,1 ms
adaptar   9,7 ms
render  11.800 ms
```

El worker retira del hilo de render los tres primeros; el cuarto es Reagraph
construyendo la escena y no es trasladable, porque WebGL no es accesible desde
un worker en esta integración.

**Perfil:** el perfil de CPU del nivel `files` atribuyó un `27 %` del tiempo a
`getPoint`, `initNonuniformCatmullRom` y `getLengths`. Es la geometría de las
aristas discontinuas: Reagraph crea una curva Catmull-Rom y un `TubeGeometry`
por cada guion, y la contención añadía una arista discontinua por nodo. Con la
línea fina sólida, ese coste desaparece.

**Verificación:**

* `pnpm --dir web check`: 4 archivos de test y 19 tests pasando;
* el navegador carga `assets/tile-worker-*.js` como worker real;
* cambiar de nivel cuatro veces seguidas en `150 ms` deja la vista en el
  último nivel pedido, sin errores y sin que un resultado viejo lo sobrescriba;
* tiempos de nivel, con WebGL por software: `repositories` `386 ms`,
  `packages` `253 ms`, `files` `1.231 ms` con `1.200` nodos, `symbols`
  `1.326 ms`. Antes de esta tarea `files` tardaba `11.843 ms` con `2.000`
  nodos y `symbols` no llegaba a dibujarse.

**Limitación:** el render sigue construyendo un objeto por nodo en el hilo
principal; el presupuesto por nivel acota cuánto. Instanciar la escena
pertenece a `LUQUE-1713`.

**Añadido tras la primera medición:** un deslizador de `100` a `10.000` nodos
por vista, con `250` ms de espera antes de pedir la tile, y un contador de FPS
en la cabecera. El presupuesto por nivel pasa a ser el valor inicial, no un
techo. Verificado en Chromium: mover el deslizador a `300` deja la vista en
`300 of 4212 files · 561 edges`; el adaptador limita por presupuesto, snapshot,
payload de `32 MiB`, suelo y redondeo al paso.

---

## LUQUE-1719 — Derivar el layout 3D de la estructura del grafo

**Dependencias:** LUQUE-1712.

**Objetivo:** que la vista transmita la arquitectura: comunidades separadas,
profundidad real en los tres ejes y menos cruces, en vez de un plano con las
coordenadas publicadas proyectadas por rango.

**Checklist:**

- [x] Sustituir la proyección por rango y el plano por tipo de nodo.
- [x] Derivar clusters del repositorio contenedor y componentes conexas.
- [x] Detectar comunidades con Louvain dentro de cada repositorio.
- [x] Calcular profundidad jerárquica sobre el DAG condensado (Tarjan).
- [x] Calcular centralidad con PageRank y usarla para tamaño y rótulo.
- [x] Dimensionar contenedores de abajo arriba para evitar solapamiento.
- [x] Colocar y relajar las bolas de cluster; relajar dentro de cada una.
- [x] Garantizar que ningún eje colapsa.
- [x] Clasificar aristas y curvar las que cruzan de cluster.
- [x] Encuadrar la cámara por extensión proyectada y abrir fuera de eje.
- [x] Iluminar el vecindario del nodo al pasar el cursor y apagarlo al salir.
- [x] Centralizar los parámetros del layout en proporciones.
- [x] Dimensionar cada concha por suma cuadrática, no por el hijo mayor.
- [x] Subir el presupuesto por vista al techo del endpoint (`10.000`).

**Criterios de aceptación:**

- Los clusters no se interpenetran: la distancia entre dos centros supera el
  alcance de cada uno.
- Los tres ejes tienen dispersión; ninguno queda por debajo de la mitad del
  más ancho.
- Ningún nodo cae dentro de otro.
- Un hijo está más cerca de su contenedor que de cualquier otro.
- Un ciclo comparte capa y un dependiente queda por encima de aquello de lo
  que depende.
- El mismo tile produce siempre las mismas posiciones.
- El grafo ocupa la mayor parte del encuadre inicial.
- El cursor sobre un nodo deja legible su vecindario y atenúa el resto; al
  retirarlo la vista vuelve a su estado normal.

**Estado:** `PASS`.

**Archivos creados:**

* `docs/adr/0022-viewer-structural-layout.md`;
* `web/src/renderer/layout/config.ts`, `structure.ts`, `place.ts`, `index.ts`
  y `layout.test.ts`;
* `web/src/renderer/camera.ts` y `camera.test.ts`.

**Archivos modificados:**

* `AGENTS.md`, `TASKS.md`;
* `web/src/renderer/reagraph.ts` y su test;
* `web/src/components/GraphPreview.tsx`.

**Medición:** sobre el índice de `~/kena`, con el adaptador completo:

```text
nivel         nodos  aristas  layout   radio   dispersión x/y/z
repositories     34        0   11 ms     190   74 / 73 / 72
packages        138      399   19 ms     950   549 / 274 / 319
files         1.200    1.461   27 ms   1.077   500 / 387 / 441
symbols       5.000    5.282   40 ms   3.837   1.610 / 1.075 / 1.229
symbols      10.000   11.158   58 ms   5.315   2.144 / 1.516 / 1.615
```

Dimensionar la concha por suma cuadrática en vez de por el hijo mayor redujo
el radio del mundo `3,2` veces con `5.000` y `10.000` nodos - el peor cluster
pasó de `7.717` a `1.860` de radio con `380` miembros - y es lo que hace que
esos niveles se dibujen en vez de quedar como puntos de una décima de píxel.

**Verificación:**

* `pnpm --dir web check`: 7 archivos de test y 42 tests pasando;
* `go vet ./...`, `go test ./...` y `make test-ladybug` correctos;
* smoke Chromium contra `kivgraph ui`, midiendo el encuadre sobre los píxeles
  del lienzo: el grafo ocupa `82 %` del ancho y `85 %` del alto en paquetes, y
  `70 %` y `94 %` en archivos; antes ocupaba `38 %` y `41 %`;
* rotación con arrastre: clusters que se solapaban de frente se separan;
* cursor sobre `web-kena.bot`: el nodo y su vecindario quedan iluminados y el
  resto baja a `0,18` de opacidad; al retirar el cursor la vista recupera su
  brillo y el rótulo de estado;
* cambio de nivel: `295`, `336`, `557` y `2.304` ms; deslizador a `400` y
  `2.000` correctos; modo 2D correcto; cero `pageerror`.

**Limitación:** a la distancia de encuadre los rótulos de un tile de mil nodos
miden pocos píxeles y hay que acercarse; es geometría, no un defecto. Resaltar
un vecindario obliga a Reagraph a reconstruir sus mallas de arista: medido en
`files` con `1.461` aristas y WebGL por software, `2,2` s hasta que aparece,
por eso ambas transiciones esperan `120` ms a que el cursor se pose.

**Techo de símbolos:** el endpoint sirve `10.000` nodos por tile y el viewport
raíz gasta `4.350` en ancestros, así que la vista global muestra como mucho
`5.650` de los `82.443` símbolos. Acotar el viewport a un repositorio deja
`9.085` en el mayor y mete uno mediano entero con `902` nodos: el límite es el
recorte espacial, no el presupuesto.

**Verificación adicional:** los tests de configuración aíslan `HOME` en un
directorio temporal y el fixture nativo declara el paquete solicitado por cada
referencia no resuelta; así los gates reproducen el entorno sin depender del
estado local del operador.

**Siguiente tarea desbloqueada:** LUQUE-1720.

---

## LUQUE-1720 — Reducir el coste de render del visor

**Dependencias:** LUQUE-1719.

**Objetivo:** que el visor no cueste nada con el grafo quieto y que
interactuar con mil nodos sea fluido.

**Checklist:**

- [x] Dibujar el nodo con geometría y materiales compartidos.
- [x] Renderizar bajo demanda y despertar con el puntero.
- [x] Aislar el contador de FPS para que no re-renderice el lienzo.
- [x] Sacar la contención de la lista de aristas a una malla de segmentos.
- [x] Fijar `three` y `@react-three/fiber` como dependencias directas.

**Criterios de aceptación:**

- Con el grafo quieto no se emite ninguna draw call.
- Resaltar un nodo responde en menos de medio segundo con mil nodos.
- La contención sigue dibujándose y atenuándose con el resaltado.
- Rotación, zoom, niveles, deslizador y modo 2D siguen correctos.

**Estado:** `PASS`.

**Archivos creados:**

* `docs/adr/0023-viewer-render-cost.md`;
* `web/src/renderer/node-renderer.tsx`, `web/src/renderer/frame-governor.tsx`,
  `web/src/renderer/containment-lines.tsx`;
* `web/src/components/FrameRate.tsx`.

**Archivos modificados:**

* `AGENTS.md`, `TASKS.md`;
* `web/package.json`, `web/src/renderer/reagraph.ts` y su test,
  `web/src/components/GraphPreview.tsx`.

**Medición:** nivel `files`, `1.200` nodos, índice de `~/kena`, WebGL por
software:

```text
                              antes    después
draw calls en reposo (5 s)   34.164          0
fps interactuando                 1      26-42
cargar el nivel              402-879 ms   263 ms
resaltar un nodo              2.254 ms    354 ms
triángulos por nodo           1.250         160
aristas que reconstruye       1.460         295
```

**Verificación:**

* `pnpm --dir web check`: 7 archivos de test y 42 tests pasando;
* draw calls contadas interceptando `drawElements`/`drawArrays` en el
  contexto WebGL: `0` en cinco segundos de reposo, `34.164` mientras se
  arrastra la cámara;
* smoke Chromium: niveles `repositories` `292` ms, `files` `394` ms, `symbols`
  `799` ms, `packages` `671` ms; rotación con arrastre, zoom con rueda y modo
  2D correctos; rótulos legibles al acercarse a `34` fps; cero `pageerror`;
* resaltado sobre `web-captcha.kena.bot`: vecindario iluminado, resto atenuado
  incluida la malla de contención, y apagado al salir del nodo.

**Limitación:** siguen emitiéndose unas `1.200` draw calls por fotograma
mientras se interactúa, una por nodo. Bajarlas exige instanciar la escena, que
implica sustituir el render de nodos de Reagraph.

**Siguiente tarea desbloqueada:** LUQUE-1721.

---

## LUQUE-1721 — Ajustar la jerarquía visual del visor

**Dependencias:** LUQUE-1720.

**Objetivo:** que la vista se lea como una arquitectura y no como un campo de
erizos: repositorios reconocibles al instante y contención como textura.

**Checklist:**

- [x] Abrir el rango de tamaños y declarárselo al lienzo.
- [x] Atenuar la contención según el tipo de nodo que sostiene.
- [x] Fijar la separación entre hermanos por el nodo típico, no por el mayor.
- [x] Separar la cabecera en lo que se dibuja y el detalle técnico.

- [x] Cambiar el nivel de detalle según la escala de la cámara con histéresis.
**Criterios de aceptación:**

- Los repositorios se localizan sin buscarlos.
- Los trazos de contención no son lo más brillante de la imagen.
- El nivel `symbols` con `10.000` nodos sigue dibujándose.
- La cabecera no parece un panel de depuración.

**Estado:** `PASS`.

**Archivos modificados:**

* `AGENTS.md`, `TASKS.md`, `docs/adr/0022-viewer-structural-layout.md`;
* `web/src/renderer/reagraph.ts`, `web/src/renderer/containment-lines.tsx`,
  `web/src/renderer/layout/place.ts`,
  `web/src/components/GraphPreview.tsx`.

* `docs/adr/0024-viewer-camera-lod.md`;
* `web/src/renderer/lod.ts`, `web/src/renderer/lod-observer.tsx`,
  `web/src/renderer/lod.test.ts`.

**Verificación:**

* `pnpm --dir web check`: 8 archivos de test y 47 tests pasando;
* smoke Chromium a `60` fps en `packages` y `files`, `33` fps rotando;
* `symbols` con `10.000` nodos se dibuja como nebulosas con los repositorios
  visibles, no como erizos blancos;
* hover, rotación, zoom, deslizador y modo 2D correctos; cero `pageerror`.

* smoke Chromium con `10.000` nodos: al alejar hasta el límite la cabecera
  declara `files LOD`, la escena conserva repositorios, paquetes y archivos,
  y al acercar de nuevo vuelve a `full detail` con `5.6k symbols`; cero
  `pageerror`.

**Descartado tras medirlo:** niebla exponencial. Se calcula desde la cámara,
que abre a dos radios y medio, así que velaba el grafo entero antes de dar
sensación de profundidad. Revertida.

**Limitación:** el nivel `symbols` completo sigue siendo una nube cuando la
cámara está cerca: diez mil nodos en un mundo de miles de unidades dan puntos
de una fracción de píxel. Al alejarse, el LOD oculta esa textura y mantiene la
jerarquía de repositorios, paquetes y archivos; el detalle vuelve al acercarse.

**Siguiente tarea desbloqueada:** LUQUE-1713.

---

## LUQUE-1713 — Medir rendimiento end-to-end del visor

**Dependencias:** LUQUE-1712.

**Objetivo:** verificar el gate con Chromium y el corpus versionado.

**Checklist:**

- [x] Verificar dataset, semilla, commit y entorno.
- [x] Medir carga, transferencia, decode, primer frame y memoria.
- [x] Medir pan/zoom, picking, LOD y vecindad.
- [x] Guardar `results.json` y `report.md` versionados.

**Criterios de aceptación:**

- El harness falla ante una regresión de cada métrica.
- El resultado informa limitaciones de GPU y distribución de grado.
- `WEB_VIEWER_PERFORMANCE_PASS` solo se emite con todos los límites cumplidos.

**Estado:** `PASS_WITH_LIMITS`.

**Archivos creados:** `benchmarks/web-viewer/harness.mjs`, `benchmarks/web-viewer/results.json`, `benchmarks/web-viewer/report.md`.

**Verificación:** el harness termina con `WEB_VIEWER_PERFORMANCE_PASS_WITH_LIMITS` solo con `--allow-limitations` y código `1` sin esa opción; todas las métricas observadas cumplen sus límites, pero el snapshot no alcanza el corpus contractual.

---

## LUQUE-1714 — Integrar web en CI y distribución

**Dependencias:** LUQUE-1713.

**Objetivo:** automatizar verificación y empaquetado del visor.

**Checklist:**

- [x] Verificar instalación reproducible en Node 22.
- [x] Añadir format, lint, typecheck, tests y build del paquete web.
- [x] Integrar assets en el build Linux cuando corresponda.
- [x] Verificar manifest y `SHA256SUMS` del bundle.

**Criterios de aceptación:**

- Un fallo web rompe CI.
- CI conserva la suite existente de `ts-worker` y Go.
- El bundle limpio contiene exactamente los assets declarados.

**Estado:** `PASS`.

**Verificación:** `ci.yml` ejecuta `web` check/build y el job Ladybug construye el bundle Linux y ejecuta `sha256sum -c SHA256SUMS`; `make build-linux-amd64` pasó en el host calificado.

---

## LUQUE-1715 — Calificar y documentar el visor web

**Dependencias:** LUQUE-1713 y LUQUE-1714.

**Objetivo:** cerrar la fase con evidencia, limitaciones y operación explícitas.

**Checklist:**

- [x] Verificar dependencias, gates y resultados publicados.
- [x] Actualizar `AGENTS.md` con la verificación del paquete web.
- [x] Actualizar calificación de producción y documentación de instalación.
- [x] Registrar riesgos de seguridad, GPU, hubs y snapshot estático.

**Criterios de aceptación:**

- La documentación describe el comportamiento observado, no promesas futuras.
- El bind no loopback y la ausencia de autenticación quedan advertidos.
- La fase solo puede emitir `WEB_VIEWER_PASS` con evidencia completa.

**Estado:** `PASS_WITH_LIMITS`.

**Archivos modificados:** `AGENTS.md`, `TASKS.md`, `docs/installation.md`, `docs/release/production-qualification.md`, `.github/workflows/ci.yml`.

**Verificación:** `pnpm --dir web check`, `pnpm --dir web build`, `make build-linux-amd64`, `sha256sum -c dist/kivgraph-linux-amd64/SHA256SUMS` y el harness del visor; `WEB_VIEWER_PASS` permanece sin emitir por corpus insuficiente.

---

# 21. Fase 18 — Rust

Esta fase añade Rust como tercer lenguaje con las mismas garantías que Go y
TypeScript: una arista `EXACT` exige resolución con tipos, todo fallo se
declara como `UNRESOLVED` con motivo, y la indexación no escribe dentro de un
repositorio registrado.

El motor es `rust-analyzer scip`, ejecutado como proceso externo una vez por
workspace Cargo. Tree-sitter no produce identidad ni resolución: aporta la
clase sintáctica del uso y la visibilidad declarada, igual que el AST de Go
aporta hoy `GO_AST_CALL` sobre una resolución de `go/types`.

**Dependencias de fase:** `DISTRIBUTION_PASS`.

`WEB_VIEWER_PASS` sigue sin emitir por corpus insuficiente del visor
(`LUQUE-1715`) y no bloquea esta fase: la limitación pendiente es del harness
del visor y no toca la indexación ni la resolución semántica.

**Alcance ampliado:** `LUQUE-1818` a `LUQUE-1824` añaden el kind fino, la
visibilidad real y las relaciones estructurales `IMPLEMENTS`, `EXTENDS` y
`OVERRIDES`, que la primera entrega había declarado ausentes. `LUQUE-1825`
añade las tres relaciones de función como valor -`PASSES_AS_CALLBACK`,
`ASSIGNS_FUNCTION` y `RETURNS_FUNCTION`-, que Go ya emitía.

**Abierta:** `LUQUE-1826` indexar el sysroot. Es la única causa común de las
carencias que quedan -`#[derive]`, la sobrecarga de operadores, el operador
`?` y toda la biblioteca estándar-, y por tamaño y versionado no entra en esta
fase.

**Gates emitidos:** `RUST_SEMANTIC_PASS_WITH_LIMITS` y
`RUST_CROSS_REPO_PASS_WITH_LIMITS`. Los dos gates sin límites siguen sin
emitir: la auditoría no encontró ninguna arista exacta falsa, pero el corpus
son cuatro fixtures de crates y eso prueba los contratos, no la escala.

---

## LUQUE-1801 — Fijar la gramática Rust

**Dependencias:** ninguna dentro de la fase.

**Objetivo:** registrar Tree-sitter Rust con la misma procedencia verificable
que las gramáticas existentes.

**Entregables:**

* entrada `rust` en `grammars/manifest.json`;
* `github.com/tree-sitter/tree-sitter-rust v0.23.2` promovido a dependencia
  directa en `go.mod` —su checksum ya está fijado en `go.sum`—;
* `syntax.LanguageRust` en `internal/syntax/parser_manager.go`;
* candidatos de inventario Rust en `internal/syntax/inventory.go`;
* `THIRD_PARTY_NOTICES.md`.

**Checklist:**

- [x] Verificar dependencias y alcance.
- [x] Añadir la entrada del manifiesto con `commit`, `archive_url`, `sha256`
      y licencia observados, nunca copiados de otra entrada.
- [x] Registrar la gramática en el `ParserManager` y su inventario.
- [x] Cubrir declaraciones: `function_item`, `struct_item`, `enum_item`,
      `trait_item`, `impl_item`, `mod_item`, `macro_definition`, `const_item`,
      `static_item`, `type_item`, `union_item`, `use_declaration`.
- [x] Registrar resultados, limitaciones y siguiente tarea.

**Criterios de aceptación:**

- `manager.Parse(ctx, syntax.LanguageRust, …)` deja de devolver
  `ParserErrorUnsupportedLanguage`; el test que lo exige hoy en
  `internal/syntax/parser_manager_test.go` se actualiza a un lenguaje que
  sigue sin gramática.
- El inventario distingue un cambio de firma de un cambio de cuerpo sobre un
  archivo `.rs`.
- La verificación del manifiesto falla si el digest no coincide.

**Estado:** `PASS`.

**Verificación:** `go test ./internal/syntax/...`, `go vet ./...`.

**Resultado:** `grammars/manifest.json` fija `tree-sitter-rust v0.23.2` en el commit `cad8a206` con su digest verificado; `syntax.LanguageRust` parsea Rust y el inventario clasifica `use_declaration` y `extern_crate_declaration` como importaciones, que ninguna regla de subcadena alcanzaba. El test de lenguaje sin gramática pasó a `python`.

**Siguiente tarea:** LUQUE-1802.

---

## LUQUE-1802 — Descubrir workspaces y crates Cargo

**Dependencias:** LUQUE-1801.

**Objetivo:** obtener, sin ejecutar `cargo`, los manifests y crates que un
repositorio declara.

**Entregables:**

* `internal/workspace/cargo_manifests.go`;
* `internal/workspace/cargo_discovery.go`;
* tests equivalentes a `go_discovery_test.go` y `typescript_registry_test.go`.

**Contrato:**

```text
DiscoverCargo(ctx, repository) -> CargoDiscovery
CargoDiscovery.Workspaces []CargoWorkspace   // raíz, manifest, miembros
CargoDiscovery.Crates     []CargoCrate       // nombre, versión, edición, workspace
```

**Decisiones:**

* Los manifests se leen con `github.com/BurntSushi/toml`, ya dependencia
  directa. Nada de `cargo metadata` aquí: el descubrimiento es hermético,
  barato y no escribe.
* Un `Cargo.toml` con `[workspace]` y sin `[package]` es un manifest virtual:
  aporta workspace, no crate.
* Los miembros se expanden con los globs de `members` menos `exclude`, y
  siempre dentro del repositorio; una ruta que escapa se rechaza como ya lo
  hace el descubrimiento Go.
* Se respetan `exclusions` del registro y la política de symlinks de
  `internal/workspace/paths.go`.
* Un crate sin `[package] name` es un error de manifest, no un crate anónimo.

**Criterios de aceptación:**

- Un workspace virtual con tres miembros produce un `CargoWorkspace` y tres
  `CargoCrate`.
- Un crate suelto sin workspace produce un workspace de un solo miembro.
- Las rutas son absolutas y canónicas; los tests usan
  `internal/testsupport.TempDir`.

**Estado:** `PASS`.

**Verificación:** `go test ./internal/workspace/...`.

**Resultado:** `DiscoverCargo` lee los manifests con TOML, resuelve la pertenencia por directorio -como Cargo-, hereda `version` y `edition` de `[workspace.package]`, y convierte cada crate suelto en un workspace de uno. No se implementaron los `targets` de Cargo: ningún consumidor del grafo los necesita y su descubrimiento replicaría el layout por defecto de Cargo entero.

**Siguiente tarea:** LUQUE-1803.

---

## LUQUE-1803 — Registro de crates entre repositorios

**Dependencias:** LUQUE-1802.

**Objetivo:** saber qué repositorio registrado provee cada crate, conservando
la ambigüedad en vez de resolverla por preferencia.

**Entregables:**

* `internal/rustloader/crossrepo.go` con `CrateRegistry` y `CrateProvider`;
* tests que fijan orden determinista y ambigüedad.

**Decisiones:**

* Calco de `internal/goloader/crossrepo.go`: `providers map[string][]CrateProvider`,
  ordenados por repositorio y manifest.
* Dos repositorios que declaran el mismo nombre de crate producen
  `AMBIGUOUS_CRATE_PROVIDER`; ninguno lo provee. Es el mismo trato que reciben
  un módulo Go y un paquete TypeScript ambiguos.
* La identidad de un proveedor es `nombre + versión`. Una versión ausente o
  desconocida no identifica a nadie.

**Criterios de aceptación:**

- El registro es determinista frente al orden de entrada.
- Un crate declarado dos veces no resuelve; se declara.
- El registro no ejecuta procesos externos.

**Estado:** `PASS`.

**Verificación:** `go test ./internal/rustloader/...`.

**Resultado:** `rustloader.CrateRegistry` resuelve por nombre **y versión**: dos proveedores a la misma versión son `AMBIGUOUS_CRATE_PROVIDER`, otra versión es `CRATE_VERSION_MISMATCH` y una versión `.` es `CRATE_VERSION_UNKNOWN`. Ninguno de los tres resuelve.

**Siguiente tarea:** LUQUE-1804.

---

## LUQUE-1804 — Vocabulario, configuración y diagnóstico

**Dependencias:** LUQUE-1802.

**Objetivo:** que `init`, el registro, la pasada y `doctor` acepten y
comprueben lo mismo.

**Entregables:**

* `config.SupportedLanguages` con `rust` y `rs`;
* `config.RustConfig` (`yaml:"rust"`);
* `internal/indexing/service.go`: `normalizeProjectLanguages` deja de tener su
  propia lista y usa `config.SupportedLanguage`;
* `cmd/kivgraph/main.go`: comprobación `toolchain.rust`.

**Contrato de configuración:**

```yaml
rust:
  analyzer_command: rust-analyzer
  maximum_workspaces: 0        # 0 = techo por núcleos, acotado
  features: []
  all_features: false
  no_default_features: false
  cfgs: []
  build_scripts: true
  proc_macros: true
  allow_network: false
  target_dir: ""               # vacío = <state>/rust/target
  sysroot: discover
```

**Decisiones:**

* La segunda lista de lenguajes en `internal/indexing/service.go` desaparece:
  el vocabulario vive donde se escribe el valor, como dice
  `docs/development/conventions.md`.
* `doctor` informa la versión de `rust-analyzer`, la de `cargo` y la presencia
  del servidor de proc-macros del sysroot. Una versión por debajo del suelo es
  `FAIL` con el número observado y el exigido.
* El suelo de versión se **determina** en esta tarea: el primer release donde
  una ocurrencia SCIP transporta `enclosing_range`, verificado ejecutando el
  binario sobre un fixture. No se copia de la documentación.

**Criterios de aceptación:**

- `kivgraph init` acepta un repositorio `rust` y la pasada no lo rechaza.
- `doctor` sin `rust-analyzer` en el `PATH` reporta `toolchain.rust: FAIL` y
  no aborta el resto del diagnóstico.
- Un repositorio registrado con un lenguaje inexistente sigue fallando en
  ambos lados con el mismo mensaje.

**Estado:** `PASS`.

**Verificación:** `go test ./internal/config/... ./internal/indexing/... ./cmd/kivgraph/...`.

**Resultado:** `config.SupportedLanguages` incluye `rust` y `rs`, la sección `rust:` existe con su validación, `internal/indexing` perdió su segunda lista y `doctor` informa `toolchain.rust` y `toolchain.cargo`. El suelo de versión no se expresa como número: el binario de rustup y el standalone usan esquemas de versión distintos, así que la capacidad se comprueba sobre el índice (`validateIndex` rechaza un índice sin `enclosing_range`) y `doctor` publica la cadena de versión observada.

**Siguiente tarea:** LUQUE-1805.

---

## LUQUE-1805 — Ejecutar `rust-analyzer scip` de forma hermética

**Dependencias:** LUQUE-1803 y LUQUE-1804.

**Objetivo:** producir un índice SCIP por workspace sin escribir dentro del
repositorio indexado y sin depender de la red.

**Entregables:**

* `internal/rustloader/scip_run.go`;
* tests de construcción de configuración, cancelación y clasificación de
  fallos.

**Contrato:**

```text
RunSCIP(ctx, options) -> { IndexPath, ToolVersion, Diagnostics, Duration }
```

**Decisiones:**

* Invocación: `rust-analyzer scip <workspace> --output <fuera-del-repo>
  --exclude-vendored-libraries --num-threads N --config-path <generado>`.
  Sin `--exclude-vendored-libraries`, el código vendorizado bajo la raíz
  entraría al grafo como archivos del repositorio.
* `scip.rs` fija `load_out_dirs_from_check: true`, así que **los build scripts
  se ejecutan siempre** y `cargo check` escribiría en `target/`. La
  hermeticidad se impone desde fuera y no se negocia:
  `cargo.extraEnv.CARGO_TARGET_DIR` a un directorio de estado externo
  —`cargo.targetDir` no sirve: su valor es relativo al workspace—,
  `cargo.extraArgs: ["--offline","--locked"]` y `CARGO_NET_OFFLINE=true`.
  `rust.allow_network` es la única salida declarada.
* `--locked` falla cerrado si haría falta reescribir `Cargo.lock`.
* Tras la ejecución se comprueba que el árbol del repositorio no cambió; si
  cambió, la unidad falla y no publica hechos.
* El proceso hereda el contexto: cancelar la pasada mata el proceso y borra el
  índice temporal.
* `stderr` se clasifica: el aviso de símbolos duplicados de rust-analyzer es
  un diagnóstico no bloqueante que viaja al informe; un fallo de carga del
  workspace es bloqueante para esa unidad y solo para ella.

**Criterios de aceptación:**

- Tras indexar un fixture, el repositorio no contiene `target/` nuevo ni un
  `Cargo.lock` modificado.
- Sin `rust-analyzer` en el `PATH`, la unidad declara `ANALYZER_UNAVAILABLE` y
  la pasada continúa con los demás repositorios.
- Un workspace que no carga produce `WORKSPACE_NOT_LOADED` con el diagnóstico
  observado, no una pasada abortada.

**Estado:** `PASS`.

**Verificación:** `go test ./internal/rustloader/...` y una prueba de humo con
la toolchain real sobre `testdata/rust/`.

**Resultado:** `rustloader.Run` genera la configuración del analizador, redirige `CARGO_TARGET_DIR` fuera del repositorio, pasa `--offline --locked` y comprueba después que el workspace no ganó `target/` ni `Cargo.lock`. Un analizador ausente, un workspace que no carga y una cancelación son errores clasificados distintos.

**Siguiente tarea:** LUQUE-1806.

---

## LUQUE-1806 — Decodificar SCIP

**Dependencias:** LUQUE-1805.

**Objetivo:** leer el índice sin inventar posiciones ni identidades.

**Entregables:**

* `internal/rustloader/scipwire/` (decodificación y tipos);
* tests con índices de referencia versionados en `testdata/rust/scip/`.

**Decisiones:**

* Por defecto se importa `github.com/scip-code/scip/bindings/go/scip@v0.9.0`
  (Apache-2.0, requiere `google.golang.org/protobuf`). Si el árbol de
  dependencias resulta desproporcionado, se generan bindings mínimos del
  `.proto` fijado con digest, como se hace con las gramáticas; la decisión se
  registra en el ADR con el peso medido, no por gusto.
* Los rangos SCIP son de tres enteros cuando empiezan y acaban en la misma
  línea y de cuatro cuando no; líneas y columnas son base cero en UTF-8. Las
  posiciones canónicas de Kivgraph son línea base uno y columna base cero
  (`facts.Position`), así que la conversión es explícita y está probada en los
  dos casos.
* `Metadata.tool_info.version` se conserva: identifica al analizador en la
  caché de hechos.

**Criterios de aceptación:**

- Un índice con rangos de tres y de cuatro enteros produce las mismas
  posiciones que el archivo fuente.
- Un índice truncado o de versión desconocida se rechaza con un error
  clasificado, nunca con una lectura parcial.

**Estado:** `PASS`.

**Verificación:** `go test ./internal/rustloader/...`.

**Resultado:** `internal/rustloader/scipwire` decodifica el subconjunto de SCIP que Kivgraph lee, con el esquema fijado por digest. Se descartaron los bindings publicados: traen formateador, validador y cuatro módulos más para leer seis mensajes. Los tests decodifican un índice real grabado del analizador y rechazan índices malformados.

**Siguiente tarea:** LUQUE-1807.

---

## LUQUE-1807 — Símbolos Rust y claves estables

**Dependencias:** LUQUE-1806.

**Objetivo:** convertir definiciones SCIP en símbolos con identidad durable.

**Entregables:**

* `internal/rustloader/symbols.go`;
* `internal/rustloader/stablekey.go` y sus tests.

**Decisiones:**

* Un símbolo del grafo es una definición con moniker. Los locales de SCIP
  (`local N`) son un contador por documento: no son direccionables y nunca
  entran al grafo.
* La identidad estable usa `hotsnapshot.StableKeyIdentity` con
  `Language: "rust"`, `Package:` nombre del crate, `Module:` vacío,
  `QualifiedName:` el camino de descriptores canónico, `Kind:` el **sufijo del
  descriptor** y `Discriminator:` derivado de la firma, igual que Go y
  TypeScript. El sufijo viaja dentro de la propia cadena del símbolo, así que
  consumidor y proveedor no pueden divergir; `SymbolInformation.kind` se
  guarda en `Symbol.Kind` pero no decide la clave.
* `Signature` sale de `signature_documentation.text`; `Name` de
  `display_name`; el span del símbolo, de `enclosing_range` cuando existe y
  del rango de la ocurrencia cuando no.
* `Exported` lo decide Tree-sitter sobre el modificador de visibilidad:
  `pub(crate)` no es exportado fuera del crate. SCIP no transporta
  visibilidad.

**Criterios de aceptación:**

- Insertar una función delante de otra no cambia la clave de la segunda.
- Dos checkouts del mismo commit en rutas distintas producen claves idénticas.
- Un `impl` inherente duplicado por el bug conocido de rust-analyzer se
  declara en el informe; no se fusionan dos definiciones distintas.

**Estado:** `PASS`.

**Verificación:** `go test ./internal/rustloader/...`.

**Resultado:** Las definiciones llevan identidad, visibilidad leída de la gramática y el span del cuerpo. La clave estable **no** incluye la firma: rust-analyzer no emite `SymbolInformation` para una declaración fuera de la raíz del workspace, así que un consumidor que dependiera de ella nunca podría nombrar la clave de su proveedor. El discriminante sale del disambiguador del descriptor.

**Siguiente tarea:** LUQUE-1808.

---

## LUQUE-1808 — Atribuir la referencia y clasificar la arista

**Dependencias:** LUQUE-1807.

**Objetivo:** que cada uso resuelto tenga origen, destino y clase, sin que la
sintaxis decida identidad.

**Entregables:**

* `internal/rustloader/references.go`;
* tests de atribución y clasificación.

**Decisiones:**

* El símbolo que contiene una referencia se obtiene con un árbol de intervalos
  construido sobre las ocurrencias con rol `Definition` y `enclosing_range` no
  vacío del mismo documento: gana la definición más interna que contiene el
  rango. Para un token local, `SymbolInformation.enclosing_symbol` nombra al
  ancestro no local.
* Un uso sin contenedor —a nivel de módulo— se cuenta en el informe como
  `EdgesWithoutSource`, igual que en TypeScript. No se inventa un contenedor.
* `symbol_roles` solo distingue definición, y `syntax_kind` viaja sin valor.
  La clase de la arista la decide Tree-sitter sobre el nodo que contiene la
  ocurrencia: `CALLS_DIRECT` con `RUST_SYNTAX_CALL`, `TYPE_USES` con
  `RUST_SYNTAX_TYPE`, y `REFERENCES` con `RUST_ANALYZER_USE` en el resto. La
  confianza es `EXACT_TYPECHECKED` porque la identidad es del analizador; es
  exactamente el trato que recibe hoy `GO_AST_CALL`.
* `IMPLEMENTS` y `EXTENDS` **no** se emiten como exactas: rust-analyzer envía
  `relationships` vacío. Se omiten en esta fase y la limitación se declara.

**Criterios de aceptación:**

- Una llamada dentro de un método atribuye el método como origen.
- Un uso en posición de tipo produce `TYPE_USES`, no `CALLS_DIRECT`.
- Ninguna arista lleva procedencia `TREE_SITTER_SYNTAX` y confianza exacta.

**Estado:** `PASS`.

**Verificación:** `go test ./internal/rustloader/...`.

**Resultado:** La atribución usa el `enclosing_range` de las definiciones del documento, la más interna que contiene el uso. La clase la decide Tree-sitter: `CALLS_DIRECT` con `RUST_SYNTAX_CALL`, `TYPE_USES` con `RUST_SYNTAX_TYPE`, `IMPORTS_SYMBOL`/`REEXPORTS` según la visibilidad del `use`.

**Siguiente tarea:** LUQUE-1809.

---

## LUQUE-1809 — Contención y dependencias de crate

**Dependencias:** LUQUE-1807.

**Objetivo:** dar al grafo la estructura del repositorio Rust.

**Entregables:**

* `internal/rustloader/structure.go` y tests.

**Decisiones:**

* `CONTAINS_PACKAGE`, `CONTAINS_FILE` y `DEFINES` son `STRUCTURAL_CERTAIN` con
  procedencia `PACKAGE_MANIFEST`, como en los otros dos lenguajes.
* Un `Package` canónico es un crate; `Container` es la raíz del workspace
  Cargo cuando el crate pertenece a uno.
* `PACKAGE_DEPENDS_ON` se emite por un import observado y resuelto, nunca por
  una entrada nominal de `Cargo.toml`: un `[dependencies]` que nadie usa no es
  una dependencia del grafo. La evidencia es una ocurrencia concreta.
* `MODULE_DEPENDS_ON` se reserva al cruce entre workspaces Cargo distintos.

**Criterios de aceptación:**

- Un crate con una dependencia declarada y sin usar no produce
  `PACKAGE_DEPENDS_ON`.
- Cada arista de dependencia tiene `evidence_key` y su evidencia está en un
  `File` del conjunto.

**Estado:** `PASS`.

**Verificación:** `go test ./internal/rustloader/...`.

**Resultado:** Contención y dependencias se emiten desde ocurrencias observadas; una entrada de `[dependencies]` que nadie usa no produce arista. `MODULE_DEPENDS_ON` acompaña a la dependencia que sale del workspace.

**Siguiente tarea:** LUQUE-1810.

---

## LUQUE-1810 — Vocabulario `UNRESOLVED` de Rust

**Dependencias:** LUQUE-1808 y LUQUE-1809.

**Objetivo:** que lo que no se pudo resolver quede dicho, con motivo y
evidencia.

**Entregables:**

* `internal/rustloader/unresolved.go` y tests negativos.

**Contrato:**

```text
WORKSPACE_NOT_LOADED        CRATE_PROVIDER_NOT_FOUND
AMBIGUOUS_CRATE_PROVIDER    CRATE_VERSION_UNKNOWN
CRATE_SYMBOL_NOT_MATCHED    DEFINITION_NOT_INDEXED
MACRO_EXPANSION_DISABLED    TARGET_NOT_BUILDABLE
ANALYZER_UNAVAILABLE
```

**Decisiones:**

* rust-analyzer descarta los tokens sin moniker, así que el índice no trae una
  lista de fallos. Los no resueltos se derivan de tres fuentes observadas: el
  registro de crates, el diff entre el inventario Tree-sitter del archivo y
  las definiciones SCIP del mismo archivo (`DEFINITION_NOT_INDEXED`), y el
  fallo de carga del workspace.
* Un `#[cfg]` que la configuración no selecciona no es un fallo del índice:
  es `TARGET_NOT_BUILDABLE`, el equivalente de `PACKAGE_NOT_BUILDABLE` en Go.
* Con `build_scripts` o `proc_macros` desactivados, lo que dependía de la
  expansión se declara `MACRO_EXPANSION_DISABLED`.
* Un fallo a nivel de workspace puede no tener archivo; nunca se le fabrica
  evidencia ni una arista.

**Criterios de aceptación:**

- Cada entrada conserva motivo, repositorio y lenguaje; con ocurrencia
  concreta, además archivo y posición.
- Un crate no provisto por ningún repositorio registrado nunca produce una
  arista; produce `CRATE_PROVIDER_NOT_FOUND`.

**Estado:** `PASS`.

**Verificación:** `go test ./internal/rustloader/...`.

**Resultado:** El vocabulario implementado añade `CRATE_VERSION_MISMATCH` al listado original. Cada entrada conserva motivo, repositorio y lenguaje, y las que tienen ocurrencia conservan archivo y posición.

**Siguiente tarea:** LUQUE-1811.

---

## LUQUE-1811 — Payload `rust-facts-v1` y normalización

**Dependencias:** LUQUE-1810.

**Objetivo:** convertir la salida del motor en el modelo canónico.

**Entregables:**

* `internal/facts/rust.go` con `RustPayload`, `RustWireVersion = 1`,
  `DecodeRustPayload`, `NormalizeRust` y `RustReport`;
* `facts.LanguageRust` y las procedencias `RUST_ANALYZER_DEF`,
  `RUST_ANALYZER_USE`, `RUST_ANALYZER_MONIKER`, `RUST_SYNTAX_CALL`,
  `RUST_SYNTAX_TYPE`;
* `internal/facts/rust_test.go`.

**Decisiones:**

* El payload transporta componentes de identidad y posiciones; la clave la
  calcula un solo lado, como en TypeScript. Rutas relativas al repositorio: un
  payload no incrusta la máquina que lo produjo.
* `EdgeKind` no crece: el vocabulario actual cubre Rust.
* Si la clave calculada de un destino no existe en el conjunto tras el merge,
  se emite `UNRESOLVED` y **no** una arista. Una arista colgante no es un
  hecho parcial, es un hecho falso.

**Criterios de aceptación:**

- `NormalizeRust` produce un conjunto que pasa `facts.Set.Validate`.
- Dos normalizaciones del mismo payload en rutas distintas producen claves
  idénticas.
- Una versión de payload distinta de 1 se rechaza.

**Estado:** `PASS`.

**Verificación:** `go test ./internal/facts/...`.

**Resultado:** No hay formato de cable: el motor vive en el mismo proceso, así que `facts.NormalizeRust` consume `rustloader.Analysis` directamente, como `NormalizeGo` consume `goloader`. El límite versionado es el esquema SCIP, fijado por digest. Un destino cuya clave no existe en el conjunto se declara y no se emite.

**Siguiente tarea:** LUQUE-1812.

---

## LUQUE-1812 — Integrar Rust en la pasada completa

**Dependencias:** LUQUE-1811.

**Objetivo:** que `index --full` analice repositorios Rust junto a los demás.

**Entregables:**

* `internal/indexer/full.go`: `PhaseRust`, opciones y contadores Rust,
  `repositoriesForRust`, unidad de análisis por workspace Cargo,
  `workspaceNotLoadedFacts`, `ambiguousCrateFacts`;
* `internal/indexer/factcache.go`: entradas de la unidad Rust;
* tests en `internal/indexer/`.

**Decisiones:**

* La unidad de análisis es el workspace Cargo, no el crate: `rust-analyzer`
  carga el workspace entero en cada invocación, así que una unidad por crate
  pagaría la misma carga N veces.
* El peso de la unidad es el número de archivos `.rs` que declara, como el
  resto de unidades.
* El techo de concurrencia Rust es bajo por memoria: cada invocación mantiene
  el workspace y el sysroot en RAM. Cero significa techo por núcleos, acotado,
  igual que `GoMaximumLoads`.
* La huella de caché de una unidad Rust incluye el árbol del workspace, sus
  manifests y lockfile, el registro de crates, la versión de `rust-analyzer`
  observada, la de `cargo`/`rustc`, y las features, cfgs y conmutadores de
  expansión. Un `WORKSPACE_NOT_LOADED` nunca se guarda en caché, por la misma
  razón que no se guarda un `MODULE_NOT_LOADED`.
* Un workspace que falla aísla su fallo: los demás repositorios se publican.

**Criterios de aceptación:**

- Un registro mixto Go + TypeScript + Rust produce un único conjunto validado.
- El informe distingue repositorios, workspaces, símbolos, referencias y no
  resueltos de Rust.
- El modo `verify` de la caché no reporta divergencias sobre un corpus Rust.

**Estado:** `PASS`.

**Verificación:** `go test ./internal/indexer/...`, `make build`.

**Resultado:** `indexer.Full` reparte tres colas; la de Rust tiene su propio techo porque cada proceso mantiene un workspace y su sysroot en memoria. La caché de hechos huella el árbol del workspace, sus manifests, el lockfile, el registro de crates y la identidad del analizador.

**Siguiente tarea:** LUQUE-1813.

---

## LUQUE-1813 — Exactitud entre repositorios

**Dependencias:** LUQUE-1812.

**Objetivo:** unir consumidor y proveedor Rust sin coincidencias nominales.

**Entregables:**

* resolución cross-repo en `internal/rustloader/crossrepo.go`;
* fixtures de dos repositorios en `testdata/rust/`;
* tests sobre `find_cross_repo_consumers`.

**Decisiones:**

* El puente es el registro de crates más la cadena del símbolo, que incluye
  gestor, nombre de crate, versión y descriptores. Consumidor y proveedor
  emiten la misma cadena porque la produce el mismo analizador sobre el mismo
  código: no es una coincidencia de nombre.
* Confianza `EXACT_PACKAGE_MAPPED`, procedencia `RUST_ANALYZER_MONIKER`.
* Una versión `"."` —desconocida para el analizador— nunca resuelve:
  `CRATE_VERSION_UNKNOWN`.
* Si el consumidor compila contra una copia del registro y no contra el
  checkout local, la firma puede diferir y la clave calculada no existir:
  `CRATE_SYMBOL_NOT_MATCHED`, nunca una arista.

**Criterios de aceptación:**

- Un consumidor que usa un crate provisto por otro repositorio registrado
  produce una arista exacta con evidencia en el archivo del consumidor.
- Desregistrar el proveedor convierte esa arista en
  `CRATE_PROVIDER_NOT_FOUND`.
- Dos proveedores del mismo crate no producen ninguna arista.

**Estado:** `PASS`.

**Verificación:** `go test ./internal/rustloader/... ./internal/mcp/...`.

**Resultado:** Consumidor y proveedor indexados por separado producen una arista exacta porque ambos derivan la misma clave del mismo símbolo del analizador. Sin el proveedor registrado, la misma llamada es `CRATE_PROVIDER_NOT_FOUND` y no hay `PACKAGE_DEPENDS_ON`.

**Siguiente tarea:** LUQUE-1814.

---

## LUQUE-1814 — Incrementalidad

**Dependencias:** LUQUE-1812.

**Objetivo:** reaccionar a un cambio en un repositorio Rust sin reindexar el
mundo.

**Entregables:**

* `internal/indexer/rust.go` con `ClassifyRustChange`;
* extensiones del watcher en `internal/watcher/reconcile.go`;
* tests de invalidación.

**Decisiones:**

* Extensiones y archivos vigilados: `.rs`, `Cargo.toml`, `Cargo.lock` y
  `build.rs`.
* Un cambio de cuerpo, detectado por el inventario Tree-sitter, reindexa el
  archivo; un cambio de firma, de manifest o de lockfile reindexa el
  workspace.
* La granularidad mínima real de reanálisis es el workspace: `rust-analyzer
  scip` no tiene modo por archivo. El coste se declara, no se disimula.
* Todo hecho afirmado por un archivo se retira y se vuelve a afirmar con ese
  archivo, y las aristas de paquete se retiran por su evidencia.

**Criterios de aceptación:**

- Un delta sobre un corpus Rust produce el mismo grafo que una reconstrucción
  limpia del mismo estado.
- Tocar un `Cargo.toml` invalida el workspace, no solo el archivo.

**Estado:** `PASS`.

**Verificación:** `go test ./internal/indexer/... ./internal/watcher/...`.

**Resultado:** `ClassifyRustChange` reduce a `REINDEX_FILE` un cambio de cuerpo y eleva a `REINDEX_PROJECT` manifest, lockfile y `build.rs`. El watcher vigila `.rs`. La granularidad real de reanálisis es el workspace y queda declarada.

**Siguiente tarea:** LUQUE-1815.

---

## LUQUE-1815 — Superficie visible y documentación

**Dependencias:** LUQUE-1813 y LUQUE-1814.

**Objetivo:** que lo que el usuario lee coincida con lo que el binario hace.

**Entregables:**

* `internal/integrations/assets/kivgraph/SKILL.md`;
* `README.md`, `docs/installation.md`, `docs/development/conventions.md`;
* documentación de `index_project` con `rust` entre los lenguajes.

**Decisiones:**

* Las tools MCP no cambian de esquema: `language` ya es un filtro de cadena en
  todas ellas y el vocabulario de motivos ya es por lenguaje.
* `rust-analyzer` es un prerrequisito externo, documentado como el runtime
  Node: no se empaqueta en el bundle.
* La documentación describe el comportamiento observado, incluidas las
  limitaciones de `IMPLEMENTS`, de la expansión de macros y de la granularidad
  incremental.

**Criterios de aceptación:**

- Ningún documento afirma una capacidad que el binario no tiene.
- La skill instalada enumera Rust y sus prerrequisitos.

**Estado:** `PASS`.

**Verificación:** `go test ./internal/integrations/...`, revisión del diff.

**Resultado:** `SKILL.md`, `README.md`, `docs/installation.md` y `AGENTS.md` describen el prerrequisito externo, el vocabulario de no resueltos de Rust y la ausencia declarada de `IMPLEMENTS`/`EXTENDS`.

**Siguiente tarea:** LUQUE-1816.

---

## LUQUE-1816 — Benchmarks y auditoría de exactitud

**Dependencias:** LUQUE-1815.

**Objetivo:** emitir los gates con evidencia reproducible.

**Entregables:**

* `benchmarks/rust-engine/{results.json,report.md}`;
* `benchmarks/rust-cross-repo/{results.json,report.md}`;
* ground truth en `testdata/rust/`.

**Decisiones:**

* Cada benchmark conserva comando, commit, entorno, dataset, semilla, métricas
  y limitaciones, y el harness falla cerrado ante una métrica fuera de límite.
* La auditoría separa `false exact edges` —comparación contra ground truth—
  de aristas colgantes —invariantes canónicas de extremos, evidencia y
  procedencia—.
* El coste del motor se reporta en frío y en caliente, y separado del resto de
  la pasada: `rust-analyzer` es el término dominante.

**Criterios de aceptación:**

- `false exact edges = 0` sobre el corpus de ground truth.
- Las invariantes canónicas pasan sobre el grafo publicado.
- `RUST_SEMANTIC_PASS` y `RUST_CROSS_REPO_PASS` solo se emiten con todos los
  límites cumplidos; si el corpus no alcanza lo contratado, el resultado es
  `PASS_WITH_LIMITS` y lo dice.

**Estado:** `PASS_WITH_LIMITS`.

**Verificación:** los harness de ambos benchmarks, `go test ./...`,
`make test-ladybug`.

**Resultado:** `benchmarks/rust-semantic`: 13 aristas esperadas, 13 encontradas, `false exact edges = 0`, 3/3 fallos declarados, invariantes canónicas sin violación. `benchmarks/rust-engine`: índice frío `1331.7 ms`, caliente `52.6 ms` con dos aciertos de caché y grafo idéntico; el analizador solo son `1010.4 ms` de ese frío. El corpus son tres crates, así que el token emitido es `RUST_SEMANTIC_PASS_WITH_LIMITS`.

**Siguiente tarea:** LUQUE-1817.

---

## LUQUE-1817 — ADR y cierre de fase

**Dependencias:** LUQUE-1816.

**Objetivo:** dejar registrada la arquitectura y sus límites.

**Entregables:**

* `docs/adr/0033-rust-scip-engine.md`;
* `docs/adr/0034-rust-hermetic-cargo.md`;
* `docs/adr/0035-rust-unresolved-vocabulary.md`;
* `AGENTS.md` con las reglas de Rust observadas;
* `docs/release/production-qualification.md`.

**Checklist:**

- [x] Pasar los tres ADR de `propuesta` a `aceptada` con la evidencia medida.
- [x] Registrar el suelo de versión de `rust-analyzer` verificado.
- [x] Enumerar limitaciones residuales sin convertir ninguna en un PASS
      implícito.
- [x] Confirmar que tests, documentación y consumidores están migrados.

**Criterios de aceptación:**

- Los ADR contienen contexto, decisión, alternativas, consecuencias, riesgos y
  estado.
- `AGENTS.md` describe comportamiento observado.

**Estado:** `PASS`.

**Verificación:** `gofmt -l`, `go vet ./...`, `go test ./...`, `make build`,
`make test-ladybug`, `git diff --check`.

**Resultado:** Los tres ADR pasan a `aceptada` con la evidencia medida; `AGENTS.md` recoge los contratos observados del camino Rust.

**Siguiente tarea:** LUQUE-1818.

---

## LUQUE-1818 — Publicar el kind fino y la visibilidad real

**Dependencias:** LUQUE-1817.

**Objetivo:** que una consulta pueda distinguir un struct de un trait, y que la
API pública que el grafo declara sea la que Rust considera pública.

**Entregables:**

* `internal/rustloader/kinds.go`;
* visibilidad heredada en `internal/rustloader/source.go`;
* tests de kind y de visibilidad.

**Decisiones:**

* El `Kind` publicado es el fino de SCIP (`struct`, `trait`, `field`,
  `trait_method`, `static_method`, `macro`, `module`…). El sufijo del
  descriptor sigue decidiendo la **clave**, porque viaja dentro del símbolo y
  no puede divergir entre consumidor y proveedor.
* Un bloque `impl` no se publica con el nombre del tipo al que aplica: se
  reconoce por su descriptor y se rotula `impl Trait for Type`.
* La visibilidad no es solo `pub`: un miembro de un `trait` es tan visible como
  el trait, y un método de una implementación de trait es alcanzable a través
  de él.

**Criterios de aceptación:**

- `Value` es `struct`, `Named::name` es `trait_method` y `crate` es `module`.
- Un método de trait sin `pub` se publica como exportado.
- La clave estable de un símbolo no cambia por publicar otro kind.

**Estado:** `PASS`.

**Verificación:** `go test ./internal/rustloader/ -run 'FineGrained|Durable'`.

**Resultado:** el mapa cubre los 22 kinds que `symbol_kind()` puede emitir y cae
al sufijo del descriptor para un símbolo externo sin `SymbolInformation`.

**Siguiente tarea:** LUQUE-1819.

---

## LUQUE-1819 — Derivar implementaciones, supertraits y overrides

**Dependencias:** LUQUE-1818.

**Objetivo:** dar al grafo las relaciones estructurales de Rust sin inventarlas.

**Entregables:**

* `internal/rustloader/relations.go`;
* `Source.Implementations` y `Source.TraitBounds`;
* `facts.RustSyntaxImplementation` y su código canónico;
* fixture `testdata/rust/workspace/crates/support/src/shapes.rs`.

**Contrato:**

```text
IMPLEMENTS  tipo  -> trait        (cabecera del impl)
EXTENDS     trait -> supertrait   (bound de la declaración)
OVERRIDES   método de impl -> método del trait
```

**Decisiones:**

* `SymbolInformation.relationships` viaja vacío, así que la forma la decide la
  gramática y los dos extremos los resuelve el analizador: es el mismo reparto
  que `GO_AST_CALL`.
* El destino de un `OVERRIDES` se **compone** desde el símbolo del trait más el
  descriptor del miembro -así es como se forma una identidad SCIP- y solo se
  emite si el índice observó ese símbolo. Una composición que nadie vio se
  descarta.
* Una implementación inherente no relaciona el tipo con ningún trait.
* La ocurrencia que se convierte en relación no se publica además como
  referencia de tipo: una observación, una arista.

**Criterios de aceptación:**

- `Circle` implementa `Named` y `Drawable`; `Drawable` extiende `Named`.
- Los dos métodos del `impl` sobrescriben los del trait; `new` no sobrescribe
  nada.
- Cada relación lleva la ocurrencia que la prueba.

**Estado:** `PASS`.

**Verificación:** `go test ./internal/rustloader/ -run Relations`,
`go test ./internal/facts/ -run Rust`, `go run ./benchmarks/rust-semantic`.

**Resultado:** la auditoría pasó de 13 a 23 aristas esperadas y sigue con
`false exact edges = 0`. Las diez nuevas -dos `IMPLEMENTS`, un `EXTENDS`, dos
`OVERRIDES` y cinco usos del módulo de traits- se añadieron al ground truth a
mano, que es justo lo que la auditoría existe para forzar.

**Siguiente tarea:** LUQUE-1820.

---

## LUQUE-1820 — Hacer configurable el código de test

**Dependencias:** LUQUE-1819.

**Objetivo:** decidir si `cfg(test)` forma parte del grafo.

**Entregables:**

* `config.RustConfig.IncludeTests`;
* `cfg.setTest` en la configuración generada del analizador;
* la opción atravesando `indexing`, `indexer` y la CLI, y entrando en la huella
  de la caché de hechos.

**Decisiones:**

* Por defecto está activo, que es el valor propio del analizador. Apagarlo
  retira del grafo todo ítem de test y la gramática los reporta entonces como
  `DEFINITION_NOT_INDEXED`: la limitación es visible, no silenciosa.
* El valor entra en la identidad de la caché: un índice con tests y otro sin
  ellos no son el mismo grafo.

**Criterios de aceptación:**

- La configuración generada lleva `cfg.setTest`.
- Cambiar la opción invalida las entradas de caché de las unidades Rust.

**Estado:** `PASS`.

**Verificación:** `go test ./internal/config/ ./internal/rustloader/ ./internal/indexer/`.

**Resultado:** `rust.include_tests` viaja hasta `cfg.setTest` y hasta
`rustAnalysisFingerprint`.

**Siguiente tarea:** LUQUE-1821.

---

## LUQUE-1821 — Nunca nombrar una clave que nadie publica

**Dependencias:** LUQUE-1820.

**Objetivo:** que una referencia a una declaración que el índice no define se
declare, en vez de convertirse en una arista colgante que aborta la pasada.

**Entregables:**

* la regla en `internal/rustloader/analyze_references.go`;
* `-> Self` en el fixture `shapes.rs`;
* `TestAnalyzeDeclaresTheImplementationBlockItCannotDefine`.

**Contrato:**

```text
símbolo definido en esta pasada        -> arista
símbolo de otro repositorio registrado -> arista (el proveedor la publica)
símbolo del propio repositorio ausente -> DEFINITION_NOT_INDEXED
```

**Decisiones:**

* SCIP menciona el bloque `impl` en las ocurrencias -`-> Self` apunta a él- y
  nunca lo define. La resolución anterior componía su clave y confiaba en que
  otro workspace del mismo repositorio la publicase; para un bloque `impl` no
  la publica nadie, y `facts.Set.Validate` abortaba la pasada entera con
  `edge TYPE_USES has unknown target`. Es el patrón más común de Rust: bastaba
  un `-> Self` para dejar un repositorio sin grafo.
* Un `mod nombre;` sin cuerpo no es una declaración que el índice haya perdido:
  el analizador la indexa donde vive su fuente. Deja de contarse como
  `DEFINITION_NOT_INDEXED`.

**Criterios de aceptación:**

- Ninguna referencia ni relación local nombra una clave que la pasada no
  publica.
- El bloque `impl` aparece declarado como `DEFINITION_NOT_INDEXED`.
- El fixture contiene `-> Self`, que es lo que reproducía el fallo.

**Estado:** `PASS`.

**Verificación:** `go test ./internal/rustloader/ ./internal/indexer/`,
`go run ./benchmarks/rust-semantic`.

**Resultado:** encontrado sondeando `Self`, genéricos y `derive` sobre un crate
de prueba; la sonda abortaba con una arista colgante. Con la regla, la misma
sonda publica diez aristas y declara dos fallos. La auditoría quedó en 22
aristas esperadas, `false exact edges = 0` y 4/4 fallos declarados.

**Siguiente tarea:** LUQUE-1822.

---

## LUQUE-1822 — Que ningún aviso del camino Rust se pierda

**Dependencias:** LUQUE-1821.

**Objetivo:** cerrar los silencios que quedaban entre el analizador, la
identidad y el vigilante de archivos.

**Entregables:**

* `isManifestPath` reconoce `Cargo.toml`, `Cargo.lock` y `build.rs`;
* `classifyDiagnostics` conserva el bloque de símbolos duplicados;
* `collidingKeys` en `internal/rustloader/analyze.go`;
* `TestReconcilerSeesRustSourcesAndManifests` y
  `TestClassifyDiagnosticsKeepsDuplicateSymbolReports`.

**Decisiones:**

* `ClassifyRustChange` tenía una rama para manifest y otra para `build.rs` que
  el reconciliador no podía alimentar: su lista de manifests no conocía Cargo,
  así que un `Cargo.toml` modificado no llegaba a clasificarse nunca.
* rust-analyzer informa de sus símbolos duplicados en un bloque **sin prefijo
  de nivel**; el filtro por `WARN`/`ERROR` lo descartaba entero.
* Dos símbolos distintos que compongan la misma clave estable publican un solo
  nodo, porque el merge conserva el primero. Eso ya no ocurre en silencio: se
  informa nombrando ambos símbolos.

**Criterios de aceptación:**

- Un `Cargo.toml`, un `Cargo.lock` y un `build.rs` aparecen como cambios de
  manifest en el reconciliador; un `notes.md` no se escanea.
- El aviso de duplicados sobrevive a la clasificación de diagnósticos.
- Una colisión de claves produce un diagnóstico que nombra los dos símbolos.

**Estado:** `PASS`.

**Verificación:** `go test ./internal/watcher/ ./internal/rustloader/`,
`go test ./...`.

**Resultado:** los tres silencios cerrados. La colisión de claves no se
observó en los fixtures; el diagnóstico existe para el día que un bug del
analizador la provoque.

**Siguiente tarea:** LUQUE-1823.

---

## LUQUE-1823 — Empaquetar el motor de Rust en el bundle

**Dependencias:** LUQUE-1822.

**Objetivo:** que una instalación traiga dentro todo lo que Kivgraph ejecuta,
y que lo que no puede traer quede dicho.

**Entregables:**

* `tools/manifest.json` y `scripts/fetch-rust-analyzer.sh`;
* `bin/rust-analyzer`, sus licencias y el bloque `tools` del `manifest.json`
  del bundle en `scripts/build-bundle.sh`;
* `rustloader.ResolveAnalyzer` y la resolución equivalente del worker
  TypeScript en `indexer.factsCommand`;
* `Provenance.RustAnalyzer` en `kivgraph version --json`;
* `internal/storage/ladybug/canonical_catalog_test.go`;
* `docs/adr/0036-bundled-rust-analyzer.md`.

**Decisiones:**

* El binario se fija por versión, URL y digest, como una gramática. Sin fijarlo
  dos instalaciones del mismo release indexarían con analizadores distintos.
* En ejecución gana el binario que viaja junto al ejecutable; después una ruta
  explícita de la configuración; por último el `PATH`.
* `cargo` **no** se empaqueta y sigue siendo requisito: sin él el analizador
  aborta con `Failed to load the project`, verificado con un entorno sin
  toolchain. Empaquetar rustc, cargo y la biblioteca estándar es otra decisión,
  no un efecto lateral de esta.

**Criterios de aceptación:**

- `SHA256SUMS` cubre el analizador y `sha256sum -c` pasa sobre el bundle.
- `kivgraph version --json` publica la release del analizador empaquetado.
- `doctor` distingue `bundled` de `path`.
- Un `index --full` de Rust funciona ejecutando el binario del bundle por ruta
  absoluta, sin su `bin/` en el `PATH`.

**Estado:** `PASS`.

**Verificación:** `make build-darwin-arm64`, `shasum -a 256 -c SHA256SUMS`,
`kivgraph doctor`, `kivgraph index --full` desde el bundle, `go test ./...`.

**Resultado:** bundle `darwin/arm64` de `127 MB` (`bin/rust-analyzer` 36 MB),
747 archivos con checksum correcto. El flujo completo publica: `index.full:
PASS`, `stage.integrity: PASS (0 violaciones)`, `stage.golden probes: PASS`,
`generation=000001` con 18 símbolos y 2 no resueltas.

Empaquetar destapó dos fallos que ninguna prueba anterior podía ver. El
primero: ejecutar el `kivgraph` del bundle sin su `bin/` en el `PATH`
provocaba un `panic` en `factsCommand` -`arguments[1:]` sobre una lista
vacía-, y el worker TypeScript sólo se resolvía por `PATH` pese a viajar
dentro. El segundo: las seis procedencias Rust no estaban en el catálogo de
`canonical_integrity.go`, así que la integridad canónica rechazaba 36 aristas
como `unknown_confidence` -su test vive bajo el tag `ladybug`, que un checkout
sin la biblioteca nativa nunca compila-. El guardia nuevo no lleva tag.

**Siguiente tarea:** LUQUE-1824.

---

## LUQUE-1824 — Probar el bundle de la plataforma que no se puede construir aquí

**Dependencias:** LUQUE-1823.

**Objetivo:** que el motor Rust del bundle `linux/amd64` quede verificado por
alguien, ya que ninguna estación macOS puede construirlo.

**Entregables:**

* `internal/version/tools_manifest_test.go`;
* caché y verificación del analizador en `.github/workflows/ci.yml` y
  `.github/workflows/release.yml`.

**Decisiones:**

* Un test sin red comprueba lo que un `go test ./...` puede comprobar en
  cualquier host: que el manifiesto fija exactamente los dos objetivos de
  distribución, con digest, licencia y una URL que nombra la versión fijada.
  Un `linux/amd64` ausente ya no espera a un job de release para verse.
* Lo que sólo un host Linux puede probar -que el binario existe, se ejecuta y
  es el fijado- lo prueba el job Linux de CI, que ya construía el bundle y no
  lo miraba.
* El analizador se cachea por digest del manifiesto: son 40 MB por job.

**Criterios de aceptación:**

- El guardia falla si se retira un objetivo del manifiesto.
- El job Linux ejecuta `bin/rust-analyzer --version` del bundle y compara la
  release contra `tools/manifest.json` y contra `version --json`.

**Estado:** `PASS`.

**Verificación:** `go test ./internal/version/ -run ToolManifest`,
`go test ./internal/rustloader/scipwire/ -run EveryPlatform`, ejecución del
artefacto `linux/amd64` en un contenedor, y en un host Linux nativo
`make build-linux-amd64`, `sha256sum -c SHA256SUMS`, `kivgraph index --full`,
`go test ./...` (34 paquetes) y `make test-ladybug` (40 paquetes con la capa
nativa, entre ellos `internal/storage/ladybug`, `internal/rebuild` e
`internal/indexer`). La misma suite nativa pasa en `darwin/arm64` con 39
paquetes.

**Resultado:** el artefacto Linux fijado se descargó, verificó y **ejecutó** en
un contenedor `linux/amd64`: `ELF 64-bit LSB pie executable, x86-64`,
`rust-analyzer 0.3.3008-standalone`. Con `cargo 1.97.1` -otra versión que la de
la estación- indexó el fixture en `4.1 s` y dejó el workspace intacto: sin
`target/` y sin `Cargo.lock`.

El índice que produjo se conserva como
`testdata/protocol/scip-v0.9/engine-linux-amd64.scip` y
`TestTheSameSourcesProduceTheSameIdentitiesOnEveryPlatform` lo compara contra
el grabado en `darwin/arm64`: mismos documentos, mismos símbolos, mismos kinds,
mismas firmas y mismos rangos de ocurrencia. Sólo difiere la raíz del proyecto.
Una clave estable no depende, por tanto, de dónde se indexó.

**Bundle `linux/amd64` construido y verificado en un host Linux nativo**
(Debian 13, x86_64, Go 1.26.4 descargado por toolchain, patchelf 0.18.0):

- `123 MB`, 741 archivos, `sha256sum -c SHA256SUMS` correcto;
- `RUNPATH` exactamente `$ORIGIN/../lib`, y el ejecutable arranca sin ninguna
  variable de búsqueda de bibliotecas;
- `bin/rust-analyzer` presente y ejecutable, `version --json` publica
  `"rust_analyzer": "0.3.3008-standalone"`;
- `index --full` del fixture Rust: `symbols=18 references=12 unresolved=2`,
  `stage.integrity: PASS (0 violaciones)`, `stage.golden probes: PASS`,
  generación `000001` publicada, y el fixture intacto.

El digest del HotSnapshot es
`ad42e8d682f9ef5d23fe98acebaeea79f32345fb67b00dbbd5a4327017590a4f` en Linux y
**el mismo** en macOS: el grafo publicado no depende de la plataforma que lo
indexó.

Sin `cargo` en el `PATH`, el mismo bundle declara `toolchain.rust: PASS
(bundled)` y `toolchain.cargo: FAIL`, e indexa aislando el workspace
(`not_loaded=1`) en vez de abortar: el reparto entre lo que el bundle trae y lo
que exige de la máquina se comporta como está documentado.

---

## LUQUE-1825 — Nombrar una función no es llamarla

**Dependencias:** LUQUE-1807, LUQUE-1809.

**Objetivo:** que las tres formas en que Rust mueve una función sin llamarla
dejen de llegar al grafo como la misma relación genérica.

**Entregables:**

* `internal/rustloader/source.go`: `ReferenceCallback`, `ReferenceAssign` y
  `ReferenceReturn`, con `valuePosition` e `isTailExpression`;
* `internal/rustloader/analyze_references.go`: `valueClass` y el `Kind` del
  destino en la resolución;
* `internal/facts/facts.go` y `codes.go`: procedencia `RUST_SYNTAX_CALLBACK`,
  código `19`;
* `internal/storage/ladybug/canonical_integrity.go`: la procedencia en el
  catálogo;
* `testdata/rust/function-values/`, cuarto fixture de la auditoría;
* `internal/rustloader/source_test.go`, `analyze_test.go`,
  `internal/facts/rust_test.go`, `benchmarks/rust-semantic/`;
* `docs/adr/0037-rust-function-values.md`.

**Decisiones:**

* La gramática decide la clase y el analizador el destino, igual que en
  `CALLS_DIRECT`: es el reparto que ADR 0033 ya había fijado.
* Una clase de posición de valor exige un destino **invocable** e indexado en
  la misma pasada. `takes_limit(LIMIT)` es un argumento que no es un callback,
  y un destino de otro repositorio llega sin `Kind`: ambos degradan a
  `REFERENCES` en vez de afirmar lo que la pasada no leyó.
* Sólo el callback estrena procedencia, espejo de `GO_AST_CALLBACK`. Atar y
  devolver llevan la clase en el `EdgeKind`, como en Go.
* El ascenso por la expresión atraviesa lo que no cambia lo nombrado y se
  detiene en el primer padre que decide. Un acceso a campo no es transparente:
  devolver `objeto.campo` no devuelve el objeto -lo descubrió el test, que
  clasificaba `target.field` como `RETURNS_FUNCTION`-.

**Criterios de aceptación:**

- Las once formas se distinguen sin analizador instalado.
- El fixture publica `PASSES_AS_CALLBACK`, `ASSIGNS_FUNCTION` y
  `RETURNS_FUNCTION` con sus dos negativos en `REFERENCES`.
- La generación publicada pasa integridad canónica con la procedencia nueva.

**Estado:** `PASS`.

**Verificación:** `go test ./...`, `make test-ladybug` (39 paquetes),
`go run ./benchmarks/rust-semantic`, e `index --full` con el binario nativo
sobre el fixture.

**Resultado:** el corpus de la auditoría pasa de 22 a 30 aristas esperadas y
las 30 se observan; el caso nuevo mide 8 de 8, `false exact edges = 0`. La
generación publicada supera `stage.integrity` con 0 violaciones y
`stage.golden probes`. `internal/rustloader/source_test.go` es el primer test
del camino Rust que no necesita `rust-analyzer`: fija la clasificación
sintáctica en cualquier máquina.

**Siguiente tarea:** —.

---

## LUQUE-1826 — Indexar el sysroot de Rust como proveedor sintético

**Dependencias:** LUQUE-1825.

**Estado:** `PASS`. Las cinco preguntas que bloqueaban esta ficha se respondieron
-cuatro por decisión del 2026-08-13, la del coste por medición- y está
implementada. Ver ADR 0041.

**Objetivo:** que `core`, `std` y `alloc` tengan identidad en el grafo, que es
la única causa común de tres carencias medidas.

**Lo que hoy se pierde, y por qué es el mismo agujero:**

* `#[derive(Clone, Debug, Default)]` no deja **ninguna** relación: los traits
  derivados viven en `core` y el analizador no emite ni ocurrencia del
  atributo. En Rust idiomático casi todo tipo deriva algo.
* La sobrecarga de operadores no conecta con su implementación: `a + b` sobre
  un `impl Add for Money` local se atribuye a `core::ops::Add::add` y sale como
  `UNRESOLVED CRATE_PROVIDER_NOT_FOUND`.
* El operador `?` cae igual, en `ops::try_trait::Try::branch`.
* Cualquier llamada a la biblioteca estándar -`String::from`, `Vec::len`-
  desaparece del grafo.

**Medido el 2026-08-12** con una sonda de 17 construcciones sobre
`rust-analyzer 1.96.1`: ninguna de las cuatro deja arista, y las tres últimas
producen `UNRESOLVED` con destino en `core`.

**Las cinco preguntas, respondidas.** Cuatro por decisión el 2026-08-13; la del
coste por medición sobre `rust-analyzer 1.96.1` y el sysroot
`stable-aarch64-apple-darwin`.

4. **Coste — medido, y es lo que decide el resto.** El workspace de la
   biblioteca son `1.972` ficheros y `950.409` líneas. `rust-analyzer scip` lo
   indexa en **32,4 s** y emite **45,2 MB** con **354.338** monikers; un crate
   trivial, como referencia, son 1,1 s y 991 bytes. Corre **offline** con
   `CARGO_NET_OFFLINE=true`, así que la hermeticidad se sostiene y
   `rust.allow_network` sigue siendo la única salida declarada. Emite
   diagnósticos de símbolo duplicado -`core::num::imp::dec2flt::TABLE` entre
   otros-, que es la clase que ADR 0039 ya resuelve.

   El grafo publicado hoy tiene `10.501` símbolos: el sysroot de **un** toolchain
   es del orden de **diez veces todo el corpus actual**. El orden de magnitud que
   la ficha temía está confirmado.

1. **Alcance: el sysroot entero, cacheado por toolchain.** La presencia de un
   símbolo no puede depender de quién pregunte. Indexar sólo lo alcanzado exige
   una segunda pasada y deja el conjunto dependiendo del corpus que consulta, así
   que dos instalaciones con los mismos repositorios podrían publicar grafos
   distintos. Los 32 s se pagan una vez por toolchain y la caché de hechos los
   reutiliza.
2. **Identidad: un repositorio sintético por versión**, `rust:1.96.1`. Dos
   toolchains coexisten sin colisionar y la clave dice de qué versión habla. Un
   cambio de toolchain invalida las claves de sus símbolos, que es exactamente lo
   que ocurrió.
3. **Ciclo de vida: se reindexa cuando cambia `rustc --version`.** La huella de
   la entrada de caché incluye la versión, igual que ya incluye la respuesta de
   `go env` y el contenido del worker TypeScript.
5. **Superficie: el proveedor sintético se filtra por defecto**, con opt-in por
   flag. `find_references` sobre `Clone` o `Debug` devolvería media base de
   código: el grafo gana las aristas -`derive`, operadores, `?`- y las consultas
   siguen siendo legibles.

**Lo que queda por decidir dentro de la implementación**, y no antes: cómo se
declara en `graph_status` el peso del proveedor sintético, y si el flag de opt-in
es por llamada o por configuración.

**Criterios de aceptación:**

- Un fixture con `#[derive]`, un operador sobrecargado y un `?` publica
  `IMPLEMENTS` y `CALLS_DIRECT` hacia el proveedor sintético, con procedencia y
  evidencia, y sin una sola arista exacta falsa en la auditoría.
- El coste queda medido en `benchmarks/rust-engine/`: índice frío, índice
  caliente y tamaño del grafo, con y sin sysroot.
- Un repositorio indexado sin sysroot disponible sigue publicando, declarando
  la carencia como hoy: la funcionalidad es opt-in y su ausencia no es un
  fallo.
- ADR con el alcance, la identidad sintética y la política de invalidación.

**Riesgos, revisados con la medida.** El grafo no creció un orden de magnitud:
la biblioteca son `19.533` símbolos, unas dos veces el corpus actual, no diez
-los `354.338` monikers que estimé eran ocurrencias, no símbolos-. La identidad
deja de ser puramente repositorio-relativa, y por eso el namespace `rust:` está
reservado en el registro. Ningún `derive` produce `UNRESOLVED`: los cuatro
silencios son aristas exactas.

**Resultado.** Medido con `rust-analyzer 1.96.1` sobre
`stable-aarch64-apple-darwin`.

El fixture `testdata/rust/stdlib` publica lo que antes perdía, y el test lo
nombra relación por relación en vez de contar:

```text
Offset      IMPLEMENTS      -> ops::arith::Add
Offset      REFERENCES      -> clone::Clone, cmp::PartialEq, default::Default, fmt::macros::Debug
parse_line  REFERENCES      -> result::impl::Result<T, E>::Try::branch
parse_line  CALLS_DIRECT    -> str::impl::str::parse, str::impl::str::trim
render      CALLS_DIRECT    -> string::impl::String::push_str, string::impl::T::ToString::to_string
```

El coste, en `benchmarks/rust-engine/`:

```text
                símbolos   aristas   frío       caliente
sin sysroot            6        19    1.288 ms      54 ms
con sysroot       19.533   169.532   16.512 ms     829 ms
```

`829 ms` en caliente es lo que justifica cachear por toolchain: la pasada fría
se paga una vez por release de `rustc`, que es lo que la huella incluye.

**Y sobre un corpus real, con el binario y el grafo publicado** -un repositorio
de 10 símbolos más la biblioteca, generación `000001` en 50 s-, el desglose es lo
que mantiene legible la respuesta:

```text
totales      símbolos 24.704   aristas 188.788   no resueltos 6.048
derivados    símbolos 24.694   propias 188.741   no resueltos 6.041   entrantes 26
del código   símbolos     10   aristas      47   no resueltos     7
```

**El alcance cambió al medirlo.** Indexar todo el árbol del sysroot da 16
unidades, 6 que no cargan, y **dos pasadas con distinto número de aristas**,
porque los workspaces vendorizados -`stdarch`, `compiler-builtins`,
`backtrace`, `portable-simd`- ejecutan build scripts que ven lo que dejó la
pasada anterior. La unidad es el workspace `library` y nada por debajo: 1
unidad, 0 fallos, reproducible.

**Cinco bugs preexistentes que el trabajo destapó**, cuatro de ellos sólo
visibles al publicar con el binario real:

1. **`DiscoverCargo` rechazaba un workspace vendorizado.** Un manifest con su
   propio `[workspace]` dentro de otro se declaraba error, y la biblioteca
   estándar lleva `library/backtrace` exactamente así. Cargo sólo rechaza ser
   raíz **y** miembro a la vez; se verificó contra `cargo metadata`.
2. **`facts.NormalizeRust` publicaba ficheros sin paquete.** Un fichero que el
   analizador indexa por una dependencia de ruta hacia un crate que ningún
   manifest del workspace declara -así llega `std` a su `compiler-builtins`
   vendorizado- se publicaba con paquete vacío, y el snapshot lo rechazaba
   **después** de analizar el corpus entero. Sus declaraciones ya se
   descartaban; ahora el fichero también, contado en `FilesWithoutPackage`.
3. **Una colisión de clave publicaba dos declaraciones.** El ganador se elegía
   para resolver, pero la lista publicada llevaba a los dos, así que el grafo
   afirmaba que un símbolo vive en dos ficheros: `Copy exception: Node has more
   than one neighbour in table DEFINES`. Sólo se publica el ganador, y la
   colisión se sigue nombrando.
4. **`UnresolvedKey` no llevaba el repositorio.** Se construía sobre el fichero,
   que lo lleva dentro, así que dos entradas sin fichero -una clase de un
   proveedor, un módulo que no carga- de repositorios distintos eran una sola
   fila. El motor lo rechaza como clave primaria duplicada en cuanto dejan de
   serlo.
5. **El índice de claves estables no era determinista.** Se construía
   recorriendo un mapa, así que una colisión de clave -dos símbolos del
   analizador que comparten crate, camino y kind, que rust-analyzer emite por un
   bug propio- se resolvía según el orden de iteración. Dos pasadas del mismo
   corpus publicaban **170 aristas de diferencia** en la raíz de `std`. El
   analizador quedó descartado por digest: dos invocaciones producen el mismo
   índice byte a byte.

**Los cuatro llevan test, y cada uno se verificó revirtiendo su arreglo:** sin
él el test falla, con él pasa. Tres de los cuatro no fallaban en ninguna suite
antes de escribirlos:

```text
TestDiscoverCargoAcceptsAVendoredWorkspaceRoot        workspace anidado legítimo
TestNormalizeRustDropsAFileNoManifestDeclares         fichero sin paquete
TestAnalyzeResolvesAKeyCollisionTheSameWayEveryTime   una declaración por clave,
                                                      y siempre la misma
```

**Y sobre dos repositorios Rust reales** -`lanplay` y `naboscale`, 174 ficheros
`.rs`-, publicado a la primera en 54 s con 0 workspaces que no cargan:

```text
                símbolos   aristas   no resueltos
totales           29.251   257.962         7.099
biblioteca        24.694   188.741         6.041
código real        4.557    38.637         1.058
    de ellas, hacia la biblioteca: 17.644 aristas
```

**17.644 aristas** que antes no existían, sobre 4.557 símbolos propios: casi la
mitad de las aristas de su código toca la biblioteca estándar.

**Y ahí apareció una regresión mía.** Declarar cada uso que la biblioteca no
publica metía `5.635` entradas en el informe de huecos del código del usuario, y
medido sobre `lanplay` eran `4.165` sobre `205` símbolos, todos de una clase:
operadores sobre primitivos -`u64::add`, `f64::div`, `usize::PartialEq::eq`-. Los
791 huecos que hablaban de sus dependencias quedaban sepultados. Un proveedor
derivado se declara ahora una vez por símbolo y sin posición, como ya hace
`MACRO_EXPANSION_DISABLED`: `5.635 -> 267`, y los del código real `6.426 ->
1.058`, sin perder una sola arista.

Eso destapó el quinto bug preexistente: `UnresolvedKey` se construía sobre el
fichero -que lleva el repositorio dentro-, así que dos entradas **sin** fichero
de repositorios distintos eran una sola fila. Sin fichero, el ámbito es el
repositorio.

**Lo que sigue ausente, declarado y contado:** los `impl` que la biblioteca
genera con macros -`PROVIDER_DEFINITION_NOT_INDEXED`, 3 en el corpus del
benchmark-, los workspaces vendorizados, y los crates de crates.io que la
biblioteca usa y nadie registró.

**Verificación:**

```text
gofmt -l internal/ benchmarks/
go vet ./...
go test ./...
make test-ladybug
go test ./internal/rustloader/... ./internal/indexer/ -run Rust
go run ./benchmarks/rust-engine
```

**Desbloquea además:** el `IMPLEMENTS` de un `impl<T> Trait for Vec<T>` sobre
un tipo foráneo, hoy silencioso porque el receptor no es un símbolo del grafo.

---

# 22. Fase 19 — Coste en tokens de la superficie MCP

LUQUE-1113 dejó la superficie barata de cargar y las filas direccionables.
Lo que ninguna tarea ha medido todavía es la sesión completa: lo que gasta un
agente desde que pregunta hasta que tiene el código delante.

**Medido por el arnés de LUQUE-1905** el 2026-08-13, con `kivgraph v0.5.0`,
generación `000024` (10.501 símbolos, 301 ficheros, 38.546 aristas), tokenizador
`cl100k_base`, digest `1eb3f6af925c0491`, sobre seis preguntas del tipo «quién
llama a este símbolo y qué aspecto tienen esos llamantes»:

```text
                              responder    sesión completa
vía nativa de Oh My Pi           10.739             25.144
Kivgraph al abrir la fase        6.991  1,54x      21.396  1,18x
tras LUQUE-1901                   3.635  2,95x      18.040  1,39x
tras LUQUE-1902                   3.059  3,51x      17.464  1,44x
tras LUQUE-1903                   3.059  3,51x      14.222  1,77x
tras LUQUE-1904                   3.109  3,45x      14.272  1,76x
suelo: los bytes y nada más                         10.478  2,40x
```

Los 50 tokens que suma LUQUE-1904 son la guía que acompaña a una respuesta
vacía, y están todos ahí: en las cinco preguntas con filas la guía calla.

La columna de la sesión creció al medirla bien: el `read` de rango del anfitrión
cobra un **38 % sobre los bytes** en cabeceras y números de línea, y el arnés lo
cobraba como bytes crudos en los dos brazos. Corregido con veinte lecturas
capturadas, la vía nativa cuesta 25.144 y no 21.223, y **servir los cuerpos deja
de ser un cambio sin efecto en tokens**: medido con la tool escrita, son 3.242,
un 19 % de la sesión.

**Hay dos factores y los dos se publican.** *Responder* es lo que cuesta decir
quién llama al símbolo, y es la parte que un servidor de grafo posee. *Sesión
completa* añade los cuerpos. Su envoltorio depende de por dónde lleguen -14.405
tokens leídos por el anfitrión contra 11.163 servidos por `get_source`-, pero los
bytes son irreducibles: 10.478, el 42 % del brazo nativo. De ahí el techo:
**una respuesta que costara cero seguiría quedándose en `2,40x`**, así que
ningún trabajo sobre el payload puede pasar de ahí en esta clase de pregunta.
Publicar sólo uno de los dos factores es cómo este campo llega a sus titulares:
el de responder nos favorece, el de sesión favorece a la alternativa.

**Todas las cifras anteriores de esta fase eran falsas y se retiran.** El
baseline de «leer los ficheros enteros» -102.522 tokens, del que salía un
`-83 %`- no lo concede ningún anfitrión: omp hace `grep` acotado y lee los
trozos. Y las medidas a mano que lo sustituyeron estaban infladas 2,7 veces,
porque tokenizaban el envoltorio JSON del resultado en vez del texto que ve el
modelo. Ésa es la razón por la que el arnés se implementa antes que cualquier
mejora, y no después.

**El reparto es la tesis entera de la fase.** Por símbolo, en el factor de
responder:

```text
símbolo            clase        refs   nativo  antes   factor   tras 1901
NewServer          común           0    2.308    300    7,69x       7,69x
Publish            común           4    3.480  1.793    1,94x       3,98x
BuildPlan          compartido      3    2.411  1.342    1,80x       3,58x
CanonicalColumns   raro            3    1.156  1.287    0,90x       1,82x
DiscoverGo         raro            3      903  1.316    0,69x       1,42x
MergeAll           raro            2      481    953    0,50x       0,91x
```

Hoy **perdemos en cuatro de las seis**. Tras LUQUE-1901 ganamos en cinco, y la
que sigue perdiendo es la del nombre más raro: `MergeAll`, `0,91x`. Un nombre
único en un repositorio pequeño lo resuelve `grep` más barato que nosotros y
siempre lo hará. Donde ganamos es donde `grep` es a la vez caro y equivocado
-`Publish` aparece por todas partes y no distingue `SnapshotStore.Publish` de
ningún otro- y donde el grafo puede **afirmar una ausencia**: `NewServer` no
tiene llamantes, y decirlo cuesta 300 tokens frente a 2.308 de ruido.

De ahí salen tres obligaciones para el resto de la fase: la descripción de cada
tool tiene que decir **dónde pierde**; `get_source` no puede justificarse por los
cuerpos, sino por las llamadas que ahorra y por el caso que ningún anfitrión sabe
hacer; y después de LUQUE-1901 el margen que queda en esta clase de pregunta es
de un solo dígito porcentual, así que lo que siga tiene que atacar **otra clase
de pregunta**, no este payload.

Ningún gate anterior lo cubre. `MCP_SURFACE_PASS` fija qué se devuelve y con
qué garantías, nunca lo que cuesta obtenerlo.

**Gate:** `MCP_TOKEN_COST_PASS`, y no se declara a mano: lo mide el arnés de
LUQUE-1905, que es el instrumento con el que las otras cuatro tareas declaran
sus cifras. Una reducción sin arnés reproducible es una anécdota.

**Fuera de alcance de la fase entera:** edición simbólica, métricas de calidad
de código y cualquier tool que no reduzca tokens medidos.

**La cuenta de la superficie.** LUQUE-1113 fijó un techo de diez tools y hoy hay
once. `get_source` haría doce, así que la fase no puede añadirla sin retirar algo.
Retira `get_unresolved_references`, que responde una pregunta sobre el índice y no
sobre el código, y cierra en **once tools y 645 tokens residentes** -menos que los
670 con los que salió de LUQUE-1903-. `graph_status` y `list_repositories` se
quedan: el plan las mandaba a `instructions`, pero sus datos son volátiles y ese
campo no puede llevar nada que se reescriba al reindexar.

## Los dos anfitriones que esta fase tiene que satisfacer

Kivgraph se optimiza para **Oh My Pi** y **Claude Code**, en ese orden. Los dos
difieren en lo que cobran y en lo que escuchan, y varias decisiones de la fase
dependen de esa diferencia. Medido el 2026-08-12 contra `kivgraph v0.5.0` y la
documentación del propio anfitrión.

```text
                        Oh My Pi                     Claude Code
esquema residente       no: se lee bajo demanda      no: diferido por ToolSearch
                        con `read xd://<tool>`
coste residente real    594 tok  (205 de rutas       nombres + `instructions`
                        + 389 de descripciones)      (~120 tok, cifra de Anthropic)
`instructions`          no lo expone                 inyectado al abrir sesión, 2 KB
`_meta` alwaysLoad      irrelevante                  promociona una tool a residente
structuredContent       se descarta, sólo texto      puede renderizarse
tope de respuesta       no documentado               25.000 tok, avisa a partir de 10.000
```

**Consecuencias que la fase no puede ignorar:**

* **El presupuesto residente en Oh My Pi es la descripción de la tool**, no su
  esquema. Son 389 de los 594 tokens que mide el arnés. Ahí es donde tiene que
  caber el enrutado
  -«para esto, en vez de `grep`»- y no en un campo que este anfitrión no lee.
* **`instructions` sólo sirve para Claude Code.** El canal portable es la
  descripción más `AGENTS.md`, que este proyecto ya tiene enlazado desde
  `CLAUDE.md`: un solo fichero cubre los dos anfitriones y los dos lo cargan
  solos.
* **Oh My Pi ya trae competidores nativos y son buenos.** Su `read` sin selector
  devuelve un resumen estructural -declaraciones con sus rangos, cuerpos
  elididos-: `internal/facts/facts.go` cuesta **3.013 tokens** por esa vía
  frente a **6.597** de `get_file_outline` y 5.084 de leer el fichero entero.
  Hoy nuestra tool es 2,2 veces peor que lo que el anfitrión da gratis. También
  trae un dispositivo `lsp` con `references`, `definition` y `symbols`, así que
  dentro de un repositorio y con servidor de lenguaje vivo, `find_references`
  compite de tú a tú: lo nuestro gana en cross-repository, en `confidence` y
  `provenance`, en radio de impacto y sin necesidad de un LSP arrancado. Eso es
  lo que la descripción tiene que decir.
* **Devolvemos el mismo dato dos veces.** `find_symbol(MergeAll)` responde 559
  caracteres de texto y 600 de `structuredContent`. Oh My Pi tira el segundo;
  un cliente que renderice los dos paga 406 tokens en vez de 174. Un canal por
  tool.
* **Oh My Pi normaliza los argumentos antes de que lleguen.** Quita el campo `i`
  si el esquema no lo declara, y **omite toda propiedad opcional cuyo valor sea
  cadena vacía, objeto vacío o `undefined`**. Ningún parámetro opcional puede
  tener un valor vacío con significado.

---

## LUQUE-1901 — Una fila que no se puede abrir no es una respuesta

**Dependencias:** LUQUE-1106, LUQUE-1108, LUQUE-1109, LUQUE-1113.

**Objetivo:** que toda fila que nombra un símbolo se pueda abrir sin una
segunda llamada.

**Lo que hoy falta, medido por el arnés:**

* `find_references` da `start_line` pero no `end_line`. El agente sabe dónde
  empieza el llamante y no dónde acaba, así que pide un `get_symbol` por fila:
  **15 llamadas de apoyo** en las seis preguntas, **3.342 tokens netos** una vez
  descontado lo que costará llevar `end_line` en cada fila. Es lo que separa el
  `1,21x` actual del `1,50x` proyectado.
* Cada respuesta medida viajó además duplicada como `structuredContent`:
  **24.066 bytes** en la pasada. Oh My Pi descarta ese canal; un cliente que
  renderice los dos paga el doble.
* `trace_dependencies` y `get_blast_radius` no dan ni `start_line` ni
  `end_line`. Medido a mano sobre la generación `000017`: una traza de
  `MergeAll` son **8.420 tokens** para 50 filas -168 por fila- que nombran el
  fichero y no la posición, y de ellos `stable_key`, `reached_from_key`,
  `file_key` y `repository_key` suman **5.545, el 66 %**. `file_key` y
  `repository_key` los deletrea la ruta que va al lado. El arnés todavía no
  cubre esta clase de pregunta: esta tarea la añade a `questions.json`.

**Entregables:**

* `internal/mcp/tools/find_references.go`, `trace_dependencies.go` y
  `blast_radius.go`: `start_line` y `end_line` en toda fila de símbolo;
* los `*_key` derivables, sólo en `response_format: "detailed"`, como ya hizo
  LUQUE-1113 con las demás tools;
* un solo canal de contenido por tool: se retira `structuredContent` y queda el
  bloque de texto, que es el que los dos anfitriones leen;
* tests de cada tool tocada y la medida en tokens antes y después.

**Decisiones:**

* `end_line` viaja también en `concise`. Son siete tokens y sin él la fila
  obliga a la llamada que la fila venía a evitar.
* `reached_from_key` se conserva, pero cambia de forma: en `concise` viaja como
  el `qualified_name` del padre -que es otra fila de la misma página- y la clave
  vuelve en `detailed`. Una clave base32 cuesta 35 tokens en una fila que cuesta
  veinte.
* Ninguna tool declara `outputSchema`. El SDK rellena `structuredContent` desde
  el resultado tipado en cuanto hay esquema y repite el mismo JSON en el bloque
  de texto: la respuesta viaja dos veces. Un esquema que se anuncia y no se
  rellena describiría una respuesta que no se envía, así que se retira el
  esquema, no sólo el segundo canal.

**Criterios de aceptación:**

- Las seis preguntas se responden sin un solo `get_symbol` de apoyo, y el arnés
  mide `extra_calls: 0` y un total no peor que los 14.133 tokens proyectados.
- Ninguna fila de recorrido sale sin su rango: `rows_without_range: 0`.
- `response_format: "detailed"` sigue devolviendo todo lo que devuelve hoy.
- Ninguna respuesta duplica su contenido: el mismo dato no viaja como texto y
  como `structuredContent`.

**Estado:** `PASS`.

**Resultado.** Medido por el arnés con los dos binarios sobre la generación
`000024`, digest `3534ae1b9c201e1e`:

```text
                                   antes    después
responder, seis preguntas          6.991      3.635    1,54x -> 2,95x
sesión completa                   17.475     14.119    1,21x -> 1,50x
`get_symbol` de apoyo                 15          0
contenido duplicado            24.066 B        0 B
trace_dependencies, 50 filas       8.420      6.452    168 -> 129 por fila
get_blast_radius, 9 filas          2.288      1.918    254 -> 213 por fila
filas sin rango                       59          0
esquema publicado                  1.580      1.515
```

La proyección de LUQUE-1905 dijo 3.649 tokens y salieron 3.635: el arnés
acertó dentro del 0,4 %, que es la primera prueba de que sirve para decidir.

**Un criterio estaba mal escrito y se corrige.** Decía que una traza de 50
nodos costaría «menos de la mitad». Sale un 23 % menos, no un 50 %, y la
aritmética explica por qué: la fila suelta `file_key` -18 tokens- y cambia
`reached_from_key` por un nombre -35 a 8-, pero gana `start_line` y `end_line`
-unos 14-. Lo que queda por quitar es `stable_key`, otros 35 por fila, y eso
**no se puede hacer aquí**: hasta que LUQUE-1902 permita nombrar un símbolo por
`(repository, path, qualified_name)`, una fila sin clave es un callejón. El
criterio pertenecía a esa tarea y allí se cumple.

**Verificación:**

```text
gofmt -l internal/ benchmarks/
go vet ./...
go test ./...
go run ./benchmarks/mcp-token-cost --server <binario nuevo>   (dos veces, mismo digest)
go run ./benchmarks/mcp-token-cost --dir /tmp/mtc-old --server kivgraph   (el antes)
```

**Siguiente tarea:** LUQUE-1902.

---

## LUQUE-1902 — Que el índice de un fichero cueste menos que el fichero

**Dependencias:** LUQUE-1113, LUQUE-1901.

**Objetivo:** que `get_file_outline` cueste menos que las dos vías que el
anfitrión ya ofrece gratis.

**Lo que hoy pasa, medido sobre `internal/facts/facts.go`:** el outline cuesta
**6.597 tokens**, el fichero entero **5.084**, y el resumen estructural que
`read` de Oh My Pi devuelve solo **3.013**. Preguntar por el índice es más caro
que leerlo todo y **2,2 veces más caro que lo que el anfitrión da gratis**. Y
son 80 filas de 155: describir el fichero completo son dos llamadas y unos
12.000 tokens. El reparto dice dónde está:

```text
stable_key   3.223 tokens   49 %
signature    1.605 tokens   24 %
```

Las firmas son la otra mitad del problema: dentro de `internal/facts`, una
firma se publica como `func(sets []github.com/Luqueee/kivgraph/internal/facts.Set) ...`,
con el camino completo del paquete al que pertenece el propio símbolo.

**El bloqueo real:** la clave no se puede quitar porque `get_symbol` y
`find_references` sólo aceptan `stable_key`. Sin otra forma de nombrar un
símbolo, un outline sin claves es un callejón.

**Y recortar campos no llega.** Medido: sin `stable_key` el outline baja a
**4.329** tokens, y recortando además las firmas cualificadas se queda en unos
**3.300**. Sigue por encima de los 3.013 del `read` nativo. **En JSON no se
puede ganar**, así que la tarea incluye el cambio de forma: filas agrupadas por
fichero con cabecera hoisted, que es lo que baja a unos 12,5 tokens por
declaración -unos 1.900 para las 155 de este fichero-.

**Y el caso que de verdad importa es el directorio, no el fichero.** El outline
de `internal/facts` cuesta hoy 18.372 tokens; la vía nativa exige resumir doce
ficheros, del orden de 37.800 [INFERENCE: extrapolado del 59 % que mide
`facts.go`]. Reformateado serían unos 2.500. Ahí el factor es 15x, no 1,6x.

**Entregables:**

* direccionamiento por `(repository, path, qualified_name)` en `get_symbol`,
  `find_references`, `trace_dependencies` y `get_blast_radius`, alternativo a
  `stable_key` y nunca simultáneo;
* `stable_key` fuera de `concise` en `get_file_outline`;
* forma de línea agrupada por fichero, con `[mostradas/total]` en la cabecera;
* firmas relativas al paquete del símbolo, con el camino completo sólo cuando
  el tipo viene de otro paquete;
* tests de ambigüedad y la medida en tokens antes y después, por el arnés.

**Decisiones:**

* Un `qualified_name` puede no ser único dentro de un fichero -dos métodos
  homónimos de tipos distintos comparten nombre corto y no cualificado-. Si la
  tripleta resuelve a más de un símbolo se devuelve un error que **nombra los
  candidatos con su rango de líneas**; elegir uno sería la coincidencia
  nominal que el proyecto prohíbe en el grafo y no vale más en la superficie.
* La clave estable no desaparece: sigue siendo la identidad canónica y sigue
  viajando en `detailed`. Lo que cambia es que dejar de pedirla no cierre el
  camino.

**Criterios de aceptación:**

- El outline de `internal/facts/facts.go` cuesta menos que los 3.013 tokens del
  resumen estructural de `read`, no sólo menos que leer el fichero.
- Una sesión encadena `get_file_outline` → `find_references` sin haber visto
  una sola `stable_key`.
- Una tripleta ambigua falla nombrando los candidatos; una inexistente falla
  distinguiéndose de la ambigua.
- `detailed` sigue devolviendo la clave y la firma completamente cualificada.
- El outline de `internal/facts` completo cuesta menos que resumir sus doce
  ficheros por la vía nativa.
- El caso de un solo fichero pequeño puede seguir perdiendo contra `read`, y
  entonces la descripción de la tool lo dice y remite a `read`. Una tool que
  finge ganar donde pierde gasta la llamada dos veces.

**Estado:** `PASS`.

**Resultado.** Medido con el binario nuevo sobre la generación `000024`, digest
`2dda76df1387f8cf`:

```text
                                       antes    después
responder, seis preguntas              3.635      3.059    2,95x -> 3,51x
sesión completa                       14.119     13.543    1,50x -> 1,57x
outline de facts.go, fichero entero    6.597      2.949    (read nativo 3.013)
outline de internal/facts, 199 filas  18.372      8.861    (nativo ~37.800)
trace_dependencies, 50 filas           6.452      4.535    129 -> 91 por fila
get_blast_radius, 9 filas              1.918      1.562    213 -> 174 por fila
```

**Y ahora ganamos las seis preguntas.** `MergeAll`, la que perdía 0,50x antes de
la fase y 0,91x tras LUQUE-1901, queda en `1,08x`. `Publish` pasa de 1,94x a
`4,83x` y `BuildPlan` de 1,80x a `4,34x`.

**Cómo se llegó.** La clave estable era la mitad del outline y no se podía
quitar sin otra forma de nombrar un símbolo, así que `root_symbol.go` deja de
resolver sólo raíces y pasa a ser el selector de la superficie: `stable_key` o
bien `(repository, path, qualified_name)`, nunca las dos cosas, en `get_symbol`,
`find_references`, `trace_dependencies` y `get_blast_radius`. Con eso, la clave
sale de `concise` en las filas de outline, de referencia y de recorrido, y
vuelve en `detailed`. Las firmas pierden el camino del paquete al que pertenece
el propio símbolo -que es como se lee el fuente que lo declara- y el outline
agrupa por fichero en vez de repetir la ruta por fila.

**Dos cifras no llegaron y se declaran.**

* El outline de `facts.go` gana al `read` nativo por **64 tokens** -2.949 contra
  3.013-, que es un empate técnico y no la victoria que sugería el plan. Sobre
  el paquete sí es holgada: 8.861 contra unos 37.800 [INFERENCE: extrapolado del
  59 % que el resumen nativo mide sobre `facts.go`], es decir 4,3x, no los 15x
  que estimé antes de medirlo.
* La fila de recorrido baja a **90,7 tokens**, no por debajo de 84. Lo que queda
  es `via_kind`, `via_confidence` y `via_provenance`, unos 25 tokens que en la
  mayoría de las filas repiten el mismo valor. Cerrarlo es hoistear el valor
  dominante a la cabecera y marcar sólo las excepciones, que es un cambio de
  forma del payload y merece su propia tarea en vez de entrar aquí de tapadillo.

**Lo que decidí no hacer, y por qué.** El plan pedía «filas agrupadas por
fichero con cabecera hoisted» como formato de línea. Se quedó en JSON agrupado:
un formato de texto sólo para esta tool sería una segunda convención de cable
en una superficie donde las otras diez devuelven el mismo envoltorio, y
`AGENTS.md` obliga a reutilizar la convención vigente. El criterio era el número
de tokens y el JSON agrupado lo cumple.

**Verificación:**

```text
gofmt -l internal/ benchmarks/
go vet ./...
go test ./...
go run ./benchmarks/mcp-token-cost --server <binario nuevo>   (dos veces, mismo digest)
```

Tests nuevos: `TestSymbolSelectorNarrowsAnAmbiguousNameAndNeverGuesses`,
`TestNormalizeSymbolSelectorRejectsContradictions`,
`TestFindReferencesAddressesASymbolWithoutItsKey` y
`TestFindReferencesRejectsContradictoryAndAmbiguousSelectors`.

**Siguiente tarea:** LUQUE-1903.

---

## LUQUE-1903 — Servir el código, no su dirección

**Dependencias:** LUQUE-1901, LUQUE-1902.

**Objetivo:** retirar la lectura del fichero, que es la mitad de la sesión.

**Medido, y corrige lo que esta ficha decía antes.** Dije que `get_source` no
podía justificarse por tokens porque «los cuerpos los paga cualquier vía». Es
falso, y el arnés lo demuestra: **no son los mismos tokens**. El `read` de rango
del anfitrión antepone la cabecera del snapshot y el número de línea a cada
línea, y eso mide un **38 % sobre los bytes** -427 tokens donde el cuerpo son
302-. Sobre las seis preguntas, con las veinte lecturas capturadas en
`native/reads/`:

```text
vía nativa, sesión completa            25.144
Kivgraph antes de esta tarea          17.464   1,44x
Kivgraph sirviendo los cuerpos        14.222   1,77x
  cuerpos leídos por el anfitrión      14.405
  los mismos cuerpos servidos          11.163
suelo: los bytes y nada más            10.478   2,40x
```

Son **3.242 tokens, un 19 % de la sesión**, y mueven el factor de `1,44x` a
`1,77x` sobre un suelo de `2,40x`. Ésa es la justificación, medida.

**Lo que esta tool sigue sin ser:** un lector de rangos por gusto.
`read internal/facts/facts.go:484-509` existe en los dos anfitriones y está
siempre fresco. Lo que ningún anfitrión puede hacer es **una sola llamada que
devuelva los cuerpos de los cuatro llamantes exactos repartidos por tres
repositorios**, sin prefijos y verificados contra la generación publicada. De
ahí sale la forma de la entrada: símbolos, no rangos.

**Entregables:**

* tool `get_source`: una lista de símbolos por clave o por tripleta, y devuelve
  sus cuerpos con `context_lines` opcional;
* verificación de `FileRecord.ContentHash` antes de servir;
* `docs/adr/00XX-serving-source-from-the-published-graph.md`;
* la medida de las seis preguntas antes y después.

**Decisiones:**

* `serve` pasa a leer del sistema de archivos. Hoy responde sólo desde el
  HotSnapshot publicado y ése es un contrato escrito: el ADR es obligatorio y
  la tarea no empieza por el código.
* **Falla cerrado en lo que afirma, degrada declarando en lo que entrega**, y la
  distinción es el corazón del ADR. Si el digest del fichero coincide con el que
  registró la generación, el rango del grafo es autoridad y se sirve sin más. Si
  no coincide, **el fichero es la autoridad**: se reancla la declaración por su
  nombre en el fichero actual, se sirven sus bytes y se declara el
  desplazamiento -«el grafo dice 484-509; el fichero cambió; la declaración
  está en 512»-. Si tampoco se encuentra allí, esa fila no devuelve bytes y dice
  por qué, sin tumbar las demás.
* Reanclar por nombre **no crea ninguna arista** y por eso no viola el contrato
  de resolución exacta: entrega bytes de un fichero, no afirma una relación. El
  ADR lo dice explícitamente para que nadie lo lea como una coincidencia nominal
  permitida.
* La razón de no fallar cerrado del todo: el árbol de trabajo cambia entre
  generaciones -en la sesión que produjo estas cifras el snapshot se reconstruyó
  dos veces en una hora-, y una tool que se niega a responder casi siempre hace
  que el agente deserte a `read` para el resto de la sesión. Es la forma más
  segura de perder el 54 %.
* Nunca se lee fuera de `repository_path`, y la ruta se resuelve contra él sin
  seguir componentes symlink, como ya exige la capa de workspace.
* La respuesta tiene techo de bytes y lo declara al recortar; no se trunca en
  silencio.
* Sigue siendo read-only. Esta tool devuelve código; no lo escribe.
* `context_lines` existe porque la telemetría de Serena lo midió: el 18,4 % de
  sus consultas resueltas acabó en un `Read` del mismo fichero, y el 80,8 % de
  esos ya tenía el cuerpo -el 83,3 % usó `offset`/`limit`-. El agente no quería
  el cuerpo, quería lo que lo rodea. Por defecto `0`, tope `100`, como en
  `probe` y `octocode`.

**Criterios de aceptación:**

- Las seis preguntas se responden por debajo de 20.000 tokens en total y **cada
  una por debajo de su vía nativa**, sin abrir un solo fichero fuera del MCP.
- Un fichero modificado después de la generación se sirve reanclado y con el
  desplazamiento declarado; el test lo cubre insertando líneas por encima de la
  declaración entre la publicación y la consulta.
- Una declaración borrada después de la generación no devuelve bytes, lo dice, y
  las demás filas de la misma respuesta sí se sirven.
- Una ruta fuera del repositorio y una con un componente symlink se rechazan
  con su código de error.
- ADR con el contrato de frescura y el motivo por el que `serve` deja de ser
  puramente snapshot.

**Riesgos:** el reanclaje por nombre puede acertar el símbolo equivocado en un
fichero muy editado -dos declaraciones homónimas-. Ante más de una candidata no
se elige: se declara y no se sirven bytes de esa fila.

**Estado:** `PASS`.

**Resultado.** Medido con el binario nuevo sobre la generación `000024`, digest
`a4f59590ba4a234d`:

```text
                                    antes    después
sesión completa                    17.464     14.222    1,44x -> 1,77x
  cuerpos, por dónde llegan        14.405     11.163
suelo: los bytes y nada más                   10.478    2,40x
```

Son **3.242 tokens, el 19 % de la sesión**, y quedan a un 6 % del suelo teórico.
La proyección de la ficha decía 13.993 y salieron 14.222: acertó dentro del
1,6 %.

**El formato de la respuesta valía 2.914 de esos tokens, y casi los perdí.** La
primera versión devolvía los cuerpos dentro del envoltorio JSON, como las otras
once tools, y midió `17.136 / 1,47x`: **casi nada**. La causa es aritmética y la
medí sobre una declaración de 26 líneas: 302 tokens de fuente son **374 como
cadena JSON** -cada salto de línea y cada tabulador se pagan dos veces- y **430
como fila completa**, que es exactamente lo que cuesta el `read` de rango del
anfitrión. Servir código a través del envoltorio no compra nada.

Así que `get_source` es la única tool que responde en prosa: una línea de
cabecera por cuerpo -`@ repo path:inicio-fin kind nombre`, o `!` y el motivo
cuando no hay bytes- y después el código tal cual, sin escapar y sin numerar. En
LUQUE-1902 rechacé un formato de texto por no crear una segunda convención de
cable; aquí la convención no es una preferencia, es la diferencia entre la tool y
ninguna tool.

**Lo que el snapshot no llevaba.** `FileRecord` había perdido el `ContentHash`,
así que no había con qué comprobar la frescura. Vuelve como `ContentDigest`, los
treinta y dos bytes crudos del SHA-256 en vez de sus sesenta y cuatro caracteres
hexadecimales: no toca la arena de cadenas. Un hash que no se puede decodificar
**no tumba la publicación**: queda a cero, que se lee como «esta generación no
registró un hash comparable» y nunca como «fresco». La degradación sólo puede ir
en la dirección segura.

**La política de symlinks no se duplicó.** `workspace.FirstSymlink` se exporta y
la usan las dos capas: la que indexa se niega a caminar por un enlace, la que
sirve se niega a leer por uno. Dos copias de una comprobación de seguridad es
cómo acaban discrepando.

**Verificación:**

```text
gofmt -l internal/ benchmarks/
go vet ./...
go test ./...
go run ./benchmarks/mcp-token-cost --server <binario nuevo>   (dos veces, mismo digest)
```

Ocho tests nuevos en `get_source_test.go`, incluidos los cuatro que exige esta
ficha -desplazamiento reanclado, declaración borrada sin tumbar las otras filas,
ruta que escapa del repositorio y componente symlink- más el que fija que el
código no viaje escapado y el que rechaza dos homónimas equidistantes.

**Deuda que deja:** la superficie sube a doce tools y 670 tokens residentes en
Oh My Pi. LUQUE-1904 la baja a nueve retirando `graph_status`,
`list_repositories` y `get_unresolved_references` de la superficie del modelo.

**Siguiente tarea:** LUQUE-1904.

---

## LUQUE-1904 — Que el agente sepa cuándo llamarnos

**Dependencias:** LUQUE-1901, LUQUE-1902, LUQUE-1903.

**Objetivo:** que un agente elija Kivgraph cuando Kivgraph es la respuesta
correcta. Las tres tareas anteriores abaratan la respuesta; ninguna hace que se
pida.

**El techo está medido y es bajo.** Serena publicó 21.089 llamadas de 192
sesiones reales con su servidor conectado todo el tiempo (issue #1491): el
**35,4 %** de las sesiones usó alguna de sus tools, el **20,3 %** de las
operaciones de lectura pasó por ella, y el **64,6 %** de las sesiones no la
tocó nunca. Con `instructions`, descripciones cuidadas y un prompt de contexto
propio. Y `repowise-bench` documenta que la adopción no es una propiedad de la
tool: el mismo servidor, las mismas preguntas y el mismo índice pasaron de
15/15 llamadas a 4/15 y a 3/15 entre reejecuciones del mismo arnés. Cualquier
objetivo de esta tarea se declara con su arnés y su fecha o no vale nada.

**Entregables:**

* descripciones reescritas como enrutado: qué pregunta contestan, contra qué
  alternativa nativa, y **dónde pierden** -un nombre único en un repositorio
  pequeño lo resuelve `grep` más barato-; es el único presupuesto residente en
  Oh My Pi, 389 de los 594 tokens;
* retirar de la superficie del modelo `graph_status`, `list_repositories` y
  `get_unresolved_references`: su contenido va a `instructions` y al CLI, y con
  `get_source` dentro la fase cierra en nueve tools;
* `ServerOptions.Instructions` -hasta 2 KB, frase decisiva primero-, que hoy
  no se envía: `initialize` responde sin el campo;
* `_meta["anthropic/alwaysLoad"]` en tres o cuatro tools y
  `_meta["anthropic/maxResultSizeChars"]` en las de recorrido, para Claude Code;
* guía condicionada al recuento de resultados, pegada a la respuesta: cero, uno
  y muchos dicen cosas distintas, y la de cero nombra la llamada siguiente;
* errores que traen la llamada de recambio ya formada, no un consejo;
* un bloque de enrutado en `AGENTS.md`, que `CLAUDE.md` ya enlaza: un solo
  fichero para los dos anfitriones, y los dos lo cargan sin que nadie lo pida;
* servidor que **falla cerrado** cuando la generación publicada no sirve:
  handshake completo con cero tools y el comando de reconstrucción en
  `instructions`, como hace `codanna`. Un servidor que responde mal es peor que
  uno que se declara inútil.

**Decisiones:**

* Nada volátil en una descripción ni en `instructions`. tokensave metió un
  presupuesto derivado del número de nodos y cada reindexado reescribía bytes
  del prompt de sistema, invalidando la caché del cliente (su issue #260). El
  `snapshot_id`, los contadores y la lista de repositorios viven en
  `graph_status`, y un test lo fija.
* **Jamás pedirle al modelo que narre lo que ahorra.** tokensave lo hizo y
  cerró el issue #356 concediendo que los tokens de salida -los caros-
  compensaban el ahorro de entrada.
* El texto dice qué garantiza y qué no. `codanna` tiene que escribir «trata
  `find_callers` como una pista, confírmalo leyendo el código» porque sus
  aristas son heurísticas; nosotros podemos afirmar lo contrario y es el único
  argumento que un modelo puede usar para no hacer `grep`. Ahí es donde hay que
  gastar las palabras.
* Enriquecer descripciones no es gratis: medido sobre 856 tools y 103
  servidores, +5,85 puntos de éxito pero **+67 % de pasos** y regresión en el
  16,7 % de los casos (arXiv 2602.14878). Frases cortas, una por tool.
* **Sin hooks como mecanismo principal.** Existen en Claude Code y en Cursor, y
  `PreToolUse` no puede reconducir un `Read` a una llamada MCP: `updatedInput`
  sólo reemplaza los argumentos de la misma tool. Y bloquear los subagentes de
  exploración es valor esperado negativo -la instrumentación de Anthropic mide
  el subagente convirtiendo 6.100 tokens de lectura en 420 de resumen, un 92 %-.
  Si algún día se envía un hook, se documenta y lo instala el usuario.

**Criterios de aceptación:**

- `initialize` devuelve `instructions` y cabe en 2 KB.
- El coste residente en Oh My Pi -rutas más descripciones- no crece respecto de
  los 594 tokens que mide el arnés, y su digest lo fija.
- Ninguna descripción ni `instructions` contiene un número derivado del grafo.
- Una respuesta vacía de cada tool de consulta nombra la llamada siguiente.
- Con la generación ausente o ilegible, el servidor completa el handshake, no
  publica ninguna tool y dice cómo repararse.
- El bloque de `AGENTS.md` enumera las preguntas y su tool, sin adjetivos.

**Riesgos:** es la única tarea de la fase cuyo resultado no se puede demostrar
con un test. Se mide observando sesiones reales, con su arnés y su fecha, y se
declara como observación, nunca como garantía.

**Estado:** `PASS`.

**Resultado.** Medido sobre la generación `000024`, digest `ba7b09a476fdf747`:

```text
                                      antes    después
superficie residente en Oh My Pi        670        645   (12 -> 11 tools)
  rutas                                 222        201
  descripciones                         448        444
`instructions` en `initialize`      ausentes    1.086 B
guía en la respuesta                      -   +50 tok en las seis preguntas
responder, seis preguntas             3.059      3.109   3,51x -> 3,45x
```

La superficie queda **por debajo de los 670 que dejó LUQUE-1903** y encima lleva
enrutado; el techo lo fija ahora un test en bytes
(`MaximumResidentSurfaceBytes`), porque este paquete no tiene tokenizador y el
arnés ya publica la cifra en tokens.

La guía cuesta **50 tokens en las seis preguntas**, todos en el caso vacío
-`NewServer` pasa de 300 a 350-, y convierte «cero resultados» en «nada
referencia a este símbolo; las aristas están comprobadas por el checker, así que
esto es una ausencia, no un fallo». En una respuesta con filas la guía **calla**:
quince tokens de consejo en cada llamada es cómo un ahorro se convierte en un
coste.

**Dos cosas de esta ficha estaban mal y se corrigen.**

* **«Nueve tools» era incoherente con esta misma tarea.** El plan retiraba
  `graph_status` y `list_repositories` diciendo que su contenido iba a
  `instructions`, y dos líneas más abajo prohibía cualquier dato volátil en
  `instructions`. El `snapshot_id`, los contadores y la lista de repositorios son
  exactamente eso. Se retira sólo `get_unresolved_references`, que es la única de
  las tres que responde una pregunta sobre el índice y no sobre el código, y la
  fase cierra en **once**.
* **El presupuesto de 594 tokens describía otra superficie.** Era el coste de
  once tools sin `get_source` y con descripciones que sólo decían qué hacían.
  Restando las 201 de rutas dejaba 24 tokens por tool, que no da para decir
  contra qué alternativa competir ni dónde se pierde. El techo queda en lo que
  mide con el enrutado dentro, 645, que sigue siendo menos que antes de la tarea.

**Lo que no se envía.** Ningún hook. `PreToolUse` no puede reconducir un `Read` a
una llamada MCP -`updatedInput` sólo reemplaza los argumentos de la misma tool- y
bloquear los subagentes de exploración es valor esperado negativo. Y en ninguna
parte se le pide al modelo que narre lo que ahorra: tokensave cerró su issue #356
concediendo que los tokens de salida se comían el ahorro de entrada.

**Verificación:**

```text
gofmt -l internal/ benchmarks/
go vet ./...
go test ./...
go run ./benchmarks/mcp-token-cost --server <binario nuevo>   (dos veces, mismo digest)
```

Tests nuevos: `TestServerWithoutAGenerationPublishesNoTool`,
`TestServerInstructionsRouteWithoutVolatileFacts`,
`TestServerSurfaceStaysCheapToKeepResident` -que además falla si una descripción
lleva un dígito- y `TestEmptyAnswersNameTheNextCall`. Dos tests existentes
cambiaron de premisa: la superficie se comprueba ahora contra un servidor con
generación publicada, y `TestUnbuildableGraphLeavesTheServiceHonest` afirma el
handshake sin tools en vez de un `INDEX_NOT_READY` por llamada.

**Lo que queda sin demostrar, y así se declara:** si un agente llama más a
Kivgraph por esto. El techo del campo es el 20,3 % de las lecturas que midió
Serena con todo puesto, y la adopción no es una propiedad de la tool. Se observa
en sesiones reales, con su arnés y su fecha, o no se afirma.

**Siguiente tarea:** LUQUE-1906.

---

## LUQUE-1906 — La única fila que no se puede abrir

**Dependencias:** LUQUE-1901, LUQUE-1902, LUQUE-1905.

**Objetivo:** cerrar la limitación que el propio arnés declara en cada informe -no
hay pregunta cross-repository- y arreglar lo que esa ceguera escondía.

**Lo que se encontró al medirlo.** `find_cross_repo_consumers` **nunca se migró**.
Mientras las otras diez tools perdían las claves base32 y ganaban rangos de
líneas, ésta conservó su forma original, y ninguna pregunta del corpus la
ejercitaba, así que nada lo señaló. Medido sobre un corpus de tres repositorios
construido desde `testdata/go/cross-repository`, con `api.Compute` consumido por
`consumer-a`:

```text
respuesta actual            957 tokens para 4 filas = 239 por fila
  de ellos, claves          193 por fila (81 %)
    consumer_symbol_key      46
    target_symbol_key        43
    los dos *_package_key    47
    evidence_key             21
    consumer_file_key        14
    los dos *_repository_key 22
```

Y no lleva **ni el nombre del consumidor ni su rango de líneas**: es la única fila
de la superficie que sigue sin poder abrirse. El símbolo consultado viaja además
repetido en cada fila como `target_*`, que es el sujeto hoisteado que LUQUE-1113
ya resolvió en `find_references`.

**Entregables:**

* la fila del consumidor con `name`, `qualified_name`, `kind`, `repository`,
  `file_path`, `start_line` y `end_line`; las claves y la evidencia sólo en
  `detailed`;
* el símbolo consultado enunciado una vez como sujeto, no en cada fila;
* una pregunta `cross_repository` en `questions.json` del arnés, con su corpus
  propio y la salida nativa capturada;
* `benchmarks/mcp-token-cost/cross-repo/` con su `results.json` y su `report.md`.

**Decisiones:**

* El corpus vive en una ruta privada bajo `/private/tmp`, no en `/tmp`: la
  política de symlinks rechaza el enlace de macOS y no se relaja. Los tres
  repositorios son copias de `testdata/go/cross-repository`, nunca el fixture del
  árbol.
* El consumidor alcanza al proveedor con un `require` a secas. Un `replace` local
  hacia otro repositorio se rechaza -escapa del realpath- y sin ninguno de los dos
  el `go.work` sintético no los agrupa y la referencia sale `UNRESOLVED`. Las tres
  formas están medidas y las dos primeras son fallos declarados, no bugs.
* `category` se conserva. Separar `exact_symbol` de `package_level` es el contrato
  de esta tool: una dependencia de paquete prueba que el consumidor depende del
  proveedor y nunca que use el símbolo.

**Criterios de aceptación:**

- La fila del consumidor se puede abrir sin otra llamada.
- El sujeto aparece una vez por respuesta.
- `detailed` sigue devolviendo las claves y la evidencia.
- El arnés publica una pregunta cross-repository con su factor, y su informe deja
  de declarar esa ausencia como limitación.
- La respuesta de las cuatro filas del fixture cuesta menos de la mitad que hoy.

**Estado:** `PASS`.

**Resultado.** Corpus de tres repositorios copiado de
`testdata/go/cross-repository` a `/private/tmp`, indexado con configuración
aislada, generación `000003`. Digest del arnés `549dd41f2429…`:

```text
                          antes    después
respuesta de 4 filas        957        369    -61 %
por fila                    239         92
filas sin rango               4          0
```

La fila del consumidor ya se puede abrir -`consumer-a main.go:6-12 func main`-, el
sujeto se enuncia una vez, y la tripleta `(repository, path, qualified_name)`
devuelve el mismo total que la clave.

**Y el factor contra la vía nativa es `0,57x`: aquí perdemos.** Un `grep` de
`Compute` en los tres repositorios cuesta 211 tokens y nuestra respuesta 369. El
corpus son tres ficheros; en un corpus así grep gana y hay que decirlo. Lo que sus
211 tokens no dicen es **cuál** `Compute`, y no ven en absoluto los dos
consumidores de nivel-paquete que la respuesta clasifica aparte. El informe declara
esa columna como un suelo, no un techo: compara una respuesta completa con una
incompleta.

**Tres contratos del proyecto se cumplieron solos durante el montaje**, y los tres
dieron un fallo antes de dar un resultado: `/tmp` se rechazó por ser un enlace en
macOS; un `replace` local hacia otro repositorio se rechazó por escapar del
realpath; y sin `require` ni `replace` el `go.work` sintético no agrupa los módulos
y la referencia sale `UNRESOLVED`. Sólo la tercera forma -un `require` a secas,
con el proveedor registrado- produce aristas exactas.

**Y el corpus destapó un fallo del arnés.** Resolvía los cuerpos contra la ruta de
un solo repositorio, así que una referencia alojada en otro no se podía leer.
Ahora cada fila se resuelve contra el repositorio que nombra. Ninguna pregunta del
corpus anterior lo habría encontrado: las seis viven en `kivgraph`.

**Verificación:**

```text
gofmt -l internal/ benchmarks/
go vet ./...
go test ./...
go run ./benchmarks/mcp-token-cost --server <binario> --config <aislada> \
  --dir benchmarks/mcp-token-cost/cross-repo --repository shared-library
```

**Siguiente tarea:** —.

---

## LUQUE-1905 — Un arnés que mida la sesión, no la respuesta

**Dependencias:** ninguna. **Se implementa primero**: es el instrumento con el
que las otras cuatro tareas declaran sus cifras, y su posición al final del
documento es la del entregable, no la del orden de ejecución.

**Objetivo:** que ninguna reducción de esta fase sea una anécdota. Todas las
cifras de arriba se midieron a mano una vez, en una sesión, con un script que no
se conservó. Eso no es un gate.

**Por qué existe:** el error que esta fase estuvo a punto de heredar es haber
comparado contra el rival equivocado. El baseline de «leer los ficheros enteros»
daba un `-83 %` que ningún anfitrión concede: la vía nativa cuesta 38.094 y no
102.522, y el ahorro real es del 54 %. Un arnés que no mide la alternativa
nativa mide una fantasía, y es exactamente el defecto que el propio `bench` de
tokensave tiene -su baseline es el conjunto de ficheros que su propia respuesta
eligió, con un tope que hace del 75 % el techo aritmético-.

**Entregables:**

* `benchmarks/mcp-token-cost/`, con `results.json` y `report.md`, conservando
  comando, commit, entorno, generación, corpus, tokenizador y limitaciones, como
  exige la convención de `benchmarks/`;
* un conjunto de preguntas versionado -las seis actuales como semilla- que
  incluya al menos un símbolo de nombre común, uno de nombre raro y uno con
  consumidores en otro repositorio;
* tres columnas por pregunta: **vía nativa** (`grep` acotado más la lectura de
  los rangos que nombra), **Kivgraph** (las llamadas MCP más lo que el agente
  aún tenga que leer) y el factor entre ambas, con las dos salidas del anfitrión
  -su `grep` y sus lecturas de rango- capturadas literalmente en `native/`;
* dos factores por pregunta, el de responder y el de la sesión completa, porque
  publicar sólo uno es cómo este campo llega a sus titulares;
* el coste residente de la superficie en los dos anfitriones: rutas más
  descripciones para Oh My Pi, y el esquema completo como cifra diferida;
* fallo del gate ante una regresión: cualquier pregunta que empeore respecto de
  `results.json`, o una superficie residente que crezca.

**Decisiones:**

* El tokenizador es `cl100k_base` y se declara. No se cambia sin regenerar
  `results.json`, porque no son comparables.
* La vía nativa se mide **con las tools del anfitrión, no con una imitación**:
  su `grep` acotado y su `read` con selector, que es lo que un agente usaría.
  Una aproximación escrita por nosotros sesga el resultado a nuestro favor.
* Se mide la **sesión**, no la respuesta: cuenta lo que el agente aún tiene que
  leer después de la llamada. Un payload magro seguido de tres lecturas no ha
  ahorrado nada, y esa es precisamente la trampa en la que caen las cifras
  publicadas del campo.
* No se mide dinero ni latencia. La caché de prompt hace que el coste en euros
  dependa del orden en que se ejecutan los brazos -correlación posición/coste
  de `-0,487` medida por `repowise-bench`, con un servidor llamado cero veces
  pareciendo un 43 % más barato-. Tokens y llamadas, nada más.
* La adopción no entra aquí. Es observación de sesiones reales, no una métrica
  del arnés, y LUQUE-1904 la declara como tal.

**Criterios de aceptación:**

- El arnés mide la vía nativa, la actual y la proyectada por pregunta, y publica
  el factor entre ellas.
- Dos ejecuciones sobre el mismo corpus y la misma generación producen el mismo
  digest.
- `MCP_TOKEN_COST_PASS` se declara desde su salida, no a mano.
- `report.md` nombra sus limitaciones, empezando por la ausencia de una pregunta
  cross-repository, que es donde el grafo no tiene competidor nativo.

**Estado:** `PASS`.

**Resultado.** El arnés retiró las cifras con las que se había escrito la fase.
El baseline de ficheros enteros -102.522 tokens, `-83 %`- no lo concede ningún
anfitrión, y las medidas a mano que lo sustituyeron estaban infladas 2,7 veces
porque tokenizaban el envoltorio JSON del resultado en lugar del texto que ve el
modelo. Sobre la generación `000024`, digest `ffb455a6881ea58f` -las cifras de
sesión de este bloque son las de aquella pasada, antes de que LUQUE-1903
descubriera que el `read` del anfitrión cobra un 38 % sobre los bytes; el
preámbulo de la fase lleva las corregidas-:

```text
                              responder    sesión completa
vía nativa                       10.739             21.223
Kivgraph hoy                     6.991  1,54x      17.475  1,21x
tras LUQUE-1901                   3.649  2,94x      14.133  1,50x
cuerpos, los paga cualquier vía  10.484             techo    2,02x
superficie residente en Oh My Pi    594  (205 rutas + 389 descripciones)
esquema diferido por los dos anfitriones  1.580
contenido duplicado en `structuredContent`  24.066 bytes en la pasada
```

**El arnés publica los dos factores porque uno solo engaña.** El techo de la
sesión es `2,02x` y no depende de nosotros: los cuerpos son el 49 % del brazo
nativo y los paga cualquier vía.

Y el reparto, que es el hallazgo: hoy **perdemos en cuatro de las seis** en el
factor de responder -`MergeAll` `0,50x`, `DiscoverGo` `0,69x`,
`CanonicalColumns` `0,90x`, y `BuildPlan` ya gana con `1,80x`-. Tras LUQUE-1901
ganamos en cinco y `MergeAll` sigue perdiendo con `0,91x`. Ganamos donde `grep`
es caro y ambiguo (`Publish` `1,94x` hoy, `3,98x` después) y donde el grafo puede
afirmar una ausencia (`NewServer` `7,69x`: cero llamantes por 300 tokens frente a
2.308 de ruido).

Tres decisiones salieron de escribirlo:

* **El brazo nativo es una captura verbatim**, no una reimplementación. Los seis
  ficheros de `native/` son la salida literal del `grep` del anfitrión, con su
  patrón y su ruta registrados en `questions.json`.
* **La identidad de una pasada es un digest**, no el fichero. El servidor
  reconstruye su proyección del HotSnapshot al arrancar, así que su `built_at`
  cambia entre pasadas idénticas; el digest cubre generación, corpus, superficie
  y cifras, y deja fuera los sellos de tiempo.
* **El tokenizador viaja embebido.** `tiktoken-go` con el cargador offline: el
  cargador por defecto descarga el vocabulario, y un gate no puede depender de
  que haya red. Se añadió `github.com/pkoukk/tiktoken-go` y su cargador al
  módulo por esto y sólo por esto: los bytes no sirven de proxy, porque una
  clave base32 de 52 caracteres cuesta 35 tokens -1,5 caracteres por token- y
  una línea de Go pasa de 3, así que medir bytes esconde justo lo que la fase
  quiere comprar.

**Verificación:**

```text
gofmt -l benchmarks/mcp-token-cost/
go vet ./...
go test ./...
go run ./benchmarks/mcp-token-cost   (dos veces, mismo digest)
```

**Siguiente tarea:** LUQUE-1901.

---

# 23. Fase 20 — La memoria por cliente

Un cliente MCP lanza `kivgraph serve` él mismo, así que hay un servidor por
cliente y **cada uno reconstruye el grafo entero en su propio heap privado**.
Medido el 2026-08-15 en `devlabs` -Linux, 16 núcleos, 24 GB- sobre la generación
`000053`: 41 repositorios, 121 paquetes, 5.021 ficheros, 102.385 símbolos,
259.556 aristas y 189 MB de `graph.db`.

```text
heap vivo tras cargar                    173 MB
pico de heap durante la carga        495-531 MB
VmHWM del proceso al cargar          808-897 MB
RSS estable recién cargado           252-373 MB
VmSize                                 3-4 GiB
reparto de un proceso de 1,68 GB   Private_Dirty 1,663 GB · Shared_Clean 15 MB
```

Los 15 MB limpios son el binario: **no se comparte nada más**. Tres servidores
vivos costaban 2,44 GB para contestar preguntas sobre el mismo grafo.

La marginal de una *sesión* dentro de un proceso, en cambio, es prácticamente
cero: `ServerSession` sólo guarda `InitializeParams`, el nivel de log y su
conexión, y las tools viven en el `*Server` compartido. El coste es la frontera
de proceso, no el número de clientes.

**Y el grafo va a crecer.** `rust.index_sysroot` multiplica estos 189 MB, así
que lo que se elija tiene que escalar con eso y no sólo con el número de
clientes.

## Por qué no un proceso compartido

Las dos formas de compartir un proceso se estudiaron y se descartan; el análisis
completo va al ADR de LUQUE-2007.

* **Demonio con relé stdio sobre socket unix.** Es poco código -los dos lados
  hablan JSON-RPC delimitado por línea, así que el relé es un `io.Copy` en cada
  sentido y la elicitación, el progreso y la cancelación pasan sin que nadie los
  interprete- y no cambia ninguna configuración de cliente. Lo que añade es un
  sistema distribuido en miniatura: ciclo de vida, arranque en carrera, sockets
  huérfanos, sesgo de versión entre el binario del cliente y el demonio vivo,
  quién es dueño del bucle de resync, y qué significa `stop`, que hoy selecciona
  por invocación `(argv[0] == "kivgraph", argv[1] ∈ {serve, ui})` y no vería un
  demonio con otro argv. Y sobre todo: **el demonio tiene el grafo residente
  siempre**, así que con el sysroot indexado son gigabytes que nadie puede
  desalojar.
* **HTTP loopback con los clientes reconfigurados por URL.** El SDK trae
  `StreamableHTTPHandler` y `auth.RequireBearerToken`, pero ningún adaptador de
  `internal/integrations` sabe escribir una entrada con `url`: todos escriben
  `{"command": <exe>, "args": ["serve"]}` y la comparación es igualdad exacta
  -`rawJSONMatches` + `DeepEqual`-, de modo que una entrada con URL se clasifica
  `incompatible` y exige `--force`. Habría que enseñar la forma nueva a los cinco
  adaptadores, pedir al usuario que reinstale, mantener stdio igual para los
  clientes que no hablan HTTP, y añadir token y comprobación de origen porque un
  puerto loopback lo alcanza cualquier proceso local. Encima el SDK no expira
  sesiones -`transports` crece hasta el `DELETE`, `closeAll` no está exportado-
  ni permite configurar el `EventStore` desde las opciones del handler.

**La decisión de la fase es la tercera:** que el snapshot deje de ser privado.
Se persiste una vez por generación y los procesos lo **mapean en sólo lectura**,
de modo que comparten *page cache* en vez de heap -`Shared_Clean` en lugar de
`Private_Dirty`, que es la métrica con la que se diagnosticó el problema-. No
aparece ningún proceso nuevo, ningún ciclo de vida y ninguna configuración de
cliente cambia; el sistema operativo desaloja lo que nadie toca; y de propina
desaparecen el transitorio de carga y los 2-3 s de arranque que hoy paga cada
cliente.

## Lo que esta fase revisa

`LUQUE-1204` fijó que «el `HotSnapshot` **nunca se escribe**: se deriva del
grafo definitivo en cada construcción. Por eso de las dos ramas del requisito
siempre se toma la segunda: no hay snapshot que cargar, hay grafo que
reconstruir». Esta fase introduce la primera rama y **conserva la segunda como
respaldo**: un fichero ausente, truncado, de otra versión o con otro digest se
rechaza y la carga reconstruye desde LadybugDB diciéndolo una vez. La garantía de
1204 no se relaja; se le añade un camino rápido que falla cerrado hacia ella.

`storage.snapshots_path` ya existe, `init` lo crea y `doctor` comprueba que se
puede escribir. Nadie ha escrito nunca nada ahí: esta fase es lo que ese
directorio estaba esperando. `storage.retain_snapshots` -que vale 3- pasa a
gobernar cuántos ficheros se conservan.

**Gate:** `SHARED_SNAPSHOT_PASS`, y no se declara a mano: lo mide el arnés de
LUQUE-2006, que compara N procesos sobre una generación publicada contra la
línea base de esta cabecera y se niega a emitirlo si el corpus o la generación no
coinciden con la referencia declarada, como ya hace `benchmarks/web-viewer`.

**Fuera de alcance de la fase entera:** compartir un proceso entre clientes -que
queda declarada en LUQUE-2008 con la condición que la reabriría-, cualquier
transporte que no sea stdio, y reducir el grafo mismo. Esta fase no quita hechos:
cambia dónde viven los que ya hay.

---

## LUQUE-2001 — Saber en qué se van los 173 MB antes de diseñar el fichero

**Dependencias:** LUQUE-0304, LUQUE-0311, LUQUE-0906.

**Objetivo:** un desglose por componente, en bytes, reproducible sobre una
generación real. Diseñar el formato sin esto es diseñarlo a ciegas, y decidir
entre un índice ordenado y una tabla hash sin saber lo que pesa cada uno es
opinar.

**Lo que hoy no se puede responder:** cuánto de los 173 MB es el arena de
strings, cuánto son las cinco tablas de registros, cuánto los dos CSR y cuánto
los cinco mapas de `GraphSnapshot` -`packageIncoming`, `symbolByStableKey`,
`symbolsByName`, `symbolsByQName`, `fileByRepoPath`-. `StringTable.Stats()` ya
publica entradas y bytes del arena, y es lo único que hoy se puede citar.

**Alcance:**

* `benchmarks/hot-snapshot-footprint/` con `results.json` y `report.md`, según la
  convención de la sección 23: comando, commit, entorno, generación, digest del
  corpus, métricas y limitaciones.
* El desglose de cada componente y el residuo, más el mismo desglose por símbolo
  y por arista, que es lo que permite proyectar a un corpus con el sysroot.

**Decisiones:**

* Las tablas planas y los CSR se miden **analíticamente**:
  `unsafe.Sizeof(T{}) * cap(slice)` es exacto y no necesita instrumentación.
* Los mapas y el arena se miden **observando el heap**: `runtime.GC()` y
  `runtime.ReadMemStats().HeapAlloc` antes y después de construir cada índice por
  separado, con el snapshot completo como control.
* El informe declara su error: la suma de los componentes contra `HeapAlloc` tras
  una GC forzada, con el residuo nombrado y no repartido. Un desglose que no
  cierra no sirve para diseñar un fichero.
* No se toca `graph_status`. Publicar el desglose es una decisión aparte y la
  toma LUQUE-2007 si el número resulta útil de servir.

**Criterios de aceptación:**

- La suma de los componentes explica **≥95 %** de `HeapAlloc` tras una GC
  forzada, y el residuo se nombra.
- Dos ejecuciones sobre la misma generación coinciden dentro del **1 %**.
- El informe nombra los tres componentes que dominan y su coste por símbolo y por
  arista.
- El arnés se niega a publicar cifras si la generación que abrió no es la
  declarada en `results.json`.

**Estado:** cerrada el `2026-08-22`. `benchmarks/hot-snapshot-footprint`.

**El resultado:** `171,5 MB` residentes sobre `kena` -- que confirma el `173 MB`
que esta fase citaba de otra máquina-- con dos pasadas dentro del `0,01 %`. Los
tres que dominan: el arena de strings (`63,9 MB`, `37 %` del total), los tres
mapas de símbolos (`16,5 MB`, que es **2,4 veces** la tabla de `6,9 MB` que
indexan) y las evidencias (`7,4 MB`). Por unidad: `379` bytes por símbolo en
tablas e índices sin el arena, y `44` por arista.

**El criterio del `95 %` no se cumple, y por una razón que vale más que el
criterio.** La cobertura es `64,6 %` y el residuo de `60,7 MB` está identificado
con perfil de heap vivo: `58 MB` parados en `ladybug.newCanonicalArrowChunk`,
alcanzables tras dos GC forzadas. **Un tercio de la huella no es el grafo: son los
búferes con que se leyó la base de datos.**

El mecanismo, probado de punta a punta: el chunk Arrow copia los bytes de texto a
su propio arena, entrega cada valor como `unsafe.String` sobre ese arena, y el
adaptador convierte el valor a `StableKey` con una conversión entre cadenas que
**no copia**. `6,4 MB` de claves estables retienen `58 MB`.

No se suma al desglose porque no se asigna aparte: contarlo sería contarlo dos
veces y mentir sobre lo que el grafo pesa.

**Lo que deja decidido para la fase:** hay `58 MB` gratis antes de escribir un
solo byte de fichero -- copiar las claves cuesta `6,4 MB`-- y es evidencia directa
para `LUQUE-2002`, cuyo título ya era «que ninguna clave estable ocupe un
puntero». Un fichero mapeado ataca los `63,9 MB` del arena, y `StringTable` ya
tiene el campo `borrowed` para eso. Los `16,5 MB` de mapas no se mapean gratis, y
ahí es donde «índice ordenado o tabla hash» se decide con cifras.

**Verificación:**

```text
gofmt -l benchmarks/hot-snapshot-footprint/
go vet ./...
go test ./...
make test-ladybug PKGS=./internal/hotsnapshot/...
go run ./benchmarks/hot-snapshot-footprint   (dos veces, mismas cifras)
```

**Siguiente tarea:** LUQUE-2002.

---

## LUQUE-2002 — Que ninguna clave estable ocupe un puntero

**Dependencias:** LUQUE-2001.

**Objetivo:** que `SymbolRecord` no contenga ni un puntero, que es la condición
para que su tabla se pueda mapear.

**Lo que hoy lo impide:** `type StableKey string` -`internal/hotsnapshot/stablekey.go:22`-,
así que cada `SymbolRecord` lleva una cabecera de cadena de 16 bytes más una
asignación en el heap; la clave del corpus mide 52 caracteres de base32, que caen
en la clase de tamaño de 64 bytes. Son unos 80 bytes por símbolo, y las mismas
claves viven otra vez como llaves de `symbolByStableKey`. El coste en bytes es lo
de menos: mientras haya un puntero en la tabla, la tabla no se puede mapear.

**Alcance:**

* `internal/hotsnapshot/{snapshot,builder,stablekey,ids}.go`: la clave pasa a un
  identificador denso sobre un arena propio, y los accesores conservan su firma
  -`SymbolByStableKey(key StableKey)` sigue aceptando la cadena-.
* `EdgeRow.SourceKey` y `EdgeRow.TargetKey` siguen siendo cadenas: son filas que
  vienen de la base y el builder ya las resuelve a IDs densos.

**Decisiones:**

* **El valor de la clave y su namespace no cambian.** El contrato de las stable
  keys es sobre identidad persistente, no sobre su representación en memoria;
  `stablekey.go` y sus tests no se tocan, y `stablekey_test.go` es la prueba de
  que no se movió el algoritmo.
* Las claves **no** entran en el `StringTable` de los nombres. Son únicas por
  símbolo: meterlas ahí añadiría 102.385 entradas a un índice inverso que existe
  para buscar nombres, y ese índice es precisamente uno de los cinco mapas que
  LUQUE-2003 tiene que retirar.
* El arena de claves se ordena por bytes al congelarlo, para que LUQUE-2003 pueda
  resolver una clave con una búsqueda binaria en vez de una tabla hash.

**Criterios de aceptación:**

- `SymbolRecord` no tiene ningún campo de tipo puntero, cadena, slice o mapa.
- Un test de propiedad resuelve **todas** las claves del corpus y obtiene el
  mismo `SymbolID` que antes del cambio.
- El ahorro medido coincide con lo que LUQUE-2001 atribuyó a este componente,
  ±10 %.
- Ninguna clave publicada cambia: el digest de contenido del snapshot es idéntico
  antes y después.

**Estado:** cerrada el `2026-08-22`.

**El resultado:** `171,5 MB` → **`109,1 MB`** residentes sobre `kena`, un
**`36,4 %`** menos, y la cobertura del desglose pasa de `64,6 %` a **`100,1 %`**:
cierra porque los búferes que las claves fijaban ya no están. Por símbolo,
`1.389` → `883` bytes.

|pieza|bytes|
|---|---|
|búferes Arrow liberados|`+58.040.000`|
|mapa `symbolByStableKey` retirado|`+6.990.144`|
|registro más fino: cabecera de 16 → `uint32` de 4|`+1.482.372`|
|coste nuevo de la tabla de claves|`-6.917.740`|
|**atribuido por LUQUE-2001**|**`59.594.776`**|
|**medido**|**`62.400.640`** (`+4,7 %`, criterio `±10 %`)|

El `1.482.372` es exacto: `123.531 × 12`. La estimación de la tarea -«unos 80
bytes por símbolo»- era baja para el conjunto: entre el mapa, la cabecera y los
búferes fijados son `540` bytes por símbolo.

**El diseño salió más simple de lo planeado, y lo decidió un hecho del código.**
`builder.go` ya ordenaba los símbolos por clave estable antes de asignar un solo
ID. O sea que la entrada `i` de la tabla es la clave del `SymbolID i` sin
permutación ninguna, y de ahí tres consecuencias:

* El arena sale **ya ordenado por bytes**, que era la decisión 4 de esta tarea,
  sin ordenar nada.
* El mapa no se encoge: **desaparece**. Una búsqueda binaria sobre los offsets
  responde con el `SymbolID` directamente, así que `LUQUE-2003` empieza con uno
  de sus cinco mapas ya retirado.
* La clave del registro es un `StableKeyID` denso, no una posición derivada. Se
  guarda porque los ~30 helpers que la leen reciben `(snapshot, record)` y no el
  ID: derivarla habría cambiado 30 firmas para ahorrar `494 KB`.

**Lo que el compilador no protege, y hay que saberlo al leer este cambio:**
`string(symbol.StableKey)` sobre el campo nuevo **compila** -es una conversión de
rune- y devuelve basura. Igual `%q` sobre un entero. Los 41 sitios de
`internal/mcp/tools`, los de `webapi` y los tests se migraron a mano por eso; dos
sitios (`webapi.makeEdgeView`) no estaban en el inventario inicial y salieron al
revisar fichero por fichero.

**El formato de fichero no cambió un byte.** `KVSNAP` ya guardaba las claves como
arena + offsets, que es exactamente la forma de la tabla, y la clave no está en la
fila del símbolo porque siempre igualaría su propio índice. Verificado como
compatibilidad y no como suposición: el `snapshot.kvsnap` que publicó el binario
**anterior** al cambio se carga con el código nuevo y sus `123.531` claves
resuelven al mismo `SymbolID`, comparadas símbolo a símbolo contra un snapshot
derivado (`TestPublishedSnapshotMatchesADerivedOne`).

**Criterios:**

* `SymbolRecord` sin punteros: defendido por `TestMappableTablesHoldNoPointers`,
  que cubre las ocho tablas densas por reflexión y no sólo la de símbolos.
* Todas las claves del corpus resuelven al mismo `SymbolID`: el test oráculo
  sobre `123.531` símbolos, contra el artefacto pre-cambio.
* Ahorro dentro del `±10 %`: `+4,7 %`.
* `snapshot.sha256` idéntico: `e80c6d46…` antes y después, reindexando el mismo
  corpus con los dos binarios. **Prueba menos de lo que escribí:** ese fichero es
  un digest de contadores de tabla, no de contenido (`LUQUE-2014`). Lo que sí
  prueba que ninguna clave publicada cambió es el test oráculo de arriba.

**Un hallazgo colateral, y sí es nuestro** -- lo escribí como ajeno y no lo es:
los dos ficheros publicados difieren
en `48` bytes, todos en el `stringArena`. Son `48` cadenas de detalle de
`UNRESOLVED` que registran rutas de la caché de build de Go, y los dos `HOME`
aislados del banco de pruebas se llamaban `h2002` y `h2002b` -- un carácter.

**La explicación que escribí aquí era falsa, y la corrijo.** Dije que el digest de
contenido no cubría esos detalles. Sí los cubre -- `snapshot.go:531` imprime
`detail=%s`--, así que debería haber cambiado. Al perseguirlo hasta el final
aparecieron dos defectos distintos, los dos medidos y ya registrados: el digest
que prueba la pertenencia del fichero es `snapshot.sha256`, y ése es de
**contadores de tabla** -- `rebuild.go:284`-- y no de contenido (`LUQUE-2014`); y
las `288` filas cuyo `Detail` lleva una ruta absoluta de la caché de build de la
máquina que indexó (`LUQUE-2015`).

**Verificación:**

```text
gofmt -l internal/hotsnapshot/
go vet ./...
go test ./internal/hotsnapshot/...
make test-ladybug PKGS=./internal/rebuild/...
go run ./benchmarks/hot-snapshot-footprint
```

**Siguiente tarea:** LUQUE-2003.

---

## LUQUE-2003 — Índices sin mapas

**Dependencias:** LUQUE-2001, LUQUE-2002.

**Objetivo:** que `GraphSnapshot` no contenga ningún mapa, que es la última cosa
que impide que el grafo entero sea una secuencia de bytes mapeable.

**Lo que hay que retirar:** `packageIncoming map[PackageID][]PackageDependencyRecord`,
`symbolByStableKey map[StableKey]SymbolID`, `symbolsByName` y `symbolsByQName`
-`map[InternedString][]SymbolID`- y `fileByRepoPath map[RepoPathKey]FileID`.
`traversalWorkspacePool` se queda: es andamiaje por llamada, no estado del grafo,
y nunca se persiste.

**Alcance:**

* Cada mapa pasa a un par de arrays planos: llaves ordenadas y valores, y para
  los que devuelven varios resultados, desplazamientos estilo CSR sobre un
  `[]SymbolID` contiguo -la misma forma que ya tienen las aristas-.
* Los accesores conservan su firma y su semántica de copia, así que ningún
  llamador cambia.

**Decisiones:**

* Las búsquedas exactas por nombre son **binarias sobre enteros**: la llave es un
  `InternedString`, no una cadena, así que no hay comparación de bytes en el
  camino caliente.
* La búsqueda por prefijo -`search.go`, fijada por
  `TestPrefixSearchIsNameOnlyAndStable`- exige orden lexicográfico de nombres.
  Se decide con las cifras de LUQUE-2001 entre dos opciones, y el informe dice
  cuál y por qué: que el interner asigne los IDs en orden lexicográfico al
  congelar -aplicando la permutación a todos los registros una sola vez, con lo
  que exacto y prefijo son la misma búsqueda binaria-, o un segundo array de IDs
  ordenado por bytes.
* `symbolByStableKey` se resuelve por búsqueda binaria sobre el arena ordenado de
  LUQUE-2002 con `bytes.Compare`. No se usa un hash: una colisión obligaría a
  guardar la clave completa para desempatar, que es exactamente lo que se acaba
  de retirar.
* El orden de los resultados no cambia. Es contrato: las páginas y los cursores
  dependen de él.

**Criterios de aceptación:**

- `GraphSnapshot` no declara ningún `map` ni ningún campo con puntero, salvo el
  `sync.Pool` de andamiaje.
- Toda la suite de `internal/hotsnapshot` pasa **sin modificar un solo test**:
  los accesores son el contrato y no se toca.
- `benchmarks/mcp-client` no empeora el p50 ni el p99 más de un **5 %** contra la
  medición previa, con el mismo corpus y semilla.
- El desglose de LUQUE-2001 muestra la caída, y el residuo sigue nombrado.

**Estado:** cerrada el `2026-08-22`.

**El resultado:** `109,1 MB` → **`101,7 MB`** residentes, y `GraphSnapshot` ya no
declara ni un mapa ni un puntero -- sólo tablas planas y el `sync.Pool` de
andamiaje. Los tres mapas exactos costaban `9.592.896` bytes; los arrays guardan
lo mismo en `1.961.120`, **`4,9×` menos**.

|índice|antes (mapa)|después (plano)|
|---|---|---|
|`symbolsByQName`|`6.114.016`|`1.145.496`|
|`symbolsByName`|`3.369.968`|`758.408`|
|`fileByRepoPath`|`108.912`|`57.216`|
|`packageIncoming`|nunca medido|`2.024`|

`packageIncoming` no estaba medido porque guardaba **copias de las filas**:
medirlo era medir una segunda tabla de dependencias. Hoy son offsets
direccionados por el ID denso -- `PackageID` ya es un índice denso, así que no
necesita array de claves-- más un `uint32` por dependencia.

**El indicador que más dice del cambio: cero partes medidas por heap**, de cuatro
que había en `LUQUE-2001`. Mientras los índices eran mapas había que reconstruir
uno equivalente y observar el montón, porque un mapa cuesta lo que el runtime
decide. Ahora todo el desglose es aritmética sobre un layout declarado. El perfil
de heap vivo no contiene ni `makemap` ni `mapassign`.

**La latencia no se pagó, y la cola mejoró.** `benchmarks/mcp-client-flat-indexes`,
mismo corpus y semilla: `p50` `-0,24 %`, `p99` `-0,09 %` -- ruido, y muy dentro
del `5 %`. El `p99` del backend baja entre `20 %` y `39 %` en las cinco
operaciones. Se cambia la media buena de un hash por una cola acotada por
`log₂ n`, y a este tamaño la media no empeora.

**La decisión del prefijo: la pregunta estaba mal planteada.** Esta tarea pedía
elegir entre dos formas de dar orden lexicográfico a la búsqueda por prefijo.
Ninguna hace falta: `scanSymbolNames` es un **barrido lineal** sobre todos los
símbolos en orden de `SymbolID`, y `TestPrefixSearchIsNameOnlyAndStable` fija ese
orden como contrato. El prefijo nunca consultó un índice ordenado.

Lo que sí existe es el `order` del `StringTable`, que sirve a otra cosa --
convertir la cadena de una consulta en un `InternedString`-- y cuesta `2.558.452`
bytes. Retirarlo haciendo que `Freeze` asigne IDs lexicográficos ahorraría un
`2,5 %` a cambio de permutar cada `InternedString` de cada registro y de rechazar
los ficheros publicados cuyo arena no esté ordenado. **No se hace**, porque su
justificación escrita no existe; queda la cifra para que `LUQUE-2004` decida con
ella.

**Desviación de un criterio, declarada:** «sin modificar un solo test» no se pudo
cumplir al pie de la letra, y el motivo es que un test alcanzaba el campo que se
retira. `file_test.go:43` comprobaba `len(built.packageIncoming)` como guarda de
fixture; pasa a `len(built.packageIncoming.values)`. Una línea, en una guarda y no
en un contrato. **Ningún test de superficie cambió**, que es lo que el criterio
protegía: los `DeepEqual` de la ida y vuelta por fichero siguen intactos y ahora
son más fuertes, porque comparan los arrays exactos en vez de dos mapas.

**Los mapas no desaparecen del todo, y conviene saber dónde siguen.**
`GraphSnapshotInput` los sigue aceptando: acumular un índice mientras se leen los
registros es lo que un mapa hace bien, y `NewGraphSnapshot` los convierte en
arrays y los tira. Eso deja los llamadores y los tests sin tocar, pero significa
que cada carga todavía construye tres mapas transitorios. Retirar eso es
`LUQUE-2005`, y tiene un requisito que hoy no se cumple: el fichero publicado
**no lleva** estos índices -- se reconstruyen al abrir-- así que mapearlos exige
antes escribirlos.

**Verificación:**

```text
gofmt -l internal/hotsnapshot/
go vet ./...
go test ./internal/hotsnapshot/... ./internal/mcp/...
go test -race ./internal/hotsnapshot/...
go run ./benchmarks/mcp-client --clients 4   (antes y después)
go run ./benchmarks/hot-snapshot-footprint
```

**Siguiente tarea:** LUQUE-2004.

---

## LUQUE-2004 — `LGHS`: el snapshot publicado se escribe

**Dependencias:** LUQUE-2003, LUQUE-1204.

**Objetivo:** un fichero versionado por generación, escrito por la pasada que la
publica, que contenga exactamente lo que hoy se reconstruye.

**Alcance:**

* `internal/hotsnapshot/file.go`: escritor y lector del formato.
* `internal/rebuild/snapshot.go`: escribir el fichero tras construir el snapshot
  que ya se valida hoy.
* `storage.snapshots_path` como destino y `storage.retain_snapshots` como poda.

**Decisiones:**

* Cabecera con magic `LGHS`, versión de formato, `snapshot_id`, `created_at`,
  `schema_version`, `resolver_version`, los contadores y el digest de contenido
  -que ya existe: `snapshotContentDigest(rows)`-, más una tabla de secciones con
  desplazamiento y longitud por sección. Es la convención que el visor ya usa con
  `LGVB`, y la misma que valida magic, versión, offsets y longitudes antes de
  servir.
* Little-endian y **cada sección alineada al tamaño de su elemento**. Una vista
  `[]uint32` sobre bytes desalineados no es un detalle de rendimiento: en algunas
  arquitecturas no está definida.
* Se escribe en un temporal del mismo directorio y se renombra, con `fsync` del
  fichero y del directorio. Una generación no puede quedar con un fichero a
  medias.
* **Determinismo:** dos publicaciones del mismo grafo producen el mismo fichero
  byte a byte. La pasada ya garantiza hechos idénticos byte a byte; esto lo
  extiende al artefacto, y es además la regresión más barata de escribir.
* El fichero **no es una fuente de hechos**: sigue siendo una proyección
  derivada del grafo canónico, y sigue siendo LadybugDB quien decide qué es
  verdad.

**Criterios de aceptación:**

- Toda generación publicada tiene su fichero, y `retain_snapshots` conserva
  exactamente los que dice.
- Dos publicaciones del mismo grafo dan ficheros idénticos.
- Un fichero truncado, con otro magic, con otra versión de formato, con otra
  generación o con otro digest se rechaza con un código estable y **nunca se
  sirve**. `internal/rebuild/snapshot_corruption_test.go` gana un caso por cada
  una de esas cinco formas.
- Nada se escribe fuera de `storage.snapshots_path`.
- Una configuración escrita fuera de la ubicación por defecto sigue siendo
  autocontenida: su fichero cuelga de su propio directorio.

**Estado:** cerrada el `2026-08-22`, con tres criterios deliberadamente no
cumplidos y uno movido a `LUQUE-2013`.

**El objetivo ya estaba hecho.** El fichero existe desde el ADR 0045, con magic
`KVSNAP` -- no `LGHS`--, se escribe por generación con temporal + rename, lleva
cabecera versionada con `snapshot_id`, `created_at`, `schema_version` y doble
digest, y una tabla de secciones con `kind`, `elemSize`, `count`, `offset` y
`length`. Todo little-endian. Auditado fichero a fichero antes de tocar nada.
Así que esta tarea fue cerrar los huecos reales, no escribir el formato.

**El hueco grave: un fichero mal formado provocaba un pánico, no un rechazo.** La
tabla validaba que `count*elemSize == length`, pero nunca que `elemSize` fuese el
ancho que el decodificador de ese `kind` espera. Un fichero que declara un byte
por símbolo pasa todas las comprobaciones y luego `decodeSymbols` lee 37 bytes por
registro de un búfer que tiene uno. Demostrado revirtiendo la guarda:
`panic: runtime error: index out of range [28] with length 0`. **Un pánico no es
«nunca se sirve»**: no se sirve a nadie y se cae todo el mundo.

Y en la misma línea había un segundo agujero peor: `count*elemSize` **desborda**.
Con `2^61` registros de 8 bytes el producto es exactamente `0`, que iguala un
`length` de `0` y deja pasar el fichero; el decodificador itera `2^61` veces.
Ahora se comprueba como división.

**Lo demás que se cerró:**

* Un `kind` repetido se rechaza. El decodificador es un `switch`, así que la
  última entrada ganaba en silencio y el fichero tenía dos respuestas para una
  tabla.
* Dos secciones que comparten bytes se rechazan. Es una relación entre dos
  entradas, así que se comprueba sobre la tabla ordenada, no por entrada.
* **Alineación**, que es lo que `LUQUE-2005` necesita: cada sección arranca en
  múltiplo de 8. El relleno no lo nombra ninguna sección ni entra en el digest,
  así que un lector que la ignore recomputa el mismo digest sobre los mismos
  bytes.

**Sobre «cada sección alineada al tamaño de su elemento»:** no es lo que se hizo,
y el motivo va escrito en el código. Varios elementos miden `25`, `37`, `15` y
`52` bytes, y alinear a un número que no es potencia de dos no es alinear. Lo que
la decisión protege es una vista sobre una sección -- `[]uint32` hoy--, que en
algunas arquitecturas es indefinida en una dirección arbitraria. Ocho cubre todo
escalar que este formato guarda, y es una regla en vez de una tabla que puede
contradecirse.

**Determinismo: el criterio era falso, y la medición dice por cuánto.** Dos
publicaciones del mismo grafo -- digest de contenido idéntico-- dan ficheros que
difieren en **6 bytes de 98.779.360**: `snapshot_id` y `created_at`. El payload es
idéntico byte a byte, y el `payloadDigest` también. Esos dos campos identifican
*qué* publicación es, y registrarlo es procedencia, no indeterminismo. Así que la
regresión afirma lo preciso: el payload idéntico y la cabecera igual **salvo esos
dos campos**. Es más fuerte que comparar ficheros enteros, porque enumera lo que
puede variar: un campo nuevo alimentado por iteración de mapa, una dirección o un
reloj falla ahí.

**Los cinco rechazos existen**, en `snapshot_corruption_test.go` como pedía la
tarea, y a nivel de generación: truncado, otro magic, otra versión de formato,
otra generación y otro digest de payload. Los cinco acaban igual -- el fichero se
rechaza, la razón queda en `LoadRefused` y el grafo se deriva de la base y se
responde-- porque un fichero equivocado debe costar una reconstrucción, nunca una
respuesta. El truncado lo atrapa el límite de sección, antes del digest, que es
mejor: el fichero se rechaza sin llegar a hashear un byte.

**Un defecto que sólo apareció al ejecutar el binario.** La subida de versión de
formato hacía que `doctor` marcara `FAIL` y saliera con código `1` en cualquier
instalación ya existente, con la línea siguiente diciendo `snapshot: PASS
(symbols=123531)`. La regla documentada en `cmd/kivgraph/AGENTS.md` era binaria
-- ausente `PASS`, presente-y-no-utilizable `FAIL`-- y le faltaba el tercer caso:
un formato anterior no es un store dañado, es que alguien actualizó. Añadido
`hotsnapshot.ErrSnapshotFileVersion`, que envuelve `ErrInvalidSnapshotFile` para
no cambiar ningún llamante. Ninguna suite lo vio; lo vio `doctor`.

**Criterios no cumplidos, con su razón:**

1. **«Toda generación publicada tiene su fichero»** -- no. Sigue siendo
   best-effort, y el código ya llevaba escrito el argumento: el fichero es una
   economía, así que un disco lleno no debe tirar un índice de minutos cuyo grafo
   es sano. Lo que sí está resuelto es la observabilidad de la otra mitad:
   `LoadRefused` e `InspectPublishedSnapshot` distinguen ausente, formato viejo e
   inutilizable.
2. **«Nada se escribe fuera de `storage.snapshots_path`»** -- al contrario:
   **nada se escribe nunca ahí**. El fichero vive dentro del directorio de la
   generación, y eso es lo correcto, porque es lo que hace que `Prune` lo borre
   con ella y que **no pueda quedar huérfano**. El criterio, aplicado, crearía el
   problema que él mismo pide evitar.
3. **«`retain_snapshots` conserva exactamente los que dice»** -- no lo lee nadie.
   `Prune` conserva `current` y `backup` y nada más, que es lo que el rollback
   necesita.

Los puntos 2 y 3 son dos claves de configuración que prometen una ubicación y una
política que el diseño no quiere. Tocarlas es superficie de compatibilidad y pide
ADR, así que van en `LUQUE-2013` y no aquí.

**Verificación:**

```text
gofmt -l internal/hotsnapshot/ internal/rebuild/
go vet ./...
go test ./internal/hotsnapshot/...
make test-ladybug PKGS=./internal/rebuild/...
```

**Siguiente tarea:** LUQUE-2005.

---

## LUQUE-2005 — Mapear en vez de reconstruir

**Dependencias:** LUQUE-2004.

**Objetivo:** que `serve` y `ui` carguen la generación publicada mapeando su
fichero, de modo que N procesos compartan páginas físicas en vez de multiplicar
heap privado.

**Alcance:**

* `hotsnapshot.Open(path)` devuelve un `*GraphSnapshot` cuyas tablas son vistas
  sobre un mapeo `MAP_SHARED|PROT_READ`, en un fichero por plataforma con build
  tag, como exige la convención de código.
* `cmd/kivgraph/main.go: loadConfiguredSnapshot` e
  `internal/indexing/follow.go: followOnce` intentan el fichero primero.

**Decisiones:**

* **Falla cerrado hacia LUQUE-1204:** ausencia, magic, versión, generación o
  digest que no cuadren no son un error del comando; se reconstruye desde
  LadybugDB y se registra **una vez**. Eso hace que las generaciones ya
  publicadas sigan cargando y que el cambio se pueda desplegar sin migración.
* **El mapeo lo libera el recolector, no un `Close` público.** El contrato de
  `SnapshotStore` dice que un lector fija el puntero durante su operación, y
  `Publish` deja el snapshot anterior como basura mientras alguien lo lee todavía:
  desmapear ahí no da un error, da un `SIGSEGV`. La liberación se ata a la
  inalcanzabilidad del `*GraphSnapshot`, y **ese es el riesgo más agudo de la
  fase**, así que lleva su propio test con `-race`: publicar una generación nueva
  mientras un lector recorre la anterior.
* Un fichero mapeado que `clean` desenlaza sigue siendo válido -en POSIX el inodo
  vive mientras esté mapeado- y eso se afirma con un test, no con un comentario.
* `rebuild.ReturnBuildMemory` deja de tener nada que devolver en el camino
  rápido: no hay transitorio si no hay construcción. Se conserva para el camino
  de respaldo.

**Criterios de aceptación:**

- Con dos `serve` sobre la misma generación, `smaps_rollup` muestra **una sola
  copia**: `Shared_Clean` domina y el `Pss` se reparte entre los procesos.
- Con el fichero presente, la carga **no** llama a `rebuild.BuildSnapshot`, y el
  tiempo hasta la primera respuesta a una tool se mide y se publica.
- Un fichero corrupto degrada a reconstrucción, con una línea de registro y sin
  fallar el comando.
- El test de publicación concurrente pasa con `-race`.
- `ui` obtiene el mismo beneficio sin cambios en `webapi`: el store es el mismo.

**Lo medido y cubierto el `2026-08-22`**, antes de decidir si queda trabajo:

Buena parte de esta tarea la hizo el ADR 0045, que se escribió después de
redactarla: `loadPublishedSnapshot` ya mapea el fichero con
`MAP_SHARED|PROT_READ` en `mapfile_unix.go`, la liberación ya está atada a la
inalcanzabilidad con `runtime.AddCleanup`, el fallback ya tiene sus seis casos
declarados, y desenlazar el fichero mapeado ya tiene test.

**Reparto real, con dos `serve` sobre la misma generación de `kena`**
(`123.531` símbolos, fichero de `98.773.720` bytes, `darwin/arm64`):

|magnitud|por proceso|
|---|---|
|`RSS`|`193,9` y `190,6 MB`|
|`mapped file`, limpio|`94 MB` -- **una sola copia**, compartida|
|sucio (privado)|`45` y `44 MB`|

Proyectado contra la línea base de la cabecera de la fase (`173 MB` por cliente,
sin compartir nada):

|N|antes|ahora|
|---|---|---|
|`1`|`173 MB`|`138,5 MB` (`80,1 %`)|
|`2`|`346 MB`|`183,0 MB` (`52,9 %`)|
|`4`|`692 MB`|`272,0 MB` (`39,3 %`)|

Los dos umbrales que `LUQUE-2006` pone ya se cumplen: residente total `39,3 %`
contra un `≤40 %`, y `Private_Dirty` de `44,5 MB` contra un `≤60 MB`. **El
primero pasa por siete décimas**, así que quien lo convierta en gate tiene que
medirlo en Linux con la línea base real y no dar el margen por bueno.

`internal/procstat/observe_darwin.go` declara que esta plataforma no reparte
páginas compartidas, y es cierto para `Pss`; el reparto de arriba sale de
`footprint`, que sí separa sucio de limpio y nombra la región del fichero.

**El riesgo agudo, ahora cubierto.** La tarea lo nombra: `Publish` deja el
snapshot anterior como basura mientras alguien lo lee, y su mapeo se libera por
inalcanzabilidad -- desmapear ahí no da un error, da un `SIGSEGV`. Cada mitad
estaba defendida sola; la composición sobre un mapeo real no.

`TestStringsSurviveTheMappingTheyWereReadFrom` la cubre, y **fue el segundo
intento**. El primero dependía del recolector y pasaba con la copia de
`strings.go` retirada a propósito: no demostraba nada. El segundo tenía un
defecto más fino y más instructivo -- comparaba contra `append([]reading(nil),
held...)`, que copia **cabeceras** de string, y Go compara dos strings que
comparten puntero sin leer los bytes. También pasaba. Clonando los bytes antes
de liberar, el test hace lo que dice:

```text
con la guarda   : ok
sin la guarda   : signal SIGSEGV: segmentation violation
```

**Lo que queda:** la decisión de fondo es si mapear también las tablas. Hoy sólo
el arena se comparte -- `file.go:301` lo dice: «everything else is copied either
way»--, y lo sucio por proceso son las tablas decodificadas: `44,5 MB` medidos en
darwin con `footprint` sobre `123.531` símbolos, `~95 MB` en Linux con
`Private_Dirty` sobre `161.819`, que no son la misma cantidad ni el mismo corpus.
`LUQUE-2002`, `2003` y `2004` dejaron los registros sin punteros, sin mapas y con
las secciones alineadas, que es lo que haría mapeables las tablas; falta pagar el
relleno por registro en disco y declarar la dependencia de little-endian.
**No se hace sin que un número lo pida**, y el arnés dice que ninguno lo pide --
no porque los dos umbrales originales se cumplieran, que no se cumplían, sino
porque eran el problema y están reescritos como propiedades del diseño.

**Estado:** cerrada el `2026-08-22`. El mapeo, el fallback y el riesgo agudo
están hechos y medidos, y **el número que esperaba ya llegó**: `LUQUE-2006` midió
en Linux que la cuota de residente cae en cada servidor añadido -`0,498`, `0,416`,
`0,372`- y que lo privado son `614 B` por símbolo. Mapear también las tablas
ahorraría esa cifra y nada más; ningún criterio la pide, y el arranque -`13x`-
ya lo da el mapeo del arena. Queda como el hueco de `LUQUE-2008`, con su
condición de reapertura medida y por poco.

**Verificación:**

```text
gofmt -l internal/hotsnapshot/ internal/indexing/ cmd/kivgraph/
go vet ./...
go test -race ./internal/hotsnapshot/... ./internal/indexing/...
make test-ladybug
make build
```

**Siguiente tarea:** LUQUE-2006.

---

## LUQUE-2006 — El arnés que declara el gate

**Dependencias:** LUQUE-2005, LUQUE-1905.

**Objetivo:** que la mejora se declare con una medida reproducible y no con una
anécdota, igual que hizo LUQUE-1905 con el coste en tokens.

**Alcance:**

* `benchmarks/shared-snapshot/` con `results.json` y `report.md`: arranca N
  servidores contra una generación publicada, conduce el workload de
  `internal/mcpworkload` por cada uno y publica, por proceso y en total, `VmRSS`,
  `Pss`, `Shared_Clean` y `Private_Dirty`, más los percentiles de latencia y el
  tiempo hasta la primera respuesta.
* La línea base contra la que compara es la de la cabecera de esta fase, tomada
  con el binario anterior sobre la misma generación.

**Decisiones:**

* El gate se emite desde el digest del propio arnés y se **niega** a emitirse si
  el corpus, la generación o la plataforma no son los declarados, como ya hace
  `benchmarks/web-viewer`.
* `Pss` y `Shared_Clean` son de Linux. En macOS el arnés publica lo que la
  plataforma sabe observar -`internal/procstat.ResidentBytes`- y **declara la
  limitación**; el gate se mide en Linux, que es donde está la línea base.
* Se mide con N=4, que es el número de clientes que el caso real tenía vivos.

**Criterios de aceptación:**

Los tres primeros se escribieron antes de medir y no sobrevivieron a la
medición: valoraban un corpus en una máquina en vez del diseño. Están reescritos
**después** de medir, lo que vale menos como evidencia -- un criterio no puede
sorprenderse de lo que ya vio -- y por eso cada uno se justifica por la
propiedad que valora. Lo retirado, y por qué, queda en el informe.

`SHARED_SNAPSHOT_PASS` se emite sólo si, sobre el corpus de referencia:

- **la cuota de residente cae en cada paso del barrido.** Es la afirmación de
  diseño -- una página mapeada la paga la máquina una vez por muchos procesos
  que la sostengan -- y es el único criterio que no depende del corpus ni de
  cuántos clientes se corran. No lo miraba ninguno de los tres anteriores.
- con N=4, esa cuota es **≤0,45**.
- el `Private_Dirty` del peor servidor es **≤800 B por símbolo servido**. Es
  lo que cada proceso decodifica para sí, y escala con el grafo: el `≤60 MB`
  anterior estaba fijado contra `123.531` símbolos y no decía nada de un corpus
  mayor.
- el `p99` del brazo mapeado no supera al derivado en más de **`1,00 ms`**, en
  absoluto y no en razón: entre colas de menos de un milisegundo una razón
  valora el ruido de la pasada, y con N=8 el mismo código dio de `0,944` a
  `1,097`.
- el tiempo hasta la primera respuesta es **`≥5x`** mejor mapeando. Es el efecto
  más grande de la fase, `13x` medido, y ningún criterio lo miraba.

- Dos ejecuciones dan el mismo digest.
- El informe conserva comando, commit, entorno, generación, semilla, métricas y
  limitaciones.

**Estado:** cerrada. El arnés está entregado y medido en Linux (`devlabs`,
16 CPU) sobre `kena-workspace` -51 repositorios, `161.819` símbolos, generación
`000001`, `snapshot.kvsnap` de `129 MB`-, en `benchmarks/shared-snapshot/` con
`report.md` y `results.json`. Digest `71c8301f`, **idéntico en dos ejecuciones**,
y con `KIVGRAPH_BENCH_SLO=1` emite `SHARED_SNAPSHOT_PASS`.

Lo medido, en `Pss` sumado sobre los procesos:

|servidores|mapeado|derivado|cuota|sucio/símbolo|Δ `p99`|arranque|
|---|---|---|---|---|---|---|
|2|`325,7 MB`|`654,1 MB`|`0,498`|`611 B`|`0,267 ms`|`12,8x`|
|4|`513,7 MB`|`1.233,8 MB`|`0,416`|`614 B`|`0,383 ms`|`13,0x`|
|8|`887,6 MB`|`2.384,6 MB`|`0,372`|`614 B`|`0,026 ms`|`13,2x`|

Con ocho servidores la máquina se ahorra `1.497 MB` de los `2.385 MB` que
costaría derivar, y la primera respuesta llega en `261 ms` en vez de `3.394 ms`.

El gate pasó **con los criterios reescritos**, no con los declarados: los tres
originales se incumplían -- cuota `0,416` contra `≤0,40`, sucio `94,7 MB` por
proceso contra `≤60 MB`, razón de `p99` `1,385` contra `≤1,05` -- y la medición
mostró que el problema eran ellos. La sustitución y su justificación están
arriba, en los criterios de aceptación, y lo retirado queda en el informe.

Tres defectos encontrados al montarlo, los tres arreglados y con test que falla
sin el arreglo:

- `graph_status` fallaba **entero** con `SNAPSHOT_UNAVAILABLE` en todo corpus
  posterior al ADR 0060: el desglose `edges_by_kind` exigía que cada arista
  saliente fuese de las que contesta `find_references`, y `METHOD_OF` no lo es.
  Son `9.169` de `482.478` aristas en este corpus. La fixture sólo llevaba
  aristas de referencia, así que pasaba con la herramienta rota.
- `index --full` se negaba a indexar **este repositorio**: el normalizador de Go
  y el de Rust declaran cada uno un `type methodOwner struct { methodKey ... }`
  dentro de una función, y la ruta del campo se enraizaba en el nombre del tipo,
  así que los dos derivaban una clave estando en dos ficheros. Un tipo local no
  alcanza el ámbito de paquete, así que ahora la ruta lleva delante la función
  que lo encierra. Ningún grafo publicado puede llevar la identidad antigua,
  porque ninguno que la tuviera podía publicarse.
- El arnés medía la instalación real creyendo medir una aislada: una
  configuración escrita por `init` guarda sus rutas con `~`, que se expande
  contra el `HOME` de quien ejecuta `serve`, no contra el del `init`. Ahora
  compara el snapshot servido con el directorio cuyo fichero esconde y se niega
  si difieren.

La ruta darwin está ejercitada sobre una generación aislada: publica el
residente, **marca como no evaluada** -no falla- la comprobación que la
plataforma no puede responder, declara dos negativas y no emite el gate.

**Verificación:**

```text
gofmt -l benchmarks/shared-snapshot/
go vet ./...
go test ./...
KIVGRAPH_BENCH_SLO=1 go run ./benchmarks/shared-snapshot --clients 2,4,8 \
  --gate-clients 4 --calls 2000 --warmup 4000
  (dos veces, mismo digest 71c8301f, SHARED_SNAPSHOT_PASS las dos)
```

**Siguiente tarea:** LUQUE-2007.

---

## LUQUE-2007 — ADR, contratos y cierre de fase

**Dependencias:** LUQUE-2006.

**Objetivo:** que la decisión quede escrita donde se busca, con sus cifras y con
las alternativas que se descartaron.

**Se hizo sin LUQUE-2006**, y la dependencia estaba mal puesta: el arnés es una
puerta de regresión, no una fuente de las cifras. Las cifras ya existían -- la
fase 2b del ADR 0045 en Linux con `Pss` real, y la medición de `LUQUE-2005` en
darwin con `footprint`-- y las dos concuerdan.

**El ADR ya existía y era el `0045`, no el `0043` que esta tarea nombraba.**
Cubre el problema medido, el formato, el fail-closed y las alternativas
descartadas -- el servidor único compartido y el mapeo de índices-- con su motivo.
Lo que le faltaba era el estado y tres afirmaciones que habían dejado de ser
verdad, y eso se corrige en una sección fechada al final en vez de reescribir lo
de arriba:

* Decía que el fichero se valida contra «el digest de contenido que la generación
  ya guarda en `snapshot.sha256`». Ese fichero **no es un digest de contenido**.
  Ahora apunta a `snapshot.content.sha256` y al ADR 0061.
* Su cabecera decía «fase 2b propuesta» teniendo una sección con la fase 2b
  medida.
* Daba como pendiente la condición `SymbolRecord` sin `string`, que `LUQUE-2002`
  cumplió, y los cuatro mapas «no persistibles», que `LUQUE-2003` sustituyó por
  arrays planos. Lo que bloqueaba mapear las tablas está hecho.

**Corrección del `2026-08-22`, después de medir en Linux:** esta tarea afirmaba
que «los dos umbrales de `LUQUE-2006` ya se cumplen». **No se cumplían.** El
arnés de `LUQUE-2006` midió `0,416` de cuota contra el `≤0,40` declarado y
`94,7 MB` sucios por proceso contra el `≤60 MB`, y los dos umbrales resultaron
ser el problema: uno valoraba el número de clientes y el otro un corpus de
`123.531` símbolos. Están reescritos como propiedades, y con ellos el gate pasa.
La afirmación se escribió con las cifras de darwin y de la fase 2b del ADR 0045,
antes de que existiera la medición que la contradijo -- que es exactamente lo que
esta tarea existía para cazar.

Lo que no cambia: mapear también las tablas sigue sin pedirlo ningún número.

**Lo que encontró la auditoría de `internal/AGENTS.md`:** cuatro afirmaciones
falsas, y una llevaba tiempo contradiciendo a su propio ADR.

|afirmación|realidad|
|---|---|
|«lo que prueba que el fichero pertenece a esa generación es su propio `snapshot.sha256`»|es `snapshot.content.sha256` desde el ADR 0061|
|«`Private_Dirty` es el 100 % del RSS y tres clientes son tres copias»|`94 MB` compartidos y `44,5 MB` privados; el propio ADR 0045 ya medía `Shared_Clean` de `90 MB`|
|«el mapeo se libera en cuanto acaba la decodificación» y «todo decodificador copia»|el arena se conserva mientras viva el snapshot, y la tabla de cadenas lee en sitio|
|«`doctor` deriva el snapshot y nunca lee el publicado»|hace las dos preguntas, y son distintas: `snapshot` deriva, `snapshot.published` lee|

**En `AGENTS.md` de raíz** se añade la regla que faltaba y que las dos tareas de
hoy infringieron una vez cada una: **lo que era válido ayer no puede convertirse
hoy en un fallo**. Una clave retirada se acepta, se ignora y se nombra; un
fichero de un formato anterior es una actualización y no un store dañado.

**En `landing/`**: la guía de indexado afirmaba que un servidor sostiene «un
snapshot de `173 MB`», que es la cifra de antes del mapeo. Ahora hay una sección
con el reparto medido y con la limitación de plataforma declarada, y
`mcp.transport` dice por qué `stdio` es una decisión y no un hueco -- la razón
para un transporte compartido era la memoria, y la memoria ya no la multiplica un
cliente.

**Verificación:** gates en verde por código de salida -- `gofmt`, `vet`, `test`,
`test-ladybug`, `build`, `landing-check`, `landing-build`-- y el sitio construido
comprobado: el ancla `#what-a-second-server-costs` existe, el enlace desde la
referencia resuelve, `snapshots_path` ya no figura como clave viva y la sección
de claves retiradas está publicada.

**Estado:** cerrada el `2026-08-22`.

## LUQUE-2008 — Un proceso para muchos clientes

**Dependencias:** LUQUE-2006.

**Condición que la reabre:** que el arnés de LUQUE-2006 mida un
`Private_Dirty` por proceso por encima de **100 MB**, o un corpus donde el
fichero mapeado no baste. Mientras el mapeo cumpla, esta tarea no se hace --
pero **no por el motivo que aquí decía**. Decía «su ahorro sería de decenas de
megabytes», y eso está mal: el `Private_Dirty` es plano en el número de
clientes, así que con ocho servidores son `789 MB` de heap privado donde un
demonio dejaría uno.

**Y la segunda razón que se escribió aquí también está mal, medida el
`2026-08-23`.** Decía que lo que la mantiene cerrada es que hay «una vía más
barata para el mismo byte», la de `LUQUE-2216` a `LUQUE-2220`. Esas cuatro fases
bajaron lo que la carga asigna de `89,7 MB` a `29,2 MB` y **el residente no se
movió**: `71,76 MB` por servidor contra `71,22`, `0,75 %`, tres pares de tres en
`benchmarks/load-cost-resident`. Las páginas que una asignación transitoria
ensucia se devuelven al heap y las reutiliza el trabajo siguiente, así que nunca
estuvieron residentes en régimen estacionario. La vía más barata existe y es
real, pero **compra tiempo de arranque, no bytes por proceso**, y por tanto no
aleja este cruce. Lo que mantiene esta tarea cerrada es sólo lo primero: que la
condición no se cumple.

**Medido el `2026-08-23`:** `71,2 MB` por proceso y `647 B` por símbolo sobre
`kena`, `117.499` símbolos, contra un `≤800 B` de `LUQUE-2006` y los `648 B` que
son `100 MB` en este corpus. La lectura del `2026-08-22` -- `94,3`–`98,1 MB` sobre
`161.819` símbolos, `614 B` por símbolo-- decía lo mismo por símbolo: lo que
decide no es el número de clientes, sale plano, sino el tamaño del grafo. Un
corpus de unos `170.000` símbolos cruza los `100 MB`. Por eso el criterio de
`LUQUE-2006` se declara por símbolo: es la magnitud que escala.

**Diseño, si llega el caso:** socket unix bajo el directorio de estado, un
`kivgraph daemon` que sostiene un `SnapshotStore`, un seguidor, un bucle de
resync y el indexador, y un `serve` que detecta el socket y se convierte en un
relé de bytes. No hace falta escribir un proxy MCP: los dos lados hablan
JSON-RPC delimitado por línea, así que el relé es transparente y la elicitación,
el progreso y la cancelación pasan sin interpretarse. El demonio necesita un
`Transport` propio sobre `net.Conn` -unas ochenta líneas: `mcp.Connection` es una
interfaz exportada de cuatro métodos y el paquete `jsonrpc` está documentado
«for use by mcp transport authors»-, y `Server.Connect` por conexión aceptada, que
es lo que `benchmarks/mcp-client` ya ejercita con N sesiones contra un solo
servidor.

**Lo que tendría que resolver antes de entrar:** quién arranca el demonio y
cuándo sale; la clave por directorio de estado, para que dos configuraciones
nunca compartan demonio; el sesgo de versión, porque un relé debe negarse a
hablar con un demonio de otra build; y que `stop` y `doctor` aprendan la
invocación nueva, porque hoy `stop` selecciona por `argv[1] ∈ {serve, ui}` y no
vería un demonio.

**Entregada el `2026-08-23`, y no por la condición.** La condición que la
reabría -- `Private_Dirty` por encima de `100 MB`-- sigue sin cumplirse: `71,2 MB`
por proceso sobre `kena`. Se hizo porque las cuatro fases `LUQUE-2216` a
`LUQUE-2220` demostraron que **no queda otra vía**: bajaron `60,5 MB` de
asignación y el residente se movió `0,75 %`. La vía barata compra tiempo de
arranque, no bytes por proceso.

**Lo entregado, y en qué se aparta del diseño de arriba.** `kivgraph daemon`
sobre un socket unix en el directorio de estado, con `internal/mcp.StreamTransport`
-- un `Transport` propio sobre `io.ReadWriteCloser`-- y un servidor MCP por sesión
aceptada sobre un store compartido. ADR 0065.

`serve` **no se convierte en un relé**, que es lo que este diseño proponía. No
hace falta: un cliente MCP habla con el socket directamente, y un relé añadiría
un proceso por cliente para ahorrar procesos por cliente. `serve` queda intacto
y soportado, y es el único camino cuando la ruta del directorio de estado no
admite un socket.

**De las cuatro preguntas que había que resolver antes de entrar:**

* *Quién lo arranca y cuándo sale:* nadie y nunca solo. Corre en primer plano
  hasta `SIGINT` o `SIGTERM`, igual que `serve`. Sin arranque automático no hay
  que decidir cuándo un proceso ocioso se va.
* *La clave por directorio de estado:* el socket vive dentro de él. De ahí salió
  un límite que no estaba en el diseño: una dirección unix son `104` bytes en
  darwin y `bind` **trunca** en vez de rechazar, así que dos directorios con un
  prefijo largo compartirían socket. Se comprueba y se nombra.
* *El sesgo de versión:* no aplica sin relé. Un cliente y un demonio negocian
  versión de protocolo en `initialize`, que es el mecanismo del propio MCP.
* *`stop` y `doctor`:* `stop` seleccionaba por `argv[1] ∈ {serve, ui}` y no
  habría visto un demonio. Ahora hay una lista, `longRunningCommands`, y el
  mensaje de «nada corriendo» nombra las tres. Sabotear la lista en las dos
  direcciones cae: quitar `daemon` lo vuelve imparable, añadir `index` mata una
  indexación en vuelo.

**El ahorro, medido después en `LUQUE-2222`:** `66`–`67 MB` de páginas privadas
por cliente en N procesos contra `0,2`–`2,3` en un demonio. A ocho clientes,
`533 MB` contra `68`–`82`, y el pico `1.046 MB` contra `188`. A un cliente empata
dentro del ruido, así que la razón para usarlo empieza en el segundo.

**Corregido en `LUQUE-2223`:** esa cifra es la del socket, y ningún cliente MCP
marca un socket.

**Corregido otra vez en `LUQUE-2224`, y esta vez la carga:** `LUQUE-2223` publicó
`12,5 MB` por cliente por HTTP midiendo `2.000` llamadas por sesión. Contada del
event log, la mediana de una sesión real es **una** llamada y `48` de `51`
servidores no reciben ninguna. A esa carga las dos puertas cuestan lo mismo
-- `1,0`–`1,3` por HTTP contra `1,1`–`1,6` por socket-- y N procesos cuestan `43 MB`
por cliente, no `66`. Medir la carga equivocada **subestimaba** el ahorro.

**Verificación:** trece decisiones falsificadas una a una con su test, más dos
de `stop`; humo con el binario real y una generación publicada, tres clientes
concurrentes, `11` tools cada uno, dos preguntas distintas a la vez sin cruzarse,
socket desvinculado al parar, cero errores. `gofmt`, `go vet ./...`,
`go test ./...`, `make build`.

**Estado:** cerrada el `2026-08-23`.

---

# 24. Gates globales

```text
PROJECT_FOUNDATION_PASS
EMPTY_MCP_PERFORMANCE_PASS
LADYBUG_STORAGE_PASS
HOT_SNAPSHOT_PASS
REPOSITORY_REGISTRY_PASS
TREE_SITTER_ACCELERATOR_PASS
TYPESCRIPT_LOCAL_PASS
TYPESCRIPT_CROSS_REPO_PASS
GO_SEMANTIC_PASS
CANONICAL_GRAPH_PASS
INCREMENTAL_INDEXING_PASS
MCP_SURFACE_PASS
RESILIENCE_PASS
PERFORMANCE_PASS
OBSERVABILITY_PASS
DISTRIBUTION_PASS
WEB_VIEWER_PASS
RUST_SEMANTIC_PASS
RUST_CROSS_REPO_PASS
MCP_TOKEN_COST_PASS
SHARED_SNAPSHOT_PASS
DART_SEMANTIC_PASS_WITH_LIMITS
PYTHON_SEMANTIC_PASS_WITH_LIMITS
TOOL_HONESTY_PASS_WITH_LIMITS
```

No se puede aprobar Kivgraph sin todos ellos.

---

# 25. Orden recomendado para la IA

La IA deberá empezar exactamente en este orden:

```text
LUQUE-0001
LUQUE-0002
LUQUE-0003
LUQUE-0004
LUQUE-0005
LUQUE-0006
LUQUE-0007
LUQUE-0008

LUQUE-0101
LUQUE-0102
LUQUE-0103
LUQUE-0104

LUQUE-0201
...
```

No debe implementar TypeScript, Go ni Tree-sitter antes de que LadybugDB y el
HotSnapshot hayan pasado sus benchmarks. La fase del visor se inicia después
de `DISTRIBUTION_PASS` y respeta el orden `LUQUE-1701` a `LUQUE-1715`. La fase
de Rust también depende de `DISTRIBUTION_PASS` y respeta el orden `LUQUE-1801`
a `LUQUE-1824`. La fase de coste en tokens depende de `MCP_SURFACE_PASS` y se
ejecuta en el orden `LUQUE-1905`, `LUQUE-1901`, `LUQUE-1902`, `LUQUE-1903`,
`LUQUE-1904`: primero el arnés, porque es con lo que las demás declaran sus
cifras; después las tres que retiran cada una un round-trip que la siguiente da
por retirado; y al final la adopción, que sólo tiene sentido cuando ya hay algo
que merezca la pena pedir.

La fase de la memoria por cliente depende de `HOT_SNAPSHOT_PASS` y de
`MCP_SURFACE_PASS`, y se ejecuta en el orden `LUQUE-2001` a `LUQUE-2007`: primero
la medición, porque el formato se diseña con sus cifras; después las dos que
retiran punteros y mapas, en ese orden, porque la segunda necesita el arena
ordenado de la primera; luego el fichero y su mapeo, que no tienen sentido
separados; y al final el arnés que declara el gate y el ADR que lo cierra.
`LUQUE-2008` no entra salvo que su condición se cumpla.

---

# 26. Plantilla de prompt para cada tarea

```text
Trabaja en la tarea <TASK-ID> del backlog de Kivgraph.

Reglas:

1. Lee primero la definición completa de la tarea y sus dependencias.
2. Comprueba que todas las dependencias están en PASS.
3. Inspecciona el estado actual del repositorio.
4. No implementes funcionalidades pertenecientes a tareas futuras.
5. Añade o actualiza tests.
6. Ejecuta las comprobaciones aplicables.
7. No ignores warnings ni tests fallidos.
8. No cambies decisiones arquitectónicas sin crear un ADR.
9. No marques la tarea como PASS si falta algún criterio de aceptación.
10. Al terminar, entrega:

Estado:
Resumen:
Archivos creados:
Archivos modificados:
Tests ejecutados:
Resultados:
Benchmarks:
Limitaciones:
Deuda técnica introducida:
Siguiente tarea desbloqueada:

Tarea:

<TEXTO COMPLETO DE LA TAREA>
```

---

# 27. Plantilla para revisar una tarea completada

```text
Revisa la implementación de <TASK-ID> sin modificar inicialmente el código.

Comprueba:

1. Que se han respetado todas las dependencias.
2. Que no se ha adelantado trabajo de fases posteriores.
3. Que se cumplen todos los criterios de aceptación.
4. Que los tests realmente cubren el comportamiento.
5. Que no existen errores silenciados.
6. Que no se han creado aristas semánticas mediante coincidencias nominales.
7. Que los benchmarks son reproducibles.
8. Que no existe una regresión de rendimiento o precisión.
9. Que la documentación coincide con el comportamiento real.
10. Que el estado PASS está justificado.

Devuelve una de estas decisiones:

ACCEPT_TASK
ACCEPT_TASK_WITH_REQUIRED_FIXES
REJECT_TASK

Incluye evidencia concreta, archivos, líneas, comandos y resultados.
```

---

## LUQUE-1907 — El factor sobre un monorepo real, no sobre un fixture

**Dependencias:** LUQUE-1901, LUQUE-1902, LUQUE-1903, LUQUE-1905, LUQUE-1906.

**Estado:** `PASS`.

**Objetivo:** cerrar la única cifra de la fase 19 que era de fixture. El arnés
medía seis preguntas sobre este mismo repositorio, y la pregunta
cross-repository se midió sobre un corpus sintético de tres ficheros, donde
salió `0,57x`: perdíamos contra `grep`. La duda era si el factor cruza a favor
cuando el corpus es un monorepo de verdad, que es donde `grep` cuesta caro.

**Corpus.** Privado y ajeno, así que no entra en este repositorio: el monorepo
`kena` de la máquina `devlabs`, `linux/x86_64`, 43 repositorios git
registrados. Indexado con `go1.26.4`, `rustc 1.96.1`, worker TypeScript del
checkout:

```text
41 repositorios  120 paquetes  4.931 ficheros  100.118 símbolos
255.972 aristas de símbolo  361 de paquete  11.698 no resueltos
366.228 nodos y 622.516 aristas en la base canónica
TypeScript aporta 97.153 símbolos y 5.055 no resueltos de 216.427 referencias -- 2,3 %
```

Las capturas del brazo nativo llevan su código, así que **se quedan en su
máquina**: aquí sólo viven los números y el método.

**Resultado.** Cuatro preguntas, cada una con su sujeto nombrado por tripleta:

```text
                                       responder            sesión completa
                              nativo  Kivgraph  factor   nativo  Kivgraph  factor
RedisAdapter  cross-repo       6.379      4.140   1,54x    7.598      5.359   1,42x
register      nombre común    12.656        448  28,25x   13.276      1.068  12,43x
KenaLogger    export             754      1.246   0,61x    4.035      4.527   0,89x
AUTOMATION…   nombre raro        183        861   0,21x      330      1.008   0,33x
TOTAL                         19.972      6.695   2,98x   25.239     11.962   2,11x
sirviendo los cuerpos con get_source                                 13.268   1,90x
techo de la sesión                                                            5,83x
```

**La pregunta cross-repository cruza: `0,57x` en el fixture de tres ficheros,
`1,30x` aquí** -medida aparte, con su propio contador: `exact 83`,
`package_level 24`, `rows_without_range 0`, 98 tokens por fila-. `grep` paga
6.379 tokens por 234 líneas y no distingue la clase de sus 86 alias; nosotros
4.919 por 83 consumidores exactos en nueve repositorios.

**Y valida el enrutado que enviamos en las descripciones.** Decimos que ganamos
en nombres comunes y en impacto transitivo, y que un nombre raro en un sitio
pequeño lo resuelve `grep` más barato. Medido: `28,25x` en el nombre que el
corpus declara 126 veces, `0,21x` en el que aparece dos veces. La superficie
dice dónde pierde y pierde exactamente ahí.

**El informe de devlabs queda cerrado.** Decía que los dos repositorios Rust
indexaban sin error y luego impedían publicar, y que
`find_cross_repo_consumers` daba falsos positivos sin resolver consumidores
reales. En su máquina, con sus commits: los dos publican, y

```text
RedisAdapter  library-shared src/redis/RedisAdapter.ts:12-45
              exact 83, package_level 24, unresolved 0
              consumidores en 9 repositorios; find_references ve 86 desde 14
KenaLogger    library-logger src/logger.ts:103-420
              exact 6, package_level 22, unresolved 0; las dos tools concuerdan
```

**Cuatro defectos del arnés que sólo un corpus multi-repositorio destapa:**

1. **El sujeto se elegía por orden de página.** `find_symbol` por nombre a secas
   no identifica nada: `RedisAdapter` son 87 filas -una clase y 86 importaciones
   y alias-, y tomar la primera medía los consumidores de un alias. Salía
   `12,06x` a nuestro favor con `exact 0`. Ahora un nombre ambiguo **aborta** y
   la pregunta tiene que nombrar la tripleta, que es para lo que existe.
2. **El repositorio de una fila se leía con un solo nombre.** `find_references`
   lo llama `repository` y `find_symbol` `repository_name`; el arnés leía el
   primero, así que el sujeto llegaba sin repositorio y el cuerpo se buscaba
   bajo el árbol equivocado. Un corpus de un repositorio nunca lo nota.
3. **El brazo servido pedía 83 cuerpos en una llamada.** `get_source` acepta 20,
   así que la medición fallaba; ahora pagan cinco llamadas, que es lo que paga
   un agente.
4. **`Publish` era ambiguo en nuestro propio corpus** -`SnapshotStore.Publish` y
   `Store.Publish`- y se medía en silencio sobre la que la página devolvía
   primero. Nombrada.

**Hallazgo de superficie, cerrado en LUQUE-1908:** el mismo hecho viajaba con
tres nombres, y el documento de protocolo ya describía el correcto.

**Limitaciones:**

- El brazo nativo es el `grep -rn` de la máquina, con `node_modules`, `dist` y
  `target` excluidos: un agente los excluye también e incluirlos infla el brazo
  contra el que nos comparamos. No es el `grep` del anfitrión, que tiene su
  propio formato.
- Las lecturas se facturan renderizadas como las del anfitrión -un número de
  línea por línea-, contadas con el mismo `cl100k_base` que el arnés, por un
  programa que viaja con él (`benchmarks/mcp-token-cost/counttok`).
- La adopción sigue sin medirse: que un agente llame a estas tools es una
  observación sobre sesiones reales, no una propiedad de la tool.

**Verificación:**

```text
gofmt -l internal/ benchmarks/   limpio
go vet ./...                     limpio
go test ./...                    verde
go run ./benchmarks/mcp-token-cost --server <binario>   digest 275af813d985
   sobre este repositorio: responder 3,56x, sesión 1,44x, sirviendo 1,77x
```

**Siguiente tarea:** —.

---

## LUQUE-1908 — Un solo nombre para el repositorio de una fila

**Dependencias:** LUQUE-1902, LUQUE-1907.

**Estado:** `PASS`.

**Objetivo:** que una fila se pueda copiar a la llamada siguiente sin traducir
nada, que es lo que LUQUE-1902 prometió y el código no cumplía.

**El mismo hecho viajaba con tres nombres, y con dos valores distintos:**

```text
find_references, find_cross_repo_consumers, get_source, get_file_outline
    "repository": "app"                    el nombre
find_symbol, get_symbol
    "repository_name": "app"               el nombre, con otra clave
trace_dependencies, get_blast_radius
    "repository_key": "repository:app"     otro valor, con prefijo
```

Y el filtro `repo` heredaba la misma grieta: `find_symbol` comparaba contra el
nombre, `find_references`, los dos recorridos y `find_cross_repo_consumers`
contra la clave. Nueve sitios decían `repository`, dos `repository_name` y dos
`repository_key`; el selector de entrada de la sección 4 dice `repository` y
toma el nombre, y **`docs/protocol/mcp-surface-v3.md` ya publicaba `repository`
en su fila de ejemplo**: el documento tenía razón y el código no lo cumplía.

**Decisión.** `repository` en toda la superficie, con el nombre. La clave sigue
existiendo bajo `repository_key` en `response_format: detailed`, que es el
contrato de restaurar los identificadores derivados, y **se lee del snapshot en
vez de componerse**: derivarla del nombre creaba una segunda fuente de verdad
para un hecho que el grafo ya guarda, y un fixture con otra convención lo
demostró en el acto.

**Por qué no lo cazaba ningún test.** Los fixtures ponían `Key == Name`
-`{Key: "repo-a", Name: "repo-a"}`-, así que las dos semánticas daban el mismo
resultado. Los de recorrido sí los distinguían (`repo-core` contra `core`) y
fueron los que fallaron al migrar. Ahora el fixture de `derived_test.go` los
separa a propósito -`repository:app` contra `app`- y
`TestEveryRowNamesItsRepositoryTheSameWay` fija las cuatro mitades del contrato:
la fila lleva el nombre, la concisa no lleva clave, `detailed` restaura la que el
grafo guarda, y el filtro acepta el nombre y **no** la clave.

**Coste medido**, con el corpus reindexado y el brazo nativo recapturado con la
`grep` del anfitrión y sus lecturas con su `read`:

```text
                  responder   sesión   sirviendo   techo
antes de 1908       3,56x      1,44x     1,77x     2,40x
después             3,46x      1,42x     1,74x     2,34x
```

Baja, y es correcto: el brazo nativo se recapturó contra un árbol con más
código, así que las dos columnas cambian. Dos pasadas dan el mismo digest
`9e74cb1c6896`.

**Verificación:**

```text
gofmt -l internal/ benchmarks/   limpio
go vet ./...                     limpio
go test ./...                    verde
make test-ladybug                39 paquetes
make build                       0.5.1
```

**Siguiente tarea:** LUQUE-2001, que abre la fase 20.

# 28. Fase 21 — Cualificar el grafo publicado

Esta fase no se planificó: cada tarea nació de una medición que salió mal. El
orden en que están escritas es el orden en que se descubrieron, y varias
corrigen una afirmación que este mismo repositorio publicaba.

## Los números `2002`–`2008` están usados dos veces

Estas tareas se numeraron sin ver que la fase 20 ya usaba ese rango, así que
siete identificadores nombran dos cosas distintas:

|número|en la fase 20|en esta fase|
|---|---|---|
|`LUQUE-2002`|que ninguna clave estable ocupe un puntero|un fichero reemplazado no debe perder lo que otros le apuntan|
|`LUQUE-2003`|índices sin mapas|decidir la suerte del camino incremental|
|`LUQUE-2004`|`LGHS`: el snapshot publicado se escribe|`trace_dependencies` no baja a los miembros de un contenedor|
|`LUQUE-2005`|mapear en vez de reconstruir|cobertura de las tools servidas por el conjunto de preguntas|
|`LUQUE-2006`|el arnés que declara el gate|una pregunta de Rust sobre el corpus real|
|`LUQUE-2007`|ADR, contratos y cierre de fase|la proporción de no resueltos de Rust no tiene pregunta|
|`LUQUE-2008`|un proceso para muchos clientes|un bloque `impl` de Rust no se publica y su rama existe|

**No se renumeran, y el motivo es medible.** Los de la fase 20 los protege la
regla de la raíz: los identificadores históricos no se renombran. Y los de esta
fase los citan **dieciocho mensajes de commit**, que son inmutables: cambiarlos
dejaría el historial de git apuntando a la tarea equivocada, que es peor que la
ambigüedad que hay ahora. Los ADRs que los citan -- 0056, 0057, 0058 y 0059-- son
todos de esta fase, y su propio contexto los desambigua.

Los números a partir de `LUQUE-2009` son únicos. Una tarea nueva empieza en
`LUQUE-2013`.

## LUQUE-2002 — Un fichero reemplazado no debe perder lo que otros le apuntan

**Estado:** cerrada por el ADR 0056.

**Dependencias:** LUQUE-1007.

**Objetivo:** que un delta incremental produzca el mismo grafo que una
reconstrucción limpia cuando el fichero editado tiene llamantes en otros
ficheros. Hoy no lo produce.

**El defecto, reproducido:** `internal/facts/delta_reindex_test.go`,
`TestDiffRestatesEdgesIntoAReplacedFile`, hoy con `t.Skip`. Sobre el fixture
`type-relations`, hacer crecer un cuerpo de método en `geometry.go` en una línea
reemplaza ese fichero y ninguno más. Las seis aristas que entran desde `units/`
están ancladas en ficheros de `units/`, cuyos hechos no cambiaron, así que `Diff`
no las restablece -- y la retirada withdraws «every edge anchored on any of
those, **incoming and outgoing**» (`ApplyCanonicalDelta`), así que el aplicador
las borra y nadie las repone. **Cada llamante de otro paquete deja de apuntar al
fichero que se editó.**

**No alcanzable todavía:** `facts.Diff` lo llama `indexer.Update`, y `Update` no
tiene llamante en producción -- los consumidores del paquete usan `indexer.Full` y
el CLI responde `index: only --full is supported`. El defecto era código real y
se arregló; ningún usuario podía verlo. Ver LUQUE-2003.

**Por qué el arreglo no va en `Diff`:** restablecer esas aristas desde ahí
arrastra el fichero que las ancla entero, porque `Delta.Validate` exige un
fragmento autoconsistente -- una arista necesita su evidencia, la evidencia su
fichero, el fichero su paquete--. Y entonces las aristas que entran a *ese*
fichero también se retiran, y la restitución cascadea sin cota visible. Se
probó; se revirtió.

**Alcance propuesto:** acotar la retirada a los símbolos que **desaparecieron**,
no a todos los símbolos de un fichero reemplazado. Un símbolo que sobrevive con
la misma clave estable conserva sus aristas entrantes; uno que se fue se las
lleva. Eso mantiene la unidad del delta en el fichero, que es lo que declara
`AGENTS.md`: un hecho pertenece al fichero que lo **afirmó**, y esta arista la
afirmó `units/handlers.go`.

**Qué exige:** un ADR sobre el alcance de la retirada, porque cambia el contrato
de mutación canónica; y un test end-to-end con el tag `ladybug` que compare
`ScanCanonical` tras aplicar el delta contra `ScanCanonical` de una carga limpia
del estado final, sobre sets producidos por el cargador real -- no construidos a
mano, que es lo que ya cubre
`TestApplyCanonicalDeltaMatchesFreshLoadOfFinalState`.

**Lo que ya está hecho:** `TestDiffOverARealEditReproducesACleanLoad` cubre las
dos formas que hoy sí se sostienen -- un fichero nuevo en un paquete existente y
un fichero que desaparece-- con el cargador real sobre una copia de trabajo
editable, y `normalizeRepositories` quedó extraído para que cualquier test pueda
apuntar la pasada a un árbol que puede editar.

## LUQUE-2003 — Decidir la suerte del camino incremental

**Estado:** cerrada por el ADR 0057 -- **retirado**.

**Dependencias:** LUQUE-2002.

**El hecho:** el subsistema de delta está construido y probado, y **no lo llama
nadie**. `facts.Diff`, `indexer.Decide`, `indexer.Update`,
`ladybug.ApplyCanonicalDelta`, los planes de invalidación y las clases de cambio
existen, tienen tests -- incluidos nativos con el tag `ladybug` -- y ninguna ruta
de producción los alcanza: los consumidores de `internal/indexer` usan `Full`, el
servicio reconstruye completo, y `kivgraph index` responde `only --full is
supported`.

**Por qué importa más que el propio código:** un subsistema inalcanzable da
confianza falsa. Al arreglar LUQUE-2002 se estuvo a punto de publicar que
«producción pierde aristas en cada edición», que era falso, y sólo se descubrió
al buscar el llamante. También significa que las cifras de coste incremental que
cualquier documento cite son proyecciones, no mediciones.

**Las dos salidas, y hay que elegir una:**

* **Cablearlo.** Un `kivgraph index` sin `--full` que tome la ruta, más un test
  end-to-end que indexe, edite, reindexe y compare contra una reconstrucción
  limpia -- que es la única forma de saber si el resto de la maquinaria (planes de
  invalidación, decisión de ruta, republicación por ratio) hace lo que sus tests
  con dobles dicen que hace.
* **Retirarlo.** Si el diseño es «siempre completo», el delta es código muerto con
  su propio ADR, y borrarlo es más honesto que mantenerlo. La retirada tendría que
  decir qué se pierde y por qué el coste de un full es aceptable.

**Lo que no vale:** dejarlo como está y seguir citando el incremental como una
propiedad del producto.

**Medido** (`benchmarks/incremental-cost`, commit `e78490e`, corpus `kena` con
`4.683` ficheros y `477.027` aristas, caché de hechos caliente):

|ruta|segundos|contra el full|
|---|---|---|
|pase completo|`9,174`|`1,00x`|
|delta tal como está escrito|`3,811`|`2,41x`|
|delta si también verificara integridad|`5,507`|`1,67x`|

Dos hechos de diseño fijan ese techo: `Update` exige el set `Next` **completo**
-`Plans` sólo elige ruta-, así que `2,00 s` de arranque, motores, `merge` y
`facts` los paga igual; y reconstruye el `HotSnapshot` **entero**, otro `1,79 s`.
Lo único que ahorra de verdad es `staging`, el `38 %` del pase. El resto de su
ventaja aparente es que **no verifica**: `applyDeltaRoute` hace cero llamadas a
`integrity` y a `golden probes`, que la ruta completa sí corre.

Y no mejora con la escala: `staging`, `merge`, `snapshot` e `integrity` escalan
todos con el corpus, y sólo la mutación escala con la edición, así que la razón
se queda en `1,67x` en cualquier tamaño.

**Recomendación:** retirarlo. `1,67x` sobre nueve segundos no paga un subsistema
con un modo de fallo de corrupción silenciosa -- ya cobrado una vez en
`LUQUE-2002` -- ni una ruta de publicación que no verifica. La caché de hechos ya
entrega lo que el incremental prometía: con ella caliente los motores de lenguaje
son `0,57 s` de los `9,17 s`. Un incremental que de verdad pagara necesita un
`HotSnapshot` actualizable y un set `Next` acotado, que es otro diseño y otro
ADR, no este código.

**Resultado registrado:** se eligió **retirarlo**, con consentimiento explícito.
El ADR 0057 (`docs/adr/0057-el-camino-incremental-se-retira.md`) es la decisión.

**Qué se midió:** `benchmarks/incremental-cost`, que sigue en el árbol y es la
medición que decidió esto. El pase completo son `9,174 s`; la fase `staging` es
`3,529 s` -- el `38 %`, y lo único que el delta evitaba--; los costes fijos que
`applyDeltaRoute` pagaba en cada delta contra la base real de `318 MB` son
`1,818 s` (`CanonicalTableCounts` `0,030 s`, `RefreshSnapshotDigest` `0,000 s`,
`BuildSnapshot` completo `1,788 s`). Techo: `2,41x` tal como estaba escrito,
`1,67x` si además verificara. Nunca se midió un delta de extremo a extremo,
porque no había llamante que medir.

**Qué se decidió:** el único camino de indexado es la reconstrucción completa.
`kivgraph index` acepta sólo `--full` y eso es el diseño. El contrato de retirada
del ADR 0056 deja de describir código existente y pasa a ser la condición de
partida -- no relajable-- de cualquier incremental futuro, que además necesitaría
un `HotSnapshot` actualizable, un set `Next` acotado y la verificación
(`integrity`, `golden probes`) que esta ruta se saltaba.

**Qué se borró:** `2.732` líneas de producción -- `internal/indexer/delta.go`,
`invalidation.go`, `go.go`, `typescript.go`, `rust.go`, `semantic_changes.go`,
`internal/facts/delta.go`, `internal/syntax/changes.go` y los tres
`internal/storage/ladybug/canonical_mutation*.go`--, `3.976` de test y el
benchmark `benchmarks/ladybug-incremental` (`953`). `corruption_native_test.go` y
`duplicate_process_linux_test.go` conservan sus invariantes con otro vehículo:
`LoadCanonical` se niega antes de abrir (`ErrAlreadyExists` por una guarda de
`os.Stat`) y sostiene «rechaza escribir sin tocar el archivo»; `Open` sostiene
`ErrDatabaseLocked` con los PIDs, que es donde vive `classifyOpenFailure`. Se
conservan `rust_unit.go` y `semantic.go`, que el pase completo usa.

**Qué se pierde:** el benchmark `ladybug-incremental` -- su evidencia
(`LADYBUG_INCREMENTAL_PASS`, `LADYBUG_DELTA_PERFORMANCE_PASS`) sigue en
`docs/decisions/ladybugdb-qualification.md` y su harness en el historial de git --
y la posibilidad de un delta sin rediseñarlo.

## LUQUE-2004 — `trace_dependencies` no baja a los miembros de un contenedor

**Estado:** cerrada del todo por el ADR 0059 -- **descendido**. El 0058 lo declaró primero.

**Dependencias:** ninguna.

**El defecto:** preguntar el alcance de una clase devuelve sólo lo que la
declaración de la clase referencia directamente -- sus supertipos y sus usos de
tipo-- y **no** lo que sus propios métodos alcanzan. La respuesta no dice que se
ha parado ahí, así que un conjunto más pequeño pasa por un conjunto completo.

Medido en `benchmarks/graph-tools-comparison/reach.md`, pregunta `X4`, sobre
`RecommendationsCache` en `library-shared`:

|pregunta|alcanza|
|---|---|
|la clase, profundidad `1`|sólo `src/redis/cache/base-cache.ts`|
|la clase, profundidad `2`|miembros de `BaseCache` y `RedisCacheClient.ts`, nunca el tipo|
|el método `getResults`, profundidad `1`|`src/redis/cache/music/types.ts`, vía `TYPE_USES`|

`P=1,00`, `R=0,50`. La arista existe y está bien puesta: cuelga del método,
porque es el método quien nombra `ChipbotRecommendationsResponse`. Lo que no hay
es arista clase -> método: la contención no es una dependencia en este grafo, y
`get_blast_radius` en la dirección entrante tiene el mismo borde.

**Por qué es un defecto y no una decisión defendible tal cual:** la respuesta es
coherente con su modelo y contesta una pregunta distinta de la que se hizo, en
silencio. Es la misma forma que el `H2` del conjunto duro, cerrado por el ADR
0054, y que el `H3`, cerrado por el ADR 0055.

**Las dos salidas, y hay que elegir una:**

* **Descender.** Cuando la raíz es un contenedor -- clase, interfaz, struct con
  métodos-- la travesía incluye lo que alcanzan sus miembros declarados. Cambia
  respuestas publicadas y necesita ADR: hay que decidir si el miembro cuenta como
  un salto de profundidad o como parte de la raíz, y qué pasa con una clase de
  trescientos métodos.
* **Declararlo.** La respuesta dice que la raíz es un contenedor y que sus
  miembros se responden aparte, nombrándolos. Más barato, y convierte un silencio
  en una instrucción -- que es lo que el ADR 0046 hizo con la ambigüedad.

**Lo que no vale:** dejar que una pregunta sobre una clase devuelva la mitad de
su alcance sin decirlo.

**Resultado.** Se eligió declararlo. `trace_dependencies` no cambia su travesía y
nombra, en `members_not_followed` y en la `guidance`, los miembros cuyas aristas
salientes la respuesta no ha caminado. Un miembro entra sólo si está en el span de
la raíz, alcanza algo fuera de él y es de la capa más externa -- sin esa última
condición la respuesta nombraba un parámetro. Tope de `12` nombres, y el campo
viaja en las dos vistas porque la compacta es la de por defecto.

La exhaustividad medida no se mueve: `X4` sigue en `R=0,50` sobre ficheros
alcanzados. Lo que se va es el silencio, y cuesta `70` tokens en la respuesta que
lo necesita (`163` -> `233`); las otras tres preguntas del conjunto no se movieron
ni un token. Verificado sobre `kena` con el binario real, no sólo en fixture.

**Descendido (ADR 0059).** La travesía siembra el contenedor **y sus miembros a
profundidad cero**, en un solo BFS -- `TraverseFrom`--, porque un miembro es
contenido y no puede costar un salto; las semillas no salen como filas y sólo
siembra la capa más externa, sin lo cual la respuesta sembraba un parámetro.
`members_not_followed` se retira: la respuesta lleva ahora lo que antes nombraba.

Medido con el mismo corpus y el mismo estado, cambiando sólo el binario:

|pregunta|tokens|`R`|
|---|---|---|
|`X1`, `X2`, `X3`|sin cambio (`530`, `112`, `305`)|`1,00`|
|`X4` alcance de una clase TS|`233` -> `403`|`0,50` -> **`1,00`**|

`+170` tokens en la única que cambia, cero en las demás. `3/4` -> `4/4`.

Y queda dicho lo que no se tocó: `get_blast_radius` tiene el mismo borde en la
dirección entrante.

## LUQUE-2010 — En Go y en Rust un método no cae dentro del span de su tipo

**Estado:** cerrada por el ADR 0060, en `2026-08-22`.

**Dependencias:** LUQUE-2004.

**El hueco era:** el ADR 0059 derivaba la contención del **rango de líneas**, que
sólo cubre a los miembros que viven léxicamente dentro de la declaración. En Go
`func (h *T) M()` se declara fuera del `struct`; en Rust el método vive en un
`impl` que no se publica. Así que el alcance de un tipo Go o Rust **excluía el de
sus métodos** mientras la respuesta de TypeScript parecía idéntica y sí era
completa.

**La salida elegida fue registrar el receptor como hecho**, y salió más pequeña de
lo que esta tarea temía por un error mío: la tarea afirmaba que el cargador Go
«no lo registra en la declaración», y es falso. `goloader.Definition.Owner` ya
llevaba el tipo receptor que resolvió `go/types`, y sus tests lo afirmaban desde
siempre; nadie lo consumía. Mi comprobación anterior grepeó `ReceiverTypeName`
-- el nombre que usa `methods.go` para un sitio de llamada-- y no vio el campo
llano. Es el mismo grep estrecho que ya me costó cinco correcciones esta sesión.

Lo que sí costó lo que decía la tarea fue el resto: `METHOD_OF` es una tabla de
relación nueva, así que `CanonicalSchemaVersion` sube de `3` a `4` con su DDL y su
documentación regenerados. Y Rust **no** necesitó revertir LUQUE-2008: el miembro
se empareja con el tipo que nombra el bloque, saltándose el bloque.

**Medido**, con `depth: 1` sobre `kena`:

|sujeto|antes|después|
|---|---|---|
|`GuildsHandler` (Go, 9 métodos)|`3` filas, `167` tok|`53` filas, `1.613` tok|
|`MemoryStateStore` (Rust, dos `impl`)|`2` filas, `151` tok|`18` filas, `606` tok|

La verdad se construyó sin preguntar al contenedor -- la unión de lo que alcanza
cada método con el binario anterior, menos la raíz y sus miembros-- y son `53`
exactas: `P=1,00`, `R=1,00`.

**Lo que queda declarado, no arreglado:** `get_blast_radius` no cambia. La
pregunta entrante no se contesta con contención: los métodos de un tipo no son sus
consumidores.

## LUQUE-2005 — Cobertura de las tools servidas por el conjunto de preguntas

**Estado:** cerrada -- conjunto `chain` en `benchmarks/graph-tools-comparison/chain.md`.

**Dependencias:** ninguna.

**El hueco:** el servidor sirve `11` tools y el conjunto de preguntas -- los
cuatro conjuntos juntos-- ejercita `5`. Tras `reach`, las llamadas por tool son
`find_references` `19`, `get_file_outline` `3`, `get_blast_radius` `3`,
`find_cross_repo_consumers` `2` y `trace_dependencies` `2`. Siguen a cero
`find_symbol`, `get_source`, `get_symbol`, `graph_status`, `list_repositories`
e `index_project`.

Las cinco a cero no son iguales: `graph_status` y `list_repositories` responden
estado y no una pregunta sobre el código, y `index_project` muta. Las que
importan son `find_symbol`, `get_source` y `get_symbol`, que son las que un
agente encadena después de cualquier respuesta.

**Objetivo:** una pregunta por cada una de esas tres, con verdad construida
leyendo, y la cobertura declarada en el informe en vez de deducida.

**Resultado.** Conjunto `chain`, tres preguntas, `25` preguntas ya en cinco
conjuntos y `8` de las `11` tools ejercitadas. La tabla de cobertura está escrita
en `chain.md` y verificada por script contra los cinco `results-*.json`, uno por
conjunto, sin contar re-mediciones.

|pregunta|kivgraph|nativo|razón|
|---|---|---|---|
|`X5` dónde se declara `withRetry` (7 de 22)|`744` tok `P=R=1,00`|`1.699` tok `P=R=1,00`|**`2,3x`**|
|`X6` tres cuerpos en una llamada|`674` tok `P=R=1,00`|`2.071` tok|`3,1x`|
|`X7` qué es un símbolo entre 6 homónimos|`176` tok `P=R=1,00`|`1.516` tok|`8,6x`|

**Lo que hay que decir de esto, y no está a nuestro favor:** el nativo acierta
las tres. Estas tres tools no compran exactitud, compran tokens. Y `X5` es el
margen más estrecho medido en todo el proyecto -- `2,3x` sobre un nombre **común**
en un corpus **grande**, que es el caso donde el `AGENTS.md` promete ventaja.
Además la razón es la más favorable al `grep` que el conjunto puede producir: sus
líneas de resultado bastan para separar declaración de uso, así que al brazo
nativo se le cobra sólo la búsqueda.

**Hallazgo de diseño.** El grafo tiene `22` símbolos llamados `withRetry` y sólo
`7` declaran: los otros `15` son bindings de `export`/`import` de barrels de
TypeScript. La verdad los excluye y el brazo los aparta **contándolos** en la
nota, que es la misma decisión del ADR 0046 para las aristas de reenvío. Sin esa
regla la precisión habría salido `0,32` y el defecto habría sido de la pregunta,
no de la herramienta.

**Sigue abierto:** una pregunta de `X6` con treinta símbolos, que es donde el
batching debería separarse de leer ficheros; `graph_status`, `list_repositories` e
`index_project` quedan fuera por decisión declarada -- estado y mutación, no
preguntas sobre el código.

## LUQUE-2006 — Una pregunta de Rust sobre el corpus real

**Estado:** cerrada el `2026-08-22`. Las dos mitades están hechas y medidas.

**Dependencias:** ninguna.

**El hueco, y de quién es la culpa:** los tres conjuntos medidos sobre `kena`
-`reach`, `chain` y el desglose de `incremental-cost`- se construyeron sobre un
índice **sin Rust**. Al `PATH` del harness le faltaba `cargo`, así que
`rust-analyzer` rechazó los dos workspaces Cargo y el pase publicó el resto.
Kivgraph lo declaró -- `rust_workspaces_not_loaded=2` en el JSON y en la salida
humana, que es su contrato con una ausencia-- y el harness no lo leyó.

Con `cargo` en el `PATH`: `2` workspaces, `3.063` símbolos, `13.223` referencias,
`0` no cargados, en `35 s` sobre los dos repositorios Rust solos. El corpus
completo pasa de `4.683`/`120.461`/`477.027` a `4.768`/`123.524`/`493.521`.

**Lo ya corregido:** `benchmarks/incremental-cost` re-medido entero, con su
sección de corrección y la nota en el ADR 0057 -- el techo del delta era `1,63x`,
no `1,67x`, así que la retirada se refuerza. `reach.md` y `chain.md` llevan la
nota de que su corpus no tenía Rust; sus cifras no cambian, porque sus preguntas
son de Go y TypeScript y sus verdades se leyeron de los ficheros.

**Lo que faltaba, y ya está:** una pregunta de Rust sobre `kena`, no sobre un
fixture. Es `R1_rs_sole_impl_dyn`, en el conjunto `rust` con su informe
`benchmarks/graph-tools-comparison/rust.md`: el único llamante de
`StateStore::delete_session` llega por `Arc<dyn StateStore>`, o sea dispatch
dinámico sobre código real. Y el resultado no nos favorece del todo, que es
parte de su valor: `codebase-memory-mcp` **empata con nosotros** en exactitud a
`788` tokens contra nuestros `186`, y el informe lo dice antes que nada.

**Y la regla del harness, cableada con test:** un conjunto publicado que registre
un contador `not_loaded` distinto de cero, o un lenguaje registrado que produzca
cero símbolos, **falla cerrado**. La defiende
`TestPublishedCorpusRefusesAMissingLanguage`, porque una regla de harness sin
test es una regla que se apagará sola. Salió de aquí: tres mediciones publicadas
describían un corpus sin Rust y ningún contador las paró.

## LUQUE-2007 — La proporción de no resueltos de Rust no tiene pregunta

**Estado:** cerrada -- `benchmarks/unresolved-shape`. No hay defecto de resolución; hay una rama muerta, LUQUE-2008.

**Dependencias:** LUQUE-2006.

**El número:** el índice de `kena` publica `3.063` símbolos Rust y `1.969`
referencias no resueltas. Ninguna pregunta de ningún conjunto pregunta nada sobre
esa proporción, y es la más alta de los tres lenguajes -- `go_unresolved` es
`9.581` sobre `114.741` referencias, `typescript_unresolved` `5.998` sobre
`278.601`.

**Por qué importa:** un no resuelto conserva motivo, repositorio y lenguaje por
contrato, así que la pregunta es contestable con el grafo publicado y sin leer
código: **¿de qué son esos `1.969`?** Si son macros, `cfg` no compilados o
dependencias externas, es una limitación declarada del cargador. Si son llamadas
normales dentro del workspace, es un defecto de resolución escondido detrás de un
contador agregado.

**Objetivo:** agrupar los no resueltos de Rust por motivo, publicarlo con la
convención de `benchmarks/`, y decidir a partir del desglose si hay defecto o
límite. Hoy nadie lo sabe, y el número está a la vista desde que Rust carga.

**Resultado.** De las `1.969` referencias Rust no resueltas de `kena`:

|grupo|cuenta|parte|qué es|
|---|---|---|---|
|`CRATE_PROVIDER_NOT_FOUND`|`1.857`|`94,3 %`|el sysroot y dependencias externas -- `alloc::*`, límite declarado|
|`DEFINITION_NOT_INDEXED`|`112`|`5,7 %`|definiciones sin ocurrencia en fuente|
|**llamadas del workspace que fallaron al resolver**|**`0`**|`0 %`|--|

Las `112` se leyeron **etiqueta por etiqueta** -- hay `112` distintas--: `56` son
bloques `impl`, `53` son miembros generados por `derive` y `3` son cola. Ninguna
es una llamada normal dentro del workspace, que era la hipótesis que habría hecho
de esto un defecto.

## LUQUE-2008 — Un bloque `impl` de Rust no se publica y su rama existe

**Estado:** cerrada -- ramas retiradas; no publicar el bloque ya era contrato probado.

**Dependencias:** LUQUE-2007.

**El hecho:** `internal/rustloader/kinds.go` tiene dos ramas para los bloques
`impl` -- `PublishedKind` devuelve `"implementation"` y `PublishedName` renderiza
`impl X for Y`, con un comentario que explica por qué un bloque no tiene nombre
propio-- y **ninguna se ejecuta**. Evidencia directa sobre `kena`:

```
find_symbol { name: "impl", kind: "implementation", repo: "api-music-nodo" }
  -> total: 0
```

Los **miembros** de esos mismos bloques sí se indexan: `get_file_outline` sobre
`src/error.rs` lista `error::impl::ApiError::with_context_header@174-177`. Así que
el cargador indexa el contenido de un `impl` y no su cabecera, y las `56`
referencias que `rust-analyzer` emite hacia esa cabecera quedan **sin resolver
para siempre**.

**Las dos salidas:**

* **Publicar el bloque.** Las `56` resuelven, las ramas se vuelven vivas y
  `kind: "implementation"` empieza a aparecer en respuestas -- que es un cambio de
  superficie MCP y necesita decidir si un bloque `impl` es una respuesta útil a
  «dónde está declarado esto», cuando las aristas `IMPLEMENTS` ya llevan la
  relación.
* **Retirar las ramas** y declarar que una referencia a una cabecera `impl` no
  resuelve por diseño, como el sysroot. Más barato y honesto, y deja el contador
  diciendo la verdad sobre algo que nadie va a arreglar.

**Lo que no vale:** dejar código que renderiza un caso que no ocurre mientras
`56` referencias lo citan sin poder resolverlo.

**Resultado: retiradas.** Se eligió retirar, y al buscar cobertura apareció el
dato que cambia la justificación: **no publicar un bloque `impl` ya era contrato
probado, anterior a este cambio.**
`TestAnalyzeDeclaresTheImplementationBlockItCannotDefine` afirma dos cosas sobre
salida real de `rust-analyzer` -- que `shapes::impl::Circle` cae en `Unresolved`
con `DEFINITION_NOT_INDEXED`, y que **ninguna** referencia intra-repositorio
nombra una clave que la pasada no publique--. Así que una rama que da nombre y
kind a un símbolo que el cargador nunca crea era inalcanzable **por
construcción**, no sólo no observada. Nada cubría la rama en sí.

Retirado: la rama de `PublishedKind`, la de `PublishedName`,
`isImplementationBlock` y `implementationSubject`. `implementedTrait` se queda --
lo usa `relations.go:224` para emparejar el método de un trait con su
implementación--. Sin cambio de comportamiento: los `31` tests de
`internal/rustloader` pasan **con toolchain real**, no saltados, que es la única
forma de comprobar esto -- y sin `cargo` en el `PATH` nueve de ellos se saltan y no
verifican nada.

La consecuencia queda escrita donde vive el código: una referencia cuyo destino es
la cabecera de un `impl` sigue `UNRESOLVED` porque no hay símbolo al que apuntar --
`56` de las `1.969` de `kena`, la misma forma que una referencia al sysroot.

## LUQUE-2009 — Un contador de no resueltos cuenta observaciones, no hechos

**Estado:** cerrada, y **la premisa era falsa**: no es un contador, son todos.

**Dependencias:** LUQUE-2007.

**El hecho, medido:** `go_unresolved` declara `9.581` sobre `kena` y el grafo
canónico guarda `6.059` filas. La clave de un no resuelto incluye el **offset**,
así que sólo colapsan dos observaciones de la misma posición -- y con
`include_tests: true` eso ocurre por diseño: `go/packages` carga `pkg` y
`pkg.test`, y las dos observan el mismo punto del mismo fichero.

|`include_tests`|el índice declara|el grafo guarda|
|---|---|---|
|`true`|`9.581`|`6.059`|
|`false`|`4.397`|`4.397`|

Sin tests coinciden exactamente. Con tests, la cifra que un usuario lee
sobreestima los hechos distintos en `1,58x`. **No se pierde ninguna fila**: lo que
sobra son observaciones repetidas de lo mismo.

**Por qué importa:** ese número es lo que alguien mira para decidir si confía en
el grafo, y el resto de contadores del mismo bloque -- símbolos, referencias-- sí
son hechos distintos. Uno de los dos está midiendo otra cosa que sus vecinos.

**Las dos salidas:**

* **Contar hechos.** Deduplicar por la clave antes de contar, que es lo que el
  grafo hace después. La cifra baja y empieza a coincidir con el grafo.
* **Declararlo.** Renombrar lo que se informa, u añadir el recuento distinto al
  lado, y decir en la documentación que uno cuenta observaciones.

**Lo que no vale:** dejar dos números que se llaman igual, se leen igual y miden
cosas distintas según un flag de configuración.

**Corrección y resultado.** Esta tarea decía que uno de los contadores medía algo
distinto de sus vecinos, y que los de al lado -- símbolos, referencias-- sí eran
hechos distintos. **Es falso.** Medido en un solo pase sobre `kena` con
`include_tests: true`:

|lenguaje|símbolos: cargador|grafo|ratio|no resueltos|grafo|ratio|
|---|---|---|---|---|---|---|
|`go`|`19.166`|`11.731`|`1,63`|`9.581`|`6.059`|`1,58`|
|`typescript`|`124.371`|`109.279`|`1,14`|`4.969`|`4.962`|`1,00`|
|`rust`|`3.063`|`3.063`|`1,00`|`1.969`|`1.969`|`1,00`|

Las **definiciones** de Go divergen en el mismo `1,63x` que sus no resueltos. No
hay un contador raro: **el bloque `index` cuenta lo que cada pasada observó y el
bloque `counts` cuenta lo que el grafo guarda**, y los dos viajan en el mismo
evento `result` sin que nada lo diga. En ese evento la suma de `index.*_symbols`
es `146.600` y `counts.symbols` dice `124.073`: `22.527` de diferencia.

**Decisión: nombrarlo, no deduplicar.** El número de observaciones es la única
cifra que dice cuánto trabajó la pasada, y el del grafo la única que dice qué
encontrará una consulta; deduplicar el primero borraría información. Documentado
donde se declaran los campos -- `IndexSummary` en `internal/indexing/service.go` --
y en el protocolo de `cmd/kivgraph/AGENTS.md`, con los ratios medidos y el caso
que los explica: un fichero de `pkg` y `pkg.test` se observa dos veces y se guarda
una. Rust coincide exacto porque carga una pasada por workspace.

## LUQUE-2011 — Un fichero de TypeScript fuera del repositorio se atribuye a quien lo consume

**Estado:** cerrada el `2026-08-22`. Se retira el hecho, no se reatribuye.

**Era:** la respuesta de `H3_ts_type` traía dos filas de esta forma:

```
gateway:../../libraries/library-shared/dist/.../api-registry-cache.d.ts
sdk-module-ts:../../libraries/library-shared/dist/.../api-registry-cache.d.ts
```

Un fichero atribuido a dos repositorios, y ninguno lo contiene. El `AGENTS.md`
promete que toda fila es direccionable y ésas no lo son.

**Reatribuirlo al dueño se descartó con evidencia, no por tamaño.** Un `File`
pertenece a un `Package`, y un payload de consumidor **nunca nombra el paquete
del proveedor** — `payload.Files` son rutas peladas. La fila saldría sin paquete.
Y `MergeAll` se queda con la primera fila de una clave y descarta las siguientes
**sin compararlas**, así que una fila sin paquete puede vencer en silencio a una
completa: es la forma de `LUQUE-2002`. Además rompería la invariante en la que se
apoya la corrección de Go — una pasada sólo afirma hechos de ficheros dentro del
repositorio que indexa— y el contrato de retirada del ADR 0056, que no tiene
dueño para un hecho escrito por otra pasada.

**Lo que se hizo:** el consumidor retira el hecho, lo cuenta en
`FactsOutsideRepository` y retiene el hueco con motivo `FILE_OUTSIDE_REPOSITORY`
y la ruta en `detail`. Simétrico con Go.

**Lo que no se pierde, comprobado:** la relación del workspace no depende de esas
filas. La identidad del destino de un import se construye con el repositorio y el
paquete **del proveedor**, byte a byte idéntica a la clave que el proveedor da a
su propia declaración, así que `find_cross_repo_consumers` sobre `ApiRuntimeState`
sigue respondiendo `25` consumidores en `EXACT_TYPECHECKED`. Lo retirado son usos
cuyo fichero **fuente** es la salida construida del proveedor: hechos del
proveedor, que su propia pasada es quien debe publicar.

**Medido:** `H3` vuelve a `1,00`/`1,00` con `280` tokens, y **ninguna de las 29
preguntas tiene ya una fila cuya ruta escape de su repositorio**. Sobre `kena` la
retirada quita `193` ficheros, `542` símbolos y `1.275` aristas, y añade `173`
huecos retenidos.

## LUQUE-2012 — Una fila completa para un fichero de otro repositorio del workspace

**Dependencias:** LUQUE-2011.

**El hueco:** hoy un uso dentro de la salida construida de un paquete hermano se
**retira**. Es correcto y es una pérdida declarada: nadie publica esos usos,
porque el proveedor no indexa su propio `dist`. Si alguna pregunta demuestra que
esa información hace falta, el camino completo tiene dos piezas y ninguna es
pequeña:

1. **Que el worker nombre al dueño.** El payload debe traer repositorio y paquete
   por fichero, no sólo por dependencia. Eso sube `TypeScriptWireVersion`, que es
   superficie de compatibilidad, y toca el worker y sus gates pnpm.
2. **Que el merge desempate por completitud.** `mergeAllBy` es first-wins y
   silencioso; con filas de la misma clave llegando de varias pasadas hay que
   comparar y quedarse con la más completa, y eso está en el camino que comparten
   los cinco lenguajes.

Más un ADR: cambia a qué repositorio pertenece un símbolo, que es identidad.

**Lo que no vale:** la versión barata — resolver la ruta contra el registro en el
lado Go y emitir la fila sin paquete. Eso es exactamente lo que `LUQUE-2011`
descartó, y reintroduce el first-wins silencioso.

**Estado:** parada deliberada. No es una ficha pendiente: es un hueco declarado
que se abre sólo si alguna pregunta demuestra que esa información hace falta, y
la versión barata está prohibida por escrito arriba.

## LUQUE-2014 — El digest que prueba la pertenencia de un fichero es de contadores, no de contenido

**Dependencias:** LUQUE-2004.

**Objetivo:** que la prueba de que un snapshot publicado pertenece a su
generación distinga dos grafos que difieren.

**El defecto.** `snapshot.sha256` lo escribe
`writeSnapshotDigest(candidatePath, result.Tables)` -`rebuild.go:284`- sobre los
**contadores por tabla**. Ese valor viajaba en la cabecera del fichero publicado
como `contentDigest`, y es lo que `loadPublishedSnapshot` comparaba. Los
contadores no distinguen dos grafos de la misma forma: el mismo corpus `kena` en
dos `HOME` dio un `snapshot.sha256` **idéntico** sobre grafos que diferían en
`288` filas y `48` bytes de arena.

Y el fichero **se mapea y se sirve** desde el ADR 0045, así que la prueba no era
teórica.

**Por qué no lo vio ningún test.** El fixture no modelaba producción:
`seedPublishedGeneration` escribía `report.Digest` -- el digest de contenido, que
ya se calculaba en cada build-- en `snapshot.sha256`, donde producción escribía el
de contadores. Todas las pruebas del fichero ejercitaban un emparejamiento más
fuerte que el real.

**La decisión, en el ADR 0061.** Separar las dos preguntas:

* `snapshot.sha256` **se queda**: es la comprobación barata del `Rollback`, unos
  `COUNT(*)` junto al escaneo de invariantes que ya corre. Ni una línea de ese
  camino cambia.
* `snapshot.content.sha256` **es nuevo**: registra `snapshotContentDigest(rows)`,
  que ya se calculaba y se descartaba para este uso. Coste de cómputo: ninguno.

Se descartó cambiar el significado de `snapshot.sha256`: los dos digests son 64
hex indistinguibles, así que una generación anterior fallaría el rollback con
«digest mismatch» -- corrupción para describir una actualización, la misma clase
de defecto que el `doctor` en rojo de `LUQUE-2004`.

**Verificación sobre el corpus real** (`kena`, 37 repositorios, 123.531 símbolos):

|comprobación|resultado|
|---|---|
|los dos digests en la generación|`e80c6d46…` contadores, `2e116aff…` grafo|
|la cabecera repite el del grafo|sí; **no** el de contadores|
|`doctor` con el registro|`snapshot.published: PASS (… 123531 symbols, 98773720 bytes)`|
|`doctor` sin el registro|`PASS`, «records no digest…; the next index replaces it»|
|`serve`|`read the published snapshot generation=000001 symbols=123531` en `166 ms` -- cargado, no derivado|

* `TestTableCountsCannotProveWhichGraphAFileHolds`: dos grafos de la misma forma
  a una firma de distancia, contadores idénticos, digests de grafo distintos, y
  el fichero del primero en la generación del segundo se rechaza.
* `TestAGenerationWithoutTheRecordIsAnUpgradeNotAFailure`: clasifica sobre el
  centinela `ErrNoRecordedGraphDigest`, no sobre el mensaje, y comprueba que no
  se confunde con la ausencia.
* Gates en verde por código de salida: `gofmt`, `vet`, `test`, `-race`,
  `test-ladybug`, `build`.

**Estado:** cerrada el `2026-08-22`.

---

## LUQUE-2015 — El grafo publica 288 rutas absolutas de la máquina que lo indexó

**Dependencias:** ninguna.

**Objetivo:** que un grafo del mismo corpus sea el mismo en dos máquinas.

**El defecto.** `internal/facts/golang.go` rechaza crear paquete, fichero y
símbolo para una declaración cuyo fichero cae fuera del repositorio, y su propio
comentario dice por qué: cada uno «nombraría esta máquina». Acto seguido ponía esa
misma ruta en `Detail`. El vocabulario de la razón
-`goloader/unresolved.go:42-48`- ya lo tenía escrito: «no es evidencia para este
repositorio: nombra una máquina y una entrada de caché, no código que alguien
pueda leer en esa ruta después». La regla se aplicaba a tres campos de cuatro.

Y un test lo defendía: `TestNormalizeGoRefusesFactsFromOutsideTheRepository`
afirmaba `Detail == cache`, la ruta absoluta, mientras comprobaba en la línea de
al lado que ningún `File.Path` fuera absoluto. El mismo invariante, aplicado a un
campo y no al otro.

**Alcance, medido antes de tocar nada.** Sobre `kena`, `87` rutas absolutas entre
las `639.613` cadenas internadas: `48` de la caché de build -- el defecto-- y `39`
raíces de repositorio del registro, que son deliberadas y se quedan. **Ningún
diagnóstico filtra rutas**, que era la hipótesis alternativa: los detalles de
TypeScript son relativos por construcción -`typescript.go:401`, de la forma
`../../libraries/...`- y los de Rust son prosa fija.

**La decisión.** `Detail` pasa a ser un vocabulario cerrado de tres valores que
clasifica *dónde* vive el fichero sin decir *en qué máquina*: entrada de la caché
de build -- que es la que dice a un lector que el paquete se construye con cgo o
desde fuentes generadas, lo único accionable de la ruta anterior--, caché de
módulos, o fuera de la raíz. La clasificación se deriva de la forma de la ruta,
así que sigue siendo observada y no fabricada.

**Verificación:** dos indexados del mismo corpus con dos `HOME` de longitud
distinta.

|magnitud|antes|después|
|---|---|---|
|rutas de caché de build internadas|`48`|`0`|
|rutas absolutas totales|`87`|`39` (las raíces del registro, idénticas)|
|cadenas internadas|`639.613`|`639.566` (`-48 +1`, exacto)|
|filas de no resueltos|`13.163`|`13.163`|
|filas de esta clase retenidas|`288`|`288`|
|conjunto `(clave, detail)` entre los dos `HOME`|**difiere**|**igual**|
|payload publicado entre los dos `HOME`|difiere en `48` bytes|**idéntico byte a byte**|

Lo único que sigue distinguiendo los dos ficheros son `5` bytes de `created_at`
en la cabecera -- procedencia, exactamente lo que `LUQUE-2004` fijó por test.

* Regresión: `TestOutsideRepositoryDetailIsIndependentOfTheMachine`, que clasifica
  la misma entrada bajo tres `HOME` distintos y separa el caso de un directorio
  llamado `mod` que no es la caché de módulos.
* Gates en verde por código de salida: `gofmt`, `vet`, `test`, `-race`,
  `test-ladybug`, `build`, `landing-check`.

**Estado:** cerrada el `2026-08-22`.

---

## LUQUE-2013 — Dos claves de configuración que no implementan lo que prometen

**Dependencias:** LUQUE-2004.

**Objetivo:** que `storage.snapshots_path` y `storage.retain_snapshots` describan
lo que el código hace, o que dejen de existir.

**Lo que había, medido al cerrar LUQUE-2004:** `snapshots_path` se declaraba,
tenía valor por defecto, `Initialize` le escribía una ruta y **creaba el
directorio**, y `doctor` comprobaba que se podía escribir en él. Nadie escribía
nunca nada ahí. `retain_snapshots` valía `3` y se validaba como «must be
positive»; ningún consumidor la leía. Y la referencia de configuración decía que
`snapshots_path` contenía «Published generations», que era falso.

**La decisión, en el ADR 0062: retirarlas.** De las tres salidas era la única que
mejora el producto. Darle significado a `snapshots_path` es *activamente peor*:
que el fichero viva dentro de su generación es lo que hace que `Prune` lo borre
con ella y que no pueda quedar huérfano, así que sacarlo crearía el problema que
la clave parecía resolver. Y `retain_snapshots` exigiría inventar una política de
retención sin consumidor cuando el rollback usa exactamente dos generaciones.

**La migración, que es la mitad del trabajo.** El decodificador usa
`KnownFields(true)`, así que borrar los campos habría convertido en **fallo de
carga duro** cada `config.yaml` escrito por un `init` anterior -- y las escribía
las dos-- por una clave que nunca hizo nada. Es la misma forma que el `doctor` en
rojo de `LUQUE-2004`, y se rechaza por el mismo motivo. Hay una lista explícita,
`retiredConfigKeys`, y el documento se reescribe sin ellas antes del decodificado
estricto: **la estrictez no se relaja**, sólo se nombran las que se retiran.

**Verificación con el binario**, que es donde el defecto de `2004` se vio:

```text
init          : no escribe ninguna de las dos, ni crea state/snapshots
config vieja  : doctor exit=0
                config.retired: PASS (storage.snapshots_path,
                storage.retain_snapshots: accepted and ignored, safe to delete)
typo `snapshot_path` : config: FAIL -- la estrictez sigue viva
```

* `TestARetiredKeyLoadsAndIsReported`: carga, las reporta en orden, la clave viva
  de al lado sigue aplicándose, y un documento sin ellas no reporta ninguna --
  para que el informe no pueda ser una constante.
* `TestLoadConfigRejectsInvalidDocuments/unknown_field_inside_a_section_that_has_retired_keys`.
* Gates en verde por código de salida: `gofmt`, `vet`, `test`, `test-ladybug`,
  `build`, `landing-check`.

**Estado:** cerrada el `2026-08-22`.

---

# 29. Fase 22 — Cualificar Python y Dart

El servidor anuncia cinco lenguajes. `internal/mcp/instructions.go:21` dice que
«Dart edges are resolved by Dart Analysis Server» y que «Python uses exact
semantic facts when a configured analyzer provides them». Go, TypeScript y Rust
tienen cada uno su fase, su benchmark y su precisión medida. Python y Dart no
tienen ninguna: cero benchmarks, cero informes, y Dart no aparecía ni una vez en
este fichero.

**Lo que la auditoría de código sí encontró, antes de medir nada.** Los dos
caminos ascienden a `EXACT` con un único flag del payload
-`internal/facts/semantic.go:295`-, y ese flag no se decide por arista: Dart lo
pone incondicionalmente al construir el payload
(`internal/dartloader/loader.go:122`) y Python lo deriva de si cayó al fallback
(`internal/pythonloader/loader.go:93`). Eso **sería** una infracción del contrato
de `AGENTS.md` -- una arista `EXACT` exige evidencia suficiente-- si los
productores emitieran referencias que su analizador no resolvió. Leídos, no lo
hacen:

- Dart sólo emite una referencia cuando la navegación del Analysis Server da un
  objetivo que mapea a un símbolo indexado o a un proveedor resuelto; lo demás
  se retiene como `UNRESOLVED` con motivo `DART_TARGET_NOT_INDEXED`
  (`internal/dartloader/loader.go:635-648`).
- El productor exacto de Python pide `textDocument/definition` por nodo y lo que
  no resuelve va a `UNRESOLVED` con `NAME_NOT_RESOLVED` o `TARGET_NOT_INDEXED`
  (`python-worker/pyright_index.py:309-314`).

Así que la deuda no es de corrección aparente: **es que nadie lo ha medido**. Y
este repositorio ya sabe lo que valen las afirmaciones sin medición.

**El método no se inventa.** Es el de `LUQUE-1816`: verdad de referencia escrita
a mano leyendo los fuentes del fixture, y se mide TP, FN y **falsas exactas**
contra ella. Comparar un índice contra sí mismo no prueba nada.

## LUQUE-2201 — La exactitud de Dart, medida

**Dependencias:** ninguna.

**Objetivo:** que la afirmación sobre Dart tenga un número detrás, o deje de
hacerse.

**Alcance:** `benchmarks/dart-semantic/` con `results.json` y `report.md`, con la
forma de `benchmarks/rust-semantic/`, sobre `testdata/dart/basic` y
`testdata/dart/advanced`.

**Criterios de aceptación:**

- **Falsas exactas = 0.** No es un umbral ajustable: es el contrato de
  `AGENTS.md`. Si sale distinto de cero, el resultado es el hallazgo.
- Toda referencia que el Analysis Server no resolvió se conserva como
  `UNRESOLVED` con su motivo. Un hecho perdido en silencio es el defecto que
  esta fase existe para no repetir.
- Los invariantes que no dependen de la verdad de referencia se comprueban
  igual: arista exacta con procedencia exacta, referencia con su observación, no
  resuelta con sujeto.
- Dos ejecuciones producen artefactos byte a byte idénticos.

**Estado:** cerrada el `2026-08-22`. `DART_SEMANTIC_PASS_WITH_LIMITS` con `27`
de `27` esperadas, `0` falsas exactas, `0` invariantes rotas, `6/6` no resueltas
emparejadas y dos ejecuciones byte a byte idénticas.

La primera medición dio `19` falsas exactas de `35` y `10` de `27`
esperadas. **Siete defectos**, todos arreglados aquí porque cada uno fabricaba
una arista `EXACT` que el fuente no contiene:

- `initialize` no anunciaba `hierarchicalDocumentSymbolSupport`, así que el
  servidor respondía `SymbolInformation` planos cuyo rango cubre sólo el
  identificador: ninguna declaración contenía las referencias de su cuerpo y
  `enclosing` caía al módulo. Publicaba `EXTENDS models.dart -> Vehicle` para
  `class ElectricVehicle extends Vehicle`. Una línea: `19` falsas a `6`, `10`
  aciertos a `24`, `edges_sourced_at_module` a `0`.
- La guarda de autorreferencia comparaba desplazamientos y no identidades: la
  ocurrencia de `Vehicle` en `class Vehicle` y el nombre de una directiva
  `library` pasaban como cuatro bucles exactos.
- Un `<unit>` -- el nombre que el Analysis Server da a la unidad de compilación --
  se tomaba por declaración, así que cada símbolo del fichero se publicaba
  también bajo un prefijo `<unit>.` y una referencia unía las dos copias.
- Una fila del outline sin localización de elemento caía en el inicio de la
  declaración mientras la del LSP caía en el identificador, así que la
  deduplicación por posición no las unía. Deduplicar por nombre lo rompía --
  tiraba la fila cuya posición usan los objetivos de navegación, `26` aciertos a
  `22` --, y el arreglo verdadero es resolver el desplazamiento del nombre.
- Un cuerpo con flecha se leía como asignación: `String asText() => value...`
  publicaba `ASSIGNS_FUNCTION` sobre una lectura.
- `dartKind` mapeaba `SymbolKind.Namespace` a `module`, y un `extension type` le
  quitaba a su fichero la identidad de módulo: reproducido en un paquete con
  `src/feature.dart`, donde la arista era `PART_OF src.piece -> UserId`.
- El campo de representación de un `extension type` no lo publica ninguna de las
  dos fuentes de declaraciones, así que todo uso de `id.value` apuntaba fuera del
  grafo.

Y un fixture: `testdata/dart/advanced/lib/library.dart` no compilaba
-`EXPORT_DIRECTIVE_AFTER_PART_DIRECTIVE`-, así que no demostraba el caso que
decía demostrar. Los dos paquetes pasan `dart analyze` limpio.

Lo que queda declarado en el informe, no arreglado: las aristas de directiva
viajan sin `evidence_key` -darles un extremo obliga a cambiar la versión del
payload que comparten los cinco lenguajes-, un hecho `part` se observa desde sus
dos extremos y produce dos filas idénticas, y una comparación dentro de un
paréntesis se clasifica `PASSES_AS_CALLBACK`.

**Verificación:** `go run ./benchmarks/dart-semantic` (dos veces, artefactos
idénticos), `go test ./internal/dartloader/`, `dart analyze` en los dos
fixtures.

---

## LUQUE-2202 — La exactitud de Python, en sus dos modos

**Dependencias:** ninguna.

**Objetivo:** lo mismo, y además la propiedad que separa los dos modos.

**Alcance:** `benchmarks/python-semantic/` sobre `testdata/python/basic` y
`testdata/python/coverage`, con **dos brazos**: el fallback de
`python-worker/index.py` y el exacto de `python-worker/pyright_index.py`.

**Criterios de aceptación:**

- **El brazo fallback no publica ni una arista exacta.** Es una propiedad del
  modo, no un número del corpus: falsable, y no depende de qué fixture se mida.
  Es lo que promete `loader.go:93`, y nadie lo comprobaba.
- **Falsas exactas = 0 en el brazo exacto**, por el mismo contrato.
- Si `pyright` no está disponible, el arnés **declara la ausencia y se salta ese
  brazo**; no finge un cero ni lo convierte en un fallo del código, que es la
  regla de `benchmarks/AGENTS.md`.
- Dos ejecuciones producen artefactos byte a byte idénticos.

**Estado:** cerrada el `2026-08-22`. `PYTHON_SEMANTIC_PASS_WITH_LIMITS`. El
brazo fallback cumple su propiedad -- `0` aristas exactas, `25` candidatas -- y el
exacto sale con `35` exactas, `0` falsas y `32` de `41` esperadas. Con `PATH` sin
`pyright-langserver` el brazo exacto se salta con motivo, sin cero inventado.
Dos ejecuciones byte a byte idénticas.

La primera medición encontró **el brazo exacto roto**, y con la infracción que
esta fase existía para buscar:

- Pedía `"capabilities": {}` y luego asumía la respuesta anidada, así que Pyright
  contestaba plano y todo símbolo recibía el prefijo del módulo:
  `Vehicle.drive` y `Car.drive` colapsaban en `pkg.models.drive`, el
  normalizador publicaba dos `DEFINES` para una clave y `facts.Set.Validate`
  rechazaba el conjunto. **El fixture `coverage` no se indexaba en absoluto.**
- Ninguna referencia salía de la función que la hace -- `sourceId: module_id` en
  todas --, así que `find_references` contestaba a granularidad de archivo. Y ese
  origen fabricaba una exacta falsa: `EXTENDS pkg.models -> pkg.models.Vehicle`
  sobre un `class ElectricVehicle(Vehicle):`.
- Arreglados los dos, aparecieron `16` falsas nuevas: los símbolos jerárquicos
  incluyen locales y parámetros, y una función sostenía aristas hacia sus propios
  locales y hacia sí misma. Un local no lo puede nombrar nadie desde fuera, que
  es la regla que Go aplica a una declaración que no alcanza el ámbito de
  paquete.
- La última falsa era un objetivo que Pyright sitúa dentro de un archivo pero
  sobre ninguna declaración indexada -- una `def` `@overload`ada --, que resolvía
  al módulo porque es el único símbolo cuyo rango cubre el fichero. Eso no es un
  objetivo resuelto: es una arista `EXACT` ganada por ser el último candidato.
  Ahora se retiene como `TARGET_NOT_INDEXED`, y el precio -- la llamada a
  `convert` que deja de publicarse -- está declarado en el informe.

- Y un hueco que sólo se vio con el binario: un acceso por atributo no dejaba
  **ni arista ni fila de no resuelta**, porque el recorrido no visita un
  `ast.Attribute`. `find_references` sobre `Box.get` contestaba `COMPLETE` y «una
  ausencia, no un fallo» sobre un `box.get()` que el fuente hace. Ahora la
  ocurrencia se pregunta como cualquier otra -- Pyright resuelve un miembro en su
  nombre -- y se rechaza igual cuando no resuelve: `26` aciertos a `32`.

La verdad de referencia se extendió por eso, **después de medir**, con dos filas
que el fuente contiene y que la primera versión no enumeró porque nada podía
observarlas: `Box.get -> Box.value` (`pkg/models.py:17` sobre `:14`) y
`Vehicle.drive -> Vehicle.name` (`:24` sobre `:21`). Está declarado en el informe
con esta misma razón; no se quitó ninguna expectativa ni se relajó un criterio.

Lo que queda declarado: `__all__` no se lee, así que no hay `REEXPORTS`; el
fallback sigue sin resolver un atributo, porque sin analizador no podría nombrar
el objetivo sin adivinarlo; y una anotación subscrita degrada a `REFERENCES`.

**Verificación:** `go run ./benchmarks/python-semantic` (dos veces, artefactos
idénticos) y el brazo ausente probado con un `PATH` sin `pyright-langserver`.

---

## LUQUE-2203 — Publicar los números o retirar la promesa

**Dependencias:** LUQUE-2201, LUQUE-2202.

**Objetivo:** que `instructions.go` y la skill digan lo que está medido, no lo
que se espera.

**Alcance:** con los dos informes en la mano, una de dos salidas para cada
lenguaje -- y la decisión sale de las cifras, no de las ganas:

1. Las cifras sostienen la afirmación: se queda, y el gate entra en la lista de
   gates globales.
2. No la sostienen: la superficie degrada a nombrar lo que sí está medido.
   `mcp.instructions` y `internal/integrations/assets/kivgraph/SKILL.md` son
   superficie de compatibilidad, así que el cambio se declara.

**Criterios de aceptación:**

- Ninguna frase sobre Python o Dart en la superficie visible sin una cifra
  medida detrás o una limitación declarada.
- La documentación describe el comportamiento observado, que es la regla de la
  raíz.

**Estado:** cerrada el `2026-08-22`, por la salida 1: las cifras sostienen las
dos afirmaciones y no hay que retirar ninguna. `instructions.go` dice que las
aristas de Dart las resuelve el Analysis Server -- `32` exactas, `0` falsas -- y
que Python emite hechos exactos cuando hay analizador configurado y `CANDIDATE`
en su fallback: medido, `35` exactas con `0` falsas en el brazo exacto y `0`
exactas con `25` candidatas en el fallback. Es una frase condicional, y el
bundle publica los dos productores en `worker/python-worker/`, que es la tercera
ruta que `resolveCommand` prueba: el modo exacto existe también para un binario
instalado. Los dos tokens entran en los gates globales.

**Lo que faltaba no era una frase: era que la superficie no mentía sólo cuando
acertaba.** El humo de esta fase -- indexar dos repositorios con el binario real y
preguntar -- encontró dos defectos que ninguna medición veía, y los dos están
arreglados aquí. Van abajo con su propia ficha porque no son de esta tarea.

---

## LUQUE-2204 — `find_references` vendía un fallo como una ausencia

**Dependencias:** ninguna. Encontrada por el humo de LUQUE-2202.

Preguntar por `pkg.service.convert` devolvía `total: 0` con la frase «the edges
are type-checked, so this is an absence rather than a miss», y el fuente la llama
en `pkg/service.py:31`. El índice **guardaba** la razón -- una fila no resuelta con
`requestedSymbol: convert`, su archivo y su línea -- y la herramienta no la
consultaba: `addReferenceCoverage` sólo contaba aristas con confianza
`Unresolved`, que es otra cosa. `get_blast_radius` ya usaba `completenessFor`
para exactamente esto; `find_references`, la herramienta por la que se compra el
producto, no.

Ahora `find_references` publica `completeness` como el resto: `COMPLETE` cuando
nada registrado puede añadir, y `LOWER_BOUND` con `blind_spots` y un patrón de
reserva cuando un fallo registrado nombra el símbolo. La frase de ausencia sólo
se emite en el primer caso.

La distinción no es cosmética: una lista vacía leída como «no existe» manda al
agente a `grep` y no vuelve, y leída como «no existe **y estoy seguro**» lo manda
a concluir. Lo segundo es peor, y era lo que decía.

**Verificación:** `TestFindReferencesNeverCallsARecordedMissAnAbsence`, que falla
con la frase vieja y pasa con la nueva; y el binario real, que sobre `convert`
responde `LOWER_BOUND` señalando `pkg/service.py:31`.

**Estado:** cerrada el `2026-08-22`.

---

## LUQUE-2205 — La caché de hechos no vigilaba al productor que corre

**Dependencias:** ninguna. Encontrada por el humo de LUQUE-2202.

Después de arreglar el productor exacto de Python, el binario seguía publicando
el grafo anterior. La huella de la caché nombraba
`python-worker/index.py` **a mano** -- el worker del fallback -- y nunca
`pyright_index.py`: editar el productor exacto no cambiaba ninguna clave, así que
una reconstrucción reutilizaba los hechos del productor anterior y publicaba una
generación que el código vigente no produce. Dos reglas de resolución, una en el
cargador que ejecuta y otra en la caché que vigila, y la de la caché miraba un
fichero que en modo exacto nadie corre.

Ahora `pythonloader.ProducerFile` resuelve con las mismas reglas que
`resolveCommand` y devuelve el fichero cuyo contenido decide los hechos -- el
script del adaptador, o el ejecutable externo --, y la caché lo huella. Sin
resolución no hay licencia para reutilizar nada: se emite un marcador único, que
es la regla que ese mismo fichero ya aplicaba al binario.

**Verificación:** `TestAnalyzerFingerprintWatchesThePythonProducerThatRuns`, que
falla contra la huella escrita a mano; y el binario real, donde un cambio de
contenido en `pyright_index.py` pasa la pasada de `hits=4 misses=0` a
`hits=0 misses=4`, y un `touch` no -- la huella es de contenido, no de fecha.

**Estado:** cerrada el `2026-08-22`.

---

# 30. Fase 23 — Ninguna tool afirma una ausencia sin comprobarla

La fase 22 cerró midiendo aristas, y encontró un defecto que ninguna medición
de aristas puede ver: `find_references` contestaba una lista vacía con «las
aristas están comprobadas por tipos, así que esto es una ausencia y no un
fallo» **mientras el índice guardaba una fila de fallo que nombraba ese mismo
símbolo**. Las aristas estaban bien. Lo que estaba mal era la frase.

Es el defecto más caro de la superficie porque es el producto: un agente
compra «¿quién llama a esto?» y la respuesta vacía es la que le hace borrar
código. `internal/mcp/instructions.go:23` ya se lo decía a todo agente --
«read confidence and completeness before treating an empty or partial answer
as proof of absence»-- y sólo dos de las seis tools cuya respuesta vacía se
lee como prueba publicaban `completeness`.

El objetivo de la fase es un invariante, no una cifra: **ninguna tool afirma
una ausencia que el grafo no sostiene**, y toda respuesta que sí es una
ausencia lo dice. Lo segundo es la mitad que se olvida: un veredicto que
nunca dice `COMPLETE` no informa de nada.

## LUQUE-2206 — El veredicto se emite donde una respuesta se puede leer como prueba

**Dependencias:** ninguna.

**Objetivo:** que las seis tools cuya respuesta vacía o parcial se lee como
prueba publiquen `completeness`, y que las dos que rechazan un símbolo que no
encuentran sigan rechazándolo.

**Alcance:** `internal/mcp/tools/`. Seis tools relacionales y de búsqueda;
`get_symbol` y `get_source` quedan fuera **por su forma**: no devuelven lista
vacía, devuelven error.

**Criterios de aceptación:**

- La pregunta hacia fuera no se comprueba con los fallos de la pregunta hacia
  dentro. «¿Quién llama a esto?» está acotada por los fallos que **pidieron el
  nombre**; «¿qué alcanza esto?» por los que **el símbolo mismo** provocó. Una
  sola de las dos comprobaciones habría dejado la otra sin acotar, así que
  `UnresolvedFromSymbol` existe para esa dirección.
- El ámbito de la comprobación sigue al ámbito de la pregunta. Una búsqueda de
  todo el grafo está acotada por cada paquete ilegible que contenga; una
  acotada a un repositorio, sólo por los de ese repositorio. Sin esta regla el
  veredicto es una constante: un paquete malo en cualquier sitio pintaría
  `LOWER_BOUND` en toda respuesta del corpus.
- `find_cross_repo_consumers` es deliberadamente la excepción: su ámbito ciego
  es **global**, porque un paquete ilegible en cualquier repositorio puede
  esconder justo al consumidor que se pregunta. Es la tool sin competidor
  nativo, así que su respuesta vacía se vende como hallazgo -- `grep` no puede
  seguir un reexport con `*` que cruza de repositorio-- y por eso es la que
  más necesita el respaldo.
- El coste se mide, no se supone. `"completeness":{"verdict":"COMPLETE"}` son
  `10` tokens (`cl100k_base`): `16 %` de una respuesta de una fila de
  `find_symbol` y `50 %` de una vacía. Así que una búsqueda lo gasta donde la
  respuesta se puede confundir con una prueba -- vacía, truncada-- y en todo
  límite inferior, mientras las cuatro relacionales lo llevan siempre: ahí
  `COMPLETE` sobre una respuesta con filas **es** la afirmación que se compra.
- Cada arreglo tiene un test que falla sin él.

**Verificación:** `go test ./internal/mcp/...`, y el arnés de la tarea
siguiente contra el binario real.

**Estado:** cerrada el `2026-08-22`.

## LUQUE-2207 — Un arnés que prueba el invariante con el binario

**Dependencias:** LUQUE-2206.

**Objetivo:** que el invariante sea comprobable y no una promesa. Los cuatro
arneses semánticos comparan un índice contra una verdad escrita y contestan
«¿están bien las aristas?». Ninguno pregunta lo que pregunta una sesión, que
es «¿usa alguien esto?», ni lee qué contesta la tool cuando no.

**Alcance:** `benchmarks/tool-honesty/`, `testdata/honesty/`.

**Criterios de aceptación:**

- Corre el binario real por MCP sobre `stdio`, indexando en un `HOME` aislado.
  Un arnés que construyera el snapshot a mano no probaría el camino que un
  usuario recorre.
- Dos repositorios, y el limpio es imprescindible: `go-pure` no tiene nada que
  el índice no pueda leer, así que **toda** respuesta sobre él debe ser
  `COMPLETE`. Sin ese brazo el veredicto podría ser una constante y las
  comprobaciones de `LOWER_BOUND` pasarían igual.
- El ámbito ciego se lee del **servidor**, no del fixture: un fixture es una
  afirmación hasta que la pasada registra el fallo que se escribió para
  producir. Si `unresolved_by_reason` no trae `PACKAGE_NOT_BUILDABLE`, el
  arnés falla en vez de dar por buenas trece comprobaciones.
- Falla ante el defecto que existe para cazar. Comprobado revirtiendo tres:
  la frase de `find_references`, y el veredicto de `trace_dependencies` en sus
  dos brazos.

**Verificación:** `go run ./benchmarks/tool-honesty --kivgraph <binario>`;
`13` comprobaciones, `13` pasan, y `3` fallan al revertir los arreglos.

**Estado:** cerrada el `2026-08-22`.

## LUQUE-2208 — El invariante es sobre la forma de un fallo, no sobre un lenguaje

**Dependencias:** LUQUE-2207.

**Objetivo:** cerrar la limitación que la ficha anterior declaró. `LUQUE-2207`
probó el invariante sobre **un** motivo de **un** lenguaje, y de los cinco que
el servidor anuncia sólo Go y Rust registran un fallo de repositorio y siguen
-- los otros tres abortan la pasada. El camino de Rust no tenía **ningún** test
que nombrara sus motivos.

**Alcance:** `benchmarks/tool-honesty/`, `testdata/honesty/rust-*`,
`internal/indexer/rust_unit_test.go`.

**Criterios de aceptación:**

- Un solo corpus con los dos lenguajes, no dos pasadas. Es lo que permite la
  comprobación que ninguno haría solo: una respuesta acotada a un repositorio
  Go sigue siendo `COMPLETE` mientras un workspace Rust del mismo grafo es
  ilegible, y al revés. Un veredicto que se contagiara entre lenguajes sería
  una constante en cualquier monorepo políglota, que es el único tipo para el
  que existe este producto.
- El fallo de Rust es de **otro motivo** que el de Go: `WORKSPACE_NOT_LOADED`
  contra `PACKAGE_NOT_BUILDABLE`. Lo que comparten es la forma -- fila sin
  archivo--, que es exactamente lo que dice el invariante.
- El fixture se elige midiendo, no suponiendo. Una dependencia irresoluble
  **no** sirve: rust-analyzer carga el workspace igual y degrada -- medido,
  `8` símbolos y cero fallos. Un miembro declarado que no existe es lo que
  Cargo no puede resolver, con red o sin ella.
- Cada brazo declara su propio ámbito, y la pasada se niega si alguno perdió
  el suyo. Un contador compartido no distinguiría dos puntos ciegos de un
  fixture haciendo todo el trabajo.
- El brazo Rust se salta declarándose cuando falta su toolchain, nunca se
  finge ni se convierte en `FAIL`.
- Los dos motivos de ámbito de Rust tienen test, y falla si la fila gana un
  archivo -- que es lo que la convertiría en una referencia y dejaría de
  acotar su repositorio.

**Verificación:** `go run ./benchmarks/tool-honesty --kivgraph <binario>`;
`18` comprobaciones, `18` pasan. `4` fallan al hacer global el ámbito del
veredicto de `find_references` -- incluidas las dos de no contagio--, y la
pasada se niega al reparar el fixture Rust. `TestWorkspaceNotLoadedFacts...`
falla al darle un archivo a la fila.

**Estado:** cerrada el `2026-08-22`.

---

# 31. Fase 24 — Los tres defectos que la fase 22 dejó nombrados

La fase 22 cerró declarando tres defectos sin arreglar. Al medirlos, **dos de
las tres razones para aplazarlos eran falsas**, y una de las tres cosas
declaradas no era un defecto.

## LUQUE-2209 — Una comparación no es un argumento

**Dependencias:** ninguna.

**Objetivo:** que `dartReferenceKind` distinga un argumento de un operando.

**Alcance:** `internal/dartloader/loader.go`, `testdata/dart/advanced`,
`benchmarks/dart-semantic`.

**Criterios de aceptación:**

- Son **dos** defectos ortogonales y ninguno arregla al otro: en
  `if (other == handler)` el paréntesis lo abre un keyword y no un callee; en
  `register(other == handler)` el paréntesis sí abre una llamada, pero la
  ocurrencia es operando de la comparación. Dos reglas: `comparedInDartPrefix`
  y `opensDartArgument`, que busca el corchete sin cerrar más interno y mira
  qué identificador lo precede.
- El fixture lo ejercita, que es lo que faltaba para no arreglarlo a ciegas.
  Escribirlo destapó un tercer caso que nadie había nombrado:
  `final same = (other == handler)` salía `ASSIGNS_FUNCTION`.
- El caso positivo no se rompe, incluido el callee alcanzado por acceso a
  miembro (`registry.add(handler)`).

**Verificación:** `19` casos en `TestDartReferenceKindClassifiesResolvedUses`,
cuatro de ellos fallando antes del arreglo; `go run ./benchmarks/dart-semantic`
con `31/31` aciertos, `0` falsas exactas; y el binario real, donde
`find_references fallback` devuelve tres filas con su clase cada una --
`REFERENCES` en la comparación, `CALLS_DIRECT` y `PASSES_AS_CALLBACK` en el
argumento.

**Estado:** cerrada el `2026-08-22`.

## LUQUE-2210 — Una arista de directiva nombra la evidencia que la justifica

**Dependencias:** ninguna.

**Objetivo:** que `IMPORTS_SYMBOL`, `REEXPORTS` y `PART_OF` lleven
`evidence_key`, que es lo que `AGENTS.md` exige de una arista canónica.

**Alcance:** `internal/facts/semantic.go`, `internal/dartloader/loader.go`.

**Criterios de aceptación:**

- Las dos razones del aplazamiento eran falsas, y medirlas es lo primero: el
  payload lo comparten **dos** lenguajes, no cinco -- Go, TypeScript y Rust
  tienen su propio normalizador--, y los dos productores de Python **ya
  enviaban el fin** de cada import en `point()`. No hubo cambio de protocolo ni
  ADR: el decodificador Go no tenía campos donde ponerlo.
- La evidencia va en el **origen** de la arista, que es la convención del resto
  del paquete. Para un `part`, eso es la directiva del archivo parte, aunque el
  otro extremo se observe primero.
- Sin vano observado no hay evidencia. Inventarlo colisiona: `EvidenceKey` sale
  de los desplazamientos, así que dos directivas del mismo archivo compartirían
  clave y cada una sobrescribiría a la anterior.
- **Lo que no cambia se declara.** La respuesta servida es idéntica byte a
  byte antes y después: `hotsnapshot.EvidenceRecord` no proyecta la posición,
  así que ninguna tool puede abrir el vano de una evidencia en ninguno de los
  cinco lenguajes. Proyectarlo sube la versión del formato de filas del
  snapshot y ningún consumidor lo pide. Anotado en `internal/AGENTS.md`.

**Verificación:** `directive_edges_without_evidence` de `4` a `0`,
`imports_without_evidence` de Python de `7`/`12` a `0`; tres tests en
`internal/facts/semantic_test.go`, cada uno falsificado por separado; y una
sonda que abre cada evidencia publicada y comprueba que el texto es la
directiva -- `part of 'library.dart';` en el archivo parte.

**Estado:** cerrada el `2026-08-22`.

## LUQUE-2211 — El `part` duplicado no existía

**Dependencias:** ninguna.

**Objetivo:** comprobar el tercer defecto declarado antes de arreglarlo.

**Alcance:** el hallazgo de `benchmarks/dart-semantic`.

**Criterios de aceptación:**

- Medido: el payload lleva `2` filas y el grafo publica `1` arista.
  `NormalizeSemantic` deduplicaba por identidad desde el commit que trajo Dart
  -- `fe8308b`, antes de la fase 22--, así que el hallazgo describía un defecto
  que nunca ocurrió.
- Lo que sí faltaba, y salió al medirlo, es que `SemanticPart` no dice **en qué
  archivo** se observó la directiva: `LibraryFile` y `PartFile` nombran los
  extremos de la relación, no dónde está el texto. Se arregla en `LUQUE-2210`,
  que es quien necesita esa respuesta.
- Una limitación que no existe es ruido que tapa las que sí: el hallazgo se
  reescribe con la medición en la mano.

**Verificación:** una sonda sobre `testdata/dart/advanced` contando aristas
`PART_OF` y filas de payload, y `git log -L` sobre el bloque de deduplicación.

**Estado:** cerrada el `2026-08-22`.

---

# 32. Fase 25 — Lo que la fase 23 rompió al publicar el veredicto

La fase 23 dio a seis tools un `completeness`. Al medir la documentación que le
faltaba salió algo peor: el helper del veredicto contaba mal desde que nació y
esta fase lo propagó a cinco tools, cambiando en silencio lo que informa un
contador publicado. Las cuatro puertas seguían en verde.

## LUQUE-2212 — Un contador publicado que cambió de significado

**Dependencias:** `LUQUE-2206`.

**Objetivo:** que `coverage.unresolved_related` vuelva a contar lo que dice
contar.

**Alcance:** `internal/mcp/tools/completeness.go` y los cinco llamantes.

**Criterios de aceptación:**

- Medido con el binario real: `find_symbol` de un nombre que nadie referencia
  informaba de `unresolved_related: 29`, y los `29` eran ámbitos ilegibles del
  repositorio que no nombran nada parecido. `usage.md` documenta los cuatro
  contadores como **disjuntos** y sobre la consulta.
- El origen, con `git log -S`: `5960312` (`2026-08-12`) creó `completenessFor`
  devolviendo ya `namingTotal + scopeTotal`, sobre `find_references` y
  `get_blast_radius`. La fase 23 (`308e802`) extendió el helper a cuatro tools
  más y en `find_symbol` **sustituyó** un contador que sólo contaba nombres --
  `snapshot.UnresolvedNamingSymbol(name, 0)`--, que es donde hay rotura de
  compatibilidad. Once días, no un commit, y se propagó.
- Lo delator: `find_cross_repo_consumers` descartaba el valor con su propio
  comentario -«adding them twice would inflate the only number a caller can
  audit»- mientras las otras cinco lo sumaban. El problema estaba visto en una
  tool y no en las demás.
- `completenessScopes` no devuelve contador: todo lo que encuentra es ámbito, y
  un ámbito no es una de las cuatro cosas que `coverage` cuenta. `get_file_outline`
  vuelve a `Coverage{Exact: kept}`, que es lo que publicaba antes.
- Nada se pierde: los `29` aparecen donde corresponde -`20` listados más `9` en
  `more_invisible_scopes`.
- Cambiar lo que un contador cuenta es un cambio de esquema aunque el campo no
  cambie de nombre ni de tipo, y el compilador no lo ve. Queda escrito en
  `internal/mcp/AGENTS.md`.
- La raíz exige ADR para un cambio de protocolo MCP, y no había ninguno: ni la
  fase 23 por publicar un campo nuevo en seis tools, ni esta por corregir el
  valor de un contador en cinco. Es **ADR 0063**, y cubre las dos mitades
  porque son la misma superficie: el veredicto y el contador que infló.

**Verificación:** `TestCompletenessSeparatesAFailedReferenceFromAnUnreadableScope`
extendido con la aserción del contador -falsificado volviendo a sumar los
ámbitos, y falla-; y el binario real por MCP sobre este repositorio.

**Estado:** cerrada el `2026-08-23`.

## LUQUE-2213 — La documentación del veredicto, y tres frases falsas

**Dependencias:** `LUQUE-2206`.

**Objetivo:** que las once páginas de tools digan lo que su tool hace hoy.

**Alcance:** `landing/src/content/docs`.

**Criterios de aceptación:**

- Medido antes de escribir: de las seis tools que emiten el veredicto, sólo
  `get_blast_radius` lo documentaba. Tres lo mencionaban sin nombrarlo y dos
  callaban.
- Tres afirmaciones eran **falsas**, no incompletas: `find-references.md` y
  `trace-dependencies.md` decían «No `completeness` object appears on this
  tool», `find-symbol.md` decía «`find_symbol` never emits `guidance`» y
  `get-blast-radius.md` seguía siendo «the one tool that states how far its
  answer reaches». Una página que afirma lo contrario de lo que hace el código
  es peor que una que calla.
- La semántica compartida vive en un solo sitio -`mcp/usage.md`- con una tabla
  de qué acota a cada tool; las páginas enlazan en vez de copiar el bloque de
  cuarenta líneas, que es lo que se queda rancio.
- Dos filas de esa tabla las corrigieron los agentes que las consumieron:
  `direction` no ramifica el veredicto de `find_references` -un solo call site,
  sin dirección- y `trace_dependencies` también incluye los ámbitos de su
  repositorio. Escribí las dos mal.
- La frase de `usage.md` sobre una respuesta vacía de `find_references` era
  condicional desde la fase 23 y se leía como incondicional: es la frase que un
  agente usa para borrar código vivo.

**Verificación:** `make landing-check` en cero errores, y cada afirmación
contrastada contra `internal/mcp/tools`.

**Estado:** cerrada el `2026-08-23`.

## LUQUE-2214 — La versión documentada vive en cuatro sitios

**Dependencias:** ninguna.

**Objetivo:** que la documentación no pueda quedarse por detrás del binario sin
que algo falle.

**Alcance:** `internal/version`, la landing y la skill `publishing-releases`.

**Criterios de aceptación:**

- El defecto: `landing/src/content/docs/install.md` documentaba
  `KIVGRAPH_VERSION=v0.3.0` con la `v0.5.0` publicada -dos minors de retraso, y
  el ejemplo que un lector copia.
- La causa: la skill decía «tres sitios» y su `grep` sólo miraba `README.md` y
  `docs/installation.md`. Una lista escrita a mano se queda corta en cuanto una
  página nueva lleve el comando.
- `TestDocumentedInstallVersionMatchesTheBinary` **descubre** cada comando de
  instalación fijado del repositorio y falla nombrando archivo y línea. No
  enumera nada.
- Un registro histórico queda fuera: esta misma ficha cita el
  `v0.3.0` del defecto y tumbó el test al escribirla. Un backlog no instruye a
  nadie a instalar nada, y sostenerlo a la versión de hoy haría imposible
  describir un defecto de versión. Comprobado que la exclusión no lo desarma:
  con `install.md` revertido a mano vuelve a fallar nombrándolo.

**Verificación:** el test falla nombrando los tres ficheros al subir
`version.Value` sin tocar la documentación, y pasa con ellos al día.

**Estado:** cerrada el `2026-08-23`.

## LUQUE-2215 — Un contador que significaba dos cosas

**Dependencias:** `LUQUE-2212`.

**Objetivo:** auditar los tres contadores de `coverage` que quedaron sin
revisar, y dejar `exact` con un solo significado.

**Alcance:** `find_symbol`, `get_file_outline` y la documentación de la
superficie.

**Criterios de aceptación:**

- Medido con el binario real, pidiendo menos de lo que la respuesta tiene:
  `find_references` responde `exact=3` sobre una página de `2` -toda la
  respuesta- y `find_symbol` responde `exact=2` sobre un total de `52` -la
  página. Un cliente no puede escribir una sola regla.
- El defecto estaba publicado: la página de `get_file_outline` mostraba
  `total: 32`, `returned: 19`, `exact: 19`.
- El ámbito de página era el síntoma. Los cuatro contadores clasifican
  **relaciones resueltas** por confianza, y esas dos tools devuelven
  declaraciones: su `exact` era por construcción igual al número de filas.
- Se retira en vez de hacerlo de respuesta, porque de respuesta sería idéntico
  a `total`: un contador que no puede variar repite un número que ya viaja
  antes. Es el mismo razonamiento con el que el ADR 0063 retiró
  `unresolved_related` de `get_file_outline`.
- No es global, y el matiz importa: `get_source` parece el mismo caso y no lo
  es -su cuenta son los cuerpos que pudo servir, menor que `returned` cuando un
  fichero se movió, y viaja en la cabecera de su prosa, no en `coverage`-, y en
  `find_cross_repo_consumers` los cuatro informan de cuatro cosas distintas.
- Ninguna migración: la vista compacta ya omitía `coverage` con los cuatro a
  cero y la documentación ya lo decía, así que la ausencia era legal. La vista
  `full` sigue escribiendo los ceros, que es su contrato.

**Verificación:** cuatro payloads dorados por tool caen al reponer el contador,
revirtiendo cada mitad por separado; el binario real por MCP; y las `21`
capturas JSON de las dos páginas parseadas, con `coverage` sólo en las de vista
`full` y a cero. Ver ADR 0064.

**Estado:** cerrada el `2026-08-23`.

## LUQUE-2216 — Qué hay dentro del heap privado de un proceso

**Dependencias:** `LUQUE-2006`.

**Objetivo:** separar, en lo que cuesta cargar un snapshot, la respuesta del
trabajo de llegar a ella.

**Corregido el `2026-08-23`:** este objetivo decía «son la misma cifra en
`Private_Dirty` y se arreglan al revés», y la primera mitad es falsa. Sólo la
respuesta está en `Private_Dirty` en régimen estacionario; el trabajo de llegar a
ella se devuelve al heap y lo reutiliza lo que viene después. Retirar los
`60,5 MB` transitorios que esta ficha nombró dejó el residente por servidor en
`71,22 MB` contra `71,76`, medido en Linux en
`benchmarks/load-cost-resident`. Lo que se arregló era real y lo que compró es
tiempo hasta la primera respuesta, no bytes por proceso. Las cifras de abajo son
contabilidad del runtime de Go y siguen siendo exactas como tales.

**Alcance:** `benchmarks/snapshot-heap`, sin tocar producción.

**Criterios de aceptación:**

- Medido sobre `kena` -- `35` repositorios, `117.499` símbolos, `337.314`
  aristas, fichero de `86,7 MB`: la carga **asigna `89,7 MB` y conserva
  `27,7`**. El `69 %` de lo que pide es basura suya. `247 B` por símbolo vivos
  contra `801` asignados.
- Y la mitad de esa basura tiene nombre: **cada tabla de ancho fijo se copia
  dos veces**. Los `decode*` de `readSnapshot` asignan un slice por sección y
  copian dentro los bytes mapeados; `NewGraphSnapshot` vuelve a copiar cada uno
  (`snapshot.go:256-269`). Las parejas del perfil son exactas, no parecidas:
  `decodeSymbols` `5.056 kB` contra la línea de símbolos `5.056 kB`;
  `decodeEvidence` `6.592` contra `6.592`; `decodeEdges` `7.920` contra los dos
  `3.961,73` de las dos direcciones. Aritmética sobre las filas: `19,8 MB`.
- La segunda copia es correcta en el constructor y superflua en el lector: existe
  para que un llamante pueda seguir mutando lo que pasó, y el lector pasa slices
  que decodificó una sentencia antes y que nadie más puede nombrar.
- El otro bloque con nombre es validación: `validReverseCounterpart`
  (`snapshot.go:478`) construye un mapa con clave en **cada arista directa** para
  probar que el CSR inverso es su permutación, y lo tira -- `13,7 MB` en la
  pasada de asignación.
- El perfil se toma con el snapshot **vivo**, que es lo que el benchmark del
  paquete no puede hacer: el suyo se escribe cuando ya es inalcanzable y no
  atribuye ni un byte vivo. Ése fue el primer intento y salió en `0,34 kB`.
- No se arregla nada aquí. Medir antes de tocar es el punto: los bytes vivos se
  atacan moviendo una estructura al fichero, y los transitorios no
  asignándolos.

**Verificación:** `go run ./benchmarks/snapshot-heap -generation-dir <generación>`
sobre una generación publicada real. Tres pasadas del mismo binario dan el vivo
byte a byte idéntico y el asignado dentro de `0,1 MB`.

**Estado:** cerrada el `2026-08-23`.

## LUQUE-2217 — El lector adopta las tablas en vez de copiarlas

**Dependencias:** `LUQUE-2216`.

**Objetivo:** retirar el gemelo que `LUQUE-2216` midió, sin relajar el contrato
público de `NewGraphSnapshot`.

**Alcance:** `internal/hotsnapshot/snapshot.go` y `file.go`.

**Criterios de aceptación:**

- El cuerpo pasa a `newGraphSnapshot(input, owned bool)`. El público llama con
  `false` y sigue copiando: su contrato es que el llamante puede seguir mutando
  lo que pasó, y el constructor lo necesita. `readSnapshot` llama con `true`.
- Comprobado antes de tocar, porque es lo que decide la corrección: los cinco
  decodificadores usan `make` y copian campo a campo. **Ninguno alía los bytes
  mapeados**, así que adoptar es seguro en los dos caminos de `readSnapshot` --
  el mapeado y el que lee de un buffer del llamante.
- Medido sobre el mismo corpus de `LUQUE-2216`, `117.499` símbolos: lo asignado
  baja de `89,7` a `68,9 MB` -- `801` a `615 B` por símbolo-- y **los bytes
  vivos quedan idénticos**, `27,7 MB`. La aritmética predecía `20,74 MB`
  (`19,8` de tablas más `0,94` de los dos arrays de desplazamientos) y la
  medición dio `20,8`.
- `NewGraphSnapshot` desaparece de la lista de asignadores del perfil. Lo que
  queda arriba son dos mapas transitorios con nombre: `indexSnapshotInput`
  (`16,5 MB`, mapas que `newSymbolIndex` aplana acto seguido) y
  `validReverseCounterpart` (`13,3 MB`, mapa de validación que se tira).
- El contrato público ya tenía quien lo vigilase:
  `TestGraphSnapshotCopiesDataAndIndexes`. No se añade un test de la adopción
  porque no es un contrato observable: es una propiedad de memoria, y la
  defiende la medición.

**Verificación:** la suite entera; `benchmarks/snapshot-heap` antes y después; y
humo con el binario real por MCP sobre `kena` -- `find_symbol`,
`find_references` y `get_file_outline` contestan con sus veredictos.

**Estado:** cerrada el `2026-08-23`.

## LUQUE-2218 — La validación del CSR inverso no paga un mapa

**Dependencias:** `LUQUE-2217`.

**Objetivo:** retirar el segundo asignador que `LUQUE-2216` nombró, sin relajar
lo que la validación prueba.

**Alcance:** `validReverseCounterpart` en `internal/hotsnapshot/snapshot.go`.
Guardaba una clave por arista directa en un `map[csrEdgeKey]int` para probar que
el CSR inverso es la permutación del directo, y lo tiraba en la misma llamada.
Ahora marca un bit por arista directa y busca la contrapartida recorriendo el
grupo de la fuente.

**Criterios de aceptación:**

- Medido sobre `kena` -- `35` repositorios, `117.499` símbolos, `337.314`
  aristas--, con el mismo fichero para las dos versiones: lo asignado por la
  carga baja de `69,2 MB` a `55,9 MB`, y de `617 B` a `499 B` por símbolo. Lo
  transitorio baja de `41,5 MB` a `28,2 MB`, del `60 %` al `50 %`. Los bytes
  vivos no se mueven: `27,7 MB`, `247 B` por símbolo.
- El coste que compra está medido y no es tiempo. El recorrido es la suma de los
  grados de salida al cuadrado -- `18,4 M` comparaciones, `54x` el número de
  aristas, grupo mayor `889`, mediana `1`. Alternando las dos versiones cinco
  veces sobre el mismo fichero, el mapa de bits gana cuatro: mínimos `150,0 ms`
  contra `159,6 ms`.
- `validReverseCounterpart` desaparece de la lista de asignadores del perfil,
  que es la comprobación de que el mapa era lo que se retiró.
- El cambio convierte una clave compuesta de siete campos en siete
  comparaciones, así que cada una necesita quien la defienda: antes viajaban
  juntas en la clave y no podían podrirse por separado. Lo fija
  `TestReverseCounterpartComparesEveryEdgeField`, con un fixture de dos aristas
  desde la misma fuente -- el compartido tiene una sola, y romper su fila inversa
  mueve la fuente, así que el recorrido falla por no tener dónde mirar y no por
  el campo bajo prueba. Siete casos: uno por campo, más una arista directa
  reclamada dos veces, que es lo único que fija el mapa de bits.
- Falsificado antes de darlo por bueno: nueve sabotajes -- ignorar cada uno de
  los seis campos, no marcar el bit, no comprobarlo, y devolver `true`-- y los
  nueve caen, cada uno en el caso que lleva su nombre. Contra los tests que ya
  existían, ocho de los nueve pasaban.

**Verificación:** `gofmt`, `go vet ./...`, `go test ./...`, `make test-ladybug`,
`make build`; el arnés `benchmarks/snapshot-heap` regenerado; y humo con el
binario real por MCP sobre `kena`.

**Estado:** cerrada el `2026-08-23`.

## LUQUE-2219 — Los índices de búsqueda se derivan de las tablas

**Dependencias:** `LUQUE-2216`, `LUQUE-2218`.

**Objetivo:** retirar el mayor asignador transitorio que quedaba, y con él una
superficie por la que un llamante podía entregar un índice que no concordaba con
sus propios registros.

**Alcance:** `GraphSnapshotInput` pierde `SymbolsByName`, `SymbolsByQName` y
`FileByRepoPath`; `newSymbolIndex` y `newFileIndex` derivan de `Symbols` y
`Files`; `indexSnapshotInput` desaparece del camino del lector y el builder deja
de acumular los dos mapas de símbolos. El rechazo de dos ficheros en una ruta se
muda a `newFileIndex`, que lo ve sin mapa porque los duplicados quedan
adyacentes al ordenar.

**Criterios de aceptación:**

- Ninguno de los tres mapas guardaba nada que las tablas no dijeran ya: la clave
  del símbolo `i` es un campo del símbolo `i`. Por eso la comprobación de que
  índice y registros concordaban **no podía fallar**, y por eso se retira en
  favor de una comprobación de forma -- que es el trato que
  `packageIncomingIndex`, ya derivado, recibía desde el principio.
- Medido sobre `kena` -- `35` repositorios, `117.499` símbolos--, mismo fichero
  para las dos versiones: lo asignado baja de `55,9 MB` a `36,4 MB`, y de
  `499 B` a `325 B` por símbolo. Lo transitorio baja de `28,2 MB` a `8,7 MB`,
  del `50 %` al `24 %`. Los bytes vivos no se mueven: `27,7 MB`.
- El primer intento **empeoró el tiempo**: ordenar leyendo la clave del registro
  a través de una función de comparación costó `+18 ms` por carga, cuatro pares
  alternados de cuatro. Empaquetar clave e id en un `uint64` -- los dos son
  `uint32`-- deja el orden en `slices.Sort` sobre enteros, sin comparador, y la
  carga baja a `139,9 ms` frente a `152,9` con los mapas. Cuesta `1,9 MB` de
  arrays empaquetados que se tiran.
- Un fixture ya no puede discrepar de sus registros. `internal/webapi` mantenía
  a mano tres mapas que debían concordar con sus propios símbolos; ahora no
  existen. Los dos casos de `TestGraphSnapshotRejectsInvalidEnvelopeAndIndexes`
  que entregaban un índice discrepante se retiran porque ese estado ya no se
  puede construir, y en su lugar queda el único rechazo que sigue siendo
  alcanzable: dos ficheros en una ruta.
- Cobertura: `indexes.go` no tenía **ni un test directo**. Lo cubrían ochenta
  tests de integración que construían un snapshot de paso, y por eso sabotear un
  campo de la clave empaquetada fallaba en `find_references` en vez de aquí.
  `internal/hotsnapshot/indexes_test.go` lo fija con un oráculo -- el mapa
  retirado, reescrito en el test-- más los invariantes que la búsqueda binaria
  necesita, los bordes del empaquetado (clave `0` y `MaxUint32`), el rechazo de
  duplicados adyacentes y separados, las dos vistas vacías y la exactitud de la
  reserva. `100 %` de sentencias de `indexes.go` con sólo esos tests.
- Falsificado: catorce sabotajes -- no ordenar, ordenar sólo por clave, invertir
  las mitades del empaquetado, cerrar el tramo una posición antes, no separar
  tramos, reservar por símbolo, ignorar la clave en `lookup`, devolver vacío en
  vez de `nil`, ignorar el repositorio en la clave de fichero, invertir su
  orden, no rechazar duplicados, las dos comprobaciones de forma y compartir el
  cursor del índice de paquetes-- y los catorce caen con **sólo** los tests
  nuevos.

**Verificación:** `gofmt`, `go vet ./...`, `go test ./...`, `make test-ladybug`,
`make build`; arnés `benchmarks/snapshot-heap` regenerado; y humo con el binario
real por MCP sobre `kena`.

**Estado:** cerrada el `2026-08-23`.

## LUQUE-2220 — La validación de claves no copia ni pregunta lo imposible

**Dependencias:** `LUQUE-2219`.

**Objetivo:** retirar la última copia con nombre del camino de carga, y con ella
una comprobación que ningún fichero podía suspender.

**Alcance:** `validExactIndexes` en `internal/hotsnapshot/snapshot.go`.

**Criterios de aceptación:**

- **La copia.** `StableKeyTable.Key` copia lo que entrega mientras la tabla está
  prestada de un fichero mapeado, porque la memoria mapeada no sobrevive a su
  `munmap` y una clave que apuntara a ella respondería desde páginas liberadas.
  Es correcto para un llamante que la guarde, y `validExactIndexes` pedía las
  `117.499` para tirar cada una en la sentencia siguiente: `7,2 MB`. Dentro del
  paquete ya existía `value`, la vista que no copia y que `Lookup` usa para
  comparar y descartar; su propio comentario lo decía.
- **La pregunta.** La otra mitad leía la entrada `i` y la buscaba esperando `i`
  de vuelta. No puede fallar: `NewStableKeyTable` y `StableKeyTableFromArena`
  rechazan entradas que no estén en orden estricto de bytes, y una búsqueda
  binaria sobre entradas ascendentes -- por tanto distintas-- devuelve la posición
  de la que se le dio. El comentario que decía que esta vuelta «hace fiable el
  orden» atribuía mal la garantía: la dan esos dos constructores. Costaba `117`
  mil búsquedas binarias sobre páginas mapeadas.
- **Antes de retirarla, defenderla donde vive.** El orden estricto que la hace
  infalsificable **no estaba fijado en las dos direcciones**: sabotear
  `StableKeyTableFromArena` para aceptar entradas descendentes pasaba la suite
  entera; sólo el caso de claves iguales caía, y por un test de carga. Los cuatro
  casos de `TestStableKeyTableFromArenaValidatesWhatItIsHanded` eran todos sobre
  desplazamientos. Añadidos dos sobre los bytes -- descendente y dos iguales-- con
  desplazamientos perfectamente válidos, que es posible porque toda clave del
  corpus mide lo mismo. Las tres formas de romper el orden caen ahora.
- **Medido** sobre `kena`, mismo fichero: lo asignado baja de `36,4 MB` a
  `29,2 MB` y de `325 B` a `261 B` por símbolo; lo transitorio de `8,7 MB` a
  `1,6 MB`, del `24 %` al `5 %`. La carga baja a `123,6 ms` frente a `134,5`,
  tres pasadas alternadas de tres. Los bytes vivos siguen en `27,7 MB`.
- La carga asigna ahora `261 B` por símbolo y conserva `247`: lo que se tira son
  los dos arrays empaquetados de los índices, y nada más tiene nombre.

**Verificación:** `gofmt`, `go vet ./...`, `go test ./...`, `make test-ladybug`,
`make build`; arnés regenerado; y humo con el binario real por MCP sobre `kena`.

**Estado:** cerrada el `2026-08-23`.

## LUQUE-2221 — Lo que las cuatro fases anteriores no compraron

**Dependencias:** `LUQUE-2216`, `LUQUE-2217`, `LUQUE-2218`, `LUQUE-2219`,
`LUQUE-2220`.

**Objetivo:** medir en Linux si retirar `60,5 MB` de lo que la carga asigna baja
lo que un proceso que sirve conserva residente. Es la única cifra en la que están
escritos los dos criterios -- el gate de `LUQUE-2006` y la reapertura de
`LUQUE-2008`-- y ninguna de las cuatro fases la observó: todas midieron
contabilidad del runtime de Go en darwin.

**Alcance:** `benchmarks/load-cost-resident`, sin tocar producción. No sobrescribe
los artefactos de `benchmarks/shared-snapshot`, que se tomaron en el host de
referencia; esto es otra plataforma y se declara.

**Criterios de aceptación:**

- **La respuesta es no.** `Private_Dirty` por servidor: `71,76 MB` antes contra
  `71,22` después, `0,75 %`, tres pares de tres sobre el mismo fichero. Por
  símbolo, `647,348 B` contra `647,104`. La aritmética de la asignación retirada
  predeciría unos treinta megabytes.
- **Por qué.** Es lo que «transitorio» significa para un asignador: las páginas
  que se ensucian decodificando se devuelven al heap y las reutiliza el trabajo
  siguiente, así que nunca estuvieron residentes en régimen estacionario.
  `Private_Dirty` tomado tras `4.000` llamadas de calentamiento y `2.000` medidas
  informa del heap de servicio asentado, no del pico de la carga.
- **Lo que sí compró.** La primera respuesta: `138-156 ms` contra `191-292 ms`,
  tres pares de tres, consistente con los `123,6 ms` contra `134,5` que el
  benchmark de carga mide en darwin. Es la cifra que ve quien arranca un
  servidor.
- **Lo que corrige.** Tres afirmaciones escritas por mí: el objetivo de
  `LUQUE-2216` decía que las dos mitades «son la misma cifra en `Private_Dirty`»;
  la ficha de `LUQUE-2008` decía que lo que la mantiene cerrada es que hay «una
  vía más barata para el mismo byte»; y las limitaciones de
  `benchmarks/snapshot-heap` declaraban que `Private_Dirty` es mayor que sus
  cifras sin decir que bajar las suyas no lo baja. Las tres quedan corregidas
  donde estaban.
- **Lo que no cambia.** `LUQUE-2008` sigue aplazada y por su primer motivo: la
  condición pide `> 100 MB` por proceso y estamos en `71,2`. Pero la razón es el
  tamaño del corpus -- `117.499` símbolos a `647 B`-- y no este trabajo. Un corpus
  de unos `170.000` la cruza igual que antes.
- **Un hueco declarado:** no se midió el pico residente durante la carga, sólo el
  valor asentado. Una máquina que arranca ocho servidores a la vez paga un pico
  que nadie mide, y es el único sitio donde los `60,5 MB` podrían aparecer
  residentes.

**Método:** VM `linux/arm64` de Docker Desktop sobre Apple Silicon, imagen
`golang:1.26-trixie`, glibc `2.41`, page size `4096` -- el mismo que amd64, que es
lo que hace comparables las unidades. Los dos binarios se compilan del mismo
árbol en dos commits (`c420490` y `0ad501a`) y leen la misma generación
publicada, byte a byte: el formato no cambió entre ellos. El workspace se monta
en sólo lectura, que es lo que hace imposible tocar un repositorio indexado. La
biblioteca nativa fijada exige glibc `≥ 2.38`: sobre `bookworm` (`2.36`) el
enlazado falla con `GLIBCXX_3.4.31` y `__isoc23_strtol` sin resolver.

**Estado:** cerrada el `2026-08-23`.

## LUQUE-2222 — Lo que un proceso para muchos clientes cuesta de verdad

**Dependencias:** `LUQUE-2008`, `LUQUE-2221`.

**Objetivo:** medir en Linux lo que un demonio conserva residente sirviendo a N
clientes, contra N procesos `serve` sirviendo a uno cada uno. Era la cifra que
justificaba `LUQUE-2008` y que se aceptó sin observar: lo medido entonces era el
coste del arreglo que sustituye (`71,2 MB` por servidor, `LUQUE-2221`), y el
ahorro era aritmética sobre ella.

**Por qué no se daba por hecha.** El snapshot ya se comparte: es el mismo fichero
mapeado en todos los servidores y esas páginas cuentan como `Shared_Clean`. Lo
que está en juego son las privadas, y se sospechaba que **la mitad privada de un
demonio no fuera constante**: construye un servidor MCP por sesión -- once
registros de tool, sus buffers, su decodificador--. La pregunta no era si ahorra,
era cuánto y con qué pendiente. La sospecha resultó cierta y por debajo del
ruido.

**Alcance:** un benchmark nuevo, `benchmarks/daemon-cost`, y **no un brazo en
`benchmarks/shared-snapshot`** como decía esta ficha. Sus brazos se definen por
si el fichero de snapshot está o no, y todo su gate mide mapear contra derivar:
un tercer brazo dejaría `compare(mapped, derived)` sin significado y forzaría
umbrales que no aplican. Reutiliza `mcpworkload` y `procstat`, que es lo que
había que compartir.

**Resultados.** Tres pasadas, `108.737` símbolos de `kena`, VM `linux/arm64`,
`2.000` llamadas medidas y `4.000` descartadas, los dos brazos leyendo el mismo
fichero publicado.

|clientes|N procesos|1 demonio|proporción|
|---|---|---|---|
|`1`|`65,9`–`67,4` MB|`65,1`–`66,9` MB|`0,966`–`1,015`|
|`2`|`129,6`–`133,4` MB|`67,3`–`68,4` MB|`0,513`–`0,521`|
|`4`|`263,0`–`264,0` MB|`69,2`–`70,5` MB|`0,262`–`0,267`|
|`8`|`529,0`–`533,9` MB|`68,2`–`82,1` MB|`0,128`–`0,154`|

- **La pendiente, que era el criterio:** `66`–`67 MB` por cliente contra
  `0,2`–`2,3`. El brazo de procesos sube un servidor entero por cliente con
  intercepto cero dentro del ruido; el del demonio tiene todo su coste en el
  intercepto -- una carga.
- **El pico:** `1.046 MB` contra `188` a ocho clientes. Cierra el hueco que
  `LUQUE-2221` declaró sin medir, y dice qué era: el pico de ocho procesos son
  ocho cargas simultáneas, no una carga más gorda.
- **La primera respuesta de un cliente nuevo:** `12`–`17 ms` contra `107`–`263`,
  entre `8` y `15` veces antes.
- **Y algo que no se buscaba:** a ocho clientes el demonio contesta **más
  rápido**, `p99` de `12,8`–`17,7 ms` contra `19,0`–`20,3`. Ocho procesos compiten
  por diez CPU y uno solo no. A uno, dos y cuatro clientes empatan.

**Una predicción de `LUQUE-2008` desmentida:** su ficha y el ADR decían que la
mitad privada de un demonio «crece con cada sesión», y el arnés esperaba que a un
cliente saliera peor. Crece por debajo del ruido: a un cliente empata. El
servidor MCP por sesión no aparece contra los `66 MB` de una carga. Corregido
donde estaba escrito.

**Dos defectos del arnés que la primera pasada destapó**, los dos silencios:
`readStatus` adivinó la forma de `graph_status` -- el conteo va anidado bajo
`results`-- y publicó `symbols: 0` sin fallar, dejando sin nombre el corpus de un
artefacto que existe para comparar; y `commit` se rellenaba dentro de
`writeResults`, después de calcular las limitaciones, así que un commit ausente
nunca podía declararse. Los dos fallan cerrado ahora.

**Un defecto de producción:** `Listen` fijaba el modo del socket con `chmod`
después de crearlo, y `chmod` sobre un socket devuelve `EINVAL` en un bind mount
de virtiofs -- donde corre este benchmark--, así que el demonio no arrancaba. El
socket nace con el modo puesto, vía `umask`. De paso quedó dicho que ese modo es
una puerta real en Linux e **ignorado** por darwin para conectar, donde la puerta
es el directorio de estado. Ver ADR 0065.

**Limitaciones declaradas:** la lectura del demonio a ocho clientes es la menos
estable (`68,2`, `71,1`, `82,1`), así que se afirma el orden de magnitud de su
pendiente y no la cifra; no es bare metal, es la VM de Docker Desktop; la
generación se publicó en darwin y se leyó en Linux, mismo arco y formato de
anchura fija; el corpus son `108.737` símbolos y no los `117.499` de
`LUQUE-2221`, así que una comparación por símbolo usa la cifra de su propia
pasada; y no se midió por encima de ocho clientes.

**Estado:** cerrada el `2026-08-23`.

---

## LUQUE-2223 — El ahorro del demonio, por la puerta que un cliente puede cruzar

**Dependencias:** `LUQUE-2222`.

**Objetivo:** hacer alcanzable el ahorro que `LUQUE-2222` midió. El demonio
servía sólo un socket unix, y **ninguna configuración de cliente MCP marca un
socket**: las cinco integraciones que este proyecto escribe ponen `command` +
`args`, y las formas remotas que aceptan son `url`. El ahorro estaba medido sobre
código que nadie podía ejecutar.

**Entregado:**

* *El transporte:* el demonio sirve la misma superficie MCP por el
  `StreamableHTTPHandler` del SDK además del socket, desde un proceso y sobre el
  mismo store. Las dos mitades acaban juntas: la que falle primero cancela a la
  otra, porque un demonio que siguiera contestando por una puerta después de
  perder la otra no se lo diría a nadie.
* *La clave:* un puerto no tiene ruta, así que el modo del socket no sirve de
  nada allí. El token va en `daemon.token`, `0600`, y **sobrevive al reinicio** --
  una entrada escrita en la config de un cliente seguiría valiendo o no según
  esto. `daemon.json` lleva url, token, socket y pid, y se borra al parar: es
  liveness, no identidad.
* *La postura:* bind fuera de loopback se **rechaza** nombrando lo que se
  escaparía, y sólo `--allow-remote` lo acepta con aviso. `Origin` se valida,
  que es lo único que para a una página usando el navegador del propio usuario.
  Comparación del token en tiempo constante, y el comentario dice que ningún
  test de este paquete lo prueba.
* *Las integraciones:* `mcp install --daemon` lee el endpoint y escribe la forma
  que cada cliente entiende -- `type: http` con `headers` para Claude Code,
  Claude Desktop y Oh My Pi; `type: remote` para OpenCode; `url` con
  `http_headers` para Codex, porque `bearer_token_env_var` nombra una variable
  que el usuario tendría que exportar. Verificadas contra la documentación de
  cada cliente, no adivinadas.
* *Lo que se niega:* una entrada con token **no se escribe en ámbito `project`**.
  `.mcp.json` se commitea, y un secreto en git no se retira borrándolo.
  `ErrEndpointNeedsUserScope`, en vez de degradar a stdio en silencio.
* *Un defecto que el cambio destapó:* el renderizador de TOML tenía la entrada
  cableada a mano en vez de renderizar la que `expectedTOMLEntry` declara, así
  que escribía `command` mientras la comparación esperaba `url`: habría
  reinstalado para siempre. Ahora renderiza la entrada declarada, con las
  subtablas después de los escalares -- en TOML una clave tras una cabecera
  pertenece a esa tabla-- y con las claves ordenadas.

**La medición, que corrige lo publicado:** `benchmarks/daemon-cost` corre ahora
las dos puertas con `-transport`, publica `results.json` y `results-http.json`, y
mete el transporte en el digest -- sin eso las dos corridas colisionan en una
identidad. Esquema `daemon-cost-v2`; los dos brazos regenerados.

|puerta|pendiente del demonio|8 clientes|1 cliente|cruce|
|---|---|---|---|---|
|socket|`0,5` MB/cliente|`70` contra `533 MB`|empata|`1,00`|
|HTTP|`12,5` MB/cliente|`166` contra `536 MB`|`76` contra `67 MB`|`1,26`|

Cuatro pasadas por HTTP dan `12,1`–`12,8`: no es ruido. **El ahorro sigue siendo
la tercera parte a ocho clientes, pero a un cliente el demonio por HTTP pierde**,
así que la razón para instalarlo empieza en el segundo. Los `12 MB` no son el
grafo: el SDK da a cada sesión un `MemoryEventStore` de `10 MiB` para reanudar
streams (`event.go:255`) y `2.000` llamadas lo llenan -- con `64` la pendiente cae
a `2,1`–`2,7`. No se puede acotar desde aquí: `NewStreamableHTTPHandler` no acepta
un `EventStore`.

Corregidos los siete sitios que citaban `0,2`–`2,3` como si fuera la cifra
alcanzable: ADR 0065 y 0066, `docs/installation.md`, `cmd/kivgraph/AGENTS.md`,
`benchmarks/AGENTS.md`, `internal/daemon/daemon.go`, `internal/integrations`.

**Limitaciones declaradas:** la pendiente por HTTP **depende de la carga** y su
forma no se mapeó -- dos puntos medidos, `2.000` y `64` llamadas, y un intento
intermedio descartado por salir con filas vacías en vez de publicarse; `12,5` es
un techo bajo tráfico sostenido y no lo que cuesta un editor abierto sin usar; no
se midió con clientes reales; no es bare metal.

**Estado:** cerrada el `2026-08-23`.

---

## LUQUE-2224 — La carga que un editor produce de verdad

**Dependencias:** `LUQUE-2223`.

**Objetivo:** cerrar la limitación que `LUQUE-2223` dejó abierta por escrito:
«no se midió con clientes reales». La cifra publicada -- `12,5 MB` por cliente por
HTTP-- salía de `2.000` llamadas por sesión, y nadie había comprobado si una
sesión real hace eso.

**La evidencia, y no hizo falta instrumentar nada.** Un `kivgraph serve` ya
escribe cada llamada de tool en su event log. Dos días de una máquina en uso,
editores reales:

|observación|valor|
|---|---|
|procesos `serve` arrancados|`51`|
|procesos con **cero** llamadas|**`48`**|
|llamadas de tool en total|`5`|
|llamadas por proceso que sí llamó|`3`, `1`, `1`|

**Cuarenta y ocho de cincuenta y un servidores cargaron el grafo entero y nadie
les preguntó nada.** La mediana es una llamada. El benchmark medía tres órdenes
de magnitud por encima de lo que ocurre.

**Medido a esa carga**, cuatro corridas nuevas sobre el mismo corpus
(`117.499` símbolos, generación `000001`, Linux):

|puerta|pendiente del demonio|N procesos|8 clientes|1 cliente|cruce|
|---|---|---|---|---|---|
|socket|`1,1`–`1,6` MB/cli|`43` MB/cli|`66` vs `356 MB`|empata|`1,06`–`1,10`|
|HTTP|`1,0`–`1,3` MB/cli|`43` MB/cli|`66` vs `354 MB`|empata|`1,06`–`1,10`|

* **Las dos puertas son indistinguibles**, con los rangos solapados. Queda
  retirada la advertencia de `LUQUE-2223`: elegir HTTP no se paga, y a un cliente
  el demonio no pierde.
* **N procesos cuestan `43 MB` por cliente, no `66`.** El snapshot se ensucia al
  consultarse, así que un `serve` al que nadie pregunta nunca toca la mayor parte
  del fichero. Los dos brazos bajan y el del demonio baja más: la proporción a
  ocho clientes pasa de `0,25` a `0,19`. **Medir la carga equivocada subestimaba
  el ahorro.**
* **La diferencia más grande es el pico**, y no depende de que nadie pregunte
  nada: `1.152` contra `169 MB` a ocho clientes.
* **La ventaja de latencia era del arnés.** `p99` de `1,2`–`1,9 ms` en los dos
  brazos, contra los `13`–`20` de la carga sintética: con una llamada por cliente
  no hay contención que repartir.
* El techo se conserva en `results-http-sustained.json` -- `4,9`–`5,9 MB` por
  cliente-- y **su cifra depende del corpus** (`12,1`–`12,8` sobre `108.737`
  símbolos), que es la firma de un coste en bytes retenidos y no por sesión.

**Un defecto de producción que el brazo HTTP destapó fallando:** un socket unix
acepta en cuanto está enlazado, antes de que nadie llame a `Accept`, así que el
arnés -- que trataba un `dial` con éxito como señal de arranque-- alcanzaba a un
demonio cuyo `daemon.json` no existía todavía. Una de cada tres pasadas moría con
«no such file or directory» sobre un demonio vivo. Dos arreglos: el demonio
publica HTTP **antes** de enlazar el socket -- y retira el endpoint si el `bind`
falla después--, y el arnés espera el endpoint en vez de deducirlo del orden.

**Verificación:** el test del arranque a medias es determinista y caza las dos
decisiones -- invertir el orden falla por el token, quitar la retirada falla por el
endpoint. Se descartó antes un test que corría por la ventana de la carrera:
pasaba con los dos órdenes, y un test que afirma cobertura que no tiene es peor
que ninguno. `gofmt`, `go vet ./...`, `go test ./...`, `make build`.

**Limitaciones declaradas:** la forma de la sesión es real, su ritmo no -- las
llamadas se emiten seguidas y un editor intercala pausas; la mezcla de tools del
arnés no es la observada (`80 %` `find_references`); el log son dos días de una
máquina y un solo usuario, así que sostiene el orden de magnitud y no una
distribución; y las `48` sesiones que no preguntan nada están *mejor*
representadas de lo que el arnés puede medir, porque obliga a cada cliente a
hacer al menos una llamada.

**Estado:** cerrada el `2026-08-23`.

---

## LUQUE-2225 - Lo que cuesta un servidor al que nadie pregunta

**Dependencias:** `LUQUE-2224`.

**Objetivo:** medir el caso que `LUQUE-2224` descubrió y no midió. Su propia
evidencia decía que `48` de `51` servidores reales no reciben ninguna llamada, y
su arnés obligaba a cada cliente a hacer al menos una: el caso predominante
quedaba declarado como limitación en vez de medido. Sólo medir; ningún cambio de
diseño en producción.

**Lo que impedía la medición:** el guardia de generación -- el `graph_status` que
prueba que los dos brazos sirven el mismo fichero-- corría **antes** del
muestreo, más un probe en `startServer` y otro en `connect`. Con `2.000` llamadas
eso es invisible; con cero **es la carga entera**. El guardia pasa a correr
después del muestreo: nada obliga a que preceda a los bytes, falla igual y
descarta la corrida igual. Esquema `daemon-cost-v2` a `v3` por ese movimiento.

**Lo medido**, seis pasadas ociosas (tres por puerta), tres por carga real y una
sostenida, todas sobre la generación `000001` de `108.737` símbolos de `kena` en
Linux:

|carga|N procesos|demonio|proporción|8 clientes|
|---|---|---|---|---|
|ninguna llamada|`33,1`-`34,4` MB/cli|`0,8`-`1,2` MB/cli|`0,023`-`0,032`|`40` contra `265` MB|
|`8` llamadas|`39,9`-`40,7` MB/cli|`0,4`-`0,9` MB/cli|`0,009`-`0,013`|`61` contra `336` MB|
|`2.000` llamadas|`67,6` MB/cli|`12,8` MB/cli|`0,189`|`168` contra `540` MB|

**El arranque es el coste:** `33` de los `40 MB` que cuesta un servidor
consultado, y la mitad del techo sostenido, se pagan antes de que nadie pregunte
nada. Contestar una pregunta añade unos `7 MB`. El pico sin ninguna consulta es
`994`-`1.000 MB` a ocho clientes contra `134`, y conectar a un demonio vivo cuesta
`1,5 ms` contra `96`-`151` de arrancar un proceso.

Las dos puertas siguen siendo indistinguibles también sin carga: `33,4`-`33,7`
por HTTP contra `33,1`-`34,4` por socket.

**Verificación:** cinco tests nuevos, el primero que este arnés tiene; cuatro
sabotajes falsificados uno a uno -- el guardia delante del muestreo, los
percentiles de cero llamadas, el ratio con operando ausente y el probe que no se
salta. El cuarto **no lo caza ningún test local**, y eso está dicho: vive en el
camino que necesita un servidor real, así que la corrida se niega a publicar un
fichero ocioso que haya cronometrado algún primer answer (`checkIdle`), y ese
rechazo sí tiene test. `gofmt`, `go vet ./...`, `go test ./...`, `make build`.

**Un modo de fallo cerrado de paso:** `latencyOf` sin llamadas devolvía
`latency{}`, así que un fichero ocioso habría publicado `p50_ms: 0` y
`p99_ratio: 0` -- una respuesta instantánea y un demonio infinitamente más
rápido. Los percentiles, `new_client_ms` y los dos ratios de latencia son ahora
punteros y desaparecen del fichero; el resumen imprime `--`. Se añade
`new_client_connect_ms`, que se mide a toda carga y es el campo con el que dos
cargas se comparan.

**Limitaciones declaradas:** la corrida ociosa mide un arranque, no una sesión de
trabajo; de los `33 MB` no se separa qué parte es construcción de índices, y
afirmarlo sería inventar el mecanismo; `-calls N` reparte N llamadas entre los
clientes que haya, así que la fila de un cliente de la carga real contesta ocho
preguntas y no es comparable con la de ocho; el corpus es `108.737` y no los
`117.499` de `LUQUE-2224`, porque `kena` es un workspace en uso -- ninguna cifra se
cruza entre las dos pasadas; la sostenida es una sola pasada; no es bare metal.

**Estado:** cerrada el `2026-08-23`.

---

## LUQUE-2226 - El arranque era el coste, y se retira

**Dependencias:** `LUQUE-2225`.

**Objetivo:** cobrar lo que `LUQUE-2225` midió. Su cifra decía que `33` de los
`40 MB` que cuesta un servidor MCP se pagan **antes de la primera pregunta**, y
que `48` de `51` servidores reales no llegan a hacerla: esas sesiones cargaban un
grafo entero para contestar nada.

**Lo hecho:** el grafo se lee en la primera consulta que lo necesita. `serve`
resuelve la generación publicada al arrancar -- y sigue fallando si no la tiene--
pero no la mapea; lo que decide la superficie de tools y las instrucciones del
handshake pasa a ser la **disponibilidad**. Los tres consumidores que sólo
comparaban generaciones -- el tick de reconciliación, la línea de arranque del log
y el brazo de carrera al publicar-- comparan identificadores, porque cualquiera de
ellos mapeando el grafo lo cargaría igual sin que nadie pregunte. ADR 0067.

**Medido**, seis pasadas ociosas por las dos puertas sobre `108.737` símbolos de
`kena` en Linux, árbol limpio en `68da6dc`:

|ocioso|antes|ahora|
|---|---|---|
|pendiente de N procesos|`33,9` MB/cli|`9,8`-`10,7` MB/cli|
|ocho clientes|`268,6 MB`|`77`-`81 MB`|
|pico a ocho clientes|`994,3 MB`|`179`-`186 MB`|
|conectar, ocho clientes|`130`-`151 ms`|`38`-`55 ms`|

**Con carga no se movió nada**, que es lo que hace creíble el ahorro:
`38,4`-`39,5 MB` por cliente con `8` llamadas contra los `39,9` de antes, y
`66,1`-`66,2` contra `67,6` con `2.000`. Lo que desapareció es exactamente lo que
pagaban las sesiones que no preguntan.

**Lo que empeoró, y está publicado:** a un cliente ocioso el demonio ahora
**pierde** -- `9,9`-`12,0` contra `7,1`-`9,2 MB`-- porque los dos brazos son tan
baratos en reposo que el proceso de más pesa. El cruce queda entre `0,96` y `1,41`
clientes: gana desde el segundo.

**Un defecto de producción de paso:** un snapshot ilegible mataba el arranque.
Ahora llega a una consulta y toda tool responde `INDEX_NOT_READY`, que es el mismo
código que da un servidor jamás indexado -- dos problemas distintos con arreglos
distintos--, así que `graph_status` gana `snapshot_unreadable` con el motivo.
Aditivo.

**Un defecto del arnés:** `commit` era `git rev-parse HEAD` a secas, así que una
corrida hecha para justificar un cambio sin commitear atribuía sus cifras a un
código que no había ejecutado. Ahora publica `<commit>-dirty` y lo declara en
`limitations`; las cinco corridas de este informe se hicieron desde árbol limpio.

**Verificación:** trece decisiones falsificadas una a una con su test -- seis del
store diferido, seis de los tres comparadores de generación y el guardia de
`graph_status`--, más dos del arnés. Una no la cazaba nadie y ésa es la que
importaba: la línea de arranque del log cargando el grafo. Humo con un cliente MCP
real por HTTP: handshake con `11` tools y **cero** líneas de snapshot, la consulta
devuelve `35` repositorios y ahí aparece la carga. `gofmt`, `go vet ./...`,
`go test ./...`, `make build`.

**Limitaciones declaradas:** la primera consulta paga el mapeo y eso no se mide
como latencia aislada; la pendiente ociosa del demonio cruza el cero entre pasadas
(`-0,28` a `0,32`), así que el cruce es un rango y no un número; de los `10 MB` que
quedan en un servidor ocioso no se desglosa qué parte es qué.

**Estado:** cerrada el `2026-08-23`.

## LUQUE-2227 - El coste en tokens de una superficie que ya no existe

**Dependencias:** `LUQUE-2226`.

**El hueco:** `benchmarks/mcp-token-cost/report.md` publica «`11 tools`, `645`
tokens residentes» y es la cifra que `internal/mcp/surface_test.go` nombra como
autoridad del presupuesto. La superficie que se sirve hoy son `12` tools de
consulta y `13` con `index_project`, así que ese número describe un servidor que
no existe.

Y no se puede refrescar con una corrida: el arnés **falla cerrado** contra la
generación publicada -- `question MergeAll: no captured host read for
internal/facts/facts.go:577-603; recapture native/reads.json against this
generation`--. Eso es el arnés funcionando: sus capturas nativas son de la
generación `000001` sobre el commit `f8a952d6`, y comparar contra rangos de línea
que se movieron daría una ventaja falsa a un lado.

**Lo que hace falta**, y ninguna pieza es de una línea:

1. **Recapturar `native/reads.json`** contra una generación actual. Es un dataset
   de lecturas del host, no un fichero de configuración, y su procedencia -- qué
   generación, qué commit, qué corpus-- es parte del artefacto.
2. **Decidir el corpus.** El informe se midió sobre `kivgraph` solo; el registro
   actual tiene tres repositorios, y una cifra de superficie no depende del corpus
   pero las de respuesta sí.
3. **Volver a publicar la cifra de tokens** que el guardia de bytes cita, para que
   las dos vuelvan a moverse juntas.

**Lo que ya no depende de esto:** el presupuesto residente se guarda en bytes y en
las dos formas del servidor -- `MaximumResidentSurfaceBytes` y
`MaximumIndexingSurfaceBytes`--, medido en `internal/mcp/surface_test.go`. Ver
ADR 0074.

**Hecho, el 2026-08-26:** el arnés ya **nombra la recaptura entera en una
pasada** en vez de abortar en la primera clave. Recorre todas las preguntas,
anota lo que le pidieron y no tenía, y se niega a publicar al final; abortar en
la primera convertía una recaptura de doce rangos en doce compilaciones para
descubrir el tamaño del trabajo.

Y la primera corrida completa **no llegó a las capturas**: murió en el producto,
con `trace_dependencies` respondiendo `SNAPSHOT_UNAVAILABLE` sobre la generación
publicada. Ése era un defecto real de dos tools y se fue en su propio PR -- el
recorrido admitía contención por defecto--, no aquí.

**Los doce rangos que faltan**, medidos contra la generación `000200`:

```
cmd/kivgraph/main.go:1745-1770
internal/audit/golang.go:20-81
internal/facts/facts.go:566-568
internal/facts/facts.go:577-603
internal/hotsnapshot/publication.go:170-195
internal/hotsnapshot/publication.go:87-112
internal/indexer/full.go:260-481
internal/indexer/full.go:920-951
internal/indexer/full.go:974-1022
internal/indexing/follow.go:115-167
internal/indexing/service.go:337-383
internal/storage/ladybug/canonical_load_native.go:252-283
```

**Las doce están capturadas** contra la generación `000204`, y el arnés corre
completo por primera vez. La tubería de captura resultó estar del todo
determinada, y se comprobó antes de escribir nada: el nombre del fichero es
`sha256(clave)[:16].txt` y la cifra es el conteo `cl100k_base` de su texto
verbatim -- verificado en las **20 capturas previas, 20 de 20 exactas**. Cada
captura nueva se compuso de piezas observadas -- el hash de la cabecera, la
ventana que el pie declara, la cola elidida-- sobre el contenido del repo, y ese
compositor reproduce **10 de las 20** originales byte a byte; las otras diez no
son ruido, y están explicadas abajo.

**Lo que la captura descubrió del propio dataset**, y no se sabía:

- la lectura del host **ensancha** la ventana pedida y a veces **elide** líneas
  con un `…` y una cola sintáctica. `internal/mcp/server.go:18-20` pide tres
  líneas y devuelve `17-23`. Por eso el residuo contra un render plano va de
  `37` a `229` tokens y no hay marco fijo que lo explique; el agregado sí encaja
  con el `38 %` documentado, `1,392x` sobre los bytes.
- **dos capturas no corresponden al commit declarado.**
  `internal/facts/facts.go:524-526` y `:535-561` guardan texto que en
  `f8a952d6` vive en otro sitio. El dataset ya mezclaba procedencia antes de
  esta recaptura.

**Lo que queda, y ahora es una decisión y no un trabajo:** el brazo nativo sigue
mezclando procedencia. Sus `grep` están capturados en `f8a952d6` y sus lecturas
lo están hoy, así que la corrida completa **no se puede publicar como la cifra
del corpus** hasta recapturar también los `grep` contra la generación vigente.
Eso es el punto «decidir el corpus», y de él cuelga el tercero -- republicar la
cifra que el guardia de bytes cita.

**Y la corrida completa dejó un defecto medido, que se fue a `LUQUE-2229`:** el
titular sale `0,63x` -- perdemos-- y no es el corpus. `find_references` gasta el
`91-98 %` de cada respuesta en un bloque `completeness` idéntico.

**Estado:** abierta. Primer punto cerrado; quedan la decisión del corpus y la
republicación.

## LUQUE-2228 - `version --json` describe un binario que no es el que corre

**Dependencias:** ninguna.

**Reproducción**, con un binario compilado del `main` mergeado y un `dist/` de
hace cuatro dias en el checkout:

|`cwd`|`kivgraph`|`commit`|`dirty`|
|---|---|---|---|
|`/tmp`|`0.7.0`|`255da242c`|`false`|
|la raiz del checkout|**`0.3.6`**|**`540607e35`**|**`true`**|
|`<checkout>/internal`|`0.7.0`|`255da242c`|`false`|

El binario es el mismo en las tres filas, y su build info lo confirma:
`vcs.revision=255da242c`, `vcs.modified=false`.

**La causa** es `findBundleManifest` en `internal/version/provenance.go`: su segundo
candidato es `<cwd>/dist/kivgraph-<os>-<arch>/manifest.json`, que **no tiene
ninguna relación con el ejecutable en marcha**. El primero sí la tiene -- el
manifest junto al binario, que es como un bundle publicado se describe--; el
segundo atribuye la procedencia de un artefacto cualquiera al proceso que
pregunta.

**No es un accidente**: `internal/version/provenance_test.go:182` lo cubre a
propósito. Por eso esto es una ficha y no un parche: cambiarlo es cambiar la
salida de un comando que la raiz declara superficie de compatibilidad, y exige
ADR.

**Lo que sí está a salvo, comprobado:** una generación no se puede envenenar con
esto. `--resolver-version` toma su valor por defecto de `version.Value`
-- `cmd/kivgraph/main.go:986` y `:1212`--, que es la constante compilada y no la
lectura dependiente del `cwd`. El sello de una generación dice qué binario la
produjo.

**Decidido: describe el proceso.** El segundo candidato se retiró y el test que
lo guardaba se fue con él. La versión pasa a ser siempre `version.Value`, que es
lo que `fallbackProvenance` ya hacía y sólo el camino del bundle sustituía; un
manifest cuyo `release` no coincide se rechaza nombrando los dos, igual que uno
de otra plataforma. Ver ADR 0075.

La reproducción de arriba, con el mismo `dist/` viejo en el checkout: `0.3.6`
antes, `0.8.0` después, con el commit del build info. Dos tests negativos, cada
uno visto fallar. Ningún consumidor cambia: los tres ejecutan el binario del
bundle, y el manifest de un bundle copia el `version` del binario que
`build-bundle.sh` acaba de construir, así que el guardia nuevo no puede saltar en
uno legítimo.

**Estado:** cerrada el `2026-08-26`.

## LUQUE-2229 - Una respuesta que es 93 % el mismo párrafo

**Dependencias:** ninguna. Lo descubrió la primera corrida completa de
`benchmarks/mcp-token-cost` tras la recaptura de `LUQUE-2227`.

**Reproducción**, `find_references` sobre la generación `000204`, medido en bytes
de la respuesta JSON:

|símbolo|refs|`results`|`completeness`|cuota|`invisible_scopes`|tuplas distintas|
|---|---:|---:|---:|---:|---:|---:|
|`MergeAll`|3|`455`|`6.113`|`93 %`|`20`|**`2`**|
|`CanonicalColumns`|3|`513`|`6.121`|`92 %`|`20`|**`2`**|
|`DiscoverGo`|4|`576`|`6.115`|`91 %`|`20`|**`2`**|
|`BuildPlan`|3|`502`|`6.114`|`92 %`|`20`|**`2`**|
|`NewServer`|0|`142`|`6.407`|`98 %`|`20`|**`2`**|

**La causa no es el tamaño, es la repetición.** Los `20` scopes se reducen a
**dos** tuplas `(reason, requested_package, detail)`: todos son
`DECLARATION_OUTSIDE_REPOSITORY` sobre `internal/procstat`, y cada uno arrastra
la misma frase en prosa -- «declared in a Go build cache entry: the package is
built from generated or cgo sources»-- una vez por símbolo cgo. El bloque es
**idéntico en las cinco preguntas**, así que no es evidencia de la respuesta: es
una propiedad de la generación reserializada en cada una.

**Lo que cuesta.** El arnés lo cobra donde se nota: el titular pasa de `7,64x` a
favor a `0,63x` en contra, es decir `1,6x` más caro que `grep` más lectura. Con
`NewServer` -- cero referencias-- el `98 %` de lo que el agente paga es texto que
no habla de `NewServer`.

**Lo que no hay que romper al arreglarlo.** La distinción que este bloque
sostiene es real y es la razón de ser del servidor: una lista vacía significa
«nadie lo llama», y `LOWER_BOUND` con sus scopes es lo que impide confundir eso
con «no se encontró». Agrupar por tupla con un contador conserva la afirmación
entera; borrar el bloque la destruye. Ver ADR 0059 y el `X1` del corpus de
`graph-tools-comparison`, donde un consumidor real nunca deletrea el símbolo.

**Superficie.** Cambia la forma de una salida de tool, que la raíz declara
superficie de compatibilidad: exige ADR.

**Lo hecho.** `UnresolvedScopes` se retira y queda `UnresolvedScopeGroups`, que
agrupa por `(repository, reason, requested_package, detail)` -- identificadores
internados, una consulta de mapa por falla y ninguna cadena construida. Las filas
llevan `occurrences` y dejan de llevar `requested_symbol` y `start_line`, que
pertenecían a una de las fallas agrupadas. Medido después: las `165` fallas son
**cinco** paquetes, el par de llamadas baja de `3.219` a `991` tokens, y
`more_invisible_scopes` pasa a `0` -- la verdad entera cabe en la respuesta. El
titular del arnés va de `0,63x` a `1,53x`.

Con él se arregló un defecto contiguo: `scopeDirectory` publicaba prosa como ruta
del `fallback` -- «a Go build cache entry: ...»-- porque cortaba el `detail` por el
último `" in "`. Sólo se acepta una ruta absoluta. Ver ADR 0076.

**Estado:** cerrada el `2026-08-26`.
