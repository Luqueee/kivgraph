# Lo que cuesta a un proceso el snapshot que ya está compartido

`benchmarks/shared-snapshot` mide `Private_Dirty` por servidor y lo
encuentra plano al crecer el número de clientes: es el único componente que
compartir no reduce, y lo único que un demonio colapsaría a una copia. Pero
`Private_Dirty` cuenta toda página que el proceso ensució alguna vez, así que
una carga que asigna el triple de lo que conserva se ve igual que una que lo
necesita todo. Este arnés separa las dos mitades, porque se arreglan al
revés: los bytes vivos, moviendo una estructura al fichero mapeado; los
transitorios, no asignándolos.

|dato|valor|
|---|---|
|fecha|`2026-08-23`|
|commit|`c420490`|
|plataforma|`darwin/arm64`, `go1.26.4`|
|corpus|`35` repositorios, `117499` símbolos, `337314` aristas|
|fichero|`86.7 MB`|

## La carga

|mitad|bytes|por símbolo|
|---|---|---|
|asignado|`89.7 MB`|`801 B`|
|vivo|`27.7 MB`|`247 B`|
|transitorio|`62.0 MB`|`69 %` de lo asignado|
|de ello, tablas copiadas dos veces|`19.8 MB`|aritmética sobre las filas|

## Lo que se puede leer en el sitio

|sección|bytes|
|---|---|
|arena de cadenas|`56.1 MB`|
|tabla de claves estables|`6.3 MB`|
|registros de ancho fijo|`12.0 MB`|
|aristas de los dos CSR|`7.7 MB`|
|**total**|`82.2 MB`|

El perfil vivo se escribe con el snapshot todavía alcanzable, que es el único
momento en que sus estructuras son atribuibles: `benchmarks/snapshot-heap/live.pprof`.

## Hallazgos

- La mayor parte de lo que asigna la carga no es la respuesta. Los bytes vivos son lo que un lector conserva; el resto ensucia una página y no sostiene nada, y un perfil de heap tomado después de la carga no puede ver ni uno de ellos.
- Cada tabla de ancho fijo se copia dos veces. Un decodificador asigna un slice de Go por sección y copia dentro los bytes mapeados (`internal/hotsnapshot/file.go`, las llamadas `decode*` de `readSnapshot`), y `NewGraphSnapshot` vuelve a copiar cada uno de esos slices (`internal/hotsnapshot/snapshot.go:256-269`). Las parejas del perfil no son parecidas, son exactas: `decodeSymbols` y la línea de los símbolos asignan los mismos bytes, y lo mismo la evidencia y las dos tablas de aristas.
- La segunda copia no es defensiva en este camino. `NewGraphSnapshot` copia para que un llamante pueda seguir mutando los slices que pasó, que es lo que hace el constructor. El lector pasa slices que decodificó una sentencia antes y que nadie más puede nombrar, así que ceder la propiedad retiraría el gemelo sin relajar el contrato público.
- La validación es la otra parte con nombre de la mitad transitoria. `validReverseCounterpart` construye un mapa con clave en cada arista directa para probar que el CSR inverso es su permutación, y lo tira (`internal/hotsnapshot/snapshot.go:478`). Un mapa de bits sobre las aristas directas contestaría lo mismo con una fracción de los bytes.
- El arena ya se lee en el sitio y es la sección más grande del fichero, que es por lo que los bytes vivos son una fracción de él. Lo que queda en el heap son las tablas, y el fichero declara cuántas filas tiene cada una, así que nada de ellas hay que reconstruirlo para poder confiar en él.

## Limitaciones

- Los bytes vivos y asignados son contabilidad del runtime de Go, no páginas residentes. `Private_Dirty` es mayor que cualquiera de los dos: lleva además metadatos del runtime, pilas y heap que el asignador nunca devolvió al sistema. Eso lo mide `benchmarks/shared-snapshot`, y sólo en Linux.
- La cifra transitoria es la basura de la propia carga, medida como lo asignado menos lo vivo después de un `GC`. Es un suelo: una página que ensucia una asignación transitoria sigue sucia hasta que el asignador la barre.
- El total mapeable es aritmética sobre las filas que el snapshot declara, no una medición de lo que un lector toma en el sitio hoy. El arena y la tabla de claves estables ya se toman en el sitio.
- Un proceso, una carga. Nada de aquí dice qué paga un segundo lector de la misma generación, que es la pregunta que contesta `shared-snapshot`.
- La cifra viva es el heap entero del proceso después de la carga, que incluye el del propio arnés, así que es una cota superior de la del snapshot. Pasadas repetidas de una misma compilación coinciden byte a byte; dos compilaciones de este arnés difirieron en torno a un megabyte, así que una comparación es entre pasadas del mismo binario y no entre ediciones de él.
