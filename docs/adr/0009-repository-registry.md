# ADR 0009: Registro en memoria de repositorios

- Estado: aceptada
- Fecha: 2026-08-05

## Contexto

`repositories.yaml` contiene la configuración declarativa, pero el indexador
necesita una representación operativa con la ruta canónica y el estado actual
del repositorio Git.

## Decisión

`internal/workspace.NewRegistry` construye un registro inmutable en memoria,
conserva el orden del archivo y permite búsquedas por nombre. Para cada entrada
registra nombre, ruta declarada, `realpath`, commit, branch, estado dirty,
lenguajes, manifests, roots y exclusiones.

La metadata Git se obtiene con `exec.CommandContext` sin shell desde el
`realpath`: `git rev-parse HEAD`, `git symbolic-ref` (con fallback para HEAD
desacoplado) y `git status --porcelain`. Las listas devueltas por el registro
son copias profundas para impedir mutaciones accidentales del índice interno.

El registro exige que la ruta exista, sea un directorio y sea un repositorio Git
operativo porque esos datos son necesarios para registrar commit y branch. No
realiza todavía comprobaciones entre repositorios de duplicados reales,
anidamiento, escapes, symlinks fuera del repositorio o permisos; esas reglas
pertenecen a LUQUE-0403. Tampoco descubre manifests automáticamente; esa
responsabilidad pertenece a LUQUE-0404 y LUQUE-0405.

## Consecuencias

- El estado Git refleja la lectura realizada al construir el registro y puede
  volver a obtenerse creando otro registro.
- Una cancelación de contexto detiene el registro antes de iniciar el siguiente
  repositorio y cancela comandos Git en curso.
- La configuración declarativa sigue siendo la fuente de orden y de los campos
  de languages, manifests, roots y exclusions.
