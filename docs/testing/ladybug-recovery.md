# Recuperación de LadybugDB ante fallos

## Resultado

Estado global: **PASS**. La base de entrada permaneció intacta: `true`.

- Commit medido: `d83058fa94eb5797b8355285ed60c72d6b944379-dirty`
- Fecha: `2026-08-04T20:59:56Z`
- Plataforma: `linux/amd64`, `go1.24.4`
- Base: `43290624` bytes, SHA-256 `11f9860e15f07981d4c5f1ddccf5e2c001cfa1c4ec060895ab95f50d9908e36a`

| Caso | Resultado | Duración ms | Observación |
| --- | --- | ---: | --- |
| `sigkill_during_insert` | PASS | 272.8 | all checks passed |
| `sigkill_before_commit` | PASS | 83.8 | all checks passed |
| `sigkill_during_bulk_load` | PASS | 274.5 | all checks passed |
| `reopen_after_crash` | PASS | 143.2 | all checks passed |
| `truncated_file` | PASS | 19.5 | all checks passed |
| `permission_denied_directory` | PASS | 11.3 | all checks passed |
| `simulated_disk_full` | PASS | 7196.8 | all checks passed |
| `generation_publication_enospc` | PASS | 6026.3 | all checks passed |

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
  --database /tmp/luque-copy.db
```

## Límites

- The probes cover Linux process termination and filesystem-call faults, not machine power loss or storage-controller cache loss.
- The full-disk case injects ENOSPC at the libc boundary only for the copied database file.
- The permission case assumes the benchmark is not run as root.
- The generation-publication cases inject directory and CURRENT failures through deterministic filesystem hooks.
- Estas pruebas no sustituyen los backups ni simulan pérdida de alimentación. Cubren la recuperación de Luque ante los puntos `ENOSPC` inyectados y la publicación de `CURRENT` en Linux.
