# ADR 0042: Indexar fuera del proceso que sirve

- **Estado:** aceptada
- **Fecha:** 2026-08-15
- **Revisa:** el contrato de `serve` y el de `index_project`

## Contexto

Un cliente MCP lanza `kivgraph serve` él mismo, así que hay un servidor por
cliente y cada uno reconstruye el grafo entero en su propio heap. Medido en
`devlabs` -Linux, 24 GB, generación `000053`: 41 repositorios, 5.021 ficheros,
102.385 símbolos, 259.556 aristas, 189 MB en disco-:

```text
heap vivo tras cargar                 173 MB
pico de heap durante la carga     495-531 MB
VmHWM del proceso al cargar       808-897 MB
RSS estable recién cargado        252-373 MB
reparto de un proceso de 1,68 GB  Private_Dirty 1,663 GB · Shared_Clean 15 MB
```

Nada se comparte: los 15 MB limpios son el binario. Tres servidores vivos
costaban 2,44 GB para contestar preguntas sobre el mismo grafo.

Lo que explica el salto de 373 MB a 1,68 GB no es una fuga -el heap vivo se
mantuvo plano en 173-175 MB a lo largo de 320 consultas, y ni `SnapshotStore`,
ni `Follow`, ni `Service` retienen nada-: es que **el proceso que sirve también
indexaba**. `resyncOnBranchChange` llama a `Service.Reindex` cuando HEAD se
mueve en cualquier repositorio registrado, y la tool `index_project` hace lo
mismo bajo demanda. La correlación es exacta:

```text
commit en appeals-module    2026-08-15 13:31:25
generación 000053 publicada 2026-08-15 13:31:37
```

Una pasada completa sostiene a la vez el universo de tipos de cada módulo Go, la
respuesta de cada worker TypeScript y el índice SCIP de cada workspace Cargo. Su
pico se mide en gigabytes -`VmHWM` de ese proceso: 3,346 GB- y el heap de Go
conserva la arena: cuando la pasada termina, el proceso se queda en 1,68 GB
mientras viva, porque nada de lo que hace después vuelve a necesitar esa memoria.
El otro servidor, que perdió `resync.lock`, se quedó en 586 MB.

## Decisión

**Una pasada de indexación nunca ocurre en el proceso que responde consultas.**
`Service.IndexProjects` y `Service.Reindex` ejecutan `kivgraph index --full
--json` como proceso hijo y leen su resultado; el pico muere con el hijo.

El hijo es **este mismo ejecutable** (`os.Executable`). Lleva la misma
biblioteca de almacenamiento, el mismo resolver y el mismo esquema: un grafo
publicado por otra cosa no es el grafo que este proceso sabe servir.

El reparto de responsabilidades no cambia de dueño:

- El **padre** conserva el registro. Valida el candidato, lo persiste antes de
  arrancar al hijo y lo restaura si el hijo falla. Registrar es un acto suyo, y
  el consentimiento se le pidió a él.
- El **hijo** lee el registro del disco y publica la generación. Por eso recibe
  siempre `--config`, `--repositories` y `--resolver-version`: un hijo al que no
  se le nombra la configuración indexaría el estado por defecto, y un `--config`
  temporal publicaría sobre el grafo real.
- El **padre** construye el `HotSnapshot` de la generación publicada, como
  siempre. Eso no se puede delegar: el servidor sirve desde un snapshot en su
  propio heap, y `Follow` lo pagaría igual dos segundos después.

### El canal

`index --full --json` escribe en `stdout` **sólo** un flujo de eventos JSON
delimitados por línea, declarado en `internal/indexing`: `progress`, cero o más,
y `result`, exactamente uno y el último. El informe que leería una persona no se
escribe en ese modo -intercalado, sería un flujo que el padre no puede parsear- y
`stderr` conserva el registro del hijo, del que se guarda la cola para explicar
un fallo.

Los contadores viajan por ahí, no por el registro. Un `msg` de log es texto para
una persona; el resultado de `index_project` es un contrato -los contadores de
los tres lenguajes- y no se deriva de parsear líneas de bitácora.

Un lector ignora una clase de evento que no conoce: el flujo es un protocolo
entre dos builds de este programa, y rechazar una línea desconocida convertiría
añadir un evento en un cambio incompatible.

### El progreso sobrevive

`index_project` emite `notifications/progress` cuando la petición trae
`progressToken`, porque un rebuild dura minutos y un cliente aplica su propio
timeout. Ese contrato no se relaja: cada evento `progress` del hijo se reenvía
como antes. Lo que el reenvío no lleva es el detalle de diagnósticos y etapas,
que se queda en el informe del hijo -que nadie estaba leyendo desde una tool-.

### Lo que se devuelve al sistema operativo

Aparte, y por la misma medición: tras publicar un snapshot se llama a
`rebuild.ReturnBuildMemory`. Es el único momento en que el transitorio de la
construcción está demostrablemente muerto -el snapshot está publicado, o se
descartó porque otro publicador ganó- y un servidor no tiene nada que hacer
hasta la siguiente petición.

No se fija `GOMEMLIMIT`. Un límite acotaría también al indexador, cuyo pico es
el trabajo mismo, y acotarlo cambia memoria por un GC que no para.

Y un `serve` que sólo carga su snapshot ya no reserva una porción de la máquina
para leer un fichero: una apertura de sólo lectura del grafo canónico recibe un
buffer pool proporcional a ese grafo -del doble de su tamaño, entre 256 MiB y
2 GiB- en lugar del 80 % de la memoria del sistema que da el motor por defecto.
Una apertura de escritura conserva el defecto: un `COPY` de millones de filas es
exactamente la carga para la que esa caché existe.

## Consecuencias

- Un servidor deja de ser el sitio donde vive el pico de una indexación. Cuesta
  su snapshot y el transitorio de construirlo, que ahora devuelve.
- Un `git commit` sigue reconstruyendo el grafo, y sigue costando lo que cuesta;
  lo que cambia es que el coste no se queda pegado al servidor.
- El hijo hereda la cancelación del padre. Un `serve` que muere mata su
  indexación, igual que antes, y una pasada muerta a mitad la limpia el store al
  publicar la siguiente.
- Un `index_project` ya no puede informar de diagnósticos por unidad ni de
  etapas del rebuild. Nunca lo hacía: `ProjectResult` sólo llevó contadores.
- La superficie del CLI gana `--json`, que es público y documentado: quien quiera
  guionizar una pasada lee el mismo flujo que lee el servidor.

## Alternativas descartadas

**Parsear el registro del hijo.** `index --full` ya escribe registros JSON en
`stderr` cuando no es una terminal, así que el padre podría leer de ahí. Se
descarta: convierte el texto de un `msg` en una API, y los contadores del
resultado son un contrato.

**Un subcomando oculto.** Un `__index-worker` con su propio protocolo evita
añadir una bandera pública. Se descarta: la superficie ya tiene un comando que
hace exactamente esto, y una bandera que la documentación nombra es más honesta
que un comando que la ayuda esconde.

**Compartir un grafo entre clientes.** Un solo servidor por máquina, o un
snapshot en `mmap`, elimina la multiplicación por cliente -que es el otro factor
medido, y el mayor-. Queda fuera de este ADR: cambia el transporte y la forma
del producto, no el sitio donde corre una pasada.
