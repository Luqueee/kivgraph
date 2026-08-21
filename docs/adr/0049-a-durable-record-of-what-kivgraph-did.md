# ADR 0049: Un registro durable de lo que Kivgraph hizo

- **Estado:** aceptada con limitaciones declaradas
- **Fecha:** 2026-08-21
- **Revisa:** ADR 0042, ADR 0046

## Contexto

Nada de lo que Kivgraph hace sobrevivía al proceso que lo hizo.

`internal/metrics` ya medía la latencia por tool con precisión: `QueryMetrics`
lleva `Calls`, `Errors`, `LatencyCount`, `LatencyTotal` y `LatencyMax` por
nombre de tool, alimentados desde el único punto de estrangulamiento real que
tiene la superficie MCP -- `tools.observe`, por el que pasan las nueve tools de
consulta. Pero el registro lo acuña `RunWithMetrics...` en cada arranque y se
descarta al salir, y su única vía de lectura era la tool `graph_status`. Una
pregunta hecha desde otro proceso -- que es lo que es un comando -- encontraba
siempre un registro vacío.

Los tiempos de indexado estaban peor: `rebuild.Report.Stages` lleva
`DurationMS` por etapa, se imprime en `stdout` y se tira. `indexing.FullDocument`
descarta las etapas enteras, e `indexing.ProjectResult` -- lo que devuelve
`index_project` -- no tiene ningún campo de duración. Y `index_project` no
pasaba por el observador en absoluto: era la única llamada que un cliente puede
hacer que ningún contador veía, y es la que cuesta minutos.

El resultado observable era que a la pregunta «¿qué está haciendo Kivgraph, y
cuánto tarda?» no había respuesta ninguna. Un `serve` que responde desde un
snapshot derivado, uno que se quedó con la imagen que un `update` reemplazó y
uno recién arrancado producen exactamente la misma salida.

## Decisión

Se añade `internal/eventlog`: un fichero **JSON-lines de solo-añadir** en el
directorio de estado, `~/.local/state/kivgraph/events.jsonl`, configurable en
`logging.event_log_path`.

El formato de una línea es una sola escritura de mucho menos que un buffer de
tubería sobre un descriptor abierto con `O_APPEND`, que POSIX mantiene entera y
sin entremezclar. Eso es lo que lo hace seguro para los varios procesos que lo
escriben a la vez sin ningún cerrojo entre procesos y sin leer antes de
escribir. Ni SQLite ni un cerrojo de fichero comprarían nada aquí: no hay
ninguna lectura en el camino de escritura que hubiera que aislar.

`Event` es un conjunto **cerrado** de campos con nombre, no un saco de
atributos libres. Todos los productores están en este repositorio, y una forma
fija es lo que permite a un lector renderizar columnas en orden estable y
agregar sin adivinar tipos.

La emisión entra por tres sitios y ninguno nuevo:

- **Tools:** `metrics.Registry` gana un segundo canal lateral opcional,
  `QueryRecorder`, simétrico con el puente OpenTelemetry que ya tenía. El
  registro sigue siendo lo que era -- atómicos locales al proceso que
  `graph_status` lee de vuelta -- porque un lector que tiene que parsear un
  fichero no puede responder a una llamada de tool. El recorder es la segunda
  copia, y es la única que sobrevive.
- **`index_project`** recibe `callObservers ...CallObserver`, variádico como
  todo `Register*` de ese paquete.
- **Indexado y ciclo de vida** los emite el CLI, que es donde se conocen la
  generación, los contadores y las duraciones por etapa.

`kivgraph logs` y `kivgraph tool-stats` son lectores sobre ese fichero.

Los percentiles viven en el lector, no en el registro. `internal/metrics`
retiene la latencia como cuenta, suma y máximo a propósito, para no mantener una
lista de muestras sin cota en un proceso vivo; eso compra una media y un peor
caso y nada intermedio. El fichero guarda cada llamada individual, así que un
lector puede responder la pregunta que una media esconde -- una tool cuya
mediana es rápida y cuya cola no -- y la paga una sola vez.

## Consecuencias

- El fichero **no es una API**. Es estado derivado: borrarlo pierde historia y
  nada más. No lleva versión de esquema, y un lector salta una línea que no
  parsea en lugar de fallar la lectura, porque el fichero lo escriben varios
  procesos y uno puede ser de una versión que conoce campos que otra no.
  Negarse a mostrar el resto convertiría un desconocido inocuo en la caída de
  la única vista que un lector tiene.
- Se rota a 8 MiB y se conserva **una** rotación, así que el coste en disco está
  acotado a 16 MiB. Un store que crece por encima de eso **pierde sus registros
  más viejos**: es el precio declarado de conservar exactamente una, y el test
  de rotación dimensiona su umbral desde un registro real para medir lo que sí
  se conserva.
- Una rotación con dos procesos escribiendo es benigna pero no atómica: el
  segundo puede seguir añadiendo al fichero renombrado. Por eso el lector lee
  siempre la rotación **antes** del fichero vivo y ordena por tiempo, en vez de
  suponer que la rotación está cerrada.
- Un store que no se puede abrir degrada con un aviso, nunca falla el trabajo
  que describe. Un `*Writer` nulo es un sumidero que descarta, así que ningún
  productor necesita una rama.
- `logging.format` y `logging.level` siguen validados y sin leer por nadie. Este
  ADR no los arregla ni los retira: la regla de «texto en una terminal, JSON si
  no» que declara `cmd/kivgraph/AGENTS.md` sigue sin implementarse, y esa deuda
  queda nombrada aquí en lugar de quedar tapada por un comando nuevo que sí
  colorea.
- `event_log_path` se reubica en `stateBesideConfig`, como todo el resto del
  estado. Sin eso una configuración aislada -- una prueba bajo `/tmp` -- habría
  escrito su historia dentro de la instalación real.
- `otel.go` sigue clasificando `get_source`, `get_file_outline` e
  `index_project` como `tool.name="other"` por una lista de nombres fijada a
  mano. El fichero no tiene ese defecto porque no tiene lista: usa el nombre
  observado. La divergencia queda declarada, no corregida.
