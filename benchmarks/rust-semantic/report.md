# Exactitud semántica Rust

Auditoría de LUQUE-1816 sobre los fixtures de LUQUE-1813.
Se regenera con `go run ./benchmarks/rust-semantic`.

## Fixtures

- `testdata/rust/workspace`
- `testdata/rust/cross-repository`

## Totales

- ocurrencias de arista exacta entre símbolos: 32
- aristas esperadas: 30
- true positives: 30
- false negatives: 0
- false exact edges: 0
- símbolos publicados: 37
- no resueltas declaradas: 5/5

## Casos

| Caso | Esperadas | TP | FN | Falsas exactas | Símbolos | No resueltas |
| --- | --- | --- | --- | --- | --- | --- |
| workspace | 16 | 16 | 0 | 0 | 18 | 2/2 |
| cross-repository | 6 | 6 | 0 | 0 | 6 | 1/1 |
| cross-repository-negative | 0 | 0 | 0 | 0 | 2 | 1/1 |
| function-values | 8 | 8 | 0 | 0 | 11 | 1/1 |

## Limitaciones

- El corpus son cuatro fixtures de crates: prueba los contratos, no la escala.
- PASSES_AS_CALLBACK, ASSIGNS_FUNCTION y RETURNS_FUNCTION exigen un destino invocable indexado en la misma pasada; hacia otro repositorio la clase degrada a REFERENCES.
- IMPLEMENTS, EXTENDS y OVERRIDES se derivan de la forma del impl y del bound: relationships viaja vacío.
- La medición depende de la versión de rust-analyzer instalada y de su sysroot.

## Gate

```text
RUST_SEMANTIC_PASS_WITH_LIMITS
```
