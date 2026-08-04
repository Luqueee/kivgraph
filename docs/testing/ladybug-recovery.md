# Recuperación de LadybugDB ante fallos

## Resultado

Estado global: **FAIL**. La base de entrada permaneció intacta: `true`.

- Commit medido: `02eef0f7485b0cf4ac47207275cc65a878644acf-dirty`
- Fecha: `2026-08-04T19:43:16Z`
- Plataforma: `linux/amd64`, `go1.24.4`
- Base: `43065344` bytes, SHA-256 `ada9dc0b704046c8b019e17efe3d443de58102b7a316964b9e105822ffc99191`

| Caso | Resultado | Duración ms | Observación |
| --- | --- | ---: | --- |
| `sigkill_during_insert` | PASS | 277.1 | all checks passed |
| `sigkill_before_commit` | PASS | 87.8 | all checks passed |
| `sigkill_during_bulk_load` | PASS | 285.6 | all checks passed |
| `reopen_after_crash` | PASS | 153.3 | all checks passed |
| `truncated_file` | PASS | 22.0 | all checks passed |
| `permission_denied_directory` | PASS | 12.7 | all checks passed |
| `simulated_disk_full` | FAIL | 4855.6 | ENOSPC status = "ENOSPC after_apply", want "ENOSPC apply" reopen recovered database: ladybug open: failed to open database with status 1 |

## Hallazgo crítico

El caso de disco lleno no es recuperable con el comportamiento observado. El shim se armó justo antes de `Writer.Apply`; `Apply` devolvió éxito sin ninguna escritura interceptada. El primer `ENOSPC` apareció después, durante el cierre (`ENOSPC after_apply`), y la copia dejó de poder abrirse. La API nativa de cierre no devuelve un error que Luque pueda propagar.

Este resultado queda registrado como **FAIL**, no como una recuperación soportada. LUQUE-0211 deberá diagnosticar esta condición y la estrategia operativa necesitará backups o reemplazo atómico de la base antes de considerar tolerado un agotamiento de disco.

## Metodología

Cada caso usa una copia privada de la base cargada. Los workers se ejecutan en procesos separados para que un `SIGKILL`, una base corrupta o un error nativo no comprometan el coordinador ni el artefacto de entrada.

- **Inserción interrumpida:** el worker confirma 32 `CREATE` dentro de una transacción y el coordinador envía `SIGKILL`.
- **Antes del commit:** el worker completa el `CREATE`, publica un marcador y queda bloqueado sin ejecutar `COMMIT`.
- **Carga masiva:** `COPY Symbol` consume al menos 1 MiB de un CSV de un millón de filas antes del `SIGKILL`; un marcador separado demuestra que `COPY` no había terminado.
- **Reapertura:** tras la caída se valida `Health`, un símbolo base, la ausencia del delta abortado y la persistencia de una transacción nueva después de una segunda reapertura.
- **Truncado y permisos:** ambos `Open` se aíslan en workers y deben devolver errores controlados, sin señales ni timeouts.
- **Disco lleno:** un shim `LD_PRELOAD` devuelve `ENOSPC` después de 8 KiB escritos únicamente sobre el descriptor de la copia. Después se comprueban rollback, reapertura y una escritura durable.

## Reproducción

```bash
CGO_ENABLED=1 \
CGO_LDFLAGS="-L/path/to/ladybug/lib -Wl,-rpath,/path/to/ladybug/lib" \
LD_LIBRARY_PATH=/path/to/ladybug/lib \
go run -tags ladybug ./benchmarks/ladybug-recovery \
  --database /tmp/luque-copy.db
```

## Límites

- The probes cover Linux process termination and filesystem-call faults, not machine power loss or storage-controller cache loss.
- The full-disk case injects ENOSPC at the libc boundary only for the copied database file.
- The permission case assumes the benchmark is not run as root.
- In the measured run, the first intercepted write occurred after Apply returned successfully; ENOSPC during close left the copied database unreopenable.
- Estas pruebas no sustituyen los backups ni validan todavía la política de recuperación operativa, que se expondrá mediante `luque doctor storage` en LUQUE-0211.
