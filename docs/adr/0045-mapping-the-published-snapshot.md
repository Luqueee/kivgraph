# ADR 0045: Publicar el HotSnapshot y mapearlo

- **Estado:** propuesta
- **Fecha:** 2026-08-16
- **Revisa:** de dónde sale el HotSnapshot de un servidor, y qué comparten dos servidores del mismo grafo

## Contexto

El ADR 0044 dejó a un servidor sin crecer, pero no cambió de dónde sale su
snapshot. Una generación en disco es `graph.db` y su digest, así que **cada
servidor lo deriva escaneando el grafo canónico**, incluido el que sólo sigue
una generación que publicó otro.

Lo que eso cuesta, medido sobre el grafo de `devlabs` -189 MB, 102.894
símbolos-:

```text
por construcción     1.003 MB asignados · 1,7 s · VmHWM ~1,05 GB
snapshot vivo          173 MB
tres servidores        3 × 173 MB privados, 0 páginas compartidas
frecuencia             diez generaciones en ochenta minutos con 42 repositorios
```

Tres servidores hacen tres veces el mismo trabajo sobre los mismos bytes y
guardan tres copias privadas del mismo resultado inmutable. El grafo publicado
no cambia nunca: es el caso de libro de una proyección que se escribe una vez y
se mapea.

## Decisión

El publicador escribe el snapshot en la generación, junto a `graph.db`, y ningún
otro servidor lo vuelve a derivar del grafo canónico. Va en dos fases, y el
orden no es una comodidad: la primera se lleva la mayor parte del coste sin
tocar ni la API ni un solo byte reinterpretado, y la segunda es la que exige
ambas cosas.

**Fase 1 -- leerlo en vez de escanear.** El servidor que sigue una generación
carga el fichero y construye el snapshot desde él con el mismo
`NewGraphSnapshot` de hoy. Desaparecen el escaneo canónico, la conversión de
filas y con ellos los 1.003 MB asignados y los ~1,05 GB de `VmHWM` por
instalación; lo que queda es leer y validar. Sin `unsafe`, sin cambio de API y
con el camino actual intacto como respaldo.

**Fase 2 -- mapearlo en vez de copiarlo.** Es la que comparte páginas entre
procesos, y tiene una condición que la fase 1 no tiene: reinterpretar una tabla
mapeada como `[]T` exige que `T` no lleve punteros, y `SymbolRecord.StableKey` es
una `string`. O el registro guarda su clave como `InternedString` -- y entonces
la clave entra en el arena, que además deduplica sus bytes -- o la tabla mapeada
tiene su propia forma y `Symbol` materializa el registro por llamada, que ya
devuelve una copia. Las demás tablas y los dos CSR ya son libres de punteros:
`PackedEdge` incluso declara su layout de 16 bytes apto para un array de
aristas. Esa elección se toma con la medición de la fase 1 delante, no antes.

**Lo que en ninguna de las dos se persiste son los cuatro índices**
-`map[StableKey]SymbolID`, los dos `map[InternedString][]SymbolID` de nombre y
nombre cualificado, `map[RepoPathKey]FileID` y `map[PackageID][]…`-. Son las
únicas estructuras con estado de hash, y reconstruirlas es una pasada sobre
tablas que ya están en memoria: sin escaneo de la base y sin decodificar
cadenas. La alternativa -un índice ordenado en el fichero y búsqueda binaria-
cambiaría un `SymbolByStableKey` de 30 ns por uno de cientos de ns para ahorrar
la parte más pequeña del snapshot. No se hace.

Lo que se escribe, en las dos fases: la tabla de cadenas -`StringTable` ya
define su serialización en orden de ID, sin que ninguna iteración de mapa
participe en el formato-, las siete tablas de registros y los dos CSR.

## Lo que hay que respetar

- **El formato se valida antes de usarse**, como el payload `LGVB` del visor:
  magic, versión, tabla de secciones con offset y longitud, y el digest de
  contenido que la generación **ya guarda** en `snapshot.sha256`. Un fichero que
  no cuadra no es un error del servidor: se descarta y se construye desde el
  grafo canónico, declarándolo. Fail-closed hacia el camino que hoy es el único.
- **Reinterpretar bytes mapeados como registros exige que los registros no
  lleven punteros**, y hoy uno lo lleva: `SymbolRecord.StableKey` es una
  `string`. Es la condición de la fase 2 y la razón de que sea la segunda. Cada
  sección pasa además su comprobación de tamaño, alineación y número de
  elementos derivada de la cabecera. Es la parte que hay que revisar con lupa:
  un decoder equivocado produce un grafo que parece correcto, y ya existe el
  precedente de cómo se defiende eso -- la ruta columnar del scan conserva
  `scanCanonicalTuples` como oráculo y se compara campo a campo.
- **Little-endian y sin relleno implícito.** Los objetivos son `linux/amd64` y
  `darwin/arm64`; el formato lo declara y el lector lo comprueba en vez de
  suponerlo.
- **Una generación mapeada se puede podar.** `clean` desenlaza el directorio y
  el mapeo sobrevive por su inodo, que es el mismo trato que ya recibe un
  servidor vivo cuyo grafo dejó de existir: sigue sirviendo lo que tiene y no
  instalará nada más nuevo.
- **El fichero vive dentro del directorio de la generación**, no en el
  `snapshots/` del estado: así una generación es un directorio autocontenido y
  podarla se lleva su snapshot sin un segundo barrido.

## Qué se espera medir

La forma de la prueba, antes de escribir el código, para que no se pueda
declarar éxito con la métrica equivocada:

- `Pss` contra `Rss` en `smaps_rollup` de dos servidores del mismo grafo: hoy
  `Private_Dirty` es el 100 % del RSS, y la única prueba de que compartir
  funciona es que el volumen pase a `Shared_Clean`.
- RSS por servidor y suma de los tres, contra los 253-255 MB de hoy.
- Tiempo de un seguidor en instalar una generación nueva, contra los 1,7 s de
  una construcción.
- `VmHWM`, que hoy es ~1,05 GB por servidor y es lo que un mapeo debería quitar
  del todo en el seguidor.
- Que el digest del snapshot mapeado sea idéntico al del construido, y que las
  suites `go test ./...` y `make test-ladybug` pasen sin tocar ningún test de
  consulta: la superficie no cambia.

## Alternativas descartadas

**Dejarlo como está y confiar en el ADR 0044.** Quita el crecimiento, no la
duplicación: tres servidores siguen costando tres veces 173 MB y reconstruyendo
cada uno por cada generación publicada.

**Construir el snapshot una vez en vez de tres dentro del proceso.** Sigue en
pie como mejora de CPU y de pico -1.003 MB y 1,7 s-, y es independiente de esto.
No comparte nada entre procesos, que es el problema que este ADR ataca.

**Un solo servidor compartido por todos los clientes.** Es la solución del
agregador, ya disponible, y está descartada por decisión: un cliente lanza su
servidor y eso no se negocia con el usuario.

**Mapear también los índices.** Cambia el coste de cada consulta por el ahorro
de la parte más pequeña. Si alguna vez el índice pesa más que el volumen, se
revisa con medición.
