# ADR 0011: Logging estructurado del proceso

- Estado: aceptada
- Fecha: 2026-08-06

## Contexto

Ladygraph usa STDIO para el transporte MCP. La salida estándar pertenece al
protocolo y no puede recibir diagnósticos del proceso. Los mensajes de error del
CLI se escribían como texto libre en `stderr`, lo que impedía consumirlos de
forma uniforme y mezclaba errores de argumentos con fallos de ejecución.

La observabilidad posterior necesita eventos que puedan parsearse sin
reconstruir el mensaje humano. La tarea LUQUE-1401 sólo cubre logging; las
métricas de consultas y latencias pertenecen a LUQUE-1402.

## Decisión

`internal/logging` usa `log/slog` de la biblioteca estándar con
`JSONHandler`. Cada evento se escribe en `stderr` como un registro JSON con las
claves estándar `time`, `level` y `msg`, más atributos estructurados cuando
aplican.

El entrypoint `cmd/ladygraph` crea un logger por proceso. El ciclo `serve`
registra inicio, cierre y error de cierre. Los escritores de error heredados del
CLI se adaptan mediante `NewErrorWriter`, que convierte cada escritura en un
evento `ERROR` sin cambiar los contratos de inyección de `run` y sus tests.

La salida estándar queda reservada para resultados del CLI y para el protocolo
MCP. No se registran argumentos completos del proceso, payloads MCP, contenido
de facts ni credenciales; los errores sólo conservan el diagnóstico que ya
producía el comando.

## Alternativas

- **`log.Printf` o `fmt.Fprintf` en texto:** conserva la implementación actual,
  pero no ofrece un contrato parseable ni campos consistentes.
- **Un paquete externo de logging:** añade una dependencia para una capacidad
  que `log/slog` ya proporciona en el Go fijado por el proyecto.
- **Escribir logs JSON en stdout:** corrompería el framing STDIO del protocolo
  MCP y queda descartado.

## Consecuencias

- Los consumidores pueden parsear `stderr` línea por línea como JSON.
- Los handlers de comandos siguen siendo testeables con `io.Writer` sin
  conocer el logger global.
- Los eventos de error del CLI pueden aparecer junto al evento final de salida
  con su código de retorno; ambos son deliberados y facilitan correlación.
- La granularidad por escritura del adaptador conserva mensajes heredados como
  un único atributo `message`; los campos de negocio de consultas quedan para
  LUQUE-1402.

## Riesgos

- Una dependencia que escriba texto directamente en `stderr` no queda
  normalizada por este adaptador; el proceso sólo controla sus propios puntos
  de salida.
- Los mensajes de error pueden incluir rutas de archivos necesarias para el
  diagnóstico. No se registran contenidos de esos archivos ni argumentos
  completos del proceso.
