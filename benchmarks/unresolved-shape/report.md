# De qué son los no resueltos

`LUQUE-2007`. El índice de `kena` publica `1.969` referencias Rust no resueltas y
ningún conjunto de preguntas había preguntado nunca **qué son**. Un contador
agregado no distingue una limitación declarada de un defecto escondido, y ésa era
toda la información disponible.

La pregunta se contesta sin leer código, porque el contrato de la raíz lo
garantiza: cada `UNRESOLVED` conserva su motivo, su repositorio y su lenguaje, y
cuando hay una ocurrencia concreta conserva su archivo y su posición. Esto lee
exactamente esos campos del grafo canónico publicado y los agrupa.

Métricas crudas en `results.json`.

## Provenance

|dato|valor|
|---|---|
|fecha|2026-08-21|
|commit|`9cad2d3`|
|corpus|`kena`, 37 repositorios, con `cargo` en el `PATH`|
|máquina|`Mac17,2` (Apple M5), macOS `26.6`|

## El panorama

|lenguaje|no resueltos|símbolos publicados|
|---|---|---|
|`go`|`6.059`|`11.731`|
|`typescript`|`5.991`|`108.730`|
|`rust`|`1.969`|`3.063`|

Contra **referencias** -- que es el denominador honesto, porque un no resuelto es
una referencia que no resolvió-- Rust es el más alto: `14,9 %` de `13.223`, contra
`8,4 %` de Go y `2,2 %` de TypeScript.

## La respuesta: ninguna es un defecto de resolución

|grupo|cuenta|parte|qué es|
|---|---|---|---|
|`CRATE_PROVIDER_NOT_FOUND`|`1.857`|`94,3 %`|el sysroot y las dependencias externas|
|`DEFINITION_NOT_INDEXED`|`112`|`5,7 %`|definiciones sin ocurrencia en fuente|
|**llamadas del workspace que fallaron al resolver**|**`0`**|`0 %`|--|

El `94,3 %` son `alloc::boxed::Box`, `alloc::macros::vec`,
`alloc::collections::vec_deque::VecDeque` y compañía: la biblioteca estándar y lo
que el caché local no trajo. El indexado de Rust es hermético por decisión, así
que eso es una **limitación declarada** funcionando como se documentó.

## El `5,7 %`, etiqueta por etiqueta

Las `112` ocurrencias tienen `112` etiquetas distintas, así que se pudieron leer
una a una:

|forma|cuenta|veredicto|
|---|---|---|
|bloques `impl` -- `impl::Type` y `impl::Type::Trait`|`56`|**hallazgo**, abajo|
|miembros generados por `derive` -- `Default::default`, `Clone::clone`, `PartialEq<Self>::eq`|`53`|ausencia inherente: no hay fuente que indexar|
|dos campos de tupla `::0` y un nombre de test|`3`|cola|

Los `53` son código que el compilador escribe: no existe ocurrencia en fuente, y
un no resuelto es el registro honesto de haber visto la referencia sin poder
resolverla.

## El hallazgo: una rama que nunca se ejecuta

Los `56` restantes son bloques `impl`, inherentes y de trait. **El cargador nunca
los publica como símbolo.** La evidencia es directa:

```
find_symbol { name: "impl", kind: "implementation", repo: "api-music-nodo" }
  -> total: 0
```

Y sin embargo `internal/rustloader/kinds.go` tiene código para ellos:
`PublishedKind` devuelve `"implementation"` cuando `isImplementationBlock`
acierta, y `PublishedName` renderiza `impl X for Y` porque «un bloque de
implementación no tiene nombre propio». Ninguna de las dos ramas se ejecuta jamás
en este corpus.

Los **miembros** de esos mismos bloques sí están indexados -- `get_file_outline`
sobre `src/error.rs` los lista, `error::impl::ApiError::with_context_header@174-177`
entre ellos. Así que no es que el cargador ignore los `impl`: indexa su contenido
y no su cabecera, y cada referencia que `rust-analyzer` emite hacia esa cabecera
queda **permanentemente sin resolver**. `56` en este corpus.

Es la misma enfermedad que el camino incremental retirado en el ADR 0057: código
que existe para un caso que no ocurre. Aquí es más pequeño y tiene una segunda
mitad -- referencias que no pueden resolver nunca-- que inflan una métrica que
alguien lee para decidir si confiar en el grafo.

## Reproducir

```bash
export PATH="$HOME/.cargo/bin:$PATH"   # sin cargo el corpus sale sin Rust
kivgraph index --full --json
go run -tags ladybug ./benchmarks/unresolved-shape \
  -database "$HOME/.local/state/kivgraph/generations/$(cat "$HOME/.local/state/kivgraph/CURRENT")/graph.db" \
  -language rust -examples 200
```

## Limitaciones

- Un corpus, una máquina, un toolchain. Otro sysroot, o una dependencia que el
  caché sí traiga, mueve el `94,3 %`.
- La clasificación de las `112` es **por forma del símbolo**, leída etiqueta a
  etiqueta. Las formas están citadas para que quien lea pueda discrepar de la
  lectura.
- **Go y TypeScript se cuentan y no se clasifican.** `6.059` y `5.991` no
  resueltos siguen sin explicación, y el mismo método los explicaría. Este
  informe no lo hace y no debe leerse como si dijera algo sobre ellos.
- El sondeo lee el grafo canónico, así que sus símbolos por lenguaje son los del
  grafo y no coinciden con `go_definitions` del evento de índice: uno cuenta
  símbolos publicados y el otro definiciones que el cargador vio.
