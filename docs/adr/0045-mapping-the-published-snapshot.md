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

El publicador escribe el snapshot en la generación, junto a `graph.db`, y todo
servidor lo mapea en lugar de reconstruirlo. La lectura es de sólo lectura, así
que la inmutabilidad pasa a estar garantizada por el kernel y no por que los
campos sean privados.

**Se mapea el volumen, que no tiene punteros:**

- la tabla de cadenas -`StringTable` ya define su serialización en orden de ID,
  sin que ninguna iteración de mapa participe en el formato-;
- las tablas de registros: repositorios, paquetes, archivos, símbolos,
  evidencias, dependencias de paquete y referencias no resueltas, que son
  structs planos de IDs internados, números y digests de tamaño fijo;
- los dos CSR: `[]uint32` de offsets y `[]PackedEdge`, cuyo layout de 16 bytes
  ya está declarado apto para un array de aristas.

**No se mapean los cuatro índices, que se reconstruyen al cargar:**
`map[StableKey]SymbolID`, los dos `map[InternedString][]SymbolID` de nombre y
nombre cualificado, `map[RepoPathKey]FileID` y `map[PackageID][]…`. Son las
únicas estructuras con estado de hash, y reconstruirlas es una pasada sobre las
tablas ya mapeadas: sin escaneo de la base, sin decodificar cadenas y sin el
gigabyte de asignación. La alternativa -un índice ordenado en el fichero y
búsqueda binaria- cambiaría un `SymbolByStableKey` de 30 ns por uno de cientos
de ns para ahorrar la parte más pequeña del snapshot. No se hace.

## Lo que hay que respetar

- **El formato se valida antes de usarse**, como el payload `LGVB` del visor:
  magic, versión, tabla de secciones con offset y longitud, y el digest de
  contenido que la generación **ya guarda** en `snapshot.sha256`. Un fichero que
  no cuadra no es un error del servidor: se descarta y se construye desde el
  grafo canónico, declarándolo. Fail-closed hacia el camino que hoy es el único.
- **Reinterpretar bytes mapeados como registros exige que los registros no
  lleven punteros** -hoy no los llevan- y que cada sección pase su comprobación
  de tamaño, alineación y número de elementos derivada de la cabecera. Es la
  parte que hay que revisar con lupa: un decoder equivocado produce un grafo que
  parece correcto, y ya existe el precedente de cómo se defiende eso -- la ruta
  columnar del scan conserva `scanCanonicalTuples` como oráculo y se compara
  campo a campo.
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
