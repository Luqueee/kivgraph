# Convenciones de desarrollo

Este documento fija las convenciones mínimas de Ladygraph. Las decisiones que
cambien la arquitectura, el contrato MCP, el formato persistente o la política
de compatibilidad deben registrarse además en un ADR.

## Go

- El formato oficial es `gofmt`; no se aceptan cambios Go sin formatear.
- Los paquetes usan nombres cortos, en minúsculas y sin guiones bajos.
- Los identificadores exportados llevan documentación Go cuando forman parte de
  un contrato entre paquetes.
- Los errores se propagan con contexto usando `%w`; no se descartan errores ni
  se comparan por texto cuando existe un código o tipo.
- Las funciones que puedan bloquear reciben `context.Context` como primer
  argumento después de los receptores.
- Las goroutines tienen un propietario claro y una ruta de cancelación. No se
  crean goroutines persistentes por petición MCP.
- Los paquetes bajo `internal/` no forman parte de una API externa estable.
- Los IDs internos se representan con tipos definidos, no con enteros desnudos
  cuando mezclar dominios pueda producir errores.
- Los datos que entran en el grafo deben validarse antes de construir nodos o
  aristas.

## TypeScript

- El worker usa TypeScript con `strict` habilitado y módulos ESM.
- Se prefieren tipos explícitos en los límites del proceso, del protocolo y de
  los adaptadores; `any` requiere una justificación local.
- Los mensajes de stdin/stdout son datos del protocolo. Los logs van a stderr y
  nunca contaminan stdout.
- Cada recurso persistente del worker tiene una función de cierre y se libera
  al recibir cancelación o al terminar el proceso.
- Las promesas rechazadas se manejan en el límite que puede clasificarlas; no
  se usan aserciones para ocultar errores de protocolo o de resolución.
- Las pruebas de fixtures deben poder ejecutarse sin depender del directorio
  de trabajo actual.

## Errores y resultados semánticos

### Clasificación programática

Todo error operativo expuesto por una API debe tener una clase y un código
estables. El formato conceptual es:

```text
class: internal | client | dependency | unavailable
code:  identificador SCREAMING_SNAKE_CASE
message: texto para diagnóstico humano
cause:  causa encadenada, solo para logs o diagnóstico autorizado
```

Los códigos de superficie MCP iniciales son:

```text
INVALID_ARGUMENT
SYMBOL_NOT_FOUND
AMBIGUOUS_SYMBOL
REPOSITORY_NOT_FOUND
CURSOR_INVALID
CURSOR_SNAPSHOT_EXPIRED
TRAVERSAL_LIMIT_REACHED
SNAPSHOT_UNAVAILABLE
INDEX_NOT_READY
```

El código, no el texto de `message`, se utiliza para decisiones programáticas.
Los mensajes pueden cambiar sin romper clientes.

### Interno frente a no resuelto

Un error interno indica que Ladygraph no pudo cumplir una operación del sistema:
fallo de I/O, corrupción, bug, dependencia caída o violación de una
invariante. Debe clasificarse como error y no convertirse en un resultado
semántico silencioso.

Una referencia no resuelta es un resultado válido del análisis cuando falta
evidencia suficiente para afirmar una relación. No es un error interno ni una
arista exacta. Los estados semánticos permitidos son:

```text
EXACT
CANDIDATE
UNRESOLVED
```

`CANDIDATE` y `UNRESOLVED` deben exponerse por separado de los resultados
exactos. Nunca se crea una relación `EXACT` por coincidencia nominal, textual,
de path o de proximidad sintáctica.

## Stable keys

La stable key es una identidad persistente y auditable, independiente de los
IDs densos de un snapshot.

1. Se construye una identidad canónica con campos definidos por lenguaje,
   repositorio, paquete/módulo, contenedor, nombre, tipo y discriminador de
   firma según corresponda.
2. En Go se incluye el module path, el package import path y el object path de
   `go/types`.
3. En TypeScript se incluye la identidad del repositorio, paquete, módulo,
   contenedor cualificado, nombre, kind y discriminador de firma.
4. La stable key se calcula como `BLAKE3(canonical_identity)`.
5. Se conserva también `canonical_identity` para auditoría y depuración.
6. La línea de origen no es un componente principal; mover una declaración no
   debe cambiar por sí solo su identidad.
7. Una stable key duplicada es un fallo de integridad, no una colisión que se
   resuelva eligiendo arbitrariamente un símbolo.

## Gates

- Los gates se nombran en mayúsculas con palabras separadas por guiones bajos y
  terminan en `_PASS`, por ejemplo `PROJECT_FOUNDATION_PASS`.
- Un gate solo puede declararse después de ejecutar los criterios definidos en
  su tarea y conservar la evidencia de los comandos, fixtures y benchmarks.
- Un gate no se hereda por proximidad: sus dependencias deben estar en `PASS`
  antes de iniciar la tarea.
- `PASS_WITH_LIMITS` solo se usa cuando la tarea lo autoriza explícitamente y
  las limitaciones quedan registradas.
- Un gate no se usa para ocultar warnings, tests fallidos, referencias no
  resueltas o regresiones de precisión.

## Benchmarks

Cada benchmark se guarda en `benchmarks/<nombre>/` con un resultado estructurado
`results.json` y un informe humano `report.md`.

El JSON debe registrar como mínimo:

```json
{
  "benchmark": "nombre-estable",
  "command": "comando reproducible",
  "commit": "sha o dirty",
  "environment": {
    "os": "...",
    "arch": "...",
    "cpu": "...",
    "memory": "..."
  },
  "dataset": {
    "name": "...",
    "seed": 0,
    "parameters": {}
  },
  "metrics": {
    "p50_ms": 0,
    "p95_ms": 0,
    "p99_ms": 0,
    "throughput_per_s": 0,
    "allocations_per_op": 0,
    "bytes_per_op": 0,
    "rss_bytes": 0,
    "goroutines": 0,
    "errors": 0
  }
}
```

Los nombres de métricas incluyen la unidad o el sufijo `_per_op`. Los informes
deben indicar hardware, versión de toolchain, dataset, semilla, número de
iteraciones, warm-up, comando exacto, resultado y limitaciones. Los resultados
versionados no se sobrescriben sin explicar la comparación.

## Compatibilidad

- El contrato MCP y el framing del worker se consideran interfaces públicas.
  Los cambios incompatibles requieren ADR, versión de protocolo nueva y
  migración explícita.
- Los cambios aditivos deben conservar los campos existentes y el significado
  de los códigos de error.
- Las stable keys son persistentes: cambiar su algoritmo o identidad canónica
  requiere migración de datos y un ADR.
- El schema persistente de LadybugDB se versiona. Un cambio incompatible exige
  full rebuild o migración documentada; no se modifica una base existente de
  forma silenciosa.
- Los paquetes `internal/` y los comandos experimentales no prometen
  compatibilidad externa, pero sus cambios deben mantener la suite del
  repositorio en verde.
- La compatibilidad de una dependencia nativa se evalúa por versión fijada,
  plataforma y licencia; no se asume compatibilidad por el nombre del paquete.

## Tests

- Cada comportamiento nuevo debe tener una prueba que falle ante una
  regresión plausible.
- Las pruebas deben cubrir límites, errores clasificados, invariantes,
  transiciones de estado y resultados negativos cuando aplique.
- Las pruebas semánticas deben incluir fixtures positivos y negativos para
  demostrar que no se crean aristas exactas por coincidencias nominales.
- Los tests Go se ejecutan con `go test ./...` y `go vet ./...` antes de cerrar
  una tarea. Los cambios del worker ejecutan además `pnpm test` y
  `pnpm typecheck` desde `ts-worker/`.
- Los tests no escriben en repositorios analizados ni dependen de servicios
  externos no declarados.

## Documentación

- La documentación técnica vive en `docs/` y usa Markdown.
- Cada ADR incluye `Context`, `Decision`, `Alternatives`, `Consequences`,
  `Risks` y `Status`.
- Los nombres de comandos, códigos, campos JSON y gates se escriben en
  backticks.
- La documentación debe describir el comportamiento observable real y debe
  registrar limitaciones conocidas, no promesas futuras.
