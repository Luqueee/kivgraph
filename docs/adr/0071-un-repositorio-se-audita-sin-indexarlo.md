# ADR 0071: un repositorio se audita sin indexarlo

- **Estado:** aceptada
- **Fecha:** 2026-08-25
- **Cambia el protocolo MCP:** no
- **Cambia el schema persistente:** no
- **Obliga a reconstruir:** no
- **Cambia la superficie del CLI:** sí -- un comando nuevo,
  `doctor repositories`, y una invocación desnuda que ahora escribe la tabla
  de comandos en `stdout` y termina en `0` donde antes escribía un error en
  `stderr` y terminaba en `2`
- **Relaja un contrato de la raíz:** no

## Contexto: el único informe de cobertura costaba una pasada completa

Un agujero de cobertura se aprendía de una sola forma: ejecutar
`index --full` -- minutos-- y leer los avisos del final, donde conviven con
cientos de diagnósticos del analizador. Y algunos agujeros no tenían aviso
propio. Medido sobre un workspace de `52` repositorios registrados:

|lo que pasaba|cómo se veía antes|
|---|---|
|un repositorio sin proyecto|un aviso al final de una pasada de minutos|
|un proyecto que no reclama ningún fichero|**nada**: el paquete existe y su cuenta de ficheros es cero|
|un paquete Go excluido por build tags|un aviso, sin decir qué tag conceder|
|`cargo metadata` sin resolver|un `WARN` entre `118` diagnósticos de Rust|

## La decisión

`kivgraph doctor repositories` contesta por repositorio, sin indexar, con un
hallazgo por agujero: `code` estable, severidad, lo observado y un remedio.
`--json` es la salida para un agente.

**Toda comprobación pregunta al código que pregunta la pasada.** Es la regla
que sostiene el comando entero: una comprobación que reimplementara la
resolución de fuentes, el descubrimiento de paquetes o la selección de un
build tag contestaría por una pasada que nadie ejecuta -- y daría por bueno un
repositorio que la pasada descarta. De ahí dos exportaciones nuevas y ninguna
lógica nueva: `workspace.ClaimedTypeScriptSources` y
`goworkspace.PackagePatternsForModule`.

**Los patrones de Go se nombran uno a uno.** `go list -e ./...` **no** informa
de un paquete cuyos ficheros excluye la configuración de build: el patrón no lo
matchea. Medido en `api-db-go`: cero hallazgos con `./...`, los dos reales al
nombrar los paquetes, que es lo que la pasada ya hacía.

**Un remedio se propone, nunca se aplica.** Kivgraph no escribe dentro del
código que indexa (ADR 0050). Un remedio lleva ruta, contenido exacto, clave de
configuración o comando, y cuando pide conceder un build tag lo **nombra**,
leído de los ficheros con el parser de constraints del compilador: «concede el
tag» sin decir cuál no lo puede aplicar nadie.

**Sólo lo `blocking` falla.** Un hallazgo `partial` es un agujero que su dueño
puede haber elegido -- un árbol de tests fuera del `include`-- y un gate que
fallara por esos sería un gate que nadie puede pasar.

## Lo que la primera ejecución encontró, incluido un defecto propio

Sobre los `52` repositorios, en `34 s`:

- `1` blocking real: un repositorio de `.mjs` sin proyecto.
- `26` partial: ficheros que ningún proyecto reclama.
- `2` Go: paquetes excluidos, con el tag nombrado -- `tools` e `integration`.
- `2` blocking **falsos**, y ahí estaba el hallazgo que importaba: dos
  repositorios cuyo `tsconfig.json` declara `include: ["src"]` aparecían
  reclamando cero ficheros. El compilador lee una entrada que nombra un
  directorio como `src/**/*`; Kivgraph la matcheaba literal, así que alcanzaba
  el directorio y ningún fichero bajo él. El auditor encontró un defecto del
  indexador en su primera ejecución, y se corrigió antes de aceptar el comando.

## La invocación desnuda

`kivgraph` a secas escribe la tabla de comandos y termina en `0`. Es la misma
pregunta que `--help` -- «qué hace esto» -- y contestarla con un error en
`stderr` mandaba a buscar una ayuda que el lector todavía no había visto. Un
comando **desconocido** sigue siendo un error de una línea con código `2`: quien
teclea `inedx` no necesita el catálogo entero.

Era válido ayer y no se convierte en un fallo: el código de salida baja de `2`
a `0`, que es la dirección segura para cualquier script que ya lo invocara.
