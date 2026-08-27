# ¿Ayuda una capa de contexto a entregar un cambio real?

Tres brazos del mismo agente sobre los mismos commits reales de `workspace`: sin capa
de contexto, con Kivgraph montado por MCP, y con `graft` montado por MCP. Se
puntúa contra los archivos que tocó el autor del commit. Sin modelo juez y sin
similitud: `git` dice qué escribió el brazo y el commit dice qué debía escribir.

**Resultado: sobre estas seis tareas, con el mismo presupuesto, ninguna de las dos
capas de contexto mejora a un agente sin capa, y las dos cuestan más.** Las
diferencias que hay caben dentro del ruido de $n=12$ por brazo.

Las métricas crudas están en `results.json` y los transcriptos completos de las 36
ejecuciones en `raw/`.

## Entorno y provenance

|dato|valor|
|---|---|
|fecha|2026-08-19|
|agente|`claude` 2.1.220 (Claude Code) headless, `-p`|
|modelo|`claude-sonnet-5`, el mismo en los tres brazos|
|Kivgraph|`0.3.0` compilado del árbol con `-tags ladybug` (incluye el arreglo del handshake, ver más abajo)|
|graft|`0.10.1` (`@nanonets/graft`), nivel estructural, sin `--deep`|
|presupuesto|`$3,00` por ejecución, idéntico en los tres brazos|
|matriz|6 tareas × 3 brazos × 2 trials = 36 ejecuciones|
|coste|`$47,62` imputados, 2 h 1 min de reloj|

## Los tres brazos

Sólo cambia la capa de contexto. Mismo modelo, mismas herramientas de archivo
(`Read`, `Glob`, `Grep`, `Edit`, `Write`, `MultiEdit`, `ToolSearch`), mismo
presupuesto, mismo prompt salvo una frase.

- `cold` — ninguna capa. Config MCP vacía y `--strict-mcp-config`.
- `kivgraph` — 11 tools por MCP, y una frase: «hay un grafo del workspace montado
  como `kivgraph`; consúltalo primero y lee archivos cuando te haya dicho cuáles
  importan».
- `graft` — 6 tools por MCP, y la misma frase con su nombre.

La frase es deliberada. Un piloto previo sin ella midió otra cosa: con la capa
sólo *disponible*, los agentes la consultaron en 1 de 6 ejecuciones y trabajaron
como en frío en las otras 5.

## Resultados

|tarea|archivos|`cold`|`kivgraph`|`graft`|
|---|---|---|---|---|
|`go-svc-a-f3a50ad` (go)|6|0/2 · `P=0,17` `R=0,08`|0/2 · `0,00`/`0,00`|0/2 · `0,00`/`0,00`|
|`core-6ad7d65` (ts)|3|0/2 · `0,58`/`0,50`|0/2 · `0,50`/`0,50`|0/2 · `0,50`/`0,33`|
|`gateway-37cd672` (ts)|2|0/2 · `1,00`/`0,50`|0/2 · `1,00`/`0,50`|0/2 · `1,00`/`0,50`|
|`library-shared-e294840` (ts)|3|**2/2** · `1,00`/`1,00`|1/2 · `0,88`/`1,00`|**2/2** · `1,00`/`1,00`|
|`rs-svc-b-188ae97` (rust)|4|**2/2** · `1,00`/`1,00`|**2/2** · `1,00`/`1,00`|**2/2** · `1,00`/`1,00`|
|`rs-svc-a-2fd7c63` (rust)|3|1/2 · `0,88`/`1,00`|1/2 · `0,88`/`1,00`|**2/2** · `1,00`/`1,00`|

`x/2` son las ejecuciones exactas ($P=R=1{,}00$) de dos trials.

### Agregado

|brazo|exactas|`P`|`R`|llamadas|de ellas al grafo|tokens|coste|tiempo|
|---|---|---|---|---|---|---|---|---|
|`cold`|**5/12**|`0,77`|**`0,68`**|`26,0`|`0,0`|`2,02 M`|**`$14,74`**|**`176 s`**|
|`graft`|**6/12**|`0,75`|`0,64`|`28,8`|`9,4`|`2,12 M`|`$15,95`|`203 s`|
|`kivgraph`|4/12|`0,71`|`0,67`|`28,6`|`3,9`|`2,16 M`|`$16,92`|`211 s`|

Cero fugas detectadas, cero errores, y ninguna ejecución escribió fuera del
repositorio de su tarea. Dos ejecuciones agotaron el presupuesto, las dos en la
tarea Go (`cold` y `kivgraph`, trial 1).

## Qué dicen estos números, y qué no

**No hay ganador.** `6`, `5` y `4` exactas de 12 no distinguen tres brazos: una
sola ejecución que cambie de lado mueve el orden. `R` va de `0,64` a `0,68` en el
sentido contrario al de las exactas, que es la firma del ruido y no de un efecto.

**Las capas cuestan.** `kivgraph` gasta `+15 %` de coste y `+20 %` de tiempo
sobre `cold`; `graft`, `+8 %` y `+15 %`. Los payloads del grafo se suman al
contexto y no se descuentan de las lecturas: los brazos con capa hicieron *más*
llamadas totales (`28,6` y `28,8` contra `26,0`), no menos.

**Los agentes sí usaron la capa.** `graft` `9,4` llamadas al grafo por ejecución
y `kivgraph` `3,9`, con una única ejecución de las 24 que la ignoró. El resultado
no es «no la tocaron»: la tocaron y no cambió el desenlace.

**Tres de las seis tareas no discriminan nada**, y conviene decirlo antes de
sacar conclusiones de la media:

- `gateway-37cd672`: los seis runs escribieron exactamente
  `src/shared/RequestMetrics.ts` y ninguno tocó
  `src/shared/RequestMetrics.test.ts`. Idéntico en los tres brazos, en los dos
  trials. El techo de `R=0,50` no es una diferencia entre herramientas: es que
  ningún agente escribe el test si no se lo piden.
- `rs-svc-b-188ae97`: 2/2 exactas en los tres brazos. Tarea fácil para todos.
- `library-shared-e294840`: 2/2, 2/2 y 1/2; la única no exacta escribió un
  archivo de más con `R=1,00`.

### La tarea Go es inválida, y es un fallo mío

En `go-svc-a-f3a50ad` los tres brazos trabajaron sobre `module_moderation_*` y la
verdad es `module_leveling_*`. No es que fallaran: **el enunciado no determina la
respuesta**. El commit tocaba los dos módulos, su asunto habla de «normalizar los
enums vacíos» sin decir cuál, y mi generador de prompts borra toda ruta para no
filtrar la solución — con lo que borró también la única pista que distinguía
`leveling` de `moderation`.

Cuenta como tarea descartada, no como derrota de nadie. El `P=0,17` de `cold`
viene de acertar `module_moderation.go`, que está en la verdad por casualidad.

Quitándola, el marcador sobre cinco tareas es `5/10`, `4/10` y `6/10`: la misma
conclusión.

## Lo que sí cambió el resultado: tres arreglos de método

Ninguno de estos números existiría sin corregir antes tres cosas que habrían
invalidado el barrido en silencio. Dos son de Kivgraph.

### 1. Kivgraph servía el grafo y sus tools eran invisibles

`serve` construía el registro del resincronizador **antes** de abrir el
transporte MCP, y ese registro lanza un `git rev-parse` por repositorio
registrado: medido `0,09 s` con 2 repositorios y `1,29 s` con 37.

Claude Code publica las tools de un servidor MCP en el toolset visible sólo si
está conectado cuando arranca la sesión. Con `1,29 s` lo marcaba `pending` y
diferían sus 11 tools detrás de `ToolSearch` **durante toda la sesión**, sin un
solo mensaje que lo dijera. El primer piloto gastó dos ejecuciones de 262 s con el
grafo montado y **cero** consultas.

Arreglado en `c35d3be`: la discovery del workspace se hace en segundo plano; el
snapshot publicado se sigue cargando antes de abrir el transporte, que es el
invariante que importa. `1,38 s` → `0,10 s`, y el host reporta `connected` con las
11 tools inline, igual que graft.

### 2. El brazo frío no era frío

Sin config explícita y `--strict-mcp-config`, el agente hereda lo que haya en el
entorno. En esta máquina había tres cosas: un `.mcp.json` del propio corpus
(`web/dash.workspace`, con dos servidores HTTP), los servidores del plugin de
Cloudflare del host, y un hook global `PreToolUse` sobre `Grep` que ejecuta
`tokensave` — es decir, **una tercera herramienta de contexto colándose en los
tres brazos**. Ahora cada brazo pasa su propia config, vacía en `cold`, y el
evento `init` confirma que no le llega ni una tool `mcp__`.

### 3. Un agente fue a por el historial

Una ejecución leyó `.git/HEAD` y `.git/logs/HEAD`. El diseño aguantó -- el estado
preparado tiene un solo commit y el commit bajo prueba es inalcanzable -- pero mi
mensaje de commit decía `state at <short>^`, así que el identificador de la
respuesta estaba dentro del workspace que yo mismo repartía. Corregido: el mensaje
no nombra nada, y `Read`, `Grep` y `Glob` están denegados bajo cualquier `.git`,
verificado contra una ejecución a la que se le pidió leerlo y no pudo.

### Y una asimetría que el barrido sí tuvo, ya cerrada

Durante el barrido, `0.3.0` **no podía indexar este corpus con tests Go**: fallaba
con «two declarations share one identity». Así que graft indexó los tests Go y
Kivgraph no, y cualquier tarea cuyo cambio incluyera un test Go jugaba a favor de
graft. Afecta a la tarea Go, que además es inválida por otra razón.

La causa se localizó después y está arreglada: `fieldOwner` construía la ruta de
un campo desde los nombres de campo intermedios **sin exigir que estuviera
enraizada en un tipo con nombre**. Con
`var env struct{ Errors []struct{ Message string } }` dentro de una función eso
devolvía `Errors` -- no vacío, así que el llamador lo tomaba y nunca preguntaba por
la función y la variable que de verdad separan una de otra. Dos archivos de test
del mismo paquete bastaban para que un `Symbol` tuviera dos `File` declarándolo.

Ahora la identidad es `TestX.env.Errors.Message`, el corpus entero indexa con
tests Go (`91.572` símbolos, `passed: true`) y el harness los vuelve a incluir en
los dos lados. **Los números de este informe son anteriores al arreglo**: rehacer
el barrido con simetría en los tests Go queda pendiente.

## Garantías del experimento

- **El corpus no se toca.** Todo ocurre en una copia privada; del original sólo se
  lee con `git archive`. Comprobado antes y después: los 37 repositorios de `workspace`
  siguen con los dos `go.sum` sucios que ya tenían y nada más.
- **La respuesta no está al alcance.** El repositorio de cada tarea se reconstruye
  en el padre del commit como repositorio de **un solo commit**; `assertNoLeak`
  comprueba por tarea que el commit bajo prueba es inalcanzable y que la
  profundidad del historial es 1.
- **Sin shell.** `Bash` denegado, verificado: un agente al que se le pidió
  `git log` respondió que no tenía cómo ejecutarlo.
- **Presupuesto igual.** `--max-budget-usd 3.00` en los tres brazos. Una ejecución
  que lo agota se marca `budget_exhausted` y se distingue de un fallo, porque el
  host reporta las dos como error con mensaje vacío.
- **Estado idéntico por brazo.** Tras cada ejecución el repositorio se restaura al
  estado preparado; los tres brazos parten de los mismos bytes y consultan la
  misma generación, indexada una vez por tarea.

## Limitaciones

- $n=12$ por brazo. Es la limitación que domina todo lo demás: con seis tareas y
  dos trials no se puede afirmar una diferencia de una o dos ejecuciones. Este
  informe concluye **ausencia de efecto detectable**, que no es lo mismo que
  ausencia de efecto.
- Un modelo, un agente, un corpus, una máquina.
- Cinco tareas útiles de seis, y de esas cinco, tres no distinguen los brazos.
  Para discriminar hacen falta tareas donde encontrar los archivos sea el trabajo
  duro, y este set tiene demasiadas donde no lo es.
- Los prompts salen del asunto y el primer párrafo del commit con las rutas
  borradas. Eso hace la tarea honesta y, en un caso, imposible.
- El tope de presupuesto trunca: dos ejecuciones pararon a medias. Con `$1,50` en
  una prueba anterior la tarea Go ahogaba a los tres brazos (ver
  `starved-1.50/`).
- De graft sólo se midió el nivel estructural. `--deep` necesita clave de
  proveedor y no se ejecutó; su capa de prosa por nodo podría cambiar esto, y es
  justamente la que su documentación presenta como el producto.
- Kivgraph se midió con el arreglo del handshake aplicado, es decir, con HEAD y no
  con el `0.2.1` publicado. Sin ese arreglo su brazo mide un servidor invisible.
- El barrido corrió con los tests Go fuera del índice de Kivgraph y dentro del de
  graft. La causa está arreglada después de medir, así que esa asimetría se puede
  eliminar rehaciendo el barrido; estos números todavía la llevan.

## Qué mediría después

- **Más trials y tareas donde localizar sea el trabajo**: cambios que toquen
  archivos no obvios en repositorios que el agente no puede recorrer a `Grep` en
  20 llamadas. En este corpus, `Grep` basta demasiadas veces, y eso es un hecho
  sobre el corpus.
- **`graft --deep`**, con clave, que es su propuesta real.
- **El eje que este benchmark no toca**: que el agente escriba el test. Seis de
  seis ejecuciones de `gateway` se lo saltaron; ninguna capa de contexto arregla
  eso porque no es un problema de contexto.

## Reproducir

```bash
# el binario medido: HEAD con soporte nativo
LIB="$(scripts/fetch-ladybug.sh)"
CGO_ENABLED=1 CGO_CFLAGS="-I$LIB" CGO_LDFLAGS="-L$LIB -llbug -Wl,-rpath,$LIB" \
  go build -tags ladybug -o /private/tmp/kivgraph-e2e ./cmd/kivgraph

# credencial del agente: suscripción, flujo interactivo
claude auth login --claudeai

go run ./benchmarks/agent-e2e \
  --kivgraph /private/tmp/kivgraph-e2e \
  --model claude-sonnet-5 --trials 2 --budget-usd 3.0 \
  --only go-svc-a-f3a50ad,core-6ad7d65,gateway-37cd672,library-shared-e294840,rs-svc-b-188ae97,rs-svc-a-2fd7c63
```

`raw/` conserva el stream completo de cada ejecución -- cada llamada de
herramienta, el uso de tokens y el motivo de terminación -- así que cualquier fila
de este informe se puede reconstruir desde los bytes que la produjeron.
