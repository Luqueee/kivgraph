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
El descubrimiento Go se describe a continuación y el registro de módulos se
construye con `LUQUE-0407`.

## Descubrimiento TypeScript

`internal/workspace.DiscoverTypeScript` recorre el árbol del repositorio con un
orden determinista y devuelve paths absolutos para `package.json`, `tsconfig`
y declaraciones de workspace. Omite `.git`, dependencias instaladas y symlinks,
además de las exclusiones configuradas.

Las declaraciones `workspaces` de `package.json` admiten la forma array y la
forma objeto de npm/Yarn. También se reconoce `pnpm-workspace.yaml`. Los
patrones se validan para impedir escapes del repositorio, pero se conservan
como patrones: la asignación de paquetes pertenece a `LUQUE-0406`, que
construye el registro de providers nombrados.

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
exclusiones configuradas. La carga semántica recibe los patrones de paquete
descubiertos de cada módulo, no un `./...` global, para que esas exclusiones
también rijan durante `go/packages`. El registro no carga tipos ni
dependencias: esa responsabilidad pertenece a la fase de carga.

## Registro de módulos Go

`internal/workspace.NewGoModuleRegistry` compone `DiscoverGo` y crea un índice
inmutable por `module path` para cada repositorio. Cada provider conserva la
raíz y el manifest del módulo, `go.sum`, versión de Go, repositorio y los
paquetes cuya ruta pertenece al módulo más específico.

Los `replace` declarados en `go.mod` se conservan en `Replaces`. Los
`replace` declarados en los `go.work` que incluyen el módulo se conservan por
separado en `WorkspaceReplaces`, con duplicados exactos eliminados y
conflictos semánticos preservados para LUQUE-0408. Los módulos sin paquetes
siguen siendo providers válidos; los paquetes fuera de cualquier módulo no
crean providers.

Dos módulos del mismo repositorio no pueden declarar el mismo `module path`.
`List` y `Get` devuelven copias profundas y el índice se ordena por
`module path`. El registro no carga tipos ni dependencias: esa responsabilidad
continúa en la fase `go/packages`.

---

## Detección de providers ambiguos

`internal/workspace.DetectProviderConflicts` construye ambos registros por
repositorio y devuelve un `ProviderConflictReport` determinista. No selecciona
automáticamente un provider cuando dos repositorios declaran el mismo nombre
de paquete o `module path`.

Cada conflicto conserva la clase, el provider afectado, los repositorios,
manifests y, para paquetes, las versiones. Se emiten
`AMBIGUOUS_PACKAGE_PROVIDER` y `AMBIGUOUS_MODULE_PROVIDER` para duplicados;
cuando las versiones del mismo paquete difieren se añade
`PACKAGE_VERSION_MISMATCH`.

Los replacements de módulo se comparan después de combinar `go.mod` y
`go.work`, eliminando únicamente duplicados exactos. Cualquier conjunto
distinto para el mismo `module path` produce `MODULE_REPLACE_CONFLICT`;
además, replacements para el mismo módulo sustituido desde módulos
distintos se comparan por origen y destino. Los destinos locales distintos
también son conflictos. El reporte y sus listas devuelven copias profundas;
un reporte sin conflictos tiene `HasConflicts() == false`.

---

## Registro de paquetes TypeScript

`internal/workspace.NewTypeScriptPackageRegistry` compone el descubrimiento
TypeScript y crea un índice inmutable por nombre para cada repositorio. Solo
los `package.json` con `name` actúan como providers; los manifests sin nombre
son válidos como manifests de raíz y se omiten. Los nombres duplicados dentro
del mismo repositorio se rechazan y los providers privados se conservan para
permitir referencias internas.

Cada provider conserva nombre, versión, privacidad, repositorio, raíz,
manifest y `exports` JSON sin perder su forma original. `types` tiene
precedencia sobre `typings`; las rutas de declaraciones y exports relativos se
validan para que permanezcan dentro de la raíz del paquete, pero no se exige
que los artefactos generados existan todavía. El proyecto TypeScript más
profundo que contiene el paquete se registra como `ProjectPath`; la indexación
semántica invoca el worker una vez por provider nombrado con proyecto y pasa
ese path explícitamente. Los manifests sin proyecto no se fuerzan contra el
`tsconfig` de la raíz.

Las raíces fuente se derivan de `rootDirs`, `rootDir`, `include` y `files` del
proyecto; si no hay una raíz aplicable se usa la raíz del paquete. Las raíces
declarativas se derivan de `types` y `declarationDir`. Las listas y el JSON
devueltos por `List` y `Get` son copias profundas. La ambigüedad entre
repositorios y las incompatibilidades de versión se clasifican mediante
`DetectProviderConflicts`.

---

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
