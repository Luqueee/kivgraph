# LUQUE — Backlog de tareas para desarrollo asistido por IA

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

**Objetivo:** crear la estructura inicial del repositorio Luque.

**Acciones:**

* Crear el repositorio Git.
* Crear `go.mod`.
* Crear `README.md`.
* Crear `LICENSE`.
* Crear `.gitignore`.
* Crear `Makefile`.
* Crear los directorios principales.
* Inicializar el paquete `cmd/luque`.
* Añadir un comando que muestre la versión provisional.

**Estructura mínima:**

```text
luque/
├── cmd/luque/
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
go build ./cmd/luque
./luque version
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
* `serverInfo.version` coincide con `luque version`.
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
* Los errores nativos se convierten en errores propios de Luque.

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
luque benchmark generate-graph \
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
* Artefactos: `benchmarks/ladybug-incremental/results.json` y `benchmarks/ladybug-incremental/report.md`.

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

## LUQUE-0211 — Crear comando `luque doctor storage`

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

* `luque doctor storage --database PATH` informa los diez diagnósticos requeridos y devuelve `0` únicamente si todos están en `PASS`.
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
* Verificación: `go test ./...`, `go vet ./...`, `go test -race` del paquete y `go build ./cmd/luque` pasan; `go tool staticcheck ./internal/config` no reporta incidencias.
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
* Verificación: `go test ./...`, `go vet ./...`, `go test -race ./internal/config ./internal/workspace`, `go build ./cmd/luque`, `go test -tags ladybug ./...` y `go vet -tags ladybug ./...` pasan; `go tool staticcheck ./internal/config ./internal/workspace` no reporta incidencias.
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
* Verificación: `go test ./...`, `go vet ./...`, `go test -race ./internal/config ./internal/workspace`, `go build ./cmd/luque` y `go tool staticcheck ./internal/config ./internal/workspace` pasan.
* Verificación Ladybug: `go test -tags ladybug ./...` y `go vet -tags ladybug ./...` pasan con la biblioteca v0.19.0 fijada en `/tmp/luque-ladybug-v0.19.0/lib`.
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
* Verificación: `go test ./...`, `go vet ./...`, `go test -race ./internal/workspace`, `go build ./cmd/luque` y `go tool staticcheck ./internal/workspace` pasan.
* Verificación Ladybug: `go test -tags ladybug ./...` y `go vet -tags ladybug ./...` pasan con la biblioteca v0.19.0 fijada en `/tmp/luque-ladybug-v0.19.0/lib`.
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
* Verificación: `go test ./...`, `go vet ./...`, `go test -race ./internal/workspace`, `go build ./cmd/luque` y `go tool staticcheck ./internal/workspace` pasan.
* Verificación Ladybug: `go test -tags ladybug ./...` y `go vet -tags ladybug ./...` pasan con la biblioteca v0.19.0 fijada en `/tmp/luque-ladybug-v0.19.0/lib`.
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
* Verificación: `go test ./...`, `go vet ./...`, `go test -race ./internal/workspace`, `go build ./cmd/luque`, `go tool staticcheck ./internal/workspace`, smoke focal y suite Ladybug pasan.
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
`go test ./...`, `go vet ./...`, `go test -race ./...`, `go build ./cmd/luque`,
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

**Verificación:** `go test ./...`, `go vet ./...`, `go build ./cmd/luque`,
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
instalación, `pinned`: el compilador que Luque distribuye.

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
go build ./cmd/luque
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
go build ./cmd/luque
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
go build ./cmd/luque
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
go build ./cmd/luque
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
go build ./cmd/luque
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
go build ./cmd/luque
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
go build ./cmd/luque
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
go build ./cmd/luque
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

- [ ] Verificar dependencias y alcance.
- [ ] Completar acciones y entregables.
- [ ] Ejecutar pruebas y benchmarks aplicables.
- [ ] Verificar criterios de aceptación y el gate aplicable.
- [ ] Registrar resultados, limitaciones y siguiente tarea.

**Debe enlazar:**

```text
símbolo consumidor
→ símbolo fuente del provider
```

---

## LUQUE-0706 — Implementar referencias no resueltas TypeScript

**Dependencias:** LUQUE-0705.

**Checklist:**

- [ ] Verificar dependencias y alcance.
- [ ] Completar acciones y entregables.
- [ ] Ejecutar pruebas y benchmarks aplicables.
- [ ] Verificar criterios de aceptación y el gate aplicable.
- [ ] Registrar resultados, limitaciones y siguiente tarea.

**Razones mínimas:**

```text
PACKAGE_PROVIDER_NOT_FOUND
EXPORT_NOT_FOUND
DECLARATION_SOURCE_NOT_MAPPED
AMBIGUOUS_PACKAGE_PROVIDER
VERSION_MISMATCH
TYPECHECK_FAILED
```

---

## LUQUE-0707 — Crear fixture cross-repository positivo

**Dependencias:** LUQUE-0705.

**Checklist:**

- [ ] Verificar dependencias y alcance.
- [ ] Completar acciones y entregables.
- [ ] Ejecutar pruebas y benchmarks aplicables.
- [ ] Verificar criterios de aceptación y el gate aplicable.
- [ ] Registrar resultados, limitaciones y siguiente tarea.

**Crear repositorios sintéticos:**

```text
shared-library
consumer-a
consumer-b
```

Con imports directos, barrels y aliases.

---

## LUQUE-0708 — Crear fixture cross-repository negativo

**Dependencias:** LUQUE-0705.

**Checklist:**

- [ ] Verificar dependencias y alcance.
- [ ] Completar acciones y entregables.
- [ ] Ejecutar pruebas y benchmarks aplicables.
- [ ] Verificar criterios de aceptación y el gate aplicable.
- [ ] Registrar resultados, limitaciones y siguiente tarea.

**Incluir:**

* homónimo local;
* package duplicado;
* export ausente;
* versión incompatible;
* `.d.ts` sin source map;
* otro paquete con mismo símbolo.

---

## LUQUE-0709 — Medir precisión TypeScript

**Dependencias:** LUQUE-0707 y LUQUE-0708.

**Checklist:**

- [ ] Verificar dependencias y alcance.
- [ ] Completar acciones y entregables.
- [ ] Ejecutar pruebas y benchmarks aplicables.
- [ ] Verificar criterios de aceptación y el gate aplicable.
- [ ] Registrar resultados, limitaciones y siguiente tarea.

**Métricas:**

```text
true positives
false positives
false negatives
precision
recall
unresolved correctly classified
```

**Gate:**

```text
TYPESCRIPT_CROSS_REPO_PASS
```

Requisito obligatorio:

```text
false exact edges = 0
```

---

# 11. Fase 8 — Go

## LUQUE-0801 — Generar `go.work` sintético

**Dependencias:** TYPESCRIPT_CROSS_REPO_PASS.

**Checklist:**

- [ ] Verificar dependencias y alcance.
- [ ] Completar acciones y entregables.
- [ ] Ejecutar pruebas y benchmarks aplicables.
- [ ] Verificar criterios de aceptación y el gate aplicable.
- [ ] Registrar resultados, limitaciones y siguiente tarea.

**Ubicación:**

```text
~/.local/state/luque/go.work
```

**No modificar repositorios.**

---

## LUQUE-0802 — Implementar carga con `go/packages`

**Dependencias:** LUQUE-0801.

**Checklist:**

- [ ] Verificar dependencias y alcance.
- [ ] Completar acciones y entregables.
- [ ] Ejecutar pruebas y benchmarks aplicables.
- [ ] Verificar criterios de aceptación y el gate aplicable.
- [ ] Registrar resultados, limitaciones y siguiente tarea.

**Configurar flags semánticos completos.**

Debe soportar:

* context cancellation;
* errores parciales;
* módulos múltiples;
* replace directives.

---

## LUQUE-0803 — Extraer definiciones Go

**Dependencias:** LUQUE-0802.

**Checklist:**

- [ ] Verificar dependencias y alcance.
- [ ] Completar acciones y entregables.
- [ ] Ejecutar pruebas y benchmarks aplicables.
- [ ] Verificar criterios de aceptación y el gate aplicable.
- [ ] Registrar resultados, limitaciones y siguiente tarea.

**Usar:**

```text
TypesInfo.Defs
```

---

## LUQUE-0804 — Generar stable keys Go

**Dependencias:** LUQUE-0803.

**Checklist:**

- [ ] Verificar dependencias y alcance.
- [ ] Completar acciones y entregables.
- [ ] Ejecutar pruebas y benchmarks aplicables.
- [ ] Verificar criterios de aceptación y el gate aplicable.
- [ ] Registrar resultados, limitaciones y siguiente tarea.

**Usar:**

* module path;
* package path;
* objectpath;
* kind;
* repository.

---

## LUQUE-0805 — Extraer usos Go

**Dependencias:** LUQUE-0803.

**Checklist:**

- [ ] Verificar dependencias y alcance.
- [ ] Completar acciones y entregables.
- [ ] Ejecutar pruebas y benchmarks aplicables.
- [ ] Verificar criterios de aceptación y el gate aplicable.
- [ ] Registrar resultados, limitaciones y siguiente tarea.

**Usar:**

```text
TypesInfo.Uses
TypesInfo.Selections
```

---

## LUQUE-0806 — Extraer llamadas directas

**Dependencias:** LUQUE-0805.

**Checklist:**

- [ ] Verificar dependencias y alcance.
- [ ] Completar acciones y entregables.
- [ ] Ejecutar pruebas y benchmarks aplicables.
- [ ] Verificar criterios de aceptación y el gate aplicable.
- [ ] Registrar resultados, limitaciones y siguiente tarea.

**Para cada `ast.CallExpr`:**

* resolver `Fun`;
* localizar objeto;
* crear `CALLS_DIRECT`.

---

## LUQUE-0807 — Extraer callbacks

**Dependencias:** LUQUE-0806.

**Checklist:**

- [ ] Verificar dependencias y alcance.
- [ ] Completar acciones y entregables.
- [ ] Ejecutar pruebas y benchmarks aplicables.
- [ ] Verificar criterios de aceptación y el gate aplicable.
- [ ] Registrar resultados, limitaciones y siguiente tarea.

**Para cada argumento de llamada:**

* determinar si es función o método;
* crear `PASSES_AS_CALLBACK`;
* no crear `CALLS_DIRECT` al callback.

---

## LUQUE-0808 — Resolver métodos y receivers

**Dependencias:** LUQUE-0805.

**Checklist:**

- [ ] Verificar dependencias y alcance.
- [ ] Completar acciones y entregables.
- [ ] Ejecutar pruebas y benchmarks aplicables.
- [ ] Verificar criterios de aceptación y el gate aplicable.
- [ ] Registrar resultados, limitaciones y siguiente tarea.

**Usar `TypesInfo.Selections`.**

---

## LUQUE-0809 — Resolver módulos cross-repository

**Dependencias:** LUQUE-0804 y LUQUE-0805.

**Checklist:**

- [ ] Verificar dependencias y alcance.
- [ ] Completar acciones y entregables.
- [ ] Ejecutar pruebas y benchmarks aplicables.
- [ ] Verificar criterios de aceptación y el gate aplicable.
- [ ] Registrar resultados, limitaciones y siguiente tarea.

**Usar:**

```text
module registry
import path
objectpath
go.work sintético
replace
```

---

## LUQUE-0810 — Implementar unresolved Go

**Dependencias:** LUQUE-0809.

**Checklist:**

- [ ] Verificar dependencias y alcance.
- [ ] Completar acciones y entregables.
- [ ] Ejecutar pruebas y benchmarks aplicables.
- [ ] Verificar criterios de aceptación y el gate aplicable.
- [ ] Registrar resultados, limitaciones y siguiente tarea.

**Razones:**

```text
MODULE_PROVIDER_NOT_FOUND
PACKAGE_NOT_LOADED
OBJECT_PATH_UNAVAILABLE
AMBIGUOUS_MODULE_PROVIDER
REPLACE_CONFLICT
TYPECHECK_FAILED
```

---

## LUQUE-0811 — Crear fixtures Go positivos

**Dependencias:** LUQUE-0809.

**Checklist:**

- [ ] Verificar dependencias y alcance.
- [ ] Completar acciones y entregables.
- [ ] Ejecutar pruebas y benchmarks aplicables.
- [ ] Verificar criterios de aceptación y el gate aplicable.
- [ ] Registrar resultados, limitaciones y siguiente tarea.

**Incluir:**

* direct call;
* callback;
* method;
* package alias;
* módulo consumidor;
* módulo proveedor;
* replace válido.

---

## LUQUE-0812 — Crear fixtures Go negativos

**Dependencias:** LUQUE-0809.

**Checklist:**

- [ ] Verificar dependencias y alcance.
- [ ] Completar acciones y entregables.
- [ ] Ejecutar pruebas y benchmarks aplicables.
- [ ] Verificar criterios de aceptación y el gate aplicable.
- [ ] Registrar resultados, limitaciones y siguiente tarea.

**Incluir:**

* homónimos;
* módulos duplicados;
* método del receiver incorrecto;
* callback con mismo nombre;
* replace conflictivo.

---

## LUQUE-0813 — Medir precisión Go

**Dependencias:** LUQUE-0811 y LUQUE-0812.

**Checklist:**

- [ ] Verificar dependencias y alcance.
- [ ] Completar acciones y entregables.
- [ ] Ejecutar pruebas y benchmarks aplicables.
- [ ] Verificar criterios de aceptación y el gate aplicable.
- [ ] Registrar resultados, limitaciones y siguiente tarea.

**Gate:**

```text
GO_SEMANTIC_PASS
```

Requisito:

```text
false exact edges = 0
```

---

# 12. Fase 9 — Grafo canónico

## LUQUE-0901 — Normalizar hechos semánticos

**Dependencias:** TYPESCRIPT_CROSS_REPO_PASS y GO_SEMANTIC_PASS.

**Checklist:**

- [ ] Verificar dependencias y alcance.
- [ ] Completar acciones y entregables.
- [ ] Ejecutar pruebas y benchmarks aplicables.
- [ ] Verificar criterios de aceptación y el gate aplicable.
- [ ] Registrar resultados, limitaciones y siguiente tarea.

**Crear un formato común para:**

* repositorios;
* paquetes;
* archivos;
* símbolos;
* aristas;
* evidencia;
* unresolved.

---

## LUQUE-0902 — Diseñar el esquema LadybugDB definitivo

**Dependencias:** LUQUE-0901.

**Checklist:**

- [ ] Verificar dependencias y alcance.
- [ ] Completar acciones y entregables.
- [ ] Ejecutar pruebas y benchmarks aplicables.
- [ ] Verificar criterios de aceptación y el gate aplicable.
- [ ] Registrar resultados, limitaciones y siguiente tarea.

**Crear nodos y relaciones definitivos.**

Documentar:

* claves primarias;
* multiplicidades;
* índices;
* constraints;
* propiedades;
* versión.

---

## LUQUE-0903 — Implementar full rebuild

**Dependencias:** LUQUE-0902.

**Checklist:**

- [ ] Verificar dependencias y alcance.
- [ ] Completar acciones y entregables.
- [ ] Ejecutar pruebas y benchmarks aplicables.
- [ ] Verificar criterios de aceptación y el gate aplicable.
- [ ] Registrar resultados, limitaciones y siguiente tarea.

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

---

## LUQUE-0904 — Implementar verificación de integridad

**Dependencias:** LUQUE-0903.

**Checklist:**

- [ ] Verificar dependencias y alcance.
- [ ] Completar acciones y entregables.
- [ ] Ejecutar pruebas y benchmarks aplicables.
- [ ] Verificar criterios de aceptación y el gate aplicable.
- [ ] Registrar resultados, limitaciones y siguiente tarea.

**Comprobar:**

```text
0 exact edges without source
0 exact edges without target
0 missing evidence files
0 duplicate stable keys
0 unknown confidence
0 invalid repository ownership
```

---

## LUQUE-0905 — Implementar backup y rollback

**Dependencias:** LUQUE-0903.

**Checklist:**

- [ ] Verificar dependencias y alcance.
- [ ] Completar acciones y entregables.
- [ ] Ejecutar pruebas y benchmarks aplicables.
- [ ] Verificar criterios de aceptación y el gate aplicable.
- [ ] Registrar resultados, limitaciones y siguiente tarea.

**Mantener:**

```text
graph.active
graph.next
graph.backup
```

---

## LUQUE-0906 — Construir HotSnapshot real

**Dependencias:** LUQUE-0904.

**Checklist:**

- [ ] Verificar dependencias y alcance.
- [ ] Completar acciones y entregables.
- [ ] Ejecutar pruebas y benchmarks aplicables.
- [ ] Verificar criterios de aceptación y el gate aplicable.
- [ ] Registrar resultados, limitaciones y siguiente tarea.

**Debe generarse desde el grafo definitivo.**

**Gate:**

```text
CANONICAL_GRAPH_PASS
```

---

# 13. Fase 10 — Incrementalidad

## LUQUE-1001 — Integrar `fsnotify`

**Dependencias:** CANONICAL_GRAPH_PASS.

**Checklist:**

- [ ] Verificar dependencias y alcance.
- [ ] Completar acciones y entregables.
- [ ] Ejecutar pruebas y benchmarks aplicables.
- [ ] Verificar criterios de aceptación y el gate aplicable.
- [ ] Registrar resultados, limitaciones y siguiente tarea.

**Objetivo:** detectar cambios en repositorios registrados.

---

## LUQUE-1002 — Implementar debounce y batching

**Dependencias:** LUQUE-1001.

**Checklist:**

- [ ] Verificar dependencias y alcance.
- [ ] Completar acciones y entregables.
- [ ] Ejecutar pruebas y benchmarks aplicables.
- [ ] Verificar criterios de aceptación y el gate aplicable.
- [ ] Registrar resultados, limitaciones y siguiente tarea.

**Defaults:**

```text
debounce: 150 ms
maximum batch: 500 ms
```

---

## LUQUE-1003 — Implementar content hashes

**Dependencias:** LUQUE-1001.

**Checklist:**

- [ ] Verificar dependencias y alcance.
- [ ] Completar acciones y entregables.
- [ ] Ejecutar pruebas y benchmarks aplicables.
- [ ] Verificar criterios de aceptación y el gate aplicable.
- [ ] Registrar resultados, limitaciones y siguiente tarea.

**No reindexar cuando el contenido no cambie.**

---

## LUQUE-1004 — Implementar reconciliación periódica

**Dependencias:** LUQUE-1003.

**Checklist:**

- [ ] Verificar dependencias y alcance.
- [ ] Completar acciones y entregables.
- [ ] Ejecutar pruebas y benchmarks aplicables.
- [ ] Verificar criterios de aceptación y el gate aplicable.
- [ ] Registrar resultados, limitaciones y siguiente tarea.

**Detectar:**

* eventos perdidos;
* archivos nuevos;
* eliminaciones;
* renombrados;
* manifest modificado.

---

## LUQUE-1005 — Implementar invalidación TypeScript

**Dependencias:** LUQUE-1002 y LUQUE-1003.

**Checklist:**

- [ ] Verificar dependencias y alcance.
- [ ] Completar acciones y entregables.
- [ ] Ejecutar pruebas y benchmarks aplicables.
- [ ] Verificar criterios de aceptación y el gate aplicable.
- [ ] Registrar resultados, limitaciones y siguiente tarea.

**Distinguir:**

```text
body only
signature
import
export
manifest
project config
```

---

## LUQUE-1006 — Implementar invalidación Go

**Dependencias:** LUQUE-1002 y LUQUE-1003.

**Checklist:**

- [ ] Verificar dependencias y alcance.
- [ ] Completar acciones y entregables.
- [ ] Ejecutar pruebas y benchmarks aplicables.
- [ ] Verificar criterios de aceptación y el gate aplicable.
- [ ] Registrar resultados, limitaciones y siguiente tarea.

**Distinguir:**

```text
body
signature
import
go.mod
replace
package deletion
```

---

## LUQUE-1007 — Implementar delta LadybugDB

**Dependencias:** LUQUE-1005 y LUQUE-1006.

**Checklist:**

- [ ] Verificar dependencias y alcance.
- [ ] Completar acciones y entregables.
- [ ] Ejecutar pruebas y benchmarks aplicables.
- [ ] Verificar criterios de aceptación y el gate aplicable.
- [ ] Registrar resultados, limitaciones y siguiente tarea.

**Debe:**

* borrar relaciones antiguas;
* actualizar nodos;
* insertar relaciones nuevas;
* actualizar unresolved;
* ejecutar una transacción.

---

## LUQUE-1008 — Implementar reconstrucción de snapshot tras delta

**Dependencias:** LUQUE-1007.

**Checklist:**

- [ ] Verificar dependencias y alcance.
- [ ] Completar acciones y entregables.
- [ ] Ejecutar pruebas y benchmarks aplicables.
- [ ] Verificar criterios de aceptación y el gate aplicable.
- [ ] Registrar resultados, limitaciones y siguiente tarea.

Primera versión:

```text
delta DB
→ rebuild completo HotSnapshot
→ atomic swap
```

Optimizar solo si excede el presupuesto.

---

## LUQUE-1009 — Probar altas

**Dependencias:** LUQUE-1008.

**Checklist:**

- [ ] Verificar dependencias y alcance.
- [ ] Completar acciones y entregables.
- [ ] Ejecutar pruebas y benchmarks aplicables.
- [ ] Verificar criterios de aceptación y el gate aplicable.
- [ ] Registrar resultados, limitaciones y siguiente tarea.

**Casos:**

* archivo;
* símbolo;
* export;
* consumidor;
* paquete.

---

## LUQUE-1010 — Probar modificaciones

**Dependencias:** LUQUE-1008.

**Checklist:**

- [ ] Verificar dependencias y alcance.
- [ ] Completar acciones y entregables.
- [ ] Ejecutar pruebas y benchmarks aplicables.
- [ ] Verificar criterios de aceptación y el gate aplicable.
- [ ] Registrar resultados, limitaciones y siguiente tarea.

**Casos:**

* cuerpo;
* firma;
* callback;
* import;
* provider.

---

## LUQUE-1011 — Probar eliminaciones

**Dependencias:** LUQUE-1008.

**Checklist:**

- [ ] Verificar dependencias y alcance.
- [ ] Completar acciones y entregables.
- [ ] Ejecutar pruebas y benchmarks aplicables.
- [ ] Verificar criterios de aceptación y el gate aplicable.
- [ ] Registrar resultados, limitaciones y siguiente tarea.

**Comprobar:**

* 0 ghost symbols;
* 0 ghost edges;
* referencias convertidas en unresolved;
* snapshot consistente.

---

## LUQUE-1012 — Benchmark incremental

**Dependencias:** LUQUE-1009, LUQUE-1010 y LUQUE-1011.

**Checklist:**

- [ ] Verificar dependencias y alcance.
- [ ] Completar acciones y entregables.
- [ ] Ejecutar pruebas y benchmarks aplicables.
- [ ] Verificar criterios de aceptación y el gate aplicable.
- [ ] Registrar resultados, limitaciones y siguiente tarea.

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

---

# 14. Fase 11 — Tools MCP

## LUQUE-1101 — Implementar respuesta estándar

**Dependencias:** INCREMENTAL_INDEXING_PASS.

**Checklist:**

- [ ] Verificar dependencias y alcance.
- [ ] Completar acciones y entregables.
- [ ] Ejecutar pruebas y benchmarks aplicables.
- [ ] Verificar criterios de aceptación y el gate aplicable.
- [ ] Registrar resultados, limitaciones y siguiente tarea.

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

---

## LUQUE-1102 — Implementar cursores

**Dependencias:** LUQUE-1101.

**Checklist:**

- [ ] Verificar dependencias y alcance.
- [ ] Completar acciones y entregables.
- [ ] Ejecutar pruebas y benchmarks aplicables.
- [ ] Verificar criterios de aceptación y el gate aplicable.
- [ ] Registrar resultados, limitaciones y siguiente tarea.

**Incluir:**

* snapshot;
* query hash;
* offset;
* sorting version;
* checksum.

---

## LUQUE-1103 — Implementar `list_repositories`

**Dependencias:** LUQUE-1101.

**Checklist:**

- [ ] Verificar dependencias y alcance.
- [ ] Completar acciones y entregables.
- [ ] Ejecutar pruebas y benchmarks aplicables.
- [ ] Verificar criterios de aceptación y el gate aplicable.
- [ ] Registrar resultados, limitaciones y siguiente tarea.

**Debe usar el HotSnapshot.**

---

## LUQUE-1104 — Implementar `find_symbol`

**Dependencias:** LUQUE-1101.

**Checklist:**

- [ ] Verificar dependencias y alcance.
- [ ] Completar acciones y entregables.
- [ ] Ejecutar pruebas y benchmarks aplicables.
- [ ] Verificar criterios de aceptación y el gate aplicable.
- [ ] Registrar resultados, limitaciones y siguiente tarea.

**Modos iniciales:**

```text
exact
qualified_exact
prefix
```

No fuzzy en el MVP.

---

## LUQUE-1105 — Implementar `get_symbol`

**Dependencias:** LUQUE-1104.

**Checklist:**

- [ ] Verificar dependencias y alcance.
- [ ] Completar acciones y entregables.
- [ ] Ejecutar pruebas y benchmarks aplicables.
- [ ] Verificar criterios de aceptación y el gate aplicable.
- [ ] Registrar resultados, limitaciones y siguiente tarea.

**Entrada principal:**

```text
stable_key
```

---

## LUQUE-1106 — Implementar `find_references`

**Dependencias:** LUQUE-1105.

**Checklist:**

- [ ] Verificar dependencias y alcance.
- [ ] Completar acciones y entregables.
- [ ] Ejecutar pruebas y benchmarks aplicables.
- [ ] Verificar criterios de aceptación y el gate aplicable.
- [ ] Registrar resultados, limitaciones y siguiente tarea.

**Filtros:**

* repo;
* language;
* edge kinds;
* confidence;
* incoming/outgoing.

---

## LUQUE-1107 — Implementar `find_cross_repo_consumers`

**Dependencias:** LUQUE-1106.

**Checklist:**

- [ ] Verificar dependencias y alcance.
- [ ] Completar acciones y entregables.
- [ ] Ejecutar pruebas y benchmarks aplicables.
- [ ] Verificar criterios de aceptación y el gate aplicable.
- [ ] Registrar resultados, limitaciones y siguiente tarea.

**Separar:**

```text
exact symbol consumers
package consumers
candidate consumers
unresolved consumers
```

---

## LUQUE-1108 — Implementar `trace_dependencies`

**Dependencias:** LUQUE-1106.

**Checklist:**

- [ ] Verificar dependencias y alcance.
- [ ] Completar acciones y entregables.
- [ ] Ejecutar pruebas y benchmarks aplicables.
- [ ] Verificar criterios de aceptación y el gate aplicable.
- [ ] Registrar resultados, limitaciones y siguiente tarea.

**Aplicar:**

* profundidad;
* límites;
* filtros;
* paginación;
* typed errors.

---

## LUQUE-1109 — Implementar `get_blast_radius`

**Dependencias:** LUQUE-1108.

**Checklist:**

- [ ] Verificar dependencias y alcance.
- [ ] Completar acciones y entregables.
- [ ] Ejecutar pruebas y benchmarks aplicables.
- [ ] Verificar criterios de aceptación y el gate aplicable.
- [ ] Registrar resultados, limitaciones y siguiente tarea.

**Agrupar por:**

* repositorio;
* paquete;
* profundidad;
* tipo de relación.

---

## LUQUE-1110 — Implementar `get_unresolved_references`

**Dependencias:** LUQUE-1101.

**Checklist:**

- [ ] Verificar dependencias y alcance.
- [ ] Completar acciones y entregables.
- [ ] Ejecutar pruebas y benchmarks aplicables.
- [ ] Verificar criterios de aceptación y el gate aplicable.
- [ ] Registrar resultados, limitaciones y siguiente tarea.

**Filtros:**

* repositorio;
* paquete;
* símbolo;
* motivo;
* lenguaje.

---

## LUQUE-1111 — Completar `graph_status`

**Dependencias:** todas las tools anteriores.

**Checklist:**

- [ ] Verificar dependencias y alcance.
- [ ] Completar acciones y entregables.
- [ ] Ejecutar pruebas y benchmarks aplicables.
- [ ] Verificar criterios de aceptación y el gate aplicable.
- [ ] Registrar resultados, limitaciones y siguiente tarea.

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

---

## LUQUE-1112 — Ejecutar pruebas de superficie negativa

**Dependencias:** LUQUE-1111.

**Checklist:**

- [ ] Verificar dependencias y alcance.
- [ ] Completar acciones y entregables.
- [ ] Ejecutar pruebas y benchmarks aplicables.
- [ ] Verificar criterios de aceptación y el gate aplicable.
- [ ] Registrar resultados, limitaciones y siguiente tarea.

**Comprobar que no existen:**

```text
execute_cypher
index
update
edit
run_command
register_repository
```

**Gate:**

```text
MCP_SURFACE_PASS
```

---

# 15. Fase 12 — Resiliencia

## LUQUE-1201 — Recuperar worker TypeScript

**Dependencias:** MCP_SURFACE_PASS.

**Checklist:**

- [ ] Verificar dependencias y alcance.
- [ ] Completar acciones y entregables.
- [ ] Ejecutar pruebas y benchmarks aplicables.
- [ ] Verificar criterios de aceptación y el gate aplicable.
- [ ] Registrar resultados, limitaciones y siguiente tarea.

**Probar:**

* SIGTERM;
* SIGKILL;
* crash loop;
* protocolo inválido;
* timeout.

**El último snapshot debe seguir disponible.**

---

## LUQUE-1202 — Probar fallo durante full rebuild

**Dependencias:** LUQUE-1201.

**Checklist:**

- [ ] Verificar dependencias y alcance.
- [ ] Completar acciones y entregables.
- [ ] Ejecutar pruebas y benchmarks aplicables.
- [ ] Verificar criterios de aceptación y el gate aplicable.
- [ ] Registrar resultados, limitaciones y siguiente tarea.

**El grafo activo no debe cambiar.**

---

## LUQUE-1203 — Probar fallo durante delta

**Dependencias:** LUQUE-1201.

**Checklist:**

- [ ] Verificar dependencias y alcance.
- [ ] Completar acciones y entregables.
- [ ] Ejecutar pruebas y benchmarks aplicables.
- [ ] Verificar criterios de aceptación y el gate aplicable.
- [ ] Registrar resultados, limitaciones y siguiente tarea.

**La transacción debe hacer rollback.**

---

## LUQUE-1204 — Probar snapshot corrupto

**Dependencias:** LUQUE-1201.

**Checklist:**

- [ ] Verificar dependencias y alcance.
- [ ] Completar acciones y entregables.
- [ ] Ejecutar pruebas y benchmarks aplicables.
- [ ] Verificar criterios de aceptación y el gate aplicable.
- [ ] Registrar resultados, limitaciones y siguiente tarea.

**Luque debe cargar el último snapshot válido o reconstruirlo.**

---

## LUQUE-1205 — Probar base corrupta

**Dependencias:** LUQUE-1201.

**Checklist:**

- [ ] Verificar dependencias y alcance.
- [ ] Completar acciones y entregables.
- [ ] Ejecutar pruebas y benchmarks aplicables.
- [ ] Verificar criterios de aceptación y el gate aplicable.
- [ ] Registrar resultados, limitaciones y siguiente tarea.

**Debe:**

* detectar;
* bloquear escrituras;
* conservar snapshot válido;
* informar mediante doctor.

---

## LUQUE-1206 — Probar proceso duplicado

**Dependencias:** LUQUE-1201.

**Checklist:**

- [ ] Verificar dependencias y alcance.
- [ ] Completar acciones y entregables.
- [ ] Ejecutar pruebas y benchmarks aplicables.
- [ ] Verificar criterios de aceptación y el gate aplicable.
- [ ] Registrar resultados, limitaciones y siguiente tarea.

**El segundo proceso debe fallar de manera clara y segura.**

---

## LUQUE-1207 — Probar apagado limpio

**Dependencias:** LUQUE-1201.

**Checklist:**

- [ ] Verificar dependencias y alcance.
- [ ] Completar acciones y entregables.
- [ ] Ejecutar pruebas y benchmarks aplicables.
- [ ] Verificar criterios de aceptación y el gate aplicable.
- [ ] Registrar resultados, limitaciones y siguiente tarea.

**Debe cerrar:**

* MCP;
* watcher;
* worker;
* conexiones;
* LadybugDB.

---

## LUQUE-1208 — Crear matriz de resiliencia

**Dependencias:** LUQUE-1202 a LUQUE-1207.

**Checklist:**

- [ ] Verificar dependencias y alcance.
- [ ] Completar acciones y entregables.
- [ ] Ejecutar pruebas y benchmarks aplicables.
- [ ] Verificar criterios de aceptación y el gate aplicable.
- [ ] Registrar resultados, limitaciones y siguiente tarea.

**Gate:**

```text
RESILIENCE_PASS
```

---

# 16. Fase 13 — Rendimiento

## LUQUE-1301 — Crear generador de workload MCP

**Dependencias:** RESILIENCE_PASS.

**Checklist:**

- [ ] Verificar dependencias y alcance.
- [ ] Completar acciones y entregables.
- [ ] Ejecutar pruebas y benchmarks aplicables.
- [ ] Verificar criterios de aceptación y el gate aplicable.
- [ ] Registrar resultados, limitaciones y siguiente tarea.

**Distribución:**

```text
40 % find_symbol
25 % get_symbol
20 % find_references
10 % find_cross_repo_consumers
5 % blast_radius
```

---

## LUQUE-1302 — Benchmark de un cliente

**Dependencias:** LUQUE-1301.

**Checklist:**

- [ ] Verificar dependencias y alcance.
- [ ] Completar acciones y entregables.
- [ ] Ejecutar pruebas y benchmarks aplicables.
- [ ] Verificar criterios de aceptación y el gate aplicable.
- [ ] Registrar resultados, limitaciones y siguiente tarea.

Medir p50, p95, p99 y allocations.

---

## LUQUE-1303 — Benchmark de 4 clientes

**Dependencias:** LUQUE-1302.

**Checklist:**

- [ ] Verificar dependencias y alcance.
- [ ] Completar acciones y entregables.
- [ ] Ejecutar pruebas y benchmarks aplicables.
- [ ] Verificar criterios de aceptación y el gate aplicable.
- [ ] Registrar resultados, limitaciones y siguiente tarea.

---

## LUQUE-1304 — Benchmark de 16 clientes

**Dependencias:** LUQUE-1303.

**Checklist:**

- [ ] Verificar dependencias y alcance.
- [ ] Completar acciones y entregables.
- [ ] Ejecutar pruebas y benchmarks aplicables.
- [ ] Verificar criterios de aceptación y el gate aplicable.
- [ ] Registrar resultados, limitaciones y siguiente tarea.

---

## LUQUE-1305 — Benchmark de 32 clientes

**Dependencias:** LUQUE-1304.

**Checklist:**

- [ ] Verificar dependencias y alcance.
- [ ] Completar acciones y entregables.
- [ ] Ejecutar pruebas y benchmarks aplicables.
- [ ] Verificar criterios de aceptación y el gate aplicable.
- [ ] Registrar resultados, limitaciones y siguiente tarea.

---

## LUQUE-1306 — Analizar perfiles

**Dependencias:** LUQUE-1305.

**Checklist:**

- [ ] Verificar dependencias y alcance.
- [ ] Completar acciones y entregables.
- [ ] Ejecutar pruebas y benchmarks aplicables.
- [ ] Verificar criterios de aceptación y el gate aplicable.
- [ ] Registrar resultados, limitaciones y siguiente tarea.

**Capturar:**

* CPU;
* heap;
* allocations;
* mutex;
* block;
* trace.

---

## LUQUE-1307 — Optimizar el primer cuello de botella real

**Dependencias:** LUQUE-1306.

**Checklist:**

- [ ] Verificar dependencias y alcance.
- [ ] Completar acciones y entregables.
- [ ] Ejecutar pruebas y benchmarks aplicables.
- [ ] Verificar criterios de aceptación y el gate aplicable.
- [ ] Registrar resultados, limitaciones y siguiente tarea.

No optimizar más de una categoría por tarea.

Prioridad:

```text
allocations
serialization
indexes
traversal
snapshot layout
```

---

## LUQUE-1308 — Repetir benchmark tras optimización

**Dependencias:** LUQUE-1307.

**Checklist:**

- [ ] Verificar dependencias y alcance.
- [ ] Completar acciones y entregables.
- [ ] Ejecutar pruebas y benchmarks aplicables.
- [ ] Verificar criterios de aceptación y el gate aplicable.
- [ ] Registrar resultados, limitaciones y siguiente tarea.

**Debe incluir comparación antes/después.**

---

## LUQUE-1309 — Verificar regresiones de precisión

**Dependencias:** LUQUE-1308.

**Checklist:**

- [ ] Verificar dependencias y alcance.
- [ ] Completar acciones y entregables.
- [ ] Ejecutar pruebas y benchmarks aplicables.
- [ ] Verificar criterios de aceptación y el gate aplicable.
- [ ] Registrar resultados, limitaciones y siguiente tarea.

Toda optimización debe ejecutar la suite semántica completa.

**Gate:**

```text
PERFORMANCE_PASS
```

---

# 17. Fase 14 — Observabilidad

## LUQUE-1401 — Implementar logging estructurado

**Dependencias:** PERFORMANCE_PASS.

**Checklist:**

- [ ] Verificar dependencias y alcance.
- [ ] Completar acciones y entregables.
- [ ] Ejecutar pruebas y benchmarks aplicables.
- [ ] Verificar criterios de aceptación y el gate aplicable.
- [ ] Registrar resultados, limitaciones y siguiente tarea.

**Formato:** JSON a stderr.

---

## LUQUE-1402 — Implementar métricas internas

**Dependencias:** LUQUE-1401.

**Checklist:**

- [ ] Verificar dependencias y alcance.
- [ ] Completar acciones y entregables.
- [ ] Ejecutar pruebas y benchmarks aplicables.
- [ ] Verificar criterios de aceptación y el gate aplicable.
- [ ] Registrar resultados, limitaciones y siguiente tarea.

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

---

## LUQUE-1403 — Exponer estado mediante `graph_status`

**Dependencias:** LUQUE-1402.

**Checklist:**

- [ ] Verificar dependencias y alcance.
- [ ] Completar acciones y entregables.
- [ ] Ejecutar pruebas y benchmarks aplicables.
- [ ] Verificar criterios de aceptación y el gate aplicable.
- [ ] Registrar resultados, limitaciones y siguiente tarea.

No requiere un endpoint de red adicional.

---

## LUQUE-1404 — Integrar OpenTelemetry opcional

**Dependencias:** LUQUE-1402.

**Checklist:**

- [ ] Verificar dependencias y alcance.
- [ ] Completar acciones y entregables.
- [ ] Ejecutar pruebas y benchmarks aplicables.
- [ ] Verificar criterios de aceptación y el gate aplicable.
- [ ] Registrar resultados, limitaciones y siguiente tarea.

**Condiciones:**

* exportadores desactivados por defecto;
* impacto de rendimiento medido;
* ninguna dependencia de un collector.

---

## LUQUE-1405 — Medir overhead de observabilidad

**Dependencias:** LUQUE-1404.

**Checklist:**

- [ ] Verificar dependencias y alcance.
- [ ] Completar acciones y entregables.
- [ ] Ejecutar pruebas y benchmarks aplicables.
- [ ] Verificar criterios de aceptación y el gate aplicable.
- [ ] Registrar resultados, limitaciones y siguiente tarea.

**Gate:**

```text
OBSERVABILITY_PASS
```

---

# 18. Fase 15 — Distribución

## LUQUE-1501 — Crear build Linux amd64

**Dependencias:** OBSERVABILITY_PASS.

**Checklist:**

- [ ] Verificar dependencias y alcance.
- [ ] Completar acciones y entregables.
- [ ] Ejecutar pruebas y benchmarks aplicables.
- [ ] Verificar criterios de aceptación y el gate aplicable.
- [ ] Registrar resultados, limitaciones y siguiente tarea.

**Incluir:**

* binario Go;
* worker;
* LadybugDB;
* grammars;
* licencias;
* manifest.

---

## LUQUE-1502 — Implementar `luque version --json`

**Dependencias:** LUQUE-1501.

**Checklist:**

- [ ] Verificar dependencias y alcance.
- [ ] Completar acciones y entregables.
- [ ] Ejecutar pruebas y benchmarks aplicables.
- [ ] Verificar criterios de aceptación y el gate aplicable.
- [ ] Registrar resultados, limitaciones y siguiente tarea.

**Mostrar:**

* Luque;
* commit;
* Go;
* Node;
* TypeScript;
* LadybugDB;
* binding;
* schema;
* resolver;
* grammars.

---

## LUQUE-1503 — Crear checksums

**Dependencias:** LUQUE-1501.

**Checklist:**

- [ ] Verificar dependencias y alcance.
- [ ] Completar acciones y entregables.
- [ ] Ejecutar pruebas y benchmarks aplicables.
- [ ] Verificar criterios de aceptación y el gate aplicable.
- [ ] Registrar resultados, limitaciones y siguiente tarea.

Generar SHA-256 de todos los artefactos.

---

## LUQUE-1504 — Crear instalación local

**Dependencias:** LUQUE-1503.

**Checklist:**

- [ ] Verificar dependencias y alcance.
- [ ] Completar acciones y entregables.
- [ ] Ejecutar pruebas y benchmarks aplicables.
- [ ] Verificar criterios de aceptación y el gate aplicable.
- [ ] Registrar resultados, limitaciones y siguiente tarea.

**Comandos esperados:**

```bash
luque init
luque doctor
luque index --full
luque serve
```

---

## LUQUE-1505 — Implementar upgrade de schema

**Dependencias:** LUQUE-1504.

**Checklist:**

- [ ] Verificar dependencias y alcance.
- [ ] Completar acciones y entregables.
- [ ] Ejecutar pruebas y benchmarks aplicables.
- [ ] Verificar criterios de aceptación y el gate aplicable.
- [ ] Registrar resultados, limitaciones y siguiente tarea.

**Debe incluir:**

* detección;
* backup;
* migración;
* validación;
* rollback.

---

## LUQUE-1506 — Probar rollback de versión

**Dependencias:** LUQUE-1505.

**Checklist:**

- [ ] Verificar dependencias y alcance.
- [ ] Completar acciones y entregables.
- [ ] Ejecutar pruebas y benchmarks aplicables.
- [ ] Verificar criterios de aceptación y el gate aplicable.
- [ ] Registrar resultados, limitaciones y siguiente tarea.

---

## LUQUE-1507 — Crear documentación de instalación

**Dependencias:** LUQUE-1504.

**Checklist:**

- [ ] Verificar dependencias y alcance.
- [ ] Completar acciones y entregables.
- [ ] Ejecutar pruebas y benchmarks aplicables.
- [ ] Verificar criterios de aceptación y el gate aplicable.
- [ ] Registrar resultados, limitaciones y siguiente tarea.

---

## LUQUE-1508 — Ejecutar build reproducible

**Dependencias:** LUQUE-1503.

**Checklist:**

- [ ] Verificar dependencias y alcance.
- [ ] Completar acciones y entregables.
- [ ] Ejecutar pruebas y benchmarks aplicables.
- [ ] Verificar criterios de aceptación y el gate aplicable.
- [ ] Registrar resultados, limitaciones y siguiente tarea.

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

- [ ] Verificar dependencias y alcance.
- [ ] Completar acciones y entregables.
- [ ] Ejecutar pruebas y benchmarks aplicables.
- [ ] Verificar criterios de aceptación y el gate aplicable.
- [ ] Registrar resultados, limitaciones y siguiente tarea.

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

---

## LUQUE-1602 — Ejecutar corpus grande sintético

**Dependencias:** LUQUE-1601.

**Checklist:**

- [ ] Verificar dependencias y alcance.
- [ ] Completar acciones y entregables.
- [ ] Ejecutar pruebas y benchmarks aplicables.
- [ ] Verificar criterios de aceptación y el gate aplicable.
- [ ] Registrar resultados, limitaciones y siguiente tarea.

Escala:

```text
1.000.000 símbolos
10.000.000 aristas
```

Registrar si el hardware disponible lo permite.

---

## LUQUE-1603 — Auditar aristas exactas

**Dependencias:** LUQUE-1601.

**Checklist:**

- [ ] Verificar dependencias y alcance.
- [ ] Completar acciones y entregables.
- [ ] Ejecutar pruebas y benchmarks aplicables.
- [ ] Verificar criterios de aceptación y el gate aplicable.
- [ ] Registrar resultados, limitaciones y siguiente tarea.

**Requisito:**

```text
0 false exact edges
0 dangling exact edges
```

---

## LUQUE-1604 — Auditar referencias no resueltas

**Dependencias:** LUQUE-1601.

**Checklist:**

- [ ] Verificar dependencias y alcance.
- [ ] Completar acciones y entregables.
- [ ] Ejecutar pruebas y benchmarks aplicables.
- [ ] Verificar criterios de aceptación y el gate aplicable.
- [ ] Registrar resultados, limitaciones y siguiente tarea.

Comprobar que cada unresolved tiene:

* motivo;
* evidencia;
* repo;
* archivo;
* lenguaje.

---

## LUQUE-1605 — Emitir informe final

**Dependencias:** LUQUE-1602, LUQUE-1603 y LUQUE-1604.

**Checklist:**

- [ ] Verificar dependencias y alcance.
- [ ] Completar acciones y entregables.
- [ ] Ejecutar pruebas y benchmarks aplicables.
- [ ] Verificar criterios de aceptación y el gate aplicable.
- [ ] Registrar resultados, limitaciones y siguiente tarea.

**Entregable:**

```text
docs/release/production-qualification.md
```

**Decisiones válidas:**

```text
ACCEPT_LUQUE_FOR_PRODUCTION
ACCEPT_LUQUE_WITH_LIMITS
REJECT_LUQUE_FOR_PRODUCTION
```

---

# 20. Gates globales

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
```

No se puede aprobar Luque sin todos ellos.

---

# 21. Orden recomendado para la IA

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

No debe implementar TypeScript, Go ni Tree-sitter antes de que LadybugDB y el HotSnapshot hayan pasado sus benchmarks.

---

# 22. Plantilla de prompt para cada tarea

```text
Trabaja en la tarea <TASK-ID> del backlog de Luque.

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

# 23. Plantilla para revisar una tarea completada

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
