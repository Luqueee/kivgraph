# ADR 0078: la skill se instala una vez y se enlaza

- **Estado:** aceptada
- **Fecha:** 2026-08-27
- **Cambia el protocolo MCP:** no
- **Cambia el schema persistente:** no
- **Obliga a reconstruir:** no
- **Cambia una salida de tool:** no -- cambia dónde escribe
  `kivgraph skill install` en alcance de usuario, y añade un fichero canónico

## Lo que pasaba

La skill vive embebida en el binario y `skill install` copiaba esos bytes a la
ruta de cada cliente. Con tres clientes instalados había **tres copias** y
ninguna fuente.

Eso deja sin respuesta la pregunta obvia: qué se hace para cambiarla. Editar la
copia de un cliente no llega a los otros dos, y deja esa ruta en estado
`incompatible`, así que la siguiente instalación se niega a tocarla y una
instalación con `--force` se lleva el cambio por delante. La skill era, en la
práctica, de sólo lectura, y nada lo decía.

## Qué se decide

En **alcance de usuario** hay un fichero canónico y las rutas de los clientes
son enlaces simbólicos a él:

```
~/.config/kivgraph/skills/kivgraph/SKILL.md      <- se edita aquí
~/.claude/skills/kivgraph/SKILL.md                -> enlace
~/.agents/skills/kivgraph/SKILL.md                -> enlace
~/.config/opencode/skills/kivgraph/SKILL.md       -> enlace
```

Editarlo una vez alcanza a todos los clientes a la vez, sin reinstalar ninguno.

Va bajo `~/.config/kivgraph`, junto a `config.yaml`, y no bajo
`~/.local/share`: este fichero existe **para ser cambiado**, y `~/.config` es
donde alguien busca lo que se le permite tocar. Una skill que nadie encuentra es
una skill que nadie adapta.

### El alcance de proyecto sigue copiando

Y no es una limitación pendiente de arreglar. Una ruta de alcance de proyecto
vive dentro del repositorio y **se commitea**. Un enlace a una ruta absoluta bajo
el `$HOME` de quien lo instaló llegaría a cada clon -- y a CI-- apuntando a un
directorio que no existe: la skill funcionaría para uno y estaría rota para
todos los demás.

No hay caso de Windows que contestar, y es la única razón por la que la regla se
escribe en una línea: `New` rechaza cualquier `GOOS` que no sea `darwin` o
`linux`, así que la pregunta de si crear un enlace exige un proceso elevado no
llega hasta aquí. Un build que ampliara eso tendría que contestarla.

### Un upgrade no se lleva la edición por delante

`install` escribe el canónico **sólo** si no hay nada o si lo que hay es la skill
que este build trae. Un canónico distinto se deja exactamente como está, y ahí
está el sentido de tenerlo: reinstalar es lo que hace un upgrade, y no puede
descartar en silencio el cambio que hacía que mereciera la pena cambiarla.

`skill status` lo dice -- «that file carries local edits»-- para que nadie tenga
que hacer un diff para enterarse, y `--force` es como se recupera la versión que
el build trae.

### La migración desde las copias

Una copia idéntica a la que este build trae pasa a enlace **sin pedir
`--force`**: los bytes que hay son los que escribiríamos, así que no se pierde
nada. Se reporta como `superseded`, que es el vocabulario que este paquete ya
usa para «es nuestro, de otra forma».

Una copia **editada** es el único caso que sí pierde algo, porque se editó cuando
todavía no había un canónico que editar en su lugar. Se niega sin `--force`, y
con `--force` se guarda en `.kivgraph.bak` antes de que el enlace ocupe su sitio.

### Un `remove` no borra el canónico

Es el fichero al que se invitó a la gente a hacer cambios. Borrar una edición
porque se desregistró un cliente sería justo lo contrario de la razón de que
exista.

## Lo que no se toca

`readDestination` se niega a seguir un enlace, y hace bien: escribe entradas MCP
y ficheros de ganchos, y escribir a través de un enlace los pondría donde nadie
pidió. Aquí un enlace es el estado esperado, así que la inspección de skills usa
`Lstat` y **cuenta lo que hay** en vez de fallar; la regla de los demás ficheros
no se ha tocado.

Los clientes siguen los enlaces. El cargador general de skills de Claude Code no
los filtra; el *mount* de `memory-skills` sí rechaza una carpeta de skill que sea
un enlace -- registra `unsafe or symlinked skill folder`-- y por eso lo que se
enlaza aquí es el **fichero**, dentro de una carpeta real, y no la carpeta.
