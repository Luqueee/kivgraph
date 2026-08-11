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

### Configuración de build del índice Go

El índice Go carga cada paquete con los tags declarados en `go.build_tags`. Un
tag ausente no es un error: el directorio no aporta ningún archivo a esta
configuración y por tanto ningún símbolo al grafo.

- Un directorio sin archivos seleccionables se declara como referencia no
  resuelta con razón `PACKAGE_NOT_BUILDABLE` y el detalle observado del
  comando `go`. No aborta la pasada ni se descarta en silencio.
- El aviso que `go/packages` adjunta cuando el comando `go` es más nuevo que
  el toolchain que compiló el binario califica a otro diagnóstico y nunca
  bloquea por sí solo.
- Cualquier otro diagnóstico del cargador sigue abortando la pasada: un
  paquete que no compila no publica hechos.
- Indexar este repositorio requiere `go.build_tags: [ladybug]`; sin él la capa
  de almacenamiento nativo queda fuera del grafo y así se declara.

### Techo de versión del lenguaje Go

`go/types` viaja enlazado dentro del binario, así que Ladygraph solo puede
comprobar tipos hasta la versión del lenguaje del toolchain que lo compiló.

- El `go.work` sintético declara la versión más alta de sus miembros y el
  comando `go` selecciona un toolchain acorde. Un módulo registrado por encima
  del techo haría fallar la carga de **todos** los repositorios dentro de la
  biblioteca estándar de ese toolchain, señalando un archivo que nadie
  registró.
- Por eso `goworkspace.BuildPlan` rechaza el plan antes de escribirlo, con
  `ErrGoVersionUnsupported`, nombrando repositorio, módulo y versión exigida.
  La pasada se rechaza entera: publicar una generación a la que le falta en
  silencio un repositorio registrado es peor que no publicarla.
- El techo es `major.minor`: una release de parche no añade features del
  lenguaje.
- La salida es rebuild de Ladygraph con ese toolchain, o retirar `go` de los
  lenguajes de ese repositorio.
- Un diagnóstico `file requires newer Go version` proveniente de una
  dependencia se acompaña de la versión con la que este binario comprueba
  tipos, porque no hay nada que el repositorio pueda cambiar.

### Resolución de módulos en un workspace multi-repositorio

Un `go.work` resuelve **una sola** lista de build para todos los módulos que
usa. Con todos los repositorios registrados dentro de un único workspace, una
dependencia subida en uno cambia las versiones seleccionadas para los demás, y
una versión que ningún repositorio descargó por su cuenta rompe todas las
cargas a la vez.

- El plan se parte en grupos independientes. Dos módulos comparten workspace
  solo cuando uno puede alcanzar al otro: un `require` o un `replace` que
  nombra un módulo registrado, o un `go.work` del repositorio indexado que ya
  los une.
- Un módulo que no alcanza a ningún otro se carga en modo módulo, sin
  workspace: su propio `go.mod` y su `go.sum` son la verdad, exactamente como
  lo cargaría su propio toolchain.
- Los ficheros sintéticos que una pasada no necesita se retiran, junto con su
  `.sum`, para que un workspace de otro conjunto de repositorios no se
  confunda con el vigente.
- La indexación sigue siendo hermética: un módulo ausente del caché local se
  reporta, no se descarga. `go.allow_network: true` levanta esa restricción
  para el comando `go`.

### La generación publicada y quien la sirve

Un servidor carga el HotSnapshot al arrancar. Sin nada más, un `index --full`
ejecutado en otra terminal lo deja respondiendo desde un grafo que ya no existe
en disco, y nada en su salida lo dice.

- `serve` y `ui` siguen el puntero `CURRENT` y republican cuando avanza.
- El seguidor no se coordina con nadie: `SnapshotStore.Publish` solo acepta una
  generación estrictamente más nueva, así que perder la carrera contra
  `index_project` no tiene consecuencia; se observa el identificador mayor en
  el siguiente ciclo.
- Una generación que no se puede construir se registra y la publicada sigue
  respondiendo. El seguidor nunca tumba el servidor.

### Ambigüedad de paquetes TypeScript

Un nombre de paquete que declaran varios `package.json` de un repositorio no
tiene un proveedor único.

- Los candidatos salen del registro y el nombre se declara como referencia no
  resuelta con razón `AMBIGUOUS_PACKAGE_PROVIDER` y los manifests observados.
- El resto del repositorio, y todos los demás, se indexan con normalidad. Antes
  un fixture duplicado en cualquier sitio hacía inindexable el conjunto entero.
- Es el mismo trato que recibe un módulo Go declarado por varios repositorios.

### Retirar generaciones

`ladygraph clean` es el único comando destructivo sobre el grafo, así que
enumera lo que haría y no toca nada hasta `--yes`.

- Sin flags retira todas las generaciones, ambos punteros y la reserva de
  espacio: el store queda vacío y vuelve a construirse desde cero. Nunca toca
  la configuración ni el registro de repositorios.
- `--keep-active` conserva exactamente la generación publicada. El puntero
  `BACKUP` se va con lo demás, porque nombraría una generación retirada, y
  `rollback` se queda sin nada que restaurar.
- Los punteros se retiran antes que los directorios. Una interrupción deja
  como mucho un directorio al que nadie apunta -- recuperable -- y nunca un
  puntero que nombra algo que ya no está.
- Después de un `clean` completo la numeración vuelve a `000001`. Un servidor
  vivo sigue sirviendo el grafo que tenía en memoria y no podrá instalar los
  siguientes, porque `Publish` solo acepta identificadores mayores. El
  seguidor lo declara una vez y el comando avisa de reiniciar.

### Registrar un proyecto dos veces

`clean` retira el grafo y deja el registro de repositorios intacto: lo que se
indexa es una decisión del operador, no un producto de la pasada. Reconstruir
lo registrado es `ladygraph index --full`.

Por eso `index_project` es idempotente:

- Un proyecto ya registrado con el mismo directorio se reindexa y el archivo
  no se reescribe.
- Registrarlo con otros lenguajes sustituye la entrada y conserva sus
  `exclusions`, que la petición no puede expresar y el operador sí decidió.
- Un nombre ocupado por **otro** directorio sigue siendo un conflicto: nada
  puede decidir cuál de los dos repositorios nombra. El error nombra el
  directorio ya registrado.

### Primer arranque de un cliente MCP

El cliente lanza `ladygraph serve` él mismo y habla el protocolo por la
tubería. Un servidor que sale porque falta la configuración deja al cliente
informando de que «el servidor falló», y obliga a abrir una terminal para algo
que la instalación debería haber resuelto.

- `serve` y `ui` crean la configuración por defecto cuando no existe, y
  continúan. Son dos archivos y un directorio de estado.
- No registran ningún repositorio ni indexan nada: el grafo sigue igual de
  vacío, y la primera consulta responde `INDEX_NOT_READY` hasta que alguien
  pide un índice. Indexar sigue siendo explícito.
- Una configuración que existe y no se puede leer aborta el arranque. Solo se
  crea la ausente.
- `INDEX_NOT_READY` nombra las dos salidas —`index_project` o
  `ladygraph index --full`— porque es la primera respuesta que recibe una
  instalación recién hecha.

### Operaciones largas sobre MCP

Un rebuild completo dura minutos en un registro grande, y un cliente MCP
aplica su propio timeout a cada llamada — treinta segundos en algunos. Sin
señales, cancela un trabajo que va bien; y como el corte es del cliente, el
servidor no se entera y termina igual.

- `index_project` reenvía cada unidad de trabajo del indexador como
  `notifications/progress` cuando la petición trae `progressToken`. Un cliente
  que las respeta espera lo que dure el trabajo.
- Sin token no hay notificaciones ni callback: el índice no paga por un canal
  que nadie lee.
- El valor de progreso siempre crece, como exige el protocolo, y una
  notificación que no se puede entregar se descarta en vez de tumbar el
  índice.
- Verificar el resultado fuera de banda sigue siendo válido: `graph_status`
  responde en milisegundos y dice qué generación se sirve.

### El lote es la unidad de indexación

Un rebuild resuelve las aristas cross-repository sobre el conjunto completo de
hechos. No hay unidad más barata: añadir un repositorio cuesta reconstruir el
corpus entero.

- `index_project` acepta `projects` y registra el lote completo antes de
  construir nada. Once repositorios en una llamada cuestan un rebuild; en once
  llamadas cuestan once, y diez de esos grafos se tiran.
- Medido sobre un corpus real, tres repositorios añadidos de uno en uno tardan
  `4,7 s` (tres rebuilds) frente a `1,5 s` en lote: el factor es el número de
  llamadas, no una constante.
- El registro del lote se valida como un único registro, así que un nombre
  repetido dentro del propio lote se detecta antes de construir.
- Mezclar `projects` con `name`/`path`/`languages` en una misma petición se
  rechaza: solo podrían contradecirse.

### El análisis es concurrente

Medido sobre el corpus real de este repositorio, la pasada se reparte en
`65 %` de análisis y `35 %` de rebuild. El análisis eran unidades
independientes ejecutadas en fila india.

- Cada módulo Go y cada paquete TypeScript es una unidad: la carga Go
  construye su propio universo de tipos y el paquete TypeScript es un proceso
  aparte con su propio fichero de salida. No comparten estado.
- El merge sigue el orden de las unidades, nunca el de finalización. Dos
  pasadas del mismo corpus producen hechos idénticos byte a byte; al tocar
  esta zona se verifica comparando el digest del snapshot, no leyendo el
  diff.
- Los presupuestos son distintos por tipo. `go.maximum_loads` acota las cargas
  Go, porque cada una sostiene un universo de tipos completo y el techo cambia
  memoria por velocidad; `typescript.maximum_workers` acota los procesos del
  worker. Cero en cualquiera de los dos usa el valor por defecto.
- El primer fallo cancela el resto: una pasada que no va a publicar no debe
  seguir pagando trabajo que nadie usará.
- Un módulo Go que el cargador no puede leer se declara como
  `MODULE_NOT_LOADED` con sus diagnósticos y la pasada continúa. Sus hechos no
  se publican, porque no serían de fiar; lo que no se hace es dejar sin grafo
  a los otros treinta y dos repositorios. El caso corriente es un repositorio
  recién clonado cuyas dependencias nadie ha descargado.
- Cada tipo drena su cola con la unidad más pesada primero. El peso es una
  estimación sobre los archivos a leer, no una medida, y solo decide el orden
  de despacho: una pasada termina cuando termina su unidad más lenta, así que
  lo único que importa es no dejarla para el final.
- Medido sobre un workspace de 33 repositorios: `21.0 s` de análisis en
  secuencial, `12.6 s` con concurrencia y cola ordenada. Ordenar la cola quitó
  `0.9 s` e hizo innecesarios los trabajadores extra -- con la cola ordenada,
  tres workers igualan a diez.

### El visor y el bundle web

- `ladygraph ui` registra la dirección enlazada antes de servir nada, también
  cuando el puerto configurado es `0`.
- Un binario sin el tag `webassets` no puede mostrar el visor. `ui` lo dice y
  no abre puerto, en lugar de servir la página de «bundle no disponible» en
  todas las rutas. El bundle MCP publicado se construye con `--mcp-only`, así
  que es el caso normal de una instalación desde release.

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
