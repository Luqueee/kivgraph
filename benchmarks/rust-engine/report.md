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
| analyzer.provider | 998.0 | documents=1 |
| index.cold | 1140.5 | workspaces=2 |
| index.warm | 40.4 | cache_hits=2 misses=0 |

El analizador externo es el término dominante: la normalización, la
atribución con Tree-sitter y el merge ocurren sobre un índice ya construido.

## Limitaciones

- El corpus son tres crates: mide el reparto del coste, no la escala.
- El caché del toolchain y del sysroot están calientes; una máquina fría paga más.
- Las cifras dependen de la versión de rust-analyzer instalada.
