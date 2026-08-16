# Recuperación de LadybugDB ante fallos

## Resultado

Estado global: **PASS**. La base de entrada permaneció intacta: `true`.

- Commit medido: `e7472c0f135df2e6152d96420a4f86223aa0b338-dirty`
- Fecha: `2026-08-05T18:43:23Z`
- Plataforma: `linux/amd64`, `go1.24.4`
- Base: `66936832` bytes, SHA-256 `6229a65c055316a5402d70d8da79e02bd7b67dcb840b3e4a1bb949826ff56031`

| Caso | Resultado | Duración ms | Observación |
| --- | --- | ---: | --- |
| `sigkill_during_insert` | PASS | 95.1 | all checks passed |
| `sigkill_before_commit` | PASS | 64.1 | all checks passed |
| `sigkill_during_bulk_load` | PASS | 270.0 | all checks passed |
| `reopen_after_crash` | PASS | 112.1 | all checks passed |
| `truncated_file` | PASS | 25.2 | all checks passed |
| `permission_denied_directory` | PASS | 11.2 | all checks passed |
| `simulated_disk_full` | PASS | 2530.2 | all checks passed |
| `generation_publication_enospc` | PASS | 6197.2 | all checks passed |

## Metodología

Cada caso usa una copia privada de la base cargada. Los workers se ejecutan en procesos separados para que un `SIGKILL`, una base corrupta o un error nativo no comprometan el coordinador ni el artefacto de entrada. Los casos `ENOSPC` publican mediante generaciones inmutables y `CURRENT`.

- **Inserción interrumpida:** el worker confirma 32 `CREATE` dentro de una transacción y el coordinador envía `SIGKILL`.
- **Antes del commit:** el worker completa el `CREATE`, publica un marcador y queda bloqueado sin ejecutar `COMMIT`.
- **Carga masiva:** `COPY Symbol` consume al menos 1 MiB de un CSV de un millón de filas antes del `SIGKILL`; un marcador separado demuestra que `COPY` no había terminado.
- **Reapertura:** tras la caída se valida `Health`, un símbolo base, la ausencia del delta abortado y la persistencia de una transacción nueva después de una segunda reapertura.
- **Truncado y permisos:** ambos `Open` se aíslan en workers y deben devolver errores controlados, sin señales ni timeouts.
- **Disco lleno:** el shim `LD_PRELOAD` daña únicamente una candidata privada. Se comprueba que `CURRENT`, su checksum y su reapertura quedan intactos; después se publica una generación nueva y se restaura la anterior.
- **Publicación:** fault injection devuelve `ENOSPC` durante el rename de la generación, escritura/fsync/rename de `CURRENT` y fsync del directorio de estado. Cada fallo debe conservar la generación activa.

## Reproducción

```bash
CGO_ENABLED=1 \
CGO_LDFLAGS="-L/path/to/ladybug/lib -Wl,-rpath,/path/to/ladybug/lib" \
LD_LIBRARY_PATH=/path/to/ladybug/lib \
go run -tags ladybug ./benchmarks/ladybug-recovery \
  --database /tmp/kivgraph-copy.db
```

## Límites

- The probes cover Linux process termination and filesystem-call faults, not machine power loss or storage-controller cache loss.
- The full-disk case injects ENOSPC at the libc boundary only for the copied database file.
- The permission case assumes the benchmark is not run as root.
- The generation-publication cases inject directory and CURRENT failures through deterministic filesystem hooks.
- Estas pruebas no sustituyen los backups ni simulan pérdida de alimentación. Cubren la recuperación de Kivgraph ante los puntos `ENOSPC` inyectados y la publicación de `CURRENT` en Linux.
