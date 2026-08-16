# ADR 0033: Motor Rust mediante `rust-analyzer scip`

- **Estado:** aceptada
- **Fecha:** 2026-08-11
- **Revisa:** ADR 0005, ADR 0006

## Contexto

Kivgraph resuelve Go en proceso con `go/packages` y `go/types`, y TypeScript
fuera de proceso con un worker Node sobre el compilador. Rust no tiene ninguna
de las dos cosas: no existe una biblioteca Go que compruebe tipos de Rust, y el
compilador no expone un índice consumible —`-Zsave-analysis` fue retirado—.

La restricción que decide el diseño no es de rendimiento sino de contrato:
`facts.Provenance.Exact()` (`internal/facts/facts.go:76-78`) devuelve `false`
para `TREE_SITTER_SYNTAX`, de modo que ninguna arista respaldada solo por
sintaxis puede ser exacta. Un soporte de Rust construido sobre Tree-sitter
produciría un grafo entero de `CANDIDATE`, que es exactamente lo que el
proyecto se niega a vender como conocimiento.

## Decisión

El motor es **`rust-analyzer scip`, ejecutado como proceso externo, una vez por
workspace Cargo**. La salida es un índice SCIP que Kivgraph decodifica y
normaliza al modelo canónico.

Tree-sitter no aporta identidad ni resolución. Aporta dos cosas que SCIP no
transporta: la clase sintáctica del uso y la visibilidad declarada. Es el mismo
reparto que ya existe en Go, donde `CALLS_DIRECT` lleva procedencia
`GO_AST_CALL` sobre una resolución de `go/types`
(`internal/facts/golang.go:404-431`).

### Qué da el índice, verificado sobre el código de `cli/scip.rs`

| Necesidad de Kivgraph | Campo SCIP | Observación |
| --- | --- | --- |
| Identidad durable | símbolo `rust-analyzer cargo <crate> <versión> <descriptores>` | `moniker_to_symbol()`; la versión es `"."` cuando se desconoce |
| Definición | `Occurrence.symbol_roles` con `Definition` | es el único rol que se emite |
| Span de la declaración | `Occurrence.enclosing_range` | poblado desde `token.definition_body` cuando cae en el mismo archivo |
| Nombre, clase y firma | `SymbolInformation.display_name`, `kind`, `signature_documentation` | poblados en `compute_symbol_info()` |
| Contenedor de un local | `SymbolInformation.enclosing_symbol` | solo se rellena para símbolos locales |
| Destinos externos | `Index.external_symbols` | identidad sin documento ni posición |

### La unidad de análisis es el workspace, no el crate

`rust-analyzer scip` carga el workspace completo y su sysroot en cada
invocación. Una unidad por crate pagaría esa carga tantas veces como crates
tenga el workspace. El techo de concurrencia es bajo por memoria, por la misma
razón por la que lo es el de las cargas Go: cada invocación mantiene un
universo entero.

### La clave estable

`hotsnapshot.StableKeyIdentity` con `Language: "rust"`, `Package` el nombre del
crate, `Module` vacío, `QualifiedName` el camino de descriptores, `Kind` el
**sufijo del descriptor** y `Discriminator` derivado de la firma. El formato
versión 1 no cambia.

`Kind` no usa `SymbolInformation.kind` a propósito: el sufijo viaja dentro de
la propia cadena del símbolo, así que consumidor y proveedor de un crate no
pueden divergir aunque el analizador clasifique distinto un método de rasgo.

### El binario no se empaqueta

`rust-analyzer` es un prerrequisito externo, documentado y comprobado por
`doctor`, exactamente como el runtime Node del worker TypeScript. Empaquetarlo
añadiría catorce megabytes por plataforma y ataría el bundle a una cadencia de
releases diaria que no controlamos.

## Alternativas descartadas

- **Solo Tree-sitter.** Ninguna arista podría ser exacta. Es la opción barata y
  es justo la que contradice el producto.
- **Worker propio sobre los crates `ra_ap_*`.** Daría control fino sobre lo que
  se extrae, pero obliga a distribuir un binario Rust por plataforma y a seguir
  una API que se publica sin promesa de estabilidad. Queda como salida si el
  índice externo se demuestra insuficiente.
- **`rustdoc --output-format json`.** Describe la API pública; no emite
  ocurrencias de referencia, que es la mitad del grafo.
- **`-Zsave-analysis`.** Retirado del compilador.
- **LSP interactivo contra `rust-analyzer`.** Una petición por referencia
  convierte una indexación en decenas de miles de idas y vueltas; el modo
  batch existe precisamente para esto.

## Consecuencias

- Rust entra al grafo con aristas `EXACT_TYPECHECKED` y, entre repositorios,
  `EXACT_PACKAGE_MAPPED`.
- El coste dominante de una pasada con Rust es el propio `rust-analyzer`, no la
  normalización. Los benchmarks lo reportan separado.
- Kivgraph adquiere una dependencia de decodificación protobuf, sea el módulo
  publicado `github.com/scip-code/scip/bindings/go/scip` (Apache-2.0) o unos
  bindings mínimos generados del `.proto` fijado con digest.

### Limitaciones declaradas

- `SymbolInformation.relationships` viaja vacío. `IMPLEMENTS`, `EXTENDS` y
  `OVERRIDES` se derivan de la forma del `impl` y del bound sobre extremos que
  el analizador resolvió; una implementación que la gramática no ve -generada
  por una macro- queda ausente.
- `symbol_roles` solo distingue definición y `syntax_kind` no se rellena: la
  clase de la arista la decide la sintaxis.
- Los símbolos locales (`local N`) son un contador por documento y no entran al
  grafo.
- Bugs conocidos aguas arriba producen símbolos duplicados para impls
  inherentes y para ítems anidados en funciones; el aviso de rust-analyzer se
  conserva como diagnóstico y esas definiciones no se fusionan.

## Riesgos

- **Cadencia del analizador.** Publica a diario y el subcomando `scip` no tiene
  promesa de compatibilidad. Mitigación: suelo de versión verificado sobre el
  binario, no sobre la documentación, y la versión observada forma parte de la
  huella de la caché de hechos.
- **Divergencia de firma entre el checkout local de un crate y la copia del
  registro contra la que compila el consumidor.** Mitigación: la clave
  calculada se comprueba contra el conjunto; si no existe, es un no resuelto y
  no una arista.

## Medición

Sobre el corpus de fixtures (`testdata/rust`, tres crates), con
`rust-analyzer 1.96.1` y `cargo 1.96.1` en `darwin/arm64`:

| Medida | Valor |
| --- | --- |
| Índice completo en frío, dos repositorios | `1331.7 ms` |
| Índice completo en caliente, con caché de hechos | `52.6 ms`, grafo idéntico |
| Solo el analizador, un workspace | `1010.4 ms` |
| Aristas exactas esperadas / encontradas | `13 / 13` |
| `false exact edges` | `0` |
| Fallos declarados / esperados | `3 / 3` |

Los artefactos están en `benchmarks/rust-semantic/` y `benchmarks/rust-engine/`.
El analizador externo es el término dominante del coste, que es exactamente lo
que esta decisión compra: identidad y resolución con tipos sin escribir un
comprobador de Rust.
