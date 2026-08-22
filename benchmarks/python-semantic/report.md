# Exactitud semántica Python

Auditoría del camino Python sobre los fixtures de `testdata/python`, en sus dos modos.
Se regenera con `go run ./benchmarks/python-semantic`.

La verdad de referencia está escrita a mano desde los fuentes del fixture, en
`auditCases` de `main.go`, y cada expectativa cita el archivo y la línea de la
que sale. Comparar un índice contra su propia salida anterior no demuestra nada.

El código de salida habla de la medición, nunca del veredicto: es `0` siempre que
la auditoría se ejecutó y escribió sus artefactos. El veredicto es el token del
gate, que va en `stdout` y en los dos archivos. Dos ejecuciones seguidas producen
`results.json` y `report.md` idénticos byte a byte; los dos únicos campos que
dependen del host son las versiones de la sección Entorno.

## Fixtures

- `testdata/python/basic`
- `testdata/python/coverage`

## Entorno

- `python3`: `Python 3.14.5`
- analizador del brazo `exact`: `pyright 1.1.413`

## Totales

`TP` cuenta las relaciones del fuente que el brazo publica con la clase esperada
a cualquier confianza; `TP exactas` el subconjunto que publica como exacta. La
diferencia entre las dos columnas es la promesa de cada brazo.

| Brazo | Modo | Exactas | Candidatas | Esperadas | TP | TP exactas | FN | Falsas exactas | Clase distinta | Símbolos | No resueltas |
| --- | --- | --- | --- | --- | --- | --- | --- | --- | --- | --- | --- |
| fallback | fallback | 0 | 25 | 41 | 22 | 0 | 19 | 0 | 3 | 28 | 6/6 |
| exact | exact | 35 | 0 | 41 | 32 | 32 | 9 | 0 | 3 | 31 | 6/6 |

## Los dos brazos

El camino Python tiene dos productores y no prometen lo mismo, así que un solo
número escondería la mitad interesante.

### Brazo `fallback`

- `PythonAnalyzerMode`: `fallback`
- productor: `python-worker/index.py`
- payload `authoritative`: `false`
- propiedad exigida: **ninguna arista exacta entre símbolos**, porque el payload
  no es autoritativo y la confianza se decide en `internal/facts/semantic.go:295`.
- propiedad cumplida: `true` (0 aristas exactas, 25 candidatas)
- cobertura como `CANDIDATE`: 22 de 41 relaciones del fuente
- usos atribuidos al módulo: 0; a una función o clase: 18
- `IMPORTS_SYMBOL` sin evidencia: 0
- relaciones con clase distinta a la del fuente: 3; pares ausentes del fuente por debajo de exacta: 0

#### Casos

| Caso | Esperadas | TP | TP exactas | FN | Falsas exactas | Clase distinta | Exactas | Candidatas | Símbolos | No resueltas | Declaradas |
| --- | --- | --- | --- | --- | --- | --- | --- | --- | --- | --- | --- |
| basic | 5 | 3 | 0 | 2 | 0 | 0 | 0 | 3 | 8 | 0/0 | 1 |
| coverage | 36 | 19 | 0 | 17 | 0 | 3 | 0 | 22 | 20 | 6/6 | 53 |

#### fallback / basic

- motivos no resueltos: `IMPORT_NOT_RESOLVED`=1
- paquetes de import no resueltos: `models`
- falta: IMPORTS_SYMBOL pkg -> pkg.models.Vehicle
- falta: REEXPORTS pkg -> pkg.models.Vehicle

#### fallback / coverage

- motivos no resueltos: `IMPORT_NOT_RESOLVED`=12, `NAME_NOT_RESOLVED`=41
- paquetes de import no resueltos: `__future__`, `collections.abc`, `models`, `service`, `typing`
- falta: CALLS_DIRECT pkg.service.build -> pkg.contracts.Runner.run
- falta: CALLS_DIRECT pkg.service.build -> pkg.models.Box.get
- falta: CALLS_DIRECT pkg.service.build -> pkg.models.Vehicle.drive
- falta: IMPORTS_SYMBOL pkg -> pkg.models.Box
- falta: IMPORTS_SYMBOL pkg -> pkg.models.Car
- falta: IMPORTS_SYMBOL pkg -> pkg.models.Vehicle
- falta: IMPORTS_SYMBOL pkg -> pkg.service.build
- falta: OVERRIDES pkg.models.Car.drive -> pkg.models.Vehicle.drive
- falta: REEXPORTS pkg -> pkg.models.Box
- falta: REEXPORTS pkg -> pkg.models.Car
- falta: REEXPORTS pkg -> pkg.models.Vehicle
- falta: REEXPORTS pkg -> pkg.service.build
- falta: REFERENCES pkg.models.Box.get -> pkg.models.Box.value
- falta: REFERENCES pkg.models.Vehicle.drive -> pkg.models.Vehicle.name
- falta: REFERENCES pkg.service.build -> pkg.models.Vehicle.name
- falta: TYPE_USES pkg.service.build -> pkg.models.Box
- falta: TYPE_USES pkg.service.build -> pkg.models.Vehicle
- clase distinta: REFERENCES pkg.service.build -> pkg.models.Box
- clase distinta: REFERENCES pkg.service.build -> pkg.models.Vehicle
- clase distinta: REFERENCES pkg.service.run_callback -> pkg.models.Vehicle

### Brazo `exact`

- `PythonAnalyzerMode`: `exact`
- productor: `python-worker/pyright_index.py + pyright-langserver`
- payload `authoritative`: `true`
- propiedad exigida: **cero falsas exactas**, el contrato de `AGENTS.md`.
- propiedad cumplida: `true` (0 falsas exactas de 35 exactas)
- cobertura exacta: 32 de 41 relaciones del fuente
- usos atribuidos al módulo: 0; a una función o clase: 23
- `IMPORTS_SYMBOL` sin evidencia: 0
- relaciones con clase distinta a la del fuente: 3; pares ausentes del fuente por debajo de exacta: 0

#### Casos

| Caso | Esperadas | TP | TP exactas | FN | Falsas exactas | Clase distinta | Exactas | Candidatas | Símbolos | No resueltas | Declaradas |
| --- | --- | --- | --- | --- | --- | --- | --- | --- | --- | --- | --- |
| basic | 5 | 4 | 4 | 1 | 0 | 0 | 4 | 0 | 8 | 0/0 | 0 |
| coverage | 36 | 28 | 28 | 8 | 0 | 3 | 31 | 0 | 23 | 6/6 | 35 |

#### exact / basic

- falta: REEXPORTS pkg -> pkg.models.Vehicle

#### exact / coverage

- motivos no resueltos: `IMPORT_NOT_RESOLVED`=8, `TARGET_NOT_INDEXED`=27
- paquetes de import no resueltos: `__future__`, `collections.abc`, `typing`
- falta: CALLS_DIRECT pkg.service.build -> pkg.service.convert
- falta: OVERRIDES pkg.models.Car.drive -> pkg.models.Vehicle.drive
- falta: REEXPORTS pkg -> pkg.models.Box
- falta: REEXPORTS pkg -> pkg.models.Car
- falta: REEXPORTS pkg -> pkg.models.Vehicle
- falta: REEXPORTS pkg -> pkg.service.build
- falta: TYPE_USES pkg.service.build -> pkg.models.Box
- falta: TYPE_USES pkg.service.build -> pkg.models.Vehicle
- clase distinta: EXACT REFERENCES pkg.service.build -> pkg.models.Box
- clase distinta: EXACT REFERENCES pkg.service.build -> pkg.models.Vehicle
- clase distinta: EXACT REFERENCES pkg.service.run_callback -> pkg.models.Vehicle

## Hallazgos

Cada uno explica un número de las tablas y se nombra con archivo y línea.
Los corregidos se describen con el defecto que tenían, para que la cifra de
esta pasada no se lea sin su historia; los que no, dicen por qué.

### Brazo `fallback`

1. Un `from .x import Y` dentro de un `__init__.py` no resuelve.
   `python-worker/index.py:185` calcula la base restando `node.level` a las
   partes del módulo actual, que es correcto para `pkg.service` -- nivel 1 da
   `pkg` -- y falso para el propio paquete: el módulo de `pkg/__init__.py` ya
   **es** `pkg`, así que nivel 1 da base `models` en vez de `pkg.models`. Es
   la causa de los `IMPORTS_SYMBOL` ausentes que salen de `pkg` en los dos
   fixtures, y de los paquetes `models` y `service` en la lista de imports no
   resueltos.
2. `__all__` no se lee. Es una lista de constantes de texto y el recorrido
   sólo mira nodos `ast.Name` en posición de lectura, así que ninguna arista
   `REEXPORTS` se publica aunque `pkg/__init__.py` declare su superficie
   pública.
3. Una anotación subscrita degrada de `TYPE_USES` a `REFERENCES`.
   `is_type_position` (`python-worker/index.py:244`) reconoce el nodo que **es**
   la anotación, no uno anidado dentro de ella, así que en
   `box: Box[Vehicle]` (`testdata/python/coverage/pkg/service.py:28`) ni `Box`
   ni `Vehicle` cuentan como uso de tipo. La relación sigue ahí; la clase es
   más gruesa.
4. Una llamada por atributo no produce ninguna arista. `box.get()`,
   `runner.run()` e `item.name` (`service.py:31`) son nodos `ast.Attribute` y el
   recorrido no los visita, así que la llamada encadenada del fixture es
   invisible en el grafo.

### Brazo `exact`

1. El adaptador pedía capacidades vacías y luego asumía la respuesta
   anidada. `pyright_index.py` mandaba `"capabilities": {}` en el
   `initialize`, así que Pyright contestaba `textDocument/documentSymbol` con
   la forma plana `SymbolInformation[]`, que no lleva `children`; `visit` sólo
   anida a partir de `children`, así que todo símbolo recibía el prefijo del
   módulo y perdía su clase. `Vehicle.drive`
   (`testdata/python/coverage/pkg/models.py:23`) y `Car.drive` (`:28`) daban
   los dos `pkg.models.drive`, el normalizador publicaba dos `DEFINES` para
   una clave y `facts.Set.Validate` rechazaba el conjunto entero: el fixture
   `coverage` no se indexaba en absoluto. Ahora se anuncia
   `hierarchicalDocumentSymbolSupport` y el fixture se indexa.
2. Ninguna referencia salía de la función que la hace: el productor ponía
   `sourceId: module_id` en todas, así que `find_references` contestaba a
   granularidad de archivo. Peor, ese origen equivocado fabricaba una arista
   exacta: `EXTENDS pkg.models -> pkg.models.Vehicle` sobre un fuente que
   dice `class ElectricVehicle(Vehicle):`
   (`testdata/python/basic/pkg/models.py:6`), y un módulo no hereda de nada.
   Ahora la referencia se atribuye a la declaración que la encierra; es la
   columna `usos atribuidos al módulo`.
3. Las variables y los parámetros locales se publicaban como símbolos, así
   que una función sostenía aristas hacia sus propios locales y hacia sí
   misma. Ninguna existe en el fuente: eran dieciséis exactas falsas. Un
   local no lo puede nombrar nadie desde fuera, que es la misma regla que el
   camino de Go aplica a una declaración que no alcanza el ámbito de
   paquete.
4. Un objetivo que Pyright sitúa dentro de un archivo pero sobre ninguna
   declaración indexada resolvía al módulo, porque es el único símbolo cuyo
   rango cubre el archivo entero. Eso no es un objetivo resuelto: es uno que
   no se pudo identificar, y publicarlo era ganar una arista `EXACT` por ser
   el último candidato. Ahora se retiene como `TARGET_NOT_INDEXED`; el precio
   está en las limitaciones, con la llamada a la función `@overload`ada que
   deja de publicarse.
5. Lo que sigue sin publicarse en este brazo: `__all__` no se lee, así que
   no hay `REEXPORTS`; un acceso por atributo es un nodo `ast.Attribute` que
   el recorrido no visita, así que `box.get()`, `runner.run()` e `item.name`
   (`testdata/python/coverage/pkg/service.py:31`) no dan arista; y una
   anotación subscrita degrada a `REFERENCES`, porque el nodo anidado dentro
   de `Box[Vehicle]` no se reconoce como posición de tipo. La relación está;
   la clase es más gruesa.

## Limitaciones

- El corpus son dos fixtures de un solo paquete cada uno: prueba los contratos, no la escala.
- La verdad de referencia se escribió leyendo `testdata/python/basic` y `testdata/python/coverage`; cada expectativa cita su archivo y su línea en `auditCases`.
- Una arista se cuenta como falsa exacta sólo cuando el par (origen, destino) no existe en el fuente. Un par que existe publicado con otra clase se cuenta aparte como discrepancia de clase: la relación está, la etiqueta es más gruesa, y llamarlo arista fabricada mentiría sobre el contrato.
- Ningún productor Python emite `OVERRIDES` ni `REEXPORTS`. El acceso por atributo lo resuelve el brazo exacto -- `box.get()` y `runner.run()` dan arista -- y el fallback no: sólo recorre nodos `ast.Name` en posición de lectura, y sin analizador no podría nombrar el objetivo sin adivinarlo.
- `Callable` aparece en `coverage/pkg/service.py:22` como anotación y el valor que se le pasa en la línea 31 es un `lambda`, que no es un símbolo declarado: no hay `PASSES_AS_CALLBACK` que esperar en este corpus.
- `CALLS_DIRECT pkg.service.build -> pkg.service.convert` no se publica porque `convert` está `@overload`ada: la definición que devuelve Pyright cae dentro del módulo y sobre ninguna declaración indexada, así que el productor se niega a nombrar el módulo como objetivo. La relación no se pierde -- queda retenida como `TARGET_NOT_INDEXED` con su archivo y su posición -- y publicarla exigiría resolver la sobrecarga concreta, no elegir el único candidato que queda.
- La verdad de referencia se extendió el `2026-08-22` con dos filas -- `REFERENCES pkg.models.Box.get -> pkg.models.Box.value` y `REFERENCES pkg.models.Vehicle.drive -> pkg.models.Vehicle.name` --, después de medir. Las dos están en el fuente (`pkg/models.py:17` sobre `:14`, y `:24` sobre `:21`) y la primera versión de la verdad no las enumeró porque el productor no visitaba un atributo y nada podía observarlas. Se declara porque extender una verdad después de medirla es exactamente lo que hay que decir: no se quitó ninguna expectativa ni se relajó ningún criterio.
- La medición del brazo `exact` depende de la versión de Pyright instalada y de la typeshed que trae; la del brazo `fallback` sólo del `python3` del PATH.
- El brazo `fallback` no publicó 19 de 41 relaciones del fuente; están enumeradas por nombre en la sección del brazo.
- El brazo `fallback` publicó 3 relaciones existentes con otra clase que la que dice el fuente.
- El brazo `exact` no publicó 9 de 41 relaciones del fuente; están enumeradas por nombre en la sección del brazo.
- El brazo `exact` publicó 3 relaciones existentes con otra clase que la que dice el fuente.

## Gate

```text
PYTHON_SEMANTIC_PASS_WITH_LIMITS
```
