# Ninguna tool afirma una ausencia que no ha comprobado

Este arnés no mide si las aristas están bien: eso lo miden los cuatro arneses
semánticos. Mide **qué dice una tool cuando su respuesta está vacía**, que es
la clase de defecto que ninguna medición veía y que sólo aparece usando el
binario.

Dos lenguajes en un solo corpus, y eso es el diseño: el invariante es sobre la
**forma de un fallo** -- una fila sin archivo--, no sobre un cargador. Cada
brazo llega a ella por un motivo distinto, y el corpus compartido permite la
comprobación que ninguno haría solo: que un punto ciego de Rust no acote una
respuesta de Go, ni al revés.

|dato|valor|
|---|---|
|comando|`go run ./benchmarks/tool-honesty --kivgraph <binary>`|
|fecha|2026-08-22|
|plataforma|darwin/arm64, go1.26.4|
|toolchain Rust|rustc 1.96.1 (31fca3adb 2026-06-26)|
|brazos|go, rust|
|corpus|go-blinded, go-pure, rust-blinded, rust-pure|
|símbolos|10|
|no resueltas|2 (PACKAGE_NOT_BUILDABLE=1, WORKSPACE_NOT_LOADED=1)|
|ámbitos ilegibles|2|
|ámbitos de `go`|1|
|ámbitos de `rust`|1|
|comprobaciones|18, 18 pasan, 0 saltadas|

## Comprobaciones

|brazo|tool|pregunta|filas|veredicto|estado|
|---|---|---|---|---|---|
|`go`|`find_symbol`|a declaration lookup that finds nothing while a package is unreadable|0|`LOWER_BOUND`|ok|
|`go`|`find_symbol`|the same lookup narrowed to the repository that can answer it|0|`COMPLETE`|ok|
|`go`|`find_symbol`|a declaration lookup that finds nothing in a corpus that can answer|0|`COMPLETE`|ok|
|`go`|`find_references`|who calls a symbol nobody calls, in a repository with nothing hidden|0|`COMPLETE`|ok|
|`go`|`find_references`|who calls a symbol in a repository the index could not fully read|0|`LOWER_BOUND`|ok|
|`go`|`trace_dependencies`|what a symbol reaches, outward, with nothing hidden|0|`COMPLETE`|ok|
|`go`|`trace_dependencies`|what a symbol reaches when its repository holds an unreadable package|0|`LOWER_BOUND`|ok|
|`go`|`get_blast_radius`|what breaks if I change this, with nothing hidden|0|`COMPLETE`|ok|
|`go`|`find_cross_repo_consumers`|who uses this from another repository|0|`LOWER_BOUND`|ok|
|`go`|`get_file_outline`|what is declared under a path the index read whole|3|`-`|ok|
|`go`|`get_file_outline`|what is declared under a path whose repository hides a package|1|`LOWER_BOUND`|ok|
|`go`|`get_symbol`|a symbol that is not there, asked of the reader|0|`rechaza`|ok|
|`go`|`get_source`|source for a symbol that is not there|0|`rechaza`|ok|
|`rust`|`find_references`|who calls a Rust symbol nobody calls, with nothing hidden|0|`COMPLETE`|ok|
|`rust`|`find_references`|who calls a Rust symbol whose repository holds a workspace that failed to load|0|`LOWER_BOUND`|ok|
|`rust`|`find_symbol`|a Rust declaration that only the unreadable workspace has|0|`LOWER_BOUND`|ok|
|`rust`|`find_references`|a Go answer is not bounded by a Rust blind spot|0|`COMPLETE`|ok|
|`rust`|`find_references`|a Rust answer is not bounded by a Go blind spot|1|`COMPLETE`|ok|

Cada fila dice qué se pierde si deja de cumplirse:

- **a declaration lookup that finds nothing while a package is unreadable** (`go`, `find_symbol`)
- **the same lookup narrowed to the repository that can answer it** (`go`, `find_symbol`)
- **a declaration lookup that finds nothing in a corpus that can answer** (`go`, `find_symbol`)
- **who calls a symbol nobody calls, in a repository with nothing hidden** (`go`, `find_references`)
- **who calls a symbol in a repository the index could not fully read** (`go`, `find_references`)
- **what a symbol reaches, outward, with nothing hidden** (`go`, `trace_dependencies`)
- **what a symbol reaches when its repository holds an unreadable package** (`go`, `trace_dependencies`)
- **what breaks if I change this, with nothing hidden** (`go`, `get_blast_radius`)
- **who uses this from another repository** (`go`, `find_cross_repo_consumers`)
- **what is declared under a path the index read whole** (`go`, `get_file_outline`)
- **what is declared under a path whose repository hides a package** (`go`, `get_file_outline`)
- **a symbol that is not there, asked of the reader** (`go`, `get_symbol`)
- **source for a symbol that is not there** (`go`, `get_source`)
- **who calls a Rust symbol nobody calls, with nothing hidden** (`rust`, `find_references`)
- **who calls a Rust symbol whose repository holds a workspace that failed to load** (`rust`, `find_references`)
- **a Rust declaration that only the unreadable workspace has** (`rust`, `find_symbol`)
- **a Go answer is not bounded by a Rust blind spot** (`rust`, `find_references`)
- **a Rust answer is not bounded by a Go blind spot** (`rust`, `find_references`)

## Hallazgos

- `find_references` answered an empty list with «the edges are type-checked, so this is an absence rather than a miss» while the index held an unresolved row naming that very symbol. The row was there, with its file and its line, and the tool did not read it: `addReferenceCoverage` counted edges whose confidence is Unresolved, which is a different fact. It is the check this harness exists to keep.
- Six tools answer a question whose empty or partial answer reads as proof, and until this phase two published a verdict. The other four -- `trace_dependencies`, `find_cross_repo_consumers`, `find_symbol`, `get_file_outline` -- said nothing, while `internal/mcp/instructions.go` told every agent to «read confidence and completeness before treating an empty or partial answer as proof of absence».
- An outward question is bounded by a different set of failures than an inward one. «Who calls this» is bounded by failures that asked for the name; «what does this reach» by failures the symbol itself made. Asking the naming question for a traversal would have missed every one of them, so `UnresolvedFromSymbol` exists for that direction.
- The scope of the check follows the scope of the question, and that is what keeps the verdict from becoming a constant. A search of the whole graph is bounded by every unreadable package in it; one narrowed to a repository only by that repository's. `find_cross_repo_consumers` is deliberately the other way round: a package unreadable anywhere can hide the consumer it is asked about.
- `get_symbol` and `get_source` refuse a symbol they cannot find instead of answering an empty list, so they claim no absence and need no verdict. The two checks here pin that shape: if either ever answers empty, it needs a verdict like the rest.
- Measured, not assumed: `"completeness":{"verdict":"COMPLETE"}` is 10 tokens under `cl100k_base` -- 16 % of a one-row `find_symbol` answer and 50 % of an empty one. So a lookup spends it where the answer could be mistaken for a proof (empty, partial) and on every lower bound, while the four relational tools always carry it: for «who calls this» and «what breaks if I change this», COMPLETE on a non-empty answer is the claim being bought.
- The invariant is about the shape of a failure, not about a language. A scope is a recorded failure with no file (`blindspots.go:88` filters on exactly that), and the two arms reach it through different reasons: Go excludes a package by build tag (`PACKAGE_NOT_BUILDABLE`), Rust names a workspace member that is not there (`WORKSPACE_NOT_LOADED`). Same envelope, same verdict, two loaders.
- The five languages split into two honest shapes when a whole repository fails, and nothing documented which did which. Go and Rust record a fileless row and continue, so the answers about that repository become lower bounds. TypeScript, Python and Dart return an error instead -- `semantic.go:83`, `full.go:1388` -- and the pass aborts without publishing, so no answer is served at all. Both refuse to claim an absence; only the first is measurable from a served graph.
- A verdict must not leak across languages, and one corpus is what makes that checkable. A Go answer scoped to a Go repository stays COMPLETE while a Rust workspace of the same graph is unreadable, and the mirror holds too. Leaking would make the verdict a constant on every polyglot monorepo, which is the only kind this product is for.
- Isolating HOME breaks the Rust toolchain, and it broke this harness first. `RUSTUP_HOME` defaults to `$HOME/.rustup`, so a temporary HOME leaves rustup with no toolchains, `rustc` stops answering, and every Rust workspace fails to load -- which would have made the clean arm indistinguishable from the blinded one and passed green having measured nothing. `indexEnvironment` moves HOME and leaves the toolchain locations alone.
- Measured while choosing the Rust fixture: an unresolvable *dependency* is not a blind spot. rust-analyzer loads the workspace anyway and degrades, so a crate depending on a package that cannot exist produced eight symbols and zero failures. A workspace naming a member directory that does not exist is what Cargo cannot resolve, and it is deterministic with or without network access.

## Limitaciones

- The corpus is two Go and two Rust repositories: it proves the invariant holds across the two languages that record a fileless failure and continue, and that the verdict is neither a constant nor leaky between them. TypeScript, Python and Dart abort the pass instead, so no served graph of theirs can be measured this way and none is measured here.
- Two of the three Rust scope reasons are unexercised. `WORKSPACE_NOT_LOADED` is the one this fixture produces; `ANALYZER_UNAVAILABLE` needs a missing analyzer and `TARGET_NOT_BUILDABLE` was not reachable by fixture at all -- an excluded crate is discovered as its own workspace and loads, so `collectEmptyCrates` never fires here.
- `MACRO_EXPANSION_DISABLED` is emitted for every Rust repository whenever `proc_macros` is off, which makes every answer about that repository a lower bound. It is literally true -- the index is incomplete by configuration and says so -- but it means the Rust verdict carries no information under that setting. The default is on, and this corpus runs with the default.
- This harness reads the envelope, not the rows: it prices what a tool claims about its own completeness, and says nothing about whether the edges are right. That is what `benchmarks/go-semantic`, `rust-semantic`, `python-semantic` and `dart-semantic` measure.
- It needs a binary built with the `ladybug` tag, and git on the PATH; the Rust arm also needs the pinned analyzer and a `rustc` that answers. The binary and git are required, and the Rust arm is skipped with its reason recorded rather than faked or failed.

## Gate

```text
TOOL_HONESTY_PASS_WITH_LIMITS
```
