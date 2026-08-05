# Protocolo Go–TypeScript, versión 1

- Estado: aceptado
- Fecha: 2026-08-05
- Tarea: LUQUE-0601
- Decisiones aplicables: ADR 0005 y ADR 0010

Este documento es el contrato que implementan LUQUE-0602 en Go y LUQUE-0603 en
TypeScript. Ambas implementaciones deben producir exactamente los mismos bytes
para los mismos mensajes.

## 1. Transporte

```text
stdin/stdout
length prefix de 32 bits
JSON UTF-8
```

- El worker lee peticiones de `stdin` y escribe respuestas y eventos en
  `stdout`.
- Cada frame es un prefijo de longitud de 32 bits sin signo en **big-endian**,
  seguido de exactamente esa cantidad de bytes de JSON codificado en UTF-8.
- El prefijo cuenta únicamente el cuerpo. Un cuerpo vacío es inválido.
- El JSON no lleva terminador ni separador. No se emite BOM.
- `stdout` transporta frames exclusivamente. Cualquier log, aviso o diagnóstico
  operativo va a `stderr` en líneas de texto y nunca forma parte del protocolo.
- El tamaño máximo de un frame es `16 MiB`. Un prefijo mayor es un error de
  protocolo y termina la sesión: no se intenta resincronizar el flujo.

## 2. Sobre

Todo frame es un objeto JSON con esta forma:

```text
v          entero, versión del protocolo, siempre 1
id         entero sin signo, correlación
type       cadena, tipo de mensaje
payload    objeto, específico del tipo
```

- `id` lo asigna el emisor de la petición y es estrictamente creciente por
  sesión. El worker responde con el mismo `id`.
- Un mensaje iniciado por el worker sin petición previa usa `id` igual a `0` y
  es un evento.
- Un `type` desconocido produce un error `UNSUPPORTED_MESSAGE` y no cierra la
  sesión.
- El orden de las respuestas no está garantizado: la correlación es por `id`.
  El worker puede procesar varias peticiones simultáneamente hasta el límite
  declarado en `HELLO`.

## 3. Mensajes

```text
HELLO
OPEN_WORKSPACE
INDEX_PROJECT
UPDATE_FILES
REMOVE_FILES
GET_STATUS
CANCEL
SHUTDOWN
```

`GET_STATUS` es el nombre normativo; `STATUS` aparece en el backlog como forma
abreviada del mismo mensaje.

### 3.1 HELLO

Primer mensaje de la sesión. Cualquier otro mensaje anterior es un error de
protocolo.

Petición:

```text
protocol_versions   lista de enteros soportados por el supervisor
supervisor_version  cadena
```

Respuesta:

```text
protocol_version      entero seleccionado
worker_version        cadena
engine                cadena, identificador del motor semántico
engine_version        cadena, versión exacta del compilador
runtime               cadena
max_concurrent        entero, peticiones simultáneas admitidas
max_frame_bytes       entero
max_batch_positions   entero, posiciones por petición de lote
supported_typescript  objeto con `min` y `max`, ventana de versiones cuyos
                      hechos pueden emitirse como exactos
```

Si no hay versión común, el worker responde con error `VERSION_MISMATCH` y
cierra.

### 3.2 OPEN_WORKSPACE

Declara los repositorios que el worker puede leer. No carga proyectos.

Petición:

```text
repositories  lista de objetos con `name` y `real_path`
```

Respuesta:

```text
projects  lista de objetos con `project_id`, `config_path`, `repository`,
          `typescript_version` y `within_supported_window`
```

`typescript_version` es la versión declarada por el proyecto. El worker no la
usa para elegir motor; la usa para decidir la confianza de los hechos según el
ADR 0010.

### 3.3 INDEX_PROJECT

Carga un proyecto y emite sus hechos.

Petición:

```text
project_id   cadena
include      lista opcional de archivos; ausente significa todo el proyecto
```

El worker responde de inmediato con:

```text
accepted   booleano
files      entero, archivos que va a recorrer
```

y a continuación emite eventos `FACTS` hasta un evento `PROJECT_INDEXED`.

### 3.4 UPDATE_FILES

Aplica cambios de contenido y re-emite los hechos afectados.

Petición:

```text
project_id  cadena
files       lista de objetos con `path` y `content_hash`
```

El worker relee los archivos del disco. El protocolo no transporta contenido de
código fuente: el worker y el supervisor comparten el sistema de archivos, y
`content_hash` sirve para detectar una lectura desincronizada.

### 3.5 REMOVE_FILES

Elimina archivos del programa y emite los hechos de invalidación.

Petición:

```text
project_id  cadena
paths       lista de cadenas
```

### 3.6 GET_STATUS

Petición sin campos. Respuesta:

```text
projects_open      entero
files_loaded       entero
pending_requests   entero
engine_alive       booleano
rss_bytes          entero, worker más servidor nativo
uptime_ms          entero
```

### 3.7 CANCEL

Petición:

```text
target_id  entero, `id` de la petición a cancelar
```

El worker deja de emitir hechos de esa petición y responde a la petición
original con error `CANCELED`. Cancelar un `id` desconocido o ya terminado no
es un error.

### 3.8 SHUTDOWN

El worker deja de aceptar peticiones, cierra el servidor nativo, responde y
termina con código de salida `0`. Un `SHUTDOWN` durante trabajo en curso
cancela ese trabajo.

## 4. Regla de lotes

El ADR 0010 fija que este protocolo es **por lotes y con granularidad de
archivo**. Es una restricción normativa, no una recomendación:

- Ninguna petición identifica un símbolo suelto.
- Una petición de resolución identifica un archivo y una lista de posiciones;
  la respuesta conserva el orden de esa lista.
- Los hechos se emiten agrupados por archivo en eventos `FACTS`.
- `max_batch_positions` acota el tamaño de un lote. Superarlo produce
  `BATCH_TOO_LARGE`.
- Una petición no debe materializar el conjunto completo de exports de un
  módulo cuando existe una consulta más estrecha.

## 5. Hechos

Un evento `FACTS` contiene:

```text
request_id  entero, petición que lo originó
project_id  cadena
file        cadena, archivo al que pertenece el lote
facts       lista de hechos
final       booleano, último lote de ese archivo
```

Tipos de hecho:

```text
RepositoryFact
PackageFact
FileFact
SymbolFact
EdgeFact
EvidenceFact
UnresolvedFact
DiagnosticFact
```

Todo `SymbolFact` transporta su identidad canónica legible y su stable key.
Todo `EdgeFact` transporta `kind`, `confidence`, `provenance`, `evidence_id`,
`source_snapshot` y `resolver_version`.

`confidence` usa los niveles del modelo semántico:

```text
EXACT_TYPECHECKED
EXACT_DECLARATION_MAPPED
EXACT_PACKAGE_MAPPED
STRUCTURAL_CERTAIN
CANDIDATE
UNRESOLVED
```

Cuando `within_supported_window` es falso para el proyecto, ningún hecho de ese
proyecto puede emitirse con un nivel exacto. Se degrada a `CANDIDATE` y el
`EvidenceFact` registra el motivo y la versión efectiva del compilador.

## 6. Errores

Una respuesta de error tiene `type` igual a `ERROR` y este payload:

```text
code     cadena
message  cadena
retryable booleano
```

Códigos:

```text
VERSION_MISMATCH
UNSUPPORTED_MESSAGE
INVALID_PAYLOAD
UNKNOWN_PROJECT
BATCH_TOO_LARGE
FRAME_TOO_LARGE
ENGINE_UNAVAILABLE
CANCELED
INTERNAL
```

`FRAME_TOO_LARGE` y `VERSION_MISMATCH` terminan la sesión. El resto la
mantienen.

El supervisor clasifica además las condiciones que no producen frame: EOF,
frame truncado, JSON inválido, timeout y salida inesperada del proceso. Todas
invalidan el estado del worker y ninguna puede contaminar el grafo con hechos
parciales: un lote incompleto se descarta entero.

## 7. Límites

```text
max_frame_bytes       16 MiB
max_batch_positions   declarado en HELLO
handshake timeout     5 s
request timeout       declarado por el supervisor por tipo de petición
shutdown grace        5 s antes de terminar el proceso
```

## 8. Versionado

`v` es la versión del protocolo y vale `1`. Un cambio incompatible la
incrementa. El worker debe rechazar una versión que no soporte en `HELLO` en
lugar de intentar interpretarla.

Añadir un campo opcional a un payload existente no es incompatible. Cambiar el
significado de un campo, eliminarlo o cambiar su tipo sí lo es.
