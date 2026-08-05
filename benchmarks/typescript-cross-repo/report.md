# Precisión cross-repository TypeScript

Medición de LUQUE-0709 sobre los fixtures de LUQUE-0707 y LUQUE-0708.
Se regenera con `pnpm precision` desde `ts-worker`.

## Fixtures

- `testdata/typescript/cross-repository`
- `testdata/typescript/cross-repository-negative`

## Totales

- true positives: 8
- false positives: 0
- false negatives: 0
- precision: 1.0000
- recall: 1.0000
- false exact edges: 0
- unresolved correctly classified: 4/4
- exact source positions: 7/7

## Casos

| Caso | Aristas esperadas | TP | FP | FN | Precisión | Recall | No resueltas correctas | Posiciones en fuente |
| --- | --- | --- | --- | --- | --- | --- | --- | --- |
| consumer-a | 3 | 3 | 0 | 0 | 1.0000 | 1.0000 | 0/0 | 3/3 |
| consumer-b | 2 | 2 | 0 | 0 | 1.0000 | 1.0000 | 0/0 | 2/2 |
| consumer-negative | 3 | 3 | 0 | 0 | 1.0000 | 1.0000 | 4/4 | 2/2 |

## Gate

```text
TYPESCRIPT_CROSS_REPO_PASS
```
