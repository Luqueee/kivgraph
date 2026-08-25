# ADR 0068: el demonio tiene dueño

- **Estado:** aceptada
- **Fecha:** 2026-08-25
- **Cambia el protocolo MCP:** no
- **Cambia el schema persistente:** no
- **Obliga a reconstruir:** no
- **Cambia la superficie del CLI:** sí, de forma aditiva -- tres comandos nuevos
  (`daemon install`, `daemon remove`, `daemon status`) y una línea nueva de
  `doctor` (`daemon.supervisor`)
- **Relaja un contrato de la raíz:** no

## Contexto: el ahorro estaba medido y no se podía cobrar

El ADR 0065 entregó `kivgraph daemon`, y `benchmarks/daemon-cost` midió lo que
ahorra sobre `108.737` símbolos de `kena`. Con ocho clientes ociosos, que es el
caso normal:

|con `8` clientes|N procesos `serve`|un demonio|factor|
|---|---|---|---|
|páginas privadas, en reposo|`77`-`81 MB`|`10`-`13 MB`|`7x`|
|contestando|`323`-`330 MB`|`60`-`62 MB`|`5,4x`|
|pico|`179`-`186 MB`|`26`-`29 MB`|`6,6x`|
|conectar|`38`-`55 ms`|`1,6`-`2,0 ms`|`25x`|

Y el régimen real no era una hipótesis. El event log de esta máquina, sobre dos
días de uso, cuenta **`69` arranques de `serve`, `66` de ellos sin una sola
llamada de tool y `5` llamadas en total**, con `8` procesos vivos a la vez en el
momento de mirarlo. Es exactamente el régimen donde el demonio gana más: clientes
que abren un servidor, no preguntan nada, y pagan `40 MB` cada uno por ello.

**Nada de eso se podía cobrar por defecto, y el motivo no era el ahorro.** Un
`command` en la configuración de un cliente lo arranca ese cliente: si falla, ese
cliente dice «sin tools» y su dueño lo arregla relanzándolo. Una `url` apuntando
a un demonio muerto rompe **todos los clientes a la vez** y ninguno puede
arreglarlo solo, porque ninguno lo arrancó.

Y no había nada que lo levantara. `kivgraph daemon &` muere con la sesión que lo
lanzó: `grep launchd|LaunchAgent|systemd|.plist|systemctl` sobre `cmd`,
`internal`, `scripts`, `Makefile` y `landing/src` devolvía cero coincidencias.
El proyecto ofrecía un modo de operación cuya única forma de arrancar era que un
humano se acordara.

Se vio en un caso concreto y aburrido: `kivgraph update` de `0.3.6` a `0.6.4`
tuvo que parar **ocho** procesos `serve`, uno a uno, nombrándolos. Con un demonio
eso es un `restart`.

## La decisión

**El demonio se instala en el supervisor de la plataforma, y el proyecto no
escribe un supervisor propio.** En macOS es un LaunchAgent con
`KeepAlive.SuccessfulExit=false`; en Linux una unit de usuario de systemd con
`Restart=on-failure`. Los dos arrancan el demonio con la sesión y lo reponen si
muere; los dos dejan en paz una salida limpia.

Escribir un gestor de procesos dentro de una herramienta de grafo de código no es
algo que este proyecto deba mantener, y el sistema operativo ya trae uno que
sobrevive a un reinicio.

**Es de usuario, no de sistema.** El demonio contesta con las rutas del usuario
que lo arrancó y lee un directorio de estado bajo su `HOME`; un servicio de
sistema respondería a quien preguntara.

**La unit es por directorio de estado, no por máquina.** El demonio ya está
cuencado así -- `internal/daemon.SocketName` dice por qué: dos configuraciones
apuntando a directorios distintos obtienen demonios distintos, así que un cliente
nunca alcanza un grafo construido con los repositorios de otro. Dos
configuraciones tienen que poder tener dos demonios supervisados a la vez, y una
etiqueta compartida haría que un `install` sustituyera en silencio el demonio del
otro. La etiqueta lleva un digest del directorio, y `daemon status` la imprime en
vez de dejar al operador adivinar qué unit sirve qué.

**Una plataforma sin supervisor lo declara.** `supervisor_other.go` devuelve
`ErrUnsupportedPlatform` con el remedio nombrado, nunca un cero silencioso: quien
decide si registrar clientes contra un demonio necesita saber que aquí el demonio
no va a tener dueño. Los objetivos de distribución son `linux/amd64` y
`darwin/arm64`, y esto no los amplía.

**`doctor` informa y no falla.** Un supervisor ausente es el estado de una
máquina que nunca pidió uno, y un cliente registrado contra `serve` no necesita
demonio ninguno. Convertir la ausencia en `FAIL` pondría el `doctor` en rojo en
cada instalación que use `serve` -- que es exactamente cómo un fallo de verdad
deja de notarse.

## Lo que se rompió por el camino, y su guardia

`main` intercepta `serve`, `daemon` y `ui` antes del despacho de la tabla, y lo
hacía comparando `os.Args[1]`. Con eso, `daemon install` habría entrado en el
bucle del servidor y `install` se habría parseado como flag del demonio. La
intercepción ahora **consulta el registro**: intercepta cuando la coincidencia
más larga tiene exactamente una palabra. Lo fija
`TestALongRunningCommandDoesNotSwallowItsSubcommands`, que cubre las tres.

Las otras decisiones también están falsificadas, y se comprobó que sus tests
fallan al romperlas: quitar `KeepAlive` del plist, hacer que la etiqueta ignore el
directorio, construir el plist por concatenación en vez de con `encoding/xml`, y
que `status` deje de comparar el contenido del fichero. Las cuatro se cazan.

Dos detalles de codificación que no son cosméticos: un `&` en una ruta produce un
plist que launchd no parsea, y un espacio en un `ExecStart` de systemd parte una
palabra en dos argumentos. Los dos dan un demonio que nunca arranca, y los dos
tienen test.

## Lo que esto no decide

**No cambia qué registra `mcp install`.** Sigue escribiendo una entrada
`command` que nombra `serve`, y `--daemon` sigue siendo la forma de pedir la
`url`. Mover ese defecto es una decisión aparte, y ésta es su precondición: sin
un dueño del proceso, el defecto habría dependido de que alguien recordara un
`kivgraph daemon &`.

**No arranca nada por su cuenta.** `daemon install` es explícito. Un comando de
integración que instalara un supervisor como efecto colateral sería una sorpresa
grande para algo que hoy sólo edita el fichero de configuración de un cliente.

## Limitaciones declaradas

- Los números de arriba son de `benchmarks/daemon-cost` sobre `kena` en la VM de
  Docker Desktop, no bare metal, y el techo por sesión depende del corpus.
- La cuenta del event log es de **una** máquina y dos días. Sostiene el orden de
  magnitud del régimen -- muchos clientes, casi ninguna llamada-- y no es una
  distribución.
- El demonio no habla `sd_notify`, así que la unit es `Type=simple`. Declarar
  `notify` haría a systemd esperar una señal de listo que nunca llega.
- `Install` y `Remove` invocan `launchctl` y `systemctl`, así que sus tests
  cubren el renderizado, `Status` y `Remove` de algo ausente; el ciclo completo
  se comprueba con el humo del binario, no en la suite.
