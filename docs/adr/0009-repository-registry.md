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

El registro exige que cada ruta exista, sea un directorio y tenga permisos
POSIX de lectura y búsqueda, sin ser world-writable. Antes de ejecutar Git,
`NewRegistry` canonicaliza cada path y rechaza cualquier componente symlink,
los realpaths duplicados, el anidamiento entre repositorios y las colisiones de
nombre sin distinguir mayúsculas. Los paths declarativos de `manifests`,
`roots` y `exclusions` se interpretan dentro del `realpath` del repositorio y
no pueden escapar de él. La validación no exige todavía que esos archivos o
directorios declarativos existan; `DiscoverTypeScript` realiza el descubrimiento
de manifests TypeScript y la resolución de referencias descrita a continuación.
El descubrimiento Go se describe a continuación; el registro de módulos queda para LUQUE-0407.

## Descubrimiento TypeScript

`internal/workspace.DiscoverTypeScript` recorre el árbol del repositorio con un
orden determinista y devuelve paths absolutos para `package.json`, `tsconfig`
y declaraciones de workspace. Omite `.git`, dependencias instaladas y symlinks,
además de las exclusiones configuradas.

Las declaraciones `workspaces` de `package.json` admiten la forma array y la
forma objeto de npm/Yarn. También se reconoce `pnpm-workspace.yaml`. Los
patrones se validan para impedir escapes del repositorio, pero se conservan
como patrones: la asignación de paquetes pertenece a LUQUE-0406.

Los `tsconfig*.json` se leen como JSONC para admitir comentarios y trailing
commas. Cada `references[].path` se resuelve relativo al `tsconfig` que lo
declara; las referencias a directorios apuntan a su `tsconfig.json`, deben
existir, ser regulares y permanecer dentro del realpath del repositorio.

## Descubrimiento Go

`internal/workspace.DiscoverGo` usa `golang.org/x/mod/modfile` para leer
`go.mod` y `go.work` sin ejecutar comandos externos. Detecta también `go.sum`
y paquetes agrupando archivos `.go` por directorio y leyendo solo su cláusula
`package`.

Los módulos declarados por `go.work use` se resuelven a su `go.mod` y deben
permanecer dentro del repositorio. Las sustituciones locales de `go.mod` y
`go.work` se canonicalizan, se conservan en la salida y no pueden escapar ni
atravesar symlinks. Las sustituciones remotas se conservan sin resolverlas.

El descubrimiento omite `.git`, `vendor`, dependencias instaladas, symlinks y
exclusiones configuradas. No carga tipos ni dependencias: esa responsabilidad
pertenece a la fase de carga con `go/packages` y al registro de módulos.

## Consecuencias

- El estado Git refleja la lectura realizada al construir el registro y puede
  volver a obtenerse creando otro registro.
- Una cancelación de contexto detiene el registro antes de iniciar el siguiente
  repositorio y cancela comandos Git en curso.
- La configuración declarativa sigue siendo la fuente de orden y de los campos
  de languages, manifests, roots y exclusions.
- La política de symlinks es deliberadamente estricta: un componente symlink
  en la ruta configurada del repositorio invalida la entrada, aunque su
  destino esté dentro del árbol.
- Los errores de límites de paths se producen antes de cualquier comando Git,
  por lo que una configuración inválida no deja un registro parcialmente
  construido.
