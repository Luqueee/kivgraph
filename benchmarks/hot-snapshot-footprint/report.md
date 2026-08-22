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
|corpus|`kena`, 37 repositorios registrados, Go + TypeScript + Rust|
|generación|`000001`, un pase completo, `graph.db` de `234 MB`|
|contenido|`123.531` símbolos, `372.320` aristas, `372.320` evidencias, `4.768` ficheros|
|`go`|`go1.26.4 darwin/arm64`|

El arnés **se niega a publicar** si la generación que abre no es la que el
llamante declaró: una huella etiquetada con la generación equivocada es peor que
no tener huella.

## El resultado, y el hallazgo

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
