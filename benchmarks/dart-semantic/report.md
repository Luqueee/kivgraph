# Exactitud semántica Dart

Auditoría de LUQUE-2201 sobre los fixtures de `testdata/dart`, con el método
de LUQUE-1816: verdad de referencia escrita a mano leyendo los fuentes.
Se regenera con `go run ./benchmarks/dart-semantic`.

Resultado: `DART_SEMANTIC_PASS_WITH_LIMITS` -- 0 falsas exactas, 0 relaciones esperadas ausentes
sobre 27 esperadas, y 6/6 fallos declarados. Los hallazgos están abajo.

## Fixtures

- `testdata/dart/basic`
- `testdata/dart/advanced`

## Totales

- ocurrencias de arista exacta entre símbolos: 32
- aristas esperadas: 27
- true positives: 27
- false negatives: 0
- false exact edges: 0
- símbolos publicados: 37
- no resueltas declaradas: 6/6 esperadas, 27 filas publicadas
- aristas de directiva sin `evidence_key`: 4
- referencias cuyo origen es el módulo del archivo y no una declaración: 0
- aristas con los dos extremos en el mismo símbolo: 0
- declaraciones cuyo rango cubre sólo su nombre: 5

## Casos

| Caso | Esperadas | TP | FN | Falsas exactas | Exactas | Símbolos | No resueltas |
| --- | --- | --- | --- | --- | --- | --- | --- |
| basic | 8 | 8 | 0 | 0 | 9 | 12 | 1/1 |
| advanced | 19 | 19 | 0 | 0 | 23 | 25 | 5/5 |

## No resueltas observadas

Cada fila es `motivo archivo clase-del-objetivo` con su número de
ocurrencias. Están todas, no sólo las esperadas: un hecho perdido en
silencio es el defecto que esta auditoría existe para no repetir.

### basic

- DART_TARGET_NOT_INDEXED lib/models.dart CLASS x4
- DART_TARGET_NOT_INDEXED lib/models.dart TOP_LEVEL_VARIABLE x1

### advanced

- DART_TARGET_NOT_INDEXED lib/conditional.dart PREFIX x3
- DART_TARGET_NOT_INDEXED lib/language_features.dart CLASS x4
- DART_TARGET_NOT_INDEXED lib/language_features.dart METHOD x1
- DART_TARGET_NOT_INDEXED lib/language_features.dart PARAMETER x3
- DART_TARGET_NOT_INDEXED lib/language_features.dart TYPE_PARAMETER x8
- DART_TARGET_NOT_INDEXED lib/library.dart CLASS x1
- DART_TARGET_NOT_INDEXED lib/models.dart CLASS x1
- DART_TARGET_NOT_INDEXED lib/part.dart CLASS x1

## Hallazgos

Cada uno nombra el mecanismo que lo produce y el número de este artefacto
que lo mide. Los que están corregidos se describen con el defecto que
tenían, para que la cifra de esta pasada no se lea sin su historia; los que
no, dicen por qué no se tocaron.

- El origen de una referencia es ahora la declaración que la contiene, no el símbolo de módulo del archivo. `initialize` anuncia `hierarchicalDocumentSymbolSupport`, así que `textDocument/documentSymbol` responde `DocumentSymbol` con hijos y con el rango del cuerpo; antes respondía `SymbolInformation` planos cuyo `location.range` cubre sólo el identificador, `enclosing` no encontraba ninguna declaración que contuviera la ocurrencia y caía al módulo. Publicaba `EXTENDS models.dart -> Vehicle` para `class ElectricVehicle extends Vehicle`: una arista `EXACT` con el origen equivocado. Lo miden `edges_sourced_at_module` y `symbols_spanning_only_their_name`.
- Una declaración no se referencia a sí misma. La guarda comparaba desplazamientos, y el elemento de una directiva `library` vive en el desplazamiento 0 mientras la región está sobre el nombre, así que cuatro bucles pasaban como exactos. Ahora compara identidades; lo mide `self_reference_edges`.
- Un cuerpo con flecha no es una asignación: `String asText() => value.toString()` publicaba `ASSIGNS_FUNCTION` porque cualquier `=` del prefijo valía. Ahora se exige un `=` que no forme `=>`, `==` ni un operador compuesto, y un `=>` seguido de la expresión completa cuenta como retorno.
- Un `enum`, un `mixin` y un `extension type` usados como tipo dan `TYPE_USES`. El Analysis Server 1.40.1 manda `kind: "UNKNOWN"` para esos objetivos, así que la clase no la puede decidir el kind y la decide la posición: en `describe(VehicleKind kind)` la ocurrencia anota el nombre que la sigue. La referencia ya venía resuelta; esto sólo elige la relación, que es lo que hace cada rama de `dartReferenceKind`.
- El campo de representación de un `extension type` se publica. Ninguna de las dos fuentes de declaraciones lo lista y el Analysis Server resuelve su uso a un `PARAMETER`, así que todo uso apuntaba fuera del grafo aunque el campo es nameable como `id.value`; `appendExtensionTypeRepresentations` lo lee de la cabecera de la propia declaración.
- Un `extension type` ya no compite con su archivo por la identidad de módulo. El LSP lo reporta como `SymbolKind.Namespace` (3) y `dartKind` mapeaba 2, 3 y 4 a `module`; `NormalizeSemantic` indexa `moduleKeys` por ese kind y gana el último del orden por `ID`, así que en un archivo cuya ruta ordene después de `module` la declaración le quitaba al archivo su identidad. Reproducido en un paquete temporal con `src/feature.dart` -- una biblioteca con `part 'piece.dart';` y un `extension type UserId(int value)` --, donde la arista publicada era `PART_OF src.piece -> UserId`; con el mapeo corregido apunta al módulo del archivo.
- Una declaración observada por las dos fuentes se publicaba dos veces. Una fila del outline del analizador sin localización de elemento caía en el inicio de la declaración mientras la del LSP caía en el identificador, así que la deduplicación por posición no las unía; ahora la fila sin localización resuelve el desplazamiento de su propio nombre, y las filas que comparten identidad se colapsan.
- Las aristas de directiva (`IMPORTS_SYMBOL`, `REEXPORTS`, `PART_OF`) se publican sin `evidence_key` aunque el productor observó su posición: `facts.SemanticImport` y `facts.SemanticPart` llevan `Start` y `StartLine`, y `NormalizeSemantic` los descarta al construir la arista (`internal/facts/semantic.go:289` y `:362`). No se arregló aquí: darles un extremo obliga a llevar el fin en el payload, que es la versión que comparten los cinco lenguajes. Lo mide `directive_edges_without_evidence`.
- Una relación `part` se observa desde sus dos extremos -- `part 'piece.dart';` y `part of 'feature.dart';` -- y cada directiva es evidencia genuina, así que el payload lleva dos filas idénticas y `NormalizeSemantic` construye dos aristas iguales. Queda nombrado, sin medición propia todavía.
- Una comparación dentro de un paréntesis se clasifica como `PASSES_AS_CALLBACK`: `if (other == handler)` tiene un `(` en el prefijo y un `)` en el sufijo, que es la firma de un argumento. Ningún fixture lo ejercita y distinguirlo pide saber si el paréntesis abre una llamada, así que se declara y no se arregla a ciegas.
- Las `32` aristas exactas publicadas caen en `27` identidades: la identidad de una arista es `clase nombre -> nombre`, así que dos relaciones de la misma clase entre homónimos -- el `OVERRIDES drive -> drive` que cubre a la vez la superclase y la interfaz -- colapsan en una fila. No hay ninguna identidad observada que la verdad no espere.

## Limitaciones

- El corpus son dos paquetes pub de un solo repositorio: prueba los contratos, no la escala ni el camino cross-repository.
- La medición depende del SDK que respalda el `dart` del PATH: el Analysis Server viaja con él, y esta pasada usó el 1.40.1 del Dart SDK 3.13.1.
- La identidad de una arista es `clase nombre -> nombre`, así que dos relaciones de la misma clase entre homónimos colapsan en una fila: `OVERRIDES drive -> drive` cubre a la vez la superclase y la interfaz de `testdata/dart/basic/lib/models.dart:7`.
- Un objetivo que el Analysis Server resuelve fuera del conjunto publicado -- el SDK, un parámetro, un parámetro de tipo, un prefijo de import -- no es una arista: se retiene como `UNRESOLVED` con motivo `DART_TARGET_NOT_INDEXED` (`internal/dartloader/loader.go:635-648`).

## Gate

```text
DART_SEMANTIC_PASS_WITH_LIMITS
```
