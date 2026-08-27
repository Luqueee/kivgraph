# ADR 0069: el demonio es el defecto

- **Estado:** aceptada
- **Fecha:** 2026-08-25
- **Cambia el protocolo MCP:** no
- **Cambia el schema persistente:** no
- **Obliga a reconstruir:** no
- **Cambia la superficie del CLI:** sí -- `mcp install` escribe una entrada `url`
  donde antes escribía `command`, y hay un flag nuevo `--stdio`. `--daemon`
  se sigue aceptando
- **Relaja un contrato de la raíz:** no

## Contexto: el régimen real es «muchos clientes, ninguna pregunta»

El ADR 0065 midió el ahorro y el ADR 0068 le dio dueño al demonio. Lo que
faltaba era cobrarlo por defecto, y la razón para hacerlo la da el event log de
una máquina en uso: **`69` arranques de `serve`, `66` sin una sola llamada de
tool, `5` llamadas en total**, con `8` procesos vivos a la vez. Un `update` de
esa máquina tuvo que parar ocho procesos uno a uno.

|con `8` clientes|N procesos `serve`|un demonio|
|---|---|---|
|páginas privadas, en reposo|`77`-`81 MB`|`10`-`13 MB`|
|contestando|`323`-`330 MB`|`60`-`62 MB`|
|pico|`179`-`186 MB`|`26`-`29 MB`|
|conectar|`38`-`55 ms`|`1,6`-`2,0 ms`|

El cruce está en `0,96`-`1,41` clientes: a uno es una moneda al aire, a ocho no
hay debate. Y hay dos cosas que sólo un proceso permite: un `update` que
reinicia en vez de matar ocho, y la certeza de que hay **uno**.

## La decisión

`kivgraph mcp install` escribe la entrada `url` por defecto. `--stdio` es la
salida explícita. `--daemon` se sigue aceptando y ya no cambia el resultado por
sí solo -- porque era válido ayer, y un flag que empezara a dar error rompería
todo script que ya lo pasa.

**Se asegura, no se detecta.** Cuando el transporte es el demonio, `mcp install`
instala el supervisor si falta, espera a que el endpoint contesta y lo dice paso
a paso. Detectar un demonio y escribir `url` en silencio haría que el mismo
comando escribiera dos ficheros distintos en dos días, que es lo que el diseño
anterior evitaba a propósito. Asegurar no tiene ese problema: el resultado no
depende del momento.

**Lo que sí decide el resultado son condiciones declaradas**, y ésa es la
distinción entera. Un lector puede predecirlas; el estado de la tabla de
procesos no:

|condición|entrada|
|---|---|
|ámbito `project`|`stdio`, porque una `url` lleva un token y ese fichero se commitea|
|plataforma sin supervisor|`stdio`, porque el demonio no tendría dueño ahí|
|sin configuración todavía|`stdio`, porque no hay directorio de estado al que apuntar|
|`--stdio`|`stdio`|
|el resto|`url` contra un demonio supervisado|

Las tres primeras se **nombran** en la salida. Una degradación silenciosa es
exactamente el defecto que esta costura existe para evitar.

**Un `--daemon` explícito no degrada.** Donde el defecto cae a `stdio`, quien lo
pidió a mano recibe un error: pidió la cosa que esta máquina no puede sostener.

**Escribir falla antes que mentir.** Si el supervisor se instala y el demonio no
contesta, el comando falla. Escribir la `url` daría a todos los clientes una
dirección muerta, y escribir `stdio` esconderá un demonio que no arranca.

**Leer nunca provisiona.** `mcp status` compara formas; no instala nada ni
arranca nada. Un comando de sólo lectura que arrancara un proceso de fondo por
haberle hecho una pregunta sería una sorpresa. Y compara contra la **identidad**
del endpoint publicado, no contra su vida: el token sobrevive a un reinicio, así
que un fichero publicado describe la entrada que esta configuración instala
aunque el demonio esté parado.

**La unit recuerda la configuración resuelta.** Un supervisor arranca el demonio
fuera de esta shell, así que un demonio que resolviera la suya podría servir un
directorio de estado distinto del que este comando acaba de dar a los clientes.

## La migración, que es la mitad del cambio

Cambiar el defecto sin más habría hecho que **la primera ejecución después del
cambio fallara en toda máquina que hubiera registrado un cliente alguna vez**: la
entrada `command` que el defecto anterior escribió se leía como «incompatible» y
exigía `--force`. Eso es lo mismo que decir que el defecto nuevo no funciona.

Así que hay un estado nuevo, `superseded`: una entrada que es exactamente lo que
un `kivgraph mcp install` anterior escribió con el otro transporte. Se sustituye
sin `--force`, entera, porque `command` junto a `url` bajo una clave son dos
registros y el cliente elige transporte **por la forma**.

`--force` conserva su sentido: protege lo que esta herramienta no escribió. Por
eso el lado `stdio` se compara **incluyendo el ejecutable** -- una entrada que
nombra otro binario `kivgraph` pertenece a otra instalación, y quedarse con sus
clientes sin pedirlo sería peor que negarse. El lado `url` se compara por
estructura, porque el puerto y el token de un demonio anterior no son conocibles
desde aquí.

## Un defecto que el humo destapó: `--addr` nunca funcionó

`runDaemon` se construye **antes** de que `runConfiguredServe` parsee la línea de
comandos, y recibía sus opciones ya empaquetadas en un valor. Ese valor se
construía leyendo el struct de flags sin parsear, así que cada flag valía su
cero: `--addr` y `--allow-remote` estaban declarados en el flag set, salían en la
ayuda y los nombra el ADR 0066, y el demonio **sólo podía bindear
`127.0.0.1:7788`**.

Se vio arrancando un demonio con `--addr 127.0.0.1:7799` para el humo de este
cambio: publicó `7788`. Las opciones se leen ahora dentro del closure, que corre
después del parseo, y `TestTheDaemonFlagsReachTheBind` conduce el parseo en el
mismo orden que `main` -- flag set, runner, y sólo entonces los argumentos--
porque capturar por valor pasa cualquier test que rellene el struct antes de
construir el runner.

Entra en el alcance de este ADR porque `daemon install --addr` grabaría en la
unit un flag que el demonio ignora, y una unit que promete un bind que no ocurre
es peor que no tener el flag. Por lo mismo el spec graba `--allow-remote` junto a
`--addr`: el demonio rechaza un bind no-loopback sin él, así que una unit con uno
y sin el otro arrancaría un demonio que sale -- y el supervisor lo repondría, en
bucle.

## Limitaciones declaradas

- Las cifras son de `benchmarks/daemon-cost` sobre `workspace` en la VM de Docker
  Desktop, no bare metal, y el techo por sesión depende del corpus.
- La cuenta del event log es de **una** máquina y dos días: sostiene el orden de
  magnitud del régimen, no es una distribución.
- La espera del endpoint es de `5 s`. Basta porque desde el ADR 0067 arrancar es
  un `bind` y un token, no un índice, pero es un número y no una medición.
- `Install` habla con el supervisor real de la máquina. Los tests cubren el
  renderizado, `Status` y la migración; el ciclo completo se comprueba con el
  humo del binario. Falsificar el guardia `provision` **instala units de
  verdad**: se comprobó, y dejó `152` entradas que hubo que retirar a mano.
- La migración se verificó sobre un `mcp.json` real de siete servidores: la
  entrada vieja se sustituyó, las otras seis y el `$schema` quedaron intactas.
- El camino completo se comprobó con un cliente MCP real hablando por la entrada
  migrada: `initialize`, `tools/list` con `11` tools, `graph_status`, y un
  `find_references` que devolvió `2` referencias exactas con su veredicto de
  completitud. Un segundo `mcp install` contestó `managed`, sin duplicar nada.
