# ADR 0037: Rust distingue nombrar una función de llamarla

- **Estado:** aceptada
- **Fecha:** 2026-08-12
- **Amplía:** ADR 0033 (motor SCIP de Rust)

## Contexto

El camino Rust publicaba `CALLS_DIRECT` cuando una función aparecía como
destino de una llamada y `REFERENCES` en cualquier otra posición. Eso metía en
un mismo saco tres relaciones que Go ya distinguía desde el primer día:

```rust
apply(double, value)      // se entrega la función
let operation = double;   // se ata a un nombre
fn pick() -> fn(i32) -> i32 { double }   // se devuelve
```

Las tres son código idiomático -la mitad del Rust asíncrono y de los iteradores
mueve funciones sin llamarlas- y las tres llegaban al grafo como el mismo hecho
genérico. Un consumidor que preguntara quién usa `double` recibía la respuesta
correcta; uno que preguntara quién *la entrega a otra función*, no podía
preguntarlo.

El vocabulario canónico ya tenía los tres nombres: `PASSES_AS_CALLBACK`,
`ASSIGNS_FUNCTION` y `RETURNS_FUNCTION`, emitidos por el cargador de Go. No
hacía falta inventar nada, sino producirlos también desde Rust.

## Decisión

**La gramática decide la clase, el analizador decide el destino**, que es la
división que ADR 0033 ya fijó para `CALLS_DIRECT`. `Source.Reference` clasifica
una ocurrencia por la forma que la rodea:

| Forma | Clase | Arista |
| --- | --- | --- |
| argumento de una `call_expression` | `callback` | `PASSES_AS_CALLBACK` |
| valor de `let`, `const`, `static` o campo de un literal | `assign` | `ASSIGNS_FUNCTION` |
| `return`, o expresión final del cuerpo | `return` | `RETURNS_FUNCTION` |

El ascenso por la expresión atraviesa las formas que no cambian lo que se
nombra -un camino, un préstamo, un paréntesis, un literal de array o tupla- y
se detiene en la primera que decide, de modo que `takes(&[f])` es un argumento
y no una asignación. **Un acceso a campo no es transparente**: devolver
`objeto.campo` devuelve el campo, no el objeto que lo contiene.

**Una clase de posición de valor exige un destino invocable indexado en la
misma pasada.** La forma sola no basta: `takes_limit(LIMIT)` es un argumento
que no es un callback. El cargador conserva el `Kind` publicado del destino
cuando lo resolvió contra sus propias definiciones y solo mantiene la clase si
es `function`, `method`, `static_method` o `trait_method`. Un destino resuelto
por el registro de crates pertenece a otro repositorio y llega sin `Kind`: la
clase degrada a `REFERENCES` antes que afirmar lo que esta pasada nunca leyó.

`PASSES_AS_CALLBACK` estrena la procedencia `RUST_SYNTAX_CALLBACK`, código
`19`, espejo exacto de `GO_AST_CALLBACK`. Atar y devolver no estrenan
procedencia: la clase viaja en el tipo de arista y el destino lo resolvió el
analizador, que es lo que `RUST_ANALYZER_USE` ya declara, y es también el
reparto que hace Go.

## Alternativas

- **Una procedencia por clase** (`RUST_SYNTAX_ASSIGN`, `RUST_SYNTAX_RETURN`).
  Tres códigos nuevos en un catálogo congelado para distinguir algo que el
  `EdgeKind` ya distingue. Go no lo hace y aquí tampoco.
- **Marcar la clase sin comprobar el destino.** Habría convertido cualquier
  constante en argumento en un callback: aristas exactas falsas, que es
  precisamente lo que el contrato prohíbe.
- **Publicar la closure como símbolo.** Una closure no tiene identidad durable
  en SCIP -es un `local N`, un contador por documento-, así que no puede ser
  extremo de una arista. Devolver una closure anónima sigue sin dejar rastro, y
  es una limitación declarada.

## Consecuencias

- El schema persistente gana el código de procedencia `19`. Es un `append` al
  final de una lista congelada: ninguna generación existente cambia de
  significado, pero una generación escrita por este binario no la entiende un
  binario anterior. `canonical_integrity.go` y su guardia sin tag lo recogen.
- La auditoría de exactitud gana un cuarto fixture, `testdata/rust/function-values`,
  con sus dos casos negativos. Mide 8 de 8 aristas exactas, 0 falsas, y el total
  del corpus pasa de 22 a 30 aristas esperadas, todas observadas.
- `internal/rustloader/source_test.go` fija las once formas sin necesitar el
  analizador: es el único test del camino Rust que corre en una máquina sin
  `rust-analyzer` instalado.

## Limitaciones

- Una closure -anónima por definición- no es extremo de ninguna arista.
- Un destino de otro repositorio no lleva `Kind`, así que una función entregada
  a través de la frontera de repositorio se publica como `REFERENCES`.
- Un puntero a función que viaja dentro de una estructura de datos compleja
  -el elemento de un `HashMap`, el campo de una `struct` construida en otra
  función- se atribuye donde la gramática lo ve, no donde acaba invocándose.
