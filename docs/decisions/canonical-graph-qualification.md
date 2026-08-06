# Calificación del grafo canónico

**Estado:** `CANONICAL_GRAPH_PASS`

**Fecha de verificación:** 2026-08-06

Este documento emite el gate de la fase 9. Certifica que Ladygraph construye el
grafo canónico definitivo desde código real, lo verifica, lo publica de forma
atómica y deriva de él un HotSnapshot **consultable**.

## Qué se califica

| Elemento | Valor |
| --- | --- |
| Esquema canónico | versión `002` |
| Numeración de códigos | `facts.CodeFormatVersion` = 1 |
| Formato de clave durable | `hotsnapshot.StableKeyFormatVersion` = 1 |
| LadybugDB | core y binding `v0.13.1` |
| Corpus | `testdata/go/cross-repository`, tres repositorios y cuatro módulos Go |
| Plataforma | `linux/amd64`, Go 1.24, CGO habilitado |

## Cadena verificada de extremo a extremo

Los hechos **no** se escribieron a mano: se derivaron del fixture con
`goloader` y `facts.NormalizeGo`, cargando cada módulo de cada repositorio,
incluido el módulo anidado `internal/legacy` de `consumer-b`.

```text
código Go real
→ goloader + facts.NormalizeGo   3 repos, 4 paquetes, 4 archivos, 9 símbolos,
                                 15 evidencias, 32 aristas, 0 no resueltas
→ Set.Validate                   0 aristas colgantes
→ ladygraph rebuild                  las ocho etapas en PASS
→ generación 000001 publicada
→ ladygraph snapshot                 9 símbolos, 15 aristas en el CSR
```

Las ocho etapas del rebuild, con biblioteca nativa:

```text
[PASS] facts          validated 3 repositories, 4 packages, 4 files, 9 symbols, 32 edges
[PASS] staging        staged 14 canonical table(s)
[PASS] graph.next     generations/000001.tmp
[PASS] bulk load      copied 39 node(s) and 47 edge(s)
[PASS] integrity      27 of 27 canonical table(s) matched; 0 invariant violation(s)
[PASS] snapshot       hot snapshot built (3 repositories, 4 packages, 4 files,
                      9 symbols, 15 edges, 17 edge(s) not represented in the CSR)
[PASS] golden probes  3 golden probe(s) passed
[PASS] publish        published generation 000001
```

## El snapshot se deriva del grafo, no de los hechos

`ladygraph snapshot --root …` lee la generación **publicada** con `ScanCanonical` y
construye el HotSnapshot desde esas filas. Ni el escáner ni el adaptador ven un
`facts.Set`. Es la diferencia que hace significativa la calificación: lo que se
consulta es lo que de verdad quedó almacenado.

Dos construcciones consecutivas del mismo grafo emiten el mismo digest de
contenido, `0c8ce3bf…`, que no depende del reloj ni de rutas de máquina.

## El snapshot es consultable, no sólo construible

Interrogado sobre la generación publicada:

```text
snapshot: 9 símbolos, 15 aristas en el CSR

  Answer         out=0 in=4        Shape          out=0 in=2
  Compute        out=1 in=3        Shape.Area     out=2 in=1
  Legacy         out=0 in=1        Shape.Width    out=0 in=3
  Register       out=1 in=1        main           out=8 in=0
                                   main           out=3 in=0
```

Las nueve claves durables hacen ida y vuelta por `SymbolByStableKey`. Y la
consulta que justifica el proyecto —**travesía cross-repository**— funciona:

```text
travesía desde "main" (consumer-a): 7 nodos alcanzados en 2 repositorios
  depth 0  main
  depth 1  Shape, Shape.Width, Shape.Area, Answer, Register, Compute
```

`main` vive en `consumer-a`; los seis destinos están declarados en
`shared-library`. La arista cruza el límite de repositorio con confianza
`EXACT_TYPECHECKED`, y la clave del consumidor coincide con la que el proveedor
asigna a su propia declaración.

## Aristas que no entran en el CSR

De las 32 aristas del grafo, 15 entran en el CSR y **17 no**: las de contención
(`CONTAINS_PACKAGE`, `CONTAINS_FILE`, `DEFINES`) y las de paquete a paquete. El
CSR del HotSnapshot indexa símbolo a símbolo; el resto no cabe por diseño.

No es pérdida de información y se declara en cada informe:

* la contención ya está en los propios nodos, en `FileKey`, `PackageKey` y
  `RepositoryKey`;
* la dependencia entre paquetes vive en la base canónica, que sigue siendo la
  fuente de verdad.

## Dos defectos que sólo apareció al usar datos reales

Ambos estaban en `internal/hotsnapshot`, escrito y probado contra fixtures
hechas a mano. Con el grafo real, la construcción fallaba.

**1. El builder exigía riqueza descriptiva, no identidad.** Rechazaba un
repositorio sin `commit`, un paquete sin `module_path` y un símbolo sin
`signature`. Los tres son legítimamente vacíos: un checkout sin metadatos de
git no tiene commit, un paquete npm no tiene ruta de módulo Go, y una constante
o un campo no tienen firma. La validación se separó: identidad e integridad
referencial se exigen; los campos descriptivos son opcionales.

**2. `EdgeRow` no podía representar dos ocurrencias de la misma relación.** El
modelo canónico declara las relaciones semánticas `MANY_MANY` justamente porque
el mismo símbolo puede alcanzar el mismo destino desde varios sitios, y cada
ocurrencia lleva su evidencia. La fila del snapshot tiraba la clave de
evidencia, así que dos usos distintos colapsaban en filas idénticas y el
detector de duplicados rechazaba el grafo entero. En el corpus real ocurre:

```text
REFERENCES  consumer-a:main -> shared-library:Shape.Width
  evidence:file:consumer-a:main.go:158:163
  evidence:file:consumer-a:main.go:206:211
```

`EdgeRow` ganó `EvidenceKey`, presente en la ordenación, en la igualdad y en el
digest. Las dos ocurrencias sobreviven como aristas distintas.

Además, `ErrInvalidSnapshotRows` era un centinela sin detalle: rechazaba el
grafo sin decir qué fila ni por qué. Ahora cada rechazo nombra la fila, su
clave y el motivo, conservando `errors.Is`.

## Reproducción

```bash
scripts/fetch-ladybug.sh
make test-ladybug
ladygraph rebuild --facts FACTS.json --root ROOT --generation 000001 \
  --resolver-version ladygraph-0906 --snapshot-id 1
ladygraph snapshot --root ROOT
```

## Límites de esta calificación

* El corpus es el fixture Go de tres repositorios. La escala grande pertenece a
  LUQUE-1602 y las cifras de rendimiento a la fase de rendimiento.
* `IMPLEMENTS`, `EMBEDS` y `OVERRIDES` tienen tabla, se cargan, se verifican y
  se codifican, pero `facts.NormalizeGo` todavía no las produce: no aparecen en
  este grafo.
* Los dos lenguajes están calificados por separado, no combinados en un solo
  grafo.
* El snapshot vive en memoria. Su persistencia y su publicación a los lectores
  MCP pertenecen a fases posteriores.

Los dos límites intermedios quedaron cerrados el mismo día; ver la segunda
ampliación.

## Ampliación del 2026-08-06: TypeScript

LUQUE-0907 cerró el hueco que este documento registraba como límite. El grafo
TypeScript recorre ahora la misma cadena completa, con sus propias aristas
cross-repository:

```text
shared-library   4 símbolos    consumer-a  3 IMPORTS_SYMBOL
consumer-b       2 IMPORTS_SYMBOL, uno de ellos a través de un alias

[PASS] facts      3 repositories, 3 packages, 4 files, 12 symbols, 26 edges
[PASS] integrity  27 of 27 canonical table(s) matched; 0 invariant violation(s)
[PASS] snapshot   12 symbols, 7 edges in the CSR
[PASS] publish    published generation 000001

ladygraph doctor graph   PASS, los seis invariantes a cero
```

Las cinco aristas `IMPORTS_SYMBOL` son `EXACT_TYPECHECKED` y su destino coincide
exactamente con la clave que el proveedor asigna a su propia declaración,
incluidos los casos con alias (`value as republished`,
`aliasedHelper as helper`), donde el nombre local del binding no coincide con el
del proveedor. Que `doctor graph` pase lo confirma de forma independiente:
`exact_edge_without_source` y `exact_edge_without_target` exigen que ambos
extremos estén declarados, y una clave mal derivada los violaría.

## Segunda ampliación del 2026-08-06: completitud y grafo mixto

Esta tanda cierra los huecos que las tareas de la fase 9 habían ido declarando,
y publica por primera vez **una generación con los dos lenguajes dentro**.

### Clases de arista que no tenían productor

| Clase | Antes | Ahora |
| --- | --- | --- |
| `IMPLEMENTS` | sin productor | `types.Implements` sobre los tipos cargados, distinguiendo satisfacción por valor y por puntero |
| `EMBEDS` | sin productor | campo anónimo de struct y tipo embebido en interfaz, distinguiendo valor y puntero |
| `OVERRIDES` | sin productor | método del tipo exterior que oculta el promovido de un embebido, decidido por el conjunto de métodos y su profundidad |
| `EXPORTS` | sin productor | el nombre público es un símbolo de clase `export` y apunta a la declaración del mismo repositorio |
| `REEXPORTS` | sin productor | igual, cuando la declaración llega por un `from`, con identidad del proveedor si cruza de repositorio |

`IMPLEMENTS` es satisfacción **real** de interfaz, nunca coincidencia de nombres
de método: sólo hay arista si el compilador lo afirma. La interfaz vacía queda
excluida, porque la satisface todo y no informa de nada. `OVERRIDES` no modela
sobrescritura virtual —Go no la tiene— sino sombreado de método promovido.

Go no tenía dónde probar esto: el fixture cross-repository no contiene ninguna
interfaz implementada, ningún embedding ni ningún método sombreado, y es el que
mide `GO_SEMANTIC_PASS`. Se añadió `testdata/go/type-relations/` en vez de
tocarlo; el gate de precisión sigue emitiendo las mismas métricas.

### Degradaciones de TypeScript, cerradas

* Cada **miembro usado** de un import de namespace (`shared.compute`) produce
  ahora su `IMPORTS_SYMBOL`. El binding `shared` sigue sin producirla, y es
  correcto: no nombra un símbolo concreto.
* Un uso local de un binding importado (`used = helper`) produce ahora
  `REFERENCES` hacia ese binding.

### Un fallo de identidad que apareció al conectarlo

El nombre público de un export coincide a menudo con el nombre cualificado de la
declaración local en el mismo fichero: `export function foo(){}` produce ambos
como `foo`. Con los bindings de import eso no podía pasar —el ámbito de
TypeScript garantiza nombres distintos—, pero con los exports es lo normal, y
colisionaba en silencio en el mapa `(fichero, nombre cualificado)` del lado Go,
corrompiendo la resolución de aristas. El emisor reserva ahora los nombres ya
ocupados por declaraciones y por imports antes de nombrar un símbolo `export`.

### `doctor storage` reconoce el esquema canónico

Validaba siempre el esquema experimental `001`, así que una generación publicada
por `ladygraph rebuild` fallaba el diagnóstico aunque estuviese perfecta. Ahora
detecta cuál de los dos esquemas tiene la base, valida el que corresponda
—derivando las tablas exigidas de la metadata, no de una lista escrita a mano— y
**declara en su salida contra cuál validó**. Un diagnóstico que no lo dice no se
puede interpretar.

### La generación mixta

Un solo `Set` con los tres repositorios Go del fixture cross-repository, el
fixture nuevo de relaciones de tipo, y los tres repositorios TypeScript:

```text
repos=4  packages=8  files=10  symbols=55 (go=31 typescript=24)  edges=137

  CALLS_DIRECT 6    CONTAINS_FILE 10   CONTAINS_PACKAGE 8   DEFINES 55
  EMBEDS 3          EXPORTS 7          IMPLEMENTS 2         IMPORTS_SYMBOL 5
  OVERRIDES 1       PASSES_AS_CALLBACK 1                    REEXPORTS 5
  REFERENCES 21     TYPE_USES 13
```

```text
[PASS] bulk load  copied 140 node(s) and 196 edge(s)
[PASS] integrity  27 of 27 canonical table(s) matched; 0 invariant violation(s)
[PASS] snapshot   55 symbols, 64 edges in the CSR
[PASS] publish    published generation 000001

ladygraph doctor storage   PASS, schema: canonical (version 2)
ladygraph doctor graph     PASS, los seis invariantes a cero
ladygraph snapshot         PASS
```

Trece de las dieciocho clases de arista del modelo aparecen en un grafo real y
verificado, con los dos lenguajes conviviendo.

### Lo que sigue sin productor

Con honestidad, y sin inventar que está hecho:

* `ASSIGNS_FUNCTION` y `RETURNS_FUNCTION` tienen productor en ambos lenguajes;
  simplemente los fixtures publicados aquí no las ejercitan.

## Tercera ampliación del 2026-08-06: EXTENDS, PACKAGE_DEPENDS_ON y MODULE_DEPENDS_ON

Esta tanda cierra las tres últimas clases de arista sin productor. Con esto,
las dieciocho clases de `facts.EdgeKind` tienen productor en al menos un
lenguaje.

### Clases de arista que no tenían productor

| Clase | Antes | Ahora |
| --- | --- | --- |
| `EXTENDS` | sin productor | TypeScript: `class A extends B` e `interface A extends B, C`, una arista por base. Go no la tiene — el embedding ya se modela como `EMBEDS` |
| `PACKAGE_DEPENDS_ON` | sin productor | Go: `internal/goloader/packagedependencies.go` agrupa cada `Use` que cruza frontera de paquete; TypeScript: `package-dependency-resolver.ts`, ya escrito y probado desde antes, ahora cableado en `facts-cli.ts` |
| `MODULE_DEPENDS_ON` | sin productor | Go únicamente: la misma dependencia de paquete, cuando además cruza una frontera de módulo (`Container` distinto en los dos extremos). TypeScript no tiene módulos propios y nunca la emite |

### TypeScript: EXTENDS y PACKAGE_DEPENDS_ON

`extends-resolver.ts` resuelve el destino de cada base exactamente como el
resto del pipeline: local contra el índice del proyecto
(`resolveLocalSymbols`, la misma función que usa `reference-extractor.ts`), o
cross-repository reutilizando la identidad ya calculada por la resolución de
`IMPORTS_SYMBOL` — nunca una segunda resolución de fuente declarativa.
`implements` queda fuera deliberadamente: el checker prueba `extends`, pero
`implements` exigiría una comprobación de conformidad estructural que este
módulo no intenta.

`resolvePackageDependencies` ya existía, probado, sin ningún llamador; ahora
`facts-cli.ts` construye el `PackageProvider` del propio repositorio indexado
(el mismo `loadProvider` que ya usa para `--provider`) y lo llama. Un
`package.json` con una dependencia que nada importa nunca produce arista: la
prueba está en el fixture (`consumer-a` declara `@ladygraph-fixture/unused`, que
ningún fichero importa).

El wire sube a `ts-facts-v4`: dos campos nuevos, `extends` (por base de
herencia, con la misma forma que `FactExport`: local o identidad de
proveedor) y `dependencies` (por paquete real dependido). Los tres goldens
(`shared-library`, `consumer-a`, `consumer-b`) se regeneraron con
`pnpm facts`, y `ts-facts-v3` se borró.

El fixture ganó `shared-library/src/inheritance.ts` (`NamedShape extends
Shape, Named` — herencia local con dos bases, cada una su propia arista — y
la clase `Widget`) y `consumer-a/src/derived.ts` (`LabeledWidget extends
Widget`, herencia cross-repository). `shared-library/dist/` se recompiló de
verdad con `tsc`, así que el declaration map cubre las declaraciones nuevas.

Verificado con las tres normalizaciones, mergeadas y validadas:

```text
repos=3  packages=3  files=7  symbols=36  edges=86  evidence=32  unresolved=0

  CALLS_DIRECT 1        CONTAINS_FILE 7         CONTAINS_PACKAGE 3
  DEFINES 36             EXPORTS 11              EXTENDS 3
  IMPORTS_SYMBOL 6       PACKAGE_DEPENDS_ON 2     REEXPORTS 8
  REFERENCES 4           TYPE_USES 5

Set.Validate() en el grafo combinado: sin aristas colgantes.
```

Las tres `EXTENDS`: dos locales dentro de `shared-library` (`NamedShape` a
`Shape` y a `Named`) y una cross-repository (`consumer-a`'s `LabeledWidget` a
`shared-library`'s `Widget`), con la misma prueba de paridad que ya existía
para `IMPORTS_SYMBOL`: la clave que deriva el consumidor coincide con la que
el proveedor asigna a su propia declaración. Las dos `PACKAGE_DEPENDS_ON`
(`consumer-a` y `consumer-b`, ambas hacia `shared-library`) resuelven
`Package→Package` con la misma prueba de clave en ambos extremos. Ninguna
`MODULE_DEPENDS_ON`: TypeScript nunca la emite.

`ts-worker`: `pnpm check` limpio (formato, lint, `tsc --noEmit`, `vitest`, 78
tests, incluidos los 5 nuevos de `extends-resolver.test.ts`).
`TYPESCRIPT_CROSS_REPO_PASS` sigue emitiéndose (`pnpm precision`), con el
nuevo `Widget` sumado a sus métricas.

Go: `go build ./internal/...` y `go test ./internal/facts` limpios, con
cuatro pruebas nuevas — `TestNormalizeTypeScriptLocalExtendsResolveWithinOneRepository`,
`TestNormalizeTypeScriptExtendsTargetKeyMatchesProvider`,
`TestNormalizeTypeScriptPackageDependsOnTargetKeyMatchesProvider`,
`TestNormalizeTypeScriptUnusedManifestDependencyProducesNoEdge`.

### Go: PACKAGE_DEPENDS_ON y MODULE_DEPENDS_ON

`internal/goloader/packagedependencies.go` agrupa cada `Use` que cruza
frontera de paquete en un `PackageDependency` por par (source, target),
deduplicado con un testigo determinista (primero por fichero y posición).
`NormalizeGo` gana el bucle sobre `input.PackageDependencies`: resuelve el
destino local contra el mapa de paquetes ya construido, o cross-repository
contra el mismo `crossByLocation` que ya usan las `References`. Emite
`PACKAGE_DEPENDS_ON` siempre que ambos extremos resuelvan, y además
`MODULE_DEPENDS_ON` cuando el `Container` difiere entre los dos — la lectura
que decide dónde cae la frontera de módulo, documentada en el propio código.
`Confidence` es siempre `EXACT_TYPECHECKED`; `Provenance` es `GO_TYPES_USE`
(local) o `GO_OBJECT_PATH` (cross-repository), sin provenance nueva: el
mismo criterio que ya usan `References` y las relaciones de tipo.

El fixture ganó `testdata/go/type-relations/units/units.go` — un paquete que
depende de la raíz de su propio módulo, para probar `PACKAGE_DEPENDS_ON` sin
`MODULE_DEPENDS_ON` — sin tocar `testdata/go/cross-repository*`, que sigue
alimentando `GO_SEMANTIC_PASS` con las mismas métricas. Nueve pruebas nuevas:
cinco en `internal/goloader/packagedependencies_test.go`
(`TestResolvePackageDependenciesGroupsUsesIntoOnePerPair`,
`TestResolvePackageDependenciesExcludesSelfDependencies`,
`TestResolvePackageDependenciesChoosesTheEarliestWitnessRegardlessOfInputOrder`,
`TestResolvePackageDependenciesSortsDistinctPairs`,
`TestResolvePackageDependenciesIsDeterministicAndCancellable`) y cuatro en
`internal/facts/golang_test.go`
(`TestNormalizeGoEmitsPackageDependencyEdgesAcrossRepositories`,
`TestNormalizeGoEmitsPackageDependencyForANestedModuleOfTheSameRepository`,
`TestNormalizeGoEmitsIntraModulePackageDependencyWithoutModuleDependsOn`,
`TestNormalizeGoNeverDuplicatesAPackageDependencyEdge`). `go build
./internal/...`, `go vet` y `go test` de `internal/goloader` e
`internal/facts` limpios; `go run ./benchmarks/go-semantic` sigue en
`GO_SEMANTIC_PASS` con `report.md`/`results.json` byte-idénticos.

### El vocabulario completo, en una generación publicada

Los dos pendientes que este cierre dejaba abiertos se resolvieron a
continuación.

`ASSIGNS_FUNCTION` y `RETURNS_FUNCTION` tenían productor pero ningún fixture
las ejercitaba: ninguno de los corpus pasaba una función **como valor**. Se
añadió `testdata/go/type-relations/units/handlers.go`, que guarda
`geometry.Measure` en una variable de paquete y la devuelve desde una función,
más la propia `Measure` en `geometry.go`. Es la distinción que separa esas dos
clases de `CALLS_DIRECT`: la misma función, usada como valor en vez de
llamada.

Con eso, un solo `Set` con los tres repositorios Go de cross-repository, el
fixture de relaciones de tipo y los tres repositorios TypeScript ejercita
**las dieciocho clases**:

```text
repos=4  packages=9  files=14  symbols=72 (go=36 typescript=36)  edges=191

  ASSIGNS_FUNCTION 1    CALLS_DIRECT 7        CONTAINS_FILE 13
  CONTAINS_PACKAGE 9    DEFINES 72            EMBEDS 3
  EXPORTS 11            EXTENDS 3             IMPLEMENTS 2
  IMPORTS_SYMBOL 6      MODULE_DEPENDS_ON 3   OVERRIDES 1
  PACKAGE_DEPENDS_ON 6  PASSES_AS_CALLBACK 1  REEXPORTS 8
  REFERENCES 22         RETURNS_FUNCTION 1    TYPE_USES 20

  clases presentes: 18 de 18
```

Y esta vez sí atraviesa el pipeline entero hasta una generación publicada, no
sólo la normalización:

```text
[PASS] integrity  27 of 27 canonical table(s) matched; 0 invariant violation(s)
[PASS] snapshot   72 symbols, 87 edges in the CSR
[PASS] publish    published generation 000001

ladygraph doctor storage   PASS, schema: canonical (version 2)
ladygraph doctor graph     PASS, los seis invariantes a cero
ladygraph snapshot         PASS
```

Que `doctor graph` pase sobre este grafo importa más que sobre los anteriores:
las clases nuevas incluyen aristas exactas cross-repository —`EXTENDS` hacia
`Widget`, `PACKAGE_DEPENDS_ON` y `MODULE_DEPENDS_ON` entre paquetes de
repositorios distintos— y `exact_edge_without_source`/`_target` exigen que
ambos extremos estén declarados. Una sola clave mal derivada, en cualquiera de
los tres productores nuevos, lo habría roto.

No queda ninguna clase del modelo sin productor ni sin ejercitar.
