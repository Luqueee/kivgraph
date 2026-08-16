# ADR 0025: Soporte de desarrollo en macOS

- **Estado:** aceptada
- **Fecha:** 2026-08-09
- **Revisa:** ADR 0015

## Contexto

Kivgraph se construía y se verificaba únicamente en Linux `amd64`. En
`darwin/arm64` (macOS `26.6`, Apple M5, Go `1.25.7`) el binario con el tag
`ladybug` enlazaba y abría bases reales -el asset `liblbug-osx-universal`
fijado por `scripts/fetch-ladybug.sh` es universal, está firmado ad-hoc y
declara `@rpath/liblbug.dylib` como `install_name`-, pero `go test -tags
ladybug ./...` fallaba en `~130` pruebas repartidas por `15` paquetes.

Las causas no eran del motor. Eran seis suposiciones sobre Linux repartidas
por el código, más una política de rutas que en macOS se dispara siempre.

## Decisión

### Capacidad y reserva de espacio

`internal/storage/generation` tenía implementación `linux` y un `!linux` que
devolvía error. `filesystemCapacity` es el primer paso de `checkSpace`, así que
en macOS **ninguna generación se podía publicar**. `platform_darwin.go`
implementa las dos operaciones:

- `filesystemCapacity` usa `syscall.Statfs`.
- `preallocate` usa `fcntl(F_PREALLOCATE)` con `F_ALLOCATEALL` antes de
  `Truncate`. En APFS un `ftruncate` produce un archivo disperso que no reserva
  nada, y una reserva parcial no es una reserva.

### Estadísticas de proceso

Tres copias leían `/proc`: `internal/tsworker` para la memoria del worker y los
benchmarks `ladybug-batch` y `ladybug-bulk` para su propio RSS. Se sustituyen
por un único seam, `internal/procstat`, con `ResidentBytes(pid)`:

- Linux lee `/proc/<pid>/statm`.
- Darwin llama a `proc_pid_rusage(RUSAGE_INFO_V2)` de `libproc`, que es la
  interfaz que usan `ps` y `top` y responde por un proceso hijo del mismo
  usuario sin task port; `task_info` exigiría uno.
- El resto de plataformas devuelve `0`, que significa "desconocido" y nunca
  "no usó memoria".

La definición de la métrica de los benchmarks no cambia: siguen muestreando el
RSS actual y quedándose con el máximo.

### Inspección de locks de LadybugDB

macOS no tiene `/proc/locks`. El sustituto natural, `fcntl(F_GETLK)`, es una
trampa: POSIX libera **todos** los locks de registro que un proceso mantiene
sobre un archivo en cuanto ese proceso cierra cualquier descriptor sobre él.
Medido antes de decidir, con `LadybugDB v0.13.1` en macOS `26.6`: un proceso
observador veía `F_WRLCK` sobre `graph.db`, y `F_UNLCK` justo después de que el
propietario abriera un descriptor de sólo lectura y lo cerrara. Es decir: la
inspección habría desbloqueado el motor.

`doctor_lock_darwin.go` enumera descriptores con `libproc`
(`proc_listpids(PROC_UID_ONLY)` + `PROC_PIDLISTFDS` + `PROC_PIDFDVNODEINFO`) y
compara `vst_dev`/`vst_ino` con los del archivo. No abre nada. En macOS
"mantiene el archivo abierto" es la forma observable de "mantiene el lock del
motor", porque LadybugDB toma un lock exclusivo durante toda la vida de la base.
`TestInspectingLocksDoesNotReleaseThem` defiende la invariante.

Límite conocido: un holder de otro usuario -en la práctica, `root`- no es
visible, porque `proc_pidinfo` lo rechaza sin privilegios. La base se crea con
modo `0600`, así que ningún otro usuario sin privilegios puede abrirla. El
check informa de lo que observó y nunca inventa un holder.

### Contrato de eventos del watcher

`fsnotify` usa `kqueue` en macOS y eso cambia dos cosas observables:

1. Un archivo creado y escrito de una vez llega como un único `Create`. El
   backend sólo ve el cambio del directorio; el archivo no está vigilado hasta
   que ya existe, y para entonces la escritura terminó. Un consumidor trata
   `Create` como cambio de contenido, y `Reconciler` sigue siendo la ruta de
   recuperación de lo que un backend no reporta. Modificar un archivo que ya
   existía cuando se instaló la vigilancia sí es `Write` en todos los backends,
   y el test lo exige.
2. El backend mantiene **un descriptor por archivo y por directorio**. Medido
   sobre el propio checkout de Kivgraph: `787` descriptores para `659`
   archivos en `152` directorios vigilados. El techo por proceso es
   `kern.maxfilesperproc`, `92160` en macOS `26`. Un árbol mayor falla en `New`
   con un error que nombra el límite a subir, en vez de vigilar un subconjunto
   en silencio.

Cambiar a FSEvents eliminaría ambas diferencias y es la alternativa evaluada y
aplazada: exige una dependencia cgo sobre CoreFoundation y su propio ADR.

### Política de symlinks y tests

`inspectRepositoryPath` rechaza cualquier ruta con un componente symlink en su
ascendencia. En macOS `t.TempDir()` devuelve algo bajo `/var/folders`, y `/var`
es un symlink a `/private/var`, así que decenas de tests ejercitaban el rechazo
en vez de lo que probaban. **La política no se relaja**: es deliberada y las
rutas reales de un usuario (`/Users/...`) no la disparan. Los tests de
`workspace`, `goworkspace`, `goloader` e `indexer` usan
`internal/testsupport.TempDir`, que resuelve el realpath.

### Herramientas de checksum

`scripts/fetch-ladybug.sh` calculaba el digest con `sha256sum`, que macOS no
trae. Ahora prefiere `sha256sum`, cae a `shasum -a 256` y falla cerrado si no
hay ninguno; nunca deja pasar una biblioteca sin verificar.

### Actualizaciones

`update.Run` sigue rechazando cualquier plataforma que no sea `linux/amd64`,
porque no existe bundle publicado para otra. Los tests que ejercitan la
instalación se saltan fuera de esa plataforma y
`TestRunRefusesAPlatformWithoutAPublishedBundle` afirma el rechazo explícito.

## Consecuencias

- En `darwin/arm64`, desde un checkout, funcionan `go test ./...`,
  `make test-ladybug`, `make build` y el flujo `init` → `doctor` → `index
  --full` → `serve` con un `HOME` temporal. Verificado con el fixture
  `testdata/go/type-relations` copiado fuera del repositorio: generación
  `000001` publicada, `27` símbolos, integridad `0` violaciones, `doctor
  storage` con los `9` checks en `PASS` -incluido `lock`-, y una sesión MCP
  stdio real levantada por `benchmarks/mcp-stdio`.
- macOS **no** es todavía un objetivo de distribución. `scripts/install.sh`,
  `scripts/build-linux-amd64.sh`, el manifest de provenance y el workflow de
  release siguen siendo `linux/amd64`, y `kivgraph update` lo dice.
- El binario nativo debe enlazarse con un `-Wl,-rpath` que apunte a la
  biblioteca: el binding fijado añade el suyo hacia su directorio de módulo,
  que no contiene ninguna `dylib`, y sin `rpath` propio `dyld` aborta el
  arranque.
- Al empaquetar para macOS, el `RUNPATH` relativo es `@loader_path/../lib` y la
  biblioteca es `liblbug.dylib`. Copiarla conserva su firma ad-hoc; modificar
  su `install_name` la invalidaría y exigiría volver a firmarla.
