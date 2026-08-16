# Matriz de resiliencia

## Alcance

Esta matriz cierra LUQUE-1208 y reúne los escenarios de fallo de LUQUE-1202 a
LUQUE-1207. Cada fila exige un invariante observable; que el proceso no se
bloquee o que una operación devuelva un error no basta por sí solo.

El gate `RESILIENCE_PASS` requiere que todas las filas estén en `PASS`, que los
controles sanos pasen y que las limitaciones estén documentadas sin ocultar
cobertura ausente.

## Matriz de escenarios

| Tarea | Fallo inyectado | Invariante exigido | Evidencia ejecutable | Estado |
| --- | --- | --- | --- | --- |
| LUQUE-1202 | Fallo en cada etapa de un full rebuild y cancelación durante la ejecución | `CURRENT`, bytes de la generación activa, snapshot servido y probes permanecen iguales; no se publica candidato ni queda una generación incompleta | `internal/rebuild/failure_test.go`: `TestRunFailureAtAnyStageLeavesCurrentGenerationUntouched`, `TestRunCancellationLeavesCurrentGenerationUntouched`, `TestRepeatedFailuresDoNotErodeTheActiveGeneration`; `internal/resilience/rebuild_test.go`: `TestFailedFullRebuildDoesNotChangeTheServedGraph` | PASS |
| LUQUE-1203 | Error de validación, fallo de aplicación del delta, fallo posterior al commit y cancelación | Un delta fallido no publica snapshot ni digest; la transacción real revierte las mutaciones; el siguiente delta válido puede aplicarse | `internal/indexer/delta_failure_test.go`: `TestUpdateNeverPublishesAfterAFailedStep`, `TestUpdateLeavesAStaleDigestWhenTheMutationOutlivesTheUpdate`, `TestUpdateHonoursCancellationBeforeTouchingTheGraph`; `internal/indexer/delta_rollback_native_test.go`: `TestUpdateDeltaRouteRollsBackOnRealStorage`, `TestUpdateDeltaRouteSucceedsAfterARollback` | PASS |
| LUQUE-1204 | Digest de snapshot ausente o corrupto y grafo no convertible | Un digest inválido no activa una generación no verificada; el snapshot se reconstruye desde el grafo válido; un grafo no convertible falla de forma explícita y no se publica | `internal/rebuild/snapshot_corruption_test.go`: `TestSnapshotGenerationRebuildsDespiteACorruptDigest`, `TestCorruptDigestBlocksRollbackUntilItIsRestored`, `TestSnapshotGenerationFailsLoudlyOnAnUnconvertibleGraph`; `internal/resilience/snapshot_test.go`: `TestCorruptSnapshotDigestDoesNotDisturbReaders`, `TestServiceRecoversByRebuildingAfterCorruption`, `TestUnbuildableGraphLeavesTheServiceHonest` | PASS |
| LUQUE-1205 | Base LadybugDB truncada o sobrescrita mientras existe un snapshot servido | El doctor detecta el daño sin repararlo; las lecturas y escrituras contra la base dañada fallan sin crear una base encima ni devolver un grafo parcial; el snapshot en memoria sigue sirviéndose sin cambios | `internal/storage/ladybug/corruption_native_test.go`: `TestDiagnoseStorageDetectsADamagedDatabaseFile`, `TestCorruptDatabaseRefusesWrites`, `TestCorruptDatabaseRefusesReads`, `TestHealthyDatabasePassesTheSameChecks`; `internal/resilience/database_native_test.go`: `TestCorruptDatabaseKeepsReadersServedAndIsReportedByDoctor` | PASS |
| LUQUE-1206 | Segundo proceso abre o muta la misma base | La segunda apertura falla antes de escribir con `ErrDatabaseLocked` y, en Linux, nombra los PIDs retenedores; una base dañada no se etiqueta como lock; al terminar el proceso retenedor la base vuelve a ser utilizable | `internal/storage/ladybug/duplicate_process_linux_test.go`: `TestSecondProcessIsRefusedWithALockedError`, `TestDamagedDatabaseIsNotReportedAsLocked` | PASS |
| LUQUE-1207 | Señal de apagado y cierre de MCP, watcher, worker, conexiones, snapshot y LadybugDB | Se cancela el contexto compartido; se cierran los recursos en orden `MCP → snapshot → watcher → worker → conexiones → LadybugDB`; se ejecutan todos los cierres aunque uno falle; los runners terminan y `serve` sale con código 0 | `internal/app/lifecycle_test.go`: `TestLifecycleShutdownCancelsRunnersClosesEveryResourceInOrderAndIsIdempotent`, `TestLifecycleClosesResourcesBeforeWaitingForDependentRunner`, `TestLifecycleShutdownContinuesAfterCloseFailure`; `internal/resilience/shutdown_test.go`: `TestLifecycleClosesMCPWatcherWorkerAndSnapshot`; `internal/app/shutdown_native_test.go`; `cmd/kivgraph/main_test.go`; smoke real de `serve` con `SIGTERM` | PASS |

## Controles positivos

Los escenarios de fallo no pueden pasar porque el sistema nunca funcionó. La
matriz exige estos controles:

- `internal/rebuild/failure_test.go`: `TestSuccessfulRebuildDoesChangeTheActiveGeneration`.
- `internal/storage/ladybug/corruption_native_test.go`:
  `TestHealthyDatabasePassesTheSameChecks`.
- `internal/resilience/snapshot_test.go`:
  `TestServiceRecoversByRebuildingAfterCorruption` y
  `TestUnbuildableGraphLeavesTheServiceHonest`.
- `internal/indexer/delta_rollback_native_test.go`:
  `TestUpdateDeltaRouteSucceedsAfterARollback`.

## Ejecución reproducible

Desde la raíz del repositorio, con el runtime LadybugDB obtenido por
`scripts/fetch-ladybug.sh`:

```bash
go test ./... -count=1
make test-ladybug
go vet ./...
go test -race ./internal/app ./internal/indexer ./internal/rebuild ./internal/resilience ./internal/tsworker -count=1
make build
```

`make test-ladybug` ejecuta la suite completa con `-tags ladybug`, incluyendo
las pruebas nativas de almacenamiento, delta, recuperación y apagado. La suite
sin tag cubre los contratos que no dependen del motor nativo; `go vet`, race y
el build verifican que la matriz no se apoya en una ejecución parcial.

## Resultados observados

```text
go test ./... -count=1: PASS; 19 paquetes con tests, 3 sin tests
make test-ladybug: PASS; suites nativas de LadybugDB y resiliencia
go vet ./...: PASS
go test -race ./internal/app ./internal/indexer ./internal/rebuild ./internal/resilience ./internal/tsworker -count=1: PASS; 5 paquetes
make build: PASS
smoke /tmp/kivgraph-1208 serve + SIGTERM: exit 0
```

La suite nativa se ejecutó mediante `make test-ladybug`, que obtiene la
biblioteca fijada y ejecuta `go test -tags ladybug ./...`; no se sustituyó por
la suite sin tag. El smoke se ejecutó sobre el binario compilado con
`go build -o /tmp/kivgraph-1208 ./cmd/kivgraph`.

Se comprobaron además los controles positivos enumerados arriba dentro de las
dos suites. No hubo fallos, warnings ni errores silenciados.

## Limitaciones conocidas

- El worker TypeScript se prueba con workers falsos y procesos auxiliares del
  propio test; la integración de `serve` todavía no instancia el pipeline
  completo de watcher, supervisor y LadybugDB.
- La corrupción de base se inyecta como truncado o sobrescritura del archivo.
  Una página dañada que LadybugDB acepte requiere las invariantes de
  `doctor graph`, no sólo el doctor de almacenamiento.
- La clasificación `ErrDatabaseLocked` que enumera PIDs usa `/proc/locks` y,
  por tanto, es específica de Linux. En otras plataformas el rechazo sigue
  siendo seguro pero puede conservar sólo el error genérico del motor.
- La exclusión de LUQUE-1206 cubre la misma base abierta por dos procesos. Dos
  rebuilds sobre generaciones distintas de una misma raíz todavía necesitarían
  un lock de raíz.
- `HotSnapshot` no se serializa: recuperar un snapshot corrupto significa
  reconstruirlo desde el grafo válido o seguir sirviendo el snapshot que ya
  estaba en memoria; no existe recuperación automática en `serve` todavía.
- Ningún escenario simula pérdida de alimentación, pérdida de caché del
  controlador o una restauración de backup; esos casos no se pueden declarar
  cubiertos por esta matriz.

## Resultado del gate

`RESILIENCE_PASS`

Todas las filas de LUQUE-1202 a LUQUE-1207 y los controles positivos pasan. Las
limitaciones están registradas arriba y no se presentan como cobertura.

