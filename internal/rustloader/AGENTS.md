# Instrucciones del camino Rust (`internal/rustloader/`)

Estas reglas se suman a las de `AGENTS.md` en la raíz del repositorio, que se
leen siempre. Una instrucción de este archivo puede añadir restricciones; nunca
puede relajar un contrato de integridad, compatibilidad o verificación
declarado en la raíz.

Las reglas del motor están en `internal/AGENTS.md`.

## Autoridad y ejecución del analizador

- Rust no se analiza en proceso: la autoridad es `rust-analyzer scip`,
  invocado como proceso externo una vez por workspace Cargo. Tree-sitter no
  produce identidad ni resolución; aporta la clase sintáctica del uso y la
  visibilidad declarada, igual que el AST de Go aporta `GO_AST_CALL` sobre una
  resolución de `go/types`.
- El bundle lleva `bin/rust-analyzer`, fijado en `tools/manifest.json` por
  versión, URL y digest, y descargado por `scripts/fetch-rust-analyzer.sh`. En
  ejecución gana el binario que viaja junto al ejecutable, después una ruta
  explícita de la configuración y por último el `PATH`; `doctor` dice cuál
  respondió y `kivgraph version --json` publica su release. Lo que el bundle
  no lleva es un toolchain de Rust: sin `cargo` el analizador no carga el
  workspace, así que `doctor` lo comprueba aparte y falla nombrándolo.
- Un `HOME` aislado deja a `rustup` sin toolchains, y con eso ningún workspace
  carga. `RUSTUP_HOME` cuelga de `$HOME/.rustup`, así que un arnés que apunte
  `HOME` a un temporal -lo que hay que hacer para no tocar la generación del
  usuario- tiene que preservar `RUSTUP_HOME` y `CARGO_HOME` aparte. Sin eso
  `rustc` deja de responder, todo repositorio Rust sale
  `WORKSPACE_NOT_LOADED`, y una medición cuyo brazo limpio dependa de que algo
  cargue pasa en verde sin haber medido nada. Reproducido y arreglado en
  `benchmarks/tool-honesty`, que separa las dos cosas en `indexEnvironment`.
- El subcomando `scip` ejecuta build scripts siempre, así que la hermeticidad
  se impone desde fuera: `CARGO_TARGET_DIR` a un directorio de estado externo
  -`cargo.targetDir` no sirve, su valor es relativo al workspace-,
  `--offline --locked`, y una comprobación posterior de que el repositorio no
  ganó `target/` ni un `Cargo.lock` nuevo. `rust.allow_network` es la única
  salida declarada.
- La unidad de análisis es el workspace Cargo, no el crate: el analizador
  carga el workspace entero en cada invocación. El techo de concurrencia es
  bajo por memoria.

## Identidad y resolución

- La identidad estable de un símbolo Rust es su cadena SCIP -crate, camino de
  descriptores y sufijo-, nunca su firma: rust-analyzer no emite
  `SymbolInformation` para una declaración fuera de la raíz del workspace, así
  que un consumidor que dependiera de la firma no podría nombrar la clave que
  su proveedor publica. El `Discriminator` sale del disambiguador del
  descriptor.
- Los símbolos locales (`local N`) son un contador por documento: no son
  direccionables y nunca entran al grafo.
- Una referencia solo se convierte en arista si alguien publica su destino: el
  propio pase, o el repositorio proveedor registrado. Un símbolo del propio
  repositorio que el índice no define -el bloque `impl` al que apunta
  `-> Self`, que SCIP menciona y nunca define- se declara
  `DEFINITION_NOT_INDEXED`. Componer su clave y confiar en que otro la publique
  aborta la pasada entera con una arista colgante.
- El símbolo que contiene una referencia se obtiene del `enclosing_range` de
  las ocurrencias de definición del mismo documento, la más interna que la
  contiene. SCIP dice a qué símbolo resuelve un uso, nunca qué declaración lo
  contiene.
- `IMPLEMENTS`, `EXTENDS` y `OVERRIDES` de Rust no salen de
  `SymbolInformation.relationships`, que viaja siempre vacío: salen de la forma
  del `impl` y del bound, con los dos extremos resueltos por el analizador. El
  destino de un `OVERRIDES` se compone desde el símbolo del trait y solo se
  emite si el índice lo observó; una implementación que la gramática no ve
  -generada por una macro- queda ausente y no se adivina.
- Nombrar una función no es llamarla, y las tres formas en que Rust la mueve
  son tres relaciones distintas: argumento de una llamada
  (`PASSES_AS_CALLBACK`, con procedencia propia `RUST_SYNTAX_CALLBACK`, el
  espejo de `GO_AST_CALLBACK`), valor de un `let`, `const`, `static` o campo
  de un literal (`ASSIGNS_FUNCTION`), y resultado de un cuerpo, con `return` o
  como expresión final (`RETURNS_FUNCTION`). La gramática decide la clase y el
  analizador el destino, como en una llamada.
- Una clase de posición de valor exige además que el destino sea invocable y
  que esta pasada lo haya indexado: `takes(LIMIT)` es un argumento que no es
  un callback, y un destino de otro repositorio llega sin `Kind`. En ambos
  casos la arista degrada a `REFERENCES` en vez de afirmar lo que nadie leyó.
  El ascenso por la expresión atraviesa lo que no cambia lo nombrado -un
  camino, un préstamo, un literal de array o tupla- y nunca un acceso a
  campo: devolver `objeto.campo` no devuelve el objeto.
- El índice de claves estables se construye sobre la lista **ordenada** de
  definiciones, primero gana. Dos símbolos del analizador pueden compartir una
  clave -no lleva la versión del crate, y rust-analyzer emite duplicados-, y
  recorriendo un mapa el orden de iteración decidía el ganador: dos pasadas del
  mismo corpus publicaban 170 aristas de diferencia en la raíz de `std`.
- La visibilidad de Rust no es solo `pub`: un miembro de un `trait` es tan
  visible como el trait, y un método de una implementación de trait es
  alcanzable a través de él. Leer únicamente el modificador publicaría una API
  pública falsa.
- El `Kind` publicado de un símbolo Rust es el fino de SCIP -`struct`,
  `trait`, `field`, `trait_method`-, no el sufijo del descriptor que decide la
  clave: con el sufijo, un struct, un enum y un alias son todos `type`.
- Un uso pertenece a una declaración de su propio documento. Si la más interna
  que lo contiene se publica en otro archivo, se sube al siguiente contenedor
  y, si no queda ninguno, el uso no tiene fuente: acreditarlo sitúa la
  observación en un archivo donde no ocurrió y el snapshot lo rechaza. Un
  fallo de resolución no necesita fuente y se declara igual, con su archivo y
  su posición.
- Ninguna arista se publica hacia un destino de este repositorio que la pasada
  no publica; se cuenta en `EdgesWithoutTarget`. Un destino de otro
  repositorio es otra cosa: está ausente a propósito y lo cierra el merge.
- Un símbolo que el analizador define en más de un documento se nombra en los
  diagnósticos, con el documento que lo publica y los que no. Ver ADR 0039.

## Biblioteca estándar y proveedores

- `core`, `std` y `alloc` entran al grafo con `rust.index_sysroot`, apagado por
  defecto. Sin ellos, cuatro silencios medidos: `#[derive(...)]` no produce
  ninguna relación, la sobrecarga de operadores no alcanza su trait, el operador
  `?` no alcanza `Try::branch`, y toda llamada a la biblioteca estándar
  desaparece. Con ellos, los cuatro son aristas exactas. La ausencia sigue
  siendo una carencia declarada y nunca un fallo: una máquina sin toolchain o
  sin `rust-src` indexa sus repositorios y dice por qué no indexó la
  biblioteca. Ver ADR 0041.
- La unidad del sysroot es el workspace `library` y nada por debajo. Un
  toolchain vendoriza ahí dentro `backtrace`, `compiler-builtins`,
  `portable-simd` y `stdarch`, que no son la biblioteca estándar: seis de esos
  workspaces no cargan, aportan crates que nadie puede nombrar y sus build
  scripts hacen que dos pasadas del mismo toolchain publiquen distinto número de
  aristas. Se nombran en los diagnósticos y sus crates no se registran, porque
  registrar un proveedor que la pasada no indexa compone una clave que nadie
  publica.
- Un crate de la biblioteca se atribuye por **origen**, nunca por versión ni por
  nombre. Los dos lados de la frontera escriben cosas distintas en el campo de
  versión del moniker -el consumidor una URL, la biblioteca indexada como
  workspace `0.0.0`-, y la clave estable no lleva la versión, así que ambos
  componen la misma clave. Una referencia que nombra `core` con una release es
  `CRATE_VERSION_MISMATCH`; un repositorio registrado que declara un crate de la
  biblioteca, o dos toolchains a la vez, es `AMBIGUOUS_CRATE_PROVIDER`.
- El repositorio sintético se llama `rust:<release>` y ese namespace está
  reservado en el registro: `init` rechaza un nombre de usuario que lo tome. Eso
  es lo que hace del nombre la autoridad y evita una columna en el esquema
  canónico para responder si una fila es derivada. No lleva `commit`, `branch`
  ni `dirty`: nadie lo clona y nadie puede moverlo.
- La huella de la caché de hechos incluye `rustc --version`, así que un cambio
  de toolchain invalida cada hecho tomado de él. La biblioteca cuesta una pasada
  fría por toolchain y se sirve del caché en las siguientes.
- Una unidad acepta a crédito un destino atribuido a otro repositorio, porque no
  puede ver los hechos del proveedor. Cuando el proveedor no lo publica -`impl
  Add for u32` la genera `add_impl!` y no existe en ningún rango de código- el
  merge retira la arista y declara el uso como `PROVIDER_DEFINITION_NOT_INDEXED`
  con su crate, su símbolo y su posición; se cuenta en `EdgesWithoutProvider`.
  La descripción viaja con la unidad que compuso la clave, también en su entrada
  de caché: sin eso una pasada caliente no puede cerrar lo que la fría publicó.
- Los no resueltos de Rust se derivan de tres fuentes observadas -el registro
  de crates, el diff entre el inventario Tree-sitter y las definiciones SCIP
  del mismo archivo, y el fallo de carga del workspace-, porque el índice
  descarta en silencio los tokens sin moniker.
- Un nombre de crate declarado por varios repositorios es una ambigüedad:
  ninguno lo provee y se declara `AMBIGUOUS_CRATE_PROVIDER`. Una versión que
  el analizador no conoce (`.`) no identifica código y nunca resuelve.

## Descubrimiento Cargo

- El descubrimiento Cargo no ejecuta `cargo`: lee los manifests con
  `BurntSushi/toml` y resuelve la pertenencia por directorio, como hace Cargo.
  Un crate sin workspace por encima es un workspace de uno.
- Un manifest que declara su propio `[workspace]` dentro del directorio de otro
  es una raíz legítima, no un error: así se vendoriza un árbol independiente y
  Cargo lo acepta -la biblioteca estándar lleva `library/backtrace` justo así-.
  Lo que Cargo rechaza es un manifest que sea raíz **y** miembro del workspace
  de encima, que llama «multiple workspace roots found in the same workspace», y
  los `members` se comparan como globs.
- La frontera de un repositorio Rust es una sola decisión, y la responde
  `workspace.CargoExcludes`: lo que el descubrimiento no camina -`.git`,
  `target`, `vendor`, `node_modules` y las `exclusions` configuradas- tampoco
  entra al análisis. Un crate vendorizado con `[patch.crates-io]` es código
  que Cargo compila, que el analizador indexa y que ningún manifest de este
  repositorio declara: publicar sus usos mientras se descartan sus
  declaraciones deja una arista colgante por cada uno y aborta la pasada. Sus
  usos se declaran `CRATE_PROVIDER_NOT_FOUND`, que es lo que son.
- Un paquete Cargo compila varios crates -biblioteca, binarios, build script,
  uno por test de integración- y el moniker SCIP nombra el paquete, así que
  sus módulos raíz llegan como un símbolo definido en varios documentos. Lo
  publica el target más alcanzable, con la ruta como desempate: sólo la
  biblioteca es enlazable y es la única que otro repositorio puede nombrar.
  Eso decide dónde vive el nodo y nunca cómo se llama; la clave no cambia.

## Verificación

Los tests que ejecutan el analizador se saltan cuando `rust-analyzer` no está
instalado, así que la verificación exige tenerlo:

```bash
rustup component add rust-analyzer
go test ./internal/rustloader/... ./internal/indexer/ -run Rust
```

Estar en el `PATH` no es estar instalado: rustup deja un proxy llamado
`rust-analyzer` para cada toolchain, exista o no el componente, y ese proxy
falla con `Unknown binary 'rust-analyzer' in official toolchain`. El guardia
de los tests (`testsupport.RequireRustAnalyzer`) ejecuta `--version` y se
salta la prueba si no responde; un guardia que sólo busque el nombre da por
instalado el analizador en cualquier máquina con rustup y convierte un
`SKIP` en un `FAIL`. CI instala el componente: saltarse la suite entera
escondería el camino Rust en vez de verificarlo.
