# ADR 0077: consentimiento y presentación del daemon local

- **Estado:** propuesta
- **Fecha:** 2026-08-30
- **Cambia el protocolo MCP:** no
- **Cambia el schema persistente:** no
- **Obliga a reconstruir:** no
- **Cambia la superficie del CLI:** sí
- **Relaja un contrato de la raíz:** no

## Contexto

`mcp install` escribe por defecto una entrada `url`, pero instalar un
supervisor es un efecto lateral distinto de editar un fichero de cliente. La
ejecución anterior podía intentar esa instalación sin pedir confirmación y, en
macOS, un puerto ocupado hacía que launchd reiniciara el daemon sin publicar
`daemon.json`.

Además, `daemon status` y varios informes de mantenimiento tenían una salida
lineal difícil de leer en un terminal, aunque ya existía una política común
para color y para no emitir ANSI en pipes.

## Decisión

Cuando `mcp install` necesita provisionar un supervisor ausente o stale, la
ejecución interactiva pregunta. Una respuesta negativa conserva la entrada
`stdio`; `--daemon` es consentimiento explícito y no muestra la pregunta. Sin
terminal, la operación no bloquea esperando una respuesta y conserva `stdio`.
La selección de clientes ocurre antes de esta decisión para que no se arranque
nada si el usuario cancela la selección.

`daemon status`, `graph status`, `rollback` y `snapshot` usan una tabla de
pares clave/valor en un terminal. La misma información sigue saliendo en el
formato lineal existente cuando la salida está redirigida. Los estados y
resultados usan la capa ANSI existente, que se desactiva con `NO_COLOR`,
`TERM=dumb` o una salida que no sea terminal.

El daemon prefiere `127.0.0.1:7788`. Cuando el llamador no pasó `--addr` y ese
puerto está ocupado, enlaza `127.0.0.1:0`, publica el puerto que obtuvo y lo
guarda en `daemon.port` con permisos `0600`. Un arranque posterior reutiliza
ese puerto. Un `--addr` explícito no hace fallback: si está ocupado, falla y
nombra la dirección.

La especificación del supervisor usa siempre la ruta de configuración resuelta.
Así `daemon status` sin repetir `--config` describe el mismo unit que
`mcp install`, en lugar de marcarlo stale por comparar una ruta vacía.

## Consecuencias

- La instalación automática sigue siendo opt-in desde una terminal y segura
  para scripts; quien necesite imponerla usa `--daemon`.
- Una máquina que ya usa `7788` no cambia de puerto. Una instalación nueva que
  encuentra la dirección ocupada obtiene un puerto estable, pero un cliente
  existente debe volver a ejecutar `mcp install` si el administrador cambia o
  libera manualmente la asignación.
- `daemon.port` es estado operativo derivado, no una generación del grafo. Se
  conserva junto a `daemon.token` y se reemplaza atómicamente.
- No se añade una dependencia de renderizado: la tabla usa la capa de salida
  que ya coloreaba ayuda, integraciones, logs y estadísticas.

## Verificación

Las pruebas cubren la negativa sin consentimiento, la reutilización de un
supervisor instalado, la selección de un puerto libre cuando `7788` está
ocupado, la persistencia del puerto y la alineación de la tabla. La salida
redirigida continúa siendo verificable con las aserciones existentes.
