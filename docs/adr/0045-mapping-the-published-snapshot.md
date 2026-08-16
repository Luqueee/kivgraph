# ADR 0045: Publicar el HotSnapshot y mapearlo

- **Estado:** fases 1 y 2a aceptadas e implementadas; fase 2b propuesta
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

## Lo medido en la fase 1

`devlabs`, generación `000090`: 41 repositorios, 5.050 ficheros, 102.894
símbolos, 262.750 aristas, `graph.db` de 189 MB. El snapshot publicado pesa
`74,6 MB`. Tres servidores reinstalados sobre la misma generación, antes
derivando y después leyendo:

```text
                 RSS por servidor   VmHWM por servidor
derivando            253-255 MB          787-836 MB
leyendo              150-152 MB          264-269 MB
```

−41 % de residente y −67 % de pico, y los tres juntos pasan de 759 a 452 MB. El
pico es lo que más baja porque es lo que se deja de hacer: ni escaneo canónico
ni conversión de filas, y con ellos los 1.003 MB que se asignaban por
instalación.

Corrección: la primera medición dio 787-836 MB de pico *después* de la fase 1 y
parecía desmentirla. No la desmentía: el camino de arranque de `serve` era el
único que seguía derivando, y es justo el que toma todo servidor al nacer. Vale
la pena anotarlo porque el fallo no se ve en ninguna suite -- las dos rutas
producen el mismo grafo, y sólo la memoria las distingue.

La corrección se comprueba sobre datos reales, no sobre un fixture: el oráculo
compara el snapshot publicado contra uno derivado del mismo `graph.db`, símbolo
a símbolo y arista a arista por la superficie pública -- 102.894 símbolos, 5.050
ficheros, 262.750 aristas-, porque dos snapshots que coincidan en sus recuentos
y discrepen en el fichero de un símbolo pasarían cualquier comprobación más
gruesa.

Lo que la fase 1 **no** cambia, y sigue esperando a la fase 2: `Private_Dirty`
sigue siendo el 100 % del RSS. Tres servidores leen el mismo fichero y guardan
tres copias privadas de lo que leyeron; compartir páginas es mapear, no leer.

## De qué está hecho un snapshot cargado, y por qué eso reordena la fase 2

Antes de mapear conviene saber qué fracción se puede mapear. Medido sobre la
misma generación -122,1 MB vivos, 102.894 símbolos, 481.494 cadenas internadas-:

```text
volumen mapeable -tablas, los dos CSR, los bytes de las cadenas-   64,90 MB   53 %
mapa de búsqueda del interner                                      20,45 MB   17 %
cabeceras de string, dieciséis bytes por valor internado            7,35 MB    6 %
los dos mapas de símbolos más pesados                               6,21 MB    5 %
resto -los otros índices, las rebanadas de sus cubetas, holgura-      ~24 MB   19 %
```

Cargar cuesta `144 ms`, contra `1,7 s` de derivar: doce veces menos, y es el
tiempo que un seguidor tarda en instalar una generación nueva.

Eso parte la fase 2 en dos trabajos que no valen lo mismo ni cuestan lo mismo:

**2a -- retirar el mapa de búsqueda de la tabla de cadenas.** `Lookup` es lo
único que lo usa, y una consulta lo llama una o dos veces para resolver un
nombre; el mapa cuesta 20,45 MB **en cada proceso**. Una permutación ordenada
por valor y una búsqueda binaria son 1,9 MB -cuatro bytes por cadena- y
diecinueve comparaciones. Además se puede persistir en el fichero, que es
derivado y determinista: el escritor ordena una vez y ningún lector paga el
`sort`. No hay `unsafe`, ni mmap, ni cambio de API, ni código por plataforma, y
**mejora también a un servidor solo**, que es lo que mapear no hace.

**2b -- mapear el volumen.** Sigue siendo la palanca mayor: 64,90 MB que pasan
de tres copias privadas a una compartida. Pero su precio es el que ya estaba
escrito -reinterpretar bytes, `SymbolRecord.StableKey` dejando de ser una
`string`- más uno que sólo aparece al medir: `StringTable.String` devuelve una
`string`, y sobre un arena mapeado eso es una vista de memoria que deja de ser
válida cuando el mapeo muere. Una respuesta que sobreviva al relevo de una
generación leería memoria liberada, así que 2b necesita además una regla de vida
útil -- copiar al entregar, o no desmapear mientras un lector viva.

El orden es 2a y después 2b, y no por comodidad: 2a se lleva un tercio del
beneficio por una fracción del riesgo, y deja 2b mejor dimensionada -- con 2a
hecha, mapear el volumen decide entre 37 MB privados por proceso y 102.

## Lo medido en la fase 2a

Misma máquina, mismo corpus, generación `000093`:

```text
                          antes de 2a     con 2a
snapshot vivo               122,1 MB     97,2 MB
parte no mapeable            57,2 MB     32,3 MB
volumen mapeable             64,9 MB     64,9 MB
tiempo de carga               144 ms     151 ms
fichero publicado           71,15 MB    73,00 MB
```

−24,9 MB en cada proceso que sostiene el snapshot, a cambio de `1,85 MB` de
fichero -- la sección del orden -- y siete milisegundos de carga. En servidores
reales, los tres de `devlabs`: `125-132 MB` de RSS con pico de `241-249 MB`,
contra `150-154 MB` y `264-271 MB`.

El recorrido completo sobre el mismo grafo inmutable, por servidor:

```text
antes del ADR 0044   1,07-1,13 GB y subiendo   pico 2,1-2,3 GB
con 0044               253-255 MB estables     pico   787-836 MB
con 0045 fase 1        150-154 MB              pico   264-271 MB
con 0045 fase 2a       125-132 MB              pico   241-249 MB
```

Queda la fase 2b, y ahora decide entre `32 MB` privados por proceso y `97`: el
volumen es dos tercios de lo que queda, no la mitad.

Su primer trozo ya está, y vale por sí solo: el fichero se **mapea** para
decodificarlo y el mapeo se libera al acabar, en vez de leerlo al heap. La
asignación por carga baja de `244,5` a `148,7 MB` -- los 73 MB del fichero y su
basura-- y el pico de un servidor de `241-249` a `233-235 MB`. Es seguro porque
todo decodificador copia, y ese es exactamente el invariante que la fase 2b
tendrá que romper a propósito y con una regla de vida útil en la mano: mantener
el mapeo vivo es querer que el snapshot **no** copie.

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
