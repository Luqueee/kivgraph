# ADR 0041: La biblioteca estándar de Rust como proveedor sintético

- **Estado:** aceptada
- **Fecha:** 2026-08-13
- **Revisa:** ADR 0033, ADR 0035, ADR 0039

## Contexto

Sin `core`, `std` y `alloc` en el grafo, cuatro cosas que un lector de código
Rust pregunta no tienen respuesta, y las cuatro son la misma ausencia:

- `#[derive(Clone, Debug, Default, PartialEq)]` no deja **ninguna** relación.
- Un operador sobrecargado no alcanza el trait que implementa.
- El operador `?` no alcanza `Try::branch`.
- Toda llamada a la biblioteca estándar desaparece.

Medido sobre `testdata/rust/stdlib`, un fixture de 58 líneas: **21 usos** se
declaraban `CRATE_PROVIDER_NOT_FOUND` y **cero** se convertían en arista.

La tarea estaba aparcada por decisión, no por olvido: publicar la biblioteca
cambia el tamaño del grafo y la identidad deja de ser puramente
repositorio-relativa.

## Decisión

La biblioteca estándar entra al grafo como un **repositorio sintético**, opt-in
con `rust.index_sysroot`, apagado por defecto.

### Alcance: el workspace `library`, y nada por debajo

La unidad de análisis es el workspace Cargo que vive en
`<sysroot>/lib/rustlib/src/rust/library`. Un toolchain vendoriza dentro de ese
directorio varios workspaces independientes -`backtrace`, `compiler-builtins`,
`portable-simd`, `stdarch`, `windows_link`-, que no son la biblioteca estándar:
`library/Cargo.toml` no los resuelve como miembros y a dos de ellos los excluye
por nombre.

Indexarlos se midió y se rechazó:

```text
                       unidades  no cargan  símbolos  aristas
todo el árbol             16          6      23.417   185.238
sólo el workspace          1          0      19.533   169.535
```

Seis workspaces no cargaban -sus propios miembros no se distribuyen con las
fuentes-, aportaban crates que nadie del corpus puede nombrar, y -la razón por
la que no es un intercambio- **dos pasadas sobre el mismo toolchain producían
distinto número de aristas**, porque sus build scripts ven lo que la pasada
anterior dejó en el directorio de destino. Un grafo que no es reproducible no se
publica.

Sus workspaces se nombran en los diagnósticos de la pasada; sus crates no se
registran, porque registrar un proveedor que la pasada no indexa compone una
clave que nadie publica.

### Identidad: un repositorio sintético por versión de toolchain

El repositorio se llama `rust:<release>` -`rust:1.96.1`-, y ese nombre viaja
dentro de la clave estable de cada símbolo que publica. Dos toolchains no
declaran el mismo código, así que no pueden compartir nodo.

El namespace `rust:` está **reservado en el registro**: `validateRepositoryName`
rechaza un nombre de usuario que lo tome. Eso es lo que convierte el nombre en
autoridad y evita una segunda columna en el esquema canónico para responder «¿es
sintético?»: nada más en el grafo puede llamarse así.

El repositorio sintético no lleva `commit`, `branch` ni `dirty`. Nadie lo
clona, nadie puede moverlo, y una consulta de frescura sobre él responde eso.

### Atribución: por origen, nunca por versión ni por nombre

Los dos lados de la frontera **no escriben lo mismo** en el campo de versión del
moniker. Medido sobre índices reales:

```text
consumidor →  rust-analyzer cargo core https://github.com/rust-lang/rust/library/core  fmt/Display#
biblioteca →  rust-analyzer cargo core 0.0.0                                           fmt/Display#
```

El crate y el camino de descriptores coinciden, y la clave estable se construye
con esos dos y no con la versión, así que los dos lados componen la misma clave.
Lo que no se puede comparar es la versión: para un crate de la biblioteca ese
campo es un **origen**, no una release.

Por eso un crate lang resuelve comparando el origen -`IsLangOrigin`-, que es
evidencia que el analizador produjo, y nunca por coincidencia de nombre. Una
referencia que nombra `core` con una release resuelve `CRATE_VERSION_MISMATCH`;
una que no nombra versión, `CRATE_VERSION_UNKNOWN`. Un repositorio registrado
que declara un crate de la biblioteca es `AMBIGUOUS_CRATE_PROVIDER`, igual que
dos repositorios entre sí, y dos toolchains a la vez también.

### Invalidación: la release de `rustc`

La huella de la entrada de caché incluye `rustc --version`, además de
`rust-analyzer --version`, `cargo --version` y el propio `index_sysroot`. Un
cambio de toolchain invalida cada hecho tomado de él. Los 16,8 s de la pasada
fría se pagan una vez por toolchain: con caché, la misma pasada cuesta 847 ms.

### Aristas que el proveedor no publica

`impl Add for u32` no existe en ningún rango de código: `core` la genera con
`add_impl!`. El analizador **resuelve** el uso a ese símbolo y el índice no lo
define, así que la unidad consumidora compone una clave que su proveedor no
publicará nunca.

Hasta ahora una unidad aceptaba a crédito todo destino atribuido a otro
repositorio -no puede ver los hechos del proveedor- y el merge cerraba la
arista. Cuando el crédito no se devuelve, la arista queda colgante y aborta la
publicación entera.

El merge ahora cierra esos casos: retira la arista y declara el uso como
`PROVIDER_DEFINITION_NOT_INDEXED`, con el crate y el símbolo que se pidieron. Se
cuenta en `FullReport.EdgesWithoutProvider`.

**Un proveedor derivado se declara una vez por símbolo, sin posición.** Un
proveedor registrado sigue declarando cada uso con su línea, porque quien lo lee
quiere ir allí. Con la biblioteca no: declara todos los operadores de todos los
primitivos con macros, y medido sobre un repositorio real -`lanplay`, 130
ficheros- eso eran **4.165 entradas sobre 205 símbolos** -`u64::add`, `f64::div`,
`usize::PartialEq::eq`- que enterraban los 791 huecos que sí hablaban de sus
dependencias. Sobre el corpus completo, `5.635 -> 267`, y los no resueltos del
código real bajaron de `6.426` a `1.058` sin perder ni una de las 17.644 aristas
hacia la biblioteca. Una clase que es propiedad del proveedor se declara como
tal, que es lo que ya hace `MACRO_EXPANSION_DISABLED`.

Eso destapó un hueco preexistente en la clave: `UnresolvedKey` se construía sobre
el fichero, que lleva el repositorio dentro, así que dos entradas **sin** fichero
-una clase, un módulo que no carga- de repositorios distintos eran una sola fila,
y el motor las rechazaba como clave primaria duplicada en cuanto dejaban de
serlo. Sin fichero, el ámbito es el repositorio. La
descripción viaja con la unidad que compuso la clave, también en su entrada de
caché: sin eso, una pasada caliente no podía cerrar lo que la fría había
publicado.

### Superficie MCP

El proveedor derivado se **retira por defecto** de las cuatro tools servidas que
pueden devolver una de sus filas: `find_symbol`, `find_references`,
`trace_dependencies` y `get_blast_radius`. `find_references` sobre `Clone`
alcanza medio corpus, y una página de `core` no es lo que pregunta quien pregunta por su
código. Dos cosas lo anulan: pedirlo con `include_derived`, y nombrar el
repositorio en `repo`, porque un filtro explícito es una petición.

Retirar una fila es una decisión de página y nunca una afirmación sobre lo
observado: la arista sigue publicada con su confianza exacta.

`graph_status` publica una sección `derived` con sus repositorios, paquetes,
ficheros, símbolos, sus propios no resueltos y dos contadores de aristas -las suyas propias y las que
entran desde un repositorio registrado-. Una arista se atribuye al repositorio
de su símbolo origen, que es el lado que hizo la observación: contar como de la
biblioteca las que llegan a ella escondería exactamente lo que la función
existe para producir. `list_repositories` marca la fila con `derived`.

## Consecuencias

Con la biblioteca en el grafo, el fixture publica lo que antes perdía:

```text
Offset      IMPLEMENTS      -> ops::arith::Add
Offset      REFERENCES      -> clone::Clone, cmp::PartialEq, default::Default, fmt::macros::Debug
parse_line  REFERENCES      -> result::impl::Result<T, E>::Try::branch
parse_line  CALLS_DIRECT    -> str::impl::str::parse, str::impl::str::trim
render      CALLS_DIRECT    -> string::impl::String::push_str, string::impl::T::ToString::to_string
```

El coste, medido en `benchmarks/rust-engine/`:

```text
                símbolos   aristas   frío       caliente
sin sysroot            6        19    1.276 ms      54 ms
con sysroot       19.533   169.535   16.826 ms     847 ms
```

Lo que sigue ausente, y declarado:

- Los `impl` que la biblioteca genera con macros. Tres en este corpus.
- Los workspaces vendorizados dentro del directorio de la biblioteca.
- Los crates de crates.io que la biblioteca usa y nadie registró
  (`CRATE_PROVIDER_NOT_FOUND`).

## Cuatro bugs preexistentes que esto destapó

Tres de ellos sólo eran visibles al publicar: la pasada validaba el conjunto de
hechos y fallaba después, al construir la generación.

- **`DiscoverCargo` rechazaba un workspace vendorizado**, y la biblioteca lleva
  `library/backtrace` así. Cargo sólo rechaza ser raíz y miembro a la vez.
- **Un fichero indexado por una dependencia de ruta hacia un crate que ningún
  manifest declara se publicaba sin paquete**, y el snapshot lo rechazaba
  después de analizar el corpus entero. Ahora se descarta y se cuenta en
  `FilesWithoutPackage`, como ya se hacía con sus declaraciones.
- **Una colisión de clave publicaba dos declaraciones**, así que el grafo
  afirmaba que un símbolo vive en dos ficheros y la copia canónica lo rechazaba:
  `Copy exception: Node has more than one neighbour in table DEFINES`.
- Y el cuarto, que no fallaba nunca y era el peor:

### El índice de claves estables no era determinista

El índice de claves estables se construía recorriendo un **mapa**, así que
cuando dos símbolos del analizador comparten una clave -la clave lleva crate,
camino y kind, nunca la versión, y rust-analyzer tiene bugs que emiten una
declaración dos veces- el orden de iteración decidía cuál ganaba, y con ello si
los usos dentro de su cuerpo tenían símbolo origen.

Sobre corpus pequeños no se veía. Con la biblioteca estándar, dos pasadas sobre
un corpus idéntico publicaban **170 aristas de diferencia** en la raíz de `std`.
El analizador no era el culpable: dos invocaciones producen un índice idéntico
byte a byte, verificado por digest.

El ganador se decide ahora sobre la lista ordenada de definiciones, primero
gana, y la colisión se sigue nombrando en los diagnósticos. Lo defiende
`TestAnalyzeResolvesAKeyCollisionTheSameWayEveryTime`, que no depende de la
suerte del orden de mapa: nombra al ganador.

## Alternativas descartadas

- **Una columna `synthetic` en el esquema canónico.** Habría subido el esquema y
  el formato de fila y exigido un rebuild completo a todo el mundo, por un
  booleano que el nombre ya lleva.
- **Indexar sólo lo que alcanzan los repositorios registrados.** Exige una
  segunda pasada y deja el conjunto de símbolos dependiendo del corpus que
  consulta: dos instalaciones con los mismos repositorios podrían publicar
  grafos distintos.
- **Un repositorio sintético `rust` sin versión.** Abarata la reindexación y
  afirma que un símbolo de `core` es el mismo entre releases.
- **Doblar la biblioteca dentro del índice de cada consumidor.** El analizador
  no lo ofrece -el índice de un crate sólo trae sus propios documentos- y
  duplicaría los nodos de `core` una vez por repositorio.
