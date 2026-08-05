# Precisión semántica Go

Medición de LUQUE-0813 sobre los fixtures de LUQUE-0811 y LUQUE-0812.
Se regenera con `go run ./benchmarks/go-semantic`.

## Fixtures

- `testdata/go/cross-repository`
- `testdata/go/cross-repository-negative`

## Totales

- true positives: 16
- false positives: 0
- false negatives: 0
- precision: 1.0000
- recall: 1.0000
- false exact edges: 0
- unresolved correctly classified: 2/2

## Casos

| Caso | Aristas esperadas | TP | FP | FN | Precisión | Recall | No resueltas correctas |
| --- | --- | --- | --- | --- | --- | --- | --- |
| consumer-a | 8 | 8 | 0 | 0 | 1.0000 | 1.0000 | 0/0 |
| consumer-b | 3 | 3 | 0 | 0 | 1.0000 | 1.0000 | 0/0 |
| consumer-negative | 5 | 5 | 0 | 0 | 1.0000 | 1.0000 | 2/2 |

## Gate

```text
GO_SEMANTIC_PASS
```
