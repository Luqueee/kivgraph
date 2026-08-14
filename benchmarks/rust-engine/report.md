# Coste del motor Rust

Medición de LUQUE-1816. Se regenera con `go run ./benchmarks/rust-engine`.

## Corpus

- `testdata/rust/cross-repository`: 2 repositorios, 2 crates, 6 símbolos, 19 aristas.

## Entorno

- darwin/arm64, 10 CPUs, go1.26.4
- `rust-analyzer 1.96.1 (31fca3ad 2026-06-26)`
- `cargo 1.96.1 (356927216 2026-06-26)`

## Medidas

| Medida | ms | Detalle |
| --- | --- | --- |
| analyzer.provider | 1027.3 | documents=1 |
| index.cold.without-stdlib | 1205.2 | workspaces=2 symbols=6 |
| index.warm.without-stdlib | 52.3 | cache_hits=2 misses=0 |
| index.cold.with-stdlib | 15266.5 | workspaces=3 symbols=19533 |
| index.warm.with-stdlib | 789.4 | cache_hits=3 misses=0 |

## Con y sin biblioteca estándar

El sysroot es opt-in (`rust.index_sysroot`). Publicarlo cambia el tamaño del
grafo en un orden de magnitud, así que las dos configuraciones se miden
aparte: un total único no describiría ninguna de las dos.

| Brazo | Proveedor | Workspaces | Símbolos | Aristas | No resueltos | Sin proveedor | Frío ms | Caliente ms | Aciertos/Fallos |
| --- | --- | --- | --- | --- | --- | --- | --- | --- | --- |
| without-stdlib | SYSROOT_NOT_REQUESTED | 2 | 6 | 19 | 1 | 0 | 1205.2 | 52.3 | 2/0 |
| with-stdlib | rust:1.96.1 | 3 | 19533 | 169532 | 6709 | 3 | 15266.5 | 789.4 | 3/0 |

`Sin proveedor` cuenta los usos que el merge no pudo cerrar porque la
biblioteca genera esa implementación con una macro y no existe en ningún
rango de código: se declaran como `PROVIDER_DEFINITION_NOT_INDEXED`, nunca
se publican como arista.

El analizador externo es el término dominante: la normalización, la
atribución con Tree-sitter y el merge ocurren sobre un índice ya construido.

## Limitaciones

- El corpus son tres crates: mide el reparto del coste, no la escala.
- El caché del toolchain y del sysroot están calientes; una máquina fría paga más.
- Las cifras dependen de la versión de rust-analyzer instalada.
- El brazo con sysroot mide la biblioteca del toolchain instalado; otra versión publica otro número de símbolos.
- El sysroot del brazo con biblioteca se indexa en su sitio, sin copiarlo: su coste incluye leer y hashear sus ficheros.
