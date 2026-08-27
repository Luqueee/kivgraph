# ADR 0077: una puerta delante de `grep` se cierra por ambigüedad

- **Estado:** aceptada
- **Fecha:** 2026-08-27
- **Cambia el protocolo MCP:** no
- **Cambia el schema persistente:** no
- **Obliga a reconstruir:** no
- **Cambia una salida de tool:** no -- añade `kivgraph hook run`, que no es una
  tool MCP sino un comando que los agentes ejecutan antes de las suyas

## Lo que pasaba

Un agente con Kivgraph instalado seguía haciendo `grep`. La tabla de
`AGENTS.md` dice qué tool contesta cada pregunta y el campo `instructions` del
servidor lo repite, pero las dos son texto en el contexto: compiten con todo lo
demás que hay allí y pierden en cuanto la sesión se alarga.

Los agentes de codificación exponen un gancho que se ejecuta **antes** de cada
tool y puede negarla. Eso no compite con nada: llega en el momento de la
llamada y la respuesta es una redirección concreta.

## Qué se decide

`kivgraph hook run` lee la llamada por la entrada estándar y contesta si el
grafo la contesta mejor. `kivgraph hook install|status|remove` lo registra en
los clientes, como tercer miembro de la familia que ya forman `mcp` y `skill`.

### La puerta se cierra por ambigüedad y por nada más

La negativa necesita un hecho positivo, y sólo uno resiste la medición: **el
nombre lo declaran dos cosas o más**. Con una declaración una búsqueda de texto
encuentra la cosa y todos sus usos; con dos no puede decir qué usos son de cuál,
y quien lee su salida tampoco.

Sobre las seis preguntas de `benchmarks/mcp-token-cost` -- commit `1a7d0266`,
tokenizador `cl100k_base` -- el factor se parte limpiamente por cuántas cosas
comparten el nombre, y **nada** por cuántas referencias tiene el símbolo:

| símbolo            | clase         | referencias | factor |
| ------------------ | ------------- | ----------: | -----: |
| `DiscoverGo`       | `rare_name`   |         `4` | `0,83` |
| `MergeAll`         | `rare_name`   |         `3` | `0,85` |
| `CanonicalColumns` | `rare_name`   |         `3` | `0,91` |
| `NewServer`        | `common_name` |         `0` | `1,68` |
| `BuildPlan`        | `shared_name` |         `3` | `1,92` |
| `Publish`          | `common_name` |         `3` | `6,42` |

Los tres por debajo de `1,0` los perdemos, y los tres son nombres sin homónimo.
Es la misma frontera que `AGENTS.md` ya documenta y que
`benchmarks/graph-tools-comparison/trivial.md` mide en `1,9x` **en contra**
sobre `newGMCClient`. Una puerta que negara esos tres cobraría al usuario por
un peor resultado.

### Lo que se escribió primero y se retiró

La primera versión tenía un segundo umbral: negar también un nombre único con
muchas referencias, porque leer cincuenta coincidencias cuesta más que preguntar
qué ficheros las tienen. El número puesto fue `8`.

No hay con qué defenderlo. Las seis preguntas del corpus tienen entre `0` y `4`
referencias, así que cualquier cifra por encima de `4` habría sido inventada, y
este repositorio no las admite. El umbral se retiró.

**Lo que haría falta para devolverlo:** extender `benchmarks/mcp-token-cost` con
nombres inequívocos de recuento alto -- del orden de `40` referencias o más -- y
medir dónde cruza el factor la unidad. Mientras no exista esa fila, un nombre
único se deja pasar por muchos sitios que lo usen.

### Un `allow` explícito no significa «adelante»

En el contrato de Claude Code y de Codex, `permissionDecision: "allow"`
**se salta la petición de permiso**. Un gancho que lo contestara a cada llamada
sobre la que no tiene opinión -- que son casi todas -- auto-aprobaría en
silencio cada comando de shell de la sesión, incluidos los que el usuario
configuró para que preguntaran.

Por eso un permiso **no escribe nada**. Es el único significado seguro de «no
tengo opinión», y deja el flujo de permisos del agente exactamente donde estaba.

### Todo fallo es un permiso

Una entrada ilegible, una configuración ausente, un directorio que ningún
repositorio cubre, un demonio que no está o que no contesta: todos terminan en
silencio y en código de salida `0`. Un código distinto de cero **es en sí mismo
una negativa** en los dos agentes, así que un gancho que fallara y lo dijera
bloquearía justo la llamada sobre la que acababa de no formarse una opinión.

El gancho tampoco arranca el demonio. Se ejecuta sobre la pulsación del usuario,
delante de una tool que está esperando; arrancar un indexador ahí convertiría un
`grep` en un minuto de espera.

## Qué clientes la alojan

| cliente          | mecanismo             | fichero                          |
| ---------------- | --------------------- | -------------------------------- |
| `claude-code`    | gancho de shell       | `.claude/settings.json`          |
| `codex`          | gancho de shell       | `.codex/hooks.json`              |
| `opencode`       | plugin generado       | `plugins/kivgraph.js`            |
| `claude-desktop` | no tiene              | --                               |
| `oh-my-pi`       | subsistema *legacy*   | --                               |

Claude Code y Codex leen el **mismo veredicto**, byte a byte, así que un solo
comando sirve a los dos; se diferencian en dónde se registra, no en lo que dice.
Ojo con Claude Code: sus ganchos viven en `settings.json`, no en el
`.claude.json` donde están sus servidores MCP. Son dos ficheros de un cliente.

OpenCode no puede ejecutar un binario: su `tool.execute.before` devuelve
`Promise<void>` y la única forma de parar una tool es lanzar un error. El plugin
generado es sólo esa traducción -- reenvía el mismo payload al mismo comando y
convierte una negativa en un `Error`.

Los otros dos se niegan por su nombre. Claude Desktop no tiene contrato previo a
la tool, y la documentación de Oh My Pi llama *legacy* a su subsistema de
ganchos y dice que el runtime usa un ejecutor de extensiones: escribir contra
eso sería escribir contra un blanco móvil.

## Un array no es un mapa

`mcpServers` es un mapa y nuestra clave es nuestra o de otro. `hooks.PreToolUse`
es un array que contiene **todas** las puertas que el usuario instaló, y otra
herramienta puede estar ya ahí. Así que la nuestra se localiza por el comando
que ejecuta y nunca por su posición, `install` añade en vez de sobrescribir, y
`remove` se lleva sólo la nuestra. No hay ningún estado `incompatible` posible
en ese fichero y `install` no necesita `--force`.

Reconocerla por el nombre base del binario fue un error, y lo destapó una
instalación de prueba: el ejecutable sólo se llama `kivgraph` cuando se instaló
con ese nombre exacto, y una compilación llamada `kivgraph-hook` registraba una
puerta que `status` daba luego por ausente y que `remove` se negaba a tocar --
una instalación repetible para siempre, apilando una copia cada vez. Todos los
tests unitarios usaban una ruta terminada en `kivgraph` y pasaban.

## Lo que cuesta

Medido sobre el grafo de este repositorio, con el demonio en marcha:

| camino                                            | por llamada |
| ------------------------------------------------- | ----------: |
| permitir sin tocar la red                          |     `~2 ms` |
| marcar el demonio y preguntar                      |   `~5,6 ms` |

La clasificación es pura y va **antes** de leer la configuración, así que casi
todas las llamadas salen por el primer camino. Sólo una búsqueda que ya se
parece a algo que el grafo contesta llega a la red.

Una sola llamada resuelve la pregunta: `find_references` ya se niega a elegir
cuando un nombre declara varios símbolos y nombra los candidatos al hacerlo, así
que la ambigüedad llega como error clasificado y no como una segunda pregunta.
La decisión se toma sobre el código `AMBIGUOUS_SYMBOL`, nunca sobre el texto del
mensaje, que es lo que `internal/mcp/tools/errors.go` pide de un cliente; el
mensaje se lee sólo para citar las filas que la negativa devuelve.

## La salida

`KIVGRAPH_DISABLE_HOOK=1` desactiva la puerta, y se lee en dos sitios: en el
entorno del propio comando, que es como un usuario la apaga para una sesión, y
al principio de la línea que se está evaluando, que es como funciona el consejo
que da la propia negativa.
