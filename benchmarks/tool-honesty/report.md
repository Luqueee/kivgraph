# Ninguna tool afirma una ausencia que no ha comprobado

Este arnés no mide si las aristas están bien: eso lo miden los cuatro arneses
semánticos. Mide **qué dice una tool cuando su respuesta está vacía**, que es
la clase de defecto que ninguna medición veía y que sólo aparece usando el
binario.

|dato|valor|
|---|---|
|comando|`go run ./benchmarks/tool-honesty --kivgraph <binary>`|
|fecha|2026-08-22|
|plataforma|darwin/arm64, go1.26.4|
|corpus|blinded, pure|
|símbolos|4|
|no resueltas|1|
|ámbitos ilegibles|1|
|comprobaciones|13, 13 pasan|

## Comprobaciones

|tool|pregunta|filas|veredicto|estado|
|---|---|---|---|---|
|`find_symbol`|a declaration lookup that finds nothing while a package is unreadable|0|`LOWER_BOUND`|ok|
|`find_symbol`|the same lookup narrowed to the repository that can answer it|0|`COMPLETE`|ok|
|`find_symbol`|a declaration lookup that finds nothing in a corpus that can answer|0|`COMPLETE`|ok|
|`find_references`|who calls a symbol nobody calls, in a repository with nothing hidden|0|`COMPLETE`|ok|
|`find_references`|who calls a symbol in a repository the index could not fully read|0|`LOWER_BOUND`|ok|
|`trace_dependencies`|what a symbol reaches, outward, with nothing hidden|0|`COMPLETE`|ok|
|`trace_dependencies`|what a symbol reaches when its repository holds an unreadable package|0|`LOWER_BOUND`|ok|
|`get_blast_radius`|what breaks if I change this, with nothing hidden|0|`COMPLETE`|ok|
|`find_cross_repo_consumers`|who uses this from another repository|0|`LOWER_BOUND`|ok|
|`get_file_outline`|what is declared under a path the index read whole|3|`-`|ok|
|`get_file_outline`|what is declared under a path whose repository hides a package|1|`LOWER_BOUND`|ok|
|`get_symbol`|a symbol that is not there, asked of the reader|0|`rechaza`|ok|
|`get_source`|source for a symbol that is not there|0|`rechaza`|ok|

Cada fila dice qué se pierde si deja de cumplirse:

- **a declaration lookup that finds nothing while a package is unreadable** (`find_symbol`)
- **the same lookup narrowed to the repository that can answer it** (`find_symbol`)
- **a declaration lookup that finds nothing in a corpus that can answer** (`find_symbol`)
- **who calls a symbol nobody calls, in a repository with nothing hidden** (`find_references`)
- **who calls a symbol in a repository the index could not fully read** (`find_references`)
- **what a symbol reaches, outward, with nothing hidden** (`trace_dependencies`)
- **what a symbol reaches when its repository holds an unreadable package** (`trace_dependencies`)
- **what breaks if I change this, with nothing hidden** (`get_blast_radius`)
- **who uses this from another repository** (`find_cross_repo_consumers`)
- **what is declared under a path the index read whole** (`get_file_outline`)
- **what is declared under a path whose repository hides a package** (`get_file_outline`)
- **a symbol that is not there, asked of the reader** (`get_symbol`)
- **source for a symbol that is not there** (`get_source`)

## Hallazgos

- `find_references` answered an empty list with «the edges are type-checked, so this is an absence rather than a miss» while the index held an unresolved row naming that very symbol. The row was there, with its file and its line, and the tool did not read it: `addReferenceCoverage` counted edges whose confidence is Unresolved, which is a different fact. It is the check this harness exists to keep.
- Six tools answer a question whose empty or partial answer reads as proof, and until this phase two published a verdict. The other four -- `trace_dependencies`, `find_cross_repo_consumers`, `find_symbol`, `get_file_outline` -- said nothing, while `internal/mcp/instructions.go` told every agent to «read confidence and completeness before treating an empty or partial answer as proof of absence».
- An outward question is bounded by a different set of failures than an inward one. «Who calls this» is bounded by failures that asked for the name; «what does this reach» by failures the symbol itself made. Asking the naming question for a traversal would have missed every one of them, so `UnresolvedFromSymbol` exists for that direction.
- The scope of the check follows the scope of the question, and that is what keeps the verdict from becoming a constant. A search of the whole graph is bounded by every unreadable package in it; one narrowed to a repository only by that repository's. `find_cross_repo_consumers` is deliberately the other way round: a package unreadable anywhere can hide the consumer it is asked about.
- `get_symbol` and `get_source` refuse a symbol they cannot find instead of answering an empty list, so they claim no absence and need no verdict. The two checks here pin that shape: if either ever answers empty, it needs a verdict like the rest.
- Measured, not assumed: `"completeness":{"verdict":"COMPLETE"}` is 10 tokens under `cl100k_base` -- 16 % of a one-row `find_symbol` answer and 50 % of an empty one. So a lookup spends it where the answer could be mistaken for a proof (empty, partial) and on every lower bound, while the four relational tools always carry it: for «who calls this» and «what breaks if I change this», COMPLETE on a non-empty answer is the claim being bought.

## Limitaciones

- The corpus is two Go repositories: it proves the invariant holds and that it is not a constant, not that it holds for every language and every shape of failure.
- The blind spot is one kind -- a package excluded by a build tag, recorded as `PACKAGE_NOT_BUILDABLE` with no file. A module that fails to load records `MODULE_NOT_LOADED` the same way, and both were observed while building this fixture; the other reasons are not exercised here.
- This harness reads the envelope, not the rows: it prices what a tool claims about its own completeness, and says nothing about whether the edges are right. That is what `benchmarks/go-semantic`, `rust-semantic`, `python-semantic` and `dart-semantic` measure.
- It needs a binary built with the `ladybug` tag, and git on the PATH. Neither is faked when absent: the run fails with the reason.

## Gate

```text
TOOL_HONESTY_PASS_WITH_LIMITS
```
