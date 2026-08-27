# En qué se van los bytes residentes de un `HotSnapshot`

`LUQUE-2001`. Un cliente MCP lanza `kivgraph serve` él mismo, así que hay un
servidor por cliente y **cada uno reconstruye el grafo entero en su propio
heap**. La fase 20 quiere cambiar eso con un fichero, y elegir su formato sin
saber lo que pesa cada componente es elegir a ciegas: un índice ordenado y una
tabla hash son una preferencia hasta que alguien los pesa.

Éste es el desglose. Las cifras crudas están en `results.json` y el perfil de
heap vivo en `profiles/inuse.pprof`.

## Entorno y provenance

|dato|valor|
|---|---|
|fecha|`2026-08-22`|
|comando|`go run ./benchmarks/hot-snapshot-footprint --graph <graph.db> --generation <id>`|
|corpus|`workspace`, 37 repositorios registrados, Go + TypeScript + Rust|
|generación|`000001`, un pase completo, `graph.db` de `234 MB`|
|contenido|`123.531` símbolos, `372.320` aristas, `372.320` evidencias, `4.768` ficheros|
|`go`|`go1.26.4 darwin/arm64`|

El arnés **se niega a publicar** si la generación que abre no es la que el
llamante declaró: una huella etiquetada con la generación equivocada es peor que
no tener huella.

## Después de `LUQUE-2003`: sin un solo mapa

|magnitud|`LUQUE-2001`|`LUQUE-2002`|`LUQUE-2003`|
|---|---|---|---|
|residente|`171,5 MB`|`109,1 MB`|**`101,7 MB`**|
|por símbolo|`1.389 B`|`883 B`|**`824 B`**|
|cobertura|`64,6 %`|`100,1 %`|**`99,9 %`**|
|partes medidas por heap|`4`|`3`|**`0`**|

Los tres mapas exactos costaban `9.592.896` bytes. Los arrays planos guardan la
misma información en `1.961.120`: **`4,9×` menos**, y el ahorro medido es
`7.394.536` bytes.

|índice|antes (mapa)|después (plano)|
|---|---|---|
|`symbolsByQName`|`6.114.016`|`1.145.496`|
|`symbolsByName`|`3.369.968`|`758.408`|
|`fileByRepoPath`|`108.912`|`57.216`|
|`packageIncoming`|nunca medido|`2.024`|

`packageIncoming` no aparecía porque era un `map[PackageID][]PackageDependencyRecord`
que guardaba **copias de las filas**: medirlo era medir una segunda tabla de
dependencias. Hoy son offsets direccionados por el ID denso más un `uint32` por
dependencia, y cabe en `2 KB`.

**La fila que más dice de este cambio es la última de la primera tabla.** Ya no
queda ninguna parte medida por heap: mientras los índices eran mapas había que
reconstruir uno equivalente y observar el montón, porque un mapa cuesta lo que el
runtime decide y ninguna aritmética lo predice. Un número que nadie puede derivar
es un número contra el que nadie puede diseñar.

El perfil de heap vivo ya no contiene ni `makemap` ni `mapassign`, y
`cloneSymbolLists` -que eran `6,18 MB`- ha desaparecido. Lo que queda del
snapshot son `65,89 MB` de `Freeze`, `23,09 MB` de `NewGraphSnapshot`, `6,91 MB`
de `NewStableKeyTable` y `0,67 MB` de `newSymbolIndex`.

La latencia no empeoró, y la cola mejoró. Contra la medición previa con el mismo
corpus y la misma semilla (`benchmarks/mcp-client --clients 4`):

|métrica|antes|después|
|---|---|---|
|`p50` ida y vuelta|`0,0350 ms`|`0,0349 ms`|
|`p99` ida y vuelta|`0,2323 ms`|`0,2321 ms`|
|`p99` backend de `get_symbol`|`1.291 ns`|`791 ns`|
|`p99` backend de `find_cross_repo_consumers`|`16.584 ns`|`10.417 ns`|

El `p50` no se mueve y el `p99` del backend baja entre un `20 %` y un `39 %`. Es
donde cabía esperarlo: una búsqueda binaria sobre enteros contiguos no tiene
hash, ni sondeo de bucket, ni comparación de clave, y su peor caso está acotado
por `log₂ n`.

### La decisión del prefijo, y por qué la pregunta estaba mal planteada

`LUQUE-2003` pedía decidir entre dos formas de dar orden lexicográfico a la
búsqueda por prefijo. **Ninguna hace falta, y el código lo dice:**
`scanSymbolNames` es un barrido lineal sobre todos los símbolos en orden de
`SymbolID`, y `TestPrefixSearchIsNameOnlyAndStable` fija ese orden como contrato
(`IDs[0]==0, IDs[1]==1`). El prefijo nunca consultó un índice ordenado.

Lo que sí existe es el array `order` del `StringTable`, y sirve a otra cosa:
convertir la cadena de una consulta en un `InternedString`. Cuesta **`2.558.452`
bytes**, la mitad de la fila `string table offsets+order`. Retirarlo -haciendo
que `Freeze` asigne los IDs en orden lexicográfico- ahorraría un `2,5 %` del
residente a cambio de permutar cada `InternedString` de cada registro una vez al
construir, y de rechazar los ficheros publicados cuyo arena no esté ordenado.

**No se hace ahora**, y el motivo es que su justificación escrita -servir a la
búsqueda por prefijo- no existe. Queda la cifra para que `LUQUE-2004` decida con
ella y no con una suposición.

## Después de `LUQUE-2002`: la huella medida hoy

La medición de abajo es la que motivó `LUQUE-2002`, y sigue publicada porque es
la evidencia que lo justificó. Ésta es la misma medición después del cambio,
sobre el mismo corpus y la misma generación:

|magnitud|antes|después|
|---|---|---|
|residente|`171,5 MB`|**`109,1 MB`**|
|por símbolo|`1.389 B`|**`883 B`**|
|cobertura del desglose|`64,6 %`|**`100,1 %`**|
|estabilidad entre pasadas|`0,01 %`|`0,03 %`|

**`62,4 MB` menos, un `36,4 %`**, y el desglose **cierra**: el residuo de
`60,7 MB` que antes eran búferes de lectura retenidos hoy es `-0,1 %`, dentro
del ruido de contar por tamaño de elemento.

De dónde sale el ahorro, contra lo que este informe atribuyó a cada pieza:

|pieza|bytes|
|---|---|
|búferes Arrow liberados|`+58.040.000`|
|mapa `symbolByStableKey` retirado|`+6.990.144`|
|registro más fino: cabecera de 16 → `uint32` de 4|`+1.482.372`|
|coste nuevo de la tabla de claves|`-6.917.740`|
|**atribuido**|**`59.594.776`**|
|**medido**|**`62.400.640`** (`+4,7 %`)|

El `1.482.372` es exacto: `123.531` símbolos × `12` bytes, y es lo que hace que
la tabla de símbolos pase de `56,0` a `44,0` bytes por entrada. Los `52,0` bytes
por clave del arena nuevo son la longitud real de una clave del corpus, que la
tarea estimaba en `52` caracteres de base32 antes de medirla.

La tabla de claves no aparecía antes porque sus bytes viajaban **dentro** del
búfer fijado, contados como residuo. Hoy son almacenamiento que el snapshot
posee, y por eso el desglose los nombra.

## La medición que lo motivó

**Residente: `171,5 MB`** de heap vivo, que confirma el «`173 MB`» que la fase
citaba de otra máquina. Dos pasadas coinciden dentro del **`0,01 %`**, muy por
debajo del `1 %` que pedía el criterio.

|componente|bytes|método|por entrada|
|---|---|---|---|
|**arena de strings (valores)**|`63.914.190`|analítico|`99,9`|
|tabla de evidencias|`7.446.400`|analítico|`20,0`|
|`symbolByStableKey` (mapa)|`6.990.144`|heap|`56,6`|
|tabla de símbolos|`6.917.736`|analítico|`56,0`|
|`symbolsByQName` (mapa)|`6.114.016`|heap|`49,5`|
|`offsets`+`order` del arena|`5.116.904`|analítico|`8,0`|
|aristas directas (CSR)|`4.467.840`|analítico|`12,0`|
|aristas inversas (CSR)|`4.467.840`|analítico|`12,0`|
|`symbolsByName` (mapa)|`3.369.952`|heap|`27,3`|
|tabla de no resueltos|`631.824`|analítico|`48,0`|
|offsets CSR (×2)|`988.256`|analítico|`4,0`|
|tabla de ficheros|`247.936`|analítico|`52,0`|
|`fileByRepoPath` (mapa)|`108.912`|heap|`22,8`|
|dependencias, paquetes, repositorios|`9.524`|analítico|—|
|**explicado**|**`110.791.474`**|||
|**residuo**|**`60.738.326`**|||

**Cobertura `64,6 %`, y el criterio pedía `≥95 %`. No se cumple, y el residuo no
es un misterio: está identificado.**

### El residuo son búferes de lectura, no grafo

El perfil de heap vivo, tras dos GC forzadas, dice esto:

```
65.89MB 39.30%  hotsnapshot.(*StringInterner).Freeze
58.04MB 34.62%  ladybug.newCanonicalArrowChunk     <-- desde ScanCanonical
23.73MB 14.15%  hotsnapshot.NewGraphSnapshot
 9.30MB  5.54%  hotsnapshot.cloneSymbolLists
 7.70MB  4.59%  hotsnapshot.cloneStableKeyIndex
```

**Un tercio de la huella no es el grafo: son los búferes Arrow con que se leyó la
base de datos, todavía alcanzables después de construir el snapshot.**

El mecanismo está probado de punta a punta y no es una hipótesis:

1. `newCanonicalArrowChunk` **copia** los bytes de todas las columnas de texto a
   su propio arena Go (`canonical_scan_arrow_native.go:396`).
2. Cada valor se entrega como `unsafe.String` **sobre ese arena**
   (`canonical_scan_arrow_native.go:455`).
3. El adaptador convierte el valor a `StableKey` con una conversión de tipo entre
   cadenas (`rebuild/snapshot.go:293`), que **no copia**.
4. El snapshot retiene `123.531` de esas claves. Una cadena que apunta a un arena
   mantiene vivo **el arena entero**.

`6,4 MB` de caracteres de clave estable retienen `58 MB` de búferes. Ese número
está en `results.json` como `stable_key_characters_bytes`, y no se cuenta como
componente **precisamente porque no se asigna aparte**: sumarlo sería contarlo dos
veces.

Es evidencia directa para `LUQUE-2002` de la fase 20, cuyo título ya era **«que
ninguna clave estable ocupe un puntero»**. Escrita antes de esta medición, y
apuntando al sitio correcto.

> **Cerrado por `LUQUE-2002`.** `SymbolRecord.StableKey` es ahora un `uint32`
> denso en una `StableKeyTable` que copia sus bytes, así que ninguna clave apunta
> a un búfer de lectura y no hay nada que fijar. La medición de arriba es la del
> commit indicado en la cabecera; el binario de hoy ya no emite
> `stable_key_characters_bytes`, y en su lugar cobra la tabla de claves como un
> componente normal (arena + offsets).

## Los tres que dominan, y lo que cuestan por unidad

1. **El arena de strings: `63,9 MB`**, `99,9` bytes por valor interno. Es el
   `37 %` de la huella y el `58 %` de todo lo explicado. Cualquier formato de
   fichero se juega aquí su rentabilidad: mapear el arena en vez de reconstruirlo
   es el único cambio que mueve una cifra de este tamaño.
2. **Los tres mapas de símbolos: `16,5 MB`** entre los tres, contra `6,9 MB` de la
   tabla de símbolos que indexan. **Los índices pesan 2,4 veces lo indexado**, y
   son la mitad de lo explicado que no es el arena. `symbolByStableKey` cuesta
   `56,6` bytes por símbolo, casi lo mismo que el registro completo (`56,0`).
3. **Las evidencias: `7,4 MB`** a `20` bytes por fila, la tabla plana más grande.
   Escala con las aristas, no con los símbolos.

Por unidad, que es lo que permite proyectar a un corpus con sysroot:

|unidad|bytes|
|---|---|
|por símbolo, residente total|`1.389`|
|por símbolo, sólo lo explicado|`897`|
|por símbolo, tablas e índices sin el arena|`379`|
|por arista (dos CSR + evidencia)|`44`|

La primera fila incluye el tercio que son búferes de lectura, así que es la que
**no** hay que usar para proyectar: un corpus mayor no multiplica ese residuo por
símbolo, lo multiplica por bytes leídos. Para proyectar sirve la tercera.

## Lo que esto dice del diseño, sin diseñarlo

- **Antes de escribir un fichero, hay `58 MB` gratis.** Copiar las claves
  estables cuesta `6,4 MB` y libera `58`. Es la intervención más barata que la
  fase tiene delante y no necesita formato ninguno.
- **Un fichero mapeado ataca `63,9 MB`** del arena. `StringTable` ya tiene el
  campo `borrowed` para exactamente eso, así que la pieza existe.
- **Los índices no se mapean gratis.** `16,5 MB` de mapas Go hay que reconstruirlos
  al abrir o sustituirlos por algo direccionable en el fichero, y ahí es donde la
  pregunta «índice ordenado o tabla hash» se decide con estas cifras y no con una
  preferencia.

## Limitaciones

- **Una generación, una máquina.** Un corpus con el sysroot de Rust indexado
  movería las tablas de símbolos y evidencias, y nada aquí predice cuánto.
- **Los cuatro índices se tasan reconstruyendo un mapa equivalente**, no leyendo
  el del snapshot: mismos tipos de clave y valor y misma cardinalidad, así que el
  coste es la respuesta del runtime para esa forma. Un mapa que el constructor
  hubiera crecido de otra manera podría ocupar otro número de buckets.
- **Las cifras analíticas son cuenta por tamaño de elemento.** Una slice redondeada
  a su clase de tamaño cuesta un poco más, y esa diferencia se queda en el residuo
  en vez de repartirse entre las tablas.
- **`HeapAlloc` es heap vivo, no RSS del proceso.** Las cifras de la propia fase
  ponen el RSS entre `252` y `373 MB` contra `173 MB` de heap vivo, y nada aquí
  explica ese hueco.
- **La cobertura del `64,6 %` no cumple el criterio del `95 %`.** El residuo está
  identificado y atribuido con perfil, pero identificado no es lo mismo que
  contabilizado: mientras las claves estables sigan apuntando al arena de lectura,
  ese tercio no es un componente del grafo y sumarlo al desglose sería mentir
  sobre lo que el grafo pesa.

## Reproducir

```bash
go run ./benchmarks/hot-snapshot-footprint \
  --graph ~/.local/state/kivgraph/generations/<id>/graph.db \
  --generation <id> \
  --heap-profile benchmarks/hot-snapshot-footprint/profiles/inuse.pprof

go tool pprof -inuse_space -top benchmarks/hot-snapshot-footprint/profiles/inuse.pprof
```
